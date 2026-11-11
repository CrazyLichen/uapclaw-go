package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestFlushHistoryQueue_队列空时返回true 验证签名改后返回值可用。
// Python: session_history.py:1384 flush_pending_writes(timeout) -> bool
//
// 注意：本测试刻意不写历史记录——写盘会给包级 workspace 缓存留下状态，
// 污染后续 TestAppendHistoryRecord_* 的 UAPCLAW_DATA_DIR 隔离。
// 这里只验证「worker 已启动 + 队列空 → 哨兵立即确认 → true」。
func TestFlushHistoryQueue_队列空时返回true(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	resetHistoryWorkspaceCache()
	ResetHistoryWorker()
	ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		ResetHistoryWorker()
		ClearAllSessionMetadataCache()
	})

	ok := FlushHistoryQueue(2.0)
	require.True(t, ok, "队列空时应立即返回 true")
}

// TestFlushMetadataQueue_队列空时返回true 验证 metadata 侧同样。
// Python: session_metadata.py:945 flush_pending_writes(timeout) -> bool
func TestFlushMetadataQueue_队列空时返回true(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	resetHistoryWorkspaceCache()
	ClearAllSessionMetadataCache()
	t.Cleanup(ClearAllSessionMetadataCache)

	ok := FlushMetadataQueue(2.0)
	require.True(t, ok, "队列空时应立即返回 true")
}

// TestFlushHistoryQueue_极小超时不panic 验证超时路径不崩。
func TestFlushHistoryQueue_极小超时不panic(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	resetHistoryWorkspaceCache()
	ResetHistoryWorker()
	ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		ResetHistoryWorker()
		ClearAllSessionMetadataCache()
	})

	// 队列空时哨兵立即被消费，极小超时也是 true；这里只验证不 panic
	_ = FlushHistoryQueue(0.000001)
}

// TestFlushMetadataQueue_超时有界 验证不无限等。
func TestFlushMetadataQueue_超时有界(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	resetHistoryWorkspaceCache()
	ClearAllSessionMetadataCache()
	t.Cleanup(ClearAllSessionMetadataCache)

	start := time.Now()
	_ = FlushMetadataQueue(0.001)
	require.Less(t, time.Since(start), 2*time.Second, "应在超时后立即返回")
}
