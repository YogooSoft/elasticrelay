package main

import (
	"context"
	"testing"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type namedConnector struct {
	pb.UnimplementedConnectorServiceServer
	name string
	got  *string
}

func (n *namedConnector) CommitCheckpoint(ctx context.Context, req *pb.CommitCheckpointRequest) (*pb.CommitCheckpointResponse, error) {
	*n.got = n.name
	return &pb.CommitCheckpointResponse{Success: true}, nil
}

func TestSwappableConnectorRoutesByJobSource(t *testing.T) {
	var got string
	s := &swappableConnector{}
	s.set(&namedConnector{name: "default", got: &got})
	s.setSource("src-a", &namedConnector{name: "a", got: &got})
	s.setSource("src-b", &namedConnector{name: "b", got: &got})
	s.setJobSourceResolver(func(jobID string) (string, bool) {
		src, ok := map[string]string{"job-a": "src-a", "job-b": "src-b", "job-x": "src-missing"}[jobID]
		return src, ok
	})

	commit := func(jobID string) error {
		_, err := s.CommitCheckpoint(context.Background(), &pb.CommitCheckpointRequest{JobId: jobID, Checkpoint: &pb.Checkpoint{}})
		return err
	}
	for jobID, want := range map[string]string{"job-a": "a", "job-b": "b", "unknown": "default", "": "default"} {
		got = ""
		if err := commit(jobID); err != nil || got != want {
			t.Fatalf("job %q served by %q (err %v), want %q", jobID, got, err, want)
		}
	}
	if err := commit("job-x"); status.Code(err) != codes.Unavailable {
		t.Fatalf("job of a source without connector must fail, got %v", err)
	}
}

func TestSyncSourceConnectors(t *testing.T) {
	r := &runtimeServices{connector: &swappableConnector{}}
	oldCfg := &config.MultiConfig{DataSources: []config.DataSourceConfig{
		{ID: "src-a", Type: "mongodb", Host: "h", Port: 27017, Database: "d"},
		{ID: "src-b", Type: "mysql", Host: "h", Port: 3306, Database: "d"},
	}}
	if err := r.syncSourceConnectors(nil, oldCfg); err != nil {
		t.Fatal(err)
	}
	a, b := r.connector.bySource["src-a"], r.connector.bySource["src-b"]
	if a == nil || b == nil {
		t.Fatalf("connectors not built: %v", r.connector.bySource)
	}

	newCfg := &config.MultiConfig{DataSources: []config.DataSourceConfig{
		oldCfg.DataSources[0],
		{ID: "src-c", Type: "postgresql", Host: "h", Port: 5432, Database: "d"},
	}}
	newCfg.DataSources[0].TableFilters = []string{"users"}
	if err := r.syncSourceConnectors(oldCfg, newCfg); err != nil {
		t.Fatal(err)
	}
	if r.connector.bySource["src-a"] == a {
		t.Fatal("changed data source must get a new connector")
	}
	if r.connector.hasSource("src-b") || !r.connector.hasSource("src-c") {
		t.Fatalf("unexpected connectors: %v", r.connector.bySource)
	}

	unchanged := r.connector.bySource["src-c"]
	if err := r.syncSourceConnectors(newCfg, newCfg); err != nil {
		t.Fatal(err)
	}
	if r.connector.bySource["src-c"] != unchanged {
		t.Fatal("unchanged data source must keep its connector")
	}
}
