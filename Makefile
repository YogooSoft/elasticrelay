# ElasticRelay Makefile

# 项目信息
BINARY_NAME = elasticrelay
MIGRATE_BINARY = config-migrate
OUTPUT_DIR = bin
MAIN_PACKAGE = ./cmd/elasticrelay
MIGRATE_PACKAGE = ./cmd/config-migrate

# 版本信息
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0")
GIT_COMMIT = $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME = $(shell date -u '+%Y-%m-%d_%H:%M:%S_UTC')

# Go构建参数
VERSION_PKG = github.com/yogoosoft/elasticrelay/internal/version
LDFLAGS = -X '$(VERSION_PKG).Version=$(VERSION)' \
          -X '$(VERSION_PKG).GitCommit=$(GIT_COMMIT)' \
          -X '$(VERSION_PKG).BuildTime=$(BUILD_TIME)'

# 默认目标
.PHONY: all
all: clean build

# 构建
.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@echo "Version: $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"
	@mkdir -p $(OUTPUT_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Build completed: $(OUTPUT_DIR)/$(BINARY_NAME)"

# 构建配置迁移工具（当前不可用 - 需要实现）
.PHONY: build-migrate
build-migrate:
	@echo "Warning: Migration tool not implemented yet ($(MIGRATE_PACKAGE) not found)"
	@echo "Skipping migration tool build..."

# 构建所有工具
.PHONY: build-tools
build-tools: build
	@echo "Main tool built successfully"

# 开发构建（不注入版本信息）
.PHONY: dev
dev:
	@echo "Building for development..."
	@mkdir -p $(OUTPUT_DIR)
	go build -o $(OUTPUT_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Dev build completed: $(OUTPUT_DIR)/$(BINARY_NAME)"

# 发布构建（优化版本）
.PHONY: release
release:
	@echo "Building for release..."
	@mkdir -p $(OUTPUT_DIR)
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS) -s -w" -a -installsuffix cgo -o $(OUTPUT_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "Release build completed: $(OUTPUT_DIR)/$(BINARY_NAME)"

# 跨平台构建
.PHONY: build-all
build-all: clean
	@echo "Building for multiple platforms..."
	@mkdir -p $(OUTPUT_DIR)
	# 主程序
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PACKAGE)
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PACKAGE)
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PACKAGE)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(OUTPUT_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PACKAGE)
	@echo "Multi-platform build completed"

# 运行
.PHONY: run
run: build
	./$(OUTPUT_DIR)/$(BINARY_NAME)

# 开发运行（不构建）
.PHONY: dev-run
dev-run:
	go run -ldflags "$(LDFLAGS)" $(MAIN_PACKAGE)

# 配置迁移相关目标（当前不可用）
.PHONY: migrate-config
migrate-config:
	@echo "Warning: Config migration tool not implemented yet"
	@echo "Please use the built-in config migration features in the main application"

.PHONY: migrate-config-dry
migrate-config-dry:
	@echo "Warning: Config migration tool not implemented yet"
	@echo "Please use the built-in config migration features in the main application"

.PHONY: migrate-config-force
migrate-config-force:
	@echo "Warning: Config migration tool not implemented yet"
	@echo "Please use the built-in config migration features in the main application"

# 测试
.PHONY: test
test:
	go test -v ./...

# 测试覆盖率
.PHONY: test-cover
test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# 代码检查
.PHONY: lint
lint:
	golangci-lint run

# 格式化代码
.PHONY: fmt
fmt:
	go fmt ./...

# 整理依赖
.PHONY: tidy
tidy:
	go mod tidy

# 清理
.PHONY: clean
clean:
	rm -rf $(OUTPUT_DIR)
	rm -f coverage.out coverage.html

# 显示版本信息
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

# 显示帮助
.PHONY: help
help:
	@echo "ElasticRelay Build Commands:"
	@echo ""
	@echo "Build targets:"
	@echo "  build           - 构建主程序"
	@echo "  build-migrate   - 构建配置迁移工具"
	@echo "  build-tools     - 构建所有工具"
	@echo "  dev             - 开发构建（快速）"
	@echo "  release         - 发布构建（优化）"
	@echo "  build-all       - 跨平台构建"
	@echo ""
	@echo "Run targets:"
	@echo "  run             - 构建并运行主程序"
	@echo "  dev-run         - 开发运行（不构建）"
	@echo ""
	@echo "Config migration:"
	@echo "  migrate-config      - 执行配置迁移"
	@echo "  migrate-config-dry  - 预览配置迁移"
	@echo "  migrate-config-force - 强制配置迁移"
	@echo ""
	@echo "Development:"
	@echo "  test        - 运行测试"
	@echo "  test-cover  - 测试覆盖率"
	@echo "  lint        - 代码检查"
	@echo "  fmt         - 格式化代码"
	@echo "  tidy        - 整理依赖"
	@echo ""
	@echo "Utility:"
	@echo "  clean       - 清理构建文件"
	@echo "  version     - 显示版本信息"
	@echo "  help        - 显示帮助信息"
