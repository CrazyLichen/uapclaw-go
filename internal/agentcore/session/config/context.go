package config

import (
	"context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// envsContextKeyType 请求级环境变量的 context key 类型
type envsContextKeyType struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// envsContextKey 请求级环境变量的 context key
var envsContextKey envsContextKeyType

// ──────────────────────────── 导出函数 ────────────────────────────

// WithEnvs 将请求级环境变量注入到 context 中。
// 优先级：os.Getenv > context.Value > 内置默认值。
// 对应 Python: workflow_session_vars (contextvars.ContextVar)
func WithEnvs(ctx context.Context, envs map[string]any) context.Context {
	return context.WithValue(ctx, envsContextKey, envs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getEnvsFromContext 从 context 中获取请求级环境变量
func getEnvsFromContext(ctx context.Context) map[string]any {
	if envs, ok := ctx.Value(envsContextKey).(map[string]any); ok {
		return envs
	}
	return nil
}
