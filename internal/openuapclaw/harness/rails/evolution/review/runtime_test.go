package review

import (
	"strings"
	"testing"
)

func newTestRuntime() *EvolutionReviewRuntime {
	return NewEvolutionReviewRuntime(60.0)
}

func createTestScope(rt *EvolutionReviewRuntime, sessionID string) EvolutionReviewLaunch {
	launch, _ := rt.CreateScope("test_source", sampleSubject(), sessionID, "", nil)
	return launch
}

// TestEvolutionReviewRuntime_CreateScope 测试 scope 创建与 ref 形态。
// Python: create_scope (runtime.py:97-124)
func TestEvolutionReviewRuntime_CreateScope(t *testing.T) {
	rt := newTestRuntime()
	launch := createTestScope(rt, "sess-1")
	if !strings.HasPrefix(launch.ScopeID, "evrs_") || len(launch.ScopeID) != len("evrs_")+12 {
		t.Fatalf("scope_id 形态不符: %s", launch.ScopeID)
	}
	if !strings.HasPrefix(launch.EvolutionReviewRef, "evrr_"+launch.ScopeID+"_") {
		t.Fatalf("ref 形态不符: %s", launch.EvolutionReviewRef)
	}
	if !subjectPayloadEqual(launch.Subject, sampleSubject()) {
		t.Fatalf("subject 未归一化携带: %v", launch.Subject)
	}
	scope, err := rt.ResolveScope(launch.EvolutionReviewRef, "sess-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if scope.Status != ScopeStatusReviewRequired || scope.Source != "test_source" {
		t.Fatalf("初始态不符: %+v", scope)
	}
	if scope.CreatedAt.IsZero() || scope.ConsumedAt != nil {
		t.Fatalf("时间字段不符: %+v", scope)
	}
}

// TestEvolutionReviewRuntime_ResolveScope 测试解析与会话匹配。
// Python: resolve_scope (runtime.py:126-144)
func TestEvolutionReviewRuntime_ResolveScope(t *testing.T) {
	rt := newTestRuntime()
	launch := createTestScope(rt, "sess-1")

	t.Run("session 不匹配报 KeyError", func(t *testing.T) {
		_, err := rt.ResolveScope(launch.EvolutionReviewRef, "other-sess")
		if err == nil || !strings.Contains(err.Error(), "scope not found") {
			t.Fatalf("期望 scope not found, got %v", err)
		}
	})

	t.Run("空 session 先绑定再匹配", func(t *testing.T) {
		rt2 := newTestRuntime()
		launch2, _ := rt2.CreateScope("src", sampleSubject(), "", "", nil)
		scope, err := rt2.ResolveScope(launch2.EvolutionReviewRef, "sess-9")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if scope.SessionID != "sess-9" {
			t.Fatalf("期望空 session 被绑定, got %s", scope.SessionID)
		}
	})

	t.Run("reviewer 子会话后缀匹配", func(t *testing.T) {
		suffix := "0123abcd"
		subSession := "sess-1_sub_evolution_reviewer_" + suffix
		_, err := rt.ResolveScope(launch.EvolutionReviewRef, subSession)
		if err != nil {
			t.Fatalf("期望 8 位十六进制后缀匹配, got %v", err)
		}
	})

	t.Run("reviewer 子会话后缀非法报错", func(t *testing.T) {
		bad := "sess-1_sub_evolution_reviewer_zzzzzzzz"
		_, err := rt.ResolveScope(launch.EvolutionReviewRef, bad)
		if err == nil || !strings.Contains(err.Error(), "scope not found") {
			t.Fatalf("期望非法后缀拒绝, got %v", err)
		}
	})

	t.Run("未知 ref 报 KeyError", func(t *testing.T) {
		_, err := rt.ResolveScope("evrr_unknown", "sess-1")
		if err == nil || !strings.Contains(err.Error(), "scope not found") {
			t.Fatalf("期望 scope not found, got %v", err)
		}
	})
}

