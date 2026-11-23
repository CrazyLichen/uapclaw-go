package review

import (
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
)

func sampleSubject() map[string]any {
	return map[string]any{"kind": "skill", "name": "demo"}
}

// TestNormalizeReviewResult 测试原始审查结果结构化。
// Python: normalize_review_result (result_schema.py:39-78)
func TestNormalizeReviewResult(t *testing.T) {
	t.Run("subject 不匹配报错", func(t *testing.T) {
		raw := map[string]any{"subject": map[string]any{"kind": "skill", "name": "other"}, "outcome": "no_evolution"}
		_, err := NormalizeReviewResult(raw, sampleSubject())
		if err == nil || !strings.Contains(err.Error(), "review result subject mismatch") {
			t.Fatalf("期望 subject mismatch, got %v", err)
		}
	})

	t.Run("非法 outcome 报错", func(t *testing.T) {
		raw := map[string]any{"subject": sampleSubject(), "outcome": "maybe"}
		_, err := NormalizeReviewResult(raw, sampleSubject())
		if err == nil || !strings.Contains(err.Error(), "invalid review outcome: maybe") {
			t.Fatalf("期望 outcome 报错, got %v", err)
		}
	})

	t.Run("recommend_evolve 缺 proposals 报错", func(t *testing.T) {
		raw := map[string]any{"subject": sampleSubject(), "outcome": "recommend_evolve"}
		_, err := NormalizeReviewResult(raw, sampleSubject())
		if err == nil || !strings.Contains(err.Error(), "recommend_evolve requires proposals") {
			t.Fatalf("期望 proposals 必填报错, got %v", err)
		}
	})

	t.Run("no_evolution 携带 proposals 报错", func(t *testing.T) {
		raw := map[string]any{
			"subject": sampleSubject(), "outcome": "no_evolution",
			"proposals": []any{map[string]any{"proposal_id": "p1"}},
		}
		_, err := NormalizeReviewResult(raw, sampleSubject())
		if err == nil || !strings.Contains(err.Error(), "no_evolution must not include proposals") {
			t.Fatalf("期望禁带 proposals 报错, got %v", err)
		}
	})

	t.Run("no_evolution 合法路径 + evidence 聚合排序", func(t *testing.T) {
		raw := map[string]any{
			"subject": sampleSubject(), "outcome": "no_evolution",
			"summary":      "ok",
			"evidence_refs": []any{"b-ref", "a-ref"},
			"proposals":     []any{},
		}
		result, err := NormalizeReviewResult(raw, sampleSubject())
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if result.Outcome != "no_evolution" || result.Summary != "ok" {
			t.Fatalf("字段未保留: %+v", result)
		}
		if len(result.EvidenceRefs) != 2 || result.EvidenceRefs[0] != "a-ref" || result.EvidenceRefs[1] != "b-ref" {
			t.Fatalf("evidence_refs 期望排序后 [a-ref b-ref], got %v", result.EvidenceRefs)
		}
	})

	t.Run("recommend_evolve 完整路径", func(t *testing.T) {
		raw := map[string]any{
			"subject": sampleSubject(), "outcome": "recommend_evolve",
			"evidence_refs": []any{"top1"},
			"proposals": []any{
				map[string]any{
					"proposal_id": "p1", "reason": "r1",
					"experience":   map[string]any{"summary": "s", "content": "c"},
					"evidence_refs": []any{"top1", "p1a"},
				},
			},
		}
		result, err := NormalizeReviewResult(raw, sampleSubject())
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(result.Proposals) != 1 {
			t.Fatalf("期望 1 条 proposal, got %d", len(result.Proposals))
		}
		p := result.Proposals[0]
		if p.ProposalID != "p1" || p.Reason != "r1" {
			t.Fatalf("proposal 字段未保留: %+v", p)
		}
		if len(p.EvidenceRefs) != 2 {
			t.Fatalf("proposal evidence 期望 2 项, got %v", p.EvidenceRefs)
		}
		// 聚合应含 top-level + proposal 的并集且排序
		if len(result.EvidenceRefs) != 2 || result.EvidenceRefs[0] != "p1a" || result.EvidenceRefs[1] != "top1" {
			t.Fatalf("evidence 聚合期望排序 [p1a top1], got %v", result.EvidenceRefs)
		}
		// proposal experience 经过 normalize_evolve_draft（含 summary/content/target/section/reason）
		exp := p.Experience
		if exp["summary"] != "s" || exp["content"] != "c" || exp["target"] != "body" || exp["section"] != "Troubleshooting" {
			t.Fatalf("experience 未归一化: %v", exp)
		}
	})
}

