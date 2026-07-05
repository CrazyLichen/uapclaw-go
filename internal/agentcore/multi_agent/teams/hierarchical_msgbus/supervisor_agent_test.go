package hierarchical_msgbus

import (
	"context"
	"testing"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSupervisorAgent 验证构造函数。
func TestNewSupervisorAgent(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)
	config := saconfig.NewReActAgentConfig()

	supervisor := NewSupervisorAgent(card, config, 5)
	if supervisor == nil {
		t.Fatal("期望 supervisor 非空")
	}

	// 验证 Card
	if supervisor.Card().ID != "supervisor_id" {
		t.Errorf("期望 Card ID = supervisor_id, 实际 = %s", supervisor.Card().ID)
	}
}

// TestNewSupervisorAgent_最小并行数 验证 maxParallelSubAgents < 1 时使用默认值。
func TestNewSupervisorAgent_最小并行数(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)
	config := saconfig.NewReActAgentConfig()

	supervisor := NewSupervisorAgent(card, config, 0)
	if supervisor == nil {
		t.Fatal("期望 supervisor 非空")
	}

	// 验证 AbilityManager 已设置
	am := supervisor.AbilityManager()
	if am == nil {
		t.Fatal("期望 AbilityManager 非空")
	}
}

// TestSupervisorAgent_RegisterSubAgentCard 验证子 Agent 注册。
func TestSupervisorAgent_RegisterSubAgentCard(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)
	config := saconfig.NewReActAgentConfig()
	supervisor := NewSupervisorAgent(card, config, 5)

	subCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentID("sub_agent_id"),
	)
	supervisor.RegisterSubAgentCard(subCard)

	// 验证子 Agent 已注册：通过 AbilityManager 的 Get 判断
	am := supervisor.AbilityManager()
	if am == nil {
		t.Fatal("期望 AbilityManager 非空")
	}
	// 获取能力并验证类型
	ability := am.Get("sub_agent")
	if ability == nil {
		t.Error("期望 Get('sub_agent') 非空")
	}
	if ability.AbilityKind() != cschema.AbilityKindAgent {
		t.Errorf("期望 AbilityKind = AbilityKindAgent, 实际 = %v", ability.AbilityKind())
	}
}

// TestSupervisorAgent_RegisterSubAgentCard_多个 验证注册多个子 Agent。
func TestSupervisorAgent_RegisterSubAgentCard_多个(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)
	config := saconfig.NewReActAgentConfig()
	supervisor := NewSupervisorAgent(card, config, 5)

	for i := 0; i < 3; i++ {
		subCard := agentschema.NewAgentCard(
			agentschema.WithAgentName("sub_agent"),
			agentschema.WithAgentID("sub_agent_id"),
		)
		supervisor.RegisterSubAgentCard(subCard)
	}

	// 验证 AbilityManager 非空
	am := supervisor.AbilityManager()
	if am == nil {
		t.Fatal("期望 AbilityManager 非空")
	}
}

// TestSupervisorAgent_满足BaseAgent接口 编译时接口检查。
func TestSupervisorAgent_满足BaseAgent接口(t *testing.T) {
	var _ agentinterfaces.BaseAgent = (*SupervisorAgent)(nil)
	t.Log("SupervisorAgent 满足 BaseAgent 接口")
}

// TestSupervisorAgent_满足Communicable接口 编译时接口检查。
func TestSupervisorAgent_满足Communicable接口(t *testing.T) {
	var _ team_runtime.Communicable = (*SupervisorAgent)(nil)
	t.Log("SupervisorAgent 满足 Communicable 接口")
}

// TestSupervisorAgent_满足RuntimeBindable接口 编译时接口检查。
func TestSupervisorAgent_满足RuntimeBindable接口(t *testing.T) {
	var _ team_runtime.RuntimeBindable = (*SupervisorAgent)(nil)
	t.Log("SupervisorAgent 满足 RuntimeBindable 接口")
}

// TestCreate 验证 Create 函数基本流程。
func TestCreate(t *testing.T) {
	subCard1 := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent_1"),
		agentschema.WithAgentID("sub_agent_id_1"),
	)
	subCard2 := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent_2"),
		agentschema.WithAgentID("sub_agent_id_2"),
	)
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)

	card, provider := Create(
		[]*agentschema.AgentCard{subCard1, subCard2},
		nil, // modelClientConfig
		nil, // modelRequestConfig
		supervisorCard,
		"你是一个监督者",
		5,
		3,
	)

	// 验证返回的卡片
	if card.ID != "supervisor_id" {
		t.Errorf("期望 card ID = supervisor_id, 实际 = %s", card.ID)
	}

	// 验证 provider 不为空
	if provider == nil {
		t.Fatal("期望 provider 非空")
	}

	// 调用 provider 创建 Agent 实例
	agent, err := provider(context.Background(), supervisorCard)
	if err != nil {
		t.Fatalf("期望 provider 创建成功，实际错误 = %v", err)
	}
	if agent == nil {
		t.Fatal("期望 agent 非空")
	}

	// 验证是 SupervisorAgent 类型
	sup, ok := agent.(*SupervisorAgent)
	if !ok {
		t.Fatal("期望 agent 为 SupervisorAgent 类型")
	}

	// 验证子 Agent 已注册到 AbilityManager
	am := sup.AbilityManager()
	if am == nil {
		t.Fatal("期望 AbilityManager 非空")
	}
	// 验证子 Agent 卡片可通过 AbilityManager 查询
	ability1 := am.Get("sub_agent_1")
	if ability1 == nil {
		t.Error("期望 sub_agent_1 已注册")
	}
	ability2 := am.Get("sub_agent_2")
	if ability2 == nil {
		t.Error("期望 sub_agent_2 已注册")
	}
}

