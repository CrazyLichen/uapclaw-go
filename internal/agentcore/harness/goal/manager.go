// 本文件对应 Python openjiuwen/harness/goal/manager.py，
// 提供会话级 Goal 能力与持久化 GoalRecord 的唯一写者。
package goal

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalStore GoalManager 所需的持久化面。
// Python: GoalStore Protocol (manager.py L27)
type GoalStore interface {
	// SessionID 返回绑定会话标识
	SessionID() string
	// Load 读取当前记录（无则 nil）
	Load() *GoalRecord
	// Save 持久化记录
	Save(record *GoalRecord)
	// Clear 移除记录
	Clear()
	// Commit 显式落盘
	Commit(ctx context.Context) error
}

// Coordinator 交互监督器所需的 Goal 能力面，由 *GoalManager 实现。
//
// Python: DeepAgent.goal_manager 是具体类 GoalManager；Go 用窄接口避免
// hschema → goal 的反向依赖（规则 10.2：这是 DeepAgent 依赖他人的窄接口）。
type Coordinator interface {
	// HasActiveRecord 当前会话是否存在 ACTIVE 记录
	HasActiveRecord() bool
	// EnsureActiveGoalWorkLocked 在 ACTIVE 目标缺少待办时补排（调用方持控制锁）
	EnsureActiveGoalWorkLocked() bool
	// BeginAttempt 开始一次尝试；返回 nil 表示该代际不应开始
	BeginAttempt(ctx context.Context, goalID string, revision int) (*GoalRecord, error)
}

// SetOptions set 的可选参数。
// Python: GoalManager.set 关键字参数 (manager.py L104)
type SetOptions struct {
	// OverwriteConfirmed 允许覆盖已存在的目标
	OverwriteConfirmed bool
	// TokenBudget token 预算上限（必须为正）
	TokenBudget *int
	// MaxAttempts 尝试次数上限（必须为正）
	MaxAttempts *int
}

// GoalManagerConfig GoalManager 构造参数。
// Python: GoalManager(*, store, event_manager, control_lock, has_output_stream,
//
//	cancel_active_round, emit_event, notify_work, language) (manager.py L54)
type GoalManagerConfig struct {
	// Store 持久化面
	Store GoalStore
	// EventManager 双队列调度器（无内部锁）
	EventManager *task_loop.EventManager
	// ControlLock 与 DeepAgent 共用的交互控制锁
	ControlLock *sync.Mutex
	// HasOutputStream 是否有输出消费者
	HasOutputStream func() bool
	// CancelActiveRound 取消在飞轮回调（调用方持控制锁，不得再取锁）
	CancelActiveRound func(ctx context.Context, kind hschema.RoundWorkKind, goalID, reason string)
	// EmitEvent 发出交互事件
	EmitEvent func(event hschema.InteractionEvent)
	// NotifyWork 唤醒监督器
	NotifyWork func()
	// Language 提示词语言
	Language string
}

