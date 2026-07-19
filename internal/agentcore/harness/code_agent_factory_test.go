package harness

import (
	"context"
	"reflect"
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestCreateCodeAgent_注入PlanAgent和ExploreAgent 验证无子 Agent 时自动注入
func TestCreateCodeAgent_注入PlanAgent和ExploreAgent(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model: model,
	}

	agent, err := CreateCodeAgent(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateCodeAgent 失败: %v", err)
	}
	if agent == nil {
		t.Fatal("agent 不应为 nil")
	}

	// 验证子 Agent 中包含 explore_agent 和 plan_agent
	cfg := agent.DeepConfig()
	if cfg == nil {
		t.Fatal("config 不应为 nil")
	}
	foundExplore := false
	foundPlan := false
	for _, spec := range cfg.Subagents {
		name := spec.SpecName()
		if name == "explore_agent" {
			foundExplore = true
		}
		if name == "plan_agent" {
			foundPlan = true
		}
	}
	if !foundExplore {
		t.Error("应自动注入 explore_agent")
	}
	if !foundPlan {
		t.Error("应自动注入 plan_agent")
	}
}

// TestCreateCodeAgent_已有ExploreAgent不重复注入 验证去重
func TestCreateCodeAgent_已有ExploreAgent不重复注入(t *testing.T) {
	model := &llm.Model{}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("explore_agent"),
		agentschema.WithAgentDescription("已有"),
	)
	params := &hschema.SubagentCreateParams{
		Model: model,
		Subagents: []hschema.SubAgentConfig{
			{AgentCard: card},
		},
	}

	agent, err := CreateCodeAgent(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateCodeAgent 失败: %v", err)
	}

	cfg := agent.DeepConfig()
	exploreCount := 0
	for _, spec := range cfg.Subagents {
		if spec.SpecName() == "explore_agent" {
			exploreCount++
		}
	}
	if exploreCount != 1 {
		t.Errorf("explore_agent 应只出现 1 次，实际 %d 次", exploreCount)
	}
}

// TestCreateCodeAgent_合并必需Rails 验证无 Rails 时不报错，且 FindRailsByType 可找到必需 Rails
func TestCreateCodeAgent_合并必需Rails(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model: model,
	}

	agent, err := CreateCodeAgent(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateCodeAgent 失败: %v", err)
	}

	// Rails 通过 AddRail 注册到 agent，使用 FindRailsByType 验证
	sysOpType := reflect.TypeOf((*rails.SysOperationRail)(nil))
	agentModeType := reflect.TypeOf((*rails.AgentModeRail)(nil))
	askUserType := reflect.TypeOf((*interrupt.AskUserRail)(nil))
	confirmType := reflect.TypeOf((*interrupt.ConfirmInterruptRail)(nil))

	if len(agent.FindRailsByType(sysOpType)) == 0 {
		t.Error("应包含 SysOperationRail")
	}
	if len(agent.FindRailsByType(agentModeType)) == 0 {
		t.Error("应包含 AgentModeRail")
	}
	if len(agent.FindRailsByType(askUserType)) == 0 {
		t.Error("应包含 AskUserRail")
	}
	if len(agent.FindRailsByType(confirmType)) == 0 {
		t.Error("应包含 ConfirmInterruptRail")
	}
}

// TestCreateCodeAgent_用户已有SysOperationRail不重复 验证去重
func TestCreateCodeAgent_用户已有SysOperationRail不重复(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model: model,
		Rails: []sainterfaces.AgentRail{
			rails.NewSysOperationRail(),
		},
	}

	agent, err := CreateCodeAgent(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateCodeAgent 失败: %v", err)
	}

	// 通过 FindRailsByType 检查只有 1 个 SysOperationRail
	sysOpType := reflect.TypeOf((*rails.SysOperationRail)(nil))
	if len(agent.FindRailsByType(sysOpType)) != 1 {
		t.Errorf("SysOperationRail 应只出现 1 次，实际 %d 次", len(agent.FindRailsByType(sysOpType)))
	}
}

// TestCreateCodeAgent_默认SystemPrompt 验证默认系统提示词包含关键内容
func TestCreateCodeAgent_默认SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model: model,
	}

	agent, err := CreateCodeAgent(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateCodeAgent 失败: %v", err)
	}

	cfg := agent.DeepConfig()
	if cfg.SystemPrompt == "" {
		t.Error("SystemPrompt 不应为空")
	}
	// 对齐 Python: DEFAULT_CODE_AGENT_SYSTEM_PROMPT 包含 "AI Coding Agent"
	if len(cfg.SystemPrompt) < 10 {
		t.Error("SystemPrompt 应有足够长度")
	}
}

