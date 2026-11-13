package processcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeAnswerClient 记录 answer 流调用（answerStreamer 接口替身）。
type fakeAnswerClient struct {
	mu       sync.Mutex
	answers  []*swruntime.InteractionAnswerInput
	entered  chan struct{}
	release  chan struct{}
	released bool
}

func newFakeAnswerClient() *fakeAnswerClient {
	return &fakeAnswerClient{entered: make(chan struct{}, 16), release: make(chan struct{})}
}

// StreamInteractionAnswer 记录回答并模拟一次流式会话。
func (f *fakeAnswerClient) StreamInteractionAnswer(
	ctx context.Context, answer *swruntime.InteractionAnswerInput,
	onEvent func(*swruntime.RuntimeEvent) bool,
) error {
	f.mu.Lock()
	f.answers = append(f.answers, answer)
	f.mu.Unlock()
	f.entered <- struct{}{}
	<-f.release
	if onEvent != nil {
		onEvent(&swruntime.RuntimeEvent{
			Payload:    map[string]any{"event_type": "chat.answer_accepted"},
			OK:         true,
			IsComplete: true,
		})
	}
	return nil
}

func (f *fakeAnswerClient) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.answers)
}

func (f *fakeAnswerClient) last() *swruntime.InteractionAnswerInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.answers[len(f.answers)-1]
}

// newControlTestRig 构造 controller + 可注入 stdin 的 reader。
func newControlTestRig(t *testing.T) (*DuplexController, *OneShotWriter, *bytes.Buffer, *os.File, *os.File) {
	t.Helper()
	pipeR, pipeW, err := os.Pipe()
	require.NoError(t, err)
	reader, err := NewDuplexLineReader(pipeR, MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, err)
	out := &bytes.Buffer{}
	writer, err := NewOneShotWriter(out, "run-req-1")
	require.NoError(t, err)
	sid := "sess-1"
	writer.SessionID = &sid
	ctrl := NewDuplexController(reader, writer, false)
	return ctrl, writer, out, pipeR, pipeW
}

// writeControlFrame 写一行控制帧进 stdin。
func writeControlFrame(t *testing.T, pipeW *os.File, body string) {
	t.Helper()
	_, err := pipeW.WriteString(body + "\n")
	require.NoError(t, err)
}

// interactionEvent 构造一张交互卡事件。
func interactionEvent(requestID string) *swruntime.RuntimeEvent {
	return &swruntime.RuntimeEvent{
		Payload: map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": requestID,
			"questions": []any{
				map[string]any{"options": []any{map[string]any{"value": "reject"}}},
			},
		},
		OK: true,
	}
}

// ──────────────────────────── 用例 ────────────────────────────

// TestDuplexController_交互登记与answer回填 完整闭环：交互卡 → 通知 →
// answer 帧 → answer 流开启。Python: duplex_control.py:343-362 + :144-154。
func TestDuplexController_交互登记与answer回填(t *testing.T) {
	ctrl, _, out, _, pipeW := newControlTestRig(t)
	client := newFakeAnswerClient()
	sid := "sess-1"
	request := &schema.AgentRequest{
		RequestID: "run-req-1", ChannelID: "process_cli", SessionID: &sid,
		Params: json.RawMessage(`{"mode":"agent","work_mode":"work"}`),
	}
	rootEvents := []*swruntime.RuntimeEvent{interactionEvent("inter-1")}
	idx := 0
	releaseRoot := make(chan struct{})
	rootFn := streamDriver(func(handler func(*swruntime.RuntimeEvent) bool) error {
		if idx < len(rootEvents) {
			handler(rootEvents[idx])
			idx++
		}
		// 等 answer 流路径被触发后结束根流
		<-releaseRoot
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctrl.Start(ctx, cancel)

	consumeDone := make(chan error, 1)
	go func() {
		_, err := ctrl.Consume(ctx, rootFn, client, request,
			func(*swruntime.RuntimeEvent) error { return nil })
		consumeDone <- err
	}()

	// 等待 interaction.requested 通知写出
	waitForOutput(t, out, "interaction.requested", 3*time.Second)
	// 从 interaction.requested 通知取真实 token，stdin 写 answer 帧
	line := lastOutputLine(t, out)
	var notice map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &notice))
	token := notice["payload"].(map[string]any)["interaction_id"].(string)
	writeControlFrame(t, pipeW, `{"schema_version":"`+protocol.CurrentSchemaVersion+
		`","type":"answer","request_id":"run-req-1","session_id":"sess-1",`+
		`"interaction_id":"`+token+`","answers":[{"selected_options":["ok"]}]}`)

	// answer 流被打开
	select {
	case <-client.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("answer 流未开启")
	}
	ans := client.last()
	require.Equal(t, "inter-1", ans.InteractionID)
	require.Equal(t, "agent", ans.Mode)
	require.Equal(t, "work", ans.WorkMode)
	require.Equal(t, []map[string]any{{"selected_options": []any{"ok"}}}, ans.Answers)

	close(releaseRoot)
	cancel()
	<-consumeDone
}

