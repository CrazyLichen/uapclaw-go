package rails

import (
	"context"
	"sync"
	"testing"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 新建 ────────────────────────────

func TestNewJiuClawStreamEventRail(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	if rail == nil {
		t.Fatal("NewJiuClawStreamEventRail 返回 nil")
	}
	if rail.Priority() != 80 {
		t.Errorf("Priority = %d, want 80", rail.Priority())
	}
}

// ──────────────────────────── 暂停/恢复/终止 ────────────────────────────

func TestJiuClawStreamEventRail_PauseResume(t *testing.T) {
	rail := NewJiuClawStreamEventRail()

	// 暂停
	rail.Pause("session-1")

	// 在另一个 goroutine 中恢复
	done := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		rail.Resume("session-1")
		close(done)
	}()

	// checkpointWait 应阻塞直到 Resume
	start := time.Now()
	rail.checkpointWait(context.Background(), "session-1")
	elapsed := time.Since(start)

	if elapsed < 40*time.Millisecond {
		t.Error("checkpointWait 应该阻塞直到 Resume")
	}
	<-done
}

func TestJiuClawStreamEventRail_Abort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()

	// 暂停后终止
	rail.Pause("session-1")
	rail.Abort("session-1")

	// Abort 应同时 Resume 暂停，以便 checkpoint 可以检查 abort 标记
	if !rail.isAbortRequested("session-1") {
		t.Error("abort 标记应已设置")
	}
	// checkpointWait 不应阻塞（Abort 会 Resume）
	rail.checkpointWait(context.Background(), "session-1")
}

func TestJiuClawStreamEventRail_CheckpointWait_Context取消(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.Pause("session-1")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		rail.checkpointWait(ctx, "session-1")
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// context 取消后应退出等待
	case <-time.After(200 * time.Millisecond):
		t.Error("context 取消后 checkpointWait 应返回")
	}
}

func TestJiuClawStreamEventRail_ResetAbort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.Abort("session-1")
	rail.ResetAbort("session-1")

	if rail.isAbortRequested("session-1") {
		t.Error("reset_abort 应清除终止标记")
	}
}

func TestJiuClawStreamEventRail_ResetForNewTask(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.Pause("session-1")
	rail.mu.Lock()
	rail.conversationIDs["session-1"] = "conv-1"
	rail.mainSessions["session-1"] = &fakeSession{}
	rail.mu.Unlock()

	rail.ResetForNewTask("session-1")

	// 应恢复暂停
	rail.checkpointWait(context.Background(), "session-1") // 不应阻塞

	// 应清除 conversation_id 和 session
	rail.mu.Lock()
	_, hasConvID := rail.conversationIDs["session-1"]
	_, hasSession := rail.mainSessions["session-1"]
	rail.mu.Unlock()
	if hasConvID {
		t.Error("reset_for_new_task 应清除 conversation_id")
	}
	if hasSession {
		t.Error("reset_for_new_task 应清除 main_session")
	}

	// 但不应清除 abort 标记
	rail.Abort("session-1")
	rail.ResetForNewTask("session-1")
	if !rail.isAbortRequested("session-1") {
		t.Error("reset_for_new_task 不应清除 abort 标记")
	}
}

func TestJiuClawStreamEventRail_CleanupSession(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.Abort("session-1")
	rail.Pause("session-1")
	rail.mu.Lock()
	rail.conversationIDs["session-1"] = "conv-1"
	rail.mainSessions["session-1"] = &fakeSession{}
	rail.cancelledToolResults["session-1"] = []map[string]any{{"key": "val"}}
	rail.mu.Unlock()

	rail.CleanupSession("session-1")

	rail.mu.Lock()
	defer rail.mu.Unlock()
	if rail.abortRequested["session-1"] {
		t.Error("cleanup 应清除 abort 标记")
	}
	if _, ok := rail.pauseConds["session-1"]; ok {
		t.Error("cleanup 应清除 pause_cond")
	}
	if _, ok := rail.conversationIDs["session-1"]; ok {
		t.Error("cleanup 应清除 conversation_id")
	}
	if _, ok := rail.mainSessions["session-1"]; ok {
		t.Error("cleanup 应清除 main_session")
	}
	if _, ok := rail.cancelledToolResults["session-1"]; ok {
		t.Error("cleanup 应清除 cancelled_tool_results")
	}
}

// ──────────────────────────── 取消工具结果 ────────────────────────────

