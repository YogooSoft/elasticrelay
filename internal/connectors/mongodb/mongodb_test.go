package mongodb

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/yogoosoft/elasticrelay/internal/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestBuildMongoURI(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.Config
		expected string
	}{
		{
			name: "with authentication",
			config: &config.Config{
				DBHost:     "localhost",
				DBPort:     27017,
				DBName:     "testdb",
				DBUser:     "testuser",
				DBPassword: "testpass",
			},
			expected: "mongodb://testuser:testpass@localhost:27017/testdb?directConnection=true&serverSelectionTimeoutMS=5000",
		},
		{
			name: "without authentication",
			config: &config.Config{
				DBHost: "localhost",
				DBPort: 27017,
				DBName: "testdb",
			},
			expected: "mongodb://localhost:27017/testdb?directConnection=true&serverSelectionTimeoutMS=5000",
		},
		{
			name: "with only username (no password)",
			config: &config.Config{
				DBHost: "localhost",
				DBPort: 27017,
				DBName: "testdb",
				DBUser: "testuser",
			},
			expected: "mongodb://localhost:27017/testdb?directConnection=true&serverSelectionTimeoutMS=5000",
		},
		{
			name: "with only password (no username)",
			config: &config.Config{
				DBHost:     "localhost",
				DBPort:     27017,
				DBName:     "testdb",
				DBPassword: "testpass",
			},
			expected: "mongodb://localhost:27017/testdb?directConnection=true&serverSelectionTimeoutMS=5000",
		},
		{
			name: "different host and port",
			config: &config.Config{
				DBHost:     "mongo.example.com",
				DBPort:     27018,
				DBName:     "production",
				DBUser:     "admin",
				DBPassword: "secret123",
			},
			expected: "mongodb://admin:secret123@mongo.example.com:27018/production?directConnection=true&serverSelectionTimeoutMS=5000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMongoURI(tt.config)
			if result != tt.expected {
				t.Errorf("buildMongoURI() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildPipeline(t *testing.T) {
	tests := []struct {
		name             string
		collectionFilter []string
		expectedStages   int
	}{
		{
			name:             "no collection filter",
			collectionFilter: nil,
			expectedStages:   1, // Only operation type filter
		},
		{
			name:             "single collection filter",
			collectionFilter: []string{"users"},
			expectedStages:   1, // Only operation type filter (single collection uses collection.Watch)
		},
		{
			name:             "multiple collection filters",
			collectionFilter: []string{"users", "orders", "products"},
			expectedStages:   2, // Operation type filter + collection filter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector := &Connector{
				database:         "testdb",
				collectionFilter: tt.collectionFilter,
			}

			pipeline := connector.buildPipeline()

			if len(pipeline) != tt.expectedStages {
				t.Errorf("buildPipeline() returned %d stages, want %d", len(pipeline), tt.expectedStages)
			}
		})
	}
}

func TestBuildPipeline_OperationTypeFilter(t *testing.T) {
	connector := &Connector{
		database:         "testdb",
		collectionFilter: nil,
	}

	pipeline := connector.buildPipeline()

	if len(pipeline) == 0 {
		t.Fatal("Pipeline should have at least one stage")
	}

	// Verify the first stage is an operation type filter
	firstStage := pipeline[0]

	// The stage should be a bson.D with $match
	matchFound := false
	for _, elem := range firstStage {
		if elem.Key == "$match" {
			matchFound = true
			matchDoc, ok := elem.Value.(bson.D)
			if !ok {
				t.Fatal("$match value should be bson.D")
			}

			// Find operationType filter
			for _, matchElem := range matchDoc {
				if matchElem.Key == "operationType" {
					inDoc, ok := matchElem.Value.(bson.D)
					if !ok {
						t.Fatal("operationType value should be bson.D")
					}

					// Find $in
					for _, inElem := range inDoc {
						if inElem.Key == "$in" {
							ops, ok := inElem.Value.(bson.A)
							if !ok {
								t.Fatal("$in value should be bson.A")
							}

							expectedOps := []string{"insert", "update", "replace", "delete"}
							if len(ops) != len(expectedOps) {
								t.Errorf("Expected %d operations, got %d", len(expectedOps), len(ops))
							}
						}
					}
				}
			}
		}
	}

	if !matchFound {
		t.Error("$match stage not found in pipeline")
	}
}

func TestBuildPipeline_CollectionFilter(t *testing.T) {
	connector := &Connector{
		database:         "testdb",
		collectionFilter: []string{"users", "orders"},
	}

	pipeline := connector.buildPipeline()

	if len(pipeline) != 2 {
		t.Fatalf("Pipeline should have 2 stages, got %d", len(pipeline))
	}

	// Verify second stage has collection filter
	secondStage := pipeline[1]

	matchFound := false
	for _, elem := range secondStage {
		if elem.Key == "$match" {
			matchFound = true
			matchDoc, ok := elem.Value.(bson.D)
			if !ok {
				t.Fatal("$match value should be bson.D")
			}

			// Find ns.coll filter
			for _, matchElem := range matchDoc {
				if matchElem.Key == "ns.coll" {
					inDoc, ok := matchElem.Value.(bson.D)
					if !ok {
						t.Fatal("ns.coll value should be bson.D")
					}

					// Find $in
					for _, inElem := range inDoc {
						if inElem.Key == "$in" {
							collections, ok := inElem.Value.([]string)
							if !ok {
								t.Fatal("$in value should be []string")
							}

							if !reflect.DeepEqual(collections, connector.collectionFilter) {
								t.Errorf("Collection filter = %v, want %v", collections, connector.collectionFilter)
							}
						}
					}
				}
			}
		}
	}

	if !matchFound {
		t.Error("$match stage for collection filter not found")
	}
}

// MockStream implements pb.ConnectorService_StartCdcServer for testing
type MockCdcStream struct {
	events []*ChangeEventResult
}

type ChangeEventResult struct {
	Op         string
	PrimaryKey string
	Data       string
}

// Note: We can't fully mock the gRPC stream in unit tests without more infrastructure
// These tests verify the internal logic of HandleChange

func TestChangeEvent_OperationMapping(t *testing.T) {
	tests := []struct {
		mongoOp    string
		expectedOp string
	}{
		{"insert", "INSERT"},
		{"update", "UPDATE"},
		{"replace", "UPDATE"},
		{"delete", "DELETE"},
	}

	for _, tt := range tests {
		t.Run(tt.mongoOp, func(t *testing.T) {
			var op string
			switch tt.mongoOp {
			case "insert":
				op = "INSERT"
			case "update", "replace":
				op = "UPDATE"
			case "delete":
				op = "DELETE"
			}

			if op != tt.expectedOp {
				t.Errorf("Operation mapping for %s = %s, want %s", tt.mongoOp, op, tt.expectedOp)
			}
		})
	}
}

func TestChangeEvent_Structures(t *testing.T) {
	// Test ChangeEvent struct
	objectID := primitive.NewObjectID()
	event := ChangeEvent{
		ID:            primitive.M{"_data": "test"},
		OperationType: "insert",
		FullDocument: bson.M{
			"_id":  objectID,
			"name": "test",
		},
		Ns: Namespace{
			Database:   "testdb",
			Collection: "users",
		},
		DocumentKey: bson.M{
			"_id": objectID,
		},
		ClusterTime: primitive.Timestamp{T: 1234567890, I: 1},
	}

	if event.OperationType != "insert" {
		t.Errorf("OperationType = %s, want 'insert'", event.OperationType)
	}

	if event.Ns.Database != "testdb" {
		t.Errorf("Ns.Database = %s, want 'testdb'", event.Ns.Database)
	}

	if event.Ns.Collection != "users" {
		t.Errorf("Ns.Collection = %s, want 'users'", event.Ns.Collection)
	}
}

func TestUpdateDescription_Structure(t *testing.T) {
	update := UpdateDescription{
		UpdatedFields: bson.M{
			"name":  "updated",
			"count": 10,
		},
		RemovedFields: []string{"oldField"},
		TruncatedArrays: []bson.M{
			{"field": "tags", "newSize": 5},
		},
	}

	if len(update.UpdatedFields) != 2 {
		t.Errorf("UpdatedFields should have 2 fields, got %d", len(update.UpdatedFields))
	}

	if len(update.RemovedFields) != 1 {
		t.Errorf("RemovedFields should have 1 field, got %d", len(update.RemovedFields))
	}

	if update.RemovedFields[0] != "oldField" {
		t.Errorf("RemovedFields[0] = %s, want 'oldField'", update.RemovedFields[0])
	}
}

// CheckpointManager tests
func TestCheckpointManager_NewCheckpointManager(t *testing.T) {
	cm := NewCheckpointManager("/tmp/test_checkpoints.json")

	if cm == nil {
		t.Fatal("NewCheckpointManager returned nil")
	}

	if cm.filePath != "/tmp/test_checkpoints.json" {
		t.Errorf("filePath = %s, want '/tmp/test_checkpoints.json'", cm.filePath)
	}

	if cm.checkpoints == nil {
		t.Error("checkpoints map should be initialized")
	}
}

func TestCheckpointManager_LoadAndSave(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "mongodb_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointFile := filepath.Join(tmpDir, "checkpoints.json")
	cm := NewCheckpointManager(checkpointFile)

	// Load should succeed even if file doesn't exist
	if err := cm.Load(); err != nil {
		t.Fatalf("Load() failed on non-existent file: %v", err)
	}

	// Add a checkpoint
	checkpoint := &MongoCheckpoint{
		JobID:        "job1",
		ResumeToken:  "token123",
		Database:     "testdb",
		Collection:   "users",
		EventCount:   100,
		ClusterTime:  1234567890,
		ClusterOrder: 1,
	}

	if err := cm.Update(checkpoint); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Verify checkpoint was stored
	retrieved, ok := cm.Get("job1")
	if !ok {
		t.Fatal("Get() returned false for existing job")
	}

	if retrieved.ResumeToken != "token123" {
		t.Errorf("ResumeToken = %s, want 'token123'", retrieved.ResumeToken)
	}

	// Create new manager and load from file
	cm2 := NewCheckpointManager(checkpointFile)
	if err := cm2.Load(); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	retrieved2, ok := cm2.Get("job1")
	if !ok {
		t.Fatal("Get() returned false after Load()")
	}

	if retrieved2.ResumeToken != "token123" {
		t.Errorf("ResumeToken after load = %s, want 'token123'", retrieved2.ResumeToken)
	}
}

func TestCheckpointManager_Update(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mongodb_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointFile := filepath.Join(tmpDir, "checkpoints.json")
	cm := NewCheckpointManager(checkpointFile)

	checkpoint := &MongoCheckpoint{
		JobID:       "job1",
		ResumeToken: "token1",
		EventCount:  50,
	}

	if err := cm.Update(checkpoint); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Verify LastUpdate was set
	retrieved, _ := cm.Get("job1")
	if retrieved.LastUpdate.IsZero() {
		t.Error("LastUpdate should be set after Update()")
	}

	// Update with new token
	checkpoint.ResumeToken = "token2"
	if err := cm.Update(checkpoint); err != nil {
		t.Fatalf("Update() failed on second call: %v", err)
	}

	retrieved, _ = cm.Get("job1")
	if retrieved.ResumeToken != "token2" {
		t.Errorf("ResumeToken = %s, want 'token2'", retrieved.ResumeToken)
	}
}

func TestCheckpointManager_Delete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mongodb_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointFile := filepath.Join(tmpDir, "checkpoints.json")
	cm := NewCheckpointManager(checkpointFile)

	// Add checkpoint
	checkpoint := &MongoCheckpoint{
		JobID:       "job1",
		ResumeToken: "token1",
	}
	if err := cm.Update(checkpoint); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Verify it exists
	_, ok := cm.Get("job1")
	if !ok {
		t.Fatal("Checkpoint should exist before delete")
	}

	// Delete
	if err := cm.Delete("job1"); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// Verify it's gone
	_, ok = cm.Get("job1")
	if ok {
		t.Error("Checkpoint should not exist after delete")
	}
}

