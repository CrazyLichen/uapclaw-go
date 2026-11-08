package types

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpClient MCP 客户端接口，定义与 MCP 服务器交互的标准方法。
// Python: openjiuwen/core/foundation/tool/mcp/client/mcp_client.py (McpClient)
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
	// ListResources 列出服务器提供的资源，返回 map 切片（每个 map 含 uri/name/mimeType/description）
	ListResources(ctx context.Context) ([]map[string]any, error)
	// ReadResource 读取指定资源，返回 map 切片（每个 map 含 uri/mimeType/text 或 uri/mimeType/blob）
	ReadResource(ctx context.Context, uri string) ([]map[string]any, error)
	// Close 关闭客户端（等价于 Disconnect）
	Close() error
}

// ConnectOptions 连接选项。
type ConnectOptions struct {
	// RetryTimes 重试次数
	RetryTimes int
	// Timeout 超时时间（秒），NoTimeout 表示不限
	Timeout float64
}

// McpServerConfig MCP 服务器配置。
// Python: openjiuwen/core/foundation/tool/mcp/base.py (McpServerConfig)
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
	// IncludeImageContent 是否把 MCP 返回的图片块桥接为多模态工具结果数据。
	// 默认 false：图片退化为文本占位符（[image content: image/png, N base64 chars]）。
	// 仅对模型必须看到图片的服务端开启（如 cua-driver 截图）。
	// Python: McpServerConfig.include_image_content (mcp/base.py:139-146)
	IncludeImageContent bool
}

// McpToolCard MCP 工具配置卡片，扩展 ToolCard 增加服务器标识。
// Python: openjiuwen/core/foundation/tool/mcp/base.py (McpToolCard)
type McpToolCard struct {
	tool.ToolCard
	// ServerName MCP 服务器名称
	ServerName string
	// ServerID MCP 服务器标识
	ServerID string
}

// ConnectOption 连接选项函数。
type ConnectOption func(*ConnectOptions)

// McpServerConfigOption 配置选项函数。
type McpServerConfigOption func(*McpServerConfig)

// McpToolCardOption MCP 工具卡片选项函数。
type McpToolCardOption func(*McpToolCard)

// MCPToolResult 携带多模态数据或失败状态的 MCP 工具结果。
// 放在 types 包而非 mcp 实现包：调度层（ability）与 agent 层（agents）都要
// 消费它，依赖纯类型包可避免两个上层直接依赖 mcp 实现包（层次渗透）。
// 与 harness 的 ToolOutput 形状（success/data/error）鸭子类型兼容，
// 使 react agent 的多模态管线无需 core 反向导入 harness。
// Python: McpToolResult (mcp/base.py:148-165)
type MCPToolResult struct {
	// Success 是否成功
	Success bool
	// Data 结果数据；多模态时含 content 与 multimodal
	Data map[string]any
	// Err 失败原因
	Err string
}

// ──────────────────────────── 常量 ────────────────────────────

// NoTimeout 不设超时，与 Python NO_TIMEOUT = -1 对齐。
const NoTimeout = -1

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 McpToolCard 满足 CardInterface 接口
var _ schema.CardInterface = (*McpToolCard)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

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
		Timeout: NoTimeout,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

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

// WithIncludeImageContent 设置是否把图片块桥接为多模态数据。
func WithIncludeImageContent(include bool) McpServerConfigOption {
	return func(c *McpServerConfig) { c.IncludeImageContent = include }
}

// NewMcpServerConfig 创建 MCP 服务器配置。
// Python: McpServerConfig(server_name=..., server_path=..., client_type=...)
func NewMcpServerConfig(name, serverPath, clientType string, opts ...McpServerConfigOption) *McpServerConfig {
	if clientType == "" {
		clientType = "sse"
	}
	c := &McpServerConfig{
		ServerID:   strings.ReplaceAll(uuid.New().String(), "-", ""),
		ServerName: name,
		ServerPath: serverPath,
		ClientType: clientType,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithMcpToolCardServerID 设置 MCP 工具卡片的服务器标识。
func WithMcpToolCardServerID(id string) McpToolCardOption {
	return func(c *McpToolCard) { c.ServerID = id }
}

// NewMcpToolCard 创建 MCP 工具卡片。
// Python: McpToolCard(name=..., server_name=..., description=..., input_params=...)
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

// ToolInfo 返回 MCP 工具描述信息，覆写 ToolCard.ToolInfo() 返回 McpToolInfo。
// McpToolInfo 嵌入 ToolInfo 并扩展 ServerName，标识此工具为 MCP 工具。
// AbilityManager 路由时可根据 ServerName 非空判断为 MCP 工具，
// 也可根据 tool_call.Name 在注册表中查找 Tool 实例。
// Python: McpToolCard.tool_info() -> McpToolInfo
func (c *McpToolCard) ToolInfo() schema.ToolInfoInterface {
	parameters := schema.ToJSONSchemaMap(c.InputParams)
	return schema.NewMcpToolInfo(c.Name, c.Description, c.ServerName, parameters)
}

// AbilityName 实现 schema.Ability 接口。
func (c *McpServerConfig) AbilityName() string { return c.ServerName }

// AbilityID 实现 schema.Ability 接口。
func (c *McpServerConfig) AbilityID() string { return c.ServerID }

// AbilityKind 实现 schema.Ability 接口。
func (c *McpServerConfig) AbilityKind() schema.AbilityKind { return schema.AbilityKindMcpServer }
