package trajectory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	obs "github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
)

// spans.go 规范化 OTLP 轨迹上的无状态访问器与变换。
// 对齐 Python: agent_evolving/trajectory/spans.py（全文，2026-12-04 快照）。
// 在线路径从观测 processor 接收普通 OTLP JSON 字典；本模块刻意不持有
// 窗口、注册表或订阅——函数返回脱离的字典/列表，变换经 Trajectory.FromOTLP
// 产生新的 Trajectory 值。属性只在当前观测约定下读取，无历史键回退。

// ──────────────────────────── 类型 ────────────────────────────

// Span 规范 OTLP span 字典类型别名。
// Python: spans.py Span = dict[str, Any]。
type Span = map[string]any

// SpanIdentity (traceId, spanId) 身份对。
// Python: spans.py SpanIdentity = tuple[str, str]。
type SpanIdentity struct {
	TraceID string
	SpanID  string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EncodeOTLPValue 以 OTLP JSON AnyValue 表示编码一个 Go 值。
// Python: spans.py encode_otlp_value。
func EncodeOTLPValue(value any) map[string]any {
	v := toJsonCompatible(value)
	if v == nil {
		return map[string]any{"stringValue": ""}
	}
	switch typed := v.(type) {
	case bool:
		return map[string]any{"boolValue": typed}
	case int:
		return map[string]any{"intValue": fmt.Sprint(typed)}
	case int64:
		return map[string]any{"intValue": fmt.Sprint(typed)}
	case float64:
		return map[string]any{"doubleValue": typed}
	case string:
		return map[string]any{"stringValue": typed}
	case []any:
		values := make([]any, len(typed))
		for i, item := range typed {
			values[i] = EncodeOTLPValue(item)
		}
		return map[string]any{"arrayValue": map[string]any{"values": values}}
	case map[string]any:
		values := make([]any, 0, len(typed))
		for key, item := range typed {
			values = append(values, map[string]any{"key": key, "value": EncodeOTLPValue(item)})
		}
		return map[string]any{"kvlistValue": map[string]any{"values": values}}
	default:
		return map[string]any{"stringValue": fmt.Sprint(typed)}
	}
}

// DecodeOTLPValue 解码 OTLP AnyValue（或原样返回脱离的普通值）。
// Python: spans.py decode_otlp_value。
func DecodeOTLPValue(value any) any {
	return decodeOTLPValue(value)
}

// AttributesToMap 把 OTLP 属性解码为脱离的 {name: value} 映射。
// Python: spans.py attributes_to_map。
func AttributesToMap(attributes any) map[string]any {
	if attributes == nil {
		return map[string]any{}
	}
	if mapping, ok := attributes.(map[string]any); ok {
		// 规范映射的条目可能仍是 {"value": AnyValue} 形态。
		result := make(map[string]any, len(mapping))
		for key, value := range mapping {
			if vm, isMap := value.(map[string]any); isMap && len(vm) == 1 {
				if _, onlyValue := vm["value"]; onlyValue {
					value = vm["value"]
				}
			}
			result[key] = DecodeOTLPValue(value)
		}
		return result
	}
	result := map[string]any{}
	items, ok := attributes.([]any)
	if !ok {
		return result
	}
	for _, item := range items {
		entry, isMap := item.(map[string]any)
		if !isMap || entry["key"] == nil {
			continue
		}
		result[fmt.Sprint(entry["key"])] = DecodeOTLPValue(entry["value"])
	}
	return result
}

// AttributesFromMap 编码并确定性排序 Python 属性映射。
// Python: spans.py attributes_from_map。
func AttributesFromMap(attributes map[string]any) []any {
	result := make([]any, 0, len(attributes))
	for _, key := range sortedKeys(attributes) {
		if attributes[key] == nil {
			continue
		}
		result = append(result, map[string]any{"key": key, "value": EncodeOTLPValue(attributes[key])})
	}
	return result
}

// normalizeAttributes 归一化属性为 OTLP 列表形态。
// Python: spans.py _normalize_attributes。
func normalizeAttributes(attributes any) []any {
	return AttributesFromMap(AttributesToMap(attributes))
}

// NormalizeSpan 返回脱离的、规范形态的 OTLP span。
// 未知字段保留；原生 trace/span ID 刻意不重散列或替换——它们是观测
// 生产方的路由与身份事实。
// Python: spans.py normalize_span。
func NormalizeSpan(span map[string]any) (map[string]any, error) {
	if span == nil {
		return nil, fmt.Errorf("span must be a mapping")
	}
	normalized := toJsonCompatible(span).(map[string]any)
	if _, exists := normalized["attributes"]; exists {
		normalized["attributes"] = normalizeAttributes(normalized["attributes"])
	}
	if _, exists := normalized["events"]; exists {
		events, _ := normalized["events"].([]any)
		normalizedEvents := make([]any, 0, len(events))
		for _, eventItem := range events {
			event, isMap := eventItem.(map[string]any)
			if !isMap {
				continue
			}
			item := toJsonCompatible(event).(map[string]any)
			if _, hasAttrs := item["attributes"]; hasAttrs {
				item["attributes"] = normalizeAttributes(item["attributes"])
			}
			normalizedEvents = append(normalizedEvents, item)
		}
		normalized["events"] = normalizedEvents
	}
	if _, exists := normalized["links"]; exists {
		links, _ := normalized["links"].([]any)
		normalizedLinks := make([]any, 0, len(links))
		for _, linkItem := range links {
			if _, isMap := linkItem.(map[string]any); isMap {
				normalizedLinks = append(normalizedLinks, toJsonCompatible(linkItem))
			}
		}
		normalized["links"] = normalizedLinks
	}
	return normalized, nil
}

// NormalizeOTLP 归一化 OTLP TraceData 映射，不改语义。
// Python: spans.py normalize_otlp。
func NormalizeOTLP(payload map[string]any) (map[string]any, error) {
	if payload == nil {
		return nil, fmt.Errorf("OTLP payload must be a mapping")
	}
	result := toJsonCompatible(payload).(map[string]any)
	resourceSpans, _ := result["resourceSpans"].([]any)
	normalizedResources := make([]any, 0, len(resourceSpans))
	for _, rsItem := range resourceSpans {
		resourceSpan, isMap := rsItem.(map[string]any)
		if !isMap {
			continue
		}
		item := toJsonCompatible(resourceSpan).(map[string]any)
		resource, _ := item["resource"].(map[string]any)
		if resource == nil {
			resource = map[string]any{}
		}
		if _, hasAttrs := resource["attributes"]; hasAttrs {
			resource["attributes"] = normalizeAttributes(resource["attributes"])
		}
		item["resource"] = resource
		scopes, _ := item["scopeSpans"].([]any)
		normalizedScopes := make([]any, 0, len(scopes))
		for _, ssItem := range scopes {
			scopeSpan, isMap := ssItem.(map[string]any)
			if !isMap {
				continue
			}
			scopeItem := toJsonCompatible(scopeSpan).(map[string]any)
			scope, _ := scopeItem["scope"].(map[string]any)
			if scope == nil {
				scope = map[string]any{}
			}
			scopeItem["scope"] = scope
			spans, _ := scopeItem["spans"].([]any)
			normalizedSpans := make([]any, 0, len(spans))
			for _, spanItem := range spans {
				span, isMap := spanItem.(map[string]any)
				if !isMap {
					continue
				}
				normalizedSpan, err := NormalizeSpan(span)
				if err != nil {
					return nil, err
				}
				normalizedSpans = append(normalizedSpans, normalizedSpan)
			}
			scopeItem["spans"] = normalizedSpans
			normalizedScopes = append(normalizedScopes, scopeItem)
		}
		item["scopeSpans"] = normalizedScopes
		normalizedResources = append(normalizedResources, item)
	}
	result["resourceSpans"] = normalizedResources
	return result, nil
}

// payloadFor 解析 Trajectory 或 OTLP 映射的载荷。
// Python: spans.py _payload_for。
func payloadFor(value any) (map[string]any, error) {
	if payload, ok := value.(map[string]any); ok {
		return payload, nil
	}
	if trajectory, ok := value.(*Trajectory); ok && trajectory != nil {
		return trajectory.ToOTLP(), nil
	}
	return nil, fmt.Errorf("expected a Trajectory or OTLP mapping")
}

// trajectoryFromPayload 归一化载荷并构造 Trajectory。
// Python: spans.py _trajectory_from_payload。
func trajectoryFromPayload(payload map[string]any) (*Trajectory, error) {
	normalized, err := NormalizeOTLP(payload)
	if err != nil {
		return nil, err
	}
	return FromOTLP(normalized)
}

// payloadSpans 枚举载荷中全部（resourceIdx, scopeIdx, spanIdx, span）。
// Python: spans.py _payload_spans。
type payloadSpanEntry struct {
	ResourceIndex int
	ScopeIndex    int
	SpanIndex     int
	Span          map[string]any
}

func payloadSpans(payload map[string]any) []payloadSpanEntry {
	var result []payloadSpanEntry
	resourceSpans, _ := payload["resourceSpans"].([]any)
	for resourceIndex, rsItem := range resourceSpans {
		resourceSpan, isMap := rsItem.(map[string]any)
		if !isMap {
			continue
		}
		scopeSpans, _ := resourceSpan["scopeSpans"].([]any)
		for scopeIndex, ssItem := range scopeSpans {
			scopeSpan, isMap := ssItem.(map[string]any)
			if !isMap {
				continue
			}
			spans, _ := scopeSpan["spans"].([]any)
			for spanIndex, spanItem := range spans {
				span, isMap := spanItem.(map[string]any)
				if !isMap {
					continue
				}
				normalized, err := NormalizeSpan(span)
				if err != nil {
					continue
				}
				result = append(result, payloadSpanEntry{resourceIndex, scopeIndex, spanIndex, normalized})
			}
		}
	}
	return result
}

// IterSpans 从每个 OTLP 资源/scope 组枚举脱离的 span。
// Python: spans.py iter_spans。value 可为 *Trajectory 或 OTLP 映射。
func IterSpans(value any) []map[string]any {
	payload, err := payloadFor(value)
	if err != nil {
		return nil
	}
	entries := payloadSpans(payload)
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Span)
	}
	return result
}

