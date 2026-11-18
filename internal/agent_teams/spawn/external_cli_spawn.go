// 本文件是外部 CLI 成员的 spawn 编排主体：把第三方 CLI 二进制作为团队
// 成员拉起。镜像 inprocess_spawn，但成员的「大脑」是一个由 ExternalCliRuntime
// 驱动的第三方 CLI 子进程。TeamAgent 壳（协调、信箱、成员句柄）跑在当前
// 进程；CLI 二进制是唯一的独立进程。runtime 在 configure 之前构建
// （子进程启动是异步的）并注入，使 configurator 跳过 DeepAgent / rail /
// memory 装配。
//
// 依赖注入缝（D-15.16-b1）：spawn 包不能 import agent 与 externalcli
// （agent → spawn 已成环；externalcli → spawn 经 client.go 的 sharedDBFactory
// 反向依赖），TeamAgent 构造 / Configure / 团队模式解析 / workspace 共享 /
// build_cli_runtime / 四路绑定 / 执行入口经 ExternalCliSpawnDeps 由 runtime
// 包注入（SetExternalCliSpawnDeps）。
//
// Python: openjiuwen/agent_teams/spawn/external_cli_spawn.py（795 行）
package spawn

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 组件常量。
const externalCliLogComponent = logger.ComponentTeam

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalCliSpawnDeps external_cli_spawn 对 TeamAgent / Runner / externalcli
// 装配面的依赖注入缝（D-15.16-b1）。
// Python 侧这些是跨包直接 import；Go 侧 agent → spawn 成环，由 runtime 包
// 在 init 时注入具体实现。
type ExternalCliSpawnDeps struct {
	// BuildMemberSystemPrompt 构建外部成员系统提示词
	//（Python: _build_member_system_prompt；loader 参数沿 D-15.16-b2 降级）
	BuildMemberSystemPrompt func(ctx context.Context, spec atschema.TeamAgentSpec, runtimeCtx atschema.TeamRuntimeContext, memberName string, hittEnabled bool) (string, error)
	// BuildCliRuntime 构建 CLI runtime（Python: build_cli_runtime 调用，
	// 两分支逐项参数由注入侧对位）
	BuildCliRuntime func(ctx context.Context, rtc *atschema.TeamRuntimeContext, cfg ExternalCliRuntimeArgs) (ExternalCliMemberRuntime, error)
	// BackendFor 后端查找（Python: backend_for(ctx.cli_agent)）
	BackendFor func(cliAgent string) *ExternalCliBackendView
	// TeamModelToExternal 池模型转换（Python: _team_model_config_to_external）
	TeamModelToExternal func(memberModel *models.TeamModelConfig, cliAgent string) *atschema.ExternalCliModelConfig
	// ProviderTakesEndpointModels 卡片是否声明 ENDPOINT_MODELS
	ProviderTakesEndpointModels func(cliAgent string) bool
	// TakesAuthFallback 卡片是否声明 AUTH_FALLBACK + ENDPOINT_MODELS
	TakesAuthFallback func(cliAgent string) bool
	// NormalizeSkills 规范化技能声明（Python: normalize_skills）
	NormalizeSkills func(skills []any, conflict string) ([]any, error)
	// InstallSkills 安装技能进本地 CLI 项目（Python: install_skills，线程化）
	InstallSkills func(ctx context.Context, skills []any, provider, cwd, conflict string) error
	// BindOrchestration 四路绑定（tracker / task tracker / trajectory /
	// reliability / team tools；externalcli 侧各 Bind 缝的组装）
	BindOrchestration func(ctx context.Context, teammate ExternalCliTeamAgent, runtime ExternalCliMemberRuntime, sessionID string) error
	// StopRuntime 停止 runtime（Python: runtime.stop()）
	StopRuntime func(runtime ExternalCliMemberRuntime)
	// RunMemberTask 以成员身份运行团队（Python: Runner.run_agent_team(member=True)）
	RunMemberTask func(ctx context.Context, teammate ExternalCliTeamAgent, inputs map[string]any, sessionID string) (map[string]any, error)
}

