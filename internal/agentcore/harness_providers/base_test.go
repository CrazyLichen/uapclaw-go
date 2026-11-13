package harness_providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// 本文件对应 Python tests/unit_tests/harness_providers/test_base.py（644 行 / 20 用例）。
//
// 按 spec §7.2 的裁决，用真实的 SerializedTurnHarness 子类而非 fake：
// scriptedHarness 嵌入基座以获得 Emit 等能力，同时实现 providerHooks。

// scriptedHarness 由测试脚本控制的 providerHooks 实现。
// Python: test_base.py:62-131 _ScriptedHarness
type scriptedHarness struct {
	*SerializedTurnHarness

	// mu 保护下列计数与切片（基座在监督协程写、测试主协程读）
	mu                sync.Mutex
	opened            int
	closed            int
	steered           []string
	steeredMessageIDs []string
	interrupted       []harnessprotocol.AbortMode
	release           chan struct{}
	crashNext         bool
	releaseOnce       bool
}

// newScriptedHarness 构造一个已注入自身为 hooks 的基座。
func newScriptedHarness(t *testing.T) *scriptedHarness {
	t.Helper()
	harness := &scriptedHarness{
		release: make(chan struct{}),
	}
	harness.closedChan()
	harness.SerializedTurnHarness = NewSerializedTurnHarness(HarnessOptions{
		Card: harnessprotocol.HarnessCard{
			Name:                       "scripted",
			ImplementationVersion:      "1.0",
			ProtocolVersion:            harnessprotocol.ProtocolVersion,
			CompatibleProtocolVersions: map[string]bool{harnessprotocol.ProtocolVersion: true},
			Capabilities: map[harnessprotocol.HarnessCapability]bool{
				harnessprotocol.HarnessCapabilitySteer:         true,
				harnessprotocol.HarnessCapabilityGracefulAbort: true,
			},
			OptionalHostCapabilities: map[harnessprotocol.HostCapability]bool{
				harnessprotocol.HostCapabilityUserInput:      true,
				harnessprotocol.HostCapabilityCheckpointSink: true,
			},
		},
		Hooks:               harness,
		EventBufferCapacity: 64, // test_base.py:77
	})
	return harness
}

// closedChan 让 release 处于「已放行」状态。
func (s *scriptedHarness) closedChan() {
	select {
	case <-s.release:
	default:
		close(s.release)
	}
}

// block 让下一次 ExecuteTurn 挂住，直到 releaseOnce 释放。
func (s *scriptedHarness) block() {
	s.release = make(chan struct{})
}

// releaseChan 释放挂住的轮次（幂等）。
func (s *scriptedHarness) releaseChan() {
	s.closedChan()
}

// OpenSession 记录开启并向宿主发一条 SESSION_ACTIVATED checkpoint。
// Python: test_base.py:88-94
func (s *scriptedHarness) OpenSession(ctx context.Context,
	harnessContext harnessprotocol.HarnessContext) (*string, error) {
	s.mu.Lock()
	s.opened++
	s.mu.Unlock()
	data := harnessprotocol.JsonObject{"cursor": 1}
	if _, err := s.PublishCheckpoint(ctx, data,
		harnessprotocol.CheckpointReasonSessionActivated, "1"); err != nil {
		return nil, err
	}
	sessionID := "scripted-session"
	return &sessionID, nil
}

// CloseSession 记录关闭并释放挂住的轮次。
// Python: test_base.py:96-98（self.release.set()）
func (s *scriptedHarness) CloseSession(ctx context.Context) error {
	s.mu.Lock()
	s.closed++
	s.mu.Unlock()
	s.releaseChan()
	return nil
}

// ExecuteTurn 按脚本返回结果；crashNext 时返回错误。
// Python: test_base.py:100-131
func (s *scriptedHarness) ExecuteTurn(ctx context.Context,
	turn *PendingTurn) (harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	timing := NewTurnTiming()
	if s.crashNext {
		s.mu.Lock()
		s.crashNext = false
		s.mu.Unlock()
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{},
			&scriptedCrashError{}
	}
	output, err := harnessprotocol.NewOutputEvent(
		turn.TurnID+":answer",
		harnessprotocol.OutputKindText,
		"hi",
		harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelAnswer,
		0,
		"text",
		nil,
	)
	if err != nil {
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}, err
	}
	if err := s.Emit(ctx, output, EmitOptions{Turn: turn}); err != nil {
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}, err
	}
	<-s.release
	if turn.abortRequested() {
		result := InterruptedResult(turn, InterruptedResultOptions{
			ProviderName: "scripted",
			Timing:       timing,
		})
		return harnessprotocol.TurnEventKindAborted, result, nil
	}
	finalOutput := "done:" + HarnessInputText(turn.Content)
	result, err := harnessprotocol.NewTurnResult(
		harnessprotocol.TurnStatusCompleted,
		harnessprotocol.ResultOptions{FinalOutput: finalOutput})
	if err != nil {
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.TurnEventKindFinished, result, nil
}

// Steer 记录注入的文本与 message_id。
// Python: test_base.py:133-136
func (s *scriptedHarness) Steer(ctx context.Context, turn *PendingTurn,
	content harnessprotocol.HarnessInput, messageID string) error {
	s.mu.Lock()
	s.steered = append(s.steered, HarnessInputText(content))
	s.steeredMessageIDs = append(s.steeredMessageIDs, messageID)
	s.mu.Unlock()
	return nil
}

// InterruptTurn 记录中止模式并释放挂住的轮次。
// Python: test_base.py:138-140
func (s *scriptedHarness) InterruptTurn(ctx context.Context,
	turn *PendingTurn, mode harnessprotocol.AbortMode) error {
	s.mu.Lock()
	s.interrupted = append(s.interrupted, mode)
	s.mu.Unlock()
	s.releaseChan()
	return nil
}

// ValidateContext 默认不追加校验。
func (s *scriptedHarness) ValidateContext(ctx context.Context,
	harnessContext harnessprotocol.HarnessContext) error {
	return nil
}

// ListModels 未声明模型发现能力时不可达（基座先判能力）。
func (s *scriptedHarness) ListModels(ctx context.Context) ([]harnessprotocol.ModelOption, error) {
	return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "scripted does not support model discovery",
	}
}

// AuthFallbackPayload 未配置回退。
func (s *scriptedHarness) AuthFallbackPayload() (harnessprotocol.JsonObject, bool) {
	return nil, false
}

// ReconnectForAuth 无回退。
func (s *scriptedHarness) ReconnectForAuth(ctx context.Context, fallback bool) error {
	return &harnessprotocol.HarnessProtocolError{Msg: "scripted has no authentication fallback"}
}

// OnAuthEndpointChanged no-op。
func (s *scriptedHarness) OnAuthEndpointChanged(ctx context.Context, fallback bool) error {
	return nil
}

