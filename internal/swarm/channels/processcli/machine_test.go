package processcli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestNormalizeRunError_达上限时翻译
// Python: machine.py:390-401 _normalize_run_error
func TestNormalizeRunError_达上限时翻译(t *testing.T) {
	maxTurns := 2
	run := &MachineRun{
		runInput: &protocol.OneShotRunInput{MaxTurns: &maxTurns},
		limits:   NewRunLimits(&maxTurns, nil),
	}
	_ = run.limits.Observe(nil)

	// 让 model_calls 达到 2
	for i := 0; i < 2; i++ {
		run.limits.Observe(usageEvent(0))
	}

	out := run.NormalizeRunError(&protocol.RuntimeErrorInfo{
		Code: "MAX_ITERATIONS_REACHED", Message: "max",
	})
	require.Equal(t, "TURN_LIMIT_EXCEEDED", out.Code)
	require.Equal(t, "Run reached max_turns before completion.", out.Message)
}

// TestNormalizeRunError_未达上限不翻译
// Python: machine.py:393 at_turn_limit 为假时原样返回
func TestNormalizeRunError_未达上限不翻译(t *testing.T) {
	maxTurns := 5
	run := &MachineRun{
		runInput: &protocol.OneShotRunInput{MaxTurns: &maxTurns},
		limits:   NewRunLimits(&maxTurns, nil),
	}
	run.limits.Observe(usageEvent(0))

	out := run.NormalizeRunError(&protocol.RuntimeErrorInfo{
		Code: "MAX_ITERATIONS_REACHED", Message: "max",
	})
	require.Equal(t, "MAX_ITERATIONS_REACHED", out.Code)
}

// TestNormalizeRunError_非迭代错误不翻译
// Python: machine.py:394 is_iteration_error 为假时原样返回
func TestNormalizeRunError_非迭代错误不翻译(t *testing.T) {
	maxTurns := 1
	run := &MachineRun{
		runInput: &protocol.OneShotRunInput{MaxTurns: &maxTurns},
		limits:   NewRunLimits(&maxTurns, nil),
	}
	run.limits.Observe(usageEvent(0))

	out := run.NormalizeRunError(&protocol.RuntimeErrorInfo{Code: "OTHER", Message: "m"})
	require.Equal(t, "OTHER", out.Code)
}

// TestNormalizeRunError_无maxTurns不翻译
func TestNormalizeRunError_无maxTurns不翻译(t *testing.T) {
	run := &MachineRun{
		runInput: &protocol.OneShotRunInput{},
		limits:   NewRunLimits(nil, nil),
	}
	out := run.NormalizeRunError(&protocol.RuntimeErrorInfo{
		Code: "MAX_ITERATIONS_REACHED", Message: "m",
	})
	require.Equal(t, "MAX_ITERATIONS_REACHED", out.Code)
}

// TestFailure_退出码映射 验证 failure 对 130 的特殊处理。
// Python: machine_entry.py:116 status = CANCELLED if exit_code == 130 else FAILED
func TestFailure_退出码映射(t *testing.T) {
	w, err := NewOneShotWriter(discardWriter{}, "r1")
	require.NoError(t, err)

	require.Equal(t, string(protocol.StatusCancelled), failure(w, "X", "m", 130).Status)
	require.Equal(t, string(protocol.StatusFailed), failure(w, "X", "m", 2).Status)
	require.Equal(t, string(protocol.StatusFailed), failure(w, "X", "m", 1).Status)
}

// TestWorkspaceParams_新建时补默认
// Python: machine.py:133-142 cwd or os.getcwd()；project_dir or cwd；trusted or [project_dir]
func TestWorkspaceParams_新建时补默认(t *testing.T) {
	params := workspaceParams(&protocol.OneShotRunInput{}, false)
	require.NotEmpty(t, params["cwd"])
	require.Equal(t, params["cwd"], params["project_dir"])
	require.Equal(t, []any{params["project_dir"]}, params["trusted_dirs"])
}

// TestWorkspaceParams_恢复时省略即继承
// Python: machine.py:123-124 resumed 且 workspace 为 None → {}
func TestWorkspaceParams_恢复时省略即继承(t *testing.T) {
	params := workspaceParams(&protocol.OneShotRunInput{}, true)
	require.Empty(t, params, "恢复会话时省略 workspace 必须返回空表")
}

// TestWorkspaceParams_恢复时只带显式字段
// Python: machine.py:125-132
func TestWorkspaceParams_恢复时只带显式字段(t *testing.T) {
	cwd := "/tmp/x"
	params := workspaceParams(&protocol.OneShotRunInput{
		Workspace: &protocol.WorkspaceSpec{Cwd: &cwd},
	}, true)
	require.Equal(t, "/tmp/x", params["cwd"])
	require.NotContains(t, params, "project_dir", "未显式给出则不得覆写")
}

// TestCancelRequest_sessionScope时target为空
// Python: machine.py:110 "target_request_id": "" if session_scope else request.request_id
func TestCancelRequest_sessionScope时target为空(t *testing.T) {
	req := &schema.AgentRequest{RequestID: "r1", ChannelID: machineChannelID}
	cancelled := cancelRequest(req, true)

	params, err := unmarshalParams(cancelled.Params)
	require.NoError(t, err)
	require.Equal(t, "", params["target_request_id"])
	require.Equal(t, "cancel", params["intent"])
}

