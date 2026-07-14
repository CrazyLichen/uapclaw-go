# 9.57 AgentConfigurator + SpawnPayloadBuilder + TeamHarness 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentConfigurator、SpawnPayloadBuilder、TeamHarness 的完整骨架（对齐 Python），回填 TeamAgent 中与 Configurator 直接相关的方法。

**Architecture:** 策略 B——骨架 + 依赖打桩。完整流程结构对齐 Python，未实现的依赖步骤用 `TODO(#9.xx)` 注释占位，结构体字段用 `any` 类型占位。TeamHarness 放 `agent_teams/` 根包（对齐 Python），AgentConfigurator 和 SpawnPayloadBuilder 放 `agent_teams/agent/` 子包（import 父包引用 TeamHarness）。

**Tech Stack:** Go 1.22+, testify/assert

---

## File Structure

| Action | File | Package | 职责 |
|--------|------|---------|------|
| Create | `internal/agent_teams/harness.go` | `agent_teams` | TeamHarness + MountedRails + AgentCustomizer 类型定义 |
| Create | `internal/agent_teams/harness_test.go` | `agent_teams_test` | TeamHarness 测试 |
| Create | `internal/agent_teams/agent/agent_configurator.go` | `agent` | AgentConfigurator 结构体 + resolveTeamMode + 全部方法 |
| Create | `internal/agent_teams/agent/agent_configurator_test.go` | `agent_test` | AgentConfigurator 测试 |
| Create | `internal/agent_teams/agent/payload.go` | `agent` | SpawnPayloadBuilder 结构体 + 全部方法 |
| Create | `internal/agent_teams/agent/payload_test.go` | `agent_test` | SpawnPayloadBuilder 测试 |
| Modify | `internal/agent_teams/agent/team_agent.go` | `agent` | 回填 configurator 字段类型 + 属性代理 + Configure() |
| Modify | `internal/agent_teams/agent/infra.go` | `agent` | 注释格式 ⤵️ → TODO(#9.xx) |
| Modify | `internal/agent_teams/agent/resources.go` | `agent` | 注释格式 ⤵️ → TODO(#9.xx)；Harness 字段类型升级 |
| Modify | `internal/agent_teams/agent/member.go` | `agent` | 注释格式 ⤵️ → TODO(#9.xx) |
| Modify | `internal/agent_teams/agent/doc.go` | `agent` | 添加新文件条目 |
| Modify | `internal/agent_teams/doc.go` | `agent_teams` | 添加 harness.go 条目 |
| Modify | `IMPLEMENTATION_PLAN.md` | — | 9.57 → 🔄 → ✅ |

---

### Task 1: TeamHarness 结构体和方法骨架（package agent_teams）

**Files:**
- Create: `internal/agent_teams/harness.go`
- Create: `internal/agent_teams/harness_test.go`
- Modify: `internal/agent_teams/doc.go`

- [ ] **Step 1: 创建 harness.go — MountedRails + AgentCustomizer + TeamHarness 结构体**

```go
package agent_teams

import (
	"context"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MountedRails 已挂载的团队侧 Rails 句柄。
// 对齐 Python: _MountedRails (openjiuwen/agent_teams/harness.py)
//
// 保留为数据类使 Rails 阵容（及哪些是可选的）对读者和测试可见。
// 字段顺序与 TeamHarness.Build 中 Rails 挂载顺序一致。
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
	// role 团队角色
	role atschema.TeamRole
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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamHarness 创建新的 TeamHarness 实例。
// 对齐 Python: TeamHarness.__init__(deep_agent, rails, ...)
func NewTeamHarness(
	deepAgent any,
	rails *MountedRails,
	role atschema.TeamRole,
	memberName string,
	initialPlanMode bool,
) *TeamHarness {
	return &TeamHarness{
		deepAgent:        deepAgent,
		rails:            rails,
		role:             role,
		memberName:       memberName,
		initialPlanMode:  initialPlanMode,
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
	role atschema.TeamRole,
	memberName string,
	teamToolRail any, // TODO(#9.68)
	teamPolicyRail any, // TODO(#9.68)
	firstIterGate any, // TODO(#9.68)
	teamWorkspaceRail any, // TODO(#9.66+#9.68)
	toolApprovalRail any, // TODO(#9.68)
	teamPlanModeRail any, // TODO(#9.68)
	initialPlanMode bool,
) *TeamHarness {
	// TODO(#9.56): deepAgent = agentSpec.Build()
	// TODO(#9.68): deepAgent.AddRail(teamToolRail) + 急切初始化
	// TODO(#9.68): deepAgent.AddRail(teamPolicyRail)
	// TODO(#9.68): if firstIterGate != nil { deepAgent.AddRail(firstIterGate) }
	// TODO(#9.66+#9.68): if teamWorkspaceRail != nil { deepAgent.AddRail(teamWorkspaceRail) }
	// TODO(#9.68): if toolApprovalRail != nil { deepAgent.AddRail(toolApprovalRail) }
	// TODO(#9.68): if teamPlanModeRail != nil { deepAgent.AddRail(teamPlanModeRail) }
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

// ──────────────────────────── 导出方法 ────────────────────────────

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
	// TODO(#9.logger): 添加 team_logger.Warn
	customizer(h.deepAgent, h.memberName, string(h.role))
}

// Rails 返回已挂载的团队侧 Rails 句柄。
// 对齐 Python: TeamHarness.rails property
func (h *TeamHarness) Rails() *MountedRails {
	return h.rails
}

// Role 返回团队角色。
func (h *TeamHarness) Role() atschema.TeamRole {
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
```

- [ ] **Step 2: 创建 harness_test.go**

```go
package agent_teams_test

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/stretchr/testify/assert"
)

// TestNewTeamHarness 测试 TeamHarness 构造函数
func TestNewTeamHarness(t *testing.T) {
	rails := &agent_teams.MountedRails{
		TeamTool:   "mock_tool_rail",
		TeamPolicy: "mock_policy_rail",
	}
	h := agent_teams.NewTeamHarness(
		"mock_deep_agent",
		rails,
		atschema.TeamRoleLeader,
		"leader_1",
		false,
	)
	assert.NotNil(t, h)
	assert.Equal(t, "mock_deep_agent", h.InnerAgent())
	assert.Equal(t, rails, h.Rails())
	assert.Equal(t, atschema.TeamRoleLeader, h.Role())
	assert.Equal(t, "leader_1", h.MemberName())
}

// TestNewTeamHarness_NilRails 测试 Rails 为 nil 时不 panic
func TestNewTeamHarness_NilRails(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, atschema.TeamRoleTeammate, "t1", true)
	assert.NotNil(t, h)
	assert.Nil(t, h.Rails())
}

// TestTeamHarness_Rails 返回 Rails 句柄
func TestTeamHarness_Rails(t *testing.T) {
	rails := &agent_teams.MountedRails{TeamTool: "x"}
	h := agent_teams.NewTeamHarness(nil, rails, atschema.TeamRoleLeader, "", false)
	assert.Equal(t, rails, h.Rails())
}

// TestTeamHarness_RunAgentCustomizer 测试自定义钩子调用
func TestTeamHarness_RunAgentCustomizer(t *testing.T) {
	var capturedAgent any
	var capturedName string
	var capturedRole string
	called := false

	customizer := func(deepAgent any, memberName string, roleValue string) {
		called = true
		capturedAgent = deepAgent
		capturedName = memberName
		capturedRole = roleValue
	}

	h := agent_teams.NewTeamHarness("my_agent", nil, atschema.TeamRoleLeader, "leader_1", false)
	h.RunAgentCustomizer(agent_teams.AgentCustomizer(customizer))

	assert.True(t, called)
	assert.Equal(t, "my_agent", capturedAgent)
	assert.Equal(t, "leader_1", capturedName)
	assert.Equal(t, "leader", capturedRole)
}

// TestTeamHarness_RunAgentCustomizer_Nil 测试 nil 自定义器不 panic
func TestTeamHarness_RunAgentCustomizer_Nil(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, atschema.TeamRoleLeader, "", false)
	assert.NotPanics(t, func() {
		h.RunAgentCustomizer(nil)
	})
}

// TestBuildTeamHarness 测试 Build 函数
func TestBuildTeamHarness(t *testing.T) {
	h := agent_teams.BuildTeamHarness(
		nil,           // agentSpec
		atschema.TeamRoleTeammate,
		"teammate_1",
		"tool_rail",   // teamToolRail
		"policy_rail", // teamPolicyRail
		nil,           // firstIterGate
		nil,           // teamWorkspaceRail
		nil,           // toolApprovalRail
		nil,           // teamPlanModeRail
		true,
	)
	assert.NotNil(t, h)
	assert.Equal(t, atschema.TeamRoleTeammate, h.Role())
	assert.Equal(t, "teammate_1", h.MemberName())
	assert.NotNil(t, h.Rails())
	assert.Equal(t, "tool_rail", h.Rails().TeamTool)
	assert.Equal(t, "policy_rail", h.Rails().TeamPolicy)
	assert.Nil(t, h.Rails().FirstIterGate)
}

// TestTeamHarness_StubMethods 测试 TODO 占位方法返回零值
func TestTeamHarness_StubMethods(t *testing.T) {
	h := agent_teams.NewTeamHarness(nil, nil, atschema.TeamRoleLeader, "", false)
	assert.Nil(t, h.DeepConfig())
	assert.Nil(t, h.Workspace())
	assert.Nil(t, h.SysOperation())
	assert.Nil(t, h.Model())
	assert.False(t, h.HasPendingInterrupt())
	assert.Nil(t, h.FindRails(nil))
}
```

- [ ] **Step 3: 更新 agent_teams/doc.go 添加 harness.go 条目**

在 `doc.go` 的文件目录树中，在 `i18n.go` 条目之后添加：
```
//	├── harness.go          # TeamHarness 团队适配层（9.57）
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...`
Expected: 编译通过

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/ -v -run "TestNewTeamHarness|TestTeamHarness_Rails|TestTeamHarness_RunAgentCustomizer|TestBuildTeamHarness|TestTeamHarness_StubMethods"`
Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agent_teams/harness.go internal/agent_teams/harness_test.go internal/agent_teams/doc.go
git commit -m "feat(agent_teams): add TeamHarness + MountedRails structs and method stubs (9.57)"
```

---

### Task 2: SpawnPayloadBuilder（package agent）

**Files:**
- Create: `internal/agent_teams/agent/payload.go`
- Create: `internal/agent_teams/agent/payload_test.go`

- [ ] **Step 1: 创建 payload.go**

```go
package agent

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnPayloadBuilder 跨进程 spawn 载荷构造器。
// 对齐 Python: SpawnPayloadBuilder (openjiuwen/agent_teams/agent/payload.py)
//
// 集中管理 spawn teammate 时的跨进程 wire 格式。
// 输出键是 TeamAgent.FromSpawnPayload 的公共契约——
// 改这里的字段要同步改子进程入口。
//
// 状态仅在 memberPortMap 和 teammatePortCounter，
// 共同充当增量端口分配器：每个成员名获得稳定的端口分配。
type SpawnPayloadBuilder struct {
	// spec 团队 Agent 规格
	spec atschema.TeamAgentSpec
	// ctx 运行时上下文
	ctx atschema.TeamRuntimeContext
	// memberPortMap 成员名到端口的稳定映射
	memberPortMap map[string]int
	// teammatePortCounter 队友端口计数器
	teammatePortCounter int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpawnPayloadBuilder 创建新的 SpawnPayloadBuilder 实例。
// 对齐 Python: SpawnPayloadBuilder.__init__(spec, ctx)
func NewSpawnPayloadBuilder(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext) *SpawnPayloadBuilder {
	return &SpawnPayloadBuilder{
		spec:          spec,
		ctx:           ctx,
		memberPortMap: make(map[string]int),
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// BuildSpawnPayload 构建跨进程 spawn 载荷。
// 对齐 Python: SpawnPayloadBuilder.build_spawn_payload(ctx, initial_message)
//
// 输出 schema 是公共 wire 契约——必须保留每个键。
func (b *SpawnPayloadBuilder) BuildSpawnPayload(ctx atschema.TeamRuntimeContext, initialMessage string) map[string]any {
	teamSpec := ctx.TeamSpec
	teamName := ""
	displayName := ""
	leaderMemberName := ""
	if teamSpec != nil {
		teamName = teamSpec.TeamName
		displayName = teamSpec.DisplayName
		leaderMemberName = teamSpec.LeaderMemberName
	}

	// TODO(#9.65): memberTransport = b.BuildMemberMessagerConfig(ctx.MemberName)
	// 当 MessagerTransportConfig 实现后，序列化为 map
	var transport any = nil

	coordination := map[string]any{
		"team_name":           teamName,
		"display_name":        displayName,
		"leader_member_name":  leaderMemberName,
		"member_name":         ctx.MemberName,
		"role":                string(ctx.Role),
		"persona":             ctx.Persona,
		"transport":           transport,
	}

	query := initialMessage
	if query == "" {
		query = "Join the team and wait for your first assignment."
	}

	return map[string]any{
		"coordination": coordination,
		"query":        query,
	}
}

// BuildMemberContext 构造成员运行时上下文。
// 对齐 Python: SpawnPayloadBuilder.build_member_context(member_spec)
func (b *SpawnPayloadBuilder) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	return atschema.TeamRuntimeContext{
		Role:           memberSpec.RoleType,
		MemberName:     memberSpec.MemberName,
		Persona:        memberSpec.Persona,
		TeamSpec:       b.ctx.TeamSpec,
		MessagerConfig: b.BuildMemberMessagerConfig(memberSpec.MemberName),
		DBConfig:       b.ctx.DBConfig,
	}
}

// BuildMemberMessagerConfig 为指定成员分配稳定的传输配置。
// 对齐 Python: SpawnPayloadBuilder.build_member_messager_config(member_name)
//
// TODO(#9.65): MessagerTransportConfig 深拷贝和端口分配实现后替换
func (b *SpawnPayloadBuilder) BuildMemberMessagerConfig(memberName string) any {
	return nil
}

// BuildSpawnConfig 构建 SpawnAgentConfig。
// 对齐 Python: SpawnPayloadBuilder.build_spawn_config(ctx)
//
// TODO(#9.58): SpawnAgentConfig 类型定义后实现
func (b *SpawnPayloadBuilder) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) any {
	return nil
}
```

- [ ] **Step 2: 创建 payload_test.go**

```go
package agent_test

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	"github.com/stretchr/testify/assert"
)

// TestNewSpawnPayloadBuilder 测试构造函数
func TestNewSpawnPayloadBuilder(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.NotNil(t, b)
}

// TestSpawnPayloadBuilder_BuildSpawnPayload 测试输出 map 键完整性
func TestSpawnPayloadBuilder_BuildSpawnPayload(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	teamSpec := &atschema.TeamSpec{
		TeamName:         "test_team",
		DisplayName:      "Test Team",
		LeaderMemberName: "leader_1",
	}
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "teammate_1",
		Persona:    "coder",
		TeamSpec:   teamSpec,
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "Hello team")

	// 验证顶层键
	assert.Contains(t, payload, "coordination")
	assert.Contains(t, payload, "query")

	// 验证 query
	assert.Equal(t, "Hello team", payload["query"])

	// 验证 coordination 内容
	coord, ok := payload["coordination"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "test_team", coord["team_name"])
	assert.Equal(t, "Test Team", coord["display_name"])
	assert.Equal(t, "leader_1", coord["leader_member_name"])
	assert.Equal(t, "teammate_1", coord["member_name"])
	assert.Equal(t, "teammate", coord["role"])
	assert.Equal(t, "coder", coord["persona"])
	assert.Nil(t, coord["transport"])
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_默认Query 测试空 initialMessage 时使用默认 query
func TestSpawnPayloadBuilder_BuildSpawnPayload_默认Query(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleTeammate, MemberName: "t1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "")
	assert.Equal(t, "Join the team and wait for your first assignment.", payload["query"])
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_NilTeamSpec 测试 TeamSpec 为 nil
func TestSpawnPayloadBuilder_BuildSpawnPayload_NilTeamSpec(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleTeammate, MemberName: "t1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "hi")
	coord := payload["coordination"].(map[string]any)
	assert.Equal(t, "", coord["team_name"])
	assert.Equal(t, "", coord["display_name"])
	assert.Equal(t, "", coord["leader_member_name"])
}

// TestSpawnPayloadBuilder_BuildMemberContext 测试成员上下文构造
func TestSpawnPayloadBuilder_BuildMemberContext(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	teamSpec := &atschema.TeamSpec{TeamName: "test_team"}
	ctx := atschema.TeamRuntimeContext{
		Role:     atschema.TeamRoleLeader,
		TeamSpec: teamSpec,
		DBConfig: "mock_db_config",
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	memberSpec := atschema.TeamMemberSpec{
		MemberName: "teammate_1",
		RoleType:   atschema.TeamRoleTeammate,
		Persona:    "coder",
	}

	result := b.BuildMemberContext(memberSpec)
	assert.Equal(t, atschema.TeamRoleTeammate, result.Role)
	assert.Equal(t, "teammate_1", result.MemberName)
	assert.Equal(t, "coder", result.Persona)
	assert.Equal(t, teamSpec, result.TeamSpec)
	assert.Equal(t, "mock_db_config", result.DBConfig)
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_未实现 测试返回 nil（TODO 占位）
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_未实现(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.Nil(t, b.BuildMemberMessagerConfig("t1"))
}

// TestSpawnPayloadBuilder_BuildSpawnConfig_未实现 测试返回 nil（TODO 占位）
func TestSpawnPayloadBuilder_BuildSpawnConfig_未实现(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.Nil(t, b.BuildSpawnConfig(ctx))
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/agent/...`
Expected: 编译通过

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/agent/ -v -run "TestNewSpawnPayloadBuilder|TestSpawnPayloadBuilder"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/payload.go internal/agent_teams/agent/payload_test.go
git commit -m "feat(agent): add SpawnPayloadBuilder struct and methods (9.57)"
```

---

### Task 3: AgentConfigurator 结构体和属性代理（package agent）

**Files:**
- Create: `internal/agent_teams/agent/agent_configurator.go`
- Create: `internal/agent_teams/agent/agent_configurator_test.go`

- [ ] **Step 1: 创建 agent_configurator.go — 结构体 + 构造函数 + Getter/Setter + resolveTeamMode + ResolveAgentSpec**

完整代码较长，按区块组织：

**import 和结构体定义：**

```go
package agent

import (
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)
```

**结构体：** 见 spec 第 1 节 `AgentConfigurator` 定义。

**SetupInfraOption / SetupTeamBackendOption：** 见 spec 对应设计。

**Getter/Setter：** 20+ 对方法，全部代理到 `infra`/`resources`/`blueprint` 内部字段。每对格式：
```go
func (c *AgentConfigurator) Messager() any           { return c.infra.Messager }
func (c *AgentConfigurator) SetMessager(v any)        { c.infra.Messager = v }
```

**ResolveAgentSpec 静态方法（完整实现）：**
```go
// ResolveAgentSpec 按角色和成员名解析 AgentSpec。
// 对齐 Python: AgentConfigurator.resolve_agent_spec(spec, role, member_name)
//
// 优先级：memberName 精确匹配 → role 值匹配 → "teammate" → "leader"
func ResolveAgentSpec(spec atschema.TeamAgentSpec, role atschema.TeamRole, memberName string) atschema.DeepAgentSpec {
	if memberName != "" {
		if agentSpec, ok := spec.Agents[memberName]; ok {
			return agentSpec
		}
	}
	if agentSpec, ok := spec.Agents[string(role)]; ok {
		return agentSpec
	}
	if agentSpec, ok := spec.Agents["teammate"]; ok {
		return agentSpec
	}
	return spec.Agents["leader"]
}
```

**resolveTeamMode 非导出函数（完整实现）：**
```go
// resolveTeamMode 解析团队模式。
// 对齐 Python: _resolve_team_mode(spec)
func resolveTeamMode(spec atschema.TeamAgentSpec) string {
	if spec.TeamMode != "" {
		return spec.TeamMode
	}
	for _, m := range spec.PredefinedMembers {
		if m.RoleType != atschema.TeamRoleHumanAgent {
			return "hybrid"
		}
	}
	return "default"
}
```

**Configure / SetupInfra / SetupAgent / SetupTeamBackend / CreateWorkspaceManager / CreateWorktreeManager / BuildMemoryManager / UpdateModelPool / AttachModelAllocator / RestoreAllocatorState：** 骨架方法，完整流程注释对齐 Python，TODO 占位标注待实现章节。

**代理方法：**
```go
func (c *AgentConfigurator) BuildSpawnPayload(ctx atschema.TeamRuntimeContext, initialMessage string) map[string]any {
	if c.spawnPayloadBuilder == nil { return nil }
	return c.spawnPayloadBuilder.BuildSpawnPayload(ctx, initialMessage)
}
func (c *AgentConfigurator) BuildMemberContext(memberSpec atschema.TeamMemberSpec) atschema.TeamRuntimeContext {
	if c.spawnPayloadBuilder == nil { return atschema.TeamRuntimeContext{} }
	return c.spawnPayloadBuilder.BuildMemberContext(memberSpec)
}
func (c *AgentConfigurator) BuildMemberMessagerConfig(memberName string) any {
	if c.spawnPayloadBuilder == nil { return nil }
	return c.spawnPayloadBuilder.BuildMemberMessagerConfig(memberName)
}
func (c *AgentConfigurator) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) any {
	if c.spawnPayloadBuilder == nil { return nil }
	return c.spawnPayloadBuilder.BuildSpawnConfig(ctx)
}
```

- [ ] **Step 2: 创建 agent_configurator_test.go**

测试用例覆盖：
- `TestNewAgentConfigurator` — 构造函数验证
- `TestResolveAgentSpec` — 按优先级解析（memberName > role > "teammate" > "leader"）
- `TestResolveTeamMode` — default / hybrid / predefined 三种场景
- `TestAgentConfigurator_GetterSetter` — 所有属性代理的读写
- `TestAgentConfigurator_Configure` — Configure 调用 SetupInfra + SetupAgent
- `TestAgentConfigurator_AttachModelAllocator` — 完整实现的方法
- `TestAgentConfigurator_RestoreAllocatorState` — 完整实现的方法
- `TestAgentConfigurator_BuildSpawnPayload代理` — 代理到 SpawnPayloadBuilder
- `TestAgentConfigurator_BuildMemberContext代理` — 代理到 SpawnPayloadBuilder

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/agent/...`
Expected: 编译通过

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/agent/ -v -run "TestNewAgentConfigurator|TestResolveAgentSpec|TestResolveTeamMode|TestAgentConfigurator"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/agent_configurator.go internal/agent_teams/agent/agent_configurator_test.go
git commit -m "feat(agent): add AgentConfigurator struct, property proxies, and method stubs (9.57)"
```

---

### Task 4: 注释格式统一 + resources.go 类型升级

**Files:**
- Modify: `internal/agent_teams/agent/infra.go`
- Modify: `internal/agent_teams/agent/resources.go`
- Modify: `internal/agent_teams/agent/member.go`

- [ ] **Step 1: 更新 infra.go 注释格式**

将所有 `⤵️ 回填: 9.xx —` 格式替换为 `TODO(#9.xx):` 格式。例如：
```
// 修改前：Messager any  // ⤵️ 回填: 9.65 — Messager 类型
// 修改后：Messager any  // TODO(#9.65): Messager 类型
```

- [ ] **Step 2: 更新 resources.go 注释格式 + Harness 字段类型升级**

注释格式同上。同时将 `Harness` 字段从 `any` 升级为 `*agentteams.TeamHarness`：

```go
// 修改前：
import ()

// 修改后：
import (
	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
)

// Harness 底层 DeepAgent 运行时的 Harness
// TODO(#9.57): 改为 TeamHarness 类型
Harness *agentteams.TeamHarness
```

- [ ] **Step 3: 更新 member.go 注释格式**

将所有 `⤵️ 回填: 9.xx —` 格式替换为 `TODO(#9.xx):` 格式。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/agent/...`
Expected: 编译通过

- [ ] **Step 5: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/agent/ -v`
Expected: 全部 PASS（包括既有测试）

- [ ] **Step 6: 提交**

```bash
git add internal/agent_teams/agent/infra.go internal/agent_teams/agent/resources.go internal/agent_teams/agent/member.go
git commit -m "refactor(agent): unify TODO comment format and upgrade Harness field type (9.57)"
```

---

### Task 5: TeamAgent 回填（9.55）

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go`
- Modify: `internal/agent_teams/agent/doc.go`

- [ ] **Step 1: 升级 configurator 字段类型**

```go
// 修改前：
configurator any

// 修改后：
configurator *AgentConfigurator
```

- [ ] **Step 2: 更新 NewTeamAgent 构造函数**

```go
// 修改前：
a := &TeamAgent{
    card:  card,
    state: NewTeamAgentState(),
}
// ⤵️ 回填: 9.57 — 构建 AgentConfigurator(card)

// 修改后：
a := &TeamAgent{
    card:         card,
    state:        NewTeamAgentState(),
    configurator: NewAgentConfigurator(card),
}
```

- [ ] **Step 3: 回填属性代理方法（20+ 个）**

将所有带 `// ⤵️ 回填: 9.57` 注释的方法替换为实际调用 configurator。格式：

```go
func (a *TeamAgent) Blueprint() *TeamAgentBlueprint {
	if a.configurator != nil {
		return a.configurator.Blueprint()
	}
	return nil
}
```

适用：`Blueprint`, `Infra`, `Resources`, `Spec`, `RuntimeContext`, `Role`, `Lifecycle`, `TeamSpec`, `MemberName`, `MessageManager`, `TaskManager`, `TeamBackend`, `TeamName`, `IsAgentReady`

`Harness()` 返回类型从 `any` 改为 `*agentteams.TeamHarness`。

- [ ] **Step 4: 回填 Configure() 方法**

```go
func (a *TeamAgent) Configure(ctx context.Context, spec atschema.TeamAgentSpec, runtimeCtx atschema.TeamRuntimeContext) *TeamAgent {
	// Phase 1: 基础设施搭建
	a.configurator.SetupInfra(spec, runtimeCtx,
		WithOnTeammateCreated(nil), // TODO(#9.55): a.onTeammateCreated
		WithOnTeamCleaned(nil),     // TODO(#9.55): a.markTeamCleaned
		WithOnTeamBuilt(nil),       // TODO(#9.55): a.markTeamBuilt
	)

	// Phase 2: Agent 组装
	a.configurator.SetupAgent(spec, runtimeCtx)

	// 构建 TeamMember 句柄
	if runtimeCtx.MemberName != "" {
		a.state.TeamMember = CreateMemberHandle(
			runtimeCtx.MemberName,
			a.configurator.Blueprint(),
			a.configurator.Infra(),
			a.card,
		)
	}

	// TODO(#9.62): coordination.setup(role=ctx.Role)
	// TODO(#9.55): a.registerTeamCompletionCallbacks()

	logger.Info(logComponent).Str("member_name", runtimeCtx.MemberName).
		Str("role", string(runtimeCtx.Role)).Msg("TeamAgent.Configure")
	return a
}
```

- [ ] **Step 5: 回填其他代理方法**

`BuildSpawnPayload`, `BuildMemberContext`, `BuildSpawnConfig`, `AttachModelAllocator`, `RestoreAllocatorState`, `UpdateModelPool`, `RegisterRail`, `UnregisterRail` — 全部改为代理到 configurator。

- [ ] **Step 6: 更新 doc.go 文件目录**

在文件目录树中添加 `agent_configurator.go` 和 `payload.go` 条目。

- [ ] **Step 7: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/agent/...`
Expected: 编译通过

- [ ] **Step 8: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -v`
Expected: 全部 PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agent_teams/agent/team_agent.go internal/agent_teams/agent/doc.go
git commit -m "feat(agent): backfill TeamAgent property proxies and Configure() via AgentConfigurator (9.55+9.57)"
```

---

### Task 6: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.57 状态为 ✅**

将 `| 9.57 | ☐ | AgentConfigurator | Agent 配置器 |` 改为 `| 9.57 | ✅ | AgentConfigurator | Agent 配置器（骨架+TODO占位） |`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: mark 9.57 AgentConfigurator as complete"
```
