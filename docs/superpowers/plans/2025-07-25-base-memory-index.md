# BaseMemoryIndex 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `internal/agentcore/store/index/` 包，提供记忆索引的抽象接口 BaseMemoryIndex、数据模型 MemoryDoc、编解码器接口 StorageCodec，以及默认实现基类 MemoryIndexBase。

**Architecture:** 采用"单接口 + 嵌入结构体"方案，BaseMemoryIndex interface 包含全部 16 个方法（严格对齐 Python ABC），MemoryIndexBase struct 提供 7 个非抽象方法的默认实现，具体实现类嵌入 MemoryIndexBase 后只需实现 9 个核心抽象方法。

**Tech Stack:** Go 1.x, github.com/google/uuid, github.com/uapclaw/uapclaw-go/internal/common/exception, github.com/uapclaw/uapclaw-go/internal/common/logger

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/store/index/base.go` | StorageCodec + MemoryDoc + MemorySearchResult + UserScope + BaseMemoryIndex + MemoryIndexBase |
| 创建 | `internal/agentcore/store/index/base_test.go` | 上述所有类型的单元测试 |
| 创建 | `internal/agentcore/store/index/doc.go` | 包文档 |
| 修改 | `internal/common/exception/codes_context.go` | 新增 StatusMemoryBackupNotFound 错误码 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 步骤 4.17 状态 ☐→✅ |

---

### Task 1: 新增错误码 StatusMemoryBackupNotFound

**Files:**
- Modify: `internal/common/exception/codes_context.go` (Memory Engine var 块末尾)

- [ ] **Step 1: 在 codes_context.go 的 Memory Engine var 块末尾添加错误码**

在 `StatusMemoryMigrateMemoryExecutionError`（158010）之后添加：

```go
	// StatusMemoryBackupNotFound 备份不存在
	StatusMemoryBackupNotFound = NewStatusCode(
		"MEMORY_BACKUP_NOT_FOUND", 158011,
		"backup not found, backup_id: {backup_id}")
```

- [ ] **Step 2: 运行编译确认无语法错误**

Run: `go build ./internal/common/exception/...`
Expected: 编译成功，无输出

- [ ] **Step 3: 运行 exception 包已有测试确认无回归**

Run: `go test ./internal/common/exception/... -count=1`
Expected: 所有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/common/exception/codes_context.go
git commit -m "feat(store): 新增 StatusMemoryBackupNotFound 错误码 (158011)"
```

---

### Task 2: 创建 index 包目录和 doc.go

**Files:**
- Create: `internal/agentcore/store/index/doc.go`

- [ ] **Step 1: 创建 index 目录**

Run: `mkdir -p internal/agentcore/store/index`

- [ ] **Step 2: 编写 doc.go**

```go
// Package index 提供记忆索引的抽象接口和数据模型。
//
// 本包定义了所有记忆索引实现必须满足的 BaseMemoryIndex 接口，
// 以及 MemoryDoc 数据模型、StorageCodec 编解码器接口、
// MemorySearchResult 搜索结果类型和 MemoryIndexBase 默认实现基类。
// 具体实现类（如 SimpleMemoryIndex）嵌入 MemoryIndexBase 后
// 只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。
//
// 文件目录：
//
//	index/
//	├── doc.go           # 包文档
//	├── base.go          # StorageCodec + MemoryDoc + MemorySearchResult + UserScope + BaseMemoryIndex + MemoryIndexBase
//	└── base_test.go     # 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_memory_index.py
//
// 核心类型/接口索引：
//
//	StorageCodec        — 存储编解码器接口，用于对记忆文本加解密
//	MemoryDoc           — 记忆文档数据模型（ID/Text/Type/Timestamp/Fields）
//	MemorySearchResult  — 搜索结果，包含 MemoryDoc 和相关度分数
//	UserScope           — 用户-作用域对，ListUserScopes 返回值
//	BaseMemoryIndex     — 记忆索引抽象接口（16 个方法）
//	MemoryIndexBase     — 默认实现基类，提供 7 个非抽象方法的通用行为
package index
```

- [ ] **Step 3: 运行编译确认 doc.go 语法正确**

