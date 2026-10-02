# 7.22+7.23 迁移操作注册表 + 迁移器 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整对齐 Python 7.22+7.23 的迁移系统，包括 5 个 Migrator、run_migrations 编排、4 个 VectorStore UpdateSchema 回填、共享工具函数和接口变更。

**Architecture:** 迁移系统采用声明+执行两层架构：operation 包定义 Operation DTO + OperationRegistry（7.21 已完成），migrator 包定义 5 个 Migrator 执行器消费 Operation，run_migrations 做顶层编排。4 个 VectorStore 的 UpdateSchema 回填使用 ComputeNewSchema + BuildTransformFunc 共享工具，各后端按不同策略（rename/double-copy）执行迁移。

**Tech Stack:** Go 1.22+ / GORM Migrator（替代 Python Alembic）/ 项目现有 foundation 层接口

**Design Doc:** `docs/superpowers/specs/2027-09-03-migration-migrator-7.22-7.23-design.md`

---

## 任务依赖图

```
Task 1 (接口变更) ──────────────────────────────────────┐
                                                          │
Task 2 (schema_utils) ── 依赖 Task 1 ──────────────────┤
                                                          │
Task 3 (KVMigrator) ──────────────────────────────────┤
Task 4 (SQLMigrator) ─────────────────────────────────┤
Task 5 (VectorMigrator) ── 依赖 Task 1, Task 2 ──────┤
Task 6 (MessageMigrator) ─────────────────────────────┤
Task 7 (IndexVersionMigrator) ────────────────────────┤
                                                          │
Task 8 (run_migrations) ── 依赖 Task 3-7 ─────────────┤
                                                          │
Task 9 (Milvus UpdateSchema) ── 依赖 Task 1, Task 2 ──┤
Task 10 (Chroma UpdateSchema) ── 依赖 Task 1, Task 2 ─┤
Task 11 (Gauss UpdateSchema) ── 依赖 Task 1, Task 2 ──┤
Task 12 (ES UpdateSchema) ── 依赖 Task 1, Task 2 ─────┤
                                                          │
Task 13 (doc.go 更新 + IMPLEMENTATION_PLAN 回填) ───────┘
```

---

### Task 1: 接口变更 — UpdateSchema 签名 + RenameCollection

**Files:**
- Modify: `internal/agentcore/foundation/store/vector/base.go`
- Modify: `internal/agentcore/foundation/store/vector/base_test.go`
- Modify: `internal/agentcore/foundation/store/vector/milvus.go`
- Modify: `internal/agentcore/foundation/store/vector/milvus_test.go`
- Modify: `internal/agentcore/foundation/store/vector/chroma.go`
- Modify: `internal/agentcore/foundation/store/vector/chroma_test.go`
- Modify: `internal/agentcore/foundation/store/vector/gauss.go`
- Modify: `internal/agentcore/foundation/store/vector/gauss_test.go`
- Modify: `internal/agentcore/foundation/store/vector/es.go`
- Modify: `internal/agentcore/foundation/store/vector/es_test.go`
- Modify: `internal/agentcore/foundation/store/index/simple_test.go`
- Modify: `internal/agentcore/memory/manage/mem_model/semantic_store_test.go`

- [ ] **Step 1: 修改 BaseVectorStore 接口**

在 `base.go` 中：
1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"`
2. 修改 `UpdateSchema` 签名：`operations []any` → `operations []operation.Operation`
3. 新增 `RenameCollection` 接口方法：
```go
// RenameCollection 重命名集合。
// 不支持的后端返回 StatusStoreVectorNotSupported 错误。
//
// Python: BaseVectorStore.rename_collection(old_name, new_name)
RenameCollection(ctx context.Context, oldName string, newName string, opts ...Option) error
```
4. 删除 `UpdateSchema` 注释中的 `⤵️ 预留` 标记

- [ ] **Step 2: 更新 4 个 VectorStore 实现的 UpdateSchema 签名**

在 `milvus.go`、`chroma.go`、`gauss.go`、`es.go` 中：
1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"`
2. 修改 UpdateSchema 方法签名：`operations []any` → `operations []operation.Operation`
3. 更新方法体：将 `return exception.BuildError(...)` 改为签名变更后的 stub（内部逻辑在 Task 9-12 回填）
4. 删除 `⤵️ 预留` 和 `TODO(#回填)` 注释

- [ ] **Step 3: 新增 RenameCollection 实现**

在 `milvus.go` 中：
```go
// RenameCollection 重命名集合。
// 使用 Milvus SDK 的 RenameCollection API。
//
// Python: MilvusVectorStore.rename_collection(old_name, new_name)
func (s *MilvusVectorStore) RenameCollection(ctx context.Context, oldName string, newName string, _ ...Option) error {
	client, err := s.getClient(ctx)
	if err != nil {
		return err
	}
	return client.RenameCollection(ctx, oldName, newName)
}
```

