package operation

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
	// TypeName 返回操作类型的名称。
	// 对齐 Python: self.__class__.__name__ —— Go 中由于嵌入 BaseOperation 的方法集
	// 无法通过反射获取子类名，因此每个子类必须显式实现此方法返回自身类型名。
	TypeName() string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时校验 BaseOperation 满足 Operation 接口
var _ Operation = (*BaseOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// SchemaVersion 返回操作的 schema 版本号。
// 对齐 Python: BaseOperation.schema_version (property)
func (op *BaseOperation) SchemaVersion() int {
	return op.Metadata.SchemaVersion
}

// Description 返回操作的描述。
// 对齐 Python: BaseOperation.description (property)
// 若 Metadata.Description 为空，返回类型名称（通过 TypeName 接口方法获取子类名）。
func (op *BaseOperation) Description() string {
	if op.Metadata.Description != "" {
		return op.Metadata.Description
	}
	// 对齐 Python: return self.__class__.__name__
	// 通过 Operation 接口的 TypeName() 获取子类类型名；
	// 若无法获取（BaseOperation 自身），回退到反射。
	return op.TypeName()
}

// TypeName 返回 "BaseOperation"。
// 嵌入 BaseOperation 的子类必须覆盖此方法返回自身类型名。
func (op *BaseOperation) TypeName() string {
	return "BaseOperation"
}
