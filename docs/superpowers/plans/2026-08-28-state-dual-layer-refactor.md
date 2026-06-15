# State 双层接口体系重构 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重构 State 体系为 Python 对齐的双层架构（底层 StateLike + 上层 SessionState），消除消费方类型断言。

**Architecture:** 底层 `ReadableStateLike → RecoverableStateLike → StateLike → CommitStateLike` 对应 Python 的 StateLike 体系；上层 `SessionState` 对应 Python 的 State 接口，提供 GetGlobal/UpdateGlobal/UpdateTrace/Dump。消费方通过 SessionState 多态调用，无需类型断言。

**Tech Stack:** Go 1.x，标准库 sync 包，项目内 state 包

**Spec:** `docs/superpowers/specs/2026-08-28-state-dual-layer-design.md`

---

## 文件结构

### 修改文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/session/state/state.go` | 接口定义：改名 + 新增 SessionState |
| `internal/agentcore/session/state/inmemory_state.go` | InMemoryState → InMemoryStateLike + SessionState 默认实现 |
| `internal/agentcore/session/state/inmemory_commit_state.go` | SessionState 默认实现 + 参数类型改名 |
| `internal/agentcore/session/state/agent_state_collection.go` | 补 traceState + SessionState 实现 + 字段类型改名 |
| `internal/agentcore/session/state/workflow_state_collection.go` | SessionState 实现 + 字段类型 CommitState→CommitStateLike |
| `internal/agentcore/session/state/workflow_commit_state.go` | Commit/Rollback 签名统一 + 删适配方法 + SessionState 实现 |
| `internal/agentcore/session/state/workflow_inmemory_state.go` | 参数类型 CommitState→CommitStateLike |
| `internal/agentcore/session/state/doc.go` | 更新文档 |
| `internal/agentcore/session/internal/agent_session.go` | State→SessionState |
| `internal/agentcore/session/internal/workflow_session.go` | State→SessionState + 消除类型断言 |
| `internal/agentcore/session/agent.go` | 消除类型断言 + CommitState→CommitStateLike |
| `internal/agentcore/session/node.go` | 消除类型断言 |
| `internal/agentcore/session/interaction/base.go` | 消除类型断言 + State→SessionState |
| `internal/agentcore/session/interaction/interaction.go` | 消除类型断言 |
| `internal/agentcore/session/state/state_test.go` | 改名 |
| `internal/agentcore/session/state/inmemory_state_test.go` | 改名 + 新增 SessionState 测试 |
| `internal/agentcore/session/state/inmemory_commit_state_test.go` | 改名 + 新增 SessionState 测试 |
| `internal/agentcore/session/state/agent_state_collection_test.go` | 改名 + 新增 UpdateTrace/Dump traceState 测试 |
| `internal/agentcore/session/state/workflow_state_collection_test.go` | CommitState→CommitStateLike |
| `internal/agentcore/session/state/workflow_commit_state_test.go` | Commit/Rollback 签名变化 + 适配 |
| `internal/agentcore/session/internal/agent_session_test.go` | State→SessionState |
| `internal/agentcore/session/agent_test.go` | 消除类型断言 |
| `internal/agentcore/session/node_test.go` | 消除类型断言 |
| `internal/agentcore/session/interaction/base_test.go` | 消除类型断言 |
| `internal/agentcore/session/interaction/interaction_test.go` | 消除类型断言 |
| `internal/agentcore/session/session_test.go` | InMemoryState→InMemoryStateLike |
| `internal/agentcore/session/doc.go` | 更新文档 |

---

### Task 1: 接口改名 + 新增 SessionState（state.go）

**Files:**
- Modify: `internal/agentcore/session/state/state.go`

- [ ] **Step 1: 修改 state.go — 接口改名 + 新增 SessionState**

将当前内容替换为：

