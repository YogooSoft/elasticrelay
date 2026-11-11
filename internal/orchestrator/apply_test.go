package orchestrator

import (
	"context"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/config"
	"google.golang.org/grpc"
)

func applyTestConfig() *config.MultiConfig {
	return &config.MultiConfig{
		Version: "3.0",
		DataSources: []config.DataSourceConfig{
			{ID: "src-a", Type: "mysql", Host: "127.0.0.1", Port: 3306, User: "u", Database: "d", ServerID: 101},
			{ID: "src-b", Type: "postgresql", Host: "127.0.0.1", Port: 5432, User: "u", Database: "d"},
		},
		Sinks: []config.SinkConfig{
			{ID: "es-1", Type: "elasticsearch", Addresses: []string{"http://127.0.0.1:9200"}},
		},
		Jobs: []config.SyncJobConfig{
			{ID: "job-a", Name: "A", SourceID: "src-a", SinkID: "es-1"},
			{ID: "job-b", Name: "B", SourceID: "src-b", SinkID: "es-1"},
			{ID: "job-keep", Name: "K", SourceID: "src-a", SinkID: "es-1"},
		},
		Global: config.GlobalConfig{LogLevel: "info"},
	}
}

func newTestOrchestrator(t *testing.T, cfg *config.MultiConfig) *MultiOrchestrator {
	t.Helper()
	t.Chdir(t.TempDir())
	mo, err := NewMultiOrchestrator("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mo.Shutdown)
	if err := mo.LoadConfiguration(cfg); err != nil {
		t.Fatal(err)
	}
	if err := mo.CreateJobsFromConfig(); err != nil {
		t.Fatal(err)
	}
	return mo
}

func jobIDs(mo *MultiOrchestrator) []string {
	mo.jobsMux.RLock()
	defer mo.jobsMux.RUnlock()
	var ids []string
	for _, id := range []string{"job-a", "job-b", "job-c", "job-keep"} {
		if _, ok := mo.jobs[id]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestApplyConfigurationReconcilesOnlyChangedObjects(t *testing.T) {
	mo := newTestOrchestrator(t, applyTestConfig())

	mo.jobsMux.RLock()
	keepBefore := mo.jobs["job-keep"]
	mo.jobsMux.RUnlock()

	var hookOld, hookNew *config.MultiConfig
	mo.SetInfraHook(func(o, n *config.MultiConfig) error {
		hookOld, hookNew = o, n
		return nil
	})

	newCfg := applyTestConfig()
	newCfg.DataSources[1].Host = "10.0.0.2"
	newCfg.Jobs = []config.SyncJobConfig{
		newCfg.Jobs[1],
		{ID: "job-keep", Name: "K renamed", SourceID: "src-a", SinkID: "es-1"},
		{ID: "job-c", Name: "C", SourceID: "src-a", SinkID: "es-1"},
	}
	newCfg.Global.LogLevel = "debug"

	report, err := mo.ApplyConfiguration(context.Background(), newCfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasFailures() {
		t.Fatalf("unexpected failures: %v", report.Failed)
	}
	if !reflect.DeepEqual(report.Stopped, []string{"job-a", "job-b"}) {
		t.Fatalf("stopped = %v", report.Stopped)
	}

	if got := jobIDs(mo); !reflect.DeepEqual(got, []string{"job-b", "job-c", "job-keep"}) {
		t.Fatalf("jobs after apply = %v", got)
	}

	mo.jobsMux.RLock()
	keepAfter := mo.jobs["job-keep"]
	mo.jobsMux.RUnlock()
	if keepAfter != keepBefore {
		t.Fatal("unaffected job must not be recreated")
	}
	if keepAfter.Name != "K renamed" {
		t.Fatalf("metadata not updated: %s", keepAfter.Name)
	}

	srcB, err := mo.connectorManager.GetConnector("src-b")
	if err != nil || srcB.Config.Host != "10.0.0.2" {
		t.Fatalf("changed data source not re-registered: %v %+v", err, srcB)
	}
	if !srcB.ActiveJobs["job-b"] {
		t.Fatal("recreated job not attached to its data source")
	}
	srcA, _ := mo.connectorManager.GetConnector("src-a")
	if srcA.ActiveJobs["job-a"] || !srcA.ActiveJobs["job-c"] || !srcA.ActiveJobs["job-keep"] {
		t.Fatalf("unexpected active jobs on src-a: %v", srcA.ActiveJobs)
	}

	if hookOld == nil || hookNew != newCfg || hookOld.DataSources[1].Host != "127.0.0.1" {
		t.Fatal("infra hook not called with old and new configuration")
	}
	if mo.CurrentConfiguration() != newCfg {
		t.Fatal("orchestrator configuration not updated")
	}
}

func TestApplyConfigurationRemovesSourceAndSink(t *testing.T) {
	mo := newTestOrchestrator(t, applyTestConfig())

	newCfg := applyTestConfig()
	newCfg.DataSources = newCfg.DataSources[:1]
	newCfg.Sinks = append(newCfg.Sinks, config.SinkConfig{ID: "es-2", Type: "elasticsearch", Addresses: []string{"http://127.0.0.1:9201"}})
	newCfg.Jobs = []config.SyncJobConfig{{ID: "job-a", SourceID: "src-a", SinkID: "es-2"}}

	report, err := mo.ApplyConfiguration(context.Background(), newCfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.HasFailures() {
		t.Fatalf("unexpected failures: %v", report.Failed)
	}
	if _, err := mo.connectorManager.GetConnector("src-b"); err == nil {
		t.Fatal("removed data source still registered")
	}
	if _, ok := mo.sinkClients["es-2"]; !ok {
		t.Fatal("added sink has no client")
	}
	mo.jobsMux.RLock()
	defer mo.jobsMux.RUnlock()
	if len(mo.jobs) != 1 || mo.jobs["job-a"].SinkID != "es-2" {
		t.Fatalf("unexpected jobs: %v", mo.jobs)
	}
}

func TestApplyConfigurationRetriesMissingJobs(t *testing.T) {
	cfg := applyTestConfig()
	cfg.Jobs = append(cfg.Jobs, config.SyncJobConfig{ID: "job-c", SourceID: "src-a", SinkID: "missing"})
	mo := newTestOrchestrator(t, cfg)
	if got := jobIDs(mo); len(got) != 3 {
		t.Fatalf("job with unknown sink must fail at startup, got %v", got)
	}

	fixed := applyTestConfig()
	fixed.Jobs = append(fixed.Jobs, config.SyncJobConfig{ID: "job-c", SourceID: "src-a", SinkID: "es-1"})
	if _, err := mo.ApplyConfiguration(context.Background(), fixed); err != nil {
		t.Fatal(err)
	}
	if got := jobIDs(mo); len(got) != 4 {
		t.Fatalf("missing job must be created, got %v", got)
	}
}

type recordingSink struct {
	mu     sync.Mutex
	events []*pb.ChangeEvent
}

func (r *recordingSink) BulkWrite(ctx context.Context, opts ...grpc.CallOption) (grpc.ClientStreamingClient[pb.ChangeEvent, pb.BulkWriteResponse], error) {
	return &recordingStream{sink: r}, nil
}

func (r *recordingSink) DescribeIndex(ctx context.Context, in *pb.DescribeIndexRequest, opts ...grpc.CallOption) (*pb.DescribeIndexResponse, error) {
	return &pb.DescribeIndexResponse{}, nil
}

func (r *recordingSink) PutTemplate(ctx context.Context, in *pb.PutTemplateRequest, opts ...grpc.CallOption) (*pb.PutTemplateResponse, error) {
	return &pb.PutTemplateResponse{}, nil
}

func (r *recordingSink) SetupIlm(ctx context.Context, in *pb.SetupIlmRequest, opts ...grpc.CallOption) (*pb.SetupIlmResponse, error) {
	return &pb.SetupIlmResponse{}, nil
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type recordingStream struct {
	grpc.ClientStream
	sink    *recordingSink
	pending []*pb.ChangeEvent
}

func (s *recordingStream) Send(e *pb.ChangeEvent) error {
	s.pending = append(s.pending, e)
	return nil
}

func (s *recordingStream) CloseAndRecv() (*pb.BulkWriteResponse, error) {
	s.sink.mu.Lock()
	defer s.sink.mu.Unlock()
	s.sink.events = append(s.sink.events, s.pending...)
	return &pb.BulkWriteResponse{}, nil
}

func TestMultiJobStopDrainsBufferedEvents(t *testing.T) {
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	procCtx, procCancel := context.WithCancel(context.Background())
	job := &MultiJob{
		ID:         "drain-test",
		ctx:        ctx,
		cancel:     cancel,
		procCtx:    procCtx,
		procCancel: procCancel,
		runDone:    make(chan struct{}),
		procDone:   make(chan struct{}),
		drainCh:    make(chan struct{}),
		grpcAddr:   "127.0.0.1:1",
		sinkClient: sink,
		cdcEvents:  make(chan *pb.ChangeEvent, batchSize*20),
		started:    true,
	}

	go job.cdcBatchProcessor()
	go func() {
		defer close(job.runDone)
		<-job.ctx.Done()
	}()

	const total = 250
	for i := 0; i < total; i++ {
		if err := job.addToBatch(&pb.ChangeEvent{PrimaryKey: "k"}); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan struct{})
	go func() {
		job.Stop(5 * time.Second)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return")
	}

	if got := sink.count(); got != total {
		t.Fatalf("want all %d buffered events written before stop, got %d", total, got)
	}
	if err := job.addToBatch(&pb.ChangeEvent{}); err == nil {
		t.Fatal("stopped job must reject new events")
	}
	job.Stop(time.Second)
}

func TestMultiJobStopNotStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	procCtx, procCancel := context.WithCancel(context.Background())
	job := &MultiJob{ID: "idle", ctx: ctx, cancel: cancel, procCtx: procCtx, procCancel: procCancel}
	job.Stop(time.Second)
	if ctx.Err() == nil || procCtx.Err() == nil {
		t.Fatal("contexts must be cancelled")
	}
}

var _ io.Closer = (*grpc.ClientConn)(nil)
