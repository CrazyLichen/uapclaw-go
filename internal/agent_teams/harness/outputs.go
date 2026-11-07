package harness

import (
	"context"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OutputIterator 输出块迭代器。
// Python: _OutputIterator (agent_teams/harness/outputs.py)，44 行。
// 契约：单消费者——END 哨兵只会被第一个迭代器读到，之后所有读取返回 false。
// Go 用"consumed 原子标志"表达 asyncio 的 StopAsyncIteration 终止语义。
type OutputIterator struct {
	// ch 输出块通道（由 NativeHarness 持有写端）
	ch <-chan stream.Schema
	// consumed 是否已完成（读到 END 或上下文取消）
	consumed atomic.Bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// endSentinel 输出终止哨兵。
// Python: _END = object()（用对象身份判定，outputs.py:15）
// Go 用指针身份判定：forwarder 投递的是同一个指针实例，因此
// `chunk == endSentinel` 是接口值比较（同指针即相等）。
var endSentinel = &stream.OutputSchema{Type: "__harness_end__"}

// ──────────────────────────── 导出函数 ────────────────────────────

// Next 取下一个输出块；返回 false 表示流已终止。
func (it *OutputIterator) Next(ctx context.Context) (stream.Schema, bool) {
	if it.consumed.Load() {
		return nil, false
	}
	select {
	case <-ctx.Done():
		it.consumed.Store(true)
		return nil, false
	case chunk, ok := <-it.ch:
		if !ok {
			it.consumed.Store(true)
			return nil, false
		}
		if chunk == endSentinel {
			it.consumed.Store(true)
			return nil, false
		}
		return chunk, true
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newOutputIterator 创建输出迭代器。
func newOutputIterator(ch <-chan stream.Schema) *OutputIterator {
	return &OutputIterator{ch: ch}
}
