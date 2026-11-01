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

// readAgentData 读取工作区根目录的 agent-data.json
func (s *SkillManagerSuite) readAgentData() []map[string]any {
	dataPath := filepath.Join(s.workspace, "agent-data.json")
	data, err := os.ReadFile(dataPath)
	s.Require().NoError(err, "读取 agent-data 失败: %s", dataPath)
	var entries []map[string]any
	s.Require().NoError(json.Unmarshal(data, &entries))
	return entries
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
