package callback

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventFilter 事件过滤器接口，在回调执行前拦截并控制执行行为。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (EventFilter)
type EventFilter interface {
	// Name 返回过滤器名称
	Name() string
	// Filter 执行过滤逻辑，返回 FilterResult 控制回调执行行为
	Filter(ctx context.Context, event string, callbackName string, data any) FilterResult
}

// RateLimitFilter 滑动窗口限流过滤器。
// 在 timeWindow 秒内最多允许 maxCalls 次调用，超限返回 SKIP。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (RateLimitFilter)
type RateLimitFilter struct {
	// name 过滤器名称
	name string
	// MaxCalls 时间窗口内最大调用次数
	MaxCalls int
	// TimeWindow 时间窗口（秒）
	TimeWindow float64
	// calls 每个键的调用时间列表
	calls map[string][]time.Time
	// mu 并发保护
	mu sync.Mutex
}

// CircuitBreakerFilter 断路器过滤器。
// 失败次数达到阈值后断开电路（返回 SKIP），超时后尝试重置。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (CircuitBreakerFilter)
type CircuitBreakerFilter struct {
	// name 过滤器名称
	name string
	// FailureThreshold 失败阈值
	FailureThreshold int
	// Timeout 断路器超时（秒），超时后尝试重置
	Timeout float64
	// failures 每个键的失败次数
	failures map[string]int
	// isOpen 每个键的断路状态
	isOpen map[string]bool
	// lastFailureTime 每个键的最后一次失败时间
	lastFailureTime map[string]time.Time
	// mu 并发保护
	mu sync.Mutex
}

// ValidationFilter 校验过滤器。
// 使用 Validator 函数校验数据，校验失败返回 SKIP。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (ValidationFilter)
type ValidationFilter struct {
	// name 过滤器名称
	name string
	// Validator 校验函数，返回 true 表示通过
	Validator func(any) bool
}

// LoggingFilter 日志过滤器。
// 记录事件/回调/参数信息，始终返回 CONTINUE。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (LoggingFilter)
type LoggingFilter struct {
	// name 过滤器名称
	name string
}

// AuthFilter 鉴权过滤器。
// 从 data（map[string]any）中取 "user_role"，与 RequiredRole 比对，不匹配返回 SKIP。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (AuthFilter)
type AuthFilter struct {
	// name 过滤器名称
	name string
	// RequiredRole 要求的角色
	RequiredRole string
}

// ParamModifyFilter 参数修改过滤器。
// 使用 Modifier 函数转换数据，返回 MODIFY 动作及修改后数据。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (ParamModifyFilter)
type ParamModifyFilter struct {
	// name 过滤器名称
	name string
	// Modifier 数据修改函数
	Modifier func(any) any
}

