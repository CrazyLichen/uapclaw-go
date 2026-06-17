# 5.8 Checkpointer 接口 + 工厂 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Checkpointer 接口、工厂、InMemory 实现，并回填所有 `⤵️ 5.8` 标记的位置。

**Architecture:** 在 `session/checkpointer/` 子包中从底向上构建：Serializer → Storage → Checkpointer → Factory，然后回填 session/internal/interaction 等包中的类型和方法。

**Tech Stack:** Go 1.22+, encoding/json, github.com/uapclaw/uapclaw-go/internal/agentcore/session/state

**重要：State 体系变更说明**

state 包近期有重大重构，本计划已同步更新以下变化：
1. 顶层接口从 `state.State` 改为 `state.SessionState`
2. 新增 `state.WorkflowState` 独立接口（WorkflowCommitState 实现此接口）
3. `StateKey` 哨兵值：`StringKey("")` 零值判断 → `AllStateKey` 哨兵值
4. `InMemoryState` 改名为 `InMemoryStateLike`
5. `AgentStateCollection.GlobalState()` 返回 `*InMemoryStateLike`
6. WorkflowStorage 断言 `state.WorkflowState` 接口而非 `*WorkflowCommitState` 具体类型

---

### Task 1: 创建 checkpointer 包骨架（doc.go + base.go 接口）

**Files:**
- Create: `internal/agentcore/session/checkpointer/doc.go`
- Create: `internal/agentcore/session/checkpointer/base.go`
- Create: `internal/agentcore/session/checkpointer/base_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package checkpointer 提供会话状态检查点持久化能力。
//
// Checkpointer 在会话生命周期的关键节点（pre/post agent/workflow 执行、
// 中断时）保存和恢复会话状态，支持会话中断后恢复执行。
//
// 工厂模式支持多种存储后端：InMemory（内存）、Persistence（SQLite/Shelve）、
// Redis。通过 CheckpointerProvider 注册，CheckpointerFactory 创建。
//
// 文件目录：
//
//	checkpointer/
//	├── doc.go              # 包文档
//	├── base.go             # Checkpointer/Storage 接口、命名空间常量、Key 构建函数
//	├── serializer.go       # Serializer 接口、JSONSerializer 实现
//	├── inmemory.go         # InMemoryCheckpointer、AgentStorage/AgentTeamStorage/WorkflowStorage
//	├── factory.go          # CheckpointerFactory、CheckpointerProvider、CheckpointerConfig
//	├── base_test.go        # 基础接口和常量测试
//	├── serializer_test.go  # Serializer 测试
//	├── inmemory_test.go    # InMemoryCheckpointer 测试
//	└── factory_test.go     # CheckpointerFactory 测试
//
// 对应 Python 代码：openjiuwen/core/session/checkpointer/
package checkpointer
```

- [ ] **Step 2: 创建 base.go — 接口、常量、辅助函数**

