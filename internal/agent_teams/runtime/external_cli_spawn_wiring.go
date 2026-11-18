// 本文件是 external_cli_spawn 的 runtime 侧依赖接线：把 spawn 包
// ExternalCliSpawnDeps 缝、externalcli.SetSharedDBFactory 缝接上具体实现。
// 全部装配序逐条对位 external_cli_spawn.py:492-774；agent / externalcli /
// prompts 的引用只在本包（runtime）发生，spawn 包保持零反向依赖（D-15.16-b1）。
//
// Python: openjiuwen/agent_teams/spawn/external_cli_spawn.py（编排主体）
package runtime

import (
	"context"
	"fmt"

	agent "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"go.opentelemetry.io/otel/attribute"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WireExternalCliSpawn 注入外部 CLI spawn 依赖（GetTeamRuntimeManager 首次
// 创建时触发，与 SetRunTeamMemberFunc 同点接线）。
func WireExternalCliSpawn() {
	// externalcli client.go 的共享 DB 工厂（Python: spawn.shared_resources.get_shared_db）
	externalcli.SetSharedDBFactory(spawn.GetSharedDB)
	// spawn.ExternalCliSpawnDeps 缝
	spawn.SetExternalCliSpawnDeps(&spawn.ExternalCliSpawnDeps{
		BuildMemberSystemPrompt:     buildMemberSystemPrompt,
		BuildCliRuntime:             buildExternalCliRuntime,
		BackendFor:                  backendViewFor,
		TeamModelToExternal:         externalcli.TeamModelConfigToExternal,
		ProviderTakesEndpointModels: externalcli.ProviderTakesEndpointModels,
		TakesAuthFallback:           externalcli.TakesAuthFallback,
		NormalizeSkills:             normalizeSkillsShim,
		InstallSkills:               installSkillsShim,
		BindOrchestration:           bindExternalMemberOrchestration,
		StopRuntime:                 stopExternalRuntime,
		RunMemberTask:               runExternalMemberTask,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMemberSystemPrompt 构建外部 CLI 成员的系统提示词：复用 in-process
// 成员的 team rail sections（同批 section，同法组装），排除不适用的
// DeepAgent rails（safety/workspace/memory……）。
// 提示词只承载团队常驻政策；成员私有约定经 spawn 后绑定的 tracker 以
// <team-context> 通道投递——与进程内成员同路。
// Python: _build_member_system_prompt (external_cli_spawn.py:124-191)。
// 偏差 D-15.16-b2：loader=make_template_loader(ws_cache)（演化后 A 类模板）
// 未接线，走框架默认 loader；group_chat 追加段随 group_chat 子系统
// ⤴️（D-15.16-b10）。
func buildMemberSystemPrompt(ctx context.Context, spec atschema.TeamAgentSpec, rtc atschema.TeamRuntimeContext, memberName string, hittEnabled bool) (string, error) {
	language := "cn"
	teamMode := agent.ResolveTeamModeExported(spec)
	if rtc.TeamSpec != nil && rtc.TeamSpec.Language != "" {
		language = rtc.TeamSpec.Language
	}
	lifecycle := string(spec.Lifecycle)
	teammateMode := string(spec.TeammateMode)
	if teammateMode == "" {
		teammateMode = "build_mode"
	}
	dispatchMode := spec.DispatchMode
	if dispatchMode == "" {
		dispatchMode = "autonomous"
	}
	prompt, err := prompts.BuildTeamMemberSystemPrompt(
		rtc.Role,
		memberName,
		lifecycle,
		teammateMode,
		teamMode,
		dispatchMode,
		spec.EnableMemberMessaging,
		spec.MaxSubtaskDepth > 0,
		rtc.Prompt,
		language,
		hittEnabled,
		spec.ExposeHumanAgentsToTeammates,
		"external",
		externalCliMCPServerName(rtc.CLIAgent),
	)
	if err != nil {
		return "", err
	}
	if prompt == "" {
		return "", nil
	}
	return prompt, nil
}

// externalCliMCPServerName 团队工具 MCP server 名：codex 的团队工具注册为
// 顶层动态工具，没有 server 可声明，提示词不包裹；其余用 TEAM_MCP_SERVER_NAME。
// Python: None if ctx.cli_agent == "codex" else TEAM_MCP_SERVER_NAME（:184）
func externalCliMCPServerName(cliAgent string) string {
	if cliAgent == "codex" {
		return ""
	}
	return externalcli.TeamMCPServerName
}

// buildExternalCliRuntime 构建 CLI runtime：cli_cfg 有/无两分支
// （Python :604-667），实参逐项对位。skills 解析在此完成；安装门卫
// （本地 CLI + project_dir ≠ cwd）的 project_dir 来源 BuildContext——
// 15.16c seed 工厂落地前门卫按 Python project_dir=None 短路（⤴️15.16c）。
func buildExternalCliRuntime(ctx context.Context, rtc *atschema.TeamRuntimeContext, args spawn.ExternalCliRuntimeArgs) (spawn.ExternalCliMemberRuntime, error) {
	opts := externalcli.Options{
		Cwd:                         args.Cwd,
		AddDirs:                     args.AddDirs,
		ExternalModelConfig:         args.ExternalModelConfig,
		FallbackExternalModelConfig: args.FallbackExternalModelConfig,
		SystemPrompt:                args.SystemPrompt,
		ResumeExternalBackend:       args.ResumeExternalBackend,
		MemberAgentID:               args.MemberAgentID,
	}
	if args.PromoteFallbackModel != nil {
		opts.PromoteFallbackModel = func(ctx context.Context) (bool, error) {
			return args.PromoteFallbackModel(ctx), nil
		}
	}
	if args.CLIConfig != nil {
		cfg := args.CLIConfig
		if len(cfg.Command) > 0 {
			opts.CommandOverride = cfg.Command
		}
		opts.CLIPath = cfg.CLIPath
		opts.SystemPromptMode = cfg.SystemPromptMode
		opts.SkillConflict = cfg.SkillConflict
		inject := cfg.InjectMCP
		opts.InjectMCP = &inject
		opts.MCPDefaultToolsApprovalMode = cfg.MCPDefaultToolsApprovalMode
		if cfg.CodexBypassApprovalsAndSandbox != nil {
			opts.CodexBypassApprovalsAndSandbox = *cfg.CodexBypassApprovalsAndSandbox
		}
		if cfg.CodexTurnIdleTimeoutS != nil {
			v := *cfg.CodexTurnIdleTimeoutS
			opts.CodexTurnIdleTimeoutS = &v
		}
		if cfg.CodexTurnIdleRetries != nil {
			v := *cfg.CodexTurnIdleRetries
			opts.CodexTurnIdleRetries = &v
		}
		if cfg.ClaudeTurnIdleTimeoutS != nil {
			v := *cfg.ClaudeTurnIdleTimeoutS
			opts.ClaudeTurnIdleTimeoutS = &v
		}
		if cfg.ClaudeMaxBufferSize != nil {
			v := *cfg.ClaudeMaxBufferSize
			opts.ClaudeMaxBufferSize = &v
		}
		if cfg.MCPServerCommand != nil {
			opts.MCPServerCommand = cfg.MCPServerCommand
		}
		if cfg.Env != nil {
			opts.ExtraEnv = cfg.Env
		}
		opts.SshTransport = cfg.SshTransport
		// Python: skills = normalize_skills(cli_cfg.skills, cli_cfg.skill_conflict)（:611）
		skills, err := harnessproviders.NormalizeSkillsSchema(cfg.Skills, cfg.SkillConflict)
		if err != nil {
			return nil, err
		}
		opts.Skills = skills
		// Python :613-622 安装门卫：skills 非空且 project_dir 非空且本地 CLI 且
		// project_dir ≠ cwd → asyncio.to_thread(install_skills, ...)。
		// project_dir 经 BuildContext（D-15.13-k 过渡工厂未产出真值）⤴️15.16c
		// 接线；此处保留调用形状，当前短路。
	}
	runtime, err := externalcli.BuildCliRuntime(ctx, rtc, opts)
	if err != nil {
		return nil, err
	}
	return runtime, nil // agent.MemberRuntime 满足 spawn.ExternalCliMemberRuntime（Stop）
}

// backendViewFor 后端能力视图（Python: backend_for(ctx.cli_agent)）。
func backendViewFor(cliAgent string) *spawn.ExternalCliBackendView {
	b := externalcli.BackendFor(cliAgent)
	if b == nil {
		return nil
	}
	return &spawn.ExternalCliBackendView{
		Name:                      b.Name,
		Kind:                      string(b.Kind),
		InjectsSystemPromptViaArg: b.InjectsSystemPromptViaArg,
	}
}

// normalizeSkillsShim 规范化技能声明（Python: normalize_skills）。
func normalizeSkillsShim(skills []any, conflict string) ([]any, error) {
	out, err := harnessproviders.NormalizeSkillsSchema(skills, conflict)
	if err != nil {
		return nil, err
	}
	result := make([]any, 0, len(out))
	for _, s := range out {
		result = append(result, s)
	}
	return result, nil
}

// installSkillsShim 安装技能进本地 CLI 项目（Python: install_skills，
// asyncio.to_thread → Go 同步直调，D-15.12-c 同源）。
func installSkillsShim(ctx context.Context, skills []any, provider, cwd, conflict string) error {
	sources, err := harnessproviders.NormalizeSkillsSchema(skills, conflict)
	if err != nil {
		return err
	}
	_, err = harnessproviders.InstallSkills(sources, provider, cwd, conflict, "")
	return err
}

// bindExternalMemberOrchestration 成员壳装配后的四路绑定（Python :689-732）：
// （成员壳构造 newTeammate 缝由 spawn_manager.spawnExternalCLI 内联承担，
// 含 Python :675 的 share_workspace_cache_with 同序调用——本文件原先的
// 零消费副本 newExternalMemberTeamAgent 已按规则 8 删除。）
//
//  1. bind_team_context_tracker（BuildTeamContextTracker，15.13 已移植）
//  2. bind_task_outcome_tracker（build_task_event_tracker）
//  3. ExternalHarnessMemberRuntime：_bind_trajectory_recorder（15.11 recorder
//     + ObservabilityExecutionSubject）
//  4. bind_reliability_context（五参调用点；实现仍桩 ⤴️15.7/15.8，D-15.16-a7）
//  5. _bind_protocol_member_team_tools（claude ⤴️15.7 / codex ⤴️15.8，
//     swarmflow host ⤴️swarmflow 章节——D-15.16-b3 挂点）
func bindExternalMemberOrchestration(ctx context.Context, teammate spawn.ExternalCliTeamAgent, runtimeValue spawn.ExternalCliMemberRuntime, sessionID string) error {
	ta, ok := teammate.(*agent.TeamAgent)
	if !ok {
		return fmt.Errorf("bind orchestration: teammate 必须是 *agent.TeamAgent，实际 %T", teammate)
	}
	// 1. bind_team_context_tracker（Python :689-697）
	if binder, ok := runtimeValue.(interface {
		BindTeamContextTracker(*tracker.TeamContextTracker)
	}); ok {
		spec := specOf(ta)
		rtc := rtcOf(ta)
		binder.BindTeamContextTracker(externalcli.BuildTeamContextTracker(
			ta.TeamBackend(), &spec, &rtc, ta.MemberName(), ta.TeamName(),
		))
	}
	// 2. bind_task_outcome_tracker（Python :698-702）
	if binder, ok := runtimeValue.(interface {
		BindTaskOutcomeTracker(tracker.TaskEventTracker)
	}); ok {
		binder.BindTaskOutcomeTracker(tracker.BuildTaskEventTracker(
			specOf(ta).DispatchMode, ta.TeamBackend(), ta.MemberName()))
	}
	// 3. _bind_trajectory_recorder（Python :705-711，15.11 create 接线）
	if binder, ok := runtimeValue.(interface {
		BindTrajectoryRecorder(*harnessproviders.HarnessTrajectoryRecorder)
	}); ok {
		subject := ta.ObservabilityExecutionSubject(sessionID)
		recorder, err := harnessproviders.CreateHarnessTrajectoryRecorder(
			&subject, subject.DisplayName, "team",
			map[string]attribute.Value{
				"agentteam.team.name":   attribute.StringValue(ta.TeamName()),
				"agentteam.member.name": attribute.StringValue(ta.MemberName()),
			},
		)
		if err == nil && recorder != nil {
			binder.BindTrajectoryRecorder(recorder)
		} else if err != nil {
			// Python: incomplete subject → warning（external_cli_spawn.py:487）
			logger.Warn(logComponent).
				Str("member_name", ta.MemberName()).
				Err(err).
				Msg("trajectory disabled for member: incomplete subject")
		}
		// recorder 为 nil（观测性未初始化）→ 静默，对齐 Python ImportError 路径
	}
	// 4. bind_reliability_context（Python :719-732）：仅 teammate_backend 非空时
	// 绑定（SDK 支撑的成员才注入可靠性投递面：failed 消息进 leader 信箱 +
	// 成员 ERROR 状态 + 重试进度事件）。
	if tb := ta.TeamBackend(); tb != nil {
		if binder, ok := runtimeValue.(interface {
			BindReliabilityContext(string, *tools.TeamBackend, string, externalcli.UpdateStatusCallback, messager.Messager)
		}); ok {
			binder.BindReliabilityContext(
				sessionID,
				tb,
				tb.ResolveLeaderMemberName(ctx),
				ta.UpdateStatus,
				ta.Messager(), // Python: teammate.infra.messager（external_cli_spawn.py:732）
			)
		}
	}
	// 5. _bind_protocol_member_team_tools（Python :712-722）：claude/codex
	// 分支体 ⤴️15.7/15.8（D-15.13-x / D-15.16-b3 挂点）；injectMCP 门卫由
	// provider 分支体落地时消费。
	return nil
}

// specOf 取成员壳的 spec（TeamAgent.Blueprint 尚未暴露 Spec()；
// binding 用的 spec/rtc 经 TeamAgent 运行时上下文回读）。
func specOf(ta *agent.TeamAgent) atschema.TeamAgentSpec {
	if bp := ta.Blueprint(); bp != nil {
		return bp.Spec
	}
	return atschema.TeamAgentSpec{}
}

// rtcOf 取成员壳的运行时上下文。
func rtcOf(ta *agent.TeamAgent) atschema.TeamRuntimeContext {
	if rc := ta.RuntimeContext(); rc != nil {
		return *rc
	}
	return atschema.TeamRuntimeContext{}
}

// stopExternalRuntime 停止 runtime（Python finally: await runtime.stop()）。
func stopExternalRuntime(runtimeValue spawn.ExternalCliMemberRuntime) {
	if stopper, ok := runtimeValue.(interface{ Stop() }); ok {
		stopper.Stop()
	}
}

// runExternalMemberTask 以成员身份运行团队（Python: Runner.run_agent_team(member=True)）。
func runExternalMemberTask(ctx context.Context, teammate spawn.ExternalCliTeamAgent, inputs map[string]any, sessionID string) (map[string]any, error) {
	ta, ok := teammate.(*agent.TeamAgent)
	if !ok {
		return nil, fmt.Errorf("external member task: teammate 必须是 *agent.TeamAgent，实际 %T", teammate)
	}
	return RunTeamMember(ctx, ta, inputs, sessionID)
}

// 编译期引用守卫（models 别名面在装配处被引用）。
var _ = models.TeamModelConfig{}
