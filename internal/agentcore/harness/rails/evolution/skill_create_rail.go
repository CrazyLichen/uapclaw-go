package evolution

import (
	"context"
	"strings"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillCreateRailOption 构造选项函数。
type SkillCreateRailOption func(*SkillCreateRail)

// SkillCreateRail 单 Agent 技能创建护栏。
//
// 在任务循环每次迭代后统计本轮轨迹的工具调用总数与种类数，达阈值时通过
// TaskLoopController 注入一条 follow-up，请模型调用 skill-creator 创建新技能。
// 与 TeamSkillCreateRail 的区别：不依赖团队完成信号，纯阈值驱动。
//
// 对齐 Python: openjiuwen/harness/rails/skills/skill_create_rail.py::SkillCreateRail
type SkillCreateRail struct {
	*EvolutionRail

	// skillsDir 技能根目录（写入 follow-up 文案）
	skillsDir string
	// language 语言（"cn" 或 "en"）
	language string
	// autoTrigger 是否自动检测触发
	autoTrigger bool
	// toolCallThreshold 工具调用总数阈值
	toolCallThreshold int
	// toolDiversityThreshold 工具种类阈值
	toolDiversityThreshold int
	// proposalSent 本轮是否已提议（before_invoke 重置）
	proposalSent bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skillCreatePriority 技能创建轨道优先级
	// 对齐 Python: SkillCreateRail.priority = 85
	skillCreatePriority = 85
	// defaultToolCallThreshold 默认工具调用总数阈值
	defaultToolCallThreshold = 10
	// defaultToolDiversityThreshold 默认工具种类阈值
	defaultToolDiversityThreshold = 5
)

// skillCreatePromptCN 中文确认提示词（一比一复刻 Python _FOLLOW_UP_PROMPT_CN）
const skillCreatePromptCN = "**重要：你必须先向用户确认，，不可跳过此步骤。**\n" +
	"系统检测到对话中存在可复用模式，可能值得创建新技能。请按以下步骤执行：\n" +
	"1. 直接询问或调用 ask_user 工具向用户确认：\n" +
	"   - 问题：\"我检测到您可能值得创建一个新技能。是否创建？\"\n" +
	"   - 选项：[\"创建\"，\"跳过\"，\"自定义指令：（请描述需求）\"]\n" +
	"2. 如果用户选择\"创建\"或提供了自定义指令，请调用 **skill-creator** 技能，" +
	"根据用户的要求和当前对话上下文执行技能创建。\n" +
	"   新技能应保存到技能目录：{skills_dir}"

// skillCreatePromptEN 英文确认提示词（一比一复刻 Python _FOLLOW_UP_PROMPT_EN）
const skillCreatePromptEN = "**Important: You MUST confirm with the user first. Do not skip this step.**\n" +
	"The system detected a reusable pattern that may be worth creating as a new skill. " +
	"Please follow these steps:\n" +
	"1. Directly inquire or invoke the `ask_user` tool to confirm with the user:\n" +
	"   - Question: \"I detected a pattern that may be worth creating as a new skill. Create it?\"\n" +
	"   - Options: [\"Create\", \"Skip\", \"Custom instruction: (describe your needs)\"]\n" +
	"2. If user chooses \"Create\" or provides a custom instruction, invoke the **skill-creator** skill " +
	"to execute the skill creation.\n" +
	"   Save the new skill to: {skills_dir}"

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译期验证 SkillCreateRail 满足 EvolutionExtension 接口
var _ EvolutionExtension = (*SkillCreateRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillCreateRail 创建单 Agent 技能创建护栏。
// 对齐 Python: SkillCreateRail(skills_dir, language="cn", auto_trigger=True,
// tool_call_threshold=10, tool_diversity_threshold=5)
func NewSkillCreateRail(skillsDir string, opts ...SkillCreateRailOption) *SkillCreateRail {
	r := &SkillCreateRail{
		skillsDir:              skillsDir,
		language:               "cn",
		autoTrigger:            true,
		toolCallThreshold:      defaultToolCallThreshold,
		toolDiversityThreshold: defaultToolDiversityThreshold,
	}
	// evolution_trigger=NONE：不自动触发演化，只在迭代回调里做阈值检测
	r.EvolutionRail = NewEvolutionRail(r, WithEvolutionTrigger(TriggerNone))
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

// WithSkillCreateThresholds 设置工具调用总数与种类阈值。
func WithSkillCreateThresholds(calls, unique int) SkillCreateRailOption {
	return func(r *SkillCreateRail) {
		r.toolCallThreshold = calls
		r.toolDiversityThreshold = unique
	}
}

// Priority 返回轨道优先级。
func (r *SkillCreateRail) Priority() int { return skillCreatePriority }

// AutoTrigger 返回是否自动检测触发。
func (r *SkillCreateRail) AutoTrigger() bool { return r.autoTrigger }

// OnBeforeInvoke 重置本轮提议标记。
func (r *SkillCreateRail) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.proposalSent = false
	return nil
}

// OnAfterTaskIteration 任务迭代后做阈值检测并提议。
func (r *SkillCreateRail) OnAfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.maybePropose(cbc)
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
func (r *SkillCreateRail) AllowEvolutionTrigger(_ EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	return true
}

// SnapshotForEvolution 返回 nil（本 rail 不参与演化快照）。
func (r *SkillCreateRail) SnapshotForEvolution(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	return nil
}

// RunEvolution 空实现。
func (r *SkillCreateRail) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *EvolutionSnapshot) error {
	return nil
}

// GetEvolutionTotalTimeoutSecs 返回 0（无后台演化）。
func (r *SkillCreateRail) GetEvolutionTotalTimeoutSecs() float64 { return 0 }

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybePropose 阈值检测 → 通过 LoopController 注入 follow-up。
// 对齐 Python: _on_after_task_iteration(ctx)
func (r *SkillCreateRail) maybePropose(cbc *agentinterfaces.AgentCallbackContext) {
	if !r.autoTrigger || r.proposalSent {
		return
	}

	builder := r.Builder()
	if builder == nil {
		logger.Debug(logComponent).Msg("SkillCreateRail trajectory builder is nil, skipping")
		return
	}

	total, unique := countToolCalls(builder.GetSteps())
	if total < r.toolCallThreshold || unique < r.toolDiversityThreshold {
		logger.Debug(logComponent).
			Int("total_calls", total).
			Int("unique_tools", unique).
			Int("call_threshold", r.toolCallThreshold).
			Int("diversity_threshold", r.toolDiversityThreshold).
			Msg("SkillCreateRail below threshold, skipping")
		return
	}

	if cbc == nil || cbc.Agent() == nil {
		return
	}
	ctl := getLoopController(cbc.Agent())
	if ctl == nil {
		logger.Warn(logComponent).
			Msg("skill creation proposal dropped: no TaskLoopController available (task-loop mode required)")
		return
	}

	prompt := r.buildPrompt()
	logger.Info(logComponent).
		Str("language", r.language).
		Str("skills_dir", r.skillsDir).
		Int("prompt_length", len(prompt)).
		Int("total_calls", total).
		Int("unique_tools", unique).
		Msg("SkillCreateRail threshold met, enqueuing follow_up")
	ctl.EnqueueFollowUp(prompt)
	r.proposalSent = true
}

// countToolCalls 统计工具调用总数与去重后的种类数。
// 对齐 Python: _should_propose_new_skill()
func countToolCalls(steps []*trajectory.TrajectoryStep) (total int, unique int) {
	seen := make(map[string]bool)
	for _, step := range steps {
		if step == nil || step.Kind != trajectory.StepKindTool {
			continue
		}
		detail, ok := step.Detail.(*trajectory.ToolCallDetail)
		if !ok || detail == nil || detail.ToolName == "" {
			continue
		}
		total++
		seen[detail.ToolName] = true
	}
	return total, len(seen)
}

// buildPrompt 按语言选择文案并替换技能目录。
// 对齐 Python: prompt = _FOLLOW_UP_PROMPT_CN/EN.format(skills_dir=self._skills_dir)
func (r *SkillCreateRail) buildPrompt() string {
	if r.language == "cn" {
		return strings.ReplaceAll(skillCreatePromptCN, "{skills_dir}", r.skillsDir)
	}
	return strings.ReplaceAll(skillCreatePromptEN, "{skills_dir}", r.skillsDir)
}
