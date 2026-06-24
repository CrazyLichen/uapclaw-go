package runner

import (
	"context"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// mockAgent 模拟 BaseAgent
type mockAgent struct {
	card   *agentschema.AgentCard
	result any
	err    error
}

func (m *mockAgent) Configure(ctx context.Context, config interfaces.AgentConfig) error {
	return nil
}

func (m *mockAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	return m.result, m.err
}

func (m *mockAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (m *mockAgent) Card() *agentschema.AgentCard {
	return m.card
}

func (m *mockAgent) Config() interfaces.AgentConfig {
	return nil
}

func (m *mockAgent) AbilityManager() any {
	return nil
}

func (m *mockAgent) CallbackManager() *rail.AgentCallbackManager {
	return nil
}

func (m *mockAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...cb.CallbackOption) error {
	return nil
}

func (m *mockAgent) RegisterRail(ctx context.Context, r rail.AgentRail, opts ...cb.CallbackOption) error {
	return nil
}

func (m *mockAgent) UnregisterRail(ctx context.Context, r rail.AgentRail) error {
	return nil
}

// mockWorkflow 模拟 Workflow
type mockWorkflow struct {
	card   *schema.WorkflowCard
	result any
	err    error
}

func (m *mockWorkflow) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.WorkflowOption) (any, error) {
	return m.result, m.err
}

func (m *mockWorkflow) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.WorkflowOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (m *mockWorkflow) Card() *schema.WorkflowCard {
	return m.card
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestRunAgent_正常调用 测试 RunAgent 正常调用路径
func TestRunAgent_正常调用(t *testing.T) {
	ag := &mockAgent{
		card:   agentschema.NewAgentCard(schema.WithName("test-agent"), schema.WithDescription("测试 Agent")),
		result: map[string]any{"output": "hello"},
	}
	sess := session.CreateAgentSession("test-agent", "test-session")

	result, err := RunAgent(context.Background(), ag, map[string]any{"input": "test"}, sess)
	if err != nil {
		t.Fatalf("RunAgent 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result 类型错误: %T", result)
	}
	if m["output"] != "hello" {
		t.Errorf("output = %v, want hello", m["output"])
	}
}

// TestRunAgent_执行错误 测试 RunAgent 执行出错
func TestRunAgent_执行错误(t *testing.T) {
	ag := &mockAgent{
		card: agentschema.NewAgentCard(schema.WithName("err-agent"), schema.WithDescription("错误 Agent")),
		err:  context.DeadlineExceeded,
	}
	sess := session.CreateAgentSession("err-agent", "err-session")

	_, err := RunAgent(context.Background(), ag, nil, sess)
	if err == nil {
		t.Error("RunAgent 应返回错误")
	}
}

// TestRunWorkflow_正常调用 测试 RunWorkflow 正常调用路径
func TestRunWorkflow_正常调用(t *testing.T) {
	wf := &mockWorkflow{
		card:   schema.NewWorkflowCard(schema.WithName("test-wf"), schema.WithDescription("测试 Workflow")),
		result: map[string]any{"status": "completed"},
	}

	result, err := RunWorkflow(context.Background(), wf, map[string]any{"input": "test"}, nil, nil)
	if err != nil {
		t.Fatalf("RunWorkflow 失败: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result 类型错误: %T", result)
	}
	if m["status"] != "completed" {
		t.Errorf("status = %v, want completed", m["status"])
	}
}

// TestRunWorkflow_执行错误 测试 RunWorkflow 执行出错
func TestRunWorkflow_执行错误(t *testing.T) {
	wf := &mockWorkflow{
		card: schema.NewWorkflowCard(schema.WithName("err-wf"), schema.WithDescription("错误 Workflow")),
		err:  context.DeadlineExceeded,
	}

	_, err := RunWorkflow(context.Background(), wf, nil, nil, nil)
	if err == nil {
		t.Error("RunWorkflow 应返回错误")
	}
}
