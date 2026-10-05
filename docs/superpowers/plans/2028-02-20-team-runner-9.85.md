# TeamRunner (9.85) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement TeamRunner — Runner 对外暴露的团队执行入口，三条路径（TeamAgent/BaseTeam/Member）一步到位，内联回填 Activate。

**Architecture:** 新建 `runtime/dispatch.go`（纯调度决策）→ 修改 `runtime/manager.go`（补充 Activate/FinalizeMember/GetMonitor/ListActiveTeams）→ 新建 `runner/team_runner.go`（门面层 15+ 包级函数 + 8+ Runner 方法）→ 回填 14 处 ⤵️ 标记。

**Tech Stack:** Go 1.22+, 现有 agent_teams/runtime + agentcore/runner 包

**Design spec:** `docs/superpowers/specs/2028-02-20-team-runner-9.85-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/agent_teams/runtime/dispatch.go` | Create | RunActionKind 枚举 + RunAction 结构体 + DecideRunAction 纯决策函数 + IsTeamRejectKind |
| `internal/agent_teams/runtime/dispatch_test.go` | Create | DecideRunAction 7+ 决策路径测试 |
| `internal/agent_teams/runtime/manager.go` | Modify | TeamRuntimeActivation 结构体 + 替换 Activate 桩实现 + FinalizeMember + GetMonitor + ListActiveTeams + inspectSession + applyAction + buildTeamSession + preRunWithInputs + flushTeamManifest |
| `internal/agentcore/runner/team_runner.go` | Create | 全部 TeamRunner 方法（RunAgentTeam/Streaming/BaseTeam/Member/Interact/Monitor/ListActive + resolve/prepare/create/close/build 辅助） |
| `internal/agentcore/runner/team_runner_test.go` | Create | TeamRunner mock 测试 |
| `internal/agentcore/runner/doc.go` | Modify | 文件目录添加 team_runner.go |

---

### Task 1: runtime/dispatch.go — 纯调度决策

**Files:**
- Create: `internal/agent_teams/runtime/dispatch.go`
- Test: `internal/agent_teams/runtime/dispatch_test.go`

- [ ] **Step 1: 编写 dispatch.go**

