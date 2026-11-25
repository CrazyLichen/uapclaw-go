// Package prompts_tool 提供演进工具的 agent 侧元数据（六工具双语卡与 subject schema）。
//
// Python 真相源：openjiuwen/agent_evolving/prompts/tools/evolution.py（439 行）
// + __init__.py（29 行导出清单）。六个 ToolMetadataProvider 注册进
// harness/prompts/tools 全局注册表，经 BuildToolCard 统一建卡——
// 与 Python 经 harness.prompts.tools.base.ToolMetadataProvider 注册同构。
//
// 文件目录：
//
//	tools/
//	├── doc.go         # 包文档
//	└── evolution.go  # 六个元数据提供者 + subject schema + 注册段
//
// 对应 Python 代码：openjiuwen/agent_evolving/prompts/tools/
package prompts_tool

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
	harnessprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/tools"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// supportedKindEnum subject kind 可选值。
	// Python: _SUPPORTED_KIND_ENUM (evolution.py:27)。
	supportedKindEnum = []string{"skill", "swarm-skill"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EvolutionText 按 language 选择 cn/en 文案。
// Python: _text (evolution.py:19-20)。
func EvolutionText(language string, cn string, en string) string {
	if language == "en" {
		return en
	}
	return cn
}

// sortedTargetValues target 可选值（派生自 schema，防漂移）。
// Python: _TARGET_VALUES = list(EVOLUTION_TARGET_VALUES) (evolution.py:15)。
func sortedTargetValues() []string {
	return append([]string{}, schema.EvolutionTargetValues...)
}

// sortedSectionValues 排序后的 section 可选值（派生自 schema，防漂移）。
// Python: _SECTION_VALUES = sorted(VALID_SECTIONS) (evolution.py:14)。
func sortedSectionValues() []string { return SortedValidSections() }

// SortedValidSections 返回排序后的 section 全集（测试对拍用）。
// Python: sorted(VALID_SECTIONS)。
func SortedValidSections() []string {
	keys := make([]string, 0, len(schema.ValidSections))
	for k := range schema.ValidSections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// BuildEvolutionSubjectSchema 构建演进 subject 的 JSON schema。
// Python: build_evolution_subject_schema / _subject_schema (evolution.py:30-57)。
func BuildEvolutionSubjectSchema(language string) map[string]any {
	kindDescription := EvolutionText(language,
		"演进对象类型。可选值：skill（常规技能）、swarm-skill（多智能体团队技能）。",
		"Evolution subject kind. Allowed values: skill, swarm-skill.")
	nameDescription := EvolutionText(language, "演进对象名称。", "Evolution subject name.")
	return map[string]any{
		"type":        "object",
		"description": EvolutionText(language, "演进目标对象。", "Evolution target object."),
		"properties": map[string]any{
			"kind": map[string]any{
				"type":        "string",
				"enum":        append([]string{}, supportedKindEnum...),
				"description": kindDescription,
			},
			"name": map[string]any{
				"type":        "string",
				"description": nameDescription,
			},
		},
		"required": []string{"kind", "name"},
	}
}

// GetEvolutionToolDescription 查询演进工具描述。
// Python: get_evolution_tool_description (evolution.py:395-400)。
func GetEvolutionToolDescription(name string, language string) (string, error) {
	provider, ok := harnessprompts.GetToolProvider(name)
	if !ok {
		return "", fmt.Errorf("Evolution tool '%s' not registered", name)
	}
	return provider.GetDescription(language), nil
}

// GetEvolutionToolInputParams 查询演进工具入参 schema。
// Python: get_evolution_tool_input_params (evolution.py:403-408)。
func GetEvolutionToolInputParams(name string, language string) (map[string]any, error) {
	provider, ok := harnessprompts.GetToolProvider(name)
	if !ok {
		return nil, fmt.Errorf("Evolution tool '%s' not registered", name)
	}
	return provider.GetInputParams(language), nil
}

// registerEvolutionProviders 注册六个演进工具元数据提供者。
// Python: _PROVIDERS/_REGISTRY (evolution.py:384-392)——注册进 harness 全局注册表。
func registerEvolutionProviders() {
	harnessprompts.RegisterToolProvider(&PrepareSkillEvolutionReviewMetadataProvider{})
	harnessprompts.RegisterToolProvider(&EvolveReviewTaskMetadataProvider{})
	harnessprompts.RegisterToolProvider(&ListSkillExperiencesMetadataProvider{})
	harnessprompts.RegisterToolProvider(&ReadSkillExperiencesMetadataProvider{})
	harnessprompts.RegisterToolProvider(&EvolveSkillExperiencesMetadataProvider{})
	harnessprompts.RegisterToolProvider(&SimplifySkillExperiencesMetadataProvider{})
}

func init() {
	registerEvolutionProviders()
}

// joinValues 逗号连接取值列表。
// Python: _values (evolution.py:23-24)。
func joinValues(values []string) string {
	return strings.Join(values, ", ")
}

// ──────────────────────────── 结构体 ────────────────────────────

// PrepareSkillEvolutionReviewMetadataProvider 准备演进审查的元数据。
// Python: PrepareSkillEvolutionReviewMetadataProvider (evolution.py:60-101)。
type PrepareSkillEvolutionReviewMetadataProvider struct{}

// GetName 返回工具注册名称。
func (p *PrepareSkillEvolutionReviewMetadataProvider) GetName() string {
	return "prepare_skill_evolution"
}

// GetDescription 返回指定语言的工具描述。
// Python: get_description (evolution.py:66-76)。
func (p *PrepareSkillEvolutionReviewMetadataProvider) GetDescription(language string) string {
	if language == "en" {
		return ("Create a Skill evolution review ref before drafting new experiences. " +
			"Use only after explicit user confirmation, pass a subject object, and set user_confirmed=true. " +
			"The returned ref is required by the review and mutation tools.")
	}
	return ("在起草新经验前创建技能演进 review ref；仅在用户明确确认后调用，必须提供 subject 对象并设置 " +
		"user_confirmed=true。返回的 ref 供后续 review 和变更工具使用。")
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: get_input_params (evolution.py:78-101)。
func (p *PrepareSkillEvolutionReviewMetadataProvider) GetInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": BuildEvolutionSubjectSchema(language),
			"user_intent": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "用户已确认的演进意图或反馈摘要。", "User-approved evolution intent or feedback summary."),
			},
			"user_confirmed": map[string]any{
				"type":        "boolean",
				"description": EvolutionText(language, "用户明确同意演进该 skill 后必须为 true。", "Must be true after the user explicitly agrees to evolve this skill."),
			},
		},
		"required": []string{"subject", "user_confirmed"},
	}
}

