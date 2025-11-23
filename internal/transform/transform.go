package transform

import (
	"io"
	"log"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
)

// Server implements the TransformService.
type Server struct {
	pb.UnimplementedTransformServiceServer
}

// NewServer creates a new Transform server.
func NewServer() (*Server, error) {
	log.Println("Transform Server created (placeholder)")
	return &Server{}, nil
}

// ApplyRules is a placeholder implementation.
// It currently acts as a simple pass-through, returning the events as it receives them.
func (s *Server) ApplyRules(stream pb.TransformService_ApplyRulesServer) error {
	log.Println("Transform: ApplyRules stream opened.")

	var receivedEvents []*pb.ChangeEvent
	for {
		event, err := stream.Recv()
		if err == io.EOF { // Client has finished sending events
			break
		}
		if err != nil {
			log.Printf("Transform: Error receiving from ApplyRules stream: %v", err)
			return err
		}
		log.Printf("Transform: Processing event for PK %s", event.PrimaryKey)
		receivedEvents = append(receivedEvents, event)
	}

	// Now send all processed events back to the client
	for _, event := range receivedEvents {
		if err := stream.Send(event); err != nil {
			log.Printf("Transform: Error sending to ApplyRules stream: %v", err)
			return err
		}
	}

	log.Println("Transform: ApplyRules stream closed after sending all transformed events.")
	return nil // Signal end of stream to client
}
