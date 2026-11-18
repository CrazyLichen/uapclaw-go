// 本文件提供 scheduled-dispatch 团队实例的过期任务清扫（F_62）。
//
// 轮询节拍组合是两种模式下唯一不同之处：自持过期任务的清扫保留（它是成员
// 错过调度器 handoff 的投递安全网），但 leader 的 stale-PENDING 自提示只属于
// autonomous 认领池——scheduled 派发下长 PENDING 是正常态（预指派、排在 owner
// 的 one-active 上限之后），调度器会在 owner 一空出来就启动它。选择发生在
// EventDispatcher，从不发生在 handler 内部。
//
// 认领清扫同样保留旧版 task.updated_at 计时（autonomous handler 在 F_65 中
// 挪走的那个）：把 scheduled 派发重新锚到 idle clock 被刻意排除在该变更范围外。
// 注意 F_65 的 pause 缺陷在这里同样存在——跨长 pause 一直 idle 的 scheduled
// 成员仍会把 pause 度量为过期。迁移此清扫登记为 F_65 的 known-residual。
//
// Python: ScheduledStaleTaskHandler(StaleTaskHandler)（stale_task.py L342-…）
package handlers

import (
	"context"

	ateamcoordtypes "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ScheduledStaleTaskHandler scheduled-dispatch 团队实例的过期任务清扫。
// Python: ScheduledStaleTaskHandler(StaleTaskHandler)（stale_task.py L342）
type ScheduledStaleTaskHandler struct {
	StaleTaskHandler
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewScheduledStaleTaskHandler 创建 scheduled 模式过期任务 handler。
// Python: EventDispatcher._STALE_TASK_CLASS["scheduled"]（dispatcher.py L219）
func NewScheduledStaleTaskHandler(
	host ateamcoordtypes.DispatcherHost,
	bp ateamcoordtypes.DispatcherBlueprint,
	inf ateamcoordtypes.DispatcherInfra,
	pollCtrl ateamcoordtypes.PollController,
) *ScheduledStaleTaskHandler {
	return &ScheduledStaleTaskHandler{
		StaleTaskHandler: StaleTaskHandler{
			BaseCoordinationHandler: NewBaseCoordinationHandler(host, bp, inf, pollCtrl),
			staleClaimThrottle:      make(map[string]float64),
		},
	}
}

// OnPollTask 周期清扫：只标记自己名下的过期活跃任务。
// leader 的 stale-PENDING 自提示属于 autonomous 认领池——scheduled 派发下长
// PENDING 是正常态（预指派、排在 owner 的 one-active 上限之后）。
// Python: ScheduledStaleTaskHandler.on_poll_task（stale_task.py L361-369）
func (h *ScheduledStaleTaskHandler) OnPollTask(ctx context.Context, _ ateamcoordtypes.CoordinationEvent) {
	memberName := h.blueprint.DispatcherMemberName()
	logger.Debug(logComponent).
		Str("member_name", memberName).
		Bool("agent_running", h.round.IsAgentRunning()).
		Msg("poll task (scheduled)")
	if memberName != "" && h.infra != nil && h.infra.TaskManager() != nil {
		h.checkScheduledStaleActiveTasks(ctx)
	}
}

// checkScheduledStaleActiveTasks 找出自己名下活跃超过过期阈值的任务。
//
// 为 scheduled 派发钉住 pre-F_65 行为。「活跃」覆盖三个持有非终态条件——
// PLANNING、IN_PROGRESS、IN_REVIEW。用数据库 updated_at 列（每次状态转换都会
// bump）度量任务已活跃多久。超过 staleClaimThrottleSeconds 时成员给自己喂一条
// 提示。只清扫指派给本成员的任务——leader 不为其他成员的过期工作去催。
// per-task 节流防止后续轮询在同一过期窗口内重复提示。
// Python: ScheduledStaleTaskHandler._check_stale_claimed_tasks（stale_task.py L371-…）
func (h *ScheduledStaleTaskHandler) checkScheduledStaleActiveTasks(ctx context.Context) {
	taskMgr := h.infra.TaskManager()
	if taskMgr == nil {
		return
	}
	ownName := h.blueprint.DispatcherMemberName()
	var active []*database.TeamTaskBase
	for _, status := range []string{
		fsm.TaskStatusPlanning, fsm.TaskStatusInProgress, fsm.TaskStatusInReview,
	} {
		rows, err := taskMgr.ListTasks(ctx, status)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("status", status).
				Msg("checkScheduledStaleActiveTasks: list tasks failed")
			return
		}
		active = append(active, rows...)
	}
	var relevant []*database.TeamTaskBase
	for _, tk := range active {
		if taskAssignee(tk) == ownName {
			relevant = append(relevant, tk)
		}
	}
	// GC 节流条目
	currentIDs := make(map[string]bool, len(relevant))
	for _, tk := range relevant {
		currentIDs[tk.TaskID] = true
	}
	for tid := range h.staleClaimThrottle {
		if !currentIDs[tid] {
			delete(h.staleClaimThrottle, tid)
		}
	}
	if len(relevant) == 0 {
		return
	}
	// 节流以秒计（time.Now）；毫秒 now 只用于渲染相对时间字符串，两者分开
	// 保存以免单位混用。
	// Python: now = time.time(); now_ms = get_current_time(); threshold_ms = ...
	nowMS := float64(time.Now().UnixMilli())
	thresholdMS := staleClaimSec * 1000
	for _, task := range relevant {
		if task.UpdatedAt == 0 {
			continue
		}
		elapsedMS := nowMS - float64(task.UpdatedAt)
		if elapsedMS < thresholdMS {
			continue
		}
		lastNudge := h.staleClaimThrottle[task.TaskID]
		if nowMS/1000-lastNudge < staleClaimSec {
			continue
		}
		h.staleClaimThrottle[task.TaskID] = nowMS / 1000
		// Python: 措辞与投递复用基类 stale 提示（与模式无关）。
		h.nudgeStaleClaim(ctx, task)
	}
}
