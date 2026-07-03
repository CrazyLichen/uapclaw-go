package hierarchical_tools

import (
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHierarchicalToolsTeamConfig 验证默认配置。
func TestNewHierarchicalToolsTeamConfig(t *testing.T) {
	cfg := NewHierarchicalToolsTeamConfig()
	if cfg.RootAgent != nil {
		t.Errorf("期望 RootAgent = nil, 实际 = %v", cfg.RootAgent)
	}
}

// TestHierarchicalToolsTeamConfig_自定义值 验证自定义配置。
func TestHierarchicalToolsTeamConfig_自定义值(t *testing.T) {
	cfg := &HierarchicalToolsTeamConfig{}
	if cfg.RootAgent != nil {
		t.Errorf("期望 RootAgent = nil, 实际 = %v", cfg.RootAgent)
	}
}

// TestHierarchicalToolsTeamConfig_设置RootAgent 验证设置 RootAgent。
func TestHierarchicalToolsTeamConfig_设置RootAgent(t *testing.T) {
	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	cfg := &HierarchicalToolsTeamConfig{
		RootAgent: rootCard,
	}
	if cfg.RootAgent == nil {
		t.Fatal("期望 RootAgent 非空")
	}
	if cfg.RootAgent.ID != "root_id" {
		t.Errorf("期望 RootAgent.ID = root_id, 实际 = %s", cfg.RootAgent.ID)
	}
}

// TestHierarchicalToolsTeamConfig_嵌入TeamConfig 验证嵌入 TeamConfig 字段。
func TestHierarchicalToolsTeamConfig_嵌入TeamConfig(t *testing.T) {
	teamCfg := maschema.NewTeamConfig()
	teamCfg.ConfigureMaxAgents(5)

	cfg := &HierarchicalToolsTeamConfig{
		TeamConfig: *teamCfg,
	}
	if cfg.TeamConfig.MaxAgents != 5 {
		t.Errorf("期望 MaxAgents = 5, 实际 = %d", cfg.TeamConfig.MaxAgents)
	}
}
