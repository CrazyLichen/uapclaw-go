// 技能演进 rail 的 configure / unconfigure 装配 API。
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/configuration.py
// （skill 半区 1:1 移植；15.24a Task 9——D-2 裁决 configuration.py 523 行
// 整缺挂本批）。
//
// 已知偏差（spec §4）：
//   - TTSE 半区（configure_ttse_evolution / configure_ttse_evolution_runtime /
//     unconfigure_ttse_evolution）⤴️ 24.5——TTSERail 类型未移植，工厂缝无法
//     表达引用（plan Step 9.2 裁决）。
//   - _validate_evolution_rail_config 的 signal_trigger / review_trigger 校验
//     ⤴️ Task 10——Go rail 侧字段随 D-15.21-1 新版对照回收落地。
//   - approval_submission_service 缝 ⤴️ 15.24b（ExperienceSubmissionService）。

package evolution

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// ──────────────────────────── 结构体 ────────────────────────────

// approvalBinding 绑定活跃演化审批中断所需的依赖。
// Python: _ApprovalBinding dataclass。
type approvalBinding struct {
	// runtime 审查运行时
	runtime *review.EvolutionReviewRuntime
	// submissionService 提交服务
	submissionService EvolutionSubmissionService
	// autoSave 是否自动保存
	autoSave bool
	// language 语言
	language string
}

// evolutionRailCarrier Skill/Team 两个演进 rail 载体的公共 API 面。
// Python: configuration.py 经 rail 公共属性（review_runtime /
// approval_submission_service）鸭子访问。
type evolutionRailCarrier interface {
	ReviewRuntime() *review.EvolutionReviewRuntime
	ApprovalSubmissionService() EvolutionSubmissionService
	AsAgentRail() agentinterfaces.AgentRail
}

