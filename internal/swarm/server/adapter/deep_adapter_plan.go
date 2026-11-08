package adapter

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
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

// PlanStateAccessor 返回 plan 状态读写入口（由 AgentRuntime 调用控制器时传入）。
// Python: PlanModeController.open_state_session(agent, session_id) 的 Go 侧适配。
func (d *DeepAdapter) PlanStateAccessor() swruntime.PlanStateAccessor { return &planStateStore{d: d} }

// openPlanSession 打开用于 plan 状态读写的 agent 与 Session。
// Python: jiuwenswarm/runtime/plan.py open_state_session（:85-140）
//
// 上游取法（:85-100）：
//
//	live_deep_agent = agent.get_live_session_instance(session_id)
//	if live_deep_agent is not None:
//	    live_session = resolve_live_agent_session(live_deep_agent, session_id or "default")
//	    if live_session is not None: return live_deep_agent, live_session, True
//	# 首轮：establish the same live adapter that chat execution will reuse
//	starter = agent.ensure_live_session_instance
//	...
//
// 即：优先用**正在跑该会话的 instance 及其绑定 Session**——状态（plan_mode）
// 缓存在绑定 Session 上，拿临时 Session 写入只会更新 checkpointer，下一轮
// 仍读旧快照（上游注释原话："the running interaction on a stale normal-mode
// snapshot"）。
//
// 10.3.28f：会话分身之后根不再持有 DeepAgent，必须走 GetLiveSessionInstance。
// 若该会话尚未启动（chat 之前就切 plan），退回 EnsureInstance 拿根实例，
// 再回退临时 Session——与上游"取不到 live 就 ensure"的顺序一致。
func (s *planStateStore) openPlanSession(ctx context.Context, sessionID string) (*harness.DeepAgent, *session.Session, error) {
	// 步骤 1: 该会话正在跑的 instance
	// Python: :85-100
	if live := s.d.GetLiveSessionInstance(sessionID); live != nil {
		if sess := resolveLiveAgentSession(live, sessionID); sess != nil {
			return live, sess, nil
		}
		// 有 instance 但没有绑定到该会话的 Session：用它的 card 开一个
		card := live.Card()
		if card == nil {
			return nil, nil, fmt.Errorf("agent card 为空")
		}
		sess := session.CreateAgentSession(sessionID, card, nil)
		if err := sess.PreRun(ctx); err != nil {
			return nil, nil, err
		}
		return live, sess, nil
	}

	// 步骤 2: 该会话尚未启动 → 建会话 child
	// Python: plan.py:119 `starter = agent.ensure_live_session_instance`
	//
	// 注意不是 EnsureInstance：后者建的是适配器**自己的** DeepAgent，
	// 而 plan 状态必须落到即将承接 chat.send 的那个 Session 上
	// （plan.py 与 interface_deep.py:12801-12804 的注释都点明了这点）。
	inst, err := s.d.EnsureLiveSessionInstance(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if inst == nil {
		return nil, nil, fmt.Errorf("DeepAdapter 未初始化")
	}
	if sess := resolveLiveAgentSession(inst, sessionID); sess != nil {
		return inst, sess, nil
	}
	card := inst.Card()
	if card == nil {
		return nil, nil, fmt.Errorf("agent card 为空")
	}
	sess := session.CreateAgentSession(sessionID, card, nil)
	if err := sess.PreRun(ctx); err != nil {
		return nil, nil, err
	}
	return inst, sess, nil
}

// LoadPlanState 读取会话 plan 状态
func (s *planStateStore) LoadPlanState(ctx context.Context, sessionID string) (swruntime.PlanState, error) {
	inst, sess, err := s.openPlanSession(ctx, sessionID)
	if err != nil {
		return swruntime.PlanState{}, err
	}
	state := inst.LoadState(sess)
	return swruntime.PlanState{Mode: state.PlanMode.Mode, PlanSlug: state.PlanMode.PlanSlug}, nil
}

// SavePlanState 保存会话 plan 状态
func (s *planStateStore) SavePlanState(ctx context.Context, sessionID string, ps swruntime.PlanState) error {
	inst, sess, err := s.openPlanSession(ctx, sessionID)
	if err != nil {
		return err
	}
	state := inst.LoadState(sess)
	state.PlanMode.Mode, state.PlanMode.PlanSlug = ps.Mode, ps.PlanSlug
	inst.SaveState(sess, state)
	return sess.PostRun(ctx)
}

// SwitchPlanMode 切换 plan 模式。
// Python: DeepAgent.switch_mode —— 调用点位于 update_runtime_config 之后，
// 此时 agent 已 Configure，可安全执行完整 session 生命周期。
func (s *planStateStore) SwitchPlanMode(ctx context.Context, sessionID, mode string) error {
	return s.d.SwitchMode(ctx, sessionID, mode)
}

// CommitSession 提交会话（SwitchMode/SavePlanState 已完成会话收尾，此处无需额外动作）
func (s *planStateStore) CommitSession(_ context.Context, _ string) error { return nil }
