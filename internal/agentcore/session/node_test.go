package session

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 构造函数测试 ────────────────────────────

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
	parent := internal.NewAgentSession("sess-123")
	ns := internal.NewNodeSession(parent, "node1", "Test", false)
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
		t.Error("DumpState 不应返回 nil")
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

// TestNodeSessionFacade_Trace_桩 测试 Trace 桩方法
func TestNodeSessionFacade_Trace_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.Trace(context.Background(), map[string]any{"data": "test"})
	if err != nil {
		t.Errorf("Trace 桩应返回 nil，实际=%v", err)
	}
}

// TestNodeSessionFacade_TraceError_桩 测试 TraceError 桩方法
func TestNodeSessionFacade_TraceError_桩(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false)

	err := facade.TraceError(context.Background(), fmt.Errorf("test error"))
	if err != nil {
		t.Errorf("TraceError 桩应返回 nil，实际=%v", err)
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

// ──────────────────────────── 交互方法测试 ────────────────────────────

// TestNodeSessionFacade_Interact_流式模式返回错误 测试流式模式下 Interact 禁止
func TestNodeSessionFacade_Interact_流式模式返回错误(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, true) // streamMode=true

	_, err := facade.Interact(context.Background(), "question")
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

	facade.Interact(context.Background(), "question")
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
