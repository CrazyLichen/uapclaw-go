# 9.72b ToolOptimizer 对齐修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.72b ToolOptimizer Go 实现与 Python 对齐审查发现的 3 个问题

**Architecture:** 三处独立修复——reviewer.go 补全 Format prompt 变体、eval.go apiWrapper nil 返回 error、format.go ParseJSON 追加 Python 字面量替换。修复 #2 涉及签名变更，需同步修改调用方和测试。

**Tech Stack:** Go 1.x, testify/assert, testify/require

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/evolving/optimizer/tool_call/reviewer.go` | 修改 | Format 方法补全 3 个英文 prompt 变体 |
| `internal/evolving/optimizer/tool_call/eval.go` | 修改 | evaluateSingleExample 返回 error + Eval() 处理 error |
| `internal/evolving/optimizer/tool_call/description_method.go` | 修改 | EvalLoop 适配 Eval() 新签名 |
| `internal/evolving/optimizer/tool_call/example_method.go` | 修改 | Step 中适配 Eval() 新签名 |
| `internal/evolving/optimizer/tool_call/format.go` | 修改 | ParseJSON 追加 Python 字面量替换 |
| `internal/evolving/optimizer/tool_call/eval_test.go` | 修改 | 适配新签名 + 新增测试 |
| `internal/evolving/optimizer/tool_call/format_test.go` | 修改 | 新增 Python 字面量测试 |

---

### Task 1: format.go — ParseJSON 追加 Python 字面量替换

**Files:**
- Modify: `internal/evolving/optimizer/tool_call/format.go:65-72`
- Test: `internal/evolving/optimizer/tool_call/format_test.go`

- [ ] **Step 1: 编写失败测试**

在 `format_test.go` 末尾添加：

```go
// TestParseJSON_Python字面量 测试 Python 字面量（True/False/None）替换
func TestParseJSON_Python字面量(t *testing.T) {
	output := `{"active": True, "visible": False, "extra": None}`
	result := ParseJSON(output)
	if result["active"] != true {
		t.Errorf("active = %v, want true", result["active"])
	}
	if result["visible"] != false {
		t.Errorf("visible = %v, want false", result["visible"])
	}
	if result["extra"] != nil {
		t.Errorf("extra = %v, want nil", result["extra"])
	}
}

