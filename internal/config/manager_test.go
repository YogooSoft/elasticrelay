package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu       sync.Mutex
	snap     *Snapshot
	onChange func(*Snapshot)
	readOnly bool
}

func (f *fakeSource) Load(ctx context.Context) (*Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap, nil
}

func (f *fakeSource) Watch(ctx context.Context, onChange func(*Snapshot)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onChange = onChange
	return nil
}

func (f *fakeSource) Close() error     { return nil }
func (f *fakeSource) Describe() string { return "fake://test" }
func (f *fakeSource) ReadOnly() bool   { return f.readOnly }

func (f *fakeSource) push(main, transform string) {
	var t []byte
	if transform != "" {
		t = []byte(transform)
	}
	s := NewSnapshot("fake://test", []byte(main), t, true)
	f.mu.Lock()
	f.snap = s
	cb := f.onChange
	f.mu.Unlock()
	cb(s)
}

type fakeApplier struct {
	mu         sync.Mutex
	applied    []*MultiConfig
	transforms [][]byte
	applyErr   error
	transErr   error
	report     *ApplyReport
}

func (f *fakeApplier) ApplyConfiguration(ctx context.Context, cfg *MultiConfig) (*ApplyReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	f.applied = append(f.applied, cfg)
	if f.report != nil {
		return f.report, nil
	}
	return &ApplyReport{}, nil
}

func (f *fakeApplier) ApplyTransform(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.transErr != nil {
		return f.transErr
	}
	f.transforms = append(f.transforms, data)
	return nil
}

func (f *fakeApplier) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applied), len(f.transforms)
}

func configWithLogLevel(level string) string {
	return strings.Replace(validMultiJSON, `"log_level": "info"`, `"log_level": "`+level+`"`, 1)
}

