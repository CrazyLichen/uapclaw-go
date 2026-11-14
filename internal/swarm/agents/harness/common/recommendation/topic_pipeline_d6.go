package recommendation

// topic_pipeline_d6.go 一比一对齐上游 topic_pipeline.py:5943-6343 的
// _distill_interests/_remember_rid_decision/_reused_rid_decision/
// _hot_news_lead。
// 已知偏差（§5 并发映射）：async def → 同步方法；provider_validation
// 上下文管理器（topic_model.py:439）→ 在契约失败路径显式记录
// business_validation_failed 后返回失败原因。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// RIDDecisionEventType RID 决策事件类型（topic_pipeline.py:185）
const RIDDecisionEventType = "rid_decision"

// RIDThresholdVersion RID 阈值版本（:186）
const RIDThresholdVersion = "icebreaker-api-proxy-threshold-v1"

// HotListMatchLimit 热榜匹配条数上限（topic_news.py:243）
const HotListMatchLimit = 50

// ──────────────────────────── 导出函数 ────────────────────────────

// DistillInterests 运行 RID 提炼（topic_pipeline.py:5943-6103
// _distill_interests）。无可判输入时返回降级 RIDResult（不调用模型）。
func (p *TopicPipeline) DistillInterests(snapshot *V6ContextSnapshot,
	summaries []*SessionSummaryArtifact, feedback *InterestFeedback,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker,
	decisionTime float64, decisionTimeSet bool, retryEmpty bool) (*RIDResult, error) {
	providerVersion := pyStrOr(config.ExpectedModel, config.Model)
	binding, err := PipelineAPIBinding(RID_COMPONENT_ID, "ark", providerVersion,
		RID_API_PROMPT_VERSION)
	if err != nil {
		return nil, err
	}
	invokeDistiller := NewArkRIDDistillationAdapter(stageInvokerOf(invokeStage))
	eligibleSummaries := EligibleNaturalRevisitSummaries(summaries, snapshot.ContextScope)
	wantedSummaries := RecentRIDSummaries(
		WantedSessionSummaries(snapshot, eligibleSummaries, feedback),
		ShortDeltaSummaryIDs(snapshot, eligibleSummaries))
	wantedProfileInterests := WantedProfileInterests(snapshot, feedback)
	frozenDecisionTime := p.Now()
	if decisionTimeSet {
		frozenDecisionTime = decisionTime
	}
	decisionContext := RidDecisionContext(snapshot, wantedSummaries, feedback,
		frozenDecisionTime)
	if len(wantedProfileInterests) == 0 && len(wantedSummaries) == 0 {
		// 无自然会话也无否定放过的兴趣时 RID 无可判：不调用，返回空选择。
		return &RIDResult{
			ContextScope:     snapshot.ContextScope,
			ActivityGroup:    pyStr(decisionContext["activity_group"]),
			ThresholdVersion: RIDThresholdVersion,
			Receipt: &LearnedComponentReceipt{
				Binding:         binding,
				InputSnapshotID: snapshot.InputSnapshotID,
				RequestID:       "v6-rid-" + newUUIDHex(),
				ScoreSemantics:  "not_scored",
				OutputStatus:    "fallback",
				FallbackReason:  RIDNothingToJudge,
			},
			Decisions: nil,
		}, nil
	}
	userProfile := map[string]any{}
	for key, value := range snapshot.ProfileProjection {
		userProfile[key] = value
	}
	userProfile["short_intent"] = snapshot.ShortIntentState.AsPayload()
	var deltaMessages []*V6SourceMessage
	for _, item := range snapshot.Messages {
		if containsString(snapshot.DeltaEventIDs, item.EventID) {
			deltaMessages = append(deltaMessages, item)
		}
	}
	deltaPayloads := make([]map[string]any, 0, len(deltaMessages))
	for _, item := range deltaMessages {
		age := frozenDecisionTime - item.ObservedAt
		if age < 0 {
			age = 0
		}
		deltaPayloads = append(deltaPayloads, map[string]any{
			"event_id": item.EventID, "intent_cluster": item.IntentCluster,
			"observed_at": item.ObservedAt, "age_seconds": age,
		})
	}
	profileInterestPayloads := make([]map[string]any, 0, len(snapshot.LongTermProfileInterests))
	for _, interest := range snapshot.LongTermProfileInterests {
		profileInterestPayloads = append(profileInterestPayloads, interest.AsPayload())
	}
	summaryPayloads := make([]map[string]any, 0, len(eligibleSummaries))
	sortedEligible := append([]*SessionSummaryArtifact(nil), eligibleSummaries...)
	sort.Slice(sortedEligible, func(i, j int) bool {
		return sortedEligible[i].SessionSummaryID < sortedEligible[j].SessionSummaryID
	})
	for _, summary := range sortedEligible {
		summaryPayloads = append(summaryPayloads, summary.AsPayload())
	}
	requiredDeltaIDs := ShortDeltaSummaryIDs(snapshot, eligibleSummaries)
	sort.Strings(requiredDeltaIDs)
	activityGroup := pyStr(decisionContext["activity_group"])
	// What RID judged, less what changes with every call（对齐 :6026-6053）。
	reuseKey := map[string]any{
		"local_date":                  LocalDay(frozenDecisionTime),
		"binding":                     binding.AsProvenance(),
		"provider_config":             config.SafeProvenance(),
		"input_snapshot_id":           snapshot.InputSnapshotID,
		"threshold_version":           RIDThresholdVersion,
		"activity_group":              activityGroup,
		"user_profile":                userProfile,
		"feedback_context":            map[string]any{"negative_preferences": feedback.ModelPayloads(false)},
		"long_term_profile_interests": profileInterestPayloads,
		"session_summaries":           summaryPayloads,
		"delta":                       deltaMessagesPlain(deltaMessages),
		"required_short_delta_interest_summary_ids": requiredDeltaIDs,
	}
	ridInputs := &DistillTriggerInterestsInput{
		UserProfile:              userProfile,
		LongTermProfileInterests: wantedProfileInterests,
		SessionSummaries:         wantedSummaries,
		InputSnapshotID:          snapshot.InputSnapshotID,
		ActivityGroup:            activityGroup,
		ThresholdVersion:         RIDThresholdVersion,
		Delta:                    deltaPayloads,
		FeedbackContext:          map[string]any{"negative_preferences": feedback.ModelPayloads(false)},
		Binding:                  binding,
		DecisionContext:          decisionContext,
	}
	reused, err := p.reusedRIDDecision(context.Background(), snapshot.ContextScope,
		reuseKey, ridInputs, invokeStage, retryEmpty)
	if err != nil {
		return nil, err
	}
	if reused != nil {
		return reused, nil
	}
	if err := PreflightRIDInputs(config, ridInputsAsMap(ridInputs), p.StageTokenCounterFactory); err != nil {
		return nil, err
	}
	var frozenRequest map[string]any
	var ridResponse any
	newlyUsedDistiller := func(ctx any, payload map[string]any) (any, error) {
		// json.loads(json.dumps(payload)) 深拷贝
		frozenRequest = jsonRoundTrip(payload)
		response, callErr := invokeDistiller.Call(ctx, payload)
		ridResponse = response
		return response, callErr
	}
	result, reason := DistillTriggerInterests(context.Background(), DistillTriggerInterestsInput{
		UserProfile:              ridInputs.UserProfile,
		LongTermProfileInterests: ridInputs.LongTermProfileInterests,
		SessionSummaries:         ridInputs.SessionSummaries,
		InputSnapshotID:          ridInputs.InputSnapshotID,
		RequestID:                "v6-rid-" + newUUIDHex(),
		ActivityGroup:            ridInputs.ActivityGroup,
		ThresholdVersion:         ridInputs.ThresholdVersion,
		Delta:                    ridInputs.Delta,
		FeedbackContext:          ridInputs.FeedbackContext,
		Binding:                  ridInputs.Binding,
		InvokeDistiller:          newlyUsedDistiller,
		DecisionContext:          ridInputs.DecisionContext,
	})
	outcome := "business_validated"
	if reason != "" || result == nil {
		outcome = "business_validation_failed"
	}
	RecordProviderValidationTopic(invokeStage, ridResponse, outcome)
	if reason != "" || result == nil {
		if reason == "" {
			reason = "rid_distillation_failed"
		}
		return nil, &TerminalGenerationRejection{Reason: reason}
	}
	contextArtifact := &RIDUserContextSnapshot{
		UserHistorySnapshotID: fmt.Sprintf("rid-user-context:%s",
			frozenRequest["request_id"]),
		ContextScope:   snapshot.ContextScope,
		CutoffAt:       frozenDecisionTime,
		RequestPayload: frozenRequest,
	}
	if !PersistOutcomeArtifact(p.Store, contextArtifact, frozenDecisionTime) {
		return nil, &TerminalGenerationRejection{Reason: "rid_user_context_persistence_failed"}
	}
	p.RememberRIDDecision(snapshot.ContextScope, result, reuseKey,
		ridInputs.LongTermProfileInterests, ridInputs.SessionSummaries,
		invokeStage, frozenDecisionTime)
	return result, nil
}

