# 3.5-3.7 MCPTool + McpClient + McpServerConfig 设计

## 概述

实现领域三（工具系统）的 3.5/3.6/3.7 三个步骤：MCP 协议工具、MCP 客户端、MCP 服务器配置。
三者紧密耦合，分步编号但连续实现。

对应 Python 代码：`openjiuwen/core/foundation/tool/mcp/`

## 包结构

```
tool/mcp/
├── doc.go                        # 包文档
├── base.go                       # McpServerConfig + McpToolCard + MCPTool + ExtractMCPToolResultContent
├── base_test.go                  # 上述类型的单元测试
└── client/
    ├── doc.go                    # 子包文档
    ├── mcp_client.go             # McpClient 接口 + ConnectOption + 工厂函数
    ├── sse_client.go             # SseClient 实现
    ├── stdio_client.go           # StdioClient 实现
    ├── streamable_http_client.go # StreamableHttpClient 实现
    ├── openapi_client.go         # OpenApiClient 实现（基于 kin-openapi）
    ├── playwright_client.go      # PlaywrightClient 实现（SSE/stdio 双传输）
    ├── mcp_client_test.go        # 接口/工厂测试 + fakeMcpClient
    ├── sse_client_test.go        # SSE 客户端测试（//go:build llm）
    ├── stdio_client_test.go      # Stdio 客户端测试（//go:build llm）
    ├── streamable_http_client_test.go  # StreamableHTTP 测试（//go:build llm）
    ├── openapi_client_test.go    # OpenAPI 客户端测试
    └── playwright_client_test.go # Playwright 客户端测试（//go:build llm）
```

## 3.7 McpServerConfig

MCP 服务器配置模型。

```go
// McpServerConfig MCP 服务器配置
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

// McpServerConfigOption 配置选项函数
type McpServerConfigOption func(*McpServerConfig)

// NewMcpServerConfig 创建 MCP 服务器配置
func NewMcpServerConfig(name, serverPath, clientType string, opts ...McpServerConfigOption) *McpServerConfig

// 便捷选项
func WithServerID(id string) McpServerConfigOption
func WithParams(params map[string]any) McpServerConfigOption
func WithAuthHeaders(headers map[string]string) McpServerConfigOption
func WithAuthQueryParams(params map[string]string) McpServerConfigOption
```

**与 Python 差异**：
- `ClientType` 默认值为 `"sse"`（与 Python `client_type: str = 'sse'` 一致）
- `ServerID` 默认自动生成 UUID（与 Python `uuid.uuid4().hex` 一致）
- 构造方式用函数式选项替代 Python 的 Pydantic BaseModel

## 3.5 McpToolCard

MCP 工具配置卡片，扩展 ToolCard 增加服务器标识。

```go
// McpToolCard MCP 工具配置卡片，扩展 ToolCard 增加服务器标识
type McpToolCard struct {
    ToolCard
    // ServerName MCP 服务器名称
    ServerName string
    // ServerID MCP 服务器标识
    ServerID string
}

// McpToolInfo 返回 MCP 工具描述信息，供 LLM function calling 消费
func (c *McpToolCard) McpToolInfo() *schema.McpToolInfo

// NewMcpToolCard 创建 MCP 工具卡片
func NewMcpToolCard(name, description, serverName string, inputParams []*schema.Param, opts ...McpToolCardOption) *McpToolCard
```

## 3.5 MCPTool

MCP 协议工具，通过 McpClient 调用远程 MCP 服务器工具。

```go
// MCPTool MCP 协议工具，通过 McpClient 调用远程 MCP 服务器工具
type MCPTool struct {
    card      *McpToolCard
    mcpClient McpClient
}

// NewMCPTool 创建 MCP 工具实例
// mcpClient 为 nil 时返回 StatusToolMcpClientNotSupported 错误
func NewMCPTool(mcpClient McpClient, card *McpToolCard) (*MCPTool, error)

// Card 返回工具配置卡片
func (t *MCPTool) Card() *ToolCard

// Invoke 调用 MCP 远程工具
func (t *MCPTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)

// Stream MCP 工具不支持流式调用，返回 ErrStreamNotSupported
func (t *MCPTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
```

### Invoke 流程

```
1. arguments = inputs（如果 card.InputParams 为 nil 则直接用 inputs）
2. 如果 card.InputParams 不为 nil：
   a. 触发 TOOL_PARSE_STARTED 事件（⤵️ 预留，等回调系统就绪后回填）
   b. SchemaUtils.FormatWithSchema(inputs, card.InputParams, ...) 格式化参数
   c. 移除 nil 值（如果 SkipNoneValue=true）
   d. 触发 TOOL_PARSE_FINISHED 事件（⤵️ 预留）
3. mcpClient.CallTool(ctx, card.Name, arguments) 调用远程工具
4. ExtractMCPToolResultContent(result) 提取紧凑结果
5. 返回 {"result": extracted_result}
6. 异常时返回 StatusToolMcpExecutionError 错误
```

