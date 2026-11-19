package stream_actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	ability "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"
)

// TestPayloadQueue_无界不阻塞 send 侧永不阻塞（D2，对齐 asyncio.Queue 无界语义）
func TestPayloadQueue_无界不阻塞(t *testing.T) {
	q := newPayloadQueue()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			q.put(StreamPayload{})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("put 10000 帧不应阻塞（无界队列）")
	}
}

// TestPayloadQueue_Get_取消 Get 阻塞时 ctx 取消可退出
func TestPayloadQueue_Get_取消(t *testing.T) {
	q := newPayloadQueue()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	start := time.Now()
	_, ok := q.get(ctx)
	if ok {
		t.Fatal("取消后 get 应返回 ok=false")
	}
	if time.Since(start) > time.Second {
		t.Fatal("ctx 取消应立即解除阻塞")
	}
}

// TestPayloadQueue_关闭 Get 在 Close 后立即返回 false（已入队帧仍可取）
func TestPayloadQueue_关闭(t *testing.T) {
	q := newPayloadQueue()
	q.put(StreamPayload{})
	q.close()
	if _, ok := q.get(context.Background()); !ok {
		t.Fatal("已入队帧应在 close 后仍可取出")
	}
	if _, ok := q.get(context.Background()); ok {
		t.Fatal("取尽后 close 队列 get 应返回 false")
	}
}

// TestPayloadQueue_len 队列长度辅助
func TestPayloadQueue_len(t *testing.T) {
	q := newPayloadQueue()
	q.put(StreamPayload{})
	q.put(StreamPayload{})
	if q.len() != 2 {
		t.Fatalf("len 应为 2, got %d", q.len())
	}
}

// TestGetProducerID 对齐 Python _get_producer_id：单键 dict 取首键，否则 ValueError（Go panic）
func TestGetProducerID(t *testing.T) {
	if got := getProducerID(map[string]any{"nodeA": "x"}); got != "nodeA" {
		t.Fatalf("got %q", got)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("双键 message 应 panic 对齐 Python ValueError")
			}
		}()
		getProducerID(map[string]any{"a": 1, "b": 2})
	}()
}

// TestIsEndMessage 对齐 Python _is_end_message：内容 END_ 前缀判定（D5）
func TestIsEndMessage(t *testing.T) {
	if !isEndMessage(map[string]any{"nodeA": "END_nodeA"}) {
		t.Fatal("END_ 前缀应判定为结束帧")
	}
	if isEndMessage(map[string]any{"nodeA": "hello"}) {
		t.Fatal("普通内容不应判定为结束帧")
	}
	if isEndMessage(map[string]any{"nodeA": 123}) {
		t.Fatal("非字符串内容不应判定为结束帧")
	}
}

// TestStreamGraph_注册幂等 首注册优先（对齐 Python add_stream_consumer）
func TestStreamGraph_注册幂等(t *testing.T) {
	g := NewStreamGraph()
	c1 := &utils.EndFrame{Source: "c1"}
	c2 := &utils.EndFrame{Source: "c2"}
	// 借用 StreamConsumer 接口验证注册行为需要 fake，这里用最小 fake
	f1 := &fakeGraphConsumer{}
	f2 := &fakeGraphConsumer{}
	g.AddStreamConsumer(f1, "n")
	g.AddStreamConsumer(f2, "n")
	if g.GetNode("n") != f1 {
		t.Fatal("重复注册应忽略（首注册优先）")
	}
	if g.GetNode("missing") != nil {
		t.Fatal("未注册应返回 nil")
	}
	_ = c1
	_ = c2
}

// fakeGraphConsumer 最小 StreamConsumer
type fakeGraphConsumer struct{}

func (f *fakeGraphConsumer) StreamCall(ctx context.Context, event chan struct{}, done func(error)) {}
func (f *fakeGraphConsumer) ShouldHandleMessage() bool                                             { return false }
func (f *fakeGraphConsumer) IsDone() bool                                                          { return false }

func newTestProcessor(groups ...[]string) *StreamProcessor {
	sgs := make([]map[string]bool, 0, len(groups))
	for _, g := range groups {
		m := map[string]bool{}
		for _, k := range g {
			m[k] = true
		}
		sgs = append(sgs, m)
	}
	return &StreamProcessor{
		nodeID:          "C",
		queue:           newPayloadQueue(),
		processorQueues: map[string][]*payloadQueue{},
		sourceGroups:    sgs,
		timeout:         -1,
	}
}

