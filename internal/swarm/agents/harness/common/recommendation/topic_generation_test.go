package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeStageInvoker 可编程的 StageInvoker 假实现（仅 mock 模型边界）
type fakeStageInvoker struct {
	responses []fakeStageResponse
	calls     []fakeStageCall
}

type fakeStageResponse struct {
	stage string
	value any
	err   error
}

type fakeStageCall struct {
	stage  string
	prompt string
	expect map[string]any // payload 中必须包含的键值对（存在才校验）
}

func (f *fakeStageInvoker) invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
	for _, r := range f.responses {
		if r.stage == stage {
			f.calls = append(f.calls, fakeStageCall{stage: stage, prompt: systemPrompt})
			if r.err != nil {
				return nil, r.err
			}
			return r.value, nil
		}
	}
	return nil, errors.New("no scripted response for stage " + stage)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

// rankPayload 构造合法的 I2 响应
func rankPayload(pairs ...[2]any) map[string]any {
	ranked := []any{}
	for _, p := range pairs {
		ranked = append(ranked, map[string]any{
			"interest_summary_id": p[0],
			"rank":                p[1],
			"api_score_nullable":  nil,
		})
	}
	return map[string]any{"ranked_interests": ranked}
}

func snapshotOf(ids ...string) []map[string]any {
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"interest_summary_id": id})
	}
	return out
}

func TestRankInterestSummaries_正常排序(t *testing.T) {
	inv := &fakeStageInvoker{responses: []fakeStageResponse{
		{stage: "interest_ranker", value: rankPayload([2]any{"a", 1}, [2]any{"b", 2})},
	}}
	ranked, reason := RankInterestSummaries(context.Background(),
		snapshotOf("a", "b"), nil, map[string]any{},
		func(ctx context.Context, payload map[string]any) (any, error) {
			return inv.invoke(ctx, "interest_ranker", "", mustJSON(t, payload))
		})
	if reason != "" {
		t.Fatalf("unexpected reason %q", reason)
	}
	if len(ranked) != 2 || ranked[0].InterestSummaryID != "a" || ranked[0].Rank != 1 ||
		ranked[1].InterestSummaryID != "b" || ranked[1].Rank != 2 {
		t.Fatalf("unexpected ranked %+v", ranked)
	}
}

func TestRankInterestSummaries_快照含非法兴趣ID拒绝(t *testing.T) {
	// Python: interest_id 非 str 或空串 → contract_failed
	snap := snapshotOf("a")
	snap[0]["interest_summary_id"] = 42
	_, reason := RankInterestSummaries(context.Background(), snap, nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil })
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_重复兴趣ID拒绝(t *testing.T) {
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a", "a"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil })
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_适配器失败返回失败原因(t *testing.T) {
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return nil, errors.New("boom")
		})
	if reason != "interest_ranking_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_响应缺键拒绝(t *testing.T) {
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"ranked_interests": []any{}, "extra": 1}, nil
		})
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_秩不连续拒绝(t *testing.T) {
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a", "b"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return rankPayload([2]any{"a", 1}, [2]any{"b", 3}), nil
		})
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_布尔秩拒绝(t *testing.T) {
	// Python: _plain_int 拒绝 bool（bool 是 int 子类）
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"ranked_interests": []any{map[string]any{
				"interest_summary_id": "a", "rank": true, "api_score_nullable": nil,
			}}}, nil
		})
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_集合不一致拒绝(t *testing.T) {
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a", "b"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return rankPayload([2]any{"a", 1}, [2]any{"c", 2}), nil
		})
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestRankInterestSummaries_可观测分数透传(t *testing.T) {
	inv := &fakeStageInvoker{responses: []fakeStageResponse{
		{stage: "interest_ranker", value: map[string]any{"ranked_interests": []any{
			map[string]any{"interest_summary_id": "a", "rank": 1, "api_score_nullable": 0.75},
			map[string]any{"interest_summary_id": "b", "rank": 2, "api_score_nullable": nil},
		}}},
	}}
	ranked, reason := RankInterestSummaries(context.Background(),
		snapshotOf("a", "b"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return inv.invoke(ctx, "interest_ranker", "", mustJSON(t, payload))
		})
	if reason != "" {
		t.Fatalf("reason=%q", reason)
	}
	if ranked[0].APIScoreNullable == nil || *ranked[0].APIScoreNullable != 0.75 {
		t.Fatalf("score=%v", ranked[0].APIScoreNullable)
	}
	if ranked[1].APIScoreNullable != nil {
		t.Fatalf("score=%v", ranked[1].APIScoreNullable)
	}
}

