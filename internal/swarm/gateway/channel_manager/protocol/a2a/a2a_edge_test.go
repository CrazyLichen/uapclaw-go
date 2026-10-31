package a2a

import (
	"context"
	"testing"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestConfig_返回配置 Config() 返回构造时配置
func TestConfig_返回配置(t *testing.T) {
	cfg := testConfig(19100)
	ch := NewA2AChannel(cfg)
	got, ok := ch.Config().(A2AChannelConfig)
	if !ok || got.Port != 19100 || got.ChannelID != "a2a" {
		t.Errorf("Config() = %#v", ch.Config())
	}
}

// TestSend_队列满丢弃 队列满时不阻塞、不报错（偏差 D11）
func TestSend_队列满丢弃(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(*schema.Message) bool { return true })
	pending, err := ch.DispatchA2ARequest(context.Background(), "req-full", "sess", "hi", nil, nil)
	if err != nil {
		t.Fatalf("dispatch 失败: %v", err)
	}
	msg := &schema.Message{ID: "req-full"}
	for i := 0; i < pendingBufferSize+2; i++ {
		if err := ch.Send(context.Background(), msg); err != nil {
			t.Fatalf("Send 第 %d 次失败: %v", i, err)
		}
	}
	if got := len(pending.ch); got != pendingBufferSize {
		t.Errorf("队列长度 = %d, 期望上限 %d", got, pendingBufferSize)
	}
}

// TestExecute_ctx取消中途退出 ctx 取消时 Execute 立即返回
func TestExecute_ctx取消中途退出(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ch.OnMessage(func(*schema.Message) bool { return true }) // 不回包，让 Execute 停在校验后

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ec := &a2asrv.ExecutorContext{
		TaskID: "task-cancel", ContextID: "ctx-cancel",
		Message: sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hi")),
	}
	kinds := collectEvents(executorFor(ch).Execute(ctx, ec))
	// 已在运行前取消：不发 artifact，也不发终态
	for _, k := range kinds {
		if k != "task:TASK_STATE_SUBMITTED" && k != "status:TASK_STATE_WORKING" {
			t.Errorf("不应出现 %q（序列 %v）", k, kinds)
		}
	}
}

// TestExecute_分发失败转FAILED 未注册回调时 dispatch 失败 → FAILED
func TestExecute_分发失败转FAILED(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	// 故意不设置 OnMessage
	ec := &a2asrv.ExecutorContext{
		TaskID: "task-nocb", ContextID: "ctx-nocb",
		Message: sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hi")),
	}
	kinds := collectEvents(executorFor(ch).Execute(context.Background(), ec))
	if last := kinds[len(kinds)-1]; last != "status:TASK_STATE_FAILED" {
		t.Errorf("终态 = %q, 期望 status:TASK_STATE_FAILED（序列 %v）", last, kinds)
	}
}

// TestCancel_无存储任务且带消息 先补 Task 再 CANCELED
func TestCancel_无存储任务且带消息(t *testing.T) {
	ch := NewA2AChannel(A2AChannelConfig{Enabled: false, ChannelID: "a2a"})
	ec := &a2asrv.ExecutorContext{
		TaskID: "task-c1", ContextID: "ctx-c1",
		Message: sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart("hi")),
	}
	kinds := collectEvents(executorFor(ch).Cancel(context.Background(), ec))
	want := []string{"task:TASK_STATE_SUBMITTED", "status:TASK_STATE_CANCELED"}
	if len(kinds) != len(want) || kinds[0] != want[0] || kinds[1] != want[1] {
		t.Errorf("Cancel 序列 = %v, 期望 %v", kinds, want)
	}
}

// TestFilterSentinelParts_含nil元素 nil 元素被跳过
func TestFilterSentinelParts_含nil元素(t *testing.T) {
	parts := []*sdka2a.Part{nil, sdka2a.NewTextPart("ok"), sdka2a.NewTextPart(`{"is_complete":true}`)}
	got := filterSentinelParts(parts)
	if len(got) != 1 || got[0].Text() != "ok" {
		t.Errorf("filterSentinelParts = %v", got)
	}
	if got := filterSentinelParts(nil); got != nil {
		t.Errorf("空入参应返回 nil, 实际 %v", got)
	}
}

// TestJoinTextParts_边界 nil 消息与空 parts
func TestJoinTextParts_边界(t *testing.T) {
	if got := joinTextParts(nil); got != "" {
		t.Errorf("nil 消息 = %q", got)
	}
	if got := joinTextParts(&sdka2a.Message{}); got != "" {
		t.Errorf("空 parts = %q", got)
	}
	if got := joinTextParts(&sdka2a.Message{Parts: sdka2a.ContentParts{nil, sdka2a.NewTextPart("x")}}); got != "x" {
		t.Errorf("含 nil Part = %q, 期望 x", got)
	}
}

// TestMessageToA2AParts_标量与无名工具 content 为标量、工具无名分支
func TestMessageToA2AParts_标量与无名工具(t *testing.T) {
	// content 为数字 → strVal 走默认分支
	parts := MessageToA2AParts(&schema.Message{Payload: map[string]any{"content": float64(42)}}, false)
	if len(parts) != 1 || parts[0].Text() != "42" {
		t.Errorf("标量 content parts = %v", parts)
	}

	// tool_call 无 name → "tool"
	callParts := MessageToA2AParts(&schema.Message{
		EventType: schema.EventTypeChatToolCall,
		Payload:   map[string]any{"tool_call": map[string]any{}},
	}, false)
	if len(callParts) != 1 || callParts[0].Text() != "[tool_call] tool" {
		t.Errorf("无名工具 parts = %v", callParts)
	}

	// tool_result 无 tool_name → "[tool_result] "
	resultParts := MessageToA2AParts(&schema.Message{
		EventType: schema.EventTypeChatToolResult,
		Payload:   map[string]any{"result": "ok"},
	}, false)
	if len(resultParts) != 2 || resultParts[1].Text() != "[tool_result] ok" {
		t.Errorf("无名 tool_result parts = %v", resultParts)
	}
}

// TestFilesToParts_raw分支 base64 可解与不可解两种
func TestFilesToParts_raw分支(t *testing.T) {
	msg := &schema.Message{Payload: map[string]any{"files": []any{
		map[string]any{"filename": "r.bin", "raw": "aGk="},
		map[string]any{"filename": "r2.bin", "raw": "not-base64!!"},
	}}}
	parts := MessageToA2AParts(msg, false)
	if len(parts) != 2 {
		t.Fatalf("parts 数量 = %d, 期望 2", len(parts))
	}
	if string(parts[0].Raw()) != "hi" {
		t.Errorf("base64 解码 = %q, 期望 hi", string(parts[0].Raw()))
	}
	if string(parts[1].Raw()) != "not-base64!!" {
		t.Errorf("非法 base64 应退化为原始字节, 实际 %q", string(parts[1].Raw()))
	}
}

// TestMapA2APartsToParams_nilPart 含 nil Part 时跳过
func TestMapA2APartsToParams_nilPart(t *testing.T) {
	query, files := MapA2APartsToParams(&sdka2a.Message{
		Parts: sdka2a.ContentParts{nil, sdka2a.NewTextPart("x")},
	})
	if query != "x" || len(files) != 0 {
		t.Errorf("query=%q files=%v", query, files)
	}
}
