# 统一建卡与输入校验规范化 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 让 harness/tools 下的 5 个工具通过统一建卡函数获取双语 inputParams，并通过 InvokeFunction 包装获得自动校验+解析+默认值填充。

**Architecture:** (1) Param 补齐 3 字段 → (2) ParseJSONSchemaMap 转换器 → (3) SchemaUtils 递归校验补齐 → (4) BuildToolCard 统一建卡 → (5) 5 个工具改用 InvokeFunction 包装 + BuildToolCard 建卡 → (6) 提示词对齐 Python

**Tech Stack:** Go 1.23+, schema.Param, InvokeFunction[I,O], SchemaUtils.FormatWithSchema, santhosh-tekuri/jsonschema/v6

**设计文档:** `docs/superpowers/specs/2026-07-08-unified-tool-card-and-validation-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/common/schema/param.go` | 新增 3 字段 + ParseJSONSchemaMap + 修改 paramToSchema/Validate |
| 修改 | `internal/common/schema/param_test.go` | 新增 ParseJSONSchemaMap 测试 + 新字段测试 |
| 修改 | `internal/agentcore/harness/prompts/tools/cron.go` | 补 bestEffert/failureDestination 的 type 字段 |
| 修改 | `internal/agentcore/foundation/tool/schema_utils.go` | 递归校验 + FormatWithSchemaMap 用 jsonschema 库 + 递归默认值填充 |
| 修改 | `internal/agentcore/foundation/tool/schema_utils_test.go` | 新增递归校验测试 |
| 修改 | `internal/agentcore/harness/prompts/tools/registry.go` | 新增 BuildToolCard 统一建卡函数 |
| 修改 | `internal/agentcore/harness/prompts/tools/registry.go` | 新增 BuildToolCard 测试（在 registry_test.go 或新文件） |
| 修改 | `internal/agentcore/foundation/tool/base.go` | 新增 NewToolCardWithID |
| 重写 | `internal/agentcore/harness/tools/tool_discovery/search_tools.go` | 改用 InvokeFunction 包装 |
| 修改 | `internal/agentcore/harness/tools/tool_discovery/search_tools_test.go` | 适配新接口 |
| 重写 | `internal/agentcore/harness/tools/tool_discovery/load_tools.go` | 改用 InvokeFunction 包装 |
| 修改 | `internal/agentcore/harness/tools/tool_discovery/load_tools_test.go` | 适配新接口 |
| 重写 | `internal/agentcore/harness/tools/subagent/session_tools.go` | 改用 InvokeFunction 包装 |
| 修改 | `internal/agentcore/harness/tools/subagent/session_tools_test.go` | 适配新接口 |

---

### Task 1: Param 补齐 AdditionalProperties/MinItems/MaxItems 字段

**Files:**
- Modify: `internal/common/schema/param.go:52-61` (Param struct)
- Modify: `internal/common/schema/param.go:435-455` (paramToSchema)
- Modify: `internal/common/schema/param.go:235-300` (Validate)
- Test: `internal/common/schema/param_test.go`

- [x] **Step 1: 在 Param struct 中新增 3 个字段**

在 `Properties []*Param` 字段之后、`AnyOf` 字段之前新增：

```go
// AdditionalProperties 对象是否允许额外属性（可选，仅 Object 类型）
AdditionalProperties bool `json:"additionalProperties,omitempty"`
// MinItems 数组最少元素数（可选，仅 Array 类型）
MinItems int `json:"minItems,omitempty"`
// MaxItems 数组最多元素数（可选，仅 Array 类型）
MaxItems int `json:"maxItems,omitempty"`
```

- [x] **Step 2: 修改 paramToSchema 输出 AdditionalProperties/MinItems/MaxItems**

在 paramToSchema 的 `ParamTypeObject` 分支中，输出 `additionalProperties`：
```go
case ParamTypeObject:
    if p.AdditionalProperties {
        s["additionalProperties"] = true
    }
    if len(p.Properties) > 0 { ... }
```

在 `ParamTypeArray` 分支中，输出 `minItems` / `maxItems`：
```go
case ParamTypeArray:
    if p.Items != nil {
        s["items"] = paramToSchema(p.Items)
    }
    if p.MinItems > 0 {
        s["minItems"] = p.MinItems
    }
    if p.MaxItems > 0 {
        s["maxItems"] = p.MaxItems
    }
```

- [x] **Step 3: 修改 Param.Validate() 校验新字段**

在 Validate 中新增校验规则：
- AdditionalProperties 仅在 ParamTypeObject 时合法（其他类型报错）
- MinItems/MaxItems 仅在 ParamTypeArray 时合法（其他类型报错）

