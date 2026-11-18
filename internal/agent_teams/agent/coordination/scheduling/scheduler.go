// 本文件是 scheduled dispatch 的 leader 侧看板决策引擎 TeamScheduler。
//
// Python: agent/scheduling/scheduler.py（25.2a 调度半区：唤醒过滤 + _scan 五遍
// reconcile + 四扫描 + 投递原语 + leader digest/escalation；评审半区
// _reconcile_reviews 及其下游随 25.2b 插入，接缝在 scan 循环内按 Python 原顺序
// 标注）。
package scheduling

import (
	"context"
	"strconv"
	"sync"

	ateamcoordtypes "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	logComponent = logger.ComponentTeam

	// maxReviewRunsPerRound 一轮评审里单个 reviewer 未投票的重派上限，超过由
	// leader 裁决。Python: _MAX_REVIEW_RUNS_PER_ROUND（scheduler.py L66，25.2b 消费）。
	maxReviewRunsPerRound = 2

	// reviewNudgeSeconds 无票 reviewer 的 steer 收尾时点（秒）。
	// Python: _REVIEW_NUDGE_SECONDS（scheduler.py L72，25.2b 消费）。
	reviewNudgeSeconds = 600

	// maxScanPasses 单次扫描的不动点轮数上限：结算解锁的依赖任务要等 leader
	// 自己的事件 echo 才能在下一轮唤醒中被拾取；多扫几轮立刻收敛。幂等 CAS
	// 写让多余轮次成为 no-op。
	// Python: _MAX_SCAN_PASSES（scheduler.py L78）。
	maxScanPasses = 4
)

// ──────────────────────────── 结构体 ────────────────────────────

// SchedulerHost 调度器对宿主 TeamAgent 的窄依赖面。刻意极小：
// DeliverInput 只允许指向 leader 自己（digest / escalation），
// AutoStartMember 是信箱交接搭车的幂等惰性拉起原语（UNSTARTED→STARTING CAS）。
// Python: SchedulerHost Protocol（scheduler.py L88-104）
type SchedulerHost interface {
	// DeliverInput 把内容注入 leader 自己的输入流。
	// Python: deliver_input(content, *, use_steer=True)
	DeliverInput(ctx context.Context, content any, useSteer bool) error
	// AutoStartMember 尽力拉起一个 UNSTARTED 成员运行时。
	// Python: auto_start_member(member_name)
	AutoStartMember(ctx context.Context, memberName string) bool
}

// SchedulerTaskManager 调度半区对任务管理器的依赖面（窄接口，装配期由
// *tools.TeamTaskManager 适配）。评审半区方法随 25.2b 扩入。
type SchedulerTaskManager interface {
	// Get 读单条任务（带正文）。
	Get(ctx context.Context, taskID string) (*database.TeamTaskBase, error)
	// ListTasks 板面扫描轻量行（with_content=False 语义）。
	// Python: list_tasks(status, with_content=False)
	ListTasks(ctx context.Context, status string) ([]*database.TeamTaskBase, error)
	// StartTask 推进已指派 PENDING 任务到执行（含入队门/幂等/one-active 守卫）。
	// Python: start_task（task_manager.py L1791-1853）
	StartTask(ctx context.Context, taskID string) (schema.TaskOpResult, error)
	// GetOtherActiveTaskID 成员持有的另一条活跃任务（三活跃态）。
	GetOtherActiveTaskID(ctx context.Context, memberName, excludeTaskID string) (string, error)
	// HasOpenTasks 板面是否仍有非终态任务（一条聚合 COUNT，B_09）。
	HasOpenTasks(ctx context.Context) (bool, error)
	// BoardCounts (total, non_terminal) 聚合（B_09）。
	BoardCounts(ctx context.Context) (int64, int64, error)
	// OpenTaskCountsByAssignee 各成员名下未终态任务计数。
	OpenTaskCountsByAssignee(ctx context.Context) (map[string]int, error)
	// UnseenRevisions 全队级未见过当前 revision 的已指派未终态任务（F_129）。
	UnseenRevisions(ctx context.Context) ([]database.UnseenRevisionRow, error)
}

// SchedulerBackend 调度半区对团队后端的探针面（装配期由 *tools.TeamBackend 适配）。
type SchedulerBackend interface {
	// ListRetirableMembers 投影携带退役策略且空闲（READY）的成员（F_118）。
	ListRetirableMembers(ctx context.Context) ([]database.RetirableMemberRow, error)
	// ListShutdownMembers SHUTDOWN 成员名集合（每次唤醒的搁浅检查）。
	ListShutdownMembers(ctx context.Context) (map[string]bool, error)
	// ShutdownMember 发出常规 shutdown_member 请求。
	ShutdownMember(ctx context.Context, memberName string) schema.MemberOpResult
	// MembersWithUnread 返回候选成员中仍有未读输入的子集（F_118 未读条件）。
	MembersWithUnread(ctx context.Context, memberNames []string) (map[string]bool, error)
	// TaskVerificationEnabled 验证开关探针（review 半区消费，25.2b）。
	TaskVerificationEnabled(ctx context.Context) (bool, error)
}

