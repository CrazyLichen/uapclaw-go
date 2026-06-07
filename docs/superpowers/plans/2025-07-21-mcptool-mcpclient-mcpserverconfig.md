# 3.5-3.7 MCPTool + McpClient + McpServerConfig 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域三的 MCP 协议工具子系统：McpServerConfig(3.7) → McpClient 接口与五种客户端(3.6) → MCPTool(3.5)

**Architecture:** 自定义 McpClient 接口包装 mcp-go SDK，解耦上层与第三方库。MCPTool 实现 Tool 接口，通过 McpClient.CallTool 调用远程工具。五种客户端（SSE/Stdio/StreamableHTTP/OpenAPI/Playwright）各自实现 McpClient 接口，内部组合 mcp-go client.Client（前四种）或 kin-openapi + net/http（OpenAPI）。包结构镜像 Python：tool/mcp/base.go + tool/mcp/client/ 子包。

**Tech Stack:** Go 1.23, github.com/mark3labs/mcp-go (MCP 协议客户端), github.com/getkin/kin-openapi (OpenAPI 规格解析), github.com/rs/zerolog (日志)

---

## 文件结构

```
internal/agentcore/foundation/tool/mcp/
├── doc.go                            # 包文档
├── base.go                           # McpServerConfig + McpToolCard + MCPTool + ExtractMCPToolResultContent + 常量/选项
├── client.go                         # McpClient 接口 + ConnectOptions + 工厂函数 NewMcpClient
├── client_test.go                    # 工厂函数测试 + fakeMcpClient
└── base_test.go                      # 上述类型的单元测试

internal/agentcore/foundation/tool/mcp/client/
├── doc.go                            # 子包文档
├── sse_client.go                     # SseClient 实现
├── sse_client_test.go                # SSE 客户端测试（//go:build llm）
├── stdio_client.go                   # StdioClient 实现
├── stdio_client_test.go              # Stdio 客户端测试（//go:build llm）
├── streamable_http_client.go         # StreamableHttpClient 实现
├── streamable_http_client_test.go    # StreamableHTTP 测试（//go:build llm）
├── openapi_client.go                 # OpenApiClient 实现（基于 kin-openapi）
├── openapi_client_test.go            # OpenAPI 客户端测试
├── playwright_client.go              # PlaywrightClient 实现（SSE/stdio 双传输）
└── playwright_client_test.go         # Playwright 客户端测试（//go:build llm）

修改文件：
├── internal/common/exception/codes_tool.go   # 新增 3 个错误码
├── internal/agentcore/foundation/tool/doc.go # 更新文件目录
└── IMPLEMENTATION_PLAN.md                     # 更新 3.5/3.6/3.7 状态
```

---

### Task 1: 添加依赖和新增错误码

**Files:**
- Modify: `go.mod`
- Modify: `internal/common/exception/codes_tool.go`

- [ ] **Step 1: 添加 mcp-go 和 kin-openapi 依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/mark3labs/mcp-go@latest && go get github.com/getkin/kin-openapi@latest
```

- [ ] **Step 2: 在 codes_tool.go 的 MCP 段(182300-182399) 新增错误码**

在 `StatusToolMcpExecutionError` 之后添加：

```go
// StatusToolMcpClientTypeUnknown MCP 客户端类型未知
StatusToolMcpClientTypeUnknown = NewStatusCode(
    "TOOL_MCP_CLIENT_TYPE_UNKNOWN", 182302,
    "mcp client type is unknown, client_type={client_type}")
// StatusToolMcpNotConnected MCP 客户端未连接
StatusToolMcpNotConnected = NewStatusCode(
    "TOOL_MCP_NOT_CONNECTED", 182303,
    "mcp client is not connected, server_name={server_name}")
```

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/common/exception/...
```

- [ ] **Step 4: 提交**

```bash
git add go.mod go.sum internal/common/exception/codes_tool.go && git commit -m "feat(tool): 添加 mcp-go/kin-openapi 依赖及 MCP 错误码 182302-182303"
```

---

### Task 2: 创建 mcp 包目录和 doc.go

**Files:**
- Create: `internal/agentcore/foundation/tool/mcp/doc.go`

- [ ] **Step 1: 创建 mcp 目录**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/agentcore/foundation/tool/mcp
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package mcp 提供 MCP（Model Context Protocol）协议工具的实现，
// 包括 MCPTool 工具类、McpToolCard 配置卡片和 McpServerConfig 服务器配置。
//
// MCPTool 是 Tool 接口的 MCP 协议实现，通过 McpClient 调用远程 MCP 服务器上的工具。
// MCPTool 不支持流式调用（Stream 返回 ErrStreamNotSupported）。
// Invoke 流程：参数格式化 → McpClient.CallTool → 提取紧凑结果 → 返回。
//
// 文件目录：
//
//	mcp/
//	├── doc.go           # 包文档
//	├── base.go          # McpServerConfig + McpToolCard + MCPTool + ExtractMCPToolResultContent
//	└── base_test.go     # 单元测试
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/mcp/
package mcp
```

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/doc.go && git commit -m "feat(tool): 创建 mcp 包目录和 doc.go"
```

---

### Task 3: 实现 McpServerConfig（3.7）

**Files:**
- Create: `internal/agentcore/foundation/tool/mcp/base.go`（McpServerConfig 部分）
- Create: `internal/agentcore/foundation/tool/mcp/base_test.go`（McpServerConfig 部分）

- [ ] **Step 1: 编写 McpServerConfig 测试**

在 `base_test.go` 中：

```go
package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMcpServerConfig_默认值(t *testing.T) {
	config := NewMcpServerConfig("test-server", "http://localhost:8080/sse", "sse")
	assert.Equal(t, "test-server", config.ServerName)
	assert.Equal(t, "http://localhost:8080/sse", config.ServerPath)
	assert.Equal(t, "sse", config.ClientType)
	assert.NotEmpty(t, config.ServerID) // UUID 自动生成
	assert.Nil(t, config.Params)
	assert.Nil(t, config.AuthHeaders)
	assert.Nil(t, config.AuthQueryParams)
}

func TestNewMcpServerConfig_ClientType默认为SSE(t *testing.T) {
	config := NewMcpServerConfig("test", "http://localhost:8080/sse", "")
	assert.Equal(t, "sse", config.ClientType)
}

func TestWithServerID(t *testing.T) {
	config := NewMcpServerConfig("test", "http://localhost:8080/sse", "sse",
		WithServerID("my-id"),
	)
	assert.Equal(t, "my-id", config.ServerID)
}

func TestWithParams(t *testing.T) {
	params := map[string]any{"command": "npx", "args": []any{"@playwright/mcp"}}
	config := NewMcpServerConfig("test", "npx", "stdio",
		WithParams(params),
	)
	assert.Equal(t, params, config.Params)
}

func TestWithAuthHeaders(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer xxx"}
	config := NewMcpServerConfig("test", "http://localhost:8080/sse", "sse",
		WithAuthHeaders(headers),
	)
	assert.Equal(t, headers, config.AuthHeaders)
}

func TestWithAuthQueryParams(t *testing.T) {
	params := map[string]string{"token": "abc"}
	config := NewMcpServerConfig("test", "http://localhost:8080/sse", "sse",
		WithAuthQueryParams(params),
	)
	assert.Equal(t, params, config.AuthQueryParams)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestNewMcpServerConfig|TestWithServerID|TestWithParams|TestWithAuthHeaders|TestWithAuthQueryParams" -v
```