在 `chroma.go` 中：
```go
// RenameCollection ChromaDB 不支持重命名集合。
//
// Python: ChromaVectorStore.rename_collection → 不支持
func (s *ChromaVectorStore) RenameCollection(_ context.Context, _ string, _ string, _ ...Option) error {
	return exception.BuildError(exception.StatusStoreVectorNotSupported,
		exception.WithParam("error_msg", "ChromaDB does not support rename_collection"),
	)
}
```

在 `gauss.go` 中：
```go
// RenameCollection 使用 ALTER TABLE RENAME 重命名集合（PostgreSQL 兼容）。
//
// Python: GaussVectorStore.rename_collection(old_name, new_name)
func (s *GaussVectorStore) RenameCollection(ctx context.Context, oldName string, newName string, _ ...Option) error {
	return s.db.WithContext(ctx).Exec(
		"ALTER TABLE ? RENAME TO ?", clause.Column{Name: oldName}, clause.Column{Name: newName},
	).Error
}
```

在 `es.go` 中：
```go
// RenameCollection ES 不支持重命名索引。
//
// Python: ESVectorStore.rename_collection → 不支持
func (s *ESVectorStore) RenameCollection(_ context.Context, _ string, _ string, _ ...Option) error {
	return exception.BuildError(exception.StatusStoreVectorNotSupported,
		exception.WithParam("error_msg", "Elasticsearch does not support rename_collection"),
	)
}
```

- [ ] **Step 4: 更新所有 fake/mock 的 UpdateSchema 签名 + 新增 RenameCollection**

在 `base_test.go` 的 `fakeVectorStore` 中：
1. `UpdateSchema` 签名改为 `operations []operation.Operation`
2. 新增 `RenameCollection` 方法（返回 nil）

在 `index/simple_test.go` 和 `memory/manage/mem_model/semantic_store_test.go` 中的 fake 同样更新。

- [ ] **Step 5: 更新现有测试**

1. `milvus_test.go`：`TestMilvusVectorStore_UpdateSchema_预留` 更新签名
2. `chroma_test.go`：`TestChromaVectorStore_UpdateSchema_预留` 更新签名
3. `gauss_test.go`：`TestGaussVectorStore_UpdateSchema_未实现` 更新签名
4. `es_test.go`：`TestESVectorStore_UpdateSchema_未实现` 更新签名
5. 新增 `TestMilvusVectorStore_RenameCollection`、`TestChromaVectorStore_RenameCollection_不支持`、`TestGaussVectorStore_RenameCollection`、`TestESVectorStore_RenameCollection_不支持`

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/...`
Expected: 编译通过，无错误

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "feat: BaseVectorStore 接口变更 — UpdateSchema []any→[]operation.Operation + RenameCollection"
```

---

### Task 2: 共享工具函数 — ComputeNewSchema + BuildTransformFunc

**Files:**
- Create: `internal/agentcore/foundation/store/vector/schema_utils.go`
- Create: `internal/agentcore/foundation/store/vector/schema_utils_test.go`

- [ ] **Step 1: 编写 schema_utils.go 失败测试**

在 `schema_utils_test.go` 中编写以下测试（先只写测试文件，schema_utils.go 为空）：

