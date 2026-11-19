package es

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yogoosoft/elasticrelay/internal/config"
)

func newESTestServer(t *testing.T, deleteStatus int) (*httptest.Server, *sync.Mutex, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var deleted []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Elastic-Product", "Elasticsearch")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"n1","cluster_name":"es","version":{"number":"8.15.0"},"tagline":"You Know, for Search"}`))
		case r.Method == http.MethodDelete:
			mu.Lock()
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/"))
			mu.Unlock()
			w.WriteHeader(deleteStatus)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, &mu, &deleted
}

func TestDeleteIndexOK(t *testing.T) {
	srv, _, deleted := newESTestServer(t, http.StatusOK)
	defer srv.Close()

	sinkCfg := &config.SinkConfig{
		Type:      "elasticsearch",
		Addresses: []string{srv.URL},
	}
	if err := DeleteIndex(context.Background(), sinkCfg, "orders"); err != nil {
		t.Fatalf("DeleteIndex: %v", err)
	}
	if got := *deleted; len(got) != 1 || got[0] != "orders" {
		t.Fatalf("deleted indices = %v", got)
	}
}

func TestDeleteIndexNotFound(t *testing.T) {
	srv, _, deleted := newESTestServer(t, http.StatusNotFound)
	defer srv.Close()

	sinkCfg := &config.SinkConfig{
		Type:      "elasticsearch",
		Addresses: []string{srv.URL},
	}
	if err := DeleteIndex(context.Background(), sinkCfg, "missing"); err != nil {
		t.Fatalf("DeleteIndex on 404 should succeed: %v", err)
	}
	if len(*deleted) != 1 {
		t.Fatalf("expected one DELETE, got %v", *deleted)
	}
}

func TestDeleteIndexError(t *testing.T) {
	srv, _, _ := newESTestServer(t, http.StatusInternalServerError)
	defer srv.Close()

	sinkCfg := &config.SinkConfig{
		Type:      "elasticsearch",
		Addresses: []string{srv.URL},
	}
	if err := DeleteIndex(context.Background(), sinkCfg, "orders"); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestDeleteIndexRequiresElasticsearchSink(t *testing.T) {
	err := DeleteIndex(context.Background(), &config.SinkConfig{Type: "kafka"}, "orders")
	if err == nil || !strings.Contains(err.Error(), "elasticsearch") {
		t.Fatalf("err = %v", err)
	}
}