# 9.70b + 9.71 Dataset + Evaluator 核心评估器层设计

## 概述

合并实现 9.70b（Dataset + Constant）和 9.71（BaseEvaluator 核心评估器层），为 Trainer 的 `evaluate` 步骤提供评估能力。

**范围：** 核心评估器层（BaseEvaluator/DefaultEvaluator/MetricEvaluator/metrics）+ 前置依赖（Case/EvaluatedCase/CaseLoader/TuneConstant）。evaluator_pipeline 仅建目录+doc.go占位，不实现。

**在自演化系统中的位置：**

```
自演化系统 9.x 分层：

┌──────────────────────────────────────────────────────────────┐
│  9.79 Experience       在线经验生命周期                        │
│  9.78 Checkpoint       断点续训状态持久化                      │
│  9.77 Trajectory       轨迹构建/提取/聚合/存储                 │
│  9.74-76 RL 子系统     离线/在线强化学习                       │
│  9.73 SignalDetector   从评估结果/对话中检测演化信号            │
│  9.72a-d Optimizer     四类优化器                              │
│  9.72e BaseOptimizer   优化器基类 + LLM 调用重试               │
├──────────────────────────────────────────────────────────────┤
│  ★ 9.71 Evaluator      评估器层 ← 本次实现                    │
│    ├── BaseEvaluator   抽象评估接口                            │
│    ├── DefaultEvaluator LLM-as-Judge 评估器                   │
│    ├── MetricEvaluator  多指标聚合评估器                       │
│    ├── metrics/         ExactMatch + LLMAsJudge 指标          │
│    └── evaluator_pipeline  ⤵️ 仅占位，后续回填                 │
├──────────────────────────────────────────────────────────────┤
│  ★ 9.70b Dataset+Constant ← 本次实现（前置依赖）              │
│    ├── Case/EvaluatedCase 数据类型                             │
│    ├── CaseLoader 样本容器                                     │
│    └── TuneConstant 超参常量                                   │
├──────────────────────────────────────────────────────────────┤
│  9.70c Updater          更新器协议接口                         │
│  9.70a Operator         抽象操作接口                           │
│  9.70  Trainer          离线演化编排（已完成骨架）              │
└──────────────────────────────────────────────────────────────┘
```

**核心作用：** Evaluator 是 Trainer 的 `evaluate → update → writeback` 循环中"评估"阶段的执行者，将 `(Case, prediction)` → `EvaluatedCase`（打分 + 原因），是 Trainer 和 Updater 之间的桥梁。

---

## 目录结构

```
internal/evolving/
├── trainer/                          # 9.70 ✅ 已完成
│   ├── doc.go
│   ├── trainer.go
│   ├── progress.go
│   └── trainer_test.go
├── dataset/                          # 9.70b 新增
│   ├── doc.go
│   ├── case.go                       # Case + EvaluatedCase
│   ├── case_loader.go                # CaseLoader
│   └── case_test.go
├── evaluator/                        # 9.71 新增
│   ├── doc.go
│   ├── evaluator.go                  # BaseEvaluator + DefaultEvaluator + MetricEvaluator + aggScore
│   ├── templates.go                  # LLMMetricTemplate + LLMMetricRetryTemplate
│   ├── evaluator_test.go
│   ├── metrics/
│   │   ├── doc.go
│   │   ├── base.go                   # Metric 接口 + MetricResult 类型
│   │   ├── exact_match.go            # ExactMatchMetric
│   │   ├── exact_match_test.go
│   │   ├── llm_as_judge.go           # LLMAsJudgeMetric
│   │   └── llm_as_judge_test.go
│   └── evaluator_pipeline/           # ⤵️ 仅建目录+doc.go
│       └── doc.go
├── constant.go                       # 9.70b TuneConstant 超参常量
└── constant_test.go
```

---

## 9.70b 核心类型

### Case 结构体

```go
// Case 单个训练/评估样本
type Case struct {
    // Inputs 输入数据（如查询或对话内容）
    Inputs map[string]any `json:"inputs"`
    // Label 期望答案或期望输出
    Label map[string]any `json:"label"`
    // Tools 当前样本可用的工具列表（可选）
    Tools []tool.ToolInfo `json:"tools,omitempty"`
    // CaseID 唯一标识，未指定时自动生成
    CaseID string `json:"case_id"`
}
```

