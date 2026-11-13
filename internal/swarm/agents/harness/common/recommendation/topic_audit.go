package recommendation

import (
	"fmt"
	"math"
	"regexp"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION 审计小票版本（对齐 topic_audit.py:20-22）
const EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION = "topic-starter-v6-external-invocation-audit-v3"

// EXTERNAL_INVOCATION_ATTEMPT_SCHEMA_VERSION 尝试小票版本（对齐 :23-25）
const EXTERNAL_INVOCATION_ATTEMPT_SCHEMA_VERSION = "topic-starter-v6-external-invocation-attempt-v2"

// EXTERNAL_INVOCATION_ATTEMPT_EVENT_TYPE 尝试事件类型（对齐 :26）
const EXTERNAL_INVOCATION_ATTEMPT_EVENT_TYPE = "external_invocation_attempt"

// EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_SCHEMA_VERSION 快照版本（对齐 :27-29）
const EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_SCHEMA_VERSION = "topic-starter-v6-external-invocation-audit-snapshot-v1"

// EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_EVENT_TYPE 快照事件类型（对齐 :30-32）
const EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_EVENT_TYPE = "external_invocation_audit_snapshot"

// 审计支持的操作集合（对齐 :33-37）
var (
	// auditOperations 终局操作集合
	auditOperations = []string{"search", "fetch", "extractor"}
	// auditAttemptOperations 尝试操作集合（含 ark）
	auditAttemptOperations = []string{"ark", "search", "fetch", "extractor"}
	// failureReasonPattern failure_reason 必须是机器可读码
	failureReasonPattern = regexp.MustCompile(`[a-z0-9][a-z0-9_.-]{0,127}`)
)

// ExternalInvocationAudit 采集一次生成运行的 search/fetch/extractor 小票。
// 调用方只提供不透明非机密 ID；公开方法刻意不含查询、响应体、页面文本、
// 请求头与凭据字段。对齐 Python topic_audit.py:57-298 ExternalInvocationAudit。
type ExternalInvocationAudit struct {
	// store 事件账本
	store *TopicStarterStore
	// policyID 策略 ID
	policyID string
	// inputSnapshotID 输入快照 ID
	inputSnapshotID string
	// contextScope 上下文 scope
	contextScope string
	// visitID 访问 ID
	visitID string
	// generationAttemptID 当前后台生成尝试 ID（⤴️ 31b 经 currentBackgroundGenerationAttemptID）
	generationAttemptID string
	// snapshotSequence 快照序号
	snapshotSequence int
	// attempts operation_id → (operation, request_id, candidate_id)
	attempts map[string][3]string
	// completedOperationIds 已完结 operation 集合
	completedOperationIds map[string]bool
	// invocations 已完结调用清单
	invocations []map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExternalInvocationAudit 构造审计器。对齐 Python :64-88 __init__。
func NewExternalInvocationAudit(store *TopicStarterStore, policyID, inputSnapshotID, contextScope, visitID string) (*ExternalInvocationAudit, error) {
	if store == nil {
		return nil, fmt.Errorf("store must be a TopicStarterStore")
	}
	for _, v := range []string{policyID, inputSnapshotID, visitID} {
		if !auditNonempty(v) {
			return nil, fmt.Errorf("policy_id, input_snapshot_id, and visit_id must be non-empty")
		}
	}
	if !auditValidScope(contextScope) {
		return nil, fmt.Errorf("context_scope must be global_new_chat or project:<id>")
	}
	return &ExternalInvocationAudit{
		store:                 store,
		policyID:              policyID,
		inputSnapshotID:       inputSnapshotID,
		contextScope:          contextScope,
		visitID:               visitID,
		generationAttemptID:   currentBackgroundGenerationAttemptID(),
		attempts:              make(map[string][3]string),
		completedOperationIds: make(map[string]bool),
	}, nil
}

// RecordAttempt 外部工作开始前立刻登记一条脱敏小票。
// 对齐 Python :90-151 record_attempt。
func (a *ExternalInvocationAudit) RecordAttempt(operation, operationID, requestID, componentID, providerID, candidateID string) error {
	if !auditIn(auditAttemptOperations, operation) {
		return fmt.Errorf("operation must be ark, search, fetch, or extractor")
	}
	for _, v := range []string{operationID, requestID, componentID, providerID} {
		if !auditNonempty(v) {
			return fmt.Errorf("operation_id, request_id, component_id, and provider_id must be non-empty")
		}
	}
	if operation == "ark" {
		if candidateID != "" && !auditNonempty(candidateID) {
			return fmt.Errorf("candidate_id must be non-empty when provided")
		}
	} else if !auditNonempty(candidateID) {
		return fmt.Errorf("candidate_id must be non-empty for search, fetch, and extractor")
	}
	payload := map[string]any{
		"schema_version":    EXTERNAL_INVOCATION_ATTEMPT_SCHEMA_VERSION,
		"operation":         operation,
		"operation_id":      operationID,
		"request_id":        requestID,
		"component_id":      componentID,
		"provider_id":       providerID,
		"candidate_id":      candidateID,
		"input_snapshot_id": a.inputSnapshotID,
		"context_scope":     a.contextScope,
		"visit_id":          a.visitID,
	}
	recorded, err := a.store.RecordEvent(map[string]any{
		"session_id":        a.visitID,
		"slate_id":          operationID,
		"event_type":        EXTERNAL_INVOCATION_ATTEMPT_EVENT_TYPE,
		"topic_id":          requestID,
		"policy_id":         a.policyID,
		"selection_mode":    "external_invocation_attempt",
		"propensity_reason": "not_estimated",
		"payload":           payload,
		"created_at":        float64(time.Now().UnixMilli()) / 1_000,
	})
	if err != nil {
		return err
	}
	if !recorded {
		return fmt.Errorf("external operation already recorded: %s", operationID)
	}
	a.attempts[operationID] = [3]string{operation, requestID, candidateID}
	if a.snapshotSequence == 0 {
		return a.persistSnapshot(operationID, "attempt_started")
	}
	return nil
}

// RecordSuccess 记录一次成功完结。对齐 Python :153-172 record_success。
func (a *ExternalInvocationAudit) RecordSuccess(operation, operationID, providerID, requestID, resultID string, latencyMS float64) error {
	return a.record(operation, operationID, providerID, "success", requestID, resultID, latencyMS, "")
}

// RecordFailure 记录一次失败完结。对齐 Python :174-193 record_failure。
func (a *ExternalInvocationAudit) RecordFailure(operation, operationID, providerID, requestID, failureReason string, latencyMS float64) error {
	return a.record(operation, operationID, providerID, "failure", requestID, "", latencyMS, failureReason)
}

// AsManifestReceipt 当前完结调用的 manifest 小票。对齐 Python :293-298。
func (a *ExternalInvocationAudit) AsManifestReceipt() map[string]any {
	return ExternalInvocationAuditReceipt(a.inputSnapshotID, a.contextScope, a.invocations)
}

// ExternalInvocationAuditReceipt 完结调用的 manifest 形态：按序编号并带计数器。
// 对齐 Python :301-334 external_invocation_audit_receipt。
func ExternalInvocationAuditReceipt(inputSnapshotID, contextScope string, invocations []map[string]any) map[string]any {
	byOperation := map[string]any{}
	for _, operation := range auditOperations {
		byOperation[operation] = map[string]any{"total": 0, "success": 0, "failure": 0}
	}
	success := 0
	failures := 0
	for _, invocation := range invocations {
		status := auditToString(invocation["status"])
		operation := auditToString(invocation["operation"])
		switch status {
		case "success":
			success++
		case "failure":
			failures++
		}
		opCounters := byOperation[operation].(map[string]any)
		opCounters["total"] = opCounters["total"].(int) + 1
		opCounters[status] = opCounters[status].(int) + 1
	}
	numbered := make([]map[string]any, 0, len(invocations))
	for i, invocation := range invocations {
		entry := copyMap(invocation)
		entry["sequence"] = i + 1
		numbered = append(numbered, entry)
	}
	return map[string]any{
		"schema_version":    EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION,
		"input_snapshot_id": inputSnapshotID,
		"context_scope":     contextScope,
		"invocations":       numbered,
		"counters": map[string]any{
			"total":        len(invocations),
			"success":      success,
			"failure":      failures,
			"by_operation": byOperation,
		},
	}
}

// LoadExternalInvocationAuditSnapshot 加载一次确切协调尝试的最新持久安全审计。
// 对齐 Python :337-390 load_external_invocation_audit_snapshot。
func LoadExternalInvocationAuditSnapshot(store *TopicStarterStore, policyID, generationAttemptID string) (map[string]any, error) {
	if store == nil {
		return nil, fmt.Errorf("store must be a TopicStarterStore")
	}
	if !auditNonempty(policyID) || !auditNonempty(generationAttemptID) {
		return nil, fmt.Errorf("policy_id and generation_attempt_id must be non-empty")
	}
	topicID := generationAttemptID
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_EVENT_TYPE,
		TopicID:   &topicID,
		PolicyID:  &policyID,
		Limit:     1_000,
	})
	if err != nil {
		return nil, err
	}
	type snap struct {
		sequence int
		audit    map[string]any
	}
	var snapshots []snap
	for _, row := range rows {
		payload, ok := row["payload"].(map[string]any)
		if !ok {
			continue
		}
		audit, ok := payload["external_invocation_audit"].(map[string]any)
		if !ok {
			continue
		}
		// 对齐 :368-369 bool 永远不是版本或计数
		sequence, ok := auditPlainInt(payload["snapshot_sequence"])
		if !ok {
			continue
		}
		if sequence < 1 {
			continue
		}
		if auditToString(payload["schema_version"]) != EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_SCHEMA_VERSION {
			continue
		}
		if auditToString(payload["generation_attempt_id"]) != generationAttemptID {
			continue
		}
		if auditToString(audit["schema_version"]) != EXTERNAL_INVOCATION_AUDIT_SCHEMA_VERSION {
			continue
		}
		if auditToString(audit["input_snapshot_id"]) != auditToString(payload["input_snapshot_id"]) {
			continue
		}
		if auditToString(audit["context_scope"]) != auditToString(payload["context_scope"]) {
			continue
		}
		if _, ok := audit["invocations"].([]any); !ok {
			if _, ok2 := audit["invocations"].([]map[string]any); !ok2 {
				continue
			}
		}
		if _, ok := audit["counters"].(map[string]any); !ok {
			continue
		}
		snapshots = append(snapshots, snap{sequence: sequence, audit: audit})
	}
	if len(snapshots) == 0 {
		return nil, nil
	}
	best := snapshots[0]
	for _, s := range snapshots[1:] {
		if s.sequence > best.sequence {
			best = s
		}
	}
	return best.audit, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// record 终局小票的公共校验与登记。对齐 Python :195-256 _record。
func (a *ExternalInvocationAudit) record(operation, operationID, providerID, status, requestID, resultID string, latencyMS float64, failureReason string) error {
	if !auditIn(auditOperations, operation) {
		return fmt.Errorf("operation must be search, fetch, or extractor")
	}
	for _, v := range []string{operationID, providerID, requestID} {
		if !auditNonempty(v) {
			return fmt.Errorf("operation_id, provider_id, and request_id must be non-empty")
		}
	}
	if status == "success" && !auditNonempty(resultID) {
		return fmt.Errorf("successful invocation requires result_id")
	}
	if status == "failure" && !failureReasonPattern.MatchString(failureReason) {
		return fmt.Errorf("failure_reason must be a machine-readable code")
	}
	if math.IsNaN(latencyMS) || math.IsInf(latencyMS, 0) || latencyMS < 0 {
		return fmt.Errorf("latency_ms must be finite and non-negative")
	}
	attempt, exists := a.attempts[operationID]
	if !exists || attempt[0] != operation || attempt[1] != requestID {
		return fmt.Errorf("terminal receipt must match its exact recorded attempt")
	}
	candidateID := attempt[2]
	if a.completedOperationIds[operationID] {
		return fmt.Errorf("external operation already completed: %s", operationID)
	}
	a.completedOperationIds[operationID] = true
	a.invocations = append(a.invocations, map[string]any{
		"sequence":          len(a.invocations) + 1,
		"input_snapshot_id": a.inputSnapshotID,
		"context_scope":     a.contextScope,
		"operation":         operation,
		"operation_id":      operationID,
		"provider_id":       providerID,
		"candidate_id":      candidateID,
		"status":            status,
		"request_id":        requestID,
		"result_id":         resultID,
		"latency_ms":        latencyMS,
		"failure_reason":    failureReason,
	})
	return a.persistSnapshot(operationID, "operation_completed")
}

// persistSnapshot 持久化一份审计快照。对齐 Python :258-291 _persist_snapshot。
func (a *ExternalInvocationAudit) persistSnapshot(operationID, phase string) error {
	generationAttemptID := a.generationAttemptID
	if generationAttemptID == "" {
		return nil
	}
	a.snapshotSequence++
	recorded, err := a.store.RecordEvent(map[string]any{
		"session_id":        a.visitID,
		"slate_id":          fmt.Sprintf("%s:%s", operationID, phase),
		"event_type":        EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_EVENT_TYPE,
		"topic_id":          generationAttemptID,
		"policy_id":         a.policyID,
		"selection_mode":    "external_invocation_audit_snapshot",
		"propensity_reason": "not_estimated",
		"payload": map[string]any{
			"schema_version":            EXTERNAL_INVOCATION_AUDIT_SNAPSHOT_SCHEMA_VERSION,
			"generation_attempt_id":     generationAttemptID,
			"snapshot_sequence":         a.snapshotSequence,
			"phase":                     phase,
			"operation_id":              operationID,
			"visit_id":                  a.visitID,
			"input_snapshot_id":         a.inputSnapshotID,
			"context_scope":             a.contextScope,
			"external_invocation_audit": a.AsManifestReceipt(),
		},
		"created_at": float64(time.Now().UnixMilli()) / 1_000,
	})
	if err != nil {
		return err
	}
	if !recorded {
		return fmt.Errorf("external audit snapshot already recorded: %s:%s", operationID, phase)
	}
	return nil
}

// auditNonempty 非空白字符串判定（对齐 :45-46 _nonempty）。
func auditNonempty(value string) bool {
	return value != "" && regexp.MustCompile(`\S`).MatchString(value)
}

// auditValidScope scope 判定（对齐 :49-54 _valid_scope）。
func auditValidScope(value string) bool {
	if value == "global_new_chat" {
		return true
	}
	const prefix = "project:"
	if len(value) > len(prefix) && value[:len(prefix)] == prefix {
		return regexp.MustCompile(`\S`).MatchString(value[len(prefix):])
	}
	return false
}

// auditIn 集合成员判定。
func auditIn(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

// auditPlainInt 整数判定（bool 不是版本或计数，对齐 :40-42 _plain_int）。
func auditPlainInt(value any) (int, bool) {
	if b, ok := value.(bool); ok && b {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		// Go JSON 解码无 int/float 之分：整值 float 接受（D-6 决策先例）
		if v == math.Trunc(v) {
			return int(v), true
		}
	}
	return 0, false
}

// auditToString 安全取字符串。
func auditToString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
