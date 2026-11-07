package rails

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

// modelSwitcher 模型切换接口
type modelSwitcher interface {
	SetLLM(model *llm.Model)
	GetLLM() (*llm.Model, error)
}

// deepStateLoader 深度状态加载接口
type deepStateLoader interface {
	LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState
}

// TaskPlanningRail 任务规划护栏，管理 todo 工具、进度提醒和模型切换。
// Python: TaskPlanningRail (task_planning_rail.py)
type TaskPlanningRail struct {
	DeepAgentRail
	// tools 已注册的 todo 工具列表
	tools []tool.Tool
	// todoTool TodoTool 基类，供 LoadTodos/SaveTodos/CleanupSession 调用
	todoTool *todo.TodoTool
	// injectPrompt 是否注入常驻 todo 提示节（默认 true，对齐上游构造器；
	// jiuwenswarm adapter 装配时显式关闭）
	injectPrompt bool
	// enableProgressRepeat 是否注入周期性进度提醒
	enableProgressRepeat bool
	// listToolCallInterval 进度提醒的工具调用间隔次数
	listToolCallInterval int
	// toolCallCounts 会话级工具调用计数
	toolCallCounts map[string]int
	// todosCache 会话级待办缓存（受 todosMu 保护）
	todosCache map[string][]hschema.TodoItem
	// todosMu 保护 todosCache
	todosMu sync.Mutex
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
	// todoModifyFactory todo_modify 工具工厂（nil 时原生构造）
	// swarm 层 todo_compat 兼容层注入点，等价 Python install_todo_modify_compat_patch()
	todoModifyFactory todo.TodoModifyToolFactory
	// todoCardBuilder todo 工具卡片构建器（nil 时走注册表默认文案）
	// code 模式注入 CC 对齐文案，等价 Python CodeTodo*Tool 子类覆盖卡片
	todoCardBuilder todo.TodoCardBuilder
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskPlanningOption TaskPlanningRail 可选配置函数
type TaskPlanningOption func(*TaskPlanningRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskPlanningRailPriority TaskPlanningRail 优先级
	// Python: TaskPlanningRail.priority = 90
	taskPlanningRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 TaskPlanningRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*TaskPlanningRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithInjectPrompt 设置是否注入常驻 todo 提示节。
// 默认 true，对齐上游构造器签名 `TaskPlanningRail(inject_prompt=True)`
// （task_planning_rail.py:49）；jiuwenswarm 的 DeepAdapter 显式传 False
// （interface_deep.py:10756），由 adapter 装配点负责关闭。
func WithInjectPrompt(inject bool) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.injectPrompt = inject }
}

// WithTodoCardBuilder 注入 todo 工具卡片构建器（code 模式用 CC 对齐文案）。
func WithTodoCardBuilder(builder todo.TodoCardBuilder) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.todoCardBuilder = builder }
}

// WithEnableProgressRepeat 设置是否注入周期性进度提醒
func WithEnableProgressRepeat(enable bool) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.enableProgressRepeat = enable }
}

// WithListToolCallInterval 设置进度提醒的工具调用间隔次数（最小为 1，默认 20）
func WithListToolCallInterval(n int) TaskPlanningOption {
	return func(r *TaskPlanningRail) {
		if n < 1 {
			n = 20
		}
		r.listToolCallInterval = n
	}
}

// WithModelSelection 设置模型选择映射
func WithModelSelection(m map[*llm.Model]string) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.modelSelection = m }
}

// WithLanguage 设置语言
func WithLanguage(lang string) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.language = lang }
}

// WithTodoModifyFactory 设置 todo_modify 工具工厂。
// swarm 层 todo_compat 兼容层通过此选项注入 CompatibleTodoModifyTool。
func WithTodoModifyFactory(f todo.TodoModifyToolFactory) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.todoModifyFactory = f }
}

// WithAgentID 设置 Agent 标识
func WithAgentID(id string) TaskPlanningOption {
	return func(r *TaskPlanningRail) { r.agentID = id }
}

