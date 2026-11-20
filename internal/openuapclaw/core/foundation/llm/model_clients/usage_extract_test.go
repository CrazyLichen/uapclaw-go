package model_clients

import "testing"

// TestExtractCacheTokens_全路径矩阵 上游 _extract_cache_tokens 的 7 条嵌套路径 + 11 个平铺字段。
// Python: base_model_client.py:86-151
func TestExtractCacheTokens_全路径矩阵(t *testing.T) {
	nested := []struct {
		name string
		path map[string]any
		want int
	}{
		{"prompt_tokens_details.cached_tokens", map[string]any{"prompt_tokens_details": map[string]any{"cached_tokens": 42}}, 42},
		{"promptTokensDetails.cachedTokens", map[string]any{"promptTokensDetails": map[string]any{"cachedTokens": 7}}, 7},
		{"input_tokens_details.cached_tokens", map[string]any{"input_tokens_details": map[string]any{"cached_tokens": 9}}, 9},
		{"input_token_details.cached_tokens", map[string]any{"input_token_details": map[string]any{"cached_tokens": 3}}, 3},
		{"inputTokensDetails.cachedTokens", map[string]any{"inputTokensDetails": map[string]any{"cachedTokens": 5}}, 5},
		{"inputTokenDetails.cachedTokens", map[string]any{"inputTokenDetails": map[string]any{"cachedTokens": 6}}, 6},
		{"usageMetadata.cachedContentTokenCount", map[string]any{"usageMetadata": map[string]any{"cachedContentTokenCount": 8}}, 8},
	}
	for _, tc := range nested {
		if got := ExtractCacheTokens(tc.path); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}

	flat := []struct {
		key  string
		want int
	}{
		{"prompt_cache_hit_tokens", 11}, {"cache_read_input_tokens", 12},
		{"cachedContentTokenCount", 13}, {"cached_content_token_count", 14},
		{"cache_tokens", 15}, {"cached_tokens", 16}, {"cache_hit_tokens", 17},
		{"cached_input_tokens", 18}, {"cache_read_tokens", 19},
		{"prompt_cache_tokens", 20}, {"prompt_cached_tokens", 21},
	}
	for _, tc := range flat {
		if got := ExtractCacheTokens(map[string]any{tc.key: tc.want}); got != tc.want {
			t.Errorf("flat %s: got %d want %d", tc.key, got, tc.want)
		}
	}

	// 非法值形态：bool/非数字/负数 → 0（对齐 _to_int）
	for _, v := range []any{true, "abc", -1.5, nil, []any{}} {
		if got := ExtractCacheTokens(map[string]any{"cache_tokens": v}); got != 0 {
			t.Errorf("invalid %v: got %d want 0", v, got)
		}
	}
	// 嵌套优先于平铺（先路径后字段）
	if got := ExtractCacheTokens(map[string]any{
		"prompt_tokens_details": map[string]any{"cached_tokens": 5}, "cache_tokens": 99,
	}); got != 5 {
		t.Errorf("nested precedence: got %d want 5", got)
	}
	// 全缺 → 0
	if got := ExtractCacheTokens(map[string]any{}); got != 0 {
		t.Errorf("empty: got %d want 0", got)
	}
}

// TestCacheUsageMetadata_归一 read/miss/write 归一 + status/source/authoritative 语义。
// Python: base_model_client.py:152-252 _cache_usage_metadata
func TestCacheUsageMetadata_归一(t *testing.T) {
	m := CacheUsageMetadata(map[string]any{
		"prompt_tokens_details":       map[string]any{"cached_tokens": 30},
		"prompt_cache_miss_tokens":    10,
		"cache_creation_input_tokens": 4,
	})
	if m["cache_read_tokens"] == nil || m["cache_read_tokens"].(*int) == nil || *m["cache_read_tokens"].(*int) != 30 {
		t.Errorf("read_tokens: %v", m["cache_read_tokens"])
	}
	if m["cache_miss_tokens"] == nil || *m["cache_miss_tokens"].(*int) != 10 {
		t.Errorf("miss_tokens: %v", m["cache_miss_tokens"])
	}
	if m["cache_write_tokens"] == nil || *m["cache_write_tokens"].(*int) != 4 {
		t.Errorf("write_tokens: %v", m["cache_write_tokens"])
	}
	if m["cache_status"] != "observed" || m["cache_source"] != "provider_usage" {
		t.Errorf("status/source: %v / %v", m["cache_status"], m["cache_source"])
	}
	if m["cache_authoritative"] != true {
		t.Errorf("authoritative: %v", m["cache_authoritative"])
	}

	// legacy：无 read 信号时回落 cache_tokens 且 authoritative=false
	legacy := CacheUsageMetadata(map[string]any{"cache_tokens": 30})
	if legacy["cache_read_tokens"] == nil || *legacy["cache_read_tokens"].(*int) != 30 {
		t.Errorf("legacy read: %v", legacy["cache_read_tokens"])
	}
	if legacy["cache_authoritative"] != false {
		t.Errorf("legacy authoritative: %v", legacy["cache_authoritative"])
	}
	// 全缺 → read/miss/write nil，status/source nil，authoritative=false
	//（Python: has_cache_usage=False → "observed" if False else None / False and not False = False）
	empty := CacheUsageMetadata(map[string]any{})
	if empty["cache_read_tokens"] != nil || empty["cache_status"] != nil {
		t.Errorf("empty: %v", empty)
	}
	if empty["cache_authoritative"] != false {
		t.Errorf("empty authoritative: %v", empty["cache_authoritative"])
	}
}

