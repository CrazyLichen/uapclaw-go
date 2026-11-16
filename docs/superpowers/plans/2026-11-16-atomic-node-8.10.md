# 8.10 AtomicNode + ComponentAbility 前置件 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地领域八 8.10 AtomicNode（节点级原子提交包装）+ 提前抽取 8.17 段的 ComponentAbility 枚举前置件。

**Architecture:** Python `graph/atomic_node.py` 双基类（AtomicNode/AsyncAtomicNode）按偏差 D1 合并为单个可嵌入 Go 结构体 `AtomicNode`，校验三分支一比一复刻（D2 注销第二道）；`_atomic_invoke` 抽象方法用构造期注入回调承载（D3）。ComponentAbility 枚举新建 `internal/agentcore/workflow/components` 子包。spec：`docs/superpowers/specs/2026-11-16-atomic-node-8.10-design.md`。

**Tech Stack:** Go 1.x、testify（assert/require）、`github.com/uapclaw/uapclaw-go/internal/common/exception`（BuildError/WithParam）、`internal/agentcore/session`（BaseSession=interfaces.InnerSession）、`internal/agentcore/session/state`（WorkflowState 接口）。

---

## 背景事实（工程师必读）

1. **Python 原文**：`openjiuwen/core/graph/atomic_node.py` 全文 48 行，无 logger 调用（零日志对齐，同 8.3 先例）；`openjiuwen/core/workflow/components/base.py:9-42` 是 ComponentAbility。
2. **错误码已存在**：`internal/common/exception/codes_graph.go:9` `StatusGraphStateCommitError = NewStatusCode("GRAPH_STATE_COMMIT_ERROR", 112030)`。
3. **状态接口已存在**：`internal/agentcore/session/state/state.go` 的 `WorkflowState` 接口（注释明确"Python: CommitState"），含 `CommitCmp()`；`WorkflowStateCollection`/`WorkflowCommitState` 实现它。
4. **会话接口已存在**：`internal/agentcore/session/session.go:28` `type BaseSession = interfaces.InnerSession`（方法含 `State() state.SessionState`）。
5. **依赖方向安全**：`internal/agentcore/session` 不 import `internal/agentcore/graph`（已验证 0 命中），graph→session 无循环。
6. **消费方现状**：本批落地后 8.10 无即时消费者（Vertex=8.11、Condition=8.19、LoopComp=8.20 均未开工），这是计划内的——它是被后续章节消费的地基件。
7. **日志纪律**：Python 源文件零 logger 调用 → Go 侧零日志对齐（不添加防御性日志，错误经返回值上抛；与 8.3 spec "零日志对齐 base.py" 同口径）。
8. **module 路径**：`github.com/uapclaw/uapclaw-go`（go.mod 已核实）。
9. **BaseError 断言 API**：`be.Status()` 取状态码、`be.Params()["reason"]` 取 WithParam 注入的参数。

## 文件结构

- Create: `internal/agentcore/graph/atomic_node.go` — AtomicNode 结构体 + 校验 + AtomicInvoke（Python atomic_node.py 一一对应）
- Create: `internal/agentcore/graph/atomic_node_test.go` — 单元测试
- Create: `internal/agentcore/workflow/components/doc.go` — components 子包文档
- Create: `internal/agentcore/workflow/components/base.go` — ComponentAbility 枚举（Python components/base.py 的枚举段）
- Create: `internal/agentcore/workflow/components/base_test.go` — 枚举映射表测试
- Modify: `internal/agentcore/graph/doc.go` — 文件树加 atomic_node.go 条目
- Modify: `IMPLEMENTATION_PLAN.md` — 8.10 打 ✅、8.11 ⤴️ 注销、追加十次记录序注

---

### Task 1: ComponentAbility 枚举（components 子包）

**Files:**
- Create: `internal/agentcore/workflow/components/doc.go`
- Create: `internal/agentcore/workflow/components/base.go`
- Test: `internal/agentcore/workflow/components/base_test.go`

- [ ] **Step 1: 写失败测试（枚举映射表全量断言）**

创建 `internal/agentcore/workflow/components/base_test.go`：

