package sections

import saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"

// ──────────────────────────── 常量 ────────────────────────────

const (
	reloadHintCN = "# 上下文压缩\n\n" +
		"你的上下文在过长时会被自动压缩，" +
		"并标记为[OFFLOAD: handle=<id>, type=<type>]。\n\n" +
		"如果你认为需要读取隐藏的内容，" +
		"可随时调用reload_original_context_messages工具。\n\n" +
		"请勿猜测或编造缺失的内容。\n\n" +
		`存储类型："in_memory"（会话缓存）`

	reloadHintEN = "# Context Compression\n\n" +
		"Your context will be automatically compressed when it becomes too long " +
		"and marked with [OFFLOAD: handle=<id>, type=<type>].\n\n" +
		`Call reload_original_context_messages(offload_handle="<id>", ` +
		`offload_type="<type>"), using the exact values from the marker.\n\n` +
		"Do not guess or fabricate missing content.\n\n" +
		`Storage types: "in_memory" (session cache)`

	// reloadSectionName 上下文压缩节名称
	reloadSectionName = "offload"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildReloadSection 构建上下文压缩节（Priority 90）
func BuildReloadSection(lang string) saprompt.PromptSection {
	var hint string
	if lang == "en" {
		hint = reloadHintEN
	} else {
		hint = reloadHintCN
	}

	return saprompt.PromptSection{
		Name:     reloadSectionName,
		Content:  map[string]string{lang: hint},
		Priority: 90,
	}
}