func TestRankInterestSummaries_非有限分数拒绝(t *testing.T) {
	_, reason := RankInterestSummaries(context.Background(), snapshotOf("a"), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"ranked_interests": []any{map[string]any{
				"interest_summary_id": "a", "rank": 1, "api_score_nullable": "x",
			}}}, nil
		})
	if reason != "interest_ranking_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestArkAdapters_阶段与提示词逐字一致(t *testing.T) {
	// 对齐 Python：三个适配器分别以固定 stage + 系统 prompt 调用 invoke_stage
	var gotStage, gotPrompt string
	adapter := NewArkInterestRankingAdapter(
		func(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
			gotStage, gotPrompt = stage, systemPrompt
			return map[string]any{}, nil
		})
	if _, err := adapter.Invoke(context.Background(), map[string]any{"k": 1}); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if gotStage != "interest_ranker" {
		t.Fatalf("stage=%q", gotStage)
	}
	if gotPrompt != i2SystemPrompt {
		t.Fatalf("prompt mismatch:\n%q\nvs\n%q", gotPrompt, i2SystemPrompt)
	}
	gen := NewArkCandidateGenerationAdapter(
		func(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
			gotStage = stage
			return map[string]any{}, nil
		})
	_, _ = gen.Invoke(context.Background(), nil)
	if gotStage != "starter_candidate_generator" {
		t.Fatalf("stage=%q", gotStage)
	}
	sel := NewArkSlateSelectionAdapter(
		func(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
			gotStage = stage
			return map[string]any{}, nil
		})
	_, _ = sel.Invoke(context.Background(), nil)
	if gotStage != "starter_slate_selector" {
		t.Fatalf("stage=%q", gotStage)
	}
}

// candidateOf 构造一个满足 _CANDIDATE_FIELDS 全字段且语义合法的暖启动个性化候选
func candidateOf(candidateID, lane, interestID string) map[string]any {
	return map[string]any{
		"candidate_id":           candidateID,
		"lane":                   lane,
		"intent":                 "intent-" + candidateID,
		"persona":                "persona",
		"provenance_relation":    "stable_progress",
		"source_memory_unit_ids": []any{interestID},
		"source_locators":        []any{[]any{"s1", 1}},
		"identity_anchor":        "anchor",
		"context_anchors":        []any{},
		"bridge":                 map[string]any{},
		"primary_query":          "query " + candidateID,
		"fallback_query":         "fallback " + candidateID,
		"freshness_class":        "evergreen_corpus",
		"source_requirement":     "web_required",
		"title":                  "title " + candidateID,
		"origin":                 "generated",
		"source_starter_id":      nil,
		"interest_summary_id":    interestID,
	}
}

func candidateContext(interestID string) map[string]any {
	return map[string]any{
		"interest_summary_id":    interestID,
		"interest_summary":       "summary",
		"rank":                   1,
		"source_memory_unit_ids": []any{interestID},
		"source_locators":        []any{[]any{"s1", 1}},
		"allowed_anchors":        []any{"anchor"},
		"provenance_relation":    "stable_progress",
	}
}

// genericCandidateOf 泛化通道候选（fresh-trend/generic-*：无个性化溯源，对齐 _valid_candidate_semantics 泛化分支）
func genericCandidateOf(candidateID, lane string) map[string]any {
	c := candidateOf(candidateID, lane, "")
	c["interest_summary_id"] = nil
	c["source_memory_unit_ids"] = []any{}
	c["source_locators"] = []any{}
	c["identity_anchor"] = ""
	c["context_anchors"] = []any{}
	c["provenance_relation"] = "generic"
	return c
}

func warmCandidates() []map[string]any {
	out := []map[string]any{}
	ranked := []string{}
	for i := 0; i < 8; i++ {
		id := "i" + string(rune('a'+i))
		ranked = append(ranked, id)
		out = append(out, candidateOf("c"+string(rune('a'+i)), "personalized-intent", id))
	}
	for i := 0; i < 2; i++ {
		out = append(out, candidateOf("p"+string(rune('a'+i)), "perspective-expansion", ranked[i]))
		// perspective-expansion 桥字段三键
		out[len(out)-1]["bridge"] = map[string]any{"source": "s", "shared_property": "p", "target": "t"}
	}
	for i := 0; i < 2; i++ {
		out = append(out, genericCandidateOf("t"+string(rune('a'+i)), "fresh-trend"))
	}
	return out
}

