package telemetry

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"google.golang.org/grpc"
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

// startLocked 占位：Task 5 替换为真实实现（真实 doc 注释随实装移入）
func (r *SharedOtlpReceiver) startLocked() { r.disabled = true }

// startGrpcLocked 占位：Task 4 替换
func (r *SharedOtlpReceiver) startGrpcLocked() {}

// handleRequest 占位：Task 3 替换
func (r *SharedOtlpReceiver) handleRequest(w http.ResponseWriter, req *http.Request) {}

// fanout 占位：Task 5 替换
func (r *SharedOtlpReceiver) fanout(events []map[string]any) {}

// Aclose 占位：Task 6 替换
func (r *SharedOtlpReceiver) Aclose(ctx context.Context) {}
