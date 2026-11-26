package visualization

// DrawableEdge 可绘制边（Python drawable_edge.py:15-20 一比一；
// 偏差 D-13-2：data Stringifiable 协议 → string，调用方实传均为字符串标签）。
type DrawableEdge struct {
	// Source 源节点（Python source）
	Source string
	// Target 目标节点（Python target）
	Target string
	// Data 条件边标签（Python data，Optional → 空串，D-13-2）
	Data string
	// Conditional 是否条件边（Python conditional）
	Conditional bool
	// Streaming 是否流式边（Python streaming）
	Streaming bool
}

// DrawableBranchRouter 分支路由 drawable 投影（Python drawable_edge.py:24-26 一比一）
type DrawableBranchRouter struct {
	// Targets 分支目标列表（Python targets）
	Targets []string
	// Datas 分支标签列表（Python datas）
	Datas []string
}