- `NewCase(inputs, label map[string]any, opts ...CaseOption) *Case` — 构造函数，默认生成 uuid CaseID
- CaseOption: `WithTools(tools []tool.ToolInfo)`, `WithCaseID(id string)`
- 便捷属性方法：`Inputs()`, `Label()`, `Tools()`, `CaseID()` （与 Python `EvaluatedCase.inputs` 等属性对齐）

### EvaluatedCase 结构体

```go
// EvaluatedCase 评估后的样本，包含模型输出和评分
type EvaluatedCase struct {
    // Case 原始样本
    Case Case `json:"case"`
    // Answer 模型输出/预测结果
    Answer map[string]any `json:"answer,omitempty"`
    // Score 综合评分，范围 [0, 1]
    Score float64 `json:"score"`
    // Reason 评分原因或错误分析
    Reason string `json:"reason"`
    // PerMetric 各指标独立评分（MetricEvaluator 使用）
    PerMetric map[string]float64 `json:"per_metric,omitempty"`
}
```

- `NewEvaluatedCase(case_ Case, answer map[string]any) *EvaluatedCase` — 构造函数，Score 默认 0.0
- Score 钳位逻辑：在 `SetScore(score float64)` 方法中 `max(0.0, min(1.0, score))`
- 便捷属性方法：`Inputs()`, `Label()`, `Tools()`, `CaseID()` — 代理到 Case 字段

### CaseLoader 结构体

```go
// CaseLoader Case 容器，支持拆分和打乱
type CaseLoader struct {
    cases []Case
}
```

- `NewCaseLoader(cases []Case) *CaseLoader`
- `Cases() []Case`
- `Len() int`
- `Split(ratio float64) (train, val *CaseLoader)` — 按 ratio 比例拆分
- `ShuffleCases()` — 随机打乱内部样本顺序

### TuneConstant 常量

```go
// TuneConstant 自演化训练超参默认值和合法范围
const (
    DefaultExampleNum              = 1
    DefaultIterationNum            = 3
    DefaultMaxSampledExampleNum    = 10
    DefaultParallelNum             = 1
    DefaultMaxNumSampleErrorCases  = 10
    DefaultEarlyStopScore          = 1.0
    MinIterationNum                = 1
    MaxIterationNum                = 20
    MinParallelNum                 = 1
    MaxParallelNum                 = 20
    MinExampleNum                  = 0
    MaxExampleNum                  = 20
)
```

Python 用 dataclass 实例字段，Go 用 const 常量（这些值不会变）。

---

## 9.71 Evaluator 核心设计

### MetricResult + MetricOption

```go
// MetricResult 指标计算结果，统一用 map 表示
// 单指标：{"exact_match": 1.0}  多指标：{"precision": 0.8, "recall": 0.6}
type MetricResult = map[string]float64

// MetricOption 指标计算的可选参数
type MetricOption func(*metricContext)

func WithQuestion(q any) MetricOption
func WithCase(c Case) MetricOption
```

### Metric 接口

```go
// Metric 评估指标抽象接口
type Metric interface {
    Name() string
    HigherIsBetter() bool
    Compute(prediction, label any, opts ...MetricOption) (MetricResult, error)
    ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error)
}
```

`ComputeBatch` 默认实现：遍历 zip(predictions, labels) 逐个调用 Compute。

### ExactMatchMetric

```go
type ExactMatchMetric struct {
    normalize bool
}

func NewExactMatchMetric(opts ...ExactMatchOption) *ExactMatchMetric
func (m *ExactMatchMetric) Name() string                        // → "exact_match"
func (m *ExactMatchMetric) HigherIsBetter() bool                // → true
func (m *ExactMatchMetric) Compute(prediction, label any, opts ...MetricOption) (MetricResult, error)
func (m *ExactMatchMetric) ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error)
```

- `normalize=true`（默认）：先 `_normalize`（小写 + strip + 合并连续空格）再比较
- 匹配 → `{"exact_match": 1.0}`，不匹配 → `{"exact_match": 0.0}`
- 一比一复刻 Python `ExactMatchMetric`

### LLMAsJudgeMetric

