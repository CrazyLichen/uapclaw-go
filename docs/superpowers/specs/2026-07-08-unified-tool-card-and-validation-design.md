# 统一建卡与输入校验规范化设计

> 日期：2026-07-08
> 范围：harness/tools（tool_discovery + session_tools）+ common/schema + harness/prompts/tools

## 1. 问题背景

### 1.1 inputParams 双语缺失

Go 的 `NewSearchToolsTool` / `NewLoadToolsTool` / `NewSessionsXxxTool` 构造函数中，`description` 从 `MetadataProvider.GetDescription(language)` 获取（支持双语），但 `inputParams` 用手写的 `buildXxxInputParams()` 硬编码中文，**跳过了 MetadataProvider 的双语 `GetInputParams()`**。

Python 通过 `build_tool_card()` 统一从 registry 同时拿 description + input_params，Go 缺少此统一建卡函数。

根因：`ToolCard.InputParams` 类型为 `[]*schema.Param`，而 `MetadataProvider.GetInputParams()` 返回 `map[string]any`（JSON Schema dict），类型不匹配导致 tool 实现只能跳过 provider 手动构建。

### 1.2 输入参数解析和校验缺失

Python 用 Pydantic `SearchToolsInput(**inputs)` 一行完成解析+校验+默认值填充。Go 的 `InvokeFunction` 用 `SchemaUtils.FormatWithSchema()` + `json.Marshal→Unmarshal` 等价实现。但 harness/tools 下的 5 个工具（SearchToolsTool、LoadToolsTool、SessionsListTool、SessionsSpawnTool、SessionsCancelTool）**直接实现 Tool 接口，手动类型断言提取参数，无校验，无默认值填充，无 TOOL_PARSE 事件触发**。

### 1.3 提示词未严格对齐 Python

`callability_note` / `next_step_hint` 等提示词，Go 自定义了中英文版本，与 Python 原文措辞不同。Python 的 `next_step_hint` 包含 detail_level 提升提示，Go 缺失。

### 1.4 Param 结构体字段缺失

`Param` 缺少 `additionalProperties`、`minItems`、`maxItems` 三个字段，但 `prompts/tools` 的 MetadataProvider（如 cron.go、ask_user.go）已在使用这些 JSON Schema 字段。

### 1.5 cron.go 中属性省略 type 字段

`cron.go` 中 `bestEffert` 和 `failureDestination` 属性只有 `description` 没有 `type`，不符合 JSON Schema 规范（type 虽然可选，但与其他属性风格不一致），且导致 ParseJSONSchemaMap 无法确定 ParamType。

## 2. 设计方案

### 2.1 Param 补齐缺失字段

在 `internal/common/schema/param.go` 的 `Param` struct 中新增：

```go
// AdditionalProperties 对象是否允许额外属性（可选，仅 Object 类型）
AdditionalProperties bool `json:"additionalProperties,omitempty"`
// MinItems 数组最少元素数（可选，仅 Array 类型）
MinItems int `json:"minItems,omitempty"`
// MaxItems 数组最多元素数（可选，仅 Array 类型）
MaxItems int `json:"maxItems,omitempty"`
```

同步修改 `paramToSchema()` 输出逻辑，在 Object 分支输出 `additionalProperties`，在 Array 分支输出 `minItems` / `maxItems`。

同步修改 `Param.Validate()` 校验逻辑：
- `AdditionalProperties` 仅在 `ParamTypeObject` 时合法
- `MinItems` / `MaxItems` 仅在 `ParamTypeArray` 时合法

### 2.2 修复 cron.go 中省略 type 的属性

给 `bestEffert` 补 `"type": "boolean"`，给 `failureDestination` 补 `"type": "string"`。

### 2.3 ParseJSONSchemaMap 转换器

在 `internal/common/schema/param.go` 新增：

```go
// ParseJSONSchemaMap 将 JSON Schema dict 转换为 []*Param。
//
// 输入格式为 MetadataProvider.GetInputParams() 返回的 map[string]any，
// 即标准 JSON Schema object 定义：
//
//	{
//	  "type": "object",
//	  "properties": { "name": {"type": "string", "description": "..."}, ... },
//	  "required": ["name"]
//	}
//
// 支持的 JSON Schema 字段：type, description, required, enum, default,
// items, properties, additionalProperties, minimum, maximum, minLength,
// maxLength, pattern, format, nullable, minItems, maxItems,
// anyOf, allOf, oneOf。
//
// 对应 Python: 无直接等价物（Python ToolCard.input_params 直接用 Dict[str, Any]）。
// Go 因 ToolCard.InputParams 类型为 []*Param 需要此转换。
func ParseJSONSchemaMap(schema map[string]any) ([]*Param, error)
```