// ApplyModelSelection 未声明模型选择能力时不可达。
func (s *scriptedHarness) ApplyModelSelection(ctx context.Context,
	selection harnessprotocol.ModelSelection) error {
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: "scripted does not support model selection",
	}
}

// openedCount 线程安全地读开启次数。
func (s *scriptedHarness) openedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened
}

// closedCount 线程安全地读关闭次数。
func (s *scriptedHarness) closedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// steeredSnapshot 线程安全地读注入记录。
func (s *scriptedHarness) steeredSnapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steered...), append([]string(nil), s.steeredMessageIDs...)
}

// interruptedSnapshot 线程安全地读中止记录。
func (s *scriptedHarness) interruptedSnapshot() []harnessprotocol.AbortMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]harnessprotocol.AbortMode(nil), s.interrupted...)
}

// setCrashNext 线程安全地置下次崩溃标记。
func (s *scriptedHarness) setCrashNext(value bool) {
	s.mu.Lock()
	s.crashNext = value
	s.mu.Unlock()
}

// scriptedCrashError 脚本化的崩溃错误（类型名用于断言 error.code）。
type scriptedCrashError struct{}

// Error 实现 error 接口。
func (e *scriptedCrashError) Error() string { return "boom" }

// testHarnessContext 构造测试用宿主上下文。
func testHarnessContext() harnessprotocol.HarnessContext {
	return harnessprotocol.HarnessContext{
		AgentName:       "test",
		AgentID:         "agent-1",
		HostSessionID:   "host-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
		HostCapabilities: map[harnessprotocol.HostCapability]bool{
			harnessprotocol.HostCapabilityUserInput:      true,
			harnessprotocol.HostCapabilityCheckpointSink: true,
		},
	}
}

// collectTurn 消费一个轮次的全部事件直到终态。
func collectTurn(t *testing.T, harness turnEventSource, turnID string) []*harnessprotocol.HarnessEvent {
	t.Helper()
	cursor := harness.TurnEvents(&turnID)
	ctx := context.Background()
	var events []*harnessprotocol.HarnessEvent
	for {
		event, err := cursor.Next(ctx)
		if errors.Is(err, harnessprotocol.ErrEventStreamDone) {
			break
		}
		require.NoError(t, err)
		events = append(events, event)
	}
	return events
}

// stateSource 能报告当前相位的对象（基座及其测试子类）。
type stateSource interface {
	State() harnessprotocol.HarnessState
}

// turnEventSource 能提供单轮观测流的对象（基座及其测试子类）。
type turnEventSource interface {
	TurnEvents(turnID *string) harnessprotocol.HarnessEventCursor
}

// terminalOf 取事件流里最后一条终态 lifecycle 事件。
func terminalOf(t *testing.T, events []*harnessprotocol.HarnessEvent) *harnessprotocol.TurnLifecycleEvent {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		kind, ok := turnLifecycleKind(events[i].Event)
		if !ok {
			continue
		}
		if harnessprotocol.IsTerminalTurnEventKind(kind) {
			if lifecycle, isValue := events[i].Event.(harnessprotocol.TurnLifecycleEvent); isValue {
				return &lifecycle
			}
			if lifecycle, isPtr := events[i].Event.(*harnessprotocol.TurnLifecycleEvent); isPtr {
				return lifecycle
			}
		}
	}
	t.Fatalf("事件流里没有终态 TurnLifecycleEvent")
	return nil
}

// newTaskInput 构造一个字符串输入。
func newTaskInput(t *testing.T, text string) harnessprotocol.HarnessInput {
	t.Helper()
	input, err := harnessprotocol.NewHarnessInput(text, nil)
	require.NoError(t, err)
	return input
}

// waitForState 等待基座进入目标相位。
func waitForState(t *testing.T, harness stateSource, want harnessprotocol.HarnessState) {
	t.Helper()
	require.Eventually(t, func() bool {
		return harness.State() == want
	}, 3*time.Second, 5*time.Millisecond, "等待状态 %s 超时，当前 %s", want, harness.State())
}

// TestSerializedTurnHarness_Start稳定在IDLE 对齐 test_base.py:193-208。
//
// Start 后应为 IDLE、开过一次会话、open 时发过一条 checkpoint。
func TestSerializedTurnHarness_Start稳定在IDLE(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	assert.Equal(t, harnessprotocol.HarnessStateIdle, harness.State())
	assert.Equal(t, 1, harness.openedCount())
	require.NotNil(t, harness.ProviderSessionID())
	assert.Equal(t, "scripted-session", *harness.ProviderSessionID())

	checkpoint, err := harness.ExportCheckpoint(ctx)
	require.NoError(t, err)
	require.NotNil(t, checkpoint)
	assert.Equal(t, 1, checkpoint.Sequence)
}

// TestSerializedTurnHarness_重复Start报错 对齐 test_base.py:275-276。
func TestSerializedTurnHarness_重复Start报错(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	err := harness.Start(ctx, testHarnessContext())
	var stateErr *harnessprotocol.HarnessStateError
	require.ErrorAs(t, err, &stateErr)
	assert.Contains(t, stateErr.Msg, "already started")
}

// TestSerializedTurnHarness_未Start时Emit报错 对齐 base.py:696-698。
func TestSerializedTurnHarness_未Start时Emit报错(t *testing.T) {
	harness := newScriptedHarness(t)
	output, err := harnessprotocol.NewOutputEvent(
		"o1", harnessprotocol.OutputKindText, "x", harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelAnswer, 0, "text", nil)
	require.NoError(t, err)

	err = harness.Emit(context.Background(), output, EmitOptions{})
	var protoErr *harnessprotocol.HarnessProtocolError
	require.ErrorAs(t, err, &protoErr)
	assert.Contains(t, protoErr.Msg, "outside an active cycle")
}

// TestSerializedTurnHarness_轮次串行与后续回执 对齐 test_base.py:211-227。
//
// 第二条 AUTO 降级 FOLLOW_UP；两条都完成并回到 IDLE。
func TestSerializedTurnHarness_轮次串行与后续回执(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	content := newTaskInput(t, "任务")

	first, err := harness.Send(ctx, content, harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.DeliveryModeAuto, first.AcceptedMode)

	// 第二条在有前序轮次时降级 FOLLOW_UP（base.py:403-404）
	second, err := harness.Send(ctx, content, harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.DeliveryModeFollowUp, second.AcceptedMode)

	for _, receipt := range []harnessprotocol.SendReceipt{first, second} {
		events := collectTurn(t, harness, receipt.TurnID)
		terminal := terminalOf(t, events)
		assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminal.Kind)
	}
	waitForState(t, harness, harnessprotocol.HarnessStateIdle)
}

