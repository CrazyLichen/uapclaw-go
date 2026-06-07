# 领域 3.3+3.12 — LocalFunction 与 SchemaUtils 设计

> 对应 Python 源码：
> - `openjiuwen/core/foundation/tool/function/function.py` (LocalFunction)
> - `openjiuwen/core/foundation/tool/tool.py` (@tool 装饰器)
> - `openjiuwen/core/foundation/tool/utils/callable_schema_extractor.py` (参数 schema 自动提取)
> - `openjiuwen/core/foundation/tool/utils/type_schema_extractor.py` (类型→JSON Schema 提取器注册表)
> - `openjiuwen/core/common/utils/schema_utils.py` (SchemaUtils)
>
> 同时覆盖实现计划 3.4（@tool 装饰器等价）和 3.12（Tool 工具函数 / SchemaUtils）。

## 1. 概述

LocalFunction 是将 Go 函数包装为 Tool 的核心机制。用户定义强类型 Input/Output struct，
通过泛型 `NewInvokeFunction` / `NewStreamFunction` 注册为工具，LLM 可通过 function calling 调用。

本设计同时实现 SchemaUtils（格式化/校验/类型转换），因为 LocalFunction 的 Invoke 流程强依赖这些能力。

## 2. 核心决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 整体方案 | 混合模式：struct 反射自动提取为主，手动 `[]*Param` 降级 | 兼顾 Go 惯用（struct tag）和 Python 对齐（auto_extract），手动模式留后路 |
| 函数签名 | 强类型泛型 `func(ctx, Input) (Output, error)` | 类型安全，MCP Go SDK 同款模式，struct tag 可自动提取 schema |
| 便捷注册 API | 函数式 `Tool(fn, WithName(...))` | Go 惯用的 functional options 模式 |
| SchemaUtils 归属 | 3.3 节一起实现 | LocalFunction 强依赖 SchemaUtils，拆开实现不自然；3.12 标记为已提前完成 |
| Struct Tag | 复用 `jsonschema` tag | 与 MCP SDK / invopop/jsonschema 生态兼容 |
| map→struct 转换 | `json.Marshal(map) → json.Unmarshal(&struct)` + 前置 SchemaUtils 格式化 | 利用 JSON 序列化做类型转换，SchemaUtils 负责校验/默认值/null 处理 |
| 流式函数 | `func(ctx, Input) (<-chan Output, error)` 签名 | 与 Tool.Stream 返回 `<-chan StreamChunk` 对齐 |
| 降级模式 | `NewMapFunction(card, fn)` 接受 `func(ctx, map[string]any) (map[string]any, error)` | 不支持自动提取 schema 的场景（如动态参数） |
| 泛型推断 | 函数参数声明为 `func(ctx, I) (O, error)` 而非 `any` | Go 编译器可自动推断 I/O，用户无需显式写 `NewInvokeFunction[SearchInput, SearchOutput](...)` |

## 3. 数据流

### 3.1 注册阶段：struct → schema

```
用户定义 SearchInput struct（带 json + jsonschema tag）
    ↓
NewInvokeFunction("search", Search) 或 Tool(Search)
    ↓ Go 编译器自动推断 I=SearchInput, O=SearchOutput
    ↓ 内部调用
StructSchemaExtractor.Extract(reflect.TypeOf((*SearchInput)(nil)).Elem())
    ↓ 反射 json:"name" → Param.Name
    ↓ 反射 jsonschema:"description=xxx,required" → Param.Description, Param.Required
    ↓ 反射 jsonschema:"default=10" → Param.Default
    ↓ 反射 jsonschema:"enum=a,b" → JSON Schema enum
    ↓ 递归处理嵌套 struct（→ ParamTypeObject）和 slice（→ ParamTypeArray）
[]*schema.Param
    ↓ 存入 ToolCard.InputParams
ToolCard.ToolInfo() → LLM function calling 可消费的 JSON Schema
```

### 3.2 调用阶段：LLM 输出 → 用户函数