// TestDuplexController_cancel终止 cancel 帧 → CANCELLED/130。
// Python: duplex_control.py:128-134。
func TestDuplexController_cancel终止(t *testing.T) {
	ctrl, _, _, _, pipeW := newControlTestRig(t)
	client := newFakeAnswerClient()
	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "run-req-1", ChannelID: "process_cli", SessionID: &sid}
	ctx, cancel := context.WithCancel(context.Background())
	rootFn := streamDriver(func(handler func(*swruntime.RuntimeEvent) bool) error {
		<-ctx.Done()
		return context.Canceled
	})
	ctrl.Start(ctx, cancel)
	consumeDone := make(chan error, 1)
	go func() {
		_, err := ctrl.Consume(ctx, rootFn, client, request,
			func(*swruntime.RuntimeEvent) error { return nil })
		consumeDone <- err
	}()

	writeControlFrame(t, pipeW, `{"schema_version":"`+protocol.CurrentSchemaVersion+
		`","type":"cancel","request_id":"run-req-1"}`)

	// owner 被取消 → Consume 返回
	select {
	case <-consumeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel 后 Consume 未返回")
	}
	require.Equal(t, "CANCELLED", ctrl.FailureInfo().Code)
	require.Equal(t, 130, ctrl.FailureExitCode())
}

// TestDuplexController_身份不匹配终止 request_id 不匹配 → 终止。
// Python: duplex_control.py:121-127。
func TestDuplexController_身份不匹配终止(t *testing.T) {
	ctrl, _, _, _, pipeW := newControlTestRig(t)
	client := newFakeAnswerClient()
	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "run-req-1", ChannelID: "process_cli", SessionID: &sid}
	ctx, cancel := context.WithCancel(context.Background())
	rootFn := streamDriver(func(handler func(*swruntime.RuntimeEvent) bool) error {
		<-ctx.Done()
		return nil
	})
	ctrl.Start(ctx, cancel)
	consumeDone := make(chan error, 1)
	go func() {
		_, err := ctrl.Consume(ctx, rootFn, client, request,
			func(*swruntime.RuntimeEvent) error { return nil })
		consumeDone <- err
	}()
	writeControlFrame(t, pipeW, `{"schema_version":"`+protocol.CurrentSchemaVersion+
		`","type":"cancel","request_id":"other-req"}`)
	select {
	case <-consumeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("身份不匹配后 Consume 未返回")
	}
	require.Equal(t, "INVALID_CONTROL", ctrl.FailureInfo().Code)
}

