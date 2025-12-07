//go:build integration

package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Integration tests for MongoDB connector
// These tests require a running MongoDB replica set
//
// To run these tests:
// 1. Start MongoDB replica set using Docker:
//    docker-compose -f docker-compose.mongodb-test.yml up -d
//
// 2. Run tests with the integration tag:
//    go test -tags=integration -v ./internal/connectors/mongodb/...
//
// 3. Cleanup:
//    docker-compose -f docker-compose.mongodb-test.yml down -v

const (
	// Default MongoDB connection URI for testing
	// Can be overridden with MONGODB_TEST_URI environment variable
	defaultTestURI = "mongodb://localhost:27017/?replicaSet=rs0"
	testDatabase   = "elasticrelay_test"
)

var (
	testClient *mongo.Client
	testURI    string
)

func TestMain(m *testing.M) {
	// Setup
	testURI = os.Getenv("MONGODB_TEST_URI")
	if testURI == "" {
		testURI = defaultTestURI
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var err error
	clientOptions := options.Client().
		ApplyURI(testURI).
		SetConnectTimeout(10 * time.Second)

	testClient, err = mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Printf("Failed to connect to MongoDB: %v", err)
		log.Printf("Make sure MongoDB replica set is running at %s", testURI)
		os.Exit(1)
	}

	// Verify connection
	if err := testClient.Ping(ctx, nil); err != nil {
		log.Printf("Failed to ping MongoDB: %v", err)
		log.Printf("Make sure MongoDB replica set is running at %s", testURI)
		os.Exit(1)
	}

	log.Printf("Connected to MongoDB at %s", testURI)

	// Run tests
	code := m.Run()

	// Cleanup
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cleanupCancel()

	if err := testClient.Database(testDatabase).Drop(cleanupCtx); err != nil {
		log.Printf("Warning: failed to drop test database: %v", err)
	}

	if err := testClient.Disconnect(cleanupCtx); err != nil {
		log.Printf("Warning: failed to disconnect: %v", err)
	}

	os.Exit(code)
}

// TestChangeStreamBasic tests basic Change Stream functionality
func TestChangeStreamBasic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coll := testClient.Database(testDatabase).Collection("test_changes")

	// Clean up before test
	if err := coll.Drop(ctx); err != nil {
		t.Logf("Warning: failed to drop collection: %v", err)
	}

	// Create change stream options
	opts := options.ChangeStream().
		SetFullDocument(options.UpdateLookup).
		SetBatchSize(10)

	// Start watching before making changes
	changeStream, err := coll.Watch(ctx, mongo.Pipeline{}, opts)
	if err != nil {
		t.Fatalf("Failed to create change stream: %v", err)
	}
	defer changeStream.Close(ctx)

	// Insert a document
	insertResult, err := coll.InsertOne(ctx, bson.M{
		"name":  "test",
		"value": 123,
	})
	if err != nil {
		t.Fatalf("Failed to insert document: %v", err)
	}

	// Wait for and verify change event
	if !changeStream.Next(ctx) {
		t.Fatal("Expected a change event, got none")
	}

	var changeEvent bson.M
	if err := changeStream.Decode(&changeEvent); err != nil {
		t.Fatalf("Failed to decode change event: %v", err)
	}

	// Verify event type
	if changeEvent["operationType"] != "insert" {
		t.Errorf("Expected operationType 'insert', got %v", changeEvent["operationType"])
	}

	// Verify document key
	if docKey, ok := changeEvent["documentKey"].(bson.M); ok {
		if docKey["_id"] != insertResult.InsertedID {
			t.Errorf("Document key _id mismatch")
		}
	} else {
		t.Error("Missing documentKey in change event")
	}

	// Verify full document
	if fullDoc, ok := changeEvent["fullDocument"].(bson.M); ok {
		if fullDoc["name"] != "test" {
			t.Errorf("Expected name 'test', got %v", fullDoc["name"])
		}
	} else {
		t.Error("Missing fullDocument in change event")
	}
}

