package utils

import (
	"testing"
	"time"
)

// TestDrainWithTimeout_已关闭通道 通道关闭即视为排空完成。
func TestDrainWithTimeout_已关闭通道(t *testing.T) {
	ch := make(chan int, 2)
	ch <- 1
	ch <- 2
	close(ch)

	drained, completed := DrainWithTimeout(ch, time.Second)
	if !completed || drained != 2 {
		t.Fatalf("期望 (2,true)，实际 (%d,%v)", drained, completed)
	}
}

// TestDrainWithTimeout_通道不关闭时超时返回 生产方不退出循环时不得永久阻塞。
func TestDrainWithTimeout_通道不关闭时超时返回(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 1 // 不关闭

	start := time.Now()
	drained, completed := DrainWithTimeout(ch, 50*time.Millisecond)
	elapsed := time.Since(start)

	if completed {
		t.Fatal("通道未关闭不应报告排空完成")
	}
	if drained != 1 {
		t.Fatalf("应读到 1 个元素，实际 %d", drained)
	}
	if elapsed > time.Second {
		t.Fatalf("应在 50ms 左右返回，实际耗时 %s", elapsed)
	}
}
