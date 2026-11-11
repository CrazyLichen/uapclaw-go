package processcli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// TestCommandSignals_finalize改写为CANCELLED
// Python: machine_signals.py:64-76 只在 error is None 时改写
func TestCommandSignals_finalize改写为CANCELLED(t *testing.T) {
	s := NewCommandSignals()
	s.Interrupted = true
	s.Cleaning = true

	// 已有 error 时保留原样（Python: :67 and result.error is None）
	orig := &protocol.OneShotRunResult{
		Sequence: 0, RequestID: "r1",
		Status: "failed", ExitCode: 1,
		Error: &protocol.RuntimeErrorInfo{Code: "X", Message: "m"},
	}
	got := s.Finalize(orig)
	require.Equal(t, "failed", got.Status)
	require.Equal(t, 1, got.ExitCode)
	require.Same(t, orig, got, "已有 error 应原样返回同一对象")

	// 无 error 时改写为 cancelled（Python: :68-75 replace）
	s2 := NewCommandSignals()
	s2.Interrupted = true
	s2.Cleaning = true
	got2 := s2.Finalize(&protocol.OneShotRunResult{
		Sequence: 0, RequestID: "r1", Status: "completed", ExitCode: 0,
	})
	require.Equal(t, "cancelled", got2.Status)
	require.Equal(t, 130, got2.ExitCode)
	require.NotNil(t, got2.Error)
	require.Equal(t, "CANCELLED", got2.Error.Code)
	require.Equal(t, "Command interrupted.", got2.Error.Message)
}

// TestCommandSignals_finalize不改原对象 验证 replace 语义（生成新对象）。
// Python: dataclasses.replace 返回新实例
func TestCommandSignals_finalize不改原对象(t *testing.T) {
	s := NewCommandSignals()
	s.Interrupted = true
	orig := &protocol.OneShotRunResult{
		Sequence: 0, RequestID: "r1", Status: "completed", ExitCode: 0,
	}
	got := s.Finalize(orig)
	require.NotSame(t, orig, got, "必须是新对象")
	require.Equal(t, "completed", orig.Status, "原对象不得被就地改写")
}

// TestCommandSignals_interrupt只生效一次
// Python: machine_signals.py:44-45 if self._settled or self.interrupted: return
func TestCommandSignals_interrupt只生效一次(t *testing.T) {
	s := NewCommandSignals()
	cancelCalls := 0
	unbind := s.BindCancel(func() { cancelCalls++ })
	defer unbind()

	s.Interrupt(2)
	require.True(t, s.Interrupted)
	require.True(t, s.Cancelled())
	require.Equal(t, 1, cancelCalls)

	s.Interrupt(2) // 第二次应直接返回
	require.Equal(t, 1, cancelCalls, "第二次中断不得再次 cancel")
}

// TestCommandSignals_cleaning期不取消
// Python: machine_signals.py:47-48 if self.cleaning: return
func TestCommandSignals_cleaning期不取消(t *testing.T) {
	s := NewCommandSignals()
	cleaned := 0
	unbind := s.BindCancel(func() { cleaned++ })
	defer unbind()

	// BindCancel 的 unbind 会置 cleaning；这里显式进入清理期
	s.mu.Lock()
	s.Cleaning = true
	s.mu.Unlock()

	s.Interrupt(2)
	require.True(t, s.Interrupted, "仍应置 interrupted")
	require.False(t, s.Cancelled(), "cleaning 期不该 cancel")
	require.Equal(t, 0, cleaned)
}

// TestCommandSignals_settled后不再响应
// Python: machine_signals.py:44 if self._settled or ...
func TestCommandSignals_settled后不再响应(t *testing.T) {
	s := NewCommandSignals()
	calls := 0
	unbind := s.BindCancel(func() { calls++ })
	defer unbind()

	// finalize 置 settled
	_ = s.Finalize(&protocol.OneShotRunResult{
		Sequence: 0, RequestID: "r1", Status: "completed", ExitCode: 0,
	})
	s.Interrupt(2)
	require.False(t, s.Interrupted, "settled 后不得再置 interrupted")
	require.Equal(t, 0, calls)
}

// TestDeferCommandSignals_置cleaning
// Python: machine_signals.py:101-105
func TestDeferCommandSignals_置cleaning(t *testing.T) {
	s := NewCommandSignals()
	DeferCommandSignals(s)
	require.True(t, s.Cleaning)
}

// TestBindCancel_解除后置cleaning并清空
// Python: machine_signals.py:60-62 finally: self.cleaning=True; self._task=previous
func TestBindCancel_解除后置cleaning并清空(t *testing.T) {
	s := NewCommandSignals()
	_, cancel := context.WithCancel(context.Background())
	unbind := s.BindCancel(cancel)

	s.mu.Lock()
	require.NotNil(t, s.cancel)
	require.False(t, s.Cleaning)
	s.mu.Unlock()

	unbind()

	s.mu.Lock()
	require.Nil(t, s.cancel, "解除后应清空绑定")
	require.True(t, s.Cleaning, "解除后应进入清理期")
	s.mu.Unlock()
}

// TestCommandSignalsContext_复用已有owner
// Python: machine_signals.py:82-85 existing is not None → yield existing
func TestCommandSignalsContext_复用已有owner(t *testing.T) {
	first, restoreFirst := CommandSignalsContext()
	require.NotNil(t, first)
	defer restoreFirst()

	second, restoreSecond := CommandSignalsContext()
	require.Same(t, first, second, "已有 owner 时应复用")
	restoreSecond() // 空操作
}
