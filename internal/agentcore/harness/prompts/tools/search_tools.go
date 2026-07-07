package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// searchToolsDescription search_tools 工具双语描述
var searchToolsDescription = map[string]string{
	"cn": "根据能力、名称、描述或参数提示搜索候选工具。仅用于发现，不会直接调用工具。",
	"en": "Search candidate tools by capability, name, description, or parameter hints. Discovery only; tools are not directly callable.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SearchToolsMetadataProvider search_tools 工具元数据提供者
type SearchToolsMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSearchToolsMetadataProviderInputParams 构建 search_tools 工具的参数 Schema
func GetSearchToolsMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"query":        {"cn": "搜索候选工具的查询文本", "en": "Search query for finding relevant candidate tools"},
		"limit":        {"cn": "返回候选工具的最大数量", "en": "Maximum number of candidate tools to return"},
		"detail_level": {"cn": "1=name+描述, 2=+参数摘要, 3=+完整参数", "en": "1=name+description, 2=+parameter summary, 3=+full parameters"},
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
			"query":        map[string]any{"type": "string", "description": d("query")},
			"limit":        map[string]any{"type": "integer", "description": d("limit"), "default": 10},
			"detail_level": map[string]any{"type": "integer", "description": d("detail_level"), "default": 1},
		},
		"required": []any{"query"},
	}
}

func (p *SearchToolsMetadataProvider) GetName() string { return "search_tools" }
func (p *SearchToolsMetadataProvider) GetDescription(language string) string {
	if d, ok := searchToolsDescription[language]; ok {
		return d
	}
	return searchToolsDescription["cn"]
}
func (p *SearchToolsMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSearchToolsMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&SearchToolsMetadataProvider{}) }
