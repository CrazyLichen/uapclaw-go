package rails

import (
	"context"
	"fmt"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 辅助类型 ────────────────────────────

// fakeSession 用于测试的模拟 Session
type fakeSession struct {
	written []any
	err     error
}

func (s *fakeSession) GetSessionID() string                                       { return "test-session" }
func (s *fakeSession) UpdateState(map[string]any)                                  {}
func (s *fakeSession) WriteStream(_ context.Context, data any) error              { s.written = append(s.written, data); return s.err }
func (s *fakeSession) WriteCustomStream(_ context.Context, data any) error        { return nil }
func (s *fakeSession) GetState(_ state.StateKey) (any, error)                           { return nil, nil }
func (s *fakeSession) DumpState() map[string]any                                  { return map[string]any{} }
func (s *fakeSession) GetEnv(_ string, _ ...any) any                              { return nil }
func (s *fakeSession) Interact(_ context.Context, _ any) error                    { return nil }

// lastOutput 获取最后写入的 OutputSchema
func (s *fakeSession) lastOutput() *stream.OutputSchema {
	if len(s.written) == 0 {
		return nil
	}
	if o, ok := s.written[len(s.written)-1].(*stream.OutputSchema); ok {
		return o
	}
	// 也接受值类型
	if o, ok := s.written[len(s.written)-1].(stream.OutputSchema); ok {
		return &o
	}
	return nil
}

// fakeTodoLoader 用于测试的模拟 TodoTool
type fakeTodoLoader struct {
	todos []hschema.TodoItem
	err   error
}

func (t *fakeTodoLoader) LoadTodos(_ context.Context, _ string) ([]hschema.TodoItem, error) {
	return t.todos, t.err
}

// ──────────────────────────── emitToolCall ────────────────────────────

func TestEmitToolCall_正常发送(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-1", "read_file", `{"path": "/tmp"}`)

	emitToolCall(context.Background(), sess, tc)

	out := sess.lastOutput()
	if out == nil {
		t.Fatal("期望写入 OutputSchema，实际为 nil")
	}
	if out.Type != "tool_call" {
		t.Errorf("Type = %q, want %q", out.Type, "tool_call")
	}
	payload, ok := out.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload 类型错误: %T", out.Payload)
	}
	toolCallMap, ok := payload["tool_call"].(map[string]any)
	if !ok {
		t.Fatalf("tool_call 类型错误: %T", payload["tool_call"])
	}
	if toolCallMap["name"] != "read_file" {
		t.Errorf("name = %q, want %q", toolCallMap["name"], "read_file")
	}
	if toolCallMap["tool_call_id"] != "tc-1" {
		t.Errorf("tool_call_id = %q, want %q", toolCallMap["tool_call_id"], "tc-1")
	}
}

func TestEmitToolCall_NilSession(t *testing.T) {
	tc := llmschema.NewToolCall("tc-1", "read_file", `{}`)
	// 不 panic 即通过
	emitToolCall(context.Background(), nil, tc)
}

func TestEmitToolCall_NilToolCall(t *testing.T) {
	sess := &fakeSession{}
	emitToolCall(context.Background(), sess, nil)

	out := sess.lastOutput()
	if out == nil {
		t.Fatal("期望写入 OutputSchema，即使 tool_call 为 nil")
	}
	payload, _ := out.Payload.(map[string]any)
	toolCallMap, _ := payload["tool_call"].(map[string]any)
	if toolCallMap["name"] != "" {
		t.Errorf("name 应为空, got %q", toolCallMap["name"])
	}
}

// ──────────────────────────── emitToolResult ────────────────────────────

func TestEmitToolResult_正常结果(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-2", "write_file", `{}`)

	emitToolResult(context.Background(), sess, tc, "file written")

	out := sess.lastOutput()
	if out.Type != "tool_result" {
		t.Errorf("Type = %q, want %q", out.Type, "tool_result")
	}
	payload, _ := out.Payload.(map[string]any)
	toolResultMap, _ := payload["tool_result"].(map[string]any)
	if toolResultMap["tool_name"] != "write_file" {
		t.Errorf("tool_name = %q, want %q", toolResultMap["tool_name"], "write_file")
	}
}

