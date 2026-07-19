# 9.72b ToolOptimizer 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 全量移植 Python tool_call/ 下 13 个文件为 Go 等价实现，实现 ToolOptimizer 工具描述优化器。

**Architecture:** 复用已有 llm_resilience + 薄包装层替代 Python rits.py；SimpleEval 直接持有 *llm.Model 对接 Function Calling；BeamSearch 用 goroutine 并行；单包扁平放在 optimizer/tool_call/。

**Tech Stack:** Go 1.x, llm.Model, llm_resilience, mcp client, encoding/json, math, sync

**Design Spec:** `docs/superpowers/specs/2026-12-30-tool-optimizer-9.72b-design.md`

---

## File Structure

```
internal/evolving/optimizer/tool_call/
├── doc.go                          # 包文档
├── format.go                       # ParseJSON / FormatPromptLlama（Python format.py + base_method.py 中重复定义）
├── format_test.go                  # format 测试
├── schema_extractor.go             # ExtractSchema（Python schema_extractor.py）
├── schema_extractor_test.go        # schema_extractor 测试
├── default_configs.go              # DefaultConfigEg / DefaultConfigDesc（Python default_configs.py）
├── default_configs_test.go         # 默认配置测试
├── rits.go                         # InvokeWithVerify 薄包装（复用 llm_resilience）
├── rits_test.go                    # rits 测试
├── beam_search.go                  # BeamSearch + TreeNode（Python beam_search.py）
├── beam_search_test.go             # beam_search 测试
├── api_wrapper.go                  # SimpleAPIWrapper / SimpleAPIWrapperFromCallable / MakeSyncMCPCaller
├── api_wrapper_test.go             # api_wrapper 测试
├── base_method.go                  # BaseMethod + ProduceAnswerFromAPICall（Python base_method.py）
├── base_method_test.go             # base_method 测试
├── eval.go                         # SimpleEval（Python customized_eval.py）
├── eval_test.go                    # eval 测试
├── example_method.go               # APICallToExampleMethod（Python toolcall_example_method.py）
├── example_method_test.go          # example_method 测试
├── description_method.go           # ToolDescriptionMethod（Python description_example_method.py）
├── description_method_test.go      # description_method 测试
├── reviewer.go                     # ToolDescriptionReviewer（Python customized_reviewer.py）
├── reviewer_test.go                # reviewer 测试
├── pipeline.go                     # CustomizedPipeline（Python customized_pipline.py）
├── pipeline_test.go                # pipeline 测试
├── base.go                         # ToolOptimizerBase（Python base.py）
└── base_test.go                    # ToolOptimizerBase 测试
```

Also modify:
- `internal/evolving/optimizer/doc.go` — 添加 tool_call/ 子目录条目
- `IMPLEMENTATION_PLAN.md` — 更新 9.72b 状态

---

### Task 1: 创建包目录 + doc.go + format.go + format_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/doc.go`
- Create: `internal/evolving/optimizer/tool_call/format.go`
- Create: `internal/evolving/optimizer/tool_call/format_test.go`

Python 对标：`format.py` + `base_method.py` 中的 `parse_json` 和 `format_prompt_llama`（两个文件中重复定义了相同函数，Go 只需一份）。

- [ ] **Step 1: 创建目录和 doc.go**

```go
// Package tool_call 提供工具描述优化器，通过两阶段 Beam Search
// 迭代优化工具的自然语言描述，提升 LLM function calling 准确率。
//
// 两阶段流程：
//   1. Example Stage（APICallToExampleMethod）：生成 API 调用示例，形成正负例集
//   2. Description Stage（ToolDescriptionMethod）：基于正负例批判并增强描述
//
// 最终通过 ToolDescriptionReviewer（clean → cross_check → translate）三步后处理，
// 输出结构化的高质量工具描述。
//
// 文件目录：
//
//	tool_call/
//	├── doc.go                # 包文档
//	├── base.go               # ToolOptimizerBase 核心
//	├── format.go             # ParseJSON / FormatPromptLlama
//	├── schema_extractor.go   # ExtractSchema
//	├── default_configs.go    # DefaultConfigEg / DefaultConfigDesc
//	├── rits.go               # InvokeWithVerify 薄包装
//	├── beam_search.go        # BeamSearch + TreeNode
//	├── api_wrapper.go        # SimpleAPIWrapper / SimpleAPIWrapperFromCallable / MakeSyncMCPCaller
//	├── base_method.go        # BaseMethod + ProduceAnswerFromAPICall
//	├── eval.go               # SimpleEval
//	├── example_method.go     # APICallToExampleMethod
//	├── description_method.go # ToolDescriptionMethod
//	├── reviewer.go           # ToolDescriptionReviewer
//	└── pipeline.go           # CustomizedPipeline
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/tool_call/
package tool_call
```

