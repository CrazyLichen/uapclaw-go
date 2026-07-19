# 9.72b ToolOptimizer 设计方案

## 1. 概述

9.72b 实现工具描述优化器（ToolOptimizer），属于自演化系统优化器子模块的第 2 个具体实现（9.72a 为 InstructionOptimizer，9.72c/d 分别为 Memory/SkillExperience 优化器）。

### 1.1 在 Agent 会话中的流程位置

```
9.70a Operator 基础接口        ← 定义可优化的 Operator 契约（ToolCallOperator 已暴露 tool_description）
9.70b Dataset + Constant        ← 提供训练数据和默认超参
9.70  Trainer                    ← 编排 evaluate→update→writeback 循环
9.70c Updater Protocol           ← 定义更新协议（bind/update/process）
9.71  Evaluator                  ← 评估器（精确匹配/LLM-as-Judge）
9.72e BaseOptimizer + LLMResilience  ← 优化器基类 + LLM 重试策略
9.72a InstructionOptimizer       ← LLM 维度：提示词文本梯度优化 ✅
9.72b ToolOptimizer              ← Tool 维度：工具描述优化器 ☐  ← 当前
9.72c MemoryOptimizer            ← Memory 维度：记忆参数优化器 ☐
9.72d SkillExperienceOptimizer   ← Skill 维度：技能经验优化器 ☐
```

### 1.2 核心作用

ToolOptimizer 解决：**工具的自然语言描述质量直接影响 LLM 的 function calling 准确率**。通过两阶段 Beam Search 迭代：

1. **Example Stage**（APICallToExampleMethod）：自动生成 API 调用示例（指令 + 函数调用 + 执行结果 + 回答），形成正负例集
2. **Description Stage**（ToolDescriptionMethod）：基于正负例，让 LLM 批判当前描述并生成增强描述

最终通过 ToolDescriptionReviewer（clean → cross_check → translate）三步后处理，输出结构化的高质量工具描述。

### 1.3 关键设计决策（已确认）

| 决策点 | 结论 |
|--------|------|
| LLM 调用封装 | 不复刻 rits.py，复用 llm_resilience + 薄包装层 |
| Function Calling 对接 | SimpleEval 直接持有 *llm.Model |
| 包结构 | 单包扁平，放 internal/evolving/optimizer/tool_call/ |
| Backward/Step | 对齐 Python 空实现 |
| MCP 封装 | Go 已有完整 MCP 客户端，MakeSyncMCPCaller 用已有 SseClient |

## 2. 文件目录

```
internal/evolving/optimizer/tool_call/
├── doc.go                          # 包文档
├── base.go                         # ToolOptimizerBase（含 OptimizeTool、Backward、Step）
├── base_test.go                    # ToolOptimizerBase 测试
├── schema_extractor.go             # ExtractSchema
├── schema_extractor_test.go        # ExtractSchema 测试
├── reviewer.go                     # ToolDescriptionReviewer
├── reviewer_test.go                # ToolDescriptionReviewer 测试
├── default_configs.go              # DefaultConfigEg / DefaultConfigDesc
├── default_configs_test.go         # 默认配置测试
├── rits.go                         # 薄包装：InvokeWithVerify（复用 llm_resilience）
├── rits_test.go                    # InvokeWithVerify 测试
├── format.go                       # ParseJSON / FormatPromptLlama
├── format_test.go                  # ParseJSON / FormatPromptLlama 测试
├── beam_search.go                  # BeamSearch + TreeNode
├── beam_search_test.go             # BeamSearch 测试
├── api_wrapper.go                  # SimpleAPIWrapper / SimpleAPIWrapperFromCallable / MakeSyncMCPCaller
├── api_wrapper_test.go             # API 封装测试
├── eval.go                         # SimpleEval
├── eval_test.go                    # SimpleEval 测试
├── base_method.go                  # BaseMethod
├── base_method_test.go             # BaseMethod 测试
├── example_method.go               # APICallToExampleMethod
├── example_method_test.go          # APICallToExampleMethod 测试
├── description_method.go           # ToolDescriptionMethod
├── description_method_test.go      # ToolDescriptionMethod 测试
├── pipeline.go                     # CustomizedPipeline
├── pipeline_test.go                # CustomizedPipeline 测试
```