// ExternalCliRuntimeArgs BuildCliRuntime 的入参束（build_cli_runtime 两分支
// 的全部实参，逐项对位 external_cli_spawn.py:623-667）。
type ExternalCliRuntimeArgs struct {
	// CLIAgent 成员后端标识（ctx.cli_agent）
	CLIAgent string
	// CLIConfig 静态启动配置；nil 走 Python :650-667 的无配置分支
	CLIConfig *atschema.ExternalCliAgentSpec
	// Cwd / AddDirs 路径解析结果
	Cwd     string
	AddDirs []string
	// ExternalModelConfig / FallbackExternalModelConfig 模型协商结果
	ExternalModelConfig         *atschema.ExternalCliModelConfig
	FallbackExternalModelConfig *atschema.ExternalCliModelConfig
	// PromoteFallbackModel 兜底晋升回调（:595-602 闭包）
	PromoteFallbackModel func(ctx context.Context) bool
	// SystemPrompt 成员的团队 rail 系统提示词
	SystemPrompt string
	// ResumeExternalBackend 是否恢复后端原生会话
	ResumeExternalBackend bool
	// MemberAgentID 寻址本成员 checkpoint 的 TeamAgent card id
	MemberAgentID string
	// MemberName 语义标识（注入侧组 Options 时使用）
	MemberName string
}

// ExternalCliBackendView 后端能力视图（externalcli.ExternalCliBackend 的最小面）。
type ExternalCliBackendView struct {
	// Name 后端名
	Name string
	// Kind 后端类别（"adapter" / "provider"）
	Kind string
	// InjectsSystemPromptViaArg 是否把 system prompt 作为启动参数携带
	InjectsSystemPromptViaArg bool
}

// ExternalCliTeamAgent 外部成员 TeamAgent 壳的最小面（由 *agent.TeamAgent 满足）。
type ExternalCliTeamAgent interface {
	// AgentCard 返回身份卡片（SpawnableAgent 子面，句柄构造用）
	AgentCard() *agentschema.AgentCard
	// UpdateStatus 更新成员状态（ERROR 终态等）
	UpdateStatus(ctx context.Context, status atschema.MemberStatus) error
	// TeamBackend 访问成员自己的 TeamBackend（promote 回调 / 绑定面用）
	TeamBackend() *tools.TeamBackend
}

// ExternalCliMemberRuntime 外部成员 runtime 的窄接口（D-15.16-b1 注入缝）。
//
// spawn 包不能 import agent（agent→spawn 已成环），`agent.MemberRuntime`
// 无法直接引用；本包对 runtime 只作不透明句柄传递（BuildCliRuntime →
// newTeammate → BindOrchestration → StopRuntime），具体能力面在注入侧
// 收窄为 agent.MemberRuntime（运行时守卫，断言失败即报错拒装）。
// Python 对应位置本为 Any 注解（external_cli_spawn.py:308/:362/:453）；
// Go 以本窄接口替代 any，消除缝上的无类型传递。
type ExternalCliMemberRuntime interface {
	// Stop 停止 runtime（Python: stop；spawn 仅在 goroutine 收尾转调）
	Stop()
}

// externalCliSpawnDeps 进程级注入实例。
var externalCliSpawnDeps *ExternalCliSpawnDeps

// ──────────────────────────── 导出函数 ────────────────────────────

// SetExternalCliSpawnDeps 注入外部 CLI spawn 依赖（由 runtime 包 init 接线）。
func SetExternalCliSpawnDeps(deps *ExternalCliSpawnDeps) { externalCliSpawnDeps = deps }

// ExternalCliSpawnDepsOrNil 返回当前注入的依赖（未注入返回 nil）。
func ExternalCliSpawnDepsOrNil() *ExternalCliSpawnDeps { return externalCliSpawnDeps }