```go
package checkpointer

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 接口 ────────────────────────────

// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
type Checkpointer interface {
	// GetThreadID 获取线程 ID（session_id:workflow_id）
	GetThreadID(session CheckpointerSession) string
	// PreWorkflowExecute 工作流执行前
	PreWorkflowExecute(ctx context.Context, session CheckpointerSession, inputs any) error
	// PostWorkflowExecute 工作流执行后
	PostWorkflowExecute(ctx context.Context, session CheckpointerSession, result any, exception error) error
	// PreAgentExecute Agent 执行前
	PreAgentExecute(ctx context.Context, session CheckpointerSession, inputs any) error
	// PreAgentTeamExecute AgentTeam 执行前
	PreAgentTeamExecute(ctx context.Context, session CheckpointerSession, inputs any) error
	// InterruptAgentExecute Agent 中断时保存检查点
	InterruptAgentExecute(ctx context.Context, session CheckpointerSession) error
	// PostAgentExecute Agent 执行后保存检查点
	PostAgentExecute(ctx context.Context, session CheckpointerSession) error
	// PostAgentTeamExecute AgentTeam 执行后保存检查点
	PostAgentTeamExecute(ctx context.Context, session CheckpointerSession) error
	// SessionExists 检查会话是否存在
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	// Release 释放会话资源
	Release(ctx context.Context, sessionID string) error
	// GraphStore 获取图状态存储
	// ⤵️ 8.7 回填：Graph Store 实现后返回 Store 实例
	GraphStore() any
}

// Storage 状态存储接口，负责单个实体的状态保存/恢复/清除。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Storage)
type Storage interface {
	// Save 保存会话状态
	Save(ctx context.Context, session CheckpointerSession) error
	// Recover 恢复会话状态
	Recover(ctx context.Context, session CheckpointerSession, inputs any) error
	// Clear 清除会话数据
	Clear(ctx context.Context, entityID string) error
	// Exists 检查状态是否存在
	Exists(ctx context.Context, session CheckpointerSession) (bool, error)
}

// CheckpointerSession Checkpointer 所需的会话最小接口。
// AgentSession/WorkflowSession/NodeSession 天然满足此接口。
// AgentID/TeamID 通过 AgentIDProvider/TeamIDProvider 类型断言获取。
// WorkflowState 的扩展方法（GetUpdates/SetUpdates/Commit 等）通过
// 类型断言为 state.WorkflowState 接口获取（比断言具体类型更优雅）。
type CheckpointerSession interface {
	// SessionID 获取会话唯一标识
	SessionID() string
	// WorkflowID 获取工作流 ID
	WorkflowID() string
	// State 获取会话状态
	State() state.SessionState
	// Config 获取会话配置
	Config() CheckpointerConfig
	// Parent 获取父会话
	Parent() CheckpointerSession
}

// CheckpointerConfig Checkpointer 所需的配置最小接口。
type CheckpointerConfig interface {
	// GetEnv 获取环境变量值
	GetEnv(key string, defaultValue ...any) any
}

// AgentIDProvider 提供 Agent ID 的接口（通过类型断言获取）。
// AgentSession 天然满足此接口。
type AgentIDProvider interface {
	AgentID() string
}

// TeamIDProvider 提供 Team ID 的接口（通过类型断言获取）。
// AgentTeamSession 天然满足此接口。
type TeamIDProvider interface {
	TeamID() string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SessionNamespaceAgent Agent 状态命名空间
	SessionNamespaceAgent = "agent"
	// SessionNamespaceAgentTeam AgentTeam 状态命名空间
	SessionNamespaceAgentTeam = "agent-team"
	// SessionNamespaceWorkflow Workflow 状态命名空间
	SessionNamespaceWorkflow = "workflow"
	// WorkflowNamespaceGraph Workflow 图状态命名空间
	WorkflowNamespaceGraph = "workflow-graph"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetThreadID 获取线程 ID（session_id:workflow_id）。
// 对应 Python: Checkpointer.get_thread_id()
func GetThreadID(session CheckpointerSession) string {
	return session.SessionID() + ":" + session.WorkflowID()
}

// BuildKey 用 ":" 连接各部分，构建存储键。
// 对应 Python: build_key(*parts)
func BuildKey(parts ...string) string {
	return strings.Join(parts, ":")
}

// BuildKeyWithNamespace 构建带命名空间的存储键：session:namespace:entity:suffixes。
// 对应 Python: build_key_with_namespace(session_id, namespace, entity_id, *suffixes)
func BuildKeyWithNamespace(sessionID, namespace, entityID string, suffixes ...string) string {
	parts := []string{sessionID, namespace, entityID}
	parts = append(parts, suffixes...)
	return BuildKey(parts...)
}

// GetAgentID 类型断言获取 Agent ID，不存在返回空字符串。
// 对应 Python: session.agent_id() if hasattr(session, "agent_id") else "Na"
func GetAgentID(session CheckpointerSession) string {
	if provider, ok := session.(AgentIDProvider); ok {
		return provider.AgentID()
	}
	return ""
}

// GetTeamID 类型断言获取 Team ID，不存在返回空字符串。
// 对应 Python: session.team_id() if hasattr(session, "team_id") else "Na"
func GetTeamID(session CheckpointerSession) string {
	if provider, ok := session.(TeamIDProvider); ok {
		return provider.TeamID()
	}
	return ""
}
```