// LeaderMessageSender leader 身份信箱投递面（装配期由 *tools.TeamMessageManager 适配）。
type LeaderMessageSender interface {
	// SendMessageWithMeta 以 leader 身份向成员信箱投一条带 meta 的消息（F_63）。
	SendMessageWithMeta(ctx context.Context, meta map[string]any, toMemberName string) (string, error)
	// MemberName leader 自身成员名（handoff 探测的 from 过滤）。
	MemberName() string
	// GetMessages 读某成员的信箱行（含已读——读到即算投递过）。
	GetMessages(ctx context.Context, toMemberName string, unreadOnly bool, fromMemberName string) ([]*database.TeamMessageBase, error)
}

// revisionKey (task_id, revision) 去重账本键（F_129 revision mail）。
type revisionKey struct {
	TaskID   string
	Revision int64
}

// handoffKey (task_id, owner) 已知信箱 handoff 账本键。
type handoffKey struct {
	TaskID string
	Owner  string
}

// escalateKey (task_id, review_round, kind) 升级去重账本键。
type escalateKey struct {
	TaskID      string
	ReviewRound int64
	Kind        string
}

// TeamScheduler scheduled dispatch 的 leader 侧任务板决策引擎。
// Python: TeamScheduler（scheduler.py L107-159 __init__）
type TeamScheduler struct {
	host       SchedulerHost
	taskMgr    SchedulerTaskManager
	backend    SchedulerBackend
	msgSender  LeaderMessageSender
	senderOnce sync.Once

	// defaultMaxRounds 每任务默认评审轮上限。Python: spec.default_max_review_rounds（25.2b 消费）。
	defaultMaxRounds int64
	// stallTimeoutSeconds 评审停滞升级超时（秒）。Python: spec.review_stall_timeout（25.2b 消费）。
	stallTimeoutSeconds int64
	// teamName 团队名（日志上下文）。Python: blueprint.spec.team_name
	teamName string
	// language 渲染语言（digest/escalation i18n）。Python: blueprint.language
	language string

	// active 调度器是否在驱动任务板。Python: self._active
	active bool
	// scanMu 串行化扫描（Python 靠事件总线单消费者串行；Go 侧显式持锁防绕过
	// 总线的调用方并发触发——asyncio 单线程共享状态到 Go 并发的既定移植模式）。
	scanMu sync.Mutex

	// 以下为进程内去重账本（全部内存态：leader 重启最坏重发一条信件/升级，
	// 板面真相在 DB —— Python __init__ L126-159 原注释）。
	// reviewDispatched 已派过 review 请求的 (task_id, round, reviewer)（25.2b）。
	reviewDispatched map[[3]string]struct{}
	// reviewRuns 每个 (task_id, round, reviewer) 已运行次数（25.2b）。
	reviewRuns map[[3]string]int
	// escalated 已发过的 (task_id, round, kind) 升级。
	escalated map[escalateKey]struct{}
	// summaryRequested 已请求返工总结的 (task_id, round)（25.2b）。
	summaryRequested map[[2]string]struct{}
	// digestedTasks 已发过终态 digest 的任务。
	digestedTasks map[string]struct{}
	// handoffKnown 已知信箱内有 start handoff 的 (task_id, owner)——写在本进程
	// 发出或扫描发现已有之时；省每次唤醒每活跃任务一次信箱查询，重启后每对
	// 恰好一次查询重新学得。
	handoffKnown map[handoffKey]struct{}
	// policyRetired 本进程按策略退役进 SHUTDOWN 的成员（lifetime "shutdown"）。
	// 只有它们搁浅的任务被升级。重启后遗忘，其搁浅任务被无声跳过。
	policyRetired map[string]struct{}
	// strandedEscalated 已因 owner 被策略退役而升级给 leader 的 (task_id, owner)
	// 对。每次扫描裁剪到仍搁浅的对——改派或完成的任务即掉出。
	strandedEscalated map[handoffKey]struct{}
	// revisionPushed 本进程已发过 revision mail 的 (task_id, revision)（F_129）。
	// 信箱投递会把该 revision 记为已见，终结条件；这里只防投递落地前的重发。
	revisionPushed map[revisionKey]struct{}
	// allDoneAnnounced all-done 一次性宣告标志（可 re-arm）。Python: _all_done_announced。
	allDoneAnnounced bool
	// reviewFeedbackDispatched 评审反馈 rail 已派发标志（25.2b）。
	reviewFeedbackDispatched bool
	// reviewFeedbackTasks 评审反馈异步任务集合（25.2b）。
	reviewFeedbackTasksMu sync.Mutex
	reviewFeedbackTasks   []func(context.Context) error
	// templateLoader 绑定团队后端 workspace cache 的 A 类加载器（懒绑定；
	// reviewer_* 模板读演进值）。Python: _template_loader_cache（scheduler.py L157-169）。
	templateLoader TemplateLoaderFunc
}

