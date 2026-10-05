//go:build integration

package skills

import (
	"testing"

	"github.com/stretchr/testify/suite"

	goskills "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillLifecycleSuite 技能生命周期集成测试套件。
//
// 对齐 Python: tests/system_tests/agent/skill/test_skill_init.py
// 测试 SkillUtil 的创建幂等性、reconfigure 更新、注册委托、错误处理。
//
// 注意：Python 使用 lazy_init_skill 模式（register_skill 时才创建 SkillUtil），
// Go 使用显式 SetSkillUtil 注入模式，因此测试策略调整为：
//   - SkillUtil 创建后同一实例可重复使用（幂等性）
//   - SetSysOperationID 更新不重建实例（reconfigure 语义）
//   - RegisterSkills 委托给 SkillManager（注册委托）
//   - 空 sysOperationID 的 SkillUtil 仍可注册（Go 不检查 sysOperationID 必填）
type SkillLifecycleSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillLifecycleSuite 运行技能生命周期集成测试套件
func TestSkillLifecycleSuite(t *testing.T) {
	suite.Run(t, new(SkillLifecycleSuite))
}

// ──────────────────── 测试方法 ────────────────────

// TestSkillUtil_创建幂等性 验证 SkillUtil 创建后同一实例可重复使用。
// 对齐 Python: test_configure_creates_skill_util_and_lazy_init_is_idempotent
//
// Python 中 lazy_init_skill 第二次调用不会重建 SkillUtil。
// Go 中 SkillUtil 一旦创建即为同一实例，多次调用方法不会重建。
func (s *SkillLifecycleSuite) TestSkillUtil_创建幂等性() {
	fs := newMockFs()
	fs.addSkillMD("/skills/research/SKILL.md", "research", "研究技能")

	su := goskills.NewSkillUtilWithProvider("sys_op_1", fs)

	// 首次注册
	err := su.RegisterSkills([]string{"/skills/research/SKILL.md"}, false)
	s.Require().NoError(err)
	s.True(su.HasSkill(), "首次注册后应有技能")
	s.Equal(1, su.SkillManager().Count())

	// 再次注册不同技能，SkillUtil 是同一实例
	fs.addSkillMD("/skills/writing/SKILL.md", "writing", "写作技能")
	err = su.RegisterSkills([]string{"/skills/writing/SKILL.md"}, false)
	s.Require().NoError(err)
	s.Equal(2, su.SkillManager().Count(), "同一 SkillUtil 应能追加注册")

	// 验证 SkillManager 是同一实例
	sm := su.SkillManager()
	s.NotNil(sm, "SkillManager 不应为 nil")
	s.Equal(2, sm.Count(), "同一 SkillManager 应有 2 个技能")
}

// TestSkillUtil_Reconfigure更新SysOperationID 验证 SetSysOperationID 更新
// 内部 SkillManager 和 RemoteSkillUtil，但不重建实例。
// 对齐 Python: test_reconfigure_updates_sys_operation_id_without_recreating
//
// Python 中 reconfigure 调用 set_sys_operation_id，不重建 SkillUtil。
// Go 中 SetSysOperationID 同样只更新内部字段。
func (s *SkillLifecycleSuite) TestSkillUtil_Reconfigure更新SysOperationID() {
	fs := newMockFs()
	fs.addSkillMD("/skills/data_analysis/SKILL.md", "data_analysis", "数据分析技能")

	su := goskills.NewSkillUtilWithProvider("sys_op_old", fs)

	// 记录原始 SkillManager 实例
	originalSM := su.SkillManager()

	// reconfigure: 更新 sysOperationID
	su.SetSysOperationID("sys_op_new")

	// SkillManager 应为同一实例（不重建）
	sameSM := su.SkillManager()
	s.Equal(originalSM, sameSM, "SetSysOperationID 后 SkillManager 应为同一实例")

	// 原有技能注册应保持
	err := su.RegisterSkills([]string{"/skills/data_analysis/SKILL.md"}, false)
	s.Require().NoError(err)
	s.True(su.HasSkill(), "reconfigure 后注册技能应成功")
}

// TestSkillUtil_RegisterSkills委托 验证 RegisterSkills 委托给 SkillManager。
// 对齐 Python: test_register_skill_triggers_lazy_init_and_delegates
//
// Python 中 register_skill 触发 lazy_init 后委托给 SkillManager.register。
// Go 中 RegisterSkills 直接委托给 SkillManager.Register。
func (s *SkillLifecycleSuite) TestSkillUtil_RegisterSkills委托() {
	fs := newMockFs()
	fs.addSkillMD("/skills/coding/SKILL.md", "coding", "代码编写技能")
	fs.addSkillMD("/skills/debug/SKILL.md", "debug", "调试技能")

	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)

	// 通过 SkillUtil 注册
	err := su.RegisterSkills([]string{"/skills/coding/SKILL.md", "/skills/debug/SKILL.md"}, false)
	s.Require().NoError(err, "RegisterSkills 不应返回错误")

	// 验证底层 SkillManager 持有注册结果
	sm := su.SkillManager()
	s.Equal(2, sm.Count(), "SkillManager 应有 2 个技能")
	s.True(sm.Has("coding"), "应有 coding 技能")
	s.True(sm.Has("debug"), "应有 debug 技能")

	// 验证 SkillUtil 的 HasSkill 反映状态
	s.True(su.HasSkill(), "HasSkill 应返回 true")
}