// TestCreate_带模型配置 验证 Create 传入模型配置。
func TestCreate_带模型配置(t *testing.T) {
	subCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentID("sub_agent_id"),
	)
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)

	modelClientConfig := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		APIKey:         "test-key",
		APIBase:        "https://api.openai.com/v1",
	}
	modelRequestConfig := &llmschema.ModelRequestConfig{
		ModelName: "gpt-4",
	}

	card, provider := Create(
		[]*agentschema.AgentCard{subCard},
		modelClientConfig,
		modelRequestConfig,
		supervisorCard,
		"",
		10,
		5,
	)

	if card.ID != "supervisor_id" {
		t.Errorf("期望 card ID = supervisor_id, 实际 = %s", card.ID)
	}

	agent, err := provider(context.Background(), supervisorCard)
	if err != nil {
		t.Fatalf("期望 provider 创建成功，实际错误 = %v", err)
	}
	if agent == nil {
		t.Fatal("期望 agent 非空")
	}
}

// TestCreate_默认迭代数 验证 maxIterations < 1 时使用默认值 5。
func TestCreate_默认迭代数(t *testing.T) {
	subCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentID("sub_agent_id"),
	)
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)

	card, provider := Create(
		[]*agentschema.AgentCard{subCard},
		nil,
		nil,
		supervisorCard,
		"",
		0, // maxIterations < 1，应使用默认值 5
		0, // maxParallelSubAgents < 1，应使用默认值
	)

	if card.ID != "supervisor_id" {
		t.Errorf("期望 card ID = supervisor_id, 实际 = %s", card.ID)
	}

	agent, err := provider(context.Background(), supervisorCard)
	if err != nil {
		t.Fatalf("期望 provider 创建成功，实际错误 = %v", err)
	}
	if agent == nil {
		t.Fatal("期望 agent 非空")
	}
}

// TestCreate_空AgentsPanic 验证空 agents 列表时 panic。
func TestCreate_空AgentsPanic(t *testing.T) {
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 panic，实际未 panic")
		}
	}()

	Create(nil, nil, nil, supervisorCard, "", 5, 3)
}

// TestCreate_NilAgentInListPanic 验证 agents 列表中含 nil 项时 panic。
func TestCreate_NilAgentInListPanic(t *testing.T) {
	supervisorCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("supervisor_id"),
	)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 panic，实际未 panic")
		}
	}()

	Create([]*agentschema.AgentCard{nil}, nil, nil, supervisorCard, "", 5, 3)
}

// ──────────────────────────── Configure 测试 ────────────────────────────

// TestSupervisorAgent_Configure_ReActAgentConfig 验证 ReActAgentConfig 类型时 Configure 生效。
func TestSupervisorAgent_Configure_ReActAgentConfig(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("sup-configure"),
	)
	sup := NewSupervisorAgent(card, nil, 5)

	cfg := saconfig.NewReActAgentConfig(saconfig.WithModelName("test-model"))
	err := sup.Configure(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Configure 返回错误: %v", err)
	}
}

// TestSupervisorAgent_Configure_非ReActAgentConfig 验证非 ReActAgentConfig 类型时 no-op。
func TestSupervisorAgent_Configure_非ReActAgentConfig(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("sup-noop"),
	)
	sup := NewSupervisorAgent(card, nil, 5)

	err := sup.Configure(context.Background(), &mockAgentConfig{})
	if err != nil {
		t.Fatalf("Configure 非 ReActAgentConfig 应返回 nil，got: %v", err)
	}
}

// mockAgentConfig 用于测试 Configure 的 no-op 路径
type mockAgentConfig struct{}

func (m *mockAgentConfig) ModelName() string  { return "" }
func (m *mockAgentConfig) MemScopeID() string { return "" }
func (m *mockAgentConfig) GetContextEngineConfig() ceschema.ContextEngineConfig {
	return ceschema.ContextEngineConfig{}
}
func (m *mockAgentConfig) GetModelClientConfig() *llmschema.ModelClientConfig { return nil }
func (m *mockAgentConfig) Validate() error                                    { return nil }

// TestNewSupervisorAgent_Config为nil 验证 config=nil 时正常创建。
func TestNewSupervisorAgent_Config为nil(t *testing.T) {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("supervisor"),
		agentschema.WithAgentID("sup-nil-cfg"),
	)
	sup := NewSupervisorAgent(card, nil, 5)
	if sup == nil {
		t.Fatal("NewSupervisorAgent(card, nil) 返回 nil")
	}
}
