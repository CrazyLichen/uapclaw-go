package adapter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
// tool 表、reactAgent.context_engine）。根只做路由与模板持有；chat 与
// 会话态 RPC 在入口分流到 child，非会话态 RPC 在根上执行。
//
// Go 侧此前只有单个 instance，会话仅作运行参数，后果：
//   - 25.8 的 stop_subagent_runtimes 没有"该会话的 DeepAgent"可停；
//   - 会话删除时 CleanupSessionAdapter 只能清一个空的 map；
//   - 一个会话切模型/切 rail 会改到共享的 ReAct 图，影响其它会话。
//
// 对应 Python: server/runtime/agent_adapter/interface_deep.py 的会话分身段，
// 见 _new_session_scoped_adapter（:2678）起。

// ──────────────────────────── 结构体 ────────────────────────────

// sessionAdapterGuard 会话级建/清理互斥锁，并记录等待者数量。
// Python: _session_adapter_locks[sid]（asyncio.Lock）
//
// 需要 waiters 是因为 Python 用 lock.locked() + lock._waiters 判断
// "是否有请求正在建/重载该 child"，Go 的 sync.Mutex 无内省能力。
// 语义一致：waiters > 0 即"有人在等"（spec §8 D-3）。
type sessionAdapterGuard struct {
	// mu 互斥锁本体
	mu sync.Mutex
	// waiters 等待者数量
	waiters int32
}

// sessionAdapterOptions getOrCreateSessionAdapter 的可选参数。
// Python 对应 _get_or_create_session_adapter 的关键字参数；本节只落
// reserve_activity，其余（model_name / pending_mcp_scan_names /
// history_before_request_id / external_harness_binding 等）随 7 组重建判据回填。
type sessionAdapterOptions struct {
	// ReserveActivity 是否把本次请求登记为该会话的在飞任务
	ReserveActivity bool
	// RequestID 在飞任务标识（ReserveActivity 为真时必填）
	RequestID string
}