对应 Python 代码：`openjiuwen/agent_evolving/optimizer/tool_call/`

## 3. 核心类型与接口

### 3.1 base.go — ToolOptimizerBase

```go
// ToolOptimizerBase 工具维度优化器基类。
// 固定 domain="tool"，默认优化目标为 ["tool_description"]。
// 核心入口是 OptimizeTool()，Backward/Step 对齐 Python 空实现。
//
// 对应 Python: ToolOptimizerBase
type ToolOptimizerBase struct {
    optimizer.BaseOptimizerMixin
    // maxTurns 最大迭代轮数
    maxTurns int
    // llmAPIKey LLM API 密钥
    llmAPIKey string
    // configEg Example Stage 配置
    configEg map[string]any
    // configDesc Description Stage 配置
    configDesc map[string]any
    // pathSaveDir 结果保存目录
    pathSaveDir string
    // model LLM 模型客户端
    model *llm.Model
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Domain() string` | `domain = "tool"` | 返回 "tool" |
| `DefaultTargets() []string` | `default_targets()` | 返回 `["tool_description"]` |
| `RequiresForwardData() bool` | — | 返回 false（黑盒优化器） |
| `OptimizeTool(ctx, tool, toolCallable) (map[string]any, error)` | `optimize_tool(tool, tool_callable)` | 核心入口：两阶段迭代优化 |
| `Backward(ctx, signals) error` | `_backward(signals)` | 返回 nil（对齐 Python 空实现） |
| `Step() map[schema.UpdateKey]any` | `_step()` | 返回空 map（对齐 Python 空实现） |

**OptimizeTool 内部流程**（对齐 Python `optimize_tool`）：

```
1. 保存原始描述 originalDesc = tool["description"]
2. for i := 0; i < maxTurns; i++ {
     if i > 0 { tool["description"] = 最新描述 }
     // Stage 1 - Example
     resultExample = CustomizedPipeline("example", tool, toolCallable, configEg)
     // Stage 2 - Description
     resultDesc = CustomizedPipeline("description", tool, toolCallable, configDesc)
   }
3. 最终审查：ToolDescriptionReviewer.Process(outputDesc, oriToolDesc, ["clean","cross_check","translate"])
4. 格式化：ToolDescriptionReviewer.Format(schema, processed)
5. 返回 finalDesc
```

### 3.2 beam_search.go — BeamSearch + TreeNode