// TestEvolutionReviewRuntime_RecordReads 测试读证据轨迹记录。
// Python: record_evidence_read / record_trajectory_read (runtime.py:150-155)
func TestEvolutionReviewRuntime_RecordReads(t *testing.T) {
	rt := newTestRuntime()
	launch := createTestScope(rt, "sess-1")
	if err := rt.RecordEvidenceRead(launch.EvolutionReviewRef, "sess-1", []string{"ref-a", "ref-a", "ref-b"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := rt.RecordTrajectoryRead(launch.EvolutionReviewRef, "sess-1", []string{"ref-c"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	scope, _ := rt.ResolveScope(launch.EvolutionReviewRef, "sess-1")
	if len(scope.ReadTrace) != 3 {
		t.Fatalf("期望 read_trace 3 项（set 去重）, got %v", scope.ReadTrace)
	}
	if !scope.ReadTrace["ref-a"] || !scope.ReadTrace["ref-b"] || !scope.ReadTrace["ref-c"] {
		t.Fatalf("read_trace 内容不符: %v", scope.ReadTrace)
	}
}

// TestEvolutionReviewRuntime_RecordReviewResult 测试审查结果写入。
// Python: record_review_result (runtime.py:157-196)
func TestEvolutionReviewRuntime_RecordReviewResult(t *testing.T) {
	rt := newTestRuntime()
	launch := createTestScope(rt, "sess-1")

	proposal := map[string]any{
		"proposal_id": "p1", "reason": "r1",
		"experience":   map[string]any{"summary": "s", "content": "c"},
		"evidence_refs": []any{"ev1"},
	}
	raw := map[string]any{
		"subject": sampleSubject(), "outcome": "recommend_evolve",
		"evidence_refs": []any{"ev1"},
		"proposals":     []any{proposal},
	}
	_ = raw
	// 未读证据先记录
	_ = rt.RecordEvidenceRead(launch.EvolutionReviewRef, "sess-1", []string{"ev1"})

	t.Run("recommend_evolve 完成态", func(t *testing.T) {
		scope, err := rt.RecordReviewResult(launch.EvolutionReviewRef, "sess-1", map[string]any{
			"subject": sampleSubject(), "outcome": "recommend_evolve",
			"evidence_refs": []any{"ev1"},
			"proposals":     []any{proposal},
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if scope.Status != ScopeStatusReviewCompleted {
			t.Fatalf("期望 review_completed, got %s", scope.Status)
		}
		if _, ok := scope.ProposalDrafts["p1"]; !ok {
			t.Fatalf("proposal_drafts 未落: %v", scope.ProposalDrafts)
		}
		if !scope.ProposalIDs["p1"] {
			t.Fatalf("proposal_ids 未落: %v", scope.ProposalIDs)
		}
		saved := scope.Result
		if saved["outcome"] != "recommend_evolve" {
			t.Fatalf("result 未保存: %v", saved)
		}
	})

	t.Run("未读证据引用报错", func(t *testing.T) {
		launch2 := createTestScope(rt, "sess-1")
		_, err := rt.RecordReviewResult(launch2.EvolutionReviewRef, "sess-1", map[string]any{
			"subject": sampleSubject(), "outcome": "no_evolution",
			"evidence_refs": []any{"unread"},
		})
		if err == nil || !strings.Contains(err.Error(), "review result references unread evidence refs: [unread]") {
			t.Fatalf("期望未读证据报错, got %v", err)
		}
	})

	t.Run("no_evolution 完成态", func(t *testing.T) {
		launch3 := createTestScope(rt, "sess-1")
		scope, err := rt.RecordReviewResult(launch3.EvolutionReviewRef, "sess-1", map[string]any{
			"subject": sampleSubject(), "outcome": "no_evolution",
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if scope.Status != ScopeStatusNoEvolution {
			t.Fatalf("期望 no_evolution, got %s", scope.Status)
		}
	})
}

// TestEvolutionReviewRuntime_ResolveSelectedProposals 测试 proposal 选择。
// Python: resolve_selected_proposals (runtime.py:198-262)
func TestEvolutionReviewRuntime_ResolveSelectedProposals(t *testing.T) {
	rt := newTestRuntime()
	launch := createTestScope(rt, "sess-1")
	_ = rt.RecordEvidenceRead(launch.EvolutionReviewRef, "sess-1", []string{"ev1"})
	_, err := rt.RecordReviewResult(launch.EvolutionReviewRef, "sess-1", map[string]any{
		"subject": sampleSubject(), "outcome": "recommend_evolve",
		"evidence_refs": []any{"ev1"},
		"proposals": []any{
			map[string]any{
				"proposal_id": "p1", "reason": "r1",
				"experience":   map[string]any{"summary": "s1", "content": "c1"},
				"evidence_refs": []any{"ev1"},
			},
			map[string]any{
				"proposal_id": "p2",
				"experience":  map[string]any{"summary": "s2", "content": "c2"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	t.Run("ref 缺失报错", func(t *testing.T) {
		_, err := rt.ResolveSelectedProposals("", sampleSubject(), []string{"p1"}, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "evolution_review_ref is required") {
			t.Fatalf("期望 ref 必填报错, got %v", err)
		}
	})

	t.Run("subject 不匹配报错", func(t *testing.T) {
		other := map[string]any{"kind": "skill", "name": "other"}
		_, err := rt.ResolveSelectedProposals(launch.EvolutionReviewRef, other, []string{"p1"}, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "evolution review subject mismatch") {
			t.Fatalf("期望 subject mismatch, got %v", err)
		}
	})

	t.Run("未完成态报错", func(t *testing.T) {
		launch2 := createTestScope(rt, "sess-1")
		_, err := rt.ResolveSelectedProposals(launch2.EvolutionReviewRef, sampleSubject(), []string{"p1"}, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "evolution review is not completed") {
			t.Fatalf("期望未完成报错, got %v", err)
		}
	})

	t.Run("选择 id 缺失/重复报错", func(t *testing.T) {
		_, err := rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), nil, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "selected_proposal_ids are required") {
			t.Fatalf("期望必填报错, got %v", err)
		}
		_, err = rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), []string{"p1", "p1"}, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "duplicate proposal id") {
			t.Fatalf("期望重复报错, got %v", err)
		}
	})

	t.Run("未知 proposal_id 报错", func(t *testing.T) {
		_, err := rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), []string{"pX"}, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "unknown proposal_id: pX") {
			t.Fatalf("期望未知 id 报错, got %v", err)
		}
	})

	t.Run("成功选择 + record_source 判定", func(t *testing.T) {
		sel, err := rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), []string{"p1", "p2"}, "sess-1")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(sel.Proposals) != 2 || len(sel.ExperienceDrafts) != 2 {
			t.Fatalf("选择结果数量不符: %+v", sel)
		}
		// p1 reason 取 proposal 层 r1；draft_for_execution 注入 reason
		if sel.Proposals[0].Reason != "r1" {
			t.Fatalf("p1 reason 期望 r1, got %s", sel.Proposals[0].Reason)
		}
		if sel.ExperienceDrafts[0]["reason"] != "r1" {
			t.Fatalf("draft_for_execution 期望注入 reason, got %v", sel.ExperienceDrafts[0])
		}
		// p2 无 proposal 层 reason，draft 亦无 → reason 空
		if sel.Proposals[1].Reason != "" {
			t.Fatalf("p2 reason 期望空, got %s", sel.Proposals[1].Reason)
		}
		// user_intent 空 → agent_inferred
		if sel.RecordSource != "agent_inferred" {
			t.Fatalf("期望 agent_inferred, got %s", sel.RecordSource)
		}
	})

	t.Run("user_intent 非空 → user_intent 来源", func(t *testing.T) {
		rt2 := newTestRuntime()
		launch2, _ := rt2.CreateScope("src", sampleSubject(), "sess-1", "想加部署脚本", nil)
		_ = rt2.RecordEvidenceRead(launch2.EvolutionReviewRef, "sess-1", []string{"ev1"})
		_, _ = rt2.RecordReviewResult(launch2.EvolutionReviewRef, "sess-1", map[string]any{
			"subject": sampleSubject(), "outcome": "no_evolution",
		})
		// no_evolution 态不能选择 → 先验证守卫
		_, err := rt2.ResolveSelectedProposals(launch2.EvolutionReviewRef, sampleSubject(), []string{"p1"}, "sess-1")
		if err == nil || !strings.Contains(err.Error(), "evolution review has no evolution proposals") {
			t.Fatalf("期望 no_evolution 守卫, got %v", err)
		}
		// user_intent 来源在 recommend 路径验证
		rt3 := newTestRuntime()
		launch3, _ := rt3.CreateScope("src", sampleSubject(), "sess-1", "意图", nil)
		_ = rt3.RecordEvidenceRead(launch3.EvolutionReviewRef, "sess-1", []string{"ev1"})
		_, _ = rt3.RecordReviewResult(launch3.EvolutionReviewRef, "sess-1", map[string]any{
			"subject": sampleSubject(), "outcome": "recommend_evolve",
			"evidence_refs": []any{"ev1"},
			"proposals": []any{map[string]any{
				"proposal_id": "p1",
				"experience":  map[string]any{"summary": "s", "content": "c"},
			}},
		})
		sel, err := rt3.ResolveSelectedProposals(launch3.EvolutionReviewRef, sampleSubject(), []string{"p1"}, "sess-1")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if sel.RecordSource != "user_intent" {
			t.Fatalf("期望 user_intent, got %s", sel.RecordSource)
		}
	})
}

// TestEvolutionReviewRuntime_SubmitLifecycle 测试提交与重放窗口裁剪。
// Python: consume_prepared_submission (runtime.py:279-289) + prune_scopes (runtime.py:86-95)
func TestEvolutionReviewRuntime_SubmitLifecycle(t *testing.T) {
	rt := NewEvolutionReviewRuntime(60.0)
	launch := createTestScope(rt, "sess-1")
	_ = rt.RecordEvidenceRead(launch.EvolutionReviewRef, "sess-1", []string{"ev1"})
	_, _ = rt.RecordReviewResult(launch.EvolutionReviewRef, "sess-1", map[string]any{
		"subject": sampleSubject(), "outcome": "recommend_evolve",
		"evidence_refs": []any{"ev1"},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience":  map[string]any{"summary": "s", "content": "c"},
		}},
	})
	sel, err := rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), []string{"p1"}, "sess-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if err := rt.ConsumePreparedSubmission(sel, "sess-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	scope, err := rt.ResolveScope(launch.EvolutionReviewRef, "sess-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if scope.Status != ScopeStatusSubmitted || scope.ConsumedAt == nil {
		t.Fatalf("期望 submitted 态: %+v", scope)
	}

	// 已提交后重复提交守卫
	_, err = rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), []string{"p1"}, "sess-1")
	if err == nil || !strings.Contains(err.Error(), "evolution review ref already submitted") {
		t.Fatalf("期望重复提交守卫, got %v", err)
	}

	// has_scope
	if !rt.HasScope(launch.EvolutionReviewRef) {
		t.Fatalf("期望 has_scope true")
	}
	if rt.HasScope("evrr_missing") {
		t.Fatalf("期望未知 ref has_scope false")
	}
}

