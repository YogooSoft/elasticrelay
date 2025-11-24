package mysql

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// MySQLCheckpointManager manages checkpoints for MySQL connections
type MySQLCheckpointManager struct {
	sourceType string
}

// MySQLPosition represents a MySQL binlog position
type MySQLPosition struct {
	File string `json:"file"`
	Pos  uint32 `json:"pos"`
}

// NewMySQLCheckpointManager creates a new MySQL checkpoint manager
func NewMySQLCheckpointManager() *MySQLCheckpointManager {
	return &MySQLCheckpointManager{
		sourceType: "mysql",
	}
}

// GetSourceType returns the source type identifier
func (m *MySQLCheckpointManager) GetSourceType() string {
	return m.sourceType
}

// IsValid checks if a checkpoint is valid for MySQL
func (m *MySQLCheckpointManager) IsValid(checkpoint *pb.Checkpoint) bool {
	if checkpoint == nil {
		return false
	}

	// Check new format first
	if checkpoint.SourceType == "mysql" && checkpoint.Position != "" {
		var pos MySQLPosition
		if err := json.Unmarshal([]byte(checkpoint.Position), &pos); err != nil {
			return false
		}
		return pos.File != "" && pos.Pos > 0
	}

	// Fallback to legacy format for backward compatibility
	return checkpoint.MysqlBinlogFile != "" && checkpoint.MysqlBinlogPos > 0
}

// CreateCheckpoint creates a new checkpoint from a MySQL position
func (m *MySQLCheckpointManager) CreateCheckpoint(position interface{}) (*pb.Checkpoint, error) {
	var pos MySQLPosition

	// Handle different input types
	switch v := position.(type) {
	case MySQLPosition:
		pos = v
	case *MySQLPosition:
		if v == nil {
			return nil, fmt.Errorf("nil MySQL position pointer")
		}
		pos = *v
	case string:
		// Try to parse from JSON string
		if err := json.Unmarshal([]byte(v), &pos); err != nil {
			return nil, fmt.Errorf("invalid MySQL position JSON: %w", err)
		}
	default:
		return nil, fmt.Errorf("invalid position type for MySQL: %T", position)
	}

	// Validate position
	if pos.File == "" || pos.Pos == 0 {
		return nil, fmt.Errorf("invalid MySQL position: file='%s', pos=%d", pos.File, pos.Pos)
	}

	// Marshal to JSON
	posJSON, err := json.Marshal(pos)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MySQL position: %w", err)
	}

	// Create checkpoint with both new and legacy fields (for backward compatibility)
	checkpoint := &pb.Checkpoint{
		Position:   string(posJSON),
		SourceType: "mysql",
		Timestamp:  time.Now().Unix(),
		// Also populate legacy fields for backward compatibility
		MysqlBinlogFile: pos.File,
		MysqlBinlogPos:  pos.Pos,
	}

	return checkpoint, nil
}

// ParseCheckpoint parses a checkpoint into a MySQL position
func (m *MySQLCheckpointManager) ParseCheckpoint(checkpoint *pb.Checkpoint) (interface{}, error) {
	if checkpoint == nil {
		return nil, fmt.Errorf("checkpoint is nil")
	}

	// Try new format first
	if checkpoint.Position != "" && checkpoint.SourceType == "mysql" {
		var pos MySQLPosition
		if err := json.Unmarshal([]byte(checkpoint.Position), &pos); err != nil {
			return nil, fmt.Errorf("failed to parse MySQL position from new format: %w", err)
		}
		return pos, nil
	}

	// Fallback to legacy format
	if checkpoint.MysqlBinlogFile != "" && checkpoint.MysqlBinlogPos > 0 {
		return MySQLPosition{
			File: checkpoint.MysqlBinlogFile,
			Pos:  checkpoint.MysqlBinlogPos,
		}, nil
	}

	return nil, fmt.Errorf("checkpoint does not contain valid MySQL position data")
}

// Compare compares two MySQL checkpoints
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func (m *MySQLCheckpointManager) Compare(a, b *pb.Checkpoint) (int, error) {
	posA, err := m.ParseCheckpoint(a)
	if err != nil {
		return 0, fmt.Errorf("failed to parse checkpoint a: %w", err)
	}

	posB, err := m.ParseCheckpoint(b)
	if err != nil {
		return 0, fmt.Errorf("failed to parse checkpoint b: %w", err)
	}

	mysqlPosA, ok := posA.(MySQLPosition)
	if !ok {
		return 0, fmt.Errorf("checkpoint a is not a MySQL position")
	}

	mysqlPosB, ok := posB.(MySQLPosition)
	if !ok {
		return 0, fmt.Errorf("checkpoint b is not a MySQL position")
	}

	// Compare binlog files first
	fileCompare := compareBinlogFiles(mysqlPosA.File, mysqlPosB.File)
	if fileCompare != 0 {
		return fileCompare, nil
	}

	// Files are the same, compare positions
	if mysqlPosA.Pos < mysqlPosB.Pos {
		return -1, nil
	} else if mysqlPosA.Pos > mysqlPosB.Pos {
		return 1, nil
	}
	return 0, nil
}

// compareBinlogFiles compares two MySQL binlog file names
// Expected format: mysql-bin.000001, mysql-bin.000002, etc.
func compareBinlogFiles(fileA, fileB string) int {
	if fileA == fileB {
		return 0
	}

	// Extract the numeric part from file names
	numA := extractBinlogNumber(fileA)
	numB := extractBinlogNumber(fileB)

	if numA < numB {
		return -1
	} else if numA > numB {
		return 1
	}

	// If numeric parts are equal, do string comparison as fallback
	if fileA < fileB {
		return -1
	}
	return 1
}

// extractBinlogNumber extracts the numeric part from a binlog filename
// Example: "mysql-bin.000123" -> 123
func extractBinlogNumber(filename string) int {
	// Find the last dot
	lastDot := strings.LastIndex(filename, ".")
	if lastDot == -1 || lastDot == len(filename)-1 {
		return 0
	}

	// Extract numeric part
	numStr := filename[lastDot+1:]
	var num int
	fmt.Sscanf(numStr, "%d", &num)
	return num
}

// String returns a human-readable representation of a MySQL position
func (p MySQLPosition) String() string {
	return fmt.Sprintf("%s:%d", p.File, p.Pos)
}

// IsZero checks if the position is at zero/initial state
func (p MySQLPosition) IsZero() bool {
	return p.File == "" || p.Pos == 0
}

