package flow

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// BranchComponent 分支组件（Python branch_comp.py:16-52 BranchComponent）：
// 排他网关——节点体空转（仅 set_session），评估在条件边 dispatch 阶段（spec §1）。
type BranchComponent struct {
	components.WorkflowComponent
	// router 分支路由器（Python self._router，构造期 report_trace=True）
	router *BranchRouter
}

// NewBranchComponent 构造组件（Python __init__: super().__init__() + BranchRouter(True)）。
func NewBranchComponent() *BranchComponent {
	c := &BranchComponent{router: NewBranchRouter(true)}
	// InvokeFn：Python invoke（branch_comp.py:38-40）—— set_session(session) → 返回 {}；
	// Python set_session 的 Session 分支取 _inner，Go 对应 Facade.InnerNodeSession()
	c.InvokeFn = func(_ context.Context, _ map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		if err := c.router.SetSession(sess.InnerNodeSession()); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}
	return c
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AddBranch 登记分支（Python add_branch：condition/target falsy 校验 + list 空项
// 逐项校验 → 转调 router.add_branch；错误码 COMPONENT_BRANCH_PARAM_INVALID）。
func (c *BranchComponent) AddBranch(cond any, target any, branchID string) error {
	if cond == nil {
		return errBranchParam("condition is None or empty")
	}
	var targets []string
	switch t := target.(type) {
	case string:
		targets = []string{t}
	case []string:
		targets = t
	default:
		return errBranchParam("target is None or empty")
	}
	if len(targets) == 0 {
		return errBranchParam("target is None or empty")
	}
	for idx, item := range targets {
		if item == "" {
			return errBranchParam(fmt.Sprintf("empty item at index %d in target list", idx))
		}
	}
	return c.router.AddBranch(cond, targets, branchID)
}

// Router 返回路由器（Python router()）。
func (c *BranchComponent) Router() *BranchRouter { return c.router }

// AddComponent 注册进图（Python add_component：add_node + add_conditional_edges +
// targets>1 时 register_branch_targets；Python hasattr 守卫在 Go 以 PregelGraph
// 具体类型承载，无需反射——8.1「特有方法由消费方断言」兑现点）。
func (c *BranchComponent) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, c.ToExecutable(), opts...)
	g.AddConditionalEdges(nodeID, c.router)
	// Register branch targets for CNF barrier resolution at compile time
	//（branch_comp.py:45-49 逐字复刻：len>1 才登记）
	allTargets := c.router.AllTargets()
	if len(allTargets) > 1 {
		ids := make([]string, 0, len(allTargets))
		for id := range allTargets {
			ids = append(ids, id)
		}
		g.RegisterBranchTargets(nodeID, ids)
	}
	return nil
}

// SkipTrace 跳过追踪（Python skip_trace → True）。
func (c *BranchComponent) SkipTrace() bool { return true }

// ──────────────────────────── 非导出函数 ────────────────────────────

// errBranchParam 参数无效（Python build_error(COMPONENT_BRANCH_PARAM_INVALID)）。
func errBranchParam(reason string) error {
	return exception.BuildError(exception.StatusComponentBranchParamInvalid,
		exception.WithParam("reason", reason))
}
