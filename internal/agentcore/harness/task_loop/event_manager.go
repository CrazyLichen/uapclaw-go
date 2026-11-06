// 本文件对应 Python openjiuwen/harness/task_loop/event_manager.py，
// 为单个会话级交互监督器提供事件调度。
package task_loop

import (
	"errors"
	"fmt"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventManager 把 user 与 goal work 放进同一个调度域。
//
// 监督器是唯一的弹出消费者；两个独立队列实现 user 优先，而不暴露两套宿主流 API。
//
// ⚠️ 并发约定：本类型**不含内部锁**。所有方法要求调用方持有 DeepAgent 的
// interaction control lock（spec K3 / D4）。Python 靠 asyncio 单线程满足此约定，
// Go 由 S2 的 supervisor 在 NextWork/MarkStarted/PushUser/MarkFinished 四处加锁。
// Python: EventManager (event_manager.py L15)
type EventManager struct {
	// userQueue 用户轮队列（高优先级）
	userQueue []*hschema.RoundWorkItem
	// goalQueue 目标轮队列（低优先级）
	goalQueue []*hschema.RoundWorkItem
	// dequeued 已弹出但尚未开始的 work
	dequeued *hschema.RoundWorkItem
	// active 正在执行的 work
	active *hschema.RoundWorkItem
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrWorkKindMismatch work item 类型与目标队列不匹配
	// Python: ValueError("push_user requires a user RoundWorkItem") (L30)
	ErrWorkKindMismatch = errors.New("work item kind mismatch")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventManager 创建调度管理器。
// Python: EventManager() (event_manager.py L22)
func NewEventManager() *EventManager {
	return &EventManager{}
}

// PushUser 入队 user work；kind 不匹配时返回 ErrWorkKindMismatch。
// Python: EventManager.push_user (event_manager.py L28)
func (m *EventManager) PushUser(work *hschema.RoundWorkItem) error {
	if work.Kind != hschema.RoundWorkKindUser {
		return fmt.Errorf("%w: push_user requires a user RoundWorkItem", ErrWorkKindMismatch)
	}
	m.userQueue = append(m.userQueue, work)
	return nil
}

// PushGoal 入队 goal work；同 (goal_id, revision) 已存在时返回 false。
//
// 返回 false 表示 pending/dequeued/active 已有同一代际，调用方不得再建一次尝试。
// Python: EventManager.push_goal (event_manager.py L33)
func (m *EventManager) PushGoal(work *hschema.RoundWorkItem) (bool, error) {
	if work.Kind != hschema.RoundWorkKindGoal {
		return false, fmt.Errorf("%w: push_goal requires a goal RoundWorkItem", ErrWorkKindMismatch)
	}
	if m.HasGoalWork(work.Context.GoalID, work.Context.Revision) {
		return false, nil
	}
	m.goalQueue = append(m.goalQueue, work)
	logger.Debug(logger.ComponentAgentCore).
		Str("event_type", "INTERACTION_GOAL_QUEUED").
		Str("goal_id", work.Context.GoalID).
		Int("revision", work.Context.Revision).
		Msg("goal work queued")
	return true, nil
}

// NextWork 弹出下一个待执行 work：user 队列优先，其次 goal 队列。
// Python: EventManager.next_work (event_manager.py L50)
func (m *EventManager) NextWork() *hschema.RoundWorkItem {
	if len(m.userQueue) > 0 {
		m.dequeued = m.userQueue[0]
		m.userQueue = m.userQueue[1:]
		return m.dequeued
	}
	if len(m.goalQueue) > 0 {
		m.dequeued = m.goalQueue[0]
		m.goalQueue = m.goalQueue[1:]
		return m.dequeued
	}
	return nil
}

// MarkStarted 标记 work 开始执行：清 dequeued、置 active。
// Python: EventManager.mark_started (event_manager.py L59)
func (m *EventManager) MarkStarted(work *hschema.RoundWorkItem) {
	if m.dequeued == work {
		m.dequeued = nil
	}
	m.active = work
}

// MarkFinished 标记 work 结束：清 active。
// Python: EventManager.mark_finished (event_manager.py L64)
func (m *EventManager) MarkFinished(work *hschema.RoundWorkItem) {
	if m.active == work {
		m.active = nil
	}
}

// HasPendingWork 返回是否有待办（不含正在执行）。
// Python: EventManager.has_pending_work (event_manager.py L68)
func (m *EventManager) HasPendingWork() bool {
	return len(m.userQueue) > 0 || len(m.goalQueue) > 0
}

// HasPendingUserWork 返回是否有待办 user work。
// Python: EventManager.has_pending_user_work (event_manager.py L71)
func (m *EventManager) HasPendingUserWork() bool {
	return len(m.userQueue) > 0
}

// HasGoalWork 返回指定代际是否已在 pending/dequeued/active 中。
// Python: EventManager.has_goal_work (event_manager.py L74)
func (m *EventManager) HasGoalWork(goalID string, revision int) bool {
	matches := func(work *hschema.RoundWorkItem) bool {
		return work != nil &&
			work.Kind == hschema.RoundWorkKindGoal &&
			work.Context.GoalID == goalID &&
			work.Context.Revision == revision
	}
	for _, work := range m.goalQueue {
		if matches(work) {
			return true
		}
	}
	return matches(m.dequeued) || matches(m.active)
}

// HasRunningGoal 返回指定目标是否已有 dequeued/active 的尝试。
//
// 忽略 revision 与 pending 队列：pause/resume 用它识别"仍在收尾的尝试"，
// 即使记录 revision 已被提升，且被丢弃的排队延续不计入。
// Python: EventManager.has_running_goal (event_manager.py L89)
func (m *EventManager) HasRunningGoal(goalID string) bool {
	matches := func(work *hschema.RoundWorkItem) bool {
		return work != nil &&
			work.Kind == hschema.RoundWorkKindGoal &&
			work.Context.GoalID == goalID
	}
	return matches(m.dequeued) || matches(m.active)
}

// DiscardGoalWork 丢弃被清除/替换记录所属的待办 goal work。
//
// goalID 为空串表示丢弃该会话的全部目标待办。
// Python: EventManager.discard_goal_work (event_manager.py L106)
func (m *EventManager) DiscardGoalWork(sessionID string, goalID string) int {
	before := len(m.goalQueue)
	kept := make([]*hschema.RoundWorkItem, 0, before)
	for _, work := range m.goalQueue {
		sameSession := work.Context.SessionID == sessionID
		sameGoal := goalID == "" || work.Context.GoalID == goalID
		if sameSession && sameGoal {
			continue
		}
		kept = append(kept, work)
	}
	m.goalQueue = kept
	discarded := before - len(m.goalQueue)
	if discarded > 0 {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "INTERACTION_GOAL_DISCARDED").
			Int("count", discarded).
			Str("session_id", sessionID).
			Str("goal_id", goalID).
			Msg("discarded pending goal work items")
	}
	return discarded
}

// DiscardAllWork 在输出租约断开时丢弃两个队列的待办。
//
// 持久化的 GoalRecord 刻意保留：之后一次 GOAL 发送会在前端重新 attach 后
// 重建下一个 work item。
// Python: EventManager.discard_all_work (event_manager.py L132)
func (m *EventManager) DiscardAllWork() {
	m.userQueue = nil
	m.goalQueue = nil
}

// ActiveWork 返回正在执行的 work（无则 nil）。
// Python: EventManager.active_work (event_manager.py L141)
func (m *EventManager) ActiveWork() *hschema.RoundWorkItem { return m.active }