// TestChangeStreamResumeToken tests resume token functionality
func TestChangeStreamResumeToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	coll := testClient.Database(testDatabase).Collection("test_resume_token")

	// Clean up before test
	if err := coll.Drop(ctx); err != nil {
		t.Logf("Warning: failed to drop collection: %v", err)
	}

	// Start initial change stream
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	changeStream, err := coll.Watch(ctx, mongo.Pipeline{}, opts)
	if err != nil {
		t.Fatalf("Failed to create change stream: %v", err)
	}

	// Insert first document
	_, err = coll.InsertOne(ctx, bson.M{"event": "first"})
	if err != nil {
		t.Fatalf("Failed to insert first document: %v", err)
	}

	// Wait for first event
	if !changeStream.Next(ctx) {
		t.Fatal("Expected first change event")
	}

	// Save resume token
	resumeToken := changeStream.ResumeToken()
	if resumeToken == nil {
		t.Fatal("Resume token is nil")
	}

	// Encode resume token
	encodedToken := encodeResumeToken(resumeToken)
	if encodedToken == "" {
		t.Fatal("Failed to encode resume token")
	}

	// Close first change stream
	changeStream.Close(ctx)

	// Insert second document (while stream is closed)
	_, err = coll.InsertOne(ctx, bson.M{"event": "second"})
	if err != nil {
		t.Fatalf("Failed to insert second document: %v", err)
	}

	// Decode resume token
	decodedToken, err := decodeResumeToken(encodedToken)
	if err != nil {
		t.Fatalf("Failed to decode resume token: %v", err)
	}

	// Resume from token
	resumeOpts := options.ChangeStream().
		SetFullDocument(options.UpdateLookup).
		SetResumeAfter(decodedToken)

	resumedStream, err := coll.Watch(ctx, mongo.Pipeline{}, resumeOpts)
	if err != nil {
		t.Fatalf("Failed to resume change stream: %v", err)
	}
	defer resumedStream.Close(ctx)

	// Should receive second event
	if !resumedStream.Next(ctx) {
		t.Fatal("Expected second change event after resume")
	}

	var changeEvent bson.M
	if err := resumedStream.Decode(&changeEvent); err != nil {
		t.Fatalf("Failed to decode change event: %v", err)
	}

	// Verify we got the second event
	if fullDoc, ok := changeEvent["fullDocument"].(bson.M); ok {
		if fullDoc["event"] != "second" {
			t.Errorf("Expected event 'second', got %v", fullDoc["event"])
		}
	} else {
		t.Error("Missing fullDocument in resumed change event")
	}
}

