package agent_teams

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MountedRails 已挂载的团队侧 Rails 句柄。
// 对齐 Python: _MountedRails (openjiuwen/agent_teams/harness.py)
//
// 保留为数据类使 Rails 阵容（及哪些是可选的）对读者和测试可见。
// 字段顺序与 BuildTeamHarness 中 Rails 挂载顺序一致。
type MountedRails struct {
	// TeamTool 团队工具轨
	// TODO(#9.68): TeamToolRail 类型
	TeamTool any
	// TeamPolicy 团队策略轨
	// TODO(#9.68): TeamPolicyRail 类型
	TeamPolicy any
	// FirstIterGate 首轮迭代门控
	// TODO(#9.68): FirstIterationGate 类型
	FirstIterGate any
	// TeamWorkspace 团队工作空间轨
	// TODO(#9.66+#9.68): TeamWorkspaceRail 类型
	TeamWorkspace any
	// ToolApproval 工具审批轨
	// TODO(#9.68): TeamToolApprovalRail 类型
	ToolApproval any
	// TeamPlanMode 团队计划模式轨
	// TODO(#9.68): TeamPlanModeRail 类型
	TeamPlanMode any
}

// AgentCustomizer 用户自定义钩子签名。
// 对齐 Python: AgentCustomizer = Callable[[Any, Optional[str], str], None]
//
// 参数：deepAgent, memberName, roleValue
type AgentCustomizer func(deepAgent any, memberName string, roleValue string)

// TeamHarness TeamAgent 与底层 DeepAgent 之间的唯一适配器。
// 对齐 Python: TeamHarness (openjiuwen/agent_teams/harness.py)
//
// 所有对 DeepAgent 的访问（配置、模型、工作空间、Rails、流式）
// 必须通过此对象。替换 DeepAgent 只需重新实现此模块；
// agent_teams 中的业务代码保持相同的调用面。
type TeamHarness struct {
	// deepAgent 内层 DeepAgent 实例
	// TODO(#9.57): 改为 hinterfaces.DeepAgentInterface 类型
	deepAgent any
	// rails 已挂载的 Rails 句柄
	rails *MountedRails
	// role 团队角色（对齐 Python TeamRole，底层为 string）
	role string
	// memberName 成员名
	memberName string
	// initialPlanMode 初始计划模式
	initialPlanMode bool
	// initialPlanModeSeeded 初始计划模式是否已种子化
	initialPlanModeSeeded bool
	// activeAgentSession 活跃的 Agent 会话
	// TODO(#9.session): AgentSession 类型
	activeAgentSession any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamHarness 创建新的 TeamHarness 实例。
// 对齐 Python: TeamHarness.__init__(deep_agent, rails, ...)
func NewTeamHarness(
	deepAgent any,
	rails *MountedRails,
	role string,
	memberName string,
	initialPlanMode bool,
) *TeamHarness {
	return &TeamHarness{
		deepAgent:       deepAgent,
		rails:           rails,
		role:            role,
		memberName:      memberName,
		initialPlanMode: initialPlanMode,
	}
}

// BuildTeamHarness 从 AgentSpec 物化 DeepAgent 并挂载所有团队 Rails。
// 对齐 Python: TeamHarness.build(...)
//
// 挂载顺序有语义：TeamToolRail 必须在 TeamPolicyRail 之前挂载并
// 急切初始化，以便 LLM 看到的能力快照与测试观察到的一致。
//
// TODO(#9.68): Rails 挂载逻辑实现后替换
func BuildTeamHarness(
	agentSpec any, // TODO(#9.56): DeepAgentSpec 类型
	role string,
	memberName string,
	teamToolRail any, // TODO(#9.68): 团队工具Rail
	teamPolicyRail any, // TODO(#9.68): 团队策略Rail
	firstIterGate any, // TODO(#9.68): 首轮门控
	teamWorkspaceRail any, // TODO(#9.66+#9.68): 团队工作空间Rail
	toolApprovalRail any, // TODO(#9.68): 工具审批Rail
	teamPlanModeRail any, // TODO(#9.68): 团队规划模式Rail
	initialPlanMode bool,
) *TeamHarness {
	// TODO(#9.56): 构建深度Agent deepAgent = agentSpec.Build()
	// TODO(#9.68): 添加团队策略Rail deepAgent.AddRail(teamPolicyRail)
	// TODO(#9.68): 首轮门控Rail deepAgent.AddRail(firstIterGate)
	// TODO(#9.66+#9.68): 团队工作空间Rail deepAgent.AddRail(teamWorkspaceRail)
	// TODO(#9.68): 工具审批Rail deepAgent.AddRail(toolApprovalRail)
	// TODO(#9.68): 团队规划模式Rail deepAgent.AddRail(teamPlanModeRail)
	rails := &MountedRails{
		TeamTool:      teamToolRail,
		TeamPolicy:    teamPolicyRail,
		FirstIterGate: firstIterGate,
		TeamWorkspace: teamWorkspaceRail,
		ToolApproval:  toolApprovalRail,
		TeamPlanMode:  teamPlanModeRail,
	}
	return NewTeamHarness(nil, rails, role, memberName, initialPlanMode)
}

// RunAgentCustomizer 调用用户自定义钩子。
// 对齐 Python: TeamHarness.run_agent_customizer(customizer)
//
// 在 Rail 挂载和依赖绑定（memory_manager 等）之后调用，
// 使自定义器看到完整准备的环境。吞掉异常以保持团队启动不被破坏；
// 失败记录到日志。
func (h *TeamHarness) RunAgentCustomizer(customizer AgentCustomizer) {
	if customizer == nil {
		return
	}
	// 对齐 Python: try/except Exception — 吞掉 customizer 的 panic
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("member_name", h.memberName).
				Any("error", r).
				Msg("agent_customizer 失败")
		}
	}()
	customizer(h.deepAgent, h.memberName, h.role)
}

