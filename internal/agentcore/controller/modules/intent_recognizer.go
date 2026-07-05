package modules

import (
	"context"
	"fmt"
	"sync"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"golang.org/x/sync/errgroup"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelProvider 意图识别所需的模型调用接口。
// ⤵️ 6.23 ResourceMgr 实现后回填
type ModelProvider interface {
	// Invoke 调用模型，messages 为消息列表，tools 为工具 Schema
	// 返回模型响应
	Invoke(ctx context.Context, messages []any, tools []map[string]any) (any, error)
}

// IntentRecognizer 意图识别器，负责识别用户输入中的意图，将事件转换为 Intent 对象。
// 对应 Python: openjiuwen/core/controller/modules/intent_recognizer.py::IntentRecognizer
type IntentRecognizer struct {
	// config 控制器配置
	config *config.ControllerConfig
	// taskManager 任务管理器
	taskManager *TaskManager
	// contextEngine 上下文引擎
	contextEngine iface.ContextEngine
	// modelProvider 模型提供者
	// ⤵️ 6.23 ResourceMgr 实现后回填
	modelProvider ModelProvider
	// systemMessage 系统提示词
	systemMessage string
	// userPromptTemplate 用户提示词模板
	userPromptTemplate string
}

// EventHandlerWithIntentRecognition 基于意图识别的事件处理器。
// 在 EventHandler 的基础上增加意图识别功能，根据识别出的意图调用相应的处理方法。
// 对应 Python: openjiuwen/core/controller/modules/intent_recognizer.py::EventHandlerWithIntentRecognition
type EventHandlerWithIntentRecognition struct {
	EventHandlerBase
	// recognizer 意图识别器
	recognizer *IntentRecognizer
}

// ──────────────────────────── 常量 ────────────────────────────
const logComponentIntent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntentRecognizer 创建意图识别器。
// 对应 Python: IntentRecognizer.__init__(config, task_manager, ability_manager, context_engine)
// 偏差15 修复：对齐 Python，Python 未将 ability_manager 赋值给 self，Go 同步删除
func NewIntentRecognizer(
	cfg *config.ControllerConfig,
	taskManager *TaskManager,
	contextEngine iface.ContextEngine,
) *IntentRecognizer {
	return &IntentRecognizer{
		config:        cfg,
		taskManager:   taskManager,
		contextEngine: contextEngine,
		systemMessage: `# 角色
你是一个任务管理助手，专门使用工具创建和管理任务。你的核心理念是：**任何用户请求都可以转化为一个任务**，并由任务管理器处理。

# 核心原则
1. **任务化一切**：对于任何用户请求（包括信息查询、事务处理、提醒等），你的第一反应不是直接执行或拒绝，而是思考如何将它创建为一个任务。
2. **透明管理**：如果任务需要外部能力（如天气API），你仍然创建它，并明确告知用户任务的状态。

# 工作流程
1. **解析请求**：理解用户想做什么。
2. **任务操作**：使用工具创建一个对应的任务或修改已有任务。
3. **永远不拒绝**：不声称"超出能力范围"，而是告知用户任务会由其他执行器处理。

# 任务目标
- 根据用户输入，**总是优先创建对应的任务**。
- 使用工具进行任务操作（创建、更新、列表、删除）。
- 只有纯粹闲聊或问候时不调用工具。
`,
		userPromptTemplate: `你当前拥有的任务有：
%s

当前用户的输入为：
%s

请根据你当前的任务和用户输入，进行合适的任务操作。
`,
	}
}

// Recognize 识别意图。
// 对应 Python: IntentRecognizer.recognize(event, session)
// ⤵️ 6.23 ResourceMgr 实现后回填 LLM 调用逻辑
func (r *IntentRecognizer) Recognize(ctx context.Context, event schema.Event, sess sessioninterfaces.SessionFacade) ([]*schema.Intent, error) {
	logger.Warn(logComponentIntent).Msg("IntentRecognizer.Recognize 尚未实现，⤵️ 6.23 ResourceMgr 实现后回填 LLM 调用逻辑")
	return nil, nil
}

// SetModelProvider 设置模型提供者。
// ⤵️ 6.23 回填时调用
func (r *IntentRecognizer) SetModelProvider(provider ModelProvider) {
	r.modelProvider = provider
}

// NewEventHandlerWithIntentRecognition 创建基于意图识别的事件处理器。
// 对应 Python: EventHandlerWithIntentRecognition.__init__()
func NewEventHandlerWithIntentRecognition() *EventHandlerWithIntentRecognition {
	return &EventHandlerWithIntentRecognition{}
}

// InitRecognizer 初始化意图识别器（在 EventHandlerBase 依赖注入完成后调用）。
// 对应 Python: EventHandlerWithIntentRecognition.__init__ 中 self.recognizer = IntentRecognizer(...)
func (h *EventHandlerWithIntentRecognition) InitRecognizer() {
	h.recognizer = NewIntentRecognizer(
		h.Config,
		h.TaskManager,
		h.ContextEngine,
	)
}

// HandleInput 处理输入事件，识别意图并分发。
// 对应 Python: EventHandlerWithIntentRecognition.handle_input(inputs)
// 偏差11 修复：对齐 Python，用 errgroup 并发处理意图（对应 asyncio.create_task + gather）
// 偏差12 修复：对齐 Python，返回 gather 结果列表
func (h *EventHandlerWithIntentRecognition) HandleInput(ctx context.Context, input *EventHandlerInput) (map[string]any, error) {
	if h.recognizer == nil {
		h.InitRecognizer()
	}

	intents, err := h.recognizer.Recognize(ctx, input.Event, input.Session)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: asyncio.create_task + asyncio.gather 并发处理意图
	eg, egCtx := errgroup.WithContext(ctx)

	// 收集并发执行的结果（对应 Python gather 的返回列表）
	var mu sync.Mutex
	results := make([]map[string]any, 0, len(intents))

	for i := range intents {
		intent := intents[i]
		eg.Go(func() error {
			var result map[string]any
			var procErr error

			switch intent.IntentType {
			case schema.IntentCreateTask:
				result, procErr = h.processCreateTaskIntent(egCtx, intent, input.Session)
			case schema.IntentPauseTask:
				result, procErr = h.processPauseTaskIntent(egCtx, intent, input.Session)
			case schema.IntentResumeTask:
				result, procErr = h.processResumeTaskIntent(egCtx, intent, input.Session)
			case schema.IntentContinueTask:
				result, procErr = h.processContinueTaskIntent(egCtx, intent, input.Session)
			case schema.IntentSupplementTask:
				result, procErr = h.processSupplementTaskIntent(egCtx, intent, input.Session)
			case schema.IntentCancelTask:
				result, procErr = h.processCancelTaskIntent(egCtx, intent, input.Session)
			case schema.IntentModifyTask:
				result, procErr = h.processModifyTaskIntent(egCtx, intent, input.Session)
			default:
				result, procErr = h.processUnknownTaskIntent(egCtx, intent, input.Session)
			}

			if procErr != nil {
				return procErr
			}

			if result != nil {
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return map[string]any{"results": results}, nil
}

// HandleTaskInteraction 处理任务交互事件，将 interaction 直接抛出给用户。
// 对应 Python: EventHandlerWithIntentRecognition.handle_task_interaction(inputs)
func (h *EventHandlerWithIntentRecognition) HandleTaskInteraction(ctx context.Context, input *EventHandlerInput) (map[string]any, error) {
	taskInteractionEvent, ok := input.Event.(*schema.TaskInteractionEvent)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 TaskInteractionEvent 类型，当前类型 %T", input.Event)),
		)
	}
	if err := input.Session.WriteStream(ctx, map[string]any{"interaction": taskInteractionEvent.Interaction}); err != nil {
		return nil, err
	}
	return nil, nil
}

// HandleTaskCompletion 处理任务完成事件，将结果抛出给用户。
// 对应 Python: EventHandlerWithIntentRecognition.handle_task_completion(inputs)
func (h *EventHandlerWithIntentRecognition) HandleTaskCompletion(ctx context.Context, input *EventHandlerInput) (map[string]any, error) {
	taskCompletionEvent, ok := input.Event.(*schema.TaskCompletionEvent)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 TaskCompletionEvent 类型，当前类型 %T", input.Event)),
		)
	}
	if err := input.Session.WriteStream(ctx, map[string]any{"result": taskCompletionEvent.TaskResult}); err != nil {
		return nil, err
	}
	return nil, nil
}

// HandleTaskFailed 处理任务失败事件，将错误信息抛出给用户。
// 对应 Python: EventHandlerWithIntentRecognition.handle_task_failed(inputs)
func (h *EventHandlerWithIntentRecognition) HandleTaskFailed(ctx context.Context, input *EventHandlerInput) (map[string]any, error) {
	taskFailedEvent, ok := input.Event.(*schema.TaskFailedEvent)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 TaskFailedEvent 类型，当前类型 %T", input.Event)),
		)
	}
	if err := input.Session.WriteStream(ctx, map[string]any{"error_message": taskFailedEvent.ErrorMessage}); err != nil {
		return nil, err
	}
	return nil, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// prepareUserMessage 构建用户消息。