// TestSerializedTurnHarness_STEER注入活跃轮次 对齐 test_base.py:229-249。
//
// 回执的 message_id 必须就是 provider 收到的那个（base.py:391-393）。
func TestSerializedTurnHarness_STEER注入活跃轮次(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	harness.block()
	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)

	steerReceipt, err := harness.Send(ctx, newTaskInput(t, "补充"), harnessprotocol.DeliveryModeSteer)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.DeliveryModeSteer, steerReceipt.AcceptedMode)
	assert.Equal(t, receipt.TurnID, steerReceipt.TurnID)
	steered, messageIDs := harness.steeredSnapshot()
	require.Len(t, messageIDs, 1)
	assert.Equal(t, steerReceipt.MessageID, messageIDs[0])
	assert.Equal(t, []string{"补充"}, steered)

	harness.releaseChan()
	events := collectTurn(t, harness, receipt.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminalOf(t, events).Kind)
}

// TestSerializedTurnHarness_空闲时STEER报错 对齐 test_base.py:251-270。
func TestSerializedTurnHarness_空闲时STEER报错(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	_, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeSteer)
	var stateErr *harnessprotocol.HarnessStateError
	require.ErrorAs(t, err, &stateErr)
	assert.Contains(t, stateErr.Msg, "no active turn to steer")
}

// TestSerializedTurnHarness_Provider崩溃变失败轮次 对齐 test_base.py:307-319。
func TestSerializedTurnHarness_Provider崩溃变失败轮次(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	harness.setCrashNext(true)
	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	terminal := terminalOf(t, events)
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, terminal.Kind)
	require.NotNil(t, terminal.Result)
	require.NotNil(t, terminal.Result.Error)
	// base.py:646 category="sdk_error"
	require.NotNil(t, terminal.Result.Error.Category)
	assert.Equal(t, "sdk_error", *terminal.Result.Error.Category)
	// base.py:645 code=type(exc).__name__
	require.NotNil(t, terminal.Result.Error.Code)
	assert.Equal(t, "scriptedCrashError", *terminal.Result.Error.Code)
	waitForState(t, harness, harnessprotocol.HarnessStateIdle)
}

// TestSerializedTurnHarness_Abort中止活跃轮次 对齐 test_base.py:251-270 的 abort 分支。
func TestSerializedTurnHarness_Abort中止活跃轮次(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	harness.block()
	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)

	require.NoError(t, harness.Abort(ctx, harnessprotocol.AbortModeGraceful))
	assert.Equal(t, []harnessprotocol.AbortMode{harnessprotocol.AbortModeGraceful}, harness.interruptedSnapshot())

	events := collectTurn(t, harness, receipt.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindAborted, terminalOf(t, events).Kind)
}

// TestSerializedTurnHarness_空闲时Abort是noop 对齐 base.py:436-438。
func TestSerializedTurnHarness_空闲时Abort是noop(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	require.NoError(t, harness.Abort(ctx, harnessprotocol.AbortModeGraceful))
	assert.Empty(t, harness.interruptedSnapshot())
}

// TestSerializedTurnHarness_Stop中止排队轮次并关流 对齐 test_base.py:285-305。
func TestSerializedTurnHarness_Stop中止排队轮次并关流(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))

	harness.block()
	active, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	queued, err := harness.Send(ctx, newTaskInput(t, "任务2"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	assert.Equal(t, harnessprotocol.DeliveryModeFollowUp, queued.AcceptedMode)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)

	require.NoError(t, harness.Stop(ctx))
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, harness.State())

	// 两个轮次都必须有终态事件（active 与 queued 都被中止）
	activeTerminal := terminalOf(t, collectTurn(t, harness, active.TurnID))
	assert.Equal(t, harnessprotocol.TurnEventKindAborted, activeTerminal.Kind)
	queuedTerminal := terminalOf(t, collectTurn(t, harness, queued.TurnID))
	assert.Equal(t, harnessprotocol.TurnEventKindAborted, queuedTerminal.Kind)

	// 停止后 Send 报错（base.py:426-427）
	_, err = harness.Send(ctx, newTaskInput(t, "迟到"), harnessprotocol.DeliveryModeAuto)
	var stateErr *harnessprotocol.HarnessStateError
	assert.ErrorAs(t, err, &stateErr)
}

// TestSerializedTurnHarness_Stop幂等 对齐 base.py:313-314。
func TestSerializedTurnHarness_Stop幂等(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	require.NoError(t, harness.Stop(ctx))
	require.NoError(t, harness.Stop(ctx))
	assert.Equal(t, 1, harness.closedCount())
}

// TestSerializedTurnHarness_未Start时Stop是noop 对齐 base.py:313-314 的循环回退。
func TestSerializedTurnHarness_未Start时Stop是noop(t *testing.T) {
	harness := newScriptedHarness(t)
	require.NoError(t, harness.Stop(context.Background()))
	assert.Equal(t, 0, harness.closedCount())
}

// TestSerializedTurnHarness_单消费者租约 对齐 test_base.py:321-330。
func TestSerializedTurnHarness_单消费者租约(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	cursor := harness.Events()
	require.NotNil(t, cursor)
	// 已有活跃迭代器时再取应得空游标（接口无 error，退化为 emptyCursor）
	second := harness.Events()
	require.NotNil(t, second)
	_, err := second.Next(ctx)
	assert.ErrorIs(t, err, harnessprotocol.ErrEventStreamDone)

	require.NoError(t, cursor.Close(ctx))
	third := harness.Events()
	_, err = third.Next(ctx)
	assert.NotErrorIs(t, err, harnessprotocol.ErrEventStreamDone, "释放租约后应能正常取游标")
	_ = third.Close(ctx)
}

// TestMergeModelSelection_保留未设置字段 对齐 test_base.py:363-368。
func TestMergeModelSelection_保留未设置字段(t *testing.T) {
	model := "m1"
	effort := "high"
	merged := MergeModelSelection(
		&harnessprotocol.ModelSelection{Model: &model, Effort: &effort},
		harnessprotocol.ModelSelection{Model: &model})
	require.NotNil(t, merged.Model)
	assert.Equal(t, "m1", *merged.Model)
	require.NotNil(t, merged.Effort)
	assert.Equal(t, "high", *merged.Effort, "update 未设置 Effort 时应保留 current 的值")
}

// TestMergeModelSelection_当前为空 直接返回 update。
func TestMergeModelSelection_当前为空(t *testing.T) {
	model := "m2"
	merged := MergeModelSelection(nil, harnessprotocol.ModelSelection{Model: &model})
	require.NotNil(t, merged.Model)
	assert.Equal(t, "m2", *merged.Model)
}

// TestSerializedTurnHarness_模型控制需要声明能力 对齐 test_base.py:370-379。
func TestSerializedTurnHarness_模型控制需要声明能力(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	_, err := harness.ListModels(ctx)
	var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
	require.ErrorAs(t, err, &unsupported)

	err = harness.SetModel(ctx, harnessprotocol.ModelSelection{})
	require.ErrorAs(t, err, &unsupported)
}

// TestSerializedTurnHarness_PauseResume不支持 对齐 base.py:444-449。
func TestSerializedTurnHarness_PauseResume不支持(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
	require.ErrorAs(t, harness.Pause(ctx), &unsupported)
	require.ErrorAs(t, harness.Resume(ctx, nil), &unsupported)
}