- [ ] **Step 3: 在 base.go 中实现 McpServerConfig**

```go
package mcp

import (
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpServerConfig MCP 服务器配置。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/base.py (McpServerConfig)
type McpServerConfig struct {
	// ServerID 服务器唯一标识，默认自动生成 UUID
	ServerID string
	// ServerName 服务器名称
	ServerName string
	// ServerPath 服务器地址或命令路径
	//   SSE/StreamableHTTP: URL（如 "http://localhost:8080/sse"）
	//   Stdio: 命令路径（如 "npx"）
	//   OpenAPI: OpenAPI 文件路径（逗号分隔多个）
	ServerPath string
	// ClientType 客户端类型：sse / stdio / streamable-http / openapi / playwright
	// 默认 "sse"
	ClientType string
	// Params 传输层参数（Stdio 的 command/args/env/cwd 等）
	Params map[string]any
	// AuthHeaders 认证请求头（预留，3.11 回填动态认证）
	AuthHeaders map[string]string
	// AuthQueryParams 认证查询参数（预留，3.11 回填动态认证）
	AuthQueryParams map[string]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// McpServerConfigOption 配置选项函数。
type McpServerConfigOption func(*McpServerConfig)

// WithServerID 设置服务器标识。
func WithServerID(id string) McpServerConfigOption {
	return func(c *McpServerConfig) { c.ServerID = id }
}

// WithParams 设置传输层参数。
func WithParams(params map[string]any) McpServerConfigOption {
	return func(c *McpServerConfig) { c.Params = params }
}

// WithAuthHeaders 设置认证请求头。
func WithAuthHeaders(headers map[string]string) McpServerConfigOption {
	return func(c *McpServerConfig) { c.AuthHeaders = headers }
}

// WithAuthQueryParams 设置认证查询参数。
func WithAuthQueryParams(params map[string]string) McpServerConfigOption {
	return func(c *McpServerConfig) { c.AuthQueryParams = params }
}

// NewMcpServerConfig 创建 MCP 服务器配置。
//
// 对应 Python: McpServerConfig(server_name=..., server_path=..., client_type=...)
func NewMcpServerConfig(name, serverPath, clientType string, opts ...McpServerConfigOption) *McpServerConfig {
	if clientType == "" {
		clientType = "sse"
	}
	c := &McpServerConfig{
		ServerID:   uuid.New().String(),
		ServerName: name,
		ServerPath: serverPath,
		ClientType: clientType,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}
```

注意：此文件后续 Task 会继续追加 McpToolCard、MCPTool、ExtractMCPToolResultContent 等内容。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestNewMcpServerConfig|TestWithServerID|TestWithParams|TestWithAuthHeaders|TestWithAuthQueryParams" -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/base.go internal/agentcore/foundation/tool/mcp/base_test.go && git commit -m "feat(tool): 实现 McpServerConfig (3.7)"
```

---

### Task 4: 实现 McpToolCard（3.5 部分）

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/base.go`
- Modify: `internal/agentcore/foundation/tool/mcp/base_test.go`

- [ ] **Step 1: 追加 McpToolCard 测试**

在 `base_test.go` 末尾追加：

```go
func TestNewMcpToolCard_基本构造(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true, Description: "搜索关键词"},
	}
	card := NewMcpToolCard("web_search", "搜索网页", "search-server", params)
	assert.Equal(t, "web_search", card.Name)
	assert.Equal(t, "搜索网页", card.Description)
	assert.Equal(t, "search-server", card.ServerName)
	assert.Equal(t, "", card.ServerID)
	assert.Equal(t, params, card.InputParams)
}

func TestMcpToolCard_McpToolInfo(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true, Description: "搜索关键词"},
	}
	card := NewMcpToolCard("web_search", "搜索网页", "search-server", params)
	info := card.McpToolInfo()
	assert.Equal(t, "web_search", info.Name)
	assert.Equal(t, "搜索网页", info.Description)
	assert.Equal(t, "search-server", info.ServerName)
	assert.Equal(t, "function", info.Type)
	assert.NotNil(t, info.Parameters)
}

func TestNewMcpToolCard_WithServerID(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "server", nil,
		WithMcpToolCardServerID("my-server-id"),
	)
	assert.Equal(t, "my-server-id", card.ServerID)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestNewMcpToolCard|TestMcpToolCard_McpToolInfo" -v
```

- [ ] **Step 3: 在 base.go 中追加 McpToolCard**

在 `McpServerConfig` 结构体之后追加（结构体区块内）：

```go
// McpToolCard MCP 工具配置卡片，扩展 ToolCard 增加服务器标识。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/base.py (McpToolCard)
type McpToolCard struct {
	tool.ToolCard
	// ServerName MCP 服务器名称
	ServerName string
	// ServerID MCP 服务器标识
	ServerID string
}
```

在导出函数区块追加：

```go
// McpToolCardOption MCP 工具卡片选项函数。
type McpToolCardOption func(*McpToolCard)

// WithMcpToolCardServerID 设置 MCP 工具卡片的服务器标识。
func WithMcpToolCardServerID(id string) McpToolCardOption {
	return func(c *McpToolCard) { c.ServerID = id }
}

// NewMcpToolCard 创建 MCP 工具卡片。
//
// 对应 Python: McpToolCard(name=..., server_name=..., description=..., input_params=...)
func NewMcpToolCard(name, description, serverName string, inputParams []*schema.Param, opts ...McpToolCardOption) *McpToolCard {
	card := &McpToolCard{
		ToolCard:   *tool.NewToolCard(name, description, inputParams, nil),
		ServerName: serverName,
	}
	for _, opt := range opts {
		opt(card)
	}
	return card
}

// McpToolInfo 返回 MCP 工具描述信息，供 LLM function calling 消费。
//
// 对应 Python: McpToolCard.tool_info() -> McpToolInfo
func (c *McpToolCard) McpToolInfo() *schema.McpToolInfo {
	parameters := schema.ToJSONSchemaMap(c.InputParams)
	return schema.NewMcpToolInfo(c.Name, c.Description, c.ServerName, parameters)
}
```