- [x] **Step 4: 运行现有测试确认不破坏**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/schema/... -v -count=1`

- [x] **Step 5: 新增新字段的单元测试**

测试点：
- `TestParam_AdditionalProperties_Object合法`
- `TestParam_AdditionalProperties_非Object非法`
- `TestParam_MinItemsMaxItems_Array合法`
- `TestParam_MinItemsMaxItems_非Array非法`
- `TestParamToSchemaMap_AdditionalProperties输出`
- `TestParamToSchemaMap_MinItemsMaxItems输出`

- [x] **Step 6: 运行测试确认全部通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... -v -count=1`

- [x] **Step 7: 提交**

```bash
git add internal/common/schema/param.go internal/common/schema/param_test.go
git commit -m "feat(schema): Param 新增 AdditionalProperties/MinItems/MaxItems 字段"
```

---

### Task 2: 修复 cron.go 中省略 type 的属性

**Files:**
- Modify: `internal/agentcore/harness/prompts/tools/cron.go`

- [x] **Step 1: 找到 bestEffert 和 failureDestination 属性，补上 type 字段**

将：
```go
"bestEffort":         map[string]any{"description": d("delivery.bestEffort")},
"failureDestination": map[string]any{"description": d("delivery.failureDestination")},
```

改为：
```go
"bestEffort":         map[string]any{"type": "boolean", "description": d("delivery.bestEffort")},
"failureDestination": map[string]any{"type": "string", "description": d("delivery.failureDestination")},
```

- [x] **Step 2: 运行 prompts/tools 测试确认不破坏**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/tools/... -v -count=1`

- [x] **Step 3: 提交**

```bash
git add internal/agentcore/harness/prompts/tools/cron.go
git commit -m "fix(prompts/tools): cron.go 补齐 bestEffert/failureDestination 的 type 字段"
```

---

### Task 3: 实现 ParseJSONSchemaMap 转换器

**Files:**
- Modify: `internal/common/schema/param.go` (新增 ParseJSONSchemaMap + parsePropertyToParam)
- Test: `internal/common/schema/param_test.go`

- [x] **Step 1: 写 ParseJSONSchemaMap 的失败测试**

在 `param_test.go` 中新增：

```go
func TestParseJSONSchemaMap_空对象(t *testing.T) {
    schema := map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}}
    params, err := ParseJSONSchemaMap(schema)
    assert.NoError(t, err)
    assert.Len(t, params, 0)
}

func TestParseJSONSchemaMap_简单参数(t *testing.T) {
    schema := map[string]any{
        "type": "object",
        "properties": map[string]any{
            "name":  map[string]any{"type": "string", "description": "名称"},
            "count": map[string]any{"type": "integer", "description": "数量"},
        },
        "required": []any{"name"},
    }
    params, err := ParseJSONSchemaMap(schema)
    assert.NoError(t, err)
    assert.Len(t, params, 2)
    // 找 name 参数
    var nameParam *Param
    for _, p := range params {
        if p.Name == "name" { nameParam = p; break }
    }
    assert.NotNil(t, nameParam)
    assert.Equal(t, ParamTypeString, nameParam.Type)
    assert.True(t, nameParam.Required)
}

func TestParseJSONSchemaMap_属性缺type报错(t *testing.T) {
    schema := map[string]any{
        "type": "object",
        "properties": map[string]any{
            "bad": map[string]any{"description": "没有type"},
        },
    }
    _, err := ParseJSONSchemaMap(schema)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "missing required 'type' field")
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... -run TestParseJSONSchemaMap -v -count=1`
Expected: FAIL (undefined ParseJSONSchemaMap)

- [x] **Step 3: 实现 ParseJSONSchemaMap + parsePropertyToParam**

在 `param.go` 中实现：

```go
// ParseJSONSchemaMap 将 JSON Schema dict 转换为 []*Param。
//
// 输入格式为 MetadataProvider.GetInputParams() 返回的 map[string]any，
// 即标准 JSON Schema object 定义。
//
// 对应 Python: 无直接等价物（Python ToolCard.input_params 直接用 Dict[str, Any]）。
// Go 因 ToolCard.InputParams 类型为 []*Param 需要此转换。
func ParseJSONSchemaMap(schemaMap map[string]any) ([]*Param, error) {
    // 1. 校验顶层 type == "object"
    typ, _ := schemaMap["type"].(string)
    if typ != "object" {
        return nil, fmt.Errorf("schema must have type 'object', got %q", typ)
    }
    // 2. 提取 properties
    propsVal, _ := schemaMap["properties"].(map[string]any)
    if propsVal == nil {
        return nil, nil
    }
    // 3. 提取 required set
    requiredSet := make(map[string]bool)
    if req, ok := schemaMap["required"]; ok {
        if reqSlice, ok := req.([]any); ok {
            for _, r := range reqSlice {
                if s, ok := r.(string); ok { requiredSet[s] = true }
            }
        }
    }
    // 4. 遍历 properties，调用 parsePropertyToParam
    params := make([]*Param, 0, len(propsVal))
    for name, prop := range propsVal {
        propMap, ok := prop.(map[string]any)
        if !ok { return nil, fmt.Errorf("property %q is not a map", name) }
        p, err := parsePropertyToParam(name, propMap, requiredSet)
        if err != nil { return nil, err }
        params = append(params, p)
    }
    return params, nil
}

