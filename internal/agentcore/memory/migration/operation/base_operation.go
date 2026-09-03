package operation

import "reflect"

// ──────────────────────────── 结构体 ────────────────────────────

// OperationMetadata 操作元数据。
// 对齐 Python: openjiuwen/core/memory/migration/operation/base_operation.py (OperationMetadata)
type OperationMetadata struct {
	// SchemaVersion Schema 版本号（用于链式升级）
	SchemaVersion int
	// Description 可选描述，用于日志和审计
	Description string
}

// BaseOperation 所有操作的基类（纯 DTO，无执行逻辑）。
// 对齐 Python: openjiuwen/core/memory/migration/operation/base_operation.py (BaseOperation)
// 嵌入此结构体的具体类型自动满足 Operation 接口。
type BaseOperation struct {
	// Metadata 操作元数据
	Metadata OperationMetadata
}

// Operation 所有操作类型必须实现的接口。
// Go 中替代 Python BaseOperation 继承的多态契约。
// 具体类型（AddColumnOperation 等）嵌入 BaseOperation 结构体来满足此接口。
type Operation interface {
	// SchemaVersion 返回操作的 schema 版本号
	SchemaVersion() int
	// Description 返回操作的描述
	Description() string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SchemaVersion 返回操作的 schema 版本号。
// 对齐 Python: BaseOperation.schema_version (property)
func (op *BaseOperation) SchemaVersion() int {
	return op.Metadata.SchemaVersion
}

// Description 返回操作的描述。
// 对齐 Python: BaseOperation.description (property)
// 若 Metadata.Description 为空，返回结构体类型名。
func (op *BaseOperation) Description() string {
	if op.Metadata.Description != "" {
		return op.Metadata.Description
	}
	// 对齐 Python: return self.__class__.__name__
	return reflect.TypeOf(op).Elem().Name()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时校验 BaseOperation 满足 Operation 接口
var _ Operation = (*BaseOperation)(nil)
