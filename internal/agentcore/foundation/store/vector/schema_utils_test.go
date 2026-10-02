package vector

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// ──────────────────────────── ComputeNewSchema 测试 ────────────────────────────

// TestComputeNewSchema_AddScalarField 测试添加标量字段
func TestComputeNewSchema_AddScalarField(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
	})

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
	fields := newSchema.Fields()
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
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
		NewFieldSchemaForTest("old_name", VectorDataTypeInt64, false),
	})

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
	for _, f := range newSchema.Fields() {
		if f.Name == "old_name" {
			t.Error("old_name 不应存在")
		}
	}
	if !newSchema.HasField("new_name") {
		t.Error("未找到 new_name")
	}
}

// TestComputeNewSchema_RenameField_OldNotExist 测试重命名不存在的字段
func TestComputeNewSchema_RenameField_OldNotExist(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
	})
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

// TestComputeNewSchema_RenameField_NewAlreadyExists 测试新字段名已存在
func TestComputeNewSchema_RenameField_NewAlreadyExists(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
		NewFieldSchemaForTest("old_name", VectorDataTypeInt64, false),
		NewFieldSchemaForTest("new_name", VectorDataTypeInt64, false),
	})
	op := &operation.RenameScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		OldFieldName:  "old_name",
		NewFieldName:  "new_name",
	}
	_, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err == nil {
		t.Error("新字段名已存在时应返回错误")
	}
}

// TestComputeNewSchema_UpdateFieldType 测试更新字段类型
func TestComputeNewSchema_UpdateFieldType(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
		NewFieldSchemaForTest("score", VectorDataTypeInt64, false),
	})

	op := &operation.UpdateScalarFieldTypeOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "score",
		NewFieldType:  "float",
	}

	newSchema, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	field := newSchema.GetField("score")
	if field == nil {
		t.Fatal("score 字段不存在")
	}
	if field.DType != VectorDataTypeFloat {
		t.Errorf("score DType = %v, want Float", field.DType)
	}
}

// TestComputeNewSchema_UpdateFieldType_VectorField 测试更新向量字段类型应报错
func TestComputeNewSchema_UpdateFieldType_VectorField(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
		NewFieldSchemaForTest("embedding", VectorDataTypeFloatVector, false, WithDim(128)),
	})

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

// TestComputeNewSchema_UpdateFieldType_FieldNotExist 测试更新不存在字段的类型
func TestComputeNewSchema_UpdateFieldType_FieldNotExist(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
	})

	op := &operation.UpdateScalarFieldTypeOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "nonexist",
		NewFieldType:  "float",
	}
	_, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err == nil {
		t.Error("字段不存在时应返回错误")
	}
}

// TestComputeNewSchema_UpdateVectorDim 测试更新向量维度
func TestComputeNewSchema_UpdateVectorDim(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
		NewFieldSchemaForTest("embedding", VectorDataTypeFloatVector, false, WithDim(128)),
	})

	op := &operation.UpdateEmbeddingDimensionOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "embedding",
		NewDimension:  256,
	}

	newSchema, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	field := newSchema.GetField("embedding")
	if field == nil {
		t.Fatal("embedding 字段不存在")
	}
	if field.Dim != 256 {
		t.Errorf("embedding Dim = %d, want 256", field.Dim)
	}
}

// TestComputeNewSchema_UpdateVectorDim_NotVectorField 测试更新非向量字段维度应报错
func TestComputeNewSchema_UpdateVectorDim_NotVectorField(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
		NewFieldSchemaForTest("score", VectorDataTypeInt64, false),
	})

	op := &operation.UpdateEmbeddingDimensionOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "score",
		NewDimension:  256,
	}
	_, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err == nil {
		t.Error("更新非向量字段的维度应返回错误")
	}
}

// TestComputeNewSchema_EmptyOperations 测试空操作列表
func TestComputeNewSchema_EmptyOperations(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
	})

	newSchema, err := ComputeNewSchema(oldSchema, nil)
	if err != nil {
		t.Fatalf("ComputeNewSchema 失败: %v", err)
	}
	if len(newSchema.Fields()) != 1 {
		t.Errorf("期望 1 个字段, 实际 %d", len(newSchema.Fields()))
	}
}

// TestComputeNewSchema_UnsupportedOperationType 测试未知操作类型
func TestComputeNewSchema_UnsupportedOperationType(t *testing.T) {
	oldSchema := newSchemaForTest([]*FieldSchema{
		NewFieldSchemaForTest("id", VectorDataTypeInt64, true),
	})

	op := &operation.UpdateKVOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
	}
	_, err := ComputeNewSchema(oldSchema, []operation.Operation{op})
	if err == nil {
		t.Error("不支持的向量操作类型应返回错误")
	}
}

// ──────────────────────────── BuildTransformFunc 测试 ────────────────────────────

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

