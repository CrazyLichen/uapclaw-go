package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ToolSearchMetadataProvider tool_search 工具元数据提供者。
//
// Python: openjiuwen/harness/prompts/tools/tool_search.py (ToolSearchMetadataProvider)
type ToolSearchMetadataProvider struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// toolSearchDescription tool_search 工具双语描述
var toolSearchDescription = map[string]string{
	"cn": "根据用户意图搜索已注册且可用的 deferred 工具。" +
		"当 direct 工具不能明确支持用户所需能力，或者你不确定应该使用哪个工具时，调用本工具。" +
		"请使用用户意图和所需能力描述进行搜索，不要猜测工具名称。" +
		"搜索使用 BM25 匹配工具名称、描述和参数信息。" +
		"返回结果包含匹配工具的名称、描述和完整 parameters schema。" +
		"搜索结果不会加入顶层 tools；要执行搜索结果，必须下一轮调用固定的 tool_call，" +
		"并使用返回的准确工具名称和参数。" +
		"只返回当前仍注册且可用的 deferred 工具。当前目录和当前注册表是判断当前能力的唯一依据；" +
		"历史消息、历史成功结果，或其它工具/子代理描述中的名称不能恢复当前已删除或已修改的工具。" +
		"工具未变化时，可以继续复用之前的搜索结果；如果工具描述或 schema 发生变化，应重新搜索；" +
		"如果最新工具目录显示某工具已删除，不得使用该工具的旧搜索结果或旧授权，" +
		"也不得通过 task_tool 或子代理间接调用。" +
		"对于同一用户意图，不要重复调用相同或相近的搜索；如果返回结果不适用，不要猜测工具名称。",
	"en": "Search registered and available deferred tools based on the user's intent. " +
		"Call this tool when no direct tool clearly supports the required capability " +
		"or when you are unsure which tool to use. " +
		"Search using the user's intent and required capability; do not guess tool names. " +
		"The search uses BM25 to match tool names, descriptions, and parameter information. " +
		"The results contain each matched tool's name, description, and complete parameters schema. " +
		"Search-result tools are not added to the top-level tools list; execute a result in the next turn " +
		"by calling the fixed tool_call with the exact returned name and schema-compatible arguments. " +
		"Only currently registered and available deferred tools are returned. The current " +
		"directory and registry are the only sources of truth; historical messages, " +
		"successful results, or names in another tool/subagent description cannot restore " +
		"a currently removed or changed tool. " +
		"A previous search result may be reused while its tool is unchanged; if its " +
		"description or schema changed, search again. If the latest directory says a tool " +
		"was removed, do not reuse its old result or authorization, and do not invoke it " +
		"through task_tool or a subagent. " +
		"Do not repeat the same or a similar search for the same user intent; " +
		"if the results are not suitable, do not guess a tool name.",
}

// toolSearchParams tool_search 参数描述
var toolSearchParams = map[string]map[string]string{
	"query": {
		"cn": "用用户意图和所需能力描述查询，不要猜测 deferred 工具名称",
		"en": "Describe the user's intent and required capability; do not guess deferred tool names",
	},
	"limit": {
		"cn": "最多返回的工具数量",
		"en": "Maximum number of tools to return",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetToolSearchInputParams 构建 tool_search 工具的参数 Schema。
//
// Python: get_tool_search_input_params
func GetToolSearchInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	describe := func(key string) string {
		if value, ok := toolSearchParams[key][lang]; ok {
			return value
		}
		return toolSearchParams[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": describe("query")},
			"limit": map[string]any{
				"type":        "integer",
				"description": describe("limit"),
				"default":     5,
			},
		},
		"required":             []any{"query"},
		"additionalProperties": false,
	}
}

// GetName 返回工具注册名称。
//
// Python: ToolSearchMetadataProvider.get_name
func (p *ToolSearchMetadataProvider) GetName() string { return "tool_search" }

// GetDescription 返回指定语言的工具描述。
//
// Python: ToolSearchMetadataProvider.get_description
func (p *ToolSearchMetadataProvider) GetDescription(language string) string {
	if description, ok := toolSearchDescription[language]; ok {
		return description
	}
	return toolSearchDescription["cn"]
}

// GetInputParams 返回指定语言的参数 JSON Schema。
//
// Python: ToolSearchMetadataProvider.get_input_params
func (p *ToolSearchMetadataProvider) GetInputParams(language string) map[string]any {
	return GetToolSearchInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 tool_search 元数据提供者。
func init() { RegisterToolProvider(&ToolSearchMetadataProvider{}) }
