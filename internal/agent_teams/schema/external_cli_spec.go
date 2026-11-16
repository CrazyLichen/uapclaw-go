package schema

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// AdapterNamesFunc 内置 CLI adapter 名单的注册钩子。
// Python 校验器在函数体内惰性 import adapter 注册表
// （team.py:474、:500，惰性 import 属偏差 D-15.13-c）；Go 侧 schema 包不能反向
// import externalcli（externalcli 依赖 schema），故以钩子变量承载同一数据源，
// 由 externalcli 包在 init 中注册。未注册时视为空名单（校验降级为无 adapter 判定）。
var AdapterNamesFunc func() []string

// adapterNames 安全取内置 adapter 名单。
func adapterNames() []string {
	if AdapterNamesFunc == nil {
		return nil
	}
	return AdapterNamesFunc()
}

// isAdapterBacked 判断给定后端名是否为内置 adapter（Python available_adapters 判定）。
func isAdapterBacked(cliAgent string) bool {
	for _, name := range adapterNames() {
		if name == cliAgent {
			return true
		}
	}
	return false
}

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalCliModelConfig SDK 外部 CLI 成员的模型端点配置。
//
// Python: ExternalCliModelConfig (team.py:284)
type ExternalCliModelConfig struct {
	// Provider 目标 CLI 运行时使用的逻辑 provider 名
	Provider string `json:"provider,omitempty"`
	// Model 传给目标 CLI 运行时的模型名
	Model string `json:"model,omitempty"`
	// APIBase 目标模型 API 的 base URL
	APIBase string `json:"api_base,omitempty"`
	// APIKey 注入目标 CLI 子进程环境的 API key
	APIKey string `json:"api_key,omitempty"`
	// Effort 传给目标 CLI 运行时的推理力度（如 "low"/"high"）
	Effort string `json:"effort,omitempty"`
}

// ExternalCliBuiltinModel CLI 自带登录可用的一个模型（如订阅内模型）。
// 声明在 ExternalCliAgentSpec.BuiltinModels 上；leader 只能挑选这里列出的
// 模型与力度。对应 provider 的 HarnessModelControl.list_models 报告真实登录
// 可用面，可用于编写本目录。
//
// Python: ExternalCliBuiltinModel (team.py:310)
type ExternalCliBuiltinModel struct {
	// Name CLI 接受的模型 id 或别名（"sonnet"、"gpt-5.5"）
	Name string `json:"name"`
	// Description leader 何时应选这个模型；展示在工具目录里
	Description string `json:"description,omitempty"`
	// Efforts 可选的推理力度列表；空表示该模型无力度维度
	Efforts []string `json:"efforts,omitempty"`
	// DefaultEffort leader 未指定力度时的默认值；空表示保持 CLI 默认
	DefaultEffort string `json:"default_effort,omitempty"`
}

