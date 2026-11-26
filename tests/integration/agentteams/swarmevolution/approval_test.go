//go:build integration

// E7 审批拒绝 / E10 审批回传 rail（部分批准，15.24c 装配闭环语义）。
//
// 链路：rail.RequestUserEvolution（mock LLM 生成 3 条 records，autoApprove=false）
// → pending 快照 + 审批事件 → host 层索引映射（Task4 产物）→
// ApproveRecord variadic 部分批准 / RejectRecord → 落盘/不落盘断言。
// Python 锚点：skill_evolution_rail.py approve_record（:1447-1460）+
// evolution_helpers.py record_ids_from_pending_approval（:454-475）。
package swarmevolution

import (
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
)

// e10Marker 落盘产物特征标记（三条记录各带独立标记，便于逐条断言）。
const e10Marker1 = "E2E-MARK-RECORD-1"
const e10Marker2 = "E2E-MARK-RECORD-2"
const e10Marker3 = "E2E-MARK-RECORD-3"

// e10GenerateResp 三条 append 草稿（Python GenerateRecords 提示词的 mock 响应）。
const e10GenerateResp = `[
	{"action": "append", "section": "Troubleshooting", "target": "body", "content": "` + e10Marker1 + ` 端口占用排查"},
	{"action": "append", "section": "Troubleshooting", "target": "body", "content": "` + e10Marker2 + ` 防火墙规则检查"},
	{"action": "append", "section": "Examples", "target": "body", "content": "` + e10Marker3 + ` 部署脚本示例"}
]`

// stagePendingForApproval 用 mock LLM 驱动 rail 生成三条 pending 记录并返回审批事件。
// 真实链：rail generate stage → manager stagePendingChange → pendingApprovalSnapshots。
func (s *EvolutionE2ESuite) stagePendingForApproval(skillName string) (requestID string, approvalEvent map[string]any) {
	s.mockScript(e10GenerateResp)
	result, err := s.rail.RequestUserEvolution(s.ctx, skillName, "部署排障经验（"+e10Marker1+"）", false)
	s.Require().NoError(err)
	s.Require().NotNil(result)
	s.Require().NotNil(result.RequestID)
	s.Require().NotNil(result.ApprovalEvent)
	// typed → map（evolutionlogic.EventPayloadDict 兼容 typed/dict 双形状）
	eventMap := evolutionlogic.EventPayloadDict(result.ApprovalEvent.ToMap())
	return *result.RequestID, eventMap
}

// TestE10审批部分批准回传Rail 配置组装 + 审批回传 rail 同链：host 索引映射 →
// approve_record(request_id, approved_record_ids) 部分批准（3 批 2）→ 同 rail 落盘。
func (s *EvolutionE2ESuite) TestE10审批部分批准回传Rail() {
	d := s.newAdapter()
	s.adapter = d
	s.makeSkill("evo_approve_skill", "审批回传演示技能")

	requestID, approvalEvent := s.stagePendingForApproval("evo_approve_skill")

	// ① 审批事件契约：event_type + request_id（前端不感知 record_id）
	s.Require().Equal("chat.ask_user_question", evolutionlogic.EventType(approvalEvent))

	// ② host 侧索引映射（Task4：evolution_helpers.py:419-451）
	recordIDs := s.rail.PendingChangeRecordIDs(requestID)
	s.Require().NotNil(recordIDs)
	s.Require().GreaterOrEqual(len(recordIDs), 2)

	// ③ 部分批准：批前 N-1 条（末条拒绝）——索引映射语义与条数无关
	selected := make([][]string, len(recordIDs))
	for i := range selected {
		if i == len(recordIDs)-1 {
			selected[i] = []string{"拒绝"}
		} else {
			selected[i] = []string{"接受"}
		}
	}
	accepted, approvedIDs, hasMapping := approvedIDsForTest(selected, recordIDs)
	s.Require().True(accepted)
	s.Require().True(hasMapping)
	s.Require().Len(approvedIDs, len(recordIDs)-1)

	// ④ 同 rail 实例消费：ApproveRecord variadic 部分批准
	//（Python approve_record :1458-1460 过滤段）
	err := s.rail.ApproveRecord(s.ctx, requestID, approvedIDs...)
	s.Require().NoError(err)

	// ⑤ 落盘归属断言：仅批准的条落盘，被拒条不落盘
	evoLog, err := s.rail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_approve_skill")
	s.Require().NoError(err)
	s.Require().Len(evoLog.Entries, len(recordIDs)-1, "部分批准应只落批准条目")
	approvedSet := map[string]bool{}
	for _, id := range approvedIDs {
		approvedSet[id] = true
	}
	for _, entry := range evoLog.Entries {
		s.Assert().True(approvedSet[entry.ID], "落盘条目 %s 必须在批准集内", entry.ID)
	}
	// 特征标记回读：落盘内容含已批准条目的标记（数量语义随生成阶段定，
	// 归属正确性由上面的 approvedSet 断言保证）
	for _, entry := range evoLog.Entries {
		s.Assert().Contains(entry.Change.Content, "E2E-MARK-RECORD")
	}
}

// TestE7审批拒绝不落盘 reject_record 链：拒绝 → 不落盘 + 无残留。
func (s *EvolutionE2ESuite) TestE7审批拒绝不落盘() {
	d := s.newAdapter()
	s.adapter = d
	s.makeSkill("evo_reject_skill", "拒绝链演示技能")

	requestID, approvalEvent := s.stagePendingForApproval("evo_reject_skill")
	s.Require().Equal("chat.ask_user_question", evolutionlogic.EventType(approvalEvent))

	err := s.rail.RejectRecord(s.ctx, requestID)
	s.Require().NoError(err)

	evoLog, err := s.rail.EvolutionStore().LoadFullEvolutionLog(s.ctx, "evo_reject_skill")
	s.Require().NoError(err)
	s.Assert().Empty(evoLog.Entries, "拒绝后不得落盘")
}


