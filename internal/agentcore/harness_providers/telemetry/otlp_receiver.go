package telemetry

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	logsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SharedOtlpReceiver 一个回环 OTLP/HTTP（+gRPC 伴随）服务器，把解码后的
// span/log 事件扇出给订阅者回调。
// Python: otlp_receiver.py:145-158 SharedOtlpReceiver
type SharedOtlpReceiver struct {
	// mu 保护 subscribers 与生命周期字段（Python asyncio.Lock/_start_lock 等价；
	// Go 同步启动无让出点，Python 的 _starting 标志省略，行为等价——D-tel-1）
	mu               sync.Mutex
	subscribers      map[int]func(map[string]any)
	nextSubscriberID int
	listener         net.Listener
	httpServer       *http.Server
	grpcServer       *grpc.Server
	disabled         bool
	// endpoint CLI 子进程应导出的 OTLP/HTTP 端点（Python :157）
	endpoint string
	// grpcEndpoint gRPC 伴随监听端点（Python :158）
	grpcEndpoint string
	// inFlight 在途请求处理协程（Python _tasks 集合等价）
	inFlight sync.WaitGroup
}

// traceServicer TraceService 的 Export 直通扇出。
// Python: otlp_receiver.py:270-275 _TraceServicer
type traceServicer struct {
	tracev1.UnimplementedTraceServiceServer
	receiver *SharedOtlpReceiver
}

// logsServicer LogsService 的 Export 直通扇出。
// Python: otlp_receiver.py:277-282 _LogsServicer
type logsServicer struct {
	logsv1.UnimplementedLogsServiceServer
	receiver *SharedOtlpReceiver
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxRequestBytes 单个 OTLP 请求体上限（Python :33 _MAX_REQUEST_BYTES）
	maxRequestBytes = 16 * 1024 * 1024
	// maxDecompressedBytes 解压后上限（Python :34 _MAX_DECOMPRESSED_BYTES）
	maxDecompressedBytes = 32 * 1024 * 1024
	// requestReadTimeout 读超时（Python :35 _REQUEST_READ_TIMEOUT_S）
	requestReadTimeout = 5 * time.Second
	// responseWriteTimeout 写超时（Python :36 _RESPONSE_WRITE_TIMEOUT_S）
	responseWriteTimeout = 2 * time.Second
	// closeTimeout Aclose 排空超时（Python :37 _CLOSE_TIMEOUT_S）
	closeTimeout = 1 * time.Second
	// maxHeaderBytes 请求头上限（Python :339 64KiB 判定，由 net/http 承担——D-tel-2）
	maxHeaderBytes = 64 * 1024

	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// OTELResourceSourceID 注入每个 CLI 进程的资源属性 key，订阅者据此把自己的
// 事件与其他 harness 区分开（D-tel-3：上游值为 openjiuwen.harness.source.id）。
// Python: otlp_receiver.py:40
const OTELResourceSourceID = "uapclaw.harness.source.id"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sharedReceiver 进程级接收器单例（Python :422 _receiver）
	sharedReceiver *SharedOtlpReceiver
	// singletonMu 保护单例惰性构造
	singletonMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSharedOtlpReceiver 返回进程级接收器单例。
// Python: otlp_receiver.py:425-430 get_shared_otlp_receiver
func GetSharedOtlpReceiver() *SharedOtlpReceiver {
	singletonMu.Lock()
	defer singletonMu.Unlock()
	if sharedReceiver == nil {
		sharedReceiver = NewSharedOtlpReceiver()
	}
	return sharedReceiver
}

// NewSharedOtlpReceiver 创建独立的接收器实例（测试与显式构造用；
// 生产入口是 GetSharedOtlpReceiver）。
func NewSharedOtlpReceiver() *SharedOtlpReceiver {
	return &SharedOtlpReceiver{
		subscribers: map[int]func(map[string]any){},
	}
}

// Subscribe 注册事件回调并返回订阅者 id；首次调用惰性启动接收器。
// ok=false 表示接收器已禁用或无法绑定，调用方回落到无原生 span 模式
// （上游行为：从 SDK 消息流单独上报请求）。
// Python: otlp_receiver.py:160-182 subscribe
func (r *SharedOtlpReceiver) Subscribe(callback func(map[string]any)) (subscriberID int, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return 0, false
	}
	if r.listener == nil {
		r.startLocked()
		if r.disabled {
			return 0, false
		}
	}
	r.nextSubscriberID++
	r.subscribers[r.nextSubscriberID] = callback
	return r.nextSubscriberID, true
}

