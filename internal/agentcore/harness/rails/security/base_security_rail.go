package security

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SecurityDecision 安全决策接口，所有决策类型实现此接口。
//
// 对齐 Python: SecurityDecision (base_security_rail.py L47-49)
type SecurityDecision interface {
	isSecurityDecision()
}

// SecurityCheckContext 安全检查上下文。
// 传递给子类的 runSecurityCheck 方法。
//
// 对齐 Python: SecurityCheckContext (base_security_rail.py L36-44)
type SecurityCheckContext struct {
	// CallbackCtx 回调上下文
	CallbackCtx *agentinterfaces.AgentCallbackContext
	// Event 触发的事件类型
	Event agentinterfaces.AgentCallbackEvent
	// UserInput 用户输入（中断恢复时存在）
	UserInput any
	// AutoConfirmConfig 自动确认配置
	AutoConfirmConfig map[string]any
	// SubjectID 主体标识（用于中断恢复匹配）
	SubjectID string
}

// SecurityAllow 允许执行。
//
// 对齐 Python: SecurityAllow(SecurityDecision) (base_security_rail.py L51-55)
type SecurityAllow struct {
	// NewArgs 可选，替换后的参数（nil=不替换，对齐 Python new_args: dict | None）
	NewArgs *map[string]any
}

