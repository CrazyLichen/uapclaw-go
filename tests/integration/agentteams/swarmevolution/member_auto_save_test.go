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

// TestE4成员AutoSave与Sharing auto_save 免审批落盘 + sharing 上传到 hub 目录。
func (s *EvolutionE2ESuite) TestE4成员AutoSave与Sharing() {
	d := s.newAdapter()
	s.adapter = d
	s.makeSkill("evo_member_skill", "成员 auto_save 演示技能")

	// hub 与本地缓存目录（local_file 后端，文件系统即真实边界）
	hubDir := filepath.Join(s.T().TempDir(), "hub")
	cacheDir := filepath.Join(s.T().TempDir(), "cache")
	s.Require().NoError(os.MkdirAll(hubDir, 0o755))

	// 成员 rail：auto_save=true + sharing enabled（构造参数与生产成员装配一致）
	memberModel := s.newMockModel()
	memberRail := coreevolution.NewSkillEvolutionRail(
		[]string{s.skillsDir}, memberModel, "test-model", "cn",
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
	s.mockScript(e4GenerateResp)

	// ① staging（requiresApproval=false → auto-approve 直落盘）
	request, event, err := memberRail.StageEvolutionFromSignalsForTest(
		s.ctx, "evo_member_skill", "部署排障经验（"+e4Marker+"）", true)
	s.Require().NoError(err)
	_ = event
	s.Require().NotNil(request, "auto-approve 仍返回 staged request（含 PendingChange）")

	// ② 落盘断言：免审批直接写 evolutions.json，内容含标记
	evoLog, err := memberRail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_member_skill")
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1, "auto_save 应免审批直接落盘 1 条")
	s.Assert().Contains(evoLog.Entries[0].Change.Content, e4Marker)

	// ③ sharing 断言：hub 目录出现该技能的上传产物（local_file 后端真实写盘）
	entries, _ := os.ReadDir(hubDir)
	found := false
	for _, entry := range entries {
		data, rerr := os.ReadFile(filepath.Join(hubDir, entry.Name()))
		if rerr == nil && len(data) > 0 {
			// hub 产物为 JSON 批文件；关键字段命中即视为上传成功
			raw := string(data)
			if strings.Contains(raw, e4Marker) || strings.Contains(raw, "evo_member_skill") {
				found = true
			}
		}
	}
	s.Assert().True(found, "sharing hub 目录应出现上传产物（entries=%d）", len(entries))
}