// TestChangeStreamUpdateDelete tests update and delete operations
func TestChangeStreamUpdateDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coll := testClient.Database(testDatabase).Collection("test_update_delete")

	// Clean up and insert initial document
	if err := coll.Drop(ctx); err != nil {
		t.Logf("Warning: failed to drop collection: %v", err)
	}

	insertResult, err := coll.InsertOne(ctx, bson.M{
		"name":  "original",
		"count": 0,
	})
	if err != nil {
		t.Fatalf("Failed to insert document: %v", err)
	}

	docID := insertResult.InsertedID

	// Start change stream after insert
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	changeStream, err := coll.Watch(ctx, mongo.Pipeline{}, opts)
	if err != nil {
		t.Fatalf("Failed to create change stream: %v", err)
	}
	defer changeStream.Close(ctx)

	// Update document
	_, err = coll.UpdateOne(ctx, bson.M{"_id": docID}, bson.M{
		"$set": bson.M{"name": "updated"},
		"$inc": bson.M{"count": 1},
	})
	if err != nil {
		t.Fatalf("Failed to update document: %v", err)
	}

	// Wait for update event
	if !changeStream.Next(ctx) {
		t.Fatal("Expected update event")
	}

	var updateEvent bson.M
	if err := changeStream.Decode(&updateEvent); err != nil {
		t.Fatalf("Failed to decode update event: %v", err)
	}

	if updateEvent["operationType"] != "update" {
		t.Errorf("Expected operationType 'update', got %v", updateEvent["operationType"])
	}

	// Verify update description
	if updateDesc, ok := updateEvent["updateDescription"].(bson.M); ok {
		if updatedFields, ok := updateDesc["updatedFields"].(bson.M); ok {
			if updatedFields["name"] != "updated" {
				t.Errorf("Expected updated name 'updated', got %v", updatedFields["name"])
			}
		}
	}

	// Delete document
	_, err = coll.DeleteOne(ctx, bson.M{"_id": docID})
	if err != nil {
		t.Fatalf("Failed to delete document: %v", err)
	}

	// Wait for delete event
	if !changeStream.Next(ctx) {
		t.Fatal("Expected delete event")
	}

	var deleteEvent bson.M
	if err := changeStream.Decode(&deleteEvent); err != nil {
		t.Fatalf("Failed to decode delete event: %v", err)
	}

	if deleteEvent["operationType"] != "delete" {
		t.Errorf("Expected operationType 'delete', got %v", deleteEvent["operationType"])
	}
}

// TestTypeConversionEndToEnd tests BSON to JSON type conversion with real MongoDB data
func TestTypeConversionEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	coll := testClient.Database(testDatabase).Collection("test_types")

	// Clean up before test
	if err := coll.Drop(ctx); err != nil {
		t.Logf("Warning: failed to drop collection: %v", err)
	}

	// Create document with various types
	objectID := primitive.NewObjectID()
	now := time.Now().UTC()
	decimal, _ := primitive.ParseDecimal128("123.456")

	doc := bson.M{
		"_id":       objectID,
		"string":    "test string",
		"int32":     int32(42),
		"int64":     int64(9999999999),
		"float":     3.14159,
		"bool":      true,
		"null":      nil,
		"date":      primitive.NewDateTimeFromTime(now),
		"decimal":   decimal,
		"binary":    primitive.Binary{Subtype: 0x00, Data: []byte{1, 2, 3, 4}},
		"regex":     primitive.Regex{Pattern: "^test.*$", Options: "i"},
		"timestamp": primitive.Timestamp{T: 1234567890, I: 1},
		"nested": bson.M{
			"level1": bson.M{
				"value": "deep",
			},
		},
		"array": bson.A{"one", 2, true},
	}

	_, err := coll.InsertOne(ctx, doc)
	if err != nil {
		t.Fatalf("Failed to insert document: %v", err)
	}

	// Read document back
	var readDoc bson.M
	err = coll.FindOne(ctx, bson.M{"_id": objectID}).Decode(&readDoc)
	if err != nil {
		t.Fatalf("Failed to read document: %v", err)
	}

	// Convert using our converter
	converted := convertBSONToMap(readDoc)

	// Verify conversions
	// ObjectID
	if idStr, ok := converted["_id"].(string); !ok || idStr != objectID.Hex() {
		t.Errorf("ObjectID conversion failed: got %v", converted["_id"])
	}

	// String
	if converted["string"] != "test string" {
		t.Errorf("String conversion failed: got %v", converted["string"])
	}

	// Int32 -> Int64
	if converted["int32"] != int64(42) {
		t.Errorf("Int32 conversion failed: got %v (type %T)", converted["int32"], converted["int32"])
	}

	// Int64
	if converted["int64"] != int64(9999999999) {
		t.Errorf("Int64 conversion failed: got %v", converted["int64"])
	}

	// Float
	if converted["float"] != 3.14159 {
		t.Errorf("Float conversion failed: got %v", converted["float"])
	}

	// Bool
	if converted["bool"] != true {
		t.Errorf("Bool conversion failed: got %v", converted["bool"])
	}

	// Null
	if converted["null"] != nil {
		t.Errorf("Null conversion failed: got %v", converted["null"])
	}

	// Decimal128 -> string
	if decStr, ok := converted["decimal"].(string); !ok || decStr != "123.456" {
		t.Errorf("Decimal128 conversion failed: got %v", converted["decimal"])
	}

	// Binary -> base64 map
	if binMap, ok := converted["binary"].(map[string]interface{}); ok {
		if _, hasKey := binMap["$binary"]; !hasKey {
			t.Error("Binary should have $binary key")
		}
	} else {
		t.Errorf("Binary conversion failed: got %v", converted["binary"])
	}

	// Regex -> map
	if regexMap, ok := converted["regex"].(map[string]interface{}); ok {
		if regexMap["$regex"] != "^test.*$" {
			t.Errorf("Regex pattern mismatch: got %v", regexMap["$regex"])
		}
	} else {
		t.Errorf("Regex conversion failed: got %v", converted["regex"])
	}

	// Nested document
	if nested, ok := converted["nested"].(map[string]interface{}); ok {
		if level1, ok := nested["level1"].(map[string]interface{}); ok {
			if level1["value"] != "deep" {
				t.Errorf("Nested value mismatch: got %v", level1["value"])
			}
		} else {
			t.Error("Nested level1 conversion failed")
		}
	} else {
		t.Errorf("Nested conversion failed: got %v", converted["nested"])
	}

	// Array
	if arr, ok := converted["array"].([]interface{}); ok {
		if len(arr) != 3 {
			t.Errorf("Array length mismatch: got %d", len(arr))
		}
	} else {
		t.Errorf("Array conversion failed: got %v", converted["array"])
	}

	// Verify JSON serialization works
	jsonBytes, err := json.Marshal(converted)
	if err != nil {
		t.Fatalf("Failed to marshal converted document to JSON: %v", err)
	}

	// Verify JSON can be parsed back
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	t.Logf("Successfully converted and serialized document: %s", string(jsonBytes)[:min(200, len(jsonBytes))])
}

