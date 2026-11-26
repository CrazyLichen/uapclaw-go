package providers

import (
	"context"
	"fmt"
	"os"

	config "github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/scheduling"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	skillrails "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/swarmsnapshot"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// evolution_rails.go —— swarm 装配的技能演进 rail provider（leader/teammate 分派）。
//
// Python: jiuwenswarm/agents/swarm/providers/evolution_rails.py（865 行 1:1）。
// 偏差 D-15.24c-3（语言层）：Python SwarmTeamSkillEvolutionRail 等子类 override
// init(agent) 自注册 TeamManager；Go 无继承 → SwarmXxx 组合体 override Init/Uninit
// 执行同一注册段（extension_binder 在挂载期调用 rail.Init(ctx, agent)）。
// 偏差 D-15.24c-4（G3 裁决）：build_symphony_graph_evolution_rail 依赖
// runtime.symphony（领域十六未实施），依赖缺失分支返回 nil——与 Python
// try/except→None 行为一致；回收条件 = 16.x symphony adapter 落地。

// ──────────────────────────── 常量 ────────────────────────────

const (
	constSymphonyGraphEvolution = "swarm.symphony_graph_evolution"
	constEvolutionInterrupt     = "swarm.evolution_interrupt"
	constTeamSkillEvolution     = "swarm.team_skill_evolution"
	constTeamSkillCreate        = "swarm.team_skill_create"
	constMemberSkillEvolution   = "swarm.member_skill_evolution"
)

// ──────────────────────────── 结构体 ────────────────────────────

// 三条独立注册回调（team 包 init 注入；nil 时跳过注册）。
// 偏差 D-15.24c-3：providers↔team 成环（team_manager_spec.go→swarm→providers），
// providers 只持有回调本体；载荷用 swarmsnapshot.Snapshot 具体类型 +
// 各 rail 具体类型参数（2026-11-26 四轮审查：消 kind 分派与 any rail 参数，
// 每条注册链一条强类型回调）。
var (
	// teamEvolutionRailRegistrar leader 团队技能演进 rail 的注册回调
	teamEvolutionRailRegistrar func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail *SwarmTeamSkillEvolutionRail)
	// teamCreateRailRegistrar leader 团队技能创建 rail 的注册回调
	teamCreateRailRegistrar func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail *SwarmTeamSkillCreateRail)
	// memberEvolutionRailRegistrar teammate 成员技能演进 rail 的注册回调
	memberEvolutionRailRegistrar func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail *SwarmMemberSkillEvolutionRail)
)

// SetTeamEvolutionRailRegistrar 注入演进 rail 注册回调。
func SetTeamEvolutionRailRegistrar(fn func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail *SwarmTeamSkillEvolutionRail)) {
	teamEvolutionRailRegistrar = fn
}

// SetTeamCreateRailRegistrar 注入创建 rail 注册回调。
func SetTeamCreateRailRegistrar(fn func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail *SwarmTeamSkillCreateRail)) {
	teamCreateRailRegistrar = fn
}

// SetMemberEvolutionRailRegistrar 注入成员演进 rail 注册回调。
func SetMemberEvolutionRailRegistrar(fn func(snapshot *swarmsnapshot.Snapshot, agent agentinterfaces.BaseAgent, rail *SwarmMemberSkillEvolutionRail)) {
	memberEvolutionRailRegistrar = fn
}

// SwarmTeamSkillEvolutionRail 自注册 TeamManager 的 TeamSkillEvolutionRail。
// Python: SwarmTeamSkillEvolutionRail（evolution_rails.py:206-287）。
type SwarmTeamSkillEvolutionRail struct {
	*evolution.TeamSkillEvolutionRail
	// swarmSnapshot 构建期捕获的上下文快照（bind_swarm_context 捕获）
	swarmSnapshot *swarmsnapshot.Snapshot
}

