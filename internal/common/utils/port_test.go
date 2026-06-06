package utils

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestWaitForTCPPort_Connected(t *testing.T) {
	// 启动一个临时 TCP 服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	port := ln.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := WaitForTCPPort(ctx, "127.0.0.1", port)
	if !result {
		t.Fatal("WaitForTCPPort should return true when port is connected")
	}
}

func TestWaitForTCPPort_Disconnected(t *testing.T) {
	// 找一个未使用的端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // 立即关闭，端口应该断开

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := WaitForTCPPort(ctx, "127.0.0.1", port,
		WithTargetState(StateDisconnected),
	)
	if !result {
		t.Fatal("WaitForTCPPort should return true when port is disconnected")
	}
}

func TestWaitForTCPPort_ContextCancelled(t *testing.T) {
	// 找一个未使用的端口，不监听
	ctx, cancel := context.WithCancel(context.Background())
	// 立即取消 context
	cancel()

	result := WaitForTCPPort(ctx, "127.0.0.1", 1) // 端口 1 通常不监听
	if result {
		t.Fatal("WaitForTCPPort should return false when context is cancelled")
	}
}

func TestWaitForTCPPort_MaxAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 端口 1 通常不监听，设置 maxAttempts=3
	result := WaitForTCPPort(ctx, "127.0.0.1", 1,
		WithMaxAttempts(3),
		WithInitialDelay(10*time.Millisecond),
	)
	if result {
		t.Fatal("WaitForTCPPort should return false when max attempts reached")
	}
}

func TestWaitForTCPPort_ExponentialBackoff(t *testing.T) {
	// 测试指数退避行为：验证延迟递增
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 端口不监听，会持续重试直到超时
	_ = WaitForTCPPort(ctx, "127.0.0.1", 1,
		WithInitialDelay(50*time.Millisecond),
		WithMaxDelay(200*time.Millisecond),
	)

	elapsed := time.Since(start)
	// 应该在 context 超时后返回（约 2s），不应立即返回
	if elapsed < 500*time.Millisecond {
		t.Fatalf("WaitForTCPPort returned too quickly (%v), expected backoff delays", elapsed)
	}
}

func TestWaitForTCPPort_WaitForServerStartup(t *testing.T) {
	// 模拟服务延迟启动：先开始等待，然后启动服务器
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	// 在另一个 goroutine 中延迟启动服务器
	go func() {
		time.Sleep(200 * time.Millisecond)
		serverLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return
		}
		defer func() { _ = serverLn.Close() }()
		// 保持服务器运行一段时间
		time.Sleep(5 * time.Second)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := WaitForTCPPort(ctx, "127.0.0.1", port,
		WithInitialDelay(50*time.Millisecond),
	)
	if !result {
		t.Fatal("WaitForTCPPort should detect server startup")
	}
}

func TestWaitForPIDExit_NonexistentProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 使用一个不太可能存在的 PID（99999999）
	err := WaitForPIDExit(ctx, 99999999)
	if err != nil {
		t.Fatalf("WaitForPIDExit should return nil for non-existent process, got: %v", err)
	}
}

func TestWaitForPIDExit_ContextCancelled(t *testing.T) {
	// PID 1 通常存在（init），等待其退出会超时
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := WaitForPIDExit(ctx, 1)
	if err == nil {
		t.Fatal("WaitForPIDExit should return error when context cancelled for running process")
	}
}
