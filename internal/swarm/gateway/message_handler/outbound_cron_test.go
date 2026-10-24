package message_handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestHandleAgentServerPush_Nil 测试 nil push 消息
func TestHandleAgentServerPush_Nil(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.handleAgentServerPush(nil)
}

// TestHandleAgentServerPush_空Map 测试空 map push 消息
func TestHandleAgentServerPush_空Map(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.handleAgentServerPush(map[string]any{})
}

// ──────────────────────────── handleCronPushPayload 测试 ────────────────────────────

// fakeCronController fake CronControllerAPI
type fakeCronController struct {
	listed           bool
	created          bool
	got              bool
	updated          bool
	deleted          bool
	toggled          bool
	preview          bool
	runNow           bool
	lastJobID        string
	lastCreateParams map[string]any
	lastPatch        map[string]any
	lastEnabled      bool
	lastCount        int
}

func (f *fakeCronController) ListJobs() ([]map[string]any, error) {
	f.listed = true
	return []map[string]any{{"id": "j1", "name": "n"}}, nil
}
func (f *fakeCronController) GetJob(jobID string) (map[string]any, error) {
	f.got = true
	f.lastJobID = jobID
	if jobID == "missing" {
		return nil, nil
	}
	return map[string]any{"id": jobID}, nil
}
func (f *fakeCronController) CreateJob(params map[string]any) (map[string]any, error) {
	f.created = true
	f.lastCreateParams = params
	return map[string]any{"id": "new-job"}, nil
}
func (f *fakeCronController) UpdateJob(jobID string, patch map[string]any) (map[string]any, error) {
	f.updated = true
	f.lastJobID = jobID
	f.lastPatch = patch
	return map[string]any{"id": jobID}, nil
}
func (f *fakeCronController) DeleteJob(jobID string) (bool, error) {
	f.deleted = true
	f.lastJobID = jobID
	return true, nil
}
func (f *fakeCronController) ToggleJob(jobID string, enabled bool) (map[string]any, error) {
	f.toggled = true
	f.lastJobID = jobID
	f.lastEnabled = enabled
	return map[string]any{"id": jobID, "enabled": enabled}, nil
}
func (f *fakeCronController) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	f.preview = true
	f.lastJobID = jobID
	f.lastCount = count
	return []map[string]any{{"push_at": "2026-01-01T09:00:00+08:00"}}, nil
}
func (f *fakeCronController) RunNow(jobID string) (string, error) {
	f.runNow = true
	f.lastJobID = jobID
	return "run-1", nil
}

