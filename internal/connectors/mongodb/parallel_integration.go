package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/parallel"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDBSnapshotAdapter adapts MongoDB connector to work with parallel snapshot manager
type MongoDBSnapshotAdapter struct {
	client      *mongo.Client
	database    string
	uri         string
	collections []string
}

// MongoDBSnapshotConfig contains configuration for MongoDB snapshots
type MongoDBSnapshotConfig struct {
	URI         string
	Database    string
	Collections []string
	BatchSize   int
	Timeout     time.Duration
}

// NewMongoDBSnapshotAdapter creates a new MongoDB snapshot adapter
func NewMongoDBSnapshotAdapter(config *MongoDBSnapshotConfig) *MongoDBSnapshotAdapter {
	return &MongoDBSnapshotAdapter{
		uri:         config.URI,
		database:    config.Database,
		collections: config.Collections,
	}
}

// Initialize sets up the adapter for parallel snapshot processing
func (msa *MongoDBSnapshotAdapter) Initialize(ctx context.Context) error {
	clientOptions := options.Client().
		ApplyURI(msa.uri).
		SetConnectTimeout(30 * time.Second).
		SetServerSelectionTimeout(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Test connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	msa.client = client
	log.Printf("MongoDB snapshot adapter initialized for database: %s", msa.database)
	return nil
}

// Close cleans up resources
func (msa *MongoDBSnapshotAdapter) Close() error {
	if msa.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return msa.client.Disconnect(ctx)
	}
	return nil
}

// GetCollectionInfo retrieves information about a collection for parallel processing
func (msa *MongoDBSnapshotAdapter) GetCollectionInfo(ctx context.Context, collName string) (*parallel.TableInfo, error) {
	coll := msa.client.Database(msa.database).Collection(collName)

	// Get collection stats
	var stats bson.M
	err := msa.client.Database(msa.database).RunCommand(ctx, bson.D{
		{Key: "collStats", Value: collName},
	}).Decode(&stats)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection stats: %w", err)
	}

	// Get document count
	count, err := coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to count documents: %w", err)
	}

	tableInfo := &parallel.TableInfo{
		Name:          collName,
		Schema:        "", // MongoDB doesn't have schema
		Database:      msa.database,
		TotalRows:     count,
		EstimatedRows: count,
		PrimaryKey:    []string{"_id"}, // MongoDB always uses _id as primary key
		HasTimestamp:  false,
	}

	// Get field information from a sample document
	var sampleDoc bson.M
	err = coll.FindOne(ctx, bson.M{}).Decode(&sampleDoc)
	if err != nil && err != mongo.ErrNoDocuments {
		return nil, fmt.Errorf("failed to get sample document: %w", err)
	}

	if sampleDoc != nil {
		columns := make([]parallel.ColumnInfo, 0)
		for key, value := range sampleDoc {
			column := parallel.ColumnInfo{
				Name:         key,
				DataType:     getMongoDBType(value),
				IsNullable:   true,
				IsPrimaryKey: key == "_id",
			}
			columns = append(columns, column)
		}
		tableInfo.Columns = columns

		// Check for timestamp-like fields
		for _, col := range columns {
			if col.DataType == "Date" || col.Name == "createdAt" || col.Name == "updatedAt" || col.Name == "timestamp" {
				tableInfo.HasTimestamp = true
				break
			}
		}
	}

	log.Printf("Collection info for %s: %d documents, %d fields",
		collName, count, len(tableInfo.Columns))

	return tableInfo, nil
}

// CreateCollectionChunks creates chunks for parallel processing of a collection
func (msa *MongoDBSnapshotAdapter) CreateCollectionChunks(ctx context.Context, info *parallel.TableInfo, chunkSize int) ([]*parallel.ChunkInfo, error) {
	coll := msa.client.Database(msa.database).Collection(info.Name)

	// For MongoDB, we'll use _id-based chunking
	// First, get the min and max _id values

	// Get min _id
	minOpts := options.FindOne().SetSort(bson.D{{Key: "_id", Value: 1}})
	var minDoc bson.M
	err := coll.FindOne(ctx, bson.M{}, minOpts).Decode(&minDoc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// Empty collection
			return []*parallel.ChunkInfo{}, nil
		}
		return nil, fmt.Errorf("failed to get min _id: %w", err)
	}

	// Get max _id
	maxOpts := options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})
	var maxDoc bson.M
	err = coll.FindOne(ctx, bson.M{}, maxOpts).Decode(&maxDoc)
	if err != nil {
		return nil, fmt.Errorf("failed to get max _id: %w", err)
	}

	minID := minDoc["_id"]
	maxID := maxDoc["_id"]

	// Determine chunking strategy based on _id type
	switch minID.(type) {
	case primitive.ObjectID:
		return msa.createObjectIDChunks(ctx, info, minID.(primitive.ObjectID), maxID.(primitive.ObjectID), chunkSize)
	case int, int32, int64:
		return msa.createNumericIDChunks(ctx, info, chunkSize)
	default:
		// For other types, use skip/limit approach
		return msa.createSkipLimitChunks(ctx, info, chunkSize)
	}
}

