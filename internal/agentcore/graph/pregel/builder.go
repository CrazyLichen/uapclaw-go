package pregel

// ──────────────────────────── 结构体 ────────────────────────────

// PregelBuilder Pregel 图的构图期组装者：持有节点表与通道列表，
// 是 PregelNode（8.3）+ Router 三实现（8.5）+ Trigger/Barrier 通道（8.4）
// 的唯一组装点；Build()（⤴️ 8.3c 回填）将两者交付给调度引擎。
// Python: builder.py:13
type PregelBuilder struct {
	// nodes 节点名 → 节点（对齐 Python self.nodes dict；重复名后者覆盖）
	nodes map[string]*PregelNode
	// channels 构图期顺序追加的通道列表（对齐 Python self.channels list）
	channels []Channel
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPregelBuilder 创建构图器并预注册 START/END 两个虚拟节点
//（执行体 NoopNodeFunc、零 routers），各自配一条 TriggerChannel。
// Python: builder.py:14-19
func NewPregelBuilder() *PregelBuilder {
	b := &PregelBuilder{
		nodes:    make(map[string]*PregelNode),
		channels: make([]Channel, 0),
	}
	b.AddNode(Start, NoopNodeFunc)
	b.AddNode(End, NoopNodeFunc)
	return b
}

// ──────────────────────────── 导出方法 ────────────────────────────

// AddNode 注册节点并追加其触发通道；重复名按 Python dict 赋值语义覆盖。
// Python routers=None 默认值对应 Go 变参零值。返回 builder 自身（链式）。
// Python: builder.py:21-26
func (b *PregelBuilder) AddNode(name string, fn NodeFunc, routers ...Router) *PregelBuilder {
	b.nodes[name] = NewPregelNode(name, fn, routers...)
	b.channels = append(b.channels, NewTriggerChannel(name))
	return b
}