```go
package state

// ──────────────────────────── 结构体 ────────────────────────────

// ReadableStateLike 只读状态访问接口
// 对应 Python: ReadableStateLike
type ReadableStateLike interface {
	// Get 根据 key 获取状态值
	Get(key StateKey) any
	// GetByPrefix 根据 key 和嵌套前缀获取状态值
	GetByPrefix(key StateKey, nestedPrefix string) any
}

// RecoverableStateLike 可恢复状态接口，支持快照保存和恢复
// 对应 Python: RecoverableStateLike
type RecoverableStateLike interface {
	// GetState 获取完整状态快照
	GetState() map[string]any
	// SetState 从快照恢复状态
	SetState(state map[string]any)
}

// StateLike 可读写状态接口，组合只读和可恢复能力
// 对应 Python: StateLike(ReadableStateLike, RecoverableStateLike)
type StateLike interface {
	ReadableStateLike
	RecoverableStateLike
	// Update 更新状态数据
	Update(data map[string]any) error
	// GetByTransformer 通过转换函数获取状态值
	GetByTransformer(transformer Transformer) any
}

// CommitStateLike 事务性状态接口，支持按节点 ID 的提交/回滚
// 对应 Python: CommitStateLike(StateLike)
type CommitStateLike interface {
	StateLike
	// UpdateByID 按节点 ID 暂存更新
	// nodeID 为空字符串时返回 error
	UpdateByID(nodeID string, data map[string]any) error
	// Commit 提交指定节点（或全部）的暂存更新
	// 不传 nodeID 则提交所有节点
	Commit(nodeID ...string)
	// Rollback 回滚指定节点的暂存更新
	Rollback(nodeID string)
	// GetUpdates 获取所有暂存更新
	GetUpdates() map[string][]map[string]any
	// SetUpdates 设置暂存更新
	SetUpdates(updates map[string][]map[string]any)
}

// SessionState 会话状态接口，面向会话调用方的统一抽象
// 对应 Python: State(RecoverableStateLike)
//
// 提供 GetGlobal/UpdateGlobal/UpdateTrace/Dump 等方法，
// 由 AgentStateCollection 和 WorkflowStateCollection 实现。
// 消费方通过此接口多态调用，无需类型断言。
type SessionState interface {
	RecoverableStateLike
	// GetGlobal 从全局状态获取值
	GetGlobal(key StateKey) any
	// UpdateGlobal 更新全局状态
	UpdateGlobal(data map[string]any)
	// UpdateTrace 更新追踪状态
	UpdateTrace(span any)
	// Update 更新状态数据
	Update(data map[string]any) error
	// Get 根据 key 获取状态值
	Get(key StateKey) any
	// Dump 导出完整状态快照
	Dump() map[string]any
}

// ──────────────────────────── 向后兼容别名 ────────────────────────────

// 以下别名保持向后兼容，后续版本移除
type ReadableState = ReadableStateLike
type RecoverableState = RecoverableStateLike
type State = StateLike
type CommitState = CommitStateLike

// ──────────────────────────── 枚举 ────────────────────────────

// Transformer 状态转换函数，接受只读状态视图返回任意值
type Transformer func(readable ReadableStateLike) any

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultNodeID 默认节点 ID
	DefaultNodeID = "default"
	// DefaultWorkflowID 默认工作流 ID
	DefaultWorkflowID = "workflow"
	// IOStateKey IO 状态键
	IOStateKey = "io_state"
	// GlobalStateKey 全局状态键
	GlobalStateKey = "global_state"
	// CompStateKey 组件状态键
	CompStateKey = "comp_state"
	// WorkflowStateKey 工作流状态键
	WorkflowStateKey = "workflow_state"
	// AgentStateKey Agent 状态键
	AgentStateKey = "agent_state"
	// IOStateUpdatesKey IO 状态更新键
	IOStateUpdatesKey = "io_state_updates"
	// GlobalStateUpdatesKey 全局状态更新键
	GlobalStateUpdatesKey = "global_state_updates"
	// CompStateUpdatesKey 组件状态更新键
	CompStateUpdatesKey = "comp_state_updates"
	// WorkflowStateUpdatesKey 工作流状态更新键
	WorkflowStateUpdatesKey = "workflow_state_updates"
)
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; go build ./internal/agentcore/session/state/...`

Expected: 编译通过（type alias 保证所有旧名仍可用）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/state.go
git commit -m "refactor(state): 接口改名加 Like 后缀 + 新增 SessionState 上层接口 + 向后兼容别名"
```

---

### Task 2: InMemoryState → InMemoryStateLike + SessionState 默认实现

**Files:**
- Modify: `internal/agentcore/session/state/inmemory_state.go`

- [ ] **Step 1: 修改 inmemory_state.go**

将结构体 `InMemoryState` 改名为 `InMemoryStateLike`，新增 `SessionState` 默认实现方法，保留 `InMemoryState` 类型别名：

```go
package state

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryStateLike StateLike 接口的内存实现
// 对应 Python: InMemoryStateLike(StateLike)
type InMemoryStateLike struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// state 内部状态存储
	state map[string]any
}

