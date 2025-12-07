package mongodb

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ClusterType represents the type of MongoDB deployment
type ClusterType string

const (
	// ClusterTypeStandalone represents a standalone MongoDB instance
	ClusterTypeStandalone ClusterType = "standalone"
	// ClusterTypeReplicaSet represents a MongoDB Replica Set
	ClusterTypeReplicaSet ClusterType = "replicaset"
	// ClusterTypeSharded represents a Sharded MongoDB Cluster
	ClusterTypeSharded ClusterType = "sharded"
)

// ShardedConnector supports monitoring a sharded MongoDB cluster via Change Streams
type ShardedConnector struct {
	client      *mongo.Client
	mongosURI   string
	database    string
	collections []string
	clusterInfo *ClusterInfo
}

// ClusterInfo contains information about the MongoDB cluster topology
type ClusterInfo struct {
	ClusterType    ClusterType       `json:"cluster_type"`
	Shards         []ShardInfo       `json:"shards,omitempty"`
	ConfigServers  []string          `json:"config_servers,omitempty"`
	MongosHosts    []string          `json:"mongos_hosts,omitempty"`
	ReplicaSetName string            `json:"replica_set_name,omitempty"`
	Version        string            `json:"version,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
}

// ShardInfo contains information about a single shard
type ShardInfo struct {
	ID       string   `json:"id"`
	Host     string   `json:"host"`
	State    int      `json:"state"`
	Tags     []string `json:"tags,omitempty"`
	Draining bool     `json:"draining"`
}

// ShardedConnectorConfig contains configuration for ShardedConnector
type ShardedConnectorConfig struct {
	MongosURI   string
	Database    string
	Collections []string
	// Options for Change Stream
	BatchSize        int32
	MaxAwaitTime     time.Duration
	FullDocument     options.FullDocument
	// Connection options
	ConnectTimeout   time.Duration
	ReadPreference   string
}

// DefaultShardedConnectorConfig returns default configuration
func DefaultShardedConnectorConfig() *ShardedConnectorConfig {
	return &ShardedConnectorConfig{
		BatchSize:        100,
		MaxAwaitTime:     time.Second * 10,
		FullDocument:     options.UpdateLookup,
		ConnectTimeout:   time.Second * 30,
		ReadPreference:   "secondary",
	}
}

// NewShardedConnector creates a new ShardedConnector
func NewShardedConnector(cfg *ShardedConnectorConfig) (*ShardedConnector, error) {
	if cfg.MongosURI == "" {
		return nil, fmt.Errorf("mongos URI is required")
	}

	if cfg.Database == "" {
		return nil, fmt.Errorf("database name is required")
	}

	return &ShardedConnector{
		mongosURI:   cfg.MongosURI,
		database:    cfg.Database,
		collections: cfg.Collections,
	}, nil
}

// Connect establishes connection to the MongoDB cluster via mongos
func (sc *ShardedConnector) Connect(ctx context.Context) error {
	clientOptions := options.Client().
		ApplyURI(sc.mongosURI).
		SetConnectTimeout(30 * time.Second).
		SetServerSelectionTimeout(10 * time.Second).
		SetHeartbeatInterval(10 * time.Second)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB cluster: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB cluster: %w", err)
	}

	sc.client = client

	// Detect cluster topology
	clusterInfo, err := sc.detectClusterTopology(ctx)
	if err != nil {
		log.Printf("Warning: failed to detect cluster topology: %v", err)
	} else {
		sc.clusterInfo = clusterInfo
		log.Printf("Connected to MongoDB cluster: type=%s, shards=%d",
			clusterInfo.ClusterType, len(clusterInfo.Shards))
	}

	return nil
}

// Close closes the MongoDB connection
func (sc *ShardedConnector) Close(ctx context.Context) error {
	if sc.client != nil {
		return sc.client.Disconnect(ctx)
	}
	return nil
}

// GetClusterInfo returns information about the cluster
func (sc *ShardedConnector) GetClusterInfo() *ClusterInfo {
	return sc.clusterInfo
}

// detectClusterTopology detects the MongoDB cluster topology
func (sc *ShardedConnector) detectClusterTopology(ctx context.Context) (*ClusterInfo, error) {
	info := &ClusterInfo{
		Timestamp: time.Now(),
	}

	// Check if this is a sharded cluster by querying config.shards
	adminDB := sc.client.Database("admin")

	// Run isMaster to get basic info
	var isMasterResult bson.M
	if err := adminDB.RunCommand(ctx, bson.D{{Key: "isMaster", Value: 1}}).Decode(&isMasterResult); err != nil {
		return nil, fmt.Errorf("failed to run isMaster: %w", err)
	}

	// Determine cluster type
	if msg, ok := isMasterResult["msg"].(string); ok && msg == "isdbgrid" {
		// Connected to mongos
		info.ClusterType = ClusterTypeSharded

		// Get shard information
		shards, err := sc.getShardInfo(ctx)
		if err != nil {
			log.Printf("Warning: failed to get shard info: %v", err)
		} else {
			info.Shards = shards
		}

		// Get config servers
		configServers, err := sc.getConfigServers(ctx)
		if err != nil {
			log.Printf("Warning: failed to get config servers: %v", err)
		} else {
			info.ConfigServers = configServers
		}
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

// getShardInfo retrieves information about all shards in the cluster
func (sc *ShardedConnector) getShardInfo(ctx context.Context) ([]ShardInfo, error) {
	configDB := sc.client.Database("config")
	shardsColl := configDB.Collection("shards")

	cursor, err := shardsColl.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to query shards: %w", err)
	}
	defer cursor.Close(ctx)

	var shards []ShardInfo
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		shard := ShardInfo{
			ID: getString(doc, "_id"),
			Host: getString(doc, "host"),
		}

		if state, ok := doc["state"].(int32); ok {
			shard.State = int(state)
		}

		if draining, ok := doc["draining"].(bool); ok {
			shard.Draining = draining
		}

		if tags, ok := doc["tags"].(bson.A); ok {
			for _, tag := range tags {
				if tagStr, ok := tag.(string); ok {
					shard.Tags = append(shard.Tags, tagStr)
				}
			}
		}

		shards = append(shards, shard)
	}

	return shards, nil
}

// getConfigServers retrieves the config server addresses
func (sc *ShardedConnector) getConfigServers(ctx context.Context) ([]string, error) {
	adminDB := sc.client.Database("admin")

	var result bson.M
	if err := adminDB.RunCommand(ctx, bson.D{{Key: "getShardMap", Value: 1}}).Decode(&result); err != nil {
		// Fallback: try to get from config.settings
		return sc.getConfigServersFromSettings(ctx)
	}

	var configServers []string
	if mapResult, ok := result["map"].(bson.M); ok {
		if config, ok := mapResult["config"].(string); ok {
			configServers = append(configServers, config)
		}
	}

	return configServers, nil
}

// getConfigServersFromSettings is a fallback method to get config servers
func (sc *ShardedConnector) getConfigServersFromSettings(ctx context.Context) ([]string, error) {
	// This is a fallback that may not work on all versions
	return nil, nil
}

// WatchShardedCluster watches the entire sharded cluster for changes via mongos
func (sc *ShardedConnector) WatchShardedCluster(ctx context.Context, opts *options.ChangeStreamOptions) (*mongo.ChangeStream, error) {
	if sc.client == nil {
		return nil, fmt.Errorf("not connected to MongoDB cluster")
	}

	// Build pipeline
	pipeline := sc.buildPipeline()

	// Watch at the database level (via mongos, this covers all shards)
	db := sc.client.Database(sc.database)
	changeStream, err := db.Watch(ctx, pipeline, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create change stream on sharded cluster: %w", err)
	}

	return changeStream, nil
}

// WatchCollection watches a specific collection across all shards
func (sc *ShardedConnector) WatchCollection(ctx context.Context, collectionName string, opts *options.ChangeStreamOptions) (*mongo.ChangeStream, error) {
	if sc.client == nil {
		return nil, fmt.Errorf("not connected to MongoDB cluster")
	}

	// Build a pipeline that filters for the operation types we care about
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "operationType", Value: bson.D{
				{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}},
			}},
		}}},
	}

	coll := sc.client.Database(sc.database).Collection(collectionName)
	changeStream, err := coll.Watch(ctx, pipeline, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create change stream on collection %s: %w", collectionName, err)
	}

	return changeStream, nil
}

// buildPipeline builds the aggregation pipeline for the sharded cluster watch
func (sc *ShardedConnector) buildPipeline() mongo.Pipeline {
	pipeline := mongo.Pipeline{}

	// Filter by operation types
	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "operationType", Value: bson.D{
			{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}},
		}},
	}}}
	pipeline = append(pipeline, matchStage)

	// Filter by collections if specified
	if len(sc.collections) > 0 {
		collectionMatch := bson.D{{Key: "$match", Value: bson.D{
			{Key: "ns.coll", Value: bson.D{
				{Key: "$in", Value: sc.collections},
			}},
		}}}
		pipeline = append(pipeline, collectionMatch)
	}

	return pipeline
}

// GetShardForDocument determines which shard a document belongs to
// This is useful for understanding data distribution
func (sc *ShardedConnector) GetShardForDocument(ctx context.Context, collectionName string, documentKey bson.M) (string, error) {
	if sc.client == nil {
		return "", fmt.Errorf("not connected to MongoDB cluster")
	}

	// Query the config.chunks collection to find the shard
	// This is a simplified implementation - in practice, you'd need to
	// evaluate the shard key against chunk boundaries

	// Get the collection's shard key
	shardKey, err := sc.getShardKey(ctx, collectionName)
	if err != nil {
		return "", fmt.Errorf("failed to get shard key: %w", err)
	}

	if shardKey == nil {
		// Collection is not sharded
		return "primary", nil
	}

	// For now, return a placeholder - full implementation would need
	// to evaluate the document against chunk boundaries
	return "unknown", nil
}

// getShardKey retrieves the shard key for a collection
func (sc *ShardedConnector) getShardKey(ctx context.Context, collectionName string) (bson.M, error) {
	configDB := sc.client.Database("config")
	collsColl := configDB.Collection("collections")

	nsName := fmt.Sprintf("%s.%s", sc.database, collectionName)

	var result bson.M
	err := collsColl.FindOne(ctx, bson.M{"_id": nsName}).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // Not sharded
		}
		return nil, fmt.Errorf("failed to query collection config: %w", err)
	}

	if key, ok := result["key"].(bson.M); ok {
		return key, nil
	}

	return nil, nil
}

// IsCollectionSharded checks if a collection is sharded
func (sc *ShardedConnector) IsCollectionSharded(ctx context.Context, collectionName string) (bool, error) {
	shardKey, err := sc.getShardKey(ctx, collectionName)
	if err != nil {
		return false, err
	}
	return shardKey != nil, nil
}

// GetChunkDistribution returns the chunk distribution across shards for a collection
func (sc *ShardedConnector) GetChunkDistribution(ctx context.Context, collectionName string) (map[string]int, error) {
	if sc.client == nil {
		return nil, fmt.Errorf("not connected to MongoDB cluster")
	}

	configDB := sc.client.Database("config")
	chunksColl := configDB.Collection("chunks")

	nsName := fmt.Sprintf("%s.%s", sc.database, collectionName)

	// Aggregate to count chunks per shard
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "ns", Value: nsName}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$shard"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
	}

	cursor, err := chunksColl.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate chunks: %w", err)
	}
	defer cursor.Close(ctx)

	distribution := make(map[string]int)
	for cursor.Next(ctx) {
		var result bson.M
		if err := cursor.Decode(&result); err != nil {
			continue
		}

		shardID := getString(result, "_id")
		if count, ok := result["count"].(int32); ok {
			distribution[shardID] = int(count)
		}
	}

	return distribution, nil
}

// HandleMigrationEvent handles chunk migration events
// This is important for maintaining consistency during migrations
type MigrationEvent struct {
	Type       string    `json:"type"` // "moveChunk", "split", "merge"
	Namespace  string    `json:"namespace"`
	FromShard  string    `json:"from_shard,omitempty"`
	ToShard    string    `json:"to_shard,omitempty"`
	ChunkMin   bson.M    `json:"chunk_min,omitempty"`
	ChunkMax   bson.M    `json:"chunk_max,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	InProgress bool      `json:"in_progress"`
}

