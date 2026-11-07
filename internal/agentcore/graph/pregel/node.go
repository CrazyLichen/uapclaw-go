package pregel

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// NodeInput 节点函数入参：把 Python 的「按形参名条件注入」换成显式结构体传递。
// 纪律：唯一构造点是 8.3b 的 NodeTask；包内别处不应构造。
// Python: pregel/task.py:137-149
type NodeInput struct {
	// Config 本次执行的内部配置，Ns/ParentNs 由 8.3b 改写为 parent:node:version
	Config InnerPregelConfig
	// State 恒 nil，对齐 task.py:149 的字面量 None。
	// 只能是 any：Python 注解的 graph_state.GraphState 全仓零实例化、零读取，
	// 且该类型属 graph 包，而 graph 要 import 本包，收窄即成编译期环（8.3 spec §3.2）。
	State any
}

// NodeFunc 节点执行体。
// 纪律：只返回普通错误。图级中断是 panic（*GraphInterrupt），由 8.3b 用 recover
// 转成返回值；本类型内不得 recover 或吞掉它（对齐 task.py:172-174 的 except 顺序）。
// 出边完全由 PregelNode 的 routers 决定，返回值不参与路由（Python 同）。
type NodeFunc func(ctx context.Context, in NodeInput) error

// PregelNode 图节点的描述体：调度键 + 执行体 + 出边。
// 字段全部非导出：8.3a/8.3b/8.3c 与本类型同在 pregel 包，非导出不阻碍任何消费方，
// 却能让「构造 / 追加」成为唯一入口（nil 路由只能在这两处被拦下）。
// 使用方式为指针：builder 构造后还要追加路由，值类型会丢 mutation。
// Python: pregel/base.py:46-50
type PregelNode struct {
	// name 调度键，同时是 TriggerChannel 的通道键
	name string
	// fn 执行体，非 nil（构造期保证）
	fn NodeFunc
	// routers 出边，构造后由 8.3a 追加、8.3b 逐条 Dispatch；元素非 nil
	routers []Router
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPregelNode 构造节点。
// fn 为 nil 或任一 router 为 nil 一律 panic：Python 对 None 是在执行期才抛
// （func=None → inspect.signature 报错；router=None → AttributeError），
// 本处把「必然崩」提前到构图期。typed-nil 挡不住——var r *StaticRouter;
// var i Router = r 时 i != nil 为真，调用 Dispatch 仍会解引用 r.targets panic，
// 故 8.3b 的 NodeTask 循环零判空（8.5 spec §9）。
// routers 复制入参切片（Python base.py:50 存引用；无消费者依赖可变性，同 8.5 D5）。
// Python: PregelNode(name, func, routers)
func NewPregelNode(name string, fn NodeFunc, routers ...Router) *PregelNode {
	if fn == nil {
		panic("pregel: 节点函数不能为 nil，节点名=" + name)
	}
	n := &PregelNode{
		name:    name,
		fn:      fn,
		routers: make([]Router, 0, len(routers)),
	}
	for _, r := range routers {
		n.addRouter(r)
	}
	return n
}

// NoopNodeFunc 空节点函数，对齐 builder.py:18-19 的 lambda: None（START/END 占位节点）。
func NoopNodeFunc(context.Context, NodeInput) error { return nil }

// ──────────────────────────── 非导出函数 ────────────────────────────

// addRouter 追加一条出边路由，供 8.3a 的 builder 在构图期使用。
// router 为 nil 时 panic：与构造期同口径，覆盖 builder 的追加路径
// （Python builder.py:49-68 直接 nodes[src].routers.append(...)）。
// 对应 Python 无独立方法（字段公开、直接 append）。
func (n *PregelNode) addRouter(r Router) {
	if r == nil {
		panic("pregel: 路由不能为 nil，节点名=" + n.name)
	}
	n.routers = append(n.routers, r)
}
