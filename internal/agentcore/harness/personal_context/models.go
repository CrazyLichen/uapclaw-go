package personal_context

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// safeSegmentPattern 安全路径段正则。
// Python: models.py:12 _SAFE_SEGMENT
var safeSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// PersonalContextStatus PersonalContext 运行态的有界、无凭据快照。
// Python: models.py:59 PersonalContextStatus
// 偏差 D-24.3a-5：pydantic 校验收敛为构造函数 + 校验方法显式调用，步骤顺序逐条对齐。
type PersonalContextStatus struct {
	// Configured 是否已配置
	Configured bool `json:"configured"`
	// CollectionEnabled 是否启用采集
	CollectionEnabled bool `json:"collection_enabled"`
	// AgentUseEnabled 是否启用 agent 消费
	AgentUseEnabled bool `json:"agent_use_enabled"`
	// State 运行状态（PersonalContextState 枚举值）
	State string `json:"state"`
	// PipelineRunning pipeline 是否运行中
	PipelineRunning bool `json:"pipeline_running"`
	// PipelineQueueSize pipeline 队列长度
	PipelineQueueSize int `json:"pipeline_queue_size"`
	// FetchServiceStates 各 fetch 服务状态
	FetchServiceStates map[string]string `json:"fetch_service_states"`
	// FetchServiceErrors 各 fetch 服务错误
	FetchServiceErrors map[string]string `json:"fetch_service_errors"`
	// FetchRunProgress 各 fetch run 进度
	FetchRunProgress map[string]map[string]any `json:"fetch_run_progress"`
	// ContextRoot Context 根目录
	ContextRoot string `json:"context_root"`
	// ContextReady Context 是否就绪
	ContextReady bool `json:"context_ready"`
	// LastError 最近错误
	LastError map[string]any `json:"last_error"`
}

