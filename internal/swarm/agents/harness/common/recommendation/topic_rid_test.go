package recommendation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ridTestConfig 合法匹配器配置（https base URL + 正窗口）。
func ridTestConfig() *ArkBackgroundConfig {
	return &ArkBackgroundConfig{
		APIKey: "test-key", Model: "m-1", BaseURL: "https://ark.example.com/v1",
		ContextWindowTokens: 128000, Temperature: 0.5, TopP: 0.9,
	}
}

// ridTestSummaries 合法先后两份摘要。
func ridTestSummaries() (*SessionSummaryArtifact, *SessionSummaryArtifact) {
	earlier := &SessionSummaryArtifact{
		SessionSummaryID: "sum-1", SessionID: "sess-1", ContextScope: "global_new_chat",
		SummaryText: "用户在学指弹吉他", CompletedAt: 1800000000.0,
	}
	later := &SessionSummaryArtifact{
		SessionSummaryID: "sum-2", SessionID: "sess-2", ContextScope: "global_new_chat",
		SummaryText: "用户在练卡农", CompletedAt: 1800086400.0,
	}
	return earlier, later
}

// ridMatchInvoker 可注入的假阶段调用器。
type ridMatchInvoker struct {
	result   any
	err      error
	receipts []map[string]any
}

func (f *ridMatchInvoker) Invoke(context.Context, string, string, string) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}
func (f *ridMatchInvoker) RecordValidation(any, string) {}
func (f *ridMatchInvoker) InvocationReceipts() []map[string]any {
	return f.receipts
}
func (f *ridMatchInvoker) AppendInvocationReceipt(map[string]any) {}

// ridValidReceipt 合法厂商小票（与 lineage 对 request_id 强绑定，测试中动态填）。
func ridValidReceipt(requestID, earlierID, laterID string) map[string]any {
	return map[string]any{
		"stage":                  RidSummaryPairMatchStage,
		"response_object":        "chat.completion",
		"actual_model":           "m-1",
		"finish_reason":          "stop",
		"structured_output_mode": "json_schema_strict",
		"provider_response_id":   "prov-1",
		"lineage": map[string]any{
			"binding_scope":        "rid_summary_pair",
			"component_request_id": requestID,
			"earlier_summary_id":   earlierID,
			"later_summary_id":     laterID,
		},
	}
}

// TestArkSpecificInterestMatcher_输入契约 输入契约三硬校验（对齐 :354-361）。
func TestArkSpecificInterestMatcher_输入契约(t *testing.T) {
	store := newTestStore(t)
	earlier, _ := ridTestSummaries()
	sameSession := &SessionSummaryArtifact{SessionID: "sess-1", ContextScope: "global_new_chat", CompletedAt: 1800086400.0}
	wrongScope := &SessionSummaryArtifact{SessionID: "sess-2", ContextScope: "project", CompletedAt: 1800086400.0}
	timeInverted := &SessionSummaryArtifact{SessionID: "sess-2", ContextScope: "global_new_chat", CompletedAt: 1799999999.0}
	cases := [][]*SessionSummaryArtifact{
		{earlier, sameSession},
		{earlier, wrongScope},
		{earlier, timeInverted},
	}
	for _, c := range cases {
		m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), &ridMatchInvoker{})
		if _, err := m.Match(c[0], c[1]); err == nil ||
			err.Error() != "summary pair matcher input contract failed" {
			t.Fatalf("err=%v", err)
		}
	}
}

// TestArkSpecificInterestMatcher_配置校验 配置硬校验（对齐 :68-96）。
func TestArkSpecificInterestMatcher_配置校验(t *testing.T) {
	if _, err := NewArkSpecificInterestMatcher(newTestStore(t), &ArkBackgroundConfig{}, &ridMatchInvoker{}); err == nil {
		t.Fatal("空配置必须拒绝")
	}
	badURL := ridTestConfig()
	badURL.BaseURL = "http://ark.example.com"
	if _, err := NewArkSpecificInterestMatcher(newTestStore(t), badURL, &ridMatchInvoker{}); err == nil {
		t.Fatal("非 https 必须拒绝")
	}
}

