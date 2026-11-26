package visualization

// DrawableNode 可绘制节点（Python drawable_node.py:8-11 一比一；
// dataclass → Go 结构体直译，偏差 D-13-2：metadata dict → map[string]any）。
type DrawableNode struct {
	// ID 节点标识（Python id）
	ID string
	// Name 名称（Python name，Optional → 空串）
	Name string
	// Metadata 元数据（Python metadata，Optional → nil）
	Metadata map[string]any
}

// NodeLike 节点同质容器接口（Python nodes dict 可同时持有 DrawableNode 与其子类
// DrawableSubgraphNode；Go 以接口承载同质表，数据契约型，零破环）
type NodeLike interface {
	// NodeID 返回节点标识（Python id 字段访问）
	NodeID() string
}

// NodeID 返回节点标识（DrawableSubgraphNode 经嵌入提升同款）
func (n *DrawableNode) NodeID() string { return n.ID }
