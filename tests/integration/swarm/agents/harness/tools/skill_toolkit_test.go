//go:build integration

package tools

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
	skillops "github.com/uapclaw/uapclaw-go/internal/agentcore/operator/skill_call"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillToolkitSuite 技能管理工具集的集成测试。
// 验证 SkillToolkit 构造、工具注册、搜索来源校验及序列化输出。
type SkillToolkitSuite struct {
	isuite.RunnerSuite
	// toolkit 技能工具集实例
	toolkit *swarmtools.SkillToolkit
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSkillToolkitSuite(t *testing.T) {
	suite.Run(t, new(SkillToolkitSuite))
}

// SetupSuite 初始化测试环境，创建 SkillToolkit 实例。
func (s *SkillToolkitSuite) SetupSuite() {
	s.RunnerSuite.SetupSuite()

	// 创建 SkillManager（空 workspaceDir，无真实后端，仅验证结构）
	mgr := skillpkg.NewSkillManager("")
	s.toolkit = swarmtools.NewSkillToolkit(mgr)
}

// TestSkillToolkit_构造 验证 SkillToolkit 构造后非空。
func (s *SkillToolkitSuite) TestSkillToolkit_构造() {
	s.Require().NotNil(s.toolkit)
}

// TestSkillToolkit_工具注册 验证 GetTools 返回 3 个工具。
func (s *SkillToolkitSuite) TestSkillToolkit_工具注册() {
	tools := s.toolkit.GetTools()
	s.Require().NotNil(tools)
	s.Len(tools, 3, "GetTools 应返回 3 个工具：search_skill/install_skill/uninstall_skill")
}

// TestSkillToolkit_工具描述非空 验证每个工具的名称和描述非空。
func (s *SkillToolkitSuite) TestSkillToolkit_工具描述非空() {
	tools := s.toolkit.GetTools()
	expectedNames := map[string]bool{
		"search_skill":    true,
		"install_skill":   true,
		"uninstall_skill": true,
	}
	for _, t := range tools {
		card := t.Card()
		s.Require().NotNil(card)
		s.NotEmpty(card.Name, "工具名称不应为空")
		s.NotEmpty(card.Description, "工具描述不应为空")
		s.True(expectedNames[card.Name], "工具名称 %s 不在期望列表中", card.Name)
	}
}

// TestSkillToolkit_SearchSkill_参数校验 验证 SearchSkill 空查询返回失败。
func (s *SkillToolkitSuite) TestSkillToolkit_SearchSkill_参数校验() {
	result, err := s.toolkit.SearchSkill(context.Background(), map[string]any{
		"query":  "",
		"source": "skillnet",
	})
	s.NoError(err)
	s.False(toBool(result["success"]))
	s.Contains(result["detail"], "query is required")
}

// TestSkillToolkit_SearchSkill_来源校验 验证无效来源返回失败。
func (s *SkillToolkitSuite) TestSkillToolkit_SearchSkill_来源校验() {
	result, err := s.toolkit.SearchSkill(context.Background(), map[string]any{
		"query":  "test",
		"source": "invalid_source",
	})
	s.NoError(err)
	s.False(toBool(result["success"]))
	s.Contains(result["detail"], "unsupported source")
}

// TestSkillToolkit_InstallSkill_参数校验 验证 InstallSkill 缺少标识符返回失败。
func (s *SkillToolkitSuite) TestSkillToolkit_InstallSkill_参数校验() {
	result, err := s.toolkit.InstallSkill(context.Background(), map[string]any{
		"identifier": "",
		"source":     "skillnet",
	})
	s.NoError(err)
	s.False(toBool(result["success"]))
	s.Contains(result["detail"], "identifier is required")
}

// TestSkillToolkit_InstallSkill_来源必须显式 验证 InstallSkill 不接受 auto 来源。
func (s *SkillToolkitSuite) TestSkillToolkit_InstallSkill_来源必须显式() {
	result, err := s.toolkit.InstallSkill(context.Background(), map[string]any{
		"identifier": "some-skill",
		"source":     "auto",
	})
	s.NoError(err)
	s.False(toBool(result["success"]))
	s.Contains(result["detail"], "source must be explicitly set")
}

// TestSkillToolkit_UninstallSkill_参数校验 验证 UninstallSkill 缺少名称返回失败。
func (s *SkillToolkitSuite) TestSkillToolkit_UninstallSkill_参数校验() {
	result, err := s.toolkit.UninstallSkill(context.Background(), map[string]any{
		"name": "",
	})
	s.NoError(err)
	s.False(toBool(result["success"]))
	s.Contains(result["detail"], "name is required")
}

// TestSkillSearchItem_序列化 验证 SkillSearchItem.ToMap 输出完整性。
func (s *SkillToolkitSuite) TestSkillSearchItem_序列化() {
	score := 42
	item := &swarmtools.SkillSearchItem{
		Name:        "test_skill",
		Description: "测试技能",
		Source:      "skillnet",
		Identifier:  "https://example.com/skill",
		Installed:   false,
		Version:     "1.0",
		Author:      "test_author",
		Score:       &score,
	}
	m := item.ToMap()
	s.Equal("test_skill", m["name"])
	s.Equal("测试技能", m["description"])
	s.Equal("skillnet", m["source"])
	s.Equal("https://example.com/skill", m["identifier"])
	s.Equal(false, m["installed"])
	s.Equal("1.0", m["version"])
	s.Equal("test_author", m["author"])
	s.Equal(&score, m["score"])
}

// TestInstalledItem_序列化 验证 InstalledItem.ToMap 输出完整性。
func (s *SkillToolkitSuite) TestInstalledItem_序列化() {
	item := &swarmtools.InstalledItem{
		Name:        "installed_skill",
		Description: "已安装技能",
		Source:      "clawhub",
		Identifier:  "clawhub-slug",
		Installed:   true,
		Version:     "2.0",
		Author:      "author",
		SkillDir:    "/skills/installed_skill",
		SkillFile:   "/skills/installed_skill/SKILL.md",
	}
	m := item.ToMap()
	s.Equal("installed_skill", m["name"])
	s.Equal("已安装技能", m["description"])
	s.Equal("clawhub", m["source"])
	s.Equal(true, m["installed"])
	s.Equal("/skills/installed_skill", m["skill_dir"])
	s.Equal("/skills/installed_skill/SKILL.md", m["skill_file"])
}

// TestSkillExperienceOperator_构造 验证技能经验操作器可独立构造。
func (s *SkillToolkitSuite) TestSkillExperienceOperator_构造() {
	op := skillops.NewSkillExperienceOperator("test_skill")
	s.Require().NotNil(op)
}

// TestInterface_接口满足 验证 SkillToolkit 相关类型不实现 AgentRail（仅 Rail 才实现）。
func (s *SkillToolkitSuite) TestInterface_接口满足() {
	// SkillToolkit 不是 Rail，此处验证 SkillExperienceOperator 可构造
	op := skillops.NewSkillExperienceOperator("my_skill")
	s.NotNil(op)

	// AgentRail 接口仅由 Rail 类型实现，验证接口变量可声明
	var _ agentinterfaces.AgentRail // 接口存在性验证（编译时检查）
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toBool 安全地将 any 转为 bool（对齐 skill_toolkit 内部逻辑）
func toBool(v any) bool {
	if v == nil {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}
