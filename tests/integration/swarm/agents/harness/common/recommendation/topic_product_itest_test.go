//go:build integration

// topic 产物与呈现链路集成测试：冷启动卡 → 整池验收落库 → 精确回读 →
// 曝光选卡 → outcome 记账回写；新闻缺席降级；RID 配对闭环。
// 跨子系统：recommendation（topic_product/topic_news/topic_rid/topic_outcomes）+
// harness/tools（RunFreeSearchStructured 搜索边界）+ server/session（summary 工件依赖的
// session 语义），覆盖 ≥3 个 internal 二级子系统。
package recommendation_itest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TopicProductITestSuite topic 产物链路。
type TopicProductITestSuite struct {
	isuite.BaseIntegrationSuite
}

// TestSuite 套件入口（审计要求自带 suite.Run）。
func TestSuite(t *testing.T) {
	suite.Run(t, new(TopicProductITestSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newStore 真实 TopicStarterStore（临时目录 recommendation.db）。
func (s *TopicProductITestSuite) newStore() *recommendation.TopicStarterStore {
	store, err := recommendation.NewTopicStarterStore(filepath.Join(s.T().TempDir(), "recommendation.db"))
	s.Require().NoError(err)
	return store
}

// itestTriggerKey 合法触发键。
func itestTriggerKey() map[string]any {
	return map[string]any{
		"local_date":                  "2026-11-12",
		"search_provider_id":          recommendation.OpenjiuwenNativeSearchProviderId,
		"profile_semantic_version":    "profile-v1",
		"short_intent_version":        "short-v1",
		"negative_preference_version": "neg-v1",
		"generation_config_version":   "gen-v19",
		"corpus_version":              recommendation.CorpusVersion,
		"document_snapshot_version":   "doc-v1",
		"retrieval_config_version":    "ret-v1",
		"evidence_extractor_version":  "ev-v1",
		"input_revision_receipt":      map[string]any{"receipt_id": "snap-1"},
	}
}

// TestColdStartPoolFullChain 全链路：冷启动卡 → 写池 → 精确回读 → 选卡 →
// outcome 记账（用例 1）。
func (s *TopicProductITestSuite) TestColdStartPoolFullChain() {
	store := s.newStore()
	now := 1800000000.0
	triggerKey := itestTriggerKey()
	cards := recommendation.BuildColdStartCards(triggerKey, now, nil, nil, recommendation.FIRST_SCREEN_CARD_TARGET, nil)
	s.Require().NotEmpty(cards)
	write := recommendation.WriteColdStartPool(store, cards, triggerKey, now)
	s.Require().True(write.Bool(), "冷启动池必须写入: %s", write.Reason)

	// 经事件表精确回读 manifest + 卡
	manifestRows, err := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: "starter_slate_manifest", Limit: 5, NewestFirst: true,
	})
	s.Require().NoError(err)
	s.Require().Len(manifestRows, 1)
	manifest := manifestRows[0]["payload"].(map[string]any)
	cardRows, err := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: "starter_card", SlateID: strPtrI(pyStrI(manifest["manifest_id"])), Limit: 10,
	})
	s.Require().NoError(err)
	s.Require().Len(cardRows, len(cards))

	// 曝光选卡：全部可集
	eligible := map[string]bool{}
	for _, card := range cards {
		eligible[pyStrI(card["card_revision_id"])] = true
	}
	selected := recommendation.SelectExposure(cards, eligible, recommendation.GlobalNewChatScope, 0, now,
		map[string]bool{}, map[string]bool{}, nil)
	s.Require().NotEmpty(selected)
	s.Require().LessOrEqual(len(selected), recommendation.FIRST_SCREEN_CARD_TARGET)

	// outcome 记账：曝光事件落库后可回读
	// 跨子系统：server/session 的请求完成行落盘（outcome 记账的上游事实源，
	// 对齐 prompt_suggestion_ledger itest 的口径）
	completionReceipt, err := session.EnqueueHistoryRequestCompletion("itest-session", "req-itest", "success")
	s.Require().NoError(err)
	s.Require().NotNil(completionReceipt)
	<-completionReceipt

	recorded, err := store.RecordEvent(map[string]any{
		"session_id":        "itest-session",
		"slate_id":          pyStrI(manifest["manifest_id"]),
		"event_type":        "training_exposure",
		"topic_id":          pyStrI(selected[0]["card_id"]),
		"policy_id":         recommendation.ProductPolicyID,
		"selection_mode":    "exposure",
		"propensity_reason": "not_applicable",
		"payload":           map[string]any{"card_id": selected[0]["card_id"]},
		"created_at":        now,
	})
	s.Require().True(recorded)
	rows, err := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: "training_exposure", TopicID: strPtrI(pyStrI(selected[0]["card_id"])), Limit: 5,
	})
	s.Require().NoError(err)
	s.Require().Len(rows, 1)
}

