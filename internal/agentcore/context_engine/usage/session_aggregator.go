package usage

import "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 导出函数 ────────────────────────────

// RequestCacheFromUsage 把 provider 上报的用量转成请求级 KV cache 用量。
// provider 归一字段（CacheReadTokens/CacheMissTokens，C-B3 批次接线）优先直接采用；
// 归一字段缺失（如只带 legacy CacheTokens 的旧 provider）时回落旧算法：
// miss = 输入 − 命中，负数钳制为 0。
// Python: provider_usage.request_kv_cache_usage()
func RequestCacheFromUsage(meta *schema.UsageMetadata) RequestKVCacheUsage {
	const notReported = "not_reported"
	out := RequestKVCacheUsage{Status: notReported, Source: notReported}
	if meta == nil {
		return out
	}
	if input := providerInputTokens(meta); input != nil {
		out.InputTokens = input
		out.Source = "provider_usage"
		out.Status = "reported"
	}
	// 归一字段优先：provider 明确区分读/miss 时直接采用
	if meta.CacheReadTokens != nil {
		read := *meta.CacheReadTokens
		out.CacheReadTokens = &read
		if meta.CacheMissTokens != nil {
			miss := *meta.CacheMissTokens
			out.CacheMissTokens = &miss
		} else if out.InputTokens != nil && *out.InputTokens > read {
			miss := *out.InputTokens - read
			out.CacheMissTokens = &miss
		}
		if out.Status == notReported {
			out.Status = "reported"
		}
		if meta.CacheSource != "" {
			out.Source = meta.CacheSource
		}
		return out
	}
	if meta.CacheTokens > 0 {
		read := meta.CacheTokens
		out.CacheReadTokens = &read
		if out.InputTokens != nil && *out.InputTokens > 0 {
			hit := float64(read) / float64(*out.InputTokens)
			if hit > 1 {
				hit = 1
			}
			out.HitRate = &hit
		}
		if out.InputTokens != nil {
			miss := *out.InputTokens - read
			if miss < 0 {
				miss = 0
			}
			out.CacheMissTokens = &miss
		}
	}
	return out
}

// AggregateSessionCache 汇总一次会话内的请求级 KV cache 用量（token 加权）。
// 命中率 = Σ命中 / Σ输入，只统计已上报输入的调用；未上报的调用单独计数，
// 让前端能区分"没有缓存"与"没上报"。
// Python: session_aggregator.aggregate()
func AggregateSessionCache(requests []RequestKVCacheUsage) SessionKVCacheUsage {
	out := SessionKVCacheUsage{
		CallsTotal:  len(requests),
		Status:      "not_reported",
		DataQuality: "not_reported",
	}
	inputSum, readSum, observed := 0, 0, 0
	for _, request := range requests {
		switch {
		case request.InputTokens == nil:
			out.CallsNotReported++
		case request.CacheReadTokens == nil:
			out.CallsPartial++
			inputSum += *request.InputTokens
			observed++
		default:
			out.CallsObserved++
			inputSum += *request.InputTokens
			readSum += *request.CacheReadTokens
			observed++
		}
	}
	out.InputTokensTotal = inputSum
	out.CacheReadTokensTotal = readSum
	if observed == 0 {
		return out
	}
	out.Status = "reported"
	out.DataQuality = "full"
	if out.CallsPartial > 0 || out.CallsNotReported > 0 {
		out.DataQuality = "partial"
	}
	if inputSum > 0 {
		hit := float64(readSum) / float64(inputSum)
		out.WeightedHitRate = &hit
	}
	return out
}
