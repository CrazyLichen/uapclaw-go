// Package observability 路由事件与事件总线。
// ← intelli_router/observability/（events.py/bus.py/handler.py；
// metrics/web_dashboard/logging_hook 不移植，偏差 D-3）
package observability

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 枚举 ────────────────────────────

// RoutingEventType 路由事件类型。← observability/events.py:9 RoutingEventType(str, Enum)
type RoutingEventType string

// 路由事件类型取值，**这是 23.4b _CoreRoutingEventBridge._event_type_value
// 的映射键，一字段不裁** ← events.py:11-29
const (
	// EventTypeRequestStarted 请求生命周期：开始
	EventTypeRequestStarted RoutingEventType = "request_started"
	// EventTypeRequestSucceeded 请求生命周期：成功
	EventTypeRequestSucceeded RoutingEventType = "request_succeeded"
	// EventTypeRequestRetried 请求生命周期：重试
	EventTypeRequestRetried RoutingEventType = "request_retried"
	// EventTypeAllDeploymentsExhausted 请求生命周期：全部部署耗尽
	EventTypeAllDeploymentsExhausted RoutingEventType = "all_deployments_exhausted"
	// EventTypeRequestCancelled 非流式请求被取消的终态事件（与流式的
	// STREAM_CANCELLED 对称），保证 REQUEST_STARTED 后口径闭合 ← py:21-23
	EventTypeRequestCancelled RoutingEventType = "request_cancelled"
	// EventTypeStreamStarted 流式请求：开始
	EventTypeStreamStarted RoutingEventType = "stream_started"
	// EventTypeStreamRouteSelected 流式请求：选中路由
	EventTypeStreamRouteSelected RoutingEventType = "stream_route_selected"
	// EventTypeStreamSucceeded 流式请求：成功
	EventTypeStreamSucceeded RoutingEventType = "stream_succeeded"
	// EventTypeStreamCancelled 流式请求被消费者提前中断（break/aclose/取消）：
	// 终态事件之一，extra 可携带 reason（"consumer_break" / "cancelled"）← py:26-28
	EventTypeStreamCancelled RoutingEventType = "stream_cancelled"
	// EventTypeDeploymentsUpdated 部署管理（热替换）：extra 携带 removed_ids
	// （List[str]），订阅方（如 MetricsCollector）据此清理已下线部署的统计键 ← py:31-33
	EventTypeDeploymentsUpdated RoutingEventType = "deployments_updated"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RoutingEvent 一次路由事件的完整信息。
// ← observability/events.py:34 RoutingEvent
//
// 不携带请求 body (messages) 以避免敏感数据泄露和内存开销。（py:36-38 原文）
type RoutingEvent struct {
	// EventType 事件类型（必填）
	EventType RoutingEventType
	// RequestID 请求唯一标识（必填）
	RequestID string
	// Model 请求模型（必填）
	Model string

	// Timestamp 时间戳
	Timestamp float64

	// DeploymentID 部署信息 ← py:44
	DeploymentID *string
	// Provider provider 名 ← py:45
	Provider *string

	// Latency 性能数据 ← py:48
	Latency *float64
	// PromptTokens ← py:49
	PromptTokens *int
	// CompletionTokens ← py:50
	CompletionTokens *int
	// TotalTokens ← py:51
	TotalTokens *int

	// Attempt 重试信息 ← py:54
	Attempt int
	// TotalAttempts ← py:55
	TotalAttempts int

	// ErrorType 错误信息 ← py:58
	ErrorType *string
	// ErrorMessage ← py:59
	ErrorMessage *string

	// ChunkCount 流式特有 ← py:62
	ChunkCount *int

	// Extra 扩展字段 ← py:65
	Extra map[string]any
}

// EventHandler 事件处理器接口见 handler.go（← handler.py EventHandler）。

// EventBus 事件总线。← observability/bus.py:13 EventBus
//
// 管理 EventHandler 的注册和事件分发。无 handler 注册时 emit() 几乎零开销。
// 事件通过直接分发，单个 handler 异常不影响其他 handler 和路由主路径。
type EventBus struct {
	handlers []EventHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRoutingEvent 构造事件（timestamp = time.time()，← py:40 field(default_factory)）。
func NewRoutingEvent(eventType RoutingEventType, requestID, model string) RoutingEvent {
	return RoutingEvent{
		EventType: eventType,
		RequestID: requestID,
		Model:     model,
		Timestamp: nowFloat(),
	}
}

// NewRequestID 生成短唯一请求 ID。← events.py:72 new_request_id
func NewRequestID() string {
	return uuid.NewString()[:12] // ← py:74 uuid.uuid4().hex[:12]
}

// Handlers 已注册的 handler 列表（只读副本）。← bus.py:23 handlers property
func (b *EventBus) Handlers() []EventHandler {
	result := make([]EventHandler, len(b.handlers))
	copy(result, b.handlers)
	return result
}

// Register 注册一个事件处理器。← bus.py:27 register
func (b *EventBus) Register(handler EventHandler) {
	b.handlers = append(b.handlers, handler)
}

// Unregister 注销一个事件处理器。← bus.py:31 unregister
// （Python list.remove 对不存在元素抛 ValueError；Go 侧对不存在元素为
// no-op——调用方契约是「注销已注册的 handler」，语义等价）
func (b *EventBus) Unregister(handler EventHandler) {
	for i, h := range b.handlers {
		if h == handler {
			b.handlers = append(b.handlers[:i], b.handlers[i+1:]...)
			return
		}
	}
}

// Emit 分发事件到所有已注册的处理器。← bus.py:35 emit
//
// 每个 handler 依次调用。如果某个 handler 抛异常（panic），
// 异常被捕获并记录日志，不会影响后续 handler 或请求路径。← py:42-47
func (b *EventBus) Emit(event RoutingEvent) {
	if len(b.handlers) == 0 {
		return // ← py:41 无 handler 零开销
	}
	for _, handler := range b.handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// ← py:45-49 logger.debug(..., exc_info=True)
					// 语义：单个 handler 失败只记日志（debug 级）
					_ = fmt.Sprintf("EventHandler %s failed for event %s: %v",
						handler.Name(), event.EventType, r)
				}
			}()
			handler.HandleEvent(event)
		}()
	}
}

