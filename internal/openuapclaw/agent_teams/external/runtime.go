package external

// CLI 成员运行时：对齐 Python openjiuwen/agent_teams/external/runtime.py
// （1093 行全文，一比一移植）。
//
// 这些是 TeamHarness 的 P2 对应物：不驱动本地 DeepAgent，而是驱动一个外部 CLI
// 作为成员的大脑。两种形态实现同一 MemberRuntime 表面，按 adapter 的
// SupportsStdinInjection 选择：
//
//   - ExternalCliRuntime —— 流式。一个长驻子进程；一轮把入站文本投给它的
//     stdin 并读 stdout 直到逐 CLI adapter 判定本轮完成。支持中途 steer。
//   - ReinvokeCliRuntime —— one-shot / 每轮重起。每轮一个全新子进程
//     （prompt 走 argv，读 stdout 到 EOF）；用于以 flag 收 prompt 并退出的
//     CLI（openclaw、hermes）。无中途 steer——轮内到达的消息被缓冲并在轮次
//     结束前作为后续重起排空。
//
// 两种形态下 CLI 的动作（消息、任务操作）都经团队 CLI/MCP 工具出进程流动，
// 所以 CLI 的 stdout 保持内部不作为 team-stream 块呈现。rail/记忆钩子是
// no-op（configurator 对外部 CLI 成员跳过那些特性）。
//
// 偏差：Python async generator（_drive）→ Go channel（D-15.13-b）；
// asyncio.Queue/Task → goroutine + channel + mutex；事件总线
// AsyncCallbackFramework → 进程内回调注册表（订阅契约等价）。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external/transport"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tracker"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// ──────────────────────────── 结构体 ────────────────────────────

// memberSessionFactory 成员子会话工厂：由调用方（15.16 接线）注入。
// Python 经鸭子类型 hasattr(team_session, "create_agent_session") 探测；
// Go 用构造期注入函数指针承载同一数据源（D-15.13-g 登记于 spec）。
type memberSessionFactory func() (sessioninterfaces.SessionFacade, error)

// stateCallback harness.state 事件的回调形状。
type stateCallback func(old, new harnessprotocol.HarnessState, sessionID string)

// roundCallback harness.round 事件的回调形状。
type roundCallback func(kind string, roundID int, turnID string, result *harnessprotocol.TurnResult)

// outputQueue 无界输出队列：对齐 Python 无界 asyncio.Queue（偏差 D-15.13-j）。
// Push 永不阻塞（内部 slice 缓冲 + 单槽通知），使 driveTurn 泵送不会因消费者
// 暂缓读取而挂死、Stop 等待轮次 goroutine 永不超时；Close 后泵把已缓冲块
// 排空再关闭通道面（保留 Python stop 后 _END 前队列内容仍可消费的语义）。
type outputQueue struct {
	mu         sync.Mutex
	buf        []stream.OutputSchema
	notify     chan struct{} // 容量 1；Push 时非阻塞投递唤醒泵
	closed     chan struct{} // Close 时关闭（sync.Once 保证单次）
	closedOnce sync.Once
	closedFlag bool
}

// newOutputQueue 创建空队列。
func newOutputQueue() *outputQueue {
	return &outputQueue{notify: make(chan struct{}, 1), closed: make(chan struct{})}
}

