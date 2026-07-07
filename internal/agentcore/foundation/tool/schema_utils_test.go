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

// ──────────────────────────── FormatWithSchemaMap 测试 ────────────────────────────

// TestSchemaUtils_FormatWithSchemaMap_默认值填充 测试 JSON Schema map 默认值填充
func TestSchemaUtils_FormatWithSchemaMap_默认值填充(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string", "description": "名称"},
			"limit": map[string]any{"type": "integer", "description": "数量上限", "default": 10},
		},
		"required": []any{"name"},
	}

	data := map[string]any{"name": "Alice"}
	result, err := SchemaUtils{}.FormatWithSchemaMap(data, schemaMap)
	if err != nil {
		t.Fatalf("FormatWithSchemaMap 失败: %v", err)
	}
	if result["name"] != "Alice" {
		t.Errorf("name: 期望 Alice，实际 %v", result["name"])
	}
	if result["limit"] != 10 {
		t.Errorf("limit default: 期望 10，实际 %v", result["limit"])
	}
}

// TestSchemaUtils_FormatWithSchemaMap_必填缺失 测试 JSON Schema map 必填字段缺失
func TestSchemaUtils_FormatWithSchemaMap_必填缺失(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "名称"},
		},
		"required": []any{"name"},
	}

	_, err := SchemaUtils{}.FormatWithSchemaMap(map[string]any{}, schemaMap)
	if err == nil {
		t.Error("缺少必填字段应返回错误")
	}
}

// TestSchemaUtils_FormatWithSchemaMap_必填但有默认值 测试必填字段有默认值时不报错
func TestSchemaUtils_FormatWithSchemaMap_必填但有默认值(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "名称", "default": "unknown"},
		},
		"required": []any{"name"},
	}

	result, err := SchemaUtils{}.FormatWithSchemaMap(map[string]any{}, schemaMap)
	if err != nil {
		t.Fatalf("有默认值的必填字段不应报错: %v", err)
	}
	if result["name"] != "unknown" {
		t.Errorf("name default: 期望 unknown，实际 %v", result["name"])
	}
}

// TestSchemaUtils_FormatWithSchemaMap_额外字段保留 测试额外字段保留
func TestSchemaUtils_FormatWithSchemaMap_额外字段保留(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "名称"},
		},
	}

	data := map[string]any{"name": "Alice", "extra": "value"}
	result, err := SchemaUtils{}.FormatWithSchemaMap(data, schemaMap)
	if err != nil {
		t.Fatalf("FormatWithSchemaMap 失败: %v", err)
	}
	if result["extra"] != "value" {
		t.Errorf("额外字段应保留: extra=%v", result["extra"])
	}
}

// TestSchemaUtils_FormatWithSchemaMap_跳过校验 测试 skipValidate 选项
func TestSchemaUtils_FormatWithSchemaMap_跳过校验(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "名称"},
		},
		"required": []any{"name"},
	}

	// 缺少必填字段但 skipValidate
	result, err := SchemaUtils{}.FormatWithSchemaMap(map[string]any{}, schemaMap,
		WithFormatSkipValidate(true),
	)
	if err != nil {
		t.Fatalf("skipValidate 时不应报错: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("期望空 map，实际 %v", result)
	}
}

// TestSchemaUtils_FormatWithSchemaMap_skipNoneValue 测试 skipNoneValue 选项
func TestSchemaUtils_FormatWithSchemaMap_skipNoneValue(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "名称"},
		},
	}

	data := map[string]any{"name": nil}
	result, err := SchemaUtils{}.FormatWithSchemaMap(data, schemaMap,
		WithFormatSkipNoneValue(true),
		WithFormatSkipValidate(true),
	)
	if err != nil {
		t.Fatalf("FormatWithSchemaMap 失败: %v", err)
	}
	if _, ok := result["name"]; ok {
		t.Error("nil 值应被移除")
	}
}

// TestSchemaUtils_FormatWithSchemaMap_nil输入 测试 nil 输入数据
func TestSchemaUtils_FormatWithSchemaMap_nil输入(t *testing.T) {
	schemaMap := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}

	result, err := SchemaUtils{}.FormatWithSchemaMap(nil, schemaMap)
	if err != nil {
		t.Fatalf("nil 数据不应报错: %v", err)
	}
	if result == nil {
		t.Error("nil 输入应返回空 map")
	}
}

