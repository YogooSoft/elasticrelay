package postgresql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/yogoosoft/elasticrelay/internal/config"
	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	_ "github.com/lib/pq"
)

// IntegrationTestSuite provides comprehensive integration tests for PostgreSQL connector
type IntegrationTestSuite struct {
	db       *sql.DB
	config   *config.Config
	server   *Server
	ctx      context.Context
}

// TestPostgreSQLIntegration runs comprehensive integration tests
// Note: This test requires a running PostgreSQL instance with proper configuration
func TestPostgreSQLIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Skip if PostgreSQL environment not configured
	cfg := getTestConfig()
	if cfg == nil {
		t.Skip("PostgreSQL test environment not configured")
	}

	suite := &IntegrationTestSuite{
		config: cfg,
		ctx:    context.Background(),
	}

	// Setup
	if err := suite.setup(t); err != nil {
		t.Fatalf("Failed to setup test suite: %v", err)
	}
	defer suite.teardown(t)

	// Run test cases
	t.Run("BasicConnection", suite.testBasicConnection)
	t.Run("TypeMapping", suite.testTypeMapping)
	t.Run("ReplicationSlotManagement", suite.testReplicationSlotManagement)
	t.Run("LSNManagement", suite.testLSNManagement)
	t.Run("SnapshotProcessing", suite.testSnapshotProcessing)
	t.Run("Performance", suite.testPerformance)
}

func (suite *IntegrationTestSuite) setup(t *testing.T) error {
	// Connect to PostgreSQL
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		suite.config.DBHost, suite.config.DBPort, suite.config.DBUser, 
		suite.config.DBPassword, suite.config.DBName)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(suite.ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	suite.db = db

	// Create test server
	server, err := NewServer(suite.config)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	suite.server = server

	// Setup test schema
	return suite.setupTestSchema()
}

func (suite *IntegrationTestSuite) teardown(t *testing.T) {
	if suite.db != nil {
		// Cleanup test schema
		suite.cleanupTestSchema()
		suite.db.Close()
	}
}

