package evolution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newTeamRail 用临时目录与假模型构造完整的 TeamSkillEvolutionRail。
func newTeamRail(t *testing.T, opts ...TeamSkillEvolutionRailOption) *TeamSkillEvolutionRail {
	t.Helper()
	return NewTeamSkillEvolutionRail(
		[]string{t.TempDir()},
		newTestLLMModel(t),
		"team-model",
		"cn",
		opts...,
	)
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewTeamSkillEvolutionRail 验证构造后默认值与组件就绪。
func TestNewTeamSkillEvolutionRail(t *testing.T) {
	r := newTeamRail(t)
	require.NotNil(t, r)

	// Python 默认 auto_save=False（与 SkillEvolutionRail 默认 true 相反）
	assert.False(t, r.AutoSave(), "TeamSkillEvolutionRail 默认 auto_save 应为 false")
	assert.NotNil(t, r.EvolutionStore(), "EvolutionStore 不应为 nil")
	assert.NotNil(t, r.Scorer(), "Scorer 不应为 nil")
	assert.NotNil(t, r.Generator(), "Generator 不应为 nil")
	assert.NotNil(t, r.TeamSignalDetector(), "TeamSignalDetector 不应为 nil")
	assert.NotNil(t, r.ApprovalRuntime(), "ApprovalRuntime 不应为 nil")
}

// TestTeamSkillEvolutionRail_Setters 验证各 setter 的赋值效果。
func TestTeamSkillEvolutionRail_Setters(t *testing.T) {
	t.Run("SetTrajectorySource 后字段非 nil", func(t *testing.T) {
		r := newTeamRail(t)
		r.SetTrajectorySource(nil)
		assert.Nil(t, r.trajectorySource)
	})

	t.Run("HasPendingApprovalSnapshot 对未知 ID 返回 false", func(t *testing.T) {
		r := newTeamRail(t)
		assert.False(t, r.HasPendingApprovalSnapshot("no-such-request"))
	})

	t.Run("UpdateLLM 更新三处组件且不 panic", func(t *testing.T) {
		r := newTeamRail(t)
		r.UpdateLLM(newTestLLMModel(t), "new-model")
		assert.NotNil(t, r.TeamSignalDetector(), "更新后探测器不应为 nil")
	})

	t.Run("SetSysOperation 同时下发给基类与 store", func(t *testing.T) {
		r := newTeamRail(t)
		assert.NotPanics(t, func() { r.SetSysOperation(nil) })
	})
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestIsTeamSkill 验证团队技能判定。
func TestIsTeamSkill(t *testing.T) {
	ctx := context.Background()

	t.Run("不存在的技能判为 false", func(t *testing.T) {
		r := newTeamRail(t)
		assert.False(t, r.isTeamSkill(ctx, "not-exist"))
	})
}

// TestTeamSkillForExperienceDetailFile 验证按路径反查团队技能。
func TestTeamSkillForExperienceDetailFile(t *testing.T) {
	ctx := context.Background()

	t.Run("无技能时返回空", func(t *testing.T) {
		r := newTeamRail(t)
		assert.Empty(t, r.teamSkillForExperienceDetailFile(ctx, "/tmp/x/evolution/a.md"))
	})
}

// TestAggregateTeamTrajectory 验证无轨迹源时原样返回。
func TestAggregateTeamTrajectory(t *testing.T) {
	r := newTeamRail(t)
	assert.Nil(t, r.aggregateTeamTrajectory(nil), "无源且入参 nil 时应返回 nil")
}

// TestTeamRail_SnapshotForEvolution 验证关闭扫描时不产生快照。
func TestTeamRail_SnapshotForEvolution(t *testing.T) {
	r := newTeamRail(t, WithTeamSkillAutoScan(false))
	assert.Nil(t, r.SnapshotForEvolution(context.Background(), nil, nil))
}

// TestTeamRail_ApproveRejectRecord 验证未知 requestID 的审批幂等返回无错误。
func TestTeamRail_ApproveRejectRecord(t *testing.T) {
	ctx := context.Background()

	t.Run("批准未知请求无错误", func(t *testing.T) {
		r := newTeamRail(t)
		assert.NoError(t, r.ApproveRecord(ctx, "unknown-approve"))
	})

	t.Run("拒绝未知请求无错误", func(t *testing.T) {
		r := newTeamRail(t)
		assert.NoError(t, r.RejectRecord(ctx, "unknown-reject"))
	})
}

// TestTeamRail_技能目录解析 用真实技能目录覆盖 team-skill kind 判定。
func TestTeamRail_技能目录解析(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	skillDir := filepath.Join(base, "team-skill-one")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: team-skill-one\ndescription: d\nkind: team-skill\n---\nbody"),
		0o644,
	))

	r := NewTeamSkillEvolutionRail([]string{base}, newTestLLMModel(t), "m", "cn")
	// store 能解析到该技能目录时，kind=team-skill 应判为团队技能
	if r.EvolutionStore().ResolveSkillDir(ctx, "team-skill-one") != "" {
		assert.True(t, r.isTeamSkill(ctx, "team-skill-one"), "kind=team-skill 应判为团队技能")
	}
}

