package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Constants moved to multi_orchestrator.go to avoid redeclaration

type Job struct {
	ID         string
	TableName  string
	ctx        context.Context
	cancel     context.CancelFunc
	connSvc    pb.ConnectorServiceClient
	sinkSvc    pb.SinkServiceClient
	transSvc   pb.TransformServiceClient // New: Transform service client
	lastCp     *pb.Checkpoint
	cpMutex    sync.RWMutex
	batch      []*pb.ChangeEvent
	batchMutex sync.Mutex
}

// Server implements the OrchestratorService.
type Server struct {
	pb.UnimplementedOrchestratorServiceServer
	jobs        map[string]*Job
	jobsMux     sync.Mutex
	grpcAddress string // Address of the gRPC server where all services are running
}

// NewServer creates a new Orchestrator server.
func NewServer(grpcAddress string) (*Server, error) {
	return &Server{
		jobs:        make(map[string]*Job),
		grpcAddress: grpcAddress,
	}, nil
}

// CreateJob starts a new synchronization job.
func (s *Server) CreateJob(ctx context.Context, req *pb.CreateJobRequest) (*pb.Job, error) {
	jobID := req.Name // Corrected: Use Name field
	if jobID == "" {
		jobID = fmt.Sprintf("job-%d", time.Now().UnixNano())
	}

	s.jobsMux.Lock()
	defer s.jobsMux.Unlock()

	if _, exists := s.jobs[jobID]; exists {
		return nil, fmt.Errorf("job with ID '%s' already exists", jobID)
	}

	jobCtx, cancel := context.WithCancel(context.Background())

	// Connect to Connector service
	connConn, err := grpc.Dial(s.grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to connector service: %w", err)
	}

	// Connect to Sink service
	sinkConn, err := grpc.Dial(s.grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to sink service: %w", err)
	}

	// Connect to Transform service
	transConn, err := grpc.Dial(s.grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to connect to transform service: %w", err)
	}

	job := &Job{
		ID:        jobID,
		TableName: req.Config, // Corrected: Use Config field for table name
		ctx:       jobCtx,
		cancel:    cancel,
		connSvc:   pb.NewConnectorServiceClient(connConn),
		sinkSvc:   pb.NewSinkServiceClient(sinkConn),
		transSvc:  pb.NewTransformServiceClient(transConn),
	}

	s.jobs[jobID] = job

	go job.run()

	log.Printf("Orchestrator: Job '%s' for table '%s' created and started.", job.ID, job.TableName)

	return &pb.Job{
		Id:     jobID,
		Name:   req.Name,
		Status: "CREATED",
		Config: req.Config,
	}, nil
}

// run is the main loop for a synchronization job.
func (j *Job) run() {
	log.Printf("Job '%s': Starting...", j.ID)
	defer log.Printf("Job '%s': Stopped.", j.ID)

	// In a real scenario, you would run snapshot and CDC conditionally.
	// For this example, we'll just start CDC.

	// Start a goroutine to periodically flush the batch
	go j.batchFlusher()

	j.startCDC()
}

func (j *Job) startCDC() {
	log.Printf("Job '%s': Starting CDC stream.", j.ID)

	stream, err := j.connSvc.StartCdc(j.ctx, &pb.StartCdcRequest{JobId: j.ID})
	if err != nil {
		log.Printf("Job '%s': Failed to start CDC: %v", j.ID, err)
		return
	}

	for {
		changeEvent, err := stream.Recv()
		if err != nil {
			if err == io.EOF || j.ctx.Err() != nil {
				log.Printf("Job '%s': CDC stream closed.", j.ID)
				return
			}
			log.Printf("Job '%s': Error receiving from CDC stream: %v", j.ID, err)
			return
		}

		j.addToBatch(changeEvent)
	}
}

func (j *Job) addToBatch(event *pb.ChangeEvent) {
	j.batchMutex.Lock()
	defer j.batchMutex.Unlock()

	j.batch = append(j.batch, event)
	if len(j.batch) >= batchSize {
		j.flushBatch()
	}
}

func (j *Job) batchFlusher() {
	ticker := time.NewTicker(commitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			j.batchMutex.Lock()
			j.flushBatch()
			j.batchMutex.Unlock()
		case <-j.ctx.Done():
			return
		}
	}
}