// ToDict 序列化为字典，省略 None 字段。← events.py:76 to_dict
func (e *RoutingEvent) ToDict() map[string]any {
	result := map[string]any{
		"event_type": string(e.EventType), // ← py:79 event_type.value
		"request_id": e.RequestID,
		"model":      e.Model,
		"timestamp":  e.Timestamp,
	}
	// ← py:82-91 optional_fields 省略 None
	if e.DeploymentID != nil {
		result["deployment_id"] = *e.DeploymentID
	}
	if e.Provider != nil {
		result["provider"] = *e.Provider
	}
	if e.Latency != nil {
		result["latency"] = *e.Latency
	}
	if e.PromptTokens != nil {
		result["prompt_tokens"] = *e.PromptTokens
	}
	if e.CompletionTokens != nil {
		result["completion_tokens"] = *e.CompletionTokens
	}
	if e.TotalTokens != nil {
		result["total_tokens"] = *e.TotalTokens
	}
	if e.ErrorType != nil {
		result["error_type"] = *e.ErrorType
	}
	if e.ErrorMessage != nil {
		result["error_message"] = *e.ErrorMessage
	}
	if e.ChunkCount != nil {
		result["chunk_count"] = *e.ChunkCount
	}
	if e.Attempt > 0 {
		result["attempt"] = e.Attempt // ← py:92-93
	}
	if e.TotalAttempts > 0 {
		result["total_attempts"] = e.TotalAttempts // ← py:94-95
	}
	if len(e.Extra) > 0 {
		result["extra"] = e.Extra // ← py:96-97
	}
	return result
}

// SupportedEventTypes 返回全部事件类型字符串（23.4b 桥接层映射核对用）。
func SupportedEventTypes() []string {
	return []string{
		string(EventTypeRequestStarted),
		string(EventTypeRequestSucceeded),
		string(EventTypeRequestRetried),
		string(EventTypeAllDeploymentsExhausted),
		string(EventTypeRequestCancelled),
		string(EventTypeStreamStarted),
		string(EventTypeStreamRouteSelected),
		string(EventTypeStreamSucceeded),
		string(EventTypeStreamCancelled),
		string(EventTypeDeploymentsUpdated),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nowFloat 当前时间（秒，对齐 Python time.time()）。
func nowFloat() float64 {
	return float64(time.Now().UnixMilli()) / 1000
}
