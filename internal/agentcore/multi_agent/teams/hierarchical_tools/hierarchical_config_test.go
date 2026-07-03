package hierarchical_tools

import "testing"

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
