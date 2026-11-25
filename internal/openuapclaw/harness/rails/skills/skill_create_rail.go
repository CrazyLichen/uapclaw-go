package skills

import (
	"context"
	"strings"

	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
)

// logComponent 日志组件（skills rail 系）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skillCreatePriority 技能创建轨道优先级
	// 对齐 Python: SkillCreateRail.priority = 85
	skillCreatePriority = 85
	// autoSkillCreationFollowUpTag 运行时插入的自检 follow-up 包裹 tag
	// Python: _AUTO_SKILL_CREATION_FOLLOW_UP_TAG（skill_create_rail.py:24）
	autoSkillCreationFollowUpTag = "auto_skill_creation_followup"
)

// normalReplyCreationActionTerms 普通回复中"创建动作"判定词表。
// Python: _NORMAL_REPLY_CREATION_ACTION_TERMS（skill_create_rail.py:25-31）。
var normalReplyCreationActionTerms = []string{"创建", "沉淀", "create", "creation", "capture"}

// normalReplySkillTerms 普通回复中"技能"判定词表。
// Python: _NORMAL_REPLY_SKILL_TERMS（skill_create_rail.py:32）。
var normalReplySkillTerms = []string{"skill", "技能"}

// skillCreationFollowUpCN 运行时插入的 Skill 创建自检 follow-up（中文）。
// Python: _SKILL_CREATION_FOLLOW_UP_CN（skill_create_rail.py:34-39，逐字拷贝）。
const skillCreationFollowUpCN = "这是运行时插入的 Skill 创建自检，不是用户的新需求。\n" +
	"参考常驻“技能沉淀自检”规则，只判断本轮是否形成可复用方法，不重新判断运行时触发门槛。\n" +
	"如需建议，只在普通最终回复末尾追加一至两句，并同时包含可复用方法和是否创建 Skill 的确认问题；" +
	"否则自然回复，不提本提醒或内部判断。"

// skillCreationFollowUpEN 运行时插入的 Skill 创建自检 follow-up（英文）。
// Python: _SKILL_CREATION_FOLLOW_UP_EN（skill_create_rail.py:41-47，逐字拷贝）。
const skillCreationFollowUpEN = "This runtime-inserted Skill creation self-check is not a new user request.\n" +
	"Refer to the standing \"Skill Capture Self-Check\" rules and judge only whether this round produced a reusable " +
	"method; do not re-evaluate the runtime trigger threshold.\n" +
	"If suggesting, append only one or two sentences to the normal final reply and include both the reusable method " +
	"and the Skill creation question; otherwise reply naturally without mentioning this reminder or internal judgment."

// ──────────────────────────── 结构体 ────────────────────────────

// SkillCreateRailOption 构造选项函数。
type SkillCreateRailOption func(*SkillCreateRail)