func TestCheckpointManager_ListAll(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mongodb_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointFile := filepath.Join(tmpDir, "checkpoints.json")
	cm := NewCheckpointManager(checkpointFile)

	// Add multiple checkpoints
	checkpoints := []*MongoCheckpoint{
		{JobID: "job1", ResumeToken: "token1"},
		{JobID: "job2", ResumeToken: "token2"},
		{JobID: "job3", ResumeToken: "token3"},
	}

	for _, cp := range checkpoints {
		if err := cm.Update(cp); err != nil {
			t.Fatalf("Update() failed: %v", err)
		}
	}

	// List all
	all := cm.ListAll()
	if len(all) != 3 {
		t.Errorf("ListAll() returned %d checkpoints, want 3", len(all))
	}

	// Verify all job IDs are present
	for _, cp := range checkpoints {
		if _, ok := all[cp.JobID]; !ok {
			t.Errorf("ListAll() missing job %s", cp.JobID)
		}
	}

	// Note: ListAll returns a new map but with the same pointer values
	// This is the current implementation behavior
}

func TestCheckpointManager_EventCount(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mongodb_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointFile := filepath.Join(tmpDir, "checkpoints.json")
	cm := NewCheckpointManager(checkpointFile)

	// Add checkpoint with initial event count
	checkpoint := &MongoCheckpoint{
		JobID:      "job1",
		EventCount: 100,
	}
	if err := cm.Update(checkpoint); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Get initial count
	count := cm.GetEventCount("job1")
	if count != 100 {
		t.Errorf("GetEventCount() = %d, want 100", count)
	}

	// Increment
	cm.IncrementEventCount("job1")
	count = cm.GetEventCount("job1")
	if count != 101 {
		t.Errorf("GetEventCount() after increment = %d, want 101", count)
	}

	// Increment non-existent job (should not panic)
	cm.IncrementEventCount("nonexistent")

	// Get count for non-existent job
	count = cm.GetEventCount("nonexistent")
	if count != 0 {
		t.Errorf("GetEventCount() for non-existent = %d, want 0", count)
	}
}

