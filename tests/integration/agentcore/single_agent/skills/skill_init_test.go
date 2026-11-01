//go:build integration

package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	goskills "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillInitSuite 技能子系统初始化集成测试。
//
// 对齐 Python: tests/system_tests/agent/skill/test_skill_init.py。
//
// Go 差异：ReActAgent 无 register_skill/lazy_init_skill（Python 的懒初始化在 BaseAgent），
// Go 通过 ReActAgent.SetSkillUtil 注入，故此处覆盖 SkillUtil 层的幂等与 ID 同步语义。
type SkillInitSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillInitSuite 运行技能初始化集成测试套件
func TestSkillInitSuite(t *testing.T) {
	suite.Run(t, new(SkillInitSuite))
}

// TestSetSysOperationID_复用实例并同步Remote 对齐 Python: reconfigure 只更新 ID 不重建对象
func (s *SkillInitSuite) TestSetSysOperationID_复用实例并同步Remote() {
	su := goskills.NewSkillUtil("sys_old")
	remote := su.RemoteSkillUtil()
	s.Require().NotNil(remote, "RemoteSkillUtil 应已构造")
	s.Equal("sys_old", remote.SysOperationID())

	su.SetSysOperationID("sys_new")
	s.Equal("sys_new", su.RemoteSkillUtil().SysOperationID(), "更新 ID 应同步到 RemoteSkillUtil")
	s.Same(remote, su.RemoteSkillUtil(), "应为同一实例（不重建）")
}

// TestRegisterSkills_真实目录注册 对齐 Python: register_skill 委托 register_skills
func (s *SkillInitSuite) TestRegisterSkills_真实目录注册() {
	root := s.T().TempDir()
	skillDir := filepath.Join(root, "init-skill")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: init-skill\ndescription: 初始化技能\n---\nbody\n"),
		0o600,
	))

	su := goskills.NewSkillUtil("sys_op_b")
	s.False(su.HasSkill(), "注册前不应有技能")

	s.Require().NoError(su.RegisterSkills([]string{root}, false))
	s.True(su.HasSkill(), "注册后应有技能")
	s.Contains(su.GetSkillPrompt(), "init-skill", "技能提示词应包含技能名")
}

// TestRegisterSkills_幂等与覆盖 对齐 Python: 重复注册不报错
func (s *SkillInitSuite) TestRegisterSkills_幂等与覆盖() {
	root := s.T().TempDir()
	skillDir := filepath.Join(root, "idem-skill")
	s.Require().NoError(os.MkdirAll(skillDir, 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: idem-skill\ndescription: 幂等技能\n---\nbody\n"),
		0o600,
	))

	su := goskills.NewSkillUtil("sys_op_c")
	s.Require().NoError(su.RegisterSkills([]string{root}, false))

	// overwrite=false：重复注册返回「技能已存在」错误（Go 语义）
	err := su.RegisterSkills([]string{root}, false)
	s.Require().Error(err)
	s.Contains(err.Error(), "已存在")

	// overwrite=true：覆盖成功且不产生重复记录
	s.Require().NoError(su.RegisterSkills([]string{root}, true))
	s.True(su.HasSkill())
	s.Equal(1, su.SkillManager().Count(), "重复注册不应产生重复技能")
}
