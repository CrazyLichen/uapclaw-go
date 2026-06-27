package schema

// ──────────────────────────── 结构体 ────────────────────────────

// Intent 意图，描述用户对任务的操作意图。
//
// 对应 Python: openjiuwen/core/controller/schema/intent.py (Intent)
type Intent struct {
	// IntentType 意图类型
	IntentType IntentType `json:"intent_type"`
	// TaskID 关联的任务ID
	TaskID string `json:"task_id,omitempty"`
	// SessionID 关联的会话ID
	SessionID string `json:"session_id,omitempty"`
	// Data 意图附带的数据
	Data map[string]any `json:"data,omitempty"`
	// Params 意图参数
	Params map[string]any `json:"params,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// IntentType 意图类型枚举，定义所有支持的用户意图。
//
// 对应 Python: openjiuwen/core/controller/schema/intent.py (IntentType)
type IntentType string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// IntentCreateTask 创建任务
	IntentCreateTask IntentType = "create_task"
	// IntentPauseTask 暂停任务
	IntentPauseTask IntentType = "pause_task"
	// IntentResumeTask 恢复任务
	IntentResumeTask IntentType = "resume_task"
	// IntentContinueTask 继续任务
	IntentContinueTask IntentType = "continue_task"
	// IntentSupplementTask 补充任务
	IntentSupplementTask IntentType = "supplement_task"
	// IntentCancelTask 取消任务
	IntentCancelTask IntentType = "cancel_task"
	// IntentModifyTask 修改任务
	IntentModifyTask IntentType = "modify_task"
	// IntentSwitchTask 切换任务
	// ⤵️ 预留：Python 中 SWITCH_TASK 已定义但意图识别处理器未实现
	IntentSwitchTask IntentType = "switch_task"
	// IntentUnknownTask 未知意图
	IntentUnknownTask IntentType = "unknown_task"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