func (j *Job) flushBatch() {
	if len(j.batch) == 0 {
		return
	}

	log.Printf("Job '%s': Flushing batch of %d events.", j.ID, len(j.batch))

	// --- Step 1: Send to Transform Service ---
	transformStream, err := j.transSvc.ApplyRules(j.ctx)
	if err != nil {
		log.Printf("Job '%s': Failed to open transform ApplyRules stream: %v", j.ID, err)
		return // In a real scenario, handle retry/DLQ
	}

	for _, event := range j.batch {
		if err := transformStream.Send(event); err != nil {
			log.Printf("Job '%s': Failed to send event to transform stream: %v", j.ID, err)
			// Handle error, maybe retry or DLQ
			return
		}
	}
	// Crucial: Signal to the server that the client has finished sending.
	if err := transformStream.CloseSend(); err != nil {
		log.Printf("Job '%s': Failed to close transform stream send: %v", j.ID, err)
		return
	}

	// Close the send stream and receive transformed events
	transformedEvents := make([]*pb.ChangeEvent, 0, len(j.batch))
	for {
		event, err := transformStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Job '%s': Error receiving from transform stream: %v", j.ID, err)
			return
		}
		transformedEvents = append(transformedEvents, event)
	}

	// If no events returned from transform, nothing to sink
	if len(transformedEvents) == 0 {
		log.Printf("Job '%s': No events returned from transform service.", j.ID)
		// Clear the batch and commit checkpoint if needed (no events to commit)
		j.batch = nil
		return
	}

	// --- Step 2: Send transformed events to Sink Service ---
	sinkStream, err := j.sinkSvc.BulkWrite(j.ctx)
	if err != nil {
		log.Printf("Job '%s': Failed to open sink BulkWrite stream: %v", j.ID, err)
		return // In a real scenario, you'd have a retry mechanism
	}

	for _, event := range transformedEvents {
		if err := sinkStream.Send(event); err != nil {
			log.Printf("Job '%s': Failed to send event to BulkWrite stream: %v", j.ID, err)
			// Handle error, maybe retry or DLQ
			return
		}
	}

	_, err = sinkStream.CloseAndRecv()
	if err != nil {
		log.Printf("Job '%s': Failed to close and receive from BulkWrite stream: %v", j.ID, err)
		// Handle error
		return
	}

	// If successful, update and commit checkpoint
	lastEventInBatch := transformedEvents[len(transformedEvents)-1]
	j.updateCheckpoint(lastEventInBatch.Checkpoint)
	j.commitCheckpoint()

	// Clear the batch
	j.batch = nil
}

func (j *Job) updateCheckpoint(cp *pb.Checkpoint) {
	j.cpMutex.Lock()
	defer j.cpMutex.Unlock()
	j.lastCp = cp
}

func (j *Job) commitCheckpoint() {
	j.cpMutex.RLock()
	defer j.cpMutex.RUnlock()

	if j.lastCp == nil {
		return
	}

	_, err := j.connSvc.CommitCheckpoint(j.ctx, &pb.CommitCheckpointRequest{
		JobId:      j.ID,
		Checkpoint: j.lastCp,
	})
	if err != nil {
		log.Printf("Job '%s': Failed to commit checkpoint: %v", j.ID, err)
	}
}

// ListJobs returns all active jobs
func (s *Server) ListJobs(ctx context.Context, req *pb.ListJobsRequest) (*pb.ListJobsResponse, error) {
	s.jobsMux.Lock()
	defer s.jobsMux.Unlock()

	jobs := make([]*pb.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, &pb.Job{
			Id:     job.ID,
			Name:   job.ID,        // Use ID as name, should be stored separately in actual application
			Status: "RUNNING",     // Simplified status, should be dynamically retrieved in actual application
			Config: job.TableName, // Simplified config, should be complete JSON in actual application
		})
	}

	log.Printf("Orchestrator: ListJobs returned %d jobs", len(jobs))

	return &pb.ListJobsResponse{
		Jobs:          jobs,
		NextPageToken: "", // Simplified pagination
	}, nil
}

// GetJob returns a specific job by ID
func (s *Server) GetJob(ctx context.Context, req *pb.GetJobRequest) (*pb.Job, error) {
	s.jobsMux.Lock()
	defer s.jobsMux.Unlock()

	job, exists := s.jobs[req.Id]
	if !exists {
		return nil, fmt.Errorf("job with ID '%s' not found", req.Id)
	}

	log.Printf("Orchestrator: GetJob returned job '%s'", job.ID)

	return &pb.Job{
		Id:     job.ID,
		Name:   job.ID,
		Status: "RUNNING",
		Config: job.TableName,
	}, nil
}

// UpdateJob updates an existing job
func (s *Server) UpdateJob(ctx context.Context, req *pb.UpdateJobRequest) (*pb.Job, error) {
	s.jobsMux.Lock()
	defer s.jobsMux.Unlock()

	job, exists := s.jobs[req.Id]
	if !exists {
		return nil, fmt.Errorf("job with ID '%s' not found", req.Id)
	}

	// Simplified update logic, should parse config and update job in actual application
	job.TableName = req.Config

	log.Printf("Orchestrator: UpdateJob updated job '%s'", job.ID)

	return &pb.Job{
		Id:     job.ID,
		Name:   job.ID,
		Status: "UPDATED",
		Config: job.TableName,
	}, nil
}

// DeleteJob stops and removes a job
func (s *Server) DeleteJob(ctx context.Context, req *pb.DeleteJobRequest) (*pb.DeleteJobResponse, error) {
	s.jobsMux.Lock()
	defer s.jobsMux.Unlock()

	job, exists := s.jobs[req.Id]
	if !exists {
		return &pb.DeleteJobResponse{Success: false}, fmt.Errorf("job with ID '%s' not found", req.Id)
	}

	// Stop job
	job.cancel()
	delete(s.jobs, req.Id)

	log.Printf("Orchestrator: DeleteJob removed job '%s'", job.ID)

	return &pb.DeleteJobResponse{Success: true}, nil
}

// ScaleJob scales a job's concurrency
func (s *Server) ScaleJob(ctx context.Context, req *pb.ScaleJobRequest) (*pb.ScaleJobResponse, error) {
	// Simplified implementation, should adjust job concurrency in actual application
	log.Printf("Orchestrator: ScaleJob for job '%s' to concurrency %d (not implemented)", req.Id, req.Concurrency)

	return &pb.ScaleJobResponse{Success: true}, nil
}

// Ensure Server implements the interface.
var _ pb.OrchestratorServiceServer = (*Server)(nil)