// Push 入队一个输出块；永不阻塞。Close 之后到达的块丢弃
// （Python 侧 stop 先 await 轮次任务再推 _END，正常路径无此窗口）。
func (q *outputQueue) Push(v stream.OutputSchema) {
	q.mu.Lock()
	if q.closedFlag {
		q.mu.Unlock()
		return
	}
	q.buf = append(q.buf, v)
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// Close 标记队列终止；泵在缓冲排空后关闭通道面。
func (q *outputQueue) Close() {
	q.closedOnce.Do(func() {
		q.mu.Lock()
		q.closedFlag = true
		q.mu.Unlock()
		close(q.closed)
	})
}

// Channel 启动泵并返回通道面。泵逐块投递已缓冲内容；Close 后继续排空剩余
// 缓冲，清空后关闭通道。消费者完全放弃读取时泵会阻塞在最后一次投递上——
// 遗留一个有界 goroutine（缓冲即上界），Python 侧等价为队列内容随对象回收。
func (q *outputQueue) Channel() <-chan stream.OutputSchema {
	out := make(chan stream.OutputSchema)
	go func() {
		defer close(out)
		for {
			q.mu.Lock()
			if len(q.buf) > 0 {
				v := q.buf[0]
				q.buf = q.buf[1:]
				q.mu.Unlock()
				out <- v
				continue
			}
			isClosed := q.closedFlag
			q.mu.Unlock()
			if isClosed {
				return
			}
			select {
			case <-q.notify:
			case <-q.closed:
			}
		}
	}()
	return out
}

// cliRuntimeBase CLI 成员运行时的共享 MemberRuntime 表面。
//
// 把每种形态的单轮 drive 生成器适配成团队 StreamController 驱动的多轮交互
// 表面：IDLE send 开一轮，其叙述块流经队列化的 outputs 迭代器，相位/轮次
// 迁移在 NativeHarness 用的同一私有事件总线上触发。
//
// Python: CliRuntimeBase (runtime.py:136)
type cliRuntimeBase struct {
	mu sync.Mutex

	// memberName 成员语义名
	memberName string
	// memberAgentID 寻址本成员自身 checkpoint 的稳定 TeamAgent card id。
	// 由它派生的成员 AgentSession 是后端原生 resume 状态（Codex thread id）
	// 与团队上下文投递基线的所在。
	memberAgentID string
	// teamContextTracker 把待告知团队状态注入出站消息的 tracker。
	teamContextTracker *tracker.TeamContextTracker
	// taskOutcomeTracker 把未告知任务事实（评审通过/漏掉的编辑）折叠进下一条
	// 消息的 tracker。构造后绑定，与团队上下文 tracker 一样。
	taskOutcomeTracker tracker.TaskEventTracker
	// newMemberSession 成员子会话工厂（Python duck-typing create_agent_session
	// 的 Go 注入面）；nil 表示无持久化成员状态。
	newMemberSession memberSessionFactory
	// memberSession 本成员的稳定子 AgentSession；无会话时 nil。
	memberSession sessioninterfaces.SessionFacade
	// phase 映射到团队 HarnessState 词汇的生命周期相位，使 StreamController
	// 把 CLI runtime 完全当 NativeHarness 对待。
	phase harnessprotocol.HarnessState
	// outputQueue 本运行周期的输出队列（无界，等价 Python 无界 asyncio.Queue，
	// 偏差 D-15.13-j）；Close 后消费者把已缓冲块排空再终止。每次 start 重建。
	outputQueue *outputQueue
	// outputCh 输出队列的通道面（outputQueue 泵在排空后关闭它）。stop 后
	// 返回已关闭通道，消费者 range 立即终止（Python _END 哨兵语义的 Go 等价物）。
	outputCh <-chan stream.OutputSchema
	// onState/onRound 订阅回调（NativeHarness 同款契约）。
	onState []stateCallback
	onRound []roundCallback
	// turnCancel 在飞轮次的取消函数；IDLE 时 nil。
	turnCancel context.CancelFunc
	// turnDone 在飞轮次 goroutine 的退出信号；Stop 等它之后再关输出通道，
	// 避免对已关闭通道发送（Python stop 先 await task 再推 _END）。
	turnDone chan struct{}
	// roundSeq 本 runtime 实例的单调轮次 id。
	roundSeq int
	// turnAborted 由 abort 置位，使轮次驱动器把终局分类为 aborted 而非
	// finished；下一轮开始时复位。
	turnAborted bool
	// currentRoundID 在飞轮次的轮次 id。
	currentRoundID int
	// reliabilityCtx Claude/Codex SDK runtime 的可靠性上下文；经
	// BindReliabilityContext 注入（实现见 reliability.go）。CLI adapter runtime
	//（传统 stdin/重起 CLI）恒 nil——外部 runtime 可靠性循环的范围之外。
	reliabilityCtx *RuntimeReliabilityContext

	// driveErr flavor 的 _drive 在轮内产生的错误（注入失败 / premature-EOF）。
	// Python 经 async generator 异常传播到 _drive_turn 的 except 分支；Go 的
	// drive 面是 channel，改为先落字段、driveTurn 在排空后读取并归入 crashed
	// 分类（D-15.13-b 同源偏差）。
	driveErr error

	// flavor 钩子（构造期注入函数指针；嵌入结构体的方法体静态绑定基类实现，
	// 子类型覆盖不生效——虚方法派发陷阱，见 Go embedding 陷阱记忆）。
	// Python 侧 _drive/steer/follow_up/_abort_turn/aclose 是子类多态方法。
	driveFn     func(ctx context.Context, inputs map[string]any) (<-chan stream.OutputSchema, context.CancelFunc)
	steerFn     func(content string) error
	followUpFn  func(content string) error
	abortTurnFn func()
	acloseFn    func()
	// started 标记 start 已调用（start 幂等准备）。
	started bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// harness 私有事件主题 + 命名空间，镜像 NativeHarness，使团队
	// StreamController 能用同一 subscribe(on_state=, on_round=) 契约订阅 CLI
	// runtime。kind 取 started / finished / aborted / failed（CLI 轮次无
	// paused 相位）。Python: _EVENT_STATE/_EVENT_ROUND/_EVENT_NAMESPACE
	eventStateTopic = "harness.state"
	eventRoundTopic = "harness.round"
	eventNamespace  = "cli_runtime"

	// followupSep 把缓冲的轮中消息拼成一条后续 prompt。
	// Python: _FOLLOWUP_SEP
	followupSep = "\n\n---\n\n"

	// stderrTailLimit 失败 CLI 的 stderr 诊断日志保留长度。
	// Python: _STDERR_TAIL_LIMIT
	stderrTailLimit = 2000

	// defaultInactivityTimeoutS 重起 one-shot CLI 轮次默认无输出（无活动）
	// 上限。只有子进程静默这么久才杀轮——做长但活跃工作的 CLI 永不被杀，
	// 挂起/启动卡死的才被杀。
	// Python: _DEFAULT_INACTIVITY_TIMEOUT_S
	defaultInactivityTimeoutS = 180.0

	// stderrPeekTimeoutS 流式进程后台 stderr 排空在 premature-EOF 路径上
	// 交付尾部的有界等待。排空在 stderr EOF 结束——死进程立即到达；上限防
	// 半关闭管道。
	// Python: _STDERR_PEEK_TIMEOUT_S
	stderrPeekTimeoutS = 5.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Start 为一个运行周期准备 CLI runtime 并打开成员会话。
// 成员 AgentSession 从团队会话派生并在任何轮次运行前恢复，因此团队上下文
// 投递基线（及 SDK 子类的后端原生 resume 状态）从第一条消息起就可用。
// teamSession 由 SetMemberSessionFactory 的工厂闭包在构造期捕获（D-15.13-g），
// 本签名保留 Python start(team_session) 的参数面，实参在此不参与派生。
//
// Python: CliRuntimeBase.start (runtime.py:192)
func (b *cliRuntimeBase) Start(teamSession any) error {
	_ = teamSession
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil
	}
	b.started = true
	b.mu.Unlock()
	if _, err := b.ensureMemberSession(); err != nil {
		return err
	}
	b.mu.Lock()
	q := newOutputQueue()
	b.outputQueue = q
	b.outputCh = q.Channel()
	b.turnAborted = false
	b.phase = harnessprotocol.HarnessStateIdle
	b.mu.Unlock()
	return nil
}

// BindTeamContextTracker 绑定把待告知团队状态注入出站消息的 tracker。
// Python: bind_team_context_tracker (runtime.py:205)
func (b *cliRuntimeBase) BindTeamContextTracker(t *tracker.TeamContextTracker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.teamContextTracker = t
}

// BindTaskOutcomeTracker 绑定把未告知任务事实折叠进下一条消息的 tracker。
// Python: bind_task_outcome_tracker (runtime.py:209)
func (b *cliRuntimeBase) BindTaskOutcomeTracker(t tracker.TaskEventTracker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.taskOutcomeTracker = t
}

// BindReliabilityContext 绑定可靠性失败/重试投递面。
// Python 对 adapter 路 runtime 也调用本方法（external_cli_spawn.py:726），
// 基类 no-op 无害——Go 侧签名与 ExternalHarnessMemberRuntime 同形，wiring 的
// typed 断言对 adapter 路同样命中（no-op 直通），语义一致。
// Python: bind_reliability_context (runtime.py:213)
func (b *cliRuntimeBase) BindReliabilityContext(
	string, *tools.TeamBackend, string, UpdateStatusCallback, messager.Messager,
) {
	// 基础 CLI adapter runtime 不在 external-runtime 可靠性循环范围：no-op
	//（Python 基类 return None，不存储上下文）。
}

// SetMemberSessionFactory 注入成员子会话工厂（Python duck-typing 的 Go 面）。
func (b *cliRuntimeBase) SetMemberSessionFactory(f memberSessionFactory) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.newMemberSession = f
}

// ensureMemberSession 只打开一次本成员的稳定子 AgentSession。
// 以 memberAgentID 为键，所以 checkpoint 是本成员自己的，不会与兄弟冲突。
// 没有工厂（独立单测，或从未接线的调用方）时 runtime 直接无持久化成员状态运行。
//
// Python: _ensure_member_session (runtime.py:231)
func (b *cliRuntimeBase) ensureMemberSession() (sessioninterfaces.SessionFacade, error) {
	b.mu.Lock()
	if b.memberSession != nil {
		s := b.memberSession
		b.mu.Unlock()
		return s, nil
	}
	factory := b.newMemberSession
	id := b.memberAgentID
	b.mu.Unlock()
	if factory == nil || id == "" {
		return nil, nil
	}
	sess, err := factory()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.memberSession = sess
	b.mu.Unlock()
	return sess, nil
}

// Stop 停止在飞轮次、释放 CLI transport、关闭 outputs。幂等。
// 先把相位标为 TERMINATED，使轮次驱动器在 abort 诱发的返回上跳过收尾的
// 轮次/状态事件；然后等待轮次 goroutine、释放 transport 并关闭输出通道，
// 使迭代 outputs 的消费者干净终止。
//
// Python: CliRuntimeBase.stop (runtime.py:253)
func (b *cliRuntimeBase) Stop() {
	b.mu.Lock()
	if b.phase == harnessprotocol.HarnessStateTerminated {
		b.mu.Unlock()
		return
	}
	b.phase = harnessprotocol.HarnessStateTerminated
	b.turnAborted = true
	cancel := b.turnCancel
	b.turnCancel = nil
	done := b.turnDone
	b.turnDone = nil
	q := b.outputQueue
	b.mu.Unlock()

	b.abortTurnFn()
	if cancel != nil {
		cancel()
	}
	// Go 侧先 aclose 再等轮次 goroutine：ExternalCliRuntime.drive 阻塞在
	// stdout ReadLine（无 ctx 感知），只有终止进程产生的 EOF 能解除阻塞；
	// Python 的 readline 是 async 可被取消，顺序为 await task → aclose。
	// 两者净效果一致：轮次任务在推 _END（关通道）前被收编。
	b.acloseFn()
	if done != nil {
		<-done
	}
	// 关闭输出队列：泵在排空剩余缓冲后关闭 outputCh（Python _END 哨兵语义）。
	// 轮次 goroutine 已收编，Push 不再有竞态窗口。队列无界（Push 永不阻塞），
	// 本调用不会因消费者暂缓读取而挂死（偏差 D-15.13-j）。
	if q != nil {
		q.Close()
	}
	return
}

