// Package review 提供受限技能演进审查的运行时状态与门禁。
//
// 本包对齐 openjiuwen/harness/rails/evolution/review/（runtime/subagent/
// result_schema 三文件一比一）：EvolutionReviewRuntime 维护审查 scope 生命
// 周期（review_required → review_completed → submitted / no_evolution /
// cancelled / failed），result_schema 承载审查 agent 的结构化结果归一化，
// subagent 承载审查子 agent 配置构建与清理。由 EvolutionInterruptRail 与
// SkillEvolutionRail 共享，跨 rail 保持审查态。
//
// 文件目录：
//
//	review/
//	├── doc.go           # 包文档
//	├── runtime.go      # EvolutionReviewRuntime scope 生命周期
//	├── result_schema.go # 审查结果 schema 归一化
//	└── subagent.go     # 审查子 agent 配置构建/清理
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/review/
package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionReviewProposal 审查 agent 产出的单条 proposal。
// Python: EvolutionReviewProposal (result_schema.py:19-26)
type EvolutionReviewProposal struct {
	// ProposalID proposal 标识。
	ProposalID string `json:"proposal_id"`
	// Experience 归一化后的经验条目。
	Experience map[string]any `json:"experience"`
	// Reason 审查理由。
	Reason string `json:"reason"`
	// EvidenceRefs 证据引用。
	EvidenceRefs []string `json:"evidence_refs"`
}

