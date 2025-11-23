package parallel

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MockESClient implements ESClient for testing
type MockESClient struct {
	indexedDocs []*ESDocument
}

func NewMockESClient() *MockESClient {
	return &MockESClient{
		indexedDocs: make([]*ESDocument, 0),
	}
}

func (m *MockESClient) BulkIndex(indexName string, documents []*ESDocument) error {
	m.indexedDocs = append(m.indexedDocs, documents...)
	return nil
}

func (m *MockESClient) GetIndexedDocuments() []*ESDocument {
	return m.indexedDocs
}

func TestParallelSnapshotManager_Basic(t *testing.T) {
	// Create test configuration
	config := &SnapshotConfig{
		MaxConcurrentTables: 2,
		MaxConcurrentChunks: 4,
		ChunkSize:           1000,
		WorkerPoolSize:      2,
		AdaptiveScheduling:  false,
		StreamingMode:       false,
		ChunkStrategy:       "id_based",
	}

	// Create mock ES client
	esClient := NewMockESClient()

	// Note: This test requires a running MySQL instance for full integration
	// For now, we'll test the basic structure
	manager := NewParallelSnapshotManager("test-job", config, nil, esClient)

	if manager == nil {
		t.Fatal("Expected manager to be created")
	}

	if manager.config.ChunkSize != 1000 {
		t.Errorf("Expected chunk size 1000, got %d", manager.config.ChunkSize)
	}

	if manager.jobID != "test-job" {
		t.Errorf("Expected job ID 'test-job', got '%s'", manager.jobID)
	}

	// Test statistics
	stats := manager.GetStatistics()
	if stats == nil {
		t.Fatal("Expected statistics to be available")
	}
}

func TestSnapshotConfig_DefaultValues(t *testing.T) {
	config := DefaultSnapshotConfig()

	if config.MaxConcurrentTables != 3 {
		t.Errorf("Expected max concurrent tables 3, got %d", config.MaxConcurrentTables)
	}

	if config.ChunkSize != 100000 {
		t.Errorf("Expected chunk size 100000, got %d", config.ChunkSize)
	}

	if config.WorkerPoolSize != 12 {
		t.Errorf("Expected worker pool size 12, got %d", config.WorkerPoolSize)
	}
}

func TestTableTask_Creation(t *testing.T) {
	task := &TableTask{
		JobID:     "test-job",
		TableName: "test_table",
		TotalRows: 50000,
		ChunkSize: 10000,
		Priority:  80,
		Status:    TaskStatusPending,
		CreatedAt: time.Now(),
	}

	if task.TableName != "test_table" {
		t.Errorf("Expected table name 'test_table', got '%s'", task.TableName)
	}

	if task.Status != TaskStatusPending {
		t.Errorf("Expected status pending, got %s", task.Status)
	}
}

func TestChunkTask_Creation(t *testing.T) {
	tableTask := &TableTask{
		JobID:     "test-job",
		TableName: "test_table",
		TotalRows: 50000,
		ChunkSize: 10000,
		Priority:  80,
		Status:    TaskStatusPending,
		CreatedAt: time.Now(),
	}

	chunk := &ChunkTask{
		ID:        "test_table_chunk_0",
		TableTask: tableTask,
		ChunkID:   0,
		StartID:   1,
		EndID:     10000,
		Status:    ChunkStatusPending,
	}

	if chunk.ChunkID != 0 {
		t.Errorf("Expected chunk ID 0, got %d", chunk.ChunkID)
	}

	if chunk.StartID != 1 {
		t.Errorf("Expected start ID 1, got %d", chunk.StartID)
	}

	if chunk.EndID != 10000 {
		t.Errorf("Expected end ID 10000, got %d", chunk.EndID)
	}
}

func TestProgressManager_Basic(t *testing.T) {
	pm := NewProgressManager()

	if pm == nil {
		t.Fatal("Expected progress manager to be created")
	}

	// Test initial state
	progress := pm.GetOverallProgress()
	if progress.TotalTables != 0 {
		t.Errorf("Expected 0 total tables, got %d", progress.TotalTables)
	}

	// Test table registration
	task := &TableTask{
		JobID:     "test-job",
		TableName: "test_table",
		TotalRows: 1000,
		Status:    TaskStatusPending,
		CreatedAt: time.Now(),
	}

	pm.RegisterTableTask(task)
	progress = pm.GetOverallProgress()
	if progress.TotalTables != 1 {
		t.Errorf("Expected 1 total table, got %d", progress.TotalTables)
	}

	if progress.TotalRows != 1000 {
		t.Errorf("Expected 1000 total rows, got %d", progress.TotalRows)
	}
}

func TestDataTransformer_Basic(t *testing.T) {
	transformer := NewDataTransformer()

	if transformer == nil {
		t.Fatal("Expected transformer to be created")
	}

	// Test transformation
	records := []*Record{
		{
			ID:        "1",
			TableName: "test_table",
			Data: map[string]interface{}{
				"id":   1,
				"name": "test",
			},
			Timestamp: time.Now(),
		},
	}

	docs, err := transformer.TransformBatch(records)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(docs) != 1 {
		t.Fatalf("Expected 1 document, got %d", len(docs))
	}

	if docs[0].ID != "1" {
		t.Errorf("Expected document ID '1', got '%s'", docs[0].ID)
	}
}

// Integration test (requires MySQL)
func TestParallelSnapshotManager_Integration(t *testing.T) {
	t.Skip("Skipping integration test - requires MySQL setup")

	// This test would require:
	// 1. A running MySQL instance
	// 2. Test database and tables
	// 3. Proper configuration

	/*
		dsn := "test_user:test_pass@tcp(localhost:3306)/test_db"
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			t.Skip("MySQL not available for integration test")
		}
		defer db.Close()

		config := DefaultSnapshotConfig()
		config.WorkerPoolSize = 2
		config.ChunkSize = 100

		esClient := NewMockESClient()
		manager := NewParallelSnapshotManager("integration-test", config, db, esClient)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tables := []string{"test_table"}
		err = manager.Start(ctx, tables)
		if err != nil {
			t.Fatalf("Failed to start manager: %v", err)
		}

		// Wait for completion or timeout
		// Verify results
	*/
}
