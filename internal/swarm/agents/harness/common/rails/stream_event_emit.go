package rails

import (
	"context"
	"strconv"
	"strings"
	"time"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	usage_snapshot "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/usage"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// todoToolLoader TodoTool 的最小接口，用于依赖注入和可测试性
//
// 通过接口而非具体类型引用，允许测试中注入 fake 实现，避免真实 TodoTool 的外部依赖。
type todoToolLoader interface {
	LoadTodos(ctx context.Context, sessionID string) ([]hschema.TodoItem, error)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// EmitToolCall 发送 tool_call 事件，对齐 Python: _emit_tool_call
//
// 在 before_model_call 钩子中调用，通知前端 Agent 正在发起工具调用。
func emitToolCall(ctx context.Context, session sessioninterfaces.SessionFacade, toolCall *llmschema.ToolCall) {
	if session == nil {
		return
	}
	payload := map[string]any{
		"tool_call": map[string]any{
			"name":         toolCallName(toolCall),
			"arguments":    toolCallArguments(toolCall),
			"tool_call_id": toolCallID(toolCall),
		},
	}
	if err := session.WriteStream(ctx, &stream.OutputSchema{
		Type:    "tool_call",
		Index:   0,
		Payload: payload,
	}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("tool_call emit failed")
	}
}

// EmitToolResult 发送 tool_result 事件，对齐 Python: _emit_tool_result
//
// 在 after_tool_call 钩子中调用，通知前端工具执行结果。
func emitToolResult(ctx context.Context, session sessioninterfaces.SessionFacade, toolCall *llmschema.ToolCall, result any) {
	if session == nil {
		return
	}
	rawOutput := structuredToolResultPayload(result)
	// 展示文本：nil → ""、string 原样、其余紧凑 JSON。
	// 曾是 fmt.Sprintf("%v")，Go 对 map 打印成 `map[k:v]`，前端与日志里全是
	// `map[...]` 前缀；改用 utils.PyText（展示专用，不参与机器解析）。
	resultText := truncateString(utils.PyText(result), 60000)
	toolResultPayload := map[string]any{
		"tool_name":    toolCallName(toolCall),
		"tool_call_id": toolCallID(toolCall),
		"result":       resultText,
	}
	if rawOutput != nil {
		toolResultPayload["raw_output"] = rawOutput
	}
	errorState := inferToolResultError(rawOutput)
	if rawOutput == nil {
		errorState = inferToolResultError(result)
	}
	if errorState != nil {
		toolResultPayload["success"] = !*errorState
		if *errorState {
			toolResultPayload["status"] = "error"
			toolResultPayload["is_error"] = true
		}
	}
	if err := session.WriteStream(ctx, &stream.OutputSchema{
		Type:    "tool_result",
		Index:   0,
		Payload: map[string]any{"tool_result": toolResultPayload},
	}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("tool_result emit failed")
	}
}

// EmitToolUpdate 发送 tool_update 事件，对齐 Python: _emit_tool_update
//
// 在工具执行过程中发送状态更新（如 in_progress/completed/cancelled）。
func emitToolUpdate(ctx context.Context, session sessioninterfaces.SessionFacade, toolCall *llmschema.ToolCall, status string) {
	if session == nil {
		return
	}
	s := strings.TrimSpace(status)
	if s == "" {
		s = "in_progress"
	}
	payload := map[string]any{
		"tool_update": map[string]any{
			"tool_name":    toolCallName(toolCall),
			"tool_call_id": toolCallID(toolCall),
			"arguments":    toolCallArguments(toolCall),
			"status":       s,
		},
	}
	if err := session.WriteStream(ctx, &stream.OutputSchema{
		Type:    "tool_update",
		Index:   0,
		Payload: payload,
	}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("tool_update emit failed")
	}
}

// EmitAskUserQuestionIfInterrupted 当 ask_user 工具被中断时发送 chat.ask_user_question 事件，
// 对齐 Python: _emit_ask_user_question_if_interrupted
func emitAskUserQuestionIfInterrupted(ctx context.Context, session sessioninterfaces.SessionFacade, toolCall *llmschema.ToolCall, toolName string, result any, exception error) {
	if session == nil {
		return
	}
	if strings.TrimSpace(toolName) != "ask_user" {
		return
	}
	interrupt := extractToolInterrupt(result)
	if interrupt == nil {
		interrupt = extractToolInterrupt(exception)
	}
	if interrupt == nil {
		return
	}
	payload := askUserQuestionPayloadFromInterrupt(toolCall, interrupt)
	if payload == nil {
		logger.Debug(logComponent).Msg("ask_user interrupt payload unavailable")
		return
	}
	if err := session.WriteStream(ctx, &stream.OutputSchema{
		Type:    "chat.ask_user_question",
		Index:   0,
		Payload: payload,
	}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("ask_user question emit failed")
	}
}

// EmitTodoUpdated 加载主 Agent 的 todo 列表并推送 todo.updated 事件，对齐 Python: _emit_todo_updated
func emitTodoUpdated(ctx context.Context, session sessioninterfaces.SessionFacade, todoTool todoToolLoader, sessionID string) {
	if session == nil || todoTool == nil {
		return
	}
	todosData, err := todoTool.LoadTodos(ctx, sessionID)
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("Failed to load todos")
		return
	}
	todos := FormatTodosForFrontend(todosData)
	if err := session.WriteStream(ctx, &stream.OutputSchema{
		Type:    "todo.updated",
		Index:   0,
		Payload: map[string]any{"todos": todos},
	}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("todo.updated emit failed")
	}
}

// EmitContextUsage 发送 context.usage 事件，对齐 Python: _emit_context_usage
//
// 报告当前上下文窗口使用率（context_max、tokens_used、rate）。
func emitContextUsage(ctx context.Context, cbCtx *sainterfaces.AgentCallbackContext) {
	session := cbCtx.Session()
	if session == nil {
		return
	}
	modelCtx := cbCtx.ModelContext()
	if modelCtx == nil {
		return
	}

	// 获取模型名称与上下文引擎配置，对齐 Python: agent._config.model_name
	var modelName string
	var ceConfig ceschema.ContextEngineConfig
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debug(logComponent).Any("recover", r).Msg("Failed to get model_name from ctx.agent")
			}
		}()
		if agent := cbCtx.Agent(); agent != nil {
			if config := agent.Config(); config != nil {
				modelName = config.ModelName()
				ceConfig = config.GetContextEngineConfig()
			}
		}
	}()

	// 计算上下文窗口上限：优先级 fallback > 模型映射 > 内置表 > 200000。
	// 对齐 Python: ContextUtils.resolve_context_max(model_name=model_name,
	//                fallback_context_window_tokens=..., model_context_window_tokens=...)
	fallbackWindow := ceConfig.ContextWindowTokens
	if fallbackWindow == 0 {
		fallbackWindow = ceConfig.ModelContextWindowTokensOverride
	}
	limitTokens := cecontext.ResolveContextMax(
		modelName, fallbackWindow, ceConfig.ModelContextWindowTokens)

	// 分类用量：系统提示词 / 工具 / 消息（技能提示词计入系统消息，单列 0）
	stats := modelCtx.Statistic()
	systemTokens := 0
	toolsTokens := 0
	totalTokens := 0
	if stats != nil {
		systemTokens = stats.SystemMessageTokens
		toolsTokens = stats.ToolTokens
		totalTokens = stats.TotalTokens
	}
	messageTokens := totalTokens - systemTokens - toolsTokens
	if messageTokens < 0 {
		messageTokens = 0
	}

	// provider 上报的用量（权威总量）
	var usage *llmschema.UsageMetadata
	if inputs := cbCtx.Inputs(); inputs != nil {
		if mcInputs, ok := inputs.(*sainterfaces.ModelCallInputs); ok && mcInputs.Response != nil {
			usage = mcInputs.Response.UsageMetadata
		}
	}

	snapshot := usage_snapshot.BuildSnapshot(usage_snapshot.SnapshotInput{
		Phase: usage_snapshot.PhasePostCall,
		// 前端只要求非空字符串；用"会话 id + 纳秒"保证同会话内可区分轮次
		RequestID:          session.GetSessionID() + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		SessionID:          session.GetSessionID(),
		ProductSessionID:   session.GetSessionID(),
		LimitTokens:        limitTokens,
		SystemPromptTokens: systemTokens,
		ToolsTokens:        toolsTokens,
		MessagesTokens:     messageTokens,
		Usage:              usage,
	})

	if err := session.WriteStream(ctx, &stream.OutputSchema{
		Type:    usage_snapshot.EventTypeContextUsage,
		Index:   0,
		Payload: snapshot,
	}); err != nil {
		logger.Debug(logComponent).Err(err).Msg("context_usage emit failed")
	}
}

