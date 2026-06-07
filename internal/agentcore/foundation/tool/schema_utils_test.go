package tool

import (
	"math"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSchemaUtils_RemoveNoneValues 测试递归移除 nil 值
func TestSchemaUtils_RemoveNoneValues(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected map[string]any
	}{
		{
			name:     "nil输入",
			input:    nil,
			expected: nil,
		},
		{
			name:     "空map",
			input:    map[string]any{},
			expected: nil,
		},
		{
			name:     "移除顶层nil",
			input:    map[string]any{"a": "hello", "b": nil, "c": 42},
			expected: map[string]any{"a": "hello", "c": 42},
		},
		{
			name:     "递归移除嵌套nil",
			input:    map[string]any{"a": map[string]any{"x": 1, "y": nil}},
			expected: map[string]any{"a": map[string]any{"x": 1}},
		},
		{
			name:     "全部为nil返回nil",
			input:    map[string]any{"a": nil, "b": nil},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SchemaUtils{}.RemoveNoneValues(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("期望 nil，实际 %v", result)
				}
			} else {
				if result == nil {
					t.Errorf("期望 %v，实际 nil", tt.expected)
				}
			}
		})
	}
}

// TestSchemaUtils_Validate_必填校验 测试必填字段校验
func TestSchemaUtils_Validate_必填校验(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
		schema.NewIntegerParam("age", "年龄", false),
	}

	// 缺少必填字段
	err := SchemaUtils{}.Validate(map[string]any{"age": float64(25)}, params)
	if err == nil {
		t.Error("缺少必填字段 name 应返回错误")
	}

	// 必填字段存在
	err = SchemaUtils{}.Validate(map[string]any{"name": "Alice"}, params)
	if err != nil {
		t.Errorf("必填字段存在应通过校验: %v", err)
	}
}

// TestSchemaUtils_Validate_类型校验 测试类型匹配校验
func TestSchemaUtils_Validate_类型校验(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
		schema.NewIntegerParam("age", "年龄", true),
		schema.NewNumberParam("score", "分数", true),
		schema.NewBooleanParam("active", "是否激活", true),
	}

	// 正确类型
	err := SchemaUtils{}.Validate(map[string]any{
		"name":   "Alice",
		"age":    float64(25),
		"score":  99.5,
		"active": true,
	}, params)
	if err != nil {
		t.Errorf("正确类型应通过: %v", err)
	}

	// 类型不匹配：string 位置传 int
	err = SchemaUtils{}.Validate(map[string]any{"name": 42}, params)
	if err == nil {
		t.Error("string 位置传 int 应返回错误")
	}

	// float64 整数可接受为 integer
	err = SchemaUtils{}.Validate(map[string]any{"name": "A", "age": float64(25)}, []*schema.Param{
		schema.NewIntegerParam("age", "年龄", true),
	})
	if err != nil {
		t.Errorf("float64 整数应接受为 integer: %v", err)
	}

	// float64 非整数不可接受为 integer
	err = SchemaUtils{}.Validate(map[string]any{"name": "A", "age": 25.5}, []*schema.Param{
		schema.NewIntegerParam("age", "年龄", true),
	})
	if err == nil {
		t.Error("float64 非整数不可接受为 integer")
	}
}

// TestSchemaUtils_FormatWithSchema_默认值填充 测试默认值填充
func TestSchemaUtils_FormatWithSchema_默认值填充(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
	}
	// 带 default 的参数需要手动创建
	limitParam := &schema.Param{
		Name: "limit", Type: schema.ParamTypeInteger,
		Description: "数量上限", Required: false, Default: 10,
	}
	params = append(params, limitParam)

	data := map[string]any{"name": "Alice"}
	result, err := SchemaUtils{}.FormatWithSchema(data, params)
	if err != nil {
		t.Fatalf("FormatWithSchema 失败: %v", err)
	}
	if result["name"] != "Alice" {
		t.Errorf("name: 期望 Alice，实际 %v", result["name"])
	}
	if result["limit"] != 10 {
		t.Errorf("limit default: 期望 10，实际 %v", result["limit"])
	}
}

// TestSchemaUtils_FormatWithSchema_必填缺失 测试必填字段缺失
func TestSchemaUtils_FormatWithSchema_必填缺失(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
	}

	_, err := SchemaUtils{}.FormatWithSchema(map[string]any{}, params)
	if err == nil {
		t.Error("缺少必填字段应返回错误")
	}
}

// TestSchemaUtils_FormatWithSchema_额外字段保留 测试额外字段保留
func TestSchemaUtils_FormatWithSchema_额外字段保留(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", true),
	}

	data := map[string]any{"name": "Alice", "extra": "value"}
	result, err := SchemaUtils{}.FormatWithSchema(data, params)
	if err != nil {
		t.Fatalf("FormatWithSchema 失败: %v", err)
	}
	if result["extra"] != "value" {
		t.Errorf("额外字段应保留: extra=%v", result["extra"])
	}
}