// TemplateLoaderFunc 模板加载器函数形（框架只读 LoadTemplate 或绑定 workspace
// cache 的 per-team A 类加载器均可注入）。Python: TemplateLoader 类型别名。
type TemplateLoaderFunc func(name, language string) (*ateamprompts.PromptTemplate, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamScheduler 构造调度器。构造期只建账本，不做任何 IO。
// Python: __init__（scheduler.py L110-159）
func NewTeamScheduler(host SchedulerHost, taskMgr SchedulerTaskManager, backend SchedulerBackend, msgSender LeaderMessageSender, defaultMaxRounds, stallTimeoutSeconds int64, teamName, language string) *TeamScheduler {
	return &TeamScheduler{
		host:                host,
		taskMgr:             taskMgr,
		backend:             backend,
		msgSender:           msgSender,
		defaultMaxRounds:    defaultMaxRounds,
		stallTimeoutSeconds: stallTimeoutSeconds,
		teamName:            teamName,
		language:            language,
		reviewDispatched:    map[[3]string]struct{}{},
		reviewRuns:          map[[3]string]int{},
		escalated:           map[escalateKey]struct{}{},
		summaryRequested:    map[[2]string]struct{}{},
		digestedTasks:       map[string]struct{}{},
		handoffKnown:        map[handoffKey]struct{}{},
		policyRetired:       map[string]struct{}{},
		strandedEscalated:   map[handoffKey]struct{}{},
		revisionPushed:      map[revisionKey]struct{}{},
	}
}

// SetTemplateLoader 绑定 per-team A 类加载器（团队后端 workspace cache；
// reviewer_* 模板读演进值）。Python: _template_loader property 的懒绑定面。
func (s *TeamScheduler) SetTemplateLoader(loader TemplateLoaderFunc) {
	s.templateLoader = loader
}

// Activate 武装调度器并对账一次。
// build_team 选定 scheduled 时与每次 kernel.start 对持久化 effective mode 为
// scheduled 的团队调用（warm resume / cold recovery）——激活即恢复扫描。幂等。
// Python: activate（scheduler.py L176-190）
func (s *TeamScheduler) Activate(ctx context.Context) {
	first := !s.active
	s.active = true
	s.allDoneAnnounced = false
	if first {
		logger.Info(logComponent).Str("team_name", s.teamName).Msg("[scheduler] activated for team")
	}
	_ = s.scan(ctx)
}

// Deactivate 解除武装（kernel pause/stop）。
// Python: deactivate（scheduler.py L192-194）
func (s *TeamScheduler) Deactivate() {
	s.active = false
}

// IsActive 调度器是否在驱动任务板。
// Python: is_active property（scheduler.py L171-174）
func (s *TeamScheduler) IsActive() bool { return s.active }

// OnEvent 来自 kernel 组合 wake 回调的唤醒提示。逐分支对齐 on_event：
//
//	内部事件：POLL_TASK / SCHEDULER_SCAN → 扫描；其余忽略；
//	跨进程事件：TASK_COMPLETED / TASK_VERIFIED → digest；
//	TASK_LIST_DRAINED → 一次性 all-done；task_*/member_* 前缀 → 扫描。
//
// swallow 语义：扫描失败绝不杀死事件总线循环，下一次唤醒或轮询重试同一幂等
// 扫描（对齐 except Exception: logger.error——协调框架的吞错语义）。
// Python: on_event（scheduler.py L196-225）
func (s *TeamScheduler) OnEvent(ctx context.Context, event ateamcoordtypes.CoordinationEvent) {
	if !s.active {
		return
	}
	s.onEventBody(ctx, event)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onEventBody 分离出以保留 Python 的 try/except 边界（recover 只包事件处理体）。
func (s *TeamScheduler) onEventBody(ctx context.Context, event ateamcoordtypes.CoordinationEvent) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Str("team_name", s.teamName).Msgf("[scheduler] scan failed: %v", r)
		}
	}()
	if event.IsInner() {
		if event.Inner.EventType == ateamcoordtypes.InnerEventTypePollTask ||
			event.Inner.EventType == ateamcoordtypes.InnerEventTypeSchedulerScan {
			_ = s.scan(ctx)
		}
		return
	}
	eventType := event.Transport.EventType
	switch eventType {
	case events.TeamEventTaskCompleted:
		s.digestCompletion(ctx, event.Transport, false)
	case events.TeamEventTaskVerified:
		s.digestCompletion(ctx, event.Transport, true)
	case events.TeamEventTaskListDrained:
		s.announceAllDone(ctx, event.Transport)
	}
	if hasTaskOrMemberPrefix(eventType) {
		_ = s.scan(ctx)
	}
}

