package resources_manager

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestWorkflowMgr_添加获取正常 测试 AddWorkflow → GetWorkflow 正常流程
func TestWorkflowMgr_添加获取正常(t *testing.T) {
	mgr := NewWorkflowMgr()

	mockWorkflow := &stubWorkflow{}
	provider := func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
		return mockWorkflow, nil
	}

	err := mgr.AddWorkflow("wf-1", provider)
	if err != nil {
		t.Fatalf("AddWorkflow 失败: %v", err)
	}

	w, err := mgr.GetWorkflow(context.Background(), "wf-1", nil)
	if err != nil {
		t.Fatalf("GetWorkflow 失败: %v", err)
	}
	if w == nil {
		t.Error("GetWorkflow 返回 nil，期望非 nil")
	}
}

// TestWorkflowMgr_Trace装饰 测试 session 非 nil 时进行追踪装饰
func TestWorkflowMgr_Trace装饰(t *testing.T) {
	mgr := NewWorkflowMgr()

	mockWorkflow := &stubWorkflow{
		card: &schema.WorkflowCard{
			BaseCard: schema.BaseCard{ID: "wf-1", Name: "test-workflow"},
		},
	}
	provider := func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
		return mockWorkflow, nil
	}

	err := mgr.AddWorkflow("wf-1", provider)
	if err != nil {
		t.Fatalf("AddWorkflow 失败: %v", err)
	}

	// session 为 nil 时不装饰，返回原始工作流
	w, err := mgr.GetWorkflow(context.Background(), "wf-1", nil)
	if err != nil {
		t.Fatalf("GetWorkflow 失败: %v", err)
	}
	if _, ok := w.(*stubWorkflow); !ok {
		t.Error("session=nil 时应返回原始工作流，而非装饰后的工作流")
	}
}

// TestWorkflowMgr_批量添加 测试 AddWorkflows 批量添加
func TestWorkflowMgr_批量添加(t *testing.T) {
	mgr := NewWorkflowMgr()

	entries := []WorkflowEntry{
		{
			ID: "wf-1",
			Provider: func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
				return &stubWorkflow{}, nil
			},
		},
		{
			ID: "wf-2",
			Provider: func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
				return &stubWorkflow{}, nil
			},
		},
	}
	mgr.AddWorkflows(entries)

	w1, err := mgr.GetWorkflow(context.Background(), "wf-1", nil)
	if err != nil {
		t.Fatalf("GetWorkflow(wf-1) 失败: %v", err)
	}
	if w1 == nil {
		t.Error("GetWorkflow(wf-1) 返回 nil")
	}

	w2, err := mgr.GetWorkflow(context.Background(), "wf-2", nil)
	if err != nil {
		t.Fatalf("GetWorkflow(wf-2) 失败: %v", err)
	}
	if w2 == nil {
		t.Error("GetWorkflow(wf-2) 返回 nil")
	}
}

// TestWorkflowMgr_获取不存在返回错误 测试不存在的 workflowID 返回错误
func TestWorkflowMgr_获取不存在返回错误(t *testing.T) {
	mgr := NewWorkflowMgr()

	_, err := mgr.GetWorkflow(context.Background(), "not-exist", nil)
	if err == nil {
		t.Error("获取不存在的工作流应返回错误")
	}
}

// TestWorkflowMgr_Provider返回错误 测试 provider 执行时返回错误
func TestWorkflowMgr_Provider返回错误(t *testing.T) {
	mgr := NewWorkflowMgr()

	provider := func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
		return nil, errors.New("internal error")
	}

	err := mgr.AddWorkflow("err-wf", provider)
	if err != nil {
		t.Fatalf("AddWorkflow 失败: %v", err)
	}

	_, err = mgr.GetWorkflow(context.Background(), "err-wf", nil)
	if err == nil {
		t.Error("provider 返回错误时 GetWorkflow 应传播错误")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