// drainRobotMessages 消费 robotMessages 并收集
func drainRobotMessages(mh *MessageHandler, timeout time.Duration) []*schema.Message {
	var out []*schema.Message
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case msg := <-mh.robotMessages:
			out = append(out, msg)
		default:
			if len(out) > 0 {
				return out
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	return out
}

// TestHandleCronPushPayload_控制器缺失 无 controller 直接 return
func TestHandleCronPushPayload_控制器缺失(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.handleCronPushPayload(map[string]any{"action": "list"}, "r", "web", "s", nil)
}

// TestHandleCronPushPayload_nilPayload nil payload 直接 return
func TestHandleCronPushPayload_nilPayload(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.SetCronController(&fakeCronController{})
	mh.handleCronPushPayload(nil, "r", "web", "s", nil)
}

// TestHandleCronPushPayload_路由 8 action 路由矩阵
func TestHandleCronPushPayload_路由(t *testing.T) {
	cases := []struct {
		name    string
		action  string
		payload map[string]any
		verify  func(f *fakeCronController, t *testing.T)
	}{
		{
			name:    "list",
			action:  "list",
			payload: map[string]any{"action": "list"},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.listed {
					t.Error("list action should route to controller")
				}
			},
		},
		{
			name:    "get",
			action:  "get",
			payload: map[string]any{"data": map[string]any{"job_id": "j-9"}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.got {
					t.Error("get action should route")
				}
				if f.lastJobID != "j-9" {
					t.Errorf("job_id = %q", f.lastJobID)
				}
			},
		},
		{
			name:    "create",
			action:  "create",
			payload: map[string]any{"data": map[string]any{"name": "n"}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.created {
					t.Error("create action should route")
				}
				if f.lastCreateParams["name"] != "n" {
					t.Errorf("create params = %v", f.lastCreateParams)
				}
			},
		},
		{
			name:    "update",
			action:  "update",
			payload: map[string]any{"data": map[string]any{"job_id": "j-2", "patch": map[string]any{"name": "x"}}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.updated {
					t.Error("update action should route")
				}
				if f.lastJobID != "j-2" || f.lastPatch["name"] != "x" {
					t.Errorf("update args: job=%q patch=%v", f.lastJobID, f.lastPatch)
				}
			},
		},
		{
			name:    "delete",
			action:  "delete",
			payload: map[string]any{"data": map[string]any{"job_id": "j-3"}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.deleted || f.lastJobID != "j-3" {
					t.Error("delete action should route")
				}
			},
		},
		{
			name:    "toggle",
			action:  "toggle",
			payload: map[string]any{"data": map[string]any{"job_id": "j-4", "enabled": true}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.toggled || !f.lastEnabled {
					t.Error("toggle action should route with enabled=true")
				}
			},
		},
		{
			name:    "preview",
			action:  "preview",
			payload: map[string]any{"data": map[string]any{"job_id": "j-5", "count": float64(7)}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.preview || f.lastJobID != "j-5" || f.lastCount != 7 {
					t.Errorf("preview action: %+v", f)
				}
			},
		},
		{
			name:    "run_now",
			action:  "run_now",
			payload: map[string]any{"data": map[string]any{"job_id": "j-6"}},
			verify: func(f *fakeCronController, t *testing.T) {
				if !f.runNow || f.lastJobID != "j-6" {
					t.Error("run_now action should route")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mh := createTestMessageHandlerWithTransport()
			cc := &fakeCronController{}
			mh.SetCronController(cc)

			// 保证 action 字段存在（payload 只描述 data 部分）
			if tc.payload == nil {
				tc.payload = map[string]any{}
			}
			tc.payload["action"] = tc.action
			mh.handleCronPushPayload(tc.payload, "req-1", "web", "sess-1", nil)

			tc.verify(cc, t)
			// 每个分支都应回发一条 tool_result 消息
			msgs := drainRobotMessages(mh, 200*time.Millisecond)
			if len(msgs) != 1 {
				t.Fatalf("tool_result message count = %d, want 1", len(msgs))
			}
			out := msgs[0]
			if out.ID != "req-1" {
				t.Errorf("message ID = %q, want req-1", out.ID)
			}
			if out.EventType != "chat.tool_result" {
				t.Errorf("event_type = %v", out.EventType)
			}
			if out.EnableStreaming {
				t.Error("enable_streaming should be false")
			}
			payload := out.Payload
			if payload["tool_name"] != "cron" {
				t.Errorf("tool_name = %v", payload["tool_name"])
			}
		})
	}
}

// TestHandleCronPushPayload_create模式覆盖 stream mode 覆盖 create 默认值
func TestHandleCronPushPayload_create模式覆盖(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)
	// 模拟 stream mode 已记录
	mh.streamMu.Lock()
	mh.streamModes["req-1"] = "agent.plan"
	mh.streamMu.Unlock()

	mh.handleCronPushPayload(map[string]any{
		"action": "create",
		"data":   map[string]any{"name": "n"},
	}, "req-1", "web", "sess-1", nil)

	if !cc.created {
		t.Fatal("create action should route")
	}
	if cc.lastCreateParams["mode"] != "agent.plan" {
		t.Errorf("stream mode should override, got %v", cc.lastCreateParams["mode"])
	}
	drainRobotMessages(mh, 100*time.Millisecond)
}

// TestHandleCronPushPayload_未知action 未知 action 返回 error 结果
func TestHandleCronPushPayload_未知action(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	mh.handleCronPushPayload(map[string]any{"action": "bogus"}, "req-1", "web", "s", nil)

	msgs := drainRobotMessages(mh, 200*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("message count = %d", len(msgs))
	}
	result := msgs[0].Payload["result"].(map[string]any)
	if _, has := result["error"]; !has {
		t.Errorf("unknown action should return error, got %v", result)
	}
}

// TestHandleCronPushPayload_控制器报错 控制器返回错误时结果含 error
func TestHandleCronPushPayload_控制器报错(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.SetCronController(&errCronController{})
	mh.handleCronPushPayload(map[string]any{"action": "list"}, "r", "web", "s", nil)

	msgs := drainRobotMessages(mh, 200*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("message count = %d", len(msgs))
	}
	result := msgs[0].Payload["result"].(map[string]any)
	if result["error"] != "boom" {
		t.Errorf("error = %v", result["error"])
	}
}

