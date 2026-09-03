package operation

import (
	"context"
	"testing"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// ──────────────────────────── 编译时接口校验 ────────────────────────────

// 所有具体类型必须满足 Operation 接口
func TestOperation_接口满足(t *testing.T) {
	var _ Operation = (*AddColumnOperation)(nil)
	var _ Operation = (*RenameColumnOperation)(nil)
	var _ Operation = (*UpdateColumnTypeOperation)(nil)
	var _ Operation = (*AddScalarFieldOperation)(nil)
	var _ Operation = (*RenameScalarFieldOperation)(nil)
	var _ Operation = (*UpdateScalarFieldTypeOperation)(nil)
	var _ Operation = (*UpdateEmbeddingDimensionOperation)(nil)
	var _ Operation = (*UpdateKVOperation)(nil)
	var _ Operation = (*UpdateMessageOperation)(nil)
	var _ Operation = (*RenameMemoryDocFieldOperation)(nil)
	var _ Operation = (*TransformMemoryDocFieldOperation)(nil)
	var _ Operation = (*AddMemoryDocFieldOperation)(nil)
	var _ Operation = (*RemoveMemoryDocFieldOperation)(nil)
}

// ──────────────────────────── SQL Operations ────────────────────────────

// TestAddColumnOperation_字段 测试 AddColumnOperation 字段
func TestAddColumnOperation_字段(t *testing.T) {
	op := AddColumnOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		Table:         "user_messages",
		ColumnName:    "test_col",
		ColumnType:    "INT",
		Nullable:      true,
		Default:       0,
	}
	if op.SchemaVersion() != 1 {
		t.Errorf("SchemaVersion() = %d, want 1", op.SchemaVersion())
	}
	if op.Table != "user_messages" {
		t.Errorf("Table = %q, want %q", op.Table, "user_messages")
	}
	if op.ColumnName != "test_col" {
		t.Errorf("ColumnName = %q, want %q", op.ColumnName, "test_col")
	}
	if op.ColumnType != "INT" {
		t.Errorf("ColumnType = %q, want %q", op.ColumnType, "INT")
	}
	if !op.Nullable {
		t.Error("Nullable 应为 true")
	}
	if op.Default != 0 {
		t.Errorf("Default = %v, want 0", op.Default)
	}
}

// TestAddColumnOperation_默认值 测试 Nullable 默认为 false
func TestAddColumnOperation_默认值(t *testing.T) {
	op := AddColumnOperation{}
	if op.Nullable {
		t.Error("Nullable 默认应为 false（Go 零值），使用者需显式设置")
	}
}

// TestRenameColumnOperation_字段 测试 RenameColumnOperation 字段
func TestRenameColumnOperation_字段(t *testing.T) {
	op := RenameColumnOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		Table:         "users",
		OldColumnName: "old",
		NewColumnName: "new",
	}
	if op.OldColumnName != "old" || op.NewColumnName != "new" {
		t.Error("RenameColumnOperation 字段赋值不正确")
	}
}

// TestUpdateColumnTypeOperation_字段 测试 UpdateColumnTypeOperation 字段
func TestUpdateColumnTypeOperation_字段(t *testing.T) {
	op := UpdateColumnTypeOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		Table:         "users",
		ColumnName:    "age",
		NewColumnType: "BIGINT",
	}
	if op.ColumnName != "age" {
		t.Errorf("ColumnName = %q, want %q", op.ColumnName, "age")
	}
	if op.NewColumnType != "BIGINT" {
		t.Errorf("NewColumnType = %q, want %q", op.NewColumnType, "BIGINT")
	}
}

// ──────────────────────────── Vector Operations ────────────────────────────

// TestAddScalarFieldOperation_字段 测试 AddScalarFieldOperation 字段
func TestAddScalarFieldOperation_字段(t *testing.T) {
	op := AddScalarFieldOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		DataType:      "vector_summary",
		FieldName:     "msg_id",
		FieldType:     "string",
	}
	if op.DataType != "vector_summary" {
		t.Errorf("DataType = %q, want %q", op.DataType, "vector_summary")
	}
	if op.FieldName != "msg_id" {
		t.Errorf("FieldName = %q, want %q", op.FieldName, "msg_id")
	}
}

// TestRenameScalarFieldOperation_字段 测试 RenameScalarFieldOperation 字段
func TestRenameScalarFieldOperation_字段(t *testing.T) {
	op := RenameScalarFieldOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		DataType:      "vector_summary",
		OldFieldName:  "old_field",
		NewFieldName:  "new_field",
	}
	if op.OldFieldName != "old_field" || op.NewFieldName != "new_field" {
		t.Error("RenameScalarFieldOperation 字段赋值不正确")
	}
}

// TestUpdateScalarFieldTypeOperation_字段 测试 UpdateScalarFieldTypeOperation 字段
func TestUpdateScalarFieldTypeOperation_字段(t *testing.T) {
	op := UpdateScalarFieldTypeOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		DataType:      "vector_summary",
		FieldName:     "score",
		NewFieldType:  "float",
	}
	if op.FieldName != "score" {
		t.Errorf("FieldName = %q, want %q", op.FieldName, "score")
	}
}

