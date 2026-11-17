package stream_actor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cutils "github.com/uapclaw/uapclaw-go/internal/common/utils"
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

// NewStreamActor 构造（对齐 Python __init__：每 ability 建一个 processor；
// abilities 为空时不建 processor，processor 可由测试/ActorManager 显式注入）。
func NewStreamActor(nodeID string, vertex StreamConsumer, abilities []components.ComponentAbility,
	sourceGroups [][]string, timeout time.Duration) *StreamActor {
	ps := map[components.ComponentAbility]*StreamProcessor{}
	for _, ab := range abilities {
		p := NewStreamProcessor(nodeID, sourceGroups, timeout)
		p.ability = ab
		ps[ab] = p
	}
	return &StreamActor{processors: ps, vertex: vertex, nodeID: nodeID}
}

// Send 接收帧：未 handle 丢弃 → 任务未启动时首帧拉起（等 event）→ 广播全部 processor。
// Python: base.py send(64-104) 一比一；偏差 D1：此处不启动 processor.run（挪至 consume 注册后）。
func (a *StreamActor) Send(ctx context.Context, message map[string]any,
	sourceAbility components.ComponentAbility, firstFrame bool, producerID string) error {
	// Python send 第一分支：未 handle 丢弃（base.py:69-74）
	if !a.vertex.ShouldHandleMessage() {
		logger.Warn(logComponent).
			Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
			Str("node_id", a.nodeID).
			Str("producer_id", producerID).
			Str("chunk", fmt.Sprintf("%v", message)).
			Msgf("Discard chunk send from [%s], %s[%s] unable to handle", producerID, a.nodeID, sourceAbility.Name())
		return nil
	}
	a.mu.Lock()
	needStart := a.taskCtx == nil || a.taskDoneClosed()
	if !needStart {
		a.mu.Unlock()
	} else {
		// task 已结束且带异常 → 拦截（base.py:76-85）
		if a.taskCtx != nil && a.taskDoneClosed() && a.taskErr != nil {
			select {
			case err := <-a.taskErr:
				logger.Warn(logComponent).
					Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
					Str("node_id", a.nodeID).
					Str("producer_id", producerID).
					Err(err).
					Msgf("Discard chunk send from [%s], %s[%s] occur exception", producerID, a.nodeID, sourceAbility.Name())
				a.mu.Unlock()
				return err
			default:
			}
		}
		// 非首帧或 vertex 已 done → 丢弃（base.py:86-90）
		if !firstFrame || a.vertex.IsDone() {
			logger.Warn(logComponent).
				Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
				Str("node_id", a.nodeID).
				Str("producer_id", producerID).
				Str("chunk", fmt.Sprintf("%v", message)).
				Msgf("Discard chunk send from [%s], %s[%s] vertex is done", producerID, a.nodeID, sourceAbility.Name())
			a.mu.Unlock()
			return nil
		}
		// 首帧拉起（base.py:91-100）
		event := make(chan struct{})
		a.taskErr = make(chan error, 1)
		taskCtx, cancel := context.WithCancel(ctx)
		a.taskCtx, a.taskCancel, a.taskDone = taskCtx, cancel, make(chan struct{})
		a.mu.Unlock()
		go func() {
			defer close(a.taskDone)
			a.vertex.StreamCall(taskCtx, event, func(err error) {
				if err != nil {
					select {
					case a.taskErr <- err:
					default:
					}
				}
			})
		}()
		select {
		case <-event: // 等 vertex 把 stream-in 策略全部启动（对齐 await event.wait()）
		case <-ctx.Done():
			return ctx.Err()
		}
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_STREAM_ACTOR_START").
			Str("node_id", a.nodeID).
			Msgf("Stream actor task node [%s] started", a.nodeID)
	}
	// 广播全部 processor（base.py:101-103，不看 ability 匹配）
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
		Str("node_id", a.nodeID).
		Str("producer_id", producerID).
		Str("chunk", fmt.Sprintf("%v", message)).
		Msgf("Send chunk from [%s] to %s[%s]", producerID, a.nodeID, sourceAbility.Name())
	for _, processor := range a.processors {
		processor.Receive(StreamPayload{Message: message, SourceAbility: sourceAbility})
	}
	return nil
}

