package operation

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OperationRegistry 按实体键管理链式升级操作的注册表。
// 对齐 Python: openjiuwen/core/memory/migration/operation/operation_registry.py (OperationRegistry)
//
// 约定：
//   - entity_key 为字符串，如 "user_messages"、"vector_summary"
//   - 同一 entity_key 下所有 Operation 的 schema_version 单调递增
type OperationRegistry struct {
	// operations entity_key → Operation 列表（按 schema_version 升序）
	operations map[string][]Operation
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOperationRegistry 创建空的注册表。
// 对齐 Python: OperationRegistry()
func NewOperationRegistry() *OperationRegistry {
	return &OperationRegistry{
		operations: make(map[string][]Operation),
	}
}

// Register 注册一个 Operation，确保 schema_version 单调递增。
// 对齐 Python: OperationRegistry.register
// 若违反单调递增约束，返回错误。
func (r *OperationRegistry) Register(entityKey string, op Operation) error {
	ops := r.operations[entityKey]

	// 首次注册
	if ops == nil {
		r.operations[entityKey] = []Operation{op}
		return nil
	}

	// 检查版本单调递增
	lastVersion := ops[len(ops)-1].SchemaVersion()
	if op.SchemaVersion() <= lastVersion {
		return exception.BuildError(
			exception.StatusMemoryRegisterOperationValidationInvalid,
			exception.WithParam("entity_key", entityKey),
			exception.WithParam("schema_version", fmt.Sprintf("%d", op.SchemaVersion())),
			exception.WithParam("error_msg", "the schema number of the new operation must be greater than the current maximum"),
		)
	}

	r.operations[entityKey] = append(ops, op)
	return nil
}

// GetOperations 获取指定实体在 [fromVersion, toVersion] 范围内的所有 Operation。
// 对齐 Python: OperationRegistry.get_operations
func (r *OperationRegistry) GetOperations(entityKey string, fromVersion, toVersion int) []Operation {
	if fromVersion > toVersion {
		return []Operation{}
	}
	ops := r.operations[entityKey]
	if len(ops) == 0 {
		return []Operation{}
	}
	var result []Operation
	for _, op := range ops {
		if op.SchemaVersion() >= fromVersion && op.SchemaVersion() <= toVersion {
			result = append(result, op)
		}
	}
	return result
}

// GetCurrentVersion 获取指定实体的最新 schema_version。
// 对齐 Python: OperationRegistry.get_current_version
// 无注册操作时返回 0。
func (r *OperationRegistry) GetCurrentVersion(entityKey string) int {
	ops := r.operations[entityKey]
	if len(ops) == 0 {
		return 0
	}
	return ops[len(ops)-1].SchemaVersion()
}

// GetAllEntities 获取所有已注册的实体键。
// 对齐 Python: OperationRegistry.get_all_entities
func (r *OperationRegistry) GetAllEntities() []string {
	entities := make([]string, 0, len(r.operations))
	for key := range r.operations {
		entities = append(entities, key)
	}
	return entities
}

// GetAllOperations 获取内部映射的浅拷贝。
// 对齐 Python: OperationRegistry.get_all_operations
func (r *OperationRegistry) GetAllOperations() map[string][]Operation {
	result := make(map[string][]Operation, len(r.operations))
	for key, ops := range r.operations {
		copied := make([]Operation, len(ops))
		copy(copied, ops)
		result[key] = copied
	}
	return result
}

// Clear 清空所有注册的操作（主要用于测试）。
// 对齐 Python: OperationRegistry.clear
func (r *OperationRegistry) Clear() {
	r.operations = make(map[string][]Operation)
}

// SetOperations 设置内部映射（主要用于测试恢复状态）。
// 对齐 Python: OperationRegistry.set_operations
func (r *OperationRegistry) SetOperations(operations map[string][]Operation) {
	r.operations = operations
}

// ──────────────────────────── 非导出函数 ────────────────────────────
