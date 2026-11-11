package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/config"
	"github.com/yogoosoft/elasticrelay/internal/logger"
)

const defaultJobStopTimeout = 30 * time.Second

// InfraHook is called during ApplyConfiguration after affected jobs have been
// stopped and before jobs are started again, so that shared services (for
// example the in-process sink and connector gRPC services) can be rebuilt.
type InfraHook func(oldCfg, newCfg *config.MultiConfig) error

// SetInfraHook registers the infrastructure hook used by ApplyConfiguration.
func (mo *MultiOrchestrator) SetInfraHook(h InfraHook) {
	mo.infraHook = h
}

// SetJobStopTimeout sets how long a job may take to drain and stop.
func (mo *MultiOrchestrator) SetJobStopTimeout(d time.Duration) {
	if d > 0 {
		mo.jobStopTimeout = d
	}
}

// serviceAddress returns the address of the in-process gRPC services.
func (j *MultiJob) serviceAddress() string {
	if j.grpcAddr != "" {
		return j.grpcAddr
	}
	return "localhost:50051"
}

// Stop gracefully stops the job: it stops reading from the source, flushes all
// buffered events to the sink, commits the final checkpoint and waits for all
// job goroutines to exit. After timeout the job is cancelled forcibly; events
// not yet written are then replayed from the last committed checkpoint.
func (j *MultiJob) Stop(timeout time.Duration) {
	j.stopOnce.Do(func() {
		j.cancel()
		if !j.started {
			j.procCancel()
			j.closeResources()
			return
		}

		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case <-j.runDone:
		case <-timer.C:
			log.Printf("⚠️  MultiJob '%s': source reader did not stop within %s", j.ID, timeout)
		}

		close(j.drainCh)
		select {
		case <-j.procDone:
		case <-timer.C:
			log.Printf("⚠️  MultiJob '%s': flushing buffered events did not finish within %s, forcing stop", j.ID, timeout)
		}

		j.procCancel()
		j.closeResources()
		log.Printf("MultiJob '%s': Stopped gracefully", j.ID)
	})
}

func (j *MultiJob) closeResources() {
	if j.parallelManager != nil {
		_ = j.parallelManager.Stop()
	}
	if j.parallelDB != nil {
		_ = j.parallelDB.Close()
	}
}

// lastCheckpoint returns a copy of the job's latest in-memory checkpoint.
func (j *MultiJob) lastCheckpoint() *pb.Checkpoint {
	j.cpMutex.RLock()
	defer j.cpMutex.RUnlock()
	return j.lastCp
}

// stopJobs stops the given jobs concurrently, unregisters them, and returns
// their last in-memory checkpoints keyed by job ID.
func (mo *MultiOrchestrator) stopJobs(ids []string) map[string]*pb.Checkpoint {
	mo.jobsMux.Lock()
	jobs := make([]*MultiJob, 0, len(ids))
	for _, id := range ids {
		if job, ok := mo.jobs[id]; ok {
			jobs = append(jobs, job)
			delete(mo.jobs, id)
		}
	}
	mo.jobsMux.Unlock()

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(j *MultiJob) {
			defer wg.Done()
			j.Stop(mo.jobStopTimeout)
		}(job)
	}
	wg.Wait()

	checkpoints := make(map[string]*pb.Checkpoint, len(jobs))
	for _, job := range jobs {
		if err := mo.connectorManager.RemoveJobFromDataSource(job.SourceID, job.ID); err != nil {
			log.Printf("MultiOrchestrator: %v", err)
		}
		if cp := job.lastCheckpoint(); cp != nil {
			checkpoints[job.ID] = cp
		}
	}
	return checkpoints
}

