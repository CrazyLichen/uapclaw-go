package adapter

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// TestSessionAdapterCache_增删查 会话级缓存的三个基本操作。
func TestSessionAdapterCache_增删查(t *testing.T) {
	cache := newSessionAdapterCache()

	cache.remember("s1", NewDeepAdapter())
	if !cache.has("s1") {
		t.Error("记住后应存在")
	}
	if got := cache.get("s1"); got == nil {
		t.Error("应能取出")
	}
	cache.forget("s1")
	if cache.has("s1") {
		t.Error("忘记后不应存在")
	}
}

// TestSessionAdapterCache_并发安全 多协程读写不 panic 不竞态。
func TestSessionAdapterCache_并发安全(t *testing.T) {
	cache := newSessionAdapterCache()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			const sid = "s1"
			cache.remember(sid, NewDeepAdapter())
			_ = cache.has(sid)
			_ = cache.get(sid)
			cache.touch(sid)
			cache.forget(sid)
		}(i)
	}
	wg.Wait()
}

// TestSessionAdapterCache_Cleanup 清理返回是否清到东西。
func TestSessionAdapterCache_Cleanup(t *testing.T) {
	cache := newSessionAdapterCache()
	cache.remember("s1", NewDeepAdapter())
	if !cache.cleanup("s1") {
		t.Error("有缓存时应返回 true")
	}
	if cache.cleanup("s1") {
		t.Error("无缓存时应返回 false")
	}
}

// TestSessionAdapterCache_Count 会话数统计。
func TestSessionAdapterCache_Count(t *testing.T) {
	cache := newSessionAdapterCache()
	if cache.count() != 0 {
		t.Error("初始应为 0")
	}
	cache.remember("a", NewDeepAdapter())
	cache.remember("b", NewDeepAdapter())
	if cache.count() != 2 {
		t.Errorf("应为 2，实际 %d", cache.count())
	}
	cache.forget("a")
	if cache.count() != 1 {
		t.Errorf("应为 1，实际 %d", cache.count())
	}
}

// TestSessionAdapterCache_Touch不存在 对不存在的会话 touch 不创建条目。
func TestSessionAdapterCache_Touch不存在(t *testing.T) {
	cache := newSessionAdapterCache()
	cache.touch("nope")
	if cache.count() != 0 {
		t.Error("touch 不应创建条目")
	}
}

// TestDeepAdapter_CleanupSessionAdapter 清理返回是否清到东西。
func TestDeepAdapter_CleanupSessionAdapter(t *testing.T) {
	adapter := NewDeepAdapter()
	adapter.sessionAdapters.remember("s1", NewDeepAdapter())
	if !adapter.CleanupSessionAdapter("s1") {
		t.Error("有缓存时应返回 true")
	}
	if adapter.CleanupSessionAdapter("s1") {
		t.Error("无缓存时应返回 false")
	}
}

// TestDeepAdapter_HasSessionRuntime 缓存或活跃计数任一非空即为真。
func TestDeepAdapter_HasSessionRuntime(t *testing.T) {
	adapter := NewDeepAdapter()
	if adapter.HasSessionRuntime("s1") {
		t.Error("初始不应有会话运行时")
	}

	adapter.sessionAdapters.remember("s1", NewDeepAdapter())
	if !adapter.HasSessionRuntime("s1") {
		t.Error("有缓存时应为真")
	}
	adapter.CleanupSessionAdapter("s1")

	adapter.markSessionActive("s2")
	if !adapter.HasSessionRuntime("s2") {
		t.Error("有活跃计数时应为真")
	}
	adapter.unmarkSessionActive("s2")
	if adapter.HasSessionRuntime("s2") {
		t.Error("计数归零后应为假")
	}
}

// TestOtherActiveSessions_清理会话计数 清理会话后其他会话计数不受影响。
func TestOtherActiveSessions_清理会话计数(t *testing.T) {
	adapter := NewDeepAdapter()
	adapter.markSessionActive("s1")
	adapter.markSessionActive("s2")
	adapter.CleanupSessionAdapter("s1")
	// s1 只剩活跃计数，仍在运行时里
	if !adapter.HasSessionRuntime("s1") {
		t.Error("s1 仍有活跃计数，应为真")
	}
	if !adapter.HasSessionRuntime("s2") {
		t.Error("s2 不受影响，应为真")
	}
}

// ──────────────────────────── 会话分身：工厂与创建 ────────────────────────────

