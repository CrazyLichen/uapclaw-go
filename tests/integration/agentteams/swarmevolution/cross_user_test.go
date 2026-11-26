//go:build integration

// E9 跨用户共享下载链（用户 A 上传 → hub → 用户 B 检索/下载/mirror）。
//
// 链路（mock 仅 LLM；文件系统 = 真实边界）：双用户双 rail（各自 skills 目录 +
// 独立 local_cache_dir，仅共享 hub_path）→ A 侧 auto_save 上传 bundle →
// B 侧 ResolveSkillID → DownloadRelevant（真实 backend.DownloadBundles）→
// bundle 内容含 A 的标记（跨用户经 hub 传递）→ mirror 写入 B 的缓存目录。
// Python 锚点：SkillEvolutionSharingMixin 系 + ExperienceSharer.download_relevant。
package swarmevolution

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	sharing "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/sharing"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// e9Marker 跨用户下载特征标记（A 侧写入，B 侧回读）。
const e9Marker = "E2E-MARK-CROSS-USER"

// newSharingRail 构造带 sharing 的 rail（隔离 skills 根目录 + 独立缓存目录）。
func (s *EvolutionE2ESuite) newSharingRail(baseDir, cacheDir, hubDir string) *coreevolution.SkillEvolutionRail {
	model := s.newMockModel()
	return coreevolution.NewSkillEvolutionRail(
		[]string{baseDir}, model, "test-model", "cn",
		coreevolution.WithSignalTrigger(false),
		coreevolution.WithAutoSave(true),
		coreevolution.WithReviewTrigger(true),
		coreevolution.WithEvolutionReviewRuntime(review.NewEvolutionReviewRuntime(60.0)),
		coreevolution.WithSharingConfigMap(map[string]any{
			"enabled":         true,
			"backend":         "local_file",
			"hub_path":        hubDir,
			"local_cache_dir": cacheDir,
		}),
	)
}

// makeSkillIn 在指定根目录下创建 skill（跨用户双份）。
func (s *EvolutionE2ESuite) makeSkillIn(baseDir, name string) string {
	store := checkpointing.NewEvolutionStore([]string{baseDir})
	skillDir, err := store.CreateSkill(s.ctx, name, "跨用户共享演示技能", "# "+name+"\n\n正文\n", "")
	s.Require().NoError(err)
	return skillDir
}

// TestSwarmEvolutionCrossUser 本文件审计入口（audit 规则：每个 _test.go 自带 suite.Run）。
func TestSwarmEvolutionCrossUser(t *testing.T) {
	suite.Run(t, new(CrossUserSuite))
}

// CrossUserSuite cross_user_test.go 的文件级 suite（嵌入主 suite 复用基建；audit 规则：每文件自带 suite.Run）
type CrossUserSuite struct {
	EvolutionE2ESuite
}

