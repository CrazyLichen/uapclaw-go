package openai

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeHTTPError 带状态码的错误，供判据测试使用。
type fakeHTTPError struct {
	status int
	msg    string
}

func (e *fakeHTTPError) Error() string   { return e.msg }
func (e *fakeHTTPError) StatusCode() int { return e.status }

// newTestClient 构造仅带拒绝集的客户端。
func newTestClient() *OpenAIModelClient {
	return &OpenAIModelClient{rejectingDisabledThinking: newRejectingThinkingSet()}
}

// ──────────────────────────── 判据：状态码 ────────────────────────────

// TestIsDisabledThinkingRejection_1210直接认
// Python: openai_model_client.py:1357-1359
func TestIsDisabledThinkingRejection_1210直接认(t *testing.T) {
	if !isDisabledThinkingRejection(&fakeHTTPError{status: 400, msg: "error 1210"}) {
		t.Fatal("1210 应认作拒绝")
	}
}

// TestIsDisabledThinkingRejection_非重试状态码
// Python: openai_model_client.py:1351-1355；上游用例 :592-600
func TestIsDisabledThinkingRejection_非重试状态码(t *testing.T) {
	for _, status := range []int{401, 403, 408, 429, 500, 503} {
		err := &fakeHTTPError{status: status, msg: "thinking disabled not supported"}
		if isDisabledThinkingRejection(err) {
			t.Errorf("status %d 不应触发重试", status)
		}
	}
}

// ──────────────────────────── 判据：文本四层 ────────────────────────────

// TestIsDisabledThinkingRejection_文本四层
// Python: openai_model_client.py:1361-1370
func TestIsDisabledThinkingRejection_文本四层(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"thinking disabled not supported", true},
		{"reasoning cannot disable", true},
		{"思考 不支持 关闭", true},
		{"parameter thinking disable unsupported", true},
		{"unsupported thinking", false},          // 缺 disable 语义
		{"thinking disabled", false},             // 缺 unsupported
		{"model not found", false},               // 与 thinking 无关
		{"thinking unsupported parameter", true}, // parameter 满足（注意是完整词）
	}
	for _, c := range cases {
		got := isDisabledThinkingRejection(&fakeHTTPError{status: 400, msg: c.msg})
		if got != c.want {
			t.Errorf("msg=%q got %v want %v", c.msg, got, c.want)
		}
	}
}

// TestIsDisabledThinkingRejection_包装错误链 验证 Unwrap 链能被识别。
// Python: openai_model_client.py:1273-1280 _iter_exception_chain
func TestIsDisabledThinkingRejection_包装错误链(t *testing.T) {
	inner := &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
	wrapped := fmt.Errorf("call failed: %w", inner)
	if !isDisabledThinkingRejection(wrapped) {
		t.Fatal("包装后的错误应能被识别")
	}
}

// ──────────────────────────── 剥离字段 ────────────────────────────

// TestStripDisabledThinkingFields_完整剥离
// Python: openai_model_client.py:1204-1239
func TestStripDisabledThinkingFields_完整剥离(t *testing.T) {
	params := map[string]any{
		"thinking":             map[string]any{"type": "disabled", "budget_tokens": 1024},
		"enable_thinking":      false,
		"reasoning_effort":     "none",
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"reasoning":            map[string]any{"enabled": false},
		"model":                "m",
	}
	stripped := withoutDisabledThinkingParams(params)

	// thinking 只删 type，保留 budget_tokens
	thinking, _ := stripped["thinking"].(map[string]any)
	if thinking == nil || thinking["budget_tokens"] != 1024 {
		t.Errorf("thinking = %#v", stripped["thinking"])
	}
	if _, exists := thinking["type"]; exists {
		t.Error("thinking.type 未删")
	}
	for _, key := range []string{"enable_thinking", "reasoning_effort", "chat_template_kwargs", "reasoning"} {
		if _, exists := stripped[key]; exists {
			t.Errorf("%s 未删", key)
		}
	}
	if stripped["model"] != "m" {
		t.Error("无关字段被误删")
	}
}

// TestStripDisabledThinkingFields_extra_body内也剥离
// Python: openai_model_client.py:1246-1253
func TestStripDisabledThinkingFields_extra_body内也剥离(t *testing.T) {
	params := map[string]any{
		"extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}},
	}
	stripped := withoutDisabledThinkingParams(params)
	if _, exists := stripped["extra_body"]; exists {
		t.Fatalf("extra_body 剥空后应删除，got %#v", stripped["extra_body"])
	}
}