// SecurityReject 拒绝执行。
//
// 对齐 Python: SecurityReject(SecurityDecision) (base_security_rail.py L58-64)
type SecurityReject struct {
	// Message 拒绝消息
	Message string
	// Result 预设结果（forceFinish 场景使用）
	Result any
	// ToolMessage 预设的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// SecurityInterrupt 中断等待用户输入。
//
// 对齐 Python: SecurityInterrupt(SecurityDecision) (base_security_rail.py L67-72)
type SecurityInterrupt struct {
	// Request 中断请求
	Request saschema.InterruptRequester
	// SubjectID 主体标识
	SubjectID string
}

// SecurityAlert 允许执行但告警。
//
// 对齐 Python: SecurityAlert(SecurityDecision) (base_security_rail.py L84-102)
type SecurityAlert struct {
	// Message 告警消息
	Message string
	// Level 告警级别（默认 Warning）
	Level SecurityAlertLevel
	// AlertType 告警类型（默认 "security"）
	AlertType string
	// DisplayMode 显示模式（默认 "popup"）
	DisplayMode string
}

// BaseSecurityRail 安全 Rail 抽象基类。
//
// 提供统一的安全检查→决策应用流程：
//   - runAndApply: 从 cbc 构造 SecurityCheckContext → 调用子类 runSecurityCheck → applySecurityDecision
//   - applySecurityDecision: 根据决策类型执行 Allow/Reject/Interrupt/Alert 分支
//   - handleInterruptResume: 中断恢复通用逻辑（auto_confirm 检查 → 解析用户输入 → store）
//
// 子类只需实现 runSecurityCheck 返回具体决策即可。
//
// 对齐 Python: BaseSecurityRail(AgentRail) (base_security_rail.py L105-748)
type BaseSecurityRail struct {
	rails.DeepAgentRail
	// supportedEvents 支持的事件集合
	supportedEvents map[agentinterfaces.AgentCallbackEvent]bool
	// toolNames 关联的工具名集合
	toolNames map[string]struct{}
}

// TypeName 返回 Rail 类型名称，用于日志和 metadata 标识。
// 子类应 override 返回自己的类型名。
//
// 对齐 Python: self.__class__.__name__
func (r *BaseSecurityRail) TypeName() string {
	return "BaseSecurityRail"
}

// SecurityRailOption BaseSecurityRail 配置选项
type SecurityRailOption func(*BaseSecurityRail)

// ──────────────────────────── 枚举 ────────────────────────────

// SecurityAlertLevel 告警级别枚举。
//
// 对齐 Python: SecurityAlertLevel(str, Enum) (base_security_rail.py L75-83)
type SecurityAlertLevel int

const (
	// SecurityAlertLevelInfo 信息
	SecurityAlertLevelInfo SecurityAlertLevel = iota
	// SecurityAlertLevelWarning 警告
	SecurityAlertLevelWarning
	// SecurityAlertLevelError 错误
	SecurityAlertLevelError
	// SecurityAlertLevelCritical 严重
	SecurityAlertLevelCritical
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// baseSecurityRailPriority BaseSecurityRail 默认优先级
	// 对齐 Python: BaseSecurityRail.priority = 90
	baseSecurityRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 BaseSecurityRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*BaseSecurityRail)(nil)

var securityLogComponent = logger.ComponentAgentCore

// modelEvents 模型调用事件集合
// 对齐 Python: _MODEL_EVENTS (base_security_rail.py L30-33)
var modelEvents = map[agentinterfaces.AgentCallbackEvent]bool{
	agentinterfaces.CallbackBeforeModelCall: true,
	agentinterfaces.CallbackAfterModelCall:  true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseSecurityRail 创建安全 Rail 基类实例。
//
// 对齐 Python: BaseSecurityRail.__init__(tool_names)
func NewBaseSecurityRail(opts ...SecurityRailOption) *BaseSecurityRail {
	r := &BaseSecurityRail{
		DeepAgentRail:   *rails.NewDeepAgentRail(),
		supportedEvents: make(map[agentinterfaces.AgentCallbackEvent]bool),
		toolNames:       make(map[string]struct{}),
	}
	r.WithPriority(baseSecurityRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithSupportedEvents 设置支持的事件集合
func WithSupportedEvents(events ...agentinterfaces.AgentCallbackEvent) SecurityRailOption {
	return func(r *BaseSecurityRail) {
		for _, e := range events {
			r.supportedEvents[e] = true
		}
	}
}

// WithSecurityToolNames 设置关联的工具名
func WithSecurityToolNames(names ...string) SecurityRailOption {
	return func(r *BaseSecurityRail) {
		for _, n := range names {
			r.toolNames[n] = struct{}{}
		}
	}
}

// Allow 返回允许决策。
//
// 对齐 Python: BaseSecurityRail.allow(new_args=None)
func (r *BaseSecurityRail) Allow(newArgs *map[string]any) *SecurityAllow {
	return &SecurityAllow{NewArgs: newArgs}
}

// Approve 返回允许决策（Allow 别名，兼容从 InterruptRail 迁移的子类）。
//
// 对齐 Python: BaseSecurityRail.approve(new_args=None)
func (r *BaseSecurityRail) Approve(newArgs *map[string]any) *SecurityAllow {
	return r.Allow(newArgs)
}

// Reject 返回拒绝决策。
// toolResult: 兼容 Python reject(tool_result=...) 的参数，result 为 nil 时自动赋值。
// message 为空且 result 非 nil 时，自动从 result 推导 message。
//
// 对齐 Python: BaseSecurityRail.reject(message, result, tool_result, tool_message)
func (r *BaseSecurityRail) Reject(message string, result any, toolResult any, toolMessage *llmschema.ToolMessage) *SecurityReject {
	// 对齐 Python: if result is None and tool_result is not None: result = tool_result
	if result == nil && toolResult != nil {
		result = toolResult
	}
	// 对齐 Python: if not message and result is not None: message = str(result)
	if message == "" && result != nil {
		message = fmt.Sprintf("%v", result)
	}
	return &SecurityReject{
		Message:     message,
		Result:      result,
		ToolMessage: toolMessage,
	}
}

// Interrupt 返回中断决策。
//
// 对齐 Python: BaseSecurityRail.interrupt(request, subject_id)
func (r *BaseSecurityRail) Interrupt(request saschema.InterruptRequester, subjectID string) *SecurityInterrupt {
	return &SecurityInterrupt{
		Request:   request,
		SubjectID: subjectID,
	}
}

// Alert 返回告警决策。
//
// 对齐 Python: BaseSecurityRail.alert(message, level, alert_type, display_mode)
func (r *BaseSecurityRail) Alert(message string, level SecurityAlertLevel, alertType string, displayMode string) *SecurityAlert {
	return &SecurityAlert{
		Message:     message,
		Level:       level,
		AlertType:   alertType,
		DisplayMode: displayMode,
	}
}

// AddTool 添加关联的工具名。
//
// 对齐 Python: BaseSecurityRail.add_tool(tool_name)
func (r *BaseSecurityRail) AddTool(toolName string) {
	r.toolNames[toolName] = struct{}{}
}

// AddTools 批量添加关联的工具名。
//
// 对齐 Python: BaseSecurityRail.add_tools(tool_names)
func (r *BaseSecurityRail) AddTools(toolNames []string) {
	for _, n := range toolNames {
		r.toolNames[n] = struct{}{}
	}
}

// GetTools 返回所有关联的工具名集合。
//
// 对齐 Python: BaseSecurityRail.get_tools()
func (r *BaseSecurityRail) GetTools() map[string]struct{} {
	return r.toolNames
}

// GetCallbacks 覆盖基类回调映射，根据 supportedEvents 自动注册钩子。
//
// 对齐 Python: BaseSecurityRail.get_callbacks() (base_security_rail.py L189-198)
func (r *BaseSecurityRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	// 对齐 Python: EVENT_METHOD_MAP 映射
	eventMethodMap := map[agentinterfaces.AgentCallbackEvent]func(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error{
		agentinterfaces.CallbackBeforeInvoke:    r.BeforeInvoke,
		agentinterfaces.CallbackAfterInvoke:     r.AfterInvoke,
		agentinterfaces.CallbackBeforeToolCall:  r.BeforeToolCall,
		agentinterfaces.CallbackAfterToolCall:   r.AfterToolCall,
		agentinterfaces.CallbackBeforeModelCall: r.BeforeModelCall,
		agentinterfaces.CallbackAfterModelCall:  r.AfterModelCall,
	}

	for event, method := range eventMethodMap {
		if r.supportedEvents[event] {
			callbacks[event] = func(ctx context.Context, railCtx any) error {
				return method(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
			}
		}
	}

	return callbacks
}

// BeforeInvoke 调用前钩子（路由到 runAndApply）。
//
// 对齐 Python: BaseSecurityRail.before_invoke(ctx) (base_security_rail.py L200-201)
func (r *BaseSecurityRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackBeforeInvoke] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackBeforeInvoke)
	}
	return nil
}

// AfterInvoke 调用后钩子（路由到 runAndApply）。
//
// 对齐 Python: BaseSecurityRail.after_invoke(ctx) (base_security_rail.py L203-204)
func (r *BaseSecurityRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackAfterInvoke] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackAfterInvoke)
	}
	return nil
}