// TestArkSpecificInterestMatcher_成功 成功路径：attempt/receipt 事件与小票绑定
// （对齐 :302-440）。
func TestArkSpecificInterestMatcher_成功(t *testing.T) {
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	invoker := &ridMatchInvoker{result: map[string]any{"label": "MATCH"}}
	m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), invoker)
	// 预置一张合法厂商小票（request_id 由请求身份派生，测试先跑一次失败拿 ID 不行——
	// 改为按同身份函数计算）
	requestID := ridSummaryPairRequestID(earlier, later, ridTestConfig().SafeProvenance())
	invoker.receipts = []map[string]any{ridValidReceipt(requestID, earlier.SessionSummaryID, later.SessionSummaryID)}
	matched, err := m.Match(earlier, later)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("MATCH 必须返回 true")
	}
	// attempt 与 receipt 各一条
	attempts, _ := store.QueryEvents(QueryEventsOptions{EventType: RidSummaryPairMatchAttemptEventType, Limit: 5})
	receipts, _ := store.QueryEvents(QueryEventsOptions{EventType: RidSummaryPairMatchReceiptEventType, Limit: 5})
	if len(attempts) != 1 || len(receipts) != 1 {
		t.Fatalf("attempts=%d receipts=%d", len(attempts), len(receipts))
	}
	payload := receipts[0]["payload"].(map[string]any)
	if payload["match_label"] != true || receipts[0]["selection_mode"] != "training_data_plane" {
		t.Fatalf("payload=%v row=%v", payload, receipts[0]) // 仅在失败时打印
	}
	if payload["provider_id"] != "ark" || payload["model"] != "m-1" {
		t.Fatalf("payload=%v", payload)
	}
}

// TestArkSpecificInterestMatcher_缓存命中 已落库小票回读，不重发请求
// （对齐 :196-271）。
func TestArkSpecificInterestMatcher_缓存命中(t *testing.T) {
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	requestID := ridSummaryPairRequestID(earlier, later, ridTestConfig().SafeProvenance())
	receipt := ridValidReceipt(requestID, earlier.SessionSummaryID, later.SessionSummaryID)
	payload := map[string]any{
		"schema_version":         RidSummaryPairMatchReceiptSchemaVersion,
		"operation_id":           "op-1",
		"request_id":             requestID,
		"matcher_id":             RidSummaryPairMatcherID,
		"matcher_version":        RidSummaryPairMatcherVersion,
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"earlier_summary_id":     earlier.SessionSummaryID,
		"later_summary_id":       later.SessionSummaryID,
		"provider_id":            "ark",
		"model":                  "m-1",
		"match_label":            true,
		"provider_config":        ridTestConfig().SafeProvenance(),
		"provider_receipt":       receipt,
	}
	ok, _ := store.RecordEvent(map[string]any{
		"session_id": later.SessionID, "slate_id": requestID,
		"event_type": RidSummaryPairMatchReceiptEventType, "topic_id": requestID,
		"policy_id": ProductPolicyID, "selection_mode": "training_data_plane",
		"propensity_reason": "not_applicable", "payload": payload,
		"created_at": 1800086400.0,
	})
	if !ok {
		t.Fatal("预置小票落库失败")
	}
	invoker := &ridMatchInvoker{} // 不给结果：命中缓存不得调模型
	m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), invoker)
	matched, err := m.Match(earlier, later)
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
}

// TestArkSpecificInterestMatcher_小票契约失败 缓存小票字段破坏必须报错
// （对齐 :246-267 rejected 分支）。
func TestArkSpecificInterestMatcher_小票契约失败(t *testing.T) {
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	requestID := ridSummaryPairRequestID(earlier, later, ridTestConfig().SafeProvenance())
	receipt := ridValidReceipt(requestID, earlier.SessionSummaryID, later.SessionSummaryID)
	delete(receipt, "finish_reason") // 破坏必填键
	payload := map[string]any{
		"schema_version": RidSummaryPairMatchReceiptSchemaVersion,
		"operation_id":   "op-1", "request_id": requestID,
		"matcher_id": RidSummaryPairMatcherID, "matcher_version": RidSummaryPairMatcherVersion,
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"earlier_summary_id": earlier.SessionSummaryID, "later_summary_id": later.SessionSummaryID,
		"provider_id": "ark", "model": "m-1", "match_label": true,
		"provider_config": ridTestConfig().SafeProvenance(), "provider_receipt": receipt,
	}
	_, _ = store.RecordEvent(map[string]any{
		"session_id": later.SessionID, "slate_id": requestID,
		"event_type": RidSummaryPairMatchReceiptEventType, "topic_id": requestID,
		"policy_id": ProductPolicyID, "selection_mode": "training_data_plane",
		"propensity_reason": "not_applicable", "payload": payload,
		"created_at": 1800086400.0,
	})
	m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), &ridMatchInvoker{})
	_, err := m.Match(earlier, later)
	if err == nil || !strings.Contains(err.Error(), "receipt") {
		t.Fatalf("err=%v", err)
	}
}

