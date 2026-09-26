package observability

import (
	"crypto/sha256"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

const redactedPrefix = "sha256:"

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
// Python: _truncate(value, max_length)
func truncate(value string, maxLength int) string {
	if maxLength <= 0 || len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + fmt.Sprintf("...<truncated %d chars>", len(value)-maxLength)
}

// hashValue 用 SHA-256 前缀哈希替换原值，保留关联性但不暴露内容。
// Python: _hash(value)
func hashValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s%x", redactedPrefix, digest[:8])
}
