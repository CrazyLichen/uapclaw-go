# 9.82 Context Evolver P1 核心框架实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.82 Context Evolver P1 核心框架，包含 RuntimeContext、ServiceContext、BaseOp/SequentialOp/ParallelOp 操作组合子、VectorNode Schema、MemoryVectorStore、JSONFileConnector、MemoryPersistenceHelper 共 7 个模块。

**Architecture:** 操作组合子模式（BaseOp + Then/With 方法链），RuntimeContext 作为操作间通信总线，ServiceContext 管理共享服务依赖注入，MemoryVectorStore 手写余弦相似度，MemoryPersistenceHelper 支持 JSON/Milvus 双后端（P1 只实现 JSON）。

**Tech Stack:** Go 标准库（sync、context、errgroup、math、encoding/json、os、fmt），无外部依赖。

---

## 文件结构

```
internal/agentcore/context_evolver/
├── doc.go                                    # 顶层包文档
├── core/
│   ├── doc.go                                # core 子包文档
│   ├── context/
│   │   ├── doc.go                            # context 子包文档
│   │   ├── runtime_context.go                # RuntimeContext 实现
│   │   ├── runtime_context_test.go           # RuntimeContext 测试
│   │   ├── service_context.go                # ServiceContext 实现
│   │   └── service_context_test.go           # ServiceContext 测试
│   ├── op/
│   │   ├── doc.go                            # op 子包文档
│   │   ├── base_op.go                        # BaseOp 接口 + Seq/Par 工厂
│   │   ├── sequential_op.go                  # SequentialOp + Then
│   │   ├── sequential_op_test.go             # SequentialOp 测试
│   │   ├── parallel_op.go                    # ParallelOp + With
│   │   └── parallel_op_test.go               # ParallelOp 测试
│   ├── schema/
│   │   ├── doc.go                            # schema 子包文档
│   │   ├── vector_node.go                    # VectorNode 实现
│   │   └── vector_node_test.go              # VectorNode 测试
│   ├── vector_store/
│   │   ├── doc.go                            # vector_store 子包文档
│   │   ├── memory_vector_store.go            # MemoryVectorStore 实现
│   │   └── memory_vector_store_test.go       # MemoryVectorStore 测试
│   ├── file_connector/
│   │   ├── doc.go                            # file_connector 子包文档
│   │   ├── json_file_connector.go            # JSONFileConnector 实现
│   │   └── json_file_connector_test.go       # JSONFileConnector 测试
│   └── persistence/
│       ├── doc.go                            # persistence 子包文档
│       ├── persistence_helper.go             # MemoryPersistenceHelper + MilvusConnector 接口
│       └── persistence_helper_test.go        # MemoryPersistenceHelper 测试
```

---

### Task 1: 顶层 doc.go + core/doc.go + 各子包 doc.go 骨架

**Files:**
- Create: `internal/agentcore/context_evolver/doc.go`
- Create: `internal/agentcore/context_evolver/core/doc.go`
- Create: `internal/agentcore/context_evolver/core/context/doc.go`
- Create: `internal/agentcore/context_evolver/core/op/doc.go`
- Create: `internal/agentcore/context_evolver/core/schema/doc.go`
- Create: `internal/agentcore/context_evolver/core/vector_store/doc.go`
- Create: `internal/agentcore/context_evolver/core/file_connector/doc.go`
- Create: `internal/agentcore/context_evolver/core/persistence/doc.go`

- [ ] **Step 1: 创建所有目录和 doc.go 文件**

```go
// internal/agentcore/context_evolver/doc.go
// Package contextevolver 提供上下文记忆演化系统（Context Evolver）。
//
// 上下文记忆演化系统让 Agent 具备跨会话的经验记忆能力——从任务执行轨迹中
// 提取蒸馏后的经验知识，以向量化记忆形式持久化，并在后续任务中检索注入。
//
// 包含 3 条独立的记忆算法管线（ACE / ReasoningBank / ReMe），
// 本包实现所有管线的共享运行时基础设施（P1）。
//
// 文件目录：
//
//	context_evolver/
//	├── doc.go                                # 包文档
//	└── core/                                 # 核心框架子包
//	    ├── context/                          # RuntimeContext + ServiceContext
//	    ├── op/                               # BaseOp + SequentialOp + ParallelOp
//	    ├── schema/                           # VectorNode
//	    ├── vector_store/                     # MemoryVectorStore
//	    ├── file_connector/                   # JSONFileConnector
//	    └── persistence/                      # MemoryPersistenceHelper
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/
package contextevolver
```

```go
// internal/agentcore/context_evolver/core/doc.go
// Package core 提供上下文记忆演化系统的核心运行时基础设施。
//
// 包含操作执行上下文（RuntimeContext/ServiceContext）、
// 可组合操作管线（BaseOp/SequentialOp/ParallelOp）、
// 向量存储格式（VectorNode）、内存向量库（MemoryVectorStore）、
// 文件持久化（JSONFileConnector）和持久化助手（MemoryPersistenceHelper）。
//
// 文件目录：
//
//	core/
//	├── doc.go                               # 包文档
//	├── context/                             # 操作执行上下文
//	├── op/                                  # 可组合操作管线
//	├── schema/                              # 向量存储格式
//	├── vector_store/                        # 内存向量库
//	├── file_connector/                      # 文件持久化连接器
//	└── persistence/                         # 持久化助手
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/
package core
```

其余子包 doc.go 遵循同样格式（包功能概述 + 文件目录 + 对应 Python 代码），每个只占 15-20 行。

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_evolver/...`
Expected: 成功，无错误

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_evolver/
git commit -m "feat(context_evolver): 添加 P1 核心框架 doc.go 骨架"
```

---

### Task 2: RuntimeContext

**Files:**
- Create: `internal/agentcore/context_evolver/core/context/runtime_context.go`
- Create: `internal/agentcore/context_evolver/core/context/runtime_context_test.go`

- [ ] **Step 1: 编写 RuntimeContext 测试**

```go
// internal/agentcore/context_evolver/core/context/runtime_context_test.go
package context

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRuntimeContext(t *testing.T) {
	rc := NewRuntimeContext()
	assert.NotNil(t, rc)
	assert.Equal(t, 0, len(rc.ToDict()))
}

func TestRuntimeContext_Set_Get(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("user_id", "alice")
	rc.Set("count", 42)

	assert.Equal(t, "alice", rc.Get("user_id"))
	assert.Equal(t, 42, rc.Get("count"))
	assert.Nil(t, rc.Get("nonexistent"))
}

func TestRuntimeContext_GetDefault(t *testing.T) {
	rc := NewRuntimeContext()
	assert.Equal(t, "fallback", rc.GetDefault("missing", "fallback"))
	rc.Set("key", "value")
	assert.Equal(t, "value", rc.GetDefault("key", "fallback"))
}

func TestRuntimeContext_GetTyped(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("name", "bob")
	rc.Set("score", 0.95)

	name, ok := GetTyped[string](rc, "name")
	assert.True(t, ok)
	assert.Equal(t, "bob", name)

	score, ok := GetTyped[float64](rc, "score")
	assert.True(t, ok)
	assert.InDelta(t, 0.95, score, 0.001)

	_, ok = GetTyped[int](rc, "name")
	assert.False(t, ok) // 类型不匹配

	_, ok = GetTyped[string](rc, "missing")
	assert.False(t, ok) // 键不存在
}

func TestRuntimeContext_ToDict_快照隔离(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("k", "v1")
	snapshot := rc.ToDict()
	rc.Set("k", "v2")
	assert.Equal(t, "v1", snapshot["k"]) // 快照不受后续修改影响
}

func TestRuntimeContext_并发安全(t *testing.T) {
	rc := NewRuntimeContext()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rc.Set("key", i)
			_ = rc.Get("key")
		}(i)
	}
	wg.Wait()
	// 不 panic 即通过
}

func TestRuntimeContext_覆盖(t *testing.T) {
	rc := NewRuntimeContext()
	rc.Set("key", "v1")
	rc.Set("key", "v2")
	assert.Equal(t, "v2", rc.Get("key"))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/context/ -v -run TestNew`
