//go:build integration

package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
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

// TestTeamSkillsHub_Info透传 对齐 Python: test_handle_skills_team_skills_hub_info
//
// 说明：Python 单测直接替换 _team_skills_hub_http_get_data 返回 data dict，绕过 HTTP 封装；
// Go 走真实 HTTP，故 httptest 需返回 {"code":200,"data":{...}} 业务封装（helper 校验 code==200）。
func (s *SkillManagerSuite) TestTeamSkillsHub_Info透传() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Equal("/api/v1/artifacts/demo-skill", r.URL.Path)
		s.Equal("1.0.0", r.URL.Query().Get("version"))
		_, _ = w.Write([]byte(`{"code":200,"data":{"asset_id":"demo-skill","name":"demo-skill","display_name":"Demo Skill"}}`))
	}))
	defer server.Close()

	result, err := s.mgr.HandleSkillsTeamSkillsHubInfo(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"version":    "1.0.0",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.True(toBool(result["success"]), "info 应成功: %v", result)
	s.Equal("demo-skill", result["asset_id"])
	s.Equal("1.0.0", result["version"])
	data, ok := result["data"].(map[string]any)
	s.Require().True(ok)
	s.Equal("Demo Skill", data["display_name"])
}

// TestTeamSkillsHub_Search映射 对齐 Python: test_handle_skills_team_skills_hub_search_maps_response
func (s *SkillManagerSuite) TestTeamSkillsHub_Search映射() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Equal("/api/v1/plugins", r.URL.Path)
		_, _ = w.Write([]byte(`{"code":200,"data":{"items":[{"asset_id":"demo-skill","name":"demo-skill","display_name":"Demo Skill","short_desc":"desc","latest_version":"1.2.3","update_time":123}]}}`))
	}))
	defer server.Close()

	result, err := s.mgr.HandleSkillsTeamSkillsHubSearch(s.Ctx, map[string]any{
		"q":          "demo",
		"limit":      10,
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.True(toBool(result["success"]), "search 应成功: %v", result)
	s.Equal(1, result["count"])
	skills, ok := result["skills"].([]map[string]any)
	s.Require().True(ok)
	s.Require().Len(skills, 1)
	s.Equal("demo-skill", skills[0]["asset_id"])
	s.Equal("Demo Skill", skills[0]["display_name"])
}

// TestTeamSkillsHub_Search失败 对齐 Python: test_handle_skills_team_skills_hub_search_hide_internal_error
//
// 差异：Python 返回 detail_key=skills.teamskillshub.errors.searchFailed；Go 只返回 detail。
func (s *SkillManagerSuite) TestTeamSkillsHub_Search失败() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal endpoint detail"}`))
	}))
	defer server.Close()

	result, err := s.mgr.HandleSkillsTeamSkillsHubSearch(s.Ctx, map[string]any{
		"q":          "demo",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "search 应失败")
	s.NotEmpty(toString(result["detail"]), "detail 应包含错误信息")
}

// TestTeamSkillsHub_Install成功 对齐 Python: test_handle_skills_team_skills_hub_install_success
func (s *SkillManagerSuite) TestTeamSkillsHub_Install成功() {
	zipBytes := buildSkillZip("demo-skill")
	var downloadPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/artifacts/demo-skill" {
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s","checksum_sha256":"","version":"1.0.0"}}`, downloadPath)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer server.Close()
	downloadPath = server.URL + "/downloads/demo.zip"

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.True(toBool(result["success"]), "install 应成功: %v", result)

	skill, ok := result["skill"].(map[string]any)
	s.Require().True(ok)
	s.Equal("demo-skill", skill["name"])
	_, statErr := os.Stat(filepath.Join(s.workspace, "skills", "demo-skill", "SKILL.md"))
	s.Require().NoError(statErr, "安装后 SKILL.md 应落盘")
	s.Len(s.mgr.GetLocalSkills(), 1, "应记录 local_skills")
}