// ExternalCliSpawn 为 ctx.cli_agent 拉起 CLI 并作为团队成员运行。
//
// Python: external_cli_spawn (external_cli_spawn.py:492-774)。
// 步骤序一比一：卡片与 backend → 成员系统提示词 → 静态启动配置三级选择 →
// 模型三路协商（builtin / pool / 静态 + provider negotiate）→ 路径解析与
// skills 安装 → build_cli_runtime → 成员壳装配（workspace 共享 → configure →
// 四路绑定）→ 首条消息组装 → task + handle。
//
// 卡片构造 / TeamAgent 构造 / configure / workspace 共享属 agent 包能力，
// 经 newTeammate 回调由注入侧完成（D-15.16-b1）；Python :675 的
// share_workspace_cache_with 同序并入 newTeammate 缝内（先共享后 configure）。
func ExternalCliSpawn(
	ctx context.Context,
	deps *ExternalCliSpawnDeps,
	rtc atschema.TeamRuntimeContext,
	spec atschema.TeamAgentSpec,
	hittEnabled bool,
	initialMessage string,
	sessionID string,
	resumeExternalBackend bool,
	newTeammate func(ctx context.Context, card ExternalCliAgentCard, rtc atschema.TeamRuntimeContext, spec atschema.TeamAgentSpec, runtime ExternalCliMemberRuntime) (ExternalCliTeamAgent, error),
) (*InProcessSpawnHandle, error) {
	// Python 步骤 1：team_name / member_name / card（:522-529）
	teamName := spec.TeamName
	if rtc.TeamSpec != nil && rtc.TeamSpec.TeamName != "" {
		teamName = rtc.TeamSpec.TeamName
	}
	memberName := rtc.MemberName
	cardID := "unknown"
	if memberName != "" {
		cardID = teamName + "_" + memberName
	}
	name := memberName
	if name == "" {
		name = "unknown"
	}
	desc := "External CLI member"
	if rtc.Desc != "" {
		desc = "External CLI member: " + rtc.Desc
	}
	card := ExternalCliAgentCard{ID: cardID, Name: name, Description: desc}

	// Python 步骤 2：backend_for + 成员系统提示词（:531-544）
	backend := deps.BackendFor(rtc.CLIAgent)
	// Python: leader_workspace_cache 绑进 loader（:537-543）——workspace
	// 演化缓存面未移植（D-15.16-b2），loader 走框架默认。
	var systemPrompt string
	if deps.BuildMemberSystemPrompt != nil {
		prompt, err := deps.BuildMemberSystemPrompt(ctx, spec, rtc, memberName, hittEnabled)
		if err != nil {
			return nil, err
		}
		systemPrompt = prompt
	}

	// Python 步骤 3：_external_cli_config_for_member（:547）
	cliCfg := externalCliConfigForMember(spec, memberName, rtc.CLIAgent, rtc.TemplateExternalCli)

	// Python 步骤 4：模型三路协商（:549-593）
	externalModelConfig := externalCliCfgModelConfig(cliCfg)
	var fallbackExternalModelConfig *atschema.ExternalCliModelConfig
	if rtc.BuiltinModel != nil {
		// 自带登录模型：替换池端点与静态端点配置（:555-564）
		logger.Info(externalCliLogComponent).
			Str("member_name", rtc.MemberName).
			Str("model", rtc.BuiltinModel.Model).
			Str("effort", rtc.BuiltinModel.Effort).
			Msg("[external-cli] member using built-in model")
		externalModelConfig = rtc.BuiltinModel
	} else if rtc.MemberModel != nil {
		poolModelConfig := deps.TeamModelToExternal(rtc.MemberModel, rtc.CLIAgent)
		if poolModelConfig != nil {
			logger.Info(externalCliLogComponent).
				Str("member_name", rtc.MemberName).
				Str("provider", poolModelConfig.Provider).
				Str("model", poolModelConfig.Model).
				Str("api_base", poolModelConfig.APIBase).
				Msg("[external-cli] member using pool-allocated model")
			externalModelConfig = poolModelConfig
		} else {
			logger.Info(externalCliLogComponent).
				Str("member_name", rtc.MemberName).
				Msg("[external-cli] member pool model conversion returned None; falling back to static config")
		}
	} else {
		logger.Info(externalCliLogComponent).
			Str("member_name", rtc.MemberName).
			Msg("[external-cli] member no pool model assigned; using static config")
	}
	if rtc.FallbackMemberModel != nil {
		fallbackExternalModelConfig = deps.TeamModelToExternal(rtc.FallbackMemberModel, rtc.CLIAgent)
	}
	if backend != nil && backend.Kind == externalCliBackendKindProvider {
		externalModelConfig = negotiateProviderEndpoint(&rtc, externalModelConfig, deps.ProviderTakesEndpointModels)
		fallbackExternalModelConfig = negotiateProviderFallback(&rtc, fallbackExternalModelConfig, deps.TakesAuthFallback)
	}

	// Python: promote_fallback_model 闭包（:595-602）——引用 teammate_backend，
	// 在 configure 之后才非 nil；Go 经闭包延迟解引用。
	var promoteFallbackModel func(ctx context.Context) bool
	promoteFallbackModel = func(ctx context.Context) bool {
		teammateBackend, ok := teammateBackendRef.Load().(*tools.TeamBackend)
		if !ok || teammateBackend == nil {
			return false
		}
		return teammateBackend.DB().Member().PromoteMemberFallbackModel(ctx, memberName, teamName)
	}

	// Python 步骤 5-6：路径解析 + skills 安装 + build_cli_runtime（:604-667）
	cwd, addDirs := resolveExternalPaths(&spec, &rtc, externalCliCfgCwd(cliCfg), teamName)
	// Python: skills = normalize_skills(cli_cfg.skills, cli_cfg.skill_conflict)；
	// 本地 CLI + project_dir ≠ cwd 时经线程 install_skills（:611-622）。
	// skills 解析与安装由 BuildCliRuntime 注入侧完成（它持有 skills.go 面）。
	runtime, err := deps.BuildCliRuntime(ctx, &rtc, ExternalCliRuntimeArgs{
		CLIAgent:                    rtc.CLIAgent,
		CLIConfig:                   cliCfg,
		Cwd:                         cwd,
		AddDirs:                     addDirs,
		ExternalModelConfig:         externalModelConfig,
		FallbackExternalModelConfig: fallbackExternalModelConfig,
		PromoteFallbackModel:        promoteFallbackModel,
		SystemPrompt:                systemPrompt,
		ResumeExternalBackend:       resumeExternalBackend,
		MemberAgentID:               cardID,
		MemberName:                  memberName,
	})
	if err != nil {
		return nil, err
	}

	// Python 步骤 7：成员壳装配（:669-688）——_TeamAgent(card) →
	// share_workspace_cache_with → configure(member_runtime=runtime) →
	// 记录 teammate_backend 引用（promote 闭包的目标）。
	teammate, err := newTeammate(ctx, card, rtc, spec, runtime)
	if err != nil {
		return nil, err
	}
	if teammate == nil {
		return nil, fmt.Errorf("external CLI spawn: teammate construction failed for %s", memberName)
	}
	if tb := teammate.TeamBackend(); tb != nil {
		teammateBackendRef.Store(tb)
	}

	// Python 步骤 8：四路绑定（:689-732）——tracker / task tracker /
	// trajectory / reliability / team tools，经 BindOrchestration 缝组装。
	if deps.BindOrchestration != nil {
		if err := deps.BindOrchestration(ctx, teammate, runtime, sessionID); err != nil {
			return nil, err
		}
	}

	// Python 步骤 9：首条消息组装（:734-743）
	baseQuery := initialMessage
	// 把 system prompt 作为启动参数接受的 backend 已自带；其余前置进首条
	// 用户消息。
	hasLaunchPrompt := baseQuery != "" && systemPrompt != ""
	needsPromptPrepend := backend != nil && !backend.InjectsSystemPromptViaArg
	query := baseQuery
	if hasLaunchPrompt && needsPromptPrepend {
		query = systemPrompt + "\n\n---\n\n" + baseQuery
	}
	inputs := map[string]any{"query": query}

	// Python 步骤 10：task + handle（:746-774）——contextvars.copy_context()
	// → task；Go 以 per-goroutine ctx 注入 session id（D-15.13-f 同源）。
	handle := NewInProcessExternalHandle(fmt.Sprintf("extcli-%s", memberName), teammate)
	go func() {
		execCtx := sessionctx.WithSessionState(ctx, func() *sessionctx.SessionState {
			state := sessionctx.InitSessionState()
			state.SetSessionID(sessionID)
			return state
		}())
		logger.Info(externalCliLogComponent).Str("member_name", memberName).Msg("[external-cli] member started")
		crashed := false
		defer func() {
			// Python finally：runtime.stop()；crashed 时标记 ERROR（:760-765）
			deps.StopRuntime(runtime)
			if crashed {
				// runtime 停止后再写，避免迟到的 harness 状态事件把成员映射回 BUSY/READY
				if err := markCrashedMemberError(execCtx, teammate, memberName); err != nil {
					logger.Error(externalCliLogComponent).Err(err).
						Str("member_name", memberName).
						Msg("[external-cli] failed to mark crashed member as error")
				}
			}
		}()
		if _, err := deps.RunMemberTask(execCtx, teammate, inputs, sessionID); err != nil {
			crashed = true
			logger.Error(externalCliLogComponent).Err(err).
				Str("member_name", memberName).
				Msg("[external-cli] member crashed")
		}
	}()
	logger.Info(externalCliLogComponent).
		Str("member_name", memberName).
		Str("process_id", handle.ProcessID()).
		Msg("[external-cli] spawned member")
	return handle, nil
}

