package handlers

import (
	"context"
	"fmt"
	"time"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StaleTaskHandler 周期轮询过期任务：CLAIMED 过期提醒 + PENDING 过期提醒（leader only）。
//
// EVENT_METHOD_MAP 事件到方法映射：
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
	// staleClaimSec 过期 CLAIMED 阈值（秒），对齐 Python _STALE_CLAIM_SECONDS
	staleClaimSec = 600.0
	// stalePendingSec 过期 PENDING 阈值（秒），对齐 Python _STALE_PENDING_SECONDS
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
	// 对齐 Python: if member_name and self._infra.task_manager
	// h.infra 接口本身可能为 nil（测试注入 nil），先判空再访问
	memberName := h.blueprint.DispatcherMemberName()
	if memberName == "" || h.infra == nil || h.infra.TaskManager() == nil {
		return
	}
	h.checkStaleClaimedTasks(ctx)
	h.checkStalePendingTasks(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// taskAssignee 安全读取任务的认领人（nil → 空串）。
func taskAssignee(tk *database.TeamTaskBase) string {
	if tk.Assignee == nil {
		return ""
	}
	return *tk.Assignee
}

// checkStaleClaimedTasks 检查过期 CLAIMED 任务。
// 每个成员检查自己认领的过期任务；leader 检查所有成员的。
// 通过 updated_at（每次状态转换会刷新，对 CLAIMED 即认领时间戳）度量耗时，
// 超过阈值时通过本地 agent 循环（自己）或直接消息（leader→其他成员）提醒。
// per-task 节流防止同一过期窗口内重复提醒。
// S-32: 对齐 Python StaleTaskHandler._check_stale_claimed_tasks
func (h *StaleTaskHandler) checkStaleClaimedTasks(ctx context.Context) {
	taskMgr := h.infra.TaskManager()
	if taskMgr == nil {
		return
	}
	claimed, err := taskMgr.ListTasks(ctx, string(schema.TaskStatusClaimed))
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("checkStaleClaimedTasks: list claimed tasks failed")
		return
	}
	ownName := h.blueprint.DispatcherMemberName()
	isLeader := h.blueprint.Role() == schema.TeamRoleLeader

	// 对齐 Python: relevant = 认领人是自己 或 leader 看所有
	relevant := make([]*database.TeamTaskBase, 0, len(claimed))
	for _, tk := range claimed {
		assignee := taskAssignee(tk)
		if assignee == "" {
			continue
		}
		if assignee == ownName || isLeader {
			relevant = append(relevant, tk)
		}
	}

	// GC：清理已不相关任务的节流条目
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
		h.nudgeStaleClaim(ctx, task)
	}
}

// nudgeStaleClaim 分发过期认领提醒给自己或认领成员。
// S-32: 对齐 Python StaleTaskHandler._nudge_stale_claim
func (h *StaleTaskHandler) nudgeStaleClaim(ctx context.Context, task *database.TeamTaskBase) {
	assignee := taskAssignee(task)
	if assignee != "" && assignee == h.blueprint.DispatcherMemberName() {
		h.selfNudgeStaleClaim(ctx, task)
	} else if h.blueprint.Role() == schema.TeamRoleLeader && assignee != "" {
		h.leaderNudgeStaleClaim(ctx, task)
	}
}

// selfNudgeStaleClaim 向本地 agent 循环投递提醒输入。
// S-32: 对齐 Python StaleTaskHandler._self_nudge_stale_claim
func (h *StaleTaskHandler) selfNudgeStaleClaim(ctx context.Context, task *database.TeamTaskBase) {
	content := formatStaleClaimNudge(task)
	// 对齐 Python: deliver_input(content)（默认 use_steer=True）
	if err := h.round.DeliverInput(ctx, content, true); err != nil {
		logger.Error(logComponent).Err(err).
			Str("task_id", task.TaskID).
			Msg("selfNudgeStaleClaim: deliver_input failed")
	}
	logger.Info(logComponent).
		Str("member_name", h.blueprint.DispatcherMemberName()).
		Str("task_id", task.TaskID).
		Msg("self-nudged stale claimed task")
}

