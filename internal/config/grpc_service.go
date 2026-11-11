package config

import (
	"context"
	"fmt"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCService exposes the configuration in effect over the ConfigService API.
type GRPCService struct {
	pb.UnimplementedConfigServiceServer
	manager *ConfigManager
}

// NewGRPCService creates a ConfigService backed by manager.
func NewGRPCService(manager *ConfigManager) *GRPCService {
	return &GRPCService{manager: manager}
}

// GetConfig returns the configuration in effect. The ConfigService message only
// carries a single source and sink, so the first data source and the first
// Elasticsearch sink are reported. Passwords are masked.
func (s *GRPCService) GetConfig(ctx context.Context, req *pb.GetConfigRequest) (*pb.GetConfigResponse, error) {
	cur := s.manager.Current()
	if cur == nil || cur.Multi == nil {
		return nil, status.Error(codes.Unavailable, "configuration not loaded")
	}

	out := &pb.Configuration{}
	if len(cur.Multi.DataSources) > 0 {
		ds := cur.Multi.DataSources[0]
		out.DbHost = ds.Host
		out.DbPort = int32(ds.Port)
		out.DbUser = ds.User
		out.DbPassword = maskSecret(ds.Password)
		out.DbName = ds.Database
		out.ServerId = ds.ServerID
		out.TableFilters = ds.TableFilters
	}
	for _, sink := range cur.Multi.Sinks {
		if sink.Type == "elasticsearch" {
			out.EsAddresses = sink.Addresses
			out.EsUser = sink.User
			out.EsPassword = maskSecret(sink.Password)
			break
		}
	}
	return &pb.GetConfigResponse{Config: out}, nil
}

// UpdateConfig is not supported: configuration is changed at its source.
func (s *GRPCService) UpdateConfig(ctx context.Context, req *pb.UpdateConfigRequest) (*pb.UpdateConfigResponse, error) {
	if s.manager.Source().ReadOnly() {
		return nil, status.Errorf(codes.FailedPrecondition, "configuration is managed by %s and is read-only", s.manager.Source().Describe())
	}
	return nil, status.Error(codes.Unimplemented, "edit the configuration file instead; changes are applied on reload")
}

// ReloadConfig fetches the configuration from its source and applies it,
// bypassing revision deduplication so that partial failures can be retried.
func (s *GRPCService) ReloadConfig(ctx context.Context, req *pb.ReloadConfigRequest) (*pb.ReloadConfigResponse, error) {
	report, err := s.manager.Reload(ctx, true)
	if err != nil {
		return &pb.ReloadConfigResponse{Success: false, Message: err.Error()}, nil
	}
	cur := s.manager.Current()
	msg := fmt.Sprintf("configuration reloaded from %s (revision %s)", cur.Origin, short(cur.MainRevision))
	if report != nil {
		msg += ": " + report.String()
	}
	return &pb.ReloadConfigResponse{Success: !report.HasFailures(), Message: msg}, nil
}