- [ ] **Step 3: 创建 base_test.go**

```go
package checkpointer

import "testing"

// ──────────────────────────── 命名空间常量测试 ────────────────────────────

// Test命名空间常量值 测试命名空间常量与 Python 一致
func Test命名空间常量值(t *testing.T) {
	if SessionNamespaceAgent != "agent" {
		t.Errorf("SessionNamespaceAgent 期望 'agent'，实际=%s", SessionNamespaceAgent)
	}
	if SessionNamespaceAgentTeam != "agent-team" {
		t.Errorf("SessionNamespaceAgentTeam 期望 'agent-team'，实际=%s", SessionNamespaceAgentTeam)
	}
	if SessionNamespaceWorkflow != "workflow" {
		t.Errorf("SessionNamespaceWorkflow 期望 'workflow'，实际=%s", SessionNamespaceWorkflow)
	}
	if WorkflowNamespaceGraph != "workflow-graph" {
		t.Errorf("WorkflowNamespaceGraph 期望 'workflow-graph'，实际=%s", WorkflowNamespaceGraph)
	}
}

// ──────────────────────────── BuildKey 测试 ────────────────────────────

// TestBuildKey_单部分 测试单部分键
func TestBuildKey_单部分(t *testing.T) {
	if got := BuildKey("abc"); got != "abc" {
		t.Errorf("BuildKey('abc') = %s，期望 'abc'", got)
	}
}

// TestBuildKey_多部分 测试多部分键
func TestBuildKey_多部分(t *testing.T) {
	if got := BuildKey("a", "b", "c"); got != "a:b:c" {
		t.Errorf("BuildKey('a','b','c') = %s，期望 'a:b:c'", got)
	}
}

// TestBuildKey_空 测试空输入
func TestBuildKey_空(t *testing.T) {
	if got := BuildKey(); got != "" {
		t.Errorf("BuildKey() = %s，期望 ''", got)
	}
}

// ──────────────────────────── BuildKeyWithNamespace 测试 ────────────────────────────

// TestBuildKeyWithNamespace_基本 测试基本构建
func TestBuildKeyWithNamespace_基本(t *testing.T) {
	got := BuildKeyWithNamespace("sess1", "agent", "agent1")
	expected := "sess1:agent:agent1"
	if got != expected {
		t.Errorf("期望 %s，实际=%s", expected, got)
	}
}

// TestBuildKeyWithNamespace_有后缀 测试带后缀
func TestBuildKeyWithNamespace_有后缀(t *testing.T) {
	got := BuildKeyWithNamespace("sess1", "agent", "agent1", "state", "blobs")
	expected := "sess1:agent:agent1:state:blobs"
	if got != expected {
		t.Errorf("期望 %s，实际=%s", expected, got)
	}
}

// ──────────────────────────── GetThreadID 测试 ────────────────────────────

// TestGetThreadID 测试线程 ID 构建
func TestGetThreadID(t *testing.T) {
	session := &testSession{sessionID: "sess1", workflowID: "wf1"}
	got := GetThreadID(session)
	if got != "sess1:wf1" {
		t.Errorf("GetThreadID = %s，期望 'sess1:wf1'", got)
	}
}

// ──────────────────────────── GetAgentID/GetTeamID 类型断言测试 ────────────────────────────

// TestGetAgentID_满足接口 测试 session 满足 AgentIDProvider
func TestGetAgentID_满足接口(t *testing.T) {
	session := &testSessionWithAgentID{sessionID: "s1", agentID: "agent1"}
	got := GetAgentID(session)
	if got != "agent1" {
		t.Errorf("GetAgentID = %s，期望 'agent1'", got)
	}
}

// TestGetAgentID_不满足接口 测试 session 不满足 AgentIDProvider 返回空字符串
func TestGetAgentID_不满足接口(t *testing.T) {
	session := &testSession{sessionID: "s1", workflowID: "wf1"}
	got := GetAgentID(session)
	if got != "" {
		t.Errorf("不满足接口时应返回空字符串，实际=%s", got)
	}
}

// TestGetTeamID_满足接口 测试 session 满足 TeamIDProvider
func TestGetTeamID_满足接口(t *testing.T) {
	session := &testSessionWithTeamID{sessionID: "s1", teamID: "team1"}
	got := GetTeamID(session)
	if got != "team1" {
		t.Errorf("GetTeamID = %s，期望 'team1'", got)
	}
}

// TestGetTeamID_不满足接口 测试 session 不满足 TeamIDProvider 返回空字符串
func TestGetTeamID_不满足接口(t *testing.T) {
	session := &testSession{sessionID: "s1", workflowID: "wf1"}
	got := GetTeamID(session)
	if got != "" {
		t.Errorf("不满足接口时应返回空字符串，实际=%s", got)
	}
}

// ──────────────────────────── 测试辅助类型 ────────────────────────────

// testSession 最小 CheckpointerSession 实现
type testSession struct {
	sessionID  string
	workflowID string
}

func (s *testSession) SessionID() string                   { return s.sessionID }
func (s *testSession) WorkflowID() string                  { return s.workflowID }
func (s *testSession) State() state.SessionState           { return nil }
func (s *testSession) Config() CheckpointerConfig          { return nil }
func (s *testSession) Parent() CheckpointerSession         { return nil }

// testSessionWithAgentID 实现 AgentIDProvider 的 session
type testSessionWithAgentID struct {
	testSession
	agentID string
}

func (s *testSessionWithAgentID) AgentID() string { return s.agentID }

// testSessionWithTeamID 实现 TeamIDProvider 的 session
type testSessionWithTeamID struct {
	testSession
	teamID string
}

func (s *testSessionWithTeamID) TeamID() string { return s.teamID }
```