// EvolveReviewTaskMetadataProvider 拉起演进审查子代理的元数据。
// Python: EvolveReviewTaskMetadataProvider (evolution.py:104-145)。
type EvolveReviewTaskMetadataProvider struct{}

// GetName 返回工具注册名称。
func (p *EvolveReviewTaskMetadataProvider) GetName() string { return "evolve_review_task" }

// GetDescription 返回指定语言的工具描述。
// Python: get_description (evolution.py:110-120)。
func (p *EvolveReviewTaskMetadataProvider) GetDescription(language string) string {
	if language == "en" {
		return ("Launch the restricted evolution reviewer for a prepared evolution_review_ref. " +
			"Use the ref returned by prepare_skill_evolution and keep subject consistent when supplied; " +
			"submit proposals only through the review result.")
	}
	return ("为已准备的 evolution_review_ref 启动受限演进 reviewer；使用 prepare_skill_evolution 返回的 ref，" +
		"如提供 subject 必须保持一致，proposal 只能来自 review 结果。")
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: get_input_params (evolution.py:122-145)。
func (p *EvolveReviewTaskMetadataProvider) GetInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"evolution_review_ref": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "prepare_skill_evolution 返回的 evolution_review_ref。", "Evolution review ref returned by prepare_skill_evolution."),
			},
			"user_intent": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "可选；用户已确认的演进意图或反馈摘要。", "Optional user-approved evolution intent or feedback summary."),
			},
			"subject": BuildEvolutionSubjectSchema(language),
		},
		"required": []string{"evolution_review_ref"},
	}
}

// ListSkillExperiencesMetadataProvider 经验索引列表的元数据。
// Python: ListSkillExperiencesMetadataProvider (evolution.py:148-218)。
type ListSkillExperiencesMetadataProvider struct{}

