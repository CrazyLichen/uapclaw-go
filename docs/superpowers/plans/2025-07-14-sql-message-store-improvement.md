# SqlMessageStore 全链路改进 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对 4.16 SqlMessageStore 进行全链路改进，包括 AesStorageCodec 独立化、MemoryMetaManager 独立化、SqlDbStore 扩展、SqlMessageStore 核心重构，与 Python 实现全链路对齐。

**Architecture:** 自底向上构建——先创建底层 AesStorageCodec 和 MemoryMetaManager 独立包，再扩展 SqlDbStore 依赖层，最后重构 SqlMessageStore 核心逻辑。每层完成后独立测试验证。

**Tech Stack:** Go 1.x, GORM, AES-256-GCM (crypto 包), SQLite (测试), JSON 序列化 (encoding/json)

---

## 文件结构

```
internal/agentcore/memory/
├── codec/                              # 新增包（Task 1-2）
│   ├── doc.go
│   ├── aes_storage_codec.go
│   └── aes_storage_codec_test.go
├── migration/                          # 新增包（Task 3-4）
│   └── migrator/
│       ├── doc.go
│       ├── memory_meta_manager.go
│       └── memory_meta_manager_test.go
└── manage/
    └── model/
        ├── doc.go                      # 修改（Task 9）
        ├── db_model.go                 # 不变
        ├── db_model_test.go            # 不变
        ├── sql_db_store.go             # 修改（Task 5-6）
        ├── sql_db_store_test.go        # 修改（Task 6）
        ├── sql_message_store.go        # 重写（Task 7-8）
        ├── sql_message_store_test.go   # 重写（Task 8）
        ├── message_manager.go          # 不变
        └── message_manager_test.go     # 不变
```

---

### Task 1: 创建 AesStorageCodec 包 — doc.go

**Files:**
- Create: `internal/agentcore/memory/codec/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package codec 提供存储层编解码器。
//
// 本包定义了存储内容的加解密编解码接口和实现，
// 用于 SqlMessageStore 等存储组件对持久化数据进行透明加解密。
// 当加密密钥为空时，编解码器以 passthrough 模式运行，
// 不对数据进行任何加解密处理。
//
// 文件目录：
//
//	codec/
//	├── doc.go                  # 包文档
//	├── aes_storage_codec.go    # AES-256-GCM 存储编解码器
//	└── aes_storage_codec_test.go
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/codec/
//
// 核心类型/接口索引：
//
//	AesStorageCodec — AES-256-GCM 存储编解码器，key 为空时 passthrough，key 非空时严格模式
package codec
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/codec/...`
Expected: 编译成功（空包可编译）

---

### Task 2: 创建 AesStorageCodec 实现 + 测试

**Files:**
- Create: `internal/agentcore/memory/codec/aes_storage_codec.go`
- Create: `internal/agentcore/memory/codec/aes_storage_codec_test.go`

- [ ] **Step 1: 编写 aes_storage_codec.go**

```go
package codec

import (
	"github.com/uapclaw/uap-claw-go/internal/common/crypto"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AesStorageCodec AES-256-GCM 存储编解码器。
//
// 严格模式：key 为空时 passthrough 不加解密，key 非空时加解密失败返回 error。
// 对应 Python: openjiuwen/core/memory/codec/aes_storage_codec.py (AesStorageCodec)
// 差异：Python 加密失败返回原文（容错），Go 返回 error（严格模式）
type AesStorageCodec struct {
	// key AES 加密密钥（nil/空 → passthrough）
	key []byte
	// provider 加密提供者（key 非空时初始化）
	provider *crypto.AesGcmProvider
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAesStorageCodec 创建存储编解码器。
// key 为 nil/空 → passthrough 模式；key 非空 → 必须为 32 字节，否则返回 error。
func NewAesStorageCodec(key []byte) (*AesStorageCodec, error) {
	if len(key) == 0 {
		return &AesStorageCodec{key: nil, provider: nil}, nil
	}

	provider, err := crypto.NewAesGcmProvider(key)
	if err != nil {
		return nil, err
	}

	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)

	return &AesStorageCodec{
		key:      keyCopy,
		provider: provider,
	}, nil
}

// Encode 加密明文。
// key 为空时原样返回（passthrough）；key 非空时加密失败返回 error（严格模式）。
func (c *AesStorageCodec) Encode(plaintext string) (string, error) {
	if c.provider == nil || plaintext == "" {
		return plaintext, nil
	}
	return c.provider.Encrypt(plaintext)
}

// Decode 解密密文。
// key 为空时原样返回（passthrough）；key 非空时解密失败返回 error（严格模式）。
func (c *AesStorageCodec) Decode(ciphertext string) (string, error) {
	if c.provider == nil || ciphertext == "" {
		return ciphertext, nil
	}
	return c.provider.Decrypt(ciphertext)
}
```

- [ ] **Step 2: 编写 aes_storage_codec_test.go**