func TestJiuClawStreamEventRail_CollectAndGetCancelledToolResults(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.mu.Lock()
	rail.inflightToolCalls["tc-1"] = inflightToolCallInfo{
		toolCall:  llmschema.NewToolCall("tc-1", "read_file", `{}`),
		sessionID: "session-1",
	}
	rail.inflightToolCalls["tc-2"] = inflightToolCallInfo{
		toolCall:  llmschema.NewToolCall("tc-2", "write_file", `{}`),
		sessionID: "session-2",
	}
	rail.mu.Unlock()

	// 只收集 session-1 的工具
	rail.CollectCancelledToolUpdates("session-1")

	results := rail.GetCancelledToolResults("session-1")
	if len(results) != 1 {
		t.Fatalf("应收集 1 个取消工具，实际 %d", len(results))
	}
	if results[0]["tool_call_id"] != "tc-1" {
		t.Errorf("tool_call_id = %q, want %q", results[0]["tool_call_id"], "tc-1")
	}
	if results[0]["status"] != "error" {
		t.Errorf("status = %q, want %q", results[0]["status"], "error")
	}

	// session-2 的工具不应被收集
	rail.mu.Lock()
	_, hasTC2 := rail.inflightToolCalls["tc-2"]
	rail.mu.Unlock()
	if !hasTC2 {
		t.Error("tc-2 不属于 session-1，不应被移除")
	}
}

func TestJiuClawStreamEventRail_ClearCancelledToolResults(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.mu.Lock()
	rail.cancelledToolResults["session-1"] = []map[string]any{{"key": "val"}}
	rail.mu.Unlock()

	rail.ClearCancelledToolResults("session-1")

	rail.mu.Lock()
	_, has := rail.cancelledToolResults["session-1"]
	rail.mu.Unlock()
	if has {
		t.Error("clear 应移除 cancelled_tool_results")
	}
}

// ──────────────────────────── resolveSID ────────────────────────────

func TestJiuClawStreamEventRail_ResolveSID_从Extra(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.Extra()[sidKey] = "session-from-extra"

	sid := rail.resolveSID(cbc, nil)
	if sid != "session-from-extra" {
		t.Errorf("sid = %q, want %q", sid, "session-from-extra")
	}
}

func TestJiuClawStreamEventRail_ResolveSID_回退Default(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)

	sid := rail.resolveSID(cbc, nil)
	if sid != defaultSessionID {
		t.Errorf("sid = %q, want %q", sid, defaultSessionID)
	}
}

func TestJiuClawStreamEventRail_ResolveSID_从Session匹配(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sess := &fakeSession{}

	rail.mu.Lock()
	rail.mainSessions["known-sid"] = sess
	rail.mu.Unlock()

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	sid := rail.resolveSID(cbc, sess)
	if sid != "known-sid" {
		t.Errorf("sid = %q, want %q", sid, "known-sid")
	}
}

// ──────────────────────────── BeforeInvoke ────────────────────────────

func TestJiuClawStreamEventRail_BeforeInvoke(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sess := &fakeSession{}

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{
		ConversationID: "conv-123",
	}, sess)

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeInvoke 返回错误: %v", err)
	}

	// 验证 conversation_id 和 session 被记录
	rail.mu.Lock()
	convID := rail.conversationIDs["conv-123"]
	storedSession := rail.mainSessions["conv-123"]
	rail.mu.Unlock()
	if convID != "conv-123" {
		t.Errorf("conversation_id = %q, want %q", convID, "conv-123")
	}
	if storedSession != sess {
		t.Error("main_session 未正确记录")
	}

	// 验证 extra 中设置了 sid
	if cbc.Extra()[sidKey] != "conv-123" {
		t.Errorf("extra[sidKey] = %v, want %q", cbc.Extra()[sidKey], "conv-123")
	}
}

func TestJiuClawStreamEventRail_BeforeInvoke_NilSession(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.InvokeInputs{}, nil)

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeInvoke 应不返回错误: %v", err)
	}

	// 子代理 session 为 nil，不应记录
	rail.mu.Lock()
	count := len(rail.mainSessions)
	rail.mu.Unlock()
	if count != 0 {
		t.Errorf("main_sessions 数量 = %d, want 0", count)
	}
}

func TestJiuClawStreamEventRail_BeforeInvoke_NonInvokeInputs(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sess := &fakeSession{}
	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.ModelCallInputs{}, sess)

	err := rail.BeforeInvoke(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeInvoke 应不返回错误: %v", err)
	}
}

// ──────────────────────────── BeforeModelCall ────────────────────────────

func TestJiuClawStreamEventRail_BeforeModelCall_Abort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.Abort("test-sid")

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.Extra()[sidKey] = "test-sid"

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err == nil {
		t.Error("终止时应返回错误")
	}
}

func TestJiuClawStreamEventRail_BeforeModelCall_正常(t *testing.T) {
	rail := NewJiuClawStreamEventRail()

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.Extra()[sidKey] = "test-sid"

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("正常时应不返回错误: %v", err)
	}
}

// ──────────────────────────── BeforeToolCall ────────────────────────────

func TestJiuClawStreamEventRail_BeforeToolCall_正常(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-1", "read_file", `{"path": "/tmp"}`)

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.ToolCallInputs{ToolCall: tc, ToolName: "read_file"}, sess)
	cbc.Extra()[sidKey] = "test-sid"

	err := rail.BeforeToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("BeforeToolCall 返回错误: %v", err)
	}

	// 验证 inflight 跟踪
	rail.mu.Lock()
	info, has := rail.inflightToolCalls["tc-1"]
	rail.mu.Unlock()
	if !has {
		t.Fatal("inflight_tool_calls 应包含 tc-1")
	}
	if info.sessionID != "test-sid" {
		t.Errorf("sessionID = %q, want %q", info.sessionID, "test-sid")
	}
}

