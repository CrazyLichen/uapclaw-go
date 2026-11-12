package recommendation

import (
	"strings"
	"testing"
)

// 合法边界与摘要的测试工厂
type errTestProvider struct{}

func (errTestProvider) Error() string { return "provider down" }

var errTestProviderInstance = errTestProvider{}

func testBoundary(sessionID string, ordinal int, completedAt float64) *SessionCompletionBoundary {
	b, err := NewSessionCompletionBoundary(sessionID, sessionID+":evt:last", "req-1", ordinal, completedAt, "success")
	if err != nil {
		panic(err)
	}
	return b
}

func testSummary(t *testing.T, sessionID string, ordinal int, text string) *SessionSummaryArtifact {
	t.Helper()
	locator, err := NewSessionSourceLocator(sessionID, ordinal, sessionID+":evt:0")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := NewSessionSummaryArtifact(
		"summary-"+sessionID, sessionID, "global_new_chat", text,
		"manual_free_text", "none",
		[]string{sessionID + ":evt:0"}, []*SessionSourceLocator{locator},
		testBoundary(sessionID, ordinal+1, 1000), 1000,
		"prov", "v1", "cfg",
	)
	if err != nil {
		t.Fatalf("构造摘要: %v", err)
	}
	return summary
}

func TestSessionCompletionBoundary_校验(t *testing.T) {
	if _, err := NewSessionCompletionBoundary("", "e", "r", 0, 1, "success"); err == nil {
		t.Fatal("空 session_id 拒绝（对齐 :78-88）")
	}
	if _, err := NewSessionCompletionBoundary("s", "e", "r", 0, 1, "pending"); err == nil {
		t.Fatal("非终态 status 拒绝（对齐 :88 白名单）")
	}
	if _, err := NewSessionCompletionBoundary("s", "e", "r", 0, -1, "success"); err == nil {
		t.Fatal("负时间戳拒绝")
	}
	// 白名单全成员（对齐 session_history.py:40-47）
	for _, status := range []string{"success", "error", "cancelled", "failed", "waiting_user", "unknown"} {
		if _, err := NewSessionCompletionBoundary("s", "e", "r", 0, 1, status); err != nil {
			t.Fatalf("终态 %s 应合法: %v", status, err)
		}
	}
}

func TestSessionSourceLocator_校验与往返(t *testing.T) {
	if _, err := NewSessionSourceLocator("s", -1, "e"); err == nil {
		t.Fatal("负 ordinal 拒绝（对齐 :281-290）")
	}
	l, err := NewSessionSourceLocator("s", 3, "e")
	if err != nil {
		t.Fatal(err)
	}
	// 对齐 :302-305 键集合全等
	payload := l.AsPayload()
	if _, err := SessionSourceLocatorFromPayload(payload); err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	payload["extra"] = 1
	if _, err := SessionSourceLocatorFromPayload(payload); err == nil {
		t.Fatal("多余键必须拒绝")
	}
}

func TestSessionSummaryArtifact_校验矩阵(t *testing.T) {
	locator, _ := NewSessionSourceLocator("s1", 0, "s1:evt:0")
	// 空 source_event_ids 拒绝（对齐 :357-363）
	if _, err := NewSessionSummaryArtifact("id", "s1", "global_new_chat", "text", "manual_free_text", "none",
		nil, nil, testBoundary("s1", 1, 1000), 1000, "p", "v", "c"); err == nil {
		t.Fatal("空 source_event_ids 拒绝")
	}
	// locator 与事件不对应拒绝（对齐 :364-386）
	if _, err := NewSessionSummaryArtifact("id", "s1", "global_new_chat", "text", "manual_free_text", "none",
		[]string{"s1:evt:1"}, []*SessionSourceLocator{locator}, testBoundary("s1", 1, 1000), 1000, "p", "v", "c"); err == nil {
		t.Fatal("locator event_id 与 source_event_ids 不对应拒绝")
	}
	// locator 越过完结边界拒绝（对齐 :387-404）
	if _, err := NewSessionSummaryArtifact("id", "s1", "global_new_chat", "text", "manual_free_text", "none",
		[]string{"s1:evt:0"}, []*SessionSourceLocator{locator}, testBoundary("s1", 0, 1000), 1000, "p", "v", "c"); err == nil {
		t.Fatal("locator.ordinal >= boundary.ordinal 拒绝")
	}
	// 合法构造
	if _, err := NewSessionSummaryArtifact("id", "s1", "global_new_chat", "text", "manual_free_text", "none",
		[]string{"s1:evt:0"}, []*SessionSourceLocator{locator}, testBoundary("s1", 1, 1000), 1000, "p", "v", "c"); err != nil {
		t.Fatalf("合法构造失败: %v", err)
	}
}