func (suite *IntegrationTestSuite) setupTestSchema() error {
	// Create test table with various data types
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS test_table (
		id SERIAL PRIMARY KEY,
		text_col TEXT,
		varchar_col VARCHAR(255),
		int_col INTEGER,
		bigint_col BIGINT,
		boolean_col BOOLEAN,
		timestamp_col TIMESTAMP,
		timestamptz_col TIMESTAMPTZ,
		json_col JSON,
		jsonb_col JSONB,
		uuid_col UUID,
		array_text_col TEXT[],
		array_int_col INTEGER[],
		numeric_col NUMERIC(10,2),
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`

	_, err := suite.db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create test table: %w", err)
	}

	// Insert test data
	insertSQL := `
	INSERT INTO test_table (
		text_col, varchar_col, int_col, bigint_col, boolean_col,
		timestamp_col, timestamptz_col, json_col, jsonb_col, uuid_col,
		array_text_col, array_int_col, numeric_col
	) VALUES (
		'test text', 'test varchar', 42, 123456789, true,
		'2023-01-01 12:00:00', '2023-01-01 12:00:00+00', 
		'{"key": "value"}', '{"key": "value"}', gen_random_uuid(),
		ARRAY['one', 'two', 'three'], ARRAY[1, 2, 3], 123.45
	)`

	_, err = suite.db.Exec(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to insert test data: %w", err)
	}

	return nil
}

func (suite *IntegrationTestSuite) cleanupTestSchema() {
	suite.db.Exec("DROP TABLE IF EXISTS test_table")
}

func (suite *IntegrationTestSuite) testBasicConnection(t *testing.T) {
	connector, err := NewConnector(suite.config)
	if err != nil {
		t.Fatalf("Failed to create connector: %v", err)
	}

	if connector == nil {
		t.Fatal("Connector is nil")
	}

	// Test connection configuration
	if connector.dbName != suite.config.DBName {
		t.Errorf("Expected database name '%s', got '%s'", 
			suite.config.DBName, connector.dbName)
	}

	if connector.slotName == "" {
		t.Error("Replication slot name should not be empty")
	}

	if connector.publication == "" {
		t.Error("Publication name should not be empty")
	}
}

func (suite *IntegrationTestSuite) testTypeMapping(t *testing.T) {
	typeMapper := NewTypeMapper()

	// Test basic types
	testCases := []struct {
		name     string
		oid      uint32
		value    interface{}
		expected interface{}
	}{
		{"text", 25, "hello world", "hello world"},
		{"integer", 23, int32(42), 42},
		{"bigint", 20, int64(123456789), int64(123456789)},
		{"boolean", 16, true, true},
		{"json", 114, `{"key": "value"}`, map[string]interface{}{"key": "value"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := typeMapper.ConvertValue(tc.value, tc.oid)
			if err != nil {
				t.Errorf("Failed to convert %s: %v", tc.name, err)
				return
			}

			// For JSON, compare serialized forms
			if tc.name == "json" {
				expectedJSON, _ := json.Marshal(tc.expected)
				resultJSON, _ := json.Marshal(result)
				if string(expectedJSON) != string(resultJSON) {
					t.Errorf("Expected %s, got %s", string(expectedJSON), string(resultJSON))
				}
			} else {
				if result != tc.expected {
					t.Errorf("Expected %v, got %v", tc.expected, result)
				}
			}
		})
	}

	// Test advanced type mapper
	advancedMapper := NewAdvancedTypeMapper(nil)
	
	// Test array parsing
	arrayResult, err := advancedMapper.parsePostgreSQLArray("{1,2,3}", advancedMapper.handleInteger)
	if err != nil {
		t.Errorf("Failed to parse array: %v", err)
	}

	resultSlice, ok := arrayResult.([]interface{})
	if !ok || len(resultSlice) != 3 {
		t.Errorf("Expected array of 3 integers, got %v", arrayResult)
	}
}

func (suite *IntegrationTestSuite) testReplicationSlotManagement(t *testing.T) {
	// Create connection pool
	poolManager := NewConnectionPoolManager()
	defer poolManager.CloseAll()

	pool, err := poolManager.CreatePool("test", suite.config, DefaultConnectionPoolConfig())
	if err != nil {
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	replicationMgr := NewReplicationSlotManager(pool.Pool)

	// Test slot creation
	slotName := fmt.Sprintf("test_slot_%d", time.Now().Unix())
	slot, err := replicationMgr.CreateReplicationSlot(suite.ctx, slotName, "pgoutput")
	if err != nil {
		t.Fatalf("Failed to create replication slot: %v", err)
	}

	if slot.SlotName != slotName {
		t.Errorf("Expected slot name '%s', got '%s'", slotName, slot.SlotName)
	}

	// Test slot retrieval
	retrievedSlot, err := replicationMgr.GetReplicationSlot(suite.ctx, slotName)
	if err != nil {
		t.Errorf("Failed to get replication slot: %v", err)
	}

	if retrievedSlot.SlotName != slotName {
		t.Errorf("Retrieved slot name mismatch: expected '%s', got '%s'", 
			slotName, retrievedSlot.SlotName)
	}

	// Test slot monitoring
	stats, err := replicationMgr.MonitorReplicationSlot(suite.ctx, slotName)
	if err != nil {
		t.Errorf("Failed to monitor replication slot: %v", err)
	}

	if stats.SlotName != slotName {
		t.Errorf("Stats slot name mismatch: expected '%s', got '%s'", 
			slotName, stats.SlotName)
	}

	// Cleanup
	err = replicationMgr.DeleteReplicationSlot(suite.ctx, slotName)
	if err != nil {
		t.Errorf("Failed to delete replication slot: %v", err)
	}
}

func (suite *IntegrationTestSuite) testLSNManagement(t *testing.T) {
	lsnManager := NewLSNManager(nil, "test_checkpoints.json")

	// Test LSN comparison
	tests := []struct {
		lsn1     string
		lsn2     string
		expected int
	}{
		{"0/1000", "0/2000", -1},
		{"0/2000", "0/1000", 1},
		{"0/1000", "0/1000", 0},
		{"1/0", "0/FFFFFFFF", 1},
	}

	for _, tt := range tests {
		result, err := lsnManager.CompareLSN(tt.lsn1, tt.lsn2)
		if err != nil {
			t.Errorf("Failed to compare LSNs %s and %s: %v", tt.lsn1, tt.lsn2, err)
			continue
		}

		if result != tt.expected {
			t.Errorf("LSN comparison %s vs %s: expected %d, got %d", 
				tt.lsn1, tt.lsn2, tt.expected, result)
		}
	}

	// Test LSN advancement
	advancedLSN, err := lsnManager.AdvanceLSN("0/1000", 1000)
	if err != nil {
		t.Errorf("Failed to advance LSN: %v", err)
	}

	if advancedLSN == "" {
		t.Error("Advanced LSN should not be empty")
	}

	// Test checkpoint save/load
	checkpoint := &LSNCheckpoint{
		JobID:       "test_job",
		LSN:         "0/1000",
		Timeline:    1,
		SlotName:    "test_slot",
		Publication: "test_pub",
		LastUpdate:  time.Now(),
	}

	err = lsnManager.SaveCheckpoint(checkpoint)
	if err != nil {
		t.Errorf("Failed to save checkpoint: %v", err)
	}

	loadedCheckpoint, err := lsnManager.LoadCheckpoint("test_job")
	if err != nil {
		t.Errorf("Failed to load checkpoint: %v", err)
	}

	if loadedCheckpoint.JobID != checkpoint.JobID {
		t.Errorf("Checkpoint JobID mismatch: expected '%s', got '%s'", 
			checkpoint.JobID, loadedCheckpoint.JobID)
	}

	// Cleanup
	lsnManager.DeleteCheckpoint("test_job")
}

func (suite *IntegrationTestSuite) testSnapshotProcessing(t *testing.T) {
	// Mock stream for testing
	mockStream := &MockSnapshotStream{
		chunks: make([]*pb.SnapshotChunk, 0),
	}

	// Create snapshot request
	req := &pb.BeginSnapshotRequest{
		JobId:     "test_snapshot_job",
		TableName: "test_table",
	}

	// Test snapshot processing
	err := suite.server.BeginSnapshot(req, mockStream)
	if err != nil {
		t.Errorf("Failed to process snapshot: %v", err)
		return
	}

	// Verify chunks were sent
	if len(mockStream.chunks) == 0 {
		t.Error("No snapshot chunks were sent")
		return
	}

	// Verify chunk content
	firstChunk := mockStream.chunks[0]
	if len(firstChunk.Records) == 0 {
		t.Error("First chunk has no records")
		return
	}

	// Parse first record to verify structure
	var recordData map[string]interface{}
	err = json.Unmarshal([]byte(firstChunk.Records[0]), &recordData)
	if err != nil {
		t.Errorf("Failed to parse record JSON: %v", err)
		return
	}

	// Verify metadata fields
	if recordData["_table"] != "test_table" {
		t.Errorf("Expected _table 'test_table', got %v", recordData["_table"])
	}

	if recordData["_schema"] != suite.config.DBName {
		t.Errorf("Expected _schema '%s', got %v", suite.config.DBName, recordData["_schema"])
	}

	// Verify data fields exist
	expectedFields := []string{"id", "text_col", "varchar_col", "int_col", "boolean_col"}
	for _, field := range expectedFields {
		if _, exists := recordData[field]; !exists {
			t.Errorf("Expected field '%s' not found in record", field)
		}
	}
}

func (suite *IntegrationTestSuite) testPerformance(t *testing.T) {
	perfMonitor := NewPerformanceMonitor()

	// Test basic metrics recording
	perfMonitor.RecordThroughput(1000, 50000, 10*time.Millisecond)
	perfMonitor.UpdateResourceMetrics()

	connMetrics, throughputMetrics, resourceMetrics := perfMonitor.GetMetrics()

	// Verify throughput metrics
	if throughputMetrics.RecordsProcessed != 1000 {
		t.Errorf("Expected 1000 records processed, got %d", throughputMetrics.RecordsProcessed)
	}

	if throughputMetrics.BytesProcessed != 50000 {
		t.Errorf("Expected 50000 bytes processed, got %d", throughputMetrics.BytesProcessed)
	}

	// Verify resource metrics are populated
	if resourceMetrics.MemoryUsageMB <= 0 {
		t.Error("Memory usage should be greater than 0")
	}

	if resourceMetrics.GoroutineCount <= 0 {
		t.Error("Goroutine count should be greater than 0")
	}

	// Test batch processor
	processed := make([]interface{}, 0)
	batchCallback := func(items []interface{}) error {
		processed = append(processed, items...)
		return nil
	}

	batchProcessor := NewBatchProcessor(5, 100*time.Millisecond, batchCallback)
	batchProcessor.Start()

	// Add items to batch
	for i := 0; i < 12; i++ {
		err := batchProcessor.Add(fmt.Sprintf("item_%d", i))
		if err != nil {
			t.Errorf("Failed to add item to batch: %v", err)
		}
	}

	// Stop and verify processing
	err := batchProcessor.Stop()
	if err != nil {
		t.Errorf("Failed to stop batch processor: %v", err)
	}

	if len(processed) != 12 {
		t.Errorf("Expected 12 processed items, got %d", len(processed))
	}

	// Test memory optimizer
	memOptimizer := NewMemoryOptimizer(1, 100*time.Millisecond) // 1MB threshold
	memOptimizer.Start()
	
	// Let it run briefly
	time.Sleep(200 * time.Millisecond)
	
	memOptimizer.Stop()

	// If we reach here without panics, the memory optimizer is working
	t.Log("Memory optimizer test passed")

	// Verify no unused variables
	_ = connMetrics
}

// MockSnapshotStream implements the gRPC stream interface for testing
type MockSnapshotStream struct {
	chunks []*pb.SnapshotChunk
	ctx    context.Context
}

func (m *MockSnapshotStream) Send(chunk *pb.SnapshotChunk) error {
	m.chunks = append(m.chunks, chunk)
	return nil
}

func (m *MockSnapshotStream) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *MockSnapshotStream) SendMsg(msg interface{}) error {
	return nil
}

func (m *MockSnapshotStream) RecvMsg(msg interface{}) error {
	return nil
}

func (m *MockSnapshotStream) SetHeader(metadata interface{}) error {
	return nil
}

func (m *MockSnapshotStream) SendHeader(metadata interface{}) error {
	return nil
}

func (m *MockSnapshotStream) SetTrailer(metadata interface{}) {
}

// getTestConfig returns test configuration or nil if not available
func getTestConfig() *config.Config {
	// This would typically read from environment variables or test config
	// For now, return nil to skip tests unless explicitly configured
	return nil
	
	// Example configuration (uncomment and modify for actual testing):
	/*
	return &config.Config{
		DBHost:     "localhost",
		DBPort:     5432,
		DBUser:     "postgres",
		DBPassword: "postgres",
		DBName:     "test_db",
	}
	*/
}

// BenchmarkTypeMapping benchmarks type conversion performance
func BenchmarkTypeMapping(b *testing.B) {
	tm := NewTypeMapper()
	
	b.Run("TextConversion", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := tm.ConvertValue("test string", 25) // text type
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("IntegerConversion", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := tm.ConvertValue(int32(42), 23) // integer type
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	
	b.Run("JSONConversion", func(b *testing.B) {
		jsonStr := `{"key": "value", "number": 42, "array": [1,2,3]}`
		for i := 0; i < b.N; i++ {
			_, err := tm.ConvertValue(jsonStr, 114) // json type
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkLSNComparison benchmarks LSN comparison performance
func BenchmarkLSNComparison(b *testing.B) {
	lsnMgr := NewLSNManager(nil, "")
	
	for i := 0; i < b.N; i++ {
		_, err := lsnMgr.CompareLSN("0/1000000", "0/2000000")
		if err != nil {
			b.Fatal(err)
		}
	}
}