**支持的字段完整列表**：

| JSON Schema 字段 | Param 字段 | 类型映射 |
|------------------|-----------|---------|
| `type` | `Type` | string→String, integer→Integer, number→Number, boolean→Boolean, array→Array, object→Object |
| `description` | `Description` | 直接赋值 |
| `required` | `Required` | 从顶层 required 列表匹配属性名 |
| `enum` | `Enum` | `[]any` 直接赋值 |
| `default` | `Default` | `any` 直接赋值 |
| `items` | `Items` | 递归调用 parsePropertyToParam |
| `properties` | `Properties` | 递归调用 parsePropertyToParam |
| `additionalProperties` | `AdditionalProperties` | bool 直接赋值 |
| `minimum` | `Minimum` | float64 直接赋值 |
| `maximum` | `Maximum` | float64 直接赋值 |
| `minLength` | `MinLength` | int 直接赋值 |
| `maxLength` | `MaxLength` | int 直接赋值 |
| `pattern` | `Pattern` | string 直接赋值 |
| `format` | `Format` | string 直接赋值 |
| `nullable` | `Nullable` | bool 直接赋值 |
| `minItems` | `MinItems` | int 直接赋值 |
| `maxItems` | `MaxItems` | int 直接赋值 |
| `anyOf` | `AnyOf` | 递归解析子 schema |
| `allOf` | `AllOf` | 递归解析子 schema |
| `oneOf` | `OneOf` | 递归解析子 schema |

**type 缺失时的行为**：返回 error（"property '%s' missing required 'type' field"）。修复 cron.go 后所有属性都有 type，此为防御性校验。

**实现结构**：

```go
func ParseJSONSchemaMap(schema map[string]any) ([]*Param, error) {
    // 1. 校验顶层 type == "object"
    // 2. 提取 properties map[string]any
    // 3. 提取 required []any → set
    // 4. 遍历 properties，调用 parsePropertyToParam
    // 5. 返回 []*Param
}

func parsePropertyToParam(name string, prop map[string]any, requiredSet map[string]bool) (*Param, error) {
    // 1. 解析 type → ParamType
    // 2. 解析 description, enum, default, nullable
    // 3. 解析约束：minimum/maximum/minLength/maxLength/pattern/format
    // 4. 解析 array 约束：items（递归）/minItems/maxItems
    // 5. 解析 object 约束：properties（递归）/additionalProperties/required
    // 6. 解析组合 schema：anyOf/allOf/oneOf（递归）
    // 7. 组装 Param 返回
}
```

### 2.4 BuildToolCard 统一建卡函数

在 `internal/agentcore/harness/prompts/tools/registry.go` 新增：

```go
// BuildToolCard 统一建卡函数。从 registry 获取 description + inputParams，
// 转换为 []*schema.Param 后构建 ToolCard。
//
// 对齐 Python: openjiuwen/harness/prompts/tools/__init__.py build_tool_card()
func BuildToolCard(
    name string,         // 工具名称（registry 查找键）
    toolID string,       // 工具 ID 前缀
    language string,     // 语言（"cn" / "en"）
    formatArgs map[string]string, // 描述占位符填充（可为 nil）
    agentID string,      // Agent 标识（可为空）
) (*tool.ToolCard, error)
```

**实现逻辑**（对齐 Python `build_tool_card`）：

1. 从 registry 查找 `ToolMetadataProvider`：`GetToolProvider(name)`
2. 获取 description：`provider.GetDescription(language)`
3. 如果有 formatArgs，用 `strings.Replace` / `fmt.Sprintf` 填充占位符
4. 获取 input_params：`provider.GetInputParams(language)` → `map[string]any`
5. 转换：`ParseJSONSchemaMap(inputParamsMap)` → `[]*schema.Param`
6. 生成 tool_id：`toolID + "_" + agentID`（agentID 非空）或 `toolID + "_" + uuid`
7. 构建 `tool.NewToolCardWithID(name, description, inputParams, toolID, properties)`