func parsePropertyToParam(name string, prop map[string]any, requiredSet map[string]bool) (*Param, error) {
    // 1. 解析 type
    typeStr, ok := prop["type"].(string)
    if !ok { return nil, fmt.Errorf("property %q missing required 'type' field", name) }
    paramType, ok := parseParamType(typeStr)
    if !ok { return nil, fmt.Errorf("property %q has unsupported type %q", name, typeStr) }

    p := &Param{Name: name, Type: paramType, Required: requiredSet[name]}

    // 2. 解析 description
    if desc, ok := prop["description"].(string); ok { p.Description = desc }
    // 3. 解析 enum
    if enum, ok := prop["enum"].([]any); ok { p.Enum = enum }
    // 4. 解析 default
    if def, ok := prop["default"]; ok { p.Default = def }
    // 5. 解析 nullable
    if nullable, ok := prop["nullable"].(bool); ok { p.Nullable = nullable }

    // 6. 解析约束
    if min, ok := prop["minimum"]; ok { p.Minimum = toFloat64(min) }
    if max, ok := prop["maximum"]; ok { p.Maximum = toFloat64(max) }
    if minLen, ok := toInt(prop["minLength"]); ok { p.MinLength = minLen }
    if maxLen, ok := toInt(prop["maxLength"]); ok { p.MaxLength = maxLen }
    if pattern, ok := prop["pattern"].(string); ok { p.Pattern = pattern }
    if format, ok := prop["format"].(string); ok { p.Format = format }

    // 7. 解析 array 约束
    if paramType == ParamTypeArray {
        if items, ok := prop["items"].(map[string]any); ok {
            item, err := parsePropertyToParam("item", items, nil)
            if err != nil { return nil, fmt.Errorf("property %q items: %w", name, err) }
            p.Items = item
        }
        if mi, ok := toInt(prop["minItems"]); ok { p.MinItems = mi }
        if mi, ok := toInt(prop["maxItems"]); ok { p.MaxItems = mi }
    }

    // 8. 解析 object 约束
    if paramType == ParamTypeObject {
        if ap, ok := prop["additionalProperties"].(bool); ok { p.AdditionalProperties = ap }
        if nestedProps, ok := prop["properties"].(map[string]any); ok {
            nestedRequired := make(map[string]bool)
            if req, ok2 := prop["required"]; ok2 {
                if reqSlice, ok2 := req.([]any); ok2 {
                    for _, r := range reqSlice {
                        if s, ok2 := r.(string); ok2 { nestedRequired[s] = true }
                    }
                }
            }
            props := make([]*Param, 0, len(nestedProps))
            for propName, propDef := range nestedProps {
                propMap, ok := propDef.(map[string]any)
                if !ok { return nil, fmt.Errorf("property %q.%q is not a map", name, propName) }
                nested, err := parsePropertyToParam(propName, propMap, nestedRequired)
                if err != nil { return nil, fmt.Errorf("property %q.%q: %w", name, propName, err) }
                props = append(props, nested)
            }
            p.Properties = props
        }
    }

    // 9. 解析组合 schema
    for _, keyword := range []string{"anyOf", "allOf", "oneOf"} {
        if subs, ok := prop[keyword].([]any); ok {
            subParams := make([]*Param, 0, len(subs))
            for i, sub := range subs {
                subMap, ok := sub.(map[string]any)
                if !ok { return nil, fmt.Errorf("property %q %s[%d] is not a map", name, keyword, i) }
                sp, err := parsePropertyToParam(fmt.Sprintf("%s_%d", keyword, i), subMap, nil)
                if err != nil { return nil, fmt.Errorf("property %q %s[%d]: %w", name, keyword, i, err) }
                subParams = append(subParams, sp)
            }
            switch keyword {
            case "anyOf": p.AnyOf = subParams
            case "allOf": p.AllOf = subParams
            case "oneOf": p.OneOf = subParams
            }
        }
    }

    return p, nil
}
```

加上辅助函数 `parseParamType`、`toFloat64`、`toInt`。

- [x] **Step 4: 运行测试确认基础用例通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... -run TestParseJSONSchemaMap -v -count=1`

- [x] **Step 5: 补齐更多测试用例**

