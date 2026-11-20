package trajectory

import (
	"encoding/json"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	obs "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/observability"
)

// zz_processor_helpers.go processor 的 OTel ReadSpan 读面辅助（25.2b）。
// Python: processor.py _span_to_otlp / _span_identity / _span_status /
// _span_kind / _context_value 的 Go 等价面——直接走 sdktrace.ReadOnlySpan。

// spanToOTLP 把一个结束的 span 拷贝为规范 OTLP JSON 表示。
// Python: processor.py _span_to_otlp。
func spanToOTLP(span sdktrace.ReadOnlySpan) (map[string]any, *SpanIdentity, error) {
	ctx := span.SpanContext()
	if !ctx.HasTraceID() || !ctx.HasSpanID() {
		return nil, nil, fmt.Errorf("span is missing a valid trace/span identity")
	}
	traceID := normalizeTraceIDText(ctx.TraceID().String())
	spanID := normalizeSpanIDText(ctx.SpanID().String())
	encoded := map[string]any{
		"traceId":    traceID,
		"spanId":     spanID,
		"name":       span.Name(),
		"kind":       spanKindName(span),
		"attributes": AttributesFromMap(attrMapOf(span)),
		"status":     spanStatusOf(span),
	}
	if parent := span.Parent(); parent.HasSpanID() {
		encoded["parentSpanId"] = normalizeSpanIDText(parent.SpanID().String())
	}
	encoded["startTimeUnixNano"] = fmt.Sprint(span.StartTime().UnixNano())
	encoded["endTimeUnixNano"] = fmt.Sprint(span.EndTime().UnixNano())
	var events []any
	for _, event := range span.Events() {
		eventData := map[string]any{"name": event.Name}
		if !event.Time.IsZero() {
			eventData["timeUnixNano"] = fmt.Sprint(event.Time.UnixNano())
		}
		eventData["attributes"] = AttributesFromMap(attrsMap(event.Attributes))
		events = append(events, eventData)
	}
	if len(events) > 0 {
		encoded["events"] = events
	}
	var links []any
	for _, link := range span.Links() {
		linkCtx := link.SpanContext
		if !linkCtx.HasTraceID() || !linkCtx.HasSpanID() {
			continue
		}
		links = append(links, map[string]any{
			"traceId":    normalizeTraceIDText(linkCtx.TraceID().String()),
			"spanId":     normalizeSpanIDText(linkCtx.SpanID().String()),
			"attributes": AttributesFromMap(attrsMap(link.Attributes)),
		})
	}
	if len(links) > 0 {
		encoded["links"] = links
	}
	resourceAttrs := attrsMap(span.Resource().Attributes())
	payload := map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{"attributes": AttributesFromMap(resourceAttrs)},
				"scopeSpans": []any{
					map[string]any{"scope": map[string]any{}, "spans": []any{encoded}},
				},
			},
		},
	}
	return payload, &SpanIdentity{TraceID: traceID, SpanID: spanID}, nil
}

// spanKindName span 类别枚举名。
// Python: processor.py _span_kind。
func spanKindName(span sdktrace.ReadOnlySpan) string {
	return fmt.Sprintf("SPAN_KIND_%s", span.SpanKind().String())
}

// spanStatusOf status 映射。
// Python: processor.py _span_status。
func spanStatusOf(span sdktrace.ReadOnlySpan) map[string]any {
	status := span.Status()
	if status.Code.String() == "unset" {
		return map[string]any{}
	}
	result := map[string]any{"code": "STATUS_CODE_" + status.Code.String()}
	if status.Description != "" {
		result["message"] = status.Description
	}
	return result
}

// attrMapOf span 属性 → Go 映射。
func attrMapOf(span sdktrace.ReadOnlySpan) map[string]any {
	return attrsMap(span.Attributes())
}

// attrsMap otel attribute.KeyValue 切片 → map。
func attrsMap(items []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(items))
	for _, kv := range items {
		result[string(kv.Key)] = kv.Value.Emit()
	}
	return result
}

// jsonMarshal 编码助手。
func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }

// obsReexport 编译期引用守卫（obs 包常量在本文件使用）。
var _ = obs.OJTrajectoryRecordKind