```go
package runtime

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RunAction 调度决策结果。
// Python: RunAction (openjiuwen/agent_teams/runtime/dispatch.py)
type RunAction struct {
	// Kind 调度动作
	Kind RunActionKind
	// RequireSpec 是否需要 TeamAgentSpec
	RequireSpec bool
	// Reason 拒绝原因（空字符串表示无原因）
	Reason string
}

// ──────────────────────────── 枚举 ────────────────────────────

// RunActionKind 调度动作枚举。
// Python: RunActionKind (dispatch.py)
type RunActionKind string

const (
	// RunActionKindCreate 创建新团队
	RunActionKindCreate RunActionKind = "create"
	// RunActionKindNewTeamInSession 在新 session 上启动已有团队
	RunActionKindNewTeamInSession RunActionKind = "new_team_in_session"
	// RunActionKindColdRecover 冷恢复（从 session checkpoint 恢复）
	RunActionKindColdRecover RunActionKind = "cold_recover"
	// RunActionKindResumeFromPause 从暂停恢复
	RunActionKindResumeFromPause RunActionKind = "resume_from_pause"
	// RunActionKindRejectRunning 拒绝：团队已在运行
	RunActionKindRejectRunning RunActionKind = "reject_running"
	// RunActionKindRejectOrphaned 拒绝：session 有桶但 DB 无行
	RunActionKindRejectOrphaned RunActionKind = "reject_orphaned"
	// RunActionKindRejectInconsistent 拒绝：pool 有条目但 DB 无行
	RunActionKindRejectInconsistent RunActionKind = "reject_inconsistent"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTeamRejectKind 判断是否为拒绝类调度动作。
// Python: _is_team_reject_kind(kind)
func IsTeamRejectKind(kind RunActionKind) bool {
	return kind == RunActionKindRejectRunning ||
		kind == RunActionKindRejectOrphaned ||
		kind == RunActionKindRejectInconsistent
}

// DecideRunAction 纯调度决策（无副作用）。
// Python: decide_run_action(team_in_db, team_in_session, pool_entry, ...)
//
// 决策真值表：
//
//	teamInDB  teamInSession  poolEntry   结果
//	false     false          nil         CREATE
//	false     false          present     REJECT_INCONSISTENT
//	false     true           —           REJECT_ORPHANED (pending_create/cleaned → CREATE)
//	true      false          nil         NEW_TEAM_IN_SESSION
//	true      true           nil         COLD_RECOVER
//	true      true           RUNNING     REJECT_RUNNING
//	true      true           PAUSED      RESUME_FROM_PAUSE
func DecideRunAction(
	teamInDB bool,
	teamInSession bool,
	poolEntry *ActiveTeam,
	targetSessionID string,
	targetTeamName string,
	teamDBState string,
) RunAction {
	// 可重建：session 桶中的 pending_create / cleaned 状态
	if !teamInDB && teamInSession {
		if teamDBState == metadata.TeamDBStatePendingCreate ||
			teamDBState == metadata.TeamDBStateCleaned {
			return RunAction{Kind: RunActionKindCreate, RequireSpec: true}
		}
		return RunAction{
			Kind:        RunActionKindRejectOrphaned,
			RequireSpec: false,
			Reason:      fmt.Sprintf("team %q not in DB but session bucket exists for %q", targetTeamName, targetSessionID),
		}
	}

	// 不一致：pool 有条目但 DB 无行
	if !teamInDB && poolEntry != nil {
		return RunAction{
			Kind:        RunActionKindRejectInconsistent,
			RequireSpec: false,
			Reason:      fmt.Sprintf("team %q present in pool but missing from DB", targetTeamName),
		}
	}

	// 全新团队
	if !teamInDB {
		return RunAction{Kind: RunActionKindCreate, RequireSpec: true}
	}

	// 冷路径（无 pool 条目，DB 有团队）
	if poolEntry == nil {
		if teamInSession {
			return RunAction{Kind: RunActionKindColdRecover, RequireSpec: false}
		}
		return RunAction{Kind: RunActionKindNewTeamInSession, RequireSpec: false}
	}

	// Pool 条目存在。Activate 保证条目属于 targetSessionID。
	if poolEntry.SessionID != targetSessionID {
		panic(fmt.Sprintf(
			"dispatch invariant violated: pool entry for %q on session %q must be torn down before dispatching to session %q",
			targetTeamName, poolEntry.SessionID, targetSessionID,
		))
	}
	if poolEntry.State == RuntimeStatePaused {
		return RunAction{Kind: RunActionKindResumeFromPause, RequireSpec: false}
	}
	return RunAction{
		Kind:        RunActionKindRejectRunning,
		RequireSpec: false,
		Reason:      fmt.Sprintf("team %q already running on session %q; use interact", targetTeamName, targetSessionID),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编写 dispatch_test.go**

编写 7+ 决策路径测试（CREATE / REJECT_ORPHANED / REJECT_INCONSISTENT / NEW_TEAM_IN_SESSION / COLD_RECOVER / REJECT_RUNNING / RESUME_FROM_PAUSE / PENDING_CREATE 覆盖 / CLEANED 覆盖 / pool 不匹配 panic），对齐 Python decide_run_action 真值表。每个 case 用表驱动测试。

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/runtime/... -run TestDecide -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/runtime/dispatch.go internal/agent_teams/runtime/dispatch_test.go
git commit -m "feat(9.85): runtime/dispatch — RunActionKind + DecideRunAction 纯调度决策"
```

---

### Task 2: runtime/manager.go — 补充 Activate + FinalizeMember + GetMonitor + ListActiveTeams

**Files:**
- Modify: `internal/agent_teams/runtime/manager.go`

- [ ] **Step 1: 添加 TeamRuntimeActivation 结构体和包级变量**

在 manager.go 结构体区块添加：

```go
// TeamRuntimeActivation 激活结果。
// Python: TeamRuntimeActivation (openjiuwen/agent_teams/runtime/manager.py)
type TeamRuntimeActivation struct {
	// Agent 激活的 TeamAgent 实例
	Agent *agent.TeamAgent
	// Session 绑定的 AgentTeamSession
	Session *session.AgentTeamSession
	// Action 调度决策结果
	Action RunAction
}
```

在全局变量区块添加：