```
LLM 返回 ToolCall.Arguments = map[string]any
    ↓
LocalFunction.Invoke(ctx, inputs, opts)  // InvokeFunction[I,O]
    ↓ 1. SchemaUtils.RemoveNoneValues(inputs)  ← 如果 opts.SkipNoneValue
    ↓ 2. SchemaUtils.Validate(inputs, card.InputParams)  ← 如果 !opts.SkipInputsValidate
    ↓ 3. SchemaUtils.FormatWithSchema(inputs, card.InputParams)  ← 填充默认值
    ↓ 4. json.Marshal(formatted) → json.Unmarshal(&Input{})
用户函数: Search(ctx, input)
    ↓ 5. json.Marshal(output) → map[string]any
返回 map[string]any
```

### 3.3 流式调用阶段

```
LLM 返回 ToolCall.Arguments = map[string]any
    ↓
StreamFunction.Stream(ctx, inputs, opts)  // StreamFunction[I,O]
    ↓ 同 Invoke 的 1-4 步骤
用户函数返回 <-chan Output
    ↓ 5. 逐个 chunk: json.Marshal(chunk) → StreamChunk{Data: map}
返回 <-chan StreamChunk
```

## 4. 类型定义

### 4.1 InvokeFunction 泛型结构体

```go
// InvokeFunction 本地函数工具（Invoke 模式），将 Go 函数包装为 Tool。
//
// 用户函数签名：func(ctx context.Context, input I) (O, error)
//
// 对应 Python: openjiuwen/core/foundation/tool/function/function.py (LocalFunction)
// Python 不区分 Invoke/Stream，Go 通过不同类型在编译期保证签名正确。
type InvokeFunction[I any, O any] struct {
    card *ToolCard
    fn   func(context.Context, I) (O, error)
}
```

### 4.2 StreamFunction 泛型结构体

```go
// StreamFunction 本地函数工具（Stream 模式），将 Go 流式函数包装为 Tool。
//
// 用户函数签名：func(ctx context.Context, input I) (<-chan O, error)
//
// 对应 Python: openjiuwen/core/foundation/tool/function/function.py (LocalFunction.stream)
type StreamFunction[I any, O any] struct {
    card *ToolCard
    fn   func(context.Context, I) (<-chan O, error)
}
```

### 4.2 LocalFunction 降级结构体（弱类型 map）

```go
// MapFunction 弱类型 map 函数工具，降级模式。
//
// 当函数参数无法用 struct 描述时（如动态参数），使用 MapFunction 代替 LocalFunction。
// 用户需手动提供 InputParams。
//
// 对应 Python: LocalFunction(func=None) 的降级场景
type MapFunction struct {
    card       *ToolCard
    invokeFn   func(ctx context.Context, inputs map[string]any) (map[string]any, error)
    streamFn   func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error)
}
```

### 4.3 SchemaUtils

```go
// SchemaUtils 工具参数 Schema 工具类，提供校验、格式化、类型转换能力。
//
// 对应 Python: openjiuwen/core/common/utils/schema_utils.py (SchemaUtils)
type SchemaUtils struct{}
```

### 4.4 StructSchemaExtractor

```go
// StructSchemaExtractor 从 Go struct 反射提取 []*schema.Param。
//
// 读取 struct 字段的 json tag（参数名）和 jsonschema tag（描述/必填/默认值/枚举），
// 递归处理嵌套 struct 和 slice，生成完整的参数定义列表。
//
// 对应 Python:
//   - openjiuwen/core/foundation/tool/utils/callable_schema_extractor.py (CallableSchemaExtractor)
//   - openjiuwen/core/foundation/tool/utils/type_schema_extractor.py (TypeSchemaExtractor 注册表)
type StructSchemaExtractor struct{}
```

## 5. API 定义

### 5.1 构造函数

