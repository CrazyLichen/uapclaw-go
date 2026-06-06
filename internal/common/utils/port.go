// utils 包提供通用工具函数。
//
// port.go 实现端口等待和进程退出等待，使用指数退避策略。
// 对应 Python：jiuwenswarm/common/utils.py → wait_for_tcp_port() / wait_for_pid_exit()

package utils

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

// ──────────────────────────── 枚举 ────────────────────────────

// TargetState 端口目标状态。
type TargetState int

const (
	// StateConnected 等待端口可连接。
	StateConnected TargetState = iota
	// StateDisconnected 等待端口断开（拒绝连接）。
	StateDisconnected
)

// ──────────────────────────── 结构体 ────────────────────────────

// portConfig 端口等待配置，通过 Functional Options 模式设置。
type portConfig struct {
	initialDelay   time.Duration
	maxDelay       time.Duration
	connectTimeout time.Duration
	maxAttempts    int
	targetState    TargetState
}

// PortOption 端口等待选项。
type PortOption func(*portConfig)

// WithInitialDelay 设置初始退避延迟（默认 100ms）。
func WithInitialDelay(d time.Duration) PortOption {
	return func(c *portConfig) { c.initialDelay = d }
}

// WithMaxDelay 设置最大退避延迟（默认 2s）。
func WithMaxDelay(d time.Duration) PortOption {
	return func(c *portConfig) { c.maxDelay = d }
}

// WithConnectTimeout 设置每次连接尝试的超时（默认 1s）。
func WithConnectTimeout(d time.Duration) PortOption {
	return func(c *portConfig) { c.connectTimeout = d }
}

// WithMaxAttempts 设置最大尝试次数（默认 0=无限制）。
func WithMaxAttempts(n int) PortOption {
	return func(c *portConfig) { c.maxAttempts = n }
}

// WithTargetState 设置目标状态（默认 StateConnected）。
func WithTargetState(s TargetState) PortOption {
	return func(c *portConfig) { c.targetState = s }
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WaitForTCPPort 等待 TCP 端口达到目标状态，使用指数退避。
//
// 对应 Python: wait_for_tcp_port()
// 使用 context.Context 控制总超时，替代 Python 的 timeout 参数。
// 通过 Functional Options 模式配置退避参数。
//
// 返回 true 表示目标状态已达到，false 表示超时或达到最大尝试次数。
func WaitForTCPPort(ctx context.Context, host string, port int, opts ...PortOption) bool {
	cfg := defaultPortConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	delay := cfg.initialDelay
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		if cfg.maxAttempts > 0 && attempt >= cfg.maxAttempts {
			return false
		}
		attempt++

		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), cfg.connectTimeout)
		if err == nil {
			_ = conn.Close()
			if cfg.targetState == StateConnected {
				return true
			}
		} else {
			if cfg.targetState == StateDisconnected {
				return true
			}
		}

		// 等待退避时间，但尊重 context 取消
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}

		delay = min(delay*2, cfg.maxDelay)
	}
}

// WaitForPIDExit 等待进程退出。
//
// 对应 Python: wait_for_pid_exit()
// Linux 使用 syscall.Kill(pid, 0) 检查进程是否存在（信号 0 不发送信号，仅检查权限/存在性）。
// 如果进程不存在，syscall.ESRCH 错误会被捕获，函数返回 nil。
// 超时后返回错误。
func WaitForPIDExit(ctx context.Context, pid int) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("process %d did not exit within deadline: %w", pid, ctx.Err())
		case <-ticker.C:
			// syscall.Kill(pid, 0) 不发送信号，仅检查进程是否存在
			err := syscall.Kill(pid, 0)
			if err == syscall.ESRCH {
				// 进程不存在
				return nil
			}
			// 进程仍然存在，继续等待
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func defaultPortConfig() *portConfig {
	return &portConfig{
		initialDelay:   100 * time.Millisecond,
		maxDelay:       2 * time.Second,
		connectTimeout: 1 * time.Second,
		maxAttempts:    0, // 无限制
		targetState:    StateConnected,
	}
}
