// 进度与终态投影使用的 Reviewer 元数据（按 tool_call_id 分桶）。
//
// 对应 Python 代码：permissions/reviewer_stream_metadata.py（57 行 / 3 函数 + 1 常量）
//
// 元数据挂在回调上下文的 extra 字典里，键为
// `permission_reviewer_metadata_by_tool_call_id`，值为
// `tool_call_id → metadata` 的桶。三个函数分别负责写入、取出（消费）与只读窥视。

package permissions

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ReviewerToolResultMetadataExtraKey 元数据在 extra 里的键。
	//
	// Python: REVIEWER_TOOL_RESULT_METADATA_EXTRA_KEY
	ReviewerToolResultMetadataExtraKey = "permission_reviewer_metadata_by_tool_call_id"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RecordReviewerToolResultMetadata 把元数据写入指定 tool_call_id 的桶。
//
// Python: record_reviewer_tool_result_metadata(extra, *, tool_call_id, metadata)
// extra 为 nil、tool_call_id 为空、metadata 为空时直接返回（不建空桶）。
func RecordReviewerToolResultMetadata(
	extra map[string]any,
	toolCallID string,
	metadata map[string]any,
) {
	if extra == nil || toolCallID == "" || len(metadata) == 0 {
		return
	}
	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		bucket = map[string]any{}
		extra[ReviewerToolResultMetadataExtraKey] = bucket
	}
	bucket[toolCallID] = deepCopyAnyMap(metadata)
}

// ConsumeReviewerToolResultMetadata 取出并删除指定 tool_call_id 的元数据。
//
// Python: consume_reviewer_tool_result_metadata(extra, *, tool_call_id)
// 桶为空时连桶一起删除，避免 extra 里留空壳。
func ConsumeReviewerToolResultMetadata(
	extra map[string]any,
	toolCallID string,
) map[string]any {
	if extra == nil || toolCallID == "" {
		return nil
	}
	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		delete(extra, ReviewerToolResultMetadataExtraKey)
		return nil
	}
	value := bucket[toolCallID]
	delete(bucket, toolCallID)
	if len(bucket) == 0 {
		delete(extra, ReviewerToolResultMetadataExtraKey)
	}
	if mapping, isMap := value.(map[string]any); isMap {
		return deepCopyAnyMap(mapping)
	}
	return nil
}

// PeekReviewerToolResultMetadata 只读窥视指定 tool_call_id 的元数据。
//
// Python: peek_reviewer_tool_result_metadata(extra, *, tool_call_id)
// 与 Consume 的区别：不删除条目，也不清理空桶。
func PeekReviewerToolResultMetadata(
	extra map[string]any,
	toolCallID string,
) map[string]any {
	if extra == nil || toolCallID == "" {
		return nil
	}
	bucket, ok := extra[ReviewerToolResultMetadataExtraKey].(map[string]any)
	if !ok {
		return nil
	}
	if mapping, isMap := bucket[toolCallID].(map[string]any); isMap {
		return deepCopyAnyMap(mapping)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