Expected: 编译失败，NewRuntimeContext 未定义

- [ ] **Step 3: 实现 RuntimeContext**

```go
// internal/agentcore/context_evolver/core/context/runtime_context.go
package context

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeContext 操作间传递中间结果的上下文。
//
// Python 用 __getattr__/__setattr__ 动态属性访问，Go 改为 map + 方法对。
// Python: context.user_id = "alice" → Go: context.Set("user_id", "alice")
//
// Python: openjiuwen/extensions/context_evolver/core/context/runtime_context.py
type RuntimeContext struct {
	mu   sync.RWMutex
	data map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimeContext 创建空的运行时上下文。
func NewRuntimeContext() *RuntimeContext {
	return &RuntimeContext{data: make(map[string]any)}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── 导出方法 ────────────────────────────

// Set 设置键值。
func (rc *RuntimeContext) Set(key string, value any) {
	rc.mu.Lock()
	rc.data[key] = value
	rc.mu.Unlock()
}

// Get 获取值，不存在时返回 nil。
func (rc *RuntimeContext) Get(key string) any {
	rc.mu.RLock()
	v := rc.data[key]
	rc.mu.RUnlock()
	return v
}

// GetDefault 获取值，不存在时返回默认值。
func (rc *RuntimeContext) GetDefault(key string, defaultVal any) any {
	rc.mu.RLock()
	v, ok := rc.data[key]
	rc.mu.RUnlock()
	if !ok {
		return defaultVal
	}
	return v
}

// GetTyped 泛型获取，带类型断言。
// 返回值和是否成功；类型不匹配或键不存在时 ok=false。
func GetTyped[T any](rc *RuntimeContext, key string) (T, bool) {
	rc.mu.RLock()
	v, ok := rc.data[key]
	rc.mu.RUnlock()
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	if !ok {
		var zero T
		return zero, false
	}
	return typed, true
}

// ToDict 返回数据快照副本。
func (rc *RuntimeContext) ToDict() map[string]any {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	cpy := make(map[string]any, len(rc.data))
	for k, v := range rc.data {
		cpy[k] = v
	}
	return cpy
}

// String 实现 Stringer 接口。
func (rc *RuntimeContext) String() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return fmt.Sprintf("RuntimeContext(%v)", rc.data)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/context/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/core/context/
git commit -m "feat(context_evolver): 实现 RuntimeContext"
```

---

### Task 3: ServiceContext

**Files:**
- Create: `internal/agentcore/context_evolver/core/context/service_context.go`
- Create: `internal/agentcore/context_evolver/core/context/service_context_test.go`
- Modify: `internal/agentcore/context_evolver/core/context/doc.go`（添加文件目录）

- [ ] **Step 1: 编写 ServiceContext 测试**

```go
// internal/agentcore/context_evolver/core/context/service_context_test.go
package context

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewServiceContext(t *testing.T) {
	sc := NewServiceContext()
	assert.NotNil(t, sc)
}

func TestServiceContext_RegisterService_GetService(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "mock-llm-client")
	assert.Equal(t, "mock-llm-client", sc.GetService("llm"))
	assert.Nil(t, sc.GetService("nonexistent"))
}

func TestServiceContext_LLM(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.LLM())
	sc.RegisterService("llm", "my-llm")
	assert.Equal(t, "my-llm", sc.LLM())
}

func TestServiceContext_EmbeddingModel(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.EmbeddingModel())
	sc.RegisterService("embedding_model", "my-embedding")
	assert.Equal(t, "my-embedding", sc.EmbeddingModel())
}

func TestServiceContext_VectorStore(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.VectorStore())
	// VectorStore 测试在 MemoryVectorStore 完成后补上
}

func TestServiceContext_Clear(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "my-llm")
	sc.Clear()
	assert.Nil(t, sc.GetService("llm"))
}

func TestServiceContext_覆盖注册(t *testing.T) {
	sc := NewServiceContext()
	sc.RegisterService("llm", "v1")
	sc.RegisterService("llm", "v2")
	assert.Equal(t, "v2", sc.GetService("llm"))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/context/ -v -run TestNewService`
Expected: 编译失败

- [ ] **Step 3: 实现 ServiceContext**

```go
// internal/agentcore/context_evolver/core/context/service_context.go
package context

import (
	"fmt"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ServiceContext 管理共享服务（LLM/Embedding/VectorStore）的上下文。
//
// Python 用 __new__ 单例模式，Go 改为依赖注入——由调用方构造并传入。
//
// Python: openjiuwen/extensions/context_evolver/core/context/service_context.py
type ServiceContext struct {
	services map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewServiceContext 创建空的服务上下文。
func NewServiceContext() *ServiceContext {
	return &ServiceContext{services: make(map[string]any)}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// RegisterService 注册服务。
func (sc *ServiceContext) RegisterService(name string, svc any) {
	sc.services[name] = svc
}

// GetService 获取已注册的服务，不存在时返回 nil。
func (sc *ServiceContext) GetService(name string) any {
	return sc.services[name]
}

// LLM 获取 LLM 服务。
func (sc *ServiceContext) LLM() any {
	return sc.GetService("llm")
}

// EmbeddingModel 获取 Embedding 模型服务。
func (sc *ServiceContext) EmbeddingModel() any {
	return sc.GetService("embedding_model")
}

// VectorStore 获取 VectorStore 服务。
func (sc *ServiceContext) VectorStore() *vector_store.MemoryVectorStore {
	svc := sc.GetService("vector_store")
	if svc == nil {
		return nil
	}
	vs, ok := svc.(*vector_store.MemoryVectorStore)
	if !ok {
		return nil
	}
	return vs
}

// Clear 清除所有已注册的服务。
func (sc *ServiceContext) Clear() {
	sc.services = make(map[string]any)
}

// String 实现 Stringer 接口。
func (sc *ServiceContext) String() string {
	names := make([]string, 0, len(sc.services))
	for k := range sc.services {
		names = append(names, k)
	}
	return fmt.Sprintf("ServiceContext(services=%v)", names)
}
```

**注意：** ServiceContext 引用了 `vector_store.MemoryVectorStore`，这会产生循环依赖问题（context 包引用 vector_store 包）。解决方案：`VectorStore()` 方法返回 `any` 类型，和 LLM()/EmbeddingModel() 保持一致，避免引入 vector_store 包的依赖。

修正实现：

```go
// VectorStore 获取 VectorStore 服务。
func (sc *ServiceContext) VectorStore() any {
	return sc.GetService("vector_store")
}
```

修正测试：

