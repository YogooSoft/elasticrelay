package config

import (
	"flag"
	"io"
	"strings"
	"testing"
	"time"
)

func newTestFlagSet() (*flag.FlagSet, *Options) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs, RegisterFlags(fs)
}

func TestOptionsDefaultsKeepFileMode(t *testing.T) {
	fs, o := newTestFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Validate(fs); err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	if o.Source != SourceFile || o.ConfigFile != "config.json" || o.TransformConfig != "" {
		t.Fatalf("unexpected defaults: %+v", o)
	}
	if o.WatchEnabled() || o.ExpandEnvEnabled() {
		t.Fatalf("file mode must not watch or expand env by default")
	}
	if o.ApplyMode != ApplyModeAuto || o.Debounce != 2*time.Second {
		t.Fatalf("unexpected apply defaults: %+v", o)
	}
}

func TestOptionsEnvAndFlagPrecedence(t *testing.T) {
	t.Setenv("ELASTICRELAY_CONFIG_SOURCE", "nacos")
	t.Setenv("NACOS_SERVER_ADDR", "10.0.0.1:8848")
	t.Setenv("NACOS_NAMESPACE", "esrelay")
	t.Setenv("NACOS_GROUP", "test")

	fs, o := newTestFlagSet()
	if err := fs.Parse([]string{"--nacos-group=override"}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Validate(fs); err != nil {
		t.Fatal(err)
	}
	if o.Source != SourceNacos || o.Nacos.ServerAddr != "10.0.0.1:8848" || o.Nacos.Namespace != "esrelay" {
		t.Fatalf("env values not applied: %+v", o)
	}
	if o.Nacos.Group != "override" {
		t.Fatalf("flag must override env, got group %q", o.Nacos.Group)
	}
	if !o.WatchEnabled() || !o.ExpandEnvEnabled() {
		t.Fatalf("nacos mode must watch and expand env")
	}
}

func TestOptionsValidate(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"bad source", []string{"--config-source=etcd"}, "unsupported config source"},
		{"nacos without server", []string{"--config-source=nacos"}, "nacos server address is required"},
		{"nacos without data id", []string{"--config-source=nacos", "--nacos-server-addr=a:1", "--nacos-data-id="}, "nacos data ID is required"},
		{"bad startup policy", []string{"--config-source=nacos", "--nacos-server-addr=a:1", "--nacos-startup-policy=x"}, "startup policy"},
		{"bad apply mode", []string{"--config-apply-mode=manual"}, "apply mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, o := newTestFlagSet()
			if err := fs.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			_, err := o.Validate(fs)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestOptionsWarnsAboutIgnoredFileFlags(t *testing.T) {
	fs, o := newTestFlagSet()
	if err := fs.Parse([]string{"--config-source=nacos", "--nacos-server-addr=a:1", "--config=x.json", "--nacos-password=p"}); err != nil {
		t.Fatal(err)
	}
	warnings, err := o.Validate(fs)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "--config is ignored") || !strings.Contains(joined, "process list") {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if strings.Contains(o.Describe(), "p)") || strings.Contains(o.Describe(), "password") {
		t.Fatalf("description must not contain secrets: %s", o.Describe())
	}
}
