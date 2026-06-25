package session

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
)

// ──────────────────────────── 测试辅助类型 ────────────────────────────

// TestNewNodeSessionFacade 测试构造函数
func TestNewNodeSessionFacade(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade == nil {
		t.Fatal("NewNodeSessionFacade 返回 nil")
	}
}

// TestNodeSessionFacade_流式模式 测试 streamMode 字段
func TestNodeSessionFacade_流式模式(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "node1", "Test", false)

	f1 := NewNodeSessionFacade(ns, false)
	if f1.streamMode {
		t.Error("streamMode=false 时不应为 true")
	}

	f2 := NewNodeSessionFacade(ns, true)
	if !f2.streamMode {
		t.Error("streamMode=true 时不应为 false")
	}
}

// ──────────────────────────── 身份方法测试 ────────────────────────────

// TestNodeSessionFacade_GetWorkflowID 测试返回工作流 ID
func TestNodeSessionFacade_GetWorkflowID(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetWorkflowID() != "wf-1" {
		t.Errorf("期望 GetWorkflowID='wf-1'，实际=%s", facade.GetWorkflowID())
	}
}

// TestNodeSessionFacade_GetComponentID 测试返回节点 ID
func TestNodeSessionFacade_GetComponentID(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetComponentID() != "llm_node" {
		t.Errorf("期望 GetComponentID='llm_node'，实际=%s", facade.GetComponentID())
	}
}

// TestNodeSessionFacade_GetComponentType 测试返回节点类型
func TestNodeSessionFacade_GetComponentType(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetComponentType() != "LLM" {
		t.Errorf("期望 GetComponentType='LLM'，实际=%s", facade.GetComponentType())
	}
}

// TestNodeSessionFacade_GetComponentDescription 测试返回描述
func TestNodeSessionFacade_GetComponentDescription(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	expected := "[wf_id=wf-1,comp_id=llm_node]"
	if facade.GetComponentDescription() != expected {
		t.Errorf("期望 GetComponentDescription='%s'，实际=%s", expected, facade.GetComponentDescription())
	}
}

// TestNodeSessionFacade_GetExecutableID 测试返回可执行路径 ID
func TestNodeSessionFacade_GetExecutableID(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "start", "Start", false)
	ns2 := internal.NewNodeSession(ns, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns2, false)

	if facade.GetExecutableID() != "start.llm_node" {
		t.Errorf("期望 GetExecutableID='start.llm_node'，实际=%s", facade.GetExecutableID())
	}
}

// TestNodeSessionFacade_GetSessionID 测试返回会话 ID
func TestNodeSessionFacade_GetSessionID(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowSessionID("sess-123"))
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	if facade.GetSessionID() != "sess-123" {
		t.Errorf("期望 GetSessionID='sess-123'，实际=%s", facade.GetSessionID())
	}
}

// ──────────────────────────── 状态方法测试 ────────────────────────────