```go
package vector

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// TestComputeNewSchema_AddScalarField 测试添加标量字段
func TestComputeNewSchema_AddScalarField(t *testing.T) {
	oldSchema := NewCollectionSchema()
	_ = oldSchema.AddField(NewFieldSchema("id", VectorDataTypeINT64, true, false, 0, 0, VectorDataType(0), 0, "", nil))

	op := &operation.AddScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		DataType:      "vector_summary",
		FieldName:     "new_field",
		FieldType:     "string",
		DefaultValue:  "default_val",
	}

	newSchema, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	// 验证 new_schema 有 id + new_field 两个字段
	fields := newSchema.GetFields()
	if len(fields) != 2 {
		t.Errorf("期望 2 个字段, 实际 %d", len(fields))
	}
	found := false
	for _, f := range fields {
		if f.Name == "new_field" {
			found = true
			if f.DefaultValue != "default_val" {
				t.Errorf("DefaultValue = %v, want default_val", f.DefaultValue)
			}
		}
	}
	if !found {
		t.Error("未找到 new_field")
	}
}

// TestComputeNewSchema_RenameScalarField 测试重命名标量字段
func TestComputeNewSchema_RenameScalarField(t *testing.T) {
	oldSchema := NewCollectionSchema()
	_ = oldSchema.AddField(NewFieldSchema("old_name", VectorDataTypeINT64, false, false, 0, 0, VectorDataType(0), 0, "", nil))

	op := &operation.RenameScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		DataType:      "vector_summary",
		OldFieldName:  "old_name",
		NewFieldName:  "new_name",
	}

	newSchema, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	fields := newSchema.GetFields()
	for _, f := range fields {
		if f.Name == "old_name" {
			t.Error("old_name 不应存在")
		}
		if f.Name == "new_name" {
			return // 成功
		}
	}
	t.Error("未找到 new_name")
}

// TestComputeNewSchema_RenameField_OldNotExist 测试重命名不存在的字段
func TestComputeNewSchema_RenameField_OldNotExist(t *testing.T) {
	oldSchema := NewCollectionSchema()
	op := &operation.RenameScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		OldFieldName:  "nonexist",
		NewFieldName:  "new_name",
	}
	_, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err == nil {
		t.Error("旧字段不存在时应返回错误")
	}
}

// TestComputeNewSchema_UpdateFieldType 测试更新字段类型
func TestComputeNewSchema_UpdateFieldType(t *testing.T) {
	oldSchema := NewCollectionSchema()
	_ = oldSchema.AddField(NewFieldSchema("score", VectorDataTypeINT64, false, false, 0, 0, VectorDataType(0), 0, "", nil))

	op := &operation.UpdateScalarFieldTypeOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "score",
		NewFieldType:  "float",
	}

	newSchema, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	fields := newSchema.GetFields()
	for _, f := range fields {
		if f.Name == "score" && f.DType != VectorDataTypeFLOAT {
			t.Errorf("score DType = %v, want FLOAT", f.DType)
		}
	}
}

// TestComputeNewSchema_UpdateFieldType_VectorField 测试更新向量字段类型应报错
func TestComputeNewSchema_UpdateFieldType_VectorField(t *testing.T) {
	oldSchema := NewCollectionSchema()
	_ = oldSchema.AddField(NewFieldSchema("embedding", VectorDataTypeFLOAT_VECTOR, false, false, 0, 128, VectorDataType(0), 0, "", nil))

	op := &operation.UpdateScalarFieldTypeOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "embedding",
		NewFieldType:  "int",
	}
	_, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err == nil {
		t.Error("更新向量字段类型应返回错误")
	}
}

// TestComputeNewSchema_UpdateVectorDim 测试更新向量维度
func TestComputeNewSchema_UpdateVectorDim(t *testing.T) {
	oldSchema := NewCollectionSchema()
	_ = oldSchema.AddField(NewFieldSchema("embedding", VectorDataTypeFLOAT_VECTOR, false, false, 0, 128, VectorDataType(0), 0, "", nil))

	op := &operation.UpdateEmbeddingDimensionOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "embedding",
		NewDimension:  256,
	}

	newSchema, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	fields := newSchema.GetFields()
	for _, f := range fields {
		if f.Name == "embedding" && f.DType == VectorDataTypeFLOAT_VECTOR && f.Dim != 256 {
			t.Errorf("embedding Dim = %d, want 256", f.Dim)
		}
	}
}

// TestBuildTransformFunc_AddField 测试 AddField 变换
func TestBuildTransformFunc_AddField(t *testing.T) {
	op := &operation.AddScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "new_field",
		FieldType:     "string",
		DefaultValue:  "hello",
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"id": 1}
	result := transformFunc(doc)
	if result["new_field"] != "hello" {
		t.Errorf("new_field = %v, want hello", result["new_field"])
	}
}

// TestBuildTransformFunc_RenameField 测试 RenameField 变换
func TestBuildTransformFunc_RenameField(t *testing.T) {
	op := &operation.RenameScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		OldFieldName:  "old",
		NewFieldName:  "new",
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"old": "value"}
	result := transformFunc(doc)
	if _, exists := result["old"]; exists {
		t.Error("old 字段不应存在")
	}
	if result["new"] != "value" {
		t.Errorf("new = %v, want value", result["new"])
	}
}

// TestBuildTransformFunc_UpdateEmbeddingDim_ZeroVector 测试无 RecomputeEmbeddingFunc 时用零向量
func TestBuildTransformFunc_UpdateEmbeddingDim_ZeroVector(t *testing.T) {
	op := &operation.UpdateEmbeddingDimensionOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "embedding",
		NewDimension:  3,
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"embedding": []float64{1.0, 2.0}}
	result := transformFunc(doc)
	vec, ok := result["embedding"].([]float64)
	if !ok {
		t.Fatalf("embedding 类型不是 []float64")
	}
	if len(vec) != 3 {
		t.Errorf("embedding 长度 = %d, want 3", len(vec))
	}
	for _, v := range vec {
		if v != 0.0 {
			t.Errorf("零向量期望 0.0, got %v", v)
		}
	}
}

// TestBuildTransformFunc_UpdateFieldType_NoOp 测试 UpdateScalarFieldTypeOperation 为 no-op
func TestBuildTransformFunc_UpdateFieldType_NoOp(t *testing.T) {
	op := &operation.UpdateScalarFieldTypeOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "score",
		NewFieldType:  "float",
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"score": 42}
	result := transformFunc(doc)
	if result["score"] != 42 {
		t.Errorf("score = %v, want 42 (no-op)", result["score"])
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/store/vector/ -run "TestComputeNewSchema|TestBuildTransformFunc" -v 2>&1 | head -20`
Expected: 编译失败（函数未定义）

- [ ] **Step 3: 实现 schema_utils.go**

创建 `schema_utils.go`，包含：
1. `ComputeNewSchema(oldSchema *CollectionSchema, operations []operation.Operation) (*CollectionSchema, error)`
2. 4 个私有子函数：`computeSchemaAddField` / `computeSchemaRenameField` / `computeSchemaUpdateFieldType` / `computeSchemaUpdateVectorDim`
3. `BuildTransformFunc(operations []operation.Operation) func(map[string]any) map[string]any`
4. `applyOperationToDoc(doc map[string]any, op operation.Operation) map[string]any`
5. 辅助函数 `mapStringToVectorDataType(typeStr string) VectorDataType`