同时在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"` 和 `"github.com/uapclaw/uapclaw-go/internal/common/schema"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestNewMcpToolCard|TestMcpToolCard_McpToolInfo" -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/base.go internal/agentcore/foundation/tool/mcp/base_test.go && git commit -m "feat(tool): 实现 McpToolCard (3.5 部分)"
```

---

### Task 5: 实现 ExtractMCPToolResultContent

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/base.go`
- Modify: `internal/agentcore/foundation/tool/mcp/base_test.go`

- [ ] **Step 1: 追加 ExtractMCPToolResultContent 测试**

在 `base_test.go` 末尾追加：

```go
func TestExtractMCPToolResultContent_文本内容(t *testing.T) {
	// 模拟 mcp.CallToolResult 的 content 为 TextContent 列表
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello world"},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Equal(t, "hello world", got)
}

func TestExtractMCPToolResultContent_图片内容(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "image", "mimeType": "image/png", "data": "iVBORw0KGgo="},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Contains(t, got, "[image content: image/png")
	assert.Contains(t, got, "base64 chars]")
}

func TestExtractMCPToolResultContent_非图片Data(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "resource", "data": "raw-data-here"},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Equal(t, "raw-data-here", got)
}

func TestExtractMCPToolResultContent_空Content(t *testing.T) {
	result := map[string]any{"content": []any{}}
	got := ExtractMCPToolResultContent(result)
	assert.Nil(t, got)
}

func TestExtractMCPToolResultContent_无Content字段(t *testing.T) {
	result := map[string]any{}
	got := ExtractMCPToolResultContent(result)
	assert.Nil(t, got)
}

func TestExtractMCPToolResultContent_取最后一个Content(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "first"},
			map[string]any{"type": "text", "text": "last"},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Equal(t, "last", got)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestExtractMCPToolResultContent" -v
```

- [ ] **Step 3: 在 base.go 导出函数区块追加 ExtractMCPToolResultContent**

```go
// NoTimeout 不设超时，与 Python NO_TIMEOUT = -1 对齐。
const NoTimeout = -1

// ExtractMCPToolResultContent 从 MCP CallToolResult 提取紧凑结果值。
//
// 解析逻辑（符合 MCP 协议）：
//   - 提取 content 最后一个元素
//   - 如果有 text 字段 → 返回 text 字符串
//   - 如果有 data 字段 + mimeType 以 image/ 开头 → 返回 "[image content: {mime}, {len} base64 chars]"
//   - 如果有 data 字段 → 返回 data
//   - 其他 → 返回字符串化结果
//
// 对应 Python: extract_mcp_tool_result_content()
func ExtractMCPToolResultContent(toolResult any) any {
	resultMap, ok := toolResult.(map[string]any)
	if !ok {
		return nil
	}
	content, ok := resultMap["content"]
	if !ok {
		return nil
	}
	contentList, ok := content.([]any)
	if !ok || len(contentList) == 0 {
		return nil
	}

	item, ok := contentList[len(contentList)-1].(map[string]any)
	if !ok {
		return fmt.Sprintf("%v", contentList[len(contentList)-1])
	}

	// 优先检查 text 字段
	if text, ok := item["text"]; ok {
		if s, ok := text.(string); ok {
			return s
		}
	}

	// 检查 data 字段
	if data, ok := item["data"]; ok {
		// 检查是否为图片
		mimeType, _ := item["mimeType"].(string)
		if !ok {
			mimeType, _ = item["mime_type"].(string)
		}
		if strings.HasPrefix(mimeType, "image/") {
			return fmt.Sprintf("[image content: %s, %d base64 chars]", mimeType, len(fmt.Sprintf("%v", data)))
		}
		return data
	}

	// 其他情况，尝试字符串化
	return fmt.Sprintf("%v", item)
}
```

同时在 import 中添加 `"fmt"` 和 `"strings"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestExtractMCPToolResultContent" -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/base.go internal/agentcore/foundation/tool/mcp/base_test.go && git commit -m "feat(tool): 实现 ExtractMCPToolResultContent"
```

---

### Task 6: 创建 client 子包和 McpClient 接口（3.6）

**Files:**
- Create: `internal/agentcore/foundation/tool/mcp/client/doc.go`
- Create: `internal/agentcore/foundation/tool/mcp/client/mcp_client.go`

- [ ] **Step 1: 创建 client 目录**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/agentcore/foundation/tool/mcp/client
```

- [ ] **Step 2: 创建 client/doc.go**

```go
// Package client 提供 MCP 客户端的接口定义和各传输协议的实现。
//
// McpClient 接口定义了与 MCP 服务器交互的标准方法：
// Connect/Disconnect/ListTools/CallTool/GetToolInfo/ListResources/ReadResource/Close。
//
// 支持的客户端类型：
//   - SseClient — SSE (Server-Sent Events) 传输
//   - StdioClient — Stdio 子进程传输
//   - StreamableHttpClient — Streamable HTTP 传输
//   - OpenApiClient — OpenAPI 规格解析客户端
//   - PlaywrightClient — Playwright 浏览器工具客户端（SSE/stdio 双传输）
//
// 使用 NewMcpClient 工厂函数根据 McpServerConfig.ClientType 创建对应客户端。
//
// 文件目录：
//
//	client/
//	├── doc.go                        # 子包文档
//	├── mcp_client.go                 # McpClient 接口 + ConnectOptions + 工厂函数
//	├── sse_client.go                 # SseClient 实现
//	├── stdio_client.go               # StdioClient 实现
//	├── streamable_http_client.go     # StreamableHttpClient 实现
//	├── openapi_client.go             # OpenApiClient 实现
//	└── playwright_client.go          # PlaywrightClient 实现
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/mcp/client/
package client
```

- [ ] **Step 3: 创建 mcp_client.go — McpClient 接口 + ConnectOptions + 工厂函数**

```go
package client

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConnectOptions 连接选项。
type ConnectOptions struct {
	// RetryTimes 重试次数
	RetryTimes int
	// Timeout 超时时间（秒），NoTimeout 表示不限
	Timeout float64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ConnectOption 连接选项函数。
type ConnectOption func(*ConnectOptions)

// WithRetryTimes 设置重试次数。
func WithRetryTimes(n int) ConnectOption {
	return func(o *ConnectOptions) { o.RetryTimes = n }
}

// WithConnectTimeout 设置连接超时时间（秒）。
func WithConnectTimeout(d float64) ConnectOption {
	return func(o *ConnectOptions) { o.Timeout = d }
}

// NewConnectOptions 从选项列表构造 ConnectOptions。
func NewConnectOptions(opts ...ConnectOption) *ConnectOptions {
	o := &ConnectOptions{
		Timeout: mcp.NoTimeout,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// McpClient MCP 客户端接口，定义与 MCP 服务器交互的标准方法。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/mcp_client.py (McpClient)
type McpClient interface {
	// Connect 建立 MCP 服务器连接
	Connect(ctx context.Context, opts ...ConnectOption) error
	// Disconnect 断开 MCP 服务器连接
	Disconnect(ctx context.Context) error
	// ListTools 列出服务器提供的工具
	ListTools(ctx context.Context) ([]*mcp.McpToolCard, error)
	// CallTool 调用指定工具
	CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error)
	// GetToolInfo 获取指定工具信息
	GetToolInfo(ctx context.Context, toolName string) (*mcp.McpToolCard, error)
	// ListResources 列出服务器提供的资源
	ListResources(ctx context.Context) ([]any, error)
	// ReadResource 读取指定资源
	ReadResource(ctx context.Context, uri string) (any, error)
	// Close 关闭客户端（等价于 Disconnect）
	Close() error
}

// NewMcpClient 根据配置创建对应类型的 MCP 客户端。
//
// 支持 clientType: sse / stdio / streamable-http / streamable_http / openapi / playwright
// 未知类型返回 StatusToolMcpClientTypeUnknown 错误。
//
// 对应 Python: 各客户端的构造逻辑
func NewMcpClient(config *mcp.McpServerConfig) (McpClient, error) {
	if config == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpClientTypeUnknown,
			exception.WithParam("client_type", "nil config"),
		)
	}

	switch config.ClientType {
	case "sse":
		return NewSseClient(config), nil
	case "stdio":
		return NewStdioClient(config), nil
	case "streamable-http", "streamable_http":
		return NewStreamableHttpClient(config), nil
	case "openapi":
		return NewOpenApiClient(config), nil
	case "playwright":
		return NewPlaywrightClient(config), nil
	default:
		logger.Error(logger.ComponentAgentCore).
			Str("client_type", config.ClientType).
			Msg("未知的 MCP 客户端类型")
		return nil, exception.BuildError(
			exception.StatusToolMcpClientTypeUnknown,
			exception.WithParam("client_type", config.ClientType),
		)
	}
}
```

- [ ] **Step 4: 创建各客户端的占位实现（确保编译通过）**

创建 `sse_client.go`：

```go
package client

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
)

