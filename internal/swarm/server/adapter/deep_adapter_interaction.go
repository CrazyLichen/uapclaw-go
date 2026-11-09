package adapter

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StartInteraction 绑定产品会话并启动本适配器的 DeepAgent 交互循环。
// Python: interface_deep.py:15044-15072 start_interaction
//
// 这是 GoalManager 的唯一构造点（deep_agent.py:3972，注释明确
// "start() is the only place that constructs GoalManager"），因此
// command.goal 的一切能力都以本方法先跑过为前提。
func (d *DeepAdapter) StartInteraction(ctx context.Context, sessionID string) error {
	if d.instance == nil {
		return fmt.Errorf("DeepAgent 实例未初始化，无法启动交互")
	}
	if sessionID == "" {
		return fmt.Errorf("start_interaction 需要显式 session_id")
	}
	sess := session.CreateAgentSession(sessionID, d.instance.Card(), nil)
	if err := sess.PreRun(ctx, map[string]any{}); err != nil {
		return fmt.Errorf("会话预运行失败: %w", err)
	}
	if err := d.instance.Start(ctx, sess); err != nil {
		return fmt.Errorf("启动交互循环失败: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// startInteractionIfNeeded 幂等启动（已绑定同会话则直接返回）。
// Python: interface_deep.py:15044（上游 start 内部对同会话重复调用返回 None）
func (d *DeepAdapter) startInteractionIfNeeded(ctx context.Context, sessionID string) error {
	if d.instance == nil {
		return nil
	}
	if d.instance.InteractionStarted() {
		bound := d.instance.InteractionSession()
		if bound == nil || bound.GetSessionID() == sessionID {
			return nil
		}
		return fmt.Errorf("交互循环已绑定会话 %s，无法改绑 %s",
			bound.GetSessionID(), sessionID)
	}
	return d.StartInteraction(ctx, sessionID)
}

// interactionStarted 交互循环是否已启动。
// Python: interface_deep.py:15418-15423 _instance_interaction_started
func (d *DeepAdapter) interactionStarted() bool {
	if d.instance == nil {
		return false
	}
	return d.instance.InteractionStarted()
}

// interactionSession 返回 start_interaction 绑定的长生命周期会话。
// Python: interface_deep.py:16230-16248 _active_loop_session
func (d *DeepAdapter) interactionSession() *session.Session {
	if d.instance == nil {
		return nil
	}
	return d.instance.InteractionSession()
}
