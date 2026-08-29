package models

import (
	"testing"
)

// TestAllocation_ToTeamModelConfig 验证委托 Entry.ToTeamModelConfig。
func TestAllocation_ToTeamModelConfig(t *testing.T) {
	entry := NewModelPoolEntry("qwen-max", "test-key", "https://dashscope.aliyuncs.com", "DashScope")
	entry.Metadata = map[string]any{
		"client": map[string]any{"timeout": 120.0},
	}

	a := Allocation{Entry: *entry, GroupIndex: 0}
	cfg := a.ToTeamModelConfig()

	if cfg.ModelClientConfig.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", cfg.ModelClientConfig.APIKey, "test-key")
	}
	if cfg.ModelClientConfig.APIBase != "https://dashscope.aliyuncs.com" {
		t.Errorf("APIBase = %q, want %q", cfg.ModelClientConfig.APIBase, "https://dashscope.aliyuncs.com")
	}
}

// TestAllocation_ToDBRef 验证返回正确的 DB 引用 map。
func TestAllocation_ToDBRef(t *testing.T) {
	entry := NewModelPoolEntry("qwen-max", "key", "url", "DashScope")
	a := Allocation{Entry: *entry, GroupIndex: 2}

	ref := a.ToDBRef()
	if ref["model_name"] != "qwen-max" {
		t.Errorf("model_name = %v, want %q", ref["model_name"], "qwen-max")
	}
	if ref["model_index"] != 2 {
		t.Errorf("model_index = %v, want 2", ref["model_index"])
	}
}

// TestBuildModelAllocatorForPool_空池 验证空池返回 nil。
func TestBuildModelAllocatorForPool_空池(t *testing.T) {
	allocator := BuildModelAllocatorForPool(nil, "round_robin", "test-team")
	if allocator != nil {
		t.Error("空池时期望 nil allocator")
	}
}

// TestResolveMemberModelFromPool_空池 验证空池返回 nil。
func TestResolveMemberModelFromPool_空池(t *testing.T) {
	result := ResolveMemberModelFromPool(nil, "qwen", 0)
	if result != nil {
		t.Error("空池时期望 nil result")
	}
}

// TestResolveMemberModelFromPool_空名称 验证空名称返回 nil。
func TestResolveMemberModelFromPool_空名称(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("qwen", "key", "url", "OpenAI")}
	result := ResolveMemberModelFromPool(pool, "", 0)
	if result != nil {
		t.Error("空名称时期望 nil result")
	}
}

// TestModelAllocator接口 检查 ModelAllocator 接口类型可用。
func TestModelAllocator接口(t *testing.T) {
	var allocator ModelAllocator = nil
	_ = allocator
}

// ──────────────────────────── RoundRobin 测试 ────────────────────────────

// TestRoundRobinModelAllocator_Allocate 验证轮询分配
func TestRoundRobinModelAllocator_Allocate(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("model-a", "key1", "http://a", "OpenAI"),
		*NewModelPoolEntry("model-a", "key2", "http://a2", "OpenAI"),
		*NewModelPoolEntry("model-b", "key3", "http://b", "OpenAI"),
	}
	alloc := NewRoundRobinModelAllocator(pool)

	a1 := alloc.Allocate("")
	if a1 == nil {
		t.Fatalf("Expected allocation")
	}
	if a1.Entry.ModelName != "model-a" {
		t.Errorf("Expected model-a, got %s", a1.Entry.ModelName)
	}

	a2 := alloc.Allocate("")
	if a2.Entry.ModelName != "model-a" {
		t.Errorf("Expected model-a on second call, got %s", a2.Entry.ModelName)
	}

	a3 := alloc.Allocate("")
	if a3.Entry.ModelName != "model-b" {
		t.Errorf("Expected model-b, got %s", a3.Entry.ModelName)
	}
}

