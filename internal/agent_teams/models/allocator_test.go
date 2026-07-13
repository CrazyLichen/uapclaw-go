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

// TestPoolDigest 验证池摘要生成。
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
	// 验证 nil 可以赋值给 ModelAllocator
	var allocator ModelAllocator = nil
	_ = allocator
}