```go
// NewInvokeFunction 创建 Invoke 模式的本地函数工具。
//
// 自动从 I 类型的 struct tag 提取 InputParams 填入 ToolCard。
// Go 编译器从 fn 参数自动推断 I 和 O，用户通常无需显式指定泛型参数。
//
// 使用示例：
//
//	fn, _ := NewInvokeFunction("search", Search)
//	// 等价于 NewInvokeFunction[SearchInput, SearchOutput]("search", Search)
//
// 对应 Python: LocalFunction(card=card, func=func)
func NewInvokeFunction[I any, O any](name string, fn func(context.Context, I) (O, error), opts ...LocalFuncOption) (*InvokeFunction[I, O], error)

// NewStreamFunction 创建 Stream 模式的本地函数工具。
//
// 使用示例：
//
//	fn, _ := NewStreamFunction("stream_search", StreamSearch)
//
func NewStreamFunction[I any, O any](name string, fn func(context.Context, I) (<-chan O, error), opts ...LocalFuncOption) (*StreamFunction[I, O], error)

// NewMapFunction 创建弱类型 map 函数工具（降级模式）。
//
// invokeFn / streamFn 二选一，另一个传 nil。
// card.InputParams 由用户手动提供。
func NewMapFunction(card *ToolCard,
    invokeFn func(ctx context.Context, inputs map[string]any) (map[string]any, error),
    streamFn func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error),
) (*MapFunction, error)

// LocalFuncOption 本地函数构造选项。
type LocalFuncOption func(*localFuncConfig)

// localFuncConfig 内部配置。
type localFuncConfig struct {
    description string
    inputParams []*schema.Param  // 手动覆盖 InputParams（降级）
    card        *ToolCard        // 预构建卡片
}

// WithDescription 设置工具描述（覆盖自动提取）。
func WithDescription(desc string) LocalFuncOption

// WithInputParams 手动设置输入参数（覆盖自动提取）。
func WithInputParams(params []*schema.Param) LocalFuncOption

// WithCard 使用预构建的 ToolCard。
func WithCard(card *ToolCard) LocalFuncOption
```

### 5.2 InvokeFunction 方法

```go
// Card 返回工具配置卡片。
func (f *InvokeFunction[I, O]) Card() *ToolCard

// Invoke 执行工具调用：校验输入 → 格式化 → map→struct → 调用用户函数 → struct→map。
func (f *InvokeFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)

// Stream 不支持流式调用，返回 ErrStreamNotSupported。
func (f *InvokeFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
```

### 5.3 StreamFunction 方法

```go
// Card 返回工具配置卡片。
func (f *StreamFunction[I, O]) Card() *ToolCard

// Invoke 不支持一次性调用，返回 ErrStreamNotSupported。
func (f *StreamFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)

// Stream 流式执行工具调用：校验输入 → 格式化 → map→struct → 调用用户流式函数 → 逐 chunk 转换。
func (f *StreamFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
```

### 5.4 便捷注册函数（3.4 @tool 装饰器等价）

```go
// ToolFuncOption 工具注册选项函数。
type ToolFuncOption func(*toolFuncConfig)

// toolFuncConfig 内部配置，由 ToolFuncOption 填充。
type toolFuncConfig struct {
    name        string
    description string
    inputParams []*schema.Param  // 手动覆盖（降级模式）
    card        *ToolCard        // 预构建卡片
}

// WithToolName 设置工具名称（覆盖函数名）。
func WithToolName(name string) ToolFuncOption

// WithToolDescription 设置工具描述（覆盖自动提取）。
func WithToolDescription(desc string) ToolFuncOption

// WithToolInputParams 手动设置输入参数（覆盖自动提取）。
func WithToolInputParams(params []*schema.Param) ToolFuncOption

// WithToolCard 使用预构建的 ToolCard。
func WithToolCard(card *ToolCard) ToolFuncOption

// Tool 便捷注册函数（Invoke 模式），对标 Python @tool 装饰器。
//
// Go 编译器从 fn 参数自动推断 I 和 O，用户无需显式指定泛型参数。
//
// 使用示例：
//
//	// 最简写法：自动推断 I/O，自动提取 schema
//	fn, _ := Tool(Search)
//
//	// 自定义名称和描述
//	fn, _ := Tool(Search, WithToolName("custom_search"), WithToolDescription("搜索工具"))
//
//	// 手动指定参数（覆盖自动提取）
//	fn, _ := Tool(Search, WithToolInputParams(params))
func Tool[I any, O any](fn func(context.Context, I) (O, error), opts ...ToolFuncOption) (*InvokeFunction[I, O], error)

// StreamTool 便捷注册函数（Stream 模式）。
//
// 使用示例：
//
//	fn, _ := StreamTool(StreamSearch, WithToolName("stream_search"))
func StreamTool[I any, O any](fn func(context.Context, I) (<-chan O, error), opts ...ToolFuncOption) (*StreamFunction[I, O], error)
```

### 5.4 SchemaUtils 方法

