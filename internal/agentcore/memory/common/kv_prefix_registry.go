package common

import (
	"fmt"
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KvPrefixRegistry KV 前缀注册表，管理记忆模块使用的 KV 存储键前缀。
//
// 允许记忆模块注册当前和旧版前缀，使 KV 迁移器能动态发现正在使用的前缀，
// 而无需硬编码。当模块在版本演进中添加或移除前缀时，迁移器能自动适应。
//
// 对应 Python: openjiuwen/core/memory/common/kv_prefix_registry.py (KvPrefixRegistry)
type KvPrefixRegistry struct {
	// mu 保护并发访问
	mu sync.RWMutex
	// allPrefixes 所有前缀集合（current + legacy）
	allPrefixes map[string]bool
	// currentPrefixes 当前使用的前缀集合
	currentPrefixes map[string]bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// KVPrefixRegistry 全局 KV 前缀注册表实例。
	// 对齐 Python: kv_prefix_registry = KvPrefixRegistry()
	KVPrefixRegistry = NewKvPrefixRegistry()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKvPrefixRegistry 创建新的 KV 前缀注册表。
func NewKvPrefixRegistry() *KvPrefixRegistry {
	return &KvPrefixRegistry{
		allPrefixes:     make(map[string]bool),
		currentPrefixes: make(map[string]bool),
	}
}

// RegisterCurrent 注册一个当前（活跃）键前缀。
//
// 如果前缀为空或纯空白字符，返回 error。已存在的前缀不会重复添加。
//
// 对应 Python: KvPrefixRegistry.register_current
func (r *KvPrefixRegistry) RegisterCurrent(prefix string) error {
	if prefix == "" || strings.TrimSpace(prefix) == "" {
		return fmt.Errorf("前缀不能为空或仅包含空白字符: %q", prefix)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentPrefixes[prefix] = true
	r.allPrefixes[prefix] = true
	return nil
}

// RegisterLegacy 注册一个旧版（已弃用）键前缀，用于迁移检测。
//
// 如果前缀为空或纯空白字符，返回 error。已存在的前缀不会重复添加。
//
// 对应 Python: KvPrefixRegistry.register_legacy
func (r *KvPrefixRegistry) RegisterLegacy(prefix string) error {
	if prefix == "" || strings.TrimSpace(prefix) == "" {
		return fmt.Errorf("前缀不能为空或仅包含空白字符: %q", prefix)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allPrefixes[prefix] = true
	return nil
}

// GetAllPrefixes 获取所有已注册的前缀（current + legacy）。
//
// 返回前缀切片的副本，调用方可以安全修改。
//
// 对应 Python: KvPrefixRegistry.get_all_prefixes
func (r *KvPrefixRegistry) GetAllPrefixes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.allPrefixes))
	for prefix := range r.allPrefixes {
		result = append(result, prefix)
	}
	return result
}

// Unregister 从 current 和 all 集合中移除指定前缀。
//
// 对应 Python: KvPrefixRegistry.unregister
func (r *KvPrefixRegistry) Unregister(prefix string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.allPrefixes, prefix)
	delete(r.currentPrefixes, prefix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