// 对应 Python: IntentRecognizer._prepare_user_message(query)
func (r *IntentRecognizer) prepareUserMessage(ctx context.Context, query string) (string, error) {
	tasks, err := r.taskManager.GetTask(ctx, &TaskFilter{})
	if err != nil {
		return "", err
	}

	var taskPrompt string
	if len(tasks) > 0 {
		parts := make([]string, 0, len(tasks))
		for _, task := range tasks {
			parts = append(parts, fmt.Sprintf("## Task id: %s\n### Task description: %s\nStatus: %s\n", task.TaskID, task.Description, task.Status))
		}
		taskPrompt = joinStrings(parts, "\n")
	} else {
		taskPrompt = "无"
	}

	return fmt.Sprintf(r.userPromptTemplate, taskPrompt, query), nil
}

// processCreateTaskIntent 处理创建任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_create_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processCreateTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	task := schema.NewTask(sess.GetSessionID(), "default_task_type")
	task.TaskID = intent.TargetTaskID
	task.Description = intent.TargetTaskDescription
	task.Priority = 1
	task.ContextID = fmt.Sprintf("%s_%s", sess.GetSessionID(), intent.TargetTaskID)
	task.Status = schema.TaskSubmitted
	task.Metadata = intent.Metadata

	if inputEvent, ok := intent.Event.(*schema.InputEvent); ok {
		task.Inputs = []schema.Event{inputEvent}
	}

	if err := h.TaskManager.AddTask(ctx, task); err != nil {
		return nil, err
	}
	return nil, nil
}