- [ ] **Step 2: 创建 format.go**

```go
package tool_call

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseJSON 从 LLM 输出中提取 JSON。
// 支持带 header 查找和兜底 eval 解析。
//
// 对齐 Python: parse_json(output, header=None)
func ParseJSON(output string, header ...string) map[string]any {
	jsonIdx := -1
	if len(header) > 0 && header[0] != "" {
		// 对齐 Python: json_idx = output.find(f'{{"{header}":')
		jsonIdx = strings.Index(output, `{"`+header[0]+`":`)
		if jsonIdx == -1 {
			// 对齐 Python: json_idx = output.find(f'{{\n"{header}":')
			jsonIdx = strings.Index(output, "{\n\""+header[0]+"\":")
		}
	}
	if jsonIdx == -1 {
		// 对齐 Python: json_idx = output.find('{\n')
		jsonIdx = strings.Index(output, "{\n")
	}
	if jsonIdx == -1 {
		jsonIdx = strings.Index(output, "{")
	}

	jsonEndIdx := strings.LastIndex(output, "}")
	if jsonEndIdx != -1 {
		jsonEndIdx++
	}

	if jsonIdx == -1 || jsonEndIdx == -1 || jsonIdx >= jsonEndIdx {
		return map[string]any{}
	}

	extracted := strings.TrimSpace(output[jsonIdx:jsonEndIdx])

	var result map[string]any
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		// 对齐 Python: ast.literal_eval(output) — 尝试简单修复
		// 常见问题：单引号 → 双引号
		fixed := strings.ReplaceAll(extracted, "'", `"`)
		if jsonErr := json.Unmarshal([]byte(fixed), &result); jsonErr != nil {
			return map[string]any{}
		}
	}
	return result
}

// FormatPromptLlama 格式化 Llama 风格提示词。
// 当前实现为直接拼接 system + user prompt（对齐 Python）。
//
// 对齐 Python: format_prompt_llama(system_prompt, user_prompt)
func FormatPromptLlama(systemPrompt, userPrompt string) string {
	return systemPrompt + userPrompt
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 创建 format_test.go**

```go
package tool_call

import "testing"

// TestParseJSON_无Header 测试不带 header 的 JSON 解析
func TestParseJSON_无Header(t *testing.T) {
	output := `Some text before {"key": "value", "num": 42} some text after`
	result := ParseJSON(output)
	if result["key"] != "value" {
		t.Errorf("key = %v, want value", result["key"])
	}
	if num, ok := result["num"].(float64); !ok || num != 42 {
		t.Errorf("num = %v, want 42", result["num"])
	}
}

// TestParseJSON_带Header 测试带 header 的 JSON 解析
func TestParseJSON_带Header(t *testing.T) {
	output := `blah blah {"description": {"name": "test"}} more blah`
	result := ParseJSON(output, "description")
	inner, ok := result["description"].(map[string]any)
	if !ok {
		t.Fatalf("description type = %T, want map[string]any", result["description"])
	}
	if inner["name"] != "test" {
		t.Errorf("name = %v, want test", inner["name"])
	}
}

// TestParseJSON_无JSON 测试无 JSON 内容
func TestParseJSON_无JSON(t *testing.T) {
	output := "no json here"
	result := ParseJSON(output)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// TestParseJSON_单引号修复 测试单引号 JSON 修复
func TestParseJSON_单引号修复(t *testing.T) {
	output := `{'key': 'value'}`
	result := ParseJSON(output)
	if result["key"] != "value" {
		t.Errorf("key = %v, want value", result["key"])
	}
}

// TestFormatPromptLlama 测试 Llama 提示词格式化
func TestFormatPromptLlama(t *testing.T) {
	result := FormatPromptLlama("sys", "user")
	if result != "sysuser" {
		t.Errorf("got %q, want %q", result, "sysuser")
	}
}

// TestFormatPromptLlama_空SystemPrompt 测试空系统提示词
func TestFormatPromptLlama_空SystemPrompt(t *testing.T) {
	result := FormatPromptLlama("", "user prompt")
	if result != "user prompt" {
		t.Errorf("got %q, want %q", result, "user prompt")
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestParseJSON|TestFormatPromptLlama"`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/tool_call/
git commit -m "feat(evolving): add tool_call package with format utilities (9.72b)"
```

---

### Task 2: schema_extractor.go + schema_extractor_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/schema_extractor.go`
- Create: `internal/evolving/optimizer/tool_call/schema_extractor_test.go`

Python 对标：`schema_extractor.py`

- [ ] **Step 1: 创建 schema_extractor.go**

```go
package tool_call

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractSchema 从 JSON Schema 字典提取结构骨架，去除类型信息。
// 递归处理嵌套字典，保留列表原样，将原始值替换为空字符串。
//
// 对齐 Python: extract_schema(schema_dict)
func ExtractSchema(schemaDict map[string]any) map[string]any {
	result := make(map[string]any, len(schemaDict))
	for key, value := range schemaDict {
		switch v := value.(type) {
		case map[string]any:
			// 对齐 Python: result[key] = extract_schema(value)
			result[key] = ExtractSchema(v)
		case []any:
			// 对齐 Python: result[key] = value（保留列表原样，如 required 数组）
			result[key] = v
		default:
			// 对齐 Python: result[key] = ""（原始值替换为空字符串）
			result[key] = ""
		}
	}
	return result
}

// ExtractSchemaFromJSON 从 JSON 字符串提取结构骨架。
// 如果输入不是 dict，尝试 json.Unmarshal。
//
// 对齐 Python: extract_schema(schema_dict) 中 isinstance(schema_dict, dict) 分支
func ExtractSchemaFromJSON(jsonStr string) map[string]any {
	var schemaDict map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &schemaDict); err != nil {
		return map[string]any{}
	}
	return ExtractSchema(schemaDict)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 schema_extractor_test.go**

```go
package tool_call

import "testing"

// TestExtractSchema_嵌套字典 测试递归处理嵌套字典
func TestExtractSchema_嵌套字典(t *testing.T) {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "名称",
			},
		},
		"required": []any{"name"},
	}
	result := ExtractSchema(input)
	// 顶层 type 应变为空字符串
	if result["type"] != "" {
		t.Errorf("type = %v, want empty string", result["type"])
	}
	// required 列表应保留
	if req, ok := result["required"].([]any); !ok || len(req) != 1 {
		t.Errorf("required = %v, want [name]", result["required"])
	}
	// properties 应递归处理
	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T", result["properties"])
	}
	nameProp, ok := props["name"].(map[string]any)
	if !ok {
		t.Fatalf("name type = %T", props["name"])
	}
	if nameProp["type"] != "" {
		t.Errorf("name.type = %v, want empty string", nameProp["type"])
	}
	if nameProp["description"] != "" {
		t.Errorf("name.description = %v, want empty string", nameProp["description"])
	}
}

