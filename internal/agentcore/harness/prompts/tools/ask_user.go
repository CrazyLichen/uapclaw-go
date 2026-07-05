package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// askUserDescription ask_user 工具双语描述
var askUserDescription = map[string]string{
	"cn": `向用户提问以收集信息、澄清歧义或做出决策。支持1-4个问题，每个问题2-4个选项。

何时主动使用：需求模糊、多种方案可选、涉及用户偏好时，应主动询问而非假设。

【禁止】选项中添加'其他'、'自定义'等兜底选项，系统已自动提供。
【推荐】将推荐选项放第一位，label末尾加'（推荐）'。
preview字段仅用于单选问题的视觉比较场景。`,
	"en": `Ask user questions to gather info, clarify ambiguity, or make decisions. Supports 1-4 questions, each with 2-4 options.

When to use proactively: Ask when requirements are vague, multiple approaches exist, or user preferences matter. Don't assume.

FORBIDDEN: Adding 'Other', 'Custom' etc. as options — system provides this automatically.
RECOMMENDED: Place recommended option first, append '(Recommended)' to its label.
Preview field is only for single-select questions with visual comparison needs.`,
}

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserMetadataProvider ask_user 工具元数据提供者
type AskUserMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetAskUserMetadataProviderInputParams 构建 ask_user 工具的参数 Schema
func GetAskUserMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"questions":           {"cn": "向用户提出的问题列表（1-4个）", "en": "Questions to ask the user (1-4 questions)"},
		"header":              {"cn": "问题的简短标题或标签", "en": "A short label or tag for the question (max 12 chars)"},
		"question":            {"cn": "完整的问题文本", "en": "The complete question to ask"},
		"options":             {"cn": "可选答案列表（2-4个）", "en": "Available choices for this question (2-4 options)"},
		"options_label":       {"cn": "选项显示文本（1-5个词）", "en": "The display text for this option (1-5 words)."},
		"options_description": {"cn": "选项详细说明", "en": "Explanation of what this option means or what will happen if chosen."},
		"options_preview":     {"cn": "可选的预览内容，用于UI模型、代码片段或视觉比较。仅在单选问题中支持。", "en": "Optional preview content rendered when this option is focused. Use for mockups, code snippets, or visual comparisons. Only supported for single-select questions."},
		"multi_select":        {"cn": "是否允许多选", "en": "Set to true to allow the user to select multiple options instead of just one."},
	}
	desc := func(key string) string {
		if d, ok := p[key][lang]; ok {
			return d
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"description": desc("questions"),
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"header":   map[string]any{"type": "string", "description": desc("header")},
						"question": map[string]any{"type": "string", "description": desc("question")},
						"options": map[string]any{
							"type":        "array",
							"description": desc("options"),
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string", "description": desc("options_label")},
									"description": map[string]any{"type": "string", "description": desc("options_description")},
									"preview":     map[string]any{"type": "string", "description": desc("options_preview")},
								},
								"required": []any{"label", "description"},
							},
						},
						"multi_select": map[string]any{"type": "boolean", "default": false, "description": desc("multi_select")},
					},
					"required": []any{"header", "question", "options"},
				},
				"minItems": 1,
				"maxItems": 4,
			},
		},
		"required": []any{"questions"},
	}
}

func (p *AskUserMetadataProvider) GetName() string { return "ask_user" }
func (p *AskUserMetadataProvider) GetDescription(language string) string {
	if d, ok := askUserDescription[language]; ok {
		return d
	}
	return askUserDescription["cn"]
}
func (p *AskUserMetadataProvider) GetInputParams(language string) map[string]any {
	return GetAskUserMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&AskUserMetadataProvider{}) }