// ExternalCliAgentSpec 一类外部 CLI agent 的静态启动配置。
// 预先声明在 TeamAgentSpec.external_cli_agents 上，运行时的
// spawn_member(role_type='external_cli', cli_agent=...) 只需要报 CLI 种类——
// 全部启动知识都在这里而不在 spawn 调用里。一个条目配置所有以同名 cli_agent
// spawn 的成员（每个成员仍各有一个自己的子进程与入团身份）。
//
// Python: ExternalCliAgentSpec (team.py:331)
type ExternalCliAgentSpec struct {
	// CLIAgent 外部 agent 种类标识（"claude"/"codex"/"openclaw"/"hermes"/
	// 第三方 harness provider 名）。"claude" 选 SDK 后端，"codex" 选 Codex
	// Python SDK 后端，内置 adapter 名走其子进程 adapter，其他值走经
	// openjiuwen.harness_providers 入口点安装的第三方 harness provider。
	// Python: cli_agent (team.py:343)
	CLIAgent string `json:"cli_agent"`
	// Skills 本地 CLI 成员的 portable skill 目录或 manifest SkillSpec 映射。
	// Python: skills (team.py:354)
	Skills []any `json:"skills,omitempty"`
	// SkillConflict 同名项目技能的处理策略："skip"/"replace"/"append"。
	// Python: skill_conflict (team.py:356)
	SkillConflict string `json:"skill_conflict,omitempty"`
	// SystemPromptMode Claude/Codex 的 prompt 策略："append"/"replace"；
	// 空保持各 provider 默认。Python: system_prompt_mode (team.py:359)
	SystemPromptMode string `json:"system_prompt_mode,omitempty"`
	// Command 覆盖 adapter 后端内置 command 的完整启动 argv。
	// SDK 后端不接受完整 argv：Claude 用 SDK 默认，Codex 需要自定义可执行时用
	// CodexBin。Python: command (team.py:364)
	Command []string `json:"command,omitempty"`
	// CLIPath SDK 与 provider 后端 CLI agent 的可执行路径（可选）。
	// Claude 传给 ClaudeAgentOptions.cli_path；Codex 映射到 CodexConfig.codex_bin；
	// 第三方 harness provider 收到 cli_path 配置键。adapter 后端应继续用
	// Command 覆盖完整启动 argv。Python: cli_path (team.py:371)
	CLIPath string `json:"cli_path,omitempty"`
	// CodexBin 传给 CodexConfig.codex_bin 的 Codex 可执行路径（可选）。
	// 该 legacy 字段仅对 cli_agent="codex" 合法；新配置优先用 CLIPath。
	// Codex SDK 仍负责构造 app-server 参数。
	// Python: codex_bin (team.py:379)
	CodexBin string `json:"codex_bin,omitempty"`
	// Cwd CLI 子进程的工作目录。空继承团队进程 cwd；设为团队工作空间可让
	// CLI 的原生文件写落进共享工作空间。
	// Python: cwd (team.py:385)
	Cwd string `json:"cwd,omitempty"`
	// InjectMCP spawn 路径是否向 CLI 自动注册团队 MCP server，使它拿到
	// view_task/claim_task/send_message 等团队协作工具。注入方式与后端相关
	// （Claude SDK MCP options、codex -c mcp_servers...）；没有注入策略的
	// adapter 忽略之。
	// Python: inject_mcp (team.py:390)
	InjectMCP bool `json:"inject_mcp"`
	// MCPDefaultToolsApprovalMode 注入 MCP server 暴露工具的 Codex 审批策略
	// （"auto"/"prompt"/"writes"/"approve"，可选）。空时对注入的团队 MCP server
	// 使用 "approve"，使模型 provider 不支持 Codex auto-review 时其工具仍可用。
	// 显式值只覆盖注入 server 的默认，不改变 shell 命令、其他 MCP server 或
	// 非 Codex 后端的审批行为。
	// Python: mcp_default_tools_approval_mode (team.py:396)
	MCPDefaultToolsApprovalMode string `json:"mcp_default_tools_approval_mode,omitempty"`
	// CodexBypassApprovalsAndSandbox Codex 成员无审批提示、无 SDK sandbox 运行。
	// Codex 成员默认开启，与 Claude 成员的 bypassPermissions 行为对齐；显式设
	// false 可恢复 Codex 审批与 sandbox。仅对 cli_agent="codex" 合法。
	// 用指针承载"显式设置"语义（Python 依 model_fields_set 判定默认注入，
	// team.py:473-476；模型 dump 往返会显式带出 false）。
	// Python: codex_bypass_approvals_and_sandbox (team.py:406)
	CodexBypassApprovalsAndSandbox *bool `json:"codex_bypass_approvals_and_sandbox,omitempty"`
	// CodexTurnIdleTimeoutS Codex 单轮无活动上限秒数（可选，>0）。
	// 未设置用运行时默认；任何 SDK 通知（包括可重试的传输错误）刷新计时器。
	// Python: codex_turn_idle_timeout_s (team.py:417)
	CodexTurnIdleTimeoutS *float64 `json:"codex_turn_idle_timeout_s,omitempty"`
	// CodexTurnIdleRetries 无 SDK 通知的 Codex 轮次重试次数（可选，>=0）。
	// 重试复用同一线程，仅在卡住轮次被成功中断后尝试；产生过通知的轮次不重放
	// （可能已有外部副作用）。
	// Python: codex_turn_idle_retries (team.py:425)
	CodexTurnIdleRetries *int `json:"codex_turn_idle_retries,omitempty"`
	// ClaudeTurnIdleTimeoutS Claude 单轮无活动上限秒数（可选，>0）。
	// 任何 SDK 消息（assistant / user 工具结果 / system）刷新计时器；消息流
	// 卡过上限的轮次被中断并以 network_timeout 失败收尾，使成员回到 READY 而
	// 不是永远挂着。
	// Python: claude_turn_idle_timeout_s (team.py:433)
	ClaudeTurnIdleTimeoutS *float64 `json:"claude_turn_idle_timeout_s,omitempty"`
	// ClaudeMaxBufferSize Claude SDK 传输的每行 stdout 缓冲上限字节（可选，>=1）；
	// 空保持 harness 默认（32 MiB）。SDK 按 NDJSON（一行一条消息）读取，超限
	// 单行以 decode error 拒收、杀掉本轮。内嵌大载荷的工具结果（CLI Read 读入
	// 的 base64 图片约为文件大小 4/3）会突破 SDK 默认 1 MiB，因此有效默认远高于
	// 它。缓冲是瞬态的单行字符串，内存开销只出现在大消息在途期间。
	// Python: claude_max_buffer_size (team.py:442)
	ClaudeMaxBufferSize *int `json:"claude_max_buffer_size,omitempty"`
	// MCPServerCommand 注册给 CLI 的团队 MCP stdio server 启动 argv。
	// 默认为 openjiuwen-team-mcp console-script 入口。
	// Python: mcp_server_command (team.py:458)
	MCPServerCommand []string `json:"mcp_server_command,omitempty"`
	// Env CLI 子进程的额外环境变量，叠加在继承的进程 env 之上
	// （团队 join 描述符另行注入）。
	// Python: env (team.py:463)
	Env map[string]string `json:"env,omitempty"`
	// ExternalModelConfig SDK 外部 CLI 成员的模型端点配置（可选）。
	// 公开 YAML 键是 model_config；Python 属性名 external_model_config 是为避开
	// Pydantic 类级 model_config 设置。
	// Python: external_model_config (team.py:468，alias="model_config")
	ExternalModelConfig *ExternalCliModelConfig `json:"model_config,omitempty"`
	// BuiltinModels CLI 自带登录可用、leader 可挑选的模型目录。
	// 非空时启用 spawn_teammate 的 external_cli.builtin_model/effort 与本类
	// 成员的 set_member_model 工具；空则成员停留在 model_config/池/CLI 默认。
	// 适用于 Claude、Codex 与第三方 harness provider（其 card 须声明
	// MODEL_SELECTION 才能让成员跑一个）；不适用于 adapter 后端。
	// Python: builtin_models (team.py:479)
	BuiltinModels []ExternalCliBuiltinModel `json:"builtin_models,omitempty"`
	// SshTransport 可选 ssh 端点，用于在远程 host 上启动本 CLI。
	// 设置后 command/cwd/env/mcp_server_command 都在远程 host 上解释；
	// 团队 join 描述符仍经 OPENJIUWEN_TEAM_JOIN 注入，使远程 DB 与 messager
	// 端点可达时 stdio MCP 子进程能继承本成员身份。
	// Python: ssh_transport (team.py:489)
	SshTransport *SshTransportConfig `json:"ssh_transport,omitempty"`
}

