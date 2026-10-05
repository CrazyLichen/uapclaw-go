# 9.85 TeamRunner 设计

## 概述

TeamRunner 是 Runner 对外暴露的"团队执行入口"，位于 Runner→TeamRuntimeManager→TeamAgent 调用链的中间层。
Python 中由 `_TeamRunnerMixin`（实例方法）+ `_TeamRunnerClassMixin`（类方法门面）实现，存放在 `openjiuwen/core/runner/team_runner.py`。

Go 实现方案：在 `runner/` 包下新建 `team_runner.go`，放置所有团队相关方法。

## 流程位置

```
外部请求 → Gateway → E2A → AgentServer → DeepAdapter(team分流)
  └─⭐ Runner.RunAgentTeam / RunAgentTeamStreaming   ← 9.85
       ├─ resolveTeamAgentSpec()          ← 解析 spec（从 pool/session bucket）
       ├─ TeamRuntimeManager.Activate()   ← 激活/恢复（内联回填 #9.62）
       ├─ TeamAgent.Invoke / Stream       ← 执行团队
       ├─ TeamRuntimeManager.Finalize()   ← 运行后终结（暂停/停止决策）
       └─ closeTeamInteractGate()         ← 关闭交互门
```

## 文件清单

| 文件 | 类型 | 职责 |
|------|------|------|
| `internal/agent_teams/runtime/dispatch.go` | **新建** | RunActionKind 枚举 + RunAction 结构体 + DecideRunAction 纯决策函数 |
| `internal/agent_teams/runtime/dispatch_test.go` | **新建** | 决策函数单元测试 |
| `internal/agent_teams/runtime/manager.go` | **修改** | 补充 Activate/FinalizeMember/GetMonitor/ListActiveTeams + 内部辅助 |
| `internal/agentcore/runner/team_runner.go` | **新建** | TeamRunner 全部方法（三条路径 + 交互 + 控制 + 辅助） |
| `internal/agentcore/runner/team_runner_test.go` | **新建** | TeamRunner 单元测试 |
| `internal/agentcore/runner/doc.go` | **修改** | 文件目录添加 team_runner.go |

## 实现步骤（先底层后上层）

### 步骤 1：runtime/dispatch.go — 纯调度决策

新建 `internal/agent_teams/runtime/dispatch.go`，放置纯函数，无副作用：

```go
// RunActionKind 调度动作枚举
// Python: RunActionKind (openjiuwen/agent_teams/runtime/dispatch.py)
type RunActionKind string

const (
    RunActionKindCreate             RunActionKind = "create"
    RunActionKindNewTeamInSession   RunActionKind = "new_team_in_session"
    RunActionKindColdRecover        RunActionKind = "cold_recover"
    RunActionKindResumeFromPause    RunActionKind = "resume_from_pause"
    RunActionKindRejectRunning      RunActionKind = "reject_running"
    RunActionKindRejectOrphaned     RunActionKind = "reject_orphaned"
    RunActionKindRejectInconsistent RunActionKind = "reject_inconsistent"
)

// RunAction 调度决策结果
// Python: RunAction (dispatch.py)
type RunAction struct {
    Kind        RunActionKind
    RequireSpec bool
    Reason      string
}

// IsTeamRejectKind 判断是否为拒绝类调度动作
// Python: _is_team_reject_kind(kind)
func IsTeamRejectKind(kind RunActionKind) bool

// DecideRunAction 纯调度决策（无副作用）
// Python: decide_run_action(team_in_db, team_in_session, pool_entry, ...)
func DecideRunAction(
    teamInDB bool, teamInSession bool,
    poolEntry *ActiveTeam,
    targetSessionID string, targetTeamName string,
    teamDBState string,
) RunAction
```

**决策真值表**（对齐 Python）：

| teamInDB | teamInSession | poolEntry | 结果 |
|----------|--------------|-----------|------|
| false | false | nil | CREATE |
| false | false | present | REJECT_INCONSISTENT |
| false | true | — | REJECT_ORPHANED (PENDING_CREATE/CLEANED → CREATE) |
| true | false | nil | NEW_TEAM_IN_SESSION |
| true | true | nil | COLD_RECOVER |
| true | true | RUNNING | REJECT_RUNNING |
| true | true | PAUSED | RESUME_FROM_PAUSE |

### 步骤 2：runtime/manager.go — 补充 Activate 等方法

在现有 `manager.go` 中补充以下类型和方法：

**新类型**：

```go
// TeamRuntimeActivation 激活结果
// Python: TeamRuntimeActivation (manager.py)
type TeamRuntimeActivation struct {
    Agent   *agent.TeamAgent
    Session *session.AgentTeamSession
    Action  RunAction
}
```