// TestSchemaUtils_FormatWithSchemaMap_输入覆盖默认值 测试输入值覆盖默认值
func TestSchemaUtils_FormatWithSchemaMap_输入覆盖默认值(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "description": "数量上限", "default": 10},
		},
	}

	data := map[string]any{"limit": 20}
	result, err := SchemaUtils{}.FormatWithSchemaMap(data, schemaMap)
	if err != nil {
		t.Fatalf("FormatWithSchemaMap 失败: %v", err)
	}
	if result["limit"] != 20 {
		t.Errorf("输入值应覆盖默认值: 期望 20，实际 %v", result["limit"])
	}
}

// TestSchemaUtils_FormatWithSchemaMap_空schema 测试空 schema
func TestSchemaUtils_FormatWithSchemaMap_空schema(t *testing.T) {
	data := map[string]any{"key": "value"}
	result, err := SchemaUtils{}.FormatWithSchemaMap(data, map[string]any{})
	if err != nil {
		t.Fatalf("空 schema 不应报错: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("空 schema 下数据应原样返回: key=%v", result["key"])
	}
}

// ──────────────────────────── toFloat64 测试 ────────────────────────────

// TestToFloat64 验证各种数值类型转 float64。
func TestToFloat64(t *testing.T) {
	tests := []struct {
		name   string
		val    any
		want   float64
		wantOK bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"int", int(42), 42.0, true},
		{"int64", int64(100), 100.0, true},
		{"int32", int32(7), 7.0, true},
		{"string", "not_a_number", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloat64(tt.val)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("got = %v, want %v", got, tt.want)
			}
		})
	}
}

// ──────────────────────────── 递归校验测试 ────────────────────────────

func TestValidate_嵌套object必填校验(t *testing.T) {
	params := []*schema.Param{
		{
			Name: "config", Type: schema.ParamTypeObject, Required: true,
			Properties: []*schema.Param{
				schema.NewStringParam("host", "主机", true),
				schema.NewIntegerParam("port", "端口", false),
			},
		},
	}
	// 缺少嵌套的 host 字段
	data := map[string]any{
		"config": map[string]any{"port": float64(8080)},
	}
	err := SchemaUtils{}.Validate(data, params)
	if err == nil {
		t.Error("嵌套 required 字段缺失应返回错误")
	}
}

func TestValidate_嵌套object类型校验(t *testing.T) {
	params := []*schema.Param{
		{
			Name: "config", Type: schema.ParamTypeObject, Required: true,
			Properties: []*schema.Param{
				schema.NewStringParam("host", "主机", true),
			},
		},
	}
	// host 类型不匹配（给数字而非字符串）
	data := map[string]any{
		"config": map[string]any{"host": 123},
	}
	err := SchemaUtils{}.Validate(data, params)
	if err == nil {
		t.Error("嵌套属性类型不匹配应返回错误")
	}
}

func TestValidate_数组items类型校验(t *testing.T) {
	params := []*schema.Param{
		schema.NewArrayParam("tags", "标签", true, schema.NewStringParam("tag", "标签", true)),
	}
	// 数组元素类型不匹配（含数字而非字符串）
	data := map[string]any{
		"tags": []any{"valid", 123},
	}
	err := SchemaUtils{}.Validate(data, params)
	if err == nil {
		t.Error("数组元素类型不匹配应返回错误")
	}
}

func TestValidate_MinItems校验(t *testing.T) {
	p := &schema.Param{
		Name: "tags", Type: schema.ParamTypeArray, Required: true,
		Items:    schema.NewStringParam("tag", "标签", true),
		MinItems: 2,
	}
	params := []*schema.Param{p}
	data := map[string]any{
		"tags": []any{"only_one"},
	}
	err := SchemaUtils{}.Validate(data, params)
	if err == nil {
		t.Error("数组长度 < minItems 应返回错误")
	}
}