// SpanAttributes 返回解码后的 span 属性（独立映射）。
// Python: spans.py span_attributes。
func SpanAttributes(span map[string]any) map[string]any {
	if span == nil {
		return map[string]any{}
	}
	return AttributesToMap(span["attributes"])
}

// SpanStatus 返回脱离的 status 映射（或空映射）。
// Python: spans.py span_status。
func SpanStatus(span map[string]any) map[string]any {
	status, ok := span["status"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if converted, ok := toJsonCompatible(status).(map[string]any); ok {
		return converted
	}
	return map[string]any{}
}

// SpanEvents 返回带解码属性的脱离 span 事件。
// Python: spans.py span_events。
func SpanEvents(span map[string]any) []map[string]any {
	events, _ := span["events"].([]any)
	result := make([]map[string]any, 0, len(events))
	for _, eventItem := range events {
		event, isMap := eventItem.(map[string]any)
		if !isMap {
			continue
		}
		item, ok := toJsonCompatible(event).(map[string]any)
		if !ok {
			continue
		}
		if _, hasAttrs := item["attributes"]; hasAttrs {
			item["attributes"] = normalizeAttributes(item["attributes"])
		}
		result = append(result, item)
	}
	return result
}

// idText 身份文本化（bytes 转 hex）。
// Python: spans.py _id_text。
func idText(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// SpanIdentityOf 返回原生 (traceId, spanId) 身份（不完整时 nil）。
// Python: spans.py span_identity。
func SpanIdentityOf(span map[string]any) *SpanIdentity {
	if span == nil {
		return nil
	}
	traceText := idText(span["traceId"])
	spanText := idText(span["spanId"])
	if traceText == "" || spanText == "" {
		return nil
	}
	return &SpanIdentity{TraceID: traceText, SpanID: spanText}
}

// timeValue 读 span 时间字段（纳秒），缺失/非法返 0。
// Python: spans.py _time_value。
func timeValue(span map[string]any, field string) int64 {
	value, exists := span[field]
	if !exists || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

// SpanSortKey merge/crop 与团队森林使用的稳定时间序键。
// Python: spans.py span_sort_key。
type SpanSortKey struct {
	StartTime int64
	EndTime   int64
	TraceID   string
	SpanID    string
}

// SpanSortKeyOf 计算单个 span 的排序键。
func SpanSortKeyOf(span map[string]any) SpanSortKey {
	identity := SpanIdentityOf(span)
	if identity == nil {
		identity = &SpanIdentity{}
	}
	return SpanSortKey{
		StartTime: timeValue(span, "startTimeUnixNano"),
		EndTime:   timeValue(span, "endTimeUnixNano"),
		TraceID:   identity.TraceID,
		SpanID:    identity.SpanID,
	}
}

// compareSpanSortKey 排序比较器（升序）。
func compareSpanSortKey(a, b SpanSortKey) int {
	if a.StartTime != b.StartTime {
		if a.StartTime < b.StartTime {
			return -1
		}
		return 1
	}
	if a.EndTime != b.EndTime {
		if a.EndTime < b.EndTime {
			return -1
		}
		return 1
	}
	if a.TraceID != b.TraceID {
		return strings.Compare(a.TraceID, b.TraceID)
	}
	return strings.Compare(a.SpanID, b.SpanID)
}

// scopeBucketKey scope 组桶键。
// Python: spans.py _scope_bucket_key。
func scopeBucketKey(scopeSpan map[string]any) string {
	scope, _ := scopeSpan["scope"].(map[string]any)
	if scope == nil {
		scope = map[string]any{}
	}
	keys := sortedKeys(scope)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", key, scope[key]))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// sortPayloadSpans 对载荷各 scope 的 spans 就地按稳定键排序。
// Python: spans.py _sort_payload_spans。
func sortPayloadSpans(payload map[string]any) {
	resourceSpans, _ := payload["resourceSpans"].([]any)
	for _, rsItem := range resourceSpans {
		resourceSpan, _ := rsItem.(map[string]any)
		if resourceSpan == nil {
			continue
		}
		scopeSpans, _ := resourceSpan["scopeSpans"].([]any)
		for _, ssItem := range scopeSpans {
			scopeSpan, _ := ssItem.(map[string]any)
			if scopeSpan == nil {
				continue
			}
			spans, _ := scopeSpan["spans"].([]any)
			sort.SliceStable(spans, func(i, j int) bool {
				a, _ := spans[i].(map[string]any)
				b, _ := spans[j].(map[string]any)
				return compareSpanSortKey(SpanSortKeyOf(a), SpanSortKeyOf(b)) < 0
			})
			scopeSpan["spans"] = spans
		}
	}
}

// MergePayloads 把多个 OTLP 载荷合并为一个逻辑资源，按身份去重。
// 首个载荷拥有资源元数据（含 trajectory/session 身份）；保留全部输入的
// 原生 trace/span ID；后续载荷的资源级身份属性不复制进第二个资源记录，
// 否则合并窗口会呈现多条轨迹。
// Python: spans.py merge_payloads。
func MergePayloads(values ...any) (map[string]any, error) {
	var payloads []map[string]any
	for _, value := range values {
		if value == nil {
			continue
		}
		raw, err := payloadFor(value)
		if err != nil {
			return nil, err
		}
		normalized, err := NormalizeOTLP(raw)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, normalized)
	}
	if len(payloads) == 0 {
		return map[string]any{"resourceSpans": []any{}}, nil
	}
	first := copyJSON(payloads[0]).(map[string]any)
	firstResources, _ := first["resourceSpans"].([]any)
	var firstResource map[string]any
	if len(firstResources) > 0 {
		firstResource, _ = firstResources[0].(map[string]any)
	}
	if firstResource == nil {
		firstResource = map[string]any{}
	}
	firstResourcePayload, _ := firstResource["resource"].(map[string]any)
	if firstResourcePayload == nil {
		firstResourcePayload = map[string]any{}
	}
	logicalResource := map[string]any{
		"resource":   copyJSON(firstResourcePayload),
		"scopeSpans": []any{},
	}
	result := map[string]any{"resourceSpans": []any{logicalResource}}
	seen := map[SpanIdentity]struct{}{}
	for _, payload := range payloads {
		resourceSpans, _ := payload["resourceSpans"].([]any)
		for _, rsItem := range resourceSpans {
			incomingResource, isMap := rsItem.(map[string]any)
			if !isMap {
				continue
			}
			scopeSpans, _ := incomingResource["scopeSpans"].([]any)
			for _, ssItem := range scopeSpans {
				incomingScope, isMap := ssItem.(map[string]any)
				if !isMap {
					continue
				}
				scopeKey := scopeBucketKey(incomingScope)
				var matchingScope map[string]any
				existingScopes, _ := logicalResource["scopeSpans"].([]any)
				for _, esItem := range existingScopes {
					existingScope, _ := esItem.(map[string]any)
					if existingScope != nil && scopeBucketKey(existingScope) == scopeKey {
						matchingScope = existingScope
						break
					}
				}
				if matchingScope == nil {
					scope, _ := incomingScope["scope"].(map[string]any)
					if scope == nil {
						scope = map[string]any{}
					}
					matchingScope = map[string]any{"scope": copyJSON(scope), "spans": []any{}}
					logicalResource["scopeSpans"] = append(existingScopes, matchingScope)
				}
				existingSpans, _ := matchingScope["spans"].([]any)
				spans, _ := incomingScope["spans"].([]any)
				for _, spanItem := range spans {
					span, isMap := spanItem.(map[string]any)
					if !isMap {
						continue
					}
					normalized, err := NormalizeSpan(span)
					if err != nil {
						continue
					}
					identity := SpanIdentityOf(normalized)
					if identity != nil {
						if _, duplicated := seen[*identity]; duplicated {
							continue
						}
					}
					existingSpans = append(existingSpans, normalized)
					if identity != nil {
						seen[*identity] = struct{}{}
					}
				}
				matchingScope["spans"] = existingSpans
			}
		}
	}
	sortPayloadSpans(result)
	return NormalizeOTLP(result)
}

// MergeTrajectories 返回包含全部输入去重 span 的新规范轨迹。
// Python: spans.py merge_trajectories。
func MergeTrajectories(values ...any) (*Trajectory, error) {
	payload, err := MergePayloads(values...)
	if err != nil {
		return nil, err
	}
	return trajectoryFromPayload(payload)
}

// MergeSpans 把脱离的 span 字典合并进轨迹值。
// Python: spans.py merge_spans。
func MergeSpans(base any, additions []map[string]any) (*Trajectory, error) {
	raw, err := payloadFor(base)
	if err != nil {
		return nil, err
	}
	payload, err := NormalizeOTLP(raw)
	if err != nil {
		return nil, err
	}
	additionSpans := make([]any, 0, len(additions))
	for _, span := range additions {
		additionSpans = append(additionSpans, span)
	}
	extra := map[string]any{"resourceSpans": []any{
		map[string]any{"resource": map[string]any{}, "scopeSpans": []any{
			map[string]any{"spans": additionSpans},
		}},
	}}
	merged, err := MergePayloads(payload, extra)
	if err != nil {
		return nil, err
	}
	return trajectoryFromPayload(merged)
}

// trimSpanIndices 选位使相同或重复身份保持独立。
// Python: spans.py _trim_span_indices。
func trimSpanIndices(spans []map[string]any, maxSpans int, startTime, endTime *int64, uncounted func(map[string]any) bool) []int {
	var selected []int
	for index, span := range spans {
		if startTime != nil && timeValue(span, "endTimeUnixNano") < *startTime {
			continue
		}
		if endTime != nil && timeValue(span, "startTimeUnixNano") > *endTime {
			continue
		}
		selected = append(selected, index)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return compareSpanSortKey(SpanSortKeyOf(spans[selected[i]]), SpanSortKeyOf(spans[selected[j]])) < 0
	})
	if maxSpans < 0 {
		maxSpans = 0
	}
	if maxSpans == 0 && !hasUncountedFilter(uncounted) {
		// max_spans None 语义由调用方区分：0 传参表示显式空窗。
		return selected
	}
	if uncounted == nil {
		if len(selected) > maxSpans {
			return selected[len(selected)-maxSpans:]
		}
		return selected
	}
	var counted []int
	for _, index := range selected {
		if !uncounted(spans[index]) {
			counted = append(counted, index)
		}
	}
	keptSet := map[int]struct{}{}
	keptCounted := counted
	if len(keptCounted) > maxSpans {
		keptCounted = keptCounted[len(keptCounted)-maxSpans:]
	}
	for _, index := range keptCounted {
		keptSet[index] = struct{}{}
	}
	if len(keptSet) == 0 {
		return []int{}
	}
	cutoff := int64(-1)
	for index := range keptSet {
		start := timeValue(spans[index], "startTimeUnixNano")
		if cutoff < 0 || start < cutoff {
			cutoff = start
		}
	}
	result := make([]int, 0, len(selected))
	for _, index := range selected {
		_, kept := keptSet[index]
		if kept || (uncounted(spans[index]) && timeValue(spans[index], "startTimeUnixNano") >= cutoff) {
			result = append(result, index)
		}
	}
	return result
}

// hasUncountedFilter 判定是否传入 uncounted 过滤器。
func hasUncountedFilter(uncounted func(map[string]any) bool) bool { return uncounted != nil }

// TrimSpans 返回脱离的、按时间裁剪的 span 列表。
// max_spans 在时间过滤后保留最新 span；非正上限产生空列表（有界净窗口）。
// Python: spans.py trim_spans。
func TrimSpans(spans []map[string]any, maxSpans int, startTime, endTime *int64) []map[string]any {
	normalized := make([]map[string]any, 0, len(spans))
	for _, span := range spans {
		if span == nil {
			continue
		}
		normalizedSpan, err := NormalizeSpan(span)
		if err != nil {
			continue
		}
		normalized = append(normalized, normalizedSpan)
	}
	selected := trimSpanIndices(normalized, maxSpans, startTime, endTime, nil)
	result := make([]map[string]any, 0, len(selected))
	for _, index := range selected {
		result = append(result, normalized[index])
	}
	return result
}

// TrimTrajectory 返回全局裁剪、保留原资源/scope 分组的轨迹。
// uncounted 接受的 span 不占 max_spans 名额；起点不早于最老保留 span 时保留。
// Python: spans.py trim_trajectory。
func TrimTrajectory(value any, maxSpans int, startTime, endTime *int64, uncounted func(map[string]any) bool) (*Trajectory, error) {
	raw, err := payloadFor(value)
	if err != nil {
		return nil, err
	}
	payload, err := NormalizeOTLP(raw)
	if err != nil {
		return nil, err
	}
	if maxSpans == 0 && startTime == nil && endTime == nil {
		// Python 的 max_spans=None 语义（不裁剪）在 Go 以 maxSpans=0 +
		// 全 nil 时间窗表达；显式传负数表示清空（见 trimSpanIndices）。
		return trajectoryFromPayload(payload)
	}
	entries := payloadSpans(payload)
	spanList := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		spanList = append(spanList, entry.Span)
	}
	selected := trimSpanIndices(spanList, maxSpans, startTime, endTime, uncounted)
	result := copyJSON(payload).(map[string]any)
	resourceSpans, _ := result["resourceSpans"].([]any)
	for _, rsItem := range resourceSpans {
		resourceSpan, _ := rsItem.(map[string]any)
		if resourceSpan == nil {
			continue
		}
		scopeSpans, _ := resourceSpan["scopeSpans"].([]any)
		for _, ssItem := range scopeSpans {
			scopeSpan, _ := ssItem.(map[string]any)
			if scopeSpan != nil {
				scopeSpan["spans"] = []any{}
			}
		}
	}
	// 保留空组与其元数据；selected 位置标识原始来源，即使不同组含相同 ID。
	for _, index := range selected {
		entry := entries[index]
		resourceSpan, _ := resourceSpans[entry.ResourceIndex].(map[string]any)
		if resourceSpan == nil {
			continue
		}
		scopeSpans, _ := resourceSpan["scopeSpans"].([]any)
		if entry.ScopeIndex >= len(scopeSpans) {
			continue
		}
		scopeSpan, _ := scopeSpans[entry.ScopeIndex].(map[string]any)
		if scopeSpan == nil {
			continue
		}
		spans, _ := scopeSpan["spans"].([]any)
		scopeSpan["spans"] = append(spans, entry.Span)
	}
	return trajectoryFromPayload(result)
}

// CropTrajectory 窗口调用方使用的 trim_trajectory 别名。
// Python: spans.py crop_trajectory。
func CropTrajectory(value any, maxSpans int, startTime, endTime *int64, uncounted func(map[string]any) bool) (*Trajectory, error) {
	return TrimTrajectory(value, maxSpans, startTime, endTime, uncounted)
}

// ──────────────────────────── 语义访问器 ────────────────────────────

// DecodeJSONAttribute 解码 JSON 编码属性，非 JSON 值原样保留。
// Python: spans.py decode_json_attribute。
func DecodeJSONAttribute(value any) any {
	text, isString := value.(string)
	if !isString {
		return copyJSON(value)
	}
	var decoded any
	if json.Unmarshal([]byte(text), &decoded) == nil {
		return decoded
	}
	return value
}

// decodeStructuredAttribute 解码对象/数组属性，标量字符串不强制转换。
// Python: spans.py _decode_structured_attribute。
func decodeStructuredAttribute(value any) any {
	text, isString := value.(string)
	if !isString {
		return copyJSON(value)
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || (trimmed[0] != '[' && trimmed[0] != '{') {
		return value
	}
	return DecodeJSONAttribute(value)
}

// messageList 解码消息列表属性。
// Python: spans.py _message_list。
func messageList(value any) []map[string]any {
	decoded := decodeStructuredAttribute(value)
	if mapping, isMap := decoded.(map[string]any); isMap {
		decoded = []any{mapping}
	}
	items, isList := decoded.([]any)
	if !isList {
		return []map[string]any{}
	}
	messages := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if message, isMap := item.(map[string]any); isMap {
			messages = append(messages, copyJSON(message).(map[string]any))
		} else if item != nil {
			messages = append(messages, map[string]any{"role": "unknown", "content": fmt.Sprint(item)})
		}
	}
	return messages
}

// structuredPartsText 拼接一条消息结构化 parts 携带的文本。
// Python: spans.py _structured_parts_text。
func structuredPartsText(parts any) string {
	items, isList := parts.([]any)
	if !isList {
		return ""
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		part, isMap := item.(map[string]any)
		if isMap {
			if content, isString := part["content"].(string); isString {
				texts = append(texts, content)
			}
		}
	}
	return strings.Join(texts, "\n")
}

// toolCallsFromParts 从消息结构化 parts 重建扁平 tool-call 列表。
// Python: spans.py _tool_calls_from_parts。
func toolCallsFromParts(parts any) []map[string]any {
	items, isList := parts.([]any)
	if !isList {
		return []map[string]any{}
	}
	toolCalls := make([]map[string]any, 0, len(items))
	for _, item := range items {
		part, isMap := item.(map[string]any)
		if !isMap || part["type"] != "tool_call" {
			continue
		}
		// 整体携带的 call 保留自身 type；被埋点摊平的字段直接在 part 上。
		nested, hasNested := part["call"].(map[string]any)
		if hasNested {
			toolCalls = append(toolCalls, copyJSON(nested).(map[string]any))
			continue
		}
		flat := map[string]any{}
		for key, value := range part {
			if key != "type" {
				flat[key] = copyJSON(value)
			}
		}
		toolCalls = append(toolCalls, flat)
	}
	return toolCalls
}

// flattenStructuredMessage 把结构化消息折叠为扁平 role/content 形态。
// 标准 GenAI 消息以 parts 携带文本与 tool calls；这里的每个消费者读
// content 与 tool_calls。其余字段（name、finish_reason）原样透传。
// Python: spans.py _flatten_structured_message。
func flattenStructuredMessage(message map[string]any) map[string]any {
	flat := map[string]any{}
	for key, value := range message {
		if key != "parts" {
			flat[key] = copyJSON(value)
		}
	}
	parts, _ := message["parts"].([]any)
	// reasoning 是模型思考而非所说内容：读回为 reasoning_content，
	// 永不折叠进 content。
	var reasoningParts, saidParts []map[string]any
	for _, item := range parts {
		part, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if part["type"] == "reasoning" {
			reasoningParts = append(reasoningParts, part)
		} else {
			saidParts = append(saidParts, part)
		}
	}
	if len(reasoningParts) > 0 {
		if _, exists := flat["reasoning_content"]; !exists {
			flat["reasoning_content"] = structuredPartsText(reasoningParts)
		}
	}
	if _, isString := flat["content"].(string); !isString {
		var contents []any
		for _, part := range saidParts {
			if content, exists := part["content"]; exists {
				contents = append(contents, content)
			}
		}
		if len(contents) == 1 {
			if _, isString := contents[0].(string); !isString {
				// 多模态内容整体在单一 part 中，原样读回。
				flat["content"] = copyJSON(contents[0])
			} else {
				flat["content"] = contents[0]
			}
		} else if len(contents) > 0 {
			flat["content"] = structuredPartsText(saidParts)
		}
	}
	if _, exists := flat["tool_calls"]; !exists {
		toolCalls := toolCallsFromParts(message["parts"])
		if len(toolCalls) > 0 {
			flat["tool_calls"] = toolCalls
		}
	}
	if role, _ := flat["role"].(string); role == "tool" {
		var responses []map[string]any
		for _, item := range parts {
			part, isMap := item.(map[string]any)
			if isMap && part["type"] == "tool_call_response" {
				responses = append(responses, part)
			}
		}
		if len(responses) > 0 {
			response := responses[0]
			if _, exists := flat["content"]; !exists {
				flat["content"] = copyJSON(response["response"])
			}
			if response["id"] != nil {
				if _, exists := flat["tool_call_id"]; !exists {
					flat["tool_call_id"] = copyJSON(response["id"])
				}
			}
		}
	}
	if _, exists := flat["role"]; !exists {
		flat["role"] = "unknown"
	}
	return flat
}

// toAnyList 把 []map[string]any / []any 统一为 []any（Go 内存映射形态宽容）。
func toAnyList(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		result := make([]any, 0, len(v))
		for _, item := range v {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

// structureMessage 把扁平 role/content 消息渲染为结构化 GenAI 消息。
// 文本、tool calls 与 tool 响应成为 OpenTelemetry GenAI JSON schema 定义的
// 精确 part 形态。
// Python: spans.py _structure_message。
func structureMessage(message map[string]any) map[string]any {
	var parts []any
	content := message["content"]
	role, _ := message["role"].(string)
	if role == "" {
		role = "unknown"
	}
	reasoning, _ := message["reasoning_content"].(string)
	if reasoning != "" {
		parts = append(parts, map[string]any{"type": "reasoning", "content": reasoning})
	}
	if role == "tool" && content != nil {
		response := map[string]any{"type": "tool_call_response", "response": copyJSON(content)}
		if message["tool_call_id"] != nil {
			response["id"] = copyJSON(message["tool_call_id"])
		}
		parts = append(parts, response)
	} else if content != nil {
		// 空内容也记录：携带空 content 字段的消息不同于没携带的。
		parts = append(parts, map[string]any{"type": "text", "content": copyJSON(content)})
	}
	toolCalls := toAnyList(message["tool_calls"])
	for _, item := range toolCalls {
		call, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		function, _ := call["function"].(map[string]any)
		if function == nil {
			function = map[string]any{}
		}
		part := map[string]any{"type": "tool_call"}
		callID := firstValue(call, "id")
		if callID == nil {
			callID = function["id"]
		}
		name := firstValue(call, "name")
		if name == nil {
			name = function["name"]
		}
		arguments := firstValue(call, "arguments")
		if arguments == nil {
			arguments = function["arguments"]
		}
		if callID != nil {
			part["id"] = copyJSON(callID)
		}
		if name != nil {
			part["name"] = copyJSON(name)
		}
		if arguments != nil {
			part["arguments"] = copyJSON(arguments)
		}
		parts = append(parts, part)
	}
	structured := map[string]any{"role": role, "parts": parts}
	if message["name"] != nil {
		structured["name"] = copyJSON(message["name"])
	}
	return structured
}

// WriteLLMExchange 以标准 GenAI span 属性渲染一次 LLM 交换。
// read_llm_exchange 的逆操作，供手工构建 span 属性字典的轨迹生产方：
// 离线抽取与 RL、SFT rails。
// Python: spans.py write_llm_exchange。
func WriteLLMExchange(prompts, completions []map[string]any) map[string]any {
	var systemParts []any
	var inputMessages []any
	for _, message := range prompts {
		role, _ := message["role"].(string)
		if role == "system" {
			content := message["content"]
			if content != nil && content != "" {
				systemParts = append(systemParts, map[string]any{"type": "text", "content": copyJSON(content)})
			}
			continue
		}
		inputMessages = append(inputMessages, structureMessage(message))
	}
	outputMessages := make([]any, 0, len(completions))
	for _, message := range completions {
		outputMessages = append(outputMessages, structureMessage(message))
	}
	encode := func(value any) string { return jsonEncodeString(value) }
	attributes := map[string]any{}
	if len(systemParts) > 0 {
		attributes[obs.GenAISystemInstructions] = encode(systemParts)
	}
	if len(inputMessages) > 0 {
		attributes[obs.GenAIInputMessages] = encode(inputMessages)
	}
	if len(outputMessages) > 0 {
		attributes[obs.GenAIOutputMessages] = encode(outputMessages)
	}
	return attributes
}

// standardPromptMessages 把标准输入属性读回为有序消息列表。
// gen_ai.system_instructions 是对话历史之外给出的指令，所以靠前；
// gen_ai.input.messages 随后按序。
// Python: spans.py _standard_prompt_messages。
func standardPromptMessages(attrs map[string]any) []map[string]any {
	messages := make([]map[string]any, 0)
	systemText := structuredPartsText(decodeStructuredAttribute(attrs[obs.GenAISystemInstructions]))
	if systemText != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemText})
	}
	for _, message := range messageList(attrs[obs.GenAIInputMessages]) {
		messages = append(messages, flattenStructuredMessage(message))
	}
	return messages
}