// ──────────────────────────── 向后兼容别名 ────────────────────────────

// InMemoryState 保持向后兼容，后续版本移除
type InMemoryState = InMemoryStateLike

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryStateLike 创建内存状态实例
func NewInMemoryStateLike() *InMemoryStateLike {
	return &InMemoryStateLike{
		state: make(map[string]any),
	}
}

// NewInMemoryState 保持向后兼容，后续版本移除
var NewInMemoryState = NewInMemoryStateLike

// ──────────────────────────── StateLike 接口实现 ────────────────────────────

// Get 根据 key 获取状态值（深拷贝返回）
func (s *InMemoryStateLike) Get(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyValue(getBySchema(key, s.state))
}

// GetByPrefix 根据 key 和嵌套前缀获取状态值（深拷贝返回）
func (s *InMemoryStateLike) GetByPrefix(key StateKey, nestedPrefix string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyValue(getBySchema(key, s.state, nestedPrefix))
}

// GetByTransformer 通过转换函数获取状态值
func (s *InMemoryStateLike) GetByTransformer(transformer Transformer) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return transformer(s)
}

// Update 更新状态数据（深拷贝输入）
func (s *InMemoryStateLike) Update(data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	updateDict(deepCopyMap(data), s.state)
	return nil
}

// GetState 获取完整状态快照（深拷贝返回）
func (s *InMemoryStateLike) GetState() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return deepCopyMap(s.state)
}

// SetState 从快照恢复状态
func (s *InMemoryStateLike) SetState(state map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state != nil {
		s.state = state
	}
}

// ──────────────────────────── SessionState 默认实现 ────────────────────────────

// GetGlobal 单存储单元无全局概念，返回 nil
func (s *InMemoryStateLike) GetGlobal(key StateKey) any { return nil }

// UpdateGlobal 单存储单元无全局概念，空操作
func (s *InMemoryStateLike) UpdateGlobal(data map[string]any) {}

// UpdateTrace 单存储单元无追踪概念，空操作
func (s *InMemoryStateLike) UpdateTrace(span any) {}

// Dump 导出完整状态，委托 GetState
func (s *InMemoryStateLike) Dump() map[string]any { return s.GetState() }
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/inmemory_state.go
git commit -m "refactor(state): InMemoryState→InMemoryStateLike + SessionState 默认实现 + 兼容别名"
```

---

### Task 3: InMemoryCommitState SessionState 默认实现 + 参数类型改名

**Files:**
- Modify: `internal/agentcore/session/state/inmemory_commit_state.go`

- [ ] **Step 1: 修改 inmemory_commit_state.go**

改动点：
1. 内部 `state` 字段类型从 `State` 改为 `StateLike`
2. `NewInMemoryCommitState` 参数类型从 `State` 改为 `StateLike`
3. 新增 4 个 SessionState 默认实现方法
4. Transformer 参数类型改为 `ReadableStateLike`（已在 Task 1 修改）

在文件末尾（`SetUpdates` 方法之后）追加：

```go

// ──────────────────────────── SessionState 默认实现 ────────────────────────────

// GetGlobal 单存储单元无全局概念，返回 nil
func (s *InMemoryCommitState) GetGlobal(key StateKey) any { return nil }

// UpdateGlobal 单存储单元无全局概念，空操作
func (s *InMemoryCommitState) UpdateGlobal(data map[string]any) {}

// UpdateTrace 单存储单元无追踪概念，空操作
func (s *InMemoryCommitState) UpdateTrace(span any) {}

// Dump 导出完整状态，委托 GetState
func (s *InMemoryCommitState) Dump() map[string]any { return s.GetState() }
```

同时将 `state State` 字段类型改为 `state StateLike`，`NewInMemoryCommitState(state ...StateLike)` 参数类型改为 `StateLike`。

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/inmemory_commit_state.go
git commit -m "refactor(state): InMemoryCommitState 参数类型 StateLike + SessionState 默认实现"
```

---

### Task 4: AgentStateCollection 补 traceState + SessionState 实现 + 字段类型改名

**Files:**
- Modify: `internal/agentcore/session/state/agent_state_collection.go`

- [ ] **Step 1: 修改 agent_state_collection.go**

