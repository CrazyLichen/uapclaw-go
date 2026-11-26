//go:build integration

package agentteams_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	upath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// EvolutionConfigTeardownSuite TeamManager.UpdateEvolutionConfig 开关热更新端到端。
//
// 对齐 Python TeamManager.update_evolution_config (team_manager.py L2846-2935)：
// 关闭 = 全量能力拆除（先取消 watcher，再反注册完整 live rail 快照——审批中断随
// 父 rail 一并移除，最后遗忘已挂载 rail、保留重建 context）；启用 = 既有 rail 套用
// 固定角色触发策略 + 从保留 context 重建缺失的角色 rail。
//
// 真实跨子系统：harness/team（TeamManager 状态机）+ harness（真 DeepAgent 实例的
// RegisterRail/UnregisterRail 面）+ rails/evolution + rails/skills + common/config
// + workspace。mock 边界：无（不涉 LLM 调用，模型仅构造不 invoke）。
type EvolutionConfigTeardownSuite struct {
	isuite.AgentSuite
	suite.Suite

	dataDir string
	mgr     *team.TeamManager
}

// TestEvolutionConfigTeardownSuite 套件入口。
func TestEvolutionConfigTeardownSuite(t *testing.T) {
	suite.Run(t, new(EvolutionConfigTeardownSuite))
}

// writeTeamConfig 写入 workspace config/config.yaml（models + 演进开关）。
func (s *EvolutionConfigTeardownSuite) writeTeamConfig(enabled bool) {
	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	evolutionValue := "false"
	if enabled {
		evolutionValue = "true"
	}
	content := `
models:
  default:
    model_client_config:
      model_name: test-model
      client_provider: openai
      api_base: http://localhost:1
      api_key: test
      verify_ssl: false
react:
  evolution:
    skill_evolution: ` + evolutionValue + `
`
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(content), 0o644))
}

// setupIsolatedEnv 隔离 workspace 根与 agent_teams home。
func (s *EvolutionConfigTeardownSuite) setupIsolatedEnv() {
	s.dataDir = s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", s.dataDir)
	upath.ResetCache()
	agentteams.ConfigureHome(s.T().TempDir())
	s.T().Cleanup(func() {
		os.Unsetenv("UAPCLAW_DATA_DIR")
		upath.ResetCache()
		agentteams.ResetHome()
	})
}

// newMountContext 构造带真 DeepAgent 的挂载 context。
func (s *EvolutionConfigTeardownSuite) newMountContext(name, role string, skillsDir string) *team.TeamRailMountContext {
	agent := harness.NewDeepAgent(agentschema.NewAgentCard())
	roleCopy := role
	return &team.TeamRailMountContext{
		Agent:      agent,
		MemberInfo: &team.MemberInfo{Role: &roleCopy},
		Runtime:    &team.RuntimeInfo{Channel: "default"},
		TeamWorkspace: &team.TeamWorkspaceInfo{
			SkillsDir: &skillsDir,
			Config:    map[string]any{},
		},
	}
}

// TestTeardownOnDisable_关闭全量拆除 真实例 rail 移除 + watcher 取消 + context 保留。
func (s *EvolutionConfigTeardownSuite) TestTeardownOnDisable_关闭全量拆除() {
	s.setupIsolatedEnv()
	s.writeTeamConfig(true)
	s.mgr = team.NewTeamManager()

	skillsDir := s.T().TempDir()
	leaderCtx := s.newMountContext("leader", "leader", skillsDir)
	s.mgr.RegisterTeamRailContext("sess-teardown", leaderCtx)

	enabledCfg := map[string]any{
		"react": map[string]any{"evolution": map[string]any{
			"skill_evolution": true, "auto_save": true,
		}},
	}
	s.mgr.UpdateEvolutionConfig(context.Background(), enabledCfg)

	// 真实例上应挂载 team rail 与 create rail
	leaderAgent := leaderCtx.Agent
	teamRails := leaderAgent.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillEvolutionRail{}))
	createRails := leaderAgent.FindRailsByType(reflect.TypeOf(&skills.TeamSkillCreateRail{}))
	s.Require().Len(teamRails, 1, "真 DeepAgent 应挂载 TeamSkillEvolutionRail")
	s.Require().Len(createRails, 1, "真 DeepAgent 应挂载 TeamSkillCreateRail")
	s.Require().NotNil(s.mgr.GetTeamSkillRail("sess-teardown"))
	s.Require().NotNil(s.mgr.GetTeamSkillCreateRail("sess-teardown"))

	// watcher 登记 → 关闭分支应取消并弹出
	watcherCancelled := false
	s.mgr.RegisterTeamEvolutionWatcher("sess-teardown", func() { watcherCancelled = true })

	s.mgr.UpdateEvolutionConfig(context.Background(), map[string]any{})

	s.True(watcherCancelled, "关闭分支应先取消 watcher（避免拆除后再发出 pending approval）")
	s.Nil(s.mgr.GetTeamEvolutionWatcher("sess-teardown"), "watcher 应从登记表弹出")
	s.Empty(leaderAgent.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillEvolutionRail{})),
		"真 DeepAgent 上的 TeamSkillEvolutionRail 应被反注册")
	s.Empty(leaderAgent.FindRailsByType(reflect.TypeOf(&skills.TeamSkillCreateRail{})),
		"真 DeepAgent 上的 TeamSkillCreateRail 应被反注册")
	s.Nil(s.mgr.GetTeamSkillRail("sess-teardown"), "teamSkillRails 应清空")
	s.Nil(s.mgr.GetTeamSkillCreateRail("sess-teardown"), "teamSkillCreateRails 应清空")
	s.NotNil(s.mgr.GetTeamRailContext("sess-teardown"), "重建 context 必须保留")
}