// hasTaskOrMemberPrefix 判断跨进程事件类型是否落在 task_*/member_* 前缀。
// Python: on_event L219 的 startswith 判断。
func hasTaskOrMemberPrefix(eventType string) bool {
	return len(eventType) >= 5 && eventType[:5] == "task_" ||
		len(eventType) >= 7 && eventType[:7] == "member_"
}

// scan 五遍 reconcile 到有界不动点。
// retired 名册每次扫描读一次（没有 pass 会把 SHUTDOWN 成员拉回来，两个 pass
// 之间不可能变化）；stranded 集合末尾裁剪到仍然搁浅的对。
// Python: _scan（scheduler.py L231-253）
func (s *TeamScheduler) scan(ctx context.Context) error {
	if s.taskMgr == nil {
		return nil
	}
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if err := s.rearmAllDone(ctx); err != nil {
		return err
	}
	retired, err := s.retiredMembers(ctx)
	if err != nil {
		return err
	}
	stranded := map[handoffKey]struct{}{}
	for range maxScanPasses {
		acted, err := s.reconcileStarts(ctx, retired, stranded)
		if err != nil {
			return err
		}
		handoffActed, err := s.reconcileHandoffs(ctx, retired, stranded)
		if err != nil {
			return err
		}
		acted = handoffActed || acted
		// 25.2b 接缝：_reconcile_reviews 按 Python _scan L248 原顺序插在此处
		// （handoffs 与 retirements 之间）。Python: acted = await self._reconcile_reviews(task_manager) or acted
		retireActed, err := s.reconcileRetirements(ctx)
		if err != nil {
			return err
		}
		acted = retireActed || acted
		revActed, err := s.reconcileRevisions(ctx)
		if err != nil {
			return err
		}
		acted = revActed || acted
		if !acted {
			break
		}
	}
	// stranded 账本裁剪：只保留仍然搁浅的对（改派/完成即掉出）。
	// Python: self._stranded_escalated &= stranded（scheduler.py L252）
	for key := range s.strandedEscalated {
		if _, still := stranded[key]; !still {
			delete(s.strandedEscalated, key)
		}
	}
	return nil
}

// rearmAllDone 板面重新有活后复位 once-only 宣告。
// 清空的板不等于完成的目标：leader 可能又开返工或新评审轮，这些任务必须流进
// 自己的 wrap-up，所以任何非终态任务清除 once-only 标志。标志置位期间只探测
// 一条聚合 COUNT。
// Python: _rearm_all_done（scheduler.py L255-268）
func (s *TeamScheduler) rearmAllDone(ctx context.Context) error {
	if !s.allDoneAnnounced {
		return nil
	}
	open, err := s.taskMgr.HasOpenTasks(ctx)
	if err != nil {
		return err
	}
	if open {
		s.allDoneAnnounced = false
		logger.Info(logComponent).Msg("[scheduler] board reopened after wrap-up, all-done re-armed")
	}
	return nil
}

// retiredMembers SHUTDOWN 成员名集合；无团队后端时为空。
// Python: _retired_members（scheduler.py L358-363）
func (s *TeamScheduler) retiredMembers(ctx context.Context) (map[string]bool, error) {
	if s.backend == nil {
		return map[string]bool{}, nil
	}
	return s.backend.ListShutdownMembers(ctx)
}

// skipRetiredOwner 任务承担者是否已关闭；非预期时升级给 leader。
// 没有 startup 路径能把 SHUTDOWN 成员拉回来，所以 start/handoff 扫描放行它的
// 任务——启动一个只会把任务停在 IN_PROGRESS 无人执行，owner 也没有 runtime
// 做自己的过期清扫。每个这样的 (task, owner) 对记入 stranded。是否要告知
// leader 取决于谁退役了 owner：
//   - 调度器按策略（_policy_retired）——只在成员板面清空后才这么做，其上的
//     开放任务是它走后才交接的；没人再期待它，每对只告知一次；
//   - leader 经 shutdown_member——该工具契约把遗留交给 leader，不升级，
//     这也让「先关成员再 clean_team」的拆除保持安静。
//
// Python: _skip_retired_owner（scheduler.py L365-403）
func (s *TeamScheduler) skipRetiredOwner(ctx context.Context, task *database.TeamTaskBase, retired map[string]bool, stranded map[handoffKey]struct{}) bool {
	if task.Assignee == nil {
		return false
	}
	owner := *task.Assignee
	if !retired[owner] {
		return false
	}
	pair := handoffKey{TaskID: task.TaskID, Owner: owner}
	stranded[pair] = struct{}{}
	if _, byPolicy := s.policyRetired[owner]; !byPolicy {
		return true
	}
	if _, escalated := s.strandedEscalated[pair]; escalated {
		return true
	}
	s.strandedEscalated[pair] = struct{}{}
	logger.Warn(logComponent).
		Str("task_id", task.TaskID).Str("status", task.Status).Str("owner", owner).
		Msg("[scheduler] task is assigned to shut-down member; escalating to the leader")
	_ = s.host.DeliverInput(ctx, RenderLeaderEscalationRetiredOwner(task, owner), false)
	return true
}

