# 8.12 StreamActor 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `openjiuwen/core/graph/stream_actor/`（base.py+manager.py），落地 workflow 流式输出的数据面中枢，并回填 session 的 ActorManager any 占位。

**Architecture:** goroutine+chan 直译 asyncio；主/路径队列用带锁无界内存队列（D2，保持 send 永不阻塞）；processor.run 启动点从 send 首帧挪至 consume 注册后（D1，用户拍板，注册串行化免竞态）；所有阻塞点 select ctx/timeout 保证可取消。session/internal 直接 import `*stream_actor.ActorManager` 具体类型（依赖方向已验证无环）。

**Tech Stack:** Go 1.x + sync/context + 既有件：`common/utils`（dict）、`session/utils`（path/ref）、`session/state`、`session/stream.StreamQueue`、`workflow/components.ComponentAbility`。

**Spec:** docs/superpowers/specs/2026-11-19-stream-actor-8.12-design.md（偏差 D1-D6 与 §7 的 11 项讨论点测试清单为准绳）

**包依赖图（已验证无环）：**
```
stream_actor → workflow/components, workflow(前置件), session/utils, session/constants, session/state, common/utils, common/logger
session/internal → stream_actor（回填方向）
```

---

### Task 1: 前置件 WorkflowSpec（偏差 D6）

**Files:**
- Create: `internal/agentcore/workflow/workflow_config.go`（package workflow）
- Create: `internal/agentcore/workflow/workflow_config_test.go`
- Modify: `internal/agentcore/workflow/doc.go`（文件树加条目）

- [ ] **Step 1: 写失败测试**

```go
package workflow

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// TestNewWorkflowSpec_默认值 对齐 Python pydantic default_factory=dict
func TestNewWorkflowSpec_默认值(t *testing.T) {
	spec := NewWorkflowSpec()
	if spec.Edges == nil || spec.StreamEdges == nil || spec.CompConfigs == nil ||
		spec.StreamSourceGroups == nil || spec.StartNodes == nil {
		t.Fatal("默认字段应初始化为空容器（default_factory=dict/list）")
	}
	if len(spec.StreamEdges) != 0 {
		t.Fatal("StreamEdges 应为空")
	}
}

// TestNodeSpec_默认值 对齐 Python NodeSpec Field 默认值
func TestNodeSpec_默认值(t *testing.T) {
	spec := NewNodeSpec()
	if spec.Abilities != nil && len(spec.Abilities) != 0 {
		t.Fatal("Abilities 默认应为空列表")
	}
	if spec.MaxRetries != 0 {
		t.Fatal("MaxRetries 默认 0")
	}
	if spec.Timeout != -1.0 {
		t.Fatal("Timeout 默认 -1.0（<=0 无超时）")
	}
}

// TestNodeSpec_Abilities字段 供 ActorManager 消费
func TestNodeSpec_Abilities字段(t *testing.T) {
	spec := NewNodeSpec()
	spec.Abilities = []components.ComponentAbility{components.ComponentAbilityCollect, components.ComponentAbilityTransform}
	if len(spec.Abilities) != 2 || spec.Abilities[0] != components.ComponentAbilityCollect {
		t.Fatal("Abilities 读写失败")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/workflow/ -run TestNewWorkflowSpec -v`
Expected: FAIL `undefined: NewWorkflowSpec`

- [ ] **Step 3: 实现 workflow_config.go**（一比一映射 workflow_config.py，WorkflowConfig 因依赖 8.16 WorkflowCard 裁剪至 8.15，D6）

```go
package workflow

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// CompIOConfig 组件输入/输出配置。
// Python: workflow_config.py CompIOConfig；inputs/outputs_schema 可为 dict 或变换函数（Transformer），
// Go 用 any 承载（map[string]any 或 MessageTransformer，MessageTransformer 定义见 session/utils）。
type CompIOConfig struct {
	// InputsSchema 输入 schema（map[string]any 或变换函数）
	InputsSchema any
	// OutputsSchema 输出 schema（map[string]any 或变换函数）
	OutputsSchema any
}

// ExceptionConfig 节点异常处理配置（Python extra="allow" 的自由字段 Go 用 map 承载）。
type ExceptionConfig struct {
	// HandleType 处理类型，默认 "interrupt"
	HandleType string
	// Extra 附加字段（Python model_config extra=allow）
	Extra map[string]any
}

// NodeSpec 工作流节点/组件规格。
// Python: workflow_config.py NodeSpec。
type NodeSpec struct {
	// IOConfigs 常规（非流式）I/O 配置
	IOConfigs *CompIOConfig
	// StreamIOConfigs 流式 I/O 配置
	StreamIOConfigs *CompIOConfig
	// Abilities 组件支持的能力列表（default_factory=list）
	Abilities []components.ComponentAbility
	// MaxRetries 最大重试次数（ge=0）
	MaxRetries int
	// Timeout 节点执行超时秒数；<=0 表示不超时
	Timeout float64
	// ExceptionConfig 异常恢复配置
	ExceptionConfig *ExceptionConfig
	// Name 人类可读组件名；空串在 trace 时回退 comp_id（Python Optional[str]，偏差：nil→空串）
	Name string
}

// WorkflowSpec 工作流结构完整规格。
// Python: workflow_config.py WorkflowSpec。
type WorkflowSpec struct {
	// Edges 常规数据流边（source -> [targets]）
	Edges map[string][]string
	// StreamEdges 流式数据流边（source -> [targets]）
	StreamEdges map[string][]string
	// CompConfigs 每个组件的配置
	CompConfigs map[string]*NodeSpec
	// StreamSourceGroups 流式消费者的 CNF 源组，键为 producer_id-ABILITY
	StreamSourceGroups map[string][][]string
	// StartNodes 起始节点
	StartNodes []string
}

// NewNodeSpec 创建带 Python 默认值的 NodeSpec
func NewNodeSpec() *NodeSpec {
	return &NodeSpec{
		Abilities: []components.ComponentAbility{},
		Timeout:   -1.0,
	}
}

// NewWorkflowSpec 创建带 Python 默认值的 WorkflowSpec（default_factory=dict/list）
func NewWorkflowSpec() *WorkflowSpec {
	return &WorkflowSpec{
		Edges:              map[string][]string{},
		StreamEdges:        map[string][]string{},
		CompConfigs:        map[string]*NodeSpec{},
		StreamSourceGroups: map[string][][]string{},
		StartNodes:         []string{},
	}
}
```

同步在 doc.go 文件树加：

```
//	├── workflow_config.go   # WorkflowSpec/NodeSpec/CompIOConfig/ExceptionConfig（8.12 前置件 D6，8.15 完整同步）
```

- [ ] **Step 4: 跑测试通过 + 编译**

Run: `go build ./internal/agentcore/workflow/... && go test ./internal/agentcore/workflow/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/workflow/workflow_config.go internal/agentcore/workflow/workflow_config_test.go internal/agentcore/workflow/doc.go
git commit -m "feat(workflow): 8.12 前置件 WorkflowSpec/NodeSpec（D6，8.15 ⤴️ 完整同步）"
```

---

### Task 2: 工具件导出与 EndFrame 回填

**Files:**
- Modify: `internal/common/utils/dict.go`（formatPath → FormatPath，更新内部调用点）
- Modify: `internal/agentcore/session/state/utils.go`（新增导出包装 GetBySchemaMap）
- Modify: `internal/agentcore/session/utils/utils.go` 或新建 `internal/agentcore/session/utils/frame.go`（EndFrame + MessageTransformer）
- Create: `internal/agentcore/session/utils/frame_test.go`

