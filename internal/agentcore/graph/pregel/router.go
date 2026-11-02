package pregel

import (
	"context"
	"fmt"
)

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

// BarrierRouter 汇聚路由（N→1），产出 BarrierMessage。
//
// target 不是节点名，而是 barrier 通道的合成路由键 "barrier:a&b->c"
// （channels.py:155-164）；sender 由 BarrierChannel 收下作为 OR 组判据。
// Python: pregel/router.py:49-56
type BarrierRouter struct {
	// targets 目标 barrier 通道路由键列表
	targets []string
}

// ConditionalRouter 条件路由：由 selector 决定目标，产出 TriggerMessage。
// Python: pregel/router.py:26-46
type ConditionalRouter struct {
	// selector 选择器；nil 时 Dispatch 返回 error（对齐 Python 在 dispatch
	// 阶段才 inspect.signature(None) 抛 TypeError 的时机）
	selector Selector
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：SelectorFunc 必须满足 Selector。
var _ Selector = SelectorFunc(nil)

// 编译期接口断言：StaticRouter 必须满足 Router。
var _ Router = (*StaticRouter)(nil)

// 编译期接口断言：BarrierRouter 必须满足 Router。
var _ Router = (*BarrierRouter)(nil)

// 编译期接口断言：ConditionalRouter 必须满足 Router。
var _ Router = (*ConditionalRouter)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStaticRouter 构造静态扇出路由。
// Python: StaticRouter(targets)（router.py:14-15）
func NewStaticRouter(targets ...string) *StaticRouter {
	return &StaticRouter{targets: append([]string(nil), targets...)}
}

// NewBarrierRouter 构造汇聚路由。
// Python: BarrierRouter(targets)（router.py:52-53）
func NewBarrierRouter(targets ...string) *BarrierRouter {
	return &BarrierRouter{targets: append([]string(nil), targets...)}
}

// NewConditionalRouter 构造条件路由。
// Python: ConditionalRouter(selector)（router.py:29-30）
func NewConditionalRouter(selector Selector) *ConditionalRouter {
	return &ConditionalRouter{selector: selector}
}

// Select 实现 Selector：调用被适配的函数。
//
// nil 函数值自守：`NewConditionalRouter(SelectorFunc(nil))` 会造出非 nil 的接口值，
// 绕过 Dispatch 里的 `r.selector == nil` 判断，直接调用会 panic。
func (f SelectorFunc) Select(ctx context.Context, state any) ([]string, error) {
	if f == nil {
		return nil, fmt.Errorf("条件路由 selector 为 nil 函数值")
	}
	return f(ctx, state)
}

// Dispatch 产出 1→N 触发消息。
// Python: router.py:17-18
func (r *StaticRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	return buildTriggerMessages(sourceNode, r.targets), nil
}

// Dispatch 产出 N→1 汇聚消息。
// Python: router.py:55-56
func (r *BarrierRouter) Dispatch(_ context.Context, sourceNode string) ([]Message, error) {
	msgs := make([]Message, 0, len(r.targets))
	for _, target := range r.targets {
		msgs = append(msgs, NewBarrierMessage(sourceNode, target))
	}
	return msgs, nil
}

// Dispatch 由 selector 决定目标并产出触发消息。
//
// 纪律：state 恒传 nil（spec §1.3）；selector 的 error 原样上抛，不包装——
// 8.3b 需要区分 GraphInterrupt 与普通 error，包装会破坏 errors.As。
//
// 空目标语义分两态（对齐 Python router.py:44-46）：
//   - nil：selector 没有返回值 → 判为构图错误并返回 error。Python 侧
//     `for to in None` 直接 TypeError，是 fail-closed；Go 若把 nil 当成
//     "边终止"，selector 漏 return 会表现为"图提前结束"，静默且无日志。
//   - 非 nil 空切片：显式表示"无出边"（END）→ 零条消息。
//
// Python: router.py:32-46
func (r *ConditionalRouter) Dispatch(ctx context.Context, sourceNode string) ([]Message, error) {
	if r.selector == nil {
		return nil, fmt.Errorf("条件路由未设置 selector，节点=%s", sourceNode)
	}
	targets, err := r.selector.Select(ctx, nil)
	if err != nil {
		return nil, err
	}
	if targets == nil {
		return nil, fmt.Errorf("条件路由 selector 未返回目标（nil），节点=%s", sourceNode)
	}
	return buildTriggerMessages(sourceNode, targets), nil
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
