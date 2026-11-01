package pregel

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// Router 路由接口：节点执行完成后，据此产出下一超步的调度消息。
//
// 实现方约定：纯函数式——只读 sourceNode，不访问图、通道或检查点。
// Python: pregel/base.py:23-26 IRouter（唯一方法 dispatch）
type Router interface {
	Dispatch(ctx context.Context, sourceNode string) ([]Message, error)
}

// Selector 条件路由的选择器：返回目标节点名列表。
//
// state 恒传 nil：Python 侧该形参的唯一取值就是字面量 None（router.py:37），
// 且全链无人读取（唯一带该形参的 selector 是 _workflow.py:275 的包装器，
// 它收下即丢）。保留形参只为对齐数据形状，将来若接通图状态也只能是 any
// —— pregel 不能 import graph（8.1 的 graph.py 反向 import pregel，成环）。
type Selector interface {
	Select(ctx context.Context, state any) ([]string, error)
}

// SelectorFunc 把普通函数适配为 Selector，对齐仓库 XxxFunc func(ctx ...) 惯例。
type SelectorFunc func(ctx context.Context, state any) ([]string, error)

// StaticRouter 静态扇出（1→N）。
//
// targets 为空时产出零条消息，等价「边终止」（Python 测试用 StaticRouter([])
// 表示 END 节点）。构造时复制入参切片，调用方后续改切片不影响本实例。
// Python: pregel/router.py:11-18
type StaticRouter struct {
	// targets 目标路由键列表（顺序即消息产出顺序）
	targets []string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：SelectorFunc 必须满足 Selector。
var _ Selector = SelectorFunc(nil)

// 编译期接口断言：StaticRouter 必须满足 Router。
var _ Router = (*StaticRouter)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStaticRouter 构造静态扇出路由。
// Python: StaticRouter(targets)（router.py:14-15）
func NewStaticRouter(targets ...string) *StaticRouter {
	return &StaticRouter{targets: append([]string(nil), targets...)}
}

// Select 实现 Selector：调用被适配的函数。
func (f SelectorFunc) Select(ctx context.Context, state any) ([]string, error) {
	return f(ctx, state)
}

// Dispatch 产出 1→N 触发消息。
// Python: router.py:17-18
func (r *StaticRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	return buildTriggerMessages(sourceNode, r.targets), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTriggerMessages 按目标顺序构造触发消息。
//
// 目标为空时返回非 nil 空切片（对齐 Python 的 []），便于 8.3b 直接 extend
// 且 len 判断语义一致。
func buildTriggerMessages(sourceNode string, targets []string) []Message {
	msgs := make([]Message, 0, len(targets))
	for _, target := range targets {
		msgs = append(msgs, NewTriggerMessage(sourceNode, target))
	}
	return msgs
}
