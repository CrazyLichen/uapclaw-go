package prompts

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// PromptMode 提示词注入模式枚举
//
// 定义在 prompts 包（而非 schema），对齐上游归属：
// openjiuwen/harness/prompts/builder.py:21 的 `class PromptMode(str, Enum)`。
// 放在 prompts 包使 schema 可以正常 import prompts，避免
// schema ↔ prompts 的循环依赖。
type PromptMode int

const (
	// PromptModeFull 完整提示词（不过滤）
	PromptModeFull PromptMode = iota
	// PromptModeMinimal 精简提示词（仅保留 priority <= 20）
	PromptModeMinimal
	// PromptModeNone 无提示词（不注入系统提示词）
	PromptModeNone
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsePromptMode 从字符串解析 PromptMode
func ParsePromptMode(s string) (PromptMode, error) {
	switch strings.ToLower(s) {
	case "full":
		return PromptModeFull, nil
	case "minimal":
		return PromptModeMinimal, nil
	case "none":
		return PromptModeNone, nil
	default:
		return PromptModeFull, fmt.Errorf("未知的 PromptMode: %q", s)
	}
}

// String 返回 PromptMode 的字符串表示
func (m PromptMode) String() string {
	switch m {
	case PromptModeFull:
		return "full"
	case PromptModeMinimal:
		return "minimal"
	case PromptModeNone:
		return "none"
	default:
		return fmt.Sprintf("unknown(%d)", m)
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (m PromptMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (m *PromptMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("PromptMode 应为字符串，解析失败: %w", err)
	}
	parsed, err := ParsePromptMode(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