func TestEmitToolResult_结构化输出(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-3", "search", `{}`)
	result := map[string]any{"data": []any{"a", "b"}, "success": true}

	emitToolResult(context.Background(), sess, tc, result)

	out := sess.lastOutput()
	payload, _ := out.Payload.(map[string]any)
	toolResultMap, _ := payload["tool_result"].(map[string]any)
	if _, ok := toolResultMap["raw_output"]; !ok {
		t.Error("期望 raw_output 字段")
	}
}

func TestEmitToolResult_错误状态(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-4", "exec", `{}`)
	result := map[string]any{"success": false, "error": "failed"}

	emitToolResult(context.Background(), sess, tc, result)

	out := sess.lastOutput()
	payload, _ := out.Payload.(map[string]any)
	toolResultMap, _ := payload["tool_result"].(map[string]any)
	if toolResultMap["success"] != false {
		t.Errorf("success = %v, want false", toolResultMap["success"])
	}
	if toolResultMap["status"] != "error" {
		t.Errorf("status = %q, want %q", toolResultMap["status"], "error")
	}
}

func TestEmitToolResult_NilSession(t *testing.T) {
	tc := llmschema.NewToolCall("tc-5", "test", `{}`)
	// 不 panic 即通过
	emitToolResult(context.Background(), nil, tc, "result")
}

// ──────────────────────────── emitToolUpdate ────────────────────────────

func TestEmitToolUpdate_正常更新(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-6", "read_file", `{}`)

	emitToolUpdate(context.Background(), sess, tc, "in_progress")

	out := sess.lastOutput()
	if out.Type != "tool_update" {
		t.Errorf("Type = %q, want %q", out.Type, "tool_update")
	}
	payload, _ := out.Payload.(map[string]any)
	toolUpdateMap, _ := payload["tool_update"].(map[string]any)
	if toolUpdateMap["status"] != "in_progress" {
		t.Errorf("status = %q, want %q", toolUpdateMap["status"], "in_progress")
	}
}

func TestEmitToolUpdate_空状态默认inProgress(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-7", "read_file", `{}`)

	emitToolUpdate(context.Background(), sess, tc, "")

	out := sess.lastOutput()
	payload, _ := out.Payload.(map[string]any)
	toolUpdateMap, _ := payload["tool_update"].(map[string]any)
	if toolUpdateMap["status"] != "in_progress" {
		t.Errorf("status = %q, want %q (空状态默认 in_progress)", toolUpdateMap["status"], "in_progress")
	}
}

func TestEmitToolUpdate_NilSession(t *testing.T) {
	tc := llmschema.NewToolCall("tc-8", "test", `{}`)
	emitToolUpdate(context.Background(), nil, tc, "completed")
}

// ──────────────────────────── emitAskUserQuestionIfInterrupted ────────────────────────────

func TestEmitAskUserQuestionIfInterrupted_非AskUser工具(t *testing.T) {
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-9", "read_file", `{}`)

	emitAskUserQuestionIfInterrupted(context.Background(), sess, tc, "read_file", nil, nil)

	if len(sess.written) != 0 {
		t.Error("非 ask_user 工具不应发送事件")
	}
}

func TestEmitAskUserQuestionIfInterrupted_NilSession(t *testing.T) {
	tc := llmschema.NewToolCall("tc-10", "ask_user", `{}`)
	// 不 panic 即通过
	emitAskUserQuestionIfInterrupted(context.Background(), nil, tc, "ask_user", nil, nil)
}

// ──────────────────────────── emitTodoUpdated ────────────────────────────