```go
var (
	// memberFinalizedStatuses 成员已终结状态集合
	// Python: _MEMBER_FINALIZED_STATUSES = frozenset({STOPPED, PAUSED, SHUTDOWN})
	memberFinalizedStatuses = map[atschema.MemberStatus]bool{
		atschema.MemberStatusStopped:  true,
		atschema.MemberStatusPaused:   true,
		atschema.MemberStatusShutdown: true,
	}
)
```

需新增 import:
- `atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"`

- [ ] **Step 2: 替换 Activate 桩实现为完整实现**

替换现有的 `Activate` 方法（当前是桩实现）为：

```go
// Activate 激活团队（完整实现，内联回填 #9.62）。
// Python: TeamRuntimeManager.activate(spec, session, inputs)
//
// 执行步骤：
//  1. buildTeamSession → 创建 AgentTeamSession
//  2. pool.Get → 查看是否有活跃池条目
//  3. 跨 session 策略：若池条目 session 不匹配，stop_team + 清空 poolEntry
//  4. inspectSession → 检查 DB 和 session 中团队状态
//  5. DecideRunAction → 纯决策
//  6. applyAction → 执行决策副作用
func (m *TeamRuntimeManager) Activate(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	session any,
	inputs any,
) (*TeamRuntimeActivation, error) {
	// 步骤 1: 创建 AgentTeamSession
	teamSession := buildTeamSession(spec, session)
	targetSessionID := teamSession.GetSessionID()
	teamName := spec.TeamName

	// 步骤 2: 查看是否有活跃池条目
	poolEntry := m.pool.Get(teamName)

	// 步骤 3: 跨 session 策略
	if poolEntry != nil && poolEntry.SessionID != targetSessionID {
		logger.Info(mgrLogComponent).
			Str("team_name", teamName).
			Str("old_session_id", poolEntry.SessionID).
			Str("new_session_id", targetSessionID).
			Msg("activate: 池条目 session 不匹配，stop+remove 后重建")
		_, _ = m.StopTeam(ctx, teamName, poolEntry.SessionID)
		poolEntry = nil
	}

	// 步骤 4: 检查 DB 和 session 状态
	teamInSession, teamInDB, teamDBState, err := m.inspectSession(ctx, spec, teamSession, teamName)
	if err != nil {
		return nil, err
	}

	// 步骤 5: 纯调度决策
	action := DecideRunAction(
		teamInDB, teamInSession,
		poolEntry,
		targetSessionID, teamName,
		teamDBState,
	)

	logger.Info(mgrLogComponent).
		Str("team_name", teamName).
		Str("session_id", targetSessionID).
		Str("action_kind", string(action.Kind)).
		Bool("in_db", teamInDB).
		Bool("in_session", teamInSession).
		Str("db_state", teamDBState).
		Bool("pooled", poolEntry != nil).
		Msg("activate: 调度决策完成")

	// 步骤 6: 执行决策副作用
	return m.applyAction(ctx, action, spec, teamSession, poolEntry, inputs)
}
```

- [ ] **Step 3: 添加 FinalizeMember 函数**

在导出函数区块添加：

```go
// FinalizeMember 终结 teammate/human-agent（非 leader）。
// Python: TeamRuntimeManager.finalize_member(agent)
//
// 决策规则：
//   - 已终结（STOPPED/PAUSED/SHUTDOWN）→ 仅关闭 kernel
//   - SHUTDOWN_REQUESTED → stop + mark SHUTDOWN
//   - 默认 → pause + mark READY
func FinalizeMember(ctx context.Context, ag *agent.TeamAgent) error {
	member := ag.TeamMemberHandle()
	memberName := ag.MemberName()

	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debug(mgrLogComponent).
					Str("member_name", memberName).
					Any("recover", r).
					Msg("FinalizeMember: 读取状态异常")
			}
		}()

		var currentStatus atschema.MemberStatus
		var hasStatus bool
		if member != nil {
			status, err := member.Status(ctx)
			if err == nil {
				currentStatus = status
				hasStatus = true
			}
		}

		if hasStatus && memberFinalizedStatuses[currentStatus] {
			logger.Info(mgrLogComponent).
				Str("member_name", memberName).
				Str("status", string(currentStatus)).
				Msg("finalize_member: 已终结，仅关闭 kernel")
			_ = ag.StopCoordination(ctx)
			return
		}

		if hasStatus && currentStatus == atschema.MemberStatusShutdownRequested {
			logger.Info(mgrLogComponent).
				Str("member_name", memberName).
				Msg("finalize_member: 按请求关闭")
			_ = ag.StopCoordination(ctx)
			if member != nil {
				_, _ = member.UpdateStatus(ctx, atschema.MemberStatusShutdown)
			}
			return
		}

		logger.Info(mgrLogComponent).
			Str("member_name", memberName).
			Msg("finalize_member: 暂停并标记 READY")
		_ = ag.PauseCoordination(ctx)
		if member != nil {
			_, _ = member.UpdateStatus(ctx, atschema.MemberStatusReady)
		}
	}()

	return nil
}
```

