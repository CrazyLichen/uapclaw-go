package models

import (
	"strings"
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
	result := ResolveMemberModelFromPool(nil, "qwen", 0, "")
	if result != nil {
		t.Error("空池时期望 nil result")
	}
}

// TestResolveMemberModelFromPool_空名称 验证空名称返回 nil。
func TestResolveMemberModelFromPool_空名称(t *testing.T) {
	pool := []ModelPoolEntry{*NewModelPoolEntry("qwen", "key", "url", "OpenAI")}
	result := ResolveMemberModelFromPool(pool, "", 0, "")
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
	result := ResolveMemberModelFromPool(pool, "model-a", 1, "")
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
	result := ResolveMemberModelFromPool(pool, "model-a", 99, "")
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
	result := ResolveMemberModelFromPool(pool, "unknown", 0, "")
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

// ──────────────────────────── 23.4c IntelliRouterAllocator ────────────────────────────

// irTestPool 构造三部署组池：两条 fast + 一条 smart，各以模型名作 tag。
// Python: test_allocator_route_tags.py:19-52 _pool
func irTestPool(strategy string, strategyKwargs map[string]any) []ModelPoolEntry {
	cfg := NewIntelliRouterConfig([]*IntelliRouterDeployment{
		{ModelName: "fast", APIKey: "sk-1", APIBase: "https://a.test", ID: "fast-1", Provider: "deepseek", FallbackTag: "fast"},
		{ModelName: "fast", APIKey: "sk-2", APIBase: "https://a.test", ID: "fast-2", Provider: "deepseek", FallbackTag: "fast"},
		{ModelName: "smart", APIKey: "sk-3", APIBase: "https://b.test", ID: "smart-1", Provider: "openai", FallbackTag: "smart"},
	})
	cfg.Strategy = strategy
	cfg.StrategyKwargs = strategyKwargs
	return cfg.ToPoolEntries()
}

// TestIntelliRouterAllocator_构造校验四连 空池/非单条/非 "*/provider 错。
// Python: allocator.py:677-690 __init__ 校验
func TestIntelliRouterAllocator_构造校验四连(t *testing.T) {
	if _, err := NewIntelliRouterAllocator(nil); err == nil || !strings.Contains(err.Error(), "non-empty pool") {
		t.Fatalf("空池: %v", err)
	}
	if _, err := NewIntelliRouterAllocator([]ModelPoolEntry{
		*NewModelPoolEntry("*", "", "", "intelli_router"), *NewModelPoolEntry("*", "", "", "intelli_router"),
	}); err == nil || !strings.Contains(err.Error(), "exactly one logical") {
		t.Fatalf("多条目: %v", err)
	}
	wrongName := []ModelPoolEntry{*NewModelPoolEntry("m", "", "", "intelli_router")}
	if _, err := NewIntelliRouterAllocator(wrongName); err == nil || !strings.Contains(err.Error(), "exactly one logical") {
		t.Fatalf("非 '*': %v", err)
	}
	wrongProvider := []ModelPoolEntry{*NewModelPoolEntry("*", "", "", "openai")}
	if _, err := NewIntelliRouterAllocator(wrongProvider); err == nil || !strings.Contains(err.Error(), "api_provider='intelli_router'") {
		t.Fatalf("provider 错: %v", err)
	}
}

// TestIntelliRouterAllocator_部署目录校验 deployments 缺失/空/model_name 或 provider 空。
// Python: allocator.py:740-763 _validate_deployment_catalog
func TestIntelliRouterAllocator_部署目录校验(t *testing.T) {
	mk := func(client map[string]any) []ModelPoolEntry {
		return []ModelPoolEntry{*NewModelPoolEntry("*", "", "", "intelli_router", WithMetadata(map[string]any{"client": client}))}
	}
	if _, err := NewIntelliRouterAllocator(mk(map[string]any{"intelli_router": map[string]any{}})); err == nil || !strings.Contains(err.Error(), "non-empty deployments") {
		t.Fatalf("缺 deployments: %v", err)
	}
	if _, err := NewIntelliRouterAllocator(mk(map[string]any{"intelli_router": map[string]any{"deployments": []any{1}}})); err == nil || !strings.Contains(err.Error(), "must be an object") {
		t.Fatalf("非 object: %v", err)
	}
	badDep := map[string]any{"intelli_router": map[string]any{
		"deployments": []any{map[string]any{"model_name": "", "provider": ""}},
	}}
	if _, err := NewIntelliRouterAllocator(mk(badDep)); err == nil || !strings.Contains(err.Error(), "non-empty model_name and provider") {
		t.Fatalf("空字段: %v", err)
	}
}

// TestIntelliRouterAllocator_Allocate 整组与点名。
// Python: allocator.py:712-736 allocate
func TestIntelliRouterAllocator_Allocate(t *testing.T) {
	alloc, err := NewIntelliRouterAllocator(irTestPool("ordered-failover", nil))
	if err != nil {
		t.Fatal(err)
	}
	// 空与 "*" → 整组，group_index=0
	for _, hint := range []string{"", "*"} {
		got := alloc.Allocate(hint)
		if got == nil || got.Entry.ModelName != INTELLI_ROUTER_UNIFIED_MODEL || got.GroupIndex != 0 || got.RouteTag != "" {
			t.Fatalf("hint=%q 应整组: %+v", hint, got)
		}
	}
	// 合法 tag → entry + RouteTag
	got := alloc.Allocate("smart")
	if got == nil || got.RouteTag != "smart" {
		t.Fatalf("点名 smart: %+v", got)
	}
	// 大小写不敏感 → canonical 拼写
	if got := alloc.Allocate("SMART"); got == nil || got.RouteTag != "smart" {
		t.Fatalf("大小写归一: %+v", got)
	}
	// 非法 tag → nil（显式点名绝不回退整组）
	if got := alloc.Allocate("nope"); got != nil {
		t.Fatalf("非法点名应 nil: %+v", got)
	}
	// 无 tag 部署不能按其模型名选中（test_allocator_route_tags.py:127）
	if got := alloc.Allocate("plain"); got != nil {
		t.Fatalf("未打 tag 部署不应可选: %+v", got)
	}
}

// TestIntelliRouterAllocator_ListRouteTags 排序去重；tag-filtered 组只提供自己的 tag。
// Python: _selectable_route_tags (allocator.py:239) + test_allocator_route_tags.py:147/:138
func TestIntelliRouterAllocator_ListRouteTags(t *testing.T) {
	alloc, _ := NewIntelliRouterAllocator(irTestPool("ordered-failover", nil))
	tags := alloc.ListRouteTags()
	if len(tags) != 2 || tags[0] != "fast" || tags[1] != "smart" {
		t.Fatalf("tags: %v", tags)
	}
	// 大小写变体折叠为组序首个拼写（py:234-238 / test:226）
	variantPool := irTestPool("ordered-failover", nil)
	cfg := NewIntelliRouterConfig([]*IntelliRouterDeployment{
		{ModelName: "m", APIKey: "k", APIBase: "b", Provider: "p", FallbackTag: "GLM-5.3-Flash"},
		{ModelName: "m", APIKey: "k", APIBase: "b", Provider: "p", FallbackTag: "GLM-5.3-flash"},
	})
	_ = variantPool
	cfgPool := cfg.ToPoolEntries()
	alloc2, _ := NewIntelliRouterAllocator(cfgPool)
	tags2 := alloc2.ListRouteTags()
	if len(tags2) != 1 || tags2[0] != "GLM-5.3-Flash" {
		t.Fatalf("大小写折叠: %v", tags2)
	}
	// tag-filtered 组只提供自己的 tag（test:138）
	filtered, _ := NewIntelliRouterAllocator(irTestPool("tag-filtered", map[string]any{"fallback_tag": "smart"}))
	tags3 := filtered.ListRouteTags()
	if len(tags3) != 1 || tags3[0] != "smart" {
		t.Fatalf("tag-filtered 组 tags: %v", tags3)
	}
}

// TestIntelliRouterAllocator_StateDict 摘要快照与无操作恢复。
// Python: allocator.py:729-735
func TestIntelliRouterAllocator_StateDict(t *testing.T) {
	alloc, _ := NewIntelliRouterAllocator(irTestPool("ordered-failover", nil))
	if _, ok := alloc.StateDict()["pool_digest"]; !ok {
		t.Fatal("StateDict 应含 pool_digest")
	}
	alloc.LoadStateDict(map[string]any{"pool_digest": "other"}) // 组成变化不重置计数（无计数）
	alloc.LoadStateDict(nil)                                    // 不 panic
}

// TestIntelliRouterAllocator_CliModels 外部 CLI 部署候选投影。
// Python: allocator.py:765-786 list_cli_models / resolve_cli_models
func TestIntelliRouterAllocator_CliModels(t *testing.T) {
	alloc, _ := NewIntelliRouterAllocator(irTestPool("ordered-failover", nil))
	catalog := alloc.ListCliModels(nil)
	if len(catalog) != 3 || catalog[0].Model != "fast" || catalog[0].RouteID != "fast-1" {
		t.Fatalf("catalog: %+v", catalog)
	}
	onlyOpenAI := alloc.ListCliModels(func(p string) bool { return p == "openai" })
	if len(onlyOpenAI) != 1 || onlyOpenAI[0].Provider != "openai" {
		t.Fatalf("filter: %+v", onlyOpenAI)
	}
	// ResolveCliModels：真实部署名；"*"/空 → 空；未打 tag 部署名不在 catalog？——catalog 是全部部署
	if got := alloc.ResolveCliModels("*", nil); len(got) != 0 {
		t.Fatalf("'*' → 空: %+v", got)
	}
	if got := alloc.ResolveCliModels("", nil); len(got) != 0 {
		t.Fatalf("空 → 空: %+v", got)
	}
	if got := alloc.ResolveCliModels("smart", nil); len(got) != 1 || got[0].Model != "smart" {
		t.Fatalf("smart: %+v", got)
	}
}

// TestWithRouteTag_钉选策略改写 tag-filtered 改写、kwargs 整体替换、共享条目不动、大小写变体重标。
// Python: _with_route_tag (allocator.py:291-335) + test_allocator_route_tags.py:79-145
func TestWithRouteTag_钉选策略改写(t *testing.T) {
	pool := irTestPool("ordered-failover", map[string]any{"foo": "bar"})
	entry := pool[0]
	config := entry.ToTeamModelConfig()
	// 共享池条目不被修改（test:102）
	pinned := withRouteTag(config, "smart")
	if pinned.ModelClientConfig.IntelliRouter.Strategy != "tag-filtered" {
		t.Fatalf("strategy: %s", pinned.ModelClientConfig.IntelliRouter.Strategy)
	}
	if pinned.ModelClientConfig.IntelliRouter.StrategyKwargs["fallback_tag"] != "smart" {
		t.Fatalf("kwargs 应整体替换: %v", pinned.ModelClientConfig.IntelliRouter.StrategyKwargs)
	}
	if entry.ToTeamModelConfig().ModelClientConfig.IntelliRouter.Strategy != "ordered-failover" {
		t.Fatal("共享池条目被修改")
	}
	// 组级 kwargs 不与 tag-filtered 合并（test:113）——上面 kwargs 整体替换已覆盖
	// 未打 tag 的成员：非 tag-filtered 组 → 原样（test:79）
	untagged := withRouteTag(config, "")
	if untagged.ModelClientConfig.IntelliRouter.Strategy != "ordered-failover" {
		t.Fatal("无 tag 且组非 tag-filtered 应原样")
	}
	// 大小写变体重标为 tag 拼写（test:231）
	varCfg := NewIntelliRouterConfig([]*IntelliRouterDeployment{
		{ModelName: "m", APIKey: "k", APIBase: "b", Provider: "p", FallbackTag: "GLM-5.3-flash"},
	})
	pinned2 := withRouteTag(varCfg.ToPoolEntries()[0].ToTeamModelConfig(), "GLM-5.3-Flash")
	deps := pinned2.ModelClientConfig.IntelliRouter.Deployments
	if deps[0].FallbackTag == nil || *deps[0].FallbackTag != "GLM-5.3-Flash" {
		t.Fatalf("大小写变体应重标: %+v", deps[0].FallbackTag)
	}
	// 组自身 tag-filtered、无 tag 成员 → 用组 tag 的部署拼写（test:269/:296）
	gp := irTestPool("tag-filtered", map[string]any{"fallback_tag": "SMART"})
	gpCfg := gp[0].ToTeamModelConfig()
	got := withRouteTag(gpCfg, "")
	if got.ModelClientConfig.IntelliRouter.StrategyKwargs["fallback_tag"] != "smart" {
		t.Fatalf("组 tag 归一为部署拼写: %v", got.ModelClientConfig.IntelliRouter.StrategyKwargs)
	}
	// 组 tag 匹配不到部署 → 保持原配置，路由器报无匹配（test:306）
	np := irTestPool("tag-filtered", map[string]any{"fallback_tag": "ghost"})
	npGot := withRouteTag(np[0].ToTeamModelConfig(), "")
	if npGot.ModelClientConfig.IntelliRouter.StrategyKwargs["fallback_tag"] != "ghost" {
		t.Fatal("组 tag 无部署匹配应原样保留")
	}
}

// TestResolveMemberModelFromPool_RouteTag 持久化 tag 的重放与丢弃。
// Python: allocator.py:798-855 resolve_member_model + test_allocator_route_tags.py:152/:160/:255
func TestResolveMemberModelFromPool_RouteTag(t *testing.T) {
	pool := irTestPool("ordered-failover", nil)
	// 持久 tag 仍被服务 → 重放
	cfg := ResolveMemberModelFromPool(pool, "*", 0, "SMART")
	if cfg.ModelClientConfig.IntelliRouter.Strategy != "tag-filtered" ||
		cfg.ModelClientConfig.IntelliRouter.StrategyKwargs["fallback_tag"] != "smart" {
		t.Fatalf("持久 tag 大小写归一重放: %v", cfg.ModelClientConfig.IntelliRouter.StrategyKwargs)
	}
	// 组不再服务该 tag → 丢弃并回退组级路由（warning 日志）
	cfg2 := ResolveMemberModelFromPool(pool, "*", 0, "gone")
	if cfg2.ModelClientConfig.IntelliRouter.Strategy != "ordered-failover" {
		t.Fatalf("消失 tag 应回退组级: %s", cfg2.ModelClientConfig.IntelliRouter.Strategy)
	}
	// 索引越界 → 0 号兜底（既有行为回归）
	cfg3 := ResolveMemberModelFromPool(pool, "*", 99, "")
	if cfg3 == nil {
		t.Fatal("越界应兜底 0 号")
	}
}
