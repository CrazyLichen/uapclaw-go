package external

// spawn 主体：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/spawn.py（988 行的 :63-537 与
// :651-985 段，一方法对一方法移植）。
//
// 职责：把第三方 CLI（provider 路的 dsh 等、adapter 路的 gemini/hermes/
// openclaw/codex-exec/generic）包装成团队成员运行时——构建入团描述符、
// 三级合成子进程环境、注入团队 MCP server（启动参数/带外注册/响亮告警三态）、
// 按 SupportsStdinInjection 分发流式/重起两种 CLI runtime，或对 provider 路
// 经既有注册缝构造 ExternalHarnessMemberRuntime。
//
// claude/codex SDK 分支体（spawn.py:308-466）依赖 15.7/15.8 的 harness
// 构造，本批以入口守卫拦截（偏差 D-15.13-x）；build_cli_workflow_runtime
// （:540-648）⤵️ swarmflow 相关章节；spawn/external_cli_spawn.py 的
// external_cli_spawn 编排主体已随 15.16b 落 spawn 包（external_cli_spawn.go
// + runtime/external_cli_spawn_wiring.go 注入缝）；本文件保留 build_cli_runtime
// 装配面（15.13 交付）。

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/agent"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/external/transport"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tracker"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Options BuildCliRuntime 的关键字参数面（Go struct 承载 Python 关键字默认值）。
//
// Python: build_cli_runtime 关键字参数 (spawn.py:200-229)。Go 无默认参数，
// 三个有 Python 默认值的布尔/切片字段以零值语义收窄（注释逐项标注）：
//   - InjectMCP：*bool，nil 取 Python 默认 true
//   - MCPServerName："" 取 TEAM_MCP_SERVER_NAME
//   - MCPServerCommand：nil 取 ["uapclawmcpserver"]
//   - SkillConflict："" 取 "skip"
//   - CommandOverride：nil ≈ Python None（Go ""≈None 合并语义，同 D-15.13-h）
type Options struct {
	// Cwd 子进程工作目录；"" ≈ Python None
	Cwd string
	// AddDirs 暴露给支持它们的 SDK 后端的额外目录
	AddDirs []string
	// CommandOverride 完整启动 argv 覆盖（仅 adapter 后端）
	CommandOverride []string
	// CLIPath SDK 与 provider 后端 CLI agent 的可执行路径
	CLIPath string
	// CodexBin Codex 可执行路径（legacy，仅 codex）
	CodexBin string
	// InjectMCP 是否注册团队 MCP server；nil 取 Python 默认 true
	InjectMCP *bool
	// MCPServerName 团队 MCP server 逻辑名；"" 取 TeamMCPServerName
	MCPServerName string
	// MCPServerCommand 团队 MCP stdio server 启动 argv；nil 取默认
	MCPServerCommand []string
	// MCPDefaultToolsApprovalMode Codex 专属：注入 server 的工具审批策略
	MCPDefaultToolsApprovalMode string
	// CodexBypassApprovalsAndSandbox Codex 专属；Python 默认 True（codex 分支
	// 本批守卫拦截，字段随 D-15.13-x 回收时生效）
	CodexBypassApprovalsAndSandbox bool
	// CodexTurnIdleTimeoutS Codex 专属：单轮无活动上限秒数
	CodexTurnIdleTimeoutS *float64
	// CodexTurnIdleRetries Codex 专属：无通知轮次重试次数
	CodexTurnIdleRetries *int
	// ClaudeTurnIdleTimeoutS Claude 专属：单轮无活动上限秒数
	ClaudeTurnIdleTimeoutS *float64
	// ClaudeMaxBufferSize Claude 专属：每行 stdout 缓冲上限字节
	ClaudeMaxBufferSize *int
	// ExternalModelConfig 模型端点配置
	ExternalModelConfig *schema.ExternalCliModelConfig
	// FallbackExternalModelConfig 认证回退端点
	FallbackExternalModelConfig *schema.ExternalCliModelConfig
	// PromoteFallbackModel 回退晋升回调；nil 对齐 Python None
	PromoteFallbackModel PromoteFallbackModel
	// SystemPrompt 成员的团队 rail 系统提示词
	SystemPrompt string
	// SystemPromptMode Claude/Codex 的 prompt 策略；"" 取 provider 默认
	SystemPromptMode string
	// Skills 复制进本地 CLI 项目的技能包
	Skills []harnessproviders.SkillSource
	// SkillConflict 同名项目技能处理策略；"" 取 "skip"
	SkillConflict string
	// ExtraEnv 叠加在继承 env 之上的额外环境变量
	ExtraEnv map[string]string
	// SshTransport 可选 ssh 端点配置（仅 Claude SDK 后端支持）
	SshTransport *schema.SshTransportConfig
	// ResumeExternalBackend 是否恢复后端原生会话
	ResumeExternalBackend bool
	// MemberAgentID 寻址本成员 checkpoint 的稳定 TeamAgent card id
	MemberAgentID string
	// TeamContextTracker 决定哪些团队状态尚未告知本成员的 tracker；
	// nil 关闭团队状态投递
	TeamContextTracker *tracker.TeamContextTracker
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterMCPOutOfBand 对无启动注入 flag 的 CLI 带外注册团队 MCP server。
// 某些 CLI（gemini、hermes）经持久化到自身配置的子命令注册 MCP server 而非
// 启动 flag。尽力执行一次该命令使成员仍拿到团队工具；adapter 连注册机制都没有
// 时记响亮告警而不是静默留下没有团队工具的成员。
// Python async（create_subprocess_exec + communicate）→ Go 同步 exec（偏差
// D-15.13-a 同源：asyncio 子进程 → os/exec）。
//
// Python: _register_mcp_out_of_band (spawn.py:151-198)
func RegisterMCPOutOfBand(
	adapter *CliAgentAdapter,
	serverName string,
	serverCommand []string,
	env map[string]string,
	cwd string,
	memberName string,
) {
	registerCmd := adapter.MCPRegisterCommand(serverName, serverCommand)
	if registerCmd == nil {
		// Python: team_logger.warning("[external-cli] {} cannot auto-inject ...")
		logger.Warn(logComponent).
			Str("adapter", adapter.Name).
			Str("member", memberName).
			Msg("[external-cli] cannot auto-inject the team MCP server (no launch flag or registration command); member will lack team tools unless registered out of band")
		return
	}
	// Python: team_logger.info("[external-cli] registering team MCP for member {} via {}", ...)
	logger.Info(logComponent).
		Str("member", memberName).
		Strs("register_cmd", registerCmd).
		Msg("[external-cli] registering team MCP for member")
	cmd := exec.Command(registerCmd[0], registerCmd[1:]...)
	// Python env=env 传入完整替换环境
	cmd.Env = envSlice(env)
	cmd.Dir = cwd
	// Python stdout=PIPE 且 communicate() 读掉（值被丢弃）；stderr 捕获供尾部诊断
	var stderrBuf strings.Builder
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		// Python: except (OSError, ValueError) → launch 失败告警
		logger.Warn(logComponent).
			Str("adapter", adapter.Name).
			Err(err).
			Msg("[external-cli] team MCP registration failed to launch")
		return
	}
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		// Python: tail = stderr.decode(...)[:][-500:]
		tail := stderrTail500(stderrBuf.String())
		logger.Warn(logComponent).
			Str("adapter", adapter.Name).
			Int("returncode", code).
			Str("stderr_tail", tail).
			Msg("[external-cli] team MCP registration exited non-zero")
	}
}

