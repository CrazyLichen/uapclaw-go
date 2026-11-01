//go:build integration

package github_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	goskills "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fixtureRepoOwner fixture 仓库 owner（本仓库）
	fixtureRepoOwner = "CrazyLichen"
	// fixtureRepoName fixture 仓库名（本仓库）
	fixtureRepoName = "uapclaw-go"
	// fixtureDirectory fixture 在仓库中的目录（用 Directory 定位子树）
	fixtureDirectory = "tests/fixtures/remote-skills"
	// expectFileCount 期望收集到的文件数（两个 SKILL.md + 一个 references）
	expectFileCount = 3
	// expectReferenceContent references 文件期望内容（无尾换行，与 fixture 逐字节一致）
	expectReferenceContent = "# Example Reference\n\nExample Reference"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RemoteSkillSuite GitHub 远程技能集成测试。
//
// 对齐 Python: tests/system_tests/agent/skill/test_remote_skill.py。
type RemoteSkillSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRemoteSkillSuite 运行远程技能集成测试套件
func TestRemoteSkillSuite(t *testing.T) {
	suite.Run(t, new(RemoteSkillSuite))
}

// TestSearchGitHubForSkills_收集技能文件 对齐 Python: test_fetch_skill_from_github
func (s *RemoteSkillSuite) TestSearchGitHubForSkills_收集技能文件() {
	s.skipUnlessGitHubTest()

	tree := goskills.NewGitHubTree(fixtureRepoOwner, fixtureRepoName, gitHubRef(), fixtureDirectory)
	util := goskills.NewRemoteSkillUtil("itest_sysop")

	files, skillPaths, err := util.SearchGitHubForSkills(s.Ctx, tree, os.Getenv("GITHUB_TOKEN"))
	s.Require().NoError(err, "GitHub Tree API 调用失败")
	s.Len(files, expectFileCount, "应收集 3 个文件（2 个 SKILL.md + 1 个 references）")
	s.ElementsMatch([]string{"example-skill", "second-skill"}, skillPaths, "应识别 2 个技能")

	for _, f := range files {
		s.NotContains(f.Path, "README.md", "README 不应被收集")
	}
}

// TestDownloadFileFromGitHub_取回原始内容 对齐 Python: test_download_skill_from_github
func (s *RemoteSkillSuite) TestDownloadFileFromGitHub_取回原始内容() {
	s.skipUnlessGitHubTest()

	tree := goskills.NewGitHubTree(fixtureRepoOwner, fixtureRepoName, gitHubRef(), fixtureDirectory)
	data, err := goskills.DownloadFileFromGitHub(s.Ctx, tree,
		fixtureDirectory+"/skills/example-skill/references/example-reference.md",
		os.Getenv("GITHUB_TOKEN"))
	s.Require().NoError(err, "GitHub Contents API 调用失败")
	s.Equal(expectReferenceContent, string(data), "内容应逐字节一致")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// skipUnlessGitHubTest RUN_GITHUB_TEST!=1 时跳过（对齐 Python 守卫语义）
func (s *RemoteSkillSuite) skipUnlessGitHubTest() {
	if os.Getenv("RUN_GITHUB_TEST") != "1" {
		s.T().Skip("GitHub 远程技能测试未启用。设置 RUN_GITHUB_TEST=1 开启（对齐 Python 守卫）。")
	}
	if os.Getenv("GITHUB_TOKEN") == "" {
		s.T().Log("未设置 GITHUB_TOKEN，匿名调用可能触发 60 次/小时限流")
	}
}

// gitHubRef 返回要读取的 tree ref：优先 GITHUB_SKILL_TEST_REF，缺省 main
func gitHubRef() string {
	if ref := os.Getenv("GITHUB_SKILL_TEST_REF"); ref != "" {
		return ref
	}
	return "main"
}
