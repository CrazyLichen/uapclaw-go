package hierarchical

import (
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
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
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
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

// TestSupervisorAgent_RegisterSubAgentCard 验证子 Agent 注册。
func TestSupervisorAgent_RegisterSubAgentCard(t *testing.T) {
	card := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
	)
	config := saconfig.NewReActAgentConfig()
	supervisor := NewSupervisorAgent(card, config, 5)

	subCard := agentschema.NewAgentCard(
		cschema.WithName("sub_agent"),
		cschema.WithID("sub_agent_id"),
	)
	supervisor.RegisterSubAgentCard(subCard)

	// 验证子 Agent 已注册
	am := supervisor.ReActAgent.AbilityManager()
	if am == nil {
		t.Fatal("期望 AbilityManager 非空")
	}
	if !am.IsAgent("sub_agent") {
		t.Error("期望 IsAgent('sub_agent') = true")
	}
}

// TestSupervisorAgent_满足BaseAgent接口 编译时接口检查。
func TestSupervisorAgent_满足BaseAgent接口(t *testing.T) {
	var _ agentinterfaces.BaseAgent = (*SupervisorAgent)(nil)
	t.Log("SupervisorAgent 满足 BaseAgent 接口")
}

// TestSupervisorAgent_满足Communicable接口 编译时接口检查。
func TestSupervisorAgent_满足Communicable接口(t *testing.T) {
	var _ maschema.Communicable = (*SupervisorAgent)(nil)
	t.Log("SupervisorAgent 满足 Communicable 接口")
}

// TestSupervisorAgent_满足RuntimeBindable接口 编译时接口检查。
func TestSupervisorAgent_满足RuntimeBindable接口(t *testing.T) {
	var _ team_runtime.RuntimeBindable = (*SupervisorAgent)(nil)
	t.Log("SupervisorAgent 满足 RuntimeBindable 接口")
}