// SkillCreateRail 单 Agent 技能创建护栏（08-13 新版：确定性信号检测驱动）。
// 注入常驻技能创建引导段；出现强执行信号时经 TaskLoopController 入队一次保守的
// 自检 follow-up（带水位线与复提示快照，不重复提醒）。
// 与 TeamSkillCreateRail 的区别：不依赖团队完成信号，纯窗口阈值驱动。
// 对齐 Python: openjiuwen/harness/rails/skills/skill_create_rail.py::SkillCreateRail（302 行新版）
type SkillCreateRail struct {
	*evolution.EvolutionRail

	// skillsDir 技能根目录（记录用；Python __init__ 持有 _skills_dir）
	skillsDir string
	// language 语言（"cn" 或 "en"）
	language string
	// autoTrigger 是否自动检测触发
	autoTrigger bool
	// trajectorySpanProcessor 轨迹 span 处理器（②：构造注入，Python 必传参数）
	trajectorySpanProcessor *trajectory.TrajectorySpanProcessor
	// followUpSent 本 invoke 内是否已入队 follow-up（before_invoke 重置）
	// Python: self._follow_up_sent
	followUpSent bool
	// skillToolSeenThisInvoke 本 invoke 内是否已使用 skill_tool（cover 信号短路）
	// Python: self._skill_tool_seen_this_invoke
	skillToolSeenThisInvoke bool
	// lastFollowedToolCallCounts 会话 → follow-up 后的原始工具调用水位线
	// Python: self._last_followed_tool_call_counts
	lastFollowedToolCallCounts map[string]int
	// lastPromptedToolTotals 会话 → 已提示时的全程有效 (iterations, calls) 快照
	// Python: self._last_prompted_tool_totals
	lastPromptedToolTotals map[string][2]int
	// currentTrajectory 最近一次迭代捕获的轨迹
	// Python: self._trajectory
	currentTrajectory *trajectory.Trajectory
	// signalDetector 确定性信号检测器
	// Python: self._signal_detector = SkillCreationSignalDetector()
	signalDetector signal.SkillCreationSignalDetector
	// systemPromptBuilder init 捕获的系统提示词构建器
	// Python: self._system_prompt_builder
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
}

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译期验证 SkillCreateRail 满足 evolution.EvolutionExtension 接口
var _ evolution.EvolutionExtension = (*SkillCreateRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillCreateRail 创建单 Agent 技能创建护栏。
// 对齐 Python: SkillCreateRail.__init__（skill_create_rail.py:59-80）：
// evolution_trigger=NONE、trajectory_span_processor 必传（②）、无 v1 阈值参数（⑦）。
func NewSkillCreateRail(skillsDir string, opts ...SkillCreateRailOption) *SkillCreateRail {
	r := &SkillCreateRail{
		skillsDir:                  skillsDir,
		language:                   "cn",
		autoTrigger:                true,
		lastFollowedToolCallCounts: make(map[string]int),
		lastPromptedToolTotals:     make(map[string][2]int),
	}
	// evolution_trigger=NONE：不自动触发演化，只在迭代回调里做信号检测
	r.EvolutionRail = evolution.NewEvolutionRail(r, evolution.WithEvolutionTrigger(evolution.TriggerNone))
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithSkillCreateAutoTrigger 设置是否自动检测触发。
func WithSkillCreateAutoTrigger(auto bool) SkillCreateRailOption {
	return func(r *SkillCreateRail) { r.autoTrigger = auto }
}

// WithSkillCreateLanguage 设置语言。
func WithSkillCreateLanguage(lang string) SkillCreateRailOption {
	return func(r *SkillCreateRail) { r.language = lang }
}

// WithSkillCreateTrajectorySpanProcessor 注入轨迹 span 处理器。
// ②：对齐 Python 构造必传 trajectory_span_processor（skill_create_rail.py:63）。
func WithSkillCreateTrajectorySpanProcessor(p *trajectory.TrajectorySpanProcessor) SkillCreateRailOption {
	return func(r *SkillCreateRail) { r.trajectorySpanProcessor = p }
}

// Priority 返回轨道优先级。
func (r *SkillCreateRail) Priority() int { return skillCreatePriority }

// AutoTrigger 返回是否自动检测触发。
func (r *SkillCreateRail) AutoTrigger() bool { return r.autoTrigger }

// Init 捕获 agent 系统提示词构建器。
// 对齐 Python: init（skill_create_rail.py:82-84）。
func (r *SkillCreateRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent != nil {
		r.systemPromptBuilder = agent.SystemPromptBuilder()
	}
	return nil
}

// Uninit 移除本 rail 持有的提示词段并复位状态。
// 对齐 Python: uninit（skill_create_rail.py:86-95）。
func (r *SkillCreateRail) Uninit(agent agentinterfaces.BaseAgent) error {
	_ = agent
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionNameSkillCreationGuidance)
	}
	r.systemPromptBuilder = nil
	r.currentTrajectory = nil
	return r.EvolutionRail.Uninit(agent)
}

// GetCallbacks 注册 BeforeModelCall（prompt 引导注入）。
// Python: before_model_call 为 rail 钩子；Go 经 GetCallbacks 显式注册（ProgressiveToolRail 先例）。
func (r *SkillCreateRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.EvolutionRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// BeforeModelCall 注入常驻技能创建引导段。
// 对齐 Python: before_model_call（skill_create_rail.py:97-104）。
func (r *SkillCreateRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	builder := r.getPromptBuilder(cbc)
	if builder == nil {
		return nil
	}
	language := r.language
	if b, ok := builder.(interface{ Language() string }); ok {
		if lang := b.Language(); lang != "" {
			language = lang
		}
	}
	builder.AddSection(sections.BuildSkillCreationGuidanceSection(language))
	return nil
}

// OnBeforeInvoke 重置本 invoke 的 follow-up 状态。
// 对齐 Python: _on_before_invoke（skill_create_rail.py:106-111）。
func (r *SkillCreateRail) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.followUpSent = false
	r.skillToolSeenThisInvoke = false
	r.currentTrajectory = nil
	return nil
}