// TestCancelRequest_非sessionScope带目标
func TestCancelRequest_非sessionScope带目标(t *testing.T) {
	req := &schema.AgentRequest{RequestID: "r1", ChannelID: machineChannelID}
	cancelled := cancelRequest(req, false)

	params, err := unmarshalParams(cancelled.Params)
	require.NoError(t, err)
	require.Equal(t, "r1", params["target_request_id"])
}

// TestLimitMessage_三档映射
// Python: machine.py:408-412
func TestLimitMessage_三档映射(t *testing.T) {
	require.Contains(t, limitMessage("BUDGET_METER_UNAVAILABLE"), "usable cost")
	require.Contains(t, limitMessage("BUDGET_EXCEEDED"), "max_budget_usd")
	require.Contains(t, limitMessage("TURN_LIMIT_EXCEEDED"), "max_turns")
	require.Contains(t, limitMessage("OTHER"), "configured limit")
}

// TestRecordCleanupErrors_并入details
// Python: machine.py:490-503
func TestRecordCleanupErrors_并入details(t *testing.T) {
	run := &MachineRun{status: string(protocol.StatusCompleted)}
	run.cleanupErrors = []string{"cancel", "runtime_close"}
	run.RecordCleanupErrors()

	require.NotNil(t, run.err)
	require.Equal(t, "SHUTDOWN_FAILED", run.err.Code)
	require.Equal(t, string(protocol.StatusFailed), run.status)
	require.Equal(t, []any{"cancel", "runtime_close"}, run.err.Details["cleanup_errors"])
}

// TestRecordCleanupErrors_已有错误时保留
// Python: machine.py:492-496 self.error or ...；if self.error is None: fail(...)
func TestRecordCleanupErrors_已有错误时保留(t *testing.T) {
	original := &protocol.RuntimeErrorInfo{Code: "ORIGINAL", Message: "m"}
	run := &MachineRun{err: original, status: string(protocol.StatusFailed)}
	run.cleanupErrors = []string{"runtime_close"}
	run.RecordCleanupErrors()

	require.Equal(t, "ORIGINAL", run.err.Code, "保留第一条错误码")
	require.Equal(t, []any{"runtime_close"}, run.err.Details["cleanup_errors"])
}

// TestCleanupStep_失败只记录不阻断
// Python: machine.py:446-448 异常只 append + warning
func TestCleanupStep_失败只记录不阻断(t *testing.T) {
	run := &MachineRun{status: string(protocol.StatusCompleted)}
	run.cleanupStep(t.Context(), "step_a", func(context.Context) error {
		return context.DeadlineExceeded
	})
	require.Equal(t, []string{"step_a"}, run.cleanupErrors)
	require.Nil(t, run.err, "非取消错误不置终态")
}

// TestConsumeEvents_交互事件报错
// Python: machine.py:431-435 event_type in _INTERACTION_EVENTS → INTERACTION_REQUIRED
func TestConsumeEvents_交互事件报错(t *testing.T) {
	run := newMachineRunFixture(t)
	_, err := run.consumeEvents([]*swruntime.RuntimeEvent{asEvent("chat.ask_user_question")})
	require.Error(t, err)
	var runErr *MachineRunError
	require.ErrorAs(t, err, &runErr)
	require.Equal(t, "INTERACTION_REQUIRED", runErr.Code)
}

// TestConsumeEvents_final标记完成
// Python: machine.py:429-430 event.ok and (is_complete or event_type == "chat.final")
func TestConsumeEvents_final标记完成(t *testing.T) {
	run := newMachineRunFixture(t)
	completed, err := run.consumeEvents([]*swruntime.RuntimeEvent{asEvent("chat.final")})
	require.NoError(t, err)
	require.True(t, completed)
}

// TestConsumeEvents_无完成事件返回false
func TestConsumeEvents_无完成事件返回false(t *testing.T) {
	run := newMachineRunFixture(t)
	completed, err := run.consumeEvents([]*swruntime.RuntimeEvent{asEvent("chat.delta")})
	require.NoError(t, err)
	require.False(t, completed)
}

// asEvent 构造一条最小 Runtime 事件。
func asEvent(eventType string) *swruntime.RuntimeEvent {
	return &swruntime.RuntimeEvent{
		Payload: map[string]any{"event_type": eventType},
		OK:      true,
	}
}

// newMachineRunFixture 构造一个可观察的最小 MachineRun。
func newMachineRunFixture(t *testing.T) *MachineRun {
	t.Helper()
	writer, err := NewOneShotWriter(discardWriter{}, "r1")
	require.NoError(t, err)
	return NewMachineRun(&protocol.OneShotRunInput{Input: "hi"}, writer, nil)
}

// TestValidateHostToolsForRun_本批拒绝
// ⤵️ 回填 20.6：用户决策 hostTools 非空即报错
func TestValidateHostToolsForRun_本批拒绝(t *testing.T) {
	err := validateHostToolsForRun(&protocol.OneShotRunInput{})
	require.Error(t, err)
	var runErr *MachineRunError
	require.ErrorAs(t, err, &runErr)
	require.Equal(t, "INVALID_HOST_TOOL", runErr.Code)
	require.Contains(t, runErr.Message, "20.6")
}
