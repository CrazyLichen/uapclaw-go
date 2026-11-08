package adapter

import (
	"sync"
	"time"
)

// 本文件承载会话级 adapter 分层。
//
// Python 的 JiuWenSwarmDeepAdapter 按会话分身：
//
//	self._session_adapters: dict[str, JiuWenSwarmDeepAdapter]
//	self._session_adapter_locks / _session_adapter_last_used
//
// 每个子适配器装一份独立的 DeepAgent 与子代理控制表。Go 侧此前只有单个
// instance，会话仅作运行参数，于是会话删除时"该释放的那一份"无处可寻——
// 这正是 A2（subagent 停止）与 A3（cleanup_session_runtime）右半的结构性前置。
//
// 对应 Python: server/runtime/agent_adapter/interface_deep.py:2126 起。

// ──────────────────────────── 结构体 ────────────────────────────

// sessionAdapterCache 会话级子适配器缓存。
// Python: interface_deep.py _session_adapters + _session_adapter_last_used
type sessionAdapterCache struct {
	// mu 保护以下字段
	mu sync.RWMutex
	// entries 会话标识 → 子适配器（Go 侧以 map 承载等价句柄）
	//
	// TODO(回填): 该 map 目前无类型约束、无生产写入方（只有测试塞空 map），
	// 是为了保持 API 面完整的占位。真正落地依赖两块未迁移的能力：
	//   1. DeepAgent 按会话分身（Python 每个子适配器一份 _instance + 子代理控制表）；
	//   2. 子代理运行时（25.8，subagents/ 整层 Go 侧 0 实现）。
	// 两者落地后应改为具体类型（如 *DeepAdapter 或 SessionAdapter 结构体），
	// 并在 ensureAdapter 路径上调用 remember。计划登记见 10.3.28e 的 D-17。
	entries map[string]map[string]any
	// lastUsed 会话标识 → 最后使用时刻
	lastUsed map[string]time.Time
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newSessionAdapterCache 创建会话级缓存。
func newSessionAdapterCache() *sessionAdapterCache {
	return &sessionAdapterCache{
		entries:  map[string]map[string]any{},
		lastUsed: map[string]time.Time{},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// remember 记住某会话的子适配器。
//
// TODO(回填): 生产代码尚无调用方，见 entries 字段的说明。
func (c *sessionAdapterCache) remember(sessionID string, entry map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sessionID] = entry
	c.lastUsed[sessionID] = time.Now()
}

// get 取某会话的子适配器（不存在返回 nil）。
func (c *sessionAdapterCache) get(sessionID string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[sessionID]
}

// has 判断某会话是否有子适配器。
func (c *sessionAdapterCache) has(sessionID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.entries[sessionID]
	return ok
}

// touch 刷新最后使用时刻（不创建条目）。
func (c *sessionAdapterCache) touch(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[sessionID]; ok {
		c.lastUsed[sessionID] = time.Now()
	}
}

// forget 移除某会话的子适配器。
func (c *sessionAdapterCache) forget(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, sessionID)
	delete(c.lastUsed, sessionID)
}

// cleanup 移除并报告是否清到了东西。
// Python: interface_deep.py cleanup_session_adapter 的缓存清理部分
func (c *sessionAdapterCache) cleanup(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[sessionID]
	delete(c.entries, sessionID)
	delete(c.lastUsed, sessionID)
	return ok
}

// count 当前缓存的会话数。
func (c *sessionAdapterCache) count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