**注意**：当前 `NewToolCard` 的 ID 由 `NewBaseCard` 自动生成 UUID，不拼接 agentID。需要新增 `NewToolCardWithID` 或给 `NewToolCard` 增加 option 支持。对齐 Python 的 tool_id 生成逻辑。

### 2.5 tool_discovery 改造

#### SearchToolsTool

改造前：
```go
type SearchToolsTool struct {
    card *tool.ToolCard
    searchToolsFn func(...)
    appendTraceFn func(...)
    language string
    agentID string
}

func NewSearchToolsTool(...) *SearchToolsTool {
    provider := &tools.SearchToolsMetadataProvider{}
    desc := provider.GetDescription(language)
    inputParams := buildSearchToolsInputParams()  // 手动构建，硬编码中文
    card := tool.NewToolCard("search_tools", desc, inputParams, ...)
}

func (t *SearchToolsTool) Invoke(...) {
    query, _ := inputs["query"].(string)           // 手动断言，无校验
    limit := searchToolsLimitDefault               // 硬编码默认值
    ...
}
```

改造后：
```go
// SearchToolsInput search_tools 工具的输入参数。
type SearchToolsInput struct {
    Query       string `json:"query" jsonschema:"description=搜索候选工具的查询文本,required"`
    Limit       int    `json:"limit" jsonschema:"description=返回候选工具的最大数量,default=10"`
    DetailLevel int    `json:"detail_level" jsonschema:"description=1=name+描述, 2=+参数摘要, 3=+完整参数,default=1"`
}

func NewSearchToolsTool(searchFn, traceFn, language, agentID) tool.Tool {
    card, _ := tools.BuildToolCard("search_tools", "SearchToolsTool", language, nil, agentID)
    fn := func(ctx context.Context, input SearchToolsInput, opts ...tool.ToolOption) (map[string]any, error) {
        limit := clampLimit(input.Limit)
        matches, err := searchFn(ctx, input.Query, limit, input.DetailLevel)
        // ... 构建 trace + 结果 map ...
        return resultMap, nil
    }
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
    return invokeFn
}
```

**关键变化**：
- 删除 `SearchToolsTool` struct，直接返回 `*InvokeFunction[SearchToolsInput, map[string]any]`
- Input struct 加 `jsonschema` tag（虽然 schema 由 WithToolCard 覆盖，但 tag 作为文档参考保留）
- `Invoke` 自动走 `FormatWithSchema` 校验 + 默认值填充 + `map→struct` 解析
- `callability_note` / `next_step_hint` 严格对齐 Python 英文原文
- 删除 `buildSearchToolsInputParams()`、`buildCallabilityNote()`、`buildNextStepHint()`

#### LoadToolsTool

同理改造，`LoadToolsInput` struct + `InvokeFunction` 包装。

### 2.6 session_tools 改造

#### SessionsListTool

```go
// SessionsListInput 无输入参数的空 struct。
type SessionsListInput struct{}

func NewSessionsListTool(toolkit, language) tool.Tool {
    card, _ := tools.BuildToolCard("sessions_list", "sessions_list", language, nil, "")
    fn := func(ctx context.Context, _ SessionsListInput, opts ...tool.ToolOption) (map[string]any, error) {
        // ... 原有逻辑 ...
    }
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
    return invokeFn
}
```

#### SessionsSpawnTool

```go
type SessionsSpawnInput struct {
    SubagentType    string `json:"subagent_type" jsonschema:"description=子 agent 类型(如 general-purpose),required"`
    TaskDescription string `json:"task_description" jsonschema:"description=任务描述,required"`
}

func NewSessionsSpawnTool(provider, toolkit, language, agentID, availableAgents) tool.Tool {
    formatArgs := map[string]string{"available_agents": availableAgents}  // 如果非空
    card, _ := tools.BuildToolCard("sessions_spawn", "sessions_spawn", language, formatArgs, agentID)
    fn := func(ctx context.Context, input SessionsSpawnInput, opts ...tool.ToolOption) (map[string]any, error) {
        callOpts := tool.NewToolCallOptions(opts...)
        session := callOpts.Session
        // ... 从 provider 获取 loopController/taskManager ...
        // ... 用 input.SubagentType, input.TaskDescription ...
    }
    invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card))
    return invokeFn
}
```

#### SessionsCancelTool

```go
type SessionsCancelInput struct {
    TaskID string `json:"task_id" jsonschema:"description=要取消的任务 ID,required"`
}
```

