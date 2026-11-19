package mongodb

import (
	"encoding/base64"
	"testing"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

func TestMongoDBCheckpointManagerIsValid(t *testing.T) {
	mgr := NewMongoDBCheckpointManager()
	token := base64.StdEncoding.EncodeToString([]byte(`{"_data":"abc"}`))
	if !mgr.IsValid(&pb.Checkpoint{MongoResumeToken: token}) {
		t.Fatal("expected valid token")
	}
	if mgr.IsValid(&pb.Checkpoint{MongoResumeToken: "not-base64"}) {
		t.Fatal("expected invalid token")
	}
}