对齐 Python `utils.py` 中的 `compute_new_schema` + `build_transform_func_for_operations` + `_apply_operation_to_doc` + 4 个 `_compute_schema_*` 函数。

关键实现细节：
- `ComputeNewSchema` 通过 `ToDict()`/`CollectionFromDict()` 深拷贝 schema
- `computeSchemaRenameField` 校验 old 存在 + new 不存在
- `computeSchemaUpdateFieldType` 校验字段非向量
- `computeSchemaUpdateVectorDim` 校验字段是向量
- `applyOperationToDoc` 中 `UpdateEmbeddingDimensionOperation` 无 RecomputeEmbeddingFunc 时用 `make([]float64, op.NewDimension)` 零向量

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/store/vector/ -run "TestComputeNewSchema|TestBuildTransformFunc" -v`
Expected: PASS

- [ ] **Step 5: 补充边界测试**

在 `schema_utils_test.go` 中补充：
- `TestComputeNewSchema_EmptyOperations` — 空操作列表返回深拷贝
- `TestComputeNewSchema_UnsupportedOperationType` — 未知操作类型报错
- `TestBuildTransformFunc_MultipleOperations` — 多个操作顺序应用
- `TestBuildTransformFunc_AddField_AlreadyExists` — 字段已存在时不覆盖
- `TestBuildTransformFunc_RenameField_OldNotExist` — 旧字段不存在时 no-op
- `TestBuildTransformFunc_UpdateEmbeddingDim_WithRecomputeFunc` — 有 RecomputeEmbeddingFunc 时调用
- `TestBuildTransformFunc_UpdateEmbeddingDim_DimensionMismatch` — 返回向量长度不匹配时报错

- [ ] **Step 6: 运行全部测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/foundation/store/vector/ -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "feat: ComputeNewSchema + BuildTransformFunc 共享工具函数"
```

---

### Task 3: KVMigrator

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/kv_migrator.go`
- Create: `internal/agentcore/memory/migration/migrator/kv_migrator_test.go`

**Python 对照**：`openjiuwen/core/memory/migration/migrator/kv_migrator.py`

- [ ] **Step 1: 编写 KVMigrator 失败测试**

在 `kv_migrator_test.go` 中编写测试，使用 `InMemoryKVStore` + 注册 prefix：
- `TestKVMigrator_TryMigrate_空操作` — operations 为空时返回 nil
- `TestKVMigrator_TryMigrate_不支持EntityKey` — entity_key 非 "kv_global" 时返回错误
- `TestKVMigrator_TryMigrate_版本已是最新` — 无需迁移
- `TestKVMigrator_TryMigrate_执行成功` — 注册 UpdateKVOperation，验证执行后版本更新
- `TestKVMigrator_TryMigrate_执行失败回滚` — UpdateFunc 返回错误时从备份恢复
- `TestKVMigrator_GetCurrentVersion_新初始化` — 无版本 key 且无数据时设置初始版本
- `TestKVMigrator_GetCurrentVersion_旧数据` — 有数据但无版本 key 时返回 -1
- `TestKVMigrator_ValidateOperationsOrder` — 版本非递增时返回 false
- `TestKVMigrator_CreateBackup` — 备份包含所有 prefix 数据
- `TestKVMigrator_RestoreFromBackup` — 从备份恢复数据

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/migration/migrator/ -run "TestKVMigrator" -v 2>&1 | head -20`
Expected: 编译失败

- [ ] **Step 3: 实现 KVMigrator**

创建 `kv_migrator.go`，包含：
1. 常量 `kvSchemaVersionKey = "MEMORY_MIGRATION_KV_SCHEMA_VERSION"` 和 `kvEntityKey = "kv_global"`
2. 结构体 `KVMigrator` 和构造函数 `NewKVMigrator`
3. `TryMigrate(ctx, entityKey, operations) error` — 实现 `migrator.Migrator` 接口
4. `getCurrentVersion(ctx) (int, error)` — 读取 KV 中的版本 key，解析 []byte→int
5. `hasMemoryModuleData(ctx) (bool, error)` — 遍历 `common.KVPrefixRegistry.GetAllPrefixes()` 检查有无数据
6. `updateVersion(ctx, version int) error` — `kvStore.Set(kvSchemaVersionKey, []byte(strconv.Itoa(version)))`
7. `executeOperation(ctx, op) error` — type switch `*operation.UpdateKVOperation` → 调用 `op.UpdateFunc(ctx, m.kvStore)`
8. `validateOperationsOrder(operations) bool` — 检查 schema_version 严格递增
9. `createBackup(ctx) (string, error)` — JSON 序列化所有 prefix 下数据
10. `restoreFromBackup(ctx, backupKey string) error` — 删除旧数据 + 反序列化 + 逐 key 恢复
11. `cleanupBackup(ctx, backupKey string) error` — 删除备份 key

