package postgresql

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// PostgreSQLCheckpointManager manages checkpoints for PostgreSQL connections
type PostgreSQLCheckpointManager struct {
	sourceType string
}

// NewPostgreSQLCheckpointManager creates a new PostgreSQL checkpoint manager
func NewPostgreSQLCheckpointManager() *PostgreSQLCheckpointManager {
	return &PostgreSQLCheckpointManager{
		sourceType: "postgresql",
	}
}

// GetSourceType returns the source type identifier
func (p *PostgreSQLCheckpointManager) GetSourceType() string {
	return p.sourceType
}

// IsValid checks if a checkpoint is valid for PostgreSQL
func (p *PostgreSQLCheckpointManager) IsValid(checkpoint *pb.Checkpoint) bool {
	if checkpoint == nil {
		return false
	}

	// Check new format first
	if checkpoint.SourceType == "postgresql" && checkpoint.Position != "" {
		return isValidLSN(checkpoint.Position)
	}

	// Fallback to legacy format for backward compatibility
	if checkpoint.PostgresLsn != "" {
		return isValidLSN(checkpoint.PostgresLsn)
	}

	return false
}

// CreateCheckpoint creates a new checkpoint from a PostgreSQL LSN
func (p *PostgreSQLCheckpointManager) CreateCheckpoint(position interface{}) (*pb.Checkpoint, error) {
	var lsn string

	// Handle different input types
	switch v := position.(type) {
	case string:
		lsn = v
	case *string:
		if v == nil {
			return nil, fmt.Errorf("nil PostgreSQL LSN pointer")
		}
		lsn = *v
	default:
		return nil, fmt.Errorf("invalid position type for PostgreSQL: %T (expected string)", position)
	}

	// Validate LSN format
	if !isValidLSN(lsn) {
		return nil, fmt.Errorf("invalid PostgreSQL LSN format: '%s'", lsn)
	}

	// Create checkpoint with both new and legacy fields (for backward compatibility)
	checkpoint := &pb.Checkpoint{
		Position:   lsn,
		SourceType: "postgresql",
		Timestamp:  time.Now().Unix(),
		// Also populate legacy field for backward compatibility
		PostgresLsn: lsn,
	}

	return checkpoint, nil
}

// ParseCheckpoint parses a checkpoint into a PostgreSQL LSN
func (p *PostgreSQLCheckpointManager) ParseCheckpoint(checkpoint *pb.Checkpoint) (interface{}, error) {
	if checkpoint == nil {
		return nil, fmt.Errorf("checkpoint is nil")
	}

	// Try new format first
	if checkpoint.Position != "" && checkpoint.SourceType == "postgresql" {
		if !isValidLSN(checkpoint.Position) {
			return nil, fmt.Errorf("invalid PostgreSQL LSN in new format: '%s'", checkpoint.Position)
		}
		return checkpoint.Position, nil
	}

	// Fallback to legacy format
	if checkpoint.PostgresLsn != "" {
		if !isValidLSN(checkpoint.PostgresLsn) {
			return nil, fmt.Errorf("invalid PostgreSQL LSN in legacy format: '%s'", checkpoint.PostgresLsn)
		}
		return checkpoint.PostgresLsn, nil
	}

	return nil, fmt.Errorf("checkpoint does not contain valid PostgreSQL LSN data")
}

// Compare compares two PostgreSQL checkpoints based on their LSNs
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func (p *PostgreSQLCheckpointManager) Compare(a, b *pb.Checkpoint) (int, error) {
	lsnA, err := p.ParseCheckpoint(a)
	if err != nil {
		return 0, fmt.Errorf("failed to parse checkpoint a: %w", err)
	}

	lsnB, err := p.ParseCheckpoint(b)
	if err != nil {
		return 0, fmt.Errorf("failed to parse checkpoint b: %w", err)
	}

	lsnStrA, ok := lsnA.(string)
	if !ok {
		return 0, fmt.Errorf("checkpoint a is not a PostgreSQL LSN string")
	}

	lsnStrB, ok := lsnB.(string)
	if !ok {
		return 0, fmt.Errorf("checkpoint b is not a PostgreSQL LSN string")
	}

	return compareLSN(lsnStrA, lsnStrB)
}

// isValidLSN validates a PostgreSQL LSN format
// Valid format: "0/17B4538" or "1/A2B3C4D5"
// Format: segment/offset (both in hexadecimal)
func isValidLSN(lsn string) bool {
	if lsn == "" {
		return false
	}

	// LSN "0/0" is technically valid but represents an uninitialized position
	if lsn == "0/0" {
		return false
	}

	// Check format: should have exactly one '/'
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return false
	}

	// Both parts should be valid hexadecimal numbers
	for _, part := range parts {
		if part == "" {
			return false
		}
		// Try to parse as hex
		if _, err := strconv.ParseUint(part, 16, 64); err != nil {
			return false
		}
	}

	return true
}

// compareLSN compares two PostgreSQL LSN strings
// LSN format: "segment/offset" where both are hexadecimal
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareLSN(lsnA, lsnB string) (int, error) {
	if lsnA == lsnB {
		return 0, nil
	}

	// Parse LSN A
	partsA := strings.Split(lsnA, "/")
	if len(partsA) != 2 {
		return 0, fmt.Errorf("invalid LSN format for A: '%s'", lsnA)
	}
	segmentA, err := strconv.ParseUint(partsA[0], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid segment in LSN A: '%s'", lsnA)
	}
	offsetA, err := strconv.ParseUint(partsA[1], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid offset in LSN A: '%s'", lsnA)
	}

	// Parse LSN B
	partsB := strings.Split(lsnB, "/")
	if len(partsB) != 2 {
		return 0, fmt.Errorf("invalid LSN format for B: '%s'", lsnB)
	}
	segmentB, err := strconv.ParseUint(partsB[0], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid segment in LSN B: '%s'", lsnB)
	}
	offsetB, err := strconv.ParseUint(partsB[1], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid offset in LSN B: '%s'", lsnB)
	}

	// Compare segments first
	if segmentA < segmentB {
		return -1, nil
	} else if segmentA > segmentB {
		return 1, nil
	}

	// Segments are equal, compare offsets
	if offsetA < offsetB {
		return -1, nil
	} else if offsetA > offsetB {
		return 1, nil
	}

	return 0, nil
}

// LSNToUint64 converts an LSN string to a uint64 for easier comparison
// This is useful for direct numerical comparison
func LSNToUint64(lsn string) (uint64, error) {
	parts := strings.Split(lsn, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid LSN format: '%s'", lsn)
	}

	segment, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid segment in LSN: %w", err)
	}

	offset, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid offset in LSN: %w", err)
	}

	// Combine segment and offset into a single uint64
	// segment is the high 32 bits, offset is the low 32 bits
	return (segment << 32) | offset, nil
}

// Uint64ToLSN converts a uint64 back to an LSN string
func Uint64ToLSN(lsnInt uint64) string {
	segment := lsnInt >> 32
	offset := lsnInt & 0xFFFFFFFF
	return fmt.Sprintf("%X/%X", segment, offset)
}

// FormatLSN formats an LSN string to ensure consistent capitalization and format
func FormatLSN(lsn string) (string, error) {
	lsnInt, err := LSNToUint64(lsn)
	if err != nil {
		return "", err
	}
	return Uint64ToLSN(lsnInt), nil
}