// TestRoundRobinModelAllocator_StateDict 验证状态快照
func TestRoundRobinModelAllocator_StateDict(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "http://x", "OpenAI")}
	alloc := NewRoundRobinModelAllocator(pool)
	alloc.Allocate("")
	alloc.Allocate("")

	state := alloc.StateDict()
	if state["index"] != 2 {
		t.Errorf("Expected index=2, got %v", state["index"])
	}
	if state["pool_digest"] != poolDigest(pool) {
		t.Errorf("Digest mismatch")
	}
}

// TestRoundRobinModelAllocator_LoadStateDict 验证状态恢复和摘要重置
func TestRoundRobinModelAllocator_LoadStateDict(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "http://x", "OpenAI")}
	alloc := NewRoundRobinModelAllocator(pool)

	// 正常恢复
	alloc.LoadStateDict(map[string]any{"index": 10, "pool_digest": poolDigest(pool)})
	if alloc.index != 10 {
		t.Errorf("Expected index=10, got %d", alloc.index)
	}

	// 摘要不匹配 → 重置
	alloc.LoadStateDict(map[string]any{"index": 20, "pool_digest": "wrong"})
	if alloc.index != 0 {
		t.Errorf("Expected reset to 0, got %d", alloc.index)
	}
}

// ──────────────────────────── ByModelName 测试 ────────────────────────────

// TestByModelNameAllocator_Allocate 验证按名分配
func TestByModelNameAllocator_Allocate(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("model-a", "key1", "http://a", "OpenAI"),
		*NewModelPoolEntry("model-a", "key2", "http://a2", "OpenAI"),
		*NewModelPoolEntry("model-b", "key3", "http://b", "OpenAI"),
	}
	alloc := NewByModelNameAllocator(pool)

	a1 := alloc.Allocate("model-a")
	if a1 == nil {
		t.Fatalf("Expected allocation")
	}
	if a1.Entry.APIBaseURL != "http://a" {
		t.Errorf("Expected first model-a endpoint, got %s", a1.Entry.APIBaseURL)
	}

	a2 := alloc.Allocate("model-a")
	if a2.Entry.APIBaseURL != "http://a2" {
		t.Errorf("Expected second model-a endpoint, got %s", a2.Entry.APIBaseURL)
	}

	// 未知名称 → nil
	a3 := alloc.Allocate("model-c")
	if a3 != nil {
		t.Errorf("Expected nil for unknown name")
	}

	// 空名称 → nil
	a4 := alloc.Allocate("")
	if a4 != nil {
		t.Errorf("Expected nil for empty name")
	}
}

// TestByModelNameAllocator_StateDict 验证状态快照列表格式
func TestByModelNameAllocator_StateDict(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "http://x", "OpenAI")}
	alloc := NewByModelNameAllocator(pool)
	alloc.Allocate("m")

	state := alloc.StateDict()
	if state["pool_digest"] != poolDigest(pool) {
		t.Errorf("Digest mismatch")
	}
	counters, ok := state["counters"].([]map[string]any)
	if !ok {
		t.Errorf("Expected counters as []map[string]any")
	}
	if len(counters) != 1 {
		t.Errorf("Expected 1 counter, got %d", len(counters))
	}
	if counters[0]["model_name"] != "m" {
		t.Errorf("Expected model_name=m, got %v", counters[0]["model_name"])
	}
}

// TestByModelNameAllocator_LoadStateDict 验证状态恢复
func TestByModelNameAllocator_LoadStateDict(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "http://x", "OpenAI")}
	alloc := NewByModelNameAllocator(pool)

	// 正常恢复（list 格式）
	alloc.LoadStateDict(map[string]any{
		"counters":    []any{map[string]any{"model_name": "m", "index": 5}},
		"pool_digest": poolDigest(pool),
	})
	if alloc.innerIndexes["m"] != 5 {
		t.Errorf("Expected index=5, got %d", alloc.innerIndexes["m"])
	}

	// 摘要不匹配 → 重置
	alloc.LoadStateDict(map[string]any{
		"counters":    []any{map[string]any{"model_name": "m", "index": 20}},
		"pool_digest": "wrong",
	})
	if alloc.innerIndexes["m"] != 0 {
		t.Errorf("Expected reset to 0, got %d", alloc.innerIndexes["m"])
	}

	// 旧 dict 格式兼容
	alloc.LoadStateDict(map[string]any{
		"inner_indexes": map[string]any{"m": 8},
		"pool_digest":   poolDigest(pool),
	})
	if alloc.innerIndexes["m"] != 8 {
		t.Errorf("Expected index=8 (legacy), got %d", alloc.innerIndexes["m"])
	}
}