func newTestManager(t *testing.T, opts ManagerOptions) (*ConfigManager, *fakeSource, *fakeApplier) {
	t.Helper()
	t.Setenv("TEST_DB_PASSWORD", "pw")
	src := &fakeSource{}
	app := &fakeApplier{}
	m := NewConfigManager(src, app, opts)

	initial := NewSnapshot("fake://test", []byte(validMultiJSON), []byte(`{"transform_rules":[]}`), true)
	cfg, err := ParseMultiConfig(initial.Main)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandSecrets(cfg); err != nil {
		t.Fatal(err)
	}
	m.SetInitial(cfg, initial)
	return m, src, app
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestManagerDebouncesAndAppliesLatest(t *testing.T) {
	m, src, app := newTestManager(t, ManagerOptions{Debounce: 50 * time.Millisecond, ExpandEnv: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}

	src.push(configWithLogLevel("debug"), `{"transform_rules":[]}`)
	src.push(configWithLogLevel("warn"), `{"transform_rules":[]}`)
	src.push(configWithLogLevel("error"), `{"transform_rules":[]}`)

	waitFor(t, func() bool { n, _ := app.counts(); return n == 1 })
	time.Sleep(100 * time.Millisecond)
	if n, tr := app.counts(); n != 1 || tr != 0 {
		t.Fatalf("want exactly one main apply and no transform apply, got %d/%d", n, tr)
	}
	if got := app.applied[0].Global.LogLevel; got != "error" {
		t.Fatalf("latest change must win, got %s", got)
	}
	if m.Current().Multi.Global.LogLevel != "error" || m.Current().Multi.DataSources[0].Password != "pw" {
		t.Fatalf("current state not updated or secrets not expanded: %+v", m.Current().Multi)
	}
	if GetMultiConfig().Global.LogLevel != "error" {
		t.Fatal("package-level configuration not updated")
	}
}

func TestManagerDeduplicatesRevision(t *testing.T) {
	m, src, app := newTestManager(t, ManagerOptions{ExpandEnv: true})
	src.snap = NewSnapshot("fake://test", []byte(validMultiJSON), []byte(`{"transform_rules":[]}`), true)

	if r, err := m.Process(context.Background(), src.snap, false); err != nil || r != nil {
		t.Fatalf("unchanged content must be skipped: %v %v", r, err)
	}
	if n, tr := app.counts(); n != 0 || tr != 0 {
		t.Fatalf("nothing should be applied, got %d/%d", n, tr)
	}

	if _, err := m.Reload(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if n, tr := app.counts(); n != 1 || tr != 1 {
		t.Fatalf("forced reload must apply main and transform, got %d/%d", n, tr)
	}
}

func TestManagerRejectsInvalidAndKeepsCurrent(t *testing.T) {
	m, _, app := newTestManager(t, ManagerOptions{ExpandEnv: true})
	before := m.Current()

	bad := strings.Replace(validMultiJSON, `"sink_id": "es-main"`, `"sink_id": "missing"`, 1)
	_, err := m.Process(context.Background(), NewSnapshot("fake://test", []byte(bad), []byte(`{"transform_rules":[{"id":"x"}]}`), true), false)
	if err == nil || !strings.Contains(err.Error(), "unknown sink") {
		t.Fatalf("want validation error, got %v", err)
	}
	if n, tr := app.counts(); n != 0 || tr != 1 {
		t.Fatalf("invalid main must not be applied while valid transform still is, got %d/%d", n, tr)
	}
	cur := m.Current()
	if cur.MainRevision != before.MainRevision || cur.Multi != before.Multi || cur.LastError == "" {
		t.Fatalf("current main must be kept and error recorded: %+v", cur)
	}

	for _, content := range []string{"", "{broken"} {
		if _, err := m.Process(context.Background(), NewSnapshot("fake://test", []byte(content), nil, false), false); err == nil {
			t.Fatalf("content %q must be rejected", content)
		}
	}
	if _, rejected, _ := m.Stats(); rejected != 3 {
		t.Fatalf("want 3 rejections, got %d", rejected)
	}
}

func TestManagerApplyErrorKeepsCurrent(t *testing.T) {
	m, _, app := newTestManager(t, ManagerOptions{ExpandEnv: true})
	app.applyErr = errors.New("boom")
	before := m.Current().MainRevision
	if _, err := m.Process(context.Background(), NewSnapshot("fake://test", []byte(configWithLogLevel("debug")), nil, false), false); err == nil {
		t.Fatal("apply error must be returned")
	}
	if m.Current().MainRevision != before {
		t.Fatal("revision must not advance when apply fails")
	}
}

func TestManagerRejectsUndefinedPlaceholder(t *testing.T) {
	m, _, app := newTestManager(t, ManagerOptions{ExpandEnv: true})
	content := strings.Replace(configWithLogLevel("debug"), "${TEST_DB_PASSWORD}", "${UNDEFINED_SECRET_FOR_TEST}", 1)
	if _, err := m.Process(context.Background(), NewSnapshot("fake://test", []byte(content), nil, false), false); err == nil {
		t.Fatal("undefined placeholder must be rejected")
	}
	if n, _ := app.counts(); n != 0 {
		t.Fatal("nothing should be applied")
	}
}

func TestManagerDryRun(t *testing.T) {
	m, _, app := newTestManager(t, ManagerOptions{ExpandEnv: true, DryRun: true})
	before := m.Current().MainRevision
	if _, err := m.Process(context.Background(), NewSnapshot("fake://test", []byte(configWithLogLevel("debug")), []byte(`{"transform_rules":[{"id":"y"}]}`), true), false); err != nil {
		t.Fatal(err)
	}
	if n, tr := app.counts(); n != 0 || tr != 0 {
		t.Fatalf("dry run must not apply anything, got %d/%d", n, tr)
	}
	if m.Current().MainRevision != before {
		t.Fatal("dry run must not advance the revision")
	}
}

func TestManagerNoEffectiveChange(t *testing.T) {
	m, _, app := newTestManager(t, ManagerOptions{ExpandEnv: true})
	reformatted := strings.ReplaceAll(validMultiJSON, "\n", "\n\n")
	if _, err := m.Process(context.Background(), NewSnapshot("fake://test", []byte(reformatted), nil, false), false); err != nil {
		t.Fatal(err)
	}
	if n, _ := app.counts(); n != 0 {
		t.Fatal("formatting-only change must not reach the applier")
	}
}

func TestFileSourceWatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(validMultiJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	src := NewFileSource(path, "")
	defer src.Close()

	snap, err := src.Load(context.Background())
	if err != nil || snap.HasTransform {
		t.Fatalf("load: %v %+v", err, snap)
	}

	changes := make(chan *Snapshot, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := src.Watch(ctx, func(s *Snapshot) { changes <- s }); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "unrelated.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(configWithLogLevel("debug")), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case s := <-changes:
		if !strings.Contains(string(s.Main), `"debug"`) {
			t.Fatalf("unexpected content: %s", s.Main)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change notification")
	}
}