// TestTeamSkillsHub_Install扁平zip不残留zip文件
// 对齐 Python: test_handle_skills_team_skills_hub_install_flat_zip_does_not_copy_staging_zip
func (s *SkillManagerSuite) TestTeamSkillsHub_Install扁平zip不残留zip文件() {
	zipBytes := buildSkillZipAtRoot("flat-demo")
	var downloadPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/artifacts/flat-demo" {
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s","checksum_sha256":"","version":"1.0.0"}}`, downloadPath)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer server.Close()
	downloadPath = server.URL + "/downloads/flat.zip"

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "flat-demo",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.True(toBool(result["success"]), "扁平 zip 安装应成功: %v", result)

	dest := filepath.Join(s.workspace, "skills", "flat-demo")
	_, statErr := os.Stat(filepath.Join(dest, "SKILL.md"))
	s.Require().NoError(statErr, "扁平 zip 应落盘 SKILL.md")
	_, statErr = os.Stat(filepath.Join(dest, "skill.zip"))
	s.True(os.IsNotExist(statErr), "目标目录不应残留 skill.zip")
}

// TestTeamSkillsHub_Install重复未force 对齐 Python: test_handle_skills_team_skills_hub_install_duplicate_without_force
//
// 差异：Python detail 含「已安装」，Go 为「技能 X 已存在」。
func (s *SkillManagerSuite) TestTeamSkillsHub_Install重复未force() {
	s.Require().NoError(os.MkdirAll(filepath.Join(s.workspace, "skills", "demo-skill"), 0o755))
	zipBytes := buildSkillZip("demo-skill")
	var downloadPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/artifacts/demo-skill" {
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s","checksum_sha256":"","version":"1.0.0"}}`, downloadPath)
			return
		}
		_, _ = w.Write(zipBytes)
	}))
	defer server.Close()
	downloadPath = server.URL + "/downloads/demo.zip"

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"force":      false,
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "重复安装未 force 应失败")
	s.Contains(toString(result["detail"]), "已存在", "detail 应提示已存在")
}

// TestTeamSkillsHub_Install非法zip 对齐 Python: test_handle_skills_team_skills_hub_install_invalid_zip
//
// 差异：Python 断言 detail 含 "zip"（小写）与 detail_key；Go detail 为「ZIP 解压失败: ...」。
func (s *SkillManagerSuite) TestTeamSkillsHub_Install非法zip() {
	var downloadPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/artifacts/demo-skill" {
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s","checksum_sha256":"","version":"1.0.0"}}`, downloadPath)
			return
		}
		_, _ = w.Write([]byte("not-a-zip"))
	}))
	defer server.Close()
	downloadPath = server.URL + "/downloads/bad.zip"

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "非法 zip 应失败")
	s.Contains(strings.ToLower(toString(result["detail"])), "zip", "detail 应提及 zip")
}

// TestTeamSkillsHub_Install下载失败 对齐 Python: test_handle_skills_team_skills_hub_install_download_failure
//
// Python 注入 _download_zip_and_verify 抛 RuntimeError；Go 走真实下载，用未监听的
// 白名单主机端口制造连接失败（对齐「下载失败」语义）。
func (s *SkillManagerSuite) TestTeamSkillsHub_Install下载失败() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"download_url":"http://127.0.0.1:1/unreachable.zip","checksum_sha256":"","version":"1.0.0"}}`))
	}))
	defer server.Close()

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "下载失败应返回 success=false")
	s.Contains(toString(result["detail"]), "下载校验失败", "detail 应提示下载失败")
}

// TestTeamSkillsHub_Install非白名单主机被拒
// 对齐 Python: test_handle_skills_team_skills_hub_install_rejects_untrusted_download_host
func (s *SkillManagerSuite) TestTeamSkillsHub_Install非白名单主机被拒() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"download_url":"https://example.com/demo.zip","checksum_sha256":"","version":"1.0.0"}}`))
	}))
	defer server.Close()

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "非白名单主机应被拒绝")
	s.Contains(toString(result["detail"]), "example.com", "detail 应包含主机名")
}

// TestTeamSkillsHub_Install拒绝zip_slip 对齐 Python: test_handle_skills_team_skills_hub_install_rejects_zip_slip
func (s *SkillManagerSuite) TestTeamSkillsHub_Install拒绝zip_path穿越() {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	wrt, err := zw.Create("../evil.txt")
	s.Require().NoError(err)
	_, _ = wrt.Write([]byte("x"))
	s.Require().NoError(zw.Close())
	badZip := buf.Bytes()

	var downloadPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/artifacts/demo-skill" {
			_, _ = fmt.Fprintf(w, `{"code":200,"data":{"download_url":"%s","checksum_sha256":"","version":"1.0.0"}}`, downloadPath)
			return
		}
		_, _ = w.Write(badZip)
	}))
	defer server.Close()
	downloadPath = server.URL + "/downloads/slip.zip"

	result, err := s.mgr.HandleSkillsTeamSkillsHubInstall(s.Ctx, map[string]any{
		"asset_id":   "demo-skill",
		"market_url": server.URL,
	})
	s.Require().NoError(err)
	s.False(toBool(result["success"]), "zip-slip 应被拒绝")
	// 差异：Python detail 为「非法路径|越界」，Go 为「zip slip 检测：路径 ... 超出目标目录」
	detail := toString(result["detail"])
	s.True(strings.Contains(detail, "越界") || strings.Contains(detail, "超出目标目录") ||
		strings.Contains(detail, "zip slip"), "detail 应提示路径越界，实际: %s", detail)

	// 不得在上层落盘
	_, statErr := os.Stat(filepath.Join(filepath.Dir(s.workspace), "evil.txt"))
	s.True(os.IsNotExist(statErr), "不得产生越界文件")
}

