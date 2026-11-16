package logger

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// TestContextIds_会话与成员标识 对照 Python logging/utils.py 上下文函数用例面
func TestContextIds_会话与成员标识(t *testing.T) {
	// 缺省：session_id 返回 default_trace_id（Python _trace_id_context default）
	ctx := context.Background()
	if got := SessionIDFrom(ctx); got != "default_trace_id" {
		t.Fatalf("SessionIDFrom 默认值 = %q, want default_trace_id", got)
	}
	// 缺省：member_id 返回空串
	if got := MemberIDFrom(ctx); got != "" {
		t.Fatalf("MemberIDFrom 默认值 = %q, want 空串", got)
	}
	// 注入后取回
	ctx2 := WithSessionID(ctx, "trace-1")
	ctx2 = WithMemberID(ctx2, "agent-1")
	if got := SessionIDFrom(ctx2); got != "trace-1" {
		t.Fatalf("SessionIDFrom = %q, want trace-1", got)
	}
	if got := MemberIDFrom(ctx2); got != "agent-1" {
		t.Fatalf("MemberIDFrom = %q, want agent-1", got)
	}
	// 上下文隔离：父 ctx 不受子 ctx 注入影响
	if got := SessionIDFrom(ctx); got != "default_trace_id" {
		t.Fatalf("父 ctx 被污染 = %q", got)
	}
}

// TestNormalizeAndValidateLogPath 对照 Python normalize_and_validate_log_path 用例面
func TestNormalizeAndValidateLogPath(t *testing.T) {
	// 空串报错：the path_str is
	_, err := NormalizeAndValidateLogPath("")
	if err == nil {
		t.Fatal("空串应报错")
	}
	if !strings.Contains(err.Error(), "the path_str is") {
		t.Fatalf("空串错误文案未对齐: %v", err)
	}
	// 纯空白报错
	if _, err := NormalizeAndValidateLogPath("   "); err == nil {
		t.Fatal("纯空白应报错")
	}
	// 非敏感路径归一化返回绝对路径
	got, err := NormalizeAndValidateLogPath("/tmp/uapclaw_logs")
	if err != nil {
		t.Fatalf("正常路径报错: %v", err)
	}
	if !strings.HasPrefix(got, "/tmp") {
		t.Fatalf("应返回归一化绝对路径, got %q", got)
	}
	// 敏感路径报错：the real_path is
	_, err = NormalizeAndValidateLogPath("/etc/passwd")
	if err == nil {
		t.Fatal("敏感路径应报错")
	}
	if !strings.Contains(err.Error(), "the real_path is") {
		t.Fatalf("敏感路径错误文案未对齐: %v", err)
	}
	// 错误类型为 COMMON_LOG_PATH_INVALID
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("应返回 BaseError, got %T", err)
	}
}
