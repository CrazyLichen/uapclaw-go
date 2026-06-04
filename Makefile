.PHONY: build clean test lint

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

# 构建所有二进制
build:
	@echo "Building $(BINARY_NAME)..."
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/
	$(GOBUILD) -o $(BUILD_DIR)/jiuwenbox ./cmd/jiuwenbox/

# 仅构建主程序
build-cli:
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/

# 运行测试
test:
	$(GOTEST) -v ./...

# 运行测试（带覆盖率）
test-cover:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

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
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/ && $(BUILD_DIR)/$(BINARY_NAME) chat

# HTTP 服务模式
serve:
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/ && $(BUILD_DIR)/$(BINARY_NAME) serve

# 完整模式
app:
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/uapclaw/ && $(BUILD_DIR)/$(BINARY_NAME) app
