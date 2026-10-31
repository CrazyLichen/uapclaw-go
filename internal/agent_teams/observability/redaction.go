package observability

import (
	"crypto/sha256"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

const redactedPrefix = "sha256:"

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// RedactPrompt 对 prompt 片段应用脱敏策略。
// Python: redact_prompt(value, config)
func RedactPrompt(value any, config *ObservabilityConfig) string {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	if config.RedactPrompts {
		return hashValue(text)
	}
	return truncate(text, config.AttributeValueMaxLength)
}

// RedactCompletion 对 completion 片段应用脱敏策略。
// Python: redact_completion(value, config)
func RedactCompletion(value any, config *ObservabilityConfig) string {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	if config.RedactCompletions {
		return hashValue(text)
	}
	return truncate(text, config.AttributeValueMaxLength)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncate 硬截断字符串并标注截断长度。
// 按字符（rune）计数与截断，对齐 Python len(str) / value[:N] 的语义：
// 按字节截断会破坏多字节字符，并在 CJK 文本上过早触发截断。
// Python: _truncate(value, max_length)
func truncate(value string, maxLength int) string {
	runes := []rune(value)
	if maxLength <= 0 || len(runes) <= maxLength {
		return value
	}
	return string(runes[:maxLength]) + fmt.Sprintf("...<truncated %d chars>", len(runes)-maxLength)
}

// hashValue 用 SHA-256 前缀哈希替换原值，保留关联性但不暴露内容。
// Python: _hash(value)
func hashValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s%x", redactedPrefix, digest[:8])
}
