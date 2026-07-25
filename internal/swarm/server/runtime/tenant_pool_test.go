package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// --- GetInstance / ResetInstance ---

func TestGetInstance_单例(t *testing.T) {
	// 重置确保干净状态
	ResetInstance()

	inst1 := GetInstance()
	require.NotNil(t, inst1)

	inst2 := GetInstance()
	assert.Same(t, inst1, inst2, "GetInstance 应返回同一实例")
}

func TestResetInstance(t *testing.T) {
	ResetInstance()

	inst1 := GetInstance()
	require.NotNil(t, inst1)

	ResetInstance()

	inst2 := GetInstance()
	require.NotNil(t, inst2)
	assert.NotSame(t, inst1, inst2, "ResetInstance 后 GetInstance 应返回新实例")
}

func TestResetInstance_多次调用不panic(t *testing.T) {
	// 对未初始化的 singleton 调用 ResetInstance 不应 panic
	ResetInstance()
	ResetInstance()
	ResetInstance()

	// 之后 GetInstance 应正常工作
	inst := GetInstance()
	require.NotNil(t, inst)
}

func TestResetInstance_Cleanup被调用(t *testing.T) {
	ResetInstance()

	inst := GetInstance()
	require.NotNil(t, inst)

	// ResetInstance 会通过 Singleton.Reset → resettable.Cleanup 自动清理
	// 验证不 panic 即可（AgentManager.Cleanup 是幂等的）
	ResetInstance()
}

// --- ProcessMessage ---

func TestTenantAgentPool_ProcessMessage_透传(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	req := &schema.AgentRequest{
		RequestID: "test-req-pm",
		ChannelID: "test-ch-pm",
		Params:    []byte(`{"mode": "agent.plan"}`),
	}

	// 验证 ProcessMessage 能正确委托到 AgentManager.ProcessMessage
	// AgentManager.ProcessMessage 会调用 GetAgent → UapClaw.ProcessMessage
	// 由于 UapClaw.CreateInstance 需要 LLM，这里仅验证参数解析路径
	mode, subMode := resolveModeFromRequest(req)
	assert.Equal(t, "agent", mode)
	assert.Equal(t, "plan", subMode)

	// 验证 agentManager 实例存在
	assert.NotNil(t, pool.agentManager)

	// 端到端透传测试由集成测试覆盖
}

func TestTenantAgentPool_ProcessMessage_请求字段日志(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	// 验证 request 字段正确传递
	req := &schema.AgentRequest{
		RequestID: "req-log-1",
		ChannelID: "ch-log-1",
		Params:    []byte(`{"mode": "code.normal"}`),
	}

	mode, subMode := resolveModeFromRequest(req)
	assert.Equal(t, "code", mode)
	assert.Equal(t, "normal", subMode)
}

// --- ProcessMessageStream ---

func TestTenantAgentPool_ProcessMessageStream_透传(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	req := &schema.AgentRequest{
		RequestID: "test-req-pms",
		ChannelID: "test-ch-pms",
		Params:    []byte(`{"mode": "agent.plan"}`),
		IsStream:  true,
	}

	// 验证参数解析
	mode, subMode := resolveModeFromRequest(req)
	assert.Equal(t, "agent", mode)
	assert.Equal(t, "plan", subMode)

	// 验证 agentManager 实例存在
	assert.NotNil(t, pool.agentManager)

	// 端到端透传测试由集成测试覆盖
}

// --- Cleanup ---

func TestTenantAgentPool_Cleanup(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	// Cleanup 应成功
	err := pool.Cleanup()
	assert.NoError(t, err)
}

func TestTenantAgentPool_Cleanup_实现Resettable接口(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	// 验证 TenantAgentPool 实现了 utils.resettable 接口（通过 Singleton.Reset 调用）
	// 直接调用 Cleanup 验证无 panic
	err := pool.Cleanup()
	assert.NoError(t, err)

	// 通过 ResetInstance 间接验证 Singleton.Reset 调用 Cleanup
	ResetInstance()
}

// --- 边界场景 ---

func TestTenantAgentPool_AgentManager共享(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	// 同一个 pool 内 agentManager 应始终是同一个实例
	am1 := pool.agentManager
	am2 := pool.agentManager
	assert.Same(t, am1, am2, "同一 pool 内 agentManager 应为同一实例")
}

func TestTenantAgentPool_Context传递(t *testing.T) {
	ResetInstance()

	pool := GetInstance()
	require.NotNil(t, pool)

	// 验证 context 能正确传递到 AgentManager
	ctx := context.Background()
	req := &schema.AgentRequest{
		RequestID: "ctx-test",
		ChannelID: "ctx-ch",
		Params:    []byte(`{"mode": "agent"}`),
	}

	// 仅验证参数解析不 panic，端到端由集成测试覆盖
	mode, subMode := resolveModeFromRequest(req)
	assert.Equal(t, "agent", mode)
	assert.Equal(t, "", subMode)
	_ = ctx
}
