package recommendation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// topic_rid.go IceBreaker 特定兴趣回访标签的版本化 Ark 匹配器。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_rid.py
// （465 行，一比一移植；async → ctx 传参等价转换）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// RidSummaryPairMatcherID 匹配器 ID（topic_rid.py:23）。
	RidSummaryPairMatcherID = "icebreaker-specific-interest-ark-api-proxy"
	// RidSummaryPairMatcherVersion 匹配器版本（:24）。
	RidSummaryPairMatcherVersion = "icebreaker-table5-summary-pair-prompt-v1"
	// RidSummaryPairMatchStage 阶段名（:25）。
	RidSummaryPairMatchStage = "v6_rid_summary_pair_matcher"
	// RidSummaryPairMatchRequestSchemaVersion 请求 schema 版本（:26-28）。
	RidSummaryPairMatchRequestSchemaVersion = "topic-starter-v6-rid-summary-pair-match-request-v1"
	// RidSummaryPairMatchReceiptSchemaVersion 小票 schema 版本（:29-31）。
	RidSummaryPairMatchReceiptSchemaVersion = "topic-starter-v6-rid-summary-pair-match-receipt-v1"
	// RidSummaryPairMatchAttemptSchemaVersion 尝试 schema 版本（:32-34）。
	RidSummaryPairMatchAttemptSchemaVersion = "topic-starter-v6-rid-summary-pair-match-attempt-v1"
	// RidSummaryPairMatchAttemptEventType 尝试事件类型（:35）。
	RidSummaryPairMatchAttemptEventType = "rid_summary_pair_match_attempt"
	// RidSummaryPairMatchReceiptEventType 小票事件类型（:36）。
	RidSummaryPairMatchReceiptEventType = "rid_summary_pair_match_receipt"
)

// ridSummaryPairMatchResponseSchema 响应 schema（:38-46）。
var ridSummaryPairMatchResponseSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"label": map[string]any{"type": "string", "enum": []string{"MATCH", "NO_MATCH"}},
	},
	"required":             []string{"label"},
	"additionalProperties": false,
}

// ridSummaryPairSystemPrompt 系统提示词（对齐 :48-54 _SYSTEM_PROMPT 逐字）。
const ridSummaryPairSystemPrompt = "You label IceBreaker interest revisits from two session summaries.\n" +
	"Use only the supplied summaries. Return MATCH only when both refer to the same " +
	"specific interest or subject; paraphrases are allowed. A shared generic theme such " +
	"as music, travel, technology, or entertainment is insufficient unless the specific " +
	"subject also aligns. Otherwise return NO_MATCH. Return only the required JSON object."

// ──────────────────────────── 导出函数 ────────────────────────────

// RidSummaryPairResponseSchema 阶段响应 schema 出口（对齐 :108-112
// _response_schema，仅认本阶段）。
func RidSummaryPairResponseSchema(stage, _ string) (map[string]any, error) {
	if stage != RidSummaryPairMatchStage {
		return nil, fmt.Errorf("unknown RID summary pair matcher stage")
	}
	return ridSummaryPairMatchResponseSchema, nil
}

// RidSummaryPairInvocationLineage 调用血缘（对齐 :114-135 _invocation_lineage）。
func RidSummaryPairInvocationLineage(stage, payload string, _ any) (map[string]any, error) {
	if stage != RidSummaryPairMatchStage {
		return nil, fmt.Errorf("unknown RID summary pair matcher stage")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, fmt.Errorf("summary pair matcher request contract failed")
	}
	earlier, _ := parsed["earlier_summary"].(map[string]any)
	later, _ := parsed["later_summary"].(map[string]any)
	if earlier == nil || later == nil {
		return nil, fmt.Errorf("summary pair matcher request contract failed")
	}
	return map[string]any{
		"binding_scope":        "rid_summary_pair",
		"component_request_id": parsed["request_id"],
		"earlier_summary_id":   earlier["session_summary_id"],
		"later_summary_id":     later["session_summary_id"],
	}, nil
}