注意：base_test.go 需要导入 `github.com/uapclaw/uapclaw-go/internal/agentcore/session/state`。

- [ ] **Step 4: 运行测试验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/checkpointer/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/checkpointer/
git commit -m "feat(checkpointer): 添加 Checkpointer/Storage 接口、命名空间常量、Key 构建函数"
```

---

### Task 2: 实现 Serializer 接口 + JSONSerializer

**Files:**
- Create: `internal/agentcore/session/checkpointer/serializer.go`
- Create: `internal/agentcore/session/checkpointer/serializer_test.go`

- [ ] **Step 1: 创建 serializer.go**

```go
package checkpointer

import (
	"encoding/json"
)

// ──────────────────────────── 接口 ────────────────────────────

// Serializer 类型化序列化器接口。
// 对应 Python: openjiuwen/core/graph/store/serde.py (Serializer)
//
// DumpsTyped 返回 (格式标签, 字节流)，LoadsTyped 从格式标签和字节流反序列化。
// 格式标签标识序列化格式（"json"/"gob"），便于存储时记录格式，读取时按格式反序列化。
type Serializer interface {
	// DumpsTyped 序列化对象，返回 (格式标签, 字节流, 错误)
	DumpsTyped(obj any) (string, []byte, error)
	// LoadsTyped 反序列化对象，根据格式标签和字节流恢复
	LoadsTyped(formatTag string, data []byte) (any, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// serdeTuple 序列化元组，对应 Python 的 tuple[str, bytes]
type serdeTuple struct {
	// FormatTag 序列化格式标签
	FormatTag string
	// Data 序列化后的字节流
	Data []byte
}

// JSONSerializer JSON 序列化器实现。
// 对应 Python: openjiuwen/core/graph/store/serde.py (JsonSerializer)
type JSONSerializer struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJSONSerializer 创建 JSON 序列化器
func NewJSONSerializer() *JSONSerializer {
	return &JSONSerializer{}
}

// ──────────────────────────── JSONSerializer 方法 ────────────────────────────

// DumpsTyped 序列化为 JSON，返回 ("json", jsonBytes)
func (s *JSONSerializer) DumpsTyped(obj any) (string, []byte, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return "", nil, err
	}
	return "json", data, nil
}

// LoadsTyped 从 JSON 反序列化，仅处理格式标签为 "json" 的数据。
// 其他格式标签返回 (nil, nil)。
func (s *JSONSerializer) LoadsTyped(formatTag string, data []byte) (any, error) {
	if formatTag != "json" {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}
```

- [ ] **Step 2: 创建 serializer_test.go**

```go
package checkpointer

import (
	"testing"
)

// ──────────────────────────── JSONSerializer 测试 ────────────────────────────

// TestNewJSONSerializer 测试创建实例
func TestNewJSONSerializer(t *testing.T) {
	s := NewJSONSerializer()
	if s == nil {
		t.Fatal("NewJSONSerializer 返回 nil")
	}
}

// TestJSONSerializer_DumpsTyped_基本 测试基本序列化
func TestJSONSerializer_DumpsTyped_基本(t *testing.T) {
	s := NewJSONSerializer()
	tag, data, err := s.DumpsTyped(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("DumpsTyped 返回错误：%v", err)
	}
	if tag != "json" {
		t.Errorf("格式标签期望 'json'，实际=%s", tag)
	}
	if len(data) == 0 {
		t.Error("序列化数据不应为空")
	}
}

// TestJSONSerializer_DumpsTyped_嵌套结构 测试嵌套 map 序列化
func TestJSONSerializer_DumpsTyped_嵌套结构(t *testing.T) {
	s := NewJSONSerializer()
	obj := map[string]any{
		"global_state": map[string]any{"k1": "v1"},
		"agent_state":  map[string]any{"k2": 42},
	}
	tag, _, err := s.DumpsTyped(obj)
	if err != nil {
		t.Fatalf("DumpsTyped 返回错误：%v", err)
	}
	if tag != "json" {
		t.Errorf("格式标签期望 'json'，实际=%s", tag)
	}
}

// TestJSONSerializer_LoadsTyped_json标签 测试 json 格式标签反序列化
func TestJSONSerializer_LoadsTyped_json标签(t *testing.T) {
	s := NewJSONSerializer()
	// 先序列化
	_, data, err := s.DumpsTyped(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("DumpsTyped 返回错误：%v", err)
	}
	// 再反序列化
	result, err := s.LoadsTyped("json", data)
	if err != nil {
		t.Fatalf("LoadsTyped 返回错误：%v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际=%T", result)
	}
	if m["key"] != "value" {
		t.Errorf("key 期望 'value'，实际=%v", m["key"])
	}
}

// TestJSONSerializer_LoadsTyped_非json标签 测试非 json 格式标签返回 nil
func TestJSONSerializer_LoadsTyped_非json标签(t *testing.T) {
	s := NewJSONSerializer()
	result, err := s.LoadsTyped("pickle", []byte("data"))
	if err != nil {
		t.Fatalf("非 json 标签不应返回错误：%v", err)
	}
	if result != nil {
		t.Errorf("非 json 标签应返回 nil，实际=%v", result)
	}
}

// TestJSONSerializer_往返测试 测试序列化-反序列化往返一致性
func TestJSONSerializer_往返测试(t *testing.T) {
	s := NewJSONSerializer()
	original := map[string]any{
		"global_state": map[string]any{"name": "test"},
		"agent_state":  map[string]any{"count": float64(10)},
	}
	tag, data, err := s.DumpsTyped(original)
	if err != nil {
		t.Fatalf("DumpsTyped 错误：%v", err)
	}
	result, err := s.LoadsTyped(tag, data)
	if err != nil {
		t.Fatalf("LoadsTyped 错误：%v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际=%T", result)
	}
	if m["global_state"] == nil || m["agent_state"] == nil {
		t.Error("往返后丢失数据")
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/checkpointer/... -v -run TestJSONSerializer -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/checkpointer/serializer.go internal/agentcore/session/checkpointer/serializer_test.go
git commit -m "feat(checkpointer): 实现 Serializer 接口和 JSONSerializer"
```

---

### Task 3: 实现 InMemoryCheckpointer + Storage 子类

**Files:**
- Create: `internal/agentcore/session/checkpointer/inmemory.go`
- Create: `internal/agentcore/session/checkpointer/inmemory_test.go`

- [ ] **Step 1: 创建 inmemory.go**

这是最大的文件，包含 `InMemoryCheckpointer`、`baseSingleStateStorage`、`AgentStorage`、`AgentTeamStorage`、`WorkflowStorage`。

对照 Python `inmemory.py` 逐方法实现。注意：
- 所有方法加 `ctx context.Context`
- Graph Store 相关逻辑留空（`⤵️ 8.7 回填`）
- `AgentStorage.GetStateToSave` → `session.State().GetState()`（SessionState 接口自带 GetState）
- `AgentStorage.RestoreState` → `session.State().SetState(state)`（SessionState 接口自带 SetState）
- `AgentTeamStorage.GetStateToSave` → 断言 `*state.AgentStateCollection` 调用 `GetGlobal(state.AllStateKey)`（使用 AllStateKey 哨兵值，对齐 Python `get_global(None)`）
- `AgentTeamStorage.RestoreState` → 断言 `*state.AgentStateCollection` 调用 `GlobalState().SetState(state)`（GlobalState 返回 `*InMemoryStateLike`）
- `WorkflowStorage.Save/Recover` → 断言 `state.WorkflowState` 接口（而非 `*state.WorkflowCommitState` 具体类型），调用 `GetUpdates/SetUpdates/Commit/UpdateAndCommitWorkflowState`
- 日志按项目规则 3 对齐 Python 调用点
- `PreWorkflowExecute` 中的 `FORCE_DEL_WORKFLOW_STATE_KEY` 使用 interaction 包中已有的常量 `InteractiveInputKey` 同级定义，或从 session/constants（5.13 才实现）暂定义为包内常量

- [ ] **Step 2: 创建 inmemory_test.go**

测试用例覆盖：
- `NewInMemoryCheckpointer` 构造
- `GetThreadID` 委托
- `PreAgentExecute` / `PostAgentExecute` / `InterruptAgentExecute` — 保存/恢复 Agent 状态
- `PreAgentTeamExecute` / `PostAgentTeamExecute` — 保存/恢复 Team 状态
- `PreWorkflowExecute` / `PostWorkflowExecute` — 基本场景（正常完成/异常/中断）
- `SessionExists` — 存在/不存在
- `Release` — 全量释放/单 Agent 释放
- `GraphStore` — 返回 nil
- `AgentStorage` / `AgentTeamStorage` / `WorkflowStorage` 单独测试 Save/Recover/Clear/Exists

需要创建满足 `CheckpointerSession` 接口的测试 fake session（含可选 AgentID/TeamID）。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/checkpointer/... -v -run TestInMemory -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/checkpointer/inmemory.go internal/agentcore/session/checkpointer/inmemory_test.go
git commit -m "feat(checkpointer): 实现 InMemoryCheckpointer 和 AgentStorage/AgentTeamStorage/WorkflowStorage"
```

---

### Task 4: 实现 CheckpointerFactory + Config + Provider

**Files:**
- Create: `internal/agentcore/session/checkpointer/factory.go`
- Create: `internal/agentcore/session/checkpointer/factory_test.go`

- [ ] **Step 1: 创建 factory.go**

包含：
- `CheckpointerConfig` 结构体
- `CheckpointerProvider` 接口
- `CheckpointerFactory` 结构体 + 方法
- `inMemoryProvider` 内部实现
- 包级 `defaultFactory` 单例 + 全局便捷函数

对照 Python `checkpointer.py` 实现。

- [ ] **Step 2: 创建 factory_test.go**

测试用例覆盖：
- `NewCheckpointerFactory` — 自动注册 in_memory provider
- `Register` — 注册自定义 provider
- `Create` — in_memory 类型创建
- `Create` — 未知类型报错
- `GetCheckpointer` — 优先级：type缓存 → 默认 → InMemory 单例
- `SetDefaultCheckpointer` / `SetCheckpointer`
- 全局便捷函数

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/checkpointer/... -v -run TestFactory -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/checkpointer/factory.go internal/agentcore/session/checkpointer/factory_test.go
git commit -m "feat(checkpointer): 实现 CheckpointerFactory、CheckpointerProvider、CheckpointerConfig"
```

---

### Task 5: 回填 session/internal 包 — 类型从 any → Checkpointer

**Files:**
- Modify: `internal/agentcore/session/internal/agent_session.go`
- Modify: `internal/agentcore/session/internal/workflow_session.go`
- Modify: `internal/agentcore/session/internal/agent_session_test.go`
- Modify: `internal/agentcore/session/internal/workflow_session_test.go`

- [ ] **Step 1: 修改 agent_session.go**

改动点：
1. 导入 `checkpointer` 包
2. `checkpointer any` → `checkpointer checkpointer.Checkpointer`
3. `Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
4. `WithCheckpointer(cp any)` → `WithCheckpointer(cp checkpointer.Checkpointer)`
5. 删除 `⤵️ 5.8 回填：any → Checkpointer` 注释

- [ ] **Step 2: 修改 workflow_session.go**

改动点：
1. 导入 `checkpointer` 包
2. `internal.baseSession` 接口 `Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
3. `WorkflowSession.Checkpointer()` 无 parent 时从工厂获取：`return checkpointer.GetCheckpointer()`
4. `NodeSession.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
5. 注意 `State()` 返回类型已是 `state.SessionState`，无需修改
6. 删除 `⤵️ 5.8 回填` 注释

- [ ] **Step 3: 修改 agent_session_test.go**

改动点：
1. 导入 `checkpointer` 包
2. `WithCheckpointer("my-cp")` → 创建 mock Checkpointer 传入
3. `s.Checkpointer() != nil` 断言适配

- [ ] **Step 4: 修改 workflow_session_test.go**

改动点：
1. 导入 `checkpointer` 包
2. `WithCheckpointer("parent_checkpointer")` → 创建 mock Checkpointer 传入
3. 断言适配新类型

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/internal/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/internal/
git commit -m "refactor(session): 回填 internal 包 Checkpointer 类型 any → checkpointer.Checkpointer"
```

---

### Task 6: 回填 session 包 — BaseSession 接口 + ProxySession

**Files:**
- Modify: `internal/agentcore/session/session.go`
- Modify: `internal/agentcore/session/session_test.go`

- [ ] **Step 1: 修改 session.go**

改动点：
1. 导入 `checkpointer` 包
2. `BaseSession.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
3. 删除 `⤵️ 5.8 回填：返回类型从 any 改为 Checkpointer` 注释
4. `ProxySession.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`

- [ ] **Step 2: 修改 session_test.go**

改动点：
1. 导入 `checkpointer` 包
2. `mockStub.checkpointerVal any` → `checkpointerVal checkpointer.Checkpointer`
3. `mockStub.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
4. 测试断言适配：`"checkpointer-value"` → mock Checkpointer 实例

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/session.go internal/agentcore/session/session_test.go
git commit -m "refactor(session): 回填 BaseSession/ProxySession Checkpointer 类型"
```

---

### Task 7: 回填 interaction 包 — 删除 InteractionCheckpointer，改用 Checkpointer

**Files:**
- Modify: `internal/agentcore/session/interaction/base.go`
- Modify: `internal/agentcore/session/interaction/base_test.go`
- Modify: `internal/agentcore/session/interaction/interaction_test.go`
- Modify: `internal/agentcore/session/interaction/doc.go`

- [ ] **Step 1: 修改 interaction/base.go**

改动点：
1. 导入 `checkpointer` 包
2. `baseSession.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
3. 删除 `InteractionCheckpointer` 接口
4. `interruptAgentExecute` 函数改为：检查 `session.Checkpointer()` 非 nil 后直接调用 `cp.InterruptAgentExecute(ctx, session)`
5. 删除"5.8 实现后迁移到 session 包"注释

- [ ] **Step 2: 修改 interaction/base_test.go**

改动点：
1. 导入 `checkpointer` 包
2. `fakeBaseSession.cpValue any` → `cpValue checkpointer.Checkpointer`
3. `fakeBaseSession.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`
4. `fakeSessionWithoutExecID.cpValue any` → `cpValue checkpointer.Checkpointer`
5. `fakeSessionWithoutExecID.Checkpointer() any` → `Checkpointer() checkpointer.Checkpointer`

- [ ] **Step 3: 修改 interaction/interaction_test.go**

改动点：
1. 导入 `checkpointer` 包
2. `fakeCheckpointer` 改为实现 `checkpointer.Checkpointer` 接口（所有 11 个方法的桩实现）
3. 测试中 `session.cpValue = &fakeCheckpointer{}` 不变，但类型适配

- [ ] **Step 4: 修改 interaction/doc.go**

删除 `InteractionCheckpointer → 5.8 时迁移到 session 包` 说明。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/interaction/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/interaction/
git commit -m "refactor(interaction): 删除 InteractionCheckpointer，改用 checkpointer.Checkpointer"
```

---

### Task 8: 回填 agent.go — PreRun/Commit 调用 Checkpointer

**Files:**
- Modify: `internal/agentcore/session/agent.go`

- [ ] **Step 1: 修改 agent.go**

改动点：
1. 导入 `checkpointer` 包
2. `PreRun` 方法中，删除 `⤵️ 5.8 回填` 注释和"当前 checkpointer 为 nil，跳过"，添加：
   ```go
   if cp := s.inner.Checkpointer(); cp != nil {
       if err := cp.PreAgentExecute(ctx, s.inner, inputs); err != nil {
           return err
       }
   }
   ```
3. `Commit` 方法中，删除 `⤵️ 5.8 回填` 注释和"当前 checkpointer 为 nil，跳过"，添加：
   ```go
   if cp := s.inner.Checkpointer(); cp != nil {
       return cp.PostAgentExecute(ctx, s.inner)
   }
   ```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/ -v -count=1`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/agent.go
git commit -m "feat(session): 回填 PreRun/Commit 调用 Checkpointer 生命周期方法"
```

---

### Task 9: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/session/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 session/doc.go**

更新说明：Checkpointer 返回类型已从 `any` 改为 `checkpointer.Checkpointer`。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

改动点：
1. 5.8 状态 `☐` → `✅`
2. 5.1 中 `⤵️ Checkpointer 持久化待 5.8 回填` → `✅ Checkpointer 持久化已回填`
3. 5.2 中 `⤵️ Checkpointer 返回类型待 5.8 回填` → `✅ Checkpointer 返回类型已回填`
4. 5.3 中 `⤵️ Checkpointer 返回类型待 5.8 回填` → `✅ Checkpointer 返回类型已回填`
5. 5.4 中 `⤵️ Checkpointer 返回类型待 5.8 回填` → `✅ Checkpointer 返回类型已回填`
6. 5.5 中如有 `⤵️ 5.8` 标记也更新

- [ ] **Step 3: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md 5.8 完成标记"
```

---

### Task 10: 全量编译和覆盖率检查

**Files:** 无新增/修改

- [ ] **Step 1: 全量编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 2: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/session/checkpointer/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行 session 包全量测试（确保回填无回归）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -count=1`
Expected: ALL PASS

- [ ] **Step 4: 最终提交（如有遗漏修复）**

如有任何遗漏修复在此提交。