// IsCompactionSpan 一次模型请求是否为总结对话而非延续对话。
// 该请求的 prompt 关于上下文而非属于上下文，重建对话或训练的读取方
// 将其排除；轨迹中保留——它产出的窗口另行提交。
// Python: spans.py is_compaction_span。
func IsCompactionSpan(span map[string]any) bool {
	value, _ := SpanAttributes(span)[obs.OJRequestPurpose].(string)
	return value == "compaction"
}

// ReadLLMExchange 从标准 GenAI 属性读取脱离的 LLM 消息（prompts, completions）。
// Python: spans.py read_llm_exchange。
func ReadLLMExchange(span map[string]any) ([]map[string]any, []map[string]any) {
	attrs := SpanAttributes(span)
	prompts := standardPromptMessages(attrs)
	completions := make([]map[string]any, 0)
	for _, message := range messageList(attrs[obs.GenAIOutputMessages]) {
		completions = append(completions, flattenStructuredMessage(message))
	}
	return copyMessageList(prompts), copyMessageList(completions)
}

// copyMessageList 深拷贝消息列表（JSON 值形态保持 map[string]any）。
func copyMessageList(messages []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		result = append(result, copyJSON(message).(map[string]any))
	}
	return result
}

// ReadLLMMessages 从 llm.call span 读取带索引的 prompt/completion 消息。
// Python: spans.py read_llm_messages。
func ReadLLMMessages(span map[string]any, includeEmpty bool) []map[string]any {
	prompts, completions := ReadLLMExchange(span)
	messages := append(append([]map[string]any{}, prompts...), completions...)
	if includeEmpty {
		return messages
	}
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message["role"] != nil || message["content"] != nil {
			result = append(result, message)
		}
	}
	return result
}