// BuildArkSpecificInterestMatcher 复用 Ark 阶段适配器构造匹配器
// （对齐 :447-465 build_ark_specific_interest_matcher）。
func BuildArkSpecificInterestMatcher(store *TopicStarterStore, config *ArkBackgroundConfig,
	clientFactory ChatClientFactory) (*ArkSpecificInterestMatcher, error) {
	if err := validateRidMatcherConfig(config); err != nil {
		return nil, err
	}
	invokeStage := BuildArkStageInvoker(
		config,
		clientFactory,
		func(stage, payload string) map[string]any {
			schema, err := RidSummaryPairResponseSchema(stage, payload)
			if err != nil {
				return nil
			}
			return schema
		},
		true,
		func(receipt map[string]any) { PersistProviderInvocation(store, receipt) },
		nil,
		nil,
		func(stage, payload string, result any) map[string]any {
			lineage, err := RidSummaryPairInvocationLineage(stage, payload, result)
			if err != nil {
				return nil
			}
			return lineage
		},
		false,
	)
	return &ArkSpecificInterestMatcher{
		store:       store,
		config:      config,
		invokeStage: invokeStage,
	}, nil
}

// ──────────────────────────── 结构体 ────────────────────────────

// ArkSpecificInterestMatcher 匹配一对冻结的同一用户摘要并持久绑定厂商小票
// （对齐 :175-194 class ArkSpecificInterestMatcher）。
type ArkSpecificInterestMatcher struct {
	// MatcherID / MatcherVersion 身份常量（对齐类属性）
	MatcherID      string
	MatcherVersion string

	store       *TopicStarterStore
	config      *ArkBackgroundConfig
	invokeStage TopicStageInvoker
}

// NewArkSpecificInterestMatcher 构造（对齐 __init__：配置与依赖硬校验）。
func NewArkSpecificInterestMatcher(store *TopicStarterStore, config *ArkBackgroundConfig,
	invokeStage TopicStageInvoker) (*ArkSpecificInterestMatcher, error) {
	if err := validateRidMatcherConfig(config); err != nil {
		return nil, err
	}
	if store == nil || invokeStage == nil {
		return nil, fmt.Errorf("Ark matcher requires a store and stage invoker")
	}
	return &ArkSpecificInterestMatcher{
		MatcherID:      RidSummaryPairMatcherID,
		MatcherVersion: RidSummaryPairMatcherVersion,
		store:          store,
		config:         config,
		invokeStage:    invokeStage,
	}, nil
}

