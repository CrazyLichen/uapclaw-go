package rail

// ──────────────────────────── 结构体 ────────────────────────────

// EventInputs 回调事件输入接口。
//
// 各事件类型对应不同的 Inputs 结构体，均实现此接口。
// 调用方通过 type switch 获取具体类型。
//
// 对应 Python: EventInputs = Union[InvokeInputs, ModelCallInputs, ToolCallInputs, TaskIterationInputs, Dict]
type EventInputs interface {
	// EventKind 返回事件输入的种类标识
	EventKind() string

// ──────────────────────────── 结构体 ────────────────────────────

// InvokeInputs BEFORE/AFTER_INVOKE 事件输入。
// ⤵️ 6.9 回填字段
type InvokeInputs struct{}

// ModelCallInputs BEFORE/AFTER_MODEL_CALL 事件输入。
// ⤵️ 6.9 回填字段
type ModelCallInputs struct{}

// ToolCallInputs BEFORE/AFTER_TOOL_CALL 事件输入。
// ⤵️ 6.9 回填字段
type ToolCallInputs struct{}

// TaskIterationInputs BEFORE/AFTER_TASK_ITERATION 事件输入。
// ⤵️ 6.9 回填字段
type TaskIterationInputs struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// EventKind 实现 EventInputs 接口
func (i *InvokeInputs) EventKind() string { return "invoke" }

func (i *ModelCallInputs) EventKind() string { return "model_call" }

func (i *ToolCallInputs) EventKind() string { return "tool_call" }

func (i *TaskIterationInputs) EventKind() string { return "task_iteration" }