func TestValidate_MaxItems校验(t *testing.T) {
	p := &schema.Param{
		Name: "tags", Type: schema.ParamTypeArray, Required: true,
		Items:    schema.NewStringParam("tag", "标签", true),
		MaxItems: 2,
	}
	params := []*schema.Param{p}
	data := map[string]any{
		"tags": []any{"a", "b", "c"},
	}
	err := SchemaUtils{}.Validate(data, params)
	if err == nil {
		t.Error("数组长度 > maxItems 应返回错误")
	}
}

func TestFormatWithSchemaMap_jsonschema校验(t *testing.T) {
	schemaMap := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"config": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"host": map[string]any{"type": "string"},
					"port": map[string]any{"type": "integer"},
				},
				"required": []any{"host"},
			},
		},
		"required": []any{"config"},
	}

	// 合法数据
	result, err := SchemaUtils{}.FormatWithSchemaMap(map[string]any{
		"config": map[string]any{"host": "localhost", "port": 8080},
	}, schemaMap)
	if err != nil {
		t.Fatalf("合法数据不应报错: %v", err)
	}
	configObj := result["config"].(map[string]any)
	if configObj["host"] != "localhost" {
		t.Errorf("host 期望 localhost，实际 %v", configObj["host"])
	}

	// 嵌套 required 缺失
	_, err = SchemaUtils{}.FormatWithSchemaMap(map[string]any{
		"config": map[string]any{"port": 8080},
	}, schemaMap)
	if err == nil {
		t.Error("嵌套 required 缺失应返回错误")
	}
}

func TestFormatWithSchema_递归默认值填充(t *testing.T) {
	params := []*schema.Param{
		{
			Name: "config", Type: schema.ParamTypeObject, Required: true,
			Properties: []*schema.Param{
				schema.NewStringParam("host", "主机", true),
				{Name: "port", Type: schema.ParamTypeInteger, Required: false, Default: 8080},
			},
		},
	}

	// 传入 host 但不传 port，应自动填充 port 默认值
	data := map[string]any{
		"config": map[string]any{"host": "localhost"},
	}
	result, err := SchemaUtils{}.FormatWithSchema(data, params)
	if err != nil {
		t.Fatalf("递归默认值填充失败: %v", err)
	}
	configObj := result["config"].(map[string]any)
	if configObj["host"] != "localhost" {
		t.Errorf("host 期望 localhost，实际 %v", configObj["host"])
	}
	if configObj["port"] != 8080 {
		t.Errorf("port 期望 8080（默认值），实际 %v", configObj["port"])
	}
}

func TestValidate_数组items约束校验(t *testing.T) {
	params := []*schema.Param{
		{
			Name: "scores", Type: schema.ParamTypeArray, Required: true,
			Items: &schema.Param{
				Name: "score", Type: schema.ParamTypeInteger, Required: true,
				Minimum: 0, Maximum: 100,
			},
		},
	}
	// 数组元素超出 maximum
	data := map[string]any{
		"scores": []any{float64(50), float64(150)},
	}
	err := SchemaUtils{}.Validate(data, params)
	if err == nil {
		t.Error("数组元素约束不满足应返回错误")
	}
}

func TestFormatWithSchema_数组items递归默认值(t *testing.T) {
	params := []*schema.Param{
		{
			Name: "users", Type: schema.ParamTypeArray, Required: true,
			Items: &schema.Param{
				Name: "user", Type: schema.ParamTypeObject, Required: true,
				Properties: []*schema.Param{
					schema.NewStringParam("name", "名称", true),
					{Name: "role", Type: schema.ParamTypeString, Required: false, Default: "user"},
				},
			},
		},
	}

	// 数组中的对象缺少 role，应自动填充默认值
	data := map[string]any{
		"users": []any{
			map[string]any{"name": "Alice"},
			map[string]any{"name": "Bob", "role": "admin"},
		},
	}
	result, err := SchemaUtils{}.FormatWithSchema(data, params, WithFormatSkipValidate(true))
	if err != nil {
		t.Fatalf("数组 items 递归默认值填充失败: %v", err)
	}
	users := result["users"].([]any)
	alice := users[0].(map[string]any)
	if alice["role"] != "user" {
		t.Errorf("Alice.role 期望 'user'（默认值），实际 %v", alice["role"])
	}
	bob := users[1].(map[string]any)
	if bob["role"] != "admin" {
		t.Errorf("Bob.role 期望 'admin'，实际 %v", bob["role"])
	}
}
