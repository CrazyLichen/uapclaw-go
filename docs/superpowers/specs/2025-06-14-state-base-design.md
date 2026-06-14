# State 基础接口体系设计

> 对应实现计划 5.1 小节 — State 基础接口层
> Python 源码：`openjiuwen/core/session/state/base.py` + `openjiuwen/core/session/utils.py`

## 1. 概述

本设计实现 State 体系的 4 层接口及其内存实现，是 Agent Session 和 Workflow Session 共享的状态基础设施。后续 Agent State（5.x）和 Workflow State（5.x）均基于本层接口构建。

## 2. 接口层次

```
ReadableState          只读访问
    └─ RecoverableState    快照恢复
        └─ State           可读写
            └─ CommitState 事务性提交/回滚
```

### 2.1 ReadableState（只读状态访问）

```go
type ReadableState interface {
    Get(key StateKey) any
    GetByPrefix(key StateKey, nestedPrefix string) any
}
```

### 2.2 RecoverableState（可恢复状态）

```go
type RecoverableState interface {
    GetState() map[string]any
    SetState(state map[string]any)
}
```

### 2.3 State（可读写状态，组合只读 + 可恢复）

```go
type Transformer func(readable ReadableState) any

type State interface {
    ReadableState
    RecoverableState
    Update(data map[string]any) error
    GetByTransformer(transformer Transformer) any
}
```

### 2.4 CommitState（事务性状态，支持按节点 ID 的提交/回滚）

```go
type CommitState interface {
    State
    UpdateByID(nodeID string, data map[string]any)
    Commit(nodeID ...string)
    Rollback(nodeID string)
    GetUpdates() map[string][]map[string]any
    SetUpdates(updates map[string][]map[string]any)
}
```

## 3. StateKey 自定义类型

封装 Python 中 `Union[str, list, dict]` 三态 key。

```go
type StateKeyType int

const (
    StateKeyString StateKeyType = iota
    StateKeyMap
    StateKeyList
)

type StateKey struct {
    keyType StateKeyType
    value   any  // 存储 string / map[string]any / []any
}

func StringKey(path string) StateKey            // 字符串路径，如 "a.b.c" 或 "${ref.path}"
func SchemaKey(schema map[string]any) StateKey  // map schema 批量读取
func ListKey(keys []any) StateKey               // list schema 批量读取

func (k StateKey) Type() StateKeyType
func (k StateKey) String() string
func (k StateKey) Map() map[string]any
func (k StateKey) List() []any
```

构造函数内部对 map/slice 做 deepCopy，防止外部修改。

## 4. InMemoryState

State 接口的内存实现，对应 Python 的 `InMemoryStateLike`。

```go
type InMemoryState struct {
    state map[string]any
}

func NewInMemoryState() *InMemoryState
func (s *InMemoryState) Get(key StateKey) any
func (s *InMemoryState) GetByPrefix(key StateKey, nestedPrefix string) any
func (s *InMemoryState) GetByTransformer(transformer Transformer) any
func (s *InMemoryState) Update(data map[string]any)
func (s *InMemoryState) GetState() map[string]any
func (s *InMemoryState) SetState(state map[string]any)
```

实现逻辑：
- `Get` → `getBySchema(key, s.state)` + `deepCopyValue` 结果
- `GetByPrefix` → `getBySchema(key, s.state, prefix)` + `deepCopyValue` 结果
- `GetByTransformer` → `transformer(s)` 直接传 self（Transformer 接收 ReadableState，无法修改）
- `Update` → `updateDict(deepCopyMap(data), s.state)` + return nil
- `GetState` → `deepCopyMap(s.state)`
- `SetState` → `s.state = state`（直接替换引用）

## 5. InMemoryCommitState

CommitState 接口的内存实现，对应 Python 的 `InMemoryCommitState`。

```go
type InMemoryCommitState struct {
    state   State                        // 底层状态（默认 InMemoryState）
    updates map[string][]map[string]any   // 按 nodeID 缓存的待提交更新
}

func NewInMemoryCommitState(state ...State) *InMemoryCommitState
```

### 方法

| 方法 | 行为 |
|------|------|
| `Get`/`GetByPrefix`/`GetByTransformer`/`GetState`/`SetState` | 委托给底层 `state` |
| `Update(data)` | 返回 error，禁止直接调用，必须使用 `UpdateByID` |
| `UpdateByID(nodeID, data)` | deepcopy data 后 append 到 `updates[nodeID]` |
| `Commit(nodeID ...string)` | 遍历 updates 调用 `state.Update(update)`，然后清空；不传 nodeID 则提交全部 |
| `Rollback(nodeID)` | 清空 `updates[nodeID]` |
| `GetUpdates()` | 返回 `updates` |
| `SetUpdates(updates)` | 替换 `updates`（非 nil 时） |

