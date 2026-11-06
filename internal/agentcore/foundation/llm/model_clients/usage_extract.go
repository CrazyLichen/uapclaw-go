package model_clients

import (
	"strconv"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractCacheTokens 从 provider usage 元数据提取提示词缓存命中 token 数。
//
// 各 provider 字段形态不一致：只统计缓存命中/读 token；缓存写/创建与 miss 有意忽略。
//
// Python: base_model_client.py:86-151 _extract_cache_tokens
// （7 条嵌套路径：prompt_tokens_details.cached_tokens 等；11 个平铺字段：prompt_cache_hit_tokens 等；
// 嵌套路径优先于平铺字段；_to_int 对 bool/非数字/负数返回 0）
func ExtractCacheTokens(obj map[string]any) int {
	cacheTokenPaths := [][2]string{
		{"prompt_tokens_details", "cached_tokens"},
		{"promptTokensDetails", "cachedTokens"},
		{"input_tokens_details", "cached_tokens"},
		{"input_token_details", "cached_tokens"},
		{"inputTokensDetails", "cachedTokens"},
		{"inputTokenDetails", "cachedTokens"},
		{"usageMetadata", "cachedContentTokenCount"},
	}
	for _, path := range cacheTokenPaths {
		if nested, ok := obj[path[0]].(map[string]any); ok {
			if v := toIntNonNegative(nested[path[1]]); v > 0 {
				return v
			}
		}
	}
	cacheTokenFields := []string{
		"prompt_cache_hit_tokens", "cache_read_input_tokens", "cachedContentTokenCount",
		"cached_content_token_count", "cache_tokens", "cached_tokens", "cache_hit_tokens",
		"cached_input_tokens", "cache_read_tokens", "prompt_cache_tokens", "prompt_cached_tokens",
	}
	for _, field := range cacheTokenFields {
		if v := toIntNonNegative(obj[field]); v > 0 {
			return v
		}
	}
	return 0
}

// CacheUsageMetadata 归一缓存 read/miss/write 字段，保留零值语义。
//
// legacy cache_tokens 仅作兼容读取值，不具权威性——provider 对它表示命中还是写定义不一致。
// 返回 map 的键：cache_read_tokens/cache_miss_tokens/cache_write_tokens（*int，nil=未上报）、
// cache_status/cache_source（"observed"/"provider_usage" 或 nil）、cache_authoritative（bool）。
//
// Python: base_model_client.py:152-252 _cache_usage_metadata
func CacheUsageMetadata(obj map[string]any) map[string]any {
	readTokens := firstInt(obj,
		[2]string{"prompt_tokens_details", "cached_tokens"},
		[2]string{"promptTokensDetails", "cachedTokens"},
		[2]string{"input_tokens_details", "cached_tokens"},
		[2]string{"input_token_details", "cached_tokens"},
		[2]string{"inputTokensDetails", "cachedTokens"},
		[2]string{"inputTokenDetails", "cachedTokens"},
		[2]string{"usageMetadata", "cachedContentTokenCount"},
	)
	legacyReadTokens := firstInt(obj, [2]string{"", "cache_tokens"})
	legacyRead := readTokens == nil && legacyReadTokens != nil
	if readTokens == nil {
		readTokens = legacyReadTokens
	}
	missTokens := firstInt(obj,
		[2]string{"", "prompt_cache_miss_tokens"},
		[2]string{"", "cache_miss_tokens"},
		[2]string{"", "cache_miss_input_tokens"},
		[2]string{"", "uncached_tokens"},
	)
	writeTokens := firstInt(obj,
		[2]string{"", "cache_write_tokens"},
		[2]string{"", "cache_creation_input_tokens"},
		[2]string{"", "cache_creation_tokens"},
	)
	hasCacheUsage := readTokens != nil || missTokens != nil || writeTokens != nil
	var status, source any
	if hasCacheUsage {
		status, source = "observed", "provider_usage"
	}
	// Python: "cache_authoritative": has_cache_usage and not legacy_read
	// （恒为 bool：False and not False = False，非 None）
	authoritative := hasCacheUsage && !legacyRead
	// nil 指针须显式转 any 存入 map，否则类型化 nil（(*int)(nil)）在接口比较中恒非空
	intf := func(p *int) any {
		if p == nil {
			return nil
		}
		return p
	}
	return map[string]any{
		"cache_read_tokens":  intf(readTokens),
		"cache_miss_tokens":  intf(missTokens),
		"cache_write_tokens": intf(writeTokens),
		"cache_status":       status,
		"cache_source":       source,
		// cache_tokens 保留为兼容读取值，但它表示命中还是写各 provider 不一致
		"cache_authoritative": authoritative,
	}
}

// ExtractCacheCreationTokens 仅在 provider 暴露缓存写 token 时提取（nil=未暴露）。
//
// Python: base_model_client.py:253-292 _extract_cache_creation_tokens
// （3 条嵌套路径 + 4 个平铺字段；bool 与不可转换值跳过；负数取 max(0, v)）
func ExtractCacheCreationTokens(obj map[string]any) *int {
	paths := [][2]string{
		{"input_tokens_details", "cache_creation_tokens"},
		{"input_token_details", "cache_creation_tokens"},
		{"prompt_tokens_details", "cache_creation_tokens"},
	}
	fields := []string{
		"cache_creation_input_tokens", "cache_write_input_tokens",
		"cache_creation_tokens", "cache_write_tokens",
	}
	for _, path := range paths {
		if nested, ok := obj[path[0]].(map[string]any); ok {
			if v, ok := coerceInt(nested[path[1]]); ok {
				return &v
			}
		}
	}
	for _, field := range fields {
		if v, ok := coerceInt(obj[field]); ok {
			return &v
		}
	}
	return nil
}

// ExtractReasoningTokens 从 provider usage 元数据提取推理/思考 token 数。
//
// Python: base_model_client.py:293-330 _extract_reasoning_tokens
// （completion_tokens_details.reasoning_tokens 嵌套 + completionTokensDetails.reasoningTokens
// camel 形态 + 顶层 reasoning_tokens 平铺；_to_int 对非法值返回 0）
func ExtractReasoningTokens(obj map[string]any) int {
	if nested, ok := obj["completion_tokens_details"].(map[string]any); ok {
		if v := toIntNonNegative(nested["reasoning_tokens"]); v > 0 {
			return v
		}
	}
	if nested, ok := obj["completionTokensDetails"].(map[string]any); ok {
		if v := toIntNonNegative(nested["reasoningTokens"]); v > 0 {
			return v
		}
	}
	return toIntNonNegative(obj["reasoning_tokens"])
}

// BuildUsageMetadata 从 provider 响应的 usage map 构建完整 UsageMetadata。
//
// Python: openai_model_client.py:2817-2829（invoke 组装点）/ 2940-2946（stream 组装点）——
// 字段集 = 基础 token 计数 + _extract_cache_tokens + **_cache_usage_metadata +
// _extract_cache_creation_tokens + _extract_reasoning_tokens + cost（cost 由调用方追加）。
func BuildUsageMetadata(usage map[string]any, modelName string) *llmschema.UsageMetadata {
	meta := llmschema.NewUsageMetadata()
	meta.ModelName = modelName
	meta.InputTokens = toIntNonNegative(usage["prompt_tokens"])
	meta.OutputTokens = toIntNonNegative(usage["completion_tokens"])
	meta.TotalTokens = toIntNonNegative(usage["total_tokens"])
	meta.CacheTokens = ExtractCacheTokens(usage)

	norm := CacheUsageMetadata(usage)
	if v, ok := norm["cache_read_tokens"].(*int); ok {
		meta.CacheReadTokens = v
	}
	if v, ok := norm["cache_miss_tokens"].(*int); ok {
		meta.CacheMissTokens = v
	}
	if v, ok := norm["cache_write_tokens"].(*int); ok {
		meta.CacheWriteTokens = v
	}
	if v, ok := norm["cache_status"].(string); ok {
		meta.CacheStatus = v
	}
	if v, ok := norm["cache_source"].(string); ok {
		meta.CacheSource = v
	}
	meta.CacheAuthoritative = norm["cache_authoritative"].(bool)
	meta.CacheCreationTokens = ExtractCacheCreationTokens(usage)
	// 推理 token：未暴露时不设（对齐 Python None 语义），>0 才取指针
	if n := ExtractReasoningTokens(usage); n > 0 {
		meta.ReasoningTokens = &n
	}
	return meta
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toIntNonNegative 数值/数字字符串 → 非负 int；bool/非数字/负数 → 0。
//
// Python: _extract_cache_tokens 内 _to_int（max(int(float(value)), 0) 形态）
func toIntNonNegative(v any) int {
	n, ok := coerceInt(v)
	if !ok || n < 0 {
		return 0
	}
	return n
}

// coerceInt 数值形态转换：int 族/float64/float32/数字字符串 → int；bool/nil/非数字 → 未命中。
//
// Python: _cache_usage_metadata 内 _to_int 的可空版本（None/bool 返回 None）
func coerceInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	case float32:
		return int(val), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// firstInt 按 path 序取第一个可转换 int（path[0] 为空 = 平铺字段，否则嵌套 map 单键）。
//
// Python: _cache_usage_metadata 内 _first（_get_path 逐段取值）
func firstInt(obj map[string]any, paths ...[2]string) *int {
	for _, path := range paths {
		var value any
		if path[0] == "" {
			value = obj[path[1]]
		} else if nested, ok := obj[path[0]].(map[string]any); ok {
			value = nested[path[1]]
		} else {
			continue
		}
		if v, ok := coerceInt(value); ok {
			return &v
		}
	}
	return nil
}