func TestSessionSummaryArtifact_往返与投影复核(t *testing.T) {
	artifact := testSummary(t, "s1", 0, "学了指弹吉他")
	payload := artifact.AsPayload()
	if payload["natural_revisit_eligible"] != true {
		t.Fatal("manual_free_text + none 必须自然回访合格（对齐 :426-430）")
	}
	round, err := SessionSummaryArtifactFromPayload(payload)
	if err != nil {
		t.Fatalf("往返失败: %v", err)
	}
	if round.SessionSummaryID != artifact.SessionSummaryID || len(round.SourceLocators) != 1 {
		t.Fatal("往返字段丢失")
	}
	// eligibility 投影复核（对齐 :499-502）
	payload["natural_revisit_eligible"] = false
	if _, err := SessionSummaryArtifactFromPayload(payload); err == nil {
		t.Fatal("eligibility 投影不符必须拒绝")
	}
	payload["natural_revisit_eligible"] = true
	payload["extra_key"] = 1
	if _, err := SessionSummaryArtifactFromPayload(payload); err == nil {
		t.Fatal("多余键必须拒绝（15 键全等，对齐 :467-483）")
	}
}

func TestLongTermProfileInterestInput_校验(t *testing.T) {
	if _, err := NewLongTermProfileInterestInput("i1", "project:abc", 1, "c", "t", []string{"u1"}); err == nil {
		t.Fatal("非 global scope 拒绝（对齐 :531-533）")
	}
	if _, err := NewLongTermProfileInterestInput("i1", "global_new_chat", 0, "c", "t", []string{"u1"}); err == nil {
		t.Fatal("语义版本 <1 拒绝")
	}
	i, err := NewLongTermProfileInterestInput("i1", "global_new_chat", 3, "cluster", "text", []string{"u1", "u2"})
	if err != nil {
		t.Fatal(err)
	}
	payload := i.AsPayload()
	if payload["source_kind"] != "long_term_profile" {
		t.Fatal("source_kind 必须为 long_term_profile（对齐 :567）")
	}
}

func TestRIDAPISystemPrompt_golden(t *testing.T) {
	// 提示词版本插值 + 输出契约行逐字在位
	if !strings.Contains(ridAPISystemPrompt, "(topic-starter-v6-rid-api-proxy-v20)") {
		t.Fatal("版本插值缺失")
	}
	if !strings.Contains(ridAPISystemPrompt, `"decisions":[{"interest_summary_id":"..."`) {
		t.Fatal("输出契约行缺失")
	}
	// 行数核对（机器核对：python 提取 f-string 主体 wc -l = 95，防截断）
	if got := strings.Count(ridAPISystemPrompt, "\n"); got != 95 {
		t.Fatalf("提示词行数漂移: %d（应为 95）", got)
	}
}