// TestBuildTransformFunc_AddField_AlreadyExists 测试字段已存在时不覆盖
func TestBuildTransformFunc_AddField_AlreadyExists(t *testing.T) {
	op := &operation.AddScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "existing",
		FieldType:     "string",
		DefaultValue:  "new_val",
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"existing": "original"}
	result := transformFunc(doc)
	if result["existing"] != "original" {
		t.Errorf("existing = %v, want original（不应覆盖已存在字段）", result["existing"])
	}
}

// TestBuildTransformFunc_AddField_NilDefaultValue 测试默认值为 nil 时不设置
func TestBuildTransformFunc_AddField_NilDefaultValue(t *testing.T) {
	op := &operation.AddScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "new_field",
		FieldType:     "string",
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"id": 1}
	result := transformFunc(doc)
	if _, exists := result["new_field"]; exists {
		t.Error("DefaultValue 为 nil 时不应设置新字段")
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

// TestBuildTransformFunc_RenameField_OldNotExist 测试旧字段不存在时 no-op
func TestBuildTransformFunc_RenameField_OldNotExist(t *testing.T) {
	op := &operation.RenameScalarFieldOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		OldFieldName:  "nonexist",
		NewFieldName:  "new",
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"id": 1}
	result := transformFunc(doc)
	if _, exists := result["new"]; exists {
		t.Error("旧字段不存在时不应添加新字段")
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

// TestBuildTransformFunc_UpdateEmbeddingDim_WithRecomputeFunc 测试有 RecomputeEmbeddingFunc 时调用
func TestBuildTransformFunc_UpdateEmbeddingDim_WithRecomputeFunc(t *testing.T) {
	op := &operation.UpdateEmbeddingDimensionOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "embedding",
		NewDimension:  2,
		RecomputeEmbeddingFunc: func(doc map[string]any) []float64 {
			return []float64{0.5, 0.7}
		},
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"text": "hello"}
	result := transformFunc(doc)
	vec, ok := result["embedding"].([]float64)
	if !ok {
		t.Fatalf("embedding 类型不是 []float64")
	}
	if len(vec) != 2 {
		t.Errorf("embedding 长度 = %d, want 2", len(vec))
	}
	if vec[0] != 0.5 || vec[1] != 0.7 {
		t.Errorf("embedding = %v, want [0.5 0.7]", vec)
	}
}

// TestBuildTransformFunc_UpdateEmbeddingDim_DimensionMismatch 测试返回向量长度不匹配时用零向量 fallback
func TestBuildTransformFunc_UpdateEmbeddingDim_DimensionMismatch(t *testing.T) {
	op := &operation.UpdateEmbeddingDimensionOperation{
		BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		FieldName:     "embedding",
		NewDimension:  4,
		RecomputeEmbeddingFunc: func(doc map[string]any) []float64 {
			return []float64{1.0, 2.0} // 长度 2，但 NewDimension 是 4
		},
	}
	transformFunc := BuildTransformFunc([]operation.Operation{op})
	doc := map[string]any{"text": "hello"}
	result := transformFunc(doc)
	vec, ok := result["embedding"].([]float64)
	if !ok {
		t.Fatalf("embedding 类型不是 []float64")
	}
	if len(vec) != 4 {
		t.Errorf("embedding 长度 = %d, want 4", len(vec))
	}
	// 维度不匹配时应 fallback 为零向量
	for _, v := range vec {
		if v != 0.0 {
			t.Errorf("维度不匹配 fallback 后期望零向量, got %v", v)
		}
	}
}

// TestBuildTransformFunc_MultipleOperations 测试多个操作顺序应用
func TestBuildTransformFunc_MultipleOperations(t *testing.T) {
	ops := []operation.Operation{
		&operation.AddScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			FieldName:     "field_a",
			DefaultValue:  "val_a",
		},
		&operation.RenameScalarFieldOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			OldFieldName:  "field_a",
			NewFieldName:  "field_b",
		},
	}
	transformFunc := BuildTransformFunc(ops)
	doc := map[string]any{"id": 1}
	result := transformFunc(doc)
	if _, exists := result["field_a"]; exists {
		t.Error("field_a 应被重命名为 field_b")
	}
	if result["field_b"] != "val_a" {
		t.Errorf("field_b = %v, want val_a", result["field_b"])
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// NewFieldSchemaForTest 创建用于测试的 FieldSchema（简化参数）
func NewFieldSchemaForTest(name string, dtype VectorDataType, isPrimary bool, opts ...FieldOption) *FieldSchema {
	if isPrimary {
		opts = append([]FieldOption{WithPrimary()}, opts...)
	}
	f, err := NewFieldSchema(name, dtype, opts...)
	if err != nil {
		panic(err)
	}
	return f
}

// newSchemaForTest 创建用于测试的 CollectionSchema（简化参数）
func newSchemaForTest(fields []*FieldSchema) *CollectionSchema {
	s, err := NewCollectionSchemaFromFields(fields)
	if err != nil {
		panic(err)
	}
	return s
}