```go
package components

import "testing"

// TestComponentAbility_NameDesc 四成员的 Name/Desc 与 Python 元组全量对齐
// Python: components/base.py INVOKE=("invoke","batch in, batch out") 等
func TestComponentAbility_NameDesc(t *testing.T) {
	cases := []struct {
		ability ComponentAbility
		name    string
		desc    string
	}{
		{ComponentAbilityInvoke, "invoke", "batch in, batch out"},
		{ComponentAbilityStream, "stream", "batch in, stream out"},
		{ComponentAbilityCollect, "collect", "stream in, batch out"},
		{ComponentAbilityTransform, "transform", "stream in, stream out"},
	}
	for _, c := range cases {
		if got := c.ability.Name(); got != c.name {
			t.Errorf("Name() = %q, want %q", got, c.name)
		}
		if got := c.ability.Desc(); got != c.desc {
			t.Errorf("Desc() = %q, want %q", got, c.desc)
		}
	}
}

// TestComponentAbility_未知值 未知成员返回空串（Go 无 Enum ValueError 对应物，空串承载）
func TestComponentAbility_未知值(t *testing.T) {
	var unknown ComponentAbility = 999
	if got := unknown.Name(); got != "" {
		t.Errorf("未知值 Name() = %q, want 空", got)
	}
	if got := unknown.Desc(); got != "" {
		t.Errorf("未知值 Desc() = %q, want 空", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/workflow/components/ -v`
Expected: 编译 FAIL（ComponentAbility 未定义）

- [ ] **Step 3: 写实现**

创建 `internal/agentcore/workflow/components/base.go`：

```go
package components

// ──────────────────────────── 枚举 ────────────────────────────

// ComponentAbility 组件能力，表示组件执行支持的 I/O 模式。
// Python: openjiuwen/core/workflow/components/base.py ComponentAbility(Enum)
// 成员为 (name, desc) 元组：name 供事件/trace 使用，desc 为人类可读说明。
type ComponentAbility int

const (
	// ComponentAbilityInvoke 同步批处理（batch in, batch out）
	ComponentAbilityInvoke ComponentAbility = iota
	// ComponentAbilityStream 流式输出（batch in, stream out）
	ComponentAbilityStream
	// ComponentAbilityCollect 聚合输出（stream in, batch out）
	ComponentAbilityCollect
	// ComponentAbilityTransform 流到流（stream in, stream out）
	ComponentAbilityTransform
)

// Name 返回能力内部名。
// Python: ComponentAbility.name 属性
func (a ComponentAbility) Name() string {
	switch a {
	case ComponentAbilityInvoke:
		return "invoke"
	case ComponentAbilityStream:
		return "stream"
	case ComponentAbilityCollect:
		return "collect"
	case ComponentAbilityTransform:
		return "transform"
	default:
		return ""
	}
}

// Desc 返回能力说明。
// Python: ComponentAbility.desc 属性
func (a ComponentAbility) Desc() string {
	switch a {
	case ComponentAbilityInvoke:
		return "batch in, batch out"
	case ComponentAbilityStream:
		return "batch in, stream out"
	case ComponentAbilityCollect:
		return "stream in, batch out"
	case ComponentAbilityTransform:
		return "stream in, stream out"
	default:
		return ""
	}
}
```

创建 `internal/agentcore/workflow/components/doc.go`：

```go
// Package components 提供工作流组件的共享类型定义，对齐 openjiuwen/core/workflow/components 包。
//
// 当前仅含 ComponentAbility 枚举（8.10 前置件，8.11 Vertex 的事件 ability 字段
// 与组件分派消费）。同 Python 文件中的 WorkflowComponentMetadata/ComponentConfig/
// ComponentState 属 8.17 段范围，届时随 StartComp/EndComp 一并落地，不在本包提前实现。
//
// 文件目录：
//
//	components/
//	├── doc.go      # 包文档
//	└── base.go    # ComponentAbility 枚举
//
// 对应 Python 代码：openjiuwen/core/workflow/components/base.py
package components
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/workflow/components/ -v`
Expected: PASS（2 个用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/workflow/components/doc.go internal/agentcore/workflow/components/base.go internal/agentcore/workflow/components/base_test.go
git commit -m "feat(workflow): 8.10 前置件 ComponentAbility 枚举（components 子包）"
```

---

### Task 2: AtomicNode 校验函数（TDD）

**Files:**
- Create: `internal/agentcore/graph/atomic_node_test.go`
- Create: `internal/agentcore/graph/atomic_node.go`（本任务先落校验函数与结构体骨架）

- [ ] **Step 1: 写失败测试（校验三分支 + NewAtomicNode panic）**

创建 `internal/agentcore/graph/atomic_node_test.go`：

```go
package graph

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// fakeAtomicState 测试用可提交状态：嵌入 SessionState 补齐接口面，仅 CommitCmp 有实现。
type fakeAtomicState struct {
	state.SessionState
	commitCmpCalled bool
}

