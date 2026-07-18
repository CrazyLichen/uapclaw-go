# 9.59 SessionManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TeamAgent 会话管理器（SessionManager），提供会话三态生命周期管理（Unbound/Half-bound/Fully-bound）和 session_id 的 context 传播。

**Architecture:** 复用 CwdState 模式——SessionState 可变容器 + context.Value 传播指针。SessionManager 持有 SessionState 实例，通过 BindSession 按需注入 ctx，ReleaseSession 原地清空字段。替换现有 agent_teams/context.go 中的简单字符串 Value 实现。

**Tech Stack:** Go 1.23, sync.RWMutex, context.WithValue, testify/assert

**Design Spec:** `docs/superpowers/specs/2026-11-25-session-manager-9.59-design.md`

---

## File Structure

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/agent_teams/context.go` | SessionState 可变容器 + context 注入/读取 |
| 新建 | `internal/agent_teams/context_test.go` | SessionState + context 传播测试 |
| 新建 | `internal/agent_teams/agent/session_manager.go` | SessionManager 核心实现 |
| 新建 | `internal/agent_teams/agent/session_manager_test.go` | SessionManager 测试 |
| 修改 | `internal/agent_teams/agent/team_agent.go` | 回填 6 处 TODO(#9.59) |
| 修改 | `internal/agent_teams/agent/state.go` | 更新 TeamSession 字段注释 |
| 修改 | `internal/agent_teams/agent/doc.go` | 添加 session_manager.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 更新 9.59 状态 + 延后项说明 |

---

### Task 1: 替换 context.go — 实现 SessionState 可变容器

**Files:**
- Modify: `internal/agent_teams/context.go`
- Create: `internal/agent_teams/context_test.go`

- [ ] **Step 1: 写 SessionState 的失败测试**

创建 `internal/agent_teams/context_test.go`：

```go
package agent_teams

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInitSessionState_基本初始化 测试创建 SessionState 实例
func TestInitSessionState_基本初始化(t *testing.T) {
	state := InitSessionState()
	assert.NotNil(t, state)
	assert.Equal(t, "", state.GetSessionID())
}

// TestSessionState_SetSessionID 测试设置 sessionID 后立即可见
func TestSessionState_SetSessionID(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-123")
	assert.Equal(t, "sess-123", state.GetSessionID())
}

// TestSessionState_并发安全 测试并发读写不 panic
func TestSessionState_并发安全(t *testing.T) {
	state := InitSessionState()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = state.GetSessionID()
		}()
		go func() {
			defer wg.Done()
			state.SetSessionID("concurrent-id")
		}()
	}
	wg.Wait()
}

// TestWithSessionState_上下文传播 测试注入 ctx 后可取出
func TestWithSessionState_上下文传播(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-456")
	ctx := WithSessionState(context.Background(), state)
	got := SessionStateFromCtx(ctx)
	assert.Equal(t, state, got)
}

// TestWithSessionState_上下文无SessionState 测试未注入时返回 nil
func TestWithSessionState_上下文无SessionState(t *testing.T) {
	got := SessionStateFromCtx(context.Background())
	assert.Nil(t, got)
}

// TestGetSessionID_从上下文获取 测试全局函数读取
func TestGetSessionID_从上下文获取(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-789")
	ctx := WithSessionState(context.Background(), state)
	assert.Equal(t, "sess-789", GetSessionID(ctx))
}

// TestGetSessionID_上下文无SessionState回退 测试 nil 时返回空串
func TestGetSessionID_上下文无SessionState回退(t *testing.T) {
	assert.Equal(t, "", GetSessionID(context.Background()))
}

// Test子Agent隔离 测试子 Agent 创建新 SessionState 不影响父
func Test子Agent隔离(t *testing.T) {
	parentState := InitSessionState()
	parentState.SetSessionID("parent-sess")
	parentCtx := WithSessionState(context.Background(), parentState)

	// 子 Agent 创建独立 SessionState
	subState := InitSessionState()
	subState.SetSessionID("sub-sess")
	subCtx := WithSessionState(parentCtx, subState)

	// 子 Agent 修改不影响父
	assert.Equal(t, "parent-sess", GetSessionID(parentCtx))
	assert.Equal(t, "sub-sess", GetSessionID(subCtx))
}

