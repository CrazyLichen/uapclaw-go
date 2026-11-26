package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTeamSkillCreateRail_ExternalProposal 对齐 propose_from_external_evidence
// （team_skill_create_rail.py:176-238）+ owns/resolve（:244-260）。
func TestTeamSkillCreateRail_ExternalProposal(t *testing.T) {
	r := NewTeamSkillCreateRail(t.TempDir())

	// auto_trigger=false → 忽略
	r.autoTrigger = false
	assert.False(t, r.ProposeFromExternalEvidence("k1", "guidance", []string{"a", "b"}, "reason"))
	r.autoTrigger = true

	// 证据不足 2 条 → 拒绝
	assert.False(t, r.ProposeFromExternalEvidence("k1", "guidance", []string{"a"}, "reason"))
	// guidance 缺失 → 拒绝
	assert.False(t, r.ProposeFromExternalEvidence("k1", "", []string{"a", "b"}, "reason"))
	// 合法登记 → true
	assert.True(t, r.ProposeFromExternalEvidence("K1", "guidance", []string{"a", "b", "a", "c"}, "reason"))
	// 归一化键查重（K1→k1）→ false
	assert.False(t, r.ProposeFromExternalEvidence("k1", "guidance", []string{"x", "y"}, ""))

	// owns 生命周期
	assert.False(t, r.OwnsExternalProposal("nope"))
	// request_id 归属未知（proposal 登记在 map，仅键去重）
	// 找出真实 request_id：pending map 只有一项
	var requestID string
	for id := range r.pendingExternalProposals {
		requestID = id
	}
	require.NotEmpty(t, requestID)
	assert.True(t, r.OwnsExternalProposal(requestID))

	// resolve(accepted=false) → 空 + pop
	assert.Empty(t, r.ResolveExternalProposal(requestID, false))
	assert.False(t, r.OwnsExternalProposal(requestID))

	// 重新登记后 resolve(true) → 批准提示词
	require.True(t, r.ProposeFromExternalEvidence("k2", "guidance-2", []string{"a", "b"}, "reason-2"))
	for id := range r.pendingExternalProposals {
		requestID = id
	}
	prompt := r.ResolveExternalProposal(requestID, true)
	assert.Contains(t, prompt, "swarmskill-creator")
	assert.Contains(t, prompt, "guidance-2")
	assert.Contains(t, prompt, "- a")
	assert.False(t, r.OwnsExternalProposal(requestID))
}

// TestTeamSkillCreateRail_ExternalProposal_英文分支 对齐 en 审批卡与批准提示词。
func TestTeamSkillCreateRail_ExternalProposal_英文分支(t *testing.T) {
	r := NewTeamSkillCreateRail(t.TempDir(), WithTeamSkillCreateLanguage("en"))
	require.True(t, r.ProposeFromExternalEvidence("k-en", "workflow", []string{"e1", "e2"}, "reason"))
	for id := range r.pendingExternalProposals {
		prompt := r.ResolveExternalProposal(id, true)
		assert.Contains(t, prompt, "The user accepted the new-Skill approval card")
	}
}

// TestTeamSkillCreateRail_外部审批卡载荷 对齐 _build_external_approval_event
// （team_skill_create_rail.py:279-340）：类型/来源/schema/evolution_meta/questions。
func TestTeamSkillCreateRail_外部审批卡载荷(t *testing.T) {
	r := NewTeamSkillCreateRail(t.TempDir())
	r.EmitHostEvent(nil) // EmitHostEvent(nil) 防御（基座应容忍）→ 跳过断言
	require.True(t, r.ProposeFromExternalEvidence("k-ev", "workflow", []string{"e1", "e2"}, "reason"))
	var reqID string
	for id := range r.pendingExternalProposals {
		reqID = id
	}
	proposal := r.pendingExternalProposals[reqID]
	event := r.buildExternalApprovalEvent(proposal)
	assert.Equal(t, "chat.ask_user_question", event.Type)
	payload, ok := event.Payload.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, reqID, payload["request_id"])
	assert.Equal(t, skillCreationApprovalSource, payload["source"])
	assert.Equal(t, skillCreationApprovalSchema, payload["approval_schema"])
	meta := payload["_evolution_meta"].(map[string]string)
	metaAny := map[string]any{}
	for k, v := range meta {
		metaAny[k] = v
	}
	meta = metaAny
	assert.Equal(t, "create", meta["approval_kind"])
	assert.Equal(t, "scheduler_review_feedback", meta["source"])
	questions := payload["questions"].([]map[string]any)
	require.Len(t, questions, 1)
	assert.Equal(t, "新建 Skill 审批", questions[0]["header"])
	assert.False(t, questions[0]["multi_select"].(bool))
}
