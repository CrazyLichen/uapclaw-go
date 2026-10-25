package cron

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SchedulerAgentClient 调度器所需的最小 AgentClient 接口。
// routing.AgentClient 隐式满足；测试 fake 零成本。
type SchedulerAgentClient interface {
	// SendRequest 发送非流式请求并等待完整响应
	SendRequest(ctx context.Context, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error)
}

// MessagePublisher 消息发布接口（破环关键：message_handler 隐式实现）。
// 对齐 Python: MessageHandler.publish_robot_messages。
type MessagePublisher interface {
	// PublishRobotMessages 发布机器人消息
	PublishRobotMessages(msg *schema.Message)
}

// CronSchedulerService 异步调度器：唤醒 agent 并将结果推送到频道。
// 对齐 Python: CronSchedulerService (scheduler.py L78-713)。
type CronSchedulerService struct {
	// store 任务存储
	store *CronJobStore
	// agentClient AgentServer 客户端（接口注入）
	agentClient SchedulerAgentClient
	// publisher 消息发布器（接口注入破环）
	publisher MessagePublisher
	// nowFn 时钟函数（可注入假时钟做确定性测试）
	// 对齐 Python: now_fn 参数
	nowFn func() float64
	// configLoader 读取 config 原始数据（channels 元数据路由用）
	configLoader func() map[string]any

	// running 是否运行中（atomic，对齐 Python self._running）
	running atomic.Bool
	// reloadCh 重载通知通道（对齐 Python _reload_event）
	reloadCh chan struct{}
	// cancel 主循环取消函数
	cancel context.CancelFunc
	// wg 等待 runAgent goroutine 退出
	wg sync.WaitGroup

	// storePollInterval store 文件 mtime 轮询间隔（实例字段对齐 Python self._store_poll_interval）
	storePollInterval float64

	// mu 保护 jobs/events/runs
	mu sync.Mutex
	// jobs 内存中任务快照：jobID → job
	jobs map[string]*CronJob
	// events 事件堆（按 at_ts+seq 排序）
	events eventHeap
	// seq 事件序号（同刻稳定序）
	seq int
	// runs 运行状态：runID → state
	runs map[string]*CronRunState
	// activeRuns 飞行中 agent goroutine：runID → 运行标记
	// 对齐 Python: _run_tasks (scheduler.py L102)——reload 重建事件堆后
	// 同一 runID 的 wake 事件可能重新入堆，飞行中任务须去重防双跑
	activeRuns map[string]struct{}
	// lastStoreMtime 上次 store 文件 mtime 快照
	lastStoreMtime float64
}

// CronRunState 单次运行内存状态（不持久化）。
// 对齐 Python: CronRunState (models.py L231-250)。
type CronRunState struct {
	// RunID 运行标识（{job_id}:{push_ts}）
	RunID string
	// JobID 任务标识
	JobID string
	// WakeAtISO 唤醒时间 ISO
	WakeAtISO string
	// PushAtISO 推送时间 ISO
	PushAtISO string
	// Status 状态 pending|running|succeeded|failed
	Status string
	// PlaceholderSent 占位符已发
	PlaceholderSent bool
	// PushedFinal 终稿已推
	PushedFinal bool
	// StartedAt 开始时间
	StartedAt float64
	// FinishedAt 结束时间
	FinishedAt float64
	// ResultText 结果文本
	ResultText string
	// Error 错误信息
	Error string
	// JobName 任务名快照
	JobName string
	// Targets 目标频道快照
	Targets string
	// SessionID 会话快照
	SessionID string
	// ChatType 群聊类型快照
	ChatType string
	// Timezone 时区快照
	Timezone string
}

// cronEvent 调度事件（对齐 Python _Event dataclass）。
type cronEvent struct {
	// AtTS 触发时间戳（秒）
	AtTS float64
	// Seq 序号（同刻稳定序）
	Seq int
	// Kind 事件类型 wake|push|push_update
	Kind string
	// JobID 任务标识
	JobID string
	// RunID 运行标识
	RunID string
}

// eventHeap 事件堆（手动实现，按 AtTS+Seq 排序）。
type eventHeap []*cronEvent

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// eventKindWake 唤醒事件
	eventKindWake = "wake"
	// eventKindPush 推送事件
	eventKindPush = "push"
	// eventKindPushUpdate 终稿补发事件
	eventKindPushUpdate = "push_update"
	// defaultStorePollInterval store 文件 mtime 轮询默认间隔（对齐 Python 5.0s）
	defaultStorePollInterval = 5.0
)