Run: `go build ./internal/agentcore/store/index/...`
Expected: 编译成功（此时包内无其他文件，但 doc.go 本身不依赖其他文件）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/index/doc.go
git commit -m "feat(store): 创建 index 包及 doc.go 包文档"
```

---

### Task 3: 编写 base.go — 核心数据类型

**Files:**
- Create: `internal/agentcore/store/index/base.go`

- [ ] **Step 1: 编写 base.go 的 package/import 和核心数据类型部分**

```go
package index

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryDoc 记忆文档，表示一条存储的记忆条目。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (MemoryDoc)
type MemoryDoc struct {
	// ID 唯一标识
	ID string `json:"id"`
	// Text 文本内容
	Text string `json:"text"`
	// Type 类型/分类
	Type string `json:"type"`
	// Timestamp 时间戳
	Timestamp time.Time `json:"timestamp"`
	// Fields 扩展字段
	Fields map[string]any `json:"fields,omitempty"`
}

// MemorySearchResult 记忆搜索结果，包含匹配文档和相关度分数。
//
// 对应 Python: search 方法返回的 tuple[MemoryDoc, float]
type MemorySearchResult struct {
	// Doc 匹配的记忆文档
	Doc *MemoryDoc
	// Score 相关度分数，范围 [0, 1]，越高越相关
	Score float64
}

// UserScope 用户-作用域对，用于 ListUserScopes 返回值。
//
// 对应 Python: list_user_scopes 返回的 tuple[str, str]
type UserScope struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
}
```

- [ ] **Step 2: 运行编译确认数据类型定义正确**

Run: `go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/base.go
git commit -m "feat(store): 添加 MemoryDoc/MemorySearchResult/UserScope 数据类型"
```

---

### Task 4: 编写 base.go — StorageCodec 接口和 BaseMemoryIndex 接口

**Files:**
- Modify: `internal/agentcore/store/index/base.go`

- [ ] **Step 1: 在结构体区块之后添加 StorageCodec 接口（归类到结构体区块，接口排在结构体之前）**

在 `UserScope` 结构体定义之后、枚举区块之前添加：

```go
// StorageCodec 存储编解码器接口，用于对记忆文本进行加解密。
//
// 实现示例：AES 编解码器，对记忆文本进行加密存储和解密读取。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (StorageCodec)
type StorageCodec interface {
	// Encode 对文本进行编码（如加密）
	Encode(text string) string
	// Decode 对数据进行解码（如解密）
	Decode(data string) string
}
```

- [ ] **Step 2: 在 StorageCodec 之后添加 BaseMemoryIndex 接口**

```go
// BaseMemoryIndex 记忆索引抽象接口，定义记忆文档的存储和检索操作。
//
// 所有记忆索引实现必须实现此接口。记忆文档以 user_id 和 scope_id 隔离，
// 支持多租户和多场景的记忆管理。
//
// 对应 Python: openjiuwen/core/foundation/store/base_memory_index.py (BaseMemoryIndex)
type BaseMemoryIndex interface {
	// SetStorageCodec 设置存储编解码器。
	SetStorageCodec(codec StorageCodec)

	// AddMemories 添加新的记忆文档。
	AddMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error

	// UpdateMemories 更新记忆文档。
	UpdateMemories(ctx context.Context, userID string, scopeID string, memories []*MemoryDoc) error

	// DeleteMemories 按 ID 删除记忆文档。
	DeleteMemories(ctx context.Context, userID string, scopeID string, ids []string) error

	// DeleteByUser 删除指定用户的所有记忆（跨所有 scope）。
	DeleteByUser(ctx context.Context, userID string) error

	// DeleteByScope 删除指定 scope 的所有记忆（跨所有 user）。
	DeleteByScope(ctx context.Context, scopeID string) error

	// DeleteByUserAndScope 删除指定用户和 scope 组合的所有记忆。
	DeleteByUserAndScope(ctx context.Context, userID string, scopeID string) error

	// Search 语义搜索记忆文档，返回最相关的结果及相关度分数。
	// memTypes 为 nil 或空切片时搜索所有类型；topK 为 0 时使用默认值 10。
	Search(ctx context.Context, userID string, scopeID string, query string, memTypes []string, topK int) ([]*MemorySearchResult, error)

	// GetByID 按 ID 获取单条记忆文档，不存在时返回 nil, nil。
	GetByID(ctx context.Context, userID string, scopeID string, memID string) (*MemoryDoc, error)

	// ListMemories 分页获取记忆文档列表。
	// memTypes 为 nil 或空切片时返回所有类型；多个 memType 时按 memType 顺序排列。
	ListMemories(ctx context.Context, userID string, scopeID string, offset int, limit int, memTypes []string) ([]*MemoryDoc, error)

	// GetSchemaVersion 获取当前 schema 版本号，未设置时返回 0。
	GetSchemaVersion() int

	// UpdateSchemaVersion 更新 schema 版本号。
	UpdateSchemaVersion(version int)

	// CreateBackup 创建当前数据的备份，返回备份标识。
	CreateBackup(ctx context.Context) (string, error)

	// RestoreBackup 从备份恢复数据。
	RestoreBackup(ctx context.Context, backupID string) error

	// CleanupBackup 清理备份。
	CleanupBackup(ctx context.Context, backupID string) error

	// ListUserScopes 列出索引中所有 (userID, scopeID) 对。
	ListUserScopes(ctx context.Context) ([]UserScope, error)
}
```

- [ ] **Step 3: 运行编译确认接口定义正确**

Run: `go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/index/base.go
git commit -m "feat(store): 添加 StorageCodec 接口和 BaseMemoryIndex 接口"
```

---

### Task 5: 编写 base.go — MemoryIndexBase 默认实现

**Files:**
- Modify: `internal/agentcore/store/index/base.go`

- [ ] **Step 1: 添加 backupData 非导出结构体和 MemoryIndexBase 结构体**

在 `BaseMemoryIndex` 接口定义之后，添加全局变量区块之前：

```go
// backupData 备份数据
type backupData struct {
	// SchemaVersion 备份时的 schema 版本
	SchemaVersion int
}