// Dispose 永久处置本 CLI runtime。
// Python: dispose (runtime.py:275)
func (b *cliRuntimeBase) Dispose() { b.Stop() }

// State 返回当前生命周期相位。
// Python: state property (runtime.py:279)
func (b *cliRuntimeBase) State() harnessprotocol.HarnessState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.phase
}

// SessionID 返回 nil：CLI 子进程拥有自己的会话，不是团队会话。
// Python session_id 恒 None（runtime.py:284-286），Go 以 nil 承载。
// Python: session_id property (runtime.py:284)
func (b *cliRuntimeBase) SessionID() *string { return nil }

// Outputs 返回本运行周期叙述块的通道（stop 时关闭）。
// Python: outputs (runtime.py:293)
func (b *cliRuntimeBase) Outputs() <-chan stream.OutputSchema {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.outputCh
}

// Send 提交输入，把任何待告知团队状态折叠进同一消息。
// 外部 CLI 没有 rail 也没有可达 context，所以团队状态必须随用户消息传播。
// 每条协调路径最终都会在这里发消息，所以出站检查一次就够——无需轮询。
// 状态放在消息最前面，置于触发它的内容之前；基线只在投递成功后推进。
// 返回 nil（CLI runtime 没有可呈现的单调序列 id）。
//
// Python: send (runtime.py:297)
func (b *cliRuntimeBase) Send(content any, immediate bool) (any, error) {
	// Python: text = content if isinstance(content, str) else str(content)（:305）
	text, ok := content.(string)
	if !ok {
		text = fmt.Sprint(content)
	}
	// 入站消息只带绝对发送时间；时钟锚告诉成员"现在"是什么，如同
	// TeamPolicyRail 对进程内成员的做法。
	clock := inboundtags.RenderClock(inboundtags.FormatAbsoluteTime(database.GetCurrentTime()))
	if text != "" {
		text = clock + "\n" + text
	} else {
		text = clock
	}
	// 任务通知折叠（F_119）：评审通过等条目折进下一条消息。
	var notice *tracker.ClaimedNotice
	b.mu.Lock()
	outcomeTracker := b.taskOutcomeTracker
	sess := b.memberSession
	b.mu.Unlock()
	if outcomeTracker != nil && sess != nil {
		notice = outcomeTracker.Claim(sess)
	}
	if notice != nil {
		if text != "" {
			text = notice.Text + "\n\n" + text
		} else {
			text = notice.Text
		}
	}
	pending, _ := b.pendingTeamContext()
	if pending != "" {
		if text != "" {
			text = pending + "\n\n" + text
		} else {
			text = pending
		}
	}
	if _, err := b.sendRaw(text, immediate); err != nil {
		if notice != nil && outcomeTracker != nil {
			outcomeTracker.Release(notice)
		}
		return nil, err
	}
	if pending != "" {
		b.commitTeamContext()
	}
	if notice != nil && outcomeTracker != nil && sess != nil {
		outcomeTracker.Persist(sess)
	}
	return nil, nil
}

// AnnounceTeamContext 把待告知团队状态作为独立消息投递。
// Send 的搭车只在本就要发消息时到达成员，名册变化可能滞后整整一轮；成员
// 生命周期事件调用它让宣告独立发出。无新内容时 no-op，调用方可在每个事件上
// 直接触发。
//
// Python: announce_team_context (runtime.py:334)
func (b *cliRuntimeBase) AnnounceTeamContext() error {
	pending, _ := b.pendingTeamContext()
	if pending == "" {
		return nil
	}
	if _, err := b.sendRaw(pending, false); err != nil {
		return err
	}
	b.commitTeamContext()
	return nil
}

// AnnounceTaskRevisions 把待发的任务编辑通知作为独立消息投递（F_129）。
// TASK_UPDATED 事件的唯一职责：让 tracker 现在说话而不是等下一次 send。
// 它像编辑本身一样 steer 忙碌的成员。tracker 无新内容时 no-op。
//
// Python: announce_task_revisions (runtime.py:349)
func (b *cliRuntimeBase) AnnounceTaskRevisions() error {
	b.mu.Lock()
	outcomeTracker := b.taskOutcomeTracker
	sess := b.memberSession
	b.mu.Unlock()
	if outcomeTracker == nil {
		return nil
	}
	_, err := outcomeTracker.Announce(sess, func(text string) error {
		_, err := b.sendRaw(text, true)
		return err
	})
	return err
}

// pendingTeamContext 渲染本成员尚未被告知的团队状态。
// Python: _pending_team_context (runtime.py:362)
func (b *cliRuntimeBase) pendingTeamContext() (string, bool) {
	b.mu.Lock()
	tr := b.teamContextTracker
	sess := b.memberSession
	b.mu.Unlock()
	if tr == nil || sess == nil {
		return "", false
	}
	return tr.PendingText(sess)
}

// commitTeamContext 成功发送后推进投递基线。
// Python: _commit_team_context (runtime.py:368)
func (b *cliRuntimeBase) commitTeamContext() {
	b.mu.Lock()
	tr := b.teamContextTracker
	sess := b.memberSession
	b.mu.Unlock()
	if tr == nil || sess == nil {
		return
	}
	tr.Commit(sess, nil)
}