// Test父改子可见 测试同一指针修改后同 ctx 可见
func Test父改子可见(t *testing.T) {
	state := InitSessionState()
	ctx := WithSessionState(context.Background(), state)

	state.SetSessionID("first")
	assert.Equal(t, "first", GetSessionID(ctx))

	state.SetSessionID("second")
	assert.Equal(t, "second", GetSessionID(ctx))
}

// TestSessionState_SetSessionID_清空 测试清空 sessionID
func TestSessionState_SetSessionID_清空(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-123")
	state.SetSessionID("")
	assert.Equal(t, "", state.GetSessionID())
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/ -run "TestInitSessionState|TestSessionState|TestWithSessionState|TestGetSessionID|Test子Agent隔离|Test父改子可见" -v
```

Expected: 编译失败 — `InitSessionState` 等函数不存在

- [ ] **Step 3: 替换 context.go 实现**

替换 `internal/agent_teams/context.go` 内容为：

```go
package agent_teams

import (
	"context"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionState 每-TeamAgent 的可变 session 状态容器。
// 对齐 Python: _session_id_context (contextvars.ContextVar)
//
// 通过 context.Value 传播 *SessionState 指针：
//   - 同一 TeamAgent 内的 goroutine 共享同一 SessionState 引用，SetSessionID 后立即可见
//   - 子 Teammate 调用 InitSessionState 创建新实例 + WithSessionState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
type SessionState struct {
	mu        sync.RWMutex
	sessionID string
}

// sessionStateKeyType SessionState 的 context key 类型。
type sessionStateKeyType struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSessionState 创建新的 SessionState 实例。
// 对齐 Python: _session_id_context = ContextVar("session_id", default=None)
func InitSessionState() *SessionState {
	return &SessionState{}
}

// WithSessionState 将 SessionState 注入 context。
// 对齐 Python: set_session_id(session_id) — 但 Go 通过 context.Value 传播指针
func WithSessionState(ctx context.Context, state *SessionState) context.Context {
	return context.WithValue(ctx, sessionStateKeyType{}, state)
}

// SessionStateFromCtx 从 context 中获取 SessionState。
// 返回 nil 表示当前 context 未绑定 SessionState。
func SessionStateFromCtx(ctx context.Context) *SessionState {
	if s, ok := ctx.Value(sessionStateKeyType{}).(*SessionState); ok {
		return s
	}
	return nil
}

// GetSessionID 从 context 中获取当前 session_id。
// 对齐 Python: get_session_id() -> Optional[str]
// 读取优先级：SessionState.sessionID → ""（空字符串）
func GetSessionID(ctx context.Context) string {
	if s := SessionStateFromCtx(ctx); s != nil {
		return s.GetSessionID()
	}
	return ""
}

// GetSessionID 获取当前 session_id。
func (s *SessionState) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

// SetSessionID 设置当前 session_id。
// 对齐 Python: set_session_id(session_id) -> Token
// Go 不需要 Token，直接原地修改，同一指针的 goroutine 立即可见。
func (s *SessionState) SetSessionID(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = sessionID
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/ -run "TestInitSessionState|TestSessionState|TestWithSessionState|TestGetSessionID|Test子Agent隔离|Test父改子可见" -v
```

Expected: 全部 PASS

- [ ] **Step 5: 确认无其他调用方受影响**

```bash
cd /home/opensource/uapclaw-gateway && grep -rn "agentteams\.SetSessionID\|agentteams\.GetSessionID" internal/ --include="*.go"
```

Expected: 无输出（旧的 `SetSessionID` 函数无调用方）

- [ ] **Step 6: 提交**

```bash
git add internal/agent_teams/context.go internal/agent_teams/context_test.go
git commit -m "feat(#9.59): 实现 SessionState 可变容器，替换原 context.go 简单字符串 Value"
```

---

### Task 2: 实现 SessionManager

**Files:**
- Create: `internal/agent_teams/agent/session_manager.go`
- Create: `internal/agent_teams/agent/session_manager_test.go`

- [ ] **Step 1: 写 SessionManager 的失败测试**

创建 `internal/agent_teams/agent/session_manager_test.go`：

```go
package agent

import (
	"context"
	"testing"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/stretchr/testify/assert"
)

// mockSession 用于测试的 mock session 对象
type mockSession struct {
	sessionID string
}

// GetSessionID 返回 mock session ID
func (m *mockSession) GetSessionID() string {
	return m.sessionID
}

// TestNewSessionManager_基本构造 测试构造函数
func TestNewSessionManager_基本构造(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)
	assert.NotNil(t, m)
	assert.NotNil(t, m.sessionState)
}

// TestSessionManager_TeamSession 测试 TeamSession getter/setter
func TestSessionManager_TeamSession(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	assert.Nil(t, m.TeamSession())
	sess := &mockSession{sessionID: "sess-1"}
	m.SetTeamSession(sess)
	assert.Equal(t, sess, m.TeamSession())
}

// TestBindSession_基本绑定 测试 sessionID 设置 + ctx 注入 + teamSession 存储
func TestBindSession_基本绑定(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "sess-123"}
	ctx, err := m.BindSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "sess-123", m.sessionState.GetSessionID())
	assert.Equal(t, sess, state.TeamSession)
	// 返回的 ctx 应包含 SessionState
	assert.Equal(t, "sess-123", agentteams.GetSessionID(ctx))
}