// Unsubscribe 注销一个订阅者回调；共享接收器继续服务。
// Python: otlp_receiver.py:184-186 unsubscribe
func (r *SharedOtlpReceiver) Unsubscribe(subscriberID int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subscribers, subscriberID)
}

// EndpointURL 返回 CLI 子进程应导出的 OTLP/HTTP 端点（可能为空串）。
// Python: otlp_receiver.py:188-191 endpoint_url
func (r *SharedOtlpReceiver) EndpointURL() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.endpoint
}

// BaseURL 返回接收器 origin，供自己拼信号路径的 CLI 使用。
// Python: otlp_receiver.py:193-199 base_url（rsplit("/v1/", 1)[0]）
func (r *SharedOtlpReceiver) BaseURL() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := strings.LastIndex(r.endpoint, "/v1/")
	if index < 0 {
		return r.endpoint
	}
	return r.endpoint[:index]
}

// GrpcEndpoint 返回 gRPC 伴随监听端点（可能为空串）。
func (r *SharedOtlpReceiver) GrpcEndpoint() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.grpcEndpoint
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decompressGzipLimited 有限解压一个 gzip body，禁止无界展开。
// Python: otlp_receiver.py:43-49 _decompress_gzip_limited
func decompressGzipLimited(payload []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decompressed, err := io.ReadAll(io.LimitReader(reader, maxDecompressedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(decompressed) > maxDecompressedBytes {
		return nil, errors.New("decompressed OTLP request is too large")
	}
	return decompressed, nil
}

// anyValue 把一个 OTLP AnyValue protobuf 转成普通 Go 值。
// Python: otlp_receiver.py:52-69 _any_value
func anyValue(value *commonv1.AnyValue) any {
	if value == nil {
		return nil
	}
	switch inner := value.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return inner.StringValue
	case *commonv1.AnyValue_BoolValue:
		return inner.BoolValue
	case *commonv1.AnyValue_IntValue:
		return inner.IntValue
	case *commonv1.AnyValue_DoubleValue:
		return inner.DoubleValue
	case *commonv1.AnyValue_BytesValue:
		return bytes.Clone(inner.BytesValue)
	case *commonv1.AnyValue_ArrayValue:
		result := make([]any, 0, len(inner.ArrayValue.GetValues()))
		for _, item := range inner.ArrayValue.GetValues() {
			result = append(result, anyValue(item))
		}
		return result
	case *commonv1.AnyValue_KvlistValue:
		result := make(map[string]any, len(inner.KvlistValue.GetValues()))
		for _, item := range inner.KvlistValue.GetValues() {
			result[item.GetKey()] = anyValue(item.GetValue())
		}
		return result
	default:
		return nil
	}
}

// attributes 把 repeated KeyValue 转成映射。
// Python: otlp_receiver.py:72-74 _attributes
func attributes(items []*commonv1.KeyValue) map[string]any {
	result := make(map[string]any, len(items))
	for _, item := range items {
		result[item.GetKey()] = anyValue(item.GetValue())
	}
	return result
}

// decodeSpans 解码一个二进制 OTLP ExportTraceServiceRequest 的全部 span。
// Python: otlp_receiver.py:77-107 _decode_spans
func decodeSpans(payload []byte) ([]map[string]any, error) {
	var request tracev1.ExportTraceServiceRequest
	if err := proto.Unmarshal(payload, &request); err != nil {
		return nil, err
	}
	var events []map[string]any
	for _, resourceSpans := range request.GetResourceSpans() {
		resourceAttributes := attributes(resourceSpans.GetResource().GetAttributes())
		for _, scopeSpans := range resourceSpans.GetScopeSpans() {
			scopeName := scopeSpans.GetScope().GetName()
			for _, span := range scopeSpans.GetSpans() {
				events = append(events, map[string]any{
					"signal":              "trace",
					"name":                span.GetName(),
					"start_time_ns":       int64(span.GetStartTimeUnixNano()),
					"end_time_ns":         int64(span.GetEndTimeUnixNano()),
					"attributes":          attributes(span.GetAttributes()),
					"resource_attributes": resourceAttributes,
					"scope_name":          scopeName,
					"trace_id":            hex.EncodeToString(span.GetTraceId()),
					"span_id":             hex.EncodeToString(span.GetSpanId()),
					"parent_span_id":      hex.EncodeToString(span.GetParentSpanId()),
					"status_code":         int32(span.GetStatus().GetCode()),
					"status_message":      span.GetStatus().GetMessage(),
				})
			}
		}
	}
	return events, nil
}

// decodeLogRecords 解码一个二进制 OTLP ExportLogsServiceRequest 的全部日志记录。
// Claude Code 的原始 API body 事件（claude_code.api_request_body /
// claude_code.api_response_body）走 OTLP logs 信号；每条变成一个带 body 的
// span 事件形状的事件字典。
// Python: otlp_receiver.py:110-142 _decode_log_records
func decodeLogRecords(payload []byte) ([]map[string]any, error) {
	var request logsv1.ExportLogsServiceRequest
	if err := proto.Unmarshal(payload, &request); err != nil {
		return nil, err
	}
	var events []map[string]any
	for _, resourceLogs := range request.GetResourceLogs() {
		resourceAttributes := attributes(resourceLogs.GetResource().GetAttributes())
		for _, scopeLogs := range resourceLogs.GetScopeLogs() {
			scopeName := scopeLogs.GetScope().GetName()
			for _, logRecord := range scopeLogs.GetLogRecords() {
				// time_ns 取 time_unix_nano，缺省回落 observed_time_unix_nano，再缺省 0
				// （Python :133-134 的 `time or observed or 0` 链）
				timeNS := int64(logRecord.GetTimeUnixNano())
				if timeNS == 0 {
					timeNS = int64(logRecord.GetObservedTimeUnixNano())
				}
				events = append(events, map[string]any{
					"signal":              "log",
					"name":                logRecord.GetBody().GetStringValue(),
					"time_ns":             timeNS,
					"attributes":          attributes(logRecord.GetAttributes()),
					"resource_attributes": resourceAttributes,
					"scope_name":          scopeName,
					"trace_id":            hex.EncodeToString(logRecord.GetTraceId()),
					"span_id":             hex.EncodeToString(logRecord.GetSpanId()),
				})
			}
		}
	}
	return events, nil
}

// ──────────────────────────── 占位实现（后续 Task 实装） ────────────────────────────

// startLocked 绑定回环 HTTP 与 gRPC 监听；失败永久禁用（观测 best-effort）。
// Python: otlp_receiver.py:213-257 _start。
// Python 先 import 探测 protobuf 支持（:216-226）；Go 侧 proto/otlp 为编译期
// 依赖，无运行时探测对应物（结构性省略，语义无损失）。
func (r *SharedOtlpReceiver) startLocked() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Warn(logComponent).Msgf("otel: shared loopback OTLP receiver could not start; native spans disabled: %v", err)
		r.disabled = true
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port
	r.listener = listener
	r.endpoint = fmt.Sprintf("http://127.0.0.1:%d/v1/traces", port)
	r.httpServer = &http.Server{
		Handler:           http.HandlerFunc(r.handleRequest),
		ReadTimeout:       requestReadTimeout,
		ReadHeaderTimeout: requestReadTimeout,
		WriteTimeout:      responseWriteTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	// 协程内用局部引用：Aclose 会把 r.httpServer 置 nil，闭包不得回读字段
	httpServer := r.httpServer
	r.inFlight.Add(1)
	go func() {
		defer r.inFlight.Done()
		// Serve 返回 ErrServerClosed 属正常停机；其他错误仅记录（best-effort）
		if serveErr := httpServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Warn(logComponent).Msgf("otel: shared OTLP receiver rejected a request: %v", serveErr)
		}
	}()
	r.startGrpcLocked()
	logger.Info(logComponent).Msgf("otel: shared loopback OTLP receiver started endpoint=%s grpc_endpoint=%s subscribers=%d", r.endpoint, r.grpcEndpoint, len(r.subscribers))
}