// sendRaw 用一条消息驱动 CLI：开新轮、steer 或后续重起。
// 唯一投递路径——Send 与 AnnounceTeamContext 都汇入它，因此恰有一个触达
// CLI 的位置、每条消息恰一次团队上下文检查：
//   - IDLE：开新轮驱动 text（迁移 RUNNING，发 started）；轮次在后台
//     goroutine 运行所以本调用不阻塞，镜像 NativeHarness 监督器。
//   - RUNNING + immediate：steer 在飞轮次（重起形态则缓冲，无法 steer）。
//   - RUNNING + 非 immediate：缓冲为轮次结束后处理的 follow-up。
//
// Python: _send_raw (runtime.py:374)
func (b *cliRuntimeBase) sendRaw(text string, immediate bool) (any, error) {
	b.mu.Lock()
	switch b.phase {
	case harnessprotocol.HarnessStateTerminated:
		b.mu.Unlock()
		return nil, exception.RaiseError(exception.StatusAgentTeamExecutionError,
			exception.WithMsg(fmt.Sprintf("external CLI member '%s' runtime already stopped.", b.memberName)))
	case harnessprotocol.HarnessStateRunning:
		b.mu.Unlock()
		if immediate {
			return nil, b.steerFn(text)
		}
		return nil, b.followUpFn(text)
	}
	roundID := b.nextRoundIDLocked()
	b.turnAborted = false
	b.transitionLocked(harnessprotocol.HarnessStateRunning)
	onRound := append([]roundCallback(nil), b.onRound...)
	b.mu.Unlock()
	b.emitRound(onRound, "started", roundID, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	b.mu.Lock()
	b.turnCancel = cancel
	b.turnDone = done
	b.mu.Unlock()
	go func() {
		defer close(done)
		b.driveTurn(ctx, text, roundID)
	}()
	return nil, nil
}

// Abort 停止在飞轮次。immediate 回滚退化为 no-op。
// 子进程轮次没有可回滚的轮中快照，所以优雅与立即 abort 都只是给轮次驱动器
// 发停止信号；轮次结束、驱动器发 aborted 轮次事件。immediate 标记在此只记录
// 意图。
//
// Python: abort (runtime.py:410)
func (b *cliRuntimeBase) Abort(immediate bool) {
	_ = immediate
	b.mu.Lock()
	if b.phase != harnessprotocol.HarnessStateRunning {
		b.mu.Unlock()
		return
	}
	b.turnAborted = true
	cancel := b.turnCancel
	b.mu.Unlock()
	b.abortTurnFn()
	if cancel != nil {
		cancel()
	}
}

// Pause no-op：CLI 子进程轮次无法暂停后干净重启。
// Python: pause (runtime.py:424)
func (b *cliRuntimeBase) Pause() {
	logger.Debug(logComponent).Str("member", b.memberName).Msg("pause is a no-op for a CLI-backed runtime")
}

// Resume 恢复 CLI 表面；冷恢复 query 成为新输入。
// CLI runtime 不保留暂停的在飞轮次，所以热恢复是 no-op。生命周期层可在重建
// runtime 后提供冷恢复 query；经正常队列投递该 query。
// Python 判 `query is not None`（runtime.py:436）；nil 即不投递，其余值
// （含空串，同 Python 空串一样是合法输入）经正常队列投递（附时钟锚）。
// Python: resume (runtime.py:429)
func (b *cliRuntimeBase) Resume(query any) error {
	b.mu.Lock()
	terminated := b.phase == harnessprotocol.HarnessStateTerminated
	b.mu.Unlock()
	if !terminated {
		if query == nil {
			return nil
		}
		_, err := b.Send(query, false)
		return err
	}
	return nil
}

// Subscribe 注册可选相位/轮次回调（NativeHarness 同款契约）。
// 只注册非 nil 回调。on_state 接收 old/new/session_id；on_round 接收
// kind/round_id。参数用字面 func 签名承载（agent.MemberRuntime 接口面），
// 内部落库时转回 stateCallback/roundCallback 命名类型。
// Python: subscribe (runtime.py:440)
func (b *cliRuntimeBase) Subscribe(
	onState func(old, new harnessprotocol.HarnessState, sessionID string),
	onRound func(kind string, roundID int, turnID string, result *harnessprotocol.TurnResult),
) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if onState != nil {
		b.onState = append(b.onState, onState)
	}
	if onRound != nil {
		b.onRound = append(b.onRound, onRound)
	}
}

// driveTurn 经 drive 驱动一轮，然后发它的终局轮次事件。
// 把每个叙述块泵入输出通道，再把轮次结果映射到轮次事件并回到 IDLE——除非
// stop 已把相位移到 TERMINATED，那种情况下收尾事件被抑制。
//
// Finalizer——覆盖正常结束、显式 abort/stop、意外取消与崩溃退出，保证每轮
// 至多一个终局轮次事件、至多一次 finalized 失败：
//   - 正常结束且无 finalized 失败 → finished。
//   - 显式 abort/stop（turnAborted 置位）→ aborted；不写 failed 消息。
//   - 无 abort 标记的意外取消是 runtime 任务异常退出：finalize sdk_error
//     失败并把成员翻到 ERROR（经 reliabilityCtx）。
//   - 其他异常：SDK runtime 已 finalize 失败则复用（不发第二条）；否则
//     finalize sdk_error 回退。轮内异常不置 ERROR——成员经正常轮次失败语义
//     回到 READY。
//
// Python: _drive_turn (runtime.py:462)
func (b *cliRuntimeBase) driveTurn(ctx context.Context, content string, roundID int) {
	b.mu.Lock()
	b.currentRoundID = roundID
	q := b.outputQueue
	b.mu.Unlock()
	finalizedByTurn := false
	abnormalExit := false
	func() {
		defer func() {
			b.mu.Lock()
			b.turnCancel = nil
			b.mu.Unlock()
		}()
		chunks, cancel := b.driveFn(ctx, map[string]any{"query": content})
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				b.mu.Lock()
				aborted := b.turnAborted
				b.mu.Unlock()
				if !aborted {
					abnormalExit = true
				}
				return
			case chunk, ok := <-chunks:
				if !ok {
					// SDK runtime 可能 finalize 一个失败然后干净地结束生成器；
					// 那是轮内失败，不是正常结束。
					b.mu.Lock()
					finalizedByTurn = b.reliabilityCtx != nil && b.hasFinalized()
					b.mu.Unlock()
					return
				}
				if q != nil {
					q.Push(chunk)
				}
			}
		}
	}()
	// flavor 轮内错误（注入失败 / premature-EOF / spawn 失败）：Python 经
	// generator 异常落到 _drive_turn 的 except 分支；Go 在此读取 drive 字段
	// 并归入 crashed 分类（D-15.13-b 同源偏差）。
	b.mu.Lock()
	driveErr := b.driveErr
	b.driveErr = nil
	b.mu.Unlock()
	if driveErr != nil {
		// Python: team_logger.exception("[{}] external cli turn crashed", ...)
		logger.Error(logComponent).Str("member", b.memberName).Err(driveErr).
			Msg("external cli turn crashed")
		// Python: crashed 分支的 await self._finalize_crash_exit(exc, round_id)
		b.finalizeCrashExit(driveErr, roundID)
	}
	// Python: CancelledError 分支的 await self._finalize_abnormal_exit(round_id)
	if abnormalExit {
		b.finalizeAbnormalExit(roundID)
	}
	b.mu.Lock()
	terminated := b.phase == harnessprotocol.HarnessStateTerminated
	aborted := b.turnAborted
	transitionIdle := b.phase == harnessprotocol.HarnessStateRunning
	onRound := append([]roundCallback(nil), b.onRound...)
	b.mu.Unlock()
	if terminated {
		return
	}
	switch {
	case aborted:
		b.emitRound(onRound, "aborted", roundID, "", nil)
	case finalizedByTurn || abnormalExit || driveErr != nil:
		b.emitRound(onRound, "failed", roundID, "", nil)
	default:
		b.emitRound(onRound, "finished", roundID, "", nil)
	}
	if transitionIdle {
		b.transition(harnessprotocol.HarnessStateIdle)
	}
}

// hasFinalized 检查 reliabilityCtx（SDK runtime 注入）是否已 finalize 失败。
// 基础 CLI adapter runtime 的 reliabilityCtx 恒 nil（false）。
func (b *cliRuntimeBase) hasFinalized() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reliabilityCtx != nil && b.reliabilityCtx.HasFinalized()
}

// reliabilityClient 取 reliabilityCtx；未注入时 nil。
func (b *cliRuntimeBase) reliabilityClient() *RuntimeReliabilityContext {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reliabilityCtx
}

// finalizeAbnormalExit 为意外任务退出 finalize 一个 sdk_error 失败。
// 只有意外取消路径（runtime 任务异常退出）把成员置 ERROR；轮内失败不置。
//
// Python: _finalize_abnormal_exit (runtime.py:522)
func (b *cliRuntimeBase) finalizeAbnormalExit(roundID int) {
	_ = roundID
	ctx := b.reliabilityClient()
	if ctx == nil {
		// 对齐 Python `if ctx is None: return`（本批 CLI adapter runtime 恒此分支）
		return
	}
	if !ctx.HasFinalized() {
		summary := b.memberName + " external runtime task exited unexpectedly"
		reason := schema.NewExternalRuntimeFailureReason(
			"external runtime task exited unexpectedly", "CancelledError", "", nil)
		_ = ctx.FinalizeFailure(schema.ExternalRuntimeFailureCategorySDKError, reason, summary)
	}
	_ = ctx.MarkMemberError()
}

