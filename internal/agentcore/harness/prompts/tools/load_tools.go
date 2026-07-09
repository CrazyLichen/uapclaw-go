package tools

// ──────────────────────────── 结构体 ────────────────────────────

// LoadToolsMetadataProvider load_tools 工具元数据提供者
type LoadToolsMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// loadToolsDescription load_tools 工具双语描述
var loadToolsDescription = map[string]string{
	"cn": "将选定的真实工具加载到当前 session 可见工具集合中。",
	"en": "Load selected real tools into the current session-visible tool set.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetLoadToolsMetadataProviderInputParams 构建 load_tools 工具的参数 Schema
func GetLoadToolsMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"tool_names": {"cn": "要在当前 session 中可见的工具名称列表", "en": "Names of tools to make visible for the current session"},
		"replace":    {"cn": "如果为 true，替换当前可见工具集，否则合并", "en": "If true, replace the current visible tool set instead of merging"},
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
			"tool_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": d("tool_names")},
			"replace":    map[string]any{"type": "boolean", "description": d("replace")},
		},
		"required": []any{"tool_names"},
	}
}

func (p *LoadToolsMetadataProvider) GetName() string { return "load_tools" }
func (p *LoadToolsMetadataProvider) GetDescription(language string) string {
	if d, ok := loadToolsDescription[language]; ok {
		return d
	}
	return loadToolsDescription["cn"]
}
func (p *LoadToolsMetadataProvider) GetInputParams(language string) map[string]any {
	return GetLoadToolsMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&LoadToolsMetadataProvider{}) }
