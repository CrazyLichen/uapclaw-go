package swarm

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestSwarmContextFactoryCoversDefault 覆盖注册后 MaterializeBuildContext
// 走新工厂（对齐 registry.py:198 注册语义 + blueprint.go:363 消费链）。
func TestSwarmContextFactoryCoversDefault(t *testing.T) {
	RegisterSwarmBuildContextFactory()
	// 幂等：二次调用安全
	RegisterSwarmBuildContextFactory()

	spec := &atschema.TeamAgentSpec{}
	spec.BuildContextSeed = map[string]any{
		"session_id": "s1", "team_id": "t1", "project_dir": "/proj", "mode": "team",
		"team_outputs_dir": "/artifacts/outputs",
	}
	if err := spec.MaterializeBuildContext(); err != nil {
		t.Fatal(err)
	}
	bc, ok := spec.BuildContext.(*SwarmBuildContext)
	if !ok {
		t.Fatalf("应重建为平台子类，got %T", spec.BuildContext)
	}
	if bc.TeamID != "t1" || bc.ProjectDir != "/proj" || bc.Mode != "team" {
		t.Fatalf("字段丢失: %+v", bc)
	}
	if bc.TeamOutputsDir != "/artifacts/outputs" {
		t.Fatalf("team_outputs_dir 丢失: %q", bc.TeamOutputsDir)
	}
}
