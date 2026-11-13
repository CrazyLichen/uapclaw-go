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
	// 先清掉上一个用例可能留在共享队列里的记录，再换一个空队列起跑：
	// 只 ResetHistoryWorker 会把残留记录一起丢弃，但 worker 已是新起的，
	// 本用例断言"队列空 → 立即确认"才成立
	ResetDrainedQueues()
	ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		ResetDrainedQueues()
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
	// 与 history 侧对称：先把两个队列排空再换新 worker，
	// 避免上一个用例残留的条目或超时哨兵顶掉本次确认
	ResetDrainedQueues()
	ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		ResetDrainedQueues()
		ClearAllSessionMetadataCache()
	})

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
	// 同上一例：用例独占 worker，且排空残留后再起，超时哨兵不跨用例残留
	ResetDrainedQueues()
	ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		ResetDrainedQueues()
		ClearAllSessionMetadataCache()
	})

	start := time.Now()
	_ = FlushMetadataQueue(0.001)
	require.Less(t, time.Since(start), 2*time.Second, "应在超时后立即返回")
}
