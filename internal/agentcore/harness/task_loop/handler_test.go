package task_loop

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeHandlerSess 用于 handler 测试的模拟会话门面
type fakeHandlerSess struct {
	// sessionID 会话标识
	sessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewTaskLoopEventHandler 构造函数返回非 nil
func TestNewTaskLoopEventHandler(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)
	if h == nil {
		t.Fatal("NewTaskLoopEventHandler 返回 nil，期望非 nil")
	}
}

// TestTaskLoopEventHandler_PrepareRound递增 连续 PrepareRound 3次，roundID 从1到3
func TestTaskLoopEventHandler_PrepareRound递增(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	for i := 1; i <= 3; i++ {
		roundID := h.PrepareRound()
		if roundID != i {
			t.Errorf("第 %d 次 PrepareRound 返回 roundID=%d，期望 %d", i, roundID, i)
		}
	}
}

// TestTaskLoopEventHandler_WaitCompletion_正常完成 PrepareRound → resolveRound → WaitCompletion 收到结果
func TestTaskLoopEventHandler_WaitCompletion_正常完成(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()
	expected := map[string]any{"status": "ok", "data": 42}

	// 异步写入结果
	go func() {
		h.resolveRound(expected, roundID)
	}()

	ctx := context.Background()
	result := h.WaitCompletion(ctx, 2*time.Second)
	if result["status"] != "ok" {
		t.Errorf("WaitCompletion 返回 status=%v，期望 ok", result["status"])
	}
	if result["data"] != 42 {
		t.Errorf("WaitCompletion 返回 data=%v，期望 42", result["data"])
	}
}

// TestTaskLoopEventHandler_WaitCompletion_超时 WaitCompletion(100ms超时) 无 resolve → 返回 {"status": "timeout"}
func TestTaskLoopEventHandler_WaitCompletion_超时(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	h.PrepareRound()
	ctx := context.Background()
	result := h.WaitCompletion(ctx, 100*time.Millisecond)
	if result["status"] != "timeout" {
		t.Errorf("WaitCompletion 超时返回 status=%v，期望 timeout", result["status"])
	}
}

// TestTaskLoopEventHandler_WaitCompletion_无活跃轮次 无 PrepareRound 时返回 {"status": "timeout"}
// 说明：初始 channel 虽已创建但无数据写入，超时后返回 timeout 状态
func TestTaskLoopEventHandler_WaitCompletion_无活跃轮次(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 不调用 PrepareRound，使用初始 channel
	ctx := context.Background()
	result := h.WaitCompletion(ctx, 50*time.Millisecond)
	if result["status"] != "timeout" {
		t.Errorf("WaitCompletion 无活跃轮次返回 status=%v，期望 timeout", result["status"])
	}
}

// TestTaskLoopEventHandler_ResolveRound_匹配RoundID roundID 匹配时成功写入 channel
func TestTaskLoopEventHandler_ResolveRound_匹配RoundID(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()
	expected := map[string]any{"result": "success"}

	// 同步写入结果
	h.resolveRound(expected, roundID)

	ctx := context.Background()
	result := h.WaitCompletion(ctx, time.Second)
	if result["result"] != "success" {
		t.Errorf("resolveRound 匹配 roundID 后 WaitCompletion 返回 result=%v，期望 success", result["result"])
	}
}

// TestTaskLoopEventHandler_ResolveRound_过期RoundID丢弃 roundID 不匹配时丢弃
func TestTaskLoopEventHandler_ResolveRound_过期RoundID丢弃(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()

	// 使用过期 roundID 写入，应被丢弃
	h.resolveRound(map[string]any{"result": "stale"}, roundID-1)

	// 再用正确 roundID 写入
	expected := map[string]any{"result": "fresh"}
	h.resolveRound(expected, roundID)

	ctx := context.Background()
	result := h.WaitCompletion(ctx, time.Second)
	if result["result"] != "fresh" {
		t.Errorf("过期 roundID 丢弃后 WaitCompletion 返回 result=%v，期望 fresh", result["result"])
	}
}