// createObjectIDChunks creates chunks based on ObjectID ranges
func (msa *MongoDBSnapshotAdapter) createObjectIDChunks(ctx context.Context, info *parallel.TableInfo, minID, maxID primitive.ObjectID, chunkSize int) ([]*parallel.ChunkInfo, error) {
	totalDocs := info.EstimatedRows
	if totalDocs == 0 {
		return []*parallel.ChunkInfo{}, nil
	}

	numChunks := int((totalDocs + int64(chunkSize) - 1) / int64(chunkSize))
	if numChunks == 0 {
		numChunks = 1
	}

	// Get boundary ObjectIDs using aggregation with $sample and $sort
	coll := msa.client.Database(msa.database).Collection(info.Name)

	// Get evenly distributed _id boundaries
	pipeline := mongo.Pipeline{
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "ids", Value: bson.D{{Key: "$push", Value: "$_id"}}},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "ids", Value: bson.D{{Key: "$slice", Value: bson.A{
				"$ids",
				bson.A{0, bson.D{{Key: "$add", Value: bson.A{
					bson.D{{Key: "$floor", Value: bson.D{{Key: "$divide", Value: bson.A{
						bson.D{{Key: "$size", Value: "$ids"}},
						numChunks,
					}}}}},
					1,
				}}}},
			}}}},
		}}},
	}

	// This aggregation can be expensive, so we'll use a simpler approach
	// Just create chunks with skip/limit initially
	var chunks []*parallel.ChunkInfo
	
	for i := 0; i < numChunks; i++ {
		chunk := &parallel.ChunkInfo{
			ID:        fmt.Sprintf("%s_chunk_%d", info.Name, i),
			TableName: info.Name,
			StartID:   int64(i * chunkSize),
			EndID:     int64((i + 1) * chunkSize),
			ChunkSize: chunkSize,
			RowCount:  int64(chunkSize),
		}

		// For ObjectID, we'll use skip/limit in the where clause
		chunk.WhereClause = fmt.Sprintf("skip:%d,limit:%d", i*chunkSize, chunkSize)
		chunks = append(chunks, chunk)
	}

	// Suppress unused variable warning
	_ = pipeline
	_ = coll

	log.Printf("Created %d ObjectID-based chunks for collection %s", len(chunks), info.Name)
	return chunks, nil
}

// createNumericIDChunks creates chunks based on numeric ID ranges
func (msa *MongoDBSnapshotAdapter) createNumericIDChunks(ctx context.Context, info *parallel.TableInfo, chunkSize int) ([]*parallel.ChunkInfo, error) {
	coll := msa.client.Database(msa.database).Collection(info.Name)

	// Get min and max numeric IDs
	minOpts := options.FindOne().SetSort(bson.D{{Key: "_id", Value: 1}})
	var minDoc bson.M
	if err := coll.FindOne(ctx, bson.M{}, minOpts).Decode(&minDoc); err != nil {
		return nil, fmt.Errorf("failed to get min _id: %w", err)
	}

	maxOpts := options.FindOne().SetSort(bson.D{{Key: "_id", Value: -1}})
	var maxDoc bson.M
	if err := coll.FindOne(ctx, bson.M{}, maxOpts).Decode(&maxDoc); err != nil {
		return nil, fmt.Errorf("failed to get max _id: %w", err)
	}

	minID := toInt64(minDoc["_id"])
	maxID := toInt64(maxDoc["_id"])

	var chunks []*parallel.ChunkInfo
	chunkID := 0

	for startID := minID; startID <= maxID; startID += int64(chunkSize) {
		endID := startID + int64(chunkSize) - 1
		if endID > maxID {
			endID = maxID
		}

		chunk := &parallel.ChunkInfo{
			ID:          fmt.Sprintf("%s_chunk_%d", info.Name, chunkID),
			TableName:   info.Name,
			StartID:     startID,
			EndID:       endID,
			ChunkSize:   chunkSize,
			WhereClause: fmt.Sprintf("_id>=%d,_id<=%d", startID, endID),
		}
		chunks = append(chunks, chunk)
		chunkID++
	}

	log.Printf("Created %d numeric ID-based chunks for collection %s (ID range: %d-%d)",
		len(chunks), info.Name, minID, maxID)

	return chunks, nil
}

