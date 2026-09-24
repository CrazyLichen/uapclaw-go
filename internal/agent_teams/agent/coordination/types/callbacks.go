package types

import (
	"context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// EventCallbackFunc 协调事件回调函数类型（同包兼容签名）。
type EventCallbackFunc func(ctx context.Context, event CoordinationEvent)

// CallbacksProvider handler 回调注册接口。
// 由 BaseCoordinationHandler 实现，供 EventDispatcher 遍历注册。
type CallbacksProvider interface {
	// GetCallbacks 返回 event_key → 回调方法，供 framework 注册。
	GetCallbacks() map[string]EventCallbackFunc
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