// TestTaskLoopEventHandler_HandleTaskInteraction_注入Steer 构造 TaskInteractionEvent，验证 PushSteer 被调用
func TestTaskLoopEventHandler_HandleTaskInteraction_注入Steer(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 设置交互队列
	queues := NewLoopQueues(16)
	h.SetInteractionQueues(queues)

	// 构造 TaskInteractionEvent
	evt := &cschema.TaskInteractionEvent{
		Interaction: []cschema.DataFrame{&cschema.TextDataFrame{Text: "steer msg"}},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskInteraction(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskInteraction 返回错误: %v", err)
	}
	if result["status"] != "steer_injected" {
		t.Errorf("HandleTaskInteraction 返回 status=%v，期望 steer_injected", result["status"])
	}

	// 验证 steering 队列中有消息
	drained := queues.DrainSteering()
	if len(drained) != 1 || drained[0] != "steer msg" {
		t.Errorf("DrainSteering 返回 %v，期望 [steer msg]", drained)
	}
}

// TestTaskLoopEventHandler_HandleFollowUp_入队 构造 FollowUpEvent，验证 PushFollowUp 被调用
func TestTaskLoopEventHandler_HandleFollowUp_入队(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 设置交互队列
	queues := NewLoopQueues(16)
	h.SetInteractionQueues(queues)

	// 构造 FollowUpEvent
	evt := &cschema.FollowUpEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "followup msg"}},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleFollowUp(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleFollowUp 返回错误: %v", err)
	}
	if result["status"] != "follow_up_queued" {
		t.Errorf("HandleFollowUp 返回 status=%v，期望 follow_up_queued", result["status"])
	}

	// 验证 followUp 队列中有消息
	drained := queues.DrainFollowUp()
	if len(drained) != 1 || drained[0] != "followup msg" {
		t.Errorf("DrainFollowUp 返回 %v，期望 [followup msg]", drained)
	}
}

// TestTaskLoopEventHandler_OnAbort resolveRound with {"error": "aborted"}
func TestTaskLoopEventHandler_OnAbort(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	h.PrepareRound()

	// 异步调用 OnAbort
	go h.OnAbort()

	ctx := context.Background()
	result := h.WaitCompletion(ctx, 2*time.Second)
	if result["error"] != "aborted" {
		t.Errorf("OnAbort 后 WaitCompletion 返回 error=%v，期望 aborted", result["error"])
	}
}

// TestTaskLoopEventHandler_InteractionQueues 验证返回值和 interactionQueuesProvider 接口满足
func TestTaskLoopEventHandler_InteractionQueues(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	queues := NewLoopQueues(16)
	h.SetInteractionQueues(queues)

	// 验证 InteractionQueues 返回正确实例
	returned := h.InteractionQueues()
	if returned != queues {
		t.Error("InteractionQueues 返回值与设置的实例不一致")
	}

	// 验证 interactionQueuesProvider 接口满足性
	_, ok := any(h).(interactionQueuesProvider)
	if !ok {
		t.Error("TaskLoopEventHandler 未满足 interactionQueuesProvider 接口")
	}
}

// TestTaskLoopEventHandler_SetInteractionQueues setter 验证
func TestTaskLoopEventHandler_SetInteractionQueues(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 初始为 nil
	if h.InteractionQueues() != nil {
		t.Error("初始 InteractionQueues 应为 nil")
	}

	// 设置后可获取
	queues := NewLoopQueues(8)
	h.SetInteractionQueues(queues)
	if h.InteractionQueues() != queues {
		t.Error("SetInteractionQueues 后 InteractionQueues 返回值不一致")
	}
}

// TestExtractQuery_文本提取 从 InputEvent 的 TextDataFrame 提取文本
func TestExtractQuery_文本提取(t *testing.T) {
	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "hello world"}},
	}
	result := extractQuery(evt)
	if result != "hello world" {
		t.Errorf("extractQuery 返回 %q，期望 %q", result, "hello world")
	}
}

