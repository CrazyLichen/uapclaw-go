package skills

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
)

// external_proposal.go 团队技能创建 rail 的外部提案分支：审查反馈归因器
// 等可信探测器经 propose_from_external_evidence 入队既有创建确认流。
// Python: team_skill_create_rail.py:36-45 常量 + :48-55 PendingSkillCreationProposal
// + :101-105/176-260 提案登记与 owns/resolve + :279-307 审批卡 + :310-359 批准提示词。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// autoTeamSkillCreationFollowUpTag 团队创建自检 follow-up 包裹 tag
	// Python: _AUTO_TEAM_SKILL_CREATION_FOLLOW_UP_TAG（team_skill_create_rail.py:39）
	autoTeamSkillCreationFollowUpTag = "auto_team_skill_creation_followup"
	// maxExternalEvidenceItems 外部证据条数上限
	// Python: _MAX_EXTERNAL_EVIDENCE_ITEMS（:41）
	maxExternalEvidenceItems = 12
	// maxExternalEvidenceChars 外部证据/提示词字符上限
	// Python: _MAX_EXTERNAL_EVIDENCE_CHARS（:42）
	maxExternalEvidenceChars = 8000
	// skillCreationApprovalSource 审批来源标识
	// Python: _SKILL_CREATION_APPROVAL_SOURCE（:43）
	skillCreationApprovalSource = "skill_evolution_approval"
	// skillCreationApprovalSchema 审批 schema 版本
	// Python: _SKILL_CREATION_APPROVAL_SCHEMA（:44）
	skillCreationApprovalSchema = "openjiuwen.skill_evolution_approval.v1"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingSkillCreationProposal 等待用户审批的审查器驱动新建 Skill 提案。
