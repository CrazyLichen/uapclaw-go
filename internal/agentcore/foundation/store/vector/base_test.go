package vector

import (
	"context"
	"fmt"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeVectorStore 用于验证 BaseVectorStore 接口可被实现
type fakeVectorStore struct{}

func (f *fakeVectorStore) CreateCollection(_ context.Context, _ string, _ *CollectionSchema, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) DeleteCollection(_ context.Context, _ string, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) CollectionExists(_ context.Context, _ string, _ ...Option) (bool, error) {
	return false, nil
}
func (f *fakeVectorStore) GetSchema(_ context.Context, _ string, _ ...Option) (*CollectionSchema, error) {
	return nil, nil
}
func (f *fakeVectorStore) AddDocs(_ context.Context, _ string, _ []map[string]any, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) Search(_ context.Context, _ string, _ []float64, _ string, _ int, _ map[string]any, _ ...Option) ([]VectorSearchResult, error) {
	return nil, nil
}
func (f *fakeVectorStore) DeleteDocsByIDs(_ context.Context, _ string, _ []string, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) DeleteDocsByFilters(_ context.Context, _ string, _ map[string]any, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) ListCollectionNames(_ context.Context) ([]string, error) {
	return nil, nil
}
func (f *fakeVectorStore) UpdateSchema(_ context.Context, _ string, _ []any, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) UpdateCollectionMetadata(_ context.Context, _ string, _ map[string]any, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) GetCollectionMetadata(_ context.Context, _ string, _ ...Option) (map[string]any, error) {
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──── VectorDataType 枚举测试 ────

// TestVectorDataType_String 验证枚举值与 Python 字符串一致
func TestVectorDataType_String(t *testing.T) {
	tests := []struct {
		dt       VectorDataType
		expected string
	}{
		{VectorDataTypeVarchar, "VARCHAR"},
		{VectorDataTypeFloatVector, "FLOAT_VECTOR"},
		{VectorDataTypeInt64, "INT64"},
		{VectorDataTypeInt32, "INT32"},
		{VectorDataTypeInt16, "INT16"},
		{VectorDataTypeInt8, "INT8"},
		{VectorDataTypeFloat, "FLOAT"},
		{VectorDataTypeDouble, "DOUBLE"},
		{VectorDataTypeBool, "BOOL"},
		{VectorDataTypeJSON, "JSON"},
		{VectorDataTypeArray, "ARRAY"},
	}
	for _, tt := range tests {
		if got := tt.dt.String(); got != tt.expected {
			t.Errorf("VectorDataType(%d).String() = %q, 期望 %q", tt.dt, got, tt.expected)
		}
	}
}

// TestVectorDataType_无效值 验证未定义枚举值的字符串输出
func TestVectorDataType_无效值(t *testing.T) {
	dt := VectorDataType(999)
	if got := dt.String(); got != "UNKNOWN(999)" {
		t.Errorf("未知枚举值 String() = %q, 期望 %q", got, "UNKNOWN(999)")
	}
}

// TestVectorDataType_所有枚举值 验证所有枚举值在有效范围内
func TestVectorDataType_所有枚举值(t *testing.T) {
	for i := VectorDataTypeVarchar; i <= VectorDataTypeArray; i++ {
		s := i.String()
		if s == "" || s == fmt.Sprintf("UNKNOWN(%d)", i) {
			t.Errorf("VectorDataType(%d) 缺少字符串表示", i)
		}
	}
}

// ──── FieldSchema 构造校验测试 ────

// TestNewFieldSchema_基本创建 验证基本字段创建
func TestNewFieldSchema_基本创建(t *testing.T) {
	field, err := NewFieldSchema("id", VectorDataTypeVarchar)
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if field.Name != "id" {
		t.Errorf("Name = %q, 期望 %q", field.Name, "id")
	}
	if field.DType != VectorDataTypeVarchar {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeVarchar)
	}
}

// TestNewFieldSchema_FloatVector必须提供Dim 验证 FLOAT_VECTOR 类型缺少 dim 时返回错误
func TestNewFieldSchema_FloatVector必须提供Dim(t *testing.T) {
	_, err := NewFieldSchema("embedding", VectorDataTypeFloatVector)
	if err == nil {
		t.Error("FLOAT_VECTOR 缺少 dim 应返回错误")
	}
}

// TestNewFieldSchema_FloatVectorDim为零 验证 dim=0 时返回错误
func TestNewFieldSchema_FloatVectorDim为零(t *testing.T) {
	_, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(0))
	if err == nil {
		t.Error("FLOAT_VECTOR dim=0 应返回错误")
	}
}

