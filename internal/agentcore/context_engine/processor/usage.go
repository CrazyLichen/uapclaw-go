package processor

import llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 导出函数 ────────────────────────────

// ResetCompressionUsage 重置压缩用量追踪。
//
// 对应 Python: ContextProcessor._reset_compression_usage()
func (p *BaseProcessor) ResetCompressionUsage() {
	p.compressionUsage = nil
}

// RecordCompressionUsage 记录压缩 LLM 调用的用量。
//
// 从 AssistantMessage 的 UsageMetadata 字段提取用量信息并合并到基类追踪中。
//
// 对应 Python: ContextProcessor._record_compression_usage(response)
func (p *BaseProcessor) RecordCompressionUsage(response *llm_schema.AssistantMessage) {
	usage := ExtractUsageMetadata(response)
	if usage == nil {
		return
	}
	p.compressionUsage = MergeCompressionUsage(p.compressionUsage, usage)
}

// CurrentCompressionUsage 获取当前压缩用量快照。
//
// 返回用量 map 的副本，避免外部修改影响内部状态。
//
// 对应 Python: ContextProcessor._current_compression_usage()
func (p *BaseProcessor) CurrentCompressionUsage() map[string]any {
	if p.compressionUsage == nil {
		return nil
	}
	result := make(map[string]any, len(p.compressionUsage))
	for k, v := range p.compressionUsage {
		result[k] = v
	}
	return result
}

// ExtractUsageMetadata 从 AssistantMessage 中提取用量元数据，
// 转换为标准 map 格式。
//
// 提取结果映射：
//
//	calls=1, input_tokens, output_tokens, total_tokens, cache_tokens,
//	输入成本、输出成本、总成本、模型名称、详情=[data]
//
// 对应 Python: ContextProcessor._extract_usage_metadata(response)
func ExtractUsageMetadata(msg *llm_schema.AssistantMessage) map[string]any {
	if msg == nil || msg.UsageMetadata == nil {
		return nil
	}
	um := msg.UsageMetadata
	return map[string]any{
		"calls":         1,
		"input_tokens":  um.InputTokens,
		"output_tokens": um.OutputTokens,
		"total_tokens":  um.TotalTokens,
		"cache_tokens":  um.CacheTokens,
		"input_cost":    um.InputCost,
		"output_cost":   um.OutputCost,
		"total_cost":    um.TotalCost,
		"model_name":    um.ModelName,
		"details":       []map[string]any{usageMetadataToMap(um)},
	}
}

// MergeCompressionUsage 合并两份压缩用量。
//
// 合并规则（与 Python 对齐）：
//   - calls, input_tokens, output_tokens, total_tokens, cache_tokens → 累加（int）
//   - input_cost, output_cost, total_cost → 累加（float64）
//   - model_name → 取 left 非空值，否则取 right
//   - details → 追加合并
//
// 对应 Python: ContextProcessor._merge_compression_usage(left, right)
func MergeCompressionUsage(left, right map[string]any) map[string]any {
	if left == nil {
		if right == nil {
			return nil
		}
		return copyMap(right)
	}
	if right == nil {
		return copyMap(left)
	}
	merged := copyMap(left)

	// 累加整数字段
	for _, key := range []string{"calls", "input_tokens", "output_tokens", "total_tokens", "cache_tokens"} {
		merged[key] = toInt(merged[key]) + toInt(right[key])
	}
	// 累加浮点数字段
	for _, key := range []string{"input_cost", "output_cost", "total_cost"} {
		merged[key] = toFloat64(merged[key]) + toFloat64(right[key])
	}
	// model_name 取 left 非空值，否则取 right
	if merged["model_name"] == "" || merged["model_name"] == nil {
		merged["model_name"] = right["model_name"]
	}
	// details 追加合并
	leftDetails := toSliceOfMaps(merged["details"])
	rightDetails := toSliceOfMaps(right["details"])
	merged["details"] = append(leftDetails, rightDetails...)

	return merged
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// usageMetadataToMap 将 UsageMetadata 转为 map
func usageMetadataToMap(um *llm_schema.UsageMetadata) map[string]any {
	return map[string]any{
		"code":               um.Code,
		"err_msg":            um.ErrMsg,
		"model_name":         um.ModelName,
		"input_tokens":       um.InputTokens,
		"output_tokens":      um.OutputTokens,
		"total_tokens":       um.TotalTokens,
		"cache_tokens":       um.CacheTokens,
		"input_cost":         um.InputCost,
		"output_cost":        um.OutputCost,
		"total_cost":         um.TotalCost,
		"total_latency":      um.TotalLatency,
		"first_token_time":   um.FirstTokenTime,
		"request_start_time": um.RequestStartTime,
	}
}

// copyMap 创建 map 的浅拷贝
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// toInt 将 any 转为 int
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// toFloat64 将 any 转为 float64
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// toSliceOfMaps 将 any 转为 []map[string]any
func toSliceOfMaps(v any) []map[string]any {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []map[string]any:
		return s
	case []any:
		result := make([]map[string]any, 0, len(s))
		for _, item := range s {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			}
		}
		return result
	default:
		return nil
	}
}
