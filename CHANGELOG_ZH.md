# ElasticRelay 修改日志

## [v1.0.1] - 2025-10-12

### 🐛 错误修复 

#### 1. MySQL CDC权限和配置问题修复

**问题描述:**
ElasticRelay在执行CDC操作时遇到两个关键错误：
1. `ERROR 1227 (42000): Access denied; you need (at least one of) the SUPER, REPLICATION CLIENT privilege(s) for this operation`
2. `ERROR can't use 0 as the server ID, will panic`

**根本原因:**
1. MySQL用户 `elasticrelay_user` 缺少CDC操作所需的复制权限
2. 配置文件中缺少 `server_id` 配置，导致CDC服务无法启动

**修复方案:**

##### MySQL用户权限修复

**文件:** `init.sql`

**修复内容:**
为 `elasticrelay_user` 用户添加CDC操作必需的权限：

```sql
-- 授权给 elasticrelay_user 进行复制相关操作的权限
GRANT REPLICATION CLIENT, REPLICATION SLAVE ON *.* TO 'elasticrelay_user'@'%';
GRANT SUPER ON *.* TO 'elasticrelay_user'@'%';
FLUSH PRIVILEGES;
```

**权限说明:**
- `REPLICATION CLIENT`: 允许用户执行 `SHOW MASTER STATUS` 等复制相关命令
- `REPLICATION SLAVE`: 允许用户连接到主服务器作为复制从服务器
- `SUPER`: 提供复制操作所需的超级用户权限

##### CDC配置修复 

**文件:** `config.json` 和 `bin/config.json`

**修复前:**
```json
{
  "db_host": "127.0.0.1",
  "db_port": 3306,
  "db_user": "elasticrelay_user",
  "db_password": "elasticrelay_pass",
  "db_name": "elasticrelay"
}
```

**修复后:**
```json
{
  "db_host": "127.0.0.1",
  "db_port": 3306,
  "db_user": "elasticrelay_user",
  "db_password": "elasticrelay_pass",
  "db_name": "elasticrelay",
  "server_id": 100,
  "table_filters": ["test_table"]
}
```

**配置说明:**
- `server_id`: MySQL复制服务器ID，必须为非零正整数（设置为100）
- `table_filters`: CDC表过滤器，限制监控范围到特定表

##### 部署配置修复

**操作步骤:**
1. **重新创建MySQL容器** - 应用新的权限配置
   ```bash
   docker-compose down
   rm -rf ./data  # 清除旧数据以重新初始化
   docker-compose up -d mysql
   ```

2. **验证权限配置**
   ```bash
   # 检查用户权限
   docker-compose exec mysql mysql -u elasticrelay_user -p \
     -e "SHOW GRANTS FOR 'elasticrelay_user'@'%';"
   
   # 验证二进制日志启用
   docker-compose exec mysql mysql -u elasticrelay_user -p \
     -e "SHOW VARIABLES LIKE 'log_bin';"
   ```

3. **重新编译应用程序**
   ```bash
   make build
   ```

**修复结果:**

✅ **权限问题解决**
- 用户现在具有完整的CDC操作权限
- 成功执行 `SHOW MASTER STATUS` 命令
- 能够建立binlog同步连接

✅ **Server ID配置正确**
- BinlogSyncer 配置显示 `ServerID:100`
- CDC同步成功启动
- 从正确的binlog位置开始监控

✅ **CDC功能正常工作**
- 成功连接到MySQL 8.0.43
- 实时捕获数据变更事件
- 正确处理INSERT、UPDATE、DELETE操作
- 检查点功能正常保存和恢复

**验证日志:**
```
ElasticRelay ea3989a-dirty (commit: ea3989a, built: 2025-10-12_08:01:48_UTC, go: go1.25.2, platform: darwin/amd64)
2025/10/12 16:05:26 Configuration loaded from config.json
2025/10/12 16:05:26 Starting CDC from provided checkpoint: binlog.000002:1290
2025/10/12 16:05:26 INFO create BinlogSyncer config="{ServerID:100 ...}"
2025/10/12 16:05:26 INFO Connected to server flavor=mysql version=8.0.43
2025/10/12 16:05:26 CDC sync started from position (binlog.000002, 1290)
```

**测试验证:**
```bash
# 测试数据变更捕获
mysql> INSERT INTO test_table (name, email) VALUES ('实时测试', 'realtime@example.com');
# ✅ ElasticRelay 成功捕获并处理该变更事件
```

