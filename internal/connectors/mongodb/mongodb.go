package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/config"
	"github.com/yogoosoft/elasticrelay/internal/parallel"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	snapshotChunkSize  = 100
	checkpointFilePath = "mongodb_checkpoints.json"
	// Default chunk size for parallel snapshot
	defaultParallelChunkSize = 100000
)

// Connector manages the MongoDB CDC process using Change Streams
type Connector struct {
	client           *mongo.Client
	database         string
	collectionFilter []string // Collection names to watch (like table_filters)
	uri              string
	clusterType      ClusterType  // Detected cluster type
	clusterInfo      *ClusterInfo // Cluster topology information
	useParallel      bool         // Use parallel snapshot processing
}

// ConnectorOptions contains additional options for the Connector
type ConnectorOptions struct {
	UseParallelSnapshot bool
	ParallelChunkSize   int
}

// DefaultConnectorOptions returns default options
func DefaultConnectorOptions() *ConnectorOptions {
	return &ConnectorOptions{
		UseParallelSnapshot: false,
		ParallelChunkSize:   defaultParallelChunkSize,
	}
}

// NewConnector creates a new MongoDB connector
func NewConnector(cfg *config.Config) (*Connector, error) {
	return NewConnectorWithOptions(cfg, nil)
}

// NewConnectorWithOptions creates a new MongoDB connector with options
func NewConnectorWithOptions(cfg *config.Config, opts *ConnectorOptions) (*Connector, error) {
	if opts == nil {
		opts = DefaultConnectorOptions()
	}

	// Build MongoDB connection URI
	uri := buildMongoURI(cfg)

	return &Connector{
		uri:              uri,
		database:         cfg.DBName,
		collectionFilter: cfg.TableFilters, // Reuse table_filters for collection names
		useParallel:      opts.UseParallelSnapshot,
	}, nil
}

// buildMongoURI constructs the MongoDB connection URI from config
func buildMongoURI(cfg *config.Config) string {
	// Basic URI format: mongodb://user:password@host:port/database
	var uri string
	if cfg.DBUser != "" && cfg.DBPassword != "" {
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%d/%s",
			cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
	} else {
		uri = fmt.Sprintf("mongodb://%s:%d/%s",
			cfg.DBHost, cfg.DBPort, cfg.DBName)
	}

	// Add default options for better reliability
	// Note: authSource=admin is required when authenticating against admin database
	uri += "?directConnection=true&serverSelectionTimeoutMS=5000&authSource=admin"

	return uri
}