// BeforeToolCall 工具调用前钩子（路由到 runAndApply）。
//
// 对齐 Python: BaseSecurityRail.before_tool_call(ctx) (base_security_rail.py L206-207)
func (r *BaseSecurityRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackBeforeToolCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackBeforeToolCall)
	}
	return nil
}

// AfterToolCall 工具调用后钩子（路由到 runAndApply）。
//
// 对齐 Python: BaseSecurityRail.after_tool_call(ctx) (base_security_rail.py L209-210)
func (r *BaseSecurityRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackAfterToolCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackAfterToolCall)
	}
	return nil
}

// BeforeModelCall 模型调用前钩子（路由到 runAndApply）。
//
// 对齐 Python: BaseSecurityRail.before_model_call(ctx) (base_security_rail.py L212-213)
func (r *BaseSecurityRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackBeforeModelCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackBeforeModelCall)
	}
	return nil
}

// AfterModelCall 模型调用后钩子（路由到 runAndApply）。
//
// 对齐 Python: BaseSecurityRail.after_model_call(ctx) (base_security_rail.py L215-216)
func (r *BaseSecurityRail) AfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackAfterModelCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackAfterModelCall)
	}
	return nil
}

// String 返回告警级别的字符串表示
func (l SecurityAlertLevel) String() string {
	switch l {
	case SecurityAlertLevelInfo:
		return "info"
	case SecurityAlertLevelWarning:
		return "warning"
	case SecurityAlertLevelError:
		return "error"
	case SecurityAlertLevelCritical:
		return "critical"
	default:
		return fmt.Sprintf("unknown(%d)", l)
	}
}

