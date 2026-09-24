package handlers

import (
	"context"
	"time"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StaleTaskHandler 周期轮询过期任务：CLAIMED 过期提醒 + PENDING 过期提醒（leader only）。
//
// EVENT_METHOD_MAP:
//   - coordination_poll_task → onPollTask
//
// Python: StaleTaskHandler
type StaleTaskHandler struct {
	BaseCoordinationHandler
	// staleClaimThrottle 过期认领节流映射（task_id → 上次提醒时间戳），
	// 与 MemberHandler 共享引用。
	// Python: _last_stale_nudge
	staleClaimThrottle map[string]float64
	// lastPendingNudge Leader 专用：PENDING 过期提醒节流映射。
	// Python: _last_pending_nudge
	lastPendingNudge map[string]float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// staleClaimSeconds 过期 CLAIMED 阈值（秒），对齐 Python _STALE_CLAIM_SECONDS
	staleClaimSec = 600.0
	// stalePendingSeconds 过期 PENDING 阈值（秒），对齐 Python _STALE_PENDING_SECONDS
	stalePendingSec = 600.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStaleTaskHandler 创建 StaleTaskHandler 实例。
// staleClaimThrottle 与 MemberHandler 共享引用。
// Python: StaleTaskHandler.__init__
func NewStaleTaskHandler(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
	staleClaimThrottle map[string]float64,
) *StaleTaskHandler {
	if staleClaimThrottle == nil {
		staleClaimThrottle = make(map[string]float64)
	}
	return &StaleTaskHandler{
		BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
		staleClaimThrottle:      staleClaimThrottle,
		lastPendingNudge:        make(map[string]float64),
	}
}

// GetCallbacks 返回 event_key → 回调方法注册表。
// Python: StaleTaskHandler.get_callbacks
func (h *StaleTaskHandler) GetCallbacks() map[string]types.EventCallbackFunc {
	return map[string]types.EventCallbackFunc{
		string(types.InnerEventTypePollTask): h.OnPollTask,
	}
}

// StaleClaimThrottle 返回共享的过期认领节流映射引用。
// 供测试验证 Member 和 StaleTask handler 共享同一映射。
func (h *StaleTaskHandler) StaleClaimThrottle() map[string]float64 {
	return h.staleClaimThrottle
}

// OnPollTask 周期任务板扫描：检查过期 CLAIMED 和 PENDING 任务。
// Python: StaleTaskHandler.on_poll_task
func (h *StaleTaskHandler) OnPollTask(ctx context.Context, _ types.CoordinationEvent) {
	h.checkStaleClaimedTasks(ctx)
	h.checkStalePendingTasks(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// checkStaleClaimedTasks 检查过期 CLAIMED 任务。
// 每个成员检查自己认领的过期任务；leader 检查所有成员的。
// 对齐 Python: StaleTaskHandler._check_stale_claimed_tasks
// TODO(#9.63): 等任务后端接口就绪后补充完整扫描逻辑
func (h *StaleTaskHandler) checkStaleClaimedTasks(ctx context.Context) {
	role := h.blueprint.Role()
	memberName := h.blueprint.MemberName()
	now := float64(time.Now().Unix())

	logger.Debug(logComponent).
		Str("role", string(role)).
		Str("member_name", memberName).
		Float64("stale_threshold", staleClaimSec).
		Msg("checkStaleClaimedTasks: TODO 等任务后端就绪")

	_ = ctx
	_ = now
	// 对齐 Python 步骤：
	// 1. 查询所有 CLAIMED 任务
	// 2. 过滤：claimed_at + staleClaimSec < now
	// 3. 每个成员检查自己的；leader 检查所有
	// 4. per-task throttle：staleClaimThrottle[task_id] + staleClaimSec < now
	// 5. self assignee → selfNudgeStaleClaim
	// 6. other member (leader only) → leaderNudgeStaleClaim
}

// checkStalePendingTasks Leader 专用：检查过期 PENDING 任务（无人认领）。
// 对齐 Python: StaleTaskHandler._check_stale_pending_tasks
// TODO(#9.63): 等任务后端接口就绪后补充完整扫描逻辑
func (h *StaleTaskHandler) checkStalePendingTasks(ctx context.Context) {
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	logger.Debug(logComponent).
		Float64("stale_threshold", stalePendingSec).
		Msg("checkStalePendingTasks: leader TODO 等任务后端就绪")

	_ = ctx
	// 对齐 Python 步骤：
	// 1. 查询所有 PENDING 任务
	// 2. 过滤：created_at + stalePendingSec < now
	// 3. per-task throttle：lastPendingNudge[task_id] + stalePendingSec < now
	// 4. 列出过期 pending 任务 → deliver_input
}