// Rails 返回已挂载的团队侧 Rails 句柄。
// 对齐 Python: TeamHarness.rails property
func (h *TeamHarness) Rails() *MountedRails {
	return h.rails
}

// Role 返回团队角色。
func (h *TeamHarness) Role() string {
	return h.role
}

// MemberName 返回成员名。
func (h *TeamHarness) MemberName() string {
	return h.memberName
}

// InnerAgent 返回底层 DeepAgent 实例。
// 对齐 Python: TeamHarness.inner_agent property
//
// 生产代码不得使用此方法。仅用于测试和少量迁移辅助。
func (h *TeamHarness) InnerAgent() any {
	return h.deepAgent
}

// DeepConfig 返回 DeepAgent 配置快照。
// 对齐 Python: TeamHarness.deep_config property
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) DeepConfig() any { return nil }

// Workspace 返回绑定到底层 Agent 的工作空间。
// 对齐 Python: TeamHarness.workspace property
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) Workspace() any { return nil }

// SysOperation 返回绑定到底层 Agent 的系统操作。
// 对齐 Python: TeamHarness.sys_operation property
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) SysOperation() any { return nil }

// Model 返回底层 Agent 使用的模型。
// 对齐 Python: TeamHarness.model property
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) Model() any { return nil }

// HasPendingInterrupt 返回 Agent 是否有待恢复的中断状态。
// 对齐 Python: TeamHarness.has_pending_interrupt()
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) HasPendingInterrupt() bool { return false }

// InitCwdForRound 从工作空间根目录初始化每轮工作目录。
// 对齐 Python: TeamHarness.init_cwd_for_round()
// TODO(#9.57+9.35): 实现
func (h *TeamHarness) InitCwdForRound() {}

// Steer 转向指令到底层 Agent。
// 对齐 Python: TeamHarness.steer(content)
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) Steer(ctx context.Context, content string) error { return nil }

// FollowUp 追加消息到底层 Agent。
// 对齐 Python: TeamHarness.follow_up(content)
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) FollowUp(ctx context.Context, content string) error { return nil }

// Abort 协作中止底层任务循环。
// 对齐 Python: TeamHarness.abort()
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) Abort(ctx context.Context) error { return nil }

// RunStreaming 从底层 Agent 流式输出 chunk。
// 对齐 Python: TeamHarness.run_streaming(...)
// TODO(#9.runner): Runner.runAgentStreaming 实现后替换
func (h *TeamHarness) RunStreaming(ctx context.Context, inputs map[string]any, sessionID string, teamSession any) (any, error) {
	return nil, nil
}

// FindRails 返回挂载在底层 Agent 上的指定类型 Rails。
// 对齐 Python: TeamHarness.find_rails(rail_type)
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) FindRails(railType any) []any { return nil }

// RegisterRail 在运行中的 Agent 上注册额外 Rail。
// 对齐 Python: TeamHarness.register_rail(rail)
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) RegisterRail(ctx context.Context, rail any) error { return nil }

// UnregisterRail 注销先前注册的 Rail。
// 对齐 Python: TeamHarness.unregister_rail(rail)
// TODO(#9.57): deepAgent 类型升级后实现
func (h *TeamHarness) UnregisterRail(ctx context.Context, rail any) error { return nil }

// RegisterMemberTools 在底层 Agent 上注册团队记忆工具集。
// 对齐 Python: TeamHarness.register_member_tools(memory_manager)
// TODO(#9.64): memory_manager 类型定义后实现
func (h *TeamHarness) RegisterMemberTools(memoryManager any) {}

// InjectMemberMemory 向 Agent 的系统提示注入加载的记忆。
// 对齐 Python: TeamHarness.inject_member_memory(memory_manager, query)
// TODO(#9.64): memory_manager 类型定义后实现
func (h *TeamHarness) InjectMemberMemory(ctx context.Context, memoryManager any, query string) error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