**新包级变量**：

```go
var (
    // teamRejectKinds 拒绝类调度动作集合
    // Python: _REJECT_KINDS
    teamRejectKinds = map[RunActionKind]bool{
        RunActionKindRejectRunning:    true,
        RunActionKindRejectOrphaned:   true,
        RunActionKindRejectInconsistent: true,
    }
    // memberFinalizedStatuses 成员已终结状态集合
    // Python: _MEMBER_FINALIZED_STATUSES
    memberFinalizedStatuses = map[atschema.MemberStatus]bool{
        atschema.MemberStatusStopped:  true,
        atschema.MemberStatusPaused:   true,
        atschema.MemberStatusShutdown: true,
    }
)
```

**替换现有 Activate 桩实现**（当前返回 nil error，无实际逻辑）：

```go
// Activate 激活团队（完整实现，内联回填 #9.62）
// Python: TeamRuntimeManager.activate(spec, session, inputs)
func (m *TeamRuntimeManager) Activate(
    ctx context.Context,
    spec *atschema.TeamAgentSpec,
    session any, // string | *session.AgentTeamSession | nil
    inputs any,
) (*TeamRuntimeActivation, error)
```

内部逻辑：
1. `buildTeamSession(spec, session)` → 创建 AgentTeamSession
2. `pool.Get(teamName)` → 查看是否有活跃池条目
3. 跨 session 策略：若池条目 session 不匹配，stop_team + 清空 pool_entry
4. `inspectSession(spec, teamSession, teamName)` → 检查 DB 和 session 中团队状态
5. `DecideRunAction(...)` → 纯决策
6. `applyAction(...)` → 执行决策副作用

**新导出方法**：

```go
// FinalizeMember 终结 teammate/human-agent（非 leader）
// Python: TeamRuntimeManager.finalize_member(agent)
// 静态方法，不依赖 pool
func FinalizeMember(ctx context.Context, ag *agent.TeamAgent) error

// GetMonitor 获取活跃团队的 TeamMonitor
// Python: TeamRuntimeManager.get_monitor(team_name, session_id, hide_dm)
func (m *TeamRuntimeManager) GetMonitor(
    ctx context.Context, teamName string, sessionID string, hideDM bool,
) (*monitor.TeamMonitor, error)

// ListActiveTeams 列出所有活跃团队
// Python: TeamRuntimeManager.list_active_teams()
func (m *TeamRuntimeManager) ListActiveTeams() []ActiveTeamInfo
```

**新内部方法**：

```go
// inspectSession 检查 DB 和 session 中的团队状态
// Python: TeamRuntimeManager._inspect_session(spec, team_session, team_name)
func (m *TeamRuntimeManager) inspectSession(
    ctx context.Context,
    spec *atschema.TeamAgentSpec,
    teamSession *session.AgentTeamSession,
    teamName string,
) (teamInSession bool, teamInDB bool, teamDBState string, err error)

// applyAction 执行调度决策的副作用
// Python: TeamRuntimeManager._apply_action(action, spec, team_session, pool_entry, inputs)
func (m *TeamRuntimeManager) applyAction(
    ctx context.Context,
    action RunAction,
    spec *atschema.TeamAgentSpec,
    teamSession *session.AgentTeamSession,
    poolEntry *ActiveTeam,
    inputs any,
) (*TeamRuntimeActivation, error)
```

**新包级辅助函数**（对齐 Python staticmethod）：

```go
// buildTeamSession 从 spec+session 参数创建 AgentTeamSession
// Python: TeamRuntimeManager._build_session(spec, session)
func buildTeamSession(spec *atschema.TeamAgentSpec, session any) *session.AgentTeamSession

// preRunWithInputs 调用 session.pre_run(inputs)
// Python: TeamRuntimeManager._pre_run_with_inputs(session, inputs)
func preRunWithInputs(ctx context.Context, sess *session.AgentTeamSession, inputs any) error

// flushTeamManifest 持久化团队清单 + 刷检查点
// Python: TeamRuntimeManager._flush_team_manifest(agent, session)
func flushTeamManifest(ctx context.Context, ag *agent.TeamAgent, sess *session.AgentTeamSession) error
```

**applyAction 四条恢复路径**：

