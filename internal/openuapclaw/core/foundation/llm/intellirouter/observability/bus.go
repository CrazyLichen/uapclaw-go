// Package observability 事件总线。← observability/bus.py（一文件一文件对齐）。
package observability

import "fmt"

// EventBus 事件总线。← observability/bus.py:13 EventBus
//
// 管理 EventHandler 的注册和事件分发。无 handler 注册时 emit() 几乎零开销。
// 事件通过直接分发，单个 handler 异常不影响其他 handler 和路由主路径。
type EventBus struct {
	handlers []EventHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

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
