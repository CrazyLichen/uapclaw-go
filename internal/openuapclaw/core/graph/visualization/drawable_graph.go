package visualization

// DrawableGraph 可绘制图（Python drawable_graph.py:11-16 一比一；
// break_nodes Optional → nil 切片承载；nodes 可持有子类 → NodeLike 同质表）
type DrawableGraph struct {
	// Nodes 节点表（Python nodes dict；值 *DrawableNode 或 *DrawableSubgraphNode）
	Nodes map[string]NodeLike
	// Edges 边列表（Python edges）
	Edges []*DrawableEdge
	// StartNodes 起始节点（Python start_nodes）
	StartNodes []NodeLike
	// EndNodes 结束节点（Python end_nodes）
	EndNodes []NodeLike
	// BreakNodes 中断节点（Python break_nodes，Optional → nil）
	BreakNodes []NodeLike
}