// GetActiveMigrations returns currently active chunk migrations
func (sc *ShardedConnector) GetActiveMigrations(ctx context.Context) ([]MigrationEvent, error) {
	if sc.client == nil {
		return nil, fmt.Errorf("not connected to MongoDB cluster")
	}

	configDB := sc.client.Database("config")
	locksColl := configDB.Collection("locks")

	// Query for active migration locks
	cursor, err := locksColl.Find(ctx, bson.M{
		"_id":   bson.M{"$regex": "^moveChunk"},
		"state": 2, // Active state
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query migration locks: %w", err)
	}
	defer cursor.Close(ctx)

	var migrations []MigrationEvent
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		migration := MigrationEvent{
			Type:       "moveChunk",
			InProgress: true,
			Timestamp:  time.Now(),
		}

		if id, ok := doc["_id"].(string); ok {
			migration.Namespace = id
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// WatchShardedClusterWithMigrationAwareness watches the cluster while being aware of migrations
// This provides additional safety for maintaining consistency during chunk migrations
func (sc *ShardedConnector) WatchShardedClusterWithMigrationAwareness(
	ctx context.Context,
	opts *options.ChangeStreamOptions,
	migrationCallback func(MigrationEvent),
) (*mongo.ChangeStream, error) {
	
	// Start a goroutine to monitor migrations
	if migrationCallback != nil {
		go sc.monitorMigrations(ctx, migrationCallback)
	}

	return sc.WatchShardedCluster(ctx, opts)
}

// monitorMigrations monitors for chunk migrations
func (sc *ShardedConnector) monitorMigrations(ctx context.Context, callback func(MigrationEvent)) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastMigrations []MigrationEvent

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			migrations, err := sc.GetActiveMigrations(ctx)
			if err != nil {
				log.Printf("Error checking migrations: %v", err)
				continue
			}

			// Detect new migrations
			for _, m := range migrations {
				isNew := true
				for _, lm := range lastMigrations {
					if m.Namespace == lm.Namespace {
						isNew = false
						break
					}
				}
				if isNew {
					callback(m)
				}
			}

			// Detect completed migrations
			for _, lm := range lastMigrations {
				stillActive := false
				for _, m := range migrations {
					if m.Namespace == lm.Namespace {
						stillActive = true
						break
					}
				}
				if !stillActive {
					completedMigration := lm
					completedMigration.InProgress = false
					callback(completedMigration)
				}
			}

			lastMigrations = migrations
		}
	}
}

// getString safely extracts a string from a bson.M
func getString(doc bson.M, key string) string {
	if val, ok := doc[key].(string); ok {
		return val
	}
	return ""
}

// CreateChangeStreamOptions creates ChangeStreamOptions with recommended settings for sharded clusters
func CreateChangeStreamOptions() *options.ChangeStreamOptions {
	return options.ChangeStream().
		SetFullDocument(options.UpdateLookup).
		SetBatchSize(100).
		SetMaxAwaitTime(10 * time.Second)
}
