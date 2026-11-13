package recommendation

// topic_pipeline_c3.go 一比一对齐上游 topic_pipeline.py:2710-2989 的
// 摘要物化小票、空结果/绑定/成功小票构造与短意图触发证据助手。
// _attach_interest_sources（:2889-2952）依赖 InterestFeedback.holding_units
// （topic_product.py:6227 ⤴️31c），随 InterestFeedback 移植批次落地。

import (
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// PipelineProjectDomainLane 项目域进度 lane（topic_pipeline.py:220）
const PipelineProjectDomainLane = "project_domain"

// PipelineGlobalRecentLane 全局近期 lane（topic_pipeline.py:221）
const PipelineGlobalRecentLane = "global_recent"

// PipelineHotNewsLane 热点新闻 lane（topic_pipeline.py:222）
const PipelineHotNewsLane = "hot_news"

// ──────────────────────────── 导出函数 ────────────────────────────

// SessionSummaryMaterializationReceipt 会话摘要物化契约小票
// （topic_pipeline.py:2710-2746）。generated 与摘要集不相容时拒绝。
func SessionSummaryMaterializationReceipt(snapshot *V6ContextSnapshot,
	summaries []*SessionSummaryArtifact, generatedSummaryIDs []string,
	summaryConfigVersion string) (map[string]any, error) {
	summaryIDs := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		summaryIDs = append(summaryIDs, summary.SessionSummaryID)
	}
	generated := append([]string(nil), generatedSummaryIDs...)
	generatedSet := map[string]bool{}
	for _, id := range generated {
		generatedSet[id] = true
	}
	rejected := len(generated) != len(generatedSet)
	if !rejected {
		for _, id := range generated {
			if !containsString(summaryIDs, id) {
				rejected = true
				break
			}
		}
	}
	if !rejected && summaryConfigVersion == "" {
		rejected = true
	}
	if !rejected {
		for _, summary := range summaries {
			if summary.SummaryConfigVersion != summaryConfigVersion {
				rejected = true
				break
			}
		}
	}
	if rejected {
		return nil, &TerminalGenerationRejection{Reason: "session_summary_materialization_contract_failed"}
	}
	reused := []string{}
	for _, summaryID := range summaryIDs {
		if !generatedSet[summaryID] {
			reused = append(reused, summaryID)
		}
	}
	return map[string]any{
		"schema_version":                SessionSummaryMaterializationReceiptVersion,
		"input_snapshot_id":             snapshot.InputSnapshotID,
		"context_scope":                 snapshot.ContextScope,
		"summary_config_version":        summaryConfigVersion,
		"generated_session_summary_ids": generated,
		"reused_session_summary_ids":    reused,
	}, nil
}

// EmptyPipelineResult 空产出（topic_pipeline.py:2749-2764）。
func EmptyPipelineResult(triggerKey map[string]any, triggerOrigin, reason string,
	inputSnapshotID string) *BackgroundGenerationResult {
	if inputSnapshotID == "" {
		// Python 默认参数 input_snapshot_id="unavailable"
		inputSnapshotID = "unavailable"
	}
	return &BackgroundGenerationResult{
		Cards: []map[string]any{},
		Manifest: map[string]any{
			"generation_trigger_key":       triggerKey,
			"trigger_origin":               triggerOrigin,
			"generation_input_snapshot_id": inputSnapshotID,
		},
		FailureReason: reason,
	}
}

// PipelineAPIBinding 组件绑定（topic_pipeline.py:2767-2782）。
// 效用与多样性组件走质量决策契约版本，其余走终局契约版本。
func PipelineAPIBinding(componentID, providerID, providerVersion, configVersion string) (*LearnedComponentBinding, error) {
	binding, err := NewLearnedComponentBinding(componentID, providerID, providerVersion, configVersion)
	if err != nil {
		return nil, err
	}
	if componentID == UTILITY_COMPONENT_ID || componentID == DIVERSITY_COMPONENT_ID {
		binding.ContractVersion = "topic-starter-v6-quality-decisions-v2"
	}
	return binding, nil
}

// PipelineSuccessReceipt 成功调用小票（topic_pipeline.py:2785-2798）。
func PipelineSuccessReceipt(binding *LearnedComponentBinding, inputSnapshotID,
	requestID string) (*LearnedComponentReceipt, error) {
	return NewLearnedComponentReceipt(binding, inputSnapshotID, requestID,
		"uncalibrated_api_proxy", "success", "")
}

// PipelineHighIntentKind 高意图行为类型到兴趣簇的映射
// （topic_pipeline.py:2801-2806）；未知行为类型返回空串。
func PipelineHighIntentKind(actionType string) string {
	kind := map[string]string{
		"explicit_goal_preference_task": "new_goal",
		"correction":                    "correction",
		"recent_action":                 "recent_action",
	}[actionType]
	return kind
}

// PipelineScopeMatches 项目 ID 与上下文范围匹配判定
// （topic_pipeline.py:2809-2812）。
func PipelineScopeMatches(projectID any, contextScope string) bool {
	if contextScope == GlobalNewChatScope {
		_, ok := GlobalProjectBuckets[pyStr(projectID)]
		return ok
	}
	return pyStr(projectID) == strings.TrimPrefix(contextScope, "project:")
}