```go
// FormatWithSchema 根据参数 schema 格式化输入数据，填充默认值。
//
// 流程：RemoveNoneValues（可选）→ Validate（可选）→ 填充默认值
//
// 对应 Python: SchemaUtils.format_with_schema()
func (SchemaUtils) FormatWithSchema(data map[string]any, params []*schema.Param, opts ...FormatOption) (map[string]any, error)

// Validate 校验输入数据是否符合参数 schema。
//
// 检查必填字段是否存在、类型是否匹配、枚举值是否合法。
//
// 对应 Python: SchemaUtils.validate_with_schema()
func (SchemaUtils) Validate(data map[string]any, params []*schema.Param) error

// RemoveNoneValues 递归移除 map 中的 nil 值。
//
// 对应 Python: SchemaUtils.remove_none_values()
func (SchemaUtils) RemoveNoneValues(data map[string]any) map[string]any
```

### 5.5 StructSchemaExtractor 方法

```go
// Extract 从 Go struct 类型反射提取 []*schema.Param。
//
// 读取字段标签：
//   - json:"name" → Param.Name（也用于判断是否导出：无 json tag 的字段跳过）
//   - json:"name,omitempty" → Param.Required=false（omitempty 表示非必填）
//   - jsonschema:"description=搜索关键词" → Param.Description
//   - jsonschema:"required" → Param.Required=true
//   - jsonschema:"default=10" → Param.Default
//   - jsonschema:"enum=a|b|c" → JSON Schema enum（用 | 分隔）
//
// 递归处理规则：
//   - 嵌套 struct → ParamTypeObject，递归提取 Properties
//   - []T / []*T → ParamTypeArray，递归提取 Items
//   - *T → 解引用后按 T 处理
//   - 基本类型 → 直接映射 ParamType
//
// 对应 Python:
//   - CallableSchemaExtractor.generate_schema()
//   - TypeSchemaExtractor 注册表
func (StructSchemaExtractor) Extract(typ reflect.Type) ([]*schema.Param, error)

// ExtractDescription 从 struct 提取工具描述（从首个字段前的注释或 jsonschema:"title=xxx"）。
func (StructSchemaExtractor) ExtractDescription(typ reflect.Type) string
```

## 6. jsonschema Tag 解析规范

### 6.1 支持的 tag 键值对

| Tag 键 | 示例 | 映射到 | 说明 |
|--------|------|--------|------|
| `description` | `jsonschema:"description=搜索关键词"` | `Param.Description` | 参数描述 |
| `required` | `jsonschema:"required"` | `Param.Required=true` | 标记必填（覆盖 omitempty 推断） |
| `default` | `jsonschema:"default=10"` | `Param.Default` | 默认值（字符串形式，运行时类型转换） |
| `enum` | `jsonschema:"enum=asc|desc"` | JSON Schema `enum` | 枚举值，用 `\|` 分隔 |
| `title` | `jsonschema:"title=搜索参数"` | JSON Schema `title` | 标题（可选） |
| `minLength` | `jsonschema:"minLength=1"` | JSON Schema `minLength` | 字符串最小长度 |
| `maxLength` | `jsonschema:"maxLength=100"` | JSON Schema `maxLength` | 字符串最大长度 |
| `minimum` | `jsonschema:"minimum=0"` | JSON Schema `minimum` | 数值最小值 |
| `maximum` | `jsonschema:"maximum=100"` | JSON Schema `maximum` | 数值最大值 |
| `pattern` | `jsonschema:"pattern=^[a-z]+$"` | JSON Schema `pattern` | 正则校验 |
| `format` | `jsonschema:"format=email"` | JSON Schema `format` | 格式约束 |

多个键值对用逗号分隔：`jsonschema:"description=搜索关键词,required,maxLength=100"`

### 6.2 Go 类型 → ParamType 映射

| Go 类型 | ParamType | JSON Schema type |
|---------|-----------|-----------------|
| `string` | `ParamTypeString` | `"string"` |
| `bool` | `ParamTypeBoolean` | `"boolean"` |
| `int`, `int8`, `int16`, `int32`, `int64` | `ParamTypeInteger` | `"integer"` |
| `float32`, `float64` | `ParamTypeNumber` | `"number"` |
| `[]T`, `[]*T` | `ParamTypeArray` | `"array"` |
| 嵌套 struct | `ParamTypeObject` | `"object"` |
| `any` / `interface{}` | `ParamTypeObject` | `"object"` |

