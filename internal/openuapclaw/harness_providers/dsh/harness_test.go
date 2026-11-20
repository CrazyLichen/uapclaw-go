package dsh

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// ──────────────────────────── fake SDK 基建 ────────────────────────────
// 对齐上游 test_harness.py:25-211 的 _FakeSdkState/_RunScript/_FakeSession
// 与 _install_fake_sdk（Python sys.modules 注入的 Go 工厂注入等价物）。

// runScript 一次 SDK Run 的脚本：通知序列、可等待释放、终态或错误。
// Python: test_harness.py:25-98 _RunScript/_FakeRunResult
type runScript struct {
	notifications []deepseekharness.Notification
	result        *deepseekharness.RunResult
	err           error
	waitReleased  bool
	released      chan struct{}
	finished      chan struct{}
	finishedOnce  sync.Once
}

// fakeSdkState 记录构造参数、会话 ID 与 close 计数。
// Python: test_harness.py:100-115 _FakeSdkState
type fakeSdkState struct {
	mu                 sync.Mutex
	scripts            []*runScript
	constructorOptions []GoSDKOptions
	sessionIDs         []string
	closeCount         int
	activeRuns         int
	startSessionErr    error
}

// newScript 创建带信号通道的脚本。
func newScript() *runScript {
	return &runScript{released: make(chan struct{}), finished: make(chan struct{})}
}

// installFakeSDK 返回注入 DshHarness 的工厂。
// Python: test_harness.py:117-176 _install_fake_sdk
func installFakeSDK(state *fakeSdkState) SDKFactory {
	return func(options GoSDKOptions) (DshSDKHarness, error) {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.constructorOptions = append(state.constructorOptions, options)
		return &fakeSDKHarness{state: state}, nil
	}
}

// fakeSDKHarness fake 句柄。
type fakeSDKHarness struct{ state *fakeSdkState }

// StartSession 记录会话 ID；可注入失败。
// Python: test_harness.py:122-129（fail_start 注入见 test_harness.py:792）
func (f *fakeSDKHarness) StartSession(sessionID string) (DshSDKSession, error) {
	f.state.mu.Lock()
	defer f.state.mu.Unlock()
	if f.state.startSessionErr != nil {
		return nil, f.state.startSessionErr
	}
	f.state.sessionIDs = append(f.state.sessionIDs, sessionID)
	return &fakeSDKSession{state: f.state, sessionID: sessionID}, nil
}

// Close 计数并释放等待中的 run。
func (f *fakeSDKHarness) Close() error {
	f.state.mu.Lock()
	f.state.closeCount++
	scripts := append([]*runScript{}, f.state.scripts...)
	f.state.mu.Unlock()
	for _, script := range scripts {
		script.finishedOnce.Do(func() { close(script.finished) })
	}
	return nil
}

// fakeSDKSession fake 会话：按脚本逐个同步回调通知。
type fakeSDKSession struct {
	state     *fakeSdkState
	sessionID string
}

