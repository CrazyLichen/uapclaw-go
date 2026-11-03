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
// 语义边界：
//   - 这是"本次上下文是否被取消"的判定，不是"任意 error 是否由取消引起"。
//     不要传入 LLM 客户端包装后的 error 来调用本函数——那种场景请用
//     errors.Is(err, context.Canceled)（WrapError 已改为 WithCause 保留错误链，
//     见 openai/client.go）。
//   - ctx 为 nil 时返回 false（公共工具函数，不要求调用方保证非 nil）。
func Canceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