// BuildCliRuntime 为 rtc.CLIAgent 构建成员运行时。
// Claude 与 Codex 由各自的 SDK 后端处理（本批入口守卫拦截，D-15.13-x）。
// 其他 CLI 按 backend_for 分发：provider 路（dsh 等）构造
// ExternalHarnessMemberRuntime；adapter 路按 SupportsStdinInjection 分发——
// 流式 CLI 启动一个长驻子进程返回 ExternalCliRuntime，one-shot CLI（openclaw/
// hermes）返回每轮重起新子进程的 ReinvokeCliRuntime。返回的 runtime 拥有其
// 子进程；Stop 收尾。Python 返回联合类型
// MemberRuntimeLike = CliRuntimeBase | ExternalHarnessMemberRuntime（同一
// 鸭子面），Go 以 agent.MemberRuntime 接口承载（spec §5 登记承载方式差异）；
// 外部第三方 provider 运行时未来也须满足该接口。
//
// Python: build_cli_runtime (spawn.py:200-537)
func BuildCliRuntime(ctx context.Context, rtc *schema.TeamRuntimeContext, opts Options) (agent.MemberRuntime, error) {
	// 步骤 1：cli_agent 判空（:297-301）
	if rtc.CLIAgent == "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("build_cli_runtime called without ctx.cli_agent set"))
	}
	// 步骤 2：描述符构建 + session_id 二次校验（:302-307）
	descriptor, err := DescriptorFromContext(ctx, rtc)
	if err != nil {
		return nil, err
	}
	if descriptor.SessionID == "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("external CLI runtime requires session_id in context"))
	}
	// 步骤 3：claude/codex SDK 分支入口守卫（D-15.13-x）。Python spawn.py:308-466
	// 两分支体（含分支内互斥校验与 _build_claude/_build_codex_member_runtime，
	// :736-894）⤵️15.7/15.8，实施即删守卫写分支。
	if rtc.CLIAgent == "claude" || rtc.CLIAgent == "codex" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg(fmt.Sprintf(
				"external CLI member '%s': claude/codex SDK backends are pending 15.7/15.8 (D-15.13-x)",
				rtc.CLIAgent)))
	}
	// 步骤 4：后端查找与 provider 路分发（:405-441）
	backend := BackendFor(rtc.CLIAgent)
	if backend != nil && backend.Kind == BackendKindProvider {
		// provider 不收的 7 项 SDK/adapter 专属配置，逐项收集、一次性报出
		unsupported := map[string]bool{
			"command_override":           opts.CommandOverride != nil,
			"codex_bin":                  opts.CodexBin != "",
			"ssh_transport":              opts.SshTransport != nil,
			"claude_turn_idle_timeout_s": opts.ClaudeTurnIdleTimeoutS != nil,
			"claude_max_buffer_size":     opts.ClaudeMaxBufferSize != nil,
			"codex_turn_idle_timeout_s":  opts.CodexTurnIdleTimeoutS != nil,
			"codex_turn_idle_retries":    opts.CodexTurnIdleRetries != nil,
		}
		var configured []string
		for name, set := range unsupported {
			if set {
				configured = append(configured, name)
			}
		}
		sort.Strings(configured)
		if len(configured) > 0 {
			return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
				exception.WithMsg(fmt.Sprintf(
					"harness provider member '%s' does not support: %s",
					rtc.CLIAgent, strings.Join(configured, ", "))))
		}
		return BuildProviderMemberRuntime(rtc, descriptor, opts)
	}
	// 步骤 5：adapter 路五项前置校验（:442-466），报文逐字对齐 Python
	if opts.SshTransport != nil {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("ssh transport is only supported for claude SDK external CLI members"))
	}
	if opts.CodexBin != "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("codex_bin is only supported for Codex SDK members"))
	}
	if opts.CLIPath != "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("cli_path is only supported for Claude and Codex SDK members"))
	}
	if opts.ClaudeTurnIdleTimeoutS != nil {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("claude_turn_idle_timeout_s is only supported for Claude SDK members"))
	}
	if opts.ClaudeMaxBufferSize != nil {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("claude_max_buffer_size is only supported for Claude SDK members"))
	}
	// 步骤 6：adapter 解析（:468）
	adapter, err := BuildAdapter(rtc.CLIAgent, opts.CommandOverride)
	if err != nil {
		return nil, err
	}
	// 步骤 7：env 三级合成（:469-478）。从继承环境起步，剥掉 adapter 声明的
	// 父 agent 会话标记前缀；描述符 env 是团队身份权威，所以最后施加——
	// 配错的 extra_env 不能遮蔽 join。
	baseEnv := make(map[string]string)
	for _, kv := range os.Environ() {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		key, value := kv[:i], kv[i+1:]
		stripped := false
		for _, prefix := range adapter.EnvStripPrefixes {
			if strings.HasPrefix(key, prefix) {
				stripped = true
				break
			}
		}
		if !stripped {
			baseEnv[key] = value
		}
	}
	env := mergeEnv(baseEnv, opts.ExtraEnv, descriptor.ToEnv())
	env = WithHomeEnv(env)

	// 步骤 8：system prompt 作为启动参数（:480-482）。无 flag 的 CLI 返回空，
	// 调用方改为把 prompt 拼到成员首条用户消息前。
	spArgs := adapter.SystemPromptArgs(opts.SystemPrompt)

	// 步骤 9：MCP 注入三态（:484-498）——启动参数拼接 / 带外注册 / 响亮告警
	var mcpArgs []string
	injectMCP := opts.InjectMCP == nil || *opts.InjectMCP
	if injectMCP {
		mcpServerName := opts.MCPServerName
		if mcpServerName == "" {
			mcpServerName = TeamMCPServerName
		}
		mcpServerCommand := opts.MCPServerCommand
		if mcpServerCommand == nil {
			mcpServerCommand = schema.DefaultMCPServerCommand()
		}
		mcpArgs = adapter.MCPLaunchArgs(mcpServerName, mcpServerCommand)
		if len(mcpArgs) == 0 {
			// 本 CLI 无启动注入 flag：带外注册团队 MCP server（如 gemini mcp add），
			// 或在无法注册时响亮告警
			RegisterMCPOutOfBand(adapter, mcpServerName, mcpServerCommand, env, opts.Cwd,
				rtc.MemberName)
		}
	}

	// 步骤 10：启动附加参数（:500）
	launchExtraArgs := append(append([]string{}, mcpArgs...), spArgs...)

	// 步骤 11a：one-shot CLI——不抢先启动；runtime 按轮启动并把 MCP 注册参数
	// 加进每次调用。canonical UUID 是成员跨轮的稳定会话 id（按 id 恢复的 CLI，
	// 如 gemini --session-id / --resume，要求真 UUID）。
	// Python: str(uuid.uuid4())（:512，带连字符形态）
	if !adapter.SupportsStdinInjection {
		rt := NewReinvokeCliRuntime(
			rtc.MemberName, adapter, env, opts.Cwd,
			uuid.New().String(), launchExtraArgs, opts.MemberAgentID)
		rt.BindTeamContextTracker(opts.TeamContextTracker)
		return rt, nil
	}

	// 步骤 11b：流式 CLI——启动长驻子进程并绑定 stdin/stdout（:518-537）
	command := adapter.BuildCommand(launchExtraArgs)
	// Python: team_logger.info("[external-cli] launching {} for member {}", ...)
	logger.Info(logComponent).
		Strs("command", command).
		Str("member", rtc.MemberName).
		Msg("[external-cli] launching for member")
	tr := &transport.LocalTransport{}
	process, err := tr.Run(command, env, opts.Cwd)
	if err != nil {
		return nil, err
	}
	if process.Stdin() == nil || process.Stdout() == nil {
		// Python 不先回收进程（保持既有行为）；pipe 缺失按执行错误报出（:522-526）
		return nil, exception.RaiseError(exception.StatusAgentTeamExecutionError,
			exception.WithMsg(fmt.Sprintf(
				"external CLI '%s' did not expose stdin/stdout pipes", rtc.CLIAgent)))
	}
	rt := NewExternalCliRuntime(
		rtc.MemberName, adapter, NewStdinPipeInjector(process.Stdin()),
		process.Stdout(), process, tr, opts.MemberAgentID)
	rt.BindTeamContextTracker(opts.TeamContextTracker)
	return rt, nil
}

