package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallMetadataProvider tool_call 工具元数据提供者。
// Python: openjiuwen/harness/prompts/tools/tool_call.py (ToolCallMetadataProvider)
type ToolCallMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// toolCallDescription tool_call 工具双语描述
var toolCallDescription = map[string]string{
	"cn": "执行 tool_search 返回的 deferred 工具。" +
		"name 必须使用最近一次 tool_search 返回的准确工具名称，" +
		"args 必须符合该结果中的完整 parameters schema。" +
		"搜索结果工具不会加入顶层 tools，不能直接调用搜索结果中的工具名称，也不能猜测工具名称。" +
		"工具未变化时可以复用之前的搜索结果；如果工具已修改，应重新搜索获取最新 schema；" +
		"如果当前目录后来显示该工具已删除，之前的搜索结果和授权立即失效，" +
		"不能继续调用，也不能改用 task_tool 或子代理间接调用。",
	"en": "Execute a deferred tool returned by tool_search. " +
		"The name must exactly match a tool name returned by the latest tool_search, " +
		"and args must conform to that result's complete parameters schema. " +
		"Search-result tools are not added to the top-level tools list; do not call a result tool by its own name " +
		"or guess a tool name. An unchanged tool may reuse a previous result; if a later " +
		"directory update changes its schema, search again, and if it marks the tool as " +
		"removed, the previous result and authorization are invalid; do not invoke it " +
		"through task_tool or a subagent.",
}

// toolCallParams tool_call 参数描述
var toolCallParams = map[string]map[string]string{
	"name": {
		"cn": "tool_search 返回的准确工具名称",
		"en": "Exact tool name returned by tool_search",
	},
	"args": {
		"cn": "按照 tool_search 返回的完整 parameters schema 填写工具参数；无参数工具使用空对象",
		"en": "Arguments matching the complete parameters schema returned by tool_search; " +
			"use an empty object for no-argument tools",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetToolCallInputParams 构建 tool_call 工具的参数 Schema。
// Python: get_tool_call_input_params
func GetToolCallInputParams(language string) map[string]any {
	lang := normalizeToolLang(language)
	describe := i18nPicker(toolCallParams, lang)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": describe("name")},
			"args": map[string]any{
				"type":                 "object",
				"description":          describe("args"),
				"additionalProperties": true,
			},
		},
		"required":             []any{"name", "args"},
		"additionalProperties": false,
	}
}

// GetName 返回工具注册名称。
// Python: ToolCallMetadataProvider.get_name
func (p *ToolCallMetadataProvider) GetName() string { return "tool_call" }

// GetDescription 返回指定语言的工具描述。
// Python: ToolCallMetadataProvider.get_description
func (p *ToolCallMetadataProvider) GetDescription(language string) string {
	if description, ok := toolCallDescription[language]; ok {
		return description
	}
	return toolCallDescription["cn"]
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: ToolCallMetadataProvider.get_input_params
func (p *ToolCallMetadataProvider) GetInputParams(language string) map[string]any {
	return GetToolCallInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 tool_call 元数据提供者。
func init() { RegisterToolProvider(&ToolCallMetadataProvider{}) }