// PopLastUserMessage 从当前 turn 移除最后一条用户消息并返回。
// 对齐 Python: BaseSecurityRail._pop_last_user_message(ctx) (base_security_rail.py L545-562)
func (r *BaseSecurityRail) PopLastUserMessage(cbc *agentinterfaces.AgentCallbackContext) []any {
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	// TODO: 依赖 session 消息操作接口，待接口实现后补齐
	return nil
}

// PopMatchingMessages 移除匹配正则的消息并返回。
// 对齐 Python: BaseSecurityRail._pop_matching_messages(ctx, patterns, with_history) (base_security_rail.py L564-585)
func (r *BaseSecurityRail) PopMatchingMessages(cbc *agentinterfaces.AgentCallbackContext, patterns []string, withHistory bool) []any {
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	// TODO: 依赖 session 消息操作接口，待接口实现后补齐
	return nil
}

// ExtractMessageContent 从消息对象提取文本内容。
// 对齐 Python: BaseSecurityRail._extract_message_content(msg) (base_security_rail.py L587-601)
func (r *BaseSecurityRail) ExtractMessageContent(msg any) string {
	if msg == nil {
		return ""
	}
	switch v := msg.(type) {
	case string:
		return v
	case map[string]any:
		if content, ok := v["content"]; ok {
			return fmt.Sprintf("%v", content)
		}
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ContainsAnyPattern 检查文本是否匹配任一正则。
// 对齐 Python: BaseSecurityRail._contains_any_pattern(text, patterns) (base_security_rail.py L603-613)
func (r *BaseSecurityRail) ContainsAnyPattern(text string, patterns []string) bool {
	for _, p := range patterns {
		if matched, err := regexp.MatchString(p, text); err == nil && matched {
			return true
		}
	}
	return false
}

// SanitizeMatchingMessages 脱敏替换匹配正则的消息内容。
// 对齐 Python: BaseSecurityRail._sanitize_matching_messages(ctx, patterns, replacement, with_history) (base_security_rail.py L615-689)
func (r *BaseSecurityRail) SanitizeMatchingMessages(cbc *agentinterfaces.AgentCallbackContext, patterns []string, replacement string, withHistory bool) []any {
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	// TODO: 依赖 session 消息操作接口，待接口实现后补齐
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSecurityDecision 实现 SecurityDecision 接口标记
func (a *SecurityAllow) isSecurityDecision()     {}
func (r *SecurityReject) isSecurityDecision()    {}
func (i *SecurityInterrupt) isSecurityDecision() {}
func (a *SecurityAlert) isSecurityDecision()     {}

// runAndApply 执行安全检查并应用决策。
//
// 对齐 Python: BaseSecurityRail._run_and_apply(ctx, event) (base_security_rail.py L225-252)
// 1. 从 cbc 构造 SecurityCheckContext
// 2. 调用子类 runSecurityCheck → SecurityDecision
// 3. Interrupt + MODEL事件 → 自动转为 Reject（Python 在此步骤处理，而非 applySecurityDecision 中）
// 4. 调用 applySecurityDecision
func (r *BaseSecurityRail) runAndApply(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	event agentinterfaces.AgentCallbackEvent,
) error {
	subjectID := r.resolveSubjectID(cbc, event)
	userInput := r.getUserInput(cbc, subjectID)

	securityCtx := &SecurityCheckContext{
		CallbackCtx:       cbc,
		Event:             event,
		UserInput:         userInput,
		AutoConfirmConfig: r.getAutoConfirmConfig(cbc),
		SubjectID:         subjectID,
	}

	decision, err := r.runSecurityCheck(ctx, securityCtx)
	if err != nil {
		logger.Error(securityLogComponent).
			Str("event", string(event)).
			Err(err).
			Msg("安全检查执行失败")
		return err
	}

	// 对齐 Python: Interrupt + MODEL事件 → 自动转为 Reject
	// (base_security_rail.py L241-249)
	if _, ok := decision.(*SecurityInterrupt); ok && modelEvents[event] {
		logger.Warn(securityLogComponent).
			Str("event", string(event)).
			Msg("SecurityInterrupt 不支持 MODEL 事件，自动转为 Reject")
		interrupt := decision.(*SecurityInterrupt)
		msg := interrupt.Request.GetMessage()
		if msg == "" {
			msg = "Security interrupt not allowed on model events"
		}
		decision = r.Reject(msg, nil, nil, nil)
	}

	// 对齐 Python: ctx.extra["_interrupt_decision"] = decision
	cbc.Extra()["_interrupt_decision"] = decision

	return r.applySecurityDecision(ctx, securityCtx, decision)
}

// runSecurityCheck 执行安全检查（抽象，子类实现）。
//
// 对齐 Python: BaseSecurityRail.run_security_check(security_ctx) (base_security_rail.py L218-223)
// 默认实现返回 Allow。
func (r *BaseSecurityRail) runSecurityCheck(_ context.Context, _ *SecurityCheckContext) (SecurityDecision, error) {
	return r.Allow(nil), nil
}

// applySecurityDecision 应用安全决策。
//
// 对齐 Python: BaseSecurityRail.apply_security_decision(security_ctx, decision) (base_security_rail.py L302-340)
// - Allow: return (continue)
// - Alert: log + stream, then continue
// - Reject: MODEL→forceFinish, BEFORE_TOOL_CALL→skipTool, AFTER_TOOL_CALL→forceFinish+toolResult
// - Interrupt: MODEL→已由 runAndApply 转为 Reject; TOOL→raiseInterrupt
func (r *BaseSecurityRail) applySecurityDecision(
	_ context.Context,
	securityCtx *SecurityCheckContext,
	decision SecurityDecision,
) error {
	switch d := decision.(type) {
	case *SecurityAllow:
		// 允许：不做任何操作
		if d.NewArgs != nil {
			if toolInputs, ok := securityCtx.CallbackCtx.Inputs().(*agentinterfaces.ToolCallInputs); ok {
				toolInputs.ToolArgs = *d.NewArgs
			}
		}
		return nil
	case *SecurityAlert:
		r.applyAlert(securityCtx, d)
		return nil
	case *SecurityReject:
		r.applyReject(securityCtx, d)
		return nil
	case *SecurityInterrupt:
		r.applyInterrupt(securityCtx, d)
		return nil
	default:
		return fmt.Errorf("未知的安全决策类型: %T", decision)
	}
}

// applyReject 拒绝分支处理。
//
// 对齐 Python: BaseSecurityRail._apply_reject(security_ctx, decision) (base_security_rail.py L342-390)
// MODEL事件 → forceFinish; BEFORE_TOOL_CALL → skipTool; AFTER_TOOL_CALL → forceFinish + toolResult
func (r *BaseSecurityRail) applyReject(securityCtx *SecurityCheckContext, decision *SecurityReject) {
	cbc := securityCtx.CallbackCtx
	event := securityCtx.Event

	// 对齐 Python: 确定 error_msg
	var errorMsg string
	if event == agentinterfaces.CallbackBeforeToolCall {
		errorMsg = decision.Message
		if errorMsg == "" {
			errorMsg = "Tool execution skipped"
		}
	} else {
		errorMsg = decision.Message
		if errorMsg == "" {
			errorMsg = "Blocked by security rail"
		}
	}

	// MODEL 事件: forceFinish
	if modelEvents[event] {
		result := r.buildForceFinishResult(decision)
		cbc.RequestForceFinish(result)
		return
	}

	// 获取 toolCallID
	var toolCallID string
	if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		if toolInputs.ToolCall != nil {
			toolCallID = toolInputs.ToolCall.ID
		}
	}

	// BEFORE_TOOL_CALL: skipTool
	if event == agentinterfaces.CallbackBeforeToolCall {
		var toolMsg *llmschema.ToolMessage
		if decision.ToolMessage != nil {
			toolMsg = decision.ToolMessage
		} else {
			toolMsg = llmschema.NewToolMessage(toolCallID, errorMsg)
		}
		r.skipTool(cbc, errorMsg, toolMsg)
		return
	}

	// AFTER_TOOL_CALL: forceFinish + 设置 toolResult/toolMsg
	if event == agentinterfaces.CallbackAfterToolCall {
		if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
			toolInputs.ToolResult = errorMsg
			toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, errorMsg)
		}
		result := r.buildForceFinishResult(decision)
		cbc.RequestForceFinish(result)
		return
	}
}

