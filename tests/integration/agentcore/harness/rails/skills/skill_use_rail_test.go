//go:build integration

package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	skilluse "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillUseRailSuite 测试 SkillUseRail 技能使用。
//
// 覆盖：
//   - Init 注册 skill_tool 工具
//   - BeforeModelCall 注入 SectionSkills
//   - 无技能目录时不崩溃
//   - Uninit 清理
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_team_skill_rail.py
type SkillUseRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillUseRailSuite(t *testing.T) {
	suite.Run(t, new(SkillUseRailSuite))
}

// TestSkillUseRail_Init注册skill_tool 测试 SkillUseRail Init 后注册 skill_tool。
// 对齐 Python: SkillUseRail.init() 中工具注册
func (s *SkillUseRailSuite) TestSkillUseRail_Init注册skill_tool() {
	skillsDir := s.T().TempDir()
	rail := skilluse.NewSkillUseRail([]string{skillsDir})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("技能测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

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

// TestSkillUseRail_BeforeModelCall_注入技能节 测试有 SKILL.md 时 BeforeModelCall 注入 SectionSkills。
// 对齐 Python: SkillUseRail.before_model_call() 中 skills section 注入
func (s *SkillUseRailSuite) TestSkillUseRail_BeforeModelCall_注入技能节() {
	// 创建含 SKILL.md 的技能目录
	skillsDir := s.T().TempDir()
	skillSubDir := filepath.Join(skillsDir, "my_skill")
	s.Require().NoError(os.MkdirAll(skillSubDir, 0755))
	s.Require().NoError(os.WriteFile(filepath.Join(skillSubDir, "SKILL.md"),
		[]byte("---\ndescription: 我的测试技能\n---\n# 测试技能\n这是一个测试技能内容"), 0644))

	rail := skilluse.NewSkillUseRail([]string{skillsDir})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("技能节测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "使用技能"})
	s.Require().NoError(err)

	// 验证 SectionSkills 已注入（skill_mode="all" 时应有内容）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionSkills), "有 SKILL.md 时应注入 SectionSkills 节")

	section := spb.GetSection(hsections.SectionSkills)
	s.Require().NotNil(section)
	s.NotEmpty(section.Content, "SectionSkills 内容不应为空")
}

// TestSkillUseRail_无技能目录时不崩溃 测试 skillsDir 为空时 Init 不崩溃。
func (s *SkillUseRailSuite) TestSkillUseRail_无技能目录时不崩溃() {
	rail := skilluse.NewSkillUseRail([]string{})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("空目录测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "空目录测试"})
	s.Require().NoError(err, "空 skillsDir 时 Invoke 不应崩溃")

	railType := reflect.TypeOf(&skilluse.SkillUseRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "空 skillsDir 时 SkillUseRail 仍应注册")
}

// TestSkillUseRail_Uninit清理 测试 Uninit 移除 skill_tool。
func (s *SkillUseRailSuite) TestSkillUseRail_Uninit清理() {
	skillsDir := s.T().TempDir()
	rail := skilluse.NewSkillUseRail([]string{skillsDir})

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 skill_tool 已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("skill_tool"), "Uninit 后应注销 skill_tool")
}
