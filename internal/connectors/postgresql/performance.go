package postgresql

import (
	"context"
	"log"
	"runtime"
	"sync"
	"time"
)

// PerformanceMonitor tracks performance metrics for the PostgreSQL connector
type PerformanceMonitor struct {
	mutex             sync.RWMutex
	connectionMetrics *ConnectionMetrics
	throughputMetrics *ThroughputMetrics
	resourceMetrics   *ResourceMetrics
	startTime         time.Time
}

// ConnectionMetrics tracks connection-related performance metrics
type ConnectionMetrics struct {
	TotalConnections    int64         `json:"total_connections"`
	ActiveConnections   int64         `json:"active_connections"`
	IdleConnections     int64         `json:"idle_connections"`
	ConnectionErrors    int64         `json:"connection_errors"`
	AverageConnectTime  time.Duration `json:"average_connect_time"`
	MaxConnectTime      time.Duration `json:"max_connect_time"`
	ConnectionTimeouts  int64         `json:"connection_timeouts"`
	PoolUtilization     float64       `json:"pool_utilization"`
}

// ThroughputMetrics tracks data processing throughput
type ThroughputMetrics struct {
	RecordsProcessed    int64         `json:"records_processed"`
	BytesProcessed      int64         `json:"bytes_processed"`
	RecordsPerSecond    float64       `json:"records_per_second"`
	BytesPerSecond      float64       `json:"bytes_per_second"`
	AverageLatency      time.Duration `json:"average_latency"`
	P99Latency          time.Duration `json:"p99_latency"`
	BatchesProcessed    int64         `json:"batches_processed"`
	AverageBatchSize    float64       `json:"average_batch_size"`
	ErrorRate           float64       `json:"error_rate"`
}

// ResourceMetrics tracks system resource usage
type ResourceMetrics struct {
	MemoryUsageMB      float64 `json:"memory_usage_mb"`
	CPUUsagePercent    float64 `json:"cpu_usage_percent"`
	GoroutineCount     int     `json:"goroutine_count"`
	GCPauseTotal       time.Duration `json:"gc_pause_total"`
	HeapSizeMB         float64 `json:"heap_size_mb"`
	HeapInUseMB        float64 `json:"heap_in_use_mb"`
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor() *PerformanceMonitor {
	return &PerformanceMonitor{
		connectionMetrics: &ConnectionMetrics{},
		throughputMetrics: &ThroughputMetrics{},
		resourceMetrics:   &ResourceMetrics{},
		startTime:         time.Now(),
	}
}

// UpdateConnectionMetrics updates connection-related metrics
func (pm *PerformanceMonitor) UpdateConnectionMetrics(stats *PoolStats, connectTime time.Duration) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.connectionMetrics.TotalConnections = int64(stats.MaxConns)
	pm.connectionMetrics.ActiveConnections = int64(stats.AcquiredConns)
	pm.connectionMetrics.IdleConnections = int64(stats.IdleConns)
	
	if stats.MaxConns > 0 {
		pm.connectionMetrics.PoolUtilization = float64(stats.AcquiredConns) / float64(stats.MaxConns)
	}

	// Update connection time metrics
	if connectTime > pm.connectionMetrics.MaxConnectTime {
		pm.connectionMetrics.MaxConnectTime = connectTime
	}
	
	// Simple moving average (in a real implementation, you'd use a proper sliding window)
	if pm.connectionMetrics.AverageConnectTime == 0 {
		pm.connectionMetrics.AverageConnectTime = connectTime
	} else {
		pm.connectionMetrics.AverageConnectTime = 
			(pm.connectionMetrics.AverageConnectTime + connectTime) / 2
	}
}

