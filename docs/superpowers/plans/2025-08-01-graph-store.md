# Graph Store (4.26) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现知识图谱图存储，支持 Entity/Relation/Episode 三种图对象的 CRUD、混合语义搜索（dense + sparse）、BFS 图扩展和可选 reranking。

**Architecture:** graph/ 包定义接口和模型，milvus/ 子包实现 Milvus 后端。MilvusGraphStore 通过嵌入 graphWriter + graphSearcher 拆分读写/搜索职责。混合搜索使用 Milvus HybridSearch API（3通道：name_embedding + content_embedding + content_bm25），BFS 扩展通过 UUID 关系链广度遍历实现。

**Tech Stack:** Go 1.25.5, `github.com/milvus-io/milvus/client/v2`（新 SDK，支持 BM25 Function）, 项目内 embedding/reranker/vector_fields 接口

**Design Spec:** `docs/superpowers/specs/2025-08-01-graph-store-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `graph/doc.go` | 包文档 |
| `graph/graph_object.go` | BaseGraphObject / NamedGraphObject / Entity / Relation / Episode / EmbedTask |
| `graph/graph_object_test.go` | 图对象模型测试 |
| `graph/config.go` | GraphConfig / GraphStoreStorageConfig / GraphStoreIndexConfig / BM25Config |
| `graph/config_test.go` | 配置测试 |
| `graph/ranking.go` | BaseRankConfig / WeightedRankConfig / RRFRankConfig / RankerRegistry |
| `graph/ranking_test.go` | 排序策略测试 |
| `graph/utils.go` | UUID生成 / 时间戳转换 / 批处理 |
| `graph/utils_test.go` | 工具函数测试 |
| `graph/base.go` | BaseGraphStore 接口 / Options / Option / QueryExpr / 常量 / 工厂 |
| `graph/base_test.go` | 工厂和接口测试 |
| `milvus/doc.go` | Milvus 子包文档 |
| `milvus/schema.go` | 三集合 Schema 构建 + 索引参数 |
| `milvus/schema_test.go` | Schema 测试 |
| `milvus/milvus_writer.go` | graphWriter 写入逻辑 |
| `milvus/milvus_writer_test.go` | 写入测试 |
| `milvus/milvus_searcher.go` | graphSearcher 搜索逻辑 |
| `milvus/milvus_searcher_test.go` | 搜索测试 |
| `milvus/milvus.go` | MilvusGraphStore 主结构体 + 接口委托 |
| `milvus/milvus_test.go` | 主结构体测试 |
| `milvus/milvus_integration_test.go` | 集成测试 (build tag: integration) |

**SDK 迁移文件（前置任务）**：

| File | Responsibility |
|------|---------------|
| `go.mod` | 替换旧 SDK 依赖 |
| `vector/milvus.go` | 迁移到新 SDK API |
| `vector/milvus_test.go` | 更新 fake client 实现 |

---

### Task 0: Milvus SDK 迁移（前置任务）

**Files:**
- Modify: `go.mod`
- Modify: `internal/agentcore/store/vector/milvus.go`
- Modify: `internal/agentcore/store/vector/milvus_test.go`

**背景**：旧 SDK `github.com/milvus-io/milvus-sdk-go/v2@v2.4.2` 已归档且不支持 BM25 Function。统一迁移到新 SDK `github.com/milvus-io/milvus/client/v2`，Graph Store 和 Vector Store 共用。

**关键 API 映射**：

| 旧 SDK | 新 SDK |
|--------|--------|
| `client.NewClient(ctx, client.Config{...})` | `milvusclient.New(ctx, milvusclient.Config{...})` |
| `client.CreateCollectionOption` | `milvusclient.CreateCollectionOption`（Builder 模式） |
| `client.SearchResult` | `milvusclient.ResultSet` |
| `client.SearchQueryOptionFunc` | `milvusclient.SearchOption`（Builder 模式） |
| `client.ANNSearchRequest` | `milvusclient.AnnRequest` |
| `client.NewWeightedReranker(weights)` | `milvusclient.NewWeightedReranker(weights)` |
| `client.NewRRFReranker()` | `milvusclient.NewRRFReranker()` |
| `entity.NewSchema()` + `.WithField()` | `milvusclient.NewSchema()` + `.WithField()` |
| `entity.NewField()` + `.WithDataType()` | `milvusclient.NewField()` + `.WithDataType()` |
| `entity.NewColumnFloatVector(name, dim, data)` | column 包构造 |
| `entity.NewColumnVarChar(name, data)` | column 包构造 |

- [ ] **Step 1: 更新 go.mod**

```bash
cd /home/opensource/uap-claw-go
# 移除旧 SDK
go mod edit -droprequire github.com/milvus-io/milvus-sdk-go/v2
# 添加新 SDK（使用 v2.5.x 兼容 Go 1.25）
go mod edit -require github.com/milvus-io/milvus/client/v2@latest
go mod tidy
```

- [ ] **Step 2: 迁移 vector/milvus.go**

更新 import：
```go
// 旧
"github.com/milvus-io/milvus-sdk-go/v2/client"
"github.com/milvus-io/milvus-sdk-go/v2/entity"

// 新
milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
"github.com/milvus-io/milvus/client/v2/entity"
"github.com/milvus-io/milvus/client/v2/index"
```

重写 `milvusClient` 接口（新 SDK Builder 模式）：
- 每个方法接收 Option 对象而非位置参数
- 返回类型从 `entity.Column` 改为 `ResultSet`
- Search 返回 `[]ResultSet`
- 新增 HybridSearch 方法

重写 `defaultCreateClient`：
```go
func defaultCreateClient(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
    return milvusclient.New(ctx, milvusclient.Config{
        Address: uri,
        APIKey:  token,
        DBName:  dbName,
    })
}
```

更新所有 helper 函数：
- `mapFieldType` / `mapMilvusTypeToOurType`：`entity.FieldType` 映射
- `mapMetricType`：`entity.MetricType` 映射
- `buildIndexParams`：使用新 `index` 包
- `buildSearchParams`：使用新 `index` 包
- `docsToColumns` / `inferColumn`：使用新 `column` 包

- [ ] **Step 3: 迁移 vector/milvus_test.go**

- 更新 `fakeMilvusClient` 及所有变体（约10个）的接口实现
- 更新 test helper 中的 SDK 类型引用

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -v`

Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add go.mod go.sum internal/agentcore/store/vector/milvus.go internal/agentcore/store/vector/milvus_test.go
git commit -m "refactor(store/vector): migrate Milvus SDK from milvus-sdk-go/v2 to milvus/client/v2"
```

**SDK 迁移文件（前置任务）**：

| File | Responsibility |
|------|---------------|
| `go.mod` | 替换旧 SDK 依赖 |
| `vector/milvus.go` | 迁移到新 SDK API |
| `vector/milvus_test.go` | 更新 fake client 实现 |

---

### Task 1: 图对象模型 — graph_object.go

**Files:**
- Create: `internal/agentcore/store/graph/graph_object.go`
- Test: `internal/agentcore/store/graph/graph_object_test.go`

- [ ] **Step 1: 创建 graph_object.go — BaseGraphObject 和 EmbedTask**

```go
package graph

import "sort"

// ──────────────────────────── 结构体 ────────────────────────────

// EmbedTask 嵌入任务（对象 + 目标字段名 + 待嵌入文本）
type EmbedTask struct {
	// Object 指向图对象的指针
	Object any
	// FieldName 目标字段名（"content_embedding" 或 "name_embedding"）
	FieldName string
	// Text 待嵌入文本
	Text string
}

