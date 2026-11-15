package pregel

import "fmt"

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

// AddEdge 声明一条边，按 Python 四形状 isinstance 分派（方案 β，spec §3.3）：
//
//	[]string → string  ：N→1 barrier，每源节点独占一组（全 AND 形态，偏差 A1）
//	string   → []string：1→N static
//	string   → string  ：1→1 static
//	其余组合           ：返回 error（对齐 Python ValueError）
//
// barrier 的合成路由键由 BarrierChannel.Key() 生成，builder 只转手给
// NewBarrierRouter（8.5 spec §9 承诺）；未注册源节点返回 error（偏差 A2）。
// Python: builder.py:28-65
func (b *PregelBuilder) AddEdge(start, end any) error {
	switch s := start.(type) {
	case []string:
		if e, ok := end.(string); ok {
			// barrier：每源节点独占一组（全 AND 形态，偏差 A1），建 BarrierChannel
			// 追加进 channels，并给每个源节点挂 BarrierRouter（目标为合成键）。
			// Python: builder.py:34-53（内联，按规则 9.4 不抽助手）
			groups := make([][]string, 0, len(s))
			for _, item := range s {
				groups = append(groups, []string{item})
			}
			barrier := NewBarrierChannel(e, groups)
			b.channels = append(b.channels, barrier)
			for _, item := range s {
				node, ok := b.nodes[item]
				if !ok {
					return fmt.Errorf("unknown node: %s", item)
				}
				node.addRouter(NewBarrierRouter(barrier.Key()))
			}
			return nil
		}
	case string:
		switch e := end.(type) {
		case []string:
			node, ok := b.nodes[s]
			if !ok {
				return fmt.Errorf("unknown node: %s", s)
			}
			node.addRouter(NewStaticRouter(e...))
			return nil
		case string:
			node, ok := b.nodes[s]
			if !ok {
				return fmt.Errorf("unknown node: %s", s)
			}
			node.addRouter(NewStaticRouter(e))
			return nil
		}
	}
	return fmt.Errorf("unsupported edge format: start %T -> end %T", start, end)
}

// AddBranch 给源节点追加条件路由（selector 运行时决定目标），返回 builder 自身
// （链式）。未注册 src 显式 panic（偏差 A2，对齐构造期 panic 纪律）。
// Python: builder.py:67-69
func (b *PregelBuilder) AddBranch(src string, selector Selector) *PregelBuilder {
	node, ok := b.nodes[src]
	if !ok {
		panic(fmt.Sprintf("unknown node: %s", src))
	}
	node.addRouter(NewConditionalRouter(selector))
	return b
}