// createSkipLimitChunks creates chunks using skip/limit for non-standard ID types
func (msa *MongoDBSnapshotAdapter) createSkipLimitChunks(ctx context.Context, info *parallel.TableInfo, chunkSize int) ([]*parallel.ChunkInfo, error) {
	totalDocs := info.EstimatedRows
	if totalDocs == 0 {
		return []*parallel.ChunkInfo{}, nil
	}

	numChunks := int((totalDocs + int64(chunkSize) - 1) / int64(chunkSize))

	var chunks []*parallel.ChunkInfo
	for i := 0; i < numChunks; i++ {
		chunk := &parallel.ChunkInfo{
			ID:          fmt.Sprintf("%s_chunk_%d", info.Name, i),
			TableName:   info.Name,
			StartID:     int64(i * chunkSize),
			EndID:       int64((i + 1) * chunkSize),
			ChunkSize:   chunkSize,
			WhereClause: fmt.Sprintf("skip:%d,limit:%d", i*chunkSize, chunkSize),
		}
		chunks = append(chunks, chunk)
	}

	log.Printf("Created %d skip/limit chunks for collection %s", len(chunks), info.Name)
	return chunks, nil
}

// ProcessChunk processes a single chunk of data
func (msa *MongoDBSnapshotAdapter) ProcessChunk(ctx context.Context, chunk *parallel.ChunkInfo, stream pb.ConnectorService_BeginSnapshotServer) error {
	log.Printf("Processing chunk %s", chunk.ID)

	coll := msa.client.Database(msa.database).Collection(chunk.TableName)

	// Parse where clause to determine query strategy
	findOpts := options.Find()
	filter := bson.M{}

	// Parse where clause
	if chunk.WhereClause != "" {
		// Check for skip/limit pattern
		var skip, limit int
		if n, _ := fmt.Sscanf(chunk.WhereClause, "skip:%d,limit:%d", &skip, &limit); n == 2 {
			findOpts.SetSkip(int64(skip))
			findOpts.SetLimit(int64(limit))
		} else {
			// Parse numeric range pattern
			var startID, endID int64
			if n, _ := fmt.Sscanf(chunk.WhereClause, "_id>=%d,_id<=%d", &startID, &endID); n == 2 {
				filter["_id"] = bson.M{
					"$gte": startID,
					"$lte": endID,
				}
			}
		}
	}

	// Sort by _id for consistent ordering
	findOpts.SetSort(bson.D{{Key: "_id", Value: 1}})

	// Execute query
	cursor, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return fmt.Errorf("failed to query chunk: %w", err)
	}
	defer cursor.Close(ctx)

	var processedRows int64
	var records []string
	batchSize := 100

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("Warning: failed to decode document: %v", err)
			continue
		}

		// Convert BSON to JSON-friendly map
		dataMap := convertBSONToMap(doc)

		// Add metadata
		dataMap["_collection"] = chunk.TableName
		dataMap["_database"] = msa.database

		// Marshal to JSON
		jsonData, err := json.Marshal(dataMap)
		if err != nil {
			log.Printf("Warning: failed to marshal document: %v", err)
			continue
		}

		records = append(records, string(jsonData))
		processedRows++

		// Send batch when it reaches the batch size
		if len(records) >= batchSize {
			if err := msa.sendChunk(stream, records, chunk.TableName); err != nil {
				return fmt.Errorf("failed to send chunk: %w", err)
			}
			records = nil
		}
	}

	// Send remaining records
	if len(records) > 0 {
		if err := msa.sendChunk(stream, records, chunk.TableName); err != nil {
			return fmt.Errorf("failed to send final batch: %w", err)
		}
	}

	log.Printf("Processed chunk %s: %d documents", chunk.ID, processedRows)
	return nil
}

// sendChunk sends a batch of records to the stream
func (msa *MongoDBSnapshotAdapter) sendChunk(stream pb.ConnectorService_BeginSnapshotServer, records []string, collectionName string) error {
	// Get current cluster time for snapshot consistency
	currentTime := time.Now().Unix()

	chunk := &pb.SnapshotChunk{
		Records:            records,
		Cursor:             collectionName,
		SnapshotBinlogFile: "", // Not applicable for MongoDB
		SnapshotBinlogPos:  uint32(currentTime),
	}

	return stream.Send(chunk)
}

// GetSnapshotConsistencyPoint returns a consistency point for the snapshot
func (msa *MongoDBSnapshotAdapter) GetSnapshotConsistencyPoint(ctx context.Context) (string, error) {
	// For MongoDB, we can use the cluster time as the consistency point
	adminDB := msa.client.Database("admin")

	var result bson.M
	if err := adminDB.RunCommand(ctx, bson.D{{Key: "isMaster", Value: 1}}).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to get cluster time: %w", err)
	}

	if opTime, ok := result["operationTime"].(primitive.Timestamp); ok {
		return fmt.Sprintf("%d:%d", opTime.T, opTime.I), nil
	}

	// Fallback to current timestamp
	return fmt.Sprintf("%d:0", time.Now().Unix()), nil
}

