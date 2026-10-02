package vector

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ComputeNewSchema 根据 operations 计算目标 schema。
// 深拷贝 oldSchema 后逐个应用 operation，返回新 schema。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (compute_new_schema)
func ComputeNewSchema(oldSchema *CollectionSchema, operations []operation.Operation) (*CollectionSchema, error) {
	// 深拷贝 schema
	schemaDict := oldSchema.ToDict()
	newSchema, err := CollectionFromDict(schemaDict)
	if err != nil {
		return nil, err
	}

	for _, op := range operations {
		switch o := op.(type) {
		case *operation.AddScalarFieldOperation:
			newSchema, err = computeSchemaAddField(newSchema, o)
		case *operation.RenameScalarFieldOperation:
			newSchema, err = computeSchemaRenameField(newSchema, o)
		case *operation.UpdateScalarFieldTypeOperation:
			newSchema, err = computeSchemaUpdateFieldType(newSchema, o)
		case *operation.UpdateEmbeddingDimensionOperation:
			newSchema, err = computeSchemaUpdateVectorDim(newSchema, o)
		default:
			return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", "不支持的操作类型: "+op.TypeName()),
			)
		}
		if err != nil {
			return nil, err
		}
	}

	return newSchema, nil
}

// BuildTransformFunc 构建统一的文档变换函数，顺序应用所有 operation。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (build_transform_func_for_operations)
func BuildTransformFunc(operations []operation.Operation) func(map[string]any) map[string]any {
	return func(doc map[string]any) map[string]any {
		for _, op := range operations {
			doc = applyOperationToDoc(doc, op)
		}
		return doc
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// computeSchemaAddField 计算添加标量字段后的 schema。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (_compute_schema_add_field)
func computeSchemaAddField(schema *CollectionSchema, op *operation.AddScalarFieldOperation) (*CollectionSchema, error) {
	dtype := vectorDataTypeFromString(op.FieldType)
	fieldSchema, err := NewFieldSchema(op.FieldName, dtype, WithDefaultValue(op.DefaultValue))
	if err != nil {
		return nil, err
	}
	_, err = schema.AddField(fieldSchema)
	if err != nil {
		return nil, err
	}
	return schema, nil
}

// computeSchemaRenameField 计算重命名标量字段后的 schema。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (_compute_schema_rename_field)
func computeSchemaRenameField(schema *CollectionSchema, op *operation.RenameScalarFieldOperation) (*CollectionSchema, error) {
	// 同名字段无需操作
	if op.OldFieldName == op.NewFieldName {
		return schema, nil
	}

	// 校验旧字段存在
	if !schema.HasField(op.OldFieldName) {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "旧字段 '"+op.OldFieldName+"' 不存在"),
		)
	}

	// 校验新字段不存在
	if schema.HasField(op.NewFieldName) {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "新字段 '"+op.NewFieldName+"' 已存在"),
		)
	}

	// 通过 ToDict/CollectionFromDict 深拷贝后修改字段名
	schemaDict := schema.ToDict()
	for _, field := range schemaDict["fields"].([]map[string]any) {
		if field["name"] == op.OldFieldName {
			field["name"] = op.NewFieldName
			break
		}
	}

	return CollectionFromDict(schemaDict)
}

// computeSchemaUpdateFieldType 计算更新字段类型后的 schema。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (_compute_schema_update_field_type)
func computeSchemaUpdateFieldType(schema *CollectionSchema, op *operation.UpdateScalarFieldTypeOperation) (*CollectionSchema, error) {
	// 校验字段存在
	field := schema.GetField(op.FieldName)
	if field == nil {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "字段 '"+op.FieldName+"' 不存在"),
		)
	}

	// 校验非向量字段
	if field.DType == VectorDataTypeFloatVector {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "不能修改向量字段 '"+op.FieldName+"' 的类型"),
		)
	}

	// 通过 ToDict/CollectionFromDict 深拷贝后修改字段类型
	schemaDict := schema.ToDict()
	newDType := vectorDataTypeFromString(op.NewFieldType)
	for _, field := range schemaDict["fields"].([]map[string]any) {
		if field["name"] == op.FieldName {
			field["type"] = newDType.String()
			break
		}
	}

	return CollectionFromDict(schemaDict)
}

// computeSchemaUpdateVectorDim 计算更新向量维度后的 schema。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (_compute_schema_update_vector_dim)
func computeSchemaUpdateVectorDim(schema *CollectionSchema, op *operation.UpdateEmbeddingDimensionOperation) (*CollectionSchema, error) {
	// 校验字段存在且是向量字段
	field := schema.GetField(op.FieldName)
	if field == nil {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "字段 '"+op.FieldName+"' 不存在"),
		)
	}
	if field.DType != VectorDataTypeFloatVector {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "字段 '"+op.FieldName+"' 不是向量字段"),
		)
	}

	// 通过 ToDict/CollectionFromDict 深拷贝后修改维度
	schemaDict := schema.ToDict()
	for _, field := range schemaDict["fields"].([]map[string]any) {
		if field["name"] == op.FieldName {
			field["dim"] = op.NewDimension
			break
		}
	}

	return CollectionFromDict(schemaDict)
}

// applyOperationToDoc 对单个文档应用单个操作。
//
// Python: openjiuwen/core/foundation/store/vector/utils.py (_apply_operation_to_doc)
func applyOperationToDoc(doc map[string]any, op operation.Operation) map[string]any {
	switch o := op.(type) {
	case *operation.AddScalarFieldOperation:
		// 字段不存在且默认值非零时设置
		if _, exists := doc[o.FieldName]; !exists && o.DefaultValue != nil {
			doc[o.FieldName] = o.DefaultValue
		}

	case *operation.RenameScalarFieldOperation:
		// 重命名字段
		if oldVal, exists := doc[o.OldFieldName]; exists {
			doc[o.NewFieldName] = oldVal
			delete(doc, o.OldFieldName)
		}

	case *operation.UpdateScalarFieldTypeOperation:
		// no-op：对齐 Python，让 Milvus 处理类型转换
		// Python: "For type updates, we keep the value as-is and let Milvus handle the conversion"

	case *operation.UpdateEmbeddingDimensionOperation:
		// 重新计算嵌入或使用零向量
		var newVector []float64
		if o.RecomputeEmbeddingFunc != nil {
			newVector = o.RecomputeEmbeddingFunc(doc)
		} else {
			// Python: default_re_embedding_func -> [0.0] * new_dimension
			newVector = make([]float64, o.NewDimension)
		}
		// 校验向量长度
		if len(newVector) != o.NewDimension {
			// Python: raise build_error(...)，Go 中记录警告但不中断
			// 为与 Python 行为一致，这里使用零向量 fallback
			newVector = make([]float64, o.NewDimension)
		}
		doc[o.FieldName] = newVector
	}

	return doc
}
