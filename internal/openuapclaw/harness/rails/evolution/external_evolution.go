package evolution

// external_evolution.go 外部生产者驱动的演进入口（F_62 评审半区集成面）。
//
// Python: skill_evolution_rail.py L970-1025 evolve_from_external_signals 与
// team_skill_evolution_rail.py L244-296 的评审反馈挂点（2026-12-04 快照）。

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReviewFeedbackCoordinator 评审反馈协调器面（实现在 scheduling 包，经接口
// 注入规避 rail→scheduling import；Python 以 lazy import 直构造协调器）。
type ReviewFeedbackCoordinator interface {
	// HandleReviewFeedback 处理一条结算失败的评审反馈负载。
	// Python: ReviewFeedbackEvolutionCoordinator.__call__。
	HandleReviewFeedback(ctx context.Context, payload map[string]any) error
	// OnTeamCompleted 团队终态聚合（返回是否有实际演进动作）。
	// Python: ReviewFeedbackEvolutionCoordinator.on_team_completed。
	OnTeamCompleted(ctx context.Context, payload map[string]any) (bool, error)
}

// ExternalEvolutionResult 外部信号演进结果（文字状态面，对齐上游返回 dict 的
// status/message 形态）。
type ExternalEvolutionResult struct {
	SkillName string
	Status    string
	Message   string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EvolveFromExternalSignals 从已归因的外部信号演化一个常规技能。
// 有意绕过被动 signal_trigger 检测。调用方负责归因与证据策略；rail 仍校验
// 目标、经标准优化器生成记录并走正常审批/持久化路径。
//
// Python: evolve_from_external_signals (skill_evolution_rail.py L970-1025)。
func (r *SkillEvolutionRail) EvolveFromExternalSignals(ctx context.Context, signals []*signal.EvolutionSignal, messages []map[string]any, traj *trajectory.Trajectory, userQuery string, requiresApproval *bool) ExternalEvolutionResult {
	skillNames := map[string]bool{}
	for _, sig := range signals {
		if sig.SkillName != nil && *sig.SkillName != "" {
			skillNames[*sig.SkillName] = true
		}
	}
	if len(signals) == 0 || len(skillNames) != 1 {
		name := ""
		for skillName := range skillNames {
			name = skillName
			break
		}
		return ExternalEvolutionResult{
			SkillName: name,
			Status:    "skipped_no_input",
			Message:   "external evolution requires signals attributed to exactly one Skill",
		}
	}
	skillName := ""
	for name := range skillNames {
		skillName = name
		break
	}
	disabled := false
	for _, name := range r.disabledSkills {
		if name == skillName {
			disabled = true
			break
		}
	}
	if disabled {
		return ExternalEvolutionResult{
			SkillName: skillName,
			Status:    "skipped_skill_not_found",
			Message:   fmt.Sprintf("external evolution skipped because skill '%s' is disabled", skillName),
		}
	}
	if !r.evolutionStore.SkillExists(ctx, skillName) {
		return ExternalEvolutionResult{
			SkillName: skillName,
			Status:    "skipped_skill_not_found",
			Message:   fmt.Sprintf("external evolution skipped because regular skill '%s' does not exist", skillName),
		}
	}
	approvalRequired := !r.autoSave
	if requiresApproval != nil {
		approvalRequired = *requiresApproval
	}
	// 外部生产者（如多个任务评审回调）可能并发到达。与自动演化相同的
	// per-rail 门保证优化器调用与演进存储写入有序（Go 侧与被动扫描共用
	// scanMu 语义的 mutex）。
	_, err := r.handleEvolutionFromSignals(ctx, skillName, signals, messages, nil, userQuery, approvalRequired, false)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", skillName).
			Msg("[external evolution] handle failed")
		return ExternalEvolutionResult{SkillName: skillName, Status: "failed", Message: err.Error()}
	}
	return ExternalEvolutionResult{SkillName: skillName, Status: "attempted"}
}

// EvolveFromExternalSignals 团队 rail 的外部信号演进入口（与单 Agent 版同语义；
// Go 侧两 rail 为兄弟关系——Python 继承 SkillEvolutionRail，Go 侧重复入口，
// 已登记的兄弟结构决策）。
// Python: evolve_from_external_signals（经继承由 TeamSkillEvolutionRail 获得）。
func (r *TeamSkillEvolutionRail) EvolveFromExternalSignals(ctx context.Context, signals []*signal.EvolutionSignal, messages []map[string]any, traj *trajectory.Trajectory, userQuery string, requiresApproval *bool) ExternalEvolutionResult {
	skillNames := map[string]bool{}
	for _, sig := range signals {
		if sig.SkillName != nil && *sig.SkillName != "" {
			skillNames[*sig.SkillName] = true
		}
	}
	if len(signals) == 0 || len(skillNames) != 1 {
		name := ""
		for skillName := range skillNames {
			name = skillName
			break
		}
		return ExternalEvolutionResult{
			SkillName: name,
			Status:    "skipped_no_input",
			Message:   "external evolution requires signals attributed to exactly one Skill",
		}
	}
	skillName := ""
	for name := range skillNames {
		skillName = name
		break
	}
	if !r.evolutionStore.SkillExists(ctx, skillName) {
		return ExternalEvolutionResult{
			SkillName: skillName,
			Status:    "skipped_skill_not_found",
			Message:   fmt.Sprintf("external evolution skipped because regular skill '%s' does not exist", skillName),
		}
	}
	approvalRequired := !r.autoSave
	if requiresApproval != nil {
		approvalRequired = *requiresApproval
	}
	result, err := r.orchestrator.Evolve(
		ctx,
		skillName,
		convertSignalsToValues(signals),
		messages,
		userQuery,
		traj,
		approvalRequired,
		map[string]any{},
		nil,
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", skillName).
			Msg("[external evolution] team handle failed")
		return ExternalEvolutionResult{SkillName: skillName, Status: "failed", Message: err.Error()}
	}
	return ExternalEvolutionResult{SkillName: skillName, Status: string(result.Status), Message: result.Message}
}

// EvolveFromExternalSignals 面的窄化（协调器消费）。
// 两个 rail 都提供同签名方法——协调器经接口消费。
// ReviewRailLike 定义在 scheduling 包（消费方窄接口）。