// TestDuplexController_toolResult回填 宿主工具挂起-回填。
// Python: duplex_control.py:263-285 call_host_tool + :135-143。
func TestDuplexController_toolResult回填(t *testing.T) {
	ctrl, _, out, _, pipeW := newControlTestRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctrl.Start(ctx, cancel)

	type callResult struct {
		result any
		err    *string
	}
	done := make(chan callResult, 1)
	go func() {
		result, errStr := ctrl.CallHostTool(ctx, "host_query", map[string]any{"q": "x"})
		done <- callResult{result, errStr}
	}()
	waitForOutput(t, out, "host_tool.requested", 3*time.Second)
	// 从通知里取 call_id
	line := lastOutputLine(t, out)
	var notice map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &notice))
	payload := notice["payload"].(map[string]any)
	callID := payload["call_id"].(string)

	writeControlFrame(t, pipeW, `{"schema_version":"`+protocol.CurrentSchemaVersion+
		`","type":"tool_result","request_id":"run-req-1","session_id":"sess-1",`+
		`"call_id":"`+callID+`","result":{"rows":7}}`)
	select {
	case r := <-done:
		require.Nil(t, r.err)
		require.Equal(t, float64(7), r.result.(map[string]any)["rows"])
	case <-time.After(3 * time.Second):
		t.Fatal("tool_result 未回填")
	}

	// 未知 call_id → HOST_TOOL_NOT_PENDING 终止
	writeControlFrame(t, pipeW, `{"schema_version":"`+protocol.CurrentSchemaVersion+
		`","type":"tool_result","request_id":"run-req-1","session_id":"sess-1",`+
		`"call_id":"missing","error":"x"}`)
	waitForFailure(t, ctrl, "HOST_TOOL_NOT_PENDING", 3*time.Second)
}

// TestDuplexController_unattended权限卡自动reject unattended 分支。
// Python: duplex_control.py:287-313 _permission_rejection + :348-355。
func TestDuplexController_unattended权限卡自动reject(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pipeW.Close() })
	// unattended：reader 必须为 nil（Python _execute_document 同款）
	reader, rerr := NewDuplexLineReader(pipeR, MaxDuplexLineBytes, 10*time.Millisecond)
	require.NoError(t, rerr)
	_ = reader
	out := &bytes.Buffer{}
	writer, werr := NewOneShotWriter(out, "run-req-1")
	require.NoError(t, werr)
	ctrl := NewDuplexController(nil, writer, true)
	client := newFakeAnswerClient()
	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "run-req-1", ChannelID: "process_cli", SessionID: &sid}

	cardEvents := []*swruntime.RuntimeEvent{
		interactionEvent("perm-1"), // 权限卡（source 缺省按权限卡处理：见 permissionRejection）
	}
	idx := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rootFn := streamDriver(func(handler func(*swruntime.RuntimeEvent) bool) error {
		if idx < len(cardEvents) {
			ev := cardEvents[idx]
			idx++
			// 权限卡标记
			ev.Payload["source"] = "permission_interrupt"
			handler(ev)
		}
		<-ctx.Done()
		return nil
	})
	ctrl.Start(ctx, cancel)
	consumeDone := make(chan error, 1)
	go func() {
		_, cerr := ctrl.Consume(ctx, rootFn, client, request,
			func(*swruntime.RuntimeEvent) error { return nil })
		consumeDone <- cerr
	}()
	// 自动 reject 的 answer 流开启
	select {
	case <-client.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("unattended 未自动 reject")
	}
	ans := client.last()
	require.Equal(t, []map[string]any{{"selected_options": []any{"reject"}}}, ans.Answers)
	waitForOutput(t, out, "interaction.auto_denied", 3*time.Second)
	cancel()
	<-consumeDone
}

// TestDuplexController_pending上限 超过 16 张交互卡报 Too many pending。
// Python: duplex_control.py:247-248。
func TestDuplexController_pending上限(t *testing.T) {
	ctrl, _, _, _, _ := newControlTestRig(t)
	// 直接灌满 pending 再登记第 17 张
	for i := 0; i < 16; i++ {
		ctrl.pending["tok"+string(rune('a'+i))] = map[string]any{}
	}
	_, _, err := ctrl.registerInteraction(interactionEvent("extra"))
	require.Error(t, err)
	require.Equal(t, "INVALID_CONTROL", errCode(err))
	require.True(t, strings.Contains(err.Error(), "Too many pending interactions."))
}

// TestDuplexController_INPUT_CLOSED stdin EOF 且有待答 → 终止。
// Python: duplex_control.py:163-169 _check_answer_available。
func TestDuplexController_INPUT_CLOSED(t *testing.T) {
	ctrl, _, _, _, _ := newControlTestRig(t)
	ctrl.inputClosed = true
	ctrl.pending["tok"] = map[string]any{}
	ctrl.checkAnswerAvailableLocked()
	require.NotNil(t, ctrl.failure)
	require.Equal(t, "INPUT_CLOSED", ctrl.failure.Code)
	require.Equal(t, string(protocol.StatusCancelled), ctrl.failureStatus)
	require.Equal(t, 130, ctrl.failureExitCode)
}

