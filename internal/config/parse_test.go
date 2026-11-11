package config

import (
	"strings"
	"testing"
)

const validMultiJSON = `{
  "version": "3.0",
  "data_sources": [{"id": "mysql-main", "type": "mysql", "host": "127.0.0.1", "port": 3306, "user": "u", "password": "${TEST_DB_PASSWORD}", "database": "d", "server_id": 100}],
  "sinks": [{"id": "es-main", "type": "elasticsearch", "addresses": ["http://127.0.0.1:9200"], "password": "es$pa${ss"}],
  "jobs": [{"id": "job-1", "source_id": "mysql-main", "sink_id": "es-main", "enabled": true}],
  "global": {"log_level": "info"}
}`

func TestDetectFormatFromBytes(t *testing.T) {
	cases := map[string]string{
		`{"version":"3.0"}`:       "3.0",
		`{"version":"2.0"}`:       "2.0",
		`{"db_host":"localhost"}`: "1.0",
	}
	for in, want := range cases {
		got, err := DetectFormatFromBytes([]byte(in))
		if err != nil || got != want {
			t.Errorf("DetectFormatFromBytes(%s) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", `{"foo":1}`, "not json"} {
		if _, err := DetectFormatFromBytes([]byte(bad)); err == nil {
			t.Errorf("DetectFormatFromBytes(%q) should fail", bad)
		}
	}
}

func TestParseMultiConfigConvertsLegacy(t *testing.T) {
	cfg, err := ParseMultiConfig([]byte(`{"db_host":"h","db_port":3306,"db_user":"u","db_name":"d","server_id":7,"es_addresses":["http://es:9200"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "2.0" || len(cfg.DataSources) != 1 || cfg.DataSources[0].Host != "h" || cfg.DataSources[0].ServerID != 7 {
		t.Fatalf("unexpected conversion: %+v", cfg)
	}
	if err := ValidateMultiConfig(cfg); err != nil {
		t.Fatalf("converted legacy config should be valid: %v", err)
	}
}

func TestValidateMultiConfig(t *testing.T) {
	cfg, err := ParseMultiConfig([]byte(validMultiJSON))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateMultiConfig(cfg); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	mutate := func(f func(c *MultiConfig)) error {
		c, _ := ParseMultiConfig([]byte(validMultiJSON))
		f(c)
		return ValidateMultiConfig(c)
	}
	checks := map[string]func(c *MultiConfig){
		"duplicate data source":        func(c *MultiConfig) { c.DataSources = append(c.DataSources, c.DataSources[0]) },
		"duplicate sink":               func(c *MultiConfig) { c.Sinks = append(c.Sinks, c.Sinks[0]) },
		"duplicate job":                func(c *MultiConfig) { c.Jobs = append(c.Jobs, c.Jobs[0]) },
		"unknown sink":                 func(c *MultiConfig) { c.Jobs[0].SinkID = "nope" },
		"unsupported data source type": func(c *MultiConfig) { c.DataSources[0].Type = "oracle" },
		"port":                         func(c *MultiConfig) { c.DataSources[0].Port = 0 },
	}
	for want, f := range checks {
		if err := mutate(f); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want error containing %q, got %v", want, err)
		}
	}
}

func TestExpandSecrets(t *testing.T) {
	cfg, _ := ParseMultiConfig([]byte(validMultiJSON))
	if err := ExpandSecrets(cfg); err == nil || !strings.Contains(err.Error(), "TEST_DB_PASSWORD") {
		t.Fatalf("undefined variable must fail, got %v", err)
	}

	t.Setenv("TEST_DB_PASSWORD", "s3cret")
	cfg, _ = ParseMultiConfig([]byte(validMultiJSON))
	if err := ExpandSecrets(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DataSources[0].Password != "s3cret" {
		t.Fatalf("placeholder not expanded: %q", cfg.DataSources[0].Password)
	}
	if cfg.Sinks[0].Password != "es$pa${ss" {
		t.Fatalf("password without a complete placeholder must be unchanged: %q", cfg.Sinks[0].Password)
	}
}

func TestMaskedMultiConfigDoesNotMutateOriginal(t *testing.T) {
	cfg, _ := ParseMultiConfig([]byte(validMultiJSON))
	masked := MaskedMultiConfig(cfg)
	if masked.DataSources[0].Password != "******" || masked.Sinks[0].Password != "******" {
		t.Fatalf("passwords not masked: %+v", masked)
	}
	if cfg.DataSources[0].Password == "******" {
		t.Fatal("original config mutated")
	}
}
