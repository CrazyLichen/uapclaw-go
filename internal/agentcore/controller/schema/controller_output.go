package schema

// ──────────────────────────── 结构体 ────────────────────────────

// ControllerOutputPayload 输出载荷，包含输出类型、数据和元数据。
//
// 对应 Python: openjiuwen/core/controller/schema/controller_output.py (ControllerOutputPayload)
type ControllerOutputPayload struct {
	// Type 输出类型
	Type string `json:"type"`
	// Data 输出数据列表
	Data []DataFrame `json:"data"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ControllerOutput 批量输出。
//
// 对应 Python: openjiuwen/core/controller/schema/controller_output.py (ControllerOutput)
type ControllerOutput struct {
	// Type 输出类型
	Type string `json:"type"`
	// Data 输出数据载荷列表
	Data []*ControllerOutputPayload `json:"data"`
	// InputEventID 关联的输入事件ID
	InputEventID string `json:"input_event_id,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TaskProcessing 处理中类型标识
	TaskProcessing = "processing"
	// AllTasksProcessed 全部任务已处理类型标识
	AllTasksProcessed = "all_tasks_processed"
)
