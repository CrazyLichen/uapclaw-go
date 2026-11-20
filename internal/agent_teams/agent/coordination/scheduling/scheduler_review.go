package scheduling

// scheduler_review.go 调度器评审半区（F_62）：派发、判定、结算与升级。
//
// Python: agent_teams/agent/scheduling/scheduler.py L527-1085 的
// _reconcile_reviews/_settle_pass/_settle_fail_or_escalate/
// _handle_undecided/_dispatch_to_reviewer/_spawn_temp_reviewer/
// _follow_up_unvoted_reviewer（2026-12-04 快照）。
//
// 已知偏差 D-25.2b-13：
//  1. 临时 reviewer harness 的构建/运行经 ReviewerRunnerFunc 窄缝注入
//     （Python 直接 TeamHarness.build+run_once——Go 侧装配点注入等价运行体，
//     便于测试与模型分配器隔离）。
//  2. _nudge_unvoted_reviewer（运行中 steer）依赖 DeepAgent 运行中注入能力，
//     随 harness steer 面落地后接线（回收条件=Steer 进 DeepAgentInterface）。

import (
	"context"
	"fmt"
	"strings"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

const (
	// reviewRunsMaxReviewRunsPerRound 每验收人每轮的最大运行次数；超过后升级 leader。
	// Python: _MAX_REVIEW_RUNS_PER_ROUND（scheduler.go 已有 maxReviewRunsPerRound）。
	reviewRunsMaxReviewRunsPerRound = maxReviewRunsPerRound
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReviewerRunnerFunc 临时 reviewer 运行体：构建一次性评审 harness 并运行
// verify_task。返回该轮是否投出票（投票判定由调用方经 GetReviewTally 复核）。
// 偏差 D-25.2b-13①：窄缝注入等价运行体。
type ReviewerRunnerFunc func(ctx context.Context, reviewer string, task *database.TeamTaskBase) (voted bool)

// SetReviewerRunner 注入临时 reviewer 运行体（装配点调用一次）。
func (s *TeamScheduler) SetReviewerRunner(runner ReviewerRunnerFunc) {
	s.reviewerRunner = runner
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reconcileReviews 派发、判定、结算与升级开放评审轮。
// Python: _reconcile_reviews (scheduler.py L527-590)。
func (s *TeamScheduler) reconcileReviews(ctx context.Context) (bool, error) {
	backend := s.backend
	if backend != nil {
		enabled, err := backend.TaskVerificationEnabled(ctx)
		if err != nil {
			return false, err
		}
		if !enabled {
			return false, nil
		}
	}
	// 扫描不读正文：只有验收人派发与 fail 反馈读 content，它们各自重读自己
	// 操作的那一条任务。
	inReview, err := s.taskMgr.ListTasks(ctx, "in_review")
	if err != nil {
		return false, err
	}
	nowMs := currentTimeMs()
	acted := false
	for _, task := range inReview {
		reviewers := task.Reviewers()
		if len(reviewers) == 0 {
			// 无验收人任务不可能经正常流程进入 IN_REVIEW；交给 leader 看板
			// 自行判断而非猜测。
			logger.Warn(logComponent).Str("task_id", task.TaskID).
				Msg("[scheduler] task is in_review without reviewers")
			continue
		}

		// 按验收人派发：结束运行仍未投票的 reviewer 释放标记
		//（followUpUnvotedReviewer）后重新运行；已投票的永不重跑。
		tally := s.taskMgr.GetReviewTally(task)
		votedSet := map[string]bool{}
		if voted, ok := tally["voted"].([]string); ok {
			for _, name := range voted {
				votedSet[name] = true
			}
		}
		for _, reviewer := range reviewers {
			dispatchKey := [3]string{task.TaskID, fmt.Sprintf("%d", task.ReviewRound), reviewer}
			if votedSet[reviewer] {
				continue
			}
			if _, dispatched := s.reviewDispatched[dispatchKey]; dispatched {
				continue
			}
			s.reviewDispatched[dispatchKey] = struct{}{}
			// 派发时带正文重读它操作的那一条任务——板面扫描保持无正文。
			fullTask := s.withContent(ctx, task)
			s.dispatchToReviewer(ctx, reviewer, fullTask)
		}

		verdict := SettleReviewTally(tally, 0.85)
		switch verdict {
		case VerdictPass:
			if s.settlePass(ctx, task) {
				logger.Info(logComponent).
					Str("task_id", task.TaskID).Int64("round", task.ReviewRound).
					Int("pass", intOfTally(tally["pass_count"])).Int("fail", intOfTally(tally["fail_count"])).
					Int("total", intOfTally(tally["reviewer_count"])).
					Msg("[judge-pass]")
				acted = true
			}
		case VerdictFail:
			fullTask := s.withContent(ctx, task)
			if s.settleFailOrEscalate(ctx, fullTask, tally) {
				logger.Info(logComponent).
					Str("task_id", task.TaskID).Int64("round", task.ReviewRound).
					Int("pass", intOfTally(tally["pass_count"])).Int("fail", intOfTally(tally["fail_count"])).
					Int("total", intOfTally(tally["reviewer_count"])).
					Msg("[judge-fail]")
				acted = true
			}
		default:
			s.handleUndecided(ctx, task, tally, nowMs)
		}
	}
	return acted, nil
}

// withContent 带正文重读一条扫描任务；回退到扫描行。
// Python: _with_content (scheduler.py L592-595)。
func (s *TeamScheduler) withContent(ctx context.Context, task *database.TeamTaskBase) *database.TeamTaskBase {
	full, err := s.taskMgr.Get(ctx, task.TaskID)
	if err != nil || full == nil {
		return task
	}
	return full
}

// settlePass 结算通过：settle_review pass + 终态 digest。
// 作者不因 pass 被唤醒（F_119）：leader 经 digest 得知结果，作者在其下一次
// 输入时经自身 rail 的任务结果通道得知。
// Python: _settle_pass (scheduler.py L596-607)。
func (s *TeamScheduler) settlePass(ctx context.Context, task *database.TeamTaskBase) bool {
	result, err := s.taskMgr.SettleReview(ctx, task.TaskID, "pass", "")
	if err != nil || !result.OK {
		logger.Debug(logComponent).Str("task_id", task.TaskID).
			Str("reason", result.Reason).
			Msg("[scheduler] pass settle did not apply")
		return false
	}
	s.digestTaskDone(ctx, task, true)
	return true
}

// settleFailOrEscalate 结算失败或升级：超限升级 + 一次性返工总结请求；
// 未超限结算 fail + 返工邮件 + 反馈分发。
// Python: _settle_fail_or_escalate (scheduler.py L608-625)。
func (s *TeamScheduler) settleFailOrEscalate(ctx context.Context, task *database.TeamTaskBase, tally map[string]any) bool {
	maxRounds := s.defaultMaxRounds
	if task.MaxReviewRounds != nil {
		maxRounds = *task.MaxReviewRounds
	}
	feedback := FormatFailFeedback(failFeedbackOf(tally))
	if inspectorAvg, has := tally["inspector_avg"].(float64); has {
		statusKey := "scheduler.inspector_avg_fail"
		if inspectorAvg >= 0.85 {
			statusKey = "scheduler.inspector_avg_pass"
		}
		status, _ := atschema.T(statusKey)
		line, _ := atschema.T("scheduler.inspector_avg_line", map[string]any{"avg": inspectorAvg, "status": status})
		feedback += line
	}
	if task.ReviewRound >= maxRounds {
		s.escalate(ctx, task, RenderLeaderEscalationRounds(task, feedback), "verdict")
		roundKey := [2]string{task.TaskID, fmt.Sprintf("%d", task.ReviewRound)}
		assignee := derefStr(task.Assignee)
		if assignee != "" {
			if _, requested := s.summaryRequested[roundKey]; !requested {
				s.summaryRequested[roundKey] = struct{}{}
				s.sendAsLeader(ctx, assignee, MetaReworkSummary(task, maxRounds))
			}
		}
		s.dispatchReviewFeedback(ctx, task, feedback)
		return false
	}
	result, err := s.taskMgr.SettleReview(ctx, task.TaskID, "fail", feedback)
	if err != nil || !result.OK {
		logger.Debug(logComponent).Str("task_id", task.TaskID).
			Str("reason", result.Reason).
			Msg("[scheduler] fail settle did not apply")
		return false
	}
	if assignee := derefStr(task.Assignee); assignee != "" {
		s.sendAsLeader(ctx, assignee, MetaRework(task, maxRounds, feedback))
	}
	s.dispatchReviewFeedback(ctx, task, feedback)
	return true
}

// SetReviewFeedbackHandler 注入任务级反馈回调（挂载 rail 的 HandleReviewFeedback；
// 装配点注入）。sessionID 进入反馈负载（Python build_context.session_id）。
func (s *TeamScheduler) SetReviewFeedbackHandler(sessionID string, handler func(ctx context.Context, payload map[string]any) error) {
	s.reviewFeedbackSessionID = sessionID
	s.reviewFeedbackHandler = handler
}

// dispatchReviewFeedback 失败一轮后即派发反馈回调（后台运行）。
// Python: _dispatch_review_feedback (scheduler.py L627-658)——构造负载、去重后
// create_task 后台运行；Go 侧 goroutine + reviewFeedbackWG，终态聚合
// （dispatchTeamReviewFeedback）Wait 等待同形。
func (s *TeamScheduler) dispatchReviewFeedback(ctx context.Context, task *database.TeamTaskBase, feedback string) {
	normalized := strings.TrimSpace(feedback)
	if normalized == "" {
		return
	}
	if s.reviewFeedbackHandler == nil {
		return
	}
	roundKey := [2]string{task.TaskID, fmt.Sprintf("%d", task.ReviewRound)}
	// 去重账本会被事件总线扫描与 reviewer follow-up goroutine 的重扫
	// （scheduler_review.go followUpUnvotedReviewer → scan）两条线并发读写，
	// 与 DB 状态机无关，必须持锁（-race 实抓路径）。
	s.reviewFeedbackMu.Lock()
	if _, dispatched := s.reviewFeedbackRounds[roundKey]; dispatched {
		s.reviewFeedbackMu.Unlock()
		return
	}
	s.reviewFeedbackRounds[roundKey] = struct{}{}
	s.reviewFeedbackMu.Unlock()
	// Python: payload 构造（scheduler.py L645-655），键名是 rail 契约不得改动。
	payload := map[string]any{
		"team_id":      s.teamName,
		"session_id":   s.reviewFeedbackSessionID,
		"task_id":      task.TaskID,
		"review_round": task.ReviewRound,
		"task_title":   task.Title,
		"task_content": task.Content,
		"assignee":     derefStr(task.Assignee),
		"feedback":     normalized,
	}
	// Python: asyncio.create_task 后台运行（scheduler.py L656-658）；Go goroutine
	// + WaitGroup（Add 先于 go，Done 随 goroutine），终态聚合 wg.Wait 同形。
	// 聚合点与派发的互斥由 DB 状态机保证：派发前提是 ReviseTask 的
	// IN_REVIEW CAS 成功（任务转 IN_PROGRESS、板未清），板清聚合触发时
	// 业务上不存在在途新派发——Wait 无「等待期间新 Add」窗口。
	s.reviewFeedbackWG.Add(1)
	go func() {
		defer s.reviewFeedbackWG.Done()
		// Python: _invoke_review_feedback_rail 的吞错日志形态（L660-671）。
		if err := s.reviewFeedbackHandler(ctx, payload); err != nil {
			logger.Error(logComponent).Err(err).
				Str("task_id", fmt.Sprint(payload["task_id"])).
				Str("review_round", fmt.Sprint(payload["review_round"])).
				Msg("[scheduler] review feedback Rail failed")
		}
	}()
}

// handleUndecided 开放轮的停滞处置：超过停滞超时后升级一次。
// reviewer 是一次性 harness 没有信箱，所以没有可发的提醒；无票结束的运行
// 由 followUpUnvotedReviewer 重派。
// Python: _handle_undecided (scheduler.py L730-754)。
func (s *TeamScheduler) handleUndecided(ctx context.Context, task *database.TeamTaskBase, tally map[string]any, nowMs int64) {
	updatedAt := task.UpdatedAt
	if updatedAt <= 0 {
		updatedAt = nowMs
	}
	ageMs := nowMs - updatedAt
	if ageMs < s.stallTimeoutSeconds*1000 {
		return
	}
	voted, _ := tally["voted"].([]string)
	votedSet := map[string]bool{}
	for _, name := range voted {
		votedSet[name] = true
	}
	var pending []string
	for _, name := range task.Reviewers() {
		if !votedSet[name] {
			pending = append(pending, name)
		}
	}
	s.escalate(ctx, task, RenderLeaderEscalationStall(task, ageMs/60000, voted, pending), "stall")
}

// dispatchToReviewer 通过生成一次性临时 reviewer harness 派发评审请求。
// harness 以 reviewer 基础规格运行，附带 verify_task + view_task，构建、
// 运行一次、释放——无票结束的运行由 followUpUnvotedReviewer 跟进。
// Python: _dispatch_to_reviewer (scheduler.py L756-764)。
func (s *TeamScheduler) dispatchToReviewer(ctx context.Context, reviewer string, task *database.TeamTaskBase) {
	logger.Info(logComponent).Str("reviewer", reviewer).
		Msg("[scheduler] spawning temp harness")
	go func() {
		voted := false
		if s.reviewerRunner != nil {
			voted = s.reviewerRunner(ctx, reviewer, task)
		} else {
			logger.Error(logComponent).Str("reviewer", reviewer).
				Msg("[scheduler] no reviewer runner injected; review dispatch skipped")
		}
		s.followUpUnvotedReviewer(ctx, reviewer, task, voted)
	}()
}

// followUpUnvotedReviewer 无票结束运行时重跑 reviewer，或升级 leader。
// 轮超时、harness 崩溃或模型从不调 verify_task 都会以无票结束。该轮的派发
// 标记若不释放会让 reviewer 永不再运行，任务停在评审直到停滞升级。每次无票
// 运行释放标记并重扫，每验收人每轮最多 maxReviewRunsPerRound 次；之后由
// leader 决断。
// Python: _follow_up_unvoted_reviewer (scheduler.py L1025-1083)。
func (s *TeamScheduler) followUpUnvotedReviewer(ctx context.Context, reviewer string, task *database.TeamTaskBase, voted bool) {
	current, err := s.taskMgr.Get(ctx, task.TaskID)
	if err != nil || current == nil ||
		current.Status != "in_review" || current.ReviewRound != task.ReviewRound {
		return
	}
	tally := s.taskMgr.GetReviewTally(current)
	votedSet := map[string]bool{}
	if voted, ok := tally["voted"].([]string); ok {
		for _, name := range voted {
			votedSet[name] = true
		}
	}
	if voted || votedSet[reviewer] {
		return
	}
	runKey := [3]string{task.TaskID, fmt.Sprintf("%d", task.ReviewRound), reviewer}
	runs := s.reviewRuns[runKey] + 1
	s.reviewRuns[runKey] = runs
	if runs < reviewRunsMaxReviewRunsPerRound {
		logger.Warn(logComponent).Str("reviewer", reviewer).
			Str("task_id", task.TaskID).Int64("round", task.ReviewRound).
			Int("run", runs).Int("max", reviewRunsMaxReviewRunsPerRound).
			Msg("[reviewer_no_vote] ended without a vote; dispatching again")
		delete(s.reviewDispatched, runKey)
		if s.active {
			_ = s.scan(ctx)
		}
		return
	}
	logger.Warn(logComponent).Str("reviewer", reviewer).
		Str("task_id", task.TaskID).Int64("round", task.ReviewRound).
		Int("runs", runs).
		Msg("[reviewer_no_vote] ended without a vote repeatedly; escalating")
	s.escalate(ctx, current, RenderLeaderEscalationNoVote(current, reviewer, int64(runs)),
		fmt.Sprintf("no_vote:%s", reviewer))
}

// failFeedbackOf 计票列取 fail_feedback 块。
func failFeedbackOf(tally map[string]any) map[string]string {
	if fb, ok := tally["fail_feedback"].(map[string]string); ok {
		return fb
	}
	return map[string]string{}
}

// intOfTally 计票列取整。
func intOfTally(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

// derefStr 字符串指针解引用。
func derefStr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// currentTimeMs 当前毫秒时间戳（停滞时长计算）。
func currentTimeMs() int64 {
	return database.GetCurrentTime()
}

// ──────────────────────────── 终态反馈聚合（25.2b） ────────────────────────────

// SetReviewFeedbackFinalizer 注入终态聚合回调（挂载 rail 的 FinalizeReviewFeedback；
// 装配点注入）。Python: _dispatch_team_review_feedback 的 rail 回调面。
func (s *TeamScheduler) SetReviewFeedbackFinalizer(finalizer func(ctx context.Context) (bool, error)) {
	s.reviewFeedbackFinalizer = finalizer
}