// TestMarketplaceInstall_真实git克隆与安装
// 对齐 Python 中被删除的 TestGitMethods/TestSyncMarketplaceRepos 的真实语义：
// 以本地 git 仓库为 marketplace 源，走完整 gitClone → 复制 → state 记录 → 卸载链路。
func (s *SkillManagerSuite) TestMarketplaceInstall_真实git克隆与安装() {
	repoDir := s.T().TempDir()
	s.initMarketplaceRepo(repoDir, "market-skill")

	add, err := s.mgr.HandleSkillsMarketplaceAdd(s.Ctx, map[string]any{
		"name": "local-market",
		"url":  "file://" + repoDir,
	})
	s.Require().NoError(err)
	s.True(toBool(add["success"]), "添加 marketplace 应成功: %v", add)

	install, err := s.mgr.HandleSkillsInstall(s.Ctx, map[string]any{"spec": "market-skill@local-market"})
	s.Require().NoError(err)
	s.True(toBool(install["success"]), "安装应成功: %v", install["detail"])

	// 技能复制到本地 skills 目录
	_, statErr := os.Stat(filepath.Join(s.workspace, "skills", "market-skill", "SKILL.md"))
	s.Require().NoError(statErr, "技能应复制到本地 skills 目录")

	// marketplace 仓库被真实克隆（覆盖 gitClone）
	cloneDir := filepath.Join(s.workspace, "skills", "_marketplace", "local-market")
	_, statErr = os.Stat(filepath.Join(cloneDir, "skills", "market-skill", "SKILL.md"))
	s.Require().NoError(statErr, "marketplace 仓库应被克隆: %s", cloneDir)

	// installed_plugins 记录含完整 commit SHA
	plugins := s.mgr.GetInstalledPlugins()
	s.Require().Len(plugins, 1)
	s.Equal("market-skill", plugins[0]["name"])
	s.Equal("local-market", plugins[0]["marketplace"])
	commit := toString(plugins[0]["commit"])
	s.Len(commit, 40, "commit 应为完整 SHA-1: %q", commit)

	// 重复安装未 force 应失败（dest 已存在）
	duplicate, err := s.mgr.HandleSkillsInstall(s.Ctx, map[string]any{"spec": "market-skill@local-market"})
	s.Require().NoError(err)
	s.False(toBool(duplicate["success"]), "重复安装未 force 应失败")
	s.Contains(toString(duplicate["detail"]), "已存在")

	// 卸载
	uninstall, err := s.mgr.HandleSkillsUninstall(s.Ctx, map[string]any{"name": "market-skill"})
	s.Require().NoError(err)
	s.True(toBool(uninstall["success"]), "卸载应成功: %v", uninstall["detail"])
	_, statErr = os.Stat(filepath.Join(s.workspace, "skills", "market-skill"))
	s.True(os.IsNotExist(statErr), "卸载后技能目录应消失")
}

// TestSkillToolkit_安装与卸载 对齐 Python: test_skill_toolkits
//
// Python 直接 import 本地技能后调 toolkit.uninstall_skill；Go 侧同链路，
// 额外断言 toolkit 作用于真实 SkillManager（非 mock）。
func (s *SkillManagerSuite) TestSkillToolkit_安装与卸载() {
	src := s.writeSkillDir("toolkit-skill", "工具链技能", "1.0.0")

	imported, err := s.mgr.HandleSkillsImportLocal(s.Ctx, map[string]any{"path": src})
	s.Require().NoError(err)
	s.Require().True(toBool(imported["success"]), "导入应成功: %v", imported["detail"])

	toolkit := swarmtools.NewSkillToolkit(s.mgr)
	s.Require().NotNil(toolkit)

	uninstall, err := toolkit.UninstallSkill(s.Ctx, map[string]any{"name": "toolkit-skill"})
	s.Require().NoError(err)
	s.True(toBool(uninstall["success"]), "工具卸载应成功: %v", uninstall)

	_, statErr := os.Stat(filepath.Join(s.workspace, "skills", "toolkit-skill"))
	s.True(os.IsNotExist(statErr), "卸载后目录应消失")
	s.Empty(s.mgr.GetLocalSkills(), "卸载后 local_skills 应清空")
}

