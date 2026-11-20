package graph

import (
	pregel "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Graph 声明式构图接口：链式登记节点/边/条件边，Compile 产出可执行图。
// Python: openjiuwen/core/graph/base.py:39-58 Graph(ABC)。
// 构图方法返回 Graph 接口自返回（2026-11-16 用户拍板，D-1 改写）——
// 对齐 Python typing.Self 的链式语义，链上每环仍走接口派发；
// PregelGraph 特有方法（RegisterBranchTargets/Reset，Python 不在 Graph ABC 内）
// 由消费方（_workflow.py 持具体类型的对应物）类型断言获取。
type Graph interface {
	// StartNode 登记起始边 START→node_id（Python start_node，base.py:40-42）
	StartNode(nodeID string) Graph
	// EndNode 标记终节点并登记边 node_id→END（Python end_node，base.py:43-45）
	EndNode(nodeID string) Graph
	// AddNode 注册节点，WithWaitForAll() 时前驱汇聚成屏障
	// （Python add_node，base.py:46-48 keyword-only 默认 False；
	// Go 变参 option 承载，零 option ≡ 默认 False，偏差 D-2）
	AddNode(nodeID string, node GeneralExecutor, opts ...AddNodeOption) Graph
	// AddEdge 登记边（Python add_edge，base.py:49-50 Union[str, list[str]]；
	// 2026-11-16 用户拍板收窄为 []string 单形态——单源调用点包一层 []string{src}，
	// 编译期即拦非法源类型，偏差 D-7）
	AddEdge(sourceNodeIDs []string, targetNodeID string) Graph
	// AddConditionalEdges 登记条件边（Python add_conditional_edges，base.py:52-53）
	AddConditionalEdges(sourceNodeID string, router Router) Graph
	// Compile 编译为可执行图（Python compile，base.py:55-56）
	// I=any 对齐 Python Input TypeVar 无类型（信封 INPUTS_KEY 取出值可为 InteractiveInput）
	Compile(sess session.BaseSession, kwargs map[string]any) ExecutableGraph[any, map[string]any]
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
