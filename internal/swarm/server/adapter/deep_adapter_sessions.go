package adapter

import (
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// 本文件承载会话级 adapter 分身。
//
// Python 的 JiuWenSwarmDeepAdapter 是双形态单类：根持有
//
//	self._session_adapters: dict[str, JiuWenSwarmDeepAdapter]
//
// 每个会话由 type(self)() 造一个 child 独享 DeepAgent（含自己的 rail 树、
// tool 表、reactAgent.context_engine）。根只做路由与模板持有。
//
// Go 侧此前只有单个 instance，会话仅作运行参数，后果：
//   - 25.8 的 stop_subagent_runtimes 没有"该会话的 DeepAgent"可停；
//   - 会话删除时 CleanupSessionAdapter 只能清一个空的 map；
//   - 一个会话切模型/切 rail 会改到共享的 ReAct 图，影响其它会话。
//
// 对应 Python: server/runtime/agent_adapter/interface_deep.py 的会话分身段，
// 见 _new_session_scoped_adapter（:2678）起。

// ──────────────────────────── 结构体 ────────────────────────────

// sessionAdapterCache 会话级子适配器缓存。
// Python: interface_deep.py _session_adapters + _session_adapter_last_used
type sessionAdapterCache struct {
	// mu 保护以下字段
	mu sync.RWMutex
	// entries 会话标识 → 子适配器
	//
	// 元素类型是 AgentAdapter 而非 DeepAdapter：CodeAdapter 的 child 必须是
	// 完整的 *CodeAdapter（对齐 Python type(self)()），否则 code 专属的
	// 20+ 方法覆写会丢失、rails 静默降级成 deep 模式。
	entries map[string]AgentAdapter
	// lastUsed 会话标识 → 最后使用时刻
	lastUsed map[string]time.Time
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newSessionAdapterCache 创建会话级缓存。
func newSessionAdapterCache() *sessionAdapterCache {
	return &sessionAdapterCache{
		entries:  map[string]AgentAdapter{},
		lastUsed: map[string]time.Time{},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSessionScopedAdapter 为一个会话造专属子适配器。
// Python: interface_deep.py _new_session_scoped_adapter（:2678-2698）
//
// 步骤：
//  1. 用工厂造同类新实例（type(self)()）
//  2. 标记为会话级子适配器
//  3. 继承父的共享依赖
func (d *DeepAdapter) newSessionScopedAdapter(sessionID string) (AgentAdapter, error) {
	// 步骤 1: type(self)() → CreateAdapter("harness", d.childMode)
	// Python: :2680 adapter = type(self)()
	if d.sessionChildFactory == nil {
		return nil, fmt.Errorf("sessionChildFactory 未注入，mode=%q", d.childMode)
	}
	child, err := d.sessionChildFactory()
	if err != nil {
		return nil, fmt.Errorf("创建会话子适配器失败: %w", err)
	}

	// 步骤 2: mark_as_session_scoped
	// Python: :2681 adapter.mark_as_session_scoped(session_id)
	child.MarkAsSessionScoped(sessionID)

	// 步骤 3: 继承父的共享依赖
	// Python: :2684-2697 七项（channel_id / cron / personal_context /
	// skill_manager / heartbeat / model_selection_can_access / 权限 notifier）。
	// Go 侧目前只有 skillManager 有等价字段与注入方法，其余六项见 spec §8 D-4。
	if d.skillManager != nil {
		if setter, ok := child.(interface{ SetSkillManager(*skill.SkillManager) }); ok {
			setter.SetSkillManager(d.skillManager)
		}
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("child_mode", d.childMode).
		Msg("session scoped adapter created")

	return child, nil
}

// remember 记住某会话的子适配器。
func (c *sessionAdapterCache) remember(sessionID string, adapter AgentAdapter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sessionID] = adapter
	c.lastUsed[sessionID] = time.Now()
}

// get 取某会话的子适配器（不存在返回 nil）。
func (c *sessionAdapterCache) get(sessionID string) AgentAdapter {
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