func TestEmitTodoUpdated_正常发送(t *testing.T) {
	sess := &fakeSession{}
	loader := &fakeTodoLoader{
		todos: []hschema.TodoItem{
			{ID: "1", Content: "任务1", ActiveForm: "执行任务1", Status: hschema.TodoStatusPending},
			{ID: "2", Content: "任务2", ActiveForm: "执行任务2", Status: hschema.TodoStatusCompleted},
			{ID: "3", Content: "任务3", ActiveForm: "执行任务3", Status: hschema.TodoStatusCancelled},
		},
	}

	emitTodoUpdated(context.Background(), sess, loader, "session1")

	out := sess.lastOutput()
	if out.Type != "todo.updated" {
		t.Errorf("Type = %q, want %q", out.Type, "todo.updated")
	}
	payload, _ := out.Payload.(map[string]any)
	todos, ok := payload["todos"].([]map[string]any)
	if !ok {
		t.Fatalf("todos 类型错误: %T", payload["todos"])
	}
	// 已取消项应被过滤
	if len(todos) != 2 {
		t.Errorf("todos 数量 = %d, want 2（取消项已过滤）", len(todos))
	}
	// 检查状态映射
	if todos[0]["status"] != "pending" {
		t.Errorf("todos[0] status = %q, want %q", todos[0]["status"], "pending")
	}
	if todos[1]["status"] != "completed" {
		t.Errorf("todos[1] status = %q, want %q", todos[1]["status"], "completed")
	}
}

func TestEmitTodoUpdated_LoadTodos失败(t *testing.T) {
	sess := &fakeSession{}
	loader := &fakeTodoLoader{err: fmt.Errorf("file not found")}

	emitTodoUpdated(context.Background(), sess, loader, "session1")

	if len(sess.written) != 0 {
		t.Error("LoadTodos 失败时不应发送事件")
	}
}

func TestEmitTodoUpdated_NilSession(t *testing.T) {
	loader := &fakeTodoLoader{todos: []hschema.TodoItem{}}
	emitTodoUpdated(context.Background(), nil, loader, "session1")
}

func TestEmitTodoUpdated_NilLoader(t *testing.T) {
	sess := &fakeSession{}
	emitTodoUpdated(context.Background(), sess, nil, "session1")
	if len(sess.written) != 0 {
		t.Error("nil loader 不应发送事件")
	}
}

// ──────────────────────────── emitContextUsage ────────────────────────────

func TestEmitContextUsage_NilSession(t *testing.T) {
	cbCtx := &sainterfaces.AgentCallbackContext{}
	emitContextUsage(context.Background(), cbCtx)
	// 不 panic 即通过
}

// ──────────────────────────── formatTodosForFrontend ────────────────────────────

func TestFormatTodosForFrontend_状态映射(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "1", Content: "任务1", ActiveForm: "执行中", Status: hschema.TodoStatusInProgress},
		{ID: "2", Content: "任务2", ActiveForm: "已完成", Status: hschema.TodoStatusCompleted},
		{ID: "3", Content: "任务3", ActiveForm: "已取消", Status: hschema.TodoStatusCancelled},
	}
	result := formatTodosForFrontend(todos)

	if len(result) != 2 {
		t.Errorf("结果数量 = %d, want 2（取消项已过滤）", len(result))
	}
	if result[0]["status"] != "in_progress" {
		t.Errorf("status = %q, want %q", result[0]["status"], "in_progress")
	}
}

func TestFormatTodosForFrontend_空列表(t *testing.T) {
	result := formatTodosForFrontend([]hschema.TodoItem{})
	if len(result) != 0 {
		t.Errorf("结果数量 = %d, want 0", len(result))
	}
}

func TestFormatTodosForFrontend_全部取消(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "1", Content: "任务1", ActiveForm: "取消", Status: hschema.TodoStatusCancelled},
	}
	result := formatTodosForFrontend(todos)
	if len(result) != 0 {
		t.Errorf("结果数量 = %d, want 0（全部取消）", len(result))
	}
}

// 确认 fakeSession 实现了 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*fakeSession)(nil)
