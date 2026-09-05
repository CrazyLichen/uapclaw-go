package operation

import (
	"context"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ==================== SQL Operations ====================

// AddColumnOperation 添加列操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (AddColumnOperation)
type AddColumnOperation struct {
	BaseOperation
	// Table 目标表名
	Table string
	// ColumnName 新列名
	ColumnName string
	// ColumnType 列数据类型
	ColumnType string
	// Nullable 是否允许 NULL（Python 默认 True，Go 使用者需显式设置）
	Nullable bool
	// Default 默认值
	Default any
}

// RenameColumnOperation 重命名列操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (RenameColumnOperation)
type RenameColumnOperation struct {
	BaseOperation
	// Table 目标表名
	Table string
	// OldColumnName 原列名
	OldColumnName string
	// NewColumnName 新列名
	NewColumnName string
}

// TypeName 返回操作类型名 "RenameColumnOperation"。
func (op *RenameColumnOperation) TypeName() string { return "RenameColumnOperation" }

// UpdateColumnTypeOperation 修改列类型操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (UpdateColumnTypeOperation)
type UpdateColumnTypeOperation struct {
	BaseOperation
	// Table 目标表名
	Table string
	// ColumnName 目标列名
	ColumnName string
	// NewColumnType 新数据类型
	NewColumnType string
}

// TypeName 返回操作类型名 "UpdateColumnTypeOperation"。
func (op *UpdateColumnTypeOperation) TypeName() string { return "UpdateColumnTypeOperation" }

// ==================== Vector Operations ====================

// AddScalarFieldOperation 添加向量标量字段操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (AddScalarFieldOperation)
type AddScalarFieldOperation struct {
	BaseOperation
	// DataType 向量数据类型名
	DataType string
	// FieldName 标量字段名
	FieldName string
	// FieldType 字段数据类型（如 "int"、"float"、"string"）
	FieldType string
	// DefaultValue 字段默认值
	DefaultValue any
}

// TypeName 返回操作类型名 "AddScalarFieldOperation"。
func (op *AddScalarFieldOperation) TypeName() string { return "AddScalarFieldOperation" }

// RenameScalarFieldOperation 重命名向量标量字段操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (RenameScalarFieldOperation)
type RenameScalarFieldOperation struct {
	BaseOperation
	// DataType 向量数据类型名
	DataType string
	// OldFieldName 原字段名
	OldFieldName string
	// NewFieldName 新字段名
	NewFieldName string
}

// TypeName 返回操作类型名 "RenameScalarFieldOperation"。
func (op *RenameScalarFieldOperation) TypeName() string { return "RenameScalarFieldOperation" }

// UpdateScalarFieldTypeOperation 修改向量标量字段类型操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (UpdateScalarFieldTypeOperation)
type UpdateScalarFieldTypeOperation struct {
	BaseOperation
	// DataType 向量数据类型名
	DataType string
	// FieldName 目标字段名
	FieldName string
	// NewFieldType 新数据类型
	NewFieldType string
}

// TypeName 返回操作类型名 "UpdateScalarFieldTypeOperation"。
func (op *UpdateScalarFieldTypeOperation) TypeName() string { return "UpdateScalarFieldTypeOperation" }

// UpdateEmbeddingDimensionOperation 更新嵌入维度操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (UpdateEmbeddingDimensionOperation)
type UpdateEmbeddingDimensionOperation struct {
	BaseOperation
	// DataType 向量数据类型名
	DataType string
	// FieldName 嵌入字段名（如 "embedding"）
	FieldName string
	// NewDimension 新的嵌入向量维度
	NewDimension int
	// RecomputeEmbeddingFunc 重新计算嵌入的回调函数（由 Adapter 调用）
	RecomputeEmbeddingFunc func(any) any
	// BatchSize 重新计算嵌入时的批量大小（Python 默认 1000，Go 使用者需显式设置）
	BatchSize int
}

// TypeName 返回操作类型名 "UpdateEmbeddingDimensionOperation"。
func (op *UpdateEmbeddingDimensionOperation) TypeName() string { return "UpdateEmbeddingDimensionOperation" }

// NewUpdateEmbeddingDimensionOperation 创建更新嵌入维度操作，BatchSize 默认 1000 对齐 Python。
// 对齐 Python: UpdateEmbeddingDimensionOperation(batch_size=1000)
func NewUpdateEmbeddingDimensionOperation(schemaVersion int, dataType, fieldName string, newDimension int) *UpdateEmbeddingDimensionOperation {
	return &UpdateEmbeddingDimensionOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: schemaVersion}},
		DataType:      dataType,
		FieldName:     fieldName,
		NewDimension:  newDimension,
		BatchSize:     1000,
	}
}