// applyInterrupt 中断分支处理。
//
// 对齐 Python: BaseSecurityRail._apply_interrupt(security_ctx, decision) (base_security_rail.py L392-417)
// MODEL 事件: 不应到达此处（已在 runAndApply 中转为 Reject）
// TOOL 事件: 仅在 user_input 为 nil 时 raiseInterrupt
func (r *BaseSecurityRail) applyInterrupt(securityCtx *SecurityCheckContext, decision *SecurityInterrupt) {
	cbc := securityCtx.CallbackCtx
	event := securityCtx.Event

	if modelEvents[event] {
		// 对齐 Python: MODEL 事件不应到达此处
		logger.Warn(securityLogComponent).
			Str("event", string(event)).
			Msg("SecurityInterrupt 在 MODEL 事件中不应到达此处")
		return
	}

	// 对齐 Python: 仅在 user_input 为 nil 时 raise
	if securityCtx.UserInput == nil {
		var toolName string
		var toolCall *llmschema.ToolCall
		if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
			toolName = toolInputs.ToolName
			toolCall = toolInputs.ToolCall
		}
		r.raiseToolInterrupt(toolName, toolCall, decision.Request)
	}
}

// applyAlert 告警分支处理。
//
// 对齐 Python: BaseSecurityRail._apply_alert(security_ctx, decision) (base_security_rail.py L419-466)
// 记录日志 + WriteStream OutputSchema with is_security_alert=true → 继续执行
func (r *BaseSecurityRail) applyAlert(securityCtx *SecurityCheckContext, decision *SecurityAlert) {
	// 对齐 Python: log_method = getattr(logger, decision.level.value, logger.warning)
	logMsg := fmt.Sprintf("[SecurityAlert] message=%s alert_type=%s level=%s display_mode=%s",
		decision.Message, decision.AlertType, decision.Level.String(), decision.DisplayMode)

	switch decision.Level {
	case SecurityAlertLevelInfo:
		logger.Info(securityLogComponent).Msg(logMsg)
	case SecurityAlertLevelWarning:
		logger.Warn(securityLogComponent).Msg(logMsg)
	case SecurityAlertLevelError:
		logger.Error(securityLogComponent).Msg(logMsg)
	case SecurityAlertLevelCritical:
		logger.Error(securityLogComponent).Msg(logMsg)
	default:
		logger.Warn(securityLogComponent).Msg(logMsg)
	}

	// 对齐 Python: stream OutputSchema to frontend
	cbc := securityCtx.CallbackCtx
	if sess := cbc.Session(); sess != nil {
		_ = sess.WriteStream(context.Background(), map[string]any{
			"type":  "message",
			"index": 0,
			"payload": map[string]any{
				"role":    "system",
				"content": fmt.Sprintf("[%s] %s", decision.Level.String(), decision.Message),
				"metadata": map[string]any{
					"is_security_alert": true,
					"level":             decision.Level.String(),
					"alert_type":        decision.AlertType,
					"display_mode":      decision.DisplayMode,
					"rail":              r.TypeName(),
				},
			},
		})
	}
}

