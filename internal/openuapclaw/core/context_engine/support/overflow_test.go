package support

import (
	"errors"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// TestIsContextOverflowError_九短语 覆盖各厂商原文。
func TestIsContextOverflowError_九短语(t *testing.T) {
	phrases := []string{
		"This model's maximum context length is 8192 tokens",
		"context window exceeded",
		"maximum context length exceeded",
		"context limit reached",
		"prompt is too long",
		"prompt too long: 200000 tokens",
		"input is too long for this model",
		"input too long",
		"Range of input length should be [1, 129024]", // 百炼
	}
	for _, phrase := range phrases {
		if !IsContextOverflowError(errors.New(phrase)) {
			t.Fatalf("%q 应识别为上下文溢出", phrase)
		}
	}
}

// TestIsContextOverflowError_大小写与因果链 验证大小写无关与包装链扫描。
func TestIsContextOverflowError_因果链(t *testing.T) {
	inner := errors.New("HTTP 400: Context Length Exceeded")
	wrapped := fmt.Errorf("llm call failed: %w", inner)
	if !IsContextOverflowError(wrapped) {
		t.Fatal("包装错误应被扫描到")
	}
	multi := errors.Join(errors.New("retry once"), errors.New("context window full"))
	if !IsContextOverflowError(multi) {
		t.Fatal("Join 错误应被扫描到")
	}
}

// TestIsContextOverflowError_BaseError详情 验证详情字段参与匹配。
func TestIsContextOverflowError_BaseError详情(t *testing.T) {
	err := exception.NewBaseError(exception.StatusContextExecutionError,
		exception.WithMsg("model call failed"),
		exception.WithParam("detail", "input too long for requested model"),
	)
	if !IsContextOverflowError(err) {
		t.Fatal("BaseError 参数里的溢出文案应被识别")
	}
}

// TestIsContextOverflowError_负例 验证非溢出错误不误判。
func TestIsContextOverflowError_负例(t *testing.T) {
	cases := []error{
		nil,
		errors.New("invalid api key"),
		errors.New("rate limit exceeded, retry later"),
		errors.New("connection reset by peer"),
	}
	for _, err := range cases {
		if IsContextOverflowError(err) {
			t.Fatalf("%v 不应识别为上下文溢出", err)
		}
	}
}