### ExtractMCPToolResultContent

```go
// ExtractMCPToolResultContent 从 MCP CallToolResult 提取紧凑结果值
// 严格按照 mcp-go 的 mcp.CallToolResult 类型解析
func ExtractMCPToolResultContent(toolResult any) any
```

解析逻辑（符合 MCP 协议）：
- 提取 `content` 最后一个元素
- 如果有 `text` 字段 → 返回 text 字符串
- 如果有 `data` 字段 + `mimeType` 以 `image/` 开头 → 返回 `[image content: {mime}, {len} base64 chars]`
- 如果有 `data` 字段 → 返回 data
- 其他 → 返回字符串化结果

## 3.6 McpClient 接口

```go
// McpClient MCP 客户端接口，定义与 MCP 服务器交互的标准方法
type McpClient interface {
    // Connect 建立 MCP 服务器连接
    Connect(ctx context.Context, opts ...ConnectOption) error
    // Disconnect 断开 MCP 服务器连接
    Disconnect(ctx context.Context) error
    // ListTools 列出服务器提供的工具
    ListTools(ctx context.Context) ([]*McpToolCard, error)
    // CallTool 调用指定工具
    CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error)
    // GetToolInfo 获取指定工具信息
    GetToolInfo(ctx context.Context, toolName string) (*McpToolCard, error)
    // ListResources 列出服务器提供的资源
    ListResources(ctx context.Context) ([]any, error)
    // ReadResource 读取指定资源
    ReadResource(ctx context.Context, uri string) (any, error)
    // Close 关闭客户端（等价于 Disconnect）
    Close() error
}
```

### ConnectOption

```go
// NoTimeout 不设超时
const NoTimeout = -1

// ConnectOptions 连接选项
type ConnectOptions struct {
    // RetryTimes 重试次数
    RetryTimes int
    // Timeout 超时时间（秒），NoTimeout 表示不限
    Timeout float64
}

// ConnectOption 连接选项函数
type ConnectOption func(*ConnectOptions)
```

### 工厂函数

```go
// NewMcpClient 根据配置创建对应类型的 MCP 客户端
// 支持 clientType: sse / stdio / streamable-http / streamable_http / openapi / playwright
// 未知类型返回 StatusToolMcpClientTypeUnknown 错误
func NewMcpClient(config *McpServerConfig) (McpClient, error)
```

## 3.6 各客户端实现

### SseClient

- 内部组合 mcp-go 的 `*client.Client`
- Connect: `client.NewSSEMCPClient(serverPath, WithHeaders(authHeaders))` → `Start` → `Initialize`
- Disconnect: `client.Close()`
- ListTools/CallTool/ListResources/ReadResource: 委托给 mcp-go Client 对应方法
- 认证：预留 AuthHeaders/AuthQueryParams 字段，暂不触发 TOOL_AUTH 回调（⤵️ 3.11 回填）

### StdioClient

- 内部组合 mcp-go 的 `*client.Client`
- Connect: `client.NewStdioMCPClient(command, env, args...)` → `Start` → `Initialize`
  - Params 中的 command/args/env/cwd/encoding_error_handler 映射到 mcp-go 参数
- Disconnect: `client.Close()`
- 无认证需求

### StreamableHttpClient

- 内部组合 mcp-go 的 `*client.Client`
- Connect: `client.NewStreamableHttpClient(serverPath, WithHeaders(authHeaders))` → `Start` → `Initialize`
- 认证：同 SseClient，预留（⤵️ 3.11 回填）
- 支持 config 为 `McpServerConfig` 或纯 URL 字符串两种构造方式（对齐 Python `_normalize_config`）

### OpenApiClient

- 内部组合 kin-openapi 解析 + net/http 执行（不依赖 mcp-go ClientSession）
- Connect:
  1. 加载 OpenAPI/YAML 文件（支持逗号分隔多文件路径）
  2. kin-openapi 解析规格为 HTTPRoute 列表
  3. 将 HTTPRoute 转换为 McpToolCard（含参数映射）
  4. 存储 toolManager map[string]OpenAPITool
- Disconnect: 关闭 http.Client
- CallTool: 根据 tool_name 找到路由 → 构造 HTTP 请求 → 发送 → 解析响应
- ListTools: 返回 Connect 时解析的 McpToolCard 列表
- ListResources/ReadResource: 返回空（与 Python 一致，OpenAPI 不支持 MCP 资源）
- 依赖：`github.com/getkin/kin-openapi`

### PlaywrightClient

