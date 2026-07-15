package callback

import (
	"sort"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// CallbackMetrics 回调执行指标，记录调用次数、耗时、错误率。
// 并发安全：内部使用 sync.Mutex 保护所有字段。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (CallbackMetrics)
type CallbackMetrics struct {
	mu sync.Mutex
	// CallCount 调用次数
	CallCount int
	// TotalTime 总耗时（秒）
	TotalTime float64
	// MinTime 最小耗时（秒）
	MinTime float64
	// MaxTime 最大耗时（秒）
	MaxTime float64
	// ErrorCount 错误次数
	ErrorCount int
	// LastCallTime 最后调用时间
	LastCallTime time.Time
}

// FilterResult 过滤器返回结果。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (FilterResult)
type FilterResult struct {
	// Action 过滤器动作
	Action FilterAction
	// ModifiedData 修改后数据（仅 FilterActionModify 时使用）
	ModifiedData any
	// Reason 原因说明
	Reason string
}

// ChainContext 链式执行上下文。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (ChainContext)
type ChainContext struct {
	// Event 事件名
	Event string
	// InitialData 初始数据
	InitialData any
	// Results 各回调执行结果
	Results []any
	// Metadata 元数据
	Metadata map[string]any
	// CurrentIndex 当前执行的回调索引
	CurrentIndex int
	// IsCompleted 是否已完成
	IsCompleted bool
	// IsRolledBack 是否已回滚
	IsRolledBack bool
	// StartTime 开始时间
	StartTime time.Time
}

// ChainResult 链式执行结果。
//
// 对应 Python: openjiuwen/core/runner/callback/models.py (ChainResult)
type ChainResult struct {
	// Action 链执行动作
	Action ChainAction
	// Result 最终结果
	Result any
	// Context 执行上下文
	Context *ChainContext
	// Error 执行错误
	Error error
}

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

// ──────────────────────────── 导出函数 ────────────────────────────

// Update 记录一次回调执行。
//
// 对应 Python: CallbackMetrics.update(execution_time, is_error)
func (m *CallbackMetrics) Update(executionTime float64, isError bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallCount++
	m.TotalTime += executionTime
	if m.CallCount == 1 || executionTime < m.MinTime {
		m.MinTime = executionTime
	}
	if executionTime > m.MaxTime {
		m.MaxTime = executionTime
	}
	if isError {
		m.ErrorCount++
	}
	m.LastCallTime = time.Now()
}

// AvgTime 平均执行时间（秒）。
//
// 对应 Python: CallbackMetrics.avg_time
func (m *CallbackMetrics) AvgTime() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CallCount == 0 {
		return 0
	}
	return m.TotalTime / float64(m.CallCount)
}

// ToDict 序列化为 map。
//
// 对应 Python: CallbackMetrics.to_dict()
func (m *CallbackMetrics) ToDict() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	avgTime := float64(0)
	if m.CallCount > 0 {
		avgTime = m.TotalTime / float64(m.CallCount)
	}
	return map[string]any{
		"call_count":     m.CallCount,
		"total_time":     m.TotalTime,
		"min_time":       m.MinTime,
		"max_time":       m.MaxTime,
		"error_count":    m.ErrorCount,
		"avg_time":       avgTime,
		"last_call_time": m.LastCallTime,
	}
}

// GetLastResult 获取最后一个结果。
func (c *ChainContext) GetLastResult() any {
	if len(c.Results) == 0 {
		return nil
	}
	return c.Results[len(c.Results)-1]
}

// ElapsedTime 已耗时。
func (c *ChainContext) ElapsedTime() time.Duration {
	return time.Since(c.StartTime)
}

// SetMetadata 设置元数据。
func (c *ChainContext) SetMetadata(key string, value any) {
	if c.Metadata == nil {
		c.Metadata = make(map[string]any)
	}
	c.Metadata[key] = value
}

// GetMetadata 获取元数据。
func (c *ChainContext) GetMetadata(key string) (any, bool) {
	if c.Metadata == nil {
		return nil, false
	}
	v, ok := c.Metadata[key]
	return v, ok
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
