package evolution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillCreateRail 团队技能创建护栏，在检测到多 Agent 协作模式后
// 提议用户创建团队技能。
//
// 嵌入 EvolutionRail 基类（evolution_trigger=NONE），不自动触发演化，
// 而是在 after_task_iteration / after_invoke 中检测 spawn_member 调用次数，
// 达到阈值后通过 LoopController enqueue follow_up 请求用户确认。
//
// 与 TeamSkillEvolutionRail 为兄弟关系：
// TeamSkillEvolutionRail 负责已有团队技能的演进优化，
// TeamSkillCreateRail 负责检测是否应创建新的团队技能。
//
// 对齐 Python: openjiuwen/harness/rails/skills/team_skill_create_rail.py
type TeamSkillCreateRail struct {
	*EvolutionRail

	// skillsDir 技能目录路径
	skillsDir string
	// autoTrigger 是否自动检测触发
	autoTrigger bool
	// minTeamMembers 创建阈值（spawn_member 调用最低次数）
	minTeamMembers int
	// language 语言（"cn" 或 "en"）
	language string

	// completedSessionID 已标记完成的会话 ID
	completedSessionID string
	// proposedSpawnCounts 已提议的 spawn 计数（防重复）
	proposedSpawnCounts map[string]int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamSkillCreatePriority 团队技能创建轨道优先级
	// 对齐 Python: TeamSkillCreateRail.priority = 85
	teamSkillCreatePriority = 85

	// defaultMinTeamMembers 默认 spawn_member 最低次数阈值
	defaultMinTeamMembers = 2
)

// followUpPromptCN 中文确认提示词
// 对齐 Python: _FOLLOW_UP_PROMPT_CN
const followUpPromptCN = "**重要：你必须先向用户确认，不可跳过此步骤。**\n" +
	"系统检测到对话中 spawn 了多个团队成员，可能值得创建团队技能。请按以下步骤执行：\n" +
	"1. 直接询问或调用 ask_user 工具向用户确认：\n" +
	"   - 问题：\"我检测到多 Agent 协作模式可能值得创建为团队技能。是否创建？\"\n" +
	"   - 选项：[\"创建\"，\"跳过\"，\"自定义指令：（请描述需求）\"]\n" +
	"2. 如果用户选择\"创建\"或提供了自定义指令，请调用 **team-skill-creator** 技能，" +
	"根据用户的要求和当前对话上下文执行团队技能创建。\n" +
	"   新技能应保存到技能目录：{skills_dir}"

// followUpPromptEN 英文确认提示词
// 对齐 Python: _FOLLOW_UP_PROMPT_EN
const followUpPromptEN = "**Important: You MUST confirm with the user first. Do not skip this step.**\n" +
	"The system detected multiple team member spawns that may be worth creating as a Team Skill. " +
	"Please follow these steps:\n" +
	"1. Directly inquire or invoke the `ask_user` tool to confirm with the user:\n" +
	"   - Question: \"I detected a multi-agent collaboration pattern that may be worth creating " +
	"as a Team Skill. Create it?\"\n" +
	"   - Options: [\"Create\", \"Skip\", \"Custom instruction: (describe your needs)\"]\n" +
	"2. If user chooses \"Create\" or provides a custom instruction, invoke the **team-skill-creator** skill " +
	"to execute the team skill creation.\n" +
	"   Save the new skill to: {skills_dir}"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSkillCreateRail 创建团队技能创建护栏实例。