日志对齐 Python `KVMigrator` 中所有 `memory_logger.info/warning/error` 调用。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/migration/migrator/ -run "TestKVMigrator" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: KVMigrator — KV 存储迁移执行器"
```

---

### Task 4: SQLMigrator

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/sql_migrator.go`
- Create: `internal/agentcore/memory/migration/migrator/sql_migrator_test.go`

**Python 对照**：`openjiuwen/core/memory/migration/migrator/sql_migrator.py`

- [ ] **Step 1: 编写 SQLMigrator 失败测试**

在 `sql_migrator_test.go` 中，使用 SQLite 内存数据库 + GORM + MemoryMetaManager：
- `TestSQLMigrator_TryMigrate_空操作` — 空操作返回 nil
- `TestSQLMigrator_TryMigrate_AddColumn` — 创建测试表 → 注册 AddColumnOperation → 验证列已添加
- `TestSQLMigrator_TryMigrate_RenameColumn` — 创建测试表 → 注册 RenameColumnOperation → 验证列已重命名
- `TestSQLMigrator_TryMigrate_版本已是最新` — 已有高版本时跳过
- `TestSQLMigrator_TryMigrate_版本跟踪` — 验证 memory_meta 表中版本记录正确

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/migration/migrator/ -run "TestSQLMigrator" -v 2>&1 | head -20`
Expected: 编译失败

- [ ] **Step 3: 实现 SQLMigrator**

创建 `sql_migrator.go`，包含：
1. 结构体 `SQLMigrator` 和构造函数 `NewSQLMigrator(sqlDb *model.SqlDbStore)`
2. `TryMigrate(ctx, entityKey, operations) error` — 实现 `migrator.Migrator` 接口
3. `migrateAddColumn(ctx, op *operation.AddColumnOperation) error` — GORM `db.Migrator().AddColumn()` 或原生 SQL fallback
4. `migrateRenameColumn(ctx, op *operation.RenameColumnOperation) error` — GORM `db.Migrator().RenameColumn()`
5. `migrateUpdateColumnType(ctx, op *operation.UpdateColumnTypeOperation) error` — GORM `db.Migrator().AlterColumn()` 或原生 SQL fallback

关键实现细节：
- GORM Migrator 需要模型结构体。由于表名/列名是动态的，使用 `db.Table(tableName).Migrator()` 或 `db.Exec(rawSQL)` fallback
- 版本跟踪：用 `MemoryMetaManager.Add/GetByTableName` 读写 `memory_meta` 表
- 跳过 `_validate_table`（Go 无 MEMORY_TABLES_CONFIG，Registry 已保证合法性）

日志对齐 Python `SQLMigrator` 中所有 `memory_logger` 调用。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/migration/migrator/ -run "TestSQLMigrator" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: SQLMigrator — SQL 表迁移执行器（GORM Migrator 替代 Alembic）"
```

---

### Task 5: VectorMigrator

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/vector_migrator.go`
- Create: `internal/agentcore/memory/migration/migrator/vector_migrator_test.go`

**Python 对照**：`openjiuwen/core/memory/migration/migrator/vector_migrator.py`

- [ ] **Step 1: 编写 VectorMigrator 失败测试**

在 `vector_migrator_test.go` 中，使用 `fakeVectorStore`：
- `TestVectorMigrator_TryMigrate_空操作` — 无操作时返回 nil
- `TestVectorMigrator_FindCollections_正常` — 集合名 `{user}_{scope}_summary` 匹配
- `TestVectorMigrator_FindCollections_不支持的MemoryType` — 返回错误
- `TestVectorMigrator_MigrateCollections_版本过滤` — 只应用 schema_version > current_version 的操作

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 VectorMigrator**

创建 `vector_migrator.go`，包含：
1. 结构体 `VectorMigrator` 和构造函数 `NewVectorMigrator(vectorStore vector.BaseVectorStore)`
2. `TryMigrate(ctx, entityKey, operations) error` — 实现 `migrator.Migrator` 接口
3. `migrateCollections(ctx, collectionNames, operations) error` — 逐集合获取 metadata→过滤操作→调用 UpdateSchema→更新 metadata
4. `findCollections(ctx, memTypeStr string) ([]string, error)` — 去掉 `vector_` 前缀→`SupportMemoryType` 校验→`ListCollectionNames` 过滤后缀

日志对齐 Python `VectorMigrator` 中所有 `memory_logger` 调用。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: VectorMigrator — 向量集合迁移执行器"
```

---