// logComponent 日志组件
const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// processSchedulerMu 进程级调度器注册表锁
	processSchedulerMu sync.RWMutex
	// processScheduler 进程级调度器（方案 B 核心）
	processScheduler *CronSchedulerService
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCronSchedulerService 创建调度器。
// 对齐 Python: CronSchedulerService.__init__(store, agent_client, message_handler, now_fn)。
func NewCronSchedulerService(
	store *CronJobStore,
	agentClient SchedulerAgentClient,
	publisher MessagePublisher,
	nowFn func() float64,
) *CronSchedulerService {
	if nowFn == nil {
		nowFn = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	return &CronSchedulerService{
		store:             store,
		agentClient:       agentClient,
		publisher:         publisher,
		nowFn:             nowFn,
		reloadCh:          make(chan struct{}, 1),
		jobs:              make(map[string]*CronJob),
		runs:              make(map[string]*CronRunState),
		activeRuns:        make(map[string]struct{}),
		storePollInterval: defaultStorePollInterval,
	}
}

// SetConfigLoader 注入 config 读取函数（pushToTargets 渠道元数据路由用）。
func (s *CronSchedulerService) SetConfigLoader(fn func() map[string]any) {
	s.configLoader = fn
}

// SetProcessScheduler 注册进程级调度器（方案 B：委托降级单调度器）。
// Gateway 启动时调用；CronTools.ensureScheduler 检查此注册表，
// 已注册且运行中则委托 reload 而非启动内嵌 fallback。
func SetProcessScheduler(svc *CronSchedulerService) {
	processSchedulerMu.Lock()
	processScheduler = svc
	processSchedulerMu.Unlock()
}

// GetProcessScheduler 获取进程级调度器（未注册返回 nil）。
func GetProcessScheduler() *CronSchedulerService {
	processSchedulerMu.RLock()
	defer processSchedulerMu.RUnlock()
	return processScheduler
}

// ResetProcessSchedulerForTest 清空注册表（仅供测试）。
func ResetProcessSchedulerForTest() {
	processSchedulerMu.Lock()
	processScheduler = nil
	processSchedulerMu.Unlock()
}

// IsRunning 是否运行中。
func (s *CronSchedulerService) IsRunning() bool {
	return s.running.Load()
}

// Start 启动调度器。
// 对齐 Python: start (scheduler.py L133-139)。
func (s *CronSchedulerService) Start(ctx context.Context) error {
	if s.running.Load() {
		return nil
	}
	s.running.Store(true)
	if err := s.Reload(); err != nil {
		// 对齐 Python：reload 失败不阻止启动
		logger.Warn(logComponent).Err(err).Msg("[Cron] initial reload failed")
	}
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	go s.loop(loopCtx)
	logger.Info(logComponent).Msg("[Cron] scheduler started")
	return nil
}

// Stop 停止调度器。
// 对齐 Python: stop (scheduler.py L141-155)。
// Go 并发补齐：mu 持有下翻转 running 并 drain 事件堆——
// ① 防止 loop 正在 handleEvent 途中（已过 running 检查）继续 onWake 启动新 run；
// ② onWake 的 wg.Add 与 activeRuns 写同在 mu 持有段内，与 Stop 的 mu 段互斥
// （WaitGroup 语义要求 Add 不得与 Wait 并发）。stop 后在途 runAgent goroutine
// 由 wg.Wait 收尾。
func (s *CronSchedulerService) Stop() {
	s.mu.Lock()
	s.running.Store(false)
	s.events = s.events[:0]
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.wg.Wait()
	logger.Info(logComponent).Msg("[Cron] scheduler stopped")
}

// Reload 重载任务并重建事件队列。
// 对齐 Python: reload (scheduler.py L157-198)。保留飞行中 push_update 事件。
// 加 sync.Mutex（Go 并发补齐：Python asyncio 单线程天然安全，Go 委托路径与 controller 路径可并发触发）。
func (s *CronSchedulerService) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobs, err := s.store.ListJobs()
	if err != nil {
		return err
	}
	newJobs := map[string]*CronJob{}
	for _, j := range jobs {
		newJobs[j.ID] = j
	}
	s.jobs = newJobs

	// 保留飞行中 push_update（单次任务 push 后即 disabled，但补发结果还未完成）
	pendingPushUpdates := make([]*cronEvent, 0)
	for _, ev := range s.events {
		if ev.Kind == eventKindPushUpdate {
			pendingPushUpdates = append(pendingPushUpdates, ev)
		}
	}
	s.events = s.events[:0]
	s.seq = 0
	for _, ev := range pendingPushUpdates {
		heapPush(&s.events, ev)
	}

	now := s.nowFn()
	for _, job := range jobs {
		if !job.Enabled {
			continue
		}
		pushDT, wakeDT, runID, err := s.computeNextRun(job, now)
		if err != nil {
			if IsNoNextDateError(err) {
				// 已过期 one-shot：标记 expired 并停用
				job.Enabled = false
				job.Expired = true
				if _, uerr := s.store.UpdateJob(job.ID, map[string]any{"enabled": false, "expired": true}); uerr != nil {
					logger.Warn(logComponent).Str("job_id", job.ID).Err(uerr).
						Msg("[Cron] mark expired failed")
				}
			} else {
				logger.Warn(logComponent).Str("job_id", job.ID).Err(err).
					Msg("[Cron] compute next run failed")
			}
			continue
		}
		s.scheduleEventLocked(wakeDT, eventKindWake, job.ID, runID)
		s.scheduleEventLocked(pushDT, eventKindPush, job.ID, runID)
	}

	s.syncStoreMtime()
	s.notifyReload()
	return nil
}

