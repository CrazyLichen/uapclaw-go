package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Task 任务实体，Controller 领域的核心执行单元。
//
// 对应 Python: openjiuwen/core/controller/schema/task.py (Task)
type Task struct {
	// SessionID 会话ID
	SessionID string `json:"session_id"`
	// TaskID 任务唯一标识
	TaskID string `json:"task_id"`
	// TaskType 任务类型
	TaskType string `json:"task_type"`
	// Description 任务描述
	Description string `json:"description,omitempty"`
	// Priority 优先级
	Priority int `json:"priority"`
	// Inputs 输入事件列表（自定义 JSON 序列化以支持多态）
	Inputs []Event `json:"inputs,omitempty"`
	// Outputs 输出分片列表
	Outputs []*stream.OutputSchema `json:"outputs,omitempty"`
	// Status 任务状态
	Status TaskStatus `json:"status"`
	// ParentTaskID 父任务ID
	ParentTaskID string `json:"parent_task_id,omitempty"`
	// ContextID 上下文ID
	ContextID string `json:"context_id,omitempty"`
	// InputRequiredFields 需要用户输入的字段
	InputRequiredFields map[string]any `json:"input_required_fields,omitempty"`
	// ErrorMessage 错误消息
	ErrorMessage string `json:"error_message,omitempty"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
	// Extensions 扩展字段
	Extensions map[string]any `json:"extensions,omitempty"`
}

// taskJSON Task 的 JSON 序列化中间结构。
type taskJSON struct {
	SessionID           string                   `json:"session_id"`
	TaskID              string                   `json:"task_id"`
	TaskType            string                   `json:"task_type"`
	Description         string                   `json:"description,omitempty"`
	Priority            int                      `json:"priority"`
	Inputs              eventSlice               `json:"inputs,omitempty"`
	Outputs             []*stream.OutputSchema   `json:"outputs,omitempty"`
	Status              TaskStatus               `json:"status"`
	ParentTaskID        string                   `json:"parent_task_id,omitempty"`
	ContextID           string                   `json:"context_id,omitempty"`
	InputRequiredFields map[string]any           `json:"input_required_fields,omitempty"`
	ErrorMessage        string                   `json:"error_message,omitempty"`
	Metadata            map[string]any           `json:"metadata,omitempty"`
	Extensions          map[string]any           `json:"extensions,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTask 创建新的 Task 实例，自动生成 TaskID。
//
// 对应 Python: Task.task_id = Field(default_factory=lambda: str(uuid.uuid4()))
func NewTask(sessionID, taskType string) *Task {
	return &Task{
		SessionID: sessionID,
		TaskID:    uuid.NewString(),
		TaskType:  taskType,
		Priority:  0,
		Status:    TaskSubmitted,
		Metadata:  map[string]any{},
	}
}

// ValidateTask 校验 Task 字段合法性，对齐 Python @field_validator + @model_validator。
//
// 校验规则：
//   - task_id 非空
//   - session_id 非空
//   - task_type 非空
//   - priority >= 0
//   - parent_task_id 不能等于 task_id（自引用检查）
//   - 状态为 failed 时 error_message 非空
//   - 状态为 input-required 时 input_required_fields 非空
func ValidateTask(task *Task) error {
	var reasons []string

	if strings.TrimSpace(task.TaskID) == "" {
		reasons = append(reasons, "task_id 不能为空")
	}
	if strings.TrimSpace(task.SessionID) == "" {
		reasons = append(reasons, "session_id 不能为空")
	}
	if strings.TrimSpace(task.TaskType) == "" {
		reasons = append(reasons, "task_type 不能为空")
	}
	if task.Priority < 0 {
		reasons = append(reasons, "priority 不能为负数")
	}
	if task.ParentTaskID != "" && task.ParentTaskID == task.TaskID {
		reasons = append(reasons, "parent_task_id 不能等于 task_id（自引用）")
	}
	if task.Status == TaskFailed && strings.TrimSpace(task.ErrorMessage) == "" {
		reasons = append(reasons, "状态为 failed 时 error_message 不能为空")
	}
	if task.Status == TaskInputRequired && len(task.InputRequiredFields) == 0 {
		reasons = append(reasons, "状态为 input-required 时 input_required_fields 不能为空")
	}

	if len(reasons) > 0 {
		return exception.NewBaseError(exception.StatusAgentControllerTaskParamError,
			exception.WithMsg(strings.Join(reasons, "; ")),
		)
	}
	return nil
}

// MarshalJSON 实现 json.Marshaler，支持 Task 的多态 Event Inputs 序列化。
func (t *Task) MarshalJSON() ([]byte, error) {
	return json.Marshal(&taskJSON{
		SessionID:           t.SessionID,
		TaskID:              t.TaskID,
		TaskType:            t.TaskType,
		Description:         t.Description,
		Priority:            t.Priority,
		Inputs:              eventSlice(t.Inputs),
		Outputs:             t.Outputs,
		Status:              t.Status,
		ParentTaskID:        t.ParentTaskID,
		ContextID:           t.ContextID,
		InputRequiredFields: t.InputRequiredFields,
		ErrorMessage:        t.ErrorMessage,
		Metadata:            t.Metadata,
		Extensions:          t.Extensions,
	})
}

// UnmarshalJSON 实现 json.Unmarshaler，支持 Task 的多态 Event Inputs 反序列化。
func (t *Task) UnmarshalJSON(data []byte) error {
	var j taskJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	t.SessionID = j.SessionID
	t.TaskID = j.TaskID
	t.TaskType = j.TaskType
	t.Description = j.Description
	t.Priority = j.Priority
	t.Inputs = []Event(j.Inputs)
	t.Outputs = j.Outputs
	t.Status = j.Status
	t.ParentTaskID = j.ParentTaskID
	t.ContextID = j.ContextID
	t.InputRequiredFields = j.InputRequiredFields
	t.ErrorMessage = j.ErrorMessage
	t.Metadata = j.Metadata
	t.Extensions = j.Extensions
	return nil
}

// String 实现 fmt.Stringer，返回 Task 的简要信息。
func (t *Task) String() string {
	return fmt.Sprintf("Task(task_id=%s, session_id=%s, type=%s, status=%s)", t.TaskID, t.SessionID, t.TaskType, t.Status)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