- [ ] **Step 4: 添加 GetMonitor 和 ListActiveTeams**

```go
// GetMonitor 获取活跃团队的 TeamMonitor。
// Python: TeamRuntimeManager.get_monitor(team_name, session_id, hide_dm)
func (m *TeamRuntimeManager) GetMonitor(
	ctx context.Context,
	teamName string,
	sessionID string,
	hideDM bool,
) (*monitor.TeamMonitor, error) {
	entry := m.resolveEntry(teamName, sessionID)
	if entry == nil {
		return nil, nil
	}
	backend := getTeamBackend(entry.Agent)
	if backend == nil {
		return nil, nil
	}
	return monitor.CreateMonitor(entry.Agent, backend.DB(), teamName, sessionID, hideDM)
}

// ListActiveTeams 列出所有活跃团队。
// Python: TeamRuntimeManager.list_active_teams()
func (m *TeamRuntimeManager) ListActiveTeams() []ActiveTeamInfo {
	return m.pool.ListAllInfo()
}
```

需新增 import: `"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"`

- [ ] **Step 5: 添加内部辅助方法**

在非导出函数区块添加：

```go
// inspectSession 检查 DB 和 session 中的团队状态。
// Python: TeamRuntimeManager._inspect_session(spec, team_session, team_name)
func (m *TeamRuntimeManager) inspectSession(
	ctx context.Context,
	spec *atschema.TeamAgentSpec,
	teamSession *session.AgentTeamSession,
	teamName string,
) (teamInSession bool, teamInDB bool, teamDBState string, err error) {
	cp := getCheckpointer()
	sessionExists := false
	if cp != nil {
		sessionExists, _ = cp.SessionExists(ctx, teamSession.GetSessionID())
	}
	if preErr := teamSession.PreRun(ctx); preErr != nil {
		logger.Warn(mgrLogComponent).Err(preErr).
			Str("session_id", teamSession.GetSessionID()).
			Msg("inspectSession: PreRun 失败")
	}

	if !sessionExists {
		teamInSession = false
		teamDBState = ""
	} else {
		bucket := metadata.ReadTeamNamespace(teamSession, teamName)
		teamInSession = bucket != nil
		teamDBState = metadata.ReadTeamDBState(teamSession, teamName)
	}

	db := spawn.GetSharedDB(spec.ResolveDBConfig())
	if db != nil {
		if initErr := db.Initialize(ctx); initErr != nil {
			logger.Warn(mgrLogComponent).Err(initErr).
				Str("team_name", teamName).
				Msg("inspectSession: 数据库初始化失败")
		} else {
			teamInDB = db.Team().TeamExists(ctx, teamName)
		}
	}

	return teamInSession, teamInDB, teamDBState, nil
}

// applyAction 执行调度决策的副作用。
// Python: TeamRuntimeManager._apply_action(action, spec, team_session, pool_entry, inputs)
func (m *TeamRuntimeManager) applyAction(
	ctx context.Context,
	action RunAction,
	spec *atschema.TeamAgentSpec,
	teamSession *session.AgentTeamSession,
	poolEntry *ActiveTeam,
	inputs any,
) (*TeamRuntimeActivation, error) {
	teamName := spec.TeamName
	sessionID := teamSession.GetSessionID()
	kind := action.Kind

	// 拒绝类：不执行副作用
	if IsTeamRejectKind(kind) {
		var ag *agent.TeamAgent
		if poolEntry != nil {
			ag = poolEntry.Agent
		}
		logger.Warn(mgrLogComponent).
			Str("team_name", teamName).
			Str("session_id", sessionID).
			Str("reason", action.Reason).
			Str("kind", string(kind)).
			Msg("run_agent_team rejected")
		return &TeamRuntimeActivation{Agent: ag, Session: teamSession, Action: action}, nil
	}

	// 恢复暂停
	if kind == RunActionKindResumeFromPause {
		if poolEntry == nil {
			return nil, fmt.Errorf("%s requires an active pool entry", kind)
		}
		if err := preRunWithInputs(ctx, teamSession, inputs); err != nil {
			return nil, err
		}
		poolEntry.State = RuntimeStateRunning
		poolEntry.InteractGate.Reset()
		return &TeamRuntimeActivation{Agent: poolEntry.Agent, Session: teamSession, Action: action}, nil
	}

	// 冷路径（无 pool 条目）
	var ag *agent.TeamAgent
	var buildErr error

	switch kind {
	case RunActionKindColdRecover:
		ag, buildErr = agent.RecoverFromSession(ctx, teamSession, teamName, spec)
		if buildErr != nil {
			return nil, buildErr
		}
		if _, recErr := ag.RecoverTeam(ctx); recErr != nil {
			return nil, recErr
		}

	case RunActionKindNewTeamInSession:
		if err := preRunWithInputs(ctx, teamSession, inputs); err != nil {
			return nil, err
		}
		ag, buildErr = spec.Build()
		if buildErr != nil {
			return nil, buildErr
		}
		if _, resumeErr := ag.ResumeForNewSession(ctx, teamSession); resumeErr != nil {
			return nil, resumeErr
		}
		if _, recErr := ag.RecoverTeam(ctx); recErr != nil {
			return nil, recErr
		}
		if flushErr := flushTeamManifest(ctx, ag, teamSession); flushErr != nil {
			return nil, flushErr
		}

	case RunActionKindCreate:
		if err := preRunWithInputs(ctx, teamSession, inputs); err != nil {
			return nil, err
		}
		ag, buildErr = spec.Build()
		if buildErr != nil {
			return nil, buildErr
		}
		if flushErr := flushTeamManifest(ctx, ag, teamSession); flushErr != nil {
			return nil, flushErr
		}

	default:
		return nil, fmt.Errorf("unhandled RunActionKind: %s", kind)
	}

	// 加入池
	m.pool.Add(&ActiveTeam{
		TeamName:    teamName,
		Agent:       ag,
		SessionID:   sessionID,
		State:       RuntimeStateRunning,
		InteractGate: NewInteractGate(),
	})

	return &TeamRuntimeActivation{Agent: ag, Session: teamSession, Action: action}, nil
}
```

