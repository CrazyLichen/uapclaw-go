package utils

import (
	"context"
	"testing"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCanceled 防 D4 回归：只认 context.Canceled，DeadlineExceeded 不算取消。
func TestCanceled(t *testing.T) {
	if Canceled(context.Background()) {
		t.Fatal("未取消的 ctx 不应判定为取消")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if !Canceled(cancelCtx) {
		t.Fatal("已取消的 ctx 应判定为取消")
	}

	dlCtx, dlCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dlCancel()
	<-dlCtx.Done()
	if Canceled(dlCtx) {
		t.Fatal("超时不应被判定为取消（需按错误上报，对齐 Python TimeoutError 属 Exception）")
	}

	if Canceled(context.TODO()) {
		t.Fatal("TODO ctx 不应判定为取消")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