// TriggerRunNow 立即触发一次运行。
// 对齐 Python: trigger_run_now (scheduler.py L200-212)。
func (s *CronSchedulerService) TriggerRunNow(jobID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	s.mu.Lock()
	job := s.jobs[jobID]
	s.mu.Unlock()
	if job == nil {
		var err error
		job, err = s.store.GetJob(jobID)
		if err != nil {
			return "", err
		}
	}
	if job == nil {
		return "", fmt.Errorf("job not found")
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return "", err
	}
	now := time.Now().In(loc)
	runID := fmt.Sprintf("%s:%d", job.ID, now.Unix())
	s.mu.Lock()
	s.scheduleEventLocked(now, eventKindWake, job.ID, runID)
	s.scheduleEventLocked(now, eventKindPush, job.ID, runID)
	s.mu.Unlock()
	s.notifyReload()
	return runID, nil
}

// ──────────────────────────── 非导出函数（事件处理） ────────────────────────────

// loop 主循环（对齐 Python _loop L239-280）。
// 事件堆空时等 reload 或 5s 轮询 mtime；有事件时等到触发时刻。
func (s *CronSchedulerService) loop(ctx context.Context) {
	for s.running.Load() {
		// 检查 ctx 取消（Stop 触发）
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.mu.Lock()
		empty := len(s.events) == 0
		var delay float64
		var ev *cronEvent
		if !empty {
			ev = s.events[0]
			delay = ev.AtTS - s.nowFn()
		}
		s.mu.Unlock()

		if empty {
			select {
			case <-s.reloadCh:
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(s.storePollInterval * float64(time.Second))):
				s.checkStoreChanged()
			}
			continue
		}

		if delay > 0 {
			wait := delay
			if wait > s.storePollInterval {
				wait = s.storePollInterval
			}
			select {
			case <-s.reloadCh:
				continue
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(wait * float64(time.Second))):
				// 检查 store 变更后再判断是否到期
				if s.checkStoreChanged() {
					continue
				}
				if s.nowFn() < ev.AtTS {
					continue
				}
			}
		}

		// 到期：弹出并处理
		s.mu.Lock()
		if len(s.events) == 0 {
			s.mu.Unlock()
			continue
		}
		ev = heapPop(&s.events)
		s.mu.Unlock()
		s.handleEvent(ctx, ev)
	}
}

// handleEvent 处理单个事件（对齐 Python _handle_event L282-339）。
func (s *CronSchedulerService) handleEvent(ctx context.Context, ev *cronEvent) {
	s.mu.Lock()
	job := s.jobs[ev.JobID]
	s.mu.Unlock()

	if job == nil && ev.Kind != eventKindPushUpdate {
		return
	}
	if job == nil && ev.Kind == eventKindPushUpdate {
		// 从 run 状态重建临时 job（任务可能已删除，补发仍需放行）
		state := s.getRun(ev.RunID)
		if state == nil {
			logger.Info(logComponent).
				Str("job_id", ev.JobID).Str("run_id", ev.RunID).
				Msg("[Cron] push_update skipped: no state and no job")
			return
		}
		job = &CronJob{
			ID: state.JobID, Name: state.JobName, Enabled: false, Expired: false,
			CronExpr: "", Timezone: state.Timezone, Targets: state.Targets,
			SessionID: state.SessionID, ChatType: state.ChatType,
		}
		if job.Timezone == "" {
			job.Timezone = "Asia/Shanghai"
		}
		logger.Info(logComponent).
			Str("job_id", ev.JobID).Str("run_id", ev.RunID).
			Msg("[Cron] push_update using rebuilt job from state")
	}
	// push_update 是补发，即使单次任务已过期也必须放行
	if !job.Enabled && ev.Kind != eventKindPushUpdate {
		return
	}

	switch ev.Kind {
	case eventKindWake:
		s.onWake(ctx, job, ev.RunID)
	case eventKindPush:
		s.onPush(job, ev.RunID)
		if job.DeleteAfterRun {
			logger.Info(logComponent).Str("job_id", job.ID).
				Msg("[Cron] delete_after_run job, deleting after push")
			if _, err := s.store.DeleteJob(job.ID); err != nil {
				logger.Warn(logComponent).Str("job_id", job.ID).Err(err).
					Msg("[Cron] delete_after_run failed")
			}
			s.mu.Lock()
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return
		}
		s.rescheduleNext(job)
	case eventKindPushUpdate:
		s.onPushUpdate(job, ev.RunID)
	}
}

// onWake 唤醒事件：启动 runAgent goroutine（对齐 Python _on_wake L341-444）。
// Python 在 _on_wake 中先建 state、再检查 _run_tasks 去重（L365-366）；
// Go 同样以 activeRuns 集合实现 in-flight 去重——reload() 会清空事件堆并按
// 当前时刻重算入堆，执行中 run 的 wake 事件可能重算到过去时刻再次触发，
// 飞行中的 runID 必须直接返回，否则同一轮 agent 被双跑（双倍 LLM 消耗 +
// state 互相覆盖）。
func (s *CronSchedulerService) onWake(ctx context.Context, job *CronJob, runID string) {
	state := s.getOrCreateRun(job, runID)
	if state == nil {
		return
	}
	s.mu.Lock()
	// 停机后不再启动新 run（Stop 已 drain 事件堆，此为 handleEvent 途中
	// 携带已弹出事件的兜底——running 翻转与事件弹出的窗口内到达的兜底）
	if !s.running.Load() {
		s.mu.Unlock()
		return
	}
	if _, running := s.activeRuns[runID]; running {
		s.mu.Unlock()
		return // in-flight 去重（对齐 Python L365-366）
	}
	s.activeRuns[runID] = struct{}{}
	// wg.Add 在 mu 持有段内（WaitGroup 语义：Add 不得与 Stop 的 Wait 并发；
	// Stop 同在 mu 内翻转 running，此处的 running 检查与 Add 原子成立）
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.activeRuns, runID)
			s.mu.Unlock()
		}()
		s.runAgent(ctx, job, state)
	}()
}