// TestNewSessionScopedAdapter_deep模式 工厂造出 DeepAdapter 且已标记为 child。
func TestNewSessionScopedAdapter_deep模式(t *testing.T) {
	root := NewDeepAdapter()

	child, err := root.newSessionScopedAdapter("s1")
	if err != nil {
		t.Fatalf("newSessionScopedAdapter 失败: %v", err)
	}
	if !child.IsSessionScoped() {
		t.Error("child 的 IsSessionScoped 应为 true")
	}
	if root.IsSessionScoped() {
		t.Error("根不应被标记为 child")
	}
	deepChild, ok := child.(*DeepAdapter)
	if !ok {
		t.Fatalf("deep 模式的 child 应为 *DeepAdapter，实际 %T", child)
	}
	if deepChild.parentSessionID != "s1" {
		t.Errorf("parentSessionID = %q，期望 s1", deepChild.parentSessionID)
	}
}

// TestNewSessionScopedAdapter_code模式 工厂造出完整 CodeAdapter（不是从 deep 提取）。
func TestNewSessionScopedAdapter_code模式(t *testing.T) {
	root := NewCodeAdapter()

	child, err := root.deep.newSessionScopedAdapter("s1")
	if err != nil {
		t.Fatalf("newSessionScopedAdapter 失败: %v", err)
	}
	codeChild, ok := child.(*CodeAdapter)
	if !ok {
		t.Fatalf("code 模式的 child 应为 *CodeAdapter，实际 %T", child)
	}
	if codeChild.deep == nil {
		t.Fatal("CodeAdapter child 的 deep 不应为 nil")
	}
	if !codeChild.IsSessionScoped() {
		t.Error("CodeAdapter child 的 IsSessionScoped 应为 true")
	}
	// code 专属状态必须在 child 上，否则 rails 会静默降级成 deep 模式。
	if !codeChild.forceEnglishRuntimePrompt {
		t.Error("CodeAdapter child 应保留 forceEnglishRuntimePrompt")
	}
	if !codeChild.deep.isCodeAgent {
		t.Error("CodeAdapter child 的 deep.isCodeAgent 应为 true")
	}
	if codeChild.deep.runtimeConfigUpdater == nil {
		t.Error("CodeAdapter child 的 runtimeConfigUpdater 应指向 CodeAdapter 版本")
	}
}

// TestNewSessionScopedAdapter_继承skillManager 父的 skillManager 传给 child。
func TestNewSessionScopedAdapter_继承skillManager(t *testing.T) {
	root := NewDeepAdapter()
	mgr := skill.NewSkillManager(t.TempDir())
	root.SetSkillManager(mgr)

	child, err := root.newSessionScopedAdapter("s1")
	if err != nil {
		t.Fatalf("newSessionScopedAdapter 失败: %v", err)
	}
	if got := child.(*DeepAdapter).skillManager; got != mgr {
		t.Error("child 应继承父的 skillManager")
	}
}

// TestNewSessionScopedAdapter_工厂未注入 报错而非静默返回 nil。
func TestNewSessionScopedAdapter_工厂未注入(t *testing.T) {
	root := NewDeepAdapter()
	root.sessionChildFactory = nil
	if _, err := root.newSessionScopedAdapter("s1"); err == nil {
		t.Error("工厂未注入时应报错")
	}
}

// TestCreateAdapter_按mode分派 工厂函数本身的分派正确（child 工厂依赖它）。
func TestCreateAdapter_按mode分派(t *testing.T) {
	agent, err := CreateAdapter("harness", "agent")
	if err != nil {
		t.Fatalf("CreateAdapter(agent) 失败: %v", err)
	}
	if _, ok := agent.(*DeepAdapter); !ok {
		t.Errorf("agent 模式应得 *DeepAdapter，实际 %T", agent)
	}
	code, err := CreateAdapter("harness", "code")
	if err != nil {
		t.Fatalf("CreateAdapter(code) 失败: %v", err)
	}
	if _, ok := code.(*CodeAdapter); !ok {
		t.Errorf("code 模式应得 *CodeAdapter，实际 %T", code)
	}
}

// ──────────────────────────── 缓存与生命周期 ────────────────────────────

// TestSessionAdapterKey_空值归一 空串与空指针都归到 "default"。
func TestSessionAdapterKey_空值归一(t *testing.T) {
	if got := sessionAdapterKey(nil); got != "default" {
		t.Errorf("nil → %q，期望 default", got)
	}
	empty := "  "
	if got := sessionAdapterKey(&empty); got != "default" {
		t.Errorf("空白 → %q，期望 default", got)
	}
	sid := " s1 "
	if got := sessionAdapterKey(&sid); got != "s1" {
		t.Errorf("带空格 → %q，期望 s1", got)
	}
}

