package config

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Applier applies validated configuration to the running system.
type Applier interface {
	// ApplyConfiguration reconciles the runtime with newCfg. A returned error
	// means nothing was changed; partial failures are reported in ApplyReport.
	ApplyConfiguration(ctx context.Context, newCfg *MultiConfig) (*ApplyReport, error)
	// ApplyTransform replaces the transform rules with the given raw content.
	// Empty content means pass-through mode.
	ApplyTransform(data []byte) error
}

// ManagerOptions configures a ConfigManager.
type ManagerOptions struct {
	Debounce  time.Duration
	ExpandEnv bool
	DryRun    bool
}

// AppliedState is the configuration currently in effect.
type AppliedState struct {
	Multi             *MultiConfig
	MainRevision      string
	TransformRevision string
	Origin            string
	AppliedAt         time.Time
	LastReport        *ApplyReport
	LastError         string
	LastErrorAt       time.Time
}

// ConfigManager turns raw snapshots from a Source into applied configuration:
// it debounces, deduplicates, parses, validates and applies changes serially.
type ConfigManager struct {
	source  Source
	applier Applier
	opts    ManagerOptions

	applyMu sync.Mutex
	current atomic.Pointer[AppliedState]

	pendingMu sync.Mutex
	pending   *Snapshot
	signal    chan struct{}

	reloadSuccess  atomic.Int64
	reloadRejected atomic.Int64
	reloadPartial  atomic.Int64
}

// NewConfigManager creates a manager. Call SetInitial before Start.
func NewConfigManager(source Source, applier Applier, opts ManagerOptions) *ConfigManager {
	m := &ConfigManager{
		source:  source,
		applier: applier,
		opts:    opts,
		signal:  make(chan struct{}, 1),
	}
	m.current.Store(&AppliedState{})
	return m
}

// SetInitial records the configuration applied at startup.
func (m *ConfigManager) SetInitial(cfg *MultiConfig, snap *Snapshot) {
	m.current.Store(&AppliedState{
		Multi:             cfg,
		MainRevision:      snap.MainRevision,
		TransformRevision: snap.TransformRevision,
		Origin:            snap.Origin,
		AppliedAt:         time.Now(),
	})
	SetCurrentMultiConfig(cfg)
}

// Current returns the configuration currently in effect.
func (m *ConfigManager) Current() *AppliedState {
	return m.current.Load()
}

// Source returns the underlying configuration source.
func (m *ConfigManager) Source() Source {
	return m.source
}

// Stats returns reload counters: success, rejected, partial.
func (m *ConfigManager) Stats() (success, rejected, partial int64) {
	return m.reloadSuccess.Load(), m.reloadRejected.Load(), m.reloadPartial.Load()
}

// Start begins watching the source and processing changes until ctx is cancelled.
func (m *ConfigManager) Start(ctx context.Context) error {
	if err := m.source.Watch(ctx, m.enqueue); err != nil {
		return err
	}
	go m.loop(ctx)
	mode := "auto"
	if m.opts.DryRun {
		mode = "dry_run"
	}
	log.Printf("[config] hot reload enabled origin=%s debounce=%s mode=%s", m.source.Describe(), m.opts.Debounce, mode)
	return nil
}

// enqueue keeps only the latest snapshot; it never blocks the source.
func (m *ConfigManager) enqueue(s *Snapshot) {
	m.pendingMu.Lock()
	m.pending = s
	m.pendingMu.Unlock()
	select {
	case m.signal <- struct{}{}:
	default:
	}
}

func (m *ConfigManager) takePending() *Snapshot {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	s := m.pending
	m.pending = nil
	return s
}

func (m *ConfigManager) loop(ctx context.Context) {
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case <-m.signal:
			if m.opts.Debounce <= 0 {
				m.processPending(ctx)
				continue
			}
			if timer == nil {
				timer = time.NewTimer(m.opts.Debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(m.opts.Debounce)
			}
			timerC = timer.C
		case <-timerC:
			timerC = nil
			m.processPending(ctx)
		}
	}
}

func (m *ConfigManager) processPending(ctx context.Context) {
	if s := m.takePending(); s != nil {
		_, _ = m.Process(ctx, s, false)
	}
}

// Reload fetches the configuration from the source and applies it synchronously.
// force bypasses revision deduplication so that partial failures can be retried.
func (m *ConfigManager) Reload(ctx context.Context, force bool) (*ApplyReport, error) {
	snap, err := m.source.Load(ctx)
	if err != nil {
		m.recordError(fmt.Errorf("reload failed: %w", err))
		return nil, err
	}
	return m.Process(ctx, snap, force)
}