// GetName 返回工具注册名称。
func (p *ListSkillExperiencesMetadataProvider) GetName() string { return "list_skill_experiences" }

// GetDescription 返回指定语言的工具描述。
// Python: get_description (evolution.py:154-160)。
func (p *ListSkillExperiencesMetadataProvider) GetDescription(language string) string {
	if language == "en" {
		return ("Query persisted evolution experience index entries for focused lookup or overflow coverage. " +
			"Returns metadata only, not full content.")
	}
	return "查询已持久化的演进经验索引，用于聚焦查找或覆盖溢出索引；只返回元数据，不返回全文。"
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: get_input_params (evolution.py:162-218)。
func (p *ListSkillExperiencesMetadataProvider) GetInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": BuildEvolutionSubjectSchema(language),
			"min_score": map[string]any{
				"type":        "number",
				"description": EvolutionText(language, "最低分数过滤。", "Minimum score filter."),
			},
			"limit": map[string]any{
				"type":        "integer",
				"default":     20,
				"description": EvolutionText(language, "最多返回的索引条目数。", "Maximum number of index items."),
			},
			"cursor": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "上一页溢出响应返回的游标。", "Cursor returned by a previous overflow response."),
			},
			"target": map[string]any{
				"type":        "string",
				"enum":        sortedTargetValues(),
				"description": EvolutionText(language, fmt.Sprintf("结构化 target 过滤；可选值：%s。", joinValues(sortedTargetValues())), fmt.Sprintf("Structured target filter. Allowed values: %s.", joinValues(sortedTargetValues()))),
			},
			"section": map[string]any{
				"type":        "string",
				"enum":        sortedSectionValues(),
				"description": EvolutionText(language, fmt.Sprintf("结构化 section 过滤；可选值：%s。", joinValues(sortedSectionValues())), fmt.Sprintf("Structured section filter. Allowed values: %s.", joinValues(sortedSectionValues()))),
			},
			"query": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "仅在索引字段上匹配的字面量 OR 查询；多个词用 | 分隔，不是自然语言查询。", "Literal OR terms separated by | over index fields only; not natural language."),
			},
			"sort": map[string]any{
				"type":        "string",
				"enum":        []string{"score_desc", "updated_desc"},
				"default":     "score_desc",
				"description": EvolutionText(language, "索引排序方式。", "Index sort order."),
			},
		},
		"required": []string{"subject"},
	}
}

// ReadSkillExperiencesMetadataProvider 经验全文读取的元数据。
// Python: ReadSkillExperiencesMetadataProvider (evolution.py:221-253)。
type ReadSkillExperiencesMetadataProvider struct{}

// GetName 返回工具注册名称。
func (p *ReadSkillExperiencesMetadataProvider) GetName() string { return "read_skill_experiences" }

// GetDescription 返回指定语言的工具描述。
// Python: get_description (evolution.py:227-230)。
func (p *ReadSkillExperiencesMetadataProvider) GetDescription(language string) string {
	if language == "en" {
		return "Read full content for selected evolution experience records."
	}
	return "读取指定演进经验记录的全文内容。"
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: get_input_params (evolution.py:232-253)。
func (p *ReadSkillExperiencesMetadataProvider) GetInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": BuildEvolutionSubjectSchema(language),
			"record_ids": map[string]any{
				"type":        "array",
				"description": EvolutionText(language, "要读取的经验记录 ID。", "Experience record IDs to read."),
				"items":       map[string]any{"type": "string"},
			},
			"max_content_chars": map[string]any{
				"type":        "integer",
				"default":     2000,
				"description": EvolutionText(language, "每条记录最多返回的内容字符数。", "Maximum content characters per record."),
			},
		},
		"required": []string{"subject", "record_ids"},
	}
}

// EvolveSkillExperiencesMetadataProvider 接受审查 proposals 的元数据。
// Python: EvolveSkillExperiencesMetadataProvider (evolution.py:256-309)。
type EvolveSkillExperiencesMetadataProvider struct{}

// GetName 返回工具注册名称。
func (p *EvolveSkillExperiencesMetadataProvider) GetName() string { return "evolve_skill_experiences" }

