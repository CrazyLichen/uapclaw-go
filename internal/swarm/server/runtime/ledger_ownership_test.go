package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 账本归属（P4） ────────────────────────────
//
// Python 门面不持有 coordinator（interface.py 里 0 命中），执行账本由
// AgentRuntime 构造期自建并唯一持有。这组用例锁定该归属，防止退回
// "每个 UapClawSwarm 各持一本账"。

// TestNewUapClaw_不自建Runtime 门面构造后没有 Runtime，账本靠注入。
// Python: 门面不持有 coordinator（interface.py 0 命中），账本只在 Runtime
func TestNewUapClaw_不自建Runtime(t *testing.T) {
	uc := NewUapClaw()
	require.Nil(t, uc.currentAgentRuntime(), "门面不应自建 Runtime")
	require.Nil(t, uc.executionCoordinator(), "未注入时无账本可取")
}

// TestSetAgentRuntime_全部门面共享同一本账 注入后覆盖自有账本。
// Python: 门面不持有 coordinator，账本只在 Runtime。
func TestSetAgentRuntime_全部门面共享同一本账(t *testing.T) {
	shared := newTestRuntimeHandle()

	first := NewUapClaw()
	second := NewUapClaw()
	first.SetAgentRuntime(shared)
	second.SetAgentRuntime(shared)

	require.Same(t, first.executionCoordinator(), second.executionCoordinator(),
		"两个门面必须共享同一本账（这是 P4 的核心目标）")
	require.Same(t, shared.Coordinator(), first.executionCoordinator())
}

// TestAgentManager_注册Runtime提供者 提供者被保存并可取用。
// Python: 门面不自建账本，由创建方传入 Runtime。
func TestAgentManager_注册Runtime提供者(t *testing.T) {
	shared := newTestRuntimeHandle()

	manager := NewAgentManager()
	manager.SetRuntimeProvider(func() AgentRuntimeHandle { return shared })

	require.Same(t, shared, manager.runtimeProvider())
}

// TestExecutionCoordinator_无Runtime时返回nil 未构造 Runtime 的极简门面不 panic。
func TestExecutionCoordinator_无Runtime时返回nil(t *testing.T) {
	uc := &UapClawSwarm{}
	require.Nil(t, uc.executionCoordinator())
}