// TestArkSpecificInterestMatcher_输出契约 模型输出多余键/非法 label 拒绝
// （对齐 :411-419）。
func TestArkSpecificInterestMatcher_输出契约(t *testing.T) {
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	cases := []any{
		map[string]any{"label": "MATCH", "extra": 1},
		map[string]any{"label": "INVALID"},
		"not-a-map",
	}
	for _, result := range cases {
		requestID := ridSummaryPairRequestID(earlier, later, ridTestConfig().SafeProvenance())
		invoker := &ridMatchInvoker{result: result,
			receipts: []map[string]any{ridValidReceipt(requestID, earlier.SessionSummaryID, later.SessionSummaryID)}}
		m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), invoker)
		if _, err := m.Match(earlier, later); err == nil ||
			err.Error() != "summary pair matcher output contract failed" {
			t.Fatalf("result=%v err=%v", result, err)
		}
	}
}

// TestArkSpecificInterestMatcher_厂商小票缺失 无匹配小票必须报错
// （对齐 :330-343）。
func TestArkSpecificInterestMatcher_厂商小票缺失(t *testing.T) {
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	requestID := ridSummaryPairRequestID(earlier, later, ridTestConfig().SafeProvenance())
	invoker := &ridMatchInvoker{result: map[string]any{"label": "MATCH"},
		receipts: []map[string]any{{"stage": "other"}}}
	m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), invoker)
	_, err := m.Match(earlier, later)
	if err == nil || err.Error() != "summary pair matcher provider receipt missing" {
		t.Fatalf("err=%v", err)
	}
	_ = requestID
}

// TestArkSpecificInterestMatcher_NO_MATCH NO_MATCH 闭环落库
// （对齐 :437 match_label=false）。
func TestArkSpecificInterestMatcher_NO_MATCH(t *testing.T) {
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	requestID := ridSummaryPairRequestID(earlier, later, ridTestConfig().SafeProvenance())
	invoker := &ridMatchInvoker{result: map[string]any{"label": "NO_MATCH"},
		receipts: []map[string]any{ridValidReceipt(requestID, earlier.SessionSummaryID, later.SessionSummaryID)}}
	m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), invoker)
	matched, err := m.Match(earlier, later)
	if err != nil || matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	receipts, _ := store.QueryEvents(QueryEventsOptions{EventType: RidSummaryPairMatchReceiptEventType, Limit: 5})
	payload := receipts[0]["payload"].(map[string]any)
	if payload["match_label"] != false {
		t.Fatalf("payload=%v", payload)
	}
}

// TestRidSummaryPairResponseSchema 阶段守卫（对齐 :108-112）。
func TestRidSummaryPairResponseSchema(t *testing.T) {
	if _, err := RidSummaryPairResponseSchema("other-stage", "{}"); err == nil {
		t.Fatal("未知阶段必须拒绝")
	}
	schema, err := RidSummaryPairResponseSchema(RidSummaryPairMatchStage, "{}")
	if err != nil {
		t.Fatal(err)
	}
	if schema == nil {
		t.Fatal("合法阶段必须返回 schema")
	}
}

// TestArkSpecificInterestMatcher_错误类型保持 error 形态（recover 不引入 panic）。
func TestArkSpecificInterestMatcher_错误类型保持(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Match 不得 panic: %v", r)
		}
	}()
	store := newTestStore(t)
	earlier, later := ridTestSummaries()
	invoker := &ridMatchInvoker{err: errors.New("boom")}
	m, _ := NewArkSpecificInterestMatcher(store, ridTestConfig(), invoker)
	if _, err := m.Match(earlier, later); err == nil {
		t.Fatal("调用失败必须返错")
	}
}