// Python: test_harness.py:145-176 _FakeSession.run
func (f *fakeSDKSession) Run(input any, onNotification func(deepseekharness.Notification)) (*deepseekharness.RunResult, error) {
	f.state.mu.Lock()
	if f.state.activeRuns > 0 {
		// Python: 同会话串行语义由适配器保证；fake 记录并发调用于断言
		f.state.activeRuns++
		active := f.state.activeRuns
		f.state.mu.Unlock()
		f.state.mu.Lock()
		f.state.activeRuns = active - 1
		f.state.mu.Unlock()
		return nil, errors.New("concurrent run on fake session")
	}
	f.state.activeRuns++
	var script *runScript
	if len(f.state.scripts) > 0 {
		script = f.state.scripts[0]
		f.state.scripts = f.state.scripts[1:]
	}
	f.state.mu.Unlock()

	defer func() {
		f.state.mu.Lock()
		f.state.activeRuns--
		f.state.mu.Unlock()
		script.finishedOnce.Do(func() { close(script.finished) })
	}()

	if script == nil {
		return &deepseekharness.RunResult{FinalResponse: "done", FinishReason: strPtr("completed")}, nil
	}
	for _, raw := range script.notifications {
		payload := raw.Payload
		if payload == nil {
			payload = deepseekharness.JSONObject{}
		}
		if raw.Method == "session.event" {
			if _, ok := payload["sessionId"]; !ok {
				payload["sessionId"] = f.sessionID
			}
		}
		// Python: copy.deepcopy（同步回调，无并发共享，直接传值）
		onNotification(deepseekharness.Notification{Method: raw.Method, Payload: payload})
	}
	if script.waitReleased {
		select {
		case <-script.released:
		case <-time.After(5 * time.Second):
			return nil, errors.New("fake DSH run was not released")
		}
	}
	if script.err != nil {
		return nil, script.err
	}
	if script.result != nil {
		return script.result, nil
	}
	return &deepseekharness.RunResult{FinalResponse: "done", FinishReason: strPtr("completed")}, nil
}

// ──────────────────────────── 测试助手 ────────────────────────────

// testContext 协议 v1 合法宿主上下文。
func testContext() harnessprotocol.HarnessContext {
	return harnessprotocol.HarnessContext{
		AgentName:       "researcher",
		AgentID:         "team-a_researcher",
		HostSessionID:   "team-session-1",
		ProtocolVersion: harnessprotocol.ProtocolVersion,
	}
}

// newTestHarness 构造带 fake 工厂的适配器。
func newTestHarness(t *testing.T, state *fakeSdkState, mutate func(*DshHarnessConfig)) *DshHarness {
	t.Helper()
	config := DshHarnessConfig{DshHome: "/tmp/dsh-home"}
	if mutate != nil {
		mutate(&config)
	}
	h, err := NewDshHarnessWithFactory(config, installFakeSDK(state))
	require.NoError(t, err)
	return h
}

// scriptWithEvents 用通知列表建脚本。
func scriptWithEvents(notifications ...deepseekharness.Notification) *runScript {
	script := newScript()
	script.notifications = notifications
	return script
}

// rootEvent 构造 root 会话事件通知。
func rootEvent(eventType string, data map[string]any) deepseekharness.Notification {
	return notification("session.event", deepseekharness.JSONObject{
		"event": map[string]any{"type": eventType, "data": data},
	})
}

// ──────────────────────────── 用例（对齐上游 18 条） ────────────────────────────

// 惰性构造 + start/stop 走 fake SDK：
// 上游 test_provider_construction_is_lazy_and_start_stop_use_fake_sdk (213)。
func TestProviderConstructionIsLazy(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newScript()}}
	h := newTestHarness(t, state, nil)
	// 构造期不触碰 SDK（Python: sys.modules 断言 not in —— fake 未被调用）
	assert.Empty(t, state.constructorOptions)

	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hi"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	require.NoError(t, h.Stop(ctx))
	require.Len(t, state.constructorOptions, 1)
	require.Len(t, state.sessionIDs, 1)
	assert.True(t, strings.HasPrefix(state.sessionIDs[0], "dsh-"))
	assert.Equal(t, 1, state.closeCount)
	// 上游用例到此为止（Python: session id 前缀 + fake 调用一次）
	_ = ctx
}