### 6.3 Required 推断规则

1. 如果字段有 `jsonschema:"required"` → `Required=true`
2. 否则，如果字段有 `json:",omitempty"` → `Required=false`
3. 否则（无 omitempty）→ `Required=true`（Go 默认值不可零值推断是否"有意缺失"，保守设为必填）

### 6.4 完整示例

```go
// SearchInput 搜索工具输入参数
type SearchInput struct {
    // Query 搜索关键词
    Query string `json:"query" jsonschema:"description=搜索关键词,required,minLength=1,maxLength=200"`
    // Limit 返回数量上限
    Limit int `json:"limit,omitempty" jsonschema:"description=返回数量上限,default=10,maximum=100"`
    // Sort 排序方式
    Sort string `json:"sort,omitempty" jsonschema:"description=排序方式,enum=asc|desc,default=asc"`
    // Filters 过滤条件
    Filters *SearchFilters `json:"filters,omitempty" jsonschema:"description=过滤条件"`
}

// SearchFilters 搜索过滤条件
type SearchFilters struct {
    // Category 分类名称
    Category string `json:"category,omitempty" jsonschema:"description=分类名称"`
    // Tags 标签列表
    Tags []string `json:"tags,omitempty" jsonschema:"description=标签列表"`
}
```

生成的 `[]*schema.Param`：

```
SearchInput:
  - Query  (String,  required=true, default=nil, description="搜索关键词", minLength=1, maxLength=200)
  - Limit  (Integer, required=false, default=10,  description="返回数量上限", maximum=100)
  - Sort   (String,  required=false, default="asc", description="排序方式", enum=["asc","desc"])
  - Filters (Object, required=false, default=nil, description="过滤条件")
      - Category (String, required=false, description="分类名称")
      - Tags     (Array[String], required=false, description="标签列表")
```

## 7. SchemaUtils 实现细节

### 7.1 FormatWithSchema 流程

```go
func (SchemaUtils) FormatWithSchema(data map[string]any, params []*schema.Param, opts ...FormatOption) (map[string]any, error) {
    o := newFormatOptions(opts...)

    // 1. 可选：移除 nil 值
    if o.skipNoneValue {
        data = SchemaUtils{}.RemoveNoneValues(data)
    }

    // 2. 可选：校验
    if !o.skipValidate {
        if err := SchemaUtils{}.Validate(data, params); err != nil {
            return nil, err
        }
    }

    // 3. 填充默认值
    result := make(map[string]any)
    for _, p := range params {
        if val, ok := data[p.Name]; ok {
            result[p.Name] = val
        } else if p.Default != nil {
            result[p.Name] = p.Default
        } else if p.Required {
            return nil, buildError(StatusSchemaFormatInvalid, "missing required param", p.Name)
        }
        // 非必填且无默认值：不填充（用户函数 struct 字段保持零值）
    }

    // 4. 保留 data 中不在 params 中的额外字段（additionalProperties 语义）
    for k, v := range data {
        if _, ok := result[k]; !ok {
            result[k] = v
        }
    }

    return result, nil
}
```

### 7.2 Validate 校验规则

```go
func (SchemaUtils) Validate(data map[string]any, params []*schema.Param) error {
    paramMap := make(map[string]*schema.Param, len(params))
    for _, p := range params {
        paramMap[p.Name] = p
    }

    // 1. 检查必填字段
    for _, p := range params {
        if p.Required {
            if _, ok := data[p.Name]; !ok {
                return buildError(StatusSchemaValidateInvalid, "missing required param", p.Name)
            }
        }
    }

    // 2. 检查类型匹配
    for key, val := range data {
        p, ok := paramMap[key]
        if !ok {
            continue // 额外字段不校验
        }
        if err := validateType(key, val, p); err != nil {
            return err
        }
    }

    return nil
}
```

类型校验规则（参考 uapclaw-main 的 `validateToolArgs`）：

| ParamType | Go `map[string]any` 中的实际类型 | 校验逻辑 |
|-----------|-------------------------------|---------|
| `ParamTypeString` | `string` | `_, ok := val.(string)` |
| `ParamTypeBoolean` | `bool` | `_, ok := val.(bool)` |
| `ParamTypeInteger` | `float64` (JSON 数字) | `f, ok := val.(float64); ok && f == math.Trunc(f)` |
| `ParamTypeNumber` | `float64` | `_, ok := val.(float64)` |
| `ParamTypeArray` | `[]any` | 递归校验 items |
| `ParamTypeObject` | `map[string]any` | 递归校验 properties |