- [ ] **Step 1: 写失败测试** `internal/agentcore/session/utils/frame_test.go`

```go
package utils

import "testing"

// TestEndFrame_字段 对齐 Python session/utils.py:391 @dataclass EndFrame{source: str}
func TestEndFrame_字段(t *testing.T) {
	f := &EndFrame{Source: "nodeA"}
	if f.Source != "nodeA" {
		t.Fatal("EndFrame.Source 应保留 producer id")
	}
}

// TestIsEndFrame_判定 PathQueue 中区分普通帧与结束帧
func TestIsEndFrame_判定(t *testing.T) {
	if IsEndFrame("normal") {
		t.Fatal("普通值不应判定为 EndFrame")
	}
	if !IsEndFrame(&EndFrame{Source: "a"}) {
		t.Fatal("*EndFrame 应判定为结束帧")
	}
}

// TestMessageTransformer_签名 D4：修正 Python 注解错误，真实语义为消息变换
func TestMessageTransformer_签名(t *testing.T) {
	var tr MessageTransformer = func(m map[string]any) map[string]any {
		return map[string]any{"k": m["v"]}
	}
	got := tr(map[string]any{"v": 1})
	if got["k"] != 1 {
		t.Fatal("MessageTransformer 应可调用变换消息")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/session/utils/ -run TestEndFrame -v`
Expected: FAIL `undefined: EndFrame`

- [ ] **Step 3: 实现** 新建 `internal/agentcore/session/utils/frame.go`：

```go
package utils

// EndFrame 流结束帧，携带结束的 producer id。
// Python: openjiuwen/core/session/utils.py:391 @dataclass EndFrame{source: str}
// Frame = Union[Any, EndFrame]（Python 侧类型别名，Go 以 any 承载帧值）
type EndFrame struct {
	// Source 结束的 producer id
	Source string
}

// IsEndFrame 判断帧值是否为结束帧
func IsEndFrame(v any) bool {
	_, ok := v.(*EndFrame)
	return ok
}

// MessageTransformer 消息变换函数。
// 偏差 D4：Python session/state/base.py:22 的 Transformer 注解为 Callable[[ReadableStateLike], Any]，
// 与 stream_actor 运行时用法（transformer(origin_message)）不符，Go 按真实语义定义。
type MessageTransformer func(map[string]any) map[string]any
```

同批改 `internal/common/utils/dict.go`：`func formatPath(path []string) string` 改名导出 `FormatPath`（签名不变），并用 Grep 找出仓内 `formatPath(` 调用点（仅 dict.go 内部 ValidateArgs 等）同步改名；`grep -rn "formatPath(" internal/` 确认 0 残留。

同批在 `internal/agentcore/session/state/utils.go` 导出区加：

```go
// GetBySchemaMap 按 schema（map 形态）从数据取值。
// Python: session/utils.py get_by_schema(stream_inputs_schema, origin_message)
// 供 stream_actor manager 的 get_by_default_transformer 使用（偏差 D6 范围内的最小导出面）。
func GetBySchemaMap(schema map[string]any, data map[string]any) map[string]any {
	return getBySchemaMap(schema, data)
}
```

- [ ] **Step 4: 跑测试通过**

Run: `go build ./... && go test ./internal/agentcore/session/utils/ ./internal/common/utils/ ./internal/agentcore/session/state/ -v`
Expected: PASS（全量编译含改名无残留）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/utils/frame.go internal/agentcore/session/utils/frame_test.go internal/common/utils/dict.go internal/agentcore/session/state/utils.go
git commit -m "feat(session): 8.12 回填 EndFrame 多源场景（utils doc.go ⤵️ 兑现）+ FormatPath/GetBySchemaMap 导出 + MessageTransformer(D4)"
```

---

### Task 3: stream_actor 包骨架：类型、无界队列、帧工具

**Files:**
- Create: `internal/agentcore/graph/stream_actor/doc.go`
- Create: `internal/agentcore/graph/stream_actor/base.go`
- Create: `internal/agentcore/graph/stream_actor/base_test.go`

- [ ] **Step 1: 写失败测试** `base_test.go`

```go
package stream_actor

import (
	"context"
	"testing"
	"time"
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

// TestPayloadQueue_关闭 Get 在 Close 后立即返回 false
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

// TestGetProducerID 对齐 Python _get_producer_id：单键 dict 取首键，否则 ValueError
func TestGetProducerID(t *testing.T) {
	if got := getProducerID(map[string]any{"nodeA": "x"}); got != "nodeA" {
		t.Fatalf("got %q", got)
	}
	if func() (panicked bool) {
		defer func() { recover(); panicked = true }()
		getProducerID(map[string]any{"a": 1, "b": 2})
		return false
	}() {
		t.Log("panic expected")
	} else {
		t.Fatal("双键 message 应 panic 对齐 Python ValueError")
	}
}

// TestIsEndMessage 对齐 Python _is_end_message：内容 END_ 前缀判定（D5）
func TestIsEndMessage(t *testing.T) {
	if !isEndMessage(map[string]any{"nodeA": "END_nodeA"}) {
		t.Fatal("END_ 前缀应判定为结束帧")
	}
	if isEndMessage(map[string]any{"nodeA": "hello"}) {
		t.Fatal("普通内容不应判定为结束帧")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/stream_actor/ -v`
Expected: FAIL（包未建/符号未定义）

- [ ] **Step 3: 实现 doc.go + base.go 的基础部分**

`doc.go`（按项目规则 1）：

```go
// Package stream_actor 提供 workflow 流式输出的数据面中枢：把上游节点的过程性流式 chunk
// 按 stream_edges 路由给下游消费者的每条输入路径（schema 叶子路径），并按 source_groups
// 的 CNF 语义关流。
//
// 与 Pregel 控制面（loop/channel/routers）并行：控制面传图调度消息，本包传流式内容帧，
// 两者仅在 Vertex（8.11）执行体内部交汇。对应 Python: openjiuwen/core/graph/stream_actor/。
//
// 文件目录：
//
//	stream_actor/
//	├── doc.go           # 包文档
//	├── base.go         # StreamConsumer/StreamGraph/StreamPayload/StreamActor/StreamProcessor/帧工具/无界队列
//	└── manager.go      # StreamTransform/ActorManager/buildReverseGraph
//
// 并发模型（偏差 D1-D3，见 spec §4）：goroutine+chan 直译 asyncio；主/路径队列为带锁无界
// 内存队列（asyncio.Queue 无界语义，D2）；processor.run 启动点在 consume 注册之后（D1）；
// 所有阻塞点 select ctx/timeout 保证可取消。
package stream_actor
```

`base.go` 基础部分（结构体区块）：

```go
package stream_actor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

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
	// timeout 路径拉数超时；<=0 表示不超时（Python stream_generator_timeout）
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
	// timeout 拉数超时；未声明源的路径才限时（_path_has_declared_source），<=0 不超时
	timeout time.Duration
	// nodeID 所属消费者节点 ID（日志用）
	nodeID string
}

// payloadQueue 带锁无界内存队列（偏差 D2：Go chan 无法无界，等价实现 asyncio.Queue）。
// put 永不阻塞；get 支持 ctx 取消与 close。
type payloadQueue struct {
	mu     sync.Mutex
	cond   chan struct{} // 广播信号：每次 put/close 换新并关闭旧
	items  []any
	closed bool
}
```

常量、导出函数、非导出函数区块（同文件后部）：

```go
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

// Next 拉取下一帧。返回 (值, true)；EndFrame/超时/ctx 取消返回 (nil, false)。
// 对齐 Python base.py:271-306 generator() 内循环。
func (r *StreamReader) Next(ctx context.Context) (any, bool) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 0 // select 分支禁用
	}
	var timer <-chan time.Time
	if timeout > 0 {
		timer = time.After(timeout)
	}
	select {
	case <-ctx.Done():
		return nil, false
	case <-timer:
		logger.Warn(logComponent).
			Str("event_type", "GRAPH_RECEIVE_STREAM_CHUNK").
			Str("node_id", r.nodeID).
			Str("k_path", r.kPath).
			Str("r_path", r.rPath).
			Msgf("Receive chunk timeout %s of [%s.%s]", timeout, r.nodeID, r.kPath)
		return nil, false
	default:
	}
	v, ok := r.q.get(ctx)
	if !ok {
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

// getProducerID 从单键帧取 producer id；非 dict 或键数 !=1 时 panic（对齐 Python ValueError）。
// Python: base.py _get_producer_id。
func getProducerID(message map[string]any) string {
	if message == nil || len(message) != 1 {
		panic(fmt.Sprintf("message is invalid"))
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
	return ok && len(content) >= 4 && content[:4] == "END_"
}
```

⚠️ 实施注意：`logger`/`logComponent` 沿用仓惯例——文件内 `const logComponent = logger.ComponentAgentCore`（pregel/task.go:85 同款），import `"github.com/uapclaw/uapclaw-go/internal/common/logger"`；event_type 用 `.Str("event_type", "...")` 字段（engine.go:207 惯例）。

- [ ] **Step 4: 跑测试通过**

Run: `go test ./internal/agentcore/graph/stream_actor/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/stream_actor/
git commit -m "feat(graph): 8.12 stream_actor 包骨架（StreamConsumer/StreamGraph/无界队列/帧工具）"
```

---

### Task 4: StreamProcessor（run 分发循环 + generator 注册 + 关流判定）

**Files:**
- Modify: `internal/agentcore/graph/stream_actor/base.go`（追加 StreamProcessor 方法）
- Modify: `internal/agentcore/graph/stream_actor/base_test.go`

- [ ] **Step 1: 写失败测试**（覆盖讨论点 2/4/5/6/8/10）

```go
package stream_actor

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

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
	go p.run(ctx, components.ComponentAbilityCollect)
	time.Sleep(20 * time.Millisecond) // 等 run 启动（D1：run 先启动后投帧）
	p.receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "你"}}, SourceAbility: components.ComponentAbilityStream})
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
	p.receive(StreamPayload{Message: map[string]any{"A": map[string]any{"answer": "你"}}})
	p.processorQueues["A.answer"] = []*payloadQueue{newPayloadQueue()}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.run(ctx, components.ComponentAbilityCollect)
	v, ok := p.processorQueues["A.answer"][0].get(context.Background())
	if !ok || v != "你" {
		t.Fatalf("注册前帧应补投, got %v ok=%v", v, ok)
	}
}