// finalizeCrashExit 为未映射崩溃 finalize 回退失败（若未做过）。
// SDK runtime 预期自行分类并 finalize 结构化失败；这是 CliRuntimeBase 兜底，
// 覆盖未映射异常。轮内崩溃不置 ERROR——成员回到 READY。
//
// Python: _finalize_crash_exit (runtime.py:546)
func (b *cliRuntimeBase) finalizeCrashExit(err error, roundID int) {
	_ = roundID
	ctx := b.reliabilityClient()
	if ctx == nil || ctx.HasFinalized() {
		return
	}
	// Python: sdk_error_type=type(exc).__name__；Go 以反射类型名承载同一信息
	excType := fmt.Sprintf("%T", err)
	message := err.Error()
	if message == "" {
		message = excType
	}
	summary := fmt.Sprintf("%s external runtime turn crashed: %s", b.memberName, excType)
	reason := schema.NewExternalRuntimeFailureReason(message, excType, "", nil)
	_ = ctx.FinalizeFailure(schema.ExternalRuntimeFailureCategorySDKError, reason, summary)
}

// nextRoundIDLocked 返回本 runtime 实例的单调轮次 id（须持锁）。
// Python: _next_round_id (runtime.py:571)
func (b *cliRuntimeBase) nextRoundIDLocked() int {
	b.roundSeq++
	return b.roundSeq
}

// transition 更新相位并触发 harness.state（不变时 no-op）。
// Python: _transition (runtime.py:576)
func (b *cliRuntimeBase) transition(newPhase harnessprotocol.HarnessState) {
	b.mu.Lock()
	old := b.phase
	if old == newPhase {
		b.mu.Unlock()
		return
	}
	b.phase = newPhase
	onState := append([]stateCallback(nil), b.onState...)
	b.mu.Unlock()
	for _, cb := range onState {
		// Python 回调收到 session_id（base 恒 None）；Go 回调签名为 string，
		// nil 以空串承载（同旧基线行为）。
		sid := b.SessionID()
		if sid != nil {
			cb(old, newPhase, *sid)
		} else {
			cb(old, newPhase, "")
		}
	}
}

// transitionLocked 持锁版本（不发回调，回调由调用方在解锁后处理）。
func (b *cliRuntimeBase) transitionLocked(newPhase harnessprotocol.HarnessState) {
	if b.phase == newPhase {
		return
	}
	b.phase = newPhase
}

// emitRound 为一次轮次生命周期迁移触发 harness.round。
// Python: _emit_round (runtime.py:589)
func (b *cliRuntimeBase) emitRound(onRound []roundCallback, kind string, roundID int, turnID string, result *harnessprotocol.TurnResult) {
	for _, cb := range onRound {
		cb(kind, roundID, turnID, result)
	}
}

// makeChunk 把叙述摘要包装成流输出块（text content）。
// Python: _make_chunk (runtime.py:602)
func (b *cliRuntimeBase) makeChunk(text string, index int) stream.OutputSchema {
	return stream.OutputSchema{
		Type:    "llm_output",
		Index:   index,
		Payload: map[string]any{"content": text, "result_type": "answer"},
	}
}

// ──────────────────────────── 成员 runtime no-op 钩子 ────────────────────────────
// （外部 CLI 成员一概没有——Python runtime.py:623-678 的 no-op hooks）

// InitCwdForRound no-op：子进程拥有自己的工作目录。
func (b *cliRuntimeBase) InitCwdForRound() {}

// HasPendingInterrupt 外部 CLI 成员没有中断恢复概念。
func (b *cliRuntimeBase) HasPendingInterrupt() bool { return false }

// IsPendingInterruptResumeValid 外部 CLI 成员没有中断恢复概念。
func (b *cliRuntimeBase) IsPendingInterruptResumeValid(userInput any) bool {
	_ = userInput
	return false
}

// RegisterRail no-op：CLI runtime 没有 rail 栈。
func (b *cliRuntimeBase) RegisterRail(rail any) error {
	_ = rail
	return nil
}

// FindRails CLI-backed runtime 没有 rail；恒空列表。
// Python: find_rails (runtime.py:634)
func (b *cliRuntimeBase) FindRails(railType any) []any {
	_ = railType
	return []any{}
}

// UnregisterRail no-op：CLI runtime 没有 rail 栈。
func (b *cliRuntimeBase) UnregisterRail(rail any) error {
	_ = rail
	return nil
}

// RegisterMemberTools no-op：外部 CLI 成员不使用团队记忆工具包。
func (b *cliRuntimeBase) RegisterMemberTools(memoryManager any) { _ = memoryManager }

// InjectMemberMemory no-op：外部 CLI 成员不使用团队记忆注入。
func (b *cliRuntimeBase) InjectMemberMemory(memoryManager any, query string) {
	_ = memoryManager
	_ = query
}

// SetBackgroundTaskController no-op：外部 CLI 成员不启动需要控制的后台工具。
func (b *cliRuntimeBase) SetBackgroundTaskController(controller any) { _ = controller }

// BackgroundTaskController 恒 nil：外部 CLI 成员自己不持有后台控制器。
// 它们的 swarmflow 工作经 ExternalSwarmflowHost 进行（⤵️ swarmflow 章节）。
// Python: background_task_controller property (runtime.py:658)
func (b *cliRuntimeBase) BackgroundTaskController() any { return nil }

// Workspace 外部 CLI runtime 不暴露团队工作空间句柄。
func (b *cliRuntimeBase) Workspace() any { return nil }

// SysOperation 外部 CLI runtime 不暴露 sys_operation 句柄。
func (b *cliRuntimeBase) SysOperation() any { return nil }

// ──────────────────────────── ExternalCliRuntime ────────────────────────────

// ExternalCliRuntime 流式 runtime：一个经 stdin 驱动的长驻 CLI 子进程。
//
// Python: ExternalCliRuntime (runtime.py:681)
type ExternalCliRuntime struct {
	cliRuntimeBase
	// adapter 启动与对话知识
	adapter *CliAgentAdapter
	// injector stdin 注入器
	injector *StdinPipeInjector
	// outputLines stdout 行读取器（spawn 包装成行流）
	outputLines transport.StreamReaderLike
	// process 被驱动的子进程（可 nil）
	process transport.ProcessLike
	// transport 启动进程的 transport（可 nil）
	transport transport.ProcessTransport
	// abortRequested abort 信号（下一输出行生效，进程存活）
	abortRequested bool
	// stderrTail 后台 stderr 排空的结果尾
	stderrOnce sync.Once
	stderrTail string
}

// NewExternalCliRuntime 绑定到已启动 CLI 子进程的输入/输出通道。
// Python: ExternalCliRuntime.__init__ (runtime.py:684)
func NewExternalCliRuntime(
	memberName string, adapter *CliAgentAdapter, injector *StdinPipeInjector,
	outputLines transport.StreamReaderLike, process transport.ProcessLike,
	tr transport.ProcessTransport, memberAgentID string,
) *ExternalCliRuntime {
	r := &ExternalCliRuntime{
		cliRuntimeBase: cliRuntimeBase{memberName: memberName, memberAgentID: memberAgentID, phase: harnessprotocol.HarnessStateIdle},
		adapter:        adapter,
		injector:       injector,
		outputLines:    outputLines,
		process:        process,
		transport:      tr,
	}
	r.cliRuntimeBase.driveFn = r.drive
	r.cliRuntimeBase.steerFn = r.Steer
	r.cliRuntimeBase.followUpFn = r.FollowUp
	r.cliRuntimeBase.abortTurnFn = r.abortTurn
	r.cliRuntimeBase.acloseFn = r.aclose
	return r
}