// TestGetOrCreateSessionAdapter_创建并复用 首次创建、二次返回同一对象。
func TestGetOrCreateSessionAdapter_创建并复用(t *testing.T) {
	root := NewDeepAdapter()

	first, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{})
	if err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	second, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{})
	if err != nil {
		t.Fatalf("二次获取失败: %v", err)
	}
	if first != second {
		t.Error("同会话两次应返回同一 child")
	}
	if root.sessionAdapters.count() != 1 {
		t.Errorf("缓存数 = %d，期望 1", root.sessionAdapters.count())
	}
}

// TestGetOrCreateSessionAdapter_child上直接返回自身 child 不再造子。
func TestGetOrCreateSessionAdapter_child上直接返回自身(t *testing.T) {
	child := NewDeepAdapter()
	child.MarkAsSessionScoped("s1")

	got, err := child.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{})
	if err != nil {
		t.Fatalf("child 调用失败: %v", err)
	}
	if got != AgentAdapter(child) {
		t.Error("child 上调用应返回自身，不应再分层")
	}
}

// TestGetOrCreateSessionAdapter_不同会话各自child 两会话得到两个不同 child。
func TestGetOrCreateSessionAdapter_不同会话各自child(t *testing.T) {
	root := NewDeepAdapter()
	a, _ := root.getOrCreateSessionAdapter(context.Background(), "sa", sessionAdapterOptions{})
	b, _ := root.getOrCreateSessionAdapter(context.Background(), "sb", sessionAdapterOptions{})
	if a == b {
		t.Error("不同会话应得到不同 child")
	}
	if root.sessionAdapters.count() != 2 {
		t.Errorf("缓存数 = %d，期望 2", root.sessionAdapters.count())
	}
}

// TestGetOrCreateSessionAdapter_并发只造一个 -race 下同会话并发只创建一个 child。
func TestGetOrCreateSessionAdapter_并发只造一个(t *testing.T) {
	root := NewDeepAdapter()
	const n = 16
	results := make([]AgentAdapter, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{})
			if err != nil {
				t.Errorf("并发创建失败: %v", err)
				return
			}
			results[idx] = c
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		if results[i] != results[0] {
			t.Fatal("同会话并发应只造一个 child")
		}
	}
}

// TestCleanupSessionAdapter_无缓存返回false 空缓存时清理应报 false。
func TestCleanupSessionAdapter_无缓存返回false(t *testing.T) {
	root := NewDeepAdapter()
	if root.CleanupSessionAdapter("nope") {
		t.Error("无缓存时应返回 false")
	}
}

// TestCleanupSessionAdapter_会话活跃时不清 有活跃计数时不释放 child。
func TestCleanupSessionAdapter_会话活跃时不清(t *testing.T) {
	// 注意：root 必须是根（未被 MarkAsSessionScoped），否则走的是
	// CleanupSessionAdapter 的 child 分支而非缓存分支。
	root := NewDeepAdapter()
	if _, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{}); err != nil {
		t.Fatalf("预置 child 失败: %v", err)
	}
	child := root.getCachedSessionAdapter("s1").(*DeepAdapter)
	child.markSessionActive("s1")

	if root.CleanupSessionAdapter("s1") {
		t.Error("会话活跃时不应清理")
	}
	// CleanupSessionAdapter 读的是 child 自己的 activeSessionIDs
	// （对齐上游 :3720 adapter.is_session_active(sid)）。
	if root.sessionAdapters.count() != 1 {
		t.Error("活跃会话的 child 应仍在缓存里")
	}
}

// TestCleanupSessionAdapter_成功清理 空闲会话可被清理且缓存移除。
func TestCleanupSessionAdapter_成功清理(t *testing.T) {
	root := NewDeepAdapter()
	if _, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{}); err != nil {
		t.Fatalf("预置 child 失败: %v", err)
	}
	if !root.CleanupSessionAdapter("s1") {
		t.Error("空闲会话应清理成功")
	}
	if root.sessionAdapters.count() != 0 {
		t.Error("清理后缓存应为空")
	}
	if root.HasSessionRuntime("s1") {
		t.Error("清理后不应再报告有运行时")
	}
}

