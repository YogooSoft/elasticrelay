package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/config"
	mongodb_connector "github.com/yogoosoft/elasticrelay/internal/connectors/mongodb"
	mysql_connector "github.com/yogoosoft/elasticrelay/internal/connectors/mysql"
	postgresql_connector "github.com/yogoosoft/elasticrelay/internal/connectors/postgresql"
	es_sink "github.com/yogoosoft/elasticrelay/internal/sink/es"
	transform "github.com/yogoosoft/elasticrelay/internal/transform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// swappableSink is a SinkService whose implementation can be replaced at runtime.
type swappableSink struct {
	pb.UnimplementedSinkServiceServer
	current atomic.Pointer[pb.SinkServiceServer]
}

func (s *swappableSink) set(srv pb.SinkServiceServer) {
	if srv == nil {
		s.current.Store(nil)
		return
	}
	s.current.Store(&srv)
}

func (s *swappableSink) get() (pb.SinkServiceServer, error) {
	p := s.current.Load()
	if p == nil {
		return nil, status.Error(codes.Unimplemented, "no sink configured")
	}
	return *p, nil
}

func (s *swappableSink) BulkWrite(stream grpc.ClientStreamingServer[pb.ChangeEvent, pb.BulkWriteResponse]) error {
	srv, err := s.get()
	if err != nil {
		return err
	}
	return srv.BulkWrite(stream)
}

func (s *swappableSink) DescribeIndex(ctx context.Context, req *pb.DescribeIndexRequest) (*pb.DescribeIndexResponse, error) {
	srv, err := s.get()
	if err != nil {
		return nil, err
	}
	return srv.DescribeIndex(ctx, req)
}

func (s *swappableSink) PutTemplate(ctx context.Context, req *pb.PutTemplateRequest) (*pb.PutTemplateResponse, error) {
	srv, err := s.get()
	if err != nil {
		return nil, err
	}
	return srv.PutTemplate(ctx, req)
}

func (s *swappableSink) SetupIlm(ctx context.Context, req *pb.SetupIlmRequest) (*pb.SetupIlmResponse, error) {
	srv, err := s.get()
	if err != nil {
		return nil, err
	}
	return srv.SetupIlm(ctx, req)
}

// swappableConnector is a ConnectorService whose implementation can be replaced
// at runtime. Requests of a known job are served by the connector of that job's
// data source; other requests fall back to the default (first) data source.
type swappableConnector struct {
	pb.UnimplementedConnectorServiceServer
	current atomic.Pointer[pb.ConnectorServiceServer]

	mu        sync.RWMutex
	bySource  map[string]pb.ConnectorServiceServer
	jobSource func(jobID string) (string, bool)
}

