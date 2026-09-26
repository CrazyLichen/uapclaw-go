# 9.61a + 9.61 — Metadata Namespace 函数 + RecoveryManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Team Namespace 持久化函数（9.61a）和 RecoveryManager 恢复管理器（9.61），回填所有 `TODO(#9.61)` 占位。

**Architecture:** 9.61a 在 `agent_teams/runtime/metadata.go` 中实现 `ReadTeamsBucket` / `WriteTeamNamespace` / `MergeTeamNamespace` 等 8 个函数，通过 `SessionFacade.UpdateState` / `GetState` 操作 `state["teams"]` 下的分桶数据。9.61 在 `agent_teams/agent/recovery_manager.go` 中实现 `RecoveryManager` 结构体及 7 个方法，对齐 Python `recovery_manager.py`。同时为 `SpawnManager` 添加 `SpawnedHandles()` 导出访问器（当前为未导出字段），并回填 `session_manager.go` / `team_agent.go` 中的所有 `TODO(#9.61)`。

**Tech Stack:** Go 1.22+, testify/assert, SessionFacade 接口

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| **创建** | `internal/agent_teams/runtime/metadata.go` | Team Namespace 读写函数（8 个导出函数 + 5 个常量） |
| **创建** | `internal/agent_teams/runtime/metadata_test.go` | metadata 函数单元测试 |
| **创建** | `internal/agent_teams/agent/recovery_manager.go` | RecoveryManager 结构体 + 7 个方法 + LiveTeammate 辅助类型 |
| **创建** | `internal/agent_teams/agent/recovery_manager_test.go` | RecoveryManager 单元测试 |
| **修改** | `internal/agent_teams/runtime/doc.go` | 文件目录添加 metadata.go 条目 |
| **修改** | `internal/agent_teams/agent/doc.go` | 文件目录更新 recovery_manager.go 条目（⤵️→正式） |
| **修改** | `internal/agent_teams/agent/spawn_manager.go` | 添加 `SpawnedHandles()` 导出访问器 |
| **修改** | `internal/agent_teams/agent/session_manager.go` | 回填 6 处 `TODO(#9.61)` + 类型从 `any` 改为 `*RecoveryManager` |
| **修改** | `internal/agent_teams/agent/team_agent.go` | 回填 6 处 `TODO(#9.61)` + 类型从 `any` 改为 `*RecoveryManager` + RecoverFromSession 占位 |
| **修改** | `internal/agent_teams/agent/session_manager_test.go` | 移除 `TODO(#9.61)` 注释 + 适配新类型 |

---

### Task 1: 9.61a — metadata.go 常量和 ReadTeamsBucket

**Files:**
- Create: `internal/agent_teams/runtime/metadata.go`
- Test: `internal/agent_teams/runtime/metadata_test.go`

- [ ] **Step 1: 写 failing test — fakeSessionFacade + TestReadTeamsBucket_空命名空间**

```go
package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// fakeSessionFacade 用于测试的内存 SessionFacade 实现
type fakeSessionFacade struct {
	data map[string]any
}

func newFakeSessionFacade() *fakeSessionFacade {
	return &fakeSessionFacade{data: make(map[string]any)}
}

func (f *fakeSessionFacade) GetSessionID() string                                          { return "test-session" }
func (f *fakeSessionFacade) UpdateState(data map[string]any)                               { f.data = data }
func (f *fakeSessionFacade) GetState(key state.StateKey) (any, error)                      { return f.data[key.String()], nil }
func (f *fakeSessionFacade) DumpState() map[string]any                                     { return f.data }
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error                    { return nil }
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error              { return nil }
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any                                 { return nil }
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error                       { return nil }

// 编译时检查
var _ interfaces.SessionFacade = (*fakeSessionFacade)(nil)

func TestReadTeamsBucket_空命名空间(t *testing.T) {
	sess := newFakeSessionFacade()
	result := runtime.ReadTeamsBucket(sess)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -run TestReadTeamsBucket -v 2>&1 | tail -5`
Expected: 编译失败（`runtime.ReadTeamsBucket` 未定义）

- [ ] **Step 3: 实现 metadata.go — 常量 + ReadTeamsBucket**

```go
package runtime

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamsKey session state 中 teams 命名空间的顶层 key
	// Python: TEAMS_KEY = "teams"
	TeamsKey = "teams"
	// TeamDBStateKey 桶内 db_state 字段 key
	// Python: TEAM_DB_STATE_KEY = "db_state"
	TeamDBStateKey = "db_state"
	// TeamDBStatePendingCreate DB 表待创建
	// Python: TEAM_DB_STATE_PENDING_CREATE = "pending_create"
	TeamDBStatePendingCreate = "pending_create"
	// TeamDBStateCreated DB 表已创建
	// Python: TEAM_DB_STATE_CREATED = "created"
	TeamDBStateCreated = "created"
	// TeamDBStateCleaned DB 表已清理
	// Python: TEAM_DB_STATE_CLEANED = "cleaned"
	TeamDBStateCleaned = "cleaned"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadTeamsBucket 读取整个 teams 命名空间，空时返回空 map（不返回 nil）。
// 对齐 Python 返回 {} 而非 None。
// Python: read_teams_bucket(session) -> dict[str, dict[str, Any]]
func ReadTeamsBucket(sess interfaces.SessionFacade) map[string]map[string]any {
	teamsAny, _ := sess.GetState(state.StringKey(TeamsKey))
	if teamsAny == nil {
		return map[string]map[string]any{}
	}
	teamsMap, ok := teamsAny.(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	result := make(map[string]map[string]any, len(teamsMap))
	for k, v := range teamsMap {
		if bucket, ok := v.(map[string]any); ok {
			result[k] = bucket
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -run TestReadTeamsBucket -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/runtime/metadata.go internal/agent_teams/runtime/metadata_test.go
git commit -m "feat(9.61a): 添加 metadata 常量和 ReadTeamsBucket 函数"
```

---

### Task 2: 9.61a — ReadTeamNamespace / ReadTeamNamesInSession / ReadTeamDBState