// Generator 按 ability 取 processor 注册 gen 树（Python generator(105-108)）
func (a *StreamActor) Generator(ability components.ComponentAbility, schema map[string]any,
	streamCallback func(map[string]any)) map[string]any {
	p := a.processors[ability]
	if p == nil {
		return map[string]any{}
	}
	return p.Generator(schema, streamCallback)
}

// startProcessor consume 注册完成后启动 processor.run（偏差 D1 新挂载点，替代 Python send:98-100）
func (a *StreamActor) startProcessor(parent context.Context, p *StreamProcessor) {
	ctx, cancel := context.WithCancel(parent)
	a.mu.Lock()
	a.runningCancel = append(a.runningCancel, cancel)
	a.runningWg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.runningWg.Done()
		p.Run(ctx, p.ability)
	}()
}

// Shutdown 级联取消：消费者任务 → taskErr → 全部 processor.run（Python:114-157 步骤一比一，finally 清空）。
func (a *StreamActor) Shutdown() error {
	logMsg := func() *zerolog.Event {
		return logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_STREAM_ACTOR_SHUTDOWN").
			Str("node_id", a.nodeID)
	}
	logMsg().Msgf("Begin to shutdown stream actor task for %s", a.nodeID)
	// 1. 取消消费者任务并等待退出（Python:121-129）
	a.mu.Lock()
	taskCancel, taskDone, taskErr := a.taskCancel, a.taskDone, a.taskErr
	a.mu.Unlock()
	if taskCancel != nil {
		taskCancel()
		if taskDone != nil {
			<-taskDone // 对齐 await self._task（CancelledError/Exception 分支由 goroutine 退出承载）
		}
		logMsg().Msgf("Cancel stream actor task for %s", a.nodeID)
	}
	// 2. taskErr 处理（Python:130-143：未完成则取消；已完成且有异常则记日志）
	if taskErr != nil {
		select {
		case err := <-taskErr:
			logMsg().Err(err).Msgf("No need cancel stream actor error task for %s with exception", a.nodeID)
		default:
		}
	}
	// 3. 全部 processor.run 取消并收割（Python:145-153 gather return_exceptions=True）
	a.mu.Lock()
	cancels := a.runningCancel
	a.runningCancel = nil
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	a.runningWg.Wait()
	// finally：清空任务状态（Python:155-157）
	a.mu.Lock()
	a.taskCtx, a.taskCancel, a.taskDone, a.taskErr = nil, nil, nil, nil
	a.mu.Unlock()
	logMsg().Msgf("Succeed to shutdown stream actor task for %s", a.nodeID)
	return nil
}

// taskDoneClosed 任务是否已退出
func (a *StreamActor) taskDoneClosed() bool {
	if a.taskDone == nil {
		return false
	}
	select {
	case <-a.taskDone:
		return true
	default:
		return false
	}
}

// Receive 帧入主队列（对齐 Python receive）
func (p *StreamProcessor) Receive(payload StreamPayload) {
	p.queue.put(payload)
}

// Run 常驻分发循环（偏差 D1：由 consume 注册完成后启动，非 send 首帧）。
// Python: base.py run(ability) 170-195 步骤一比一：
// 取帧 → end 帧关流判定（记录 handle_map、按 source_path_map 关路径、全组满足关全队列）
// → 非帧关非活跃组源 + 按 r_path 摘值投路径队列 → 全组满足退出。
func (p *StreamProcessor) Run(ctx context.Context, ability components.ComponentAbility) {
	handleMap := map[string]bool{}
	sourcePathMap := map[string]map[string]bool{}
	for {
		raw, ok := p.queue.get(ctx)
		if !ok {
			return // ctx 取消/shutdown（Python 由 task cancel 承担）
		}
		payload := raw.(StreamPayload)
		sourceKey := uniqueSourceKey(payload)
		if isEndMessage(payload.Message) {
			sourceID := getProducerID(payload.Message)
			handleMap[sourceKey] = true
			p.closeQueuesForSourceKey(sourceID, sourceKey, sourcePathMap)
			if p.allSourceGroupsFinished(handleMap) {
				p.closeAllQueues(sourceID)
			}
		} else {
			p.closeInactiveGroupSources(sourceKey)
			p.mu.Lock()
			for path, queues := range p.processorQueues {
				originPath := utils.ExtractOriginKey(path)
				value := utils.GetValueByNestedPath(originPath, payload.Message)
				if value != nil {
					if sourcePathMap[sourceKey] == nil {
						sourcePathMap[sourceKey] = map[string]bool{}
					}
					sourcePathMap[sourceKey][path] = true
					for _, q := range queues {
						q.put(value)
					}
				}
			}
			p.mu.Unlock()
		}
		if p.allSourceGroupsFinished(handleMap) {
			return
		}
	}
}

