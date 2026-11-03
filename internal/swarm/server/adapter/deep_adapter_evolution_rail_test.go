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

// newRailBuildAdapter 构造只用于 rail 构建的 adapter。
func newRailBuildAdapter(t *testing.T, cache map[string]any) *DeepAdapter {
	t.Helper()
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(cache, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"
	return d
}

// TestBuildSkillEvolutionRail_参数矩阵 构造参数必须与 Python 对齐：
// auto_scan 缺省关闭、env 认 1/yes、auto_save 恒 false、sharing 不接、model_name 兜底。
func TestBuildSkillEvolutionRail_参数矩阵(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	t.Run("缺auto_scan默认关闭", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true}})
		rail := d.buildSkillEvolutionRail()
		if rail == nil {
			t.Fatal("rail 不应为 nil")
		}
		if rail.AutoScan() {
			t.Fatal("auto_scan 缺省必须为 false（对齐 Python :1955）")
		}
	})

	t.Run("环境变量1开启", func(t *testing.T) {
		t.Setenv("EVOLUTION_AUTO_SCAN", "1")
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true, "auto_scan": false}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || !rail.AutoScan() {
			t.Fatal("EVOLUTION_AUTO_SCAN=1 应开启 auto_scan（对齐 Python :1953）")
		}
	})

	t.Run("环境变量yes开启", func(t *testing.T) {
		t.Setenv("EVOLUTION_AUTO_SCAN", "yes")
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || !rail.AutoScan() {
			t.Fatal("EVOLUTION_AUTO_SCAN=yes 应开启 auto_scan")
		}
	})

	t.Run("auto_save被忽略", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true, "auto_save": true}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.AutoSave() {
			t.Fatal("auto_save 必须恒为 false（Python :1962 硬编码）")
		}
	})

	t.Run("sharing被忽略", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{
			"enabled": true, "sharing": map[string]any{"enabled": true},
		}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.IsSharingEnabled() {
			t.Fatal("adapter 路径不应接 sharing（Python builder 不传 sharing_config）")
		}
	})

	t.Run("model_name兜底配置", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{"enabled": true}, "model_name": "cfg-model"})
		d.defaultModelName = ""
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.ModelName() != "cfg-model" {
			t.Fatalf("model_name 应回落到 config.model_name")
		}
	})
}

// TestBuildSkillCreateRail_未启用 未开启时返回 nil 且不写字段。
func TestBuildSkillCreateRail_未启用(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{"evolution": map[string]any{"skill_create": false}}, map[string]any{})

	if rail := d.buildSkillCreateRail(); rail != nil {
		t.Fatalf("skill_create=false 应返回 nil，实际 %T", rail)
	}
	if d.skillCreateRail != nil {
		t.Fatal("builder 不得写字段")
	}
}

// TestBuildSkillCreateRail_启用返回单Agent版 启用时构建单 Agent 版且 auto_trigger 为真。
func TestBuildSkillCreateRail_启用返回单Agent版(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"skill_create": true},
		"language":  "cn",
	}, map[string]any{})

	rail := d.buildSkillCreateRail()
	if rail == nil {
		t.Fatal("skill_create=true 应构建 rail")
	}
	if !rail.AutoTrigger() {
		t.Fatal("auto_trigger 必须恒为 true（Python :1988）")
	}
	if d.skillCreateRail != nil {
		t.Fatal("builder 不得写字段，由调用方在注册成功后赋值")
	}
}