// reconcileStarts 给每个空闲成员启动其最早的已指派 PENDING 任务。
// retired 中的 owner 被跳过（见 skipRetiredOwner）：其任务停在 PENDING 等
// leader 改派。
// Python: _reconcile_starts（scheduler.py L405-444）
func (s *TeamScheduler) reconcileStarts(ctx context.Context, retired map[string]bool, stranded map[handoffKey]struct{}) (bool, error) {
	pending, err := s.taskMgr.ListTasks(ctx, string(schema.TaskStatusPending))
	if err != nil {
		return false, err
	}
	queueByMember := map[string][]*database.TeamTaskBase{}
	var memberOrder []string
	for _, task := range pending {
		if task.Assignee == nil || s.skipRetiredOwner(ctx, task, retired, stranded) {
			continue
		}
		owner := *task.Assignee
		if _, seen := queueByMember[owner]; !seen {
			memberOrder = append(memberOrder, owner)
		}
		queueByMember[owner] = append(queueByMember[owner], task)
	}

	acted := false
	for _, memberName := range memberOrder {
		queue := queueByMember[memberName]
		// 从子任务回来的父任务（F_128）优先：其 owner 可能已接了别的活，
		// 半成品任务不该排在新鲜任务后面。
		var resumable, all []*database.TeamTaskBase
		for _, task := range queue {
			all = append(all, task)
			if task.ResumePending {
				resumable = append(resumable, task)
			}
		}
		queue = all
		if len(resumable) > 0 {
			queue = resumable
		}
		candidate := earliestTask(queue)
		busyTaskID, err := s.taskMgr.GetOtherActiveTaskID(ctx, memberName, candidate.TaskID)
		if err != nil {
			return acted, err
		}
		if busyTaskID != "" {
			continue
		}
		resume := candidate.ResumePending
		result, err := s.taskMgr.StartTask(ctx, candidate.TaskID)
		if err != nil {
			return acted, err
		}
		if !result.OK {
			// 竞争失败或成员转忙——下一次唤醒重试。
			logger.Debug(logComponent).
				Str("task_id", candidate.TaskID).Str("member", memberName).Str("reason", result.Reason).
				Msg("[scheduler] start of task for member did not apply")
			continue
		}
		started, err := s.taskMgr.Get(ctx, candidate.TaskID)
		if err != nil {
			return acted, err
		}
		if started == nil {
			continue
		}
		if s.sendTaskStart(ctx, started, resume) {
			acted = true
		}
	}
	return acted, nil
}

// earliestTask 取 (updated_at, task_id) 最小者（updated_at 为 0 视作最早，
// 对齐 Python `task.updated_at or 0`）。
// Python: min(resumable or queue, key=lambda task: (task.updated_at or 0, task.task_id))（L424）
func earliestTask(tasks []*database.TeamTaskBase) *database.TeamTaskBase {
	best := tasks[0]
	for _, task := range tasks[1:] {
		bestAt, at := best.UpdatedAt, task.UpdatedAt
		if at < bestAt || (at == bestAt && task.TaskID < best.TaskID) {
			best = task
		}
	}
	return best
}

// reconcileHandoffs 把每条活跃任务交给从未收到启动信的 owner。
//
// start 扫描恰好在把任务翻出 PENDING 时交接。leader 对已活跃任务（
// TASK_REASSIGNABLE_STATUSES 保持状态）做 update_task(assignee=...) 不产生
// 交接：旧 owner 由 TASK_REVOKED steer 走，但新 owner 只看到一条瞬态
// TASK_CLAIMED——runtime 未起就什么都看不到，且 scheduled 模式没有启动看板
// 巡检可追补（ScheduledTaskBoardHandler.on_initial_task_poll）。任务就此停在
// IN_PROGRESS 无人做，直到 owner 自己的过期清扫或 leader 发现。
//
// 本 pass 仅凭板面补上缺口：每条活跃任务的当前 owner 信箱里必须持有绑定该
// 任务的 leader 身份 start handoff（持久——离线 owner 启动时 drain）。缺失 →
// 发 start 扫描会发的同一条启动消息，它同样惰性拉起 owner。幂等经信箱本身
// 加进程内 _handoff_known 备忘；重启的 leader 每对花一次信箱读取重新学得。
// retired 中的 owner 被跳过——没有 handoff 能拉起他们（见 skipRetiredOwner）；
// 该检查先于备忘运行，handoff 已落地的 owner 离开时仍能被发现。
//
// Python: _reconcile_handoffs（scheduler.py L446-494）
func (s *TeamScheduler) reconcileHandoffs(ctx context.Context, retired map[string]bool, stranded map[handoffKey]struct{}) (bool, error) {
	if s.msgSender == nil {
		return false, nil
	}
	var active []*database.TeamTaskBase
	for _, status := range []schema.TaskStatus{schema.TaskStatusPlanning, schema.TaskStatusInProgress} {
		rows, err := s.taskMgr.ListTasks(ctx, string(status))
		if err != nil {
			return false, err
		}
		active = append(active, rows...)
	}
	acted := false
	for _, task := range active {
		if task.Assignee == nil {
			continue
		}
		owner := *task.Assignee
		if s.skipRetiredOwner(ctx, task, retired, stranded) {
			continue
		}
		pair := handoffKey{TaskID: task.TaskID, Owner: owner}
		if _, known := s.handoffKnown[pair]; known {
			continue
		}
		has, err := s.ownerHasStartHandoff(ctx, owner, task.TaskID)
		if err != nil {
			return acted, err
		}
		if has {
			s.handoffKnown[pair] = struct{}{}
			continue
		}
		logger.Info(logComponent).
			Str("task_id", task.TaskID).Str("owner", owner).
			Msg("[scheduler] active task has no start handoff for its owner; sending one")
		if s.sendTaskStart(ctx, task, false) {
			acted = true
		}
	}
	return acted, nil
}