func TestGenerateStarterCandidates_暖启动合法池通过(t *testing.T) {
	candidates := warmCandidates()
	ranked := []RankedInterest{}
	for _, c := range candidates {
		if c["lane"] == "personalized-intent" {
			ranked = append(ranked, RankedInterest{InterestSummaryID: c["interest_summary_id"].(string), Rank: 1})
		}
	}
	contextByID := map[string]map[string]any{}
	for _, c := range candidates {
		if id, ok := c["interest_summary_id"].(string); ok && id != "" {
			contextByID[id] = candidateContext(id)
		}
	}
	_, reason := GenerateStarterCandidates(context.Background(), ranked, nil, map[string]any{},
		false,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": anySliceOf(candidates)}, nil
		}, 12, contextByID)
	if reason != "" {
		t.Fatalf("reason=%q", reason)
	}
}

func anySliceOf(maps []map[string]any) []any {
	out := make([]any, 0, len(maps))
	for _, m := range maps {
		out = append(out, m)
	}
	return out
}

func TestGenerateStarterCandidates_目标数不为12拒绝(t *testing.T) {
	_, reason := GenerateStarterCandidates(context.Background(), nil, nil, nil, false,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil },
		8, nil)
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_上下文键集合不一致拒绝(t *testing.T) {
	_, reason := GenerateStarterCandidates(context.Background(),
		[]RankedInterest{{InterestSummaryID: "a", Rank: 1}}, nil, nil, false,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil },
		12, map[string]map[string]any{"b": candidateContext("b")})
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_候选数不足12拒绝(t *testing.T) {
	_, reason := GenerateStarterCandidates(context.Background(), nil, nil, nil, false,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": []any{}}, nil
		}, 12, nil)
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_形状缺字段拒绝(t *testing.T) {
	bad := candidateOf("c1", "personalized-intent", "i")
	delete(bad, "persona")
	_, reason := GenerateStarterCandidates(context.Background(),
		[]RankedInterest{{InterestSummaryID: "i", Rank: 1}}, nil, nil, false,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": []any{bad}}, nil
		}, 12, map[string]map[string]any{"i": candidateContext("i")})
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_重复候选ID拒绝(t *testing.T) {
	a := candidateOf("c1", "personalized-intent", "i")
	b := candidateOf("c1", "personalized-intent", "i")
	_, reason := GenerateStarterCandidates(context.Background(),
		[]RankedInterest{{InterestSummaryID: "i", Rank: 1}}, nil, nil, false,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": []any{a, b}}, nil
		}, 12, map[string]map[string]any{"i": candidateContext("i")})
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_槽位计数不符拒绝(t *testing.T) {
	// 8 personalized + 3 perspective + 1 trend = 计数错
	candidates := warmCandidates()
	for _, c := range candidates {
		if c["lane"] == "perspective-expansion" {
			c["lane"] = "fresh-trend"
		}
	}
	ranked := []RankedInterest{{InterestSummaryID: "ia", Rank: 1}}
	_, reason := GenerateStarterCandidates(context.Background(), ranked, nil, nil, false,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": anySliceOf(candidates)}, nil
		}, 12, map[string]map[string]any{"ia": candidateContext("ia")})
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_泛化通道非空溯源拒绝(t *testing.T) {
	// 冷启动下 generic-intent 候选带 interest_summary_id → 语义非法
	c := candidateOf("g1", "generic-intent", "i")
	c["provenance_relation"] = "generic"
	ranked := []RankedInterest{{InterestSummaryID: "i", Rank: 1}}
	_, reason := GenerateStarterCandidates(context.Background(), ranked, nil, nil, true,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": []any{c}}, nil
		}, 12, nil)
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestGenerateStarterCandidates_固定命中数量不符拒绝(t *testing.T) {
	// 响应中 retrieved-fixed 候选数 != fixed_hits 数
	hit := map[string]any{
		"starter_id": "s1", "text": "hello", "interest_summary_id": nil,
		"source_memory_unit_ids": []any{}, "source_locators": []any{},
		"identity_anchor": "", "context_anchors": []any{},
		"freshness_class": "evergreen_corpus",
	}
	fixed := candidateOf("f1", "generic-intent", "")
	fixed["origin"] = "retrieved-fixed"
	fixed["source_starter_id"] = "s1"
	fixed["title"] = "hello"
	fixed["provenance_relation"] = "generic"
	fixed["source_memory_unit_ids"] = []any{}
	fixed["source_locators"] = []any{}
	fixed["identity_anchor"] = ""
	fixed["context_anchors"] = []any{}
	extra := candidateOf("g1", "generic-intent", "")
	extra["origin"] = "generated"
	extra["provenance_relation"] = "generic"
	ranked := []RankedInterest{{InterestSummaryID: "i", Rank: 1}}
	_, reason := GenerateStarterCandidates(context.Background(), ranked, []map[string]any{hit}, nil, true,
		func(ctx context.Context, payload map[string]any) (any, error) {
			// 返回两个 fixed（fixed_hits 只有 1 个）
			return map[string]any{"candidates": []any{fixed, extra, cloneCandidate(fixed, "f2"), cloneCandidate(extra, "g2")}}, nil
		}, 12, nil)
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func cloneCandidate(src map[string]any, id string) map[string]any {
	dst := map[string]any{}
	for k, v := range src {
		dst[k] = v
	}
	dst["candidate_id"] = id
	return dst
}

func TestSelectStarterSlate_正常选择去重趋势(t *testing.T) {
	// Python :select_starter_slate 尾段：第二个 fresh-trend 被丢弃
	g1 := candidateOf("c1", "personalized-intent", "i1")
	g2 := candidateOf("c2", "personalized-intent", "i2")
	g2["identity_anchor"] = "anchor2" // 与 g1 锚点不同，避免个性化去重丢弃
	t1 := genericCandidateOf("t1", "fresh-trend")
	t2 := genericCandidateOf("t2", "fresh-trend")
	grounded := []map[string]any{g1, g2, t1, t2}
	resp := map[string]any{"selections": []any{
		map[string]any{"candidate_id": "t1", "selector_rank": 1, "api_score_nullable": nil},
		map[string]any{"candidate_id": "t2", "selector_rank": 2, "api_score_nullable": nil},
		map[string]any{"candidate_id": "c1", "selector_rank": 3, "api_score_nullable": nil},
		map[string]any{"candidate_id": "c2", "selector_rank": 4, "api_score_nullable": nil},
	}}
	sels, reason := SelectStarterSlate(context.Background(), grounded,
		map[string]any{},
		func(ctx context.Context, payload map[string]any) (any, error) { return resp, nil }, 4)
	if reason != "" {
		t.Fatalf("reason=%q", reason)
	}
	if len(sels) != 3 {
		t.Fatalf("selections=%d", len(sels))
	}
	if sels[0].CandidateID != "t1" || sels[1].CandidateID != "c1" || sels[2].CandidateID != "c2" {
		t.Fatalf("unexpected %+v", sels)
	}
}

func TestSelectStarterSlate_目标数不为4拒绝(t *testing.T) {
	_, reason := SelectStarterSlate(context.Background(), nil, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return nil, nil }, 5)
	if reason != "slate_selection_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestSelectStarterSlate_未知候选ID拒绝(t *testing.T) {
	resp := map[string]any{"selections": []any{
		map[string]any{"candidate_id": "ghost", "selector_rank": 1, "api_score_nullable": nil},
	}}
	_, reason := SelectStarterSlate(context.Background(), []map[string]any{candidateOf("c1", "personalized-intent", "i")}, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return resp, nil }, 4)
	if reason != "slate_selection_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestSelectStarterSlate_重复候选ID拒绝(t *testing.T) {
	resp := map[string]any{"selections": []any{
		map[string]any{"candidate_id": "c1", "selector_rank": 1, "api_score_nullable": nil},
		map[string]any{"candidate_id": "c1", "selector_rank": 2, "api_score_nullable": nil},
	}}
	_, reason := SelectStarterSlate(context.Background(), []map[string]any{candidateOf("c1", "personalized-intent", "i")}, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return resp, nil }, 4)
	if reason != "slate_selection_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestSelectStarterSlate_选择数超上限拒绝(t *testing.T) {
	resp := map[string]any{"selections": []any{}}
	for i := 0; i < 5; i++ {
		resp["selections"] = append(resp["selections"].([]any),
			map[string]any{"candidate_id": "c" + string(rune('a'+i)), "selector_rank": i + 1, "api_score_nullable": nil})
	}
	grounded := []map[string]any{}
	for i := 0; i < 5; i++ {
		grounded = append(grounded, candidateOf("c"+string(rune('a'+i)), "personalized-intent", "i"))
	}
	_, reason := SelectStarterSlate(context.Background(), grounded, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return resp, nil }, 4)
	if reason != "slate_selection_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestSelectStarterSlate_有固定命中但被丢弃拒绝(t *testing.T) {
	// grounded 含 retrieved-fixed，但过滤后 kept 无 retrieved-fixed → contract_failed
	fixed := candidateOf("f1", "personalized-intent", "i")
	fixed["origin"] = "retrieved-fixed"
	fixed["source_starter_id"] = "s1"
	gen := candidateOf("c1", "personalized-intent", "i")
	resp := map[string]any{"selections": []any{
		map[string]any{"candidate_id": "c1", "selector_rank": 1, "api_score_nullable": nil},
		map[string]any{"candidate_id": "f1", "selector_rank": 2, "api_score_nullable": nil},
	}}
	// 两候选同 bucket+anchor：选择序 gen 在前 → fixed 被去重丢弃 → contract_failed
	_, reason := SelectStarterSlate(context.Background(), []map[string]any{fixed, gen}, nil,
		func(ctx context.Context, payload map[string]any) (any, error) { return resp, nil }, 4)
	if reason != "slate_selection_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestFiniteNullableScore_各类值(t *testing.T) {
	valid, score := finiteNullableScore(nil)
	if !valid || score != nil {
		t.Fatalf("nil → %v %v", valid, score)
	}
	valid, score = finiteNullableScore(3)
	if !valid || score == nil || *score != 3 {
		t.Fatalf("int → %v %v", valid, score)
	}
	valid, _ = finiteNullableScore(true)
	if valid {
		t.Fatal("bool 应拒绝")
	}
	valid, _ = finiteNullableScore("x")
	if valid {
		t.Fatal("string 应拒绝")
	}
}

