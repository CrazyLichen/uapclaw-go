package rsi

import (
	"math"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// usage.go —— 运行级模型用量观察与持久化、无内容的调用账本。
// Python: openjiuwen/rsi/usage.py

// ──────────────────────────── 导出函数 ────────────────────────────

// UsageTokens 归一化 core/OpenAI 计数器，缺失的提供方数据不做估算。
// Python: usage_tokens()。输入含缓存命中（缓存命中是输入的子集而非额外
// 输入）；推理 token（若提供）已含在输出 token 中。
// 键回退序：input: input_tokens→prompt_tokens→input；output:
// output_tokens→completion_tokens→output；cache: cache_read_tokens→
// prompt_cache_hit_tokens→cache_hit→prompt_tokens_details.cached_tokens。
// 旧 cache_tokens 可能指缓存写入，不得标为命中。
func UsageTokens(raw any) *RsiUsageTokens {
	// Python: usage.model_dump(exclude_unset=True) 分支 → *UsageMetadata 归一
	if md, ok := raw.(*llmschema.UsageMetadata); ok {
		raw = usageMetadataToMap(md)
	}
	// 非 Mapping 输入：计数器全缺失（Python getattr 路径在 Go 侧仅覆盖
	// 结构化两种形态，见 spec 已知偏差说明）
	m, _ := raw.(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	// Python: cache = _counter(usage, "cache_read_tokens", "prompt_cache_hit_tokens", "cache_hit")
	cache := counter(m, "cache_read_tokens", "prompt_cache_hit_tokens", "cache_hit")
	if cache == nil {
		// Python: cache = _counter(_field(usage, "prompt_tokens_details"), "cached_tokens")
		cache = counter(fieldOf(m, "prompt_tokens_details"), "cached_tokens")
	}
	return NewRsiUsageTokens(
		counter(m, "input_tokens", "prompt_tokens", "input"),
		counter(m, "output_tokens", "completion_tokens", "output"),
		cache,
	)
}

// UsageSnapshot 从持久化 state 装载共享累计投影。
// Python: usage_snapshot()——仅从 Mapping 装载；cost_estimate 恒为 nil。
func UsageSnapshot(raw any) *RsiUsage {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return NewRsiUsage(UsageTokens(m["tokens"]), nil, intOf(m["call_count"], 0))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// usageMetadataToMap 将 *UsageMetadata 展开为 Python model_dump 等价映射。
// 指针字段（CacheReadTokens）nil 视为未设置（对齐 exclude_unset）；
// 非指针 int 字段恒存在（Go 无 unset 概念，取值 0 时 counter 仍合法返回 0）。
func usageMetadataToMap(md *llmschema.UsageMetadata) map[string]any {
	m := map[string]any{
		"input_tokens":  md.InputTokens,
		"output_tokens": md.OutputTokens,
	}
	if md.CacheReadTokens != nil {
		m["cache_read_tokens"] = *md.CacheReadTokens
	}
	return m
}

// fieldOf 读取 Mapping 字段（Python: _field 的 Mapping 分支；结构体属性分支
// 由 *UsageMetadata 展开覆盖）。
func fieldOf(value any, key string) any {
	if m, ok := value.(map[string]any); ok {
		return m[key]
	}
	return nil
}

// counter 按键序取第一个合法计数器：非 bool 的非负整数（Python:
// isinstance(raw, int) and not isinstance(raw, bool) and raw >= 0）。
// JSON 反序列化数字为 float64，取整数值等价接受（Python json.loads 得 int）。
func counter(value any, keys ...string) *int {
	m, _ := value.(map[string]any)
	if m == nil {
		return nil
	}
	for _, key := range keys {
		raw, ok := m[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case bool:
			continue
		case int:
			if v >= 0 {
				return &v
			}
		case int64:
			if v >= 0 {
				iv := int(v)
				return &iv
			}
		case float64:
			// 整数值才视为计数器（Python json.loads 的 int 形态）
			if v >= 0 && v == math.Trunc(v) {
				iv := int(v)
				return &iv
			}
		}
	}
	return nil
}

// intOf 以缺省值读取整数（Python: int(raw.get(key, default))）。
func intOf(raw any, defaultValue int) int {
	if raw == nil {
		return defaultValue
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}