// BindSwarmContext 捕获 TeamManager 注册所需构建期句柄。
// Python: bind_swarm_context（evolution_rails.py:214-244）。
func (r *SwarmTeamSkillEvolutionRail) BindSwarmContext(ws *swarmsnapshot.Snapshot) {
	r.swarmSnapshot = ws
}

// Init 挂载 agent 并经父包 registrar 向 per-channel TeamManager 注册。
// Python: init（evolution_rails.py:246-287）。
func (r *SwarmTeamSkillEvolutionRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if err := r.TeamSkillEvolutionRail.Init(ctx, agent); err != nil {
		return err
	}
	if teamEvolutionRailRegistrar != nil {
		teamEvolutionRailRegistrar(r.swarmSnapshot, agent, r)
	}
	return nil
}

// Uninit 反注册（热重载注销链消费）。
func (r *SwarmTeamSkillEvolutionRail) Uninit(agent agentinterfaces.BaseAgent) error {
	return r.TeamSkillEvolutionRail.Uninit(agent)
}

// SwarmTeamSkillCreateRail 自注册 TeamManager 的 TeamSkillCreateRail。
// Python: SwarmTeamSkillCreateRail（evolution_rails.py:290-358）。
type SwarmTeamSkillCreateRail struct {
	*skillrails.TeamSkillCreateRail
	swarmSnapshot *swarmsnapshot.Snapshot
}

// BindSwarmContext 捕获构建期句柄。
// Python: bind_swarm_context（evolution_rails.py:293-323）。
func (r *SwarmTeamSkillCreateRail) BindSwarmContext(ws *swarmsnapshot.Snapshot) {
	r.swarmSnapshot = ws
}

// Init 挂载 agent 并经父包 registrar 注册。
// Python: init（evolution_rails.py:324-358）。
func (r *SwarmTeamSkillCreateRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if err := r.TeamSkillCreateRail.Init(ctx, agent); err != nil {
		return err
	}
	if teamCreateRailRegistrar != nil {
		teamCreateRailRegistrar(r.swarmSnapshot, agent, r)
	}
	return nil
}

// Uninit 反注册。
func (r *SwarmTeamSkillCreateRail) Uninit(agent agentinterfaces.BaseAgent) error {
	return r.TeamSkillCreateRail.Uninit(agent)
}

// SwarmMemberSkillEvolutionRail 自注册 teammate 演进 rail。
// Python: SwarmMemberSkillEvolutionRail（evolution_rails.py:361-455）。
type SwarmMemberSkillEvolutionRail struct {
	*evolution.MemberSkillEvolutionRail
	swarmSnapshot *swarmsnapshot.Snapshot
}

// BindSwarmContext 捕获构建期句柄。
// Python: bind_swarm_context（evolution_rails.py:369-402）。
func (r *SwarmMemberSkillEvolutionRail) BindSwarmContext(ws *swarmsnapshot.Snapshot) {
	r.swarmSnapshot = ws
}

// Init 挂载 agent 并经父包 registrar 注册。
// Python: init（evolution_rails.py:404-455）。
func (r *SwarmMemberSkillEvolutionRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if err := r.MemberSkillEvolutionRail.Init(ctx, agent); err != nil {
		return err
	}
	if memberEvolutionRailRegistrar != nil {
		memberEvolutionRailRegistrar(r.swarmSnapshot, agent, r)
	}
	return nil
}