以及包级辅助函数：

```go
// buildTeamSession 从 spec+session 参数创建 AgentTeamSession。
// Python: TeamRuntimeManager._build_session(spec, session)
func buildTeamSession(spec *atschema.TeamAgentSpec, sess any) *session.AgentTeamSession {
	switch s := sess.(type) {
	case *session.AgentTeamSession:
		return s
	case string:
		return session.NewAgentTeamSession(session.WithAgentTeamSessionID(s))
	default:
		return session.NewAgentTeamSession()
	}
}

// preRunWithInputs 调用 session.pre_run(inputs)。
// Python: TeamRuntimeManager._pre_run_with_inputs(session, inputs)
func preRunWithInputs(ctx context.Context, sess *session.AgentTeamSession, inputs any) error {
	inputsDict, _ := inputs.(map[string]any)
	return sess.PreRun(ctx, inputsDict)
}

// flushTeamManifest 持久化团队清单 + 刷检查点。
// Python: TeamRuntimeManager._flush_team_manifest(agent, session)
func flushTeamManifest(ctx context.Context, ag *agent.TeamAgent, sess *session.AgentTeamSession) error {
	ag.PersistSessionManifest(sess)
	return sess.FlushCheckpoint(ctx)
}
```

需新增 import:
- `"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"`
- `"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"`
- `atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"`

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/runtime/...`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/agent_teams/runtime/manager.go internal/agent_teams/runtime/dispatch.go internal/agent_teams/runtime/dispatch_test.go
git commit -m "feat(9.85): runtime/manager — Activate 完整实现 + FinalizeMember + GetMonitor + ListActiveTeams"
```

---

### Task 3: runner/team_runner.go — 门面层

**Files:**
- Create: `internal/agentcore/runner/team_runner.go`
- Test: `internal/agentcore/runner/team_runner_test.go`
- Modify: `internal/agentcore/runner/doc.go`

- [ ] **Step 1: 编写 team_runner.go**

