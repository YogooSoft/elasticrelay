package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/yogoosoft/elasticrelay/api/gateway/v1"
	"github.com/yogoosoft/elasticrelay/internal/config"
	mysql_connector "github.com/yogoosoft/elasticrelay/internal/connectors/mysql"
	"github.com/yogoosoft/elasticrelay/internal/logger"
	orchestrator "github.com/yogoosoft/elasticrelay/internal/orchestrator"
	es_sink "github.com/yogoosoft/elasticrelay/internal/sink/es"
	transform "github.com/yogoosoft/elasticrelay/internal/transform"
	"github.com/yogoosoft/elasticrelay/internal/version"
)

// DummySinkServer is a sink server that immediately fails all operations
// This is used when the real sink (like Elasticsearch) is unavailable during startup
// It allows the application to continue running and route failed events to DLQ
type DummySinkServer struct {
	pb.UnimplementedSinkServiceServer
}

// BulkWrite immediately fails all write operations, triggering DLQ
func (d *DummySinkServer) BulkWrite(stream pb.SinkService_BulkWriteServer) error {
	log.Printf("DummySinkServer: BulkWrite called - immediately failing to trigger DLQ")
	return fmt.Errorf("sink unavailable - triggering DLQ")
}

// DescribeIndex fails all describe operations
func (d *DummySinkServer) DescribeIndex(ctx context.Context, req *pb.DescribeIndexRequest) (*pb.DescribeIndexResponse, error) {
	return nil, fmt.Errorf("sink unavailable")
}

const shutdownGracePeriod = 10 * time.Second

func main() {
	// Command-line flags
	var (
		showVersion = flag.Bool("version", false, "Show version information and exit")
		port        = flag.String("port", "50051", "gRPC service port")
	)
	opts := config.RegisterFlags(flag.CommandLine)
	flag.Parse()

	// Handle version flag
	if *showVersion {
		version.DisplayLogo()
		os.Exit(0)
	}

	// Display startup logo and version information
	version.DisplayLogo()
	fmt.Println("🚀 ElasticRelay starting...")

	warnings, err := opts.Validate(flag.CommandLine)
	if err != nil {
		log.Fatalf("invalid configuration source options: %v", err)
	}
	for _, w := range warnings {
		log.Printf("⚠️  Warning: %s", w)
	}
	log.Printf("Config source: %s", opts.Describe())

	if opts.Source == config.SourceFile {
		// Detect configuration format and load accordingly
		migrationService := config.NewMigrationService(opts.ConfigFile)
		configVersion, err := migrationService.DetectConfigFormat()
		if err != nil {
			log.Fatalf("failed to detect config format: %v", err)
		}
		log.Printf("Detected configuration format version: %s", configVersion)

		if configVersion != "2.0" && configVersion != "3.0" {
			runLegacy(opts.ConfigFile, opts.TransformConfig, *port)
			return
		}
	}

	runMulti(opts, *port)
}

// orchestratorApplier applies runtime configuration changes.
type orchestratorApplier struct {
	orch     *orchestrator.MultiOrchestrator
	services *runtimeServices
}

func (a *orchestratorApplier) ApplyConfiguration(ctx context.Context, newCfg *config.MultiConfig) (*config.ApplyReport, error) {
	return a.orch.ApplyConfiguration(ctx, newCfg)
}

func (a *orchestratorApplier) ApplyTransform(data []byte) error {
	return a.services.applyTransform(data)
}

