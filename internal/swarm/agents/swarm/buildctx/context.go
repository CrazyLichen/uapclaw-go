// Package swarm 提供平台侧（jiuwenswarm 对应层）的 provider 组装运行时。
//
// SwarmBuildContext 是 provider 化团队组装的运行时构建载体：承载能力
// provider 构建期所需的平台活句柄。它扩展 openjiuwen 的 BuildContext，
// 使 setup_agent 能经 derive() 填充 per-member 视图（member_name / role /
// workspace / member_card_id），平台则挂载 per-team / per-process 句柄。
//
// 它刻意不持有父 DeepAgent：团队成员完全从共享配置源（config.yaml）加
// provider 名引用组装，不继承任何预构建的单 agent。
//
// 文件目录：
//
//	swarm/
//	├── doc.go        # 包文档
//	├── context.go    # SwarmBuildContext 平台子类 + seed 往返 + heartbeat 单槽
//	├── registry.go   # 种子工厂覆盖注册缝（15.16c）
//	└── assembly.go   # enrich seed 生产段接线（15.16c）
//
// 对应 Python 代码：jiuwenswarm/agents/swarm/
package buildctx

import (
	agentteams "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/workspace"
	common "github.com/uapclaw/uapclaw-go/internal/swarm/common"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// 叶包迁移注记（2026-12-04）：本文件自父包 swarm 迁入（用户裁决"立即挪
// 叶包"），providers 工厂直接使用具体类型 *SwarmBuildContext，消灭
// D-15.16f-view 的 22 个访问器接口；team 包经本包引用平台上下文，
// 脱离对父包 swarm 的依赖（消 team→swarm 边）。Python: jiuwenswarm/
// agents/swarm/context.py（上游为 providers 同目录父级模块，Go 因包系统
// 需独立叶包承载）。

// ──────────────────────────── 类型别名 ────────────────────────────

// HeartbeatJobService 进程级 Heartbeat 服务的占位类型。
// 偏差 D-15.16-c2③（用户 12-02 裁决）：Go 侧 Heartbeat 域 ⤴️AgentServer
// 章节未移植，先以 any 别名承载字段，调用处仅调用不处理返回值；实现注入
// 随实现计划对应章节回填，届时收窄为具体类型/窄接口。
// Python: heartbeat_job_service: Any | None（context.py:119-122）
type HeartbeatJobService = any

// ──────────────────────────── 结构体 ────────────────────────────

// SwarmBuildContextParams NewSwarmBuildContext 的全参入参。
// 字段顺序对齐 Python dataclass 字段顺序（context.py:104-123）。
type SwarmBuildContextParams struct {
	// SessionID 当前会话 id
	SessionID string
	// RequestID 发起请求 id（可为空）
	RequestID string
	// UserID 认证请求属主（用户路由工具使用，可为空）
	UserID string
	// ChannelID 请求原始 channel id（可为空）
	ChannelID string
	// Channel get_team_manager 解析后的 channel 键（channel_id 或 "default"）
	Channel string
	// RequestMetadata 请求元数据（携带 mode 等）
	RequestMetadata map[string]any
	// Mode 请求模式（如 "team"）
	Mode string
	// ProjectDir 已解析的项目目录（可空）
	ProjectDir string
	// TrustedDirs 客户端声明为可信的目录
	TrustedDirs []string
	// DisableTeammateWorktree 是否禁用 teammate worktree 隔离
	DisableTeammateWorktree bool
	// TeamID 团队名
	TeamID string
	// TeamWsRoot 团队共享工作空间根路径
	TeamWsRoot string
	// TaskWorkspaceRoot 无项目成员被告知的共享最终产物根目录（可空）
	TaskWorkspaceRoot string
	// TeamOutputsDir 最终产物目录（project_dir 存在时为空）
	TeamOutputsDir string
	// TeamSkillVisibilityPath 团队级 Skill 可见性元数据文件路径
	TeamSkillVisibilityPath string
	// GlobalSkillsDir 全体 agent 共读的唯一天然 Skill 库目录
	GlobalSkillsDir string
	// TrajectorySpanProcessor 团队演进 rails 共享的进程级轨迹 span 处理器
	TrajectorySpanProcessor sdktrace.SpanProcessor
	// HeartbeatJobService 进程级 AgentServer Heartbeat 服务（seed 不携带）
	HeartbeatJobService HeartbeatJobService
	// Config 已解析的 config.yaml 映射
	Config map[string]any
	// MemberName 成员名（per-member 视图，derive 填充）
	MemberName string
	// Role 团队角色（per-member 视图，derive 填充）
	Role string
	// Language 语言码（per-member 视图）
	Language string
	// Workspace 活工作空间（per-member 视图）
	Workspace *workspace.Workspace
	// MemberCardID 解析后的 card id（per-member 视图）
	MemberCardID string
}

// SwarmBuildContext 承载 jiuwenswarm 运行时句柄的 BuildContext 平台子类。
//
// per-team / per-process 字段在 enrich 团队 spec 时一次性设置；per-member
// 字段（member_name / role / language / workspace / member_card_id）继承自
// 基类 BuildContext，由 setup_agent 经 derive() 填充。
//
// Python: SwarmBuildContext (jiuwenswarm/agents/swarm/context.py:40-126)
type SwarmBuildContext struct {
	// 基类字段：language/member_name/role/workspace/member_card_id/
	// project_dir/team_outputs_dir/extras/moa
	*atschema.BuildContext

	// SessionID 当前会话 id（context.py:104）
	SessionID string
	// RequestID 发起请求 id（context.py:105）
	RequestID string
	// UserID 认证请求属主（context.py:106）
	UserID string
	// ChannelID 请求原始 channel id（context.py:107）
	ChannelID string
	// Channel get_team_manager 解析后的 channel 键（context.py:108）
	Channel string
	// RequestMetadata 请求元数据（context.py:109）
	RequestMetadata map[string]any
	// Mode 请求模式（context.py:110）
	Mode string
	// TrustedDirs 客户端声明可信目录（context.py:112）
	TrustedDirs []string
	// DisableTeammateWorktree 是否禁用 teammate worktree（context.py:113）
	DisableTeammateWorktree bool
	// TeamID 团队名（context.py:114）
	TeamID string
	// TeamWsRoot 团队共享工作空间根（context.py:115）
	TeamWsRoot string
	// TaskWorkspaceRoot 无项目成员的共享产物根（context.py:116）
	TaskWorkspaceRoot string
	// TeamSkillVisibilityPath 团队级 Skill 可见性元数据文件（context.py:120）
	TeamSkillVisibilityPath string
	// GlobalSkillsDir 唯一天然 Skill 库目录（context.py:121）
	GlobalSkillsDir string
	// TrajectorySpanProcessor 团队演进 rails 共享的轨迹处理器（context.py:122）
	TrajectorySpanProcessor sdktrace.SpanProcessor
	// HeartbeatJobService AgentServer Heartbeat 服务（context.py:123）
	HeartbeatJobService HeartbeatJobService
	// Config 已解析 config.yaml（context.py:124）
	Config map[string]any
	// SkillRetrievalToolkit 成员级 Symphony 发现运行时（context.py:125，
	// Go 侧 skill_index 消费面 ⤴️，暂以 any 占位）
	SkillRetrievalToolkit any
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// heartbeatJobServiceMu 保护 heartbeatJobService 单槽。
	heartbeatJobServiceMu sync.RWMutex
	// heartbeatJobService 进程级 Heartbeat 服务槽位。
	// Python: _heartbeat_job_service (context.py:28)
	heartbeatJobService HeartbeatJobService
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetHeartbeatJobService 把 AgentServer 拥有的 Heartbeat 服务发布给团队构建器。
// Python: set_heartbeat_job_service (context.py:31-35)
func SetHeartbeatJobService(service HeartbeatJobService) {
	heartbeatJobServiceMu.Lock()
	defer heartbeatJobServiceMu.Unlock()
	heartbeatJobService = service
}

// GetHeartbeatJobService 返回进程级 Heartbeat 服务（AgentServer 未安装则 nil）。
// Python: get_heartbeat_job_service (context.py:37-41)
func GetHeartbeatJobService() HeartbeatJobService {
	heartbeatJobServiceMu.RLock()
	defer heartbeatJobServiceMu.RUnlock()
	return heartbeatJobService
}

// NewSwarmBuildContext 构造平台上下文。
// Python dataclass 构造 + __post_init__ cron 桥（context.py:127-147）：
// cron 调度器在每个 cron 请求上写 request_metadata["cron"]；SDK 的
// BuildContext 不暴露 request_metadata，镜像到 extras["is_cron"] 让派生
// 上下文经 extras.get("is_cron") 读到同一信号。
func NewSwarmBuildContext(p SwarmBuildContextParams) *SwarmBuildContext {
	if p.Channel == "" {
		p.Channel = "default"
	}
	if p.Mode == "" {
		p.Mode = "team"
	}
	c := &SwarmBuildContext{
		BuildContext: &atschema.BuildContext{
			Language:       p.Language,
			MemberName:     p.MemberName,
			Role:           p.Role,
			Workspace:      p.Workspace,
			MemberCardID:   p.MemberCardID,
			ProjectDir:     p.ProjectDir,
			TeamOutputsDir: p.TeamOutputsDir,
			Extras:         map[string]any{},
		},
		SessionID:               p.SessionID,
		RequestID:               p.RequestID,
		UserID:                  p.UserID,
		ChannelID:               p.ChannelID,
		Channel:                 p.Channel,
		RequestMetadata:         p.RequestMetadata,
		Mode:                    p.Mode,
		TrustedDirs:             p.TrustedDirs,
		DisableTeammateWorktree: p.DisableTeammateWorktree,
		TeamID:                  p.TeamID,
		TeamWsRoot:              p.TeamWsRoot,
		TaskWorkspaceRoot:       p.TaskWorkspaceRoot,
		TeamSkillVisibilityPath: p.TeamSkillVisibilityPath,
		GlobalSkillsDir:         p.GlobalSkillsDir,
		TrajectorySpanProcessor: p.TrajectorySpanProcessor,
		HeartbeatJobService:     p.HeartbeatJobService,
		Config:                  p.Config,
	}
	// __post_init__（context.py:144-147）
	if v, ok := p.RequestMetadata["cron"]; ok && v != nil && v != "" {
		c.GetExtras()["is_cron"] = true
	}
	return c
}

// ToSeed 导出 per-team / per-process 可序列化字段为种子。
// 种子经 TeamAgentSpec.build_context_seed 跨序列化边界传递；
// FromSeed 以本地句柄重建。per-member 五字段（member_name/role/language/
// workspace/member_card_id）刻意排除——setup_agent 经 derive() 按成员填充。
// 活的 config 与 trajectory_span_processor 亦排除：由接收方提供。
// Python: to_seed (context.py:196-228)
func (c *SwarmBuildContext) ToSeed() map[string]any {
	seed := map[string]any{
		"session_id":                 c.SessionID,
		"request_id":                 c.RequestID,
		"user_id":                    c.UserID,
		"channel_id":                 c.ChannelID,
		"channel":                    c.Channel,
		"request_metadata":           c.RequestMetadata,
		"mode":                       c.Mode,
		"project_dir":                c.ProjectDir,
		"trusted_dirs":               nil,
		"disable_teammate_worktree":  c.DisableTeammateWorktree,
		"team_id":                    c.TeamID,
		"team_ws_root":               c.TeamWsRoot,
		"task_workspace_root":        c.TaskWorkspaceRoot,
		"team_outputs_dir":           c.TeamOutputsDir,
		"team_skill_visibility_path": c.TeamSkillVisibilityPath,
		"global_skills_dir":          c.GlobalSkillsDir,
	}
	// Python: list(self.trusted_dirs) if self.trusted_dirs else None
	if len(c.TrustedDirs) > 0 {
		seed["trusted_dirs"] = append([]string(nil), c.TrustedDirs...)
	}
	return seed
}

// SwarmBuildContextFromSeed 从 ToSeed 映射 + 本地句柄重建上下文。
// Python: from_seed (context.py:230-260)：seed 键逐键回读；config 与
// trajectory_span_processor 由接收进程提供；heartbeat 从本进程单槽取。
func SwarmBuildContextFromSeed(seed map[string]any, config map[string]any, trajectorySpanProcessor sdktrace.SpanProcessor) *SwarmBuildContext {
	c := NewSwarmBuildContext(SwarmBuildContextParams{
		SessionID:               seedString(seed, "session_id"),
		RequestID:               seedString(seed, "request_id"),
		UserID:                  seedString(seed, "user_id"),
		ChannelID:               seedString(seed, "channel_id"),
		Channel:                 seedString(seed, "channel"),
		RequestMetadata:         seedAnyMap(seed, "request_metadata"),
		Mode:                    seedString(seed, "mode"),
		ProjectDir:              seedString(seed, "project_dir"),
		TrustedDirs:             seedStringSlice(seed, "trusted_dirs"),
		DisableTeammateWorktree: seedBool(seed, "disable_teammate_worktree"),
		TeamID:                  seedString(seed, "team_id"),
		TeamWsRoot:              seedString(seed, "team_ws_root"),
		TaskWorkspaceRoot:       seedString(seed, "task_workspace_root"),
		TeamOutputsDir:          seedString(seed, "team_outputs_dir"),
		TeamSkillVisibilityPath: seedString(seed, "team_skill_visibility_path"),
		GlobalSkillsDir:         seedString(seed, "global_skills_dir"),
		TrajectorySpanProcessor: trajectorySpanProcessor,
		HeartbeatJobService:     GetHeartbeatJobService(),
		Config:                  config,
	})
	// Python: seed.get("channel") or "default" / seed.get("mode") or "team"
	if c.Channel == "" {
		c.Channel = "default"
	}
	if c.Mode == "" {
		c.Mode = "team"
	}
	return c
}

// ResolveMemberSkillVisibilityPath 解析当前成员的 Skill 可见性元数据文件路径。
// 刻意命名 resolve_*：团队级对应物 team_skill_visibility_path 是普通字段，
// 读取两者的消费方不得把本方法误当值。派生而非存储：基础上下文是 per-team
// 的，member_name / workspace 只有经 setup_agent derive() 后才有 per-member
// 值，所以 per-member 路径不可能出现在 ToSeed 中。
// 成员工作空间根优先于重算稳定布局路径，被重定位（symlink 暴露在团队树下
// 的独立成员工作空间）的成员其元数据保持在它实际使用的工作空间旁。
// Python: resolve_member_skill_visibility_path (context.py:150-181)
func (c *SwarmBuildContext) ResolveMemberSkillVisibilityPath() string {
	if c.Workspace != nil {
		if rootPath := c.Workspace.RootPath; rootPath != "" {
			return filepath.Join(rootPath, agentteams.SKILL_VISIBILITY_FILENAME)
		}
	}
	memberName := strings.TrimSpace(c.MemberName)
	if memberName == "" || c.TeamID == "" {
		return ""
	}
	return agentteams.MemberSkillVisibilityPath(c.TeamID, memberName)
}

// ResolveMemberWorkDir 解析本成员的隔离临时工作目录。
// 刻意命名 resolve_*：同 ResolveMemberSkillVisibilityPath，须待 per-member
// 视图（member_name）被 setup_agent 经 derive() 填充后才能计算。成员工作
// 目录位于共享 task_workspace_root 下（work/<member_slug>/），因此是
// per-member 的，永不进入 ToSeed。
// Python: resolve_member_work_dir (context.py:183-195)
// 返回值：无共享产物根配置时返回空串（绑定项目的成员，产物留在项目内）。
func (c *SwarmBuildContext) ResolveMemberWorkDir() (string, error) {
	if c.TaskWorkspaceRoot == "" {
		return "", nil
	}
	return common.ResolveMemberWorkDir(c.TaskWorkspaceRoot, c.MemberName)
}

// Derive 返回带 overrides 的浅拷贝。
// 偏差 D-15.16-c4：基类 Derive 返回 *BuildContext 会丢平台字段——平台子类
// 必须返回自身具体类型（Python copy.copy 天然保留子类）。平台字段不在
// overrides 覆盖面内，全部随浅拷贝保留。
// Python: BuildContext.derive (build_context.py:85-95)
func (c *SwarmBuildContext) Derive(o hschema.BuildContextOverrides) hschema.BuildContextInterface {
	clone := *c
	base := *c.BuildContext
	clone.BuildContext = &base
	// 基类具名字段覆盖（overrides 语义：nil 继承）
	if o.Language != nil {
		clone.Language = *o.Language
	}
	if o.MemberName != nil {
		clone.MemberName = *o.MemberName
	}
	if o.Role != nil {
		clone.Role = *o.Role
	}
	if o.HasWorkspace {
		clone.Workspace = o.Workspace
	}
	if o.MemberCardID != nil {
		clone.MemberCardID = *o.MemberCardID
	}
	if o.ProjectDir != nil {
		clone.ProjectDir = *o.ProjectDir
	}
	if o.TeamOutputsDir != nil {
		clone.TeamOutputsDir = *o.TeamOutputsDir
	}
	if o.Moa != nil {
		clone.Moa = o.Moa
	}
	return &clone
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// seedString 读取种子字符串键（缺失/类型不符返回空串）。
func seedString(seed map[string]any, key string) string {
	if v, ok := seed[key].(string); ok {
		return v
	}
	return ""
}

// seedAnyMap 读取种子映射键（缺失/类型不符返回 nil）。
func seedAnyMap(seed map[string]any, key string) map[string]any {
	if v, ok := seed[key].(map[string]any); ok {
		return v
	}
	return nil
}

// seedBool 读取种子布尔键（缺失返回 false）。
// Python: bool(seed.get(key, False))
func seedBool(seed map[string]any, key string) bool {
	if v, ok := seed[key].(bool); ok {
		return v
	}
	return false
}

// seedStringSlice 读取种子字符串切片键（缺失/类型不符返回 nil）。
func seedStringSlice(seed map[string]any, key string) []string {
	raw, ok := seed[key].([]string)
	if ok {
		return raw
	}
	// JSON 反序列化路径：[]any → []string
	rawAny, ok := seed[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawAny))
	for _, item := range rawAny {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// 编译期断言：*SwarmBuildContext 必须满足构建上下文契约。
var _ hschema.BuildContextInterface = (*SwarmBuildContext)(nil)
