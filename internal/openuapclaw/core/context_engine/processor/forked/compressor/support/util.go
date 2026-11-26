package support

import (
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"

	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/support"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
)

// ──────────────────────────── 常量 ────────────────────────────

// StateReinjectionMarker 状态回注标记
// Python: util.py STATE_REINJECTION_MARKER
const StateReinjectionMarker = "[STATE_REINJECTION]"

// InternalUserPrefixes 标记"用户角色但携带内部状态"的消息前缀表；
// 此类消息不得视为真实用户轮次。
// Python: util.py:38 (INTERNAL_USER_PREFIXES)
// 注：<memory_block_session> 为 session_memory_compressor.py 的
// SESSION_MEMORY_BLOCK_OPEN 内联（Python 为避免循环依赖同样内联于此）。
var InternalUserPrefixes = []string{
	"<system-reminder>",
	"<memory_block_current>",
	"<memory_block_dialogue>",
	"<memory_block_round>",
	"<memory_block_session>",
	"<recovered_context>",
	"[STATE_REINJECTION]",
}

// ──────────────────────────── 结构体 ────────────────────────────

// ContextWindowProvider 暴露模型名与窗口 token 原料的可选能力面。
// Python 通过 getattr(context, "_model_name"/"_context_window_tokens"/
// "_model_context_window_tokens") 读取私有属性；Go 侧 ModelContext 无对应
// 公开访问器，SessionModelContext 按需实现本接口（偏差 D-24.2-2 平台差异）。
type ContextWindowProvider interface {
	// ModelName 当前模型名
	ModelName() string
	// ContextWindowTokens 全局上下文窗口 token 数（0 表示未设置）
	ContextWindowTokens() int
	// ModelContextWindowTokens 模型名到窗口 token 的映射表
	ModelContextWindowTokens() map[string]int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateContentTokens 无 token 计数器时的近似计数（len//3）。
// Python: util.py estimate_content_tokens
func EstimateContentTokens(content any) int {
	text := cesupport.SanitizeContentForText(content)
	n := len([]rune(text)) / 3
	if n < 1 {
		return 1
	}
	return n
}

// CountMessagesTokens 按 tokenizer 优先、字符兜底的策略计数。
// usageAware=true 时优先取最后一条携带 usage_metadata 的 AssistantMessage
// 上报的累计 total_tokens 作基线，仅对其后的尾部消息做 len//3 估算；
// 单消息/局部列表调用方必须保持 usageAware=false（累计口径不可按消息切分）。
// Python: util.py:271 (count_messages_tokens)
func CountMessagesTokens(
	messages []llm_schema.BaseMessage,
	counter token.TokenCounter,
	modelName string,
	processorType string,
	usageAware bool,
) int {
	if len(messages) == 0 {
		return 0
	}
	if usageAware {
		if usageTokens, ok := countUsageTokensWithTail(messages); ok {
			return usageTokens
		}
	}
	if counter != nil {
		if n, err := counter.CountMessages(messages, modelName); err == nil {
			return n
		}
		// Python: except Exception → logger.warning + 字符兜底（防御路径）
	}
	total := 0
	for _, message := range messages {
		total += EstimateContentTokens(message.GetContent().String())
	}
	return total
}

// ResolveContextMax 解析上下文处理器比例阈值使用的 token 容量。
// Python: util.py:332 (resolve_context_max) → ContextUtils.resolve_context_max
func ResolveContextMax(ctx any, modelName string) int {
	name := modelName
	fallback := 0
	mapping := map[string]int{}
	if p, ok := ctx.(ContextWindowProvider); ok {
		if name == "" {
			name = p.ModelName()
		}
		fallback = p.ContextWindowTokens()
		mapping = p.ModelContextWindowTokens()
	}
	// Python: ContextUtils.resolve_context_max
	return cecontext.ResolveContextMax(name, fallback, mapping)
}

// ResolveRatioTokenThreshold 把容量比例换算为正数 token 阈值。
// Python: util.py resolve_ratio_token_threshold
func ResolveRatioTokenThreshold(contextMax int, ratio float64) int {
	t := int(float64(contextMax) * ratio)
	if t < 1 {
		return 1
	}
	return t
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// countUsageTokensWithTail 返回模型上报的累计基线 + 尾部消息估算之和。
// Python: util.py count_usage_tokens_with_tail
func countUsageTokensWithTail(messages []llm_schema.BaseMessage) (int, bool) {
	base, tail, ok := lastUsageBase(messages)
	if !ok {
		return 0, false
	}
	total := base
	for _, message := range tail {
		total += len3(message)
	}
	return total, true
}

// lastUsageBase 从尾部向前找最后一条携带有效 usage_metadata 的 AssistantMessage，
// 返回 (total_tokens, 其后的尾部消息)。
// Python: util.py _last_usage_base
func lastUsageBase(messages []llm_schema.BaseMessage) (int, []llm_schema.BaseMessage, bool) {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		message := messages[idx]
		if !iface.HasValidUsageMetadata(message) {
			continue
		}
		assistant, ok := message.(*llm_schema.AssistantMessage)
		if !ok || assistant.UsageMetadata == nil || assistant.UsageMetadata.TotalTokens <= 0 {
			continue
		}
		return assistant.UsageMetadata.TotalTokens, messages[idx+1:], true
	}
	return 0, nil, false
}

// len3 单消息 len//3 估算（与本模块兜底系数一致）。
// Python: util.py _len3
func len3(message llm_schema.BaseMessage) int {
	return len([]rune(cesupport.SanitizeContentForText(message.GetContent().String()))) / 3
}