// MemberContext 构建外部 SDK 成员的 provider 中立启动上下文。
// agent_id 回落 "{team}_{member}"（Python descriptor.team_name + member_name）。
//
// Python: _member_context (spawn.py:651-668)
func MemberContext(
	rtc *schema.TeamRuntimeContext,
	descriptor *TeamJoinDescriptor,
	memberAgentID string,
	systemPrompt string,
	cwd string,
) (*harnessprotocol.HarnessContext, error) {
	memberName := rtc.MemberName
	agentID := memberAgentID
	if agentID == "" {
		agentID = descriptor.TeamName + "_" + memberName
	}
	var cwdPtr *string
	if cwd != "" {
		cwdPtr = &cwd
	}
	ctxValue, err := harnessprotocol.NewHarnessContext(
		memberName, agentID, descriptor.SessionID, systemPrompt,
		harnessprotocol.ContextOptions{
			Cwd: cwdPtr,
			Metadata: harnessprotocol.JsonObject{
				"team_name": descriptor.TeamName,
				"role":      string(rtc.Role),
			},
		})
	if err != nil {
		return nil, err
	}
	return &ctxValue, nil
}

// IsNativeEndpoint 成员是否跑在 CLI 自身登录上。
// 只有这样的成员才吃 auth fallback：已经在端点上（池分配或静态端点配置）的
// 成员没有可回退的东西。内置模型选择不携带 api_base 也不携带 provider。
//
// Python: _is_native_endpoint (spawn.py:671-678)
func IsNativeEndpoint(config *schema.ExternalCliModelConfig) bool {
	return config == nil || (config.APIBase == "" && config.Provider == "")
}