// leaderNudgeStaleClaim 向持有过期认领的成员发送直接提醒。
// S-32: 对齐 Python StaleTaskHandler._leader_nudge_stale_claim
func (h *StaleTaskHandler) leaderNudgeStaleClaim(ctx context.Context, task *database.TeamTaskBase) {
	msgMgr := h.infra.MessageManager()
	if msgMgr == nil {
		return
	}
	content := formatStaleClaimNudge(task)
	assignee := taskAssignee(task)
	// 对齐 Python: message_manager.send_message(content, task.assignee)
	if _, err := msgMgr.SendMessage(ctx, content, assignee, h.blueprint.DispatcherMemberName()); err != nil {
		logger.Error(logComponent).Err(err).
			Str("task_id", task.TaskID).
			Str("assignee", assignee).
			Msg("leaderNudgeStaleClaim: send_message failed")
	}
	logger.Info(logComponent).
		Str("assignee", assignee).
		Str("task_id", task.TaskID).
		Msg("leader nudged about stale claimed task")
}

// checkStalePendingTasks Leader 专用：检查过期 PENDING 任务（无人认领）。
// 超过阈值的 pending 任务列表投递给 leader 自身，由模型决定通知哪位成员。
// per-task 节流防止同一过期窗口内重复提醒。
// S-32: 对齐 Python StaleTaskHandler._check_stale_pending_tasks
func (h *StaleTaskHandler) checkStalePendingTasks(ctx context.Context) {
	if h.blueprint.Role() != schema.TeamRoleLeader {
		return
	}
	taskMgr := h.infra.TaskManager()
	if taskMgr == nil {
		return
	}
	pending, err := taskMgr.ListTasks(ctx, string(schema.TaskStatusPending))
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("checkStalePendingTasks: list pending tasks failed")
		return
	}

	nowMS := float64(time.Now().UnixMilli())
	thresholdMS := stalePendingSec * 1000
	staleIDs := make(map[string]bool, len(pending))
	for _, tk := range pending {
		if tk.UpdatedAt != 0 && (nowMS-float64(tk.UpdatedAt)) >= thresholdMS {
			staleIDs[tk.TaskID] = true
		}
	}

	// GC：清理已不 stale 任务的节流条目
	for tid := range h.lastPendingNudge {
		if !staleIDs[tid] {
			delete(h.lastPendingNudge, tid)
		}
	}

	fresh := make([]*database.TeamTaskBase, 0)
	for _, task := range pending {
		if !staleIDs[task.TaskID] {
			continue
		}
		last := h.lastPendingNudge[task.TaskID]
		if nowMS/1000-last < stalePendingSec {
			continue
		}
		fresh = append(fresh, task)
	}
	if len(fresh) == 0 {
		return
	}
	for _, task := range fresh {
		h.lastPendingNudge[task.TaskID] = nowMS / 1000
	}

	// 对齐 Python: lines = [t("dispatcher.stale_pending_header")] + 任务列表
	content, err := agentteams.T("dispatcher.stale_pending_header")
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("checkStalePendingTasks: i18n lookup failed")
		return
	}
	for _, task := range fresh {
		content += fmt.Sprintf("\n- [%s] %s: %s", task.TaskID, task.Title, task.Content)
	}
	if err := h.round.DeliverInput(ctx, content, true); err != nil {
		logger.Error(logComponent).Err(err).Msg("checkStalePendingTasks: deliver_input failed")
	}
	logger.Info(logComponent).
		Int("stale_count", len(fresh)).
		Msg("leader self-prompted about stale pending tasks")
}

// formatStaleClaimNudge 格式化过期认领提醒内容。
// 对齐 Python: t("dispatcher.stale_claim_self", task_id=..., title=..., content=...)
func formatStaleClaimNudge(task *database.TeamTaskBase) string {
	content, err := agentteams.T("dispatcher.stale_claim_self", map[string]any{
		"task_id": task.TaskID,
		"title":   task.Title,
		"content": task.Content,
	})
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("formatStaleClaimNudge: i18n lookup failed")
		return fmt.Sprintf("[Nudge] claimed task [%s] %s: %s", task.TaskID, task.Title, task.Content)
	}
	return content
}
