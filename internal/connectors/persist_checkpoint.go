package connectors

import (
	"encoding/json"
	"fmt"
	"os"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/connectors/mongodb"
	"github.com/yogoosoft/elasticrelay/internal/connectors/mysql"
	"github.com/yogoosoft/elasticrelay/internal/connectors/postgresql"
)

// CheckpointFilePath returns the on-disk checkpoint store for a source type.
func CheckpointFilePath(sourceType string) string {
	switch sourceType {
	case "postgresql":
		return "postgresql_checkpoints.json"
	case "mongodb":
		return "mongodb_checkpoints.json"
	default:
		return "checkpoints.json"
	}
}

// LoadPersistedCheckpoint reads the saved checkpoint for jobID from the store
// used by that source type's connector server.
func LoadPersistedCheckpoint(jobID, sourceType string) (*pb.Checkpoint, error) {
	if jobID == "" {
		return nil, fmt.Errorf("job ID is empty")
	}
	switch sourceType {
	case "postgresql":
		return loadPostgreSQLPersistedCheckpoint(jobID)
	case "mysql", "mongodb":
		return loadProtoCheckpointFile(CheckpointFilePath(sourceType), jobID)
	default:
		return nil, fmt.Errorf("unsupported source type %q", sourceType)
	}
}

// IsValidPersistedCheckpoint reports whether cp is meaningful for sourceType.
func IsValidPersistedCheckpoint(cp *pb.Checkpoint, sourceType string) bool {
	if cp == nil {
		return false
	}
	switch sourceType {
	case "mysql":
		return mysql.NewMySQLCheckpointManager().IsValid(cp)
	case "postgresql":
		return postgresql.NewPostgreSQLCheckpointManager().IsValid(cp)
	case "mongodb":
		return mongodb.NewMongoDBCheckpointManager().IsValid(cp)
	default:
		return false
	}
}

func loadProtoCheckpointFile(path, jobID string) (*pb.Checkpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no checkpoint file at %s", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var checkpoints map[string]*pb.Checkpoint
	if err := json.Unmarshal(data, &checkpoints); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cp, ok := checkpoints[jobID]
	if !ok || cp == nil {
		return nil, fmt.Errorf("no checkpoint for job %s in %s", jobID, path)
	}
	return cp, nil
}

type postgresPersistedCheckpoint struct {
	LSN string `json:"lsn"`
}

func loadPostgreSQLPersistedCheckpoint(jobID string) (*pb.Checkpoint, error) {
	path := CheckpointFilePath("postgresql")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no checkpoint file at %s", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var checkpoints map[string]*postgresPersistedCheckpoint
	if err := json.Unmarshal(data, &checkpoints); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	stored, ok := checkpoints[jobID]
	if !ok || stored == nil || stored.LSN == "" {
		return nil, fmt.Errorf("no checkpoint for job %s in %s", jobID, path)
	}
	return &pb.Checkpoint{
		SourceType:  "postgresql",
		PostgresLsn: stored.LSN,
		Position:    stored.LSN,
	}, nil
}