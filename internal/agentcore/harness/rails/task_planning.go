package rails

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// modelSwitcher 模型切换能力接口
type modelSwitcher interface {
	SwitchModel(model *llm.Model)
	GetLLM() (*llm.Model, error)
}

// deepStateLoader DeepAgent 状态加载能力接口
type deepStateLoader interface {
	LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState
}

// TaskPlanningRail 任务规划 Rail，注册 todo 工具并提供 7 个钩子。
//
// 负责五件事：
//  1. Init:               注册 todo 工具（todo_create/list/get/modify）
//  2. BeforeModelCall:    注入 todo 提示词节 + 模型切换
//  3. AfterToolCall:      刷新 todos 缓存 + 进度提醒
//  4. AfterModelCall:     累计 token 使用量
//  5. AfterTaskIteration: 从 TaskPlan 同步 todo 状态
//  6. AfterInvoke:        日志汇总 + 清理缓存
//  7. Uninit:             注销 todo 工具
//
// 对齐 Python: TaskPlanningRail (openjiuwen/harness/rails/task_planning_rail.py)
type TaskPlanningRail struct {
	DeepAgentRail
	// tools 已注册的 todo 工具列表
	tools []tool.Tool
	// todoTool TodoTool 基类，供 LoadTodos/SaveTodos/CleanupSession 调用
	todoTool *todo.TodoTool
	// enableProgressRepeat 是否注入周期性进度提醒
	enableProgressRepeat bool
	// listToolCallInterval 进度提醒的工具调用间隔次数
	listToolCallInterval int
	// toolCallCounts 会话级工具调用计数
	toolCallCounts map[string]int
	// todosCache 会话级待办缓存
	todosCache map[string][]hschema.TodoItem
	// modelSelection 模型选择映射：Model → 描述
	modelSelection map[*llm.Model]string
	// modelIDToModel 模型 ID → Model 实例映射
	modelIDToModel map[string]*llm.Model
	// usageRecords 模型使用记录：model_id → ModelUsageRecord
	usageRecords map[string]*hschema.ModelUsageRecord
	// defaultLLM 默认 LLM 实例（首次调用时捕获）
	defaultLLM *llm.Model
	// language 语言
	language string
	// agentID Agent 标识
	agentID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskPlanningRailPriority TaskPlanningRail 优先级
	// 对齐 Python: TaskPlanningRail.priority = 90
	taskPlanningRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 TaskPlanningRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*TaskPlanningRail)(nil)

// taskPlanLogComponent 日志组件标识
var taskPlanLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskPlanningOption TaskPlanningRail 构造选项函数。
type TaskPlanningOption func(*TaskPlanningRail)

// WithEnableProgressRepeat 设置是否注入周期性进度提醒。
func WithEnableProgressRepeat(enable bool) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.enableProgressRepeat = enable }
}

// WithListToolCallInterval 设置进度提醒的工具调用间隔次数。
func WithListToolCallInterval(n int) TaskPlanningOption {
	return func(r *TaskPlanningRail) {
		if n < 1 {
			n = 20
		}
		r.listToolCallInterval = n
	}
}

// WithModelSelection 设置模型选择映射。
func WithModelSelection(m map[*llm.Model]string) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.modelSelection = m }
}

// WithLanguage 设置语言。
func WithLanguage(lang string) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.language = lang }
}

// WithAgentID 设置 Agent 标识。
func WithAgentID(id string) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.agentID = id }
}

// NewTaskPlanningRail 创建 TaskPlanningRail 实例。
//
// 默认无参创建，与 Python TaskPlanningRail() 对齐。
// 用户通过 opts 传入策略参数。
// 对齐 Python: TaskPlanningRail.__init__()
func NewTaskPlanningRail(opts ...TaskPlanningOption) *TaskPlanningRail {
	r := &TaskPlanningRail{
		DeepAgentRail:        *NewDeepAgentRail(),
		listToolCallInterval: 20,
		toolCallCounts:       make(map[string]int),
		todosCache:           make(map[string][]hschema.TodoItem),
		modelSelection:       make(map[*llm.Model]string),
		modelIDToModel:       make(map[string]*llm.Model),
		usageRecords:         make(map[string]*hschema.ModelUsageRecord),
	}
	for _, opt := range opts {
		opt(r)
	}
	// 构造 modelIDToModel 映射
	// 对齐 Python L70-73: for model, desc in model_selection.items()
	for model, desc := range r.modelSelection {
		_ = desc // 描述在 buildModelSelectionString 时使用
		if model != nil && model.ClientConfig != nil && model.ClientConfig.ClientID != "" {
			r.modelIDToModel[model.ClientConfig.ClientID] = model
		}
	}
	r.WithPriority(taskPlanningRailPriority)
	return r
}

