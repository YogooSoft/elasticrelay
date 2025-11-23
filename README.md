# ElasticRelay - Multi-Source CDC Gateway to Elasticsearch

## Vision

ElasticRelay is a seamless, heterogeneous data synchronizer designed to provide real-time Change Data Capture (CDC) from major OLTP databases (MySQL, PostgreSQL, MongoDB) to Elasticsearch. It aims to be more user-friendly and reliable than existing solutions like Logstash or Flink.

## Key Features

- **Zero-Code Configuration**: A wizard-style GUI for setting up connections, selecting tables, governing fields, and mapping to Elasticsearch.
- **Multi-Table Dynamic Indexing**: Automatically creates separate Elasticsearch indices for each source table with configurable naming patterns (e.g., `elasticrelay-users`, `elasticrelay-orders`).
- **Built-in Governance**: Handles data structuring, anonymization, type conversion, normalization, and enrichment.
- **Reliability by Default**: Utilizes transaction log-level CDC, precise checkpointing for resuming, and idempotent writes to ensure data integrity.

## Technology Stack

- **Data Plane (Go)**: The core data synchronization logic is built in Go (1.25.2+) for high concurrency, low memory footprint, and simple deployment. Uses advanced MySQL binlog parsing and Elasticsearch bulk APIs.
- **Control Plane & GUI (TypeScript/Next.js)**: A rich, interactive UI for configuration and monitoring (in development).
- **APIs (gRPC)**: Internal communication between components is handled via gRPC for high performance with complete service implementations.
- **Database Support**: MySQL CDC via binlog parsing (go-mysql library)
- **Elasticsearch Integration**: Official Elasticsearch Go client (v8) with bulk indexing support
- **Configuration**: JSON-based configuration with automatic format detection and migration
- **Reliability**: Comprehensive error handling, DLQ system, and checkpoint management

## Architecture

The system is composed of several key components:

- **Source Connectors**: Capture changes from source databases.
- **Durable Buffer**: A persistent buffer for decoupling sources and sinks and enabling replayability.
- **Transform & Governance Engine**: Executes data transformation rules.
- **ES Sink Writer**: Writes data to Elasticsearch in efficient batches.
- **Orchestrator**: Manages the lifecycle of synchronization tasks.
- **Control Plane**: The UI and configuration management backend.

## Quick Start

To quickly get ElasticRelay up and running, follow these three simple steps:

### Step 1: Build
```sh
./scripts/build.sh
```

### Step 2: Configure
Edit the configuration file `./config/parallel_config.json` and ensure the database and Elasticsearch connection information is correct.

### Step 3: Execute
```sh
./start.sh
```

After completing these steps, ElasticRelay will start monitoring database changes and synchronizing them to Elasticsearch.

---

## How to Run

### Prerequisites

- Go (1.25.2+)
- Protobuf Compiler (`protoc`)
- Elasticsearch (7.x or 8.x)
- MySQL (5.7+ or 8.x) with binlog enabled

### Installation

1.  **Install Go dependencies and tools**:
    ```sh
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
    ```

2.  **Install `protoc`**:
    On macOS with Homebrew:
    ```sh
    brew install protobuf
    ```

3.  **Tidy dependencies**:
    ```sh
    go mod tidy
    ```

### Building and Running the Server

#### Quick Build (Development)
```sh
# Simple build without version info
go build -o elasticrelay ./cmd/elasticrelay

# Run the server
./elasticrelay -config multi_config.json
```

#### Production Build (Recommended)
```sh
# Build with version information using Makefile
make build

# Run the versioned binary
./bin/elasticrelay -config multi_config.json
```

#### Version Management
ElasticRelay has comprehensive version management with build-time injection:

```sh
# View current version info with detailed build information
./bin/elasticrelay -version

# Check version info from Makefile
make version

# Development build (fast, no version injection)
make dev

# Production build (optimized with version info)
make release

# Cross-platform builds for multiple architectures
make build-all

# Build with custom version
VERSION="v1.3.0" make build

# Build all tools including migration utilities
make build-tools
```

The version system includes:
- **Git Integration**: Automatic version detection from git tags
- **Build Metadata**: Commit hash, build time, Go version, and platform information
- **Colorized Output**: Rich console output with version details and ASCII art logo
- **Cross-Platform**: Support for Linux, macOS (Intel/ARM), and Windows

The server will start and listen on port `50051` by default.

**Alternative**: You can also run directly without building:
```sh
go run ./cmd/elasticrelay -config multi_config.json
```

### Multi-Table Configuration

ElasticRelay supports both legacy single-config and modern multi-config formats with automatic detection and migration.

#### Modern Multi-Config Format (`multi_config.json`):