```go
// TreeNode Beam Search 树节点。
//
// 对应 Python: TreeNode
type TreeNode struct {
    // Data 节点数据
    Data any
    // Score 节点得分
    Score float64
    // Results 节点结果
    Results any
    // History 历史路径
    History []any
    // Parent 父节点
    Parent *TreeNode
    // Children 子节点
    Children []*TreeNode
}

// BeamSearchMethod Beam Search 方法接口。
// APICallToExampleMethod 和 ToolDescriptionMethod 均实现此接口。
//
// 对应 Python: method 对象（隐式接口，需实现 step/get_examples）
type BeamSearchMethod interface {
    // Step 执行一步搜索。
    // 返回 (output, data, score)
    Step(ctx context.Context, tool map[string]any, examples any, prevOutputs []any, it int) (any, any, float64, error)
    // GetExamples 获取示例（可选）。
    GetExamples(ctx context.Context, tool map[string]any) any
}

// BeamSearch Beam Search 搜索算法。
//
// 对应 Python: BeamSearch
type BeamSearch struct {
    // method 搜索方法
    method BeamSearchMethod
    // beamWidth 束宽
    beamWidth int
    // expandNum 展开数量
    expandNum int
    // maxDepth 最大深度
    maxDepth int
    // numWorkers 并行 worker 数
    numWorkers int
    // verbose 是否输出详细日志
    verbose bool
    // earlyStop 是否早停
    earlyStop bool
    // checkValid 是否检查有效性
    checkValid bool
    // maxScore 最大分数
    maxScore float64
    // topK 返回 top-k 结果
    topK int
    // timeout 超时时间（秒）
    timeout float64
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `NewTreeNode(data, score, results, history)` | `TreeNode(data, score, results, history)` | 构造节点 |
| `GetDepth() int` | `get_depth()` | 获取节点深度 |
| `Search(ctx, tool) [][]any` | `search(tool)` | 执行搜索，返回 top-k 历史路径 |
| `expand(beamList, tool, examples, depth)` | `expand(...)` | 展开节点（用 goroutine 并行） |
| `prune(beamList)[]*TreeNode` | `prune(beamList)` | 按分数剪枝 |
| `checkEarlyStop(beamList, maxScore, k) bool` | `check_early_stop(...)` | 早停检查 |

### 3.3 eval.go — SimpleEval

```go
// SimpleEval 评估包装器，生成函数调用并评估准确性和输出有效性。
//
// 对应 Python: SimpleEval
type SimpleEval struct {
    // apiWrapper API 调用封装
    apiWrapper APIWrapperFunc
    // fnCallWeight 函数调用准确性权重
    fnCallWeight float64
    // outputEffectivenessWeight 输出有效性权重
    outputEffectivenessWeight float64
    // config 配置
    config map[string]any
    // model LLM 模型客户端（直接持有，用于 Function Calling）
    model *llm.Model
}

// APIWrapperFunc API 调用封装函数类型。
// 参数: (tool, toolInput) → 返回: (responseJSON, statusCode)
type APIWrapperFunc func(tool map[string]any, toolInput map[string]any) (string, int)

// EvalResult 评估结果。
//
// 对应 Python: SimpleEval.__call__ 返回值
type EvalResult struct {
    ScoreAvg           float64        `json:"score_avg"`
    ScoreStd           float64        `json:"score_std"`
    FnCallAccuracy     float64        `json:"fn_call_accuracy"`
    OutputEffectiveness float64       `json:"output_effectiveness"`
    Results            []EvalItemResult `json:"results"`
}

// EvalItemResult 单个示例评估结果。
type EvalItemResult struct {
    Instruction              string         `json:"instruction"`
    ExpectedFnCall           map[string]any `json:"expected_fn_call"`
    GeneratedFnCall          map[string]any `json:"generated_fn_call"`
    FnCallScore              float64        `json:"fn_call_score"`
    ExecutionResult          any            `json:"execution_result"`
    ExecutionError           any            `json:"execution_error"`
    OutputEffectivenessScore float64        `json:"output_effectiveness_score"`
    WeightedScore            float64        `json:"weighted_score"`
    Answer                   string         `json:"answer"`
    Errors                   []EvalError    `json:"errors"`
}