// GoalManager 会话级 Goal 能力与唯一写者。
//
// ⚠️ 所有状态写操作共用 DeepAgent 的交互控制锁（Python 同）。
// 带 Locked 语义的方法要求调用方已持锁，方法内部绝不再取锁。
// Python: GoalManager (manager.py L47)
type GoalManager struct {
	// store 持久化面
	store GoalStore
	// eventManager 双队列调度器
	eventManager *task_loop.EventManager
	// controlMu 与监督器共用的控制锁
	controlMu *sync.Mutex
	// hasOutputStreamFn 输出消费者探测
	hasOutputStreamFn func() bool
	// cancelActiveRound 取消在飞轮
	cancelActiveRound func(ctx context.Context, kind hschema.RoundWorkKind, goalID, reason string)
	// emitEvent 事件出口
	emitEvent func(event hschema.InteractionEvent)
	// notifyWork 唤醒监督器
	notifyWork func()
	// language 提示词语言
	language string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrSessionMismatch 目标管理器绑定了其它会话。
// Python: ValueError("GoalManager is bound to a different session") (manager.py L86)
var ErrSessionMismatch = errors.New("goal manager is bound to a different session")

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGoalManager 构造目标管理器。
// Python: GoalManager.__init__ (manager.py L54)
func NewGoalManager(config GoalManagerConfig) *GoalManager {
	return &GoalManager{
		store:             config.Store,
		eventManager:      config.EventManager,
		controlMu:         config.ControlLock,
		hasOutputStreamFn: config.HasOutputStream,
		cancelActiveRound: config.CancelActiveRound,
		emitEvent:         config.EmitEvent,
		notifyWork:        config.NotifyWork,
		language:          prompts.ResolveLanguage(config.Language),
	}
}

// Language 返回提示词语言。
// Python: GoalManager.language (manager.py L75)
func (m *GoalManager) Language() string { return m.language }

// SetLanguage 设置提示词语言（经 resolve_language 归一）。
// Python: language.setter (manager.py L79)
func (m *GoalManager) SetLanguage(value string) { m.language = prompts.ResolveLanguage(value) }

// GetStore 暴露会话存储供只读工具与 rail 使用。
// Python: GoalManager.get_store (manager.py L83)
func (m *GoalManager) GetStore(sessionID string) (GoalStore, error) {
	if sessionID != "" && sessionID != m.store.SessionID() {
		return nil, ErrSessionMismatch
	}
	return m.store, nil
}

// Peek 返回深拷贝记录，不等待控制锁。
// Python: GoalManager.peek (manager.py L89)
func (m *GoalManager) Peek() *GoalRecord {
	return m.store.Load().Clone()
}

// Get 在控制锁内返回深拷贝记录。
// Python: GoalManager.get (manager.py L99)
func (m *GoalManager) Get(ctx context.Context) (*GoalRecord, error) {
	_ = ctx
	m.lock()
	defer m.unlock()
	return m.store.Load().Clone(), nil
}

// HasActiveRecord 返回当前是否存在 ACTIVE 记录（不加锁，供监督器持锁调用）。
// Python: deep_agent._load_goal_record_locked + status 判定 (deep_agent.py L4065)
func (m *GoalManager) HasActiveRecord() bool {
	record := m.store.Load()
	return record != nil && record.Status == GoalStatusActive
}

// Set 创建目标记录；首次创建或（确认后）覆盖既有目标。
//
// 注意：三处参数校验在取锁之前执行，与上游一致（非法参数不排队等锁）。
// Python: GoalManager.set (manager.py L104)
func (m *GoalManager) Set(ctx context.Context, objective string, opts SetOptions) (*GoalRecord, error) {
	normalized := strings.TrimSpace(objective)
	if normalized == "" {
		return nil, newGoalOperationError(
			"set", "invalid_objective", "goal objective must not be empty", nil,
		)
	}
	if opts.TokenBudget != nil && *opts.TokenBudget <= 0 {
		return nil, newGoalOperationError(
			"set", "invalid_objective", "token_budget must be positive", nil,
		)
	}
	if opts.MaxAttempts != nil && *opts.MaxAttempts <= 0 {
		return nil, newGoalOperationError(
			"set", "invalid_objective", "max_attempts must be positive", nil,
		)
	}

	m.lock()
	defer m.unlock()

	existing := m.store.Load()
	if existing != nil && !opts.OverwriteConfirmed {
		return nil, newGoalOperationError(
			"set", "already_exists", "a goal already exists for this session", existing,
		)
	}
	if existing != nil && m.eventManager != nil {
		m.eventManager.DiscardGoalWork(existing.SessionID, existing.GoalID)
	}

	record := CreateGoalRecord(m.store.SessionID(), normalized, opts.MaxAttempts, opts.TokenBudget)
	m.store.Save(record)
	if err := m.commitStoreLocked(ctx); err != nil {
		return nil, err
	}

	// 既有输出流仍是唯一消费者：先排替代轮再中止旧轮，流会自然续到新目标。
	if m.hasOutputStream() {
		m.ensureGoalWorkLocked(record)
		m.emitGoalUpdatedLocked(record)
	}

	if existing != nil {
		m.cancelRound(ctx, hschema.RoundWorkKindGoal, existing.GoalID, "goal_overwrite")
	}

	return record.Clone(), nil
}

// Pause 暂停目标；不提升 revision，让在飞轮自然收尾并提交评估。
// Python: GoalManager.pause (manager.py L173)
func (m *GoalManager) Pause(ctx context.Context) (*GoalRecord, error) {
	m.lock()
	defer m.unlock()

	record := m.store.Load()
	if record == nil {
		return nil, nil
	}
	if record.Status == GoalStatusActive {
		// 不提升 revision：在飞轮（含评估落盘）必须仍然有效。
		if m.eventManager != nil {
			m.eventManager.DiscardGoalWork(record.SessionID, record.GoalID)
		}
		// 先结算，保证只显示 time_used 的宿主在暂停时不会跳到 0。
		record.SettleActiveTime(false)
		// 在飞轮仍在跑：重开时钟以便后端继续计时。
		if m.hasInFlightGoalAttempt(record) {
			record.StartTiming()
		}
		record.Status = GoalStatusPaused
		record.Touch(false)
		m.store.Save(record)
		if err := m.commitStoreLocked(ctx); err != nil {
			return nil, err
		}
		if m.hasOutputStream() {
			m.emitGoalUpdatedLocked(record)
		}
	}
	return record.Clone(), nil
}

// Resume 恢复 PAUSED/BLOCKED 目标；空闲时提升 revision 并补排一轮。
// Python: GoalManager.resume (manager.py L203)
func (m *GoalManager) Resume(ctx context.Context) (*GoalRecord, error) {
	m.lock()
	defer m.unlock()

	record := m.store.Load()
	if record == nil {
		return nil, nil
	}
	if record.Status == GoalStatusPaused || record.Status == GoalStatusBlocked {
		inFlight := m.hasInFlightGoalAttempt(record)
		// 暂停期若尝试仍在跑，先把这段时间折入 time_used。
		record.SettleActiveTime(false)
		record.Status = GoalStatusActive
		record.StartTiming()
		// 空闲/阻塞恢复提升代际；同一尝试仍在飞则不提升、也不重复排队。
		record.Touch(!inFlight)
		m.store.Save(record)
		if err := m.commitStoreLocked(ctx); err != nil {
			return nil, err
		}
		if m.hasOutputStream() {
			if !inFlight {
				m.ensureGoalWorkLocked(record)
			}
			m.emitGoalUpdatedLocked(record)
		}
	}
	return record.Clone(), nil
}

// Clear 清除目标记录、待办与在飞轮。
// Python: GoalManager.clear (manager.py L230)
func (m *GoalManager) Clear(ctx context.Context) (*GoalRecord, error) {
	m.lock()
	defer m.unlock()

	record := m.store.Load()
	if record == nil {
		return nil, nil
	}
	record.SettleActiveTime(false)
	m.store.Clear()
	if err := m.commitStoreLocked(ctx); err != nil {
		return nil, err
	}
	if m.eventManager != nil {
		m.eventManager.DiscardGoalWork(record.SessionID, record.GoalID)
	}
	m.cancelRound(ctx, hschema.RoundWorkKindGoal, record.GoalID, "goal_clear")
	if m.hasOutputStream() && m.emitEvent != nil {
		m.emitEvent(hschema.NewGoalUpdatedEvent(nil))
	}
	return record.Clone(), nil
}

// EnsureActiveGoalWorkLocked 为当前 ACTIVE 记录补齐一个待办/在飞 work。
//
// ⚠️ 调用方必须已持交互控制锁，且已持有输出流。
// Python: GoalManager.ensure_active_goal_work_locked (manager.py L251)
func (m *GoalManager) EnsureActiveGoalWorkLocked() bool {
	record := m.store.Load()
	if record == nil || record.Status != GoalStatusActive {
		return false
	}
	return m.ensureGoalWorkLocked(record)
}

// BeginAttempt 校验代际并记账一次尝试开始。
//
// 返回 nil 表示该 work 已过期（目标被覆盖/清除/换代），调用方应丢弃它。
// Python: GoalManager.begin_attempt (manager.py L262)
func (m *GoalManager) BeginAttempt(
	ctx context.Context,
	goalID string,
	revision int,
) (*GoalRecord, error) {
	m.lock()
	defer m.unlock()

	record := m.store.Load()
	if !matchesActive(record, goalID, revision) {
		return nil, nil
	}
	record.AttemptCount++
	record.StartTiming()
	record.Touch(false)
	m.store.Save(record)
	if err := m.commitStoreLocked(ctx); err != nil {
		return nil, err
	}
	return record.Clone(), nil
}

// AccumulateUsage 累加一次 token 用量，并结算已流逝的工时段。
//
// 注意：上游此处只 save 不 commit（manager.py L289-301），Go 照抄。
// Python: GoalManager.accumulate_usage (manager.py L280)
func (m *GoalManager) AccumulateUsage(
	goalID string,
	revision int,
	inputTokens int,
	outputTokens int,
	cachedInputTokens int,
) error {
	m.lock()
	defer m.unlock()

	record := m.store.Load()
	if !matchesInFlight(record, goalID, revision) {
		return nil
	}
	// PAUSED 的在飞轮仍持有 active_started_at，同样要结算。
	if record.ActiveStartedAt != nil &&
		(record.Status == GoalStatusActive || record.Status == GoalStatusPaused) {
		record.SettleActiveTime(true)
	}
	record.TokenUsage.Accumulate(inputTokens, outputTokens, cachedInputTokens)
	record.Touch(false)
	m.store.Save(record)
	return nil
}

// ApplyAssessment 在当前代际仍有效时提交一次评估。
//
// 接受 ACTIVE 与 PAUSED：暂停只停"续跑"，允许在飞轮把评估写完。
// Python: GoalManager.apply_assessment (manager.py L303)
func (m *GoalManager) ApplyAssessment(
	ctx context.Context,
	goalID string,
	revision int,
	assessment GoalAssessment,
) (*GoalRecord, error) {
	m.lock()
	defer m.unlock()

	record := m.store.Load()
	if !matchesInFlight(record, goalID, revision) {
		return nil, nil
	}
	record.LastAssessment = &assessment
	switch assessment.Status {
	case GoalAssessmentStatusComplete:
		record.SettleActiveTime(false)
		record.Status = GoalStatusCompleted
		record.LastStopReason = strPtr("completed")
	case GoalAssessmentStatusBlocked:
		record.SettleActiveTime(false)
		record.Status = GoalStatusBlocked
		record.LastStopReason = strPtr("blocked")
	default:
		if record.Status == GoalStatusActive {
			record.SettleActiveTime(true)
		} else if record.Status == GoalStatusPaused {
			// PAUSED 下收尾：关时钟，恢复时另开新段，空闲暂停不计费。
			record.SettleActiveTime(false)
		}
	}
	record.Touch(false)
	m.store.Save(record)
	if err := m.commitStoreLocked(ctx); err != nil {
		return nil, err
	}

	if record.Status == GoalStatusActive && m.hasOutputStream() {
		m.ensureGoalWorkLocked(record)
	}
	if m.hasOutputStream() {
		m.emitGoalUpdatedLocked(record)
	}
	return record.Clone(), nil
}

// TaskView 转换 <goal_task> 渲染所需的只读视图。
//
// Python 侧 build_goal_task_query 直接吃 GoalRecord；Go 用视图类型打断
// sections ↔ goal 的循环依赖（spec D4），渲染逻辑不变。
func (r *GoalRecord) TaskView() sections.GoalTaskView {
	if r == nil {
		return sections.GoalTaskView{}
	}
	view := sections.GoalTaskView{
		Objective:    r.Objective,
		AttemptCount: r.AttemptCount,
		MaxAttempts:  r.MaxAttempts,
		TokenBudget:  r.TokenBudget,
		TotalTokens:  r.TokenUsage.TotalTokens,
	}
	if r.LastAssessment != nil {
		view.Previous = &sections.GoalTaskAssessment{
			Status:          string(r.LastAssessment.Status),
			Evidence:        r.LastAssessment.Evidence,
			RemainingWork:   derefStr(r.LastAssessment.RemainingWork),
			NextInstruction: derefStr(r.LastAssessment.NextInstruction),
		}
	}
	return view
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lock 取控制锁（未注入锁时为 no-op）。
func (m *GoalManager) lock() {
	if m.controlMu != nil {
		m.controlMu.Lock()
	}
}

// unlock 放控制锁。
func (m *GoalManager) unlock() {
	if m.controlMu != nil {
		m.controlMu.Unlock()
	}
}

// hasOutputStream 返回是否有输出消费者。
func (m *GoalManager) hasOutputStream() bool {
	if m.hasOutputStreamFn == nil {
		return false
	}
	return m.hasOutputStreamFn()
}

// commitStoreLocked 显式落盘（调用方持锁）。
// Python: GoalManager._commit_store_locked (manager.py L364)
func (m *GoalManager) commitStoreLocked(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	return m.store.Commit(ctx)
}

// emitGoalUpdatedLocked 发出 goal.updated 事件（调用方持锁）。
// Python: GoalManager._emit_goal_updated_locked (manager.py L361)
func (m *GoalManager) emitGoalUpdatedLocked(record *GoalRecord) {
	if m.emitEvent == nil || record == nil {
		return
	}
	m.emitEvent(hschema.NewGoalUpdatedEvent(record.ToDict()))
}

// cancelRound 取消在飞轮（调用方持锁）。
// Python: GoalManager._cancel_active_round 回调
func (m *GoalManager) cancelRound(
	ctx context.Context,
	kind hschema.RoundWorkKind,
	goalID string,
	reason string,
) {
	if m.cancelActiveRound == nil {
		return
	}
	m.cancelActiveRound(ctx, kind, goalID, reason)
}

// ensureGoalWorkLocked 为 ACTIVE 记录排一个 goal work（调用方持锁）。
// Python: GoalManager._ensure_goal_work_locked (manager.py L345)
func (m *GoalManager) ensureGoalWorkLocked(record *GoalRecord) bool {
	if record == nil || record.Status != GoalStatusActive || !m.hasOutputStream() {
		return false
	}
	if m.eventManager == nil {
		return false
	}
	query := sections.BuildGoalTaskQuery(record.TaskView(), m.language)
	work := hschema.NewGoalWorkItem(
		map[string]any{"query": query},
		record.GoalID,
		record.Revision,
		record.SessionID,
	)
	queued, err := m.eventManager.PushGoal(work)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Err(err).
			Str("event_type", "GOAL_PUSH_WORK_ERROR").
			Str("goal_id", record.GoalID).
			Msg("push goal work failed")
		return false
	}
	if queued && m.notifyWork != nil {
		m.notifyWork()
	}
	return queued
}

// matchesActive 判定记录是否为指定代际的 ACTIVE。
// Python: GoalManager._matches_active (manager.py L369)
func matchesActive(record *GoalRecord, goalID string, revision int) bool {
	return record != nil &&
		record.Status == GoalStatusActive &&
		record.GoalID == goalID &&
		record.Revision == revision
}

// matchesInFlight 判定记录是否允许写入用量/评估（含 PAUSED）。
// Python: GoalManager._matches_in_flight (manager.py L391)
func matchesInFlight(record *GoalRecord, goalID string, revision int) bool {
	if record == nil || record.GoalID != goalID || record.Revision != revision {
		return false
	}
	return record.Status == GoalStatusActive || record.Status == GoalStatusPaused
}

// hasInFlightGoalAttempt 判定该目标是否仍有在飞轮尝试。
// Python: GoalManager._has_in_flight_goal_attempt (manager.py L382)
func (m *GoalManager) hasInFlightGoalAttempt(record *GoalRecord) bool {
	if m.eventManager == nil || record == nil {
		return false
	}
	return m.eventManager.HasRunningGoal(record.GoalID)
}