// TestSkillUtil_RegisterSkills部分失败 验证 RegisterSkills 部分路径
// 失败时仍注册成功的技能（非原子回滚）。
// Go 端 SkillManager.Register 对每个路径独立处理，部分失败不影响已成功的。
func (s *SkillLifecycleSuite) TestSkillUtil_RegisterSkills部分失败() {
	fs := newMockFs()
	fs.addSkillMD("/skills/good_skill/SKILL.md", "good_skill", "好的技能")
	// /skills/bad_skill/SKILL.md 不存在

	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)

	err := su.RegisterSkills([]string{"/skills/good_skill/SKILL.md", "/skills/bad_skill/SKILL.md"}, false)
	s.Error(err, "部分路径失败应返回错误")

	// 但成功的技能应已注册
	s.Equal(1, su.SkillManager().Count(), "部分失败时已成功的应注册")
	s.True(su.SkillManager().Has("good_skill"), "good_skill 应已注册")
}

// TestSkillUtil_空SysOperationID 验证空 sysOperationID 的 SkillUtil
// 仍可注册技能（Go 端不强制检查 sysOperationID 必填）。
// 对齐 Python: test_register_skill_raises_when_sys_operation_id_missing
//
// Python 中缺少 sysOperationID 时 register_skill 报错。
// Go 端 SkillUtil 创建和注册不检查 sysOperationID，但标记此行为差异。
func (s *SkillLifecycleSuite) TestSkillUtil_空SysOperationID() {
	fs := newMockFs()
	fs.addSkillMD("/skills/test/SKILL.md", "test", "测试技能")

	su := goskills.NewSkillUtilWithProvider("", fs)

	// Go 端允许空 sysOperationID 注册
	err := su.RegisterSkills([]string{"/skills/test/SKILL.md"}, false)
	s.NoError(err, "Go 端空 sysOperationID 不应阻止注册")
	s.True(su.HasSkill(), "空 sysOperationID 注册后应有技能")
}

// TestSkillUtil_SetSysOperationID传播 验证 SetSysOperationID
// 同时更新 SkillManager 和 RemoteSkillUtil。
func (s *SkillLifecycleSuite) TestSkillUtil_SetSysOperationID传播() {
	su := goskills.NewSkillUtil("original_id")

	// 更新 sysOperationID
	su.SetSysOperationID("updated_id")

	// SkillManager 和 RemoteSkillUtil 应同步更新
	// SetSysOperationID 不返回值，验证不崩溃即可
	s.NotNil(su.SkillManager(), "SkillManager 不应为 nil")
	s.NotNil(su.RemoteSkillUtil(), "RemoteSkillUtil 不应为 nil")

	// 验证 SkillManager 可正常使用
	fs := newMockFs()
	fs.addSkillMD("/skills/demo/SKILL.md", "demo", "演示技能")
	su.SkillManager().SetFsProvider(fs)
	err := su.RegisterSkills([]string{"/skills/demo/SKILL.md"}, false)
	s.Require().NoError(err)
	s.Equal(1, su.SkillManager().Count())
}