// GetDescription 返回指定语言的工具描述。
// Python: get_description (evolution.py:262-272)。
func (p *EvolveSkillExperiencesMetadataProvider) GetDescription(language string) string {
	if language == "en" {
		return ("Accept reviewed Skill evolution proposals from a completed evolution review. " +
			"Pass the same subject, the completed review ref, and proposal IDs exactly as returned by the review; " +
			"do not rewrite proposal content. Runtime validation checks the ref, subject, IDs, and drafts.")
	}
	return ("接受已完成 evolution review 中的已审查 Skill 演进 proposals；subject、review ref 和 proposal ID 必须与 " +
		"review 结果一致，不要改写 proposal 内容。运行时会校验 ref、对象、ID 及草稿。")
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: get_input_params (evolution.py:274-309)。
func (p *EvolveSkillExperiencesMetadataProvider) GetInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": BuildEvolutionSubjectSchema(language),
			"evolution_review_ref": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "包含已审查 proposals 的已完成 Evolution Review Ref。", "Completed Evolution Review Ref containing reviewed proposals."),
			},
			"selected_proposal_ids": map[string]any{
				"type":        "array",
				"description": EvolutionText(language, "从已完成 review result 中选择要接受的 proposal_id 列表；不要复制或改写 proposal 正文。", "Proposal ids selected from the completed review result; do not copy or rewrite proposal content."),
				"items":       map[string]any{"type": "string"},
			},
			"selection_reason": map[string]any{
				"type":        "string",
				"description": EvolutionText(language, "可选；说明为什么接受这些 proposals，仅用于审计和审批摘要。", "Optional rationale for accepting these proposals, used for audit and approval summary."),
			},
		},
		"required": []string{"subject", "evolution_review_ref", "selected_proposal_ids"},
	}
}

// SimplifySkillExperiencesMetadataProvider 精简动作的元数据。
// Python: SimplifySkillExperiencesMetadataProvider (evolution.py:312-381)。
type SimplifySkillExperiencesMetadataProvider struct{}

// GetName 返回工具注册名称。
func (p *SimplifySkillExperiencesMetadataProvider) GetName() string {
	return "simplify_skill_experiences"
}

// GetDescription 返回指定语言的工具描述。
// Python: get_description (evolution.py:318-328)。
func (p *SimplifySkillExperiencesMetadataProvider) GetDescription(language string) string {
	if language == "en" {
		return ("Apply validated delete, merge, refine, or keep actions to existing evolution experiences. " +
			"Pass a subject and a non-empty actions array matching the schema; record references and action " +
			"fields are validated by the tool even when no interrupt rail is installed.")
	}
	return ("对已有演进经验执行经过校验的删除、合并、改写或保留动作；必须提供 subject 和非空 actions 数组并符合 " +
		"schema。即使未安装 Interrupt rail，工具自身也会校验记录引用和动作字段并安全失败。")
}

// GetInputParams 返回指定语言的参数 JSON Schema。
// Python: get_input_params (evolution.py:330-381)。
func (p *SimplifySkillExperiencesMetadataProvider) GetInputParams(language string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subject": BuildEvolutionSubjectSchema(language),
			"actions": map[string]any{
				"type":        "array",
				"description": EvolutionText(language, "要执行的精简动作。", "Simplify actions to apply."),
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"action": map[string]any{
							"type":        "string",
							"enum":        schema.SimplifyActionValues,
							"description": EvolutionText(language, fmt.Sprintf("动作类型；可选值：%s。", joinValues(schema.SimplifyActionValues)), fmt.Sprintf("Action type. Allowed values: %s.", joinValues(schema.SimplifyActionValues))),
						},
						"record_id": map[string]any{
							"type":        "string",
							"description": EvolutionText(language, "主记录 ID。", "Primary record ID."),
						},
						"merge_remove_ids": map[string]any{
							"type":        "array",
							"description": EvolutionText(language, "MERGE 时被移除的记录 ID。", "Record IDs removed during MERGE."),
							"items":       map[string]any{"type": "string"},
						},
						"new_content": map[string]any{
							"type":        "string",
							"description": EvolutionText(language, "MERGE 或 REFINE 使用的替换内容。", "Replacement content for MERGE or REFINE."),
						},
						"reason": map[string]any{
							"type":        "string",
							"description": EvolutionText(language, "执行该动作的原因。", "Reason for the action."),
						},
					},
					"required": []string{"action", "record_id"},
				},
			},
		},
		"required": []string{"subject", "actions"},
	}
}