// 讨论点 2：帧广播不看 ability 匹配——producer STREAM 帧照常分发
func TestProcessorRun_广播不匹配ability(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"})
	p.sources = map[string]bool{"A-stream": true}
	p.sourceIDs = map[string]bool{"A": true}
	p.processorQueues["A.answer"] = []*payloadQueue{newPayloadQueue()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx, ability.ComponentAbilityCollect)
	time.Sleep(20 * time.Millisecond) // 等 run 启动（D1：run 先启动后投帧）
	p.Receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "你"}}, SourceAbility: ability.ComponentAbilityStream})
	v, ok := p.processorQueues["A.answer"][0].get(context.Background())
	if !ok {
		t.Fatal("应收到分发值")
	}
	if v != "你" {
		t.Fatalf("got %v", v)
	}
}

// 讨论点 4（D1 新行为）：注册先于 run，注册前的帧滞留主队列、run 启动后补投不丢
func TestProcessorRun_注册前帧不丢失(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"})
	p.sources = map[string]bool{"A-stream": true}
	p.sourceIDs = map[string]bool{"A": true}
	// 注意：先投帧，后注册，再启动 run（D1 顺序）
	p.Receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "你"}}})
	p.processorQueues["A.answer"] = []*payloadQueue{newPayloadQueue()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx, ability.ComponentAbilityCollect)
	v, ok := p.processorQueues["A.answer"][0].get(context.Background())
	if !ok || v != "你" {
		t.Fatalf("注册前帧应补投, got %v ok=%v", v, ok)
	}
}

// 讨论点 5：多源组全到齐才关全流；单源到齐只关自己路径
func TestProcessorRun_多源组关流(t *testing.T) {
	p := newTestProcessor([]string{"A-stream", "B-stream"})
	p.sources = map[string]bool{"A-stream": true, "B-stream": true}
	p.sourceIDs = map[string]bool{"A": true, "B": true}
	qa := newPayloadQueue()
	qb := newPayloadQueue()
	p.processorQueues["A.answer"] = []*payloadQueue{qa}
	p.processorQueues["B.results"] = []*payloadQueue{qb}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx, ability.ComponentAbilityCollect); close(done) }()
	time.Sleep(20 * time.Millisecond)
	// A 先发数据帧（source_path_map 记录路径后 END 才关这些路径——Python 同语义）
	p.Receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "x"}}, SourceAbility: ability.ComponentAbilityStream})
	_, _ = qa.get(context.Background()) // 消费数据帧
	// A 组内到齐：只关 A 路径，B 路径不动
	p.Receive(StreamPayload{Message: map[string]any{"A": "END_A"}, SourceAbility: ability.ComponentAbilityStream})
	// closeInactiveGroupSources：A 数据帧时 B（同组非活跃源）路径已被关流
	if v, ok := qb.get(context.Background()); !ok {
		t.Fatal("B 路径应收到 EndFrame(B)（close_inactive_group_sources）")
	} else if ef, isEF := v.(*utils.EndFrame); !isEF || ef.Source != "B" {
		t.Fatalf("B 路径应收到 EndFrame(B), got %v", v)
	}
	// Python `group & handled` 为组内 OR：{A,B} 组任一源到齐即满足 → A END 后关全队列退出
	p.Receive(StreamPayload{Message: map[string]any{"A": "END_A"}, SourceAbility: ability.ComponentAbilityStream})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("OR 组任一源到齐后 run 应退出（对齐 Python _all_source_groups_finished）")
	}
	// closeAllQueues 给所有路径放 EndFrame(A)
	if v, ok := qb.get(context.Background()); !ok {
		t.Fatal("关全队列后 B 路径应收到 EndFrame(A)")
	} else if ef, isEF := v.(*utils.EndFrame); !isEF || ef.Source != "A" {
		t.Fatalf("B 路径应收到 EndFrame(A), got %v", v)
	}
}