// TestSerializedTurnHarness_Stop后Send与Start均被拒 周期已结束。
func TestSerializedTurnHarness_Stop后Send与Start均被拒(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	require.NoError(t, harness.Stop(ctx))

	var stateErr *harnessprotocol.HarnessStateError
	_, sendErr := harness.Send(ctx, newTaskInput(t, "x"), harnessprotocol.DeliveryModeAuto)
	require.ErrorAs(t, sendErr, &stateErr)
	// Stop 后 cycleStarted=false，Start 可重新开周期（对应上游 _cycle_started 复位）
	assert.NoError(t, harness.Start(ctx, testHarnessContext()))
	require.NoError(t, harness.Stop(ctx))
}

// ──────────────────────────── 只读访问器 ────────────────────────────

// TestSerializedTurnHarness_只读访问器 五个访问器在启动前后的取值。
func TestSerializedTurnHarness_只读访问器(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()

	// 启动前
	assert.Equal(t, "scripted", harness.Card().Name)
	assert.Nil(t, harness.Context())
	assert.Nil(t, harness.ActiveTurn())
	assert.False(t, harness.FallbackActivated())
	assert.Equal(t, 64, harness.EventBufferConfig().Capacity)
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, harness.State())

	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	// 启动后
	require.NotNil(t, harness.Context())
	assert.Equal(t, "agent-1", harness.Context().AgentID)
	assert.Nil(t, harness.ActiveTurn())
}

// ──────────────────────────── checkpoint 测试分组 ────────────────────────────

// fakeSink 记录收到的 checkpoint。
type fakeSink struct {
	saved    []harnessprotocol.HarnessCheckpoint
	revision string
	err      error
}

// Save 实现 HarnessCheckpointSink。
func (f *fakeSink) Save(ctx context.Context, checkpoint harnessprotocol.HarnessCheckpoint,
	reason harnessprotocol.CheckpointReason, expectedRevision *string) (harnessprotocol.CheckpointSaveReceipt, error) {
	if f.err != nil {
		return harnessprotocol.CheckpointSaveReceipt{}, f.err
	}
	f.saved = append(f.saved, checkpoint)
	revision := f.revision
	if revision == "" {
		revision = "rev-1"
	}
	return harnessprotocol.CheckpointSaveReceipt{StorageRevision: revision}, nil
}

// TestSerializedTurnHarness_Checkpoint推送到sink 对齐 base.py:804-820。
func TestSerializedTurnHarness_Checkpoint推送到sink(t *testing.T) {
	sink := &fakeSink{}
	harness := newScriptedHarness(t)
	harnessContext := testHarnessContext()
	harnessContext.CheckpointSink = sink
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	// OpenSession 里已推过一次（脚本约定）
	require.Len(t, sink.saved, 1)
	assert.Equal(t, "scripted", sink.saved[0].Provider)
	assert.Equal(t, 1, sink.saved[0].Sequence)
	// 推送成功后应记录 storage revision
	require.NotNil(t, harness.checkpointStorageRevision)
	assert.Equal(t, "rev-1", *harness.checkpointStorageRevision)
}

// TestSerializedTurnHarness_Checkpoint推送失败仍保留快照 对齐 base.py:816-818。
//
// 先保存 latestCheckpoint 再推送：推送失败不丢快照。
func TestSerializedTurnHarness_Checkpoint推送失败仍保留快照(t *testing.T) {
	sink := &fakeSink{err: errors.New("sink down")}
	harness := newScriptedHarness(t)
	harnessContext := testHarnessContext()
	harnessContext.CheckpointSink = sink
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	checkpoint, err := harness.ExportCheckpoint(ctx)
	require.NoError(t, err)
	require.NotNil(t, checkpoint, "推送失败也必须保留最新快照")
	assert.Empty(t, sink.saved)
}

// TestSerializedTurnHarness_Checkpoint冲突只告警 对齐 base.py:813-815。
func TestSerializedTurnHarness_Checkpoint冲突只告警(t *testing.T) {
	sink := &fakeSink{err: &harnessprotocol.CheckpointConflictError{Msg: "stale"}}
	harness := newScriptedHarness(t)
	harnessContext := testHarnessContext()
	harnessContext.CheckpointSink = sink
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	checkpoint, err := harness.ExportCheckpoint(ctx)
	require.NoError(t, err)
	require.NotNil(t, checkpoint)
	// 冲突不更新 revision
	assert.Nil(t, harness.checkpointStorageRevision)
}

// TestSerializedTurnHarness_RestoredCheckpointData作用域校验 对齐 base.py:822-836。
func TestSerializedTurnHarness_RestoredCheckpointData作用域校验(t *testing.T) {
	harness := newScriptedHarness(t)
	harnessContext := testHarnessContext()

	t.Run("无 checkpoint 返回 nil", func(t *testing.T) {
		data, err := harness.RestoredCheckpointData(harnessContext)
		require.NoError(t, err)
		assert.Nil(t, data)
	})
	t.Run("provider 不匹配报错", func(t *testing.T) {
		scoped := harnessContext
		scoped.Checkpoint = &harnessprotocol.HarnessCheckpoint{Provider: "other"}
		_, err := harness.RestoredCheckpointData(scoped)
		var protoErr *harnessprotocol.HarnessProtocolError
		require.ErrorAs(t, err, &protoErr)
		assert.Contains(t, protoErr.Msg, "does not belong to")
	})
	t.Run("作用域不匹配报错", func(t *testing.T) {
		scoped := harnessContext
		scoped.Checkpoint = &harnessprotocol.HarnessCheckpoint{
			Provider:      "scripted",
			AgentID:       "别的 agent",
			HostSessionID: harnessContext.HostSessionID,
		}
		_, err := harness.RestoredCheckpointData(scoped)
		var protoErr *harnessprotocol.HarnessProtocolError
		require.ErrorAs(t, err, &protoErr)
		assert.Contains(t, protoErr.Msg, "scope does not match")
	})
	t.Run("匹配时返回数据并推进序号", func(t *testing.T) {
		scoped := harnessContext
		revision := "rev-9"
		scoped.Checkpoint = &harnessprotocol.HarnessCheckpoint{
			Provider:      "scripted",
			AgentID:       harnessContext.AgentID,
			HostSessionID: harnessContext.HostSessionID,
			Sequence:      7,
			Revision:      &revision,
			Data:          harnessprotocol.JsonObject{"k": "v"},
		}
		data, err := harness.RestoredCheckpointData(scoped)
		require.NoError(t, err)
		require.NotNil(t, data)
		assert.Equal(t, "v", data["k"])
		assert.Equal(t, 7, harness.checkpointSequence)
		require.NotNil(t, harness.checkpointStorageRevision)
		assert.Equal(t, "rev-9", *harness.checkpointStorageRevision)
	})
}

// ──────────────────────────── 认证回退 ────────────────────────────