// Connect establishes connection to MongoDB
func (c *Connector) Connect(ctx context.Context) error {
	clientOptions := options.Client().
		ApplyURI(c.uri).
		SetConnectTimeout(10 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetHeartbeatInterval(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	c.client = client

	// Detect cluster topology
	clusterInfo, err := c.detectClusterTopology(ctx)
	if err != nil {
		log.Printf("Warning: failed to detect cluster topology: %v", err)
		c.clusterType = ClusterTypeStandalone
	} else {
		c.clusterInfo = clusterInfo
		c.clusterType = clusterInfo.ClusterType
		log.Printf("Connected to MongoDB cluster: type=%s, version=%s", clusterInfo.ClusterType, clusterInfo.Version)
	}

	log.Printf("Connected to MongoDB database: %s", c.database)
	return nil
}

// detectClusterTopology detects the MongoDB cluster topology
func (c *Connector) detectClusterTopology(ctx context.Context) (*ClusterInfo, error) {
	info := &ClusterInfo{
		Timestamp: time.Now(),
	}

	adminDB := c.client.Database("admin")

	// Run isMaster to get basic info
	var isMasterResult bson.M
	if err := adminDB.RunCommand(ctx, bson.D{{Key: "isMaster", Value: 1}}).Decode(&isMasterResult); err != nil {
		return nil, fmt.Errorf("failed to run isMaster: %w", err)
	}

	// Determine cluster type
	if msg, ok := isMasterResult["msg"].(string); ok && msg == "isdbgrid" {
		// Connected to mongos (sharded cluster)
		info.ClusterType = ClusterTypeSharded
	} else if setName, ok := isMasterResult["setName"].(string); ok {
		// Connected to a replica set
		info.ClusterType = ClusterTypeReplicaSet
		info.ReplicaSetName = setName
	} else {
		// Standalone
		info.ClusterType = ClusterTypeStandalone
	}

	// Get MongoDB version
	var buildInfo bson.M
	if err := adminDB.RunCommand(ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&buildInfo); err == nil {
		if version, ok := buildInfo["version"].(string); ok {
			info.Version = version
		}
	}

	return info, nil
}

// GetClusterType returns the detected cluster type
func (c *Connector) GetClusterType() ClusterType {
	return c.clusterType
}

// GetClusterInfo returns the cluster topology information
func (c *Connector) GetClusterInfo() *ClusterInfo {
	return c.clusterInfo
}

// IsSharded returns true if connected to a sharded cluster
func (c *Connector) IsSharded() bool {
	return c.clusterType == ClusterTypeSharded
}

// IsReplicaSet returns true if connected to a replica set
func (c *Connector) IsReplicaSet() bool {
	return c.clusterType == ClusterTypeReplicaSet
}

// Close closes the MongoDB connection
func (c *Connector) Close(ctx context.Context) error {
	if c.client != nil {
		return c.client.Disconnect(ctx)
	}
	return nil
}

// Start runs the CDC process using Change Streams
func (c *Connector) Start(stream pb.ConnectorService_StartCdcServer, startCheckpoint *pb.Checkpoint) error {
	ctx := stream.Context()

	// Connect to MongoDB
	if err := c.Connect(ctx); err != nil {
		return err
	}
	defer c.Close(ctx)

	// Create event handler
	handler := &EventHandler{
		stream:   stream,
		database: c.database,
	}

	// Build Change Stream options
	opts := options.ChangeStream().
		SetFullDocument(options.UpdateLookup). // Get full document for updates
		SetBatchSize(100)

	// Set resume token if checkpoint exists
	if startCheckpoint != nil && startCheckpoint.MongoResumeToken != "" {
		log.Printf("Starting CDC from checkpoint resume token: %s", startCheckpoint.MongoResumeToken)
		resumeToken, err := decodeResumeToken(startCheckpoint.MongoResumeToken)
		if err != nil {
			log.Printf("Warning: failed to decode resume token, starting from current: %v", err)
		} else {
			opts.SetResumeAfter(resumeToken)
		}
	} else {
		log.Println("No checkpoint provided, starting from current position")
	}

	// Build pipeline for filtering collections
	pipeline := c.buildPipeline()

	// Watch the database or specific collections
	var changeStream *mongo.ChangeStream
	var err error

	if len(c.collectionFilter) == 1 {
		// Watch single collection
		coll := c.client.Database(c.database).Collection(c.collectionFilter[0])
		changeStream, err = coll.Watch(ctx, pipeline, opts)
	} else {
		// Watch entire database (with optional pipeline filter)
		db := c.client.Database(c.database)
		changeStream, err = db.Watch(ctx, pipeline, opts)
	}

	if err != nil {
		return fmt.Errorf("failed to create change stream: %w", err)
	}
	defer changeStream.Close(ctx)

	log.Printf("MongoDB Change Stream started for database: %s", c.database)

	// Process change events
	for changeStream.Next(ctx) {
		var changeEvent ChangeEvent
		if err := changeStream.Decode(&changeEvent); err != nil {
			log.Printf("Error decoding change event: %v", err)
			continue
		}

		// Get resume token for checkpoint
		resumeToken := changeStream.ResumeToken()

		if err := handler.HandleChange(&changeEvent, resumeToken); err != nil {
			log.Printf("Error handling change event: %v", err)
			return err
		}
	}

	if err := changeStream.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			log.Println("Context cancelled, stopping CDC")
			return nil
		}
		return fmt.Errorf("change stream error: %w", err)
	}

	return nil
}

