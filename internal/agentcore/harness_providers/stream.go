package harness_providers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// 本文件对应 Python openjiuwen/harness_providers/stream.py（168 行）：
// provider 共享的、有界单消费者观测流。
//
// 15.2 只落了协议侧的 HarnessEventCursor 接口（harness_protocol/stream.go），
// 并发状态机在本文件实现（15.2 spec B7 的 ⏵ 承接项）。

// ──────────────────────────── 结构体 ────────────────────────────

// BoundedEventBuffer 可关闭的 FIFO，必需事件用阻塞背压。
// Python: stream.py:25-84 BoundedEventBuffer
type BoundedEventBuffer struct {
	// mu 保护全部字段
	mu sync.Mutex
	// notify 等待者的唤醒信号（容量 1 的信箱）。
	//
	// 为什么不用 sync.Cond（2026-11-27 实施修正，替代原 Q4 裁决）：
	// cond.Broadcast 的唤醒在「Broadcast 先于 Wait 注册」的交错下依赖
	// 条件重查才能存活——但实测在 Stop 链（doStop 的 buffer.Close 广播后
	// 泵仍滞留在 cond.Wait，goroutine 永久悬挂）上偶发失效。
	// channel 信号无此竞态：signal 用非阻塞投递，信箱容量 1，
	// 「检查条件→解锁→收信号→重上锁→重查」环内任何时刻到达的信号都会
	// 被缓存，等待方必然醒来重查条件。
	notify chan struct{}
	// items 事件队列。用切片 + 头部前移（items = items[1:]）而非环形缓冲：
	// 容量上限固定（上游默认 1024），前移只移动 slice header，摊销 O(1)，
	// 且 Peek 就是 items[0]，最直白。
	items []harnessprotocol.HarnessEvent
	// capacity 队列容量（上游默认 1024）
	capacity int
	// closed 生产者侧是否已关闭
	closed bool
	// consumerClosed 消费者游标是否已主动关闭。
	//
	// 偏差 D3 的实现载体：上游 aclose 只置游标自己的标志并释放租约，不唤醒
	// 阻塞在 get/peek 的协程（它们在 asyncio 里靠 task 取消兜底）。Go 里
	// 必须让等待方感知到「这个游标已经不要数据了」，否则 goroutine 永久悬挂。
	// 置位后 Get/Peek 的等待循环退出并按「耗尽」返回。
	consumerClosed bool
	// consumerActive 单消费者租约是否已被占用
	consumerActive bool
}