// TestNewFieldSchema_FloatVectorDim为负 验证 dim<0 时返回错误
func TestNewFieldSchema_FloatVectorDim为负(t *testing.T) {
	_, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(-1))
	if err == nil {
		t.Error("FLOAT_VECTOR dim<0 应返回错误")
	}
}

// TestNewFieldSchema_FloatVectorDim合法 验证合法 dim 创建成功
func TestNewFieldSchema_FloatVectorDim合法(t *testing.T) {
	field, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if field.Dim != 768 {
		t.Errorf("Dim = %d, 期望 %d", field.Dim, 768)
	}
}

// TestNewFieldSchema_WithOptions 验证 FieldOption 全部生效
func TestNewFieldSchema_WithOptions(t *testing.T) {
	field, err := NewFieldSchema("id", VectorDataTypeVarchar,
		WithPrimary(),
		WithAutoID(),
		WithMaxLength(256),
		WithFieldDescription("主键字段"),
	)
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if !field.IsPrimary {
		t.Error("IsPrimary 应为 true")
	}
	if !field.AutoID {
		t.Error("AutoID 应为 true")
	}
	if field.MaxLength != 256 {
		t.Errorf("MaxLength = %d, 期望 %d", field.MaxLength, 256)
	}
	if field.Description != "主键字段" {
		t.Errorf("Description = %q, 期望 %q", field.Description, "主键字段")
	}
}

// TestNewFieldSchema_名字为空 验证名字为空时返回错误
func TestNewFieldSchema_名字为空(t *testing.T) {
	_, err := NewFieldSchema("", VectorDataTypeVarchar)
	if err == nil {
		t.Error("名字为空应返回错误")
	}
}

// TestNewFieldSchema_ArrayWithElementType 验证 ARRAY 类型带元素类型
func TestNewFieldSchema_ArrayWithElementType(t *testing.T) {
	field, err := NewFieldSchema("tags", VectorDataTypeArray,
		WithElementType(VectorDataTypeVarchar),
		WithMaxCapacity(10),
	)
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if field.ElementType != VectorDataTypeVarchar {
		t.Errorf("ElementType = %v, 期望 %v", field.ElementType, VectorDataTypeVarchar)
	}
	if field.MaxCapacity != 10 {
		t.Errorf("MaxCapacity = %d, 期望 %d", field.MaxCapacity, 10)
	}
}

// TestNewFieldSchema_NonFloatVectorDim 验证非 FLOAT_VECTOR 类型设置 dim 不报错
func TestNewFieldSchema_NonFloatVectorDim(t *testing.T) {
	field, err := NewFieldSchema("count", VectorDataTypeInt32, WithDim(100))
	if err != nil {
		t.Fatalf("非 FLOAT_VECTOR 带 dim 不应报错: %v", err)
	}
	if field.Dim != 100 {
		t.Errorf("Dim = %d, 期望 %d", field.Dim, 100)
	}
}

// ──── FieldSchema 序列化测试 ────