// SseClient SSE (Server-Sent Events) 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/sse_client.py (SseClient)
type SseClient struct {
	config     *mcp.McpServerConfig
	serverName string
	isConnected bool
}

// NewSseClient 创建 SSE 客户端。
func NewSseClient(config *mcp.McpServerConfig) *SseClient {
	return &SseClient{
		config:     config,
		serverName: config.ServerName,
	}
}

func (c *SseClient) Connect(ctx context.Context, opts ...ConnectOption) error {
	return fmt.Errorf("TODO: SseClient.Connect")
}

func (c *SseClient) Disconnect(ctx context.Context) error {
	return fmt.Errorf("TODO: SseClient.Disconnect")
}

func (c *SseClient) ListTools(ctx context.Context) ([]*mcp.McpToolCard, error) {
	return nil, fmt.Errorf("TODO: SseClient.ListTools")
}

func (c *SseClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	return nil, fmt.Errorf("TODO: SseClient.CallTool")
}

func (c *SseClient) GetToolInfo(ctx context.Context, toolName string) (*mcp.McpToolCard, error) {
	return nil, fmt.Errorf("TODO: SseClient.GetToolInfo")
}

func (c *SseClient) ListResources(ctx context.Context) ([]any, error) {
	return nil, fmt.Errorf("TODO: SseClient.ListResources")
}

func (c *SseClient) ReadResource(ctx context.Context, uri string) (any, error) {
	return nil, fmt.Errorf("TODO: SseClient.ReadResource")
}

func (c *SseClient) Close() error {
	return c.Disconnect(context.Background())
}
```

类似地创建 `stdio_client.go`、`streamable_http_client.go`、`openapi_client.go`、`playwright_client.go`，每个文件结构相同，替换 `SseClient` 为对应类型名。

- [ ] **Step 5: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/... && go build ./internal/agentcore/foundation/tool/mcp/client/...
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/ && git commit -m "feat(tool): 创建 McpClient 接口和五种客户端占位实现 (3.6)"
```

---

### Task 7: 实现 SseClient

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/sse_client.go`
- Create: `internal/agentcore/foundation/tool/mcp/client/sse_client_test.go`

- [ ] **Step 1: 实现 SseClient**

替换 `sse_client.go` 的占位实现，内部组合 mcp-go 的 `*client.Client`：

```go
package client

import (
	"context"
	"fmt"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SseClient SSE (Server-Sent Events) 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/sse_client.py (SseClient)
type SseClient struct {
	config      *mcp.McpServerConfig
	serverName  string
	mcpClient   *mcpclient.Client
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSseClient 创建 SSE 客户端。
func NewSseClient(config *mcp.McpServerConfig) *SseClient {
	return &SseClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// Connect 建立 SSE 连接。
func (c *SseClient) Connect(ctx context.Context, opts ...ConnectOption) error {
	if c.isConnected {
		return nil
	}

	connOpts := NewConnectOptions(opts...)
	timeout := connOpts.Timeout
	if timeout == mcp.NoTimeout {
		timeout = 60.0
	}

	// 构建 SSE 传输选项
	var transportOpts []mcptransport.ClientOption
	if len(c.config.AuthHeaders) > 0 {
		transportOpts = append(transportOpts, mcptransport.WithHeaders(c.config.AuthHeaders))
	}

	// 创建 SSE 客户端
	client, err := mcpclient.NewSSEMCPClient(c.config.ServerPath, transportOpts...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("server_path", c.config.ServerPath).
			Err(err).
			Msg("SSE 连接失败")
		return err
	}

	// 启动连接
	if err := client.Start(ctx); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("server_path", c.config.ServerPath).
			Err(err).
			Msg("SSE 启动失败")
		return err
	}

	// 初始化会话
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2025-03-26",
			ClientInfo: mcp.Implementation{
				Name:    "uapclaw-go",
				Version: "1.0.0",
			},
		},
	}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("server_path", c.config.ServerPath).
			Err(err).
			Msg("SSE 初始化失败")
		_ = client.Close()
		return err
	}

	c.mcpClient = client
	c.isConnected = true
	logger.Info(logger.ComponentAgentCore).
		Str("server_path", c.config.ServerPath).
		Msg("SSE 客户端连接成功")
	return nil
}

// Disconnect 断开 SSE 连接。
func (c *SseClient) Disconnect(_ context.Context) error {
	if !c.isConnected || c.mcpClient == nil {
		return nil
	}
	if err := c.mcpClient.Close(); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("SSE 断开连接失败")
		return err
	}
	c.mcpClient = nil
	c.isConnected = false
	logger.Info(logger.ComponentAgentCore).Msg("SSE 客户端断开连接成功")
	return nil
}