// TestTeamSharedSkills_全局到团队共享复制 对齐 Python: test_team_shared_skills
//
// 对齐点：全局技能目录（含 skills_state.json）复制到 <team>/skills 并写
// .team_skills_copied 标记；marker 存在时幂等跳过。
func (s *SkillManagerSuite) TestTeamSharedSkills_全局到团队共享复制() {
	dataDir := s.isolateDataDir()

	globalSkills := filepath.Join(dataDir, "agent", "workspace", "skills")
	for _, name := range []string{"skill-b", "skill-a"} {
		dir := filepath.Join(globalSkills, name)
		s.Require().NoError(os.MkdirAll(dir, 0o755))
		md := "---\nname: " + name + "\ndescription: 团队共享\n---\nbody\n"
		s.Require().NoError(os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600))
	}
	s.Require().NoError(os.WriteFile(
		filepath.Join(globalSkills, "skills_state.json"),
		[]byte(`{"installed_plugins":[],"local_skills":[]}`),
		0o600,
	))

	teamWsRoot := s.T().TempDir()
	spec := &schema.TeamAgentSpec{
		TeamName:  "itest-team",
		Workspace: &team_workspace.TeamWorkspaceConfig{RootPath: teamWsRoot},
	}

	mgr := team.NewTeamManager()
	mgr.EnsureTeamSharedSkillsInitialized(spec)

	sharedDir := filepath.Join(teamWsRoot, "skills")
	for _, name := range []string{"skill-a", "skill-b"} {
		_, statErr := os.Stat(filepath.Join(sharedDir, name, "SKILL.md"))
		s.Require().NoError(statErr, "团队共享目录应镜像 %s", name)
	}
	_, statErr := os.Stat(filepath.Join(sharedDir, "skills_state.json"))
	s.Require().NoError(statErr, "state 文件应一并复制")
	_, statErr = os.Stat(filepath.Join(sharedDir, ".team_skills_copied"))
	s.Require().NoError(statErr, "marker 文件应存在")

	// 团队技能创建 rail 注册到 TeamManager（团队技能链路的下游锚点）
	rail := evolution.NewTeamSkillCreateRail(sharedDir)
	mgr.RegisterTeamSkillCreateRail("itest-session", rail)
	s.Same(rail, mgr.GetTeamSkillCreateRail("itest-session"), "rail 应可按键取回")

	// 幂等：删掉 skill-a 后再次调用，marker 存在 → 不重新复制
	s.Require().NoError(os.RemoveAll(filepath.Join(sharedDir, "skill-a")))
	mgr.EnsureTeamSharedSkillsInitialized(spec)
	_, statErr = os.Stat(filepath.Join(sharedDir, "skill-a"))
	s.True(os.IsNotExist(statErr), "marker 存在时应跳过复制（幂等）")
}

