package graph

// ──────────────────────────── 结构体 ────────────────────────────

// GraphState 节点输出聚合 schema，对齐 openjiuwen/core/graph/graph_state.py 的 TypedDict。
// Python 声明为 source_node_id: Annotated[list, operator.add]——语义为
// "执行过的来源节点 ID 以列表拼接（operator.add）追加式归并，不覆盖"。
// 全仓零实例化死类型：唯一 Python 消费者是 vertex.py:364 对 __call__ state 形参的类型标注，
// 节点返回值在 task.py:152 被丢弃，reducer 在 Python 全仓无归并执行点（spec 偏差 D1：
// Go 类型系统无 Annotated reducer 注解对应物，语义以本注释承载，不实现归并方法）。
// 与 pregel/state.go 的检查点 GraphState 同名不同义，靠包隔离区分。
type GraphState struct {
	// SourceNodeID 执行过的来源节点 ID 列表（operator.add 追加语义）
	SourceNodeID []string
}
