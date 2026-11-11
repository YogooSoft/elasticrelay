package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ConfigDiff describes the difference between two multi configurations, keyed by object ID.
type ConfigDiff struct {
	SourcesAdded   []string `json:"sources_added,omitempty"`
	SourcesRemoved []string `json:"sources_removed,omitempty"`
	SourcesChanged []string `json:"sources_changed,omitempty"`

	SinksAdded   []string `json:"sinks_added,omitempty"`
	SinksRemoved []string `json:"sinks_removed,omitempty"`
	SinksChanged []string `json:"sinks_changed,omitempty"`

	JobsAdded   []string `json:"jobs_added,omitempty"`
	JobsRemoved []string `json:"jobs_removed,omitempty"`
	JobsChanged []string `json:"jobs_changed,omitempty"`
	// JobsMetaOnly lists jobs where only name/description changed.
	JobsMetaOnly []string `json:"jobs_meta_only,omitempty"`

	LogLevelChanged bool `json:"log_level_changed,omitempty"`
	DLQChanged      bool `json:"dlq_changed,omitempty"`

	// RestartRequired lists changed fields that cannot be hot reloaded.
	RestartRequired []string `json:"restart_required,omitempty"`
}

// Empty reports whether the diff contains no changes at all.
func (d *ConfigDiff) Empty() bool {
	return len(d.SourcesAdded)+len(d.SourcesRemoved)+len(d.SourcesChanged)+
		len(d.SinksAdded)+len(d.SinksRemoved)+len(d.SinksChanged)+
		len(d.JobsAdded)+len(d.JobsRemoved)+len(d.JobsChanged)+len(d.JobsMetaOnly)+
		len(d.RestartRequired) == 0 && !d.LogLevelChanged && !d.DLQChanged
}

// String returns a compact single-line summary.
func (d *ConfigDiff) String() string {
	if d.Empty() {
		return "no changes"
	}
	var parts []string
	add := func(label string, ids []string) {
		if len(ids) > 0 {
			parts = append(parts, fmt.Sprintf("%s=%v", label, ids))
		}
	}
	add("sources+", d.SourcesAdded)
	add("sources-", d.SourcesRemoved)
	add("sources~", d.SourcesChanged)
	add("sinks+", d.SinksAdded)
	add("sinks-", d.SinksRemoved)
	add("sinks~", d.SinksChanged)
	add("jobs+", d.JobsAdded)
	add("jobs-", d.JobsRemoved)
	add("jobs~", d.JobsChanged)
	add("jobs_meta", d.JobsMetaOnly)
	if d.LogLevelChanged {
		parts = append(parts, "log_level")
	}
	if d.DLQChanged {
		parts = append(parts, "dlq_config")
	}
	add("restart_required", d.RestartRequired)
	return strings.Join(parts, " ")
}

