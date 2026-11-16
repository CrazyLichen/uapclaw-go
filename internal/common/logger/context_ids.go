package logger

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/security"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 上下文键类型（非导出防碰撞）。Python 用 contextvars.ContextVar 等价。
type (
	// sessionIDKey 会话标识上下文键（Python _trace_id_context）
	sessionIDKey struct{}
	// memberIDKey 成员标识上下文键（Python _member_id_context）
	memberIDKey struct{}
)

// ──────────────────────────── 常量 ────────────────────────────

// DefaultTraceID 缺省 trace_id（Python _trace_id_context default）
const DefaultTraceID = "default_trace_id"

// ──────────────────────────── 导出函数 ────────────────────────────

// WithSessionID 在上下文中写入 trace_id（Python set_session_id）。
// Python ContextVar 在 async 链中隐式继承；Go 用 ctx value 显式传递（偏差 D-14 登记于 spec）。
func WithSessionID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, traceID)
}

// SessionIDFrom 读取 trace_id，缺省 default_trace_id（Python get_session_id）
func SessionIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(sessionIDKey{}).(string); ok && v != "" {
		return v
	}
	return DefaultTraceID
}

// WithMemberID 在上下文中写入 member_id（Python set_member_id）
func WithMemberID(ctx context.Context, memberID string) context.Context {
	return context.WithValue(ctx, memberIDKey{}, memberID)
}

// MemberIDFrom 读取 member_id，缺省空串（Python get_member_id）
func MemberIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(memberIDKey{}).(string); ok {
		return v
	}
	return ""
}

// NormalizeAndValidateLogPath 归一化日志路径（realpath → abspath）并做敏感路径校验。
// Python: normalize_and_validate_log_path（logging/utils.py:92）
func NormalizeAndValidateLogPath(pathValue string) (string, error) {
	// Python: os.fspath(path_value) 类型守卫——Go 入参恒为 string，类型分支不可达，仅保留空串守卫
	// Python: if not path_str or str(path_str).strip() == "": raise COMMON_LOG_PATH_INVALID
	if pathValue == "" || strings.TrimSpace(pathValue) == "" {
		return "", exception.BuildError(exception.StatusCommonLogPathInvalid,
			exception.WithMsg("the path_str is "+pathValue))
	}

	// Python: real_path = os.path.realpath(path_str)，OSError 时回退 abspath(expanduser)
	realPath := normalizeRealPath(pathValue)
	if realPath == "" {
		// Python except OSError 分支：os.path.abspath(os.path.expanduser(path_str))
		realPath = filepath.Clean(expandUser(pathValue))
		if !filepath.IsAbs(realPath) {
			if abs, err := filepath.Abs(realPath); err == nil {
				realPath = abs
			}
		}
	}

	// Python: if is_sensitive_path(real_path): raise COMMON_LOG_PATH_INVALID
	if security.IsSensitivePath(realPath) {
		return "", exception.BuildError(exception.StatusCommonLogPathInvalid,
			exception.WithMsg("the real_path is "+realPath))
	}

	return realPath, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// expandUser 展开 ~ 前缀（Python os.path.expanduser 等价）
func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// normalizeRealPath 解析符号链接（Python os.path.realpath 等价）；失败返回空串
func normalizeRealPath(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(real); err == nil {
		return abs
	}
	return real
}