// buildPipeline builds the aggregation pipeline for filtering
func (c *Connector) buildPipeline() mongo.Pipeline {
	pipeline := mongo.Pipeline{}

	// Filter by operation types we care about
	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "operationType", Value: bson.D{
			{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}},
		}},
	}}}
	pipeline = append(pipeline, matchStage)

	// Filter by collection names if specified (for database-level watch)
	if len(c.collectionFilter) > 1 {
		collectionMatch := bson.D{{Key: "$match", Value: bson.D{
			{Key: "ns.coll", Value: bson.D{
				{Key: "$in", Value: c.collectionFilter},
			}},
		}}}
		pipeline = append(pipeline, collectionMatch)
	}

	return pipeline
}

// ChangeEvent represents a MongoDB Change Stream event
type ChangeEvent struct {
	ID                primitive.M         `bson:"_id"`
	OperationType     string              `bson:"operationType"`
	FullDocument      bson.M              `bson:"fullDocument,omitempty"`
	Ns                Namespace           `bson:"ns"`
	DocumentKey       bson.M              `bson:"documentKey"`
	UpdateDescription *UpdateDescription  `bson:"updateDescription,omitempty"`
	ClusterTime       primitive.Timestamp `bson:"clusterTime"`
}

// Namespace represents the database and collection
type Namespace struct {
	Database   string `bson:"db"`
	Collection string `bson:"coll"`
}

// UpdateDescription contains update details
type UpdateDescription struct {
	UpdatedFields   bson.M   `bson:"updatedFields,omitempty"`
	RemovedFields   []string `bson:"removedFields,omitempty"`
	TruncatedArrays []bson.M `bson:"truncatedArrays,omitempty"`
}

// EventHandler handles MongoDB change events
type EventHandler struct {
	stream   pb.ConnectorService_StartCdcServer
	database string
}

// HandleChange processes a single change event
func (h *EventHandler) HandleChange(event *ChangeEvent, resumeToken bson.Raw) error {
	// Map MongoDB operation to our operation type
	var op string
	switch event.OperationType {
	case "insert":
		op = "INSERT"
	case "update", "replace":
		op = "UPDATE"
	case "delete":
		op = "DELETE"
	default:
		log.Printf("Unknown operation type: %s", event.OperationType)
		return nil
	}

	// Get document data
	var dataMap map[string]interface{}
	if event.FullDocument != nil {
		dataMap = convertBSONToMap(event.FullDocument)
	} else if op == "DELETE" {
		// For deletes, use document key as data
		dataMap = convertBSONToMap(event.DocumentKey)
	} else {
		log.Printf("Warning: no document data for %s operation", op)
		return nil
	}

	// Add metadata
	dataMap["_collection"] = event.Ns.Collection
	dataMap["_database"] = event.Ns.Database

	// Convert to JSON
	jsonData, err := json.Marshal(dataMap)
	if err != nil {
		log.Printf("Failed to marshal document to JSON: %v", err)
		return nil
	}

	// Get primary key (_id)
	pkValue := getPrimaryKey(event.DocumentKey)

	// Encode resume token for checkpoint
	resumeTokenStr := encodeResumeToken(resumeToken)

	// Create and send change event
	changeEvent := &pb.ChangeEvent{
		Op: op,
		Checkpoint: &pb.Checkpoint{
			MongoResumeToken: resumeTokenStr,
		},
		PrimaryKey: pkValue,
		Data:       string(jsonData),
	}

	if err := h.stream.Send(changeEvent); err != nil {
		log.Printf("Failed to send ChangeEvent to stream: %v", err)
		return err
	}

	return nil
}

// Server implements the ConnectorService for MongoDB
type Server struct {
	pb.UnimplementedConnectorServiceServer
	config          *config.Config
	checkpointMutex sync.RWMutex
	useParallel     bool // Use parallel snapshot processing
	parallelConfig  *parallel.SnapshotConfig
}