func TestLearnedComponentBinding_与Receipt校验(t *testing.T) {
	if _, err := NewLearnedComponentBinding("bogus", "p", "v", "c"); err == nil {
		t.Fatal("非法 component_id 拒绝（对齐 :189-191）")
	}
	binding, err := NewLearnedComponentBinding(RID_COMPONENT_ID, "ark", "v1", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewLearnedComponentReceipt(binding, "snap", "req", "not_scored", "success", ""); err == nil {
		t.Fatal("success 必须 uncalibrated_api_proxy（对齐 :246-254）")
	}
	if _, err := NewLearnedComponentReceipt(binding, "snap", "req", "uncalibrated_api_proxy", "success", "why"); err == nil {
		t.Fatal("success 禁止 fallback_reason")
	}
	if _, err := NewLearnedComponentReceipt(binding, "snap", "req", "not_scored", "failed", ""); err == nil {
		t.Fatal("非 success 必须 fallback_reason")
	}
	r, err := NewLearnedComponentReceipt(binding, "snap", "req", "uncalibrated_api_proxy", "success", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.AsPayload()["receipt_version"] != COMPONENT_RECEIPT_SCHEMA_VERSION {
		t.Fatal("receipt_version 不符")
	}
}

func TestDistillTriggerInterests_输入契约(t *testing.T) {
	distiller := func(ctx any, payload map[string]any) (any, error) {
		return map[string]any{"decisions": []any{}}, nil
	}
	binding, _ := NewLearnedComponentBinding(RID_COMPONENT_ID, "ark", "v", "c")
	base := DistillTriggerInterestsInput{
		UserProfile: map[string]any{}, InputSnapshotID: "snap", RequestID: "req",
		ActivityGroup: "steady", ThresholdVersion: "tv", Binding: binding,
		InvokeDistiller: distiller, FeedbackContext: map[string]any{},
	}
	// 空输入拒绝（对齐 :712-714）
	if r, reason := DistillTriggerInterests(nil, base); r != nil || reason != "rid_input_contract_failed" {
		t.Fatalf("空输入: %v", reason)
	}
	// 非 RID 组件拒绝（对齐 :711-714）
	wrongBinding, _ := NewLearnedComponentBinding("diversity_scorer", "ark", "v", "c")
	in := base
	in.Binding = wrongBinding
	in.SessionSummaries = []*SessionSummaryArtifact{testSummary(t, "s1", 0, "x")}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_input_contract_failed" {
		t.Fatalf("非 RID 组件: %v", reason)
	}
	// ID 冲突拒绝（对齐 :728-729）
	in = base
	sum := testSummary(t, "summary-s1", 0, "x")
	in.SessionSummaries = []*SessionSummaryArtifact{sum}
	in.LongTermProfileInterests = []*LongTermProfileInterestInput{{
		InterestSummaryID: sum.SessionSummaryID, ContextScope: "global_new_chat",
		ProfileSemanticVersion: 1, InterestCluster: "c", SummaryText: "t",
		SourceProfileUnitIDs: []string{"u1"}, SchemaVersion: LONG_TERM_PROFILE_INTEREST_SCHEMA_VERSION,
	}}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_input_contract_failed" {
		t.Fatalf("ID 冲突: %v", reason)
	}
	// 摘要非自然回访拒绝（对齐 :730-737）
	in = base
	bad := testSummary(t, "s1", 0, "x")
	bad.InputOrigin = "manual_assistance"
	in.SessionSummaries = []*SessionSummaryArtifact{bad}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_input_contract_failed" {
		t.Fatalf("非自然回访: %v", reason)
	}
}

func TestDistillTriggerInterests_输出契约(t *testing.T) {
	binding, _ := NewLearnedComponentBinding(RID_COMPONENT_ID, "ark", "v", "c")
	sum1 := testSummary(t, "s1", 0, "练指弹吉他")
	distiller := func(ctx any, payload map[string]any) (any, error) { return payload, nil }
	in := DistillTriggerInterestsInput{
		UserProfile: map[string]any{}, InputSnapshotID: "snap", RequestID: "req",
		ActivityGroup: "steady", ThresholdVersion: "tv", Binding: binding,
		InvokeDistiller: distiller, FeedbackContext: map[string]any{},
		SessionSummaries: []*SessionSummaryArtifact{sum1},
	}
	// 非 dict 响应拒绝（对齐 :765-770）
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) { return "text", nil }
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_output_contract_failed" {
		t.Fatalf("非 dict: %v", reason)
	}
	// 多余键拒绝
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) {
		return map[string]any{"decisions": []any{}, "extra": 1}, nil
	}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_output_contract_failed" {
		t.Fatalf("多余键: %v", reason)
	}
	// 决策多余字段拒绝（对齐 :778-782 四键全等）
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) {
		return map[string]any{"decisions": []any{map[string]any{
			"interest_summary_id": sum1.SessionSummaryID, "selected": true,
			"rank": 1, "resonance_score_nullable": nil, "extra": 1,
		}}}, nil
	}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_output_contract_failed" {
		t.Fatalf("决策多余字段: %v", reason)
	}
	// 未知 id 拒绝（对齐 :791-793）
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) {
		return map[string]any{"decisions": []any{map[string]any{
			"interest_summary_id": "unknown", "selected": true,
			"rank": 1, "resonance_score_nullable": nil,
		}}}, nil
	}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_output_contract_failed" {
		t.Fatalf("未知 id: %v", reason)
	}
	// resonance 非 null 拒绝（本通道必须 null，对齐 :802-804）
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) {
		return map[string]any{"decisions": []any{map[string]any{
			"interest_summary_id": sum1.SessionSummaryID, "selected": true,
			"rank": 1, "resonance_score_nullable": 0.9,
		}}}, nil
	}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_output_contract_failed" {
		t.Fatalf("resonance 非 null: %v", reason)
	}
	// rank 不连续拒绝（对齐 :839-840）
	sum2 := testSummary(t, "s2", 0, "国际象棋残局")
	in.SessionSummaries = []*SessionSummaryArtifact{sum1, sum2}
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) {
		return map[string]any{"decisions": []any{
			map[string]any{"interest_summary_id": sum1.SessionSummaryID, "selected": true, "rank": 1, "resonance_score_nullable": nil},
			map[string]any{"interest_summary_id": sum2.SessionSummaryID, "selected": true, "rank": 3, "resonance_score_nullable": nil},
		}}, nil
	}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_output_contract_failed" {
		t.Fatalf("rank 不连续: %v", reason)
	}
	// provider 失败关闭（对齐 :760-763）
	in.InvokeDistiller = func(ctx any, payload map[string]any) (any, error) {
		return nil, errTestProviderInstance
	}
	if _, reason := DistillTriggerInterests(nil, in); reason != "rid_distillation_failed" {
		t.Fatalf("provider 失败: %v", reason)
	}
}