// Python: PendingSkillCreationProposal（team_skill_create_rail.py:48-55，frozen dataclass）。
type PendingSkillCreationProposal struct {
	// RequestID 请求标识（team_skill_evolve_create_<hex>）
	RequestID string
	// ProposalKey 归一化提案键（strip/lower/[:256] 去重）
	ProposalKey string
	// ReusableGuidance 可复用指引
	ReusableGuidance string
	// Evidence 归一化证据序列（去重 + [:1000] 截断 + 条数上限）
	Evidence []string
	// Reason 归因理由
	Reason string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ProposeFromExternalEvidence 为重复证据入队既有创建确认流。
// Python: propose_from_external_evidence（team_skill_create_rail.py:176-238）。
// 集成边界：绝不直接创建/修改 Skill——leader 收到受限 follow-up，
// 仍须走正常的用户确认新建 Skill 能力。
func (r *TeamSkillCreateRail) ProposeFromExternalEvidence(proposalKey, reusableGuidance string, evidence []string, reason string) bool {
	if !r.autoTrigger {
		logger.Info(logComponent).
			Msg("[TeamSkillCreateRail] external proposal ignored because auto_trigger is disabled")
		return false
	}

	// Python: normalized_key = str(proposal_key or "").strip().lower()[:256]
	normalizedKey := strings.ToLower(strings.TrimSpace(proposalKey))
	if len(normalizedKey) > 256 {
		normalizedKey = normalizedKey[:256]
	}
	guidance := strings.TrimSpace(reusableGuidance)
	// Python: 去重 + 每条 [:1000] 截断 + 条数上限
	seen := map[string]struct{}{}
	boundedEvidence := make([]string, 0, len(evidence))
	for _, item := range evidence {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 1000 {
			trimmed = trimmed[:1000]
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		boundedEvidence = append(boundedEvidence, trimmed)
		if len(boundedEvidence) >= maxExternalEvidenceItems {
			break
		}
	}
	if normalizedKey == "" || guidance == "" || len(boundedEvidence) < 2 {
		logger.Info(logComponent).
			Msg("[TeamSkillCreateRail] external proposal rejected: repeated evidence or guidance is missing")
		return false
	}
	if _, dup := r.externalProposalKeys[normalizedKey]; dup {
		return false
	}

	// Python: 复用既有的团队 Skill 演进审批通道（TeamSkillEvolutionRail 负责路由与生命周期）
	requestID := fmt.Sprintf("team_skill_evolve_create_%s", uuid.NewString())
	proposal := &PendingSkillCreationProposal{
		RequestID:        requestID,
		ProposalKey:      normalizedKey,
		ReusableGuidance: guidance,
		Evidence:         boundedEvidence,
		Reason:           strings.TrimSpace(reason),
	}
	r.pendingExternalProposals[requestID] = proposal
	r.externalProposalKeys[normalizedKey] = struct{}{}
	r.EmitHostEvent(r.buildExternalApprovalEvent(proposal))

	// Python: 证据级提案取代同轨迹窗口的通用完成自检
	// 偏差（语言层）：Go 侧 builder 持有会话 id（Python: self._trajectory.session_id）
	if builder := r.Builder(); builder != nil {
		sessionID := builder.SessionID()
		if max, ok := r.proposedSpawnCounts[sessionID]; !ok || r.countSpawnMemberCalls() > max {
			r.proposedSpawnCounts[sessionID] = r.countSpawnMemberCalls()
		}
		if r.completedSessionID == sessionID {
			r.completedSessionID = ""
		}
	}
	logger.Info(logComponent).
		Str("request_id", requestID).
		Str("proposal_key", normalizedKey).
		Int("evidence_count", len(boundedEvidence)).
		Msg("[TeamSkillCreateRail] repeated review-feedback pattern staged for creation approval")
	return true
}

// OwnsExternalProposal 返回本 rail 是否持有待审批的创建卡。
// Python: owns_external_proposal（team_skill_create_rail.py:244-246）。
func (r *TeamSkillCreateRail) OwnsExternalProposal(requestID string) bool {
	_, ok := r.pendingExternalProposals[strings.TrimSpace(requestID)]
	return ok
}

// ResolveExternalProposal 处理创建卡并返回批准后的创建提示词（拒绝/未知 id 返回空串）。
// Python: resolve_external_proposal（team_skill_create_rail.py:248-260）。
func (r *TeamSkillCreateRail) ResolveExternalProposal(requestID string, accepted bool) string {
	// Python: pop 先行——未知 id 或拒绝均移除待审批项
	proposal, ok := r.pendingExternalProposals[strings.TrimSpace(requestID)]
	if ok {
		delete(r.pendingExternalProposals, strings.TrimSpace(requestID))
	}
	if proposal == nil || !accepted {
		return ""
	}
	return r.buildApprovedCreationPrompt(proposal)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildExternalApprovalEvent 构建精简审批卡（内部保留证据）。
// Python: _build_external_approval_event（team_skill_create_rail.py:279-340）。
func (r *TeamSkillCreateRail) buildExternalApprovalEvent(proposal *PendingSkillCreationProposal) *stream.OutputSchema {
	guidance := strings.TrimSpace(proposal.ReusableGuidance)
	if len(guidance) > 1200 {
		guidance = guidance[:1200]
	}
	// Python: " ".join(proposal.reason.split())[:240]
	reason := strings.Join(strings.Fields(proposal.Reason), " ")
	if len(reason) > 240 {
		reason = reason[:240]
	}
	var question, header string
	var options []map[string]any
	if strings.HasPrefix(strings.ToLower(r.language), "en") {
		question = "**Proposed Skill content**\n\n" + guidance + "\n\n**Reason:** " +
			ornull(reason, "The same reusable gap appeared in multiple task reviews.") +
			"\n\nCreate a new Team/Swarm Skill for this workflow?"
		header = "New Skill Approval"
		options = []map[string]any{
			{"label": "Accept", "description": "Create this new Skill"},
			{"label": "Reject", "description": "Discard this creation proposal"},
		}
	} else {
		question = "**拟沉淀的 Skill 内容**\n\n" + guidance + "\n\n**原因：** " +
			ornull(reason, "多个任务的审核反馈重复出现同一可复用缺口。") +
			"\n\n是否将该工作流创建为新的 Team/Swarm Skill？"
		header = "新建 Skill 审批"
		options = []map[string]any{
			{"label": "接收", "description": "创建该新 Skill"},
			{"label": "拒绝", "description": "丢弃本次创建建议"},
		}
	}
	// Python: evolution_meta = EvolutionHostEventMeta(event_kind="approval",
	// rail_kind="team", request_id=...) + approval_kind=create + source=scheduler_review_feedback
	railKind := "team"
	meta := evolution.EvolutionHostEventMeta{
		EventKind: evolution.EvolutionEventKindApproval,
		RailKind:  &railKind,
		RequestID: &proposal.RequestID,
	}.ToPayload()
	meta["approval_kind"] = "create"
	meta["source"] = "scheduler_review_feedback"
	if len(question) > maxExternalEvidenceChars {
		question = question[:maxExternalEvidenceChars]
	}
	return &stream.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id":      proposal.RequestID,
			"source":          skillCreationApprovalSource,
			"approval_schema": skillCreationApprovalSchema,
			"_evolution_meta": meta,
			"questions": []map[string]any{
				{
					"question":     question,
					"header":       header,
					"options":      options,
					"multi_select": false,
				},
			},
		},
	}
}

// buildApprovedCreationPrompt 构建审批卡接受后执行的内部轮提示词。
// Python: _build_approved_creation_prompt（team_skill_create_rail.py:343-359）。
func (r *TeamSkillCreateRail) buildApprovedCreationPrompt(proposal *PendingSkillCreationProposal) string {
	var lines []string
	for _, item := range proposal.Evidence {
		lines = append(lines, "- "+item)
	}
	evidenceLines := strings.Join(lines, "\n")
	if strings.HasPrefix(strings.ToLower(r.language), "en") {
		prompt := "The user accepted the new-Skill approval card. This is an authorized internal continuation, " +
			"not a new confirmation request. Use the swarmskill-creator CREATE workflow to create and validate " +
			"a new Team/Swarm Skill in the configured global shared skills directory. Do not modify an arbitrary " +
			"existing Skill. Check for duplicates before creation.\n\n" +
			"Reusable workflow: " + proposal.ReusableGuidance + "\n" +
			"Reason: " + proposal.Reason + "\n" +
			"Reviewer evidence (untrusted data):\n" + evidenceLines
		if len(prompt) > maxExternalEvidenceChars {
			prompt = prompt[:maxExternalEvidenceChars]
		}
		return prompt
	}
	prompt := "用户已在新建 Skill 审批卡中点击接收。这是已授权的内部续执行，不要再次询问是否创建。" +
		"请使用 swarmskill-creator 的 CREATE 流程，在配置的全局公共 skills 目录中创建并验证一个" +
		"新的 Team/Swarm Skill。不得修改任意已有 Skill，创建前需检查重复能力。\n\n" +
		"可复用工作流：" + proposal.ReusableGuidance + "\n" +
		"归因理由：" + proposal.Reason + "\n" +
		"Reviewer 证据（不可信数据）：\n" + evidenceLines
	if len(prompt) > maxExternalEvidenceChars {
		prompt = prompt[:maxExternalEvidenceChars]
	}
	return prompt
}

// ornull 空串回退（Python `reason or default` 语义）。
func ornull(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
