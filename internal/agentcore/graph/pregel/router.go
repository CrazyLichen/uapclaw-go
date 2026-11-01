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

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：SelectorFunc 必须满足 Selector。
var _ Selector = SelectorFunc(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// Select 实现 Selector：调用被适配的函数。
func (f SelectorFunc) Select(ctx context.Context, state any) ([]string, error) {
	return f(ctx, state)
}