func TestPlainInt(t *testing.T) {
	if !plainInt(3) {
		t.Fatal("3 应为 plain int")
	}
	if plainInt(true) {
		t.Fatal("bool 不是 plain int")
	}
	if plainInt(3.5) {
		t.Fatal("float 不是 plain int")
	}
}

func TestGenerateStarterCandidates_冷启动带固定命中通过(t *testing.T) {
	// 对齐 Python：retrieved_fixed_hits 是不可变前缀，fixed 候选逐字段拷贝
	hit := map[string]any{
		"starter_id": "s1", "text": "hello", "interest_summary_id": nil,
		"source_memory_unit_ids": []any{}, "source_locators": []any{},
		"identity_anchor": "", "context_anchors": []any{},
		"freshness_class": "evergreen_corpus",
	}
	fixed := genericCandidateOf("f1", "generic-intent")
	fixed["origin"] = "retrieved-fixed"
	fixed["source_starter_id"] = "s1"
	fixed["title"] = "hello"
	fixed["freshness_class"] = "evergreen_corpus"
	candidates := []map[string]any{fixed}
	for i := 0; i < 7; i++ {
		candidates = append(candidates, genericCandidateOf("g"+string(rune('b'+i)), "generic-intent"))
	}
	for i := 0; i < 2; i++ {
		// 对齐 _valid_candidate_semantics：generic-perspective 桥必须为空
		candidates = append(candidates, genericCandidateOf("p"+string(rune('a'+i)), "generic-perspective"))
	}
	for i := 0; i < 2; i++ {
		candidates = append(candidates, genericCandidateOf("t"+string(rune('a'+i)), "fresh-trend"))
	}
	got, reason := GenerateStarterCandidates(context.Background(), nil, []map[string]any{hit},
		map[string]any{}, true,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": anySliceOf(candidates)}, nil
		}, 12, nil)
	if reason != "" {
		t.Fatalf("reason=%q", reason)
	}
	if len(got) != 12 || got[0]["origin"] != "retrieved-fixed" {
		t.Fatalf("got=%d", len(got))
	}
}