// RawChangeItem 交给 Context pipeline 的一条 provider 变更。
// Python: models.py:237 RawChangeItem
type RawChangeItem struct {
	// LogicalID 逻辑标识
	LogicalID string `json:"logical_id"`
	// RevisionID 修订标识
	RevisionID string `json:"revision_id"`
	// Operation 操作（恒 "upsert"）
	Operation string `json:"operation"`
	// Title 标题
	Title *string `json:"title"`
	// Content 正文
	Content *string `json:"content"`
	// OriginalRef 原始引用
	OriginalRef string `json:"original_ref"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
	// RawSnapshot 原始快照
	RawSnapshot any `json:"raw_snapshot"`
}

// FetchBatch 一次 fetch run 中 provider 产出的有界批次。
// Python: models.py:298 FetchBatch
type FetchBatch struct {
	// BatchID 批次标识
	BatchID string `json:"batch_id"`
	// Items 批内变更项
	Items []RawChangeItem `json:"items"`
	// AttemptedCount 尝试条目数
	AttemptedCount int `json:"attempted_count"`
	// SuccessOffsets 成功下标
	SuccessOffsets []int `json:"success_offsets"`
	// SkippedOffsets 跳过下标
	SkippedOffsets []int `json:"skipped_offsets"`
	// Failures 失败登记
	Failures []map[string]any `json:"failures"`
	// NextCursor 下一游标
	NextCursor map[string]any `json:"next_cursor"`
	// MaterializedSourcePath 物化候选路径
	MaterializedSourcePath *string `json:"materialized_source_path"`
	// MaterializedRevision 物化修订
	MaterializedRevision *string `json:"materialized_revision"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// PersonalContext 运行状态枚举。
// Python: models.py:13 _PersonalContextState
const (
	PersonalContextStateCreated    = "CREATED"
	PersonalContextStateConfigured = "CONFIGURED"
	PersonalContextStateStarting   = "STARTING"
	PersonalContextStateRunning    = "RUNNING"
	PersonalContextStateStopping   = "STOPPING"
	PersonalContextStateStopped    = "STOPPED"
	PersonalContextStateFailed     = "FAILED"
)

// Fetch 服务状态枚举。Python: models.py:14 _FetchState
const (
	FetchStateStopped  = "STOPPED"
	FetchStateStarting = "STARTING"
	FetchStateRunning  = "RUNNING"
	FetchStateStopping = "STOPPING"
	FetchStateFailed   = "FAILED"
)

// ──────────────────────────── 常量 ────────────────────────────

// fetchRunStates 合法 run 状态集合。Python: models.py:15-23 _FETCH_RUN_STATES
var fetchRunStates = map[string]struct{}{
	"idle":              {},
	"running":           {},
	"stopping":          {},
	"succeeded":         {},
	"partial_succeeded": {},
	"failed":            {},
	"cancelled":         {},
}

// fetchRunProgressFields 进度字段白名单。
// Python: models.py:24-35 _FETCH_RUN_PROGRESS_FIELDS
var fetchRunProgressFields = []string{
	"service_id", "run_state", "progress_percent", "total_items", "completed_items",
	"failed_items", "quarantined_items", "item_errors", "omitted_item_errors", "last_error",
}

// fetchRunResultFields 结果字段白名单。
// Python: models.py:36 _FETCH_RUN_RESULT_FIELDS
var fetchRunResultFields = []string{"created_node_count", "updated_node_count", "no_new_content"}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPersonalContextStatus 构造并校验运行态快照（before → 字段 → 校验链顺序对齐）。
// Python: models.py:59 PersonalContextStatus（model_config extra=forbid frozen）
func NewPersonalContextStatus(value map[string]any) (*PersonalContextStatus, error) {
	copied := CopyNestedStatusState(value) // Python: model_validator mode=before copy_nested_state
	status := &PersonalContextStatus{}
	if err := decodeStatusFields(copied, status); err != nil {
		return nil, err
	}
	if err := status.ValidateFetchRunProgress(); err != nil {
		return nil, err
	}
	if err := status.ValidateLastError(); err != nil {
		return nil, err
	}
	if err := status.BoundErrorMessages(); err != nil {
		return nil, err
	}
	return status, nil
}

// NewRawChangeItem 构造并校验一条变更项。
// Python: models.py:237 RawChangeItem
func NewRawChangeItem(value map[string]any) (*RawChangeItem, error) {
	copied := CopyRawChangeInput(value) // Python: model_validator mode=before copy_input
	item := &RawChangeItem{}
	if err := decodeRawChangeFields(copied, item); err != nil {
		return nil, err
	}
	if err := item.RejectBlankIdentifiers(); err != nil {
		return nil, err
	}
	if err := item.BoundContent(); err != nil {
		return nil, err
	}
	if err := item.BoundMetadata(); err != nil {
		return nil, err
	}
	if err := item.BoundSnapshot(); err != nil {
		return nil, err
	}
	if err := item.ValidateOperationPairing(); err != nil {
		return nil, err
	}
	return item, nil
}

// NewFetchBatch 构造并校验一个批次。
// Python: models.py:298 FetchBatch
func NewFetchBatch(value map[string]any) (*FetchBatch, error) {
	copied, err := CopyFetchBatchInput(value) // Python: model_validator mode=before copy_input
	if err != nil {
		return nil, err
	}
	batch := &FetchBatch{}
	if err := decodeFetchBatchFields(copied, batch); err != nil {
		return nil, err
	}
	if err := batch.ValidateBatchID(); err != nil {
		return nil, err
	}
	if err := batch.ValidateCursor(); err != nil {
		return nil, err
	}
	if err := batch.ValidateMaterializedPath(); err != nil {
		return nil, err
	}
	if err := batch.ValidateMaterializedRevision(); err != nil {
		return nil, err
	}
	if err := batch.ValidateMaterializedPair(); err != nil {
		return nil, err
	}
	return batch, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jsonSize 校验值可 JSON 编码且可选有界。
// Python: models.py:41 _json_size
func jsonSize(value any, fieldName string, maxBytes int) error {
	if _, err := json.Marshal(value); err != nil {
		return fmt.Errorf("%s must be JSON serializable", fieldName)
	}
	if maxBytes > 0 {
		encoded, _ := json.Marshal(value)
		if len(encoded) > maxBytes {
			return fmt.Errorf("%s exceeds %d bytes", fieldName, maxBytes)
		}
	}
	return nil
}

// copyMapping 深拷贝映射并校验 JSON 可编码。
// Python: models.py:51 _copy_mapping
func copyMapping(value any, fieldName string) (map[string]any, error) {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", fieldName)
	}
	copied := deepCopyJSON(mapping)
	if err := jsonSize(copied, fieldName, 0); err != nil {
		return nil, err
	}
	return copied, nil
}

// deepCopyJSON 经 JSON 往返实现深拷贝。Python: deepcopy（对 JSON 树语义等价）
func deepCopyJSON(value map[string]any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	copied := map[string]any{}
	if json.Unmarshal(encoded, &copied) != nil {
		return value
	}
	return copied
}

// decodeStatusFields 解析运行态字段（字段顺序 = Python 声明顺序）。
func decodeStatusFields(value map[string]any, status *PersonalContextStatus) error {
	boolFields := []struct {
		key    string
		target *bool
	}{
		{"configured", &status.Configured},
		{"collection_enabled", &status.CollectionEnabled},
		{"agent_use_enabled", &status.AgentUseEnabled},
		{"pipeline_running", &status.PipelineRunning},
		{"context_ready", &status.ContextReady},
	}
	for _, field := range boolFields {
		raw, ok := value[field.key]
		if !ok {
			return fmt.Errorf("%s is required", field.key)
		}
		checked, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("%s must be a boolean", field.key)
		}
		*field.target = checked
	}
	state, ok := value["state"].(string)
	if !ok {
		return fmt.Errorf("state must be a string")
	}
	switch state {
	case PersonalContextStateCreated, PersonalContextStateConfigured, PersonalContextStateStarting,
		PersonalContextStateRunning, PersonalContextStateStopping, PersonalContextStateStopped,
		PersonalContextStateFailed:
		status.State = state
	default:
		return fmt.Errorf("state must be one of CREATED/CONFIGURED/STARTING/RUNNING/STOPPING/STOPPED/FAILED")
	}
	queueSize, ok := value["pipeline_queue_size"].(float64)
	if !ok || queueSize < 0 {
		return fmt.Errorf("pipeline_queue_size must be a non-negative integer")
	}
	status.PipelineQueueSize = int(queueSize)

	states, err := copyMapping(value["fetch_service_states"], "fetch_service_states")
	if err != nil {
		return err
	}
	status.FetchServiceStates = map[string]string{}
	for key, raw := range states {
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("fetch_service_states values must be strings")
		}
		status.FetchServiceStates[key] = text
	}
	errors, err := copyMapping(value["fetch_service_errors"], "fetch_service_errors")
	if err != nil {
		return err
	}
	status.FetchServiceErrors = map[string]string{}
	for key, raw := range errors {
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("fetch_service_errors values must be strings")
		}
		status.FetchServiceErrors[key] = text
	}
	progressRaw, present := value["fetch_run_progress"]
	if present && progressRaw != nil {
		progress, err := copyMapping(progressRaw, "fetch_run_progress")
		if err != nil {
			return err
		}
		status.FetchRunProgress = map[string]map[string]any{}
		for key, entry := range progress {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf("fetch_run_progress values must be objects")
			}
			status.FetchRunProgress[key] = entryMap
		}
	}
	contextRoot, ok := value["context_root"].(string)
	if !ok || len(contextRoot) < 1 {
		return fmt.Errorf("context_root must be a non-empty string")
	}
	status.ContextRoot = contextRoot
	if raw, present := value["last_error"]; present && raw != nil {
		copied, err := copyMapping(raw, "last_error")
		if err != nil {
			return err
		}
		status.LastError = copied
	}
	return nil
}