**Files:**
- Modify: `internal/agent_teams/runtime/metadata.go`
- Modify: `internal/agent_teams/runtime/metadata_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestReadTeamNamespace_存在(t *testing.T) {
	sess := newFakeSessionFacade()
	sess.UpdateState(map[string]any{
		runtime.TeamsKey: map[string]any{
			"team1": map[string]any{"spec": "data", "db_state": "created"},
		},
	})
	result := runtime.ReadTeamNamespace(sess, "team1")
	assert.NotNil(t, result)
	assert.Equal(t, "data", result["spec"])
}

func TestReadTeamNamespace_不存在(t *testing.T) {
	sess := newFakeSessionFacade()
	result := runtime.ReadTeamNamespace(sess, "no_team")
	assert.Nil(t, result)
}

func TestReadTeamNamesInSession_正常(t *testing.T) {
	sess := newFakeSessionFacade()
	sess.UpdateState(map[string]any{
		runtime.TeamsKey: map[string]any{
			"team1": map[string]any{},
			"team2": map[string]any{},
		},
	})
	names := runtime.ReadTeamNamesInSession(sess)
	assert.Len(t, names, 2)
	assert.Contains(t, names, "team1")
	assert.Contains(t, names, "team2")
}

func TestReadTeamDBState_正常(t *testing.T) {
	sess := newFakeSessionFacade()
	sess.UpdateState(map[string]any{
		runtime.TeamsKey: map[string]any{
			"team1": map[string]any{runtime.TeamDBStateKey: "created"},
		},
	})
	result := runtime.ReadTeamDBState(sess, "team1")
	assert.Equal(t, "created", result)
}

func TestReadTeamDBState_不存在(t *testing.T) {
	sess := newFakeSessionFacade()
	result := runtime.ReadTeamDBState(sess, "no_team")
	assert.Equal(t, "", result)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -run "TestReadTeamNamespace|TestReadTeamNamesInSession|TestReadTeamDBState" -v 2>&1 | tail -5`
Expected: 编译失败

- [ ] **Step 3: 实现 3 个函数**

在 `metadata.go` 的导出函数区追加：

```go
// ReadTeamNamespace 读取单个 team 桶，不存在时返回 nil。
// Python: read_team_namespace(session, team_name) -> dict | None
func ReadTeamNamespace(sess interfaces.SessionFacade, teamName string) map[string]any {
	bucket := ReadTeamsBucket(sess)[teamName]
	if bucket == nil {
		return nil
	}
	return bucket
}

// ReadTeamNamesInSession 列出 session 中已持久化的 team 名。
// Python: read_team_names_in_session(session) -> list[str]
func ReadTeamNamesInSession(sess interfaces.SessionFacade) []string {
	teams := ReadTeamsBucket(sess)
	names := make([]string, 0, len(teams))
	for k := range teams {
		names = append(names, k)
	}
	return names
}

// ReadTeamDBState 读取 db_state 字段值，不存在时返回空字符串。
// Python: read_team_db_state(session, team_name) -> str | None
func ReadTeamDBState(sess interfaces.SessionFacade, teamName string) string {
	bucket := ReadTeamNamespace(sess, teamName)
	if bucket == nil {
		return ""
	}
	value, ok := bucket[TeamDBStateKey]
	if !ok {
		return ""
	}
	strVal, ok := value.(string)
	if !ok {
		return ""
	}
	return strVal
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -run "TestReadTeamNamespace|TestReadTeamNamesInSession|TestReadTeamDBState" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/runtime/metadata.go internal/agent_teams/runtime/metadata_test.go
git commit -m "feat(9.61a): 添加 ReadTeamNamespace / ReadTeamNamesInSession / ReadTeamDBState"
```

---

### Task 3: 9.61a — WriteTeamNamespace / MergeTeamNamespace / MergeTeamDBState / RemoveTeamNamespace

**Files:**
- Modify: `internal/agent_teams/runtime/metadata.go`
- Modify: `internal/agent_teams/runtime/metadata_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestWriteTeamNamespace_覆盖(t *testing.T) {
	sess := newFakeSessionFacade()
	runtime.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v1"})
	result := runtime.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v1", result["spec"])

	runtime.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v2"})
	result = runtime.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v2", result["spec"])
}

func TestMergeTeamNamespace_浅合并不覆盖无关key(t *testing.T) {
	sess := newFakeSessionFacade()
	runtime.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v1", "context": "c1"})
	runtime.MergeTeamNamespace(sess, "team1", map[string]any{"spec": "v2"})
	result := runtime.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v2", result["spec"])
	assert.Equal(t, "c1", result["context"])
}

func TestMergeTeamNamespace_桶不存在时创建(t *testing.T) {
	sess := newFakeSessionFacade()
	runtime.MergeTeamNamespace(sess, "team1", map[string]any{"spec": "v1"})
	result := runtime.ReadTeamNamespace(sess, "team1")
	assert.Equal(t, "v1", result["spec"])
}

func TestMergeTeamDBState_写入后读取(t *testing.T) {
	sess := newFakeSessionFacade()
	runtime.MergeTeamDBState(sess, "team1", "created")
	result := runtime.ReadTeamDBState(sess, "team1")
	assert.Equal(t, "created", result)
}

func TestRemoveTeamNamespace_存在时删除(t *testing.T) {
	sess := newFakeSessionFacade()
	runtime.WriteTeamNamespace(sess, "team1", map[string]any{"spec": "v1"})
	removed := runtime.RemoveTeamNamespace(sess, "team1")
	assert.True(t, removed)
	assert.Nil(t, runtime.ReadTeamNamespace(sess, "team1"))
}

func TestRemoveTeamNamespace_不存在返回false(t *testing.T) {
	sess := newFakeSessionFacade()
	removed := runtime.RemoveTeamNamespace(sess, "no_team")
	assert.False(t, removed)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -run "TestWriteTeamNamespace|TestMergeTeamNamespace|TestMergeTeamDBState|TestRemoveTeamNamespace" -v 2>&1 | tail -5`
Expected: 编译失败

- [ ] **Step 3: 实现 4 个函数**

在 `metadata.go` 的导出函数区追加：

