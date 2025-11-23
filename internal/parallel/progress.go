package parallel

import (
	"sync"
	"time"
)

// ProgressManager manages progress tracking for parallel snapshot processing
type ProgressManager struct {
	mutex           sync.RWMutex
	tableTasks      map[string]*TableTask
	chunkTasks      map[string]*ChunkTask
	completedChunks int64
	totalChunks     int64
	totalRows       int64
	processedRows   int64
	startTime       time.Time
}

// NewProgressManager creates a new progress manager
func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		tableTasks: make(map[string]*TableTask),
		chunkTasks: make(map[string]*ChunkTask),
		startTime:  time.Now(),
	}
}

// RegisterTableTask registers a new table task
func (pm *ProgressManager) RegisterTableTask(task *TableTask) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.tableTasks[task.TableName] = task
	pm.totalRows += task.TotalRows
}

// RegisterChunkTask registers a new chunk task
func (pm *ProgressManager) RegisterChunkTask(chunk *ChunkTask) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.chunkTasks[chunk.ID] = chunk
	pm.totalChunks++
}

// UpdateChunkProgress updates the progress of a chunk
func (pm *ProgressManager) UpdateChunkProgress(chunk *ChunkTask) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	if existingChunk, exists := pm.chunkTasks[chunk.ID]; exists {
		// Update existing chunk
		existingChunk.Status = chunk.Status
		existingChunk.ProcessedRows = chunk.ProcessedRows
		existingChunk.CompletedAt = chunk.CompletedAt
		existingChunk.ErrorMsg = chunk.ErrorMsg

		// Update counters if completed
		if chunk.Status == ChunkStatusCompleted {
			pm.completedChunks++
			pm.processedRows += chunk.ProcessedRows
		}
	}
}

// GetOverallProgress returns overall progress information
func (pm *ProgressManager) GetOverallProgress() *ProgressInfo {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	// Calculate progress percentages
	var chunkProgress, rowProgress float64
	if pm.totalChunks > 0 {
		chunkProgress = float64(pm.completedChunks) / float64(pm.totalChunks) * 100
	}
	if pm.totalRows > 0 {
		rowProgress = float64(pm.processedRows) / float64(pm.totalRows) * 100
	}

	// Calculate processing speed
	elapsed := time.Since(pm.startTime)
	var rowsPerSecond float64
	if elapsed.Seconds() > 0 {
		rowsPerSecond = float64(pm.processedRows) / elapsed.Seconds()
	}

	// Estimate remaining time
	var etaSeconds int64
	if rowsPerSecond > 0 && pm.totalRows > pm.processedRows {
		remainingRows := pm.totalRows - pm.processedRows
		etaSeconds = int64(float64(remainingRows) / rowsPerSecond)
	}

	return &ProgressInfo{
		TotalTables:     int64(len(pm.tableTasks)),
		CompletedTables: pm.countCompletedTables(),
		TotalChunks:     pm.totalChunks,
		CompletedChunks: pm.completedChunks,
		TotalRows:       pm.totalRows,
		ProcessedRows:   pm.processedRows,
		ChunkProgress:   chunkProgress,
		RowProgress:     rowProgress,
		RowsPerSecond:   rowsPerSecond,
		ETASeconds:      etaSeconds,
		ElapsedSeconds:  int64(elapsed.Seconds()),
	}
}

// GetTableProgress returns progress for a specific table
func (pm *ProgressManager) GetTableProgress(tableName string) *TableProgressInfo {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	task, exists := pm.tableTasks[tableName]
	if !exists {
		return nil
	}

	// Count chunks for this table
	var totalChunks, completedChunks int64
	var processedRows int64

	for _, chunk := range pm.chunkTasks {
		if chunk.TableTask.TableName == tableName {
			totalChunks++
			if chunk.Status == ChunkStatusCompleted {
				completedChunks++
				processedRows += chunk.ProcessedRows
			}
		}
	}

	var progress float64
	if totalChunks > 0 {
		progress = float64(completedChunks) / float64(totalChunks) * 100
	}

	return &TableProgressInfo{
		TableName:       tableName,
		Status:          task.Status,
		TotalRows:       task.TotalRows,
		ProcessedRows:   processedRows,
		TotalChunks:     totalChunks,
		CompletedChunks: completedChunks,
		Progress:        progress,
		StartedAt:       task.StartedAt,
		CompletedAt:     task.CompletedAt,
		ErrorMsg:        task.ErrorMsg,
	}
}

// countCompletedTables counts the number of completed tables
func (pm *ProgressManager) countCompletedTables() int64 {
	var completed int64
	for _, task := range pm.tableTasks {
		if task.Status == TaskStatusCompleted {
			completed++
		}
	}
	return completed
}

// GetFailedChunks returns information about failed chunks
func (pm *ProgressManager) GetFailedChunks() []*ChunkTask {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var failed []*ChunkTask
	for _, chunk := range pm.chunkTasks {
		if chunk.Status == ChunkStatusFailed {
			failed = append(failed, chunk)
		}
	}
	return failed
}

// GetRetryingChunks returns information about chunks being retried
func (pm *ProgressManager) GetRetryingChunks() []*ChunkTask {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	var retrying []*ChunkTask
	for _, chunk := range pm.chunkTasks {
		if chunk.Status == ChunkStatusRetrying {
			retrying = append(retrying, chunk)
		}
	}
	return retrying
}

// IsCompleted checks if all processing is completed
func (pm *ProgressManager) IsCompleted() bool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return pm.completedChunks == pm.totalChunks && pm.totalChunks > 0
}

// Reset resets the progress manager
func (pm *ProgressManager) Reset() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.tableTasks = make(map[string]*TableTask)
	pm.chunkTasks = make(map[string]*ChunkTask)
	pm.completedChunks = 0
	pm.totalChunks = 0
	pm.totalRows = 0
	pm.processedRows = 0
	pm.startTime = time.Now()
}

// ProgressInfo contains overall progress information
type ProgressInfo struct {
	TotalTables     int64   `json:"total_tables"`
	CompletedTables int64   `json:"completed_tables"`
	TotalChunks     int64   `json:"total_chunks"`
	CompletedChunks int64   `json:"completed_chunks"`
	TotalRows       int64   `json:"total_rows"`
	ProcessedRows   int64   `json:"processed_rows"`
	ChunkProgress   float64 `json:"chunk_progress"`
	RowProgress     float64 `json:"row_progress"`
	RowsPerSecond   float64 `json:"rows_per_second"`
	ETASeconds      int64   `json:"eta_seconds"`
	ElapsedSeconds  int64   `json:"elapsed_seconds"`
}

// TableProgressInfo contains progress information for a specific table
type TableProgressInfo struct {
	TableName       string     `json:"table_name"`
	Status          TaskStatus `json:"status"`
	TotalRows       int64      `json:"total_rows"`
	ProcessedRows   int64      `json:"processed_rows"`
	TotalChunks     int64      `json:"total_chunks"`
	CompletedChunks int64      `json:"completed_chunks"`
	Progress        float64    `json:"progress"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ErrorMsg        string     `json:"error_msg,omitempty"`
}