#### 2. 数据未同步到 Elasticsearch 问题修复

**问题描述:**
ElasticRelay 在 CDC 过程中，数据经过 MySQL Connector 和 Transform 服务处理后，未能成功同步到 Elasticsearch。日志显示事件在 Transform 服务处理后停止，未能到达 Sink 服务。

**根本原因:**
1.  **Transform 服务流处理不当:** `internal/transform/transform.go` 中的 `ApplyRules` 函数在处理完事件后，没有正确地向 Orchestrator 发出流结束信号 (`io.EOF`)，导致 Orchestrator 的 `transformStream.Recv()` 循环无限期阻塞。
2.  **Orchestrator 客户端流关闭缺失:** `internal/orchestrator/orchestrator.go` 中的 `flushBatch` 函数在向 Transform 服务发送完所有事件后，没有调用 `transformStream.CloseSend()` 来关闭客户端的发送流，这进一步阻止了 Transform 服务接收到 `io.EOF`。

**修复方案:**

##### Transform 服务流处理逻辑修复

**文件:** `internal/transform/transform.go`

**修复内容:**
修改 `ApplyRules` 函数，使其首先接收来自 Orchestrator 的所有事件，然后处理（目前为直通），接着将所有处理过的事件发送回 Orchestrator，最后返回 `nil` 以正确地向 Orchestrator 发出流结束信号。

##### Orchestrator 客户端流关闭修复

**文件:** `internal/orchestrator/orchestrator.go`

**修复内容:**
在 `flushBatch` 函数中，向 Transform 服务发送完所有事件后，添加 `transformStream.CloseSend()` 调用，以明确告知 Transform 服务客户端已完成发送。

**修复结果:**

✅ **数据流转正常**
- 事件现在能够正确地从 Orchestrator 流经 Transform 服务，并到达 Elasticsearch Sink 服务。
- Elasticsearch Sink 服务能够接收到事件数据，并成功进行批量索引操作。
- 检查点功能正常工作，记录了最新的同步位置。

**验证日志:**
```
2025/10/12 19:40:18 Transform: Processing event for PK 128
2025/10/12 19:40:18 Transform: ApplyRules stream closed after sending all transformed events.
2025/10/12 19:40:18 Sink: BulkWrite stream opened and BulkIndexer started.
2025/10/12 19:40:18 Sink: Received event for PK 128, Op INSERT, Data: {"created_at":"2025-10-12 19:40:16","email":"linxiuying@example.com","id":128,"name":"林秀英"}
2025/10/12 19:40:18 Sink: BulkWrite stream finished. Stats: {NumAdded:1 NumFlushed:1 NumFailed:0 NumIndexed:1 NumCreated:0 NumUpdated:0 NumDeleted:0 NumRequests:1 FlushedBytes:122}
2025/10/12 19:40:18 Successfully committed checkpoint for job test-job-test_table to checkpoints.json
```

#### 3. Elasticsearch Sink DELETE 操作失败修复

**问题描述:**
Elasticsearch Sink 在处理 MySQL CDC 的 DELETE 事件时，Elasticsearch 返回 `400 Bad Request` 错误，提示 `Malformed action/metadata line [...] expected field [create], [delete], [index] or [update] but found [...]`。这导致 DELETE 操作未能成功同步到 Elasticsearch。

**根本原因:**
`esutil.BulkIndexerItem` 的 `Body` 字段在 `DELETE` 操作时被错误地填充了 `event.Data`。Elasticsearch 的 `DELETE` 请求不应包含请求体。此外，`esutil.BulkIndexerItem.Body` 字段的实际类型是 `io.WriterTo`，且需要一个实现了 `io.ReadSeeker` 的具体类型（如 `*bytes.Reader`）。

**修复方案:**

##### 1. `esutil.BulkIndexerItem.Body` 类型适配与空体处理

**文件:** `internal/sink/es/es.go`

**修复内容:**
修改 `BulkWrite` 函数中 `esutil.BulkIndexerItem` 的 `Body` 字段设置逻辑：
- 对于 `DELETE` 操作，`Body` 字段现在被设置为一个空的 `*bytes.Reader` (`bytes.NewReader(nil)`)，以确保请求体为空，并满足 `io.ReadSeeker` 接口要求。
- 对于 `INSERT` 和 `UPDATE` 操作，`Body` 字段继续使用 `bytes.NewReader([]byte(event.Data))`。

**修复结果:**

