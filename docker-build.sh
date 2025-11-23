#!/bin/bash

# ElasticRelay Docker 构建脚本

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认配置
IMAGE_NAME="elasticrelay"
TAG="latest"
REGISTRY=""
PLATFORM="linux/amd64"
PUSH=false
NO_CACHE=false

# 帮助信息
show_help() {
    echo "ElasticRelay Docker 构建脚本"
    echo ""
    echo "用法: $0 [选项]"
    echo ""
    echo "选项:"
    echo "  -n, --name NAME       镜像名称 (默认: elasticrelay)"
    echo "  -t, --tag TAG         镜像标签 (默认: latest)"
    echo "  -r, --registry REG    镜像仓库前缀 (如: docker.io/username/)"
    echo "  -p, --platform PLAT   目标平台 (默认: linux/amd64)"
    echo "      --push            构建后推送到仓库"
    echo "      --no-cache        不使用缓存构建"
    echo "  -h, --help            显示帮助信息"
    echo ""
    echo "示例:"
    echo "  $0                                    # 基本构建"
    echo "  $0 -t v1.2.0                        # 指定标签"
    echo "  $0 -r docker.io/myuser/ -t v1.2.0   # 指定仓库和标签"
    echo "  $0 --push -t v1.2.0                 # 构建并推送"
    echo "  $0 --no-cache                       # 无缓存构建"
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--name)
            IMAGE_NAME="$2"
            shift 2
            ;;
        -t|--tag)
            TAG="$2"
            shift 2
            ;;
        -r|--registry)
            REGISTRY="$2"
            shift 2
            ;;
        -p|--platform)
            PLATFORM="$2"
            shift 2
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --no-cache)
            NO_CACHE=true
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo -e "${RED}未知选项: $1${NC}"
            show_help
            exit 1
            ;;
    esac
done

# 构建完整的镜像名称
FULL_IMAGE_NAME="${REGISTRY}${IMAGE_NAME}:${TAG}"

# 获取版本信息
# 如果用户指定了镜像标签且不是latest，使用标签作为版本号
if [ "$TAG" != "latest" ] && [[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
    VERSION="$TAG"
    echo -e "${YELLOW}注意: 使用镜像标签 ${TAG} 作为应用程序版本号${NC}"
else
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0")
fi
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S_UTC')

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}     ElasticRelay Docker 构建${NC}"
echo -e "${BLUE}======================================${NC}"
echo -e "${YELLOW}镜像名称:${NC} ${FULL_IMAGE_NAME}"
echo -e "${YELLOW}版本信息:${NC} ${VERSION}"
echo -e "${YELLOW}Git提交:${NC} ${GIT_COMMIT}"
echo -e "${YELLOW}构建时间:${NC} ${BUILD_TIME}"
echo -e "${YELLOW}目标平台:${NC} ${PLATFORM}"
echo -e "${YELLOW}使用缓存:${NC} $([ "$NO_CACHE" = true ] && echo "否" || echo "是")"
echo -e "${YELLOW}推送镜像:${NC} $([ "$PUSH" = true ] && echo "是" || echo "否")"
echo ""

# 检查 Docker 是否可用
if ! command -v docker &> /dev/null; then
    echo -e "${RED}错误: Docker 未安装或不可用${NC}"
    exit 1
fi

# 检查是否在项目根目录
if [ ! -f "go.mod" ] || [ ! -f "Dockerfile" ]; then
    echo -e "${RED}错误: 请在项目根目录运行此脚本${NC}"
    exit 1
fi

# 构建 Docker 命令
DOCKER_CMD="docker build"

# 添加平台参数
DOCKER_CMD="${DOCKER_CMD} --platform ${PLATFORM}"

# 添加缓存选项
if [ "$NO_CACHE" = true ]; then
    DOCKER_CMD="${DOCKER_CMD} --no-cache"
fi

# 添加构建参数
DOCKER_CMD="${DOCKER_CMD} --build-arg VERSION='${VERSION}'"
DOCKER_CMD="${DOCKER_CMD} --build-arg GIT_COMMIT='${GIT_COMMIT}'"
DOCKER_CMD="${DOCKER_CMD} --build-arg BUILD_TIME='${BUILD_TIME}'"

# 添加标签
DOCKER_CMD="${DOCKER_CMD} -t ${FULL_IMAGE_NAME}"

# 如果不是 latest 标签，同时添加 latest 标签
if [ "$TAG" != "latest" ]; then
    LATEST_IMAGE_NAME="${REGISTRY}${IMAGE_NAME}:latest"
    DOCKER_CMD="${DOCKER_CMD} -t ${LATEST_IMAGE_NAME}"
fi

# 添加构建上下文
DOCKER_CMD="${DOCKER_CMD} ."

echo -e "${GREEN}开始构建镜像...${NC}"
echo -e "${BLUE}执行命令:${NC} ${DOCKER_CMD}"
echo ""

# 执行构建
if eval $DOCKER_CMD; then
    echo ""
    echo -e "${GREEN}✅ 镜像构建成功!${NC}"
    
    # 显示镜像信息
    echo ""
    echo -e "${YELLOW}镜像信息:${NC}"
    docker images "${REGISTRY}${IMAGE_NAME}" --format "table {{.Repository}}:{{.Tag}}\t{{.Size}}\t{{.CreatedSince}}"
    
    # 推送镜像
    if [ "$PUSH" = true ]; then
        echo ""
        echo -e "${GREEN}开始推送镜像...${NC}"
        
        if docker push "${FULL_IMAGE_NAME}"; then
            echo -e "${GREEN}✅ 镜像推送成功: ${FULL_IMAGE_NAME}${NC}"
            
            # 如果有 latest 标签也推送
            if [ "$TAG" != "latest" ]; then
                echo -e "${GREEN}推送 latest 标签...${NC}"
                docker push "${LATEST_IMAGE_NAME}"
                echo -e "${GREEN}✅ 镜像推送成功: ${LATEST_IMAGE_NAME}${NC}"
            fi
        else
            echo -e "${RED}❌ 镜像推送失败${NC}"
            exit 1
        fi
    fi
    
    echo ""
    echo -e "${GREEN}🎉 构建完成!${NC}"
    echo ""
    echo -e "${YELLOW}运行镜像:${NC}"
    echo "  docker run -p 50051:50051 -v \$(pwd)/config:/app/config ${FULL_IMAGE_NAME}"
    echo ""
    echo -e "${YELLOW}查看日志:${NC}"
    echo "  docker logs \$(docker run -d -p 50051:50051 ${FULL_IMAGE_NAME})"
    
else
    echo ""
    echo -e "${RED}❌ 镜像构建失败${NC}"
    exit 1
fi