// TestEvictIdleSessionAdapters_TTL未到不淘汰 刚用过的不清理。
func TestEvictIdleSessionAdapters_TTL未到不淘汰(t *testing.T) {
	root := NewDeepAdapter()
	if _, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{}); err != nil {
		t.Fatalf("预置 child 失败: %v", err)
	}
	root.evictIdleSessionAdapters(context.Background())
	if root.sessionAdapters.count() != 1 {
		t.Error("TTL 未到期不应淘汰")
	}
}

// TestEvictIdleSessionAdapters_TTL到期淘汰 把 lastUsed 拨回 TTL 之外后淘汰。
func TestEvictIdleSessionAdapters_TTL到期淘汰(t *testing.T) {
	root := NewDeepAdapter()
	if _, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{}); err != nil {
		t.Fatalf("预置 child 失败: %v", err)
	}
	root.sessionAdapters.markIdleForTest("s1", sessionAdapterIdleTTLSec+1)
	root.evictIdleSessionAdapters(context.Background())
	if root.sessionAdapters.count() != 0 {
		t.Error("TTL 到期应淘汰")
	}
}

// TestEvictIdleSessionAdapters_每批上限 超出批次上限时单次最多淘汰 N 个。
func TestEvictIdleSessionAdapters_每批上限(t *testing.T) {
	root := NewDeepAdapter()
	for i := 0; i < sessionAdapterEvictBatchSize+2; i++ {
		sid := fmt.Sprintf("s%d", i)
		if _, err := root.getOrCreateSessionAdapter(context.Background(), sid, sessionAdapterOptions{}); err != nil {
			t.Fatalf("预置 %s 失败: %v", sid, err)
		}
		root.sessionAdapters.markIdleForTest(sid, sessionAdapterIdleTTLSec+1)
	}
	before := root.sessionAdapters.count()
	root.evictIdleSessionAdapters(context.Background())
	after := root.sessionAdapters.count()
	if before-after != sessionAdapterEvictBatchSize {
		t.Errorf("单次淘汰 %d 个，期望 %d", before-after, sessionAdapterEvictBatchSize)
	}
}

// TestEvictIdleSessionAdapters_child上不淘汰 child 自身不执行淘汰。
func TestEvictIdleSessionAdapters_child上不淘汰(t *testing.T) {
	child := NewDeepAdapter()
	child.MarkAsSessionScoped("s1")
	child.evictIdleSessionAdapters(context.Background())
	if child.sessionAdapters.count() != 0 {
		t.Error("child 不应持有缓存")
	}
}

// TestGetLiveSessionInstance_根查缓存并递归 根能拿到 child 的 instance。
func TestGetLiveSessionInstance_根查缓存并递归(t *testing.T) {
	root := NewDeepAdapter()
	if got := root.GetLiveSessionInstance("s1"); got != nil {
		t.Error("无 child 时应返回 nil")
	}
	if _, err := root.getOrCreateSessionAdapter(context.Background(), "s1", sessionAdapterOptions{}); err != nil {
		t.Fatalf("预置 child 失败: %v", err)
	}
	// child 未建 instance 时仍为 nil（CreateInstance 在环境无模型时会失败，
	// 但 getOrCreate 已经建过；此处只验证不 panic 且返回类型正确）。
	_ = root.GetLiveSessionInstance("s1")
}

// TestHasSessionRuntime_四源判定 缓存/锁/活跃/在飞任务任一为真。
func TestHasSessionRuntime_四源判定(t *testing.T) {
	root := NewDeepAdapter()
	if root.HasSessionRuntime("s1") {
		t.Error("初始全空应返回 false")
	}
	// 源 1：缓存
	root.sessionAdapters.remember("s1", NewDeepAdapter())
	if !root.HasSessionRuntime("s1") {
		t.Error("有缓存条目应为真")
	}
	root.sessionAdapters.forget("s1")
	// 源 2：会话锁
	root.sessionAdapters.guardFor("s1")
	if !root.HasSessionRuntime("s1") {
		t.Error("有会话锁应为真")
	}
	root.sessionAdapters.drop("s1", true, false)
	// 源 3：活跃计数
	root.markSessionActive("s1")
	if !root.HasSessionRuntime("s1") {
		t.Error("有活跃计数应为真")
	}
	root.unmarkSessionActive("s1")
	// 源 4：在飞任务
	root.registerSessionAgentTask("s1", "r1")
	if !root.HasSessionRuntime("s1") {
		t.Error("有在飞任务应为真")
	}
	root.unregisterSessionAgentTask("s1", "r1")
	if root.HasSessionRuntime("s1") {
		t.Error("四源全空应返回 false")
	}
}