//
// 对齐 Python: TeamSkillCreateRail(skills_dir, language="cn", auto_trigger=True, min_team_members_for_create=2)
func NewTeamSkillCreateRail(
	skillsDir string,
	opts ...TeamSkillCreateRailOption,
) *TeamSkillCreateRail {
	r := &TeamSkillCreateRail{
		skillsDir:           skillsDir,
		autoTrigger:         true,
		minTeamMembers:      defaultMinTeamMembers,
		language:            "cn",
		proposedSpawnCounts: make(map[string]int),
	}

	// 创建 EvolutionRail 基类（evolution_trigger=NONE）
	r.EvolutionRail = NewEvolutionRail(r,
		WithEvolutionTrigger(TriggerNone),
	)

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Priority 返回轨道优先级。
// 对齐 Python: TeamSkillCreateRail.priority = 85
func (r *TeamSkillCreateRail) Priority() int { return teamSkillCreatePriority }

// NotifyTeamCompleted 标记当前 invoke 可在后续生命周期边界提议团队技能创建。
//
// 对齐 Python: TeamSkillCreateRail.notify_team_completed()
func (r *TeamSkillCreateRail) NotifyTeamCompleted(cbc *agentinterfaces.AgentCallbackContext) bool {
	if !r.autoTrigger {
		logger.Info(logComponent).Msg("TeamSkillCreateRail notify_team_completed 已忽略：autoTrigger 关闭")
		return false
	}

	builder := r.Builder()
	if builder == nil {
		logger.Warn(logComponent).Msg("TeamSkillCreateRail notify_team_completed: 无轨迹 builder")
		return false
	}

	r.completedSessionID = builder.SessionID()
	logger.Debug(logComponent).
		Str("session_id", r.completedSessionID).
		Msg("TeamSkillCreateRail 标记团队完成")
	return true
}

// ──────────────────────────── EvolutionExtension 接口实现 ────────────────────────────

// OnAfterTaskIteration 任务迭代后检测是否应提议创建。
// 对齐 Python: _on_after_task_iteration(ctx)
func (r *TeamSkillCreateRail) OnAfterTaskIteration(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.maybeEnqueueCreationFollowUp(cbc)
	return nil
}

// OnAfterInvoke invoke 结束后也检测。
// 对齐 Python: _on_after_invoke(ctx)
func (r *TeamSkillCreateRail) OnAfterInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.maybeEnqueueCreationFollowUp(cbc)
	return nil
}

// AllowEvolutionTrigger 始终返回 true（NONE 模式下不会被自动触发）。
func (r *TeamSkillCreateRail) AllowEvolutionTrigger(_ EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	return true
}

// SnapshotForEvolution 返回基本快照。
func (r *TeamSkillCreateRail) SnapshotForEvolution(_ context.Context, traj *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	messages := collectMessagesFromTrajectory(traj)
	return &EvolutionSnapshot{Trajectory: traj, Messages: messages}
}

// RunEvolution 空实现（TeamSkillCreateRail 不使用演化逻辑）。
func (r *TeamSkillCreateRail) RunEvolution(_ context.Context, _ *trajectory.Trajectory, _ *EvolutionSnapshot) error {
	return nil
}

// GetEvolutionTotalTimeoutSecs 返回 0（不限时）。
func (r *TeamSkillCreateRail) GetEvolutionTotalTimeoutSecs() float64 { return 0 }