### Task 6: MessageMigrator

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/message_migrator.go`
- Create: `internal/agentcore/memory/migration/migrator/message_migrator_test.go`

**Python 对照**：`openjiuwen/core/memory/migration/migrator/message_migrator.py`

- [ ] **Step 1: 编写 MessageMigrator 失败测试**

在 `message_migrator_test.go` 中，使用 `fakeMessageStore`：
- `TestMessageMigrator_TryMigrate_空操作` — 无操作返回 nil
- `TestMessageMigrator_TryMigrate_不支持EntityKey` — 非 "message_global" 时返回错误
- `TestMessageMigrator_TryMigrate_版本已是最新` — 无需迁移
- `TestMessageMigrator_TryMigrate_执行成功` — 验证 UpdateFunc 被调用 + 版本更新
- `TestMessageMigrator_TryMigrate_执行失败回滚` — 备份恢复逻辑
- `TestMessageMigrator_ValidateOperationsOrder` — 版本非递增时返回错误
- `TestMessageMigrator_CreateBackup` — 备份包含消息数据
- `TestMessageMigrator_RestoreFromBackup` — 恢复消息数据 + 重置版本

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 MessageMigrator**

创建 `message_migrator.go`，包含：
1. 常量 `messageEntityKey = "message_global"` 和 `backupPageSize = 1000`
2. 结构体 `MessageMigrator` 和构造函数 `NewMessageMigrator(messageStore db.BaseMessageStore)`
3. `TryMigrate(ctx, entityKey, operations) error` — 实现 `migrator.Migrator` 接口
4. `executeOperation(ctx, op) error` — type switch `*operation.UpdateMessageOperation` → 调用 `op.UpdateFunc(ctx, m.messageStore)`
5. `validateOperationsOrder(operations) bool`
6. `createBackup(ctx) ([]*messageBackupRecord, error)` — `GetMessages` 全量读取→序列化
7. `restoreFromBackup(ctx, backup []*messageBackupRecord, preVersion int32)` — `DeleteMessages` 全量删除→逐条 `AddMessage`→重置版本

日志对齐 Python `MessageMigrator` 中所有 `memory_logger` 调用。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: MessageMigrator — 消息存储迁移执行器"
```

---

### Task 7: IndexVersionMigrator

**Files:**
- Create: `internal/agentcore/memory/migration/migrator/index_version_migrator.go`
- Create: `internal/agentcore/memory/migration/migrator/index_version_migrator_test.go`

**Python 对照**：`openjiuwen/core/memory/migration/migrator/index_version_migrator.py`

- [ ] **Step 1: 编写 IndexVersionMigrator 失败测试**

在 `index_version_migrator_test.go` 中，使用 `SimpleMemoryIndex`：
- `TestIndexVersionMigrator_TryMigrate_空操作` — 无操作返回 nil
- `TestIndexVersionMigrator_TryMigrate_RenameField` — 添加 MemoryDoc → rename → 验证字段名已变
- `TestIndexVersionMigrator_TryMigrate_TransformField` — 验证字段值已变换
- `TestIndexVersionMigrator_TryMigrate_AddField` — 验证新字段已添加 + 默认值
- `TestIndexVersionMigrator_TryMigrate_AddField_WithFunc` — DefaultValueOrFunc 为函数时调用
- `TestIndexVersionMigrator_TryMigrate_RemoveField` — 验证字段已删除
- `TestIndexVersionMigrator_TryMigrate_版本已是最新` — 无需迁移
- `TestIndexVersionMigrator_TryMigrate_执行失败回滚` — 备份恢复逻辑
- `TestIndexVersionMigrator_TryMigrate_不支持的Operation` — 非 Index 操作类型返回错误

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 实现 IndexVersionMigrator**

创建 `index_version_migrator.go`，包含：
1. 结构体 `IndexVersionMigrator{}`（无状态）
2. `TryMigrate(ctx, index index.BaseMemoryIndex, operations []operation.Operation) error` — **注意此签名不接受 entityKey，不实现 `migrator.Migrator` 接口**
3. `applyOperation(ctx, index, op) error` — type switch 分发
4. `applyRenameField(ctx, index, op *operation.RenameMemoryDocFieldOperation) error`
5. `applyTransformField(ctx, index, op *operation.TransformMemoryDocFieldOperation) error`
6. `applyAddField(ctx, index, op *operation.AddMemoryDocFieldOperation) error`
7. `applyRemoveField(ctx, index, op *operation.RemoveMemoryDocFieldOperation) error`

每个 apply 方法遵循 Python 的模式：`ListUserScopes` → 逐 scope 批量（batch=100）`ListMemories` → 变换 → `DeleteMemories` + `AddMemories`