func TestGenerateStarterCandidates_固定命中与候选不匹配拒绝(t *testing.T) {
	hit := map[string]any{
		"starter_id": "s1", "text": "hello", "interest_summary_id": nil,
		"source_memory_unit_ids": []any{}, "source_locators": []any{},
		"identity_anchor": "", "context_anchors": []any{},
		"freshness_class": "evergreen_corpus",
	}
	fixed := genericCandidateOf("f1", "generic-intent")
	fixed["origin"] = "retrieved-fixed"
	fixed["source_starter_id"] = "s1"
	fixed["title"] = "REWRITTEN" // 与 hit.text 不一致 → 前缀匹配失败
	candidates := []map[string]any{fixed}
	for i := 0; i < 7; i++ {
		candidates = append(candidates, genericCandidateOf("g"+string(rune('b'+i)), "generic-intent"))
	}
	for i := 0; i < 2; i++ {
		// 对齐 _valid_candidate_semantics：generic-perspective 桥必须为空
		candidates = append(candidates, genericCandidateOf("p"+string(rune('a'+i)), "generic-perspective"))
	}
	for i := 0; i < 2; i++ {
		candidates = append(candidates, genericCandidateOf("t"+string(rune('a'+i)), "fresh-trend"))
	}
	_, reason := GenerateStarterCandidates(context.Background(), nil, []map[string]any{hit},
		nil, true,
		func(ctx context.Context, payload map[string]any) (any, error) {
			return map[string]any{"candidates": anySliceOf(candidates)}, nil
		}, 12, nil)
	if reason != "candidate_generation_contract_failed" {
		t.Fatalf("reason=%q", reason)
	}
}