// processPauseTaskIntent 处理暂停任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_pause_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processPauseTaskIntent(ctx context.Context, intent *schema.Intent, _ sessioninterfaces.SessionFacade) (map[string]any, error) {
	_, err := h.TaskScheduler.PauseTask(ctx, intent.TargetTaskID)
	return nil, err
}

// processResumeTaskIntent 处理恢复任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_resume_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processResumeTaskIntent(ctx context.Context, intent *schema.Intent, _ sessioninterfaces.SessionFacade) (map[string]any, error) {
	tasks, err := h.TaskManager.GetTask(ctx, &TaskFilter{TaskID: intent.TargetTaskID})
	if err != nil {
		return nil, err
	}
	if len(tasks) > 0 && tasks[0].Status == schema.TaskPaused {
		tasks[0].Status = schema.TaskSubmitted
		h.TaskManager.UpdateTask(ctx, tasks[0])
	}
	return nil, nil
}

// processContinueTaskIntent 处理接续任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_continue_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processContinueTaskIntent(ctx context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	inputEvent, ok := intent.Event.(*schema.InputEvent)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 InputEvent 类型，当前类型 %T", intent.Event)),
		)
	}

	previousEvents := make([]schema.Event, 0)
	contextIDs := make([]string, 0)
	for _, taskID := range intent.DependTaskID {
		oldTasks, err := h.TaskManager.GetTask(ctx, &TaskFilter{TaskID: taskID})
		if err != nil {
			return nil, err
		}
		if len(oldTasks) > 0 {
			previousEvents = append(previousEvents, oldTasks[0].Inputs...)
			contextIDs = append(contextIDs, oldTasks[0].ContextID)
		}
	}

	// ⤵️ 6.23 ContextEngine.GetContext 回填：将依赖任务的上下文消息附加到 InputEvent
	// Python: event.input_data.append(JsonDataFrame(data={context_id: context.get_messages() for context_id in context_ids}))
	// 当前简化处理：仅合并前置事件
	_ = contextIDs // 预留：后续回填时使用 ContextEngine 获取上下文消息
	previousEvents = append(previousEvents, inputEvent)

	task := schema.NewTask(sess.GetSessionID(), "default_task_type")
	task.TaskID = intent.TargetTaskID
	task.Description = intent.TargetTaskDescription
	task.Priority = 1
	task.ContextID = fmt.Sprintf("%s_%s", sess.GetSessionID(), intent.TargetTaskID)
	task.Inputs = previousEvents
	task.Status = schema.TaskSubmitted
	task.Metadata = intent.Metadata

	if err := h.TaskManager.AddTask(ctx, task); err != nil {
		return nil, err
	}
	return nil, nil
}

