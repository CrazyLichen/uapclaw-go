package processcli

import (
	"bytes"
	"context"
	"errors"
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

// fixedStream 构造一个按序触发 handler 的 streamDriver。
// 返回的 drove 记录实际被消费到第几条（用于验证「提前停流」）。
func fixedStream(events ...*swruntime.RuntimeEvent) (streamDriver, *int) {
	drove := 0
	driver := func(handler func(*swruntime.RuntimeEvent) bool) error {
		for _, event := range events {
			drove++
			if !handler(event) {
				return nil
			}
		}
		return nil
	}
	return driver, &drove
}

// TestConsumeNoninteractive_交互事件报错
// Python: machine.py:431-435 event_type in _INTERACTION_EVENTS → INTERACTION_REQUIRED
func TestConsumeNoninteractive_交互事件报错(t *testing.T) {
	run := newMachineRunFixture(t)
	driver, _ := fixedStream(asEvent("chat.ask_user_question"))
	_, err := run.consumeNoninteractive(driver)
	require.Error(t, err)
	var runErr *MachineRunError
	require.ErrorAs(t, err, &runErr)
	require.Equal(t, "INTERACTION_REQUIRED", runErr.Code)
}

// TestConsumeNoninteractive_交互事件立刻停流
// Python: machine.py:425 async for 里 raise → 后续事件不再被消费
//
// 这是「边收边处理」的核心不变量：不能先把流攒完再判。
func TestConsumeNoninteractive_交互事件立刻停流(t *testing.T) {
	run := newMachineRunFixture(t)
	driver, drove := fixedStream(
		asEvent("chat.delta"),
		asEvent("chat.ask_user_question"),
		asEvent("chat.delta"),
		asEvent("chat.final"),
	)
	_, err := run.consumeNoninteractive(driver)
	require.Error(t, err)
	require.Equal(t, 2, *drove, "交互事件后必须立刻停流，不得继续消费")
}

// TestConsumeNoninteractive_final标记完成
// Python: machine.py:429-430 event.ok and (is_complete or event_type == "chat.final")
func TestConsumeNoninteractive_final标记完成(t *testing.T) {
	run := newMachineRunFixture(t)
	driver, _ := fixedStream(asEvent("chat.final"))
	completed, err := run.consumeNoninteractive(driver)
	require.NoError(t, err)
	require.True(t, completed)
}

// TestConsumeNoninteractive_无完成事件返回false
func TestConsumeNoninteractive_无完成事件返回false(t *testing.T) {
	run := newMachineRunFixture(t)
	driver, _ := fixedStream(asEvent("chat.delta"))
	completed, err := run.consumeNoninteractive(driver)
	require.NoError(t, err)
	require.False(t, completed)
}

// TestConsumeNoninteractive_写失败立刻停流
// Python: machine.py:426 observe 抛 OSError → 流中断
func TestConsumeNoninteractive_写失败立刻停流(t *testing.T) {
	writer, err := NewOneShotWriter(errWriter{}, "r1")
	require.NoError(t, err)
	run := NewMachineRun(&protocol.OneShotRunInput{Input: "hi"}, writer, nil)

	driver, drove := fixedStream(asEvent("chat.delta"), asEvent("chat.final"))
	_, consumeErr := run.consumeNoninteractive(driver)
	require.Error(t, consumeErr)
	require.Equal(t, 1, *drove, "写失败后不再消费")
	var outputErr *OutputError
	require.ErrorAs(t, consumeErr, &outputErr, "写失败必须是 OutputError")
}

// TestConsumeNoninteractive_流错误上抛
// 流本身返回错误且 handler 未判定失败时，以流错误为准
func TestConsumeNoninteractive_流错误上抛(t *testing.T) {
	run := newMachineRunFixture(t)
	streamErr := errors.New("stream broke")
	driver := func(func(*swruntime.RuntimeEvent) bool) error { return streamErr }
	_, got := run.consumeNoninteractive(driver)
	require.ErrorIs(t, got, streamErr)
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

// TestClassifyFailure_输出关闭时报OUTPUT_CLOSED
// Python: machine.py:562-564 code = "OUTPUT_CLOSED" if writer.broken else "RUNTIME_FAILED"
func TestClassifyFailure_输出关闭时报OUTPUT_CLOSED(t *testing.T) {
	writer, err := NewOneShotWriter(errWriter{}, "r1")
	require.NoError(t, err)
	run := NewMachineRun(&protocol.OneShotRunInput{Input: "hi"}, writer, nil)

	// 先触发一次写失败，把 writer.Broken 置真
	ev, err := protocol.NewOneShotEvent(protocol.OneShotEventParams{
		Sequence: 0, RequestID: "r1", EventType: "chat.delta",
	})
	require.NoError(t, err)
	writeErr := writer.WriteEvent(ev)
	require.Error(t, writeErr)

	run.classifyFailure(writeErr, writer)
	require.NotNil(t, run.err)
	require.Equal(t, "OUTPUT_CLOSED", run.err.Code)
	require.Equal(t, "Command I/O failed.", run.err.Message)
	require.Equal(t, string(protocol.StatusFailed), run.status)
}

// TestClassifyFailure_非输出错误仍是RUNTIME_FAILED
// Python: machine.py:563 writer.broken 为假时用 RUNTIME_FAILED
func TestClassifyFailure_非输出错误仍是RUNTIME_FAILED(t *testing.T) {
	writer, err := NewOneShotWriter(discardWriter{}, "r1")
	require.NoError(t, err)
	run := NewMachineRun(&protocol.OneShotRunInput{Input: "hi"}, writer, nil)

	run.classifyFailure(&OutputError{Err: errors.New("io")}, writer)
	require.NotNil(t, run.err)
	require.Equal(t, "RUNTIME_FAILED", run.err.Code)
}

// TestClassifyFailure_超时与取消优先
// Python: machine.py:540-561 两个分支在 OSError 之前
func TestClassifyFailure_超时与取消优先(t *testing.T) {
	writer, err := NewOneShotWriter(discardWriter{}, "r1")
	require.NoError(t, err)

	timeoutRun := NewMachineRun(&protocol.OneShotRunInput{Input: "hi"}, writer, nil)
	timeoutRun.classifyFailure(context.DeadlineExceeded, writer)
	require.Equal(t, "TIMEOUT", timeoutRun.err.Code)
	require.Equal(t, string(protocol.StatusTimedOut), timeoutRun.status)
	require.Equal(t, 124, timeoutRun.exitCode)

	cancelRun := NewMachineRun(&protocol.OneShotRunInput{Input: "hi"}, writer, nil)
	cancelRun.classifyFailure(context.Canceled, writer)
	require.Equal(t, "CANCELLED", cancelRun.err.Code)
	require.Equal(t, 130, cancelRun.exitCode)
}

// TestRunMachine_cleanupControlSteps control 非 nil 时 cleanup 走
// control_input / control_streams 两步。
// Python: machine.py:457-458, 472-473。
func TestRunMachine_cleanupControlSteps(t *testing.T) {
	out := &bytes.Buffer{}
	writer, err := NewOneShotWriter(out, "r1")
	require.NoError(t, err)
	ctrl := NewDuplexController(nil, writer, true)
	run := &MachineRun{
		writer:  writer,
		control: ctrl,
		client:  nil, // client 为 nil 时只记录清理错误（Python :459-464）
	}
	run.Cleanup(context.Background())
	// control_input 步骤成功执行（无错误不留 cleanup 记录）
	require.True(t, ctrl.stoppingInput)
}

// TestRunMachine_cleanupControlStreams client 非 nil 时 control_streams 也执行。
// Python: machine.py:472-473。
func TestRunMachine_cleanupControlStreams(t *testing.T) {
	out := &bytes.Buffer{}
	writer, err := NewOneShotWriter(out, "r1")
	require.NoError(t, err)
	sid := "s1"
	writer.SessionID = &sid
	ctrl := NewDuplexController(nil, writer, true)
	client := NewInProcessRuntimeClient(nil)
	run := &MachineRun{
		writer:  writer,
		control: ctrl,
		client:  client,
	}
	run.Cleanup(context.Background())
	require.True(t, ctrl.stoppingStreams)
}

// TestRunMachine_control消费路径 control 非 nil 时走 DuplexController.Consume。
// Python: machine.py:364-372。
func TestRunMachine_control消费路径(t *testing.T) {
	out := &bytes.Buffer{}
	writer, err := NewOneShotWriter(out, "r1")
	require.NoError(t, err)
	sid := "s1"
	writer.SessionID = &sid
	ctrl := NewDuplexController(nil, writer, true)
	sidReq := "s1"
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "process_cli", SessionID: &sidReq}
	run := &MachineRun{
		writer:  writer,
		summary: NewRunSummary(),
		control: ctrl,
		client:  NewInProcessRuntimeClient(nil),
		request: request,
	}
	emit := []*swruntime.RuntimeEvent{
		{Payload: map[string]any{"event_type": "chat.final", "content": "done"}, OK: true, IsComplete: true},
	}
	idx := 0
	streamFn := streamDriver(func(handler func(*swruntime.RuntimeEvent) bool) error {
		if idx < len(emit) {
			handler(emit[idx])
			idx++
		}
		return nil
	})
	completed, consumeErr := run.executeConsumeStep(context.Background(), streamFn)
	require.NoError(t, consumeErr)
	require.True(t, completed)
}