```json
{
  "version": "3.0",
  "data_sources": [
    {
      "id": "mysql-main",
      "type": "mysql",
      "host": "localhost",
      "port": 3306,
      "user": "elastic_user",
      "password": "password",
      "database": "elasticrelay",
      "server_id": 100,
      "table_filters": ["users", "orders", "products"]
    }
  ],
  "sinks": [
    {
      "id": "es-main",
      "type": "elasticsearch",
      "addresses": ["http://localhost:9200"],
      "options": {
        "index_prefix": "elasticrelay"
      }
    }
  ],
  "jobs": [],
  "global": {
    "log_level": "info",
    "grpc_port": 50051,
    "dlq_config": {
      "enabled": true,
      "storage_path": "dlq",
      "max_retries": 3,
      "retry_delay": "30s"
    }
  }
}
```

#### Legacy Config Format (`config.json`):

```json
{
  "db_host": "localhost",
  "db_port": 3306,
  "db_user": "elastic_user",
  "db_password": "password",
  "db_name": "elasticrelay",
  "server_id": 100,
  "table_filters": ["users", "orders", "products"],
  "es_addresses": ["http://localhost:9200"]
}
```

The system automatically detects configuration format and supports migration between formats. This creates separate indices:
- `elasticrelay-users` for the `users` table
- `elasticrelay-orders` for the `orders` table  
- `elasticrelay-products` for the `products` table

### Dead Letter Queue (DLQ) Support

ElasticRelay includes a comprehensive DLQ system for handling failed events:

- **Automatic Retry**: Failed events are automatically retried with exponential backoff
- **Persistent Storage**: DLQ items are persisted to disk with full state management
- **Deduplication**: Prevents duplicate events from being added to the queue
- **Status Tracking**: Complete lifecycle tracking (pending, retrying, exhausted, resolved, discarded)
- **Manual Management**: Support for manual item inspection and management
- **Automatic Cleanup**: Resolved items are automatically cleaned up after configurable duration

### Parallel Processing

Advanced parallel snapshot processing capabilities:

- **Chunking Strategies**: Support for ID-based, time-based, and hash-based chunking
- **Worker Pools**: Configurable worker pool sizes with adaptive scheduling
- **Progress Tracking**: Real-time progress monitoring and statistics
- **Large Table Support**: Optimized handling of large tables with intelligent chunking
- **Streaming Mode**: Memory-efficient streaming processing for large datasets

## Current Status

This project is currently in active development (MVP Phase ~95%). The following has been completed:

### ✅ Completed Features
- **Core Data Pipeline**: Full MySQL CDC implementation with binlog-based real-time synchronization
- **Multi-Table Dynamic Indexing**: Automatic per-table Elasticsearch index creation and management with configurable naming
- **gRPC Architecture**: Complete service definitions and implementations (Connector, Orchestrator, Sink, Transform, Health)
- **Advanced Configuration Management**: 
  - Multi-source configuration system with legacy migration support
  - Configuration synchronization and hot-reload capabilities
  - Automatic format detection and migration tools
- **Elasticsearch Integration**: High-performance bulk writing with automatic index management and data cleaning
- **Checkpoint/Resume**: Persistent binlog position tracking for fault tolerance with automatic recovery
- **Data Transformation**: Complete pipeline for data processing and governance
- **Dead Letter Queue (DLQ)**: 
  - Comprehensive DLQ system with exponential backoff retry (configurable max retries)
  - Persistent storage with deduplication and status tracking
  - Automatic cleanup of resolved items
  - Support for manual item management and inspection
- **Parallel Processing**: 
  - Advanced parallel snapshot processing with chunking strategies
  - Configurable worker pools and adaptive scheduling
  - Progress tracking and statistics collection
  - Support for large table optimization
- **Version Management**: Complete version injection system with build-time metadata
- **Robust Error Handling**: Comprehensive error handling with fallback mechanisms

### 🚧 In Progress
- **Frontend Development**: Control Plane GUI (TypeScript/Next.js)
- **Enhanced Monitoring**: Metrics collection and observability dashboards
- **DLQ Management API**: gRPC/REST endpoints for DLQ inspection and manual operations

### 📋 Upcoming
- **Multi-Database Support**: PostgreSQL and MongoDB connectors
- **Advanced GUI Features**: Drag-and-drop field mapping and transformation wizards
- **DLQ Web UI**: Visual interface for managing failed events
- **Advanced Governance**: Rich data transformation rules and field-level governance

---

## 📄 License

ElasticRelay is licensed under the [Apache License 2.0](LICENSE).

```
Copyright 2024 上海悦高软件股份有限公司 (Shanghai Yogoo Software Co., Ltd.)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) for details.

## 📞 Support

- 🌐 Official Website: [www.yogoo.net](https://www.yogoo.net)
- 📧 Email: support@yogoo.net
- 💬 Community: [GitHub Discussions](https://github.com/yogoosoft/ElasticRelay/discussions)
- 🐛 Bug Reports: [GitHub Issues](https://github.com/yogoosoft/ElasticRelay/issues)
- 📖 Documentation: [docs.elasticrelay.io](https://docs.elasticrelay.io)
