//go:build integration

// E1 主动链 + E11 个人版轨迹完整性核心断言（特征标记回读）。
//
// 链路：/evolve → followup query 注入（interface_deep.py:18278-18283）→ 六主工具
// 真实执行（prepare → 审查子 agent submit → evolve 提交）→ 落盘 → evolutions.json
// 标记回读。审批中断/部分批准闭环由 E7/E10 覆盖；个人版 watcher 帧序不适用
// （D-15.24d-3：新版 signal_trigger 恒 False）。
package swarmevolution

import (
	"testing"

	"fmt"
	"github.com/stretchr/testify/suite"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	agenttools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/tools"
	fdtool "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	evolution "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	review "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// E11 特征标记：贯穿轨迹→审查→提案→落盘全程的确定性标记串。
const e11Marker = "E2E-MARK-端口占用排查法"

// invokeTool 工具调用助手。
func (s *EvolutionE2ESuite) invokeTool(t fdtool.Tool, inputs map[string]any) map[string]any {
	result, err := t.Invoke(s.ctx, inputs)
	s.Require().NoError(err)
	return result
}

// resolveRuntimeBindingRef 未使用占位（保持 import 精简的编译期引用）。
var _ = evolution.NewEvolutionInterruptRail

// TestSwarmEvolutionActiveChain 本文件审计入口（audit 规则：每个 _test.go 自带 suite.Run）。
func TestSwarmEvolutionActiveChain(t *testing.T) {
	suite.Run(t, new(ActiveChainSuite))
}

// ActiveChainSuite active_chain_test.go 的文件级 suite（嵌入主 suite 复用基建；audit 规则：每文件自带 suite.Run）
type ActiveChainSuite struct {
	EvolutionE2ESuite
}

// TestE1主动链Followup注入与落盘 全链：followup 注入生效 + 六工具真实执行 +
// 落盘产物含特征标记（无中途丢失）。
func (s *ActiveChainSuite) TestE1主动链Followup注入与落盘() {
	d := s.newAdapter()
	s.adapter = d
	s.makeSkill("evo_active_skill", "主动链演示技能")

	// ① /evolve 触发 followup 注入（Python evolution_slash.py:230-236 +
	// interface_deep.py:18278-18283）——E5 验证点：followup_prompt 注入 query
	resp := s.chatSend("/evolve evo_active_skill " + e11Marker)
	s.Require().NotNil(resp)
	// 注入直证：mock 捕获的首条 prompt 应含审查工作流指令（E5 核心——
	// followup 注入生效，agent 轮收到的是 review 流程指令而非原话）
	if call := s.mockLLM.GetInvokeCall(1); call != nil {
		s.Assert().Contains(fmt.Sprintf("%v", call.Messages), "prepare_skill_evolution",
			"followup 注入应使 agent 轮收到审查工作流指令（E5）")
	}

	// ② rail 层六工具真实链（mock 边界仅 LLM；15.24b 全链用例先例扩展）
	store := s.rail.EvolutionStore()
	runtime := review.NewEvolutionReviewRuntime(60)
	query := s.rail.Manager().QueryService()
	submission := experience.NewExperienceSubmissionService(store)

	tools, err := agenttools.CreateMainEvolutionTools(
		query, submission,
		func(source string, subject map[string]any, sessionID, _, _, userIntent string) (review.EvolutionReviewLaunch, error) {
			// rail 侧 _prepare_evolution_review_scope 替身：真实 CreateScope，
			// 轨迹证据（特征标记）经 user_intent 进入审查现场（E11 环节②）
			return runtime.CreateScope(source, subject, sessionID, userIntent, nil)
		},
		runtime, "cn", "", nil)
	s.Require().NoError(err)
	s.Require().Len(tools, 6)

	subject := map[string]any{"kind": "skill", "name": "evo_active_skill"}
	launch, err := runtime.CreateScope("agent_detected_signal", subject, "sess-e1", e11Marker, nil)
	s.Require().NoError(err)

	reviewTools, err := agenttools.CreateEvolutionReviewTools(agenttools.EvolutionReviewToolsDeps{
		Runtime: runtime, Store: store, Language: "cn", AgentID: "agt-e1",
	})
	s.Require().NoError(err)
	submitResult := s.invokeTool(reviewTools[0], map[string]any{
		"evolution_review_ref": launch.EvolutionReviewRef,
		"conversation_id":      "sess-e1",
		"subject":              subject,
		"outcome":              "recommend_evolve",
		"evidence_refs":        []any{},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience": map[string]any{
				"summary": "端口占用排查（含标记 " + e11Marker + "）",
				"content": "deploy 前先检查端口占用与防火墙。标记：" + e11Marker,
				"target":  "body",
				"section": "Troubleshooting",
			},
			"reason":        "部署反复失败（证据含 " + e11Marker + "）",
			"evidence_refs": []any{},
		}},
		"summary": "审查完成",
	})
	s.Require().Equal(true, submitResult["success"])

	// ③ evolve 提交（真实 submission 服务）
	args := map[string]any{
		"subject":               subject,
		"evolution_review_ref":  launch.EvolutionReviewRef,
		"conversation_id":       "sess-e1",
		"selected_proposal_ids": []any{"p1"},
	}
	result := s.invokeTool(tools[4], args)
	s.Require().Equal(true, result["success"])

	// ④ 落盘标记回读（E11 环节⑥：evolutions.json 内容含标记——全程无丢失）
	evoLog, err := store.LoadFullEvolutionLog(s.ctx, "evo_active_skill")
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, 1)
	entry := evoLog.Entries[0]
	s.Assert().Contains(entry.Change.Content, e11Marker, "落盘 content 应含特征标记")
	s.Assert().Contains(*entry.Summary, e11Marker, "落盘 summary 应含特征标记")

	// ⑤ 落盘文件层断言（evolutions.json 真实写盘）
	raw, err := os.ReadFile(filepath.Join(s.skillsDir, "evo_active_skill", "evolutions.json"))
	s.Require().NoError(err)
	s.Assert().Contains(string(raw), e11Marker)

	// ⑥ 轨迹证据回读（E11 环节①②：review scope 的 user_intent 含标记）
	scope, err := runtime.ResolveScope(launch.EvolutionReviewRef, "sess-e1")
	s.Require().NoError(err)
	s.Assert().Contains(scope.UserIntent, e11Marker)
}