// decodeRawChangeFields 解析变更项字段。
func decodeRawChangeFields(value map[string]any, item *RawChangeItem) error {
	logicalID, err := requiredBoundedString(value, "logical_id", 1, 512)
	if err != nil {
		return err
	}
	item.LogicalID = logicalID
	revisionID, err := requiredBoundedString(value, "revision_id", 1, 256)
	if err != nil {
		return err
	}
	item.RevisionID = revisionID
	operation, ok := value["operation"].(string)
	if !ok || operation != "upsert" {
		return fmt.Errorf("operation must be upsert")
	}
	item.Operation = operation
	if title, present := value["title"]; present && title != nil {
		text, ok := title.(string)
		if !ok {
			return fmt.Errorf("title must be a string")
		}
		item.Title = &text
	}
	if content, present := value["content"]; present && content != nil {
		text, ok := content.(string)
		if !ok {
			return fmt.Errorf("content must be a string")
		}
		item.Content = &text
	}
	originalRef, ok := value["original_ref"].(string)
	if !ok || len(originalRef) < 1 {
		return fmt.Errorf("original_ref must be a non-empty string")
	}
	item.OriginalRef = originalRef
	if metadata, present := value["metadata"]; present && metadata != nil {
		copied, err := copyMapping(metadata, "metadata")
		if err != nil {
			return err
		}
		item.Metadata = copied
	} else {
		item.Metadata = map[string]any{}
	}
	item.RawSnapshot = value["raw_snapshot"]
	return nil
}