// TestBindSession_返回新Ctx 测试返回的 ctx 含 SessionState
func TestBindSession_返回新Ctx(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "new-ctx"}
	newCtx, err := m.BindSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.NotNil(t, agentteams.SessionStateFromCtx(newCtx))
	assert.Equal(t, "new-ctx", agentteams.GetSessionID(newCtx))

	// 原 ctx 不含 SessionState
	assert.Nil(t, agentteams.SessionStateFromCtx(context.Background()))
}

// TestBindSession_重复绑定 测试两次 bind 覆盖前一次
func TestBindSession_重复绑定(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess1 := &mockSession{sessionID: "sess-1"}
	ctx1, err := m.BindSession(context.Background(), sess1)
	assert.NoError(t, err)

	sess2 := &mockSession{sessionID: "sess-2"}
	ctx2, err := m.BindSession(ctx1, sess2)
	assert.NoError(t, err)

	assert.Equal(t, "sess-2", m.sessionState.GetSessionID())
	assert.Equal(t, sess2, state.TeamSession)
	assert.Equal(t, "sess-2", agentteams.GetSessionID(ctx2))
}

// TestReleaseSession_释放 测试 sessionID 清空 + teamSession 置 nil
func TestReleaseSession_释放(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "sess-1"}
	_, _ = m.BindSession(context.Background(), sess)

	m.ReleaseSession()

	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)
}

// TestReleaseSession_幂等 测试多次 release 不 panic
func TestReleaseSession_幂等(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	m.ReleaseSession() // 未 bind 就 release
	m.ReleaseSession() // 再次 release
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)
}

// TestSessionManager_三态转换 测试 Unbound → Fully-bound → Half-bound → Unbound
func TestSessionManager_三态转换(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	// Unbound: sessionID="" teamSession=nil
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)

	// → Fully-bound
	sess := &mockSession{sessionID: "sess-1"}
	_, _ = m.BindSession(context.Background(), sess)
	assert.Equal(t, "sess-1", m.sessionState.GetSessionID())
	assert.Equal(t, sess, state.TeamSession)

	// → Half-bound (ReleaseSession 清空 teamSession 但 sessionID 也清空)
	m.ReleaseSession()
	assert.Equal(t, "", m.sessionState.GetSessionID())
	assert.Nil(t, state.TeamSession)

	// → Fully-bound again
	sess2 := &mockSession{sessionID: "sess-2"}
	_, _ = m.BindSession(context.Background(), sess2)
	assert.Equal(t, "sess-2", m.sessionState.GetSessionID())
	assert.Equal(t, sess2, state.TeamSession)
}

// TestResumeForNewSession_基本流程 测试返回新 ctx（TODO #9.61 回填内部逻辑）
func TestResumeForNewSession_基本流程(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "resume-sess"}
	newCtx, err := m.ResumeForNewSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "resume-sess", m.sessionState.GetSessionID())
	assert.Equal(t, "resume-sess", agentteams.GetSessionID(newCtx))
}

// TestRecoverForExistingSession_基本流程 测试返回新 ctx（TODO #9.61 回填内部逻辑）
func TestRecoverForExistingSession_基本流程(t *testing.T) {
	state := NewTeamAgentState()
	configurator := NewAgentConfigurator(nil)
	m := NewSessionManager(state, configurator, nil)

	sess := &mockSession{sessionID: "recover-sess"}
	newCtx, err := m.RecoverForExistingSession(context.Background(), sess)

	assert.NoError(t, err)
	assert.Equal(t, "recover-sess", m.sessionState.GetSessionID())
	assert.Equal(t, "recover-sess", agentteams.GetSessionID(newCtx))
}

