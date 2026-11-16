package videotask

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	dupextasks "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/duplex/tasks"
)

// seedTask 经账本事务种入任务行（对齐 Execute 的 owner/core_session_id/request_id 三元组校验）。
func seedTask(t *testing.T, store *dupextasks.TaskStore, id, owner, coreSession, requestID, status string) {
	t.Helper()
	seed := map[string]any{
		"id": id, "owner": owner, "session": "s1", "status": status,
		"sequence": 1, "instruction": "任务", "request": map[string]any{},
		"changes": []any{}, "progress": []any{}, "position": 1,
		"request_id": requestID, "core_session_id": coreSession, "checkpoint_open": true,
	}
	if err := store.Transaction(func(db *sql.Tx) error { return dupextasks.TxPut(db, seed) }); err != nil {
		t.Fatalf("种入任务失败: %v", err)
	}
}

// fakeHarness 受控执行句柄桩。
type fakeHarness struct {
	done        chan struct{}
	activeRound bool
}

func (f *fakeHarness) CurrentExecutionDone() <-chan struct{} { return f.done }
func (f *fakeHarness) HasActiveRound() bool                  { return f.activeRound }

// fakeStream 输出租约桩。
type fakeStream struct{ closed bool }

func (f *fakeStream) Close(bool) error { f.closed = true; return nil }

