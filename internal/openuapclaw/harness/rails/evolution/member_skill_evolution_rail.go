package evolution

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	llmpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// member_skill_evolution_rail.go 成员域 Skill 演进（copy-on-write 隔离）。
// Python: openjiuwen/harness/rails/evolution/member_skill_evolution_rail.py（68 行 1:1）。

// ──────────────────────────── 结构体 ────────────────────────────

// MemberSkillEvolutionRail 成员 Skill rail：变更隔离到一个成员工作空间的常规 Skill rail。
// Python: MemberSkillEvolutionRail(SkillEvolutionRail)（member_skill_evolution_rail.py:18-31）。
// 偏差 D-15.24c-2（语言层）：Python 继承 override 两个入口方法；Go 无继承，
// 组合 SkillEvolutionRail 并提供同名包装方法（基座入口先确保私有拷贝再转发）。
type MemberSkillEvolutionRail struct {
	// SkillEvolutionRail 基座 rail
	*SkillEvolutionRail

	// memberSkillsDir 成员私库根
	// Python: self._member_skills_dir
	memberSkillsDir string
	// globalSkillsDir 全局库根
	// Python: self._global_skills_dir
	globalSkillsDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberSkillEvolutionRail 构造成员 Skill 演进 rail。
// Python: MemberSkillEvolutionRail.__init__（member_skill_evolution_rail.py:21-31）：
// 基座 kwargs 经 SkillEvolutionRailOption 透传。
func NewMemberSkillEvolutionRail(
	skillsDir []string,
	memberSkillsDir string,
	globalSkillsDir string,
	llmModel *llmpkg.Model,
	model string,
	language string,
	opts ...SkillEvolutionRailOption,
) *MemberSkillEvolutionRail {
	return &MemberSkillEvolutionRail{
		SkillEvolutionRail: NewSkillEvolutionRail(skillsDir, llmModel, model, language, opts...),
		memberSkillsDir:    memberSkillsDir,
		globalSkillsDir:    globalSkillsDir,
	}
}

// HandleEvolutionFromSignalsDetached 外部信号演进入口：变更前先确保目标为成员私有拷贝。
// Python: _handle_evolution_from_signals（member_skill_evolution_rail.py:33-44）；
// 基座签名透传（signals/messages/approval 语义与 skill_evolution_rail.go 同源）。
func (r *MemberSkillEvolutionRail) HandleEvolutionFromSignalsDetached(
	ctx context.Context,
	skillName string,
	signals []*signal.EvolutionSignal,
	messages []map[string]any,
	cbc *agentinterfaces.AgentCallbackContext,
	userQuery string,
	requiresApproval bool,
	emitHostEvents bool,
) (*experience.ExperienceApprovalRequest, error) {
	if _, err := r.ensurePrivateSkill(skillName); err != nil {
		return nil, err
	}
	// Python: return await super()._handle_evolution_from_signals(...)
	return r.SkillEvolutionRail.handleEvolutionFromSignals(
		ctx, skillName, signals, messages, cbc, userQuery, requiresApproval, emitHostEvents)
}

// EvolveSkillWithSharingDetached 自动/共享演进入口：变更前先确保目标为成员私有拷贝。
// Python: _evolve_skill_with_sharing（member_skill_evolution_rail.py:46-57）。
func (r *MemberSkillEvolutionRail) EvolveSkillWithSharingDetached(
	ctx context.Context,
	skillName string,
	skillSignals []*signal.EvolutionSignal,
	messages []map[string]any,
	sharedRecords []checkpointing.EvolutionRecord,
) bool {
	if _, err := r.ensurePrivateSkill(skillName); err != nil {
		return false
	}
	// Python: return await super()._evolve_skill_with_sharing(...)
	return r.SkillEvolutionRail.evolveSkillWithSharing(ctx, skillName, skillSignals, messages, sharedRecords)
}

// Init 透传基座初始化。
func (r *MemberSkillEvolutionRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	return r.SkillEvolutionRail.Init(ctx, agent)
}

// Uninit 透传基座注销。
func (r *MemberSkillEvolutionRail) Uninit(agent agentinterfaces.BaseAgent) error {
	return r.SkillEvolutionRail.Uninit(agent)
}

// ReviewRuntime 透传基座审查运行时访问器（供审批栈装配）。
func (r *MemberSkillEvolutionRail) ReviewRuntime() *review.EvolutionReviewRuntime {
	return r.SkillEvolutionRail.ReviewRuntime()
}

// SnapshotForEvolution 透传基座。
func (r *MemberSkillEvolutionRail) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	return r.SkillEvolutionRail.SnapshotForEvolution(ctx, traj, cbc)
}

// RunEvolution 透传基座。
func (r *MemberSkillEvolutionRail) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	return r.SkillEvolutionRail.RunEvolution(ctx, traj, snapshot)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensurePrivateSkill 物化成员私有 Skill 拷贝。
// Python: _ensure_private_skill（member_skill_evolution_rail.py:59-65）：
// asyncio.to_thread(ensure_member_skill_copy, ...) —— 调用方本就 await，Go 侧同步调用。
func (r *MemberSkillEvolutionRail) ensurePrivateSkill(skillName string) (string, error) {
	return EnsureMemberSkillCopy(r.memberSkillsDir, r.globalSkillsDir, skillName)
}
