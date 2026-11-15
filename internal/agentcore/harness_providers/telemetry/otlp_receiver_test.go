package telemetry

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"
	"testing"
	"time"

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

// ──────────────────────────── HTTP 门（T3） ────────────────────────────

// TestHandleRequest_正常导出扇出 POST /v1/traces 的事件到达订阅者
func TestHandleRequest_正常导出扇出(t *testing.T) {
	r := NewSharedOtlpReceiver()
	received := make(chan map[string]any, 8)
	if _, ok := r.Subscribe(func(event map[string]any) { received <- event }); !ok {
		t.Fatal("subscribe")
	}
	url := "http://" + strings.TrimPrefix(r.EndpointURL(), "http://")
	resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader(spanRequest(t, "claude_code.llm_request", nil)))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	select {
	case event := <-received:
		if event["name"] != "claude_code.llm_request" {
			t.Fatalf("event=%v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("事件未到达订阅者")
	}
	r.Aclose(t.Context())
}

// TestHandleRequest_拒绝路径 405/404/400 语义（Python :343-361）
func TestHandleRequest_拒绝路径(t *testing.T) {
	r := NewSharedOtlpReceiver()
	_, _ = r.Subscribe(func(map[string]any) {})
	// endpoint 形如 http://127.0.0.1:P/v1/traces，取 origin 拼 404 路径
	host := strings.TrimSuffix(strings.TrimPrefix(r.EndpointURL(), "http://"), "/v1/traces")

	// 405：GET
	resp, _ := http.Get("http://" + host + "/v1/traces")
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	// 404：未知路径
	resp, _ = http.Post("http://"+host+"/v1/other", "application/x-protobuf", bytes.NewReader([]byte{0x01}))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("404 status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	// 400：空 body（Content-Length 0）
	resp, _ = http.Post("http://"+host+"/v1/traces", "application/x-protobuf", bytes.NewReader([]byte{}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("400 status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	r.Aclose(t.Context())
}

// TestHandleRequest_gzip导出 gzip 编码请求被有限解压后正常扇出
func TestHandleRequest_gzip导出(t *testing.T) {
	r := NewSharedOtlpReceiver()
	received := make(chan map[string]any, 8)
	_, _ = r.Subscribe(func(event map[string]any) { received <- event })
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, _ = writer.Write(spanRequest(t, "claude_code.llm_request", nil))
	writer.Close()
	url := "http://" + strings.TrimPrefix(r.EndpointURL(), "http://")
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("gzip post: %v status=%v", err, resp)
	}
	resp.Body.Close()
	select {
	case event := <-received:
		if event["name"] != "claude_code.llm_request" {
			t.Fatalf("event=%v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gzip 事件未到达")
	}
	r.Aclose(t.Context())
}

// TestHandleRequest_坏protobuf 400（Python ParseFromString 异常路径）
func TestHandleRequest_坏protobuf(t *testing.T) {
	r := NewSharedOtlpReceiver()
	_, _ = r.Subscribe(func(map[string]any) {})
	url := "http://" + strings.TrimPrefix(r.EndpointURL(), "http://")
	resp, _ := http.Post(url, "application/x-protobuf", bytes.NewReader([]byte{0xff, 0xff, 0xff}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	r.Aclose(t.Context())
}