// ApplyConfiguration reconciles running data sources, sinks and jobs with
// newCfg, rebuilding only what changed. Affected jobs are always stopped before
// their replacements start, because MySQL server IDs and PostgreSQL replication
// slots allow only one active consumer.
func (mo *MultiOrchestrator) ApplyConfiguration(ctx context.Context, newCfg *config.MultiConfig) (*config.ApplyReport, error) {
	if newCfg == nil {
		return nil, fmt.Errorf("configuration is nil")
	}
	mo.applyMux.Lock()
	defer mo.applyMux.Unlock()

	oldCfg := mo.multiConfig
	diff := config.DiffMultiConfig(oldCfg, newCfg)
	report := &config.ApplyReport{Diff: diff, Failed: map[string]string{}}

	oldSources := map[string]config.DataSourceConfig{}
	if oldCfg != nil {
		for _, ds := range oldCfg.DataSources {
			oldSources[ds.ID] = ds
		}
	}
	newSources := map[string]config.DataSourceConfig{}
	for _, ds := range newCfg.DataSources {
		newSources[ds.ID] = ds
	}
	newJobs := map[string]config.SyncJobConfig{}
	for _, j := range newCfg.Jobs {
		newJobs[j.ID] = j
	}

	// 1. Stop affected jobs.
	affected := diff.AffectedJobIDs(oldCfg)
	mo.jobsMux.RLock()
	var toStop []string
	for id := range mo.jobs {
		if affected[id] {
			toStop = append(toStop, id)
		}
	}
	mo.jobsMux.RUnlock()
	sort.Strings(toStop)

	stoppedCps := map[string]*pb.Checkpoint{}
	if len(toStop) > 0 {
		log.Printf("MultiOrchestrator: Stopping %d job(s) for configuration change: %v", len(toStop), toStop)
		stoppedCps = mo.stopJobs(toStop)
	}

	// 2. Remove changed and removed data sources and sinks.
	for _, id := range append(append([]string{}, diff.SourcesRemoved...), diff.SourcesChanged...) {
		if err := mo.connectorManager.UnregisterDataSource(id); err != nil {
			log.Printf("MultiOrchestrator: %v", err)
		}
	}
	for _, id := range append(append([]string{}, diff.SinksRemoved...), diff.SinksChanged...) {
		mo.closeSinkClient(id)
	}

	// 3. Rebuild shared in-process services.
	if mo.infraHook != nil {
		if err := mo.infraHook(oldCfg, newCfg); err != nil {
			report.Failed["services"] = err.Error()
			log.Printf("⚠️  MultiOrchestrator: Failed to rebuild shared services: %v", err)
		}
	}

	// 4. Register added and changed data sources and sinks, plus any configured
	// ones that are missing at runtime (for example after a failed apply).
	for _, ds := range newCfg.DataSources {
		if _, err := mo.connectorManager.GetConnector(ds.ID); err == nil {
			continue
		}
		ds := ds
		if err := mo.connectorManager.RegisterDataSource(&ds); err != nil {
			report.Failed["source:"+ds.ID] = err.Error()
			log.Printf("⚠️  MultiOrchestrator: Failed to register data source '%s': %v", ds.ID, err)
		}
	}
	var newSinks []config.SinkConfig
	for _, s := range newCfg.Sinks {
		if _, ok := mo.sinkClients[s.ID]; !ok {
			newSinks = append(newSinks, s)
		}
	}
	if err := mo.initializeSinkClients(newSinks); err != nil {
		report.Failed["sinks"] = err.Error()
		log.Printf("⚠️  MultiOrchestrator: Failed to initialize sink clients: %v", err)
	}

	// 5. Global settings.
	mo.multiConfig = newCfg
	if diff.LogLevelChanged && newCfg.Global.LogLevel != "" {
		logger.SetLogLevel(newCfg.Global.LogLevel)
		log.Printf("MultiOrchestrator: Log level set to %s", newCfg.Global.LogLevel)
	}
	if diff.DLQChanged {
		if err := mo.updateDLQConfiguration(newCfg.Global.DLQConfig); err != nil {
			report.Failed["dlq"] = err.Error()
			log.Printf("⚠️  MultiOrchestrator: Failed to update DLQ configuration: %v", err)
		}
	}

	// 6. Update metadata of jobs whose name/description changed only.
	mo.jobsMux.Lock()
	for _, id := range diff.JobsMetaOnly {
		if job, ok := mo.jobs[id]; ok {
			job.Name = newJobs[id].Name
			job.Description = newJobs[id].Description
		}
	}
	mo.jobsMux.Unlock()

	// 7. Start added, changed and dependency-restarted jobs.
	stopped := map[string]bool{}
	for _, id := range toStop {
		stopped[id] = true
	}
	toStart := map[string]bool{}
	for _, id := range diff.JobsAdded {
		toStart[id] = true
	}
	for _, id := range diff.JobsChanged {
		toStart[id] = true
	}
	for id := range stopped {
		if _, ok := newJobs[id]; ok {
			toStart[id] = true
		}
	}
	mo.jobsMux.RLock()
	for id := range newJobs {
		if _, running := mo.jobs[id]; !running {
			toStart[id] = true
		}
	}
	mo.jobsMux.RUnlock()

	for _, jobCfg := range newCfg.Jobs {
		if !toStart[jobCfg.ID] {
			continue
		}
		var resume *pb.Checkpoint
		if cp, ok := stoppedCps[jobCfg.ID]; ok && sameSourceIdentity(oldSources, newSources, jobCfg.SourceID, cp) {
			resume = cp
		}
		job, err := mo.createJob(jobCfg, resume)
		if err != nil {
			report.Failed["job:"+jobCfg.ID] = err.Error()
			log.Printf("⚠️  MultiOrchestrator: Failed to start job '%s': %v", jobCfg.ID, err)
			continue
		}
		if job.Enabled && stopped[jobCfg.ID] {
			report.Restarted = append(report.Restarted, jobCfg.ID)
		} else if job.Enabled {
			report.Started = append(report.Started, jobCfg.ID)
		}
		if job.Enabled && stopped[jobCfg.ID] && job.isInitialSyncEnabledInConfig() && job.shouldForceInitialSync() {
			log.Printf("⚠️  MultiOrchestrator: Job '%s' has force_initial_sync enabled and will run a full snapshot again after this reload", job.ID)
		}
	}
	for id := range stopped {
		if !contains(report.Restarted, id) {
			report.Stopped = append(report.Stopped, id)
		}
	}
	sort.Strings(report.Started)
	sort.Strings(report.Stopped)
	sort.Strings(report.Restarted)

	if len(report.Failed) == 0 {
		report.Failed = nil
	}
	return report, nil
}