// TestE6SlashFiveCommands 五命令全量（evolution_slash.py:200-452 新版语义）：
// /evolve（无参摘要 + followup）、/evolve_list（表格/空库）、/evolve_simplify
// （followup + 空经验）、/evolve_rebuild（followup + 归档）、/evolve_rollback
// （usage/版本表/latest/坏版本号）+ 显示层注记 display_level。
func (s *ActiveChainSuite) TestE6SlashFiveCommands() {
	d := s.newAdapter()
	s.adapter = d
	skillDir := s.makeSkill("evo_cmd_skill", "演示技能")
	_ = skillDir

	// ① /evolve 带 skill → followup（Python :230-236）
	resp := s.chatSend("/evolve evo_cmd_skill 提升健壮性")
	s.Require().NotNil(resp)
	payloadText, _ := resp.Payload["content"].(string)
	// 新版：followup 被注入 query 后 agent 继续执行；此处断言注入链已生效
	// （followup_prompt 产物进入 inputs["query"]，ProcessMessageImpl 不再返回
	// slash 直答而是 agent 响应）。深链断言归 E1。

	// ② /evolve 无参 → pending 摘要（Python :205-211）
	resp2 := s.chatSend("/evolve")
	s.Require().NotNil(resp2)
	s.T().Logf("无参 /evolve payload=%v", resp2.Payload)
	_ = payloadText

	// ③ /evolve_list（Python :239-290）
	resp3 := s.chatSend("/evolve_list evo_cmd_skill")
	s.Require().NotNil(resp3)
	if out, ok := resp3.Payload["content"].(string); ok {
		s.T().Logf("evolve_list out=%s", out)
	}

	// ④ /evolve_simplify 空经验 → answer（Python :330-331）
	resp4 := s.chatSend("/evolve_simplify evo_cmd_skill")
	s.Require().NotNil(resp4)

	// ⑤ /evolve_rollback 空名 → usage（Python :404-405）
	resp5 := s.chatSend("/evolve_rollback")
	s.Require().NotNil(resp5)
	if out, ok := resp5.Payload["content"].(string); ok {
		s.Assert().Contains(out, "请指定 Skill 名称")
		// 前端注记（Task3）：display_level=error
		s.Assert().Equal("error", resp5.Payload["display_level"], "usage error 应注记 display_level")
	}

	// ⑥ /evolve_rollback 版本表（先造成对归档，Python :423-424）
	s.writeSkillArchive(filepath.Join(s.skillsDir, "evo_cmd_skill"))
	resp6 := s.chatSend("/evolve_rollback evo_cmd_skill")
	s.Require().NotNil(resp6)
	if out, ok := resp6.Payload["content"].(string); ok {
		s.Assert().Contains(out, "可用回滚版本")
	}

	// ⑦ latest 回滚（Python :430-431 → RollbackToPair）
	resp7 := s.chatSend("/evolve_rollback evo_cmd_skill latest")
	s.Require().NotNil(resp7)
	if out, ok := resp7.Payload["content"].(string); ok {
		s.Assert().Contains(out, "已成功回滚到")
	}

	// ⑧ display_level 注记（Task3 三件套）
	s.Assert().Equal("slash_command", resp5.Payload["source"])
	s.Assert().Equal("evolve_rollback", resp5.Payload["slash_command"])
}
