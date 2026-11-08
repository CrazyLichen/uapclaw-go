package adapter

import (
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