// runAgent 执行 agent 调用（对齐 Python _run_agent L368-443）。
// finally 语义：即时排 push_update + 迟到补发判定。
func (s *CronSchedulerService) runAgent(ctx context.Context, job *CronJob, state *CronRunState) {
	s.mu.Lock()
	state.Status = "running"
	state.StartedAt = s.nowFn()
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		state.FinishedAt = s.nowFn()
		// 失败运行也产 result_text 供推送
		if state.ResultText == "" && state.Error != "" {
			state.ResultText = "[cron] 任务执行失败: " + state.Error
		}
		// placeholder 已发且终稿未推 → 即时排 push_update
		if state.PlaceholderSent && !state.PushedFinal && state.ResultText != "" {
			logger.Info(logComponent).
				Str("job_id", job.ID).Str("run_id", state.RunID).
				Int("text_len", len(state.ResultText)).
				Msg("[Cron] scheduling immediate push_update after agent finished")
			s.scheduleEventLocked(s.nowTime(), eventKindPushUpdate, job.ID, state.RunID)
		}
		// push_at 已过 → 迟到补发
		if pushDT, err := time.Parse(time.RFC3339, state.PushAtISO); err == nil {
			if pushDT.Unix() <= int64(s.nowFn()) && !state.PushedFinal && state.ResultText != "" {
				logger.Info(logComponent).
					Str("job_id", job.ID).Str("run_id", state.RunID).
					Int("text_len", len(state.ResultText)).
					Msg("[Cron] scheduling late push_update because push_at<=now")
				s.scheduleEventLocked(s.nowTime(), eventKindPushUpdate, job.ID, state.RunID)
			}
		}
		s.mu.Unlock()
	}()

	// 构造 E2A envelope（对齐 Python e2a_from_agent_fields）
	ts := time.Now().UnixMilli()
	sessionID := fmt.Sprintf("cron_%d_%s", ts, job.ID)
	mode := job.Mode
	if mode == "" {
		mode = "agent"
	}
	params := map[string]any{
		"content": job.Description,
		"query":   job.Description,
		"mode":    mode,
		"cron": map[string]any{
			"job_id":   job.ID,
			"job_name": job.Name,
			"run_id":   state.RunID,
			"push_at":  state.PushAtISO,
			"wake_at":  state.WakeAtISO,
		},
	}
	envelope := e2a.E2AFromAgentFields("cron-"+state.RunID,
		e2a.WithFieldChannelID("__cron__"),
		e2a.WithFieldSessionID(sessionID),
		e2a.WithFieldReqMethod(string(schema.ReqMethodChatSend)),
		e2a.WithFieldParams(params),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(s.nowFn()),
		e2a.WithFieldMetadata(map[string]any{
			"cron": map[string]any{"job_id": job.ID, "run_id": state.RunID},
		}),
	)

	// nil 客户端守卫（对齐 Python: agent_client=None 时 send_request 抛
	// AttributeError 被 _run_agent 的 except 捕获置 failed——Go nil 接口
	// 方法调用直接 panic，须显式拦截为 failed 状态）
	if s.agentClient == nil {
		s.mu.Lock()
		state.Status = "failed"
		state.Error = "agent client is nil"
		s.mu.Unlock()
		return
	}
	resp, err := s.agentClient.SendRequest(ctx, envelope)
	if err != nil {
		s.mu.Lock()
		state.Status = "failed"
		state.Error = err.Error()
		s.mu.Unlock()
		return
	}
	text := extractTextFromAgentPayload(resp.Payload)
	if text == "" {
		text = "[cron] 任务完成，但未返回可展示文本"
	}
	s.mu.Lock()
	state.ResultText = text
	if resp.OK {
		state.Status = "succeeded"
	} else {
		state.Status = "failed"
	}
	s.mu.Unlock()
}

// onPush 推送事件（对齐 Python _on_push L446-480）。
func (s *CronSchedulerService) onPush(job *CronJob, runID string) {
	state := s.getOrCreateRun(job, runID)
	if state == nil {
		return
	}
	s.mu.Lock()
	pushedFinal := state.PushedFinal
	resultText := state.ResultText
	s.mu.Unlock()

	if pushedFinal {
		return
	}
	if resultText != "" {
		s.pushToTargets(job, state, resultText, false)
		s.mu.Lock()
		state.PushedFinal = true
		s.mu.Unlock()
		return
	}
	// 未就绪：发占位符
	placeholder := fmt.Sprintf("[cron] %s 正在执行中，结果稍后补发（push_at=%s）", job.Name, state.PushAtISO)
	s.pushToTargets(job, state, placeholder, true)
	s.mu.Lock()
	state.PlaceholderSent = true
	s.mu.Unlock()
}

