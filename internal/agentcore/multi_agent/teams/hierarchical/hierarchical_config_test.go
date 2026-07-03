package hierarchical

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHierarchicalTeamConfig 验证默认配置。
func TestNewHierarchicalTeamConfig(t *testing.T) {
	cfg := NewHierarchicalTeamConfig()
	if cfg.Timeout != defaultP2PTimeout {
		t.Errorf("期望 Timeout = %v, 实际 = %v", defaultP2PTimeout, cfg.Timeout)
	}
	if cfg.SupervisorAgent != nil {
		t.Errorf("期望 SupervisorAgent = nil, 实际 = %v", cfg.SupervisorAgent)
	}
}

// TestHierarchicalTeamConfig_自定义值 验证自定义配置。
func TestHierarchicalTeamConfig_自定义值(t *testing.T) {
	cfg := &HierarchicalTeamConfig{
		Timeout: 600.0,
	}
	if cfg.Timeout != 600.0 {
		t.Errorf("期望 Timeout = 600.0, 实际 = %v", cfg.Timeout)
	}
}