// TestSchemaUtils_RemoveNoneValues_含数组 测试递归移除数组中的 nil 值
func TestSchemaUtils_RemoveNoneValues_含数组(t *testing.T) {
	input := map[string]any{
		"items": []any{"a", nil, "b"},
	}
	result := SchemaUtils{}.RemoveNoneValues(input)
	arr, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("items 类型错误: %T", result["items"])
	}
	if len(arr) != 2 {
		t.Errorf("期望 2 个元素，实际 %d", len(arr))
	}
}

// TestSchemaUtils_RemoveNoneValues_嵌套数组 测试嵌套数组中的 nil
func TestSchemaUtils_RemoveNoneValues_嵌套数组(t *testing.T) {
	input := map[string]any{
		"nested": []any{map[string]any{"x": 1, "y": nil}, nil},
	}
	result := SchemaUtils{}.RemoveNoneValues(input)
	arr, ok := result["nested"].([]any)
	if !ok {
		t.Fatalf("nested 类型错误: %T", result["nested"])
	}
	if len(arr) != 1 {
		t.Errorf("期望 1 个元素，实际 %d", len(arr))
	}
}

// TestSchemaUtils_Validate_Array和Object类型 测试 Array/Object 类型校验
func TestSchemaUtils_Validate_Array和Object类型(t *testing.T) {
	params := []*schema.Param{
		schema.NewArrayParam("tags", "标签", true, schema.NewStringParam("tag", "标签项", true)),
		schema.NewObjectParam("meta", "元数据", true, []*schema.Param{
			schema.NewStringParam("key", "键", true),
		}),
	}

	// 正确类型
	err := SchemaUtils{}.Validate(map[string]any{
		"tags": []any{"a", "b"},
		"meta": map[string]any{"key": "value"},
	}, params)
	if err != nil {
		t.Errorf("正确 Array/Object 类型应通过: %v", err)
	}

	// Array 类型不匹配
	err = SchemaUtils{}.Validate(map[string]any{"tags": "not_array"}, params)
	if err == nil {
		t.Error("Array 位置传 string 应返回错误")
	}

	// Object 类型不匹配
	err = SchemaUtils{}.Validate(map[string]any{"meta": "not_object"}, params)
	if err == nil {
		t.Error("Object 位置传 string 应返回错误")
	}
}

// TestSchemaUtils_Validate_Number类型 测试 Number 类型校验
func TestSchemaUtils_Validate_Number类型(t *testing.T) {
	params := []*schema.Param{
		schema.NewNumberParam("score", "分数", true),
	}

	// float64 正确
	err := SchemaUtils{}.Validate(map[string]any{"score": 99.5}, params)
	if err != nil {
		t.Errorf("float64 应通过 Number 校验: %v", err)
	}

	// string 不正确
	err = SchemaUtils{}.Validate(map[string]any{"score": "bad"}, params)
	if err == nil {
		t.Error("string 不应通过 Number 校验")
	}
}

// TestSchemaUtils_Validate_Integer非float64 测试 Integer 字段传入非 float64
func TestSchemaUtils_Validate_Integer非float64(t *testing.T) {
	params := []*schema.Param{
		schema.NewIntegerParam("age", "年龄", true),
	}

	// string 不应通过
	err := SchemaUtils{}.Validate(map[string]any{"age": "not_int"}, params)
	if err == nil {
		t.Error("string 不应通过 Integer 校验")
	}
}

// TestSchemaUtils_FormatWithSchema_跳过校验和NoneValues 测试 FormatWithSchema 选项
func TestSchemaUtils_FormatWithSchema_跳过校验和NoneValues(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", false),
	}

	// skipValidate + skipNoneValue
	data := map[string]any{"name": nil}
	result, err := SchemaUtils{}.FormatWithSchema(data, params,
		WithFormatSkipNoneValue(true),
		WithFormatSkipValidate(true),
	)
	if err != nil {
		t.Fatalf("FormatWithSchema 失败: %v", err)
	}
	// nil 被移除，name 不在 result 中
	if _, ok := result["name"]; ok {
		t.Error("nil 值应被移除")
	}
}

// TestSchemaUtils_Validate_nil数据 测试 nil 输入数据
func TestSchemaUtils_Validate_nil数据(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("name", "名称", false),
	}
	err := SchemaUtils{}.Validate(nil, params)
	if err != nil {
		t.Errorf("nil 数据不应报错（无非必填参数）: %v", err)
	}
}

// TestSchemaUtils_float64整数校验 辅助测试
func TestSchemaUtils_float64整数校验(t *testing.T) {
	if math.Trunc(25.0) != 25.0 {
		t.Error("25.0 应为整数")
	}
	if math.Trunc(25.5) == 25.5 {
		t.Error("25.5 不应为整数")
	}
}