// Init 注册 todo 工具到 Agent。
//
// 对齐 Python: TaskPlanningRail.init() L77-133
func (r *TaskPlanningRail) Init(agent agentinterfaces.BaseAgent) error {
	// 对齐 Python L87-92: 检查 agent 是 DeepAgent 且有 ability_manager
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		return nil
	}
	am := agent.AbilityManager()
	if am == nil {
		return nil
	}

	// 设置 sysOperation 和 workspace
	if r.SysOperation() == nil {
		// 对齐 Python L96-97: self.set_sys_operation(agent.deep_config.sys_operation)
		if deepAgent.DeepConfig() != nil && deepAgent.DeepConfig().SysOperation != nil {
			r.SetSysOperation(deepAgent.DeepConfig().SysOperation)
		}
	}
	if r.Workspace() == nil {
		// 对齐 Python L98-99: self.set_workspace(agent.deep_config.workspace)
		if deepAgent.DeepConfig() != nil && deepAgent.DeepConfig().Workspace != nil {
			r.SetWorkspace(deepAgent.DeepConfig().Workspace)
		}
	}

	// 计算 workspaceDir 和 agentID
	// 对齐 Python L101: workspace_dir = str(self.workspace.get_node_path(WorkspaceNode.TODO))
	var workspaceDir string
	if r.Workspace() != nil {
		todoPath := r.Workspace().GetNodePath(workspace.WorkspaceNodeTODO)
		if todoPath != nil {
			workspaceDir = *todoPath
		}
	}
	// 对齐 Python L102: agent_id = getattr(getattr(agent, "card", None), "id", None)
	agentID := r.agentID
	if agentID == "" {
		if card := agent.Card(); card != nil {
			agentID = card.ID
		}
	}
	// 对齐 Python L103: language = self.system_prompt_builder.language if self.system_prompt_builder else "cn"
	language := r.language
	if language == "" {
		if sb := agent.SystemPromptBuilder(); sb != nil {
			language = sb.Language()
		} else {
			language = "cn"
		}
	}

	// 对齐 Python L105-131: 检查已有 todo 工具，已有的保留，缺失的创建
	todoToolNames := []string{"todo_create", "todo_list", "todo_get", "todo_modify"}
	foundTools := make(map[string]bool, 4)
	for _, ability := range am.List() {
		name := ability.AbilityName()
		for _, todoName := range todoToolNames {
			if name == todoName {
				foundTools[todoName] = true
				break
			}
		}
	}

	// 创建 todo 工具集（共享 TodoTool 基类实例）
	fsOp := r.SysOperation()
	if fsOp == nil || workspaceDir == "" {
		logger.Warn(taskPlanLogComponent).
			Str("event_type", "task_planning_init_skip").
			Msg("sysOperation 或 workspaceDir 为空，跳过 todo 工具注册")
		return nil
	}

	allTools, todoToolBase := todo.CreateTodosTool(workspaceDir, fsOp.Fs(), language, agentID)
	r.todoTool = &todoToolBase

	resourceMgr := runner.GetResourceMgr()

	// 对齐 Python L124-131: 只注册尚未存在的工具，已有的保留
	var registeredTools []tool.Tool
	for i, t := range allTools {
		if t == nil {
			continue
		}
		toolName := todoToolNames[i]
		if foundTools[toolName] {
			// 已存在，跳过注册但保留引用
			registeredTools = append(registeredTools, t)
			continue
		}
		// 不存在，注册新工具
		am.Add(t.Card())
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
		registeredTools = append(registeredTools, t)
	}
	r.tools = registeredTools

	logger.Debug(taskPlanLogComponent).
		Str("event_type", "task_planning_init").
		Int("tool_count", len(registeredTools)).
		Msg("TaskPlanningRail 已注册 todo 工具")
	return nil
}

