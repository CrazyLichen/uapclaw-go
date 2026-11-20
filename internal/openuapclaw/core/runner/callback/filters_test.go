package callback

import (
	"context"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRateLimitFilter_未超限返回Continue 测试限流未超限时返回 CONTINUE
func TestRateLimitFilter_未超限返回Continue(t *testing.T) {
	f := NewRateLimitFilter(3, 1.0, "")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		result := f.Filter(ctx, "test_event", "my_callback", nil)
		if result.Action != FilterActionContinue {
			t.Errorf("第 %d 次调用期望 CONTINUE，实际 %v", i+1, result.Action)
		}
	}
}

// TestRateLimitFilter_超限返回Skip 测试限流超限时返回 SKIP
func TestRateLimitFilter_超限返回Skip(t *testing.T) {
	f := NewRateLimitFilter(2, 10.0, "")
	ctx := context.Background()

	// 前两次应该通过
	for i := 0; i < 2; i++ {
		result := f.Filter(ctx, "test_event", "my_callback", nil)
		if result.Action != FilterActionContinue {
			t.Errorf("第 %d 次调用期望 CONTINUE，实际 %v", i+1, result.Action)
		}
	}

	// 第三次应该被限流
	result := f.Filter(ctx, "test_event", "my_callback", nil)
	if result.Action != FilterActionSkip {
		t.Errorf("超限后期望 SKIP，实际 %v", result.Action)
	}
	if result.Reason == "" {
		t.Error("超限时 Reason 不应为空")
	}
}

// TestRateLimitFilter_窗口过期后重置 测试时间窗口过期后限流重置
func TestRateLimitFilter_窗口过期后重置(t *testing.T) {
	f := NewRateLimitFilter(1, 0.05, "") // 50ms 窗口
	ctx := context.Background()

	// 第一次通过
	result1 := f.Filter(ctx, "ev", "cb", nil)
	if result1.Action != FilterActionContinue {
		t.Errorf("第一次调用期望 CONTINUE，实际 %v", result1.Action)
	}

	// 第二次被限流
	result2 := f.Filter(ctx, "ev", "cb", nil)
	if result2.Action != FilterActionSkip {
		t.Errorf("第二次调用期望 SKIP，实际 %v", result2.Action)
	}

	// 等待窗口过期
	time.Sleep(60 * time.Millisecond)

	// 窗口过期后应重新允许
	result3 := f.Filter(ctx, "ev", "cb", nil)
	if result3.Action != FilterActionContinue {
		t.Errorf("窗口过期后期望 CONTINUE，实际 %v", result3.Action)
	}
}

// TestRateLimitFilter_不同键独立计数 测试不同 event:callbackName 键独立计数
func TestRateLimitFilter_不同键独立计数(t *testing.T) {
	f := NewRateLimitFilter(1, 10.0, "")
	ctx := context.Background()

	// 第一个键通过
	r1 := f.Filter(ctx, "event1", "cb1", nil)
	if r1.Action != FilterActionContinue {
		t.Errorf("event1:cb1 期望 CONTINUE，实际 %v", r1.Action)
	}

	// 第二个键也通过（独立计数）
	r2 := f.Filter(ctx, "event2", "cb2", nil)
	if r2.Action != FilterActionContinue {
		t.Errorf("event2:cb2 期望 CONTINUE，实际 %v", r2.Action)
	}
}

// TestRateLimitFilter_Name 测试 Name 方法
func TestRateLimitFilter_Name(t *testing.T) {
	f := NewRateLimitFilter(1, 1.0, "")
	if f.Name() != "RateLimit" {
		t.Errorf("期望名称 RateLimit，实际 %s", f.Name())
	}
	f2 := NewRateLimitFilter(1, 1.0, "Custom")
	if f2.Name() != "Custom" {
		t.Errorf("期望名称 Custom，实际 %s", f2.Name())
	}
}

// TestCircuitBreakerFilter_断路关闭时返回Continue 测试断路器关闭时正常放行
func TestCircuitBreakerFilter_断路关闭时返回Continue(t *testing.T) {
	f := NewCircuitBreakerFilter(3, 60.0, "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", nil)
	if result.Action != FilterActionContinue {
		t.Errorf("断路关闭时期望 CONTINUE，实际 %v", result.Action)
	}
}

// TestCircuitBreakerFilter_断路打开时返回Skip 测试断路器打开时返回 SKIP
func TestCircuitBreakerFilter_断路打开时返回Skip(t *testing.T) {
	f := NewCircuitBreakerFilter(2, 60.0, "")
	ctx := context.Background()

	// 记录足够失败次数打开断路器
	f.RecordFailure("event", "callback")
	f.RecordFailure("event", "callback")

	result := f.Filter(ctx, "event", "callback", nil)
	if result.Action != FilterActionSkip {
		t.Errorf("断路打开时期望 SKIP，实际 %v", result.Action)
	}
	if result.Reason == "" {
		t.Error("断路打开时 Reason 不应为空")
	}
}