// fallbackHarness 声明 AUTH_FALLBACK 能力并可按脚本控制回退行为。
// Python: test_base.py:455-499 _FallbackHarness
type fallbackHarness struct {
	scriptedHarness

	// mu 保护 seen/replayed/reconnectCalls/sessionEndpoint
	mu       sync.Mutex
	seen     map[string]bool
	replayed map[string]bool

	payload         harnessprotocol.JsonObject
	reconnectErr    error
	bookkeepingErr  error
	ratifyHandler   harnessprotocol.HarnessInteractionHandler
	sessionEndpoint string
	reconnectCalls  []bool
}

// AuthFallbackPayload 返回配置的回退载荷。
func (f *fallbackHarness) AuthFallbackPayload() (harnessprotocol.JsonObject, bool) {
	if f.payload == nil {
		return nil, false
	}
	return f.payload, true
}

// ReconnectForAuth 记录重连并可按脚本失败。
func (f *fallbackHarness) ReconnectForAuth(ctx context.Context, fallback bool) error {
	f.mu.Lock()
	f.reconnectCalls = append(f.reconnectCalls, fallback)
	f.mu.Unlock()
	if f.reconnectErr != nil && fallback {
		return f.reconnectErr
	}
	return nil
}

// OnAuthEndpointChanged 记录端点并可按脚本失败。
func (f *fallbackHarness) OnAuthEndpointChanged(ctx context.Context, fallback bool) error {
	f.mu.Lock()
	if fallback {
		f.sessionEndpoint = "fallback"
	} else {
		f.sessionEndpoint = "primary"
	}
	f.mu.Unlock()
	return f.bookkeepingErr
}

// newFallbackHarness 构造一个声明 AUTH_FALLBACK 的基座。
func newFallbackHarness(t *testing.T) *fallbackHarness {
	t.Helper()
	harness := &fallbackHarness{
		payload: harnessprotocol.JsonObject{"model": "backup-model"},
	}
	harness.scriptedHarness.release = make(chan struct{})
	harness.scriptedHarness.closedChan()
	harness.SerializedTurnHarness = NewSerializedTurnHarness(HarnessOptions{
		Card: harnessprotocol.HarnessCard{
			Name:                  "fallback",
			ImplementationVersion: "1.0",
			ProtocolVersion:       harnessprotocol.ProtocolVersion,
			CompatibleProtocolVersions: map[string]bool{
				harnessprotocol.ProtocolVersion: true,
			},
			Capabilities: map[harnessprotocol.HarnessCapability]bool{
				harnessprotocol.HarnessCapabilityAuthFallback:  true,
				harnessprotocol.HarnessCapabilityGracefulAbort: true,
			},
			OptionalHostCapabilities: map[harnessprotocol.HostCapability]bool{
				harnessprotocol.HostCapabilityProviderInteraction: true,
			},
		},
		Hooks:               harness,
		EventBufferCapacity: 64,
	})
	return harness
}

// OpenSession 开启会话（不发 checkpoint，与 scriptedHarness 区分）。
func (f *fallbackHarness) OpenSession(ctx context.Context,
	harnessContext harnessprotocol.HarnessContext) (*string, error) {
	f.opened++
	f.ratifyHandler = harnessContext.Interactions
	sessionID := "fallback-session"
	return &sessionID, nil
}

// ExecuteTurn 首次以 auth_required 失败，重放时成功。
func (f *fallbackHarness) ExecuteTurn(ctx context.Context, turn *PendingTurn) (
	harnessprotocol.TurnEventKind, harnessprotocol.TurnResult, error) {
	if !f.TryReplayed(turn) {
		category := "auth_required"
		authErr, buildErr := harnessprotocol.NewTurnError(
			"login expired", nil, &category, nil, nil)
		if buildErr != nil {
			return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}, buildErr
		}
		result, buildErr := harnessprotocol.NewTurnResult(
			harnessprotocol.TurnStatusFailed,
			harnessprotocol.ResultOptions{Error: &authErr})
		if buildErr != nil {
			return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}, buildErr
		}
		return harnessprotocol.TurnEventKindFailed, result, nil
	}
	finalOutput := "done:" + HarnessInputText(turn.Content)
	result, err := harnessprotocol.NewTurnResult(
		harnessprotocol.TurnStatusCompleted,
		harnessprotocol.ResultOptions{FinalOutput: finalOutput})
	if err != nil {
		return harnessprotocol.TurnEventKindFailed, harnessprotocol.TurnResult{}, err
	}
	return harnessprotocol.TurnEventKindFinished, result, nil
}

// replayed 记录哪些轮次已重放过（首次失败，第二次成功）。
func (f *fallbackHarness) TryReplayed(turn *PendingTurn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.replayed == nil {
		f.replayed = map[string]bool{}
	}
	if f.replayed[turn.TurnID] {
		return true
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	if f.seen[turn.TurnID] {
		f.replayed[turn.TurnID] = true
		return true
	}
	f.seen[turn.TurnID] = true
	return false
}

// reconnectSnapshot 线程安全地读重连记录。
func (f *fallbackHarness) reconnectSnapshot() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.reconnectCalls...)
}

// endpointSnapshot 线程安全地读当前端点。
func (f *fallbackHarness) endpointSnapshot() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessionEndpoint
}

// ratifier 按脚本批准或拒绝宿主扩展请求。
// Python: test_base.py:501-530 _Ratifier
type ratifier struct {
	approve  bool
	holdBack bool
}

// Handle 批准或拒绝 provider 扩展请求。
func (r *ratifier) Handle(ctx context.Context,
	request harnessprotocol.HarnessInteractionRequest) (harnessprotocol.HarnessInteractionResponse, error) {
	providerRequest, ok := request.(*harnessprotocol.ProviderInteractionRequest)
	if !ok {
		return nil, errors.New("unexpected request type")
	}
	status := harnessprotocol.InteractionResponseStatusDeclined
	if r.approve {
		status = harnessprotocol.InteractionResponseStatusCompleted
	}
	return &harnessprotocol.ProviderInteractionResponse{
		RequestID: providerRequest.RequestID,
		Status:    status,
	}, nil
}

// Cancel 无操作。
func (r *ratifier) Cancel(ctx context.Context, requestID string,
	opts ...harnessprotocol.CancelOption) error {
	return nil
}

// TestAuthFallback_失败后切回退并重放 对齐 test_base.py:532-546。
func TestAuthFallback_失败后切回退并重放(t *testing.T) {
	harness := newFallbackHarness(t)
	harnessContext := testHarnessContext()
	harnessContext.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction] = true
	harnessContext.Interactions = &ratifier{approve: true}
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	// 重放后的终态应为 finished
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminalOf(t, events).Kind)
	// 回退已被激活
	assert.True(t, harness.FallbackActivated())
	assert.Equal(t, "fallback", harness.endpointSnapshot())
}