// handleRequest 处理一个 OTLP/HTTP 导出请求。
// D-tel-2：承载层 net/http（Python _accept 的 408 分支由 ReadTimeout 连接
// 掐断等价承担），业务校验与 Python :330-375 _handle_request 逐条对应：
// 仅 POST（405）、仅 /v1/traces /v1/logs（404）、Content-Length 校验（400）、
// 16MiB 上限、gzip 有限解压（32MiB）、解码失败 400。
func (r *SharedOtlpReceiver) handleRequest(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		respond(w, http.StatusMethodNotAllowed)
		return
	}
	path := req.URL.Path
	if path != "/v1/traces" && path != "/v1/logs" {
		respond(w, http.StatusNotFound)
		return
	}
	contentLength := req.ContentLength
	if contentLength <= 0 || contentLength > maxRequestBytes {
		respond(w, http.StatusBadRequest)
		return
	}
	payload, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBytes+1))
	if err != nil || len(payload) > maxRequestBytes {
		respond(w, http.StatusBadRequest)
		return
	}
	if strings.EqualFold(req.Header.Get("Content-Encoding"), "gzip") {
		payload, err = decompressGzipLimited(payload)
		if err != nil {
			respond(w, http.StatusBadRequest)
			return
		}
	} else if len(payload) > maxDecompressedBytes {
		respond(w, http.StatusBadRequest)
		return
	}
	var events []map[string]any
	if path == "/v1/traces" {
		events, err = decodeSpans(payload)
	} else {
		events, err = decodeLogRecords(payload)
	}
	if err != nil {
		respond(w, http.StatusBadRequest)
		return
	}
	r.fanout(events)
	respond(w, http.StatusOK)
}