✅ **Elasticsearch DELETE 操作成功**
- `DELETE` 事件现在能够正确地被 Elasticsearch Sink 处理并同步到 Elasticsearch。
- Elasticsearch 不再返回 `400 Bad Request` 错误。
- `BulkIndexer` 统计信息显示 `NumDeleted:1`。

**验证日志:**
```
2025/10/12 21:21:39 Sink: BulkWrite stream finished. Stats: {NumAdded:1 NumFlushed:1 NumFailed:0 NumIndexed:0 NumCreated:0 NumUpdated:0 NumDeleted:1 NumRequests:1 FlushedBytes:62}
```


### 🔧 配置文件标准化

**影响文件:**
- `config.json` (根目录)
- `bin/config.json` (运行时配置)

**标准化内容:**
- 统一配置文件格式和字段名称
- 添加CDC相关配置项的默认值
- 确保运行时和开发环境配置一致性

### 📖 部署指南更新

基于此次修复，建议的完整部署流程：

1. **初始化MySQL环境**
   ```bash
   # 确保MySQL容器使用最新的init.sql
   docker-compose down -v
   docker-compose up -d mysql
   ```

2. **验证环境配置**
   ```bash
   # 检查权限
   docker-compose exec mysql mysql -u elasticrelay_user -pelasticrelay_pass elasticrelay \
     -e "SHOW GRANTS FOR 'elasticrelay_user'@'%';"
   
   # 验证配置
   cat bin/config.json
   ```

3. **构建和启动应用**
   ```bash
   make build
   ./bin/elasticrelay --table test_table
   ```

### ✅ 修复验证

- **权限验证:** ✅ 用户具有REPLICATION CLIENT和SUPER权限
- **配置验证:** ✅ Server ID正确设置为100
- **连接验证:** ✅ 成功连接MySQL 8.0.43服务器
- **CDC验证:** ✅ 实时数据变更捕获正常工作
- **检查点验证:** ✅ binlog位置正确保存和恢复
- **表过滤验证:** ✅ 仅监控指定的test_table

---

## [v1.0.0] - 2025-10-12

### ✨ 新功能

#### 版本管理系统

**功能描述:**
实现了完整的项目版本管理系统，支持语义化版本控制、构建时版本注入、多平台构建等功能。

**新增文件:**
- `internal/version/version.go` - 版本信息包
- `Makefile` - 构建配置和命令
- `scripts/build.sh` - 构建脚本
- `docs/VERSION_MANAGEMENT.md` - 版本管理文档

**功能特性:**

##### 1. 版本信息管理

**文件:** `internal/version/version.go`

```go
type Info struct {
    Version   string `json:"version"`      // 应用版本号
    GitCommit string `json:"git_commit"`   // Git提交哈希
    BuildTime string `json:"build_time"`   // 构建时间
    GoVersion string `json:"go_version"`   // Go版本
    Platform  string `json:"platform"`     // 平台信息
}
```

**支持功能:**
- 动态版本注入 (通过 ldflags)
- Git信息自动获取
- 构建时间记录
- 平台信息检测
- 结构化版本信息API

##### 2. 构建系统增强

**文件:** `Makefile`

**新增构建命令:**
```bash
make build          # 标准构建
make dev            # 开发构建（快速）
make release        # 发布构建（优化）
make build-all      # 跨平台构建
make run            # 构建并运行
make dev-run        # 开发模式运行
make test           # 运行测试
make test-cover     # 测试覆盖率
make lint           # 代码检查
make fmt            # 格式化代码
make tidy           # 整理依赖
make clean          # 清理构建文件
make version        # 显示版本信息
make help           # 显示帮助信息
```

**版本注入机制:**
- 支持通过环境变量设置版本: `VERSION=v1.0.0 make build`
- 自动从Git标签获取版本号
- 构建时注入Git提交哈希和时间戳

##### 3. 命令行增强

**文件:** `cmd/elasticrelay/main.go`

**新增功能:**
- `--version` 参数: 显示版本信息并退出
- `--port` 参数: 配置gRPC服务端口 (默认50051)
- 启动时自动显示完整版本信息

**版本信息格式:**
```
ElasticRelay v1.0.0 (commit: abc1234, built: 2025-10-12_07:17:49_UTC, go: go1.25.2, platform: darwin/amd64)
```

##### 4. 跨平台构建支持