// TestDatabaseLevelChangeStream tests watching changes at the database level
func TestDatabaseLevelChangeStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db := testClient.Database(testDatabase)

	// Create change stream at database level
	opts := options.ChangeStream().SetFullDocument(options.UpdateLookup)
	changeStream, err := db.Watch(ctx, mongo.Pipeline{}, opts)
	if err != nil {
		t.Fatalf("Failed to create database change stream: %v", err)
	}
	defer changeStream.Close(ctx)

	// Insert into different collections
	collections := []string{"coll_a", "coll_b", "coll_c"}
	for _, collName := range collections {
		_, err := db.Collection(collName).InsertOne(ctx, bson.M{
			"collection": collName,
			"timestamp":  time.Now(),
		})
		if err != nil {
			t.Fatalf("Failed to insert into %s: %v", collName, err)
		}
	}

	// Receive events from all collections
	receivedCollections := make(map[string]bool)
	for i := 0; i < len(collections); i++ {
		if !changeStream.Next(ctx) {
			t.Fatalf("Expected change event %d/%d", i+1, len(collections))
		}

		var event bson.M
		if err := changeStream.Decode(&event); err != nil {
			t.Fatalf("Failed to decode event: %v", err)
		}

		if ns, ok := event["ns"].(bson.M); ok {
			if coll, ok := ns["coll"].(string); ok {
				receivedCollections[coll] = true
			}
		}
	}

	// Verify all collections received
	for _, collName := range collections {
		if !receivedCollections[collName] {
			t.Errorf("Did not receive event for collection %s", collName)
		}
	}
}

