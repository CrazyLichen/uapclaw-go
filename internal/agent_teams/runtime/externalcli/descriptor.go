// 外部成员入团描述符：对齐 Python openjiuwen/agent_teams/external/descriptor.py
// 与 spawn/external_cli_spawn.py 的 descriptor_from_context/_with_home_env。
package externalcli

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamJoinDescriptor 外部成员加入团队所需的完整凭据。
// 一个外部 Agent 进程（第三方 CLI 或独立服务）要成为一等团队成员需要三件事：
// 团队数据库在哪、如何触达团队 messager、以及它承载哪个成员身份。本结构把它们
// 打包成一个可 JSON 序列化的载荷，由团队在 spawn 时经环境变量注入。
//
// Python: TeamJoinDescriptor (external/descriptor.py:60)
type TeamJoinDescriptor struct {
	// SessionID 会话标识，用于构建事件主题与隔离会话动态表；必须匹配团队当前活跃会话
	SessionID string `json:"session_id"`
	// TeamName 目标团队标识
	TeamName string `json:"team_name"`
	// MemberName 本外部 Agent 服务的成员身份；团队必须已在该名字下注册成员行
	MemberName string `json:"member_name"`
	// Role 成员角色，驱动操作面过滤（"teammate" 默认 / "leader"）
	Role string `json:"role"`
	// Scope 本连接服务的外部访问场景，与 role（团队角色）正交：
	// "member" = spawn 的第三方 CLI，一等团队成员，获得原生 teammate 工具集，
	// 团队 system prompt 在 spawn 时注入；"operator"（默认）= 外部操作者界面。
	// 团队成员 spawn 路径固定传 "member"。
	Scope string `json:"scope"`
	// Language 团队运行语言（"cn"/"en"），渲染入站消息与任务面板文本
	Language string `json:"language"`
	// DispatchMode 任务如何到达成员（"autonomous"/"scheduled"），与 role/scope 正交
	DispatchMode string `json:"dispatch_mode"`
	// EnableMemberMessaging scheduled 成员是否可直接给同伴发消息（F_126）
	EnableMemberMessaging bool `json:"enable_member_messaging"`
	// MaxSubtaskDepth scheduled 成员的子任务拆分深度上限（F_128）；0 表示
	// "member" scope 工具集不含 create_task
	MaxSubtaskDepth int `json:"max_subtask_depth"`
	// TeammateMode 队友执行任务模式（"build_mode"/"plan_mode"）；plan-mode 成员
	// 据此获得 submit_plan
	TeammateMode string `json:"teammate_mode"`
	// DBConfig 团队数据库连接；跨进程使用必须是文件落盘的 sqlite connection_string
	// （Python DatabaseConfig，Go 侧对齐 DBConfigProvider 联合面）
	DBConfig database.DBConfigProvider `json:"db_config,omitempty"`
	// TransportConfig 外部客户端使用的消息传输配置；external_publish_url 是
	// Gateway 中继 WebSocket 端点
	TransportConfig *schema.MessagerTransportConfig `json:"transport_config,omitempty"`
	// WorkspaceConfig 共享工作空间配置；团队工作空间启用时对 spawn 的外部成员存在
	WorkspaceConfig *teamWorkspaceConfig `json:"workspace_config,omitempty"`
	// WorkspacePath 团队 host 解析出的共享工作空间绝对路径；外部客户端用它挂载
	// workspace_meta
	WorkspacePath string `json:"workspace_path,omitempty"`
}

// teamWorkspaceConfig 对 workspace 配置的本地别名引用，保持与
// team_workspace.TeamWorkspaceConfig 一致的序列化形状。
//
// Python: TeamWorkspaceConfig | None (descriptor.py workspace_config 字段)
type teamWorkspaceConfig = team_workspace.TeamWorkspaceConfig

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamJoinEnv 携带 JSON 描述符的唯一环境变量。
	// Python: TEAM_JOIN_ENV (external/descriptor.py:26)
	TeamJoinEnv = "OPENJIUWEN_TEAM_JOIN"

	// OpenjiuwenHomeEnv 覆盖运行时 home 的环境变量。spawn 的子进程（Codex 的
	// MCP server）无法继承 host 平台内存中的 configure_openjiuwen_home 覆盖，
	// home 必须经 env 传播以对齐 session 文件 spill/deref 路径。
	// Python: OPENJIUWEN_HOME_ENV (external/descriptor.py:31)
	OpenjiuwenHomeEnv = "OPENJIUWEN_HOME"
)