// Match 匹配一对摘要并落库小票；返回是否同一具体兴趣
// （对齐 :345-440 match）。
func (m *ArkSpecificInterestMatcher) Match(earlier, later *SessionSummaryArtifact) (bool, error) {
	if earlier == nil || later == nil ||
		earlier.SessionID == later.SessionID ||
		earlier.ContextScope != later.ContextScope ||
		later.CompletedAt <= earlier.CompletedAt {
		return false, fmt.Errorf("summary pair matcher input contract failed")
	}
	expectedModel := m.config.ExpectedModel
	if expectedModel == "" {
		expectedModel = m.config.Model
	}
	providerConfig := m.config.SafeProvenance()
	requestID := ridSummaryPairRequestID(earlier, later, providerConfig)
	cached, cacheErr := m.cachedLabel(earlier, later, requestID)
	if cacheErr != nil {
		return false, cacheErr
	}
	if cached != nil {
		return *cached, nil
	}
	operationID, err := m.recordAttempt(earlier, later, requestID)
	if err != nil {
		return false, err
	}
	requestPayload, err := ridSummaryPairRequestPayload(earlier, later, requestID)
	if err != nil {
		return false, err
	}
	result, invokeErr := m.invokeStage.Invoke(context.Background(),
		RidSummaryPairMatchStage, ridSummaryPairSystemPrompt, requestPayload)
	if invokeErr != nil {
		return false, invokeErr
	}
	labelMap, isMap := result.(map[string]any)
	if !isMap || len(labelMap) != 1 {
		return false, fmt.Errorf("summary pair matcher output contract failed")
	}
	label, _ := labelMap["label"].(string)
	if label != "MATCH" && label != "NO_MATCH" {
		return false, fmt.Errorf("summary pair matcher output contract failed")
	}
	providerReceipt, err := m.providerReceipt(requestID,
		earlier.SessionSummaryID, later.SessionSummaryID)
	if err != nil {
		return false, err
	}
	payload := map[string]any{
		"schema_version":         RidSummaryPairMatchReceiptSchemaVersion,
		"operation_id":           operationID,
		"request_id":             requestID,
		"matcher_id":             RidSummaryPairMatcherID,
		"matcher_version":        RidSummaryPairMatcherVersion,
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"earlier_summary_id":     earlier.SessionSummaryID,
		"later_summary_id":       later.SessionSummaryID,
		"provider_id":            "ark",
		"model":                  expectedModel,
		"match_label":            label == "MATCH",
		"provider_config":        providerConfig,
		"provider_receipt":       providerReceipt,
	}
	recorded, _ := m.store.RecordEvent(map[string]any{
		"session_id":        later.SessionID,
		"slate_id":          requestID,
		"event_type":        RidSummaryPairMatchReceiptEventType,
		"topic_id":          requestID,
		"policy_id":         ProductPolicyID,
		"selection_mode":    "training_data_plane",
		"propensity_reason": "not_applicable",
		"payload":           payload,
		"created_at":        float64(time.Now().UnixNano()) / 1e9,
	})
	if !recorded {
		cached, cacheErr := m.cachedLabel(earlier, later, requestID)
		if cacheErr != nil || cached == nil || *cached != (label == "MATCH") {
			return false, fmt.Errorf("summary pair matcher receipt conflict")
		}
	}
	return label == "MATCH", nil
}

