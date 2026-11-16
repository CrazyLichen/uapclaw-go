// Package graph 提供图引擎顶层的共享类型定义，对齐 openjiuwen/core/graph 包根。
//
// 当前仅含 GraphState（节点输出聚合 schema）：Python 侧为零运行时效果的 TypedDict
// 类型标注，唯一消费者是 vertex.py:364 对 Vertex.__call__ state 形参的类型标注；
// 节点返回值在 pregel/task.py:152 被丢弃，Annotated[list, operator.add] reducer
// 全仓无归并执行点，故 Go 侧仅复刻类型不实现归并（spec 偏差 D1）。
// 与子包 pregel 的检查点 GraphState（internal/agentcore/graph/pregel/state.go）同名不同义。
//
// 文件目录：
//
//	graph/
//	├── doc.go            # 包文档
//	└── graph_state.go   # GraphState 节点输出聚合 schema
//
// 对应 Python 代码：openjiuwen/core/graph/graph_state.py
package graph