// ModelChoice 把团队的模型配置翻译成 provider 中立的 ModelChoice。
// 既无 api_base 也无 provider 的配置指内置模型（CLI 自身登录）；否则由该
// 端点服务。Go 函数名与 harnessprotocol.ModelChoice 类型同形，限定符可区分。
//
// Python: model_choice (spawn.py:681-692)
func ModelChoice(config *schema.ExternalCliModelConfig) *harnessprotocol.ModelChoice {
	if config == nil {
		return nil
	}
	var endpoint *harnessprotocol.ModelEndpoint
	if !IsNativeEndpoint(config) {
		endpoint = &harnessprotocol.ModelEndpoint{
			APIBase:  strPtrIfSet(config.APIBase),
			APIKey:   strPtrIfSet(config.APIKey),
			Provider: strPtrIfSet(config.Provider),
		}
	}
	return &harnessprotocol.ModelChoice{
		Model:    strPtrIfSet(config.Model),
		Effort:   strPtrIfSet(config.Effort),
		Endpoint: endpoint,
	}
}

// BuildTeamContextTracker 构建把团队状态喂进本 CLI 成员消息的 tracker。
// 外部 CLI 没有 rail，runtime 把 tracker 输出折叠进它发送的下一条消息。与
// 进程内成员不同，外部 CLI 的 cwd 从不挂载团队工作空间（setup_agent 在
// mount_into_workspace 前短路），任何 agent 相对挂载串都是成员不可达的路径——
// 所以只暴露共享工作空间的绝对路径，成员直接写那里。workspace 未启用时
// team_workspace_path 传空串（Python None 的 Go 形态）。
//
// Python: _build_team_context_tracker (spawn/external_cli_spawn.py:194-236)
func BuildTeamContextTracker(
	teamBackend *tools.TeamBackend,
	spec *schema.TeamAgentSpec,
	rtc *schema.TeamRuntimeContext,
	memberName string,
	teamName string,
) *tracker.TeamContextTracker {
	language := "cn"
	if rtc.TeamSpec != nil && rtc.TeamSpec.Language != "" {
		language = rtc.TeamSpec.Language
	}
	workspaceEnabled := spec.Workspace != nil && spec.Workspace.Enabled
	teamWorkspacePath := ""
	if workspaceEnabled {
		// Python: _team_workspace_path(spec, team_name) if workspace_enabled else None
		teamWorkspacePath = workspacePathFor(spec, teamName)
	}
	return tracker.NewTeamContextTracker(
		teamBackend,
		memberName,
		rtc.Role,
		rtc.DisplayName,
		"", // member_workspace_path：外部 CLI 成员无私有挂载（Python 不传，默认空）
		rtc.Prompt,
		teamWorkspacePath,
		buildContextTeamOutputsDir(spec),
		spec.ExposeHumanAgentsToTeammates,
		language,
		"", // fork_source：普通 spawn
		nil,
	)
}