```go
func TestServiceContext_VectorStore(t *testing.T) {
	sc := NewServiceContext()
	assert.Nil(t, sc.VectorStore())
	sc.RegisterService("vector_store", "my-vs")
	assert.Equal(t, "my-vs", sc.VectorStore())
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/context/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 更新 doc.go 文件目录**

在 `internal/agentcore/context_evolver/core/context/doc.go` 中添加：

```
// 文件目录：
//
//	context/
//	├── doc.go                # 包文档
//	├── runtime_context.go    # RuntimeContext 操作间上下文
//	└── service_context.go    # ServiceContext 共享服务上下文
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_evolver/core/context/
git commit -m "feat(context_evolver): 实现 ServiceContext"
```

---

### Task 4: VectorNode Schema

**Files:**
- Create: `internal/agentcore/context_evolver/core/schema/vector_node.go`
- Create: `internal/agentcore/context_evolver/core/schema/vector_node_test.go`
- Modify: `internal/agentcore/context_evolver/core/schema/doc.go`

- [ ] **Step 1: 编写 VectorNode 测试**

```go
// internal/agentcore/context_evolver/core/schema/vector_node_test.go
package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewVectorNode(t *testing.T) {
	n := NewVectorNode("id1", "hello", []float64{1, 2, 3}, map[string]any{"type": "task"})
	assert.Equal(t, "id1", n.ID)
	assert.Equal(t, "hello", n.Content)
	assert.Equal(t, []float64{1, 2, 3}, n.Embedding)
	assert.Equal(t, "task", n.Metadata["type"])
}

func TestVectorNode_ToDict_FromDict_往返(t *testing.T) {
	original := NewVectorNode("id2", "content text", []float64{0.1, 0.2}, map[string]any{"key": "val"})
	dict := original.ToDict()

	recovered, err := VectorNodeFromDict(dict)
	require.NoError(t, err)
	assert.Equal(t, original.ID, recovered.ID)
	assert.Equal(t, original.Content, recovered.Content)
	assert.InDeltaSlice(t, original.Embedding, recovered.Embedding, 0.001)
	assert.Equal(t, original.Metadata["key"], recovered.Metadata["key"])
}

func TestVectorNode_JSON_序列化(t *testing.T) {
	n := NewVectorNode("id3", "text", []float64{1.0}, map[string]any{"x": 1})
	data, err := json.Marshal(n)
	require.NoError(t, err)

	var recovered VectorNode
	err = json.Unmarshal(data, &recovered)
	require.NoError(t, err)
	assert.Equal(t, n.ID, recovered.ID)
	assert.Equal(t, n.Content, recovered.Content)
}

func TestVectorNode_Embedding_omitempty(t *testing.T) {
	n := &VectorNode{ID: "id4", Content: "text", Metadata: map[string]any{}}
	data, err := json.Marshal(n)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "embedding")
}

func TestVectorNodeFromDict_缺失字段(t *testing.T) {
	_, err := VectorNodeFromDict(map[string]any{})
	require.Error(t, err)

	_, err = VectorNodeFromDict(map[string]any{"id": "x"})
	require.Error(t, err) // content 缺失
}

func TestVectorNode_String(t *testing.T) {
	n := NewVectorNode("id5", "short", nil, nil)
	s := n.String()
	assert.Contains(t, s, "id5")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/schema/ -v -run TestNew`
Expected: 编译失败

- [ ] **Step 3: 实现 VectorNode**

```go
// internal/agentcore/context_evolver/core/schema/vector_node.go
package schema

import (
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorNode 向量存储标准序列化格式。
//
// 所有记忆类型（ACE/ReasoningBank/ReMe）通过此格式统一序列化。
// Embedding 使用 float64（Go 无 float32 向量库生态约束）。
//
// Python: openjiuwen/extensions/context_evolver/core/schema/vector_node.py
type VectorNode struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Content 文本内容（用于 embedding）
	Content string `json:"content"`
	// Embedding 向量嵌入
	Embedding []float64 `json:"embedding,omitempty"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorNode 创建 VectorNode 实例。
func NewVectorNode(id, content string, embedding []float64, metadata map[string]any) *VectorNode {
	if metadata == nil {
		metadata = make(map[string]any)
	}
	return &VectorNode{
		ID:        id,
		Content:   content,
		Embedding: embedding,
		Metadata:  metadata,
	}
}

// VectorNodeFromDict 从字典创建 VectorNode。
func VectorNodeFromDict(data map[string]any) (*VectorNode, error) {
	id, _ := data["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("VectorNodeFromDict: 缺少 id 字段")
	}
	content, _ := data["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("VectorNodeFromDict: 缺少 content 字段")
	}

	var embedding []float64
	if emb, ok := data["embedding"]; ok && emb != nil {
		// JSON 反序列化 []interface{} → []float64
		switch e := emb.(type) {
		case []float64:
			embedding = e
		case []any:
			embedding = make([]float64, len(e))
			for i, v := range e {
				f, ok := v.(float64)
				if !ok {
					return nil, fmt.Errorf("VectorNodeFromDict: embedding[%d] 不是 float64", i)
				}
				embedding[i] = f
			}
		}
	}

	metadata, _ := data["metadata"].(map[string]any)
	if metadata == nil {
		metadata = make(map[string]any)
	}

	return &VectorNode{
		ID:        id,
		Content:   content,
		Embedding: embedding,
		Metadata:  metadata,
	}, nil
}

// ──────────────────────────── 导出方法 ────────────────────────────

// ToDict 转换为字典。
func (n *VectorNode) ToDict() map[string]any {
	dict := map[string]any{
		"id":      n.ID,
		"content": n.Content,
	}
	if n.Embedding != nil {
		dict["embedding"] = n.Embedding
	}
	if n.Metadata != nil {
		dict["metadata"] = n.Metadata
	}
	return dict
}

// String 实现 Stringer 接口。
func (n *VectorNode) String() string {
	preview := n.Content
	if len(preview) > 50 {
		preview = preview[:50] + "..."
	}
	return fmt.Sprintf("VectorNode(id=%s, content='%s')", n.ID, preview)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/schema/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 更新 doc.go，提交**

```bash
git add internal/agentcore/context_evolver/core/schema/
git commit -m "feat(context_evolver): 实现 VectorNode Schema"
```

---

### Task 5: BaseOp + SequentialOp + ParallelOp

**Files:**
- Create: `internal/agentcore/context_evolver/core/op/base_op.go`
- Create: `internal/agentcore/context_evolver/core/op/sequential_op.go`
- Create: `internal/agentcore/context_evolver/core/op/sequential_op_test.go`
- Create: `internal/agentcore/context_evolver/core/op/parallel_op.go`
- Create: `internal/agentcore/context_evolver/core/op/parallel_op_test.go`

这是最核心的模块，分 3 步实现。

- [ ] **Step 1: 编写 BaseOp + SequentialOp + ParallelOp 全部测试**

```go
// internal/agentcore/context_evolver/core/op/sequential_op_test.go
package op

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/context"
)

// mockOp 用于测试的 mock 操作
type mockOp struct {
	name    string
	execute func(ctx context.Context, rc *context.RuntimeContext) error
}

func (m *mockOp) Execute(ctx context.Context, rc *context.RuntimeContext) error {
	if m.execute != nil {
		return m.execute(ctx, rc)
	}
	rc.Set(m.name+"_called", true)
	return nil
}

func TestSeq_单操作包装(t *testing.T) {
	op := &mockOp{name: "a"}
	seq := Seq(op)
	require.NotNil(t, seq)
	assert.Len(t, seq.ops, 1)
}

func TestSequentialOp_Then_链式(t *testing.T) {
	rc := context.NewRuntimeContext()
	op1 := &mockOp{name: "a"}
	op2 := &mockOp{name: "b"}
	op3 := &mockOp{name: "c"}

	flow := Seq(op1).Then(op2).Then(op3)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)

	assert.True(t, rc.Get("a_called").(bool))
	assert.True(t, rc.Get("b_called").(bool))
	assert.True(t, rc.Get("c_called").(bool))
}