// RememberRIDDecision 保留一次成功的 RID 调用，当日后续轮次可复用
// （topic_pipeline.py:6105-6161）。Ark 小票随之：复用决策构建的卡仍点名
// 产生它的调用。
func (p *TopicPipeline) RememberRIDDecision(contextScope string, result *RIDResult,
	reuseKey map[string]any, judgedProfileInterests []*LongTermProfileInterestInput,
	judgedSessionSummaries []*SessionSummaryArtifact, invokeStage TopicStageInvoker,
	decidedAt float64) {
	var providerReceipt map[string]any
	for _, receipt := range invokeStage.InvocationReceipts() {
		if pyStr(receipt["stage"]) != "rid_interest_distiller" {
			continue
		}
		lineage, _, _ := payloadMap(receipt["lineage"])
		if lineage == nil {
			continue
		}
		if pyStr(lineage["component_request_id"]) != result.Receipt.RequestID {
			continue
		}
		providerReceipt = receipt
		break
	}
	if providerReceipt == nil {
		return
	}
	var judgedInterestIDs []any
	for _, interest := range judgedProfileInterests {
		judgedInterestIDs = append(judgedInterestIDs, interest.InterestSummaryID)
	}
	var judgedSummaryIDs []any
	for _, summary := range judgedSessionSummaries {
		judgedSummaryIDs = append(judgedSummaryIDs, summary.SessionSummaryID)
	}
	var selectedIDs []any
	for _, decision := range result.TriggerInterests() {
		selectedIDs = append(selectedIDs, decision.InterestSummaryID)
	}
	_, _ = p.Store.RecordEvent(map[string]any{
		"session_id":        "topic-starter-rid",
		"slate_id":          contextScope,
		"event_type":        RIDDecisionEventType,
		"topic_id":          result.Receipt.RequestID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "rid_decision",
		"propensity_reason": "not_applicable",
		"payload": map[string]any{
			"reuse_key":                     reuseKey,
			"request_id":                    result.Receipt.RequestID,
			"judged_profile_interest_ids":   judgedInterestIDs,
			"judged_session_summary_ids":    judgedSummaryIDs,
			"selected_interest_summary_ids": selectedIDs,
			"provider_receipt":              providerReceipt,
		},
		"created_at": decidedAt,
	})
}