// setJobSourceResolver sets how a job ID is mapped to its data source ID.
func (s *swappableConnector) setJobSourceResolver(f func(jobID string) (string, bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobSource = f
}

// setSource sets or, when srv is nil, removes the connector of a data source.
func (s *swappableConnector) setSource(sourceID string, srv pb.ConnectorServiceServer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if srv == nil {
		delete(s.bySource, sourceID)
		return
	}
	if s.bySource == nil {
		s.bySource = map[string]pb.ConnectorServiceServer{}
	}
	s.bySource[sourceID] = srv
}

func (s *swappableConnector) hasSource(sourceID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.bySource[sourceID]
	return ok
}

func (s *swappableConnector) forJob(jobID string) (pb.ConnectorServiceServer, error) {
	s.mu.RLock()
	resolve := s.jobSource
	s.mu.RUnlock()
	if resolve != nil && jobID != "" {
		if sourceID, ok := resolve(jobID); ok {
			s.mu.RLock()
			srv, ok := s.bySource[sourceID]
			s.mu.RUnlock()
			if !ok {
				return nil, status.Errorf(codes.Unavailable, "no connector for data source %q of job %q", sourceID, jobID)
			}
			return srv, nil
		}
	}
	return s.get()
}

func (s *swappableConnector) set(srv pb.ConnectorServiceServer) {
	if srv == nil {
		s.current.Store(nil)
		return
	}
	s.current.Store(&srv)
}

func (s *swappableConnector) get() (pb.ConnectorServiceServer, error) {
	p := s.current.Load()
	if p == nil {
		return nil, status.Error(codes.Unimplemented, "no connector configured")
	}
	return *p, nil
}

func (s *swappableConnector) BeginSnapshot(req *pb.BeginSnapshotRequest, stream grpc.ServerStreamingServer[pb.SnapshotChunk]) error {
	srv, err := s.forJob(req.GetJobId())
	if err != nil {
		return err
	}
	return srv.BeginSnapshot(req, stream)
}

func (s *swappableConnector) StartCdc(req *pb.StartCdcRequest, stream grpc.ServerStreamingServer[pb.ChangeEvent]) error {
	srv, err := s.forJob(req.GetJobId())
	if err != nil {
		return err
	}
	return srv.StartCdc(req, stream)
}

func (s *swappableConnector) CommitCheckpoint(ctx context.Context, req *pb.CommitCheckpointRequest) (*pb.CommitCheckpointResponse, error) {
	srv, err := s.forJob(req.GetJobId())
	if err != nil {
		return nil, err
	}
	return srv.CommitCheckpoint(ctx, req)
}

func (s *swappableConnector) LoadCheckpoint(ctx context.Context, req *pb.LoadCheckpointRequest) (*pb.LoadCheckpointResponse, error) {
	srv, err := s.forJob(req.GetJobId())
	if err != nil {
		return nil, err
	}
	return srv.LoadCheckpoint(ctx, req)
}

// firstESSink returns the first Elasticsearch sink, which backs the in-process SinkService.
func firstESSink(cfg *config.MultiConfig) *config.SinkConfig {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Sinks {
		if cfg.Sinks[i].Type == "elasticsearch" {
			return &cfg.Sinks[i]
		}
	}
	return nil
}

// firstDataSource returns the first data source, which backs the in-process ConnectorService.
func firstDataSource(cfg *config.MultiConfig) *config.DataSourceConfig {
	if cfg == nil || len(cfg.DataSources) == 0 {
		return nil
	}
	return &cfg.DataSources[0]
}

// buildSinkServer creates the sink server for the first Elasticsearch sink. If
// Elasticsearch is unreachable a DummySinkServer is returned so that events are
// routed to the DLQ instead of failing startup.
func buildSinkServer(sinkCfg *config.SinkConfig) pb.SinkServiceServer {
	if sinkCfg == nil {
		return nil
	}
	realSinkServer, err := es_sink.NewServerFromSinkConfig(sinkCfg)
	if err != nil {
		log.Printf("⚠️  Warning: failed to create es sink server for '%s': %v", sinkCfg.ID, err)
		log.Printf("⚠️  Creating dummy sink server - events will go to DLQ when sink fails")
		return &DummySinkServer{}
	}
	log.Printf("Sink Server created for '%s' (may be in DLQ-only mode)", sinkCfg.ID)
	return realSinkServer
}

// buildConnectorServer creates the connector server for a data source.
func buildConnectorServer(ds *config.DataSourceConfig) (pb.ConnectorServiceServer, error) {
	if ds == nil {
		return nil, nil
	}
	legacy := &config.Config{
		DBHost:       ds.Host,
		DBPort:       ds.Port,
		DBUser:       ds.User,
		DBPassword:   ds.Password,
		DBName:       ds.Database,
		TableFilters: ds.TableFilters,
	}
	switch ds.Type {
	case "mysql":
		legacy.ServerID = ds.ServerID
		return mysql_connector.NewServer(legacy)
	case "postgresql":
		return postgresql_connector.NewServer(legacy)
	case "mongodb":
		return mongodb_connector.NewServer(legacy)
	default:
		log.Printf("Warning: unsupported data source type '%s', connector server not created", ds.Type)
		return nil, nil
	}
}

func sameJSON(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ja) == string(jb)
}

// runtimeServices wires shared in-process services that depend on configuration.
type runtimeServices struct {
	sink      *swappableSink
	connector *swappableConnector
	transform *transform.Server
}

// rebuild replaces the sink and connector services if their backing configuration changed.
func (r *runtimeServices) rebuild(oldCfg, newCfg *config.MultiConfig) error {
	if oldSink, newSink := firstESSink(oldCfg), firstESSink(newCfg); !sameJSON(oldSink, newSink) {
		log.Printf("Rebuilding sink service for configuration change")
		r.sink.set(buildSinkServer(newSink))
	}

	if oldDS, newDS := firstDataSource(oldCfg), firstDataSource(newCfg); !sameJSON(oldDS, newDS) {
		log.Printf("Rebuilding connector service for configuration change")
		srv, err := buildConnectorServer(newDS)
		if err != nil {
			return fmt.Errorf("failed to rebuild connector service: %w", err)
		}
		r.connector.set(srv)
	}
	return r.syncSourceConnectors(oldCfg, newCfg)
}

// syncSourceConnectors keeps one connector server per data source, rebuilding
// those whose configuration changed. A nil oldCfg builds all of them.
func (r *runtimeServices) syncSourceConnectors(oldCfg, newCfg *config.MultiConfig) error {
	oldSources := map[string]config.DataSourceConfig{}
	if oldCfg != nil {
		for _, ds := range oldCfg.DataSources {
			oldSources[ds.ID] = ds
		}
	}
	var errs []error
	for _, ds := range newCfg.DataSources {
		if old, ok := oldSources[ds.ID]; ok && sameJSON(old, ds) && r.connector.hasSource(ds.ID) {
			delete(oldSources, ds.ID)
			continue
		}
		delete(oldSources, ds.ID)
		ds := ds
		srv, err := buildConnectorServer(&ds)
		if err != nil {
			r.connector.setSource(ds.ID, nil)
			errs = append(errs, fmt.Errorf("failed to build connector service for data source '%s': %w", ds.ID, err))
			continue
		}
		r.connector.setSource(ds.ID, srv)
	}
	for id := range oldSources {
		r.connector.setSource(id, nil)
	}
	return errors.Join(errs...)
}

// applyTransform replaces the transform rules; empty content means pass-through.
func (r *runtimeServices) applyTransform(data []byte) error {
	if len(data) == 0 {
		return r.transform.LoadConfigs(nil)
	}
	rules, err := transform.LoadTransformConfigFromBytes(data)
	if err != nil {
		return err
	}
	return r.transform.LoadConfigs(rules)
}