func (s *fakeAtomicState) CommitCmp() { s.commitCmpCalled = true }

// nonCommitState 不可提交状态：只嵌入 SessionState，不实现 WorkflowState（CommitCmp），
// 用于校验分支3。
type nonCommitState struct {
	state.SessionState
}

// fakeAtomicSession 测试用最小会话：嵌入 BaseSession（= interfaces.InnerSession）
// 补齐接口面，仅 State() 有实现。
type fakeAtomicSession struct {
	session.BaseSession
	st state.SessionState
}

func (s *fakeAtomicSession) State() state.SessionState { return s.st }

// TestValidateSessionAndState 校验三分支一比一复刻 atomic_node.py:13-22，
// 顺序不可调换：nil →（D2 注销）→ state 非 WorkflowState。
func TestValidateSessionAndState(t *testing.T) {
	// 分支1：session 为 nil → "session is None"
	_, err := validateSessionAndState(nil)
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok)
	assert.Equal(t, exception.StatusGraphStateCommitError, be.Status())
	assert.Equal(t, "session is None", be.Params()["reason"])

	// 分支3：state 非 WorkflowState → "session is not support commit state"
	//（分支2 isinstance(BaseSession) 已在 Go 编译期由接口参数类型保证，偏差 D2 注销）
	sess := &fakeAtomicSession{st: &nonCommitState{}}
	_, err = validateSessionAndState(sess)
	require.Error(t, err)
	be = err.(*exception.BaseError)
	assert.Equal(t, exception.StatusGraphStateCommitError, be.Status())
	assert.Equal(t, "session is not support commit state", be.Params()["reason"])

	// 成功路径：返回断言后的 WorkflowState
	sessOK := &fakeAtomicSession{st: &fakeAtomicState{}}
	ws, err := validateSessionAndState(sessOK)
	require.NoError(t, err)
	require.NotNil(t, ws)
}

// TestNewAtomicNode_Nil panic 对齐 Python 抽象方法必须实现的约束（偏差 D3）
func TestNewAtomicNode_Nil(t *testing.T) {
	assert.Panics(t, func() { NewAtomicNode(nil) })
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/graph/ -run "TestValidateSessionAndState|TestNewAtomicNode_Nil" -v`
Expected: 编译 FAIL（validateSessionAndState/NewAtomicNode 未定义）

- [ ] **Step 3: 写实现（结构体 + 构造 + 校验函数）**

创建 `internal/agentcore/graph/atomic_node.go`：

```go
package graph

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AtomicNode 原子节点：节点级状态提交的原子性包装。
// Python: openjiuwen/core/graph/atomic_node.py。Python 的 AtomicNode/AsyncAtomicNode
// 双基类按 spec 偏差 D1 合并为单结构体（包装逻辑逐字相同，async 语义由 8.11 Vertex
// 的执行管线承载）；_atomic_invoke 抽象方法由构造期注入回调承载（偏差 D3）。
type AtomicNode struct {
	// atomicInvokeFunc 原子执行回调（Python 抽象方法 _atomic_invoke）
	atomicInvokeFunc func(ctx context.Context, kwargs map[string]any) (any, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAtomicNode 创建原子节点。atomicInvokeFunc 为 nil 时 panic，
// 对齐 Python ABC 抽象方法 _atomic_invoke 必须被实现的约束（偏差 D3）。
func NewAtomicNode(atomicInvokeFunc func(ctx context.Context, kwargs map[string]any) (any, error)) AtomicNode {
	if atomicInvokeFunc == nil {
		panic("atomicInvokeFunc 不能为 nil（对齐 Python 抽象方法 _atomic_invoke 必须实现）")
	}
	return AtomicNode{atomicInvokeFunc: atomicInvokeFunc}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateSessionAndState 校验会话与状态可提交（atomic_node.py:13-22 一比一，
// 三道检查顺序不可调换）。返回断言后的 WorkflowState 供 AtomicInvoke 提交复用，
// 避免二次断言（Python 侧由属性访问天然获得，Go 需显式传递）。
// 偏差 D2：Python 第二道 isinstance(session, BaseSession) 检查在 Go 由接口参数
// 类型编译期保证，注销。
func validateSessionAndState(sess session.BaseSession) (state.WorkflowState, error) {
	if sess == nil {
		return nil, exception.BuildError(exception.StatusGraphStateCommitError,
			exception.WithParam("reason", "session is None"))
	}

	ws, ok := sess.State().(state.WorkflowState)
	if !ok {
		return nil, exception.BuildError(exception.StatusGraphStateCommitError,
			exception.WithParam("reason", "session is not support commit state"))
	}

	return ws, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/graph/ -run "TestValidateSessionAndState|TestNewAtomicNode_Nil" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/atomic_node.go internal/agentcore/graph/atomic_node_test.go
git commit -m "feat(graph): 8.10 AtomicNode 校验与构造（偏差 D2/D3）"
```

---

### Task 3: AtomicInvoke 主体（TDD）

**Files:**
- Modify: `internal/agentcore/graph/atomic_node_test.go`
- Modify: `internal/agentcore/graph/atomic_node.go`

- [ ] **Step 1: 写失败测试（成功/失败/顺序锁定）**

`atomic_node_test.go` 追加：

```go
// TestAtomicInvoke_Success 成功路径：回调返回值透传 + CommitCmp 在回调之后被调用
func TestAtomicInvoke_Success(t *testing.T) {
	st := &fakeAtomicState{}
	sess := &fakeAtomicSession{st: st}
	called := false
	node := NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		called = true
		assert.False(t, st.commitCmpCalled, "commit 必须发生在回调之后")
		assert.Equal(t, sess, kwargs["session"])
		return "ok", nil
	})
	result, err := node.AtomicInvoke(context.Background(), map[string]any{"session": sess})
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
	assert.True(t, called)
	assert.True(t, st.commitCmpCalled, "回调成功后必须提交")
}

// TestAtomicInvoke_回调错误 回调失败：错误原样上抛、不提交
func TestAtomicInvoke_回调错误(t *testing.T) {
	st := &fakeAtomicState{}
	sess := &fakeAtomicSession{st: st}
	sentinel := assert.AnError
	node := NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		return nil, sentinel
	})
	result, err := node.AtomicInvoke(context.Background(), map[string]any{"session": sess})
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, result)
	assert.False(t, st.commitCmpCalled, "失败不得提交")
}

