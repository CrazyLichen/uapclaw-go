package anthropic

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// UsageFromAnthropic 从 Anthropic usage 对象构建 UsageMetadata。
// input_tokens 视为模型看到的总 prompt（uncached + cache-read + cache-write）；
// cache_tokens 是读计数（便宜的部分）。cache_write 是 uncached/miss 部分的子集，
// 必须计入 miss，同时单独暴露为重叠诊断。
// Python: anthropic_model_client.py:1368-1417 _usage_from_anthropic
func UsageFromAnthropic(usage map[string]any, modelName string) *llmschema.UsageMetadata {
	if usage == nil {
		return nil
	}
	uncached := intOf(usage["input_tokens"])
	cacheRead := intOf(usage["cache_read_input_tokens"])
	cacheWriteRaw, hasWrite := usage["cache_creation_input_tokens"]
	cacheWrite := intOf(cacheWriteRaw)
	output := intOf(usage["output_tokens"])
	totalInput := uncached + cacheRead + cacheWrite

	// 尽力提取费用：Anthropic 不返回 $；走基础 helper（认识 OpenRouter 风格 cost 字段）。
	// 两者皆无则留零——postrun 脚本套价格。
	inputCost, outputCost, totalCost := model_clients.ExtractCostInfo(usage)

	// cache_miss = uncached + cache_write（canonical 契约：write 是 miss 子集）
	cacheMiss := uncached + cacheWrite

	meta := &llmschema.UsageMetadata{
		ModelName:          modelName,
		InputTokens:        totalInput,
		OutputTokens:       output,
		TotalTokens:        totalInput + output,
		CacheTokens:        cacheRead,
		CacheMissTokens:    &cacheMiss,
		CacheWriteTokens:   &cacheWrite,
		CacheStatus:        "observed",
		CacheSource:        "provider_usage",
		CacheAuthoritative: true,
		InputCost:          inputCost,
		OutputCost:         outputCost,
		TotalCost:          totalCost,
	}
	meta.CacheReadTokens = &cacheRead
	if hasWrite && cacheWriteRaw != nil {
		meta.CacheCreationTokens = &cacheWrite
	}
	return meta
}
