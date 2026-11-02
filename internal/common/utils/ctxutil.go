package utils

import (
	"context"
	"errors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Canceled 判定 ctx 是否被外部取消。
//
// 只认 context.Canceled：DeadlineExceeded 仍须按错误上报
// （Python 的 TimeoutError 是 Exception 会被上报，CancelledError 不会）。
//
// 注意：不要用 errors.Is(err, context.Canceled) 替代本判定——LLM 客户端的
// WrapError 历史上用 %v 拼串，错误链可能已断（见 openai/client.go）。
// 调用方必须传非 nil ctx（Go 惯例）。
func Canceled(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.Canceled)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