// onPushUpdate 终稿补发事件（对齐 Python _on_push_update L482-501）。
func (s *CronSchedulerService) onPushUpdate(job *CronJob, runID string) {
	s.mu.Lock()
	state := s.runs[runID]
	s.mu.Unlock()
	if state == nil {
		logger.Info(logComponent).
			Str("job_id", job.ID).Str("run_id", runID).
			Msg("[Cron] push_update skipped: no state")
		return
	}
	s.mu.Lock()
	pushedFinal := state.PushedFinal
	resultText := state.ResultText
	s.mu.Unlock()
	if pushedFinal {
		logger.Info(logComponent).
			Str("job_id", job.ID).Str("run_id", runID).
			Msg("[Cron] push_update skipped: already pushed_final")
		return
	}
	if resultText == "" {
		logger.Info(logComponent).
			Str("job_id", job.ID).Str("run_id", runID).
			Msg("[Cron] push_update skipped: empty result_text")
		return
	}
	logger.Info(logComponent).
		Str("job_id", job.ID).Str("run_id", runID).Int("text_len", len(resultText)).
		Msg("[Cron] push_update start")
	s.pushToTargets(job, state, resultText, false)
	s.mu.Lock()
	state.PushedFinal = true
	s.mu.Unlock()
	logger.Info(logComponent).
		Str("job_id", job.ID).Str("run_id", runID).
		Msg("[Cron] push_update done")
}

