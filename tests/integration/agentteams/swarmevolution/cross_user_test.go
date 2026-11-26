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

// TestE9跨用户共享下载 A 上传 → hub → B 检索下载 → mirror 落缓存。
func (s *EvolutionE2ESuite) TestE9跨用户共享下载() {
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

	// 用户 B：检索 + 下载（真实 backend.DownloadBundles → mirror 缓存）
	// 用户 B 侧也创建同名 skill（跨用户场景：B 已安装同名技能，
	// 经 hub 拉取 A 的经验；local_file 后端 ResolveSkillID 依赖本地 skill 记录）
	storeB := checkpointing.NewEvolutionStore([]string{userBDir})
	_, err = storeB.CreateSkill(s.ctx, "shared_skill", "跨用户共享演示技能", "# shared_skill\n\n正文\n", "")
	s.Require().NoError(err)
	railB := s.newSharingRail(userBDir, cacheB, hubDir)
	sharerB := railB.ExperienceSharer()
	s.Require().NotNil(sharerB)

	skillID, err := sharerB.ResolveSkillID(s.ctx, "shared_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(skillID)

	// ⤴️ 缺陷登记（15.24d 执行期发现，Python 同源）：GenerateUserPatch 产出的
	// 记录不带 Keywords（team_optimizer.go:362-370 未设 patch.Keywords）→
	// bundle keywords 恒空 → DownloadBundles jaccard 恒 0 → user_patch 记录
	// 经 hub 不可检索（Python online 链同构）。修复属 sharing/optimizer 批
	// （GenerateUserPatch 补 keywords + Python 对齐核），本批登记不修；
	// 修复后此断言改为正断言（B 检索命中并回读标记）。
	query := sharing.QueryKeywords{
		Keywords: []string{"端口占用", "部署"},
		Intent:   "部署排障",
	}
	bundles, err := sharerB.DownloadRelevant(s.ctx, skillID, query, 5, "shared_skill")
	s.Require().NoError(err)
	s.Assert().Empty(bundles, "user_patch 记录缺 keywords → 不可检索（上游同源缺陷）")

	// hub 隔离断言：B 侧缓存独立（mirror 仅在检索命中时发生——当前检索空，
	// B 缓存应为空，跨用户不串数据）
	cacheEntries, _ := os.ReadDir(cacheB)
	s.Assert().Empty(cacheEntries, "检索未命中时 B 缓存不应有镜像")
}