### 7.3 RemoveNoneValues

```go
func (SchemaUtils) RemoveNoneValues(data map[string]any) map[string]any {
    if data == nil {
        return nil
    }
    result := make(map[string]any, len(data))
    for k, v := range data {
        if v == nil {
            continue
        }
        switch tv := v.(type) {
        case map[string]any:
            cleaned := SchemaUtils{}.RemoveNoneValues(tv)
            if cleaned != nil {
                result[k] = cleaned
            }
        case []any:
            cleaned := removeNoneFromArray(tv)
            if cleaned != nil {
                result[k] = cleaned
            }
        default:
            result[k] = v
        }
    }
    if len(result) == 0 {
        return nil
    }
    return result
}
```

## 8. InvokeFunction.Invoke 实现细节

```go
func (f *InvokeFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
    o := NewToolCallOptions(opts...)

    // 1. 参数格式化（校验 + 默认值填充 + null 处理）
    if f.card.InputParams != nil {
        formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
            WithSkipNoneValue(o.SkipNoneValue),
            WithSkipValidate(o.SkipInputsValidate),
        )
        if err != nil {
            return nil, err
        }
        inputs = formatted
    }

    // 2. map → struct（JSON 序列化中转）
    jsonBytes, err := json.Marshal(inputs)
    if err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "marshal inputs failed", err)
    }
    var input I
    if err := json.Unmarshal(jsonBytes, &input); err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "unmarshal inputs to struct failed", err)
    }

    // 3. 调用用户函数（强类型，无需类型断言）
    output, err := f.fn(ctx, input)
    if err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "function execution failed", err)
    }

    // 4. struct → map（JSON 序列化中转）
    jsonBytes, err = json.Marshal(output)
    if err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "marshal output failed", err)
    }
    var result map[string]any
    if err := json.Unmarshal(jsonBytes, &result); err != nil {
        // 非 struct 输出（如 string），包装为 {"result": output}
        result = map[string]any{"result": output}
    }

    return result, nil
}

func (f *InvokeFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
    return nil, NewErrStreamNotSupported(f.card.String())
}
```

## 9. StreamFunction.Stream 实现细节

```go
func (f *StreamFunction[I, O]) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
    return nil, NewErrStreamNotSupported(f.card.String())
}

func (f *StreamFunction[I, O]) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
    o := NewToolCallOptions(opts...)

    // 1. 参数格式化（同 InvokeFunction）
    if f.card.InputParams != nil {
        formatted, err := SchemaUtils{}.FormatWithSchema(inputs, f.card.InputParams,
            WithSkipNoneValue(o.SkipNoneValue),
            WithSkipValidate(o.SkipInputsValidate),
        )
        if err != nil {
            return nil, err
        }
        inputs = formatted
    }

    // 2. map → struct
    jsonBytes, err := json.Marshal(inputs)
    if err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "marshal inputs failed", err)
    }
    var input I
    if err := json.Unmarshal(jsonBytes, &input); err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "unmarshal inputs to struct failed", err)
    }

    // 3. 调用用户流式函数（强类型，无需类型断言）
    ch, err := f.fn(ctx, input)
    if err != nil {
        return nil, buildError(StatusToolLocalFunctionExecutionError, "stream function failed", err)
    }

    // 4. 包装输出 channel：O → StreamChunk
    outCh := make(chan StreamChunk, 1)
    go func() {
        defer close(outCh)
        for chunk := range ch {
            data, err := structToMap(chunk)
            if err != nil {
                outCh <- StreamChunk{Error: err}
                return
            }
            outCh <- StreamChunk{Data: data}
        }
        outCh <- StreamChunk{Done: true}
    }()

    return outCh, nil
}
```

## 10. Tool() / StreamTool() 便捷函数实现细节

对标 Python `@tool` 装饰器的多种用法。Go 编译器从函数参数自动推断泛型 I/O，schema 从 I 类型的 struct tag 自动提取。

