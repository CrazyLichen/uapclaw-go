package adapter

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// planStateStore 把 DeepAdapter 的会话状态机适配为 runtime.PlanStateStore。
type planStateStore struct {
	// d 所属适配器
	d *DeepAdapter
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PlanStateAccessor 返回 plan 状态读写入口（由 UapClawSwarm 调用控制器时传入）。
//
// Python: PlanModeController.open_state_session(agent, session_id) 的 Go 侧适配。
func (d *DeepAdapter) PlanStateAccessor() swruntime.PlanStateAccessor { return &planStateStore{d: d} }

// PlanController 返回计划模式控制器（未初始化时为 nil）。
func (d *DeepAdapter) PlanController() *swruntime.PlanModeController { return d.planController }

// ResetPlanSession 清理会话级的计划状态记忆。
//
// Python: PlanModeController.reset_session（删除会话时调用）
func (d *DeepAdapter) ResetPlanSession(sessionID string) {
	if d.planController != nil {
		d.planController.ResetSession(sessionID)
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
