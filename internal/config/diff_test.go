package config

import (
	"reflect"
	"testing"
)

func baseConfig() *MultiConfig {
	return &MultiConfig{
		Version: "3.0",
		DataSources: []DataSourceConfig{
			{ID: "src-a", Name: "A", Type: "mysql", Host: "h1", Port: 3306, Database: "d", ServerID: 1},
			{ID: "src-b", Name: "B", Type: "postgresql", Host: "h2", Port: 5432, Database: "d"},
		},
		Sinks: []SinkConfig{
			{ID: "es-1", Name: "ES", Type: "elasticsearch", Addresses: []string{"http://es:9200"}, Options: map[string]interface{}{"index_prefix": "x"}},
		},
		Jobs: []SyncJobConfig{
			{ID: "job-a", Name: "Job A", SourceID: "src-a", SinkID: "es-1", Enabled: true},
			{ID: "job-b", Name: "Job B", SourceID: "src-b", SinkID: "es-1", Enabled: true},
		},
		Global: GlobalConfig{LogLevel: "info", GRPCPort: 50051, DLQConfig: &DLQConfig{Enabled: true, MaxRetries: 3}},
	}
}

func TestDiffNoChanges(t *testing.T) {
	if d := DiffMultiConfig(baseConfig(), baseConfig()); !d.Empty() {
		t.Fatalf("expected no changes, got %s", d)
	}
}

func TestDiffFromNilIsAllAdded(t *testing.T) {
	d := DiffMultiConfig(nil, baseConfig())
	if len(d.SourcesAdded) != 2 || len(d.SinksAdded) != 1 || len(d.JobsAdded) != 2 {
		t.Fatalf("unexpected diff: %s", d)
	}
}

func TestDiffClassifiesChanges(t *testing.T) {
	newCfg := baseConfig()
	newCfg.DataSources[0].Name = "renamed only"
	newCfg.DataSources[1].TableFilters = []string{"t1"}
	newCfg.Sinks[0].Options = map[string]interface{}{"index_prefix": "y"}
	newCfg.Jobs[0].Description = "meta only"
	newCfg.Jobs = append(newCfg.Jobs, SyncJobConfig{ID: "job-c", SourceID: "src-a", SinkID: "es-1"})
	newCfg.Global.LogLevel = "debug"
	newCfg.Global.GRPCPort = 50052
	newCfg.Global.DLQConfig = &DLQConfig{Enabled: true, MaxRetries: 5}

	d := DiffMultiConfig(baseConfig(), newCfg)
	expect := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	expect("SourcesChanged", d.SourcesChanged, []string{"src-b"})
	expect("SinksChanged", d.SinksChanged, []string{"es-1"})
	expect("JobsAdded", d.JobsAdded, []string{"job-c"})
	expect("JobsMetaOnly", d.JobsMetaOnly, []string{"job-a"})
	expect("RestartRequired", d.RestartRequired, []string{"global.grpc_port"})
	if len(d.JobsChanged) != 0 || len(d.SourcesAdded) != 0 {
		t.Errorf("unexpected changes: %s", d)
	}
	if !d.LogLevelChanged || !d.DLQChanged {
		t.Errorf("global changes not detected: %s", d)
	}
}

func TestAffectedJobIDs(t *testing.T) {
	oldCfg := baseConfig()

	newCfg := baseConfig()
	newCfg.DataSources[1].Host = "h3"
	d := DiffMultiConfig(oldCfg, newCfg)
	if got := d.AffectedJobIDs(oldCfg); !reflect.DeepEqual(got, map[string]bool{"job-b": true}) {
		t.Fatalf("source change: got %v", got)
	}

	newCfg = baseConfig()
	newCfg.Sinks[0].Addresses = []string{"http://other:9200"}
	d = DiffMultiConfig(oldCfg, newCfg)
	if got := d.AffectedJobIDs(oldCfg); len(got) != 2 {
		t.Fatalf("sink change must affect all its jobs, got %v", got)
	}

	newCfg = baseConfig()
	newCfg.Jobs[0].Enabled = false
	newCfg.Jobs = newCfg.Jobs[:1]
	d = DiffMultiConfig(oldCfg, newCfg)
	if got := d.AffectedJobIDs(oldCfg); !reflect.DeepEqual(got, map[string]bool{"job-a": true, "job-b": true}) {
		t.Fatalf("toggle and removal: got %v", got)
	}
}