// OnAfterTaskIteration 捕获轨迹并在确定性强信号出现时入队一次性 follow-up。
// 对齐 Python: _on_after_task_iteration（skill_create_rail.py:113-121）。
// 偏差（语言层）：Python 由回调机制传入 trajectory 参数；Go 接口无此参数，
// 从 Builder 构建当前轨迹（与 team_skill_create_rail.go 同款读面）。
func (r *SkillCreateRail) OnAfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc == nil {
		return nil
	}
	if builder := r.Builder(); builder != nil {
		if traj, err := builder.Build(); err == nil {
			r.currentTrajectory = traj
		}
	}
	r.maybeEnqueueCreationFollowUp(cbc)
	return nil
}

// OnAfterModelCall 空实现。
func (r *SkillCreateRail) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 空实现。
func (r *SkillCreateRail) OnAfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterInvoke 空实现（Python 仅在 after_task_iteration 检测）。
func (r *SkillCreateRail) OnAfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterEvolutionTriggered 空实现。
func (r *SkillCreateRail) OnAfterEvolutionTriggered(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AllowEvolutionTrigger 始终返回 true（NONE 模式下不会被自动触发）。
func (r *SkillCreateRail) AllowEvolutionTrigger(_ evolution.EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	return true
}

// SnapshotForEvolution 返回 nil（本 rail 不参与演化快照）。
func (r *SkillCreateRail) SnapshotForEvolution(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *evolution.EvolutionSnapshot {
	return nil
}

// RunEvolution 空实现。
func (r *SkillCreateRail) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *evolution.EvolutionSnapshot) error {
	return nil
}

// GetEvolutionTotalTimeoutSecs 返回 0（无后台演化）。
func (r *SkillCreateRail) GetEvolutionTotalTimeoutSecs() float64 { return 0 }

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybeEnqueueCreationFollowUp 阈值与运行时门通过时入队自检 follow-up。
// 对齐 Python: _maybe_enqueue_creation_follow_up（skill_create_rail.py:123-153）。
func (r *SkillCreateRail) maybeEnqueueCreationFollowUp(cbc *agentinterfaces.AgentCallbackContext) bool {
	sig := r.shouldEnqueueCreationFollowUp(cbc)
	if sig == nil {
		return false
	}

	if r.assistantOutputProposesSkillCreation(cbc) {
		r.followUpSent = true
		r.recordPromptedToolTotals(sig.Metrics)
		r.refreshFollowUpWatermark(sig.Metrics)
		logger.Info(logComponent).
			Str("session_id", r.currentSessionID()).
			Msg("[SkillCreateRail] skill creation follow-up skipped: normal reply already proposed creation")
		return false
	}

	var ctl loopController
	if cbc != nil && cbc.Agent() != nil {
		ctl = getLoopController(cbc.Agent())
	}
	if ctl == nil {
		logger.Info(logComponent).
			Msg("[SkillCreateRail] skill creation follow-up skipped: no task loop controller")
		return false
	}

	ctl.EnqueueFollowUp(r.buildFollowUpPrompt())
	r.followUpSent = true
	r.recordPromptedToolTotals(sig.Metrics)
	logger.Info(logComponent).
		Str("session_id", r.currentSessionID()).
		Msg("[SkillCreateRail] skill creation follow-up enqueued")
	return true
}

// shouldEnqueueCreationFollowUp 返回可提示信号（无则 nil）。
// 对齐 Python: _should_enqueue_creation_follow_up（skill_create_rail.py:157-214）。
func (r *SkillCreateRail) shouldEnqueueCreationFollowUp(cbc *agentinterfaces.AgentCallbackContext) *signal.SkillCreationSignal {
	if !r.autoTrigger {
		logger.Debug(logComponent).
			Msg("[SkillCreateRail] skill creation follow-up skipped: auto trigger disabled")
		return nil
	}
	if r.currentTrajectory == nil {
		logger.Debug(logComponent).
			Msg("[SkillCreateRail] skill creation follow-up skipped: no trajectory")
		return nil
	}

	sessionID := r.currentSessionID()
	rawToolCallWatermark := r.lastFollowedToolCallCounts[sessionID]
	metrics := r.signalDetector.CollectMetrics(r.currentTrajectory, rawToolCallWatermark)
	if r.followUpBlockedByContext(cbc) {
		r.refreshFollowUpWatermark(metrics)
		logger.Debug(logComponent).
			Msg("[SkillCreateRail] skill creation follow-up skipped: blocked context")
		return nil
	}

	signals := r.signalDetector.Detect(
		r.currentTrajectory,
		rawToolCallWatermark,
		promptedSnapshotOf(r.lastPromptedToolTotals, sessionID),
		&metrics,
	)
	var coverSignal *signal.SkillCreationSignal
	var promptSignal *signal.SkillCreationSignal
	for i := range signals {
		switch signals[i].SignalType {
		case signal.SkillCreationSignalSkillToolCover:
			s := signals[i]
			coverSignal = &s
		case signal.SkillCreationSignalPromptEligible:
			s := signals[i]
			promptSignal = &s
		}
	}
	if coverSignal != nil {
		r.skillToolSeenThisInvoke = true
	}
	if r.skillToolSeenThisInvoke {
		r.refreshFollowUpWatermark(metrics)
		logger.Debug(logComponent).
			Msg("[SkillCreateRail] skill creation follow-up skipped: skill tool already used")
		return nil
	}

	if r.followUpSent {
		logger.Debug(logComponent).
			Msg("[SkillCreateRail] skill creation follow-up skipped: already sent in this invoke")
		return nil
	}

	if promptSignal != nil {
		logger.Debug(logComponent).
			Str("reason", promptSignal.Reason).
			Int("window_effective_tool_calling_iterations", promptSignal.Metrics.WindowEffectiveToolCallingIterations).
			Int("window_effective_tool_calls", promptSignal.Metrics.WindowEffectiveToolCalls).
			Msg("[SkillCreateRail] skill creation follow-up threshold met")
		return promptSignal
	}
	logger.Debug(logComponent).
		Int("window_effective_tool_calling_iterations", metrics.WindowEffectiveToolCallingIterations).
		Int("window_effective_tool_calls", metrics.WindowEffectiveToolCalls).
		Msg("[SkillCreateRail] skill creation follow-up skipped: threshold not met")
	return nil
}

// followUpBlockedByContext 非用户运行轮或 follow-up 轮时抑制 follow-up。
// 对齐 Python: _follow_up_blocked_by_context（skill_create_rail.py:224-238）：
// extra.is_follow_up / inputs.is_follow_up 双来源 + run_kind ∈ {background,heartbeat,cron}。
// 偏差（语言层）：Go 的 TaskIterationInputs 结构化承载 IsFollowUp/RunKind，
// 语义与 Python 的 duck-typing getattr 链等价。
func (r *SkillCreateRail) followUpBlockedByContext(cbc *agentinterfaces.AgentCallbackContext) bool {
	if cbc == nil {
		return false
	}
	// Python: extra.get("is_follow_up", False)
	if extra := cbc.Extra(); extra != nil {
		if v, ok := extra["is_follow_up"].(bool); ok && v {
			return true
		}
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil {
		return false
	}
	if inputs.IsFollowUp {
		return true
	}
	runKind := strings.TrimSpace(strings.ToLower(string(inputs.RunKind)))
	switch runKind {
	case "background", "heartbeat", "cron":
		return true
	}
	return false
}

// buildFollowUpPrompt 按语言选择文案并加 tag 包裹。
// 对齐 Python: _build_follow_up_prompt（skill_create_rail.py:240-243）。
func (r *SkillCreateRail) buildFollowUpPrompt() string {
	if strings.HasPrefix(strings.ToLower(r.language), "en") {
		return wrapFollowUpPrompt(skillCreationFollowUpEN)
	}
	return wrapFollowUpPrompt(skillCreationFollowUpCN)
}

// wrapFollowUpPrompt 用 <auto_skill_creation_followup> tag 包裹文案。
// Python: _wrap_follow_up_prompt（skill_create_rail.py:245-247）。
func wrapFollowUpPrompt(prompt string) string {
	return "<" + autoSkillCreationFollowUpTag + ">\n" + prompt + "\n</" + autoSkillCreationFollowUpTag + ">"
}

// assistantOutputProposesSkillCreation 普通回复已提议创建 Skill 时返回 true。
// 对齐 Python: _assistant_output_proposes_skill_creation（skill_create_rail.py:250-258）。
func (r *SkillCreateRail) assistantOutputProposesSkillCreation(cbc *agentinterfaces.AgentCallbackContext) bool {
	output := extractAssistantOutput(cbc)
	if output == "" {
		return false
	}
	normalized := strings.ToLower(output)
	hasAction := false
	for _, term := range normalReplyCreationActionTerms {
		if strings.Contains(normalized, strings.ToLower(term)) {
			hasAction = true
			break
		}
	}
	hasSkill := false
	for _, term := range normalReplySkillTerms {
		if strings.Contains(normalized, strings.ToLower(term)) {
			hasSkill = true
			break
		}
	}
	return hasAction && hasSkill
}

// extractAssistantOutput 从迭代结果中提取可见助手输出。
// 对齐 Python: _extract_assistant_output（skill_create_rail.py:261-277）。
func extractAssistantOutput(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil {
		return ""
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs)
	if !ok || inputs == nil || inputs.Result == nil {
		return ""
	}
	for _, key := range []string{"output", "message", "content", "text", "response"} {
		value, present := inputs.Result[key]
		if !present {
			continue
		}
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case map[string]any:
			if content, ok := v["content"].(string); ok && strings.TrimSpace(content) != "" {
				return strings.TrimSpace(content)
			}
		}
	}
	return ""
}

// currentSessionID 返回当前轨迹会话 id（无轨迹或空则空串）。
// 对齐 Python: _current_session_id（skill_create_rail.py:279-280）。
func (r *SkillCreateRail) currentSessionID() string {
	if r.currentTrajectory == nil {
		return ""
	}
	if sid := r.currentTrajectory.SessionID(); sid != nil {
		return *sid
	}
	return ""
}

// refreshFollowUpWatermark 将会话水位线推进到指标快照。
// 对齐 Python: _refresh_follow_up_watermark（skill_create_rail.py:282-285）。
func (r *SkillCreateRail) refreshFollowUpWatermark(metrics signal.SkillCreationWindowMetrics) {
	r.lastFollowedToolCallCounts[r.currentSessionID()] = metrics.TotalRawToolCalls
}

// recordPromptedToolTotals 记录已提示时的全程有效总量（不消耗统计窗口）。
// 对齐 Python: _record_prompted_tool_totals（skill_create_rail.py:287-292）。
func (r *SkillCreateRail) recordPromptedToolTotals(metrics signal.SkillCreationWindowMetrics) {
	r.lastPromptedToolTotals[r.currentSessionID()] = [2]int{
		metrics.TotalEffectiveToolCallingIterations,
		metrics.TotalEffectiveToolCalls,
	}
}

// getPromptBuilder 优先取 init 捕获的构建器，缺省回退 agent。
// 对齐 Python: _get_prompt_builder（skill_create_rail.py:294-299）。
func (r *SkillCreateRail) getPromptBuilder(cbc *agentinterfaces.AgentCallbackContext) saprompt.SystemPromptBuilderInterface {
	if r.systemPromptBuilder != nil {
		return r.systemPromptBuilder
	}
	if cbc == nil || cbc.Agent() == nil {
		return nil
	}
	r.systemPromptBuilder = cbc.Agent().SystemPromptBuilder()
	return r.systemPromptBuilder
}

// promptedSnapshotOf 取会话的复提示快照指针（无记录则 nil）。
func promptedSnapshotOf(m map[string][2]int, sessionID string) *[2]int {
	if v, ok := m[sessionID]; ok {
		snapshot := v
		return &snapshot
	}
	return nil
}