```go
func Tool[I any, O any](fn func(context.Context, I) (O, error), opts ...ToolFuncOption) (*InvokeFunction[I, O], error) {
    cfg := &toolFuncConfig{}
    for _, opt := range opts {
        opt(cfg)
    }

    // 确定名称（从函数名提取，或由选项覆盖）
    name := cfg.name
    if name == "" {
        name = extractFuncName(fn)  // 通过 runtime.FuncForPC 提取函数名
    }

    // 确定 InputParams（从 I 类型反射提取，或由选项覆盖）
    var inputParams []*schema.Param
    if cfg.inputParams != nil {
        inputParams = cfg.inputParams
    } else {
        typ := reflect.TypeOf((*I)(nil)).Elem()
        if typ.Kind() == reflect.Pointer {
            typ = typ.Elem()
        }
        extracted, err := StructSchemaExtractor{}.Extract(typ)
        if err != nil {
            return nil, fmt.Errorf("auto-extract schema failed: %w", err)
        }
        inputParams = extracted
    }

    // 确定描述
    description := cfg.description
    if description == "" {
        typ := reflect.TypeOf((*I)(nil)).Elem()
        if typ.Kind() == reflect.Pointer {
            typ = typ.Elem()
        }
        description = StructSchemaExtractor{}.ExtractDescription(typ)
    }
    if description == "" {
        description = name
    }

    // 构建 ToolCard
    var card *ToolCard
    if cfg.card != nil {
        card = cfg.card
    } else {
        card = NewToolCard(name, description, inputParams, nil)
    }

    return &InvokeFunction[I, O]{card: card, fn: fn}, nil
}
```

## 11. 错误处理

| 错误场景 | StatusCode | 触发位置 |
|---------|------------|---------|
| 函数签名不合法 | `StatusToolLocalFunctionFuncNotSupported (182200)` | `NewLocalFunction` 构造时 |
| 函数执行失败 | `StatusToolLocalFunctionExecutionError (182205)` | `Invoke` / `Stream` 运行时 |
| 参数缺失必填字段 | `StatusSchemaValidateInvalid (189001)` | `SchemaUtils.Validate` |
| 参数格式化失败 | `StatusSchemaFormatInvalid (189002)` | `SchemaUtils.FormatWithSchema` |
| 不支持 Stream | `StatusToolStreamNotSupported (182010)` | `Invoke 模式调用 Stream` |

## 12. 日志对齐

对照 Python LocalFunction 的 logger 调用，Go 代码中等价位置需补充日志：

| Python 日志 | Go 等价 |
|------------|--------|
| `ToolCallEvents.TOOL_PARSE_STARTED` | 由 LifecycleTool 回调处理（已有） |
| `ToolCallEvents.TOOL_PARSE_FINISHED` | 由 LifecycleTool 回调处理（已有） |
| `TOOL_LOCAL_FUNCTION_FUNC_NOT_SUPPORTED` 异常 | `logger.Error(logComponent).Err(err).Str("card", card).Msg("func not supported")` | 仅 MapFunction 降级场景使用 |
| `TOOL_LOCAL_FUNCTION_EXECUTION_ERROR` 异常 | `logger.Error(logComponent).Err(err).Str("method", "invoke").Msg("execution error")` | InvokeFunction.Invoke / StreamFunction.Stream 中 |
| `TOOL_LOCAL_FUNCTION_EXECUTION_ERROR` 异常 | `logger.Error(logComponent).Err(err).Str("method", "stream").Msg("execution error")` | StreamFunction.Stream 中 |

## 13. 与 Python 的差异说明