// MarkCrashedMemberError 把运行任务崩溃的外部 CLI 成员标记为 ERROR。
// 崩溃的任务把状态留在上一轮写的值（通常 BUSY），成员看起来活着但信箱无人
// 消费。ERROR 是 leader 自动启动漏斗在下一帧消息时重启的依据，也是名册对
// 死成员应显示的状态。
// Python: _mark_crashed_member_error (external_cli_spawn.py:777-792)
func markCrashedMemberError(ctx context.Context, teammate ExternalCliTeamAgent, memberName string) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(externalCliLogComponent).Any("panic", r).
				Str("member_name", memberName).
				Msg("[external-cli] failed to mark crashed member as error")
		}
	}()
	return teammate.UpdateStatus(ctx, atschema.MemberStatusError)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// externalCliBackendKindProvider provider 后端类别（对应 backends.kind）。
const externalCliBackendKindProvider = "provider"

// teammateBackendRef configure 之后的成员 TeamBackend 引用（promote 闭包目标）。
// Python 以闭包捕获局部变量；Go 以原子引用承载 configure 后才出现的值。
var teammateBackendRef atomicValue

// atomicValue 极简原子引用（避免为本文件引入 sync/atomic 全量面）。
type atomicValue struct {
	v any
}

// Store 存值。
func (a *atomicValue) Store(v any) { externalCliSpawnRefMu.Lock(); a.v = v; externalCliSpawnRefMu.Unlock() }