同理改造。

### 2.7 提示词严格对齐 Python

所有 `callability_note` / `next_step_hint` 等提示词**严格对齐 Python 原文**：
- Python 有 cn/en 就保留 cn/en
- Python 只有英文就只用英文
- 不自定义中文翻译

具体变更：
- `SearchToolsTool` 的 `callability_note` → Python 原文："Search results are discovery-only. Tools shown here are not callable until load_tools is called."
- `SearchToolsTool` 的 `next_step_hint` → Python 原文："If the result is clear enough, call load_tools directly. Increase detail_level to 2 or 3 when you need more parameter detail."
- 删除 `buildCallabilityNote()` / `buildNextStepHint()` 中英文分支

### 2.3 SchemaUtils 递归校验补齐

当前 `SchemaUtils.Validate` 和 `FormatWithSchema` **只做顶层扁平校验**，不递归校验嵌套 object 的 properties 和 array 的 items，不校验新增的 MinItems/MaxItems/AdditionalProperties，不递归填充嵌套默认值。

Python 的 `validate_with_schema` 有两个分支：
- `schema: Dict[str,Any]` → 用 `jsonschema` 库完整校验（完全支持嵌套/约束/组合schema）
- `schema: Type[BaseModel]` → 用 Pydantic `model_validate` 校验（同样完全支持）

Go 对应两个入口，采用**两种路径并存**策略：

| Go 入口 | 对齐 Python 路径 | 校验方式 |
|---------|----------------|---------|
| `FormatWithSchema(data, []*Param)` | Pydantic Model 路径 | 手写递归校验（Go 无 Pydantic，手写对等） |
| `FormatWithSchemaMap(data, map[string]any)` | dict 路径 | `santhosh-tekuri/jsonschema/v6` 库校验 |

#### FormatWithSchemaMap 改用 jsonschema 库

项目已有 `santhosh-tekuri/jsonschema/v6` 依赖（go.mod indirect），可直接使用：

```go
c := jsonschema.NewCompiler()
c.AddResource("schema.json", schemaMap)
sch, _ := c.Compile("schema.json")
err := sch.Validate(data)
```

替换当前 `FormatWithSchemaMap` 中的手写校验逻辑。

#### FormatWithSchema 手写递归校验增强

1. `validateParamType` 增加 Array 递归校验 items、Object 递归校验 properties
2. `validateParamConstraints` 增加 MinItems/MaxItems 校验
3. `FormatWithSchema` 增加递归默认值填充（嵌套 object 的 properties 默认值）

## 3. 影响范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `common/schema/param.go` | 修改 | 新增 3 字段 + ParseJSONSchemaMap + 修改 paramToSchema/Validate |
| `foundation/tool/schema_utils.go` | 修改 | 递归校验 + FormatWithSchemaMap 用 jsonschema 库 + 递归默认值填充 |
| `foundation/tool/schema_utils_test.go` | 修改 | 新增递归校验测试 |
| `common/schema/param_test.go` | 修改 | 新增测试 |
| `harness/prompts/tools/registry.go` | 修改 | 新增 BuildToolCard |
| `harness/prompts/tools/cron.go` | 修改 | 补 type 字段 |
| `harness/tools/tool_discovery/search_tools.go` | 重写 | 改用 InvokeFunction 包装 |
| `harness/tools/tool_discovery/search_tools_test.go` | 修改 | 适配新接口 |
| `harness/tools/tool_discovery/load_tools.go` | 重写 | 改用 InvokeFunction 包装 |
| `harness/tools/tool_discovery/load_tools_test.go` | 修改 | 适配新接口 |
| `harness/tools/subagent/session_tools.go` | 重写 | 改用 InvokeFunction 包装 |
| `harness/tools/subagent/session_tools_test.go` | 修改 | 适配新接口 |
| `foundation/tool/base.go` | 修改 | 新增 NewToolCardWithID（支持指定 ID） |

## 4. 不变更

- **循环依赖 any 使用**：`PerAgentCallbackFunc` 第二参数保持 `any`，这是避免循环依赖的必要设计
- **prompts/tools 的 init() 注册机制**：已对齐 Python 的 _REGISTRY，无需改动
- **foundation/tool 的 InvokeFunction/SchemaUtils 逻辑**：本身正确，只是 harness tools 之前没用上