// TestFieldSchema_ToDict_完整字段 验证所有非零值字段序列化
func TestFieldSchema_ToDict_完整字段(t *testing.T) {
	field, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector,
		WithDim(768), WithFieldDescription("向量字段"))
	got := field.ToDict()

	if got["name"] != "embedding" {
		t.Errorf("name = %v, 期望 %q", got["name"], "embedding")
	}
	if got["type"] != "FLOAT_VECTOR" {
		t.Errorf("type = %v, 期望 %q", got["type"], "FLOAT_VECTOR")
	}
	if got["dim"] != 768 {
		t.Errorf("dim = %v, 期望 %v", got["dim"], 768)
	}
	if got["description"] != "向量字段" {
		t.Errorf("description = %v, 期望 %q", got["description"], "向量字段")
	}
}

// TestFieldSchema_ToDict_主键字段 验证主键和 auto_id 序列化
func TestFieldSchema_ToDict_主键字段(t *testing.T) {
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar,
		WithPrimary(), WithAutoID(), WithMaxLength(256))
	got := field.ToDict()

	if got["is_primary"] != true {
		t.Error("is_primary 应为 true")
	}
	if got["auto_id"] != true {
		t.Error("auto_id 应为 true")
	}
	if got["max_length"] != 256 {
		t.Errorf("max_length = %v, 期望 %v", got["max_length"], 256)
	}
}

// TestFieldSchema_ToDict_零值省略 验证零值字段不序列化
func TestFieldSchema_ToDict_零值省略(t *testing.T) {
	field, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	got := field.ToDict()

	if _, ok := got["is_primary"]; ok {
		t.Error("IsPrimary=false 不应序列化")
	}
	if _, ok := got["auto_id"]; ok {
		t.Error("AutoID=false 不应序列化")
	}
	if _, ok := got["dim"]; ok {
		t.Error("Dim=0 不应序列化")
	}
}

// TestFieldSchema_ToDict_MaxLength默认值 验证未设置 MaxLength 时 VARCHAR 序列化为默认 65535
func TestFieldSchema_ToDict_MaxLength默认值(t *testing.T) {
	field, _ := NewFieldSchema("content", VectorDataTypeVarchar)
	got := field.ToDict()

	if got["max_length"] != defaultMaxLength {
		t.Errorf("max_length = %v, 期望 %v", got["max_length"], defaultMaxLength)
	}
}

// TestFieldSchema_ToDict_Array字段 验证 ARRAY 字段序列化包含 element_type 和 max_capacity
func TestFieldSchema_ToDict_Array字段(t *testing.T) {
	field, _ := NewFieldSchema("tags", VectorDataTypeArray,
		WithElementType(VectorDataTypeInt64),
		WithMaxCapacity(20),
	)
	got := field.ToDict()
	if got["element_type"] != "INT64" {
		t.Errorf("element_type = %v, 期望 %q", got["element_type"], "INT64")
	}
	if got["max_capacity"] != 20 {
		t.Errorf("max_capacity = %v, 期望 %d", got["max_capacity"], 20)
	}
}

// TestFieldSchema_ToDict_DefaultValue 验证 DefaultValue 序列化
func TestFieldSchema_ToDict_DefaultValue(t *testing.T) {
	field, _ := NewFieldSchema("status", VectorDataTypeVarchar,
		WithDefaultValue("active"),
	)
	got := field.ToDict()
	if got["default_value"] != "active" {
		t.Errorf("default_value = %v, 期望 %q", got["default_value"], "active")
	}
}

// TestFieldFromDict_基本反序列化 验证从字典创建 FieldSchema
func TestFieldFromDict_基本反序列化(t *testing.T) {
	data := map[string]any{
		"name":       "embedding",
		"type":       "FLOAT_VECTOR",
		"dim":        768,
		"is_primary": false,
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if field.Name != "embedding" {
		t.Errorf("Name = %q, 期望 %q", field.Name, "embedding")
	}
	if field.DType != VectorDataTypeFloatVector {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeFloatVector)
	}
	if field.Dim != 768 {
		t.Errorf("Dim = %d, 期望 %d", field.Dim, 768)
	}
}