func TestDistillTriggerInterests_成功与拒绝投影(t *testing.T) {
	binding, _ := NewLearnedComponentBinding(RID_COMPONENT_ID, "ark", "v", "c")
	sum1 := testSummary(t, "s1", 0, "练指弹吉他")
	sum2 := testSummary(t, "s2", 0, "国际象棋残局")
	in := DistillTriggerInterestsInput{
		UserProfile: map[string]any{}, InputSnapshotID: "snap", RequestID: "req",
		ActivityGroup: "steady", ThresholdVersion: "tv", Binding: binding,
		FeedbackContext:  map[string]any{},
		SessionSummaries: []*SessionSummaryArtifact{sum1, sum2},
		InvokeDistiller: func(ctx any, payload map[string]any) (any, error) {
			// 只选 s1；s2 被省略 → host 记 rejected（对齐 :842-866）
			return map[string]any{"decisions": []any{
				map[string]any{"interest_summary_id": sum1.SessionSummaryID, "selected": true, "rank": 1, "resonance_score_nullable": nil},
			}}, nil
		},
	}
	result, reason := DistillTriggerInterests(nil, in)
	if reason != "" || result == nil {
		t.Fatalf("成功路径: %v", reason)
	}
	if len(result.TriggerInterests()) != 1 || len(result.RejectedInterests()) != 1 {
		t.Fatalf("触发/拒绝划分不符: %d/%d", len(result.TriggerInterests()), len(result.RejectedInterests()))
	}
	// 被省略输入血缘完整（session_summary 源）
	rejected := result.RejectedInterests()[0]
	if rejected.SourceKind != "session_summary" || len(rejected.SourceSessionSummaryIDs) != 1 {
		t.Fatalf("省略项血缘不符: %+v", rejected.AsPayload())
	}
	// 下游 payload
	payload := result.AsDownstreamPayload()
	if payload["rid_result_version"] != RID_RESULT_SCHEMA_VERSION {
		t.Fatal("rid_result_version 不符")
	}
	if len(payload["trigger_interests"].([]map[string]any)) != 1 {
		t.Fatal("trigger_interests 数量不符")
	}
	if result.Receipt.AsPayload()["output_status"] != "success" {
		t.Fatal("receipt output_status 必须 success")
	}
}

func TestEligibleNaturalRevisitSummaries_按scope过滤(t *testing.T) {
	g := testSummary(t, "s1", 0, "x")
	// project scope 摘要
	locator, _ := NewSessionSourceLocator("sp", 0, "sp:evt:0")
	p, err := NewSessionSummaryArtifact("id-p", "sp", "project:p1", "t", "manual_free_text", "none",
		[]string{"sp:evt:0"}, []*SessionSourceLocator{locator}, testBoundary("sp", 1, 1000), 1000, "p", "v", "c")
	if err != nil {
		t.Fatal(err)
	}
	out := EligibleNaturalRevisitSummaries([]*SessionSummaryArtifact{g, p}, "global_new_chat")
	if len(out) != 1 || out[0] != g {
		t.Fatal("scope 过滤不符（对齐 :589-597）")
	}
}