// Process parses, validates and applies a snapshot. The main and transform
// configurations are handled independently: a rejected main configuration
// does not block a valid transform change, and vice versa.
func (m *ConfigManager) Process(ctx context.Context, snap *Snapshot, force bool) (*ApplyReport, error) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	cur := m.current.Load()
	next := *cur

	mainChanged := force || snap.MainRevision != cur.MainRevision
	transformChanged := snap.HasTransform && (force || snap.TransformRevision != cur.TransformRevision)
	if !mainChanged && !transformChanged {
		return nil, nil
	}

	log.Printf("[config] change detected origin=%s main_revision=%s transform_revision=%s",
		snap.Origin, short(snap.MainRevision), short(snap.TransformRevision))

	var report *mainResult
	var firstErr error

	if mainChanged {
		r, err := m.applyMain(ctx, cur, snap, force)
		switch {
		case err != nil:
			firstErr = err
			next.LastError, next.LastErrorAt = err.Error(), time.Now()
		case r != nil:
			report = r
			next.Multi = r.applied
			next.MainRevision = snap.MainRevision
			next.LastReport = r.ApplyReport
			next.AppliedAt = time.Now()
			if r.HasFailures() {
				next.LastError, next.LastErrorAt = fmt.Sprintf("partial apply failures: %v", r.Failed), time.Now()
			} else {
				next.LastError = ""
			}
		}
	}

	if transformChanged {
		if m.opts.DryRun {
			log.Printf("[config] dry_run: transform configuration changed (revision=%s), not applied", short(snap.TransformRevision))
		} else if err := m.applier.ApplyTransform(snap.Transform); err != nil {
			log.Printf("[config] transform rejected revision=%s reason=%q, keeping current rules", short(snap.TransformRevision), err)
			m.reloadRejected.Add(1)
			if firstErr == nil {
				firstErr = err
			}
			next.LastError, next.LastErrorAt = "transform: "+err.Error(), time.Now()
		} else {
			log.Printf("[config] transform applied revision=%s", short(snap.TransformRevision))
			next.TransformRevision = snap.TransformRevision
		}
	}

	next.Origin = snap.Origin
	m.current.Store(&next)

	if report != nil {
		return report.ApplyReport, firstErr
	}
	return nil, firstErr
}

type mainResult struct {
	*ApplyReport
	applied *MultiConfig
}

func (m *ConfigManager) applyMain(ctx context.Context, cur *AppliedState, snap *Snapshot, force bool) (*mainResult, error) {
	cfg, err := ParseMultiConfig(snap.Main)
	if err == nil && m.opts.ExpandEnv {
		err = ExpandSecrets(cfg)
	}
	if err == nil {
		err = ValidateMultiConfig(cfg)
	}
	if err != nil {
		m.reloadRejected.Add(1)
		log.Printf("❌ [config] rejected revision=%s reason=%q, keeping current configuration", short(snap.MainRevision), err)
		return nil, err
	}

	diff := DiffMultiConfig(cur.Multi, cfg)
	if cur.Multi != nil && cur.Multi.Version != cfg.Version {
		log.Printf("⚠️  [config] configuration version changed %s -> %s", cur.Multi.Version, cfg.Version)
	}
	if len(diff.RestartRequired) > 0 {
		log.Printf("⚠️  [config] fields %v changed but cannot be hot reloaded, restart the process to apply them", diff.RestartRequired)
	}

	if m.opts.DryRun {
		log.Printf("[config] dry_run: revision=%s diff: %s (not applied)", short(snap.MainRevision), diff)
		return nil, nil
	}

	if diff.Empty() && !force {
		log.Printf("[config] revision=%s has no effective changes", short(snap.MainRevision))
		m.reloadSuccess.Add(1)
		SetCurrentMultiConfig(cfg)
		return &mainResult{ApplyReport: &ApplyReport{Diff: diff}, applied: cfg}, nil
	}

	start := time.Now()
	report, err := m.applier.ApplyConfiguration(ctx, cfg)
	if err != nil {
		m.reloadRejected.Add(1)
		log.Printf("❌ [config] apply aborted revision=%s reason=%q, keeping current configuration", short(snap.MainRevision), err)
		return nil, err
	}
	if report.Diff == nil {
		report.Diff = diff
	}

	SetCurrentMultiConfig(cfg)
	if report.HasFailures() {
		m.reloadPartial.Add(1)
		log.Printf("⚠️  [config] applied with failures revision=%s %s cost=%s", short(snap.MainRevision), report, time.Since(start).Round(time.Millisecond))
	} else {
		m.reloadSuccess.Add(1)
		log.Printf("✅ [config] applied revision=%s %s cost=%s", short(snap.MainRevision), report, time.Since(start).Round(time.Millisecond))
	}
	return &mainResult{ApplyReport: report, applied: cfg}, nil
}

func (m *ConfigManager) recordError(err error) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	cur := m.current.Load()
	next := *cur
	next.LastError, next.LastErrorAt = err.Error(), time.Now()
	m.current.Store(&next)
}

func short(rev string) string {
	if rev == "" {
		return "<none>"
	}
	return rev
}
