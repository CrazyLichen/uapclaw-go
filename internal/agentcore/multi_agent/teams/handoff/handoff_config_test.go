package handoff

import (
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHandoffConfig_默认值 测试 NewHandoffConfig 返回默认配置
func TestNewHandoffConfig_默认值(t *testing.T) {
	cfg := NewHandoffConfig()

	if cfg.StartAgent != nil {
		t.Errorf("期望 StartAgent 为 nil，实际不为 nil")
	}
	if cfg.MaxHandoffs != 10 {
		t.Errorf("期望 MaxHandoffs = 10，实际 = %d", cfg.MaxHandoffs)
	}
	if cfg.Routes != nil {
		t.Errorf("期望 Routes 为 nil，实际不为 nil")
	}
	if cfg.TerminationCondition != nil {
		t.Errorf("期望 TerminationCondition 为 nil，实际不为 nil")
	}
}

// TestHandoffRoute_字段正确 测试 HandoffRoute 结构体字段赋值
func TestHandoffRoute_字段正确(t *testing.T) {
	route := HandoffRoute{
		Source: "agent_a",
		Target: "agent_b",
	}

	if route.Source != "agent_a" {
		t.Errorf("期望 Source = agent_a，实际 = %s", route.Source)
	}
	if route.Target != "agent_b" {
		t.Errorf("期望 Target = agent_b，实际 = %s", route.Target)
	}
}

// TestHandoffConfig_自定义值 测试 HandoffConfig 自定义赋值
func TestHandoffConfig_自定义值(t *testing.T) {
	card := agentschema.NewAgentCard()
	termCond := func(_ *HandoffOrchestrator) bool { return true }
	routes := []HandoffRoute{
		{Source: "a", Target: "b"},
		{Source: "b", Target: "c"},
	}

	cfg := &HandoffConfig{
		StartAgent:           card,
		MaxHandoffs:          5,
		Routes:               routes,
		TerminationCondition: termCond,
	}

	if cfg.StartAgent != card {
		t.Errorf("期望 StartAgent 为指定卡片")
	}
	if cfg.MaxHandoffs != 5 {
		t.Errorf("期望 MaxHandoffs = 5，实际 = %d", cfg.MaxHandoffs)
	}
	if len(cfg.Routes) != 2 {
		t.Errorf("期望 Routes 长度 = 2，实际 = %d", len(cfg.Routes))
	}
	if cfg.TerminationCondition == nil {
		t.Errorf("期望 TerminationCondition 不为 nil")
	}
}

// TestNewHandoffTeamConfig_默认值 测试 NewHandoffTeamConfig 嵌入 TeamConfig + Handoff
func TestNewHandoffTeamConfig_默认值(t *testing.T) {
	cfg := NewHandoffTeamConfig()

	// 验证 TeamConfig 默认值
	if cfg.MaxAgents != 10 {
		t.Errorf("期望 MaxAgents = 10，实际 = %d", cfg.MaxAgents)
	}
	if cfg.MaxConcurrentMessages != 100 {
		t.Errorf("期望 MaxConcurrentMessages = 100，实际 = %d", cfg.MaxConcurrentMessages)
	}
	if cfg.MessageTimeout != 30.0 {
		t.Errorf("期望 MessageTimeout = 30.0，实际 = %f", cfg.MessageTimeout)
	}

	// 验证 Handoff 默认值
	if cfg.Handoff.MaxHandoffs != 10 {
		t.Errorf("期望 Handoff.MaxHandoffs = 10，实际 = %d", cfg.Handoff.MaxHandoffs)
	}
	if cfg.Handoff.StartAgent != nil {
		t.Errorf("期望 Handoff.StartAgent 为 nil")
	}
	if cfg.Handoff.Routes != nil {
		t.Errorf("期望 Handoff.Routes 为 nil")
	}
}

// TestHandoffTeamConfig_链式配置 测试通过 TeamConfig 继承的链式配置方法
func TestHandoffTeamConfig_链式配置(t *testing.T) {
	cfg := NewHandoffTeamConfig()

	// 验证 ConfigureMaxAgents 链式调用（提升方法返回 *TeamConfig）
	result := cfg.ConfigureMaxAgents(20)
	if result != &cfg.TeamConfig {
		t.Errorf("期望 ConfigureMaxAgents 返回嵌入的 TeamConfig 引用")
	}
	if cfg.MaxAgents != 20 {
		t.Errorf("期望 MaxAgents = 20，实际 = %d", cfg.MaxAgents)
	}

	// 验证 ConfigureTimeout 链式调用
	result = cfg.ConfigureTimeout(60.0)
	if result != &cfg.TeamConfig {
		t.Errorf("期望 ConfigureTimeout 返回嵌入的 TeamConfig 引用")
	}
	if cfg.MessageTimeout != 60.0 {
		t.Errorf("期望 MessageTimeout = 60.0，实际 = %f", cfg.MessageTimeout)
	}

	// 验证链式组合（在 TeamConfig 层面链式调用）
	cfg2 := NewHandoffTeamConfig()
	cfg2.TeamConfig.ConfigureMaxAgents(5).ConfigureTimeout(10.0)
	if cfg2.MaxAgents != 5 {
		t.Errorf("期望 MaxAgents = 5，实际 = %d", cfg2.MaxAgents)
	}
	if cfg2.MessageTimeout != 10.0 {
		t.Errorf("期望 MessageTimeout = 10.0，实际 = %f", cfg2.MessageTimeout)
	}
}

// TestHandoffTeamConfig_嵌入TeamConfig方法 测试嵌入的 TeamConfig 方法可直接调用
func TestHandoffTeamConfig_嵌入TeamConfig方法(t *testing.T) {
	cfg := NewHandoffTeamConfig()

	// 验证 ConfigureConcurrency 继承可用（提升方法返回 *TeamConfig）
	result := cfg.ConfigureConcurrency(50)
	if result != &cfg.TeamConfig {
		t.Errorf("期望 ConfigureConcurrency 返回嵌入的 TeamConfig 引用")
	}
	if cfg.MaxConcurrentMessages != 50 {
		t.Errorf("期望 MaxConcurrentMessages = 50，实际 = %d", cfg.MaxConcurrentMessages)
	}

	// 验证 SetExtra/GetExtra 继承可用
	cfg.SetExtra("custom_key", "custom_value")
	val, ok := cfg.GetExtra("custom_key")
	if !ok || val != "custom_value" {
		t.Errorf("期望 GetExtra 返回 custom_value，实际 = %v, ok = %v", val, ok)
	}
}

// TestHandoffTeamConfig_与TeamConfig类型兼容 测试 HandoffTeamConfig.TeamConfig 可作为 TeamConfig 使用
func TestHandoffTeamConfig_与TeamConfig类型兼容(t *testing.T) {
	cfg := NewHandoffTeamConfig()

	// 提取嵌入的 TeamConfig 引用，验证与 maschema.TeamConfig 类型兼容
	teamCfg := &cfg.TeamConfig
	if teamCfg.MaxAgents != 10 {
		t.Errorf("期望 TeamConfig.MaxAgents = 10，实际 = %d", teamCfg.MaxAgents)
	}
}