// TestStripDisabledThinkingFields_没有关闭字段不动 验证幂等。
func TestStripDisabledThinkingFields_没有关闭字段不动(t *testing.T) {
	params := map[string]any{
		"thinking":         map[string]any{"type": "enabled"},
		"reasoning_effort": "high",
	}
	stripped := withoutDisabledThinkingParams(params)
	thinking, _ := stripped["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Error("enabled 不应被剥")
	}
	if stripped["reasoning_effort"] != "high" {
		t.Error("high 不应被剥")
	}
}

// TestWithoutDisabledThinkingParams_不改入参 验证深拷贝。
func TestWithoutDisabledThinkingParams_不改入参(t *testing.T) {
	params := map[string]any{
		"thinking": map[string]any{"type": "disabled"},
	}
	_ = withoutDisabledThinkingParams(params)
	if _, exists := params["thinking"]; !exists {
		t.Fatal("入参被就地修改")
	}
}

// ──────────────────────────── 缓存 ────────────────────────────

// TestApplyDisabledThinkingCache_按模型名缓存
// Python: openai_model_client.py:1264-1270
func TestApplyDisabledThinkingCache_按模型名缓存(t *testing.T) {
	c := newTestClient()
	params := map[string]any{
		"model":      "m1",
		"extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}},
	}
	// 未缓存 → 原样
	if got := c.applyDisabledThinkingCache(params); got["extra_body"] == nil {
		t.Fatal("未缓存时不应剥离")
	}
	// 记入缓存 → 剥离
	c.rejectingDisabledThinking.add("m1")
	got := c.applyDisabledThinkingCache(params)
	if _, exists := got["extra_body"]; exists {
		t.Fatalf("已缓存应剥离，got %#v", got["extra_body"])
	}
}

// TestApplyDisabledThinkingCache_不同模型互不影响 验证按名隔离。
func TestApplyDisabledThinkingCache_不同模型互不影响(t *testing.T) {
	c := newTestClient()
	c.rejectingDisabledThinking.add("m1")
	params := map[string]any{
		"model":      "m2",
		"extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}},
	}
	if _, exists := c.applyDisabledThinkingCache(params)["extra_body"]; !exists {
		t.Fatal("m2 未缓存，不应被剥离")
	}
}

// ──────────────────────────── 三道闸门（核心）────────────────────────────

// TestFallback_默认不重试 安全设计：scope 外失败即失败。
// Python: openai_model_client.py:1390-1391；上游用例 :482-497
// test_rejected_disable_is_not_silently_retried_with_model_defaults
func TestFallback_默认不重试(t *testing.T) {
	c := newTestClient()
	calls := 0
	err := c.doWithDisabledThinkingFallback(context.Background(),
		map[string]any{"model": "m1", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(map[string]any) error {
			calls++
			return &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
		})
	if err == nil {
		t.Fatal("应上抛错误")
	}
	if calls != 1 {
		t.Fatalf("默认不重试，调用次数 = %d, want 1", calls)
	}
}

// TestFallback_scope内被拒时重试一次
// Python: openai_model_client.py:1393-1410；上游用例 :117
// test_symphony_retries_dashscope_disabled_thinking_bad_request
func TestFallback_scope内被拒时重试一次(t *testing.T) {
	c := newTestClient()
	calls := 0
	var retried map[string]any
	err := c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "m1", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(params map[string]any) error {
			calls++
			if calls == 1 {
				return &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
			}
			retried = params
			return nil
		})
	if err != nil {
		t.Fatalf("重试成功但返回错误: %v", err)
	}
	if calls != 2 {
		t.Fatalf("调用次数 = %d, want 2", calls)
	}
	if _, exists := retried["extra_body"]; exists {
		t.Errorf("重试参数应已剥离 extra_body，got %#v", retried["extra_body"])
	}
}

// TestFallback_无关错误不重试
// Python: 上游用例 :142 test_symphony_does_not_retry_unrelated_bad_request
func TestFallback_无关错误不重试(t *testing.T) {
	c := newTestClient()
	calls := 0
	err := c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "m1", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(map[string]any) error {
			calls++
			return &fakeHTTPError{status: 400, msg: "invalid request body"}
		})
	if err == nil {
		t.Fatal("应上抛")
	}
	if calls != 1 {
		t.Fatalf("无关错误不应重试，调用次数 = %d", calls)
	}
}

