# ElasticRelay - 多源 CDC 网关到 Elasticsearch

## 愿景

ElasticRelay 是一个无缝的异构数据同步器，旨在提供从主要 OLTP 数据库（MySQL、PostgreSQL、MongoDB）到 Elasticsearch 的实时变更数据捕获（CDC）。它的目标是比现有的解决方案（如 Logstash 或 Flink）更用户友好和可靠。

## 主要特性

- **零代码配置**：通过向导式 GUI 设置连接、选择表、管理字段以及映射到 Elasticsearch。
- **多表动态索引**：自动为每个源表创建独立的 Elasticsearch 索引，支持可配置的命名模式（如 `elasticrelay-users`、`elasticrelay-orders`）。
- **内置治理**：处理数据结构化、匿名化、类型转换、规范化和丰富化。
- **默认可靠**：利用事务日志级别的 CDC、用于断点续传的精确检查点以及幂等写入，以确保数据完整性。

## 技术栈

- **数据平面 (Go)**：核心数据同步逻辑使用 Go (1.25.2+) 构建，以实现高并发、低内存占用和简单部署。使用先进的 MySQL binlog 解析和 Elasticsearch 批量 API。
- **控制平面 & GUI (TypeScript/Next.js)**：一个丰富的交互式 UI，用于配置和监控（开发中）。
- **API (gRPC)**：组件之间的内部通信通过 gRPC 进行，以实现高性能，具有完整的服务实现。
- **数据库支持**：通过 binlog 解析实现 MySQL CDC（使用 go-mysql 库）
- **Elasticsearch 集成**：官方 Elasticsearch Go 客户端（v8）支持批量索引
- **配置管理**：基于 JSON 的配置，支持自动格式检测和迁移
- **可靠性保证**：全面的错误处理、DLQ 系统和检查点管理

## 架构

该系统由几个关键组件组成：

- **源连接器 (Source Connectors)**：从源数据库捕获变更。
- **持久化缓冲区 (Durable Buffer)**：一个持久化缓冲区，用于解耦源和目标，并支持重放。
- **转换与治理引擎 (Transform & Governance Engine)**：执行数据转换规则。
- **ES 写入器 (ES Sink Writer)**：以高效的批处理方式将数据写入 Elasticsearch。
- **编排器 (Orchestrator)**：管理同步任务的生命周期。
- **控制平面 (Control Plane)**：UI 和配置管理后端。

## 快速运行

要快速启动并运行 ElasticRelay，请按照以下三个简单步骤：

### 第一步：构建
```sh
./scripts/build.sh
```

### 第二步：配置
编辑配置文件 `./config/parallel_config.json`，确保数据库和 Elasticsearch 连接信息正确。

### 第三步：执行
```sh
./start.sh
```

完成这些步骤后，ElasticRelay 将开始监控数据库变更并同步到 Elasticsearch。

---

## 如何运行

### 先决条件

- Go (1.25.2+)
- Protobuf 编译器 (`protoc`)
- Elasticsearch (7.x 或 8.x)
- MySQL (5.7+ 或 8.x) 并启用 binlog

### 安装

1.  **安装 Go 依赖和工具**：
    ```sh
    go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.28
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.2
    ```

2.  **安装 `protoc`**：
    在 macOS 上使用 Homebrew：
    ```sh
    brew install protobuf
    ```

3.  **整理依赖**：
    ```sh
    go mod tidy
    ```

### 构建和运行服务

#### 快速构建（开发）
```sh
# 简单构建，不包含版本信息
go build -o elasticrelay ./cmd/elasticrelay

# 运行服务
./elasticrelay -config multi_config.json
```

#### 生产构建（推荐）
您可以选择以下任一方式：

**方式一：使用 Makefile**
```sh
# 构建带版本信息的程序
make build

# 运行版本化的二进制文件
./bin/elasticrelay -config multi_config.json
```

**方式二：使用构建脚本**
```sh
# 基本使用
./scripts/build.sh

# 自定义版本号
VERSION="v1.2.0" ./scripts/build.sh

# 自定义输出目录和文件名
VERSION="v1.2.0" OUTPUT_DIR="release" BINARY_NAME="elasticrelay-v1.2.0" ./scripts/build.sh
```

#### 版本管理
ElasticRelay 具有全面的版本管理和构建时注入：

```sh
# 查看当前版本信息和详细构建信息
./bin/elasticrelay -version

# 查看 Makefile 中的版本信息
make version

# 开发构建（快速，无版本注入）
make dev

# 生产构建（优化，带版本信息）
make release

# 多架构跨平台构建
make build-all

# 使用自定义版本构建
VERSION="v1.3.0" make build

# 构建所有工具（包括迁移实用程序）
make build-tools
```

