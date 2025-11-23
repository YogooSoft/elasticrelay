#!/bin/bash

# 构建脚本 - 支持版本信息注入

set -e

# 默认值
VERSION=${VERSION:-"dev"}
OUTPUT_DIR=${OUTPUT_DIR:-"bin"}
BINARY_NAME=${BINARY_NAME:-"elasticrelay"}

# 获取Git信息
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S_UTC')

# 版本信息注入的包路径
VERSION_PKG="github.com/yogoosoft/elasticrelay/internal/version"

# 构建标志
LDFLAGS=(
    "-X '${VERSION_PKG}.Version=${VERSION}'"
    "-X '${VERSION_PKG}.GitCommit=${GIT_COMMIT}'"
    "-X '${VERSION_PKG}.BuildTime=${BUILD_TIME}'"
)

# 创建输出目录
mkdir -p "${OUTPUT_DIR}"

echo "Building ElasticRelay..."
echo "Version: ${VERSION}"
echo "Git Commit: ${GIT_COMMIT}"
echo "Build Time: ${BUILD_TIME}"
echo ""

# 构建二进制文件
go build \
    -ldflags "${LDFLAGS[*]}" \
    -o "${OUTPUT_DIR}/${BINARY_NAME}" \
    ./cmd/elasticrelay

echo "Build completed: ${OUTPUT_DIR}/${BINARY_NAME}"

# 显示版本信息
echo ""
echo "Version info:"
"${OUTPUT_DIR}/${BINARY_NAME}" --version 2>/dev/null || echo "Binary built successfully"
