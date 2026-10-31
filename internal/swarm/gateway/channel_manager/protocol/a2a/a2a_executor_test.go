package a2a

import (
	"context"
	"encoding/json"
	"testing"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// collectEvents 收集序列产出的事件类型标签
func collectEvents(seq func(func(sdka2a.Event, error) bool)) []string {
	var kinds []string
	seq(func(evt sdka2a.Event, err error) bool {
		if err != nil {
			kinds = append(kinds, "error")
			return false
		}
		switch v := evt.(type) {
		case *sdka2a.Task:
			kinds = append(kinds, "task:"+string(v.Status.State))
		case *sdka2a.TaskStatusUpdateEvent:
			kinds = append(kinds, "status:"+string(v.Status.State))
		case *sdka2a.TaskArtifactUpdateEvent:
			label := "artifact"
			if v.LastChunk {
				label += ":last"
			}
			if v.Append {
				label += ":append"
			}
			kinds = append(kinds, label)
		default:
			kinds = append(kinds, "other")
		}
		return true
	})
	return kinds
}

// TestExecute_完成路径 事件序列：task(submitted) → working → artifact → completed
func TestExecute_完成路径(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	var gotQuery string
	ch.OnMessage(func(msg *schema.Message) bool {
		var params map[string]any
		_ = json.Unmarshal(msg.Params, &params)
		gotQuery, _ = params["query"].(string)
		p := ch.pending[msg.ID]
		p.ch <- &schema.Message{
			ID: msg.ID, Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatDelta,
			Payload: map[string]any{"content": "hello"},
		}
		p.ch <- &schema.Message{
			ID: msg.ID, Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatFinal,
			Payload: map[string]any{"content": "done", "is_complete": true},
		}
		return true
	})

	ec := &a2asrv.ExecutorContext{
		TaskID:    "task-1",
		ContextID: "ctx-1",
		Message:   sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hi")),
		Metadata:  map[string]any{"trace": "t"},
	}
	kinds := collectEvents(executorFor(ch).Execute(context.Background(), ec))

	want := []string{
		"task:TASK_STATE_SUBMITTED",
		"status:TASK_STATE_WORKING",
		"artifact",
		// Python: append=artifact_started, last_chunk=is_terminal —— 终块同时 append=true
		"artifact:last:append",
		"status:TASK_STATE_COMPLETED",
	}
	if len(kinds) != len(want) {
		t.Fatalf("事件序列 = %v, 期望 %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Errorf("事件[%d] = %q, 期望 %q", i, kinds[i], want[i])
		}
	}
	if gotQuery != "hi" {
		t.Errorf("分发 query = %q, 期望 hi", gotQuery)
	}
	if _, ok := ch.pending["task-1"]; ok {
		t.Error("终态后 pending 应被清理")
	}
}

// TestExecute_空query直接失败 无可用 query 时直接 FAILED
func TestExecute_空query直接失败(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(*schema.Message) bool { return true })

	ec := &a2asrv.ExecutorContext{
		TaskID: "task-2", ContextID: "ctx-2",
		Message: sdka2a.NewMessage(sdka2a.MessageRoleUser),
	}
	kinds := collectEvents(executorFor(ch).Execute(context.Background(), ec))

	want := []string{"task:TASK_STATE_SUBMITTED", "status:TASK_STATE_FAILED"}
	if len(kinds) != len(want) {
		t.Fatalf("事件序列 = %v, 期望 %v", kinds, want)
	}
	if kinds[1] != want[1] {
		t.Errorf("终态 = %q, 期望 %q", kinds[1], want[1])
	}
}

// TestExecute_错误终态映射 CHAT_ERROR → FAILED
func TestExecute_错误终态映射(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(msg *schema.Message) bool {
		ch.pending[msg.ID].ch <- &schema.Message{
			ID: msg.ID, Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatError,
			Payload: map[string]any{"error": "boom"},
		}
		return true
	})
	ec := &a2asrv.ExecutorContext{
		TaskID: "task-3", ContextID: "ctx-3",
		Message: sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hi")),
	}
	kinds := collectEvents(executorFor(ch).Execute(context.Background(), ec))

	if last := kinds[len(kinds)-1]; last != "status:TASK_STATE_FAILED" {
		t.Errorf("终态 = %q, 期望 status:TASK_STATE_FAILED（序列 %v）", last, kinds)
	}
}

// TestExecute_中断终态映射 CHAT_INTERRUPT_RESULT → CANCELED
func TestExecute_中断终态映射(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(msg *schema.Message) bool {
		ch.pending[msg.ID].ch <- &schema.Message{
			ID: msg.ID, Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatInterruptResult,
			Payload: map[string]any{"content": "stopped"},
		}
		return true
	})
	ec := &a2asrv.ExecutorContext{
		TaskID: "task-4", ContextID: "ctx-4",
		Message: sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hi")),
	}
	kinds := collectEvents(executorFor(ch).Execute(context.Background(), ec))

	if last := kinds[len(kinds)-1]; last != "status:TASK_STATE_CANCELED" {
		t.Errorf("终态 = %q, 期望 status:TASK_STATE_CANCELED（序列 %v）", last, kinds)
	}
}

// TestCancel_产出取消状态 Cancel 序列末位为 CANCELED
func TestCancel_产出取消状态(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ec := &a2asrv.ExecutorContext{TaskID: "task-5", ContextID: "ctx-5"}
	kinds := collectEvents(executorFor(ch).Cancel(context.Background(), ec))

	if len(kinds) == 0 || kinds[len(kinds)-1] != "status:TASK_STATE_CANCELED" {
		t.Errorf("Cancel 事件序列 = %v", kinds)
	}
}