// ConditionalFilter 条件过滤器。
// 条件为假时返回 ActionOnFalse 动作，为真时返回 CONTINUE。
//
// 对应 Python: openjiuwen/core/runner/callback/filters.py (ConditionalFilter)
type ConditionalFilter struct {
	// name 过滤器名称
	name string
	// Condition 条件谓词函数
	Condition func(ctx context.Context, event, callbackName string, data any) bool
	// ActionOnFalse 条件为假时的动作
	ActionOnFalse FilterAction
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Name 返回过滤器名称。
func (f *RateLimitFilter) Name() string { return f.name }

// Filter 执行滑动窗口限流检查。
// 超限返回 FilterResult{Action: FilterActionSkip}，否则返回 FilterActionContinue。
//
// 对应 Python: RateLimitFilter.filter()
func (f *RateLimitFilter) Filter(_ context.Context, event string, callbackName string, _ any) FilterResult {
	now := time.Now()
	key := event + ":" + callbackName

	f.mu.Lock()
	defer f.mu.Unlock()

	// 清理超出时间窗口的旧记录
	windowDuration := time.Duration(f.TimeWindow * float64(time.Second))
	times := f.calls[key]
	validIdx := 0
	for i, t := range times {
		if now.Sub(t) <= windowDuration {
			validIdx = i
			break
		}
		validIdx = i + 1
	}
	f.calls[key] = times[validIdx:]

	// 检查限流
	if len(f.calls[key]) >= f.MaxCalls {
		return FilterResult{
			Action: FilterActionSkip,
			Reason: fmt.Sprintf("Rate limit exceeded: %d calls per %.1fs", f.MaxCalls, f.TimeWindow),
		}
	}

	// 记录本次调用
	f.calls[key] = append(f.calls[key], now)

	return FilterResult{Action: FilterActionContinue}
}

// NewRateLimitFilter 创建滑动窗口限流过滤器。
// name 为空时使用默认名称 "RateLimit"。
func NewRateLimitFilter(maxCalls int, timeWindow float64, name string) *RateLimitFilter {
	n := "RateLimit"
	if name != "" {
		n = name
	}
	return &RateLimitFilter{
		name:       n,
		MaxCalls:   maxCalls,
		TimeWindow: timeWindow,
		calls:      make(map[string][]time.Time),
	}
}

// Name 返回过滤器名称。
func (f *CircuitBreakerFilter) Name() string { return f.name }

// Filter 执行断路器检查。
// 断路打开时返回 SKIP，超时后尝试重置并返回 CONTINUE。
//
// 对应 Python: CircuitBreakerFilter.filter()
func (f *CircuitBreakerFilter) Filter(_ context.Context, event string, callbackName string, _ any) FilterResult {
	key := event + ":" + callbackName
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.isOpen[key] {
		// 超时后尝试关闭断路器
		lastTime, hasLast := f.lastFailureTime[key]
		if hasLast && now.Sub(lastTime).Seconds() > f.Timeout {
			f.isOpen[key] = false
			f.failures[key] = 0
		} else {
			return FilterResult{
				Action: FilterActionSkip,
				Reason: fmt.Sprintf("Circuit breaker open, retry after %.1fs", f.Timeout),
			}
		}
	}

	return FilterResult{Action: FilterActionContinue}
}

// IsOpen 检查断路器是否打开。
//
// 对应 Python: CircuitBreakerFilter.is_open()
func (f *CircuitBreakerFilter) IsOpen(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isOpen[key] {
		lastTime, hasLast := f.lastFailureTime[key]
		if hasLast && time.Since(lastTime).Seconds() > f.Timeout {
			f.isOpen[key] = false
			f.failures[key] = 0
			return false
		}
		return true
	}
	return false
}

// RecordSuccess 记录成功执行，重置失败计数。
//
// 对应 Python: CircuitBreakerFilter.record_success()
func (f *CircuitBreakerFilter) RecordSuccess(event string, callbackName string) {
	key := event + ":" + callbackName
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[key] = 0
}

// RecordFailure 记录失败执行，达到阈值后打开断路器。
//
// 对应 Python: CircuitBreakerFilter.record_failure()
func (f *CircuitBreakerFilter) RecordFailure(event string, callbackName string) {
	key := event + ":" + callbackName
	now := time.Now()

	f.mu.Lock()
	defer f.mu.Unlock()

	f.failures[key]++
	f.lastFailureTime[key] = now

	if f.failures[key] >= f.FailureThreshold {
		f.isOpen[key] = true
		logger.Warn(logger.ComponentAgentCore).
			Str("event", event).
			Str("callback_name", callbackName).
			Str("key", key).
			Msg("断路器已打开")
	}
}

// NewCircuitBreakerFilter 创建断路器过滤器。
// name 为空时使用默认名称 "CircuitBreaker"。
func NewCircuitBreakerFilter(failureThreshold int, timeout float64, name string) *CircuitBreakerFilter {
	n := "CircuitBreaker"
	if name != "" {
		n = name
	}
	return &CircuitBreakerFilter{
		name:             n,
		FailureThreshold: failureThreshold,
		Timeout:          timeout,
		failures:         make(map[string]int),
		isOpen:           make(map[string]bool),
		lastFailureTime:  make(map[string]time.Time),
	}
}

// Name 返回过滤器名称。
func (f *ValidationFilter) Name() string { return f.name }

// Filter 执行数据校验，校验失败返回 SKIP。
//
// 对应 Python: ValidationFilter.filter()
func (f *ValidationFilter) Filter(_ context.Context, _ string, _ string, data any) FilterResult {
	if !f.Validator(data) {
		return FilterResult{
			Action: FilterActionSkip,
			Reason: "Argument validation failed",
		}
	}
	return FilterResult{Action: FilterActionContinue}
}

// NewValidationFilter 创建校验过滤器。
// name 为空时使用默认名称 "Validation"。
func NewValidationFilter(validator func(any) bool, name string) *ValidationFilter {
	n := "Validation"
	if name != "" {
		n = name
	}
	return &ValidationFilter{
		name:      n,
		Validator: validator,
	}
}

// Name 返回过滤器名称。
func (f *LoggingFilter) Name() string { return f.name }

// Filter 记录事件/回调/参数信息，始终返回 CONTINUE。
//
// 对应 Python: LoggingFilter.filter()
func (f *LoggingFilter) Filter(_ context.Context, event string, callbackName string, data any) FilterResult {
	logger.Info(logger.ComponentAgentCore).
		Str("event", event).
		Str("callback_name", callbackName).
		Any("data", data).
		Msg("事件过滤日志")
	return FilterResult{Action: FilterActionContinue}
}

// NewLoggingFilter 创建日志过滤器。
// name 为空时使用默认名称 "Logging"。
func NewLoggingFilter(name string) *LoggingFilter {
	n := "Logging"
	if name != "" {
		n = name
	}
	return &LoggingFilter{name: n}
}

// Name 返回过滤器名称。
func (f *AuthFilter) Name() string { return f.name }

// Filter 执行角色鉴权，从 data（map[string]any）中取 "user_role" 进行比对。
// 不匹配返回 SKIP，匹配返回 CONTINUE。
//
// 对应 Python: AuthFilter.filter()
func (f *AuthFilter) Filter(_ context.Context, _ string, _ string, data any) FilterResult {
	userRole := "guest"
	if m, ok := data.(map[string]any); ok {
		if v, exists := m["user_role"]; exists {
			if s, isStr := v.(string); isStr {
				userRole = s
			}
		}
	}

	if userRole != f.RequiredRole {
		return FilterResult{
			Action: FilterActionSkip,
			Reason: fmt.Sprintf("Unauthorized: requires %s, got %s", f.RequiredRole, userRole),
		}
	}

	return FilterResult{Action: FilterActionContinue}
}

// NewAuthFilter 创建鉴权过滤器。
// name 为空时使用默认名称 "Auth"。
func NewAuthFilter(requiredRole string, name string) *AuthFilter {
	n := "Auth"
	if name != "" {
		n = name
	}
	return &AuthFilter{
		name:         n,
		RequiredRole: requiredRole,
	}
}

// Name 返回过滤器名称。
func (f *ParamModifyFilter) Name() string { return f.name }

// Filter 使用 Modifier 修改数据，返回 MODIFY 动作及修改后数据。
//
// 对应 Python: ParamModifyFilter.filter()
func (f *ParamModifyFilter) Filter(_ context.Context, _ string, _ string, data any) FilterResult {
	modified := f.Modifier(data)
	return FilterResult{
		Action:       FilterActionModify,
		ModifiedData: modified,
	}
}

// NewParamModifyFilter 创建参数修改过滤器。
// name 为空时使用默认名称 "ParamModify"。
func NewParamModifyFilter(modifier func(any) any, name string) *ParamModifyFilter {
	n := "ParamModify"
	if name != "" {
		n = name
	}
	return &ParamModifyFilter{
		name:     n,
		Modifier: modifier,
	}
}

// Name 返回过滤器名称。
func (f *ConditionalFilter) Name() string { return f.name }

// Filter 评估条件，为真返回 CONTINUE，为假返回 ActionOnFalse。
//
// 对应 Python: ConditionalFilter.filter()
func (f *ConditionalFilter) Filter(ctx context.Context, event string, callbackName string, data any) FilterResult {
	if f.Condition(ctx, event, callbackName, data) {
		return FilterResult{Action: FilterActionContinue}
	}
	return FilterResult{
		Action: f.ActionOnFalse,
		Reason: "Condition not satisfied",
	}
}

// NewConditionalFilter 创建条件过滤器。
// name 为空时使用默认名称 "Conditional"。
func NewConditionalFilter(
	condition func(ctx context.Context, event, callbackName string, data any) bool,
	actionOnFalse FilterAction,
	name string,
) *ConditionalFilter {
	n := "Conditional"
	if name != "" {
		n = name
	}
	return &ConditionalFilter{
		name:          n,
		Condition:     condition,
		ActionOnFalse: actionOnFalse,
	}
}