// MCPServerEnvVars Codex MCP server 子进程必须从其 Codex App Server 父进程
// 继承的环境变量。Codex 的 mcp_servers.<key>.env_vars 是白名单——只有列出的
// 名字能进入 MCP server 进程——所以 server 需要的共享 DB、#file# 内容解引用、
// 会话根解析所需变量都必须出现在这里。
//
// Python: MCP_SERVER_ENV_VARS (external/descriptor.py:37)
var MCPServerEnvVars = []string{TeamJoinEnv, OpenjiuwenHomeEnv}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToJSON 序列化为紧凑 JSON 字符串。
// Python: TeamJoinDescriptor.to_json (descriptor.py:99)
func (d *TeamJoinDescriptor) ToJSON() string {
	b, err := json.Marshal(d)
	if err != nil {
		// Python model_dump_json 对本结构不会失败；Go 侧字段全为基础类型，
		// 序列化失败属于不可达路径，退化为空串保证调用方拿得到 env 键形状。
		return ""
	}
	return string(b)
}

// ToEnv 返回 spawn 外部成员时应注入的环境变量映射。
// Python: TeamJoinDescriptor.to_env (descriptor.py:102)
func (d *TeamJoinDescriptor) ToEnv() map[string]string {
	return map[string]string{TeamJoinEnv: d.ToJSON()}
}

// TeamJoinDescriptorFromJSON 从 JSON 字符串解析描述符。
// 报文非法时报 AGENT_TEAM_CONFIG_INVALID（对齐 Python ValidationError 分支）；
// pydantic required 字段（session_id/team_name/member_name，descriptor.py:116-118）
// 缺失同样报 AGENT_TEAM_CONFIG_INVALID。
//
// Python: TeamJoinDescriptor.from_json (descriptor.py:107)
func TeamJoinDescriptorFromJSON(raw string) (*TeamJoinDescriptor, error) {
	var d TeamJoinDescriptor
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("malformed team join descriptor: "+err.Error()),
			exception.WithCause(err))
	}
	// pydantic required 字段校验：缺失报错，文案对齐 pydantic "Field required"
	for _, req := range []struct{ field, value string }{
		{"session_id", d.SessionID},
		{"team_name", d.TeamName},
		{"member_name", d.MemberName},
	} {
		if req.value == "" {
			return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
				exception.WithMsg(req.field+": Field required"))
		}
	}
	return &d, nil
}

// TeamJoinDescriptorFromEnv 从 OPENJIUWEN_TEAM_JOIN 环境变量加载描述符。
// env 为 nil 时回落进程环境（Python 默认 os.environ）；变量未设置或值非法时报
// AGENT_TEAM_CONFIG_INVALID。
//
// Python: TeamJoinDescriptor.from_env (descriptor.py:122)
func TeamJoinDescriptorFromEnv(env map[string]string) (*TeamJoinDescriptor, error) {
	source := env
	if source == nil {
		source = osEnvelope()
	}
	raw, ok := source[TeamJoinEnv]
	if !ok || raw == "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("environment variable "+TeamJoinEnv+" is not set; cannot join team"))
	}
	return TeamJoinDescriptorFromJSON(raw)
}