// decodeFetchBatchFields 解析批次字段。
func decodeFetchBatchFields(value map[string]any, batch *FetchBatch) error {
	batchID, ok := value["batch_id"].(string)
	if !ok || len(batchID) < 1 {
		return fmt.Errorf("batch_id must be a non-empty string")
	}
	batch.BatchID = batchID
	if items, present := value["items"]; present && items != nil {
		list, ok := items.([]any)
		if !ok {
			return fmt.Errorf("items must be a list")
		}
		if len(list) > 20 {
			return fmt.Errorf("items must contain at most 20 entries")
		}
		for _, entry := range list {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf("items entries must be objects")
			}
			item, err := NewRawChangeItem(entryMap)
			if err != nil {
				return err
			}
			batch.Items = append(batch.Items, *item)
		}
	}
	if raw, present := value["attempted_count"]; present && raw != nil {
		count, ok := raw.(float64)
		if !ok || count != float64(int(count)) || count < 0 || count > 20 {
			return fmt.Errorf("attempted_count must be an integer in [0, 20]")
		}
		batch.AttemptedCount = int(count)
	}
	offsets, err := decodeOffsetList(value, "success_offsets")
	if err != nil {
		return err
	}
	batch.SuccessOffsets = offsets
	skipped, err := decodeOffsetList(value, "skipped_offsets")
	if err != nil {
		return err
	}
	batch.SkippedOffsets = skipped
	if raw, present := value["failures"]; present && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("failures must be a list")
		}
		for _, entry := range list {
			copied, err := copyMapping(entry, "failure")
			if err != nil {
				return err
			}
			batch.Failures = append(batch.Failures, copied)
		}
	}
	if raw, present := value["next_cursor"]; present && raw != nil {
		copied, err := copyMapping(raw, "next_cursor")
		if err != nil {
			return err
		}
		batch.NextCursor = copied
	}
	if raw, present := value["materialized_source_path"]; present && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("materialized_source_path must be a string")
		}
		batch.MaterializedSourcePath = &text
	}
	if raw, present := value["materialized_revision"]; present && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("materialized_revision must be a string")
		}
		batch.MaterializedRevision = &text
	}
	return nil
}

// decodeOffsetList 解析下标列表字段（缺省为空）。
func decodeOffsetList(value map[string]any, key string) ([]int, error) {
	raw, present := value[key]
	if !present || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", key)
	}
	offsets := make([]int, 0, len(list))
	for _, entry := range list {
		number, ok := entry.(float64)
		if !ok || number != float64(int(number)) {
			return nil, fmt.Errorf("%s must contain integers", key)
		}
		offsets = append(offsets, int(number))
	}
	return offsets, nil
}

// requiredBoundedString 必填且有界字符串字段。
func requiredBoundedString(value map[string]any, key string, minLen, maxLen int) (string, error) {
	raw, ok := value[key]
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	if len(text) < minLen || len(text) > maxLen {
		return "", fmt.Errorf("%s length must be in [%d, %d]", key, minLen, maxLen)
	}
	return text, nil
}

// ──────────────────────────── PersonalContextStatus 校验链 ────────────────────────────

// CopyNestedStatusState 深拷贝嵌套状态并补齐进度缺省值。
// Python: models.py:79 copy_nested_state（model_validator mode=before）
func CopyNestedStatusState(value map[string]any) map[string]any {
	copied := deepCopyJSON(value)
	for _, fieldName := range []string{"fetch_service_states", "fetch_service_errors", "fetch_run_progress", "last_error"} {
		if copied[fieldName] != nil {
			mapped, err := copyMapping(copied[fieldName], fieldName)
			if err == nil {
				copied[fieldName] = mapped
			}
		}
	}
	if progress, ok := copied["fetch_run_progress"].(map[string]any); ok {
		for _, entry := range progress {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if _, present := entryMap["failed_items"]; !present {
				entryMap["failed_items"] = 0
			}
			if _, present := entryMap["quarantined_items"]; !present {
				entryMap["quarantined_items"] = 0
			}
			if _, present := entryMap["item_errors"]; !present {
				entryMap["item_errors"] = []any{}
			}
			if _, present := entryMap["omitted_item_errors"]; !present {
				entryMap["omitted_item_errors"] = 0
			}
		}
	}
	return copied
}

// BoundErrorMessages 错误消息长度上限校验。
// Python: models.py:101 bound_error_messages
func (s *PersonalContextStatus) BoundErrorMessages() error {
	for _, message := range s.FetchServiceErrors {
		if len([]rune(message)) > 512 {
			return fmt.Errorf("fetch service errors must be at most 512 characters")
		}
	}
	return nil
}