// Load 取值。
func (a *atomicValue) Load() any {
	externalCliSpawnRefMu.Lock()
	defer externalCliSpawnRefMu.Unlock()
	return a.v
}

// negotiateProviderEndpoint 丢弃第三方 provider 跑不了的端点模型。
// 内置模型直通（provider 的 model_config 决定能不能跑）；端点模型只有在其
// 卡片声明了 ENDPOINT_MODELS 时才到达 provider，否则成员保留其 CLI 配置的
// 端点。
// Python: _negotiate_provider_endpoint (external_cli_spawn.py:49-71)
func negotiateProviderEndpoint(ctx *atschema.TeamRuntimeContext, config *atschema.ExternalCliModelConfig, takes func(string) bool) *atschema.ExternalCliModelConfig {
	if config == nil || (config.APIBase == "" && config.Provider == "") {
		return config
	}
	if takes(ctx.CLIAgent) {
		return config
	}
	logger.Info(externalCliLogComponent).
		Str("cli_agent", ctx.CLIAgent).
		Str("member_name", ctx.MemberName).
		Msg("[external-cli] provider takes no endpoint models; member keeps its CLI's own model")
	return nil
}

// negotiateProviderFallback 丢弃第三方 provider 切换不了的兜底模型。
// 兜底只有 takes_auth_fallback 成立时到达 provider：卡片声明 AUTH_FALLBACK，
// 且兜底是池条目，还要声明 ENDPOINT_MODELS。
// Python: _negotiate_provider_fallback (external_cli_spawn.py:74-95)
func negotiateProviderFallback(ctx *atschema.TeamRuntimeContext, config *atschema.ExternalCliModelConfig, takes func(string) bool) *atschema.ExternalCliModelConfig {
	if config == nil {
		return nil
	}
	if takes(ctx.CLIAgent) {
		return config
	}
	logger.Info(externalCliLogComponent).
		Str("cli_agent", ctx.CLIAgent).
		Str("member_name", ctx.MemberName).
		Msg("[external-cli] provider has no authentication fallback; member runs without one")
	return nil
}

// externalCliConfigForMember 挑一个外部 CLI 成员的启动配置。
// 顺序：预定义成员自带配置；spawn_expert 持久化在成员行上的配置
// （template_config，回读进 TeamRuntimeContext.template_external_cli）；
// 团队对该 CLI kind 的声明配置。
// Python: _external_cli_config_for_member (external_cli_spawn.py:98-121)
func externalCliConfigForMember(spec atschema.TeamAgentSpec, memberName, cliAgent string, templateConfig *atschema.ExternalCliAgentSpec) *atschema.ExternalCliAgentSpec {
	for i := range spec.PredefinedMembers {
		member := &spec.PredefinedMembers[i]
		if member.MemberName == memberName && member.ExternalCLI != nil {
			return member.ExternalCLI
		}
	}
	if templateConfig != nil {
		return templateConfig
	}
	for i := range spec.ExternalCliAgents {
		if spec.ExternalCliAgents[i].CLIAgent == cliAgent {
			return &spec.ExternalCliAgents[i]
		}
	}
	return nil
}