// reusedRIDDecision scope 最近一次 RID 决策在当日输入未变时复用
// （topic_pipeline.py:6163-6243）。RID 读的反馈属于该决策的输入：方向被
// 否定或恢复都需新调用。未变输入保留原调用请求 ID 与 Ark 小票。
func (p *TopicPipeline) reusedRIDDecision(ctx context.Context, contextScope string,
	reuseKey map[string]any, ridInputs *DistillTriggerInterestsInput,
	invokeStage TopicStageInvoker, retryEmpty bool) (*RIDResult, error) {
	receipts := invokeStage.InvocationReceipts()
	rows, err := p.Store.QueryEvents(QueryEventsOptions{
		EventType:   RIDDecisionEventType,
		SlateID:     stringPtr(contextScope),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil {
		return nil, err
	}
	if receipts == nil || len(rows) == 0 {
		return nil, nil
	}
	cached, _, ok := payloadMap(rows[0]["payload"])
	if !ok {
		return nil, nil
	}
	cachedKey, _, _ := payloadMap(cached["reuse_key"])
	if cachedKey == nil || !reuseKeyMatchesIgnoringSnapshot(cachedKey, reuseKey) {
		return nil, nil
	}
	inputIDs := map[string]bool{}
	for _, interest := range ridInputs.LongTermProfileInterests {
		inputIDs[interest.InterestSummaryID] = true
	}
	for _, summary := range ridInputs.SessionSummaries {
		inputIDs[summary.SessionSummaryID] = true
	}
	judged := map[string]bool{}
	for _, id := range pyListStrings(cached["judged_profile_interest_ids"]) {
		judged[id] = true
	}
	for _, id := range pyListStrings(cached["judged_session_summary_ids"]) {
		judged[id] = true
	}
	for id := range inputIDs {
		if !judged[id] {
			return nil, nil
		}
	}
	var kept []string
	for _, interestID := range pyListStrings(cached["selected_interest_summary_ids"]) {
		if inputIDs[interestID] {
			kept = append(kept, interestID)
		}
	}
	if retryEmpty && len(kept) == 0 {
		return nil, nil
	}
	replay := func(ctx any, payload map[string]any) (any, error) {
		decisions := []any{}
		for rank, interestID := range kept {
			decisions = append(decisions, map[string]any{
				"interest_summary_id": interestID, "selected": true,
				"rank": rank + 1, "resonance_score_nullable": nil,
			})
		}
		return map[string]any{"decisions": decisions}, nil
	}
	result, reason := DistillTriggerInterests(ctx, DistillTriggerInterestsInput{
		UserProfile:              ridInputs.UserProfile,
		LongTermProfileInterests: ridInputs.LongTermProfileInterests,
		SessionSummaries:         ridInputs.SessionSummaries,
		InputSnapshotID:          ridInputs.InputSnapshotID,
		RequestID:                pyStr(cached["request_id"]),
		ActivityGroup:            ridInputs.ActivityGroup,
		ThresholdVersion:         ridInputs.ThresholdVersion,
		Delta:                    ridInputs.Delta,
		FeedbackContext:          ridInputs.FeedbackContext,
		Binding:                  ridInputs.Binding,
		InvokeDistiller:          replay,
		DecisionContext:          ridInputs.DecisionContext,
	})
	if reason != "" || result == nil {
		return nil, nil
	}
	providerReceipt := mapCopyAny(payloadMapOrEmpty(cached["provider_receipt"]))
	lineage := payloadMapOrEmpty(providerReceipt["lineage"])
	if pyStr(lineage["input_snapshot_id"]) != ridInputs.InputSnapshotID {
		lineage["input_snapshot_id"] = lineage["input_snapshot_id"]
		providerReceipt["reused_for_input_snapshot_id"] = ridInputs.InputSnapshotID
	}
	invokeStage.AppendInvocationReceipt(providerReceipt)
	logger.Info(logger.ComponentAgentCore).Str("request_id", pyStr(cached["request_id"])).
		Str("context_scope", contextScope).Msg("Topic Starter v6 RID decision reused")
	return result, nil
}

// HotNewsLead 从热榜匹配本轮新闻候选，或说明为何没有
// （topic_pipeline.py:6245-6343 _hot_news_lead）。匹配失败时页面少新闻
// 而不缺其他卡。
func (p *TopicPipeline) HotNewsLead(snapshot *V6ContextSnapshot, pull *HotListPull,
	feedback *InterestFeedback, invokeStage TopicStageInvoker) (*map[string]any, string) {
	var entries []HotListEntry
	if pull != nil {
		for index, entry := range pull.Entries {
			if index >= HotListMatchLimit {
				break
			}
			if CardCopyRejectionReason(HotListTitle(entry.Word), CardTitleMaxChars) != "" {
				continue
			}
			if feedback.BlocksNews(SearchPageURL(entry.Word)) {
				continue
			}
			if feedback.BlocksNewsEvent(HotEventKey(entry.Word)) {
				continue
			}
			entryCopy := entry
			entries = append(entries, *entryCopy)
		}
	}
	if pull == nil || len(entries) == 0 {
		return nil, "hot_list_unavailable"
	}
	interests := WantedProfileInterests(snapshot, feedback)
	candidateIDs := []string{newCandidateID(), newCandidateID()}
	interestPayloads := make([]any, 0, len(interests))
	for _, interest := range interests {
		interestPayloads = append(interestPayloads, map[string]any{
			"interest_summary_id": interest.InterestSummaryID,
			"interest_summary":    interest.SummaryText,
		})
	}
	entryPayloads := make([]any, 0, len(entries))
	for _, entry := range entries {
		entryPayloads = append(entryPayloads, map[string]any{
			"hot_entry_id": entry.EntryID, "title": entry.Word, "summary": entry.Summary,
		})
	}
	payload := map[string]any{
		"input_snapshot_id":    snapshot.InputSnapshotID,
		"interests":            interestPayloads,
		"hot_list":             entryPayloads,
		"negative_preferences": feedback.ModelPayloads(true),
		"direction_feedback":   feedback.DirectionPayloads(),
		"candidate_ids":        candidateIDs,
		"request_id":           "v6-hot-news-" + newUUIDHex(),
	}
	encoded, err := marshalNoEscape(payload)
	if err != nil {
		return nil, "hot_news_matcher_failed"
	}
	response, invokeErr := invokeStage.Invoke(context.Background(),
		"v6_hot_news_matcher", pipelineHotNewsMatcherPrompt, string(encoded))
	entry, interest, match, query, direction, contractErr := validateHotNewsResponse(
		response, entries, interests)
	if invokeErr != nil || contractErr != nil {
		RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
		logger.Warn(logger.ComponentAgentCore).Msg("Topic Starter v6 hot news match failed")
		return nil, "hot_news_matcher_failed"
	}
	RecordProviderValidationTopic(invokeStage, response, "business_validated")
	plan := HotNewsPlan(candidateIDs, entry, pull, interest, match,
		strings.TrimSpace(query), direction,
		snapshot.ContextScope)
	return &plan, ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reuseKeyMatchesIgnoringSnapshot 除 input_snapshot_id 外键值全等
// （对齐 :6192-6196）。
func reuseKeyMatchesIgnoringSnapshot(cachedKey, reuseKey map[string]any) bool {
	// 双侧 JSON 往返规整（DB 解出的 []any vs Go 直构 []map[string]any）
	roundedCached := jsonRoundTrip(stripKey(cachedKey, "input_snapshot_id"))
	roundedReuse := jsonRoundTrip(stripKey(reuseKey, "input_snapshot_id"))
	return stableJSON(roundedCached) == stableJSON(roundedReuse)
}

// stripKey 复制映射并剔除一个键。
func stripKey(source map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, value := range source {
		if k != key {
			out[k] = value
		}
	}
	return out
}

// deltaMessagesPlain delta 消息的复用键形态（无 age_seconds，对齐 :6042-6049）。
func deltaMessagesPlain(messages []*V6SourceMessage) []any {
	out := make([]any, 0, len(messages))
	for _, item := range messages {
		out = append(out, map[string]any{
			"event_id": item.EventID, "intent_cluster": item.IntentCluster,
			"observed_at": item.ObservedAt,
		})
	}
	return out
}

// ridInputsAsMap RID 预检载荷（RIDRequestEstimatePayload 的输入形状）。
func ridInputsAsMap(in *DistillTriggerInterestsInput) map[string]any {
	summaryArtifacts := in.SessionSummaries
	return map[string]any{
		"binding":                     in.Binding,
		"input_snapshot_id":           in.InputSnapshotID,
		"user_profile":                in.UserProfile,
		"long_term_profile_interests": in.LongTermProfileInterests,
		"session_summaries":           summaryArtifacts,
		"activity_group":              in.ActivityGroup,
		"threshold_version":           in.ThresholdVersion,
		"feedback_context":            in.FeedbackContext,
		"delta":                       in.Delta,
		"decision_context":            in.DecisionContext,
	}
}

// stageInvokerOf 接口到底层调用器（适配器需要 StageInvoker 形态）。
func stageInvokerOf(invoker TopicStageInvoker) StageInvoker {
	return func(ctx any, stage, systemPrompt, payload string) (any, error) {
		if ctxValue, ok := ctx.(context.Context); ok {
			return invoker.Invoke(ctxValue, stage, systemPrompt, payload)
		}
		return invoker.Invoke(context.Background(), stage, systemPrompt, payload)
	}
}

// validateHotNewsResponse 匹配响应的形状与语义校验
// （对齐 :6300-6327）。
func validateHotNewsResponse(response any, entries []HotListEntry,
	interests []*LongTermProfileInterestInput) (*HotListEntry,
	*LongTermProfileInterestInput, string, string, any, error) {
	payload, ok := response.(map[string]any)
	if !ok || len(payload) != 5 {
		return nil, nil, "", "", nil, &TerminalGenerationRejection{Reason: "hot_news_match_contract_failed"}
	}
	for _, key := range []string{"hot_entry_id", "interest_summary_id", "match",
		"news_query", "direction"} {
		if _, present := payload[key]; !present {
			return nil, nil, "", "", nil, &TerminalGenerationRejection{Reason: "hot_news_match_contract_failed"}
		}
	}
	var entry *HotListEntry
	for index := range entries {
		if entries[index].EntryID == pyStr(payload["hot_entry_id"]) {
			entry = &entries[index]
			break
		}
	}
	interestID := payload["interest_summary_id"]
	var interest *LongTermProfileInterestInput
	if pyStr(interestID) != "" {
		for _, item := range interests {
			if item.InterestSummaryID == pyStr(interestID) {
				interest = item
				break
			}
		}
	}
	match := pyStr(payload["match"])
	query, queryIsStr := payload["news_query"].(string)
	direction := payload["direction"]
	directionStr, directionIsStr := direction.(string)
	interestGiven := interestID != nil && pyStr(interestID) != ""
	rejected := entry == nil ||
		(match != "matched" && match != "closest") ||
		!queryIsStr || strings.TrimSpace(query) == "" ||
		(interestGiven && interest == nil) ||
		(match == "matched") != (interest != nil) ||
		(match == "matched" && (!directionIsStr || strings.TrimSpace(directionStr) == "")) ||
		(match == "closest" && direction != nil)
	if rejected {
		return nil, nil, "", "", nil, &TerminalGenerationRejection{Reason: "hot_news_match_contract_failed"}
	}
	return entry, interest, match, query, direction, nil
}

// newCandidateID 候选 ID。
func newCandidateID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "candidate"
	}
	return "candidate-" + hex.EncodeToString(buffer)
}
