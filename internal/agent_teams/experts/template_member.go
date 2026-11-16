package experts

import (
	"path"

	hres "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// defaultLanguage persona 渲染默认语言。
// Python: template_member.py:34 _DEFAULT_LANGUAGE
const defaultLanguage = "cn"

// ──────────────────────────── 结构体 ────────────────────────────

// TemplateMember 一个模板装配成的 Team 成员。
// Python: template_member.py:99-106 TemplateMember（dataclass frozen，D-2：不可变靠约定）
type TemplateMember struct {
	// Spec base spec + 能力快照
	Spec hschema.DeepAgentSpec
	// Persona 成员私有 prompt 的渲染产物
	Persona string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderTemplatePersona 把模板 prompt_sections 渲染拍平成一段人格文本。
// 段落按 priority 排序；每段以 language 渲染（{{ language }}/{{ workspace }} 加
// 段自身 render_params），strip 后为空的段落丢弃；其余用空行拼接。
// Python: template_member.py:37-66 render_template_persona
func RenderTemplatePersona(template hschema.AgentTemplateSpec, language string, workspace string) string {
	resolved := strings.TrimSpace(language)
	if resolved == "" {
		resolved = defaultLanguage
	}
	baseParams := map[string]any{
		"language":  resolved,
		"workspace": workspace,
	}
	// Python: sorted(template.prompt_sections, key=lambda item: item.priority)（稳定排序）
	sections := append([]hschema.PromptSectionSpec(nil), template.PromptSections...)
	stableSortSectionsByPriority(sections)
	var rendered []string
	for _, section := range sections {
		params := map[string]any{}
		for k, v := range baseParams {
			params[k] = v
		}
		// Python: params = {**base_params, **dict(section.render_params or {})} —— 段参数覆盖基础值
		for k, v := range section.RenderParams {
			params[k] = v
		}
		text := strings.TrimSpace(hres.RenderPromptContent(section.Content, resolved, params))
		if text != "" {
			rendered = append(rendered, text)
		}
	}
	// Python: "\n\n".join(rendered)
	return strings.Join(rendered, "\n\n")
}

// TemplateSnapshot 能力快照：去掉 prompt_sections 的模板，JSON 数据形态。
// Python: template_member.py:69-71 template_snapshot
// （model_copy(update={"prompt_sections": []}).model_dump(mode="json")；
// pydantic 默认列表序列化保留键为 []，故 Go 侧 dump 后补键）
func TemplateSnapshot(template hschema.AgentTemplateSpec) map[string]any {
	template.PromptSections = nil
	dumped := template.ModelDump()
	dumped["prompt_sections"] = []any{}
	return dumped
}

// TemplateSkillNames 快照的技能目录名，保序去重。
// Python: template_member.py:74-82 template_skill_names
func TemplateSkillNames(snapshot map[string]any) []string {
	var names []string
	rawSkills, _ := snapshot["skills"].([]any)
	for _, item := range rawSkills {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		dir, _ := m["dir"].(string)
		name := ""
		if dir != "" {
			// Python: PurePath(raw).name —— 取路径基名（D-4：path.Base 等价）
			name = path.Base(dir)
		}
		if name != "" && !containsString(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// ApplyTemplateSnapshot 把能力快照挂到 base spec 并种子 Skill 可见性。
// 返回 base 的拷贝：agent_template_spec=snapshot，skills = base.skills 后接快照
// 技能目录名（保序去重）。
// Python: template_member.py:85-96 apply_template_snapshot
func ApplyTemplateSnapshot(baseSpec hschema.DeepAgentSpec, snapshot map[string]any) hschema.DeepAgentSpec {
	skills := append([]string(nil), baseSpec.Skills...)
	for _, name := range TemplateSkillNames(snapshot) {
		if name != "" && !containsString(skills, name) {
			skills = append(skills, name)
		}
	}
	baseSpec.AgentTemplateSpec = snapshot
	baseSpec.Skills = skills
	return baseSpec
}

// ComposeTemplateMember 在 base spec 上装配模板（persona + 能力快照）。
// language 取 baseSpec.Language；workspace 取 baseSpec.workspace 根路径。
// Python: template_member.py:109-130 compose_template_member
func ComposeTemplateMember(baseSpec hschema.DeepAgentSpec, template hschema.AgentTemplateSpec, language string) TemplateMember {
	var workspace string
	if baseSpec.Workspace != nil {
		workspace = baseSpec.Workspace.RootPath
	}
	lang := language
	if lang == "" {
		lang = baseSpec.Language
	}
	return TemplateMember{
		Spec:    ApplyTemplateSnapshot(baseSpec, TemplateSnapshot(template)),
		Persona: RenderTemplatePersona(template, lang, workspace),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stableSortSectionsByPriority 按 priority 升序稳定排序（对齐 Python sorted 稳定性）。
func stableSortSectionsByPriority(sections []hschema.PromptSectionSpec) {
	for i := 1; i < len(sections); i++ { // 插入排序：稳定
		for j := i; j > 0 && sections[j].Priority < sections[j-1].Priority; j-- {
			sections[j], sections[j-1] = sections[j-1], sections[j]
		}
	}
}

// containsString 列表包含判断。
func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