// Uninit 从 Agent 注销 todo 工具。
//
// 对齐 Python: TaskPlanningRail.uninit() L135-149
func (r *TaskPlanningRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// 对齐 Python L138-139: 移除 todo 提示词节
	if sb := agent.SystemPromptBuilder(); sb != nil {
		sb.RemoveSection(sections.SectionTodo)
	}

	// 对齐 Python L140-148: 从 ability_manager 和 resource_mgr 移除工具
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	if am != nil && len(r.tools) > 0 {
		for _, t := range r.tools {
			if t != nil && t.Card() != nil {
				am.Remove(t.Card().Name)
				if resourceMgr != nil {
					_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
				}
			}
		}
	}

	r.tools = nil
	r.todoTool = nil
	return nil
}

// BeforeModelCall 在每次 LLM 调用前注入 todo 提示词节和切换模型。
//
// 对齐 Python: TaskPlanningRail.before_model_call() L153-184
func (r *TaskPlanningRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python L155-156: system_prompt_builder 为 nil 时整体 return
	sb := cbc.Agent().SystemPromptBuilder()
	if sb == nil {
		return nil
	}

	// 对齐 Python L157-165: 注入 todo 提示词节
	lang := sb.Language()
	modelSelStr := r.buildModelSelectionString()
	section := sections.BuildTodoSection(modelSelStr, lang)
	if section != nil {
		sb.AddSection(*section)
	} else {
		// 对齐 Python L165: section 为 nil 时移除旧 todo 节
		sb.RemoveSection(sections.SectionTodo)
	}

	// 对齐 Python L167-168: 若无模型选择配置则跳过模型切换
	if len(r.modelSelection) == 0 {
		return nil
	}

	// 对齐 Python L170-171: 首次调用时捕获 defaultLLM
	switcher, ok := cbc.Agent().(modelSwitcher)
	if !ok {
		logger.Warn(taskPlanLogComponent).
			Str("event_type", "task_planning_before_model_call_skip").
			Str("agent_type", fmt.Sprintf("%T", cbc.Agent())).
			Msg("agent 未实现 modelSwitcher 接口，跳过模型切换")
		return nil
	}

	if r.defaultLLM == nil {
		llmModel, err := switcher.GetLLM()
		if err == nil && llmModel != nil {
			r.defaultLLM = llmModel
		}
	}

	// 对齐 Python L173: 获取 in_progress 任务的 selected_model_id
	selectedModelID := r.getInProgressModelID(ctx, cbc)

	// 对齐 Python L175-178: 根据 selected_model_id 查找目标模型
	var targetModel *llm.Model
	if selectedModelID != "" {
		if m, found := r.modelIDToModel[selectedModelID]; found {
			targetModel = m
		}
	}
	if targetModel == nil {
		targetModel = r.defaultLLM
	}

	// 对齐 Python L180-185: 切换模型
	if targetModel != nil {
		switcher.SwitchModel(targetModel)
		logger.Debug(taskPlanLogComponent).
			Str("event_type", "task_planning_model_switched").
			Str("selected_model_id", selectedModelID).
			Msg("TaskPlanningRail 已切换模型")
	}

	return nil
}

// AfterToolCall 工具调用后刷新 todos 缓存和注入进度提醒。
//
// 对齐 Python: TaskPlanningRail.after_tool_call() L187-242
func (r *TaskPlanningRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.todoTool == nil {
		return nil
	}

	// 对齐 Python L203-212: 刷新 todos 缓存
	sess := cbc.Session()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if ok && inputs != nil && sess != nil {
		toolName := inputs.ToolName
		if toolName != "" && strings.HasPrefix(toolName, "todo_") {
			sessionID := sess.GetSessionID()
			todos, err := r.todoTool.LoadTodos(ctx, sessionID)
			if err != nil {
				logger.Debug(taskPlanLogComponent).
					Str("event_type", "task_planning_after_tool_call_refresh_failed").
					Msg("刷新 todos 缓存失败")
			} else {
				r.todosCache[sessionID] = todos
			}
		}
	}

	// 对齐 Python L214-215: 若未启用进度提醒或缺少 session/context 则跳过
	if !r.enableProgressRepeat || sess == nil {
		return nil
	}
	modelCtx := cbc.ModelContext()
	if modelCtx == nil {
		return nil
	}

	sessionID := sess.GetSessionID()

	// 对齐 Python L218-223: 累计工具调用次数，每 N 次注入进度提醒
	r.toolCallCounts[sessionID]++
	if r.toolCallCounts[sessionID]%r.listToolCallInterval != 0 {
		return nil
	}

	// 对齐 Python L225-229: 加载当前 todos
	todos, err := r.todoTool.LoadTodos(ctx, sessionID)
	if err != nil {
		logger.Debug(taskPlanLogComponent).
			Str("event_type", "task_planning_after_tool_call_load_failed").
			Msg("加载 todos 失败，跳过进度提醒")
		return nil
	}
	if len(todos) == 0 {
		return nil
	}

	// 对齐 Python L234-241: 构造进度提醒并注入消息
	tasksStr, inProgressTask := r.formatTaskContent(todos)
	var lang string
	if sb := cbc.Agent().SystemPromptBuilder(); sb != nil {
		lang = sb.Language()
	} else {
		lang = "cn"
	}
	prompt := sections.BuildProgressReminderUserPrompt(tasksStr, inProgressTask, lang)

	// 对齐 Python L240-242: 向上下文注入 UserMessage
	userMsg := llmschema.NewUserMessage(prompt)
	_, _ = modelCtx.AddMessages(ctx, userMsg)

	return nil
}