// NewTaskPlanningRail 创建任务规划护栏实例。
// Python: TaskPlanningRail.__init__()（inject_prompt 默认 True，由装配方按需关闭）
func NewTaskPlanningRail(opts ...TaskPlanningOption) *TaskPlanningRail {
	r := &TaskPlanningRail{
		DeepAgentRail:        *NewDeepAgentRail(),
		injectPrompt:         true,
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
	// Python: L70-73: for model, desc in model_selection.items()
	for model, desc := range r.modelSelection {
		_ = desc // 描述在 buildModelSelectionString 时使用
		if model != nil && model.ClientConfig != nil && model.ClientConfig.ClientID != "" {
			r.modelIDToModel[model.ClientConfig.ClientID] = model
		}
	}
	r.WithPriority(taskPlanningRailPriority)
	return r
}

// Init 绑定 Agent 的待办工具与模型列表，并初始化 rail 运行态。
// Agent 不是 DeepAgent 或缺少能力管理器时直接跳过（不参与规划增强）。
func (r *TaskPlanningRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	// Python: L87-92: 检查 agent 是 DeepAgent 且有 ability_manager
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
		// Python: L96-97: self.set_sys_operation(agent.deep_config.sys_operation)
		if deepAgent.DeepConfig() != nil && deepAgent.DeepConfig().SysOperation != nil {
			r.SetSysOperation(deepAgent.DeepConfig().SysOperation)
		}
	}
	if r.Workspace() == nil {
		// Python: L98-99: self.set_workspace(agent.deep_config.workspace)
		if deepAgent.DeepConfig() != nil && deepAgent.DeepConfig().Workspace != nil {
			r.SetWorkspace(deepAgent.DeepConfig().Workspace)
		}
	}

	// 计算 workspaceDir 和 agentID
	// Python: L101: workspace_dir = str(self.workspace.get_node_path(WorkspaceNode.TODO))
	var workspaceDir string
	if r.Workspace() != nil {
		todoPath := r.Workspace().GetNodePath(workspace.WorkspaceNodeTODO)
		if todoPath != nil {
			workspaceDir = *todoPath
		}
	}
	// Python: L102: agent_id = getattr(getattr(agent, "card", None), "id", None)
	agentID := r.agentID
	if agentID == "" {
		if card := agent.Card(); card != nil {
			agentID = card.ID
		}
	}
	// Python: L103: language = self.system_prompt_builder.language if self.system_prompt_builder else "cn"
	language := r.language
	if language == "" {
		if sb := agent.SystemPromptBuilder(); sb != nil {
			language = sb.Language()
		} else {
			language = "cn"
		}
	}

	// Python: L105-131: 检查已有 todo 工具，已有的保留，缺失的创建
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
		logger.Warn(logComponent).
			Str("event_type", "task_planning_init_skip").
			Msg("sysOperation or workspaceDir is empty, skipping todo tool registration")
		return nil
	}

	allTools, todoToolBase := todo.CreateTodosToolWithCardBuilder(workspaceDir, fsOp.Fs(), language, agentID, r.todoCardBuilder, r.todoModifyFactory)
	r.todoTool = &todoToolBase

	resourceMgr := runner.GetResourceMgr()

	// Python: L124-131: 只注册尚未存在的工具，已有的保留
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

	logger.Debug(logComponent).
		Str("event_type", "task_planning_init").
		Int("tool_count", len(registeredTools)).
		Msg("TaskPlanningRail registered todo tool")
	return nil
}

// Uninit 解绑 Agent 上的规划增强，把注入过的待办段落从提示词里移除。
func (r *TaskPlanningRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// Python: L138-139: 移除 todo 提示词节
	if sb := agent.SystemPromptBuilder(); sb != nil {
		sb.RemoveSection(sections.SectionTodo)
	}

	// Python: L140-148: 从 ability_manager 和 resource_mgr 移除工具
	// Python: try/except：添加 recover 保护，防止注销工具时 panic
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	if am != nil && len(r.tools) > 0 {
		for _, t := range r.tools {
			func(t tool.Tool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(logComponent).
							Str("event_type", "task_planning_uninit").
							Str("tool_name", func() string {
								if t != nil && t.Card() != nil {
									return t.Card().Name
								}
								return ""
							}()).
							Msgf("failed to unregister tool: %v", rec)
					}
				}()
				if t != nil && t.Card() != nil {
					am.Remove(t.Card().Name)
					if resourceMgr != nil {
						_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
					}
				}
			}(t)
		}
	}

	r.tools = nil
	r.todoTool = nil
	return nil
}