// ReadToolCall 从 tool span 读取规范的工具身份/输入/输出与状态。
// Python: spans.py read_tool_call。
func ReadToolCall(span map[string]any) map[string]any {
	attrs := SpanAttributes(span)
	result := map[string]any{}
	if name, exists := attrs[obs.GenAIToolName]; exists && name != nil {
		result["name"] = copyJSON(name)
	}
	if toolID, exists := attrs[obs.GenAIToolCallID]; exists && toolID != nil {
		result["id"] = copyJSON(toolID)
	}
	if arguments, exists := attrs[obs.GenAIToolCallArguments]; exists {
		result["input"] = decodeStructuredAttribute(arguments)
	}
	if callResult, exists := attrs[obs.GenAIToolCallResult]; exists {
		result["output"] = decodeStructuredAttribute(callResult)
	}
	if err := ReadSpanError(span); err != nil {
		result["error"] = *err
	}
	return copyJSON(result).(map[string]any)
}

// ReadUsage 以观测规范名返回 token 用量。
// Python: spans.py read_usage。
func ReadUsage(span map[string]any) map[string]int {
	attrs := SpanAttributes(span)
	result := map[string]int{}
	for _, pair := range []struct{ outputKey, attrKey string }{
		{"prompt_tokens", obs.GenAIUsageInputTokens},
		{"completion_tokens", obs.GenAIUsageOutputTokens},
	} {
		if value, exists := attrs[pair.attrKey]; exists {
			switch v := value.(type) {
			case int:
				result[pair.outputKey] = v
			case int64:
				result[pair.outputKey] = int(v)
			case float64:
				result[pair.outputKey] = int(v)
			case string:
				var parsed int
				if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
					result[pair.outputKey] = parsed
				}
			}
		}
	}
	if len(result) > 0 {
		result["total_tokens"] = result["prompt_tokens"] + result["completion_tokens"]
	}
	return result
}

