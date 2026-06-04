package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInfo 工具描述信息，供 LLM function calling 消费。
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
}

// McpToolInfo MCP 协议工具描述信息，扩展 ToolInfo。
//
// 对应 Python: openjiuwen/core/foundation/tool/schema.py (McpToolInfo)
type McpToolInfo struct {
	ToolInfo
	// ServerName MCP 服务器名称
	ServerName string `json:"server_name"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolInfo 创建 ToolInfo 实例，Type 默认为 "function"。
func NewToolInfo(name, description string, parameters map[string]any) *ToolInfo {
	if parameters == nil {
		parameters = make(map[string]any)
	}
	return &ToolInfo{
		Type:        "function",
		Name:        name,
		Description: description,
		Parameters:  parameters,
	}
}

// NewMcpToolInfo 创建 McpToolInfo 实例。
func NewMcpToolInfo(name, description, serverName string, parameters map[string]any) *McpToolInfo {
	return &McpToolInfo{
		ToolInfo: ToolInfo{
			Type:        "function",
			Name:        name,
			Description: description,
			Parameters:  parameters,
		},
		ServerName: serverName,
	}
}
