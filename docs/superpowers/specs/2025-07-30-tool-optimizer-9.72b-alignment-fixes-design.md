# 9.72b ToolOptimizer 对齐修复设计

> 日期：2025-07-30
> 范围：9.72b ToolOptimizer Go 实现与 Python 对齐审查发现的 3 项修复

## 1. 背景

9.72b ToolOptimizer 实现对齐审查发现 7 个问题，经逐个讨论后确认需修复 3 项：

| # | 文件 | 问题 | 决定 |
|---|------|------|------|
| 1 | reviewer.go | Format 缺少 3 个英文 prompt 变体 | 补全 4 个变量，只用中文 prompt |
| 2 | eval.go | apiWrapper nil 时继续执行 vs Python raise | 直接返回 error |
| 3 | format.go | ParseJSON 缺少 Python 字面量替换 | 追加 True/False/None 替换 |

其余 4 项（rits.go 缺少 InvokeText/InvokeFunctionCall、explainations 拼写、runction 拼写、example 参数未使用）确认不修改。

## 2. 修复 #1：reviewer.go Format — 补全 4 个 prompt 变体

### 2.1 Python 参考

`customized_reviewer.py` 的 `format()` 方法定义了 4 个 prompt 变量：
- `prompt_original`（第 20-32 行）— 基础版英文 prompt
- `prompt_1`（第 33-52 行）— 精简规则版英文 prompt
- `prompt_2`（第 54-73 行）— 更严格规则版英文 prompt
- `prompt`（第 74-97 行）— 中文版 prompt（**最终使用的版本**）

Python 第 102 行 `get_rits_response('gpt-5.2', prompt, ...)` 只使用中文 `prompt`，其他三个定义后未引用。

### 2.2 Go 修改

**文件**：`internal/evolving/optimizer/tool_call/reviewer.go`

在 `Format` 方法内，现有中文 `prompt` 定义之前，按 Python 原文顺序添加三个变量：

```go
// 对齐 Python: prompt_original — 一比一复刻，定义后未使用
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

// 对齐 Python: prompt_1 — 一比一复刻，定义后未使用
_ = promptOriginal  // 对齐 Python：定义后未使用

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

_ = prompt1  // 对齐 Python：定义后未使用

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

_ = prompt2  // 对齐 Python：定义后未使用

// 对齐 Python: prompt（中文版）— 实际使用的版本
prompt := fmt.Sprintf(`...现有中文 prompt...`, string(schemaJSON), description)
```

### 2.3 测试

- 现有 `TestFormat_*` 测试无需修改（最终仍使用中文 prompt 传给 LLM）
- 可选：添加 `TestFormat_PromptVariantsExist` 验证变量确实被定义

## 3. 修复 #2：eval.go — apiWrapper nil 时返回 error

### 3.1 Python 参考

`customized_eval.py` 第 174-182 行：
```python
else:
    logger.error("Missing required input: api_wrapper")
    error_msg = "Missing required input: api_wrapper"
    errors.append({...})
    raise ValueError(error_msg)
```

Python 先记录日志、添加 error，然后 `raise ValueError` 中断当前 `_evaluate_single_example`。上层 `__call__` 循环没有 try/except，因此 ValueError 会直接传播中断整个评估。

### 3.2 Go 修改

**文件**：`internal/evolving/optimizer/tool_call/eval.go`

#### 改动 1：evaluateSingleExample 返回 error

```go
func (e *SimpleEval) evaluateSingleExample(
    ctx context.Context,
    tool map[string]any,
    description string,
    example ExampleTuple,
    exampleID int,
) (EvalItemResult, error) {
    // ... 现有逻辑 ...

    if e.apiWrapper != nil {
        // ... 现有 apiWrapper 调用逻辑 ...
    } else {
        // 对齐 Python: raise ValueError("Missing required input: api_wrapper")
        logger.Error(logComponent).
            Str("method", "evaluateSingleExample").
            Msg("缺少必需输入: api_wrapper")
        return EvalItemResult{}, fmt.Errorf("缺少必需输入: api_wrapper")
    }

    // ... 后续逻辑 ...
}
```

#### 改动 2：Eval() 循环处理 error

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
}
```

#### 改动 3：generateFunctionCall 失败时也返回 error

现有 generateFunctionCall 失败时返回零值 EvalItemResult，改为返回 error（与 apiWrapper nil 统一模式）：

```go
generatedFnCall, err := e.generateFunctionCall(ctx, tool, description, example.Instruction)
if err != nil {
    logger.Error(logComponent).
        Str("method", "evaluateSingleExample").
        Int("example_id", exampleID).
        Err(err).
        Msg("生成函数调用出错")
    return EvalItemResult{}, fmt.Errorf("生成函数调用出错: %w", err)
}
```

### 3.3 测试

- 修改 `TestEvaluateSingleExample_*` 适配新的返回签名 `(EvalItemResult, error)`
- 添加 `TestEvaluateSingleExample_ApiWrapperNil` 验证返回 error
- 修改 `TestEval_*` 验证 error 传播时返回 nil

## 4. 修复 #3：format.go ParseJSON — 追加 Python 字面量替换

### 4.1 Python 参考

`format.py` 第 22-24 行：
```python
output_json = json.loads(output)
except json.JSONDecodeError:
    output_json = ast.literal_eval(output)
```

`ast.literal_eval` 可解析 Python 字面量：`True`/`False`/`None`/单引号字符串等。

### 4.2 Go 修改

**文件**：`internal/evolving/optimizer/tool_call/format.go`

在现有单引号→双引号替换后，追加 Python 字面量替换，然后再尝试 json.Unmarshal：

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

### 4.3 测试

- 添加 `TestParseJSON_PythonLiterals` — 输入含 True/False/None，验证正确解析为 true/false/null
- 添加 `TestParseJSON_SingleQuotesAndLiterals` — 混合单引号 + Python 字面量
- 现有测试不受影响

## 5. 影响范围

| 修改文件 | 影响范围 | 破坏性 |
|----------|---------|--------|
| reviewer.go | Format 方法内部新增变量 | 无破坏性（死变量，不影响现有逻辑） |
| eval.go | evaluateSingleExample 签名变更 | **破坏性**：调用方和测试需适配 |
| eval_test.go | 适配新签名 + 新增测试 | 跟随 eval.go 变更 |
| format.go | ParseJSON 兜底逻辑增强 | 无破坏性（更宽松的解析） |
| format_test.go | 新增 Python 字面量测试 | 纯新增 |

## 6. 风险评估

- **修复 #1**：极低风险。新增死变量，不影响运行时行为
- **修复 #2**：中等风险。签名变更影响 evaluateSingleExample 所有调用方和测试。需确保 Eval() 循环的 error 处理不引入新的 nil pointer
- **修复 #3**：低风险。True/False/None 替换可能误替换 JSON 值中恰好包含这些字符串的场景（如 `"name": "TrueStory"`），但实际 LLM 输出中此类碰撞概率极低，且替换后 json.Unmarshal 仍会校验合法性