改动点：
1. 字段类型 `*InMemoryState` → `*InMemoryStateLike`（通过 alias 兼容）
2. 新增 `traceState map[string]any` 字段
3. `NewAgentStateCollection` 初始化 `traceState: make(map[string]any)`
4. `UpdateTrace` 方法：空实现（与 Python 一致）
5. `Dump` 方法：增加 `trace_state` 键
6. `GlobalState()` 返回类型改为 `*InMemoryStateLike`
7. 删除 `State 接口实现` 分隔注释（方法已在 SessionState 中定义）
8. 确保 `Get` 方法签名与 `SessionState` 一致：`Get(key StateKey) any`（已有）

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/agent_state_collection.go
git commit -m "refactor(state): AgentStateCollection 补 traceState + SessionState 实现 + 字段类型改名"
```

---

### Task 5: WorkflowStateCollection SessionState 实现 + 字段类型改名

**Files:**
- Modify: `internal/agentcore/session/state/workflow_state_collection.go`

- [ ] **Step 1: 修改 workflow_state_collection.go**

改动点：
1. 4 个子状态字段类型 `CommitState` → `CommitStateLike`（通过 alias 兼容）
2. `NewWorkflowStateCollection` 参数类型 `CommitState` → `CommitStateLike`
3. 删除 `State 接口实现` 分隔注释
4. 所有方法已满足 SessionState 接口，无需新增方法

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/workflow_state_collection.go
git commit -m "refactor(state): WorkflowStateCollection 字段类型 CommitStateLike + SessionState 实现"
```

---

### Task 6: WorkflowCommitState Commit/Rollback 签名统一 + 删适配方法 + SessionState

**Files:**
- Modify: `internal/agentcore/session/state/workflow_commit_state.go`

- [ ] **Step 1: 修改 workflow_commit_state.go**

改动点：
1. `Commit()` 无参 → `Commit(nodeID ...string)` 可变参数
2. `Rollback()` 无参 → `Rollback(nodeID string)` 单参数
3. 删除 `CommitCommitState()` 和 `RollbackNode()` 方法
4. `CommitUserInputs` 中：`s.Commit()` → `s.Commit()`（不传参，语义不变）
5. `CommitUserInputs` 中：四个子状态 `Commit()` 改为 `Commit()`（不传参已是全部提交）
6. 删除 `CommitState 兼容方法` 分隔注释
7. 删除注释中关于"Go 接口方法签名冲突"的说明（已通过可变参数解决）
8. `NewWorkflowCommitState` 参数类型 `CommitState` → `CommitStateLike`
9. 所有子状态字段访问不变（通过嵌入的 `WorkflowStateCollection`）

关键代码变更：

```go
// Commit 提交全部（或指定节点）的四个子状态暂存更新。
// 不传 nodeID 则提交全部；传参则提交指定节点。
func (s *WorkflowCommitState) Commit(nodeID ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ioState.Commit(nodeID...)
	s.compState.Commit(nodeID...)
	s.globalState.Commit(nodeID...)
	s.workflowState.Commit(nodeID...)
}

// Rollback 回滚指定节点的四个子状态暂存更新。
func (s *WorkflowCommitState) Rollback(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.compState.Rollback(nodeID)
	s.ioState.Rollback(nodeID)
	s.globalState.Rollback(nodeID)
	s.workflowState.Rollback(nodeID)
}
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/workflow_commit_state.go
git commit -m "refactor(state): WorkflowCommitState Commit/Rollback 签名统一 + 删适配方法 + SessionState"
```

---

### Task 7: workflow_inmemory_state.go 参数类型改名

**Files:**
- Modify: `internal/agentcore/session/state/workflow_inmemory_state.go`

- [ ] **Step 1: 修改 workflow_inmemory_state.go**

将 `NewInMemoryWorkflowState(globalState ...CommitState)` 改为 `NewInMemoryWorkflowState(globalState ...CommitStateLike)`，内部变量 `gs` 类型从 `CommitState` 改为 `CommitStateLike`。

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/workflow_inmemory_state.go
git commit -m "refactor(state): NewInMemoryWorkflowState 参数类型 CommitStateLike"
```

---

### Task 8: state 包内编译验证 + 接口满足测试

**Files:**
- Modify: `internal/agentcore/session/state/state_test.go`

- [ ] **Step 1: 更新 state_test.go**

将接口满足测试改为新名，并新增 SessionState 接口满足测试：

```go
package state

import "testing"

