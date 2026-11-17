package stream_actor

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// StreamConsumer 流式消费者接口，由 8.11 Vertex 实现。
// Python: base.py StreamConsumer(ABC) 三抽象方法。
type StreamConsumer interface {
	// StreamCall 消费者执行体；启动就绪后必须关闭 event（对齐 asyncio.Event.set），
	// 执行错误经 doneCallback 上报（对齐 error_callback）。
	// Python: stream_call(self, event, done_callback)
	StreamCall(ctx context.Context, event chan struct{}, doneCallback func(error))
	// ShouldHandleMessage 当前是否可接收流式帧
	// Python: should_handle_message()
	ShouldHandleMessage() bool
	// IsDone 消费者是否已完成（首帧启动判定用）
	// Python: is_done()
	IsDone() bool
}

// StreamGraph 流式节点注册表：node_id → StreamConsumer。
// Python: base.py StreamGraph。
type StreamGraph struct {
	// streamNodes node_id → consumer（首注册优先，Python add_stream_consumer 幂等语义）
	streamNodes map[string]StreamConsumer
}

// StreamPayload 流式载荷：原始帧 + 来源能力。
// Python: base.py @dataclass StreamPayload{message, source_ability}。
type StreamPayload struct {
	// Message 帧内容（单键 dict：{producer_id: content}）
	Message map[string]any
	// SourceAbility 产出方的组件能力
	SourceAbility components.ComponentAbility
}

// StreamProcessor 单能力分发器：主队列收帧，按 schema 叶子路径分发到注册的路径队列。
// Python: base.py StreamProcessor。每消费者节点的每个 (COLLECT|TRANSFORM) 能力一个。
type StreamProcessor struct {
	nodeID string
	// ability 所属能力（日志用，Go 侧承载 Python run(ability) 的入参）
	ability components.ComponentAbility
	// queue 主队列（D2 无界，send 永不阻塞）
	queue *payloadQueue
	// mu 保护 processorQueues（防 consume 之后再次 generator 注册与 run 并发，D1 伴生防御）
	mu sync.Mutex
	// processorQueues r_path → 路径队列列表（generator 注册）
	processorQueues map[string][]*payloadQueue
	// sourceGroups CNF 源组（保持声明顺序），键为 producerId-ABILITY
	sourceGroups []map[string]bool
	// sources 全部源键并集
	sources map[string]bool
	// sourceIDs 全部源键中的 producer id 集合
	sourceIDs map[string]bool
	// timeout 路径拉数超时；0 表示不超时（Python stream_generator_timeout<=0 → None）
	timeout time.Duration
}

// StreamActor 单消费者节点的门卫：首帧拉起消费者执行体、帧广播给全部 processor、错误拦截。
// Python: base.py StreamActor。每流式消费者节点一个，ActorManager 持有。
type StreamActor struct {
	// processors ability → processor（构造期按 consumer abilities ∩ {COLLECT, TRANSFORM} 建满）
	processors map[components.ComponentAbility]*StreamProcessor
	// mu 保护 task/taskErr/running 状态
	mu sync.Mutex
	// taskCtx 消费者执行体任务 ctx（首帧创建，shutdown 取消）
	taskCtx context.Context
	// taskCancel 配套取消函数
	taskCancel context.CancelFunc
	// taskDone 消费者执行体退出信号（close 语义）
	taskDone chan struct{}
	// taskErr 消费者错误回调上报表（容量 1）
	taskErr chan error
	// vertex 消费者（8.11 Vertex）
	vertex StreamConsumer
	// nodeID 消费者节点 ID
	nodeID string
	// runningCancel processor.run 的取消函数集（consume 时追加，shutdown 收割）
	runningCancel []context.CancelFunc
	// runningWg processor.run goroutine 计数
	runningWg sync.WaitGroup
}