// TestFieldFromDict_主键字段 验证主键字段反序列化
func TestFieldFromDict_主键字段(t *testing.T) {
	data := map[string]any{
		"name":       "id",
		"type":       "VARCHAR",
		"is_primary": true,
		"auto_id":    true,
		"max_length": 256,
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if !field.IsPrimary {
		t.Error("IsPrimary 应为 true")
	}
	if !field.AutoID {
		t.Error("AutoID 应为 true")
	}
	if field.MaxLength != 256 {
		t.Errorf("MaxLength = %d, 期望 %d", field.MaxLength, 256)
	}
}

// TestFieldFromDict_dtype兼容 验证 "dtype" 键也能解析（Python 兼容）
func TestFieldFromDict_dtype兼容(t *testing.T) {
	data := map[string]any{
		"name":  "text",
		"dtype": "VARCHAR",
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if field.DType != VectorDataTypeVarchar {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeVarchar)
	}
}

// TestFieldFromDict_缺少名字 验证缺少 name 时返回错误
func TestFieldFromDict_缺少名字(t *testing.T) {
	data := map[string]any{
		"type": "VARCHAR",
	}
	_, err := FieldFromDict(data)
	if err == nil {
		t.Error("缺少 name 应返回错误")
	}
}

// TestFieldFromDict_Array字段 验证 ARRAY 字段反序列化
func TestFieldFromDict_Array字段(t *testing.T) {
	data := map[string]any{
		"name":         "tags",
		"type":         "ARRAY",
		"element_type": "INT64",
		"max_capacity": 20,
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if field.DType != VectorDataTypeArray {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeArray)
	}
	if field.ElementType != VectorDataTypeInt64 {
		t.Errorf("ElementType = %v, 期望 %v", field.ElementType, VectorDataTypeInt64)
	}
	if field.MaxCapacity != 20 {
		t.Errorf("MaxCapacity = %d, 期望 %d", field.MaxCapacity, 20)
	}
}

// TestFieldSchema_序列化往返 验证 ToDict → FieldFromDict 往返一致性
func TestFieldSchema_序列化往返(t *testing.T) {
	original, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector,
		WithDim(768), WithFieldDescription("测试"))
	dict := original.ToDict()
	restored, err := FieldFromDict(dict)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if restored.Name != original.Name {
		t.Errorf("Name 往返不一致: %q vs %q", restored.Name, original.Name)
	}
	if restored.DType != original.DType {
		t.Errorf("DType 往返不一致: %v vs %v", restored.DType, original.DType)
	}
	if restored.Dim != original.Dim {
		t.Errorf("Dim 往返不一致: %d vs %d", restored.Dim, original.Dim)
	}
	if restored.Description != original.Description {
		t.Errorf("Description 往返不一致: %q vs %q", restored.Description, original.Description)
	}
}

// ──── CollectionSchema 测试 ────

// TestNewCollectionSchema_空创建 验证空 Schema 创建成功
func TestNewCollectionSchema_空创建(t *testing.T) {
	schema, err := NewCollectionSchema()
	if err != nil {
		t.Fatalf("NewCollectionSchema() 返回错误: %v", err)
	}
	if len(schema.fields) != 0 {
		t.Errorf("fields 长度 = %d, 期望 0", len(schema.fields))
	}
}

// TestNewCollectionSchema_带选项 验证 CollectionOption 生效
func TestNewCollectionSchema_带选项(t *testing.T) {
	schema, err := NewCollectionSchema(
		WithCollectionDescription("测试集合"),
		WithEnableDynamicField(),
	)
	if err != nil {
		t.Fatalf("NewCollectionSchema() 返回错误: %v", err)
	}
	if schema.Description != "测试集合" {
		t.Errorf("Description = %q, 期望 %q", schema.Description, "测试集合")
	}
	if !schema.EnableDynamicField {
		t.Error("EnableDynamicField 应为 true")
	}
}

