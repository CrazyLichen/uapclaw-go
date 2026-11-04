package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// planStateStore 把 DeepAdapter 的会话状态机适配为 runtime.PlanStateStore。
type planStateStore struct {
	// d 所属适配器
	d *DeepAdapter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PlanController 返回计划模式控制器（未初始化时为 nil）。
func (d *DeepAdapter) PlanController() *swruntime.PlanModeController { return d.planController }

// DrainControlEvents 取出并清空会话的控制事件（供 server 层下发前端）。
func (d *DeepAdapter) DrainControlEvents(sessionID string) []swruntime.ControlEvent {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	events := d.controlEvents[sessionID]
	delete(d.controlEvents, sessionID)
	return events
}

// ResetPlanSession 清理会话级的计划状态记忆。
//
// Python: PlanModeController.reset_session（删除会话时调用）
func (d *DeepAdapter) ResetPlanSession(sessionID string) {
	if d.planController != nil {
		d.planController.ResetSession(sessionID)
	}
	d.controlMu.Lock()
	delete(d.controlEvents, sessionID)
	d.controlMu.Unlock()
}

// ensurePlanState 每轮执行前同步 plan 状态，返回可能被改写的 query。
//
// Python: PlanModeController.ensure_state + request.params["query"] 回写
func (d *DeepAdapter) ensurePlanState(ctx context.Context, req *schema.AgentRequest, sessionID, mode, query string, params map[string]any) string {
	if d.planController == nil || sessionID == "" {
		return query
	}
	res, err := d.planController.EnsureState(ctx, swruntime.PlanRequest{
		SessionID:         sessionID,
		Mode:              mode,
		SubMode:           paramsString(params, "sub_mode", ""),
		ReqMethod:         req.ReqMethod.String(),
		EntrySource:       paramsString(params, swruntime.PlanEntrySourceKey, ""),
		IsInterruptResume: params["resume"] == true || params["is_resume"] == true,
		// Python: params[_session_previous_mode]（会话已锁定的模式，用于 may_hold_state）
		PreviousSessionMode: paramsString(params, swruntime.PreviousSessionModeKey, ""),
		Query:               query,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "plan_state_sync_failed").
			Str("session_id", sessionID).
			Msg("plan state sync failed, continuing")
		return query
	}
	d.emitControlEvents(sessionID, res.Events)
	if res.Query != "" && res.Query != query {
		// 原 query 存键 + 改写回写，保证下游写历史时带约束、原值可还原
		params[swruntime.PlanReminderOriginalQueryKey] = query
		params["query"] = res.Query
		if updated, mErr := json.Marshal(params); mErr == nil {
			req.Params = updated
		}
		return res.Query
	}
	return query
}

// checkPlanPostProcess 执行后检测工具触发的退出并记录事件。
//
// Python: PlanModeController.check_post_process_exit
func (d *DeepAdapter) checkPlanPostProcess(ctx context.Context, req *schema.AgentRequest, sessionID, mode string, params map[string]any) {
	if d.planController == nil || sessionID == "" {
		return
	}
	events, err := d.planController.CheckPostProcessExit(ctx, swruntime.PlanRequest{
		SessionID: sessionID,
		Mode:      mode,
		SubMode:   paramsString(params, "sub_mode", ""),
		ReqMethod: req.ReqMethod.String(),
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Str("event_type", "plan_post_process_failed").
			Str("session_id", sessionID).
			Msg("plan post-process check failed, continuing")
		return
	}
	d.emitControlEvents(sessionID, events)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emitControlEvents 记录控制事件（server 层通过 DrainControlEvents 下发）。
func (d *DeepAdapter) emitControlEvents(sessionID string, events []swruntime.ControlEvent) {
	if len(events) == 0 {
		return
	}
	d.controlMu.Lock()
	if d.controlEvents == nil {
		d.controlEvents = map[string][]swruntime.ControlEvent{}
	}
	d.controlEvents[sessionID] = append(d.controlEvents[sessionID], events...)
	d.controlMu.Unlock()
	for _, ev := range events {
		logger.Info(logComponent).
			Str("event_type", ev.EventType).
			Str("session_id", sessionID).
			Str("mode", ev.Mode).
			Msg("plan control event")
	}
}

// openPlanSession 打开（或复用）会话，供状态读写使用。
//
// Python: PlanModeController.open_state_session
func (s *planStateStore) openPlanSession(ctx context.Context, sessionID string) (*session.Session, error) {
	if s.d.instance == nil {
		return nil, fmt.Errorf("DeepAdapter 未初始化")
	}
	card := s.d.instance.Card()
	if card == nil {
		return nil, fmt.Errorf("agent card 为空")
	}
	sess := session.CreateAgentSession(sessionID, card, nil)
	if err := sess.PreRun(ctx); err != nil {
		return nil, err
	}
	return sess, nil
}

// LoadPlanState 读取会话 plan 状态
func (s *planStateStore) LoadPlanState(ctx context.Context, sessionID string) (swruntime.PlanState, error) {
	sess, err := s.openPlanSession(ctx, sessionID)
	if err != nil {
		return swruntime.PlanState{}, err
	}
	state := s.d.instance.LoadState(sess)
	return swruntime.PlanState{Mode: state.PlanMode.Mode, PlanSlug: state.PlanMode.PlanSlug}, nil
}

// SavePlanState 保存会话 plan 状态
func (s *planStateStore) SavePlanState(ctx context.Context, sessionID string, ps swruntime.PlanState) error {
	sess, err := s.openPlanSession(ctx, sessionID)
	if err != nil {
		return err
	}
	state := s.d.instance.LoadState(sess)
	state.PlanMode.Mode, state.PlanMode.PlanSlug = ps.Mode, ps.PlanSlug
	s.d.instance.SaveState(sess, state)
	return sess.PostRun(ctx)
}

// SwitchPlanMode 切换 plan 模式。
//
// Python: DeepAgent.switch_mode —— 调用点位于 update_runtime_config 之后，
// 此时 agent 已 Configure，可安全执行完整 session 生命周期。
func (s *planStateStore) SwitchPlanMode(ctx context.Context, sessionID, mode string) error {
	return s.d.SwitchMode(ctx, sessionID, mode)
}

// CommitSession 提交会话（SwitchMode/SavePlanState 已完成会话收尾，此处无需额外动作）
func (s *planStateStore) CommitSession(_ context.Context, _ string) error { return nil }
