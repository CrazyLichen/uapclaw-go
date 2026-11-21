package flow

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/condition"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Branch 单条分支：条件 + 目标列表（Python branch_router.py:19-38 Branch）。
type Branch struct {
	// BranchID 分支标识（Python branch_id，可空）
	BranchID string
	// target 命中后的目标节点列表
	target []string
	// condition 条件（三态归一产物）
	condition condition.Condition
}

// NewBranch 构造分支（Python __init__ 三态归一：Condition 实例 / str→ExpressionCondition /
// Callable→FuncCondition；其余 ValueError，add_branch 捕获转 COMPONENT_BRANCH_PARAM_INVALID）。
func NewBranch(cond any, target []string, branchID string) (*Branch, error) {
	b := &Branch{BranchID: branchID, target: target}
	switch c := cond.(type) {
	case condition.Condition:
		b.condition = c
	case string:
		ec, err := condition.NewExpressionCondition(c)
		if err != nil {
			return nil, err
		}
		b.condition = ec
	case func() bool:
		b.condition = condition.NewFuncCondition(c)
	default:
		return nil, errBranchConditionType()
	}
	return b, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Evaluate 评估条件（Python evaluate: return self._condition(session)）。
func (b *Branch) Evaluate(ctx context.Context, sess session.BaseSession) (bool, error) {
	return b.condition.Call(ctx, sess)
}

// TraceInfo 追踪信息（Python trace_info: return self._condition.trace_info(session)）。
func (b *Branch) TraceInfo(sess session.BaseSession) any {
	return b.condition.TraceInfo(sess)
}

// BranchRouter 分支路由器（Python branch_router.py:41-112）。
// 实现 pregel.Router：dispatch 阶段同步评估（spec §1 链路②）；
// session 走实例字段捕获（8.5 D2 惯例：Dispatch 的 state 形参恒 nil 不读）。
// 偏差 D-19-1：drawable 三处（env 读取 / DrawableBranchRouter / add_branch 填充）
// 整段跳过，回收条件挂 8.13。
type BranchRouter struct {
	branches    []*Branch
	session     session.BaseSession
	reportTrace bool
}

// NewBranchRouter 构造路由器（Python __init__(report_trace=False)）。
func NewBranchRouter(reportTrace bool) *BranchRouter {
	return &BranchRouter{reportTrace: reportTrace}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AddBranch 登记分支（Python add_branch：nil 校验 → 构造 Branch；
// branch_router.py:58-64 drawable 填充段按 D-19-1 跳过）。
// 偏差 D-19-4：target 收窄为 []string（Python Union[str, list[str]] 的 str 归一
// `[target] if isinstance(target, str)` 上移到调用方——用户拍板调用层自己转，
// 边界 any 只保留 cond 的三态归一）。
func (r *BranchRouter) AddBranch(cond any, target []string, branchID string) error {
	if cond == nil || target == nil {
		return exception.BuildError(exception.StatusComponentBranchParamInvalid,
			exception.WithParam("reason", "condition is None or target is None"))
	}
	if len(target) == 0 {
		return exception.BuildError(exception.StatusComponentBranchParamInvalid,
			exception.WithParam("reason", "target is None or empty"))
	}
	b, err := NewBranch(cond, target, branchID)
	if err != nil {
		return err
	}
	r.branches = append(r.branches, b)
	return nil
}

// AllTargets 目标全集（Python all_targets property：set 去重）。
func (r *BranchRouter) AllTargets() map[string]struct{} {
	targets := map[string]struct{}{}
	for _, b := range r.branches {
		for _, t := range b.target {
			targets[t] = struct{}{}
		}
	}
	return targets
}

// SetSession 注入会话（Python set_session 双类型分支：
// Session→取 _inner / BaseSession→直取 / 其余 ValueError；Go 统一 BaseSession 形态）。
func (r *BranchRouter) SetSession(sess session.BaseSession) error {
	if sess == nil {
		return errSessionTypeWrong()
	}
	r.session = sess
	return nil
}

// Call 评估分支（Python __call__，branch_router.py:92-112）：
// report_trace 三段 trace → 顺序评估首个命中返回其 target → 全不命中 101021。
func (r *BranchRouter) Call(ctx context.Context) ([]string, error) {
	sess := r.session
	if r.reportTrace {
		// 对齐 Python trace_component_begin/inputs（branch_router.py:94-102）；
		// Go 侧 trace 工具要求 WorkflowNodeSession 形态，不满足时跳过
		//（util 自身有 Tracer()==nil 守卫；接缝偏差记 8.19 spec §5）
		if ns, ok := sess.(tracer.WorkflowNodeSession); ok {
			var branches []map[string]any
			for _, b := range r.branches {
				branches = append(branches, map[string]any{
					"branch_id": b.BranchID,
					"condition": b.TraceInfo(sess),
				})
			}
			utils := tracer.TracerWorkflowUtils{}
			utils.TraceComponentBegin(ctx, ns, nil, nil)
			utils.TraceComponentInputs(ctx, ns, map[string]any{"branches": branches}, false)
		}
	}
	for _, b := range r.branches {
		hit, err := b.Evaluate(ctx, sess)
		if err != nil {
			return nil, err
		}
		if hit {
			// 对齐 Python trace_component_outputs/done（branch_router.py:105-107）
			if r.reportTrace {
				if ns, ok := sess.(tracer.WorkflowNodeSession); ok {
					utils := tracer.TracerWorkflowUtils{}
					utils.TraceComponentOutputs(ctx, ns, map[string]any{"branch_id": b.BranchID})
					utils.TraceComponentDone(ctx, ns)
				}
			}
			return b.target, nil
		}
	}
	return nil, exception.BuildError(exception.StatusComponentBranchExecutionError,
		exception.WithParam("reason", "branch meeting the condition was not found"))
}

// Select 实现 graph.Router（= pregel.Selector，Python 经 ConditionalRouter 包装
// __call__；Go 侧 BranchRouter 直挂 Selector 接口）。
// state 恒传 nil 不读（8.5 D2 惯例：Python selector 形参唯一取值 None，session
// 走实例字段捕获）；error 原样上抛（8.3b FIRST_EXCEPTION 分类依赖 errors.As）。
// 消息产出（TriggerMessage）由 ConditionalRouter.dispatch 承载（router.py:47 同形）。
func (r *BranchRouter) Select(_ context.Context, _ any) ([]string, error) {
	return r.Call(context.Background())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errBranchConditionType 条件类型不符（Python ValueError 逐字复刻，add_branch 转译）。
func errBranchConditionType() error {
	return exception.BuildError(exception.StatusComponentBranchParamInvalid,
		exception.WithParam("reason", "branch condition type does not meet the requirements"))
}

// errSessionTypeWrong 会话类型不符（Python "session type is wrong" 逐字复刻）。
func errSessionTypeWrong() error {
	return exception.BuildError(exception.StatusComponentBranchParamInvalid,
		exception.WithParam("reason", "session type is wrong"))
}