func TestValidCandidateShape_负向分支(t *testing.T) {
	base := candidateOf("c", "personalized-intent", "i")
	// 非法 source_locators 长度
	c := cloneCandidate(base, "c")
	c["source_locators"] = []any{[]any{"s1"}}
	if validCandidateShape(c) {
		t.Fatal("locator 长度 1 应拒绝")
	}
	// 负序号 locator
	c = cloneCandidate(base, "c")
	c["source_locators"] = []any{[]any{"s1", -1}}
	if validCandidateShape(c) {
		t.Fatal("负序号应拒绝")
	}
	// 空 source_memory_unit_ids 元素
	c = cloneCandidate(base, "c")
	c["source_memory_unit_ids"] = []any{""}
	if validCandidateShape(c) {
		t.Fatal("空 bucket 应拒绝")
	}
	// 空 context_anchors 元素
	c = cloneCandidate(base, "c")
	c["context_anchors"] = []any{""}
	if validCandidateShape(c) {
		t.Fatal("空 anchor 应拒绝")
	}
	// 非 str 的 source_starter_id
	c = cloneCandidate(base, "c")
	c["source_starter_id"] = 3
	if validCandidateShape(c) {
		t.Fatal("非 str starter id 应拒绝")
	}
	// 非 web_required
	c = cloneCandidate(base, "c")
	c["source_requirement"] = "local"
	if validCandidateShape(c) {
		t.Fatal("非 web_required 应拒绝")
	}
	// 非法 freshness
	c = cloneCandidate(base, "c")
	c["freshness_class"] = "stale"
	if validCandidateShape(c) {
		t.Fatal("非法 freshness 应拒绝")
	}
	// bridge 非 dict
	c = cloneCandidate(base, "c")
	c["bridge"] = "x"
	if validCandidateShape(c) {
		t.Fatal("bridge 非 dict 应拒绝")
	}
	if validCandidateShape(nil) {
		t.Fatal("nil 应拒绝")
	}
}

func TestValidCandidateSemantics_桥字段非法(t *testing.T) {
	ranked := map[string]struct{}{"i": {}}
	c := candidateOf("p1", "perspective-expansion", "i")
	c["bridge"] = map[string]any{"source": "s", "shared_property": "p"} // 缺 target
	if validCandidateSemantics(c, false, ranked, map[string]map[string]any{"i": candidateContext("i")}) {
		t.Fatal("缺 target 应拒绝")
	}
	c2 := candidateOf("p1", "perspective-expansion", "i")
	c2["bridge"] = map[string]any{"source": "s", "shared_property": "p", "target": ""}
	if validCandidateSemantics(c2, false, ranked, map[string]map[string]any{"i": candidateContext("i")}) {
		t.Fatal("空 target 应拒绝")
	}
}