| kind | 逻辑 |
|------|------|
| REJECT_* | 返回 `TeamRuntimeActivation{agent: pool_entry.agent or nil, action}`，不执行副作用 |
| RESUME_FROM_PAUSE | `pre_run_with_inputs` → 设 pool_entry.state=RUNNING → `interact_gate.reset()` → 返回现有 agent |
| COLD_RECOVER | `TeamAgent.RecoverFromSession()` → `agent.RecoverTeam()` → pool.Add → 返回新 agent |
| NEW_TEAM_IN_SESSION | `pre_run_with_inputs` → `spec.Build()` → `agent.ResumeForNewSession()` → `agent.RecoverTeam()` → `flushTeamManifest` → pool.Add |
| CREATE | `pre_run_with_inputs` → `spec.Build()` → `flushTeamManifest` → pool.Add |

**FinalizeMember 逻辑**：

```
1. member = agent.TeamMember() (可能 nil)
2. current_status = member.Status() (可能 nil/异常)
3. 若 current_status ∈ {STOPPED, PAUSED, SHUTDOWN}:
     → agent.StopCoordination() (仅关闭 kernel)
4. 若 current_status == SHUTDOWN_REQUESTED:
     → agent.StopCoordination() + member.UpdateStatus(SHUTDOWN)
5. 默认（含 nil）:
     → agent.PauseCoordination() + member.UpdateStatus(READY)
```

### 步骤 3：runner/team_runner.go — 门面层

**包级函数**（对齐 Python `_TeamRunnerClassMixin` classmethod，路由到 Runner 方法）：

```go
// RunAgentTeam 执行团队（统一入口，路由到三条路径）
// Python: Runner.run_agent_team(agent_team, inputs, *, base, member, session, context, envs)
func RunAgentTeam(
    ctx context.Context,
    agentTeam any, // string | *schema.TeamAgentSpec | interfaces.BaseAgent
    inputs map[string]any,
    member bool, base bool,
    session any, // string | *session.AgentTeamSession | *session.Session | nil
    modelCtx any, envs map[string]any,
) (map[string]any, error)

// RunAgentTeamStreaming 流式执行团队
// Python: Runner.run_agent_team_streaming(...)
func RunAgentTeamStreaming(
    ctx context.Context,
    agentTeam any,
    inputs map[string]any,
    member bool, base bool,
    session any,
    modelCtx any,
    streamModes []stream.StreamMode,
    envs map[string]any,
    streamLogger *monitor.TeamStreamLogger,
) (<-chan stream.Schema, error)

// InteractAgentTeam 向活跃团队发送交互载荷
// Python: Runner.interact_agent_team(payload, *, team_name, session_id)
func InteractAgentTeam(
    ctx context.Context,
    payload any, // string | InteractPayload
    teamName string, sessionID string,
) (*interaction.DeliverResult, error)

// RegisterHumanAgentInbound 注册团队→用户通知回调
// Python: Runner.register_human_agent_inbound(*, team_name, session_id, member_name, callback)
func RegisterHumanAgentInbound(
    ctx context.Context,
    teamName string, sessionID string, memberName string, callback any,
) (bool, error)

// GetAgentTeamMonitor 获取活跃团队的 TeamMonitor
// Python: Runner.get_agent_team_monitor(*, team_name, session_id, hide_dm)
func GetAgentTeamMonitor(
    ctx context.Context,
    teamName string, sessionID string, hideDM bool,
) (*monitor.TeamMonitor, error)

// ListActiveTeams 列出所有活跃团队
// Python: Runner.list_active_teams()
func ListActiveTeams() []runtime.ActiveTeamInfo
```

**Runner 实例方法**（对齐 Python `_TeamRunnerMixin`，同包可访问私有字段）：

```go
// TeamAgent 路径
func (r *Runner) runAgentTeam(ctx, spec, inputs, session) (map[string]any, error)
func (r *Runner) runAgentTeamStreaming(ctx, spec, inputs, session, ...) (<-chan stream.Schema, error)

// BaseTeam 路径 (base=True)
func (r *Runner) runBaseTeam(ctx, baseTeam, inputs, session, ...) (any, error)
func (r *Runner) runBaseTeamStreaming(ctx, baseTeam, inputs, session, ...) (<-chan stream.Schema, error)

// Member 路径 (member=True, spawn-only)
func (r *Runner) runTeamMember(ctx, agent, inputs, session) (map[string]any, error)
func (r *Runner) runTeamMemberStreaming(ctx, agent, inputs, session) (<-chan stream.Schema, error)
```

**内部辅助方法/函数**：

