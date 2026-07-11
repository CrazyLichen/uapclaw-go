package schema

// ──────────────────────────── 枚举 ────────────────────────────

// TaskStatus 任务状态枚举，定义任务的所有可能状态。
//
// 状态流转：submitted → working → (completed | failed | paused | canceled | input-required | waiting)
//
// 对应 Python: openjiuwen/core/controller/schema/task.py (TaskStatus)
type TaskStatus string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// TaskSubmitted 已提交，等待执行
	TaskSubmitted TaskStatus = "submitted"
	// TaskWorking 执行中
	TaskWorking TaskStatus = "working"
	// TaskPaused 已暂停
	TaskPaused TaskStatus = "paused"
	// TaskInputRequired 需要用户输入
	TaskInputRequired TaskStatus = "input-required"
	// TaskCompleted 已完成
	TaskCompleted TaskStatus = "completed"
	// TaskCanceled 已取消
	TaskCanceled TaskStatus = "canceled"
	// TaskFailed 已失败
	TaskFailed TaskStatus = "failed"
	// TaskWaiting 等待中（可能等待依赖任务完成）
	TaskWaiting TaskStatus = "waiting"
	// TaskUnknown 未知状态
	TaskUnknown TaskStatus = "unknown"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTerminal 判断是否为终态（completed/failed/canceled）。
//
// 终态任务不会再发生状态转换，A2A 协议据此决定是否关闭流。
func (s TaskStatus) IsTerminal() bool {
	return s == TaskCompleted || s == TaskFailed || s == TaskCanceled
}

// IsInputRequired 判断是否为需要用户输入状态。
func (s TaskStatus) IsInputRequired() bool {
	return s == TaskInputRequired
}