// TestConnectorIntegration tests the full Connector integration
func TestConnectorIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Parse URI to get host and port
	connector := &Connector{
		uri:              testURI,
		database:         testDatabase,
		collectionFilter: []string{"integration_test"},
	}

	// Connect
	if err := connector.Connect(ctx); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer connector.Close(ctx)

	// Verify connection
	if connector.client == nil {
		t.Error("Client should not be nil after connect")
	}

	// Test pipeline building
	pipeline := connector.buildPipeline()
	if len(pipeline) == 0 {
		t.Error("Pipeline should have at least one stage")
	}
}

// TestCheckpointManagerPersistence tests checkpoint manager with actual file I/O
func TestCheckpointManagerPersistence(t *testing.T) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "mongodb_checkpoint_*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Create first manager and add checkpoints
	cm1 := NewCheckpointManager(tmpPath)
	
	checkpoints := []*MongoCheckpoint{
		{JobID: "job1", ResumeToken: "token1", Database: "db1", EventCount: 100},
		{JobID: "job2", ResumeToken: "token2", Database: "db2", EventCount: 200},
	}

	for _, cp := range checkpoints {
		if err := cm1.Update(cp); err != nil {
			t.Fatalf("Failed to update checkpoint: %v", err)
		}
	}

	// Create second manager and load
	cm2 := NewCheckpointManager(tmpPath)
	if err := cm2.Load(); err != nil {
		t.Fatalf("Failed to load checkpoints: %v", err)
	}

	// Verify data persisted
	for _, cp := range checkpoints {
		loaded, ok := cm2.Get(cp.JobID)
		if !ok {
			t.Errorf("Checkpoint %s not found after load", cp.JobID)
			continue
		}

		if loaded.ResumeToken != cp.ResumeToken {
			t.Errorf("ResumeToken mismatch for %s: got %s, want %s", cp.JobID, loaded.ResumeToken, cp.ResumeToken)
		}

		if loaded.Database != cp.Database {
			t.Errorf("Database mismatch for %s: got %s, want %s", cp.JobID, loaded.Database, cp.Database)
		}

		if loaded.EventCount != cp.EventCount {
			t.Errorf("EventCount mismatch for %s: got %d, want %d", cp.JobID, loaded.EventCount, cp.EventCount)
		}
	}

	// Delete one checkpoint
	if err := cm2.Delete("job1"); err != nil {
		t.Fatalf("Failed to delete checkpoint: %v", err)
	}

	// Load again and verify deletion
	cm3 := NewCheckpointManager(tmpPath)
	if err := cm3.Load(); err != nil {
		t.Fatalf("Failed to load checkpoints after delete: %v", err)
	}

	if _, ok := cm3.Get("job1"); ok {
		t.Error("job1 should not exist after delete")
	}

	if _, ok := cm3.Get("job2"); !ok {
		t.Error("job2 should still exist after deleting job1")
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BenchmarkChangeStreamProcessing benchmarks change stream processing
func BenchmarkChangeStreamProcessing(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	coll := testClient.Database(testDatabase).Collection("benchmark_changes")

	// Clean up
	coll.Drop(ctx)

	// Pre-insert documents
	docs := make([]interface{}, 1000)
	for i := 0; i < 1000; i++ {
		docs[i] = bson.M{
			"index": i,
			"data":  fmt.Sprintf("data_%d", i),
		}
	}

	_, err := coll.InsertMany(ctx, docs)
	if err != nil {
		b.Fatalf("Failed to insert documents: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Start change stream
		changeStream, err := coll.Watch(ctx, mongo.Pipeline{})
		if err != nil {
			b.Fatalf("Failed to create change stream: %v", err)
		}

		// Update a document
		_, err = coll.UpdateOne(ctx,
			bson.M{"index": i % 1000},
			bson.M{"$inc": bson.M{"count": 1}},
		)
		if err != nil {
			b.Fatalf("Failed to update: %v", err)
		}

		// Receive event
		if changeStream.Next(ctx) {
			var event bson.M
			changeStream.Decode(&event)
			_ = convertBSONToMap(event["fullDocument"].(bson.M))
		}

		changeStream.Close(ctx)
	}
}