// DescriptorFromContext 从成员运行时上下文构建入团描述符。
// 步骤逐条对齐 Python descriptor_from_context (spawn/external_cli_spawn.py:79-139)：
//
//  1. member_name 缺失报错（:81-86）
//  2. team_spec 缺失或无 team_name 报错（:87-93）
//  3. 当前团队 session_id 缺失报错（:97-102）
//  4. transport 三级选择 + team_name/node_id 更新 + pyzmq direct_addr 通配改写（:103-107）
//  5. workspace enabled 门卫与 workspace_path 解析（:109-116）
//  6. 组装描述符；scope 固定 "member"（一等队员，spawn 时注入团队 prompt）
//
// D-15.13-f：Python get_session_id() 读 contextvar，Go 侧改为显式 ctx 传参。
func DescriptorFromContext(ctx context.Context, rtc *schema.TeamRuntimeContext) (*TeamJoinDescriptor, error) {
	memberName := rtc.MemberName
	if memberName == "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("external CLI member requires a member_name in its runtime context"))
	}
	teamSpec := rtc.TeamSpec
	if teamSpec == nil || teamSpec.TeamName == "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("external CLI member requires a team spec with team_name"))
	}
	teamName := teamSpec.TeamName
	language := teamSpec.Language
	if language == "" {
		language = "cn"
	}
	dispatchMode := teamSpec.DispatchMode
	if dispatchMode == "" {
		dispatchMode = "autonomous"
	}
	teammateMode := string(teamSpec.TeammateMode)
	if teammateMode == "" {
		teammateMode = "build_mode"
	}
	sessionID := agentteams.GetSessionID(ctx)
	if sessionID == "" {
		return nil, exception.RaiseError(exception.StatusAgentTeamConfigInvalid,
			exception.WithMsg("external CLI member requires an active team session_id"))
	}
	// transport 三级选择：team_spec.external_messager_config → ctx.messager_config → 默认
	transport := teamSpec.ExternalMessagerConfig
	if transport == nil {
		transport = rtc.MessagerConfig
	}
	if transport == nil {
		transport = &schema.MessagerTransportConfig{}
	}
	copied := *transport
	copied.TeamName = teamName
	copied.NodeID = memberName
	// pyzmq 直连端口由成员侧自行监听，host 侧只保留通配形状（spawn.py:105-106）
	if copied.Backend == "pyzmq" && copied.DirectAddr != "" {
		copied.DirectAddr = "tcp://127.0.0.1:*"
	}
	transportConfig := &copied

	// workspace enabled 门卫（spawn.py:109-116）
	workspaceConfig := (*teamWorkspaceConfig)(nil)
	if teamSpec.Workspace != nil {
		candidate := teamSpec.Workspace
		if candidate.Enabled {
			workspaceConfig = candidate
		}
	}
	workspacePath := ""
	if workspaceConfig != nil {
		if workspaceConfig.RootPath != "" {
			workspacePath = workspaceConfig.RootPath
		} else {
			workspacePath = agentteams.TeamWorkspaceDir(teamName)
		}
	}

	role := string(rtc.Role)
	if role == "" {
		role = "teammate"
	}
	return &TeamJoinDescriptor{
		SessionID:             sessionID,
		TeamName:              teamName,
		MemberName:            memberName,
		Role:                  role,
		Scope:                 "member",
		Language:              language,
		DispatchMode:          dispatchMode,
		EnableMemberMessaging: teamSpec.EnableMemberMessaging,
		MaxSubtaskDepth:       teamSpec.MaxSubtaskDepth,
		TeammateMode:          teammateMode,
		DBConfig:              rtc.DBConfig,
		TransportConfig:       transportConfig,
		WorkspaceConfig:       workspaceConfig,
		WorkspacePath:         workspacePath,
	}, nil
}

// WithHomeEnv 保证运行时 home 跨进程传播到 CLI 子进程。
// host 平台经 configure_openjiuwen_home（进程级模块变量）配置 home，它不会跨过
// 进程边界；spawn 的 CLI（以及它再 spawn 的 MCP server）否则会解析默认
// ~/.openjiuwen 而错过配置根下写的 session spill 文件。把解析出的 home 以
// OPENJIUWEN_HOME 传播出去，get_openjiuwen_home（env 回落）与 Codex 的
// mcp_servers.<key>.env_vars 白名单（再传一跳进 MCP server）才能保持路径对齐。
//
// Python: _with_home_env (spawn/external_cli_spawn.py:63-76)；setdefault 语义
func WithHomeEnv(env map[string]string) map[string]string {
	if _, ok := env[OpenjiuwenHomeEnv]; !ok {
		env[OpenjiuwenHomeEnv] = agentteams.GetHome()
	}
	return env
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// osEnvelope 快照进程环境变量（from_env 的默认来源）。
// Python os.environ 的 Go 等价物；加锁仅保护 map 构建过程。
func osEnvelope() map[string]string {
	osEnvMu.Lock()
	defer osEnvMu.Unlock()
	out := make(map[string]string, len(os.Environ()))
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

var osEnvMu sync.Mutex