// buildForceFinishResult 构建 forceFinish 结果。
//
// 对齐 Python: BaseSecurityRail._build_force_finish_result(decision) (base_security_rail.py L468-475)
func (r *BaseSecurityRail) buildForceFinishResult(decision *SecurityReject) map[string]any {
	// 对齐 Python: if isinstance(decision.result, dict): return decision.result
	if decision.Result != nil {
		if m, ok := decision.Result.(map[string]any); ok {
			return m
		}
	}
	// 对齐 Python: return {"output": message, "result_type": "error"}
	msg := decision.Message
	if msg == "" && decision.Result != nil {
		msg = fmt.Sprintf("%v", decision.Result)
	}
	if msg == "" {
		msg = "Rejected by security rail."
	}
	return map[string]any{
		"output":      msg,
		"result_type": "error",
	}
}

// raiseToolInterrupt 抛出工具中断异常。
//
// 对齐 Python: BaseSecurityRail._raise_tool_interrupt(tool_name, tool_call, request) (base_security_rail.py L477-486)
func (r *BaseSecurityRail) raiseToolInterrupt(
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

// skipTool 跳过工具执行。
//
// 对齐 Python: BaseSecurityRail._skip_tool(ctx, tool_call, tool_result, tool_message) (base_security_rail.py L488-502)
func (r *BaseSecurityRail) skipTool(
	cbc *agentinterfaces.AgentCallbackContext,
	toolResult any,
	toolMessage *llmschema.ToolMessage,
) {
	cbc.Extra()["_skip_tool"] = true
	if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		toolInputs.ToolResult = toolResult
		toolInputs.ToolMsg = toolMessage
	}
}