// TestExtractSchema_空字典 测试空输入
func TestExtractSchema_空字典(t *testing.T) {
	result := ExtractSchema(map[string]any{})
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// TestExtractSchemaFromJSON_从字符串解析 测试从 JSON 字符串提取
func TestExtractSchemaFromJSON_从字符串解析(t *testing.T) {
	jsonStr := `{"type": "object", "properties": {"id": {"type": "number"}}}`
	result := ExtractSchemaFromJSON(jsonStr)
	if result["type"] != "" {
		t.Errorf("type = %v, want empty string", result["type"])
	}
}

// TestExtractSchemaFromJSON_无效JSON 测试无效 JSON
func TestExtractSchemaFromJSON_无效JSON(t *testing.T) {
	result := ExtractSchemaFromJSON("not json")
	if len(result) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", result)
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestExtractSchema"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/schema_extractor.go internal/evolving/optimizer/tool_call/schema_extractor_test.go
git commit -m "feat(evolving): add schema extractor for tool_call (9.72b)"
```

---

### Task 3: default_configs.go + default_configs_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/default_configs.go`
- Create: `internal/evolving/optimizer/tool_call/default_configs_test.go`

Python 对标：`default_configs.py`

- [ ] **Step 1: 创建 default_configs.go**

```go
package tool_call

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// DefaultConfigEg Example Stage 默认配置。
//
// 对齐 Python: default_config_eg
var DefaultConfigEg = map[string]any{
	"gen_model_id":       "gpt-5-mini",
	"eval_model_id":      "gpt-5-mini",
	"verbose":            1,
	"num_init_loop":      1,
	"num_refine_steps":   1,
	"num_feedback_steps": 2,
	"score_eval_weight":  0.0,
	"beam_width":         2,
	"expand_num":         3,
	"max_depth":          2,
	"num_workers":        2,
	"top_k":              5,
}

// DefaultConfigDesc Description Stage 默认配置。
//
// 对齐 Python: default_config_desc
var DefaultConfigDesc = map[string]any{
	"gen_model_id":          "gpt-5-mini",
	"eval_model_id":         "gpt-5-mini",
	"verbose":               1,
	"num_init_loop":         1,
	"num_feedback_steps":    2,
	"score_eval_weight":     0.0,
	"num_examples_for_desc": 4,
	"beam_width":            2,
	"expand_num":            2,
	"max_depth":             2,
	"num_workers":           2,
	"top_k":                 3,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 default_configs_test.go**

```go
package tool_call

import "testing"

// TestDefaultConfigEg_完整 测试 Example Stage 配置完整性
func TestDefaultConfigEg_完整(t *testing.T) {
	requiredKeys := []string{
		"gen_model_id", "eval_model_id", "verbose",
		"num_init_loop", "num_refine_steps", "num_feedback_steps",
		"score_eval_weight", "beam_width", "expand_num",
		"max_depth", "num_workers", "top_k",
	}
	for _, key := range requiredKeys {
		if _, ok := DefaultConfigEg[key]; !ok {
			t.Errorf("DefaultConfigEg missing key: %s", key)
		}
	}
}

// TestDefaultConfigDesc_完整 测试 Description Stage 配置完整性
func TestDefaultConfigDesc_完整(t *testing.T) {
	requiredKeys := []string{
		"gen_model_id", "eval_model_id", "verbose",
		"num_init_loop", "num_feedback_steps",
		"score_eval_weight", "num_examples_for_desc",
		"beam_width", "expand_num", "max_depth",
		"num_workers", "top_k",
	}
	for _, key := range requiredKeys {
		if _, ok := DefaultConfigDesc[key]; !ok {
			t.Errorf("DefaultConfigDesc missing key: %s", key)
		}
	}
}

// TestDefaultConfigEg_Desc差异 测试两个配置的差异键
func TestDefaultConfigEg_Desc差异(t *testing.T) {
	// eg 有 num_refine_steps，desc 没有
	if _, ok := DefaultConfigEg["num_refine_steps"]; !ok {
		t.Error("DefaultConfigEg should have num_refine_steps")
	}
	if _, ok := DefaultConfigDesc["num_refine_steps"]; ok {
		t.Error("DefaultConfigDesc should not have num_refine_steps")
	}
	// desc 有 num_examples_for_desc，eg 没有
	if _, ok := DefaultConfigDesc["num_examples_for_desc"]; !ok {
		t.Error("DefaultConfigDesc should have num_examples_for_desc")
	}
	if _, ok := DefaultConfigEg["num_examples_for_desc"]; ok {
		t.Error("DefaultConfigEg should not have num_examples_for_desc")
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestDefaultConfig"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/default_configs.go internal/evolving/optimizer/tool_call/default_configs_test.go
git commit -m "feat(evolving): add default configs for tool_call optimizer (9.72b)"
```

---

### Task 4: rits.go + rits_test.go（InvokeWithVerify 薄包装）

**Files:**
- Create: `internal/evolving/optimizer/tool_call/rits.go`
- Create: `internal/evolving/optimizer/tool_call/rits_test.go`

Python 对标：`rits.py` 中的 `get_rits_response` / `rits_response`，但不复刻独立 LLM 调用，改为复用 llm_resilience。

- [ ] **Step 1: 创建 rits.go**

```go
package tool_call

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// VerifyFunc 验证+解析函数类型。
// 接收 LLM 输出文本，返回解析后的对象；验证失败时返回 error 触发重试。
//
// 对齐 Python: verify_fn(output) → output_json / raise
type VerifyFunc func(string) (any, error)

// InvokeWithVerify 带验证的 LLM 文本调用。
// 复用 llm_resilience.InvokeTextWithRetry，将 Python 的 verify_fn 适配为
// isResultUsable（验证文本合法性）+ parseResult（解析验证后的结果）两步。
//
// 对齐 Python: get_rits_response(model_id, prompt, api_key, verify_fn, max_attempts, ...)
//
// 适配逻辑：
//   - verifyFn 失败 → isResultUsable 返回 false → 触发 llm_resilience 重试
//   - verifyFn 成功 → 缓存 parsedResult → isResultUsable 返回 true
//   - 最终返回缓存的 parsedResult
func InvokeWithVerify(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
	verifyFn VerifyFunc,
) (any, error) {
	var cachedResult any
	var parseErr error

	isResultUsable := func(text string) bool {
		if verifyFn == nil {
			return true
		}
		result, err := verifyFn(text)
		if err != nil {
			parseErr = err
			return false
		}
		cachedResult = result
		return true
	}

	raw, err := llm_resilience.InvokeTextWithRetry(
		ctx, model, modelName, prompt, policy,
		llm_resilience.WithIsResultUsable(isResultUsable),
	)
	if err != nil {
		// 对齐 Python get_rits_response: 吞异常返回 error 字典
		return map[string]any{
			"error": fmt.Sprintf("Cannot complete LLM call. Error: %v", err),
		}, nil
	}

	if verifyFn == nil {
		return raw, nil
	}

	return cachedResult, nil
}

// InvokeText 简单 LLM 文本调用（无 verify_fn）。
// 内部直接调用 llm_resilience.InvokeTextWithRetry。
//
// 对齐 Python: rits_response(model_id, prompt, llm_api_key) 不带 verify_fn 的情况
func InvokeText(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	prompt string,
	policy llm_resilience.LLMInvokePolicy,
) (string, error) {
	return llm_resilience.InvokeTextWithRetry(ctx, model, modelName, prompt, policy)
}

// InvokeFunctionCall 使用 LLM Function Calling 模式生成函数调用。
// SimpleEval 专用，直接调用 model.Invoke + WithTools。
//
// 对齐 Python: SimpleEval._generate_function_call 中 client.invoke(messages, tools=[...])
func InvokeFunctionCall(
	ctx context.Context,
	model *llm.Model,
	modelName string,
	instruction string,
	toolInfo llmschema.ToolInfoInterface,
) (*llmschema.ToolCall, error) {
	messages := model_clients.NewMessagesParam(
		llmschema.NewUserMessage(instruction),
	)
	response, err := model.Invoke(ctx, messages,
		model_clients.WithInvokeModel(modelName),
		model_clients.WithTools(toolInfo),
	)
	if err != nil {
		return nil, err
	}

	toolCalls := response.GetToolCalls()
	if len(toolCalls) == 0 {
		return nil, fmt.Errorf("LLM did not generate any tool calls")
	}
	return toolCalls[0], nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 rits_test.go**

```go
package tool_call

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
)

// TestInvokeWithVerify_无VerifyFunc 测试不带验证函数
func TestInvokeWithVerify_无VerifyFunc(t *testing.T) {
	// 无 model 时仅验证逻辑分支
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:         1,
		TotalBudgetSecs:    1,
		AttemptTimeoutSecs: 1,
	}
	// 传入 nil model 会报错，验证吞异常行为
	result, err := InvokeWithVerify(context.Background(), nil, "test", "prompt", policy, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 应返回 error 字典
	errMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if _, hasErr := errMap["error"]; !hasErr {
		t.Error("expected error key in result map")
	}
}

// TestInvokeWithVerify_VerifyFunc失败 测试验证函数失败时重试
func TestInvokeWithVerify_VerifyFunc失败(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:         1,
		TotalBudgetSecs:    1,
		AttemptTimeoutSecs: 1,
	}
	verifyFn := func(text string) (any, error) {
		return nil, fmt.Errorf("parse failed")
	}
	result, err := InvokeWithVerify(context.Background(), nil, "test", "prompt", policy, verifyFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 应返回 error 字典（因为 model 为 nil 导致调用失败）
	errMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if _, hasErr := errMap["error"]; !hasErr {
		t.Error("expected error key in result map")
	}
}

// TestInvokeText_无Model 测试简单调用（无 model）
func TestInvokeText_无Model(t *testing.T) {
	policy := llm_resilience.LLMInvokePolicy{
		MaxAttempts:         1,
		TotalBudgetSecs:    1,
		AttemptTimeoutSecs: 1,
	}
	_, err := InvokeText(context.Background(), nil, "test", "prompt", policy)
	if err == nil {
		t.Error("expected error for nil model")
	}
}
```

- [ ] **Step 3: 运行测试（修复编译问题）**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/optimizer/tool_call/...`
Expected: 编译通过

Run: `go test ./internal/evolving/optimizer/tool_call/... -v -run "TestInvokeWithVerify|TestInvokeText"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/rits.go internal/evolving/optimizer/tool_call/rits_test.go
git commit -m "feat(evolving): add InvokeWithVerify wrapper for tool_call (9.72b)"
```

---

### Task 5: beam_search.go + beam_search_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/beam_search.go`
- Create: `internal/evolving/optimizer/tool_call/beam_search_test.go`

Python 对标：`beam_search.py`

- [ ] **Step 1: 创建 beam_search.go**

包含 `TreeNode`, `BeamSearchMethod` 接口, `BeamSearch` 结构体。
`Search` 方法对齐 Python `search(tool)`，`expand` 用 goroutine + channel 并行，`prune` 按分数排序剪枝，`checkEarlyStop` 早停检查。

核心签名：
```go
type TreeNode struct { Data, Score, Results, History, Parent, Children }
type BeamSearchMethod interface { Step(ctx, tool, examples, prevOutputs, it) (any, any, float64, error); GetExamples(ctx, tool) any }
type BeamSearch struct { method, beamWidth, expandNum, maxDepth, numWorkers, ... }
func NewBeamSearch(method BeamSearchMethod, opts ...BeamSearchOption) *BeamSearch
func (bs *BeamSearch) Search(ctx context.Context, tool map[string]any) ([][]any, error)
```

完整实现对照 Python beam_search.py 的 `search`/`expand`/`prune`/`check_early_stop`，用 `sync.WaitGroup` + `chan *TreeNode` 替代 Python 的 `ThreadPoolExecutor` + `as_completed`。

- [ ] **Step 2: 创建 beam_search_test.go**

使用 mock BeamSearchMethod 测试：
- TreeNode 深度计算
- BeamSearch.Search 基本流程
- BeamSearch.prune 剪枝
- BeamSearch.checkEarlyStop 早停
- 超时退出

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestTreeNode|TestBeamSearch"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/beam_search.go internal/evolving/optimizer/tool_call/beam_search_test.go
git commit -m "feat(evolving): add BeamSearch with goroutine parallelism (9.72b)"
```

---

### Task 6: api_wrapper.go + api_wrapper_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/api_wrapper.go`
- Create: `internal/evolving/optimizer/tool_call/api_wrapper_test.go`

Python 对标：`customized_api.py` + `callable_fortest.py`

- [ ] **Step 1: 创建 api_wrapper.go**

包含 `APIWrapperFunc` 类型, `SimpleAPIWrapper`, `SimpleAPIWrapperFromCallable`, `MakeSyncMCPCaller`。

核心签名：
```go
type APIWrapperFunc func(tool map[string]any, toolInput map[string]any) (string, int)
type SimpleAPIWrapperFromCallable struct { functions map[string]APIWrapperFunc; fnCallName string }
func NewSimpleAPIWrapperFromCallable(callable APIWrapperFunc, name string) *SimpleAPIWrapperFromCallable
func (w *SimpleAPIWrapperFromCallable) Call(tool, toolInput map[string]any) (string, int)
func MakeSyncMCPCaller(url, name string) APIWrapperFunc
```

`MakeSyncMCPCaller` 内部使用已有 `mcp.NewMcpClient` + `SseClient` 的 Connect/CallTool/Disconnect 流程。

- [ ] **Step 2: 创建 api_wrapper_test.go**

测试：
- SimpleAPIWrapperFromCallable.Call 成功/失败
- MakeSyncMCPCaller 构造（无法真实连接，仅验证类型签名）

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestSimpleAPIWrapper|TestMakeSyncMCPCaller"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/api_wrapper.go internal/evolving/optimizer/tool_call/api_wrapper_test.go
git commit -m "feat(evolving): add API wrapper and MakeSyncMCPCaller (9.72b)"
```

---

### Task 7: base_method.go + base_method_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/base_method.go`
- Create: `internal/evolving/optimizer/tool_call/base_method_test.go`

Python 对标：`base_method.py`

- [ ] **Step 1: 创建 base_method.go**

包含 `BaseMethod` 结构体和 `ProduceAnswerFromAPICall` 方法。

核心签名：
```go
type BaseMethod struct { config map[string]any; verbose bool; model *llm.Model }
func NewBaseMethod(config map[string]any, model *llm.Model) *BaseMethod
func (m *BaseMethod) ProduceAnswerFromAPICall(ctx context.Context, instruction, docStr, apiResponse string) (string, error)
```

`ProduceAnswerFromAPICall` 内部使用 `InvokeWithVerify`，提示词一比一复刻 Python `base_method.py` 中的 answer generation prompt。

- [ ] **Step 2: 创建 base_method_test.go**

测试 ProduceAnswerFromAPICall 签名（mock model）。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestBaseMethod"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/base_method.go internal/evolving/optimizer/tool_call/base_method_test.go
git commit -m "feat(evolving): add BaseMethod with ProduceAnswerFromAPICall (9.72b)"
```

---

### Task 8: eval.go + eval_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/eval.go`
- Create: `internal/evolving/optimizer/tool_call/eval_test.go`

Python 对标：`customized_eval.py`

- [ ] **Step 1: 创建 eval.go**

包含 `SimpleEval`, `EvalResult`, `EvalItemResult`, `EvalError` 结构体和所有方法。

核心方法对齐 Python `SimpleEval`：
- `Eval` — 对齐 `__call__`
- `evaluateSingleExample` — 对齐 `_evaluate_single_example`
- `generateFunctionCall` — 对齐 `_generate_function_call`（使用 `InvokeFunctionCall`）
- `evaluateFunctionCallAccuracy` — 对齐 `_evaluate_function_call_accuracy`
- `compareParameterValues` — 对齐 `_compare_parameter_values`
- `evaluateOutputEffectiveness` — 对齐 `_evaluate_output_effectiveness`
- `simpleOutputComparison` — 对齐 `_simple_output_comparison`

均值/标准差用标准库实现，不依赖 numpy。`evaluateOutputEffectiveness` 提示词一比一复刻 Python。

- [ ] **Step 2: 创建 eval_test.go**

测试纯逻辑方法（无需 LLM）：
- `evaluateFunctionCallAccuracy` 各种场景
- `compareParameterValues` 类型容忍比较
- `simpleOutputComparison` 简单比较

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestSimpleEval|TestEvaluateFunction|TestCompare|TestSimpleOutput"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/eval.go internal/evolving/optimizer/tool_call/eval_test.go
git commit -m "feat(evolving): add SimpleEval for tool description evaluation (9.72b)"
```

---

### Task 9: example_method.go + example_method_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/example_method.go`
- Create: `internal/evolving/optimizer/tool_call/example_method_test.go`

Python 对标：`toolcall_example_method.py`

- [ ] **Step 1: 创建 example_method.go**

包含 `APICallToExampleMethod` 结构体，实现 `BeamSearchMethod` 接口。

核心方法对齐 Python：
- `Step` — 对齐 `step(tool, examples, prev_outputs, it)`
- `GenerateAPICallFromDescription` — 对齐 `generate_api_call_from_description`
- `CritiqueAPICall` — 对齐 `critique_api_call`
- `GenerateInstructionFromAPICall` — 对齐 `generate_instruction_from_api_call`
- `CritiqueInstruction` — 对齐 `critique_instruction`
- `BatchReflectionWithScores` — 对齐 `batch_reflection_with_scores`
- `GetOriginalDescription` — 对齐 `get_original_description`

所有提示词一比一复刻 Python 原文。内部使用 `InvokeWithVerify` + `VerifyFunc`。

- [ ] **Step 2: 创建 example_method_test.go**

测试：
- `GetOriginalDescription` 各种格式
- `Step` 签名验证（mock model）

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestAPICallToExampleMethod"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/example_method.go internal/evolving/optimizer/tool_call/example_method_test.go
git commit -m "feat(evolving): add APICallToExampleMethod for example generation (9.72b)"
```

---

### Task 10: description_method.go + description_method_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/description_method.go`
- Create: `internal/evolving/optimizer/tool_call/description_method_test.go`

Python 对标：`description_example_method.py`

- [ ] **Step 1: 创建 description_method.go**

包含 `ToolDescriptionMethod` 结构体，实现 `BeamSearchMethod` 接口。

核心方法对齐 Python：
- `Step` — 对齐 `step(tool, examples, prev_outputs, it)`
- `Generate` — 对齐 `generate`
- `EvalLoop` — 对齐 `eval_loop`
- `CritiqueDescriptions` — 对齐 `critique_descriptions`（正负例对比版）
- `CritiqueAllDescriptions` — 对齐 `critique_all_descriptions`
- `CritiqueNegativeExamples` — 对齐 `critique_negative_examples`
- `GenerateDescriptionFromDocumentation` — 对齐 `generate_description_from_documentation`
- `LoadExamples` — 对齐 `load_examples`
- `GetNegativeExamples` — 对齐 `get_negative_examples`
- `GetOriginalDescription` — 对齐 `get_original_description`
- `GetExamples` — 对齐 `get_examples`

所有提示词一比一复刻 Python 原文。

- [ ] **Step 2: 创建 description_method_test.go**

测试：
- `GetOriginalDescription` 各种格式
- `GetExamples` / `LoadExamples` / `GetNegativeExamples` 文件 IO
- `Step` 签名验证

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestToolDescriptionMethod"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/description_method.go internal/evolving/optimizer/tool_call/description_method_test.go
git commit -m "feat(evolving): add ToolDescriptionMethod for description optimization (9.72b)"
```

---

### Task 11: reviewer.go + reviewer_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/reviewer.go`
- Create: `internal/evolving/optimizer/tool_call/reviewer_test.go`

Python 对标：`customized_reviewer.py`

- [ ] **Step 1: 创建 reviewer.go**

包含 `ToolDescriptionReviewer` 结构体和所有方法。

核心方法对齐 Python：
- `Format` — 对齐 `format(json_schema, description, example)`
- `CleanAndDeduplicate` — 对齐 `clean_and_deduplicate(data)`
- `CrossCheck` — 对齐 `cross_check(data, ori_tool)`
- `TranslateToChinese` — 对齐 `translate_to_chinese(data)`
- `Process` — 对齐 `process(data, ori_tool, steps)`
- `isMostlyEnglish` — 对齐 `_is_mostly_english(text)`

所有提示词一比一复刻 Python 原文（4 个 format prompt 变体 + clean prompt + cross_check prompt + translate prompt）。

- [ ] **Step 2: 创建 reviewer_test.go**

测试：
- `isMostlyEnglish` 各种语言判断
- `Process` 步骤顺序
- `Format` 签名验证

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestToolDescriptionReviewer|TestIsMostlyEnglish"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/reviewer.go internal/evolving/optimizer/tool_call/reviewer_test.go
git commit -m "feat(evolving): add ToolDescriptionReviewer with clean/cross_check/translate (9.72b)"
```

---

### Task 12: pipeline.go + pipeline_test.go

**Files:**
- Create: `internal/evolving/optimizer/tool_call/pipeline.go`
- Create: `internal/evolving/optimizer/tool_call/pipeline_test.go`

Python 对标：`customized_pipline.py`

- [ ] **Step 1: 创建 pipeline.go**

包含 `CustomizedPipeline` 函数。

核心签名：
```go
func CustomizedPipeline(
    ctx context.Context,
    stage string,
    tool map[string]any,
    config map[string]any,
    toolCallable APIWrapperFunc,
    model *llm.Model,
) ([][]any, error)
```

对齐 Python `customized_pipeline` 内部流程：
1. 创建 `SimpleAPIWrapperFromCallable`
2. 创建 `SimpleEval`
3. 根据 stage 创建 `APICallToExampleMethod` 或 `ToolDescriptionMethod`
4. 创建 `BeamSearch` 并调用 `Search`
5. 保存结果到 JSON 文件

- [ ] **Step 2: 创建 pipeline_test.go**

测试：
- stage 参数校验
- 签名验证

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestCustomizedPipeline"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/pipeline.go internal/evolving/optimizer/tool_call/pipeline_test.go
git commit -m "feat(evolving): add CustomizedPipeline for tool optimization (9.72b)"
```

---

### Task 13: base.go + base_test.go（ToolOptimizerBase）

**Files:**
- Create: `internal/evolving/optimizer/tool_call/base.go`
- Create: `internal/evolving/optimizer/tool_call/base_test.go`

Python 对标：`base.py`

- [ ] **Step 1: 创建 base.go**

包含 `ToolOptimizerBase` 结构体，实现 `optimizer.BaseOptimizer` 接口。

核心方法对齐 Python：
- `Domain()` → "tool"
- `DefaultTargets()` → ["tool_description"]
- `RequiresForwardData()` → false
- `OptimizeTool(ctx, tool, toolCallable)` — 两阶段迭代优化
- `Backward(ctx, signals)` → nil（空实现）
- `Step()` → 空 map（空实现）
- `Bind` / `AddTrajectory` / `GetTrajectories` / `ClearTrajectories` / `Parameters` / `SelectSignals` — 委托 BaseOptimizerMixin

`OptimizeTool` 内部流程对齐 Python `optimize_tool`：
1. 保存 originalDesc
2. 迭代 maxTurns 轮：Example Stage → Description Stage
3. ToolDescriptionReviewer.Process + Format
4. 返回 finalDesc

依赖未完成项标注：`// ⤵️ 9.70: 等待 Trainer 实现后回填 Model 注入方式`

- [ ] **Step 2: 创建 base_test.go**

测试：
- Domain / DefaultTargets / RequiresForwardData
- Backward 返回 nil
- Step 返回空 map
- OptimizeTool 签名验证

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestToolOptimizerBase"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/evolving/optimizer/tool_call/base.go internal/evolving/optimizer/tool_call/base_test.go
git commit -m "feat(evolving): add ToolOptimizerBase with OptimizeTool (9.72b)"
```

---

### Task 14: 更新 optimizer/doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/evolving/optimizer/doc.go` — 添加 tool_call/ 子目录
- Modify: `IMPLEMENTATION_PLAN.md` — 更新 9.72b 状态为 ✅

- [ ] **Step 1: 更新 optimizer/doc.go**

在文件目录中添加 tool_call/ 条目。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 9.72b 行的 `☐` 改为 `✅`。

- [ ] **Step 3: 提交**

```bash
git add internal/evolving/optimizer/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(evolving): update doc.go and plan for 9.72b completion"
```

---

### Task 15: 全量编译 + 测试验证

**Files:**
- All files in `internal/evolving/optimizer/tool_call/`

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译通过，无错误

- [ ] **Step 2: 运行包级测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/optimizer/tool_call/... -v -cover`
Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 运行整体测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -cover`
Expected: 所有测试通过

- [ ] **Step 4: 最终提交**

```bash
git add -A
git commit -m "feat(evolving): complete 9.72b ToolOptimizer implementation"
```