测试点（每个写独立测试函数）：
- `TestParseJSONSchemaMap_布尔和数组参数` — boolean + array with items
- `TestParseJSONSchemaMap_enum枚举` — enum 字段
- `TestParseJSONSchemaMap_default默认值` — default 字段（string/int/bool）
- `TestParseJSONSchemaMap_约束字段` — minimum/maximum/minLength/maxLength/pattern/format
- `TestParseJSONSchemaMap_嵌套object` — 嵌套 properties + required
- `TestParseJSONSchemaMap_数组items嵌套` — items 含 object
- `TestParseJSONSchemaMap_additionalProperties` — additionalProperties=true
- `TestParseJSONSchemaMap_minItemsMaxItems` — minItems/maxItems
- `TestParseJSONSchemaMap_anyOf组合` — anyOf 子 schema
- `TestParseJSONSchemaMap_顶层非object报错` — type 不是 object 时报错

- [x] **Step 6: 运行全部测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... -v -count=1`

- [x] **Step 7: 提交**

```bash
git add internal/common/schema/param.go internal/common/schema/param_test.go
git commit -m "feat(schema): 实现 ParseJSONSchemaMap 转换器（map→[]*Param）"
```

---

### Task 4: SchemaUtils 递归校验补齐

**Files:**
- Modify: `internal/agentcore/foundation/tool/schema_utils.go` (手写递归校验 + jsonschema 库校验)
- Modify: `internal/agentcore/foundation/tool/schema_utils_test.go` (新增递归校验测试)

**设计：两种路径并存，对齐 Python 的双路径**

Python 的 `validate_with_schema` 有两个分支：
- `schema: Dict[str,Any]` → 用 `jsonschema` 库完整校验
- `schema: Type[BaseModel]` → 用 Pydantic `model_validate` 校验

Go 对应两个入口：
- `FormatWithSchemaMap(data, map[string]any)` → 用 `santhosh-tekuri/jsonschema/v6` 库校验（对齐 Python dict 路径）
- `FormatWithSchema(data, []*Param)` → 手写递归校验（Go 没有 Pydantic，手写对等）

- [x] **Step 1: FormatWithSchemaMap 改用 jsonschema 库校验**

将 `FormatWithSchemaMap` 中的手写校验逻辑（第 123-143 行）替换为 jsonschema 库调用：

```go
import jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

// 在 FormatWithSchemaMap 的校验分支中：
if !o.skipValidate {
    c := jsonschema.NewCompiler()
    if err := c.AddResource("schema.json", schemaMap); err != nil {
        return nil, exception.BuildError(
            exception.StatusSchemaValidateInvalid,
            exception.WithParam("reason", fmt.Sprintf("compile schema failed: %v", err)),
        )
    }
    sch, err := c.Compile("schema.json")
    if err != nil {
        return nil, exception.BuildError(
            exception.StatusSchemaValidateInvalid,
            exception.WithParam("reason", fmt.Sprintf("compile schema failed: %v", err)),
        )
    }
    if err := sch.Validate(data); err != nil {
        return nil, exception.BuildError(
            exception.StatusSchemaValidateInvalid,
            exception.WithParam("reason", err.Error()),
        )
    }
}
```

- [x] **Step 2: Validate 增加嵌套递归校验**

在 `validateParamType` 中增加递归校验：

```go
case schema.ParamTypeArray:
    arr, ok := val.([]any)
    if !ok {
        return exception.BuildError(...)
    }
    // 递归校验 array items
    if p.Items != nil {
        for i, item := range arr {
            if err := validateParamType(fmt.Sprintf("%s[%d]", key, i), item, p.Items); err != nil {
                return err
            }
            if err := validateParamConstraints(fmt.Sprintf("%s[%d]", key, i), item, p.Items); err != nil {
                return err
            }
        }
    }

case schema.ParamTypeObject:
    obj, ok := val.(map[string]any)
    if !ok {
        return exception.BuildError(...)
    }
    // 递归校验 object properties
    if len(p.Properties) > 0 {
        propMap := make(map[string]*schema.Param, len(p.Properties))
        for _, prop := range p.Properties { propMap[prop.Name] = prop }
        for k, v := range obj {
            if prop, ok := propMap[k]; ok {
                if err := validateParamType(k, v, prop); err != nil { return err }
                if err := validateParamConstraints(k, v, prop); err != nil { return err }
            }
        }
        // 校验嵌套 required
        for _, prop := range p.Properties {
            if prop.Required {
                if _, ok := obj[prop.Name]; !ok {
                    return exception.BuildError(
                        exception.StatusSchemaValidateInvalid,
                        exception.WithParam("param", prop.Name),
                        exception.WithParam("reason", "missing required param in nested object"),
                    )
                }
            }
        }
    }