// 讨论点 5 补充：单源组（默认拆分）单源到齐即全组满足 → 立即关全队列
func TestProcessorRun_单源组到齐即关全流(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"}, []string{"B-stream"})
	p.sources = map[string]bool{"A-stream": true, "B-stream": true}
	p.sourceIDs = map[string]bool{"A": true, "B": true}
	qa := newPayloadQueue()
	qb := newPayloadQueue()
	p.processorQueues["A.answer"] = []*payloadQueue{qa}
	p.processorQueues["B.results"] = []*payloadQueue{qb}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.Run(ctx, ability.ComponentAbilityCollect); close(done) }()
	time.Sleep(20 * time.Millisecond)
	p.Receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "x"}}, SourceAbility: ability.ComponentAbilityStream})
	_, _ = qa.get(context.Background())
	p.Receive(StreamPayload{Message: map[string]any{"A": "END_A"}, SourceAbility: ability.ComponentAbilityStream})
	select {
	case <-done:
		t.Fatal("仅 A 组到齐 run 不应退出（B 组未到齐）")
	case <-time.After(100 * time.Millisecond):
	}
	p.Receive(StreamPayload{Message: map[string]any{"B": map[string]any{"results": "y"}}, SourceAbility: ability.ComponentAbilityStream})
	_, _ = qb.get(context.Background())
	p.Receive(StreamPayload{Message: map[string]any{"B": "END_B"}, SourceAbility: ability.ComponentAbilityStream})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("两组均到齐后 run 应退出（对齐 Python _all_source_groups_finished）")
	}
}

// 讨论点 6：END 哨兵前缀判定 + EndFrame 进路径队列
func TestProcessorRun_EndFrame传播(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"})
	p.sources = map[string]bool{"A-stream": true}
	p.sourceIDs = map[string]bool{"A": true}
	q := newPayloadQueue()
	p.processorQueues["A.answer"] = []*payloadQueue{q}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx, ability.ComponentAbilityCollect)
	time.Sleep(20 * time.Millisecond)
	p.Receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "x"}}, SourceAbility: ability.ComponentAbilityStream})
	_, _ = q.get(context.Background())
	p.Receive(StreamPayload{Message: map[string]any{"A": "END_A"}, SourceAbility: ability.ComponentAbilityStream})
	v, ok := q.get(context.Background())
	if !ok {
		t.Fatal("路径队列应收到 EndFrame")
	}
	ef, isEF := v.(*utils.EndFrame)
	if !isEF || ef.Source != "A" {
		t.Fatalf("EndFrame.Source 应为 A, got %v", v)
	}
}

// 讨论点 8：未声明源的路径限时，超时退出；声明源不限时
func TestStreamReader_超时(t *testing.T) {
	r := &StreamReader{kPath: "x", rPath: "unknown.foo", q: newPayloadQueue(),
		timeout: 30 * time.Millisecond, nodeID: "C"}
	start := time.Now()
	if _, ok := r.Next(context.Background()); ok {
		t.Fatal("未声明源超时应返回 false")
	}
	if time.Since(start) > time.Second {
		t.Fatal("应在 timeout 后退出")
	}
	// 声明源：manager.generator 置 timeout=0 → 不限时，只能 ctx 取消
	r2 := &StreamReader{kPath: "x", rPath: "A.foo", q: newPayloadQueue(), timeout: 0, nodeID: "C"}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	if _, ok := r2.Next(ctx); ok {
		t.Fatal("ctx 取消应退出")
	}
}

// 讨论点 10：generator 按 schema 叶子路径建 gen 树（rebuild_dict 形状）+ 每路径多 chan 广播
func TestProcessorGenerator_叶子树重建(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"})
	p.sources = map[string]bool{"A-stream": true}
	p.sourceIDs = map[string]bool{"A": true, "B": true}
	schema := map[string]any{
		"llm_text": "${A.answer}",
		"search":   map[string]any{"hits": "${B.results}"},
		"plain":    3,
	}
	out := p.Generator(schema, nil)
	if _, ok := out["llm_text"].(*StreamReader); !ok {
		t.Fatalf("llm_text 叶子应为 StreamReader, got %T", out["llm_text"])
	}
	sub, ok := out["search"].(map[string]any)
	if !ok {
		t.Fatal("search 应保持嵌套 map")
	}
	if _, ok := sub["hits"].(*StreamReader); !ok {
		t.Fatalf("search.hits 叶子应为 StreamReader, got %T", sub["hits"])
	}
	if out["plain"] != 3 {
		t.Fatal("非 $ref 叶子保持原值")
	}
	if len(p.processorQueues) != 2 {
		t.Fatalf("应注册 2 条路径, got %d", len(p.processorQueues))
	}
}