```go
package codec

import (
	"testing"
)

// TestNewAesStorageCodec_空key 验证空 key 创建 passthrough 模式
func TestNewAesStorageCodec_空key(t *testing.T) {
	c, err := NewAesStorageCodec(nil)
	if err != nil {
		t.Fatalf("空 key 不应报错: %v", err)
	}
	if c.provider != nil {
		t.Error("空 key 时 provider 应为 nil")
	}
}

// TestNewAesStorageCodec_有效key 验证 32 字节 key 正常创建
func TestNewAesStorageCodec_有效key(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewAesStorageCodec(key)
	if err != nil {
		t.Fatalf("32 字节 key 不应报错: %v", err)
	}
	if c.provider == nil {
		t.Error("有效 key 时 provider 不应为 nil")
	}
}

// TestNewAesStorageCodec_key长度错误 验证非 32 字节 key 返回 error
func TestNewAesStorageCodec_key长度错误(t *testing.T) {
	key := []byte{1, 2, 3}
	_, err := NewAesStorageCodec(key)
	if err == nil {
		t.Error("非 32 字节 key 应返回 error")
	}
}

// TestAesStorageCodec_Encode_空key_passthrough 验证空 key 时不加密
func TestAesStorageCodec_Encode_空key_passthrough(t *testing.T) {
	c, _ := NewAesStorageCodec(nil)
	result, err := c.Encode("hello")
	if err != nil {
		t.Fatalf("passthrough 模式不应报错: %v", err)
	}
	if result != "hello" {
		t.Errorf("passthrough 应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Encode_空文本 验证空字符串原样返回
func TestAesStorageCodec_Encode_空文本(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewAesStorageCodec(key)
	result, err := c.Encode("")
	if err != nil {
		t.Fatalf("空文本不应报错: %v", err)
	}
	if result != "" {
		t.Errorf("空文本应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Encode_加密成功 验证正常加密返回密文
func TestAesStorageCodec_Encode_加密成功(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)
	result, err := c.Encode("hello world")
	if err != nil {
		t.Fatalf("加密不应报错: %v", err)
	}
	if result == "hello world" {
		t.Error("加密结果不应与原文相同")
	}
}

// TestAesStorageCodec_加密往返 验证 Encode → Decode 还原原文
func TestAesStorageCodec_加密往返(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	plaintext := "secret message 你好世界"
	encrypted, err := c.Encode(plaintext)
	if err != nil {
		t.Fatalf("加密不应报错: %v", err)
	}

	decrypted, err := c.Decode(encrypted)
	if err != nil {
		t.Fatalf("解密不应报错: %v", err)
	}

	if decrypted != plaintext {
		t.Errorf("解密结果 = %q, want %q", decrypted, plaintext)
	}
}

// TestAesStorageCodec_Decode_空key_passthrough 验证空 key 时不解密
func TestAesStorageCodec_Decode_空key_passthrough(t *testing.T) {
	c, _ := NewAesStorageCodec(nil)
	result, err := c.Decode("ciphertext")
	if err != nil {
		t.Fatalf("passthrough 模式不应报错: %v", err)
	}
	if result != "ciphertext" {
		t.Errorf("passthrough 应原样返回, got %q", result)
	}
}

// TestAesStorageCodec_Decode_解密失败 验证篡改密文时返回 error（严格模式）
func TestAesStorageCodec_Decode_解密失败(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	_, err := c.Decode("invalid_ciphertext_data")
	if err == nil {
		t.Error("篡改密文时应返回 error（严格模式）")
	}
}

// TestAesStorageCodec_Encode_多模态内容 验证 JSON array 格式内容加解密
func TestAesStorageCodec_Encode_多模态内容(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	c, _ := NewAesStorageCodec(key)

	multimodal := `[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]`
	encrypted, err := c.Encode(multimodal)
	if err != nil {
		t.Fatalf("多模态内容加密不应报错: %v", err)
	}

	decrypted, err := c.Decode(encrypted)
	if err != nil {
		t.Fatalf("多模态内容解密不应报错: %v", err)
	}

	if decrypted != multimodal {
		t.Errorf("解密结果不匹配, got %q", decrypted)
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/codec/... -v`
Expected: 所有测试 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/codec/
git commit -m "feat(memory): 新增 AesStorageCodec 独立编解码包

- AesStorageCodec 支持空 key passthrough 模式和严格加密模式
- key 非空时加密/解密失败返回 error（与 Python 容错模式不同）
- 底层委托 crypto.AesGcmProvider，密文格式与 Python 兼容
- 对应 Python: openjiuwen/core/memory/codec/aes_storage_codec.py"
```

---

### Task 3: 创建 MemoryMetaManager 包 — doc.go

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package migrator 提供存储层迁移管理工具。
//
// 本包定义了数据库 schema 版本管理器，
// 用于跟踪和更新各存储表的 schema 版本。
// MemoryMetaManager 通过 SqlDbStore 操作 memory_meta 表，
// 实现版本记录的增删查功能。
//
// 文件目录：
//
//	migrator/
//	├── doc.go                     # 包文档
//	├── memory_meta_manager.go     # MemoryMetaManager schema 版本管理器
//	└── memory_meta_manager_test.go
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/migration/migrator/
//
// 核心类型/接口索引：
//
//	MemoryMetaManager — schema 版本管理器，基于 SqlDbStore 操作 memory_meta 表
package migrator
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/migration/migrator/...`
Expected: 编译成功

---

### Task 4: 创建 MemoryMetaManager 实现 + 测试

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/memory_meta_manager.go`
- Create: `internal/agentcore/memory/migration/migrator/memory_meta_manager_test.go`

- [ ] **Step 1: 编写 memory_meta_manager.go**

migrator 包通过 `SqlDbQuerier` 接口与 model 包解耦，避免循环依赖。`model.SqlDbStore` 隐式实现此接口。

```go
package migrator

import (
	"context"
)

// ──────────────────────────── 接口 ────────────────────────────

// SqlDbQuerier SqlDbStore 的最小接口，用于解耦 migrator 和 model 包。
// model.SqlDbStore 隐式实现此接口。
type SqlDbQuerier interface {
	// Write 插入一行数据
	Write(ctx context.Context, table string, data map[string]any) error
	// ConditionGet 条件查询
	ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error)
	// Exist 检查记录是否存在
	Exist(ctx context.Context, table string, conditions map[string]any) (bool, error)
	// Delete 条件删除
	Delete(ctx context.Context, table string, conditions map[string]any) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryMetaManager 内存元数据管理器，基于 SqlDbQuerier 操作 memory_meta 表。