// TestEvolutionReviewRuntime_PruneScopes 测试过期裁剪。
// Python: prune_scopes (runtime.py:86-95)
func TestEvolutionReviewRuntime_PruneScopes(t *testing.T) {
	// retention=0：submitted 且已消费的 scope 立即可裁剪
	rt := NewEvolutionReviewRuntime(0.0)
	launch := createTestScope(rt, "sess-1")
	_ = rt.RecordEvidenceRead(launch.EvolutionReviewRef, "sess-1", []string{"ev1"})
	_, _ = rt.RecordReviewResult(launch.EvolutionReviewRef, "sess-1", map[string]any{
		"subject": sampleSubject(), "outcome": "recommend_evolve",
		"evidence_refs": []any{"ev1"},
		"proposals": []any{map[string]any{
			"proposal_id": "p1",
			"experience":  map[string]any{"summary": "s", "content": "c"},
		}},
	})
	sel, _ := rt.ResolveSelectedProposals(launch.EvolutionReviewRef, sampleSubject(), []string{"p1"}, "sess-1")
	_ = rt.ConsumePreparedSubmission(sel, "sess-1")

	// retention=0 时 consumed_at <= now-retention(=now) 恒成立 → 裁剪
	rt.PruneScopes()
	if rt.HasScope(launch.EvolutionReviewRef) {
		t.Fatalf("期望 submitted scope 被裁剪")
	}

	// 未提交的 scope 不裁剪
	rt2 := NewEvolutionReviewRuntime(0.0)
	launch2 := createTestScope(rt2, "sess-1")
	rt2.PruneScopes()
	if !rt2.HasScope(launch2.EvolutionReviewRef) {
		t.Fatalf("期望未提交 scope 保留")
	}
}

// TestEvolutionReviewRuntime_NormalizeSubjectError 测试 subject 归一化错误透传。
// Python: _normalize_subject (runtime.py:264-268)
func TestEvolutionReviewRuntime_NormalizeSubjectError(t *testing.T) {
	rt := newTestRuntime()
	_, err := rt.CreateScope("src", "not-a-dict", "sess-1", "", nil)
	if err == nil || !strings.Contains(err.Error(), "subject must be an object") {
		t.Fatalf("期望 subject 报错透传, got %v", err)
	}
}