文件结构（对齐项目 Go 编码规范）：

1. 结构体区块：无新结构体
2. 枚举区块：无
3. 常量区块：无
4. 全局变量区块：无
5. 导出函数区块（15 个）：
   - `RunAgentTeam` — 统一入口，路由 base/member/TeamAgent 三条路径
   - `RunAgentTeamStreaming` — 流式版本
   - `InteractAgentTeam` — 交互
   - `RegisterHumanAgentInbound` — HITT 注册
   - `GetAgentTeamMonitor` — 获取 Monitor
   - `ListActiveTeams` — 列出活跃团队
   - `(r *Runner) runAgentTeam` — TeamAgent 路径
   - `(r *Runner) runAgentTeamStreaming` — TeamAgent 流式
   - `(r *Runner) runBaseTeam` — BaseTeam 路径
   - `(r *Runner) runBaseTeamStreaming` — BaseTeam 流式
   - `(r *Runner) runTeamMember` — Member 路径
   - `(r *Runner) runTeamMemberStreaming` — Member 流式
6. 非导出函数区块（8 个）：
   - `resolveTeamAgentSpec`
   - `resolveSpecFromSessionBucket`
   - `prepareBaseTeam`
   - `createAgentTeamSessionFromRef`
   - `closeTeamInteractGate`
   - `buildTeamRuntimeReadyChunk`
   - `buildTeamControlChunk`
   - `bindInteractTeamSession`

核心逻辑对齐 Python team_runner.py：

**RunAgentTeam 路由逻辑：**
```go
func RunAgentTeam(ctx, agentTeam, inputs, member, base, session, modelCtx, envs) {
    r := getRunner()
    if base {
        return r.runBaseTeam(ctx, agentTeam, inputs, session, modelCtx, envs)
    }
    if member {
        return r.runTeamMember(ctx, agentTeam, inputs, session)
    }
    return r.runAgentTeam(ctx, agentTeam, inputs, session)
}
```

**runAgentTeam 逻辑（对齐 Python `run_agent_team`）：**
```
1. spec = resolveTeamAgentSpec(ctx, agentTeam, session)
2. activation = GetTeamRuntimeManager().Activate(ctx, spec, session, inputs)
3. if IsTeamRejectKind(activation.Action.Kind): log warn, return nil
4. result = activation.Agent.Invoke(ctx, inputs, interfaces.WithSession(activation.Session))
5. finally:
   a. GetTeamRuntimeManager().Finalize(ctx, spec.TeamName, activation.Session.GetSessionID())
   b. closeTeamInteractGate(spec.TeamName, activation.Session.GetSessionID())
   c. activation.Session.PostRun(ctx)
```

**runAgentTeamStreaming 逻辑（对齐 Python `run_agent_team_streaming`）：**
```
1. spec = resolveTeamAgentSpec(ctx, agentTeam, session)
2. activation = GetTeamRuntimeManager().Activate(ctx, spec, session, inputs)
3. if IsTeamRejectKind(activation.Action.Kind): log warn, return empty channel
4. readyChunk = buildTeamRuntimeReadyChunk(...)
5. yield readyChunk
6. yield from activation.Agent.Stream(ctx, inputs, interfaces.WithSession(activation.Session))
7. finally:
   a. if streamLogger != nil: streamLogger.Flush()
   b. Finalize + closeTeamInteractGate + PostRun
```

**runBaseTeam 逻辑（对齐 Python `_run_base_team`）：**
```
1. teamInstance = prepareBaseTeam(ctx, baseTeam)
2. teamSession = createAgentTeamSessionFromRef(baseTeam, session)
3. teamSession.PreRun(ctx, inputs)
4. teamRuntime = teamInstance.Runtime() (可能 nil)
5. if teamRuntime != nil: teamRuntime.BindTeamSession(teamSession)
6. result = teamInstance.Invoke(ctx, inputs, maschema.WithTeamSession(teamSession))
7. finally:
   a. if teamRuntime != nil: teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
   b. teamSession.PostRun(ctx)
```