// Test接口满足_ReadableStateLike 验证 InMemoryStateLike 满足 ReadableStateLike 接口。
func Test接口满足_ReadableStateLike(t *testing.T) {
	var _ ReadableStateLike = (*InMemoryStateLike)(nil)
}

// Test接口满足_RecoverableStateLike 验证 InMemoryStateLike 满足 RecoverableStateLike 接口。
func Test接口满足_RecoverableStateLike(t *testing.T) {
	var _ RecoverableStateLike = (*InMemoryStateLike)(nil)
}

// Test接口满足_StateLike 验证 InMemoryStateLike 满足 StateLike 接口。
func Test接口满足_StateLike(t *testing.T) {
	var _ StateLike = (*InMemoryStateLike)(nil)
}

// Test接口满足_CommitStateLike 验证 InMemoryCommitState 满足 CommitStateLike 接口。
func Test接口满足_CommitStateLike(t *testing.T) {
	var _ CommitStateLike = (*InMemoryCommitState)(nil)
}

// Test接口满足_SessionState_InMemoryStateLike 验证 InMemoryStateLike 满足 SessionState 接口。
func Test接口满足_SessionState_InMemoryStateLike(t *testing.T) {
	var _ SessionState = (*InMemoryStateLike)(nil)
}

// Test接口满足_SessionState_InMemoryCommitState 验证 InMemoryCommitState 满足 SessionState 接口。
func Test接口满足_SessionState_InMemoryCommitState(t *testing.T) {
	var _ SessionState = (*InMemoryCommitState)(nil)
}

// Test接口满足_SessionState_AgentStateCollection 验证 AgentStateCollection 满足 SessionState 接口。
func Test接口满足_SessionState_AgentStateCollection(t *testing.T) {
	var _ SessionState = (*AgentStateCollection)(nil)
}

// Test接口满足_SessionState_WorkflowStateCollection 验证 WorkflowStateCollection 满足 SessionState 接口。
func Test接口满足_SessionState_WorkflowStateCollection(t *testing.T) {
	var _ SessionState = (*WorkflowStateCollection)(nil)
}

// Test接口满足_SessionState_WorkflowCommitState 验证 WorkflowCommitState 满足 SessionState 接口。
func Test接口满足_SessionState_WorkflowCommitState(t *testing.T) {
	var _ SessionState = (*WorkflowCommitState)(nil)
}
```

- [ ] **Step 2: 运行 state 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/... -count=1`

Expected: 全部 PASS（通过 alias 旧测试名仍然可用，但会有 deprecation 警告无碍）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/state_test.go
git commit -m "test(state): 更新接口满足测试为 Like 后缀 + 新增 SessionState 接口满足测试"
```

---

### Task 9: state 包内测试文件改名适配

**Files:**
- Modify: `internal/agentcore/session/state/inmemory_state_test.go`
- Modify: `internal/agentcore/session/state/inmemory_commit_state_test.go`
- Modify: `internal/agentcore/session/state/agent_state_collection_test.go`
- Modify: `internal/agentcore/session/state/workflow_state_collection_test.go`
- Modify: `internal/agentcore/session/state/workflow_commit_state_test.go`

- [ ] **Step 1: 批量替换测试文件中的旧名**

在所有 `_test.go` 文件中执行替换：
- `NewInMemoryState()` → `NewInMemoryStateLike()`（但通过 alias 两者等价，实际可保持不变）
- `*InMemoryState` → `*InMemoryStateLike`
- `InMemoryState_` → `InMemoryStateLike_`（测试函数名）
- `NewInMemoryCommitState()` 不变
- `CommitState` → `CommitStateLike`（仅在 workflow_state_collection_test.go 和 workflow_commit_state_test.go）
- `cs.Rollback()` → `cs.Rollback(s.nodeID)` 或相应 nodeID（workflow_commit_state_test.go 中）
- `CommitCommitState` → 已删除，替换为 `Commit(nodeID...)`

**workflow_commit_state_test.go 中的 Rollback 测试**：
- `cs.Rollback()` → `cs.Rollback("node1")`（需要传入 nodeID）

- [ ] **Step 2: 运行 state 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/... -count=1`

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/inmemory_state_test.go internal/agentcore/session/state/inmemory_commit_state_test.go internal/agentcore/session/state/agent_state_collection_test.go internal/agentcore/session/state/workflow_state_collection_test.go internal/agentcore/session/state/workflow_commit_state_test.go
git commit -m "test(state): 测试文件适配 Like 后缀 + Rollback 签名变化"
```

---

### Task 10: internal 层 State() 返回类型 → SessionState

**Files:**
- Modify: `internal/agentcore/session/internal/agent_session.go`
- Modify: `internal/agentcore/session/internal/workflow_session.go`

- [ ] **Step 1: 修改 agent_session.go**

改动点：
1. `state` 字段类型 `state.State` → `state.SessionState`
2. `WithState` 参数类型 `state.State` → `state.SessionState`
3. `State()` 返回类型 `state.State` → `state.SessionState`
4. 默认初始化 `state.NewAgentStateCollection()` 不变（AgentStateCollection 已实现 SessionState）

- [ ] **Step 2: 修改 workflow_session.go**

改动点：
1. `baseSession` 接口中 `State() state.State` → `State() state.SessionState`
2. `st` 字段类型 `state.State` → `state.SessionState`
3. `nodeState` 字段类型 `state.State` → `state.SessionState`
4. `WithWorkflowState` 参数类型 `state.State` → `state.SessionState`
5. `State()` 返回类型 `state.State` → `state.SessionState`
6. `NewNodeSession` 和 `NewSubWorkflowSession` 中的局部变量 `nodeState state.State` → `state.SessionState`
7. **消除类型断言**：`if cs, ok := parent.State().(*state.WorkflowCommitState); ok { nodeState = cs.CreateNodeState(...) }` → 仍需保留此断言，因为 `CreateNodeState` 是 `WorkflowCommitState` 特有方法，不在 `SessionState` 接口中。但断言的 else 分支中 `nodeState = parent.State()` 不变。

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/internal/agent_session.go internal/agentcore/session/internal/workflow_session.go
git commit -m "refactor(session): internal 层 State() 返回类型 SessionState"
```