// externalCliCfgModelConfig 取静态配置上的模型端点（:553）。
func externalCliCfgModelConfig(cfg *atschema.ExternalCliAgentSpec) *atschema.ExternalCliModelConfig {
	if cfg == nil {
		return nil
	}
	return cfg.ExternalModelConfig
}

// externalCliCfgCwd 取静态配置上的 cwd（:608）。
func externalCliCfgCwd(cfg *atschema.ExternalCliAgentSpec) string {
	if cfg == nil {
		return ""
	}
	return cfg.Cwd
}

// samePath 规范化比较两个路径（normcase+normpath 等价，Windows 大小写
// 不敏感语义以 ToLower 近似）。
// Python: _same_path (external_cli_spawn.py:246-247)
func samePath(left, right string) bool {
	norm := func(p string) string {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		return strings.ToLower(filepath.Clean(abs))
	}
	return norm(left) == norm(right)
}

// resolveExternalPaths 解析外部成员的 cwd 与额外可访问目录。
// Python: _resolve_external_paths (external_cli_spawn.py:281-298)，
// 四级 cwd 选择 + extra_dirs 去重（复用 paths.go 的同级逻辑在 spawn 侧
// 独立成函数——Python 同文件双段填充先例 D-15.12-e）。
func resolveExternalPaths(spec *atschema.TeamAgentSpec, ctx *atschema.TeamRuntimeContext, configuredCwd, teamName string) (string, []string) {
	explicitCwd := pathValue(configuredCwd)
	worktreePath := pathValue(ctx.WorktreePath)
	projectDir := buildContextProjectDir(spec)
	teamWorkspace := teamWorkspacePath(spec, teamName)
	cwd := explicitCwd
	if cwd == "" {
		cwd = worktreePath
	}
	if cwd == "" {
		cwd = projectDir
	}
	if cwd == "" {
		cwd = teamWorkspace
	}

	var extraDirs []string
	for _, path := range []string{explicitCwd, worktreePath, projectDir, teamWorkspace} {
		appendExtraDir(&extraDirs, path, cwd)
	}
	return cwd, extraDirs
}

// pathValue 去空白后的非空字符串；否则空串。
// Python: _path_value (external_cli_spawn.py:239-243)
func pathValue(value string) string {
	stripped := strings.TrimSpace(value)
	if stripped == "" {
		return ""
	}
	return stripped
}

// appendExtraDir 追加目录到结果，跳过空 / 与 cwd 相同 / 已存在的路径。
// Python: _append_extra_dir (external_cli_spawn.py:250-257)
func appendExtraDir(result *[]string, path string, cwd string) {
	if path == "" {
		return
	}
	if cwd != "" && samePath(path, cwd) {
		return
	}
	for _, existing := range *result {
		if samePath(path, existing) {
			return
		}
	}
	*result = append(*result, path)
}

// buildContextProjectDir spec.build_context.project_dir 取值。
// Python: _build_context_project_dir (external_cli_spawn.py:267-271)
func buildContextProjectDir(spec *atschema.TeamAgentSpec) string {
	if spec == nil || spec.BuildContext == nil {
		return ""
	}
	return pathValue(spec.BuildContext.GetProjectDir())
}

// buildContextTeamOutputsDir spec.build_context.team_outputs_dir 取值。
// Python: _build_context_team_outputs_dir (external_cli_spawn.py:274-278)
func buildContextTeamOutputsDir(spec *atschema.TeamAgentSpec) string {
	if spec == nil || spec.BuildContext == nil {
		return ""
	}
	return pathValue(spec.BuildContext.GetTeamOutputsDir())
}

// teamWorkspacePath 团队共享工作区绝对路径（root_path 优先，否则平台默认）。
// Python: _team_workspace_path (external_cli_spawn.py:260-264)
func teamWorkspacePath(spec *atschema.TeamAgentSpec, teamName string) string {
	if spec != nil && spec.Workspace != nil && spec.Workspace.RootPath != "" {
		return spec.Workspace.RootPath
	}
	return agentteams.TeamWorkspaceDir(teamName)
}