// RecordThroughput records throughput metrics
func (pm *PerformanceMonitor) RecordThroughput(records int64, bytes int64, latency time.Duration) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.throughputMetrics.RecordsProcessed += records
	pm.throughputMetrics.BytesProcessed += bytes
	pm.throughputMetrics.BatchesProcessed++

	// Calculate rates
	elapsed := time.Since(pm.startTime).Seconds()
	if elapsed > 0 {
		pm.throughputMetrics.RecordsPerSecond = float64(pm.throughputMetrics.RecordsProcessed) / elapsed
		pm.throughputMetrics.BytesPerSecond = float64(pm.throughputMetrics.BytesProcessed) / elapsed
	}

	// Update batch size average
	pm.throughputMetrics.AverageBatchSize = 
		float64(pm.throughputMetrics.RecordsProcessed) / float64(pm.throughputMetrics.BatchesProcessed)

	// Update latency metrics (simplified)
	if pm.throughputMetrics.AverageLatency == 0 {
		pm.throughputMetrics.AverageLatency = latency
		pm.throughputMetrics.P99Latency = latency
	} else {
		pm.throughputMetrics.AverageLatency = 
			(pm.throughputMetrics.AverageLatency + latency) / 2
		
		if latency > pm.throughputMetrics.P99Latency {
			pm.throughputMetrics.P99Latency = latency
		}
	}
}

// RecordError records an error for error rate calculation
func (pm *PerformanceMonitor) RecordError() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Simple error rate calculation
	totalOperations := pm.throughputMetrics.BatchesProcessed
	if totalOperations > 0 {
		errorCount := float64(totalOperations) * pm.throughputMetrics.ErrorRate + 1
		pm.throughputMetrics.ErrorRate = errorCount / float64(totalOperations + 1)
	}
}

// UpdateResourceMetrics updates system resource metrics
func (pm *PerformanceMonitor) UpdateResourceMetrics() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	pm.resourceMetrics.MemoryUsageMB = float64(memStats.Alloc) / 1024 / 1024
	pm.resourceMetrics.HeapSizeMB = float64(memStats.HeapSys) / 1024 / 1024
	pm.resourceMetrics.HeapInUseMB = float64(memStats.HeapInuse) / 1024 / 1024
	pm.resourceMetrics.GoroutineCount = runtime.NumGoroutine()
	pm.resourceMetrics.GCPauseTotal = time.Duration(memStats.PauseTotalNs)

	// CPU usage would require more sophisticated measurement
	// For now, we'll leave it as 0 (placeholder)
	pm.resourceMetrics.CPUUsagePercent = 0.0
}

// GetMetrics returns a snapshot of all performance metrics
func (pm *PerformanceMonitor) GetMetrics() (ConnectionMetrics, ThroughputMetrics, ResourceMetrics) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return *pm.connectionMetrics, *pm.throughputMetrics, *pm.resourceMetrics
}

// LogMetrics logs current performance metrics
func (pm *PerformanceMonitor) LogMetrics() {
	connMetrics, throughputMetrics, resourceMetrics := pm.GetMetrics()
	
	log.Printf("Performance Metrics:")
	log.Printf("  Connections: Active=%d, Idle=%d, Pool Utilization=%.2f%%",
		connMetrics.ActiveConnections, connMetrics.IdleConnections, connMetrics.PoolUtilization*100)
	log.Printf("  Throughput: %.2f records/sec, %.2f MB/sec, Avg Latency=%v, P99 Latency=%v",
		throughputMetrics.RecordsPerSecond, throughputMetrics.BytesPerSecond/1024/1024,
		throughputMetrics.AverageLatency, throughputMetrics.P99Latency)
	log.Printf("  Resources: %.2f MB memory, %d goroutines, %.2f MB heap",
		resourceMetrics.MemoryUsageMB, resourceMetrics.GoroutineCount, resourceMetrics.HeapSizeMB)
}

// BatchProcessor optimizes batch processing for better performance
type BatchProcessor struct {
	batchSize     int
	flushInterval time.Duration
	buffer        []interface{}
	mutex         sync.Mutex
	flushCallback func([]interface{}) error
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, flushInterval time.Duration, callback func([]interface{}) error) *BatchProcessor {
	return &BatchProcessor{
		batchSize:     batchSize,
		flushInterval: flushInterval,
		buffer:        make([]interface{}, 0, batchSize),
		flushCallback: callback,
		stopCh:        make(chan struct{}),
	}
}

// Start starts the batch processor
func (bp *BatchProcessor) Start() {
	bp.wg.Add(1)
	go bp.flushLoop()
}