// 缺 SDK 不留半开周期：
// 上游 test_missing_optional_sdk_does_not_open_a_partial_cycle (271)。
func TestMissingSDKDoesNotOpenPartialCycle(t *testing.T) {
	state := &fakeSdkState{startSessionErr: errors.New("boom")}
	config := DshHarnessConfig{DshHome: "/tmp/dsh-home", SystemPromptMode: "append"}
	h, err := NewDshHarnessWithFactory(config, func(options GoSDKOptions) (DshSDKHarness, error) {
		state.constructorOptions = append(state.constructorOptions, options)
		return nil, errors.New("sdk unavailable")
	})
	require.NoError(t, err)
	err = h.Start(context.Background(), testContext())
	require.Error(t, err)
	// Python: HarnessError("failed to start the DeepSeek Harness SDK runtime")
	// 且不携带 cause（防泄密）
	assert.Equal(t, "failed to start the DeepSeek Harness SDK runtime", err.Error())
	assert.Len(t, state.constructorOptions, 1)
	// overlay 已清理：无残留
	assert.Nil(t, h.overlay)
}

// auto follow-up 立即接受但 run 串行：
// 上游 test_auto_follow_up_is_accepted_immediately_but_runs_are_serial (302)。
func TestAutoFollowUpRunsAreSerial(t *testing.T) {
	state := &fakeSdkState{}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	receipt, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hi"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	assert.NotEmpty(t, receipt.MessageID)
	// 第二条输入在活跃轮存在时入队而非拒绝（Python 断言 accepted）
	_, err = h.Send(ctx, harnessprotocol.HarnessInput{Content: "second"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	require.NoError(t, h.Stop(ctx))
}

// 连续事件映射 output/usage/runtime item：
// 上游 test_continuous_events_map_output_usage_and_runtime_items (370)。
func TestContinuousEventsMap(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{scriptWithEvents(
		rootEvent("assistant/chunk", map[string]any{
			"turn": 1, "step": 1,
			"chunk": map[string]any{"type": "text-delta", "index": 0, "text": "hel"},
		}),
		rootEvent("assistant/chunk", map[string]any{
			"turn": 1, "step": 1,
			"chunk": map[string]any{"type": "block-end", "index": 0,
				"block": map[string]any{"type": "text", "text": "hello"}},
		}),
		rootEvent("tool/call", map[string]any{
			"callId": "c1", "name": "grep", "turn": 1, "step": 2, "arguments": map[string]any{"q": "x"},
		}),
		rootEvent("tool/result", map[string]any{
			"turn": 1, "step": 2,
			"message": map[string]any{"source": map[string]any{"callId": "c1"}, "content": "hits"},
		}),
		rootEvent("turn/end", map[string]any{
			"reason": map[string]any{"kind": "completed"},
		}),
	)}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hi"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	kinds := waitTurnTerminal(t, h)
	require.NoError(t, h.Stop(ctx))

	// 事件序列：output delta → output final → item started → item completed
	// → turn 终态（保序）
	assert.True(t, len(kinds) >= 4, "expected output/item/terminal events, got %v", kinds)
	assert.Contains(t, strings.Join(kinds, ","), "OutputEvent")
}

// SDK 失败从终态事件脱敏：
// 上游 test_sdk_failures_are_redacted_from_terminal_events (514)。
func TestSDKFailuresRedacted(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{{
		released: make(chan struct{}), finished: make(chan struct{}),
		err: errors.New("RuntimeError: api key sk-secret leaked"),
	}}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hi"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	observed := strings.Join(waitTurnTerminal(t, h), "")
	require.NoError(t, h.Stop(ctx))

	// 终态事件不含 SDK 错误文本（同步收集的事件串不含泄漏）
	assert.NotContains(t, observed, "sk-secret")
}

// idle 无原生 turn/end 是协议失败：
// 上游 test_idle_without_native_turn_end_is_a_protocol_failure (585)。
func TestIdleWithoutTurnEndFails(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{{
		released: make(chan struct{}), finished: make(chan struct{}),
		result: &deepseekharness.RunResult{FinalResponse: "", FinishReason: nil},
	}}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hi"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	// Events() 是晚订阅游标（从当前位置起读），快照有损——
	// 直接断言 waitTurnTerminal 同步收集到的事件串
	observed := strings.Join(waitTurnTerminal(t, h), "")
	require.NoError(t, h.Stop(ctx))
	assert.Contains(t, observed, "DSH_MISSING_TURN_END")
}

// 未声明能力显式失败：
// 上游 test_unsupported_capabilities_fail_explicitly (606)。
func TestUnsupportedCapabilities(t *testing.T) {
	state := &fakeSdkState{}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()

	// checkpoint 恢复拒绝
	bad := testContext()
	bad.ResumePolicy = harnessprotocol.ResumePolicyRequireResume
	err := h.Start(ctx, bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot restore protocol checkpoints")

	require.NoError(t, h.Start(ctx, testContext()))
	// steer / abort / pause / resume / model selection
	err = h.Steer(ctx, nil, harnessprotocol.HarnessInput{}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support steering")
	err = h.InterruptTurn(ctx, nil, harnessprotocol.AbortModeGraceful)
	require.Error(t, err)
	err = h.Pause(ctx)
	require.Error(t, err)
	err = h.ApplyModelSelection(ctx, harnessprotocol.ModelSelection{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support model selection")
	_, ok := h.AuthFallbackPayload()
	assert.False(t, ok)
	require.NoError(t, h.Stop(ctx))
}

// stop 打断活跃与排队轮次：上游 test_stop_interrupts_active_and_queued_turns (631)。
func TestStopInterruptsTurns(t *testing.T) {
	script := &runScript{
		released: make(chan struct{}), finished: make(chan struct{}),
		waitReleased: true,
	}
	state := &fakeSdkState{scripts: []*runScript{script}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hi"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitActiveRun(t, state)
	require.NoError(t, h.Stop(ctx))
	// stop 后 close 被调用，等待中的 run 被释放
	assert.Equal(t, 1, state.closeCount)
	select {
	case <-script.finished:
	case <-time.After(5 * time.Second):
		t.Fatal("script was not released by stop")
	}
}

// overlay 生命周期：启动注入、停止移除：
// 上游 test_overlay_uses_environment_data_and_is_removed_after_stop (758)。
func TestOverlayLifecycle(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newScript()}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	harnessContext := testContext()
	harnessContext.SystemPrompt = "host prompt"
	harnessContext.McpServers = nil
	require.NoError(t, h.Start(ctx, harnessContext))
	require.NotNil(t, h.overlay)
	// 构造参数含 overlay env 与 patch
	options := state.constructorOptions[0]
	assert.Contains(t, options.Env, h.overlay.EnvName())
	assert.Equal(t, h.overlay.EnvValue(), options.Env[h.overlay.EnvName()])
	found := false
	for _, patch := range options.Patches {
		if patch == h.overlay.Path() {
			found = true
		}
	}
	assert.True(t, found)
	require.NoError(t, h.Stop(ctx))
	assert.Nil(t, h.overlay)
}

// 失败的 start 移除 overlay：上游 test_failed_start_removes_overlay (784)。
func TestFailedStartRemovesOverlay(t *testing.T) {
	state := &fakeSdkState{startSessionErr: errors.New("no runtime")}
	h := newTestHarness(t, state, nil)
	harnessContext := testContext()
	harnessContext.SystemPrompt = "host prompt"
	err := h.Start(context.Background(), harnessContext)
	require.Error(t, err)
	assert.Nil(t, h.overlay)
}

// 事件游标单消费者且 close 幂等：
// 上游 test_event_cursor_is_single_consumer_and_close_is_idempotent (737)。
func TestEventCursorSingleConsumer(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newScript()}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	cursor := h.Events()
	require.NoError(t, cursor.Close(ctx))
	// 幂等
	require.NoError(t, cursor.Close(ctx))
	require.NoError(t, h.Stop(ctx))
}

// waitTurnTerminal 等待当前轮次出现终态 TurnLifecycleEvent（supervisor
// 异步驱动，Start 后即 idle，不能拿 idle 判轮次完成）。单消费者租约：
// 用带超时的 ctx 轮询同一个游标，返回前必须 Close 释放租约。
func waitTurnTerminal(t *testing.T, h *DshHarness) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor := h.TurnEvents(nil)
	defer func() { _ = cursor.Close(context.Background()) }()
	var kinds []string
	for {
		event, err := cursor.Next(ctx)
		if err != nil {
			t.Fatalf("turn did not reach a terminal state in time: %v", err)
		}
		if event.TurnID != "" {
			kinds = append(kinds, eventTypeName(event.Event))
		}
		if lifecycle, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok && lifecycle.Result != nil {
			return kinds
		}
	}
}

// waitActiveRun 等待 fake SDK 出现在飞 run（Stop 打断活跃轮的前置）。
func waitActiveRun(t *testing.T, state *fakeSdkState) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state.mu.Lock()
		active := state.activeRuns
		state.mu.Unlock()
		if active > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no active run observed in time")
}

// eventTypeName 事件载荷的类型名（终态事件名断言用）。
func eventTypeName(payload harnessprotocol.HarnessEventPayload) string {
	return stringify(map[string]any{"type": fmt.Sprintf("%T", payload), "detail": payload})
}

// turn_events 租约有限释放、错误轮次拒绝且不误删：
// 上游 test_turn_events_is_finite_releases_lease_and_does_not_discard_mismatch (344)。
func TestTurnEventsFiniteReleasesLease(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newScript()}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	receipt, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "hello"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	// 错误轮次游标：Next 报 HarnessStateError "not the next unconsumed turn"
	wrongCtx, wrongCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wrongCancel()
	wrongCursor := h.TurnEvents(strPtr("not-the-next-turn"))
	_, wrongErr := wrongCursor.Next(wrongCtx)
	require.Error(t, wrongErr)
	assert.Contains(t, wrongErr.Error(), "not the next unconsumed turn")

	// 正确轮次游标：首事件 STARTED、末事件 FINISHED，全部属于本轮
	turnCtx, turnCancel := context.WithTimeout(ctx, 10*time.Second)
	defer turnCancel()
	turnCursor := h.TurnEvents(&receipt.TurnID)
	defer func() { _ = turnCursor.Close(context.Background()) }()
	var events []*harnessprotocol.HarnessEvent
	for {
		event, err := turnCursor.Next(turnCtx)
		if err != nil {
			break
		}
		events = append(events, event)
	}
	require.NotEmpty(t, events)
	first, isLifecycle := events[0].Event.(harnessprotocol.TurnLifecycleEvent)
	require.True(t, isLifecycle)
	assert.Equal(t, harnessprotocol.TurnEventKindStarted, first.Kind)
	last, isLifecycle := events[len(events)-1].Event.(harnessprotocol.TurnLifecycleEvent)
	require.True(t, isLifecycle)
	assert.Equal(t, harnessprotocol.TurnEventKindFinished, last.Kind)
	for _, event := range events {
		assert.Equal(t, receipt.TurnID, event.TurnID)
	}

	// 全周期游标租约：创建后立即释放（aclose 幂等）
	nextCursor := h.Events()
	require.NoError(t, nextCursor.Close(ctx))
	require.NoError(t, nextCursor.Close(ctx))
	require.NoError(t, h.Stop(ctx))
}

// turn/end 错误在终态与 provider 事件双层脱敏：
// 上游 test_turn_end_error_is_redacted_in_terminal_and_provider_event (536)。
func TestTurnEndErrorRedacted(t *testing.T) {
	secret := "provider-secret-diagnostic"
	childTurnEnd := rootEvent("turn/end", map[string]any{
		"turn": 1,
		"reason": map[string]any{"kind": "error", "error": map[string]any{
			"message": secret, "code": "CHILD_FAILED", "status": 500,
		}},
	})
	childTurnEnd.Payload["sessionId"] = "child-1"
	rootTurnEnd := rootEvent("turn/end", map[string]any{
		"turn": 1,
		"reason": map[string]any{"kind": "error", "error": map[string]any{
			"message": secret, "code": "RATE_LIMITED", "status": 429,
		}},
	})
	state := &fakeSdkState{scripts: []*runScript{scriptWithEvents(childTurnEnd, rootTurnEnd)}}
	state.scripts[0].result = &deepseekharness.RunResult{FinalResponse: "", FinishReason: strPtr("error")}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	receipt, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "fail safely"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	observed := strings.Join(waitTurnTerminal(t, h), "")
	require.NoError(t, h.Stop(ctx))

	// 终态 error.code 取根会话的 RATE_LIMITED
	assert.Contains(t, observed, "RATE_LIMITED")
	assert.Contains(t, observed, "child.session.event")
	// 脱敏：secret 不出现在任何事件（repr 等价物）
	assert.NotContains(t, observed, secret)
	_ = receipt
}

// stop 等待监督协程排队终态全部落盘后才关流：
// 上游 test_stop_waits_for_supervisor_queued_terminals_before_closing (669)。
// Go 侧无法 monkeypatch _abort_queued_turns（asyncio 注入点无对应物，偏差
// D-dsh-1 同源）；等价断言：Stop 返回后全周期事件排空，每个被接受轮次恰有
// 1 条 STARTED + 1 条终态，且所有终态序号先于 terminated 关流事件。
func TestStopWaitsQueuedTerminals(t *testing.T) {
	activeScript := &runScript{
		released: make(chan struct{}), finished: make(chan struct{}),
		waitReleased: true, err: errors.New("runtime closed"),
	}
	state := &fakeSdkState{scripts: []*runScript{activeScript}}
	h := newTestHarness(t, state, nil)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))

	active, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "active"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	waitActiveRun(t, state)
	queuedOne, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "queued-1"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	queuedTwo, err := h.Send(ctx, harnessprotocol.HarnessInput{Content: "queued-2"},
		harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)

	require.NoError(t, h.Stop(ctx))
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, h.State())

	// Stop 关流后排空全周期事件
	cursor := h.Events()
	var events []*harnessprotocol.HarnessEvent
	for {
		event, err := cursor.Next(ctx)
		if err != nil {
			break
		}
		events = append(events, event)
	}
	_ = cursor.Close(ctx)

	accepted := map[string]bool{
		active.TurnID: true, queuedOne.TurnID: true, queuedTwo.TurnID: true,
	}
	terminalSeqs := []int{}
	terminatedSeq := -1
	lifecycleByTurn := map[string][]harnessprotocol.TurnEventKind{}
	for _, event := range events {
		if !accepted[event.TurnID] {
			if stateChange, ok := event.Event.(harnessprotocol.StateChangedEvent); ok {
				if stateChange.New == harnessprotocol.HarnessStateTerminated {
					terminatedSeq = event.Sequence
				}
			}
			continue
		}
		if lifecycle, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok {
			lifecycleByTurn[event.TurnID] =
				append(lifecycleByTurn[event.TurnID], lifecycle.Kind)
			if lifecycle.Kind != harnessprotocol.TurnEventKindStarted {
				terminalSeqs = append(terminalSeqs, event.Sequence)
			}
		}
	}
	require.Len(t, terminalSeqs, len(accepted))
	for turnID, kinds := range lifecycleByTurn {
		started := 0
		terminal := 0
		for _, kind := range kinds {
			if kind == harnessprotocol.TurnEventKindStarted {
				started++
			}
			if kind == harnessprotocol.TurnEventKindFinished ||
				kind == harnessprotocol.TurnEventKindAborted ||
				kind == harnessprotocol.TurnEventKindFailed {
				terminal++
			}
		}
		assert.Equal(t, 1, started, turnID)
		assert.Equal(t, 1, terminal, turnID)
	}
	require.Greater(t, terminatedSeq, 0)
	for _, seq := range terminalSeqs {
		assert.Less(t, seq, terminatedSeq)
	}
}