// TestDuplexController_完成判定 三形态 + suspended 排除。
// Python: duplex_control.py:363-381。
func TestDuplexController_完成判定(t *testing.T) {
	sid := "sess-1"
	request := &schema.AgentRequest{RequestID: "run-req-1", ChannelID: "process_cli", SessionID: &sid}

	run := func(t *testing.T, events []*swruntime.RuntimeEvent) bool {
		ctrl, _, _, _, _ := newControlTestRig(t)
		client := newFakeAnswerClient()
		idx := 0
		rootFn := streamDriver(func(handler func(*swruntime.RuntimeEvent) bool) error {
			if idx < len(events) {
				handler(events[idx])
				idx++
			}
			return nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctrl.Start(ctx, cancel)
		completed, err := ctrl.Consume(ctx, rootFn, client, request,
			func(*swruntime.RuntimeEvent) error { return nil })
		require.NoError(t, err)
		return completed
	}

	// chat.final 且无 pending → 完成
	require.True(t, run(t, []*swruntime.RuntimeEvent{
		{Payload: map[string]any{"event_type": "chat.final", "content": "done"}, OK: true, IsComplete: true},
	}))
	// 根流 is_complete → 完成
	require.True(t, run(t, []*swruntime.RuntimeEvent{
		{Payload: map[string]any{"event_type": "runtime.event"}, OK: true, IsComplete: true},
	}))
	// runtime_completion == "completed" → 完成
	require.True(t, run(t, []*swruntime.RuntimeEvent{
		{Payload: map[string]any{"event_type": "runtime.event"}, OK: true,
			RuntimeCompletion: "completed"},
	}))
	// suspended 刷出 → 不算完成
	require.False(t, run(t, []*swruntime.RuntimeEvent{
		{Payload: map[string]any{"event_type": "chat.final"}, OK: true,
			IsComplete: true, RuntimeCompletion: "suspended"},
	}))
	// 错误事件 → false
	require.False(t, run(t, []*swruntime.RuntimeEvent{
		{Payload: map[string]any{"event_type": "chat.error", "error": "boom"}, OK: false, IsComplete: true},
	}))
}

// TestDuplexController_StopInput排空 队列满时生产者不被卡死。
// Python: duplex_control.py:383-401 stop_input。
func TestDuplexController_StopInput排空(t *testing.T) {
	ctrl, _, _, _, _ := newControlTestRig(t)
	// 灌满队列（32）
	for i := 0; i < 32; i++ {
		ctrl.queue <- streamItem{operationID: "root", event: &swruntime.RuntimeEvent{OK: true}}
	}
	// 模拟一个阻塞在生产者上的泵
	producerDone := make(chan struct{})
	go func() {
		ctrl.queue <- streamItem{operationID: "root", event: &swruntime.RuntimeEvent{OK: true}}
		close(producerDone)
	}()
	ctrl.StopInput()
	select {
	case <-producerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("StopInput 应排空队列解除生产者阻塞")
	}
	require.True(t, ctrl.stoppingInput)
}

// ──────────────────────────── 小助手 ────────────────────────────

// waitForOutput 轮询等待输出包含子串。
func waitForOutput(t *testing.T, out *bytes.Buffer, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), substr) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("输出在 %v 内未出现 %q: %s", timeout, substr, out.String())
}

// lastOutputLine 取输出最后一条非空行。
func lastOutputLine(t *testing.T, out *bytes.Buffer) string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	require.NotEmpty(t, lines)
	return lines[len(lines)-1]
}

// waitForFailure 轮询等待 failure 出现。
func waitForFailure(t *testing.T, ctrl *DuplexController, code string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info := ctrl.FailureInfo(); info != nil && info.Code == code {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("failure 在 %v 内未出现 %q", timeout, code)
}

// errCode 取错误码。
func errCode(err error) string {
	if dce, ok := err.(*DuplexControlError); ok {
		return dce.Code
	}
	return "INVALID_CONTROL"
}
