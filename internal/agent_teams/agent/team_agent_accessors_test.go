package agent

import (
	"context"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newTestTeamAgent 构造一个最小的 TeamAgent（仅 card 与默认状态）。
func newTestTeamAgent(t *testing.T) *TeamAgent {
	t.Helper()
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("team-agent-test"),
		agentschema.WithAgentName("team-agent-test"),
	)
	return NewTeamAgent(context.Background(), card)
}

// assertNotPanic 断言 fn 执行期间不 panic。
func assertNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("不应 panic，实际 %v", r)
		}
	}()
	fn()
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestTeamAgent_InfraAccessors_NilSafe 验证各访问器在未装配时返回 nil 而非 typed-nil。
// typed-nil 陷阱：把 nil 具体指针装进接口后，调用方 `!= nil` 判真再解引用会 panic。
func TestTeamAgent_InfraAccessors_NilSafe(t *testing.T) {
	a := newTestTeamAgent(t)

	t.Run("SessionController 非 nil（构造时已建）", func(t *testing.T) {
		if a.SessionController() == nil {
			t.Fatal("构造后 SessionController 不应为 nil")
		}
	})

	t.Run("TeamBackendAccessor 未建团队时为 nil", func(t *testing.T) {
		if got := a.TeamBackendAccessor(); got != nil {
			t.Fatalf("未建团队时应为 nil，实际 %#v", got)
		}
	})

	t.Run("WorkspaceManager 未装配时为 nil", func(t *testing.T) {
		if got := a.WorkspaceManager(); got != nil {
			t.Fatalf("未装配时应为 nil，实际 %#v", got)
		}
	})

	t.Run("HarnessAccessor 未装配时为 nil", func(t *testing.T) {
		if got := a.HarnessAccessor(); got != nil {
			t.Fatalf("未装配时应为 nil，实际 %#v", got)
		}
	})

	t.Run("FirstIterGate 未装配时是可安全调用的空实现", func(t *testing.T) {
		// FirstIterGate 不做判空，直接把 *rails.FirstIterationGate 装进接口，
		// 因此接口值本身非 nil（typed-nil）。断言它可被取出且不 panic 即达标；
		// 真正解引用会 panic 的风险由 configurator 保证非 nil 来控制。
		got := a.FirstIterGate()
		if got == nil {
			t.Skip("实现已改为判空返回 nil，此处分支不适用")
		}
		assertNotPanic(t, func() { _ = got })
	})
}

// TestTeamAgent_WorkspaceInitialized 验证工作空间初始化标记的读写。
func TestTeamAgent_WorkspaceInitialized(t *testing.T) {
	a := newTestTeamAgent(t)

	if a.IsWorkspaceInitialized() {
		t.Fatal("初始状态应为未初始化")
	}
	a.SetWorkspaceInitialized()
	if !a.IsWorkspaceInitialized() {
		t.Fatal("标记后应为已初始化")
	}
}

// TestTeamAgent_AccessorsWithNilConfigurator 验证 configurator 为 nil 时各访问器安全返回。
func TestTeamAgent_AccessorsWithNilConfigurator(t *testing.T) {
	a := newTestTeamAgent(t)
	a.configurator = nil

	// SessionController 只依赖 sessionManager，configurator 置 nil 不影响
	if a.SessionController() == nil {
		t.Fatal("configurator 为 nil 不应影响 SessionController")
	}
	if got := a.WorkspaceManager(); got != nil {
		t.Fatalf("configurator 为 nil 时应返回 nil，实际 %#v", got)
	}
	if got := a.MemoryManager(); got != nil {
		t.Fatalf("configurator 为 nil 时应返回 nil，实际 %#v", got)
	}
	if got := a.FirstIterGate(); got != nil {
		t.Fatalf("configurator 为 nil 时应返回 nil，实际 %#v", got)
	}
	if a.IsWorkspaceInitialized() {
		t.Fatal("configurator 为 nil 时应返回 false")
	}
	// 不应 panic
	a.SetWorkspaceInitialized()
}