// TestRebuildOnEnable_重新启用重建缺失角色rail 关闭→启用后从保留 context 重建，
// 固定角色触发策略生效（team: signal=false/review=true/auto_save=config；
// member: signal=true/review=false/auto_save=true）。
func (s *EvolutionConfigTeardownSuite) TestRebuildOnEnable_重新启用重建缺失角色rail() {
	s.setupIsolatedEnv()
	s.writeTeamConfig(true)
	s.mgr = team.NewTeamManager()

	skillsDir := s.T().TempDir()
	leaderCtx := s.newMountContext("leader", "leader", skillsDir)
	memberCtx := s.newMountContext("member", "teammate", skillsDir)
	s.mgr.RegisterTeamRailContext("sess-rebuild", leaderCtx)
	s.mgr.RegisterTeamMemberRailContext("sess-rebuild", memberCtx)

	// 关闭（未挂载过任何 rail）→ 启用：全部角色 rail 重建并挂上真实例
	s.mgr.UpdateEvolutionConfig(context.Background(), map[string]any{})
	enabledCfg := map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}},
	}
	s.mgr.UpdateEvolutionConfig(context.Background(), enabledCfg)

	leaderAgent := leaderCtx.Agent
	s.Len(leaderAgent.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillEvolutionRail{})), 1,
		"重建后 leader 真实例应有 TeamSkillEvolutionRail")
	s.Len(leaderAgent.FindRailsByType(reflect.TypeOf(&skills.TeamSkillCreateRail{})), 1,
		"重建后 leader 真实例应有 TeamSkillCreateRail")
	memberRails := memberCtx.Agent.FindRailsByType(reflect.TypeOf(&evolution.SkillEvolutionRail{}))
	s.Require().Len(memberRails, 1, "重建后 teammate 真实例应有 SkillEvolutionRail")

	// 固定角色策略（上游均不可配置）
	teamRail := s.mgr.GetTeamSkillRail("sess-rebuild")
	s.Require().NotNil(teamRail)
	s.False(teamRail.SignalTrigger(), "团队 rail signal_trigger 恒为 False")
	s.True(teamRail.ReviewTrigger(), "团队 rail review_trigger 恒为 True")
	s.False(teamRail.AutoSave(), "auto_save 缺省（config 未开）应为 False")

	memberRail := memberRails[0].(*evolution.SkillEvolutionRail)
	s.True(memberRail.SignalTrigger(), "成员 rail signal_trigger 恒为 True")
	s.False(memberRail.ReviewTrigger(), "成员 rail review_trigger 恒为 False")
	s.True(memberRail.AutoSave(), "成员 rail auto_save 恒为 True（跳过审批）")
}

// TestConfigSwitchReading_开关读取对齐 configutil.SkillEvolutionEnabled 语义
// （strictly True 才启用；auto_save 独立读取）。
func (s *EvolutionConfigTeardownSuite) TestConfigSwitchReading_开关读取对齐() {
	s.True(configutil.SkillEvolutionEnabled(map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": true}},
	}))
	s.False(configutil.SkillEvolutionEnabled(map[string]any{
		"react": map[string]any{"evolution": map[string]any{"skill_evolution": "true"}},
	}), "字符串 true 不应启用（Python is True）")
	s.False(configutil.EvolutionAutoSaveEnabled(nil), "缺省 auto_save=False")
}