// TestCircuitBreakerFilter_超时后重置 测试断路器超时后尝试重置
func TestCircuitBreakerFilter_超时后重置(t *testing.T) {
	f := NewCircuitBreakerFilter(1, 0.05, "") // 50ms 超时
	ctx := context.Background()

	// 记录失败打开断路器
	f.RecordFailure("event", "callback")

	// 断路器应打开
	r1 := f.Filter(ctx, "event", "callback", nil)
	if r1.Action != FilterActionSkip {
		t.Errorf("断路打开时期望 SKIP，实际 %v", r1.Action)
	}

	// 等待超时
	time.Sleep(60 * time.Millisecond)

	// 超时后应重置为关闭
	r2 := f.Filter(ctx, "event", "callback", nil)
	if r2.Action != FilterActionContinue {
		t.Errorf("超时重置后期望 CONTINUE，实际 %v", r2.Action)
	}
}

// TestCircuitBreakerFilter_RecordSuccess重置失败计数 测试 RecordSuccess 重置失败计数
func TestCircuitBreakerFilter_RecordSuccess重置失败计数(t *testing.T) {
	f := NewCircuitBreakerFilter(3, 60.0, "")
	ctx := context.Background()

	f.RecordFailure("event", "callback")
	f.RecordFailure("event", "callback")
	f.RecordSuccess("event", "callback")

	// 再记录一次失败，不应达到阈值
	f.RecordFailure("event", "callback")
	result := f.Filter(ctx, "event", "callback", nil)
	if result.Action != FilterActionContinue {
		t.Errorf("RecordSuccess 重置后期望 CONTINUE，实际 %v", result.Action)
	}
}

// TestCircuitBreakerFilter_Name 测试 Name 方法
func TestCircuitBreakerFilter_Name(t *testing.T) {
	f := NewCircuitBreakerFilter(5, 60.0, "")
	if f.Name() != "CircuitBreaker" {
		t.Errorf("期望名称 CircuitBreaker，实际 %s", f.Name())
	}
	f2 := NewCircuitBreakerFilter(5, 60.0, "MyBreaker")
	if f2.Name() != "MyBreaker" {
		t.Errorf("期望名称 MyBreaker，实际 %s", f2.Name())
	}
}

// TestValidationFilter_校验通过返回Continue 测试校验通过返回 CONTINUE
func TestValidationFilter_校验通过返回Continue(t *testing.T) {
	validator := func(data any) bool { return true }
	f := NewValidationFilter(validator, "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", "valid_data")
	if result.Action != FilterActionContinue {
		t.Errorf("校验通过期望 CONTINUE，实际 %v", result.Action)
	}
}

// TestValidationFilter_校验失败返回Skip 测试校验失败返回 SKIP
func TestValidationFilter_校验失败返回Skip(t *testing.T) {
	validator := func(data any) bool { return false }
	f := NewValidationFilter(validator, "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", "invalid_data")
	if result.Action != FilterActionSkip {
		t.Errorf("校验失败期望 SKIP，实际 %v", result.Action)
	}
	if result.Reason == "" {
		t.Error("校验失败时 Reason 不应为空")
	}
}

// TestValidationFilter_条件校验 测试基于数据内容的条件校验
func TestValidationFilter_条件校验(t *testing.T) {
	validator := func(data any) bool {
		if s, ok := data.(string); ok {
			return s != "bad"
		}
		return false
	}
	f := NewValidationFilter(validator, "")
	ctx := context.Background()

	r1 := f.Filter(ctx, "ev", "cb", "good")
	if r1.Action != FilterActionContinue {
		t.Errorf("有效数据期望 CONTINUE，实际 %v", r1.Action)
	}

	r2 := f.Filter(ctx, "ev", "cb", "bad")
	if r2.Action != FilterActionSkip {
		t.Errorf("无效数据期望 SKIP，实际 %v", r2.Action)
	}
}

// TestValidationFilter_Name 测试 Name 方法
func TestValidationFilter_Name(t *testing.T) {
	f := NewValidationFilter(func(any) bool { return true }, "")
	if f.Name() != "Validation" {
		t.Errorf("期望名称 Validation，实际 %s", f.Name())
	}
}

// TestLoggingFilter_始终返回Continue 测试 LoggingFilter 始终返回 CONTINUE
func TestLoggingFilter_始终返回Continue(t *testing.T) {
	f := NewLoggingFilter("")
	ctx := context.Background()

	result := f.Filter(ctx, "test_event", "test_callback", map[string]any{"key": "value"})
	if result.Action != FilterActionContinue {
		t.Errorf("LoggingFilter 期望 CONTINUE，实际 %v", result.Action)
	}
}

