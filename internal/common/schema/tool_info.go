package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInfoProvider 工具信息提供者接口，供 LLM 层统一消费。
//
// ToolInfo 和 McpToolInfo 均实现此接口。
// LLM 层（ConvertToolsToDict / InvokeParams.Tools）只依赖此接口，
// 不关心具体是本地工具还是 MCP 工具。
//
// 对应 Python: _convert_tools_to_dict() 中统一处理 ToolInfo / McpToolInfo 的逻辑
type ToolInfoProvider interface {
	// GetToolInfo 返回基础工具描述信息（传给 LLM 的公共字段）
	GetToolInfo() *ToolInfo
}

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

// McpToolInfo MCP 协议工具描述信息，扩展 ToolInfo 增加服务器标识。
//
// ServerName 仅框架内部路由使用，不发送给 LLM（ConvertToolsToDict 只提取 ToolInfo 公共字段）。
// AbilityManager 路由时可根据 ServerName 非空判断为 MCP 工具，
// 也可根据 tool_call.Name 在注册表中查找 Tool 实例（与 Python 对齐）。
//
// 对应 Python: openjiuwen/core/foundation/tool/schema.py (McpToolInfo)
type McpToolInfo struct {
	ToolInfo
	// ServerName MCP 服务器名称，非空时表示 MCP 工具
	ServerName string `json:"server_name,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetToolInfo 实现 ToolInfoProvider 接口，返回自身。
func (t *ToolInfo) GetToolInfo() *ToolInfo { return t }

// GetToolInfo 实现 ToolInfoProvider 接口，返回内嵌的 ToolInfo。
func (m *McpToolInfo) GetToolInfo() *ToolInfo { return &m.ToolInfo }

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

// NewMcpToolInfo 创建 McpToolInfo 实例，Type 默认为 "function"。
//
// 对应 Python: McpToolCard.tool_info() -> McpToolInfo
func NewMcpToolInfo(name, description, serverName string, parameters map[string]any) *McpToolInfo {
	if parameters == nil {
		parameters = make(map[string]any)
	}
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