**runTeamMember 逻辑（对齐 Python `_run_team_member`）：**
```
1. teamSession = createAgentTeamSessionFromRef(agent, session)
2. teamSession.PreRun(ctx, inputs)
3. teamRuntime = agent.Runtime() (可能 nil)
4. if teamRuntime != nil: teamRuntime.BindTeamSession(teamSession)
5. result = agent.Invoke(ctx, inputs, interfaces.WithSession(teamSession))
6. finally:
   a. FinalizeMember(ctx, agent)
   b. if teamRuntime != nil: teamRuntime.UnbindTeamSession(teamSession.GetSessionID())
   c. teamSession.PostRun(ctx)
```

**InteractAgentTeam 逻辑（对齐 Python `interact_agent_team`）：**
```
1. if teamName == "" || sessionID == "": return DeliverResult.failure("missing_target")
2. ctx = bindInteractTeamSession(ctx, sessionID)
3. return GetTeamRuntimeManager().Interact(ctx, payload, teamName, sessionID)
```

**resolveTeamAgentSpec 逻辑（对齐 Python `_resolve_team_agent_spec`）：**
```
1. if spec, ok := agentTeam.(*atschema.TeamAgentSpec): return spec
2. if name, ok := agentTeam.(string):
   a. poolEntry = pool.Get(name)
   b. if poolEntry != nil && poolEntry.Agent.Spec() != nil: return poolEntry.Agent.Spec()
   c. if session != nil: return resolveSpecFromSessionBucket(ctx, name, session)
   d. raise AGENT_TEAM_CONFIG_INVALID
3. raise AGENT_TEAM_CONFIG_INVALID
```

**resolveSpecFromSessionBucket 逻辑（对齐 Python `_resolve_spec_from_session_bucket`）：**
```
1. teamSession = createAgentTeamSessionFromRef(nil, session)
2. teamSession.PreRun(ctx) — 失败时 log warn 并返回 nil
3. bucket = metadata.ReadTeamNamespace(teamSession, teamName)
4. if bucket == nil: return nil
5. specData = bucket["spec"]
6. if specData == nil: return nil
7. return atschema.NewTeamAgentSpecFromDict(specData) — 失败时 log warn 并返回 nil
```

**createAgentTeamSessionFromRef 逻辑（对齐 Python `_create_agent_team_session`）：**
```
switch s := session.(type):
case *session.AgentTeamSession: return s
case *session.Session: return session.CreateAgentTeamSession(s.GetSessionID(), s.GetEnvs(), "")
case string: return session.NewAgentTeamSession(session.WithAgentTeamSessionID(s))
default: return session.NewAgentTeamSession()
```

**closeTeamInteractGate 逻辑（对齐 Python `_close_team_interact_gate`）：**
```
1. entry = pool.Get(teamName)
2. if entry == nil || entry.SessionID != sessionID: return
3. entry.InteractGate.CloseAndDrain()
```

**buildTeamRuntimeReadyChunk 逻辑（对齐 Python `_build_team_runtime_ready_chunk`）：**
```go
func buildTeamRuntimeReadyChunk(teamName, sessionID string, actionKind runtime.RunActionKind,
    leaderMemberName string, leaderRole atschema.TeamRole) *atschema.TeamOutputSchema {
    return atschema.NewTeamOutputSchema(
        &stream.OutputSchema{Type: "message", Index: 0},
        strPtr(leaderMemberName),
        strPtr(leaderRole),
        map[string]any{
            "event_type":     "team.runtime_ready",
            "team_name":      teamName,
            "session_id":     sessionID,
            "activation_kind": string(actionKind),
        },
    )
}
```

注意：`atschema.NewTeamOutputSchema` 的签名需要确认，可能需要调整 payload 传递方式。查看 `internal/agent_teams/schema/stream.go` 中 `NewTeamOutputSchema` 的实际签名并适配。

**bindInteractTeamSession 逻辑（对齐 Python `_bind_interact_team_session`）：**
```go
func bindInteractTeamSession(ctx context.Context, sessionID string) context.Context {
    if sessionID == "" {
        return ctx
    }
    state := sessionctx.InitSessionState()
    state.SetSessionID(sessionID)
    return sessionctx.WithSessionState(ctx, state)
}
```

- [ ] **Step 2: 编写 team_runner_test.go**