// ServerOptions contains options for the MongoDB server
type ServerOptions struct {
	UseParallelSnapshot bool
	ParallelConfig      *parallel.SnapshotConfig
}

// DefaultServerOptions returns default server options
func DefaultServerOptions() *ServerOptions {
	return &ServerOptions{
		UseParallelSnapshot: false,
		ParallelConfig:      parallel.DefaultSnapshotConfig(),
	}
}

// NewServer creates a new MongoDB connector server
func NewServer(cfg *config.Config) (*Server, error) {
	return NewServerWithOptions(cfg, nil)
}

// NewServerWithOptions creates a new MongoDB connector server with options
func NewServerWithOptions(cfg *config.Config, opts *ServerOptions) (*Server, error) {
	if opts == nil {
		opts = DefaultServerOptions()
	}

	log.Printf("MongoDB Connector Server created (parallel=%v)", opts.UseParallelSnapshot)
	return &Server{
		config:         cfg,
		useParallel:    opts.UseParallelSnapshot,
		parallelConfig: opts.ParallelConfig,
	}, nil
}

// BeginSnapshot implements the gRPC service endpoint for taking snapshots
func (s *Server) BeginSnapshot(req *pb.BeginSnapshotRequest, stream pb.ConnectorService_BeginSnapshotServer) error {
	log.Printf("Received BeginSnapshot request for job %s, collection %s (parallel=%v)", req.JobId, req.TableName, s.useParallel)

	// Use parallel snapshot if enabled
	if s.useParallel {
		return s.beginParallelSnapshot(req, stream)
	}

	// Standard snapshot processing
	return s.beginStandardSnapshot(req, stream)
}

// beginParallelSnapshot uses the parallel snapshot manager for efficient processing
func (s *Server) beginParallelSnapshot(req *pb.BeginSnapshotRequest, stream pb.ConnectorService_BeginSnapshotServer) error {
	ctx := stream.Context()

	// Build URI
	uri := buildMongoURI(s.config)

	// Create snapshot adapter
	adapterConfig := &MongoDBSnapshotConfig{
		URI:         uri,
		Database:    s.config.DBName,
		Collections: []string{req.TableName},
		BatchSize:   snapshotChunkSize,
		Timeout:     30 * time.Second,
	}

	adapter := NewMongoDBSnapshotAdapter(adapterConfig)

	// Create parallel snapshot manager
	manager := NewMongoDBParallelSnapshotManager(adapter)
	if s.parallelConfig != nil {
		manager.SetConfig(s.parallelConfig)
	}

	// Start parallel snapshot
	return manager.StartSnapshot(ctx, req.JobId, []string{req.TableName}, stream)
}

// beginStandardSnapshot uses the standard sequential snapshot processing
func (s *Server) beginStandardSnapshot(req *pb.BeginSnapshotRequest, stream pb.ConnectorService_BeginSnapshotServer) error {
	ctx := stream.Context()

	// Create connector
	conn, err := NewConnector(s.config)
	if err != nil {
		return fmt.Errorf("failed to create connector: %w", err)
	}

	// Connect
	if err := conn.Connect(ctx); err != nil {
		return err
	}
	defer conn.Close(ctx)

	// Log cluster info
	if conn.clusterInfo != nil {
		log.Printf("Snapshot from %s cluster (version: %s)", conn.clusterType, conn.clusterInfo.Version)
	}

	// Get collection
	coll := conn.client.Database(conn.database).Collection(req.TableName)

	// Query all documents with sorting for consistent ordering
	findOpts := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}})
	cursor, err := coll.Find(ctx, bson.M{}, findOpts)
	if err != nil {
		return fmt.Errorf("failed to query collection %s: %w", req.TableName, err)
	}
	defer cursor.Close(ctx)

	var records []string
	var processedCount int64

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("Failed to decode document: %v", err)
			continue
		}

		// Convert BSON to JSON-friendly map
		dataMap := convertBSONToMap(doc)

		// Add metadata (use _table to be consistent with ES sink expectations)
		dataMap["_table"] = req.TableName
		dataMap["_database"] = conn.database

		jsonData, err := json.Marshal(dataMap)
		if err != nil {
			log.Printf("Failed to marshal document to JSON: %v", err)
			continue
		}

		records = append(records, string(jsonData))
		processedCount++

		// Send in chunks
		if len(records) >= snapshotChunkSize {
			if err := stream.Send(&pb.SnapshotChunk{
				Records: records,
			}); err != nil {
				return fmt.Errorf("failed to send snapshot chunk: %w", err)
			}
			records = nil
		}
	}

	// Send remaining records
	if len(records) > 0 {
		if err := stream.Send(&pb.SnapshotChunk{
			Records: records,
		}); err != nil {
			return fmt.Errorf("failed to send final snapshot chunk: %w", err)
		}
	}

	log.Printf("Finished snapshot for job %s, collection %s: %d documents", req.JobId, req.TableName, processedCount)
	return nil
}

