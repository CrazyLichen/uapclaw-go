// Vertex 工作流图节点执行器：双链模型——批入侧（INVOKE/STREAM，Pregel 调度同步链）
// 与流入侧（COLLECT/TRANSFORM，StreamActor 首帧异步链），两半经当轮 streamDone
// 握手对齐后 call() 才 mark_producer_done 交屏障。
//
// 并发映射零锁依据（spec docs/superpowers/specs/2026-11-17-vertex-8.11-design.md §1
// 不变式）：计数器/streamDone 槽位的跨 goroutine 访问对全部被引擎屏障定序——
// 批侧判定窗口内无并发写方，新一轮 spawn（含 streamDone 槽位重写）只会发生在
// 旧任务结束且批侧已消费当轮结果之后。-race 并发用例锁定该不变式。
package graph

import (
	"context"
	"strings"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Vertex 工作流图节点执行器。
// Python: Vertex(AsyncAtomicNode, StreamConsumer)（vertex.py:65），单结构体承载
// 双基类（AsyncAtomicNode 由 8.10 AtomicNode 承载，于批入编排任务接入；
// StreamConsumer 三方法由本类型实现）。
type Vertex struct {
	// executable 可执行体（Python: self._executable: Executable；GeneralExecutor
	// 对齐 Python GeneralExecutor = Executable[dict, dict] 的默认实例化形状）
	executable GeneralExecutor
	// nodeID 节点标识（Python: self._node_id）
	nodeID string
	// ctx 执行上下文（Python: self._context，init 时从 kwargs 注入）
	ctx context.Context
	// streamCalledTimeout 流调用等待超时秒数（Python: self._stream_called_timeout = 10）
	streamCalledTimeout float64
	// streamDone 当轮流侧结果容器（Python: self._stream_done asyncio.Future，
	// vertex.py:74；每轮 StreamCall 新建，缓冲 1 对齐 set_result 不阻塞语义）
	streamDone chan error
	// callCount 批入轮次计数（Python: self._call_count，单写方=批侧）
	callCount atomic.Int64
	// streamCallCount 流入轮次计数（Python: self._stream_call_count，单写方=流侧）
	streamCallCount atomic.Int64
	// isEndNode 是否 End 节点（Python: self.is_end_node）
	isEndNode bool
	// isStarted trace 去重：一轮只发一次 component_begin（Python: asyncio.Event）
	isStarted atomic.Bool
	// isCallStarted 批入侧已记输入标记（Python: asyncio.Event；trace 合并发送协议）
	isCallStarted atomic.Bool
	// nodeConfig 节点规格（Python: self._node_config；NodeSession.NodeConfig() any 断言）
	nodeConfig *workflow.NodeSpec
	// componentAbility 组件能力面（Python: self._component_ability）
	componentAbility []components.ComponentAbility
	// hasStreamCall 是否含流能力（Python: self._has_stream_call）
	hasStreamCall bool
	// hasCall 是否含批能力（Python: self._has_call）
	hasCall bool
	// sourceID 源标识（Python: self._source_id）
	sourceID []string
	// isFirstInit 是否首次初始化（Python: self._is_first_init；首初始化才记 debug 日志）
	isFirstInit atomic.Bool
}

// ──────────────────────────── 常量 ────────────────────────────

// subWorkflowComponent 子工作流组件类型标签（Python vertex.py:30 模块级常量）
const subWorkflowComponent = "sub_workflow"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVertex 创建 Vertex（Python __init__，vertex.py:67-85 字段一比一）
func NewVertex(nodeID string, executable GeneralExecutor) *Vertex {
	v := &Vertex{
		nodeID:              nodeID,
		executable:          executable,
		streamCalledTimeout: 10,
		streamDone:          make(chan error, 1),
	}
	v.isFirstInit.Store(true)
	return v
}

// IsDone 消费者是否完成（Python is_done，vertex.py:711-713：call==stream 或
// call==stream+1，即两半轮次对齐或批入多一轮的纯批入节点）。只读对账，跨 goroutine
// 一致性由引擎屏障定序保证（spec §1 不变式）。
func (v *Vertex) IsDone() bool {
	c := v.callCount.Load()
	s := v.streamCallCount.Load()
	return c == s || c == s+1
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// streamCalled 本轮流侧是否已先于批侧启动（Python _stream_called，vertex.py:715-716）
func (v *Vertex) streamCalled() bool {
	return v.streamCallCount.Load() == v.callCount.Load()+1
}

// collectRefSourceIDs 递归收集 schema 中 ${comp_id...} 引用的组件 ID 集合。
// Python: _collect_ref_source_ids（vertex.py:33-62，模块级函数 → 包级函数）
func collectRefSourceIDs(schema map[string]any) map[string]bool {
	ids := map[string]bool{}
	var walk func(obj any)
	walk = func(obj any) {
		switch val := obj.(type) {
		case map[string]any:
			for _, item := range val {
				walk(item)
			}
		case []any:
			for _, item := range val {
				walk(item)
			}
		case string:
			if utils.IsRefPath(val) {
				origin := utils.ExtractOriginKey(val)
				if origin != "" {
					ids[strings.SplitN(origin, ".", 2)[0]] = true
				}
			}
		}
	}
	walk(schema)
	return ids
}

// resetStreamDone 重建当轮流结果容器（Python reset 的 cancel→新建 Future 语义，
// vertex.py:885-889；Go 无等待方时取消无副作用，直接新建）
func (v *Vertex) resetStreamDone() {
	v.streamDone = make(chan error, 1)
}

// Reset 重置计数与流容器（Python reset，vertex.py:882-890）
func (v *Vertex) Reset() {
	v.callCount.Store(0)
	v.streamCallCount.Store(0)
	v.resetStreamDone()
}
