package modules

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IntentToolkits 意图识别工具集，提供 8 个 OpenAI Tool Schema 和对应方法。
// 低置信度时自动转为 unknown_task 意图。
// 对应 Python: openjiuwen/core/controller/modules/intent_toolkits.py::IntentToolkits
type IntentToolkits struct {
	// event 关联的输入事件
	event schema.Event
	// confidenceThreshold 置信度阈值
	confidenceThreshold float64
	// toolSchemaChoices 工具 Schema 映射：tool name → OpenAI tool schema
	toolSchemaChoices map[string]map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntentToolkits 创建意图工具集。
// 对应 Python: IntentToolkits.__init__(event, confidence_threshold)
func NewIntentToolkits(event schema.Event, confidenceThreshold float64) *IntentToolkits {
	t := &IntentToolkits{
		event:               event,
		confidenceThreshold: confidenceThreshold,
		toolSchemaChoices:   make(map[string]map[string]any),
	}
	t.initToolSchemaChoices()
	return t
}

// CreateTask 创建任务意图。
// 对应 Python: IntentToolkits.create_task(confidence, task_description)
func (t *IntentToolkits) CreateTask(confidence float64, taskDescription string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	targetTaskID := uuid.New().String()
	intent, err := schema.NewIntent(
		schema.IntentCreateTask,
		t.event,
		schema.WithTargetTaskID(targetTaskID),
		schema.WithTargetTaskDescription(taskDescription),
		schema.WithDependTaskID([]string{}),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := fmt.Sprintf("任务 ID: %s, 任务描述: %s, 当前状态: 已创建并提交执行", targetTaskID, taskDescription)
	return intent, result, nil
}

// PauseTask 暂停任务意图。
// 对应 Python: IntentToolkits.pause_task(confidence, task_id)
func (t *IntentToolkits) PauseTask(confidence float64, taskID string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	intent, err := schema.NewIntent(
		schema.IntentPauseTask,
		t.event,
		schema.WithTargetTaskID(taskID),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := fmt.Sprintf("任务 ID: %s, 当前状态: 已暂停", taskID)
	return intent, result, nil
}

// CancelTask 取消任务意图。
// 对应 Python: IntentToolkits.cancel_task(confidence, task_id)
func (t *IntentToolkits) CancelTask(confidence float64, taskID string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	intent, err := schema.NewIntent(
		schema.IntentCancelTask,
		t.event,
		schema.WithTargetTaskID(taskID),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := fmt.Sprintf("任务 ID: %s, 当前状态: 已取消", taskID)
	return intent, result, nil
}

// ResumeTask 恢复任务意图。
// 对应 Python: IntentToolkits.resume_task(confidence, task_id)
func (t *IntentToolkits) ResumeTask(confidence float64, taskID string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	intent, err := schema.NewIntent(
		schema.IntentResumeTask,
		t.event,
		schema.WithTargetTaskID(taskID),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := fmt.Sprintf("任务 ID: %s, 当前状态: 已恢复", taskID)
	return intent, result, nil
}

// UnknownTask 未知任务意图。
// 对应 Python: IntentToolkits.unknown_task(confidence, question_for_user)
func (t *IntentToolkits) UnknownTask(confidence float64, questionForUser string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	intent, err := schema.NewIntent(
		schema.IntentUnknownTask,
		t.event,
		schema.WithConfidence(confidence),
		schema.WithClarificationPrompt(questionForUser),
	)
	if err != nil {
		return nil, "", err
	}
	result := "请求已发送，等待用户响应。"
	return intent, result, nil
}

// CreateDependentTask 创建依赖任务意图。
// 对应 Python: IntentToolkits.create_dependent_task(confidence, task_description, dependent_task_ids)
func (t *IntentToolkits) CreateDependentTask(confidence float64, taskDescription string, dependentTaskIDs []string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	targetTaskID := uuid.New().String()
	intent, err := schema.NewIntent(
		schema.IntentContinueTask,
		t.event,
		schema.WithTargetTaskID(targetTaskID),
		schema.WithTargetTaskDescription(taskDescription),
		schema.WithDependTaskID(dependentTaskIDs),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := fmt.Sprintf("任务 ID: %s, 任务描述: %s, 当前状态: 已创建并提交执行", targetTaskID, taskDescription)
	return intent, result, nil
}

// ModifyTask 修改任务意图。
// 对应 Python: IntentToolkits.modify_task(confidence, task_id, new_task_description)
func (t *IntentToolkits) ModifyTask(confidence float64, taskID string, newTaskDescription string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	targetTaskID := uuid.New().String()
	intent, err := schema.NewIntent(
		schema.IntentModifyTask,
		t.event,
		schema.WithTargetTaskID(targetTaskID),
		schema.WithTargetTaskDescription(newTaskDescription),
		schema.WithDependTaskID([]string{taskID}),
		schema.WithModificationDetails(newTaskDescription),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := fmt.Sprintf("任务 ID: %s, 任务描述: %s, 当前状态: 已创建并提交执行", targetTaskID, newTaskDescription)
	return intent, result, nil
}

// SupplementTask 补充任务意图。
// 对应 Python: IntentToolkits.supplement_task(confidence, task_id, supplement_info)
func (t *IntentToolkits) SupplementTask(confidence float64, taskID string, supplementInfo string) (*schema.Intent, string, error) {
	if confidence < t.confidenceThreshold {
		intent, result := t.lowConfidenceIntent(confidence)
		return intent, result, nil
	}
	intent, err := schema.NewIntent(
		schema.IntentSupplementTask,
		t.event,
		schema.WithTargetTaskID(taskID),
		schema.WithSupplementaryInfo(supplementInfo),
		schema.WithConfidence(confidence),
	)
	if err != nil {
		return nil, "", err
	}
	result := "任务补充信息已提交。"
	return intent, result, nil
}

// GetOpenAIToolSchemas 获取 OpenAI Tool Schema 列表。
// 对应 Python: IntentToolkits.get_openai_tool_schemas(choices)
func (t *IntentToolkits) GetOpenAIToolSchemas(choices ...string) []map[string]any {
	if len(choices) == 0 {
		result := make([]map[string]any, 0, len(t.toolSchemaChoices))
		for _, v := range t.toolSchemaChoices {
			result = append(result, v)
		}
		return result
	}
	result := make([]map[string]any, 0, len(choices))
	for _, c := range choices {
		if v, ok := t.toolSchemaChoices[c]; ok {
			result = append(result, v)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lowConfidenceIntent 低置信度自动转 unknown_task 意图。
// 对应 Python: IntentToolkits._low_confidence_intent(confidence)
func (t *IntentToolkits) lowConfidenceIntent(confidence float64) (*schema.Intent, string) {
	intent, _ := schema.NewIntent(
		schema.IntentUnknownTask,
		t.event,
		schema.WithConfidence(confidence),
		schema.WithClarificationPrompt(
			"抱歉，无法理解您的意图。请明确是要创建新任务还是修改已有任务。",
		),
	)
	result := "由于置信度较低，自动转换为 unknown_task"
	return intent, result
}

// initToolSchemaChoices 初始化 8 个 OpenAI Tool Schema。
// 对应 Python: IntentToolkits.__init__ 中 self._tool_schema_choices 赋值
func (t *IntentToolkits) initToolSchemaChoices() {
	t.toolSchemaChoices = map[string]map[string]any{
		"create_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "create_task",
				"description": "Create a new task. Use this method when the user wants to start a new task or activity.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_description": map[string]any{
							"type":        "string",
							"description": "Detailed description of the task, specifying what the user wants to accomplish",
						},
						"dependent_task_id": map[string]any{
							"type":        "string",
							"description": "Optional parameter specifying the ID of the predecessor task on which this task depends, used for task dependencies",
						},
					},
					"required":             []string{"confidence", "task_description"},
					"additionalProperties": false,
				},
			},
		},
		"pause_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "pause_task",
				"description": "Pause a specific task. Use when the user wants to temporarily interrupt or suspend an ongoing task.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_id": map[string]any{
							"type":        "string",
							"description": "Unique identifier of the task to be paused",
						},
					},
					"required":             []string{"confidence", "task_id"},
					"additionalProperties": false,
				},
			},
		},
		"cancel_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "cancel_task",
				"description": "Cancel a specific task. Use when the user wants to completely terminate or abandon a task.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_id": map[string]any{
							"type":        "string",
							"description": "Unique identifier of the task to be canceled",
						},
					},
					"required":             []string{"confidence", "task_id"},
					"additionalProperties": false,
				},
			},
		},
		"resume_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "resume_task",
				"description": "Resume a specific task. Use when the user wants to continue a previously paused or interrupted task.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_id": map[string]any{
							"type":        "string",
							"description": "Unique identifier of the task to be resumed",
						},
					},
					"required":             []string{"confidence", "task_id"},
					"additionalProperties": false,
				},
			},
		},
		"unknown_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "unknown_task",
				"description": "Handle unknown or ambiguous user intents. Use this method when the exact user intent cannot be determined to create clarification questions for the user.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"question_for_user": map[string]any{
							"type":        "string",
							"description": "Clarification question to ask the user to obtain more information to determine the exact intent",
						},
					},
					"required":             []string{"confidence", "question_for_user"},
					"additionalProperties": false,
				},
			},
		},
		"create_dependent_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "create_dependent_task",
				"description": "Create a new task that depends on one or more existing tasks. Use when the user wants to start a task that requires completion of other tasks first.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_description": map[string]any{
							"type":        "string",
							"description": "Detailed description of the dependent task",
						},
						"dependent_task_ids": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "List of task IDs that this task depends on",
						},
					},
					"required":             []string{"confidence", "task_description", "dependent_task_ids"},
					"additionalProperties": false,
				},
			},
		},
		"modify_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "modify_task",
				"description": "Modify an existing task by creating a new version with updated description. Use when the user wants to change the details or requirements of an existing task.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_id": map[string]any{
							"type":        "string",
							"description": "Unique identifier of the task to be modified",
						},
						"new_task_description": map[string]any{
							"type":        "string",
							"description": "Updated description for the task",
						},
					},
					"required":             []string{"confidence", "task_id", "new_task_description"},
					"additionalProperties": false,
				},
			},
		},
		"supplement_task": {
			"type": "function",
			"function": map[string]any{
				"name":        "supplement_task",
				"description": "Add supplementary information to an existing task. Use when the user wants to provide additional details or context for an ongoing task without changing its core description.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confidence": map[string]any{
							"type":        "number",
							"description": "Your confidence score for this operation (0-1.0), typically used when confidence is low",
						},
						"task_id": map[string]any{
							"type":        "string",
							"description": "Unique identifier of the task to be supplemented",
						},
						"supplement_info": map[string]any{
							"type":        "string",
							"description": "Additional information or context to add to the task",
						},
					},
					"required":             []string{"confidence", "task_id", "supplement_info"},
					"additionalProperties": false,
				},
			},
		},
	}
}
