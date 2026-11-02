.PHONY: build build-cli web-build clean test test-integration test-llm test-e2e test-all lint proto-gen release

# 项目名称
BINARY_NAME=uapclaw
BUILD_DIR=./bin

# 前端目录（对齐 Python jiuwenswarm/channels/web/frontend；构建产物经 go:embed 进二进制）
FRONTEND_DIR=internal/swarm/channels/web/frontend

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

# 构建前端产物（对齐 Python `npm run build`，产物经 go:embed 进二进制）
#
# 独立目标而非 build 的依赖：build 保持快速，需要带真前端的二进制时先跑本目标。
# dist/ 只有占位 index.html 时二进制里的前端是空壳（仅提示"前端构建产物未部署"），
# CI 在 go build 之前串本目标，因此发布产物自带前端。
web-build:
	@command -v npm >/dev/null 2>&1 || { echo "缺少 npm，请先安装 Node.js（建议 20+）"; exit 1; }
	cd $(FRONTEND_DIR) && npm ci && npm run build
	@echo "前端构建完成：$(FRONTEND_DIR)/dist"

# 仅构建主程序
build-cli:
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/

# 运行测试
test:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" ./...

# ──────────────────────────── 发布打包 ────────────────────────────
#
# 产出 dist/ 下的两个分发包（解压即用：程序 + resources + vec0 扩展 + 快速开始）：
#   dist/uapclaw-$(VERSION)-linux-amd64.tar.gz
#   dist/uapclaw-$(VERSION)-windows-amd64.zip
#   dist/sha256sums.txt
#
# 前置：Linux 需 gcc（CGO）；Windows 交叉编译需 x86_64-w64-mingw32-gcc（可 WIN_CC 覆盖）；
# 打包 zip 用 python3 -m zipfile（不依赖 zip 命令）。同时执行 web-build，把前端打进二进制。
DIST_DIR := $(CURDIR)/dist
RELEASE_STAGE := $(DIST_DIR)/stage
RELEASE_LDFLAGS := -s -w $(LDFLAGS)
WIN_CC ?= x86_64-w64-mingw32-gcc

release: web-build
	@echo "打包 uapclaw $(VERSION)（linux-amd64 / windows-amd64）..."
	rm -rf $(DIST_DIR)
	mkdir -p $(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/third_party/sqlite-vec/linux-amd64
	mkdir -p $(RELEASE_STAGE)/uapclaw-$(VERSION)-windows-amd64/third_party/sqlite-vec/windows-amd64
	CGO_ENABLED=1 $(GOBUILD) -tags sqlite_fts5 -trimpath -ldflags "$(RELEASE_LDFLAGS)" \
		-o $(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/uapclaw ./cmd/uapclaw/
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=$(WIN_CC) $(GOBUILD) -tags sqlite_fts5 -trimpath -ldflags "$(RELEASE_LDFLAGS)" \
		-o $(RELEASE_STAGE)/uapclaw-$(VERSION)-windows-amd64/uapclaw.exe ./cmd/uapclaw/
	cp -r resources $(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/
	cp -r resources $(RELEASE_STAGE)/uapclaw-$(VERSION)-windows-amd64/
	cp third_party/sqlite-vec/linux-amd64/vec0.so \
		$(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/third_party/sqlite-vec/linux-amd64/
	cp third_party/sqlite-vec/windows-amd64/vec0.dll \
		$(RELEASE_STAGE)/uapclaw-$(VERSION)-windows-amd64/third_party/sqlite-vec/windows-amd64/
	cp packaging/release/README.md packaging/release/start.sh $(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/
	cp packaging/release/README.md packaging/release/start.bat $(RELEASE_STAGE)/uapclaw-$(VERSION)-windows-amd64/
	chmod +x $(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/uapclaw \
		$(RELEASE_STAGE)/uapclaw-$(VERSION)-linux-amd64/start.sh
	tar -C $(RELEASE_STAGE) -czf $(DIST_DIR)/uapclaw-$(VERSION)-linux-amd64.tar.gz uapclaw-$(VERSION)-linux-amd64
	cd $(RELEASE_STAGE) && python3 -m zipfile -c $(DIST_DIR)/uapclaw-$(VERSION)-windows-amd64.zip uapclaw-$(VERSION)-windows-amd64
	cd $(DIST_DIR) && sha256sum uapclaw-$(VERSION)-linux-amd64.tar.gz uapclaw-$(VERSION)-windows-amd64.zip > sha256sums.txt
	rm -rf $(RELEASE_STAGE)
	@ls -lh $(DIST_DIR)

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

# 重新生成扩展插件 gRPC 协议代码（需要 protoc + protoc-gen-go + protoc-gen-go-grpc）
proto-gen:
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		internal/swarm/extensions/plugin/proto/extension.proto

# CI 重触发专用（2026-10-24）：空提交被 paths-ignore 拦截不触发流水线，
# 递增本计数器以携带文件变更重新触发 CI。
ci-retrigger-counter := 4