// TestParseJSON_单引号加字面量 测试单引号和 Python 字面量混合
func TestParseJSON_单引号加字面量(t *testing.T) {
	output := `{'active': True, 'visible': False}`
	result := ParseJSON(output)
	if result["active"] != true {
		t.Errorf("active = %v, want true", result["active"])
	}
	if result["visible"] != false {
		t.Errorf("visible = %v, want false", result["visible"])
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestParseJSON_Python字面量|TestParseJSON_单引号加字面量" 2>&1 | tail -20`
Expected: FAIL — `True` 不是合法 JSON 值，`json.Unmarshal` 失败后单引号替换也不够

- [ ] **Step 3: 实现修复**

在 `format.go` 的 `ParseJSON` 函数中，将第 65-72 行的兜底逻辑改为：

```go
	if err := json.Unmarshal([]byte(extracted), &result); err != nil {
		// 对齐 Python: ast.literal_eval 的核心场景
		// 1. 单引号 → 双引号
		fixed := strings.ReplaceAll(extracted, "'", `"`)
		// 2. Python 字面量 → JSON 字面量
		fixed = strings.ReplaceAll(fixed, "True", "true")
		fixed = strings.ReplaceAll(fixed, "False", "false")
		fixed = strings.ReplaceAll(fixed, "None", "null")
		if jsonErr := json.Unmarshal([]byte(fixed), &result); jsonErr != nil {
			return map[string]any{}
		}
	}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestParseJSON" 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/tool_call/format.go internal/evolving/optimizer/tool_call/format_test.go
git commit -m "fix(tool_call): ParseJSON 追加 Python 字面量替换对齐 ast.literal_eval"
```

---

### Task 2: reviewer.go — Format 补全 4 个 prompt 变体

**Files:**
- Modify: `internal/evolving/optimizer/tool_call/reviewer.go:57-82`

- [ ] **Step 1: 在 Format 方法中补全 promptOriginal/prompt1/prompt2 变量**

将 `reviewer.go` 第 57-82 行替换为以下内容（保留现有中文 prompt 不变，在其前面添加 3 个英文 prompt 变体）：

```go
	// 对齐 Python: prompt_original — 一比一复刻，定义后未使用
	schemaJSON, _ := json.MarshalIndent(jsonSchema, "", "  ")
	promptOriginal := fmt.Sprintf(`You will receive an input that contains a textual description.
The input may be free-form text, bullet points, or JSON in any structure.
Your task is to convert that content into MY target JSON format, while keeping the information and meaning exactly the same.

Do not add new information, remove information, or reinterpret ambiguous content.
Only reorganize and reformat the content according to the schema provided below.
Target JSON format:
%s

Output only valid JSON, following the exact structure of the target schema. Do not include explanations, comments, or additional text outside the JSON.

Now convert my following input to desired JSON format:
Input to be converted:
%s
`, string(schemaJSON), description)
	_ = promptOriginal // 对齐 Python：定义后未使用

	// 对齐 Python: prompt_1 — 一比一复刻，定义后未使用
	prompt1 := fmt.Sprintf(`You will receive an input that contains a textual description.
Your task is to convert it into the target JSON format below.

Rules:
- Preserve all information and meaning exactly.
- Do not add, remove, or reinterpret any information.
- You may rewrite and compress wording to eliminate redundancy.
- Do not restate information already implied by the schema (e.g. type=number/string, required fields).
- Enum/value lists must appear only once at the most relevant location; do not repeat them at field level.
- Use short, content-focused phrases for field descriptions.

Target JSON format:
%s

Output only valid JSON following the exact structure.
No explanations or extra text.

Input:
%s
`, string(schemaJSON), description)
	_ = prompt1 // 对齐 Python：定义后未使用

	// 对齐 Python: prompt_2 — 一比一复刻，定义后未使用
	prompt2 := fmt.Sprintf(`You will receive an input that contains a textual description.
Your task is to convert it into the target JSON format below.

Rules:
- Preserve all information and meaning exactly.
- Do not add, remove, or reinterpret any information.
- You may rewrite and compress wording to eliminate redundancy.
- Do not restate information already implied by the schema (e.g. field types, required fields).
- Do not describe required fields in natural language (e.g. phrases like "each item includes/contains …").
- Enum/value lists must appear only once at the most relevant location.

Target JSON format:
%s

Output only valid JSON following the exact structure.
No explanations or extra text.

Input:
%s
`, string(schemaJSON), description)
	_ = prompt2 // 对齐 Python：定义后未使用

	// 对齐 Python: prompt（中文版）— 实际使用的版本
	prompt := fmt.Sprintf(`将下面输入转换为目标 JSON 结构。必须满足：

- 输出只允许是有效 JSON，且严格匹配目标结构的键路径与层级（不多不少）。
- 语义必须完全保留：不新增、不删减、不改写含义；可改写措辞以压缩。
- description 去冗余是强制要求：
    - 任何 "每项包含/含有/由…组成/字段包括…" 这类字段清单式描述都必须删除或改写为非清单表述。
    - 不得在 description 中重复 schema 已表达的信息：字段名、字段类型、required 已涵盖的"必填"。
    - 仅保留 schema 无法表达或未显式表达的约束到 description，例如：
        - 覆盖区间/不得留隙/分段规则
        - 默认值语义（如 inflationRate 默认 0）
        - 业务规则（按年累加、考虑通胀等）
    - 枚举值列表只出现一次，放在最贴近字段的位置（通常是该字段的 description）；不得在父级/子级重复。
    如输入中 description 同时包含"字段清单 + 业务约束"，只保留业务约束部分。
    - 若某个 description 完全是冗余字段清单，允许变为简短描述，但不得留空（除非输入本身为空）。
- 请直接输出转换后的 JSON，不要附加解释。

这是目标的json 模板:
%s

下面是你需要修改的json，生成后请自检：所有 description 中不得出现"含/包含/包括/each item/contains/fields"等字段列举句式；否则重写直到满足。

Input:
%s
`, string(schemaJSON), description)
```

注意：`schemaJSON` 变量的声明需要从 `prompt` 定义之前移到 `promptOriginal` 定义之前。修改后 `schemaJSON` 只声明一次，供 4 个 prompt 共用。

- [ ] **Step 2: 更新 Format 方法注释**

将第 47-50 行的方法注释从：

```go
// Format 将文本描述转换为目标 JSON 结构。
// 使用中文版 prompt，一比一复刻 Python 原文。
//
// 对齐 Python: ToolDescriptionReviewer.format(json_schema, description, example)
```

改为：

```go
// Format 将文本描述转换为目标 JSON 结构。
// 定义 4 个 prompt 变体（promptOriginal/prompt1/prompt2/中文prompt），一比一复刻 Python 原文。
// 最终只使用中文 prompt 传给 LLM，与 Python 行为一致。
//
// 对齐 Python: ToolDescriptionReviewer.format(json_schema, description, example)
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestIsMostlyEnglish|TestFormat" 2>&1 | tail -20`
Expected: ALL PASS（Format 的测试走 mock，不受 prompt 变量影响）

- [ ] **Step 4: 编译检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/optimizer/tool_call/... 2>&1 | tail -10`
Expected: 无错误

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/optimizer/tool_call/reviewer.go
git commit -m "fix(tool_call): Format 补全 4 个 prompt 变体对齐 Python customized_reviewer.py"
```

---

### Task 3: eval.go — evaluateSingleExample 返回 error + Eval() 处理 error

**Files:**
- Modify: `internal/evolving/optimizer/tool_call/eval.go:301-395`
- Modify: `internal/evolving/optimizer/tool_call/eval.go:143-182`
- Modify: `internal/evolving/optimizer/tool_call/description_method.go:135-143`
- Modify: `internal/evolving/optimizer/tool_call/example_method.go:257-262`
- Modify: `internal/evolving/optimizer/tool_call/eval_test.go`

- [ ] **Step 1: 修改 evaluateSingleExample 签名和 apiWrapper nil 处理**

将 `eval.go` 第 301 行函数签名从：

```go
func (e *SimpleEval) evaluateSingleExample(
	ctx context.Context,
	tool map[string]any,
	description string,
	example ExampleTuple,
	exampleID int,
) EvalItemResult {
```

改为：

```go
func (e *SimpleEval) evaluateSingleExample(
	ctx context.Context,
	tool map[string]any,
	description string,
	example ExampleTuple,
	exampleID int,
) (EvalItemResult, error) {
```

将 `eval.go` 第 309-332 行（generateFunctionCall 失败时的零值返回）从：

```go
	generatedFnCall, err := e.generateFunctionCall(ctx, tool, description, example.Instruction)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "evaluateSingleExample").
			Int("example_id", exampleID).
			Err(err).
			Msg("生成函数调用出错")
		return EvalItemResult{
			Instruction:              example.Instruction,
			ExpectedFnCall:           example.FnCall,
			GeneratedFnCall:          nil,
			FnCallScore:              0.0,
			ExecutionResult:          nil,
			ExecutionError:           map[string]any{"error": err.Error()},
			OutputEffectivenessScore: 0.0,
			WeightedScore:            0.0,
			Answer:                   example.Answer,
			Errors: []EvalError{{
				FunctionName: getToolName(tool),
				Arguments:    map[string]any{},
				ErrorMsg:     err.Error(),
			}},
		}
	}
```

改为：

```go
	generatedFnCall, err := e.generateFunctionCall(ctx, tool, description, example.Instruction)
	if err != nil {
		logger.Error(logComponent).
			Str("method", "evaluateSingleExample").
			Int("example_id", exampleID).
			Err(err).
			Msg("生成函数调用出错")
		// 对齐 Python: generateFunctionCall 失败时返回 error
		return EvalItemResult{}, fmt.Errorf("生成函数调用出错: %w", err)
	}
```

将 `eval.go` 第 364-373 行（apiWrapper nil 处理）从：

```go
	} else {
		logger.Error(logComponent).
			Str("method", "evaluateSingleExample").
			Msg("缺少必需输入: api_wrapper")
		errors = append(errors, EvalError{
			FunctionName: getToolName(tool),
			Arguments:    map[string]any{},
			ErrorMsg:     "缺少必需输入: api_wrapper",
		})
	}
```

改为：

```go
	} else {
		// 对齐 Python: raise ValueError("Missing required input: api_wrapper")
		logger.Error(logComponent).
			Str("method", "evaluateSingleExample").
			Msg("缺少必需输入: api_wrapper")
		return EvalItemResult{}, fmt.Errorf("缺少必需输入: api_wrapper")
	}
```

将函数最后的 return 语句（第 383-394 行）从：

```go
	return EvalItemResult{
		...
	}
}
```

改为：

```go
	return EvalItemResult{
		Instruction:              example.Instruction,
		ExpectedFnCall:           example.FnCall,
		GeneratedFnCall:          generatedFnCall,
		FnCallScore:              fnCallScore,
		ExecutionResult:          executionResult,
		ExecutionError:           executionError,
		OutputEffectivenessScore: outputEffectivenessScore,
		WeightedScore:            weightedScore,
		Answer:                   example.Answer,
		Errors:                   errors,
	}, nil
}
```

- [ ] **Step 2: 修改 Eval() 循环处理 error**

将 `eval.go` 第 160-164 行从：

```go
		for i, example := range examples {
			result := e.evaluateSingleExample(ctx, tool, description, example, i)
			allResults = append(allResults, result)
			totalFnCallScore += result.FnCallScore
			totalOutputScore += result.OutputEffectivenessScore
```

改为：

```go
		for i, example := range examples {
			result, err := e.evaluateSingleExample(ctx, tool, description, example, i)
			if err != nil {
				// 对齐 Python: ValueError 传播中断整个评估
				logger.Error(logComponent).
					Str("method", "Eval").
					Int("example_id", i).
					Err(err).
					Msg("评估单个示例失败，中断评估")
				return nil
			}
			allResults = append(allResults, result)
			totalFnCallScore += result.FnCallScore
			totalOutputScore += result.OutputEffectivenessScore
```

- [ ] **Step 3: 修改 description_method.go EvalLoop 适配 nil 返回**

将 `description_method.go` 第 142 行从：

```go
	return m.evalFn.Eval(ctx, tool, description, examples, runs)
```

改为：

```go
	result := m.evalFn.Eval(ctx, tool, description, examples, runs)
	if result == nil {
		// 对齐 Python: ValueError 传播时返回默认 0 分
		return &EvalResult{ScoreAvg: 0, ScoreStd: 0, FnCallAccuracy: 0, OutputEffectiveness: 0}
	}
	return result
```

- [ ] **Step 4: 修改 example_method.go 适配 nil 返回**

将 `example_method.go` 第 257-259 行从：

```go
				// 对齐 Python: eval_res = self.eval_fn(tool, description, examples, runs=1)
				evalRes := m.evalFn.Eval(ctx, tool, description, exampleTuples, 1)
				evalScore = evalRes.ScoreAvg / 100.0
```

改为：

```go
				// 对齐 Python: eval_res = self.eval_fn(tool, description, examples, runs=1)
				evalRes := m.evalFn.Eval(ctx, tool, description, exampleTuples, 1)
				if evalRes != nil {
					evalScore = evalRes.ScoreAvg / 100.0
				} else {
					// 对齐 Python: ValueError 传播时评估分数为 0
					evalScore = 0.0
				}
```

- [ ] **Step 5: 编译检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/evolving/optimizer/tool_call/... 2>&1 | tail -10`
Expected: 无错误

- [ ] **Step 6: 修改 eval_test.go 适配新签名**

在 `eval_test.go` 中搜索所有直接调用 `evaluateSingleExample` 的测试（如果有），适配 `(EvalItemResult, error)` 返回值。

如果 `eval_test.go` 中没有直接调用 `evaluateSingleExample` 的测试（它是包内非导出函数，但同包测试可访问），则只需添加新测试：

在 `eval_test.go` 末尾添加：

```go
// TestEvaluateSingleExample_ApiWrapperNil 测试 apiWrapper 为 nil 时返回 error
func TestEvaluateSingleExample_ApiWrapperNil(t *testing.T) {
	e := NewSimpleEval(nil, map[string]any{}, 0.4, 0.6, nil)
	tool := map[string]any{"name": "test_tool", "type": "function"}
	example := ExampleTuple{
		Instruction: "test instruction",
		FnCall:      map[string]any{"name": "test_tool", "arguments": map[string]any{}},
		FnOutput:    "test output",
		Answer:      "test answer",
	}
	_, err := e.evaluateSingleExample(context.Background(), tool, "test desc", example, 0)
	if err == nil {
		t.Error("expected error when apiWrapper is nil")
	}
	if err.Error() != "缺少必需输入: api_wrapper" {
		t.Errorf("error = %q, want %q", err.Error(), "缺少必需输入: api_wrapper")
	}
}
```

同时需在文件头部添加 import：

```go
import (
	"context"
	"math"
	"testing"
)
```

注意：现有 `eval_test.go` 只导入了 `"math"` 和 `"testing"`，需添加 `"context"`。

- [ ] **Step 7: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/tool_call/... -v -run "TestEvaluateSingleExample_ApiWrapperNil|TestEvaluateFunctionCall|TestCompareParameter|TestSimpleOutput|TestMean|TestStd|TestNewSimpleEval|TestGetArgsMap" 2>&1 | tail -30`
Expected: ALL PASS

- [ ] **Step 8: 运行 tool_call 包完整测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/tool_call/... -v -count=1 2>&1 | tail -40`
Expected: ALL PASS

- [ ] **Step 9: 提交**

```bash
git add internal/evolving/optimizer/tool_call/eval.go internal/evolving/optimizer/tool_call/eval_test.go internal/evolving/optimizer/tool_call/description_method.go internal/evolving/optimizer/tool_call/example_method.go
git commit -m "fix(tool_call): apiWrapper nil 时返回 error 对齐 Python raise ValueError"
```

---

### Task 4: 最终验证

- [ ] **Step 1: 运行 tool_call 包完整测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/tool_call/... -count=1 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 2: 运行 evolving 模块测试确认无破坏**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/... -count=1 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 状态**

无需更新（9.72b 状态已为 ✅，本次是对齐修复补丁）