---

### Task 11: 消费方消除类型断言 — agent.go

**Files:**
- Modify: `internal/agentcore/session/agent.go`

- [ ] **Step 1: 修改 agent.go**

改动点：
1. `UpdateState`：`if coll, ok := s.inner.State().(*state.AgentStateCollection); ok { coll.UpdateGlobal(data) }` → `s.inner.State().UpdateGlobal(data)`
2. `GetState`：`if coll, ok := s.inner.State().(*state.AgentStateCollection); ok { return coll.GetGlobal(key), nil }` → `return s.inner.State().GetGlobal(key), nil`
3. `DumpState`：`if coll, ok := s.inner.State().(*state.AgentStateCollection); ok { return coll.Dump() }` → `return s.inner.State().Dump()`
4. `CreateWorkflowSession`：`*state.AgentStateCollection` 类型断言仍需保留，因为需要访问 `coll.GlobalState()` 获取底层 `*InMemoryStateLike` 引用（这不是 SessionState 接口方法，是 AgentStateCollection 特有方法）
5. `sharedGlobalState := state.NewInMemoryCommitState(coll.GlobalState())` 中 `CommitState` → `CommitStateLike`（通过 alias 已兼容）

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/agent.go
git commit -m "refactor(session): agent.go 消除 SessionState 接口可多态调用的类型断言"
```

---

### Task 12: 消费方消除类型断言 — node.go

**Files:**
- Modify: `internal/agentcore/session/node.go`

- [ ] **Step 1: 修改 node.go**

改动点：
1. `UpdateState`：`if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok { cs.Update(data) }` → `if err := f.inner.State().Update(data); err != nil { ... }`
2. `GetState`：`if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok { return cs.Get(key), nil }` → `return f.inner.State().Get(key), nil`
3. `UpdateGlobalState`：`if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok { cs.UpdateGlobal(data) }` → `f.inner.State().UpdateGlobal(data)`
4. `GetGlobalState`：`if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok { return cs.GetGlobal(key), nil }` → `return f.inner.State().GetGlobal(key), nil`
5. `DumpState`：`if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok { return cs.Dump() }` → `return f.inner.State().Dump()`

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/node.go
git commit -m "refactor(session): node.go 消除 SessionState 接口可多态调用的类型断言"
```

---

### Task 13: 消费方消除类型断言 — interaction/base.go

**Files:**
- Modify: `internal/agentcore/session/interaction/base.go`

- [ ] **Step 1: 修改 base.go**