// sameSourceIdentity reports whether a checkpoint taken against the old data
// source is still meaningful for the new one (same server and database).
func sameSourceIdentity(oldSources, newSources map[string]config.DataSourceConfig, sourceID string, cp *pb.Checkpoint) bool {
	o, ok1 := oldSources[sourceID]
	n, ok2 := newSources[sourceID]
	if !ok1 || !ok2 || cp == nil {
		return false
	}
	return o.Type == n.Type && o.Host == n.Host && o.Port == n.Port && o.Database == n.Database
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Shutdown gracefully stops all jobs and DLQ background workers.
func (mo *MultiOrchestrator) Shutdown() {
	mo.applyMux.Lock()
	defer mo.applyMux.Unlock()

	mo.jobsMux.RLock()
	ids := make([]string, 0, len(mo.jobs))
	for id := range mo.jobs {
		ids = append(ids, id)
	}
	mo.jobsMux.RUnlock()

	if len(ids) > 0 {
		log.Printf("MultiOrchestrator: Shutting down %d job(s)", len(ids))
		mo.stopJobs(ids)
	}
	if mo.dlqCancel != nil {
		mo.dlqCancel()
		mo.dlqCancel = nil
	}
	for id := range mo.sinkConns {
		mo.closeSinkClient(id)
	}
}

// CurrentConfiguration returns the configuration the orchestrator is running.
func (mo *MultiOrchestrator) CurrentConfiguration() *config.MultiConfig {
	mo.applyMux.Lock()
	defer mo.applyMux.Unlock()
	return mo.multiConfig
}
