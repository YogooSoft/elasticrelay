#!/bin/bash

# ElasticRelay 后端开发启动脚本

set -e

echo "🚀 启动 ElasticRelay 后端开发服务"
echo "======================================"

# 颜色定义
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

cd "$(dirname "$0")/.."

echo -e "${YELLOW}📋 检查 Go 环境...${NC}"
if ! command -v go &> /dev/null; then
    echo "❌ Go not found. Please install Go first."
    exit 1
fi

echo -e "${YELLOW}🔧 构建后端服务...${NC}"
go build -o bin/elasticrelay cmd/elasticrelay/main.go

echo -e "${YELLOW}🎯 启动 gRPC 服务...${NC}"
echo "服务地址: localhost:50051"
echo "日志文件: logs/backend.log"
echo ""
echo "按 Ctrl+C 停止服务"
echo ""

# 创建日志目录
mkdir -p logs

# 启动服务并记录日志
./bin/elasticrelay -config multi_config.json -port 50051 2>&1 | tee logs/backend.log