// ensureStderrDrain 对长驻进程 stderr 只排空一次。
// 未读的 stderr 管道会填满并阻塞 CLI 的下一次 stderr 写（卡住的写看起来像
// 挂起），所以进程生命周期内后台排空；尾部在 aclose 时呈现。
// Python: _ensure_stderr_drain (runtime.py:710)
func (r *ExternalCliRuntime) ensureStderrDrain() {
	if r.process == nil || r.process.Stderr() == nil {
		return
	}
	r.stderrOnce.Do(func() {
		go func() {
			tail, _ := readStderrTail(r.process.Stderr())
			r.mu.Lock()
			r.stderrTail = tail
			r.mu.Unlock()
		}()
	})
}

// drive 实现：经 stdin 投递 prompt，读 stdout 行直到完成判定或 abort。
// stdout EOF 且无完成哨兵、无 abort：长驻流式 CLI 的子进程在轮中死亡
// （auth/配额/额度耗尽/崩溃）。在此返回会让崩溃轮看起来像成功的空轮，把失败
// 藏到 aclose——立即以原因呈现。
// Python: ExternalCliRuntime._drive (runtime.py:720)
func (r *ExternalCliRuntime) drive(ctx context.Context, inputs map[string]any) (<-chan stream.OutputSchema, context.CancelFunc) {
	ch := make(chan stream.OutputSchema)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(ch)
		r.ensureStderrDrain()
		query, _ := inputs["query"].(string)
		r.mu.Lock()
		r.abortRequested = false
		r.mu.Unlock()
		if err := r.injector.Write(r.adapter.FormatInput(query)); err != nil {
			// Python: await self._injector.write(...) 抛出经 generator 传播到
			// _drive_turn 的 failed 分类；Go 落 driveErr 后关闭通道。
			r.setDriveErr(err)
			return
		}
		chunkIndex := 0
		for {
			select {
			case <-ctx.Done():
				logger.Debug(logComponent).Str("member", r.memberName).Msg("external cli turn aborted")
				return
			default:
			}
			line, err := r.outputLines.ReadLine()
			if err != nil {
				// stdout EOF：premature-EOF 判定
				if perr := r.raiseOnPrematureEOF(); perr != nil {
					r.setDriveErr(perr)
				}
				return
			}
			r.mu.Lock()
			aborted := r.abortRequested
			r.mu.Unlock()
			if aborted {
				logger.Debug(logComponent).Str("member", r.memberName).Msg("external cli turn aborted")
				return
			}
			summary := r.adapter.SummarizeOutputLine(strings.TrimRight(string(line), "\n"))
			if summary != "" {
				// Python: team_logger.debug("[{}] {}", member, summary)（runtime.py:730）
				logger.Debug(logComponent).Str("member", r.memberName).Str("summary", summary).
					Msg("external cli output")
				ch <- r.makeChunk(summary, chunkIndex)
				chunkIndex++
			}
			if r.adapter.IsTurnComplete(string(line)) {
				return
			}
		}
	}()
	return ch, cancel
}

// raiseOnPrematureEOF 当 stdout EOF 反映子进程死亡时报结构化错误。
// 区分进程仍在运行（无句柄或退出码未定——输出迭代器提前结束的退化 adapter）
// 与已崩溃（非干净退出码）。只有后者是错误，且携带排空的 stderr 尾以免失败
// 原因丢失。错误经返回值交给 drive 落 driveErr（Python 是 raise_error 上抛）。
// Python: _raise_on_premature_eof (runtime.py:745)
func (r *ExternalCliRuntime) raiseOnPrematureEOF() error {
	if r.process == nil {
		return nil
	}
	returncode := r.process.ReturnCode()
	if returncode == 0 || returncode == -1 {
		// 无子进程可查、干净退出、或仍在运行：把耗尽的迭代器当良性轮次边界
		// 而不是崩溃。
		return nil
	}
	stderrTail := r.peekStderrTail()
	detail := ""
	if stderrTail != "" {
		detail = "; stderr: " + stderrTail
	}
	return exception.RaiseError(exception.StatusAgentTeamExecutionError,
		exception.WithMsg(fmt.Sprintf(
			"external CLI member '%s' exited with code %d mid-turn (likely auth/quota/credit exhaustion or a crash)%s",
			r.memberName, returncode, detail)))
}

// setDriveErr 记录 flavor 轮内错误（drive goroutine 单写、driveTurn 排空后单读）。
func (b *cliRuntimeBase) setDriveErr(err error) {
	b.mu.Lock()
	b.driveErr = err
	b.mu.Unlock()
}

// hasDriveErr 查询轮内错误是否已落（Reinvoke 重起循环的停止信号）。
func (b *cliRuntimeBase) hasDriveErr() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.driveErr != nil
}