// MemoryIndexBase 记忆索引的默认实现基类。
//
// 嵌入此结构体后，实现类只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。
// 默认提供 ListMemories / GetSchemaVersion / UpdateSchemaVersion /
// CreateBackup / RestoreBackup / CleanupBackup / ListUserScopes 的通用行为。
//
// 对应 Python: BaseMemoryIndex 中的非抽象方法默认实现
type MemoryIndexBase struct {
	// schemaVersion schema 版本号
	schemaVersion int
	// backups 备份数据（内存中的简单实现）
	backups map[string]*backupData
}
```

- [ ] **Step 2: 在导出函数区块添加 NewMemoryIndexBase 构造函数**

```go
// NewMemoryIndexBase 创建记忆索引基类实例。
func NewMemoryIndexBase() *MemoryIndexBase {
	return &MemoryIndexBase{
		backups: make(map[string]*backupData),
	}
}
```

- [ ] **Step 3: 在导出函数区块添加 MemoryIndexBase 的默认实现方法**

```go
// ListMemories 分页获取记忆文档列表（默认实现：返回空结果）。
// 具体实现类应覆盖此方法以提供真正的数据列举。
func (b *MemoryIndexBase) ListMemories(_ context.Context, _ string, _ string, _ int, _ int, _ []string) ([]*MemoryDoc, error) {
	return nil, nil
}

// GetSchemaVersion 获取当前 schema 版本号，未设置时返回 0。
func (b *MemoryIndexBase) GetSchemaVersion() int {
	return b.schemaVersion
}

// UpdateSchemaVersion 更新 schema 版本号。
func (b *MemoryIndexBase) UpdateSchemaVersion(version int) {
	b.schemaVersion = version
}

// CreateBackup 创建当前数据的备份，返回备份标识。
func (b *MemoryIndexBase) CreateBackup(_ context.Context) (string, error) {
	bid := uuid.New().String()
	b.backups[bid] = &backupData{SchemaVersion: b.schemaVersion}
	return bid, nil
}

// RestoreBackup 从备份恢复数据。备份不存在时返回 StatusMemoryBackupNotFound 错误。
func (b *MemoryIndexBase) RestoreBackup(_ context.Context, backupID string) error {
	data, ok := b.backups[backupID]
	if !ok {
		return exception.BuildError(exception.StatusMemoryBackupNotFound,
			exception.WithParam("backup_id", backupID),
		)
	}
	b.schemaVersion = data.SchemaVersion
	return nil
}