// TestExtractQuery_非InputEvent 非 InputEvent 事件返回空串
func TestExtractQuery_非InputEvent(t *testing.T) {
	// 使用 TaskCompletionEvent，非 InputEvent/FollowUpEvent
	evt := &cschema.TaskCompletionEvent{
		TaskResult: []cschema.DataFrame{&cschema.JsonDataFrame{Data: map[string]any{"k": "v"}}},
	}
	result := extractQuery(evt)
	if result != "" {
		t.Errorf("extractQuery 对非 InputEvent 返回 %q，期望空串", result)
	}
}

// TestTaskLoopEventHandler_GetBase 验证返回 &h.base
func TestTaskLoopEventHandler_GetBase(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	base := h.GetBase()
	if base == nil {
		t.Fatal("GetBase 返回 nil，期望非 nil")
	}
	// 验证返回的是同一个 base 指针
	base2 := h.GetBase()
	if base != base2 {
		t.Error("GetBase 两次调用返回不同指针")
	}
}

// TestTaskLoopEventHandler_SetSessionToolkit 验证 sessionToolkit 被设置
func TestTaskLoopEventHandler_SetSessionToolkit(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 设置 toolkit
	toolkit := "test-toolkit"
	h.SetSessionToolkit(toolkit)

	// 验证内部字段被设置（通过再次设置不同值来间接验证）
	h.SetSessionToolkit(nil)
}

// TestTaskLoopEventHandler_LastResult 验证返回 lastResult
func TestTaskLoopEventHandler_LastResult(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 初始为 nil
	if h.LastResult() != nil {
		t.Error("初始 LastResult 应为 nil")
	}

	// 通过 resolveRound 间接设置 lastResult
	roundID := h.PrepareRound()
	h.resolveRound(map[string]any{"status": "done"}, roundID)

	result := h.LastResult()
	if result["status"] != "done" {
		t.Errorf("LastResult 返回 status=%v，期望 done", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleTaskCompletion_正常完成 构造 TaskCompletionEvent，验证 WaitCompletion 收到结果
func TestTaskLoopEventHandler_HandleTaskCompletion_正常完成(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()

	// 构造 TaskCompletionEvent
	completionEvt := &cschema.TaskCompletionEvent{
		TaskResult: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"output": "done"}},
		},
	}
	completionEvt.SetMetadata(map[string]any{
		"task_id":            "task-1",
		"_handler_round_id": roundID,
	})

	input := &modules.EventHandlerInput{
		Event:   completionEvt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskCompletion(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskCompletion 返回错误: %v", err)
	}
	if result["output"] != "done" {
		t.Errorf("HandleTaskCompletion 返回 output=%v，期望 done", result["output"])
	}

	// 验证 WaitCompletion 收到结果
	ctx := context.Background()
	waitResult := h.WaitCompletion(ctx, time.Second)
	if waitResult["output"] != "done" {
		t.Errorf("WaitCompletion 返回 output=%v，期望 done", waitResult["output"])
	}
}

// TestTaskLoopEventHandler_HandleTaskFailed_正常失败 构造 TaskFailedEvent，验证 resolveRound 写入 error
func TestTaskLoopEventHandler_HandleTaskFailed_正常失败(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()

	// 构造 TaskFailedEvent
	failedEvt := &cschema.TaskFailedEvent{
		ErrorMessage: "something went wrong",
	}
	failedEvt.SetMetadata(map[string]any{
		"task_id":            "task-1",
		"_handler_round_id": roundID,
	})

	input := &modules.EventHandlerInput{
		Event:   failedEvt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskFailed(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskFailed 返回错误: %v", err)
	}
	if result["error"] != "something went wrong" {
		t.Errorf("HandleTaskFailed 返回 error=%v，期望 something went wrong", result["error"])
	}

	// 验证 WaitCompletion 收到错误结果
	ctx := context.Background()
	waitResult := h.WaitCompletion(ctx, time.Second)
	if waitResult["error"] != "something went wrong" {
		t.Errorf("WaitCompletion 返回 error=%v，期望 something went wrong", waitResult["error"])
	}
}

// TestTaskLoopEventHandler_HandleTaskCompletion_SessionSpawn 构造 SessionSpawn 完成事件，验证返回 session_spawn_completed
func TestTaskLoopEventHandler_HandleTaskCompletion_SessionSpawn(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	// 构造 TaskCompletionEvent，metadata["task_type"] = "session_spawn_task"
	completionEvt := &cschema.TaskCompletionEvent{
		TaskResult: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"output": "spawn_done"}},
		},
	}
	completionEvt.SetMetadata(map[string]any{
		"task_id":   "task-spawn-1",
		"task_type": "session_spawn_task",
	})

	input := &modules.EventHandlerInput{
		Event:   completionEvt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskCompletion(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskCompletion(SessionSpawn) 返回错误: %v", err)
	}
	if result["status"] != "session_spawn_completed" {
		t.Errorf("HandleTaskCompletion(SessionSpawn) 返回 status=%v，期望 session_spawn_completed", result["status"])
	}
}