// Uninit 反注册。
func (r *SwarmMemberSkillEvolutionRail) Uninit(agent agentinterfaces.BaseAgent) error {
	return r.MemberSkillEvolutionRail.Uninit(agent)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterEvolutionRailProviders 注册 evolution_rails 全部 provider。
// 清单顺序按 evolution_rails.py 声明序（symphony_graph_evolution →
// evolution_interrupt → team_skill_evolution → team_skill_create →
// member_skill_evolution；Python :507-852 @harness_element 段）。
func RegisterEvolutionRailProviders() {
	hschema.RegisterRailProvider(constSymphonyGraphEvolution, railProviderAdapter(buildSymphonyGraphEvolutionRail))
	hschema.RegisterRailProvider(constEvolutionInterrupt, railProviderAdapter(buildEvolutionInterruptProvider))
	hschema.RegisterRailProvider(constTeamSkillEvolution, railProviderAdapter(buildTeamSkillEvolutionRail))
	hschema.RegisterRailProvider(constTeamSkillCreate, railProviderAdapter(buildTeamSkillCreateRail))
	hschema.RegisterRailProvider(constMemberSkillEvolution, railProviderAdapter(buildMemberSkillEvolutionRail))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildEvolutionLLMFrom 从可序列化 model-config param 构建 evolution LLM。
// Python: _build_evolution_llm_from（evolution_rails.py:458-485）：
// 语义镜像 team_runtime_inheritance.build_evolution_llm，但取 spec 构建期
// 烘入 params 的已解析模型配置（live LLM 句柄在本 build 期构造，schema 内
// 永不构造）。
// 偏差：reasoning kwargs 注入在 spec 构建期 ResolveModelConfig 承载（运行时
// 继承批），此处直接消费已解析三段配置。
func buildEvolutionLLMFrom(modelConfig map[string]any) (*llm.Model, string, error) {
	mcc := mapAnyOrEmpty(mapAny(modelConfig, "model_client_config"))
	mco := mapAnyOrEmpty(mapAny(modelConfig, "model_config_obj"))
	modelName := stringFieldOrEmpty(mapString(modelConfig, "model_name"), "gpt-4")
	// 镜像 team_runtime_inheritance.build_evolution_llm 的构造段：
	// temperature 取 model_config_obj.temperature，默认 0.95。
	temperature := 0.95
	if t, ok := config.Float(mco, "temperature"); ok {
		temperature = t
	}
	requestConfig := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(modelName),
		llmschema.WithTemperatureValue(temperature),
	)
	provider := mapStrField(mcc, "client_provider")
	if provider == "" {
		return nil, modelName, fmt.Errorf("evolution LLM 构建失败（config 无有效模型）")
	}
	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: provider,
		ClientID:       mapStrField(mcc, "client_id"),
		APIKey:         mapStrField(mcc, "api_key"),
		APIBase:        mapStrField(mcc, "api_base"),
	}
	model, err := llm.NewModel(clientConfig, requestConfig)
	if err != nil {
		return nil, modelName, err
	}
	return model, modelName, nil
}

// buildEvolutionApprovalStack 返回按注册顺序排列的审批中断 + 演进 rail 栈。
// Python: _build_evolution_approval_stack（evolution_rails.py:488-504）。
func buildEvolutionApprovalStack(
	rail agentinterfaces.AgentRail,
	runtime *review.EvolutionReviewRuntime,
	submissionService evolution.EvolutionSubmissionService,
	autoSave bool,
	language string,
) []any {
	interrupt := evolution.NewEvolutionInterruptRail()
	interrupt.Configure(runtime, submissionService, autoSave, language)
	return []any{interrupt, rail}
}

