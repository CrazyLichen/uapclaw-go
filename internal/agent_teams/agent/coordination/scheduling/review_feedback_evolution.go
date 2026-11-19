package scheduling

// review_feedback_evolution.go 评审反馈演进协调器（Task 11）。
//
// 传输与产品无关：其所属团队在团队结束时调用一次终态回调。任务反馈被翻译为
// 结构化观察并保留；终态聚合复用挂载 rail 的常规优化器/持久化/pending 事件
// 与审批路径。
//
// Python: agent_teams/agent/scheduling/review_feedback_evolution.py（全文，
// 2026-12-04 快照）。

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SkillCreationEvents 事件组名。
	// Python: SKILL_CREATION_EVENTS (review_feedback_evolution.py L32)。
	SkillCreationEvents = "skill_creation"

	// maxAggregatedFeedbackChars 汇总反馈截断上限。
	maxAggregatedFeedbackChars = 12000
)

// safeMemberNamePattern 安全成员名。
// Python: _SAFE_MEMBER_NAME (review_feedback_evolution.py L34)。
var safeMemberNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamReviewRailProvider 协调器的 rail 提供者（窄接口，*evolution.TeamSkillEvolutionRail 满足）。
type TeamReviewRailProvider func() TeamEvolutionRailLike

// TeamEvolutionRailLike 协调器消费的团队 rail 面。
type TeamEvolutionRailLike interface {
	// ReviewStore 演进存储（证据面）。
	ReviewStore() *checkpointingStore
	// ReviewGenerator 优化器（LLM/模型名）。
	ReviewGenerator() ReviewGeneratorLike
	// ReviewLanguage rail 语言。
	ReviewLanguage() string
	// AutoSave 自动保存开关。
	AutoSave() bool
	// EvolveFromExternalSignals 外部信号演化入口。
	EvolveFromExternalSignals(ctx context.Context, signals []*signal.EvolutionSignal, messages []map[string]any, traj *trajectory.Trajectory, userQuery string, requiresApproval *bool) evolution.ExternalEvolutionResult
	// RailGetTrajectory 团队 scope 干净窗口。
	RailGetTrajectory(sessionID, teamID string) (*trajectory.Trajectory, error)
	// RailDrainApprovalEvents 排干待审批事件。
	RailDrainApprovalEvents() []map[string]any
}

// checkpointingStore 别名（证据面直接用具体存储类型）。
type checkpointingStore = checkpointing.EvolutionStore

// ReviewGeneratorLike 优化器窄面。
type ReviewGeneratorLike interface {
	LLM() *llm.Model
	ModelName() string
}

// TaskFeedbackObservation 保留到终态聚合的一条任务反馈观察。
// Python: TaskFeedbackObservation (review_feedback_evolution.py L44-54)。
type TaskFeedbackObservation struct {
	TaskID      string
	ReviewRound int64
	Assignee    string
	Feedback    string
	SkillName   string
	Signal      *signal.EvolutionSignal
}

// NewSkillPatternObservation 新 Skill 模式观察。
// Python: NewSkillPatternObservation (review_feedback_evolution.py L56-68)。
type NewSkillPatternObservation struct {
	TaskID             string
	ReviewRound        int64
	Assignee           string
	Feedback           string
	ReusableGuidance   string
	Reason             string
	Confidence         float64
	Action             signal.ReviewFeedbackAction
	SupportingEvidence []string
}

