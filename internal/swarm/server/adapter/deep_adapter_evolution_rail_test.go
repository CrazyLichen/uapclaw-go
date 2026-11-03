package adapter

import (
	"testing"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestBuildAgentRails_不构建演进与创建rail 冷启动装配不得构建两条按模式注册的 rail，
// 否则"字段非 nil ⇒ 已注册"的判据会被提前填充的字段打破。
func TestBuildAgentRails_不构建演进与创建rail(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"enabled": true, "skill_create": true, "auto_scan": true},
	}, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"

	railsList := d.buildAgentRails(map[string]any{}, map[string]any{}, "agent")

	if d.skillEvolutionRail != nil {
		t.Fatalf("冷启动不应构建 SkillEvolutionRail，实际 = %T", d.skillEvolutionRail)
	}
	if d.skillCreateRail != nil {
		t.Fatalf("冷启动不应构建 SkillCreateRail，实际 = %T", d.skillCreateRail)
	}
	for _, r := range railsList {
		switch r.(type) {
		case *coreevolution.SkillEvolutionRail, *coreevolution.TeamSkillCreateRail:
			t.Fatalf("冷启动 railsList 不应包含 %T", r)
		}
	}
}