改动点：
1. `baseSession` 接口中 `State() state.State` → `State() state.SessionState`
2. `initInteractiveInputs` 中 4 处 `if cs, ok := st.(*state.WorkflowCommitState); ok { cs.GetGlobal(...) }` → `st.GetGlobal(...)`
3. `initInteractiveInputs` 中 2 处 `cs.UpdateGlobal(...)` → `st.UpdateGlobal(...)`
4. `commitCMP` 中 `if cs, ok := st.(*state.WorkflowCommitState); ok { cs.CommitCmp() }` → 仍需保留断言，因为 `CommitCmp()` 是 `WorkflowStateCollection` 特有方法，不在 `SessionState` 接口中

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interaction/base.go
git commit -m "refactor(interaction): base.go 消除 GetGlobal/UpdateGlobal 类型断言 + SessionState 接口"
```

---

### Task 14: 消费方消除类型断言 — interaction/interaction.go

**Files:**
- Modify: `internal/agentcore/session/interaction/interaction.go`

- [ ] **Step 1: 修改 interaction.go**

改动点：
1. `NewWorkflowInteraction` 中 `if st, ok := session.State().(*state.WorkflowCommitState); ok { ... }` → 仍需保留断言，因为 `GetWorkflowState` 和 `UpdateAndCommitWorkflowState` 是 `WorkflowCommitState` 特有方法，不在 `SessionState` 接口中

**注意**：此处无法消除类型断言，因为 `GetWorkflowState`/`UpdateAndCommitWorkflowState` 是工作流专属操作。保持现状即可，无需修改。

- [ ] **Step 2: 提交（如无改动则跳过）**

如果 `interaction.go` 中的 `session.State()` 返回类型已通过 Task 10 变为 `SessionState`，断言方式不变，无需额外修改。

---

### Task 15: 外部测试文件适配

**Files:**
- Modify: `internal/agentcore/session/agent_test.go`
- Modify: `internal/agentcore/session/node_test.go`
- Modify: `internal/agentcore/session/interaction/base_test.go`
- Modify: `internal/agentcore/session/interaction/interaction_test.go`
- Modify: `internal/agentcore/session/session_test.go`
- Modify: `internal/agentcore/session/internal/agent_session_test.go`
- Modify: `internal/agentcore/session/internal/workflow_session_test.go`

- [ ] **Step 1: 批量替换外部测试文件**

改动点：
- `state.NewInMemoryState()` → `state.NewInMemoryStateLike()`（或保持旧名通过 alias）
- `*state.WorkflowCommitState` 类型断言中的 `GetGlobal/UpdateGlobal/Get/Dump` 调用改为通过 `SessionState` 接口调用（无需断言）
- `*state.WorkflowCommitState` 类型断言中的 `GetWorkflowState/UpdateAndCommitWorkflowState/CreateNodeState/CommitCmp` 保持断言（特有方法）
- `cs.Rollback()` → `cs.Rollback(nodeID)` 适配
- `CommitCommitState(...)` → `Commit(...)` 适配

- [ ] **Step 2: 运行完整测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -count=1`

Expected: 全部 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/agent_test.go internal/agentcore/session/node_test.go internal/agentcore/session/interaction/base_test.go internal/agentcore/session/interaction/interaction_test.go internal/agentcore/session/session_test.go internal/agentcore/session/internal/agent_session_test.go internal/agentcore/session/internal/workflow_session_test.go
git commit -m "test(session): 外部测试文件适配 Like 后缀 + 消除类型断言 + Rollback 签名变化"
```

---

### Task 16: 更新 doc.go 文档

**Files:**
- Modify: `internal/agentcore/session/state/doc.go`
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 更新 state/doc.go**

更新包文档，反映双层接口体系：
- 底层：ReadableStateLike → RecoverableStateLike → StateLike → CommitStateLike
- 上层：SessionState（面向会话调用方）
- 向后兼容别名

- [ ] **Step 2: 更新 session/doc.go**

更新引用的接口名称（State → StateLike/SessionState 等）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/state/doc.go internal/agentcore/session/doc.go
git commit -m "docs(state): 更新 doc.go 反映双层接口体系"
```

---

### Task 17: 全量编译 + 全量测试

**Files:**
- 无新增修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

Expected: 编译通过

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/... -count=1 -timeout 120s`

Expected: 全部 PASS

- [ ] **Step 3: 最终提交**

如有遗漏修复：
```bash
git add -A
git commit -m "fix: State 双层接口体系重构遗漏修复"
```
