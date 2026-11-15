# KV 版 GraphStore（Persistence 侧回填）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 Persistence 侧 KV 版 GraphStore（persistence.go，对齐 Python persistence.py:554-720）+ Message/PendingNode 定制 JSON 序列化（D-10 kind 重建 / D-8 error 适配）+ 接口收窄 `store.Store` 与 nil 守卫删除（D-11）。

**Architecture:** GraphState → 定制 MarshalJSON → JSON 字节 → `kv.BaseKVStore`（GORM+SQLite 两列 key/blob 表）→ Get → 定制 UnmarshalJSON → struct。serde 接口扩 `LoadsTyped(dataType, data, target)`（D-12）以显式还原目标类型。

**Tech Stack:** encoding/json（定制 Marshal/Unmarshal）、kv.BaseKVStore/Pipeline/DeleteByPrefix、store.Serializer（JSONSerializer）。

**Spec:** `docs/superpowers/specs/2026-11-15-kv-graph-store-persistence-design.md`

**测试纪律：** 单包裸跑；收口全量 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`；module 路径 `github.com/uapclaw/uapclaw-go`。

---

### Task 1: serde 接口扩 LoadsTyped target（D-12 前置修正）

**Files:**
- Modify: `internal/agentcore/graph/store/serde.go`
- Test: `internal/agentcore/graph/store/serde_test.go`

- [ ] **Step 1: 改失败测试**（`TestJSONSerializer_RoundTrip` 的 loads 段改为 target 形态，并加类型还原用例）

```go
	got, err := s.LoadsTyped("json", data, &out)  // out 为 map[string]any 预声明变量
	if err != nil {
		t.Fatal(err)
	}
	_ = got // 签名变更后无返回值，断言走 out