// sessionAdapterCache 会话级子适配器缓存。
// Python: interface_deep.py _session_adapters（:2126）+ _session_adapter_locks
// + _session_adapter_last_used
type sessionAdapterCache struct {
	// mu 保护以下全部字段
	mu sync.RWMutex
	// entries 会话标识 → 子适配器
	//
	// 元素类型是 AgentAdapter 而非 DeepAdapter：CodeAdapter 的 child 必须是
	// 完整的 *CodeAdapter（对齐 Python type(self)()），否则 code 专属的
	// 20+ 方法覆写会丢失、rails 静默降级成 deep 模式。
	entries map[string]AgentAdapter
	// guards 会话标识 → 建/清理互斥锁
	guards map[string]*sessionAdapterGuard
	// lastUsed 会话标识 → 最后使用时刻
	lastUsed map[string]time.Time
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionAdapterIdleTTLSec 子适配器空闲淘汰阈值（秒）
	// Python: SESSION_ADAPTER_IDLE_TTL_SEC（:1882）
	sessionAdapterIdleTTLSec = 2 * 60 * 60
	// sessionAdapterEvictBatchSize 单次淘汰批次上限
	// Python: SESSION_ADAPTER_EVICT_BATCH_SIZE（:1883）
	sessionAdapterEvictBatchSize = 3
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newSessionAdapterCache 创建会话级缓存。
func newSessionAdapterCache() *sessionAdapterCache {
	return &sessionAdapterCache{
		entries:  map[string]AgentAdapter{},
		guards:   map[string]*sessionAdapterGuard{},
		lastUsed: map[string]time.Time{},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionAdapterKey 归一化会话标识，空值回退 "default"。
// Python: interface_deep.py _session_adapter_key（:2469-2471）
func sessionAdapterKey(sessionID *string) string {
	sid := ""
	if sessionID != nil {
		sid = strings.TrimSpace(*sessionID)
	}
	if sid == "" {
		return "default"
	}
	return sid
}

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

// getOrCreateSessionAdapter 取会话的子适配器，缺失则创建。
// Python: interface_deep.py _get_or_create_session_adapter（:3921-4434）
//
// 本节实现骨架：锁、命中复用、未命中创建、touch、登记在飞任务。
// 7 组重建判据留位后回填，见 spec §4.6。
func (d *DeepAdapter) getOrCreateSessionAdapter(
	ctx context.Context,
	sessionID string,
	opts sessionAdapterOptions,
) (AgentAdapter, error) {
	// 步骤 1: child 上直接返回自身
	// Python: :3941-3943
	if d.isSessionScoped {
		d.touchSessionAdapter(sessionID)
		return d, nil
	}

	// 步骤 2: 归一化
	// Python: :3945
	sid := sessionAdapterKey(&sessionID)

	// 步骤 3-4: 取该会话的锁并加锁
	// Python: :3949 lock = self._session_adapter_locks.setdefault(sid, asyncio.Lock())
	guard := d.sessionAdapters.guardFor(sid)
	atomic.AddInt32(&guard.waiters, 1)
	guard.mu.Lock()
	atomic.AddInt32(&guard.waiters, -1)
	defer guard.mu.Unlock()

	// 步骤 5: 已有 child
	// Python: :3951 existing = self._session_adapters.get(sid)
	existing := d.getCachedSessionAdapter(sid)

	// 步骤 6: ★ 7 组重建判据（本节全部留位，回填时按上游原序插入本处）
	//   ⤵️ R-1 cron 执行配方变化            Python :3953-3970
	//   ⤵️ R-2 external harness binding 变化 Python :3974-4003
	//   ⤵️ R-3 MoA 快照指纹变化              Python :4183-4195
	//   ⤵️ R-4 权限隔离未完成                Python :4199-4212
	//   ⤵️ R-5 tool_retrieval 开关翻转       Python :4214-4232
	//   ⤵️ R-6 首次 MCP reconcile 需替换     Python :4124-4158
	//   ⤵️ R-7 模型选择变更                  Python :4046-4182

	// 步骤 7: 命中则复用
	// Python: :4234-4250（MCP 快照声明 + 懒重载 + touch + 登记在飞任务）
	// ⤵️ R-6 的 mark_session_mcp_reconcile_started 与
	//    _reload_session_adapter_if_stale 依赖 MCP/热重载链路，随 R-6/R-5 回填。
	if existing != nil {
		d.touchSessionAdapter(sid)
		if opts.ReserveActivity {
			d.registerSessionAgentTask(sid, opts.RequestID)
		}
		return existing, nil
	}

	// 步骤 8: 造新 child
	// Python: :4253 adapter = self._new_session_scoped_adapter(sid)
	child, err := d.newSessionScopedAdapter(sid)
	if err != nil {
		return nil, err
	}

	// 步骤 9: 让 child 建自己的 DeepAgent（含自己的 rail 树与 context engine）
	// Python: :4310 await adapter.create_instance(config, mode=..., sub_mode=...)
	// ⤵️ R-2 的 external_harness 分支（:4254-4282，只装 binding 不建 instance）
	//    与下面这段互斥，随 R-2 回填时插在其前。
	cfg := d.instanceOverridesSnapshot()
	if err := child.CreateInstance(ctx, cfg, d.mode, d.subMode); err != nil {
		return nil, fmt.Errorf("会话子适配器 CreateInstance 失败: %w", err)
	}

	// 步骤 10-11: 入缓存并 touch
	// Python: :4260 self._session_adapters[sid] = adapter / :4419 touch
	d.sessionAdapters.remember(sid, child)
	d.touchSessionAdapter(sid)

	// 步骤 12: 登记在飞任务
	// Python: :4431 adapter._register_session_agent_task(sid)
	if opts.ReserveActivity {
		d.registerSessionAgentTask(sid, opts.RequestID)
	}
	return child, nil
}

// touchSessionAdapter 刷新最后使用时刻。
// Python: interface_deep.py _touch_session_adapter（:3316-3317）
func (d *DeepAdapter) touchSessionAdapter(sessionID string) {
	d.sessionAdapters.touch(sessionAdapterKey(&sessionID))
}

// getCachedSessionAdapter 取缓存的子适配器（不存在返回 nil）。
// Python: interface_deep.py _get_cached_session_adapter（:3254-3256）
func (d *DeepAdapter) getCachedSessionAdapter(sessionID string) AgentAdapter {
	return d.sessionAdapters.get(sessionAdapterKey(&sessionID))
}

// dropSessionAdapterCacheEntry 从缓存移除并可选删除运行时文件。
// Python: interface_deep.py _drop_session_adapter_cache_entry（:3340-3361）
func (d *DeepAdapter) dropSessionAdapterCacheEntry(
	sessionID string,
	removeLock bool,
	removeRuntimeState bool,
) {
	d.sessionAdapters.drop(sessionID, removeLock, removeRuntimeState)
}

// CleanupSessionAdapter 释放某会话的子适配器，返回是否真的释放了。
// Python: interface_deep.py cleanup_session_adapter（:3642-3755）
//
// 守卫链对齐上游：心跳保留 → child 自身分支 → 锁忙 → 活跃/执行中 →
// 真释放 → 修剪空锁。
func (d *DeepAdapter) CleanupSessionAdapter(sessionID string) bool {
	sid := sessionAdapterKey(&sessionID)

	// 步骤 1: 心跳保留
	// Python: :3650-3656 if heartbeat_service.should_retain_session(sid): return False
	// ⤵️ heartbeatService 在 Go 侧尚未落地，见 spec §8 D-9。

	// 步骤 2: child 自身
	// Python: :3658-3667
	if d.isSessionScoped {
		if d.parentSessionID != sid {
			return false
		}
		if d.isSessionActive(sid) {
			return false
		}
		_ = d.Cleanup()
		return true
	}

	// 步骤 3: 锁被持有或有等待者 → 不清（等请求方自己收尾）
	// Python: :3668-3690
	if guard, ok := d.sessionAdapters.guardOf(sid); ok {
		if atomic.LoadInt32(&guard.waiters) > 0 || guard.isLocked() {
			return false
		}
	}

	// 步骤 4: 加锁后判定（用闭包限定解锁范围，避免多返回点漏解锁）
	guard := d.sessionAdapters.guardFor(sid)
	cleaned := func() bool {
		guard.mu.Lock()
		defer guard.mu.Unlock()

		child := d.sessionAdapters.get(sid)
		if child == nil {
			d.sessionAdapters.drop(sid, true, false)
			return false
		}

		// 步骤 5: 三条守卫（活跃 / 执行中 / 权限 owner）
		// Python: :3700-3730
		if deepChild, ok := child.(*DeepAdapter); ok {
			if deepChild.isSessionActive(sid) {
				return false
			}
		}

		// 步骤 6: 真释放
		// Python: :3728 await adapter.cleanup()
		if err := child.Cleanup(); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sid).
				Msg("session adapter cleanup failed")
			return false
		}
		d.sessionAdapters.drop(sid, false, false)
		return true
	}()

	// 步骤 7: 锁空闲则修剪
	// Python: :3742-3755
	d.sessionAdapters.pruneGuardIfIdle(sid, guard)

	if !cleaned {
		return false
	}

	logger.Info(logComponent).Str("session_id", sid).
		Msg("session scoped DeepAgent removed")
	return true
}

// evictIdleSessionAdapters 淘汰空闲超时的子适配器。
// Python: interface_deep.py _evict_idle_session_adapters（:3591-3618）
func (d *DeepAdapter) evictIdleSessionAdapters(_ context.Context) {
	// 步骤 1: child 上不淘汰
	// Python: :3592
	if d.isSessionScoped {
		return
	}

	now := time.Now()
	evicted := 0
	for _, sid := range d.sessionAdapters.sessionIDs() {
		// 步骤 2a: 批次上限
		// Python: :3598
		if evicted >= sessionAdapterEvictBatchSize {
			break
		}
		// 步骤 2b: TTL 未到
		// Python: :3600
		lastUsed, _ := d.sessionAdapters.lastUsedOf(sid)
		if now.Sub(lastUsed) < sessionAdapterIdleTTLSec*time.Second {
			continue
		}
		// 步骤 2c-2d: 有活子代理 / 锁忙 → 跳过
		// Python: :3602-3616
		// ⤵️ session_has_live_subagent_runtime（:3320）与
		// session_has_live_subagents（:3607）依赖 25.8 的 _subagent_controls，
		// Go 侧本批返回 false（spec §8 D-2）。
		if d.sessionHasLiveSubagentRuntime(sid) {
			continue
		}
		if guard, ok := d.sessionAdapters.guardOf(sid); ok {
			if atomic.LoadInt32(&guard.waiters) > 0 || guard.isLocked() {
				continue
			}
		}
		// 步骤 2f: 真淘汰
		// Python: :3618 if await self.cleanup_session_adapter(sid): evicted += 1
		if d.CleanupSessionAdapter(sid) {
			evicted++
		}
	}
}

// sessionHasLiveSubagentRuntime 该会话是否仍占用子代理槽位。
// Python: interface_deep.py _session_has_live_subagent_runtime（:3320-3338）
// ⤵️ 依赖 25.8 的 DeepAgent._subagent_controls（capacity()["used"]），
// 本批返回 false，见 spec §8 D-2。
func (d *DeepAdapter) sessionHasLiveSubagentRuntime(_ string) bool {
	return false
}

// registerSessionAgentTask 登记该会话的一个在飞请求。
// Python: interface_deep.py _register_session_agent_task（:4869-4876）
//
// Python 记录 asyncio.current_task()；Go 无 task 对象，改记请求标识，
// 语义等价："有未完成请求" ⇔ "有未 done 的 task"（spec §8 D-5）。
func (d *DeepAdapter) registerSessionAgentTask(sessionID, requestID string) {
	if requestID == "" {
		return
	}
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if d.sessionAgentTasks == nil {
		d.sessionAgentTasks = map[string]map[string]struct{}{}
	}
	if d.sessionAgentTasks[sessionID] == nil {
		d.sessionAgentTasks[sessionID] = map[string]struct{}{}
	}
	d.sessionAgentTasks[sessionID][requestID] = struct{}{}
}

// unregisterSessionAgentTask 注销在飞请求。
// Python: interface_deep.py _unregister_session_agent_task（:4878-4890）
func (d *DeepAdapter) unregisterSessionAgentTask(sessionID, requestID string) {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if bucket := d.sessionAgentTasks[sessionID]; bucket != nil {
		delete(bucket, requestID)
		if len(bucket) == 0 {
			delete(d.sessionAgentTasks, sessionID)
		}
	}
}

// hasRegisteredSessionTasks 该会话是否有在飞请求。
// Python: interface_deep.py _session_has_registered_tasks（:4601-4603）
func (d *DeepAdapter) hasRegisteredSessionTasks(sessionID string) bool {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	return len(d.sessionAgentTasks[sessionID]) > 0
}

// instanceOverridesSnapshot 取 instanceOverrides 的安全拷贝（克隆用）。
// Python: create_instance 的 config 入参（dict(self._instance_overrides)）
func (d *DeepAdapter) instanceOverridesSnapshot() map[string]any {
	out := make(map[string]any, len(d.instanceOverrides))
	for k, v := range d.instanceOverrides {
		out[k] = v
	}
	return out
}

// ──────────────────────────── 缓存自身方法 ────────────────────────────

// guardFor 取或创建该会话的互斥锁。
func (c *sessionAdapterCache) guardFor(sid string) *sessionAdapterGuard {
	c.mu.Lock()
	defer c.mu.Unlock()
	if g, ok := c.guards[sid]; ok {
		return g
	}
	g := &sessionAdapterGuard{}
	c.guards[sid] = g
	return g
}

// guardOf 取该会话的互斥锁（不存在返回 false）。
func (c *sessionAdapterCache) guardOf(sid string) (*sessionAdapterGuard, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.guards[sid]
	return g, ok
}

// isLocked 该锁是否正被持有（TryLock 探测）。
func (g *sessionAdapterGuard) isLocked() bool {
	if g.mu.TryLock() {
		g.mu.Unlock()
		return false
	}
	return true
}

// pruneGuardIfIdle 锁空闲且无人等待时移除。
// Python: :3750-3755 _is_session_lock_idle + pop
//
// 注意锁顺序：isLocked() 用 TryLock 探测，必须在持有 c.mu 之外做，
// 否则与 getOrCreateSessionAdapter（先拿 guard 再拿 c.mu）构成反向顺序。
func (c *sessionAdapterCache) pruneGuardIfIdle(sid string, guard *sessionAdapterGuard) {
	if atomic.LoadInt32(&guard.waiters) > 0 || guard.isLocked() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.guards[sid] != guard {
		return
	}
	delete(c.guards, sid)
}

// remember 记住某会话的子适配器。
func (c *sessionAdapterCache) remember(sid string, adapter AgentAdapter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sid] = adapter
	c.lastUsed[sid] = time.Now()
}

