package harnessrsi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestModelCall_常量与空输出 重试常量与空输出可重试语义
func TestModelCall_常量与空输出(t *testing.T) {
	if DefaultModelCallMaxRetries != 20 || DefaultModelCallRetryDelaySeconds != 1.0 || DefaultModelCallMaxRetryDelaySeconds != 10.0 {
		t.Fatal("constants must match Python defaults")
	}
	disableSleep(t)
	_, err := RunModelCallWithRetries(context.Background(), func() (string, error) {
		return "  ", nil
	}, "op", 2, 0, 0)
	if !errors.Is(err, ErrRetryableModelOutput) {
		t.Fatalf("empty output must be retryable, got %v", err)
	}
}

// TestModelCall_Mojibake修复路径 修复成功返回 repaired；最后一次仍 mojibake 也返回 repaired
func TestModelCall_Mojibake修复路径(t *testing.T) {
	disableSleep(t)
	mojibake := mustMojibake(t, "需要修复的中文样本数据内容较多一些才有效果显示")
	calls := 0
	got, err := RunModelCallWithRetries(context.Background(), func() (string, error) {
		calls++
		if calls == 1 {
			return mojibake, nil
		}
		return "clean text", nil
	}, "op", 3, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 第一次调用触发 mojibake 可重试错误，第二次干净文本返回
	if calls != 2 || got != "clean text" {
		t.Fatalf("calls=%d got=%q", calls, got)
	}
	// 最后一次仍 mojibake：attempt>=attempts 分支返回 repaired（Python L93-95）
	calls = 0
	final, err := RunModelCallWithRetries(context.Background(), func() (string, error) {
		calls++
		return mojibake, nil
	}, "op", 1, 0, 0)
	if err != nil {
		t.Fatalf("final-attempt repair must return without error: %v", err)
	}
	if RepairTextMojibake(final) != final {
		t.Fatalf("final output must be the repaired text: %q", final)
	}
}

// TestModelCall_非瞬态立即失败 non-retryable 错误不重试
func TestModelCall_非瞬态立即失败(t *testing.T) {
	disableSleep(t)
	calls := 0
	_, err := RunModelCallWithRetries(context.Background(), func() (string, error) {
		calls++
		return "", errors.New("invalid api key provided")
	}, "op", 5, 0, 0)
	if calls != 1 {
		t.Fatalf("non-retryable must fail immediately, calls=%d", calls)
	}
	if err == nil {
		t.Fatal("error must propagate")
	}
}

// TestModelCall_重试计数 20 次重试 = 21 次尝试
func TestModelCall_重试计数(t *testing.T) {
	disableSleep(t)
	calls := 0
	_, _ = RunModelCallWithRetries(context.Background(), func() (string, error) {
		calls++
		return "", errors.New("bad gateway")
	}, "op", DefaultModelCallMaxRetries, 0, 0)
	if calls != DefaultModelCallMaxRetries+1 {
		t.Fatalf("expected %d attempts, got %d", DefaultModelCallMaxRetries+1, calls)
	}
}

// TestModelCall_IsRetryable_标记矩阵 各标记类别命中与非命中
func TestModelCall_IsRetryable_标记矩阵(t *testing.T) {
	retryable := []string{
		"read timed out after 30s", "502 bad gateway", "too many requests",
		"error code 503 received", "<h1> 500 Internal Server Error",
		"database system is starting up",
	}
	for _, msg := range retryable {
		if !IsRetryableModelCallFailure(errors.New(msg)) {
			t.Fatalf("expected retryable: %q", msg)
		}
	}
	nonRetryable := []string{
		"invalid api key", "authentication failed", "permission denied",
		"budget_exceeded for tenant",
	}
	for _, msg := range nonRetryable {
		if IsRetryableModelCallFailure(errors.New(msg)) {
			t.Fatalf("expected non-retryable: %q", msg)
		}
	}
	if !IsRetryableModelCallFailure(context.DeadlineExceeded) {
		t.Fatal("DeadlineExceeded must be retryable")
	}
	// non-retryable 优先于 transient：含两类标记时判否
	if IsRetryableModelCallFailure(errors.New("invalid api key after gateway timeout")) {
		t.Fatal("non-retryable markers must win over transient markers")
	}
}

// TestModelCall_退避序列 指数退避封顶；RetryableModelOutputError 延迟 0
func TestModelCall_退避序列(t *testing.T) {
	if retryDelaySeconds(errors.New("bad gateway"), 1, 1.0, 10.0) != 1.0 {
		t.Fatal("first retry delay must be initial")
	}
	if retryDelaySeconds(errors.New("bad gateway"), 4, 1.0, 10.0) != 8.0 {
		t.Fatal("fourth retry delay must be initial*2^3")
	}
	if retryDelaySeconds(errors.New("bad gateway"), 6, 1.0, 10.0) != 10.0 {
		t.Fatal("delay must be capped at max")
	}
	if retryDelaySeconds(ErrRetryableModelOutput, 3, 1.0, 10.0) != 0 {
		t.Fatal("retryable output error must have zero delay")
	}
}

// TestModelCall_错误摘要 空白折叠与 300 截断
func TestModelCall_错误摘要(t *testing.T) {
	excerpt := errorExcerpt(errors.New("  a\n b\t c  "))
	if excerpt != "a b c" {
		t.Fatalf("whitespace must collapse: %q", excerpt)
	}
	long := strings.Repeat("x", 400)
	if got := errorExcerpt(errors.New(long)); len(got) != 303 || !strings.HasSuffix(got, "...") {
		t.Fatalf("excerpt must be capped at 300 + ellipsis: %d", len(got))
	}
}

// disableSleep 测试助手：延迟置零避免真实睡眠（全包只定义一次）
func disableSleep(t *testing.T) {
	t.Helper()
	original := sleepFn
	sleepFn = func(seconds float64) {}
	t.Cleanup(func() { sleepFn = original })
}
