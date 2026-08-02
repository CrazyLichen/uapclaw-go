package experience

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewOnlineEvolutionOrchestrator 测试编排器创建
func TestNewOnlineEvolutionOrchestrator(t *testing.T) {
	orch := NewOnlineEvolutionOrchestrator(nil, nil, nil, nil, "test_prefix", "test_source")
	if orch.requestIDPrefix != "test_prefix" {
		t.Errorf("requestIDPrefix = %s, 期望 test_prefix", orch.requestIDPrefix)
	}
	if orch.stageSource != "test_source" {
		t.Errorf("stageSource = %s, 期望 test_source", orch.stageSource)
	}
}