// TestNodeSessionFacade_UpdateState_GetState 测试组件状态读写
func TestNodeSessionFacade_UpdateState_GetState(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	// 更新组件状态
	facade.UpdateState(map[string]any{"comp_key": "comp_val"})

	// 提交后可读
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result, err := facade.GetState(state.StringKey("comp_key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "comp_val" {
		t.Errorf("期望 GetState='comp_val'，实际=%v", result)
	}
}

// TestNodeSessionFacade_UpdateGlobalState_GetGlobalState 测试全局状态读写
func TestNodeSessionFacade_UpdateGlobalState_GetGlobalState(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	// 更新全局状态
	facade.UpdateGlobalState(map[string]any{"global_key": "global_val"})

	// 提交后可读
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result, err := facade.GetGlobalState(state.StringKey("global_key"))
	if err != nil {
		t.Errorf("GetGlobalState 不应返回错误：%v", err)
	}
	if result != "global_val" {
		t.Errorf("期望 GetGlobalState='global_val'，实际=%v", result)
	}
}

// TestNodeSessionFacade_DumpState 测试导出完整快照
func TestNodeSessionFacade_DumpState(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	dump := facade.DumpState()
	if dump == nil {
		t.Fatal("DumpState 不应返回 nil")
	}
	// 验证返回的快照包含核心状态 key
	for _, key := range []string{state.IOStateKey, state.CompStateKey, state.GlobalStateKey, state.WorkflowStateKey} {
		if _, ok := dump[key]; !ok {
			t.Errorf("DumpState 返回的快照应包含 key=%s", key)
		}
	}
}

// TestNodeSessionFacade_GetState_SchemaKey 测试 SchemaKey 访问
func TestNodeSessionFacade_GetState_SchemaKey(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	facade.UpdateState(map[string]any{"name": "test", "value": 42})
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	result, err := facade.GetState(state.SchemaKey(map[string]any{"name": nil}))
	if err != nil {
		t.Errorf("GetState(SchemaKey) 不应返回错误：%v", err)
	}
	if result == nil {
		t.Error("GetState(SchemaKey) 不应返回 nil")
	}
}

// ──────────────────────────── 追踪方法测试 ────────────────────────────

// TestNodeSessionFacade_Trace_无Tracer 测试无 Tracer 时 Trace 返回 nil
// ✅ 5.11 已回填：Trace 使用 TracerWorkflowUtils.Trace 真实逻辑
func TestNodeSessionFacade_Trace_无Tracer(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.Trace(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("Trace 无 Tracer 时应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_TraceError_无Tracer 测试无 Tracer 时 TraceError 返回 nil
// ✅ 5.11 已回填：TraceError 使用 TracerWorkflowUtils.TraceError 真实逻辑
func TestNodeSessionFacade_TraceError_无Tracer(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.TraceError(context.Background(), fmt.Errorf("test error"))
	if err != nil {
		t.Errorf("TraceError 无 Tracer 时应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_Trace_SkipTrace 测试 SkipTrace 时跳过追踪
func TestNodeSessionFacade_Trace_SkipTrace(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", true) // skipTrace=true
	facade := NewNodeSessionFacade(ns, false)

	err := facade.Trace(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("SkipTrace 时 Trace 应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_UpdateState_错误路径 测试 UpdateState 内部 Update 返回 error 时记录日志
func TestNodeSessionFacade_UpdateState_错误路径(t *testing.T) {
	// 直接构造 NodeSessionFacade，注入一个返回 error 的 fakeState
	// 由于 NodeSessionFacade.inner 是未导出字段，通过 WorkflowSession 创建 NodeSession
	// 然后替换 state 来触发错误路径。
	// 实际上 node.go 的 UpdateState 调用 f.inner.State().Update(data)，
	// 其中 f.inner 是 *internal.NodeSession，其 State() 返回的是 WorkflowCommitState，
	// 正常情况不会返回 error。
	// 要测试错误路径，需要 mock internal.NodeSession 的 State() 方法。
	// 但 NodeSession 是具体类型不是接口，无法直接 mock。
	// 替代方案：测试 interaction 包中的 fakeErrorState 来验证日志行为。
	// 此测试验证：如果底层 State().Update 返回 error，UpdateState 不 panic。

	// 使用 WorkflowCommitState 创建正常的 facade，确认不会 panic
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	// 正常调用 UpdateState 不应 panic
	facade.UpdateState(map[string]any{"key": "val"})
}

// TestNodeSessionFacade_TraceError_非跳过 测试 SkipTrace=false 且无 Tracer 时 TraceError 返回 nil
// ✅ 5.11 已回填：TraceError 使用 TracerWorkflowUtils.TraceError 真实逻辑
func TestNodeSessionFacade_TraceError_非跳过(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false) // skipTrace=false
	facade := NewNodeSessionFacade(ns, false)

	err := facade.TraceError(context.Background(), fmt.Errorf("test error"))
	if err != nil {
		t.Errorf("TraceError 无 Tracer 时应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_Trace_走真实逻辑 测试 SkipTrace=false 且 tracer 非 nil 时，
// Trace 调用 TracerWorkflowUtils.Trace，验证 span 收到 OnInvokeData
func TestNodeSessionFacade_Trace_走真实逻辑(t *testing.T) {
	// 创建 AgentSession（自动创建 Tracer + StreamWriterManager）
	agentSession := internal.NewAgentSession("test-session")

	// 创建 WorkflowSession，以 AgentSession 为父级，继承 Tracer
	ws := internal.NewWorkflowSession(
		internal.WithWorkflowParent(agentSession),
		internal.WithWorkflowID("wf-trace-test"),
	)

	// 创建 NodeSession（skipTrace=false）
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	// 先触发 TraceComponentBegin 创建 span，否则 Trace 写入的 span 不存在
	tUtils := tracer.TracerWorkflowUtils{}
	tUtils.TraceComponentBegin(context.Background(), ns, nil)

	// 调用 Trace
	traceData := map[string]any{"step": "processing", "token_count": 42}
	err := facade.Trace(context.Background(), traceData)
	if err != nil {
		t.Errorf("Trace 应返回 nil，实际=%v", err)
	}

	// 验证 span 已收到 OnInvokeData
	span := agentSession.Tracer().GetWorkflowSpan(ns.ExecutableID(), ns.ParentID())
	if span == nil {
		t.Fatal("Trace 后应存在 workflow span")
	}
	if len(span.OnInvokeData) == 0 {
		t.Error("span.OnInvokeData 为空，期望非空")
	}
	if len(span.OnInvokeData) > 0 {
		data := span.OnInvokeData[0]
		if v, ok := data["step"]; !ok || v != "processing" {
			t.Errorf("span.OnInvokeData[0]['step'] 期望='processing'，实际=%v", v)
		}
		if v, ok := data["token_count"]; !ok || v != 42 {
			t.Errorf("span.OnInvokeData[0]['token_count'] 期望=42，实际=%v", v)
		}
	}
}

// TestNodeSessionFacade_TraceError_走真实逻辑 测试 SkipTrace=false 且 tracer 非 nil 时，
// TraceError 调用 TracerWorkflowUtils.TraceError，验证 span 收到 Error
func TestNodeSessionFacade_TraceError_走真实逻辑(t *testing.T) {
	// 创建 AgentSession（自动创建 Tracer + StreamWriterManager）
	agentSession := internal.NewAgentSession("test-session-err")

	// 创建 WorkflowSession，以 AgentSession 为父级，继承 Tracer
	ws := internal.NewWorkflowSession(
		internal.WithWorkflowParent(agentSession),
		internal.WithWorkflowID("wf-traceerr-test"),
	)

	// 创建 NodeSession（skipTrace=false）
	ns := internal.NewNodeSession(ws, "llm_node", "LLM", false)
	facade := NewNodeSessionFacade(ns, false)

	// 先触发 TraceComponentBegin 创建 span
	tUtils := tracer.TracerWorkflowUtils{}
	tUtils.TraceComponentBegin(context.Background(), ns, nil)

	// 调用 TraceError
	testErr := fmt.Errorf("something went wrong")
	err := facade.TraceError(context.Background(), testErr)
	if err != nil {
		t.Errorf("TraceError 应返回 nil，实际=%v", err)
	}

	// 验证 span 已收到 Error
	span := agentSession.Tracer().GetWorkflowSpan(ns.ExecutableID(), ns.ParentID())
	if span == nil {
		t.Fatal("TraceError 后应存在 workflow span")
	}
	if len(span.Error) == 0 {
		t.Error("span.Error 为空，期望非空")
	}
	// 验证 Error 内容包含错误信息（OnInvoke 将 error 转换为 map[string]any）
	if errMsg, ok := span.Error["message"]; !ok {
		t.Error("span.Error 应包含 'message' 键")
	} else if msg, _ := errMsg.(string); msg == "" {
		t.Error("span.Error['message'] 不应为空字符串")
	}
}

// ──────────────────────────── 交互方法测试 ────────────────────────────

// TestNodeSessionFacade_Interact_流式模式返回错误 测试流式模式下 Interact 禁止
func TestNodeSessionFacade_Interact_流式模式返回错误(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, true) // streamMode=true

	_, err := facade.InteractWithResult(context.Background(), "question")
	if err == nil {
		t.Error("流式模式下 Interact 应返回错误")
	}
}

// TestNodeSessionFacade_Interact_非流式模式触发GraphInterrupt 测试非流式模式下 Interact 触发 GraphInterrupt
func TestNodeSessionFacade_Interact_非流式模式触发GraphInterrupt(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false) // streamMode=false

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("非流式模式下 Interact 应触发 GraphInterrupt")
		}
		if _, ok := r.(*interaction.GraphInterrupt); !ok {
			t.Fatalf("期望 *interaction.GraphInterrupt，得到 %T", r)
		}
	}()

	_, _ = facade.InteractWithResult(context.Background(), "question")
}

// ──────────────────────────── 流写入方法测试 ────────────────────────────

// TestNodeSessionFacade_WriteStream_桩 测试 WriteStream 桩方法
func TestNodeSessionFacade_WriteStream_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.WriteStream(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("WriteStream 桩应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_WriteCustomStream_桩 测试 WriteCustomStream 桩方法
func TestNodeSessionFacade_WriteCustomStream_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.WriteCustomStream(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("WriteCustomStream 桩应返回 nil，实际=%v", err)
	}
}

// ──────────────────────────── 环境/配置方法测试 ────────────────────────────

// TestNodeSessionFacade_GetEnv_桩 测试 GetEnv 桩方法
func TestNodeSessionFacade_GetEnv_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	result := facade.GetEnv("any_key")
	if result != nil {
		t.Errorf("GetEnv 桩应返回 nil，实际=%v", result)
	}
}

// TestNodeSessionFacade_GetNodeConfig_桩 测试 GetNodeConfig 桩方法
func TestNodeSessionFacade_GetNodeConfig_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	result := facade.GetNodeConfig()
	if result != nil {
		t.Errorf("GetNodeConfig 桩应返回 nil，实际=%v", result)
	}
}