// TestAuthFallback_宿主拒绝后恢复主模型 对齐 test_base.py:548-557。
func TestAuthFallback_宿主拒绝后恢复主模型(t *testing.T) {
	harness := newFallbackHarness(t)
	harnessContext := testHarnessContext()
	harnessContext.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction] = true
	harnessContext.Interactions = &ratifier{approve: false}
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	// 宿主拒绝则保留原失败
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, terminalOf(t, events).Kind)
	assert.False(t, harness.FallbackActivated())
	assert.Equal(t, "primary", harness.endpointSnapshot())
}

// TestAuthFallback_切换失败每轮只试一次 对齐 test_base.py:559-571。
func TestAuthFallback_切换失败每轮只试一次(t *testing.T) {
	harness := newFallbackHarness(t)
	harness.reconnectErr = errors.New("cannot reconnect")
	harnessContext := testHarnessContext()
	harnessContext.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction] = true
	harnessContext.Interactions = &ratifier{approve: true}
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, terminalOf(t, events).Kind)
	// 只重连一次（失败后不再重试）
	assert.Len(t, harness.reconnectSnapshot(), 1)
}

// TestAuthFallback_记账失败仍重放 对齐 test_base.py:615-631。
func TestAuthFallback_记账失败仍重放(t *testing.T) {
	harness := newFallbackHarness(t)
	harness.bookkeepingErr = errors.New("bookkeeping failed")
	harnessContext := testHarnessContext()
	harnessContext.HostCapabilities[harnessprotocol.HostCapabilityProviderInteraction] = true
	harnessContext.Interactions = &ratifier{approve: true}
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, harnessContext))
	defer func() { _ = harness.Stop(context.Background()) }()

	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	// 记账失败不应让已批准的重放落空
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminalOf(t, events).Kind)
	assert.True(t, harness.FallbackActivated())
}

// TestAuthFallback_未配置回退时不激活 门卫 3（base.py:569-571）。
func TestAuthFallback_未配置回退时不激活(t *testing.T) {
	harness := newFallbackHarness(t)
	harness.payload = nil
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, terminalOf(t, events).Kind)
	assert.Empty(t, harness.reconnectSnapshot())
}

// TestAuthFallback_非auth类别不激活 门卫 1（base.py:565-566）。
func TestAuthFallback_非auth类别不激活(t *testing.T) {
	harness := newScriptedHarness(t)
	harness.setCrashNext(true)
	ctx := context.Background()

	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	events := collectTurn(t, harness, receipt.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindFailed, terminalOf(t, events).Kind)
}

// ──────────────────────────── 模型选择 ────────────────────────────

// selectableHarness 声明模型发现与选择能力。
// Python: test_base.py:332-361 _SelectableHarness
type selectableHarness struct {
	scriptedHarness

	// mu 保护 applyCalls（基座在监督协程写、测试主协程读）
	mu         sync.Mutex
	applyCalls []harnessprotocol.ModelSelection
	applyErr   error
}

// appliedCount 线程安全地读已应用次数。
func (s *selectableHarness) appliedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.applyCalls)
}

// firstApplied 线程安全地读第一次应用的选项。
func (s *selectableHarness) firstApplied() (harnessprotocol.ModelSelection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.applyCalls) == 0 {
		return harnessprotocol.ModelSelection{}, false
	}
	return s.applyCalls[0], true
}

// ListModels 返回固定目录。
func (s *selectableHarness) ListModels(ctx context.Context) ([]harnessprotocol.ModelOption, error) {
	return []harnessprotocol.ModelOption{{ModelID: "m1"}}, nil
}

// ApplyModelSelection 记录切换并可按脚本失败。
func (s *selectableHarness) ApplyModelSelection(ctx context.Context,
	selection harnessprotocol.ModelSelection) error {
	s.mu.Lock()
	s.applyCalls = append(s.applyCalls, selection)
	s.mu.Unlock()
	return s.applyErr
}

// newSelectableHarness 构造一个声明模型控制的基座。
func newSelectableHarness(t *testing.T) *selectableHarness {
	t.Helper()
	harness := &selectableHarness{}
	harness.scriptedHarness.release = make(chan struct{})
	harness.SerializedTurnHarness = NewSerializedTurnHarness(HarnessOptions{
		Card: harnessprotocol.HarnessCard{
			Name:                  "selectable",
			ImplementationVersion: "1.0",
			ProtocolVersion:       harnessprotocol.ProtocolVersion,
			CompatibleProtocolVersions: map[string]bool{
				harnessprotocol.ProtocolVersion: true,
			},
			Capabilities: map[harnessprotocol.HarnessCapability]bool{
				harnessprotocol.HarnessCapabilitySteer:          true,
				harnessprotocol.HarnessCapabilityModelSelection: true,
				harnessprotocol.HarnessCapabilityModelDiscovery: true,
			},
		},
		Hooks:               harness,
		EventBufferCapacity: 64,
	})
	return harness
}

// TestSetModel_空闲立即应用 对齐 test_base.py:381-393。
func TestSetModel_空闲立即应用(t *testing.T) {
	harness := newSelectableHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	model := "m2"
	require.NoError(t, harness.SetModel(ctx, harnessprotocol.ModelSelection{Model: &model}))
	assert.Equal(t, 1, harness.appliedCount())
	applied, ok := harness.firstApplied()
	require.True(t, ok)
	require.NotNil(t, applied.Model)
	assert.Equal(t, "m2", *applied.Model)
}

// TestSetModel_轮次中延后到下一轮前 对齐 test_base.py:395-413。
func TestSetModel_轮次中延后到下一轮前(t *testing.T) {
	harness := newSelectableHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	harness.block()
	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)

	// 轮次运行中切换：延后，不立即应用
	model := "m2"
	require.NoError(t, harness.SetModel(ctx, harnessprotocol.ModelSelection{Model: &model}))
	assert.Equal(t, 0, harness.appliedCount(), "轮次运行中应延后应用")

	// 再排一轮：延后的切换在该轮开始前应用（base.py:509-510）
	second, err := harness.Send(ctx, newTaskInput(t, "第二轮"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	harness.releaseChan()
	require.Eventually(t, func() bool {
		return harness.appliedCount() == 1
	}, 3*time.Second, 5*time.Millisecond, "延后的模型选择应在下一轮前应用")
	applied, ok := harness.firstApplied()
	require.True(t, ok)
	require.NotNil(t, applied.Model)
	assert.Equal(t, "m2", *applied.Model)

	events := collectTurn(t, harness, receipt.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminalOf(t, events).Kind)
	secondEvents := collectTurn(t, harness, second.TurnID)
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminalOf(t, secondEvents).Kind)
}