// ──────────────────────────── Router 测试 ────────────────────────────

// TestRouterAllocator_Allocate 验证路由分配
func TestRouterAllocator_Allocate(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("model-a", "key", "http://router", "OpenAI"),
		*NewModelPoolEntry("model-b", "key", "http://router", "OpenAI"),
	}
	alloc, err := NewRouterAllocator(pool)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// 空名称 → 首条目
	a1 := alloc.Allocate("")
	if a1 == nil {
		t.Fatalf("Expected allocation")
	}
	if a1.Entry.ModelName != "model-a" {
		t.Errorf("Expected model-a as default, got %s", a1.Entry.ModelName)
	}

	// 已知名 → 精确查找
	a2 := alloc.Allocate("model-b")
	if a2.Entry.ModelName != "model-b" {
		t.Errorf("Expected model-b, got %s", a2.Entry.ModelName)
	}

	// 未知名 → nil
	a3 := alloc.Allocate("model-z")
	if a3 != nil {
		t.Errorf("Expected nil for unknown name")
	}
}

// TestRouterAllocator_EmptyPool 验证空池返回 error
func TestRouterAllocator_EmptyPool(t *testing.T) {
	_, err := NewRouterAllocator([]ModelPoolEntry{})
	if err == nil {
		t.Errorf("Expected error for empty pool")
	}
}

// TestRouterAllocator_DuplicateNames 验证重复名称返回 error
func TestRouterAllocator_DuplicateNames(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("same", "key1", "http://a", "OpenAI"),
		*NewModelPoolEntry("same", "key2", "http://b", "OpenAI"),
	}
	_, err := NewRouterAllocator(pool)
	if err == nil {
		t.Errorf("Expected error for duplicate model names")
	}
}

// TestRouterAllocator_StateDict 验证状态快照仅含摘要
func TestRouterAllocator_StateDict(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "http://x", "OpenAI")}
	alloc, _ := NewRouterAllocator(pool)
	state := alloc.StateDict()
	if state["pool_digest"] != poolDigest(pool) {
		t.Errorf("Digest mismatch")
	}
	if _, ok := state["index"]; ok {
		t.Errorf("RouterAllocator should not have index in state")
	}
}

// ──────────────────────────── BuildModelAllocatorForPool 测试 ────────────────────────────

// TestBuildModelAllocatorForPool_RoundRobin 验证 round_robin 策略
func TestBuildModelAllocatorForPool_RoundRobin(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "url", "OpenAI")}
	alloc := BuildModelAllocatorForPool(pool, "round_robin", "team")
	if alloc == nil {
		t.Error("Expected non-nil allocator")
	}
	// 验证是 RoundRobin 类型
	a := alloc.Allocate("")
	if a == nil {
		t.Error("Expected allocation")
	}
}

// TestBuildModelAllocatorForPool_ByModelName 验证 by_model_name 策略
func TestBuildModelAllocatorForPool_ByModelName(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "url", "OpenAI")}
	alloc := BuildModelAllocatorForPool(pool, "by_model_name", "team")
	if alloc == nil {
		t.Error("Expected non-nil allocator")
	}
}

// TestBuildModelAllocatorForPool_Router 验证 router 策略
func TestBuildModelAllocatorForPool_Router(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "url", "OpenAI")}
	alloc := BuildModelAllocatorForPool(pool, "router", "team")
	if alloc == nil {
		t.Error("Expected non-nil allocator")
	}
}

// TestBuildModelAllocatorForPool_未知策略 验证未知策略回退 nil
func TestBuildModelAllocatorForPool_未知策略(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "url", "OpenAI")}
	alloc := BuildModelAllocatorForPool(pool, "unknown_strategy", "team")
	if alloc != nil {
		t.Error("Expected nil for unknown strategy")
	}
}