// EvolutionConfigurableAgent configure/unconfigure 操作的 agent 最小能力面。
// Python 侧 agent 为鸭子类型（DeepAgent 形态）；Go 以窄接口表达
// （"DeepAgent 用来调用别人"方向的依赖，规则 10.2 允许）。
type EvolutionConfigurableAgent interface {
	// FindRailsByType 按类型查找排队或已注册的 rail
	FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail
	// AddRail 添加 rail（排队）
	AddRail(rail agentinterfaces.AgentRail)
	// StripRailsByType 按类型移除 rail，返回移除数
	StripRailsByType(railTypes ...reflect.Type) int
	// PendingRails 排队中 rail 快照（runtime 变体用）
	PendingRails() []agentinterfaces.AgentRail
	// RemovePendingRail 从排队队列摘除 rail
	RemovePendingRail(rail agentinterfaces.AgentRail)
	// RegisterRail 运行时注册 rail
	RegisterRail(ctx context.Context, rail agentinterfaces.AgentRail) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ConfigureSkillEvolution 在 agent 上装配技能演进 rail 栈。
// 幂等：同配置重复调用为 no-op；已配置 agent 换配置报错；
// 跨模式（regular/team）切换也报错。
// Python: configure_skill_evolution（configuration.py:44-113）。
func ConfigureSkillEvolution(
	agent EvolutionConfigurableAgent,
	skillsDir []string,
	llmModel *llm.Model,
	model string,
	trajectorySpanProcessor *trajectory.TrajectorySpanProcessor,
	team bool,
	reviewRuntime *review.EvolutionReviewRuntime,
	autoSave bool,
	language string,
	signalTrigger bool,
	reviewTrigger bool,
	disabledSkills []string,
) error {
	existingRegular := findExistingSkillEvolutionRail(agent)
	existingTeam := findExistingTeamSkillEvolutionRail(agent)
	if err := ensureNoCrossModeConflict(team, existingRegular != nil, existingTeam != nil); err != nil {
		return err
	}

	existingInterrupt := findExistingInterruptRail(agent)

	var targetCarrier evolutionRailCarrier
	var targetBase *EvolutionRail
	if team {
		if existingTeam != nil {
			targetCarrier = existingTeam
			targetBase = existingTeam.EvolutionRail
		}
	} else if existingRegular != nil {
		targetCarrier = existingRegular
		targetBase = existingRegular.EvolutionRail
	}
	runtime := reviewRuntime
	if runtime == nil {
		runtime = reviewRuntimeFromRail(targetBase, targetCarrier)
	}
	if runtime == nil {
		// Python: EvolutionReviewRuntime()（缺省 submitted_scope_retention_secs=60.0）
		runtime = review.NewEvolutionReviewRuntime(60.0)
	}

	var carrier evolutionRailCarrier
	created := false
	switch {
	case team && existingTeam != nil:
		if err := validateEvolutionRailConfig(existingTeam.EvolutionRail, autoSave, language, trajectorySpanProcessor, signalTrigger, reviewTrigger); err != nil {
			return err
		}
		carrier = existingTeam
	case !team && existingRegular != nil:
		if err := validateEvolutionRailConfig(existingRegular.EvolutionRail, autoSave, language, trajectorySpanProcessor, signalTrigger, reviewTrigger); err != nil {
			return err
		}
		carrier = existingRegular
	case team:
		// Python: TeamSkillEvolutionRail(..., **rail_kwargs)（configuration.py:241-254）
		// ——signal_trigger/review_trigger 经 rail_kwargs 全量转发进构造器。
		carrier = NewTeamSkillEvolutionRail(skillsDir, llmModel, model, language,
			WithTeamEvolutionReviewRuntime(runtime),
			WithTeamEvolutionTrajectorySpanProcessor(trajectorySpanProcessor),
			WithTeamSkillAutoSave(autoSave),
			WithTeamSkillSignalTrigger(signalTrigger),
			WithTeamSkillReviewTrigger(reviewTrigger))
		created = true
	default:
		// Python: SkillEvolutionRail(..., **rail_kwargs)（:257-266）——同上全量转发。
		carrier = NewSkillEvolutionRail(skillsDir, llmModel, model, language,
			WithEvolutionReviewRuntime(runtime),
			WithEvolutionTrajectorySpanProcessor(trajectorySpanProcessor),
			WithAutoSave(autoSave),
			WithSignalTrigger(signalTrigger),
			WithReviewTrigger(reviewTrigger),
			WithDisabledSkillsSet(disabledSkills))
		// rail_kwargs 其余成员（two_stage/subject_kind/review_interval/
		// review_agent_max_iterations）：无生产调用方传参（Python 全仓仅
		// examples 传 signal/review_trigger）；two_stage 依赖 optimizer 双段
		// 管线（⤴️ 独立批），落地时随批补形参转发。
		created = true
	}

	binding := approvalBinding{
		runtime:           carrier.ReviewRuntime(),
		submissionService: carrier.ApprovalSubmissionService(),
		autoSave:          autoSave,
		language:          language,
	}
	interruptRail, interruptCreated := getOrCreateOrBindInterruptRail(existingInterrupt, binding)

	// Python: _add_new_rail_stack(agent, interrupt_rail=... if interrupt_created else None, ...)
	if interruptCreated {
		agent.AddRail(interruptRail)
	}
	if created {
		agent.AddRail(carrier.AsAgentRail())
	}
	return nil
}

// ConfigureSkillEvolutionRuntime 装配技能演进 rail 并立即注册本次新增的 rail。
// Python: configure_skill_evolution_runtime（configuration.py:115-141）。
func ConfigureSkillEvolutionRuntime(
	agent EvolutionConfigurableAgent,
	skillsDir []string,
	llmModel *llm.Model,
	model string,
	trajectorySpanProcessor *trajectory.TrajectorySpanProcessor,
	team bool,
	reviewRuntime *review.EvolutionReviewRuntime,
	autoSave bool,
	language string,
	signalTrigger bool,
	reviewTrigger bool,
	disabledSkills []string,
) error {
	// Python: before_pending = _pending_rail_identities(agent)
	beforePending := pendingRailIdentities(agent)
	if err := ConfigureSkillEvolution(agent, skillsDir, llmModel, model,
		trajectorySpanProcessor, team, reviewRuntime, autoSave, language, signalTrigger, reviewTrigger, disabledSkills); err != nil {
		return err
	}

	// Python: new_rails = [rail for rail in _pending_rails(agent) if id(rail) not in before_pending]
	// 注册顺序满足演化依赖（中断 rail 先于演化 rail）。
	newRails := pendingRailsExcluding(agent, beforePending)
	for _, rail := range sortRailsByRuntimeRegistrationOrder(newRails) {
		if err := agent.RegisterRail(context.Background(), rail); err != nil {
			return err
		}
		agent.RemovePendingRail(rail)
	}
	return nil
}

// UnconfigureSkillEvolution 从 agent 上移除技能演进 rail。
// team 为 nil 移除全部演进 rail 与中断 rail；true/false 按模式移除。
// Python: unconfigure_skill_evolution（configuration.py:412-444）。
// 返回移除的 rail 数。
func UnconfigureSkillEvolution(agent EvolutionConfigurableAgent, team *bool) int {
	var railTypes []reflect.Type
	if team == nil {
		railTypes = []reflect.Type{
			railTypeOf(&SkillEvolutionRail{}),
			railTypeOf(&TeamSkillEvolutionRail{}),
			railTypeOf(&EvolutionInterruptRail{}),
		}
	} else if *team {
		railTypes = []reflect.Type{
			railTypeOf(&TeamSkillEvolutionRail{}),
			railTypeOf(&EvolutionInterruptRail{}),
		}
	} else {
		railTypes = []reflect.Type{
			railTypeOf(&SkillEvolutionRail{}),
			railTypeOf(&EvolutionInterruptRail{}),
		}
	}
	return agent.StripRailsByType(railTypes...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// findExistingSkillEvolutionRail 查找既有 regular 演进 rail（精确类匹配）。
// Python: _find_existing_evolution_rail(agent, team=False)。
func findExistingSkillEvolutionRail(agent EvolutionConfigurableAgent) *SkillEvolutionRail {
	target := railTypeOf(&SkillEvolutionRail{})
	for _, rail := range agent.FindRailsByType(target) {
		if sr, ok := rail.(*SkillEvolutionRail); ok && reflect.TypeOf(rail) == target {
			_ = sr
			return rail.(*SkillEvolutionRail)
		}
	}
	return nil
}

// findExistingTeamSkillEvolutionRail 查找既有 team 演进 rail（精确类匹配）。
// Python: _find_existing_evolution_rail(agent, team=True)。
func findExistingTeamSkillEvolutionRail(agent EvolutionConfigurableAgent) *TeamSkillEvolutionRail {
	target := railTypeOf(&TeamSkillEvolutionRail{})
	for _, rail := range agent.FindRailsByType(target) {
		if reflect.TypeOf(rail) == target {
			return rail.(*TeamSkillEvolutionRail)
		}
	}
	return nil
}

// reviewRuntimeFromRail 经公共访问器返回 rail 的审查运行时。
// Python: _review_runtime_from_rail（configuration.py:190-197）。
func reviewRuntimeFromRail(base *EvolutionRail, carrier evolutionRailCarrier) *review.EvolutionReviewRuntime {
	if carrier == nil {
		return nil
	}
	_ = base
	return carrier.ReviewRuntime()
}

// findExistingInterruptRail 查找既有 EvolutionInterruptRail。
// Python: _find_existing_interrupt_rail（configuration.py:199-206）。
func findExistingInterruptRail(agent EvolutionConfigurableAgent) *EvolutionInterruptRail {
	for _, rail := range agent.FindRailsByType(railTypeOf(&EvolutionInterruptRail{})) {
		if ir, ok := rail.(*EvolutionInterruptRail); ok {
			return ir
		}
	}
	return nil
}

// ensureNoCrossModeConflict 请求模式与已配置模式冲突时报错。
// Python: _ensure_no_cross_mode_conflict（configuration.py:208-219）。
func ensureNoCrossModeConflict(team bool, existingRegular, existingTeam bool) error {
	if !team && existingTeam {
		return fmt.Errorf("Cannot configure regular SkillEvolutionRail: " +
			"TeamSkillEvolutionRail is already configured. " +
			"Use unconfigure_skill_evolution(team=True) first.")
	}
	if team && existingRegular {
		return fmt.Errorf("Cannot configure TeamSkillEvolutionRail: " +
			"SkillEvolutionRail is already configured. " +
			"Use unconfigure_skill_evolution(team=False) first.")
	}
	return nil
}

// validateEvolutionRailConfig 校验既有 rail 配置与请求配置一致。
// 只检查可存储属性：skills_dir、llm、model 转发到内部服务不存于 rail。
// Python: _validate_evolution_rail_config（configuration.py:283-330）。
// signal_trigger / review_trigger 校验 ⤴️ Task 10（Go rail 字段随新版
// 对照回收落地）。
func validateEvolutionRailConfig(base *EvolutionRail, autoSave bool, language string, trajectorySpanProcessor *trajectory.TrajectorySpanProcessor, signalTrigger, reviewTrigger bool) error {
	var mismatches []string
	var existingAutoSave bool
	var existingLanguage string
	var existingProcessor *trajectory.TrajectorySpanProcessor
	var existingSignalTrigger, existingReviewTrigger bool
	switch carrier := base.ext.(type) {
	case *SkillEvolutionRail:
		existingAutoSave = carrier.AutoSave()
		existingLanguage = carrier.ReviewLanguage()
		existingProcessor = carrier.TrajectorySpanProcessor()
		existingSignalTrigger = carrier.SignalTrigger()
		existingReviewTrigger = carrier.ReviewTrigger()
	case *TeamSkillEvolutionRail:
		existingAutoSave = carrier.AutoSave()
		existingLanguage = carrier.ReviewLanguage()
		existingProcessor = carrier.TrajectorySpanProcessor()
		// team rail 的 signal/review trigger 字段随 Task 11 对照回收落地
	}
	if existingAutoSave != autoSave {
		mismatches = append(mismatches, fmt.Sprintf("auto_save: %v != %v", existingAutoSave, autoSave))
	}
	if existingLanguage != language {
		mismatches = append(mismatches, fmt.Sprintf("language: %v != %v", existingLanguage, language))
	}
	if existingProcessor != trajectorySpanProcessor {
		mismatches = append(mismatches, "trajectory_span_processor: object identity differs")
	}
	// Python: requested_signal_trigger/review_trigger 来自 rail_kwargs，
	// expected = bool(requested)；Go 以 ConfigureSkillEvolution 形参承载
	//（configuration.py:308-324）。team rail 字段随 Task 11 落地后补分支。
	if existingSignalTrigger != signalTrigger {
		mismatches = append(mismatches, fmt.Sprintf("signal_trigger: %v != %v", existingSignalTrigger, signalTrigger))
	}
	if existingReviewTrigger != reviewTrigger {
		mismatches = append(mismatches, fmt.Sprintf("review_trigger: %v != %v", existingReviewTrigger, reviewTrigger))
	}
	if len(mismatches) > 0 {
		out := ""
		for i, item := range mismatches {
			if i > 0 {
				out += "; "
			}
			out += item
		}
		return fmt.Errorf("Evolution rail config mismatch. Existing configuration differs: %s. "+
			"Use unconfigure_skill_evolution() first to remove the existing rail.", out)
	}
	return nil
}

// getOrCreateOrBindInterruptRail 一致时返回既有中断 rail，否则创建/绑定。
// Python: _get_or_create_or_bind_interrupt_rail（configuration.py:332-369）。
// 绑定不一致属编程错误：Python raise RuntimeError，Go panic 等价契约。
func getOrCreateOrBindInterruptRail(existing *EvolutionInterruptRail, binding approvalBinding) (*EvolutionInterruptRail, bool) {
	if existing == nil {
		rail := NewEvolutionInterruptRail()
		rail.Configure(binding.runtime, binding.submissionService, binding.autoSave, binding.language)
		return rail, true
	}
	existingRuntime := existing.reviewRuntime
	existingSubmission := existing.submissionService

	if existingRuntime == binding.runtime && existingSubmission == binding.submissionService {
		return existing, false
	}
	if existingRuntime == nil {
		existing.Configure(binding.runtime, binding.submissionService, binding.autoSave, binding.language)
		return existing, false
	}
	panic("EvolutionInterruptRail binding mismatch. " +
		"The existing interrupt rail is bound to a different runtime or submission service. " +
		"Use unconfigure_skill_evolution() first to remove the existing rail.")
}

// pendingRailIdentities 排队 rail 对象身份快照。
// Python: _pending_rail_identities（configuration.py:153-156）。
func pendingRailIdentities(agent EvolutionConfigurableAgent) map[uintptr]struct{} {
	result := map[uintptr]struct{}{}
	for _, rail := range agent.PendingRails() {
		result[railIdentity(rail)] = struct{}{}
	}
	return result
}

// pendingRailsExcluding 返回排队中排除身份集后的 rail 列表。
// Python: new_rails 推导（configuration.py:137）。
func pendingRailsExcluding(agent EvolutionConfigurableAgent, before map[uintptr]struct{}) []agentinterfaces.AgentRail {
	var result []agentinterfaces.AgentRail
	for _, rail := range agent.PendingRails() {
		if _, seen := before[railIdentity(rail)]; !seen {
			result = append(result, rail)
		}
	}
	return result
}

// sortRailsByRuntimeRegistrationOrder 排序使运行时注册满足演化依赖。
// Python: _runtime_registration_order（configuration.py:158-164）——
// EvolutionInterruptRail 最先、Skill/TeamEvolutionRail 次之、其余最后。
func sortRailsByRuntimeRegistrationOrder(rails []agentinterfaces.AgentRail) []agentinterfaces.AgentRail {
	order := func(rail agentinterfaces.AgentRail) (int, string) {
		switch rail.(type) {
		case *EvolutionInterruptRail:
			return 0, fmt.Sprintf("%T", rail)
		case *SkillEvolutionRail, *TeamSkillEvolutionRail:
			return 1, fmt.Sprintf("%T", rail)
		default:
			return 2, fmt.Sprintf("%T", rail)
		}
	}
	sorted := make([]agentinterfaces.AgentRail, len(rails))
	copy(sorted, rails)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi, pn := order(sorted[i])
		ci, cn := order(sorted[j])
		if pi != ci {
			return pi < ci
		}
		return pn < cn
	})
	return sorted
}

// railIdentity 取 rail 的对象身份（Python id(rail) 等价）。
func railIdentity(rail agentinterfaces.AgentRail) uintptr {
	if rail == nil {
		return 0
	}
	return reflect.ValueOf(rail).Pointer()
}

// railTypeOf 取 rail 载体类型的 reflect.Type。
func railTypeOf(rail agentinterfaces.AgentRail) reflect.Type {
	return reflect.TypeOf(rail)
}