// TestSetModel_延后应用失败发WARNING诊断 对齐 test_base.py:434-453。
//
// 单消费者租约：只用一个周期级游标收集全部事件，从中筛终态与诊断。
func TestSetModel_延后应用失败发WARNING诊断(t *testing.T) {
	harness := newSelectableHarness(t)
	harness.applyErr = errors.New("switch failed")
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	cursor := harness.Events()
	// 收集协程与主协程共享状态，必须加锁（-race 会抓）
	var mu sync.Mutex
	var diagnostics []harnessprotocol.DiagnosticEvent
	terminals := map[string]harnessprotocol.TurnEventKind{}
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for {
			event, nextErr := cursor.Next(context.Background())
			if nextErr != nil {
				return
			}
			mu.Lock()
			if diagnostic, ok := event.Event.(harnessprotocol.DiagnosticEvent); ok {
				diagnostics = append(diagnostics, diagnostic)
			}
			if pointer, ok := event.Event.(*harnessprotocol.DiagnosticEvent); ok {
				diagnostics = append(diagnostics, *pointer)
			}
			if kind, ok := turnLifecycleKind(event.Event); ok &&
				harnessprotocol.IsTerminalTurnEventKind(kind) {
				terminals[event.TurnID] = kind
			}
			mu.Unlock()
		}
	}()

	harness.block()
	receipt, err := harness.Send(ctx, newTaskInput(t, "任务"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitForState(t, harness, harnessprotocol.HarnessStateRunning)

	model := "m2"
	require.NoError(t, harness.SetModel(ctx, harnessprotocol.ModelSelection{Model: &model}))
	second, err := harness.Send(ctx, newTaskInput(t, "第二轮"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	harness.releaseChan()

	// 两轮都完成
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(terminals) == 2
	}, 3*time.Second, 5*time.Millisecond, "两轮都应到达终态")

	mu.Lock()
	// 延后应用失败不应让轮次失败
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminals[receipt.TurnID])
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, terminals[second.TurnID])
	mu.Unlock()

	// 应发出 WARNING 诊断（base.py:625-632）
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(diagnostics) > 0
	}, 2*time.Second, 5*time.Millisecond, "应发出 WARNING 诊断")
	mu.Lock()
	assert.Equal(t, harnessprotocol.DiagnosticLevelWarning, diagnostics[0].Level)
	assert.Contains(t, diagnostics[0].Message, "could not switch model")
	mu.Unlock()

	_ = cursor.Close(context.Background())
	<-collected
}

// TestListModels_声明能力时返回目录 对齐 base.py:458-463。
func TestListModels_声明能力时返回目录(t *testing.T) {
	harness := newSelectableHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	models, err := harness.ListModels(ctx)
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "m1", models[0].ModelID)
}

// ──────────────────────────── 空缓冲与边界 ────────────────────────────

// TestSerializedTurnHarness_无缓冲时游标立即耗尽 Start 前取 Events/TurnEvents。
func TestSerializedTurnHarness_无缓冲时游标立即耗尽(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()

	for _, cursor := range []harnessprotocol.HarnessEventCursor{
		harness.Events(),
		harness.TurnEvents(nil),
	} {
		require.NotNil(t, cursor)
		_, err := cursor.Next(ctx)
		assert.ErrorIs(t, err, harnessprotocol.ErrEventStreamDone)
		assert.NoError(t, cursor.Close(ctx))
	}
}

// TestSerializedTurnHarness_Emit去重causationIDs 对齐 base.py:700-702。
//
// turn.message_id 已在 causes 首位，额外传入同值应被排除。
func TestSerializedTurnHarness_Emit去重causationIDs(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	// 先发一条 STARTED 选定轮次，再用 TurnEvents 只取该轮的事件
	turn := &PendingTurn{MessageID: "msg-1", TurnID: "turn-1"}
	started, err := harnessprotocol.NewTurnLifecycleEvent(harnessprotocol.TurnEventKindStarted, nil)
	require.NoError(t, err)
	require.NoError(t, harness.Emit(ctx, started, EmitOptions{Turn: turn}))
	cursor := harness.TurnEvents(&turn.TurnID)
	_, err = cursor.Next(ctx) // 消费 STARTED
	require.NoError(t, err)

	output, err := harnessprotocol.NewOutputEvent(
		"o1", harnessprotocol.OutputKindText, "x", harnessprotocol.OutputOperationDelta,
		harnessprotocol.OutputChannelAnswer, 0, "text", nil)
	require.NoError(t, err)
	require.NoError(t, harness.Emit(ctx, output, EmitOptions{
		Turn:         turn,
		CausationIDs: []string{"msg-1", "other-cause"},
	}))

	event, err := cursor.Next(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"msg-1", "other-cause"}, event.CausationIDs,
		"turn.MessageID 重复传入应被去重，只保留一次")
	assert.Equal(t, "msg-1", event.CorrelationID)
	assert.Equal(t, "turn-1", event.TurnID)
	_ = cursor.Close(ctx)
}

// TestSerializedTurnHarness_Transition同相位不发射 对齐 base.py:666-671。
func TestSerializedTurnHarness_Transition同相位不发射(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	// 已 IDLE，再转 IDLE 不应产生事件
	before := harness.sequence.Load()
	require.NoError(t, harness.transition(ctx, harnessprotocol.HarnessStateIdle))
	assert.Equal(t, before, harness.sequence.Load(), "同相位转移不应自增事件序号")
}

