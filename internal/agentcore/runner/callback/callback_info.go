package callback

import "sort"

// ──────────────────────────── 结构体 ────────────────────────────

// CallbackInfo 回调注册信息，包装回调函数及其元数据。
//
// 对应 Python: CallbackInfo (openjiuwen/core/runner/callback/models.py)
// 回调按 Priority 降序排列（数值越大越先执行），
// 相同 Priority 按 CreatedAt 升序排列（先注册的先执行）。
type CallbackInfo[F any] struct {
	// Callback 回调函数
	Callback F
	// Priority 执行优先级（降序，数值越大越先执行）
	Priority int
	// Once 是否只执行一次（执行后自动禁用）
	Once bool
	// Enabled 是否启用
	Enabled bool
	// Namespace 命名空间（用于分组注销）
	Namespace string
	// Tags 标签集合（用于过滤）
	Tags []string
	// MaxRetries 失败后最大重试次数
	MaxRetries int
	// RetryDelay 重试间隔（秒）
	RetryDelay float64
	// Timeout 执行超时（秒），0 表示不限
	Timeout float64
	// CreatedAt 注册时间戳（秒）
	CreatedAt float64
	// Wrapper 装饰器 wrapper 函数（用于反注册）
	Wrapper F
	// CallbackType 语义类型标记（如 "transform"）
	CallbackType string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortCallbacks 按 Priority 降序排列，相同 Priority 按 CreatedAt 升序排列（先注册的先执行）。
//
// 对应 Python: self._callbacks[event].sort(key=lambda x: x.priority, reverse=True)
func sortCallbacks[F any](callbacks []*CallbackInfo[F]) {
	sort.SliceStable(callbacks, func(i, j int) bool {
		if callbacks[i].Priority != callbacks[j].Priority {
			return callbacks[i].Priority > callbacks[j].Priority // 降序
		}
		return callbacks[i].CreatedAt < callbacks[j].CreatedAt // 升序
	})
}
