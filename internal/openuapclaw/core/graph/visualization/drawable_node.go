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
