// Package runtime —— 会话生命周期 Runtime 侧端口（25.6 批二归还）。
// Python: runtime/session_lifecycle.py 的两个 Protocol（25.3 修订 2 删除，
// 回收条件=执行取消流 team 分支落地，随本批归还）：
//   - RuntimeResourceLease（:213-217）
//   - SessionRuntimeLifecyclePort（:219-256）
//
// 其余符号（Descriptor/Target/participant/registry 等）按依赖方向留守
// internal/swarm/runtime/session/session_lifecycle.go（25.3 修订 2 既有拆分）。
package runtime

import (
	"context"

	teamharness "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeResourceLease 执行资源租约（释放即放弃对 Team 运行时资源的持有）。
// Python: runtime/session_lifecycle.py:213-217 RuntimeResourceLease
type RuntimeResourceLease interface {
	// Release 释放租约。
	// Python: async def release(self) -> None
	Release(ctx context.Context) error
}

// SessionRuntimeLifecyclePort AgentRuntime 编排的 Team 域生命周期动作。
// Python: runtime/session_lifecycle.py:219-256 SessionRuntimeLifecyclePort
type SessionRuntimeLifecyclePort interface {
	// RemoveResponseWaiter 移除当前请求持有的响应等待者。
	// Python: :225-231 remove_response_waiter(context, *, reason)
	RemoveResponseWaiter(ctx context.Context, context sessionpkg.ExecutionLifecycleContext, reason string) error
	// PauseSession 暂停（或排空）一个 Team 会话代次，返回是否暂停成功。
	// Python: :233-241 pause_session(context, *, reason, channel_id=None) -> bool
	PauseSession(ctx context.Context, context sessionpkg.SessionLifecycleContext, reason string, channelID string) (bool, error)
	// StopSession 停止一个 Team 会话代次。
	// Python: :243-249 stop_session(context, *, reason)
	StopSession(ctx context.Context, context sessionpkg.SessionLifecycleContext, reason string) error
	// PublishPauseCompleted 经存活的会话观察者发布暂停完成。
	// Python: :251-255 publish_pause_completed(session_id, *, generation, channel_id=None)
	PublishPauseCompleted(ctx context.Context, sessionID string, generation *int, channelID string) error
	// IsRunning 一个 Team 代次是否拥有活跃工作。
	// Python: :213-215 is_running(context) -> bool
	IsRunning(context sessionpkg.SessionLifecycleContext) bool
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// teamLifecycle 取（懒构造并缓存）指定渠道的团队生命周期端。
// Python: service.py:4022-4031 _team_lifecycle()（懒构造
// TeamSessionLifecycleAdapter(coordinator=self._session_coordinator) 并缓存到
// _session_runtime_lifecycle）；Go 因 GetTeamManager(channelID) 渠道粒度既定
// 差异（spec 附录 A）按渠道缓存，team_manager 懒取 get_team_manager() 等价。
func (r *AgentRuntime) teamLifecycle(channelID string) SessionRuntimeLifecyclePort {
	r.sessionRuntimeLifecycleMu.Lock()
	defer r.sessionRuntimeLifecycleMu.Unlock()
	if r.sessionRuntimeLifecycle == nil {
		r.sessionRuntimeLifecycle = map[string]SessionRuntimeLifecyclePort{}
	}
	if existing, ok := r.sessionRuntimeLifecycle[channelID]; ok {
		return existing
	}
	adapter := NewTeamSessionLifecycleAdapter(
		teamharness.GetTeamManager(channelID), r.coordinator)
	r.sessionRuntimeLifecycle[channelID] = adapter
	return adapter
}