// ValidateFetchRunProgress run 进度形状与数值一致性校验。
// Python: models.py:108 validate_fetch_run_progress
func (s *PersonalContextStatus) ValidateFetchRunProgress() error {
	for serviceID, progress := range s.FetchRunProgress {
		// 形状白名单（Python: set(progress) not in (FIELDS, FIELDS|RESULT_FIELDS)）
		keys := make([]string, 0, len(progress))
		for key := range progress {
			keys = append(keys, key)
		}
		if !isValidProgressShape(keys) {
			return fmt.Errorf("fetch run progress has an invalid shape")
		}
		// service_id 与键一致
		if text, _ := progress["service_id"].(string); text != serviceID {
			return fmt.Errorf("fetch run progress service_id does not match its key")
		}
		// run_state 合法
		runState, _ := progress["run_state"].(string)
		if _, ok := fetchRunStates[runState]; !ok {
			return fmt.Errorf("fetch run progress has an invalid run_state")
		}
		// 结果字段三件套（Python: if "created_node_count" in progress）
		if _, present := progress["created_node_count"]; present {
			for _, fieldName := range []string{"created_node_count", "updated_node_count"} {
				count, ok := progress[fieldName].(float64)
				if !ok || count != float64(int(count)) || count < 0 {
					return fmt.Errorf("fetch run progress %s must be a non-negative integer", fieldName)
				}
			}
			noNewContent, ok := progress["no_new_content"].(bool)
			if !ok {
				return fmt.Errorf("fetch run progress no_new_content must be a boolean")
			}
			created, _ := progress["created_node_count"].(float64)
			updated, _ := progress["updated_node_count"].(float64)
			hasNodeChanges := created != 0 || updated != 0
			if noNewContent && (runState != "succeeded" || hasNodeChanges) {
				return fmt.Errorf("no_new_content requires a successful run without node changes")
			}
		}
		// 数值字段收集（Python: numeric dict）
		numeric := map[string]int{}
		for _, fieldName := range []string{"progress_percent", "total_items", "completed_items",
			"failed_items", "quarantined_items", "omitted_item_errors"} {
			raw, ok := progress[fieldName].(float64)
			if !ok || raw != float64(int(raw)) {
				return fmt.Errorf("fetch run progress %s must be an integer", fieldName)
			}
			numeric[fieldName] = int(raw)
		}
		percent := numeric["progress_percent"]
		total := numeric["total_items"]
		completed := numeric["completed_items"]
		failed := numeric["failed_items"]
		quarantined := numeric["quarantined_items"]
		omitted := numeric["omitted_item_errors"]
		hasNegativeCount := total < 0 || completed < 0 || failed < 0 || quarantined < 0 || omitted < 0
		hasOvercount := completed+failed > total
		if percent < 0 || percent > 100 || hasNegativeCount || hasOvercount {
			return fmt.Errorf("fetch run progress counts are out of range")
		}
		// item_errors 列表校验
		itemErrors, ok := progress["item_errors"].([]any)
		if !ok || len(itemErrors) > 20 {
			return fmt.Errorf("fetch run progress item_errors must be a bounded list")
		}
		for _, entry := range itemErrors {
			entryMap, ok := entry.(map[string]any)
			if !ok || !isValidItemErrorShape(entryMap) {
				return fmt.Errorf("fetch run progress item_error has an invalid shape")
			}
			code, ok := entryMap["code"].(float64)
			if !ok || code != float64(int(code)) {
				return fmt.Errorf("fetch run progress item_error code must be an integer")
			}
			for _, fieldName := range []string{"item_ref", "message"} {
				text, ok := entryMap[fieldName].(string)
				if !ok || strings.TrimSpace(text) == "" || len([]rune(text)) > 256 {
					return fmt.Errorf("fetch run progress item_error %s must be a bounded non-empty string", fieldName)
				}
			}
			failedAt, ok := entryMap["failed_at"].(string)
			if !ok || len(failedAt) > 64 {
				return fmt.Errorf("fetch run progress item_error failed_at must be RFC 3339")
			}
			parsed, err := time.Parse(time.RFC3339, strings.Replace(failedAt, "Z", "+00:00", 1))
			if err != nil {
				return fmt.Errorf("fetch run progress item_error failed_at must be RFC 3339")
			}
			if parsed.Location() == time.UTC && !strings.Contains(failedAt, "Z") &&
				!strings.Contains(failedAt, "+") && !strings.Contains(failedAt, "-") {
				// Python: parsed.tzinfo is None 拒绝无时区；Go RFC3339 解析必然带时区，语义已覆盖
				return fmt.Errorf("fetch run progress item_error failed_at must include a timezone")
			}
		}
		if len(itemErrors)+omitted != quarantined {
			return fmt.Errorf("fetch run progress quarantined diagnostics are inconsistent")
		}
		lastError := progress["last_error"]
		if runState == "succeeded" {
			if completed != total || failed != 0 {
				return fmt.Errorf("succeeded fetch run progress requires all items completed")
			}
			if percent != 100 {
				return fmt.Errorf("succeeded fetch run progress must be 100 percent")
			}
		} else if runState == "partial_succeeded" {
			if completed == 0 || failed == 0 || completed+failed != total {
				return fmt.Errorf("partial fetch run progress requires completed and failed items")
			}
			if percent != 100 {
				return fmt.Errorf("partial fetch run progress must be 100 percent")
			}
		}
		if runState == "idle" {
			if percent != 0 || total != 0 || completed != 0 || failed != 0 {
				return fmt.Errorf("idle fetch run progress must be empty")
			}
		}
		if runState == "failed" {
			allItemsFailed := total > 0 && completed == 0 && failed == total && percent == 100
			if allItemsFailed {
				if lastError != nil {
					return fmt.Errorf("item failure progress must not contain last_error")
				}
			} else {
				text, ok := lastError.(string)
				if !ok || strings.TrimSpace(text) == "" || len([]rune(text)) > 512 {
					return fmt.Errorf("system failure progress requires a bounded last_error")
				}
			}
		} else if lastError != nil {
			return fmt.Errorf("only failed fetch run progress may contain last_error")
		}
		if percent == 100 && runState != "succeeded" && runState != "partial_succeeded" && runState != "failed" {
			return fmt.Errorf("only terminal fetch run progress may be 100 percent")
		}
	}
	return nil
}

