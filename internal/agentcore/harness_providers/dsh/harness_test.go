package dsh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"testing"
	"time"

	deepseekharness "github.com/theoneLee/deepseek-harness-sdk-go"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── fake SDK（Python test_harness.py:25-211 一比一）───

// runScript 一次 SDK Run 的脚本：注入通知序列、可等待释放、最终结果或错误。
// Python: test_harness.py:25-70 _RunScript/_FakeRunResult
type runScript struct {
	notifications []deepseekharness.Notification
	result        *deepseekharness.RunResult
	err           error
	waitReleased  bool
	released      chan struct{}
	finished      chan struct{}
}

func newRunScript() *runScript {
	return &runScript{
		result:   &deepseekharness.RunResult{FinalResponse: "done", FinishReason: strPtr("completed")},
		released: make(chan struct{}),
		finished: make(chan struct{}),
	}
}

// fakeSdkState 记录构造参数、会话 ID 与 close 计数。
// Python: test_harness.py:100-115 _FakeSdkState
type fakeSdkState struct {
	mu                 sync.Mutex
	scripts            []*runScript
	constructorOptions []dshSDKOptions
	sessionIDs         []string
	closeCount         int
	activeRuns         int
}

// fakeSDKHarness Python test_harness.py:118-152 FakeDeepSeekHarness/_FakeSession
type fakeSDKHarness struct {
	state   *fakeSdkState
	session *fakeSDKSession
}

// fakeSDKSession _FakeSession：按脚本回放通知并同步回调。
type fakeSDKSession struct {
	state     *fakeSdkState
	sessionID string
}

// installFakeSDK 返回注入用工厂（Python sys.modules 注入的 Go 等价）。
func installFakeSDK(state *fakeSdkState) SDKFactory {
	return func(options dshSDKOptions) (dshSDKHarness, error) {
		state.mu.Lock()
		state.constructorOptions = append(state.constructorOptions, options)
		state.mu.Unlock()
		return &fakeSDKHarness{state: state}, nil
	}
}

// failingFactory 恒失败工厂（Python start_session 失败注入等价）。
func failingFactory(err error) SDKFactory {
	return func(options dshSDKOptions) (dshSDKHarness, error) { return nil, err }
}

// StartSession Python test_harness.py:125-129
func (f *fakeSDKHarness) StartSession(sessionID string) (dshSDKSession, error) {
	f.state.mu.Lock()
	f.state.sessionIDs = append(f.state.sessionIDs, sessionID)
	f.state.mu.Unlock()
	session := &fakeSDKSession{state: f.state, sessionID: sessionID}
	f.session = session
	return session, nil
}

// Close Python test_harness.py:130-134
func (f *fakeSDKHarness) Close() error {
	f.state.mu.Lock()
	f.state.closeCount++
	scripts := append([]*runScript{}, f.state.scripts...)
	f.state.mu.Unlock()
	for _, script := range scripts {
		script.releasedOnce()
	}
	return nil
}

// releasedOnce 幂等释放。
func (s *runScript) releasedOnce() {
	select {
	case <-s.released:
	default:
		close(s.released)
	}
}

// Run Python test_harness.py:145-176 _FakeSession.run：逐个深拷贝通知、补 sessionId、
// 同步回调、按需等释放、结束计数。
func (s *fakeSDKSession) Run(
	input any, onNotification func(deepseekharness.Notification),
) (*deepseekharness.RunResult, error) {
	s.state.mu.Lock()
	var script *runScript
	if len(s.state.scripts) > 0 {
		script = s.state.scripts[0]
		s.state.scripts = s.state.scripts[1:]
		s.state.activeRuns++
	}
	s.state.mu.Unlock()
	if script == nil {
		return nil, fmt.Errorf("fake DSH run script queue exhausted")
	}
	defer func() {
		s.state.mu.Lock()
		s.state.activeRuns--
		s.state.mu.Unlock()
		close(script.finished)
	}()

	// Python: test_harness.py:150-161 逐条通知回调（payload 补 sessionId）
	for _, raw := range script.notifications {
		payload := map[string]any{}
		for key, value := range raw.Payload {
			payload[key] = value
		}
		if raw.Method == "session.event" {
			if _, ok := payload["sessionId"]; !ok {
				payload["sessionId"] = s.sessionID
			}
		}
		onNotification(deepseekharness.Notification{Method: raw.Method, Payload: payload})
	}
	if script.waitReleased {
		select {
		case <-script.released:
		case <-time.After(5 * time.Second):
			return nil, fmt.Errorf("fake DSH run was not released")
		}
	}
	if script.err != nil {
		return nil, script.err
	}
	return script.result, nil
}

