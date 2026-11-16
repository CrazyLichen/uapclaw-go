package graph

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AtomicNode 原子节点：节点级状态提交的原子性包装。
// Python: openjiuwen/core/graph/atomic_node.py。Python 的 AtomicNode/AsyncAtomicNode
// 双基类按 spec 偏差 D1 合并为单结构体（包装逻辑逐字相同，async 语义由 8.11 Vertex
// 的执行管线承载）；_atomic_invoke 抽象方法由构造期注入回调承载（偏差 D3）。
type AtomicNode struct {
	// atomicInvokeFunc 原子执行回调（Python 抽象方法 _atomic_invoke）
	atomicInvokeFunc func(ctx context.Context, kwargs map[string]any) (any, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAtomicNode 创建原子节点。atomicInvokeFunc 为 nil 时 panic，
// 对齐 Python ABC 抽象方法 _atomic_invoke 必须被实现的约束（偏差 D3）。
func NewAtomicNode(atomicInvokeFunc func(ctx context.Context, kwargs map[string]any) (any, error)) AtomicNode {
	if atomicInvokeFunc == nil {
		panic("atomicInvokeFunc 不能为 nil（对齐 Python 抽象方法 _atomic_invoke 必须实现）")
	}
	return AtomicNode{atomicInvokeFunc: atomicInvokeFunc}
}

// AtomicInvoke 原子调用：校验会话与状态 → 执行回调 → 提交暂存 → 返回结果。
// Python: AtomicNode.atomic_invoke / AsyncAtomicNode.atomic_invoke（偏差 D1 合并），
// 步骤顺序对齐 atomic_node.py:26-31，任一步失败不提交、错误原样上抛。
func (a *AtomicNode) AtomicInvoke(ctx context.Context, kwargs map[string]any) (any, error) {
	// Python: session = kwargs.get("session", None)
	var sess session.BaseSession
	if raw, ok := kwargs["session"].(session.BaseSession); ok {
		sess = raw
	}

	// Python: _validate_session_and_state(session)
	ws, err := validateSessionAndState(sess)
	if err != nil {
		return nil, err
	}

	// Python: result = self._atomic_invoke(**kwargs)
	result, err := a.atomicInvokeFunc(ctx, kwargs)
	if err != nil {
		return nil, err
	}

	// Python: session.state().commit_cmp()
	ws.CommitCmp()

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateSessionAndState 校验会话与状态可提交（atomic_node.py:13-22 一比一，
// 三道检查顺序不可调换）。返回断言后的 WorkflowState 供 AtomicInvoke 提交复用，
// 避免二次断言（Python 侧由属性访问天然获得，Go 需显式传递）。
// 偏差 D2：Python 第二道 isinstance(session, BaseSession) 检查在 Go 由接口参数
// 类型编译期保证，注销。
func validateSessionAndState(sess session.BaseSession) (state.WorkflowState, error) {
	if sess == nil {
		return nil, exception.BuildError(exception.StatusGraphStateCommitError,
			exception.WithParam("reason", "session is None"))
	}

	ws, ok := sess.State().(state.WorkflowState)
	if !ok {
		return nil, exception.BuildError(exception.StatusGraphStateCommitError,
			exception.WithParam("reason", "session is not support commit state"))
	}

	return ws, nil
}