// ==================== KV Operations ====================

// UpdateKVCallable KV 更新回调函数类型。
// 对齐 Python: UpdateKVCallable = Callable[[BaseKVStore], Awaitable[None]]
type UpdateKVCallable func(ctx context.Context, kvStore kv.BaseKVStore) error

// UpdateKVOperation KV 更新操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (UpdateKVOperation)
type UpdateKVOperation struct {
	BaseOperation
	// UpdateFunc 执行 KV 更新的回调函数
	UpdateFunc UpdateKVCallable
}

// TypeName 返回操作类型名 "UpdateKVOperation"。
func (op *UpdateKVOperation) TypeName() string { return "UpdateKVOperation" }

// ==================== Message Operations ====================

// MessageUpdateCallable 消息更新回调函数类型。
// 对齐 Python: MessageUpdateCallable = Callable[[Any], Awaitable[None]]
type MessageUpdateCallable func(ctx context.Context, store any) error

// UpdateMessageOperation 消息更新操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (UpdateMessageOperation)
type UpdateMessageOperation struct {
	BaseOperation
	// UpdateFunc 执行消息更新的回调函数
	UpdateFunc MessageUpdateCallable
}

// TypeName 返回操作类型名 "UpdateMessageOperation"。
func (op *UpdateMessageOperation) TypeName() string { return "UpdateMessageOperation" }

// ==================== Index Operations ====================

// RenameMemoryDocFieldOperation 索引文档字段重命名操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (RenameMemoryDocFieldOperation)
type RenameMemoryDocFieldOperation struct {
	BaseOperation
	// OldFieldName 原字段名
	OldFieldName string
	// NewFieldName 新字段名
	NewFieldName string
}

// TypeName 返回操作类型名 "RenameMemoryDocFieldOperation"。
func (op *RenameMemoryDocFieldOperation) TypeName() string { return "RenameMemoryDocFieldOperation" }

// TransformMemoryDocFieldOperation 索引文档字段变换操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (TransformMemoryDocFieldOperation)
type TransformMemoryDocFieldOperation struct {
	BaseOperation
	// FieldName 目标字段名
	FieldName string
	// TransformFunc 字段值变换函数
	TransformFunc func(any) any
}

// TypeName 返回操作类型名 "TransformMemoryDocFieldOperation"。
func (op *TransformMemoryDocFieldOperation) TypeName() string { return "TransformMemoryDocFieldOperation" }

// AddMemoryDocFieldOperation 索引文档字段添加操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (AddMemoryDocFieldOperation)
type AddMemoryDocFieldOperation struct {
	BaseOperation
	// FieldName 新字段名
	FieldName string
	// DefaultValueOrFunc 默认值或生成默认值的函数（运行时判断类型）
	DefaultValueOrFunc any
}

// TypeName 返回操作类型名 "AddMemoryDocFieldOperation"。
func (op *AddMemoryDocFieldOperation) TypeName() string { return "AddMemoryDocFieldOperation" }

// RemoveMemoryDocFieldOperation 索引文档字段删除操作。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operations.py (RemoveMemoryDocFieldOperation)
type RemoveMemoryDocFieldOperation struct {
	BaseOperation
	// FieldName 要删除的字段名
	FieldName string
}

// TypeName 返回操作类型名 "RemoveMemoryDocFieldOperation"。
func (op *RemoveMemoryDocFieldOperation) TypeName() string { return "RemoveMemoryDocFieldOperation" }

// ──────────────────────────── 导出函数 ────────────────────────────

// TypeName 返回操作类型名 "AddColumnOperation"。
func (op *AddColumnOperation) TypeName() string { return "AddColumnOperation" }

// NewAddColumnOperation 创建添加列操作，Nullable 默认 true 对齐 Python。
// 对齐 Python: AddColumnOperation(nullable=True)
func NewAddColumnOperation(schemaVersion int, table, columnName, columnType string) *AddColumnOperation {
	return &AddColumnOperation{
		BaseOperation: BaseOperation{Metadata: OperationMetadata{SchemaVersion: schemaVersion}},
		Table:         table,
		ColumnName:    columnName,
		ColumnType:    columnType,
		Nullable:      true,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
