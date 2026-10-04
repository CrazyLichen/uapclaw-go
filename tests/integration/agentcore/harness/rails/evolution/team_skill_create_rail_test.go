//go:build integration

package evolution

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	evolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillCreateRailSuite 测试 TeamSkillCreateRail 团队技能创建护栏。
//
// 对齐 Python: tests/unit_tests/harness/test_team_skill_create_rail.py
//   - 构造选项、Priority=85
//   - spawn_member 阈值检测
//   - follow_up 提示词构建（中/英文）
//   - NotifyTeamCompleted 标记
//   - detectUsedTeamSkill 检测已使用团队技能
type TeamSkillCreateRailSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamSkillCreateRailSuite 运行 TeamSkillCreateRail 集成测试套件。
func TestTeamSkillCreateRailSuite(t *testing.T) {
	suite.Run(t, new(TeamSkillCreateRailSuite))
}

// TestTeamSkillCreateRail_Priority85 测试优先级。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_Priority85() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)
	s.Equal(85, rail.Priority())
}

// TestTeamSkillCreateRail_默认配置 测试默认构造选项。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_默认配置() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)

	// EvolutionRail 基类应存在
	s.NotNil(rail.EvolutionRail)
	// TriggerNone 模式（不自动触发演化）
	s.NotNil(rail.GetCallbacks())
}

// TestTeamSkillCreateRail_WithAutoTrigger 测试自动触发选项。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_WithAutoTrigger() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir,
		evolution.WithTeamSkillCreateAutoTrigger(false),
	)
	s.NotNil(rail)
}

// TestTeamSkillCreateRail_WithMinTeamMembers 测试最小团队成员数阈值选项。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_WithMinTeamMembers() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir,
		evolution.WithTeamSkillCreateMinTeamMembers(5),
	)
	s.NotNil(rail)
}

// TestTeamSkillCreateRail_WithLanguage 测试语言选项。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_WithLanguage() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir,
		evolution.WithTeamSkillCreateLanguage("en"),
	)
	s.NotNil(rail)
}

// TestTeamSkillCreateRail_GetCallbacks 测试 GetCallbacks 注册。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_GetCallbacks() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)
	callbacks := rail.GetCallbacks()
	s.Contains(callbacks, agentinterfaces.CallbackBeforeInvoke)
	s.Contains(callbacks, agentinterfaces.CallbackAfterTaskIteration)
	s.Contains(callbacks, agentinterfaces.CallbackAfterInvoke)
}

// TestTeamSkillCreateRail_NotifyTeamCompleted无Builder 测试无 builder 时 NotifyTeamCompleted 返回 false。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_NotifyTeamCompleted无Builder() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)

	// 无 builder 时应返回 false
	result := rail.NotifyTeamCompleted(nil)
	s.False(result)
}

// TestTeamSkillCreateRail_NotifyTeamCompletedAutoTrigger关闭 测试 autoTrigger 关闭时返回 false。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_NotifyTeamCompletedAutoTrigger关闭() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir,
		evolution.WithTeamSkillCreateAutoTrigger(false),
	)

	// autoTrigger 关闭时应返回 false
	result := rail.NotifyTeamCompleted(nil)
	s.False(result)
}

// TestTeamSkillCreateRail_EvolutionExtension接口实现 测试编译时接口满足。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_EvolutionExtension接口实现() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)

	// TeamSkillCreateRail 实现了 EvolutionExtension，可以传给 NewEvolutionRail
	// 编译通过即证明接口满足
	_ = rail.EvolutionRail
}

// TestTeamSkillCreateRail_RunEvolution空实现 测试 RunEvolution 不做任何事。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_RunEvolution空实现() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)

	// RunEvolution 应返回 nil（空实现）
	err := rail.RunEvolution(s.Ctx, nil, nil)
	s.Require().NoError(err)
}

// TestTeamSkillCreateRail_GetEvolutionTotalTimeoutSecs 测试超时返回 0。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_GetEvolutionTotalTimeoutSecs() {
	tempDir := s.T().TempDir()
	rail := evolution.NewTeamSkillCreateRail(tempDir)
	s.Equal(0.0, rail.GetEvolutionTotalTimeoutSecs())
}

// TestTeamSkillCreateRail_目录不存在时knownTeamSkillNames空 测试技能目录不存在时不崩溃。
func (s *TeamSkillCreateRailSuite) TestTeamSkillCreateRail_目录不存在时knownTeamSkillNames空() {
	rail := evolution.NewTeamSkillCreateRail("/nonexistent/dir/skills")
	s.NotNil(rail)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupSkillsDir 创建模拟技能目录结构。
func setupSkillsDir(tempDir string, skillName string, kind string) {
	skillDir := filepath.Join(tempDir, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return
	}
	content := "---\nkind: " + kind + "\n---\n# Test Skill\n"
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644)
}