// TestCollectionSchema_AddField_正常添加 验证字段添加成功
func TestCollectionSchema_AddField_正常添加(t *testing.T) {
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	_, err := schema.AddField(idField)
	if err != nil {
		t.Fatalf("AddField() 返回错误: %v", err)
	}
	if len(schema.fields) != 1 {
		t.Errorf("fields 长度 = %d, 期望 1", len(schema.fields))
	}
}

// TestCollectionSchema_AddField_链式调用 验证 AddField 返回自身支持链式调用
func TestCollectionSchema_AddField_链式调用(t *testing.T) {
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	_, err := schema.AddField(idField)
	if err != nil {
		t.Fatalf("第一个 AddField() 返回错误: %v", err)
	}
	_, err = schema.AddField(embedField)
	if err != nil {
		t.Fatalf("链式 AddField() 返回错误: %v", err)
	}
	if len(schema.fields) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(schema.fields))
	}
}

// TestCollectionSchema_AddField_重复名字 验证重复字段名返回错误
func TestCollectionSchema_AddField_重复名字(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field1, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	field2, _ := NewFieldSchema("id", VectorDataTypeVarchar)
	_, _ = schema.AddField(field1)
	_, err := schema.AddField(field2)
	if err == nil {
		t.Error("重复字段名应返回错误")
	}
}

// TestCollectionSchema_AddField_重复主键 验证添加第二个主键返回错误
func TestCollectionSchema_AddField_重复主键(t *testing.T) {
	schema, _ := NewCollectionSchema()
	pk1, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	pk2, _ := NewFieldSchema("id2", VectorDataTypeVarchar, WithPrimary())
	_, _ = schema.AddField(pk1)
	_, err := schema.AddField(pk2)
	if err == nil {
		t.Error("第二个主键应返回错误")
	}
}

// TestCollectionSchema_RemoveField 验证字段移除
func TestCollectionSchema_RemoveField(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	_, _ = schema.AddField(field)
	schema.RemoveField("id")
	if schema.HasField("id") {
		t.Error("移除后 HasField 应返回 false")
	}
}

// TestCollectionSchema_RemoveField_链式 验证 RemoveField 支持链式调用
func TestCollectionSchema_RemoveField_链式(t *testing.T) {
	schema, _ := NewCollectionSchema()
	f1, _ := NewFieldSchema("a", VectorDataTypeVarchar)
	f2, _ := NewFieldSchema("b", VectorDataTypeVarchar)
	_, _ = schema.AddField(f1)
	_, _ = schema.AddField(f2)
	result := schema.RemoveField("a")
	if result != schema {
		t.Error("RemoveField 应返回自身")
	}
	if len(schema.fields) != 1 {
		t.Errorf("fields 长度 = %d, 期望 1", len(schema.fields))
	}
}

// TestCollectionSchema_GetField 验证字段获取
func TestCollectionSchema_GetField(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	_, _ = schema.AddField(field)
	got := schema.GetField("embedding")
	if got == nil {
		t.Fatal("GetField 返回 nil")
	}
	if got.Name != "embedding" {
		t.Errorf("Name = %q, 期望 %q", got.Name, "embedding")
	}
}

// TestCollectionSchema_GetField_不存在 验证不存在字段返回 nil
func TestCollectionSchema_GetField_不存在(t *testing.T) {
	schema, _ := NewCollectionSchema()
	if got := schema.GetField("no_such"); got != nil {
		t.Error("不存在字段应返回 nil")
	}
}

// TestCollectionSchema_GetPrimaryKeyField 验证获取主键
func TestCollectionSchema_GetPrimaryKeyField(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	_, _ = schema.AddField(field)
	pk := schema.GetPrimaryKeyField()
	if pk == nil {
		t.Fatal("主键字段不应为 nil")
	}
	if pk.Name != "id" {
		t.Errorf("主键 Name = %q, 期望 %q", pk.Name, "id")
	}
}

