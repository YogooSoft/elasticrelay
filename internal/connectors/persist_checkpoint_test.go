package connectors

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

func TestLoadPersistedCheckpointBySourceType(t *testing.T) {
	t.Chdir(t.TempDir())

	token := base64.StdEncoding.EncodeToString([]byte(`{"_data":"t"}`))
	writeJSON(t, CheckpointFilePath("mongodb"), map[string]*pb.Checkpoint{
		"job-mongo": {MongoResumeToken: token},
	})
	writeJSON(t, CheckpointFilePath("mysql"), map[string]*pb.Checkpoint{
		"job-mysql": {MysqlBinlogFile: "bin.000001", MysqlBinlogPos: 4},
	})
	writeJSON(t, CheckpointFilePath("postgresql"), map[string]map[string]string{
		"job-pg": {"job_id": "job-pg", "lsn": "0/16B3748"},
	})

	cp, err := LoadPersistedCheckpoint("job-mongo", "mongodb")
	if err != nil || !IsValidPersistedCheckpoint(cp, "mongodb") {
		t.Fatalf("mongo: %v %+v", err, cp)
	}
	cp, err = LoadPersistedCheckpoint("job-mysql", "mysql")
	if err != nil || !IsValidPersistedCheckpoint(cp, "mysql") {
		t.Fatalf("mysql: %v %+v", err, cp)
	}
	cp, err = LoadPersistedCheckpoint("job-pg", "postgresql")
	if err != nil || cp.PostgresLsn != "0/16B3748" {
		t.Fatalf("postgres: %v %+v", err, cp)
	}
}

func writeJSON(t *testing.T, name string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, b, 0644); err != nil {
		t.Fatal(err)
	}
}