// CleanupBackup 清理备份。
func (b *MemoryIndexBase) CleanupBackup(_ context.Context, backupID string) error {
	delete(b.backups, backupID)
	return nil
}

// ListUserScopes 列出索引中所有 (userID, scopeID) 对（默认实现：返回空结果）。
// 具体实现类应覆盖此方法以提供真正的数据扫描。
func (b *MemoryIndexBase) ListUserScopes(_ context.Context) ([]UserScope, error) {
	return nil, nil
}
```

- [ ] **Step 4: 在常量区块添加默认 topK 常量和 logComponent**

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTopK Search 默认返回结果数量
	defaultTopK = 10
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件常量，store 属于基础设施层
	logComponent = logger.ComponentCommon
)
```

- [ ] **Step 5: 运行编译确认全部代码正确**

Run: `go build ./internal/agentcore/store/index/...`
Expected: 编译成功

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/index/base.go
git commit -m "feat(store): 添加 MemoryIndexBase 默认实现基类"
```

---

### Task 6: 编写 base_test.go — MemoryDoc 和 MemorySearchResult 测试

**Files:**
- Create: `internal/agentcore/store/index/base_test.go`

- [ ] **Step 1: 编写测试文件头部和 MemoryDoc 测试**

```go
package index

import (
	"encoding/json"
	"testing"
	"time"
)

// ──────────────────────────── MemoryDoc 测试 ────────────────────────────

func TestMemoryDoc_JSON序列化(t *testing.T) {
	ts := time.Date(2025, 7, 25, 10, 30, 0, 0, time.UTC)
	doc := &MemoryDoc{
		ID:        "mem-001",
		Text:      "用户偏好深色模式",
		Type:      "user_profile",
		Timestamp: ts,
		Fields:    map[string]any{"priority": "high"},
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored MemoryDoc
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != doc.ID {
		t.Errorf("ID: 期望 %q, 实际 %q", doc.ID, restored.ID)
	}
	if restored.Text != doc.Text {
		t.Errorf("Text: 期望 %q, 实际 %q", doc.Text, restored.Text)
	}
	if restored.Type != doc.Type {
		t.Errorf("Type: 期望 %q, 实际 %q", doc.Type, restored.Type)
	}
	if !restored.Timestamp.Equal(doc.Timestamp) {
		t.Errorf("Timestamp: 期望 %v, 实际 %v", doc.Timestamp, restored.Timestamp)
	}
}

func TestMemoryDoc_Fields为nil时omitempty(t *testing.T) {
	doc := &MemoryDoc{
		ID:   "mem-002",
		Text: "测试",
		Type: "test",
	}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// Fields 为 nil 时，omitempty 应省略 fields 字段
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化到 map 失败: %v", err)
	}
	if _, ok := raw["fields"]; ok {
		t.Error("Fields 为 nil 时 JSON 中不应包含 fields 字段")
	}
}

func TestMemoryDoc_零值序列化(t *testing.T) {
	doc := &MemoryDoc{}

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored MemoryDoc
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.ID != "" {
		t.Errorf("ID: 期望空字符串, 实际 %q", restored.ID)
	}
	if restored.Text != "" {
		t.Errorf("Text: 期望空字符串, 实际 %q", restored.Text)
	}
	if !restored.Timestamp.IsZero() {
		t.Errorf("Timestamp: 期望零值, 实际 %v", restored.Timestamp)
	}
}

// ──────────────────────────── MemorySearchResult 测试 ────────────────────────────

func TestMemorySearchResult_字段访问(t *testing.T) {
	doc := &MemoryDoc{ID: "mem-001", Text: "测试", Type: "test"}
	result := &MemorySearchResult{Doc: doc, Score: 0.95}

	if result.Doc.ID != "mem-001" {
		t.Errorf("Doc.ID: 期望 mem-001, 实际 %q", result.Doc.ID)
	}
	if result.Score != 0.95 {
		t.Errorf("Score: 期望 0.95, 实际 %f", result.Score)
	}
}

