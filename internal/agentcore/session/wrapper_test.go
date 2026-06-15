package session

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRouterSessionFacade_全部方法 测试 RouterSessionFacade 所有方法
func TestRouterSessionFacade_全部方法(t *testing.T) {
	ws := internal.NewWorkflowSession(internal.WithWorkflowID("wf-1"))
	ns := internal.NewNodeSession(ws, "router_node", "Router", false)
	facade := NewNodeSessionFacade(ns, false)
	router := NewRouterSessionFacade(facade)

	// 只读身份方法应委托给 inner
	if router.GetWorkflowID() != "wf-1" {
		t.Errorf("期望 GetWorkflowID='wf-1'，实际=%s", router.GetWorkflowID())
	}
	if router.GetComponentID() != "router_node" {
		t.Errorf("期望 GetComponentID='router_node'，实际=%s", router.GetComponentID())
	}
	if router.GetComponentType() != "Router" {
		t.Errorf("期望 GetComponentType='Router'，实际=%s", router.GetComponentType())
	}

	expectedDesc := "[wf_id=wf-1,comp_id=router_node]"
	if router.GetComponentDescription() != expectedDesc {
		t.Errorf("期望 GetComponentDescription='%s'，实际=%s", expectedDesc, router.GetComponentDescription())
	}

	if router.GetExecutableID() != "router_node" {
		t.Errorf("期望 GetExecutableID='router_node'，实际=%s", router.GetExecutableID())
	}

	if router.GetSessionID() == "" {
		t.Error("期望 GetSessionID 非空")
	}
}

// TestRouterSessionFacade_状态读取 测试只读状态方法
func TestRouterSessionFacade_状态读取(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "router_node", "Router", false)
	facade := NewNodeSessionFacade(ns, false)
	router := NewRouterSessionFacade(facade)

	// 先写入状态
	facade.UpdateState(map[string]any{"key": "value"})
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}

	// 读取方法应委托给 inner
	result, err := router.GetState(state.StringKey("key"))
	if err != nil {
		t.Errorf("GetState 不应返回错误：%v", err)
	}
	if result != "value" {
		t.Errorf("期望 GetState='value'，实际=%v", result)
	}

	// 全局状态读取
	facade.UpdateGlobalState(map[string]any{"global_key": "global_val"})
	if cs, ok := ns.State().(*state.WorkflowCommitState); ok {
		cs.Commit()
	}
	gResult, gErr := router.GetGlobalState(state.StringKey("global_key"))
	if gErr != nil {
		t.Errorf("GetGlobalState 不应返回错误：%v", gErr)
	}
	if gResult != "global_val" {
		t.Errorf("期望 GetGlobalState='global_val'，实际=%v", gResult)
	}

	// DumpState 应委托
	dump := router.DumpState()
	if dump == nil {
		t.Error("DumpState 不应返回 nil")
	}
}

// TestRouterSessionFacade_禁止写入 测试写入方法静默忽略
func TestRouterSessionFacade_禁止写入(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "router_node", "Router", false)
	facade := NewNodeSessionFacade(ns, false)
	router := NewRouterSessionFacade(facade)

	// UpdateState 应静默忽略，不 panic
	router.UpdateState(map[string]any{"forbidden": "write"})

	// UpdateGlobalState 应静默忽略
	router.UpdateGlobalState(map[string]any{"forbidden_global": "write"})
}

// TestRouterSessionFacade_追踪方法 测试 Trace 和 TraceError 委托
func TestRouterSessionFacade_追踪方法(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "router_node", "Router", false)
	facade := NewNodeSessionFacade(ns, false)
	router := NewRouterSessionFacade(facade)

	// Trace 应委托给 inner
	err := router.Trace(context.Background(), map[string]any{"trace": "data"})
	if err != nil {
		t.Errorf("Trace 应返回 nil，实际=%v", err)
	}

	// TraceError 应委托给 inner
	err = router.TraceError(context.Background(), nil)
	if err != nil {
		t.Errorf("TraceError 应返回 nil，实际=%v", err)
	}
}

// TestRouterSessionFacade_禁止交互和流 测试禁止操作的方法
func TestRouterSessionFacade_禁止交互和流(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "router_node", "Router", false)
	facade := NewNodeSessionFacade(ns, false)
	router := NewRouterSessionFacade(facade)

	// Interact 应返回 nil, nil（静默忽略）
	result, err := router.Interact(context.Background(), "question")
	if err != nil {
		t.Errorf("Interact 应返回 nil 错误，实际=%v", err)
	}
	if result != nil {
		t.Errorf("Interact 应返回 nil 结果，实际=%v", result)
	}

	// WriteStream 应返回 nil
	if err := router.WriteStream(context.Background(), "data"); err != nil {
		t.Errorf("WriteStream 应返回 nil，实际=%v", err)
	}

	// WriteCustomStream 应返回 nil
	if err := router.WriteCustomStream(context.Background(), "data"); err != nil {
		t.Errorf("WriteCustomStream 应返回 nil，实际=%v", err)
	}

	// GetEnv 应返回 nil
	if router.GetEnv("key") != nil {
		t.Error("GetEnv 应返回 nil")
	}

	// GetNodeConfig 应返回 nil
	if router.GetNodeConfig() != nil {
		t.Error("GetNodeConfig 应返回 nil")
	}
}
