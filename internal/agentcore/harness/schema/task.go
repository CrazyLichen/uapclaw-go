package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TodoItem 任务计划中的单个待办项
type TodoItem struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Content 内容描述
	Content string `json:"content"`
	// ActiveForm 进行中表述
	ActiveForm string `json:"active_form"`
	// Description 详细说明
	Description string `json:"description"`
	// Status 当前状态
	Status TodoStatus `json:"status"`
	// DependsOn 依赖的任务 ID 列表
	DependsOn []string `json:"depends_on,omitempty"`
	// ResultSummary 结果摘要
	ResultSummary string `json:"result_summary,omitempty"`
	// MetaData 元数据
	MetaData map[string]any `json:"meta_data,omitempty"`
	// SelectedModelID 选定的模型标识
	SelectedModelID string `json:"selected_model_id,omitempty"`
}

// TaskPlan 任务计划
type TaskPlan struct {
	// TaskName 任务名称
	TaskName string `json:"task_name"`
	// Goal 目标描述
	Goal string `json:"goal"`
	// Tasks 任务列表
	Tasks []TodoItem `json:"tasks,omitempty"`
	// CurrentTaskID 当前执行中的任务标识
	CurrentTaskID string `json:"current_task_id,omitempty"`
}

// ModelUsageRecord 模型使用记录
type ModelUsageRecord struct {
	// ModelID 模型标识
	ModelID string `json:"model_id"`
	// InputTokens 输入 token 数
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 token 数
	OutputTokens int `json:"output_tokens"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// TodoStatus 待办项状态枚举
type TodoStatus int

const (
	// TodoStatusPending 待执行
	TodoStatusPending TodoStatus = iota
	// TodoStatusInProgress 执行中
	TodoStatusInProgress
	// TodoStatusCompleted 已完成
	TodoStatusCompleted
	// TodoStatusCancelled 已取消
	TodoStatusCancelled
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// StatusIcons 状态对应的图标映射
var StatusIcons = map[TodoStatus]string{
	TodoStatusPending:    "[ ]",
	TodoStatusInProgress: "[→]",
	TodoStatusCompleted:  "[√]",
	TodoStatusCancelled:  "[×]",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseTodoStatus 从字符串解析 TodoStatus
func ParseTodoStatus(s string) (TodoStatus, error) {
	switch strings.ToLower(s) {
	case "pending":
		return TodoStatusPending, nil
	case "in_progress":
		return TodoStatusInProgress, nil
	case "completed":
		return TodoStatusCompleted, nil
	case "cancelled":
		return TodoStatusCancelled, nil
	default:
		return TodoStatusPending, fmt.Errorf("未知的 TodoStatus: %q", s)
	}
}

// NewTodoItem 创建带默认值的 TodoItem，ID 自动生成，状态默认为 Pending
func NewTodoItem() TodoItem {
	return TodoItem{
		ID:     uuid.New().String(),
		Status: TodoStatusPending,
	}
}

// ToDict 将 TodoItem 序列化为 JSON 友好的字典
func (item TodoItem) ToDict() map[string]any {
	result := map[string]any{
		"id":           item.ID,
		"content":      item.Content,
		"active_form":  item.ActiveForm,
		"description":  item.Description,
		"status":       item.Status.String(),
	}
	if len(item.DependsOn) > 0 {
		result["depends_on"] = item.DependsOn
	}
	if item.ResultSummary != "" {
		result["result_summary"] = item.ResultSummary
	}
	if item.MetaData != nil {
		result["meta_data"] = item.MetaData
	}
	if item.SelectedModelID != "" {
		result["selected_model_id"] = item.SelectedModelID
	}
	return result
}

// FromDict 从序列化字典恢复 TodoItem
func (TodoItem) FromDict(data map[string]any) TodoItem {
	item := TodoItem{
		ID:          strVal(data, "id", ""),
		Content:     strVal(data, "content", ""),
		ActiveForm:  strVal(data, "active_form", ""),
		Description: strVal(data, "description", ""),
	}
	if statusStr, ok := data["status"].(string); ok {
		if parsed, err := ParseTodoStatus(statusStr); err == nil {
			item.Status = parsed
		}
	}
	if v, ok := data["depends_on"].([]any); ok {
		for _, dep := range v {
			if s, ok := dep.(string); ok {
				item.DependsOn = append(item.DependsOn, s)
			}
		}
	}
	if v, ok := data["result_summary"].(string); ok {
		item.ResultSummary = v
	}
	if v, ok := data["meta_data"].(map[string]any); ok {
		item.MetaData = v
	}
	if v, ok := data["selected_model_id"].(string); ok {
		item.SelectedModelID = v
	}
	return item
}

// NewTaskPlan 创建带默认值的 TaskPlan
func NewTaskPlan(taskName, goal string) TaskPlan {
	return TaskPlan{
		TaskName: taskName,
		Goal:     goal,
	}
}

// GetTask 根据 ID 查找任务，未找到返回 nil
func (tp *TaskPlan) GetTask(taskID string) *TodoItem {
	for i := range tp.Tasks {
		if tp.Tasks[i].ID == taskID {
			return &tp.Tasks[i]
		}
	}
	return nil
}

// GetNextTask 获取下一个可执行的任务：状态为 Pending 且所有依赖项已完成
func (tp *TaskPlan) GetNextTask() *TodoItem {
	for i := range tp.Tasks {
		if tp.Tasks[i].Status != TodoStatusPending {
			continue
		}
		if tp.allDepsCompleted(tp.Tasks[i]) {
			return &tp.Tasks[i]
		}
	}
	return nil
}

// AddTask 向计划中添加任务
func (tp *TaskPlan) AddTask(task TodoItem) {
	tp.Tasks = append(tp.Tasks, task)
}

// MarkInProgress 将任务标记为执行中，同时更新 CurrentTaskID
func (tp *TaskPlan) MarkInProgress(taskID string) error {
	task := tp.GetTask(taskID)
	if task == nil {
		return fmt.Errorf("任务不存在: %q", taskID)
	}
	task.Status = TodoStatusInProgress
	tp.CurrentTaskID = taskID
	return nil
}

// MarkCompleted 将任务标记为已完成，并设置结果摘要
func (tp *TaskPlan) MarkCompleted(taskID string, summary string) error {
	task := tp.GetTask(taskID)
	if task == nil {
		return fmt.Errorf("任务不存在: %q", taskID)
	}
	task.Status = TodoStatusCompleted
	task.ResultSummary = summary
	return nil
}

// MarkCancelled 将任务标记为已取消，并设置取消原因
func (tp *TaskPlan) MarkCancelled(taskID string, reason string) error {
	task := tp.GetTask(taskID)
	if task == nil {
		return fmt.Errorf("任务不存在: %q", taskID)
	}
	task.Status = TodoStatusCancelled
	task.ResultSummary = reason
	return nil
}

// GetProgressSummary 返回进度摘要，如 "3/7 completed"
func (tp *TaskPlan) GetProgressSummary() string {
	completed := 0
	for _, task := range tp.Tasks {
		if task.Status == TodoStatusCompleted {
			completed++
		}
	}
	return fmt.Sprintf("%d/%d completed", completed, len(tp.Tasks))
}

// ToMarkdown 将任务计划渲染为 Markdown 格式
func (tp *TaskPlan) ToMarkdown() string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(tp.TaskName)
	sb.WriteString("\n\n**Goal:** ")
	sb.WriteString(tp.Goal)
	sb.WriteString("\n\n")
	for _, task := range tp.Tasks {
		icon, ok := StatusIcons[task.Status]
		if !ok {
			icon = "[?]"
		}
		sb.WriteString("- ")
		sb.WriteString(icon)
		sb.WriteString(" ")
		sb.WriteString(task.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ToDict 将 TaskPlan 序列化为 JSON 友好的字典
func (tp TaskPlan) ToDict() map[string]any {
	result := map[string]any{
		"task_name": tp.TaskName,
		"goal":      tp.Goal,
	}
	if len(tp.Tasks) > 0 {
		tasks := make([]any, len(tp.Tasks))
		for i, task := range tp.Tasks {
			tasks[i] = task.ToDict()
		}
		result["tasks"] = tasks
	}
	if tp.CurrentTaskID != "" {
		result["current_task_id"] = tp.CurrentTaskID
	}
	return result
}

// FromDict 从序列化字典恢复 TaskPlan
func (TaskPlan) FromDict(data map[string]any) TaskPlan {
	tp := TaskPlan{
		TaskName: strVal(data, "task_name", ""),
		Goal:     strVal(data, "goal", ""),
	}
	if v, ok := data["tasks"].([]any); ok {
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				tp.Tasks = append(tp.Tasks, TodoItem{}.FromDict(m))
			}
		}
	}
	if v, ok := data["current_task_id"].(string); ok {
		tp.CurrentTaskID = v
	}
	return tp
}

// TaskPlanFromDict 从序列化字典恢复 TaskPlan（包级导出函数，供 state.go 调用）
func TaskPlanFromDict(data map[string]any) TaskPlan {
	return TaskPlan{}.FromDict(data)
}

// Add 累加 token 使用量
func (r *ModelUsageRecord) Add(inputTokens, outputTokens int) {
	r.InputTokens += inputTokens
	r.OutputTokens += outputTokens
}

// String 返回 ModelUsageRecord 的字符串表示
func (r ModelUsageRecord) String() string {
	return fmt.Sprintf("%s: input=%d, output=%d", r.ModelID, r.InputTokens, r.OutputTokens)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// allDepsCompleted 检查任务的所有依赖项是否已完成
func (tp *TaskPlan) allDepsCompleted(task TodoItem) bool {
	for _, depID := range task.DependsOn {
		dep := tp.GetTask(depID)
		if dep == nil || dep.Status != TodoStatusCompleted {
			return false
		}
	}
	return true
}

// String 返回 TodoStatus 的字符串表示
func (s TodoStatus) String() string {
	switch s {
	case TodoStatusPending:
		return "pending"
	case TodoStatusInProgress:
		return "in_progress"
	case TodoStatusCompleted:
		return "completed"
	case TodoStatusCancelled:
		return "cancelled"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (s TodoStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (s *TodoStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("TodoStatus 应为字符串，解析失败: %w", err)
	}
	parsed, err := ParseTodoStatus(str)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}