// TestExtractSessionID_空session 测试 nil session 返回空串
func TestExtractSessionID_空session(t *testing.T) {
	assert.Equal(t, "", extractSessionID(nil))
}

// TestExtractSessionID_有GetSessionID 测试含 GetSessionID 方法的对象
func TestExtractSessionID_有GetSessionID(t *testing.T) {
	sess := &mockSession{sessionID: "extracted-123"}
	assert.Equal(t, "extracted-123", extractSessionID(sess))
}

// TestExtractSessionID_无GetSessionID 测试不含 GetSessionID 方法的对象
func TestExtractSessionID_无GetSessionID(t *testing.T) {
	assert.Equal(t, "", extractSessionID("not-a-session"))
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestNewSessionManager|TestSessionManager|TestBindSession|TestReleaseSession|TestResumeForNewSession|TestRecoverForExistingSession|TestExtractSessionID" -v
```

Expected: 编译失败 — `SessionManager` 等类型不存在

- [ ] **Step 3: 实现 SessionManager**

创建 `internal/agent_teams/agent/session_manager.go`：

```go
package agent

import (
	"context"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionManager 管理团队会话生命周期和持久化。
// 对齐 Python: SessionManager (openjiuwen/agent_teams/agent/session_manager.py)
//
// 三态模型：
//
//	Unbound     — sessionID="" teamSession=nil  从未绑定或显式解绑
//	Half-bound  — sessionID="X" teamSession=nil 会话释放但 ID 保留用于日志
//	Fully-bound — sessionID="X" teamSession=ses 活跃会话，可读写
//
// 禁止状态：sessionID="" + teamSession!=nil（悬空状态）
type SessionManager struct {
	// state 可变运行时状态
	state *TeamAgentState
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// recoveryManager 恢复管理器
	// TODO(#9.61): 替换 any 为 RecoveryManager 类型
	recoveryManager any
	// sessionState session_id 可变容器（通过 context 传播）
	sessionState *agentteams.SessionState
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMgrLogComponent 日志组件
	sessionMgrLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionManager 创建新的 SessionManager。
// 对齐 Python: SessionManager.__init__(state, configurator, recovery_manager)
func NewSessionManager(
	state *TeamAgentState,
	configurator *AgentConfigurator,
	recoveryManager any, // TODO(#9.61): RecoveryManager 类型
) *SessionManager {
	return &SessionManager{
		state:           state,
		configurator:    configurator,
		recoveryManager: recoveryManager,
		sessionState:    agentteams.InitSessionState(),
	}
}

// TeamSession 返回当前团队会话。
// 对齐 Python: SessionManager.team_session (property getter)
func (m *SessionManager) TeamSession() any {
	return m.state.TeamSession
}

// SetTeamSession 设置团队会话。
// 对齐 Python: SessionManager.team_session (property setter)
func (m *SessionManager) SetTeamSession(session any) {
	m.state.TeamSession = session
}

// BindSession 完整绑定到指定会话。
// 对齐 Python: SessionManager.bind_session(session)
//
// 操作：
//  1. 设置 sessionID 到 SessionState（立即可见）
//  2. 注入 SessionState 到 context（如尚未注入）
//  3. 存储 session 到 TeamAgentState.TeamSession
//  4. 如果有 teamBackend：创建会话 DB 表（幂等）  TODO(#9.61)
//  5. 如果是 Leader：持久化 leader 配置            TODO(#9.61)
//
// 返回新的 context.Context（含 SessionState），调用方必须用于后续传播。
func (m *SessionManager) BindSession(
	ctx context.Context,
	session any, // TODO(#9.59): AgentTeamSession 接口
) (context.Context, error) {
	sessionID := extractSessionID(session)

	// 设置 sessionID 到 SessionState（原地修改，共享指针立即可见）
	m.sessionState.SetSessionID(sessionID)

	// 注入 SessionState 到 context（如果尚未注入）
	newCtx := ctx
	if agentteams.SessionStateFromCtx(ctx) == nil {
		newCtx = agentteams.WithSessionState(ctx, m.sessionState)
	}

	// 存储 session 到 state
	m.state.TeamSession = session

	// TODO(#9.61): 如果有 teamBackend → teamBackend.DB().CreateCurSessionTables()
	// TODO(#9.61): 如果是 Leader + spec 存在 → recoveryManager.PersistLeaderConfig(session)

	logger.Info(sessionMgrLogComponent).
		Str("session_id", sessionID).
		Msg("SessionManager.BindSession")

	return newCtx, nil
}

// ReleaseSession 从当前会话解绑。
// 对齐 Python: SessionManager.release_session()
//
// 操作：
//  1. 清空 SessionState 的 sessionID
//  2. 置 TeamAgentState.TeamSession = nil
//
// 清空后 sessionID=""，teamSession=nil → Unbound 状态。
func (m *SessionManager) ReleaseSession() {
	m.sessionState.SetSessionID("")
	m.state.TeamSession = nil

	logger.Info(sessionMgrLogComponent).
		Msg("SessionManager.ReleaseSession")
}

// ResumeForNewSession 切换到新会话并重新绑定活着的 teammate 运行时。
// 对齐 Python: SessionManager.resume_for_new_session(session)
//
// 用于 NEW_TEAM_IN_SESSION 分发路径：
//  1. 收集活着的 teammate（快照）  TODO(#9.61)
//  2. 绑定新会话
//  3. Leader 侧重启 teammate（cleanup_first=true）  TODO(#9.61)
func (m *SessionManager) ResumeForNewSession(
	ctx context.Context,
	session any,
) (context.Context, error) {
	// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()
	newCtx, err := m.BindSession(ctx, session)
	if err != nil {
		return newCtx, err
	}

	// TODO(#9.61): if m.configurator.Role() != Leader || m.configurator.TeamBackend() == nil → return
	// TODO(#9.61): m.recoveryManager.RestartForSessionSwitch(recoverableMembers, true)

	return newCtx, nil
}

// RecoverForExistingSession 绑定到检查点恢复的会话（不清理）。
// 对齐 Python: SessionManager.recover_for_existing_session(session)
//
// 用于 COLD_RECOVER 分发路径：
// 调用方必须已经拆卸协调（清除了活跃句柄）并验证了检查点。
func (m *SessionManager) RecoverForExistingSession(
	ctx context.Context,
	session any,
) (context.Context, error) {
	// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()
	newCtx, err := m.BindSession(ctx, session)
	if err != nil {
		return newCtx, err
	}

	// TODO(#9.61): if m.configurator.Role() != Leader || m.configurator.TeamBackend() == nil → return
	// TODO(#9.61): m.recoveryManager.RestartForSessionSwitch(recoverableMembers, false)

	return newCtx, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractSessionID 从 session 对象中提取 session_id。
// 对齐 Python: session.get_session_id()
// TODO(#9.59): 定义 AgentTeamSession 接口后替换为接口方法调用
func extractSessionID(session any) string {
	if session == nil {
		return ""
	}
	// 尝试类型断言到含 GetSessionID() 方法的接口
	type sessionIDer interface {
		GetSessionID() string
	}
	if s, ok := session.(sessionIDer); ok {
		return s.GetSessionID()
	}
	return ""
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestNewSessionManager|TestSessionManager|TestBindSession|TestReleaseSession|TestResumeForNewSession|TestRecoverForExistingSession|TestExtractSessionID" -v
```

Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/session_manager.go internal/agent_teams/agent/session_manager_test.go
git commit -m "feat(#9.59): 实现 SessionManager 会话三态管理"
```

---

### Task 3: 回填 team_agent.go 和 state.go 中的 TODO(#9.59)

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go`
- Modify: `internal/agent_teams/agent/state.go`

- [ ] **Step 1: 修改 team_agent.go — sessionManager 字段类型**

将 `sessionManager any` 改为 `sessionManager *SessionManager`：

```go
// 旧代码（第 77 行附近）:
	// sessionManager 会话管理器
	// TODO(#9.59): SessionManager 类型
	sessionManager any

// 新代码:
	// sessionManager 会话管理器
	sessionManager *SessionManager
```

- [ ] **Step 2: 修改 team_agent.go — 构造函数中构建 SessionManager**

将 TODO 注释改为实际构造：

```go
// 旧代码（第 105 行附近）:
	// TODO(#9.59): 构建 SessionManager(state, configurator, recoveryManager)

// 新代码:
	a.sessionManager = NewSessionManager(a.state, a.configurator, a.recoveryManager)
```

- [ ] **Step 3: 修改 team_agent.go — SessionID 方法**

```go
// 旧代码（第 269 行附近）:
func (a *TeamAgent) SessionID(ctx context.Context) string {
	// TODO(#9.59): 从 agent_teams.GetSessionID(ctx) 读取
	return ""
}

// 新代码:
func (a *TeamAgent) SessionID(ctx context.Context) string {
	return agentteams.GetSessionID(ctx)
}
```

- [ ] **Step 4: 修改 team_agent.go — SessionManager 返回类型**

```go
// 旧代码（第 275 行附近）:
func (a *TeamAgent) SessionManager() any {
	return a.sessionManager
}

// 新代码:
func (a *TeamAgent) SessionManager() *SessionManager {
	return a.sessionManager
}
```

- [ ] **Step 5: 修改 team_agent.go — ResumeForNewSession 方法**

```go
// 旧代码（第 639 行附近）:
func (a *TeamAgent) ResumeForNewSession(ctx context.Context, session any) error {
	// TODO(#9.59): 会话管理器恢复新会话 sessionManager.resume_for_new_session(session)
	return nil
}

// 新代码:
func (a *TeamAgent) ResumeForNewSession(ctx context.Context, session any) (context.Context, error) {
	if a.sessionManager != nil {
		return a.sessionManager.ResumeForNewSession(ctx, session)
	}
	return ctx, nil
}
```

- [ ] **Step 6: 修改 team_agent.go — RecoverForExistingSession 方法**

```go
// 旧代码（第 646 行附近）:
func (a *TeamAgent) RecoverForExistingSession(ctx context.Context, session any) error {
	// TODO(#9.59+#9.62): 停止协调 → 会话管理器恢复已有会话 sessionManager.recover_for_existing_session(session)
	return nil
}

// 新代码:
func (a *TeamAgent) RecoverForExistingSession(ctx context.Context, session any) (context.Context, error) {
	if a.sessionManager != nil {
		return a.sessionManager.RecoverForExistingSession(ctx, session)
	}
	return ctx, nil
}
```

- [ ] **Step 7: 修改 state.go — 更新 TeamSession 注释**

```go
// 旧代码（第 15 行附近）:
	// TeamSession 团队会话
	// TODO(#9.59): AgentTeamSession 类型
	TeamSession any

// 新代码:
	// TeamSession 团队会话
	// TODO: 定义 AgentTeamSession 接口后替换 any
	TeamSession any
```

- [ ] **Step 8: 运行全部测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -v
```

Expected: 全部 PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agent_teams/agent/team_agent.go internal/agent_teams/agent/state.go
git commit -m "feat(#9.59): 回填 TeamAgent 中 6 处 TODO(#9.59)，接入 SessionManager"
```

---

### Task 4: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agent_teams/agent/doc.go`
- Modify: `internal/agent_teams/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 agent/doc.go — 添加 session_manager.go 条目**

在文件目录树中，将 `session_manager.go` 的 `TODO(#9.59)` 标记更新：

```go
// 旧代码:
//	├── session_manager.go    # TODO(#9.59) 会话管理器

// 新代码:
//	├── session_manager.go    # SessionManager 会话三态管理（9.59）
```

- [ ] **Step 2: 更新 agent_teams/doc.go — 更新 interaction/ 说明**

```go
// 旧代码:
//	├── interaction/        # ⤵️ 回填: 9.59 团队交互

// 新代码:
//	├── interaction/        # ⤵️ 回填: 后续独立章节（payload/router/UserInbox/HumanAgentInbox）
```

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

更新 9.59 行：

```markdown
<!-- 旧代码 -->
| 9.59 | ☐ | SessionManager | 团队会话管理 | `openjiuwen/agent_teams/interaction/` |

<!-- 新代码 -->
| 9.59 | ✅ | SessionManager | 会话三态管理（SessionState+SessionManager+6处回填）；⤵️ Interaction 层延后 | `openjiuwen/agent_teams/agent/session_manager.py` · `agent_teams/context.py` |
```

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/agent/doc.go internal/agent_teams/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(#9.59): 更新 doc.go 和 IMPLEMENTATION_PLAN.md，标记 9.59 完成"
```

---

### Task 5: 全量编译验证

- [ ] **Step 1: 运行全量编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

Expected: 编译成功，无错误

- [ ] **Step 2: 运行全量测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -cover
```

Expected: 全部 PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 最终提交（如有修正）**

```bash
git add -A && git commit -m "fix(#9.59): 全量编译验证修正"
```