// BeforeModelCall 模型调用前注入待办清单段落，并按进行中的任务决定是否切换模型。
func (r *TaskPlanningRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: L155-156: system_prompt_builder 为 nil 时整体 return
	sb := cbc.Agent().SystemPromptBuilder()
	if sb == nil {
		return nil
	}

	// Python: L157-165: 注入 todo 提示词节（inject_prompt=False 时只注册工具，不注入）
	lang := sb.Language()
	if r.injectPrompt {
		modelSelStr := r.buildModelSelectionString()
		section := sections.BuildTodoSection(modelSelStr, lang)
		if section != nil {
			sb.AddSection(*section)
		} else {
			// Python: L165: section 为 nil 时移除旧 todo 节
			sb.RemoveSection(sections.SectionTodo)
		}
	} else {
		sb.RemoveSection(sections.SectionTodo)
	}

	// Python: L167-185: 按 in_progress 任务切换模型（缓存读；code rail 用 freshLoad）
	r.SwitchModelIfNeeded(ctx, cbc, false)

	return nil
}

// AfterToolCall 工具调用后刷新待办缓存，让下一轮提示词看到最新的任务状态。
func (r *TaskPlanningRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.todoTool == nil {
		return nil
	}

	// Python: L203-212: 刷新 todos 缓存
	sess := cbc.Session()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if ok && inputs != nil && sess != nil {
		toolName := inputs.ToolName
		if toolName != "" && strings.HasPrefix(toolName, "todo_") {
			sessionID := sess.GetSessionID()
			todos, err := r.todoTool.LoadTodos(ctx, sessionID)
			if err != nil {
				logger.Debug(logComponent).
					Str("event_type", "task_planning_after_tool_call_refresh_failed").
					Msg("failed to refresh todos cache")
			} else {
				r.todosMu.Lock()
				r.todosCache[sessionID] = todos
				r.todosMu.Unlock()
			}
		}
	}

	// Python: L214-215: 若未启用进度提醒或缺少 session/context 则跳过
	if !r.enableProgressRepeat || sess == nil {
		return nil
	}
	modelCtx := cbc.ModelContext()
	if modelCtx == nil {
		return nil
	}

	sessionID := sess.GetSessionID()

	// Python: L218-223: 累计工具调用次数，每 N 次注入进度提醒
	r.toolCallCounts[sessionID]++
	if r.toolCallCounts[sessionID]%r.listToolCallInterval != 0 {
		return nil
	}

	// Python: L225-229: 加载当前 todos
	todos, err := r.todoTool.LoadTodos(ctx, sessionID)
	if err != nil {
		logger.Debug(logComponent).
			Str("event_type", "task_planning_after_tool_call_load_failed").
			Msg("failed to load todos, skipping progress reminder")
		return nil
	}
	if len(todos) == 0 {
		return nil
	}

	// Python: L234-241: 构造进度提醒并注入消息
	tasksStr, inProgressTask := r.formatTaskContent(todos)
	var lang string
	if sb := cbc.Agent().SystemPromptBuilder(); sb != nil {
		lang = sb.Language()
	} else {
		lang = "cn"
	}
	prompt := sections.BuildProgressReminderUserPrompt(tasksStr, inProgressTask, lang)

	// Python: L240-242: 向上下文注入 UserMessage
	// Python: 获取消息列表并追加用户消息
	// 使用 GetMessages+append+SetMessages 而非 AddMessages，对齐 Python 纯数据操作不触发处理器链
	userMsg := llmschema.NewUserMessage(prompt)
	messages, _ := modelCtx.GetMessages(0, true)
	messages = append(messages, userMsg)
	modelCtx.SetMessages(messages, true)

	return nil
}

