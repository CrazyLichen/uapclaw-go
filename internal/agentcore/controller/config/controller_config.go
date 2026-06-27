package config

// ──────────────────────────── 结构体 ────────────────────────────

// DefaultResponse 默认响应配置。
//
// 对应 Python: openjiuwen/core/controller/config.py (DefaultResponse)
type DefaultResponse struct {
	// Type 响应类型："text" 或 "workflow"
	Type string `json:"type"`
	// Text 文本响应内容
	Text string `json:"text,omitempty"`
}

// ControllerConfig Controller 配置。
//
// 定义 Controller 及其子组件的运行参数，分为以下几组：
//   - 任务调度：max_concurrent_tasks、schedule_interval、task_timeout
//   - 任务管理：default_task_priority、enable_task_persistence
//   - 事件队列：event_queue_size、event_timeout
//   - 意图识别：enable_intent_recognition、intent_llm_id、intent_confidence_threshold、intent_type_list
//   - 完成信号：suppress_completion_signal
//   - 流：stream_first_frame_timeout
//
// 对应 Python: openjiuwen/core/controller/config.py (ControllerConfig)
type ControllerConfig struct {
	// ─── 任务调度配置 ───

	// MaxConcurrentTasks 最大并发任务数，默认 5。0 表示无限制。
	MaxConcurrentTasks int `json:"max_concurrent_tasks"`
	// ScheduleInterval 调度间隔（秒），默认 1.0。越小响应越快但 CPU 占用越高。
	ScheduleInterval float64 `json:"schedule_interval"`
	// TaskTimeout 任务超时（秒），nil 表示无超时。默认 nil。
	TaskTimeout *float64 `json:"task_timeout,omitempty"`

	// ─── 任务管理配置 ───

	// DefaultTaskPriority 默认任务优先级，默认 1。数值越大优先级越高。
	DefaultTaskPriority int `json:"default_task_priority"`
	// EnableTaskPersistence 是否启用任务持久化，默认 false。
	EnableTaskPersistence bool `json:"enable_task_persistence"`

	// ─── 事件队列配置 ───

	// EventQueueSize 事件队列大小，默认 10000。
	EventQueueSize int `json:"event_queue_size"`
	// EventTimeout 事件处理超时（秒），默认 300。
	EventTimeout float64 `json:"event_timeout"`

	// ─── 意图识别配置 ───

	// EnableIntentRecognition 是否启用意图识别，默认 false。
	EnableIntentRecognition bool `json:"enable_intent_recognition"`
	// IntentLLMID 意图识别使用的 LLM 模型 ID，默认空。
	IntentLLMID string `json:"intent_llm_id"`
	// IntentConfidenceThreshold 意图识别置信度阈值，默认 0.7。低于此值的意图视为 UNKNOWN_TASK。
	IntentConfidenceThreshold float64 `json:"intent_confidence_threshold"`
	// IntentTypeList 支持的意图类型列表。
	IntentTypeList []string `json:"intent_type_list"`

	// ─── 默认响应配置 ───

	// DefaultResponse 默认响应
	DefaultResponse DefaultResponse `json:"default_response"`

	// ─── 完成信号配置 ───

	// SuppressCompletionSignal 是否抑制完成信号，默认 false。
	// 设为 true 时 TaskScheduler 不发送 all_tasks_processed 信号。
	SuppressCompletionSignal bool `json:"suppress_completion_signal"`

	// ─── 流配置 ───

	// StreamFirstFrameTimeout 首帧超时（秒），默认 30.0。
	StreamFirstFrameTimeout float64 `json:"stream_first_frame_timeout"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultControllerConfig 返回默认 Controller 配置。
//
// 默认值对齐 Python ControllerConfig。
func DefaultControllerConfig() *ControllerConfig {
	return &ControllerConfig{
		MaxConcurrentTasks:        5,
		ScheduleInterval:          1.0,
		TaskTimeout:               nil,
		DefaultTaskPriority:       1,
		EnableTaskPersistence:     false,
		EventQueueSize:            10000,
		EventTimeout:              300,
		EnableIntentRecognition:   false,
		IntentLLMID:              "",
		IntentConfidenceThreshold: 0.7,
		IntentTypeList:           []string{"create_task", "pause_task", "resume_task", "cancel_task", "unknown_task"},
		DefaultResponse: DefaultResponse{
			Type: "text",
		},
		SuppressCompletionSignal:  false,
		StreamFirstFrameTimeout:    30.0,
	}
}