// pushToTargets 推送结果到目标频道（对齐 Python _push_to_targets L503-713）。
// 9 渠道元数据路由：feishu/xiaoyi/whatsapp/wecom/wechat/dingtalk + feishu_enterprise 前缀。
func (s *CronSchedulerService) pushToTargets(job *CronJob, state *CronRunState, text string, isPlaceholder bool) {
	// nil 守卫兜底（对齐 Python message_handler=None 时 _loop 捕获异常的净效果：
	// 该轮消息丢弃 + Error 日志 + 调度器存活——Go 无 recover 的 loop goroutine
	// 无法容忍 nil 接口 panic，须显式拦截）
	if s.publisher == nil {
		logger.Error(logComponent).
			Str("job_id", job.ID).Str("run_id", state.RunID).
			Msg("[Cron] push skipped: message publisher is nil")
		return
	}
	// Go 并发补齐：快照 state 状态字段（Python asyncio 单线程天然安全，
	// Go 的 runAgent goroutine 可能并发写 Status/RunID/ISO 字段）
	s.mu.Lock()
	statusSnap := state.Status
	runIDSnap := state.RunID
	pushAtSnap := state.PushAtISO
	wakeAtSnap := state.WakeAtISO
	s.mu.Unlock()

	logger.Info(logComponent).
		Str("job_id", job.ID).Str("run_id", runIDSnap).
		Str("channel", strings.TrimSpace(job.Targets)).
		Bool("is_placeholder", isPlaceholder).
		Int("text_len", len(text)).Str("status", statusSnap).
		Msg("[Cron] push_to_targets")

	payloadExtra := map[string]any{
		"content": text,
		"cron": map[string]any{
			"job_id":         job.ID,
			"job_name":       job.Name,
			"run_id":         runIDSnap,
			"push_at":        pushAtSnap,
			"wake_at":        wakeAtSnap,
			"is_placeholder": isPlaceholder,
			"status":         statusSnap,
		},
	}
	channelID := strings.TrimSpace(job.Targets)
	if channelID == "" {
		return
	}

	// ─── 路由 session_id 与 metadata 构造（对齐 Python L529-547）───
	// 企业飞书：优先用作业里绑定的 SessionMap session_id（feishu::chat_id::bot_id::...），
	// 避免多群共用 bot 时误用 config 中的 last_*。Web/TUI：不绑定 session_id。
	var metadata map[string]any
	msgSessionID := ""
	routingSID := strings.TrimSpace(job.SessionID)
	if routingSID != "" && channelID != "web" && channelID != "tui" {
		msgSessionID = routingSID
	}
	if strings.HasPrefix(channelID, "feishu_enterprise:") && routingSID != "" && strings.Contains(routingSID, "::") {
		parts := strings.Split(routingSID, "::")
		if len(parts) >= 3 && parts[0] == "feishu" {
			chatPart := strings.TrimSpace(parts[1])
			if chatPart != "" {
				metadata = map[string]any{"feishu_chat_id": chatPart}
				if len(parts) >= 6 {
					openPart := strings.TrimSpace(parts[3])
					if openPart != "" {
						metadata["feishu_open_id"] = openPart
					}
				}
				msgSessionID = chatPart
			}
		}
	}

	// ─── config.yaml 最近回发身份（对齐 Python L549-633）───
	if metadata == nil {
		metadata = s.channelFallbackMetadata(channelID)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	// dingtalk 兜底（对齐 Python L635-642）
	if channelID == "dingtalk" {
		if routingSID != "" {
			if v, ok := metadata["dingtalk_sender_id"].(string); !ok || strings.TrimSpace(v) == "" {
				metadata["dingtalk_sender_id"] = routingSID
			}
		}
		if v, ok := metadata["conversation_type"].(string); !ok || strings.TrimSpace(v) == "" {
			metadata["conversation_type"] = "1"
		}
	}

	// ─── group_digital_avatar 路由决策（对齐 Python L644-698）───
	groupDigitalAvatar := false
	myUserID := ""
	chCfg := s.channelConfig(channelID)
	switch {
	case channelID == "wecom" || channelID == "feishu":
		groupDigitalAvatar = boolAny(chCfg["group_digital_avatar"])
		myUserID = strings.TrimSpace(strAny(chCfg["my_user_id"]))
	case strings.HasPrefix(channelID, "feishu_enterprise:"):
		enterpriseCfg := s.enterpriseBotConfig(channelID)
		groupDigitalAvatar = boolAny(enterpriseCfg["group_digital_avatar"])
		myUserID = strings.TrimSpace(strAny(enterpriseCfg["my_user_id"]))
	}

	if groupDigitalAvatar && myUserID != "" {
		// 判断定时任务是在群聊还是私聊中创建的（优先 job.chat_type）
		if job.ChatType == "group" {
			// 补充 outbound pipeline 所需的 metadata 前置条件
			metadata["chat_type"] = "group"
			if channelID == "wecom" {
				metadata["reply_wecom_user_id"] = myUserID
			} else if channelID == "feishu" || strings.HasPrefix(channelID, "feishu_enterprise:") {
				metadata["reply_candidate_feishu_open_id"] = myUserID
			}
			metadata["reply_candidate_reason"] = "cron_target_user"
			metadata["reply_target_name"] = myUserID
			// 标记为定时任务消息，避免在群聊中重复发送确认消息
			metadata["is_cron_job"] = true
			logger.Info(logComponent).
				Str("my_user_id", myUserID).Str("channel", channelID).Str("job_id", job.ID).
				Msg("[Cron] job created in group chat, enabling IMOutboundPipeline routing")
		} else {
			logger.Info(logComponent).
				Str("chat_type", job.ChatType).Str("channel", channelID).Str("job_id", job.ID).
				Msg("[Cron] job created in p2p chat, skipping IMOutboundPipeline routing")
		}
	}

	// ─── 构造 Message 并发布（对齐 Python L700-713）───
	msg := schema.NewEventMessage(channelID, msgSessionID, schema.EventTypeChatFinal, payloadExtra,
		schema.WithMetadata(metadata),
		schema.WithGroupDigitalAvatar(groupDigitalAvatar),
	)
	msg.ID = fmt.Sprintf("cron-push-%s-%s", runIDSnap, channelID)
	msg.Timestamp = s.nowFn()
	s.publisher.PublishRobotMessages(msg)
}

// channelFallbackMetadata 从 config.yaml 取渠道最近回发身份（对齐 Python L549-633）。
// feishu/xiaoyi/whatsapp/wecom/wechat/dingtalk 六渠道逐字段对齐。
func (s *CronSchedulerService) channelFallbackMetadata(channelID string) map[string]any {
	cfg := s.loadConfig()
	if cfg == nil {
		return nil
	}
	channelsCfg, _ := cfg["channels"].(map[string]any)
	if channelsCfg == nil {
		return nil
	}
	chCfg, _ := channelsCfg[channelID].(map[string]any)
	if chCfg == nil {
		chCfg = map[string]any{}
	}
	var metadata map[string]any
	switch channelID {
	case "feishu":
		lastChatID := strings.TrimSpace(strAny(chCfg["last_chat_id"]))
		lastOpenID := strings.TrimSpace(strAny(chCfg["last_open_id"]))
		if lastChatID != "" || lastOpenID != "" {
			metadata = map[string]any{"feishu_chat_id": lastChatID, "feishu_open_id": lastOpenID}
		}
	case "xiaoyi":
		lastSessionID := strings.TrimSpace(strAny(chCfg["last_session_id"]))
		lastTaskID := strings.TrimSpace(strAny(chCfg["last_task_id"]))
		if lastSessionID != "" || lastTaskID != "" {
			metadata = map[string]any{"xiaoyi_session_id": lastSessionID, "xiaoyi_task_id": lastTaskID}
		}
	case "whatsapp":
		lastJID := strings.TrimSpace(strAny(chCfg["last_jid"]))
		if lastJID != "" {
			metadata = map[string]any{"whatsapp_jid": lastJID}
		}
	case "wecom":
		lastChatID := strings.TrimSpace(strAny(chCfg["last_chat_id"]))
		lastUserID := strings.TrimSpace(strAny(chCfg["last_user_id"]))
		if lastChatID != "" || lastUserID != "" {
			metadata = map[string]any{"wecom_chat_id": lastChatID, "wecom_user_id": lastUserID}
		}
	case "wechat":
		lastUserID := strings.TrimSpace(strAny(chCfg["last_user_id"]))
		lastContextToken := strings.TrimSpace(strAny(chCfg["last_context_token"]))
		if lastUserID != "" {
			metadata = map[string]any{
				"wechat_user_id":   lastUserID,
				"reply_to_user_id": lastUserID,
			}
			if lastContextToken != "" {
				metadata["wechat_context_token"] = lastContextToken
				metadata["context_token"] = lastContextToken
			}
		}
	case "dingtalk":
		lastSenderID := strings.TrimSpace(strAny(chCfg["last_sender_id"]))
		lastConversationID := strings.TrimSpace(strAny(chCfg["last_conversation_id"]))
		lastConversationType := strings.TrimSpace(strAny(chCfg["last_conversation_type"]))
		if lastSenderID != "" || lastConversationID != "" {
			ct := lastConversationType
			if ct == "" {
				ct = "1"
			}
			metadata = map[string]any{
				"dingtalk_sender_id": lastSenderID,
				"dingtalk_chat_id":   lastConversationID,
				"conversation_id":    lastConversationID,
				"conversation_type":  ct,
			}
		}
	}
	return metadata
}

// channelConfig 读取渠道配置 map。
func (s *CronSchedulerService) channelConfig(channelID string) map[string]any {
	cfg := s.loadConfig()
	if cfg == nil {
		return map[string]any{}
	}
	channelsCfg, _ := cfg["channels"].(map[string]any)
	if channelsCfg == nil {
		return map[string]any{}
	}
	chCfg, _ := channelsCfg[channelID].(map[string]any)
	if chCfg == nil {
		return map[string]any{}
	}
	return chCfg
}

// enterpriseBotConfig 读取 feishu_enterprise:<app_id> 对应 bot 配置。
// 对齐 Python L568-585：遍历 channels.feishu_enterprise 下 bot 配置匹配 app_id。
func (s *CronSchedulerService) enterpriseBotConfig(channelID string) map[string]any {
	cfg := s.loadConfig()
	if cfg == nil {
		return map[string]any{}
	}
	channelsCfg, _ := cfg["channels"].(map[string]any)
	if channelsCfg == nil {
		return map[string]any{}
	}
	enterpriseCfg, _ := channelsCfg["feishu_enterprise"].(map[string]any)
	if enterpriseCfg == nil {
		return map[string]any{}
	}
	appID := ""
	if parts := strings.SplitN(channelID, ":", 2); len(parts) == 2 {
		appID = strings.TrimSpace(parts[1])
	}
	for _, v := range enterpriseCfg {
		botCfg, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if appID != "" && strings.TrimSpace(strAny(botCfg["app_id"])) == appID {
			return botCfg
		}
	}
	return map[string]any{}
}

// loadConfig 读取 config（注入的 loader，nil 返回 nil）。
func (s *CronSchedulerService) loadConfig() map[string]any {
	if s.configLoader == nil {
		return nil
	}
	return s.configLoader()
}

// checkStoreChanged 检查 store 文件 mtime 变更（对齐 Python _check_store_changed L117-128）。
// 方案 B 单调度器下保留——双部署形态另一进程写文件仍需感知。
func (s *CronSchedulerService) checkStoreChanged() bool {
	mtime := s.getStoreMtime()
	s.mu.Lock()
	last := s.lastStoreMtime
	s.mu.Unlock()
	if mtime != 0 && mtime != last && last != 0 {
		logger.Info(logComponent).
			Float64("last_mtime", last).Float64("mtime", mtime).
			Msg("[Cron] store file changed, reloading")
		if err := s.Reload(); err != nil {
			logger.Warn(logComponent).Err(err).Msg("[Cron] reload after store change failed")
		}
		return true
	}
	return false
}

// getStoreMtime 读 store 文件 mtime（失败 0.0）。
func (s *CronSchedulerService) getStoreMtime() float64 {
	info, err := os.Stat(s.store.Path())
	if err != nil {
		return 0
	}
	return float64(info.ModTime().UnixNano()) / 1e9
}

// syncStoreMtime 快照当前 mtime（需在 mu 持有下调用）。
func (s *CronSchedulerService) syncStoreMtime() {
	s.lastStoreMtime = s.getStoreMtime()
}

// getOrCreateRun 获取或创建运行状态（对齐 Python _on_wake/_on_push 的 state 构造）。
func (s *CronSchedulerService) getOrCreateRun(job *CronJob, runID string) *CronRunState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state, ok := s.runs[runID]; ok {
		return state
	}
	// 从 run_id 尾段恢复 push_ts
	pushTS := int64(s.nowFn())
	if parts := strings.Split(runID, ":"); len(parts) > 0 {
		if n, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil {
			pushTS = n
		}
	}
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		loc = time.UTC
	}
	pushDT := time.Unix(pushTS, 0).In(loc)
	wakeDT := pushDT.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
	state := &CronRunState{
		RunID:     runID,
		JobID:     job.ID,
		WakeAtISO: wakeDT.Format(time.RFC3339),
		PushAtISO: pushDT.Format(time.RFC3339),
		Status:    "pending",
		JobName:   job.Name,
		Targets:   job.Targets,
		SessionID: job.SessionID,
		ChatType:  job.ChatType,
		Timezone:  job.Timezone,
	}
	s.runs[runID] = state
	return state
}