// EvalError 评估错误信息。
type EvalError struct {
    FunctionName string         `json:"function_name"`
    Arguments    map[string]any `json:"arguments"`
    ErrorMsg     string         `json:"error_msg"`
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Eval(ctx, tool, description, examples, runs) *EvalResult` | `__call__(tool, description, examples, runs)` | 评估工具描述 |
| `evaluateSingleExample(ctx, example) *EvalItemResult` | `_evaluate_single_example(example)` | 评估单个示例 |
| `generateFunctionCall(ctx, tool, description, instruction) (map[string]any, error)` | `_generate_function_call(tool, description, instruction)` | Function Calling 模式生成调用 |
| `evaluateFunctionCallAccuracy(generated, expected) float64` | `_evaluate_function_call_accuracy(generated, expected)` | 评估函数调用准确性 |
| `compareParameterValues(actual, expected) bool` | `_compare_parameter_values(actual, expected)` | 参数值比较 |
| `evaluateOutputEffectiveness(ctx, instruction, result, err, answer) float64` | `_evaluate_output_effectiveness(...)` | 评估输出有效性 |
| `simpleOutputComparison(result, answer) float64` | `_simple_output_comparison(result, answer)` | 简单输出比较（兜底） |

### 3.4 reviewer.go — ToolDescriptionReviewer

```go
// ToolDescriptionReviewer 工具描述后处理器。
// 三步流程：clean → cross_check → translate，最终 format 为目标 JSON 结构。
//
// 对应 Python: ToolDescriptionReviewer
type ToolDescriptionReviewer struct {
    // evalModelID 评估模型 ID
    evalModelID string
    // llmAPIKey LLM API 密钥
    llmAPIKey string
    // model LLM 模型客户端
    model *llm.Model
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Format(ctx, jsonSchema, description, example) (map[string]any, error)` | `format(json_schema, description, example)` | 格式化为目标 JSON |
| `CleanAndDeduplicate(ctx, data) (map[string]any, error)` | `clean_and_deduplicate(data)` | 清理去冗余 |
| `CrossCheck(ctx, data, oriTool) (map[string]any, error)` | `cross_check(data, ori_tool)` | 交叉检查 |
| `TranslateToChinese(ctx, data) (map[string]any, error)` | `translate_to_chinese(data)` | 翻译为中文 |
| `Process(ctx, data, oriTool, steps) (map[string]any, error)` | `process(data, ori_tool, steps)` | 按步骤顺序处理 |
| `isMostlyEnglish(text) bool` | `_is_mostly_english(text)` | 判断是否主要为英文 |

### 3.5 example_method.go — APICallToExampleMethod

```go
// APICallToExampleMethod API 调用示例生成方法。
// 生成函数调用→执行→评估→生成指令→自我反思→批量反思 循环。
//
// 对应 Python: APICallToExampleMethod
type APICallToExampleMethod struct {
    BaseMethod
    // runToolWithAPICall API 调用函数
    runToolWithAPICall APIWrapperFunc
    // evalFn 评估函数
    evalFn *SimpleEval
    // apiKeys API 密钥模板
    apiKeys any
    // nonOptParams 非优化参数
    nonOptParams []string
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Step(ctx, tool, examples, prevOutputs, it) (any, any, float64, error)` | `step(tool, examples, prev_outputs, it)` | 执行一步搜索 |
| `GenerateAPICallFromDescription(ctx, tool, exampleCalls, numGen, prevOutput) (map[string]any, error)` | `generate_api_call_from_description(...)` | 根据描述生成 API 调用 |
| `CritiqueAPICall(ctx, tool, fnCall, fnResponse) (map[string]any, error)` | `critique_api_call(tool, fn_call, fn_response)` | 批判 API 调用 |
| `GenerateInstructionFromAPICall(ctx, tool, fnCall, fnResponse, prevOutput) (string, error)` | `generate_instruction_from_api_call(...)` | 根据 API 调用生成指令 |
| `CritiqueInstruction(ctx, tool, instruction, fnCall, fnResponse, answer) (map[string]any, error)` | `critique_instruction(...)` | 批判指令质量 |
| `BatchReflectionWithScores(ctx, tool, fnCall, instructions, scores, analyses) (string, error)` | `batch_reflection_with_scores(...)` | 批量反思 |
| `GetOriginalDescription(tool map[string]any) string` | `get_original_description(tool)` | 获取原始描述 |

### 3.6 description_method.go — ToolDescriptionMethod

```go
// ToolDescriptionMethod 工具描述优化方法。
// 基于正负例批判当前描述，让 LLM 生成增强描述。
//
// 对应 Python: ToolDescriptionMethod
type ToolDescriptionMethod struct {
    BaseMethod
    // evalFn 评估函数
    evalFn *SimpleEval
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Step(ctx, tool, examples, prevOutputs, it) (any, any, float64, error)` | `step(tool, examples, prev_outputs, it)` | 执行一步搜索 |
| `Generate(ctx, tool, examples, prevOutputs, it) (map[string]any, error)` | `generate(tool, examples, prev_outputs, it)` | 生成描述 |
| `EvalLoop(ctx, tool, description, examples, runs) *EvalResult` | `eval_loop(tool, description, examples, runs)` | 评估循环 |
| `CritiqueDescriptions(ctx, tool, examples, prevOutputs) (map[string]any, error)` | `critique_descriptions(tool, examples, prev_outputs)` | 批判描述（正负例对比） |
| `CritiqueAllDescriptions(ctx, tool, examples, prevOutputs) (map[string]any, error)` | `critique_all_descriptions(...)` | 批判所有描述（含正负例） |
| `CritiqueNegativeExamples(ctx, tool, examples) (map[string]any, error)` | `critique_negative_examples(tool, examples)` | 批判负例 |
| `GenerateDescriptionFromDocumentation(ctx, tool, examples, prevOutputs) (map[string]any, error)` | `generate_description_from_documentation(...)` | 从文档生成增强描述 |
| `LoadExamples(examplesDir, functionName, maxNum) ([]any, error)` | `load_examples(examples_dir, function_name, max_num_examples)` | 加载正例 |
| `GetNegativeExamples(functionName string) ([]any, error)` | `get_negative_examples(function_name)` | 获取负例 |
| `GetOriginalDescription(tool map[string]any) string` | `get_original_description(tool)` | 获取原始描述 |
| `GetExamples(ctx, tool map[string]any) any` | `get_examples(tool)` | 获取示例 |

### 3.7 base_method.go — BaseMethod

```go
// BaseMethod Beam Search 方法基类。
//
// 对应 Python: BaseMethod
type BaseMethod struct {
    // config 配置字典
    config map[string]any
    // verbose 是否输出详细日志
    verbose bool
    // model LLM 模型客户端
    model *llm.Model
}
```

**方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `ProduceAnswerFromAPICall(ctx, instruction, docStr, apiResponse) (string, error)` | `produce_answer_from_api_call(instruction, doc_str, api_response)` | 根据 API 调用生成回答 |

### 3.8 pipeline.go — CustomizedPipeline

```go
// CustomizedPipeline 运行优化流水线。
// 根据阶段（example/description）选择对应方法，创建 BeamSearch 执行搜索。
//
// 对应 Python: customized_pipeline()
```

**函数签名**（对齐 Python）：

```go
func CustomizedPipeline(
    ctx context.Context,
    stage string,          // "example" 或 "description"
    tool map[string]any,
    config map[string]any,
    toolCallable APIWrapperFunc,
    model *llm.Model,
) ([][]any, error)
```

### 3.9 api_wrapper.go — SimpleAPIWrapper / SimpleAPIWrapperFromCallable / MakeSyncMCPCaller

```go
// SimpleAPIWrapper 简化版 API 调用封装（从文件加载函数）。
//
// 对应 Python: SimpleAPIWrapper
type SimpleAPIWrapper struct {
    // functions 已注册函数
    functions map[string]any
    // fnCallName 调用函数名
    fnCallName string
}

// SimpleAPIWrapperFromCallable 从可调用对象创建 API 封装。
//
// 对应 Python: SimpleAPIWrapperFromCallable
type SimpleAPIWrapperFromCallable struct {
    // functions 已注册函数
    functions map[string]any
    // fnCallName 调用函数名
    fnCallName string
}

// MakeSyncMCPCaller 创建 MCP 同步调用函数。
// 内部使用已有的 SseClient（或 StreamableHttpClient），
// 封装 Connect→CallTool→Disconnect 为可调用函数。
//
// 对应 Python: make_sync_mcp_caller()
func MakeSyncMCPCaller(url, name string) APIWrapperFunc
```

**SimpleAPIWrapperFromCallable 方法**（对齐 Python）：

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `Call(tool, toolInput) (string, int)` | `__call__(tool, tool_input)` | 执行函数调用 |

### 3.10 rits.go — InvokeWithVerify 薄包装

```go
// InvokeWithVerify 带验证的 LLM 文本调用。
// 复用 llm_resilience.InvokeTextWithRetry，将 Python 的 verify_fn 适配为
// isResultUsable（验证文本合法性）+ parseResult（解析验证后的结果）两步。
//
// 对应 Python: get_rits_response(model_id, prompt, api_key, verify_fn, max_attempts, ...)
func InvokeWithVerify(
    ctx context.Context,
    model *llm.Model,
    modelName string,
    prompt string,
    policy llm_resilience.LLMInvokePolicy,
    verifyFn func(string) (any, error),  // 验证+解析函数
) (any, error)
```

**适配逻辑**：
- `verifyFn(text) → (parsedResult, error)` 失败时 → 适配为 `isResultUsable` 返回 false，触发 llm_resilience 重试
- 成功时 → 返回 `parsedResult`
- 最多重试 `policy.MaxAttempts` 次

### 3.11 format.go — ParseJSON / FormatPromptLlama

```go
// ParseJSON 从 LLM 输出中提取 JSON。
// 支持带 header 查找和兜底 ast.literal_eval。
//
// 对应 Python: parse_json(output, header)
func ParseJSON(output string, header ...string) (map[string]any, error)

// FormatPromptLlama 格式化 Llama 风格提示词。
// 当前实现为直接拼接 system + user prompt（对齐 Python）。
//
// 对应 Python: format_prompt_llama(system_prompt, user_prompt)
func FormatPromptLlama(systemPrompt, userPrompt string) string
```

### 3.12 schema_extractor.go — ExtractSchema

```go
// ExtractSchema 从 JSON Schema 字典提取结构骨架，去除类型信息。
// 递归处理嵌套字典，保留列表原样，将原始值替换为空字符串。
//
// 对应 Python: extract_schema(schema_dict)
func ExtractSchema(schemaDict map[string]any) map[string]any
```

### 3.13 default_configs.go — 默认配置

```go
// DefaultConfigEg Example Stage 默认配置。
// 对应 Python: default_config_eg
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
// 对应 Python: default_config_desc
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
```

## 4. 数据流与调用链路

### 4.1 OptimizeTool 主流程

```
ToolOptimizerBase.OptimizeTool(ctx, tool, toolCallable)
│
├── 保存 originalDesc
│
├── for i := 0; i < maxTurns; i++ {
│   │
│   ├── if i > 0 { tool["description"] = 最新描述 }
│   │
│   ├── CustomizedPipeline(ctx, "example", tool, configEg, toolCallable, model)
│   │   ├── SimpleAPIWrapperFromCallable{toolCallable}
│   │   ├── SimpleEval{apiWrapper, config, model}
│   │   ├── APICallToExampleMethod{config, callAPIFn, evalFn}
│   │   ├── BeamSearch{method, beamWidth, expandNum, maxDepth, ...}
│   │   │   └── method.Step(...) → method.GetExamples(...)
│   │   │       ├── generateAPICallFromDescription → LLM 生成
│   │   │       ├── callAPIFn(tool, fnCall) → 执行
│   │   │       ├── critiqueAPICall → LLM 批判
│   │   │       ├── generateInstructionFromAPICall → LLM 生成
│   │   │       ├── produceAnswerFromAPICall → LLM 生成
│   │   │       ├── critiqueInstruction → LLM 批判
│   │   │       ├── batchReflectionWithScores → LLM 反思
│   │   │       └── evalFn.Eval → SimpleEval 评估
│   │   └── 保存结果到 {saveDir}/{toolName}.json
│   │
│   └── CustomizedPipeline(ctx, "description", tool, configDesc, toolCallable, model)
│       ├── SimpleEval{apiWrapper, config, model}
│       ├── ToolDescriptionMethod{config, evalFn}
│       ├── BeamSearch{method, beamWidth, expandNum, maxDepth, ...}
│       │   └── method.Step(...)
│       │       ├── getOriginalDescription / getNegativeExamples
│       │       ├── critiqueDescriptions → LLM 批判
│       │       ├── critiqueAllDescriptions → LLM 批判
│       │       ├── generateDescriptionFromDocumentation → LLM 生成
│       │       └── evalLoop → SimpleEval.Eval
│       └── 保存结果到 {saveDir}/{toolName}.json
│   }
│
├── ToolDescriptionReviewer.Process(outputDesc, oriToolDesc, ["clean","cross_check","translate"])
│   ├── CleanAndDeduplicate → InvokeWithVerify → llm_resilience
│   ├── CrossCheck → InvokeWithVerify → llm_resilience
│   └── TranslateToChinese → InvokeWithVerify → llm_resilience
│
├── ExtractSchema(originalDesc)
│
├── ToolDescriptionReviewer.Format(schema, processed)
│   └── InvokeWithVerify → llm_resilience
│
└── 返回 finalDesc
```

### 4.2 LLM 调用路径

```
┌─────────────────────────────────────────────────┐
│ 纯文本 LLM 调用（大部分场景）                      │
│                                                  │
│ Method/Critic/Reviewer                           │
│   └── InvokeWithVerify(ctx, model, name, prompt, policy, verifyFn) │
│       ├── verifyFn 适配为 isResultUsable          │
│       └── llm_resilience.InvokeTextWithRetry()   │
│           └── model.Invoke(ctx, messages)         │
└─────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────┐
│ Function Calling LLM 调用（SimpleEval 专用）      │
│                                                  │
│ SimpleEval.generateFunctionCall()                │
│   └── model.Invoke(ctx, messages, WithTools(...)) │
│       └── response.GetToolCalls()[0]             │
└─────────────────────────────────────────────────┘
```

### 4.3 MCP 调用路径

```
MakeSyncMCPCaller(url, name)
  └── 返回 APIWrapperFunc
      └── 内部: mcp.NewMcpClient(config) → client.Connect → client.CallTool → client.Disconnect
```

## 5. 依赖与回填

### 5.1 已就绪的依赖（无需回填）

| 依赖项 | 章节 | 状态 |
|--------|------|------|
| ToolCallOperator（暴露 tool_description tunable） | 9.70a | ✅ |
| BaseOptimizer + BaseOptimizerMixin | 9.72e | ✅ |
| TextualParameter | 9.72e | ✅ |
| LLMResilience.InvokeTextWithRetry | 9.72e | ✅ |
| McpClient / SseClient | 3.x | ✅ |
| llm.Model（含 Function Calling WithTools） | LLM 层 | ✅ |

### 5.2 依赖未完成的项（标注 ⤵️ 等待回填）

| 依赖项 | 说明 | 回填来源 |
|--------|------|----------|
| `*llm.Model` 实例获取 | ToolOptimizerBase 需要 `*llm.Model` 来调用 LLM。当前构造函数接收 `*llm.Model` 参数，但 Trainer 如何将 Model 注入到 Optimizer 取决于 Trainer 的实现 | 9.70 Trainer ⤵️ |
| `ToolOptimizerBase.Bind()` config 传递 | Bind 的 config 参数如何传递 model/llmAPIKey 等信息，取决于 Trainer 的编排方式 | 9.70 Trainer ⤵️ |
| `OptimizeTool` 的调用入口 | Python 中 `optimize_tool` 是独立于 Trainer 循环的入口，Go 版本中何时何地调用 `OptimizeTool` 取决于上层编排 | 9.70 Trainer ⤵️ |

**标注方式**：在代码中用 `// ⤵️ 9.70: 等待 Trainer 实现后回填` 注释。

### 5.3 不需要的回填

| 项 | 说明 |
|----|------|
| evaluator_pipeline（9.71 ⤵️） | SimpleEval 是 tool_call 专用评估器，与 9.71 的 MetricEvaluator 体系完全独立 |
| Signal（9.73） | ToolOptimizerBase 的 Backward 是空实现，不消费信号 |

## 6. 测试策略

### 6.1 可 mock 的测试（默认 go test）

| 文件 | 测试内容 | Mock 方式 |
|------|---------|-----------|
| format_test.go | ParseJSON 各种格式、FormatPromptLlama | 无需 mock |
| schema_extractor_test.go | ExtractSchema 递归、空输入、非 dict 输入 | 无需 mock |
| default_configs_test.go | 默认配置完整性 | 无需 mock |
| beam_search_test.go | TreeNode 构造/深度、Search/expand/prune | Mock BeamSearchMethod |
| base_method_test.go | ProduceAnswerFromAPICall | Mock *llm.Model |
| base_test.go | ToolOptimizerBase Domain/DefaultTargets/RequiresForwardData | 无需 mock |
| api_wrapper_test.go | SimpleAPIWrapperFromCallable.Call | 传入可调用函数 |
| reviewer_test.go | Process 流程、isMostlyEnglish | Mock *llm.Model |
| eval_test.go | evaluateFunctionCallAccuracy、compareParameterValues、simpleOutputComparison | 无需 mock |
| pipeline_test.go | CustomizedPipeline 流程 | Mock method/model |
| example_method_test.go | GenerateAPICallFromDescription 签名 | Mock *llm.Model |
| description_method_test.go | GenerateDescriptionFromDocumentation 签名 | Mock *llm.Model |

### 6.2 需要 LLM API 的测试（//go:build llm）

| 测试 | 说明 |
|------|------|
| SimpleEval.Eval 真实调用 | 需要 LLM Function Calling 能力 |
| ToolDescriptionReviewer.Format 真实调用 | 需要 LLM 文本生成能力 |
| OptimizeTool 端到端 | 需要完整 LLM + MCP 环境 |

### 6.3 覆盖率目标

整体覆盖率 ≥ 85%。LLM API 调用相关的测试用 `//go:build llm` 隔离，不纳入覆盖率基线。

## 7. 提示词一比一复刻清单

以下提示词/模板必须一比一复刻 Python 原文（中文/英文保持原文语言），不做自行翻译：

| 来源文件 | 提示词用途 |
|----------|-----------|
| `customized_reviewer.py` | `format()` 中的 4 个 prompt 变体（prompt_original / prompt_1 / prompt_2 / prompt 中文版） |
| `customized_reviewer.py` | `clean_and_deduplicate()` 中的 clean prompt |
| `customized_reviewer.py` | `cross_check()` 中的 cross_check prompt |
| `customized_reviewer.py` | `translate_to_chinese()` 中的 translate prompt |
| `base_method.py` | `produce_answer_from_api_call()` 中的 answer generation prompt |
| `toolcall_example_method.py` | `generate_api_call_from_description()` 中的 API call generation prompt |
| `toolcall_example_method.py` | `critique_api_call()` 中的 critique prompt |
| `toolcall_example_method.py` | `generate_instruction_from_api_call()` 中的 instruction generation prompt |
| `toolcall_example_method.py` | `critique_instruction()` 中的 instruction scoring prompt |
| `toolcall_example_method.py` | `batch_reflection_with_scores()` 中的 reflection prompt |
| `description_example_method.py` | `critique_descriptions()` 中的 description critique prompt（正负例对比版） |
| `description_example_method.py` | `critique_all_descriptions()` 中的 all descriptions critique prompt |
| `description_example_method.py` | `critique_negative_examples()` 中的 negative examples critique prompt |
| `description_example_method.py` | `generate_description_from_documentation()` 中的 description generation prompt |
| `customized_eval.py` | `_evaluate_output_effectiveness()` 中的 output effectiveness scoring prompt |