// TestNormalizeReviewProposals 测试 proposals 归一化校验。
// Python: normalize_review_proposals (result_schema.py:81-133)
func TestNormalizeReviewProposals(t *testing.T) {
	subject := experience.EvolutionSubject{Kind: "skill", Name: "demo"}
	mk := func(id string) map[string]any {
		return map[string]any{
			"proposal_id": id,
			"experience":  map[string]any{"summary": "s", "content": "c"},
		}
	}

	t.Run("非列表报错", func(t *testing.T) {
		_, err := NormalizeReviewProposals(subject, "bad")
		if err == nil || !strings.Contains(err.Error(), "proposals must be a list") {
			t.Fatalf("期望列表校验报错, got %v", err)
		}
	})

	t.Run("超上限报错", func(t *testing.T) {
		items := make([]any, experience.MaxEvolutionReviewProposals+1)
		for i := range items {
			items[i] = mk(fmt.Sprintf("p%d", i))
		}
		_, err := NormalizeReviewProposals(subject, items)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("proposals must contain at most %d items", experience.MaxEvolutionReviewProposals)) {
			t.Fatalf("期望上限报错, got %v", err)
		}
	})

	t.Run("非对象项报错", func(t *testing.T) {
		_, err := NormalizeReviewProposals(subject, []any{"bad"})
		if err == nil || !strings.Contains(err.Error(), "proposal at index 0 must be an object") {
			t.Fatalf("期望 index 报错, got %v", err)
		}
	})

	t.Run("proposal_id 缺失/重复报错", func(t *testing.T) {
		_, err := NormalizeReviewProposals(subject, []any{map[string]any{"experience": map[string]any{}}})
		if err == nil || !strings.Contains(err.Error(), "proposal_id is required") {
			t.Fatalf("期望 id 必填报错, got %v", err)
		}
		_, err = NormalizeReviewProposals(subject, []any{mk("p1"), mk("p1")})
		if err == nil || !strings.Contains(err.Error(), "duplicate proposal_id: p1") {
			t.Fatalf("期望重复报错, got %v", err)
		}
	})

	t.Run("experience 缺失报错", func(t *testing.T) {
		_, err := NormalizeReviewProposals(subject, []any{map[string]any{"proposal_id": "p1"}})
		if err == nil || !strings.Contains(err.Error(), "proposal experience is required") {
			t.Fatalf("期望 experience 必填报错, got %v", err)
		}
	})

	t.Run("顺序保留与 evidence 提取", func(t *testing.T) {
		proposals, err := NormalizeReviewProposals(subject, []any{
			mk("z"), mk("a"),
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if proposals[0].ProposalID != "z" || proposals[1].ProposalID != "a" {
			t.Fatalf("期望保持输入顺序, got %v %v", proposals[0].ProposalID, proposals[1].ProposalID)
		}
	})
}

// TestCollectEvidenceRefs 测试 evidence refs 聚合。
// Python: _collect_evidence_refs (result_schema.py:136-142)
func TestCollectEvidenceRefs(t *testing.T) {
	raw := map[string]any{
		"evidence_refs": []any{"t1"},
		"proposals": []any{
			map[string]any{"evidence_refs": []any{"p1", "t1"}},
			"not-a-dict",
		},
	}
	refs := CollectEvidenceRefs(raw)
	if len(refs) != 2 || !refs["t1"] || !refs["p1"] {
		t.Fatalf("期望聚合 {t1,p1} 且跳过非对象项, got %v", refs)
	}
}