```

- [x] **Step 3: validateParamConstraints 增加 MinItems/MaxItems 校验**

```go
case schema.ParamTypeArray:
    arr, ok := val.([]any)
    if !ok { return nil }
    if p.MinItems > 0 && len(arr) < p.MinItems {
        return exception.BuildError(
            exception.StatusSchemaValidateInvalid,
            exception.WithParam("param", key),
            exception.WithParam("reason", fmt.Sprintf("array length %d < minItems %d", len(arr), p.MinItems)),
        )
    }
    if p.MaxItems > 0 && len(arr) > p.MaxItems {
        return exception.BuildError(
            exception.StatusSchemaValidateInvalid,
            exception.WithParam("param", key),
            exception.WithParam("reason", fmt.Sprintf("array length %d > maxItems %d", len(arr), p.MaxItems)),
        )
    }
```

- [x] **Step 4: FormatWithSchema 增加递归默认值填充**

当前 `FormatWithSchema` 只填充顶层默认值（第 67-80 行），需要递归处理嵌套 object 和 array：

```go
// 3. 填充默认值（递归）
result := make(map[string]any, len(data))
for _, p := range params {
    if val, ok := data[p.Name]; ok {
        // 递归填充嵌套默认值
        result[p.Name] = su.fillDefaults(val, p)
    } else if p.Default != nil {
        result[p.Name] = p.Default
    } else if p.Required {
        return nil, exception.BuildError(...)
    }
}
```

新增 `fillDefaults` 辅助方法：
```go
func (su SchemaUtils) fillDefaults(val any, p *schema.Param) any {
    switch p.Type {
    case schema.ParamTypeObject:
        obj, ok := val.(map[string]any)
        if !ok || len(p.Properties) == 0 { return val }
        result := make(map[string]any, len(obj))
        for k, v := range obj { result[k] = v }
        for _, prop := range p.Properties {
            if _, ok := result[prop.Name]; !ok && prop.Default != nil {
                result[prop.Name] = prop.Default
            }
        }
        return result
    case schema.ParamTypeArray:
        arr, ok := val.([]any)
        if !ok || p.Items == nil { return val }
        return arr // array items 的默认值填充不常见，暂不递归
    default:
        return val
    }
}
```

- [x] **Step 5: 写递归校验的单元测试**

测试点：
- `TestValidate_嵌套object必填校验` — 嵌套 required 字段缺失时报错
- `TestValidate_嵌套object类型校验` — 嵌套属性类型不匹配时报错
- `TestValidate_数组items类型校验` — array 元素类型不匹配时报错
- `TestValidate_数组items约束校验` — array 元素约束不满足时报错
- `TestValidate_MinItems校验` — array 长度 < minItems 报错
- `TestValidate_MaxItems校验` — array 长度 > maxItems 报错
- `TestFormatWithSchemaMap_jsonschema校验` — FormatWithSchemaMap 走 jsonschema 库校验（含嵌套）
- `TestFormatWithSchema_递归默认值填充` — 嵌套 object 默认值填充

- [x] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/... -v -count=1`

- [x] **Step 7: 提交**

```bash
git add internal/agentcore/foundation/tool/schema_utils.go internal/agentcore/foundation/tool/schema_utils_test.go
git commit -m "feat(tool): SchemaUtils 递归校验补齐（嵌套object/array/MinItems/MaxItems + FormatWithSchemaMap 用 jsonschema 库）"
```

---

### Task 5: 实现 BuildToolCard 统一建卡函数

**Files:**
- Modify: `internal/agentcore/foundation/tool/base.go` (新增 NewToolCardWithID)
- Modify: `internal/agentcore/harness/prompts/tools/registry.go` (新增 BuildToolCard)
- Test: `internal/agentcore/harness/prompts/tools/registry_test.go` (新建)

- [x] **Step 1: 在 foundation/tool/base.go 新增 NewToolCardWithID**

```go
// NewToolCardWithID 创建 ToolCard 实例，使用指定 ID。
//
// 对齐 Python: build_tool_card 中的 tool_id 生成逻辑。
func NewToolCardWithID(id, name, description string, inputParams []*schema.Param, properties map[string]any) *ToolCard {
	card := &ToolCard{
		BaseCard:    *schema.NewBaseCard(schema.WithID(id), schema.WithName(name), schema.WithDescription(description)),
		InputParams: inputParams,
		Properties:  properties,
	}
	if card.Properties == nil {
		card.Properties = make(map[string]any)
	}
	return card
}
```

- [x] **Step 2: 实现 BuildToolCard**

在 `registry.go` 中新增：