// ──────────────────────────── 测试基建 ────────────────────────────

// newTestHarness 构造挂 fake 工厂的 harness。
func newTestHarness(t *testing.T, state *fakeSdkState) *DshHarness {
	t.Helper()
	h, err := NewDshHarness(DefaultDshHarnessConfig())
	require.NoError(t, err)
	h.sdkFactory = installFakeSDK(state)
	return h
}

// testContext 构造最小合法上下文。
// Python: test_harness.py:141-167 _context
func testContext() harnessprotocol.HarnessContext {
	testCtx, err := harnessprotocol.NewHarnessContext("researcher", "team-a_researcher",
		"team-session-1", "", harnessprotocol.ContextOptions{})
	if err != nil {
		panic(err)
	}
	return testCtx
}

// testInput 构造文本输入。
func testInput(t *testing.T, text string) harnessprotocol.HarnessInput {
	input, err := harnessprotocol.NewHarnessInput(text, nil)
	require.NoError(t, err)
	return input
}

// rootEvent 构造根会话事件通知。
func rootEvent(eventType string, data map[string]any) deepseekharness.Notification {
	return notification("session.event", map[string]any{"event": map[string]any{
		"type": eventType, "data": data,
	}})
}

// collectEvents 消费事件游标直到 _END 哨兵；游标未关闭时以 2s 超时兜底
// （对齐上游：turn 终态后 Stop 关闭事件流，drain 在 Stop 后进行或超时截断）。
func collectEvents(t *testing.T, cursor harnessprotocol.HarnessEventCursor) []harnessprotocol.HarnessEvent {
	t.Helper()
	// BoundedEventBuffer.Get 不响应 ctx 取消（基座实现注释「保留位」），
	// 泵在 goroutine 中执行，主协程按哨兵或 2s 超时收割
	type drainResult struct {
		events []harnessprotocol.HarnessEvent
		err    error
	}
	done := make(chan drainResult, 1)
	go func() {
		var events []harnessprotocol.HarnessEvent
		for {
			event, err := cursor.Next(context.Background())
			if err != nil {
				done <- drainResult{events, err}
				return
			}
			if event == nil {
				done <- drainResult{events, nil}
				return
			}
			events = append(events, *event)
		}
	}()
	select {
	case result := <-done:
		_ = result.err // exhausted/截断由调用方按断言覆盖
		return result.events
	case <-time.After(2 * time.Second):
		t.Fatalf("cursor drain timed out")
		return nil
	}
}

// waitForState 轮询等待状态。
func waitForState(t *testing.T, h *DshHarness, want harnessprotocol.HarnessState) {
	t.Helper()
	require.Eventually(t, func() bool { return h.State() == want },
		5*time.Second, 10*time.Millisecond)
}

// sendAwait 发送输入并泵事件直到终态轮次事件（finished/aborted/failed）或超时。
// 返回泵到的事件（游标租约已消费，调用方据此断言；随后应 Stop 收尾）。
func sendAwait(t *testing.T, h *DshHarness, ctx context.Context,
	input harnessprotocol.HarnessInput) []harnessprotocol.HarnessEvent {
	t.Helper()
	cursor := h.Events()
	receipt, err := h.Send(ctx, input, harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	require.NotNil(t, receipt.MessageID)

	type pumpOut struct {
		events   []harnessprotocol.HarnessEvent
		terminal bool
	}
	done := make(chan pumpOut, 1)
	go func() {
		var events []harnessprotocol.HarnessEvent
		terminal := false
		for !terminal {
			event, err := cursor.Next(ctx)
			if err != nil || event == nil {
				done <- pumpOut{events, terminal}
				return
			}
			events = append(events, *event)
			if lifecycle, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok &&
				harnessprotocol.IsTerminalTurnEventKind(lifecycle.Kind) {
				terminal = true
			}
		}
		done <- pumpOut{events, true}
	}()
	select {
	case out := <-done:
		require.True(t, out.terminal, "terminal turn event not observed")
		return out.events
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for terminal turn event")
		return nil
	}
}

// assertTerminalResult 从事件中取终态结果（无则返回 nil）。
func assertTerminalResult(events []harnessprotocol.HarnessEvent) *harnessprotocol.TurnResult {
	for _, event := range events {
		if lifecycle, ok := event.Event.(harnessprotocol.TurnLifecycleEvent); ok &&
			lifecycle.Result != nil &&
			harnessprotocol.IsTerminalTurnEventKind(lifecycle.Kind) {
			return lifecycle.Result
		}
	}
	return nil
}

// ──────────────────────────── 用例 ────────────────────────────

// Python: test_harness.py:213-269 test_provider_construction_is_lazy_and_start_stop_use_fake_sdk
func TestProviderConstructionIsLazy(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newRunScript()}}
	h := newTestHarness(t, state)
	ctx := context.Background()

	// 构造即懒：未 Start 不触 SDK
	assert.Empty(t, state.constructorOptions)
	require.NoError(t, h.Start(ctx, testContext()))
	require.NoError(t, h.Stop(ctx))
	state.mu.Lock()
	closeCount, sessionCount := state.closeCount, len(state.sessionIDs)
	state.mu.Unlock()
	assert.Equal(t, 1, closeCount)
	assert.Equal(t, 1, sessionCount)
}

