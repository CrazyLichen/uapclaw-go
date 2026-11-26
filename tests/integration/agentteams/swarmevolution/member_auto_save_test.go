//go:build integration

// E4 成员 auto_save + sharing（团队成员视角：auto_save=true 免审批直落盘 + 共享上传）。
//
// 链路（mock 仅 LLM）：成员 rail（auto_save=true + sharing enabled，hub_path=测试目录）
// → signal staging（requiresApproval=false）→ orchestrator auto-approve →
// manager.ApproveRequest 落盘 → sharingAfterAutoApproved → stageRecordsForShare +
// flushShareUploads → local_file 后端写 hub 目录。
// Python 锚点：SkillEvolutionSharingMixin._sharing_after_auto_approved /
// _upload_approved_records_for_sharing（skill_evolution_rail.py 系）。
package swarmevolution

import (
	"os"
	"path/filepath"
	"strings"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// e4Marker 成员 auto_save 落盘特征标记。
const e4Marker = "E2E-MARK-MEMBER-AUTO-SAVE"

// e4GenerateResp 成员生成台本（skill GenerateRecords 契约：records 数组）。
const e4GenerateResp = `[
	{"action": "append", "section": "Troubleshooting", "target": "body", "content": "` + e4Marker + ` 端口占用排查法", "summary": "端口占用排查（auto_save）"}
]`

// TestE4成员AutoSave落盘 auto_save 免审批直落盘；sharing 分支与上游同为
// no-op（auto-approve 先 commit 清空 payload，_sharing_after_auto_approved 读到
// 空 records 返回——Python skill_evolution_sharing.py:339-354 + common.py L112
// 同行为；真实上传链在审批路径，见 TestE4审批路径Sharing上传）。
func (s *EvolutionE2ESuite) TestE4成员AutoSave落盘() {
	d := s.newAdapter()
	s.adapter = d
	s.makeSkill("evo_member_skill", "成员 auto_save 演示技能")

	hubDir := filepath.Join(s.T().TempDir(), "hub")
	s.Require().NoError(os.MkdirAll(hubDir, 0o755))

	// 成员 rail：auto_save=true + sharing enabled
	memberModel := s.newMockModel()
	memberRail := coreevolution.NewSkillEvolutionRail(
		[]string{s.skillsDir}, memberModel, "test-model", "cn",
		coreevolution.WithSignalTrigger(false),
		coreevolution.WithAutoSave(true),
		coreevolution.WithReviewTrigger(true),
		coreevolution.WithEvolutionReviewRuntime(review.NewEvolutionReviewRuntime(60.0)),
		coreevolution.WithSharingConfigMap(map[string]any{
			"enabled":  true,
			"backend":  "local_file",
			"hub_path": hubDir,
		}),
	)
	s.mockScript(e4GenerateResp)

	request, _, err := memberRail.StageEvolutionFromSignalsForTest(
		s.ctx, "evo_member_skill", "部署排障经验（"+e4Marker+"）", true)
	s.Require().NoError(err)
	s.Require().NotNil(request)

	// 落盘断言：免审批直接写 evolutions.json，内容含标记
	evoLog, err := memberRail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_member_skill")
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "auto_save 应免审批直接落盘 1 条")
	s.Assert().Contains(evoLog.Entries[0].Change.Content, e4Marker)

	// sharing 边界断言（与上游一致）：auto-approve 路径 payload 已清空 →
	// sharing no-op → hub 目录无产物
	hubEntries, _ := os.ReadDir(hubDir)
	s.Assert().Empty(hubEntries, "auto-approve 路径 sharing 应 no-op（Python 同行为）")
}

// TestE4审批路径Sharing上传 审批批准路径的真实上传链（records 在 commit 前
// 从快照读取）：stage pending → ApproveRecord → hub 产物写盘。
func (s *EvolutionE2ESuite) TestE4审批路径Sharing上传() {
	d := s.newAdapter()
	s.adapter = d
	s.makeSkill("evo_share_skill", "审批路径共享演示技能")

	hubDir := filepath.Join(s.T().TempDir(), "hub")
	cacheDir := filepath.Join(s.T().TempDir(), "cache")
	s.Require().NoError(os.MkdirAll(hubDir, 0o755))
	s.Require().NoError(os.MkdirAll(cacheDir, 0o755))

	rail := coreevolution.NewSkillEvolutionRail(
		[]string{s.skillsDir}, s.newMockModel(), "test-model", "cn",
		coreevolution.WithSignalTrigger(false),
		coreevolution.WithAutoSave(false),
		coreevolution.WithReviewTrigger(true),
		coreevolution.WithEvolutionReviewRuntime(review.NewEvolutionReviewRuntime(60.0)),
		coreevolution.WithSharingConfigMap(map[string]any{
			"enabled":         true,
			"backend":         "local_file",
			"hub_path":        hubDir,
			"local_cache_dir": cacheDir,
		}),
	)
	s.mockScriptN(e4GenerateResp, 6) // 生成 + keyword/dedup 等 LLM 调用都要台本

	request, _, err := rail.StageEvolutionFromSignalsForTest(
		s.ctx, "evo_share_skill", "部署排障经验（"+e4Marker+"）", false)
	s.Require().NoError(err)
	s.Require().NotNil(request)

	err = rail.ApproveRecord(s.ctx, request.RequestID)
	s.Require().NoError(err)

	// hub 产物为目录结构（bundles/index/packages 子目录）——递归遍历找 bundle 文件
	found := false
	walkErr := filepath.WalkDir(hubDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil && (strings.Contains(string(data), e4Marker) || strings.Contains(string(data), "evo_share_skill")) {
			found = true
		}
		return nil
	})
	s.Require().NoError(walkErr)
	s.Assert().True(found, "hub bundle 文件应含标记或技能名")
}