```go
// BuildToolCard 统一建卡函数。从 registry 获取 description + inputParams，
// 转换为 []*schema.Param 后构建 ToolCard。
//
// 对齐 Python: openjiuwen/harness/prompts/tools/__init__.py build_tool_card()
func BuildToolCard(
	name string,
	toolID string,
	language string,
	formatArgs map[string]string,
	agentID string,
) (*tool.ToolCard, error) {
	// 1. 从 registry 查找 provider
	provider, ok := GetToolProvider(name)
	if !ok {
		return nil, fmt.Errorf("tool %q not registered in prompts/tools registry", name)
	}

	// 2. 获取 description
	description := provider.GetDescription(language)

	// 3. 如果有 formatArgs，填充描述中的占位符
	for key, value := range formatArgs {
		description = strings.ReplaceAll(description, "{"+key+"}", value)
	}

	// 4. 获取 input_params 并转换
	inputParamsMap := provider.GetInputParams(language)
	inputParams, err := cschema.ParseJSONSchemaMap(inputParamsMap)
	if err != nil {
		return nil, fmt.Errorf("tool %q: parse input params failed: %w", name, err)
	}

	// 5. 生成 tool_id
	var finalID string
	if agentID != "" {
		finalID = toolID + "_" + agentID
	} else {
		finalID = toolID + "_" + uuid.New().String()[:8]
	}

	// 6. 构建 ToolCard
	card := tool.NewToolCardWithID(finalID, name, description, inputParams, nil)
	return card, nil
}
```

需要在 registry.go 中添加 import：`tool`, `cschema`, `strings`, `fmt`, `github.com/google/uuid`。

- [x] **Step 3: 写 BuildToolCard 测试**

新建 `registry_test.go`：

```go
func TestBuildToolCard_已注册工具(t *testing.T) {
    card, err := tools.BuildToolCard("search_tools", "SearchToolsTool", "cn", nil, "agent1")
    assert.NoError(t, err)
    assert.Equal(t, "search_tools", card.GetName())
    assert.Contains(t, card.GetID(), "SearchToolsTool_agent1")
    assert.NotEmpty(t, card.GetDescription())
    assert.NotEmpty(t, card.InputParams)
}

func TestBuildToolCard_双语切换(t *testing.T) {
    cardCN, _ := tools.BuildToolCard("search_tools", "SearchToolsTool", "cn", nil, "")
    cardEN, _ := tools.BuildToolCard("search_tools", "SearchToolsTool", "en", nil, "")
    // 中文和英文描述应该不同
    assert.NotEqual(t, cardCN.GetDescription(), cardEN.GetDescription())
}

func TestBuildToolCard_未注册工具报错(t *testing.T) {
    _, err := tools.BuildToolCard("nonexistent", "Xxx", "cn", nil, "")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "not registered")
}

func TestBuildToolCard_formatArgs填充(t *testing.T) {
    // sessions_spawn 使用 {available_agents} 占位符
    card, err := tools.BuildToolCard("sessions_spawn", "sessions_spawn", "cn",
        map[string]string{"available_agents": "general-purpose,react"}, "agent1")
    assert.NoError(t, err)
    assert.Contains(t, card.GetDescription(), "general-purpose,react")
}
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/tools/... -v -count=1`

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/tool/base.go internal/agentcore/harness/prompts/tools/registry.go internal/agentcore/harness/prompts/tools/registry_test.go
git commit -m "feat(harness): 实现 BuildToolCard 统一建卡函数（对齐 Python build_tool_card）"
```

---

### Task 6: SearchToolsTool 改用 InvokeFunction 包装 + BuildToolCard

**Files:**
- Rewrite: `internal/agentcore/harness/tools/tool_discovery/search_tools.go`
- Modify: `internal/agentcore/harness/tools/tool_discovery/search_tools_test.go`

- [x] **Step 1: 重写 search_tools.go**

核心变化：
1. 删除 `SearchToolsTool` struct，改为函数式构造
2. `SearchToolsInput` struct 保留（InvokeFunction 需要）
3. `NewSearchToolsTool` 返回 `tool.Tool`（实际为 `*InvokeFunction[SearchToolsInput, map[string]any]`）
4. 用 `tools.BuildToolCard()` 建卡，删除 `buildSearchToolsInputParams()`
5. `callability_note` / `next_step_hint` 使用 Python 英文原文常量
6. 删除 `buildCallabilityNote()` / `buildNextStepHint()` / `clampLimit()` 移入 invoke 函数内部

关键代码结构：

```go
package tool_discovery