日志对齐 Python `IndexVersionMigrator` 中所有 `memory_logger` 调用。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: IndexVersionMigrator — 记忆索引版本迁移执行器"
```

---

### Task 8: run_migrations 编排

**Files:**
- Create: `internal/agentcore/memory/migration/run_migrations.go`
- Create: `internal/agentcore/memory/migration/run_migrations_test.go`

**Python 对照**：`openjiuwen/core/memory/migration/run_migrations.py`

- [ ] **Step 1: 编写 run_migrations 失败测试**

在 `run_migrations_test.go` 中：
- `TestRunMigrationsWithRegistry_空Registry` — 无注册操作时跳过
- `TestRunMigrationsWithRegistry_成功` — mock Migrator 验证调用
- `TestRunMigrationsWithRegistry_失败` — mock Migrator 返回错误时传播
- `TestRunIndexVersionMigrations_空Registry` — 无注册操作时跳过
- `TestRunIndexVersionMigrations_成功` — 验证 IndexVersionMigrator.TryMigrate 被正确调用

- [ ] **Step 2: 运行测试验证失败**

- [ ] **Step 3: 定义 Migrator 接口 + 实现 run_migrations.go**

在 `run_migrations.go` 中：

1. 定义 `Migrator` 接口（放在 `migration` 包根）：
```go
// Migrator 通用迁移器接口。
type Migrator interface {
    TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error
}
```

2. 实现 `runMigrationsWithRegistry`：
```go
func runMigrationsWithRegistry(ctx context.Context, registry *operation.OperationRegistry, migrator Migrator, storeName string) error
```

3. 实现 5 个公开函数：
- `RunVectorMigrations(ctx, vectorStore)` — 创建 VectorMigrator → runMigrationsWithRegistry
- `RunKVMigrations(ctx, kvStore)` — 创建 KVMigrator → runMigrationsWithRegistry
- `RunSQLMigrations(ctx, sqlDbStore)` — 创建 SQLMigrator → runMigrationsWithRegistry
- `RunMessageMigrations(ctx, messageStore)` — 创建 MessageMigrator → runMigrationsWithRegistry
- `RunIndexVersionMigrations(ctx, index)` — **独立实现**，不使用 runMigrationsWithRegistry，因为 IndexVersionMigrator 不实现 Migrator 接口

日志对齐 Python `run_migrations.py` 中所有 `memory_logger` 调用。

- [ ] **Step 4: 运行测试验证通过**

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "feat: run_migrations — 迁移编排函数（5 个 RunXxx + runMigrationsWithRegistry）"
```

---

### Task 9: MilvusVectorStore UpdateSchema 回填

**Files:**
- Modify: `internal/agentcore/foundation/store/vector/milvus.go`
- Modify: `internal/agentcore/foundation/store/vector/milvus_test.go`

**Python 对照**：`openjiuwen/core/foundation/store/vector/milvus_vector_store.py` (`update_schema` + `_execute_migration`)

- [ ] **Step 1: 实现 MilvusVectorStore.UpdateSchema**

替换现有 stub，实现完整迁移逻辑：
1. operations 为空 → return nil
2. `GetSchema(collectionName)` 获取旧 schema
3. `ComputeNewSchema(oldSchema, operations)` 计算新 schema
4. `BuildTransformFunc(operations)` 构建变换函数
5. `GetCollectionMetadata(collectionName)` 获取元数据
6. `executeMigration(collectionName, newSchema, transformFunc, metadata)`：
   - 创建临时集合 `{collectionName}_migration_{timestamp}`
   - 流式读取旧集合文档（分页 batch=100）
   - 批量应用 transformFunc 写入临时集合
   - 删除旧集合
   - `RenameCollection` 临时集合 → 原名
   - 清除 `_collectionMetadata` 缓存
   - 失败时清理临时集合

- [ ] **Step 2: 更新测试**

1. 删除旧 stub 测试 `TestMilvusVectorStore_UpdateSchema_预留`
2. 新增 `//go:build integration` 测试（需要真实 Milvus）

- [ ] **Step 3: 编译验证**

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat: MilvusVectorStore.UpdateSchema 回填 — 临时集合+流式读取+rename"
```

---

### Task 10: ChromaVectorStore UpdateSchema 回填

**Files:**
- Modify: `internal/agentcore/foundation/store/vector/chroma.go`
- Modify: `internal/agentcore/foundation/store/vector/chroma_test.go`

**Python 对照**：`openjiuwen/core/foundation/store/vector/chroma_vector_store.py` (`update_schema` + `_execute_migration`)

- [ ] **Step 1: 实现 ChromaVectorStore.UpdateSchema**

替换现有 stub，实现双重拷贝迁移逻辑：
1. 同 Milvus 的步骤 1-5
2. `executeMigration`：
   - 创建临时集合
   - 全量读取旧集合文档
   - 应用 transformFunc 写入临时集合
   - 删除旧集合
   - **双重拷贝**：全量读取临时集合 → 创建原名称新集合 → 写入 → 删除临时集合
   - 失败时清理临时集合

- [ ] **Step 2: 更新测试**

1. 删除旧 stub 测试
2. 新增 `//go:build integration` 测试

- [ ] **Step 3: 编译验证**

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat: ChromaVectorStore.UpdateSchema 回填 — 双重拷贝策略"
```

---

### Task 11: GaussVectorStore UpdateSchema 回填

**Files:**
- Modify: `internal/agentcore/foundation/store/vector/gauss.go`
- Modify: `internal/agentcore/foundation/store/vector/gauss_test.go`

**Python 对照**：`openjiuwen/core/foundation/store/vector/gauss_vector_store.py` (`update_schema`)

- [ ] **Step 1: 实现 GaussVectorStore.UpdateSchema**

替换现有 stub，实现 ALTER TABLE RENAME 迁移逻辑：
1. 同 Milvus 的步骤 1-5
2. 迁移执行（对齐 Python 内联实现）：
   - 创建临时集合
   - SQL cursor `SELECT * FROM {collectionName}` 读取所有行
   - 批量（batch=100）应用 transformFunc 写入临时集合
   - 删除旧集合
   - `ALTER TABLE {tempName} RENAME TO {collectionName}`
   - 清除 `_collectionMetadata` 缓存
   - 失败时清理临时集合

- [ ] **Step 2: 更新测试**

1. 删除旧 stub 测试
2. 新增 `//go:build integration` 测试