// runMulti runs the multi-configuration (2.0 / 3.0) mode from any configuration source.
func runMulti(opts *config.Options, port string) {
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	source, err := config.NewSource(opts)
	if err != nil {
		log.Fatalf("failed to create configuration source: %v", err)
	}

	snap, err := source.Load(ctx)
	if err != nil {
		source.Close()
		log.Fatalf("failed to load configuration from %s: %v", source.Describe(), err)
	}

	log.Printf("Loading multi-configuration from %s", snap.Origin)
	multiCfg, err := config.ParseMultiConfig(snap.Main)
	if err != nil {
		log.Fatalf("failed to load multi-configuration: %v", err)
	}
	if opts.ExpandEnvEnabled() {
		if err := config.ExpandSecrets(multiCfg); err != nil {
			log.Fatalf("failed to expand configuration placeholders: %v", err)
		}
	}
	if err := config.ValidateMultiConfig(multiCfg); err != nil {
		if opts.Source == config.SourceNacos {
			log.Fatalf("invalid configuration from %s: %v", snap.Origin, err)
		}
		log.Printf("⚠️  Warning: configuration validation failed: %v (runtime reloads with this error will be rejected)", err)
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create multi-orchestrator for handling multiple sources/sinks
	log.Println("Creating multi-orchestrator for handling multiple configurations...")
	orchServer, err := orchestrator.NewMultiOrchestrator("localhost:" + port)
	if err != nil {
		log.Fatalf("failed to create multi orchestrator server: %v", err)
	}

	// Set global log level from configuration
	if multiCfg.Global.LogLevel != "" {
		logger.SetLogLevel(multiCfg.Global.LogLevel)
		log.Printf("Set log level to: %s", multiCfg.Global.LogLevel)
	}

	// Load multi-configuration
	if err := orchServer.LoadConfiguration(multiCfg); err != nil {
		log.Fatalf("failed to load multi-configuration into orchestrator: %v", err)
	}

	// Create and start jobs from configuration
	if err := orchServer.CreateJobsFromConfig(); err != nil {
		log.Fatalf("failed to create jobs from configuration: %v", err)
	}

	// Log connector types that were created
	for _, ds := range multiCfg.DataSources {
		log.Printf("%s Connector Server created for data source '%s'",
			strings.Title(ds.Type), ds.ID)
	}

	transServer := newTransformServer(snap)

	services := &runtimeServices{
		sink:      &swappableSink{},
		connector: &swappableConnector{},
		transform: transServer,
	}

	// The in-process sink service is backed by the first Elasticsearch sink
	services.sink.set(buildSinkServer(firstESSink(multiCfg)))

	// For legacy compatibility, the connector service is backed by the first data source
	connectorServer, err := buildConnectorServer(firstDataSource(multiCfg))
	if err != nil {
		log.Fatalf("failed to create %s connector server for multi-config: %v", firstDataSource(multiCfg).Type, err)
	}
	services.connector.set(connectorServer)

	manager := config.NewConfigManager(source, &orchestratorApplier{orch: orchServer, services: services}, config.ManagerOptions{
		Debounce:  opts.Debounce,
		ExpandEnv: opts.ExpandEnvEnabled(),
		DryRun:    opts.ApplyMode == config.ApplyModeDryRun,
	})
	manager.SetInitial(multiCfg, snap)
	orchServer.SetInfraHook(services.rebuild)

	// Create and register services on gRPC server
	s := grpc.NewServer()
	pb.RegisterOrchestratorServiceServer(s, orchServer)
	pb.RegisterTransformServiceServer(s, transServer)
	pb.RegisterConnectorServiceServer(s, services.connector)
	pb.RegisterSinkServiceServer(s, services.sink)
	pb.RegisterConfigServiceServer(s, config.NewGRPCService(manager))

	if opts.WatchEnabled() {
		if err := manager.Start(ctx); err != nil {
			log.Fatalf("failed to start configuration hot reload: %v", err)
		}
	}

	go func() {
		<-ctx.Done()
		stopSignals()
		log.Printf("Shutdown signal received, stopping ElasticRelay gracefully (press Ctrl+C again to force)...")
		source.Close()
		orchServer.Shutdown()

		stopped := make(chan struct{})
		go func() {
			s.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(shutdownGracePeriod):
			s.Stop()
		}
	}()

	log.Printf("gRPC server listening on port %s with multi-configuration support", port)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	log.Printf("ElasticRelay stopped")
}

// newTransformServer creates the transform server from the loaded snapshot.
// Unreadable or invalid transform configuration falls back to pass-through mode.
func newTransformServer(snap *config.Snapshot) *transform.Server {
	if snap.HasTransform && len(snap.Transform) > 0 {
		log.Printf("Loading transform configuration from %s", snap.Origin)
		transformConfigs, err := transform.LoadTransformConfigFromBytes(snap.Transform)
		if err == nil {
			transServer, err := transform.NewServer(transform.WithConfigs(transformConfigs))
			if err != nil {
				log.Fatalf("failed to create transform server: %v", err)
			}
			log.Printf("✅ Transform engine loaded with %d rules", len(transformConfigs))
			return transServer
		}
		log.Printf("⚠️  Warning: failed to load transform config: %v", err)
		log.Printf("⚠️  Transform engine will run in pass-through mode")
	} else if snap.HasTransform {
		log.Printf("⚠️  Transform engine will run in pass-through mode")
	}

	transServer, err := transform.NewServer()
	if err != nil {
		log.Fatalf("failed to create transform server: %v", err)
	}
	return transServer
}

// runLegacy runs the legacy single-config (1.0) mode from a local file.
func runLegacy(configFile, transformConfig, port string) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Use legacy single-config format
	log.Printf("Loading legacy configuration from %s", configFile)
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}
	log.Printf("Legacy configuration loaded from %s", configFile)

	// Create service instances using legacy format
	connServer, err := mysql_connector.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to create mysql connector server: %v", err)
	}

	sinkServer, err := es_sink.NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to create es sink server: %v", err)
	}

	orchServer, err := orchestrator.NewServer("localhost:" + port)
	if err != nil {
		log.Fatalf("failed to create orchestrator server: %v", err)
	}

	// Create transform server with optional configuration (legacy mode)
	var transServer *transform.Server
	if transformConfig != "" {
		log.Printf("Loading transform configuration from %s", transformConfig)
		transformConfigs, err := transform.LoadTransformConfig(transformConfig)
		if err != nil {
			log.Printf("⚠️  Warning: failed to load transform config: %v", err)
			log.Printf("⚠️  Transform engine will run in pass-through mode")
			transServer, err = transform.NewServer()
			if err != nil {
				log.Fatalf("failed to create transform server: %v", err)
			}
		} else {
			transServer, err = transform.NewServer(transform.WithConfigs(transformConfigs))
			if err != nil {
				log.Fatalf("failed to create transform server: %v", err)
			}
			log.Printf("✅ Transform engine loaded with %d rules", len(transformConfigs))
		}
	} else {
		transServer, err = transform.NewServer()
		if err != nil {
			log.Fatalf("failed to create transform server: %v", err)
		}
	}

	// Create and register all services on one gRPC server
	s := grpc.NewServer()
	pb.RegisterConnectorServiceServer(s, connServer)
	pb.RegisterSinkServiceServer(s, sinkServer)
	pb.RegisterOrchestratorServiceServer(s, orchServer)
	pb.RegisterTransformServiceServer(s, transServer)

	log.Printf("gRPC server listening on port %s with legacy configuration", port)

	// Auto-start a default CDC job for legacy mode
	go func() {
		// Wait for server to be ready
		time.Sleep(2 * time.Second)

		log.Printf("Auto-starting default CDC job for legacy configuration...")

		// Create a default job for the configured table
		req := &pb.CreateJobRequest{
			Name:   "default-legacy-job",
			Config: "test_table", // Use first table from config or default
		}

		if len(cfg.TableFilters) > 0 {
			req.Config = cfg.TableFilters[0]
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Connect to local orchestrator
		conn, err := grpc.Dial("localhost:"+port, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("Failed to connect to orchestrator for auto-start: %v", err)
			return
		}
		defer conn.Close()

		client := pb.NewOrchestratorServiceClient(conn)
		job, err := client.CreateJob(ctx, req)
		if err != nil {
			log.Printf("Failed to auto-start CDC job: %v", err)
			return
		}

		log.Printf("✅ Auto-started CDC job: %s (ID: %s)", job.Name, job.Id)
	}()

	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