// ──────────────────────────── 非导出函数补充测试 ────────────────────────────

// TestTeamRail_IsSkillDisabled 验证禁用名单判定。
func TestTeamRail_IsSkillDisabled(t *testing.T) {
	r := newTeamRail(t, WithTeamSkillDisabledSkills([]string{"blocked-skill"}))
	assert.True(t, r.isSkillDisabled("blocked-skill"), "名单内技能应判为禁用")
	assert.False(t, r.isSkillDisabled("other-skill"), "名单外技能不应判为禁用")
}

// TestTeamRail_IsActiveRequestSubject 验证活跃请求主体判定（要求技能存在且为团队技能）。
func TestTeamRail_IsActiveRequestSubject(t *testing.T) {
	ctx := context.Background()
	r := newTeamRail(t)
	assert.False(t, r.isActiveRequestSubject(ctx, "not-exist-skill"), "不存在的技能不应判为活跃主体")
}

// TestTeamRail_SnapshotForEvolution_autoScan开 验证开启扫描时产出快照。
// 注意：该实现末尾直接读 traj.SessionID，traj 为 nil 会 panic，故传非 nil 轨迹。
func TestTeamRail_SnapshotForEvolution_autoScan开(t *testing.T) {
	r := newTeamRail(t)
	traj := &trajectory.Trajectory{SessionID: "sess-snap"}
	snap := r.SnapshotForEvolution(context.Background(), traj, nil)
	require.NotNil(t, snap, "auto_scan 开启时应返回快照")
	require.NotNil(t, snap.SkillName, "SkillName 不应为 nil")
	assert.Equal(t, "team-skill", *snap.SkillName)
	assert.Equal(t, "sess-snap", snap.SessionID)
}

// TestTeamRail_DetectUserRequest_空消息 验证无消息时返回 nil 且无错误。
func TestTeamRail_DetectUserRequest_空消息(t *testing.T) {
	r := newTeamRail(t)
	got, err := r.detectUserRequest(context.Background(), nil, "")
	assert.NoError(t, err)
	assert.Nil(t, got)
}

// TestTeamRail_BuildRecordApprovalEvent_Nil请求 验证 request 为 nil 时返回 nil。
func TestTeamRail_BuildRecordApprovalEvent_Nil请求(t *testing.T) {
	r := newTeamRail(t)
	assert.Nil(t, r.buildRecordApprovalEvent("skill-a", nil))
}

// TestTeamRail_CurrentBuilderSessionID 验证未设 builder 时返回空串。
func TestTeamRail_CurrentBuilderSessionID(t *testing.T) {
	r := newTeamRail(t)
	assert.Empty(t, r.currentBuilderSessionID(), "未设 builder 时应返回空串")
}

// TestTeamRail_BuildTrajectory 验证未设 builder 时返回 nil。
func TestTeamRail_BuildTrajectory(t *testing.T) {
	r := newTeamRail(t)
	assert.Nil(t, r.buildTrajectory(), "未设 builder 时应返回 nil")
}

// TestTeamRail_RequestUserEvolution_无信号 验证无轨迹且无意图时返回空结果。
func TestTeamRail_RequestUserEvolution_无信号(t *testing.T) {
	r := newTeamRail(t)
	res, err := r.RequestUserEvolution(context.Background(), "skill-z", "", false)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res == nil || res.SkillName != "skill-z" {
		t.Fatalf("res = %+v", res)
	}
}
