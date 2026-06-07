package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInfo 工具描述信息，供 LLM function calling 消费。
//
// ServerName 非空时表示 MCP 工具，为空时表示本地工具。
// AbilityManager 路由时根据 ServerName 判断工具类型：
//   - ServerName == "" → 本地工具（InvokeFunction/StreamFunction/MapFunction）
//   - ServerName != "" → MCP 工具，通过 ServerName 找到对应 McpClient 路由
//
// 对应 Python: openjiuwen/core/foundation/tool/schema.py (ToolInfo)
// 注：完整实现在领域三（工具系统），此处仅定义基础结构供 BaseCard.ToolInfo() 使用。
type ToolInfo struct {
	// Type 类型标识，默认 "function"
	Type string `json:"type"`
	// Name 工具名称
	Name string `json:"name"`
	// Description 工具功能描述
	Description string `json:"description"`
	// Parameters 参数定义，对应 JSON Schema 的 parameters 字段
	Parameters map[string]any `json:"parameters"`
	// ServerName MCP 服务器名称，非空时表示 MCP 工具。
	// 不发送给 LLM（omitempty），仅框架内部路由使用。
	ServerName string `json:"server_name,omitempty"`
}

// McpToolInfo 是 ToolInfo 的类型别名，保持语义兼容。
//
// ServerName 非空时为 MCP 工具。类型别名保证 McpToolInfo 与 ToolInfo
// 完全等价，可自由互换，统一了工具列表管理。
//
// 对应 Python: openjiuwen/core/foundation/tool/schema.py (McpToolInfo)
type McpToolInfo = ToolInfo

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolInfoOption ToolInfo 构造选项函数。
type ToolInfoOption func(*ToolInfo)

// WithServerName 设置 MCP 服务器名称（非空时表示 MCP 工具）。
func WithServerName(name string) ToolInfoOption {
	return func(t *ToolInfo) { t.ServerName = name }
}

// NewToolInfo 创建 ToolInfo 实例，Type 默认为 "function"。
func NewToolInfo(name, description string, parameters map[string]any, opts ...ToolInfoOption) *ToolInfo {
	if parameters == nil {
		parameters = make(map[string]any)
	}
	ti := &ToolInfo{
		Type:        "function",
		Name:        name,
		Description: description,
		Parameters:  parameters,
	}
	for _, opt := range opts {
		opt(ti)
	}
	return ti
}

// NewMcpToolInfo 创建 MCP 工具描述信息（ToolInfo + ServerName）。
//
// 对应 Python: McpToolCard.tool_info() -> McpToolInfo
func NewMcpToolInfo(name, description, serverName string, parameters map[string]any) *McpToolInfo {
	return NewToolInfo(name, description, parameters, WithServerName(serverName))
}
