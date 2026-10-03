.PHONY: build clean test test-integration test-llm test-e2e test-all lint

# 项目名称
BINARY_NAME=uapclaw
BUILD_DIR=./bin

# Go 编译参数
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOFMT=gofmt
GOLINT=golangci-lint

# 版本信息（可从环境变量覆盖，正式构建时由 CI/CD 注入）
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# ldflags 构建参数，注入版本信息到 version 包
LDFLAGS = -X github.com/uapclaw/uapclaw-go/internal/common/version.Version=$(VERSION) \
          -X github.com/uapclaw/uapclaw-go/internal/common/version.GitCommit=$(GIT_COMMIT) \
          -X github.com/uapclaw/uapclaw-go/internal/common/version.BuildDate=$(BUILD_DATE)

# 构建所有二进制
build:
	@echo "Building $(BINARY_NAME) (version=$(VERSION), commit=$(GIT_COMMIT))..."
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/jiuwenbox ./cmd/jiuwenbox/

# 仅构建主程序
build-cli:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/

# 运行测试
test:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" ./...

# 运行测试（带覆盖率）
test-cover:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# 集成测试（Mock LLM + 真实逻辑，无需外部服务）
test-integration:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test integration" ./tests/integration/...

# LLM 真实调用测试（需要 API Key 环境变量）
test-llm:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test llm" ./...

# E2E 端到端测试
test-e2e:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test e2e" ./tests/e2e/...

# 全量测试（单元 + 集成 + LLM，不含 e2e）
test-all:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test integration llm" ./... ./tests/integration/...

# 集成测试覆盖率
test-integration-cover:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test integration" -coverprofile=integration_coverage.out ./tests/integration/...
	$(GOCMD) tool cover -html=integration_coverage.out -o integration_coverage.html

# 代码格式化
fmt:
	$(GOFMT) -w .

# 代码检查
lint:
	$(GOLINT) run ./...

# 清理构建产物
clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)

# 初始化工作区
init:
	$(BUILD_DIR)/$(BINARY_NAME) init

# 快速聊天模式
chat:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/ && $(BUILD_DIR)/$(BINARY_NAME) chat

# HTTP 服务模式
serve:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/ && $(BUILD_DIR)/$(BINARY_NAME) serve

# 完整模式
app:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/ && $(BUILD_DIR)/$(BINARY_NAME) app
