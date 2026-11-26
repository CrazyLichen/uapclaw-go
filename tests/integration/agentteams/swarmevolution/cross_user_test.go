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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	sharing "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/sharing"
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

	// 用户 A：创建同名 skill + auto_save 上传
	railA := s.newSharingRail(userADir, cacheA, hubDir)
	s.Require().NoError(os.MkdirAll(filepath.Join(userADir, "shared_skill"), 0o755))
	storeA := checkpointing.NewEvolutionStore([]string{userADir})
	_, err := storeA.CreateSkill(s.ctx, "shared_skill", "跨用户共享演示技能", "# shared_skill\n\n正文\n", "")
	s.Require().NoError(err)
	s.mockScript(e4GenerateResp)
	_, _, err = railA.StageEvolutionFromSignalsForTest(
		s.ctx, "shared_skill", "部署排障经验（"+e9Marker+"）", true)
	s.Require().NoError(err)

	// hub 侧断言：A 的上传产物已写盘
	hubEntries, _ := os.ReadDir(hubDir)
	s.Require().NotEmpty(hubEntries, "A 上传后 hub 目录应有 bundle")

	// 用户 B：检索 + 下载（真实 backend.DownloadBundles → mirror 缓存）
	railB := s.newSharingRail(userBDir, cacheB, hubDir)
	sharerB := railB.ExperienceSharer()
	s.Require().NotNil(sharerB)

	skillID, err := sharerB.ResolveSkillID(s.ctx, "shared_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(skillID)

	query := sharing.QueryKeywords{
		Keywords: []string{"端口占用", "部署"},
		Intent:   "部署排障",
	}
	bundles, err := sharerB.DownloadRelevant(s.ctx, skillID, query, 5, "shared_skill")
	s.Require().NoError(err)
	s.Require().NotEmpty(bundles, "B 应能从 hub 下载到 A 上传的 bundle")

	// 跨用户内容断言：bundle 内容含 A 的标记（全程无丢失，经 hub 传递）
	contents := ""
	for _, bundle := range bundles {
		raw, merr := jsonMarshalBundle(bundle)
		s.Require().NoError(merr)
		contents += raw
	}
	s.Assert().True(strings.Contains(contents, e9Marker), "下载 bundle 应含 A 侧标记")

	// mirror 断言：B 的本地缓存目录出现下载镜像文件
	cacheEntries, _ := os.ReadDir(cacheB)
	s.Assert().NotEmpty(cacheEntries, "mirror 应写入 B 的 local_cache_dir")
}

// jsonMarshalBundle bundle 序列化（内容断言用）。
func jsonMarshalBundle(bundle any) (string, error) {
	data, err := json.Marshal(bundle)
	return string(data), err
}