// ValidateLastError last_error 形状校验。
// Python: models.py:218 validate_last_error
func (s *PersonalContextStatus) ValidateLastError() error {
	if s.LastError == nil {
		return nil
	}
	if !sameStringSet(s.LastError, []string{"code", "status", "message", "operation"}) {
		return fmt.Errorf("last_error must contain only code, status, message, and operation")
	}
	code, ok := s.LastError["code"].(float64)
	if !ok || code != float64(int(code)) {
		return fmt.Errorf("last_error code must be an integer")
	}
	for _, fieldName := range []string{"status", "operation"} {
		text, ok := s.LastError[fieldName].(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("last_error %s must be a non-empty string", fieldName)
		}
	}
	message, ok := s.LastError["message"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		return fmt.Errorf("last_error message must be a non-empty string")
	}
	if len([]rune(message)) > 512 {
		return fmt.Errorf("last_error message must be at most 512 characters")
	}
	return nil
}

// ──────────────────────────── RawChangeItem 校验链 ────────────────────────────

// CopyRawChangeInput 深拷贝输入映射。
// Python: models.py:253 copy_input（model_validator mode=before）
func CopyRawChangeInput(value map[string]any) map[string]any {
	copied := deepCopyJSON(value)
	if copied["metadata"] != nil {
		if mapped, err := copyMapping(copied["metadata"], "metadata"); err == nil {
			copied["metadata"] = mapped
		}
	}
	return copied
}

// RejectBlankIdentifiers 标识符非空白校验。
// Python: models.py:263 reject_blank_identifiers
func (r *RawChangeItem) RejectBlankIdentifiers() error {
	for _, text := range []string{r.LogicalID, r.RevisionID, r.OriginalRef} {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("identifier must not be blank")
		}
	}
	return nil
}

// BoundContent 内容长度上限校验。
// Python: models.py:270 bound_content
func (r *RawChangeItem) BoundContent() error {
	if r.Content != nil && len([]rune(*r.Content)) > 2_000_000 {
		return fmt.Errorf("content exceeds 2,000,000 characters")
	}
	return nil
}

// BoundMetadata 元数据 64KiB 上限校验。
// Python: models.py:277 bound_metadata
func (r *RawChangeItem) BoundMetadata() error {
	return jsonSize(r.Metadata, "metadata", 64*1024)
}

// BoundSnapshot 快照 2MiB 上限校验。
// Python: models.py:283 bound_snapshot
func (r *RawChangeItem) BoundSnapshot() error {
	if r.RawSnapshot == nil {
		return nil
	}
	switch value := r.RawSnapshot.(type) {
	case string:
		if len(value) > 2*1024*1024 {
			return fmt.Errorf("raw_snapshot exceeds 2 MiB")
		}
	case []byte:
		if len(value) > 2*1024*1024 {
			return fmt.Errorf("raw_snapshot exceeds 2 MiB")
		}
	}
	return nil
}

// ValidateOperationPairing upsert 配对校验。
// Python: models.py:292 validate_operation_pairing（model_validator mode=after）
func (r *RawChangeItem) ValidateOperationPairing() error {
	if r.Content == nil || strings.TrimSpace(*r.Content) == "" {
		return fmt.Errorf("upsert changes require non-empty content")
	}
	return nil
}