// StreamReader gen 树叶子：从路径队列逐帧拉数的读取器（替代 Python async generator）。
// 组件在 on_collect/on_transform 内循环调用 Next(ctx)。
type StreamReader struct {
	// kPath 消费者输入树上的键路径
	kPath string
	// rPath 数据来源引用路径
	rPath string
	// q 底层路径队列
	q *payloadQueue
	// timeout 拉数超时；未声明源的路径才限时（_path_has_declared_source），0 不超时
	timeout time.Duration
	// nodeID 所属消费者节点 ID（日志用）
	nodeID string
	// callback 每帧回调（对齐 Python stream_callable，yield 后调用）
	callback func(map[string]any)
}

// payloadQueue 带锁无界内存队列（偏差 D2：Go chan 无法无界，等价实现 asyncio.Queue）。
// put 永不阻塞；get 支持 ctx 取消与 close。
type payloadQueue struct {
	mu     sync.Mutex
	cond   chan struct{} // 广播信号：每次 put/close 换新并关闭旧
	items  []any
	closed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamGraph 创建流式节点注册表
func NewStreamGraph() *StreamGraph {
	return &StreamGraph{streamNodes: map[string]StreamConsumer{}}
}

// AddStreamConsumer 注册消费者（node_id 已注册时忽略，对齐 Python 幂等）
func (g *StreamGraph) AddStreamConsumer(consumer StreamConsumer, nodeID string) {
	if _, ok := g.streamNodes[nodeID]; !ok {
		g.streamNodes[nodeID] = consumer
	}
}

// GetNode 按节点 ID 取消费者
func (g *StreamGraph) GetNode(nodeID string) StreamConsumer {
	return g.streamNodes[nodeID]
}

// NewStreamProcessor 构造（对齐 Python __init__：source_groups 去空、组内集合化、源并集、timeout<=0 置不超时）
func NewStreamProcessor(nodeID string, sourceGroups [][]string, timeout time.Duration) *StreamProcessor {
	sgs := make([]map[string]bool, 0, len(sourceGroups))
	sources := map[string]bool{}
	for _, g := range sourceGroups {
		if len(g) == 0 {
			continue
		}
		m := map[string]bool{}
		for _, k := range g {
			m[k] = true
			sources[k] = true
		}
		sgs = append(sgs, m)
	}
	sourceIDs := map[string]bool{}
	for k := range sources {
		sourceIDs[producerIDFromSourceKey(k)] = true
	}
	if timeout <= 0 {
		timeout = 0
	}
	return &StreamProcessor{
		nodeID:          nodeID,
		queue:           newPayloadQueue(),
		processorQueues: map[string][]*payloadQueue{},
		sourceGroups:    sgs,
		sources:         sources,
		sourceIDs:       sourceIDs,
		timeout:         timeout,
	}
}

// Receive 帧入主队列（对齐 Python receive）
func (p *StreamProcessor) Receive(payload StreamPayload) {
	p.queue.put(payload)
}

// Next 拉取下一帧。返回 (值, true)；EndFrame/超时/ctx 取消返回 (nil, false)。
// 对齐 Python base.py:271-306 generator() 内循环（yield 后调 stream_callable）。
func (r *StreamReader) Next(ctx context.Context) (any, bool) {
	v, ok := r.q.getWithTimeout(ctx, r.timeout)
	if !ok {
		// 区分超时与取消/关闭：超时记 warning（对齐 Python asyncio.TimeoutError 分支）
		if ctx.Err() == nil {
			logger.Warn(logComponent).
				Str("event_type", "GRAPH_RECEIVE_STREAM_CHUNK").
				Str("node_id", r.nodeID).
				Str("k_path", r.kPath).
				Str("r_path", r.rPath).
				Msgf("Receive chunk timeout %s of [%s.%s]", r.timeout, r.nodeID, r.kPath)
		}
		return nil, false
	}
	if utils.IsEndFrame(v) {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_RECEIVE_STREAM_CHUNK").
			Str("node_id", r.nodeID).
			Str("k_path", r.kPath).
			Str("r_path", r.rPath).
			Msgf("Receive EndFrame chunk of [%s.%s]", r.nodeID, r.kPath)
		return nil, false
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_RECEIVE_STREAM_CHUNK").
		Str("node_id", r.nodeID).
		Str("k_path", r.kPath).
		Str("r_path", r.rPath).
		Msgf("Receive chunk of [%s.%s]", r.nodeID, r.kPath)
	if r.callback != nil {
		r.callback(map[string]any{r.kPath: v})
	}
	return v, true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPayloadQueue 创建无界队列
func newPayloadQueue() *payloadQueue {
	return &payloadQueue{cond: make(chan struct{})}
}

// put 入队（永不阻塞）
func (q *payloadQueue) put(v any) {
	q.mu.Lock()
	q.items = append(q.items, v)
	old := q.cond
	q.cond = make(chan struct{})
	q.mu.Unlock()
	close(old)
}

// close 关闭队列（已入队帧仍可取）
func (q *payloadQueue) close() {
	q.mu.Lock()
	q.closed = true
	old := q.cond
	q.cond = make(chan struct{})
	q.mu.Unlock()
	close(old)
}

// get 取帧；空且未关闭时阻塞等待 put/close/ctx 取消
func (q *payloadQueue) get(ctx context.Context) (any, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			v := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return v, true
		}
		if q.closed {
			q.mu.Unlock()
			return nil, false
		}
		wait := q.cond
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false
		case <-wait:
		}
	}
}