// AfterModelCall 在 LLM 调用后累计 token 使用量。
//
// 对齐 Python: TaskPlanningRail.after_model_call() L244-262
func (r *TaskPlanningRail) AfterModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python L246-249: 获取当前模型
	switcher, ok := cbc.Agent().(modelSwitcher)
	if !ok {
		return nil
	}
	llmModel, err := switcher.GetLLM()
	if err != nil || llmModel == nil {
		return nil
	}

	// 对齐 Python L249: model_id = use_model.model_client_config.client_id
	var modelID string
	if llmModel.ClientConfig != nil {
		modelID = llmModel.ClientConfig.ClientID
	}

	// 对齐 Python L250-253: 从响应中获取 UsageMetadata
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs == nil || inputs.Response == nil {
		return nil
	}
	usage := inputs.Response.UsageMetadata
	if usage == nil {
		return nil
	}

	// 对齐 Python L255-258: 提取 token 数
	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens
	if inputTokens == 0 && outputTokens == 0 {
		return nil
	}

	// 对齐 Python L260-262: 累加到 usageRecords
	if _, exists := r.usageRecords[modelID]; !exists {
		r.usageRecords[modelID] = &hschema.ModelUsageRecord{ModelID: modelID}
	}
	r.usageRecords[modelID].Add(inputTokens, outputTokens)

	return nil
}

// AfterTaskIteration 从 TaskPlan 同步 todo 状态到持久化文件。
//
// 对齐 Python: TaskPlanningRail.after_task_iteration() L288-317 + _sync_todos_from_plan() L319-399
func (r *TaskPlanningRail) AfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.syncTodosFromPlan(ctx, cbc)
	return nil
}

// AfterInvoke 日志汇总 token 使用量并清理缓存。
//
// 对齐 Python: TaskPlanningRail.after_invoke() L264-286
func (r *TaskPlanningRail) AfterInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python L266-269: 日志汇总 token 使用量
	for modelID, record := range r.usageRecords {
		logger.Info(taskPlanLogComponent).
			Str("event_type", "task_planning_token_usage").
			Str("model_id", modelID).
			Int("input_tokens", record.InputTokens).
			Int("output_tokens", record.OutputTokens).
			Msg("TaskPlanningRail token 使用量")
	}
	r.usageRecords = make(map[string]*hschema.ModelUsageRecord)

	// 对齐 Python L271-273: 获取 sessionID
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	sessionID := sess.GetSessionID()

	// 对齐 Python L275-278: 清理 todos 缓存
	delete(r.todosCache, sessionID)

	// 对齐 Python L279-281: 清理工具调用计数
	delete(r.toolCallCounts, sessionID)

	// 对齐 Python L283-286: 清理会话资源
	if r.todoTool != nil {
		r.todoTool.CleanupSession(sessionID)
	}

	return nil
}