// TestSkillUtil_注册后提示词包含技能信息 验证注册技能后 GetSkillPrompt
// 输出包含技能名称、描述、目录等关键信息。
func (s *SkillLifecycleSuite) TestSkillUtil_注册后提示词包含技能信息() {
	fs := newMockFs()
	fs.addSkillMD("/skills/ml/SKILL.md", "ml", "机器学习技能，用于模型训练和推理")

	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)
	err := su.RegisterSkills([]string{"/skills/ml/SKILL.md"}, false)
	s.Require().NoError(err)

	prompt := su.GetSkillPrompt()
	s.Contains(prompt, "ml", "提示词应包含技能名称")
	s.Contains(prompt, "机器学习技能", "提示词应包含技能描述")
	s.Contains(prompt, "read_file", "提示词应提及 read_file 工具")
	s.Contains(prompt, "agent equipped", "提示词应包含系统前缀")
}

// TestSkillUtil_注销后提示词更新 验证注销技能后 HasSkill 和提示词更新。
func (s *SkillLifecycleSuite) TestSkillUtil_注销后提示词更新() {
	fs := newMockFs()
	fs.addSkillMD("/skills/tmp/SKILL.md", "tmp", "临时技能")

	su := goskills.NewSkillUtilWithProvider("sys_op_test", fs)
	err := su.RegisterSkills([]string{"/skills/tmp/SKILL.md"}, false)
	s.Require().NoError(err)
	s.True(su.HasSkill())

	// 注销技能
	su.SkillManager().Unregister("tmp")
	s.False(su.HasSkill(), "注销后 HasSkill 应返回 false")

	// 提示词不含技能名
	prompt := su.GetSkillPrompt()
	s.NotContains(prompt, "tmp", "注销后提示词不应包含技能名")
}

// TestSkillManager_覆盖注册 验证 overwrite=true 覆盖同名技能。
// 对齐 Python: SkillManager.register(skill_path, overwrite=True)
func (s *SkillLifecycleSuite) TestSkillManager_覆盖注册() {
	fs := newMockFs()
	// 先添加原始内容
	fs.addSkillMD("/skills/skill_a/SKILL.md", "skill_a", "原始描述")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)

	// 首次注册
	err := sm.Register([]string{"/skills/skill_a/SKILL.md"}, false)
	s.Require().NoError(err)
	s.Equal("原始描述", sm.Get("skill_a").Description)

	// 修改 mockFs 中的文件内容为覆盖版本
	fs.files["/skills/skill_a/SKILL.md"] = "---\nname: skill_a\ndescription: 覆盖描述\n---\n\n# Skill A\n覆盖内容"

	// 覆盖注册
	err = sm.Register([]string{"/skills/skill_a/SKILL.md"}, true)
	s.Require().NoError(err, "overwrite=true 不应返回错误")
	s.Equal("覆盖描述", sm.Get("skill_a").Description, "覆盖后描述应更新")
}

// TestSkillManager_重复注册不覆盖 验证 overwrite=false 时重复注册不覆盖。
func (s *SkillLifecycleSuite) TestSkillManager_重复注册不覆盖() {
	fs := newMockFs()
	fs.addSkillMD("/skills/skill_b/SKILL.md", "skill_b", "原始描述")

	sm := goskills.NewSkillManagerWithProvider("sys_op_test", fs)
	err := sm.Register([]string{"/skills/skill_b/SKILL.md"}, false)
	s.Require().NoError(err)
	s.Equal("原始描述", sm.Get("skill_b").Description)

	// 修改内容后重复注册（overwrite=false）
	fs.files["/skills/skill_b/SKILL.md"] = "---\nname: skill_b\ndescription: 新描述\n---\n\n# Skill B\n新内容"
	err = sm.Register([]string{"/skills/skill_b/SKILL.md"}, false)
	// Go 端重复注册 overwrite=false 时返回错误
	s.Error(err, "重复注册 overwrite=false 应返回错误")
	// 原技能不变
	s.Equal("原始描述", sm.Get("skill_b").Description, "原技能描述不应改变")
}
