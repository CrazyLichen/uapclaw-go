package harness_protocol

import "context"

// 本文件对应 Python openjiuwen/harness_protocol/stream.py（30 行）：
// 可提前释放消费者租约的单消费者观测游标契约。
// 本文件只定义接口与哨兵，**不含任何游标行为**：
// 有界缓冲与游标的并发状态机（单消费者租约 / peek 防竞态 / per-turn 结束条件 /
// Close 后 Next 返回哨兵）在 15.5 的 provider 基座落地，
// Python 依据 harness_providers/stream.py:19-155。

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrEventStreamDone 观测游标到达末尾的哨兵，等价 Python 的 StopAsyncIteration。
// 用法：errors.Is(err, ErrEventStreamDone) 判定正常结束，与真实错误区分。
var ErrEventStreamDone = errEventStreamDone{}

// ──────────────────────────── 结构体（接口定义）────────────────────────────

// HarnessEventCursor 可提前释放消费者租约的有序观测游标。
// Python: stream.py:13-27 HarnessEventCursor（3 方法）
type HarnessEventCursor interface {
	// Iter 返回本游标（对齐 __aiter__）
	Iter() HarnessEventCursor
	// Next 返回下一条有序事件；耗尽时返回 ErrEventStreamDone（对齐 __anext__）
	Next(ctx context.Context) (*HarnessEvent, error)
	// Close 幂等释放观测消费者租约（对齐 aclose）
	Close(ctx context.Context) error
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errEventStreamDone 观测游标耗尽的哨兵类型。
type errEventStreamDone struct{}

// Error 实现 error 接口。
func (errEventStreamDone) Error() string { return "harness event stream exhausted" }