// AfterModelCall 模型返回后把计划同步进待办列表（模型可能在回复里改了计划）。
func (r *TaskPlanningRail) AfterModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: L246-249: 获取当前模型
	switcher, ok := cbc.Agent().(modelSwitcher)
	if !ok {
		return nil
	}
	llmModel, err := switcher.GetLLM()
	if err != nil || llmModel == nil {
		return nil
	}

	// Python: L249: model_id = use_model.model_client_config.client_id
	var modelID string
	if llmModel.ClientConfig != nil {
		modelID = llmModel.ClientConfig.ClientID
	}

	// Python: L250-253: 从响应中获取 UsageMetadata
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs == nil || inputs.Response == nil {
		return nil
	}
	usage := inputs.Response.UsageMetadata
	if usage == nil {
		return nil
	}

	// Python: L255-258: 提取 token 数
	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens
	if inputTokens == 0 && outputTokens == 0 {
		return nil
	}

	// Python: L260-262: 累加到 usageRecords
	if _, exists := r.usageRecords[modelID]; !exists {
		r.usageRecords[modelID] = &hschema.ModelUsageRecord{ModelID: modelID}
	}
	r.usageRecords[modelID].Add(inputTokens, outputTokens)

	return nil
}

func (r *TaskPlanningRail) AfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.syncTodosFromPlan(ctx, cbc)
	return nil
}

// AfterInvoke 本轮结束时汇总各模型的 token 用量并记日志。
func (r *TaskPlanningRail) AfterInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: L266-269: 日志汇总 token 使用量
	for modelID, record := range r.usageRecords {
		logger.Info(logComponent).
			Str("event_type", "task_planning_token_usage").
			Str("model_id", modelID).
			Int("input_tokens", record.InputTokens).
			Int("output_tokens", record.OutputTokens).
			Msg("TaskPlanningRail token usage")
	}
	r.usageRecords = make(map[string]*hschema.ModelUsageRecord)

	// Python: L271-273: 获取 sessionID
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	sessionID := sess.GetSessionID()

	// Python: L275-278: 清理 todos 缓存
	r.todosMu.Lock()
	delete(r.todosCache, sessionID)
	r.todosMu.Unlock()

	// Python: L279-281: 清理工具调用计数
	delete(r.toolCallCounts, sessionID)

	// Python: L283-286: 清理会话资源
	if r.todoTool != nil {
		r.todoTool.CleanupSession(sessionID)
	}

	return nil
}

// GetCallbacks 返回本 rail 关心的事件到回调函数的映射，供框架注册。
// Init/Uninit 由注册框架直接调用，不在映射里。
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

// SwitchModelIfNeeded 按 in_progress 待办的 selected_model_id 切换模型。
// freshLoad=true 时每次都从存储重读待办（code rail 行为，对齐
// CodeTaskPlanningRail._get_fresh_in_progress_model_id），否则读缓存。
// Python: TaskPlanningRail.before_model_call L167-185
func (r *TaskPlanningRail) SwitchModelIfNeeded(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, freshLoad bool) {
	if len(r.modelSelection) == 0 || cbc == nil || cbc.Agent() == nil {
		return
	}
	switcher, ok := cbc.Agent().(modelSwitcher)
	if !ok {
		logger.Warn(logComponent).
			Str("event_type", "task_planning_before_model_call_skip").
			Str("agent_type", fmt.Sprintf("%T", cbc.Agent())).
			Msg("agent does not implement modelSwitcher interface, skipping model switch")
		return
	}
	if r.defaultLLM == nil {
		llmModel, err := switcher.GetLLM()
		if err == nil && llmModel != nil {
			r.defaultLLM = llmModel
		}
	}

	var selectedModelID string
	if freshLoad {
		selectedModelID = r.getFreshInProgressModelID(ctx, cbc)
	} else {
		selectedModelID = r.getInProgressModelID(ctx, cbc)
	}

	var targetModel *llm.Model
	if selectedModelID != "" {
		if m, found := r.modelIDToModel[selectedModelID]; found {
			targetModel = m
		}
	}
	if targetModel == nil {
		targetModel = r.defaultLLM
	}
	if targetModel != nil {
		switcher.SetLLM(targetModel)
		logger.Debug(logComponent).
			Str("event_type", "task_planning_model_switched").
			Str("selected_model_id", selectedModelID).
			Msg("TaskPlanningRail switched model")
	}
}

// HasTodoTool 是否已注册 todo 工具基类。
func (r *TaskPlanningRail) HasTodoTool() bool { return r.todoTool != nil }