// TestExtractCacheCreationTokens_矩阵 3 条嵌套路径 + 4 个平铺字段。
// Python: base_model_client.py:253-292 _extract_cache_creation_tokens
func TestExtractCacheCreationTokens_矩阵(t *testing.T) {
	nestedKeys := [][2]string{
		{"input_tokens_details", "cache_creation_tokens"},
		{"input_token_details", "cache_creation_tokens"},
		{"prompt_tokens_details", "cache_creation_tokens"},
	}
	for _, path := range nestedKeys {
		if got := ExtractCacheCreationTokens(map[string]any{
			path[0]: map[string]any{path[1]: 12}}); got == nil || *got != 12 {
			t.Errorf("nested %v: %v", path, got)
		}
	}
	for _, key := range []string{"cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens", "cache_write_tokens"} {
		if got := ExtractCacheCreationTokens(map[string]any{key: 8}); got == nil || *got != 8 {
			t.Errorf("flat %s: %v", key, got)
		}
	}
	if got := ExtractCacheCreationTokens(map[string]any{}); got != nil {
		t.Errorf("absent: %v", got)
	}
	// 非法值跳过（bool 不取）
	if got := ExtractCacheCreationTokens(map[string]any{"cache_creation_tokens": true}); got != nil {
		t.Errorf("bool: %v", got)
	}
}

// TestExtractReasoningTokens_矩阵 completion_tokens_details.reasoning_tokens 嵌套 + 顶层平铺。
// Python: base_model_client.py:293-330 _extract_reasoning_tokens
func TestExtractReasoningTokens_矩阵(t *testing.T) {
	if got := ExtractReasoningTokens(map[string]any{
		"completion_tokens_details": map[string]any{"reasoning_tokens": 900}}); got != 900 {
		t.Errorf("nested: %d", got)
	}
	if got := ExtractReasoningTokens(map[string]any{"completionTokensDetails": map[string]any{"reasoningTokens": 55}}); got != 55 {
		t.Errorf("nested camel: %d", got)
	}
	if got := ExtractReasoningTokens(map[string]any{"reasoning_tokens": 77}); got != 77 {
		t.Errorf("flat: %d", got)
	}
	if got := ExtractReasoningTokens(map[string]any{}); got != 0 {
		t.Errorf("empty: %d", got)
	}
	// 非法值 → 0
	if got := ExtractReasoningTokens(map[string]any{"reasoning_tokens": "abc"}); got != 0 {
		t.Errorf("invalid: %d", got)
	}
}

// TestBuildUsageMetadata_同构契约 三客户端共用一个构建函数：同输入同输出。
// Python: openai/siliconflow(=openai profile)/inference_affinity 三处组装点字段集一致
func TestBuildUsageMetadata_同构契约(t *testing.T) {
	usage := map[string]any{
		"prompt_tokens": 100, "completion_tokens": 200, "total_tokens": 300,
		"prompt_tokens_details":     map[string]any{"cached_tokens": 60},
		"completion_tokens_details": map[string]any{"reasoning_tokens": 150},
	}
	meta := BuildUsageMetadata(usage, "m1")
	if meta.InputTokens != 100 || meta.OutputTokens != 200 || meta.TotalTokens != 300 {
		t.Errorf("basic: %+v", meta)
	}
	if meta.CacheTokens != 60 || meta.CacheReadTokens == nil || *meta.CacheReadTokens != 60 {
		t.Errorf("cache: %+v", meta)
	}
	if meta.ReasoningTokens == nil || *meta.ReasoningTokens != 150 {
		t.Errorf("reasoning: %+v", meta)
	}
	if meta.CacheStatus != "observed" || !meta.CacheAuthoritative {
		t.Errorf("status: %+v", meta)
	}
}

// TestBuildUsageMetadata_未暴露不臆造 reasoning_tokens/cache_creation 未暴露时保持 nil。
// Python: None 字段不进 UsageMetadata（_extract_* 返回 None）
func TestBuildUsageMetadata_未暴露不臆造(t *testing.T) {
	meta := BuildUsageMetadata(map[string]any{"prompt_tokens": 5}, "m")
	if meta.ReasoningTokens != nil || meta.CacheCreationTokens != nil {
		t.Errorf("未暴露应 nil: %+v", meta)
	}
	if meta.CacheReadTokens != nil || meta.CacheStatus != "" {
		t.Errorf("无缓存信号应零值: %+v", meta)
	}
}