// ──────────────────────────── UserScope 测试 ────────────────────────────

func TestUserScope_字段访问(t *testing.T) {
	us := UserScope{UserID: "user-1", ScopeID: "scope-1"}

	if us.UserID != "user-1" {
		t.Errorf("UserID: 期望 user-1, 实际 %q", us.UserID)
	}
	if us.ScopeID != "scope-1" {
		t.Errorf("ScopeID: 期望 scope-1, 实际 %q", us.ScopeID)
	}
}
```

- [ ] **Step 2: 运行测试确认通过**

Run: `go test ./internal/agentcore/store/index/... -run "TestMemoryDoc|TestMemorySearchResult|TestUserScope" -v`
Expected: 所有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/base_test.go
git commit -m "test(store): 添加 MemoryDoc/MemorySearchResult/UserScope 单元测试"
```

---

### Task 7: 编写 base_test.go — StorageCodec 接口约束测试

**Files:**
- Modify: `internal/agentcore/store/index/base_test.go`

- [ ] **Step 1: 在 base_test.go 末尾添加 StorageCodec 测试**

```go
// ──────────────────────────── StorageCodec 测试 ────────────────────────────

// fakeCodec 用于测试的模拟编解码器
type fakeCodec struct {
	encoded map[string]string
	decoded map[string]string
}

func newFakeCodec() *fakeCodec {
	return &fakeCodec{
		encoded: make(map[string]string),
		decoded: make(map[string]string),
	}
}

func (f *fakeCodec) Encode(text string) string {
	encoded := "enc:" + text
	f.encoded[encoded] = text
	return encoded
}

func (f *fakeCodec) Decode(data string) string {
	decoded := strings.TrimPrefix(data, "enc:")
	f.decoded[data] = decoded
	return decoded
}

func TestStorageCodec_接口约束(t *testing.T) {
	// 验证 fakeCodec 满足 StorageCodec 接口
	var _ StorageCodec = &fakeCodec{}
}

func TestStorageCodec_EncodeDecode(t *testing.T) {
	codec := newFakeCodec()
	original := "敏感数据"
	encoded := codec.Encode(original)
	decoded := codec.Decode(encoded)

	if decoded != original {
		t.Errorf("编解码往返失败: 期望 %q, 实际 %q", original, decoded)
	}
}
```

注意：需要在 base_test.go 的 import 中添加 `"strings"`。

- [ ] **Step 2: 运行测试确认通过**

Run: `go test ./internal/agentcore/store/index/... -run "TestStorageCodec" -v`
Expected: 所有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/store/index/base_test.go
git commit -m "test(store): 添加 StorageCodec 接口约束测试"
```

---

### Task 8: 编写 base_test.go — MemoryIndexBase 默认实现测试

**Files:**
- Modify: `internal/agentcore/store/index/base_test.go`

- [ ] **Step 1: 在 base_test.go 末尾添加 MemoryIndexBase 测试**

```go
// ──────────────────────────── MemoryIndexBase 测试 ────────────────────────────

func TestNewMemoryIndexBase(t *testing.T) {
	base := NewMemoryIndexBase()

	if base == nil {
		t.Fatal("NewMemoryIndexBase 返回 nil")
	}
	if base.backups == nil {
		t.Error("backups map 未初始化")
	}
	if len(base.backups) != 0 {
		t.Errorf("backups map 应为空, 实际长度 %d", len(base.backups))
	}
}

func TestGetSchemaVersion_默认值(t *testing.T) {
	base := NewMemoryIndexBase()

	if v := base.GetSchemaVersion(); v != 0 {
		t.Errorf("默认 schema 版本应为 0, 实际 %d", v)
	}
}

func TestUpdateSchemaVersion(t *testing.T) {
	base := NewMemoryIndexBase()

	base.UpdateSchemaVersion(3)
	if v := base.GetSchemaVersion(); v != 3 {
		t.Errorf("更新后 schema 版本应为 3, 实际 %d", v)
	}

	base.UpdateSchemaVersion(7)
	if v := base.GetSchemaVersion(); v != 7 {
		t.Errorf("再次更新后 schema 版本应为 7, 实际 %d", v)
	}
}

