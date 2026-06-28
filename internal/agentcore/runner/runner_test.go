package runner

import (
	"context"
	"strings"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
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

	result, err := RunAgent(context.Background(), ByAgent(ag), map[string]any{"input": "test"}, sess, nil, nil)
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

	_, err := RunAgent(context.Background(), ByAgent(ag), nil, sess, nil, nil)
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

	result, err := RunWorkflow(context.Background(), ByWorkflow(wf), map[string]any{"input": "test"}, nil, nil, nil)
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

	_, err := RunWorkflow(context.Background(), ByWorkflow(wf), nil, nil, nil, nil)
	if err == nil {
		t.Error("RunWorkflow 应返回错误")
	}
}

// TestStart_正常启动 测试 Start 正常启动
func TestStart_正常启动(t *testing.T) {
	if err := Start(context.Background()); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
}

// TestStop_正常停止 测试 Stop 正常停止
func TestStop_正常停止(t *testing.T) {
	if err := Stop(context.Background()); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
}

// TestGetResourceMgr 测试 GetResourceMgr 返回非 nil
func TestGetResourceMgr(t *testing.T) {
	mgr := GetResourceMgr()
	if mgr == nil {
		t.Error("GetResourceMgr 返回 nil, 期望非 nil")
	}
}

// TestGetCallbackFramework 测试 GetCallbackFramework 返回非 nil
func TestGetCallbackFramework(t *testing.T) {
	fw := GetCallbackFramework()
	if fw == nil {
		t.Error("GetCallbackFramework 返回 nil, 期望非 nil")
	}
}

// TestRelease_正常释放 测试 Release 正常释放
func TestRelease_正常释放(t *testing.T) {
	if err := Release(context.Background(), "test-session", false); err != nil {
		t.Fatalf("Release 失败: %v", err)
	}
}

// TestSpawnAgent_未实现 测试 SpawnAgent 返回未实现错误
func TestSpawnAgent_未实现(t *testing.T) {
	_, err := SpawnAgent(context.Background(), nil, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("SpawnAgent 应返回错误")
	}
	if !strings.Contains(err.Error(), "6.28") {
		t.Errorf("错误信息应包含 '6.28', 实际: %v", err)
	}
}

// TestSetConfig_GetConfig 测试 SetConfig 和 GetConfig 往返
func TestSetConfig_GetConfig(t *testing.T) {
	cfg := &config.RunnerConfig{
		DistributedMode: false,
		EnvPrefix:       "test-prefix",
		InstanceID:       "test-instance",
	}
	SetConfig(cfg)
	got := GetConfig()
	if got == nil {
		t.Fatal("GetConfig 返回 nil")
	}
	if got.EnvPrefix != "test-prefix" {
		t.Errorf("EnvPrefix = %q, want %q", got.EnvPrefix, "test-prefix")
	}
	if got.InstanceID != "test-instance" {
		t.Errorf("InstanceID = %q, want %q", got.InstanceID, "test-instance")
	}
}
