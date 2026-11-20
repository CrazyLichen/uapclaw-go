package schema

// ClientToolRequest 客户端工具调用中断请求。
// ability manager 对 ToolCard(execution="client") 的调用，在全部 before_tool_call
// rail 放行后抛出，携带最终参数交由连接的客户端执行；宿主以 ClientToolResult
// （或同形状的 string/dict）应答。
// Python: core/single_agent/interrupt/response.py:74-89 ClientToolRequest(InterruptRequest)
type ClientToolRequest struct {
	InterruptRequest
	// ToolName 工具名称
	// Python: interrupt_kind/owner 有字段默认值（client_tool/CLIENT_TOOL_INTERRUPT_OWNER），
	// Go 无字段默认值机制，构造器字面量赋值
	ToolName string `json:"tool_name"`
	// Arguments 最终工具参数（rail 改写后）；应答按这组参数 complete
	// Python: arguments: dict = {}
	Arguments map[string]any `json:"arguments"`
	// ClientRequest ClientTool.build_request 返回的内容（发给客户端的执行说明）
	// Python: client_request: dict = {}
	ClientRequest map[string]any `json:"client_request"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewClientToolRequest 构造客户端工具中断请求（kind/owner 固定）。
// Python: 字段默认值 interrupt_kind=InterruptKind.CLIENT_TOOL、
// interrupt_owner=CLIENT_TOOL_INTERRUPT_OWNER、
// message=f"Run `{tool_call.name}` on the client and send back its result."
// （ability_manager.py:1494-1500）
func NewClientToolRequest(toolName string, arguments, clientRequest map[string]any) *ClientToolRequest {
	return &ClientToolRequest{
		InterruptRequest: InterruptRequest{
			Message:        "Run `" + toolName + "` on the client and send back its result.",
			PayloadSchema:  ClientToolResultPayloadSchema(),
			InterruptKind:  InterruptKindClientTool,
			InterruptOwner: ClientToolInterruptOwner,
		},
		ToolName:      toolName,
		Arguments:     arguments,
		ClientRequest: clientRequest,
	}
}

// ClientToolResultPayloadSchema 返回 ClientToolResult 的 JSON Schema（宿主应答形状）。
// Python: ClientToolResult.model_json_schema()（ability_manager.py:1496）；
// Go 无 Pydantic 反射，按 client.py:62-78 字段手工展开等价 schema。
func ClientToolResultPayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status":   map[string]any{"type": "string", "enum": []any{"completed", "error", "timeout", "client_gone", "cancelled"}},
			"content":  map[string]any{},
			"is_error": map[string]any{"type": "boolean"},
			"data":     map[string]any{},
		},
	}
}