```

新增：

```go
// TestJSONSerializer_LoadsTypedTarget 还原到具体结构体（KV GraphStore 的真实用法）
func TestJSONSerializer_LoadsTypedTarget(t *testing.T) {
	s := &JSONSerializer{}
	orig := &GraphState{Ns: "ns", Step: 3, NodeVersion: map[string]int{"n": 1}}
	_, data, err := s.DumpsTyped(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back GraphState
	if err := s.LoadsTyped("json", data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Ns != "ns" || back.Step != 3 || back.NodeVersion["n"] != 1 {
		t.Fatalf("还原失真: %+v", back)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/store/ -run LoadsTyped -v`
Expected: FAIL（参数数不符编译错）

- [ ] **Step 3: 改实现**（serde.go：接口方法 + JSONSerializer 实现，LoadsTyped 原签名删除）

```go
	// LoadsTyped 反序列化到 target（显式还原目标，Python pickle 原对象语义的 Go 等价，偏差 D-12）；
	// data 为 nil 或类型不匹配时不动 target 并返回 nil error（对齐 Python 返回 None）
	LoadsTyped(dataType string, data []byte, target any) error
```

```go
// LoadsTyped 反序列化：data 为 nil 或类型不匹配不动 target 返回 nil，对齐 Python 返回 None
func (s *JSONSerializer) LoadsTyped(dataType string, data []byte, target any) error {
	if data == nil || dataType != "json" {
		return nil
	}
	return json.Unmarshal(data, target)
}
```

同步改 `serde.go` 内既有测试引用与 `base_test.go` 等调用点（grep `LoadsTyped(` 全仓核实，当前仅 serde 自身测试）。

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/store/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/store/serde.go internal/agentcore/graph/store/serde_test.go
git commit -m "feat(store): LoadsTyped 扩 target 参数（D-12，pickle 原对象还原语义的 Go 等价）"
```

---

### Task 2: Message 定制 JSON 序列化（D-10 兑现）

**Files:**
- Modify: `internal/agentcore/graph/pregel/base.go`（文件头 import 加 `encoding/json`、`fmt`）
- Test: `internal/agentcore/graph/pregel/base_test.go`

- [ ] **Step 1: 写失败测试**（追加）

```go
// TestMessage_JSONRoundTrip kind 保真往返（D-10）
func TestMessage_JSONRoundTrip(t *testing.T) {
	trigger := NewTriggerMessage("a", "n1")
	barrier := NewBarrierMessage("b", "barrier:x&y->z")

	tb, err := json.Marshal(trigger)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tb), `"kind":"trigger"`) {
		t.Fatalf("trigger 序列化应含 kind 槽: %s", tb)
	}
	var ct, cb Message
	if err := json.Unmarshal(tb, &ct); err != nil {
		t.Fatal(err)
	}
	if ct.Sender != "a" || ct.Target != "n1" || ct.kind != KindTrigger {
		t.Fatalf("trigger 还原失真: %+v kind=%s", ct, ct.kind)
	}

	bb, _ := json.Marshal(barrier)
	if err := json.Unmarshal(bb, &cb); err != nil {
		t.Fatal(err)
	}
	if cb.kind != KindBarrier || cb.Target != "barrier:x&y->z" {
		t.Fatalf("barrier 还原失真: %+v kind=%s", cb, cb.kind)
	}
}

// TestMessage_UnmarshalJSON_Reject 零值/未知 kind 拒绝（守住 8.6 不变式）
func TestMessage_UnmarshalJSON_Reject(t *testing.T) {
	var m Message
	if err := json.Unmarshal([]byte(`{"sender":"a","target":"n"}`), &m); err == nil {
		t.Fatal("缺 kind 应拒绝")
	}
	if err := json.Unmarshal([]byte(`{"sender":"a","target":"n","kind":"weird"}`), &m); err == nil {
		t.Fatal("未知 kind 应拒绝")
	}
}
```

（`base_test.go` 头补 import `encoding/json`、`strings`。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/pregel/ -run TestMessage_JSON -v`
Expected: FAIL（方法未定义）

- [ ] **Step 3: 实现**（base.go 导出函数区追加；import 补 `encoding/json`）

```go
// messageJSON Message 的 JSON 形态（Go 侧内部存储格式，Python pickle 无对应物，偏差 D-10'）
type messageJSON struct {
	Sender  string      `json:"sender"`
	Target  string      `json:"target"`
	Payload any         `json:"payload"`
	Kind    MessageKind `json:"kind"`
}

// MarshalJSON 序列化含 kind 槽。Python 无此 JSON 形态（pickle 全保真），偏差 D-10'
func (m Message) MarshalJSON() ([]byte, error) {
	return json.Marshal(messageJSON{Sender: m.Sender, Target: m.Target, Payload: m.Payload, Kind: m.kind})
}

// UnmarshalJSON 按 kind 精确重建；未知/缺失 kind 返回 error，
// 守住「零值 kind 被任何通道拒绝接收」的 8.6 不变式
func (m *Message) UnmarshalJSON(data []byte) error {
	var aux messageJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	switch aux.Kind {
	case KindTrigger, KindBarrier:
		m.kind = aux.Kind
	default:
		return fmt.Errorf("unknown message kind: %q", aux.Kind)
	}
	m.Sender, m.Target, m.Payload = aux.Sender, aux.Target, aux.Payload
	return nil
}
```

（`messageJSON` 类型按规范 2 归结构体区。）

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/pregel/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/pregel/base.go internal/agentcore/graph/pregel/base_test.go
git commit -m "feat(pregel): Message 定制 Marshal/UnmarshalJSON（D-10 kind 精确重建，未知拒绝）"
```

---

### Task 3: PendingNode 定制 JSON 序列化（D-8 适配）

**Files:**
- Modify: `internal/agentcore/graph/store/base.go`（import 补 `encoding/json`、`errors`、`fmt`）
- Test: `internal/agentcore/graph/store/base_test.go`

- [ ] **Step 1: 写失败测试**（追加）

```go
// TestPendingNode_JSONRoundTrip error↔string 双向适配（D-8）
func TestPendingNode_JSONRoundTrip(t *testing.T) {
	orig := PendingNode{NodeName: "n1", Status: "error", Exception: []error{errors.New("boom")}}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var back PendingNode
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.NodeName != "n1" || back.Status != "error" || len(back.Exception) != 1 || back.Exception[0].Error() != "boom" {
		t.Fatalf("还原失真: %+v", back)
	}
}

// TestPendingNode_JSONNilException null→nil（对齐 Python exception 默认 None）
func TestPendingNode_JSONNilException(t *testing.T) {
	var back PendingNode
	if err := json.Unmarshal([]byte(`{"NodeName":"n","Status":"interrupt","Exception":null}`), &back); err != nil {
		t.Fatal(err)
	}
	if back.Exception != nil {
		t.Fatalf("null 应还原为 nil: %+v", back.Exception)
	}
}

// TestGraphState_JSONRoundTrip 组合往返验证嵌套自动递归（Message kind + PendingNode error）
func TestGraphState_JSONRoundTrip(t *testing.T) {
	orig := &GraphState{
		Ns: "ns", Step: 2,
		ChannelValues: map[string][]pregel.ChannelSnapshot{
			"n1": {{Messages: []pregel.Message{pregel.NewBarrierMessage("b", "barrier:a&b->c")}}},
		},
		PendingBuffer: []pregel.Message{pregel.NewTriggerMessage("a", "n1")},
		PendingNode:   map[string]PendingNode{"n2": {NodeName: "n2", Status: "error", Exception: []error{errors.New("x")}}},
		NodeVersion:   map[string]int{"n1": 7},
	}
	var back GraphState
	if err := jsonRoundTrip(orig, &back); err != nil {
		t.Fatal(err)
	}
	mv := back.ChannelValues["n1"][0].Messages[0]
	if mv.kind != KindBarrier {
		t.Fatalf("嵌套 Message kind 未还原: %+v", mv)
	}
	if back.PendingBuffer[0].kind != KindTrigger {
		t.Fatalf("PendingBuffer kind 未还原: %+v", back.PendingBuffer[0])
	}
	if back.PendingNode["n2"].Exception[0].Error() != "x" {
		t.Fatalf("嵌套 Exception 未还原: %+v", back.PendingNode["n2"])
	}
	if back.NodeVersion["n1"] != 7 || back.Step != 2 {
		t.Fatalf("标量字段失真: %+v", back)
	}
}

// jsonRoundTrip 测试辅助：序列化再还原
func jsonRoundTrip(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/graph/store/ -run "PendingNode|GraphState_JSON" -v`
Expected: FAIL（Exception 往返报错或内容丢）

- [ ] **Step 3: 实现**（base.go 结构体区后追加；PendingNode 字段加 json 标签稳定键名）

PendingNode 定义改为：

```go
type PendingNode struct {
	// NodeName 节点名
	NodeName string `json:"node_name"`
	// Status 任务状态（取值对齐 8.3d 的 TaskStatusInterrupt/TaskStatusError 字面量）
	Status string `json:"status"`
	// Exception 累积的异常列表（Python 默认 None，Go nil 等价；JSON 往返经 string 适配，偏差 D-8'）
	Exception []error `json:"exception,omitempty"`
}
```

追加方法（导出函数区）：

```go
// pendingNodeJSON PendingNode 的 JSON 形态（Exception 经 string 适配，偏差 D-8'）
type pendingNodeJSON struct {
	NodeName  string   `json:"node_name"`
	Status    string   `json:"status"`
	Exception []string `json:"exception"`
}

// MarshalJSON Exception []error → []string（Go error 接口 JSON 序列化静默丢内容，D-8）
func (p PendingNode) MarshalJSON() ([]byte, error) {
	aux := pendingNodeJSON{NodeName: p.NodeName, Status: p.Status}
	for _, e := range p.Exception {
		if e != nil {
			aux.Exception = append(aux.Exception, e.Error())
		}
	}
	return json.Marshal(aux)
}

// UnmarshalJSON []string → errors.New 重建；null → nil（对齐 Python exception 默认 None）
func (p *PendingNode) UnmarshalJSON(data []byte) error {
	var aux pendingNodeJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	p.NodeName, p.Status = aux.NodeName, aux.Status
	if aux.Exception == nil {
		p.Exception = nil
		return nil
	}
	p.Exception = make([]error, len(aux.Exception))
	for i, s := range aux.Exception {
		p.Exception[i] = errors.New(s)
	}
	return nil
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/graph/store/ -v`
Expected: PASS（若既有测试用例依赖 `Exception` 字段名 JSON 形态则同步修正——grep 确认）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/graph/store/base.go internal/agentcore/graph/store/base_test.go
git commit -m "feat(store): PendingNode 定制 Marshal/UnmarshalJSON（D-8 error↔string 双向适配）"
```

---

### Task 4: persistence.go 内 KV GraphStore 三方法

**Files:**
- Modify: `internal/agentcore/session/checkpointer/persistence.go`
- Test: `internal/agentcore/session/checkpointer/persistence_test.go`（追加；沿用该文件既有测试构造惯例，如临时 sqlite/gorm 实例，见 Step 1 内代码）

- [ ] **Step 1: 写失败测试**（追加；后端用 `kv.NewInMemoryKVStore()`——BaseKVStore 接口级测试，与具体方言无关）

```go
// newKVGraphStore 构造被测 KV GraphStore
func newKVGraphStore() *GraphStore {
	return NewPersistenceGraphStore(kv.NewInMemoryKVStore())
}

func sampleGraphState() *store.GraphState {
	return &store.GraphState{
		Ns: "ns1", Step: 2,
		ChannelValues: map[string][]pregel.ChannelSnapshot{
			"n1": {{Messages: []pregel.Message{pregel.NewTriggerMessage("a", "n1")}}},
		},
		PendingBuffer: []pregel.Message{pregel.NewBarrierMessage("b", "barrier:x&y->z")},
		PendingNode:   map[string]store.PendingNode{"n2": {NodeName: "n2", Status: "error", Exception: []error{errors.New("boom")}}},
		NodeVersion:   map[string]int{"n1": 3},
	}
}

// TestKVGraphStore_SaveGet 往返保真（含 kind 与 Exception）
func TestKVGraphStore_SaveGet(t *testing.T) {
	gs := newKVGraphStore()
	if err := gs.Save("s1", "ns1", sampleGraphState()); err != nil {
		t.Fatal(err)
	}
	got, err := gs.Get("s1", "ns1")
	if err != nil || got == nil {
		t.Fatalf("get 失败: %v %+v", err, got)
	}
	if got.Step != 2 || got.NodeVersion["n1"] != 3 {
		t.Fatalf("标量失真: %+v", got)
	}
	if got.ChannelValues["n1"][0].Messages[0].Kind() != pregel.KindTrigger {
		t.Fatalf("kind 未还原: %+v", got.ChannelValues["n1"][0].Messages[0])
	}
	if got.PendingBuffer[0].Kind() != pregel.KindBarrier {
		t.Fatalf("buffer kind 未还原: %+v", got.PendingBuffer[0])
	}
	if got.PendingNode["n2"].Exception[0].Error() != "boom" {
		t.Fatalf("exception 未还原: %+v", got.PendingNode["n2"])
	}
}

// TestKVGraphStore_GetMissing 未命中 (nil, nil)
func TestKVGraphStore_GetMissing(t *testing.T) {
	gs := newKVGraphStore()
	got, err := gs.Get("s1", "nope")
	if err != nil || got != nil {
		t.Fatalf("未命中应 (nil,nil): %v %+v", err, got)
	}
}

// TestKVGraphStore_Delete 单 ns / 全 session 前缀
func TestKVGraphStore_Delete(t *testing.T) {
	gs := newKVGraphStore()
	_ = gs.Save("s1", "ns1", sampleGraphState())
	_ = gs.Save("s1", "ns2", sampleGraphState())
	_ = gs.Save("s2", "ns1", sampleGraphState())

	if err := gs.Delete("s1", "ns1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := gs.Get("s1", "ns1"); got != nil {
		t.Fatal("ns1 应已删除")
	}
	if got, _ := gs.Get("s1", "ns2"); got == nil {
		t.Fatal("ns2 不应被删")
	}

	if err := gs.Delete("s1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := gs.Get("s1", "ns2"); got != nil {
		t.Fatal("全删后 ns2 应已删除")
	}
	if got, _ := gs.Get("s2", "ns1"); got == nil {
		t.Fatal("s2 不应被连带删除")
	}
}
```

（persistence_test.go 头补 import：`errors`、`github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv`、`graph/pregel`、`graph/store`，以文件既有 import 为准增补。）

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/agentcore/session/checkpointer/ -run TestKVGraphStore -v`
Expected: FAIL（`GraphStore`/`NewPersistenceGraphStore` 未定义）

- [ ] **Step 3: 实现**（persistence.go 结构体区追加类型，导出函数区追加构造与三方法；文件头 import 补 `github.com/uapclaw/uapclaw-go/internal/agentcore/graph/store`）

```go
// ──────────────────────────── 常量（追加进既有常量区或本文件 const）──────────────

const (
	// graphStoreDataType KV 键后缀：图状态数据类型槽
	graphStoreDataType = "checkpoint_data_type"
	// graphStoreDataValue KV 键后缀：图状态数据值槽
	graphStoreDataValue = "checkpoint_data_value"
	// graphStoreKeyNums 图状态 KV 键数量
	graphStoreKeyNums = 2
)

// GraphStore KV 版图状态存储（键形 session:workflow-graph:<ns>:<suffix>，与 workflow 自身状态隔离）。
// Python: persistence.py:554-720（局部类 GraphStore(Store)）
type GraphStore struct {
	// kvStore KV 存储后端（工厂路径为 GORM+SQLite 的 DbBasedKVStore）
	kvStore kv.BaseKVStore
	// serde 序列化器（CreateSerializer("pickle") → JSONSerializer，偏差 D-1）
	serde store.Serializer
}

// NewPersistenceGraphStore 构造 KV 版图状态存储
// Python: persistence.py:571-576（__init__）
func NewPersistenceGraphStore(kvStore kv.BaseKVStore) *GraphStore {
	serde, _ := store.CreateSerializer("pickle") // "pickle" 分支恒成功（8.9a 偏差 D-1）
	return &GraphStore{kvStore: kvStore, serde: serde}
}

// Get 读取图状态：两键 pipeline 读取，未命中/反序列化失败均 (nil, nil)。
// Python: persistence.py:603-653（JSON 往返即副本，无需 Clone——对齐 pickle 语义）
func (g *GraphStore) Get(sessionID, ns string) (*store.GraphState, error) {
	pipeline := g.kvStore.Pipeline(ctxTODO()) // 见下方说明：ctx 由方法签名携带
```

> ⚠️ 签名修正：`store.Store` 接口（8.9a 定）是 `Get(sessionID, ns string)` 不带 ctx，而 kv 层全部要 ctx。**本任务连带修正 `store.Store` 接口为带 ctx**（`Get(ctx context.Context, sessionID, ns string) (*store.GraphState, error)` 等，D-13 登记：Python async 方法天然带运行时 loop 上下文，Go 显式 ctx 传参；8.9a 的 InMemoryStore/GraphStore 壳三方法签名同步加 ctx，调用方连锁）。

```go
func (g *GraphStore) Get(ctx context.Context, sessionID, ns string) (*store.GraphState, error) {
	pipeline := g.kvStore.Pipeline(ctx)
	keyType := BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns, graphStoreDataType)
	_ = pipeline.Get(ctx, keyType)
	keyValue := BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns, graphStoreDataValue)
	_ = pipeline.Get(ctx, keyValue)
	results, err := pipeline.Execute(ctx)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "CHECKPOINT_ERROR").
			Str("session_id", sessionID).
			Str("namespace", ns).
			Err(err).
			Msg("failed to read graph state keys")
		return nil, err
	}
	if len(results) != graphStoreKeyNums {
		logger.Error(logComponent).
			Str("event_type", "CHECKPOINT_ERROR").
			Str("session_id", sessionID).
			Int("expected_keys", graphStoreKeyNums).
			Int("actual_keys", len(results)).
			Str("namespace", ns).
			Msg("unexpected key count during graph state retrieval")
		return nil, nil
	}
	dataType, blob := results[0].Value, results[1].Value
	if len(dataType) == 0 || len(blob) == 0 {
		logger.Debug(logComponent).
			Str("event_type", "CHECKPOINT_RESTORE").
			Str("session_id", sessionID).
			Str("namespace", ns).
			Msg("graph state not found in KV store")
		return nil, nil
	}
	var state store.GraphState
	if err := g.serde.LoadsTyped(string(dataType), blob, &state); err != nil {
		logger.Error(logComponent).
			Str("event_type", "CHECKPOINT_ERROR").
			Str("session_id", sessionID).
			Str("namespace", ns).
			Err(err).
			Msg("failed to deserialize graph state")
		return nil, nil
	}
	return &state, nil
}

// Save 保存图状态：序列化失败 warn 后静默返回（Python 不 raise）；pipeline 写入失败 error 后返回 error（Python re-raise）。
// Python: persistence.py:656-700
func (g *GraphStore) Save(ctx context.Context, sessionID, ns string, state *store.GraphState) error {
	dataType, blob, err := g.serde.DumpsTyped(state)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "CHECKPOINT_ERROR").
			Str("session_id", sessionID).
			Str("namespace", ns).
			Str("operation", "serialize").
			Err(err).
			Msg("failed to serialize graph state")
		return nil
	}
	pipeline := g.kvStore.Pipeline(ctx)
	_ = pipeline.Set(ctx, BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns, graphStoreDataType), []byte(dataType), 0)
	_ = pipeline.Set(ctx, BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns, graphStoreDataValue), blob, 0)
	if _, err := pipeline.Execute(ctx); err != nil {
		logger.Error(logComponent).
			Str("event_type", "CHECKPOINT_ERROR").
			Str("session_id", sessionID).
			Str("namespace", ns).
			Str("storage_type", "graph").
			Err(err).
			Msg("failed to save graph state")
		return err
	}
	logger.Debug(logComponent).
		Str("event_type", "CHECKPOINT_SAVE").
		Str("session_id", sessionID).
		Str("namespace", ns).
		Str("storage_type", "graph").
		Msg("graph state saved successfully")
	return nil
}

// Delete 删除图状态：ns 空串删该 session 全部图状态（前缀），否则删单 ns 前缀。
// Python: persistence.py:702-737（batch_size=None 默认 → Go batchSize=0 一次性删除）
func (g *GraphStore) Delete(ctx context.Context, sessionID, ns string) error {
	var prefix string
	if ns == "" {
		prefix = BuildKey(sessionID, WorkflowNamespaceGraph)
	} else {
		prefix = BuildKeyWithNamespace(sessionID, WorkflowNamespaceGraph, ns)
	}
	if err := g.kvStore.DeleteByPrefix(ctx, prefix, 0); err != nil {
		return err
	}
	target := ns
	if target == "" {
		target = "all namespaces"
	}
	logger.Debug(logComponent).
		Str("event_type", "CHECKPOINT_CLEAR").
		Str("session_id", sessionID).
		Str("namespace", ns).
		Str("storage_type", "graph").
		Msg("graph checkpoint cleared for " + target)
	return nil
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./internal/agentcore/session/checkpointer/ -run TestKVGraphStore -v`
Expected: PASS（本任务内含 store.Store 加 ctx 的连锁编译修复）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/checkpointer/persistence.go internal/agentcore/session/checkpointer/persistence_test.go internal/agentcore/graph/store/base.go internal/agentcore/graph/store/base_test.go internal/agentcore/graph/store/inmemory.go internal/agentcore/graph/store/inmemory_test.go
git commit -m "feat(checkpointer): KV 版 GraphStore 三方法（persistence.py:554-720 对齐，store.Store 接口加 ctx=D-13）"
```

---

### Task 5: 接口收窄 store.Store + 注入 + 删 nil 守卫（D-11）

**Files:**
- Modify: `internal/agentcore/graph/store/base.go`（GraphStore 壳三方法签名加 ctx）
- Modify: `internal/agentcore/graph/store/inmemory.go`（同上）
- Modify: `internal/agentcore/session/interfaces/interfaces.go`（`GraphStore() store.Store`）
- Modify: `internal/agentcore/session/checkpointer/inmemory.go`（构造 `NewGraphStore(NewInMemoryStore())` 保持、Delete 调用加 ctx——以两处调用点的可用 ctx 为准，函数签名已有 ctx 传递）
- Modify: `internal/agentcore/session/checkpointer/persistence.go`（字段 `graphStore store.Store`、构造注入 `NewPersistenceGraphStore(kvStore)`、`GraphStore()` 返回类型、两处 Delete **删除 nil 守卫** D-11）
- 连锁: 13 处 `GraphStore() *store.GraphStore` fake → `GraphStore() store.Store`

- [ ] **Step 1: 接口与壳签名加 ctx**

`store/base.go` Store 接口与 GraphStore 壳、`inmemory.go` InMemoryStore 全部三方法加首参 `ctx context.Context`（import `context`）；`base_test.go`/`inmemory_test.go` 的 fakeStore 与直接调用同步加 ctx（`context.Background()`）。

- [ ] **Step 2: 收窄 interfaces.go**

```go
	// GraphStore 获取图状态存储
	GraphStore() store.Store
```

（import 补 store 包；删 `⤵️` 注释。）

- [ ] **Step 3: persistence 注入与守卫删除**

```go
	// graphStore 图状态存储（构造期必注入，Python 同构无空判，偏差 D-11）
	graphStore store.Store
```

构造：`graphStore: NewPersistenceGraphStore(kvStore),`。`GraphStore()` 改 `store.Store` 返回。两处 Delete 改为直接调用（删除 `if cp.graphStore != nil` 包裹，日志字段 `action`/`workflow_id` 保持既有值）：

```go
				if err := cp.graphStore.Delete(ctx, sessionID, workflowID); err != nil {
					logger.Warn(logComponent).
						Str("action", "clear_workflow").
						Str("event_type", "checkpoint_clear").
						Str("session_id", sessionID).
						Str("workflow_id", workflowID).
						Err(err).
						Msg("failed to delete graph store state")
				}
```

（另一处 `action: workflow_completed` 同构。）

- [ ] **Step 4: 13 处 fake 连锁**

Run: `grep -rln "GraphStore() \*store.GraphStore" internal tests`
逐文件 `*store.GraphStore` → `store.Store`（import 补 store 包；返回值 nil 不变，接口类型 nil 合法）。收口锚点：

Run: `grep -rn "GraphStore() \*store.GraphStore\|GraphStore() any" internal tests cmd | wc -l`
Expected: `0`

- [ ] **Step 5: 验证**

Run: `go build ./... && go vet ./... && go test ./internal/agentcore/session/checkpointer/ ./internal/agentcore/graph/... -v 2>&1 | tail -3`
Expected: 编译零错、PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/graph/store/ internal/agentcore/session/interfaces/interfaces.go internal/agentcore/session/checkpointer/ internal/swarm/agents/harness/team/team_manager_lifecycle_test.go internal/agent_teams/runtime/team_runner_test.go internal/agentcore/session/session_test.go internal/agentcore/session/agent_team_test.go internal/agentcore/session/agent_test.go internal/agentcore/session/internal/agent_session_test.go internal/agentcore/session/interaction/interaction_test.go internal/agentcore/session/checkpointer/factory_test.go tests/integration/agentcore/session/agent_team_session_test.go
git commit -m "feat(checkpointer): GraphStore 收窄 store.Store 接口 + persistence 注入 + nil 守卫删除（D-11/D-13）"
```

---

### Task 6: 计划表登记（不丢失清单）

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 8.9 行 Go 事实段追加**（原文保留）

```markdown
；✅ 8.9b 兑现（2026-11-15 二轮）：persistence.go KV 版 GraphStore 落地并构造期注入（GORM+SQLite 后端），nil 守卫删除（D-11）；Message 定制 Marshal/UnmarshalJSON（kind 槽精确重建、未知拒绝）兑现序列化面 D-10；PendingNode error↔string 适配（D-8'）；serde LoadsTyped 扩 target（D-12）；store.Store 三方法加 ctx（D-13）；8.9b 悬挂就此清空，8.8 维持后移
```

- [ ] **Step 2: 顺序注记区追加第六行**（原五行保留）

```markdown
> **8.x 实施顺序（2026-11-15 六次记录）**：8.9b 内容并入 Persistence KV GraphStore 节提前兑现（kind 重建/error 适配/serde target/ctx 化），8.9b 行注销、8.8 维持后移（依据见 KV GraphStore spec §2）。原注记保留。
```

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): KV GraphStore 落地登记（8.9b 注销、D-11/D-12/D-13 归档）"
```

---

### Task 7: doc.go 核对 + 全量回归收口

- [ ] **Step 1: doc.go 核对**（pregel/store 两包 doc.go 无文件增删则不动；persistence 无 doc 变更）

Run: `go vet ./internal/agentcore/...`
Expected: 零错

- [ ] **Step 2: 包覆盖率**

Run: `go test -cover ./internal/agentcore/graph/store/ ./internal/agentcore/session/checkpointer/`
Expected: ≥85%

- [ ] **Step 3: 全量回归（CI 同款标签）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep FAIL | head`
Expected: 空

- [ ] **Step 4: 提交（若有 doc 修补）+ 推送**

```bash
git push
```

---

## 已知偏差速查（本轮新增）

| 编号 | 内容 |
|---|---|
| D-8' | error 保真收窄：仅 err.Error() 文本（pickle 全保真近似） |
| D-10' | Message/PendingNode JSON 格式为 Go 侧发明（Python pickle 无 JSON 形态），不跨语言互操作 |
| D-11 | Python 构造期必有故无空判；Go 构造期注入后删除 nil 守卫 |
| D-12 | loads_typed pickle 原对象还原 → Go LoadsTyped 扩 target 参数 |
| D-13 | Python async 隐式 loop 上下文 → Go store.Store 三方法显式 ctx 首参 |
