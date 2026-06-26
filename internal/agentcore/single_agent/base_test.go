package single_agent

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewBaseAgent 构造函数验证
func TestNewBaseAgent(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("test_agent"), schema.WithDescription("测试 Agent"))
	agent := NewBaseAgent(card, nil)

	if agent == nil {
		t.Fatal("NewBaseAgent 不应返回 nil")
	}
	if agent.Card() == nil {
		t.Fatal("Card 不应为 nil")
	}
	if agent.Card().Name != "test_agent" {
		t.Errorf("Card.Name = %q, want test_agent", agent.Card().Name)
	}
	if agent.AbilityManager() == nil {
		t.Error("AbilityManager 不应为 nil")
	}
}

// TestBaseAgent_Configure Configure 设置 config 成功
func TestBaseAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("cfg_agent"), schema.WithDescription("配置测试"))
	agent := NewBaseAgent(card, nil)

	cfg := agentconfig.NewReActAgentConfig(
		agentconfig.WithModelName("qwen-max"),
		agentconfig.WithMaxIterations(10),
	)
	err := agent.Configure(context.Background(), cfg)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	got, ok := agent.Config().(*agentconfig.ReActAgentConfig)
	if !ok {
		t.Fatalf("Config 类型应为 *ReActAgentConfig，实际 %T", agent.Config())
	}
	if got.ModelName() != "qwen-max" {
		t.Errorf("ModelName() = %v, want qwen-max", got.ModelName())
	}
}

// TestBaseAgent_访问器 Card/Config/AbilityManager/CallbackManager 返回正确值
func TestBaseAgent_访问器(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("acc_agent"), schema.WithDescription("访问器测试"))
	agent := NewBaseAgent(card, nil)

	// Card
	if agent.Card() != card {
		t.Error("Card() 应返回构造时传入的 card")
	}

	// Config 默认为 nil
	if agent.Config() != nil {
		t.Error("默认 Config 应为 nil")
	}

	// AbilityManager 不为 nil
	if agent.AbilityManager() == nil {
		t.Error("AbilityManager() 不应为 nil")
	}
	am, ok := agent.AbilityManager().(*AbilityManager)
	if !ok {
		t.Fatalf("AbilityManager 类型应为 *AbilityManager，实际 %T", agent.AbilityManager())
	}
	if am == nil {
		t.Error("AbilityManager 内部值不应为 nil")
	}

	// CallbackManager 不为 nil（构造时初始化），返回具体类型
	cm := agent.CallbackManager()
	if cm == nil {
		t.Error("CallbackManager 不应为 nil")
	}
}

// TestBaseAgent_AgentID 返回 card.ID
func TestBaseAgent_AgentID(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("id_test"), schema.WithDescription("AgentID 测试"))
	agent := NewBaseAgent(card, nil)

	if agent.AgentID() != card.ID {
		t.Errorf("AgentID() = %q, want %q", agent.AgentID(), card.ID)
	}
}

// TestBaseAgent_AgentID_card为nil card 为 nil 时返回空串
func TestBaseAgent_AgentID_card为nil(t *testing.T) {
	agent := &BaseAgent{}
	if agent.AgentID() != "" {
		t.Errorf("card 为 nil 时 AgentID() 应返回空串，实际 %q", agent.AgentID())
	}
}

// TestBaseAgent_RegisterCallback callbackManager 非 nil 时委托注册
func TestBaseAgent_RegisterCallback(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("rc_agent"), schema.WithDescription("注册回调测试"))
	agent := NewBaseAgent(card, nil)

	fn := callback.PerAgentCallbackFunc(func(_ context.Context, _ any) error { return nil })
	err := agent.RegisterCallback(context.Background(), rail.CallbackBeforeInvoke, fn)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}

// TestBaseAgent_RegisterCallback_nilManager callbackManager 为 nil 时不 panic
func TestBaseAgent_RegisterCallback_nilManager(t *testing.T) {
	agent := &BaseAgent{}
	fn := callback.PerAgentCallbackFunc(func(_ context.Context, _ any) error { return nil })
	err := agent.RegisterCallback(context.Background(), rail.CallbackBeforeInvoke, fn)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}

// TestBaseAgent_RegisterRail callbackManager 非 nil 时委托注册
func TestBaseAgent_RegisterRail(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("rr_agent"), schema.WithDescription("注册 Rail 测试"))
	agent := NewBaseAgent(card, nil)

	r := &testRail{}
	err := agent.RegisterRail(context.Background(), r)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}

// TestBaseAgent_RegisterRail_nilManager callbackManager 为 nil 时直接返回 nil
func TestBaseAgent_RegisterRail_nilManager(t *testing.T) {
	agent := &BaseAgent{}
	r := &testRail{}
	err := agent.RegisterRail(context.Background(), r)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}

