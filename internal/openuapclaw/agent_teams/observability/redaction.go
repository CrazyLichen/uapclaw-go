package observability

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const redactedPrefix = "sha256:"

// errorSummaryMaxLength 错误摘要上限：原位脱敏、永不整体哈希，
// 上限远低于通用属性上限以保证轨迹状态行紧凑。
// Python: redaction.py:26
const errorSummaryMaxLength = 4096

// secretMask 凭据掩码。
// Python: redaction.py:29
const secretMask = "***"

// sensitiveAssignmentRE key=value / key: value 形状（带引号或裸值），
// 含前导 bearer 方案使 "authorization: Bearer <token>" 被作为整体脱敏。
// Python: redaction.py:33-40
var sensitiveAssignmentRE = regexp.MustCompile(
	`(?i)\b(authorization|api[_-]?key|access[_-]?token|refresh[_-]?token|` +
		`session[_-]?token|id[_-]?token|cookie|password|passwd|secret|` +
		`client[_-]?secret|private[_-]?key)` +
		`(\s*[=:]\s*)` +
		`("[^"]*"|'[^']*'|(?:bearer\s+)?[^\s,;&"]+)`)

// bearerTokenRE 无标签键的独立 "Bearer <token>"。
// Python: redaction.py:42-44
var bearerTokenRE = regexp.MustCompile(`(?i)\bbearer\s+([a-z0-9._~+/=-]{8,})`)

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

// RedactErrorSummary 把错误原因浓缩为脱敏、单行、有上限的摘要。
// 刻意不做哈希：原因必须保持可读，仅对凭据形状片段原位脱敏。
// Python: redact_error_summary(value, config)（redaction.py:56-72）
func RedactErrorSummary(value any, config *ObservabilityConfig) string {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	text = maskSensitiveText(text)
	text = strings.Join(strings.Fields(text), " ")
	maxLength := errorSummaryMaxLength
	if config != nil && config.AttributeValueMaxLength > 0 {
		maxLength = config.AttributeValueMaxLength
	}
	return truncate(text, min(maxLength, errorSummaryMaxLength))
}

// RedactSystemPrompt 保护系统提示词但不应用属性长度上限。
// 系统指令是轨迹对比的规范输入：截断会把截断后缀纳入其身份并产生假差异。
// 显式隐私脱敏仍然优先；否则完整保留。
// Python: redact_system_prompt(value, config)（redaction.py:115-126）
func RedactSystemPrompt(value any, config *ObservabilityConfig) string {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	if config.RedactPrompts {
		return hashValue(text)
	}
	return text
}

// truncate 硬截断 OTel 属性并标注截断层。
// 按字符（rune）计数与截断，对齐 Python len(str) / value[:N] 的语义：
// 按字节截断会破坏多字节字符，并在 CJK 文本上过早触发截断。
// Python: truncate(value, max_length)（redaction.py:93-99）
func truncate(value string, maxLength int) string {
	runes := []rune(value)
	if maxLength <= 0 || len(runes) <= maxLength {
		return value
	}
	omitted := len(runes) - maxLength
	return string(runes[:maxLength]) + fmt.Sprintf("...<OTel attribute truncated: %d chars omitted>", omitted)
}

// hashValue 用 SHA-256 前缀哈希替换原值，保留关联性但不暴露内容。
// Python: _hash(value)
func hashValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s%x", redactedPrefix, digest[:8])
}

// maskSensitiveText 凭据形状片段原位脱敏，保留周边文本。
// Python: _mask_sensitive_text(text)（redaction.py:46-54）
func maskSensitiveText(text string) string {
	text = sensitiveAssignmentRE.ReplaceAllStringFunc(text, func(match string) string {
		groups := sensitiveAssignmentRE.FindStringSubmatch(match)
		return groups[1] + groups[2] + secretMask
	})
	return bearerTokenRE.ReplaceAllString(text, "Bearer "+secretMask)
}