```go
// WriteTeamNamespace 整体覆盖某 team 桶。
// Python: write_team_namespace(session, team_name, payload) -> None
func WriteTeamNamespace(sess interfaces.SessionFacade, teamName string, payload map[string]any) {
	teams := readTeamsBucketMutable(sess)
	teams[teamName] = payload
	sess.UpdateState(map[string]any{TeamsKey: teams})
}

// MergeTeamNamespace 浅合并 partial 到某 team 桶，key 覆盖同名，不动其他。
// Python: merge_team_namespace(session, team_name, partial) -> None
func MergeTeamNamespace(sess interfaces.SessionFacade, teamName string, partial map[string]any) {
	teams := readTeamsBucketMutable(sess)
	bucket, ok := teams[teamName]
	if !ok || bucket == nil {
		bucket = make(map[string]any)
	} else {
		// 深拷贝桶，避免修改原始数据
		bucket = copyMap(bucket)
	}
	for k, v := range partial {
		bucket[k] = v
	}
	teams[teamName] = bucket
	sess.UpdateState(map[string]any{TeamsKey: teams})
}

// MergeTeamDBState 通过 MergeTeamNamespace 写入 db_state 字段。
// Python: merge_team_db_state(session, team_name, state) -> None
func MergeTeamDBState(sess interfaces.SessionFacade, teamName string, dbState string) {
	MergeTeamNamespace(sess, teamName, map[string]any{TeamDBStateKey: dbState})
}

// RemoveTeamNamespace 删除 team 桶，返回是否实际删除。
// Python: remove_team_namespace(session, team_name) -> bool
func RemoveTeamNamespace(sess interfaces.SessionFacade, teamName string) bool {
	teams := readTeamsBucketMutable(sess)
	if _, exists := teams[teamName]; !exists {
		return false
	}
	delete(teams, teamName)
	sess.UpdateState(map[string]any{TeamsKey: teams})
	return true
}
```

在非导出函数区添加：

```go
// readTeamsBucketMutable 读取 teams 命名空间的可变副本。
// 对齐 Python：read → mutate → write back 模式需要可变副本。
func readTeamsBucketMutable(sess interfaces.SessionFacade) map[string]any {
	teamsAny, _ := sess.GetState(state.StringKey(TeamsKey))
	if teamsAny == nil {
		return make(map[string]any)
	}
	teamsMap, ok := teamsAny.(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return copyMap(teamsMap)
}

// copyMap 浅拷贝 map[string]any。
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/runtime/ -v`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/runtime/metadata.go internal/agent_teams/runtime/metadata_test.go
git commit -m "feat(9.61a): 完成 WriteTeamNamespace / MergeTeamNamespace / MergeTeamDBState / RemoveTeamNamespace"
```

---

### Task 4: 9.61a — 更新 runtime/doc.go

**Files:**
- Modify: `internal/agent_teams/runtime/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

将现有文件目录：
```
//	runtime/
//	├── doc.go      # 包文档
//	├── gate.go     # InteractGate 并发门控
//	├── pool.go     # ActiveTeam/ActiveTeamInfo/TeamRuntimePool
//	└── manager.go  # TeamRuntimeManager（interact 完整实现，其余空 stub）
```

更新为：
```
//	runtime/
//	├── doc.go       # 包文档
//	├── gate.go      # InteractGate 并发门控
//	├── pool.go      # ActiveTeam/ActiveTeamInfo/TeamRuntimePool
//	├── manager.go   # TeamRuntimeManager（interact 完整实现，其余空 stub）
//	└── metadata.go  # Team Namespace 读写函数（对齐 Python metadata.py）
```

- [ ] **Step 2: 提交**

```bash
git add internal/agent_teams/runtime/doc.go
git commit -m "docs(9.61a): 更新 runtime/doc.go 添加 metadata.go 条目"
```

---

### Task 5: 为 SpawnManager 添加 SpawnedHandles 导出访问器

**Files:**
- Modify: `internal/agent_teams/agent/spawn_manager.go`

- [ ] **Step 1: 添加 SpawnedHandles 方法**

在 `spawn_manager.go` 的导出函数区追加：

```go
// SpawnedHandles 返回已生成的句柄快照（key=memberName）。
// RecoveryManager 通过此方法判断哪些 teammate 有存活句柄。
// Python: SpawnManager.spawned_handles (property)
func (m *SpawnManager) SpawnedHandles() map[string]spawn.SpawnHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]spawn.SpawnHandle, len(m.spawnedHandles))
	for k, v := range m.spawnedHandles {
		result[k] = v
	}
	return result
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/agent/spawn_manager.go
git commit -m "feat(9.61): 添加 SpawnManager.SpawnedHandles 导出访问器"
```

---

### Task 6: 9.61 — RecoveryManager 结构体 + NewRecoveryManager + RecoverTeam

**Files:**
- Create: `internal/agent_teams/agent/recovery_manager.go`
- Create: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 写 failing test — TestNewRecoveryManager + TestRecoverTeam_teamBackend为nil**

```go
package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// fakeConfigurator 用于测试的轻量 AgentConfigurator 替身。
// 因为 AgentConfigurator 依赖较多，这里通过构造真实的 AgentConfigurator
// 并在测试中设置字段来验证行为。
func newTestConfigurator() *agent.AgentConfigurator {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("test_leader"),
		agentschema.WithAgentName("test_leader"),
	)
	return agent.NewAgentConfigurator(card)
}

func TestNewRecoveryManager(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	assert.NotNil(t, rm)
}

func TestRecoverTeam_teamBackend为nil(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	result := rm.RecoverTeam(context.Background())
	assert.Empty(t, result)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestNewRecoveryManager|TestRecoverTeam_teamBackend为nil" -v 2>&1 | tail -5`
Expected: 编译失败（`agent.NewRecoveryManager` 未定义）

- [ ] **Step 3: 实现 recovery_manager.go — LiveTeammate + 结构体 + NewRecoveryManager + RecoverTeam**