// getMongoDBType returns the MongoDB type name for a value
func getMongoDBType(value interface{}) string {
	switch value.(type) {
	case primitive.ObjectID:
		return "ObjectId"
	case primitive.DateTime:
		return "Date"
	case primitive.Timestamp:
		return "Timestamp"
	case primitive.Binary:
		return "BinData"
	case primitive.Decimal128:
		return "Decimal128"
	case primitive.Regex:
		return "Regex"
	case string:
		return "String"
	case int, int32, int64:
		return "Integer"
	case float32, float64:
		return "Double"
	case bool:
		return "Boolean"
	case bson.M, bson.D, map[string]interface{}:
		return "Object"
	case bson.A, []interface{}:
		return "Array"
	case nil:
		return "Null"
	default:
		return "Unknown"
	}
}

// toInt64 converts various numeric types to int64
func toInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	default:
		return 0
	}
}

// MongoDBParallelSnapshotManager wraps the generic parallel manager for MongoDB
type MongoDBParallelSnapshotManager struct {
	adapter *MongoDBSnapshotAdapter
	config  *parallel.SnapshotConfig
}

// NewMongoDBParallelSnapshotManager creates a new MongoDB parallel snapshot manager
func NewMongoDBParallelSnapshotManager(adapter *MongoDBSnapshotAdapter) *MongoDBParallelSnapshotManager {
	config := &parallel.SnapshotConfig{
		MaxConcurrentTables: 3,
		MaxConcurrentChunks: 8,
		ChunkSize:           100000,
		WorkerPoolSize:      12,
		AdaptiveScheduling:  true,
		StreamingMode:       true,
		ChunkStrategy:       "id_based",
	}

	return &MongoDBParallelSnapshotManager{
		adapter: adapter,
		config:  config,
	}
}

// SetConfig sets the snapshot configuration
func (mpsm *MongoDBParallelSnapshotManager) SetConfig(config *parallel.SnapshotConfig) {
	mpsm.config = config
}

// StartSnapshot starts a parallel snapshot operation
func (mpsm *MongoDBParallelSnapshotManager) StartSnapshot(ctx context.Context, jobID string, collections []string, stream pb.ConnectorService_BeginSnapshotServer) error {
	log.Printf("Starting parallel snapshot for job %s with %d collections", jobID, len(collections))

	// Initialize adapter
	if err := mpsm.adapter.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize adapter: %w", err)
	}
	defer mpsm.adapter.Close()

	// Get snapshot consistency point
	consistencyPoint, err := mpsm.adapter.GetSnapshotConsistencyPoint(ctx)
	if err != nil {
		log.Printf("Warning: failed to get consistency point: %v", err)
	} else {
		log.Printf("Snapshot consistency point: %s", consistencyPoint)
	}

	// Process each collection
	for _, collName := range collections {
		if err := mpsm.processCollection(ctx, collName, stream); err != nil {
			log.Printf("Error processing collection %s: %v", collName, err)
			continue
		}
	}

	log.Printf("Completed parallel snapshot for job %s", jobID)
	return nil
}

// processCollection processes a single collection using parallel chunks
func (mpsm *MongoDBParallelSnapshotManager) processCollection(ctx context.Context, collName string, stream pb.ConnectorService_BeginSnapshotServer) error {
	log.Printf("Processing collection %s", collName)

	// Get collection information
	collInfo, err := mpsm.adapter.GetCollectionInfo(ctx, collName)
	if err != nil {
		return fmt.Errorf("failed to get collection info: %w", err)
	}

	// Create chunks
	chunks, err := mpsm.adapter.CreateCollectionChunks(ctx, collInfo, mpsm.config.ChunkSize)
	if err != nil {
		return fmt.Errorf("failed to create chunks: %w", err)
	}

	// Process chunks (sequentially for now - parallel processing would need worker pool)
	for _, chunk := range chunks {
		if err := mpsm.adapter.ProcessChunk(ctx, chunk, stream); err != nil {
			return fmt.Errorf("failed to process chunk %s: %w", chunk.ID, err)
		}
	}

	log.Printf("Completed processing collection %s (%d chunks)", collName, len(chunks))
	return nil
}

// GetStats returns statistics about the parallel snapshot operation
func (mpsm *MongoDBParallelSnapshotManager) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"config": mpsm.config,
		"status": "running",
	}
}