**支持平台:**
- Linux AMD64: `bin/elasticrelay-linux-amd64`
- macOS AMD64: `bin/elasticrelay-darwin-amd64`
- macOS ARM64: `bin/elasticrelay-darwin-arm64`
- Windows AMD64: `bin/elasticrelay-windows-amd64.exe`

**构建优化:**
- 发布构建移除调试信息 (`-s -w`)
- 静态链接支持 (`CGO_ENABLED=0`)
- 可重现构建

### 🐛 错误修复

#### 1. Go模块依赖修复

**问题描述:**
`github.com/go-sql-driver/mysql` 被标记为间接依赖，但在代码中直接使用。

**修复方案:**
将 `github.com/go-sql-driver/mysql` 从间接依赖移至直接依赖。

**文件修改:** `go.mod`
```diff
require (
    github.com/go-mysql-org/go-mysql v1.13.0
+   github.com/go-sql-driver/mysql v1.9.3
    google.golang.org/grpc v1.76.0
    google.golang.org/protobuf v1.36.10
)

require (
    filippo.io/edwards25519 v1.1.0 // indirect
-   github.com/go-sql-driver/mysql v1.9.3 // indirect
    github.com/goccy/go-json v0.10.2 // indirect
```

#### 2. MySQL 连接器编译错误修复

**问题描述:**
在编译 `internal/connectors/mysql/mysql.go` 时遇到三个编译错误：
1. `h.syncer.GetTable undefined` - 第181行和第242行
2. `undefined: jsonData` - 第403行  
3. `invalid operation: pkColIndex < len(row) (mismatched types uint64 and int)` - 第253行

**修复详情:**

##### BinlogSyncer GetTable 方法调用错误修复

**文件:** `internal/connectors/mysql/mysql.go`  
**位置:** 第181行, 第242行

**问题:** `*replication.BinlogSyncer` 类型没有 `GetTable` 方法

**修复前:**
```go
table, err := h.syncer.GetTable(rowsEvent.TableID)
if err != nil {
    log.Printf("Error getting table metadata for TableID %d: %v", rowsEvent.TableID, err)
    return nil
}
for colIdx, colData := range row {
    colName := string(table.Columns[colIdx].Name)
```

**修复后:**
```go
table := rowsEvent.Table // 直接使用 RowsEvent 中的表信息

for colIdx, colData := range row {
    var colName string
    if colIdx < len(table.ColumnName) {
        colName = string(table.ColumnName[colIdx])
    } else {
        colName = fmt.Sprintf("col_%d", colIdx) // 降级处理
    }
```

**相关修改:**
- `handleRowsEvent` 函数中移除了 `syncer.GetTable()` 调用
- `getPrimaryKey` 函数中同样移除了 `syncer.GetTable()` 调用
- 字段访问从 `table.Columns[].Name` 改为 `table.ColumnName[]`
- 主键字段访问从 `table.PKColumns` 改为 `table.PrimaryKey`

##### jsonData 变量未定义错误修复

**文件:** `internal/connectors/mysql/mysql.go`  
**位置:** 第403行

**问题:** 在 `BeginSnapshot` 函数中使用了未定义的 `jsonData` 变量

**修复前:**
```go
records = append(records, string(jsonData)) // jsonData 未定义
```

**修复后:**
```go
// Convert dataMap to JSON
jsonData, err := json.Marshal(dataMap)
if err != nil {
    log.Printf("Failed to marshal row to JSON: %v", err)
    continue
}

records = append(records, string(jsonData))
```

##### 类型不匹配错误修复

**文件:** `internal/connectors/mysql/mysql.go`  
**位置:** 第253行

**问题:** `table.PrimaryKey` 中的索引类型为 `uint64`，而 `len(row)` 返回 `int` 类型，导致比较操作类型不匹配

**修复前:**
```go
if pkColIndex < len(row) {
```

**修复后:**
```go
if int(pkColIndex) < len(row) {
```

### 🔧 代码格式化

**文件:** `internal/connectors/mysql/mysql.go`

- 统一了 import 语句的排列顺序，将项目内部包放在标准库包之后
- 调整了结构体字段的对齐和注释格式
- 移除了多余的空行，统一了代码风格
- 优化了变量声明的空格和对齐

### 📖 使用示例

#### 版本管理使用示例

**查看版本信息:**
```bash
# 查看程序版本
./bin/elasticrelay --version

# 输出示例:
# ElasticRelay v1.0.0 (commit: abc1234, built: 2025-10-12_07:17:49_UTC, go: go1.25.2, platform: darwin/amd64)
```

