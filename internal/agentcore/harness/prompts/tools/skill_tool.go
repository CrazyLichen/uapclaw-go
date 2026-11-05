package tools

// ──────────────────────────── 结构体 ────────────────────────────

// SkillToolMetadataProvider skill_tool 工具元数据提供者
type SkillToolMetadataProvider struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// skillToolDescription skill_tool 工具双语描述
var skillToolDescription = map[string]string{
	"cn": "使用此工具查看特定技能的内容。成功时默认附带技能根目录的 ASCII 目录树（directory_tree）" +
		"以及嵌套子技能相对路径列表（discovered_skill_names，含 SKILL.md 的子目录）；" +
		"加载子技能请再次调用本工具，并设置 relative_file_path（如 designer/SKILL.md）。",
	"en": "Use this tool to view the skill contents of a certain skill. " +
		"On success it always includes an ASCII directory_tree of the skill root " +
		"and discovered_skill_names (relative paths of nested dirs that contain SKILL.md). " +
		"To load a nested skill, call again with relative_file_path " +
		"(e.g. designer/SKILL.md).",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSkillToolMetadataProviderInputParams 构建 skill_tool 工具的参数 Schema
func GetSkillToolMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"skill_name":         {"cn": "技能的名称", "en": "Name of the skill"},
		"relative_file_path": {"cn": "可选。查看技能目录中指定路径（relative_file_path）下的特定文件。留空则查看主 SKILL.md 文件。", "en": "Optional. Views a specific file within the skill directory at the relative_file_path. Leave blank to view the main SKILL.md file."},
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
			"skill_name":         map[string]any{"type": "string", "description": d("skill_name")},
			"relative_file_path": map[string]any{"type": "string", "description": d("relative_file_path")},
		},
		"required": []any{"skill_name"},
	}
}

func (p *SkillToolMetadataProvider) GetName() string { return "skill_tool" }
func (p *SkillToolMetadataProvider) GetDescription(language string) string {
	if d, ok := skillToolDescription[language]; ok {
		return d
	}
	return skillToolDescription["cn"]
}
func (p *SkillToolMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSkillToolMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&SkillToolMetadataProvider{}) }
