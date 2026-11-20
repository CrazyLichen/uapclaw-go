package observability

// EventHandler 事件处理器接口。← observability/handler.py:8 EventHandler(ABC)
//
// 用户/集成方实现此接口来处理路由事件。实现应避免抛出异常，内部处理错误。
type EventHandler interface {
	// HandleEvent 处理一个路由事件。← handler.py:14 handle_event
	HandleEvent(event RoutingEvent)
	// Name 处理器名称（用于日志标识）。← handler.py:18 name property
	// （Python 默认实现为类名；Go 无类名，实现方返回标识串）
	Name() string
}