// OnBeforeInvoke 空实现。
func (r *TeamSkillCreateRail) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterModelCall 空实现。
func (r *TeamSkillCreateRail) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 空实现。
func (r *TeamSkillCreateRail) OnAfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterEvolutionTriggered 空实现。
func (r *TeamSkillCreateRail) OnAfterEvolutionTriggered(_ context.Context, _ *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// ──────────────────────────── TeamSkillCreateRailOption ────────────────────────────

// TeamSkillCreateRailOption 构造选项函数。
type TeamSkillCreateRailOption func(*TeamSkillCreateRail)

// WithTeamSkillCreateAutoTrigger 设置是否自动触发。
func WithTeamSkillCreateAutoTrigger(auto bool) TeamSkillCreateRailOption {
	return func(r *TeamSkillCreateRail) { r.autoTrigger = auto }
}

// WithTeamSkillCreateMinTeamMembers 设置 spawn_member 最低阈值。
func WithTeamSkillCreateMinTeamMembers(min int) TeamSkillCreateRailOption {
	return func(r *TeamSkillCreateRail) { r.minTeamMembers = min }
}

// WithTeamSkillCreateLanguage 设置语言。
func WithTeamSkillCreateLanguage(lang string) TeamSkillCreateRailOption {
	return func(r *TeamSkillCreateRail) { r.language = lang }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybeEnqueueCreationFollowUp 检测并提议团队技能创建。
// 对齐 Python: _maybe_enqueue_creation_follow_up(ctx)
func (r *TeamSkillCreateRail) maybeEnqueueCreationFollowUp(cbc *agentinterfaces.AgentCallbackContext) {
	builder := r.Builder()
	sessionID := ""
	if builder != nil {
		sessionID = builder.SessionID()
	}
	spawnCount := r.countSpawnMemberCalls()

	if !r.canEnqueueCreationFollowUp(sessionID, spawnCount) {
		return
	}

	// 获取 LoopController（对齐 Python: controller = getattr(agent, "_loop_controller", None)）
	if cbc == nil || cbc.Agent() == nil {
		return
	}
	// LoopController 通过类型断言获取
	controller := getLoopController(cbc.Agent())
	if controller == nil {
		logger.Warn(logComponent).Msg("TeamSkillCreateRail 无法获取 LoopController，跳过提议")
		return
	}

	prompt := r.buildFollowUpPrompt()
	logger.Info(logComponent).
		Str("language", r.language).
		Str("skills_dir", r.skillsDir).
		Int("prompt_length", len(prompt)).
		Msg("TeamSkillCreateRail 检测到多 Agent 协作模式，提议创建团队技能")

	controller.EnqueueFollowUp(prompt)
	r.proposedSpawnCounts[sessionID] = spawnCount
	if r.completedSessionID == sessionID {
		r.completedSessionID = ""
	}
	logger.Info(logComponent).Msg("TeamSkillCreateRail follow_up 已入队")
}

// canEnqueueCreationFollowUp 检查是否满足提议条件。
// 对齐 Python: _can_enqueue_creation_follow_up()
func (r *TeamSkillCreateRail) canEnqueueCreationFollowUp(sessionID string, spawnCount int) bool {
	if !r.autoTrigger || sessionID == "" || r.completedSessionID != sessionID {
		return false
	}
	if spawnCount <= r.proposedSpawnCounts[sessionID] {
		return false
	}
	if spawnCount < r.minTeamMembers {
		logger.Debug(logComponent).
			Int("spawn_count", spawnCount).
			Int("threshold", r.minTeamMembers).
			Msg("TeamSkillCreateRail spawn_member 低于阈值，跳过")
		return false
	}
	return true
}

// buildFollowUpPrompt 构建确认提示词。
// 对齐 Python: _build_follow_up_prompt()
func (r *TeamSkillCreateRail) buildFollowUpPrompt() string {
	if r.language == "cn" {
		return strings.ReplaceAll(followUpPromptCN, "{skills_dir}", r.skillsDir)
	}
	return strings.ReplaceAll(followUpPromptEN, "{skills_dir}", r.skillsDir)
}

// countSpawnMemberCalls 计算 spawn_member 工具调用次数。
// 对齐 Python: _count_spawn_member_calls()
func (r *TeamSkillCreateRail) countSpawnMemberCalls() int {
	builder := r.Builder()
	if builder == nil {
		return 0
	}

	count := 0
	for _, step := range builder.GetSteps() {
		if step.Kind == trajectory.StepKindTool && step.Detail != nil {
			if toolDetail, ok := step.Detail.(*trajectory.ToolCallDetail); ok {
				if strings.Contains(toolDetail.ToolName, "spawn_member") {
					count++
				}
			}
		}
	}
	return count
}

// knownTeamSkillNames 列出已有团队技能名称。
// 对齐 Python: _known_team_skill_names()
func (r *TeamSkillCreateRail) knownTeamSkillNames() map[string]bool {
	names := make(map[string]bool)
	root := r.skillsDir
	if root == "" {
		return names
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return names
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(root, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			continue
		}
		// 简单检查 frontmatter 中的 kind 字段
		if containsTeamSkillKind(string(data)) {
			names[entry.Name()] = true
		}
	}
	return names
}

// containsTeamSkillKind 检查 SKILL.md 内容是否包含团队技能 kind。
func containsTeamSkillKind(content string) bool {
	for kind := range teamSkillKinds {
		if strings.Contains(content, fmt.Sprintf("kind: %s", kind)) ||
			strings.Contains(content, fmt.Sprintf("kind: \"%s\"", kind)) {
			return true
		}
	}
	return false
}

// loopController LoopController 接口子集。
type loopController interface {
	EnqueueFollowUp(prompt string)
}

// getLoopController 从 Agent 获取 LoopController。
func getLoopController(agent agentinterfaces.BaseAgent) loopController {
	if agent == nil {
		return nil
	}
	// 尝试类型断言获取 LoopController
	type loopControllerProvider interface {
		LoopController() loopController
	}
	if provider, ok := agent.(loopControllerProvider); ok {
		return provider.LoopController()
	}
	return nil
}

// compile-time 接口断言
var _ EvolutionExtension = (*TeamSkillCreateRail)(nil)