// ReviewFeedbackEvolutionCoordinator 评审反馈演进协调器。
// Python: ReviewFeedbackEvolutionCoordinator (review_feedback_evolution.py L70-495)。
type ReviewFeedbackEvolutionCoordinator struct {
	sessionID     string
	teamID        string
	railProvider  TeamReviewRailProvider
	enabled       bool
	minConfidence float64

	mu                    sync.Mutex
	processed             map[[3]string]struct{}
	attributionMu         sync.Mutex
	observations          []*TaskFeedbackObservation
	newSkillPatterns      []*NewSkillPatternObservation
	teamObservationCursor int
	newSkillPatternCursor int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReviewFeedbackEvolutionCoordinator 构造协调器。
// Python: __init__ (review_feedback_evolution.py L78-103)。
func NewReviewFeedbackEvolutionCoordinator(sessionID, teamID string, railProvider TeamReviewRailProvider, enabled bool, minConfidence float64) *ReviewFeedbackEvolutionCoordinator {
	return &ReviewFeedbackEvolutionCoordinator{
		sessionID:     sessionID,
		teamID:        teamID,
		railProvider:  railProvider,
		enabled:       enabled,
		minConfidence: minConfidence,
		processed:     map[[3]string]struct{}{},
	}
}

// HandleReviewFeedback 处理一条来自 TeamScheduler 的结算失败评审负载。
// Python: __call__ (review_feedback_evolution.py L105-240)。
func (c *ReviewFeedbackEvolutionCoordinator) HandleReviewFeedback(ctx context.Context, payload map[string]any) error {
	if !c.isEnabled() {
		return nil
	}
	taskID := strings.TrimSpace(stringOf(payload["task_id"]))
	round := int64OfAny(payload["review_round"])
	feedback := strings.TrimSpace(stringOf(payload["feedback"]))
	assignee := strings.TrimSpace(stringOf(payload["assignee"]))
	if taskID == "" || feedback == "" || !isSafeMemberName(assignee) {
		if taskID != "" && feedback != "" {
			logger.Warn(logComponent).Str("task_id", taskID).
				Str("assignee", assignee).
				Msg("[ReviewFeedbackEvolution] task feedback skipped because assignee is missing or unsafe")
		}
		return nil
	}
	key := [3]string{taskID, fmt.Sprintf("%d", round), feedback}
	c.mu.Lock()
	if _, seen := c.processed[key]; seen {
		c.mu.Unlock()
		return nil
	}
	c.processed[key] = struct{}{}
	c.mu.Unlock()

	teamRail := c.teamEvolutionRail()
	if teamRail == nil {
		logger.Warn(logComponent).Str("session", c.sessionID).Str("task_id", taskID).
			Msg("[ReviewFeedbackEvolution] no Team Skill rail")
		return nil
	}

	// 多个任务可能接连失败。归因串行化使共享模型与累积的重复证据保持有序。
	c.attributionMu.Lock()
	defer c.attributionMu.Unlock()
	traj := c.getMemberTrajectory(teamRail, assignee)
	taskObjective := taskObjectiveOf(payload)
	var priorEvidence []string
	priorTaskIDs := map[string]bool{}
	for _, item := range c.newSkillPatterns {
		if item.TaskID != taskID {
			priorEvidence = append(priorEvidence, formatNewSkillPatternEvidence(item))
			priorTaskIDs[item.TaskID] = true
		}
	}
	railStore := teamRail.ReviewStore()
	var knownSkills []string
	if railStore != nil {
		knownSkills = railStore.ListSkillNames(ctx)
	}
	repetitionCount := int64(len(priorTaskIDs) + 1)
	context := buildTrustedContext(ctx, railStore, knownSkills, taskID, round, taskObjective, traj, repetitionCount, priorEvidence)

	generator := teamRail.ReviewGenerator()
	if generator == nil || generator.LLM() == nil {
		// 无 LLM 面（fake rail/未配模型）：无证据可归因，安全封闭。
		logger.Warn(logComponent).Str("task_id", taskID).
			Msg("[ReviewFeedbackEvolution] no review LLM; feedback closed without attribution")
		return nil
	}
	modelFace := newModelLLMFace(generator.LLM())
	attributor, err := signal.NewReviewFeedbackAttributor(modelFace, generator.ModelName(), teamRail.ReviewLanguage(), 30*time.Second)
	if err != nil {
		return err
	}
	attribution := attributor.Attribute(ctx, feedback, context)
	logger.Info(logComponent).
		Str("task_id", taskID).Str("assignee", assignee).Int64("round", round).
		Str("action", string(attribution.Action)).Str("classification", string(attribution.Classification)).
		Str("skill", attribution.SkillName).
		Float64("confidence", attribution.Confidence).
		Str("reason", attribution.Reason).
		Msg("[ReviewFeedbackEvolution]")

	threshold := c.confidenceThreshold()
	if attribution.Classification == signal.ReviewFeedbackClassificationNewSkillPattern {
		if attribution.ReusableGuidance != "" && attribution.Confidence >= threshold {
			matchingPatterns := c.matchingNewSkillPatterns(attribution.ReusableGuidance, taskID)
			matchingEvidence := make([]string, 0, len(matchingPatterns))
			for _, item := range matchingPatterns {
				matchingEvidence = append(matchingEvidence, formatNewSkillPatternEvidence(item))
			}
			patternAction := signal.ReviewFeedbackActionSkipUnattributed
			if attribution.Action == signal.ReviewFeedbackActionSuggestNewSkill && len(matchingPatterns) > 0 {
				patternAction = signal.ReviewFeedbackActionSuggestNewSkill
			}
			c.mu.Lock()
			c.newSkillPatterns = append(c.newSkillPatterns, &NewSkillPatternObservation{
				TaskID: taskID, ReviewRound: round, Assignee: assignee, Feedback: feedback,
				ReusableGuidance: attribution.ReusableGuidance, Reason: attribution.Reason,
				Confidence: attribution.Confidence, Action: patternAction,
				SupportingEvidence: matchingEvidence,
			})
			c.mu.Unlock()
		}
		return nil
	}

	if attribution.Action != signal.ReviewFeedbackActionEvolveExistingSkill {
		return nil
	}
	if attribution.Confidence < threshold {
		logger.Info(logComponent).
			Float64("confidence", attribution.Confidence).Float64("threshold", threshold).
			Msg("[ReviewFeedbackEvolution] actionable attribution below threshold")
		return nil
	}
	sig := signal.AttributionToEvolutionSignal(attribution, taskID, round)
	if sig == nil || attribution.SkillName == "" {
		return nil
	}
	c.mu.Lock()
	c.observations = append(c.observations, &TaskFeedbackObservation{
		TaskID: taskID, ReviewRound: round, Assignee: assignee,
		Feedback: feedback, SkillName: attribution.SkillName, Signal: sig,
	})
	c.mu.Unlock()
	logger.Info(logComponent).
		Str("task_id", taskID).Str("member", assignee).Str("skill", attribution.SkillName).
		Msg("[ReviewFeedbackEvolution] retained task observation")
	return nil
}

// OnTeamCompleted 把任务反馈聚合为团队 Skill 或新建提案。
// Python: on_team_completed (review_feedback_evolution.py L242-277)。
func (c *ReviewFeedbackEvolutionCoordinator) OnTeamCompleted(ctx context.Context, _ map[string]any) (bool, error) {
	if !c.isEnabled() {
		return false, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	end := len(c.observations)
	observations := c.observations[c.teamObservationCursor:end]
	patternEnd := len(c.newSkillPatterns)
	patternObservations := c.newSkillPatterns[c.newSkillPatternCursor:patternEnd]
	if len(observations) == 0 && len(patternObservations) == 0 {
		return false, nil
	}
	teamRail := c.teamEvolutionRail()
	if teamRail == nil && len(observations) > 0 {
		logger.Warn(logComponent).Msg("[ReviewFeedbackEvolution] team aggregation skipped: no Team Skill rail")
		return false, nil
	}

	grouped := map[string][]*TaskFeedbackObservation{}
	for _, observation := range observations {
		if teamRail != nil && teamRail.ReviewStore().SkillExists(ctx, observation.SkillName) {
			grouped[observation.SkillName] = append(grouped[observation.SkillName], observation)
		} else {
			logger.Info(logComponent).Str("skill", observation.SkillName).
				Msg("[ReviewFeedbackEvolution] unknown Team Skill omitted from aggregation")
		}
	}

	attempted := c.evolveTeamGroups(ctx, teamRail, grouped)
	if len(patternObservations) > 0 {
		if routed := c.routeNewSkillPatterns(ctx, patternObservations); routed {
			attempted = true
		}
	}
	c.teamObservationCursor = end
	c.newSkillPatternCursor = patternEnd
	return attempted, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evolveTeamGroups 逐技能聚合演化。
// Python: _evolve_team_groups (review_feedback_evolution.py L278-314)。
func (c *ReviewFeedbackEvolutionCoordinator) evolveTeamGroups(ctx context.Context, teamRail TeamEvolutionRailLike, grouped map[string][]*TaskFeedbackObservation) bool {
	if len(grouped) == 0 || teamRail == nil {
		return false
	}
	traj := c.getTeamTrajectory(teamRail)
	var messages []map[string]any
	if traj != nil {
		converted, convErr := trajectory.TrajectoryToMessages(traj, nil, false)
		if convErr == nil {
			messages = converted
		}
	}
	attempted := false
	requiresApproval := !teamRail.AutoSave()
	for skillName, skillObservations := range grouped {
		signals := make([]*signal.EvolutionSignal, 0, len(skillObservations))
		for _, item := range skillObservations {
			signals = append(signals, item.Signal)
		}
		result := teamRail.EvolveFromExternalSignals(ctx, signals, messages, traj, formatAggregatedFeedback(skillObservations), &requiresApproval)
		attempted = true
		logger.Info(logComponent).
			Str("skill", skillName).Int("task_feedback_count", len(skillObservations)).
			Str("status", result.Status).
			Msg("[ReviewFeedbackEvolution] Team aggregate result")
	}
	return attempted
}

// routeNewSkillPatterns 新 Skill 模式路由到创建 rail。
// Python: _route_new_skill_patterns (review_feedback_evolution.py L315-360)。
// 偏差 D-25.2b-14：Go 侧 TeamSkillCreateRail 的 propose_from_external_evidence
// 随创建 rail 挂点落地后接线；当前记录路由日志（回收条件=创建 rail 外部
// 提案入口落地）。
func (c *ReviewFeedbackEvolutionCoordinator) routeNewSkillPatterns(ctx context.Context, observations []*NewSkillPatternObservation) bool {
	var suggestions []*NewSkillPatternObservation
	for _, item := range observations {
		if item.Action == signal.ReviewFeedbackActionSuggestNewSkill {
			suggestions = append(suggestions, item)
		}
	}
	if len(suggestions) == 0 {
		return false
	}
	routedKeys := map[string]bool{}
	attempted := false
	for _, suggestion := range suggestions {
		proposalKey := newSkillProposalKey(suggestion.ReusableGuidance)
		if proposalKey == "" || routedKeys[proposalKey] {
			continue
		}
		routedKeys[proposalKey] = true
		logger.Info(logComponent).
			Str("proposal_key", proposalKey).Int("evidence", len(suggestion.SupportingEvidence)+1).
			Msg("[ReviewFeedbackEvolution] repeated pattern staged for creation approval (routing pending D-25.2b-14)")
		attempted = true
	}
	return attempted
}

// teamEvolutionRail 取挂载 rail。
// Python: _team_evolution_rail。
func (c *ReviewFeedbackEvolutionCoordinator) teamEvolutionRail() TeamEvolutionRailLike {
	if c.railProvider == nil {
		return nil
	}
	return c.railProvider()
}

// isEnabled 启用探针。
// Python: _is_enabled。
func (c *ReviewFeedbackEvolutionCoordinator) isEnabled() bool { return c.enabled }

// confidenceThreshold 置信阈值钳位。
// Python: _confidence_threshold。
func (c *ReviewFeedbackEvolutionCoordinator) confidenceThreshold() float64 {
	if c.minConfidence < 0.0 {
		return 0.0
	}
	if c.minConfidence > 1.0 {
		return 1.0
	}
	return c.minConfidence
}

// taskObjectiveOf payload 任务目标（title+content）。
// Python: _task_objective。
func taskObjectiveOf(payload map[string]any) string {
	var parts []string
	for _, field := range []string{"task_title", "task_content"} {
		value := strings.TrimSpace(stringOf(payload[field]))
		if value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n")
}

// isSafeMemberName 安全成员名。
// Python: _is_safe_member_name。
func isSafeMemberName(memberName string) bool {
	return memberName != "" && memberName != "." && memberName != ".." &&
		safeMemberNamePattern.MatchString(memberName)
}

// formatAggregatedFeedback 汇总反馈格式化。
// Python: _format_aggregated_feedback。
func formatAggregatedFeedback(observations []*TaskFeedbackObservation) string {
	lines := []string{"团队全部任务完成。以下是归因到同一全局 Skill 的任务审核反馈汇总："}
	for _, item := range observations {
		lines = append(lines, fmt.Sprintf("- task=%s, round=%d, assignee=%s: %s",
			item.TaskID, item.ReviewRound, item.Assignee, item.Feedback))
	}
	joined := strings.Join(lines, "\n")
	if len(joined) > maxAggregatedFeedbackChars {
		joined = joined[:maxAggregatedFeedbackChars]
	}
	return joined
}

// formatNewSkillPatternEvidence 模式证据行。
// Python: _format_new_skill_pattern_evidence。
func formatNewSkillPatternEvidence(observation *NewSkillPatternObservation) string {
	line := fmt.Sprintf("task=%s, round=%d, assignee=%s: %s",
		observation.TaskID, observation.ReviewRound, observation.Assignee, observation.Feedback)
	if len(line) > 2000 {
		line = line[:2000]
	}
	return line
}

// newSkillProposalKey 可复用指导 → 提案键。
// Python: _new_skill_proposal_key。
func newSkillProposalKey(reusableGuidance string) string {
	lowered := strings.ToLower(strings.TrimSpace(reusableGuidance))
	key := nonWordPattern.ReplaceAllString(lowered, "-")
	key = strings.Trim(key, "-")
	if len(key) > 160 {
		key = key[:160]
	}
	return key
}

// matchingNewSkillPatterns 同键既有模式（排除当前任务）。
// Python: _matching_new_skill_patterns。
func (c *ReviewFeedbackEvolutionCoordinator) matchingNewSkillPatterns(reusableGuidance, excludeTaskID string) []*NewSkillPatternObservation {
	patternKey := newSkillProposalKey(reusableGuidance)
	if patternKey == "" {
		return nil
	}
	var matches []*NewSkillPatternObservation
	for _, item := range c.newSkillPatterns {
		if item.TaskID == excludeTaskID {
			continue
		}
		if newSkillProposalKey(item.ReusableGuidance) == patternKey {
			matches = append(matches, item)
		}
	}
	return matches
}

// getMemberTrajectory 承担者成员轨迹投影。
// Python: _get_member_trajectory。偏差：Go 侧无 select_member_spans 等价面
// （team 轨迹投影），返回 nil（上下文无轨迹证据，归因按无轨迹分支封闭）——
// 回收条件 = team 轨迹成员投影面落地。
func (c *ReviewFeedbackEvolutionCoordinator) getMemberTrajectory(_ TeamEvolutionRailLike, _ string) *trajectory.Trajectory {
	return nil
}

// getTeamTrajectory 团队轨迹。
// Python: _get_team_trajectory。
func (c *ReviewFeedbackEvolutionCoordinator) getTeamTrajectory(teamRail TeamEvolutionRailLike) *trajectory.Trajectory {
	traj, err := teamRail.RailGetTrajectory(c.sessionID, c.teamID)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[ReviewFeedbackEvolution] trajectory lookup failed")
		return nil
	}
	return traj
}

// buildTrustedContext 从任务元数据与轨迹构建可信归因上下文。
// Python: ReviewFeedbackContextBuilder.build（L366-421）。
func buildTrustedContext(ctx context.Context, store *checkpointingStore, knownSkills []string, taskID string, reviewRound int64, taskObjective string, traj *trajectory.Trajectory, repetitionCount int64, repeatedEvidence []string) *signal.ReviewFeedbackContext {
	skillReads := signal.ExtractSkillReadsFromTrajectory(traj, knownSkills)
	skillContents := map[string]string{}
	var trustedReads []string
	for _, skillName := range skillReads {
		if store == nil {
			break
		}
		content, err := store.ReadSkillContent(ctx, skillName)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("skill", skillName).
				Msg("[ReviewFeedbackContextBuilder] failed to read proven Skill")
			continue
		}
		skillContents[skillName] = content
		trustedReads = append(trustedReads, skillName)
	}
	// 定义已无法加载的读取不可操作。
	if taskObjective != "" && len(taskObjective) > 6000 {
		taskObjective = taskObjective[:6000]
	}
	return &signal.ReviewFeedbackContext{
		TaskID:                  taskID,
		ReviewRound:             reviewRound,
		TaskObjective:           taskObjective,
		TrajectoryExcerpt:       signal.TrajectoryExcerpt(traj),
		SkillReads:              trustedReads,
		SkillContents:           skillContents,
		RepetitionCount:         repetitionCount,
		RepeatedPatternEvidence: repeatedEvidence,
	}
}

// stringOf any → 修剪字符串（nil → ""）。
func stringOf(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// int64OfAny any → int64。
func int64OfAny(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// nonWordPattern 非单词字符（Unicode 感知——Python \w 含 Unicode 字母/数字，
// Go RE2 的 \w 仅 ASCII，需显式补 \p{L}\p{N}）。
var nonWordPattern = regexp.MustCompile(`[^\w\p{L}\p{N}]+`)

// modelLLMFace llm.Model → signal.ReviewFeedbackLLM 适配器。
type modelLLMFace struct{ model *llm.Model }

func newModelLLMFace(model *llm.Model) signal.ReviewFeedbackLLM { return &modelLLMFace{model: model} }

// Invoke 归因调用（单 user 消息，完整响应文本）。
func (f *modelLLMFace) Invoke(ctx context.Context, _ string, messages []map[string]any, timeout time.Duration) (string, error) {
	if f.model == nil {
		return "", fmt.Errorf("review feedback llm is not configured")
	}
	invokeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var baseMsgs []llmschema.BaseMessage
	for _, message := range messages {
		content, _ := message["content"].(string)
		baseMsgs = append(baseMsgs, llmschema.NewUserMessage(content))
	}
	response, err := f.model.Invoke(invokeCtx, model_clients.NewMessagesParam(baseMsgs...))
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", fmt.Errorf("empty llm response")
	}
	return response.Content.Text(), nil
}