// 每路径多 chan 广播：同一路径注册两次，帧同时投两处
func TestProcessorGenerator_每路径多chan广播(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"})
	p.sources = map[string]bool{"A-stream": true}
	p.sourceIDs = map[string]bool{"A": true}
	_ = p.Generator(map[string]any{"a": "${A.answer}"}, nil)
	_ = p.Generator(map[string]any{"b": "${A.answer}"}, nil)
	queues := p.processorQueues["${A.answer}"]
	if len(queues) != 2 {
		t.Fatalf("同路径应注册 2 个队列, got %d", len(queues))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx, ability.ComponentAbilityCollect)
	time.Sleep(20 * time.Millisecond)
	p.Receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "v"}}})
	for i, q := range queues {
		if v, ok := q.get(context.Background()); !ok || v != "v" {
			t.Fatalf("队列 %d 应广播到帧, got %v ok=%v", i, v, ok)
		}
	}
}

// 未声明源超时：generator 生成的 StreamReader 带限速，超时后 reader 退出
func TestProcessorGenerator_未声明源限时(t *testing.T) {
	p := newTestProcessor([]string{"A-stream"})
	p.sources = map[string]bool{"A-stream": true}
	p.sourceIDs = map[string]bool{"A": true}
	p.timeout = 30 * time.Millisecond
	r := p.createGenerator("x", "unknown.foo", nil)
	if r.timeout != 30*time.Millisecond {
		t.Fatal("未声明源路径应限时")
	}
	r2 := p.createGenerator("x", "A.foo", nil)
	if r2.timeout != 0 {
		t.Fatal("声明源路径应不限时")
	}
}

// fakeConsumer 可控 StreamConsumer
type fakeConsumer struct {
	handle     bool
	done       bool
	eventFn    func(chan struct{})
	callErr    error
	callWaitCh chan struct{} // 非空时 StreamCall 阻塞至此关闭
}

func (f *fakeConsumer) StreamCall(ctx context.Context, event chan struct{}, done func(error)) {
	if f.eventFn != nil {
		f.eventFn(event)
	}
	if f.callErr != nil {
		done(f.callErr)
	}
	if f.callWaitCh != nil {
		select {
		case <-f.callWaitCh:
		case <-ctx.Done(): // 对齐真实 Vertex：执行体响应 ctx 取消
		}
	}
}
func (f *fakeConsumer) ShouldHandleMessage() bool { return f.handle }
func (f *fakeConsumer) IsDone() bool              { return f.done }

// 讨论点 3：首帧拉起消费者、等 event 置位、未 handle 丢弃
func TestActorSend_首帧拉起(t *testing.T) {
	eventClosed := make(chan struct{})
	v := &fakeConsumer{handle: true, done: true, eventFn: func(e chan struct{}) { close(e); close(eventClosed) }}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	// 未 handle 直接丢弃，不拉起任务
	vNoHandle := &fakeConsumer{handle: false}
	a2 := NewStreamActor("C2", vNoHandle, nil, [][]string{{"A-stream"}}, -1)
	_ = a2.Send(ctx, map[string]any{"A": "x"}, ability.ComponentAbilityStream, false, "A")
	if a2.taskCtx != nil {
		t.Fatal("未 handle 不应拉起任务")
	}
	// 首帧拉起
	if err := actor.Send(ctx, map[string]any{"A": map[string]any{"answer": "你"}},
		ability.ComponentAbilityStream, true, "A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-eventClosed:
	case <-time.After(time.Second):
		t.Fatal("应等待 event 置位")
	}
}

// 讨论点 3：非首帧且任务未启动且 vertex 已 done → 丢弃
func TestActorSend_任务已结束丢弃(t *testing.T) {
	v := &fakeConsumer{handle: true, done: true}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	if err := actor.Send(ctx, map[string]any{"A": "x"}, ability.ComponentAbilityStream, false, "A"); err != nil {
		t.Fatal(err)
	}
	if actor.taskCtx != nil {
		t.Fatal("非首帧且 vertex 已 done 不应拉起任务")
	}
}

// 讨论点 3：错误回调置 taskErr，后续首帧被拦截
func TestActorSend_错误拦截(t *testing.T) {
	v := &fakeConsumer{handle: true, done: true, callErr: errors.New("boom"),
		eventFn: func(e chan struct{}) { close(e) }}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	if err := actor.Send(ctx, map[string]any{"A": "x"}, ability.ComponentAbilityStream, true, "A"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond) // 等 taskErr 被错误回调填充
	if err := actor.Send(ctx, map[string]any{"A": "y"}, ability.ComponentAbilityStream, true, "A"); err == nil {
		t.Fatal("task_error 有异常时后续首帧应被拦截")
	}
}

