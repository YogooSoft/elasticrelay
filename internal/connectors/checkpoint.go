package connectors

import (
	"fmt"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// CheckpointManager defines the interface for managing checkpoints across different data sources
type CheckpointManager interface {
	// IsValid checks if a checkpoint is valid for this connector type
	IsValid(checkpoint *pb.Checkpoint) bool

	// CreateCheckpoint creates a new checkpoint from a position value
	// The position parameter type depends on the connector:
	// - MySQL: MySQLPosition struct
	// - PostgreSQL: string (LSN)
	// - MongoDB: string (resume token)
	CreateCheckpoint(position interface{}) (*pb.Checkpoint, error)

	// ParseCheckpoint parses a checkpoint into a connector-specific position format
	ParseCheckpoint(checkpoint *pb.Checkpoint) (interface{}, error)

	// GetSourceType returns the source type identifier (e.g., "mysql", "postgresql")
	GetSourceType() string

	// Compare compares two checkpoints and returns:
	// -1 if a < b, 0 if a == b, 1 if a > b, error if not comparable
	Compare(a, b *pb.Checkpoint) (int, error)
}

// BaseCheckpointManager provides common functionality for checkpoint managers
type BaseCheckpointManager struct {
	sourceType string
}

// NewBaseCheckpointManager creates a new base checkpoint manager
func NewBaseCheckpointManager(sourceType string) *BaseCheckpointManager {
	return &BaseCheckpointManager{
		sourceType: sourceType,
	}
}

// GetSourceType returns the source type identifier
func (b *BaseCheckpointManager) GetSourceType() string {
	return b.sourceType
}

// MigrateFromLegacy converts a legacy checkpoint format to the new universal format
// This helps with backward compatibility during the migration period
func MigrateFromLegacy(legacy *pb.Checkpoint, manager CheckpointManager) (*pb.Checkpoint, error) {
	if legacy == nil {
		return nil, fmt.Errorf("legacy checkpoint is nil")
	}

	// If already using new format, return as-is
	if legacy.Position != "" && legacy.SourceType != "" {
		return legacy, nil
	}

	// Try to parse legacy format and create new checkpoint
	position, err := manager.ParseCheckpoint(legacy)
	if err != nil {
		return nil, fmt.Errorf("failed to parse legacy checkpoint: %w", err)
	}

	return manager.CreateCheckpoint(position)
}

// NewCheckpointFromPosition is a helper function to create a checkpoint with common fields
func NewCheckpointFromPosition(sourceType string, position string, metadata map[string]string) *pb.Checkpoint {
	checkpoint := &pb.Checkpoint{
		Position:   position,
		SourceType: sourceType,
		Timestamp:  time.Now().Unix(),
	}

	if metadata != nil {
		checkpoint.Metadata = metadata
	}

	return checkpoint
}