// ──────────────────────────── FetchBatch 校验链 ────────────────────────────

// CopyFetchBatchInput 深拷贝输入并补齐缺省结果字段。
// Python: models.py:315 copy_input（model_validator mode=before）
func CopyFetchBatchInput(value map[string]any) (map[string]any, error) {
	copied := deepCopyJSON(value)
	outcomeFields := []string{"attempted_count", "success_offsets", "skipped_offsets", "failures"}
	hasOutcome := false
	for _, key := range outcomeFields {
		if _, present := copied[key]; present {
			hasOutcome = true
			break
		}
	}
	if !hasOutcome {
		itemCount := 0
		if items, ok := copied["items"].([]any); ok {
			itemCount = len(items)
		}
		copied["attempted_count"] = float64(itemCount)
		successOffsets := make([]any, itemCount)
		for index := range successOffsets {
			successOffsets[index] = float64(index)
		}
		copied["success_offsets"] = successOffsets
		copied["skipped_offsets"] = []any{}
		copied["failures"] = []any{}
	}
	if copied["next_cursor"] != nil {
		mapped, err := copyMapping(copied["next_cursor"], "next_cursor")
		if err != nil {
			return nil, err
		}
		copied["next_cursor"] = mapped
	}
	if copied["failures"] != nil {
		rawFailures, ok := copied["failures"].([]any)
		if !ok {
			return nil, fmt.Errorf("failures must be a list or tuple")
		}
		normalized := make([]any, 0, len(rawFailures))
		for _, failure := range rawFailures {
			mapped, err := copyMapping(failure, "failure")
			if err != nil {
				return nil, err
			}
			normalized = append(normalized, mapped)
		}
		copied["failures"] = normalized
	}
	return copied, nil
}

// ValidateBatchID 批次标识安全段校验。
// Python: models.py:341 validate_batch_id
func (b *FetchBatch) ValidateBatchID() error {
	if b.BatchID == "." || b.BatchID == ".." || !safeSegmentPattern.MatchString(b.BatchID) {
		return fmt.Errorf("batch_id must be a safe path segment")
	}
	return nil
}

// ValidateCursor 游标 JSON 有界校验。
// Python: models.py:348 validate_cursor
func (b *FetchBatch) ValidateCursor() error {
	if b.NextCursor != nil {
		return jsonSize(b.NextCursor, "next_cursor", 0)
	}
	return nil
}

// ValidateMaterializedPath 物化候选路径校验。
// Python: models.py:355 validate_materialized_path
func (b *FetchBatch) ValidateMaterializedPath() error {
	if b.MaterializedSourcePath == nil {
		return nil
	}
	value := *b.MaterializedSourcePath
	expanded := value
	if strings.HasPrefix(expanded, "~") {
		home, err := userHomeDir()
		if err == nil {
			expanded = filepath.Join(home, expanded[1:])
		}
	}
	if !filepath.IsAbs(expanded) {
		return fmt.Errorf("materialized_source_path must be an absolute candidate path")
	}
	resolved, err := filepath.Abs(expanded)
	if err != nil {
		return fmt.Errorf("materialized_source_path must be an absolute candidate path")
	}
	hasCandidate := false
	for _, part := range strings.Split(filepath.ToSlash(resolved), "/") {
		if strings.EqualFold(part, "candidate") {
			hasCandidate = true
			break
		}
	}
	if !hasCandidate {
		return fmt.Errorf("materialized_source_path must be an absolute candidate path")
	}
	b.MaterializedSourcePath = &resolved
	return nil
}

// ValidateMaterializedRevision 物化修订非空白校验。
// Python: models.py:368 validate_materialized_revision
func (b *FetchBatch) ValidateMaterializedRevision() error {
	if b.MaterializedRevision != nil && strings.TrimSpace(*b.MaterializedRevision) == "" {
		return fmt.Errorf("materialized_revision must not be blank")
	}
	return nil
}

