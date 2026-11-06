package usage

import "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RequestCacheFromUsage 把 provider 上报的用量转成请求级 KV cache 用量。
//
// Go 的 UsageMetadata 只有 CacheTokens（= 缓存读 token），缓存写字段缺失属 C 组缺陷，
// 这里按未上报处理，不臆造数据。miss = 输入 − 命中，负数钳制为 0。
//
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
//
// 命中率 = Σ命中 / Σ输入，只统计已上报输入的调用；未上报的调用单独计数，
// 让前端能区分"没有缓存"与"没上报"。
//
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

// ──────────────────────────── 非导出函数 ────────────────────────────
