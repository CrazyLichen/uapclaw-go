package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 账本归属（P4） ────────────────────────────
//
// Python 门面不持有 coordinator（interface.py 里 0 命中），执行账本由
// AgentRuntime 构造期自建并唯一持有。这组用例锁定该归属，防止退回
// "每个 UapClawSwarm 各持一本账"。

// TestNewUapClaw_自带账本 门面构造后即可取到账本（Runtime 自建）。
// Python: AgentRuntime.__init__ `session_coordinator or RuntimeSessionCoordinator()`
func TestNewUapClaw_自带账本(t *testing.T) {
	uc := NewUapClaw()
	require.NotNil(t, uc.agentRuntime, "门面应持有 Runtime")
	require.NotNil(t, uc.agentRuntime.Coordinator(), "Runtime 应自建账本")
	require.NotNil(t, uc.executionCoordinator(), "门面经 Runtime 取账本")
}

// TestSetAgentRuntime_全部门面共享同一本账 注入后覆盖自有账本。
// Python: 门面不持有 coordinator，账本只在 Runtime。
func TestSetAgentRuntime_全部门面共享同一本账(t *testing.T) {
	shared := swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{})

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
	shared := swruntime.NewAgentRuntime(swruntime.AgentRuntimeDeps{})

	manager := NewAgentManager()
	manager.SetRuntimeProvider(func() *swruntime.AgentRuntime { return shared })

	require.Same(t, shared, manager.runtimeProvider())
}

// TestExecutionCoordinator_无Runtime时返回nil 未构造 Runtime 的极简门面不 panic。
func TestExecutionCoordinator_无Runtime时返回nil(t *testing.T) {
	uc := &UapClawSwarm{}
	require.Nil(t, uc.executionCoordinator())
}