// get 取某会话的子适配器（不存在返回 nil）。
func (c *sessionAdapterCache) get(sid string) AgentAdapter {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[sid]
}

// has 判断某会话是否有子适配器。
func (c *sessionAdapterCache) has(sid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.entries[sid]
	return ok
}

// hasGuard 判断某会话是否有锁（锁也算运行时占用）。
// Python: has_session_runtime 的 _session_adapter_locks 分支（:4572）
func (c *sessionAdapterCache) hasGuard(sid string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.guards[sid]
	return ok
}

// touch 刷新最后使用时刻（不创建条目）。
func (c *sessionAdapterCache) touch(sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[sid]; ok {
		c.lastUsed[sid] = time.Now()
	}
}

// forget 移除某会话的子适配器（保留锁与文件）。
func (c *sessionAdapterCache) forget(sid string) {
	c.drop(sid, false, false)
}

// drop 移除条目，按参数决定是否一并删锁与运行时文件。
// Python: :3340-3361 _drop_session_adapter_cache_entry
func (c *sessionAdapterCache) drop(sid string, removeLock bool, removeRuntimeState bool) {
	c.mu.Lock()
	delete(c.entries, sid)
	if removeLock {
		delete(c.guards, sid)
	}
	delete(c.lastUsed, sid)
	c.mu.Unlock()

	if !removeRuntimeState {
		return
	}
	// ⤵️ D-8：Python 同时删 get_runtime_state_path(sid) 与
	// _skill_retrieval_profile_path(sid)。前者是
	// <config_dir>/runtime_state/<sid>.yaml（jiuwenswarm/common/utils.py:1758-1765），
	// per-session 一文件。
	//
	// Go 现状与之不同：runtime_prompt_rail.WriteRuntimeStateYAML 写的是
	// <configDir>/runtime_state.yaml（runtime_prompt_rail.go:348），**全进程共用一个
	// 单文件、不分 session**。因此这里无对应文件可删，删了也不是同一个东西。
	// 本批只清内存缓存，不做文件删除。回填条件：Go 侧改为 per-session 路径
	// （<configDir>/runtime_state/<sid>.yaml）后一并实现删除与迁移。
}

// cleanup 移除并报告是否清到了东西（历史入口，等价于 drop 且不删锁）。
func (c *sessionAdapterCache) cleanup(sid string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[sid]
	delete(c.entries, sid)
	delete(c.lastUsed, sid)
	return ok
}

// sessionIDs 当前缓存的会话标识快照。
func (c *sessionAdapterCache) sessionIDs() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.entries))
	for sid := range c.entries {
		out = append(out, sid)
	}
	return out
}

// lastUsedOf 取该会话最后使用时刻。
func (c *sessionAdapterCache) lastUsedOf(sid string) (time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.lastUsed[sid]
	return t, ok
}

// count 当前缓存的会话数。
func (c *sessionAdapterCache) count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// markIdleForTest 把 lastUsed 拨回指定秒数前（仅测试用）。
func (c *sessionAdapterCache) markIdleForTest(sid string, secondsAgo int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastUsed[sid] = time.Now().Add(-time.Duration(secondsAgo) * time.Second)
}