// ownerHasStartHandoff owner 信箱里是否持有绑定 task_id 的 leader start handoff。
// 已读未读不论——已 drain 的 handoff 算已投递，未 drain 的会在 owner 下一次
// 信箱清扫时到达。
// Python: _owner_has_start_handoff（scheduler.py L496-509）
func (s *TeamScheduler) ownerHasStartHandoff(ctx context.Context, owner, taskID string) (bool, error) {
	rows, err := s.msgSender.GetMessages(ctx, owner, false, s.msgSender.MemberName())
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if IsTaskStartMeta(ateamprompts.ParseMeta(row.Meta), taskID) {
			return true, nil
		}
	}
	return false, nil
}

// sendTaskStart 给 owner 发启动 handoff 并记住它在信箱里。
// resume 在 start_task 消费它之前从行上读出（F_128）。handoff 兜底永不传它：
// 缺 handoff 的活跃任务有了新 owner，它开始任务而非恢复。
// Python: _send_task_start（scheduler.py L511-525）
func (s *TeamScheduler) sendTaskStart(ctx context.Context, task *database.TeamTaskBase, resume bool) bool {
	if task.Assignee == nil {
		return false
	}
	owner := *task.Assignee
	sent := s.sendAsLeader(ctx, owner, MetaTaskStart(task, resume))
	if sent {
		s.handoffKnown[handoffKey{TaskID: task.TaskID, Owner: owner}] = struct{}{}
		// handoff 按投递时的任务现状渲染，所以已携带本 revision：不再单独发
		// revision mail（F_129）。
		// Python: self._revision_pushed.add((task.task_id, int(getattr(task, "revision", 0) or 0)))
		s.revisionPushed[revisionKey{TaskID: task.TaskID, Revision: task.Revision}] = struct{}{}
	}
	return sent
}

// reconcileRetirements 给板面已空的 task-scoped 成员发退役请求（F_118）。
//
// create_task 内联声明的成员在其行上携带 lifetime 策略。一旦它已拉起且再无
// 可做之事，调度器发出常规 shutdown_member 请求；成员从同一策略自行落入
// SHUTDOWN 或 DORMANT——本 pass 永不碰一轮。
//
// 三个条件各堵一个洞：
//   - 空闲（READY）——绝不 BUSY（轮中：对它正在说的话的回复会搁浅）、
//     UNSTARTED（行落了任务没落）、RESTARTING（唤醒进行中）或已请求过；
//   - 名下无非终态任务——IN_REVIEW / PENDING / BLOCKED 都算开放，已提交的
//     任务让作者留到返工和验收后汇报；
//   - 无未读——刚唤醒的 DORMANT 成员在读到唤醒它的消息之前就 READY 了；
//     在那个窗口请求退役会让它无轮即 settle，消息搁浅。
//
// lifetime 只存在于内联成员，整个 pass 由 constants.InlineTaskMemberEnabled
// 关死。Python: _reconcile_retirements（scheduler.py L299-356）
func (s *TeamScheduler) reconcileRetirements(ctx context.Context) (bool, error) {
	if !schema.InlineTaskMemberEnabled {
		return false, nil
	}
	if s.backend == nil || s.msgSender == nil {
		return false, nil
	}
	candidates, err := s.backend.ListRetirableMembers(ctx)
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		return false, nil
	}
	openCounts, err := s.taskMgr.OpenTaskCountsByAssignee(ctx)
	if err != nil {
		return false, err
	}
	names := make([]string, 0, len(candidates))
	for _, row := range candidates {
		names = append(names, row.MemberName)
	}
	unread, err := s.backend.MembersWithUnread(ctx, names)
	if err != nil {
		return false, err
	}
	acted := false
	for _, row := range candidates {
		if openCounts[row.MemberName] > 0 || unread[row.MemberName] {
			continue
		}
		result := s.backend.ShutdownMember(ctx, row.MemberName)
		if !result.OK {
			logger.Debug(logComponent).
				Str("member", row.MemberName).Str("status", row.Status).Str("reason", result.Reason).
				Msg("[scheduler] retirement request did not apply")
			continue
		}
		if row.Lifetime == "shutdown" {
			s.policyRetired[row.MemberName] = struct{}{}
		}
		logger.Info(logComponent).
			Str("member", row.MemberName).Str("lifetime", row.Lifetime).Str("was", row.Status).
			Msg("[scheduler] retirement requested")
		acted = true
	}
	return acted, nil
}