// TestUpdateEmbeddingDimensionOperation_字段 测试 UpdateEmbeddingDimensionOperation 字段
func TestUpdateEmbeddingDimensionOperation_字段(t *testing.T) {
	op := UpdateEmbeddingDimensionOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 2}},
		DataType:      "vector_summary",
		FieldName:     "embedding",
		NewDimension:  1024,
		BatchSize:     500,
	}
	if op.NewDimension != 1024 {
		t.Errorf("NewDimension = %d, want 1024", op.NewDimension)
	}
	if op.BatchSize != 500 {
		t.Errorf("BatchSize = %d, want 500", op.BatchSize)
	}
}

// TestUpdateEmbeddingDimensionOperation_默认batchSize 测试 BatchSize 默认值
func TestUpdateEmbeddingDimensionOperation_默认batchSize(t *testing.T) {
	op := UpdateEmbeddingDimensionOperation{}
	// Python 默认 batch_size=1000，Go 零值为 0，使用者需显式设置
	if op.BatchSize != 0 {
		t.Errorf("BatchSize 默认应为 0（Go 零值），got %d", op.BatchSize)
	}
}

// ──────────────────────────── KV Operations ────────────────────────────

// TestUpdateKVOperation_回调 测试 UpdateKVOperation 回调函数
func TestUpdateKVOperation_回调(t *testing.T) {
	called := false
	fn := func(ctx context.Context, store kv.BaseKVStore) error {
		called = true
		return nil
	}
	op := UpdateKVOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		UpdateFunc:    fn,
	}
	if op.UpdateFunc == nil {
		t.Error("UpdateFunc 不应为 nil")
	}
	_ = op.UpdateFunc(context.Background(), nil)
	if !called {
		t.Error("UpdateFunc 应被调用")
	}
}

// ──────────────────────────── Message Operations ────────────────────────────

// TestUpdateMessageOperation_回调 测试 UpdateMessageOperation 回调函数
func TestUpdateMessageOperation_回调(t *testing.T) {
	called := false
	fn := func(ctx context.Context, store any) error {
		called = true
		return nil
	}
	op := UpdateMessageOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		UpdateFunc:    fn,
	}
	if op.UpdateFunc == nil {
		t.Error("UpdateFunc 不应为 nil")
	}
	_ = op.UpdateFunc(context.Background(), nil)
	if !called {
		t.Error("UpdateFunc 应被调用")
	}
}

// ──────────────────────────── Index Operations ────────────────────────────

// TestRenameMemoryDocFieldOperation_字段 测试 RenameMemoryDocFieldOperation 字段
func TestRenameMemoryDocFieldOperation_字段(t *testing.T) {
	op := RenameMemoryDocFieldOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		OldFieldName:  "memory_text",
		NewFieldName:  "text",
	}
	if op.OldFieldName != "memory_text" {
		t.Errorf("OldFieldName = %q, want %q", op.OldFieldName, "memory_text")
	}
}

// TestTransformMemoryDocFieldOperation_回调 测试 TransformMemoryDocFieldOperation 回调
func TestTransformMemoryDocFieldOperation_回调(t *testing.T) {
	op := TransformMemoryDocFieldOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		FieldName:     "score",
		TransformFunc: func(v any) any { return v.(int) * 100 },
	}
	if op.TransformFunc == nil {
		t.Error("TransformFunc 不应为 nil")
	}
	result := op.TransformFunc(1)
	if result != 100 {
		t.Errorf("TransformFunc(1) = %v, want 100", result)
	}
}

// TestAddMemoryDocFieldOperation_字段 测试 AddMemoryDocFieldOperation 字段
func TestAddMemoryDocFieldOperation_字段(t *testing.T) {
	op := AddMemoryDocFieldOperation{
		BaseOperation:      BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		FieldName:          "new_field",
		DefaultValueOrFunc: "default_val",
	}
	if op.FieldName != "new_field" {
		t.Errorf("FieldName = %q, want %q", op.FieldName, "new_field")
	}
	if op.DefaultValueOrFunc != "default_val" {
		t.Errorf("DefaultValueOrFunc = %v, want %q", op.DefaultValueOrFunc, "default_val")
	}
}

// TestAddMemoryDocFieldOperation_函数默认值 测试函数类型默认值
func TestAddMemoryDocFieldOperation_函数默认值(t *testing.T) {
	fn := func() any { return 42 }
	op := AddMemoryDocFieldOperation{
		BaseOperation:      BaseOperation{Metadata: OperationMetadata{SchemaVersion: 2}},
		FieldName:          "computed_field",
		DefaultValueOrFunc: fn,
	}
	// 运行时判断是值还是函数
	if computed, ok := op.DefaultValueOrFunc.(func() any); ok {
		if computed() != 42 {
			t.Errorf("函数默认值返回 %v, want 42", computed())
		}
	} else {
		t.Error("DefaultValueOrFunc 应为 func() any 类型")
	}
}

// TestRemoveMemoryDocFieldOperation_字段 测试 RemoveMemoryDocFieldOperation 字段
func TestRemoveMemoryDocFieldOperation_字段(t *testing.T) {
	op := RemoveMemoryDocFieldOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: 1}},
		FieldName:     "obsolete_field",
	}
	if op.FieldName != "obsolete_field" {
		t.Errorf("FieldName = %q, want %q", op.FieldName, "obsolete_field")
	}
}