// Generator 按 schema 叶子路径建 gen 树（Python:248-260 generator 一比一）。
// $ref 叶子建 StreamReader 注册路径队列；非 $ref 叶子原样保留；rebuild_dict 还原形状。
func (p *StreamProcessor) Generator(schema map[string]any,
	streamCallback func(map[string]any)) map[string]any {
	if len(schema) == 0 {
		return map[string]any{}
	}
	leaves := cutils.ExtractLeafNodes(schema)
	pairs := make([]cutils.LeafNode, 0, len(leaves))
	for _, leaf := range leaves {
		pathStr := cutils.FormatPath(leaf.Path)
		refStr, ok := leaf.Value.(string)
		if !ok || !strings.Contains(refStr, "$") {
			pairs = append(pairs, leaf)
			continue
		}
		pairs = append(pairs, cutils.LeafNode{Path: leaf.Path,
			Value: p.createGenerator(pathStr, refStr, streamCallback)})
	}
	return cutils.RebuildDict(pairs)
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

// allSourceGroupsFinished 组全到齐判定（Python:197-200，空组恒 false）
func (p *StreamProcessor) allSourceGroupsFinished(handled map[string]bool) bool {
	if len(p.sourceGroups) == 0 {
		return false
	}
	for _, group := range p.sourceGroups {
		hit := false
		for k := range group {
			if handled[k] {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// closeInactiveGroupSources 组内其他源提前关流（Python:202-207）
func (p *StreamProcessor) closeInactiveGroupSources(activeKey string) {
	for _, group := range p.sourceGroups {
		if !group[activeKey] || len(group) <= 1 {
			continue
		}
		for k := range group {
			if k != activeKey {
				p.closeQueuesForSource(producerIDFromSourceKey(k))
			}
		}
	}
}

// closeQueuesForSource 按源 id 关其名下路径队列（Python:209-213，put EndFrame(source_id)）
func (p *StreamProcessor) closeQueuesForSource(sourceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for path, queues := range p.processorQueues {
		if isValueFromSource(utils.ExtractOriginKey(path), sourceID) {
			for _, q := range queues {
				q.put(&utils.EndFrame{Source: sourceID})
			}
		}
	}
}

// closeQueuesForSourceKey 按源键只关其处理过的路径（Python:215-222，用 source_path_map）
func (p *StreamProcessor) closeQueuesForSourceKey(sourceID, sourceKey string, sourcePathMap map[string]map[string]bool) {
	paths := sourcePathMap[sourceKey]
	if len(paths) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for path := range paths {
		for _, q := range p.processorQueues[path] {
			q.put(&utils.EndFrame{Source: sourceID})
		}
	}
}

// closeAllQueues 全部路径队列关流（Python:224-229）
func (p *StreamProcessor) closeAllQueues(sourceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, queues := range p.processorQueues {
		for _, q := range queues {
			q.put(&utils.EndFrame{Source: sourceID})
		}
	}
}

// createGenerator 建路径队列并注册，返回 StreamReader（Python:262-268 _create_generator 前半）。
// 未声明源的路径才带 timeout（Python:269 _path_has_declared_source）。
func (p *StreamProcessor) createGenerator(kPath, rPath string, streamCallback func(map[string]any)) *StreamReader {
	q := newPayloadQueue()
	p.mu.Lock()
	p.processorQueues[rPath] = append(p.processorQueues[rPath], q)
	p.mu.Unlock()
	timeout := time.Duration(0)
	if !p.pathHasDeclaredSource(rPath) {
		timeout = p.timeout
	}
	return &StreamReader{kPath: kPath, rPath: rPath, q: q, timeout: timeout, nodeID: p.nodeID,
		callback: streamCallback}
}

// pathHasDeclaredSource 路径的源 id 是否在 source_ids 中（Python:308-313）
func (p *StreamProcessor) pathHasDeclaredSource(rPath string) bool {
	origin := utils.ExtractOriginKey(rPath)
	if origin == "" {
		return false
	}
	sourceID := origin
	if i := strings.Index(origin, "."); i >= 0 {
		sourceID = origin[:i]
	}
	return p.sourceIDs[sourceID]
}
