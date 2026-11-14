package rsi

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestUsageTokens_多键回退 归一化按 Python 键序回退且不估算
func TestUsageTokens_多键回退(t *testing.T) {
	// OpenAI 风格 + prompt_tokens_details.cached_tokens 回退
	got := UsageTokens(map[string]any{
		"prompt_tokens":         100,
		"completion_tokens":     30,
		"prompt_tokens_details": map[string]any{"cached_tokens": 40},
	})
	if got.Input() == nil || *got.Input() != 100 || got.Output() == nil || *got.Output() != 30 {
		t.Fatalf("input/output 回退不符: %+v", got)
	}
	if got.CacheHit() == nil || *got.CacheHit() != 40 {
		t.Fatalf("cache_hit 应回退到 cached_tokens: %+v", got)
	}
}

// TestUsageTokens_核心直接键 直接键（input_tokens/cache_read_tokens）优先于回退键
func TestUsageTokens_核心直接键(t *testing.T) {
	got := UsageTokens(map[string]any{
		"input_tokens":      10,
		"output_tokens":     20,
		"cache_read_tokens": 3,
	})
	if *got.Input() != 10 || *got.Output() != 20 || *got.CacheHit() != 3 {
		t.Fatalf("直接键不符: %+v", got)
	}
}

// TestUsageTokens_全缺失不估算 缺失计数全 nil，不得估算
func TestUsageTokens_全缺失不估算(t *testing.T) {
	empty := UsageTokens(map[string]any{})
	if empty.Input() != nil || empty.Output() != nil || empty.CacheHit() != nil {
		t.Fatal("缺失计数应全 nil")
	}
}

// TestUsageTokens_非负整数校验 bool/负数不是合法计数器（Python: int 且非 bool 且 >=0）
func TestUsageTokens_非负整数校验(t *testing.T) {
	bad := UsageTokens(map[string]any{"input_tokens": true, "output_tokens": -5})
	if bad.Input() != nil || bad.Output() != nil {
		t.Fatal("bool/负数不应计入计数器")
	}
}

// TestUsageTokens_UsageMetadata输入 llmschema.UsageMetadata 归一化（model_dump 等价）
func TestUsageTokens_UsageMetadata输入(t *testing.T) {
	cached := 9
	md := &llmschema.UsageMetadata{
		InputTokens:     100,
		OutputTokens:    30,
		CacheReadTokens: &cached,
	}
	got := UsageTokens(md)
	if got.Input() == nil || *got.Input() != 100 || got.Output() == nil || *got.Output() != 30 {
		t.Fatalf("UsageMetadata input/output 不符: %+v", got)
	}
	if got.CacheHit() == nil || *got.CacheHit() != 9 {
		t.Fatalf("UsageMetadata cache 不符: %+v", got)
	}
}

// TestUsageTokens_嵌套details缓存 无直接缓存键时回退 prompt_tokens_details.cached_tokens
func TestUsageTokens_嵌套details缓存(t *testing.T) {
	got := UsageTokens(map[string]any{
		"input_tokens":          5,
		"prompt_tokens_details": map[string]any{"cached_tokens": 2},
	})
	if got.CacheHit() == nil || *got.CacheHit() != 2 {
		t.Fatalf("嵌套缓存回退不符: %+v", got)
	}
	// 旧 cache_tokens 可能指写入：不得标为命中
	legacy := UsageTokens(map[string]any{"cache_tokens": 7})
	if legacy.CacheHit() != nil {
		t.Fatal("legacy cache_tokens 不得标为命中")
	}
}

// TestUsageSnapshot_从持久化状态装载 snapshot 仅从 Mapping 装载
func TestUsageSnapshot_从持久化状态装载(t *testing.T) {
	if UsageSnapshot(nil) != nil {
		t.Fatal("非 Mapping 返回 nil")
	}
	snap := UsageSnapshot(map[string]any{
		"tokens":     map[string]any{"input_tokens": 5},
		"call_count": 3,
	})
	if snap == nil || snap.Tokens().Input() == nil || *snap.Tokens().Input() != 5 {
		t.Fatal("快照装载不符")
	}
	if snap.CallCount() != 3 {
		t.Fatal("call_count 不符")
	}
	if snap.CostEstimate() != nil {
		t.Fatal("cost_estimate 恒 nil")
	}
	// call_count 缺省为 0
	snap2 := UsageSnapshot(map[string]any{"tokens": map[string]any{}})
	if snap2.CallCount() != 0 {
		t.Fatal("call_count 缺省应为 0")
	}
}