```go
package agent

import (
	"context"
	"encoding/json"
	"fmt"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LiveTeammate 活跃队友快照（成员名 + 当前状态）。
// Python: collect_live_teammates_for_session_switch 返回的 list[tuple[str, MemberStatus]]
type LiveTeammate struct {
	// MemberName 成员名
	MemberName string
	// Status 当前 DB 状态
	Status atschema.MemberStatus
}

// RecoveryManager 团队恢复管理器。
// 负责：团队恢复、成员状态转换、Leader 配置持久化、Allocator 状态持久化。
//
// Python: openjiuwen/agent_teams/agent/recovery_manager.py
type RecoveryManager struct {
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// spawnManager 子进程管理器
	spawnManager *SpawnManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// rmLogComponent 日志组件
	rmLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRecoveryManager 创建恢复管理器实例。
// Python: RecoveryManager.__init__(configurator, spawn_manager)
func NewRecoveryManager(configurator *AgentConfigurator, spawnManager *SpawnManager) *RecoveryManager {
	return &RecoveryManager{
		configurator: configurator,
		spawnManager: spawnManager,
	}
}

// RecoverTeam 从数据库状态恢复团队，返回成功重启的成员名列表。
// Python: async recover_team(self) -> list[str]
func (m *RecoveryManager) RecoverTeam(ctx context.Context) []string {
	teamBackend := m.configurator.TeamBackend()
	if teamBackend == nil {
		return []string{}
	}

	memberName := m.configurator.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(rmLogComponent).
		Str("member_name", memberName).
		Msg("recovering team")

	// 重建 HITT 名册缓存（冷启动 Leader 后缓存为空）
	// Python: await team_backend.refresh_human_agent_roster()
	teamBackend.RefreshHumanAgentRoster(ctx)

	// 获取所有成员
	// Python: all_members = await team_backend.list_members()
	allMembers, err := teamBackend.ListMembers(ctx)
	if err != nil {
		logger.Error(rmLogComponent).Err(err).Msg("recover_team: list_members 失败")
		return []string{}
	}

	restarted := []string{}
	teamName := m.configurator.TeamName()

	for _, member := range allMembers {
		// 跳过自身
		// Python: if member.member_name == member_name: continue
		if member.MemberName == m.configurator.MemberName() {
			continue
		}

		// 更新 DB 状态为 RESTARTING
		// Python: await team_backend.db.member.update_member_status(member.member_name, team_name, MemberStatus.RESTARTING.value)
		if teamName != "" {
			teamBackend.DB().Member().UpdateMemberStatus(
				ctx, member.MemberName, teamName, string(atschema.MemberStatusRestarting),
			)
		}

		// 重启 teammate
		// Python: if await self._spawn_manager.restart_teammate(member.member_name)
		if err := m.spawnManager.RestartTeammate(ctx, member.MemberName, defaultMaxRetries); err == nil {
			restarted = append(restarted, member.MemberName)
		}
	}

	return restarted
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：`defaultMaxRetries` 已在 `spawn_manager.go` 中定义为常量 `3`，同包可直接引用。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestNewRecoveryManager|TestRecoverTeam_teamBackend为nil" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager.go internal/agent_teams/agent/recovery_manager_test.go
git commit -m "feat(9.61): 添加 RecoveryManager 结构体 + RecoverTeam 方法"
```

---

### Task 7: 9.61 — PersistLeaderConfig

**Files:**
- Modify: `internal/agent_teams/agent/recovery_manager.go`
- Modify: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestPersistLeaderConfig_spec为nil时跳过(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade() // 需要 import fakeSessionFacade
	// Spec 为 nil，不应写入任何数据
	rm.PersistLeaderConfig(sess)
	// 验证 teams 命名空间为空
	teams := runtime.ReadTeamsBucket(sess)
	assert.Empty(t, teams)
}

func TestPersistLeaderConfig_正常持久化(t *testing.T) {
	cfg := newTestConfigurator()
	// 设置 blueprint 使 Spec/TeamName 生效
	spec := atschema.TeamAgentSpec{Agents: map[string]atschema.DeepAgentSpec{"leader": {}}}
	runtimeCtx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader", TeamName: ptrString("my_team")}
	cfg.SetupInfra(spec, runtimeCtx)
	cfg.SetupAgent(spec, runtimeCtx)

	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade()

	rm.PersistLeaderConfig(sess)

	ns := runtime.ReadTeamNamespace(sess, "my_team")
	assert.NotNil(t, ns)
	// 应包含 spec 和 context key
	assert.Contains(t, ns, "spec")
	assert.Contains(t, ns, "context")
	assert.Contains(t, ns, runtime.TeamDBStateKey)
}
```

注意：测试中需要把 `fakeSessionFacade` 从 `runtime_test` 包移到共享位置或直接在 `agent_test` 包内重新定义。为简化，直接在 `recovery_manager_test.go` 中定义一个本地的 `fakeSessionFacade`（实现 `interfaces.SessionFacade`），并 `import runtime` 包来读取。

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestPersistLeaderConfig" -v 2>&1 | tail -5`
Expected: 编译失败（`PersistLeaderConfig` 未定义）

- [ ] **Step 3: 实现 PersistLeaderConfig**

在 `recovery_manager.go` 的导出函数区追加：

```go
// PersistLeaderConfig 持久化 Leader 配置到 team namespace。
// Python: persist_leader_config(self, session) -> None
func (m *RecoveryManager) PersistLeaderConfig(sess interfaces.SessionFacade) {
	spec := m.configurator.Spec()
	ctx := m.configurator.RuntimeContext()
	teamName := m.configurator.TeamName()

	// Python: if spec is None or ctx is None or team_name is None: return
	if spec == nil || ctx == nil || teamName == "" {
		return
	}

	// 构建 payload
	// Python: payload = {"spec": spec.model_dump(mode="json"), "context": ctx.model_dump(mode="json"), TEAM_DB_STATE_KEY: ...}
	specBytes, _ := json.Marshal(spec)
	ctxBytes, _ := json.Marshal(ctx)

	var specMap map[string]any
	var ctxMap map[string]any
	_ = json.Unmarshal(specBytes, &specMap)
	_ = json.Unmarshal(ctxBytes, &ctxMap)

	payload := map[string]any{
		"spec":    specMap,
		"context": ctxMap,
	}

	// db_state: 从 session 读取，空则 fallback 为 pending_create
	// Python: TEAM_DB_STATE_KEY: read_team_db_state(session, team_name) or TEAM_DB_STATE_PENDING_CREATE
	dbState := runtime.ReadTeamDBState(sess, teamName)
	if dbState == "" {
		dbState = runtime.TeamDBStatePendingCreate
	}
	payload[runtime.TeamDBStateKey] = dbState

	// 如果 model_allocator 存在，加入 allocator_state
	// Python: if allocator is not None: payload["model_allocator_state"] = allocator.state_dict()
	allocator := m.configurator.ModelAllocator()
	if allocator != nil {
		payload["model_allocator_state"] = allocator.StateDict()
	}

	// Python: write_team_namespace(session, team_name, payload)
	runtime.WriteTeamNamespace(sess, teamName, payload)
}
```

