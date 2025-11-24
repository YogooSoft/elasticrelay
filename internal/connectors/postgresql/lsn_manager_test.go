package postgresql

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewLSNManager(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "test_checkpoints.json")
	
	if lsnMgr == nil {
		t.Fatal("LSNManager is nil")
	}
	
	if lsnMgr.checkpointFile != "test_checkpoints.json" {
		t.Errorf("Expected checkpoint file 'test_checkpoints.json', got '%s'", lsnMgr.checkpointFile)
	}
}

func TestCompareLSN(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "")
	
	tests := []struct {
		lsn1     string
		lsn2     string
		expected int
		hasError bool
	}{
		{"0/1000", "0/2000", -1, false},
		{"0/2000", "0/1000", 1, false},
		{"0/1000", "0/1000", 0, false},
		{"1/0", "0/FFFFFFFF", 1, false},
		{"invalid", "0/1000", 0, true},
		{"0/1000", "invalid", 0, true},
	}
	
	for _, tt := range tests {
		t.Run(tt.lsn1+"_vs_"+tt.lsn2, func(t *testing.T) {
			result, err := lsnMgr.CompareLSN(tt.lsn1, tt.lsn2)
			
			if tt.hasError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			
			if result != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestAdvanceLSN(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "")
	
	tests := []struct {
		lsn      string
		bytes    int64
		expected string
		hasError bool
	}{
		{"0/1000", 1000, "0/1FA0", false},
		{"0/FFFF", 1, "0/10000", false},
		{"FFFFFFFF/FFFFFFFF", 1, "100000000/0", false},
		{"invalid", 1000, "", true},
	}
	
	for _, tt := range tests {
		t.Run(tt.lsn, func(t *testing.T) {
			result, err := lsnMgr.AdvanceLSN(tt.lsn, tt.bytes)
			
			if tt.hasError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			
			if !strings.EqualFold(result, tt.expected) {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestValidateLSN(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "")
	
	tests := []struct {
		lsn      string
		hasError bool
	}{
		{"0/1000", false},
		{"FFFFFFFF/FFFFFFFF", false},
		{"12345678/ABCDEF12", false},
		{"invalid", true},
		{"0/100", true},     // too short
		{"0/1000000", true}, // too long
		{"G/1000", true},    // invalid hex
		{"0", true},         // missing slash
	}
	
	for _, tt := range tests {
		t.Run(tt.lsn, func(t *testing.T) {
			err := lsnMgr.ValidateLSN(tt.lsn)
			
			if tt.hasError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestLSNManagerGetSetLastCommittedLSN(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "")
	
	// Initially should be empty
	if lsn := lsnMgr.GetLastCommittedLSN(); lsn != "" {
		t.Errorf("Expected empty LSN initially, got '%s'", lsn)
	}
	
	// Set and get
	testLSN := "0/1000"
	lsnMgr.SetLastCommittedLSN(testLSN)
	
	if lsn := lsnMgr.GetLastCommittedLSN(); lsn != testLSN {
		t.Errorf("Expected '%s', got '%s'", testLSN, lsn)
	}
}

func TestLSNManagerGetSetLastReceivedLSN(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "")
	
	// Initially should be empty
	if lsn := lsnMgr.GetLastReceivedLSN(); lsn != "" {
		t.Errorf("Expected empty LSN initially, got '%s'", lsn)
	}
	
	// Set and get
	testLSN := "0/2000"
	lsnMgr.SetLastReceivedLSN(testLSN)
	
	if lsn := lsnMgr.GetLastReceivedLSN(); lsn != testLSN {
		t.Errorf("Expected '%s', got '%s'", testLSN, lsn)
	}
}

func TestSaveLoadCheckpoint(t *testing.T) {
	// Create temporary file
	tmpFile := "test_checkpoint_" + time.Now().Format("20060102150405") + ".json"
	defer os.Remove(tmpFile)
	
	lsnMgr := NewLSNManager(nil, tmpFile)
	
	// Create test checkpoint
	checkpoint := &LSNCheckpoint{
		JobID:       "test_job_1",
		LSN:         "0/1000",
		Timeline:    1,
		SlotName:    "test_slot",
		Publication: "test_pub",
		LastUpdate:  time.Now(),
	}
	
	// Save checkpoint
	err := lsnMgr.SaveCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("Failed to save checkpoint: %v", err)
	}
	
	// Load checkpoint
	loaded, err := lsnMgr.LoadCheckpoint("test_job_1")
	if err != nil {
		t.Fatalf("Failed to load checkpoint: %v", err)
	}
	
	if loaded.JobID != checkpoint.JobID {
		t.Errorf("Expected JobID '%s', got '%s'", checkpoint.JobID, loaded.JobID)
	}
	
	if loaded.LSN != checkpoint.LSN {
		t.Errorf("Expected LSN '%s', got '%s'", checkpoint.LSN, loaded.LSN)
	}
	
	if loaded.SlotName != checkpoint.SlotName {
		t.Errorf("Expected SlotName '%s', got '%s'", checkpoint.SlotName, loaded.SlotName)
	}
	
	// Try to load non-existent checkpoint
	_, err = lsnMgr.LoadCheckpoint("non_existent")
	if err == nil {
		t.Error("Expected error for non-existent checkpoint")
	}
}

func TestDeleteCheckpoint(t *testing.T) {
	// Create temporary file
	tmpFile := "test_checkpoint_delete_" + time.Now().Format("20060102150405") + ".json"
	defer os.Remove(tmpFile)
	
	lsnMgr := NewLSNManager(nil, tmpFile)
	
	// Create and save test checkpoint
	checkpoint := &LSNCheckpoint{
		JobID: "test_job_delete",
		LSN:   "0/1000",
	}
	
	err := lsnMgr.SaveCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("Failed to save checkpoint: %v", err)
	}
	
	// Verify it exists
	_, err = lsnMgr.LoadCheckpoint("test_job_delete")
	if err != nil {
		t.Fatalf("Checkpoint should exist: %v", err)
	}
	
	// Delete checkpoint
	err = lsnMgr.DeleteCheckpoint("test_job_delete")
	if err != nil {
		t.Fatalf("Failed to delete checkpoint: %v", err)
	}
	
	// Verify it's gone
	_, err = lsnMgr.LoadCheckpoint("test_job_delete")
	if err == nil {
		t.Error("Checkpoint should be deleted")
	}
}

func TestListCheckpoints(t *testing.T) {
	// Create temporary file
	tmpFile := "test_checkpoint_list_" + time.Now().Format("20060102150405") + ".json"
	defer os.Remove(tmpFile)
	
	lsnMgr := NewLSNManager(nil, tmpFile)
	
	// Initially should be empty
	checkpoints, err := lsnMgr.ListCheckpoints()
	if err != nil {
		t.Fatalf("Failed to list checkpoints: %v", err)
	}
	
	if len(checkpoints) != 0 {
		t.Errorf("Expected 0 checkpoints initially, got %d", len(checkpoints))
	}
	
	// Add some checkpoints
	checkpoint1 := &LSNCheckpoint{JobID: "job1", LSN: "0/1000"}
	checkpoint2 := &LSNCheckpoint{JobID: "job2", LSN: "0/2000"}
	
	err = lsnMgr.SaveCheckpoint(checkpoint1)
	if err != nil {
		t.Fatalf("Failed to save checkpoint1: %v", err)
	}
	
	err = lsnMgr.SaveCheckpoint(checkpoint2)
	if err != nil {
		t.Fatalf("Failed to save checkpoint2: %v", err)
	}
	
	// List again
	checkpoints, err = lsnMgr.ListCheckpoints()
	if err != nil {
		t.Fatalf("Failed to list checkpoints: %v", err)
	}
	
	if len(checkpoints) != 2 {
		t.Errorf("Expected 2 checkpoints, got %d", len(checkpoints))
	}
	
	if _, exists := checkpoints["job1"]; !exists {
		t.Error("job1 checkpoint should exist")
	}
	
	if _, exists := checkpoints["job2"]; !exists {
		t.Error("job2 checkpoint should exist")
	}
}

func TestNewCheckpointManager(t *testing.T) {
	lsnMgr := NewLSNManager(nil, "test.json")
	slotMgr := NewReplicationSlotManager(nil)
	
	checkpointMgr := NewCheckpointManager(lsnMgr, slotMgr)
	
	if checkpointMgr == nil {
		t.Fatal("CheckpointManager is nil")
	}
	
	if checkpointMgr.lsnManager != lsnMgr {
		t.Error("LSN manager not set correctly")
	}
	
	if checkpointMgr.slotManager != slotMgr {
		t.Error("Slot manager not set correctly")
	}
	
	if checkpointMgr.commitFrequency != 30*time.Second {
		t.Errorf("Expected default commit frequency 30s, got %v", checkpointMgr.commitFrequency)
	}
	
	if checkpointMgr.batchSize != 1000 {
		t.Errorf("Expected default batch size 1000, got %d", checkpointMgr.batchSize)
	}
}
