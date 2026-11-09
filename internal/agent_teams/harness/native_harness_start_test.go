package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// newTestNativeHarness 构造一个不含模型的最小 NativeHarness。
func newTestNativeHarness(t *testing.T) *NativeHarness {
	t.Helper()
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test-native"))
	// 25.1b：前向构造——由 spec 解析 parts 并物化，不再收外部 agent。
	// card 必须放进 spec（上游：spec 的真实 card 在构造期配置 agent）。
	spec := atschema.NewDeepAgentSpec()
	spec.Card = card
	h, err := NewNativeHarness(spec, nil)
	if err != nil {
		t.Fatalf("NewNativeHarness: %v", err)
	}
	return h
}

// TestNativeHarness_Start幂等 验证重复 Start 不重建内核（C1）。
func TestNativeHarness_Start幂等(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("default", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	require.NoError(t, h.Start(context.Background(), sess))
	require.NoError(t, h.Start(context.Background(), sess))

	assert.Equal(t, HarnessStateIdle, h.State(), "Start 后保持 idle，首条 send 才进入 running")
	assert.Equal(t, sess.GetSessionID(), h.SessionID())

	require.NoError(t, h.Stop(context.Background()))
	assert.Equal(t, HarnessStateTerminated, h.State())
}

// TestNativeHarness_Start绑定不同会话报错 对齐上游"一个实例一个会话"约束。
func TestNativeHarness_Start绑定不同会话报错(t *testing.T) {
	h := newTestNativeHarness(t)
	s1 := session.CreateAgentSession("s1", h.DeepAgent().Card(), nil)
	require.NoError(t, s1.PreRun(context.Background(), map[string]any{}))
	s2 := session.CreateAgentSession("s2", h.DeepAgent().Card(), nil)
	require.NoError(t, s2.PreRun(context.Background(), map[string]any{}))

	require.NoError(t, h.Start(context.Background(), s1))
	assert.Error(t, h.Start(context.Background(), s2))
	require.NoError(t, h.Stop(context.Background()))
}

// TestNativeHarness_InitialPhaseIsIdle 验证未启动即 idle。
func TestNativeHarness_InitialPhaseIsIdle(t *testing.T) {
	h := newTestNativeHarness(t)
	assert.Equal(t, HarnessStateIdle, h.State())
	assert.Empty(t, h.SessionID())
}

// TestIsControlResult 验证控制信号判定（对齐 Python _is_control_result）。
func TestIsControlResult(t *testing.T) {
	// nil / 空 result → 非控制信号
	assert.False(t, isControlResult(nil))
	assert.False(t, isControlResult(map[string]any{}))
	// error 非空 → 控制信号（不写流）
	assert.True(t, isControlResult(map[string]any{"error": "cancelled"}))
	// error 空串 / 非 string → 非控制信号
	assert.False(t, isControlResult(map[string]any{"error": ""}))
	assert.False(t, isControlResult(map[string]any{"error": 123}))
	// 正常 answer → 非控制信号
	assert.False(t, isControlResult(map[string]any{"output": "text", "result_type": "answer"}))
}

// TestStartRound_ResumeContinuation续用turn且带轮前基线 验证启动模式与轮前快照。
func TestStartRound_ResumeContinuation续用turn且带轮前基线(t *testing.T) {
	h := newTestNativeHarness(t)
	h.sess = session.NewSession(session.WithSessionID("round-sess-1"))

	first := h.startRound(context.Background(), "任务", startModeDefault, nil)
	assert.True(t, first.TurnOpened)
	assert.Equal(t, 1, first.Turn.TurnNumber)
	require.NotNil(t, first.PreRoundSnapshot)
	assert.Equal(t, 0, first.PreRoundSnapshot.IterationIndex)

	second := h.startRound(context.Background(), "任务", startModeResumeContinuation, nil)
	assert.False(t, second.TurnOpened)
	assert.Equal(t, first.Turn.TurnNumber, second.Turn.TurnNumber)
}
