# State 基础接口体系实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 State 体系的 4 层接口（ReadableState/RecoverableState/State/CommitState）及其内存实现，为后续 Agent/Workflow Session 提供状态基础设施。

**Architecture:** 1:1 对齐 Python `openjiuwen/core/session/state/base.py` 的接口层次，用 Go 接口组合替代 Python 多继承。StateKey 封装 string/map/slice 三态 key，utils.go 提供嵌套路径解析和字典操作工具函数。

**Tech Stack:** Go 标准库，无第三方依赖

**Spec:** `docs/superpowers/specs/2025-06-14-state-base-design.md`

**Python 参考：**
- `openjiuwen/core/session/state/base.py` — 接口定义 + InMemoryStateLike + InMemoryCommitState
- `openjiuwen/core/session/utils.py` — update_dict / get_by_schema / split_nested_path 等

---

## 文件结构

```
internal/agentcore/session/state/
├── doc.go                     # 包文档
├── key.go                     # StateKey 类型 + StateKeyType 枚举 + 构造函数
├── state.go                   # 4 层接口 + Transformer 类型 + 常量
├── inmemory_state.go          # InMemoryState 实现 State 接口
├── inmemory_commit_state.go   # InMemoryCommitState 实现 CommitState 接口
└── utils.go                   # 深拷贝 / 嵌套路径解析 / 状态读写工具函数

测试文件：
internal/agentcore/session/state/
├── key_test.go
├── state_test.go              # 接口满足验证
├── inmemory_state_test.go
├── inmemory_commit_state_test.go
└── utils_test.go
```

---

### Task 1: 创建包目录 + doc.go

**Files:**
- Create: `internal/agentcore/session/state/doc.go`

- [ ] **Step 1: 创建目录并编写 doc.go**

```go
// Package state 提供会话状态管理的抽象接口定义和内存实现。
//
// 本包定义了 4 层接口层次：ReadableState（只读访问）→ RecoverableState（快照恢复）
// → State（可读写）→ CommitState（事务性提交/回滚），以及 StateKey 类型用于封装
// string/map/slice 三态访问键。InMemoryState 和 InMemoryCommitState 提供基于内存的实现。
//
// 本包是 Agent Session 和 Workflow Session 共享的状态基础设施。后续 Agent State
// 和 Workflow State 的 StateCollection 均基于本层接口构建。
//
// 文件目录：
//
//	state/
//	├── doc.go                     # 包文档
//	├── key.go                     # StateKey 类型 + StateKeyType 枚举 + 构造函数
//	├── state.go                   # 4 层接口 + Transformer 类型 + 常量
//	├── inmemory_state.go          # InMemoryState 实现 State 接口
//	├── inmemory_commit_state.go   # InMemoryCommitState 实现 CommitState 接口
//	└── utils.go                   # 深拷贝 / 嵌套路径解析 / 状态读写工具函数
//
// 对应 Python 代码：openjiuwen/core/session/state/base.py + openjiuwen/core/session/utils.py
//
// 核心类型/接口索引：
//
//	ReadableState        — 只读状态访问接口
//	RecoverableState     — 可恢复状态接口，支持快照保存和恢复
//	State                — 可读写状态接口，组合只读和可恢复能力
//	CommitState          — 事务性状态接口，支持按节点 ID 的提交/回滚
//	StateKey             — 状态访问键，封装 string/map/slice 三态
//	InMemoryState        — State 接口的内存实现
//	InMemoryCommitState  — CommitState 接口的内存实现
package state
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/state/`
Expected: 编译成功（无输出）

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/session/state/doc.go
git commit -m "feat(session): add state package doc.go"
```

---

### Task 2: 实现 StateKey 类型

**Files:**
- Create: `internal/agentcore/session/state/key.go`
- Create: `internal/agentcore/session/state/key_test.go`

- [ ] **Step 1: 编写 key_test.go 失败测试**

```go
package state

import (
	"reflect"
	"testing"
)

// TestStringKey_基本 验证 StringKey 创建的 StateKey 类型正确、值可获取。
func TestStringKey_基本(t *testing.T) {
	k := StringKey("a.b.c")
	if k.Type() != StateKeyString {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
	if k.String() != "a.b.c" {
		t.Errorf("String() = %q, 期望 %q", k.String(), "a.b.c")
	}
}

// TestSchemaKey_基本 验证 SchemaKey 创建的 StateKey 类型正确、值可获取。
func TestSchemaKey_基本(t *testing.T) {
	schema := map[string]any{"name": "user.name", "age": "user.age"}
	k := SchemaKey(schema)
	if k.Type() != StateKeyMap {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyMap)
	}
	m := k.Map()
	if m["name"] != "user.name" {
		t.Errorf("Map()[\"name\"] = %v, 期望 %v", m["name"], "user.name")
	}
}

// TestListKey_基本 验证 ListKey 创建的 StateKey 类型正确、值可获取。
func TestListKey_基本(t *testing.T) {
	keys := []any{"user.name", "user.age"}
	k := ListKey(keys)
	if k.Type() != StateKeyList {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyList)
	}
	l := k.List()
	if l[0] != "user.name" {
		t.Errorf("List()[0] = %v, 期望 %v", l[0], "user.name")
	}
}

// TestSchemaKey_深拷贝 验证 SchemaKey 构造时做了深拷贝，外部修改不影响 StateKey。
func TestSchemaKey_深拷贝(t *testing.T) {
	schema := map[string]any{"key": "value"}
	k := SchemaKey(schema)
	schema["key"] = "changed"
	m := k.Map()
	if m["key"] != "value" {
		t.Errorf("外部修改后 Map()[\"key\"] = %v, 期望 %v（应深拷贝隔离）", m["key"], "value")
	}
}

// TestListKey_深拷贝 验证 ListKey 构造时做了深拷贝，外部修改不影响 StateKey。
func TestListKey_深拷贝(t *testing.T) {
	keys := []any{"a", "b"}
	k := ListKey(keys)
	keys[0] = "changed"
	l := k.List()
	if l[0] != "a" {
		t.Errorf("外部修改后 List()[0] = %v, 期望 %v（应深拷贝隔离）", l[0], "a")
	}
}

