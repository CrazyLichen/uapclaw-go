package server

import (
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamKeepalive 流式心跳：空闲超时发 keepalive，收到真实 chunk 或 terminal 后重置/停止。
// Python: jiuwenswarm/server/agent_ws_server.py:699 StreamKeepalive
type StreamKeepalive struct {
	// send 发送一次 keepalive
	send func()
	// interval 空闲间隔
	interval time.Duration
	// trigger 有真实 chunk 时重置计时
	trigger chan struct{}
	// stopCh 停止信号
	stopCh chan struct{}
	// done goroutine 退出信号
	done chan struct{}
	// once 保证 stopCh 只关一次
	once sync.Once
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamKeepalive 创建心跳（interval<=0 时用 keepaliveInterval）。
func NewStreamKeepalive(send func(), interval time.Duration) *StreamKeepalive {
	if interval <= 0 {
		interval = keepaliveInterval
	}
	return &StreamKeepalive{
		send:     send,
		interval: interval,
		trigger:  make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start 启动心跳 goroutine。
func (k *StreamKeepalive) Start() {
	go func() {
		defer close(k.done)
		timer := time.NewTimer(k.interval)
		defer timer.Stop()
		for {
			select {
			case <-k.stopCh:
				return
			case <-k.trigger:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(k.interval)
			case <-timer.C:
				k.send()
				timer.Reset(k.interval)
			}
		}
	}()
}

// NotifyActivity 收到真实 chunk（terminal=true 时停止后续心跳）。
func (k *StreamKeepalive) NotifyActivity(terminal bool) {
	if terminal {
		k.SignalStop()
		return
	}
	select {
	case k.trigger <- struct{}{}:
	default:
	}
}

// SignalStop 尽早停止新心跳（Runtime 清理仍先完成）。
func (k *StreamKeepalive) SignalStop() { k.once.Do(func() { close(k.stopCh) }) }

// Stop 停止并等待 goroutine 退出。
func (k *StreamKeepalive) Stop() {
	k.SignalStop()
	<-k.done
}