// TestLoggingFilter_Name 测试 Name 方法
func TestLoggingFilter_Name(t *testing.T) {
	f := NewLoggingFilter("")
	if f.Name() != "Logging" {
		t.Errorf("期望名称 Logging，实际 %s", f.Name())
	}
	f2 := NewLoggingFilter("MyLogger")
	if f2.Name() != "MyLogger" {
		t.Errorf("期望名称 MyLogger，实际 %s", f2.Name())
	}
}

// TestAuthFilter_角色匹配返回Continue 测试角色匹配时返回 CONTINUE
func TestAuthFilter_角色匹配返回Continue(t *testing.T) {
	f := NewAuthFilter("admin", "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", map[string]any{"user_role": "admin"})
	if result.Action != FilterActionContinue {
		t.Errorf("角色匹配期望 CONTINUE，实际 %v", result.Action)
	}
}

// TestAuthFilter_角色不匹配返回Skip 测试角色不匹配时返回 SKIP
func TestAuthFilter_角色不匹配返回Skip(t *testing.T) {
	f := NewAuthFilter("admin", "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", map[string]any{"user_role": "guest"})
	if result.Action != FilterActionSkip {
		t.Errorf("角色不匹配期望 SKIP，实际 %v", result.Action)
	}
	if result.Reason == "" {
		t.Error("鉴权失败时 Reason 不应为空")
	}
}

// TestAuthFilter_缺少角色默认Guest 测试 data 中无 user_role 时默认为 guest
func TestAuthFilter_缺少角色默认Guest(t *testing.T) {
	f := NewAuthFilter("admin", "")
	ctx := context.Background()

	// 空数据
	r1 := f.Filter(ctx, "event", "callback", map[string]any{})
	if r1.Action != FilterActionSkip {
		t.Errorf("缺少 user_role 期望 SKIP，实际 %v", r1.Action)
	}

	// 非 map 数据
	r2 := f.Filter(ctx, "event", "callback", "string_data")
	if r2.Action != FilterActionSkip {
		t.Errorf("非 map 数据期望 SKIP，实际 %v", r2.Action)
	}

	// guest 角色匹配 guest 要求
	fGuest := NewAuthFilter("guest", "")
	r3 := fGuest.Filter(ctx, "event", "callback", nil)
	if r3.Action != FilterActionContinue {
		t.Errorf("guest 角色匹配期望 CONTINUE，实际 %v", r3.Action)
	}
}

// TestAuthFilter_Name 测试 Name 方法
func TestAuthFilter_Name(t *testing.T) {
	f := NewAuthFilter("admin", "")
	if f.Name() != "Auth" {
		t.Errorf("期望名称 Auth，实际 %s", f.Name())
	}
}

// TestParamModifyFilter_返回Modify动作 测试 ParamModifyFilter 返回 MODIFY 动作
func TestParamModifyFilter_返回Modify动作(t *testing.T) {
	modifier := func(data any) any {
		if m, ok := data.(map[string]any); ok {
			m["modified"] = true
			return m
		}
		return data
	}
	f := NewParamModifyFilter(modifier, "")
	ctx := context.Background()

	data := map[string]any{"key": "value"}
	result := f.Filter(ctx, "event", "callback", data)
	if result.Action != FilterActionModify {
		t.Errorf("ParamModifyFilter 期望 MODIFY，实际 %v", result.Action)
	}

	modified, ok := result.ModifiedData.(map[string]any)
	if !ok {
		t.Fatal("ModifiedData 类型断言失败")
	}
	if modified["modified"] != true {
		t.Error("修改后数据应包含 modified=true")
	}
}

// TestParamModifyFilter_简单值修改 测试简单值的修改
func TestParamModifyFilter_简单值修改(t *testing.T) {
	doubler := func(data any) any {
		if n, ok := data.(int); ok {
			return n * 2
		}
		return data
	}
	f := NewParamModifyFilter(doubler, "")
	ctx := context.Background()

	result := f.Filter(ctx, "ev", "cb", 5)
	if result.Action != FilterActionModify {
		t.Errorf("期望 MODIFY，实际 %v", result.Action)
	}
	if result.ModifiedData != 10 {
		t.Errorf("期望修改后数据为 10，实际 %v", result.ModifiedData)
	}
}

// TestParamModifyFilter_Name 测试 Name 方法
func TestParamModifyFilter_Name(t *testing.T) {
	f := NewParamModifyFilter(func(data any) any { return data }, "")
	if f.Name() != "ParamModify" {
		t.Errorf("期望名称 ParamModify，实际 %s", f.Name())
	}
}