// TestAbort_未声明能力时报错 对齐 base.py:432-434。
func TestAbort_未声明能力时报错(t *testing.T) {
	harness := newScriptedHarness(t)
	ctx := context.Background()
	require.NoError(t, harness.Start(ctx, testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()

	// scriptedHarness 只声明 GRACEFUL_ABORT，未声明 FORCE_ABORT
	err := harness.Abort(ctx, harnessprotocol.AbortModeForce)
	var unsupported *harnessprotocol.UnsupportedHarnessCapabilityError
	require.ErrorAs(t, err, &unsupported)
}

// TestTurnTiming_DurationMs非负 单调耗时永不为负。
func TestTurnTiming_DurationMs非负(t *testing.T) {
	timing := NewTurnTiming()
	assert.True(t, timing.DurationMs() >= 0)
	assert.True(t, timing.CompletedAt() > 0)
	assert.True(t, timing.StartedAt > 0)
}

// ──────────────────────────── rollbackStart 与崩溃分支 ────────────────────────────

// failingOpenHarness OpenSession 失败、CloseSession 也失败的基座。
type failingOpenHarness struct {
	scriptedHarness
	openErr  error
	closeErr error
}

// OpenSession 恒失败。
func (f *failingOpenHarness) OpenSession(ctx context.Context,
	harnessContext harnessprotocol.HarnessContext) (*string, error) {
	f.opened++
	return nil, f.openErr
}

// CloseSession 恒失败（覆盖 rollback 的 debug 日志分支）。
func (f *failingOpenHarness) CloseSession(ctx context.Context) error {
	f.closed++
	return f.closeErr
}

// TestStart_开启会话失败时回滚 对齐 base.py:292-296 与 :300-307。
//
// 回滚会调 CloseSession（即使它也失败）、清空 eventBuffer/context/sessionID。
func TestStart_开启会话失败时回滚(t *testing.T) {
	harness := &failingOpenHarness{
		openErr:  errors.New("cannot open"),
		closeErr: errors.New("cannot close"),
	}
	harness.scriptedHarness.release = make(chan struct{})
	harness.SerializedTurnHarness = NewSerializedTurnHarness(HarnessOptions{
		Card: harnessprotocol.HarnessCard{
			Name:                  "failing",
			ImplementationVersion: "1.0",
			ProtocolVersion:       harnessprotocol.ProtocolVersion,
			CompatibleProtocolVersions: map[string]bool{
				harnessprotocol.ProtocolVersion: true,
			},
		},
		Hooks:               harness,
		EventBufferCapacity: 8,
	})

	err := harness.Start(context.Background(), testHarnessContext())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot open")
	// 回滚已执行
	assert.Equal(t, 1, harness.closedCount())
	assert.Nil(t, harness.eventBuffer)
	assert.Nil(t, harness.harnessContext)
	assert.Nil(t, harness.sessionID)
	assert.False(t, harness.cycleStarted)
}

// TestCrashResult_已请求中止时返回ABORTED 对齐 base.py:635-641。
//
// provider 在轮次中崩溃但该轮已被 abort：终态应为 ABORTED 而非 FAILED。
func TestCrashResult_已请求中止时返回ABORTED(t *testing.T) {
	harness := newScriptedHarness(t)

	t.Run("用户中止", func(t *testing.T) {
		turn := &PendingTurn{MessageID: "m", TurnID: "t"}
		turn.requestAbort(harnessprotocol.AbortModeGraceful)
		kind, result := harness.crashResult(turn, errors.New("boom"))
		assert.Equal(t, harnessprotocol.TurnEventKindAborted, kind)
		assert.Equal(t, harnessprotocol.TurnStatusInterrupted, result.Status)
		require.NotNil(t, result.Termination)
		assert.Equal(t, harnessprotocol.TurnTerminationKindUserAbort, result.Termination.Kind)
	})
	t.Run("harness 停止", func(t *testing.T) {
		turn := &PendingTurn{MessageID: "m", TurnID: "t"}
		turn.requestStop()
		kind, result := harness.crashResult(turn, errors.New("boom"))
		assert.Equal(t, harnessprotocol.TurnEventKindAborted, kind)
		require.NotNil(t, result.Termination)
		assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
	})
}

// TestInterruptedResult_两种停止原因 对齐 base.py:866-871。
func TestInterruptedResult_两种停止原因(t *testing.T) {
	timing := NewTurnTiming()

	t.Run("用户中止", func(t *testing.T) {
		turn := &PendingTurn{MessageID: "m", TurnID: "t"}
		turn.requestAbort(harnessprotocol.AbortModeGraceful)
		result := InterruptedResult(turn, InterruptedResultOptions{
			ProviderName: "p", Timing: timing})
		assert.Equal(t, harnessprotocol.TurnStatusInterrupted, result.Status)
		require.NotNil(t, result.Termination)
		assert.Equal(t, harnessprotocol.TurnTerminationKindUserAbort, result.Termination.Kind)
	})
	t.Run("harness 停止", func(t *testing.T) {
		turn := &PendingTurn{MessageID: "m", TurnID: "t"}
		turn.requestStop()
		result := InterruptedResult(turn, InterruptedResultOptions{
			ProviderName: "p", Timing: timing})
		require.NotNil(t, result.Termination)
		assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
	})
}

// TestBuildQueuedStopResult_字段 对齐 base.py:839-852。
func TestBuildQueuedStopResult_字段(t *testing.T) {
	result := BuildQueuedStopResult("scripted")
	assert.Equal(t, harnessprotocol.TurnStatusInterrupted, result.Status)
	require.NotNil(t, result.Termination)
	assert.Equal(t, harnessprotocol.TurnTerminationKindHarnessStop, result.Termination.Kind)
	assert.Contains(t, *result.Termination.Message, "stopped before the queued turn started")
	require.NotNil(t, result.DurationMS)
	assert.Equal(t, 0, *result.DurationMS)
}

// TestInteractionHelpers_各类型分支 interactionRequestID 与
// interactionResponseCompleted 覆盖全部 5 种请求与 4 种应答。
func TestInteractionHelpers_各类型分支(t *testing.T) {
	// 请求类型
	assert.Equal(t, "a", interactionRequestID(&harnessprotocol.ToolApprovalRequest{RequestID: "a"}))
	assert.Equal(t, "b", interactionRequestID(&harnessprotocol.UserInputRequest{RequestID: "b"}))
	assert.Equal(t, "c", interactionRequestID(&harnessprotocol.McpElicitationRequest{RequestID: "c"}))
	assert.Equal(t, "d", interactionRequestID(&harnessprotocol.DynamicToolCallRequest{RequestID: "d"}))
	assert.Equal(t, "e", interactionRequestID(&harnessprotocol.ProviderInteractionRequest{RequestID: "e"}))
	assert.Equal(t, "", interactionRequestID(nil))

	// 应答类型
	assert.True(t, interactionResponseCompleted(&harnessprotocol.UserInputResponse{
		Status: harnessprotocol.InteractionResponseStatusCompleted}))
	assert.True(t, interactionResponseCompleted(&harnessprotocol.McpElicitationResponse{
		Status: harnessprotocol.InteractionResponseStatusCompleted}))
	assert.True(t, interactionResponseCompleted(&harnessprotocol.DynamicToolCallResponse{
		Status: harnessprotocol.InteractionResponseStatusCompleted}))
	assert.True(t, interactionResponseCompleted(&harnessprotocol.ProviderInteractionResponse{
		Status: harnessprotocol.InteractionResponseStatusCompleted}))
	assert.False(t, interactionResponseCompleted(&harnessprotocol.ProviderInteractionResponse{
		Status: harnessprotocol.InteractionResponseStatusDeclined}))
	assert.False(t, interactionResponseCompleted(nil))

	// 工具审批应答没有 Status 字段，恒为 false（决策在 Decision 上）
	assert.False(t, interactionResponseCompleted(&harnessprotocol.ToolApprovalResponse{}))
}

// TestEmptyCursor_Iter与Close 空游标的两个方法。
func TestEmptyCursor_Iter与Close(t *testing.T) {
	cursor := &emptyCursor{}
	assert.Equal(t, cursor, cursor.Iter())
	assert.NoError(t, cursor.Close(context.Background()))
}

// TestSerializedTurnHarness_HasHostInteractions 宿主是否提供交互处理器。
func TestSerializedTurnHarness_HasHostInteractions(t *testing.T) {
	harness := newScriptedHarness(t)
	assert.False(t, harness.hasHostInteractions(), "未启动时无上下文")

	require.NoError(t, harness.Start(context.Background(), testHarnessContext()))
	defer func() { _ = harness.Stop(context.Background()) }()
	assert.False(t, harness.hasHostInteractions(), "上下文未带处理器")
}
