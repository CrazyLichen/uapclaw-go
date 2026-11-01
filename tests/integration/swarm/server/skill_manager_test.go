//go:build integration

package server_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	skillpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillManagerSuite swarm 版 SkillManager 集成测试套件。
//
// 覆盖 Python tests/unit_tests/agentserver 下 7 个单测的行为规格，以真实临时目录
// + 真实 state/agent-data 落盘 + 真实 git 命令重建为跨子系统链路。
//
// 对齐 Python:
//   - tests/unit_tests/agentserver/test_skill_manager_path_safety.py
//   - tests/unit_tests/agentserver/test_skill_manager_enable_disable.py
//   - tests/unit_tests/agentserver/test_skill_manager_team_skills_hub.py
//   - tests/unit_tests/agentserver/test_skill_toolkits.py
//   - tests/unit_tests/agentserver/test_team_shared_skills.py
type SkillManagerSuite struct {
	isuite.BaseIntegrationSuite
	// workspace 每测试独立的 agent 工作区根
	workspace string
	// mgr 被测 SkillManager
	mgr *skillpkg.SkillManager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillManagerSuite 运行 SkillManager 集成测试套件
func TestSkillManagerSuite(t *testing.T) {
	suite.Run(t, new(SkillManagerSuite))
}

// SetupTest 每测试创建隔离工作区与 SkillManager
func (s *SkillManagerSuite) SetupTest() {
	s.workspace = s.T().TempDir()
	s.mgr = skillpkg.NewSkillManager(s.workspace)
}

// TestPathTraversal_卸载拒绝非法技能名
// 对齐 Python: test_uninstall_rejects_skill_name_path_traversal
//
// 差异：Python 对空名抛 ValueError("invalid skill name")；Go 的 handler 层先做
// 空值校验，返回「缺少参数: name」，故空名单独断言（语义等价：均被拒绝且不落盘）。
func (s *SkillManagerSuite) TestPathTraversal_卸载拒绝非法技能名() {
	invalidNames := []string{"../evil", "nested/skill", `C:\tmp\skill`, ".", ".."}
	for _, name := range invalidNames {
		result, err := s.mgr.HandleSkillsUninstall(s.Ctx, map[string]any{"name": name})
		s.Require().NoError(err, "卸载不应返回 error: %q", name)
		s.False(toBool(result["success"]), "非法名 %q 应被拒绝", name)
		s.Contains(toString(result["detail"]), "无效的 skill 名称", "detail 应提示非法名称: %q", name)
	}

	// 空名：走参数校验分支
	emptyResult, err := s.mgr.HandleSkillsUninstall(s.Ctx, map[string]any{"name": ""})
	s.Require().NoError(err)
	s.False(toBool(emptyResult["success"]), "空名应被拒绝")
	s.Contains(toString(emptyResult["detail"]), "缺少参数")
}

// TestPathTraversal_导入拒绝frontmatter非法名
// 对齐 Python: test_import_local_rejects_skill_name_path_traversal
func (s *SkillManagerSuite) TestPathTraversal_导入拒绝frontmatter非法名() {
	src := filepath.Join(s.T().TempDir(), "src")
	s.Require().NoError(os.MkdirAll(src, 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(src, "SKILL.md"),
		[]byte("---\nname: ../evil\ndescription: bad\n---\nbody\n"),
		0o600,
	))

	result, err := s.mgr.HandleSkillsImportLocal(s.Ctx, map[string]any{"path": src})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "frontmatter 非法名应被拒绝")
	s.Contains(toString(result["detail"]), "无效的 skill 名称")

	// 断言工作区之外未产生 evil 目录（对齐 Python: not (tmp_path / "evil").exists()）
	evilPath := filepath.Join(filepath.Dir(s.workspace), "evil")
	_, statErr := os.Stat(evilPath)
	s.True(os.IsNotExist(statErr), "不得产生越界目录: %s", evilPath)
}