func TestHelpersEdge(t *testing.T) {
	if marshalCompactPayload(map[string]any{"bad": make(chan int)}) != "null" {
		t.Fatal("不可序列化 payload 应返回 null")
	}
	if anyToMaps([]any{map[string]any{"a": 1}}) == nil {
		t.Fatal("anyToMaps 应提取 map")
	}
	if anyToMaps([]any{1}) == nil || len(anyToMaps([]any{1})) != 0 {
		t.Fatal("非 map 元素应被跳过")
	}
	if !anyScalarEqual("a", "a") || anyScalarEqual("a", "b") {
		t.Fatal("string 比较错误")
	}
	if !anyScalarEqual(nil, nil) || anyScalarEqual(nil, "x") {
		t.Fatal("nil 比较错误")
	}
	if !anyScalarEqual(true, true) || anyScalarEqual(true, false) {
		t.Fatal("bool 比较错误")
	}
	if intOf(3.9) != 3 {
		t.Fatal("float 截断错误")
	}
	if !intMapsEqual(map[string]int{"a": 1}, map[string]int{"a": 1}) ||
		intMapsEqual(map[string]int{"a": 1}, map[string]int{"a": 2}) ||
		intMapsEqual(map[string]int{"a": 1}, map[string]int{}) {
		t.Fatal("int map 比较错误")
	}
	if nullableFloatValue(nil) != nil {
		t.Fatal("nil 指针应返回 nil")
	}
}

func TestValidCandidateSemantics_负向分支补齐(t *testing.T) {
	ranked := map[string]struct{}{"i": {}}
	// 泛化通道带 identity_anchor → 拒绝
	c := genericCandidateOf("g1", "fresh-trend")
	c["identity_anchor"] = "x"
	if validCandidateSemantics(c, false, ranked, nil) {
		t.Fatal("泛化通道带 anchor 应拒绝")
	}
	// 非泛化 + 上下文缺该兴趣 → 拒绝
	c2 := candidateOf("c1", "personalized-intent", "i")
	if validCandidateSemantics(c2, false, ranked, map[string]map[string]any{}) {
		t.Fatal("缺上下文应拒绝")
	}
	// identity_anchor 在 context_anchors 内 → 拒绝
	ctxI := candidateContext("i")
	ctxI["context_anchors"] = []any{"anchor", "anchor"}
	c3 := candidateOf("c1", "personalized-intent", "i")
	c3["context_anchors"] = []any{"anchor"}
	if validCandidateSemantics(c3, false, ranked, map[string]map[string]any{"i": ctxI}) {
		t.Fatal("identity 与 context 重叠应拒绝")
	}
	// provenance 与上下文不一致 → 拒绝
	c4 := candidateOf("c1", "personalized-intent", "i")
	c4["provenance_relation"] = "recent_follow_up"
	if validCandidateSemantics(c4, false, ranked, map[string]map[string]any{"i": candidateContext("i")}) {
		t.Fatal("provenance 不一致应拒绝")
	}
	// locator 不在允许集 → 拒绝
	c5 := candidateOf("c1", "personalized-intent", "i")
	c5["source_locators"] = []any{[]any{"s9", 9}}
	if validCandidateSemantics(c5, false, ranked, map[string]map[string]any{"i": candidateContext("i")}) {
		t.Fatal("locator 越界应拒绝")
	}
	// source_memory_unit_ids 与上下文不一致 → 拒绝
	c6 := candidateOf("c1", "personalized-intent", "i")
	c6["source_memory_unit_ids"] = []any{"other"}
	if validCandidateSemantics(c6, false, ranked, map[string]map[string]any{"i": candidateContext("i")}) {
		t.Fatal("bucket 不一致应拒绝")
	}
}

func TestHelpersEdge2(t *testing.T) {
	if !anySliceEqual("a", "a") || anySliceEqual("a", "b") {
		t.Fatal("非列表经 fmt 路径比较")
	}
	if anySliceEqual([]any{1, 2}, []any{1}) || anySliceEqual([]any{1}, []any{2}) {
		t.Fatal("列表比较错误")
	}
	if nullableFloatValue(floatPtr(1.5)) != 1.5 {
		t.Fatal("非 nil 指针取值错误")
	}
	if intOf("x") != 0 {
		t.Fatal("默认分支应为 0")
	}
}
