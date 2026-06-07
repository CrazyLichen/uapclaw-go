# Tool 接口与 ToolCard + LifecycleTool 包装器 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域 3.1 的 Tool 接口、ToolCard、ToolOption、StreamChunk 和 LifecycleTool 包装器，为后续子类（LocalFunction/MCPTool/RestfulApi）提供统一抽象基础。

**Architecture:** Tool 接口只定义纯业务方法（Invoke/Stream/Card），LifecycleTool 包装器负责在调用前后触发 ToolCallEvents 生命周期回调，AbilityManager 注册时自动包装。ToolCard 嵌入 BaseCard 并增加 InputParams ([]*Param) 和 Properties 字段。ToolOption 函数式选项传递扩展参数。

**Tech Stack:** Go 1.23, 依赖 common/schema (BaseCard/ToolInfo/Param), common/exception (StatusCode), agentcore/foundation/llm/callback (CallbackFramework)

---

## 文件结构

```
internal/agentcore/foundation/tool/
├── doc.go                       # 包文档 [Task 1]
├── base.go                      # Tool 接口 + ToolCard + ToolOption + ToolCallOptions + StreamChunk [Task 2]
├── base_test.go                 # ToolCard/ToolOption/StreamChunk 测试 [Task 3]
├── tool_info.go                 # ToolCard.ToolInfo() — Param→JSON Schema 转换 [Task 4]
├── tool_info_test.go            # ToolInfo 转换测试 [Task 5]
├── lifecycle_tool.go            # LifecycleTool 包装器 [Task 6]
├── lifecycle_tool_test.go       # LifecycleTool 测试 [Task 7]
├── schema/
│   └── tool_info.go             # ToolCallEventType + ToolCallEventData 定义 [Task 8]
│   └── tool_info_test.go        # ToolCallEventType 测试 [Task 9]
```

**修改文件：**
- `internal/common/schema/tool_info.go` — 已有 ToolInfo/McpToolInfo，无需修改
- `internal/common/schema/param.go` — 已有 Param，需增加 `ToJSONSchemaMap()` 方法 [Task 4]

---

### Task 1: 创建包目录和 doc.go

**Files:**
- Create: `internal/agentcore/foundation/tool/doc.go`

- [ ] **Step 1: 创建目录结构**

```bash
mkdir -p internal/agentcore/foundation/tool/schema
```

- [ ] **Step 2: 编写 doc.go**

```go
// Package tool 提供工具系统的核心抽象，包括 Tool 接口、ToolCard 配置卡片和
// LifecycleTool 生命周期包装器。
//
// Tool 是 Agent 调用外部能力的统一抽象。LLM 返回 ToolCall 后，
// Agent 通过 Tool 接口执行工具调用并拿回结果。
// Tool 接口只定义纯业务方法（Invoke/Stream/Card），
// LifecycleTool 包装器负责在调用前后触发 ToolCallEvents 生命周期回调，
// AbilityManager 注册时自动包装，对调用方透明。
//
// 工具类型体系：
//
//	Tool 接口 — 统一抽象（Invoke/Stream/Card）
//	  ├── LocalFunction   — 本地函数工具（后续 3.3 节）
//	  ├── MCPTool         — MCP 协议远程工具（后续 3.5 节）
//	  └── RestfulApi      — RESTful API 工具（后续 3.8 节）
//
// Card 配置体系：
//
//	BaseCard (common/schema) — 数字名片基类
//	  └── ToolCard — 工具配置卡片（InputParams + Properties）
//	        ├── McpToolCard — MCP 工具卡片（后续 3.5 节）
//	        └── RestfulApiCard — RESTful API 工具卡片（后续 3.8 节）
//
// 回调生命周期：
//
//	LifecycleTool 包装器在 Invoke/Stream 调用前后自动触发以下事件：
//	  TOOL_CALL_STARTED → TOOL_INVOKE_INPUT → [执行] → TOOL_INVOKE_OUTPUT → TOOL_CALL_FINISHED
//	  异常时触发 TOOL_CALL_ERROR
//	  Stream 模式额外触发 TOOL_RESULT_RECEIVED（逐 chunk）
//
// 文件目录：
//
//	tool/
//	├── doc.go                # 包文档
//	├── base.go               # Tool 接口 + ToolCard + ToolOption + ToolCallOptions + StreamChunk
//	├── tool_info.go          # ToolCard.ToolInfo() — Param→JSON Schema 转换
//	├── lifecycle_tool.go     # LifecycleTool 包装器（回调生命周期）
//	└── schema/
//	    └── tool_info.go      # ToolCallEventType + ToolCallEventData 定义
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/
package tool
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/...`
Expected: 编译通过（包为空但 doc.go 合法）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/doc.go
git commit -m "feat(tool): 添加 tool 包文档 doc.go"
```

---

### Task 2: 实现 Tool 接口、ToolCard、ToolOption、ToolCallOptions、StreamChunk

**Files:**
- Create: `internal/agentcore/foundation/tool/base.go`

- [ ] **Step 1: 编写 base.go**

```go
package tool

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCard 工具配置卡片，嵌入 BaseCard，增加输入参数定义和扩展属性。
//
// 对应 Python: openjiuwen/core/foundation/tool/base.py (ToolCard)
type ToolCard struct {
	schema.BaseCard
	// InputParams 输入参数定义，用于校验和生成 ToolInfo 传给 LLM
	InputParams []*schema.Param
	// Properties 扩展属性
	Properties map[string]any
}

// ToolCallOptions 工具调用的扩展选项。
//
// 对应 Python: Tool.invoke/Stream 中的 **kwargs 参数集合
type ToolCallOptions struct {
	// SkipNoneValue 是否跳过 None 值（LocalFunction 使用）
	SkipNoneValue bool
	// SkipInputsValidate 是否跳过输入校验（LocalFunction 使用）
	SkipInputsValidate bool
	// Timeout 超时时间，单位秒（RestfulApi 使用）
	Timeout float64
	// MaxResponseBytes 最大响应字节数（RestfulApi 使用）
	MaxResponseBytes int
	// RaiseForStatus HTTP 错误是否抛异常（RestfulApi 使用）
	RaiseForStatus bool
}