// buildTeamSkillEvolutionRail 构建 leader-only 团队技能演进 rail（配置源）。
// Python: build_team_skill_evolution_rail（evolution_rails.py:553-647）。
func buildTeamSkillEvolutionRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	// Cheap gate（先于 resolve，非 leader 不构建演进 LLM）
	// Python: :573-578
	if !config.SkillEvolutionEnabled(view.Config) || view.Role != "leader" || view.GlobalSkillsDir == "" {
		return nil, nil
	}
	// Python: TeamSkillEvolutionInput.resolve（params: evolution_model_config/auto_save）
	evolutionModelConfig := mapAnyOrEmpty(mapAny(params, "evolution_model_config"))
	autoSave := mapBool(params, "auto_save")
	skillsDir := skillLibraryDir(view)
	// Python: Path(skills_dir).mkdir(parents=True, exist_ok=True)（:589 材料化例外）
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir skills_dir: %w", err)
	}
	llmModel, actualModelName, err := buildEvolutionLLMFrom(evolutionModelConfig)
	if err != nil {
		// Python: except Exception → warning + return []
		logger.Warn(logComponent).Err(err).Msg("[swarm.team_skill_evolution] build failed")
		return nil, nil
	}
	reviewRuntime := review.NewEvolutionReviewRuntime(60)
	rail := &SwarmTeamSkillEvolutionRail{TeamSkillEvolutionRail: evolution.NewTeamSkillEvolutionRail(
		[]string{skillsDir}, llmModel, actualModelName, stringFieldOrEmpty(view.Language, "cn"),
		evolution.WithTeamEvolutionReviewRuntime(reviewRuntime),
		evolution.WithTeamSkillSignalTrigger(false),
		evolution.WithTeamSkillReviewTrigger(true),
		evolution.WithTeamSkillMemberRole(view.Role),
		evolution.WithTeamSkillAutoSave(autoSave),
		evolution.WithTeamSkillTeamID(view.TeamID),
		evolution.WithTeamSkillDisabledSkills(skillruntime.LoadExecutionDisabledSkills()),
	)}
	// Python: configure_review_feedback_evolution（:610-620 回灌缝）
	if view.GlobalSkillsDir != "" {
		rail.SetReviewFeedbackCoordinator(scheduling.NewReviewFeedbackEvolutionCoordinator(
			view.SessionID, view.TeamID, teamReviewRailProviderOf(rail.TeamSkillEvolutionRail), true,
			config.EvolutionReviewFeedbackMinConfidence(view.Config),
		))
	} else {
		logger.Warn(logComponent).
			Msg("[swarm.team_skill_evolution] review-feedback integration skipped: global_skills_dir is unavailable")
	}
	rail.BindSwarmContext(workspaceSnapshotOf(view, skillsDir))
	logger.Info(logComponent).
		Str("skills_dir", skillsDir).
		Str("model", actualModelName).
		Bool("auto_save", autoSave).
		Msg("[swarm.team_skill_evolution] built")
	// Python: return _build_evolution_approval_stack(rail, ...)（审批栈成对返回）
	return buildEvolutionApprovalStack(rail, reviewRuntime, rail.ApprovalSubmissionService(), autoSave, stringFieldOrEmpty(view.Language, "cn")), nil
}

// buildTeamSkillCreateRail 构建 leader-only 团队技能创建 rail（配置源）。
// Python: build_team_skill_create_rail（evolution_rails.py:685-745）。
func buildTeamSkillCreateRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	// Python: :706-711 门控
	if !config.SkillEvolutionEnabled(view.Config) || view.Role != "leader" || view.GlobalSkillsDir == "" {
		return nil, nil
	}
	skillsDir := skillLibraryDir(view)
	rail := &SwarmTeamSkillCreateRail{TeamSkillCreateRail: skillrails.NewTeamSkillCreateRail(
		skillsDir,
		skillrails.WithTeamSkillCreateAutoTrigger(true),
		skillrails.WithTeamSkillCreateLanguage(stringFieldOrEmpty(view.Language, "cn")),
	)}
	rail.BindSwarmContext(workspaceSnapshotOf(view, skillsDir))
	logger.Info(logComponent).
		Str("skills_dir", skillsDir).
		Str("team_visibility", view.TeamSkillVisibilityPath).
		Msg("[swarm.team_skill_create] built")
	return rail, nil
}