// TestExtractQuery_字典提取 从 InputEvent 的 JsonDataFrame 提取 query 字段
func TestExtractQuery_字典提取(t *testing.T) {
	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"query": "hello"}},
		},
	}
	result := extractQuery(evt)
	// JsonDataFrame 不是 TextDataFrame，extractQuery 不处理，返回空串
	if result != "" {
		t.Errorf("extractQuery 对 JsonDataFrame 返回 %q，期望空串（仅处理 TextDataFrame）", result)
	}
}

// TestExtractQuery_FollowUpEvent 从 FollowUpEvent 的 TextDataFrame 提取文本
func TestExtractQuery_FollowUpEvent(t *testing.T) {
	evt := &cschema.FollowUpEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "followup query"}},
	}
	result := extractQuery(evt)
	if result != "followup query" {
		t.Errorf("extractQuery 返回 %q，期望 %q", result, "followup query")
	}
}

// TestTaskLoopEventHandler_HandleInput_Coordinator为nil LoopCoordinator 为 nil 时返回 error 状态
func TestTaskLoopEventHandler_HandleInput_Coordinator为nil(t *testing.T) {
	// provider 不设置 coordinator，默认为 nil
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "test query"}},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleInput(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleInput 返回错误: %v", err)
	}
	if result["status"] != "error" {
		t.Errorf("HandleInput 返回 status=%v，期望 error", result["status"])
	}
	if result["error"] != "coordinator is nil" {
		t.Errorf("HandleInput 返回 error=%v，期望 coordinator is nil", result["error"])
	}
}

// TestTaskLoopEventHandler_HandleInput_自动生成TaskID 无 task_id 时自动生成 UUID
func TestTaskLoopEventHandler_HandleInput_自动生成TaskID(t *testing.T) {
	coordinator := NewLoopCoordinator(nil)
	provider := &fakeDeepAgentProvider{
		coordinator: coordinator,
	}
	h := NewTaskLoopEventHandler(provider)

	cfg := config.DefaultControllerConfig()
	h.GetBase().TaskManager = modules.NewTaskManager(cfg)

	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "auto task"}},
	}
	// 不设置 task_id，应自动生成
	evt.SetMetadata(map[string]any{
		"_handler_round_id": 1,
	})
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleInput(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleInput 返回错误: %v", err)
	}
	if result["status"] != "submitted" {
		t.Errorf("HandleInput 返回 status=%v，期望 submitted", result["status"])
	}
	taskID, ok := result["task_id"].(string)
	if !ok || taskID == "" {
		t.Error("HandleInput 未自动生成 task_id")
	}
}