// processSupplementTaskIntent 处理补充任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_supplement_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processSupplementTaskIntent(ctx context.Context, intent *schema.Intent, _ sessioninterfaces.SessionFacade) (map[string]any, error) {
	if intent.IntentType != schema.IntentSupplementTask {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 SUPPLEMENT_TASK 类型，当前类型 %T", intent.Event)),
		)
	}

	tasks, err := h.TaskManager.GetTask(ctx, &TaskFilter{TaskID: intent.TargetTaskID})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("任务未找到: %s", intent.TargetTaskID)),
		)
	}

	if _, err := h.TaskScheduler.PauseTask(ctx, intent.TargetTaskID); err != nil {
		logger.Warn(logComponentIntent).Err(err).Str("task_id", intent.TargetTaskID).Msg("暂停任务失败，继续补充信息")
	}

	task := tasks[0]
	task.Description = task.Description + "\n\n任务补充信息:\n" + intent.SupplementaryInfo
	task.Status = schema.TaskSubmitted
	h.TaskManager.UpdateTask(ctx, task)
	return nil, nil
}

// processCancelTaskIntent 处理取消任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_cancel_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processCancelTaskIntent(ctx context.Context, intent *schema.Intent, _ sessioninterfaces.SessionFacade) (map[string]any, error) {
	if intent.IntentType != schema.IntentCancelTask {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 CANCEL_TASK 类型，当前类型 %T", intent.Event)),
		)
	}
	_, err := h.TaskScheduler.CancelTask(ctx, intent.TargetTaskID)
	return nil, err
}

// processModifyTaskIntent 处理修改任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_modify_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processModifyTaskIntent(ctx context.Context, intent *schema.Intent, _ sessioninterfaces.SessionFacade) (map[string]any, error) {
	if intent.IntentType != schema.IntentModifyTask {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("输入事件必须是 InputEvent 类型，当前类型 %T", intent.Event)),
		)
	}

	if _, err := h.TaskScheduler.CancelTask(ctx, intent.TargetTaskID); err != nil {
		logger.Warn(logComponentIntent).Err(err).Str("task_id", intent.TargetTaskID).Msg("取消旧任务失败，继续修改")
	}

	tasks, err := h.TaskManager.GetTask(ctx, &TaskFilter{TaskID: intent.TargetTaskID})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg(fmt.Sprintf("任务未找到: %s", intent.TargetTaskID)),
		)
	}

	task := tasks[0]
	task.Description = intent.TargetTaskDescription
	if task.Inputs == nil {
		task.Inputs = []schema.Event{intent.Event}
	} else {
		task.Inputs = append(task.Inputs, intent.Event)
	}
	task.Status = schema.TaskSubmitted
	h.TaskManager.UpdateTask(ctx, task)
	return nil, nil
}

// processUnknownTaskIntent 处理未知任务意图。
// 对应 Python: EventHandlerWithIntentRecognition._process_unknown_task_intent(intent, session)
func (h *EventHandlerWithIntentRecognition) processUnknownTaskIntent(_ context.Context, intent *schema.Intent, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	if intent.IntentType != schema.IntentUnknownTask {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerRuntimeError,
			exception.WithMsg("意图类型必须是 UNKNOWN_TASK"),
		)
	}
	if err := sess.WriteStream(context.Background(), map[string]any{"clarification_prompt": intent.ClarificationPrompt}); err != nil {
		return nil, err
	}
	return nil, nil
}

// joinStrings 连接字符串切片
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, s := range parts {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
