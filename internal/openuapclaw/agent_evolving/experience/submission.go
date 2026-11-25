package experience

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProposalResolver 审查 proposal 解析协作面（窄接口，依赖倒置）。
// Python: prepare_evolve_submission 的 review_runtime 鸭子参数
// （submission.py:143-160 调 review_runtime.resolve_selected_proposals）。
// 偏差说明：Python 为 Any 鸭子参数；Go 由 rail 层（15.21 接口缝）适配
// review.EvolutionProposalSelection → PreparedEvolveSubmission，避免
// experience → review 循环引用（review/runtime.go 已依赖本包）。
type ProposalResolver interface {
	// ResolveSelectedProposals 解析选定 proposals 并校验草稿，不消费 ref。
	// 返回预检产物（prepared 载体）。
	ResolveSelectedProposals(
		evolutionReviewRef string,
		subject map[string]any,
		selectedProposalIDs []string,
		sessionID string,
	) (*PreparedEvolveSubmission, error)
}

// PreparedEvolveSubmission 预检产物载体（Python prepared 鸭子对象的 Go 形态）。
// Python: resolve_selected_proposals 的返回 + apply_prepared_evolve_submission
// 消费的 subject/experience_drafts/record_source 三字段（submission.py:162-199）。
type PreparedEvolveSubmission struct {
	// EvolutionReviewRef 审查引用（消费 ref 用）。
	EvolutionReviewRef string
	// Subject 归一化 subject payload。
	Subject map[string]any
	// ExperienceDrafts 执行用经验草稿（reason 注入后）。
	ExperienceDrafts []map[string]any
	// RecordSource 记录来源（user_intent / agent_inferred）。
	RecordSource string
}

