# ElasticRelay Dockerfile - 多阶段构建优化
# 第一阶段：构建阶段
FROM golang:1.25.2-alpine AS builder

# 设置工作目录
WORKDIR /app

# 安装构建依赖
RUN apk add --no-cache \
    git \
    make \
    ca-certificates \
    tzdata

# 复制 go.mod 和 go.sum 用于依赖缓存
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用程序 - 版本信息通过构建参数传入
ARG VERSION="v0.1.0"
ARG GIT_COMMIT="unknown"
ARG BUILD_TIME="unknown"

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags "-s -w -X 'github.com/yogoosoft/elasticrelay/internal/version.Version=${VERSION}' \
              -X 'github.com/yogoosoft/elasticrelay/internal/version.GitCommit=${GIT_COMMIT}' \
              -X 'github.com/yogoosoft/elasticrelay/internal/version.BuildTime=${BUILD_TIME}'" \
    -a -installsuffix cgo \
    -o elasticrelay \
    ./cmd/elasticrelay

# 第二阶段：运行阶段
FROM alpine:3.18

# 安装运行时依赖
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    curl

# 创建非特权用户
RUN addgroup -g 1001 elasticrelay && \
    adduser -u 1001 -G elasticrelay -s /bin/sh -D elasticrelay

# 设置工作目录
WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/elasticrelay .

# 创建配置和日志目录
RUN mkdir -p /app/config /app/logs /app/dlq && \
    chown -R elasticrelay:elasticrelay /app

# 复制示例配置文件
COPY --chown=elasticrelay:elasticrelay examples/config.json /app/config/config.json.example
COPY --chown=elasticrelay:elasticrelay examples/multi_config.json /app/config/multi_config.json.example

# 设置时区
ENV TZ=Asia/Shanghai

# 暴露端口
EXPOSE 50051

# 切换到非特权用户
USER elasticrelay

# 健康检查
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD curl -f http://localhost:50051/health || exit 1

# 启动命令
ENTRYPOINT ["./elasticrelay"]
CMD ["--config", "/app/config/config.json", "--port", "50051"]