// GetCallbacks 覆盖基类回调映射，声明 TaskPlanningRail 的 7 个钩子。
//
// 对齐 Python: TaskPlanningRail 隐式覆盖 init/uninit/before_model_call/after_tool_call/after_model_call/after_task_iteration/after_invoke
func (r *TaskPlanningRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	// 覆盖基础事件（Init/Uninit 由 RegisterRail 框架调用，不在回调映射中）
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	// 覆盖 DeepAgent 扩展事件
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = func(ctx context.Context, railCtx any) error {
		return r.AfterTaskIteration(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getInProgressModelID 查找 in_progress 任务的 selected_model_id。
//
// 对齐 Python: TaskPlanningRail._get_in_progress_model_id() L294-317
func (r *TaskPlanningRail) getInProgressModelID(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) string {
	sess := cbc.Session()
	if sess == nil {
		return ""
	}
	if r.todoTool == nil {
		return ""
	}
	sessionID := sess.GetSessionID()

	// 优先使用缓存
	todos, cached := r.todosCache[sessionID]
	if !cached {
		loaded, err := r.todoTool.LoadTodos(ctx, sessionID)
		if err != nil {
			return ""
		}
		todos = loaded
		r.todosCache[sessionID] = todos
	}

	// 对齐 Python L314-317: 查找 in_progress 任务
	for _, item := range todos {
		if item.Status == hschema.TodoStatusInProgress {
			return item.SelectedModelID
		}
	}
	return ""
}

// formatTaskContent 格式化 todos 为可读的任务内容和进度描述。
//
// 返回 (tasks, in_progress_task)，对齐 Python: TaskPlanningRail._format_task_content() L382-401
func (r *TaskPlanningRail) formatTaskContent(todos []hschema.TodoItem) (string, string) {
	var lines []string
	inProgressStr := ""
	for _, item := range todos {
		if item.Status == hschema.TodoStatusInProgress {
			inProgressStr = item.Content
		}
		line := fmt.Sprintf("id: %s |status: %s |content: %s", item.ID, item.Status.String(), item.Content)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), inProgressStr
}

// syncTodosFromPlan 从 TaskPlan 同步 todo 状态到持久化文件。
//
// 对齐 Python: TaskPlanningRail._sync_todos_from_plan() L319-399
func (r *TaskPlanningRail) syncTodosFromPlan(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	sess := cbc.Session()
	if sess == nil {
		return
	}

	// 对齐 Python L330-331: 获取 TaskPlan
	loader, ok := cbc.Agent().(deepStateLoader)
	if !ok {
		logger.Debug(taskPlanLogComponent).
			Str("event_type", "task_planning_sync_todos_skip").
			Str("agent_type", fmt.Sprintf("%T", cbc.Agent())).
			Msg("agent 未实现 deepStateLoader 接口，跳过同步")
		return
	}
	state := loader.LoadState(sess)
	if state == nil || state.TaskPlan == nil || len(state.TaskPlan.Tasks) == 0 {
		return
	}

	if r.todoTool == nil {
		return
	}
	sessionID := sess.GetSessionID()

	// 对齐 Python L341-345: 加载当前 todos
	todos, err := r.todoTool.LoadTodos(ctx, sessionID)
	if err != nil {
		logger.Debug(taskPlanLogComponent).
			Str("event_type", "task_planning_sync_todo_no_todos").
			Msg("无 todos 可同步")
		return
	}
	if len(todos) == 0 {
		return
	}

	// 对齐 Python L350-353: 构建状态映射
	statusByTaskID := make(map[string]hschema.TodoStatus, len(state.TaskPlan.Tasks))
	for _, task := range state.TaskPlan.Tasks {
		statusByTaskID[task.ID] = task.Status
	}

	// 对齐 Python L354-363: 比较并更新状态
	changed := false
	for i := range todos {
		desired, exists := statusByTaskID[todos[i].ID]
		if !exists {
			continue
		}
		if todos[i].Status != desired {
			todos[i].Status = desired
			changed = true
		}
	}
	if !changed {
		return
	}

	// 对齐 Python L367-371: 保存更新后的 todos
	if err := r.todoTool.SaveTodos(ctx, sessionID, todos); err != nil {
		logger.Warn(taskPlanLogComponent).
			Str("event_type", "task_planning_sync_todo_save_failed").
			Err(err).
			Msg("同步 todos 保存失败")
		return
	}

	logger.Info(taskPlanLogComponent).
		Str("event_type", "task_planning_sync_todos_from_plan").
		Int("todo_count", len(todos)).
		Msg("TaskPlanningRail 已从 TaskPlan 同步 todos")
}

// buildModelSelectionString 将 modelSelection 映射格式化为模型列表字符串。
//
// 对齐 Python: build_model_selection_prompt() 中 model_list_lines 构建逻辑
func (r *TaskPlanningRail) buildModelSelectionString() string {
	if len(r.modelSelection) == 0 {
		return ""
	}
	var lines []string
	for model, desc := range r.modelSelection {
		if model != nil && model.ClientConfig != nil && model.ClientConfig.ClientID != "" {
			lines = append(lines, fmt.Sprintf(" -selected_model_id: %s: %s", model.ClientConfig.ClientID, desc))
		}
	}
	return strings.Join(lines, "\n")
}
