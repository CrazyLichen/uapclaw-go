package interrupt

import (
	"context"
	"fmt"
	"reflect"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptDecision 中断决策接口，由 ApproveResult/RejectResult/InterruptResult 实现。
type InterruptDecision interface {
	isInterruptDecision()
}

// ApproveResult 允许继续执行决策。
// Python: ApproveResult(InterruptDecision)
type ApproveResult struct {
	// NewArgs 可选，替换工具参数（nil=不替换，对齐 Python new_args: dict | None）
	NewArgs *map[string]any
}

// RejectResult 拒绝执行决策。
type RejectResult struct {
	// ToolResult 预设的工具返回结果。
	// 实际值类型：string（如 "已拒绝"、用户反馈文本）。
	// 传入 skipTool 后写入 inputs.ToolResult。
	ToolResult any
	// ToolMessage 可选，预设的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// InterruptResult 中断决策，挂起执行等待用户输入。
type InterruptResult struct {
	// Request 中断请求接口，可存 InterruptRequest 或其子类（如 AskUserRequest）
	Request saschema.InterruptRequester
}

// BaseInterruptRail 中断拦截基础 Rail，提供工具调用的审批/拒绝/中断机制。
type BaseInterruptRail struct {
	// interruptKind 本 rail 抛出的中断类型（构造期注入；空串 = UNDECLARED）
	interruptKind string
	agentinterfaces.BaseRail
	// toolNames 需拦截的工具名集合
	toolNames map[string]struct{}
	// ResolveInterruptFn 中断解析函数，子类设置。默认：无输入→中断，有输入→允许。
	ResolveInterruptFn ResolveInterruptFn
	// interruptOwner 自定义 owner 键，空则取类型全名。
	// Python: InterruptOwnerMixin._interrupt_owner
	interruptOwner string
}

// ResolveInterruptFn 中断解析函数类型，子类设置。默认：无输入→中断，有输入→允许。
type ResolveInterruptFn func(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) InterruptDecision

// ──────────────────────────── 常量 ────────────────────────────

const (
	// baseInterruptRailPriority BaseInterruptRail 默认优先级
	// Python: BaseInterruptRail.priority = 90
	baseInterruptRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 BaseInterruptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*BaseInterruptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseInterruptRail 创建中断拦截基础 Rail 实例。
func NewBaseInterruptRail(toolNames ...string) *BaseInterruptRail {
	r := &BaseInterruptRail{
		BaseRail:  *agentinterfaces.NewBaseRail(),
		toolNames: make(map[string]struct{}, len(toolNames)),
	}
	for _, name := range toolNames {
		r.toolNames[name] = struct{}{}
	}
	r.WithPriority(baseInterruptRailPriority)
	// 设置默认 ResolveInterruptFn：无输入→中断，有输入→允许
	r.ResolveInterruptFn = r.defaultResolveInterrupt
	return r
}

// Approve 返回允许继续执行的决策。
func (r *BaseInterruptRail) Approve(newArgs *map[string]any) *ApproveResult {
	return &ApproveResult{NewArgs: newArgs}
}

// Reject 返回拒绝执行的决策。
func (r *BaseInterruptRail) Reject(toolResult any) *RejectResult {
	return &RejectResult{ToolResult: toolResult}
}

// Interrupt 创建中断决策。
// Python: BaseInterruptRail.interrupt(request)
func (r *BaseInterruptRail) Interrupt(request saschema.InterruptRequester) *InterruptResult {
	return &InterruptResult{Request: request}
}

// AddTool 添加需要拦截的工具名。
func (r *BaseInterruptRail) AddTool(toolName string) {
	r.toolNames[toolName] = struct{}{}
}

// AddTools 批量添加需要拦截的工具名。
func (r *BaseInterruptRail) AddTools(toolNames []string) {
	for _, name := range toolNames {
		r.toolNames[name] = struct{}{}
	}
}

// GetTools 返回所有已注册的拦截工具名列表。
func (r *BaseInterruptRail) GetTools() []string {
	names := make([]string, 0, len(r.toolNames))
	for name := range r.toolNames {
		names = append(names, name)
	}
	return names
}

// BeforeToolCall 工具调用前拦截，根据 ResolveInterruptFn 决策放行、拒绝或中断。
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
	userInput := r.GetUserInput(cbc, toolCallID)

	// 从 session 获取 auto_confirm 配置
	var autoConfirmConfig map[string]any
	if sess := cbc.Session(); sess != nil {
		if val, err := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && val != nil {
			if cfg, ok := val.(map[string]any); ok {
				autoConfirmConfig = cfg
			}
		}
	}

	// 调用 ResolveInterruptFn 获取决策
	decision := r.ResolveInterruptFn(ctx, cbc, toolInputs.ToolCall, userInput, autoConfirmConfig)

	// 执行决策
	r.applyDecision(cbc, toolInputs, decision)

	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeToolCall 回调。
func (r *BaseInterruptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// InterruptKind 返回本 rail 抛出中断时盖上的类型。
// 空串表示不声明（对齐 Python InterruptKind.UNDECLARED）。
// Python 的 interrupt_kind 是类属性，子类赋值即可覆盖；Go 方法在基类方法体内
// 静态绑定（embedding 虚方法陷阱，第 3 次实锤），故改为字段驱动 + SetInterruptKind
// 注入，子类不再方法覆盖。
// Python: InterruptOwnerMixin.interrupt_kind = InterruptKind.UNDECLARED
func (r *BaseInterruptRail) InterruptKind() string {
	if r.interruptKind != "" {
		return r.interruptKind
	}
	return saschema.InterruptKindUndeclared
}

// SetInterruptKind 覆盖本 rail 的中断类型（对齐 Python 子类
// interrupt_kind 类属性覆盖；传空串即恢复 UNDECLARED）。
func (r *BaseInterruptRail) SetInterruptKind(kind string) {
	r.interruptKind = kind
}

// InterruptOwner 返回本 rail 的稳定 owner 键，答案按此键路由回本 rail。
// 默认取类型全名，保证中断暂停与恢复之间 rail 被重建后仍能对上；
// 同一 agent 上两个同类 rail 必须给出不同 owner，否则会互相读到对方的答案。
// 子类可覆盖以实现自定义 owner。
// Python: InterruptOwnerMixin.interrupt_owner（默认 f"{cls.__module__}.{cls.__qualname__}"）
func (r *BaseInterruptRail) InterruptOwner() string {
	if r.interruptOwner != "" {
		return r.interruptOwner
	}
	return reflect.TypeOf(r).String()
}

// SetInterruptOwner 覆盖本 rail 的 owner 键。
// Python: InterruptOwnerMixin.interrupt_owner setter（非空字符串校验）
func (r *BaseInterruptRail) SetInterruptOwner(owner string) error {
	if owner == "" {
		return fmt.Errorf("interrupt_owner must be a non-empty string")
	}
	r.interruptOwner = owner
	return nil
}

// StampInterrupt 给 request 盖上本 rail 的 owner 与 kind（未声明 kind 时）。
// 中断 ID 由中断处理器在收集暂停调用时填入，此处不设置。
// Python: InterruptOwnerMixin.stamp_interrupt(request)
func (r *BaseInterruptRail) StampInterrupt(request saschema.InterruptRequester) {
	if request == nil {
		return
	}
	// 接口多态写入（Python: request.interrupt_owner/kind = ... 属性赋值）；
	// 旧式 *InterruptRequest 断言对嵌入基类的子类（AskUserRequest 等）失效。
	request.SetInterruptOwner(r.InterruptOwner())
	if request.GetInterruptKind() == "" {
		request.SetInterruptKind(r.InterruptKind())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isInterruptDecision 实现 InterruptDecision 接口标记。
func (a *ApproveResult) isInterruptDecision() {}

// isInterruptDecision 实现 InterruptDecision 接口标记。
func (r *RejectResult) isInterruptDecision() {}

// isInterruptDecision 实现 InterruptDecision 接口标记。
func (i *InterruptResult) isInterruptDecision() {}

// defaultResolveInterrupt 默认中断解析逻辑：无用户输入→中断，有输入→允许。
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
	return r.Approve(nil)
}

// applyDecision 根据中断决策类型执行对应的处理逻辑。
func (r *BaseInterruptRail) applyDecision(
	cbc *agentinterfaces.AgentCallbackContext,
	toolInputs *agentinterfaces.ToolCallInputs,
	decision InterruptDecision,
) {
	switch d := decision.(type) {
	case *ApproveResult:
		// Python: new_args is not None
		if d.NewArgs != nil {
			toolInputs.ToolArgs = *d.NewArgs
		}
	case *RejectResult:
		r.skipTool(cbc, toolInputs, d)
	case *InterruptResult:
		r.RaiseInterrupt(toolInputs.ToolName, toolInputs.ToolCall, d.Request)
	}
}

// raiseInterrupt 抛出工具中断异常，中止当前执行流程。
// 抛之前先给 request 盖上 owner 与 kind，供中断处理器按 owner 路由答案。
// Python: BaseInterruptRail._raise_interrupt（内部先 self.stamp_interrupt(request)）
func (r *BaseInterruptRail) RaiseInterrupt(
	toolName string,
	toolCall *llmschema.ToolCall,
	request saschema.InterruptRequester,
) {
	r.StampInterrupt(request)
	exc := &saschema.ToolInterruptException{
		Request:  request,
		ToolCall: toolCall,
	}
	panic(cb.NewAbortError(
		fmt.Sprintf("工具执行已中断: %s", toolName),
		exc,
	))
}

// skipTool 跳过工具执行，设置拒绝结果和错误消息。
func (r *BaseInterruptRail) skipTool(
	cbc *agentinterfaces.AgentCallbackContext,
	toolInputs *agentinterfaces.ToolCallInputs,
	reject *RejectResult,
) {
	toolCallID := r.resolveToolCallID(toolInputs.ToolCall)
	cbc.SetExtra("_skip_tool", true)
	toolInputs.ToolResult = reject.ToolResult
	if reject.ToolMessage != nil {
		toolInputs.ToolMsg = reject.ToolMessage
	} else {
		toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, fmt.Sprintf("%v", reject.ToolResult))
	}
}

// resolveToolCallID 从 ToolCall 中提取 ID，为空返回空字符串。
func (r *BaseInterruptRail) resolveToolCallID(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.ID
}

// FindOwnAnswer 返回本次恢复中本 rail 在指定调用上的答案（无则 nil）。
// 答案按 interrupt_owner 路由，rail 只读自己 owner 那份。
// Python: core/single_agent/interrupt/ownership.py:53-55
// InterruptOwnerMixin.find_own_answer（InterruptOwnerMixin 的 Go 对应）
func (r *BaseInterruptRail) FindOwnAnswer(
	cbc *agentinterfaces.AgentCallbackContext,
	toolCallID string,
) *saschema.InterruptAnswer {
	return interruptpkg.FindOwnAnswer(cbc, r.InterruptOwner(), toolCallID)
}

// IsCarriedAnswer 本 rail 在此调用上的答案是否来自更早恢复轮（已 acted on）。
// Python: ownership.py:57-64 is_carried_answer
func (r *BaseInterruptRail) IsCarriedAnswer(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) bool {
	return interruptpkg.IsCarriedAnswer(cbc, r.InterruptOwner(), toolCallID)
}

// getUserInput 从回调上下文中提取本 rail 的用户输入。
// Python: BaseInterruptRail._get_user_input（interrupt_base.py:279-283）：
// answer = self.find_own_answer(ctx.extra, tool_call_id)；返回 answer.value。
// 答案按 owner 路由（RESUME_INTERRUPT_ANSWERS_KEY），与 RESUME_USER_INPUT_KEY
// 的原始 InteractiveInput 无关。
func (r *BaseInterruptRail) GetUserInput(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) any {
	answer := r.FindOwnAnswer(cbc, toolCallID)
	if answer == nil {
		return nil
	}
	return answer.Value
}
