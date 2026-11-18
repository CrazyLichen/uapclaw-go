package graph

import (
	pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Graph 声明式构图接口：链式登记节点/边/条件边，Compile 产出可执行图。
// Python: openjiuwen/core/graph/base.py:39-58 Graph(ABC)。
// Self 返回类型收窄为 *PregelGraph（唯一实现者，spec 偏差 D-1）。
type Graph interface {
	// StartNode 登记起始边 START→node_id（Python start_node，base.py:40-42）
	StartNode(nodeID string) *PregelGraph
	// EndNode 标记终节点并登记边 node_id→END（Python end_node，base.py:43-45）
	EndNode(nodeID string) *PregelGraph
	// AddNode 注册节点，waitForAll=true 时前驱汇聚成屏障（Python add_node，base.py:46-48；
	// keyword-only 默认 False → Go 显式 bool 形参，spec 偏差 D-2）
	AddNode(nodeID string, node GeneralExecutor, waitForAll bool) *PregelGraph
	// AddEdge 登记边，sourceNodeID 为 string 或 []string（Python add_edge，base.py:49-50）
	AddEdge(sourceNodeID any, targetNodeID string) *PregelGraph
	// AddConditionalEdges 登记条件边（Python add_conditional_edges，base.py:52-53）
	AddConditionalEdges(sourceNodeID string, router Router) *PregelGraph
	// Compile 编译为可执行图（Python compile，base.py:55-56）
	Compile(sess session.BaseSession, kwargs map[string]any) ExecutableGraph[map[string]any, map[string]any]
	// GetNodes 返回节点表浅拷贝（Python get_nodes，base.py:58-59）
	GetNodes() map[string]*Vertex
}

// ──────────────────────────── 常量 ────────────────────────────

// Router 条件路由选择器。Python: base.py:33-36 Router 类型别名
// （同步/async callable 双形态，Go 无 async 统一同步）。
// 注意：与 8.5 的 pregel.Router（IRouter 接口，Dispatch）同名不同物——
// 本别名指向 pregel.Selector（SelectorProtocol 的 Go 对应物）。
// 运行时数据语义见 8.1 spec §5.1：state 恒 nil，判断靠捕获 session 引用。
type Router = pregel.Selector