// Python: test_harness.py:271-300 test_missing_optional_sdk_does_not_open_a_partial_cycle
func TestMissingSDKDoesNotOpenPartialCycle(t *testing.T) {
	h, err := NewDshHarness(DefaultDshHarnessConfig())
	require.NoError(t, err)
	h.sdkFactory = failingFactory(fmt.Errorf("sdk unavailable"))
	err = h.Start(context.Background(), testContext())
	require.Error(t, err)
	assert.Equal(t, "sdk unavailable", err.Error())
	// 未留半开周期：状态未进入 IDLE
	assert.NotEqual(t, harnessprotocol.HarnessStateIdle, h.State())
}

// Python: test_harness.py:302-342 test_auto_follow_up_is_accepted_immediately_but_runs_are_serial
func TestAutoFollowUpRunsAreSerial(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newRunScript(), newRunScript()}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))

	receipt, err := h.Send(ctx, testInput(t, "one"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	require.NotNil(t, receipt.MessageID)
	// 立即再发（auto follow-up 立即接受）
	receipt2, err := h.Send(ctx, testInput(t, "two"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	require.NotNil(t, receipt2.MessageID)

	waitForState(t, h, harnessprotocol.HarnessStateIdle)
	// 两轮都被执行且串行
	state.mu.Lock()
	activeRuns := state.activeRuns
	state.mu.Unlock()
	assert.Equal(t, 0, activeRuns)
}

// Python: test_harness.py:344-368 test_turn_events_is_finite_releases_lease_and_does_not_discard_mismatch
func TestTurnEventsFiniteReleasesLease(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newRunScript()}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	events := sendAwait(t, h, ctx, testInput(t, "go"))
	require.NoError(t, h.Stop(ctx))
	assert.NotEmpty(t, events)
}

// Python: test_harness.py:370-512 test_continuous_events_map_output_usage_and_runtime_items
func TestContinuousEventsMap(t *testing.T) {
	script := newRunScript()
	script.result = &deepseekharness.RunResult{FinalResponse: "final answer", FinishReason: strPtr("completed")}
	script.notifications = []deepseekharness.Notification{
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
			"turn": 1, "step": 1, "callId": "c-1", "name": "read_file",
			"arguments": map[string]any{"path": "x"},
		}),
		rootEvent("tool/result", map[string]any{
			"turn": 1, "step": 1,
			"message": map[string]any{
				"source": map[string]any{"callId": "c-1"}, "content": "body",
			},
		}),
		rootEvent("assistant/chunk", map[string]any{
			"turn": 1, "step": 1,
			"chunk": map[string]any{"type": "usage", "usage": map[string]any{
				"inputTokens": 10, "outputTokens": 5,
			}},
		}),
		rootEvent("turn/end", map[string]any{
			"reason": map[string]any{"kind": "completed"},
		}),
	}
	state := &fakeSdkState{scripts: []*runScript{script}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	events := sendAwait(t, h, ctx, testInput(t, "go"))
	var outputDeltas, outputFinals, itemStarted, itemCompleted, usageEvents, providerEvents int
	for _, event := range events {
		switch payload := event.Event.(type) {
		case harnessprotocol.OutputEvent:
			if payload.Operation == harnessprotocol.OutputOperationDelta {
				outputDeltas++
			}
			if payload.Operation == harnessprotocol.OutputOperationFinal {
				outputFinals++
			}
		case harnessprotocol.ItemLifecycleEvent:
			if payload.Kind == harnessprotocol.ItemEventKindStarted {
				itemStarted++
			}
			if payload.Kind == harnessprotocol.ItemEventKindCompleted {
				itemCompleted++
			}
		case harnessprotocol.UsageUpdatedEvent:
			usageEvents++
			assert.Equal(t, 15, *payload.Usage.TotalTokens)
		case harnessprotocol.ProviderEvent:
			providerEvents++
			assert.Equal(t, "turn/end", payload.EventType)
		}
	}
	assert.Equal(t, 1, outputDeltas)
	assert.Equal(t, 1, outputFinals)
	assert.Equal(t, 1, itemStarted)
	assert.Equal(t, 1, itemCompleted)
	assert.Equal(t, 1, usageEvents)
	assert.Equal(t, 1, providerEvents)
}

// Python: test_harness.py:514-534 test_sdk_failures_are_redacted_from_terminal_events
func TestSDKFailuresRedacted(t *testing.T) {
	script := newRunScript()
	script.err = fmt.Errorf("SECRET stderr tail with api key")
	state := &fakeSdkState{scripts: []*runScript{script}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	events := sendAwait(t, h, ctx, testInput(t, "go"))
	require.NoError(t, h.Stop(ctx))

	for _, event := range events {
		b, _ := jsonMarshal(event)
		assert.NotContains(t, string(b), "SECRET")
	}
}

// Python: test_harness.py:536-583 test_turn_end_error_is_redacted_in_terminal_and_provider_event
func TestTurnEndErrorRedacted(t *testing.T) {
	script := newRunScript()
	script.result = &deepseekharness.RunResult{FinalResponse: "", FinishReason: strPtr("error")}
	script.notifications = []deepseekharness.Notification{
		rootEvent("turn/end", map[string]any{
			"reason": map[string]any{
				"kind": "error",
				"error": map[string]any{
					"code": "E1", "message": "SECRET diagnostics", "retryable": false,
				},
			},
		}),
	}
	state := &fakeSdkState{scripts: []*runScript{script}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	events := sendAwait(t, h, ctx, testInput(t, "go"))
	require.NoError(t, h.Stop(ctx))

	var sawCode bool
	for _, event := range events {
		b, _ := jsonMarshal(event)
		assert.NotContains(t, string(b), "SECRET diagnostics")
		if _, ok := event.Event.(harnessprotocol.ProviderEvent); ok {
			sawCode = true
		}
	}
	assert.True(t, sawCode)
}

// Python: test_harness.py:585-604 test_idle_without_native_turn_end_is_a_protocol_failure
func TestIdleWithoutTurnEndFails(t *testing.T) {
	script := newRunScript()
	script.result = &deepseekharness.RunResult{FinalResponse: "text", FinishReason: nil}
	state := &fakeSdkState{scripts: []*runScript{script}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	events := sendAwait(t, h, ctx, testInput(t, "go"))
	require.NoError(t, h.Stop(ctx))

	result := assertTerminalResult(events)
	require.NotNil(t, result)
	require.NotNil(t, result.Error)
	require.NotNil(t, result.Error.Code)
	assert.Equal(t, "DSH_MISSING_TURN_END", *result.Error.Code)
	// final_response 非串回落 last_text_output
	assert.Equal(t, "text", result.FinalOutput)
}

// Python: test_harness.py:606-629 test_unsupported_capabilities_fail_explicitly
func TestUnsupportedCapabilities(t *testing.T) {
	h, err := NewDshHarness(DefaultDshHarnessConfig())
	require.NoError(t, err)
	ctx := context.Background()
	// checkpoint 恢复拒绝
	requireCtx := testContext()
	requireCtx.ResumePolicy = harnessprotocol.ResumePolicyRequireResume
	err = h.ValidateContext(ctx, requireCtx)
	require.Error(t, err)
	assert.Equal(t, "the DSH SDK server cannot restore protocol checkpoints", err.Error())
	// in-process MCP 拒绝
	inprocCtx := testContext()
	inprocCtx.McpServers = []harnessprotocol.McpServerConfig{{
		Name: "inproc", Transport: harnessprotocol.McpTransportInProcess,
	}}
	err = h.ValidateContext(ctx, inprocCtx)
	require.Error(t, err)
	assert.Equal(t, "DSH cannot mount in-process MCP servers", err.Error())
}

// Python: test_harness.py:631-667 test_stop_interrupts_active_and_queued_turns
func TestStopInterruptsTurns(t *testing.T) {
	script := newRunScript()
	script.waitReleased = true
	script.err = fmt.Errorf("runtime torn down")
	state := &fakeSdkState{scripts: []*runScript{script, newRunScript()}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, testInput(t, "active"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	// Stop 触发 Close → 释放活跃 Run → stop_requested 分支
	require.NoError(t, h.Stop(ctx))
	// 活跃轮次以停止收尾：终态为 TERMINATED（base doStop 步骤 6）
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, h.State())
	state.mu.Lock()
	closeCount := state.closeCount
	state.mu.Unlock()
	assert.Equal(t, 1, closeCount)
}

// Python: test_harness.py:669-735 test_stop_waits_for_supervisor_queued_terminals_before_closing
func TestStopWaitsQueuedTerminals(t *testing.T) {
	script := newRunScript()
	script.waitReleased = true
	state := &fakeSdkState{scripts: []*runScript{script}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	_, err := h.Send(ctx, testInput(t, "go"), harnessprotocol.DeliveryModeAuto)
	require.NoError(t, err)
	require.NoError(t, h.Stop(ctx))
	// Stop 返回后 close 恰一次
	state.mu.Lock()
	closeCount := state.closeCount
	state.mu.Unlock()
	assert.Equal(t, 1, closeCount)
}

// Python: test_harness.py:737-756 test_event_cursor_is_single_consumer_and_close_is_idempotent
func TestEventCursorSingleConsumer(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newRunScript()}}
	h := newTestHarness(t, state)
	ctx := context.Background()
	require.NoError(t, h.Start(ctx, testContext()))
	events := sendAwait(t, h, ctx, testInput(t, "go"))
	require.NoError(t, h.Stop(ctx))
	assert.NotEmpty(t, events)

	// 游标租约单消费者：再取游标立刻耗尽
	exhausted := h.Events()
	event, err := exhausted.Next(ctx)
	if err == nil {
		assert.Nil(t, event)
	} else {
		// 已耗尽流返回哨兵错误
		assert.Equal(t, harnessprotocol.ErrEventStreamDone, err)
	}
	require.NoError(t, h.Stop(ctx))
}

// Python: test_harness.py:758-774 test_overlay_uses_environment_data_and_is_removed_after_stop
func TestOverlayLifecycle(t *testing.T) {
	state := &fakeSdkState{scripts: []*runScript{newRunScript()}}
	config := DefaultDshHarnessConfig()
	h, err := NewDshHarness(config)
	require.NoError(t, err)
	h.sdkFactory = installFakeSDK(state)
	ctx := context.Background()
	overlayCtx, ctxErr := harnessprotocol.NewHarnessContext("researcher", "team-a_researcher",
		"team-session-1", "HOST PROMPT", harnessprotocol.ContextOptions{})
	require.NoError(t, ctxErr)
	require.NoError(t, h.Start(ctx, overlayCtx))

	// overlay 已创建且 env 注入了变量
	require.NotNil(t, h.overlay)
	overlayPath := h.overlay.path
	_, err = osStat(overlayPath)
	require.NoError(t, err)
	state.mu.Lock()
	require.Len(t, state.constructorOptions, 1)
	options := state.constructorOptions[0]
	state.mu.Unlock()
	assert.NotEmpty(t, options.extraEnv[h.overlay.envName])

	require.NoError(t, h.Stop(ctx))
	_, err = osStat(overlayPath)
	assert.True(t, osIsNotExist(err))
	assert.Nil(t, h.overlay)
}

// Python: test_harness.py:784-799 test_failed_start_removes_overlay
func TestFailedStartRemovesOverlay(t *testing.T) {
	config := DefaultDshHarnessConfig()
	h, err := NewDshHarness(config)
	require.NoError(t, err)
	h.sdkFactory = failingFactory(fmt.Errorf("boom"))
	err = h.Start(context.Background(), testContext())
	require.Error(t, err)
	assert.Nil(t, h.overlay)
}

// Python: test_harness.py:776-782 test_mcp_transport_validation_rejects_duplicate_names
func TestMCPTransportValidationRejectsDuplicateNames(t *testing.T) {
	h, err := NewDshHarness(DefaultDshHarnessConfig())
	require.NoError(t, err)
	dupCtx := testContext()
	dupCtx.McpServers = []harnessprotocol.McpServerConfig{
		{Name: "dup", Transport: harnessprotocol.McpTransportHTTP},
		{Name: "dup", Transport: harnessprotocol.McpTransportHTTP},
	}
	err = h.ValidateContext(context.Background(), dupCtx)
	require.Error(t, err)
	assert.Equal(t, "DSH MCP names must be unique and match [A-Za-z0-9_-]{1,32}", err.Error())
}

// osStat/osIsNotExist 测试助手。
func osStat(path string) (os.FileInfo, error) { return os.Stat(path) }
func osIsNotExist(err error) bool             { return errors.Is(err, fs.ErrNotExist) }
