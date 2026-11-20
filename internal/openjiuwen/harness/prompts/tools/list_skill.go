package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ListSkillMetadataProvider list_skill 工具元数据提供者
type ListSkillMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// listSkillDescription list_skill 工具双语描述
var listSkillDescription = map[string]string{
	"cn": "列出可用技能或为当前任务选择相关技能。",
	"en": "List available skills or select relevant skills for the current task.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetListSkillMetadataProviderInputParams 构建 list_skill 工具的参数 Schema
func GetListSkillMetadataProviderInputParams(language string) map[string]any {
	lang := normalizeToolLang(language)
	p := map[string]map[string]string{
		"query": {"cn": "可选。当前用户任务。为空时返回所有可用技能。", "en": "Optional. Current user task. If empty, return all available skills."},
	}
	d := i18nPicker(p, lang)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": d("query")},
		},
		"required": []any{},
	}
}

// GetName 返回工具名称 "list_skill"。
func (p *ListSkillMetadataProvider) GetName() string { return "list_skill" }

// GetDescription 返回 "list_skill" 工具的描述文本，按 language 选择中文或英文，未命中时用中文。
func (p *ListSkillMetadataProvider) GetDescription(language string) string {
	if d, ok := listSkillDescription[language]; ok {
		return d
	}
	return listSkillDescription["cn"]
}

// GetInputParams 返回 "list_skill" 工具的参数 Schema。
func (p *ListSkillMetadataProvider) GetInputParams(language string) map[string]any {
	return GetListSkillMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&ListSkillMetadataProvider{}) }