// ShortTriggerEvidenceEventIDs 短意图触发背后的新归因事件
// （topic_pipeline.py:2815-2838）：event_ids 并集加 current−reference 差集。
func ShortTriggerEvidenceEventIDs(reasonEvidenceLineage []map[string]any) map[string]bool {
	eventIDs := map[string]bool{}
	for _, lineage := range reasonEvidenceLineage {
		for _, eventID := range stringListOrNone(lineage["event_ids"]) {
			if eventID != "" {
				eventIDs[eventID] = true
			}
		}
		currentEventIDs := map[string]bool{}
		for _, eventID := range stringListOrNone(lineage["current_event_ids"]) {
			if eventID != "" {
				currentEventIDs[eventID] = true
			}
		}
		referenceEventIDs := map[string]bool{}
		for _, eventID := range stringListOrNone(lineage["reference_event_ids"]) {
			if eventID != "" {
				referenceEventIDs[eventID] = true
			}
		}
		for eventID := range currentEventIDs {
			if !referenceEventIDs[eventID] {
				eventIDs[eventID] = true
			}
		}
	}
	return eventIDs
}

// ShortTriggerNaturalDeltaEventIDs 本次生成未消费的自然触发事件
// （topic_pipeline.py:2841-2857）：优先 pending 血缘，缺失时回落状态血缘
// （仅当存在触发原因）。
func ShortTriggerNaturalDeltaEventIDs(snapshot *V6ContextSnapshot) map[string]bool {
	lineage := snapshot.PendingShortIntentTriggerLineage
	if lineage == nil {
		reasons, _ := snapshot.ShortIntentState["trigger_reasons"].([]any)
		if len(reasons) > 0 {
			lineage = mapListOrNone(snapshot.ShortIntentState["reason_evidence_lineage"])
		}
	}
	shortEventIDs := ShortTriggerEvidenceEventIDs(lineage)
	result := map[string]bool{}
	for _, message := range snapshot.Messages {
		if message.Natural() && shortEventIDs[message.EventID] {
			result[message.EventID] = true
		}
	}
	return result
}

// ExactDeltaLinkedInterestIDs 与增量事件精确关联的选中兴趣
// （topic_pipeline.py:2860-2870）：源事件或源 locator 命中即关联。
func ExactDeltaLinkedInterestIDs(rid *RIDResult, deltaEventIDs map[string]bool) map[string]bool {
	linked := map[string]bool{}
	for _, interest := range rid.TriggerInterests() {
		sourceEventIDs := map[string]bool{}
		for _, eventID := range interest.SourceEventIDs {
			sourceEventIDs[eventID] = true
		}
		locatorEventIDs := map[string]bool{}
		for _, locator := range interest.SourceLocators {
			locatorEventIDs[locator.EventID] = true
		}
		for eventID := range deltaEventIDs {
			if sourceEventIDs[eventID] || locatorEventIDs[eventID] {
				linked[interest.InterestSummaryID] = true
				break
			}
		}
	}
	return linked
}

// InterestSummaryTextByID 兴趣与摘要的 ID→文本索引
// （topic_pipeline.py:2873-2886）：画像兴趣在前，会话摘要在后覆盖。
func InterestSummaryTextByID(snapshot *V6ContextSnapshot,
	summaries []*SessionSummaryArtifact) map[string]string {
	result := map[string]string{}
	for _, interest := range snapshot.LongTermProfileInterests {
		result[interest.InterestSummaryID] = interest.SummaryText
	}
	for _, summary := range summaries {
		result[summary.SessionSummaryID] = summary.SummaryText
	}
	return result
}

// WithoutIdentifiers 剔除标识符后的规范化文本（topic_pipeline.py:2955-2959）：
// 长标识符优先替换，折叠空白，去首尾分隔符。
func WithoutIdentifiers(value any, identifiers []string) string {
	text := strings.TrimSpace(pyStr(value))
	sorted := append([]string(nil), identifiers...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, identifier := range sorted {
		text = strings.ReplaceAll(text, identifier, "")
	}
	text = strings.Join(strings.Fields(text), " ")
	return strings.Trim(text, " ,，;；:：-/")
}

// UnitSourceSessionIDs companion 的规范源 locator 会话集合
// （topic_pipeline.py:2962-2974）：仅收两元素且首元素非空的列表 locator。
func UnitSourceSessionIDs(companion map[string]any) map[string]bool {
	rawLocators, _ := companion["canonical_source_locators"].([]any)
	sessionIDs := map[string]bool{}
	for _, raw := range rawLocators {
		locator, ok := raw.([]any)
		if !ok || len(locator) != 2 {
			continue
		}
		if strings.TrimSpace(pyStr(locator[0])) == "" {
			continue
		}
		sessionIDs[pyStr(locator[0])] = true
	}
	return sessionIDs
}

// SummarizableUserTurn 上下文快照能成一条消息的用户轮
// （topic_pipeline.py:2977-2988）：正文非空，且为自然输入或可归因协助输入。
func SummarizableUserTurn(record map[string]any) bool {
	content, ok := record["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return false
	}
	_, hasInputOrigin := record["input_origin"]
	_, hasAssistanceOrigin := record["assistance_origin"]
	if !hasInputOrigin && !hasAssistanceOrigin {
		return true
	}
	assistanceOrigin := pyStr(record["assistance_origin"])
	inputOrigin := pyStr(record["input_origin"])
	if inputOrigin == "manual_free_text" && assistanceOrigin == "none" {
		return true
	}
	switch assistanceOrigin {
	case "", "none", "unknown":
		return false
	}
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringListOrNone 元素列表到字符串列表（非列表返回 nil）。
func stringListOrNone(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, pyStr(item))
	}
	return out
}

// mapListOrNone 映射列表（非列表返回 nil）。
func mapListOrNone(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