// reconcileRevisions 把被编辑任务的最新正文推给未见过它的执行者（F_129）。
//
// 由板面 level-triggered：revision 领先 assignee 所见 revision 的任务收一封信，
// 无论编辑事件遭遇了什么。执行中的任务收到 revision 本身；停在等子任务的
// 父任务收到审视请求（其 owner 必须对照新内容检查子任务，而不是恢复父任务）。
// 尚未开始的任务什么都不收——它的 start handoff 渲染当前 revision。信是持久
// 信箱行，同时拉起离线 owner，投递它即把该 revision 记为已见，终结条件。
// Python: _reconcile_revisions（scheduler.py L270-297）
func (s *TeamScheduler) reconcileRevisions(ctx context.Context) (bool, error) {
	rows, err := s.taskMgr.UnseenRevisions(ctx)
	if err != nil {
		return false, err
	}
	acted := false
	for _, row := range rows {
		key := revisionKey{TaskID: row.TaskID, Revision: row.Revision}
		if _, pushed := s.revisionPushed[key]; pushed {
			continue
		}
		var meta map[string]any
		if row.Status == string(schema.TaskStatusPlanning) || row.Status == string(schema.TaskStatusInProgress) {
			meta = MetaTaskRevised(row.TaskID)
		} else if row.Status == string(schema.TaskStatusBlocked) && row.ResumePending {
			meta = MetaSubtaskReview(row.TaskID)
		} else {
			continue
		}
		if s.sendAsLeader(ctx, row.Assignee, meta) {
			s.revisionPushed[key] = struct{}{}
			logger.Info(logComponent).
				Str("revision", strconv.FormatInt(row.Revision, 10)).
				Str("task_id", row.TaskID).Str("assignee", row.Assignee).
				Msg("[scheduler] pushed revision of task to assignee")
			acted = true
		}
	}
	return acted, nil
}

// sendAsLeader leader 身份信箱 handoff + 幂等的成员惰性拉起。
//
// 行携带投递载荷而非正文：content 为空、meta 指名模板及其绑定的任务，收件方
// 的信箱 drain 按*当下*的任务行渲染（F_63）。行先落（持久——离线成员第一次
// 信箱清扫就 drain 它），然后经 send_message 工具用的同一 UNSTARTED→STARTING
// CAS 尽力拉起 runtime；已运行的成员直接收到 MESSAGE 唤醒。单收件人失败记
// 日志且绝不中止扫描。
//
// 返回 true 当且仅当信箱行落地（成员启动是尽力而为，不影响结果——启动失败
// 若让调用方以为信没落地，下一次扫描会重发）。
// Python: _send_as_leader（scheduler.py L1085-1118）
func (s *TeamScheduler) sendAsLeader(ctx context.Context, memberName string, meta map[string]any) bool {
	if s.msgSender == nil {
		return false
	}
	messageID, err := s.msgSender.SendMessageWithMeta(ctx, meta, memberName)
	if err != nil {
		logger.Error(logComponent).Str("member", memberName).Err(err).
			Msg("[scheduler] handoff to member failed")
		return false
	}
	if messageID == "" {
		logger.Error(logComponent).Str("member", memberName).
			Msg("[scheduler] handoff message to member was not delivered")
	}
	// 行就是 handoff；启动是独立的尽力步骤，其失败不得让调用方认为信没落地
	//（否则下一次扫描会重发）。
	// Python: try: await self._host.auto_start_member(member_name) except → error log
	_ = s.host.AutoStartMember(ctx, memberName)
	return messageID != ""
}

// escalate 向 leader 注入一次升级，按 (task_id, round, kind) 去重。
// 轮上限与停滞升级共用 "verdict" kind：两者汇报同一未决轮，其中之一之后的
// 迟到决定性投票不得再次升级。持续无票收尾的 reviewer 是自己的 kind（每
// reviewer 一个），同轮更早的停滞升级不吞掉它。
// Python: _escalate（scheduler.py L1120-1145）
func (s *TeamScheduler) escalate(ctx context.Context, task *database.TeamTaskBase, content, kind string) {
	key := escalateKey{TaskID: task.TaskID, ReviewRound: task.ReviewRound, Kind: kind}
	if _, sent := s.escalated[key]; sent {
		logger.Debug(logComponent).
			Str("kind", kind).Str("task_id", task.TaskID).Int64("round", task.ReviewRound).
			Msg("[scheduler] escalation for task round already sent; skipped")
		return
	}
	s.escalated[key] = struct{}{}
	logger.Info(logComponent).
		Str("task_id", task.TaskID).Int64("round", task.ReviewRound).Str("message", content).
		Msg("[scheduler] escalating task round to the leader")
	_ = s.host.DeliverInput(ctx, content, false)
}