// coerceIntList 结构化属性转 int 列表（非法项跳过）。
// Python: spans.py _coerce_int_list。
func coerceIntList(value any) []int {
	decoded := decodeStructuredAttribute(value)
	items, isList := decoded.([]any)
	if !isList {
		return nil
	}
	result := make([]int, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case int:
			result = append(result, v)
		case int64:
			result = append(result, int(v))
		case float64:
			result = append(result, int(v))
		case string:
			var parsed int
			if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
				result = append(result, parsed)
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// coerceFloatList 结构化属性转 float 列表。
// Python: spans.py _coerce_float_list。
func coerceFloatList(value any) []float64 {
	decoded := decodeStructuredAttribute(value)
	if mapping, isMap := decoded.(map[string]any); isMap {
		decoded = mapping["content"]
	}
	items, isList := decoded.([]any)
	if !isList {
		return nil
	}
	result := make([]float64, 0, len(items))
	for _, item := range items {
		if mapping, isMap := item.(map[string]any); isMap {
			item = mapping["logprob"]
		}
		switch v := item.(type) {
		case float64:
			result = append(result, v)
		case int:
			result = append(result, float64(v))
		case int64:
			result = append(result, float64(v))
		case string:
			var parsed float64
			if _, err := fmt.Sscanf(v, "%g", &parsed); err == nil {
				result = append(result, parsed)
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ReadRLFields 读取可选 RL 字段，不复制模型响应数据。
// Python: spans.py read_rl_fields。
func ReadRLFields(span map[string]any) map[string]any {
	attrs := SpanAttributes(span)
	fields := map[string]any{}
	for _, pair := range []struct{ outputKey, attrKey string }{
		{"prompt_token_ids", RLPromptTokenIDs},
		{"completion_token_ids", RLCompletionTokenIDs},
		{"logprobs", RLLogprobs},
		{"reward", RLReward},
	} {
		value := attrs[pair.attrKey]
		if value == nil {
			continue
		}
		switch pair.outputKey {
		case "prompt_token_ids", "completion_token_ids":
			if normalized := coerceIntList(value); normalized != nil {
				fields[pair.outputKey] = normalized
			}
		case "logprobs":
			if normalized := coerceFloatList(value); normalized != nil {
				fields[pair.outputKey] = normalized
			}
		default:
			fields[pair.outputKey] = DecodeJSONAttribute(value)
		}
	}
	return fields
}

// SpanError 规范 status/exception 失败信息。
// Python: spans.py read_span_error 的返回 dict。
type SpanError struct {
	Status    string
	Message   string
	Exception map[string]any
}

// ReadSpanError 读取规范的 status/exception 失败信息（无失败时 nil）。
// Python: spans.py read_span_error。
func ReadSpanError(span map[string]any) *SpanError {
	status := SpanStatus(span)
	code := strings.ToUpper(fmt.Sprint(firstValue(status, "code", "status_code")))
	message, _ := status["message"].(string)
	var exceptionEvent map[string]any
	for _, event := range SpanEvents(span) {
		if strings.EqualFold(fmt.Sprint(event["name"]), "exception") {
			exceptionEvent = event
			break
		}
	}
	if code != "ERROR" && code != "STATUS_CODE_ERROR" && message == "" && exceptionEvent == nil {
		return nil
	}
	displayCode := code
	if displayCode == "" {
		displayCode = "ERROR"
	}
	err := &SpanError{Status: displayCode, Message: message}
	if exceptionEvent != nil {
		err.Exception = AttributesToMap(exceptionEvent["attributes"])
	}
	return err
}