// TestStringKey_引用路径 验证引用路径风格的字符串 key。
func TestStringKey_引用路径(t *testing.T) {
	k := StringKey("${start123.p2}")
	if k.Type() != StateKeyString {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
	if k.String() != "${start123.p2}" {
		t.Errorf("String() = %q, 期望 %q", k.String(), "${start123.p2}")
	}
}

// TestSchemaKey_返回深拷贝 验证 Map() 返回的是深拷贝，修改返回值不影响内部。
func TestSchemaKey_返回深拷贝(t *testing.T) {
	k := SchemaKey(map[string]any{"key": "value"})
	m := k.Map()
	m["key"] = "changed"
	m2 := k.Map()
	if m2["key"] != "value" {
		t.Errorf("修改返回值后再次 Map()[\"key\"] = %v, 期望 %v（返回值应深拷贝）", m2["key"], "value")
	}
}

// TestListKey_返回深拷贝 验证 List() 返回的是深拷贝，修改返回值不影响内部。
func TestListKey_返回深拷贝(t *testing.T) {
	k := ListKey([]any{"a", "b"})
	l := k.List()
	l[0] = "changed"
	l2 := k.List()
	if l2[0] != "a" {
		t.Errorf("修改返回值后再次 List()[0] = %v, 期望 %v（返回值应深拷贝）", l2[0], "a")
	}
}

// TestStateKey_零值 验证零值 StateKey 的 Type 为默认值。
func TestStateKey_零值(t *testing.T) {
	var k StateKey
	if k.Type() != StateKeyString {
		t.Errorf("零值 StateKey Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
}

// TestStateKey_嵌套深拷贝 验证 SchemaKey 对嵌套 map 做了深拷贝。
func TestStateKey_嵌套深拷贝(t *testing.T) {
	schema := map[string]any{
		"user": map[string]any{"name": "user.name"},
	}
	k := SchemaKey(schema)
	nested := schema["user"].(map[string]any)
	nested["name"] = "changed"
	m := k.Map()
	if m["user"].(map[string]any)["name"] != "user.name" {
		t.Errorf("嵌套 map 未深拷贝，修改后 = %v", m["user"].(map[string]any)["name"])
	}
}

// TestStateKey_列表嵌套深拷贝 验证 ListKey 对嵌套 map 做了深拷贝。
func TestStateKey_列表嵌套深拷贝(t *testing.T) {
	keys := []any{
		map[string]any{"key": "value"},
	}
	k := ListKey(keys)
	keys[0].(map[string]any)["key"] = "changed"
	l := k.List()
	if l[0].(map[string]any)["key"] != "value" {
		t.Errorf("嵌套 map 未深拷贝，修改后 = %v", l[0].(map[string]any)["key"])
	}
}

// ──── 构造函数类型安全测试 ────

// TestStringKey_空字符串 验证空字符串 key 正常创建。
func TestStringKey_空字符串(t *testing.T) {
	k := StringKey("")
	if k.Type() != StateKeyString {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyString)
	}
	if k.String() != "" {
		t.Errorf("String() = %q, 期望空字符串", k.String())
	}
}

// TestSchemaKey_空map 验证空 map schema 正常创建。
func TestSchemaKey_空map(t *testing.T) {
	k := SchemaKey(map[string]any{})
	if k.Type() != StateKeyMap {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyMap)
	}
	if len(k.Map()) != 0 {
		t.Errorf("Map() 长度 = %d, 期望 0", len(k.Map()))
	}
}

// TestListKey_空slice 验证空 slice schema 正常创建。
func TestListKey_空slice(t *testing.T) {
	k := ListKey([]any{})
	if k.Type() != StateKeyList {
		t.Errorf("Type() = %v, 期望 %v", k.Type(), StateKeyList)
	}
	if len(k.List()) != 0 {
		t.Errorf("List() 长度 = %d, 期望 0", len(k.List()))
	}
}

// ──── 类型不匹配访问测试 ────

// TestStateKey_类型不匹配访问 验证用错误访问方法获取值时返回零值。
func TestStateKey_类型不匹配访问(t *testing.T) {
	k := StringKey("a.b.c")
	// StringKey 上调用 Map() 应返回 nil
	if m := k.Map(); m != nil {
		t.Errorf("StringKey.Map() = %v, 期望 nil", m)
	}
	// StringKey 上调用 List() 应返回 nil
	if l := k.List(); l != nil {
		t.Errorf("StringKey.List() = %v, 期望 nil", l)
	}

	schemaKey := SchemaKey(map[string]any{"key": "val"})
	if s := schemaKey.String(); s != "" {
		t.Errorf("SchemaKey.String() = %q, 期望空字符串", s)
	}
	if l := schemaKey.List(); l != nil {
		t.Errorf("SchemaKey.List() = %v, 期望 nil", l)
	}

	listKey := ListKey([]any{"a"})
	if s := listKey.String(); s != "" {
		t.Errorf("ListKey.String() = %q, 期望空字符串", s)
	}
	if m := listKey.Map(); m != nil {
		t.Errorf("ListKey.Map() = %v, 期望 nil", m)
	}
}

// ──── 反射验证 ────

// TestStateKey_类型校验 验证 StateKey 内部 value 字段存储的类型正确。
func TestStateKey_类型校验(t *testing.T) {
	sk := StringKey("test")
	if reflect.TypeOf(sk.value) != reflect.TypeOf("") {
		t.Errorf("StringKey 内部 value 类型 = %T, 期望 string", sk.value)
	}

	mk := SchemaKey(map[string]any{"k": "v"})
	if reflect.TypeOf(mk.value) != reflect.TypeOf(map[string]any{}) {
		t.Errorf("SchemaKey 内部 value 类型 = %T, 期望 map[string]any", mk.value)
	}

	lk := ListKey([]any{"a"})
	if reflect.TypeOf(lk.value) != reflect.TypeOf([]any{}) {
		t.Errorf("ListKey 内部 value 类型 = %T, 期望 []any", lk.value)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run TestStringKey -v`
Expected: FAIL（编译错误，StringKey 等未定义）

- [ ] **Step 3: 编写 key.go 实现**

```go
package state

// ──────────────────────────── 枚举 ────────────────────────────

// StateKeyType 标识 StateKey 的类型
type StateKeyType int

const (
	// StateKeyString 字符串路径类型
	StateKeyString StateKeyType = iota
	// StateKeyMap map schema 类型
	StateKeyMap
	// StateKeyList list schema 类型
	StateKeyList
)

// ──────────────────────────── 结构体 ────────────────────────────

// StateKey 状态访问键，封装 string/map/slice 三态
// 内部用 value 字段存储实际值，keyType 标识具体类型
type StateKey struct {
	keyType StateKeyType
	value   any // 存储 string / map[string]any / []any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// StringKey 创建字符串路径键，如 "a.b.c" 或 "${ref.path}"
func StringKey(path string) StateKey {
	return StateKey{keyType: StateKeyString, value: path}
}

// SchemaKey 创建 map schema 键，用于批量按 schema 读取
// 构造时深拷贝传入的 map，防止外部修改
func SchemaKey(schema map[string]any) StateKey {
	return StateKey{keyType: StateKeyMap, value: deepCopyMap(schema)}
}

// ListKey 创建 list schema 键，用于按列表 schema 读取
// 构造时深拷贝传入的 slice，防止外部修改
func ListKey(keys []any) StateKey {
	return StateKey{keyType: StateKeyList, value: deepCopySlice(keys)}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 此文件无非导出函数，deepCopyMap/deepCopySlice 在 utils.go 中定义

// ──────────────────────────── StateKey 方法 ────────────────────────────

// Type 返回 StateKey 的类型
func (k StateKey) Type() StateKeyType {
	return k.keyType
}

// String 返回字符串值，仅当 Type 为 StateKeyString 时有效，否则返回空字符串
func (k StateKey) String() string {
	if k.keyType == StateKeyString {
		return k.value.(string)
	}
	return ""
}

// Map 返回 map 值的深拷贝，仅当 Type 为 StateKeyMap 时有效，否则返回 nil
func (k StateKey) Map() map[string]any {
	if k.keyType == StateKeyMap {
		return deepCopyMap(k.value.(map[string]any))
	}
	return nil
}

// List 返回 slice 值的深拷贝，仅当 Type 为 StateKeyList 时有效，否则返回 nil
func (k StateKey) List() []any {
	if k.keyType == StateKeyList {
		return deepCopySlice(k.value.([]any))
	}
	return nil
}
```

**注意**：`key.go` 依赖 `utils.go` 中的 `deepCopyMap` 和 `deepCopySlice`。为了编译通过，需要先创建一个最小化的 `utils.go`：

- [ ] **Step 4: 创建最小化 utils.go（仅 deepCopy 函数）**

```go
package state

// ──────────────────────────── 深拷贝 ────────────────────────────

// deepCopyMap 深拷贝 map[string]any
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

// deepCopySlice 深拷贝 []any
func deepCopySlice(src []any) []any {
	if src == nil {
		return nil
	}
	dst := make([]any, len(src))
	for i, v := range src {
		dst[i] = deepCopyValue(v)
	}
	return dst
}

// deepCopyValue 深拷贝任意值（map/slice/原始值）
func deepCopyValue(val any) any {
	switch v := val.(type) {
	case map[string]any:
		return deepCopyMap(v)
	case []any:
		return deepCopySlice(v)
	default:
		return v // string/int/float/bool/nil 等原始值直接返回
	}
}
```

- [ ] **Step 5: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/session/state/key.go internal/agentcore/session/state/key_test.go internal/agentcore/session/state/utils.go
git commit -m "feat(session): add StateKey type with StringKey/SchemaKey/ListKey constructors"
```

---

### Task 3: 实现 4 层接口定义 + 常量

**Files:**
- Create: `internal/agentcore/session/state/state.go`
- Create: `internal/agentcore/session/state/state_test.go`

- [ ] **Step 1: 编写 state_test.go 失败测试（接口满足验证）**

```go
package state

import "testing"

// Test接口满足_ReadableState 验证 InMemoryState 满足 ReadableState 接口。
func Test接口满足_ReadableState(t *testing.T) {
	var _ ReadableState = (*InMemoryState)(nil)
}

// Test接口满足_RecoverableState 验证 InMemoryState 满足 RecoverableState 接口。
func Test接口满足_RecoverableState(t *testing.T) {
	var _ RecoverableState = (*InMemoryState)(nil)
}

// Test接口满足_State 验证 InMemoryState 满足 State 接口。
func Test接口满足_State(t *testing.T) {
	var _ State = (*InMemoryState)(nil)
}

// Test接口满足_CommitState 验证 InMemoryCommitState 满足 CommitState 接口。
func Test接口满足_CommitState(t *testing.T) {
	var _ CommitState = (*InMemoryCommitState)(nil)
}

// Test常量值 验证常量值与 Python 一致。
func Test常量值(t *testing.T) {
	if DefaultNodeID != "default" {
		t.Errorf("DefaultNodeID = %q, 期望 %q", DefaultNodeID, "default")
	}
	if DefaultWorkflowID != "workflow" {
		t.Errorf("DefaultWorkflowID = %q, 期望 %q", DefaultWorkflowID, "workflow")
	}
	if IOStateKey != "io_state" {
		t.Errorf("IOStateKey = %q, 期望 %q", IOStateKey, "io_state")
	}
	if GlobalStateKey != "global_state" {
		t.Errorf("GlobalStateKey = %q, 期望 %q", GlobalStateKey, "global_state")
	}
	if CompStateKey != "comp_state" {
		t.Errorf("CompStateKey = %q, 期望 %q", CompStateKey, "comp_state")
	}
	if WorkflowStateKey != "workflow_state" {
		t.Errorf("WorkflowStateKey = %q, 期望 %q", WorkflowStateKey, "workflow_state")
	}
	if AgentStateKey != "agent_state" {
		t.Errorf("AgentStateKey = %q, 期望 %q", AgentStateKey, "agent_state")
	}
	if IOStateUpdatesKey != "io_state_updates" {
		t.Errorf("IOStateUpdatesKey = %q, 期望 %q", IOStateUpdatesKey, "io_state_updates")
	}
	if GlobalStateUpdatesKey != "global_state_updates" {
		t.Errorf("GlobalStateUpdatesKey = %q, 期望 %q", GlobalStateUpdatesKey, "global_state_updates")
	}
	if CompStateUpdatesKey != "comp_state_updates" {
		t.Errorf("CompStateUpdatesKey = %q, 期望 %q", CompStateUpdatesKey, "comp_state_updates")
	}
	if WorkflowStateUpdatesKey != "workflow_state_updates" {
		t.Errorf("WorkflowStateUpdatesKey = %q, 期望 %q", WorkflowStateUpdatesKey, "workflow_state_updates")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run Test接口满足 -v`
Expected: FAIL（ReadableState 等未定义）

- [ ] **Step 3: 编写 state.go**

```go
package state

// ──────────────────────────── 结构体 ────────────────────────────

// （StateKey 在 key.go 中定义）

// ──────────────────────────── 接口 ────────────────────────────

// ReadableState 只读状态访问接口
type ReadableState interface {
	// Get 根据 key 获取状态值
	Get(key StateKey) any
	// GetByPrefix 根据 key 和嵌套前缀获取状态值
	GetByPrefix(key StateKey, nestedPrefix string) any
}

// RecoverableState 可恢复状态接口，支持快照保存和恢复
type RecoverableState interface {
	// GetState 获取完整状态快照
	GetState() map[string]any
	// SetState 从快照恢复状态
	SetState(state map[string]any)
}

// State 可读写状态接口，组合只读和可恢复能力
type State interface {
	ReadableState
	RecoverableState
	// Update 更新状态数据
	Update(data map[string]any) error
	// GetByTransformer 通过转换函数获取状态值
	GetByTransformer(transformer Transformer) any
}

// CommitState 事务性状态接口，支持按节点 ID 的提交/回滚
type CommitState interface {
	State
	// UpdateByID 按节点 ID 暂存更新
	UpdateByID(nodeID string, data map[string]any)
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

// ──────────────────────────── 全局变量 ────────────────────────────

// （暂无）

// ──────────────────────────── 导出函数 ────────────────────────────

// （暂无）

// ──────────────────────────── 非导出函数 ────────────────────────────

// （暂无）

// Transformer 状态转换函数，接受只读状态视图返回任意值
type Transformer func(readable ReadableState) any
```

**注意**：接口满足测试还依赖 `InMemoryState` 和 `InMemoryCommitState`，需要创建占位文件。先创建最小占位，后续 Task 再完善。

- [ ] **Step 4: 创建 inmemory_state.go 最小占位**

```go
package state

// InMemoryState State 接口的内存实现（占位，后续 Task 完善）
type InMemoryState struct{}

// NewInMemoryState 创建内存状态实例（占位）
func NewInMemoryState() *InMemoryState {
	return &InMemoryState{}
}
```

- [ ] **Step 5: 创建 inmemory_commit_state.go 最小占位**

```go
package state

// InMemoryCommitState CommitState 接口的内存实现（占位，后续 Task 完善）
type InMemoryCommitState struct{}

// NewInMemoryCommitState 创建内存事务状态实例（占位）
func NewInMemoryCommitState(state ...State) *InMemoryCommitState {
	return &InMemoryCommitState{}
}
```

- [ ] **Step 6: 运行接口满足测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run Test接口满足 -v`
Expected: FAIL（InMemoryState/InMemoryCommitState 未实现接口方法）

这是预期行为，占位文件只保证了类型存在。完整实现在后续 Task 中。

- [ ] **Step 7: Commit（接口定义 + 常量 + 占位文件）**

```bash
git add internal/agentcore/session/state/state.go internal/agentcore/session/state/state_test.go internal/agentcore/session/state/inmemory_state.go internal/agentcore/session/state/inmemory_commit_state.go
git commit -m "feat(session): add 4-layer state interfaces, constants, and placeholder implementations"
```

---

### Task 4: 完善 utils.go 工具函数

**Files:**
- Modify: `internal/agentcore/session/state/utils.go`
- Create: `internal/agentcore/session/state/utils_test.go`

- [ ] **Step 1: 编写 utils_test.go 失败测试**

```go
package state

import (
	"reflect"
	"testing"
)

// ──── deepCopyMap 测试 ────

// TestDeepCopyMap_基本 验证深拷贝 map 值相同但引用不同。
func TestDeepCopyMap_基本(t *testing.T) {
	src := map[string]any{"a": 1, "b": "hello"}
	dst := deepCopyMap(src)
	if !reflect.DeepEqual(dst, src) {
		t.Errorf("深拷贝结果 %v 与源 %v 不一致", dst, src)
	}
	dst["a"] = 2
	if src["a"] != 1 {
		t.Error("修改拷贝后源被影响，深拷贝失败")
	}
}

// TestDeepCopyMap_嵌套 验证嵌套 map 深拷贝。
func TestDeepCopyMap_嵌套(t *testing.T) {
	src := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	dst := deepCopyMap(src)
	dst["user"].(map[string]any)["name"] = "bob"
	if src["user"].(map[string]any)["name"] != "alice" {
		t.Error("修改嵌套 map 后源被影响，深拷贝失败")
	}
}

// TestDeepCopyMap_nil 验证 nil map 深拷贝返回 nil。
func TestDeepCopyMap_nil(t *testing.T) {
	var src map[string]any
	dst := deepCopyMap(src)
	if dst != nil {
		t.Errorf("nil map 深拷贝 = %v, 期望 nil", dst)
	}
}

// TestDeepCopyMap_空map 验证空 map 深拷贝返回空 map。
func TestDeepCopyMap_空map(t *testing.T) {
	src := map[string]any{}
	dst := deepCopyMap(src)
	if len(dst) != 0 {
		t.Errorf("空 map 深拷贝长度 = %d, 期望 0", len(dst))
	}
}

// TestDeepCopySlice_基本 验证深拷贝 slice 值相同但引用不同。
func TestDeepCopySlice_基本(t *testing.T) {
	src := []any{1, "hello", true}
	dst := deepCopySlice(src)
	if !reflect.DeepEqual(dst, src) {
		t.Errorf("深拷贝结果 %v 与源 %v 不一致", dst, src)
	}
	dst[0] = 2
	if src[0] != 1 {
		t.Error("修改拷贝后源被影响，深拷贝失败")
	}
}

// TestDeepCopySlice_嵌套Map 验证 slice 中嵌套 map 的深拷贝。
func TestDeepCopySlice_嵌套Map(t *testing.T) {
	src := []any{map[string]any{"key": "value"}}
	dst := deepCopySlice(src)
	dst[0].(map[string]any)["key"] = "changed"
	if src[0].(map[string]any)["key"] != "value" {
		t.Error("修改嵌套 map 后源被影响，深拷贝失败")
	}
}

// TestDeepCopySlice_nil 验证 nil slice 深拷贝返回 nil。
func TestDeepCopySlice_nil(t *testing.T) {
	var src []any
	dst := deepCopySlice(src)
	if dst != nil {
		t.Errorf("nil slice 深拷贝 = %v, 期望 nil", dst)
	}
}

// ──── splitNestedPath 测试 ────

// TestSplitNestedPath_点分隔 验证点分隔路径解析。
func TestSplitNestedPath_点分隔(t *testing.T) {
	result := splitNestedPath("a.b.c")
	expected := []any{"a", "b", "c"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a.b.c\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_数组索引 验证数组索引路径解析。
func TestSplitNestedPath_数组索引(t *testing.T) {
	result := splitNestedPath("a.b[0].c")
	expected := []any{"a", "b", 0, "c"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a.b[0].c\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_复合路径 验证复合路径解析。
func TestSplitNestedPath_复合路径(t *testing.T) {
	result := splitNestedPath("a_1.b.c[1].d")
	expected := []any{"a_1", "b", "c", 1, "d"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a_1.b.c[1].d\") = %v, 期望 %v", result, expected)
	}
}

// TestSplitNestedPath_无分隔符 验证无分隔符路径返回空切片。
func TestSplitNestedPath_无分隔符(t *testing.T) {
	result := splitNestedPath("simple")
	if len(result) != 0 {
		t.Errorf("splitNestedPath(\"simple\") = %v, 期望空切片", result)
	}
}

// TestSplitNestedPath_非字符串 验证非字符串输入返回空切片。
func TestSplitNestedPath_非字符串(t *testing.T) {
	result := splitNestedPath("")
	if len(result) != 0 {
		t.Errorf("splitNestedPath(\"\") = %v, 期望空切片", result)
	}
}

// TestSplitNestedPath_负数索引 验证负数索引路径解析。
func TestSplitNestedPath_负数索引(t *testing.T) {
	result := splitNestedPath("a[-1]")
	expected := []any{"a", -1}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("splitNestedPath(\"a[-1]\") = %v, 期望 %v", result, expected)
	}
}

// ──── isRefPath / extractOriginKey 测试 ────

// TestIsRefPath_引用路径 验证引用路径判断。
func TestIsRefPath_引用路径(t *testing.T) {
	if !isRefPath("${start123.p2}") {
		t.Error("isRefPath(\"${start123.p2}\") = false, 期望 true")
	}
}

// TestIsRefPath_普通路径 验证普通路径不是引用路径。
func TestIsRefPath_普通路径(t *testing.T) {
	if isRefPath("a.b.c") {
		t.Error("isRefPath(\"a.b.c\") = true, 期望 false")
	}
}

// TestIsRefPath_过短 验证过短字符串不是引用路径。
func TestIsRefPath_过短(t *testing.T) {
	if isRefPath("${}") {
		t.Error("isRefPath(\"${}\") = true, 期望 false")
	}
}

// TestExtractOriginKey_引用路径 验证引用路径提取原始 key。
func TestExtractOriginKey_引用路径(t *testing.T) {
	result := extractOriginKey("${start123.p2}")
	if result != "start123.p2" {
		t.Errorf("extractOriginKey(\"${start123.p2}\") = %q, 期望 %q", result, "start123.p2")
	}
}

// TestExtractOriginKey_普通路径 验证普通路径原样返回。
func TestExtractOriginKey_普通路径(t *testing.T) {
	result := extractOriginKey("a.b.c")
	if result != "a.b.c" {
		t.Errorf("extractOriginKey(\"a.b.c\") = %q, 期望 %q", result, "a.b.c")
	}
}

// TestExtractOriginKey_无$ 验证不含 $ 的路径原样返回。
func TestExtractOriginKey_无$(t *testing.T) {
	result := extractOriginKey("simple")
	if result != "simple" {
		t.Errorf("extractOriginKey(\"simple\") = %q, 期望 %q", result, "simple")
	}
}

// ──── expandNestedStructure 测试 ────

// TestExpandNestedStructure_嵌套key 验证嵌套 key 展开为嵌套结构。
func TestExpandNestedStructure_嵌套key(t *testing.T) {
	input := map[string]any{"a.b": 1}
	result := expandNestedStructure(input)
	expected := map[string]any{"a": map[string]any{"b": 1}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expandNestedStructure = %v, 期望 %v", result, expected)
	}
}

// TestExpandNestedStructure_扁平key 验证扁平 key 不变。
func TestExpandNestedStructure_扁平key(t *testing.T) {
	input := map[string]any{"a": 1}
	result := expandNestedStructure(input)
	if !reflect.DeepEqual(result, input) {
		t.Errorf("expandNestedStructure = %v, 期望 %v", result, input)
	}
}

// TestExpandNestedStructure_列表 验证列表中的嵌套结构展开。
func TestExpandNestedStructure_列表(t *testing.T) {
	input := []any{map[string]any{"a.b": 1}}
	result := expandNestedStructure(input)
	expected := []any{map[string]any{"a": map[string]any{"b": 1}}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("expandNestedStructure = %v, 期望 %v", result, expected)
	}
}

// TestExpandNestedStructure_原始值 验证原始值直接返回。
func TestExpandNestedStructure_原始值(t *testing.T) {
	result := expandNestedStructure(42)
	if result != 42 {
		t.Errorf("expandNestedStructure(42) = %v, 期望 42", result)
	}
}

// ──── updateDict 测试 ────

// TestUpdateDict_基本更新 验证基本字典更新。
func TestUpdateDict_基本更新(t *testing.T) {
	source := map[string]any{"a": 1}
	update := map[string]any{"b": 2}
	updateDict(update, source)
	if source["b"] != 2 {
		t.Errorf("更新后 source[\"b\"] = %v, 期望 2", source["b"])
	}
}

// TestUpdateDict_嵌套路径 验证嵌套路径 key 更新。
func TestUpdateDict_嵌套路径(t *testing.T) {
	source := map[string]any{}
	update := map[string]any{"a.b": 1}
	updateDict(update, source)
	if source["a"] == nil {
		t.Fatal("source[\"a\"] 为 nil")
	}
	nested := source["a"].(map[string]any)
	if nested["b"] != 1 {
		t.Errorf("更新后 source[\"a\"][\"b\"] = %v, 期望 1", nested["b"])
	}
}

// TestUpdateDict_nil删除 验证 value 为 nil 时删除对应 key。
func TestUpdateDict_nil删除(t *testing.T) {
	source := map[string]any{"a": 1, "b": 2}
	update := map[string]any{"a": nil}
	updateDict(update, source)
	if _, exists := source["a"]; exists {
		t.Error("value 为 nil 时应删除 key \"a\"")
	}
	if source["b"] != 2 {
		t.Errorf("不应删除无关 key \"b\"")
	}
}

// TestUpdateDict_覆盖 验证覆盖已有值。
func TestUpdateDict_覆盖(t *testing.T) {
	source := map[string]any{"a": 1}
	update := map[string]any{"a": 2}
	updateDict(update, source)
	if source["a"] != 2 {
		t.Errorf("更新后 source[\"a\"] = %v, 期望 2", source["a"])
	}
}

// ──── getBySchema 测试 ────

// TestGetBySchema_字符串key 验证字符串 key 读取。
func TestGetBySchema_字符串key(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getBySchema(StringKey("name"), data)
	if result != "alice" {
		t.Errorf("getBySchema(StringKey(\"name\")) = %v, 期望 %v", result, "alice")
	}
}

// TestGetBySchema_字符串key_嵌套 验证嵌套路径字符串 key 读取。
func TestGetBySchema_字符串key_嵌套(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "alice"}}
	result := getBySchema(StringKey("user.name"), data)
	if result != "alice" {
		t.Errorf("getBySchema(StringKey(\"user.name\")) = %v, 期望 %v", result, "alice")
	}
}

// TestGetBySchema_字符串key_引用路径 验证引用路径字符串 key 读取。
func TestGetBySchema_字符串key_引用路径(t *testing.T) {
	data := map[string]any{"user": map[string]any{"name": "alice"}}
	result := getBySchema(StringKey("${user.name}"), data)
	if result != "alice" {
		t.Errorf("getBySchema(StringKey(\"${user.name}\")) = %v, 期望 %v", result, "alice")
	}
}

// TestGetBySchema_mapSchema 验证 map schema 批量读取。
func TestGetBySchema_mapSchema(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}
	schema := SchemaKey(map[string]any{
		"name": "user.name",
		"age":  "user.age",
	})
	result := getBySchema(schema, data)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 map[string]any", result)
	}
	if m["name"] != "alice" {
		t.Errorf("result[\"name\"] = %v, 期望 alice", m["name"])
	}
	if m["age"] != 30 {
		t.Errorf("result[\"age\"] = %v, 期望 30", m["age"])
	}
}

// TestGetBySchema_listSchema 验证 list schema 批量读取。
func TestGetBySchema_listSchema(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	}
	schema := ListKey([]any{"user.name", "user.age"})
	result := getBySchema(schema, data)
	l, ok := result.([]any)
	if !ok {
		t.Fatalf("getBySchema 返回类型 %T, 期望 []any", result)
	}
	if l[0] != "alice" {
		t.Errorf("result[0] = %v, 期望 alice", l[0])
	}
	if l[1] != 30 {
		t.Errorf("result[1] = %v, 期望 30", l[1])
	}
}

// TestGetBySchema_不存在 验证 key 不存在返回 nil。
func TestGetBySchema_不存在(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getBySchema(StringKey("missing"), data)
	if result != nil {
		t.Errorf("getBySchema(不存在的 key) = %v, 期望 nil", result)
	}
}

// TestGetBySchema_nil数据 验证 nil 数据返回 nil。
func TestGetBySchema_nil数据(t *testing.T) {
	result := getBySchema(StringKey("name"), nil)
	if result != nil {
		t.Errorf("getBySchema(nil 数据) = %v, 期望 nil", result)
	}
}

// TestGetBySchema_带前缀 验证带 nestedPath 前缀读取。
func TestGetBySchema_带前缀(t *testing.T) {
	data := map[string]any{
		"node1": map[string]any{"name": "alice"},
	}
	result := getBySchema(StringKey("name"), data, "node1")
	if result != "alice" {
		t.Errorf("getBySchema(带前缀) = %v, 期望 %v", result, "alice")
	}
}

// ──── getValueByNestedPath 测试 ────

// TestGetValueByNestedPath_扁平 验证扁平 key 读取。
func TestGetValueByNestedPath_扁平(t *testing.T) {
	data := map[string]any{"name": "alice"}
	result := getValueByNestedPath("name", data)
	if result != "alice" {
		t.Errorf("getValueByNestedPath(\"name\") = %v, 期望 alice", result)
	}
}

// TestGetValueByNestedPath_嵌套 验证嵌套路径读取。
func TestGetValueByNestedPath_嵌套(t *testing.T) {
	data := map[string]any{
		"user": map[string]any{"name": "alice"},
	}
	result := getValueByNestedPath("user.name", data)
	if result != "alice" {
		t.Errorf("getValueByNestedPath(\"user.name\") = %v, 期望 alice", result)
	}
}

// TestGetValueByNestedPath_数组索引 验证数组索引读取。
func TestGetValueByNestedPath_数组索引(t *testing.T) {
	data := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	result := getValueByNestedPath("items[1]", data)
	if result != "b" {
		t.Errorf("getValueByNestedPath(\"items[1]\") = %v, 期望 b", result)
	}
}

// TestGetValueByNestedPath_不存在 验证路径不存在返回 nil。
func TestGetValueByNestedPath_不存在(t *testing.T) {
	data := map[string]any{}
	result := getValueByNestedPath("missing", data)
	if result != nil {
		t.Errorf("getValueByNestedPath(不存在的路径) = %v, 期望 nil", result)
	}
}

// ──── rootToPath 测试 ────

// TestRootToPath_扁平key 验证扁平 key 导航。
func TestRootToPath_扁平key(t *testing.T) {
	source := map[string]any{"a": 1}
	key, container := rootToPath("a", source)
	if key != "a" {
		t.Errorf("rootToPath key = %v, 期望 \"a\"", key)
	}
	if container["a"] != 1 {
		t.Errorf("rootToPath container[\"a\"] = %v, 期望 1", container["a"])
	}
}

// TestRootToPath_嵌套路径 验证嵌套路径导航到最终容器。
func TestRootToPath_嵌套路径(t *testing.T) {
	source := map[string]any{
		"a": map[string]any{"b": 1},
	}
	key, container := rootToPath("a.b", source)
	if key != "b" {
		t.Errorf("rootToPath key = %v, 期望 \"b\"", key)
	}
	if container["b"] != 1 {
		t.Errorf("rootToPath container[\"b\"] = %v, 期望 1", container["b"])
	}
}

// TestRootToPath_不存在 验证路径不存在时返回 nil。
func TestRootToPath_不存在(t *testing.T) {
	source := map[string]any{}
	key, container := rootToPath("missing", source)
	if key != nil {
		t.Errorf("rootToPath key = %v, 期望 nil", key)
	}
}

// TestRootToPath_创建中间节点 验证 createIfAbsent 时自动创建中间节点。
func TestRootToPath_创建中间节点(t *testing.T) {
	source := map[string]any{}
	key, container := rootToPath("a.b", source, true)
	if key != "b" {
		t.Errorf("rootToPath key = %v, 期望 \"b\"", key)
	}
	if container == nil {
		t.Fatal("container 为 nil")
	}
}

// ──── updateByKey / deleteByKey 测试 ────

// TestUpdateByKey_新key 验证新增 key。
func TestUpdateByKey_新key(t *testing.T) {
	source := map[string]any{}
	updateByKey("a", 1, source)
	if source["a"] != 1 {
		t.Errorf("updateByKey 后 source[\"a\"] = %v, 期望 1", source["a"])
	}
}

// TestUpdateByKey_覆盖 验证覆盖已有值。
func TestUpdateByKey_覆盖(t *testing.T) {
	source := map[string]any{"a": 1}
	updateByKey("a", 2, source)
	if source["a"] != 2 {
		t.Errorf("updateByKey 后 source[\"a\"] = %v, 期望 2", source["a"])
	}
}

// TestDeleteByKey_存在 验证删除存在的 key。
func TestDeleteByKey_存在(t *testing.T) {
	source := map[string]any{"a": 1}
	deleteByKey("a", source)
	if _, exists := source["a"]; exists {
		t.Error("deleteByKey 后 key \"a\" 仍存在")
	}
}

// TestDeleteByKey_不存在 验证删除不存在的 key 不报错。
func TestDeleteByKey_不存在(t *testing.T) {
	source := map[string]any{}
	deleteByKey("missing", source) // 不应 panic
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run TestDeepCopyMap -v`
Expected: FAIL（deepCopyMap 已存在，但 splitNestedPath 等新函数未定义）

- [ ] **Step 3: 完善 utils.go，补充所有工具函数**

在已有 `utils.go` 的 deepCopy 函数之后追加：

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// regexMaxLength 正则匹配最大长度
	regexMaxLength = 1000
	// nestedPathSplit 嵌套路径分隔符
	nestedPathSplit = "."
	// nestedPathListSplit 列表索引开始符
	nestedPathListSplit = "["
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// （工具函数均为非导出，供同包使用）

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitNestedPath 拆分嵌套路径
// 例: "a_1.b.c[1].d" → ["a_1", "b", "c", 1, "d"]
func splitNestedPath(nestedKey string) []any {
	if nestedKey == "" {
		return nil
	}
	if !containsChar(nestedKey, nestedPathSplit) &&
		!containsChar(nestedKey, nestedPathListSplit) &&
		!containsChar(nestedKey, "['") {
		return nil
	}

	var result []any
	parts := splitString(nestedKey, nestedPathSplit)
	for _, part := range parts {
		if containsChar(part, nestedPathListSplit) {
			// 处理 "c[1]" 或 "[1]" 格式
			baseAndIndexes := parseListIndexes(part)
			result = append(result, baseAndIndexes...)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// isRefPath 判断是否为引用路径，如 "${start123.p2}"
func isRefPath(path string) bool {
	return len(path) > 3 && len(path) <= regexMaxLength &&
		path[:2] == "${" && path[len(path)-1] == '}'
}

// extractOriginKey 从引用路径中提取原始 key
// 例: "${start123.p2}" → "start123.p2"
func extractOriginKey(key string) string {
	if !containsChar(key, "$") {
		return key
	}
	// 查找 ${...} 模式
	start := -1
	for i := 0; i < len(key) && i < regexMaxLength; i++ {
		if i+1 < len(key) && key[i] == '$' && key[i+1] == '{' {
			start = i + 2
			break
		}
	}
	if start == -1 {
		return key
	}
	for i := start; i < len(key); i++ {
		if key[i] == '}' {
			return key[start:i]
		}
	}
	return key
}

// updateDict 用 update 字典更新 source 字典
// source 是扁平结构，update 的 key 支持嵌套路径
// 如果 value 为 nil 则删除对应 key
func updateDict(update map[string]any, source map[string]any) {
	type removal struct {
		key       any
		container map[string]any
	}
	var removed []removal

	for key, value := range update {
		currentKey, current := rootToPath(key, source, true)
		if value == nil {
			removed = append(removed, removal{key: currentKey, container: current})
		} else {
			updateByKey(currentKey, value, current)
		}
	}
	for _, r := range removed {
		deleteByKey(r.key, r.container)
	}
}

// getBySchema 根据 schema 从 data 中获取值
// schema 可以是 string（路径）、map[string]any（批量映射）、[]any（列表映射）
func getBySchema(schema StateKey, data map[string]any, nestedPath ...string) any {
	// 如果有前缀，先定位到前缀位置
	if len(nestedPath) > 0 && nestedPath[0] != "" {
		data = getValueByNestedPathMap(nestedPath[0], data)
	}

	if data == nil {
		return nil
	}

	switch schema.Type() {
	case StateKeyString:
		originKey := extractOriginKey(schema.String())
		return getValueByNestedPath(originKey, data)
	case StateKeyMap:
		return getBySchemaMap(schema.Map(), data)
	case StateKeyList:
		return getBySchemaList(schema.List(), data)
	default:
		return nil
	}
}

// getValueByNestedPath 根据嵌套路径从 source 获取值
// 例: "a.b[0].c" → source["a"]["b"][0]["c"]
func getValueByNestedPath(nestedKey string, source map[string]any) any {
	key, container := rootToPath(nestedKey, source)
	if key == nil || container == nil {
		return nil
	}
	return container[key]
}

// rootToPath 沿嵌套路径导航到最终容器
// 返回 (最终key, 最终容器)
// createIfAbsent 为 true 时自动创建缺失的中间节点
func rootToPath(nestedPath string, source map[string]any, createIfAbsent ...bool) (any, map[string]any) {
	create := len(createIfAbsent) > 0 && createIfAbsent[0]
	paths := splitNestedPath(nestedPath)
	if len(paths) == 0 {
		return nestedPath, source
	}

	current := source
	for i, path := range paths {
		isLast := i == len(paths)-1
		switch p := path.(type) {
		case string:
			if _, exists := current[p]; !exists {
				if !create {
					return nil, nil
				}
				if !isLast && i+1 < len(paths) {
					if _, isInt := paths[i+1].(int); isInt {
						current[p] = []any{}
					} else {
						current[p] = map[string]any{}
					}
				} else {
					current[p] = map[string]any{}
				}
			}
			if isLast {
				return p, current
			}
			next, ok := current[p].(map[string]any)
			if !ok {
				if !create {
					return nil, nil
				}
				next = map[string]any{}
				current[p] = next
			}
			current = next
		case int:
			list, ok := current[p].([]any)
			if !ok || p >= len(list) {
				return nil, nil
			}
			if isLast {
				return p, current
			}
			next, ok := list[p].(map[string]any)
			if !ok {
				return nil, nil
			}
			current = next
		}
	}
	return nil, nil
}

// expandNestedStructure 将嵌套 key 的字典展开为嵌套结构
// 例: {"a.b": 1} → {"a": {"b": 1}}
func expandNestedStructure(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, value := range v {
			currentKey, current := rootToPath(key, result, true)
			current[currentKey.(string)] = expandNestedStructure(value)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = expandNestedStructure(item)
		}
		return result
	default:
		return data
	}
}

// updateByKey 在 source 中按 key 更新值
func updateByKey(key any, newValue any, source map[string]any) {
	keyStr, ok := key.(string)
	if !ok {
		return
	}
	if _, exists := source[keyStr]; !exists {
		source[keyStr] = expandNestedStructure(newValue)
		return
	}
	if existing, ok := source[keyStr].(map[string]any); ok {
		if newMap, ok := newValue.(map[string]any); ok {
			updateDict(newMap, existing)
			return
		}
	}
	source[keyStr] = expandNestedStructure(newValue)
}

// deleteByKey 在 source 中按 key 删除
func deleteByKey(key any, source map[string]any) {
	keyStr, ok := key.(string)
	if !ok {
		return
	}
	delete(source, keyStr)
}

// ──────────────────────────── 内部辅助函数 ────────────────────────────

// getValueByNestedPathMap 与 getValueByNestedPath 类似，但返回 map[string]any
// 用于 getBySchema 中根据前缀定位
func getValueByNestedPathMap(nestedKey string, source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	result := getValueByNestedPath(nestedKey, source)
	if m, ok := result.(map[string]any); ok {
		return m
	}
	return nil
}

// getBySchemaMap 处理 map schema 的递归读取
func getBySchemaMap(schema map[string]any, data map[string]any) map[string]any {
	result := map[string]any{}
	for targetKey, targetSchema := range schema {
		switch s := targetSchema.(type) {
		case []any:
			result[targetKey] = getBySchema(ListKey(s), data)
		case map[string]any:
			result[targetKey] = getBySchema(SchemaKey(s), data)
		case string:
			if isRefPath(s) {
				result[targetKey] = getBySchema(StringKey(s), data)
			} else {
				result[targetKey] = s
			}
		default:
			result[targetKey] = targetSchema
		}
	}
	return result
}

// getBySchemaList 处理 list schema 的递归读取
func getBySchemaList(schema []any, data map[string]any) []any {
	result := make([]any, len(schema))
	for i, item := range schema {
		switch s := item.(type) {
		case string:
			result[i] = getBySchema(StringKey(s), data)
		case map[string]any:
			result[i] = getBySchema(SchemaKey(s), data)
		case []any:
			result[i] = getBySchema(ListKey(s), data)
		default:
			result[i] = item
		}
	}
	return result
}

// containsChar 检查字符串是否包含指定字符/子串
func containsChar(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// splitString 按分隔符拆分字符串
func splitString(s, sep string) []string {
	if sep == "" {
		return []string{s}
	}
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
			i += len(sep) - 1
		}
	}
	result = append(result, s[start:])
	return result
}

// parseListIndexes 解析包含数组索引的部分
// 例: "c[1]" → ["c", 1], "[1]" → [1], "a[-1]" → ["a", -1]
func parseListIndexes(part string) []any {
	var result []any
	// 找到第一个 [
	bracketIdx := -1
	for i := 0; i < len(part); i++ {
		if part[i] == '[' {
			bracketIdx = i
			break
		}
	}
	if bracketIdx == -1 {
		return []any{part}
	}

	// 提取基础部分
	base := part[:bracketIdx]
	if base != "" {
		result = append(result, base)
	}

	// 解析所有索引
	remaining := part[bracketIdx:]
	for len(remaining) > 0 {
		if remaining[0] != '[' {
			break
		}
		// 找到对应的 ]
		end := -1
		for i := 1; i < len(remaining); i++ {
			if remaining[i] == ']' {
				end = i
				break
			}
		}
		if end == -1 {
			break
		}
		indexStr := remaining[1:end]
		// 尝试解析为整数
		var idx int
		isNeg := false
		parseStart := 0
		if len(indexStr) > 0 && indexStr[0] == '-' {
			isNeg = true
			parseStart = 1
		}
		isInt := true
		if parseStart >= len(indexStr) {
			isInt = false
		} else {
			parsed := 0
			for i := parseStart; i < len(indexStr); i++ {
				if indexStr[i] < '0' || indexStr[i] > '9' {
					// 可能是 'key' 格式的字符串索引
					isInt = false
					break
				}
				parsed = parsed*10 + int(indexStr[i]-'0')
			}
			if isInt {
				if isNeg {
					idx = -parsed
				} else {
					idx = parsed
				}
			}
		}
		if isInt {
			result = append(result, idx)
		} else {
			// 字符串索引，如 ['key']
			result = append(result, indexStr)
		}
		remaining = remaining[end+1:]
	}
	return result
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run "TestDeepCopy|TestSplit|TestIsRef|TestExtract|TestExpand|TestUpdateDict|TestGetBySchema|TestGetValue|TestRootToPath|TestUpdateByKey|TestDeleteByKey" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/state/utils.go internal/agentcore/session/state/utils_test.go
git commit -m "feat(session): add state utils - deepCopy, nested path parsing, updateDict, getBySchema"
```

---

### Task 5: 实现 InMemoryState

**Files:**
- Modify: `internal/agentcore/session/state/inmemory_state.go`
- Create: `internal/agentcore/session/state/inmemory_state_test.go`

- [ ] **Step 1: 编写 inmemory_state_test.go 失败测试**

```go
package state

import (
	"reflect"
	"testing"
)

// TestNewInMemoryState 验证构造函数创建非 nil 实例。
func TestNewInMemoryState(t *testing.T) {
	s := NewInMemoryState()
	if s == nil {
		t.Fatal("NewInMemoryState 返回 nil")
	}
}

// TestInMemoryState_Get_字符串key 验证字符串 key 读取。
func TestInMemoryState_Get_字符串key(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"name": "alice"})
	result := s.Get(StringKey("name"))
	if result != "alice" {
		t.Errorf("Get = %v, 期望 alice", result)
	}
}

// TestInMemoryState_Get_嵌套路径 验证嵌套路径读取。
func TestInMemoryState_Get_嵌套路径(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"user": map[string]any{"name": "alice"}})
	result := s.Get(StringKey("user.name"))
	if result != "alice" {
		t.Errorf("Get = %v, 期望 alice", result)
	}
}

// TestInMemoryState_Get_mapSchema 验证 map schema 批量读取。
func TestInMemoryState_Get_mapSchema(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	})
	result := s.Get(SchemaKey(map[string]any{
		"name": "user.name",
		"age":  "user.age",
	}))
	m := result.(map[string]any)
	if m["name"] != "alice" {
		t.Errorf("name = %v, 期望 alice", m["name"])
	}
}

// TestInMemoryState_Get_listSchema 验证 list schema 批量读取。
func TestInMemoryState_Get_listSchema(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{
		"user": map[string]any{"name": "alice", "age": 30},
	})
	result := s.Get(ListKey([]any{"user.name", "user.age"}))
	l := result.([]any)
	if l[0] != "alice" {
		t.Errorf("result[0] = %v, 期望 alice", l[0])
	}
}

// TestInMemoryState_Get_深拷贝 验证 Get 返回深拷贝，修改返回值不影响内部。
func TestInMemoryState_Get_深拷贝(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"user": map[string]any{"name": "alice"}})
	result := s.Get(StringKey("user"))
	result.(map[string]any)["name"] = "bob"
	// 再次获取，内部应不受影响
	result2 := s.Get(StringKey("user.name"))
	if result2 != "alice" {
		t.Errorf("Get 返回非深拷贝，修改返回值后内部被影响")
	}
}

// TestInMemoryState_Get_不存在 验证不存在的 key 返回 nil。
func TestInMemoryState_Get_不存在(t *testing.T) {
	s := NewInMemoryState()
	result := s.Get(StringKey("missing"))
	if result != nil {
		t.Errorf("Get(不存在的 key) = %v, 期望 nil", result)
	}
}

// TestInMemoryState_GetByPrefix 验证带前缀读取。
func TestInMemoryState_GetByPrefix(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{
		"node1": map[string]any{"name": "alice"},
	})
	result := s.GetByPrefix(StringKey("name"), "node1")
	if result != "alice" {
		t.Errorf("GetByPrefix = %v, 期望 alice", result)
	}
}

// TestInMemoryState_Update 验证 Update 更新状态。
func TestInMemoryState_Update(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("Update 后 Get = %v, 期望 1", s.Get(StringKey("a")))
	}
}

// TestInMemoryState_Update_深拷贝输入 验证 Update 深拷贝输入数据。
func TestInMemoryState_Update_深拷贝输入(t *testing.T) {
	s := NewInMemoryState()
	data := map[string]any{"a": 1}
	s.Update(data)
	data["a"] = 2
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("Update 未深拷贝输入，外部修改影响了内部状态")
	}
}

// TestInMemoryState_Update_覆盖 验证 Update 覆盖已有值。
func TestInMemoryState_Update_覆盖(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	s.Update(map[string]any{"a": 2})
	if s.Get(StringKey("a")) != 2 {
		t.Errorf("覆盖后 Get = %v, 期望 2", s.Get(StringKey("a")))
	}
}

// TestInMemoryState_GetByTransformer 验证通过转换函数获取值。
func TestInMemoryState_GetByTransformer(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1, "b": 2})
	result := s.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("a"))
	})
	if result != 1 {
		t.Errorf("GetByTransformer = %v, 期望 1", result)
	}
}

// TestInMemoryState_GetState 验证 GetState 返回完整快照。
func TestInMemoryState_GetState(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	state := s.GetState()
	if state["a"] != 1 {
		t.Errorf("GetState[\"a\"] = %v, 期望 1", state["a"])
	}
}

// TestInMemoryState_GetState_深拷贝 验证 GetState 返回深拷贝。
func TestInMemoryState_GetState_深拷贝(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	state := s.GetState()
	state["a"] = 2
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("GetState 返回非深拷贝，修改后内部被影响")
	}
}

// TestInMemoryState_SetState 验证 SetState 从快照恢复状态。
func TestInMemoryState_SetState(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	s.SetState(map[string]any{"b": 2})
	if s.Get(StringKey("a")) != nil {
		t.Errorf("SetState 后旧 key 仍存在")
	}
	if s.Get(StringKey("b")) != 2 {
		t.Errorf("SetState 后 Get = %v, 期望 2", s.Get(StringKey("b")))
	}
}

// TestInMemoryState_SetState_nil 验证 SetState 传入 nil 不影响当前状态。
func TestInMemoryState_SetState_nil(t *testing.T) {
	s := NewInMemoryState()
	s.Update(map[string]any{"a": 1})
	s.SetState(nil)
	if s.Get(StringKey("a")) != 1 {
		t.Errorf("SetState(nil) 后状态被清空，期望保留")
	}
}

// TestInMemoryState_接口满足 验证 InMemoryState 满足 State 接口。
func TestInMemoryState_接口满足(t *testing.T) {
	var _ State = (*InMemoryState)(nil)
}

// TestInMemoryState_完整读写流程 验证完整的读写流程。
func TestInMemoryState_完整读写流程(t *testing.T) {
	s := NewInMemoryState()

	// 初始写入
	s.Update(map[string]any{
		"user": map[string]any{
			"name": "alice",
			"age":  30,
		},
		"tags": []any{"go", "python"},
	})

	// 读取嵌套值
	name := s.Get(StringKey("user.name"))
	if name != "alice" {
		t.Errorf("user.name = %v, 期望 alice", name)
	}

	// 批量 schema 读取
	result := s.Get(SchemaKey(map[string]any{
		"userName": "user.name",
		"userAge":  "user.age",
	}))
	m := result.(map[string]any)
	if m["userName"] != "alice" {
		t.Errorf("userName = %v, 期望 alice", m["userName"])
	}

	// 获取完整快照
	snapshot := s.GetState()
	if !reflect.DeepEqual(snapshot["tags"], []any{"go", "python"}) {
		t.Errorf("tags = %v, 期望 [go python]", snapshot["tags"])
	}

	// 快照恢复
	s2 := NewInMemoryState()
	s2.SetState(snapshot)
	if s2.Get(StringKey("user.name")) != "alice" {
		t.Errorf("恢复后 user.name = %v, 期望 alice", s2.Get(StringKey("user.name")))
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run TestInMemoryState -v`
Expected: FAIL（InMemoryState 占位未实现方法）

- [ ] **Step 3: 编写完整的 inmemory_state.go**

```go
package state

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryState State 接口的内存实现
// 对应 Python 的 InMemoryStateLike
type InMemoryState struct {
	// state 内部状态存储
	state map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryState 创建内存状态实例
func NewInMemoryState() *InMemoryState {
	return &InMemoryState{
		state: make(map[string]any),
	}
}

// ──────────────────────────── InMemoryState 方法 ────────────────────────────

// Get 根据 key 获取状态值（深拷贝返回）
func (s *InMemoryState) Get(key StateKey) any {
	return deepCopyValue(getBySchema(key, s.state))
}

// GetByPrefix 根据 key 和嵌套前缀获取状态值（深拷贝返回）
func (s *InMemoryState) GetByPrefix(key StateKey, nestedPrefix string) any {
	return deepCopyValue(getBySchema(key, s.state, nestedPrefix))
}

// GetByTransformer 通过转换函数获取状态值
func (s *InMemoryState) GetByTransformer(transformer Transformer) any {
	return transformer(s)
}

// Update 更新状态数据（深拷贝输入）
func (s *InMemoryState) Update(data map[string]any) error {
	updateDict(deepCopyMap(data), s.state)
	return nil
}

// GetState 获取完整状态快照（深拷贝返回）
func (s *InMemoryState) GetState() map[string]any {
	return deepCopyMap(s.state)
}

// SetState 从快照恢复状态
func (s *InMemoryState) SetState(state map[string]any) {
	if state != nil {
		s.state = state
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run TestInMemoryState -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/state/inmemory_state.go internal/agentcore/session/state/inmemory_state_test.go
git commit -m "feat(session): implement InMemoryState with Get/Update/GetState/SetState"
```

---

### Task 6: 实现 InMemoryCommitState

**Files:**
- Modify: `internal/agentcore/session/state/inmemory_commit_state.go`
- Create: `internal/agentcore/session/state/inmemory_commit_state_test.go`

- [ ] **Step 1: 编写 inmemory_commit_state_test.go 失败测试**

```go
package state

import (
	"reflect"
	"testing"
)

// TestNewInMemoryCommitState_默认 验证默认构造创建非 nil 实例。
func TestNewInMemoryCommitState_默认(t *testing.T) {
	cs := NewInMemoryCommitState()
	if cs == nil {
		t.Fatal("NewInMemoryCommitState 返回 nil")
	}
}

// TestNewInMemoryCommitState_传入State 验证传入底层 State 构造。
func TestNewInMemoryCommitState_传入State(t *testing.T) {
	inner := NewInMemoryState()
	inner.Update(map[string]any{"a": 1})
	cs := NewInMemoryCommitState(inner)
	result := cs.Get(StringKey("a"))
	if result != 1 {
		t.Errorf("Get = %v, 期望 1", result)
	}
}

// TestInMemoryCommitState_Update_禁止 验证直接调用 Update 返回错误。
func TestInMemoryCommitState_Update_禁止(t *testing.T) {
	cs := NewInMemoryCommitState()
	err := cs.Update(map[string]any{"a": 1})
	if err == nil {
		t.Error("Update 应返回错误")
	}
}

// TestInMemoryCommitState_UpdateByID_基本 验证按节点 ID 暂存更新。
func TestInMemoryCommitState_UpdateByID_基本(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	// 暂存更新不影响底层 state
	result := cs.Get(StringKey("a"))
	if result != nil {
		t.Errorf("UpdateByID 后 Get = %v, 期望 nil（未 commit）", result)
	}
}

// TestInMemoryCommitState_Commit_指定节点 验证提交指定节点的暂存更新。
func TestInMemoryCommitState_Commit_指定节点(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.Commit("node1")
	result := cs.Get(StringKey("a"))
	if result != 1 {
		t.Errorf("Commit 后 Get = %v, 期望 1", result)
	}
}

// TestInMemoryCommitState_Commit_全部 验证不传 nodeID 提交全部暂存。
func TestInMemoryCommitState_Commit_全部(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.UpdateByID("node2", map[string]any{"b": 2})
	cs.Commit()
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("全部 Commit 后 Get(\"a\") = %v, 期望 1", cs.Get(StringKey("a")))
	}
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("全部 Commit 后 Get(\"b\") = %v, 期望 2", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_Commit_清空暂存 验证 commit 后暂存被清空。
func TestInMemoryCommitState_Commit_清空暂存(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.Commit("node1")
	updates := cs.GetUpdates()
	if len(updates["node1"]) != 0 {
		t.Errorf("Commit 后暂存未清空，len = %d", len(updates["node1"]))
	}
}

// TestInMemoryCommitState_Rollback 验证回滚丢弃暂存更新。
func TestInMemoryCommitState_Rollback(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.Rollback("node1")
	// commit 不应生效
	cs.Commit("node1")
	result := cs.Get(StringKey("a"))
	if result != nil {
		t.Errorf("Rollback 后 Commit 不应生效，Get = %v, 期望 nil", result)
	}
}

// TestInMemoryCommitState_GetUpdates 验证获取暂存更新。
func TestInMemoryCommitState_GetUpdates(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.UpdateByID("node1", map[string]any{"b": 2})
	updates := cs.GetUpdates()
	if len(updates["node1"]) != 2 {
		t.Errorf("GetUpdates 长度 = %d, 期望 2", len(updates["node1"]))
	}
}

// TestInMemoryCommitState_SetUpdates 验证设置暂存更新。
func TestInMemoryCommitState_SetUpdates(t *testing.T) {
	cs := NewInMemoryCommitState()
	updates := map[string][]map[string]any{
		"node1": {{"a": 1}},
	}
	cs.SetUpdates(updates)
	result := cs.GetUpdates()
	if len(result["node1"]) != 1 {
		t.Errorf("SetUpdates 后 GetUpdates 长度 = %d, 期望 1", len(result["node1"]))
	}
}

// TestInMemoryCommitState_SetUpdates_nil 验证传入 nil 不影响暂存。
func TestInMemoryCommitState_SetUpdates_nil(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.SetUpdates(nil)
	if len(cs.GetUpdates()) == 0 {
		t.Error("SetUpdates(nil) 不应清空已有暂存")
	}
}

// TestInMemoryCommitState_GetByPrefix 验证委托给底层 state 的 GetByPrefix。
func TestInMemoryCommitState_GetByPrefix(t *testing.T) {
	inner := NewInMemoryState()
	inner.Update(map[string]any{
		"node1": map[string]any{"name": "alice"},
	})
	cs := NewInMemoryCommitState(inner)
	result := cs.GetByPrefix(StringKey("name"), "node1")
	if result != "alice" {
		t.Errorf("GetByPrefix = %v, 期望 alice", result)
	}
}

// TestInMemoryCommitState_GetByTransformer 验证委托给底层 state 的 GetByTransformer。
func TestInMemoryCommitState_GetByTransformer(t *testing.T) {
	inner := NewInMemoryState()
	inner.Update(map[string]any{"a": 1})
	cs := NewInMemoryCommitState(inner)
	result := cs.GetByTransformer(func(r ReadableState) any {
		return r.Get(StringKey("a"))
	})
	if result != 1 {
		t.Errorf("GetByTransformer = %v, 期望 1", result)
	}
}

// TestInMemoryCommitState_GetState 验证委托给底层 state 的 GetState。
func TestInMemoryCommitState_GetState(t *testing.T) {
	inner := NewInMemoryState()
	inner.Update(map[string]any{"a": 1})
	cs := NewInMemoryCommitState(inner)
	state := cs.GetState()
	if state["a"] != 1 {
		t.Errorf("GetState[\"a\"] = %v, 期望 1", state["a"])
	}
}

// TestInMemoryCommitState_SetState 验证委托给底层 state 的 SetState。
func TestInMemoryCommitState_SetState(t *testing.T) {
	inner := NewInMemoryState()
	inner.Update(map[string]any{"a": 1})
	cs := NewInMemoryCommitState(inner)
	cs.SetState(map[string]any{"b": 2})
	if cs.Get(StringKey("a")) != nil {
		t.Error("SetState 后旧 key 仍存在")
	}
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("SetState 后 Get = %v, 期望 2", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_接口满足 验证 InMemoryCommitState 满足 CommitState 接口。
func TestInMemoryCommitState_接口满足(t *testing.T) {
	var _ CommitState = (*InMemoryCommitState)(nil)
}

// TestInMemoryCommitState_多次UpdateByID 验证同一节点多次 UpdateByID 累积。
func TestInMemoryCommitState_多次UpdateByID(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.UpdateByID("node1", map[string]any{"b": 2})
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("Get(\"a\") = %v, 期望 1", cs.Get(StringKey("a")))
	}
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("Get(\"b\") = %v, 期望 2", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_完整事务流程 验证完整的事务流程。
func TestInMemoryCommitState_完整事务流程(t *testing.T) {
	cs := NewInMemoryCommitState()

	// 暂存更新
	cs.UpdateByID("node1", map[string]any{"a": 1})

	// 验证未提交
	if cs.Get(StringKey("a")) != nil {
		t.Error("未 commit 时不应能读到数据")
	}

	// 提交
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("commit 后 Get = %v, 期望 1", cs.Get(StringKey("a")))
	}

	// 再次暂存并回滚
	cs.UpdateByID("node1", map[string]any{"b": 2})
	cs.Rollback("node1")
	cs.Commit("node1") // 回滚后 commit 无数据
	if cs.Get(StringKey("b")) != nil {
		t.Error("rollback 后 commit 不应写入数据")
	}
}

// TestInMemoryCommitState_UpdateByID_深拷贝 验证 UpdateByID 深拷贝输入数据。
func TestInMemoryCommitState_UpdateByID_深拷贝(t *testing.T) {
	cs := NewInMemoryCommitState()
	data := map[string]any{"a": 1}
	cs.UpdateByID("node1", data)
	data["a"] = 2
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("UpdateByID 未深拷贝输入，外部修改影响了内部")
	}
}

// TestInMemoryCommitState_Commit_无暂存 验证没有暂存时 Commit 不报错。
func TestInMemoryCommitState_Commit_无暂存(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.Commit("nonexistent") // 不应 panic
}

// TestInMemoryCommitState_GetUpdates_深拷贝 验证 GetUpdates 返回的不是内部引用。
func TestInMemoryCommitState_GetUpdates_深拷贝(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	updates := cs.GetUpdates()
	// 修改返回值不应影响内部
	if len(updates["node1"]) > 0 {
		updates["node1"][0]["a"] = 999
		updates2 := cs.GetUpdates()
		if updates2["node1"][0]["a"] == 999 {
			t.Error("GetUpdates 返回了内部引用，修改后影响了内部")
		}
	}
}

// TestInMemoryCommitState_Rollback_不影响其他节点 验证回滚一个节点不影响其他节点。
func TestInMemoryCommitState_Rollback_不影响其他节点(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.UpdateByID("node2", map[string]any{"b": 2})
	cs.Rollback("node1")
	cs.Commit("node2")
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("回滚 node1 后 node2 commit 应正常，Get(\"b\") = %v", cs.Get(StringKey("b")))
	}
}

// TestInMemoryCommitState_多节点提交 验证分别提交多个节点。
func TestInMemoryCommitState_多节点提交(t *testing.T) {
	cs := NewInMemoryCommitState()
	cs.UpdateByID("node1", map[string]any{"a": 1})
	cs.UpdateByID("node2", map[string]any{"b": 2})
	cs.Commit("node1")
	if cs.Get(StringKey("a")) != 1 {
		t.Errorf("node1 commit 后 Get(\"a\") = %v, 期望 1", cs.Get(StringKey("a")))
	}
	// node2 未 commit
	if cs.Get(StringKey("b")) != nil {
		t.Errorf("node2 未 commit，Get(\"b\") = %v, 期望 nil", cs.Get(StringKey("b")))
	}
	cs.Commit("node2")
	if cs.Get(StringKey("b")) != 2 {
		t.Errorf("node2 commit 后 Get(\"b\") = %v, 期望 2", cs.Get(StringKey("b")))
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run TestInMemoryCommitState -v`
Expected: FAIL（InMemoryCommitState 占位未实现方法）

- [ ] **Step 3: 编写完整的 inmemory_commit_state.go**

```go
package state

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// InMemoryCommitState CommitState 接口的内存实现
// 对应 Python 的 InMemoryCommitState
type InMemoryCommitState struct {
	// state 底层状态（默认 InMemoryState）
	state State
	// updates 按 nodeID 缓存的待提交更新
	updates map[string][]map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryCommitState 创建内存事务状态实例
// 可选传入底层 State，默认创建新的 InMemoryState
func NewInMemoryCommitState(state ...State) *InMemoryCommitState {
	var s State
	if len(state) > 0 && state[0] != nil {
		s = state[0]
	} else {
		s = NewInMemoryState()
	}
	return &InMemoryCommitState{
		state:   s,
		updates: make(map[string][]map[string]any),
	}
}

// ──────────────────────────── InMemoryCommitState 方法 ────────────────────────────

// Get 委托给底层 state
func (s *InMemoryCommitState) Get(key StateKey) any {
	return s.state.Get(key)
}

// GetByPrefix 委托给底层 state
func (s *InMemoryCommitState) GetByPrefix(key StateKey, nestedPrefix string) any {
	return s.state.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 委托给底层 state
func (s *InMemoryCommitState) GetByTransformer(transformer Transformer) any {
	return s.state.GetByTransformer(transformer)
}

// GetState 委托给底层 state
func (s *InMemoryCommitState) GetState() map[string]any {
	return s.state.GetState()
}

// SetState 委托给底层 state
func (s *InMemoryCommitState) SetState(state map[string]any) {
	s.state.SetState(state)
}

// Update 禁止直接调用，必须使用 UpdateByID
// 对应 Python: raise build_error(StatusCode.ERROR, msg="commit state update must support node_id")
func (s *InMemoryCommitState) Update(data map[string]any) error {
	return fmt.Errorf("commit state update must support node_id")
}

// UpdateByID 按节点 ID 暂存更新（深拷贝 data）
func (s *InMemoryCommitState) UpdateByID(nodeID string, data map[string]any) {
	if nodeID == "" {
		return
	}
	s.updates[nodeID] = append(s.updates[nodeID], deepCopyMap(data))
}

// Commit 提交暂存更新到底层 state
// 不传 nodeID 则提交所有节点的暂存
func (s *InMemoryCommitState) Commit(nodeID ...string) {
	if len(nodeID) == 0 {
		// 提交全部
		for key, updates := range s.updates {
			for _, update := range updates {
				s.state.Update(update)
			}
			s.updates[key] = nil
		}
		// 清空所有节点
		s.updates = make(map[string][]map[string]any)
	} else {
		// 提交指定节点
		for _, id := range nodeID {
			nodeUpdates, exists := s.updates[id]
			if !exists || len(nodeUpdates) == 0 {
				continue
			}
			for _, update := range nodeUpdates {
				s.state.Update(update)
			}
			s.updates[id] = nil
		}
	}
}

// Rollback 丢弃指定节点的暂存更新
func (s *InMemoryCommitState) Rollback(nodeID string) {
	s.updates[nodeID] = nil
}

// GetUpdates 获取所有暂存更新
func (s *InMemoryCommitState) GetUpdates() map[string][]map[string]any {
	result := make(map[string][]map[string]any, len(s.updates))
	for key, updates := range s.updates {
		if len(updates) > 0 {
			copied := make([]map[string]any, len(updates))
			for i, u := range updates {
				copied[i] = deepCopyMap(u)
			}
			result[key] = copied
		}
	}
	return result
}

// SetUpdates 设置暂存更新
func (s *InMemoryCommitState) SetUpdates(updates map[string][]map[string]any) {
	if updates != nil {
		s.updates = updates
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -run TestInMemoryCommitState -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/session/state/inmemory_commit_state.go internal/agentcore/session/state/inmemory_commit_state_test.go
git commit -m "feat(session): implement InMemoryCommitState with UpdateByID/Commit/Rollback"
```

---

### Task 7: 全量测试 + 覆盖率检查 + 接口满足验证

**Files:**
- Verify: `internal/agentcore/session/state/` 下所有 `_test.go`

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/state/ -v`
Expected: 全部 PASS

- [ ] **Step 2: 检查测试覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/session/state/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 如果覆盖率不足 85%，补充测试用例**

根据 `go tool cover -func=coverage.out` 输出，对覆盖率不足的函数补充测试。

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 5.1 的状态从 `☐` 改为 `✅`：

```
| 5.1 | ✅ | State 体系 | ... | ... |
```

- [ ] **Step 5: 最终 Commit**

```bash
git add -A
git commit -m "feat(session): complete state base interfaces - 5.1 State体系 ✅"
```
