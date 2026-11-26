package adapter

import (
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	coreevolutionreview "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	skillrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// TestBuildAgentRails_不构建演进与创建rail 冷启动装配不得构建两条按模式注册的 rail，
// 否则"字段非 nil ⇒ 已注册"的判据会被提前填充的字段打破。
func TestBuildAgentRails_不构建演进与创建rail(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"skill_evolution": true, "skill_create": true, "auto_scan": true},
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
		case *coreevolution.SkillEvolutionRail, *skillrails.TeamSkillCreateRail:
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
		coreevolution.WithSignalTrigger(false),
		coreevolution.WithAutoSave(false),
		coreevolution.WithReviewTrigger(true),
		coreevolution.WithEvolutionReviewRuntime(
			coreevolutionreview.NewEvolutionReviewRuntime(60.0)),
		coreevolution.WithEvolutionTrajectorySpanProcessor(demandSpanProcessorOf()),
	)
}

// TestUpdateSkillEvolutionRail_就地更新 热重载只换 LLM 与 auto_save，实例不变；
// signal_trigger 不随热更新（Python 新版个人 rail 构造恒 False，无热更新点）。
// Python: _get_current_agent_rails 顶部（interface_deep.py:12328-12333）。
func TestUpdateSkillEvolutionRail_就地更新(t *testing.T) {
	d := NewDeepAdapter()
	rail := newTestEvolutionRail(t, "old-model")
	d.skillEvolutionRail = rail
	d.model = usableTestModel()
	d.defaultModelName = "new-model"

	// 单参签名（24.15 收敛）：auto_save 读 configBase 的 react.evolution.auto_save
	d.updateSkillEvolutionRail(map[string]any{
		"react": map[string]any{"evolution": map[string]any{"auto_save": true}},
	})

	if d.skillEvolutionRail != rail {
		t.Fatal("就地更新不得替换 rail 实例")
	}
	if got := rail.ModelName(); got != "new-model" {
		t.Fatalf("ModelName = %q, want new-model", got)
	}
	if !rail.AutoSave() {
		t.Fatal("auto_save 应跟随配置更新为 true（Python :12330-12333）")
	}
	if rail.SignalTrigger() {
		t.Fatal("signal_trigger 不得随热更新翻转（Python 新版恒 False）")
	}
}

// newRailBuildAdapter 构造只用于 rail 构建的 adapter。
func newRailBuildAdapter(t *testing.T, base map[string]any) *DeepAdapter {
	t.Helper()
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{}, base)
	d.model = usableTestModel()
	d.defaultModelName = "test-model"
	return d
}

// TestBuildSkillEvolutionRail_参数矩阵 构造参数必须与 Python 新版对齐
// （interface_deep.py:10236-10259）：signal_trigger 恒 False、auto_save 跟随
// config evolution_auto_save、review_trigger 恒 True、review_runtime 必传、
// processor 注入、sharing 不接、model_name 兜底 gpt-4。
func TestBuildSkillEvolutionRail_参数矩阵(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	t.Run("signal_trigger恒False", func(t *testing.T) {
		t.Setenv("EVOLUTION_AUTO_SCAN", "1") // 环境变量不再影响个人 rail（Python :10250 恒 False）
		d := newRailBuildAdapter(t, map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}}})
		rail := d.buildSkillEvolutionRail()
		if rail == nil || rail.SignalTrigger() {
			t.Fatal("signal_trigger 必须恒为 false（Python :10250）")
		}
	})

	t.Run("auto_save跟随配置", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true, "auto_save": true}}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || !rail.AutoSave() {
			t.Fatal("auto_save 应跟随 config（Python :10253）")
		}
		d2 := newRailBuildAdapter(t, map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}}})
		if rail := d2.buildSkillEvolutionRail(); rail == nil || rail.AutoSave() {
			t.Fatal("auto_save 缺省为 false（Python :10253）")
		}
	})

	t.Run("review_trigger恒True且runtime必传", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}}})
		rail := d.buildSkillEvolutionRail()
		if rail == nil || !rail.ReviewTrigger() {
			t.Fatal("review_trigger 必须恒为 true（Python :10251）")
		}
		if rail.ReviewRuntime() == nil {
			t.Fatal("review_runtime 必传（Python :10249）")
		}
	})

	t.Run("sharing被忽略", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"evolution": map[string]any{
			"skill_evolution": true, "sharing": map[string]any{"enabled": true},
		}})
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.IsSharingEnabled() {
			t.Fatal("adapter 路径不应接 sharing（Python builder 不传 sharing_config）")
		}
	})

	t.Run("model_name兜底gpt4", func(t *testing.T) {
		d := newRailBuildAdapter(t, map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}}})
		d.defaultModelName = ""
		if rail := d.buildSkillEvolutionRail(); rail == nil || rail.ModelName() != "gpt-4" {
			t.Fatalf("model_name 应回落 gpt-4（Python :10252 兜底）")
		}
	})
}

// TestBuildSkillCreateRail_未启用 未开启时返回 nil 且不写字段。
func TestBuildSkillCreateRail_未启用(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true, "skill_create": false}}}, map[string]any{})

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
	d.setConfigSnapshots(map[string]any{"language": "cn"}, map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}},
	})

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

// TestBuildDynamicRail_写字段 动态路径构建演进 rail 后必须登记字段。
func TestBuildDynamicRail_写字段(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}}}, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"

	rail := d.buildDynamicRail("buildSkillEvolutionRail", map[string]any{})
	if rail == nil {
		t.Fatal("动态路径应构建出 rail")
	}
	if d.skillEvolutionRail == nil {
		t.Fatal("动态路径必须把实例登记到字段，否则注册后无人可追踪")
	}
	if d.skillEvolutionRail != rail {
		t.Fatal("字段与返回值必须是同一实例")
	}
}

// TestGetCurrentAgentRails_仅skill重建其余复用 热重载只新建 skillRail，
// 其余 5 个 rail 复用现有实例，有状态 rail 不进列表。
func TestGetCurrentAgentRails_仅skill重建其余复用(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	pathutil.ResetCache()
	d := NewDeepAdapter()
	d.setConfigSnapshots(map[string]any{
		"evolution": map[string]any{"skill_evolution": true, "auto_scan": false},
	}, map[string]any{})
	d.model = usableTestModel()
	d.defaultModelName = "test-model"
	d.instanceOverrides = map[string]any{}

	reused := []sainterfaces.AgentRail{
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
		rails.NewHeartbeatRail(),
	}
	d.contextAssembleRail = reused[0]
	d.contextProcessorRail = reused[1]
	d.memoryRail = reused[2]
	d.avatarRail = reused[3]
	d.permissionRail = reused[4]

	railsList := d.getCurrentAgentRails(map[string]any{}, map[string]any{})

	if d.skillRail == nil {
		t.Fatal("skillRail 应被重建")
	}
	for i, want := range reused {
		found := false
		for _, r := range railsList {
			if r == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("复用实例 #%d 应出现在热重载列表中", i)
		}
	}
	// 有状态 rail 不得入列（否则会被重建并丢内存状态）
	for _, r := range railsList {
		if _, ok := r.(*rails.TaskPlanningRail); ok {
			t.Fatal("TaskPlanningRail 不得进入热重载列表")
		}
		if _, ok := r.(*commrails.JiuClawStreamEventRail); ok {
			t.Fatal("StreamEventRail 不得进入热重载列表")
		}
	}
}