// TestCollectionSchema_GetPrimaryKeyField_无主键 验证无主键时返回 nil
func TestCollectionSchema_GetPrimaryKeyField_无主键(t *testing.T) {
	schema, _ := NewCollectionSchema()
	if pk := schema.GetPrimaryKeyField(); pk != nil {
		t.Error("无主键时应返回 nil")
	}
}

// TestCollectionSchema_GetVectorFields 验证获取向量字段
func TestCollectionSchema_GetVectorFields(t *testing.T) {
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	_, _ = schema.AddField(idField)
	_, _ = schema.AddField(embedField)
	vf := schema.GetVectorFields()
	if len(vf) != 1 {
		t.Fatalf("向量字段数量 = %d, 期望 1", len(vf))
	}
	if vf[0].Name != "embedding" {
		t.Errorf("向量字段 Name = %q, 期望 %q", vf[0].Name, "embedding")
	}
}

// TestCollectionSchema_初始带主键冲突 验证 NewCollectionSchemaFromFields 传入多个主键字段返回错误
func TestCollectionSchema_初始带主键冲突(t *testing.T) {
	pk1, _ := NewFieldSchema("id1", VectorDataTypeVarchar, WithPrimary())
	pk2, _ := NewFieldSchema("id2", VectorDataTypeVarchar, WithPrimary())
	_, err := NewCollectionSchemaFromFields([]*FieldSchema{pk1, pk2})
	if err == nil {
		t.Error("多个主键字段应返回错误")
	}
}

// TestCollectionSchema_Fields 验证 Fields() 返回副本
func TestCollectionSchema_Fields(t *testing.T) {
	schema, _ := NewCollectionSchema()
	f1, _ := NewFieldSchema("a", VectorDataTypeVarchar)
	_, _ = schema.AddField(f1)
	fields := schema.Fields()
	if len(fields) != 1 {
		t.Errorf("Fields() 长度 = %d, 期望 1", len(fields))
	}
	// 修改副本不应影响原对象
	fields[0] = nil
	if schema.GetField("a") == nil {
		t.Error("修改副本不应影响原对象")
	}
}

// ──── CollectionSchema 序列化测试 ────

// TestCollectionSchema_ToDict 验证序列化输出
func TestCollectionSchema_ToDict(t *testing.T) {
	schema, _ := NewCollectionSchema(
		WithCollectionDescription("测试"),
		WithEnableDynamicField(),
	)
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	_, _ = schema.AddField(idField)
	_, _ = schema.AddField(embedField)

	got := schema.ToDict()
	if got["description"] != "测试" {
		t.Errorf("description = %v, 期望 %q", got["description"], "测试")
	}
	if got["enable_dynamic_field"] != true {
		t.Error("enable_dynamic_field 应为 true")
	}
	// ToDict 返回 []map[string]any，存入 map[string]any 后运行时类型不变
	fieldsRaw, ok := got["fields"].([]map[string]any)
	if !ok {
		t.Fatalf("fields 类型不正确: %T", got["fields"])
	}
	if len(fieldsRaw) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(fieldsRaw))
	}
}

// TestCollectionFromDict_基本反序列化 验证从字典创建 CollectionSchema
func TestCollectionFromDict_基本反序列化(t *testing.T) {
	data := map[string]any{
		"fields": []map[string]any{
			{"name": "id", "type": "VARCHAR", "is_primary": true, "max_length": 256},
			{"name": "embedding", "type": "FLOAT_VECTOR", "dim": 768},
		},
		"description":          "测试集合",
		"enable_dynamic_field": true,
	}
	schema, err := CollectionFromDict(data)
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if schema.Description != "测试集合" {
		t.Errorf("Description = %q, 期望 %q", schema.Description, "测试集合")
	}
	if !schema.EnableDynamicField {
		t.Error("EnableDynamicField 应为 true")
	}
	if len(schema.fields) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(schema.fields))
	}
	pk := schema.GetPrimaryKeyField()
	if pk == nil || pk.Name != "id" {
		t.Error("主键字段应为 id")
	}
}

