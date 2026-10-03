//go:build integration

package skills

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	skilluse "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillUseRailSuite 测试 SkillUseRail 技能使用。
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_team_skill_rail.py
// 注意：SkillUseRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 注册工具和构造成功。
type SkillUseRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillUseRailSuite(t *testing.T) {
	suite.Run(t, new(SkillUseRailSuite))
}

// TestSkillUseRail_Init注册工具 测试 SkillUseRail Init 后注册 skill_tool 等工具。
// 对齐 Python: TestMcpRailInit.test_adds_both_cards_to_ability_manager ——
// Python 中 SkillUseRail.init 注册 skill_tool / list_skill / read_file 等工具到 AbilityManager。
// 注意：Rail Init 在第一次 Invoke（ensureInitialized）时触发，需先 Invoke。
func (s *SkillUseRailSuite) TestSkillUseRail_Init注册工具() {
	skillsDir := s.T().TempDir()
	rail := skilluse.NewSkillUseRail([]string{skillsDir})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("技能测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 先 Invoke 触发 ensureInitialized → Rail Init
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "技能测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 SkillUseRail 已注册
	railType := reflect.TypeOf(&skilluse.SkillUseRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 SkillUseRail")

	// 验证 skill_tool 已注册到 AbilityManager
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("skill_tool"), "应注册 skill_tool")
}

// TestSkillUseRail_无技能目录时不崩溃 测试 skillsDir 为空时 Init 不崩溃。
// 对齐 Python: test_team_skill_rail 空目录降级 ——
// Python 中 skills_dir 为空时 SkillUseRail.init 仍注册工具，不崩溃。
func (s *SkillUseRailSuite) TestSkillUseRail_无技能目录时不崩溃() {
	// 空 skillsDir 列表
	rail := skilluse.NewSkillUseRail([]string{})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("空目录测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "空目录测试"})
	s.Require().NoError(err, "空 skillsDir 时 Invoke 不应崩溃")

	// 即使空 skillsDir，SkillUseRail 仍注册
	railType := reflect.TypeOf(&skilluse.SkillUseRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "空 skillsDir 时 SkillUseRail 仍应注册")
}