// BufferedEventCursor 一个 BoundedEventBuffer 上的周期级或有限轮次视图。
// Python: stream.py:87-165 BufferedEventCursor
type BufferedEventCursor struct {
	// buffer 底层缓冲
	buffer *BoundedEventBuffer
	// expectedTurnID 期望的轮次 ID（空串表示「下一轮未消费的轮次」，对应上游 None）
	expectedTurnID string
	// perTurn 是否单轮视图（true）还是周期级（false）
	perTurn bool
	// selectedTurnID 已选定的轮次 ID（perTurn 专用；空串表示尚未选定）
	selectedTurnID string
	// hasSelected 是否已选定轮次。
	//
	// 上游用 `selected_turn_id is None` 区分「未选定」；Go 的 HarnessEvent.TurnID
	// 是 string（空串表示不属于某轮），若用空串表达「未选定」，就无法区分
	// 「已选定一个 TurnID 为空串的轮次」。故另用一个 bool 承载该状态。
	hasSelected bool
	// closed 游标是否已关闭（由 buffer.mu 保护）
	closed bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrEventBufferClosed 缓冲已关闭时 Put 的返回值。
// Python: stream.py:21-22 EventBufferClosed（RuntimeError）
//
// Python 侧全仓零捕获点（grep 只命中 stream.py 自身），实际靠
// _run_provider_turn 的 except Exception 兜成 FAILED 轮次结果。
// Go 侧同样由 RunProviderTurn 兜底。
var ErrEventBufferClosed = errors.New("harness event buffer is closed")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBoundedEventBuffer 创建一个容量为 capacity 的有界缓冲。
// Python: stream.py:28-33 BoundedEventBuffer.__init__
func NewBoundedEventBuffer(capacity int) *BoundedEventBuffer {
	buffer := &BoundedEventBuffer{
		capacity: capacity,
		items:    make([]harnessprotocol.HarnessEvent, 0, capacity),
		notify:   make(chan struct{}, 1),
	}
	return buffer
}

// Put 追加一条事件；缓冲满时阻塞。
// Python: stream.py:35-44 put
//
// ctx 保留位：sync.Cond.Wait() 不响应取消，关闭靠 Close 的 Broadcast 唤醒。
// 与上游一致（asyncio 侧同样靠 close() 而非 task cancel 解锁）。
func (b *BoundedEventBuffer) Put(ctx context.Context, event harnessprotocol.HarnessEvent) error {
	_ = ctx // 保留位：等待不响应取消，与上游一致（close 解锁）
	b.mu.Lock()
	defer b.mu.Unlock()
	b.awaitFor(func() bool {
		return len(b.items) < b.capacity || b.closed
	})
	if b.closed {
		return ErrEventBufferClosed
	}
	b.items = append(b.items, event)
	b.signal()
	return nil
}

// Get 取出下一条事件；关闭且已排空时返回 ErrEventStreamDone。
// Python: stream.py:46-56 get（返回 HarnessEvent | None）
func (b *BoundedEventBuffer) Get(ctx context.Context) (*harnessprotocol.HarnessEvent, error) {
	_ = ctx // 保留位
	b.mu.Lock()
	defer b.mu.Unlock()
	b.awaitFor(func() bool {
		return len(b.items) > 0 || b.closed || b.consumerClosed
	})
	if len(b.items) == 0 {
		return nil, harnessprotocol.ErrEventStreamDone
	}
	event := b.items[0]
	// 清零已消费格子：items = items[1:] 只移动 header，底层数组第 0 格仍持有
	// 该事件的引用，不清零会让其 payload 一直不可回收（Go 切片前移的标准陷阱）。
	// 标准库 container/ring、net/http 的 buffer 同款做法。
	b.items[0] = harnessprotocol.HarnessEvent{}
	b.items = b.items[1:]
	b.signal()
	return &event, nil
}

// Peek 查看下一条事件但不释放生产者容量。
// Python: stream.py:58-64 peek
//
// per-turn 游标靠它避免 pop-then-push 竞态：先 Get 再放回时，生产者可能立刻
// 填满刚释放的槽位，导致 STARTED 事件丢失。
func (b *BoundedEventBuffer) Peek(ctx context.Context) (*harnessprotocol.HarnessEvent, error) {
	_ = ctx // 保留位
	b.mu.Lock()
	defer b.mu.Unlock()
	b.awaitFor(func() bool {
		return len(b.items) > 0 || b.closed || b.consumerClosed
	})
	if len(b.items) == 0 {
		return nil, harnessprotocol.ErrEventStreamDone
	}
	return &b.items[0], nil
}

// Close 关闭生产者侧并唤醒全部阻塞的读写者。幂等。
// Python: stream.py:66-71 close
func (b *BoundedEventBuffer) Close(ctx context.Context) error {
	_ = ctx // Close 本身不阻塞，ctx 仅为与其他方法签名统一
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.signal()
	return nil
}

// signal 非阻塞投递一枚唤醒信号（信箱容量 1，已有信号则合并）。
func (b *BoundedEventBuffer) signal() {
	select {
	case b.notify <- struct{}{}:
	default:
	}
}

// awaitFor 在 b.mu 已持锁的前提下等待 predicate 成立。
//
// 环路：检查条件 → 解锁 → 等信号 → 重上锁 → 重查。
// 任何在「解锁」之后到达的 signal 都会被信箱缓存，`<-ch` 立即返回，
// 因此不存在丢唤醒。
func (b *BoundedEventBuffer) awaitFor(predicate func() bool) {
	for !predicate() {
		notify := b.notify
		b.mu.Unlock()
		<-notify
		b.mu.Lock()
	}
}

// Cursor 获取单消费者租约并返回游标。
// Python: stream.py:73-79 cursor
//
// turnID 为空串表示「下一轮未消费的轮次」或「周期级」（配合 perTurn 解释）。
func (b *BoundedEventBuffer) Cursor(turnID string, perTurn bool) (harnessprotocol.HarnessEventCursor, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.consumerActive {
		return nil, &harnessprotocol.HarnessStateError{
			Msg: "the harness observation stream already has an active consumer",
		}
	}
	b.consumerActive = true
	// 新游标接管：清掉上一轮 aclose 留下的消费者关闭标记，
	// 否则新游标的 Get/Peek 会立刻按「耗尽」返回。
	b.consumerClosed = false
	return &BufferedEventCursor{
		buffer:         b,
		expectedTurnID: turnID,
		perTurn:        perTurn,
	}, nil
}

// Iter 返回本游标（对齐 __aiter__）。
// Python: stream.py:103-104 __aiter__
func (c *BufferedEventCursor) Iter() harnessprotocol.HarnessEventCursor { return c }

// Close 幂等释放观测消费者租约。
// Python: stream.py:161-165 aclose
//
// **与上游的偏差（spec D3）**：此处额外 Broadcast 一次，让阻塞在 Next 里的
// 协程立刻醒来并返回 ErrEventStreamDone。上游 aclose 只置标志与释放租约、
// 不唤醒，阻塞者必须等 buffer.close()；这在 asyncio 里有 task 取消兜底，
// 在 Go 里是真实 goroutine 泄漏（-race 的泄漏检测会抓到），故此处修正。
func (c *BufferedEventCursor) Close(ctx context.Context) error {
	_ = ctx
	buffer := c.buffer
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	buffer.consumerActive = false
	// 偏差 D3：让阻塞在 Get/Peek 的等待方感知「此游标已不要数据」。
	// 只置游标标志 + Broadcast 不够——Peek/Get 的等待条件看的是 buffer 侧
	// 状态，醒来后会重新判定队列仍空而继续 Wait（实测悬挂）。
	buffer.consumerClosed = true
	buffer.signal()
	return nil
}

// Next 返回下一条有序事件；耗尽时返回 ErrEventStreamDone。
// Python: stream.py:106-159 __anext__
//
// 周期级游标（perTurn=false）直通，逐条返回。
//
// 单轮游标（perTurn=true）的 5 步：
//  1. 未选定轮次时用 Peek 验证队首，避免 pop-then-push 与生产者的竞态
//  2. 非 STARTED 的事件真正 Get 丢弃，继续找
//  3. 期望轮次与队首不符 → 关闭并报 HarnessStateError
//  4. 兑现 Peek：Get 真正消费掉 STARTED（流此时变化则报 HarnessProtocolError）
//  5. 事件属于已选定轮次且是终态 → 自动关闭（释放租约）
func (c *BufferedEventCursor) Next(ctx context.Context) (*harnessprotocol.HarnessEvent, error) {
	if c.isClosed() {
		return nil, harnessprotocol.ErrEventStreamDone
	}

	for {
		var event *harnessprotocol.HarnessEvent
		var err error
		if c.perTurn && !c.hasSelected {
			event, err = c.buffer.Peek(ctx)
		} else {
			event, err = c.buffer.Get(ctx)
		}
		if errors.Is(err, harnessprotocol.ErrEventStreamDone) {
			return nil, c.finishOnDrain()
		}
		if err != nil {
			return nil, err
		}
		if event == nil {
			return nil, c.finishOnDrain()
		}

		if !c.perTurn {
			return event, nil
		}

		// 步骤 1-4：未选定轮次
		if !c.hasSelected {
			startedKind, isStarted := turnLifecycleKind(event.Event)
			if !isStarted || startedKind != harnessprotocol.TurnEventKindStarted {
				// 非 STARTED：真正消费掉（Peek 时未删除）并继续
				// Python: stream.py:135 `await self._buffer.get()`
				if _, dropErr := c.buffer.Get(ctx); dropErr != nil {
					_ = c.Close(ctx)
					return nil, dropErr
				}
				continue
			}
			// 步骤 3：期望轮次比对
			if c.expectedTurnID != "" && event.TurnID != c.expectedTurnID {
				_ = c.Close(ctx)
				return nil, &harnessprotocol.HarnessStateError{
					Msg: fmt.Sprintf(
						"requested turn is not the next unconsumed turn: expected %q, found %q",
						c.expectedTurnID, event.TurnID),
				}
			}
			// 步骤 4：兑现 Peek
			// Python: stream.py:138 `consumed = await self._buffer.get()`
			//
			// 此处不会阻塞：从上面 Peek 返回到这里之间没有任何 await / 协程切换，
			// 而队列只有本游标能删（单消费者租约）、生产者只做追加，
			// 故刚才 Peek 到的那条必然还在队首。
			consumed, getErr := c.buffer.Get(ctx)
			if getErr != nil || consumed == nil {
				_ = c.Close(ctx)
				return nil, &harnessprotocol.HarnessProtocolError{
					Msg: "harness event stream changed while selecting a turn",
				}
			}
			event = consumed
			c.selectedTurnID = event.TurnID
			c.hasSelected = true
		}

		// 步骤 5：终态自动关闭
		if event.TurnID == c.selectedTurnID {
			if kind, isLifecycle := turnLifecycleKind(event.Event); isLifecycle &&
				harnessprotocol.IsTerminalTurnEventKind(kind) {
				_ = c.Close(ctx)
			}
		}
		return event, nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// turnLifecycleKind 识别轮次生命周期载荷并返回其 kind。
//
// ⚠️ **必须同时识别值形态与指针形态**：15.2 的 NewTurnLifecycleEvent 返回
// 值类型 TurnLifecycleEvent，而 provider/测试也可能把指针塞进
// HarnessEventPayload（interface 承载）。只断言 *TurnLifecycleEvent 会让
// 所有用构造器产生的事件判型失败——表现为 per-turn 游标把 STARTED 当
// 「非 STARTED」无限丢弃、最终阻塞（实测死锁）。
//
// 这与 15.2 的 EventRetentionOf 采用同一约定（15.2 spec:462 已说明
// 「payload 可传指针或值，两者结果一致」）。
func turnLifecycleKind(payload harnessprotocol.HarnessEventPayload) (harnessprotocol.TurnEventKind, bool) {
	switch typed := payload.(type) {
	case harnessprotocol.TurnLifecycleEvent:
		return typed.Kind, true
	case *harnessprotocol.TurnLifecycleEvent:
		if typed != nil {
			return typed.Kind, true
		}
	}
	return "", false
}

// isClosed 读游标关闭标志。
func (c *BufferedEventCursor) isClosed() bool {
	c.buffer.mu.Lock()
	defer c.buffer.mu.Unlock()
	return c.closed
}

// finishOnDrain 处理「流已耗尽」的收尾并返回应报的错误。
// Python: stream.py:119-126
//
// 关闭游标后：
//   - 单轮游标已选定轮次却未终态 → HarnessProtocolError（流提前关闭）
//   - 其余 → ErrEventStreamDone
func (c *BufferedEventCursor) finishOnDrain() error {
	selected := c.selectedTurnID
	hadSelected := c.hasSelected
	_ = c.Close(context.Background())
	if c.perTurn && hadSelected {
		return &harnessprotocol.HarnessProtocolError{
			Msg: fmt.Sprintf("harness event stream closed before turn %q terminated", selected),
		}
	}
	return harnessprotocol.ErrEventStreamDone
}
