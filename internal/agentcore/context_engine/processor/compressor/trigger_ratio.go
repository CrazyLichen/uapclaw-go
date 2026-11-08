package compressor

import (
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultTriggerContextRatio 默认触发比例（对齐上游 forked 压缩器默认 0.8）
	DefaultTriggerContextRatio = 0.8
	// DefaultMinTargetContextRatio 默认待压缩内容占比下限（对齐上游默认 0.1）
	DefaultMinTargetContextRatio = 0.1
)

// ──────────────────────────── 导出函数 ────────────────────────────

// triggerThresholdByRatio 返回按上下文预算与比例算出的触发阈值（至少 1）。
// Python: forked/compressor/base.py 里 threshold = context_max * trigger_context_ratio
func triggerThresholdByRatio(mc iface.ModelContext, ratio float64) int {
	budget := processor.EffectiveContextBudget(mc, 0)
	threshold := int(float64(budget) * ratio)
	if threshold < 1 {
		threshold = 1
	}
	return threshold
}

// targetAboveMinRatio 判断待压缩内容是否达到占比下限。
// 低于下限说明这次能压的太少：继续压缩只会白花一次模型调用，
// 总量几乎不降，还会把碎片摘要塞进上下文（对齐上游 forked base 的
// min_target_context_ratio 判定）。
func targetAboveMinRatio(mc iface.ModelContext, messages []llm_schema.BaseMessage, minRatio float64) bool {
	if minRatio <= 0 || len(messages) == 0 {
		return true
	}
	budget := processor.EffectiveContextBudget(mc, 0)
	minTokens := int(float64(budget) * minRatio)
	if minTokens <= 0 {
		return true
	}
	return processor.CountMessagesTokens(mc.TokenCounter(), messages, "", "") >= minTokens
}
