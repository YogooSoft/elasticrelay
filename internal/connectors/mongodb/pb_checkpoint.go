package mongodb

import (
	"fmt"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// MongoDBCheckpointManager validates pb.Checkpoint values persisted for MongoDB CDC.
type MongoDBCheckpointManager struct {
	sourceType string
}

// NewMongoDBCheckpointManager creates a MongoDB checkpoint manager for pb.Checkpoint.
func NewMongoDBCheckpointManager() *MongoDBCheckpointManager {
	return &MongoDBCheckpointManager{sourceType: "mongodb"}
}

// GetSourceType returns the source type identifier.
func (m *MongoDBCheckpointManager) GetSourceType() string {
	return m.sourceType
}

// IsValid checks if a checkpoint is valid for MongoDB change streams.
func (m *MongoDBCheckpointManager) IsValid(checkpoint *pb.Checkpoint) bool {
	if checkpoint == nil {
		return false
	}
	if checkpoint.MongoResumeToken == "" {
		return false
	}
	_, err := decodeResumeToken(checkpoint.MongoResumeToken)
	return err == nil
}

// CreateCheckpoint stores a resume token string in pb.Checkpoint format.
func (m *MongoDBCheckpointManager) CreateCheckpoint(position interface{}) (*pb.Checkpoint, error) {
	token, ok := position.(string)
	if !ok || token == "" {
		return nil, fmt.Errorf("MongoDB position must be a non-empty resume token string")
	}
	return &pb.Checkpoint{
		SourceType:       m.sourceType,
		MongoResumeToken: token,
		Position:         token,
		Timestamp:        time.Now().Unix(),
	}, nil
}

// ParseCheckpoint returns the MongoDB resume token.
func (m *MongoDBCheckpointManager) ParseCheckpoint(checkpoint *pb.Checkpoint) (interface{}, error) {
	if checkpoint == nil || checkpoint.MongoResumeToken == "" {
		return nil, fmt.Errorf("checkpoint does not contain a MongoDB resume token")
	}
	return checkpoint.MongoResumeToken, nil
}

// Compare reports whether two resume tokens are equal.
func (m *MongoDBCheckpointManager) Compare(a, b *pb.Checkpoint) (int, error) {
	tA, err := m.ParseCheckpoint(a)
	if err != nil {
		return 0, err
	}
	tB, err := m.ParseCheckpoint(b)
	if err != nil {
		return 0, err
	}
	if tA == tB {
		return 0, nil
	}
	return 1, fmt.Errorf("MongoDB resume tokens are not ordered; unequal tokens are incomparable")
}