// TestE9跨用户共享下载 A 上传 → hub → B 检索下载 → mirror 落缓存。
func (s *CrossUserSuite) TestE9跨用户共享下载() {
	d := s.newAdapter()
	s.adapter = d
	hubDir := filepath.Join(s.T().TempDir(), "hub")
	s.Require().NoError(os.MkdirAll(hubDir, 0o755))

	userADir := filepath.Join(s.T().TempDir(), "user_a_skills")
	cacheA := filepath.Join(s.T().TempDir(), "cache_a")
	userBDir := filepath.Join(s.T().TempDir(), "user_b_skills")
	cacheB := filepath.Join(s.T().TempDir(), "cache_b")
	for _, dir := range []string{userADir, userBDir} {
		s.Require().NoError(os.MkdirAll(dir, 0o755))
	}
	s.Require().NoError(os.MkdirAll(cacheA, 0o755))
	s.Require().NoError(os.MkdirAll(cacheB, 0o755))

	// 用户 A：与 E4 审批路径同构造（workspace skills 根；跨用户隔离由
	// 独立 local_cache_dir 与 hub 中转保证——B 侧 skills/缓存均为独立目录）
	railA := s.newSharingRail(s.skillsDir, cacheA, hubDir)
	_ = userADir
	storeA := checkpointing.NewEvolutionStore([]string{s.skillsDir})
	_, err := storeA.CreateSkill(s.ctx, "shared_skill", "跨用户共享演示技能", "# shared_skill\n\n正文\n", "")
	s.Require().NoError(err)
	s.mockScriptN(e4GenerateResp, 6)
	// 上传走审批路径（auto-approve 路径 payload 先被 commit 清空，sharing no-op
	// ——与上游一致；真实上传链 = ApproveRecord commit 前快照 records）
	stagedA, _, serr := railA.StageEvolutionFromSignalsForTest(
		s.ctx, "shared_skill", "部署排障经验（"+e9Marker+"）", false)
	s.Require().NoError(serr)
	s.Require().NotNil(stagedA)
	if sh := railA.ExperienceSharer(); sh == nil {
		s.T().Logf("[DIAG] sharerA nil")
	} else {
		s.T().Logf("[DIAG] sharerA HasPending pre=%v", sh.HasPending("shared_skill"))
	}
	err = railA.ApproveRecord(s.ctx, stagedA.RequestID)
	s.Require().NoError(err)
	if sh := railA.ExperienceSharer(); sh != nil {
		s.T().Logf("[DIAG] sharerA HasPending post=%v", sh.HasPending("shared_skill"))
	}
	_ = filepath.WalkDir(hubDir, func(path string, dd os.DirEntry, err error) error {
		if err == nil && !dd.IsDir() {
			s.T().Logf("[DIAG] hub file %s", path)
		}
		return nil
	})

	// hub 侧断言：A 的上传产物已写盘
	hubEntries, _ := os.ReadDir(hubDir)
	s.Require().NotEmpty(hubEntries, "A 上传后 hub 目录应有 bundle")

	// [DIAG-E9] hub bundle keywords 实况
	filepath.WalkDir(hubDir, func(path string, dd os.DirEntry, err error) error {
		if err == nil && !dd.IsDir() {
			if data, rerr := os.ReadFile(path); rerr == nil && len(data) > 0 {
				head := string(data)
				if len(head) > 200 {
					head = head[:200]
				}
				s.T().Logf("[DIAG-E9] %s head=%q", path, head)
			}
		}
		return nil
	})
	// 用户 B：检索 + 下载（真实 backend.DownloadBundles → mirror 缓存）
	// 用户 B 侧也创建同名 skill（跨用户场景：B 已安装同名技能，
	// 经 hub 拉取 A 的经验；local_file 后端 ResolveSkillID 依赖本地 skill 记录）
	storeB := checkpointing.NewEvolutionStore([]string{userBDir})
	_, err = storeB.CreateSkill(s.ctx, "shared_skill", "跨用户共享演示技能", "# shared_skill\n\n正文\n", "")
	s.Require().NoError(err)
	railB := s.newSharingRail(userBDir, cacheB, hubDir)
	sharerB := railB.ExperienceSharer()
	s.Require().NotNil(sharerB)

	// ⤴️ 语义澄清（2026-12-26 会话8）：A、B 两侧同名 skill 的 skill_id 不同
	//（ResolveSkillID 含内容哈希外的分量）——跨用户检索的真实入口是
	// SearchSkills（全局索引 global.jsonl，按名称/描述匹配）而非本地
	// ResolveSkillID；按 id 检索恒空（原负断言即此假象）。
	searchResults := sharerB.SearchSkills(s.ctx, sharing.QueryKeywords{Keywords: []string{"shared_skill"}}, 5)
	s.Require().NotEmpty(searchResults, "全局检索应命中 A 上传的 skill")
	var skillID string
	for _, res := range searchResults {
		if res.SkillName == "shared_skill" {
			skillID = res.SkillID
		}
	}
	s.Require().NotEmpty(skillID, "SearchSkills 应返回 shared_skill 的 hub skill_id")

	// D-15.24d-4 已修复（GenerateUserPatch keywords 透传 + prompt 要求）：
	// bundle keywords 非空 → jaccard 命中 → B 检索到 A 的 bundle（跨用户标记回读）。
	// 查询词与 A 侧台本 keywords（deploy/port-check/端口占用）取交集保证命中。
	query := sharing.QueryKeywords{
		Keywords: []string{"deploy", "port-check"},
		Intent:   "部署排障",
	}
	bundles, err := sharerB.DownloadRelevant(s.ctx, skillID, query, 5, "shared_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(bundles, "B 应能从 hub 检索到 A 上传的 bundle（D-15.24d-4 修复后转正）")
	contents := ""
	for _, bundle := range bundles {
		for _, exp := range bundle.Experiences {
			contents += exp.Record.Change.Content
		}
	}
	s.Assert().Contains(contents, e4Marker, "检索命中的 bundle 应含 A 侧标记（E9 复用 e4 台本，记录内容=e4Marker；跨用户内容无丢失）")

	// mirror 断言：检索命中后 B 的本地缓存目录出现下载镜像（跨用户不串数据：
	// 仅经 hub 传递，B 的 skills 目录独立）
	cacheEntries, _ := os.ReadDir(cacheB)
	s.Assert().NotEmpty(cacheEntries, "检索命中后 mirror 应写入 B 的 local_cache_dir")
}
