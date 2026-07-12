package interrupt

import (
	"context"
	"fmt"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptDecision 中断决策接口，三种决策类型均实现此接口。
//
// 对齐 Python: InterruptDecision(BaseModel) — 标记基类
type InterruptDecision interface {
	isInterruptDecision()
}

// ApproveResult 允许继续执行决策。
//
// 对齐 Python: ApproveResult(InterruptDecision)
type ApproveResult struct {
	// NewArgs 可选，替换工具参数的 JSON 字符串
	NewArgs string
}

// RejectResult 拒绝执行决策，跳过工具调用并返回预设结果。
//
// 对齐 Python: RejectResult(InterruptDecision)
type RejectResult struct {
	// ToolResult 预设的工具返回结果
	ToolResult any
	// ToolMessage 可选，预设的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// InterruptResult 中断决策，暂停执行等待用户输入。
//
// 对齐 Python: InterruptResult(InterruptDecision)
type InterruptResult struct {
	// Request 中断请求接口，可存 InterruptRequest 或其子类（如 AskUserRequest）
	Request saschema.InterruptRequester
}

// resolveInterruptFn 中断解析函数签名。
// 子类通过设置此函数实现各自的 resolveInterrupt 逻辑。
type resolveInterruptFn func(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) InterruptDecision

// BaseInterruptRail 中断-恢复 Rail 基类。
//
// 在 BeforeToolCall 钩子中拦截已注册工具名的调用，
// 通过 resolveInterruptFn 获取决策（approve/reject/interrupt），
// applyDecision 执行对应动作。
//
// 子类在构造时设置 resolveInterruptFn 实现具体中断逻辑。
// Go 不支持 Python 风格的虚方法动态分派，因此使用函数字段替代。
//
// 对齐 Python: BaseInterruptRail(AgentRail) — openjiuwen/harness/rails/interrupt/interrupt_base.py
type BaseInterruptRail struct {
	agentinterfaces.BaseRail
	// toolNames 需拦截的工具名集合
	toolNames map[string]struct{}
	// resolveInterruptFn 中断解析函数，子类设置。默认：无输入→中断，有输入→允许。
	resolveInterruptFn resolveInterruptFn
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// baseInterruptRailPriority BaseInterruptRail 默认优先级
	// 对齐 Python: BaseInterruptRail.priority = 90
	baseInterruptRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 BaseInterruptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*BaseInterruptRail)(nil)

// interruptLogComponent 日志组件标识
var interruptLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseInterruptRail 创建 BaseInterruptRail 实例。
// toolNames 为需拦截的工具名列表，可为空。
//
// 对齐 Python: BaseInterruptRail.__init__(tool_names)
func NewBaseInterruptRail(toolNames ...string) *BaseInterruptRail {
	r := &BaseInterruptRail{
		BaseRail:  *agentinterfaces.NewBaseRail(),
		toolNames: make(map[string]struct{}, len(toolNames)),
	}
	for _, name := range toolNames {
		r.toolNames[name] = struct{}{}
	}
	r.WithPriority(baseInterruptRailPriority)
	// 设置默认 resolveInterruptFn：无输入→中断，有输入→允许
	r.resolveInterruptFn = r.defaultResolveInterrupt
	return r
}

// Approve 创建允许决策。
//
// 对齐 Python: BaseInterruptRail.approve(new_args)
func (r *BaseInterruptRail) Approve(newArgs string) *ApproveResult {
	return &ApproveResult{NewArgs: newArgs}
}

// Reject 创建拒绝决策。
//
// 对齐 Python: BaseInterruptRail.reject(tool_result)
func (r *BaseInterruptRail) Reject(toolResult any) *RejectResult {
	return &RejectResult{ToolResult: toolResult}
}

// Interrupt 创建中断决策。
//
// 对齐 Python: BaseInterruptRail.interrupt(request)
func (r *BaseInterruptRail) Interrupt(request saschema.InterruptRequester) *InterruptResult {
	return &InterruptResult{Request: request}
}

// AddTool 注册需拦截的工具名。
//
// 对齐 Python: BaseInterruptRail.add_tool(tool_name)
func (r *BaseInterruptRail) AddTool(toolName string) {
	r.toolNames[toolName] = struct{}{}
}

// AddTools 批量注册需拦截的工具名。
//
// 对齐 Python: BaseInterruptRail.add_tools(tool_names)
func (r *BaseInterruptRail) AddTools(toolNames []string) {
	for _, name := range toolNames {
		r.toolNames[name] = struct{}{}
	}
}

// GetTools 返回所有已注册的工具名列表。
//
// 对齐 Python: BaseInterruptRail.get_tools()
func (r *BaseInterruptRail) GetTools() []string {
	names := make([]string, 0, len(r.toolNames))
	for name := range r.toolNames {
		names = append(names, name)
	}
	return names
}

// BeforeToolCall 拦截工具调用的入口。
//
// 从 ctx.extra 提取用户输入，从 session 获取 auto_confirm 配置，
// 调用 resolveInterrupt 获取决策，然后 applyDecision 执行。
//
// 对齐 Python: BaseInterruptRail.before_tool_call(ctx)
func (r *BaseInterruptRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 获取 ToolCallInputs
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	// 检查工具名是否在拦截列表中
	if _, exists := r.toolNames[toolInputs.ToolName]; !exists {
		return nil
	}

	// 提取 toolCallID
	toolCallID := r.resolveToolCallID(toolInputs.ToolCall)

	// 提取用户输入
	userInput := r.getUserInput(cbc, toolCallID)

	// 从 session 获取 auto_confirm 配置
	var autoConfirmConfig map[string]any
	if sess := cbc.Session(); sess != nil {
		if val, err := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && val != nil {
			if cfg, ok := val.(map[string]any); ok {
				autoConfirmConfig = cfg
			}
		}
	}

	// 调用 resolveInterruptFn 获取决策
	decision := r.resolveInterruptFn(ctx, cbc, toolInputs.ToolCall, userInput, autoConfirmConfig)

	// 执行决策
	r.applyDecision(cbc, toolInputs, decision)

	return nil
}

// GetCallbacks 覆盖基类回调映射，注册 BeforeToolCall。
//
// BaseInterruptRail 嵌入 BaseRail（非 DeepAgentRail），只需注册基础事件。
func (r *BaseInterruptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isInterruptDecision 实现 InterruptDecision 接口标记方法。
func (a *ApproveResult) isInterruptDecision() {}

// isInterruptDecision 实现 InterruptDecision 接口标记方法。
func (r *RejectResult) isInterruptDecision() {}

// isInterruptDecision 实现 InterruptDecision 接口标记方法。
func (i *InterruptResult) isInterruptDecision() {}

// defaultResolveInterrupt 默认中断解析逻辑：无用户输入→中断；有输入→允许。
//
// 对齐 Python: BaseInterruptRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config)
func (r *BaseInterruptRail) defaultResolveInterrupt(
	_ context.Context,
	_ *agentinterfaces.AgentCallbackContext,
	_ *llmschema.ToolCall,
	userInput any,
	_ map[string]any,
) InterruptDecision {
	if userInput == nil {
		return r.Interrupt(&saschema.InterruptRequest{
			Message: "等待用户确认",
		})
	}
	return r.Approve("")
}

// applyDecision 根据决策类型执行对应动作。
//
// 对齐 Python: BaseInterruptRail._apply_decision(ctx, tool_call, tool_name, decision)
func (r *BaseInterruptRail) applyDecision(
	cbc *agentinterfaces.AgentCallbackContext,
	toolInputs *agentinterfaces.ToolCallInputs,
	decision InterruptDecision,
) {
	switch d := decision.(type) {
	case *ApproveResult:
		if d.NewArgs != "" {
			toolInputs.ToolArgs = d.NewArgs
		}
	case *RejectResult:
		r.skipTool(cbc, toolInputs, d)
	case *InterruptResult:
		r.raiseInterrupt(toolInputs.ToolName, toolInputs.ToolCall, d.Request)
	}
}

// raiseInterrupt 抛出 AbortError 中断执行。
//
// 对齐 Python: BaseInterruptRail._raise_interrupt(tool_name, tool_call, request)
func (r *BaseInterruptRail) raiseInterrupt(
	toolName string,
	toolCall *llmschema.ToolCall,
	request saschema.InterruptRequester,
) {
	exc := &saschema.ToolInterruptException{
		Request:  request,
		ToolCall: toolCall,
	}
	panic(cb.NewAbortError(
		fmt.Sprintf("Tool execution interrupted: %s", toolName),
		exc,
	))
}

// skipTool 跳过工具执行，设置预设返回结果。
//
// 对齐 Python: BaseInterruptRail._skip_tool(ctx, tool_call, tool_result, tool_message)
func (r *BaseInterruptRail) skipTool(
	cbc *agentinterfaces.AgentCallbackContext,
	toolInputs *agentinterfaces.ToolCallInputs,
	reject *RejectResult,
) {
	toolCallID := r.resolveToolCallID(toolInputs.ToolCall)
	cbc.Extra()["_skip_tool"] = true
	toolInputs.ToolResult = reject.ToolResult
	if reject.ToolMessage != nil {
		toolInputs.ToolMsg = reject.ToolMessage
	} else {
		toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, fmt.Sprintf("%v", reject.ToolResult))
	}
}

// resolveToolCallID 从 ToolCall 提取 ID。
//
// 对齐 Python: BaseInterruptRail._resolve_tool_call_id(tool_call)
func (r *BaseInterruptRail) resolveToolCallID(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.ID
}

// getUserInput 从 ctx.extra 提取用户输入。
//
// 对齐 Python: BaseInterruptRail._get_user_input(ctx, tool_call_id)
// 支持三种格式：
//   - *InteractiveInput: 按 toolCallID 从 UserInputs 查找
//   - map[string]any: 按 toolCallID 查找，找不到返回整个 map
//   - 其他类型: 直接返回
func (r *BaseInterruptRail) getUserInput(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) any {
	rawInput, exists := cbc.Extra()[saschema.ResumeUserInputKey]
	if !exists || rawInput == nil {
		return nil
	}

	// 对齐 Python logger.info：提取用户输入日志
	logger.Info(interruptLogComponent).
		Str("tool_call_id", toolCallID).
		Str("raw_input_type", fmt.Sprintf("%T", rawInput)).
		Msg("提取用户输入")

	// InteractiveInput 格式
	if interactive, ok := rawInput.(*sessioninteraction.InteractiveInput); ok {
		// 对齐 Python logger.info：记录 keys 列表
		keys := make([]string, 0, len(interactive.UserInputs))
		for k := range interactive.UserInputs {
			keys = append(keys, k)
		}
		logger.Info(interruptLogComponent).
			Str("tool_call_id", toolCallID).
			Strs("keys", keys).
			Msg("InteractiveInput.user_inputs")

		if val, found := interactive.UserInputs[toolCallID]; found {
			// 对齐 Python logger.info：匹配成功，记录 value 截断
			valRepr := fmt.Sprintf("%v", val)
			if len(valRepr) > 200 {
				valRepr = valRepr[:200]
			}
			logger.Info(interruptLogComponent).
				Str("tool_call_id", toolCallID).
				Str("value", valRepr).
				Msg("InteractiveInput 匹配成功")
			return val
		}
		logger.Warn(interruptLogComponent).
			Str("tool_call_id", toolCallID).
			Strs("keys", keys).
			Msg("InteractiveInput 中未找到匹配的 tool_call_id")
		return nil
	}

	// map[string]any 格式
	if m, ok := rawInput.(map[string]any); ok {
		if val, found := m[toolCallID]; found {
			return val
		}
		return m
	}

	// 其他类型直接返回
	return rawInput
}