// respond 写最小 HTTP 响应（Python :377-391 _respond：application/x-protobuf、
// 空 body、Connection: close）。
func respond(w http.ResponseWriter, statusCode int) {
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Header().Set("Connection", "close")
	w.WriteHeader(statusCode)
}

// startGrpcLocked 在 HTTP 监听旁启动 gRPC OTLP 监听。
// Claude Code 的 OTLP exporter 只说 gRPC（其 http/protobuf 模式会静默连上
// 而不发送数据），故 HTTP 门旁伴随一扇 gRPC 门。失败非致命：HTTP 门继续
// 服务说 HTTP 的 exporter。
// Python: otlp_receiver.py:259-299 _start_grpc
func (r *SharedOtlpReceiver) startGrpcLocked() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		logger.Warn(logComponent).Msg("otel: shared OTLP gRPC listener could not bind; native model-request observation disabled")
		return
	}
	server := grpc.NewServer()
	tracev1.RegisterTraceServiceServer(server, &traceServicer{receiver: r})
	logsv1.RegisterLogsServiceServer(server, &logsServicer{receiver: r})
	go func() {
		// gRPC 是可选副通道（Python :298 注释），Serve 失败仅记录
		if serveErr := server.Serve(listener); serveErr != nil {
			logger.Warn(logComponent).Msgf("otel: shared OTLP gRPC listener failed to start: %v", serveErr)
		}
	}()
	r.grpcServer = server
	r.grpcEndpoint = fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
}