// handleInterruptResume 中断恢复通用逻辑。
//
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume(security_ctx, auto_confirm_key) (base_security_rail.py L691-735)
// 返回 nil 表示首次调用（走 Interrupt），非 nil 表示恢复决策。
func (r *BaseSecurityRail) handleInterruptResume(
	securityCtx *SecurityCheckContext,
	autoConfirmKey string,
) SecurityDecision {
	// 1. 检查 auto_confirm
	if r.isAutoConfirmed(securityCtx.AutoConfirmConfig, autoConfirmKey) {
		return r.Allow(nil)
	}

	// 2. 获取 userInput
	userInput := securityCtx.UserInput
	if userInput == nil {
		return nil // 首次调用，走 Interrupt
	}

	// 3. 解析 userInput
	// 对齐 Python: isinstance(user_input, dict) → .get("approved"), .get("auto_confirm")
	var approved, autoConfirm bool
	switch input := userInput.(type) {
	case map[string]any:
		approved, _ = input["approved"].(bool)
		autoConfirm, _ = input["auto_confirm"].(bool)
	default:
		// 对齐 Python: hasattr(user_input, "approved")
		// 尝试通过反射访问 approved 字段（ConfirmPayload 等）
		if hasApproved, ok := tryGetApproved(userInput); ok {
			approved = hasApproved
			autoConfirm, _ = tryGetAutoConfirm(userInput)
		}
	}

	// 4. approved → Allow; approved + auto_confirm → store
	if approved {
		if autoConfirm && securityCtx.CallbackCtx != nil {
			r.storeAutoConfirm(securityCtx.CallbackCtx, autoConfirmKey)
		}
		return r.Allow(nil)
	}

	// 5. rejected → Reject
	return r.Reject("", nil, nil, nil)
}

// isAutoConfirmed 检查 auto_confirm 配置。
//
// 对齐 Python: BaseSecurityRail._is_auto_confirmed(auto_confirm_config, auto_confirm_key) (base_security_rail.py L504-520)
func (r *BaseSecurityRail) isAutoConfirmed(config map[string]any, key string) bool {
	if config == nil || key == "" {
		return false
	}
	val, ok := config[key]
	if !ok {
		return false
	}
	// 对齐 Python: bool(auto_confirm_config.get(auto_confirm_key, False))
	return isSecurityTruthy(val)
}