// getRun 读取运行状态。
func (s *CronSchedulerService) getRun(runID string) *CronRunState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs[runID]
}

// scheduleEventLocked 入队事件（需在 mu 持有下调用）。
// 对齐 Python _schedule_event L214-221：1 秒内到期的事件额外唤醒主循环。
func (s *CronSchedulerService) scheduleEventLocked(at time.Time, kind, jobID, runID string) {
	atTS := float64(at.UnixNano()) / 1e9
	s.seq++
	ev := &cronEvent{AtTS: atTS, Seq: s.seq, Kind: kind, JobID: jobID, RunID: runID}
	heapPush(&s.events, ev)
	if atTS <= s.nowFn()+1.0 {
		s.notifyReload()
	}
}

// notifyReload 非阻塞通知主循环。
func (s *CronSchedulerService) notifyReload() {
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

// nowTime 将 nowFn 秒转 time.Time（对齐 Python datetime.fromtimestamp(self._now_fn())）。
func (s *CronSchedulerService) nowTime() time.Time {
	return time.Unix(int64(s.nowFn()), 0)
}

// computeNextRun 计算下次运行三元组（需在 mu 持有下调用）。
// 对齐 Python _compute_next_run L223-229 + _cron_next_push_dt L50-66。
func (s *CronSchedulerService) computeNextRun(job *CronJob, nowTS float64) (time.Time, time.Time, string, error) {
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	base := time.Unix(int64(nowTS), 0).In(loc)
	normalized, err := NormalizeCronExpr(job.CronExpr)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	pushDT, err := NextFireTime(normalized, base)
	if err != nil {
		return time.Time{}, time.Time{}, "", err
	}
	wakeDT := pushDT.Add(-time.Duration(max(0, job.WakeOffsetSeconds) * int(time.Second)))
	runID := fmt.Sprintf("%s:%d", job.ID, pushDT.Unix())
	return pushDT, wakeDT, runID, nil
}

// rescheduleNext push 完成后调度下一轮（对齐 Python _handle_event push 分支 L319-337）。
func (s *CronSchedulerService) rescheduleNext(job *CronJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pushDT, wakeDT, nextRunID, err := s.computeNextRun(job, s.nowFn())
	if err != nil {
		if IsNoNextDateError(err) {
			job.Enabled = false
			job.Expired = true
			if _, uerr := s.store.UpdateJob(job.ID, map[string]any{"enabled": false, "expired": true}); uerr != nil {
				logger.Warn(logComponent).Str("job_id", job.ID).Err(uerr).
					Msg("[Cron] mark expired after push failed")
			}
		} else {
			logger.Warn(logComponent).Str("job_id", job.ID).Err(err).
				Msg("[Cron] compute next run failed after push")
		}
		return
	}
	s.scheduleEventLocked(wakeDT, eventKindWake, job.ID, nextRunID)
	s.scheduleEventLocked(pushDT, eventKindPush, job.ID, nextRunID)
}

// extractTextFromAgentPayload 从 agent 响应 payload 提取文本。
// 对齐 Python _extract_text_from_agent_payload L26-47。
func extractTextFromAgentPayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if content, ok := payload["content"].(map[string]any); ok {
		if out, ok := content["output"].(string); ok {
			return out
		}
		if out, ok := content["output"]; ok && out != nil {
			return fmt.Sprintf("%v", out)
		}
		return fmt.Sprintf("%v", content)
	}
	if content, ok := payload["content"].(string); ok {
		return content
	}
	if hb, ok := payload["heartbeat"].(string); ok && hb != "" {
		return hb
	}
	if text, ok := payload["text"].(string); ok && text != "" {
		return text
	}
	return ""
}