// buildMemberSkillEvolutionRail 构建 teammate-only 成员技能演进 rail。
// Python: build_member_skill_evolution_rail（evolution_rails.py:779-852）。
func buildMemberSkillEvolutionRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	// Cheap gate（先于 resolve，leader 不构建演进 LLM）
	// Python: :798-803
	if !config.SkillEvolutionEnabled(view.Config) || view.Role == "leader" || view.GlobalSkillsDir == "" {
		return nil, nil
	}
	evolutionModelConfig := mapAnyOrEmpty(mapAny(params, "evolution_model_config"))
	skillsDir := skillLibraryDir(view)
	llmModel, actualModelName, err := buildEvolutionLLMFrom(evolutionModelConfig)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[swarm.member_skill_evolution] build failed")
		return nil, nil
	}
	reviewRuntime := review.NewEvolutionReviewRuntime(60)
	language := stringFieldOrEmpty(view.Language, "cn")
	rail := &SwarmMemberSkillEvolutionRail{MemberSkillEvolutionRail: evolution.NewMemberSkillEvolutionRail(
		[]string{skillsDir},
		skillsDir,
		view.GlobalSkillsDir,
		llmModel,
		actualModelName,
		language,
		evolution.WithEvolutionReviewRuntime(reviewRuntime),
		evolution.WithSignalTrigger(true),
		evolution.WithAutoSave(true),
		evolution.WithReviewTrigger(false),
		evolution.WithDisabledSkillsSet(skillruntime.LoadExecutionDisabledSkills()),
	)}
	rail.BindSwarmContext(workspaceSnapshotOf(view, skillsDir))
	logger.Info(logComponent).
		Str("model", actualModelName).
		Msg("[swarm.member_skill_evolution] built")
	return rail, nil
}

// buildSymphonyGraphEvolutionRail 构建 leader-only Symphony 执行图演进 rail。
// Python: build_symphony_graph_evolution_rail（evolution_rails.py:63-96）。
// 偏差 D-15.24c-4：见文件头。
func buildSymphonyGraphEvolutionRail(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	_ = params
	_ = view
	// Python: 非 leader / core_evolution_enabled 关闭 → None；构建失败 warning+None。
	return nil, nil
}

// buildEvolutionInterruptProvider 演进审批中断 rail 的直注册 builder。
// Python: harness_element(kind=RAIL, name=EVOLUTION_INTERRUPT, builder=...)（:507-512）。
func buildEvolutionInterruptProvider(params map[string]any, view *buildctx.SwarmBuildContext) (any, error) {
	_ = params
	_ = view
	return evolution.NewEvolutionInterruptRail(), nil
}

// buildTeamWorkspaceInfo 构建热重载重建用全字段工作区信息。
// Python: _build_team_workspace_info（evolution_rails.py:116-156）：
// TeamWorkspaceInfo 全字段填充；trajectory_span_processor 由 build context
// 提供（缺省 demand 进程级单例在 buildctx 工厂层已回填）。
// workspaceSnapshotOf 捕获构建期工作区句柄。
// Python: _build_team_workspace_info（evolution_rails.py:116-156）——全字段
// 填充 TeamWorkspaceInfo；Go 侧 snapshot 由父包 registrar 换算 team 类型。
func workspaceSnapshotOf(view *buildctx.SwarmBuildContext, skillsDir string) *swarmsnapshot.Snapshot {
	channel := stringFieldOrEmpty(view.Channel, "default")
	return &swarmsnapshot.Snapshot{
		SessionID:  view.SessionID,
		Channel:    channel,
		TeamID:     view.TeamID,
		Role:       view.Role,
		Language:   stringFieldOrEmpty(view.Language, "cn"),
		MemberName: view.MemberName,
		ProjectDir: view.ProjectDir,
		TeamWsRoot: view.TeamWsRoot,
		SkillsDir:  skillsDir,
		Config:     view.Config,
	}
}

// teamReviewRailProviderOf 返回协调器的 rail 回取闭包。
// Python: team_rail_provider=lambda: self（:267）。
func teamReviewRailProviderOf(rail *evolution.TeamSkillEvolutionRail) scheduling.TeamReviewRailProvider {
	return func() scheduling.TeamEvolutionRailLike { return rail }
}

// deepOf 断言 DeepAgent 能力面（TeamManager 注册签名要求）。
func deepOf(agent agentinterfaces.BaseAgent) harnessinterfaces.DeepAgentInterface {
	if deep, ok := agent.(harnessinterfaces.DeepAgentInterface); ok {
		return deep
	}
	return nil
}
