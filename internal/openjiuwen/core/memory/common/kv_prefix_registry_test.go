//go:build test

package common

import (
	"testing"
)

func TestNewKvPrefixRegistry(t *testing.T) {
	r := NewKvPrefixRegistry()
	if r == nil {
		t.Fatal("NewKvPrefixRegistry 返回 nil")
	}
	if len(r.GetAllPrefixes()) != 0 {
		t.Errorf("新建注册表应为空，得到 %d 个前缀", len(r.GetAllPrefixes()))
	}
}

func TestKvPrefixRegistry_RegisterCurrent(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterCurrent("user_var")
	if err != nil {
		t.Fatalf("RegisterCurrent 返回 error: %v", err)
	}
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 1 {
		t.Fatalf("期望 1 个前缀，得到 %d", len(prefixes))
	}
	found := false
	for _, p := range prefixes {
		if p == "user_var" {
			found = true
		}
	}
	if !found {
		t.Error("期望包含 user_var 前缀")
	}
}

func TestKvPrefixRegistry_RegisterCurrent_Empty(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterCurrent("")
	if err == nil {
		t.Fatal("空前缀应返回 error")
	}
}

func TestKvPrefixRegistry_RegisterCurrent_Whitespace(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterCurrent("  ")
	if err == nil {
		t.Fatal("纯空白前缀应返回 error")
	}
}

func TestKvPrefixRegistry_RegisterLegacy(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterLegacy("old_prefix")
	if err != nil {
		t.Fatalf("RegisterLegacy 返回 error: %v", err)
	}
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 1 {
		t.Fatalf("期望 1 个前缀，得到 %d", len(prefixes))
	}
}

func TestKvPrefixRegistry_RegisterLegacy_Empty(t *testing.T) {
	r := NewKvPrefixRegistry()
	err := r.RegisterLegacy("")
	if err == nil {
		t.Fatal("空前缀应返回 error")
	}
}

func TestKvPrefixRegistry_RegisterCurrent_Duplicate(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterCurrent("user_var")
	_ = r.RegisterCurrent("user_var") // 重复注册不应报错
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 1 {
		t.Errorf("重复注册应只有 1 个前缀，得到 %d", len(prefixes))
	}
}

func TestKvPrefixRegistry_RegisterLegacy_NotInCurrent(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterLegacy("legacy_prefix")
	// legacy 前缀只出现在 allPrefixes 中，不出现在 currentPrefixes 中
	r.mu.RLock()
	_, inCurrent := r.currentPrefixes["legacy_prefix"]
	r.mu.RUnlock()
	if inCurrent {
		t.Error("legacy 前缀不应出现在 currentPrefixes 中")
	}
}

func TestKvPrefixRegistry_GetAllPrefixes_Copy(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterCurrent("a")
	_ = r.RegisterLegacy("b")
	prefixes := r.GetAllPrefixes()
	prefixes[0] = "modified" // 修改返回值不应影响原注册表
	if len(r.GetAllPrefixes()) != 2 {
		t.Error("修改返回值不应影响原注册表")
	}
}

func TestKvPrefixRegistry_Unregister(t *testing.T) {
	r := NewKvPrefixRegistry()
	_ = r.RegisterCurrent("user_var")
	r.Unregister("user_var")
	prefixes := r.GetAllPrefixes()
	if len(prefixes) != 0 {
		t.Errorf("注销后期望 0 个前缀，得到 %d", len(prefixes))
	}
}

func TestKvPrefixRegistry_Unregister_Nonexistent(t *testing.T) {
	r := NewKvPrefixRegistry()
	r.Unregister("nonexistent") // 不存在的前缀不报错
	if len(r.GetAllPrefixes()) != 0 {
		t.Error("注销不存在的前缀不应产生副作用")
	}
}

func TestKVPrefixRegistry_GlobalInstance(t *testing.T) {
	// 验证全局实例可用
	err := KVPrefixRegistry.RegisterCurrent("test_global_prefix")
	if err != nil {
		t.Fatalf("全局实例 RegisterCurrent 返回 error: %v", err)
	}
	// 清理
	KVPrefixRegistry.Unregister("test_global_prefix")
}