//
// 对应 Python: openjiuwen/core/memory/migration/migrator/memory_meta_manager.py (MemoryMetaManager)
type MemoryMetaManager struct {
	// db 数据库查询接口
	db SqlDbQuerier
	// metaTable 元数据表名
	metaTable string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryMetaManager 创建 MemoryMetaManager 实例。
func NewMemoryMetaManager(db SqlDbQuerier) *MemoryMetaManager {
	return &MemoryMetaManager{
		db:        db,
		metaTable: "memory_meta",
	}
}

// Add 添加 schema 版本记录。
// tableName 或 schemaVersion 为空时静默返回 nil。
// 若记录已存在则跳过（幂等）。
//
// 对应 Python: MemoryMetaManager.add(table_name, schema_version)
func (m *MemoryMetaManager) Add(ctx context.Context, tableName string, schemaVersion string) error {
	if tableName == "" || schemaVersion == "" {
		return nil
	}
	data := map[string]any{
		"table_name":     tableName,
		"schema_version": schemaVersion,
	}
	exists, err := m.db.Exist(ctx, m.metaTable,
		map[string]any{"table_name": tableName, "schema_version": schemaVersion})
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return m.db.Write(ctx, m.metaTable, data)
}

// GetByTableName 按 table_name 查询 schema 版本记录。
//
// 对应 Python: MemoryMetaManager.get_by_table_name(table_name)
func (m *MemoryMetaManager) GetByTableName(ctx context.Context, tableName string) ([]map[string]any, error) {
	results, err := m.db.ConditionGet(ctx, m.metaTable,
		map[string]any{"table_name": []string{tableName}}, nil)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// DeleteByTableName 按 table_name 删除 schema 版本记录。
// 补齐 Python 中存在但 Go 之前缺失的方法。
//
// 对应 Python: MemoryMetaManager.delete_by_table_name(table_name)
func (m *MemoryMetaManager) DeleteByTableName(ctx context.Context, tableName string) error {
	return m.db.Delete(ctx, m.metaTable,
		map[string]any{"table_name": tableName})
}
```

- [ ] **Step 2: 编写 memory_meta_manager_test.go**

注意：migrator 包的测试使用 mock 实现 SqlDbQuerier 接口，避免循环依赖。

```go
package migrator

import (
	"context"
	"testing"
)

// mockSqlDbQuerier 用于测试的 SqlDbQuerier 模拟实现
type mockSqlDbQuerier struct {
	records []map[string]any
}

func newMockSqlDbQuerier() *mockSqlDbQuerier {
	return &mockSqlDbQuerier{records: make([]map[string]any, 0)}
}

func (m *mockSqlDbQuerier) Write(_ context.Context, _ string, data map[string]any) error {
	m.records = append(m.records, data)
	return nil
}

func (m *mockSqlDbQuerier) ConditionGet(_ context.Context, _ string, conditions map[string]any, _ []string) ([]map[string]any, error) {
	// 简化实现：只支持 table_name 条件
	tableNames, ok := conditions["table_name"]
	if !ok {
		return m.records, nil
	}
	names, ok := tableNames.([]string)
	if !ok {
		return m.records, nil
	}
	var result []map[string]any
	for _, r := range m.records {
		if tn, ok := r["table_name"].(string); ok {
			for _, n := range names {
				if tn == n {
					result = append(result, r)
					break
				}
			}
		}
	}
	return result, nil
}

func (m *mockSqlDbQuerier) Exist(_ context.Context, _ string, conditions map[string]any) (bool, error) {
	for _, r := range m.records {
		match := true
		for k, v := range conditions {
			if rv, ok := r[k]; !ok || rv != v {
				match = false
				break
			}
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockSqlDbQuerier) Delete(_ context.Context, _ string, conditions map[string]any) error {
	var remaining []map[string]any
	for _, r := range m.records {
		match := true
		for k, v := range conditions {
			if rv, ok := r[k]; !ok || rv != v {
				match = false
				break
			}
		}
		if !match {
			remaining = append(remaining, r)
		}
	}
	m.records = remaining
	return nil
}

// TestMemoryMetaManager_Add 添加版本记录
func TestMemoryMetaManager_Add(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	if err := mgr.Add(ctx, "user_message", "1"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	results, err := mgr.GetByTableName(ctx, "user_message")
	if err != nil {
		t.Fatalf("GetByTableName 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 条记录, got %d", len(results))
	}
}

// TestMemoryMetaManager_Add_幂等 验证重复添加不报错
func TestMemoryMetaManager_Add_幂等(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	if err := mgr.Add(ctx, "user_message", "1"); err != nil {
		t.Fatalf("第一次 Add 失败: %v", err)
	}
	if err := mgr.Add(ctx, "user_message", "1"); err != nil {
		t.Fatalf("第二次 Add 失败: %v", err)
	}

	results, _ := mgr.GetByTableName(ctx, "user_message")
	if len(results) != 1 {
		t.Errorf("幂等添加后应只有 1 条记录, got %d", len(results))
	}
}

// TestMemoryMetaManager_Add_空参数 验证空参数时静默返回
func TestMemoryMetaManager_Add_空参数(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	if err := mgr.Add(ctx, "", "1"); err != nil {
		t.Errorf("空 tableName 应静默返回, got error: %v", err)
	}
	if err := mgr.Add(ctx, "user_message", ""); err != nil {
		t.Errorf("空 schemaVersion 应静默返回, got error: %v", err)
	}
}

// TestMemoryMetaManager_GetByTableName_不存在 验证不存在的表返回空切片
func TestMemoryMetaManager_GetByTableName_不存在(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	results, err := mgr.GetByTableName(ctx, "nonexistent_table")
	if err != nil {
		t.Fatalf("GetByTableName 不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("不存在的表应返回 0 条记录, got %d", len(results))
	}
}

// TestMemoryMetaManager_DeleteByTableName 删除版本记录
func TestMemoryMetaManager_DeleteByTableName(t *testing.T) {
	q := newMockSqlDbQuerier()
	mgr := NewMemoryMetaManager(q)
	ctx := context.Background()

	_ = mgr.Add(ctx, "user_message", "1")
	_ = mgr.Add(ctx, "user_message", "2")

	if err := mgr.DeleteByTableName(ctx, "user_message"); err != nil {
		t.Fatalf("DeleteByTableName 失败: %v", err)
	}

	results, _ := mgr.GetByTableName(ctx, "user_message")
	if len(results) != 0 {
		t.Errorf("删除后应无记录, got %d", len(results))
	}
}
```

注意：测试文件中 `newTestSqlDbQuerier` 返回 `model.SqlDbStore`（值类型），`NewMemoryMetaManager` 接收 `SqlDbQuerier` 接口。需要确保 `model.SqlDbStore` 实现了 `SqlDbQuerier` 接口的所有方法。由于 `SqlDbQuerier` 定义在 migrator 包中，而 `model.SqlDbStore` 在 model 包中，这需要接口方法签名完全匹配。

但是，当前 `model.SqlDbStore` 的 `Delete` 方法签名为 `(ctx, table, conditions) error`，而 `SqlDbQuerier.Delete` 签名也是 `(ctx, table, conditions) error`——匹配。`Exist` 方法签名也匹配。所以 `*model.SqlDbStore` 隐式实现 `SqlDbQuerier` 接口。

测试中用 `&q` 传入指针即可。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/migration/migrator/... -v`
Expected: 所有测试 PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/migration/
git commit -m "feat(memory): 新增 MemoryMetaManager 独立迁移管理包

- MemoryMetaManager 通过 SqlDbQuerier 接口解耦，避免循环依赖
- 补齐 DeleteByTableName 方法（Python 有，Go 之前缺失）
- Add 方法支持幂等（记录已存在时跳过）
- 对应 Python: openjiuwen/core/memory/migration/migrator/memory_meta_manager.py"
```

---

### Task 5: 扩展 SqlDbStore — 新增 CreateBatch 和表缓存

**Files:**
- Modify: `internal/agentcore/memory/manage/model/sql_db_store.go`

- [ ] **Step 1: 在 SqlDbStore 结构体中添加表缓存字段**

在 `sql_db_store.go` 第 31-36 行，修改 SqlDbStore 结构体：

```go
type SqlDbStore struct {
	// dbStore 数据库存储抽象
	dbStore db.BaseDbStore
	// db GORM 数据库实例
	db *gorm.DB
	// tableCache 表列名缓存（表名 → 列名列表）
	tableCache sync.Map
}
```

在 import 中添加 `"sync"`。

- [ ] **Step 2: 添加 CreateBatch 方法**

在 `sql_db_store.go` 的导出函数区块末尾（`GetWithSortAndTimeRange` 之后）添加：

```go
// CreateBatch 批量插入多行数据到指定表。
// 使用 GORM Create 一次性批量 INSERT，而非循环调用 Write。
// rows 为空时直接返回 nil。
//
// 对应 Python: Go 新增（Python 无对应方法，add_messages 循环调用 write）
func (s *SqlDbStore) CreateBatch(ctx context.Context, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if err := s.db.Table(table).Create(rows).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("table_name", table).
			Int("row_count", len(rows)).
			Err(err).
			Msg("批量写入数据失败")
		return exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("create_batch failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}
```

- [ ] **Step 3: 添加 GetTable 和 InvalidateTableCache 方法**

```go
// GetTable 获取表的列名列表（带缓存）。
// 用于列存在性校验，避免重复查询数据库 schema。
//
// 对应 Python: SqlDbStore.get_table(table_name)
func (s *SqlDbStore) GetTable(ctx context.Context, tableName string) ([]string, error) {
	// 检查缓存
	if cached, ok := s.tableCache.Load(tableName); ok {
		return cached.([]string), nil
	}

	// 查询列信息
	columns, err := s.db.Table(tableName).Migrator().ColumnTypes(tableName)
	if err != nil {
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_table failed for table %s: %s", tableName, err.Error())),
		)
	}

	colNames := make([]string, 0, len(columns))
	for _, col := range columns {
		colNames = append(colNames, col.Name())
	}

	// 写入缓存
	s.tableCache.Store(tableName, colNames)
	return colNames, nil
}

// InvalidateTableCache 清除指定表的列名缓存。
// 下次 GetTable 调用会重新查询数据库 schema。
//
// 对应 Python: SqlDbStore.invalidate_table_cache(table_name)
func (s *SqlDbStore) InvalidateTableCache(tableName string) {
	s.tableCache.Delete(tableName)
}
```

- [ ] **Step 4: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/manage/model/...`
Expected: 编译成功

---

### Task 6: 扩展 SqlDbStore — 新增 BatchGet/Get/DeleteTable + ConditionGet/GetWithSort 校验 + 测试

**Files:**
- Modify: `internal/agentcore/memory/manage/model/sql_db_store.go`
- Modify: `internal/agentcore/memory/manage/model/sql_db_store_test.go`

- [ ] **Step 1: 添加 BatchGet 方法**

在 `sql_db_store.go` 导出函数区块添加：

```go
// BatchGet 多组 OR 条件查询。
// conditionsList 中每个 condition 之间用 OR 连接，
// 单个 condition 内部用 AND 连接。
//
// 对应 Python: SqlDbStore.batch_get(table, conditions_list)
func (s *SqlDbStore) BatchGet(ctx context.Context, table string, conditionsList []map[string]any) ([]map[string]any, error) {
	query := s.db.Table(table)

	if len(conditionsList) > 0 {
		var orConditions []string
		var orArgs []any
		for _, cond := range conditionsList {
			var andParts []string
			var andArgs []any
			for col, val := range cond {
				andParts = append(andParts, fmt.Sprintf("%s = ?", col))
				andArgs = append(andArgs, val)
			}
			if len(andParts) > 0 {
				orConditions = append(orConditions, fmt.Sprintf("(%s)", joinWithAnd(andParts)))
				orArgs = append(orArgs, andArgs...)
			}
		}
		if len(orConditions) > 0 {
			query = query.Where(fmt.Sprintf("(%s)", joinWithOr(orConditions)), orArgs...)
		}
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("批量条件查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("batch_get failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}
```

- [ ] **Step 2: 添加辅助函数 joinWithAnd 和 joinWithOr**

在非导出函数区块添加：

```go
// joinWithAnd 用 AND 连接字符串切片
func joinWithAnd(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += " AND " + parts[i]
	}
	return result
}

// joinWithOr 用 OR 连接字符串切片
func joinWithOr(parts []string) string {
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += " OR " + parts[i]
	}
	return result
}
```

- [ ] **Step 3: 添加 Get 方法**

```go
// Get 按条件查询单条记录（limit 1）。
// Python 硬编码 WHERE id = record_id，Go 改为通用 conditions 参数，
// 避免硬编码主键列名（不同表的主键不同）。
// columns 指定需要返回的列，为空时返回所有列。
//
// 对应 Python: SqlDbStore.get(table, record_id, columns)
func (s *SqlDbStore) Get(ctx context.Context, table string, conditions map[string]any, columns []string) (map[string]any, error) {
	query := s.db.Table(table)
	if len(columns) > 0 {
		query = query.Select(columns)
	}
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	query = query.Limit(1)

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("单条查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get failed for table %s: %s", table, err.Error())),
		)
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}
```

- [ ] **Step 4: 添加 DeleteTable 方法**

```go
// DeleteTable 删除整张表（DROP TABLE）。
//
// 对应 Python: SqlDbStore.delete_table(table_name)
func (s *SqlDbStore) DeleteTable(ctx context.Context, tableName string) error {
	if err := s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("table_name", tableName).
			Err(err).
			Msg("删除表失败")
		return exception.BuildError(exception.StatusStoreMessageDeleteExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("delete_table failed for %s: %s", tableName, err.Error())),
		)
	}
	// 清除缓存
	s.InvalidateTableCache(tableName)
	return nil
}
```

- [ ] **Step 5: 修改 ConditionGet 添加类型校验**

修改 `sql_db_store.go` 中的 `ConditionGet` 方法（第 74-97 行），在循环构建 IN 条件前添加类型校验：

```go
func (s *SqlDbStore) ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error) {
	query := s.db.Table(table)
	// 选择指定列
	if len(columns) > 0 {
		query = query.Select(columns)
	}
	// 构建 IN 条件，校验 values 必须为切片类型
	for col, val := range conditions {
		switch val.(type) {
		case []string, []any, []int, []int64, []float64:
			query = query.Where(fmt.Sprintf("%s IN ?", col), val)
		default:
			return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("condition_get: conditions[%q] must be a slice, got %T", col, val)),
			)
		}
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("条件查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("condition_get failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}
```

- [ ] **Step 6: 修改 GetWithSort 添加排序列校验**

修改 `sql_db_store.go` 中的 `GetWithSort` 方法（第 103-134 行），在排序前添加列存在性校验：

```go
func (s *SqlDbStore) GetWithSort(ctx context.Context, table string, filters map[string]any, sortBy string, order string, limit int) ([]map[string]any, error) {
	// 排序列校验
	if sortBy != "" {
		colNames, err := s.GetTable(ctx, table)
		if err != nil {
			return nil, err
		}
		found := false
		for _, name := range colNames {
			if name == sortBy {
				found = true
				break
			}
		}
		if !found {
			return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("sort column '%s' does not exist in table '%s'", sortBy, table)),
			)
		}
	}

	query := s.db.Table(table)
	// 等值过滤
	for col, val := range filters {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	// 排序
	if sortBy != "" {
		dir := "ASC"
		if order == "DESC" || order == "desc" {
			dir = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", sortBy, dir))
	}
	// 限制行数
	if limit > 0 {
		query = query.Limit(limit)
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("排序查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_with_sort failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}
```

- [ ] **Step 7: 补充 sql_db_store_test.go 新方法测试**

在现有测试文件中添加以下测试：

```go
// TestSqlDbStore_CreateBatch 批量插入
func TestSqlDbStore_CreateBatch(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	rows := make([]map[string]any, 3)
	for i := 0; i < 3; i++ {
		rows[i] = map[string]any{
			"message_id": fmt.Sprintf("msg_batch_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  fmt.Sprintf("2024-01-0%dT00:00:00Z", i+1),
		}
	}

	if err := store.CreateBatch(context.Background(), "user_message", rows); err != nil {
		t.Fatalf("CreateBatch 失败: %v", err)
	}

	count, _ := store.Count(context.Background(), "user_message", map[string]any{"user_id": "user1"})
	if count != 3 {
		t.Errorf("CreateBatch 后 Count = %d, want 3", count)
	}
}

// TestSqlDbStore_CreateBatch_空切片 验证空切片直接返回 nil
func TestSqlDbStore_CreateBatch_空切片(t *testing.T) {
	store, _ := newTestSqlDbStore(t)
	if err := store.CreateBatch(context.Background(), "user_message", []map[string]any{}); err != nil {
		t.Fatalf("空切片应返回 nil, got error: %v", err)
	}
}

// TestSqlDbStore_BatchGet 多组 OR 条件查询
func TestSqlDbStore_BatchGet(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 写入两条不同用户的消息
	store.Write(context.Background(), "user_message", map[string]any{
		"message_id": "msg_bg_1", "user_id": "user1", "scope_id": "scope1",
		"content": "hello1", "role": "user", "timestamp": "2024-01-01T00:00:00Z",
	})
	store.Write(context.Background(), "user_message", map[string]any{
		"message_id": "msg_bg_2", "user_id": "user2", "scope_id": "scope1",
		"content": "hello2", "role": "user", "timestamp": "2024-01-02T00:00:00Z",
	})

	results, err := store.BatchGet(context.Background(), "user_message", []map[string]any{
		{"user_id": "user1"},
		{"user_id": "user2"},
	})
	if err != nil {
		t.Fatalf("BatchGet 失败: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("BatchGet 返回 %d 条, 期望 2 条", len(results))
	}
}

// TestSqlDbStore_Get 按条件查询单条
func TestSqlDbStore_Get(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	store.Write(context.Background(), "user_message", map[string]any{
		"message_id": "msg_get_1", "user_id": "user1", "scope_id": "scope1",
		"content": "hello", "role": "user", "timestamp": "2024-01-01T00:00:00Z",
	})

	result, err := store.Get(context.Background(), "user_message",
		map[string]any{"message_id": "msg_get_1"}, nil)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if result == nil {
		t.Fatal("Get 应返回一条记录")
	}
}

// TestSqlDbStore_Get_不存在 验证不存在的记录返回 nil
func TestSqlDbStore_Get_不存在(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	result, err := store.Get(context.Background(), "user_message",
		map[string]any{"message_id": "nonexistent"}, nil)
	if err != nil {
		t.Fatalf("Get 不应报错: %v", err)
	}
	if result != nil {
		t.Error("不存在的记录应返回 nil")
	}
}

// TestSqlDbStore_DeleteTable 删除整表
func TestSqlDbStore_DeleteTable(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	if err := store.DeleteTable(context.Background(), "user_message"); err != nil {
		t.Fatalf("DeleteTable 失败: %v", err)
	}

	if gormDB.Migrator().HasTable("user_message") {
		t.Error("表应该已被删除")
	}
}

// TestSqlDbStore_ConditionGet_类型校验 验证 values 非切片时返回 error
func TestSqlDbStore_ConditionGet_类型校验(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	_, err := store.ConditionGet(context.Background(), "user_message",
		map[string]any{"message_id": "not_a_slice"}, nil)
	if err == nil {
		t.Error("values 非切片时应返回 error")
	}
}

// TestSqlDbStore_GetWithSort_排序列不存在 验证排序列不存在时返回 error
func TestSqlDbStore_GetWithSort_排序列不存在(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	_, err := store.GetWithSort(context.Background(), "user_message",
		map[string]any{"user_id": "user1"}, "nonexistent_column", "ASC", 10)
	if err == nil {
		t.Error("排序列不存在时应返回 error")
	}
}
```

- [ ] **Step 8: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/manage/model/... -v -run "SqlDbStore"`
Expected: 所有 SqlDbStore 相关测试 PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/memory/manage/model/sql_db_store.go internal/agentcore/memory/manage/model/sql_db_store_test.go
git commit -m "feat(memory): 扩展 SqlDbStore 依赖层

- 新增 CreateBatch 批量插入方法
- 新增 BatchGet 多组 OR 条件查询
- 新增 Get 按条件查询单条记录
- 新增 DeleteTable 整表删除
- 新增 GetTable/InvalidateTableCache 表列名缓存
- ConditionGet 添加 values 类型校验（必须为切片）
- GetWithSort 添加排序列存在性校验
- 对齐 Python: batch_get, delete_table, get_table, invalidate_table_cache"
```

---

### Task 7: 重构 SqlMessageStore — 核心逻辑

**Files:**
- Modify: `internal/agentcore/memory/manage/model/sql_message_store.go`

此任务将 sql_message_store.go 完全重写，改动量大，需要一次性替换整个文件。

- [ ] **Step 1: 重写 sql_message_store.go**

关键改动：
1. 移除内嵌 `encodeContent/decodeContent`，委托给 `codec.AesStorageCodec`
2. 移除内嵌 `memoryMetaManager`，改用 `migrator.MemoryMetaManager`
3. 构造函数返回 `(*SqlMessageStore, error)`
4. `AddMessages` 改用 `CreateBatch`
5. Content 序列化改用 `json.Marshal/Unmarshal`
6. `rowToMessageAndMeta` 安全类型断言
7. `generateMessageID` 改用 `json.Marshal` 序列化 Content

```go
package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/memory/migration/migrator"
	storedb "github.com/uapclaw/uap-claw-go/internal/agentcore/store/db"
	"github.com/uapclaw/uap-claw-go/internal/common/exception"
	"github.com/uapclaw/uap-claw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultTableName 默认消息表名
	DefaultTableName = "user_message"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlMessageStore BaseMessageStore 的 SQL 实现。
//
// 基于 SqlDbStore 执行数据库操作，使用 AesStorageCodec 加密消息内容。
// 严格模式：key 为空时 passthrough 不加密，key 非空时加解密失败返回 error。
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_message_store.py (SqlMessageStore)
type SqlMessageStore struct {
	// codec AES 存储编解码器
	codec *codec.AesStorageCodec
	// sqlDbStore 通用 SQL CRUD 层
	sqlDbStore *SqlDbStore
	// tableName 消息表名
	tableName string
	// metaMgr schema 版本管理器
	metaMgr *migrator.MemoryMetaManager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSqlMessageStore 创建 SqlMessageStore 实例。
// cryptoKey 为空时 passthrough 模式，非空时必须为 32 字节。
//
// 对应 Python: SqlMessageStore.__init__(crypto_key, sql_db_store, table_name)
func NewSqlMessageStore(cryptoKey []byte, sqlDbStore *SqlDbStore, tableName string) (*SqlMessageStore, error) {
	if tableName == "" {
		tableName = DefaultTableName
	}

	c, err := codec.NewAesStorageCodec(cryptoKey)
	if err != nil {
		return nil, exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("create codec failed: %s", err.Error())),
		)
	}

	return &SqlMessageStore{
		codec:      c,
		sqlDbStore: sqlDbStore,
		tableName:  tableName,
		metaMgr:    migrator.NewMemoryMetaManager(sqlDbStore),
	}, nil
}

// AddMessage 添加单条消息，返回 message_id。
//
// 对应 Python: SqlMessageStore.add_message(message_add)
func (s *SqlMessageStore) AddMessage(ctx context.Context, messageAdd *storedb.MessageAdd) (string, error) {
	message := messageAdd.Message
	timestamp := messageAdd.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// 序列化并生成消息 ID
	contentStr, err := marshalContent(message.Content)
	if err != nil {
		return "", err
	}
	messageID := generateMessageID(contentStr, timestamp)

	// 加密内容
	encrypted, err := s.codec.Encode(contentStr)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Str("method", "AddMessage").
			Err(err).
			Msg("加密消息内容失败")
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("encode content failed: %s", err.Error())),
		)
	}

	// 组装数据行
	data := map[string]any{
		"message_id": messageID,
		"user_id":    messageAdd.UserID,
		"session_id": messageAdd.SessionID,
		"scope_id":   messageAdd.ScopeID,
		"role":       message.Role.String(),
		"content":    encrypted,
		"timestamp":  timestamp.Format(time.RFC3339),
	}

	if err := s.sqlDbStore.Write(ctx, s.tableName, data); err != nil {
		return "", err
	}

	return messageID, nil
}

// AddMessages 批量添加消息，返回 ID 列表。
// 使用 CreateBatch 一次 GORM Create 插入所有行。
//
// 对应 Python: SqlMessageStore.add_messages(message_adds)
func (s *SqlMessageStore) AddMessages(ctx context.Context, messageAdds []*storedb.MessageAdd) ([]string, error) {
	messageIDs := make([]string, 0, len(messageAdds))
	rows := make([]map[string]any, 0, len(messageAdds))

	for _, messageAdd := range messageAdds {
		message := messageAdd.Message
		timestamp := messageAdd.Timestamp
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		// 序列化并生成消息 ID
		contentStr, err := marshalContent(message.Content)
		if err != nil {
			return nil, err
		}
		messageID := generateMessageID(contentStr, timestamp)

		// 加密内容
		encrypted, err := s.codec.Encode(contentStr)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "STORE_MESSAGE_ERROR").
				Str("method", "AddMessages").
				Str("message_id", messageID).
				Err(err).
				Msg("加密消息内容失败")
			return nil, exception.BuildError(exception.StatusStoreMessageAddExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("encode content failed: %s", err.Error())),
			)
		}

		data := map[string]any{
			"message_id": messageID,
			"user_id":    messageAdd.UserID,
			"session_id": messageAdd.SessionID,
			"scope_id":   messageAdd.ScopeID,
			"role":       message.Role.String(),
			"content":    encrypted,
			"timestamp":  timestamp.Format(time.RFC3339),
		}

		rows = append(rows, data)
		messageIDs = append(messageIDs, messageID)
	}

	// 批量写入
	if err := s.sqlDbStore.CreateBatch(ctx, s.tableName, rows); err != nil {
		return nil, err
	}

	return messageIDs, nil
}

// GetMessageByID 按 ID 获取消息，不存在时返回错误。
//
// 对应 Python: SqlMessageStore.get_message_by_id(message_id)
func (s *SqlMessageStore) GetMessageByID(ctx context.Context, messageID string) (*schema.BaseMessage, *storedb.MessageMetadata, error) {
	results, err := s.sqlDbStore.ConditionGet(ctx, s.tableName,
		map[string]any{"message_id": []string{messageID}}, nil)
	if err != nil {
		return nil, nil, err
	}

	if len(results) == 0 {
		return nil, nil, exception.BuildError(exception.StatusStoreMessageNotFound,
			exception.WithParam("message_id", messageID),
		)
	}

	return s.rowToMessageAndMeta(results[0])
}

// GetMessages 按条件过滤查询消息。
// 实现 StartTime/EndTime 范围查询（Python 定义了但未实现）。
//
// 对应 Python: SqlMessageStore.get_messages(message_filter, limit, order_by, order_direction)
func (s *SqlMessageStore) GetMessages(ctx context.Context, filter *storedb.MessageFilter, limit int, orderBy string, orderDirection string) ([]*storedb.MessageAndMeta, error) {
	if limit <= 0 {
		limit = 10
	}
	if orderBy == "" {
		orderBy = "timestamp"
	}

	// 构建等值过滤条件
	filters := map[string]any{}
	if filter.UserID != "" {
		filters["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		filters["scope_id"] = filter.ScopeID
	}
	if filter.SessionID != "" {
		filters["session_id"] = filter.SessionID
	}

	// 使用带时间范围查询的方法
	rows, err := s.sqlDbStore.GetWithSortAndTimeRange(ctx, s.tableName,
		filters, orderBy, orderDirection, limit, filter.StartTime, filter.EndTime)
	if err != nil {
		return nil, err
	}

	result := make([]*storedb.MessageAndMeta, 0, len(rows))
	for _, row := range rows {
		msg, meta, err := s.rowToMessageAndMeta(row)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "STORE_MESSAGE_ERROR").
				Str("method", "GetMessages").
				Err(err).
				Msg("解析消息行失败，跳过")
			continue
		}
		result = append(result, &storedb.MessageAndMeta{
			Message:  msg,
			Metadata: meta,
		})
	}

	return result, nil
}

// UpdateMessage 更新消息内容。
//
// 对应 Python: SqlMessageStore.update_message(message_id, content)
func (s *SqlMessageStore) UpdateMessage(ctx context.Context, messageID string, content schema.MessageContent) error {
	contentStr, err := marshalContent(content)
	if err != nil {
		return err
	}

	encrypted, err := s.codec.Encode(contentStr)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Str("method", "UpdateMessage").
			Str("message_id", messageID).
			Err(err).
			Msg("加密消息内容失败")
		return exception.BuildError(exception.StatusStoreMessageUpdateExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("encode content failed: %s", err.Error())),
		)
	}

	return s.sqlDbStore.Update(ctx, s.tableName,
		map[string]any{"message_id": messageID},
		map[string]any{"content": encrypted})
}

// DeleteMessageByID 按 ID 删除单条消息。
//
// 对应 Python: SqlMessageStore.delete_message_by_id(message_id)
func (s *SqlMessageStore) DeleteMessageByID(ctx context.Context, messageID string) error {
	return s.sqlDbStore.Delete(ctx, s.tableName,
		map[string]any{"message_id": messageID})
}

// DeleteMessages 按条件删除消息，返回删除数量。
//
// 对应 Python: SqlMessageStore.delete_messages(message_filter)
func (s *SqlMessageStore) DeleteMessages(ctx context.Context, filter *storedb.MessageFilter) (int64, error) {
	// 先获取数量
	count, err := s.CountMessages(ctx, filter)
	if err != nil {
		return 0, err
	}

	// 构建删除条件
	conditions := map[string]any{}
	if filter.UserID != "" {
		conditions["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		conditions["scope_id"] = filter.ScopeID
	}
	if filter.SessionID != "" {
		conditions["session_id"] = filter.SessionID
	}

	if err := s.sqlDbStore.Delete(ctx, s.tableName, conditions); err != nil {
		return 0, err
	}

	return count, nil
}

// CountMessages 统计匹配消息数量。
// 使用 SQL COUNT，而非取回全部数据后 len()。
//
// 对应 Python: SqlMessageStore.count_messages(message_filter)
func (s *SqlMessageStore) CountMessages(ctx context.Context, filter *storedb.MessageFilter) (int64, error) {
	conditions := map[string]any{}
	if filter.UserID != "" {
		conditions["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		conditions["scope_id"] = filter.ScopeID
	}
	if filter.SessionID != "" {
		conditions["session_id"] = filter.SessionID
	}

	return s.sqlDbStore.Count(ctx, s.tableName, conditions)
}

// GetSchemaVersion 获取当前 schema 版本号。
//
// 对应 Python: SqlMessageStore.get_schema_version()
func (s *SqlMessageStore) GetSchemaVersion(ctx context.Context) (int32, error) {
	results, err := s.metaMgr.GetByTableName(ctx, s.tableName)
	if err != nil {
		return 0, err
	}
	if len(results) > 0 {
		versionStr, ok := results[0]["schema_version"]
		if ok {
			var version int32
			if _, err := fmt.Sscanf(fmt.Sprintf("%v", versionStr), "%d", &version); err == nil {
				return version, nil
			}
		}
	}
	return 0, nil
}

// SetSchemaVersion 设置 schema 版本号。
//
// 对应 Python: SqlMessageStore.set_schema_version(version)
func (s *SqlMessageStore) SetSchemaVersion(ctx context.Context, version int32) error {
	return s.metaMgr.Add(ctx, s.tableName, fmt.Sprintf("%d", version))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateMessageID 基于 content + timestamp 生成消息 ID。
// 格式: msg_{sha256(content_json+timestamp)[:16]}_{timestamp_ms}
//
// 对应 Python: SqlMessageStore._generate_message_id(message, timestamp)
func generateMessageID(content string, timestamp time.Time) string {
	messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s%v", content, timestamp)))
	return fmt.Sprintf("msg_%x_%d", messageHash[:8], timestamp.UnixMilli())
}

// marshalContent 将 MessageContent 序列化为 JSON 字符串。
// 纯文本 → "hello"（JSON string），多模态 → [{"type":"text",...}]（JSON array）。
func marshalContent(content schema.MessageContent) (string, error) {
	bytes, err := json.Marshal(content)
	if err != nil {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("marshal content failed: %s", err.Error())),
		)
	}
	return string(bytes), nil
}

// unmarshalContent 将 JSON 字符串反序列化为 MessageContent。
// 兼容旧数据：纯文本字符串也可被 MessageContent.UnmarshalJSON 处理。
func unmarshalContent(data string) (schema.MessageContent, error) {
	var content schema.MessageContent
	if err := json.Unmarshal([]byte(data), &content); err != nil {
		return schema.MessageContent{}, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("unmarshal content failed: %s", err.Error())),
		)
	}
	return content, nil
}

// rowToMessageAndMeta 将数据库行转换为 BaseMessage 和 MessageMetadata。
// 使用安全类型断言，断言失败时返回 error。
func (s *SqlMessageStore) rowToMessageAndMeta(row map[string]any) (*schema.BaseMessage, *storedb.MessageMetadata, error) {
	// 安全类型断言辅助
	getStr := func(key string) (string, error) {
		v, ok := row[key]
		if !ok {
			return "", fmt.Errorf("行数据缺少字段 %q", key)
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("字段 %q 类型错误: 期望 string, 实际 %T", key, v)
		}
		return s, nil
	}

	// 解密 content
	contentStr, err := getStr("content")
	if err != nil {
		return nil, nil, err
	}
	decrypted, err := s.codec.Decode(contentStr)
	if err != nil {
		return nil, nil, err
	}

	// 反序列化 content
	content, err := unmarshalContent(decrypted)
	if err != nil {
		return nil, nil, err
	}

	// 解析 role
	roleStr, err := getStr("role")
	if err != nil {
		return nil, nil, err
	}
	role := roleTypeFromString(roleStr)

	// 构造 BaseMessage
	msg := &schema.BaseMessage{
		Role:    role,
		Content: content,
	}

	// 解析 timestamp
	timestampStr, err := getStr("timestamp")
	if err != nil {
		return nil, nil, err
	}
	timestamp, _ := time.Parse(time.RFC3339, timestampStr)

	// 解析其他字段
	messageID, err := getStr("message_id")
	if err != nil {
		return nil, nil, err
	}
	userID, err := getStr("user_id")
	if err != nil {
		return nil, nil, err
	}
	scopeID, err := getStr("scope_id")
	if err != nil {
		return nil, nil, err
	}
	sessionID, _ := getStr("session_id") // session_id 允许缺失

	meta := &storedb.MessageMetadata{
		MessageID:   messageID,
		UserID:      userID,
		ScopeID:     scopeID,
		SessionID:   sessionID,
		Timestamp:   timestamp,
		MessageType: roleStr,
	}

	return msg, meta, nil
}

// roleTypeFromString 从字符串解析 RoleType
func roleTypeFromString(s string) schema.RoleType {
	switch s {
	case "system":
		return schema.RoleTypeSystem
	case "user":
		return schema.RoleTypeUser
	case "assistant":
		return schema.RoleTypeAssistant
	case "tool":
		return schema.RoleTypeTool
	default:
		return schema.RoleTypeUser
	}
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/manage/model/...`
Expected: 编译成功

---

### Task 8: 重写 SqlMessageStore 测试

**Files:**
- Modify: `internal/agentcore/memory/manage/model/sql_message_store_test.go`

- [ ] **Step 1: 修改辅助函数适配构造函数变化**

将 `newTestSqlMessageStore` 的返回值适配 `NewSqlMessageStore` 的新签名：

```go
func newTestSqlMessageStore(t *testing.T) (*SqlMessageStore, *SqlDbStore, *gorm.DB) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(nil, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}
	return store, sqlDbStore, gormDB
}
```

- [ ] **Step 2: 添加构造函数 key 校验测试和多模态测试**

在测试文件中添加：

```go
// TestNewSqlMessageStore_key长度错误 验证非 32 字节 key 返回 error
func TestNewSqlMessageStore_key长度错误(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)

	_, err := NewSqlMessageStore([]byte{1, 2, 3}, sqlDbStore, "")
	if err == nil {
		t.Error("非 32 字节 key 应返回 error")
	}
}

// TestSqlMessageStore_多模态消息存储 验证多模态消息写入读取
func TestSqlMessageStore_多模态消息存储(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(key, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}

	// 创建多模态消息
	textPart := schema.ContentPart{Type: "text", Text: "hello"}
	imagePart := schema.ContentPart{Type: "image_url", ImageURL: &schema.ImageURL{URL: "https://example.com/img.png"}}
	content := schema.NewMultiModalContent(textPart, imagePart)
	msg := &schema.BaseMessage{Role: schema.RoleTypeUser, Content: content}

	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: time.Now(),
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}

	if gotMsg.Content.IsText() {
		t.Error("多模态消息不应是纯文本")
	}
	parts := gotMsg.Content.Parts()
	if len(parts) != 2 {
		t.Errorf("多模态消息应有 2 个分片, got %d", len(parts))
	}
}

// TestSqlMessageStore_多模态消息更新 验证 UpdateMessage 更新多模态内容
func TestSqlMessageStore_多模态消息更新(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "old_text")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		Timestamp: ts,
	}
	messageID, _ := store.AddMessage(context.Background(), add)

	// 更新为多模态内容
	textPart := schema.ContentPart{Type: "text", Text: "new_text"}
	newContent := schema.NewMultiModalContent(textPart)
	if err := store.UpdateMessage(context.Background(), messageID, newContent); err != nil {
		t.Fatalf("UpdateMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}
	if gotMsg.Content.IsText() {
		t.Error("更新后应为多模态内容")
	}
}
```

- [ ] **Step 3: 适配加解密往返测试**

修改 `TestSqlMessageStore_加解密往返`：

```go
func TestSqlMessageStore_加解密往返(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(key, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "secret message")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: ts,
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}
	if gotMsg.Content.Text() != "secret message" {
		t.Errorf("Content = %q, want %q (解密后应还原原文)", gotMsg.Content.Text(), "secret message")
	}
}
```

- [ ] **Step 4: 适配 DefaultTableName 测试**

修改 `TestSqlMessageStore_DefaultTableName`：

```go
func TestSqlMessageStore_DefaultTableName(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(nil, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}
	if store.tableName != "user_message" {
		t.Errorf("默认表名 = %q, want %q", store.tableName, "user_message")
	}
}
```

- [ ] **Step 5: 运行全部 model 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/manage/model/... -v`
Expected: 所有测试 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/manage/model/sql_message_store.go internal/agentcore/memory/manage/model/sql_message_store_test.go
git commit -m "refactor(memory): 重构 SqlMessageStore 核心逻辑

- 委托 AesStorageCodec 替代内嵌 encodeContent/decodeContent
- 委托 MemoryMetaManager 替代内嵌 memoryMetaManager
- 构造函数返回 (*SqlMessageStore, error)，校验 key 长度
- AddMessages 改用 CreateBatch 真正批量写入
- Content 序列化改用 json.Marshal/Unmarshal 支持多模态
- rowToMessageAndMeta 改用安全类型断言
- generateMessageID 改用 json.Marshal 序列化 Content"
```

---

### Task 9: 更新 doc.go 和清理

**Files:**
- Modify: `internal/agentcore/memory/manage/model/doc.go`

- [ ] **Step 1: 更新 model/doc.go**

```go
// Package model 提供记忆系统的数据模型和数据库操作。
//
// 本包定义了消息存储相关的数据库模型（UserMessage）、
// 通用 SQL CRUD 层（SqlDbStore）、消息存储实现（SqlMessageStore）
// 和消息管理器（MessageManager）。
// Schema 版本管理已迁移到 migrator 包，加解密编解码已迁移到 codec 包。
//
// 文件目录：
//
//	model/
//	├── doc.go                 # 包文档
//	├── db_model.go            # 数据库模型（UserMessage、ScopeUserMapping、MemoryMeta）
//	├── sql_db_store.go        # SqlDbStore 通用 SQL CRUD 层
//	├── sql_message_store.go   # SqlMessageStore 消息存储实现
//	└── message_manager.go     # MessageManager 消息管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/mem_model/
//
// 关联包：
//
//	memory/codec/              — AesStorageCodec 存储编解码器
//	memory/migration/migrator/ — MemoryMetaManager schema 版本管理器
//
// 核心类型/接口索引：
//
//	UserMessage      — 用户消息表 GORM 模型
//	ScopeUserMapping — 作用域用户映射表 GORM 模型
//	MemoryMeta       — 记忆元数据表 GORM 模型
//	SqlDbStore       — 通用 SQL CRUD 层，封装 GORM 通用操作
//	SqlMessageStore  — BaseMessageStore 的 SQL 实现
//	MessageManager   — 消息管理器，BaseMessageStore 的上层封装
package model
```

- [ ] **Step 2: 运行全量测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/... -v`
Expected: 所有测试 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/memory/...`
Expected: 所有包覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/manage/model/doc.go
git commit -m "docs(memory): 更新 model/doc.go 包文档

- 添加关联包说明（codec、migrator）
- 文件目录保持不变（memoryMetaManager 已迁移，原文件已删除）"
```

---

### Task 10: 最终验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./... -count=1`
Expected: 所有测试 PASS

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/memory/...`
Expected: 各包覆盖率 ≥ 85%