// BuildProviderMemberRuntime 在第三方 harness provider 上构建成员运行时。
// 成员的模型像 Claude 与 Codex 一样抵达 provider：作为经 provider model_config
// 映射的 ModelChoice。spawn 路只在 card 声明 ENDPOINT_MODELS 时交出端点模型，
// 认证回退只在同时声明 AUTH_FALLBACK 时交出；切换像 Claude/Codex 一样持久化。
// provider 只收到所有生成 provider 包共享的配置键（cwd/env/inherit_process_env/
// skills/skill_conflict），外加团队配置了的 cli_path 与 system_prompt_mode。
// 团队工具经 uapclawmcpserver stdio server 抵达成员，它从环境继承入团
// 描述符；card 不声明 MCP 工具的 provider 无团队工具运行。
//
// Python: _build_provider_member_runtime (spawn.py:897-985)
func BuildProviderMemberRuntime(
	rtc *schema.TeamRuntimeContext,
	descriptor *TeamJoinDescriptor,
	opts Options,
) (*ExternalHarnessMemberRuntime, error) {
	providerName := rtc.CLIAgent
	// Python: env = _with_home_env({**dict(os.environ), **(extra_env or {}), **descriptor.to_env()})
	env := mergeEnv(osEnvelope(), opts.ExtraEnv, descriptor.ToEnv())
	env = WithHomeEnv(env)
	// 五公共键（:936-942）：cwd/env/inherit_process_env/skills/skill_conflict
	config := map[string]any{
		"cwd":                 anyPtrString(opts.Cwd),
		"env":                 env,
		"inherit_process_env": false,
		"skills":              opts.Skills,
		"skill_conflict":      skillConflictOf(opts),
	}
	// 条件键（:943-946）
	if opts.CLIPath != "" {
		config["cli_path"] = opts.CLIPath
	}
	if opts.SystemPromptMode != "" {
		config["system_prompt_mode"] = opts.SystemPromptMode
	}
	// 模型映射并入（:947）
	model, err := memberModelConfig(providerName, opts.ExternalModelConfig, opts.FallbackExternalModelConfig)
	if err != nil {
		return nil, err
	}
	for key, value := range model {
		config[key] = value
	}
	// resolve_provider(provider_name).create(config)（:948）——走既有注册缝
	provider, err := harnessproviders.ResolveProvider(providerName)
	if err != nil {
		return nil, err
	}
	jsonConfig, err := harnessprotocol.FreezeJsonObject(config)
	if err != nil {
		return nil, err
	}
	harness, err := provider.Create(jsonConfig)
	if err != nil {
		return nil, err
	}
	// Python: team_logger.info("[external-cli] preparing provider member ...")
	logger.Info(logComponent).
		Str("member", rtc.MemberName).
		Str("provider", providerName).
		Str("cwd", opts.Cwd).
		Bool("cli_path_configured", opts.CLIPath != "").
		Bool("inject_mcp", opts.InjectMCP == nil || *opts.InjectMCP).
		Msg("[external-cli] preparing provider member")
	// 上下文与 runtime 组装（:957-966）
	memberCtx, err := MemberContext(rtc, descriptor, opts.MemberAgentID, opts.SystemPrompt, opts.Cwd)
	if err != nil {
		return nil, err
	}
	mcpServerName := opts.MCPServerName
	if mcpServerName == "" {
		mcpServerName = TeamMCPServerName
	}
	runtime, err := NewExternalHarnessMemberRuntime(
		harness,
		func(any) (*harnessprotocol.HarnessContext, error) { return memberCtx, nil },
		ExternalHarnessMemberRuntimeOptions{
			TeamContextTracker:    opts.TeamContextTracker,
			ResumeExternalBackend: opts.ResumeExternalBackend,
			AgentKind:             providerName,
			CLIPath:               opts.CLIPath,
			InjectMCP:             opts.InjectMCP == nil || *opts.InjectMCP,
			MCPServerName:         mcpServerName,
		})
	if err != nil {
		return nil, err
	}
	// 回退晋升绑定（:967）
	runtime.BindFallbackPromotion(opts.PromoteFallbackModel)
	// MCP 注入两分支（:968-984）
	if (opts.InjectMCP == nil || *opts.InjectMCP) && harness.Card().Supports(harnessprotocol.HarnessCapabilityMcpTools) {
		mcpServerCommand := opts.MCPServerCommand
		if mcpServerCommand == nil {
			mcpServerCommand = schema.DefaultMCPServerCommand()
		}
		// env 只传 MCP_SERVER_ENV_VARS 交集（:975）
		mcpEnv := make(map[string]string)
		for _, key := range MCPServerEnvVars {
			if value, ok := env[key]; ok {
				mcpEnv[key] = value
			}
		}
		runtime.BindMCPServers([]harnessprotocol.McpServerConfig{{
			Name:      mcpServerName,
			Transport: harnessprotocol.McpTransportStdio,
			Command:   mcpServerCommand,
			Env:       mcpEnv,
		}})
	} else if opts.InjectMCP == nil || *opts.InjectMCP {
		// Python: team_logger.warning("[external-cli] provider {} declares no MCP tools; ...")
		logger.Warn(logComponent).
			Str("provider", providerName).
			Str("member", rtc.MemberName).
			Msg("[external-cli] provider declares no MCP tools; member runs without team tools")
	}
	return runtime, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberModelConfig 把成员的模型（与认证回退）经其 provider 映射。
// 回退只守卫跑在 CLI 自身登录上的成员：已在端点上的成员没有可回退的东西，
// 端点形态下回退被丢弃。仅回退被选中时 pop 掉 model 键（成员保持 CLI 默认模型）。
// provider 拒绝映射时 ValueError → AGENT_TEAM_CONFIG_INVALID（cause 链保留）。
//
// Python: _member_model_config (spawn.py:695-720)
func memberModelConfig(
	providerName string,
	primary *schema.ExternalCliModelConfig,
	fallback *schema.ExternalCliModelConfig,
) (map[string]any, error) {
	choice := ModelChoice(primary)
	var fallbackChoice *harnessprotocol.ModelChoice
	if IsNativeEndpoint(primary) {
		fallbackChoice = ModelChoice(fallback)
	}
	if choice == nil && fallbackChoice == nil {
		return map[string]any{}, nil
	}
	// Python: provider_model_config(provider_name, choice or ModelChoice(), fallback=fallback_choice)
	effectiveChoice := choice
	if effectiveChoice == nil {
		effectiveChoice = &harnessprotocol.ModelChoice{}
	}
	mapped, err := harnessproviders.ProviderModelConfig(providerName, *effectiveChoice, fallbackChoice)
	if err != nil {
		// Python: except ValueError → raise_error(AGENT_TEAM_CONFIG_INVALID, cause=exc)
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg(err.Error()),
			exception.WithCause(err))
	}
	if choice == nil {
		// 仅回退被选中：成员保持 CLI 默认模型（:717-719）
		delete(mapped, "model")
	}
	return mapped, nil
}