func TestSequentialOp_Then_扁平化嵌套(t *testing.T) {
	rc := context.NewRuntimeContext()
	inner := Seq(&mockOp{name: "a"}).Then(&mockOp{name: "b"})
	outer := Seq(&mockOp{name: "c"}).Then(inner)
	// inner 是 *SequentialOp，Then 应扁平化而非嵌套
	assert.Len(t, outer.ops, 3)
}

func TestSequentialOp_中间失败短路(t *testing.T) {
	rc := context.NewRuntimeContext()
	op1 := &mockOp{name: "a"}
	op2 := &mockOp{name: "b", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		return fmt.Errorf("op2 failed")
	}}
	op3 := &mockOp{name: "c"}

	flow := Seq(op1).Then(op2).Then(op3)
	err := flow.Execute(context.Background(), rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "op2 failed")

	assert.True(t, rc.Get("a_called").(bool))
	assert.Nil(t, rc.Get("c_called")) // op3 未执行
}

func TestSequentialOp_数据传递(t *testing.T) {
	rc := context.NewRuntimeContext()
	op1 := &mockOp{name: "a", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		rc.Set("result", 42)
		return nil
	}}
	op2 := &mockOp{name: "b", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		val := rc.Get("result")
		rc.Set("doubled", val.(int)*2)
		return nil
	}}

	flow := Seq(op1).Then(op2)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)
	assert.Equal(t, 84, rc.Get("doubled"))
}

func TestSequentialOp_Context取消(t *testing.T) {
	rc := context.NewRuntimeContext()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	op := &mockOp{name: "a"}
	flow := Seq(op)
	err := flow.Execute(ctx, rc)
	assert.Error(t, err) // context cancelled
}
```

```go
// internal/agentcore/context_evolver/core/op/parallel_op_test.go
package op

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/context"
)

func TestPar_单操作包装(t *testing.T) {
	op := &mockOp{name: "a"}
	par := Par(op)
	require.NotNil(t, par)
	assert.Len(t, par.ops, 1)
}

func TestParallelOp_With_链式(t *testing.T) {
	rc := context.NewRuntimeContext()
	op1 := &mockOp{name: "a"}
	op2 := &mockOp{name: "b"}

	flow := Par(op1).With(op2)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)

	assert.True(t, rc.Get("a_called").(bool))
	assert.True(t, rc.Get("b_called").(bool))
}

func TestParallelOp_With_扁平化嵌套(t *testing.T) {
	inner := Par(&mockOp{name: "a"}).With(&mockOp{name: "b"})
	outer := Par(&mockOp{name: "c"}).With(inner)
	assert.Len(t, outer.ops, 3)
}

func TestParallelOp_并行执行(t *testing.T) {
	rc := context.NewRuntimeContext()
	var counter atomic.Int32

	makeOp := func(name string) *mockOp {
		return &mockOp{name: name, execute: func(ctx context.Context, rc *context.RuntimeContext) error {
			counter.Add(1)
			rc.Set(name+"_called", true)
			return nil
		}}
	}

	flow := Par(makeOp("a")).With(makeOp("b")).With(makeOp("c"))
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)
	assert.Equal(t, int32(3), counter.Load())
}

func TestParallelOp_单失败取消其余(t *testing.T) {
	rc := context.NewRuntimeContext()
	op1 := &mockOp{name: "a", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		return fmt.Errorf("op1 failed")
	}}
	op2 := &mockOp{name: "b", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	flow := Par(op1).With(op2)
	err := flow.Execute(context.Background(), rc)
	require.Error(t, err)
}

func TestParallelOp_Then_混合顺序并行(t *testing.T) {
	rc := context.NewRuntimeContext()
	loadOp := &mockOp{name: "load", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		rc.Set("loaded", true)
		return nil
	}}
	reflectOp := &mockOp{name: "reflect", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		rc.Set("reflected", true)
		return nil
	}}
	parallelReflectOp := &mockOp{name: "parallel_reflect", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		rc.Set("parallel_reflected", true)
		return nil
	}}
	applyOp := &mockOp{name: "apply", execute: func(ctx context.Context, rc *context.RuntimeContext) error {
		rc.Set("applied", true)
		return nil
	}}

	// Seq(load).Then(Par(reflect).With(parallelReflect)).Then(apply)
	flow := Seq(loadOp).Then(Par(reflectOp).With(parallelReflectOp)).Then(applyOp)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)

	assert.True(t, rc.Get("loaded").(bool))
	assert.True(t, rc.Get("reflected").(bool))
	assert.True(t, rc.Get("parallel_reflected").(bool))
	assert.True(t, rc.Get("applied").(bool))
}

func TestParallelOp_Context取消(t *testing.T) {
	rc := context.NewRuntimeContext()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	op := &mockOp{name: "a"}
	flow := Par(op)
	err := flow.Execute(ctx, rc)
	assert.Error(t, err)
}
```

- [ ] **Step 2: 实现 BaseOp + SequentialOp + ParallelOp**

```go
// internal/agentcore/context_evolver/core/op/base_op.go
package op