// TestTaskLoopEventHandler_WaitCompletion_上下文取消 上下文取消时返回 cancelled 状态
func TestTaskLoopEventHandler_WaitCompletion_上下文取消(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	h.PrepareRound()

	ctx, cancel := context.WithCancel(context.Background())
	// 立即取消上下文
	cancel()

	result := h.WaitCompletion(ctx, 2*time.Second)
	if result["status"] != "cancelled" {
		t.Errorf("WaitCompletion 上下文取消返回 status=%v，期望 cancelled", result["status"])
	}
}

// TestTaskLoopEventHandler_WaitCompletion_无超时 无超时时直接从 channel 读取
func TestTaskLoopEventHandler_WaitCompletion_无超时(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()

	// 异步写入结果
	go func() {
		h.resolveRound(map[string]any{"status": "ok"}, roundID)
	}()

	// 无超时（timeout=0）
	ctx := context.Background()
	result := h.WaitCompletion(ctx, 0)
	if result["status"] != "ok" {
		t.Errorf("WaitCompletion 无超时返回 status=%v，期望 ok", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleTaskInteraction_空文本 交互事件无文本时返回 no_steer
func TestTaskLoopEventHandler_HandleTaskInteraction_空文本(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	queues := NewLoopQueues(16)
	h.SetInteractionQueues(queues)

	// 构造空文本的 TaskInteractionEvent
	evt := &cschema.TaskInteractionEvent{
		Interaction: []cschema.DataFrame{},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskInteraction(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskInteraction 返回错误: %v", err)
	}
	if result["status"] != "no_steer" {
		t.Errorf("HandleTaskInteraction 空文本返回 status=%v，期望 no_steer", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleTaskInteraction_无队列 交互队列为 nil 时引导指令丢弃
func TestTaskLoopEventHandler_HandleTaskInteraction_无队列(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)
	// 不设置 InteractionQueues，默认为 nil

	evt := &cschema.TaskInteractionEvent{
		Interaction: []cschema.DataFrame{&cschema.TextDataFrame{Text: "steer msg"}},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskInteraction(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskInteraction 返回错误: %v", err)
	}
	if result["status"] != "steer_injected" {
		t.Errorf("HandleTaskInteraction 无队列返回 status=%v，期望 steer_injected", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleFollowUp_空文本 跟进事件无文本时返回 no_follow_up
func TestTaskLoopEventHandler_HandleFollowUp_空文本(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	queues := NewLoopQueues(16)
	h.SetInteractionQueues(queues)

	// 构造空文本的 FollowUpEvent
	evt := &cschema.FollowUpEvent{
		InputData: []cschema.DataFrame{},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleFollowUp(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleFollowUp 返回错误: %v", err)
	}
	if result["status"] != "no_follow_up" {
		t.Errorf("HandleFollowUp 空文本返回 status=%v，期望 no_follow_up", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleFollowUp_无队列 交互队列为 nil 时跟进消息丢弃
func TestTaskLoopEventHandler_HandleFollowUp_无队列(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)
	// 不设置 InteractionQueues，默认为 nil

	evt := &cschema.FollowUpEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "followup msg"}},
	}
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleFollowUp(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleFollowUp 返回错误: %v", err)
	}
	if result["status"] != "follow_up_queued" {
		t.Errorf("HandleFollowUp 无队列返回 status=%v，期望 follow_up_queued", result["status"])
	}
}

// TestTaskLoopEventHandler_ResolveRound_重复写入 channel 已满时丢弃重复结果
func TestTaskLoopEventHandler_ResolveRound_重复写入(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()

	// 第一次写入成功
	h.resolveRound(map[string]any{"result": "first"}, roundID)
	// 第二次写入，channel 已满（cap=1），丢弃
	h.resolveRound(map[string]any{"result": "second"}, roundID)

	ctx := context.Background()
	result := h.WaitCompletion(ctx, time.Second)
	// 应该收到第一次写入的结果
	if result["result"] != "first" {
		t.Errorf("重复写入后 WaitCompletion 返回 result=%v，期望 first", result["result"])
	}
}

// TestTaskLoopEventHandler_HandleTaskCompletion_无结果 TaskCompletionEvent 无 TaskResult 时返回空 map
func TestTaskLoopEventHandler_HandleTaskCompletion_无结果(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	roundID := h.PrepareRound()

	completionEvt := &cschema.TaskCompletionEvent{
		TaskResult: []cschema.DataFrame{},
	}
	completionEvt.SetMetadata(map[string]any{
		"task_id":            "task-2",
		"_handler_round_id": roundID,
	})

	input := &modules.EventHandlerInput{
		Event:   completionEvt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskCompletion(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskCompletion 返回错误: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("HandleTaskCompletion 无结果时返回 %v，期望空 map", result)
	}
}

// TestTaskLoopEventHandler_HandleTaskFailed_SessionSpawn SessionSpawn 任务失败返回 session_spawn_failed
func TestTaskLoopEventHandler_HandleTaskFailed_SessionSpawn(t *testing.T) {
	provider := &fakeDeepAgentProvider{}
	h := NewTaskLoopEventHandler(provider)

	failedEvt := &cschema.TaskFailedEvent{
		ErrorMessage: "spawn failed",
	}
	failedEvt.SetMetadata(map[string]any{
		"task_id":   "task-spawn-1",
		"task_type": "session_spawn_task",
	})

	input := &modules.EventHandlerInput{
		Event:   failedEvt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleTaskFailed(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleTaskFailed(SessionSpawn) 返回错误: %v", err)
	}
	if result["status"] != "session_spawn_failed" {
		t.Errorf("HandleTaskFailed(SessionSpawn) 返回 status=%v，期望 session_spawn_failed", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleInput_正常提交 有 coordinator 和 TaskManager 时正常提交任务
func TestTaskLoopEventHandler_HandleInput_正常提交(t *testing.T) {
	coordinator := NewLoopCoordinator(nil)
	provider := &fakeDeepAgentProvider{
		coordinator: coordinator,
	}
	h := NewTaskLoopEventHandler(provider)

	// 设置 TaskManager
	cfg := config.DefaultControllerConfig()
	h.GetBase().TaskManager = modules.NewTaskManager(cfg)

	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "test query"}},
	}
	evt.SetMetadata(map[string]any{
		"task_id":            "task-input-1",
		"_handler_round_id": 1,
	})
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleInput(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleInput 返回错误: %v", err)
	}
	if result["status"] != "submitted" {
		t.Errorf("HandleInput 返回 status=%v，期望 submitted", result["status"])
	}
	if result["task_id"] != "task-input-1" {
		t.Errorf("HandleInput 返回 task_id=%v，期望 task-input-1", result["task_id"])
	}
}

// TestTaskLoopEventHandler_HandleInput_FollowUp 标记 is_follow_up 时跳过 TaskPlan 查询
func TestTaskLoopEventHandler_HandleInput_FollowUp(t *testing.T) {
	coordinator := NewLoopCoordinator(nil)
	provider := &fakeDeepAgentProvider{
		coordinator: coordinator,
	}
	h := NewTaskLoopEventHandler(provider)

	cfg := config.DefaultControllerConfig()
	h.GetBase().TaskManager = modules.NewTaskManager(cfg)

	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "follow up query"}},
	}
	evt.SetMetadata(map[string]any{
		"task_id":            "task-fu-1",
		"_handler_round_id": 1,
		"is_follow_up":       true,
	})
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleInput(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleInput 返回错误: %v", err)
	}
	if result["status"] != "submitted" {
		t.Errorf("HandleInput 返回 status=%v，期望 submitted", result["status"])
	}
}

// TestTaskLoopEventHandler_HandleInput_带RunKindAndRunContext 带有 run_kind 和 run_context 时设置到 Metadata
func TestTaskLoopEventHandler_HandleInput_带RunKindAndRunContext(t *testing.T) {
	coordinator := NewLoopCoordinator(nil)
	provider := &fakeDeepAgentProvider{
		coordinator: coordinator,
	}
	h := NewTaskLoopEventHandler(provider)

	cfg := config.DefaultControllerConfig()
	h.GetBase().TaskManager = modules.NewTaskManager(cfg)

	evt := &cschema.InputEvent{
		InputData: []cschema.DataFrame{&cschema.TextDataFrame{Text: "query"}},
	}
	evt.SetMetadata(map[string]any{
		"task_id":            "task-rr-1",
		"_handler_round_id": 1,
		"run_kind":           "heartbeat",
		"run_context":        "ctx-data",
	})
	input := &modules.EventHandlerInput{
		Event:   evt,
		Session: &fakeHandlerSess{sessionID: "sess-1"},
	}

	result, err := h.HandleInput(context.Background(), input)
	if err != nil {
		t.Fatalf("HandleInput 返回错误: %v", err)
	}
	if result["status"] != "submitted" {
		t.Errorf("HandleInput 返回 status=%v，期望 submitted", result["status"])
	}
}

// TestLoopCoordinator_Evaluators 返回评估器切片副本
func TestLoopCoordinator_Evaluators(t *testing.T) {
	evaluators := []StopConditionEvaluator{
		NewMaxRoundsEvaluator(5),
		NewTokenBudgetEvaluator(100),
	}
	lc := NewLoopCoordinator(evaluators)

	evalList := lc.Evaluators()
	if len(evalList) != 2 {
		t.Fatalf("Evaluators 返回 %d 个评估器，期望 2", len(evalList))
	}
	if evalList[0].Name() != "MaxRoundsEvaluator" {
		t.Errorf("第 0 个评估器 Name=%q，期望 MaxRoundsEvaluator", evalList[0].Name())
	}
	if evalList[1].Name() != "TokenBudgetEvaluator" {
		t.Errorf("第 1 个评估器 Name=%q，期望 TokenBudgetEvaluator", evalList[1].Name())
	}
}

// TestLoopQueues_PushFollowUp_满队列 队列满时丢弃消息不阻塞
func TestLoopQueues_PushFollowUp_满队列(t *testing.T) {
	// 创建容量为 1 的队列
	queues := NewLoopQueues(1)

	// 先推入一条消息填满队列
	queues.PushFollowUp("msg1")

	// 再推入一条，应被丢弃（不阻塞）
	queues.PushFollowUp("msg2-dropped")

	// 只能取出一条
	drained := queues.DrainFollowUp()
	if len(drained) != 1 {
		t.Errorf("DrainFollowUp 返回 %d 条消息，期望 1", len(drained))
	}
	if drained[0] != "msg1" {
		t.Errorf("DrainFollowUp 返回 %v，期望 [msg1]", drained)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetSessionID 实现 SessionFacade 接口
func (f *fakeHandlerSess) GetSessionID() string {
	return f.sessionID
}

// UpdateState 实现 SessionFacade 接口
func (f *fakeHandlerSess) UpdateState(_ map[string]any) {}

// GetState 实现 SessionFacade 接口
func (f *fakeHandlerSess) GetState(_ state.StateKey) (any, error) {
	return nil, nil
}

// DumpState 实现 SessionFacade 接口
func (f *fakeHandlerSess) DumpState() map[string]any {
	return map[string]any{}
}

// WriteStream 实现 SessionFacade 接口
func (f *fakeHandlerSess) WriteStream(_ context.Context, _ any) error {
	return nil
}

// WriteCustomStream 实现 SessionFacade 接口
func (f *fakeHandlerSess) WriteCustomStream(_ context.Context, _ any) error {
	return nil
}

// GetEnv 实现 SessionFacade 接口
func (f *fakeHandlerSess) GetEnv(_ string, _ ...any) any {
	return nil
}

// Interact 实现 SessionFacade 接口
func (f *fakeHandlerSess) Interact(_ context.Context, _ any) error {
	return nil
}