// Stop stops the batch processor
func (bp *BatchProcessor) Stop() error {
	close(bp.stopCh)
	bp.wg.Wait()
	
	// Flush remaining items
	bp.mutex.Lock()
	defer bp.mutex.Unlock()
	
	if len(bp.buffer) > 0 {
		return bp.flushCallback(bp.buffer)
	}
	
	return nil
}

// Add adds an item to the batch
func (bp *BatchProcessor) Add(item interface{}) error {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()

	bp.buffer = append(bp.buffer, item)
	
	if len(bp.buffer) >= bp.batchSize {
		return bp.flush()
	}
	
	return nil
}

// flush flushes the current batch (must be called with mutex held)
func (bp *BatchProcessor) flush() error {
	if len(bp.buffer) == 0 {
		return nil
	}
	
	items := make([]interface{}, len(bp.buffer))
	copy(items, bp.buffer)
	bp.buffer = bp.buffer[:0] // Reset buffer
	
	return bp.flushCallback(items)
}

// flushLoop periodically flushes the buffer
func (bp *BatchProcessor) flushLoop() {
	defer bp.wg.Done()
	
	ticker := time.NewTicker(bp.flushInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			bp.mutex.Lock()
			if len(bp.buffer) > 0 {
				if err := bp.flush(); err != nil {
					log.Printf("Error flushing batch: %v", err)
				}
			}
			bp.mutex.Unlock()
			
		case <-bp.stopCh:
			return
		}
	}
}

// MemoryOptimizer provides memory optimization utilities
type MemoryOptimizer struct {
	gcThreshold   uint64 // Memory threshold to trigger GC (bytes)
	gcInterval    time.Duration
	lastGCTime    time.Time
	monitoring    bool
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewMemoryOptimizer creates a new memory optimizer
func NewMemoryOptimizer(gcThresholdMB uint64, gcInterval time.Duration) *MemoryOptimizer {
	return &MemoryOptimizer{
		gcThreshold: gcThresholdMB * 1024 * 1024, // Convert MB to bytes
		gcInterval:  gcInterval,
		stopCh:      make(chan struct{}),
	}
}

// Start starts memory monitoring and optimization
func (mo *MemoryOptimizer) Start() {
	mo.monitoring = true
	mo.wg.Add(1)
	go mo.monitorLoop()
}

// Stop stops memory monitoring
func (mo *MemoryOptimizer) Stop() {
	if mo.monitoring {
		close(mo.stopCh)
		mo.wg.Wait()
		mo.monitoring = false
	}
}

// monitorLoop monitors memory usage and triggers GC when needed
func (mo *MemoryOptimizer) monitorLoop() {
	defer mo.wg.Done()
	
	ticker := time.NewTicker(mo.gcInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			mo.checkMemoryAndGC()
			
		case <-mo.stopCh:
			return
		}
	}
}

// checkMemoryAndGC checks memory usage and triggers GC if needed
func (mo *MemoryOptimizer) checkMemoryAndGC() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	if memStats.Alloc > mo.gcThreshold {
		timeSinceLastGC := time.Since(mo.lastGCTime)
		
		// Only trigger GC if enough time has passed since last GC
		if timeSinceLastGC > mo.gcInterval {
			log.Printf("Memory usage high (%.2f MB), triggering GC", 
				float64(memStats.Alloc)/1024/1024)
			
			runtime.GC()
			mo.lastGCTime = time.Now()
			
			// Log memory usage after GC
			runtime.ReadMemStats(&memStats)
			log.Printf("Memory usage after GC: %.2f MB", 
				float64(memStats.Alloc)/1024/1024)
		}
	}
}

// OptimizedConnector wraps the PostgreSQL connector with performance optimizations
type OptimizedConnector struct {
	*Connector
	perfMonitor     *PerformanceMonitor
	batchProcessor  *BatchProcessor
	memoryOptimizer *MemoryOptimizer
	optimizations   *OptimizationConfig
}