| 维度 | Python | Go | 差异原因 |
|------|--------|-----|---------|
| 函数签名 | 通用 `Callable`，`**kwargs` 展开 | 强类型泛型 `func(ctx, I) (O, error)` | Go 无 `**kwargs`，泛型 + struct 更类型安全 |
| `*args` 支持 | `support_args_param` 处理 VAR_POSITIONAL | 不支持 | Go 无可变位置参数语义 |
| `**kwargs` 支持 | 检测 VAR_KEYWORD → `additionalProperties: true` | 不支持 | Go 无可变关键字参数语义 |
| docstring 自动提取 | `inspect.getdoc()` + 正则解析参数描述 | 不支持（Go 注释编译后不可获取） | 用 `jsonschema:"description=xxx"` tag 替代 |
| Pydantic BaseModel schema | `model_json_schema()` 自动生成 | `StructSchemaExtractor` 反射提取 | Go 无 Pydantic，自建反射 |
| `format_with_schema` | Pydantic `model_validate` + `model_dump` | `json.Marshal → json.Unmarshal` 中转 | Go 无 Pydantic，JSON 序列化做类型转换 |
| 异步/生成器区分 | `inspect.iscoroutinefunction` / `inspect.isgeneratorfunction` | 两个独立类型 `InvokeFunction` / `StreamFunction`，编译期区分 | Go 用类型系统代替运行时检测，编译期保证签名正确 |
| LocalFunction 类型 | 统一 `LocalFunction`，运行时检测函数类型 | `InvokeFunction[I,O]` + `StreamFunction[I,O]` 两个泛型类型 | Go 无运行时函数类型检测，拆分为两个类型可在编译期保证签名正确 |
| 元类生命周期注入 | `_ToolMeta.__call__` 自动包装 invoke/stream | `LifecycleTool` 手动包装 | Go 无元类，包装器模式替代 |

## 14. 文件规划

```
internal/agentcore/foundation/tool/
├── doc.go                          # 包文档（更新文件目录）
├── base.go                         # 已有：Tool 接口 + ToolCard + ToolOption
├── tool_info.go                    # 已有：ToolCard.ToolInfo()
├── lifecycle_tool.go               # 已有：LifecycleTool 包装器
├── invoke_function.go              # 新增：InvokeFunction[I,O] 泛型（Invoke 模式）
├── stream_function.go              # 新增：StreamFunction[I,O] 泛型（Stream 模式）
├── map_function.go                 # 新增：MapFunction 降级（弱类型 map）
├── tool_func.go                    # 新增：Tool() / StreamTool() 便捷函数 + ToolFuncOption
├── schema_utils.go                 # 新增：SchemaUtils（FormatWithSchema/Validate/RemoveNoneValues）
├── struct_schema_extractor.go      # 新增：struct → []*Param 反射提取器
├── invoke_function_test.go         # 新增：InvokeFunction 测试
├── stream_function_test.go         # 新增：StreamFunction 测试
├── map_function_test.go            # 新增：MapFunction 测试
├── tool_func_test.go               # 新增：Tool()/StreamTool() 便捷函数测试
├── schema_utils_test.go            # 新增：SchemaUtils 测试
└── struct_schema_extractor_test.go # 新增：反射提取器测试
```

## 15. 测试策略

### 15.1 StructSchemaExtractor 测试

- 基本类型映射：string/bool/int/float → 正确的 ParamType
- json tag 解析：字段名、omitempty → Required
- jsonschema tag 解析：description/required/default/enum/minLength/maxLength/minimum/maximum
- 嵌套 struct → ParamTypeObject 递归
- slice 类型 → ParamTypeArray 递归
- 指针类型解引用
- 无 json tag 的字段跳过
- 空 struct 返回空列表

### 15.2 SchemaUtils 测试

- FormatWithSchema：默认值填充、必填校验、额外字段保留
- Validate：类型匹配、必填检查、枚举校验、嵌套对象校验
- RemoveNoneValues：递归移除、空 map 返回 nil
- float64→int 类型校验（JSON 数字精度）

### 15.3 InvokeFunction 测试

- NewInvokeFunction 正确创建（自动推断泛型参数）
- Invoke：完整流程 map→struct→fn→map
- Invoke：默认值填充
- Invoke：必填缺失返回错误
- Invoke：float64→int 类型转换（JSON 数字精度）
- Stream：返回 ErrStreamNotSupported

### 15.4 StreamFunction 测试

- NewStreamFunction 正确创建
- Stream：正确包装 channel，逐 chunk 输出
- Stream：默认值填充
- Stream：channel 关闭后发送 Done
- Invoke：返回 ErrStreamNotSupported

### 15.5 MapFunction 测试

- NewMapFunction 正确创建
- Invoke/Stream：直接透传 map，不做 struct 转换

### 15.6 Tool() / StreamTool() 便捷函数测试

- 最简用法 Tool(fn) — 自动推断
- WithToolName / WithToolDescription
- WithToolInputParams（覆盖自动提取）
- WithToolCard

### 15.7 覆盖率目标

≥ 85%，所有导出函数和关键非导出函数均需覆盖。