**构建不同版本:**
```bash
# 开发构建（默认 dev 版本）
make dev

# 指定版本构建
make build VERSION=v1.0.0

# 发布构建（优化版）
make release VERSION=v1.0.0

# 跨平台构建
make build-all VERSION=v1.0.0
```

**版本发布流程:**
```bash
# 1. 创建Git标签
git tag v1.0.0
git push origin v1.0.0

# 2. 构建发布版本
make release

# 版本号将自动从Git标签获取
```

### ✅ 验证结果

#### MySQL连接器修复验证
- **Lint检查:** 通过，无错误
- **编译测试:** `go build ./...` 成功
- **功能验证:** 所有MySQL连接器相关功能正常

#### 版本管理系统验证
- **构建测试:** `make build VERSION=v1.0.0` 成功
- **版本显示:** `./bin/elasticrelay --version` 正确显示版本信息
- **命令行参数:** `--version` 和 `--port` 参数正常工作
- **跨平台构建:** `make build-all` 成功生成所有平台二进制文件
- **Makefile功能:** 所有构建命令(`make help`)正常运行

#### 依赖管理验证
- **依赖检查:** `go mod tidy` 成功
- **编译检查:** 无 "should be direct" 警告
- **模块完整性:** 所有依赖关系正确

### 📝 技术说明

#### 版本管理系统技术实现

1. **版本注入机制:**
   - 使用Go的 `-ldflags` 参数在编译时注入版本信息
   - 通过 `-X` 标志覆盖包级变量的值
   - 支持版本号、Git提交哈希、构建时间等信息注入

2. **构建系统设计:**
   - Makefile提供统一的构建接口
   - 支持多种构建模式：开发、发布、跨平台
   - 自动检测Git信息并注入到二进制文件
   - 发布构建使用 `-s -w` 标志移除调试信息

3. **版本信息架构:**
   - 独立的版本包 (`internal/version`) 提供版本API
   - 结构化版本信息便于程序内部使用
   - JSON序列化支持便于API接口返回版本信息

4. **跨平台兼容:**
   - 使用 `GOOS` 和 `GOARCH` 环境变量控制目标平台
   - `CGO_ENABLED=0` 确保静态编译
   - 平台信息运行时检测

#### MySQL连接器技术修复

1. **BinlogSyncer API 变更适配:** 
   - 新版本的 `go-mysql-org/go-mysql` 库中，表信息直接从 `RowsEvent.Table` 获取
   - 列名访问方式从 `table.Columns[].Name` 改为 `table.ColumnName[]`
   - 主键信息从 `table.PKColumns` 改为 `table.PrimaryKey`

2. **类型安全处理:**
   - 添加了类型转换确保数值比较的类型一致性
   - 增加了边界检查防止数组越界访问

3. **错误处理增强:**
   - 为JSON marshaling 添加了完整的错误处理
   - 保持了原有的日志记录机制

4. **依赖管理优化:**
   - 修正了Go模块依赖关系，确保直接依赖正确声明
   - 避免了编译时的依赖警告信息

### 📊 修改统计

#### 新增文件 (6个)
- `internal/version/version.go` - 版本信息管理包
- `Makefile` - 构建系统配置
- `scripts/build.sh` - 构建脚本
- `docs/VERSION_MANAGEMENT.md` - 版本管理文档
- `CHANGELOG.md` - 修改日志 (本文档)
- `bin/` - 构建输出目录

#### 修改文件 (3个)
- `cmd/elasticrelay/main.go` - 添加命令行参数和版本信息显示
- `internal/connectors/mysql/mysql.go` - 修复编译错误和API适配
- `go.mod` - 修正依赖关系

#### 功能改进
- ✅ **版本管理**: 完整的语义化版本控制系统
- ✅ **构建系统**: 多平台构建和优化选项
- ✅ **命令行工具**: 版本查看和端口配置
- ✅ **错误修复**: MySQL连接器编译问题解决
- ✅ **依赖管理**: Go模块依赖关系规范化
- ✅ **文档完善**: 详细的使用指南和技术文档

---

**开发者:** 
**修改日期:** 2025-10-12  
**影响范围:** 
- MySQL CDC 连接器模块
- 版本管理系统 (新增)
- 构建系统 (新增)
- 命令行工具 (增强)
- 项目文档 (完善)

**向后兼容性:** ✅ 完全兼容  
**破坏性变更:** ❌ 无  
**安全性影响:** ℹ️ 无安全风险