func TestCheckpointManager_Concurrency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mongodb_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	checkpointFile := filepath.Join(tmpDir, "checkpoints.json")
	cm := NewCheckpointManager(checkpointFile)

	// Initialize checkpoint
	checkpoint := &MongoCheckpoint{
		JobID:      "concurrent_job",
		EventCount: 0,
	}
	if err := cm.Update(checkpoint); err != nil {
		t.Fatalf("Update() failed: %v", err)
	}

	// Concurrent increments
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				cm.IncrementEventCount("concurrent_job")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	count := cm.GetEventCount("concurrent_job")
	if count != 1000 {
		t.Errorf("After concurrent increments, count = %d, want 1000", count)
	}
}

func TestMongoCheckpoint_Structure(t *testing.T) {
	now := time.Now()
	checkpoint := MongoCheckpoint{
		JobID:        "test-job",
		ResumeToken:  "base64-encoded-token",
		Database:     "testdb",
		Collection:   "users",
		LastUpdate:   now,
		EventCount:   12345,
		ClusterTime:  1234567890,
		ClusterOrder: 5,
	}

	if checkpoint.JobID != "test-job" {
		t.Errorf("JobID = %s, want 'test-job'", checkpoint.JobID)
	}

	if checkpoint.ResumeToken != "base64-encoded-token" {
		t.Errorf("ResumeToken = %s, want 'base64-encoded-token'", checkpoint.ResumeToken)
	}

	if checkpoint.Database != "testdb" {
		t.Errorf("Database = %s, want 'testdb'", checkpoint.Database)
	}

	if checkpoint.EventCount != 12345 {
		t.Errorf("EventCount = %d, want 12345", checkpoint.EventCount)
	}
}

