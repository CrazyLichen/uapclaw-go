package recommendation

import (
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultPromptSuggestionEnabled 推荐默认关闭
	// Python: common/recommendation_switches.py:26 DEFAULT_PROMPT_SUGGESTION_ENABLED
	DefaultPromptSuggestionEnabled = false
	// PolicyID 建议事件在推荐账本里的命名空间
	// Python: prompt_suggestion.py:46 POLICY_ID
	PolicyID = "prompt-suggestion-mvp-v1"
	// ReadyEvent AgentServer 推给网关的事件名
	// Python: prompt_suggestion.py:48 READY_EVENT
	ReadyEvent = "prompt_suggestion.ready"
	// PromptVersion 建议提示词版本
	// Python: prompt_suggestion.py:52 PROMPT_VERSION
	PromptVersion = "prompt-suggestion-fork-v13"
	// MaxSuggestions 一轮只给一条建议
	// Python: prompt_suggestion.py:54 MAX_SUGGESTIONS
	MaxSuggestions = 1
	// MaxForkAnswerTokens 重放回答的付费上限，超出判 cache_cold
	// Python: prompt_suggestion.py:60 MAX_FORK_ANSWER_TOKENS
	MaxForkAnswerTokens = 10_000
	// SuggestionPrompt 建议生成的用户消息，逐字复刻上游（禁止本地化）
	// Python: prompt_suggestion.py:63-75 SUGGESTION_PROMPT
	SuggestionPrompt = "This is an app-generated request for a suggested next user message. " +
		"The user did not write this message.\n\n" +
		"Your suggestion will appear in the user's empty message composer. " +
		"They can accept it, edit it, or ignore it; it will not be sent " +
		"automatically.\n\n" +
		"Predict the single message this user is most likely to type next from " +
		"the visible conversation.\n" +
		"Use the whole conversation to understand their current intent and the " +
		"latest exchange. Choose the\n" +
		"action and specific object that best continue that intent, rather than " +
		"a generally useful follow-up.\n" +
		"Express its core naturally in the user's voice, keeping the requirements " +
		"needed to retain its meaning.\n" +
		"Choose the strongest grounded move even if several continuations are " +
		"possible; uncertainty about\n" +
		"exact wording or incidental details is not a reason to return null. " +
		"Use null only when the\n" +
		"conversation offers no grounded next move at all.\n\n" +
		"Set suggestion to exactly one natural user message, one line, at most " +
		"240 characters, or null.\n" +
		"No alternatives or explanation."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// PromptSuggestionEnabled 读推荐开关；未配置时取默认值 false。
// Python: common/recommendation_switches.py:79 is_prompt_suggestion_enabled
func PromptSuggestionEnabled(config map[string]any) bool {
	section := recommendationSection(config)
	value, ok := section["prompt_enabled"]
	if !ok {
		return DefaultPromptSuggestionEnabled
	}
	return ResolveRecommendationSwitch(value, DefaultPromptSuggestionEnabled)
}

// ResolveRecommendationSwitch 解析显式开关值，null/缺失回落默认值。
// Python: common/recommendation_switches.py:44 resolve_recommendation_switch
// 支持 true/false、1/0、yes/no、on/off 四种字符串写法，其余回落 default。
func ResolveRecommendationSwitch(value any, defaultValue bool) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	if value == nil {
		return defaultValue
	}
	text := strings.ToLower(strings.TrimSpace(toString(value)))
	switch text {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return defaultValue
}

// ConsentEpochValue 当前同意 epoch。Active=false 等价 Python 的 None。
// Python: prompt_suggestion.py:150-160 consent_epoch —— None 只由 prompt 开关决定；
// deleted_at 走 or 0.0，0.0 是合法 epoch（从未删除）。
type ConsentEpochValue struct {
	// Value epoch 值（consent 表 deleted_at，缺失为 0.0）
	Value float64
	// Active 是否有效（prompt 开关开）
	Active bool
}

// Equal 对齐 Python 的 ==：None 与值不等；同为值时按数值相等。
func (e ConsentEpochValue) Equal(other ConsentEpochValue) bool {
	if e.Active != other.Active {
		return false
	}
	return !e.Active || e.Value == other.Value
}

// ConsentEpoch 返回当前同意 epoch。
// Python: prompt_suggestion.py:150 consent_epoch
// epoch 值来自 consent 只读探测（consent_state.go），写侧 ⤵️ 10.3.31d（spec 10.7.2 D-1）。
func ConsentEpoch(config map[string]any) ConsentEpochValue {
	if !PromptSuggestionEnabled(config) {
		return ConsentEpochValue{}
	}
	row := readConsentState()
	value := 0.0
	if row.deletedAt != nil {
		value = *row.deletedAt
	}
	return ConsentEpochValue{Value: value, Active: true}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recommendationSection 取配置里的 recommendation 段。
// Python: common/recommendation_switches.py:57 _section
func recommendationSection(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	section, ok := config["recommendation"].(map[string]any)
	if !ok {
		return nil
	}
	return section
}

// toString 把配置值转成字符串，供开关文本解析。
func toString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