// ──────────────────────────── ResolveMemberModelFromPool 测试 ────────────────────────────

// TestResolveMemberModelFromPool_正常 验证正常解析
func TestResolveMemberModelFromPool_正常(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("model-a", "key1", "http://a", "OpenAI"),
		*NewModelPoolEntry("model-a", "key2", "http://a2", "OpenAI"),
	}
	result := ResolveMemberModelFromPool(pool, "model-a", 1)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.ModelClientConfig.APIBase != "http://a2" {
		t.Errorf("Expected http://a2, got %s", result.ModelClientConfig.APIBase)
	}
}

// TestResolveMemberModelFromPool_索引越界 验证越界回退 index=0
func TestResolveMemberModelFromPool_索引越界(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("model-a", "key1", "http://a", "OpenAI"),
		*NewModelPoolEntry("model-a", "key2", "http://a2", "OpenAI"),
	}
	result := ResolveMemberModelFromPool(pool, "model-a", 99)
	if result == nil {
		t.Fatal("Expected non-nil result (fallback to index 0)")
	}
	if result.ModelClientConfig.APIBase != "http://a" {
		t.Errorf("Expected fallback to index 0, got %s", result.ModelClientConfig.APIBase)
	}
}

// TestResolveMemberModelFromPool_未知名称 验证未知名称返回 nil
func TestResolveMemberModelFromPool_未知名称(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("qwen", "key", "url", "OpenAI")}
	result := ResolveMemberModelFromPool(pool, "unknown", 0)
	if result != nil {
		t.Error("Expected nil for unknown name")
	}
}

// TestPoolDigest 验证池摘要生成
func TestPoolDigest(t *testing.T) {
	pool := []ModelPoolEntry{
		*NewModelPoolEntry("model-a", "key", "url-a", "OpenAI"),
		*NewModelPoolEntry("model-b", "key", "url-b", "OpenAI"),
	}

	digest1 := poolDigest(pool)
	digest2 := poolDigest(pool)
	if digest1 != digest2 {
		t.Error("相同池应产生相同摘要")
	}
	if digest1 == "" {
		t.Error("摘要不应为空")
	}

	// 不同池应产生不同摘要
	pool2 := []ModelPoolEntry{
		*NewModelPoolEntry("model-c", "key", "url-c", "OpenAI"),
	}
	digest3 := poolDigest(pool2)
	if digest1 == digest3 {
		t.Error("不同池应产生不同摘要")
	}
}

// TestAllocation_ToTeamModelConfig_无Metadata 验证无 metadata 时默认行为。
func TestAllocation_ToTeamModelConfig_无Metadata(t *testing.T) {
	entry := NewModelPoolEntry("qwen-max", "key2", "https://api.example.com", "OpenAI")
	a := Allocation{Entry: *entry, GroupIndex: 1}
	cfg := a.ToTeamModelConfig()

	if cfg.ModelClientConfig.ClientProvider != "OpenAI" {
		t.Errorf("ClientProvider = %q, want %q", cfg.ModelClientConfig.ClientProvider, "OpenAI")
	}
}

// TestRouterAllocator_LoadStateDict 验证 RouterAllocator.LoadStateDict 空操作不 panic
func TestRouterAllocator_LoadStateDict(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("m", "key", "http://x", "OpenAI")}
	alloc, _ := NewRouterAllocator(pool)
	// 应不 panic
	alloc.LoadStateDict(map[string]any{"pool_digest": "abc"})
}

// TestMarshalForSignature 测试序列化辅助函数
func TestMarshalForSignature(t *testing.T) {
	result := marshalForSignature(map[string]string{"key": "value"})
	if result != `{"key":"value"}` {
		t.Errorf("marshalForSignature = %q, want {\"key\":\"value\"}", result)
	}
}

// TestMarshalForSignature_空map 测试空 map
func TestMarshalForSignature_空map(t *testing.T) {
	result := marshalForSignature(map[string]string{})
	if result != `{}` {
		t.Errorf("marshalForSignature = %q, want {}", result)
	}
}