// TestBaseAgent_RegisterRail_init失败 Rail Init 返回错误时传播
func TestBaseAgent_RegisterRail_init失败(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("rr_fail"), schema.WithDescription("Rail Init 失败"))
	agent := NewBaseAgent(card, nil)

	r := &testRail{initErr: errors.New("init failed")}
	err := agent.RegisterRail(context.Background(), r)
	if err == nil {
		t.Fatal("应有错误")
	}
	if err.Error() != "init failed" {
		t.Errorf("错误 = %v, want init failed", err)
	}
}

// TestBaseAgent_UnregisterRail 正常注销
func TestBaseAgent_UnregisterRail(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("ur_agent"), schema.WithDescription("注销 Rail 测试"))
	agent := NewBaseAgent(card, nil)

	r := &testRail{}
	err := agent.UnregisterRail(context.Background(), r)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}

// TestBaseAgent_UnregisterRail_nilManager callbackManager 为 nil 时不 panic
func TestBaseAgent_UnregisterRail_nilManager(t *testing.T) {
	agent := &BaseAgent{}
	r := &testRail{}
	err := agent.UnregisterRail(context.Background(), r)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}

// TestBaseAgent_UnregisterRail_uninit失败 Uninit 返回错误且 callbackManager.UnregisterRail 成功时返回 uninitErr
func TestBaseAgent_UnregisterRail_uninit失败(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("ur_fail"), schema.WithDescription("Uninit 失败测试"))
	agent := NewBaseAgent(card, nil)

	r := &testRail{uninitErr: errors.New("uninit failed")}
	err := agent.UnregisterRail(context.Background(), r)
	if err == nil {
		t.Fatal("应有错误")
	}
	if err.Error() != "uninit failed" {
		t.Errorf("错误 = %v, want uninit failed", err)
	}
}

// testRail 实现 rail.AgentRail 接口，用于 RegisterRail/UnregisterRail 测试
type testRail struct {
	initErr   error
	uninitErr error
}

func (r *testRail) Priority() int                                                      { return 50 }
func (r *testRail) Init(_ rail.RailAgent) error                                        { return r.initErr }
func (r *testRail) Uninit(_ rail.RailAgent) error                                      { return r.uninitErr }
func (r *testRail) BeforeInvoke(_ context.Context, _ *rail.AgentCallbackContext) error { return nil }
func (r *testRail) AfterInvoke(_ context.Context, _ *rail.AgentCallbackContext) error  { return nil }
func (r *testRail) BeforeTaskIteration(_ context.Context, _ *rail.AgentCallbackContext) error {
	return nil
}
func (r *testRail) AfterTaskIteration(_ context.Context, _ *rail.AgentCallbackContext) error {
	return nil
}
func (r *testRail) BeforeModelCall(_ context.Context, _ *rail.AgentCallbackContext) error { return nil }
func (r *testRail) AfterModelCall(_ context.Context, _ *rail.AgentCallbackContext) error  { return nil }
func (r *testRail) OnModelException(_ context.Context, _ *rail.AgentCallbackContext) error {
	return nil
}
func (r *testRail) BeforeToolCall(_ context.Context, _ *rail.AgentCallbackContext) error  { return nil }
func (r *testRail) AfterToolCall(_ context.Context, _ *rail.AgentCallbackContext) error   { return nil }
func (r *testRail) OnToolException(_ context.Context, _ *rail.AgentCallbackContext) error { return nil }
func (r *testRail) GetCallbacks() map[rail.AgentCallbackEvent]callback.PerAgentCallbackFunc {
	return nil
}

// TestGlobalAgentEventType_事件名对齐Python 验证事件名与 Python AgentEvents 对齐
func TestGlobalAgentEventType_事件名对齐Python(t *testing.T) {
	// 对应 Python: openjiuwen/core/runner/callback/events.py AgentEvents
	tests := []struct {
		got  callback.GlobalAgentEventType
		want string
	}{
		{callback.GlobalAgentStarted, "_framework:agent_started"},
		{callback.GlobalAgentInvokeInput, "_framework:agent_invoke_input"},
		{callback.GlobalAgentInvokeOutput, "_framework:agent_invoke_output"},
		{callback.GlobalAgentStreamInput, "_framework:agent_stream_input"},
		{callback.GlobalAgentStreamOutput, "_framework:agent_stream_output"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("事件名 = %q, want %q", tt.got, tt.want)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBaseError_StatusAgentNotConfigured 验证 StatusAgentNotConfigured 错误码可用
func TestBaseError_StatusAgentNotConfigured(t *testing.T) {
	err := exception.NewBaseError(exception.StatusAgentNotConfigured, exception.WithMsg("未配置"))
	if err.Status() != exception.StatusAgentNotConfigured {
		t.Errorf("Status = %v, want StatusAgentNotConfigured", err.Status())
	}
}
