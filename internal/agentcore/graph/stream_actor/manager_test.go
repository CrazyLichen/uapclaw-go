package stream_actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// fakeSession ActorSession 测试替身
type fakeSession struct {
	env map[string]float64
	wf  map[string]any
}

func (s *fakeSession) GetEnvFloat(key string) (float64, bool) {
	v, ok := s.env[key]
	return v, ok
}
func (s *fakeSession) GetWorkflowStateValue(key string) any { return s.wf[key] }
func (s *fakeSession) UpdateAndCommitWorkflowState(data map[string]any) {
	for k, v := range data {
		s.wf[k] = v
	}
}

func buildTestManager(t *testing.T, spec *workflow.WorkflowSpec, graph *StreamGraph, sess *fakeSession) *ActorManager {
	t.Helper()
	if sess == nil {
		sess = &fakeSession{wf: map[string]any{}}
	}
	mgr, err := NewActorManager(context.Background(), spec, graph, false, sess)
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func testSpec() *workflow.WorkflowSpec {
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"C"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	spec.StreamSourceGroups = map[string][][]string{"C": {{"A-stream"}}}
	return spec
}

// 讨论点 11：构造期反向图 + 源键收集 + 默认 source_groups 单源排序拆分
func TestNewActorManager_反向图与源键(t *testing.T) {
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"C"}, "B": {"C"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	graph := NewStreamGraph()
	mgr := buildTestManager(t, spec, graph, nil)
	if len(mgr.consumerDict["C"]) != 2 {
		t.Fatal("反向图 C 应有 A/B 两个 producer")
	}
	actor := mgr.streams["C"]
	if actor == nil || len(actor.processors) != 1 {
		t.Fatal("C 应有一个 COLLECT processor")
	}
	p := actor.processors[components.ComponentAbilityCollect]
	if len(p.sourceGroups) != 2 {
		t.Fatalf("默认应单拆为 2 组, got %d", len(p.sourceGroups))
	}
	if !p.sourceGroups[0]["A-stream"] && !p.sourceGroups[0]["B-stream"] {
		t.Fatal("源键应为 producerId-ABILITY 且排序")
	}
	if !mgr.producerAbilities["A"][components.ComponentAbilityStream] {
		t.Fatal("producerAbilities 应收集 A-stream")
	}
}

// 讨论点 7：consume 时 finished_stream_nodes 补发 end_message。
// 恢复场景：A 在上一轮已完成（无活跃记录），B 的首帧拉起消费者后，consume 补发 A 的 END。
func TestConsume_补发EndMessage(t *testing.T) {
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"C"}, "B": {"C"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	graph := NewStreamGraph()
	wait := make(chan struct{})
	st := &fakeSession{wf: map[string]any{"finished_stream_nodes": []any{"A"}}}
	// 模拟真实 Vertex 契约：StreamCall 内先注册 gen 树（Consume），后 close event
	//（D1 还原后 processor.run 启动于 event 放行，注册必须先行，否则帧被空转丢弃）
	var out map[string]any
	var consumeErr error
	schema := map[string]any{"a": "${A.answer}", "b": "${B.out}"}
	var am *ActorManager
	v := &fakeConsumer{handle: true, done: true, callWaitCh: wait, eventFn: func(e chan struct{}) {
		out, consumeErr = am.Consume(context.Background(), "C", components.ComponentAbilityCollect, schema, nil)
		close(e)
	}}
	graph.AddStreamConsumer(v, "C") // 必须先注册：NewActorManager 构造期读取 vertex
	defer close(wait)               // 测试结束时放行消费者执行体
	am = buildTestManager(t, spec, graph, st)
	// B 首帧拉起消费者（B 活跃；A 已完成但本轮无活跃记录）
	if err := am.Produce(context.Background(), "B",
		map[string]any{"out": "x"},
		components.ComponentAbilityStream, true); err != nil {
		t.Fatal(err)
	}
	p := am.streams["C"].processors[components.ComponentAbilityCollect]
	if consumeErr != nil || out == nil {
		t.Fatalf("应返回 gen 树, err=%v", consumeErr)
	}
	// 补发后 A 应登记活跃能力（Python consume:141 active_produce_ability）
	if !am.activeProducerIDs["A"][components.ComponentAbilityStream] {
		t.Fatal("补发 end_message 后应登记 A 活跃能力")
	}
	// B END 后全组满足 → closeAllQueues → A 路径也收到 EndFrame（不挂死）
	if err := am.EndMessage(context.Background(), "B", components.ComponentAbilityStream); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	qa := p.processorQueues["${A.answer}"][0]
	// A 路径可能在 B 数据帧时未被关（无数据记录），全组满足后 closeAllQueues 关全队列
	vv, ok := qa.get(context.Background())
	if !ok {
		t.Fatal("A 路径应收到 EndFrame（不无限挂起）")
	}
	if ef, isEF := vv.(*utils.EndFrame); !isEF {
		t.Fatalf("A 路径应收到 EndFrame, got %v", vv)
	} else if ef.Source != "A" && ef.Source != "B" {
		t.Fatalf("EndFrame.Source 异常, got %q", ef.Source)
	}
}

// produce 路由 + active_produce_ability + 首帧拉起消费者
func TestProduce_路由(t *testing.T) {
	spec := testSpec()
	graph := NewStreamGraph()
	v := &fakeConsumer{handle: true, eventFn: func(e chan struct{}) { close(e) }}
	graph.AddStreamConsumer(v, "C")
	am := buildTestManager(t, spec, graph, nil)
	err := am.Produce(context.Background(), "A",
		map[string]any{"A": map[string]any{"answer": "x"}},
		components.ComponentAbilityStream, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(am.activeProducerIDs["A"]) == 0 {
		t.Fatal("produce 应登记活跃能力")
	}
	// D1 还原后（8.11 前置）：fake 立即 close event → Send 拉起任务并启动全部 processor.run；
	// 帧广播后 run 已消费主队列（此时无注册订阅者，值按 Python 语义被丢弃、主队列清空）。
	// 真实 Vertex 在 Consume 注册完成后才 close event，故订阅路径不丢帧。
	time.Sleep(30 * time.Millisecond) // 等 run 循环拉取
	p := am.streams["C"].processors[components.ComponentAbilityCollect]
	if p.queue.len() != 0 {
		t.Fatalf("run 已启动应消费主队列, got %d", p.queue.len())
	}
}

// 无消费者时丢弃不报错
func TestProduce_无消费者丢弃(t *testing.T) {
	spec := testSpec()
	am := buildTestManager(t, spec, NewStreamGraph(), nil)
	if err := am.Produce(context.Background(), "Z", map[string]any{"Z": "x"},
		components.ComponentAbilityStream, false); err != nil {
		t.Fatal(err)
	}
}

// shouldSanitizeStreamSource 三分支（讨论点 5）
func TestShouldSanitizeStreamSource_三分支(t *testing.T) {
	spec := testSpec()
	am := buildTestManager(t, spec, NewStreamGraph(), nil)
	// 无匹配组 → true
	if !am.ShouldSanitizeStreamSource("C", "Z") {
		t.Fatal("无匹配组应 sanitize")
	}
	// 单源组=AND → false（可能只是晚到）
	if am.ShouldSanitizeStreamSource("C", "A") {
		t.Fatal("单源组不应 sanitize")
	}
	// 多源组内有活跃替代源 → true
	spec2 := workflow.NewWorkflowSpec()
	spec2.StreamEdges = map[string][]string{"A": {"C"}, "B": {"C"}}
	spec2.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	spec2.StreamSourceGroups = map[string][][]string{"C": {{"A-stream", "B-stream"}}}
	am2 := buildTestManager(t, spec2, NewStreamGraph(), nil)
	if am2.ShouldSanitizeStreamSource("C", "A") {
		t.Fatal("多源组无活跃替代源不应 sanitize")
	}
	am2.ActiveProduceAbility("B", components.ComponentAbilityStream)
	if !am2.ShouldSanitizeStreamSource("C", "A") {
		t.Fatal("组内有活跃替代源应 sanitize")
	}
}

// subWorkflowStream 非子图报错；子图返回队列
func TestSubWorkflowStream_非子图报错(t *testing.T) {
	spec := testSpec()
	am := buildTestManager(t, spec, NewStreamGraph(), nil)
	_, err := am.SubWorkflowStream()
	if err == nil {
		t.Fatal("非子图应报错（对齐 Python build_error）")
	}
	// 错误码断言：对齐 Python StatusCode.GRAPH_STREAM_ACTOR_EXECUTION_ERROR（复刻 Python 字面量 112030）
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("应为结构化 BaseError, got %T", err)
	}
	if baseErr.Code() != 112030 {
		t.Fatalf("错误码应为 112030, got %d", baseErr.Code())
	}
	// 子图
	subMgr, err := NewActorManager(context.Background(), spec, NewStreamGraph(), true,
		&fakeSession{wf: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	q, err := subMgr.SubWorkflowStream()
	if err != nil || q == nil {
		t.Fatal("子图应返回队列")
	}
}

// timeout 从 session env 读取（GetEnvFloat）
func TestNewActorManager_EnvTimeout(t *testing.T) {
	spec := testSpec()
	graph := NewStreamGraph()
	v := &fakeConsumer{handle: true, eventFn: func(e chan struct{}) { close(e) }}
	graph.AddStreamConsumer(v, "C")
	sess := &fakeSession{wf: map[string]any{}, env: map[string]float64{
		"_stream_input_generator_timeout": 0.05,
	}}
	am := buildTestManager(t, spec, graph, sess)
	p := am.streams["C"].processors[components.ComponentAbilityCollect]
	// 未声明源路径应限时 50ms
	sr := p.createGenerator("x", "unknown.foo", nil)
	if sr.timeout != 50*time.Millisecond {
		t.Fatalf("env timeout 应生效, got %v", sr.timeout)
	}
}

// MarkProducerDone 幂等写回
func TestMarkProducerDone_幂等(t *testing.T) {
	spec := testSpec()
	st := &fakeSession{wf: map[string]any{}}
	am := buildTestManager(t, spec, NewStreamGraph(), st)
	am.MarkProducerDone("A")
	am.MarkProducerDone("A")
	am.MarkProducerDone("B")
	nodes := st.wf["finished_stream_nodes"].([]string)
	if len(nodes) != 2 || nodes[0] != "A" || nodes[1] != "B" {
		t.Fatalf("finished_stream_nodes 应为 [A B], got %v", nodes)
	}
}

// GetByDefaultTransformer/GetByDefinedTransformer
func TestStreamTransform_两种变换(t *testing.T) {
	tr := &StreamTransform{}
	out := tr.GetByDefaultTransformer(map[string]any{"A": map[string]any{"answer": "x"}},
		map[string]any{"k": "${A.answer}"})
	if out["k"] != "x" {
		t.Fatalf("default transformer 取值失败, got %v", out)
	}
	out2 := tr.GetByDefinedTransformer(map[string]any{"raw": 1},
		func(m map[string]any) map[string]any { return map[string]any{"wrapped": m} })
	if out2["wrapped"] == nil {
		t.Fatal("defined transformer 应直调")
	}
}