// StreamChunk 流式执行的返回块。
//
// 消费者通过读取 channel 中的 StreamChunk 获取流式数据：
//   - Data 非 nil 且 Done=false：正常数据块
//   - Done=true：流正常结束
//   - Error 非 nil：流出错
type StreamChunk struct {
	// Data 本块数据
	Data map[string]any
	// Error 非 nil 表示流结束且出错
	Error error
	// Done true 表示流正常结束（Data 为空）
	Done bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolOption 工具调用选项函数。
type ToolOption func(*ToolCallOptions)

// WithSkipNoneValue 设置是否跳过 None 值。
func WithSkipNoneValue(skip bool) ToolOption {
	return func(o *ToolCallOptions) { o.SkipNoneValue = skip }
}

// WithSkipInputsValidate 设置是否跳过输入校验。
func WithSkipInputsValidate(skip bool) ToolOption {
	return func(o *ToolCallOptions) { o.SkipInputsValidate = skip }
}

// WithTimeout 设置超时时间（秒）。
func WithTimeout(d float64) ToolOption {
	return func(o *ToolCallOptions) { o.Timeout = d }
}

// WithMaxResponseBytes 设置最大响应字节数。
func WithMaxResponseBytes(n int) ToolOption {
	return func(o *ToolCallOptions) { o.MaxResponseBytes = n }
}

// WithRaiseForStatus 设置 HTTP 错误是否抛异常。
func WithRaiseForStatus(raise bool) ToolOption {
	return func(o *ToolCallOptions) { o.RaiseForStatus = raise }
}

// NewToolCallOptions 从选项列表构造 ToolCallOptions。
func NewToolCallOptions(opts ...ToolOption) *ToolCallOptions {
	o := &ToolCallOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// NewToolCard 创建 ToolCard 实例，自动生成 BaseCard。
//
// 对应 Python: ToolCard(input_params=..., properties=...)
func NewToolCard(name, description string, inputParams []*schema.Param, properties map[string]any) *ToolCard {
	card := &ToolCard{
		BaseCard:    *schema.NewBaseCard(schema.WithName(name), schema.WithDescription(description)),
		InputParams: inputParams,
		Properties:  properties,
	}
	if card.Properties == nil {
		card.Properties = make(map[string]any)
	}
	return card
}

// Tool 工具接口，所有工具类型（LocalFunction/MCPTool/RestfulApi）的统一抽象。
//
// Tool 接口只定义纯业务方法，生命周期回调由 LifecycleTool 包装器处理。
//
// 对应 Python: openjiuwen/core/foundation/tool/base.py (Tool)
type Tool interface {
	// Card 返回工具的配置卡片
	Card() *ToolCard
	// Invoke 一次性执行工具，返回完整结果。
	// 不支持 Stream 的工具在 Stream 方法中返回 ErrStreamNotSupported。
	Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)
	// Stream 流式执行工具，逐步返回结果块。
	// 不支持 Stream 的工具返回 ErrStreamNotSupported 错误。
	Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
}

// ErrStreamNotSupported 工具不支持流式调用时返回的错误。
//
// 对应 Python: TOOL_STREAM_NOT_SUPPORTED (182010)
var ErrStreamNotSupported = exception.BuildError(
	exception.StatusToolStreamNotSupported,
	exception.WithParam("card", ""),
)

// NewErrStreamNotSupported 创建带 card 信息的 Stream 不支持错误。
func NewErrStreamNotSupported(card string) *exception.BaseError {
	return exception.BuildError(
		exception.StatusToolStreamNotSupported,
		exception.WithParam("card", card),
	)
}

// ValidateToolCard 校验 ToolCard 的合法性。
//
// 规则：
//   - card 不能为 nil
//   - card.ID 不能为空
//
// 对应 Python: Tool.__init__ 中的 card 校验
func ValidateToolCard(card *ToolCard) error {
	if card == nil {
		return exception.BuildError(
			exception.StatusToolCardInvalid,
			exception.WithParam("card", "nil"),
			exception.WithParam("reason", "card is None"),
		)
	}
	if card.ID == "" {
		return exception.BuildError(
			exception.StatusToolCardInvalid,
			exception.WithParam("card", card.String()),
			exception.WithParam("reason", "card id is empty"),
		)
	}
	return nil
}

// String 实现 fmt.Stringer 接口，返回 ToolCard 的简洁描述。
func (c *ToolCard) String() string {
	return fmt.Sprintf("id=%s,name=%s", c.ID, c.Name)
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/base.go
git commit -m "feat(tool): 实现 Tool 接口、ToolCard、ToolOption、StreamChunk"
```

---

### Task 3: 编写 base.go 的单元测试

**Files:**
- Create: `internal/agentcore/foundation/tool/base_test.go`

- [ ] **Step 1: 编写 base_test.go**

```go
package tool

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewToolCard(t *testing.T) {
	card := NewToolCard("weather", "查询天气", nil, nil)
	if card.Name != "weather" {
		t.Errorf("Name = %q, want %q", card.Name, "weather")
	}
	if card.Description != "查询天气" {
		t.Errorf("Description = %q, want %q", card.Description, "查询天气")
	}
	if card.ID == "" {
		t.Error("ID 不应为空")
	}
	if card.InputParams != nil {
		t.Errorf("InputParams = %v, want nil", card.InputParams)
	}
	if card.Properties == nil {
		t.Error("Properties 不应为 nil")
	}
	if len(card.Properties) != 0 {
		t.Errorf("Properties 应为空 map，实际有 %d 项", len(card.Properties))
	}
}

func TestNewToolCard_带参数(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("city", "城市名", true),
	}
	props := map[string]any{"source": "openweather"}
	card := NewToolCard("weather", "查询天气", params, props)
	if len(card.InputParams) != 1 {
		t.Errorf("InputParams 长度 = %d, want 1", len(card.InputParams))
	}
	if card.Properties["source"] != "openweather" {
		t.Errorf("Properties[source] = %v, want openweather", card.Properties["source"])
	}
}

func TestNewToolCard_ID自动生成(t *testing.T) {
	card1 := NewToolCard("a", "", nil, nil)
	card2 := NewToolCard("b", "", nil, nil)
	if card1.ID == card2.ID {
		t.Error("两个 ToolCard 的 ID 不应相同")
	}
}

func TestToolCard_String(t *testing.T) {
	card := NewToolCard("weather", "查询天气", nil, nil)
	s := card.String()
	if s != "id="+card.ID+",name=weather" {
		t.Errorf("String() = %q, want %q", s, "id="+card.ID+",name=weather")
	}
}

func TestNewToolCallOptions(t *testing.T) {
	opts := NewToolCallOptions(
		WithSkipNoneValue(true),
		WithTimeout(30.0),
		WithMaxResponseBytes(1024),
		WithRaiseForStatus(true),
	)
	if !opts.SkipNoneValue {
		t.Error("SkipNoneValue 应为 true")
	}
	if !opts.SkipInputsValidate {
		t.Error("SkipInputsValidate 默认应为 false")
	}
	if opts.Timeout != 30.0 {
		t.Errorf("Timeout = %f, want 30.0", opts.Timeout)
	}
	if opts.MaxResponseBytes != 1024 {
		t.Errorf("MaxResponseBytes = %d, want 1024", opts.MaxResponseBytes)
	}
	if !opts.RaiseForStatus {
		t.Error("RaiseForStatus 应为 true")
	}
}

func TestNewToolCallOptions_空选项(t *testing.T) {
	opts := NewToolCallOptions()
	if opts.SkipNoneValue {
		t.Error("SkipNoneValue 默认应为 false")
	}
	if opts.Timeout != 0 {
		t.Errorf("Timeout 默认应为 0, 实际 %f", opts.Timeout)
	}
}

func TestValidateToolCard_NilCard(t *testing.T) {
	err := ValidateToolCard(nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != 182000 {
		t.Errorf("Code = %d, want 182000", baseErr.Code())
	}
}

func TestValidateToolCard_空ID(t *testing.T) {
	card := &ToolCard{
		BaseCard: schema.BaseCard{ID: "", Name: "test"},
	}
	err := ValidateToolCard(card)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != 182000 {
		t.Errorf("Code = %d, want 182000", baseErr.Code())
	}
}

func TestValidateToolCard_合法(t *testing.T) {
	card := NewToolCard("test", "测试", nil, nil)
	err := ValidateToolCard(card)
	if err != nil {
		t.Errorf("合法 ToolCard 不应返回错误: %v", err)
	}
}

func TestNewErrStreamNotSupported(t *testing.T) {
	err := NewErrStreamNotSupported("weather_tool")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if err.Code() != 182010 {
		t.Errorf("Code = %d, want 182010", err.Code())
	}
}

func TestStreamChunk_数据块(t *testing.T) {
	chunk := StreamChunk{Data: map[string]any{"result": "ok"}}
	if chunk.Done {
		t.Error("Done 应为 false")
	}
	if chunk.Error != nil {
		t.Error("Error 应为 nil")
	}
}

func TestStreamChunk_结束标记(t *testing.T) {
	chunk := StreamChunk{Done: true}
	if !chunk.Done {
		t.Error("Done 应为 true")
	}
	if chunk.Data != nil {
		t.Error("Data 应为 nil")
	}
}

func TestStreamChunk_错误标记(t *testing.T) {
	chunk := StreamChunk{Error: fmt.Errorf("timeout")}
	if chunk.Error == nil {
		t.Error("Error 不应为 nil")
	}
}
```

注意：测试文件需要 `import "fmt"` 补充。

- [ ] **Step 2: 修正测试文件，补充 import**

在测试文件的 import 中添加 `"fmt"`。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/... -v`
Expected: 所有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/foundation/tool/base_test.go
git commit -m "test(tool): 添加 ToolCard/ToolOption/StreamChunk 单元测试"
```

---

### Task 4: 实现 Param→JSON Schema 转换和 ToolCard.ToolInfo()

**Files:**
- Create: `internal/agentcore/foundation/tool/tool_info.go`
- Modify: `internal/common/schema/param.go` — 添加 `ToJSONSchemaMap()` 方法

- [ ] **Step 1: 在 param.go 中添加 ToJSONSchemaMap 方法**

在 `internal/common/schema/param.go` 的导出函数区块末尾添加：

```go
// ToJSONSchemaMap 将 []*Param 列表转换为 OpenAI function calling 格式的 JSON Schema parameters。
//
// 生成格式：
//
//	{
//	  "type": "object",
//	  "properties": { <每个 Param 的 JSON Schema> },
//	  "required": [ <必填参数名列表> ]
//	}
//
// 对应 Python: ToolInfo.parameters 从 ToolCard.input_params 自动生成的逻辑
func ToJSONSchemaMap(params []*Param) map[string]any {
	if len(params) == 0 {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	properties := make(map[string]any, len(params))
	var required []string

	for _, p := range params {
		properties[p.Name] = paramToSchema(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}

	result := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

// paramToSchema 将单个 Param 转换为 JSON Schema 字典。
func paramToSchema(p *Param) map[string]any {
	schema := map[string]any{
		"type":        p.Type.String(),
		"description": p.Description,
	}
	if p.Default != nil {
		schema["default"] = p.Default
	}
	switch p.Type {
	case ParamTypeArray:
		if p.Items != nil {
			schema["items"] = paramToSchema(p.Items)
		}
	case ParamTypeObject:
		if len(p.Properties) > 0 {
			objProps := make(map[string]any, len(p.Properties))
			var objRequired []string
			for _, prop := range p.Properties {
				objProps[prop.Name] = paramToSchema(prop)
				if prop.Required {
					objRequired = append(objRequired, prop.Name)
				}
			}
			objSchema := map[string]any{
				"type":       "object",
				"properties": objProps,
			}
			if len(objRequired) > 0 {
				objSchema["required"] = objRequired
			}
			schema["properties"] = objProps
			if len(objRequired) > 0 {
				schema["required"] = objRequired
			}
		}
	}
	return schema
}
```

- [ ] **Step 2: 编写 tool_info.go**

```go
package tool

import (
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ToolInfo 从 ToolCard 生成工具描述信息，供 LLM function calling 消费。
//
// 将 InputParams ([]*Param) 转换为 JSON Schema map，构造 ToolInfo 返回。
//
// 对应 Python: ToolCard.tool_info() -> ToolInfo(name=..., description=..., parameters=...)
func (c *ToolCard) ToolInfo() *schema.ToolInfo {
	parameters := schema.ToJSONSchemaMap(c.InputParams)
	return schema.NewToolInfo(c.Name, c.Description, parameters)
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/common/schema/... ./internal/agentcore/foundation/tool/...`
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/common/schema/param.go internal/agentcore/foundation/tool/tool_info.go
git commit -m "feat(tool): 实现 Param→JSON Schema 转换和 ToolCard.ToolInfo()"
```

---

### Task 5: 编写 Param→JSON Schema 转换和 ToolCard.ToolInfo() 的单元测试

**Files:**
- Modify: `internal/common/schema/param_test.go` — 添加 ToJSONSchemaMap 测试
- Create: `internal/agentcore/foundation/tool/tool_info_test.go`

- [ ] **Step 1: 在 param_test.go 中添加 ToJSONSchemaMap 测试**

在 `internal/common/schema/param_test.go` 末尾添加以下测试函数：

```go
func TestToJSONSchemaMap_空参数列表(t *testing.T) {
	result := ToJSONSchemaMap(nil)
	if result["type"] != "object" {
		t.Errorf("type = %v, want object", result["type"])
	}
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties 类型不是 map[string]any")
	}
	if len(props) != 0 {
		t.Errorf("properties 应为空，实际有 %d 项", len(props))
	}
	if _, hasRequired := result["required"]; hasRequired {
		t.Error("空参数列表不应有 required 字段")
	}
}

func TestToJSONSchemaMap_简单参数(t *testing.T) {
	params := []*Param{
		NewStringParam("city", "城市名", true),
		NewIntegerParam("count", "数量", false),
	}
	result := ToJSONSchemaMap(params)

	if result["type"] != "object" {
		t.Errorf("type = %v, want object", result["type"])
	}

	props := result["properties"].(map[string]any)
	citySchema := props["city"].(map[string]any)
	if citySchema["type"] != "string" {
		t.Errorf("city type = %v, want string", citySchema["type"])
	}
	if citySchema["description"] != "城市名" {
		t.Errorf("city description = %v, want 城市名", citySchema["description"])
	}

	required := result["required"].([]string)
	if len(required) != 1 || required[0] != "city" {
		t.Errorf("required = %v, want [city]", required)
	}
}

func TestToJSONSchemaMap_嵌套对象参数(t *testing.T) {
	params := []*Param{
		NewObjectParam("config", "配置", true, []*Param{
			NewStringParam("host", "主机", true),
			NewIntegerParam("port", "端口", false, 8080),
		}),
	}
	result := ToJSONSchemaMap(params)
	props := result["properties"].(map[string]any)
	configSchema := props["config"].(map[string]any)
	if configSchema["type"] != "object" {
		t.Errorf("config type = %v, want object", configSchema["type"])
	}
	innerProps := configSchema["properties"].(map[string]any)
	hostSchema := innerProps["host"].(map[string]any)
	if hostSchema["type"] != "string" {
		t.Errorf("host type = %v, want string", hostSchema["type"])
	}
}

func TestToJSONSchemaMap_数组参数(t *testing.T) {
	params := []*Param{
		NewArrayParam("tags", "标签", false, NewStringParam("", "", false)),
	}
	result := ToJSONSchemaMap(params)
	props := result["properties"].(map[string]any)
	tagsSchema := props["tags"].(map[string]any)
	if tagsSchema["type"] != "array" {
		t.Errorf("tags type = %v, want array", tagsSchema["type"])
	}
	itemsSchema := tagsSchema["items"].(map[string]any)
	if itemsSchema["type"] != "string" {
		t.Errorf("items type = %v, want string", itemsSchema["type"])
	}
}
```

- [ ] **Step 2: 编写 tool_info_test.go**

```go
package tool

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

func TestToolCard_ToolInfo_无参数(t *testing.T) {
	card := NewToolCard("test_tool", "测试工具", nil, nil)
	info := card.ToolInfo()
	if info.Type != "function" {
		t.Errorf("Type = %q, want function", info.Type)
	}
	if info.Name != "test_tool" {
		t.Errorf("Name = %q, want test_tool", info.Name)
	}
	if info.Description != "测试工具" {
		t.Errorf("Description = %q, want 测试工具", info.Description)
	}
	params, ok := info.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("Parameters.properties 类型不正确")
	}
	if len(params) != 0 {
		t.Errorf("Parameters.properties 应为空，实际有 %d 项", len(params))
	}
}

func TestToolCard_ToolInfo_带参数(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("city", "城市名", true),
		schema.NewIntegerParam("days", "预报天数", false),
	}
	card := NewToolCard("weather", "查询天气", params, nil)
	info := card.ToolInfo()

	props := info.Parameters["properties"].(map[string]any)
	if len(props) != 2 {
		t.Errorf("properties 数量 = %d, want 2", len(props))
	}

	citySchema := props["city"].(map[string]any)
	if citySchema["type"] != "string" {
		t.Errorf("city type = %v, want string", citySchema["type"])
	}

	required := info.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "city" {
		t.Errorf("required = %v, want [city]", required)
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... ./internal/agentcore/foundation/tool/... -v`
Expected: 所有测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/common/schema/param_test.go internal/agentcore/foundation/tool/tool_info_test.go
git commit -m "test(tool): 添加 Param→JSON Schema 和 ToolCard.ToolInfo() 单元测试"
```

---

### Task 6: 实现 ToolCallEventType 和 ToolCallEventData

**Files:**
- Create: `internal/agentcore/foundation/tool/schema/tool_info.go`

- [ ] **Step 1: 编写 schema/tool_info.go**

```go
// Package schema 定义工具系统的事件类型和事件数据。
//
// 本包独立于 tool 主包，避免 LifecycleTool 与 Tool 之间的循环依赖。
//
// 文件目录：
//
//	schema/
//	└── tool_info.go   # ToolCallEventType + ToolCallEventData
//
// 对应 Python 代码：openjiuwen/core/runner/callback/events.py (ToolCallEvents)
package schema

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 枚举 ────────────────────────────

// ToolCallEventType 工具调用事件类型。
//
// 事件名格式 "_framework:{event_name}"，与 Python EventBase.get_event() 构建规则一致。
// 与 LLMCallEventType 并列，使用相同的 scope "_framework"。
//
// 对应 Python: openjiuwen/core/runner/callback/events.py (ToolCallEvents)
type ToolCallEventType string

const (
	// ToolCallStarted 工具调用启动
	ToolCallStarted ToolCallEventType = "_framework:tool_call_started"
	// ToolCallFinished 工具调用完成
	ToolCallFinished ToolCallEventType = "_framework:tool_call_finished"
	// ToolCallError 工具调用出错
	ToolCallError ToolCallEventType = "_framework:tool_call_error"
	// ToolResultReceived 工具结果接收（流式逐 chunk）
	ToolResultReceived ToolCallEventType = "_framework:tool_result_received"
	// ToolParseStarted 工具参数解析开始
	ToolParseStarted ToolCallEventType = "_framework:tool_parse_started"
	// ToolParseFinished 工具参数解析完成
	ToolParseFinished ToolCallEventType = "_framework:tool_parse_finished"
	// ToolInvokeInput invoke 调用前触发
	ToolInvokeInput ToolCallEventType = "_framework:tool_invoke_input"
	// ToolInvokeOutput invoke 调用后触发
	ToolInvokeOutput ToolCallEventType = "_framework:tool_invoke_output"
	// ToolStreamInput stream 调用前触发
	ToolStreamInput ToolCallEventType = "_framework:tool_stream_input"
	// ToolStreamOutput stream 每项触发
	ToolStreamOutput ToolCallEventType = "_framework:tool_stream_output"
	// ToolAuth 工具认证事件
	ToolAuth ToolCallEventType = "_framework:tool_auth"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCallEventData 工具调用事件数据，回调函数接收此结构获取上下文信息。
//
// 对应 Python: _ToolMeta.__call__ 中 trigger 调用时的 kwargs 参数集合
type ToolCallEventData struct {
	// Event 事件类型
	Event ToolCallEventType
	// ToolName 工具名称
	ToolName string
	// ToolID 工具 ID
	ToolID string
	// Inputs 调用输入参数
	Inputs map[string]any
	// Result 调用结果（Finished/InvokeOutput/StreamOutput 时有值）
	Result map[string]any
	// Error 错误信息（Error 事件时有值）
	Error error
	// Extra 额外数据
	Extra map[string]any
}

// ToolCallbackFunc 工具回调函数类型。
type ToolCallbackFunc func(ctx context.Context, data *ToolCallEventData)

// ToolCallbackFramework 工具调用回调框架，独立于 LLM CallbackFramework。
//
// 与 llm/callback.CallbackFramework 设计一致但事件类型不同。
// 后续 6.24 节统一回调框架时合并。
//
// 对应 Python: AsyncCallbackFramework（Tool 事件部分）
type ToolCallbackFramework struct {
	mu        sync.RWMutex
	callbacks map[ToolCallEventType][]ToolCallbackFunc
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolCallbackFramework 创建工具回调框架实例。
func NewToolCallbackFramework() *ToolCallbackFramework {
	return &ToolCallbackFramework{
		callbacks: make(map[ToolCallEventType][]ToolCallbackFunc),
	}
}

// On 注册回调函数。
func (fw *ToolCallbackFramework) On(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	fw.callbacks[event] = append(fw.callbacks[event], fn)
}

// Off 注销回调函数。
func (fw *ToolCallbackFramework) Off(event ToolCallEventType, fn ToolCallbackFunc) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	callbacks, ok := fw.callbacks[event]
	if !ok {
		return
	}

	for i, cb := range callbacks {
		if fmt.Sprintf("%p", cb) == fmt.Sprintf("%p", fn) {
			fw.callbacks[event] = append(callbacks[:i], callbacks[i+1:]...)
			return
		}
	}
}

// Trigger 触发事件。
func (fw *ToolCallbackFramework) Trigger(ctx context.Context, data *ToolCallEventData) {
	if ctx == nil || data == nil {
		return
	}

	fw.mu.RLock()
	callbacks := fw.callbacks[data.Event]
	fw.mu.RUnlock()

	for _, fn := range callbacks {
		fn(ctx, data)
	}
}

// NewToolCallEventData 创建工具调用事件数据。
func NewToolCallEventData(event ToolCallEventType, card *schema.ToolCard) *ToolCallEventData {
	if card == nil {
		return &ToolCallEventData{Event: event}
	}
	return &ToolCallEventData{
		Event:    event,
		ToolName: card.Name,
		ToolID:   card.ID,
	}
}
```

注意：需要 `import "fmt"` 补充。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/schema/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/schema/tool_info.go
git commit -m "feat(tool): 实现 ToolCallEventType、ToolCallEventData 和 ToolCallbackFramework"
```

---

### Task 7: 编写 schema/tool_info.go 的单元测试

**Files:**
- Create: `internal/agentcore/foundation/tool/schema/tool_info_test.go`

- [ ] **Step 1: 编写 tool_info_test.go**

```go
package schema

import (
	"context"
	"sync/atomic"
	"testing"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

func TestToolCallEventType_值(t *testing.T) {
	tests := []struct {
		event ToolCallEventType
		want  string
	}{
		{ToolCallStarted, "_framework:tool_call_started"},
		{ToolCallFinished, "_framework:tool_call_finished"},
		{ToolCallError, "_framework:tool_call_error"},
		{ToolResultReceived, "_framework:tool_result_received"},
		{ToolParseStarted, "_framework:tool_parse_started"},
		{ToolParseFinished, "_framework:tool_parse_finished"},
		{ToolInvokeInput, "_framework:tool_invoke_input"},
		{ToolInvokeOutput, "_framework:tool_invoke_output"},
		{ToolStreamInput, "_framework:tool_stream_input"},
		{ToolStreamOutput, "_framework:tool_stream_output"},
		{ToolAuth, "_framework:tool_auth"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.want {
			t.Errorf("ToolCallEventType = %q, want %q", tt.event, tt.want)
		}
	}
}

func TestNewToolCallbackFramework(t *testing.T) {
	fw := NewToolCallbackFramework()
	if fw == nil {
		t.Fatal("NewToolCallbackFramework 返回 nil")
	}
}

func TestToolCallbackFramework_On和Trigger(t *testing.T) {
	fw := NewToolCallbackFramework()
	var called int32

	fw.On(ToolCallStarted, func(_ context.Context, data *ToolCallEventData) {
		if data.ToolName != "weather" {
			t.Errorf("ToolName = %q, want weather", data.ToolName)
		}
		atomic.AddInt32(&called, 1)
	})

	card := &commonschema.ToolCard{Name: "weather"}
	data := NewToolCallEventData(ToolCallStarted, card)
	fw.Trigger(context.Background(), data)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

func TestToolCallbackFramework_Off(t *testing.T) {
	fw := NewToolCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *ToolCallEventData) {
		atomic.AddInt32(&called, 1)
	}

	fw.On(ToolCallStarted, fn)
	fw.Off(ToolCallStarted, fn)

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.Trigger(context.Background(), data)

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("Off 后不应触发，called = %d", called)
	}
}

func TestToolCallbackFramework_多回调按序执行(t *testing.T) {
	fw := NewToolCallbackFramework()
	var order []int

	fw.On(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) {
		order = append(order, 1)
	})
	fw.On(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) {
		order = append(order, 2)
	})

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.Trigger(context.Background(), data)

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("执行顺序 = %v, want [1 2]", order)
	}
}

func TestToolCallbackFramework_Trigger_NilContext(t *testing.T) {
	fw := NewToolCallbackFramework()
	var called int32
	fw.On(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) {
		atomic.AddInt32(&called, 1)
	})
	fw.Trigger(nil, NewToolCallEventData(ToolCallStarted, nil))
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil context 不应触发回调")
	}
}

func TestNewToolCallEventData(t *testing.T) {
	card := &commonschema.ToolCard{Name: "test", ID: "abc123"}
	data := NewToolCallEventData(ToolCallStarted, card)
	if data.Event != ToolCallStarted {
		t.Errorf("Event = %v, want ToolCallStarted", data.Event)
	}
	if data.ToolName != "test" {
		t.Errorf("ToolName = %q, want test", data.ToolName)
	}
	if data.ToolID != "abc123" {
		t.Errorf("ToolID = %q, want abc123", data.ToolID)
	}
}

func TestNewToolCallEventData_NilCard(t *testing.T) {
	data := NewToolCallEventData(ToolCallError, nil)
	if data.Event != ToolCallError {
		t.Errorf("Event = %v, want ToolCallError", data.Event)
	}
	if data.ToolName != "" {
		t.Errorf("ToolName 应为空，实际 %q", data.ToolName)
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/schema/... -v`
Expected: 所有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/schema/tool_info_test.go
git commit -m "test(tool): 添加 ToolCallEventType 和 ToolCallbackFramework 单元测试"
```

---

### Task 8: 实现 LifecycleTool 包装器

**Files:**
- Create: `internal/agentcore/foundation/tool/lifecycle_tool.go`

- [ ] **Step 1: 编写 lifecycle_tool.go**

```go
package tool

import (
	"context"

	toolschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LifecycleTool 包装 Tool，在 Invoke/Stream 调用前后自动触发回调事件。
//
// LifecycleTool 实现了 Tool 接口，可以像普通 Tool 一样使用。
// 注册到 AbilityManager 时自动包装，对调用方透明。
//
// 对应 Python: _ToolMeta.__call__ 中的生命周期注入逻辑
type LifecycleTool struct {
	inner Tool
	fw    *toolschema.ToolCallbackFramework
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLifecycleTool 创建带生命周期回调的工具包装器。
func NewLifecycleTool(inner Tool, fw *toolschema.ToolCallbackFramework) *LifecycleTool {
	return &LifecycleTool{
		inner: inner,
		fw:    fw,
	}
}

// Card 委托给内部 Tool。
func (t *LifecycleTool) Card() *ToolCard {
	return t.inner.Card()
}

// Invoke 包装生命周期：STARTED → INVOKE_INPUT → [执行] → INVOKE_OUTPUT → FINISHED / ERROR
//
// 对应 Python: _ToolMeta 中对 invoke 的 _lifecycle_invoke 包装 + IO 转换钩子
func (t *LifecycleTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	card := t.inner.Card()

	// 1. 触发 TOOL_CALL_STARTED
	t.fw.Trigger(ctx, newStartedData(card, inputs))

	// 2. 触发 TOOL_INVOKE_INPUT（emit_before）
	t.fw.Trigger(ctx, newInvokeInputData(card, inputs))

	// 3. 执行内部 Tool
	result, err := t.inner.Invoke(ctx, inputs, opts...)

	if err != nil {
		// 4. 触发 TOOL_CALL_ERROR
		t.fw.Trigger(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 5. 触发 TOOL_INVOKE_OUTPUT（emit_after）
	t.fw.Trigger(ctx, newInvokeOutputData(card, result))

	// 6. 触发 TOOL_CALL_FINISHED
	t.fw.Trigger(ctx, newFinishedData(card, inputs, result))

	return result, nil
}

// Stream 包装生命周期：STARTED → STREAM_INPUT → [执行] → 逐 chunk RESULT_RECEIVED → FINISHED / ERROR
//
// 对应 Python: _ToolMeta 中对 stream 的 _lifecycle_stream 包装
func (t *LifecycleTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	card := t.inner.Card()

	// 1. 触发 TOOL_CALL_STARTED
	t.fw.Trigger(ctx, newStartedData(card, inputs))

	// 2. 触发 TOOL_STREAM_INPUT（emit_before）
	t.fw.Trigger(ctx, newStreamInputData(card, inputs))

	// 3. 执行内部 Tool
	innerCh, err := t.inner.Stream(ctx, inputs, opts...)
	if err != nil {
		// 出错时触发 TOOL_CALL_ERROR
		t.fw.Trigger(ctx, newErrorData(card, inputs, err))
		return nil, err
	}

	// 4. 包装输出 channel，逐 chunk 触发 RESULT_RECEIVED
	outCh := make(chan StreamChunk, 1)
	go func() {
		defer close(outCh)
		for chunk := range innerCh {
			if chunk.Error != nil {
				// 流出错
				t.fw.Trigger(ctx, newErrorData(card, inputs, chunk.Error))
				outCh <- chunk
				return
			}
			if chunk.Done {
				// 流正常结束
				t.fw.Trigger(ctx, newStreamOutputData(card, nil))
				t.fw.Trigger(ctx, newFinishedData(card, inputs, nil))
				outCh <- chunk
				return
			}
			// 正常数据块：触发 TOOL_RESULT_RECEIVED
			t.fw.Trigger(ctx, newResultReceivedData(card, chunk.Data))
			// 触发 TOOL_STREAM_OUTPUT
			t.fw.Trigger(ctx, newStreamOutputData(card, chunk.Data))
			outCh <- chunk
		}
	}()

	return outCh, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newStartedData 创建 TOOL_CALL_STARTED 事件数据
func newStartedData(card *ToolCard, inputs map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolCallStarted, &card.BaseCard)
	data.Inputs = inputs
	return data
}

// newFinishedData 创建 TOOL_CALL_FINISHED 事件数据
func newFinishedData(card *ToolCard, inputs map[string]any, result map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolCallFinished, &card.BaseCard)
	data.Inputs = inputs
	data.Result = result
	return data
}

// newErrorData 创建 TOOL_CALL_ERROR 事件数据
func newErrorData(card *ToolCard, inputs map[string]any, err error) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolCallError, &card.BaseCard)
	data.Inputs = inputs
	data.Error = err
	return data
}

// newResultReceivedData 创建 TOOL_RESULT_RECEIVED 事件数据
func newResultReceivedData(card *ToolCard, result map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolResultReceived, &card.BaseCard)
	data.Result = result
	return data
}

// newInvokeInputData 创建 TOOL_INVOKE_INPUT 事件数据
func newInvokeInputData(card *ToolCard, inputs map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolInvokeInput, &card.BaseCard)
	data.Inputs = inputs
	return data
}

// newInvokeOutputData 创建 TOOL_INVOKE_OUTPUT 事件数据
func newInvokeOutputData(card *ToolCard, result map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolInvokeOutput, &card.BaseCard)
	data.Result = result
	return data
}

// newStreamInputData 创建 TOOL_STREAM_INPUT 事件数据
func newStreamInputData(card *ToolCard, inputs map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolStreamInput, &card.BaseCard)
	data.Inputs = inputs
	return data
}

// newStreamOutputData 创建 TOOL_STREAM_OUTPUT 事件数据
func newStreamOutputData(card *ToolCard, result map[string]any) *toolschema.ToolCallEventData {
	data := toolschema.NewToolCallEventData(toolschema.ToolStreamOutput, &card.BaseCard)
	data.Result = result
	return data
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/foundation/tool/...`
Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/lifecycle_tool.go
git commit -m "feat(tool): 实现 LifecycleTool 包装器（回调生命周期）"
```

---

### Task 9: 编写 LifecycleTool 的单元测试

**Files:**
- Create: `internal/agentcore/foundation/tool/lifecycle_tool_test.go`

- [ ] **Step 1: 编写 lifecycle_tool_test.go**

需要一个 mock Tool 实现来测试 LifecycleTool 的包装行为。

```go
package tool

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	toolschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/schema"
)

// mockTool 用于测试的模拟 Tool
type mockTool struct {
	card      *ToolCard
	invokeFn  func(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)
	streamFn  func(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
}

func (m *mockTool) Card() *ToolCard { return m.card }
func (m *mockTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	return m.invokeFn(ctx, inputs, opts...)
}
func (m *mockTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	return m.streamFn(ctx, inputs, opts...)
}

func TestLifecycleTool_Card(t *testing.T) {
	card := NewToolCard("test", "测试", nil, nil)
	inner := &mockTool{card: card}
	fw := toolschema.NewToolCallbackFramework()
	lt := NewLifecycleTool(inner, fw)

	if lt.Card().Name != "test" {
		t.Errorf("Card().Name = %q, want test", lt.Card().Name)
	}
}

func TestLifecycleTool_Invoke_成功(t *testing.T) {
	card := NewToolCard("weather", "查询天气", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, inputs map[string]any, _ ...ToolOption) (map[string]any, error) {
			return map[string]any{"result": "晴"}, nil
		},
	}

	fw := toolschema.NewToolCallbackFramework()
	var started, invokeInput, invokeOutput, finished int32

	fw.On(toolschema.ToolCallStarted, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&started, 1)
	})
	fw.On(toolschema.ToolInvokeInput, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&invokeInput, 1)
	})
	fw.On(toolschema.ToolInvokeOutput, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&invokeOutput, 1)
	})
	fw.On(toolschema.ToolCallFinished, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&finished, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	result, err := lt.Invoke(context.Background(), map[string]any{"city": "北京"})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["result"] != "晴" {
		t.Errorf("result = %v, want 晴", result["result"])
	}

	if atomic.LoadInt32(&started) != 1 {
		t.Errorf("ToolCallStarted 触发 %d 次, want 1", started)
	}
	if atomic.LoadInt32(&invokeInput) != 1 {
		t.Errorf("ToolInvokeInput 触发 %d 次, want 1", invokeInput)
	}
	if atomic.LoadInt32(&invokeOutput) != 1 {
		t.Errorf("ToolInvokeOutput 触发 %d 次, want 1", invokeOutput)
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Errorf("ToolCallFinished 触发 %d 次, want 1", finished)
	}
}

func TestLifecycleTool_Invoke_错误(t *testing.T) {
	card := NewToolCard("bad_tool", "坏工具", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (map[string]any, error) {
			return nil, errors.New("执行失败")
		},
	}

	fw := toolschema.NewToolCallbackFramework()
	var errEvent int32
	fw.On(toolschema.ToolCallError, func(_ context.Context, data *toolschema.ToolCallEventData) {
		if data.Error == nil {
			t.Error("ToolCallError 事件缺少 Error")
		}
		atomic.AddInt32(&errEvent, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	_, err := lt.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}

	if atomic.LoadInt32(&errEvent) != 1 {
		t.Errorf("ToolCallError 触发 %d 次, want 1", errEvent)
	}
}

func TestLifecycleTool_Stream_成功(t *testing.T) {
	card := NewToolCard("stream_tool", "流式工具", nil, nil)
	inner := &mockTool{
		card: card,
		streamFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 3)
			ch <- StreamChunk{Data: map[string]any{"chunk": 1}}
			ch <- StreamChunk{Data: map[string]any{"chunk": 2}}
			ch <- StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	fw := toolschema.NewToolCallbackFramework()
	var resultReceived, streamOutput, finished int32
	fw.On(toolschema.ToolResultReceived, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&resultReceived, 1)
	})
	fw.On(toolschema.ToolStreamOutput, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&streamOutput, 1)
	})
	fw.On(toolschema.ToolCallFinished, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&finished, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	ch, err := lt.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk 数量 = %d, want 3", len(chunks))
	}
	if atomic.LoadInt32(&resultReceived) != 2 {
		t.Errorf("ToolResultReceived 触发 %d 次, want 2（仅数据块）", resultReceived)
	}
	if atomic.LoadInt32(&streamOutput) != 2 {
		t.Errorf("ToolStreamOutput 触发 %d 次, want 2（仅数据块）", streamOutput)
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Errorf("ToolCallFinished 触发 %d 次, want 1", finished)
	}
}

func TestLifecycleTool_Stream_不支持(t *testing.T) {
	card := NewToolCard("no_stream", "不支持流式", nil, nil)
	inner := &mockTool{
		card: card,
		streamFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (<-chan StreamChunk, error) {
			return nil, NewErrStreamNotSupported(card.String())
		},
	}

	fw := toolschema.NewToolCallbackFramework()
	lt := NewLifecycleTool(inner, fw)
	_, err := lt.Stream(context.Background(), nil)
	if err == nil {
		t.Fatal("期望返回 ErrStreamNotSupported")
	}
}

func TestLifecycleTool_Stream_中途出错(t *testing.T) {
	card := NewToolCard("err_stream", "出错流式", nil, nil)
	inner := &mockTool{
		card: card,
		streamFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Data: map[string]any{"chunk": 1}}
			ch <- StreamChunk{Error: errors.New("流中断")}
			close(ch)
			return ch, nil
		},
	}

	fw := toolschema.NewToolCallbackFramework()
	var errEvent int32
	fw.On(toolschema.ToolCallError, func(_ context.Context, _ *toolschema.ToolCallEventData) {
		atomic.AddInt32(&errEvent, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	ch, _ := lt.Stream(context.Background(), nil)

	for range ch {
		// 消费所有 chunk
	}

	if atomic.LoadInt32(&errEvent) != 1 {
		t.Errorf("ToolCallError 触发 %d 次, want 1", errEvent)
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/... -v`
Expected: 所有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/foundation/tool/lifecycle_tool_test.go
git commit -m "test(tool): 添加 LifecycleTool 包装器单元测试"
```

---

### Task 10: 运行全部测试并检查覆盖率

**Files:** 无新文件

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/foundation/tool/... -v`
Expected: 所有测试通过

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/foundation/tool/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行 common/schema 测试确保没有破坏**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/common/schema/... -v`
Expected: 所有测试通过

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md 中 3.1 和 3.2 的状态**

将 3.1（Tool 接口与 ToolCard）和 3.2（ToolInfo / McpToolInfo 模型）的状态从 `☐` 更新为 `✅`。

---

## 自检清单

| 检查项 | 结果 |
|--------|------|
| Spec 覆盖：Tool 接口 | ✅ Task 2 |
| Spec 覆盖：ToolCard | ✅ Task 2 |
| Spec 覆盖：ToolOption / ToolCallOptions | ✅ Task 2 |
| Spec 覆盖：StreamChunk | ✅ Task 2 |
| Spec 覆盖：ToolCard.ToolInfo() (Param→JSON Schema) | ✅ Task 4 |
| Spec 覆盖：ToolCallEventType (11种事件) | ✅ Task 6 |
| Spec 覆盖：ToolCallEventData | ✅ Task 6 |
| Spec 覆盖：ToolCallbackFramework | ✅ Task 6 |
| Spec 覆盖：LifecycleTool 包装器 (Invoke) | ✅ Task 8 |
| Spec 覆盖：LifecycleTool 包装器 (Stream) | ✅ Task 8 |
| Spec 覆盖：ValidateToolCard | ✅ Task 2 |
| Spec 覆盖：ErrStreamNotSupported | ✅ Task 2 |
| Placeholder 扫描 | ✅ 无 TBD/TODO |
| 类型一致性 | ✅ ToolCard/ToolOption/StreamChunk/ToolCallEventData 各 Task 引用一致 |
