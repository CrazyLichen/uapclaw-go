package visualization

// DrawableSubgraphNode 子图节点（Python drawable_subgraph_node.py:12-13 一比一；
// 继承 DrawableNode → Go 嵌入）。
type DrawableSubgraphNode struct {
	DrawableNode
	// Subgraph 子图（Python subgraph，Optional → nil）
	Subgraph *DrawableGraph
}
