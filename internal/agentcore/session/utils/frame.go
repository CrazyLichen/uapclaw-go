package utils

// EndFrame 流结束帧，携带结束的 producer id。
// Python: openjiuwen/core/session/utils.py:391 @dataclass EndFrame{source: str}
// Frame = Union[Any, EndFrame]（Python 侧类型别名，Go 以 any 承载帧值）
type EndFrame struct {
	// Source 结束的 producer id
	Source string
}

// IsEndFrame 判断帧值是否为结束帧
func IsEndFrame(v any) bool {
	_, ok := v.(*EndFrame)
	return ok
}

// MessageTransformer 消息变换函数。
// 偏差 D4：Python session/state/base.py:22 的 Transformer 注解为 Callable[[ReadableStateLike], Any]，
// 与 stream_actor 运行时用法（transformer(origin_message)）不符，Go 按真实语义定义。
type MessageTransformer func(map[string]any) map[string]any