// TestNewsAbsenceDegradation 新闻缺席降级：fetch 超时留痕 absent 路径，
// trigger 缺 trigger_interests 时冷启动兜底仍可写池（用例 2）。
func (s *TopicProductITestSuite) TestNewsAbsenceDegradation() {
	store := s.newStore()
	now := 1800000000.0
	triggerKey := itestTriggerKey()
	// 热榜全部来源失败 → Pull 返回 nil（含 failure 留痕）
	feed := recommendation.NewHotListFeed(store, func(string, int, map[string]string) (string, error) {
		return "", context.DeadlineExceeded
	}, func() float64 { return now })
	pull := feed.Pull(context.Background())
	s.Require().Nil(pull)
	rows, err := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: recommendation.HotListPullEventType, Limit: 10, NewestFirst: true,
	})
	s.Require().NoError(err)
	s.Require().Len(rows, 2)
	for _, row := range rows {
		payload := row["payload"].(map[string]any)
		s.Require().Equal("failure", payload["status"])
	}
	// 缺热榜时冷启动仍可组卡（语料兜底）
	cards := recommendation.BuildColdStartCards(triggerKey, now, nil, nil, recommendation.FIRST_SCREEN_CARD_TARGET, nil)
	write := recommendation.WriteColdStartPool(store, cards, triggerKey, now)
	s.Require().True(write.Bool(), "无热榜冷启动池必须可写: %s", write.Reason)
}

// TestFreeSearchBoundaryNativeLane 原生 lane 的结构化搜索边界：
// harness/tools.RunFreeSearchStructured 在引擎未启用的环境下必须显式失败，
// 契约错误归因由 recommendation 侧吸收为 provider 失败小票（用例 3 前置）。
func (s *TopicProductITestSuite) TestFreeSearchBoundaryNativeLane() {
	_, _, err := tools.RunFreeSearchStructured("AI 芯片", 5, 5)
	s.Require().Error(err, "引擎未启用必须显式失败（fail-closed）")
}

// TestRIDPairMatcherLoop RID 配对闭环：合法摘要对 → MATCH 落库 →
// 第二次调用走缓存不重发请求（用例 3）。
func (s *TopicProductITestSuite) TestRIDPairMatcherLoop() {
	store := s.newStore()
	config := &recommendation.ArkBackgroundConfig{
		APIKey: "itest-key", Model: "m-1",
		BaseURL:             "https://ark.example.com/v1",
		ContextWindowTokens: 128000, Temperature: 0.5, TopP: 0.9,
	}
	earlier := &recommendation.SessionSummaryArtifact{
		SessionSummaryID: "sum-1", SessionID: "sess-1",
		ContextScope: recommendation.GlobalNewChatScope, SummaryText: "用户在学指弹吉他",
		CompletedAt: 1800000000,
	}
	later := &recommendation.SessionSummaryArtifact{
		SessionSummaryID: "sum-2", SessionID: "sess-2",
		ContextScope: recommendation.GlobalNewChatScope, SummaryText: "用户在练卡农",
		CompletedAt: 1800086400,
	}
	invoker := &ridStageInvokerStub{result: map[string]any{"label": "MATCH"}}
	m, err := recommendation.NewArkSpecificInterestMatcher(store, config, invoker)
	s.Require().NoError(err)
	// 首次调用：小票未备 → provider receipt missing，但 attempt 事件已落库并
	// 携带 request_id——集成测试经事件表取回身份，再补小票重试
	_, firstErr := m.Match(earlier, later)
	s.Require().Error(firstErr)
	attemptRows, err := store.QueryEvents(recommendation.QueryEventsOptions{
		EventType: recommendation.RidSummaryPairMatchAttemptEventType, Limit: 5, NewestFirst: true,
	})
	s.Require().NoError(err)
	s.Require().Len(attemptRows, 1)
	requestID := pyStrI(attemptRows[0]["payload"].(map[string]any)["request_id"])
	s.Require().NotEmpty(requestID)
	invoker.receipts = []map[string]any{ridValidReceiptFor(requestID, earlier, later, config)}
	matched, err := m.Match(earlier, later)
	s.Require().NoError(err)
	s.Require().True(matched)
	// 第二次：命中缓存
	invokeCount := invoker.invokes
	matched2, err := m.Match(earlier, later)
	s.Require().NoError(err)
	s.Require().True(matched2)
	s.Require().Equal(invokeCount, invoker.invokes, "缓存命中不得重发请求")
}

// ridStageInvokerStub 可注入的阶段调用器。
type ridStageInvokerStub struct {
	result   any
	err      error
	receipts []map[string]any
	invokes  int
}

// Invoke 记录调用并回放结果。
func (f *ridStageInvokerStub) Invoke(_ context.Context, _, _, _ string) (any, error) {
	f.invokes++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// RecordValidation 记录校验（桩）。
func (f *ridStageInvokerStub) RecordValidation(any, string) {}

// InvocationReceipts 返回调用小票。
func (f *ridStageInvokerStub) InvocationReceipts() []map[string]any { return f.receipts }

// AppendInvocationReceipt 追加小票（桩）。
func (f *ridStageInvokerStub) AppendInvocationReceipt(map[string]any) {}

// ridValidReceiptFor 合法厂商小票（lineage 绑定 request_id）。
func ridValidReceiptFor(requestID string, earlier, later *recommendation.SessionSummaryArtifact,
	config *recommendation.ArkBackgroundConfig) map[string]any {
	return map[string]any{
		"stage":                  recommendation.RidSummaryPairMatchStage,
		"response_object":        "chat.completion",
		"actual_model":           config.Model,
		"finish_reason":          "stop",
		"structured_output_mode": "json_schema_strict",
		"provider_response_id":   "prov-1",
		"lineage": map[string]any{
			"binding_scope":        "rid_summary_pair",
			"component_request_id": requestID,
			"earlier_summary_id":   earlier.SessionSummaryID,
			"later_summary_id":     later.SessionSummaryID,
		},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pyStrI any → string。
func pyStrI(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// strPtrI string → *string。
func strPtrI(value string) *string { return &value }
