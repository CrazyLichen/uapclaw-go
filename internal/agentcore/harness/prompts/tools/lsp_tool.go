package tools

// ──────────────────────────── 结构体 ────────────────────────────

// LspToolMetadataProvider lsp 工具元数据提供者
type LspToolMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// lspToolDescription lsp 工具双语描述
var lspToolDescription = map[string]string{
	"cn": `通过 Language Server Protocol (LSP) 服务器获取代码智能功能（如定义跳转、引用查找、诊断等）。

支持的操作：
- goToDefinition: 查找符号的定义位置
- findReferences: 查找符号的所有引用
- documentSymbol: 获取文档中的所有符号（函数、类、变量等）
- workspaceSymbol: 在整个工作区搜索符号
- goToImplementation: 查找接口或抽象方法的具体实现
- prepareCallHierarchy: 获取光标位置的调用层次结构条目
- incomingCalls: 查找所有调用当前函数的函数/方法
- outgoingCalls: 查找当前函数调用的所有函数/方法

注意：hover（悬停信息）操作暂不支持。

导航操作均需要 file_path、line 和 character 参数。
workspaceSymbol 不需要 line 和 character，而是使用 query 参数。

导航操作的结果会自动过滤掉位于 gitignored 目录（如 node_modules、__pycache__ 等）中的条目。

大文件（超过 10MB）不会被发送到 LSP 服务器。

注意：必须为文件类型配置对应的 LSP 服务器。如果没有可用的服务器，将返回错误。`,
	"en": `Interact with Language Server Protocol (LSP) servers to get code intelligence features.

Supported operations:
- goToDefinition: Find where a symbol is defined
- findReferences: Find all references to a symbol
- documentSymbol: Get all symbols (functions, classes, variables) in a document
- workspaceSymbol: Search for symbols across the entire workspace
- goToImplementation: Find implementations of an interface or abstract method
- prepareCallHierarchy: Get call hierarchy item at a position (functions/methods)
- incomingCalls: Find all functions/methods that call the function at a position
- outgoingCalls: Find all functions/methods called by the function at a position

Note: Hover (hover information) is not currently supported.

Navigation operations require file_path, line, and character.
workspaceSymbol uses query instead of line/character.

Results from gitignored files (node_modules, __pycache__, etc.) are automatically filtered out for navigation operations.

Large files (exceeding 10MB) are not sent to the LSP server.

Note: LSP servers must be configured for the file type. If no server is available, an error will be returned.`,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetLspToolMetadataProviderInputParams 构建 lsp 工具的参数 Schema
func GetLspToolMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"operation":           {"cn": "LSP 操作类型，可选值：goToDefinition、findReferences、documentSymbol、workspaceSymbol、goToImplementation、prepareCallHierarchy、incomingCalls、outgoingCalls", "en": "LSP operation type. Options: goToDefinition, findReferences, documentSymbol, workspaceSymbol, goToImplementation, prepareCallHierarchy, incomingCalls, outgoingCalls"},
		"file_path":           {"cn": "文件路径（绝对路径或相对于工作区根目录的路径）", "en": "The absolute or relative path to the file"},
		"line":                {"cn": "行号（1-indexed，编辑器中显示的行号）", "en": "The line number (1-based, as shown in editors)"},
		"character":           {"cn": "列号（1-indexed，默认为 1）", "en": "The character offset (1-based, as shown in editors; defaults to 1)"},
		"query":               {"cn": "搜索查询字符串；为空时返回所有可用符号（仅 workspaceSymbol 使用）", "en": "Search query string; when empty, returns all available symbols (used by workspaceSymbol only)"},
		"include_declaration": {"cn": "为 true 时，结果中包含符号的定义位置（默认 true）", "en": "When true, the declaration location itself is included in the results (default: true)"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation":           map[string]any{"type": "string", "enum": []any{"goToDefinition", "findReferences", "documentSymbol", "workspaceSymbol", "goToImplementation", "prepareCallHierarchy", "incomingCalls", "outgoingCalls"}, "description": d("operation")},
			"file_path":           map[string]any{"type": "string", "description": d("file_path")},
			"line":                map[string]any{"type": "integer", "minimum": 1, "description": d("line")},
			"character":           map[string]any{"type": "integer", "minimum": 1, "description": d("character")},
			"query":               map[string]any{"type": "string", "description": d("query")},
			"include_declaration": map[string]any{"type": "boolean", "description": d("include_declaration")},
		},
		"required": []any{"operation", "file_path"},
	}
}

func (p *LspToolMetadataProvider) GetName() string { return "lsp" }
func (p *LspToolMetadataProvider) GetDescription(language string) string {
	if d, ok := lspToolDescription[language]; ok {
		return d
	}
	return lspToolDescription["cn"]
}
func (p *LspToolMetadataProvider) GetInputParams(language string) map[string]any {
	return GetLspToolMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&LspToolMetadataProvider{}) }