// TestCollectionFromDict_空字典 验证空字典创建空 Schema
func TestCollectionFromDict_空字典(t *testing.T) {
	schema, err := CollectionFromDict(map[string]any{})
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if len(schema.fields) != 0 {
		t.Errorf("fields 长度 = %d, 期望 0", len(schema.fields))
	}
}

// TestCollectionSchema_序列化往返 验证 ToDict → CollectionFromDict 往返一致性
func TestCollectionSchema_序列化往返(t *testing.T) {
	original, _ := NewCollectionSchema(
		WithCollectionDescription("往返测试"),
		WithEnableDynamicField(),
	)
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	_, _ = original.AddField(idField)
	_, _ = original.AddField(embedField)

	dict := original.ToDict()
	restored, err := CollectionFromDict(dict)
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if restored.Description != original.Description {
		t.Errorf("Description 往返不一致: %q vs %q", restored.Description, original.Description)
	}
	if restored.EnableDynamicField != original.EnableDynamicField {
		t.Errorf("EnableDynamicField 往返不一致: %v vs %v", restored.EnableDynamicField, original.EnableDynamicField)
	}
	if len(restored.fields) != len(original.fields) {
		t.Errorf("fields 长度往返不一致: %d vs %d", len(restored.fields), len(original.fields))
	}
}

// TestCollectionFromDict_JSON格式 验证 []any 格式（JSON 反序列化产物）也能解析
func TestCollectionFromDict_JSON格式(t *testing.T) {
	data := map[string]any{
		"fields": []any{
			map[string]any{"name": "id", "type": "VARCHAR", "is_primary": true},
			map[string]any{"name": "embedding", "type": "FLOAT_VECTOR", "dim": 768},
		},
	}
	schema, err := CollectionFromDict(data)
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if len(schema.fields) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(schema.fields))
	}
}

// ──── VectorSearchResult 测试 ────

// TestVectorSearchResult_字段 验证搜索结果字段赋值
func TestVectorSearchResult_字段(t *testing.T) {
	result := VectorSearchResult{
		Score: 0.95,
		Fields: map[string]any{
			"id":   "doc_1",
			"text": "测试文档",
		},
	}
	if result.Score != 0.95 {
		t.Errorf("Score = %f, 期望 0.95", result.Score)
	}
	if result.Fields["id"] != "doc_1" {
		t.Errorf("Fields[id] = %v, 期望 %q", result.Fields["id"], "doc_1")
	}
}

// ──── BaseVectorStore 接口测试 ────

// TestBaseVectorStore_接口满足 验证 fakeVectorStore 满足 BaseVectorStore 接口
func TestBaseVectorStore_接口满足(t *testing.T) {
	var _ BaseVectorStore = (*fakeVectorStore)(nil)
}

// ──── Options 测试 ────

// TestOptions_全部选项 验证 Options 函数选项全部生效
func TestOptions_全部选项(t *testing.T) {
	o := newOptions(
		WithDistanceMetric("COSINE"),
		WithBatchSize(100),
		WithOutputFields("id", "text"),
	)
	if o.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %q, 期望 %q", o.DistanceMetric, "COSINE")
	}
	if o.BatchSize != 100 {
		t.Errorf("BatchSize = %d, 期望 %d", o.BatchSize, 100)
	}
	if len(o.OutputFields) != 2 {
		t.Errorf("OutputFields 长度 = %d, 期望 2", len(o.OutputFields))
	}
}

// TestOptions_空选项 验证零值 Options
func TestOptions_空选项(t *testing.T) {
	o := newOptions()
	if o.DistanceMetric != "" {
		t.Errorf("DistanceMetric = %q, 期望空字符串", o.DistanceMetric)
	}
	if o.BatchSize != 0 {
		t.Errorf("BatchSize = %d, 期望 0", o.BatchSize)
	}
	if o.OutputFields != nil {
		t.Errorf("OutputFields = %v, 期望 nil", o.OutputFields)
	}
}
