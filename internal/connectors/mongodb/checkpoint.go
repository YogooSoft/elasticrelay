package mongodb

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// MongoCheckpoint represents a MongoDB checkpoint with resume token
type MongoCheckpoint struct {
	JobID        string    `json:"job_id"`
	ResumeToken  string    `json:"resume_token"`
	Database     string    `json:"database"`
	Collection   string    `json:"collection,omitempty"`
	LastUpdate   time.Time `json:"last_update"`
	EventCount   int64     `json:"event_count"`
	ClusterTime  uint32    `json:"cluster_time,omitempty"`  // Timestamp.T
	ClusterOrder uint32    `json:"cluster_order,omitempty"` // Timestamp.I
}

// CheckpointManager manages MongoDB checkpoints
type CheckpointManager struct {
	filePath    string
	checkpoints map[string]*MongoCheckpoint
	mu          sync.RWMutex
}

// NewCheckpointManager creates a new checkpoint manager
func NewCheckpointManager(filePath string) *CheckpointManager {
	return &CheckpointManager{
		filePath:    filePath,
		checkpoints: make(map[string]*MongoCheckpoint),
	}
}

// Load loads all checkpoints from file
func (cm *CheckpointManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			cm.checkpoints = make(map[string]*MongoCheckpoint)
			return nil
		}
		return fmt.Errorf("failed to read checkpoint file: %w", err)
	}

	if err := json.Unmarshal(data, &cm.checkpoints); err != nil {
		return fmt.Errorf("failed to parse checkpoint file: %w", err)
	}

	return nil
}

// Save saves all checkpoints to file
func (cm *CheckpointManager) Save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	data, err := json.MarshalIndent(cm.checkpoints, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoints: %w", err)
	}

	if err := os.WriteFile(cm.filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint file: %w", err)
	}

	return nil
}

// Get returns checkpoint for a specific job
func (cm *CheckpointManager) Get(jobID string) (*MongoCheckpoint, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	cp, ok := cm.checkpoints[jobID]
	return cp, ok
}

// Update updates checkpoint for a specific job
func (cm *CheckpointManager) Update(checkpoint *MongoCheckpoint) error {
	cm.mu.Lock()
	checkpoint.LastUpdate = time.Now()
	cm.checkpoints[checkpoint.JobID] = checkpoint
	cm.mu.Unlock()

	return cm.Save()
}

// Delete removes checkpoint for a specific job
func (cm *CheckpointManager) Delete(jobID string) error {
	cm.mu.Lock()
	delete(cm.checkpoints, jobID)
	cm.mu.Unlock()

	return cm.Save()
}

// ListAll returns all checkpoints
func (cm *CheckpointManager) ListAll() map[string]*MongoCheckpoint {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make(map[string]*MongoCheckpoint)
	for k, v := range cm.checkpoints {
		result[k] = v
	}
	return result
}

// IncrementEventCount increments the event count for a job
func (cm *CheckpointManager) IncrementEventCount(jobID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cp, ok := cm.checkpoints[jobID]; ok {
		cp.EventCount++
	}
}

// GetEventCount returns the event count for a job
func (cm *CheckpointManager) GetEventCount(jobID string) int64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cp, ok := cm.checkpoints[jobID]; ok {
		return cp.EventCount
	}
	return 0
}