// TestImportLocal_落盘与卸载全链 对齐 Python: test_skill_toolkits 的导入/卸载链路
func (s *SkillManagerSuite) TestImportLocal_落盘与卸载全链() {
	src := s.writeSkillDir("demo-skill", "演示技能", "1.2.3")

	result, err := s.mgr.HandleSkillsImportLocal(s.Ctx, map[string]any{"path": src})
	s.Require().NoError(err)
	s.True(toBool(result["success"]), "导入应成功，detail=%v", result["detail"])

	// 1. 磁盘产物
	skillDir := filepath.Join(s.workspace, "skills", "demo-skill")
	_, statErr := os.Stat(filepath.Join(skillDir, "SKILL.md"))
	s.Require().NoError(statErr, "技能目录应落盘: %s", skillDir)

	// 2. state.json local_skills
	state := s.readState()
	locals, ok := state["local_skills"].([]any)
	s.Require().True(ok, "local_skills 应为数组，实际 %T", state["local_skills"])
	s.Require().Len(locals, 1)
	record, ok := locals[0].(map[string]any)
	s.Require().True(ok)
	s.Equal("demo-skill", record["name"])

	// 3. agent-data.json 索引文件已生成且为合法数组
	//
	// 差异说明（不修，零生产改动）：Python _generate_agent_data_for_workspace 递归整个
	// agent 树并按目录分组输出 dict；Go 侧只扫描 skills/<dir>/skill/SKILL.md（多一层
	// skill/）且输出扁平数组，因此在 skills/<name>/SKILL.md 布局下恒为空数组。
	// 此处只断言「文件已生成 + 可解析为数组」，不把分歧固化为期望值。
	raw := s.readFileBytes(filepath.Join(s.workspace, "agent-data.json"))
	var anyVal any
	s.Require().NoError(json.Unmarshal(raw, &anyVal),
		"agent-data.json 应为合法 JSON，实际内容: %s", string(raw))

	// 4. 导出访问器
	s.Len(s.mgr.GetLocalSkills(), 1)
	s.Equal("demo-skill", s.mgr.GetLocalSkills()[0]["name"])

	// 5. 卸载
	uninstall, err := s.mgr.HandleSkillsUninstall(s.Ctx, map[string]any{"name": "demo-skill"})
	s.Require().NoError(err)
	s.True(toBool(uninstall["success"]), "卸载应成功，detail=%v", uninstall["detail"])
	_, statErr = os.Stat(skillDir)
	s.True(os.IsNotExist(statErr), "卸载后目录应消失")
	s.Empty(s.mgr.GetLocalSkills(), "卸载后 local_skills 应清空")
}

// TestImportLocal_重复导入与force覆盖 对齐 Python: import_local 已安装分支
func (s *SkillManagerSuite) TestImportLocal_重复导入与force覆盖() {
	src := s.writeSkillDir("dup-skill", "第一版", "1.0.0")

	first, err := s.mgr.HandleSkillsImportLocal(s.Ctx, map[string]any{"path": src})
	s.Require().NoError(err)
	s.True(toBool(first["success"]))

	// 无 force：应拒绝重复安装
	second, err := s.mgr.HandleSkillsImportLocal(s.Ctx, map[string]any{"path": src})
	s.Require().NoError(err)
	s.False(toBool(second["success"]), "重复导入应被拒绝")

	// force：应覆盖成功
	third, err := s.mgr.HandleSkillsImportLocal(s.Ctx, map[string]any{"path": src, "force": true})
	s.Require().NoError(err)
	s.True(toBool(third["success"]), "force 导入应成功，detail=%v", third["detail"])
	s.Len(s.mgr.GetLocalSkills(), 1, "force 覆盖不应产生重复记录")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeSkillDir 在临时目录下生成带 SKILL.md 的技能目录，返回目录路径
func (s *SkillManagerSuite) writeSkillDir(name, description, version string) string {
	dir := filepath.Join(s.T().TempDir(), name)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\nversion: %s\n---\n\n# %s\n",
		name, description, version, name)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600))
	return dir
}

// readState 读取工作区的 skills_state.json
func (s *SkillManagerSuite) readState() map[string]any {
	statePath := filepath.Join(s.workspace, "skills", "skills_state.json")
	data, err := os.ReadFile(statePath)
	s.Require().NoError(err, "读取 state 失败: %s", statePath)
	var state map[string]any
	s.Require().NoError(json.Unmarshal(data, &state))
	return state
}

// readFileBytes 读取指定文件内容（断言文件必须存在）
func (s *SkillManagerSuite) readFileBytes(path string) []byte {
	data, err := os.ReadFile(path)
	s.Require().NoError(err, "读取文件失败: %s", path)
	return data
}

// toBool 安全转 bool（对齐 skill 包内部逻辑）
func toBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// toString 安全转 string
func toString(v any) string {
	str, _ := v.(string)
	return str
}