```go
// resolveTeamAgentSpec 解析 str/TeamAgentSpec 为完整 TeamAgentSpec
// Python: _TeamRunnerMixin._resolve_team_agent_spec(agent_team, session)
func (r *Runner) resolveTeamAgentSpec(ctx, agentTeam, session) (*atschema.TeamAgentSpec, error)

// resolveSpecFromSessionBucket 从 session bucket 恢复 TeamAgentSpec
// Python: _TeamRunnerMixin._resolve_spec_from_session_bucket(team_name, session)
func (r *Runner) resolveSpecFromSessionBucket(ctx, teamName, session) (*atschema.TeamAgentSpec, error)

// prepareBaseTeam 解析 str/BaseTeam 为 BaseTeam 实例
// Python: _TeamRunnerMixin._prepare_base_team(base_team)
func (r *Runner) prepareBaseTeam(ctx, baseTeam) (maschema.BaseTeam, error)

// createAgentTeamSessionFromRef 从多种 session 引用创建 AgentTeamSession
// Python: _TeamRunnerMixin._create_agent_team_session(agent_team, session)
func createAgentTeamSessionFromRef(agentTeam any, session any) *session.AgentTeamSession

// closeTeamInteractGate 关闭并排空交互门
// Python: _TeamRunnerMixin._close_team_interact_gate(team_name, session_id)
func (r *Runner) closeTeamInteractGate(teamName, sessionID)

// buildTeamRuntimeReadyChunk 构造运行时就绪 chunk
// Python: _TeamRunnerMixin._build_team_runtime_ready_chunk(...)
func buildTeamRuntimeReadyChunk(teamName, sessionID string, actionKind runtime.RunActionKind,
    leaderMemberName string, leaderRole atschema.TeamRole) *atschema.TeamOutputSchema

// buildTeamControlChunk 构造控制 chunk
// Python: _TeamRunnerMixin._build_team_control_chunk(payload, ...)
func buildTeamControlChunk(payload map[string]any, leaderMemberName string,
    leaderRole atschema.TeamRole) *atschema.TeamOutputSchema

// bindInteractTeamSession 绑定交互会话上下文（用于 InteractAgentTeam）
// Python: _TeamRunnerMixin._bind_interact_team_session(session_id)
func bindInteractTeamSession(ctx context.Context, sessionID string) context.Context
```

### 步骤 4：回填 14 处 ⤵️ 标记

实现 TeamRunner 后，清理以下占位：

| 文件 | 标记内容 | 回填方式 |
|------|---------|---------|
| `spawn/inprocess_spawn.go` (2处) | goroutine 调 Runner.RunAgentTeam | 调用 `runner.RunAgentTeam()` |
| `spawn/shared_resources.go` (3处) | 共享 TeamRuntime 单例 | 调用 `runtime.GetTeamRuntimeManager()` |
| `spawn/shared_resources_test.go` | 返回 nil | 改为返回真实实例 |
| `runtime/manager.go` (1处) | onInbound nil 注入 | 从 `runner.RegisterHumanAgentInbound` 回填 |
| `runner/spawn/child.go` | team_agent 模式报错 | 调用 `runner.RunAgentTeam()` |
| `deep_adapter_team.go` (7处) | 创建/streaming/monitor/watcher | 调用 `runner.RunAgentTeamStreaming()` / `runner.GetAgentTeamMonitor()` |
| `team_manager_monitor.go` (2处) | pending_waiters / server_push | 调用 `runner.ListActiveTeams()` + channel 广播 |

### 步骤 5：测试

- `dispatch_test.go`：7 种决策路径 + PENDING_CREATE/CLEANED 覆盖
- `team_runner_test.go`：三条路径的 mock 测试（mock TeamRuntimeManager / TeamAgent）

## Session 传递方式

- TeamAgent 的 `Invoke`/`Stream` 接受 `interfaces.WithSession(facade)`
- `*session.AgentTeamSession` 满足 `sessioninterfaces.SessionFacade`，直接传
- BaseTeam 的 `Invoke`/`Stream` 用 `maschema.WithTeamSession(sess)` 传 `*session.AgentTeamSession`

## 与现有 runner.go 的关系

- `PauseAgentTeam` / `StopAgentTeam` / `DeleteAgentTeam` 已在 `runner.go` 中实现，**保留不动**
- 新方法全部放 `team_runner.go`，避免 `runner.go` 膨胀
- `team_runner.go` 中的包级函数可直接调 `runner.go` 中的 `getRunner()` 和已有方法
- `team_runner.go` 中的 `Runner` 方法可访问 `r.teamRuntimeManager` 等私有字段

## import 依赖

`runner/team_runner.go` 需要新增的 import：

```go
import (
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
    atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
    "github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
    maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/session"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)
```

注意：`runner` 包已 import `agent_teams/registry`（通过 `teamRuntimeManager registry.PoolAccessor`），新增 import 无循环依赖风险。