// claudeProcessEnv Claude 子进程的进程环境；workflow 成员的 join_env 为空。
// ssh_transport 非 nil 时 base_env 清空（远程环境完全自持）。
// strip_parent_claude_env 属 harness_providers/claudecode（Python options.py，
// :55），Go 侧随 15.7 落地（⤴️15.7）——本批 claude 分支被入口守卫拦截（
// D-15.13-x），此处以全量继承环境起步并登记待回填。
//
// Python: _claude_process_env (spawn.py:723-728)
func claudeProcessEnv(extraEnv map[string]string, joinEnv map[string]string, sshTransport *schema.SshTransportConfig) map[string]string {
	var baseEnv map[string]string
	if sshTransport != nil {
		baseEnv = map[string]string{}
	} else {
		// ⤴️15.7：Python 为 strip_parent_claude_env(dict(os.environ))
		baseEnv = osEnvelope()
	}
	return WithHomeEnv(mergeEnv(baseEnv, extraEnv, joinEnv))
}

// codexProcessEnv Codex 子进程的进程环境；workflow 成员的 join_env 为空。
//
// Python: _codex_process_env (spawn.py:731-733)
func codexProcessEnv(extraEnv map[string]string, joinEnv map[string]string) map[string]string {
	return WithHomeEnv(mergeEnv(osEnvelope(), extraEnv, joinEnv))
}