## 6. utils.go 工具函数

### 6.1 深拷贝

```go
func deepCopyMap(src map[string]any) map[string]any
func deepCopySlice(src []any) []any
func deepCopyValue(val any) any
```

手动递归实现，只支持 `map[string]any` / `[]any` / 原始值（string/int/float/bool/nil）。

### 6.2 嵌套路径解析

```go
func splitNestedPath(nestedKey string) []any
// 例: "a_1.b.c[1].d" → ["a_1", "b", "c", 1, "d"]

func isRefPath(path string) bool
// 判断 "${start123.p2}" 风格引用路径

func extractOriginKey(key string) string
// "${start123.p2}" → "start123.p2"
```

### 6.3 状态读写

```go
func updateDict(update map[string]any, source map[string]any)
// 用 update 更新 source，支持嵌套路径 key，value 为 nil 时删除

func getBySchema(schema StateKey, data map[string]any, nestedPath ...string) any
// 根据 schema 类型分发读取

func getValueByNestedPath(nestedKey string, source map[string]any) any
// "a.b[0].c" → source["a"]["b"][0]["c"]

func rootToPath(nestedPath string, source map[string]any, createIfAbsent ...bool) (any, map[string]any)
// 沿路径导航到最终容器，createIfAbsent 为 true 时自动创建中间节点

func expandNestedStructure(data any) any
// {"a.b": 1} → {"a": {"b": 1}}
```

### 6.4 辅助函数

```go
func updateByKey(key any, newValue any, source map[string]any)
func deleteByKey(key any, source map[string]any)
```

## 7. 常量

```go
const (
    DefaultNodeID     = "default"
    DefaultWorkflowID = "workflow"
    IOStateKey        = "io_state"
    GlobalStateKey    = "global_state"
    CompStateKey      = "comp_state"
    WorkflowStateKey  = "workflow_state"
    AgentStateKey     = "agent_state"
    IOStateUpdatesKey    = "io_state_updates"
    GlobalStateUpdatesKey   = "global_state_updates"
    CompStateUpdatesKey     = "comp_state_updates"
    WorkflowStateUpdatesKey = "workflow_state_updates"
)
```

## 8. 文件结构

```
internal/agentcore/session/state/
├── doc.go                     # 包文档
├── key.go                     # StateKey 类型 + StateKeyType 枚举 + 构造函数
├── state.go                   # 4 层接口 + Transformer 类型 + 常量
├── inmemory_state.go          # InMemoryState 实现
├── inmemory_commit_state.go   # InMemoryCommitState 实现
└── utils.go                   # 深拷贝 / 嵌套路径解析 / 状态读写工具函数
```

## 9. 与 Python 对照

| Python | Go |
|--------|-----|
| `ReadableStateLike` | `ReadableState` |
| `RecoverableStateLike` | `RecoverableState` |
| `StateLike` | `State` |
| `CommitStateLike` | `CommitState` |
| `InMemoryStateLike` | `InMemoryState` |
| `InMemoryCommitState` | `InMemoryCommitState` |
| `Transformer = Callable[[ReadableStateLike], Any]` | `Transformer func(ReadableState) any` |
| `Union[str, list, dict]` | `StateKey` (StringKey/SchemaKey/ListKey) |
| `deepcopy` | `deepCopyMap`/`deepCopySlice`/`deepCopyValue` |
| `update_dict` | `updateDict` |
| `get_by_schema` | `getBySchema` |
| `raise build_error(...)` | `return error` |
| `node_id=None` | `nodeID ...string` 可变参数 |

## 10. 不在本步骤实现的内容（⤵️ 回填点）

以下内容在后续实现时需要回填到本包：

- ⤵️ `agent_state.py` 的 `StateCollection`（Agent State 分支：global+agent 两层，对应 5.x Agent Session）
- ⤵️ `workflow_state.py` 的 `StateCollection` / `CommitState` / `InMemoryState`（Workflow State 分支：io/global/comp/workflow 四层 + commit/rollback，对应 5.x Workflow Session）
- ⤵️ `session_controller/` 的 scope 和 controller（状态作用域管理）
- ⤵️ `checkpointer/` 持久化（状态快照存储与恢复）

回填时需在本包新增文件，并在 `doc.go` 中同步更新文件目录。
