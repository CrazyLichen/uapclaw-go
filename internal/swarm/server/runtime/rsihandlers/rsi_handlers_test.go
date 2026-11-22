package rsihandlers

// rsi_handlers.go 单测：分发表逐字、错误语义、session 标记、P1/P2/P3 绑定。

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/rsi"
)

// newFixture 构造带 context 的 handlers。
func newFixture(t *testing.T) (*AgentServerHandlers, *[]map[string]any) {
	t.Helper()
	root := t.TempDir()
	ctx := rsi.BuildRsiServiceContext(root, false, false)
	var pushed []map[string]any
	h := NewAgentServerHandlers(ctx, func(msg map[string]any) error {
		pushed = append(pushed, msg)
		return nil
	}, nil, "web")
	return h, &pushed
}

// TestMethodDispatch_18项逐字 对齐 Python _METHOD_DISPATCH。
func TestMethodDispatch_18项逐字(t *testing.T) {
	want := []string{
		"rsi.dataset.validate", "rsi.task.create", "rsi.task.list", "rsi.task.get",
		"rsi.task.delete", "rsi.training.start", "rsi.training.pause",
		"rsi.training.resume", "rsi.training.terminate", "rsi.report.get",
		"rsi.usage.get", "rsi.artifact.download", "rsi.artifact.files.list",
		"rsi.artifact.files.get", "rsi.tree.get", "rsi.harness.install",
		"rsi.harness.versions.list", "rsi.harness.rollback",
	}
	if len(methodDispatch) != 18 {
		t.Fatalf("method 数漂移: %d", len(methodDispatch))
	}
	for _, m := range want {
		if methodDispatch[m] == "" {
			t.Fatalf("缺 method: %s", m)
		}
	}
}

// TestHandle_未知method BAD_REQUEST。
func TestHandle_未知method(t *testing.T) {
	h, _ := newFixture(t)
	out := h.Handle(map[string]any{"req_method": "rsi.bogus"})
	if out["ok"] != false || out["code"] != "BAD_REQUEST" {
		t.Fatalf("未知 method 漂移: %v", out)
	}
}

// TestHandle_taskList空表 基础查询链。
func TestHandle_taskList空表(t *testing.T) {
	h, _ := newFixture(t)
	out := h.Handle(map[string]any{"req_method": "rsi.task.list", "params": map[string]any{}})
	if out["ok"] != true {
		t.Fatalf("list 漂移: %v", out)
	}
	tasks := out["payload"].(map[string]any)["tasks"].([]map[string]any)
	if len(tasks) != 0 {
		t.Fatalf("空表漂移")
	}
}

// TestHandle_session标记 session_id 注入私有键（task_create 链验证）。
func TestHandle_session标记(t *testing.T) {
	h, _ := newFixture(t)
	// 缺材料 create → BAD_REQUEST（此处仅验证错误归一与 session 透传路径可达）。
	out := h.Handle(map[string]any{
		"req_method": "rsi.task.create",
		"session_id": "sess-1",
		"params":     map[string]any{"scenario": "BOGUS"},
	})
	if out["ok"] != false {
		t.Fatalf("非法场景应拒: %v", out)
	}
	if !strings.Contains(valueOf(out["error"]), "scenario") {
		t.Fatalf("错误语义漂移: %v", out)
	}
}

// TestP1状态钩子 FAILED 推送带 failure_reason。
func TestP1状态钩子(t *testing.T) {
	h, pushed := newFixture(t)
	// 直接造 RUNNING→FAILED 迁移（钩子在服务侧权威）。
	task := rsi.RsiTask{TaskID: "rsi-p1", Name: "n", Scenario: "HARNESS",
		Status: "RUNNING", CreatedAt: rsi.UtcnowISO(),
		ModelRefs: map[string]any{}, Config: map[string]any{}, StatusHistory: []map[string]any{}}
	if _, err := h.context.Store.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := h.context.Store.UpdateStatus("rsi-p1", []string{"RUNNING"}, "FAILED", "引擎崩溃"); err != nil {
		t.Fatal(err)
	}
	if len(*pushed) == 0 {
		t.Fatalf("P1 未推送")
	}
	msg := (*pushed)[len(*pushed)-1]
	payload := msg["payload"].(map[string]any)
	if payload["event_type"] != PushStatusChanged || payload["failure_reason"] != "引擎崩溃" {
		t.Fatalf("P1 载荷漂移: %v", payload)
	}
	if msg["channel_id"] != "web" {
		t.Fatalf("channel_id 漂移: %v", msg)
	}
}

// TestP2P3推送 统一出口组装。
func TestP2P3推送(t *testing.T) {
	h, pushed := newFixture(t)
	h.context.RegisterWorkerPush(map[string]func(string, string, map[string]any) error{
		PushProgress: func(eventType, taskID string, payload map[string]any) error {
			merged := map[string]any{"task_id": taskID}
			for k, v := range payload {
				merged[k] = v
			}
			return h.sendPush(h.buildPushMessage(eventType, merged))
		},
	})
	h.context.Worker.RegisterPushCallbacks(map[string]func(string, string, map[string]any) error{
		PushProgress: func(eventType, taskID string, payload map[string]any) error {
			merged := map[string]any{"task_id": taskID}
			for k, v := range payload {
				merged[k] = v
			}
			return h.sendPush(h.buildPushMessage(eventType, merged))
		},
	})
	// 直接触发 worker 事件回调（模拟引擎 progress 事件链）。
	for _, cb := range h.context.Worker.RegisteredPushCallbacksForTest() {
		_ = cb(PushProgress, "rsi-x", map[string]any{"iteration": 1})
	}
	if len(*pushed) == 0 {
		t.Fatalf("P2 未推送")
	}
	msg := (*pushed)[len(*pushed)-1]
	payload := msg["payload"].(map[string]any)
	if payload["event_type"] != PushProgress || payload["task_id"] != "rsi-x" {
		t.Fatalf("P2 载荷漂移: %v", payload)
	}
}

// TestHarnessInstall_未完成态 前置校验经分发层透出。
func TestHarnessInstall_未完成态(t *testing.T) {
	h, _ := newFixture(t)
	task := rsi.RsiTask{TaskID: "rsi-h", Name: "n", Scenario: "HARNESS",
		Status: "RUNNING", CreatedAt: rsi.UtcnowISO(),
		ModelRefs: map[string]any{}, Config: map[string]any{}}
	if _, err := h.context.Store.Create(task); err != nil {
		t.Fatal(err)
	}
	out := h.Handle(map[string]any{"req_method": "rsi.harness.install",
		"params": map[string]any{"task_id": "rsi-h"}})
	if out["ok"] != false || !strings.Contains(valueOf(out["error"]), "尚未完成") {
		t.Fatalf("安装前置校验漂移: %v", out)
	}
}