// ListTools 列出 SSE 服务器提供的工具。
func (c *SseClient) ListTools(ctx context.Context) ([]*mcp.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	resp, err := c.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("从 SSE 服务器获取工具列表失败")
		return nil, err
	}
	tools := make([]*mcp.McpToolCard, 0, len(resp.Tools))
	for _, tool := range resp.Tools {
		// 将 mcp-go 的 tool.InputSchema 转为 []*schema.Param（此处简化为直接使用 inputSchema）
		card := mcp.NewMcpToolCard(tool.Name, tool.Description, c.serverName, nil)
		tools = append(tools, card)
	}
	logger.Info(logger.ComponentAgentCore).
		Int("tool_count", len(tools)).
		Msg("从 SSE 服务器获取工具列表")
	return tools, nil
}

// CallTool 通过 SSE 调用工具。
func (c *SseClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	logger.Info(logger.ComponentAgentCore).
		Str("tool_name", toolName).
		Msg("通过 SSE 调用工具")

	resp, err := c.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(err).
			Msg("SSE 工具调用失败")
		return nil, err
	}

	// 将 mcp.CallToolResult 转为 map[string]any 供 ExtractMCPToolResultContent 使用
	result := callToolResultToMap(resp)
	logger.Info(logger.ComponentAgentCore).
		Str("tool_name", toolName).
		Msg("SSE 工具调用完成")
	return result, nil
}

// GetToolInfo 获取 SSE 服务器上指定工具的信息。
func (c *SseClient) GetToolInfo(ctx context.Context, toolName string) (*mcp.McpToolCard, error) {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tools {
		if t.Name == toolName {
			logger.Debug(logger.ComponentAgentCore).
				Str("tool_name", toolName).
				Msg("在 SSE 服务器上找到工具")
			return t, nil
		}
	}
	logger.Warn(logger.ComponentAgentCore).
		Str("tool_name", toolName).
		Msg("在 SSE 服务器上未找到工具")
	return nil, nil
}

// ListResources 列出 SSE 服务器提供的资源。
func (c *SseClient) ListResources(ctx context.Context) ([]any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	resp, err := c.mcpClient.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("从 SSE 服务器获取资源列表失败")
		return nil, err
	}
	result := make([]any, len(resp.Resources))
	for i, r := range resp.Resources {
		result[i] = r
	}
	return result, nil
}

// ReadResource 读取 SSE 服务器上的资源。
func (c *SseClient) ReadResource(ctx context.Context, uri string) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	resp, err := c.mcpClient.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: uri},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("uri", uri).
			Err(err).
			Msg("从 SSE 服务器读取资源失败")
		return nil, err
	}
	return resp.Contents, nil
}

// Close 关闭 SSE 客户端。
func (c *SseClient) Close() error {
	return c.Disconnect(context.Background())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callToolResultToMap 将 mcp.CallToolResult 转为 map[string]any 供 ExtractMCPToolResultContent 使用。
func callToolResultToMap(resp *mcp.CallToolResult) map[string]any {
	content := make([]any, 0, len(resp.Content))
	for _, c := range resp.Content {
		// mcp-go 的 Content 是 TextContent / ImageContent / EmbeddedResource 等
		// 统一转为 map[string]any
		content = append(content, contentToMap(c))
	}
	return map[string]any{"content": content}
}

// contentToMap 将 mcp.Content 转为 map[string]any。
func contentToMap(c mcp.Content) map[string]any {
	result := map[string]any{"type": string(c.Type)}
	switch v := c.(type) {
	case mcp.TextContent:
		result["text"] = v.Text
	case mcp.ImageContent:
		result["data"] = v.Data
		result["mimeType"] = v.MIMEType
	case mcp.ResourceLink:
		result["uri"] = v.URI
		result["name"] = v.Name
	}
	return result
}
```

- [ ] **Step 2: 创建 sse_client_test.go（//go:build llm）**

```go
//go:build llm

package client

import (
	"context"
	"testing"
)

// TestSseClient_真实调用 测试 SSE 客户端真实连接
// 运行方式: go test -tags=llm ./internal/agentcore/foundation/tool/mcp/client/... -run TestSseClient -v
func TestSseClient_真实调用(t *testing.T) {
	// 需要真实 MCP SSE 服务器
	t.Skip("需要真实 MCP SSE 服务器，手动测试时移除 Skip")
}
```

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/client/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/sse_client.go internal/agentcore/foundation/tool/mcp/client/sse_client_test.go && git commit -m "feat(tool): 实现 SseClient (3.6)"
```

---

### Task 8: 实现 StdioClient

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/stdio_client.go`
- Create: `internal/agentcore/foundation/tool/mcp/client/stdio_client_test.go`

- [ ] **Step 1: 实现 StdioClient**

```go
package client

import (
	"context"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StdioClient Stdio 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/stdio_client.py (StdioClient)
type StdioClient struct {
	config      *mcp.McpServerConfig
	serverName  string
	mcpClient   *mcpclient.Client
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStdioClient 创建 Stdio 客户端。
func NewStdioClient(config *mcp.McpServerConfig) *StdioClient {
	return &StdioClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// Connect 建立 Stdio 连接。
func (c *StdioClient) Connect(ctx context.Context, opts ...ConnectOption) error {
	if c.isConnected {
		return nil
	}

	// 从 Params 中提取 command/args/env
	command, _ := c.config.Params["command"].(string)
	if command == "" {
		command = c.config.ServerPath
	}

	var args []string
	if rawArgs, ok := c.config.Params["args"]; ok {
		if argSlice, ok := rawArgs.([]any); ok {
			for _, a := range argSlice {
				if s, ok := a.(string); ok {
					args = append(args, s)
				}
			}
		}
	}

	var env []string
	if rawEnv, ok := c.config.Params["env"]; ok {
		if envMap, ok := rawEnv.(map[string]any); ok {
			for k, v := range envMap {
				if s, ok := v.(string); ok {
					env = append(env, k+"="+s)
				}
			}
		}
	}

	// 创建 Stdio 客户端
	client, err := mcpclient.NewStdioMCPClient(command, env, args...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Stdio 连接失败")
		return err
	}

	// 启动连接
	if err := client.Start(ctx); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Stdio 启动失败")
		return err
	}

	// 初始化会话
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2025-03-26",
			ClientInfo: mcp.Implementation{
				Name:    "uapclaw-go",
				Version: "1.0.0",
			},
		},
	}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Stdio 初始化失败")
		_ = client.Close()
		return err
	}

	c.mcpClient = client
	c.isConnected = true
	logger.Info(logger.ComponentAgentCore).Msg("Stdio 客户端连接成功")
	return nil
}

// Disconnect 断开 Stdio 连接。
func (c *StdioClient) Disconnect(_ context.Context) error {
	if !c.isConnected || c.mcpClient == nil {
		return nil
	}
	if err := c.mcpClient.Close(); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("Stdio 断开连接失败")
		return err
	}
	c.mcpClient = nil
	c.isConnected = false
	logger.Info(logger.ComponentAgentCore).Msg("Stdio 客户端断开连接成功")
	return nil
}