// Export Python :274 经 SerializeToString 复用 HTTP 解码路径，Go 一比一保留
// （Marshal + decodeSpans，而非直接遍历 req）。
func (s *traceServicer) Export(ctx context.Context, req *tracev1.ExportTraceServiceRequest) (*tracev1.ExportTraceServiceResponse, error) {
	payload, err := proto.Marshal(req)
	if err == nil {
		events, decodeErr := decodeSpans(payload)
		if decodeErr == nil {
			s.receiver.fanout(events)
		}
	}
	return &tracev1.ExportTraceServiceResponse{}, nil
}

// Export 同 traceServicer：Marshal + decodeLogRecords 复用解码路径。
func (s *logsServicer) Export(ctx context.Context, req *logsv1.ExportLogsServiceRequest) (*logsv1.ExportLogsServiceResponse, error) {
	payload, err := proto.Marshal(req)
	if err == nil {
		events, decodeErr := decodeLogRecords(payload)
		if decodeErr == nil {
			s.receiver.fanout(events)
		}
	}
	return &logsv1.ExportLogsServiceResponse{}, nil
}

// fanout 把解码事件广播给每个订阅者回调。
// D-tel-1：Python 把事件投递到订阅者的事件循环（claudecode/observation.py:622-625
// call_soon_threadsafe）；Go 无事件循环，fanout 同步直调，跨线程投递由订阅者
// 自行解决（channel/事件）。回调可能来自任意 goroutine（HTTP handler 或
// gRPC worker），订阅者必须非阻塞。
// Python: otlp_receiver.py:201-211 _fanout
func (r *SharedOtlpReceiver) fanout(events []map[string]any) {
	r.mu.Lock()
	callbacks := make([]func(map[string]any), 0, len(r.subscribers))
	for _, callback := range r.subscribers {
		callbacks = append(callbacks, callback)
	}
	r.mu.Unlock()
	for _, event := range events {
		for _, callback := range callbacks {
			invokeSubscriber(callback, event)
		}
	}
}

// invokeSubscriber 调用单个回调；panic 吞掉记 Warn（telemetry best-effort）。
// Python: otlp_receiver.py:205-211
func invokeSubscriber(callback func(map[string]any), event map[string]any) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Msgf("otel: shared OTLP receiver subscriber callback failed: %v", rec)
		}
	}()
	callback(event)
}

// Aclose 停止接收遥测并排空在途请求。
// 进程级单例通常与进程同寿；本方法供测试与显式停机路径使用。
// Python: otlp_receiver.py:393-419 aclose
func (r *SharedOtlpReceiver) Aclose(ctx context.Context) {
	r.mu.Lock()
	httpServer := r.httpServer
	grpcServer := r.grpcServer
	r.listener = nil
	r.httpServer = nil
	r.grpcServer = nil
	r.endpoint = ""
	r.grpcEndpoint = ""
	r.mu.Unlock()
	if httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, closeTimeout)
		defer cancel()
		// Shutdown 关 listener 并等待在途 handler；超时后未完成的连接由
		// Python _tasks 的 cancel 语义等价兜底（连接随进程收尾，无泄漏面）。
		_ = httpServer.Shutdown(shutdownCtx)
	}
	if grpcServer != nil {
		grpcServer.Stop() // Python grpc_server.stop(0)：立即停
	}
	// 排空在途请求协程（Python :410-416 asyncio.wait + cancel）
	done := make(chan struct{})
	go func() {
		r.inFlight.Wait()
		close(done)
	}()
	timer := time.NewTimer(closeTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	r.mu.Lock()
	r.subscribers = map[int]func(map[string]any){}
	r.disabled = true
	r.mu.Unlock()
}