// peekStderrTail 不永久阻塞地读后台 stderr 排空的尾。
// 排空在 stderr EOF 结束；死进程立即到达 EOF，所以通常立即返回。有界等待
// 防护崩溃后 stderr 管道半开的边缘情况。
// Python: _peek_stderr_tail (runtime.py:768)
func (r *ExternalCliRuntime) peekStderrTail() string {
	deadline := time.Now().Add(stderrPeekTimeoutS * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		tail := r.stderrTail
		r.mu.Unlock()
		if tail != "" {
			return tail
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

// Steer 向运行中的 CLI 注入内容（轮中 steer）。
// Python: steer (runtime.py:781)
func (r *ExternalCliRuntime) Steer(content string) error {
	return r.injector.Write(r.adapter.FormatInput(content))
}

// FollowUp 注入让 CLI 在当前轮之后处理的内容。
// Python: follow_up (runtime.py:785)
func (r *ExternalCliRuntime) FollowUp(content string) error {
	return r.injector.Write(r.adapter.FormatInput(content))
}

// abortTurn 在下一输出行停止在飞轮次（进程存活）。
// Python: _abort_turn (runtime.py:789)
func (r *ExternalCliRuntime) abortTurn() {
	r.mu.Lock()
	r.abortRequested = true
	r.mu.Unlock()
}

// aclose 关闭 stdin 并终止长驻子进程。幂等。
// 非干净退出码且有 stderr 尾时发 Warn（对齐 Python aclose 的告警分支）。
// Python: aclose (runtime.py:793)
func (r *ExternalCliRuntime) aclose() {
	if r.injector != nil {
		_ = r.injector.Close()
	}
	terminateProcess(r.process)
	r.mu.Lock()
	stderrTail := r.stderrTail
	r.mu.Unlock()
	if r.process != nil {
		_, _ = r.process.Wait()
		returncode := r.process.ReturnCode()
		if returncode != 0 && returncode != -1 && stderrTail != "" {
			logger.Warn(logComponent).
				Str("member", r.memberName).
				Int("returncode", returncode).
				Str("stderr", stderrTail).
				Msg("[external-cli] member CLI exited with code (likely auth/quota/credit exhaustion or a crash)")
		}
	}
	if r.transport != nil {
		_ = r.transport.Aclose()
	}
}

// ──────────────────────────── ReinvokeCliRuntime ────────────────────────────

// ReinvokeCliRuntime one-shot runtime：每轮一个全新 CLI 子进程（prompt 走 argv）。
// 轮中到达的消息（steer/followUp）无法打断 one-shot 进程；它们被缓冲并在同
// 一轮内作为后续重起排空后才返回，不丢任何东西。轮次在子进程 stdout EOF /
// 退出时完成。
//
// Python: ReinvokeCliRuntime (runtime.py:819)
type ReinvokeCliRuntime struct {
	cliRuntimeBase
	// adapter 启动与对话知识
	adapter *CliAgentAdapter
	// env 每次重起子进程的完整环境
	env map[string]string
	// cwd 子进程工作目录
	cwd string
	// cliSessionID 跨轮稳定会话标识（CLI 按 id resume 时需要真 UUID）
	cliSessionID string
	// inactivityTimeoutS 无输出上限；stdout 或 stderr 的每个字节都重置
	// deadline，只有子进程静默这么久才终止。
	inactivityTimeoutS float64
	// turnTimeoutS 可选绝对 wall-clock 安全网。空表示无绝对上限（无活动上限
	// 是主守卫）。设置为约束靠在无活动窗口之下滴输出而永远"活跃"的病态 CLI。
	turnTimeoutS   float64
	hasTurnTimeout bool
	// launchExtraArgs 每次重起在 prompt 前追加的额外启动参数
	// （如 MCP server 注册——one-shot CLI 每个新进程都需要它们）。
	launchExtraArgs []string
	// firstTurn 是否首轮（控制 session_flag/resume_flag/continue_args）
	firstTurn bool
	// pending 缓冲的轮中消息（_FOLLOWUP_SEP 连接为一条后续 prompt）
	pending []string
	// aborted abort 标记；置位后 drive 不再发起下一次重起
	aborted bool
	// current 在飞子进程（可 nil）
	current transport.ProcessLike
	// transport 每轮启动用 transport
	transport transport.ProcessTransport
}

// NewReinvokeCliRuntime 持有启动配置；子进程按轮创建。
// Python: ReinvokeCliRuntime.__init__ (runtime.py:828)
func NewReinvokeCliRuntime(
	memberName string, adapter *CliAgentAdapter, env map[string]string, cwd string,
	cliSessionID string, launchExtraArgs []string, memberAgentID string,
) *ReinvokeCliRuntime {
	if cliSessionID == "" {
		// Python: cli_session_id or uuid.uuid4().hex（runtime.py:867）——无连字符
		cliSessionID = strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	r := &ReinvokeCliRuntime{
		cliRuntimeBase:     cliRuntimeBase{memberName: memberName, memberAgentID: memberAgentID, phase: harnessprotocol.HarnessStateIdle},
		adapter:            adapter,
		env:                env,
		cwd:                cwd,
		cliSessionID:       cliSessionID,
		inactivityTimeoutS: defaultInactivityTimeoutS,
		launchExtraArgs:    launchExtraArgs,
		firstTurn:          true,
		transport:          &transport.LocalTransport{},
	}
	r.cliRuntimeBase.driveFn = r.drive
	r.cliRuntimeBase.steerFn = r.Steer
	r.cliRuntimeBase.followUpFn = r.FollowUp
	r.cliRuntimeBase.abortTurnFn = r.abortTurn
	r.cliRuntimeBase.acloseFn = r.aclose
	return r
}

// SetTurnTimeoutS 设置可选绝对 wall-clock 安全网（Python turn_timeout_s 参数）。
func (r *ReinvokeCliRuntime) SetTurnTimeoutS(seconds float64) {
	r.turnTimeoutS = seconds
	r.hasTurnTimeout = true
}

// SetTransport 注入每轮启动用 transport（测试探针用；Python 无对应——
// create_subprocess_exec 直启属 D-15.13-a 差异缝，Go 以本字段承载）。
func (r *ReinvokeCliRuntime) SetTransport(t transport.ProcessTransport) {
	r.transport = t
}

// SetInactivityTimeoutS 覆盖默认无输出上限（Python inactivity_timeout_s 参数）。
func (r *ReinvokeCliRuntime) SetInactivityTimeoutS(seconds float64) {
	r.inactivityTimeoutS = seconds
}

// drive 实现：循环重起子进程直到 abort 或无缓冲后续。
// abort 可能在首次重起前落地：不开子进程直接返回，使 abort 永远抢在轮次前。
// Python: ReinvokeCliRuntime._drive (runtime.py:879)
func (r *ReinvokeCliRuntime) drive(ctx context.Context, inputs map[string]any) (<-chan stream.OutputSchema, context.CancelFunc) {
	ch := make(chan stream.OutputSchema)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(ch)
		query, _ := inputs["query"].(string)
		r.mu.Lock()
		r.aborted = false
		r.mu.Unlock()
		chunkIndex := 0
		prompt := query
		// Python: while prompt is not None and not self._aborted（runtime.py:882）
		// ——None 哨兵结束循环；空串是合法 prompt 照常重起。Go 以 drainPending
		// 的 ok 位承载 None 语义（D-15.13-b 同源偏差）。
		for !r.isAborted() {
			// 单轮 stdout 排空与 stderr 排空、无活动 watchdog 并发运行
			//（见 runOnce），不能直接产出。它把叙述摘要推到本通道；
			// runOnce 在任何退出路径（完成/超时/abort/启动错误）上以关闭通道
			// 收尾。watchdog 保证关闭总会到达。
			turnCh, _ := r.runOnce(ctx, prompt)
			forwardedCleanly := false
			func() {
				defer func() {
					if !forwardedCleanly {
						// 消费者取消 / 生成器轮中关闭：停子进程使轮次任务
						// 及时收尾而不是等 watchdog。
						r.abortTurn()
					}
				}()
				for summary := range turnCh {
					ch <- r.makeChunk(summary, chunkIndex)
					chunkIndex++
				}
				forwardedCleanly = true
			}()
			// Python: await turn_task ——spawn/run 错误经 turn task 传播出
			// 生成器（runtime.py:905-907）；Go 落 driveErr 后立即停止重起循环。
			if r.isAborted() || r.hasDriveErr() {
				return
			}
			next, ok := r.drainPending()
			if !ok {
				return
			}
			prompt = next
		}
	}()
	return ch, cancel
}

// runOnce 启动一次子进程并消费其输出。
// Python: _run_once (runtime.py:920)
func (r *ReinvokeCliRuntime) runOnce(ctx context.Context, prompt string) (<-chan string, context.CancelFunc) {
	out := make(chan string)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer close(out)
		argv := r.adapter.BuildTurnCommand(prompt, r.cliSessionID, r.firstTurn, r.launchExtraArgs)
		r.firstTurn = false
		logger.Info(logComponent).Strs("argv", argv).Str("member", r.memberName).
			Msg("[external-cli] re-invoke for member")
		proc, err := r.transport.Run(argv, r.env, r.cwd)
		if err != nil {
			// Python: spawn 异常经 _run_once → _drive 的 await turn_task 传播出
			// 生成器，落 _drive_turn 的 except 分支（failed 事件）；Go 落
			// driveErr（driveTurn 归入 crashed 分类）并停止重起循环。
			r.setDriveErr(err)
			return
		}
		r.mu.Lock()
		r.current = proc
		r.mu.Unlock()
		defer func() {
			r.mu.Lock()
			r.current = nil
			r.mu.Unlock()
		}()
		r.consumeTurn(ctx, cancel, proc, out)
	}()
	return out, cancel
}

// consumeTurn 在无活动守卫下把 stdout+stderr 排空到 EOF，呈现失败。
// 输出（stdout 或 stderr）重置共享无活动 deadline；子进程静默
// inactivityTimeoutS 后轮次才被终止（外加可选绝对 turnTimeoutS 安全网）。
// 这样做长但活跃工作的 CLI 永不被杀，而挂起/启动卡死的进程仍被终止。
//
// stderr 并发排空使 CLI 不会阻塞在满的 stderr 管道上；非零退出带 stderr 尾记
// 日志，使失败轮（auth/配额/额度耗尽/崩溃）可见而不是看起来成员什么都没做。
//
// Python: _consume_turn (runtime.py:948)
func (r *ReinvokeCliRuntime) consumeTurn(ctx context.Context, cancel context.CancelFunc, proc transport.ProcessLike, out chan<- string) {
	if proc.Stdin() != nil {
		_ = proc.Stdin().Close()
	}
	var activityMu sync.Mutex
	lastActivity := time.Now()
	bump := func() {
		activityMu.Lock()
		lastActivity = time.Now()
		activityMu.Unlock()
	}
	stderrTail := ""
	var tailMu sync.Mutex

	var wg sync.WaitGroup
	// stderr 排空
	if proc.Stderr() != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tail, _ := readStderrTailDrain(proc.Stderr(), bump)
			tailMu.Lock()
			stderrTail = tail
			tailMu.Unlock()
		}()
	}
	// stdout 排空
	drainDone := make(chan struct{})
	if proc.Stdout() != nil {
		go func() {
			defer close(drainDone)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				line, err := proc.Stdout().ReadLine()
				if err != nil || r.isAborted() {
					return
				}
				bump()
				summary := r.adapter.SummarizeOutputLine(strings.TrimRight(string(line), "\n"))
				if summary != "" {
					logger.Debug(logComponent).Str("member", r.memberName).Str("summary", summary).Msg("external cli output")
					out <- summary
				}
			}
		}()
	} else {
		close(drainDone)
	}
	// watchdog：无活动（或绝对超时）即终止静默/超预算子进程
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		start := time.Now()
		for {
			activityMu.Lock()
			idleFor := time.Since(lastActivity)
			activityMu.Unlock()
			idleRemaining := r.inactivityTimeoutS - idleFor.Seconds()
			if idleRemaining <= 0 {
				logger.Warn(logComponent).Str("member", r.memberName).
					Msg("[external-cli] member turn inactivity timeout; terminating subprocess")
				terminateProcess(proc)
				return
			}
			wait := time.Duration(idleRemaining * float64(time.Second))
			if r.hasTurnTimeout {
				absRemaining := r.turnTimeoutS - time.Since(start).Seconds()
				if absRemaining <= 0 {
					logger.Warn(logComponent).Str("member", r.memberName).
						Msg("[external-cli] member turn absolute timeout; terminating subprocess")
					terminateProcess(proc)
					return
				}
				if d := time.Duration(absRemaining * float64(time.Second)); d < wait {
					wait = d
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
	select {
	case <-drainDone:
		// 排空先完成：进程退出时关闭了自己的管道。先撤销 watchdog
		//（Python finally: watchdog.cancel() 的等价物）再收尾。
		cancel()
		code, _ := proc.Wait()
		// 因 abort 而结束的排空意味着用户杀了子进程（信号导致的负退出码）；
		// 那不是 CLI 失败，不得当作失败报告。
		if !r.isAborted() {
			tailMu.Lock()
			t := stderrTail
			tailMu.Unlock()
			r.reportExit(code, t)
		}
	case <-ctx.Done():
		terminateProcess(proc)
		_, _ = proc.Wait()
	}
	<-watchdogDone
	wg.Wait()
}

// reportExit CLI 轮次非零退出时记 Warn 日志。
// Python: _report_exit (runtime.py:1051)
func (r *ReinvokeCliRuntime) reportExit(returncode int, stderrTail string) {
	if returncode == 0 || returncode == -1 {
		return
	}
	detail := ""
	if stderrTail != "" {
		detail = " stderr: " + stderrTail
	}
	logger.Warn(logComponent).
		Str("member", r.memberName).
		Int("returncode", returncode).
		Str("stderr_detail", detail).
		Msg("[external-cli] member CLI turn exited with code and did no team work (likely auth/quota/credit exhaustion or a crash)")
}

// drainPending 取出缓冲的轮中消息（多条以 _FOLLOWUP_SEP 连接）。
// ok=false 对应 Python None 哨兵（无缓冲消息，重起循环终止）。
// Python: _drain_pending (runtime.py:1064)
func (r *ReinvokeCliRuntime) drainPending() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 {
		return "", false
	}
	combined := strings.Join(r.pending, followupSep)
	r.pending = nil
	return combined, true
}

// Steer 缓冲内容作为后续重起（无轮中 steer）。
// Python: steer (runtime.py:1071)
func (r *ReinvokeCliRuntime) Steer(content string) error {
	r.mu.Lock()
	r.pending = append(r.pending, content)
	r.mu.Unlock()
	return nil
}

// FollowUp 缓冲内容作为后续重起。
// Python: follow_up (runtime.py:1075)
func (r *ReinvokeCliRuntime) FollowUp(content string) error {
	return r.Steer(content)
}

// abortTurn 抢占在飞轮次：杀当前子进程并停止。
// 置 abort 标记并立即终止运行中的子进程（如有），使高优先级 steer/abort 不必
// 等轮次自然结束。标记置位后 drive 循环不再发起下一次重起。无子进程时幂等
// 且安全。
// Python: _abort_turn (runtime.py:1079)
func (r *ReinvokeCliRuntime) abortTurn() {
	r.mu.Lock()
	r.aborted = true
	current := r.current
	r.mu.Unlock()
	terminateProcess(current)
}

// aclose 终止任何在飞子进程。幂等。
// Python: aclose (runtime.py:1091)
func (r *ReinvokeCliRuntime) aclose() {
	r.mu.Lock()
	current := r.current
	r.mu.Unlock()
	terminateProcess(current)
}

// isAborted 读 abort 标记。
func (r *ReinvokeCliRuntime) isAborted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.aborted
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readStderrTail 把子进程 stderr 排空到 EOF 并返回其尾。
// 排空有两个理由：未读的 stderr 管道会填满 OS 缓冲并阻塞 CLI 的下一次
// stderr 写——卡住的写与挂起无法区分——且尾部携带人类可读的失败原因
// （auth/配额/额度耗尽/崩溃），只消费 stdout 时会丢失。
// Python: _read_stderr_tail (runtime.py:106)
func readStderrTail(stream transport.StreamReaderLike) (string, error) {
	return readStderrTailDrain(stream, nil)
}

// readStderrTailDrain 同上，另以 bump 标记活动（无活动 watchdog 复位用）。
func readStderrTailDrain(stream transport.StreamReaderLike, bump func()) (string, error) {
	if stream == nil {
		return "", nil
	}
	var tail []byte
	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if bump != nil {
				bump()
			}
			tail = append(tail, buf[:n]...)
			if len(tail) > stderrTailLimit {
				tail = tail[len(tail)-stderrTailLimit:]
			}
		}
		if err != nil {
			break
		}
	}
	return strings.TrimSpace(string(tail)), nil
}

// terminateProcess 子进程仍在运行时终止它。幂等且安静。
// Python: _terminate (runtime.py:126)
func terminateProcess(p transport.ProcessLike) {
	if p == nil {
		return
	}
	if code := p.ReturnCode(); code != -1 {
		return
	}
	p.Terminate()
	_, _ = p.Wait()
}
