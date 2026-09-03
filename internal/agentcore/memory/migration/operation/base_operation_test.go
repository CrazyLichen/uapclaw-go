package operation

import (
	"testing"
)

// TestOperationMetadata_字段 测试 OperationMetadata 字段赋值
func TestOperationMetadata_字段(t *testing.T) {
	meta := OperationMetadata{SchemaVersion: 3, Description: "测试操作"}
	if meta.SchemaVersion != 3 {
		t.Errorf("SchemaVersion = %d, want 3", meta.SchemaVersion)
	}
	if meta.Description != "测试操作" {
		t.Errorf("Description = %q, want %q", meta.Description, "测试操作")
	}
}

// TestBaseOperation_SchemaVersion 测试 BaseOperation.SchemaVersion()
func TestBaseOperation_SchemaVersion(t *testing.T) {
	op := BaseOperation{Metadata: OperationMetadata{SchemaVersion: 5}}
	if op.SchemaVersion() != 5 {
		t.Errorf("SchemaVersion() = %d, want 5", op.SchemaVersion())
	}
}

// TestBaseOperation_Description_有值 测试 Description 有值
func TestBaseOperation_Description_有值(t *testing.T) {
	op := BaseOperation{Metadata: OperationMetadata{Description: "添加列"}}
	if op.Description() != "添加列" {
		t.Errorf("Description() = %q, want %q", op.Description(), "添加列")
	}
}

// TestBaseOperation_Description_空值 测试 Description 为空时返回类型名
func TestBaseOperation_Description_空值(t *testing.T) {
	op := BaseOperation{Metadata: OperationMetadata{}}
	// BaseOperation.Description() 空值时返回 "BaseOperation"
	if op.Description() != "BaseOperation" {
		t.Errorf("Description() = %q, want %q", op.Description(), "BaseOperation")
	}
}

// TestBaseOperation_接口满足 编译时校验 BaseOperation 满足 Operation 接口
func TestBaseOperation_接口满足(t *testing.T) {
	var _ Operation = (*BaseOperation)(nil)
}