func TestJiuClawStreamEventRail_BeforeToolCall_Abort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	rail.Abort("test-sid")

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.Extra()[sidKey] = "test-sid"

	err := rail.BeforeToolCall(context.Background(), cbc)
	if err == nil {
		t.Error("终止时应返回错误")
	}
}

// ──────────────────────────── AfterToolCall ────────────────────────────

func TestJiuClawStreamEventRail_AfterToolCall_正常(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sess := &fakeSession{}
	tc := llmschema.NewToolCall("tc-1", "read_file", `{}`)

	// 设置 inflight 跟踪
	rail.mu.Lock()
	rail.inflightToolCalls["tc-1"] = inflightToolCallInfo{toolCall: tc, sessionID: "test-sid"}
	rail.conversationIDs["test-sid"] = "conv-1"
	rail.mu.Unlock()

	cbc := sainterfaces.NewAgentCallbackContext(nil, &sainterfaces.ToolCallInputs{
		ToolCall:   tc,
		ToolName:   "read_file",
		ToolResult: "file content",
	}, sess)
	cbc.Extra()[sidKey] = "test-sid"

	err := rail.AfterToolCall(context.Background(), cbc)
	if err != nil {
		t.Errorf("AfterToolCall 返回错误: %v", err)
	}

	// 验证 inflight 被移除
	rail.mu.Lock()
	_, has := rail.inflightToolCalls["tc-1"]
	rail.mu.Unlock()
	if has {
		t.Error("inflight_tool_calls 应移除 tc-1")
	}
}

// ──────────────────────────── GetCallbacks ────────────────────────────

func TestJiuClawStreamEventRail_GetCallbacks(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	callbacks := rail.GetCallbacks()

	expectedEvents := []sainterfaces.AgentCallbackEvent{
		sainterfaces.CallbackBeforeInvoke,
		sainterfaces.CallbackBeforeModelCall,
		sainterfaces.CallbackAfterModelCall,
		sainterfaces.CallbackBeforeToolCall,
		sainterfaces.CallbackAfterToolCall,
		sainterfaces.CallbackOnModelException,
	}

	for _, event := range expectedEvents {
		if _, ok := callbacks[event]; !ok {
			t.Errorf("GetCallbacks 缺少事件: %v", event)
		}
	}
	if len(callbacks) != len(expectedEvents) {
		t.Errorf("GetCallbacks 数量 = %d, want %d", len(callbacks), len(expectedEvents))
	}
}

// ──────────────────────────── SetTodoTool ────────────────────────────

func TestJiuClawStreamEventRail_SetTodoTool(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	loader := &fakeTodoLoader{}
	rail.SetTodoTool(loader)

	rail.mu.Lock()
	stored := rail.todoTool
	rail.mu.Unlock()
	if stored != loader {
		t.Error("todoTool 未正确设置")
	}
}

// ──────────────────────────── pauseCond 单元测试 ────────────────────────────

func TestPauseCond_初始未暂停(t *testing.T) {
	pc := newPauseCond()
	// Wait 应立即返回
	done := make(chan struct{})
	go func() {
		pc.Wait()
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("初始未暂停时 Wait 应立即返回")
	}
}

func TestPauseCond_PauseResume(t *testing.T) {
	pc := newPauseCond()
	pc.Pause()

	done := make(chan struct{})
	go func() {
		pc.Wait()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	pc.Resume()

	select {
	case <-done:
		// OK
	case <-time.After(200 * time.Millisecond):
		t.Error("Resume 后 Wait 应返回")
	}
}

func TestPauseCond_并发Wait(t *testing.T) {
	pc := newPauseCond()
	pc.Pause()

	var wg sync.WaitGroup
	count := 3
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pc.Wait()
		}()
	}

	time.Sleep(50 * time.Millisecond)
	pc.Resume()
	wg.Wait()
	// 所有 goroutine 应已返回
}

func TestPauseCond_WaitWithContext_取消(t *testing.T) {
	pc := newPauseCond()
	pc.Pause()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		err := pc.WaitWithContext(ctx)
		done <- err
	}()

	// 短暂延迟确保 goroutine 进入等待
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("context 取消时应返回错误")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("context 取消后 WaitWithContext 应返回")
	}
}

func TestPauseCond_WaitWithContext_正常恢复(t *testing.T) {
	pc := newPauseCond()
	pc.Pause()

	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		err := pc.WaitWithContext(ctx)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	pc.Resume()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("正常恢复时不应返回错误: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("Resume 后 WaitWithContext 应返回")
	}
}

// ──────────────────────────── 确认接口实现 ────────────────────────────

var _ sainterfaces.AgentRail = (*JiuClawStreamEventRail)(nil)
var _ sessioninterfaces.SessionFacade = (*fakeSession)(nil)
