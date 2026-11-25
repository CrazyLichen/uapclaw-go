package evolution

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPrepareEvolutionReviewScope_无活跃invoke无捕获 对齐
// skill_evolution_rail.py:471-474 第一条 RuntimeError。
func TestPrepareEvolutionReviewScope_无活跃invoke无捕获(t *testing.T) {
	r := newConstructedRail(t)
	_, err := r.PrepareEvolutionReviewScope("agent_detected_signal",
		map[string]any{"kind": "skill", "name": "demo"}, "sess-1", "", "", "")
	require.Error(t, err)
	assert.Equal(t, "evolution review must be prepared during an active invoke", err.Error())
}

// TestPrepareEvolutionReviewScope_有活跃捕获但会话不匹配 对齐
// skill_evolution_rail.py:472-474 第二条 RuntimeError。
func TestPrepareEvolutionReviewScope_有活跃捕获但会话不匹配(t *testing.T) {
	r := newConstructedRail(t)
	// 手工登记一个其他会话的活跃采集（skill_evolution_rail.py:473 has_active_capture）
	r.beginScopeCapture("sess-other", "", "")
	_, err := r.PrepareEvolutionReviewScope("agent_detected_signal",
		map[string]any{"kind": "skill", "name": "demo"}, "sess-1", "", "", "")
	require.Error(t, err)
	assert.Equal(t, "review session does not match the active trajectory scope", err.Error())
}

// TestPrepareEvolutionReviewScope_无干净轨迹 对齐
// skill_evolution_rail.py:479-482 第三条 RuntimeError。
func TestPrepareEvolutionReviewScope_无干净轨迹(t *testing.T) {
	r := newConstructedRail(t)
	r.beginScopeCapture("sess-1", "", "")
	_, err := r.PrepareEvolutionReviewScope("agent_detected_signal",
		map[string]any{"kind": "skill", "name": "demo"}, "sess-1", "", "", "")
	require.Error(t, err)
	assert.Equal(t, "no clean trajectory is available for evolution review", err.Error())
}

// TestPrepareEvolutionReviewScope_成功创建scope 对齐成功路径（:484-493）：
// scope 经 review_runtime.CreateScope 创建 + skipSignalTriggerThisInvoke=true。
func TestPrepareEvolutionReviewScope_成功创建scope(t *testing.T) {
	r := newConstructedRail(t)
	r.reviewRuntime = review.NewEvolutionReviewRuntime(60)
	r.beginScopeCapture("sess-1", "", "")
	// 注入干净窗口轨迹（GetTrajectory 读 scopeWindows）
	key := scopeKeyOf("agent", "sess-1", "")
	traj := trajectory.NewTestTrajectoryWithIDWithSession("sess-1")
	r.scopeMu.Lock()
	r.scopeWindows[key] = &scopeWindowState{trajectory: traj}
	r.scopeMu.Unlock()

	launch, err := r.PrepareEvolutionReviewScope("agent_detected_signal",
		map[string]any{"kind": "skill", "name": "demo"}, "sess-1", "", "", "表格加汇总行")
	require.NoError(t, err)
	assert.NotEmpty(t, launch.EvolutionReviewRef)
	assert.True(t, strings.HasPrefix(launch.EvolutionReviewRef, "evrr_"))
	assert.True(t, r.skipSignalTriggerThisInvoke)
}

// TestResolveCaptureByScope_匹配语义 对齐 _capture_matches（:623-637）：
// 当前采集按全部给定定位符匹配；sessionID 为空时不按 scope 兜底查表。
func TestResolveCaptureByScope_匹配语义(t *testing.T) {
	r := newConstructedRail(t)
	capture := r.beginScopeCapture("sess-1", "", "")
	assert.Same(t, capture, r.resolveCaptureByScope("sess-1", "", ""))
	// member_id 给定且与采集不符 → 不匹配（_capture_matches 全定位符匹配语义）
	assert.Nil(t, r.resolveCaptureByScope("sess-1", "m-1", ""))
	assert.Nil(t, r.resolveCaptureByScope("sess-2", "", ""))
	r.endScopeCapture(capture)
	// 收口后仅剩 scope 表兜底路径（sessionID 非空）
	assert.Nil(t, r.resolveCaptureByScope("sess-2", "", ""))
}

// TestEnsureEvolveReviewTaskAvailable 对齐 :445-452：
// ability_manager 无 evolve_review_task → 报错原文；有 → nil；无 agent → nil。
func TestEnsureEvolveReviewTaskAvailable(t *testing.T) {
	r := newConstructedRail(t)
	require.NoError(t, r.EnsureEvolveReviewTaskAvailable())

	r.agentRef = newFakeAbilityAgent(t, false)
	err := r.EnsureEvolveReviewTaskAvailable()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires evolve_review_task to be registered")

	r.agentRef = newFakeAbilityAgent(t, true)
	require.NoError(t, r.EnsureEvolveReviewTaskAvailable())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fakeAbilityAgent 仅实现 AbilityManager 的测试替身。
type fakeAbilityAgent struct {
	agentinterfaces.BaseAgent
	hasTool bool
}

// AbilityManager 返回假能力管理器。
func (f *fakeAbilityAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return &fakeAbilityManager{hasTool: f.hasTool}
}

// fakeAbilityManager 假能力管理器（Get 按名字返回占位能力）。
type fakeAbilityManager struct {
	agentinterfaces.AbilityManagerInterface
	hasTool bool
}

// Get 按名字返回占位能力（nil 语义：未注册时返回 nil 接口值）。
func (f *fakeAbilityManager) Get(name string) commonschema.Ability {
	if f.hasTool && name == "evolve_review_task" {
		return fakeAbility{Name: name}
	}
	return nil
}

// fakeAbility 最小 Ability 实现（仅承载名称）。
type fakeAbility struct {
	Name string
}

// AbilityName 返回能力名称。
func (a fakeAbility) AbilityName() string { return a.Name }

// AbilityID 返回能力唯一标识。
func (a fakeAbility) AbilityID() string { return a.Name }

// AbilityKind 返回能力类型。
func (a fakeAbility) AbilityKind() commonschema.AbilityKind { return commonschema.AbilityKind(0) }

// newFakeAbilityAgent 构造带假能力面的 agent 替身。
func newFakeAbilityAgent(t *testing.T, hasTool bool) *fakeAbilityAgent {
	t.Helper()
	return &fakeAbilityAgent{hasTool: hasTool}
}

// ensureCtx 兜底引用（避免 unused import）。
var _ = context.Background