// digestTaskDone 每 task 一次的终态 digest 给 leader。
//
// 携带任务的完成摘要（F_119），它顶替成员自己的完成汇报。同时是 leader 本地
// 结算板面的 all-done 兜底：leader 本地结算发布的 drained 事件在总线上被
// 自过滤，零剩余的 digest 直接宣告 wrap-up。
//
// 成员的子任务（F_128）没有自己的 digest：其结果经 resume handoff 回到父任务
// 的 owner，leader 只从父任务听到工作一次。它仍计入 all-done 检查。
// Python: _digest_task_done（scheduler.py L1147-1178）
func (s *TeamScheduler) digestTaskDone(ctx context.Context, task *database.TeamTaskBase, verified bool) {
	if _, done := s.digestedTasks[task.TaskID]; done {
		return
	}
	s.digestedTasks[task.TaskID] = struct{}{}
	total, remaining, err := s.taskMgr.BoardCounts(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("[scheduler] board_counts failed during digest")
		return
	}
	if task.ParentTaskID == nil {
		_ = s.host.DeliverInput(ctx, RenderLeaderTaskDone(task, verified), false)
	}
	if remaining == 0 && total > 0 && !s.allDoneAnnounced {
		s.allDoneAnnounced = true
		_ = s.host.DeliverInput(ctx, RenderLeaderAllDone(total), false)
	}
	if remaining == 0 && total > 0 {
		s.dispatchTeamReviewFeedback(ctx)
	}
}

// digestCompletion 经由传输事件观察到的完成的 digest。
//
// 覆盖调度器自己没有执行的转换（成员的直接无 reviewer 完成）。本地执行的
// 结算在结算点 digest——leader 自己的事件不通过总线回来（自过滤）。
// Python: _digest_completion（scheduler.py L1180-1207）
func (s *TeamScheduler) digestCompletion(ctx context.Context, event *events.EventMessage, verified bool) {
	if s.taskMgr == nil {
		return
	}
	taskID := payloadString(event.Payload, "task_id")
	if taskID == "" {
		return
	}
	if _, done := s.digestedTasks[taskID]; done {
		return
	}
	task, err := s.taskMgr.Get(ctx, taskID)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("task_id", taskID).
			Msg("[scheduler] digest completion get failed")
		return
	}
	if task == nil {
		// 行在事件与 digest 之间被清扫：单独宣告 id 而不是丢掉这次完成。
		// Python: task = SimpleNamespace(task_id=task_id, title="", summary=None, note=None)
		task = &database.TeamTaskBase{TaskID: taskID}
	}
	s.digestTaskDone(ctx, task, verified)
}

// announceAllDone 向 leader 注入一次性最终 all-terminal digest。
//
// drained 事件可能在 leader 已开新工作之后才到达总线循环：重查板面，让过期
// 事件永不告诉 leader 开放的板是空的（B_09）。检查刻意在标志测试之前：刚
// re-open 之后标志可能仍置位直到下一次扫描 re-arm 它，那个窗口里的过期事件
// 同样不得触达 once-only 的团队评审反馈。
// Python: _announce_all_done（scheduler.py 搜索 _announce_all_done）
func (s *TeamScheduler) announceAllDone(ctx context.Context, event *events.EventMessage) {
	if s.taskMgr != nil {
		open, err := s.taskMgr.HasOpenTasks(ctx)
		if err == nil && open {
			return
		}
	}
	if !s.allDoneAnnounced {
		s.allDoneAnnounced = true
		_ = s.host.DeliverInput(ctx, RenderLeaderAllDone(int64(payloadInt(event.Payload, "task_count"))), false)
	}
	s.dispatchTeamReviewFeedback(ctx)
}

// dispatchTeamReviewFeedback 25.2b 接缝：板面清空时派发团队级评审反馈 rail
// 调用（TeamSkillEvolutionRail.finalize_review_feedback）。25.2a 无消费方，
// 计划裁决按 Python 原顺序保留调用点、函数体随 25.2b 落。
// Python: _dispatch_team_review_feedback（scheduler.py L673-706）
func (s *TeamScheduler) dispatchTeamReviewFeedback(_ context.Context) {
	// 25.2b：等 in-flight 反馈任务聚合完毕后调 rail.finalize_review_feedback(payload)
}

// payloadString 从事件载荷取字符串值（对齐 Python payload.task_id 的缺省空串）。
func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, _ := payload[key].(string)
	return v
}

// payloadInt 从事件载荷取数值（JSON 解出的数字是 float64）。
func payloadInt(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	switch v := payload[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}
