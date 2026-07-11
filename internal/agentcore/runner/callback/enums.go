package callback

// ──────────────────────────── 枚举 ────────────────────────────

// FilterAction 过滤器动作，控制回调是否执行。
//
// 对应 Python: openjiuwen/core/runner/callback/enums.py (FilterAction)
type FilterAction string

// ChainAction 链式执行动作，控制回调链流程。
//
// 对应 Python: openjiuwen/core/runner/callback/enums.py (ChainAction)
type ChainAction string

// HookType 生命周期钩子类型。
//
// 对应 Python: openjiuwen/core/runner/callback/enums.py (HookType)
type HookType string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// FilterActionContinue 正常执行
	FilterActionContinue FilterAction = "continue"
	// FilterActionStop 停止整个事件处理（不再执行后续回调）
	FilterActionStop FilterAction = "stop"
	// FilterActionSkip 跳过当前回调，继续下一个
	FilterActionSkip FilterAction = "skip"
	// FilterActionModify 修改参数后继续执行
	FilterActionModify FilterAction = "modify"
)

const (
	// ChainActionContinue 继续下一个回调
	ChainActionContinue ChainAction = "continue"
	// ChainActionBreak 中断链，返回当前结果
	ChainActionBreak ChainAction = "break"
	// ChainActionRetry 重试当前回调
	ChainActionRetry ChainAction = "retry"
	// ChainActionRollback 回滚所有已执行回调
	ChainActionRollback ChainAction = "rollback"
)

const (
	// HookTypeBefore 事件处理前
	HookTypeBefore HookType = "before"
	// HookTypeAfter 事件处理后
	HookTypeAfter HookType = "after"
	// HookTypeError 出错时
	HookTypeError HookType = "error"
	// HookTypeCleanup 清理阶段
	HookTypeCleanup HookType = "cleanup"
)