// StartCdc implements the gRPC service endpoint for CDC
func (s *Server) StartCdc(req *pb.StartCdcRequest, stream pb.ConnectorService_StartCdcServer) error {
	log.Printf("Received StartCdc request for job %s", req.JobId)

	startCheckpoint := req.StartCheckpoint
	if startCheckpoint == nil || startCheckpoint.MongoResumeToken == "" {
		log.Printf("No checkpoint in request for job %s, attempting to load from file.", req.JobId)
		loadedCheckpoint, err := s.loadCheckpoint(req.JobId)
		if err != nil {
			log.Printf("Could not load checkpoint for job %s: %v. This is normal if it's a new job.", req.JobId, err)
		} else {
			startCheckpoint = loadedCheckpoint
		}
	}

	conn, err := NewConnector(s.config)
	if err != nil {
		log.Printf("Failed to create connector: %v", err)
		return err
	}

	return conn.Start(stream, startCheckpoint)
}

// CommitCheckpoint implements the gRPC service endpoint for checkpoint commits
func (s *Server) CommitCheckpoint(ctx context.Context, req *pb.CommitCheckpointRequest) (*pb.CommitCheckpointResponse, error) {
	if req.Checkpoint == nil {
		return nil, fmt.Errorf("commit request has no checkpoint")
	}

	s.checkpointMutex.Lock()
	defer s.checkpointMutex.Unlock()

	checkpoints, err := s.loadAllCheckpoints()
	if err != nil {
		return nil, err
	}

	checkpoints[req.JobId] = req.Checkpoint

	data, err := json.MarshalIndent(checkpoints, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal checkpoints: %w", err)
	}

	if err := os.WriteFile(checkpointFilePath, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to write checkpoints file: %w", err)
	}

	log.Printf("Successfully committed checkpoint for job %s to %s", req.JobId, checkpointFilePath)
	return &pb.CommitCheckpointResponse{Success: true}, nil
}

func (s *Server) loadCheckpoint(jobId string) (*pb.Checkpoint, error) {
	s.checkpointMutex.RLock()
	defer s.checkpointMutex.RUnlock()

	checkpoints, err := s.loadAllCheckpoints()
	if err != nil {
		return nil, err
	}

	cp, ok := checkpoints[jobId]
	if !ok {
		return nil, fmt.Errorf("no checkpoint found for job ID: %s", jobId)
	}
	return cp, nil
}

func (s *Server) loadAllCheckpoints() (map[string]*pb.Checkpoint, error) {
	data, err := os.ReadFile(checkpointFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*pb.Checkpoint), nil
		}
		return nil, fmt.Errorf("failed to read checkpoints file: %w", err)
	}

	var checkpoints map[string]*pb.Checkpoint
	if err := json.Unmarshal(data, &checkpoints); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoints: %w", err)
	}
	return checkpoints, nil
}