- 内部组合 mcp-go 的 `*client.Client`（根据 server_path 类型选择传输）
- Connect:
  - server_path 为 URL（http/https 开头）→ 用 SSE 传输
  - server_path 为命令 → 用 Stdio 传输（npx @playwright/mcp）
- Disconnect: `client.Close()`
- ListTools/CallTool 等: 委托给 mcp-go Client

### 共同模式

所有客户端共享：
- 持有 `*McpServerConfig` 配置
- 持有 mcp-go `*client.Client`（SSE/Stdio/StreamableHTTP/Playwright）或自实现逻辑（OpenAPI）
- 持有 `serverName` 用于日志和 McpToolCard 标识
- `isConnected` 状态标记
- 标准日志记录（Info/Warn/Error，使用 `ComponentAgentCore` 组件）

## 错误码

`codes_tool.go` 中已有：
- `StatusToolMcpClientNotSupported (182300)` — MCPTool 构造时 mcpClient 为 nil
- `StatusToolMcpExecutionError (182301)` — MCPTool.Invoke 执行失败

需新增：
- `StatusToolMcpClientTypeUnknown (182302)` — 工厂遇到未知 clientType
- `StatusToolMcpNotConnected (182303)` — 客户端未连接时调用操作
- `StatusToolOpenApiClientExecutionError (182304)` — OpenAPI 客户端执行错误

## 日志规则

遵循项目日志同步规则（规则 3），对齐 Python 日志调用：

| Python 日志点 | Go 日志 |
|-------------|--------|
| `logger.info(f"SSE client connected successfully to {self._server_path}")` | `logger.Info(ComponentAgentCore).Str("server_path", path).Msg("SSE 客户端连接成功")` |
| `logger.error(f"SSE connection failed to {self._server_path}: {e}")` | `logger.Error(ComponentAgentCore).Str("server_path", path).Err(err).Msg("SSE 连接失败")` |
| `logger.info(f"Retrieved {len(tools_list)} tools from SSE server")` | `logger.Info(ComponentAgentCore).Int("tool_count", len(tools)).Msg("从 SSE 服务器获取工具列表")` |
| `logger.info(f"Calling tool '{tool_name}' via SSE with arguments: {arguments}")` | `logger.Info(ComponentAgentCore).Str("tool_name", name).Msg("通过 SSE 调用工具")` |
| `logger.error(f"Tool call failed via SSE: {e}")` | `logger.Error(ComponentAgentCore).Str("tool_name", name).Err(err).Msg("SSE 工具调用失败")` |

异常路径日志必须包含 `event_type=LLM_CALL_ERROR`、`method`、`model_provider` 等上下文字段。

## 依赖新增

```
go get github.com/mark3labs/mcp-go      # MCP 协议客户端
go get github.com/getkin/kin-openapi    # OpenAPI 规格解析（OpenApiClient）
```

## 测试策略

| 组件 | 测试方式 | 说明 |
|------|---------|------|
| McpServerConfig | 单元测试 | 构造函数、默认值、Option 函数 |
| McpToolCard | 单元测试 | 构造、McpToolInfo() 输出 |
| MCPTool | 单元测试 + fakeMcpClient | mock McpClient 测试 Invoke/Stream 逻辑 |
| ExtractMCPToolResultContent | 单元测试 | 各种 content 类型（text/image/data/dict） |
| NewMcpClient 工厂 | 单元测试 + mock | 各种 clientType 分支、未知类型报错 |
| SseClient | `//go:build llm` | 依赖真实 MCP 服务器 |
| StdioClient | `//go:build llm` | 依赖真实 MCP 子进程 |
| StreamableHttpClient | `//go:build llm` | 依赖真实 MCP 服务器 |
| PlaywrightClient | `//go:build llm` | 依赖 Node.js + @playwright/mcp |
| OpenApiClient | 单元测试 + `//go:build integration` | 解析逻辑可单测，HTTP 调用需真实服务 |

fakeMcpClient 实现 McpClient 接口，用于 MCPTool 的单元测试：

```go
type fakeMcpClient struct {
    callToolFunc func(ctx context.Context, toolName string, arguments map[string]any) (any, error)
    listToolsFunc func(ctx context.Context) ([]*McpToolCard, error)
    // ... 其他方法
}
```

## 认证预留

McpServerConfig 保留 AuthHeaders/AuthQueryParams 字段，McpClient 接口预留认证相关字段传递。
SSE/StreamableHttpClient 的 Connect 方法中：
- 当前：直接将 AuthHeaders 透传给 mcp-go 的 WithHeaders 选项
- 后续（3.11）：增加 TOOL_AUTH 回调触发逻辑，动态获取认证信息

实现计划中用 ⤵️ 标记预留点，3.11 中用 ⤴️ 标记回填来源。
