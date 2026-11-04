package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试 ────────────────────────────

// TestStreamKeepalive_NotifyActivity重置计时 验证收到真实 chunk 后重置空闲计时。
func TestStreamKeepalive_NotifyActivity重置计时(t *testing.T) {
	beats := make(chan struct{}, 4)
	k := NewStreamKeepalive(func() { beats <- struct{}{} }, 40*time.Millisecond)
	k.Start()
	defer k.Stop()

	time.Sleep(20 * time.Millisecond)
	k.NotifyActivity(false) // 重置，不应在重置窗口内发心跳
	select {
	case <-beats:
		t.Fatal("NotifyActivity 后不应在重置窗口内发心跳")
	case <-time.After(25 * time.Millisecond):
	}
}

// TestStreamKeepalive_空闲超时发送 验证空闲到点后发送心跳。
func TestStreamKeepalive_空闲超时发送(t *testing.T) {
	beats := make(chan struct{}, 4)
	k := NewStreamKeepalive(func() { beats <- struct{}{} }, 10*time.Millisecond)
	k.Start()
	defer k.Stop()

	select {
	case <-beats:
	case <-time.After(time.Second):
		t.Fatal("空闲超时后应发送心跳")
	}
}

// TestStreamKeepalive_NotifyActivityTerminal停止 验证 terminal=true 停止后续心跳。
func TestStreamKeepalive_NotifyActivityTerminal停止(t *testing.T) {
	beats := make(chan struct{}, 4)
	k := NewStreamKeepalive(func() { beats <- struct{}{} }, 10*time.Millisecond)
	k.Start()
	k.NotifyActivity(true)

	select {
	case <-beats:
		t.Fatal("terminal 之后不应再发心跳")
	case <-time.After(50 * time.Millisecond):
	}
	k.Stop()
}

// TestStreamKeepalive_零间隔回落默认值 验证 interval<=0 使用 keepaliveInterval。
func TestStreamKeepalive_零间隔回落默认值(t *testing.T) {
	k := NewStreamKeepalive(func() {}, 0)
	require.Equal(t, keepaliveInterval, k.interval)
}
