package review

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// ──────────────────────────── 枚举 ────────────────────────────

// ScopeStatus 审查 scope 状态枚举。
// Python: ScopeStatus = Literal[...] (runtime.py:28-35)
type ScopeStatus string

const (
	// ScopeStatusReviewRequired 待审查。
	ScopeStatusReviewRequired ScopeStatus = "review_required"
	// ScopeStatusReviewCompleted 审查完成。
	ScopeStatusReviewCompleted ScopeStatus = "review_completed"
	// ScopeStatusSubmitted 已提交。
	ScopeStatusSubmitted ScopeStatus = "submitted"
	// ScopeStatusNoEvolution 无演进。
	ScopeStatusNoEvolution ScopeStatus = "no_evolution"
	// ScopeStatusCancelled 已取消。
	ScopeStatusCancelled ScopeStatus = "cancelled"
	// ScopeStatusFailed 已失败。
	ScopeStatusFailed ScopeStatus = "failed"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionReviewLaunch 审查启动返回体。
// Python: EvolutionReviewLaunch (runtime.py:30-34)
type EvolutionReviewLaunch struct {
	// ScopeID scope 标识。
	ScopeID string `json:"scope_id"`
	// EvolutionReviewRef 审查引用。
	EvolutionReviewRef string `json:"evolution_review_ref"`
	// Subject 归一化 subject payload。
	Subject map[string]any `json:"subject"`
}

// EvolutionReviewScope 单个审查 scope 状态。
// Python: EvolutionReviewScope (runtime.py:37-53)
type EvolutionReviewScope struct {
	// ScopeID scope 标识。
	ScopeID string `json:"scope_id"`
	// Source 创建来源。
	Source string `json:"source"`
	// Subject 归一化 subject payload。
	Subject map[string]any `json:"subject"`
	// SessionID 所属会话。
	SessionID string `json:"session_id"`
	// EvolutionReviewRef 审查引用。
	EvolutionReviewRef string `json:"evolution_review_ref"`
	// UserIntent 用户意图。
	UserIntent string `json:"user_intent"`
	// Trajectory 轨迹快照（可为空）。
	Trajectory *trajectory.Trajectory `json:"trajectory,omitempty"`
	// Status 当前状态。
	Status ScopeStatus `json:"status"`
	// ProposalIDs 已记录 proposal id 集合。
	ProposalIDs map[string]bool `json:"proposal_ids"`
	// ProposalDrafts proposal id → 经验草稿。
	ProposalDrafts map[string]map[string]any `json:"proposal_drafts"`
	// ReadTrace 已读证据引用集合。
	ReadTrace map[string]bool `json:"read_trace"`
	// Result 结构化审查结果（原始 payload 形态）。
	Result map[string]any `json:"result"`
	// CreatedAt 创建时间。
	CreatedAt time.Time `json:"created_at"`
	// ConsumedAt 消费时间（可为空）。
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
}

// EvolutionProposalDetail 单条已解析 proposal 明细。
// Python: EvolutionProposalDetail (runtime.py:55-65)
type EvolutionProposalDetail struct {
	// ProposalID proposal 标识。
	ProposalID string `json:"proposal_id"`
	// Summary 摘要。
	Summary string `json:"summary"`
	// Content 内容。
	Content string `json:"content"`
	// Target 目标层。
	Target string `json:"target"`
	// Section SKILL.md 区域。
	Section string `json:"section"`
	// Reason 理由。
	Reason string `json:"reason"`
	// EvidenceRefs 证据引用。
	EvidenceRefs []string `json:"evidence_refs"`
}

// EvolutionProposalSelection 用户选定 proposals 后的执行载体。
// Python: EvolutionProposalSelection (runtime.py:68-75)
type EvolutionProposalSelection struct {
	// EvolutionReviewRef 审查引用。
	EvolutionReviewRef string `json:"evolution_review_ref"`
	// Subject 归一化 subject payload。
	Subject map[string]any `json:"subject"`
	// SelectedProposalIDs 选定 proposal id。
	SelectedProposalIDs []string `json:"selected_proposal_ids"`
	// Proposals 已解析 proposal 明细。
	Proposals []EvolutionProposalDetail `json:"proposals"`
	// ExperienceDrafts 执行用经验草稿（reason 注入后）。
	ExperienceDrafts []map[string]any `json:"experience_drafts"`
	// RecordSource 记录来源（user_intent / agent_inferred）。
	RecordSource string `json:"record_source"`
}

// EvolutionReviewRuntime 承载 SkillEvolutionRail 之外的审查 scope 状态。
// Python: EvolutionReviewRuntime (runtime.py:78-289)
// 偏差 D-15.21-3：Python 单线程共享 dict → Go sync.Mutex 保护（asyncio→Go 约定）。
type EvolutionReviewRuntime struct {
	mu sync.Mutex
	// scopesByRef ref → scope。
	scopesByRef map[string]*EvolutionReviewScope
	// submittedScopeRetention 已提交 scope 重放窗口。
	submittedScopeRetention time.Duration
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionReviewRuntime 创建审查运行时。
// Python: EvolutionReviewRuntime.__init__ (runtime.py:78-85)
func NewEvolutionReviewRuntime(submittedScopeRetentionSecs float64) *EvolutionReviewRuntime {
	if submittedScopeRetentionSecs < 0 {
		submittedScopeRetentionSecs = 0
	}
	return &EvolutionReviewRuntime{
		scopesByRef:             make(map[string]*EvolutionReviewScope),
		submittedScopeRetention: time.Duration(submittedScopeRetentionSecs * float64(time.Second)),
	}
}

// PruneScopes 移除超出重放窗口的已提交 scope。
// Python: prune_scopes (runtime.py:86-95)
func (rt *EvolutionReviewRuntime) PruneScopes() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	now := time.Now().UTC()
	submittedCutoff := now.Add(-rt.submittedScopeRetention)
	for ref, scope := range rt.scopesByRef {
		if scope.Status == ScopeStatusSubmitted && scope.ConsumedAt != nil && !scope.ConsumedAt.After(submittedCutoff) {
			delete(rt.scopesByRef, ref)
		}
	}
}

// CreateScope 创建审查 scope 并返回启动返回体。
// Python: create_scope (runtime.py:97-124)
func (rt *EvolutionReviewRuntime) CreateScope(source string, subject any, sessionID string, userIntent string, traj *trajectory.Trajectory) (EvolutionReviewLaunch, error) {
	rt.PruneScopes()
	normalizedSubject, err := normalizeSubjectOrError(subject)
	if err != nil {
		return EvolutionReviewLaunch{}, err
	}
	subjectPayload := normalizedSubject.ToPayload()
	scopeID := fmt.Sprintf("evrs_%s", uuid.NewString()[:12])
	ref := fmt.Sprintf("evrr_%s_%s", scopeID, uuid.NewString()[:8])
	scope := &EvolutionReviewScope{
		ScopeID:            scopeID,
		Source:             source,
		Subject:            subjectPayload,
		SessionID:          sessionID,
		EvolutionReviewRef: ref,
		UserIntent:         userIntent,
		Status:             ScopeStatusReviewRequired,
		ProposalIDs:        make(map[string]bool),
		ProposalDrafts:     make(map[string]map[string]any),
		ReadTrace:          make(map[string]bool),
		CreatedAt:          time.Now().UTC(),
	}
	if traj != nil {
		// Python: Trajectory.from_otlp(trajectory.to_otlp())——快照拷贝语义
		snapshot, snapErr := trajectory.FromOTLP(traj.ToOTLP())
		if snapErr != nil {
			return EvolutionReviewLaunch{}, snapErr
		}
		scope.Trajectory = snapshot
	}
	rt.mu.Lock()
	rt.scopesByRef[ref] = scope
	rt.mu.Unlock()
	return EvolutionReviewLaunch{
		ScopeID:            scopeID,
		EvolutionReviewRef: ref,
		Subject:            subjectPayload,
	}, nil
}

// ResolveScope 按引用解析 scope（校验会话匹配）。
// Python: resolve_scope (runtime.py:126-144)
func (rt *EvolutionReviewRuntime) ResolveScope(evolutionReviewRef string, sessionID string) (*EvolutionReviewScope, error) {
	rt.PruneScopes()
	rt.mu.Lock()
	scope, ok := rt.scopesByRef[evolutionReviewRef]
	if ok && scope.SessionID == "" && sessionID != "" {
		scope.SessionID = sessionID
	}
	matched := ok && sessionMatchesScope(scope.SessionID, sessionID)
	rt.mu.Unlock()
	if !matched {
		// Python: KeyError(evolution_review_ref)
		return nil, fmt.Errorf("scope not found: %s", evolutionReviewRef)
	}
	return scope, nil
}

// HasScope 判断引用是否存在。
// Python: has_scope (runtime.py:146-148)
func (rt *EvolutionReviewRuntime) HasScope(evolutionReviewRef string) bool {
	rt.PruneScopes()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, ok := rt.scopesByRef[evolutionReviewRef]
	return ok
}

// RecordEvidenceRead 记录已读证据引用。
// Python: record_evidence_read (runtime.py:150-152)
func (rt *EvolutionReviewRuntime) RecordEvidenceRead(evolutionReviewRef string, sessionID string, refs []string) error {
	scope, err := rt.ResolveScope(evolutionReviewRef, sessionID)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, ref := range refs {
		scope.ReadTrace[ref] = true
	}
	return nil
}

// RecordTrajectoryRead 记录已读轨迹引用（委托 evidence read）。
// Python: record_trajectory_read (runtime.py:154-155)
func (rt *EvolutionReviewRuntime) RecordTrajectoryRead(evolutionReviewRef string, sessionID string, refs []string) error {
	return rt.RecordEvidenceRead(evolutionReviewRef, sessionID, refs)
}

// RecordReviewResult 写入审查结果并推进 scope 状态。
// Python: record_review_result (runtime.py:157-196)
func (rt *EvolutionReviewRuntime) RecordReviewResult(evolutionReviewRef string, sessionID string, result map[string]any) (*EvolutionReviewScope, error) {
	scope, err := rt.ResolveScope(evolutionReviewRef, sessionID)
	if err != nil {
		return nil, err
	}
	scopeSubject := scope.Subject
	normalized, err := NormalizeReviewResult(result, scopeSubject)
	if err != nil {
		return nil, err
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	// 未读证据引用校验
	missingRefs := make([]string, 0)
	for _, ref := range normalized.EvidenceRefs {
		if !scope.ReadTrace[ref] {
			missingRefs = append(missingRefs, ref)
		}
	}
	if len(missingRefs) > 0 {
		sort.Strings(missingRefs)
		return nil, fmt.Errorf("review result references unread evidence refs: %v", missingRefs)
	}

	proposalDrafts := make(map[string]map[string]any)
	proposalIDs := make(map[string]bool)
	if normalized.Outcome == "recommend_evolve" {
		for _, proposal := range normalized.Proposals {
			proposalDrafts[proposal.ProposalID] = proposal.Experience
			proposalIDs[proposal.ProposalID] = true
		}
	}

	proposalsPayload := make([]map[string]any, 0, len(normalized.Proposals))
	for _, p := range normalized.Proposals {
		proposalsPayload = append(proposalsPayload, map[string]any{
			"proposal_id":   p.ProposalID,
			"experience":    p.Experience,
			"reason":        p.Reason,
			"evidence_refs": p.EvidenceRefs,
		})
	}
	scope.Result = map[string]any{
		"subject":       normalized.Subject.ToPayload(),
		"outcome":       normalized.Outcome,
		"evidence_refs": normalized.EvidenceRefs,
		"proposals":     proposalsPayload,
		"summary":       normalized.Summary,
	}
	scope.ProposalIDs = proposalIDs
	scope.ProposalDrafts = proposalDrafts
	if normalized.Outcome == "recommend_evolve" {
		scope.Status = ScopeStatusReviewCompleted
	} else {
		scope.Status = ScopeStatusNoEvolution
	}
	return scope, nil
}

// ResolveSelectedProposals 解析用户选定的 proposals 并产出执行载体。
// Python: resolve_selected_proposals (runtime.py:198-262)
func (rt *EvolutionReviewRuntime) ResolveSelectedProposals(evolutionReviewRef string, subject map[string]any, selectedProposalIDs []string, sessionID string) (EvolutionProposalSelection, error) {
	if evolutionReviewRef == "" {
		return EvolutionProposalSelection{}, fmt.Errorf("evolution_review_ref is required")
	}
	scope, err := rt.ResolveScope(evolutionReviewRef, sessionID)
	if err != nil {
		return EvolutionProposalSelection{}, err
	}
	normalizedSubject, err := normalizeSubjectOrError(subject)
	if err != nil {
		return EvolutionProposalSelection{}, err
	}
	normalizedPayload := normalizedSubject.ToPayload()
	if !subjectPayloadEqual(normalizedPayload, scope.Subject) {
		return EvolutionProposalSelection{}, fmt.Errorf("evolution review subject mismatch")
	}
	if scope.Status == ScopeStatusSubmitted {
		return EvolutionProposalSelection{}, fmt.Errorf("evolution review ref already submitted")
	}
	if scope.Status == ScopeStatusNoEvolution {
		return EvolutionProposalSelection{}, fmt.Errorf("evolution review has no evolution proposals")
	}
	if scope.Status != ScopeStatusReviewCompleted {
		return EvolutionProposalSelection{}, fmt.Errorf("evolution review is not completed")
	}
	ids := make([]string, 0, len(selectedProposalIDs))
	seen := make(map[string]bool)
	for _, item := range selectedProposalIDs {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		ids = append(ids, trimmed)
	}
	if len(ids) == 0 {
		return EvolutionProposalSelection{}, fmt.Errorf("selected_proposal_ids are required")
	}
	for _, id := range ids {
		if seen[id] {
			return EvolutionProposalSelection{}, fmt.Errorf("duplicate proposal id")
		}
		seen[id] = true
	}

	// Python: proposal_by_id 从 scope.result["proposals"] 构建
	proposalByID := make(map[string]map[string]any)
	resultProposals, _ := scope.Result["proposals"].([]map[string]any)
	for _, proposal := range resultProposals {
		if id, ok := proposal["proposal_id"].(string); ok {
			proposalByID[id] = proposal
		}
	}
	resolvedProposals := make([]EvolutionProposalDetail, 0, len(ids))
	drafts := make([]map[string]any, 0, len(ids))
	for _, proposalID := range ids {
		draft := scope.ProposalDrafts[proposalID]
		proposal := proposalByID[proposalID]
		if draft == nil || proposal == nil {
			return EvolutionProposalSelection{}, fmt.Errorf("unknown proposal_id: %s", proposalID)
		}
		reason := asReviewString(proposal["reason"])
		if reason == "" {
			reason = asReviewString(draft["reason"])
		}
		evidenceRefs := make([]string, 0)
		for _, ref := range toAnySlice(proposal["evidence_refs"]) {
			evidenceRefs = append(evidenceRefs, fmt.Sprintf("%v", ref))
		}
		resolvedProposals = append(resolvedProposals, EvolutionProposalDetail{
			ProposalID:   proposalID,
			Summary:      asReviewString(draft["summary"]),
			Content:      asReviewString(draft["content"]),
			Target:       asReviewString(draft["target"]),
			Section:      asReviewString(draft["section"]),
			Reason:       reason,
			EvidenceRefs: evidenceRefs,
		})
		draftForExecution := make(map[string]any, len(draft))
		for k, v := range draft {
			draftForExecution[k] = v
		}
		if reason != "" {
			draftForExecution["reason"] = reason
		}
		drafts = append(drafts, draftForExecution)
	}

	recordSource := "agent_inferred"
	if strings.TrimSpace(scope.UserIntent) != "" {
		recordSource = "user_intent"
	}
	return EvolutionProposalSelection{
		EvolutionReviewRef:  evolutionReviewRef,
		Subject:             scope.Subject,
		SelectedProposalIDs: ids,
		Proposals:           resolvedProposals,
		ExperienceDrafts:    drafts,
		RecordSource:        recordSource,
	}, nil
}

// ConsumePreparedSubmission 将 scope 标记为已提交（不重复解析 proposals）。
// Python: consume_prepared_submission (runtime.py:279-289)
func (rt *EvolutionReviewRuntime) ConsumePreparedSubmission(prepared EvolutionProposalSelection, sessionID string) error {
	rt.PruneScopes()
	scope, err := rt.ResolveScope(prepared.EvolutionReviewRef, sessionID)
	if err != nil {
		return err
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	scope.Status = ScopeStatusSubmitted
	now := time.Now().UTC()
	scope.ConsumedAt = &now
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sessionMatchesScope 校验会话与 scope 匹配（reviewer 子会话后缀规则）。
// Python: EvolutionReviewRuntime._session_matches_scope (runtime.py:136-144)
func sessionMatchesScope(scopeSessionID string, sessionID string) bool {
	if scopeSessionID == sessionID {
		return true
	}
	prefix := scopeSessionID + "_sub_evolution_reviewer_"
	sessionText := sessionID
	if scopeSessionID == "" || !strings.HasPrefix(sessionText, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(sessionText, prefix)
	if len(suffix) != 8 {
		return false
	}
	for _, ch := range suffix {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return false
		}
	}
	return true
}

// normalizeSubjectOrError subject 归一化，异常透传为普通 error。
// Python: EvolutionReviewRuntime._normalize_subject (runtime.py:264-268)
func normalizeSubjectOrError(subject any) (evoschema.EvolutionSubject, error) {
	normalized, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return evoschema.EvolutionSubject{}, fmt.Errorf("%v", err)
	}
	return normalized, nil
}
