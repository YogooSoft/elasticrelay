package dlq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// DLQManager manages dead letter queue operations
type DLQManager struct {
	mu          sync.RWMutex
	storagePath string
	maxRetries  int
	retryDelay  time.Duration
	items       map[string]*DLQItem
}

// DLQItem represents a failed event in the dead letter queue
type DLQItem struct {
	ID          string                 `json:"id"`
	JobID       string                 `json:"job_id"`
	Event       *pb.ChangeEvent        `json:"event"`
	ErrorMsg    string                 `json:"error_msg"`
	ErrorType   string                 `json:"error_type"` // transform, sink, etc.
	RetryCount  int                    `json:"retry_count"`
	MaxRetries  int                    `json:"max_retries"`
	FirstFailed time.Time              `json:"first_failed"`
	LastRetried *time.Time             `json:"last_retried,omitempty"`
	Status      DLQStatus              `json:"status"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Checkpoint  *pb.Checkpoint         `json:"checkpoint,omitempty"` // Associated checkpoint for replay
}

// DLQStatus represents the status of a DLQ item
type DLQStatus string

const (
	DLQStatusPending   DLQStatus = "pending"   // Waiting for retry
	DLQStatusRetrying  DLQStatus = "retrying"  // Currently being retried
	DLQStatusExhausted DLQStatus = "exhausted" // Retry limit reached
	DLQStatusResolved  DLQStatus = "resolved"  // Successfully processed
	DLQStatusDiscarded DLQStatus = "discarded" // Manually discarded
)

// DLQConfig represents DLQ configuration
type DLQConfig struct {
	StoragePath string        `json:"storage_path"`
	MaxRetries  int           `json:"max_retries"`
	RetryDelay  time.Duration `json:"retry_delay"`
	Enabled     bool          `json:"enabled"`
}

// NewDLQManager creates a new dead letter queue manager
func NewDLQManager(config *DLQConfig) (*DLQManager, error) {
	if !config.Enabled {
		return nil, nil
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(config.StoragePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create DLQ storage directory: %w", err)
	}

	dlq := &DLQManager{
		storagePath: config.StoragePath,
		maxRetries:  config.MaxRetries,
		retryDelay:  config.RetryDelay,
		items:       make(map[string]*DLQItem),
	}

	// Load existing DLQ items from disk
	if err := dlq.loadFromDisk(); err != nil {
		log.Printf("DLQ: Warning - failed to load existing items: %v", err)
	}

	log.Printf("DLQ: Initialized with storage path %s, max retries %d", config.StoragePath, config.MaxRetries)
	return dlq, nil
}

// AddFailedEvent adds a failed event to the dead letter queue
func (d *DLQManager) AddFailedEvent(jobID string, event *pb.ChangeEvent, errorType, errorMsg string, checkpoint *pb.Checkpoint) error {
	if d == nil {
		return nil // DLQ disabled
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Deduplication: Check if this event already exists in DLQ (pending/retrying status)
	// Use jobID + primary_key + checkpoint to identify unique events
	dedupeKey := d.getDedupeKey(jobID, event, checkpoint)

	for existingID, existingItem := range d.items {
		// Check if this is the same event that hasn't been resolved yet
		if d.getDedupeKey(existingItem.JobID, existingItem.Event, existingItem.Checkpoint) == dedupeKey {
			if existingItem.Status == DLQStatusPending || existingItem.Status == DLQStatusRetrying {
				// Event already in DLQ, skip adding duplicate
				log.Printf("DLQ: Skipping duplicate event for job %s, PK %s (existing item: %s, status: %s)",
					jobID, event.PrimaryKey, existingID, existingItem.Status)
				return fmt.Errorf("duplicate event already in DLQ")
			}
		}
	}

	itemID := fmt.Sprintf("%s_%s_%d", jobID, event.PrimaryKey, time.Now().UnixNano())

	item := &DLQItem{
		ID:          itemID,
		JobID:       jobID,
		Event:       event,
		ErrorMsg:    errorMsg,
		ErrorType:   errorType,
		RetryCount:  0,
		MaxRetries:  d.maxRetries,
		FirstFailed: time.Now(),
		Status:      DLQStatusPending,
		Checkpoint:  checkpoint,
		Metadata: map[string]interface{}{
			"operation":   event.Op,
			"primary_key": event.PrimaryKey,
			"data_length": len(event.Data),
		},
	}

	d.items[itemID] = item

	// Persist to disk
	if err := d.saveToDisk(); err != nil {
		log.Printf("DLQ: Warning - failed to persist item %s: %v", itemID, err)
	}

	log.Printf("DLQ: Added failed event %s for job %s (type: %s, PK: %s)", itemID, jobID, errorType, event.PrimaryKey)
	return nil
}

// getDedupeKey generates a deduplication key for an event
func (d *DLQManager) getDedupeKey(jobID string, event *pb.ChangeEvent, checkpoint *pb.Checkpoint) string {
	// Use jobID + primary_key + binlog position to uniquely identify an event
	binlogPos := ""
	if checkpoint != nil && checkpoint.MysqlBinlogFile != "" {
		binlogPos = fmt.Sprintf("%s:%d", checkpoint.MysqlBinlogFile, checkpoint.MysqlBinlogPos)
	}
	return fmt.Sprintf("%s|%s|%s", jobID, event.PrimaryKey, binlogPos)
}

// ProcessRetries processes pending retry items
func (d *DLQManager) ProcessRetries(ctx context.Context, retryFunc func(*DLQItem) error) {
	if d == nil {
		return
	}

	ticker := time.NewTicker(d.retryDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.processRetryBatch(retryFunc)
		}
	}
}

// processRetryBatch processes a batch of retry-eligible items
func (d *DLQManager) processRetryBatch(retryFunc func(*DLQItem) error) {
	d.mu.Lock()
	pendingItems := make([]*DLQItem, 0)
	totalPending := 0
	totalRetrying := 0

	for _, item := range d.items {
		if item.Status == DLQStatusPending {
			totalPending++
		} else if item.Status == DLQStatusRetrying {
			totalRetrying++
		}

		if item.Status == DLQStatusPending && item.RetryCount < item.MaxRetries {
			// Check if enough time has passed for retry (exponential backoff)
			if d.shouldRetry(item) {
				pendingItems = append(pendingItems, item)
			}
		}
	}
	d.mu.Unlock()

	if len(pendingItems) > 0 {
		log.Printf("DLQ: Processing retry batch - %d items to retry (total pending: %d, retrying: %d)",
			len(pendingItems), totalPending, totalRetrying)
	}

	// Process retries outside of the lock
	successCount := 0
	failCount := 0
	for _, item := range pendingItems {
		d.retryItem(item, retryFunc)
		// Check the result
		d.mu.RLock()
		if item.Status == DLQStatusResolved {
			successCount++
		} else {
			failCount++
		}
		d.mu.RUnlock()
	}

	if len(pendingItems) > 0 {
		log.Printf("DLQ: Retry batch completed - %d succeeded, %d failed", successCount, failCount)
	}
}

// shouldRetry determines if an item should be retried based on backoff strategy
func (d *DLQManager) shouldRetry(item *DLQItem) bool {
	if item.LastRetried == nil {
		return true // First retry
	}

	// Exponential backoff: delay = baseDelay * 2^retryCount
	backoffDelay := d.retryDelay * time.Duration(1<<item.RetryCount)
	return time.Since(*item.LastRetried) >= backoffDelay
}

// retryItem attempts to retry processing a single DLQ item
func (d *DLQManager) retryItem(item *DLQItem, retryFunc func(*DLQItem) error) {
	d.mu.Lock()
	item.Status = DLQStatusRetrying
	now := time.Now()
	item.LastRetried = &now
	d.mu.Unlock()

	log.Printf("DLQ: Retrying item %s (attempt %d/%d)", item.ID, item.RetryCount+1, item.MaxRetries)

	err := retryFunc(item)

	d.mu.Lock()
	defer d.mu.Unlock()

	item.RetryCount++

	if err == nil {
		// Success
		item.Status = DLQStatusResolved
		log.Printf("DLQ: Successfully retried item %s", item.ID)
	} else if item.RetryCount >= item.MaxRetries {
		// Exhausted retries
		item.Status = DLQStatusExhausted
		item.ErrorMsg = fmt.Sprintf("Final retry failed: %v", err)
		log.Printf("DLQ: Item %s exhausted retries (final error: %v)", item.ID, err)
	} else {
		// Will retry again
		item.Status = DLQStatusPending
		item.ErrorMsg = fmt.Sprintf("Retry %d failed: %v", item.RetryCount, err)
		log.Printf("DLQ: Item %s retry %d failed: %v", item.ID, item.RetryCount, err)
	}

	// Persist changes
	if err := d.saveToDisk(); err != nil {
		log.Printf("DLQ: Warning - failed to persist retry state for item %s: %v", item.ID, err)
	}
}

// GetStats returns DLQ statistics
func (d *DLQManager) GetStats() map[string]int {
	if d == nil {
		return map[string]int{}
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := map[string]int{
		"total":     len(d.items),
		"pending":   0,
		"retrying":  0,
		"exhausted": 0,
		"resolved":  0,
		"discarded": 0,
	}

	for _, item := range d.items {
		stats[string(item.Status)]++
	}

	return stats
}

// ListItems returns DLQ items with optional filtering
func (d *DLQManager) ListItems(status DLQStatus, limit int) []*DLQItem {
	if d == nil {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	items := make([]*DLQItem, 0)
	count := 0

	for _, item := range d.items {
		if status != "" && item.Status != status {
			continue
		}

		items = append(items, item)
		count++

		if limit > 0 && count >= limit {
			break
		}
	}

	return items
}

// DiscardItem manually discards a DLQ item
func (d *DLQManager) DiscardItem(itemID string) error {
	if d == nil {
		return fmt.Errorf("DLQ not enabled")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	item, exists := d.items[itemID]
	if !exists {
		return fmt.Errorf("DLQ item %s not found", itemID)
	}

	item.Status = DLQStatusDiscarded
	log.Printf("DLQ: Manually discarded item %s", itemID)

	return d.saveToDisk()
}

// loadFromDisk loads DLQ items from persistent storage
func (d *DLQManager) loadFromDisk() error {
	filePath := filepath.Join(d.storagePath, "dlq_items.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist yet, that's ok
		}
		return err
	}

	var items map[string]*DLQItem
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("failed to unmarshal DLQ items: %w", err)
	}

	d.items = items
	log.Printf("DLQ: Loaded %d items from disk", len(items))
	return nil
}

// saveToDisk persists DLQ items to disk
func (d *DLQManager) saveToDisk() error {
	filePath := filepath.Join(d.storagePath, "dlq_items.json")

	data, err := json.MarshalIndent(d.items, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal DLQ items: %w", err)
	}

	return os.WriteFile(filePath, data, 0644)
}

// CleanupResolved removes resolved and discarded items older than the specified duration
func (d *DLQManager) CleanupResolved(olderThan time.Duration) int {
	if d == nil {
		return 0
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for id, item := range d.items {
		if (item.Status == DLQStatusResolved || item.Status == DLQStatusDiscarded) &&
			item.FirstFailed.Before(cutoff) {
			delete(d.items, id)
			removed++
		}
	}

	if removed > 0 {
		log.Printf("DLQ: Cleaned up %d resolved/discarded items older than %v", removed, olderThan)
		d.saveToDisk()
	}

	return removed
}