// ValidateMaterializedPair 物化配对与结果下标一致性校验。
// Python: models.py:374 validate_materialized_pair（model_validator mode=after）
func (b *FetchBatch) ValidateMaterializedPair() error {
	if (b.MaterializedSourcePath == nil) != (b.MaterializedRevision == nil) {
		return fmt.Errorf("materialized source path and revision must be provided together")
	}
	for _, entry := range []struct {
		fieldName string
		offsets   []int
	}{
		{"success_offsets", b.SuccessOffsets},
		{"skipped_offsets", b.SkippedOffsets},
	} {
		sortedCopy := append([]int(nil), entry.offsets...)
		sort.Ints(sortedCopy)
		unique := sortedCopy[:0]
		for index, value := range sortedCopy {
			if index == 0 || value != sortedCopy[index-1] {
				unique = append(unique, value)
			}
		}
		if len(unique) != len(entry.offsets) {
			return fmt.Errorf("%s must be strictly increasing", entry.fieldName)
		}
	}
	failureOffsets := make([]int, 0, len(b.Failures))
	for _, failure := range b.Failures {
		if !sameStringSet(failure, []string{"offset", "item_ref", "code", "message"}) {
			return fmt.Errorf("failure must contain offset, item_ref, code, and message")
		}
		offset, ok := failure["offset"].(float64)
		if !ok || offset != float64(int(offset)) {
			return fmt.Errorf("failure offset must be an integer")
		}
		code, ok := failure["code"].(float64)
		if !ok || code != float64(int(code)) {
			return fmt.Errorf("failure code must be an integer")
		}
		itemRef, ok := failure["item_ref"].(string)
		if !ok || strings.TrimSpace(itemRef) == "" || len([]rune(itemRef)) > 256 {
			return fmt.Errorf("failure item_ref must be a bounded non-empty string")
		}
		message, ok := failure["message"].(string)
		if !ok || strings.TrimSpace(message) == "" || len([]rune(message)) > 256 {
			return fmt.Errorf("failure message must be a bounded non-empty string")
		}
		failureOffsets = append(failureOffsets, int(offset))
	}
	seen := map[int]struct{}{}
	for _, offset := range failureOffsets {
		if _, duplicated := seen[offset]; duplicated {
			return fmt.Errorf("failure offsets must be unique")
		}
		seen[offset] = struct{}{}
	}
	if len(b.Items) != len(b.SuccessOffsets) {
		return fmt.Errorf("items must match success_offsets")
	}
	success := toSet(b.SuccessOffsets)
	skipped := toSet(b.SkippedOffsets)
	failed := toSet(failureOffsets)
	for offset := range success {
		if _, clash := skipped[offset]; clash {
			return fmt.Errorf("candidate outcome offsets must be disjoint")
		}
		if _, clash := failed[offset]; clash {
			return fmt.Errorf("candidate outcome offsets must be disjoint")
		}
	}
	for offset := range skipped {
		if _, clash := failed[offset]; clash {
			return fmt.Errorf("candidate outcome offsets must be disjoint")
		}
	}
	for index := 0; index < b.AttemptedCount; index++ {
		_, inSuccess := success[index]
		_, inSkipped := skipped[index]
		_, inFailed := failed[index]
		if !inSuccess && !inSkipped && !inFailed {
			return fmt.Errorf("candidate outcomes must cover attempted_count")
		}
	}
	return nil
}

// isValidProgressShape 进度键集合是否命中两个白名单之一。
func isValidProgressShape(keys []string) bool {
	base := map[string]struct{}{}
	for _, key := range fetchRunProgressFields {
		base[key] = struct{}{}
	}
	extended := map[string]struct{}{}
	for key := range base {
		extended[key] = struct{}{}
	}
	for _, key := range fetchRunResultFields {
		extended[key] = struct{}{}
	}
	baseOnly, extendedOK := true, true
	for _, key := range keys {
		if _, ok := base[key]; !ok {
			baseOnly = false
		}
		if _, ok := extended[key]; !ok {
			extendedOK = false
		}
	}
	return (baseOnly && len(keys) == len(fetchRunProgressFields)) ||
		(extendedOK && len(keys) == len(fetchRunProgressFields)+len(fetchRunResultFields))
}

// isValidItemErrorShape 单条错误键集合精确匹配。
func isValidItemErrorShape(entry map[string]any) bool {
	return sameStringSet(entry, []string{"item_ref", "code", "message", "failed_at"})
}

// sameStringSet 键集合精确匹配（无多、无少）。
func sameStringSet(value map[string]any, expected []string) bool {
	if len(value) != len(expected) {
		return false
	}
	allowed := map[string]struct{}{}
	for _, key := range expected {
		allowed[key] = struct{}{}
	}
	for key := range value {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

// toSet 下标切片转集合。
func toSet(offsets []int) map[int]struct{} {
	result := make(map[int]struct{}, len(offsets))
	for _, offset := range offsets {
		result[offset] = struct{}{}
	}
	return result
}

// osUserHomeDir os.UserHomeDir 的窄包装（便于测试注入）。
func userHomeDir() (string, error) {
	return os.UserHomeDir()
}
