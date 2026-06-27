package schema

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Intent 意图，描述用户对任务的操作意图。
//
// 对应 Python: openjiuwen/core/controller/schema/intent.py (Intent)
type Intent struct {
	// IntentType 意图类型
	IntentType IntentType `json:"intent_type"`
	// Event 关联事件（通常为 InputEvent）
	Event Event `json:"event"`
	// TargetTaskID 目标任务ID
	TargetTaskID string `json:"target_task_id,omitempty"`
	// TargetTaskDescription 目标任务描述（CREATE_TASK/SWITCH_TASK 必需）
	TargetTaskDescription string `json:"target_task_description,omitempty"`
	// DependTaskID 依赖任务ID列表（CONTINUE_TASK 必需）
	DependTaskID []string `json:"depend_task_id,omitempty"`
	// SupplementaryInfo 补充信息（SUPPLEMENT_TASK 必需）
	SupplementaryInfo string `json:"supplementary_info,omitempty"`
	// ModificationDetails 修改详情（MODIFY_TASK 必需）
	ModificationDetails string `json:"modification_details,omitempty"`
	// Confidence 意图识别置信度，范围 [0.0, 1.0]，默认 1.0
	Confidence float64 `json:"confidence"`
	// Metadata 意图元数据
	Metadata map[string]any `json:"metadata,omitempty"`
	// ClarificationPrompt 澄清提示（UNKNOWN_TASK 必需）
	ClarificationPrompt string `json:"clarification_prompt,omitempty"`
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

// NewIntent 创建意图实例，初始化默认值并校验。
// 对齐 Python: Intent.__init__ + _post_init + _validate
func NewIntent(intentType IntentType, event Event, opts ...IntentOption) (*Intent, error) {
	i := &Intent{
		IntentType: intentType,
		Event:      event,
		Confidence: 1.0,
		Metadata:   make(map[string]any),
	}
	for _, opt := range opts {
		opt(i)
	}
	if i.Metadata == nil {
		i.Metadata = make(map[string]any)
	}
	if err := i.Validate(); err != nil {
		return nil, err
	}
	return i, nil
}

// Validate 校验意图字段是否满足类型约束。
// 对齐 Python: Intent._validate
func (i *Intent) Validate() error {
	// 校验置信度范围
	if i.Confidence < 0.0 || i.Confidence > 1.0 {
		return exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("Confidence must be between 0.0 and 1.0, got %v", i.Confidence)),
		)
	}

	// 按意图类型校验必填字段
	switch i.IntentType {
	case IntentCreateTask:
		if i.TargetTaskDescription == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("CREATE_TASK intent requires target_task_description"),
			)
		}
	case IntentContinueTask:
		if len(i.DependTaskID) == 0 {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("CONTINUE_TASK intent requires depend_task_id"),
			)
		}
	case IntentSupplementTask:
		if i.TargetTaskID == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("SUPPLEMENT_TASK intent requires target_task_id"),
			)
		}
		if i.SupplementaryInfo == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("SUPPLEMENT_TASK intent requires supplementary_info"),
			)
		}
	case IntentModifyTask:
		if i.TargetTaskID == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("MODIFY_TASK intent requires target_task_id"),
			)
		}
		if i.ModificationDetails == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("MODIFY_TASK intent requires modification_details"),
			)
		}
	case IntentPauseTask, IntentResumeTask, IntentCancelTask:
		if i.TargetTaskID == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg(fmt.Sprintf("%s intent requires target_task_id", string(i.IntentType))),
			)
		}
	case IntentSwitchTask:
		if i.TargetTaskDescription == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("SWITCH_TASK intent requires target_task_description"),
			)
		}
	case IntentUnknownTask:
		if i.ClarificationPrompt == "" {
			return exception.NewBaseError(
				exception.StatusAgentControllerRuntimeError,
				exception.WithMsg("UNKNOWN_TASK intent requires clarification_prompt"),
			)
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// IntentOption 意图可选配置函数。
type IntentOption func(*Intent)

// WithTargetTaskID 设置目标任务ID。
func WithTargetTaskID(id string) IntentOption {
	return func(i *Intent) { i.TargetTaskID = id }
}

// WithTargetTaskDescription 设置目标任务描述。
func WithTargetTaskDescription(desc string) IntentOption {
	return func(i *Intent) { i.TargetTaskDescription = desc }
}

// WithDependTaskID 设置依赖任务ID列表。
func WithDependTaskID(ids []string) IntentOption {
	return func(i *Intent) { i.DependTaskID = ids }
}

// WithSupplementaryInfo 设置补充信息。
func WithSupplementaryInfo(info string) IntentOption {
	return func(i *Intent) { i.SupplementaryInfo = info }
}

// WithModificationDetails 设置修改详情。
func WithModificationDetails(details string) IntentOption {
	return func(i *Intent) { i.ModificationDetails = details }
}

// WithConfidence 设置置信度。
func WithConfidence(c float64) IntentOption {
	return func(i *Intent) { i.Confidence = c }
}

// WithClarificationPrompt 设置澄清提示。
func WithClarificationPrompt(prompt string) IntentOption {
	return func(i *Intent) { i.ClarificationPrompt = prompt }
}