// TestHandleCronPushPayload_data非map data 非 map 时静默空参数
func TestHandleCronPushPayload_data非map(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	mh.handleCronPushPayload(map[string]any{
		"action": "create",
		"data":   "not-a-map",
	}, "r", "web", "s", nil)

	if !cc.created {
		t.Error("create should still route with empty params")
	}
	if len(cc.lastCreateParams) != 0 {
		t.Errorf("params should be empty, got %v", cc.lastCreateParams)
	}
	drainRobotMessages(mh, 100*time.Millisecond)
}

// TestHandleCronPushPayload_metadata回传 metadata 原样回传
func TestHandleCronPushPayload_metadata回传(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	md := map[string]any{"request_id": "req-1", "chat_type": "group"}
	mh.handleCronPushPayload(map[string]any{"action": "list"}, "req-1", "feishu", "sess-9", md)

	msgs := drainRobotMessages(mh, 200*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("message count = %d", len(msgs))
	}
	out := msgs[0]
	if out.ChannelID != "feishu" || out.SessionID != "sess-9" {
		t.Errorf("routing: channel=%q session=%q", out.ChannelID, out.SessionID)
	}
	if out.Metadata["chat_type"] != "group" {
		t.Errorf("metadata should pass through: %v", out.Metadata)
	}
	if out.Timestamp <= 0 {
		t.Error("timestamp should be set")
	}
}

// TestHandleCronPushPayload_delete结果结构 delete 分支结果含 deleted 布尔
func TestHandleCronPushPayload_delete结果结构(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	mh.handleCronPushPayload(map[string]any{
		"action": "delete", "data": map[string]any{"job_id": "j"},
	}, "r", "web", "s", nil)

	msgs := drainRobotMessages(mh, 200*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("message count = %d", len(msgs))
	}
	result := msgs[0].Payload["result"].(map[string]any)
	if result["deleted"] != true {
		t.Errorf("deleted = %v", result["deleted"])
	}
}

// TestHandleCronPushPayload_runNow结果结构 run_now 分支结果含 run_id
func TestHandleCronPushPayload_runNow结果结构(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	mh.handleCronPushPayload(map[string]any{
		"action": "run_now", "data": map[string]any{"job_id": "j"},
	}, "r", "web", "s", nil)

	msgs := drainRobotMessages(mh, 200*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("message count = %d", len(msgs))
	}
	result := msgs[0].Payload["result"].(map[string]any)
	if result["run_id"] != "run-1" {
		t.Errorf("run_id = %v", result["run_id"])
	}
}

// TestHandleCronPushPayload_消息JSON可序列化 回发消息可 JSON 序列化（wire 兼容）
func TestHandleCronPushPayload_消息JSON可序列化(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	cc := &fakeCronController{}
	mh.SetCronController(cc)

	mh.handleCronPushPayload(map[string]any{"action": "list"}, "r", "web", "s", nil)

	msgs := drainRobotMessages(mh, 200*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("message count = %d", len(msgs))
	}
	if _, err := json.Marshal(msgs[0]); err != nil {
		t.Errorf("message should marshal: %v", err)
	}
}

// errCronController 所有方法返回错误的 controller
type errCronController struct{}

func (e *errCronController) ListJobs() ([]map[string]any, error) {
	return nil, errBoom
}
func (e *errCronController) GetJob(jobID string) (map[string]any, error) {
	return nil, errBoom
}
func (e *errCronController) CreateJob(params map[string]any) (map[string]any, error) {
	return nil, errBoom
}
func (e *errCronController) UpdateJob(jobID string, patch map[string]any) (map[string]any, error) {
	return nil, errBoom
}
func (e *errCronController) DeleteJob(jobID string) (bool, error) {
	return false, errBoom
}
func (e *errCronController) ToggleJob(jobID string, enabled bool) (map[string]any, error) {
	return nil, errBoom
}
func (e *errCronController) PreviewJob(jobID string, count int) ([]map[string]any, error) {
	return nil, errBoom
}
func (e *errCronController) RunNow(jobID string) (string, error) {
	return "", errBoom
}

// errBoom 固定测试错误
var errBoom = errorString("boom")

// errorString 简单错误类型
type errorString string

func (e errorString) Error() string { return string(e) }