// DiffMultiConfig compares two configurations. A nil old configuration means
// everything in newCfg is added.
func DiffMultiConfig(oldCfg, newCfg *MultiConfig) *ConfigDiff {
	if oldCfg == nil {
		oldCfg = &MultiConfig{}
	}
	if newCfg == nil {
		newCfg = &MultiConfig{}
	}
	d := &ConfigDiff{}

	oldSources := map[string]string{}
	for _, ds := range oldCfg.DataSources {
		ds.Name = ""
		oldSources[ds.ID] = fingerprint(ds)
	}
	newSources := map[string]string{}
	for _, ds := range newCfg.DataSources {
		ds.Name = ""
		newSources[ds.ID] = fingerprint(ds)
	}
	d.SourcesAdded, d.SourcesRemoved, d.SourcesChanged = diffMaps(oldSources, newSources)

	oldSinks := map[string]string{}
	for _, s := range oldCfg.Sinks {
		s.Name = ""
		oldSinks[s.ID] = fingerprint(s)
	}
	newSinks := map[string]string{}
	for _, s := range newCfg.Sinks {
		s.Name = ""
		newSinks[s.ID] = fingerprint(s)
	}
	d.SinksAdded, d.SinksRemoved, d.SinksChanged = diffMaps(oldSinks, newSinks)

	oldJobs, oldJobMeta := map[string]string{}, map[string]string{}
	for _, j := range oldCfg.Jobs {
		oldJobMeta[j.ID] = j.Name + "\x00" + j.Description
		j.Name, j.Description = "", ""
		oldJobs[j.ID] = fingerprint(j)
	}
	newJobs, newJobMeta := map[string]string{}, map[string]string{}
	for _, j := range newCfg.Jobs {
		newJobMeta[j.ID] = j.Name + "\x00" + j.Description
		j.Name, j.Description = "", ""
		newJobs[j.ID] = fingerprint(j)
	}
	d.JobsAdded, d.JobsRemoved, d.JobsChanged = diffMaps(oldJobs, newJobs)
	for id, fp := range newJobs {
		if oldFp, ok := oldJobs[id]; ok && oldFp == fp && oldJobMeta[id] != newJobMeta[id] {
			d.JobsMetaOnly = append(d.JobsMetaOnly, id)
		}
	}
	sort.Strings(d.JobsMetaOnly)

	d.LogLevelChanged = oldCfg.Global.LogLevel != newCfg.Global.LogLevel
	d.DLQChanged = fingerprint(oldCfg.Global.DLQConfig) != fingerprint(newCfg.Global.DLQConfig)

	if oldCfg.Global.GRPCPort != newCfg.Global.GRPCPort {
		d.RestartRequired = append(d.RestartRequired, "global.grpc_port")
	}
	if oldCfg.Global.MetricsPort != newCfg.Global.MetricsPort {
		d.RestartRequired = append(d.RestartRequired, "global.metrics_port")
	}

	return d
}

// AffectedJobIDs returns IDs of jobs in oldCfg that must be stopped to apply the diff:
// removed or changed jobs, and jobs depending on a removed or changed source/sink.
func (d *ConfigDiff) AffectedJobIDs(oldCfg *MultiConfig) map[string]bool {
	affected := map[string]bool{}
	for _, id := range d.JobsRemoved {
		affected[id] = true
	}
	for _, id := range d.JobsChanged {
		affected[id] = true
	}
	if oldCfg == nil {
		return affected
	}
	sources := toSet(d.SourcesRemoved, d.SourcesChanged)
	sinks := toSet(d.SinksRemoved, d.SinksChanged)
	for _, j := range oldCfg.Jobs {
		if sources[j.SourceID] || sinks[j.SinkID] {
			affected[j.ID] = true
		}
	}
	return affected
}

func toSet(lists ...[]string) map[string]bool {
	s := map[string]bool{}
	for _, l := range lists {
		for _, v := range l {
			s[v] = true
		}
	}
	return s
}

func diffMaps(oldM, newM map[string]string) (added, removed, changed []string) {
	for id, fp := range newM {
		oldFp, ok := oldM[id]
		switch {
		case !ok:
			added = append(added, id)
		case oldFp != fp:
			changed = append(changed, id)
		}
	}
	for id := range oldM {
		if _, ok := newM[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return
}

// fingerprint returns a canonical JSON encoding; map keys are sorted by encoding/json.
func fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("unmarshalable:%v", err)
	}
	return string(b)
}

// ApplyReport summarizes the result of applying a configuration at runtime.
type ApplyReport struct {
	Diff      *ConfigDiff       `json:"diff"`
	Started   []string          `json:"started,omitempty"`
	Stopped   []string          `json:"stopped,omitempty"`
	Restarted []string          `json:"restarted,omitempty"`
	Failed    map[string]string `json:"failed,omitempty"`
}

// String returns a compact single-line summary.
func (r *ApplyReport) String() string {
	if r == nil {
		return "<nil>"
	}
	diff := "<none>"
	if r.Diff != nil {
		diff = r.Diff.String()
	}
	return fmt.Sprintf("diff=[%s] started=%v stopped=%v restarted=%v failed=%v",
		diff, r.Started, r.Stopped, r.Restarted, r.Failed)
}

// HasFailures reports whether any object failed to apply.
func (r *ApplyReport) HasFailures() bool {
	return r != nil && len(r.Failed) > 0
}
