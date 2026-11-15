# 8.9a Graph Store 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 8.9a Graph Store（`internal/agentcore/graph/store/` 新包：GraphState/Store/GraphStore 壳/InMemoryStore/serde）+ `pregel.Message.Copy` 深拷贝 + 5.8 checkpointer `GraphStore() any` 收窄与 Delete 回填。

**Architecture:** store 包与 pregel 平级、依赖单向 store→pregel；深拷贝由 pregel 包内 `Message.Copy()`（保留非导出 kind）+ store 包 `GraphState.Clone()` 承担；serde 用 JSON 承担 pickle 角色、`CreateSerializer("json")` 复刻 Python raise 语义。

**Tech Stack:** Go 1.x 标准库（strings/encoding-json 不直接用于 serde 内核）、项目 logger（ComponentAgentCore）。

**Spec:** `docs/superpowers/specs/2026-11-15-graph-store-8.9a-design.md`

**测试纪律（全计划适用）：** 单包测试裸跑 `go test ./internal/agentcore/graph/store/...`；最终全量回归必须 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`（testing-build-tags 记忆）。

---

### Task 1: `pregel.Message.Copy()` 深拷贝方法

**Files:**
- Modify: `internal/agentcore/graph/pregel/base.go`（Message 定义后追加方法，归入导出函数区）
- Test: `internal/agentcore/graph/pregel/base_test.go`

- [ ] **Step 1: 写失败测试**（追加到 `base_test.go` 末尾）

```go
// TestMessage_Copy 验证 Copy 深拷贝且 kind 保留（8.9a GraphState.Clone 的地基）
func TestMessage_Copy(t *testing.T) {
	trigger := NewTriggerMessage("a", "n1")
	barrier := NewBarrierMessage("b", "barrier:a&b->c")

	ct := trigger.Copy()
	if ct.Sender != "a" || ct.Target != "n1" || ct.kind != MessageKindTrigger {
		t.Fatalf("trigger Copy 字段失真: %+v kind=%s", ct, ct.kind)
	}
	cb := barrier.Copy()
	if cb.Sender != "b" || cb.Target != "barrier:a&b->c" || cb.kind != MessageKindBarrier {
		t.Fatalf("barrier Copy 字段失真: %+v kind=%s", cb, cb.kind)
	}

	// 修改副本不影响原值（值语义本身隔离，但 Sender 字符串为引用型确认）
	ct.Sender = "changed"
	if trigger.Sender != "a" {
		t.Fatal("Copy 修改副本影响原值")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestMessage_Copy -v`
Expected: FAIL（`trigger.Copy undefined`）

- [ ] **Step 3: 实现**（`base.go` 导出函数区，`NewBarrierMessage` 之后）

```go
// Copy 返回 Message 的深拷贝，kind 原样保留。
// 供 8.9a store.GraphState.Clone 使用；Python 侧由 copy.deepcopy 承担同等语义。
// Python: 无对应方法（语言层 deepcopy），偏差见 8.9a spec §3.6 D-2
func (m Message) Copy() Message {
	c := Message{Sender: m.Sender, Target: m.Target, Payload: m.Payload}
	c.kind = m.kind
	return c
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestMessage_Copy -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git commit -m "feat(pregel): Message.Copy 深拷贝方法（8.9a 前置，kind 原样保留）"
```

---

### Task 2: store 包 base.go（PendingNode/GraphState/Store/CreateState/Clone）

**Files:**
- Create: `internal/agentcore/graph/store/base.go`
- Test: `internal/agentcore/graph/store/base_test.go`

- [ ] **Step 1: 写失败测试**

```go
package store

import (
	"errors"
	"testing"

	"uapclaw/internal/agentcore/graph/pregel"
)

// TestGraphState_IsResumable 三条件判定
func TestGraphState_IsResumable(t *testing.T) {
	empty := &GraphState{}
	if empty.IsResumable() {
		t.Fatal("空状态不应 resumable")
	}
	withNode := &GraphState{PendingNode: map[string]PendingNode{"n": {NodeName: "n", Status: "interrupt"}}}
	if !withNode.IsResumable() {
		t.Fatal("有 pending_node 应 resumable")
	}
	withBuf := &GraphState{PendingBuffer: []pregel.Message{pregel.NewTriggerMessage("a", "n")}}
	if !withBuf.IsResumable() {
		t.Fatal("有 pending_buffer 应 resumable")
	}
	withCV := &GraphState{ChannelValues: map[string][]pregel.ChannelSnapshot{"n": {{}}}}
	if !withCV.IsResumable() {
		t.Fatal("有 channel_values 应 resumable")
	}
}

// TestCreateState_nil 兜底三容器为空容器非 nil
func TestCreateState_nil(t *testing.T) {
	s := CreateState("ns1", 3, nil, nil, nil, nil)
	if s.Ns != "ns1" || s.Step != 3 {
		t.Fatalf("基础字段失真: %+v", s)
	}
	if s.PendingBuffer == nil || len(s.PendingBuffer) != 0 {
		t.Fatal("pending_buffer 应兜底为空切片")
	}
	if s.PendingNode == nil || len(s.PendingNode) != 0 {
		t.Fatal("pending_node 应兜底为空 map")
	}
	if s.NodeVersion == nil || len(s.NodeVersion) != 0 {
		t.Fatal("node_version 应兜底为空 map")
	}
}

// TestGraphState_Clone 深拷贝独立性
func TestGraphState_Clone(t *testing.T) {
	orig := &GraphState{
		Ns:   "ns",
		Step: 2,
		ChannelValues: map[string][]pregel.ChannelSnapshot{
			"n1": {{Messages: []pregel.Message{pregel.NewTriggerMessage("a", "n1")}, Received: []string{"a"}}},
		},
		PendingBuffer: []pregel.Message{pregel.NewBarrierMessage("b", "barrier:x&y->z")},
		PendingNode:   map[string]PendingNode{"n2": {NodeName: "n2", Status: "error", Exception: []error{errors.New("boom")}}},
		NodeVersion:   map[string]int{"n1": 1},
	}
	cp := orig.Clone()

	// 改副本不影响原值
	cp.ChannelValues["n1"][0].Messages[0].Sender = "changed"
	cp.ChannelValues["n1"][0].Received[0] = "changed"
	cp.PendingBuffer[0].Target = "changed"
	cp.PendingNode["n2"].Status = "changed"
	cp.NodeVersion["n1"] = 99

	if orig.ChannelValues["n1"][0].Messages[0].Sender != "a" {
		t.Fatal("ChannelValues 未深拷贝")
	}
	if orig.ChannelValues["n1"][0].Received[0] != "a" {
		t.Fatal("Received 未深拷贝")
	}
	if orig.PendingBuffer[0].Target != "barrier:x&y->z" {
		t.Fatal("PendingBuffer 未深拷贝")
	}
	if orig.PendingNode["n2"].Status != "error" {
		t.Fatal("PendingNode 值未隔离")
	}
	if orig.NodeVersion["n1"] != 1 {
		t.Fatal("NodeVersion 值未隔离")
	}

	// nil 状态 Clone 返回 nil
	var nilState *GraphState
	if nilState.Clone() != nil {
		t.Fatal("nil Clone 应返回 nil")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/store/ -v`
Expected: FAIL（包不存在/类型未定义）

- [ ] **Step 3: 实现 `base.go`**

```go
// Package store 提供 Pregel 引擎的检查点状态持久化地基：GraphState 快照、
// Store 存储抽象、GraphStore 日志壳、InMemoryStore 内存实现与 serde 序列化。
// 依赖方向 store→pregel 单向（对齐 Python TYPE_CHECKING 的 Message 引用）。
//
// 文件目录：
//
//	store/
//	├── doc.go        # 包文档
//	├── base.go       # PendingNode/GraphState/Store/CreateState/GraphStore 壳
//	├── inmemory.go   # InMemoryStore 内存实现
//	└── serde.go      # Serializer 接口与 JSON 实现
//
// 对应 Python 代码：openjiuwen/core/graph/store/
package store

import (
	"uapclaw/internal/agentcore/graph/pregel"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingNode 未完成节点的持久化记录
// Python: store/base.py:26-29
type PendingNode struct {
	// NodeName 节点名
	NodeName string
	// Status 任务状态（取值对齐 8.3d 的 TaskStatusInterrupt/TaskStatusError 字面量）
	Status string
	// Exception 累积的异常列表（Python 默认 None，Go nil 等价）
	Exception []error
}

// GraphState 引擎检查点状态快照。
// ⚠️ 与 8.8 graph_state.GraphState（节点返回值聚合）同名不同义，Go 靠包隔离。
// Python: store/base.py:32-39
type GraphState struct {
	// Ns 命名空间
	Ns string
	// Step super-step 序号
	Step int
	// ChannelValues 通道快照（Python Dict[str, Any]，按计划行收窄，偏差 D-5）
	ChannelValues map[string][]pregel.ChannelSnapshot
	// PendingBuffer 待发消息
	PendingBuffer []pregel.Message
	// PendingNode 未完成节点表
	PendingNode map[string]PendingNode
	// NodeVersion 节点版本
	NodeVersion map[string]int
}

// Store 图状态存储抽象
// Python: store/base.py:59-68（async 三方法，Go 同步化见偏差 D-4）
type Store interface {
	// Get 读取状态，未命中返回 (nil, nil)
	Get(sessionID, ns string) (*GraphState, error)
	// Save 保存状态
	Save(sessionID, ns string, state *GraphState) error
	// Delete 删除状态；ns 为空串时删整个 session（Python Optional[str]=None，偏差 D-3）
	Delete(sessionID, ns string) error
}

// GraphStore 日志壳：包装 Store 并按 Python 逐条复刻日志与异常透传。
// Python: store/base.py:80-159
type GraphStore struct {
	// saver 被包装的真实存储
	saver Store
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateState 工厂：nil 入参兜底为空容器（对齐 Python or [] / or {} 语义）
// Python: store/base.py:71-82
func CreateState(ns string, step int, channelSnapshot map[string][]pregel.ChannelSnapshot,
	pendingBuffer []pregel.Message, pendingNode map[string]PendingNode, nodeVersion map[string]int) *GraphState {
	if pendingBuffer == nil {
		pendingBuffer = []pregel.Message{}
	}
	if pendingNode == nil {
		pendingNode = map[string]PendingNode{}
	}
	if nodeVersion == nil {
		nodeVersion = map[string]int{}
	}
	return &GraphState{
		Ns:            ns,
		Step:          step,
		ChannelValues: channelSnapshot,
		PendingBuffer: pendingBuffer,
		PendingNode:   pendingNode,
		NodeVersion:   nodeVersion,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 空置位：Clone 归属 GraphState 方法见下方（Go 方法须与类型同区块，此处保持声明序）
```

> 注：`IsResumable` 与 `Clone` 是 `GraphState` 方法、`NewGraphStore` 是 `GraphStore` 构造函数，按规范 2 方法跟在类型声明区之后的导出函数区（同 pregel 包现有惯例），实现如下（紧接上面文件）：

```go
// IsResumable 有未完成节点/待发消息或 channel 值时引擎 init 才走恢复分支。
// 恢复判定必须与 8.3c PregelLoop._is_resume 同口径，两处经本方法共享，避免口径漂移。
// Python: store/base.py:42-47
func (s *GraphState) IsResumable() bool {
	return len(s.PendingNode) > 0 || len(s.PendingBuffer) > 0 || len(s.ChannelValues) > 0
}

// Clone 返回 GraphState 的深拷贝（Python copy.deepcopy 等价物，偏差 D-2）。
// nil 接收者返回 nil。
func (s *GraphState) Clone() *GraphState {
	if s == nil {
		return nil
	}
	c := &GraphState{Ns: s.Ns, Step: s.Step}
	if s.ChannelValues != nil {
		c.ChannelValues = make(map[string][]pregel.ChannelSnapshot, len(s.ChannelValues))
		for k, snaps := range s.ChannelValues {
			csnaps := make([]pregel.ChannelSnapshot, len(snaps))
			for i, sn := range snaps {
				csnaps[i] = cloneSnapshot(sn)
			}
			c.ChannelValues[k] = csnaps
		}
	}
	if s.PendingBuffer != nil {
		c.PendingBuffer = make([]pregel.Message, len(s.PendingBuffer))
		for i, m := range s.PendingBuffer {
			c.PendingBuffer[i] = m.Copy()
		}
	}
	if s.PendingNode != nil {
		c.PendingNode = make(map[string]PendingNode, len(s.PendingNode))
		for k, pn := range s.PendingNode {
			if pn.Exception != nil {
				exc := make([]error, len(pn.Exception))
				copy(exc, pn.Exception)
				pn.Exception = exc
			}
			c.PendingNode[k] = pn
		}
	}
	if s.NodeVersion != nil {
		c.NodeVersion = make(map[string]int, len(s.NodeVersion))
		for k, v := range s.NodeVersion {
			c.NodeVersion[k] = v
		}
	}
	return c
}

// NewGraphStore 构造日志壳
// Python: store/base.py:81-83（__init__）
func NewGraphStore(saver Store) *GraphStore {
	return &GraphStore{saver: saver}
}
```

> `cloneSnapshot` 是包内非导出助手（Python deepcopy 隐式承担，登记偏差 D-2 的组成部分）：

```go
// cloneSnapshot 深拷贝单个通道快照
func cloneSnapshot(sn pregel.ChannelSnapshot) pregel.ChannelSnapshot {
	c := pregel.ChannelSnapshot{}
	if sn.Messages != nil {
		c.Messages = make([]pregel.Message, len(sn.Messages))
		for i, m := range sn.Messages {
			c.Messages[i] = m.Copy()
		}
	}
	if sn.Received != nil {
		c.Received = make([]string, len(sn.Received))
		copy(c.Received, sn.Received)
	}
	return c
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/store/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/store/base.go internal/agentcore/graph/store/base_test.go internal/agentcore/graph/store/doc.go
git commit -m "feat(store): GraphState/Store/CreateState/Clone 与包骨架（8.9a base.py）"
```

> 注意：doc.go 的包注释在 Task 2 落地（见上方 base.go 头部注释内容——**实际是 doc.go 文件**，需将头注释移到独立 `doc.go`，`base.go` 只留 `package store` 与 import）。执行时按项目规则 1 建独立 `doc.go`。

---

### Task 3: inmemory.go（InMemoryStore）

**Files:**
- Create: `internal/agentcore/graph/store/inmemory.go`
- Test: `internal/agentcore/graph/store/inmemory_test.go`

- [ ] **Step 1: 写失败测试**

```go
package store

import "testing"

// newFilledStore 造一条已保存状态
func newFilledStore(t *testing.T) *InMemoryStore {
	s := NewInMemoryStore()
	if err := s.Save("sess", "ns1", &GraphState{Ns: "ns1", Step: 1}); err != nil {
		t.Fatalf("save 失败: %v", err)
	}
	return s
}

// TestInMemoryStore_Get 命中返回副本；未命中 (nil, nil)
func TestInMemoryStore_Get(t *testing.T) {
	s := newFilledStore(t)
	got, err := s.Get("sess", "ns1")
	if err != nil || got == nil || got.Ns != "ns1" {
		t.Fatalf("命中失败: %v %+v", err, got)
	}
	got.Step = 99
	again, _ := s.Get("sess", "ns1")
	if again.Step != 1 {
		t.Fatal("Get 应返回副本")
	}
	miss, err := s.Get("sess", "nope")
	if err != nil || miss != nil {
		t.Fatalf("未命中应为 (nil,nil): %v %+v", err, miss)
	}
}

// TestInMemoryStore_Save 存副本，外部改动不影响内部
func TestInMemoryStore_Save(t *testing.T) {
	s := NewInMemoryStore()
	st := &GraphState{Ns: "ns1", Step: 1}
	if err := s.Save("sess", "ns1", st); err != nil {
		t.Fatal(err)
	}
	st.Step = 42
	again, _ := s.Get("sess", "ns1")
	if again.Step != 1 {
		t.Fatal("Save 应存副本")
	}
}

// TestInMemoryStore_Delete 三分支：无 session / 全删 / 前缀删 + 删空连带
func TestInMemoryStore_Delete(t *testing.T) {
	// 无 session：静默
	s := NewInMemoryStore()
	if err := s.Delete("ghost", "ns"); err != nil {
		t.Fatal(err)
	}

	// 全删（ns 空串）
	s = newFilledStore(t)
	_ = s.Save("sess", "ns2", &GraphState{Ns: "ns2", Step: 1})
	if err := s.Delete("sess", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("sess", "ns1"); got != nil {
		t.Fatal("全删后 ns1 仍在")
	}

	// 前缀删：ns=ns1 应只删 ns1 开头的（含 ns10），ns2 保留；删空后外层键连带删除
	s = newFilledStore(t)
	_ = s.Save("sess", "ns10", &GraphState{Ns: "ns10", Step: 1})
	_ = s.Save("sess", "ns2", &GraphState{Ns: "ns2", Step: 1})
	if err := s.Delete("sess", "ns1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("sess", "ns1"); got != nil {
		t.Fatal("前缀删后 ns1 仍在")
	}
	if got, _ := s.Get("sess", "ns10"); got != nil {
		t.Fatal("前缀删应命中 ns10")
	}
	if got, _ := s.Get("sess", "ns2"); got == nil {
		t.Fatal("ns2 不应被删")
	}
	if err := s.Delete("sess", "ns2"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.store["sess"]; ok {
		t.Fatal("session 删空后应连带删除外层键")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/store/ -run InMemoryStore -v`
Expected: FAIL（`NewInMemoryStore undefined`）

- [ ] **Step 3: 实现**

```go
package store

import "strings"

// InMemoryStore 内存图状态存储：{ session_id: { ns: state } }
// Python: store/inmemory.py:13-47
type InMemoryStore struct {
	// store 最新图状态：session_id → ns → state
	store map[string]map[string]*GraphState
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryStore 构造内存存储
// Python: store/inmemory.py:14-16（defaultdict）
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{store: map[string]map[string]*GraphState{}}
}

// Get 读取状态，返回深拷贝；未命中返回 (nil, nil)（对齐 Python Optional=None）
// Python: store/inmemory.py:18-19
func (s *InMemoryStore) Get(sessionID, ns string) (*GraphState, error) {
	return s.store[sessionID][ns].Clone(), nil
}

// Save 保存状态的深拷贝（对齐 Python 双向 deepcopy——存副本防外部篡改）
// Python: store/inmemory.py:22-24
func (s *InMemoryStore) Save(sessionID, ns string, state *GraphState) error {
	sub, ok := s.store[sessionID]
	if !ok {
		sub = map[string]*GraphState{}
		s.store[sessionID] = sub
	}
	sub[ns] = state.Clone()
	return nil
}

// Delete 删除状态：session 不存在静默返回；ns 空串删整个 session；
// 否则前缀匹配删除，session 删空后连带删除外层键。
// Python: store/inmemory.py:27-39
func (s *InMemoryStore) Delete(sessionID, ns string) error {
	sub, ok := s.store[sessionID]
	if !ok {
		return nil // 会话不存在，无事可删
	}
	if ns == "" {
		delete(s.store, sessionID)
		return nil
	}
	deleteNSByPrefix(sub, ns)
	// session 删空后清理外层键
	if len(sub) == 0 {
		delete(s.store, sessionID)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deleteNSByPrefix 按前缀收集并逐个删除命名空间
// Python: store/inmemory.py:42-47（_delete_ns_by_prefix）
func deleteNSByPrefix(sub map[string]*GraphState, prefix string) {
	var toDelete []string
	for ns := range sub {
		if strings.HasPrefix(ns, prefix) {
			toDelete = append(toDelete, ns)
		}
	}
	for _, ns := range toDelete {
		delete(sub, ns)
	}
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/store/ -run InMemoryStore -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/store/inmemory.go internal/agentcore/graph/store/inmemory_test.go
git commit -m "feat(store): InMemoryStore 深拷贝读写与前缀删除（8.9a inmemory.py）"
```

---

### Task 4: serde.go（Serializer/JSONSerializer/CreateSerializer）

**Files:**
- Create: `internal/agentcore/graph/store/serde.go`
- Test: `internal/agentcore/graph/store/serde_test.go`

- [ ] **Step 1: 写失败测试**

```go
package store

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestJSONSerializer_RoundTrip 类型元组往返
func TestJSONSerializer_RoundTrip(t *testing.T) {
	s := &JSONSerializer{}
	typ, data, err := s.DumpsTyped(map[string]any{"k": "值"})
	if err != nil || typ != "json" {
		t.Fatalf("dumps: %v %s", err, typ)
	}
	// ensure_ascii=False / SetEscapeHTML(false) 语义：< > & 与非 ASCII 均不转义（D-9）
	if !json.Valid(data) || bytes.Contains(data, []byte("\\u003c")) {
		t.Fatal("产物非法或含 HTML 转义")
	}
	got, err := s.LoadsTyped("json", data)
	if err != nil {
		t.Fatal(err)
	}
	if got.(map[string]any)["k"] != "值" {
		t.Fatalf("往返失真: %+v", got)
	}
}

// TestJSONSerializer_HTMLNoEscape 字节层面验证不转义 < > &
func TestJSONSerializer_HTMLNoEscape(t *testing.T) {
	s := &JSONSerializer{}
	_, data, err := s.DumpsTyped(map[string]string{"k": "a<b>&c"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"k":"a<b>&c"}` {
		t.Fatalf("应与 Python ensure_ascii=False 字节一致: %s", data)
	}
}

// TestJSONSerializer_LoadsTyped 边界：nil 数据与类型不匹配都返回 nil
func TestJSONSerializer_LoadsTyped(t *testing.T) {
	s := &JSONSerializer{}
	got, err := s.LoadsTyped("json", nil)
	if err != nil || got != nil {
		t.Fatalf("nil 数据: %v %+v", err, got)
	}
	got, err = s.LoadsTyped("pickle", []byte(`{}`))
	if err != nil || got != nil {
		t.Fatalf("类型不匹配应 (nil,nil): %v %+v", err, got)
	}
}

// TestCreateSerializer 工厂三路：json 拒绝 / pickle 放行（JSON 承担）/ 未知拒绝
func TestCreateSerializer(t *testing.T) {
	if _, err := CreateSerializer("json"); err == nil || err.Error() != "json is not yet supported" {
		t.Fatalf("json 应按 Python raise 语义拒绝: %v", err)
	}
	s, err := CreateSerializer("pickle")
	if err != nil || s == nil {
		t.Fatalf("pickle 应返回实现: %v", err)
	}
	if _, err := CreateSerializer("msgpack"); err == nil || err.Error() != "Unknown serializer type: msgpack" {
		t.Fatalf("未知类型应拒绝: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/store/ -run "Serializer|CreateSerializer" -v`
Expected: FAIL（类型未定义）

- [ ] **Step 3: 实现**

```go
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Serializer 序列化抽象：dumps/loads 携带类型名元组。
// Python: store/serde.py:11-19
type Serializer interface {
	// DumpsTyped 序列化并返回 (类型名, 字节)
	DumpsTyped(obj any) (string, []byte, error)
	// LoadsTyped 反序列化；data 为 nil 或类型不匹配时返回 (nil, nil)（对齐 Python 返回 None）
	LoadsTyped(dataType string, data []byte) (any, error)
}

// JSONSerializer JSON 实现。功能上承担 Python PickleSerializer 的默认可用角色
// （偏差 D-1：Go 无 pickle）。Python 的 JsonSerializer 类本身完整实现、照抄其行为。
// Python: store/serde.py:22-32
type JSONSerializer struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// DumpsTyped 序列化对象为 JSON 字节。
// ⚠️ 必须用 json.Encoder + SetEscapeHTML(false)：默认 json.Marshal 会把 < > &
// 转义为 \u003c/\u003e/\u0026，与 Python ensure_ascii=False 字节不一致（偏差 D-9）。
func (s *JSONSerializer) DumpsTyped(obj any) (string, []byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(obj); err != nil {
		return "json", nil, err
	}
	// Encoder.Encode 末尾带 \n，json.dumps 无换行，剥掉对齐
	data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return "json", data, nil
}

// LoadsTyped 反序列化：data 为 nil 或类型不匹配返回 (nil, nil)，对齐 Python 返回 None
func (s *JSONSerializer) LoadsTyped(dataType string, data []byte) (any, error) {
	if data == nil {
		return nil, nil
	}
	if dataType != "json" {
		return nil, nil
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateSerializer 按类型名构造序列化器。
// "json" 复刻 Python raise 语义拒绝（Python 侧类已实现但工厂刻意未开放，不自作主张解锁）；
// "pickle" 无 Go 对应物，返回 JSONSerializer 承担（偏差 D-1）。
// Python: store/serde.py:44-50
func CreateSerializer(typeName string) (Serializer, error) {
	switch typeName {
	case "json":
		return nil, fmt.Errorf("json is not yet supported")
	case "pickle":
		return &JSONSerializer{}, nil
	default:
		return nil, fmt.Errorf("Unknown serializer type: %s", typeName)
	}
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/store/ -run "Serializer|CreateSerializer" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/store/serde.go internal/agentcore/graph/store/serde_test.go
git commit -m "feat(store): Serializer 接口与 JSON 实现，CreateSerializer 复刻 raise 语义（8.9a serde.py）"
```

---

### Task 5: GraphStore 日志壳（base.go 补齐三方法）

**Files:**
- Modify: `internal/agentcore/graph/store/base.go`
- Test: `internal/agentcore/graph/store/base_test.go`（追加）

- [ ] **Step 1: 写失败测试**（追加到 `base_test.go`）

```go
// fakeStore 可控返回的存储桩
type fakeStore struct {
	getState *GraphState
	getErr   error
	saveErr  error
	delErr   error

	getCalls int
	saveCalls int
	delCalls  int
	lastDelNs string
}

func (f *fakeStore) Get(sessionID, ns string) (*GraphState, error) {
	f.getCalls++
	return f.getState, f.getErr
}
func (f *fakeStore) Save(sessionID, ns string, state *GraphState) error {
	f.saveCalls++
	return f.saveErr
}
func (f *fakeStore) Delete(sessionID, ns string) error {
	f.delCalls++
	f.lastDelNs = ns
	return f.delErr
}

// TestGraphStore_Get 未命中 debug 日志、异常透传
func TestGraphStore_Get(t *testing.T) {
	gs := NewGraphStore(&fakeStore{getState: &GraphState{Ns: "ns"}})
	st, err := gs.Get("s1", "ns")
	if err != nil || st == nil {
		t.Fatalf("命中透传失败: %v %+v", err, st)
	}

	gs = NewGraphStore(&fakeStore{}) // 未命中
	st, err = gs.Get("s1", "ns")
	if err != nil || st != nil {
		t.Fatalf("未命中应透传 (nil,nil): %v %+v", err, st)
	}

	boom := errors.New("db down")
	gs = NewGraphStore(&fakeStore{getErr: boom})
	if _, err = gs.Get("s1", "ns"); err != boom {
		t.Fatalf("异常应原样透传: %v", err)
	}
}

// TestGraphStore_Save 前后日志 + 异常透传
func TestGraphStore_Save(t *testing.T) {
	boom := errors.New("disk full")
	gs := NewGraphStore(&fakeStore{saveErr: boom})
	if err := gs.Save("s1", "ns", &GraphState{}); err != boom {
		t.Fatalf("save 异常应透传: %v", err)
	}
	ok := NewGraphStore(&fakeStore{})
	if err := ok.Save("s1", "ns", &GraphState{}); err != nil {
		t.Fatal(err)
	}
}

// TestGraphStore_Delete 空 ns 透传 all 语义 + 异常透传（Python 走 debug 级也 re-raise）
func TestGraphStore_Delete(t *testing.T) {
	fs := &fakeStore{}
	gs := NewGraphStore(fs)
	if err := gs.Delete("s1", ""); err != nil {
		t.Fatal(err)
	}
	if fs.lastDelNs != "" {
		t.Fatal("空 ns 应原样透传")
	}
	boom := errors.New("del fail")
	gs = NewGraphStore(&fakeStore{delErr: boom})
	if err := gs.Delete("s1", "ns1"); err != boom {
		t.Fatalf("delete 异常应透传: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/store/ -run GraphStore -v`
Expected: FAIL（方法未定义）

- [ ] **Step 3: 实现**（追加到 `base.go`；文件头补 import：`"uapclaw/internal/common/logger"`——以仓内实际 module 路径为准，实施时先 `grep module go.mod` 核对；logger 组件常量在包内声明）

```go
// logComponent 日志组件常量（规则 3）
const logComponent = logger.ComponentAgentCore
```

> 注意：常量按规范 2 归常量区；三方法挂 `GraphStore`，与 `NewGraphStore` 同处导出函数区。日志 Msg 英文（logs-english 纪律），结构化字段对齐 Python（event_type/session_id/graph_id）。

```go
// Get 读取图状态：未命中记 debug、异常 error 后原样透传。
// Python: store/base.py:85-103
func (g *GraphStore) Get(sessionID, ns string) (*GraphState, error) {
	state, err := g.saver.Get(sessionID, ns)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_STORE_GET").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed to get graph state")
		return nil, err
	}
	if state == nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_STORE_GET").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Msg("Not found graph state for session")
	}
	return state, nil
}

// Save 保存图状态：前后 debug（含 step）、异常 error 后原样透传。
// Python: store/base.py:105-130
func (g *GraphStore) Save(sessionID, ns string, state *GraphState) error {
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_SAVE").
		Str("session_id", sessionID).
		Int("step", state.Step).
		Str("graph_id", ns).
		Msg("Begin to save graph state of super-step")
	if err := g.saver.Save(sessionID, ns, state); err != nil {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_STORE_SAVE").
			Str("session_id", sessionID).
			Int("step", state.Step).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed to save graph state of super-step")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_SAVE").
		Str("session_id", sessionID).
		Int("step", state.Step).
		Str("graph_id", ns).
		Msg("Succeed to save graph state of super-step")
	return nil
}

// Delete 删除图状态：ns 空串打 all、前后 debug、异常走 debug 级（Python 原文即 logger.debug + re-raise，照抄）后透传。
// Python: store/base.py:132-159
func (g *GraphStore) Delete(sessionID, ns string) error {
	target := ns
	if target == "" {
		target = "all"
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_DELETE").
		Str("session_id", sessionID).
		Str("graph_id", ns).
		Msg("Begin to delete " + target + " graph states for session")
	if err := g.saver.Delete(sessionID, ns); err != nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_STORE_DELETE").
			Str("session_id", sessionID).
			Str("graph_id", ns).
			Err(err).
			Msg("Failed delete " + target + " graph states for session")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_STORE_DELETE").
		Str("session_id", sessionID).
		Str("graph_id", ns).
		Msg("Succeed to delete " + target + " graph states for session")
	return nil
}
```

> ⚠️ Python 侧 Save 异常日志的 Msg 原文是 `"Succeed to save..."`（复制粘贴 bug），Go 按规则 3 译英文时**修正为 Failed**——这是一比一复制之上的文案纠错，属日志文案而非行为，在 spec §3.6 偏差表补 D-7 登记一句即可。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/store/ -v`
Expected: PASS（含前序全部用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/store/base.go internal/agentcore/graph/store/base_test.go
git commit -m "feat(store): GraphStore 日志壳三方法（8.9a base.py:80-159）"
```

---

### Task 6: checkpointer 回填——接口收窄 + Delete 落地

**Files:**
- Modify: `internal/agentcore/session/interfaces/interfaces.go:81-83`
- Modify: `internal/agentcore/session/checkpointer/inmemory.go`（字段/构造/GraphStore/2 处 Delete 占位/release 循环）
- Modify: `internal/agentcore/session/checkpointer/persistence.go:875-880`
- Test: `internal/agentcore/session/checkpointer/inmemory_test.go:383-390`

- [ ] **Step 1: 写失败测试**（改写 `inmemory_test.go:383-390`）

```go
// TestInMemoryCheckpointer_GraphStore 测试 Graph Store 返回非 nil 壳（8.9a 回填）
func TestInMemoryCheckpointer_GraphStore(t *testing.T) {
	cp := newTestInMemoryCheckpointer(t) // 以文件内既有构造方式为准
	gs := cp.GraphStore()
	if gs == nil {
		t.Fatal("8.9a 回填后 GraphStore 应返回非 nil")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go build ./internal/agentcore/session/... 2>&1 | head`
Expected: 收窄前测试 FAIL（返回 nil）

- [ ] **Step 3: 实现回填**

`interfaces.go`：

```go
	// GraphStore 获取图状态存储
	GraphStore() *store.GraphStore
```

（import 增加 `uapclaw/internal/agentcore/graph/store`，以 `go.mod` module 名为准。）

`inmemory.go`：
1. 字段：`graphStore any` → `graphStore *store.GraphStore`，删去 `⤵️ 8.9 回填` 注释
2. 构造：`graphStore: nil` → `graphStore: store.NewGraphStore(store.NewInMemoryStore())`
3. `GraphStore()` 返回类型改 `*store.GraphStore`，删 `⤵️` 注释
4. L181 强制删除占位：

```go
				if err := cp.graphStore.Delete(sessionID, workflowID); err != nil {
					logger.Warn(logComponent).
						Str("action", "pre_workflow_execute").
						Str("event_type", "checkpoint_clear").
						Str("session_id", sessionID).
						Str("workflow_id", workflowID).
						Err(err).
						Msg("failed to delete graph store state")
				}
				// 无论 graph store 是否成功，都清除 workflow store
```

（删掉原 `func(){ /* 预留 */ }()` 整块。）

5. L568 release 循环：

```go
	for wid := range workflowIDs {
		if err := cp.graphStore.Delete(sessionID, wid); err != nil {
			logger.Warn(logComponent).
				Str("action", "release").
				Str("event_type", "checkpoint_clear").
				Str("session_id", sessionID).
				Str("workflow_id", wid).
				Err(err).
				Msg("failed to delete graph store state")
		}
	}
```

（删掉 `_ = wid // 占位`。）

`persistence.go:875-880`：签名改 `GraphStore() *store.GraphStore`，保留 `return cp.graphStore`；`persistence.go` 的 `graphStore` 字段若为 `any` 改 `*store.GraphStore` 并保持 nil + `⤵️` 注释（Persistence 侧真实注入不属本节）。

- [ ] **Step 4: 连锁改全部 fake（grep 清单收口，规则 10 同款纪律）**

对以下 13 处 `GraphStore() any`（grep 实测清单）逐个改为 `GraphStore() *store.GraphStore { return nil }`（import store 包）：

```
internal/swarm/agents/harness/team/team_manager_lifecycle_test.go:194
internal/agent_teams/runtime/team_runner_test.go:265
internal/agent_teams/runtime/team_runner_test.go:452
internal/agentcore/session/checkpointer/factory_test.go:413
internal/agentcore/session/internal/agent_session_test.go:196
internal/agentcore/session/session_test.go:85
internal/agentcore/session/agent_team_test.go:46
internal/agentcore/session/agent_test.go:48
internal/agentcore/session/interaction/interaction_test.go:50
tests/integration/agentcore/session/agent_team_session_test.go:50
```

Run: `grep -rn "GraphStore() any" internal/ tests/ cmd/ | wc -l`
Expected: `0`（规则 8 锚点为空）

- [ ] **Step 5: 验证**

Run: `go build ./... && go vet ./... && go test ./internal/agentcore/session/... ./internal/agentcore/graph/... -v 2>&1 | tail -5`
Expected: 编译零错、测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/interfaces/interfaces.go internal/agentcore/session/checkpointer/ internal/agentcore/session/internal/agent_session_test.go internal/agentcore/session/session_test.go internal/agentcore/session/agent_team_test.go internal/agentcore/session/agent_test.go internal/agentcore/session/interaction/interaction_test.go internal/swarm/agents/harness/team/team_manager_lifecycle_test.go internal/agent_teams/runtime/team_runner_test.go tests/integration/agentcore/session/agent_team_session_test.go
git commit -m "feat(checkpointer): 8.9a 回填 GraphStore 收窄 *store.GraphStore + 4 处 Delete 落地"
```

---

### Task 7: 计划表与注记变更（不丢失清单）

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（8.9 行 + 顺序注记区追加第五行）

- [ ] **Step 1: 顺序注记区追加第五行**（原四行注记逐字保留）

```markdown
> **8.x 实施顺序（2026-11-15 五次记录）**：8.9a 按方案 A 落地（store 独立包 + serde JSON 承担 pickle + Message.Copy 深拷贝 + 5.8 GraphStore 本节回填）；依赖核验：8.3b 与本节无依赖维持编号序、8.8 维持 2026-11-15 后移拍板（依据见 8.9a spec §2）。原注记保留。
```

- [ ] **Step 2: 8.9 行拆分登记**

原 8.9 行状态改 ✅，描述**原文逐字保留**，行尾追加：

```markdown
；✅ Go 事实（8.9a）：包 `internal/agentcore/graph/store/`（base/inmemory/serde/doc 四文件一比一映射）；`Message.Copy`（pregel/base.go，kind 原样保留）+ `GraphState.Clone` 承担 deepcopy（偏差 D-2）；`CreateSerializer("json")` 复刻 raise、pickle 由 JSON 承担（D-1）；⚠️ Save 异常日志 Msg 原文为 "Succeed..."（Python 复制粘贴笔误），Go 修正为 Failed（D-7）；5.8 `GraphStore() any` 已收窄 `*store.GraphStore`、4 处 Delete 已落地；⤵️ 8.9b：serde 序列化面的 Message.kind 保真（8.4 偏差 D-10 的 JSON 字节流面）仍留 8.9b；8.8 ⤵️ 维持后移拍板
```

- [ ] **Step 2b: 8.3c 行加上游解锁跟踪标注（⤴️，用户 2026-11-15 要求）**

8.3c 行描述**原文保留**，行尾追加：

```markdown
；⤴️ 8.9a serde 上游解锁跟踪：实施 8.3c 时必须核对 Python `store/serde.py` 的 `create_serializer("json")` 是否仍 raise "json is not yet supported"——若上游已解锁 json，同步回填 Go `CreateSerializer` 放行 json 并注销 8.9a 偏差 D-1（pickle 由 JSON 承担）；另注意上游拒绝 json 的根因是异常不可 json.dumps，Go 侧 error 序列化同样静默丢内容（8.9a spec D-8），届时须 error→string 适配
```

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 8.9a Graph Store 落地登记（五次顺序注记 + 8.9 行 Go 事实段）"
```

---

### Task 8: doc.go 终稿 + 全量回归收口

**Files:**
- Verify: `internal/agentcore/graph/store/doc.go`（文件树含 base/inmemory/serde/doc，职责与实际一致）

- [ ] **Step 1: 核对 doc.go 文件树与实际文件一致**（规则 1：新增文件必须入树）

Run: `ls internal/agentcore/graph/store/ && go vet ./internal/agentcore/graph/store/`
Expected: 四个 .go 文件（不含 _test.go）与 doc.go 树逐一对上

- [ ] **Step 2: 包覆盖率检查**

Run: `go test -cover ./internal/agentcore/graph/store/`
Expected: ≥ 85%

- [ ] **Step 3: 全量回归（CI 同款标签，testing-build-tags 纪律）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "FAIL|ok" | grep FAIL | head`
Expected: 空（零 FAIL）

- [ ] **Step 4: 收尾提交（若有 doc.go 修补）**

```bash
git add internal/agentcore/graph/store/doc.go
git commit -m "docs(store): doc.go 文件树终稿核对（8.9a 收口）"
```

- [ ] **Step 5: 推送**

```bash
git push
```

---

## 已知偏差速查（实施时逐条带入代码注释）

| 编号 | 内容 |
|---|---|
| D-1 | pickle → JSONSerializer 承担 |
| D-2 | deepcopy → Message.Copy + GraphState.Clone + cloneSnapshot |
| D-3 | Optional[str]=None → 空串哨兵 |
| D-4 | async → 同步签名 |
| D-5 | channel_values Dict[str, Any] → map[string][]pregel.ChannelSnapshot |
| D-6 | create_state 关键字参数 → 平铺位置参数 |
| D-7 | Save 异常日志 Msg "Succeed"（Python 笔误）→ Go "Failed" |
| D-8 | error 接口 JSON 序列化静默丢内容（Python pickle 保真 / json.dumps Exception 直接 TypeError）——本节无 serde 消费方不预做，⤴️ 8.3c 消费落地时 error→string 适配 |
| D-9 | DumpsTyped 用 json.Encoder + SetEscapeHTML(false) 对齐 ensure_ascii=False 字节 |