// TestConditionalFilter_条件为真返回Continue 测试条件为真时返回 CONTINUE
func TestConditionalFilter_条件为真返回Continue(t *testing.T) {
	condition := func(_ context.Context, _, _ string, _ any) bool { return true }
	f := NewConditionalFilter(condition, FilterActionSkip, "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", nil)
	if result.Action != FilterActionContinue {
		t.Errorf("条件为真期望 CONTINUE，实际 %v", result.Action)
	}
}

// TestConditionalFilter_条件为假返回Skip 测试条件为假时返回 SKIP
func TestConditionalFilter_条件为假返回Skip(t *testing.T) {
	condition := func(_ context.Context, _, _ string, _ any) bool { return false }
	f := NewConditionalFilter(condition, FilterActionSkip, "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", nil)
	if result.Action != FilterActionSkip {
		t.Errorf("条件为假期望 SKIP，实际 %v", result.Action)
	}
}

// TestConditionalFilter_条件为假返回Stop 测试条件为假时返回自定义动作 STOP
func TestConditionalFilter_条件为假返回Stop(t *testing.T) {
	condition := func(_ context.Context, _, _ string, _ any) bool { return false }
	f := NewConditionalFilter(condition, FilterActionStop, "")
	ctx := context.Background()

	result := f.Filter(ctx, "event", "callback", nil)
	if result.Action != FilterActionStop {
		t.Errorf("条件为假期望 STOP，实际 %v", result.Action)
	}
}

// TestConditionalFilter_基于事件名条件 测试基于事件名的条件判断
func TestConditionalFilter_基于事件名条件(t *testing.T) {
	condition := func(_ context.Context, event, _ string, _ any) bool {
		return event == "allowed_event"
	}
	f := NewConditionalFilter(condition, FilterActionSkip, "")
	ctx := context.Background()

	r1 := f.Filter(ctx, "allowed_event", "cb", nil)
	if r1.Action != FilterActionContinue {
		t.Errorf("允许的事件期望 CONTINUE，实际 %v", r1.Action)
	}

	r2 := f.Filter(ctx, "denied_event", "cb", nil)
	if r2.Action != FilterActionSkip {
		t.Errorf("拒绝的事件期望 SKIP，实际 %v", r2.Action)
	}
}

// TestConditionalFilter_Name 测试 Name 方法
func TestConditionalFilter_Name(t *testing.T) {
	f := NewConditionalFilter(func(_ context.Context, _, _ string, _ any) bool { return true }, FilterActionSkip, "")
	if f.Name() != "Conditional" {
		t.Errorf("期望名称 Conditional，实际 %s", f.Name())
	}
}

// TestEventFilter_接口兼容性 测试所有过滤器都实现 EventFilter 接口
func TestEventFilter_接口兼容性(t *testing.T) {
	var _ EventFilter = NewRateLimitFilter(1, 1.0, "")
	var _ EventFilter = NewCircuitBreakerFilter(5, 60.0, "")
	var _ EventFilter = NewValidationFilter(func(any) bool { return true }, "")
	var _ EventFilter = NewLoggingFilter("")
	var _ EventFilter = NewAuthFilter("admin", "")
	var _ EventFilter = NewParamModifyFilter(func(data any) any { return data }, "")
	var _ EventFilter = NewConditionalFilter(
		func(_ context.Context, _, _ string, _ any) bool { return true },
		FilterActionSkip,
		"",
	)
}

// TestCircuitBreakerFilter_IsOpen 测试 IsOpen 方法
func TestCircuitBreakerFilter_IsOpen(t *testing.T) {
	f := NewCircuitBreakerFilter(1, 60.0, "")

	// 初始状态关闭
	if f.IsOpen("event:callback") {
		t.Error("初始状态断路器应关闭")
	}

	// 记录失败打开断路器
	f.RecordFailure("event", "callback")
	if !f.IsOpen("event:callback") {
		t.Error("达到阈值后断路器应打开")
	}

	// 不存在的键应关闭
	if f.IsOpen("nonexistent") {
		t.Error("不存在的键断路器应关闭")
	}
}

// TestCircuitBreakerFilter_IsOpen_超时后重置 测试 IsOpen 超时后重置
func TestCircuitBreakerFilter_IsOpen_超时后重置(t *testing.T) {
	f := NewCircuitBreakerFilter(1, 0.05, "") // 50ms 超时
	f.RecordFailure("event", "callback")
	if !f.IsOpen("event:callback") {
		t.Error("断路器应打开")
	}

	time.Sleep(60 * time.Millisecond)
	if f.IsOpen("event:callback") {
		t.Error("超时后断路器应重置为关闭")
	}
}