// ListTools 列出 Stdio 服务器提供的工具。
func (c *StdioClient) ListTools(ctx context.Context) ([]*mcp.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	resp, err := c.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("从 Stdio 服务器获取工具列表失败")
		return nil, err
	}
	tools := make([]*mcp.McpToolCard, 0, len(resp.Tools))
	for _, tool := range resp.Tools {
		card := mcp.NewMcpToolCard(tool.Name, tool.Description, c.serverName, nil)
		tools = append(tools, card)
	}
	logger.Info(logger.ComponentAgentCore).
		Int("tool_count", len(tools)).
		Msg("从 Stdio 服务器获取工具列表")
	return tools, nil
}

// CallTool 通过 Stdio 调用工具。
func (c *StdioClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	logger.Info(logger.ComponentAgentCore).
		Str("tool_name", toolName).
		Msg("通过 Stdio 调用工具")

	resp, err := c.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Err(err).
			Msg("Stdio 工具调用失败")
		return nil, err
	}

	result := callToolResultToMap(resp)
	logger.Info(logger.ComponentAgentCore).
		Str("tool_name", toolName).
		Msg("Stdio 工具调用完成")
	return result, nil
}

// GetToolInfo 获取 Stdio 服务器上指定工具的信息。
func (c *StdioClient) GetToolInfo(ctx context.Context, toolName string) (*mcp.McpToolCard, error) {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tools {
		if t.Name == toolName {
			logger.Debug(logger.ComponentAgentCore).
				Str("tool_name", toolName).
				Msg("在 Stdio 服务器上找到工具")
			return t, nil
		}
	}
	logger.Warn(logger.ComponentAgentCore).
		Str("tool_name", toolName).
		Msg("在 Stdio 服务器上未找到工具")
	return nil, nil
}

// ListResources 列出 Stdio 服务器提供的资源。
func (c *StdioClient) ListResources(ctx context.Context) ([]any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	resp, err := c.mcpClient.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).Msg("从 Stdio 服务器获取资源列表失败")
		return nil, err
	}
	result := make([]any, len(resp.Resources))
	for i, r := range resp.Resources {
		result[i] = r
	}
	return result, nil
}

// ReadResource 读取 Stdio 服务器上的资源。
func (c *StdioClient) ReadResource(ctx context.Context, uri string) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
	resp, err := c.mcpClient.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{URI: uri},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("uri", uri).
			Err(err).
			Msg("从 Stdio 服务器读取资源失败")
		return nil, err
	}
	return resp.Contents, nil
}

// Close 关闭 Stdio 客户端。
func (c *StdioClient) Close() error {
	return c.Disconnect(context.Background())
}
```

- [ ] **Step 2: 创建 stdio_client_test.go（//go:build llm）**

```go
//go:build llm

package client

import (
	"context"
	"testing"
)

// TestStdioClient_真实调用 测试 Stdio 客户端真实连接
// 运行方式: go test -tags=llm ./internal/agentcore/foundation/tool/mcp/client/... -run TestStdioClient -v
func TestStdioClient_真实调用(t *testing.T) {
	// 需要真实 MCP Stdio 服务器
	t.Skip("需要真实 MCP Stdio 服务器，手动测试时移除 Skip")
}
```

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/client/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/stdio_client.go internal/agentcore/foundation/tool/mcp/client/stdio_client_test.go && git commit -m "feat(tool): 实现 StdioClient (3.6)"
```

---

### Task 9: 实现 StreamableHttpClient

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/streamable_http_client.go`
- Create: `internal/agentcore/foundation/tool/mcp/client/streamable_http_client_test.go`

- [ ] **Step 1: 实现 StreamableHttpClient**

结构与 SseClient 高度相似，差异在 Connect 中使用 `mcpclient.NewStreamableHttpClient`。代码模式与 Task 7/8 一致，此处省略重复的 ListTools/CallTool/GetToolInfo/ListResources/ReadResource 实现（与 SseClient 相同逻辑）。

关键差异：
- `Connect` 使用 `mcpclient.NewStreamableHttpClient(serverPath, transportOpts...)`
- 支持 `_normalizeConfig`：如果 config 为 McpServerConfig，直接使用；如果为 URL 字符串，自动构造 McpServerConfig
- 日志消息中使用 "StreamableHTTP" 替代 "SSE"

- [ ] **Step 2: 创建 streamable_http_client_test.go（//go:build llm）**

```go
//go:build llm

package client

import (
	"testing"
)

// TestStreamableHttpClient_真实调用 测试 StreamableHTTP 客户端真实连接
// 运行方式: go test -tags=llm ./internal/agentcore/foundation/tool/mcp/client/... -run TestStreamableHttpClient -v
func TestStreamableHttpClient_真实调用(t *testing.T) {
	t.Skip("需要真实 MCP StreamableHTTP 服务器，手动测试时移除 Skip")
}
```

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/client/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/streamable_http_client.go internal/agentcore/foundation/tool/mcp/client/streamable_http_client_test.go && git commit -m "feat(tool): 实现 StreamableHttpClient (3.6)"
```

---

### Task 10: 实现 PlaywrightClient

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/playwright_client.go`
- Create: `internal/agentcore/foundation/tool/mcp/client/playwright_client_test.go`

- [ ] **Step 1: 实现 PlaywrightClient**

关键差异（与 SseClient/StdioClient 对比）：
- `Connect` 根据 `config.ServerPath` 判断传输类型：
  - 以 `http://` 或 `https://` 开头 → 创建 SSE 客户端连接
  - 其他 → 创建 Stdio 客户端连接（通过 `npx @playwright/mcp` 启动）
- 内部持有 `delegate McpClient`（由 Connect 时决定是 SseClient 还是 StdioClient）
- 所有方法委托给 delegate

```go
// PlaywrightClient Playwright 浏览器工具 MCP 客户端，支持 SSE/stdio 双传输。
type PlaywrightClient struct {
	config     *mcp.McpServerConfig
	serverName string
	delegate   McpClient // SSE 或 Stdio 客户端
	isConnected bool
}

func (c *PlaywrightClient) Connect(ctx context.Context, opts ...ConnectOption) error {
	// 判断传输类型
	if strings.HasPrefix(c.config.ServerPath, "http://") || strings.HasPrefix(c.config.ServerPath, "https://") {
		c.delegate = NewSseClient(c.config)
	} else {
		c.delegate = NewStdioClient(c.config)
	}
	err := c.delegate.Connect(ctx, opts...)
	if err == nil {
		c.isConnected = true
	}
	return err
}

// Disconnect/ListTools/CallTool/GetToolInfo/ListResources/ReadResource/Close
// 全部委托给 c.delegate
```

- [ ] **Step 2: 创建 playwright_client_test.go（//go:build llm）**

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/client/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/playwright_client.go internal/agentcore/foundation/tool/mcp/client/playwright_client_test.go && git commit -m "feat(tool): 实现 PlaywrightClient (3.6)"
```

---

### Task 11: 实现 OpenApiClient

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/client/openapi_client.go`
- Create: `internal/agentcore/foundation/tool/mcp/client/openapi_client_test.go`