// TestAtomicInvoke_校验失败 session 缺失：不触碰回调、不提交、错误码与 reason 锁定
func TestAtomicInvoke_校验失败(t *testing.T) {
	called := false
	node := NewAtomicNode(func(ctx context.Context, kwargs map[string]any) (any, error) {
		called = true
		return nil, nil
	})
	result, err := node.AtomicInvoke(context.Background(), map[string]any{})
	require.Error(t, err)
	assert.Nil(t, result)
	assert.False(t, called, "校验失败不得进入回调")
	be := err.(*exception.BaseError)
	assert.Equal(t, exception.StatusGraphStateCommitError, be.Status())
	assert.Equal(t, "session is None", be.Params()["reason"])
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/graph/ -run TestAtomicInvoke -v`
Expected: 编译 FAIL（AtomicInvoke 未定义）

- [ ] **Step 3: 写实现**

`atomic_node.go` 导出函数区追加（步骤一比一复刻 atomic_node.py:26-31）：

```go
// AtomicInvoke 原子调用：校验会话与状态 → 执行回调 → 提交暂存 → 返回结果。
// Python: AtomicNode.atomic_invoke / AsyncAtomicNode.atomic_invoke（偏差 D1 合并），
// 步骤顺序对齐 atomic_node.py:26-31，任一步失败不提交、错误原样上抛。
func (a *AtomicNode) AtomicInvoke(ctx context.Context, kwargs map[string]any) (any, error) {
	// Python: session = kwargs.get("session", None)
	var sess session.BaseSession
	if raw, ok := kwargs["session"].(session.BaseSession); ok {
		sess = raw
	}

	// Python: _validate_session_and_state(session)
	ws, err := validateSessionAndState(sess)
	if err != nil {
		return nil, err
	}

	// Python: result = self._atomic_invoke(**kwargs)
	result, err := a.atomicInvokeFunc(ctx, kwargs)
	if err != nil {
		return nil, err
	}

	// Python: session.state().commit_cmp()
	ws.CommitCmp()

	return result, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/graph/ -run TestAtomicInvoke -v`
Expected: PASS（3 个用例）

- [ ] **Step 5: 覆盖率检查**

Run: `go test ./internal/agentcore/graph/ -cover`
Expected: atomic_node.go 覆盖率 ≥85%（目标接近 100%）；不足则补用例

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/graph/atomic_node.go internal/agentcore/graph/atomic_node_test.go
git commit -m "feat(graph): 8.10 AtomicInvoke 主体（校验→回调→CommitCmp 一比一）"
```

---

### Task 4: doc.go 更新

**Files:**
- Modify: `internal/agentcore/graph/doc.go`

- [ ] **Step 1: 更新包文档**

`internal/agentcore/graph/doc.go`：包概述段末尾追加一句：

```go
// AtomicNode（atomic_node.go）为节点级原子提交包装（Python atomic_node.py，
// 8.10），消费方为 8.11 Vertex 与 8.19/8.20 条件组件；当前无即时消费者，
// 属后续章节的地基件。
```

文件目录树改为：

```go
//	graph/
//	├── doc.go            # 包文档
//	├── atomic_node.go   # AtomicNode 原子提交包装（8.10）
//	└── graph_state.go   # GraphState 节点输出聚合 schema
```

- [ ] **Step 2: 编译验证**

Run: `go build ./internal/agentcore/graph/...`
Expected: 无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/graph/doc.go
git commit -m "docs(graph): doc.go 收录 atomic_node.go（8.10）"
```

---

### Task 5: IMPLEMENTATION_PLAN 同步

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 8.10 行**

将（约第 515 行）：

```
| 8.10 | ☐ | AtomicNode | 原子节点（强制校验 CommitState 后 `CommitCmp`） | `openjiuwen/core/graph/atomic_node.py` |
```

改为：

```
| 8.10 | ✅ | AtomicNode | ✅ Go 事实（2026-11-16）：`internal/agentcore/graph/atomic_node.go` 提供 `AtomicNode`（单结构体承载 Python 双基类，偏差 D1）+ `NewAtomicNode`（nil 回调 panic，偏差 D3）+ `AtomicInvoke`（kwargs 取 session→校验→回调→`WorkflowState.CommitCmp`→返回，atomic_node.py:26-31 步骤一比一）+ `validateSessionAndState`（三分支一比一、D2 注销 isinstance(BaseSession)、断言 `state.WorkflowState`≙Python CommitState）；Python 源零 logger 故零日志对齐；错误码复用 `StatusGraphStateCommitError(112030)`；当前无即时消费者（Vertex=8.11/Condition=8.19/LoopComp=8.20 后续章节），覆盖率 atomic_node.go 100%；另 8.10 前置件兑现：`internal/agentcore/workflow/components/base.go` ComponentAbility 四成员枚举（8.11 ⤴️ 已注销）；设计：docs/superpowers/specs/2026-11-16-atomic-node-8.10-design.md（偏差 D1-D3 见 §6） | `openjiuwen/core/graph/atomic_node.py` |
```

（覆盖率数字以 Task 3 Step 5 实测为准替换。）

- [ ] **Step 2: 注销 8.11 的 ComponentAbility ⤴️**

将 8.11 行中的：

```
⤴️ 依赖 8.17 段的 `ComponentAbility` 枚举提前抽取
```

改为：

```
✅ 已回填：`ComponentAbility` 枚举已随 8.10 前置件落地（internal/agentcore/workflow/components/base.go，2026-11-16）
```

- [ ] **Step 3: 追加十次记录序注**

在"九次记录"注记块之后追加：

```
> **8.x 实施顺序（2026-11-16 十次记录）**：8.10 与 ComponentAbility 前置件落地（spec docs/superpowers/specs/2026-11-16-atomic-node-8.10-design.md §2 方案 B+①）；新实施序 8.12（StreamActor，含 session ActorManager ⤴️ 回填）→ 8.11（Vertex，依赖 StreamConsumer 属 8.12）→ 8.1/8.2 → 8.13-8.26 组件。原注记保留。
```

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 8.10 完成 + 8.11 ComponentAbility ⤴️ 注销 + 十次记录"
```

---

### Task 6: 全量回归 + 推送

- [ ] **Step 1: 全量回归（CI 同款标签，memory 包 FTS 需要）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
Expected: 零 FAIL

- [ ] **Step 2: vet**

Run: `go vet ./...`
Expected: 无错误

- [ ] **Step 3: 推送**

```bash
git push origin main
```

（若遇 non-fast-forward 先 `git pull --rebase origin main` 再推。按规则 6/11：推送即交付，不追 CI。）

---

## 删除清单

无（本批为纯新增，无被替代旧实现；实施前可 grep 验证 `AtomicNode` 在 internal/ 下 0 命中——pregel 包的检查点 GraphState 与本 AtomicNode 无符号交集）。