// EvolutionReviewResult 审查 agent 的结构化结果。
// Python: EvolutionReviewResult (result_schema.py:29-36)
type EvolutionReviewResult struct {
	// Subject 归一化 subject。
	Subject evoschema.EvolutionSubject `json:"subject"`
	// Outcome 结论（recommend_evolve / no_evolution）。
	Outcome string `json:"outcome"`
	// EvidenceRefs 聚合证据引用（排序后）。
	EvidenceRefs []string `json:"evidence_refs"`
	// Proposals 归一化 proposals。
	Proposals []EvolutionReviewProposal `json:"proposals"`
	// Summary 审查摘要。
	Summary string `json:"summary"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeReviewResult 将原始审查结果 dict 结构化为 EvolutionReviewResult。
// 校验 subject 匹配、outcome、proposal 结构。
// 不检查 scope.read_trace——那是 runtime 的职责。
// Python: normalize_review_result (result_schema.py:39-78)
func NormalizeReviewResult(raw map[string]any, scopeSubject map[string]any) (EvolutionReviewResult, error) {
	// Python: raw = dict(raw)（拷贝语义，Go map 赋值本身不深拷贝，逐字段读取等价）
	subjectRaw, hasSubject := raw["subject"]
	if !hasSubject {
		subjectRaw = nil
	}
	normalizedSubject, err := evoschema.NormalizeSubject(subjectRaw, nil)
	if err != nil {
		return EvolutionReviewResult{}, err
	}
	if !subjectPayloadEqual(normalizedSubject.ToPayload(), scopeSubject) {
		return EvolutionReviewResult{}, fmt.Errorf("review result subject mismatch")
	}

	evidenceRefs := CollectEvidenceRefs(raw)
	outcome := raw["outcome"]
	if outcome != "recommend_evolve" && outcome != "no_evolution" {
		return EvolutionReviewResult{}, fmt.Errorf("invalid review outcome: %v", outcome)
	}

	summary := asReviewString(orReviewDefault(raw["summary"], ""))

	var proposals []EvolutionReviewProposal
	rawProposals := orReviewDefault(raw["proposals"], []any{})
	if outcome == "recommend_evolve" {
		list := toAnySlice(rawProposals)
		if len(list) == 0 {
			return EvolutionReviewResult{}, fmt.Errorf("recommend_evolve requires proposals")
		}
		proposals, err = NormalizeReviewProposals(normalizedSubject, list)
		if err != nil {
			return EvolutionReviewResult{}, err
		}
	} else {
		list := toAnySlice(rawProposals)
		if len(list) > 0 {
			return EvolutionReviewResult{}, fmt.Errorf("no_evolution must not include proposals")
		}
	}

	return EvolutionReviewResult{
		Subject:      normalizedSubject,
		Outcome:      outcome.(string),
		EvidenceRefs: sortedStrings(evidenceRefs),
		Proposals:    proposals,
		Summary:      summary,
	}, nil
}

// NormalizeReviewProposals 归一化并校验审查 proposals。
// 校验 proposal_id 唯一性、experience 存在性，experience 草稿经
// normalize_evolve_draft 归一化。
// Python: normalize_review_proposals (result_schema.py:81-133)
func NormalizeReviewProposals(subject evoschema.EvolutionSubject, proposals any) ([]EvolutionReviewProposal, error) {
	list, ok := proposals.([]any)
	if !ok {
		return nil, fmt.Errorf("proposals must be a list")
	}
	if len(list) > evoschema.MaxEvolutionReviewProposals {
		return nil, fmt.Errorf("proposals must contain at most %d items", evoschema.MaxEvolutionReviewProposals)
	}

	proposalIDs := make(map[string]bool)
	draftItems := make([]map[string]any, 0, len(list))
	orderedIDs := make([]string, 0, len(list))
	reasons := make(map[string]string)
	evidence := make(map[string][]string)

	for index, raw := range list {
		proposal, isMap := raw.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("proposal at index %d must be an object", index)
		}
		proposalID := strings.TrimSpace(asReviewString(orReviewDefault(proposal["proposal_id"], "")))
		if proposalID == "" {
			return nil, fmt.Errorf("proposal_id is required")
		}
		if proposalIDs[proposalID] {
			return nil, fmt.Errorf("duplicate proposal_id: %s", proposalID)
		}
		proposalIDs[proposalID] = true
		experienceRaw, isExpMap := proposal["experience"].(map[string]any)
		if !isExpMap {
			return nil, fmt.Errorf("proposal experience is required")
		}
		orderedIDs = append(orderedIDs, proposalID)
		draftItems = append(draftItems, experienceRaw)
		reasons[proposalID] = asReviewString(orReviewDefault(proposal["reason"], ""))
		refs := make([]string, 0)
		for _, ref := range toAnySlice(orReviewDefault(proposal["evidence_refs"], []any{})) {
			refs = append(refs, fmt.Sprintf("%v", ref))
		}
		evidence[proposalID] = refs
	}

	// Python: normalize_evolve_draft 异常转 ValueError（消息透传）
	draft, err := evoschema.NormalizeEvolveDraft(subject, draftItems)
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}

	result := make([]EvolutionReviewProposal, 0, len(orderedIDs))
	for i, proposalID := range orderedIDs {
		result = append(result, EvolutionReviewProposal{
			ProposalID:   proposalID,
			Experience:   draft.Experiences[i],
			Reason:       reasons[proposalID],
			EvidenceRefs: evidence[proposalID],
		})
	}
	return result, nil
}

// CollectEvidenceRefs 从顶层与各 proposal 聚合全部 evidence refs。
// Python: _collect_evidence_refs (result_schema.py:136-142)
func CollectEvidenceRefs(result map[string]any) map[string]bool {
	refs := make(map[string]bool)
	for _, ref := range toAnySlice(result["evidence_refs"]) {
		refs[fmt.Sprintf("%v", ref)] = true
	}
	for _, proposal := range toAnySlice(orReviewDefault(result["proposals"], []any{})) {
		if p, isMap := proposal.(map[string]any); isMap {
			for _, ref := range toAnySlice(orReviewDefault(p["evidence_refs"], []any{})) {
				refs[fmt.Sprintf("%v", ref)] = true
			}
		}
	}
	return refs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// subjectPayloadEqual 比较 subject payload 与 scope subject（Python dict 相等）。
func subjectPayloadEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return false
		}
		switch avMap := av.(type) {
		case map[string]any:
			bvMap, isMap := bv.(map[string]any)
			if !isMap || !subjectPayloadEqual(avMap, bvMap) {
				return false
			}
		default:
			if fmt.Sprintf("%v", av) != fmt.Sprintf("%v", bv) {
				return false
			}
		}
	}
	return true
}

// asReviewString 宽松转字符串（Python str() 等价）。
func asReviewString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// orReviewDefault nil 回退默认值。
func orReviewDefault(v any, fallback any) any {
	if v == nil {
		return fallback
	}
	return v
}

// toAnySlice 宽松转 []any（非切片回空）。
func toAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// mapSliceToAnySlice []map[string]any → []any。
// sortedStrings 排序去重后的字符串集合转切片。
func sortedStrings(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