```go
type LLMAsJudgeMetric struct {
    model    *llm.Model
    template *prompt.PromptTemplate
    parser   *output_parsers.JsonOutputParser
}

func NewLLMAsJudgeMetric(modelClientConfig llm.ModelClientConfig, modelRequestConfig llm.ModelRequestConfig, userMetrics string) *LLMAsJudgeMetric
func (m *LLMAsJudgeMetric) Name() string                        // → "llm_as_judge"
func (m *LLMAsJudgeMetric) HigherIsBetter() bool                // → true
func (m *LLMAsJudgeMetric) Compute(prediction, label any, opts ...MetricOption) (MetricResult, error)
```

- 传入 `ModelClientConfig + ModelRequestConfig`，内部创建 `llm.Model`（一比一复刻 Python）
- Format 模板 → `model.Invoke` → `parser.Parse` → 解析 `result` 字段 → `1.0` 或 `0.0`
- 调用失败返回 `{"llm_as_judge": 0.0}` + error

### BaseEvaluator 接口

```go
// BaseEvaluator 抽象评估器接口
type BaseEvaluator interface {
    Evaluate(ctx context.Context, case_ Case, predict map[string]any) (*EvaluatedCase, error)
    BatchEvaluate(ctx context.Context, cases []Case, predicts []map[string]any, numParallel int) ([]*EvaluatedCase, error)
}
```

`BatchEvaluate` 用 `errgroup.Group` + `SetLimit(numParallel)` 实现并行。

### DefaultEvaluator

```go
type DefaultEvaluator struct {
    model         *llm.Model
    metricTemplate *prompt.PromptTemplate
    parser        *output_parsers.JsonOutputParser
}

func NewDefaultEvaluator(modelClientConfig llm.ModelClientConfig, modelRequestConfig llm.ModelRequestConfig, metric string) *DefaultEvaluator
func (d *DefaultEvaluator) Evaluate(ctx context.Context, case_ Case, predict map[string]any) (*EvaluatedCase, error)
func (d *DefaultEvaluator) BatchEvaluate(ctx context.Context, cases []Case, predicts []map[string]any, numParallel int) ([]*EvaluatedCase, error)
```

逻辑一比一复刻 Python `DefaultEvaluator`：
1. Format `LLMMetricTemplate` → `model.Invoke` → `parser.Parse`
2. 解析 `result` + `reason` 字段
3. 解析失败 → Format `LLMMetricRetryTemplate` 重试一次
4. 仍然失败 → 返回 `reason="Failed to evaluate case due to parsing error"`
5. LLM 调用异常 → 返回 `reason="Failed to evaluate case due to model error"`

### MetricEvaluator

```go
type MetricEvaluator struct {
    metrics   []Metric
    aggregate string  // "mean" 或 "first"
}

func NewMetricEvaluator(metrics []Metric, aggregate string) *MetricEvaluator
func (m *MetricEvaluator) Evaluate(ctx context.Context, case_ Case, predict map[string]any) (*EvaluatedCase, error)
func (m *MetricEvaluator) BatchEvaluate(ctx context.Context, cases []Case, predicts []map[string]any, numParallel int) ([]*EvaluatedCase, error)
```

逻辑一比一复刻 Python `MetricEvaluator`：
1. 遍历所有 metric → `Compute` → 收集 per_metric
2. `_aggScore` 聚合（"mean" = 平均值，"first" = 第一个分数）
3. 非法 aggregate 值默认 "mean"
4. `_safeConvert` 将非 float 值安全转为 float64

### aggScore 函数

```go
// aggScore 聚合分数
func aggScore(results []float64, aggregate string) float64
```

- `"mean"` → `sum / len`
- `"first"` → `results[0]`
- 其他 → 默认 mean
- 空切片 → `0.0`

### Templates

```go
var (
    LLMMetricTemplate     = prompt.NewPromptTemplate("llm_metric", LLM_METRIC_TEMPLATE_CONTENT)
    LLMMetricRetryTemplate = prompt.NewPromptTemplate("llm_metric_retry", LLM_METRIC_RETRY_TEMPLATE_CONTENT)
)
```

模板内容一比一复刻 Python `LLM_METRIC_TEMPLATE` 和 `LLMMETRICRETRY_TEMPLATE`。占位符 `{{question}}`/`{{expected_answer}}`/`{{model_answer}}`/`{{user_metrics}}` 与 Python 一致，复用项目已有的 `prompt.PromptTemplate`。