// TestCreateCodeAgent_默认AgentCard 验证默认 AgentCard 名称
func TestCreateCodeAgent_默认AgentCard(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{
		Model: model,
	}

	agent, err := CreateCodeAgent(context.Background(), params)
	if err != nil {
		t.Fatalf("CreateCodeAgent 失败: %v", err)
	}

	cfg := agent.DeepConfig()
	if cfg.Card == nil {
		t.Fatal("Card 不应为 nil")
	}
	if cfg.Card.Name != "code_agent" {
		t.Errorf("Card.Name 期望 %q，实际 %q", "code_agent", cfg.Card.Name)
	}
}

// --- 辅助函数测试 ---

// TestMergeRailsWithRequired_全部缺失 验证 4 个必需 Rails 全部注入
func TestMergeRailsWithRequired_全部缺失(t *testing.T) {
	userRails := []sainterfaces.AgentRail{}
	required := []requiredRailEntry{
		{railType: (*rails.SysOperationRail)(nil), factory: func() sainterfaces.AgentRail { return rails.NewSysOperationRail() }},
		{railType: (*rails.AgentModeRail)(nil), factory: func() sainterfaces.AgentRail { return rails.NewAgentModeRail(nil) }},
		{railType: (*interrupt.AskUserRail)(nil), factory: func() sainterfaces.AgentRail { return interrupt.NewAskUserRail() }},
		{railType: (*interrupt.ConfirmInterruptRail)(nil), factory: func() sainterfaces.AgentRail { return interrupt.NewConfirmInterruptRail("switch_mode") }},
	}

	result := mergeRailsWithRequired(userRails, required)

	if len(result) != 4 {
		t.Errorf("期望 4 个 Rails，实际 %d 个", len(result))
	}
}

// TestMergeRailsWithRequired_部分已存在 验证只注入缺失的
func TestMergeRailsWithRequired_部分已存在(t *testing.T) {
	userRails := []sainterfaces.AgentRail{
		rails.NewSysOperationRail(),
	}
	required := []requiredRailEntry{
		{railType: (*rails.SysOperationRail)(nil), factory: func() sainterfaces.AgentRail { return rails.NewSysOperationRail() }},
		{railType: (*rails.AgentModeRail)(nil), factory: func() sainterfaces.AgentRail { return rails.NewAgentModeRail(nil) }},
		{railType: (*interrupt.AskUserRail)(nil), factory: func() sainterfaces.AgentRail { return interrupt.NewAskUserRail() }},
		{railType: (*interrupt.ConfirmInterruptRail)(nil), factory: func() sainterfaces.AgentRail { return interrupt.NewConfirmInterruptRail("switch_mode") }},
	}

	result := mergeRailsWithRequired(userRails, required)

	if len(result) != 4 {
		t.Errorf("期望 4 个 Rails（1 用户 + 3 注入），实际 %d 个", len(result))
	}
	// 验证只有 1 个 SysOperationRail
	sysOpCount := 0
	for _, r := range result {
		if _, ok := r.(*rails.SysOperationRail); ok {
			sysOpCount++
		}
	}
	if sysOpCount != 1 {
		t.Errorf("SysOperationRail 应只出现 1 次，实际 %d 次", sysOpCount)
	}
}

// TestInjectBuiltinPlanAgents_全部缺失 验证注入 explore + plan
func TestInjectBuiltinPlanAgents_全部缺失(t *testing.T) {
	model := &llm.Model{}
	subs := []hschema.SubagentSpec{}

	result := injectBuiltinPlanAgents(subs, model, "cn")

	if len(result) != 2 {
		t.Fatalf("期望 2 个子 Agent，实际 %d 个", len(result))
	}
	names := make(map[string]bool)
	for _, s := range result {
		names[s.SpecName()] = true
	}
	if !names["explore_agent"] {
		t.Error("应包含 explore_agent")
	}
	if !names["plan_agent"] {
		t.Error("应包含 plan_agent")
	}
}

// TestInjectBuiltinPlanAgents_部分已存在 验证只注入缺失的
func TestInjectBuiltinPlanAgents_部分已存在(t *testing.T) {
	model := &llm.Model{}
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("plan_agent"),
		agentschema.WithAgentDescription("已有"),
	)
	subs := []hschema.SubagentSpec{
		&hschema.SubAgentConfig{AgentCard: card},
	}

	result := injectBuiltinPlanAgents(subs, model, "cn")

	planCount := 0
	for _, s := range result {
		if s.SpecName() == "plan_agent" {
			planCount++
		}
	}
	if planCount != 1 {
		t.Errorf("plan_agent 应只出现 1 次，实际 %d 次", planCount)
	}
	if len(result) != 2 {
		t.Errorf("期望 2 个子 Agent（1 已有 plan + 1 注入 explore），实际 %d 个", len(result))
	}
}
