package harnessrsi

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRaiseIfCancelled_ctx正常 返回 nil，不中断调用方。
func TestRaiseIfCancelled_ctx正常(t *testing.T) {
	ctx := context.Background()
	if err := RaiseIfCancelled(ctx); err != nil {
		t.Fatalf("正常 ctx 应返回 nil，得到 %v", err)
	}
}

// TestRaiseIfCancelled_已取消 返回取消哨兵错误。
func TestRaiseIfCancelled_已取消(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RaiseIfCancelled(ctx)
	if err == nil {
		t.Fatal("已取消 ctx 应返回错误")
	}
	if !errors.Is(err, ErrTaskCancelled) {
		t.Fatalf("应返回 ErrTaskCancelled 哨兵，得到 %v", err)
	}
}

// TestRaiseIfCancelled_已达deadline deadline 到期同样视为取消。
func TestRaiseIfCancelled_已达deadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	time.Sleep(20 * time.Millisecond)
	err := RaiseIfCancelled(ctx)
	if !errors.Is(err, ErrTaskCancelled) {
		t.Fatalf("超时 ctx 应视为取消，得到 %v", err)
	}
}