版本系统包括：
- **Git 集成**：从 git 标签自动检测版本
- **构建元数据**：提交哈希、构建时间、Go 版本和平台信息
- **彩色输出**：富有的控制台输出，包含版本详情和 ASCII 艺术标志
- **跨平台**：支持 Linux、macOS（Intel/ARM）和 Windows

服务将启动并默认监听 `50051` 端口。

**替代方案**：您也可以直接运行而无需构建：
```sh
go run ./cmd/elasticrelay -config multi_config.json
```

### 多表配置

ElasticRelay 支持传统单配置和现代多配置格式，具有自动检测和迁移功能。

#### 现代多配置格式 (`multi_config.json`)：

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

#### 传统配置格式 (`config.json`)：

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

系统自动检测配置格式并支持格式间迁移。这将创建独立的索引：
- `elasticrelay-users` 对应 `users` 表
- `elasticrelay-orders` 对应 `orders` 表  
- `elasticrelay-products` 对应 `products` 表

### 死信队列 (DLQ) 支持

ElasticRelay 包含全面的 DLQ 系统来处理失败事件：

- **自动重试**：失败事件自动使用指数退避策略重试
- **持久化存储**：DLQ 项目持久化到磁盘，具有完整状态管理
- **去重处理**：防止重复事件添加到队列中
- **状态跟踪**：完整的生命周期跟踪（待处理、重试中、已耗尽、已解决、已丢弃）
- **手动管理**：支持手动项目检查和管理
- **自动清理**：已解决项目在可配置时长后自动清理

### 并行处理

先进的并行快照处理能力：

- **分块策略**：支持基于 ID、时间和哈希的分块
- **工作池**：可配置工作池大小，支持自适应调度
- **进度跟踪**：实时进度监控和统计
- **大表支持**：智能分块优化大表处理
- **流式模式**：大型数据集的内存高效流式处理

## 当前状态

该项目目前处于积极开发阶段（MVP 阶段 ~95%）。已完成以下功能：

### ✅ 已完成功能
- **核心数据管道**：完整的 MySQL CDC 实现，基于 binlog 的实时同步
- **多表动态索引**：自动为每个表创建 Elasticsearch 索引，支持可配置命名
- **gRPC 架构**：完整的服务定义和实现（连接器、编排器、写入器、转换器、健康检查）
- **高级配置管理**：
  - 多源配置系统，支持传统配置迁移
  - 配置同步和热重载功能
  - 自动格式检测和迁移工具
- **Elasticsearch 集成**：高性能批量写入，自动索引管理和数据清理
- **检查点/恢复**：持久化 binlog 位置跟踪，支持故障容错和自动恢复
- **数据转换**：完整的数据处理和治理管道
- **死信队列 (DLQ)**：
  - 全面的 DLQ 系统，支持指数退避重试（可配置最大重试次数）
  - 持久化存储，支持去重和状态跟踪
  - 已解决项目的自动清理
  - 支持手动项目管理和检查
- **并行处理**：
  - 先进的并行快照处理，支持分块策略
  - 可配置工作池和自适应调度
  - 进度跟踪和统计收集
  - 支持大表优化
- **版本管理**：完整的版本注入系统，包含构建时元数据
- **健壮的错误处理**：全面的错误处理和回退机制

### 🚧 进行中
- **前端开发**：控制平面 GUI (TypeScript/Next.js)
- **增强监控**：指标收集和可观测性仪表板
- **DLQ 管理 API**：用于 DLQ 检查和手动操作的 gRPC/REST 端点

### 📋 计划中
- **多数据库支持**：PostgreSQL 和 MongoDB 连接器
- **高级 GUI 功能**：拖拽式字段映射和转换向导
- **DLQ Web UI**：管理失败事件的可视化界面
- **高级治理**：丰富的数据转换规则和字段级治理

---

## 📄 许可证

ElasticRelay 基于 [Apache License 2.0](LICENSE) 许可证。

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

## 🤝 贡献

我们欢迎贡献！请查看我们的 [贡献指南](CONTRIBUTING.md) 了解详情。

## 📞 支持

- 🌐 官方网站: [www.yogoo.net](https://www.yogoo.net)
- 📧 邮箱: support@yogoo.net
- 💬 社区: [GitHub Discussions](https://github.com/yogoosoft/ElasticRelay/discussions)
- 🐛 问题报告: [GitHub Issues](https://github.com/yogoosoft/ElasticRelay/issues)
- 📖 文档: [docs.elasticrelay.com](https://docs.elasticrelay.com)
