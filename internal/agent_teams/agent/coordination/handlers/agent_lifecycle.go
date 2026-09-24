package handlers

import (
	"context"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentLifecycleHandler 处理 agent 生命周期事件：用户输入、待机、清理、工具审批、计划审批。
//
// EVENT_METHOD_MAP:
//   - user_input          → onUserInput
//   - team_standby        → onStandby
//   - team_cleaned        → onCleaned
//   - tool_approval_result → onToolApprovalResult
//   - task_plan_response  → onTaskPlanResponse
//
// Python: AgentLifecycleHandler
type AgentLifecycleHandler struct {
	BaseCoordinationHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 协调子系统的日志组件
var logComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentLifecycleHandler 创建 AgentLifecycleHandler 实例。
// Python: AgentLifecycleHandler.__init__
func NewAgentLifecycleHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *AgentLifecycleHandler {
	return &AgentLifecycleHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: AgentLifecycleHandler.get_callbacks
func (h *AgentLifecycleHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		string(types.InnerEventTypeUserInput): h.OnUserInput,
		events.TeamEventStandby:              h.OnStandby,
		events.TeamEventCleaned:             h.OnCleaned,
		events.TeamEventToolApprovalResult:  h.OnToolApprovalResult,
		events.TeamEventTaskPlanResponse:    h.OnTaskPlanResponse,
	}
}

// OnUserInput 转发协调引导的用户输入到 agent。
// Python: AgentLifecycleHandler.on_user_input
func (h *AgentLifecycleHandler) OnUserInput(ctx context.Context, event types.CoordinationEvent) {
	if !event.IsInner() {
		return
	}
	content := event.Inner.Payload["content"]
	if content == nil {
		logger.Debug(logComponent).Msg("on_user_input: payload 缺少 content，跳过")
		return
	}
	// 对齐 Python: self._round.deliver_input(content)
	if err := h.round.DeliverInput(ctx, content, false); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", string(event.Inner.EventType)).
			Msg("on_user_input: deliver_input 失败")
	}
}

// OnStandby 收到 TEAM_STANDBY 事件时暂停周期轮询。
// Python: AgentLifecycleHandler.on_standby
func (h *AgentLifecycleHandler) OnStandby(_ context.Context, _ types.CoordinationEvent) {
	h.poll.PausePolls()
	logger.Info(logComponent).Msg("on_standby: 已暂停周期轮询")
}

// OnCleaned 收到 TEAM_CLEANED 事件，非 leader 成员关闭自身。
// Leader 不关闭（需要存活等待下次交互）。
// Python: AgentLifecycleHandler.on_cleaned
func (h *AgentLifecycleHandler) OnCleaned(ctx context.Context, event types.CoordinationEvent) {
	if h.blueprint.Role() == schema.TeamRoleLeader {
		logger.Debug(logComponent).Msg("on_cleaned: leader 忽略 team_cleaned")
		return
	}
	if err := h.lifecycle.ShutdownSelf(ctx); err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("on_cleaned: shutdown_self 失败")
	}
}

// OnToolApprovalResult 收到工具审批结果后恢复 HITL 中断。
// 仅当事件的目标成员名匹配自身时才处理。
// Python: AgentLifecycleHandler.on_tool_approval_result
func (h *AgentLifecycleHandler) OnToolApprovalResult(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	// 对齐 Python: 仅处理目标为自己的事件
	memberName := h.blueprint.MemberName()
	em := event.Transport
	targetMember, _ := em.Payload["member_name"].(string)
	if targetMember != "" && targetMember != memberName {
		return
	}

	// 构建 InteractiveInput 输入，恢复中断
	// 对齐 Python: InteractiveInput(tool_call_id=..., approved=..., feedback=..., auto_confirm=...)
	input := buildInteractiveInput(em.Payload)
	if input == nil {
		logger.Debug(logComponent).Msg("on_tool_approval_result: 无法构建 InteractiveInput，跳过")
		return
	}

	if err := h.round.ResumeInterrupt(ctx, input); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", em.EventType).
			Msg("on_tool_approval_result: resume_interrupt 失败")
	}
}

// OnTaskPlanResponse 收到 Leader 对成员计划的审批决策后恢复 HITL 中断。
// 仅当事件目标为自己且含 tool_call_id 时处理。
// Python: AgentLifecycleHandler.on_task_plan_response
func (h *AgentLifecycleHandler) OnTaskPlanResponse(ctx context.Context, event types.CoordinationEvent) {
	if event.IsInner() {
		return
	}
	memberName := h.blueprint.MemberName()
	em := event.Transport
	targetMember, _ := em.Payload["member_name"].(string)
	if targetMember != "" && targetMember != memberName {
		return
	}

	// 对齐 Python: 只有包含 tool_call_id 时才恢复中断
	toolCallID, _ := em.Payload["tool_call_id"].(string)
	if toolCallID == "" {
		return
	}

	input := buildInteractiveInput(em.Payload)
	if input == nil {
		logger.Debug(logComponent).Msg("on_task_plan_response: 无法构建 InteractiveInput，跳过")
		return
	}

	if err := h.round.ResumeInterrupt(ctx, input); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("event_type", em.EventType).
			Msg("on_task_plan_response: resume_interrupt 失败")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildInteractiveInput 从事件 payload 构建 InteractiveInput 结构。
// 对齐 Python: InteractiveInput(tool_call_id=..., approved=..., feedback=..., auto_confirm=...)
// TODO(#9.63): 等 InteractiveInput 类型定义后补充完整构建逻辑
func buildInteractiveInput(payload map[string]any) map[string]any {
	toolCallID, _ := payload["tool_call_id"].(string)
	if toolCallID == "" {
		return nil
	}
	approved, _ := payload["approved"].(bool)
	feedback, _ := payload["feedback"].(string)
	autoConfirm, _ := payload["auto_confirm"].(bool)

	return map[string]any{
		"tool_call_id": toolCallID,
		"approved":     approved,
		"feedback":     feedback,
		"auto_confirm": autoConfirm,
	}
}