// len 当前积压帧数（测试与调试辅助）
func (q *payloadQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// getWithTimeout 取帧；空且未关闭时等待，timeout>0 时限时（对齐 asyncio.wait_for(queue.get(), timeout)）。
// 超时返回 (nil, false) 且 ctx.Err()==nil；取消返回 (nil, false) 且 ctx.Err()!=nil。
func (q *payloadQueue) getWithTimeout(ctx context.Context, timeout time.Duration) (any, bool) {
	if timeout <= 0 {
		return q.get(ctx)
	}
	// 先取现成帧（超时从等待起算）
	q.mu.Lock()
	if len(q.items) > 0 {
		v := q.items[0]
		q.items = q.items[1:]
		q.mu.Unlock()
		return v, true
	}
	if q.closed {
		q.mu.Unlock()
		return nil, false
	}
	wait := q.cond
	q.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, false
	case <-timer.C:
		return nil, false
	case <-wait:
		// 新帧到达，重入取值（循环由调用方 Next 处理，此处直接取一次）
		return q.getWithTimeout(ctx, timeout)
	}
}

// getProducerID 从单键帧取 producer id；非 dict 或键数 !=1 时 panic（对齐 Python ValueError）。
// Python: base.py _get_producer_id。
func getProducerID(message map[string]any) string {
	if message == nil || len(message) != 1 {
		panic("message is invalid")
	}
	for k := range message {
		return k
	}
	return ""
}

// isEndMessage 判定帧是否为结束哨兵：内容为字符串且以 END_ 开头（偏差 D5，复刻字符串约定）。
// Python: base.py _is_end_message。
func isEndMessage(message map[string]any) bool {
	producerID := getProducerID(message)
	content, ok := message[producerID].(string)
	return ok && strings.HasPrefix(content, "END_")
}

// isValueFromSource 路径是否属于源（path == id 或 path 以 "id." 开头）（Python:231-233）
func isValueFromSource(path, sourceID string) bool {
	return path == sourceID || strings.HasPrefix(path, sourceID+".")
}

// producerIDFromSourceKey 源键反解 producer id（rsplit("-",1)[0]，Python:241-243）
func producerIDFromSourceKey(sourceKey string) string {
	if i := strings.LastIndex(sourceKey, "-"); i >= 0 {
		return sourceKey[:i]
	}
	return sourceKey
}

// uniqueSourceKey 源键 = producerId-ABILITY（Python:236-239）
func uniqueSourceKey(payload StreamPayload) string {
	return getProducerID(payload.Message) + "-" + payload.SourceAbility.Name()
}

// secondsToDuration 负值→0（不超时）
func secondsToDuration(sec float64) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}
