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

// newTestEvolutionRail 构造一个带可用 model 的演进 rail。
func newTestEvolutionRail(t *testing.T, modelName string) *coreevolution.SkillEvolutionRail {
	t.Helper()
	return coreevolution.NewSkillEvolutionRail(
		[]string{t.TempDir()},
		usableTestModel(),
		modelName,
		"cn",
		coreevolution.WithAutoScan(false),
		coreevolution.WithAutoSave(false),
	)
}

// TestUpdateSkillEvolutionRail_就地更新 热重载必须就地换 LLM 与 auto_scan，实例不变。
func TestUpdateSkillEvolutionRail_就地更新(t *testing.T) {
	d := NewDeepAdapter()
	rail := newTestEvolutionRail(t, "old-model")
	d.skillEvolutionRail = rail
	d.model = usableTestModel()
	d.defaultModelName = "new-model"

	d.updateSkillEvolutionRail(map[string]any{
		"evolution": map[string]any{"auto_scan": true},
	})

	if d.skillEvolutionRail != rail {
		t.Fatal("就地更新不得替换 rail 实例")
	}
	if got := rail.ModelName(); got != "new-model" {
		t.Fatalf("ModelName = %q, want new-model", got)
	}
	if !rail.AutoScan() {
		t.Fatal("auto_scan 应跟随配置更新为 true")
	}
}

// TestUpdateSkillEvolutionRail_环境变量优先 EVOLUTION_AUTO_SCAN=1 覆盖配置里的 false。
func TestUpdateSkillEvolutionRail_环境变量优先(t *testing.T) {
	t.Setenv("EVOLUTION_AUTO_SCAN", "1")
	d := NewDeepAdapter()
	rail := newTestEvolutionRail(t, "m")
	d.skillEvolutionRail = rail
	d.model = usableTestModel()
	d.defaultModelName = "m"

	d.updateSkillEvolutionRail(map[string]any{
		"evolution": map[string]any{"auto_scan": false},
	})

	if !rail.AutoScan() {
		t.Fatal("环境变量 1 应覆盖配置的 false")
	}
}