// FormatTodosForFrontend 将 TodoItem 列表格式化为前端兼容格式，对齐 Python: _format_todos_for_frontend
//
// 映射内部 TodoStatus 到前端 status 字符串，过滤已取消项。
// 导出供 adapter 等跨包消费方复用，避免各自复制一份映射逻辑。
func FormatTodosForFrontend(todosData []hschema.TodoItem) []map[string]any {
	statusMapping := map[hschema.TodoStatus]string{
		hschema.TodoStatusPending:    "pending",
		hschema.TodoStatusInProgress: "in_progress",
		hschema.TodoStatusCompleted:  "completed",
	}

	result := make([]map[string]any, 0, len(todosData))
	for _, item := range todosData {
		if item.Status == hschema.TodoStatusCancelled {
			continue
		}
		statusStr, ok := statusMapping[item.Status]
		if !ok {
			statusStr = item.Status.String()
		}
		result = append(result, map[string]any{
			"id":         item.ID,
			"content":    item.Content,
			"activeForm": item.ActiveForm,
			"status":     statusStr,
		})
	}
	return result
}

// contextUsageTokensUsed 计算 context.usage 的分子：usage_metadata.total_tokens。
//
// 取 total_tokens（输入 + 输出）而非仅输入侧：本轮输出会在下一轮作为历史重新
// 发给模型，所以「下一轮真实占用」应把输出算进去（2026-11-03 决策，取代原
// 设计 D6 的输入侧口径，与 Python usage_metadata.total_tokens 一致）。
func contextUsageTokensUsed(usage *llmschema.UsageMetadata) int {
	if usage == nil {
		return 0
	}
	return usage.TotalTokens
}