// ExternalCliMemberSpec 由一份既有外部 CLI 配置支撑的预定义队友。
//
// Python: ExternalCliMemberSpec (team.py:536)
type ExternalCliMemberSpec struct {
	// RoleType 角色类型固定 external_cli
	RoleType string `json:"role_type"`
	// ExternalCLI 既有外部 CLI 配置
	ExternalCLI ExternalCliAgentSpec `json:"external_cli"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Validate 校验后端启动覆盖的归属（保持 SDK 二进制选择与 adapter argv 覆盖分离）。
// 逐条对齐 Python _validate_backend_launch_override (team.py:492-534)。
func (s *ExternalCliAgentSpec) Validate() error {
	// codex 成员默认开启 bypass（Python model_fields_set 判定，:473-476）
	if s.CLIAgent == "codex" && s.CodexBypassApprovalsAndSandbox == nil {
		def := true
		s.CodexBypassApprovalsAndSandbox = &def
	}
	if s.CLIAgent == "codex" && s.Command != nil {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("Codex SDK config does not support command; use cli_path to select a custom executable"))
	}
	if s.CLIPath != "" && isAdapterBacked(s.CLIAgent) {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("cli_path is not valid for adapter-backed CLI agents; use command instead"))
	}
	if s.CLIAgent != "codex" && s.CodexBin != "" {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("codex_bin is only valid when cli_agent='codex'"))
	}
	if s.CLIAgent != "codex" && s.MCPDefaultToolsApprovalMode != "" {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("mcp_default_tools_approval_mode is only valid when cli_agent='codex'"))
	}
	if s.CLIAgent != "codex" && s.CodexBypassApprovalsAndSandbox != nil && *s.CodexBypassApprovalsAndSandbox {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("codex_bypass_approvals_and_sandbox is only valid when cli_agent='codex'"))
	}
	if s.CLIAgent != "codex" && s.CodexTurnIdleTimeoutS != nil {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("codex_turn_idle_timeout_s is only valid when cli_agent='codex'"))
	}
	if s.CLIAgent != "codex" && s.CodexTurnIdleRetries != nil {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("codex_turn_idle_retries is only valid when cli_agent='codex'"))
	}
	if s.CLIAgent != "claude" && s.ClaudeTurnIdleTimeoutS != nil {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("claude_turn_idle_timeout_s is only valid when cli_agent='claude'"))
	}
	// 依值而非 fields_set：模型 dump 往返（spawn payload / checkpoint）会显式带出
	// 包括 nil 在内的每个字段，非 Claude 成员仍须校验通过（Python team.py:521-524 注释）
	if s.CLIAgent != "claude" && s.ClaudeMaxBufferSize != nil {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("claude_max_buffer_size is only valid when cli_agent='claude'"))
	}
	adapterBacked := isAdapterBacked(s.CLIAgent)
	if adapterBacked && s.ExternalModelConfig != nil {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("model_config is not valid for adapter-backed CLI agents"))
	}
	if adapterBacked && len(s.BuiltinModels) > 0 {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("builtin_models is not valid for adapter-backed CLI agents"))
	}
	names := make(map[string]struct{}, len(s.BuiltinModels))
	for _, m := range s.BuiltinModels {
		names[m.Name] = struct{}{}
	}
	if len(names) != len(s.BuiltinModels) {
		return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("builtin_models names must be unique"))
	}
	return nil
}

// FindBuiltinModel 返回声明名叫 name 的内置模型（没有则 nil）。
// Python: ExternalCliAgentSpec.find_builtin_model (team.py:537)
func (s *ExternalCliAgentSpec) FindBuiltinModel(name string) *ExternalCliBuiltinModel {
	for i := range s.BuiltinModels {
		if s.BuiltinModels[i].Name == name {
			return &s.BuiltinModels[i]
		}
	}
	return nil
}

// DefaultMCPServerCommand 团队 MCP stdio server 的默认启动 argv。
// Python: mcp_server_command default_factory (team.py:458)
func DefaultMCPServerCommand() []string {
	return []string{"openjiuwen-team-mcp"}
}

// ValidateBuiltinModelDefaultEffort 校验 default_effort 必须属于 efforts。
// Python: ExternalCliBuiltinModel._validate_default_effort (team.py:334)
func (m *ExternalCliBuiltinModel) ValidateBuiltinModelDefaultEffort() error {
	if m.DefaultEffort == "" {
		return nil
	}
	for _, e := range m.Efforts {
		if e == m.DefaultEffort {
			return nil
		}
	}
	return exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
		exception.WithMsg(fmt.Sprintf("builtin model '%s' default_effort must be one of its efforts", m.Name)))
}
