package runtime

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// ──────────────────────────── 接口 ────────────────────────────

// TeamLifecycleManager 生命周期适配器需要的团队运行时操作。
// Python: runtime/team_session_lifecycle.py _TeamLifecycleManager（Protocol，6 方法）
// Go 侧 2 个可用现有能力实现（PauseSessionRuntime / StopSessionRuntime），
// 其余 4 个为桩，spec §10 R-7。
type TeamLifecycleManager interface {
	// SetRoundStateReporter 附加活跃/停泊团队轮的 Runtime 观察者（桩）
	SetRoundStateReporter(reporter func(sessionID string, generation int, running bool))
	// RemoveWaiter 从匹配代次移除一个响应等待者（桩）
	RemoveWaiter(sessionID, requestID string, generation *int)
	// PauseSessionRuntime 暂停匹配代次的运行时资源
	PauseSessionRuntime(ctx context.Context, sessionID, reason string, generation *int) (bool, error)
	// StopSessionRuntime 停止匹配代次的运行时资源
	StopSessionRuntime(ctx context.Context, sessionID, reason string, stopRunner bool, generation *int) (bool, error)
	// BroadcastEvent 向匹配代次广播事件（桩）
	BroadcastEvent(sessionID, eventType string, payload map[string]any)
	// IsGenerationRunning 匹配代次是否拥有活跃工作（桩）
	IsGenerationRunning(sessionID string, generation int) bool
}

// RoundStateCoordinator 轮状态协调器。
// Python: runtime/team_session_lifecycle.py _RoundStateCoordinator（Protocol）
type RoundStateCoordinator interface {
	// ResumeWaitingForInput 恢复等待输入的持久 owner
	ResumeWaitingForInput(sessionID string, generation int)
	// MarkSessionWaitingForInput 把空闲团队代次全部持久 owner 置为等待输入
	MarkSessionWaitingForInput(sessionID string, generation int)
}

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSessionLifecycleAdapter 团队实现 Runtime 会话生命周期端口。
// Python: runtime/team_session_lifecycle.py TeamSessionLifecycleAdapter
// 构造时把 reportRoundState 注册为团队运行时的轮状态观察者，
// 使团队轮的 active/idle 变化回写到协调器的持久执行上。
type TeamSessionLifecycleAdapter struct {
	// manager 团队运行时操作
	manager TeamLifecycleManager
	// rounds 轮状态协调器
	rounds RoundStateCoordinator
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSessionLifecycleAdapter 创建适配器。
// Python: TeamSessionLifecycleAdapter.__init__（L94-105）
// 末尾必须注册轮状态观察者，否则团队轮的 active/idle 变化不会回写协调器。
func NewTeamSessionLifecycleAdapter(manager TeamLifecycleManager, rounds RoundStateCoordinator) *TeamSessionLifecycleAdapter {
	adapter := &TeamSessionLifecycleAdapter{manager: manager, rounds: rounds}
	if manager != nil {
		manager.SetRoundStateReporter(adapter.reportRoundState)
	}
	return adapter
}

// RemoveResponseWaiter 移除当前请求拥有的响应等待者。
// Python: TeamSessionLifecycleAdapter.remove_response_waiter（L122-134）
// reason 仅用于日志，上游 `del reason` 后未使用。
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
// Python: TeamSessionLifecycleAdapter.pause_session（L136-151）
// 返回匹配代次是否被暂停或排空；channelID 上游未使用。
func (a *TeamSessionLifecycleAdapter) PauseSession(
	ctx context.Context,
	sess session.SessionLifecycleContext,
	reason, channelID string,
) (bool, error) {
	generation := sess.Generation
	return a.manager.PauseSessionRuntime(ctx, sess.SessionID, reason, &generation)
}

// StopSession 停止一个团队会话代次。
// Python: TeamSessionLifecycleAdapter.stop_session（L153-163）
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
// Python: TeamSessionLifecycleAdapter.publish_pause_completed（L165-186）
// 两步且顺序不得调整：
//  1. generation 非空时先 mark_session_waiting_for_input（停泊存活观察者）
//  2. 广播 chat.processing_status 事件（is_processing=false / is_complete=true）
//
// ⚠️ event_type 与 payload 键是**协议字符串**，前端按它分支，不得改动。
func (a *TeamSessionLifecycleAdapter) PublishPauseCompleted(
	_ context.Context,
	sessionID string,
	generation *int,
	channelID string,
) error {
	if generation != nil {
		a.rounds.MarkSessionWaitingForInput(sessionID, *generation)
	}
	logger.Info(logComponent).
		Str("channel_id", channelID).
		Str("session_id", sessionID).
		Str("generation", intPtrText(generation)).
		Msg("[TeamProcessingStatus] generated: source=pause_completed is_processing=false is_complete=true")
	a.manager.BroadcastEvent(sessionID, "chat.processing_status", map[string]any{
		"event_type":    "chat.processing_status",
		"session_id":    sessionID,
		"is_processing": false,
		"is_complete":   true,
	})
	return nil
}

// IsRunning 一个团队代次是否拥有活跃工作。
// Python: TeamSessionLifecycleAdapter.is_running（L189-196）
func (a *TeamSessionLifecycleAdapter) IsRunning(sess session.SessionLifecycleContext) bool {
	return a.manager.IsGenerationRunning(sess.SessionID, sess.Generation)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reportRoundState 把团队轮活动回写到持久 Runtime 执行。
// Python: TeamSessionLifecycleAdapter._report_round_state（L107-120）
//
//	running 为真 → 恢复等待输入的持久 owner
//	running 为假 → 把该代次全部持久 owner 置为等待输入
func (a *TeamSessionLifecycleAdapter) reportRoundState(sessionID string, generation int, running bool) {
	if running {
		a.rounds.ResumeWaitingForInput(sessionID, generation)
		return
	}
	a.rounds.MarkSessionWaitingForInput(sessionID, generation)
}

// intPtrText 把可空整数渲染为日志文本（nil → "None"，对齐上游 %s）。
func intPtrText(v *int) string {
	if v == nil {
		return "None"
	}
	return itoaValue(*v)
}

// itoaValue 整数转十进制字符串。
func itoaValue(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// 编译期断言：适配器实现 SessionRuntimeLifecyclePort。
var _ session.SessionRuntimeLifecyclePort = (*TeamSessionLifecycleAdapter)(nil)
