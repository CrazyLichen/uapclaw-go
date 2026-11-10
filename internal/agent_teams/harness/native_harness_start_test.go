package harness

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
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

	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State(), "Start 后保持 idle，首条 send 才进入 running")
	assert.Equal(t, sess.GetSessionID(), h.SessionID())

	require.NoError(t, h.Stop(context.Background()))
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, h.State())
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
	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State())
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

// TestStartRound_按启动模式决定是否新开turn 表驱动验证 keepsTurn 判定。
// Python: keeps_turn = mode ∈ {CONTINUES_TURN, FAILURE_RETRY, RESUME_CONTINUATION}
// or isinstance(query, InteractiveInput)（native_harness.py L1316-1325）
func TestStartRound_按启动模式决定是否新开turn(t *testing.T) {
	cases := []struct {
		name       string
		query      any
		mode       startMode
		wantOpened bool
		wantNumber int
	}{
		{"空闲起轮开新turn", "q", startModeDefault, true, 2},
		{"排队输入开新turn", "q", startModeFollowUp, true, 2},
		{"崩溃重试续用", "q", startModeFailureRetry, false, 1},
		{"暂停续跑续用", "q", startModeResumeContinuation, false, 1},
		{"显式续用", "q", startModeContinuesTurn, false, 1},
		{"中断应答续用", &interaction.InteractiveInput{}, startModeDefault, false, 1},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestNativeHarness(t)
			h.sess = session.NewSession(session.WithSessionID(fmt.Sprintf("mode-sess-%d", i)))

			first := h.startRound(context.Background(), "任务", startModeDefault, nil)
			require.Equal(t, 1, first.Turn.TurnNumber)

			second := h.startRound(context.Background(), tc.query, tc.mode, nil)
			assert.Equal(t, tc.wantOpened, second.TurnOpened)
			assert.Equal(t, tc.wantNumber, second.Turn.TurnNumber)
			if tc.wantOpened {
				assert.NotEqual(t, first.Turn.TurnID, second.Turn.TurnID, "新开 turn 必须换新工单号")
			} else {
				assert.Equal(t, first.Turn.TurnID, second.Turn.TurnID, "续用必须复用同一工单号")
			}
		})
	}
}

// TestActiveRound_访问器反映在飞轮次 验证 rails 可经 harness 反向引用读到活轮。
// Python: NativeHarness.active_round property（native_harness.py L268-275）
func TestActiveRound_访问器反映在飞轮次(t *testing.T) {
	h := newTestNativeHarness(t)
	h.sess = session.NewSession(session.WithSessionID("active-round-sess"))

	assert.Nil(t, h.ActiveRound(), "未起轮时无活轮")

	active := h.startRound(context.Background(), "任务", startModeDefault, nil)

	require.NotNil(t, h.ActiveRound())
	assert.Equal(t, active.RoundID, h.ActiveRound().RoundID)
	assert.Equal(t, active.Turn, h.ActiveRound().Turn)
}

// TestStop后Start重建运行期容器 回归 G-08：
// Stop 后 started 必须复位，Start 才能重建 supervisor/forwarder 与输出队列。
func TestStop后Start重建运行期容器(t *testing.T) {
	h := newTestNativeHarness(t)
	sess := session.CreateAgentSession("restart-sess", h.DeepAgent().Card(), nil)
	require.NoError(t, sess.PreRun(context.Background(), map[string]any{}))

	require.NoError(t, h.Start(context.Background(), sess))
	require.NoError(t, h.Stop(context.Background()))
	require.Equal(t, harnessprotocol.HarnessStateTerminated, h.State())

	require.NoError(t, h.Start(context.Background(), sess))
	assert.Equal(t, harnessprotocol.HarnessStateIdle, h.State(), "重建后应回到 IDLE 并接受新命令")
	require.NoError(t, h.Stop(context.Background()))
}