// InjectPromptEnabled 是否注入常驻 todo 提示节。
func (r *TaskPlanningRail) InjectPromptEnabled() bool { return r.injectPrompt }

// BindTodoTool 绑定 todo 工具基类（自定义装配/子类 rail 单测注入用）。
func (r *TaskPlanningRail) BindTodoTool(t *todo.TodoTool) { r.todoTool = t }

// LoadTodosFresh 从存储重读会话待办并刷新缓存。
// Python: CodeTaskPlanningRail._load_fresh_todos
func (r *TaskPlanningRail) LoadTodosFresh(ctx context.Context, sessionID string) []hschema.TodoItem {
	if r.todoTool == nil || sessionID == "" {
		return nil
	}
	todos, err := r.todoTool.LoadTodos(ctx, sessionID)
	if err != nil {
		logger.Debug(logComponent).
			Str("event_type", "task_planning_load_fresh_todos_failed").
			Str("session_id", sessionID).
			Msg("failed to load fresh todos")
		return nil
	}
	r.todosMu.Lock()
	r.todosCache[sessionID] = todos
	r.todosMu.Unlock()
	return todos
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getFreshInProgressModelID 从存储重读待办后取 in_progress 任务的 selected_model_id。
// Python: CodeTaskPlanningRail._get_fresh_in_progress_model_id
func (r *TaskPlanningRail) getFreshInProgressModelID(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) string {
	sess := cbc.Session()
	if sess == nil {
		return ""
	}
	for _, item := range r.LoadTodosFresh(ctx, sess.GetSessionID()) {
		if item.Status == hschema.TodoStatusInProgress {
			return item.SelectedModelID
		}
	}
	return ""
}

// getInProgressModelID 取当前进行中任务指定的模型 ID；没有待办工具或没有进行中任务时返回空串。
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
	r.todosMu.Lock()
	todos, cached := r.todosCache[sessionID]
	r.todosMu.Unlock()
	if !cached {
		loaded, err := r.todoTool.LoadTodos(ctx, sessionID)
		if err != nil {
			return ""
		}
		todos = loaded
		r.todosMu.Lock()
		r.todosCache[sessionID] = todos
		r.todosMu.Unlock()
	}

	// Python: L314-317: 查找 in_progress 任务
	for _, item := range todos {
		if item.Status == hschema.TodoStatusInProgress {
			return item.SelectedModelID
		}
	}
	return ""
}

// formatTaskContent 把待办列表格式化成提示词里的逐行文本，并顺带取出进行中任务的内容。
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

// syncTodosFromPlan 把模型回复里的计划项同步进待办列表，保持两边一致。
func (r *TaskPlanningRail) syncTodosFromPlan(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	sess := cbc.Session()
	if sess == nil {
		return
	}

	// Python: L330-331: 获取 TaskPlan
	loader, ok := cbc.Agent().(deepStateLoader)
	if !ok {
		logger.Debug(logComponent).
			Str("event_type", "task_planning_sync_todos_skip").
			Str("agent_type", fmt.Sprintf("%T", cbc.Agent())).
			Msg("agent does not implement deepStateLoader interface, skipping sync")
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

	// Python: L341-345: 加载当前 todos
	todos, err := r.todoTool.LoadTodos(ctx, sessionID)
	if err != nil {
		logger.Debug(logComponent).
			Str("event_type", "task_planning_sync_todo_no_todos").
			Msg("no todos to sync")
		return
	}
	if len(todos) == 0 {
		return
	}

	// Python: L350-353: 构建状态映射
	statusByTaskID := make(map[string]hschema.TodoStatus, len(state.TaskPlan.Tasks))
	for _, task := range state.TaskPlan.Tasks {
		statusByTaskID[task.ID] = task.Status
	}

	// Python: L354-363: 比较并更新状态
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

	// Python: L367-371: 保存更新后的 todos
	if err := r.todoTool.SaveTodos(ctx, sessionID, todos); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "task_planning_sync_todo_save_failed").
			Err(err).
			Msg("failed to save synced todos")
		return
	}

	logger.Info(logComponent).
		Str("event_type", "task_planning_sync_todos_from_plan").
		Int("todo_count", len(todos)).
		Msg("TaskPlanningRail synced todos from TaskPlan")
}

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
