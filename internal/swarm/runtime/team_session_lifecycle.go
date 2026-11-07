package runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// ──────────────────────────── 接口 ────────────────────────────

// TeamLifecycleManager 生命周期适配器需要的团队运行时操作。
//
// Python: runtime/team_session_lifecycle.py _TeamLifecycleManager（9 方法中
// 本适配器实际用到的 6 个）
// 2 个已实现（PauseSessionRuntime / StopSessionRuntime 对应 Go 现有能力），
// 4 个为桩（SetRoundStateReporter / RemoveWaiter / BroadcastEvent /
// IsGenerationRunning），spec §10 R-7。
type TeamLifecycleManager interface {
	// SetRoundStateReporter 附加活跃/停泊团队轮的 Runtime 观察者（桩）
	SetRoundStateReporter(reporter func(sessionID string, generation int, active bool))
	// RemoveWaiter 从匹配代次移除一个响应等待者（桩）
	RemoveWaiter(sessionID, requestID string, generation *int)
	// PauseSessionRuntime 暂停匹配代次的运行时资源
	PauseSessionRuntime(ctx context.Context, sessionID, reason string, generation *int) (bool, error)
	// StopSessionRuntime 停止匹配代次的运行时资源
	StopSessionRuntime(ctx context.Context, sessionID, reason string, stopRunner bool, generation *int) (bool, error)
	// BroadcastEvent 向匹配代次广播事件（桩）
	BroadcastEvent(sessionID, event string, payload map[string]any)
	// IsGenerationRunning 匹配代次是否拥有活跃工作（桩）
	IsGenerationRunning(sessionID string, generation int) bool
}

// RoundStateCoordinator 轮状态协调器。
//
// Python: runtime/team_session_lifecycle.py _RoundStateCoordinator
type RoundStateCoordinator interface {
	// ResumeWaitingForInput 恢复等待输入的持久 owner
	ResumeWaitingForInput(sessionID string, generation int)
	// MarkSessionWaitingForInput 把空闲团队代次全部持久 owner 置为等待输入
	MarkSessionWaitingForInput(sessionID string, generation int)
}

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSessionLifecycleAdapter 团队实现 Runtime 会话生命周期端口。
//
// Python: runtime/team_session_lifecycle.py TeamSessionLifecycleAdapter
type TeamSessionLifecycleAdapter struct {
	// manager 团队运行时操作
	manager TeamLifecycleManager
	// rounds 轮状态协调器
	rounds RoundStateCoordinator
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSessionLifecycleAdapter 创建适配器。
//
// Python: TeamSessionLifecycleAdapter.__init__
func NewTeamSessionLifecycleAdapter(manager TeamLifecycleManager, rounds RoundStateCoordinator) *TeamSessionLifecycleAdapter {
	return &TeamSessionLifecycleAdapter{manager: manager, rounds: rounds}
}

// RemoveResponseWaiter 移除当前请求拥有的响应等待者。
//
// Python: TeamSessionLifecycleAdapter.remove_response_waiter
func (a *TeamSessionLifecycleAdapter) RemoveResponseWaiter(
	_ context.Context,
	exec session.ExecutionLifecycleContext,
	reason string,
) error {
	generation := exec.Session.Generation
	a.manager.RemoveWaiter(exec.Session.SessionID, exec.RequestID, &generation)
	return nil
}

// PauseSession 暂停一个团队会话代次。
//
// Python: TeamSessionLifecycleAdapter.pause_session
func (a *TeamSessionLifecycleAdapter) PauseSession(
	ctx context.Context,
	sess session.SessionLifecycleContext,
	reason, channelID string,
) (bool, error) {
	generation := sess.Generation
	return a.manager.PauseSessionRuntime(ctx, sess.SessionID, reason, &generation)
}

// StopSession 停止一个团队会话代次。
//
// Python: TeamSessionLifecycleAdapter.stop_session
func (a *TeamSessionLifecycleAdapter) StopSession(
	ctx context.Context,
	sess session.SessionLifecycleContext,
	reason string,
) error {
	generation := sess.Generation
	_, err := a.manager.StopSessionRuntime(ctx, sess.SessionID, reason, true, &generation)
	return err
}

// PublishPauseCompleted 通过存活的会话观察者发布暂停完成。
//
// Python: TeamSessionLifecycleAdapter.publish_pause_completed
func (a *TeamSessionLifecycleAdapter) PublishPauseCompleted(
	_ context.Context,
	sessionID string,
	generation *int,
	channelID string,
) error {
	payload := map[string]any{"session_id": sessionID, "channel_id": channelID}
	if generation != nil {
		payload["generation"] = *generation
	}
	a.manager.BroadcastEvent(sessionID, "team.pause_completed", payload)
	return nil
}

// IsRunning 一个团队代次是否拥有活跃工作。
//
// Python: TeamSessionLifecycleAdapter.is_running
func (a *TeamSessionLifecycleAdapter) IsRunning(sess session.SessionLifecycleContext) bool {
	return a.manager.IsGenerationRunning(sess.SessionID, sess.Generation)
}

// 编译期断言：适配器实现 SessionRuntimeLifecyclePort。
var _ session.SessionRuntimeLifecyclePort = (*TeamSessionLifecycleAdapter)(nil)