import (
	"context"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseOp 操作接口。
//
// 操作是可组合的计算原子，通过 Then（顺序）和 With（并行）组合成流水线。
// Python 用 __rshift__ (>>) 和 __or__ (|) 运算符，Go 用方法链。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type BaseOp interface {
	// Execute 执行操作，读写 RuntimeContext
	Execute(ctx context.Context, rc *context.RuntimeContext) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Seq 单操作包装为 SequentialOp（链式起点）。
func Seq(op BaseOp) *SequentialOp {
	return &SequentialOp{ops: []BaseOp{op}}
}

// Par 单操作包装为 ParallelOp（链式起点）。
func Par(op BaseOp) *ParallelOp {
	return &ParallelOp{ops: []BaseOp{op}}
}
```

```go
// internal/agentcore/context_evolver/core/op/sequential_op.go
package op

import (
	"context"
	"fmt"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SequentialOp 顺序组合。
//
// 按 ops 顺序依次执行，任一失败立即返回 error。
// Then 支持扁平化嵌套的 SequentialOp（对齐 Python SequentialOp.__rshift__）。
//
// Python: openjiuwen/extensions/context_evolver/core/op/sequential_op.py
type SequentialOp struct {
	ops []BaseOp
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Then 追加操作，返回扩展后的 SequentialOp。
//
// 如果 other 是 *SequentialOp，则扁平化追加其所有操作（对齐 Python 的 __rshift__ 扁平化）。
func (s *SequentialOp) Then(other BaseOp) *SequentialOp {
	if nested, ok := other.(*SequentialOp); ok {
		s.ops = append(s.ops, nested.ops...)
	} else {
		s.ops = append(s.ops, other)
	}
	return s
}

// Execute 顺序执行所有操作。
// 任一操作失败立即返回 error，后续操作不执行。
// 如果 context 被取消，返回 context 错误。
func (s *SequentialOp) Execute(ctx context.Context, rc *context.RuntimeContext) error {
	for _, op := range s.ops {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sequential op context cancelled: %w", err)
		}
		if err := op.Execute(ctx, rc); err != nil {
			return fmt.Errorf("sequential op failed: %w", err)
		}
	}
	return nil
}

// String 实现 Stringer 接口。
func (s *SequentialOp) String() string {
	return fmt.Sprintf("SequentialOp(len=%d)", len(s.ops))
}
```

```go
// internal/agentcore/context_evolver/core/op/parallel_op.go
package op

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ParallelOp 并行组合。
//
// 使用 errgroup.Group 并行执行，任一失败 cancel 其余。
// 共享 RuntimeContext（需调用方保证并发安全）。
// With 支持扁平化嵌套的 ParallelOp（对齐 Python ParallelOp.__or__）。
//
// Python: openjiuwen/extensions/context_evolver/core/op/parallel_op.py
type ParallelOp struct {
	ops []BaseOp
}

// ──────────────────────────── 导出方法 ────────────────────────────

// With 追加并行操作，返回扩展后的 ParallelOp。
//
// 如果 other 是 *ParallelOp，则扁平化追加其所有操作（对齐 Python 的 __or__ 扁平化）。
func (p *ParallelOp) With(other BaseOp) *ParallelOp {
	if nested, ok := other.(*ParallelOp); ok {
		p.ops = append(p.ops, nested.ops...)
	} else {
		p.ops = append(p.ops, other)
	}
	return p
}

// Execute 并行执行所有操作。
// 使用 errgroup.Group，任一操作失败会 cancel 其余操作。
func (p *ParallelOp) Execute(ctx context.Context, rc *context.RuntimeContext) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("parallel op context cancelled: %w", err)
	}
	g, gctx := errgroup.WithContext(ctx)
	for _, op := range p.ops {
		op := op // 捕获循环变量
		g.Go(func() error {
			return op.Execute(gctx, rc)
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("parallel op failed: %w", err)
	}
	return nil
}

// String 实现 Stringer 接口。
func (p *ParallelOp) String() string {
	return fmt.Sprintf("ParallelOp(len=%d)", len(p.ops))
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/op/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 4: 更新 doc.go，提交**

```bash
git add internal/agentcore/context_evolver/core/op/
git commit -m "feat(context_evolver): 实现 BaseOp + SequentialOp + ParallelOp 操作组合子"
```

---

### Task 6: MemoryVectorStore

**Files:**
- Create: `internal/agentcore/context_evolver/core/vector_store/memory_vector_store.go`
- Create: `internal/agentcore/context_evolver/core/vector_store/memory_vector_store_test.go`
- Modify: `internal/agentcore/context_evolver/core/vector_store/doc.go`

- [ ] **Step 1: 编写 MemoryVectorStore 测试**

```go
// internal/agentcore/context_evolver/core/vector_store/memory_vector_store_test.go
package vector_store

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/schema"
)

func TestNewMemoryVectorStore(t *testing.T) {
	s := NewMemoryVectorStore()
	assert.NotNil(t, s)
	assert.Equal(t, 0, s.Count())
}

func TestMemoryVectorStore_Upsert_Search(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	// 插入两个节点
	n1 := schema.NewVectorNode("1", "hello world", []float64{1, 0, 0}, map[string]any{"ws": "a"})
	n2 := schema.NewVectorNode("2", "goodbye world", []float64{0, 1, 0}, map[string]any{"ws": "a"})

	require.NoError(t, s.Upsert(ctx, n1))
	require.NoError(t, s.Upsert(ctx, n2))
	assert.Equal(t, 2, s.Count())

	// 搜索与 [1,0,0] 最相似的 → 应该返回 n1
	results, err := s.Search(ctx, []float64{1, 0, 0}, 1, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].ID)
}

func TestMemoryVectorStore_Search_metadataFilter(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	n1 := schema.NewVectorNode("1", "a", []float64{1, 0}, map[string]any{"ws": "alice"})
	n2 := schema.NewVectorNode("2", "b", []float64{0, 1}, map[string]any{"ws": "bob"})

	require.NoError(t, s.Upsert(ctx, n1))
	require.NoError(t, s.Upsert(ctx, n2))

	// 只搜索 ws=alice
	results, err := s.Search(ctx, []float64{0, 1}, 10, map[string]any{"ws": "alice"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "1", results[0].ID)
}

func TestMemoryVectorStore_Search_空库(t *testing.T) {
	s := NewMemoryVectorStore()
	results, err := s.Search(context.Background(), []float64{1, 0}, 5, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestMemoryVectorStore_Upsert_无embedding报错(t *testing.T) {
	s := NewMemoryVectorStore()
	n := schema.NewVectorNode("1", "text", nil, nil)
	err := s.Upsert(context.Background(), n)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no embedding")
}

func TestMemoryVectorStore_Upsert_覆盖(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	n1 := schema.NewVectorNode("1", "old", []float64{1, 0}, nil)
	require.NoError(t, s.Upsert(ctx, n1))
	n2 := schema.NewVectorNode("1", "new", []float64{0, 1}, nil)
	require.NoError(t, s.Upsert(ctx, n2))

	assert.Equal(t, 1, s.Count())
	all := s.GetAll(nil)
	assert.Equal(t, "new", all[0].Content)
}

func TestMemoryVectorStore_Delete(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	n := schema.NewVectorNode("1", "text", []float64{1}, nil)
	require.NoError(t, s.Upsert(ctx, n))

	deleted, err := s.Delete(ctx, "1")
	require.NoError(t, err)
	assert.True(t, deleted)
	assert.Equal(t, 0, s.Count())

	deleted, err = s.Delete(ctx, "nonexistent")
	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestMemoryVectorStore_Clear(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("1", "a", []float64{1}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("2", "b", []float64{2}, nil)))
	s.Clear()
	assert.Equal(t, 0, s.Count())
}

func TestMemoryVectorStore_GetAll(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("1", "a", []float64{1}, map[string]any{"ws": "x"})))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("2", "b", []float64{2}, map[string]any{"ws": "y"})))

	all := s.GetAll(nil)
	assert.Len(t, all, 2)

	filtered := s.GetAll(map[string]any{"ws": "x"})
	assert.Len(t, filtered, 1)
	assert.Equal(t, "1", filtered[0].ID)
}

func TestMemoryVectorStore_LoadNode(t *testing.T) {
	s := NewMemoryVectorStore()
	n := schema.NewVectorNode("1", "text", []float64{1, 2, 3}, nil)
	s.LoadNode("1", n)
	assert.Equal(t, 1, s.Count())
}

func TestMemoryVectorStore_LoadFromDict(t *testing.T) {
	s := NewMemoryVectorStore()
	data := map[string]map[string]any{
		"1": {"id": "1", "content": "hello", "embedding": []float64{1, 0}, "metadata": map[string]any{}},
	}
	err := s.LoadFromDict(data)
	require.NoError(t, err)
	assert.Equal(t, 1, s.Count())
}

func TestMemoryVectorStore_余弦相似度计算(t *testing.T) {
	s := NewMemoryVectorStore()
	ctx := context.Background()

	// 三个向量：[1,0] [0,1] [0.707,0.707]
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("a", "a", []float64{1, 0}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("b", "b", []float64{0, 1}, nil)))
	require.NoError(t, s.Upsert(ctx, schema.NewVectorNode("c", "c", []float64{1 / math.Sqrt2, 1 / math.Sqrt2}, nil)))

	// 查询 [1,0]，top2 应返回 a 和 c（c 余弦 ≈ 0.707）
	results, err := s.Search(ctx, []float64{1, 0}, 2, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "a", results[0].ID)
	assert.Equal(t, "c", results[1].ID)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/vector_store/ -v -run TestNew`
Expected: 编译失败

- [ ] **Step 3: 实现 MemoryVectorStore**

```go
// internal/agentcore/context_evolver/core/vector_store/memory_vector_store.go
package vector_store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryVectorStore 内存向量库，使用余弦相似度搜索。
//
// Python 用 numpy 计算余弦相似度，Go 手写（点积 / 范数乘积），纯 math 包。
//
// Python: openjiuwen/extensions/context_evolver/core/vector_store/memory_vector_store.py
type MemoryVectorStore struct {
	mu      sync.RWMutex
	vectors map[string]*schema.VectorNode
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryVectorStore 创建空的内存向量库。
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{vectors: make(map[string]*schema.VectorNode)}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Upsert 插入或更新向量节点。
// 节点必须有 embedding，否则返回 error。
func (s *MemoryVectorStore) Upsert(ctx context.Context, node *schema.VectorNode) error {
	if node.Embedding == nil {
		return fmt.Errorf("node %s has no embedding", node.ID)
	}
	s.mu.Lock()
	s.vectors[node.ID] = node
	s.mu.Unlock()
	return nil
}

// Search 向量相似度搜索（余弦相似度）。
// 返回与 embedding 最相似的 topK 个节点，可选按 metadata 过滤。
func (s *MemoryVectorStore) Search(ctx context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*schema.VectorNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.vectors) == 0 {
		return nil, nil
	}

	type scored struct {
		node      *schema.VectorNode
		similarity float64
	}

	var candidates []scored
	for _, node := range s.vectors {
		if !matchesMetadata(node.Metadata, metadataFilter) {
			continue
		}
		if node.Embedding == nil {
			continue
		}
		sim := cosineSimilarity(embedding, node.Embedding)
		candidates = append(candidates, scored{node: node, similarity: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}
	results := make([]*schema.VectorNode, topK)
	for i := 0; i < topK; i++ {
		results[i] = candidates[i].node
	}
	return results, nil
}

// Delete 删除向量节点。返回是否实际删除。
func (s *MemoryVectorStore) Delete(_ context.Context, nodeID string) (bool, error) {
	s.mu.Lock()
	_, exists := s.vectors[nodeID]
	if exists {
		delete(s.vectors, nodeID)
	}
	s.mu.Unlock()
	return exists, nil
}

// Clear 清除所有向量。
func (s *MemoryVectorStore) Clear() {
	s.mu.Lock()
	s.vectors = make(map[string]*schema.VectorNode)
	s.mu.Unlock()
}

// Count 获取向量数量。
func (s *MemoryVectorStore) Count() int {
	s.mu.RLock()
	n := len(s.vectors)
	s.mu.RUnlock()
	return n
}

// GetAll 获取所有向量，可选按 metadata 过滤。
func (s *MemoryVectorStore) GetAll(metadataFilter map[string]any) []*schema.VectorNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*schema.VectorNode
	for _, node := range s.vectors {
		if matchesMetadata(node.Metadata, metadataFilter) {
			result = append(result, node)
		}
	}
	return result
}

// LoadNode 直接加载单个向量节点（用于反序列化，跳过 embedding 检查）。
func (s *MemoryVectorStore) LoadNode(nodeID string, node *schema.VectorNode) {
	s.mu.Lock()
	s.vectors[nodeID] = node
	s.mu.Unlock()
}

// LoadFromDict 从字典加载多个向量节点。
func (s *MemoryVectorStore) LoadFromDict(data map[string]map[string]any) error {
	for nodeID, nodeData := range data {
		node, err := schema.VectorNodeFromDict(nodeData)
		if err != nil {
			return fmt.Errorf("load node %s: %w", nodeID, err)
		}
		s.LoadNode(nodeID, node)
	}
	return nil
}

// String 实现 Stringer 接口。
func (s *MemoryVectorStore) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("MemoryVectorStore(count=%d)", len(s.vectors))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cosineSimilarity 计算两个向量的余弦相似度。
func cosineSimilarity(a, b []float64) float64 {
	dot := 0.0
	normA := 0.0
	normB := 0.0
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}

// matchesMetadata 检查节点的 metadata 是否匹配过滤条件。
func matchesMetadata(metadata, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	for k, v := range filter {
		if metadata[k] != v {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/vector_store/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 更新 doc.go，提交**

```bash
git add internal/agentcore/context_evolver/core/vector_store/
git commit -m "feat(context_evolver): 实现 MemoryVectorStore"
```

---

### Task 7: JSONFileConnector

**Files:**
- Create: `internal/agentcore/context_evolver/core/file_connector/json_file_connector.go`
- Create: `internal/agentcore/context_evolver/core/file_connector/json_file_connector_test.go`
- Modify: `internal/agentcore/context_evolver/core/file_connector/doc.go`

- [ ] **Step 1: 编写 JSONFileConnector 测试**

```go
// internal/agentcore/context_evolver/core/file_connector/json_file_connector_test.go
package file_connector

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJSONFileConnector(t *testing.T) {
	c := NewJSONFileConnector()
	assert.NotNil(t, c)
	assert.Equal(t, 2, c.indent) // 默认 indent=2
}

func TestNewJSONFileConnector_自定义选项(t *testing.T) {
	c := NewJSONFileConnector(WithIndent(4), WithEnsureASCII(true))
	assert.Equal(t, 4, c.indent)
	assert.True(t, c.ensureASCII)
}

func TestJSONFileConnector_SaveToFile_LoadFromFile(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "sub", "test.json")

	data := map[string]any{"key": "value", "count": 42.0}
	require.NoError(t, c.SaveToFile(fp, data))

	loaded, err := c.LoadFromFile(fp)
	require.NoError(t, err)
	assert.Equal(t, "value", loaded["key"])
	assert.Equal(t, 42.0, loaded["count"])
}

func TestJSONFileConnector_SaveToFile_自动建目录(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "a", "b", "c", "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{"ok": true}))
	assert.FileExists(t, fp)
}

func TestJSONFileConnector_SaveToFile_中文编码(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "cn.json")

	data := map[string]any{"内容": "你好世界"}
	require.NoError(t, c.SaveToFile(fp, data))

	loaded, err := c.LoadFromFile(fp)
	require.NoError(t, err)
	assert.Equal(t, "你好世界", loaded["内容"])
}

func TestJSONFileConnector_Exists(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	assert.False(t, c.Exists(fp))
	require.NoError(t, c.SaveToFile(fp, map[string]any{}))
	assert.True(t, c.Exists(fp))
}

func TestJSONFileConnector_Delete(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{}))

	deleted, err := c.Delete(fp)
	require.NoError(t, err)
	assert.True(t, deleted)
	assert.NoFileExists(t, fp)

	deleted, err = c.Delete(fp)
	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestJSONFileConnector_LoadFromFile_不存在(t *testing.T) {
	c := NewJSONFileConnector()
	_, err := c.LoadFromFile("/nonexistent/path.json")
	require.Error(t, err)
}