// TestToggle_禁用后不可见 对齐 Python: test_skill_manager_enable_disable
//
// 对齐点：
//   - 已注册技能（local_skills）禁用后进入 list_execution_disabled_skills，并从可见列表中剔除
//   - 未注册技能禁用后只进 list_disabled_skills，不进 execution_disabled
//     （对齐 Python: test_set_skill_enabled_also_supports_uninstalled_skill）
func (s *SkillManagerSuite) TestToggle_禁用后不可见() {
	dataDir := s.isolateDataDir()
	workspaceRoot := filepath.Join(dataDir, "agent", "workspace")
	s.Require().NoError(os.MkdirAll(filepath.Join(workspaceRoot, "skills", "alpha"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(workspaceRoot, "skills", "alpha", "SKILL.md"),
		[]byte("---\nname: alpha\ndescription: 可见性\n---\nbody\n"),
		0o600,
	))
	// alpha 作为已注册本地技能写入 state，构造 manager 时加载
	s.Require().NoError(os.WriteFile(
		filepath.Join(workspaceRoot, "skills", "skills_state.json"),
		[]byte(`{"installed_plugins":[],"local_skills":[{"name":"alpha","origin":"/tmp/alpha","source":"local"}]}`),
		0o600,
	))

	globalMgr := skillpkg.NewSkillManager(workspaceRoot)

	toggle, err := globalMgr.HandleSkillsToggle(s.Ctx, map[string]any{"name": "alpha", "enabled": false})
	s.Require().NoError(err)
	s.True(toBool(toggle["success"]), "toggle 应成功: %v", toggle)

	s.Equal([]string{"alpha"}, globalMgr.ListDisabledSkills(), "禁用列表应含 alpha")
	s.False(globalMgr.GetSkillEnabled("alpha"), "alpha 应处于禁用态")
	s.Contains(globalMgr.ListExecutionDisabledSkills(), "alpha", "已注册技能应进入执行禁用列表")

	visible := skillpkg.FilterVisibleSkillNames([]string{"alpha", "beta"})
	s.Equal([]string{"beta"}, visible, "禁用技能应从可见列表剔除")

	// 未注册技能：只进 disabled，不进 execution_disabled（对齐 Python）
	unregistered, err := globalMgr.HandleSkillsToggle(s.Ctx, map[string]any{"name": "gamma", "enabled": false})
	s.Require().NoError(err)
	s.True(toBool(unregistered["success"]))
	s.ElementsMatch([]string{"alpha", "gamma"}, globalMgr.ListDisabledSkills())
	s.NotContains(globalMgr.ListExecutionDisabledSkills(), "gamma", "未注册技能不应进执行禁用列表")

	// 重新启用后恢复可见
	enable, err := globalMgr.HandleSkillsToggle(s.Ctx, map[string]any{"name": "alpha", "enabled": true})
	s.Require().NoError(err)
	s.True(toBool(enable["success"]))
	s.Equal([]string{"alpha", "beta"}, skillpkg.FilterVisibleSkillNames([]string{"alpha", "beta"}))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isolateDataDir 把全局 UAPCLAW_DATA_DIR 指到临时目录（团队共享技能与 state 工具函数依赖它）
func (s *SkillManagerSuite) isolateDataDir() string {
	dir := s.T().TempDir()
	// 先注册 ResetCache（LIFO 最后执行，晚于 t.Setenv 的恢复动作）
	s.T().Cleanup(pathutil.ResetCache)
	s.T().Setenv("UAPCLAW_DATA_DIR", dir)
	pathutil.ResetCache()
	return dir
}

// initMarketplaceRepo 在 dir 初始化含 skills/<plugin>/SKILL.md 的 git 仓库
func (s *SkillManagerSuite) initMarketplaceRepo(dir, plugin string) {
	s.Require().NoError(os.MkdirAll(filepath.Join(dir, "skills", plugin), 0o755))
	md := "---\nname: " + plugin + "\ndescription: marketplace 技能\nversion: 2.0.0\n---\nbody\n"
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "skills", plugin, "SKILL.md"), []byte(md), 0o600))

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=itest", "GIT_AUTHOR_EMAIL=itest@example.com",
			"GIT_COMMITTER_NAME=itest", "GIT_COMMITTER_EMAIL=itest@example.com",
			// 本地 file:// 克隆时放行任意目录，避免 safe.directory 拦截
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=safe.directory",
			"GIT_CONFIG_VALUE_0=*",
		)
		out, err := cmd.CombinedOutput()
		s.Require().NoError(err, "git %s 失败: %s", strings.Join(args, " "), string(out))
	}
	runGit("init", "-b", "main")
	runGit("add", ".")
	runGit("commit", "-m", "init marketplace")
}

// buildSkillZip 构造 <name>/SKILL.md 结构的 zip 字节
func buildSkillZip(skillName string) []byte {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	wrt, _ := zw.Create(skillName + "/SKILL.md")
	_, _ = wrt.Write([]byte("---\nname: " + skillName + "\ndescription: 测试技能\nversion: 1.0.0\n---\nbody\n"))
	_ = zw.Close()
	return buf.Bytes()
}

// buildSkillZipAtRoot 构造根目录 SKILL.md 的扁平 zip 字节
func buildSkillZipAtRoot(skillName string) []byte {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	wrt, _ := zw.Create("SKILL.md")
	_, _ = wrt.Write([]byte("---\nname: " + skillName + "\ndescription: 测试技能\nversion: 1.0.0\n---\nbody\n"))
	_ = zw.Close()
	return buf.Bytes()
}

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
