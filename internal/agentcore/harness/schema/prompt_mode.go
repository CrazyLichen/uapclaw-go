package schema

import (
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// PromptMode 提示词注入模式的 schema 侧别名。
//
// 类型本体定义在 prompts 包（internal/agentcore/harness/prompts/prompt_mode.go），
// 对齐上游归属：openjiuwen/harness/prompts/builder.py:21。
//
// 保留别名让既有 hschema.PromptMode 引用继续工作，
// 且与 prompts 包是**同一类型**（非副本）。
type PromptMode = hprompts.PromptMode

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PromptModeFull 完整提示词（不过滤）
	PromptModeFull = hprompts.PromptModeFull
	// PromptModeMinimal 精简提示词（仅保留 priority <= 20）
	PromptModeMinimal = hprompts.PromptModeMinimal
	// PromptModeNone 无提示词（不注入系统提示词）
	PromptModeNone = hprompts.PromptModeNone
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsePromptMode 从字符串解析 PromptMode（转发 prompts 实现）。
func ParsePromptMode(s string) (PromptMode, error) {
	return hprompts.ParsePromptMode(s)
}