- [ ] **Step 3: 编译验证**

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat: GaussVectorStore.UpdateSchema 回填 — SQL cursor + ALTER TABLE RENAME"
```

---

### Task 12: ESVectorStore UpdateSchema 回填

**Files:**
- Modify: `internal/agentcore/foundation/store/vector/es.go`
- Modify: `internal/agentcore/foundation/store/vector/es_test.go`

**Python 对照**：`openjiuwen/extensions/store/vector/es_vector_store.py` (`update_schema`)

- [ ] **Step 1: 实现 ESVectorStore.UpdateSchema**

替换现有 stub，实现双重拷贝迁移逻辑：
1. 同 Milvus 的步骤 1-5
2. 迁移执行：
   - 创建临时索引
   - `_es.search` 读取旧索引文档（排除 `_METADATA_DOC_ID`，size=10000）
   - 应用 transformFunc 写入临时索引
   - 删除旧索引
   - **双重拷贝**：全量读取临时索引 → 创建原名称新索引 → 写入 → 删除临时索引
   - 失败时清理临时索引

- [ ] **Step 2: 更新测试**

1. 删除旧 stub 测试
2. 新增 `//go:build integration` 测试

- [ ] **Step 3: 编译验证**

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "feat: ESVectorStore.UpdateSchema 回填 — search 读取 + 双重拷贝"
```

---

### Task 13: doc.go 更新 + IMPLEMENTATION_PLAN 回填标记

**Files:**
- Modify: `internal/agentcore/foundation/store/vector/doc.go`
- Modify: `internal/agentcore/memory/migration/migrator/doc.go`
- Modify: `internal/agentcore/memory/migration/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

1. `foundation/store/vector/doc.go` — 添加 `schema_utils.go` 条目
2. `migration/migrator/doc.go` — 添加 5 个新文件条目（sql_migrator / kv_migrator / vector_migrator / message_migrator / index_version_migrator）
3. `migration/doc.go` — 添加 `run_migrations.go` 条目 + 更新 migrator 子包描述

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 回填标记**

| 条目 | 变更 |
|------|------|
| 7.21 | 移除 `⤵️ 7.22-7.23 Migrator + run_migrations 待后续回填`，改为 `✅`（不再有 ⤵️） |
| 7.22 | `☐` → `✅`，移除 `⤴️ 需回填 MilvusVectorStore.UpdateSchema` |
| 7.23 | `☐` → `✅`，移除 `⤴️ 需回填 MilvusVectorStore.UpdateSchema` |
| 4.8 | 移除 `UpdateSchema 待 7.22/7.23 回填` |
| 4.9 | 移除 `UpdateSchema 待 7.22/7.23 回填` |
| 4.10 | 移除 `UpdateSchema 待 7.22/7.23 回填` |
| 4.11 | 移除 `UpdateSchema 待 7.22/7.23 回填` |

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "docs: 更新 doc.go + IMPLEMENTATION_PLAN 回填标记（7.22+7.23 完成）"
```

---

## 自查清单

### Spec 覆盖率

| Spec 章节 | 对应 Task | 状态 |
|-----------|----------|------|
| 一、接口变更 | Task 1 | ✅ |
| 二、共享工具函数 | Task 2 | ✅ |
| 三.1 SQLMigrator | Task 4 | ✅ |
| 三.2 KVMigrator | Task 3 | ✅ |
| 三.3 VectorMigrator | Task 5 | ✅ |
| 三.4 MessageMigrator | Task 6 | ✅ |
| 三.5 IndexVersionMigrator | Task 7 | ✅ |
| 四、run_migrations | Task 8 | ✅ |
| 五.2 Milvus UpdateSchema | Task 9 | ✅ |
| 五.3 Chroma UpdateSchema | Task 10 | ✅ |
| 五.4 Gauss UpdateSchema | Task 11 | ✅ |
| 五.5 ES UpdateSchema | Task 12 | ✅ |
| 六、日志同步 | 每个 Task 内含 | ✅ |
| 七、测试策略 | 每个 Task 内含 | ✅ |
| 八、回填标记 | Task 13 | ✅ |
| 九、文件清单 | Task 1-13 覆盖 | ✅ |

### Placeholder 扫描

无 TBD/TODO/implement later/fill in details 等占位符。

### 类型一致性

- `ComputeNewSchema` 接受 `[]operation.Operation`，与 `UpdateSchema` 签名一致
- `Migrator` 接口 `TryMigrate(ctx, entityKey, operations []operation.Operation)` 与各 Migrator 实现一致
- `BuildTransformFunc` 返回 `func(map[string]any) map[string]any`，与 VectorStore 的 AddDocs 签名一致
- `IndexVersionMigrator.TryMigrate` 签名特殊（不接受 entityKey），与 `RunIndexVersionMigrations` 的独立实现一致