// TestFallback_非重试状态码不重试
// Python: 上游用例 :160 参数化 401/403/408/429/500/503
func TestFallback_非重试状态码不重试(t *testing.T) {
	for _, status := range []int{401, 403, 408, 429, 500, 503} {
		c := newTestClient()
		calls := 0
		_ = c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
			map[string]any{"model": "m1", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
			func(map[string]any) error {
				calls++
				return &fakeHTTPError{status: status, msg: "thinking disable unsupported"}
			})
		if calls != 1 {
			t.Errorf("status %d 不应重试，调用次数 = %d", status, calls)
		}
	}
}

// TestFallback_重试失败不再重试
// Python: 上游用例 :177 test_symphony_fallback_does_not_retry_a_failed_retry
func TestFallback_重试失败不再重试(t *testing.T) {
	c := newTestClient()
	calls := 0
	err := c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "m1", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(map[string]any) error {
			calls++
			return &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
		})
	if err == nil {
		t.Fatal("应上抛")
	}
	if calls != 2 {
		t.Fatalf("调用次数 = %d, want 2（首次 + 一次重试）", calls)
	}
}

// TestFallback_成功时只调一次
// Python: 上游用例 :462 test_supported_disabled_thinking_request_is_sent_once_unchanged
func TestFallback_成功时只调一次(t *testing.T) {
	c := newTestClient()
	calls := 0
	err := c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "m1", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(map[string]any) error {
			calls++
			return nil
		})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("成功时调用次数 = %d, want 1", calls)
	}
}

// TestFallback_重试后写入缓存
// Python: openai_model_client.py:1401-1403
func TestFallback_重试后写入缓存(t *testing.T) {
	c := newTestClient()
	calls := 0
	_ = c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "m-cache", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(map[string]any) error {
			calls++
			if calls == 1 {
				return &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
			}
			return nil
		})
	if !c.rejectingDisabledThinking.has("m-cache") {
		t.Fatal("重试成功后应记入缓存")
	}
}

// TestFallback_缓存在不同客户端间隔离
// Python: 上游用例 :58 test_symphony_retry_cache_stays_scoped_to_symphony_calls
func TestFallback_缓存在不同客户端间隔离(t *testing.T) {
	c1 := newTestClient()
	calls := 0
	_ = c1.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "shared-model", "extra_body": map[string]any{"thinking": map[string]any{"type": "disabled"}}},
		func(map[string]any) error {
			calls++
			if calls == 1 {
				return &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
			}
			return nil
		})
	c2 := newTestClient()
	if c2.rejectingDisabledThinking.has("shared-model") {
		t.Fatal("缓存不应跨客户端实例泄漏")
	}
}

// TestFallback_无关闭字段时被拒不重试 验证 retry 前置条件。
// Python: openai_model_client.py:1377-1378 if not self._has_disabled_thinking_params: return None
func TestFallback_无关闭字段时被拒不重试(t *testing.T) {
	c := newTestClient()
	calls := 0
	_ = c.doWithDisabledThinkingFallback(reasoning.WithDisabledThinkingFallback(context.Background()),
		map[string]any{"model": "m1"},
		func(map[string]any) error {
			calls++
			return &fakeHTTPError{status: 400, msg: "thinking disable unsupported"}
		})
	if calls != 1 {
		t.Fatalf("无关闭字段时不应重试，调用次数 = %d", calls)
	}
}

var _ = errors.New

// TestExceptionStatusCode_从BaseErrorParams读取 验证判据能读到 HandleHTTPError 写入的状态码。
//
// 上游从异常对象的 response.status_code 读；Go 侧 HTTP 状态放在
// BaseError.Params()["http_status"]（不改错误具体类型，避免破坏
// 全仓 10+ 处 err.(*exception.BaseError) 类型断言）。
func TestExceptionStatusCode_从BaseErrorParams读取(t *testing.T) {
	err := exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("OpenAI API HTTP 429: rate limited"),
		exception.WithParams(map[string]any{"http_status": 429}),
	)
	status, ok := exceptionStatusCode(err)
	if !ok || status != 429 {
		t.Fatalf("status = %d, ok = %v, want 429/true", status, ok)
	}
	// 429 属于非重试状态
	if isDisabledThinkingRejection(err) {
		t.Fatal("429 不应触发 disabled-thinking 重试")
	}
}

// TestExceptionStatusCode_包装后仍可读 验证 BaseError 被 WrapError 包一层后状态码不丢。
func TestExceptionStatusCode_包装后仍可读(t *testing.T) {
	inner := exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("OpenAI API HTTP 400: thinking disable unsupported"),
		exception.WithParams(map[string]any{"http_status": 400}),
	)
	wrapped := fmt.Errorf("invoke failed: %w", inner)
	if !isDisabledThinkingRejection(wrapped) {
		t.Fatal("包装后应仍能识别 400 + thinking disable unsupported")
	}
}