这是最复杂的客户端，不使用 mcp-go ClientSession，而是基于 kin-openapi 自行实现 OpenAPI→MCP 转换。

- [ ] **Step 1: 实现 OpenApiClient**

核心结构：

```go
// OpenApiClient 基于 OpenAPI 规格的 MCP 客户端。
// 不走 mcp-go ClientSession，而是用 kin-openapi 解析规格 + net/http 执行请求。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/openapi_client.py (OpenApiClient)
type OpenApiClient struct {
	config      *mcp.McpServerConfig
	serverName  string
	httpClient  *http.Client
	tools       map[string]*openAPIToolInfo // tool_name → 路由+参数信息
	toolCards   []*mcp.McpToolCard          // Connect 时解析的工具列表
	isConnected bool
}

// openAPIToolInfo OpenAPI 工具路由信息
type openAPIToolInfo struct {
	method      string
	path        string
	description string
	parameters  []openapiParameterInfo
	requestBody *openapiRequestBodyInfo
}

type openapiParameterInfo struct {
	name     string
	in       string // path, query, header, cookie
	required bool
	schema   map[string]any
}

type openapiRequestBodyInfo struct {
	contentType string
	schema      map[string]any
}
```

Connect 流程：
1. 读取 OpenAPI/YAML 文件（支持逗号分隔多文件）
2. kin-openapi 解析规格
3. 遍历 Paths → Operations → 生成 openAPIToolInfo + McpToolCard
4. 存储 tools map

CallTool 流程：
1. 根据 toolName 查找 openAPIToolInfo
2. 构造 HTTP 请求（path 参数替换、query 参数拼接、header 注入、body 序列化）
3. 发送请求 → 解析响应 → 返回

ListResources/ReadResource: 返回空

- [ ] **Step 2: 创建 openapi_client_test.go**

单测部分：OpenAPI 文件解析逻辑（使用测试 YAML 文件），HTTP 调用部分用 `//go:build integration`。

- [ ] **Step 3: 验证编译通过**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/mcp/client/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/openapi_client.go internal/agentcore/foundation/tool/mcp/client/openapi_client_test.go && git commit -m "feat(tool): 实现 OpenApiClient (3.6)"
```

---

### Task 12: 实现 MCPTool（3.5）

**Files:**
- Modify: `internal/agentcore/foundation/tool/mcp/base.go`
- Modify: `internal/agentcore/foundation/tool/mcp/base_test.go`

- [ ] **Step 1: 追加 MCPTool 测试**

在 `base_test.go` 末尾追加：

```go
func TestNewMCPTool_客户端为nil时返回错误(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "server", nil)
	_, err := NewMCPTool(nil, card)
	assert.Error(t, err)
}

func TestMCPTool_Card(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "server", nil)
	fake := &fakeMcpClient{}
	mcpTool, _ := NewMCPTool(fake, card)
	assert.Equal(t, &card.ToolCard, mcpTool.Card())
}

func TestMCPTool_Invoke_直接传参(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "server", nil) // InputParams 为 nil
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, toolName string, arguments map[string]any) (any, error) {
			assert.Equal(t, "tool", toolName)
			assert.Equal(t, map[string]any{"key": "val"}, arguments)
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "result"},
			}}, nil
		},
	}
	mcpTool, _ := NewMCPTool(fake, card)
	result, err := mcpTool.Invoke(context.Background(), map[string]any{"key": "val"})
	assert.NoError(t, err)
	assert.Equal(t, "result", result["result"])
}

func TestMCPTool_Invoke_参数格式化(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true},
	}
	card := NewMcpToolCard("tool", "desc", "server", params)
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, toolName string, arguments map[string]any) (any, error) {
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "ok"},
			}}, nil
		},
	}
	mcpTool, _ := NewMCPTool(fake, card)
	result, err := mcpTool.Invoke(context.Background(), map[string]any{"query": "test"})
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["result"])
}

func TestMCPTool_Invoke_客户端调用失败(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "server", nil)
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, _ string, _ map[string]any) (any, error) {
			return nil, fmt.Errorf("connection lost")
		},
	}
	mcpTool, _ := NewMCPTool(fake, card)
	_, err := mcpTool.Invoke(context.Background(), map[string]any{})
	assert.Error(t, err)
}

func TestMCPTool_Stream_不支持(t *testing.T) {
	card := NewMcpToolCard("tool", "desc", "server", nil)
	fake := &fakeMcpClient{}
	mcpTool, _ := NewMCPTool(fake, card)
	_, err := mcpTool.Stream(context.Background(), map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream is not support")
}
```

同时添加 fakeMcpClient：

```go
// fakeMcpClient 用于 MCPTool 单元测试的模拟客户端
type fakeMcpClient struct {
	callToolFunc   func(ctx context.Context, toolName string, arguments map[string]any) (any, error)
	listToolsFunc  func(ctx context.Context) ([]*McpToolCard, error)
}

func (f *fakeMcpClient) Connect(_ context.Context, _ ...client.ConnectOption) error { return nil }
func (f *fakeMcpClient) Disconnect(_ context.Context) error                        { return nil }
func (f *fakeMcpClient) ListTools(ctx context.Context) ([]*McpToolCard, error) {
	if f.listToolsFunc != nil {
		return f.listToolsFunc(ctx)
	}
	return nil, nil
}
func (f *fakeMcpClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if f.callToolFunc != nil {
		return f.callToolFunc(ctx, toolName, arguments)
	}
	return nil, nil
}
func (f *fakeMcpClient) GetToolInfo(_ context.Context, _ string) (*McpToolCard, error) {
	return nil, nil
}
func (f *fakeMcpClient) ListResources(_ context.Context) ([]any, error) { return nil, nil }
func (f *fakeMcpClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}
func (f *fakeMcpClient) Close() error { return nil }
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestNewMCPTool|TestMCPTool" -v
```

- [ ] **Step 3: 在 base.go 中追加 MCPTool**

在导出函数区块追加：

```go
// MCPTool MCP 协议工具，通过 McpClient 调用远程 MCP 服务器工具。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/base.py (MCPTool)
type MCPTool struct {
	card      *McpToolCard
	mcpClient client.McpClient
}

// NewMCPTool 创建 MCP 工具实例。
// mcpClient 为 nil 时返回 StatusToolMcpClientNotSupported 错误。
//
// 对应 Python: MCPTool.__init__(mcp_client, tool_info)
func NewMCPTool(mcpClient client.McpClient, card *McpToolCard) (*MCPTool, error) {
	if mcpClient == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpClientNotSupported,
			exception.WithParam("card", card.String()),
		)
	}
	return &MCPTool{card: card, mcpClient: mcpClient}, nil
}

// Card 返回工具配置卡片。
func (t *MCPTool) Card() *tool.ToolCard {
	return &t.card.ToolCard
}

