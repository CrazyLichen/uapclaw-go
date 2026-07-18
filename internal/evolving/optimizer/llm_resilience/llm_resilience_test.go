package llm_resilience

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBaseModelClient 用于测试的模拟 LLM 客户端
type mockBaseModelClient struct {
	// invokeFn 自定义 Invoke 行为
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

func (m *mockBaseModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return nil, fmt.Errorf("mock invoke not configured")
}
func (m *mockBaseModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockBaseModelClient) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (m *mockBaseModelClient) SupportsKVCacheRelease() bool { return false }

// 确保 mockBaseModelClient 满足 BaseModelClient 接口
var _ model_clients.BaseModelClient = (*mockBaseModelClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLLMInvokePolicy_默认值 验证 LLMInvokePolicy 结构体字段
func TestLLMInvokePolicy_默认值(t *testing.T) {
	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 30,
		TotalBudgetSecs:    60,
		MaxAttempts:        2,
		BackoffBaseSecs:    1.0,
		RetryEmptyResponse: true,
	}
	if policy.AttemptTimeoutSecs != 30 {
		t.Errorf("AttemptTimeoutSecs = %f, want 30", policy.AttemptTimeoutSecs)
	}
	if policy.TotalBudgetSecs != 60 {
		t.Errorf("TotalBudgetSecs = %f, want 60", policy.TotalBudgetSecs)
	}
	if policy.MaxAttempts != 2 {
		t.Errorf("MaxAttempts = %d, want 2", policy.MaxAttempts)
	}
	if policy.BackoffBaseSecs != 1.0 {
		t.Errorf("BackoffBaseSecs = %f, want 1.0", policy.BackoffBaseSecs)
	}
	if !policy.RetryEmptyResponse {
		t.Error("RetryEmptyResponse should be true")
	}
}

// TestInvokeTextWithRetry_成功 首次调用成功返回文本
func TestInvokeTextWithRetry_成功(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("hello world"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	result, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result = %q, want %q", result, "hello world")
	}
}

// TestInvokeTextWithRetryAndPrompt_成功 首次调用成功返回文本和使用的 prompt
func TestInvokeTextWithRetryAndPrompt_成功(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("hello world"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	text, promptUsed, err := InvokeTextWithRetryAndPrompt(context.Background(), model, "test-model", "hi", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
	if promptUsed != "hi" {
		t.Errorf("promptUsed = %q, want %q", promptUsed, "hi")
	}
}

// TestInvokeTextWithRetry_空响应重试 首次返回空、第二次返回有效文本
func TestInvokeTextWithRetry_空响应重试(t *testing.T) {
	callCount := 0
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			return llmschema.NewAssistantMessage(""), nil
		}
		return llmschema.NewAssistantMessage("valid response"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	result, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "valid response" {
		t.Errorf("result = %q, want %q", result, "valid response")
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}

// TestInvokeTextWithRetry_空响应不重试 RetryEmptyResponse=false 时不重试空响应
func TestInvokeTextWithRetry_空响应不重试(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(""), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: false,
	}

	// RetryEmptyResponse=false 时空响应直接返回（视为成功）
	result, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("result = %q, want empty string", result)
	}
}

// TestInvokeTextWithRetry_调用失败 LLM 调用返回错误
func TestInvokeTextWithRetry_调用失败(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, fmt.Errorf("internal server error")
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	_, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	// 非超时类错误应走 invoke_failed 路径
	details, ok := baseErr.Details().(map[string]any)
	if !ok {
		t.Fatalf("details type = %T, want map[string]any", baseErr.Details())
	}
	if details["reason"] != reasonInvokeFailed {
		t.Errorf("reason = %v, want %q", details["reason"], reasonInvokeFailed)
	}
}

// TestInvokeTextWithRetry_超时回退到RetryPrompt 首次超时后使用短 prompt 重试
func TestInvokeTextWithRetry_超时回退到RetryPrompt(t *testing.T) {
	callCount := 0
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			return nil, context.DeadlineExceeded
		}
		return llmschema.NewAssistantMessage("short response"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        3,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	text, promptUsed, err := InvokeTextWithRetryAndPrompt(
		context.Background(), model, "test-model", "long prompt",
		policy,
		WithRetryPrompt("short prompt"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "short response" {
		t.Errorf("text = %q, want %q", text, "short response")
	}
	if promptUsed != "short prompt" {
		t.Errorf("promptUsed = %q, want %q", promptUsed, "short prompt")
	}
}

// TestInvokeTextWithRetry_不可用响应 isResultUsable 返回 false 时重试
func TestInvokeTextWithRetry_不可用响应(t *testing.T) {
	callCount := 0
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			return llmschema.NewAssistantMessage("invalid"), nil
		}
		return llmschema.NewAssistantMessage("valid JSON"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        3,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	result, err := InvokeTextWithRetry(
		context.Background(), model, "test-model", "hi",
		policy,
		WithIsResultUsable(func(s string) bool {
			return strings.Contains(s, "JSON")
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "valid JSON" {
		t.Errorf("result = %q, want %q", result, "valid JSON")
	}
}

// TestInvokeTextWithRetry_不可用响应耗尽 所有 attempt 都不可用时报错
func TestInvokeTextWithRetry_不可用响应耗尽(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage("invalid"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	_, err := InvokeTextWithRetry(
		context.Background(), model, "test-model", "hi",
		policy,
		WithIsResultUsable(func(s string) bool { return false }),
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	details, ok := baseErr.Details().(map[string]any)
	if !ok {
		t.Fatalf("details type = %T, want map[string]any", baseErr.Details())
	}
	if details["reason"] != reasonUnusableResponse {
		t.Errorf("reason = %v, want %q", details["reason"], reasonUnusableResponse)
	}
}

// TestInvokeTextWithRetry_总预算超限 policy.TotalBudgetSecs <= 0 直接报错
func TestInvokeTextWithRetry_总预算超限(t *testing.T) {
	model := newMockModel(t, nil)

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    0,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	_, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	details, ok := baseErr.Details().(map[string]any)
	if !ok {
		t.Fatalf("details type = %T, want map[string]any", baseErr.Details())
	}
	if details["reason"] != reasonTotalBudgetExceeded {
		t.Errorf("reason = %v, want %q", details["reason"], reasonTotalBudgetExceeded)
	}
	if details["attempts"] != 0 {
		t.Errorf("attempts = %v, want 0", details["attempts"])
	}
}

// TestInvokeTextWithRetry_空响应耗尽 所有 attempt 都返回空时报错
func TestInvokeTextWithRetry_空响应耗尽(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return llmschema.NewAssistantMessage(""), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	_, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	details, ok := baseErr.Details().(map[string]any)
	if !ok {
		t.Fatalf("details type = %T, want map[string]any", baseErr.Details())
	}
	if details["reason"] != reasonEmptyResponse {
		t.Errorf("reason = %v, want %q", details["reason"], reasonEmptyResponse)
	}
}

// TestIsTimeoutLike_各分支 验证超时判断的三个分支
func TestIsTimeoutLike_各分支(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"DeadlineExceeded", context.DeadlineExceeded, true},
		{"类型名包含timeout", &timeoutTestError{}, true},
		{"消息包含timeout", fmt.Errorf("connection timeout after 30s"), true},
		{"消息包含timed out", fmt.Errorf("request timed out"), true},
		{"非超时错误", fmt.Errorf("internal server error"), false},
		{"大小写不敏感", fmt.Errorf("Connection TIMEOUT"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTimeoutLike(tt.err)
			if got != tt.want {
				t.Errorf("isTimeoutLike(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// timeoutTestError 用于测试类型名包含 "timeout" 的错误
type timeoutTestError struct{}

func (e *timeoutTestError) Error() string { return "timeout test error" }

// TestSleepBeforeRetry_退避计算 验证指数退避和预算尊重
func TestSleepBeforeRetry_退避计算(t *testing.T) {
	tests := []struct {
		name           string
		backoffBase    float64
		attempt        int
		shouldSleep    bool
		maxDurationSec float64
	}{
		{"零退避跳过", 0, 1, false, 0},
		{"负退避跳过", -1, 1, false, 0},
		{"正常退避", 0.01, 1, true, 0.1},
		{"第二次退避翻倍", 0.01, 2, true, 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := LLMInvokePolicy{
				TotalBudgetSecs:    30,
				BackoffBaseSecs:    tt.backoffBase,
				MaxAttempts:        5,
				AttemptTimeoutSecs: 10,
			}
			startedAt := time.Now()

			start := time.Now()
			err := sleepBeforeRetry(context.Background(), policy, startedAt, tt.attempt)
			elapsed := time.Since(start).Seconds()

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.shouldSleep {
				if elapsed < 0.001 {
					t.Errorf("expected some sleep, got %fs", elapsed)
				}
				if elapsed > tt.maxDurationSec {
					t.Errorf("slept too long: %fs, max %fs", elapsed, tt.maxDurationSec)
				}
			} else {
				if elapsed > 0.1 {
					t.Errorf("should not sleep, but slept %fs", elapsed)
				}
			}
		})
	}
}

// TestSleepBeforeRetry_上下文取消 验证上下文取消时立即返回
func TestSleepBeforeRetry_上下文取消(t *testing.T) {
	policy := LLMInvokePolicy{
		TotalBudgetSecs:    30,
		BackoffBaseSecs:    10, // 很长的退避
		MaxAttempts:        5,
		AttemptTimeoutSecs: 10,
	}
	startedAt := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	err := sleepBeforeRetry(ctx, policy, startedAt, 1)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

// TestRaiseLLMResilienceError_错误构建 验证错误包含 reason/attempts/last_response
func TestRaiseLLMResilienceError_错误构建(t *testing.T) {
	lastErr := fmt.Errorf("some error")
	causeErr := fmt.Errorf("root cause")

	err := raiseLLMResilienceError(
		exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
		reasonInvokeFailed,
		3,
		lastErr,
		"partial response",
		causeErr,
	)

	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}

	// 验证消息
	if baseErr.Message() != reasonInvokeFailed {
		t.Errorf("Message() = %q, want %q", baseErr.Message(), reasonInvokeFailed)
	}

	// 验证详情
	details, ok := baseErr.Details().(map[string]any)
	if !ok {
		t.Fatalf("details type = %T, want map[string]any", baseErr.Details())
	}
	if details["reason"] != reasonInvokeFailed {
		t.Errorf("reason = %v, want %q", details["reason"], reasonInvokeFailed)
	}
	if details["attempts"] != 3 {
		t.Errorf("attempts = %v, want 3", details["attempts"])
	}
	if details["last_response"] != "partial response" {
		t.Errorf("last_response = %v, want %q", details["last_response"], "partial response")
	}
	if details["last_error"] != "some error" {
		t.Errorf("last_error = %v, want %q", details["last_error"], "some error")
	}

	// 验证 cause（优先使用传入的 cause）
	if !errors.Is(baseErr.Unwrap(), causeErr) {
		t.Errorf("cause = %v, want %v", baseErr.Unwrap(), causeErr)
	}
}

// TestRaiseLLMResilienceError_cause回退 验证 cause 为 nil 时回退到 lastError
func TestRaiseLLMResilienceError_cause回退(t *testing.T) {
	lastErr := fmt.Errorf("last error")

	err := raiseLLMResilienceError(
		exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, ""),
		reasonInvokeFailed,
		1,
		lastErr,
		"",
		nil, // cause 为 nil
	)

	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	// cause 应回退到 lastError
	if !errors.Is(baseErr.Unwrap(), lastErr) {
		t.Errorf("cause = %v, want %v (lastError fallback)", baseErr.Unwrap(), lastErr)
	}
}

// TestResponseToTextFromAssistantMessage_各种情况 验证文本提取
func TestResponseToTextFromAssistantMessage_各种情况(t *testing.T) {
	tests := []struct {
		name     string
		response *llmschema.AssistantMessage
		want     string
	}{
		{"nil", nil, ""},
		{"有内容", llmschema.NewAssistantMessage("hello"), "hello"},
		{"空内容", llmschema.NewAssistantMessage(""), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := responseToTextFromAssistantMessage(tt.response)
			if got != tt.want {
				t.Errorf("responseToTextFromAssistantMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestInvokeTextWithRetry_MaxAttempts小于1 验证 MaxAttempts < 1 时调整为 1
func TestInvokeTextWithRetry_MaxAttempts小于1(t *testing.T) {
	callCount := 0
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		return llmschema.NewAssistantMessage("ok"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        0, // 应调整为 1
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	result, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
}

// TestInvokeTextWithRetry_上下文取消 验证上下文取消时正确返回
func TestInvokeTextWithRetry_上下文取消(t *testing.T) {
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, context.Canceled
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	_, err := InvokeTextWithRetry(context.Background(), model, "test-model", "hi", policy)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestInvokeTextWithRetry_温度参数 验证温度参数传递
func TestInvokeTextWithRetry_温度参数(t *testing.T) {
	var capturedTemp *float64
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		params := model_clients.NewInvokeParams(opts...)
		capturedTemp = params.Temperature
		return llmschema.NewAssistantMessage("ok"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        2,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	_, err := InvokeTextWithRetry(
		context.Background(), model, "test-model", "hi",
		policy,
		WithTemperature(0.7),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedTemp == nil || *capturedTemp != 0.7 {
		t.Errorf("capturedTemp = %v, want 0.7", capturedTemp)
	}
}

// TestInvokeTextWithRetry_isResultUsablePanic 验证 isResultUsable panic 时视为不可用
func TestInvokeTextWithRetry_isResultUsablePanic(t *testing.T) {
	callCount := 0
	model := newMockModel(t, func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		callCount++
		if callCount == 1 {
			return llmschema.NewAssistantMessage("bad"), nil
		}
		return llmschema.NewAssistantMessage("good"), nil
	})

	policy := LLMInvokePolicy{
		AttemptTimeoutSecs: 10,
		TotalBudgetSecs:    30,
		MaxAttempts:        3,
		BackoffBaseSecs:    0,
		RetryEmptyResponse: true,
	}

	result, err := InvokeTextWithRetry(
		context.Background(), model, "test-model", "hi",
		policy,
		WithIsResultUsable(func(s string) bool {
			if s == "bad" {
				panic("check failed")
			}
			return true
		}),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "good" {
		t.Errorf("result = %q, want %q", result, "good")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMockModel 创建一个使用 mock client 的 Model 实例
func newMockModel(t *testing.T, invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)) *llm.Model {
	t.Helper()

	// 注册 mock client 到全局 registry
	mockClient := &mockBaseModelClient{invokeFn: invokeFn}
	providerName := fmt.Sprintf("mock_llm_resilience_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(providerName, "llm", func(modelConfig *llmschema.ModelRequestConfig, clientConfig *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		return mockClient
	})

	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: providerName,
		ClientID:       providerName + "_id",
	}
	modelConfig := &llmschema.ModelRequestConfig{
		ModelName: "test-model",
	}

	model, err := llm.NewModel(clientConfig, modelConfig)
	if err != nil {
		t.Fatalf("failed to create mock model: %v", err)
	}
	return model
}