// Aclose 关闭底层 invoker（对齐 :441-444 aclose；存在即调）。
func (m *ArkSpecificInterestMatcher) Aclose() error {
	if closer, ok := m.invokeStage.(interface{ Aclose() error }); ok {
		return closer.Aclose()
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateRidMatcherConfig 匹配器配置硬校验（对齐 :68-96 _validate_config）。
func validateRidMatcherConfig(config *ArkBackgroundConfig) error {
	if config == nil {
		return fmt.Errorf("Ark matcher config is invalid")
	}
	expectedModel := config.ExpectedModel
	if expectedModel == "" {
		expectedModel = config.Model
	}
	temperature := config.EffectiveTemperature()
	topP := config.EffectiveTopP()
	invalid := config.APIKey == "" || strings.TrimSpace(config.APIKey) == "" ||
		config.Model == "" || strings.TrimSpace(config.Model) == "" ||
		expectedModel == "" ||
		config.BaseURL == "" || !strings.HasPrefix(config.BaseURL, "https://") ||
		config.ContextWindowTokens <= 0 ||
		math.IsNaN(temperature) || math.IsInf(temperature, 0) ||
		temperature < 0 || temperature > 2 ||
		math.IsNaN(topP) || math.IsInf(topP, 0) ||
		topP < 0 || topP > 1
	if invalid {
		return fmt.Errorf("Ark matcher config is invalid")
	}
	return nil
}

// ridSummaryPairRequestID 身份摘要请求 ID（对齐 :98-123 _request_id）。
func ridSummaryPairRequestID(earlier, later *SessionSummaryArtifact, providerConfig map[string]any) string {
	identity := map[string]any{
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"matcher_id":             RidSummaryPairMatcherID,
		"matcher_version":        RidSummaryPairMatcherVersion,
		"provider_config":        providerConfig,
		"earlier_summary_id":     earlier.SessionSummaryID,
		"earlier_summary_text":   earlier.SummaryText,
		"later_summary_id":       later.SessionSummaryID,
		"later_summary_text":     later.SummaryText,
	}
	digest := sha256.Sum256([]byte(pyJSONSorted(identity)))
	return fmt.Sprintf("rid-summary-pair-match:%s", hex.EncodeToString(digest[:]))
}

// ridSummaryPairRequestPayload 请求载荷（对齐 :125-141 _request_payload）。
func ridSummaryPairRequestPayload(earlier, later *SessionSummaryArtifact, requestID string) (string, error) {
	payload := map[string]any{
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"request_id":             requestID,
		"matcher_id":             RidSummaryPairMatcherID,
		"matcher_version":        RidSummaryPairMatcherVersion,
		"earlier_summary": map[string]any{
			"session_summary_id": earlier.SessionSummaryID,
			"summary_text":       earlier.SummaryText,
		},
		"later_summary": map[string]any{
			"session_summary_id": later.SessionSummaryID,
			"summary_text":       later.SummaryText,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// cachedLabel 已落库小票回读缓存（对齐 :196-271 _cached_label）。
func (m *ArkSpecificInterestMatcher) cachedLabel(earlier, later *SessionSummaryArtifact, requestID string) (*bool, error) {
	rows, err := m.store.QueryEvents(QueryEventsOptions{
		EventType: RidSummaryPairMatchReceiptEventType,
		TopicID:   &requestID,
		PolicyID:  &[]string{ProductPolicyID}[0],
		Limit:     2,
	})
	if err != nil || len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 || pyStr(rows[0]["slate_id"]) != requestID {
		return nil, fmt.Errorf("summary pair matcher receipt is ambiguous")
	}
	payload, _ := rows[0]["payload"].(map[string]any)
	expectedModel := m.config.ExpectedModel
	if expectedModel == "" {
		expectedModel = m.config.Model
	}
	expected := map[string]any{
		"schema_version":         RidSummaryPairMatchReceiptSchemaVersion,
		"request_id":             requestID,
		"matcher_id":             RidSummaryPairMatcherID,
		"matcher_version":        RidSummaryPairMatcherVersion,
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"earlier_summary_id":     earlier.SessionSummaryID,
		"later_summary_id":       later.SessionSummaryID,
		"provider_id":            "ark",
		"model":                  expectedModel,
	}
	expectedFields := map[string]bool{}
	for key := range expected {
		expectedFields[key] = true
	}
	for _, extra := range []string{"operation_id", "match_label", "provider_config", "provider_receipt"} {
		expectedFields[extra] = true
	}
	providerReceipt, _ := payload["provider_receipt"].(map[string]any)
	rejected := payload == nil ||
		!sameStringKeys(payload, expectedFields) ||
		!mapValuesEqual(payload, expected) ||
		!nonEmptyStringAny(payload["operation_id"]) ||
		!isBoolAny(payload["match_label"]) ||
		!anyMapEqual(payload["provider_config"], m.config.SafeProvenance()) ||
		!m.validProviderReceipt(providerReceipt, requestID,
			earlier.SessionSummaryID, later.SessionSummaryID)
	if rejected {
		return nil, fmt.Errorf("summary pair matcher receipt contract failed")
	}
	matchLabel, _ := payload["match_label"].(bool)
	return &matchLabel, nil
}

// validProviderReceipt 厂商小票校验（对齐 :273-300 _valid_provider_receipt）。
func (m *ArkSpecificInterestMatcher) validProviderReceipt(receipt map[string]any, requestID,
	earlierSummaryID, laterSummaryID string) bool {
	if receipt == nil {
		return false
	}
	expectedModel := m.config.ExpectedModel
	if expectedModel == "" {
		expectedModel = m.config.Model
	}
	lineage, _ := receipt["lineage"].(map[string]any)
	return pyStr(receipt["stage"]) == RidSummaryPairMatchStage &&
		pyStr(receipt["response_object"]) == "chat.completion" &&
		pyStr(receipt["actual_model"]) == expectedModel &&
		pyStr(receipt["finish_reason"]) == "stop" &&
		pyStr(receipt["structured_output_mode"]) == "json_schema_strict" &&
		nonEmptyStringAny(receipt["provider_response_id"]) &&
		lineage != nil &&
		pyStr(lineage["binding_scope"]) == "rid_summary_pair" &&
		pyStr(lineage["component_request_id"]) == requestID &&
		pyStr(lineage["earlier_summary_id"]) == earlierSummaryID &&
		pyStr(lineage["later_summary_id"]) == laterSummaryID
}

// recordAttempt 落一条尝试事件（对齐 :302-328 _record_attempt）。
func (m *ArkSpecificInterestMatcher) recordAttempt(earlier, later *SessionSummaryArtifact, requestID string) (string, error) {
	operationID := fmt.Sprintf("rid-summary-pair-match-attempt:%s", newUUIDHex())
	expectedModel := m.config.ExpectedModel
	if expectedModel == "" {
		expectedModel = m.config.Model
	}
	payload := map[string]any{
		"schema_version":         RidSummaryPairMatchAttemptSchemaVersion,
		"operation_id":           operationID,
		"request_id":             requestID,
		"matcher_id":             RidSummaryPairMatcherID,
		"matcher_version":        RidSummaryPairMatcherVersion,
		"request_schema_version": RidSummaryPairMatchRequestSchemaVersion,
		"earlier_summary_id":     earlier.SessionSummaryID,
		"later_summary_id":       later.SessionSummaryID,
		"provider_id":            "ark",
		"model":                  expectedModel,
	}
	recorded, _ := m.store.RecordEvent(map[string]any{
		"session_id":        later.SessionID,
		"slate_id":          operationID,
		"event_type":        RidSummaryPairMatchAttemptEventType,
		"topic_id":          requestID,
		"policy_id":         ProductPolicyID,
		"selection_mode":    "training_data_plane",
		"propensity_reason": "not_applicable",
		"payload":           payload,
		"created_at":        float64(time.Now().UnixNano()) / 1e9,
	})
	if !recorded {
		return "", fmt.Errorf("summary pair matcher attempt was not recorded")
	}
	return operationID, nil
}

// providerReceipt 从 invoker 调用小票中取匹配的最后一张（对齐 :330-343
// _provider_receipt）。
func (m *ArkSpecificInterestMatcher) providerReceipt(requestID, earlierSummaryID, laterSummaryID string) (map[string]any, error) {
	receipts := m.invokeStage.InvocationReceipts()
	var matching []map[string]any
	for _, receipt := range receipts {
		if m.validProviderReceipt(receipt, requestID, earlierSummaryID, laterSummaryID) {
			matching = append(matching, receipt)
		}
	}
	if len(matching) == 0 {
		return nil, fmt.Errorf("summary pair matcher provider receipt missing")
	}
	receipt := matching[len(matching)-1]
	clone := make(map[string]any, len(receipt))
	for key, value := range receipt {
		clone[key] = value
	}
	return clone, nil
}

// nonEmptyStringAny 非空字符串判定。
func nonEmptyStringAny(value any) bool {
	s, ok := value.(string)
	return ok && s != ""
}

// isBoolAny 布尔判定。
func isBoolAny(value any) bool {
	_, ok := value.(bool)
	return ok
}

// pyJSONSorted 键排序、无空格紧凑 JSON（等价 Python
// json.dumps(..., ensure_ascii=False, sort_keys=True, separators=(",", ":"))）。
// Go json.Encoder 对 map 键天然有序；marshalNoEscape 关闭 HTML 转义，
// 与 Python 不转义 <>& 的行为一致（复用既有助手，不另造序列化）。
func pyJSONSorted(value any) string {
	encoded, err := marshalNoEscape(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// sameStringKeys 键集合恰等（等价 Python set(payload) != expected_fields）。
func sameStringKeys(payload map[string]any, expected map[string]bool) bool {
	if len(payload) != len(expected) {
		return false
	}
	for key := range payload {
		if !expected[key] {
			return false
		}
	}
	return true
}

// mapValuesEqual 键值对等（等价 any(payload.get(key) != value for ...) 取反）。
func mapValuesEqual(payload, expected map[string]any) bool {
	for key, value := range expected {
		if !anyEqual(payload[key], value) {
			return false
		}
	}
	return true
}