// ──────────────────────────── heap 实现 ────────────────────────────

// heapPush 入堆（包内简写，避免 container/heap.Interface 样板）。
func heapPush(h *eventHeap, ev *cronEvent) {
	*h = append(*h, ev)
	up(*h, len(*h)-1)
}

// heapPop 弹堆顶。
func heapPop(h *eventHeap) *cronEvent {
	if len(*h) == 0 {
		return nil
	}
	top := (*h)[0]
	last := (*h)[len(*h)-1]
	*h = (*h)[:len(*h)-1]
	if len(*h) > 0 {
		(*h)[0] = last
		down(*h, 0)
	}
	return top
}

// up 上浮。
func up(h eventHeap, i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if eventLess(h[i], h[parent]) {
			h[i], h[parent] = h[parent], h[i]
			i = parent
		} else {
			break
		}
	}
}

// down 下沉。
func down(h eventHeap, i int) {
	n := len(h)
	for {
		l, r := 2*i+1, 2*i+2
		smallest := i
		if l < n && eventLess(h[l], h[smallest]) {
			smallest = l
		}
		if r < n && eventLess(h[r], h[smallest]) {
			smallest = r
		}
		if smallest == i {
			return
		}
		h[i], h[smallest] = h[smallest], h[i]
		i = smallest
	}
}

// eventLess 事件比较：AtTS 优先，同刻 Seq 稳定序。
func eventLess(a, b *cronEvent) bool {
	if a.AtTS != b.AtTS {
		return a.AtTS < b.AtTS
	}
	return a.Seq < b.Seq
}
