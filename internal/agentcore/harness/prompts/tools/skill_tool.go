package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// skillToolDescription skill_tool 工具双语描述
var skillToolDescription = map[string]string{
	"cn": "使用此工具查看特定技能的内容",
	"en": "Use this tool to view the skill contents of a certain skill",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SkillToolMetadataProvider skill_tool 工具元数据提供者
type SkillToolMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSkillToolMetadataProviderInputParams 构建 skill_tool 工具的参数 Schema
func GetSkillToolMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"skill_name":          {"cn": "技能的名称", "en": "Name of the skill"},
		"relative_file_path":  {"cn": "可选。查看技能目录中指定路径（relative_file_path）下的特定文件。留空则查看主 SKILL.md 文件。", "en": "Optional. Views a specific file within the skill directory at the relative_file_path. Leave blank to view the main SKILL.md file."},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_name":         map[string]any{"type": "string", "description": d("skill_name")},
			"relative_file_path": map[string]any{"type": "string", "description": d("relative_file_path")},
		},
		"required": []any{"skill_name"},
	}
}

func (p *SkillToolMetadataProvider) GetName() string { return "skill_tool" }
func (p *SkillToolMetadataProvider) GetDescription(language string) string {
	if d, ok := skillToolDescription[language]; ok { return d }
	return skillToolDescription["cn"]
}
func (p *SkillToolMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSkillToolMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&SkillToolMetadataProvider{}) }