func TestCreateBackup(t *testing.T) {
	base := NewMemoryIndexBase()
	base.UpdateSchemaVersion(5)

	ctx := context.Background()
	bid, err := base.CreateBackup(ctx)
	if err != nil {
		t.Fatalf("CreateBackup 失败: %v", err)
	}
	if bid == "" {
		t.Error("备份 ID 不应为空")
	}
	if len(base.backups) != 1 {
		t.Errorf("backups map 应有 1 条记录, 实际 %d", len(base.backups))
	}
	if data, ok := base.backups[bid]; !ok {
		t.Error("备份 ID 在 backups map 中不存在")
	} else if data.SchemaVersion != 5 {
		t.Errorf("备份中 schema 版本应为 5, 实际 %d", data.SchemaVersion)
	}
}

func TestRestoreBackup_存在(t *testing.T) {
	base := NewMemoryIndexBase()
	base.UpdateSchemaVersion(5)

	ctx := context.Background()
	bid, _ := base.CreateBackup(ctx)

	// 修改版本号后恢复
	base.UpdateSchemaVersion(10)
	if v := base.GetSchemaVersion(); v != 10 {
		t.Fatalf("恢复前 schema 版本应为 10, 实际 %d", v)
	}

	err := base.RestoreBackup(ctx, bid)
	if err != nil {
		t.Fatalf("RestoreBackup 失败: %v", err)
	}
	if v := base.GetSchemaVersion(); v != 5 {
		t.Errorf("恢复后 schema 版本应为 5, 实际 %d", v)
	}
}

func TestRestoreBackup_不存在(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	err := base.RestoreBackup(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("恢复不存在的备份应返回错误")
	}
}

func TestCleanupBackup(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	bid, _ := base.CreateBackup(ctx)

	if len(base.backups) != 1 {
		t.Fatalf("清理前应有 1 条备份, 实际 %d", len(base.backups))
	}

	err := base.CleanupBackup(ctx, bid)
	if err != nil {
		t.Fatalf("CleanupBackup 失败: %v", err)
	}
	if len(base.backups) != 0 {
		t.Errorf("清理后应有 0 条备份, 实际 %d", len(base.backups))
	}
}

func TestCleanupBackup_不存在时不报错(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	err := base.CleanupBackup(ctx, "nonexistent-id")
	if err != nil {
		t.Errorf("清理不存在的备份应返回 nil, 实际 %v", err)
	}
}

func TestListMemories_默认(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	docs, err := base.ListMemories(ctx, "user-1", "scope-1", 0, 100, nil)
	if err != nil {
		t.Fatalf("ListMemories 失败: %v", err)
	}
	if docs != nil {
		t.Errorf("默认 ListMemories 应返回 nil, 实际 %v", docs)
	}
}

func TestListUserScopes_默认(t *testing.T) {
	base := NewMemoryIndexBase()

	ctx := context.Background()
	scopes, err := base.ListUserScopes(ctx)
	if err != nil {
		t.Fatalf("ListUserScopes 失败: %v", err)
	}
	if scopes != nil {
		t.Errorf("默认 ListUserScopes 应返回 nil, 实际 %v", scopes)
	}
}
```

注意：需要在 base_test.go 的 import 中添加 `"context"`。

- [ ] **Step 2: 运行全部测试确认通过**

Run: `go test ./internal/agentcore/store/index/... -v`
Expected: 所有测试通过

- [ ] **Step 3: 检查覆盖率**

Run: `go test -cover ./internal/agentcore/store/index/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/index/base_test.go
git commit -m "test(store): 添加 MemoryIndexBase 默认实现单元测试"
```

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将步骤 4.17 的状态从 ☐ 改为 ✅**

找到 `4.17` 行，将 `☐` 改为 `✅`。

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划步骤 4.17 状态为已完成"
```

---

### Task 10: 最终验证

- [ ] **Step 1: 运行全量编译**

Run: `go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行 index 包测试并确认覆盖率**

Run: `go test -cover -v ./internal/agentcore/store/index/...`
Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 运行项目全量测试确认无回归**

Run: `go test ./... -count=1`
Expected: 所有测试通过（如有 integration/llm/e2e 标签的测试跳过属正常）