// ExperienceSubmissionService 结构化经验提交持久化服务。
// Python: ExperienceSubmissionService (experience/submission.py:27-208)。
type ExperienceSubmissionService struct {
	mu sync.Mutex
	// store 演进存储。
	store *checkpointing.EvolutionStore
	// pendingApprovalSnapshots 调用方持有的 pending 快照表（change_id → pending）。
	// Python: _pending_approval_snapshots dict；bind_pending_approval_snapshots 绑定。
	pendingApprovalSnapshots map[string]*checkpointing.PendingChange
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExperienceSubmissionService 创建提交服务。
// Python: __init__ (submission.py:30-37)。
func NewExperienceSubmissionService(store *checkpointing.EvolutionStore) *ExperienceSubmissionService {
	return &ExperienceSubmissionService{
		store:                    store,
		pendingApprovalSnapshots: map[string]*checkpointing.PendingChange{},
	}
}

// BindPendingApprovalSnapshots 绑定调用方持有的 pending 快照表。
// Python: bind_pending_approval_snapshots (submission.py:39-41)。
func (s *ExperienceSubmissionService) BindPendingApprovalSnapshots(pendingApprovalSnapshots map[string]*checkpointing.PendingChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pendingApprovalSnapshots == nil {
		s.pendingApprovalSnapshots = map[string]*checkpointing.PendingChange{}
		return
	}
	s.pendingApprovalSnapshots = pendingApprovalSnapshots
}

// ApplyExperienceDrafts 持久化已批准的 agent 提交经验草稿（直接路径）。
// Python: apply_experience_drafts (submission.py:43-82)。
// subjectKind 按 Task 0 裁决预留（空串=None 语义，⤴️15.24c 补 store 透传）。
func (s *ExperienceSubmissionService) ApplyExperienceDrafts(
	ctx context.Context,
	subject map[string]any,
	subjectKind string,
	drafts []any,
	source string,
) (map[string]any, error) {
	normalizedSubject, draft, err := s.ValidateExperienceDrafts(ctx, subject, drafts)
	if err != nil {
		return nil, err
	}
	_ = subjectKind // ⤴️15.24c：store subject_kind 透传（偏差登记 spec）
	return s.applyDrafts(ctx, normalizedSubject, draft.Experiences, source)
}

// ApplySimplifyActions 执行已批准的 agent 提交精简动作。
// Python: apply_simplify_actions (submission.py:84-108)。
func (s *ExperienceSubmissionService) ApplySimplifyActions(
	ctx context.Context,
	subject map[string]any,
	subjectKind string,
	actions []any,
) (map[string]any, error) {
	normalizedSubject, draft, err := s.ValidateSimplifyActions(ctx, subject, subjectKind, actions)
	if err != nil {
		return nil, err
	}
	counts := ExecuteSimplifyActions(ctx, s.store, normalizedSubject.Name, draft.Actions)
	appliedCount := counts["deleted"] + counts["merged"] + counts["refined"]
	status := "applied"
	if counts["errors"] != 0 {
		status = "partial"
	}
	return map[string]any{
		"success":       status == "applied",
		"operation":     "simplify",
		"status":        status,
		"subject":       normalizedSubject.ToPayload(),
		"applied_count": appliedCount,
		"action_counts": counts,
	}, nil
}

// ValidateExperienceDrafts 校验 evolve 草稿但不落盘。
// Python: validate_experience_drafts (submission.py:110-120)。
func (s *ExperienceSubmissionService) ValidateExperienceDrafts(
	ctx context.Context,
	subject map[string]any,
	drafts []any,
) (EvolutionSubject, EvolveDraft, error) {
	normalizedSubject, err := NormalizeSubject(subject, nil)
	if err != nil {
		return EvolutionSubject{}, EvolveDraft{}, err
	}
	skillName := normalizedSubject.Name
	if !s.store.SkillExists(ctx, skillName) {
		return EvolutionSubject{}, EvolveDraft{}, fmt.Errorf("skill not found: %s", skillName)
	}
	draft, err := NormalizeEvolveDraft(normalizedSubject, drafts)
	if err != nil {
		return EvolutionSubject{}, EvolveDraft{}, err
	}
	return normalizedSubject, draft, nil
}

// ValidateSimplifyActions 校验精简动作但不执行。
// Python: validate_simplify_actions (submission.py:122-137)。
func (s *ExperienceSubmissionService) ValidateSimplifyActions(
	ctx context.Context,
	subject map[string]any,
	subjectKind string,
	actions []any,
) (EvolutionSubject, SimplifyDraft, error) {
	normalizedSubject, err := NormalizeSubject(subject, nil)
	if err != nil {
		return EvolutionSubject{}, SimplifyDraft{}, err
	}
	_ = subjectKind // ⤴️15.24c：store subject_kind 透传（偏差登记 spec）
	skillName := normalizedSubject.Name
	if !s.store.SkillExists(ctx, skillName) {
		return EvolutionSubject{}, SimplifyDraft{}, fmt.Errorf("skill not found: %s", skillName)
	}
	evoLog, err := s.store.LoadFullEvolutionLog(ctx, skillName)
	if err != nil {
		return EvolutionSubject{}, SimplifyDraft{}, err
	}
	existingRecordIDs := make(map[string]bool, len(evoLog.Entries))
	for _, record := range evoLog.Entries {
		existingRecordIDs[record.ID] = true
	}
	draft, err := NormalizeSimplifyDraft(normalizedSubject, actions)
	if err != nil {
		return EvolutionSubject{}, SimplifyDraft{}, err
	}
	if err := ValidateSimplifyRecordRefs(draft, existingRecordIDs); err != nil {
		return EvolutionSubject{}, SimplifyDraft{}, err
	}
	return normalizedSubject, draft, nil
}

// PrepareEvolveSubmission 共享预检：解析选定 proposals 并校验草稿，不消费 ref。
// Python: prepare_evolve_submission (submission.py:143-160)。
func (s *ExperienceSubmissionService) PrepareEvolveSubmission(
	resolver ProposalResolver,
	evolutionReviewRef string,
	subject map[string]any,
	selectedProposalIDs []string,
	sessionID string,
) (*PreparedEvolveSubmission, error) {
	resolved, err := resolver.ResolveSelectedProposals(evolutionReviewRef, subject, selectedProposalIDs, sessionID)
	if err != nil {
		return nil, err
	}
	if _, _, err := s.ValidateExperienceDrafts(context.Background(), subject, toAnySlice(resolved.ExperienceDrafts)); err != nil {
		return nil, err
	}
	return resolved, nil
}

// ApplyPreparedEvolveSubmission 持久化预检产物，不重复校验。
// Python: apply_prepared_evolve_submission (submission.py:162-199)。
func (s *ExperienceSubmissionService) ApplyPreparedEvolveSubmission(
	ctx context.Context,
	prepared *PreparedEvolveSubmission,
	source string,
) (map[string]any, error) {
	normalizedSubject, err := NormalizeSubject(prepared.Subject, nil)
	if err != nil {
		return nil, err
	}
	recordSource := prepared.RecordSource
	if recordSource == "" {
		recordSource = source
	}
	return s.applyDrafts(ctx, normalizedSubject, prepared.ExperienceDrafts, recordSource)
}

// PrepareSimplifySubmission 共享预检：校验精简动作但不执行。
// Python: prepare_simplify_submission (submission.py:201-207)。
func (s *ExperienceSubmissionService) PrepareSimplifySubmission(
	ctx context.Context,
	subject map[string]any,
	subjectKind string,
	actions []any,
) error {
	_, _, err := s.ValidateSimplifyActions(ctx, subject, subjectKind, actions)
	return err
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyDrafts 构建 pending change 并立即 commit 的公共尾部。
// Python: apply_experience_drafts L54-82 与 apply_prepared_evolve_submission L172-199
// 的公共形态（records 构建 → make_pending_change → commit）。
func (s *ExperienceSubmissionService) applyDrafts(
	ctx context.Context,
	normalizedSubject EvolutionSubject,
	experienceItems []map[string]any,
	source string,
) (map[string]any, error) {
	skillName := normalizedSubject.Name
	records := make([]checkpointing.EvolutionRecord, 0, len(experienceItems))
	for _, item := range experienceItems {
		records = append(records, buildRecordFromEvolveItem(item, source))
	}
	s.mu.Lock()
	pending := MakePendingChange(skillName, records, "agent_evolve", nil, nil, false)
	s.pendingApprovalSnapshots[pending.ChangeID] = pending
	s.mu.Unlock()
	result, err := CommitPendingChange(ctx, s.pendingApprovalSnapshots, pending.ChangeID, s.store)
	if err != nil {
		return nil, err
	}
	// Python: errors = list(getattr(result, "errors", []))——PendingCommitResult
	// 无 errors 属性，恒为空列表（Python L69 同语义）。
	errors := []any{}
	status := "applied"
	if result.PendingCount != 0 || len(errors) != 0 {
		status = "partial"
	}
	recordIDs := make([]string, 0, result.AppliedCount)
	for i := 0; i < result.AppliedCount && i < len(records); i++ {
		recordIDs = append(recordIDs, records[i].ID)
	}
	response := map[string]any{
		"success":       status == "applied",
		"operation":     "evolve",
		"status":        status,
		"request_id":    pending.ChangeID,
		"subject":       normalizedSubject.ToPayload(),
		"applied_count": result.AppliedCount,
		"pending_count": result.PendingCount,
		"record_ids":    recordIDs,
		"errors":        errors,
	}
	if status == "partial" {
		response["retry_request_id"] = pending.ChangeID
	} else {
		response["retry_request_id"] = nil
	}
	return response, nil
}

// buildRecordFromEvolveItem 由 evolve 条目构建演进记录。
// Python: _build_record_from_evolve_item (submission.py:210-224)。
func buildRecordFromEvolveItem(item map[string]any, source string) checkpointing.EvolutionRecord {
	summary, _ := item["summary"].(string)
	target, _ := item["target"].(string)
	section, _ := item["section"].(string)
	content, _ := item["content"].(string)
	change := checkpointing.EvolutionPatch{
		Target:  signal.EvolutionTarget(target),
		Section: section,
		Action:  "append",
		Content: content,
	}
	if v, ok := item["script_filename"].(string); ok && v != "" {
		change.ScriptFilename = &v
	}
	if v, ok := item["script_language"].(string); ok && v != "" {
		change.ScriptLanguage = &v
	}
	if v, ok := item["script_purpose"].(string); ok && v != "" {
		change.ScriptPurpose = &v
	}
	return *checkpointing.MakeEvolutionRecord(source, summary, change, 0.6, nil, &summary)
}

// toAnySlice 将 map 切片转为 any 切片（校验入口 Normalize*Draft 的入参形态）。
func toAnySlice(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}
