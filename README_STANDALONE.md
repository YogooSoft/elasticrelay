# ElasticRelay 独立部署指南

这个指南介绍如何使用 `docker-compose.elasticrelay.yml` 独立部署 ElasticRelay 服务。

## 适用场景

- 已有外部 MySQL 和 Elasticsearch 服务
- 需要灵活的独立部署
- 开发和测试环境
- 微服务架构中的独立组件

## 快速开始

### 1. 准备配置文件

```bash
# 创建配置目录
mkdir -p config logs dlq

# 复制并修改配置文件
cp examples/config.json config/config.json
# 或者使用并行配置
cp examples/parallel_config.json config/config.json
```

### 2. 配置环境变量

```bash
# 复制环境配置模板
cp .env.elasticrelay .env

# 根据需要修改配置
vim .env
```

### 3. 启动服务

```bash
# 使用默认配置启动
docker-compose -f docker-compose.elasticrelay.yml up -d

# 使用自定义环境文件启动
docker-compose -f docker-compose.elasticrelay.yml --env-file .env.elasticrelay up -d

# 查看日志
docker-compose -f docker-compose.elasticrelay.yml logs -f
```

## 配置选项详解

### 基础配置

| 环境变量 | 描述 | 默认值 |
|---------|------|--------|
| `ELASTICRELAY_IMAGE` | Docker 镜像 | `elasticrelay:v1.2.0` |
| `CONTAINER_NAME` | 容器名称 | `elasticrelay-standalone` |
| `GRPC_PORT` | gRPC 服务端口 | `50051` |
| `METRICS_PORT` | 监控端口 | `8080` |

### 配置文件挂载

有两种方式挂载配置：

#### 方式1：挂载配置目录（推荐）
```bash
CONFIG_PATH=./config
CONFIG_FILE=/app/config/config.json
```

#### 方式2：挂载单个文件
```bash
CONFIG_PATH=./examples/parallel_config.json
CONFIG_FILE=/app/config/config.json
```

### 数据目录

| 环境变量 | 描述 | 默认值 |
|---------|------|--------|
| `LOGS_PATH` | 日志目录 | `./logs` |
| `DLQ_PATH` | 死信队列目录 | `./dlq` |
| `CHECKPOINTS_PATH` | 检查点文件 | `./checkpoints.json` |

## 使用示例

### 示例1：使用默认配置

```bash
# 1. 准备配置
mkdir -p config
cp examples/config.json config/

# 2. 启动服务
docker-compose -f docker-compose.elasticrelay.yml up -d

# 3. 查看状态
docker ps
docker logs elasticrelay-standalone
```

### 示例2：使用并行配置

```bash
# 1. 准备并行配置
mkdir -p config
cp examples/parallel_config.json config/config.json

# 2. 修改 MySQL 和 ES 连接地址
vim config/config.json

# 3. 启动服务
docker-compose -f docker-compose.elasticrelay.yml up -d
```

### 示例3：自定义端口和资源

```bash
# 创建自定义环境文件
cat > .env.custom << EOF
GRPC_PORT=50052
METRICS_PORT=8081
MEMORY_LIMIT=1g
CPU_LIMIT=2.0
CONTAINER_NAME=elasticrelay-custom
EOF

# 使用自定义配置启动
docker-compose -f docker-compose.elasticrelay.yml --env-file .env.custom up -d
```

### 示例4：连接外部网络

```bash
# 如果需要连接到现有的网络
export NETWORK_NAME=existing-network
export NETWORK_EXTERNAL=true

docker-compose -f docker-compose.elasticrelay.yml up -d
```

## 配置文件示例

### MySQL 连接配置
```json
{
  "db_host": "your-mysql-host.example.com",
  "db_port": 3306,
  "db_user": "elasticrelay_user",
  "db_password": "your_password",
  "db_name": "your_database"
}
```

### Elasticsearch 连接配置
```json
{
  "es_addresses": ["http://your-es-host.example.com:9200"],
  "es_user": "elastic",
  "es_password": "your_es_password"
}
```

## 常用操作

### 启动和停止

```bash
# 启动服务
docker-compose -f docker-compose.elasticrelay.yml up -d

# 停止服务
docker-compose -f docker-compose.elasticrelay.yml down

# 重启服务
docker-compose -f docker-compose.elasticrelay.yml restart

# 强制重新创建容器
docker-compose -f docker-compose.elasticrelay.yml up -d --force-recreate
```

### 日志查看

```bash
# 查看实时日志
docker-compose -f docker-compose.elasticrelay.yml logs -f

# 查看最近100行日志
docker-compose -f docker-compose.elasticrelay.yml logs --tail=100

# 只查看错误日志
docker-compose -f docker-compose.elasticrelay.yml logs | grep -i error
```

### 健康检查

```bash
# 检查容器状态
docker-compose -f docker-compose.elasticrelay.yml ps

# 检查健康状态
docker inspect elasticrelay-standalone | grep -A 10 '"Health"'

# 测试 gRPC 连接
docker exec elasticrelay-standalone ps aux | grep elasticrelay
```

## 故障排除

### 常见问题

1. **配置文件找不到**
   ```bash
   # 检查配置文件是否存在
   ls -la config/
   
   # 检查挂载是否正确
   docker exec elasticrelay-standalone ls -la /app/config/
   ```

2. **网络连接问题**
   ```bash
   # 检查网络连接
   docker exec elasticrelay-standalone ping your-mysql-host
   docker exec elasticrelay-standalone curl http://your-es-host:9200
   ```

3. **权限问题**
   ```bash
   # 检查目录权限
   ls -la logs/ dlq/
   
   # 修复权限（如果需要）
   sudo chown -R 1001:1001 logs/ dlq/
   ```

### 调试模式

```bash
# 启用调试模式
export LOG_LEVEL=debug
docker-compose -f docker-compose.elasticrelay.yml up -d

# 进入容器调试
docker exec -it elasticrelay-standalone sh
```

## 监控和维护

### 监控端点

- **健康检查**: 容器内部进程检查
- **指标端口**: `http://localhost:8080` （如果配置了监控）
- **日志文件**: `./logs/` 目录

### 备份重要数据

```bash
# 备份配置文件
tar -czf elasticrelay-config-$(date +%Y%m%d).tar.gz config/

# 备份 DLQ 数据
tar -czf elasticrelay-dlq-$(date +%Y%m%d).tar.gz dlq/

# 备份检查点
cp checkpoints.json checkpoints.json.backup.$(date +%Y%m%d)
```

## 安全建议

1. **网络安全**: 使用防火墙限制访问端口
2. **配置安全**: 不要在配置文件中使用明文密码
3. **更新策略**: 定期更新镜像版本
4. **监控日志**: 监控异常访问和错误

## 支持

如有问题，请参考：
- [完整部署文档](./DOCKER_DEPLOYMENT.md)
- [项目文档](./docs/)
- [GitHub Issues](https://github.com/yogoosoft/elasticrelay/issues)
