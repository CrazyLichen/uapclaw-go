package resources_manager

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestTag常量_值 测试特殊标签常量值
func TestTag常量_值(t *testing.T) {
	if TagAll != "*" {
		t.Errorf("TagAll = %q, want \"*\"", TagAll)
	}
	if TagGlobal != "__global__" {
		t.Errorf("TagGlobal = %q, want \"__global__\"", TagGlobal)
	}
	if TagActive != "__active__" {
		t.Errorf("TagActive = %q, want \"__active__\"", TagActive)
	}
	if TagInactive != "__inactive__" {
		t.Errorf("TagInactive = %q, want \"__inactive__\"", TagInactive)
	}
}

// TestTagMatchStrategy_值 测试标签匹配策略枚举值
func TestTagMatchStrategy_值(t *testing.T) {
	if TagMatchAll != 0 {
		t.Errorf("TagMatchAll = %d, want 0", TagMatchAll)
	}
	if TagMatchAny != 1 {
		t.Errorf("TagMatchAny = %d, want 1", TagMatchAny)
	}
}

// TestTagUpdateStrategy_值 测试标签更新策略枚举值
func TestTagUpdateStrategy_值(t *testing.T) {
	if TagUpdateMerge != 0 {
		t.Errorf("TagUpdateMerge = %d, want 0", TagUpdateMerge)
	}
	if TagUpdateReplace != 1 {
		t.Errorf("TagUpdateReplace = %d, want 1", TagUpdateReplace)
	}
}

// TestAgentProvider_调用 测试 AgentProvider 函数类型可调用
func TestAgentProvider_调用(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("test"), schema.WithDescription("测试"))
	provider := AgentProvider(func(_ context.Context, _ *agentschema.AgentCard) (interfaces.BaseAgent, error) {
		return nil, nil
	})
	_, err := provider(context.Background(), card)
	if err != nil {
		t.Errorf("AgentProvider 调用失败: %v", err)
	}
}

// TestWorkflowProvider_调用 测试 WorkflowProvider 函数类型可调用
func TestWorkflowProvider_调用(t *testing.T) {
	wfCard := schema.NewWorkflowCard(schema.WithName("test-wf"), schema.WithDescription("测试"))
	provider := WorkflowProvider(func(_ context.Context, _ *schema.WorkflowCard) (interfaces.Workflow, error) {
		return nil, nil
	})
	_, err := provider(context.Background(), wfCard)
	if err != nil {
		t.Errorf("WorkflowProvider 调用失败: %v", err)
	}
}

// TestModelProvider_调用 测试 ModelProvider 函数类型可调用
func TestModelProvider_调用(t *testing.T) {
	provider := ModelProvider(func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return nil, nil
	})
	_, err := provider(context.Background(), "test-model")
	if err != nil {
		t.Errorf("ModelProvider 调用失败: %v", err)
	}
}

// TestPromptEntry_字段 测试 PromptEntry 结构体字段
func TestPromptEntry_字段(t *testing.T) {
	entry := PromptEntry{ID: "prompt-1"}
	if entry.ID != "prompt-1" {
		t.Errorf("PromptEntry.ID = %q, want \"prompt-1\"", entry.ID)
	}
}

// TestWorkflowEntry_字段 测试 WorkflowEntry 结构体字段
func TestWorkflowEntry_字段(t *testing.T) {
	entry := WorkflowEntry{ID: "wf-1"}
	if entry.ID != "wf-1" {
		t.Errorf("WorkflowEntry.ID = %q, want \"wf-1\"", entry.ID)
	}
}

// TestModelEntry_字段 测试 ModelEntry 结构体字段
func TestModelEntry_字段(t *testing.T) {
	entry := ModelEntry{ID: "model-1"}
	if entry.ID != "model-1" {
		t.Errorf("ModelEntry.ID = %q, want \"model-1\"", entry.ID)
	}
}

// TestAgentEntry_字段 测试 AgentEntry 结构体字段
func TestAgentEntry_字段(t *testing.T) {
	entry := AgentEntry{Card: agentschema.NewAgentCard(schema.WithName("test"))}
	if entry.Card == nil {
		t.Error("AgentEntry.Card 不应为 nil")
	}
}