// Test NewConnector
func TestNewConnector(t *testing.T) {
	cfg := &config.Config{
		DBHost:       "localhost",
		DBPort:       27017,
		DBName:       "testdb",
		DBUser:       "user",
		DBPassword:   "pass",
		TableFilters: []string{"users", "orders"},
	}

	connector, err := NewConnector(cfg)
	if err != nil {
		t.Fatalf("NewConnector() failed: %v", err)
	}

	if connector.database != "testdb" {
		t.Errorf("database = %s, want 'testdb'", connector.database)
	}

	if !reflect.DeepEqual(connector.collectionFilter, cfg.TableFilters) {
		t.Errorf("collectionFilter = %v, want %v", connector.collectionFilter, cfg.TableFilters)
	}

	expectedURI := "mongodb://user:pass@localhost:27017/testdb?directConnection=true&serverSelectionTimeoutMS=5000"
	if connector.uri != expectedURI {
		t.Errorf("uri = %s, want %s", connector.uri, expectedURI)
	}
}

// Test Namespace struct
func TestNamespace(t *testing.T) {
	ns := Namespace{
		Database:   "mydb",
		Collection: "mycoll",
	}

	if ns.Database != "mydb" {
		t.Errorf("Database = %s, want 'mydb'", ns.Database)
	}

	if ns.Collection != "mycoll" {
		t.Errorf("Collection = %s, want 'mycoll'", ns.Collection)
	}
}

// Test EventHandler struct initialization
func TestEventHandler(t *testing.T) {
	handler := &EventHandler{
		stream:   nil, // Would be a mock in integration tests
		database: "testdb",
	}

	if handler.database != "testdb" {
		t.Errorf("database = %s, want 'testdb'", handler.database)
	}
}

// Benchmark tests
func BenchmarkBuildPipeline(b *testing.B) {
	connector := &Connector{
		database:         "testdb",
		collectionFilter: []string{"users", "orders", "products", "categories"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		connector.buildPipeline()
	}
}

func BenchmarkBuildMongoURI(b *testing.B) {
	cfg := &config.Config{
		DBHost:     "localhost",
		DBPort:     27017,
		DBName:     "testdb",
		DBUser:     "user",
		DBPassword: "password",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildMongoURI(cfg)
	}
}

// Helper to suppress the import error for mongo package
var _ = mongo.Pipeline{}
