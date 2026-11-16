// Package graph 提供图引擎顶层的共享类型定义，对齐 openjiuwen/core/graph 包根。
//
// 含 GraphState（节点输出聚合 schema）与 AtomicNode（8.10）：GraphState 在 Python
// 侧为零运行时效果的 TypedDict 类型标注，唯一消费者是 vertex.py:364 对
// Vertex.__call__ state 形参的类型标注；节点返回值在 pregel/task.py:152 被丢弃，
// Annotated[list, operator.add] reducer 全仓无归并执行点，故 Go 侧仅复刻类型
// 不实现归并（spec 偏差 D1）。与子包 pregel 的检查点 GraphState
//（internal/agentcore/graph/pregel/state.go）同名不同义。
// AtomicNode（atomic_node.go）为节点级原子提交包装（Python atomic_node.py，
// 8.10），消费方为 8.11 Vertex 与 8.19/8.20 条件组件；当前无即时消费者，
// 属后续章节的地基件。
//
// Executable（executable.go）为可执行节点契约（8.2）：泛型接口承载四执行入口
// （OnInvoke/OnStream/OnCollect/OnTransform，"单/流 × 单/流"矩阵）与四策略钩子
// （SkipTrace/GraphInvoker/PostCommit/ComponentType，由 8.11 Vertex 消费）；
// 流式语义经 StreamChunk（Err 非 nil 为末元素后关闭）承载。ExecutableGraph
// （executable_graph.go）为可执行图契约，Invoke 信封拆分（InputsKey/ConfigKey）
// 对齐 base.py:13-14，默认 pass 流式方法经包级泛型函数承载（spec 偏差 D5）。
//
// 文件目录：
//
//	graph/
//	├── doc.go                 # 包文档
//	├── executable.go          # Executable 泛型接口 + StreamChunk + BaseExecutable 基座（8.2）
//	├── executable_graph.go    # ExecutableGraph 契约 + InputsKey/ConfigKey 信封常量（8.2）
//	├── atomic_node.go         # AtomicNode 原子提交包装（8.10）
//	└── graph_state.go        # GraphState 节点输出聚合 schema
//
// 对应 Python 代码：openjiuwen/core/graph/graph_state.py · atomic_node.py · executable.py · base.py
package graph