// mergeEnv 依次叠加多张环境映射（后者覆盖前者），返回新 map 不改入参。
// Python: {**a, **b, **c} 字典合并的 Go 等价。
func mergeEnv(sources ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, source := range sources {
		for key, value := range source {
			out[key] = value
		}
	}
	return out
}

// envSlice 环境映射转 exec 所需的 KEY=VALUE 切片。
// Python 传 dict 给 create_subprocess_exec(env=) 的 Go 等价。
func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

// stderrTail500 取文本尾 500 字节（按字节，对齐 Python 切片 [-500:]）。
func stderrTail500(text string) string {
	if len(text) <= 500 {
		return text
	}
	return text[len(text)-500:]
}

// strPtrIfSet 空串取 nil（Python None 字段语义），非空取指针。
func strPtrIfSet(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// anyPtrString 空串取 any(nil)（Python None 进配置 dict），非空取字符串。
func anyPtrString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// skillConflictOf SkillConflict 空值回落 Python 默认 "skip"。
func skillConflictOf(opts Options) string {
	if opts.SkillConflict == "" {
		return "skip"
	}
	return opts.SkillConflict
}

// workspacePathFor 团队共享工作区绝对路径（paths.go 同名助手的 spawn 侧别名，
// Python _team_workspace_path@260-264 已在 paths.go 落地，此处复用）。
func workspacePathFor(spec *schema.TeamAgentSpec, teamName string) string {
	return teamWorkspacePath(spec, teamName)
}