import (
    "context"
    "fmt"

    "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
    "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
    "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchToolsInput search_tools 工具的输入参数。
// 对齐 Python: SearchToolsInput
type SearchToolsInput struct {
    // Query 搜索查询文本
    Query string `json:"query"`
    // Limit 返回候选工具的最大数量，默认 10，范围 [1, 20]
    Limit int `json:"limit"`
    // DetailLevel 详情级别：1=name+描述, 2=+参数摘要, 3=+完整参数
    DetailLevel int `json:"detail_level"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
    searchToolsLimitMin       = 1
    searchToolsLimitMax       = 20
    searchToolsLimitDefault   = 10
    searchToolsDetailLevelDefault = 1

    // callabilityNote 严格对齐 Python SearchToolsTool.invoke 中的 callability_note
    callabilityNote = "Search results are discovery-only. Tools shown here are not callable until load_tools is called."
    // nextStepHint 严格对齐 Python SearchToolsTool.invoke 中的 next_step_hint
    nextStepHint = "If the result is clear enough, call load_tools directly. Increase detail_level to 2 or 3 when you need more parameter detail."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSearchToolsTool 创建搜索候选工具元工具。
// 对齐 Python: SearchToolsTool.__init__
func NewSearchToolsTool(
    searchFn func(ctx context.Context, query string, limit int, detailLevel int) ([]map[string]any, error),
    traceFn func(session interfaces.SessionFacade, event map[string]any),
    language string,
    agentID string,
) tool.Tool {
    card, _ := tools.BuildToolCard("search_tools", "SearchToolsTool", language, nil, agentID)

    fn := func(ctx context.Context, input SearchToolsInput, opts ...tool.ToolOption) (map[string]any, error) {
        // 限幅 limit 到 [1, 20]
        limit := input.Limit
        if limit < searchToolsLimitMin { limit = searchToolsLimitMin }
        if limit > searchToolsLimitMax { limit = searchToolsLimitMax }

        // 调用搜索回调
        matches, err := searchFn(ctx, input.Query, limit, input.DetailLevel)
        if err != nil {
            logger.Warn(searchLogComponent).
                Str("tool_name", "search_tools").
                Str("query", input.Query).
                Int("limit", limit).
                Int("detail_level", input.DetailLevel).
                Err(err).
                Msg("SearchToolsTool 搜索失败")
            return map[string]any{"success": false, "error": err.Error()}, nil
        }

        // 追加轨迹
        callOpts := tool.NewToolCallOptions(opts...)
        if callOpts.Session != nil {
            if sess, ok := callOpts.Session.(interfaces.SessionFacade); ok {
                traceFn(sess, map[string]any{
                    "action":       "search_tools",
                    "query":        input.Query,
                    "limit":        limit,
                    "detail_level": input.DetailLevel,
                    "match_count":  len(matches),
                })
            }
        }

        // 日志
        logger.Info(searchLogComponent).
            Str("tool_name", "search_tools").
            Str("query", input.Query).
            Int("limit", limit).
            Int("detail_level", input.DetailLevel).
            Int("match_count", len(matches)).
            Msg("SearchToolsTool 搜索完成")

        return map[string]any{
            "success":          true,
            "query":            input.Query,
            "matches":          matches,
            "count":            len(matches),
            "callability_note": callabilityNote,
            "next_step_hint":   nextStepHint,
        }, nil
    }

    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
    return invokeFn
}
```

注意：删除 `buildSearchToolsInputParams()`、`buildCallabilityNote()`、`buildNextStepHint()`、`clampLimit()` 等非导出函数。删除 `SearchToolsTool` struct 及其 `Card()`/`Invoke()`/`Stream()` 方法。

- [x] **Step 2: 更新 search_tools_test.go 适配新接口**

测试中通过 `NewSearchToolsTool()` 构造得到的不再是 `*SearchToolsTool`，而是 `tool.Tool`（InvokeFunction）。
- 调用方式不变：`result, err := t.Invoke(ctx, inputs)`
- 删除对 `SearchToolsTool` struct 的直接引用
- 删除 `buildCallabilityNote` / `buildNextStepHint` 相关测试
- 新增验证 `callability_note` / `next_step_hint` 等于 Python 原文的测试

- [x] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/tool_discovery/... -v -count=1`

- [x] **Step 4: 检查 progressive_test.go 是否引用了 SearchToolsTool struct**

如果有引用需要同步修改。

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/tool_discovery/search_tools.go internal/agentcore/harness/tools/tool_discovery/search_tools_test.go
git commit -m "refactor(tool_discovery): SearchToolsTool 改用 InvokeFunction 包装 + BuildToolCard 建卡"
```

---

### Task 7: LoadToolsTool 改用 InvokeFunction 包装 + BuildToolCard

**Files:**
- Rewrite: `internal/agentcore/harness/tools/tool_discovery/load_tools.go`
- Modify: `internal/agentcore/harness/tools/tool_discovery/load_tools_test.go`

- [x] **Step 1: 重写 load_tools.go**

与 SearchToolsTool 同理：
- `LoadToolsInput` struct 保留
- `NewLoadToolsTool` 返回 `tool.Tool`
- 用 `tools.BuildToolCard("load_tools", "LoadToolsTool", language, nil, agentID)` 建卡
- 删除 `buildLoadToolsInputParams()`

- [x] **Step 2: 更新 load_tools_test.go**

- [x] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/tool_discovery/... -v -count=1`

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/tool_discovery/load_tools.go internal/agentcore/harness/tools/tool_discovery/load_tools_test.go
git commit -m "refactor(tool_discovery): LoadToolsTool 改用 InvokeFunction 包装 + BuildToolCard 建卡"
```

---

### Task 8: SessionsListTool/SessionsSpawnTool/SessionsCancelTool 改用 InvokeFunction + BuildToolCard

**Files:**
- Rewrite: `internal/agentcore/harness/tools/subagent/session_tools.go`
- Modify: `internal/agentcore/harness/tools/subagent/session_tools_test.go`

- [x] **Step 1: 重写 session_tools.go**

对三个工具分别改造：

**SessionsListTool:**
```go
type SessionsListInput struct{}

func NewSessionsListTool(toolkit *SessionToolkit, language string) tool.Tool {
    card, _ := tools.BuildToolCard("sessions_list", "sessions_list", language, nil, "")
    fn := func(ctx context.Context, _ SessionsListInput, opts ...tool.ToolOption) (map[string]any, error) {
        // 原有 SessionsListTool.Invoke 的逻辑，无需从 inputs 提取参数
    }
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
    return invokeFn
}
```

**SessionsSpawnTool:**
```go
type SessionsSpawnInput struct {
    SubagentType    string `json:"subagent_type"`
    TaskDescription string `json:"task_description"`
}

func NewSessionsSpawnTool(provider, toolkit, language, agentID, availableAgents) tool.Tool {
    var formatArgs map[string]string
    if availableAgents != "" {
        formatArgs = map[string]string{"available_agents": availableAgents}
    }
    card, _ := tools.BuildToolCard("sessions_spawn", "sessions_spawn", language, formatArgs, agentID)
    fn := func(ctx context.Context, input SessionsSpawnInput, opts ...tool.ToolOption) (map[string]any, error) {
        callOpts := tool.NewToolCallOptions(opts...)
        session := callOpts.Session
        // 用 input.SubagentType, input.TaskDescription 代替 inputs["subagent_type"].(string)
        // 其余逻辑保持不变
    }
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
    return invokeFn
}
```

**SessionsCancelTool:**
```go
type SessionsCancelInput struct {
    TaskID string `json:"task_id"`
}
```

同理改造。注意 `SessionsCancelTool` 原有手动的 `taskID` 空值校验不再需要（FormatWithSchema 自动校验 required）。

删除 `buildSessionsListInputParams()`、`buildSessionsSpawnInputParams()`、`buildSessionsCancelInputParams()`。
删除 `SessionsListTool`/`SessionsSpawnTool`/`SessionsCancelTool` struct 及其方法。
删除编译时接口检查 `var _ tool.Tool = ...`。

- [x] **Step 2: 更新 session_tools_test.go 适配新接口**

- [x] **Step 3: 检查是否有其他文件引用 SessionsXxxTool struct**

搜索 `SessionsListTool`/`SessionsSpawnTool`/`SessionsCancelTool` 在 harness/tools 之外的引用，如有需同步修改。

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/subagent/... -v -count=1`

- [x] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/subagent/session_tools.go internal/agentcore/harness/tools/subagent/session_tools_test.go
git commit -m "refactor(subagent): session_tools 改用 InvokeFunction 包装 + BuildToolCard 建卡"
```

---

### Task 9: 提示词严格对齐 Python

**Files:**
- 已在 Task 5 中处理 search_tools.go 的 callability_note/next_step_hint
- 检查 progressive.go 中是否有其他需要对齐的提示词

- [x] **Step 1: 搜索 harness/rails/ 和 harness/tools/ 中所有中文硬编码提示词**

搜索 `buildCallabilityNote`、`buildNextStepHint`、`"搜索结果`、`"从搜索结果` 等模式，确认 Task 5 已全部清理。

- [x] **Step 2: 对照 Python progressive_tool_rail.py 中的提示词**

检查 `buildNavigationSection`、`buildProgressiveToolRulesSection` 等方法中的提示词是否与 Python 对齐。如有不一致需修正。

- [x] **Step 3: 运行相关测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -count=1`

- [x] **Step 4: 提交**

```bash
git add -A
git commit -m "fix(harness): 提示词严格对齐 Python 原文，删除自定义中文翻译"
```

---

### Task 10: 全量编译 + 测试验证

- [x] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

- [x] **Step 2: 运行核心包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... ./internal/agentcore/foundation/tool/... ./internal/agentcore/harness/prompts/tools/... ./internal/agentcore/harness/tools/... ./internal/agentcore/harness/rails/... -v -count=1`

- [x] **Step 3: 检查 ProgressiveToolRail 是否受影响**

搜索 `SearchToolsTool`/`LoadToolsTool` 在 `progressive.go` 中的引用，确认构造函数签名变化已适配。

- [x] **Step 4: 提交最终状态**

```bash
git push
```