func TestJSONFileConnector_覆盖写入(t *testing.T) {
	c := NewJSONFileConnector()
	dir := t.TempDir()
	fp := filepath.Join(dir, "test.json")

	require.NoError(t, c.SaveToFile(fp, map[string]any{"v": 1.0}))
	require.NoError(t, c.SaveToFile(fp, map[string]any{"v": 2.0}))

	loaded, err := c.LoadFromFile(fp)
	require.NoError(t, err)
	assert.Equal(t, 2.0, loaded["v"])
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 JSONFileConnector**

```go
// internal/agentcore/context_evolver/core/file_connector/json_file_connector.go
package file_connector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JSONFileConnector 通用 JSON 文件 I/O 连接器。
//
// 处理文件读写操作，对数据结构无关——调用方负责序列化/反序列化。
// 自动创建父目录，UTF-8 编码。
//
// Python: openjiuwen/extensions/context_evolver/core/file_connector/json_file_connector.py
type JSONFileConnector struct {
	indent      int
	ensureASCII bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJSONFileConnector 创建 JSON 文件连接器。
func NewJSONFileConnector(opts ...JSONFileConnectorOption) *JSONFileConnector {
	c := &JSONFileConnector{indent: 2}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ──────────────────────────── 导出方法 ────────────────────────────

// SaveToFile 保存字典数据到 JSON 文件。
// 自动创建父目录，覆盖写入。
func (c *JSONFileConnector) SaveToFile(filePath string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	raw, err := json.MarshalIndent(data, "", string([]byte{byte(' '), byte(' '), byte(' '), byte(' ')})[:c.indent]))
	if err != nil {
		return fmt.Errorf("JSON 编码失败: %w", err)
	}
	raw = append(raw, '\n')

	if err := os.WriteFile(filePath, raw, 0o644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

// LoadFromFile 从 JSON 文件加载字典数据。
func (c *JSONFileConnector) LoadFromFile(filePath string) (map[string]any, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("JSON 解码失败: %w", err)
	}
	return data, nil
}

// Exists 检查文件是否存在。
func (c *JSONFileConnector) Exists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// Delete 删除文件。返回是否实际删除。
func (c *JSONFileConnector) Delete(filePath string) (bool, error) {
	err := os.Remove(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("删除文件失败: %w", err)
	}
	return true, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// JSONFileConnectorOption JSON 文件连接器配置选项。
type JSONFileConnectorOption func(*JSONFileConnector)

// WithIndent 设置缩进空格数。
func WithIndent(n int) JSONFileConnectorOption {
	return func(c *JSONFileConnector) { c.indent = n }
}

// WithEnsureASCII 设置是否转义非 ASCII 字符。
func WithEnsureASCII(ensure bool) JSONFileConnectorOption {
	return func(c *JSONFileConnector) { c.ensureASCII = ensure }
}
```

**注意：** `SaveToFile` 中 `json.MarshalIndent` 的 indent 参数需要是字符串。修正：

```go
// SaveToFile 的正确实现中 indent 字符串：
indentStr := ""
for i := 0; i < c.indent; i++ {
    indentStr += " "
}
raw, err := json.MarshalIndent(data, "", indentStr)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/file_connector/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 更新 doc.go，提交**

```bash
git add internal/agentcore/context_evolver/core/file_connector/
git commit -m "feat(context_evolver): 实现 JSONFileConnector"
```

---

### Task 8: MemoryPersistenceHelper

**Files:**
- Create: `internal/agentcore/context_evolver/core/persistence/persistence_helper.go`
- Create: `internal/agentcore/context_evolver/core/persistence/persistence_helper_test.go`
- Modify: `internal/agentcore/context_evolver/core/persistence/doc.go`

- [ ] **Step 1: 编写 MemoryPersistenceHelper 测试**

```go
// internal/agentcore/context_evolver/core/persistence/persistence_helper_test.go
package persistence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMemoryPersistenceHelper(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.NotNil(t, h)
	assert.Equal(t, "json", h.persistType) // P1 默认 json
}

func TestNewMemoryPersistenceHelper_自定义选项(t *testing.T) {
	h := NewMemoryPersistenceHelper(
		WithPersistType("json"),
		WithPersistPath("./custom/{algo_name}/{user_id}.json"),
		WithMilvusHost("my-host"),
		WithMilvusPort(19530),
		WithMilvusCollection("test_coll"),
	)
	assert.Equal(t, "json", h.persistType)
	assert.Equal(t, "./custom/{algo_name}/{user_id}.json", h.persistPath)
	assert.Equal(t, "my-host", h.milvusHost)
	assert.Equal(t, 19530, h.milvusPort)
	assert.Equal(t, "test_coll", h.milvusCollection)
}

func TestMemoryPersistenceHelper_Save_Load_JSON往返(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(
		WithPersistPath(dir+"/{algo_name}/{user_id}.json"),
	)

	nodes := map[string]any{
		"node1": map[string]any{"id": "node1", "content": "hello", "metadata": map[string]any{}},
	}

	require.NoError(t, h.Save("alice", "ace", nodes))

	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	assert.Equal(t, "hello", loaded["node1"].(map[string]any)["content"])
}

func TestMemoryPersistenceHelper_Save_空数据(t *testing.T) {
	h := NewMemoryPersistenceHelper(WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"))
	err := h.Save("alice", "ace", nil)
	assert.NoError(t, err) // 空数据直接返回
}

func TestMemoryPersistenceHelper_Load_不存在(t *testing.T) {
	h := NewMemoryPersistenceHelper(WithPersistPath(t.TempDir()+"/{algo_name}/{user_id}.json"))
	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

func TestMemoryPersistenceHelper_Save_合并(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(WithPersistPath(dir+"/{algo_name}/{user_id}.json"))

	require.NoError(t, h.Save("alice", "ace", map[string]any{"n1": map[string]any{"id": "n1", "content": "a"}}))
	require.NoError(t, h.Save("alice", "ace", map[string]any{"n2": map[string]any{"id": "n2", "content": "b"}}))

	loaded, err := h.Load("alice", "ace")
	require.NoError(t, err)
	_, hasN1 := loaded["n1"]
	_, hasN2 := loaded["n2"]
	assert.True(t, hasN1)
	assert.True(t, hasN2)
}

func TestMemoryPersistenceHelper_路径模板替换(t *testing.T) {
	dir := t.TempDir()
	h := NewMemoryPersistenceHelper(WithPersistPath(dir+"/{algo_name}/{user_id}.json"))
	require.NoError(t, h.Save("bob", "rb", map[string]any{"n1": map[string]any{"id": "n1", "content": "x"}}))

	loaded, err := h.Load("bob", "rb")
	require.NoError(t, err)
	assert.NotEmpty(t, loaded)
}

func TestMemoryPersistenceHelper_SetMilvusConnector(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	mock := &mockMilvusConnector{}
	h.SetMilvusConnector(mock)
	assert.NotNil(t, h.milvusConnector)
}

func TestMemoryPersistenceHelper_ResolvedType(t *testing.T) {
	h := NewMemoryPersistenceHelper()
	assert.Equal(t, "json", h.ResolvedType())
}

// mockMilvusConnector MilvusConnector 的 mock 实现
type mockMilvusConnector struct{}

func (m *mockMilvusConnector) SaveToDB(_ string, _ map[string]any) error { return nil }
func (m *mockMilvusConnector) LoadFromDB(_ string) (map[string]any, error) { return nil, nil }
func (m *mockMilvusConnector) Exists(_ string) bool                        { return false }
func (m *mockMilvusConnector) Delete(_ string) bool                       { return false }
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 MemoryPersistenceHelper**

```go
// internal/agentcore/context_evolver/core/persistence/persistence_helper.go
package persistence

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/context_evolver/core/file_connector"
	"github.com/uapclaw/uap-claw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// MilvusConnector Milvus 后端接口（P7 实现）。
//
// P1 只定义接口，实现在 P7 和 ContextEvolutionRail 一起完成。
//
// Python: openjiuwen/extensions/context_evolver/core/db_connector/milvus_connector.py
type MilvusConnector interface {
	// SaveToDB 保存数据到 Milvus 命名空间
	SaveToDB(namespace string, data map[string]any) error
	// LoadFromDB 从 Milvus 命名空间加载数据
	LoadFromDB(namespace string) (map[string]any, error)
	// Exists 检查命名空间是否有数据
	Exists(namespace string) bool
	// Delete 删除命名空间数据
	Delete(namespace string) bool
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryPersistenceHelper 记忆持久化助手。
//
// 支持 JSON 文件和 Milvus 双后端。"auto" 模式探测 Milvus 可达性后回退 JSON。
// P1 阶段只实现 JSON 后端，Milvus 在 P7 启用。
//
// Python: openjiuwen/extensions/context_evolver/core/persistence.py
type MemoryPersistenceHelper struct {
	persistType      string
	persistPath      string
	milvusHost       string
	milvusPort       int
	milvusCollection string

	jsonConnector   *file_connector.JSONFileConnector
	milvusConnector MilvusConnector
	resolvedType    string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultPersistPath 默认持久化路径模板
	defaultPersistPath = "./memories/{algo_name}/{user_id}.json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryPersistenceHelper 创建记忆持久化助手。
// P1 默认 persistType="json"。
func NewMemoryPersistenceHelper(opts ...PersistenceOption) *MemoryPersistenceHelper {
	h := &MemoryPersistenceHelper{
		persistType:    "json",
		persistPath:    defaultPersistPath,
		milvusHost:     "localhost",
		milvusPort:     19530,
		milvusCollection: "vector_nodes",
		jsonConnector:  file_connector.NewJSONFileConnector(),
		resolvedType:   "json", // P1 固定 json
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Save 持久化节点到后端。
// P1 只实现 JSON 后端。JSON 模式下先加载已有文件再合并写入（upsert 语义）。
func (h *MemoryPersistenceHelper) Save(userID, algoName string, nodesDict map[string]any) error {
	if len(nodesDict) == 0 {
		return nil
	}
	return h.saveJSON(userID, algoName, nodesDict)
}

// Load 从后端加载节点。
func (h *MemoryPersistenceHelper) Load(userID, algoName string) (map[string]any, error) {
	return h.loadJSON(userID, algoName)
}

// SetMilvusConnector 注入 Milvus 连接器（P7 使用）。
func (h *MemoryPersistenceHelper) SetMilvusConnector(conn MilvusConnector) {
	h.milvusConnector = conn
}

// ResolvedType 获取解析后的后端类型。
func (h *MemoryPersistenceHelper) ResolvedType() string {
	return h.resolvedType
}

// String 实现 Stringer 接口。
func (h *MemoryPersistenceHelper) String() string {
	return fmt.Sprintf("MemoryPersistenceHelper(persist_type=%s, persist_path=%s)", h.persistType, h.persistPath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jsonPath 根据模板生成 JSON 文件路径。
func (h *MemoryPersistenceHelper) jsonPath(userID, algoName string) string {
	p := h.persistPath
	p = strings.ReplaceAll(p, "{user_id}", userID)
	p = strings.ReplaceAll(p, "{algo_name}", algoName)
	return p
}

// saveJSON 保存到 JSON 文件（合并已有数据）。
func (h *MemoryPersistenceHelper) saveJSON(userID, algoName string, nodesDict map[string]any) error {
	path := h.jsonPath(userID, algoName)

	existing := make(map[string]any)
	if h.jsonConnector.Exists(path) {
		loaded, err := h.jsonConnector.LoadFromFile(path)
		if err != nil {
			logger.Error(logComponent, "加载已有 JSON 失败").Str("path", path).Err(err).Msg("")
			// 不阻断，继续写入
		} else {
			existing = loaded
		}
	}

	// 合并（upsert 语义）
	for k, v := range nodesDict {
		existing[k] = v
	}

	if err := h.jsonConnector.SaveToFile(path, existing); err != nil {
		return fmt.Errorf("保存 JSON 失败: %w", err)
	}

	logger.Info(logComponent, "持久化记忆到 JSON").
		Str("path", path).
		Int("count", len(nodesDict)).
		Str("algo", algoName).
		Msg("")
	return nil
}

// loadJSON 从 JSON 文件加载。
func (h *MemoryPersistenceHelper) loadJSON(userID, algoName string) (map[string]any, error) {
	path := h.jsonPath(userID, algoName)
	if !h.jsonConnector.Exists(path) {
		return map[string]any{}, nil
	}
	data, err := h.jsonConnector.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("加载 JSON 失败: %w", err)
	}
	logger.Info(logComponent, "从 JSON 加载记忆").
		Str("path", path).
		Int("count", len(data)).
		Str("algo", algoName).
		Msg("")
	return data, nil
}

// PersistenceOption 持久化助手配置选项。
type PersistenceOption func(*MemoryPersistenceHelper)

// WithPersistType 设置持久化类型。
func WithPersistType(pt string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.persistType = pt }
}

// WithPersistPath 设置路径模板。
func WithPersistPath(p string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.persistPath = p }
}

// WithMilvusHost 设置 Milvus 主机。
func WithMilvusHost(host string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.milvusHost = host }
}

// WithMilvusPort 设置 Milvus 端口。
func WithMilvusPort(port int) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.milvusPort = port }
}

// WithMilvusCollection 设置 Milvus 集合名。
func WithMilvusCollection(name string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.milvusCollection = name }
}

// logComponent 日志组件标识
const logComponent = logger.ComponentCommon
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/core/persistence/ -v -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 更新 doc.go，提交**

```bash
git add internal/agentcore/context_evolver/core/persistence/
git commit -m "feat(context_evolver): 实现 MemoryPersistenceHelper"
```

---

### Task 9: 更新顶层 doc.go 文件目录 + 全量编译验证

**Files:**
- Modify: `internal/agentcore/context_evolver/doc.go`
- Modify: `internal/agentcore/context_evolver/core/doc.go`
- Modify: 所有子包 `doc.go`

- [ ] **Step 1: 更新所有 doc.go 的文件目录**

确保每个 doc.go 的文件目录与实际文件一致，包含所有已实现的 `.go` 和 `_test.go` 文件（`_test.go` 不列入文件目录，按项目规则）。

- [ ] **Step 2: 全量编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_evolver/...`
Expected: 成功

- [ ] **Step 3: 全量测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 4: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/context_evolver/... -cover -count=1`
Expected: 各包覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/
git commit -m "feat(context_evolver): P1 核心框架实现完成，更新 doc.go 并通过全量验证"
```

---

## 自审检查

**1. Spec 覆盖：**
- RuntimeContext ✅ (Task 2)
- ServiceContext ✅ (Task 3)
- BaseOp + SequentialOp + ParallelOp ✅ (Task 5)
- VectorNode ✅ (Task 4)
- MemoryVectorStore ✅ (Task 6)
- JSONFileConnector ✅ (Task 7)
- MemoryPersistenceHelper ✅ (Task 8)
- MilvusConnector 接口 ✅ (Task 8)
- doc.go 骨架 ✅ (Task 1)
- 测试策略 ✅ (每个 Task 都有测试)
- 不实现的内容（config/Message/Milvus实现）✅ (设计文档明确列出)

**2. Placeholder 扫描：** 无 TBD/TODO/实现后补 等占位符。

**3. 类型一致性检查：**
- `NewRuntimeContext()` → `*RuntimeContext` ✅
- `GetTyped[T]` 泛型签名在 Task 2 定义，Task 5 测试中使用 `context.NewRuntimeContext()` ✅
- `NewVectorNode` 在 Task 4 定义，Task 6 测试中使用 ✅
- `NewMemoryVectorStore` 在 Task 6 定义 ✅
- `NewJSONFileConnector` 在 Task 7 定义，Task 8 使用 ✅
- `NewMemoryPersistenceHelper` 在 Task 8 定义 ✅
- `ServiceContext.VectorStore()` 返回 `any`（非 `*MemoryVectorStore`），避免循环依赖 ✅
- `JSONFileConnectorOption` 类型在 Task 7 定义 ✅
- `PersistenceOption` 类型在 Task 8 定义 ✅

**修复项：** Task 7 中 `SaveToFile` 的 indent 字符串生成有误，已在 Step 3 中标注修正说明。实际实现应使用 `strings.Repeat(" ", c.indent)` 而非手动拼接。
