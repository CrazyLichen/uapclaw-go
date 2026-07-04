package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// listSkillDescription list_skill 工具双语描述
var listSkillDescription = map[string]string{
	"cn": "列出可用技能或为当前任务选择相关技能。",
	"en": "List available skills or select relevant skills for the current task.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// ListSkillMetadataProvider list_skill 工具元数据提供者
type ListSkillMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetListSkillMetadataProviderInputParams 构建 list_skill 工具的参数 Schema
func GetListSkillMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"query": {"cn": "可选。当前用户任务。为空时返回所有可用技能。", "en": "Optional. Current user task. If empty, return all available skills."},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": d("query")},
		},
		"required": []any{},
	}
}

func (p *ListSkillMetadataProvider) GetName() string { return "list_skill" }
func (p *ListSkillMetadataProvider) GetDescription(language string) string {
	if d, ok := listSkillDescription[language]; ok { return d }
	return listSkillDescription["cn"]
}
func (p *ListSkillMetadataProvider) GetInputParams(language string) map[string]any {
	return GetListSkillMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&ListSkillMetadataProvider{}) }
