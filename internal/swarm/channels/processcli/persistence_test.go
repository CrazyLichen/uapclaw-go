package processcli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestFlushSessionWrites_正常返回nil
// Python: persistence.py:10-25 两个屏障都成功时不报错
func TestFlushSessionWrites_正常返回nil(t *testing.T) {
	require.NoError(t, FlushSessionWrites(5.0))
}

// TestFlushSessionWrites_预算扣减
// Python: persistence.py:18 flush_timeout = timeout_seconds - min(0.5, timeout_seconds / 10)
func TestFlushSessionWrites_预算扣减(t *testing.T) {
	require.InDelta(t, 4.5, flushBudget(5.0), 0.001, "5.0 - min(0.5, 0.5) = 4.5")
	require.InDelta(t, 0.9, flushBudget(1.0), 0.001, "1.0 - min(0.5, 0.1) = 0.9")
	require.InDelta(t, 0.0, flushBudget(0.0), 0.001)
}

// TestFlushSessionWrites_有界返回
// 预算为负时不得长时间阻塞
func TestFlushSessionWrites_有界返回(t *testing.T) {
	start := time.Now()
	_ = FlushSessionWrites(0.1)
	require.Less(t, time.Since(start), 2*time.Second)
}

// TestFlushSessionWrites_负预算不阻塞
// Python: flush_timeout 可能为负（timeout=0 时），flush 应立即超时返回
func TestFlushSessionWrites_负预算不阻塞(t *testing.T) {
	start := time.Now()
	_ = FlushSessionWrites(0.0)
	require.Less(t, time.Since(start), 2*time.Second)
}