// newTestRing 组装网关半边（真账本）+ 推送环 + ACK 环。
func newTestRing(t *testing.T) (*dupextasks.GatewayTaskEndpoint, *dupextasks.TaskStore) {
	t.Helper()
	store, err := dupextasks.NewTaskStore(t.TempDir() + "/tasks.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := dupextasks.NewGatewayTaskEndpoint(store)
	if err != nil {
		t.Fatal(err)
	}
	// ACK 回环：sendAck → server_adapter.Handle（checkpointACKMethod）→ ResolveCheckpointAck
	SendRuntimePush = func(msg map[string]any) bool {
		payload, _ := msg["payload"].(map[string]any)
		frame := dupextasks.CheckpointFrame{
			EventType: strOf(payload["event_type"]),
			ChannelID: strOf(msg["channel_id"]),
			RequestID: strOf(msg["request_id"]),
			SessionID: strOf(msg["session_id"]),
			Payload:   payload,
		}
		adapter := &VoiceTaskServerAdapter{}
		_, _ = dupextasks.HandleCheckpointPush(frame, func(params map[string]any) error {
			if dbgResult, ok2 := params["result"].(map[string]any); ok2 {
				fmt.Println("DBG ack result:", dbgResult)
			}
			params["execution_request_id"] = frame.RequestID
			_, err := adapter.Handle(checkpointACKMethod, params, "ack", strOf(msg["session_id"]), "u1")
			return err
		})
		return true
	}
	t.Cleanup(func() { SendRuntimePush = nil })
	return endpoint, store
}

func TestRemoteTaskEndpoint_Call_环形ACK(t *testing.T) {
	endpoint, store := newTestRing(t)
	// 种入 running 任务行（Execute 校验 owner/core_session_id/request_id 三元组）
	seedTask(t, store, "t1", "u1", "managed-task-s1", "req-1", "running")
	bindingMap := endpoint.Binding(map[string]any{"id": "t1"})

	params := map[string]any{"managed_task_binding": bindingMap}
	ep, err := NewRemoteTaskEndpoint(params, "req-1", "managed-task-s1", "u1")
	if err != nil {
		t.Fatalf("端点解析失败: %v", err)
	}
	result, err := ep.Call("status", nil)
	if err != nil {
		t.Fatalf("环形 ACK 失败: %v", err)
	}
	if result == nil {
		t.Fatal("ACK 结果不应为空")
	}
}

func TestBindTaskExecution_绑定与结算环(t *testing.T) {
	endpoint, store := newTestRing(t)
	seedTask(t, store, "t2", "u1", "managed-task-s1", "req-2", "running")
	bindingMap := endpoint.Binding(map[string]any{"id": "t2"})
	RegisterVoiceRail(NewVoiceAgentTaskRail("owner-1"))
	t.Cleanup(func() { RegisterVoiceRail(nil) })
	done := make(chan struct{})
	harness := &fakeHarness{done: done, activeRound: true}
	inputs := map[string]any{}

	binding, unbind, err := BindTaskExecution(
		map[string]any{"managed_task_binding": bindingMap},
		"req-2", "managed-task-s1", "u1", harness, inputs)
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	if binding == nil {
		t.Fatal("受管请求应产出绑定")
	}
	// run.extra.managed_task_request 已注入
	run := toMap(inputs["run"])
	extra := toMap(toMap(run["context"])["extra"])
	if extra["managed_task_request"] != "req-2" {
		t.Fatalf("应注入 managed_task_request，得到 %v", extra["managed_task_request"])
	}
	// Check 应经环形 ACK 校验通过；run_context 对应 inputs["run"]["context"]
	if err := binding.Check(map[string]any{"run_context": toMap(run["context"])}); err != nil {
		t.Fatalf("Check 失败: %v", err)
	}
	// capture + unbind 结算
	binding.CaptureExecution(true)
	close(done)
	unbind()
	// 重复 unbind 安全
	unbind()
	WaitSettlements()
}

func TestBindTaskExecution_非受管请求短路(t *testing.T) {
	newTestRing(t)
	RegisterVoiceRail(NewVoiceAgentTaskRail("owner-1"))
	t.Cleanup(func() { RegisterVoiceRail(nil) })
	// Python 语义：无 managed_task_binding 且无 managed-task- 前缀才短路
	binding, unbind, err := BindTaskExecution(
		map[string]any{}, "req-3", "ordinary-session", "u1", nil, map[string]any{})
	if err != nil {
		t.Fatalf("普通会话不应报错: %v", err)
	}
	if binding != nil {
		t.Fatal("普通会话不应绑定")
	}
	unbind() // 空操作
}

func TestTaskCheckpointDispatch_阶段分派(t *testing.T) {
	endpoint, store := newTestRing(t)
	seedTask(t, store, "t4", "u1", "managed-task-s1", "req-4", "running")
	bindingMap := endpoint.Binding(map[string]any{"id": "t4"})
	rail := NewVoiceAgentTaskRail("owner-1")
	RegisterVoiceRail(rail)
	t.Cleanup(func() { RegisterVoiceRail(nil) })
	h4 := &fakeHarness{done: make(chan struct{}), activeRound: true}
	_, unbind, err0 := BindTaskExecution(
		map[string]any{"managed_task_binding": bindingMap},
		"req-4", "managed-task-s1", "u1", h4, map[string]any{})
	if err0 != nil {
		t.Fatalf("绑定失败: %v", err0)
	}
	defer func() { unbind(); WaitSettlements() }()
	defer close(h4.done) // LIFO：先关句柄再 unbind，结算上报不悬挂

	// before_tool → check
	if err := TaskCheckpointDispatch(rail, "before_tool", "req-4",
		map[string]any{"run_context": map[string]any{
			"extra": map[string]any{"managed_task_request": "req-4"}}},
		func(*TaskExecutionBinding) bool { return true }, nil, nil); err != nil {
		t.Fatalf("before_tool check 失败: %v", err)
	}
	// after_model → 观察回执
	if err := TaskCheckpointDispatch(rail, "after_model", "req-4",
		map[string]any{"run_context": map[string]any{
			"extra": map[string]any{"managed_task_request": "req-4"}}},
		func(*TaskExecutionBinding) bool { return true }, nil,
		[]string{`{"managed_task_change":"ch1"}`, "not-json"}); err != nil {
		t.Fatalf("after_model 失败: %v", err)
	}
	// 未绑定请求 ID → TASK_EXECUTION_BINDING_CLOSED
	err := TaskCheckpointDispatch(rail, "before_tool", "req-other", map[string]any{},
		func(*TaskExecutionBinding) bool { return false }, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "TASK_EXECUTION_BINDING_CLOSED") {
		t.Fatalf("未绑定请求应报 BINDING_CLOSED，得到 %v", err)
	}
}

func TestCloseTaskOutput_取消时关租约(t *testing.T) {
	endpoint, store := newTestRing(t)
	seedTask(t, store, "t5", "u1", "managed-task-s2", "req-5", "running")
	bindingMap := endpoint.Binding(map[string]any{"id": "t5"})
	rail := NewVoiceAgentTaskRail("owner-1")
	RegisterVoiceRail(rail)
	t.Cleanup(func() { RegisterVoiceRail(nil) })
	h5 := &fakeHarness{done: make(chan struct{}), activeRound: true}
	binding, unbind, err0 := BindTaskExecution(
		map[string]any{"managed_task_binding": bindingMap},
		"req-5", "managed-task-s2", "u1", h5, map[string]any{})
	if err0 != nil {
		t.Fatalf("绑定失败: %v", err0)
	}
	defer func() { unbind(); WaitSettlements() }()
	defer close(h5.done) // LIFO：先关句柄再 unbind，结算上报不悬挂
	stream := &fakeStream{}
	binding.SetOutputStream(stream)
	// 非 cancelling 状态不关租约
	CloseTaskOutput(rail, "managed-task-s2", "req-5")
	if stream.closed {
		t.Fatal("非 cancelling 不应关租约")
	}
	_ = time.Now // 防 import 抖动
}

func TestResolveCheckpointAck_身份不匹配(t *testing.T) {
	newTestRing(t)
	params := map[string]any{"command_id": "no-such"}
	ok, err := ResolveCheckpointAck(params, "u1", "s1")
	if ok || err != nil {
		t.Fatalf("未知 command_id 应 (false,nil)，得到 %v,%v", ok, err)
	}
}