---

## 依赖复用

| Python 依赖 | Go 复用 |
|-------------|--------|
| `PromptTemplate` | `internal/agentcore/foundation/prompt.PromptTemplate` |
| `Model(client_config, request_config)` | `internal/agentcore/foundation/llm` 中的 Model |
| `TuneUtils.parse_json_from_llm_response()` | `internal/agentcore/foundation/llm/output_parsers.JsonOutputParser.Parse()` |
| `ToolInfo` | `internal/agentcore/foundation/tool.ToolInfo` |
| `asyncio.run()` | 同步调用（Go 天然同步，无需 asyncio） |
| `ThreadPoolExecutor` | `errgroup.Group` + `SetLimit()` |
| `tqdm` | 不引入进度条（batch 评估场景可选） |
| `StatusCode.TOOLCHAIN_EVALUATOR_EXECUTION_ERROR` | `internal/common/exception.StatusToolchainEvaluatorExecutionError` |

---

## Trainer 回填

9.71 完成后，替换 `internal/evolving/trainer/trainer.go` 中的 any 占位：

```go
// 替换前
evaluator any  // 依赖 9.71 BaseEvaluator，暂用 any 占位

// 替换后
evaluator BaseEvaluator  // 依赖 9.71 evaluator.BaseEvaluator
```

同时调整 `WithEvaluator` 选项函数参数类型和 import。

---

## evaluator_pipeline 占位

`internal/evolving/evaluator/evaluator_pipeline/doc.go`：

```go
// Package evaluator_pipeline 提供容器化技能演化流水线。
//
// ⤵️ 等待后续章节回填：SkillEvolutionPipeline、PipelineConfig、
// IterationResult、PipelineResult、ContainerManager、SkillEvolutionManager、
// Verifier、JiuWenSwarmAdapter 等组件。
//
// 对应 Python 代码：openjiuwen/agent_evolving/evaluator/evaluator_pipeline/
package evaluator_pipeline
```

---

## 测试策略

| 文件 | 测试内容 |
|------|---------|
| `dataset/case_test.go` | Case 构造/默认 CaseID 生成/EvaluatedCase Score 钳位 [0,1]/PerMetric 可选/便捷属性代理 |
| `dataset/case_loader_test.go` | CaseLoader 构造/Len/Split 比例/ShuffleCases 打乱 |
| `constant_test.go` | TuneConstant 常量值验证（与 Python 对齐） |
| `evaluator/metrics/exact_match_test.go` | 精确匹配/归一化匹配/不匹配/大小写差异/空格差异/normalize=false |
| `evaluator/metrics/llm_as_judge_test.go` | 用 httptest mock LLM 服务，测试通过/失败/解析失败/LLM调用异常 |
| `evaluator/evaluator_test.go` | DefaultEvaluator（mock LLM via httptest）/重试逻辑/MetricEvaluator 多指标聚合/aggScore mean+first/BatchEvaluate 并行/长度不匹配报错 |

**mock 策略：**
- LLM 调用通过 `httptest.NewServer` mock 模型 API（符合项目规则 3.3）
- 不使用 build tag 逃避
- 真实 LLM API 调用测试用 `//go:build llm` 标签隔离

---

## 实现顺序

1. `internal/evolving/constant.go` — TuneConstant 常量
2. `internal/evolving/dataset/case.go` — Case + EvaluatedCase
3. `internal/evolving/dataset/case_loader.go` — CaseLoader
4. `internal/evolving/evaluator/metrics/base.go` — Metric 接口 + MetricResult + MetricOption
5. `internal/evolving/evaluator/metrics/exact_match.go` — ExactMatchMetric
6. `internal/evolving/evaluator/metrics/llm_as_judge.go` — LLMAsJudgeMetric
7. `internal/evolving/evaluator/templates.go` — 评估模板
8. `internal/evolving/evaluator/evaluator.go` — BaseEvaluator + DefaultEvaluator + MetricEvaluator + aggScore
9. `internal/evolving/evaluator/evaluator_pipeline/doc.go` — 占位
10. 回填 `internal/evolving/trainer/trainer.go` — evaluator 字段类型替换
11. 所有 doc.go 文件
12. 所有测试文件
