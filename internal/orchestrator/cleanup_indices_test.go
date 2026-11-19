package orchestrator

import (
	"context"
	"sync"
	"testing"

	"github.com/yogoosoft/elasticrelay/internal/config"
)

func TestDeleteIndicesOnTableFilterRemove(t *testing.T) {
	tests := []struct {
		name   string
		opts   map[string]interface{}
		want   bool
	}{
		{"nil options", nil, false},
		{"unset", map[string]interface{}{"index_prefix": "x"}, false},
		{"true", map[string]interface{}{"delete_indices_on_table_filter_remove": true}, true},
		{"false", map[string]interface{}{"delete_indices_on_table_filter_remove": false}, false},
		{"string true", map[string]interface{}{"delete_indices_on_table_filter_remove": "true"}, true},
		{"string 1", map[string]interface{}{"delete_indices_on_table_filter_remove": "1"}, true},
		{"string false", map[string]interface{}{"delete_indices_on_table_filter_remove": "false"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &MultiJob{sinkConfig: tt.opts}
			if got := j.deleteIndicesOnTableFilterRemove(); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCleanupRemovedTableIndices(t *testing.T) {
	var mu sync.Mutex
	var calls []struct {
		index string
		sink  string
	}
	restore := deleteIndexForCleanup
	deleteIndexForCleanup = func(ctx context.Context, sinkCfg *config.SinkConfig, indexName string) error {
		mu.Lock()
		calls = append(calls, struct {
			index string
			sink  string
		}{indexName, sinkCfg.ID})
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() { deleteIndexForCleanup = restore })

	esSink := &config.SinkConfig{
		ID:   "es-1",
		Type: "elasticsearch",
		Options: map[string]interface{}{
			"index_prefix":                            "pfx",
			"delete_indices_on_table_filter_remove": true,
		},
	}
	job := &MultiJob{
		ID:            "job-1",
		removedTables: []string{"Orders", "legacy"},
		sinkConfig:    esSink.Options,
		fullSinkConfig: esSink,
	}

	job.cleanupRemovedTableIndices()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"pfx-orders", "pfx-legacy"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %d, want %d", len(calls), len(want))
	}
	for i, c := range calls {
		if c.index != want[i] {
			t.Fatalf("call %d index = %q, want %q", i, c.index, want[i])
		}
		if c.sink != "es-1" {
			t.Fatalf("call %d sink = %q", i, c.sink)
		}
	}
}

func TestCleanupRemovedTableIndicesSkipsWhenDisabled(t *testing.T) {
	called := false
	restore := deleteIndexForCleanup
	deleteIndexForCleanup = func(ctx context.Context, sinkCfg *config.SinkConfig, indexName string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { deleteIndexForCleanup = restore })

	job := &MultiJob{
		ID:            "job-1",
		removedTables: []string{"b"},
		sinkConfig:    map[string]interface{}{"index_prefix": "pfx"},
		fullSinkConfig: &config.SinkConfig{ID: "es-1", Type: "elasticsearch"},
	}
	job.cleanupRemovedTableIndices()
	if called {
		t.Fatal("must not delete when option disabled")
	}
}

func TestCleanupRemovedTableIndicesNoRemovedTables(t *testing.T) {
	called := false
	restore := deleteIndexForCleanup
	deleteIndexForCleanup = func(ctx context.Context, sinkCfg *config.SinkConfig, indexName string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { deleteIndexForCleanup = restore })

	job := &MultiJob{
		sinkConfig: map[string]interface{}{"delete_indices_on_table_filter_remove": true},
		fullSinkConfig: &config.SinkConfig{Type: "elasticsearch"},
	}
	job.cleanupRemovedTableIndices()
	if called {
		t.Fatal("must not delete when removedTables empty")
	}
}