// 广播：帧进入全部 processor 主队列（不看 ability 匹配）
func TestActorSend_广播全部processor(t *testing.T) {
	v := &fakeConsumer{handle: true, done: true, eventFn: func(e chan struct{}) { close(e) }}
	actor := NewStreamActor("C", v,
		[]ability.ComponentAbility{ability.ComponentAbilityCollect, ability.ComponentAbilityTransform},
		[][]string{{"A-stream"}}, -1)
	if len(actor.processors) != 2 {
		t.Fatal("应建两个 processor")
	}
	// 注册订阅路径须先于 Send（run 于 event 放行后启动，广播帧直接路由到订阅者）
	qa := newPayloadQueue()
	qb := newPayloadQueue()
	actor.processors[ability.ComponentAbilityCollect].processorQueues["A.x"] = []*payloadQueue{qa}
	actor.processors[ability.ComponentAbilityTransform].processorQueues["A.x"] = []*payloadQueue{qb}
	ctx := context.Background()
	if err := actor.Send(ctx, map[string]any{"A": map[string]any{"x": "hello"}},
		ability.ComponentAbilityStream, true, "A"); err != nil {
		t.Fatal(err)
	}
	for ab, q := range map[string]*payloadQueue{"collect": qa, "transform": qb} {
		if got, ok := q.getWithTimeout(ctx, time.Second); !ok || got != "hello" {
			t.Fatalf("processor %s 应路由到帧, got %v ok=%v", ab, got, ok)
		}
	}
}

// D1 还原（8.11 前置）：外层 event 放行后 Send 启动【全部】processor.run
// （Python send:98-100，含未走 Consume 注册的 ability 空转）。
func TestActorSend_启动全部processor(t *testing.T) {
	v := &fakeConsumer{handle: true, done: true, eventFn: func(e chan struct{}) { close(e) }}
	actor := NewStreamActor("C", v,
		[]ability.ComponentAbility{ability.ComponentAbilityCollect, ability.ComponentAbilityTransform},
		[][]string{{"A-stream"}}, -1)
	qa := newPayloadQueue()
	qb := newPayloadQueue()
	actor.processors[ability.ComponentAbilityCollect].processorQueues["A.answer"] = []*payloadQueue{qa}
	actor.processors[ability.ComponentAbilityTransform].processorQueues["A.answer"] = []*payloadQueue{qb}
	ctx := context.Background()
	if err := actor.Send(ctx, map[string]any{"A": map[string]any{"answer": "你"}},
		ability.ComponentAbilityStream, true, "A"); err != nil {
		t.Fatal(err)
	}
	// 路由值按 Python 语义：origin 路径解析到叶值
	got, ok := qa.getWithTimeout(ctx, time.Second)
	if !ok || got != "你" {
		t.Fatalf("COLLECT 订阅路径应收到分发值, got %v ok=%v", got, ok)
	}
	got2, ok2 := qb.getWithTimeout(ctx, time.Second)
	if !ok2 || got2 != "你" {
		t.Fatalf("TRANSFORM（未走 Consume）run 也应被启动并路由, got %v ok=%v", got2, ok2)
	}
	if err := actor.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

// 讨论点 9：shutdown 级联取消（-race）
func TestActorShutdown_级联取消(t *testing.T) {
	wait := make(chan struct{})
	v := &fakeConsumer{handle: true, done: true, callWaitCh: wait, eventFn: func(e chan struct{}) { close(e) }}
	actor := NewStreamActor("C", v, []ability.ComponentAbility{ability.ComponentAbilityCollect},
		[][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	_ = actor.Send(ctx, map[string]any{"A": "x"}, ability.ComponentAbilityStream, true, "A")
	actor.startProcessor(ctx, actor.processors[ability.ComponentAbilityCollect])
	if err := actor.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wait:
		t.Fatal("shutdown 应取消消费者任务（callWaitCh 阻塞的 goroutine 应回收）")
	default:
	}
	// 二次 Shutdown 幂等（task 已清空）
	if err := actor.Shutdown(); err != nil {
		t.Fatal(err)
	}
}