需要在文件头部 import 中添加：
```go
"encoding/json"
"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestPersistLeaderConfig" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager.go internal/agent_teams/agent/recovery_manager_test.go
git commit -m "feat(9.61): 实现 RecoveryManager.PersistLeaderConfig"
```

---

### Task 8: 9.61 — markTeammateRestartingForSessionSwitch

**Files:**
- Modify: `internal/agent_teams/agent/recovery_manager.go`
- Modify: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestMarkTeammateRestarting_已是Restarting(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	// 不设 teamBackend，无法操作 DB
	result := rm.MarkTeammateRestartingForSessionSwitch(
		context.Background(), "member1", atschema.MemberStatusRestarting,
	)
	// 无 teamBackend 时返回 false
	assert.False(t, result)
}
```

完整测试还需要 mock TeamBackend 和 DB。为简化，先测试边界条件（无 backend 返回 false），后续通过集成测试验证状态转换。

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 markTeammateRestartingForSessionSwitch（非导出方法，但同包测试可访问）**

在 `recovery_manager.go` 的非导出函数区添加：

```go
// markTeammateRestartingForSessionSwitch 将活跃队友的状态转为 RESTARTING。
// READY/BUSY/SHUTDOWN_REQUESTED/UNSTARTED 需先经 ERROR 中转。
// Python: _mark_teammate_restarting_for_session_switch(self, member_name, current_status) -> bool
func (m *RecoveryManager) markTeammateRestartingForSessionSwitch(
	ctx context.Context,
	memberName string,
	currentStatus atschema.MemberStatus,
) bool {
	teamBackend := m.configurator.TeamBackend()
	if teamBackend == nil {
		return false
	}
	teamName := m.configurator.TeamName()
	if teamName == "" {
		return false
	}
	db := teamBackend.DB()

	// 已经是 RESTARTING，直接返回 true
	if currentStatus == atschema.MemberStatusRestarting {
		return true
	}

	// 可直接转换到 RESTARTING 的状态集合
	// Python: directly_restartable = {PAUSED, STOPPED, ERROR, SHUTDOWN}
	directlyRestartable := map[atschema.MemberStatus]bool{
		atschema.MemberStatusPaused:   true,
		atschema.MemberStatusStopped:  true,
		atschema.MemberStatusError:    true,
		atschema.MemberStatusShutdown: true,
	}

	// READY/BUSY/SHUTDOWN_REQUESTED/UNSTARTED 需先经 ERROR 中转
	if !directlyRestartable[currentStatus] {
		// 先转为 ERROR
		updated := db.Member().UpdateMemberStatus(
			ctx, memberName, teamName, string(atschema.MemberStatusError),
		)
		if !updated {
			logger.Warn(rmLogComponent).
				Str("member_name", memberName).
				Str("current_status", string(currentStatus)).
				Msg("Failed to move teammate from current_status to ERROR before session rebind")
			return false
		}
		currentStatus = atschema.MemberStatusError
	}

	// 再转为 RESTARTING
	if currentStatus == atschema.MemberStatusRestarting {
		return true
	}
	updated := db.Member().UpdateMemberStatus(
		ctx, memberName, teamName, string(atschema.MemberStatusRestarting),
	)
	if !updated {
		logger.Warn(rmLogComponent).
			Str("member_name", memberName).
			Msg("Failed to move teammate into RESTARTING during session rebind")
		return false
	}
	return true
}
```

注意：此方法设计为非导出，但测试在同包 `agent_test` 中无法直接访问。两种方案：
1. 将方法改为导出 `MarkTeammateRestartingForSessionSwitch`（Python 中虽然是 `_` 前缀私有，但 Go 习惯同包测试可访问非导出）
2. 通过 `RestartForSessionSwitch` 间接测试

选择方案 1：**导出为 `MarkTeammateRestartingForSessionSwitch`**，因为 Go 中同包 `_test` 后缀文件无法访问非导出方法（`agent_test` 包 vs `agent` 包），而单独的 `agent_test` 包是项目测试惯例。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestMarkTeammateRestarting" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager.go internal/agent_teams/agent/recovery_manager_test.go
git commit -m "feat(9.61): 实现 MarkTeammateRestartingForSessionSwitch"
```

---

### Task 9: 9.61 — CollectLiveTeammatesForSessionSwitch