使用 mock TeamRuntimeManager 和 fake TeamAgent 测试：
- `TestRunAgentTeam_TeamAgentPath` — 验证 Activate→Invoke→Finalize 调用链
- `TestRunAgentTeam_RejectedPath` — 验证拒绝类返回 nil
- `TestRunAgentTeam_BaseTeamPath` — 验证 BaseTeam 路径
- `TestRunAgentTeam_MemberPath` — 验证 Member 路径 + FinalizeMember
- `TestInteractAgentTeam_MissingTarget` — 验证 missing_target
- `TestResolveTeamAgentSpec_BySpec` — 直接传 TeamAgentSpec
- `TestResolveTeamAgentSpec_ByName` — 从 pool 或 session bucket 解析
- `TestBuildTeamRuntimeReadyChunk` — chunk 构造

- [ ] **Step 3: 更新 doc.go**

在 `internal/agentcore/runner/doc.go` 文件目录中添加：
```
//	├── team_runner.go     # 团队运行入口（RunAgentTeam/Streaming/BaseTeam/Member/Interact/Monitor/ListActive）
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/runner/... -v -run "TestRunAgentTeam|TestInteractAgent|TestResolveTeam|TestBuildTeam" -count=1`
Expected: PASS

- [ ] **Step 5: 编译检查全项目**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/runner/team_runner.go internal/agentcore/runner/team_runner_test.go internal/agentcore/runner/doc.go
git commit -m "feat(9.85): runner/team_runner — 团队执行入口（TeamAgent/BaseTeam/Member 三路径）"
```

---

### Task 4: 回填 14 处 ⤵️ 标记

**Files:**
- Modify: `internal/agent_teams/spawn/inprocess_spawn.go` (2处)
- Modify: `internal/agent_teams/spawn/shared_resources.go` (3处)
- Modify: `internal/agent_teams/spawn/shared_resources_test.go` (1处)
- Modify: `internal/agent_teams/runtime/manager.go` (1处)
- Modify: `internal/agentcore/runner/spawn/child.go` (1处)
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go` (7处)
- Modify: `internal/swarm/agents/harness/team/team_manager_monitor.go` (2处)

- [ ] **Step 1: 回填 spawn/inprocess_spawn.go**

搜索 `⤵️ 预留：TeamRunner` 和 `TODO(#9.85)` 注释，替换为调用 `runner.RunAgentTeam()` 或 `runner.RunAgentTeamStreaming()`。

- [ ] **Step 2: 回填 spawn/shared_resources.go + shared_resources_test.go**

搜索 `⤵️ 预留：TeamRuntime（9.85）` 和 `TODO(#9.85)` 注释，替换为调用 `runtime.GetTeamRuntimeManager()`。

- [ ] **Step 3: 回填 runtime/manager.go onInbound nil**

搜索 `TODO(#9.85): 注入 onInbound` 注释，替换为从 `runner.RegisterHumanAgentInbound` 回填的回调注入逻辑。

- [ ] **Step 4: 回填 runner/spawn/child.go**

搜索 `team_agent 模式尚未实现：⤵️ 预留 TeamRunner` 注释，替换为调用 `runner.RunAgentTeam()`。

- [ ] **Step 5: 回填 deep_adapter_team.go (7处)**

逐一搜索 `⤵️(#9.85)` 注释：
1. TeamAgent 创建 → 调用 `runner.RunAgentTeamStreaming()`
2. consumeStreamWithQuery → 调用流式接口
3. ensureMonitorForActiveRuntime → 调用 `runner.GetAgentTeamMonitor()`
4. watchTeamEvolutionAndPushTeam → 调用 `runner.RunAgentTeamStreaming()` + 事件推送
5. ensureTeamEvolutionWatcher → 启动 watcher goroutine
6. 两处日志占位 → 替换为实际调用

- [ ] **Step 6: 回填 team_manager_monitor.go (2处)**

搜索 `TODO(#9.85)`：
1. pending_waiters → 调用 `runner.ListActiveTeams()` + channel 广播
2. server_push → 调用推送机制

- [ ] **Step 7: 运行全项目编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 8: 提交**

```bash
git add -A
git commit -m "feat(9.85): 回填 14 处 ⤵️ 标记（spawn/shared_resources/deep_adapter/monitor）"
```

---

### Task 5: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.85 状态**

将 `| 9.85 | ☐ | TeamRunner |` 改为 `| 9.85 | ✅ | TeamRunner | 团队 Runner（run_agent_team/streaming 三路径 + Activate 内联回填 #9.62 + FinalizeMember + GetMonitor + ListActiveTeams + 14 处 ⤵️ 回填） |`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 9.85 TeamRunner 标记完成"
```
