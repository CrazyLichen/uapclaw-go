package sections

import saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	reloadHintCN = "# 上下文压缩\n\n" +
		"你的上下文在过长时会被自动压缩，" +
		"并标记为[OFFLOAD: handle=<id>, type=<type>]。\n\n" +
		"如果你认为需要读取隐藏的内容，" +
		"可随时调用reload_original_context_messages工具，" +
		`使用标记中的handle和type值：reload_original_context_messages(offload_handle="<id>", offload_type="<type>")。` +
		"\n\n" +
		"请勿猜测或编造缺失的内容。\n\n" +
		// 注意：Python 原文只列了 "in_memory"，Go 额外增加 "filesystem"。
		// 原因：Go 的 context_engine 默认使用 filesystem 卸载模式（offload.go 默认 offloadType="filesystem"），
		// 若提示词不声明 filesystem 类型，LLM 遇到 type=filesystem 的 OFFLOAD 标记时无法正确调用 reload 工具。
		// Python 缺少此声明是一个缺陷。
		`存储类型："in_memory"（会话缓存）、"filesystem"（磁盘文件）`

	reloadHintEN = "# Context Compression\n\n" +
		"Your context will be automatically compressed when it becomes too long " +
		"and marked with [OFFLOAD: handle=<id>, type=<type>].\n\n" +
		`Call reload_original_context_messages(offload_handle="<id>", ` +
		`offload_type="<type>"), using the exact values from the marker.\n\n` +
		"Do not guess or fabricate missing content.\n\n" +
		// 注意：Python 原文只列了 "in_memory"，Go 额外增加 "filesystem"。
		// 原因：Go 的 context_engine 默认使用 filesystem 卸载模式（offload.go 默认 offloadType="filesystem"），
		// 若提示词不声明 filesystem 类型，LLM 遇到 type=filesystem 的 OFFLOAD 标记时无法正确调用 reload 工具。
		// Python 缺少此声明是一个缺陷。
		`Storage types: "in_memory" (session cache), "filesystem" (disk file)`

	// reloadSectionName 上下文压缩节名称
	reloadSectionName = "offload"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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

// ──────────────────────────── 非导出函数 ────────────────────────────