**Files:**
- Modify: `internal/agent_teams/agent/recovery_manager.go`
- Modify: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestCollectLiveTeammates_非Leader返回空(t *testing.T) {
	cfg := newTestConfigurator()
	// 未 configure，默认 Role 为空
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	result := rm.CollectLiveTeammatesForSessionSwitch(context.Background())
	assert.Empty(t, result)
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 CollectLiveTeammatesForSessionSwitch**

在 `recovery_manager.go` 的导出函数区追加：

```go
// CollectLiveTeammatesForSessionSwitch 快照需要 session 切换时重新绑定的活跃队友。
// 仅 Leader 角色且 teamBackend 存在时执行。
// Python: collect_live_teammates_for_session_switch(self) -> list[tuple[str, MemberStatus]]
func (m *RecoveryManager) CollectLiveTeammatesForSessionSwitch(ctx context.Context) []LiveTeammate {
	if m.configurator.Role() != atschema.TeamRoleLeader || m.configurator.TeamBackend() == nil {
		return nil
	}

	teamBackend := m.configurator.TeamBackend()
	members, err := teamBackend.ListMembers(ctx)
	if err != nil {
		logger.Error(rmLogComponent).Err(err).Msg("collect_live_teammates: list_members 失败")
		return nil
	}

	leaderMemberName := m.configurator.MemberName()
	spawned := m.spawnManager.SpawnedHandles()

	// 构建 live 集合：有运行时句柄 + 非 Leader
	// Python: live_teammates = {name for name, handle in spawned.items() if name != leader and handle is not None}
	liveTeammates := make(map[string]struct{})
	for name, handle := range spawned {
		if name != leaderMemberName && handle != nil {
			liveTeammates[name] = struct{}{}
		}
	}

	result := []LiveTeammate{}
	// 排除终态
	// Python: if member_status in {UNSTARTED, SHUTDOWN, STOPPED}: continue
	excludedStatuses := map[atschema.MemberStatus]bool{
		atschema.MemberStatusUnstarted: true,
		atschema.MemberStatusShutdown:  true,
		atschema.MemberStatusStopped:   true,
	}

	for _, member := range members {
		if _, ok := liveTeammates[member.MemberName]; !ok {
			continue
		}
		memberStatus := atschema.MemberStatus(member.Status)
		if excludedStatuses[memberStatus] {
			continue
		}
		result = append(result, LiveTeammate{
			MemberName: member.MemberName,
			Status:     memberStatus,
		})
	}

	return result
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestCollectLiveTeammates" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager.go internal/agent_teams/agent/recovery_manager_test.go
git commit -m "feat(9.61): 实现 CollectLiveTeammatesForSessionSwitch"
```

---

### Task 10: 9.61 — RestartForSessionSwitch

**Files:**
- Modify: `internal/agent_teams/agent/recovery_manager.go`
- Modify: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestRestartForSessionSwitch_空列表无操作(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	// 空列表不应 panic
	rm.RestartForSessionSwitch(context.Background(), nil, true)
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 RestartForSessionSwitch**

在 `recovery_manager.go` 的导出函数区追加：

```go
// RestartForSessionSwitch 在 session 切换后重启指定队友。
// cleanupFirst=true 时先清理旧 handle 再重启；false 表示协调层已清理。
// Python: restart_for_session_switch(self, recoverable_members, *, cleanup_first) -> None
func (m *RecoveryManager) RestartForSessionSwitch(
	ctx context.Context,
	recoverableMembers []LiveTeammate,
	cleanupFirst bool,
) {
	for _, lt := range recoverableMembers {
		// cleanupFirst=true 时先清理旧 handle
		if cleanupFirst {
			m.spawnManager.CleanupTeammate(ctx, lt.MemberName)
		}

		// 标记为 RESTARTING，失败则跳过
		if !m.MarkTeammateRestartingForSessionSwitch(ctx, lt.MemberName, lt.Status) {
			continue
		}

		// 重启 teammate（对齐 Python：不抛异常，忽略失败）
		_ = m.spawnManager.RestartTeammate(ctx, lt.MemberName, defaultMaxRetries)
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestRestartForSessionSwitch" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager.go internal/agent_teams/agent/recovery_manager_test.go
git commit -m "feat(9.61): 实现 RestartForSessionSwitch"
```

---

### Task 11: 9.61 — PersistAllocatorState

**Files:**
- Modify: `internal/agent_teams/agent/recovery_manager.go`
- Modify: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 写 failing test**

```go
func TestPersistAllocatorState_allocator为nil时跳过(t *testing.T) {
	cfg := newTestConfigurator()
	sm := agent.NewSpawnManager(agent.NewTeamAgentState(), cfg, nil)
	rm := agent.NewRecoveryManager(cfg, sm)
	sess := newFakeSessionFacade()
	rm.PersistAllocatorState(sess)
	// 无写入
	teams := runtime.ReadTeamsBucket(sess)
	assert.Empty(t, teams)
}
```

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 PersistAllocatorState**

在 `recovery_manager.go` 的导出函数区追加：

```go
// PersistAllocatorState 持久化 allocator 状态到 team namespace。
// Python: persist_allocator_state(self, team_session) -> None
func (m *RecoveryManager) PersistAllocatorState(sess interfaces.SessionFacade) {
	allocator := m.configurator.ModelAllocator()
	teamName := m.configurator.TeamName()
	memberName := m.configurator.MemberName()
	if memberName == "" {
		memberName = "?"
	}

	// Python: if team_session is None or allocator is None or team_name is None: return
	if sess == nil || allocator == nil || teamName == "" {
		return
	}

	// Python: try: merge_team_namespace(session, team_name, {"model_allocator_state": allocator.state_dict()})
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(rmLogComponent).
					Str("member_name", memberName).
					Str("error", fmt.Sprintf("%v", r)).
					Msg("failed to persist allocator state")
			}
		}()
		runtime.MergeTeamNamespace(sess, teamName, map[string]any{
			"model_allocator_state": allocator.StateDict(),
		})
	}()
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestPersistAllocatorState" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager.go internal/agent_teams/agent/recovery_manager_test.go
git commit -m "feat(9.61): 实现 PersistAllocatorState"
```

---

### Task 12: 回填 session_manager.go 的 TODO(#9.61)

**Files:**
- Modify: `internal/agent_teams/agent/session_manager.go`

- [ ] **Step 1: 替换 recoveryManager 字段类型和构造参数**

旧：
```go
	// recoveryManager 恢复管理器
	// TODO(#9.61): 替换 any 为 RecoveryManager 类型
	recoveryManager any
```

新：
```go
	// recoveryManager 恢复管理器
	recoveryManager *RecoveryManager
```

旧：
```go
	recoveryManager any, // TODO(#9.61): RecoveryManager 类型
```

新：
```go
	recoveryManager *RecoveryManager,
```

- [ ] **Step 2: 回填 BindSession 步骤 4-5**

旧（步骤 4 注释）：
```go
	// 步骤 4: 创建 DB 表（幂等）
	// Python: if team_backend: await team_backend.db.create_cur_session_tables()
	// TODO(#9.61): if teamBackend := m.configurator.TeamBackend(); teamBackend != nil { ... }
```

新：
```go
	// 步骤 4: 创建 DB 表（幂等）
	// Python: if team_backend: await team_backend.db.create_cur_session_tables()
	if teamBackend := m.configurator.TeamBackend(); teamBackend != nil {
		if err := teamBackend.DB().CreateCurSessionTables(ctx); err != nil {
			logger.Warn(sessionMgrLogComponent).Err(err).
				Str("session_id", sessionID).
				Msg("创建会话表失败")
		}
	}
```

旧（步骤 5 注释）：
```go
	// 步骤 5: Leader 侧持久化
	// Python: if spec and role == TeamRole.LEADER: recovery_manager.persist_leader_config(session)
	// TODO(#9.61): if m.configurator.Role() == TeamRoleLeader && m.configurator.Spec() != nil { ... }
```

新：
```go
	// 步骤 5: Leader 侧持久化
	// Python: if spec and role == TeamRole.LEADER: recovery_manager.persist_leader_config(session)
	if m.recoveryManager != nil && m.configurator.Role() == atschema.TeamRoleLeader && m.configurator.Spec() != nil {
		type sessionFacer interface{ interfaces.SessionFacade }
		if sf, ok := session.(sessionFacer); ok {
			m.recoveryManager.PersistLeaderConfig(sf)
		}
	}
```

- [ ] **Step 3: 回填 ResumeForNewSession 步骤 1, 3-4**

旧（步骤 1）：
```go
	// 步骤 1: 收集活着的 teammate（快照）
	// Python: recoverable_members = await self._recovery_manager.collect_live_teammates_for_session_switch()
	// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()
```

新：
```go
	// 步骤 1: 收集活着的 teammate（快照）
	// Python: recoverable_members = await self._recovery_manager.collect_live_teammates_for_session_switch()
	var recoverableMembers []LiveTeammate
	if m.recoveryManager != nil {
		recoverableMembers = m.recoveryManager.CollectLiveTeammatesForSessionSwitch(ctx)
	}
```

旧（步骤 3-4）：
```go
	// 步骤 3-4: Leader 侧重启 teammate
	// Python:
	//   if self._configurator.role != TeamRole.LEADER or not team_backend: return
	//   await self._recovery_manager.restart_for_session_switch(recoverable_members, cleanup_first=True)
	// TODO(#9.61): if m.configurator.Role() != Leader || m.configurator.TeamBackend() == nil → return
	// TODO(#9.61): m.recoveryManager.RestartForSessionSwitch(recoverableMembers, true)
```

新：
```go
	// 步骤 3-4: Leader 侧重启 teammate
	// Python:
	//   if self._configurator.role != TeamRole.LEADER or not team_backend: return
	//   await self._recovery_manager.restart_for_session_switch(recoverable_members, cleanup_first=True)
	if m.recoveryManager != nil && m.configurator.Role() == atschema.TeamRoleLeader && m.configurator.TeamBackend() != nil {
		m.recoveryManager.RestartForSessionSwitch(ctx, recoverableMembers, true)
	}
```

- [ ] **Step 4: 回填 RecoverForExistingSession 步骤 1, 3-4**

与 ResumeForNewSession 相同模式，但 `cleanupFirst=false`：

旧（步骤 1）：
```go
	// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()
```

新：
```go
	var recoverableMembers []LiveTeammate
	if m.recoveryManager != nil {
		recoverableMembers = m.recoveryManager.CollectLiveTeammatesForSessionSwitch(ctx)
	}
```

旧（步骤 3-4）：
```go
	// TODO(#9.61): if m.configurator.Role() != Leader || m.configurator.TeamBackend() == nil → return
	// TODO(#9.61): m.recoveryManager.RestartForSessionSwitch(recoverableMembers, false)
```

新：
```go
	if m.recoveryManager != nil && m.configurator.Role() == atschema.TeamRoleLeader && m.configurator.TeamBackend() != nil {
		m.recoveryManager.RestartForSessionSwitch(ctx, recoverableMembers, false)
	}
```

- [ ] **Step 5: 更新 import**

在 `session_manager.go` 的 import 中添加：
```go
atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
```

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/`
Expected: 编译成功

- [ ] **Step 7: 运行现有测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agent_teams/agent/session_manager.go
git commit -m "feat(9.61): 回填 session_manager.go 的 TODO(#9.61) 占位"
```

---

### Task 13: 回填 team_agent.go 的 TODO(#9.61) + RecoverFromSession 占位

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go`

- [ ] **Step 1: 替换 recoveryManager 字段类型**

旧：
```go
	// recoveryManager 恢复管理器
	// TODO(#9.61): RecoveryManager 类型
	recoveryManager any
```

新：
```go
	// recoveryManager 恢复管理器
	recoveryManager *RecoveryManager
```

- [ ] **Step 2: 回填 NewTeamAgent 构造**

旧：
```go
	// TODO(#9.61): 构建 RecoveryManager(configurator, spawnManager)
	a.sessionManager = NewSessionManager(a.state, a.configurator, a.recoveryManager)
```

新：
```go
	a.recoveryManager = NewRecoveryManager(a.configurator, a.spawnManager)
	a.sessionManager = NewSessionManager(a.state, a.configurator, a.recoveryManager)
```

- [ ] **Step 3: 回填 RecoverTeam**

旧：
```go
func (a *TeamAgent) RecoverTeam(ctx context.Context) ([]string, error) {
	// TODO(#9.61): 恢复管理器恢复团队 recoveryManager.recover_team()
	return nil, nil
}
```

新：
```go
func (a *TeamAgent) RecoverTeam(ctx context.Context) ([]string, error) {
	if a.recoveryManager != nil {
		return a.recoveryManager.RecoverTeam(ctx), nil
	}
	return nil, nil
}
```

- [ ] **Step 4: 回填 RecoverFromSession（占位函数）**

旧：
```go
func RecoverFromSession(ctx context.Context, session any, teamName string, runtimeSpec *atschema.TeamAgentSpec) (*TeamAgent, error) {
	// TODO(#9.61): 从 session 读取 bucket → 解析 spec/context → NewTeamAgent → configure → restore_allocator_state → set_session_id
	return nil, nil
}
```

新：
```go
// RecoverFromSession 从 session 检查点重构 Leader TeamAgent。
// 流程：读取 team namespace → 解析 spec/context → NewTeamAgent → configure → restore_allocator_state → set_session_id
//
// Python: TeamAgent.recover_from_session(session, team_name, runtime_spec)
// TODO(#9.55): 实现完整恢复逻辑
func RecoverFromSession(ctx context.Context, session any, teamName string, runtimeSpec *atschema.TeamAgentSpec) (*TeamAgent, error) {
	return nil, fmt.Errorf("RecoverFromSession 尚未实现（#9.55）")
}
```

- [ ] **Step 5: 回填 PersistSessionManifest**

旧：
```go
func (a *TeamAgent) PersistSessionManifest(session any) {
	// TODO(#9.61): 持久化领导者配置 recoveryManager.persist_leader_config(session)
}
```

新：
```go
func (a *TeamAgent) PersistSessionManifest(session any) {
	if a.recoveryManager != nil {
		if sf, ok := session.(interfaces.SessionFacade); ok {
			a.recoveryManager.PersistLeaderConfig(sf)
		}
	}
}
```

- [ ] **Step 6: 回填 UpdateModelPool 中的 TODO**

旧：
```go
	// TODO(#9.61): 持久化领导者配置 recoveryManager.persist_leader_config
```

新：
```go
	// 持久化领导者配置（模型池变更后）
	if a.recoveryManager != nil && a.sessionManager != nil {
		if sf, ok := a.sessionManager.TeamSession().(interfaces.SessionFacade); ok {
			a.recoveryManager.PersistLeaderConfig(sf)
		}
	}
```

- [ ] **Step 7: 更新 RecoveryManager() 访问器返回类型**

旧：
```go
func (a *TeamAgent) RecoveryManager() any {
	return a.recoveryManager
}
```

新：
```go
func (a *TeamAgent) RecoveryManager() *RecoveryManager {
	return a.recoveryManager
}
```

- [ ] **Step 8: 更新 import**

确保 `team_agent.go` 的 import 中包含：
```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
```

- [ ] **Step 9: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/agent/`
Expected: 编译成功

- [ ] **Step 10: 运行现有测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 11: 提交**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "feat(9.61): 回填 team_agent.go 的 TODO(#9.61) + RecoverFromSession 占位"
```

---

### Task 14: 更新 agent/doc.go

**Files:**
- Modify: `internal/agent_teams/agent/doc.go`

- [ ] **Step 1: 更新文件目录中 recovery_manager.go 条目**

旧：
```
//	└── recovery_manager.go   # ⤵️ 回填: #9.61 恢复管理器
```

新：
```
//	└── recovery_manager.go   # 恢复管理器（团队恢复、会话切换、配置持久化）
```

- [ ] **Step 2: 提交**

```bash
git add internal/agent_teams/agent/doc.go
git commit -m "docs(9.61): 更新 agent/doc.go recovery_manager.go 条目"
```

---

### Task 15: 更新 session_manager_test.go

**Files:**
- Modify: `internal/agent_teams/agent/session_manager_test.go`

- [ ] **Step 1: 移除 TODO(#9.61) 注释 + 适配新类型**

搜索 `TODO(#9.61)` 注释并移除。将 `NewSessionManager(state, configurator, nil)` 中 `nil` 的类型适配为 `*RecoveryManager`（传 nil 仍然合法）。

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/ -run "TestSessionManager" -v -count=1`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/agent/session_manager_test.go
git commit -m "test(9.61): 清理 session_manager_test.go TODO(#9.61) 注释"
```

---

### Task 16: 补充 RecoveryManager 测试覆盖率

**Files:**
- Modify: `internal/agent_teams/agent/recovery_manager_test.go`

- [ ] **Step 1: 补充 PersistLeaderConfig 测试**

需要构造带 Spec/TeamName 的 configurator，验证 payload 内容。

- [ ] **Step 2: 补充 MarkTeammateRestartingForSessionSwitch 测试**

需要 mock TeamBackend + DB + MemberDao。创建 `fakeTeamBackend` 和 `fakeMemberDao`：

```go
// fakeMemberDao 实现 database.MemberDao 接口（仅 UpdateMemberStatus）
type fakeMemberDao struct {
	statuses map[string]string // key=memberName, value=status
}

func (d *fakeMemberDao) UpdateMemberStatus(_ context.Context, memberName, _, status string) bool {
	d.statuses[memberName] = status
	return true
}
// ... 其余方法返回零值
```

测试用例：
- `PAUSED` 可直接转 `RESTARTING` → 验证 DB 调用一次
- `READY` 需先转 `ERROR` 再转 `RESTARTING` → 验证 DB 调用两次
- `ERROR` 中转失败返回 false

- [ ] **Step 3: 补充 CollectLiveTeammatesForSessionSwitch 测试**

需要设置 Role 为 Leader + TeamBackend + SpawnedHandles 中的模拟句柄。

- [ ] **Step 4: 补充 PersistAllocatorState 正常路径测试**

构造带 ModelAllocator 的 configurator，验证 MergeTeamNamespace 写入。

- [ ] **Step 5: 运行覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agent_teams/agent/ -run "TestRecoveryManager|TestNewRecoveryManager|TestRecoverTeam|TestPersistLeader|TestMarkTeammate|TestCollectLive|TestRestartFor|TestPersistAllocator"`
Expected: RecoveryManager 覆盖率 ≥ 85%

- [ ] **Step 6: 提交**

```bash
git add internal/agent_teams/agent/recovery_manager_test.go
git commit -m "test(9.61): 补充 RecoveryManager 测试覆盖率至 85%+"
```

---

### Task 17: 全量编译 + 测试 + 覆盖率验证

**Files:**
- 无修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -count=1`
Expected: 全部 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agent_teams/runtime/ ./internal/agent_teams/agent/`
Expected: runtime ≥ 85%, agent ≥ 85%

- [ ] **Step 4: 确认无残留 TODO(#9.61)**

Run: `grep -rn "TODO(#9.61)" /home/opensource/uapclaw-gateway/internal/agent_teams/`
Expected: 无结果（除了 RecoverFromSession 的 `TODO(#9.55)`）

---

### Task 18: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 标记 9.61 为 ✅**

将 9.61 行的状态从 `☐` 改为 `✅`。

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(9.61): 标记 9.61 RecoveryManager 为已完成"
```

---

## 自查清单

**Spec 覆盖：**
- 9.61a 8 个 metadata 函数 → Task 1-3 ✅
- 9.61 RecoveryManager 7 个方法 → Task 6-11 ✅
- SpawnedHandles 访问器 → Task 5 ✅
- session_manager.go 回填 → Task 12 ✅
- team_agent.go 回填 → Task 13 ✅
- RecoverFromSession 占位 → Task 13 ✅
- doc.go 更新 → Task 4, 14 ✅
- IMPLEMENTATION_PLAN.md → Task 18 ✅

**Placeholder scan:** 无 TBD/TODO（除 RecoverFromSession 的 `TODO(#9.55)` 是设计意图）✅

**Type consistency:**
- `LiveTeammate` 在 Task 6 定义，在 Task 9/10/12 使用 ✅
- `*RecoveryManager` 类型在 Task 6 定义，在 Task 12/13 替换 `any` ✅
- `SpawnedHandles()` 在 Task 5 定义，在 Task 9 使用 ✅
- `interfaces.SessionFacade` 参数在 Task 7/11 定义，在 Task 12/13 使用 ✅
- `runtime.*` 函数在 Task 1-3 定义，在 Task 7/11 使用 ✅
