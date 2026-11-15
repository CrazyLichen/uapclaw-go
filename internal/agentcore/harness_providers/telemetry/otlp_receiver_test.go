package telemetry

import (
	"testing"

	logsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// TestGetSharedOtlpReceiver_单例 单例恒等（Python get_shared_otlp_receiver 语义）
func TestGetSharedOtlpReceiver_单例(t *testing.T) {
	first := GetSharedOtlpReceiver()
	second := GetSharedOtlpReceiver()
	if first != second {
		t.Fatal("GetSharedOtlpReceiver 应返回同一实例")
	}
}

// TestNewSharedOtlpReceiver_初始态 新实例未启动、未禁用、端点为空
func TestNewSharedOtlpReceiver_初始态(t *testing.T) {
	r := NewSharedOtlpReceiver()
	if r.EndpointURL() != "" || r.GrpcEndpoint() != "" || r.BaseURL() != "" {
		t.Fatal("新实例端点应为空")
	}
	if _, ok := r.Subscribe(func(map[string]any) {}); !ok {
		t.Fatal("新实例首次订阅应成功并惰性启动")
	}
	r.Unsubscribe(1)
	if r.EndpointURL() == "" {
		t.Fatal("惰性启动后应产出 HTTP 端点")
	}
	r.Aclose(t.Context())
	if _, ok := r.Subscribe(func(map[string]any) {}); ok {
		t.Fatal("Aclose 后订阅应失败（永久禁用）")
	}
}

// ──────────────────────────── 解码层（T2） ────────────────────────────

// spanRequest 构造一个含单 span 的 ExportTraceServiceRequest 字节流（测试助手）
func spanRequest(t *testing.T, spanName string, attrs []*commonv1.KeyValue) []byte {
	t.Helper()
	req := &tracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcev1.Resource{Attributes: attrs},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonv1.InstrumentationScope{Name: "claude-code"},
				Spans: []*tracepb.Span{{
					Name:              spanName,
					TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
					SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
					ParentSpanId:      []byte{},
					StartTimeUnixNano: 1000,
					EndTimeUnixNano:   2000,
					Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
				}},
			}},
		}},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}

// logRequest 构造一个含单条日志的 ExportLogsServiceRequest 字节流（测试助手）
func logRequest(t *testing.T, body string, timeNS, observedNS uint64) []byte {
	t.Helper()
	req := &logsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			Resource: &resourcev1.Resource{},
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope: &commonv1.InstrumentationScope{Name: "claude-code"},
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano:         timeNS,
					ObservedTimeUnixNano: observedNS,
					Body:                 &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: body}},
					TraceId:              []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
					SpanId:               []byte{1, 2, 3, 4, 5, 6, 7, 8},
				}},
			}},
		}},
	}
	payload, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return payload
}

// TestDecodeSpans_span事件形状 Python :77-107 的字段全集逐项核对
func TestDecodeSpans_span事件形状(t *testing.T) {
	attrs := []*commonv1.KeyValue{
		{Key: "request_id", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "req-1"}}},
		{Key: "attempt", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 2}}},
		{Key: "success", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}}},
	}
	events, err := decodeSpans(spanRequest(t, "claude_code.llm_request", attrs))
	if err != nil || len(events) != 1 {
		t.Fatalf("decode: %v, n=%d", err, len(events))
	}
	event := events[0]
	for key, want := range map[string]any{
		"signal":         "trace",
		"name":           "claude_code.llm_request",
		"start_time_ns":  int64(1000),
		"end_time_ns":    int64(2000),
		"scope_name":     "claude-code",
		"status_code":    int32(tracepb.Status_STATUS_CODE_OK),
		"status_message": "",
		"trace_id":       "0102030405060708090a0b0c0d0e0f10",
		"span_id":        "0102030405060708",
		"parent_span_id": "",
	} {
		if event[key] != want {
			t.Fatalf("event[%q]=%v, want %v", key, event[key], want)
		}
	}
	ra := event["resource_attributes"].(map[string]any)
	if ra["request_id"] != "req-1" || ra["attempt"] != int64(2) || ra["success"] != true {
		t.Fatalf("resource_attributes 解码不符: %v", ra)
	}
}

// TestDecodeSpans_坏protobuf返回错误 对齐 Python ParseFromString 异常 → HTTP 400
func TestDecodeSpans_坏protobuf返回错误(t *testing.T) {
	if _, err := decodeSpans([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatal("坏 protobuf 应返回 error")
	}
}

// TestDecodeLogRecords_log事件形状 Python :110-142（time_ns 取 time 缺省回落 observed）
func TestDecodeLogRecords_log事件形状(t *testing.T) {
	events, err := decodeLogRecords(logRequest(t, "claude_code.api_response_body", 0, 3000))
	if err != nil || len(events) != 1 {
		t.Fatalf("decode: %v, n=%d", err, len(events))
	}
	event := events[0]
	if event["time_ns"] != int64(3000) || event["name"] != "claude_code.api_response_body" || event["signal"] != "log" {
		t.Fatalf("log 事件形状不符: %v", event)
	}
	if event["trace_id"] != "0102030405060708090a0b0c0d0e0f10" || event["span_id"] != "0102030405060708" {
		t.Fatalf("log id 字段不符: %v", event)
	}
	// time_unix_nano 有值时优先
	events, err = decodeLogRecords(logRequest(t, "body", 5000, 3000))
	if err != nil || events[0]["time_ns"] != int64(5000) {
		t.Fatalf("time 优先级不符: %v %v", err, events[0]["time_ns"])
	}
}

// TestAnyValue_嵌套结构 Python :52-69 的 array/kvlist/bytes 分支
func TestAnyValue_嵌套结构(t *testing.T) {
	value := &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{ArrayValue: &commonv1.ArrayValue{
		Values: []*commonv1.AnyValue{
			{Value: &commonv1.AnyValue_StringValue{StringValue: "a"}},
			{Value: &commonv1.AnyValue_KvlistValue{KvlistValue: &commonv1.KeyValueList{Values: []*commonv1.KeyValue{
				{Key: "k", Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 1.5}}},
			}}}},
			{Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte{9}}},
		},
	}}}
	got := anyValue(value)
	arr, ok := got.([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("array 解码不符: %#v", got)
	}
	if arr[0] != "a" {
		t.Fatalf("arr[0]=%v", arr[0])
	}
	kv, ok := arr[1].(map[string]any)
	if !ok || kv["k"] != 1.5 {
		t.Fatalf("kvlist 解码不符: %#v", arr[1])
	}
	if b, ok := arr[2].([]byte); !ok || len(b) != 1 {
		t.Fatalf("bytes 解码不符: %#v", arr[2])
	}
	if anyValue(nil) != nil {
		t.Fatal("nil AnyValue 应返回 nil（Python which-oneof 无值分支）")
	}
}