// 讨论点 5：单源组=AND 必须等该源 end frame；多源组全到齐才关全流
func TestProcessorRun_多源组关流(t *testing.T) {
	p := newTestProcessor([]string{"A-stream", "B-stream"}, []string{"A-stream", "B-stream"})
	p.sources = map[string]bool{"A-stream": true, "B-stream": true}
	p.sourceIDs = map[string]bool{"A": true, "B": true}
	qa := newPayloadQueue()
	qb := newPayloadQueue()
	p.processorQueues["A.answer"] = []*payloadQueue{qa}
	p.processorQueues["B.results"] = []*payloadQueue{qb}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { p.run(ctx, components.ComponentAbilityCollect); close(done) }()
	time.Sleep(20 * time.Millisecond)
	// A 组内到齐：只关 A 路径，B 路径不动
	p.receive(StreamPayload{Message: map[string]any{"A": "END_A"}})
	if _, ok := qa.get(context.Background()); ok {
		t.Fatal("A 路径应收到 EndFrame")
	}
	if _, ok := qb.get(context.Background()); ok {
		t.Fatal("B 路径不应提前收到 EndFrame")
	}
	select {
	case <-done:
		t.Fatal("组未全到齐 run 不应退出")
	default:
	}
	// B 到齐：全组满足 → 关全队列 → run 退出
	p.receive(StreamPayload{Message: map[string]any{"B": "END_B"}})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("全组到齐后 run 应退出")
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
	go p.run(ctx, components.ComponentAbilityCollect)
	time.Sleep(20 * time.Millisecond)
	p.receive(StreamPayload{Message: map[string]any{"A": "END_A"}})
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
	// 声明源：timeout 生效前提是 _path_has_declared_source 为 false，声明源由 manager.generator 置 timeout=0
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
	p.sourceIDs = map[string]bool{"A": true}
	schema := map[string]any{
		"llm_text": "$A.answer",
		"search":   map[string]any{"hits": "$B.results"},
		"plain":    3,
	}
	out := p.generator(schema, nil)
	if _, ok := out["llm_text"].(*StreamReader); !ok {
		t.Fatal("llm_text 叶子应为 StreamReader")
	}
	sub, ok := out["search"].(map[string]any)
	if !ok {
		t.Fatal("search 应保持嵌套 map")
	}
	if _, ok := sub["hits"].(*StreamReader); !ok {
		t.Fatal("search.hits 叶子应为 StreamReader")
	}
	if out["plain"] != 3 {
		t.Fatal("非 $ref 叶子保持原值")
	}
	if len(p.processorQueues) != 2 {
		t.Fatalf("应注册 2 条路径, got %d", len(p.processorQueues))
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/stream_actor/ -run "TestProcessor|TestStreamReader" -v`
Expected: FAIL（方法未定义）

- [ ] **Step 3: 实现 StreamProcessor 方法**（追加到 base.go）

```go
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

// Run 常驻分发循环（偏差 D1：由 consume 注册完成后启动，非 send 首帧）。
// Python: base.py run(ability) 170-195。
func (p *StreamProcessor) Run(ctx context.Context, ability components.ComponentAbility) {
	handleMap := map[string]bool{}
	sourcePathMap := map[string]map[string]bool{}
	for {
		raw, ok := p.queue.get(ctx)
		if !ok {
			return // ctx 取消/shutdown
		}
		payload := raw.(StreamPayload)
		sourceKey := uniqueSourceKey(payload)
		if isEndMessage(payload.Message) {
			sourceID := getProducerID(payload.Message)
			handleMap[sourceKey] = true
			p.closeQueuesForSourceKey(ctx, sourceID, sourceKey, sourcePathMap)
			if p.allSourceGroupsFinished(handleMap) {
				p.closeAllQueues(ctx, sourceID)
			}
		} else {
			p.closeInactiveGroupSources(ctx, sourceKey)
			p.mu.Lock()
			for path, queues := range p.processorQueues {
				originPath := utils.ExtractOriginKey(path)
				value := sutils.GetValueByNestedPath(originPath, payload.Message)
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
```

（`sutils` 为 `session/utils` import 别名，避免与本包 utils 混淆；按 9.3 一比一保留 Python 步骤边界。）其余非导出方法一比一映射 base.py:197-243：

```go
// allSourceGroups_finished 组全到齐判定（Python:197-200，空组恒 false）
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
func (p *StreamProcessor) closeInactiveGroupSources(ctx context.Context, activeKey string) {
	for _, group := range p.sourceGroups {
		if !group[activeKey] || len(group) <= 1 {
			continue
		}
		for k := range group {
			if k != activeKey {
				p.closeQueuesForSource(ctx, producerIDFromSourceKey(k))
			}
		}
	}
}

// closeQueuesForSource 按源 id 关其名下路径队列（Python:209-213，put EndFrame(source_id)）
func (p *StreamProcessor) closeQueuesForSource(ctx context.Context, sourceID string) {
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
func (p *StreamProcessor) closeQueuesForSourceKey(ctx context.Context, sourceID, sourceKey string, sourcePathMap map[string]map[string]bool) {
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
func (p *StreamProcessor) closeAllQueues(ctx context.Context, sourceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, queues := range p.processorQueues {
		for _, q := range queues {
			q.put(&utils.EndFrame{Source: sourceID})
		}
	}
}

// isValueFromSource 路径是否属于源（path == id 或 path 以 "id." 开头）（Python:231-233）
func isValueFromSource(path, sourceID string) bool {
	return path == sourceID || len(path) > len(sourceID) && path[:len(sourceID)+1] == sourceID+"."
}

// uniqueSourceKey 源键 = producerId-ABILITY（Python:236-239）
func uniqueSourceKey(payload StreamPayload) string {
	return getProducerID(payload.Message) + "-" + payload.SourceAbility.Name()
}

// producerIDFromSourceKey 源键反解 producer id（rsplit("-",1)[0]，Python:241-243）
func producerIDFromSourceKey(sourceKey string) string {
	for i := len(sourceKey) - 1; i >= 0; i-- {
		if sourceKey[i] == '-' {
			return sourceKey[:i]
		}
	}
	return sourceKey
}

// Generator 按 schema 叶子路径建 gen 树（Python:248-260）。
// $ref 叶子建 StreamReader 注册路径队列；非 $ref 叶子原样保留；rebuild_dict 还原形状。
func (p *StreamProcessor) Generator(schema map[string]any,
	streamCallback func(map[string]any)) map[string]any {
	if len(schema) == 0 {
		return map[string]any{}
	}
	leaves := cutils.ExtractLeafNodes(schema)
	type kv struct {
		path []string
		val  any
	}
	inputs := make([]kv, 0, len(leaves))
	for _, leaf := range leaves {
		pathStr := cutils.FormatPath(leaf.Path)
		refStr, ok := leaf.Value.(string)
		if !ok || !strings.Contains(refStr, "$") {
			inputs = append(inputs, kv{leaf.Path, leaf.Value})
			continue
		}
		inputs = append(inputs, kv{leaf.Path, p.createGenerator(pathStr, refStr, streamCallback)})
	}
	return cutils.RebuildDictMap(inputs)
}
```

⚠️ 实施时核对 `common/utils.ExtractLeafNodes`/`RebuildDict` 真实签名（dict.go:84/123：`ExtractLeafNodes(data any, currentPath ...string) []LeafNode`、`RebuildDict(pairs []LeafNode) map[string]any`、LeafNode 结构体字段名按实际源码），`FormatPath` 按 Task 2 导出后的真实签名适配；上面代码中的 `cutils.RebuildDictMap`/`leaf.Path`/`leaf.Value` 为占位示意，**以实际签名为准改写**，不改语义。

`createGenerator`（Python:262-313）：

```go
// createGenerator 建路径队列并注册，返回 StreamReader（Python async generator 的 Go 等价物）。
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
```

（`StreamReader` 增加私有字段 `callback func(map[string]any)`：Task 3 的 Next 成功返回前若 callback 非 nil，先 `callback(map[string]any{kPath: v})` 再返回——对齐 Python:302-303 stream_callable 在 yield 后调用。）

- [ ] **Step 4: 跑测试通过 + -race**

Run: `go test -race ./internal/agentcore/graph/stream_actor/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/stream_actor/
git commit -m "feat(graph): 8.12 StreamProcessor 分发循环/generator/关流判定（讨论点2/4/5/6/8/10）"
```

---

### Task 5: StreamActor（send 首帧拉起 / 错误拦截 / shutdown）

**Files:**
- Modify: `internal/agentcore/graph/stream_actor/base.go`
- Modify: `internal/agentcore/graph/stream_actor/base_test.go`

- [ ] **Step 1: 写失败测试**（覆盖讨论点 3/9）

```go
package stream_actor

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
		<-f.callWaitCh
	}
}
func (f *fakeConsumer) ShouldHandleMessage() bool { return f.handle }
func (f *fakeConsumer) IsDone() bool              { return f.done }

func newTestActor(vertex *fakeConsumer, abilities ...int) *StreamActor // 实现见 Step 3 注

// 讨论点 3：首帧拉起消费者、等 event 置位、未 handle 丢弃
func TestActorSend_首帧拉起(t *testing.T) {
	eventClosed := make(chan struct{})
	v := &fakeConsumer{handle: true, eventFn: func(e chan struct{}) { close(e); close(eventClosed) }}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	// 未启动时不 handle 直接丢弃（对齐 Python send 第一分支）
	vNoHandle := &fakeConsumer{handle: false}
	a2 := NewStreamActor("C2", vNoHandle, nil, [][]string{{"A-stream"}}, -1)
	a2.Send(ctx, map[string]any{"A": "x"}, 0, false, "A") // 0=Stream，ability 广播不看匹配
	if a2.taskCtx != nil {
		t.Fatal("未 handle 不应拉起任务")
	}
	// 首帧拉起
	if err := actor.Send(ctx, map[string]any{"A": map[string]any{"answer": "你"}}, 0, true, "A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-eventClosed:
	case <-time.After(time.Second):
		t.Fatal("应等待 event 置位")
	}
}

// 讨论点 3：非首帧且任务已退出 → 丢弃（vertex is done）
func TestActorSend_任务已结束丢弃(t *testing.T) {
	v := &fakeConsumer{handle: true, done: true}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	if err := actor.Send(ctx, map[string]any{"A": "x"}, 0, false, "A"); err != nil {
		t.Fatal(err)
	}
	if actor.taskCtx != nil {
		t.Fatal("非首帧且 vertex 已 done 不应拉起任务")
	}
}

// 讨论点 3：错误回调置 taskErr，后续帧被拦截
func TestActorSend_错误拦截(t *testing.T) {
	v := &fakeConsumer{handle: true, callErr: errors.New("boom"),
		eventFn: func(e chan struct{}) { close(e) }}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	if err := actor.Send(ctx, map[string]any{"A": "x"}, 0, true, "A"); err != nil {
		t.Fatal(err)
	}
	// 等 taskErr 被错误回调填充
	time.Sleep(30 * time.Millisecond)
	// 后续首帧：task_error 已有异常 → 丢弃并返回
	if err := actor.Send(ctx, map[string]any{"A": "y"}, 0, true, "A"); err == nil {
		t.Fatal("task_error 有异常时后续首帧应被拦截")
	}
}

// 讨论点 9：shutdown 级联取消（-race）
func TestActorShutdown_级联取消(t *testing.T) {
	wait := make(chan struct{})
	v := &fakeConsumer{handle: true, callWaitCh: wait, eventFn: func(e chan struct{}) { close(e) }}
	actor := NewStreamActor("C", v, nil, [][]string{{"A-stream"}}, -1)
	ctx := context.Background()
	_ = actor.Send(ctx, map[string]any{"A": "x"}, 0, true, "A")
	p := NewStreamProcessor("C", [][]string{{"A-stream"}}, -1)
	actor.startProcessor(context.Background(), p) // consume 时由 manager 调
	if err := actor.Shutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wait:
		t.Fatal("shutdown 应取消消费者任务")
	default:
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/stream_actor/ -run TestActor -v`
Expected: FAIL

- [ ] **Step 3: 实现 StreamActor 方法**

```go
// NewStreamActor 构造（对齐 Python __init__：每 ability 建一个 processor；abilities 传 nil 时仅建消费者壳，
// processor 由 ActorManager 构造期按 Python 逻辑显式注入——测试场景直用 NewStreamProcessor）。
func NewStreamActor(nodeID string, vertex StreamConsumer, abilities []components.ComponentAbility,
	sourceGroups [][]string, timeout time.Duration) *StreamActor {
	ps := map[components.ComponentAbility]*StreamProcessor{}
	for _, ab := range abilities {
		ps[ab] = NewStreamProcessor(nodeID, sourceGroups, timeout)
	}
	return &StreamActor{processors: ps, vertex: vertex, nodeID: nodeID}
}

// Send 接收帧：未 handle 丢弃 → 任务未启动时首帧拉起（等 event）→ 广播全部 processor。
// Python: base.py send(64-104) 一比一；偏差 D1：此处不再启动 processor.run（挪至 consume 注册后）。
func (a *StreamActor) Send(ctx context.Context, message map[string]any,
	sourceAbility components.ComponentAbility, firstFrame bool, producerID string) error {
	// Python send 第一分支：未 handle 丢弃
	if !a.vertex.ShouldHandleMessage() {
		logger.Warn(logComponent).
			Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
			Str("node_id", a.nodeID).
			Str("producer_id", producerID).
			Msgf("Discard chunk send from [%s], %s[%s] unable to handle", producerID, a.nodeID, sourceAbility.Name())
		return nil
	}
	a.mu.Lock()
	needStart := a.taskCtx == nil || a.taskDoneClosed()
	if !needStart {
		a.mu.Unlock()
	} else {
		// task 已结束且带异常 → 拦截（Python send:76-85）
		if a.taskDoneClosed() && a.taskErr != nil {
			select {
			case err := <-a.taskErr:
				logger.Warn(logComponent).
					Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
					Str("node_id", a.nodeID).
					Err(err).
					Msgf("Discard chunk send from [%s], %s[%s] occur exception", producerID, a.nodeID, sourceAbility.Name())
				a.mu.Unlock()
				return err
			default:
			}
		}
		// 非首帧或 vertex 已 done → 丢弃（Python send:86-90）
		if !firstFrame || a.vertex.IsDone() {
			logger.Warn(logComponent).
				Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
				Str("node_id", a.nodeID).
				Msgf("Discard chunk send from [%s], %s[%s] vertex is done", producerID, a.nodeID, sourceAbility.Name())
			a.mu.Unlock()
			return nil
		}
		// 首帧拉起（Python send:91-100）
		event := make(chan struct{})
		a.taskErr = make(chan error, 1)
		taskCtx, cancel := context.WithCancel(ctx)
		a.taskCtx, a.taskCancel = taskCtx, cancel
		a.taskDone = make(chan struct{})
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
		a.mu.Unlock()
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
	// 广播全部 processor（Python send:101-103，不看 ability 匹配）
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
		Str("node_id", a.nodeID).
		Msgf("Send chunk from [%s] to %s[%s]", producerID, a.nodeID, sourceAbility.Name())
	for _, processor := range a.processors {
		processor.Receive(StreamPayload{Message: message, SourceAbility: sourceAbility})
	}
	return nil
}

// Generator 按 ability 取 processor 注册 gen 树（Python generator(105-108)）
func (a *StreamActor) Generator(ability components.ComponentAbility, schema map[string]any,
	streamCallback func(map[string]any)) map[string]any {
	return a.processors[ability].Generator(schema, streamCallback)
}

// startProcessor consume 注册完成后启动 processor.run（偏差 D1 新增挂载点，替代 Python send:98-100）
func (a *StreamActor) startProcessor(parent context.Context, p *StreamProcessor) {
	ctx, cancel := context.WithCancel(parent)
	a.mu.Lock()
	a.runningCancel = append(a.runningCancel, cancel)
	a.runningWg.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.runningWg.Done()
		p.Run(ctx, p.ability())
	}()
}

// Shutdown 级联取消：消费者任务 → taskErr → 全部 processor.run（Python:114-157 一比一，含日志）。
func (a *StreamActor) Shutdown() error { /* 按 Python 步骤：cancel task await → cancel taskErr → gather running_tasks → finally 清空 */ ... }
```

⚠️ 实施细节：
- `taskDoneClosed()`：`select { case <-a.taskDone: return true; default: return false }`。
- `p.ability()`：StreamProcessor 增加私有字段 `ability components.ComponentAbility`（构造期填），仅供日志。
- `Shutdown()` 完整实现按 Python 步骤边界一比一：先 cancel taskCancel 并等 taskDone（带异常转发日志），再判 taskErr（收错误记日志），再遍历 runningCancel 逐个 cancel 后 `runningWg.Wait()`，finally 置空 taskCtx/taskDone/runningCancel。日志 Msg 英文化（feedback-logs-english）。
- 测试里的 `newTestActor` 辅助函数若未用到则删除（避免 unused 报错）。

- [ ] **Step 4: 跑测试通过 + -race**

Run: `go test -race ./internal/agentcore/graph/stream_actor/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/stream_actor/
git commit -m "feat(graph): 8.12 StreamActor send 首帧拉起/错误拦截/级联 shutdown（讨论点3/9，D1）"
```

---

### Task 6: ActorManager + StreamTransform（manager.py 一比一）

**Files:**
- Create: `internal/agentcore/graph/stream_actor/manager.go`
- Create: `internal/agentcore/graph/stream_actor/manager_test.go`

- [ ] **Step 1: 写失败测试**（覆盖讨论点 5/7/11 + produce 路由）

```go
package stream_actor

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

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

type fakeState struct {
	wf map[string]any
}

func (s *fakeState) getWorkflowState(key string) any { return s.wf[key] }
func (s *fakeState) updateAndCommitWorkflowState(data map[string]any) {
	for k, v := range data {
		s.wf[k] = v
	}
}

// 构造参数形状以实际 NewActorManager 签名为准：session 以最小接口注入（见 Step 3 注）

// 讨论点 11：构造期反向图 + 源键收集 + 默认 source_groups 单源排序拆分
func TestNewActorManager_反向图与源键(t *testing.T) {
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"C"}, "B": {"C"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	// 不声明 stream_source_groups → 默认每源单拆
	graph := NewStreamGraph()
	mgr := buildTestManager(t, spec, graph, nil)
	am := mgr.(*ActorManager)
	actor := am.streams["C"]
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
}

// 讨论点 7：consume 时 finished_stream_nodes 补发 end_message
func TestConsume_补发EndMessage(t *testing.T) {
	spec := testSpec()
	graph := NewStreamGraph()
	// 注册一个立即 event 的 fake consumer
	v := &fakeConsumer{handle: true, eventFn: func(e chan struct{}) { close(e) }}
	graph.AddStreamConsumer(v, "C")
	st := &fakeState{wf: map[string]any{"finished_stream_nodes": []any{"A"}}}
	mgr := buildTestManager(t, spec, graph, st)
	am := mgr.(*ActorManager)
	am.streams["C"] // 已构造
	p := am.streams["C"].processors[components.ComponentAbilityCollect]
	schema := map[string]any{"answer": "$A.answer"}
	out := am.Consume(context.Background(), "C", components.ComponentAbilityCollect, schema, nil)
	if out == nil {
		t.Fatal("应返回 gen 树")
	}
	// A 在 finished_stream_nodes 且无活跃能力 → 补发 END_A → 路径队列应收到 EndFrame
	time.Sleep(30 * time.Millisecond) // 等 run 循环补投
	q := p.processorQueues["A.answer"][0]
	vv, ok := q.get(context.Background())
	if !ok {
		t.Fatal("补发 end_message 后路径应收到 EndFrame")
	}
	if ef, isEF := vv.(*utils.EndFrame); !isEF || ef.Source != "A" {
		t.Fatalf("EndFrame.Source 应为 A, got %v", vv)
	}
}

// produce 路由 + active_produce_ability
func TestProduce_路由(t *testing.T) {
	spec := testSpec()
	graph := NewStreamGraph()
	v := &fakeConsumer{handle: true, eventFn: func(e chan struct{}) { close(e) }}
	graph.AddStreamConsumer(v, "C")
	mgr := buildTestManager(t, spec, graph, nil)
	am := mgr.(*ActorManager)
	err := am.Produce(context.Background(), "A", map[string]any{"A": map[string]any{"answer": "x"}},
		components.ComponentAbilityStream, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(am.activeProducerIDs["A"]) == 0 {
		t.Fatal("produce 应登记活跃能力")
	}
	// 消费者未启动前帧滞留主队列（D1）
	p := am.streams["C"].processors[components.ComponentAbilityCollect]
	if p.queue.len() != 1 {
		t.Fatalf("帧应滞留主队列, got %d", p.queue.len())
	}
}

// 无消费者时丢弃并记日志
func TestProduce_无消费者丢弃(t *testing.T) {
	spec := testSpec()
	graph := NewStreamGraph()
	mgr := buildTestManager(t, spec, graph, nil)
	am := mgr.(*ActorManager)
	if err := am.Produce(context.Background(), "Z", map[string]any{"Z": "x"},
		components.ComponentAbilityStream, false); err != nil {
		t.Fatal(err)
	}
}

// shouldSanitizeStreamSource 三分支（讨论点 5）
func TestShouldSanitizeStreamSource_三分支(t *testing.T) {
	spec := testSpec()
	graph := NewStreamGraph()
	mgr := buildTestManager(t, spec, graph, nil)
	am := mgr.(*ActorManager)
	// 无匹配组 → true
	if !am.ShouldSanitizeStreamSource("C", "Z") {
		t.Fatal("无匹配组应 sanitize")
	}
	// 单源组=AND → false（可能只是晚到）
	if am.ShouldSanitizeStreamSource("C", "A") {
		t.Fatal("单源组不应 sanitize")
	}
	// 组内有活跃替代源 → true
	am.ActiveProduceAbility("B", components.ComponentAbilityStream)
	spec2 := workflow.NewWorkflowSpec()
	spec2.StreamEdges = map[string][]string{"A": {"C"}, "B": {"C"}}
	spec2.CompConfigs = map[string]*workflow.NodeSpec{
		"A": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"B": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"C": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
	}
	spec2.StreamSourceGroups = map[string][][]string{"C": {{"A-stream", "B-stream"}}}
	graph2 := NewStreamGraph()
	mgr2 := buildTestManager(t, spec2, graph2, nil)
	am2 := mgr2.(*ActorManager)
	am2.ActiveProduceAbility("B", components.ComponentAbilityStream)
	if !am2.ShouldSanitizeStreamSource("C", "A") {
		t.Fatal("组内有活跃替代源应 sanitize")
	}
}

// subWorkflowStream 非子图报错
func TestSubWorkflowStream_非子图报错(t *testing.T) {
	spec := testSpec()
	mgr := buildTestManager(t, spec, NewStreamGraph(), nil)
	am := mgr.(*ActorManager)
	if _, err := am.SubWorkflowStream(); err == nil {
		t.Fatal("非子图应报错（对齐 Python build_error）")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/graph/stream_actor/ -run "TestNewActorManager|TestConsume|TestProduce|TestShouldSanitize|TestSubWorkflow" -v`
Expected: FAIL

- [ ] **Step 3: 实现 manager.go**

关键签名设计（Python 依赖 session 的三处解法）：
- `session`（workflow_session）参数：ActorManager 只用 `config().get_env(STREAM_INPUT_GEN_TIMEOUT_KEY)` 与 `state()` 读写 `finished_stream_nodes` —— 定义窄接口（规则 10 允许：本包是使用方）：

```go
// ActorSession ActorManager 对 session 的依赖面（窄接口，规则 10 使用方方向）。
// Python: manager.py __init__ 的 session 参数消费面。
type ActorSession interface {
	// GetEnvFloat 读 session env 浮点值（STREAM_INPUT_GEN_TIMEOUT_KEY）
	GetEnvFloat(key string) (float64, bool)
	// GetWorkflowStateValue 读工作流状态
	GetWorkflowStateValue(key string) any
	// UpdateAndCommitWorkflowState 更新并提交工作流状态
	UpdateAndCommitWorkflowState(data map[string]any)
}
```

```go
package stream_actor

import (
	"context"
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sutils "github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamTransform 流式变换器集合（Python manager.py StreamTransform，两 classmethod → 包内类型方法）。
type StreamTransform struct{}

// GetByDefinedTransformer 自定义变换函数直调（Python:19-21）
func (t *StreamTransform) GetByDefinedTransformer(originMessage map[string]any,
	tr sutils.MessageTransformer) map[string]any {
	return tr(originMessage)
}

// GetByDefaultTransformer 按 schema 默认取值（Python:23-25）
func (t *StreamTransform) GetByDefaultTransformer(originMessage map[string]any,
	schema map[string]any) map[string]any {
	return state.GetBySchemaMap(schema, originMessage)
}

// ActorManager 流式 actor 管理器（Python manager.py ActorManager 一比一）。
type ActorManager struct {
	streamEdges map[string][]string
	streams map[string]*StreamActor
	streamsTransform *StreamTransform
	activeProducerIDs map[string]map[components.ComponentAbility]bool
	consumerDict map[string][]string
	producerAbilities map[string]map[components.ComponentAbility]bool
	streamSourceGroups map[string][][]string
	workflowSession ActorSession
	subGraph bool
	subWorkflowStreamQ *stream.StreamQueue
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewActorManager 构造（Python __init__ 28-58 一比一：反向图、consumer abilities ∩ {COLLECT,TRANSFORM}
// 建 actor、producer abilities ∩ {STREAM,TRANSFORM} 收源键、默认源组单拆排序、sub_graph 建 AsyncStreamQueue）。
func NewActorManager(ctx context.Context, spec *workflow.WorkflowSpec, graph *StreamGraph,
	subGraph bool, session ActorSession) (*ActorManager, error) {
	timeout := -1.0
	if session != nil {
		if v, ok := session.GetEnvFloat(constants.StreamInputGenTimeoutKey); ok {
			timeout = v
		}
	}
	m := &ActorManager{
		streamEdges:        spec.StreamEdges,
		streams:            map[string]*StreamActor{},
		streamsTransform:   &StreamTransform{},
		activeProducerIDs:  map[string]map[components.ComponentAbility]bool{},
		consumerDict:       buildReverseGraph(spec.StreamEdges),
		producerAbilities:  map[string]map[components.ComponentAbility]bool{},
		streamSourceGroups: map[string][][]string{},
		workflowSession:    session,
		subGraph:           subGraph,
	}
	if subGraph {
		m.subWorkflowStreamQ = stream.NewStreamQueue(10 * 1024)
	}
	for consumerID, producerIDs := range m.consumerDict {
		consumerSpec := spec.CompConfigs[consumerID]
		var consumerAbilities []components.ComponentAbility
		if consumerSpec != nil {
			for _, ab := range consumerSpec.Abilities {
				if ab == components.ComponentAbilityCollect || ab == components.ComponentAbilityTransform {
					consumerAbilities = append(consumerAbilities, ab)
				}
			}
		}
		sources := map[string]bool{}
		for _, producerID := range producerIDs {
			abilities := m.producerAbilities[producerID]
			if abilities == nil {
				abilities = map[components.ComponentAbility]bool{}
			}
			producerSpec := spec.CompConfigs[producerID]
			if producerSpec != nil {
				for _, ab := range producerSpec.Abilities {
					if ab == components.ComponentAbilityStream || ab == components.ComponentAbilityTransform {
						abilities[ab] = true
						sources[producerID+"-"+ab.Name()] = true
					}
				}
			}
			m.producerAbilities[producerID] = abilities
		}
		sourceGroups := spec.StreamSourceGroups[consumerID]
		if len(sourceGroups) == 0 {
			keys := make([]string, 0, len(sources))
			for k := range sources {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sourceGroups = append(sourceGroups, []string{k})
			}
		}
		groups := make([][]string, 0, len(sourceGroups))
		for _, g := range sourceGroups {
			if len(g) > 0 {
				groups = append(groups, g)
			}
		}
		m.streams[consumerID] = NewStreamActor(consumerID, graph.GetNode(consumerID),
			consumerAbilities, groups, secondsToDuration(timeout))
	}
	return m, nil
}

// SubWorkflowStream 子图流队列；非子图报错（Python:60-64 build_error）。
func (m *ActorManager) SubWorkflowStream() (*stream.StreamQueue, error) {
	if !m.subGraph {
		return nil, fmt.Errorf("only sub graph has sub_workflow_stream")
	}
	return m.subWorkflowStreamQ, nil
}

// ActiveProduceAbility 登记活跃能力（Python:66-69）
func (m *ActorManager) ActiveProduceAbility(producerID string, ability components.ComponentAbility) {
	if m.activeProducerIDs[producerID] == nil {
		m.activeProducerIDs[producerID] = map[components.ComponentAbility]bool{}
	}
	m.activeProducerIDs[producerID][ability] = true
}

// MarkProducerDone 写回 finished_stream_nodes（Python:71-76，幂等 append）
func (m *ActorManager) MarkProducerDone(producerID string) {
	raw := m.workflowSession.GetWorkflowStateValue("finished_stream_nodes")
	nodes := toStringSlice(raw)
	found := false
	for _, n := range nodes {
		if n == producerID {
			found = true
			break
		}
	}
	if !found {
		nodes = append(nodes, producerID)
	}
	m.workflowSession.UpdateAndCommitWorkflowState(map[string]any{"finished_stream_nodes": nodes})
}

// ShouldSanitizeStreamSource 三分支判定（Python:78-97，含注释语义：单源组=AND 可能只是晚到）
func (m *ActorManager) ShouldSanitizeStreamSource(consumerID, producerID string) bool { /* 一比一 */ }

// StreamTransform 访问器（Python property stream_transform）
func (m *ActorManager) StreamTransform() *StreamTransform { return m.streamsTransform }

// Produce 路由帧到 stream_edges 指定的消费者（Python:106-120；无消费者记 debug 日志丢弃）
func (m *ActorManager) Produce(ctx context.Context, producerID string, messageContent map[string]any,
	ability components.ComponentAbility, firstFrame bool) error { /* 一比一 */ }

// EndMessage 发 END 哨兵（Python:122-124）
func (m *ActorManager) EndMessage(ctx context.Context, producerID string, ability components.ComponentAbility) error {
	return m.Produce(ctx, producerID, map[string]any{producerID: fmt.Sprintf("END_%s", producerID)}, ability, false)
}

// Consume 注册 gen 树并启动 processor.run（Python:126-142 + 偏差 D1 新启动点）。
// 尾部补发：finished_stream_nodes 中已完成的 producer 若无活跃能力 → EndMessage 补发（Python:131-141）。
func (m *ActorManager) Consume(ctx context.Context, consumerID string, ability components.ComponentAbility,
	schema map[string]any, streamCallback func(map[string]any)) (map[string]any, error) {
	actor := m.getActor(consumerID)
	consumeIter := actor.Generator(ability, schema, streamCallback)
	producerIDs := m.consumerDict[consumerID]
	finished := toStringSlice(m.workflowSession.GetWorkflowStateValue("finished_stream_nodes"))
	for _, producerID := range producerIDs {
		fin := false
		for _, n := range finished {
			if n == producerID {
				fin = true
				break
			}
		}
		if !fin {
			continue
		}
		allAbilities := m.producerAbilities[producerID]
		active := m.activeProducerIDs[producerID]
		if len(active) == 0 {
			for ab := range allAbilities {
				if err := m.EndMessage(ctx, producerID, ab); err != nil {
					return nil, err
				}
				m.ActiveProduceAbility(producerID, ab)
			}
		}
	}
	// 偏差 D1：注册完成后启动该 ability 的 processor.run（替代 Python send:98-100 的启动点）
	p := actor.processors[ability]
	if p != nil {
		actor.startProcessor(ctx, p)
	}
	return consumeIter, nil
}

// Shutdown 全 actor 级联关闭（Python:165-167）
func (m *ActorManager) Shutdown() error {
	for _, actor := range m.streams {
		if err := actor.Shutdown(); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getActor 取 actor（Python _get_actor）
func (m *ActorManager) getActor(consumerID string) *StreamActor { return m.streams[consumerID] }

// buildReverseGraph 反向图（Python manager.py:170-178 一比一）
func buildReverseGraph(graph map[string][]string) map[string][]string {
	rev := map[string][]string{}
	for source, targets := range graph {
		for _, target := range targets {
			rev[target] = append(rev[target], source)
		}
	}
	return rev
}

// secondsToDuration 负值→0（不超时）
func secondsToDuration(sec float64) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}

// toStringSlice / groupHasActiveAlternative / sourceKeyMatchesProducer / splitSourceKey（Python:144-163 一比一）
```

⚠️ 实施注意：
- `constants.StreamInputGenTimeoutKey` 来自 `internal/agentcore/session/constants`（已存在）。
- `stream.NewStreamQueue(10*1024)` 对齐 Python `AsyncStreamQueue(maxsize=10*1024)`；`stream` 包为 `internal/agentcore/session/stream`（queue.go 已有 Send/Receive/Close）。
- `state.GetBySchemaMap` 为 Task 2 导出的包装。
- `ShouldSanitizeStreamSource`/`Produce` 等标了「一比一」的方法按 Python 对应行号逐段复刻，勿合并步骤（规则 9.3）；`splitSourceKey` 反解能力名未知时返回 error（对齐 Python:163 ValueError）。
- `p.queue.len()` 测试辅助：payloadQueue 加 `func (q *payloadQueue) len() int`。
- `buildTestManager` 测试辅助：用 `fakeSession{...}` 实现 ActorSession 三方法后调 `NewActorManager`，失败 t.Fatal。`MarkProducerDone` 的 `workflowSession` 为 nil 时跳过（测试兼容），生产路径 session 恒非 nil。

- [ ] **Step 4: 跑测试通过 + -race**

Run: `go test -race ./internal/agentcore/graph/stream_actor/ -v -count=1`
Expected: PASS

- [ ] **Step 5: 覆盖率检查**

Run: `go test -cover ./internal/agentcore/graph/stream_actor/`
Expected: ≥85%

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/graph/stream_actor/manager.go internal/agentcore/graph/stream_actor/manager_test.go
git commit -m "feat(graph): 8.12 ActorManager/StreamTransform（反向图/produce/consume/sanitize/子图队列）"
```

---

### Task 7: session 回填（any → 具体类型 + Close 兑现）

**Files:**
- Modify: `internal/agentcore/session/interfaces/interfaces.go:45-47`
- Modify: `internal/agentcore/session/session.go`、`internal/agentcore/session/internal/workflow_session.go`、`internal/agentcore/session/internal/agent_session.go`、`internal/agentcore/session/internal/agent_team_session.go`（ActorManager 签名与字段类型化）
- Modify: 测试替身：`internal/agentcore/session/checkpointer/base_test.go:144`、`internal/agentcore/session/interaction/base_test.go`、`internal/agentcore/session/session_test.go`（签名同步）
- Modify: 各 `_test.go` 中断言文案不变，仅类型适配

- [ ] **Step 1: 接口收窄**

`interfaces.go`：

```go
	// ActorManager 获取 Actor 管理器（可选，默认返回 nil）
	// ✅ 8.12 已回填：any → *stream_actor.ActorManager
	ActorManager() *stream_actor.ActorManager
```

import `"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/stream_actor"`（已验证无环）。

- [ ] **Step 2: 全实现者替换（规则 10.3 精神：同改动改完）**

Run: `grep -rn "ActorManager() any" internal/`
逐文件改：`workflow_session.go` 三处方法 + `actorManager any` 字段 → `*stream_actor.ActorManager`；`agent_session.go`/`agent_team_session.go`/`session.go` 的实现签名同步；三个测试替身返回 `nil`。`NewSubWorkflowSession(nodeSession, workflowID, actorManager any)` → `*stream_actor.ActorManager`。

- [ ] **Step 3: Close 兑现 Shutdown**

`workflow_session.go`：

```go
// Close 关闭会话。如果 actorManager 不为 nil，调用其 Shutdown。
// ✅ 8.12 已回填：ActorManager 类型化后兑现 Shutdown 调用（Python internal/workflow.py:89-90）
func (s *WorkflowSession) Close() error {
	if s.actorManager != nil {
		return s.actorManager.Shutdown()
	}
	return nil
}
```

`SubWorkflowSession.Close()` 同款（对齐 Python internal/workflow.py:208-209）。

- [ ] **Step 4: 编译 + 测试**

Run: `go build ./... && go test ./internal/agentcore/session/... -count=1`
Expected: PASS（11 处 `ActorManager() any` grep 锚点清零：`grep -rn "ActorManager() any" internal/` 无输出）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/
git commit -m "feat(session): 8.12 回填 ActorManager 具体类型化 + Close 兑现 Shutdown（占位 ⤵️ 兑现）"
```

---

### Task 8: 集成用例（规则 7：真实跨 ≥3 子系统）

**Files:**
- Create: `tests/integration/stream_actor/suite_test.go`（`//go:build integration` + `suite.Run(`）

- [ ] **Step 1: 写集成用例**（覆盖讨论点 1/5/7/8/9 集成层）

场景：真实 `workflow.WorkflowSpec` + `session` 状态（`state.WorkflowCommitState`）+ `session/stream.StreamQueue` + `graph/stream_actor` 全链：

1. **跨子系统链路**：构造 WorkflowSpec（子系统 workflow）→ NewActorManager（stream_actor）→ session env 读 timeout（session/config）→ produce→consume 全链（stream_actor）→ finished_stream_nodes 写回 session state（session/state）。调用跨度 ≥3 子系统，真实覆盖。
2. **用例 1（讨论点 1）**：INVOKE 数据不经 actor——produce 只对 stream_edges 声明的边生效，未声明 producer 的帧不进任何队列。
3. **用例 2（讨论点 5+7）**：多源组 + finished_stream_nodes 恢复补发 end_message → generator 正常退出（不挂 timeout）。
4. **用例 3（讨论点 8+9）**：ctx 取消贯穿 produce 阻塞/consume 拉数/shutdown 级联，`-race` 干净。

测试骨架（完整断言按上述 4 条展开，每条 ≥2 断言）：

```go
//go:build integration

package stream_actor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

type StreamActorIntegrationSuite struct {
	suite.Suite
}

func (s *StreamActorIntegrationSuite) TestProduceConsume全链_跨子系统() {
	// workflow.WorkflowSpec 构造 → NewActorManager(session env/state) → Produce → Consume → state 写回
	// 断言：gen 树形状正确；帧经主队列→路径队列；MarkProducerDone 后 GetWorkflowStateValue 可读回
}

func (s *StreamActorIntegrationSuite) Test恢复场景_补发EndMessage_免超时退出() { /* 讨论点 7 */ }
func (s *StreamActorIntegrationSuite) TestCtx取消与Shutdown级联_race()        { /* 讨论点 8+9 */ }

func TestStreamActorIntegrationSuite(t *testing.T) {
	suite.Run(t, new(StreamActorIntegrationSuite))
}
```

- [ ] **Step 2: 本地自检审计门禁**

Run: `go run ./cmd/integration-audit -verify`
Expected: stream_actor 目录两层门禁通过（跨度 ≥3 子系统或覆盖交叉 ≥3 子系统各 ≥30 行）

- [ ] **Step 3: 跑集成测试（含 -race）**

Run: `go test -tags=integration -race ./tests/integration/stream_actor/ -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add tests/integration/stream_actor/
git commit -m "test(integration): 8.12 StreamActor 跨子系统集成用例（讨论点1/5/7/8/9）"
```

---

### Task 9: 全量回归 + 计划同步 + 收尾

- [ ] **Step 1: 全量回归（CI 同款标签）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 0 FAIL

- [ ] **Step 2: vet + build**

Run: `go build ./... && go vet ./...`
Expected: 无错误

- [ ] **Step 3: IMPLEMENTATION_PLAN.md 同步**

1. 8.12 行：`☐` → `✅`，追加 Go 事实（文件/覆盖率/偏差 D1-D6 归档 spec §6）；
2. 8.15 行加 ⤴️：「8.12 已前置落地 WorkflowSpec/NodeSpec 骨架（workflow_config.go，D6 裁剪 WorkflowConfig/NodeSpec.Name 等），实施时完整同步」；
3. 8.11 行加 ⤴️：「StreamConsumer/StreamGraph 已随 8.12 落地（stream_actor 包），实施时直接消费」；
4. 顺序注记追加「十二次记录」：方案 B 撤销，换回 8.12 → 8.11（依据本 spec §2.1）；
5. session 侧 `⤵️ 后续回填：any → ActorManager` 三处注记改「✅ 8.12 已回填」。

- [ ] **Step 4: 提交并推送**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 8.12 StreamActor 完成同步（8.11/8.15 ⤴️ + 十二次顺序记录）"
git push
```

---

## 删除清单（规则 8）

- `internal/agentcore/session/utils/doc.go` 中「EndFrame/Frame ⤵️ 延后到 8.x stream_actor 回填」注记 → 改「✅ 8.12 已回填」
- `session/internal/workflow_session.go` 三处 `⤵️ 后续回填：any → ActorManager` 注记删除
- `grep -rn "ActorManager() any" internal/` → 0 命中
- `grep -rn "formatPath(" internal/` → 0 命中（全部改 FormatPath）