// storeAutoConfirm 写入 auto_confirm 到 session 状态。
//
// 对齐 Python: BaseSecurityRail._store_auto_confirm(ctx, auto_confirm_key) (base_security_rail.py L522-543)
func (r *BaseSecurityRail) storeAutoConfirm(cbc *agentinterfaces.AgentCallbackContext, autoConfirmKey string) {
	sess := cbc.Session()
	if sess == nil || autoConfirmKey == "" {
		return
	}

	// 对齐 Python: config = ctx.session.get_state(INTERRUPT_AUTO_CONFIRM_KEY) or {}
	var config map[string]any
	if val, err := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && val != nil {
		if m, ok := val.(map[string]any); ok {
			config = m
		}
	}
	if config == nil {
		config = make(map[string]any)
	}

	config[autoConfirmKey] = true
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: config,
	})

	logger.Info(securityLogComponent).
		Str("auto_confirm_key", autoConfirmKey).
		Msg("auto_confirm.store")
}

// resolveSubjectID 解析 subject_id（用于中断恢复匹配）。
//
// 对齐 Python: BaseSecurityRail._resolve_subject_id(ctx, event) (base_security_rail.py L260-269)
func (r *BaseSecurityRail) resolveSubjectID(cbc *agentinterfaces.AgentCallbackContext, event agentinterfaces.AgentCallbackEvent) string {
	if event == agentinterfaces.CallbackBeforeToolCall || event == agentinterfaces.CallbackAfterToolCall {
		if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok && toolInputs.ToolCall != nil {
			return toolInputs.ToolCall.ID
		}
	}
	// 对齐 Python: f"{self.__class__.__name__}:{event.value}"
	return r.TypeName() + ":" + string(event)
}

// getUserInput 从 session 获取用户输入。
//
// 对齐 Python: BaseSecurityRail._get_user_input(ctx, subject_id) (base_security_rail.py L276-300)
func (r *BaseSecurityRail) getUserInput(cbc *agentinterfaces.AgentCallbackContext, subjectID string) any {
	rawInput, exists := cbc.Extra()[saschema.ResumeUserInputKey]
	if !exists || rawInput == nil {
		return nil
	}

	// 对齐 Python: logger.info
	logger.Info(securityLogComponent).
		Str("subject_id", subjectID).
		Str("raw_input_type", fmt.Sprintf("%T", rawInput)).
		Msg("get_user_input")

	// 对齐 Python: isinstance(raw_input, InteractiveInput)
	if interactive, ok := rawInput.(*sessioninteraction.InteractiveInput); ok {
		if val, found := interactive.UserInputs[subjectID]; found {
			return val
		}
		return nil
	}

	// 对齐 Python: isinstance(raw_input, dict)
	if m, ok := rawInput.(map[string]any); ok {
		if val, found := m[subjectID]; found {
			return val
		}
		return m
	}

	// 对齐 Python: return raw_input
	return rawInput
}

// getAutoConfirmConfig 从 session 获取 auto_confirm 配置。
//
// 对齐 Python: BaseSecurityRail._get_auto_confirm_config(ctx) (base_security_rail.py L254-258)
func (r *BaseSecurityRail) getAutoConfirmConfig(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	val, err := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey))
	if err != nil || val == nil {
		return nil
	}
	if m, ok := val.(map[string]any); ok {
		return m
	}
	return nil
}

// isSecurityTruthy 宽松真值判断（对齐 Python bool(val)）。
// bool → 直接判断；int/int64/float64 → 非0为true；
// string → "true"/"1"/"yes" 为 true（安全语义，非 Python 全非空字符串为 true）；
// 其余 → false
func isSecurityTruthy(val any) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
	default:
		return false
	}
}

// tryGetApproved 尝试从任意类型获取 approved 字段（通过反射或接口断言）
func tryGetApproved(val any) (bool, bool) {
	type approver interface {
		GetApproved() bool
	}
	if a, ok := val.(approver); ok {
		return a.GetApproved(), true
	}
	return false, false
}

// tryGetAutoConfirm 尝试从任意类型获取 auto_confirm 字段
func tryGetAutoConfirm(val any) (bool, bool) {
	type autoConfirmer interface {
		GetAutoConfirm() bool
	}
	if a, ok := val.(autoConfirmer); ok {
		return a.GetAutoConfirm(), true
	}
	return false, false
}