// Invoke 调用 MCP 远程工具。
//
// 流程：
//  1. 如果 card.InputParams 不为 nil，用 SchemaUtils 格式化参数
//  2. mcpClient.CallTool 调用远程工具
//  3. ExtractMCPToolResultContent 提取紧凑结果
//  4. 返回 {"result": extracted}
//
// 对应 Python: MCPTool.invoke()
func (t *MCPTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	arguments := inputs
	if t.card.InputParams != nil {
		callOpts := tool.NewToolCallOptions(opts...)
		// ⤵️ 预留：触发 TOOL_PARSE_STARTED 事件（等回调系统就绪后回填）
		formatted, err := tool.SchemaUtils{}.FormatWithSchema(
			inputs, t.card.InputParams,
			tool.WithFormatSkipValidate(callOpts.SkipInputsValidate),
		)
		if err != nil {
			return nil, err
		}
		arguments = formatted
		if callOpts.SkipNoneValue {
			arguments = tool.SchemaUtils{}.RemoveNoneValues(arguments)
			if arguments == nil {
				arguments = make(map[string]any)
			}
		}
		// ⤵️ 预留：触发 TOOL_PARSE_FINISHED 事件（等回调系统就绪后回填）
	}

	result, err := t.mcpClient.CallTool(ctx, t.card.Name, arguments)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "invoke"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", t.card.String()),
			exception.WithCause(err),
		)
	}

	extracted := ExtractMCPToolResultContent(result)
	return map[string]any{"result": extracted}, nil
}

// Stream MCP 工具不支持流式调用，返回 ErrStreamNotSupported。
//
// 对应 Python: MCPTool.stream() → raise TOOL_STREAM_NOT_SUPPORTED
func (t *MCPTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.String())
}
```

import 中需添加 `client` 子包引用和 `tool` 包引用。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/... -run "TestNewMCPTool|TestMCPTool" -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/base.go internal/agentcore/foundation/tool/mcp/base_test.go && git commit -m "feat(tool): 实现 MCPTool (3.5)"
```

---

### Task 13: 实现工厂函数测试和 fakeMcpClient

**Files:**
- Create: `internal/agentcore/foundation/tool/mcp/client/mcp_client_test.go`

- [ ] **Step 1: 编写工厂函数测试**

```go
package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp"
)

func TestNewMcpClient_SSE(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
	_, ok := client.(*SseClient)
	assert.True(t, ok, "应为 SseClient 类型")
}

func TestNewMcpClient_Stdio(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "npx", "stdio")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	_, ok := client.(*StdioClient)
	assert.True(t, ok, "应为 StdioClient 类型")
}

func TestNewMcpClient_StreamableHTTP(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	_, ok := client.(*StreamableHttpClient)
	assert.True(t, ok, "应为 StreamableHttpClient 类型")
}

func TestNewMcpClient_StreamableHTTP下划线(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable_http")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	_, ok := client.(*StreamableHttpClient)
	assert.True(t, ok, "streamable_http 应创建 StreamableHttpClient")
}

func TestNewMcpClient_OpenAPI(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "openapi.json", "openapi")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	_, ok := client.(*OpenApiClient)
	assert.True(t, ok, "应为 OpenApiClient 类型")
}

func TestNewMcpClient_Playwright(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "npx @playwright/mcp", "playwright")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	_, ok := client.(*PlaywrightClient)
	assert.True(t, ok, "应为 PlaywrightClient 类型")
}

func TestNewMcpClient_未知类型(t *testing.T) {
	config := mcp.NewMcpServerConfig("test", "http://localhost:8080", "unknown")
	_, err := NewMcpClient(config)
	assert.Error(t, err)
}

func TestNewMcpClient_nil配置(t *testing.T) {
	_, err := NewMcpClient(nil)
	assert.Error(t, err)
}

// fakeMcpClient 用于其他包单元测试的模拟客户端
type fakeMcpClient struct {
	callToolFunc  func(ctx context.Context, toolName string, arguments map[string]any) (any, error)
	listToolsFunc func(ctx context.Context) ([]*mcp.McpToolCard, error)
}

func (f *fakeMcpClient) Connect(_ context.Context, _ ...ConnectOption) error { return nil }
func (f *fakeMcpClient) Disconnect(_ context.Context) error                  { return nil }
func (f *fakeMcpClient) ListTools(ctx context.Context) ([]*mcp.McpToolCard, error) {
	if f.listToolsFunc != nil {
		return f.listToolsFunc(ctx)
	}
	return nil, nil
}
func (f *fakeMcpClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if f.callToolFunc != nil {
		return f.callToolFunc(ctx, toolName, arguments)
	}
	return nil, nil
}
func (f *fakeMcpClient) GetToolInfo(_ context.Context, _ string) (*mcp.McpToolCard, error) {
	return nil, nil
}
func (f *fakeMcpClient) ListResources(_ context.Context) ([]any, error) { return nil, nil }
func (f *fakeMcpClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}
func (f *fakeMcpClient) Close() error { return nil }
```

- [ ] **Step 2: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/mcp/client/... -run "TestNewMcpClient" -v
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/mcp/client/mcp_client_test.go && git commit -m "test(tool): 添加 McpClient 工厂函数测试和 fakeMcpClient"
```

---

### Task 14: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/foundation/tool/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 tool/doc.go 文件目录，添加 mcp/ 条目**

在文件目录的 `tool/` 树中添加：

```
//	├── mcp/
//	│   ├── doc.go                        # MCP 包文档
//	│   ├── base.go                       # McpServerConfig + McpToolCard + MCPTool + ExtractMCPToolResultContent
//	│   ├── base_test.go                  # 单元测试
//	│   └── client/
//	│       ├── doc.go                    # 客户端子包文档
//	│       ├── mcp_client.go             # McpClient 接口 + ConnectOptions + 工厂函数
//	│       ├── sse_client.go             # SseClient 实现
//	│       ├── stdio_client.go           # StdioClient 实现
//	│       ├── streamable_http_client.go # StreamableHttpClient 实现
//	│       ├── openapi_client.go         # OpenApiClient 实现
//	│       └── playwright_client.go      # PlaywrightClient 实现
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 3.5/3.6/3.7 的状态**

将 3.5、3.6、3.7 三行从 `☐` 改为 `✅`。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/doc.go IMPLEMENTATION_PLAN.md && git commit -m "docs(tool): 更新 doc.go 文件目录和实现计划状态 3.5-3.7 ✅"
```

---

### Task 15: 全量测试验证

- [ ] **Step 1: 运行所有单元测试**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/foundation/tool/mcp/... -v
```

- [ ] **Step 2: 运行全量编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

- [ ] **Step 3: 运行已有测试确保无回归**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/... -v
```

- [ ] **Step 4: 最终提交（如有修复）**

```bash
git add -A && git commit -m "fix(tool): 修复全量测试发现的问题"
```