// OptimizationConfig contains performance optimization settings
type OptimizationConfig struct {
	BatchSize           int           `json:"batch_size"`
	BatchFlushInterval  time.Duration `json:"batch_flush_interval"`
	GCThresholdMB       uint64        `json:"gc_threshold_mb"`
	GCInterval          time.Duration `json:"gc_interval"`
	MetricsInterval     time.Duration `json:"metrics_interval"`
	EnableBatching      bool          `json:"enable_batching"`
	EnableMemoryOptim   bool          `json:"enable_memory_optim"`
	EnableMonitoring    bool          `json:"enable_monitoring"`
}

// DefaultOptimizationConfig returns default optimization settings
func DefaultOptimizationConfig() *OptimizationConfig {
	return &OptimizationConfig{
		BatchSize:          1000,
		BatchFlushInterval: 5 * time.Second,
		GCThresholdMB:      100, // Trigger GC when memory usage exceeds 100MB
		GCInterval:         30 * time.Second,
		MetricsInterval:    60 * time.Second,
		EnableBatching:     true,
		EnableMemoryOptim:  true,
		EnableMonitoring:   true,
	}
}

// NewOptimizedConnector creates a new optimized PostgreSQL connector
func NewOptimizedConnector(baseConnector *Connector, config *OptimizationConfig) *OptimizedConnector {
	if config == nil {
		config = DefaultOptimizationConfig()
	}

	oc := &OptimizedConnector{
		Connector:     baseConnector,
		optimizations: config,
	}

	if config.EnableMonitoring {
		oc.perfMonitor = NewPerformanceMonitor()
	}

	if config.EnableBatching {
		// Example batch callback (would be customized for actual use)
		batchCallback := func(items []interface{}) error {
			log.Printf("Processing batch of %d items", len(items))
			// Actual batch processing logic would go here
			return nil
		}
		oc.batchProcessor = NewBatchProcessor(
			config.BatchSize, 
			config.BatchFlushInterval, 
			batchCallback,
		)
	}

	if config.EnableMemoryOptim {
		oc.memoryOptimizer = NewMemoryOptimizer(config.GCThresholdMB, config.GCInterval)
	}

	return oc
}

// StartOptimizations starts all enabled optimizations
func (oc *OptimizedConnector) StartOptimizations(ctx context.Context) error {
	if oc.batchProcessor != nil {
		oc.batchProcessor.Start()
		log.Printf("Started batch processor (batch size: %d, flush interval: %v)",
			oc.optimizations.BatchSize, oc.optimizations.BatchFlushInterval)
	}

	if oc.memoryOptimizer != nil {
		oc.memoryOptimizer.Start()
		log.Printf("Started memory optimizer (GC threshold: %d MB, interval: %v)",
			oc.optimizations.GCThresholdMB, oc.optimizations.GCInterval)
	}

	if oc.perfMonitor != nil {
		// Start periodic metrics logging
		go func() {
			ticker := time.NewTicker(oc.optimizations.MetricsInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					oc.perfMonitor.UpdateResourceMetrics()
					oc.perfMonitor.LogMetrics()
				}
			}
		}()
		
		log.Printf("Started performance monitoring (metrics interval: %v)",
			oc.optimizations.MetricsInterval)
	}

	return nil
}

// StopOptimizations stops all optimizations
func (oc *OptimizedConnector) StopOptimizations() error {
	if oc.batchProcessor != nil {
		if err := oc.batchProcessor.Stop(); err != nil {
			log.Printf("Error stopping batch processor: %v", err)
		}
	}

	if oc.memoryOptimizer != nil {
		oc.memoryOptimizer.Stop()
	}

	log.Printf("Stopped all performance optimizations")
	return nil
}

// RecordMetrics records performance metrics
func (oc *OptimizedConnector) RecordMetrics(records int64, bytes int64, latency time.Duration) {
	if oc.perfMonitor != nil {
		oc.perfMonitor.RecordThroughput(records, bytes, latency)
	}
}

// GetPerformanceMetrics returns current performance metrics
func (oc *OptimizedConnector) GetPerformanceMetrics() (ConnectionMetrics, ThroughputMetrics, ResourceMetrics) {
	if oc.perfMonitor != nil {
		return oc.perfMonitor.GetMetrics()
	}
	return ConnectionMetrics{}, ThroughputMetrics{}, ResourceMetrics{}
}
