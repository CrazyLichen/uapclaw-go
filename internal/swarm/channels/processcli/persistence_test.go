package processcli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// TestFlushSessionWrites_正常返回nil
// Python: persistence.py:10-25 两个屏障都成功时不报错
//
// 用例边界重置包级常驻的异步写入 worker，把自己造出来的会话与落盘任务
// 收拾干净，避免后续用例读到或写到前一个用例的临时目录。
func TestFlushSessionWrites_正常返回nil(t *testing.T) {
	session.ResetDrainedQueues()
	path.ResetCache()
	t.Cleanup(func() {
		session.ResetDrainedQueues()
		path.ResetCache()
	})
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
