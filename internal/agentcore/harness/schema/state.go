package schema

// ──────────────────────────── 结构体 ────────────────────────────

// PlanModeState 规划模式会话级状态
type PlanModeState struct {
	// Mode 当前 Agent 模式（"normal" 或 "plan"）
	Mode string `json:"mode"`
	// PrePlanMode 进入规划模式前的模式
	PrePlanMode string `json:"pre_plan_mode"`
	// PlanSlug 活跃计划文件的短标识符（如 "gleaming-brewing-phoenix"）
	PlanSlug string `json:"plan_slug,omitempty"`
	// PromptContext 旧版提示词特化标记，为旧检查点保留
	PromptContext string `json:"prompt_context,omitempty"`
}

// DeepAgentState 单次调用可变状态
//
// 该对象在 invoke/stream 请求运行期间存在于 ctx.session 上，
// 其可序列化子集可以检查点到会话状态。
type DeepAgentState struct {
	// Iteration 当前迭代次数
	Iteration int `json:"iteration"`
	// TaskPlan 任务计划
	TaskPlan *TaskPlan `json:"task_plan,omitempty"`
	// StopConditionState 停止条件状态
	StopConditionState map[string]any `json:"stop_condition_state,omitempty"`
	// PendingFollowUps 待跟进项列表
	PendingFollowUps []string `json:"pending_follow_ups,omitempty"`
	// PlanMode 规划模式状态
	PlanMode PlanModeState `json:"plan_mode"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionStateKey 会话状态字典中的 DeepAgent 状态键
	sessionStateKey = "deepagent"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlanModeState 创建带默认值的 PlanModeState
func NewPlanModeState() PlanModeState {
	return PlanModeState{
		Mode:        "normal",
		PrePlanMode: "normal",
	}
}

// ToDict 将 PlanModeState 序列化为 JSON 友好的字典
func (s PlanModeState) ToDict() map[string]any {
	return map[string]any{
		"mode":           s.Mode,
		"pre_plan_mode":  s.PrePlanMode,
		"plan_slug":      s.PlanSlug,
		"prompt_context": s.PromptContext,
	}
}

// FromDict 从序列化字典恢复 PlanModeState，nil 输入返回默认值
func (PlanModeState) FromDict(data map[string]any) PlanModeState {
	if data == nil {
		return NewPlanModeState()
	}
	prePlanMode, _ := data["pre_plan_mode"].(string)
	planSlug, _ := data["plan_slug"].(string)
	promptContext, _ := data["prompt_context"].(string)
	return PlanModeState{
		Mode:          strVal(data, "mode", "normal"),
		PrePlanMode:   prePlanMode,
		PlanSlug:      planSlug,
		PromptContext: promptContext,
	}
}

// NewDeepAgentState 创建带默认值的 DeepAgentState
func NewDeepAgentState() DeepAgentState {
	return DeepAgentState{
		Iteration: 0,
		PlanMode:  NewPlanModeState(),
	}
}

// ToSessionDict 将 DeepAgentState 序列化为 JSON 友好的字典，包装在 deepagent 键下
func (s DeepAgentState) ToSessionDict() map[string]any {
	inner := map[string]any{
		"iteration":            s.Iteration,
		"stop_condition_state": s.StopConditionState,
		"pending_follow_ups":   s.PendingFollowUps,
		"plan_mode":            s.PlanMode.ToDict(),
	}
	if s.TaskPlan != nil {
		inner["task_plan"] = s.TaskPlan.ToDict()
	}
	return map[string]any{
		sessionStateKey: inner,
	}
}

// FromSessionDict 从会话快照字典恢复 DeepAgentState，nil 输入返回默认值
func (DeepAgentState) FromSessionDict(data map[string]any) DeepAgentState {
	if data == nil {
		return NewDeepAgentState()
	}
	inner, ok := data[sessionStateKey].(map[string]any)
	if !ok {
		return NewDeepAgentState()
	}

	// 解析 task_plan
	var taskPlan *TaskPlan
	if rawPlan, ok := inner["task_plan"].(map[string]any); ok {
		tp := TaskPlanFromDict(rawPlan)
		taskPlan = &tp
	}

	// 解析 iteration
	iteration := intVal(inner, "iteration", 0)

	// 解析 stop_condition_state
	var stopCondState map[string]any
	if v, ok := inner["stop_condition_state"].(map[string]any); ok {
		stopCondState = v
	}

	// 解析 pending_follow_ups
	var pendingFollowUps []string
	pendingFollowUps = parseStringSlice(inner, "pending_follow_ups")

	// 解析 plan_mode
	var planMode PlanModeState
	if v, ok := inner["plan_mode"].(map[string]any); ok {
		planMode = PlanModeState{}.FromDict(v)
	} else {
		planMode = NewPlanModeState()
	}

	return DeepAgentState{
		Iteration:          iteration,
		TaskPlan:           taskPlan,
		StopConditionState: stopCondState,
		PendingFollowUps:   pendingFollowUps,
		PlanMode:           planMode,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strVal 从字典中获取字符串值，键不存在时返回默认值
func strVal(data map[string]any, key string, defaultVal string) string {
	if v, ok := data[key].(string); ok {
		return v
	}
	return defaultVal
}

// intVal 从字典中获取整数值，键不存在时返回默认值
func intVal(data map[string]any, key string, defaultVal int) int {
	switch v := data[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	default:
		return defaultVal
	}
}

// parseStringSlice 从字典中获取字符串切片，兼容 []string 和 []any 两种类型
func parseStringSlice(data map[string]any, key string) []string {
	switch v := data[key].(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}