// BaseGraphObject 图对象基础结构
type BaseGraphObject struct {
	// UUID 唯一标识（32位十六进制）
	UUID string `json:"uuid"`
	// CreatedAt 创建时间戳（UTC秒级）
	CreatedAt int64 `json:"created_at"`
	// UserID 用户标识
	UserID string `json:"user_id"`
	// ObjType 对象类型
	ObjType string `json:"obj_type"`
	// Language 语言标识（cn/en）
	Language string `json:"language"`
	// Metadata 附加属性
	Metadata map[string]any `json:"metadata,omitempty"`
	// Content 文本内容
	Content string `json:"content"`
	// ContentEmbedding 内容向量
	ContentEmbedding []float64 `json:"content_embedding,omitempty"`
	// ContentBM25 内容BM25稀疏向量
	ContentBM25 map[uint32]float32 `json:"content_bm25,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseGraphObject 创建带默认值的 BaseGraphObject
func NewBaseGraphObject() *BaseGraphObject {
	return &BaseGraphObject{
		UUID:      GetUUID(),
		CreatedAt: GetCurrentUTCTimestamp(),
		UserID:    "default_user",
		Language:  "cn",
		Metadata:  make(map[string]any),
	}
}

// EmbedTasks 返回该对象需要执行的嵌入任务列表
func (b *BaseGraphObject) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: b, FieldName: "content_embedding", Text: b.Content},
	}
}

// ToMap 将图对象序列化为 Milvus 插入所需的 map[string]any
func (b *BaseGraphObject) ToMap() map[string]any {
	m := map[string]any{
		"uuid":       b.UUID,
		"created_at": b.CreatedAt,
		"user_id":    b.UserID,
		"obj_type":   b.ObjType,
		"language":   b.Language,
		"content":    b.Content,
	}
	if b.Metadata != nil {
		m["metadata"] = b.Metadata
	}
	if b.ContentEmbedding != nil {
		m["content_embedding"] = b.ContentEmbedding
	}
	if b.ContentBM25 != nil {
		m["content_bm25"] = b.ContentBM25
	}
	return m
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// uniqueSortedStrings 去重并排序字符串切片
func uniqueSortedStrings(ss []string) []string {
	if len(ss) == 0 {
		return ss
	}
	seen := make(map[string]struct{}, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	sort.Strings(result)
	return result
}
```

- [ ] **Step 2: 添加 NamedGraphObject**

在 `graph_object.go` 结构体区块的 BaseGraphObject 之后添加：

```go
// NamedGraphObject 具名图对象（嵌入 BaseGraphObject + Name 字段）
type NamedGraphObject struct {
	BaseGraphObject
	// Name 名称
	Name string `json:"name"`
	// NameEmbedding 名称向量
	NameEmbedding []float64 `json:"name_embedding,omitempty"`
}

// NewNamedGraphObject 创建带默认值的 NamedGraphObject
func NewNamedGraphObject() *NamedGraphObject {
	return &NamedGraphObject{
		BaseGraphObject: *NewBaseGraphObject(),
	}
}

// EmbedTasks 返回 content_embedding + name_embedding 两个任务
func (n *NamedGraphObject) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: n, FieldName: "content_embedding", Text: n.Content},
		{Object: n, FieldName: "name_embedding", Text: n.Name},
	}
}

// ToMap 序列化 NamedGraphObject
func (n *NamedGraphObject) ToMap() map[string]any {
	m := n.BaseGraphObject.ToMap()
	m["name"] = n.Name
	if n.NameEmbedding != nil {
		m["name_embedding"] = n.NameEmbedding
	}
	return m
}
```

- [ ] **Step 3: 添加 Entity**

```go
// Entity 实体（知识图谱节点）
type Entity struct {
	NamedGraphObject
	// Relations 关联的关系UUID列表
	Relations []string `json:"relations"`
	// Episodes 关联的片段UUID列表
	Episodes []string `json:"episodes"`
	// Attributes 实体属性
	Attributes map[string]any `json:"attributes,omitempty"`
}

// NewEntity 创建带默认值的 Entity
func NewEntity() *Entity {
	return &Entity{
		NamedGraphObject: *NewNamedGraphObject(),
		ObjType:          "Entity",
	}
}

// EmbedTasks Entity 覆写：返回 content + name 两个嵌入任务
func (e *Entity) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: e, FieldName: "content_embedding", Text: e.Content},
		{Object: e, FieldName: "name_embedding", Text: e.Name},
	}
}

// ToMap 序列化 Entity（Relations/Episodes 去重排序后写入）
func (e *Entity) ToMap() map[string]any {
	m := e.NamedGraphObject.ToMap()
	m["relations"] = uniqueSortedStrings(e.Relations)
	m["episodes"] = uniqueSortedStrings(e.Episodes)
	if e.Attributes != nil {
		m["attributes"] = e.Attributes
	}
	return m
}
```

- [ ] **Step 4: 添加 Relation 和 Episode**

```go
// Relation 关系（知识图谱边）
type Relation struct {
	NamedGraphObject
	// ValidSince 有效起始时间戳
	ValidSince int64 `json:"valid_since"`
	// ValidUntil 有效终止时间戳
	ValidUntil int64 `json:"valid_until"`
	// OffsetSince 起始时区偏移（15分钟为单位）
	OffsetSince int8 `json:"offset_since"`
	// OffsetUntil 终止时区偏移（15分钟为单位）
	OffsetUntil int8 `json:"offset_until"`
	// LHS 左侧实体UUID
	LHS string `json:"lhs"`
	// RHS 右侧实体UUID
	RHS string `json:"rhs"`
}

// NewRelation 创建带默认值的 Relation
func NewRelation() *Relation {
	return &Relation{
		NamedGraphObject: *NewNamedGraphObject(),
		ObjType:          "Relation",
		ValidSince:       -1,
		ValidUntil:       -1,
	}
}

// EmbedTasks Relation 覆写：返回 content + name 两个嵌入任务
func (r *Relation) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: r, FieldName: "content_embedding", Text: r.Content},
		{Object: r, FieldName: "name_embedding", Text: r.Name},
	}
}

// ToMap 序列化 Relation
func (r *Relation) ToMap() map[string]any {
	m := r.NamedGraphObject.ToMap()
	m["lhs"] = r.LHS
	m["rhs"] = r.RHS
	m["valid_since"] = r.ValidSince
	m["valid_until"] = r.ValidUntil
	m["offset_since"] = r.OffsetSince
	m["offset_until"] = r.OffsetUntil
	return m
}

// UpdateConnectedEntities 将自身 UUID 添加到 lhs/rhs 实体的 Relations 中
func (r *Relation) UpdateConnectedEntities(lhs, rhs *Entity) {
	if lhs != nil {
		for _, rel := range lhs.Relations {
			if rel == r.UUID {
				return
			}
		}
		lhs.Relations = append(lhs.Relations, r.UUID)
	}
	if rhs != nil {
		for _, rel := range rhs.Relations {
			if rel == r.UUID {
				return
			}
		}
		rhs.Relations = append(rhs.Relations, r.UUID)
	}
}

// Episode 片段（对话片段）
type Episode struct {
	BaseGraphObject
	// ValidSince 有效起始时间戳
	ValidSince int64 `json:"valid_since"`
	// Entities 关联的实体UUID列表
	Entities []string `json:"entities"`
}

// NewEpisode 创建带默认值的 Episode
func NewEpisode() *Episode {
	return &Episode{
		BaseGraphObject: *NewBaseGraphObject(),
		ObjType:         "Episode",
		ValidSince:      -1,
	}
}

// EmbedTasks Episode 仅返回 content_embedding 任务
func (p *Episode) EmbedTasks() []EmbedTask {
	return []EmbedTask{
		{Object: p, FieldName: "content_embedding", Text: p.Content},
	}
}

// ToMap 序列化 Episode（Entities 去重排序后写入）
func (p *Episode) ToMap() map[string]any {
	m := p.BaseGraphObject.ToMap()
	m["valid_since"] = p.ValidSince
	m["entities"] = uniqueSortedStrings(p.Entities)
	return m
}
```

- [ ] **Step 5: 创建 graph_object_test.go**

```go
package graph

import (
	"testing"
)

// TestNewBaseGraphObject_默认值 测试 BaseGraphObject 默认值
func TestNewBaseGraphObject_默认值(t *testing.T) {
	obj := NewBaseGraphObject()
	if len(obj.UUID) != 32 {
		t.Errorf("UUID 长度应为 32，实际为 %d", len(obj.UUID))
	}
	if obj.CreatedAt <= 0 {
		t.Error("CreatedAt 应大于 0")
	}
	if obj.UserID != "default_user" {
		t.Errorf("UserID 默认应为 default_user，实际为 %s", obj.UserID)
	}
	if obj.Language != "cn" {
		t.Errorf("Language 默认应为 cn，实际为 %s", obj.Language)
	}
}

// TestBaseGraphObject_EmbedTasks 测试 BaseGraphObject 的 EmbedTasks
func TestBaseGraphObject_EmbedTasks(t *testing.T) {
	obj := NewBaseGraphObject()
	obj.Content = "test content"
	tasks := obj.EmbedTasks()
	if len(tasks) != 1 {
		t.Fatalf("EmbedTasks 应返回 1 个任务，实际返回 %d", len(tasks))
	}
	if tasks[0].FieldName != "content_embedding" {
		t.Errorf("FieldName 应为 content_embedding，实际为 %s", tasks[0].FieldName)
	}
	if tasks[0].Text != "test content" {
		t.Errorf("Text 应为 test content，实际为 %s", tasks[0].Text)
	}
}

// TestBaseGraphObject_ToMap 测试 BaseGraphObject 的 ToMap
func TestBaseGraphObject_ToMap(t *testing.T) {
	obj := NewBaseGraphObject()
	obj.Content = "hello"
	obj.ContentEmbedding = []float64{0.1, 0.2}
	m := obj.ToMap()
	if m["uuid"] != obj.UUID {
		t.Error("ToMap uuid 字段不匹配")
	}
	if m["content"] != "hello" {
		t.Error("ToMap content 字段不匹配")
	}
	if m["content_embedding"] == nil {
		t.Error("ToMap content_embedding 不应为 nil")
	}
}

// TestEntity_EmbedTasks 测试 Entity 的 EmbedTasks
func TestEntity_EmbedTasks(t *testing.T) {
	e := NewEntity()
	e.Content = "entity content"
	e.Name = "entity name"
	tasks := e.EmbedTasks()
	if len(tasks) != 2 {
		t.Fatalf("Entity EmbedTasks 应返回 2 个任务，实际返回 %d", len(tasks))
	}
	if tasks[0].FieldName != "content_embedding" {
		t.Errorf("第1个任务 FieldName 应为 content_embedding，实际为 %s", tasks[0].FieldName)
	}
	if tasks[1].FieldName != "name_embedding" {
		t.Errorf("第2个任务 FieldName 应为 name_embedding，实际为 %s", tasks[1].FieldName)
	}
}

// TestEntity_ToMap_去重排序 测试 Entity ToMap 中 Relations/Episodes 去重排序
func TestEntity_ToMap_去重排序(t *testing.T) {
	e := NewEntity()
	e.Relations = []string{"c", "a", "b", "a"}
	e.Episodes = []string{"z", "x", "x"}
	m := e.ToMap()
	relations := m["relations"].([]string)
	if len(relations) != 3 {
		t.Fatalf("Relations 去重后应为 3 个，实际为 %d", len(relations))
	}
	if relations[0] != "a" || relations[1] != "b" || relations[2] != "c" {
		t.Errorf("Relations 排序不正确: %v", relations)
	}
	episodes := m["episodes"].([]string)
	if len(episodes) != 2 {
		t.Fatalf("Episodes 去重后应为 2 个，实际为 %d", len(episodes))
	}
}

// TestRelation_ToMap 测试 Relation 的 ToMap
func TestRelation_ToMap(t *testing.T) {
	r := NewRelation()
	r.LHS = "entity-uuid-1"
	r.RHS = "entity-uuid-2"
	r.ValidSince = 1000
	r.ValidUntil = 2000
	m := r.ToMap()
	if m["lhs"] != "entity-uuid-1" {
		t.Error("ToMap lhs 字段不匹配")
	}
	if m["rhs"] != "entity-uuid-2" {
		t.Error("ToMap rhs 字段不匹配")
	}
	if m["valid_since"] != int64(1000) {
		t.Error("ToMap valid_since 字段不匹配")
	}
}

// TestRelation_UpdateConnectedEntities 测试 UpdateConnectedEntities
func TestRelation_UpdateConnectedEntities(t *testing.T) {
	r := NewRelation()
	lhs := NewEntity()
	rhs := NewEntity()
	r.UpdateConnectedEntities(lhs, rhs)
	if len(lhs.Relations) != 1 || lhs.Relations[0] != r.UUID {
		t.Error("lhs.Relations 应包含关系的 UUID")
	}
	if len(rhs.Relations) != 1 || rhs.Relations[0] != r.UUID {
		t.Error("rhs.Relations 应包含关系的 UUID")
	}
	// 重复调用不应重复添加
	r.UpdateConnectedEntities(lhs, rhs)
	if len(lhs.Relations) != 1 {
		t.Error("重复调用不应重复添加关系 UUID")
	}
}

// TestEpisode_ToMap_去重排序 测试 Episode ToMap 中 Entities 去重排序
func TestEpisode_ToMap_去重排序(t *testing.T) {
	p := NewEpisode()
	p.Entities = []string{"b", "a", "b"}
	m := p.ToMap()
	entities := m["entities"].([]string)
	if len(entities) != 2 {
		t.Fatalf("Entities 去重后应为 2 个，实际为 %d", len(entities))
	}
	if entities[0] != "a" || entities[1] != "b" {
		t.Errorf("Entities 排序不正确: %v", entities)
	}
}

// TestUniqueSortedStrings 测试 uniqueSortedStrings
func TestUniqueSortedStrings(t *testing.T) {
	result := uniqueSortedStrings([]string{"c", "a", "b", "a", "c"})
	if len(result) != 3 {
		t.Fatalf("去重后应为 3 个，实际为 %d", len(result))
	}
	if result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("排序不正确: %v", result)
	}
	// 空切片
	empty := uniqueSortedStrings(nil)
	if len(empty) != 0 {
		t.Errorf("空切片应返回空，实际为 %v", empty)
	}
}
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/ -run "TestNewBaseGraphObject|TestBaseGraphObject_EmbedTasks|TestBaseGraphObject_ToMap|TestEntity_EmbedTasks|TestEntity_ToMap|TestRelation_ToMap|TestRelation_UpdateConnectedEntities|TestEpisode_ToMap|TestUniqueSortedStrings" -v`

Expected: ALL PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/store/graph/graph_object.go internal/agentcore/store/graph/graph_object_test.go
git commit -m "feat(store/graph): add graph object models — BaseGraphObject, Entity, Relation, Episode"
```

---

### Task 2: 工具函数 — utils.go

**Files:**
- Create: `internal/agentcore/store/graph/utils.go`
- Test: `internal/agentcore/store/graph/utils_test.go`

- [ ] **Step 1: 创建 utils.go**

```go
package graph

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetUUID 生成32位十六进制UUID（无连字符）
func GetUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GetCurrentUTCTimestamp 获取当前UTC时间戳（秒级整数）
func GetCurrentUTCTimestamp() int64 {
	return time.Now().UTC().Unix()
}

// Batched 将切片分批，每批n个元素
func Batched[T any](items []T, n int) [][]T {
	if n <= 0 {
		return nil
	}
	var batches [][]T
	for i := 0; i < len(items); i += n {
		end := i + n
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}

// FormatTimestamp 将时间戳格式化为可读字符串
func FormatTimestamp(t int64, tz *time.Location, layout string) string {
	if tz == nil {
		tz = time.UTC
	}
	return time.Unix(t, 0).In(tz).Format(layout)
}

// FormatTimestampISO 将时间戳格式化为ISO 8601字符串
func FormatTimestampISO(t int64, tz *time.Location) string {
	return FormatTimestamp(t, tz, time.RFC3339)
}

// ISO2Timestamp 将ISO 8601字符串转换为时间戳和时区偏移
func ISO2Timestamp(isoStr string) (timestamp int64, offset int8, err error) {
	t, err := time.Parse(time.RFC3339, isoStr)
	if err != nil {
		return 0, 0, fmt.Errorf("解析ISO时间字符串失败: %w", err)
	}
	_, tzOffset := t.Zone()
	return t.Unix(), storeTZOffset(tzOffset), nil
}

// LoadStoredTimeFromDB 从数据库存储的时间戳和偏移重建时间
func LoadStoredTimeFromDB(timestamp int64, offset int8) (*time.Time, error) {
	tz := loadTZOffset(offset)
	t := time.Unix(timestamp, 0).In(tz)
	return &t, nil
}

// EnsureUniqueUUIDs 去重UUID：查询集合中已存在的UUID，返回不存在的新UUID列表
func EnsureUniqueUUIDs(ctx context.Context, store BaseGraphStore, ids []string, collection string, skip bool) ([]string, error) {
	if skip || len(ids) == 0 {
		return ids, nil
	}
	existing, err := store.Query(ctx, collection, WithIDs(stringsToAny(ids)...), WithOutputFields("uuid"))
	if err != nil {
		return nil, err
	}
	existingSet := make(map[string]struct{}, len(existing))
	for _, row := range existing {
		if uuid, ok := row["uuid"].(string); ok {
			existingSet[uuid] = struct{}{}
		}
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, found := existingSet[id]; !found {
			result = append(result, id)
		}
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// storeTZOffset 解析时区秒偏移为15分钟单位偏移
func storeTZOffset(tzOffsetSeconds int) int8 {
	return int8(tzOffsetSeconds / (15 * 60))
}

// loadTZOffset 从15分钟单位偏移重建时区
func loadTZOffset(offset int8) *time.Location {
	seconds := int(offset) * 15 * 60
	return time.FixedZone("", seconds)
}

// stringsToAny 将 []string 转为 []any
func stringsToAny(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}
```

注意：`EnsureUniqueUUIDs` 依赖 `BaseGraphStore` 接口和 `WithIDs`/`WithOutputFields` 选项，这些在 Task 5 中定义。此文件可以先创建（不包含 `EnsureUniqueUUIDs`），在 Task 5 完成后再补充。

- [ ] **Step 2: 创建 utils_test.go**

```go
package graph

import (
	"testing"
	"time"
)

// TestGetUUID_格式 测试 UUID 格式
func TestGetUUID_格式(t *testing.T) {
	uuid := GetUUID()
	if len(uuid) != 32 {
		t.Errorf("UUID 长度应为 32，实际为 %d", len(uuid))
	}
	for _, c := range uuid {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("UUID 应为十六进制，发现字符 %c", c)
			break
		}
	}
}

// TestGetUUID_唯一性 测试 UUID 唯一性
func TestGetUUID_唯一性(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		uuid := GetUUID()
		if _, ok := seen[uuid]; ok {
			t.Fatalf("生成重复 UUID: %s", uuid)
		}
		seen[uuid] = struct{}{}
	}
}

// TestGetCurrentUTCTimestamp 测试时间戳
func TestGetCurrentUTCTimestamp(t *testing.T) {
	ts := GetCurrentUTCTimestamp()
	if ts <= 0 {
		t.Error("时间戳应大于 0")
	}
	// 应为秒级（10位数字左右）
	now := time.Now().UTC().Unix()
	if ts < now-1 || ts > now+1 {
		t.Errorf("时间戳与当前时间相差过大: got %d, expect around %d", ts, now)
	}
}

// TestBatched_正常分批 测试 Batched 正常分批
func TestBatched_正常分批(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	batches := Batched(items, 2)
	if len(batches) != 3 {
		t.Fatalf("应有 3 批，实际为 %d", len(batches))
	}
	if len(batches[0]) != 2 || len(batches[1]) != 2 || len(batches[2]) != 1 {
		t.Errorf("批次大小不正确: %v", batches)
	}
}

// TestBatched_空切片 测试 Batched 空切片
func TestBatched_空切片(t *testing.T) {
	batches := Batched([]int{}, 3)
	if len(batches) != 0 {
		t.Errorf("空切片应返回空批次，实际为 %d", len(batches))
	}
}

// TestBatched_无效批次大小 测试 Batched 无效批次大小
func TestBatched_无效批次大小(t *testing.T) {
	batches := Batched([]int{1, 2, 3}, 0)
	if batches != nil {
		t.Errorf("n<=0 应返回 nil，实际为 %v", batches)
	}
}

// TestFormatTimestampISO 测试 ISO 时间格式化
func TestFormatTimestampISO(t *testing.T) {
	ts := int64(1700000000)
	result := FormatTimestampISO(ts, time.UTC)
	if len(result) == 0 {
		t.Error("ISO 格式化结果不应为空")
	}
	// 应包含 T 分隔符
	found := false
	for _, c := range result {
		if c == 'T' {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ISO 格式应包含 T，实际为 %s", result)
	}
}

// TestISO2Timestamp_正常 测试 ISO 时间戳解析
func TestISO2Timestamp_正常(t *testing.T) {
	ts, offset, err := ISO2Timestamp("2023-11-14T22:13:20+08:00")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if ts <= 0 {
		t.Error("时间戳应大于 0")
	}
	// +08:00 = 8*4=32 个15分钟单位
	if offset != 32 {
		t.Errorf("偏移量应为 32，实际为 %d", offset)
	}
}

// TestISO2Timestamp_无效格式 测试无效格式
func TestISO2Timestamp_无效格式(t *testing.T) {
	_, _, err := ISO2Timestamp("not-a-date")
	if err == nil {
		t.Error("无效格式应返回错误")
	}
}

// TestLoadStoredTimeFromDB 测试从数据库存储重建时间
func TestLoadStoredTimeFromDB(t *testing.T) {
	ts := int64(1700000000)
	offset := int8(32) // +08:00
	tm, err := LoadStoredTimeFromDB(ts, offset)
	if err != nil {
		t.Fatalf("重建时间失败: %v", err)
	}
	if tm == nil {
		t.Fatal("重建时间不应为 nil")
	}
}

// TestStoreTZOffset 测试时区偏移转换
func TestStoreTZOffset(t *testing.T) {
	// +08:00 = 28800秒 = 32 * 15 * 60
	result := storeTZOffset(28800)
	if result != 32 {
		t.Errorf("+08:00 偏移应为 32，实际为 %d", result)
	}
	// UTC = 0
	result = storeTZOffset(0)
	if result != 0 {
		t.Errorf("UTC 偏移应为 0，实际为 %d", result)
	}
}

// TestLoadTZOffset 测试从偏移重建时区
func TestLoadTZOffset(t *testing.T) {
	loc := loadTZOffset(32)
	if loc == nil {
		t.Fatal("时区不应为 nil")
	}
	_, offset := time.Now().In(loc).Zone()
	if offset != 28800 {
		t.Errorf("时区偏移应为 28800 秒，实际为 %d", offset)
	}
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/ -run "TestGetUUID|TestGetCurrentUTCTimestamp|TestBatched|TestFormatTimestampISO|TestISO2Timestamp|TestLoadStoredTimeFromDB|TestStoreTZOffset|TestLoadTZOffset" -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/utils.go internal/agentcore/store/graph/utils_test.go
git commit -m "feat(store/graph): add utility functions — UUID, timestamp, batching"
```

---

### Task 3: 配置体系 — config.go

**Files:**
- Create: `internal/agentcore/store/graph/config.go`
- Test: `internal/agentcore/store/graph/config_test.go`

- [ ] **Step 1: 创建 config.go**

```go
package graph

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GraphConfig 图存储顶层配置
type GraphConfig struct {
	// URI 数据库连接地址
	URI string `json:"uri"`
	// Name 数据库名称
	Name string `json:"name"`
	// Token 认证令牌
	Token string `json:"token"`
	// Backend 后端类型（目前仅 "milvus"）
	Backend string `json:"backend"`
	// Timeout 连接超时（秒）
	Timeout float64 `json:"timeout"`
	// Extras 额外参数
	Extras map[string]any `json:"extras,omitempty"`
	// MaxConcurrent 最大并发数
	MaxConcurrent int `json:"max_concurrent"`
	// EmbedDim 嵌入向量维度
	EmbedDim int `json:"embed_dim"`
	// EmbedBatchSize 嵌入批大小
	EmbedBatchSize int `json:"embed_batch_size"`
	// StorageConfig 存储限制配置
	StorageConfig *GraphStoreStorageConfig `json:"db_storage_config"`
	// IndexConfig 索引配置
	IndexConfig *GraphStoreIndexConfig `json:"db_embed_config"`
	// RequestMaxRetries 请求最大重试次数
	RequestMaxRetries int `json:"request_max_retries"`
}

// GraphStoreStorageConfig 图存储字段长度限制配置
type GraphStoreStorageConfig struct {
	// UUID UUID最大长度
	UUID int `json:"uuid"`
	// Name 名称最大长度
	Name int `json:"name"`
	// Content 内容最大长度
	Content int `json:"content"`
	// Language 语言字段最大长度
	Language int `json:"language"`
	// UserID 用户ID最大长度
	UserID int `json:"user_id"`
	// Entities 每片段最大实体数
	Entities int `json:"entities"`
	// Relations 每实体最大关系数
	Relations int `json:"relations"`
	// Episodes 每实体最大片段数
	Episodes int `json:"episodes"`
	// ObjType 对象类型最大长度
	ObjType int `json:"obj_type"`
}

// GraphStoreIndexConfig 图存储索引配置
type GraphStoreIndexConfig struct {
	// IndexType 向量索引类型
	IndexType vector_fields.VectorField `json:"index_type"`
	// DistanceMetric 距离度量方式（cosine/euclidean/dot）
	DistanceMetric string `json:"distance_metric"`
	// ExtraConfigs 额外索引配置
	ExtraConfigs map[string]any `json:"extra_configs,omitempty"`
	// BM25Config BM25参数配置
	BM25Config *BM25Config `json:"bm25_config"`
	// BM25AnalyzerSettings BM25分析器设置
	BM25AnalyzerSettings map[string]any `json:"bm25_analyzer_settings,omitempty"`
}

// BM25Config BM25检索参数
type BM25Config struct {
	// B 文档长度归一化参数（0~1）
	B float64 `json:"b"`
	// K1 词频饱和度参数（≥0）
	K1 float64 `json:"k1"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultGraphBackend 默认后端
	DefaultGraphBackend = "milvus"
	// DefaultGraphTimeout 默认超时（秒）
	DefaultGraphTimeout = 15.0
	// DefaultGraphMaxConcurrent 默认最大并发
	DefaultGraphMaxConcurrent = 10
	// DefaultGraphEmbedDim 默认嵌入维度
	DefaultGraphEmbedDim = 512
	// DefaultGraphEmbedBatchSize 默认嵌入批大小
	DefaultGraphEmbedBatchSize = 10
	// DefaultGraphRequestMaxRetries 默认请求最大重试次数
	DefaultGraphRequestMaxRetries = 5

	// 存储限制默认值
	DefaultStorageUUID     = 32
	DefaultStorageName     = 500
	DefaultStorageContent  = 65535
	DefaultStorageLanguage = 10
	DefaultStorageUserID   = 32
	DefaultStorageEntities = 4096
	DefaultStorageRelations = 4096
	DefaultStorageEpisodes = 4096
	DefaultStorageObjType  = 20

	// BM25 默认参数
	DefaultBM25B  = 0.75
	DefaultBM25K1 = 1.2
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGraphConfig 创建带默认值的图存储配置
func NewGraphConfig(uri string) *GraphConfig {
	return &GraphConfig{
		URI:               uri,
		Backend:           DefaultGraphBackend,
		Timeout:           DefaultGraphTimeout,
		MaxConcurrent:     DefaultGraphMaxConcurrent,
		EmbedDim:          DefaultGraphEmbedDim,
		EmbedBatchSize:    DefaultGraphEmbedBatchSize,
		RequestMaxRetries: DefaultGraphRequestMaxRetries,
		StorageConfig:     NewDefaultStorageConfig(),
		IndexConfig:       NewDefaultIndexConfig(),
	}
}

// NewDefaultStorageConfig 创建默认存储限制配置
func NewDefaultStorageConfig() *GraphStoreStorageConfig {
	return &GraphStoreStorageConfig{
		UUID:      DefaultStorageUUID,
		Name:      DefaultStorageName,
		Content:   DefaultStorageContent,
		Language:  DefaultStorageLanguage,
		UserID:    DefaultStorageUserID,
		Entities:  DefaultStorageEntities,
		Relations: DefaultStorageRelations,
		Episodes:  DefaultStorageEpisodes,
		ObjType:   DefaultStorageObjType,
	}
}

// NewDefaultIndexConfig 创建默认索引配置
func NewDefaultIndexConfig() *GraphStoreIndexConfig {
	return &GraphStoreIndexConfig{
		DistanceMetric: "cosine",
		BM25Config: &BM25Config{
			B:  DefaultBM25B,
			K1: DefaultBM25K1,
		},
	}
}

// Validate 校验 GraphConfig 字段合法性
func (c *GraphConfig) Validate() error {
	if c.URI == "" {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", "URI 不能为空"))
	}
	if c.Timeout <= 0 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("Timeout 必须大于 0，当前值: %v", c.Timeout)))
	}
	if c.EmbedDim < 32 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("EmbedDim 必须 ≥ 32，当前值: %d", c.EmbedDim)))
	}
	if c.MaxConcurrent < 0 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("MaxConcurrent 必须 ≥ 0，当前值: %d", c.MaxConcurrent)))
	}
	if c.EmbedBatchSize < 1 {
		return exception.ValidateError(exception.StatusStoreGraphParamInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("EmbedBatchSize 必须 ≥ 1，当前值: %d", c.EmbedBatchSize)))
	}
	return nil
}
```

- [ ] **Step 2: 创建 config_test.go**

```go
package graph

import "testing"

// TestNewGraphConfig_默认值 测试 GraphConfig 默认值
func TestNewGraphConfig_默认值(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	if cfg.Backend != "milvus" {
		t.Errorf("Backend 默认应为 milvus，实际为 %s", cfg.Backend)
	}
	if cfg.Timeout != 15.0 {
		t.Errorf("Timeout 默认应为 15.0，实际为 %v", cfg.Timeout)
	}
	if cfg.MaxConcurrent != 10 {
		t.Errorf("MaxConcurrent 默认应为 10，实际为 %d", cfg.MaxConcurrent)
	}
	if cfg.EmbedDim != 512 {
		t.Errorf("EmbedDim 默认应为 512，实际为 %d", cfg.EmbedDim)
	}
	if cfg.EmbedBatchSize != 10 {
		t.Errorf("EmbedBatchSize 默认应为 10，实际为 %d", cfg.EmbedBatchSize)
	}
	if cfg.StorageConfig == nil {
		t.Error("StorageConfig 不应为 nil")
	}
	if cfg.IndexConfig == nil {
		t.Error("IndexConfig 不应为 nil")
	}
}

// TestNewDefaultStorageConfig_默认值 测试 StorageConfig 默认值
func TestNewDefaultStorageConfig_默认值(t *testing.T) {
	cfg := NewDefaultStorageConfig()
	if cfg.UUID != 32 {
		t.Errorf("UUID 默认应为 32，实际为 %d", cfg.UUID)
	}
	if cfg.Content != 65535 {
		t.Errorf("Content 默认应为 65535，实际为 %d", cfg.Content)
	}
}

// TestNewDefaultIndexConfig_默认值 测试 IndexConfig 默认值
func TestNewDefaultIndexConfig_默认值(t *testing.T) {
	cfg := NewDefaultIndexConfig()
	if cfg.DistanceMetric != "cosine" {
		t.Errorf("DistanceMetric 默认应为 cosine，实际为 %s", cfg.DistanceMetric)
	}
	if cfg.BM25Config == nil {
		t.Error("BM25Config 不应为 nil")
	}
	if cfg.BM25Config.B != 0.75 {
		t.Errorf("BM25 B 默认应为 0.75，实际为 %v", cfg.BM25Config.B)
	}
}

// TestGraphConfig_Validate_正常 测试正常配置校验
func TestGraphConfig_Validate_正常(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	if err := cfg.Validate(); err != nil {
		t.Errorf("正常配置校验不应报错: %v", err)
	}
}

// TestGraphConfig_Validate_URI为空 测试 URI 为空
func TestGraphConfig_Validate_URI为空(t *testing.T) {
	cfg := NewGraphConfig("")
	if err := cfg.Validate(); err == nil {
		t.Error("URI 为空应返回错误")
	}
}

// TestGraphConfig_Validate_Timeout无效 测试 Timeout 无效
func TestGraphConfig_Validate_Timeout无效(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.Timeout = -1
	if err := cfg.Validate(); err == nil {
		t.Error("Timeout ≤ 0 应返回错误")
	}
}

// TestGraphConfig_Validate_EmbedDim过小 测试 EmbedDim 过小
func TestGraphConfig_Validate_EmbedDim过小(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.EmbedDim = 16
	if err := cfg.Validate(); err == nil {
		t.Error("EmbedDim < 32 应返回错误")
	}
}

// TestGraphConfig_Validate_EmbedBatchSize无效 测试 EmbedBatchSize 无效
func TestGraphConfig_Validate_EmbedBatchSize无效(t *testing.T) {
	cfg := NewGraphConfig("http://localhost:19530")
	cfg.EmbedBatchSize = 0
	if err := cfg.Validate(); err == nil {
		t.Error("EmbedBatchSize < 1 应返回错误")
	}
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/ -run "TestNewGraphConfig|TestNewDefaultStorageConfig|TestNewDefaultIndexConfig|TestGraphConfig_Validate" -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/config.go internal/agentcore/store/graph/config_test.go
git commit -m "feat(store/graph): add configuration types — GraphConfig, StorageConfig, IndexConfig, BM25Config"
```

---

### Task 4: 排序策略 — ranking.go

**Files:**
- Create: `internal/agentcore/store/graph/ranking.go`
- Test: `internal/agentcore/store/graph/ranking_test.go`

- [ ] **Step 1: 创建 ranking.go**

```go
package graph

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// WeightedRankConfig 加权排序配置
type WeightedRankConfig struct {
	// NameDense 名称向量权重
	NameDense float64 `json:"name_dense"`
	// ContentDense 内容向量权重
	ContentDense float64 `json:"content_dense"`
	// ContentSparse 内容BM25稀疏权重
	ContentSparse float64 `json:"content_sparse"`
}

// RRFRankConfig 倒数排名融合配置
type RRFRankConfig struct {
	// K RRF常数
	K int `json:"k"`
	// NameDense 是否包含名称向量通道
	NameDense bool `json:"name_dense"`
	// ContentDense 是否包含内容向量通道
	ContentDense bool `json:"content_dense"`
	// ContentSparse 是否包含BM25稀疏通道
	ContentSparse bool `json:"content_sparse"`
}

// RankerRegistry 排序器注册表（线程安全）
type RankerRegistry struct {
	mu       sync.RWMutex
	backends map[string]map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultWeightNameDense 默认名称向量权重
	DefaultWeightNameDense = 0.15
	// DefaultWeightContentDense 默认内容向量权重
	DefaultWeightContentDense = 0.60
	// DefaultWeightContentSparse 默认内容稀疏权重
	DefaultWeightContentSparse = 0.25
	// DefaultRRFK 默认RRF常数
	DefaultRRFK = 40
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// GlobalRankerRegistry 全局排序器注册表
	GlobalRankerRegistry = NewRankerRegistry()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BaseRankConfig 排序基础配置接口
type BaseRankConfig interface {
	// Name 排序策略名称
	Name() string
	// HigherIsBetter 分数越高是否越好
	HigherIsBetter() bool
	// IsActive 各通道开关 [name_dense, content_dense, content_sparse]
	IsActive() [3]int
	// Args 返回构建排序器所需的位置参数和关键字参数
	Args() ([]any, map[string]any)
}

// NewWeightedRankConfig 创建带默认值的加权排序配置
func NewWeightedRankConfig() *WeightedRankConfig {
	return &WeightedRankConfig{
		NameDense:     DefaultWeightNameDense,
		ContentDense:  DefaultWeightContentDense,
		ContentSparse: DefaultWeightContentSparse,
	}
}

// NewRRFRankConfig 创建带默认值的RRF排序配置
func NewRRFRankConfig() *RRFRankConfig {
	return &RRFRankConfig{
		K:              DefaultRRFK,
		NameDense:      true,
		ContentDense:   true,
		ContentSparse:  true,
	}
}

// WeightedRankConfig 实现 BaseRankConfig 接口

func (w *WeightedRankConfig) Name() string        { return "weighted" }
func (w *WeightedRankConfig) HigherIsBetter() bool { return false }
func (w *WeightedRankConfig) IsActive() [3]int {
	return [3]int{
		boolToInt(w.NameDense > 0),
		boolToInt(w.ContentDense > 0),
		boolToInt(w.ContentSparse > 0),
	}
}
func (w *WeightedRankConfig) Args() ([]any, map[string]any) {
	// 归一化权重：过滤零值后除以总和
	var weights []float64
	var total float64
	for _, v := range []float64{w.NameDense, w.ContentDense, w.ContentSparse} {
		if v > 0 {
			weights = append(weights, v)
			total += v
		}
	}
	if total > 0 {
		for i := range weights {
			weights[i] /= total
		}
	}
	positional := make([]any, len(weights))
	for i, v := range weights {
		positional[i] = v
	}
	return positional, map[string]any{}
}

// RRFRankConfig 实现 BaseRankConfig 接口

func (r *RRFRankConfig) Name() string        { return "rrf" }
func (r *RRFRankConfig) HigherIsBetter() bool { return true }
func (r *RRFRankConfig) IsActive() [3]int {
	return [3]int{
		boolToInt(r.NameDense),
		boolToInt(r.ContentDense),
		boolToInt(r.ContentSparse),
	}
}
func (r *RRFRankConfig) Args() ([]any, map[string]any) {
	return []any{r.K}, map[string]any{}
}

// NewRankerRegistry 创建排序器注册表
func NewRankerRegistry() *RankerRegistry {
	return &RankerRegistry{
		backends: make(map[string]map[string]any),
	}
}

// Register 注册某后端的排序器构造函数
func (r *RankerRegistry) Register(backend string, rankers map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[backend] = rankers
}

// GetRanker 获取指定后端和策略的排序器
func (r *RankerRegistry) GetRanker(backend, strategy string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rankers, ok := r.backends[backend]
	if !ok {
		return nil, false
	}
	v, ok := rankers[strategy]
	return v, ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
```

- [ ] **Step 2: 创建 ranking_test.go**

```go
package graph

import "testing"

// TestWeightedRankConfig_Name 测试加权排序名称
func TestWeightedRankConfig_Name(t *testing.T) {
	w := NewWeightedRankConfig()
	if w.Name() != "weighted" {
		t.Errorf("Name 应为 weighted，实际为 %s", w.Name())
	}
}

// TestWeightedRankConfig_HigherIsBetter 测试加权排序方向
func TestWeightedRankConfig_HigherIsBetter(t *testing.T) {
	w := NewWeightedRankConfig()
	if w.HigherIsBetter() {
		t.Error("WeightedRankConfig HigherIsBetter 应为 false")
	}
}

// TestWeightedRankConfig_IsActive_默认 测试默认通道活跃状态
func TestWeightedRankConfig_IsActive_默认(t *testing.T) {
	w := NewWeightedRankConfig()
	active := w.IsActive()
	if active != [3]int{1, 1, 1} {
		t.Errorf("默认3通道应全部活跃，实际为 %v", active)
	}
}

// TestWeightedRankConfig_IsActive_部分通道 测试部分通道关闭
func TestWeightedRankConfig_IsActive_部分通道(t *testing.T) {
	w := &WeightedRankConfig{NameDense: 0, ContentDense: 0.6, ContentSparse: 0.4}
	active := w.IsActive()
	if active != [3]int{0, 1, 1} {
		t.Errorf("应只有2通道活跃，实际为 %v", active)
	}
}

// TestWeightedRankConfig_Args_归一化 测试权重归一化
func TestWeightedRankConfig_Args_归一化(t *testing.T) {
	w := NewWeightedRankConfig() // 0.15, 0.60, 0.25
	pos, _ := w.Args()
	if len(pos) != 3 {
		t.Fatalf("应有 3 个位置参数，实际为 %d", len(pos))
	}
	// 归一化后总和应为 1.0
	total := 0.0
	for _, v := range pos {
		total += v.(float64)
	}
	if total < 0.99 || total > 1.01 {
		t.Errorf("归一化后总和应为 1.0，实际为 %v", total)
	}
}

// TestWeightedRankConfig_Args_过滤零值 测试零值权重过滤
func TestWeightedRankConfig_Args_过滤零值(t *testing.T) {
	w := &WeightedRankConfig{NameDense: 0, ContentDense: 0.6, ContentSparse: 0.4}
	pos, _ := w.Args()
	if len(pos) != 2 {
		t.Fatalf("过滤零值后应有 2 个参数，实际为 %d", len(pos))
	}
}

// TestRRFRankConfig_Name 测试RRF排序名称
func TestRRFRankConfig_Name(t *testing.T) {
	r := NewRRFRankConfig()
	if r.Name() != "rrf" {
		t.Errorf("Name 应为 rrf，实际为 %s", r.Name())
	}
}

// TestRRFRankConfig_HigherIsBetter 测试RRF排序方向
func TestRRFRankConfig_HigherIsBetter(t *testing.T) {
	r := NewRRFRankConfig()
	if !r.HigherIsBetter() {
		t.Error("RRFRankConfig HigherIsBetter 应为 true")
	}
}

// TestRRFRankConfig_IsActive_默认 测试默认通道活跃状态
func TestRRFRankConfig_IsActive_默认(t *testing.T) {
	r := NewRRFRankConfig()
	active := r.IsActive()
	if active != [3]int{1, 1, 1} {
		t.Errorf("默认3通道应全部活跃，实际为 %v", active)
	}
}

// TestRRFRankConfig_Args 测试RRF参数
func TestRRFRankConfig_Args(t *testing.T) {
	r := NewRRFRankConfig()
	pos, kw := r.Args()
	if len(pos) != 1 {
		t.Fatalf("应有 1 个位置参数，实际为 %d", len(pos))
	}
	if pos[0].(int) != 40 {
		t.Errorf("K 应为 40，实际为 %v", pos[0])
	}
	if len(kw) != 0 {
		t.Errorf("关键字参数应为空，实际为 %v", kw)
	}
}

// TestRankerRegistry_RegisterAndGet 测试注册和获取
func TestRankerRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRankerRegistry()
	rankers := map[string]any{
		"weighted": func() string { return "weighted_ranker" },
		"rrf":      func() string { return "rrf_ranker" },
	}
	reg.Register("milvus", rankers)
	v, ok := reg.GetRanker("milvus", "weighted")
	if !ok {
		t.Error("应能获取已注册的排序器")
	}
	_, ok = reg.GetRanker("milvus", "nonexistent")
	if ok {
		t.Error("未注册的策略应返回 false")
	}
	_, ok = reg.GetRanker("unknown", "weighted")
	if ok {
		t.Error("未注册的后端应返回 false")
	}
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/ -run "TestWeightedRankConfig|TestRRFRankConfig|TestRankerRegistry" -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/ranking.go internal/agentcore/store/graph/ranking_test.go
git commit -m "feat(store/graph): add ranking strategies — WeightedRankConfig, RRFRankConfig, RankerRegistry"
```

---

### Task 5: 接口与工厂 — base.go

**Files:**
- Create: `internal/agentcore/store/graph/base.go`
- Test: `internal/agentcore/store/graph/base_test.go`

- [ ] **Step 1: 创建 base.go — 接口、选项、常量、工厂、QueryExpr**

```go
package graph

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseGraphStore 图存储基础接口
//
// 对应 Python: GraphStore Protocol (base_graph_store.py)
type BaseGraphStore interface {
	// Config 获取图存储配置
	Config() *GraphConfig
	// Rebuild 重建所有集合和索引
	Rebuild(ctx context.Context) error
	// Refresh 刷新数据（flush + compact）
	Refresh(ctx context.Context, opts ...Option) error
	// Close 关闭存储连接
	Close() error

	// AddEntity 添加实体，自动嵌入向量（除非 no_embed=true）
	AddEntity(ctx context.Context, entities []*Entity, opts ...Option) error
	// AddRelation 添加关系，自动嵌入向量（除非 no_embed=true）
	AddRelation(ctx context.Context, relations []*Relation, opts ...Option) error
	// AddEpisode 添加片段，自动嵌入向量（除非 no_embed=true）
	AddEpisode(ctx context.Context, episodes []*Episode, opts ...Option) error

	// Query 按ID或过滤表达式查询数据
	Query(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error)
	// Delete 按ID或过滤表达式删除数据
	Delete(ctx context.Context, collection string, opts ...Option) error
	// IsEmpty 检查集合是否为空
	IsEmpty(ctx context.Context, collection string) (bool, error)

	// Search 混合搜索，支持 BFS 图扩展和可选 reranking
	Search(ctx context.Context, query string, opts ...Option) (map[string][]map[string]any, error)

	// AttachEmbedder 绑定嵌入模型
	AttachEmbedder(embedder embedding.BaseEmbedding)
}

// QueryExpr 查询过滤表达式接口（最小定义，4.28 完善后替换）
type QueryExpr interface {
	// ToExpr 将过滤表达式转换为后端特定格式
	ToExpr(backend string) (string, error)
}

// Options 图存储操作选项
type Options struct {
	// 写入选项
	Flush   bool
	Upsert  bool
	NoEmbed bool

	// 查询选项
	IDs           []any
	Expr          QueryExpr
	SilenceErrors bool

	// 搜索选项
	Collection     string
	K              int
	RankerConfig   BaseRankConfig
	Reranker       reranker.BaseReranker
	BFSDepth       int
	BFSK           int
	FilterExpr     QueryExpr
	OutputFields   []string
	QueryEmbedding []float64
	Language       string
	MinScore       float64
}

// Option 函数式选项
type Option func(*Options)

// GraphStoreFactory 图存储工厂（线程安全）
type GraphStoreFactory struct {
	mu       sync.RWMutex
	backends map[string]func(*GraphConfig) (BaseGraphStore, error)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EntityCollection 实体集合名称
	EntityCollection = "ENTITY_COLLECTION"
	// RelationCollection 关系集合名称
	RelationCollection = "RELATION_COLLECTION"
	// EpisodeCollection 片段集合名称
	EpisodeCollection = "EPISODE_COLLECTION"
	// AllCollections 搜索全部集合
	AllCollections = "all"
	// DefaultWorkerNum 默认嵌入并发数
	DefaultWorkerNum = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalFactory 全局图存储工厂
	globalFactory = &GraphStoreFactory{
		backends: make(map[string]func(*GraphConfig) (BaseGraphStore, error)),
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// newOptions 应用选项
func newOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithFlush 设置写入后刷盘
func WithFlush(flush bool) Option {
	return func(o *Options) { o.Flush = flush }
}

// WithUpsert 设置覆盖写入
func WithUpsert(upsert bool) Option {
	return func(o *Options) { o.Upsert = upsert }
}

// WithNoEmbed 跳过自动嵌入
func WithNoEmbed(noEmbed bool) Option {
	return func(o *Options) { o.NoEmbed = noEmbed }
}

// WithIDs 按ID查询/删除
func WithIDs(ids ...any) Option {
	return func(o *Options) { o.IDs = ids }
}

// WithExpr 按过滤表达式查询/删除
func WithExpr(expr QueryExpr) Option {
	return func(o *Options) { o.Expr = expr }
}

// WithSilenceErrors 静默错误
func WithSilenceErrors(silence bool) Option {
	return func(o *Options) { o.SilenceErrors = silence }
}

// WithCollection 指定搜索集合
func WithCollection(collection string) Option {
	return func(o *Options) { o.Collection = collection }
}

// WithK 设置搜索返回数量
func WithK(k int) Option {
	return func(o *Options) { o.K = k }
}

// WithRankerConfig 设置排序策略
func WithRankerConfig(config BaseRankConfig) Option {
	return func(o *Options) { o.RankerConfig = config }
}

// WithReranker 设置重排序器
func WithReranker(r reranker.BaseReranker) Option {
	return func(o *Options) { o.Reranker = r }
}

// WithBFS 设置BFS图扩展参数
func WithBFS(depth, k int) Option {
	return func(o *Options) { o.BFSDepth = depth; o.BFSK = k }
}

// WithFilterExpr 设置搜索过滤表达式
func WithFilterExpr(expr QueryExpr) Option {
	return func(o *Options) { o.FilterExpr = expr }
}

// WithOutputFields 设置返回字段
func WithOutputFields(fields ...string) Option {
	return func(o *Options) { o.OutputFields = fields }
}

// WithQueryEmbedding 直接提供查询向量（跳过嵌入步骤）
func WithQueryEmbedding(embedding []float64) Option {
	return func(o *Options) { o.QueryEmbedding = embedding }
}

// WithLanguage 设置搜索语言
func WithLanguage(lang string) Option {
	return func(o *Options) { o.Language = lang }
}

// WithMinScore 设置最低相似度分数
func WithMinScore(score float64) Option {
	return func(o *Options) { o.MinScore = score }
}

// RegisterBackend 注册后端构造函数
func RegisterBackend(name string, constructor func(*GraphConfig) (BaseGraphStore, error), force ...bool) error {
	globalFactory.mu.Lock()
	defer globalFactory.mu.Unlock()

	if existing, ok := globalFactory.backends[name]; ok {
		if len(force) == 0 || !force[0] {
			return exception.BuildError(exception.StatusStoreGraphBackendAlreadyExists,
				exception.WithParam("name", name),
				exception.WithParam("existing", fmt.Sprintf("%v", existing)))
		}
	}
	globalFactory.backends[name] = constructor
	return nil
}

// NewFromConfig 从配置创建图存储实例
func NewFromConfig(config *GraphConfig, backendName ...string) (BaseGraphStore, error) {
	globalFactory.mu.RLock()
	defer globalFactory.mu.RUnlock()

	name := config.Backend
	if len(backendName) > 0 && backendName[0] != "" {
		name = backendName[0]
	}
	constructor, ok := globalFactory.backends[name]
	if !ok {
		return nil, exception.BuildError(exception.StatusStoreGraphBackendNotFound,
			exception.WithParam("name", name))
	}
	return constructor(config)
}
```

- [ ] **Step 2: 补充 utils.go 中的 EnsureUniqueUUIDs**

在 utils.go 的 import 中添加 `"context"` 和 `"strings"`（如缺少），确保 `EnsureUniqueUUIDs` 函数可以编译。

- [ ] **Step 3: 创建 base_test.go**

```go
package graph

import (
	"context"
	"testing"
)

// fakeGraphStore 用于测试的模拟图存储
type fakeGraphStore struct {
	config *GraphConfig
}

func (f *fakeGraphStore) Config() *GraphConfig                                { return f.config }
func (f *fakeGraphStore) Rebuild(ctx context.Context) error                   { return nil }
func (f *fakeGraphStore) Refresh(ctx context.Context, opts ...Option) error   { return nil }
func (f *fakeGraphStore) Close() error                                        { return nil }
func (f *fakeGraphStore) AddEntity(ctx context.Context, entities []*Entity, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) AddRelation(ctx context.Context, relations []*Relation, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) AddEpisode(ctx context.Context, episodes []*Episode, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) Query(ctx context.Context, collection string, opts ...Option) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeGraphStore) Delete(ctx context.Context, collection string, opts ...Option) error {
	return nil
}
func (f *fakeGraphStore) IsEmpty(ctx context.Context, collection string) (bool, error) {
	return true, nil
}
func (f *fakeGraphStore) Search(ctx context.Context, query string, opts ...Option) (map[string][]map[string]any, error) {
	return nil, nil
}
func (f *fakeGraphStore) AttachEmbedder(embedder embedding.BaseEmbedding) {}

// TestRegisterBackend_正常注册 测试正常注册
func TestRegisterBackend_正常注册(t *testing.T) {
	// 使用独立的 factory 避免污染全局
	f := &GraphStoreFactory{backends: make(map[string]func(*GraphConfig) (BaseGraphStore, error))}
	constructor := func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{config: cfg}, nil
	}
	f.backends["test"] = constructor
	if _, ok := f.backends["test"]; !ok {
		t.Error("注册后应能找到后端")
	}
}

// TestNewFromConfig_正常创建 测试正常创建
func TestNewFromConfig_正常创建(t *testing.T) {
	// 临时注册
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	err := RegisterBackend("test_backend", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{config: cfg}, nil
	})
	if err != nil {
		t.Fatalf("注册后端失败: %v", err)
	}

	cfg := NewGraphConfig("http://localhost:19530")
	cfg.Backend = "test_backend"
	store, err := NewFromConfig(cfg)
	if err != nil {
		t.Fatalf("创建图存储失败: %v", err)
	}
	if store == nil {
		t.Error("创建的图存储不应为 nil")
	}
}

// TestNewFromConfig_未找到后端 测试未找到后端
func TestNewFromConfig_未找到后端(t *testing.T) {
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	cfg := NewGraphConfig("http://localhost:19530")
	cfg.Backend = "nonexistent"
	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Error("未注册的后端应返回错误")
	}
}

// TestRegisterBackend_重复注册 测试重复注册
func TestRegisterBackend_重复注册(t *testing.T) {
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	_ = RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	})
	err := RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	})
	if err == nil {
		t.Error("重复注册应返回错误")
	}
}

// TestRegisterBackend_强制覆盖 测试强制覆盖
func TestRegisterBackend_强制覆盖(t *testing.T) {
	origBackends := globalFactory.backends
	globalFactory.backends = make(map[string]func(*GraphConfig) (BaseGraphStore, error))
	defer func() { globalFactory.backends = origBackends }()

	_ = RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	})
	err := RegisterBackend("dup", func(cfg *GraphConfig) (BaseGraphStore, error) {
		return &fakeGraphStore{}, nil
	}, true)
	if err != nil {
		t.Errorf("强制覆盖不应返回错误: %v", err)
	}
}

// TestNewOptions 测试选项应用
func TestNewOptions(t *testing.T) {
	opts := newOptions(
		WithFlush(true),
		WithUpsert(false),
		WithK(10),
		WithCollection(EntityCollection),
	)
	if !opts.Flush {
		t.Error("Flush 应为 true")
	}
	if opts.Upsert {
		t.Error("Upsert 应为 false")
	}
	if opts.K != 10 {
		t.Errorf("K 应为 10，实际为 %d", opts.K)
	}
	if opts.Collection != EntityCollection {
		t.Errorf("Collection 应为 %s，实际为 %s", EntityCollection, opts.Collection)
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/ -v`

Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/graph/base.go internal/agentcore/store/graph/base_test.go internal/agentcore/store/graph/utils.go
git commit -m "feat(store/graph): add BaseGraphStore interface, Options, factory, and QueryExpr"
```

---

### Task 6: 包文档 — doc.go

**Files:**
- Create: `internal/agentcore/store/graph/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package graph 提供知识图谱图存储的接口定义和核心数据模型。
//
// 图存储用于管理知识图谱中的实体（Entity）、关系（Relation）和片段（Episode），
// 支持基于 Milvus 的混合语义搜索（dense embedding + BM25 sparse）、BFS 图扩展和可选 reranking。
// 本包定义了 BaseGraphStore 接口、图对象模型、配置体系、排序策略和工厂模式，
// 具体后端实现由 milvus 子包提供。
//
// 文件目录：
//
//	graph/
//	├── doc.go           # 包文档
//	├── base.go          # BaseGraphStore 接口 / Options / Option / QueryExpr / 常量 / 工厂
//	├── graph_object.go  # BaseGraphObject / NamedGraphObject / Entity / Relation / Episode / EmbedTask
//	├── config.go        # GraphConfig / GraphStoreStorageConfig / GraphStoreIndexConfig / BM25Config
//	├── ranking.go       # BaseRankConfig / WeightedRankConfig / RRFRankConfig / RankerRegistry
//	└── utils.go         # UUID生成 / 时间戳转换 / 批处理 / 格式化
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/graph/
//
// 核心类型/接口索引：
//
//	BaseGraphStore — 图存储基础接口
//	Entity — 知识图谱实体（节点）
//	Relation — 知识图谱关系（边）
//	Episode — 对话片段
//	GraphConfig — 图存储顶层配置
//	BaseRankConfig — 排序策略接口
//	GraphStoreFactory — 图存储工厂
package graph
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/store/graph/doc.go
git commit -m "feat(store/graph): add package doc.go"
```

---

### Task 7: Milvus Schema 构建 — milvus/schema.go

**Files:**
- Create: `internal/agentcore/store/graph/milvus/schema.go`
- Test: `internal/agentcore/store/graph/milvus/schema_test.go`

- [ ] **Step 1: 创建 milvus/schema.go**

此文件实现三个集合的 Schema 构建和索引创建。对照 Python 的 `generate_milvus_schema.py`。

关键设计点：
- 三个集合共享通用字段（通过 `addCommonFields` 函数避免重复）
- Entity 集合额外有 name/name_embedding/attributes/relations/episodes
- Relation 集合额外有 name/lhs/rhs/valid_since/valid_until/offset_since/offset_until
- Episode 集合额外有 valid_since/entities
- 所有集合都有 content/content_embedding/content_bm25
- Go SDK v2.4.2 不支持 BM25 Function，content_bm25 字段使用 `entity.FieldTypeSparseVector`
- 索引使用 `entity.NewIndexSparseInverted` 作为稀疏向量索引

完整代码需包含：
- `EnsureCollections(ctx, client, storageCfg, indexCfg, embedDim) error`
- `buildEntitySchema(storageCfg, embedDim) (*entity.Schema, error)`
- `buildRelationSchema(storageCfg, embedDim) (*entity.Schema, error)`
- `buildEpisodeSchema(storageCfg, embedDim) (*entity.Schema, error)`
- `buildIndexParams(indexCfg, collection) ([]entity.Index, []string, error)`
- `addCommonFields(schema *entity.Schema, storageCfg *GraphStoreStorageConfig, embedDim int)`

- [ ] **Step 2: 创建 milvus/schema_test.go**

测试内容：
- `TestBuildEntitySchema_字段完整性`
- `TestBuildRelationSchema_字段完整性`
- `TestBuildEpisodeSchema_字段完整性`
- `TestBuildIndexParams_Entity三索引`
- `TestBuildIndexParams_Relation双索引`

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/milvus/ -run "TestBuild.*Schema|TestBuildIndexParams" -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/milvus/schema.go internal/agentcore/store/graph/milvus/schema_test.go
git commit -m "feat(store/graph/milvus): add collection schema and index building"
```

---

### Task 8: Milvus Writer — milvus/milvus_writer.go

**Files:**
- Create: `internal/agentcore/store/graph/milvus/milvus_writer.go`
- Test: `internal/agentcore/store/graph/milvus/milvus_writer_test.go`

- [ ] **Step 1: 创建 milvus_writer.go**

实现 `graphWriter` 结构体和写入逻辑：

```go
type graphWriter struct {
    client     milvusClient
    storageCfg *graph.GraphStoreStorageConfig
    embedder   embedding.BaseEmbedding
    embedDim   int
    batchSize  int
    sem        chan struct{}
}
```

核心方法：
- `addEntity(ctx, entities, opts...) error`：收集 EmbedTasks → 批量嵌入 → 截断字段 → 序列化 → Upsert/Insert → Flush
- `addRelation(ctx, relations, opts...) error`：同上
- `addEpisode(ctx, episodes, opts...) error`：同上
- `delete(ctx, collection, opts...) error`：构建过滤表达式 → client.Delete
- `addData(ctx, collection, data, opts...) error`：通用写入流程
- `fetchAndEmbed(ctx, tasks) error`：批量调用 embedder.EmbedDocuments → 回填向量
- `truncateFields(objMap, collection) map[string]any`：按 storageCfg 截断超长字段

写入流程对照 Python `_add_data`：
1. 遍历图对象 → EmbedTasks()
2. noEmbed=false && embedder!=nil → fetchAndEmbed
3. truncateFields
4. ToMap → []map[string]any → 构建 Columns → client.Upsert/Insert
5. flush → client.Flush
6. 批量插入失败 → 逐条回退

- [ ] **Step 2: 创建 milvus_writer_test.go**

测试内容：
- `TestGraphWriter_AddEntity_基本写入`
- `TestGraphWriter_AddEntity_自动嵌入`
- `TestGraphWriter_AddEntity_NoEmbed跳过嵌入`
- `TestGraphWriter_AddRelation_基本写入`
- `TestGraphWriter_AddEpisode_基本写入`
- `TestGraphWriter_Delete_按ID删除`
- `TestGraphWriter_TruncateFields_截断超长内容`

需要 fake 组件：`fakeMilvusClient`, `fakeEmbedder`

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/milvus/ -run "TestGraphWriter" -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/milvus/milvus_writer.go internal/agentcore/store/graph/milvus/milvus_writer_test.go
git commit -m "feat(store/graph/milvus): add graphWriter — entity/relation/episode write logic"
```

---

### Task 9: Milvus Searcher — milvus/milvus_searcher.go

**Files:**
- Create: `internal/agentcore/store/graph/milvus/milvus_searcher.go`
- Test: `internal/agentcore/store/graph/milvus/milvus_searcher_test.go`

- [ ] **Step 1: 创建 milvus_searcher.go**

实现 `graphSearcher` 结构体和搜索逻辑：

```go
type graphSearcher struct {
    client   milvusClient
    embedder embedding.BaseEmbedding
    indexCfg *graph.GraphStoreIndexConfig
    registry *graph.RankerRegistry
    metric   entity.MetricType // COSINE/L2/IP
}
```

核心方法：
- `search(ctx, query, opts...) (map[string][]map[string]any, error)`：主入口
- `searchAll(ctx, query, opts...) (map[string][]map[string]any, error)`：并发搜索三集合 + combinedRerank
- `searchSingle(ctx, query, collection, opts...) ([]map[string]any, error)`：单集合搜索（有/无BFS）
- `rawHybridSearch(ctx, query, collection, k, rankerConfig, opts...) ([]map[string]any, error)`：原始混合搜索
- `buildSearchRequests(queryEmbedding []float64, k int, expr string) ([]*client.ANNSearchRequest, error)`：构建3路搜索请求
- `getRankerAndRequests(rankerConfig, collection, requests) (client.Reranker, []*client.ANNSearchRequest, error)`：调整权重构建排序器
- `expandEntities(ctx, expr, uuids) (map[string]struct{}, error)`：BFS实体扩展
- `expandRelations(ctx, expr, uuids) (map[string]struct{}, error)`：BFS关系扩展
- `rankResults(ctx, query, candidates, rk, language, minScore) ([]map[string]any, error)`：minScore过滤 + reranking
- `combinedRerank(ctx, query, results, rk, language, minScore) (map[string][]map[string]any, error)`：全集合增强重排序
- `queryEmbedding(ctx, query) ([]float64, error)`：获取查询向量

HybridSearch 调用方式：
```go
results, err := s.client.HybridSearch(ctx, collection, nil, k, outputFields, reranker, searchRequests, opts...)
```

Reranker 构建：
```go
// WeightedReranker
reranker := client.NewWeightedReranker(normalizedWeights)

// RRFReranker
reranker := client.NewRRFReranker() // 默认 K=60
```

ANNSearchRequest 构建：
```go
// Dense 向量搜索
req := client.NewANNSearchRequest(fieldName, metricType, expr, vectors, searchParam, limit)

// Sparse 向量搜索
sparseEmb := entity.NewSparseEmbedding(dimValues, scoreValues)
req := client.NewANNSearchRequest(fieldName, entity.IP, expr, []entity.Vector{sparseEmb}, searchParam, limit)
```

- [ ] **Step 2: 创建 milvus_searcher_test.go**

测试内容：
- `TestGraphSearcher_Search_单集合无BFS`
- `TestGraphSearcher_Search_单集合有BFS`
- `TestGraphSearcher_SearchAll_三集合并发`
- `TestGraphSearcher_RawHybridSearch_三通道`
- `TestGraphSearcher_ExpandEntities_BFS扩展`
- `TestGraphSearcher_ExpandRelations_BFS扩展`
- `TestGraphSearcher_RankResults_过滤和重排序`
- `TestGraphSearcher_CombinedRerank_增强重排序`
- `TestBuildSearchRequests_三路请求构建`
- `TestGetRankerAndRequests_Entity三通道`
- `TestGetRankerAndRequests_Relation两通道`

需要 fake 组件：`fakeMilvusClientWithSearch`, `fakeEmbedder`, `fakeReranker`

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/milvus/ -run "TestGraphSearcher|TestBuildSearch|TestGetRanker" -v`

Expected: ALL PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/milvus/milvus_searcher.go internal/agentcore/store/graph/milvus/milvus_searcher_test.go
git commit -m "feat(store/graph/milvus): add graphSearcher — hybrid search, BFS expansion, reranking"
```

---

### Task 10: Milvus 主结构体 — milvus/milvus.go

**Files:**
- Create: `internal/agentcore/store/graph/milvus/milvus.go`
- Create: `internal/agentcore/store/graph/milvus/doc.go`
- Test: `internal/agentcore/store/graph/milvus/milvus_test.go`

- [ ] **Step 1: 创建 milvus.go — MilvusGraphStore 主结构体**

```go
package milvus

import (
    "context"
    "sync"

    "github.com/milvus-io/milvus-sdk-go/v2/client"
    "github.com/milvus-io/milvus-sdk-go/v2/entity"

    graph "github.com/uapclaw/uapclaw-go/internal/agentcore/store/graph"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusGraphStore Milvus图存储实现
type MilvusGraphStore struct {
    *graphWriter
    *graphSearcher

    config       *graph.GraphConfig
    client       milvusClient
    createClient func(ctx context.Context, uri, token, dbName string) (milvusClient, error)
    mu           sync.RWMutex
    initialized  bool
}

// ──────────────────────────── 常量 ────────────────────────────

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusGraphStore 创建Milvus图存储实例
func NewMilvusGraphStore(config *graph.GraphConfig) *MilvusGraphStore { ... }

// 编译时检查
var _ graph.BaseGraphStore = (*MilvusGraphStore)(nil)

// BaseGraphStore 接口实现（委托给嵌入的 writer/searcher + 自身逻辑）

func (s *MilvusGraphStore) Config() *graph.GraphConfig { return s.config }
func (s *MilvusGraphStore) Rebuild(ctx context.Context) error { ... }
func (s *MilvusGraphStore) Refresh(ctx context.Context, opts ...graph.Option) error { ... }
func (s *MilvusGraphStore) Close() error { ... }
func (s *MilvusGraphStore) AttachEmbedder(embedder embedding.BaseEmbedding) { ... }
func (s *MilvusGraphStore) AddEntity(ctx context.Context, entities []*graph.Entity, opts ...graph.Option) error {
    return s.graphWriter.addEntity(ctx, entities, opts...)
}
func (s *MilvusGraphStore) AddRelation(ctx context.Context, relations []*graph.Relation, opts ...graph.Option) error {
    return s.graphWriter.addRelation(ctx, relations, opts...)
}
func (s *MilvusGraphStore) AddEpisode(ctx context.Context, episodes []*graph.Episode, opts ...graph.Option) error {
    return s.graphWriter.addEpisode(ctx, episodes, opts...)
}
func (s *MilvusGraphStore) Query(ctx context.Context, collection string, opts ...graph.Option) ([]map[string]any, error) {
    ...
}
func (s *MilvusGraphStore) Delete(ctx context.Context, collection string, opts ...graph.Option) error {
    return s.graphWriter.delete(ctx, collection, opts...)
}
func (s *MilvusGraphStore) IsEmpty(ctx context.Context, collection string) (bool, error) { ... }
func (s *MilvusGraphStore) Search(ctx context.Context, query string, opts ...graph.Option) (map[string][]map[string]any, error) {
    return s.graphSearcher.search(ctx, query, opts...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient lazy 初始化 Milvus 客户端（double-check locking）
func (s *MilvusGraphStore) getClient(ctx context.Context) (milvusClient, error) { ... }

// defaultCreateClient 默认客户端创建函数
func defaultCreateClient(ctx context.Context, uri, token, dbName string) (milvusClient, error) {
    return client.NewClient(ctx, client.Config{Address: uri, APIKey: token, DBName: dbName})
}

// init 注册 Milvus 后端和排序器
func init() {
    _ = graph.RegisterBackend("milvus", func(cfg *graph.GraphConfig) (graph.BaseGraphStore, error) {
        return NewMilvusGraphStore(cfg), nil
    })
    graph.GlobalRankerRegistry.Register("milvus", map[string]any{
        "weighted": func(weights []float64) client.Reranker { return client.NewWeightedReranker(weights) },
        "rrf":      func() client.Reranker { return client.NewRRFReranker() },
    })
}
```

同时定义 `milvusClient` 私有接口（与 vector 包模式一致）。

- [ ] **Step 2: 创建 milvus/doc.go**

```go
// Package milvus 提供 Graph Store 的 Milvus 后端实现。
//
// 基于 Milvus 向量数据库实现图存储，支持三集合（Entity/Relation/Episode）的
// CRUD 操作、3通道混合搜索（name_embedding + content_embedding + content_bm25）、
// BFS 图扩展和可选 reranking。
//
// 文件目录：
//
//	milvus/
//	├── doc.go              # 包文档
//	├── milvus.go           # MilvusGraphStore 主结构体 + 接口委托 + lazy init
//	├── milvus_writer.go    # graphWriter 写入逻辑
//	├── milvus_searcher.go  # graphSearcher 搜索逻辑
//	└── schema.go           # 集合 Schema 和索引构建
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/graph/milvus/
package milvus
```

- [ ] **Step 3: 创建 milvus_test.go**

测试内容：
- `TestNewMilvusGraphStore_创建`
- `TestMilvusGraphStore_LazyInit`
- `TestMilvusGraphStore_Close`
- `TestMilvusGraphStore_AttachEmbedder`
- `TestMilvusGraphStore_Rebuild`
- `TestMilvusGraphStore_编译时接口检查`

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/milvus/ -v`

Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/graph/milvus/milvus.go internal/agentcore/store/graph/milvus/doc.go internal/agentcore/store/graph/milvus/milvus_test.go
git commit -m "feat(store/graph/milvus): add MilvusGraphStore — main struct, lazy init, interface delegation"
```

---

### Task 11: 集成测试和覆盖率检查

**Files:**
- Create: `internal/agentcore/store/graph/milvus/milvus_integration_test.go`

- [ ] **Step 1: 创建集成测试占位**

```go
//go:build integration

package milvus

// TestMilvusGraphStore_真实调用 测试真实Milvus图存储读写
// 运行方式: go test -tags=integration ./internal/agentcore/store/graph/milvus/...
func TestMilvusGraphStore_真实调用(t *testing.T) {
    // TODO: 需要真实 Milvus 实例，从环境变量读取 URI
}
```

- [ ] **Step 2: 运行全量单元测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/... -v`

Expected: ALL PASS

- [ ] **Step 3: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/graph/...`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/graph/milvus/milvus_integration_test.go
git commit -m "feat(store/graph/milvus): add integration test placeholder"
```

---

### Task 12: 更新实现计划状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 4.26 状态为 ✅**

将 `| 4.26 | ☐ | Graph Store |` 更新为 `| 4.26 | ✅ | Graph Store |`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: mark step 4.26 Graph Store as completed"
```
