# 9.70b + 9.71 Dataset + Evaluator 核心评估器层实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.70b（Case/EvaluatedCase/CaseLoader/TuneConstant）和 9.71（BaseEvaluator/DefaultEvaluator/MetricEvaluator/ExactMatch/LLMAsJudge/Templates），为 Trainer 的 evaluate 步骤提供评估能力，并回填 Trainer 中的 any 占位。

**Architecture:** 核心评估器层分为 dataset（数据类型）和 evaluator（评估逻辑+指标）两个平级包。Metric 接口返回统一 `map[string]float64`，MetricEvaluator 聚合多指标，DefaultEvaluator 通过 LLM-as-Judge 评估语义一致性。BatchEvaluate 用 errgroup 并行。模板复用项目已有的 `prompt.PromptTemplate`，JSON 解析复用 `output_parsers.JsonOutputParser`。

**Tech Stack:** Go 1.26, errgroup, github.com/google/uuid, 项目已有 prompt/output_parsers/llm 包

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| Create | `internal/evolving/constant.go` | TuneConstant 超参常量 |
| Create | `internal/evolving/constant_test.go` | 常量值验证 |
| Create | `internal/evolving/dataset/doc.go` | dataset 包文档 |
| Create | `internal/evolving/dataset/case.go` | Case + EvaluatedCase 结构体 |
| Create | `internal/evolving/dataset/case_test.go` | Case/EvaluatedCase 测试 |
| Create | `internal/evolving/dataset/case_loader.go` | CaseLoader 容器 |
| Create | `internal/evolving/dataset/case_loader_test.go` | CaseLoader 测试 |
| Create | `internal/evolving/evaluator/doc.go` | evaluator 包文档 |
| Create | `internal/evolving/evaluator/metrics/doc.go` | metrics 包文档 |
| Create | `internal/evolving/evaluator/metrics/base.go` | Metric 接口 + MetricResult + MetricOption |
| Create | `internal/evolving/evaluator/metrics/exact_match.go` | ExactMatchMetric |
| Create | `internal/evolving/evaluator/metrics/exact_match_test.go` | ExactMatch 测试 |
| Create | `internal/evolving/evaluator/metrics/llm_as_judge.go` | LLMAsJudgeMetric |
| Create | `internal/evolving/evaluator/metrics/llm_as_judge_test.go` | LLMAsJudge 测试 |
| Create | `internal/evolving/evaluator/templates.go` | 评估提示词模板 |
| Create | `internal/evolving/evaluator/evaluator.go` | BaseEvaluator + DefaultEvaluator + MetricEvaluator + aggScore |
| Create | `internal/evolving/evaluator/evaluator_test.go` | 评估器测试 |
| Create | `internal/evolving/evaluator/evaluator_pipeline/doc.go` | pipeline 占位 |
| Modify | `internal/evolving/trainer/trainer.go` | 回填 evaluator 字段类型 |
| Modify | `internal/evolving/trainer/trainer_test.go` | 更新 WithEvaluator 测试 |

---

### Task 1: TuneConstant 超参常量

**Files:**
- Create: `internal/evolving/constant.go`
- Create: `internal/evolving/constant_test.go`

- [ ] **Step 1: 编写 constant_test.go 失败测试**

```go
package evolving

import "testing"

// ──────────────────────────── 常量测试 ────────────────────────────

// TestTuneConstant_默认值 验证 TuneConstant 常量与 Python TuneConstant 对齐
func TestTuneConstant_默认值(t *testing.T) {
	// 默认值
	if DefaultExampleNum != 1 {
		t.Errorf("DefaultExampleNum 期望 1, 实际 %d", DefaultExampleNum)
	}
	if DefaultIterationNum != 3 {
		t.Errorf("DefaultIterationNum 期望 3, 实际 %d", DefaultIterationNum)
	}
	if DefaultMaxSampledExampleNum != 10 {
		t.Errorf("DefaultMaxSampledExampleNum 期望 10, 实际 %d", DefaultMaxSampledExampleNum)
	}
	if DefaultParallelNum != 1 {
		t.Errorf("DefaultParallelNum 期望 1, 实际 %d", DefaultParallelNum)
	}
	if DefaultMaxNumSampleErrorCases != 10 {
		t.Errorf("DefaultMaxNumSampleErrorCases 期望 10, 实际 %d", DefaultMaxNumSampleErrorCases)
	}
	if DefaultEarlyStopScore != 1.0 {
		t.Errorf("DefaultEarlyStopScore 期望 1.0, 实际 %f", DefaultEarlyStopScore)
	}

	// 合法范围
	if MinIterationNum != 1 {
		t.Errorf("MinIterationNum 期望 1, 实际 %d", MinIterationNum)
	}
	if MaxIterationNum != 20 {
		t.Errorf("MaxIterationNum 期望 20, 实际 %d", MaxIterationNum)
	}
	if MinParallelNum != 1 {
		t.Errorf("MinParallelNum 期望 1, 实际 %d", MinParallelNum)
	}
	if MaxParallelNum != 20 {
		t.Errorf("MaxParallelNum 期望 20, 实际 %d", MaxParallelNum)
	}
	if MinExampleNum != 0 {
		t.Errorf("MinExampleNum 期望 0, 实际 %d", MinExampleNum)
	}
	if MaxExampleNum != 20 {
		t.Errorf("MaxExampleNum 期望 20, 实际 %d", MaxExampleNum)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/ -run TestTuneConstant -v 2>&1 | head -20`
Expected: 编译失败，未定义常量

- [ ] **Step 3: 编写 constant.go 实现**

```go
package evolving

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultExampleNum 每次迭代示例数
	// 对应 Python: TuneConstant.default_example_num = 1
	DefaultExampleNum = 1
	// DefaultIterationNum 默认训练迭代次数
	// 对应 Python: TuneConstant.default_iteration_num = 3
	DefaultIterationNum = 3
	// DefaultMaxSampledExampleNum 最大采样示例数
	// 对应 Python: TuneConstant.default_max_sampled_example_num = 10
	DefaultMaxSampledExampleNum = 10
	// DefaultParallelNum 默认并行数
	// 对应 Python: TuneConstant.default_parallel_num = 1
	DefaultParallelNum = 1
	// DefaultMaxNumSampleErrorCases 最大采样错误用例数
	// 对应 Python: TuneConstant.default_max_num_sample_error_cases = 10
	DefaultMaxNumSampleErrorCases = 10
	// DefaultEarlyStopScore 早停分数阈值
	// 对应 Python: TuneConstant.default_early_stop_score = 1.0
	DefaultEarlyStopScore = 1.0

	// MinIterationNum 最小迭代次数
	// 对应 Python: TuneConstant.min_iteration_num = 1
	MinIterationNum = 1
	// MaxIterationNum 最大迭代次数
	// 对应 Python: TuneConstant.max_iteration_num = 20
	MaxIterationNum = 20
	// MinParallelNum 最小并行数
	// 对应 Python: TuneConstant.min_parallel_num = 1
	MinParallelNum = 1
	// MaxParallelNum 最大并行数
	// 对应 Python: TuneConstant.max_parallel_num = 20
	MaxParallelNum = 20
	// MinExampleNum 最小示例数
	// 对应 Python: TuneConstant.min_example_num = 0
	MinExampleNum = 0
	// MaxExampleNum 最大示例数
	// 对应 Python: TuneConstant.max_example_num = 20
	MaxExampleNum = 20
)
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/ -run TestTuneConstant -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/constant.go internal/evolving/constant_test.go
git commit -m "feat(evolving): 添加 TuneConstant 超参常量 (9.70b)"
```

---

### Task 2: Case + EvaluatedCase 数据类型

**Files:**
- Create: `internal/evolving/dataset/case.go`
- Create: `internal/evolving/dataset/case_test.go`

- [ ] **Step 1: 编写 case_test.go 失败测试**

```go
package dataset

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewCase 测试 Case 构造函数
func TestNewCase(t *testing.T) {
	inputs := map[string]any{"query": "hello"}
	label := map[string]any{"answer": "world"}
	c := NewCase(inputs, label)
	if c.CaseID == "" {
		t.Error("期望 CaseID 自动生成，实际为空")
	}
	if len(c.Inputs) != 1 || c.Inputs["query"] != "hello" {
		t.Errorf("期望 Inputs 包含 query=hello, 实际=%v", c.Inputs)
	}
	if len(c.Label) != 1 || c.Label["answer"] != "world" {
		t.Errorf("期望 Label 包含 answer=world, 实际=%v", c.Label)
	}
	if len(c.Tools) != 0 {
		t.Errorf("期望 Tools 为空, 实际=%v", c.Tools)
	}
}

// TestNewCase_使用选项 测试 Case 构造函数带选项
func TestNewCase_使用选项(t *testing.T) {
	inputs := map[string]any{"q": "1"}
	label := map[string]any{"a": "2"}
	tools := []schema.ToolInfo{{Name: "tool1"}}
	c := NewCase(inputs, label, WithCaseTools(tools), WithCaseID("my-id"))
	if c.CaseID != "my-id" {
		t.Errorf("期望 CaseID=my-id, 实际=%s", c.CaseID)
	}
	if len(c.Tools) != 1 {
		t.Errorf("期望 Tools 长度 1, 实际=%d", len(c.Tools))
	}
}

// TestNewEvaluatedCase 测试 EvaluatedCase 构造
func TestNewEvaluatedCase(t *testing.T) {
	c := NewCase(map[string]any{"q": "1"}, map[string]any{"a": "2"})
	answer := map[string]any{"output": "result"}
	ec := NewEvaluatedCase(*c, answer)
	if ec.Score != 0.0 {
		t.Errorf("期望默认 Score=0.0, 实际=%f", ec.Score)
	}
	if ec.Reason != "" {
		t.Errorf("期望默认 Reason 为空, 实际=%s", ec.Reason)
	}
	if ec.PerMetric != nil {
		t.Errorf("期望默认 PerMetric 为 nil, 实际=%v", ec.PerMetric)
	}
	if len(ec.Answer) != 1 || ec.Answer["output"] != "result" {
		t.Errorf("期望 Answer 包含 output=result, 实际=%v", ec.Answer)
	}
}

// TestEvaluatedCase_SetScore_钳位 测试 Score 钳位到 [0, 1]
func TestEvaluatedCase_SetScore_钳位(t *testing.T) {
	c := NewCase(map[string]any{"q": "1"}, map[string]any{"a": "2"})
	ec := NewEvaluatedCase(*c, nil)

	ec.SetScore(1.5)
	if ec.Score != 1.0 {
		t.Errorf("期望 Score 钳位到 1.0, 实际=%f", ec.Score)
	}

	ec.SetScore(-0.5)
	if ec.Score != 0.0 {
		t.Errorf("期望 Score 钳位到 0.0, 实际=%f", ec.Score)
	}

	ec.SetScore(0.7)
	if ec.Score != 0.7 {
		t.Errorf("期望 Score=0.7, 实际=%f", ec.Score)
	}
}

// TestEvaluatedCase_便捷属性 测试 EvaluatedCase 代理属性
func TestEvaluatedCase_便捷属性(t *testing.T) {
	tools := []schema.ToolInfo{{Name: "tool1"}}
	c := NewCase(map[string]any{"q": "1"}, map[string]any{"a": "2"}, WithCaseTools(tools), WithCaseID("test-id"))
	ec := NewEvaluatedCase(*c, nil)

	if ec.GetInputs()["q"] != "1" {
		t.Errorf("期望 GetInputs 返回原始 inputs")
	}
	if ec.GetLabel()["a"] != "2" {
		t.Errorf("期望 GetLabel 返回原始 label")
	}
	if ec.GetCaseID() != "test-id" {
		t.Errorf("期望 GetCaseID=test-id, 实际=%s", ec.GetCaseID())
	}
	if len(ec.GetTools()) != 1 {
		t.Errorf("期望 GetTools 长度 1, 实际=%d", len(ec.GetTools()))
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/dataset/ -run "TestNewCase|TestNewEvaluatedCase|TestEvaluatedCase" -v 2>&1 | head -20`
Expected: 编译失败，未定义类型

- [ ] **Step 3: 编写 case.go 实现**

```go
package dataset

import (
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Case 单个训练/评估样本。
//
// 对应 Python: openjiuwen/agent_evolving/dataset/case.py Case
type Case struct {
	// Inputs 输入数据（如查询或对话内容）
	Inputs map[string]any `json:"inputs"`
	// Label 期望答案或期望输出
	Label map[string]any `json:"label"`
	// Tools 当前样本可用的工具列表（可选）
	Tools []schema.ToolInfo `json:"tools,omitempty"`
	// CaseID 唯一标识，未指定时自动生成
	CaseID string `json:"case_id"`
}

// EvaluatedCase 评估后的样本，包含模型输出和评分。
//
// Score 钳位到 [0, 1] 范围。
// 对应 Python: openjiuwen/agent_evolving/dataset/case.py EvaluatedCase
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

// CaseOption Case 构造选项函数。
type CaseOption func(*Case)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCase 创建 Case 实例，默认自动生成 CaseID。
//
// 对应 Python: Case(inputs=..., label=..., tools=..., case_id=uuid...)
func NewCase(inputs, label map[string]any, opts ...CaseOption) *Case {
	c := &Case{
		Inputs: inputs,
		Label:  label,
		CaseID: uuid.New().String(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// NewEvaluatedCase 创建 EvaluatedCase 实例，Score 默认 0.0。
//
// 对应 Python: EvaluatedCase(case=..., answer=...)
func NewEvaluatedCase(case_ Case, answer map[string]any) *EvaluatedCase {
	return &EvaluatedCase{
		Case:   case_,
		Answer: answer,
		Score:  0.0,
	}
}

// SetScore 设置评分，自动钳位到 [0, 1]。
//
// 对应 Python: EvaluatedCase 的 field_validator("score") clamp_score
func (ec *EvaluatedCase) SetScore(score float64) {
	if score > 1.0 {
		score = 1.0
	}
	if score < 0.0 {
		score = 0.0
	}
	ec.Score = score
}

// GetInputs 返回原始样本的输入数据。
// 对应 Python: EvaluatedCase.inputs 属性
func (ec *EvaluatedCase) GetInputs() map[string]any {
	return ec.Case.Inputs
}

// GetLabel 返回原始样本的期望答案。
// 对应 Python: EvaluatedCase.label 属性
func (ec *EvaluatedCase) GetLabel() map[string]any {
	return ec.Case.Label
}

// GetTools 返回原始样本的工具列表。
// 对应 Python: EvaluatedCase.tools 属性
func (ec *EvaluatedCase) GetTools() []schema.ToolInfo {
	return ec.Case.Tools
}

// GetCaseID 返回原始样本的唯一标识。
// 对应 Python: EvaluatedCase.case_id 属性
func (ec *EvaluatedCase) GetCaseID() string {
	return ec.Case.CaseID
}

// WithCaseTools 设置 Case 的工具列表。
func WithCaseTools(tools []schema.ToolInfo) CaseOption {
	return func(c *Case) { c.Tools = tools }
}

// WithCaseID 设置 Case 的唯一标识。
func WithCaseID(id string) CaseOption {
	return func(c *Case) { c.CaseID = id }
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/dataset/ -run "TestNewCase|TestNewEvaluatedCase|TestEvaluatedCase" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/dataset/case.go internal/evolving/dataset/case_test.go
git commit -m "feat(evolving/dataset): 添加 Case 和 EvaluatedCase 数据类型 (9.70b)"
```

---

### Task 3: CaseLoader 样本容器

**Files:**
- Create: `internal/evolving/dataset/case_loader.go`
- Create: `internal/evolving/dataset/case_loader_test.go`

- [ ] **Step 1: 编写 case_loader_test.go 失败测试**

```go
package dataset

import "testing"

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewCaseLoader 测试 CaseLoader 构造
func TestNewCaseLoader(t *testing.T) {
	cases := []Case{
		*NewCase(map[string]any{"q": "1"}, map[string]any{"a": "1"}),
		*NewCase(map[string]any{"q": "2"}, map[string]any{"a": "2"}),
	}
	loader := NewCaseLoader(cases)
	if loader.Len() != 2 {
		t.Errorf("期望 Len=2, 实际=%d", loader.Len())
	}
	if len(loader.Cases()) != 2 {
		t.Errorf("期望 Cases 长度 2, 实际=%d", len(loader.Cases()))
	}
}

// TestCaseLoader_Split 测试拆分
func TestCaseLoader_Split(t *testing.T) {
	cases := make([]Case, 10)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i})
	}
	loader := NewCaseLoader(cases)

	train, val := loader.Split(0.8)
	// 10 个样本 80% 拆分 → train 8, val 2
	if train.Len() != 8 {
		t.Errorf("期望 train.Len=8, 实际=%d", train.Len())
	}
	if val.Len() != 2 {
		t.Errorf("期望 val.Len=2, 实际=%d", val.Len())
	}
}

// TestCaseLoader_Split_空样本 测试空 CaseLoader 拆分
func TestCaseLoader_Split_空样本(t *testing.T) {
	loader := NewCaseLoader(nil)
	train, val := loader.Split(0.8)
	if train.Len() != 0 {
		t.Errorf("期望 train.Len=0, 实际=%d", train.Len())
	}
	if val.Len() != 0 {
		t.Errorf("期望 val.Len=0, 实际=%d", val.Len())
	}
}

// TestCaseLoader_ShuffleCases 测试打乱
func TestCaseLoader_ShuffleCases(t *testing.T) {
	cases := make([]Case, 100)
	for i := range cases {
		cases[i] = *NewCase(map[string]any{"q": i}, map[string]any{"a": i}, WithCaseID(string(rune(i))))
	}
	loader := NewCaseLoader(cases)

	original := make([]Case, len(loader.Cases()))
	copy(original, loader.Cases())

	loader.ShuffleCases()

	// 打乱后长度不变
	if loader.Len() != 100 {
		t.Errorf("期望 Len=100, 实际=%d", loader.Len())
	}
	// 高概率不会完全一致（100 个元素打乱后与原序列相同的概率极低）
	sameCount := 0
	for i := range original {
		if original[i].CaseID == loader.Cases()[i].CaseID {
			sameCount++
		}
	}
	if sameCount == 100 {
		t.Error("期望 ShuffleCases 后序列与原始不同，但完全一致")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/dataset/ -run "TestCaseLoader" -v 2>&1 | head -20`
Expected: 编译失败，未定义类型

- [ ] **Step 3: 编写 case_loader.go 实现**

```go
package dataset

import (
	"math/rand/v2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CaseLoader Case 容器，支持拆分和打乱。
//
// 对应 Python: openjiuwen/agent_evolving/dataset/case_loader.py CaseLoader
type CaseLoader struct {
	// cases 内部样本列表
	cases []Case
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCaseLoader 创建 CaseLoader 实例。
func NewCaseLoader(cases []Case) *CaseLoader {
	if cases == nil {
		cases = []Case{}
	}
	return &CaseLoader{cases: cases}
}

// Cases 返回内部样本列表的副本。
func (cl *CaseLoader) Cases() []Case {
	result := make([]Case, len(cl.cases))
	copy(result, cl.cases)
	return result
}

// Len 返回样本数量。
func (cl *CaseLoader) Len() int {
	return len(cl.cases)
}

// Split 按比例拆分训练集和验证集。
//
// ratio 为训练集占比（0.0-1.0），返回 (trainLoader, valLoader)。
// 对应 Python: CaseLoader.split(ratio)
func (cl *CaseLoader) Split(ratio float64) (*CaseLoader, *CaseLoader) {
	if len(cl.cases) == 0 {
		return NewCaseLoader(nil), NewCaseLoader(nil)
	}

	splitIdx := int(float64(len(cl.cases)) * ratio)
	if splitIdx < 0 {
		splitIdx = 0
	}
	if splitIdx > len(cl.cases) {
		splitIdx = len(cl.cases)
	}

	trainCases := make([]Case, splitIdx)
	copy(trainCases, cl.cases[:splitIdx])

	valCases := make([]Case, len(cl.cases)-splitIdx)
	copy(valCases, cl.cases[splitIdx:])

	return NewCaseLoader(trainCases), NewCaseLoader(valCases)
}

// ShuffleCases 随机打乱内部样本顺序。
//
// 对应 Python: shuffle_cases(cases)
func (cl *CaseLoader) ShuffleCases() {
	rand.Shuffle(len(cl.cases), func(i, j int) {
		cl.cases[i], cl.cases[j] = cl.cases[j], cl.cases[i]
	})
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/dataset/ -run "TestCaseLoader" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/dataset/case_loader.go internal/evolving/dataset/case_loader_test.go
git commit -m "feat(evolving/dataset): 添加 CaseLoader 样本容器 (9.70b)"
```

---

### Task 4: dataset 包 doc.go

**Files:**
- Create: `internal/evolving/dataset/doc.go`

- [ ] **Step 1: 编写 doc.go**

```go
// Package dataset 提供自演化系统的训练/评估数据类型。
//
// 包含 Case（单个样本）、EvaluatedCase（评估后样本）和 CaseLoader（样本容器）。
// Case 定义输入、期望答案和可选工具列表；EvaluatedCase 在 Case 基础上
// 附加模型输出、综合评分、评分原因和各指标独立评分。
// CaseLoader 支持按比例拆分训练集/验证集和随机打乱。
//
// 文件目录：
//
//	dataset/
//	├── doc.go            # 包文档
//	├── case.go           # Case + EvaluatedCase 结构体
//	└── case_loader.go    # CaseLoader 样本容器
//
// 对应 Python 代码：openjiuwen/agent_evolving/dataset/
package dataset
```

- [ ] **Step 2: 运行测试确认包通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/dataset/ -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/evolving/dataset/doc.go
git commit -m "docs(evolving/dataset): 添加包文档 (9.70b)"
```

---

### Task 5: Metric 接口 + MetricResult + MetricOption

**Files:**
- Create: `internal/evolving/evaluator/metrics/base.go`

- [ ] **Step 1: 编写 base.go**

```go
package metrics

import (
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
)

// ──────────────────────────── 结构体 ────────────────────────────

// metricContext 指标计算的上下文信息，通过 MetricOption 注入。
type metricContext struct {
	// question 查询上下文
	question any
	// case_ 原始 Case
	case_ *dataset.Case
}

// ──────────────────────────── 类型别名 ────────────────────────────

// MetricResult 指标计算结果，统一用 map 表示。
// 单指标：{"exact_match": 1.0}  多指标：{"precision": 0.8, "recall": 0.6}
//
// 对应 Python: MetricResult = Union[float, Dict[str, float]]
type MetricResult = map[string]float64

// ──────────────────────────── 接口 ────────────────────────────

// Metric 评估指标抽象接口。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/base.py Metric
type Metric interface {
	// Name 指标标识名
	Name() string
	// HigherIsBetter 是否越高越好，默认 true
	HigherIsBetter() bool
	// Compute 计算单个样本的指标分数
	Compute(prediction, label any, opts ...MetricOption) (MetricResult, error)
	// ComputeBatch 批量计算指标分数
	ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MetricOption 指标计算的可选参数函数。
type MetricOption func(*metricContext)

// WithQuestion 设置查询上下文。
func WithQuestion(q any) MetricOption {
	return func(mc *metricContext) { mc.question = q }
}

// WithCase 设置原始 Case。
func WithCase(c dataset.Case) MetricOption {
	return func(mc *metricContext) { mc.case_ = &c }
}

// applyMetricOptions 应用选项并返回上下文。
func applyMetricOptions(opts ...MetricOption) *metricContext {
	mc := &metricContext{}
	for _, opt := range opts {
		opt(mc)
	}
	return mc
}

// DefaultComputeBatch 默认批量计算实现：逐个调用 Compute。
//
// 对应 Python: Metric.compute_batch() 默认实现
func DefaultComputeBatch(m Metric, predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	results := make([]MetricResult, len(predictions))
	for i := range predictions {
		result, err := m.Compute(predictions[i], labels[i], opts...)
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}
```

- [ ] **Step 2: 运行编译确认通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/evaluator/metrics/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/evolving/evaluator/metrics/base.go
git commit -m "feat(evolving/evaluator/metrics): 添加 Metric 接口和 MetricResult 类型 (9.71)"
```

---

### Task 6: ExactMatchMetric

**Files:**
- Create: `internal/evolving/evaluator/metrics/exact_match.go`
- Create: `internal/evolving/evaluator/metrics/exact_match_test.go`

- [ ] **Step 1: 编写 exact_match_test.go 失败测试**

```go
package metrics

import "testing"

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestExactMatchMetric_Name 测试指标名称
func TestExactMatchMetric_Name(t *testing.T) {
	m := NewExactMatchMetric()
	if m.Name() != "exact_match" {
		t.Errorf("期望 Name=exact_match, 实际=%s", m.Name())
	}
}

// TestExactMatchMetric_HigherIsBetter 测试 HigherIsBetter
func TestExactMatchMetric_HigherIsBetter(t *testing.T) {
	m := NewExactMatchMetric()
	if !m.HigherIsBetter() {
		t.Error("期望 HigherIsBetter=true")
	}
}

// TestExactMatchMetric_匹配 测试归一化匹配
func TestExactMatchMetric_匹配(t *testing.T) {
	m := NewExactMatchMetric()

	// 完全匹配
	result, err := m.Compute("hello", "hello")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}

	// 大小写不同，归一化后匹配
	result, err = m.Compute("Hello World", "hello world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("归一化后期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}

	// 多余空格，归一化后匹配
	result, err = m.Compute("  hello   world  ", "hello world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("归一化后期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}
}

// TestExactMatchMetric_不匹配 测试不匹配
func TestExactMatchMetric_不匹配(t *testing.T) {
	m := NewExactMatchMetric()

	result, err := m.Compute("hello", "world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 0.0 {
		t.Errorf("期望 exact_match=0.0, 实际=%f", result["exact_match"])
	}
}

// TestExactMatchMetric_非归一化 测试非归一化模式
func TestExactMatchMetric_非归一化(t *testing.T) {
	m := NewExactMatchMetric(WithNormalize(false))

	// 大小写不同，非归一化不匹配
	result, err := m.Compute("Hello", "hello")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 0.0 {
		t.Errorf("非归一化期望 exact_match=0.0, 实际=%f", result["exact_match"])
	}

	// 完全匹配
	result, err = m.Compute("hello", "hello")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["exact_match"] != 1.0 {
		t.Errorf("期望 exact_match=1.0, 实际=%f", result["exact_match"])
	}
}

// TestExactMatchMetric_ComputeBatch 测试批量计算
func TestExactMatchMetric_ComputeBatch(t *testing.T) {
	m := NewExactMatchMetric()
	predictions := []any{"hello", "world"}
	labels := []any{"hello", "foo"}

	results, err := m.ComputeBatch(predictions, labels)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("期望 2 个结果, 实际=%d", len(results))
	}
	if results[0]["exact_match"] != 1.0 {
		t.Errorf("期望 results[0] exact_match=1.0, 实际=%f", results[0]["exact_match"])
	}
	if results[1]["exact_match"] != 0.0 {
		t.Errorf("期望 results[1] exact_match=0.0, 实际=%f", results[1]["exact_match"])
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/evaluator/metrics/ -run TestExactMatchMetric -v 2>&1 | head -20`
Expected: 编译失败，未定义类型

- [ ] **Step 3: 编写 exact_match.go 实现**

```go
package metrics

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExactMatchMetric 精确匹配或归一化匹配指标。
//
// 匹配返回 {"exact_match": 1.0}，不匹配返回 {"exact_match": 0.0}。
// normalize=true 时先归一化（小写+去空格+合并连续空格）再比较。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/exact_match.py ExactMatchMetric
type ExactMatchMetric struct {
	// normalize 是否归一化后比较
	normalize bool
}

// ExactMatchOption ExactMatchMetric 构造选项函数。
type ExactMatchOption func(*ExactMatchMetric)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExactMatchMetric 创建 ExactMatchMetric 实例，默认 normalize=true。
func NewExactMatchMetric(opts ...ExactMatchOption) *ExactMatchMetric {
	m := &ExactMatchMetric{normalize: true}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Name 返回指标名称 "exact_match"。
func (m *ExactMatchMetric) Name() string { return "exact_match" }

// HigherIsBetter 返回 true。
func (m *ExactMatchMetric) HigherIsBetter() bool { return true }

// Compute 计算精确匹配分数。
//
// 对应 Python: ExactMatchMetric.compute(prediction, label)
func (m *ExactMatchMetric) Compute(prediction, label any, opts ...MetricOption) (MetricResult, error) {
	_ = applyMetricOptions(opts...) // ExactMatch 不需要上下文

	var predStr, labelStr string
	if m.normalize {
		predStr = normalizeExactMatch(prediction)
		labelStr = normalizeExactMatch(label)
	} else {
		predStr = fmt.Sprintf("%v", prediction)
		labelStr = fmt.Sprintf("%v", label)
	}

	score := 0.0
	if predStr == labelStr {
		score = 1.0
	}
	return MetricResult{"exact_match": score}, nil
}

// ComputeBatch 批量计算精确匹配分数。
func (m *ExactMatchMetric) ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	return DefaultComputeBatch(m, predictions, labels, opts...)
}

// WithNormalize 设置是否归一化。
func WithNormalize(n bool) ExactMatchOption {
	return func(m *ExactMatchMetric) { m.normalize = n }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeExactMatch 归一化输入：小写 + strip + 合并连续空格。
//
// 对应 Python: ExactMatchMetric._normalize(input_data)
func normalizeExactMatch(input any) string {
	result := strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", input)))
	return strings.Join(strings.Fields(result), " ")
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/evaluator/metrics/ -run TestExactMatchMetric -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/evaluator/metrics/exact_match.go internal/evolving/evaluator/metrics/exact_match_test.go
git commit -m "feat(evolving/evaluator/metrics): 添加 ExactMatchMetric 精确匹配指标 (9.71)"
```

---

### Task 7: LLMAsJudgeMetric

**Files:**
- Create: `internal/evolving/evaluator/metrics/llm_as_judge.go`
- Create: `internal/evolving/evaluator/metrics/llm_as_judge_test.go`

- [ ] **Step 1: 编写 llm_as_judge_test.go 失败测试**

```go
package metrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestLLMAsJudgeMetric_Name 测试指标名称
func TestLLMAsJudgeMetric_Name(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer server.Close()

	m, err := NewLLMAsJudgeMetric(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if m.Name() != "llm_as_judge" {
		t.Errorf("期望 Name=llm_as_judge, 实际=%s", m.Name())
	}
}

// TestLLMAsJudgeMetric_HigherIsBetter 测试 HigherIsBetter
func TestLLMAsJudgeMetric_HigherIsBetter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer server.Close()

	m, err := NewLLMAsJudgeMetric(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if !m.HigherIsBetter() {
		t.Error("期望 HigherIsBetter=true")
	}
}

// TestLLMAsJudgeMetric_通过 测试 LLM 判定通过
func TestLLMAsJudgeMetric_通过(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "```json\n{\"result\": true, \"reason\": \"语义一致\"}\n```",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	m, err := NewLLMAsJudgeMetric(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	result, err := m.Compute("hello world", "hello world", WithQuestion("greet"))
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["llm_as_judge"] != 1.0 {
		t.Errorf("期望 llm_as_judge=1.0, 实际=%f", result["llm_as_judge"])
	}
}

// TestLLMAsJudgeMetric_不通过 测试 LLM 判定不通过
func TestLLMAsJudgeMetric_不通过(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "```json\n{\"result\": false, \"reason\": \"语义不一致\"}\n```",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	m, err := NewLLMAsJudgeMetric(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	result, err := m.Compute("hello", "world")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if result["llm_as_judge"] != 0.0 {
		t.Errorf("期望 llm_as_judge=0.0, 实际=%f", result["llm_as_judge"])
	}
}

// TestLLMAsJudgeMetric_LLM调用异常 测试 LLM 调用失败返回 0.0
func TestLLMAsJudgeMetric_LLM调用异常(t *testing.T) {
	// 服务器返回 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	m, err := NewLLMAsJudgeMetric(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	result, _ := m.Compute("hello", "world")
	if result["llm_as_judge"] != 0.0 {
		t.Errorf("LLM 异常时期望 llm_as_judge=0.0, 实际=%f", result["llm_as_judge"])
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/evaluator/metrics/ -run TestLLMAsJudgeMetric -v 2>&1 | head -20`
Expected: 编译失败，未定义类型

- [ ] **Step 3: 编写 llm_as_judge.go 实现**

```go
package metrics

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMAsJudgeMetric 使用 LLM 作为裁判判断语义一致性的指标。
//
// 通过模板生成评估 prompt，调用 LLM 判断 prediction 与 label 的语义一致性，
// 返回 {"llm_as_judge": 1.0}（通过）或 {"llm_as_judge": 0.0}（失败）。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/metrics/llm_as_judge.py LLMAsJudgeMetric
type LLMAsJudgeMetric struct {
	// model LLM 模型实例
	model *llm.Model
	// template 已填充 user_metrics 的评估模板
	template *prompt.PromptTemplate
	// parser JSON 输出解析器
	parser *output_parsers.JsonOutputParser
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLLMAsJudgeMetric 创建 LLMAsJudgeMetric 实例。
//
// 传入 ModelClientConfig + ModelRequestConfig，内部创建 llm.Model。
// userMetrics 为自定义验证规则，会注入到评估模板中。
//
// 对应 Python: LLMAsJudgeMetric(model_config, model_client_config, user_metrics)
func NewLLMAsJudgeMetric(
	clientConfig llmschema.ModelClientConfig,
	requestConfig llmschema.ModelRequestConfig,
	userMetrics string,
) (*LLMAsJudgeMetric, error) {
	model, err := llm.NewModel(&clientConfig, &requestConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 模型失败: %w", err)
	}

	// 填充 user_metrics 到模板
	template, err := LLMMetricTemplate.Format(map[string]any{"user_metrics": userMetrics})
	if err != nil {
		return nil, fmt.Errorf("格式化评估模板失败: %w", err)
	}
	if template == nil {
		template = LLMMetricTemplate
	}

	return &LLMAsJudgeMetric{
		model:    model,
		template: template,
		parser:   output_parsers.NewJsonOutputParser(),
	}, nil
}

// Name 返回指标名称 "llm_as_judge"。
func (m *LLMAsJudgeMetric) Name() string { return "llm_as_judge" }

// HigherIsBetter 返回 true。
func (m *LLMAsJudgeMetric) HigherIsBetter() bool { return true }

// Compute 使用 LLM-as-Judge 计算语义一致性分数。
//
// 对应 Python: LLMAsJudgeMetric.compute(prediction, label, question=None)
func (m *LLMAsJudgeMetric) Compute(prediction, label any, opts ...MetricOption) (MetricResult, error) {
	mc := applyMetricOptions(opts...)

	// 格式化模板
	formatted, err := m.template.Format(map[string]any{
		"question":        fmt.Sprintf("%v", mc.question),
		"expected_answer": fmt.Sprintf("%v", label),
		"model_answer":    fmt.Sprintf("%v", prediction),
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("格式化 LLM 评估模板失败")
		return MetricResult{"llm_as_judge": 0.0}, nil
	}

	// 转为消息列表
	messages, err := formatted.ToMessages()
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("转换评估模板为消息列表失败")
		return MetricResult{"llm_as_judge": 0.0}, nil
	}

	// 调用 LLM
	response, err := m.model.Invoke(context.Background(), messages)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "LLMAsJudgeMetric.Compute").
			Err(err).
			Msg("LLM 调用失败")
		return MetricResult{"llm_as_judge": 0.0}, nil
	}

	// 解析响应
	return m.parseResult(response), nil
}

// ComputeBatch 批量计算语义一致性分数。
func (m *LLMAsJudgeMetric) ComputeBatch(predictions, labels []any, opts ...MetricOption) ([]MetricResult, error) {
	return DefaultComputeBatch(m, predictions, labels, opts...)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseResult 解析 LLM 评估结果，返回 1.0（通过）或 0.0（失败）。
//
// 对应 Python: LLMAsJudgeMetric._parse_result(response)
func (m *LLMAsJudgeMetric) parseResult(response any) MetricResult {
	// 从响应中提取文本内容
	text := ""
	switch v := response.(type) {
	case *llmschema.AssistantMessage:
		text = v.Content.Text()
	case string:
		text = v
	default:
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "LLMAsJudgeMetric.parseResult").
			Str("response_type", fmt.Sprintf("%T", response)).
			Msg("不支持的响应类型")
		return MetricResult{"llm_as_judge": 0.0}
	}

	parsed, err := m.parser.Parse(text)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "LLMAsJudgeMetric.parseResult").
			Err(err).
			Msg("解析 LLM 评估结果失败")
		return MetricResult{"llm_as_judge": 0.0}
	}

	data, ok := parsed.(map[string]any)
	if !ok {
		return MetricResult{"llm_as_judge": 0.0}
	}

	result, exists := data["result"]
	if !exists {
		return MetricResult{"llm_as_judge": 0.0}
	}

	// 判断 result：true 或 "true" → 1.0，其余 → 0.0
	if isPassResult(result) {
		return MetricResult{"llm_as_judge": 1.0}
	}
	return MetricResult{"llm_as_judge": 0.0}
}

// isPassResult 判断评估结果是否通过。
//
// 对应 Python: DefaultEvaluator._is_pass_result(result)
func isPassResult(result any) bool {
	if result == true {
		return true
	}
	if s, ok := result.(string); ok {
		return s == "true" || s == "True" || s == "TRUE"
	}
	return false
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/evaluator/metrics/ -run TestLLMAsJudgeMetric -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/evaluator/metrics/llm_as_judge.go internal/evolving/evaluator/metrics/llm_as_judge_test.go
git commit -m "feat(evolving/evaluator/metrics): 添加 LLMAsJudgeMetric 语义评估指标 (9.71)"
```

---

### Task 8: 评估提示词模板

**Files:**
- Create: `internal/evolving/evaluator/templates.go`

- [ ] **Step 1: 编写 templates.go**

模板内容一比一复刻 Python 的 `LLM_METRIC_TEMPLATE` 和 `LLM_METRIC_RETRY_TEMPLATE`，占位符保持 `{{variable}}` 格式与项目已有的 PromptTemplate 兼容。

```go
package evaluator

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// LLMMetricTemplate LLM-as-Judge 评估提示词模板。
	//
	// 对应 Python: openjiuwen/agent_evolving/evaluator/templates.py LLM_METRIC_TEMPLATE
	LLMMetricTemplate = prompt.NewPromptTemplate("llm_metric", llmMetricTemplateContent)

	// LLMMetricRetryTemplate 评估结果解析失败时的重试模板。
	//
	// 对应 Python: openjiuwen/agent_evolving/evaluator/templates.py LLM_METRIC_RETRY_TEMPLATE
	LLMMetricRetryTemplate = prompt.NewPromptTemplate("llm_metric_retry", llmMetricRetryTemplateContent)
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// 一比一复刻 Python LLM_METRIC_TEMPLATE，占位符 {{variable}} 与 PromptTemplate 格式一致
const llmMetricTemplateContent = `You are an answer verification expert responsible for checking the semantic and
conclusion consistency between the given model response and the expected answer.
Please determine if the model response is consistent with the expected answer
based on the following criteria:

- If the model response and expected answer have consistent meaning, return ` + "`true`" + `.
- If the model response and expected answer have inconsistent meaning, return ` + "`false`" + `.
- Pay special attention to distinguish between dialogues and tool calls, as they
  usually cannot be judged as consistent based on semantics.
- Briefly analyze the reasons why the model response and expected answer are
  inconsistent, combining with the user question and expected answer.

The following are custom verification rules added by the user. If they conflict
with the above rules, the user's custom rules should take precedence. Please
strictly follow them:
{{user_metrics}}

Output JSON format:
` + "```json" + `
{
"result": true/false,
"reason": "Verification reason"
}
` + "```" + `

[Question]: {{question}}

The following are the model response and expected answer to be compared:
[Expected Answer]: {{expected_answer}}

[Model Response]: {{model_answer}}

Please verify and return the result:
`

// 一比一复刻 Python LLM_METRIC_RETRY_TEMPLATE
const llmMetricRetryTemplateContent = `You are an answer verification expert responsible for fixing non-standard evaluation results.

## Original Evaluation Result to Assess
[Question]: {{question}}
The following are the model response and expected answer to be compared:
[Expected Answer]: {{expected_answer}}
[Model Response]: {{model_answer}}

## Non-standard Evaluation Result Received
However, a non-standard evaluation result has been received, which cannot be correctly parsed into JSON format:
<EVALUATED_RESULT>
{{nonstandard_evaluated_result}}
</EVALUATED_RESULT>

## Format Correction
Please correct the format of the current evaluation result, reason why the
above evaluation result could not be parsed by JSON, correct it, and return
the correct evaluation format as follows:
Output JSON format:
` + "```json" + `
{
"result": true/false,
"reason": "Verification reason"
}
` + "```" + `

## Requirements
- The generated JSON must be wrapped with ` + "```json```" + `
- Pay attention to whether there are non-standard quotation marks in the
  evaluation result, such as incorrect use of double and single quotes,
  nested quotes, etc.

Please verify and return the result:
`
```

- [ ] **Step 2: 运行编译确认通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/evolving/evaluator/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/evolving/evaluator/templates.go
git commit -m "feat(evolving/evaluator): 添加 LLM 评估提示词模板 (9.71)"
```

---

### Task 9: BaseEvaluator + DefaultEvaluator + MetricEvaluator + aggScore

**Files:**
- Create: `internal/evolving/evaluator/evaluator.go`
- Create: `internal/evolving/evaluator/evaluator_test.go`

- [ ] **Step 1: 编写 evaluator_test.go 失败测试**

```go
package evaluator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator/metrics"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewDefaultEvaluator 测试 DefaultEvaluator 构造
func TestNewDefaultEvaluator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	e, err := NewDefaultEvaluator(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if e == nil {
		t.Error("期望返回非 nil DefaultEvaluator")
	}
}

// TestDefaultEvaluator_Evaluate_通过 测试 LLM 判定通过
func TestDefaultEvaluator_Evaluate_通过(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "```json\n{\"result\": true, \"reason\": \"语义一致\"}\n```",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e, err := NewDefaultEvaluator(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	predict := map[string]any{"output": "2"}

	ec, err := e.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ec.Score != 1.0 {
		t.Errorf("期望 Score=1.0, 实际=%f", ec.Score)
	}
	if ec.Reason != "语义一致" {
		t.Errorf("期望 Reason=语义一致, 实际=%s", ec.Reason)
	}
}

// TestDefaultEvaluator_Evaluate_不通过 测试 LLM 判定不通过
func TestDefaultEvaluator_Evaluate_不通过(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "```json\n{\"result\": false, \"reason\": \"不一致\"}\n```",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	e, err := NewDefaultEvaluator(
		schema.ModelClientConfig{APIKey: "test", APIBase: server.URL},
		schema.ModelRequestConfig{ModelName: "test-model"},
		"",
	)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	predict := map[string]any{"output": "3"}

	ec, err := e.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ec.Score != 0.0 {
		t.Errorf("期望 Score=0.0, 实际=%f", ec.Score)
	}
}

// TestMetricEvaluator_Evaluate 测试多指标聚合
func TestMetricEvaluator_Evaluate(t *testing.T) {
	em := metrics.NewExactMatchMetric()
	me := NewMetricEvaluator([]metrics.Metric{em}, "mean")

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	predict := map[string]any{"output": "2"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ec.Score != 1.0 {
		t.Errorf("期望 Score=1.0, 实际=%f", ec.Score)
	}
	if ec.PerMetric["exact_match"] != 1.0 {
		t.Errorf("期望 PerMetric[exact_match]=1.0, 实际=%f", ec.PerMetric["exact_match"])
	}
}

// TestMetricEvaluator_Evaluate_不匹配 测试不匹配场景
func TestMetricEvaluator_Evaluate_不匹配(t *testing.T) {
	em := metrics.NewExactMatchMetric()
	me := NewMetricEvaluator([]metrics.Metric{em}, "mean")

	case_ := dataset.NewCase(
		map[string]any{"query": "1+1"},
		map[string]any{"answer": "2"},
	)
	predict := map[string]any{"output": "3"}

	ec, err := me.Evaluate(context.Background(), *case_, predict)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if ec.Score != 0.0 {
		t.Errorf("期望 Score=0.0, 实际=%f", ec.Score)
	}
}

// TestBatchEvaluate_长度不匹配 测试 cases 和 predicts 长度不一致报错
func TestBatchEvaluate_长度不匹配(t *testing.T) {
	me := NewMetricEvaluator([]metrics.Metric{metrics.NewExactMatchMetric()}, "mean")

	cases := []dataset.Case{*dataset.NewCase(map[string]any{"q": "1"}, map[string]any{"a": "1"})}
	predicts := []map[string]any{{"output": "1"}, {"output": "2"}}

	_, err := me.BatchEvaluate(context.Background(), cases, predicts, 1)
	if err == nil {
		t.Error("期望长度不匹配时返回错误")
	}
}

// TestBatchEvaluate_并行测试 测试并行批量评估
func TestBatchEvaluate_并行测试(t *testing.T) {
	me := NewMetricEvaluator([]metrics.Metric{metrics.NewExactMatchMetric()}, "mean")

	cases := []dataset.Case{
		*dataset.NewCase(map[string]any{"q": "1"}, map[string]any{"a": "hello"}),
		*dataset.NewCase(map[string]any{"q": "2"}, map[string]any{"a": "world"}),
	}
	predicts := []map[string]any{
		{"output": "hello"},
		{"output": "wrong"},
	}

	results, err := me.BatchEvaluate(context.Background(), cases, predicts, 2)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("期望 2 个结果, 实际=%d", len(results))
	}
	if results[0].Score != 1.0 {
		t.Errorf("期望 results[0].Score=1.0, 实际=%f", results[0].Score)
	}
	if results[1].Score != 0.0 {
		t.Errorf("期望 results[1].Score=0.0, 实际=%f", results[1].Score)
	}
}

// TestAggScore_mean 测试 mean 聚合
func TestAggScore_mean(t *testing.T) {
	result := aggScore([]float64{1.0, 0.0, 0.5}, "mean")
	if result != 0.5 {
		t.Errorf("期望 mean=0.5, 实际=%f", result)
	}
}

// TestAggScore_first 测试 first 聚合
func TestAggScore_first(t *testing.T) {
	result := aggScore([]float64{1.0, 0.0, 0.5}, "first")
	if result != 1.0 {
		t.Errorf("期望 first=1.0, 实际=%f", result)
	}
}

// TestAggScore_空切片 测试空切片
func TestAggScore_空切片(t *testing.T) {
	result := aggScore([]float64{}, "mean")
	if result != 0.0 {
		t.Errorf("期望空切片=0.0, 实际=%f", result)
	}
}

// TestAggScore_未知聚合方式 测试未知聚合方式默认 mean
func TestAggScore_未知聚合方式(t *testing.T) {
	result := aggScore([]float64{1.0, 0.0}, "unknown")
	if result != 0.5 {
		t.Errorf("期望未知聚合方式默认 mean=0.5, 实际=%f", result)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/evaluator/ -run "TestNewDefaultEvaluator|TestDefaultEvaluator|TestMetricEvaluator|TestBatchEvaluate|TestAggScore" -v 2>&1 | head -20`
Expected: 编译失败，未定义类型

- [ ] **Step 3: 编写 evaluator.go 实现**

```go
package evaluator

import (
	"context"
	"fmt"
	"math"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/dataset"
	"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator/metrics"
	"golang.org/x/sync/errgroup"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseEvaluator 抽象评估器接口。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/evaluator.py BaseEvaluator
type BaseEvaluator interface {
	// Evaluate 评估单个样本
	Evaluate(ctx context.Context, case_ dataset.Case, predict map[string]any) (*dataset.EvaluatedCase, error)
	// BatchEvaluate 并行批量评估
	BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// DefaultEvaluator 使用 LLM-as-Judge 评估模型输出一致性。
//
// 判定模型输出与期望答案的语义一致性，映射为 0/1 分数。
// 解析失败时使用重试模板再调一次 LLM。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/evaluator.py DefaultEvaluator
type DefaultEvaluator struct {
	// model LLM 模型实例
	model *llm.Model
	// metricTemplate 已填充 user_metrics 的评估模板
	metricTemplate *prompt.PromptTemplate
	// parser JSON 输出解析器
	parser *output_parsers.JsonOutputParser
}

// MetricEvaluator 多指标聚合评估器。
//
// 遍历所有 Metric 计算 score，支持 per-metric 分解和可配置聚合方式。
//
// 对应 Python: openjiuwen/agent_evolving/evaluator/evaluator.py MetricEvaluator
type MetricEvaluator struct {
	// metrics 指标列表
	metrics []metrics.Metric
	// aggregate 聚合方式，"mean" 或 "first"
	aggregate string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultEvaluator 创建 DefaultEvaluator 实例。
//
// 传入 ModelClientConfig + ModelRequestConfig，内部创建 llm.Model。
// metric 为自定义验证规则字符串，会注入到评估模板中。
//
// 对应 Python: DefaultEvaluator(model_config, model_client_config, metric)
func NewDefaultEvaluator(
	clientConfig llmschema.ModelClientConfig,
	requestConfig llmschema.ModelRequestConfig,
	metric string,
) (*DefaultEvaluator, error) {
	model, err := llm.NewModel(&clientConfig, &requestConfig)
	if err != nil {
		return nil, fmt.Errorf("创建 LLM 模型失败: %w", err)
	}

	// 填充 user_metrics 到模板
	template, err := LLMMetricTemplate.Format(map[string]any{"user_metrics": metric})
	if err != nil {
		return nil, fmt.Errorf("格式化评估模板失败: %w", err)
	}
	if template == nil {
		template = LLMMetricTemplate
	}

	return &DefaultEvaluator{
		model:          model,
		metricTemplate: template,
		parser:         output_parsers.NewJsonOutputParser(),
	}, nil
}

// NewMetricEvaluator 创建 MetricEvaluator 实例。
//
// metrics 为指标列表，aggregate 为聚合方式（"mean" 或 "first"）。
//
// 对应 Python: MetricEvaluator(metrics, aggregate)
func NewMetricEvaluator(ms []metrics.Metric, aggregate string) *MetricEvaluator {
	if aggregate == "" {
		aggregate = "mean"
	}
	return &MetricEvaluator{metrics: ms, aggregate: aggregate}
}

// Evaluate 使用 LLM-as-Judge 评估单个样本。
//
// 对应 Python: DefaultEvaluator.evaluate(case, predict)
func (d *DefaultEvaluator) Evaluate(ctx context.Context, case_ dataset.Case, predict map[string]any) (*dataset.EvaluatedCase, error) {
	ec := dataset.NewEvaluatedCase(case_, predict)

	// 格式化模板
	formatted, err := d.metricTemplate.Format(map[string]any{
		"question":        fmt.Sprintf("%v", case_.Inputs),
		"expected_answer": fmt.Sprintf("%v", case_.Label),
		"model_answer":    fmt.Sprintf("%v", predict),
	})
	if err != nil {
		ec.Reason = "Failed to format evaluation template"
		return ec, nil
	}

	// 转为消息列表
	messages, err := formatted.ToMessages()
	if err != nil {
		ec.Reason = "Failed to convert template to messages"
		return ec, nil
	}

	// 调用 LLM
	response, err := d.model.Invoke(ctx, messages)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "DefaultEvaluator.Evaluate").
			Err(err).
			Msg("LLM 调用失败")
		ec.Reason = "Failed to evaluate case due to model error"
		return ec, nil
	}

	// 提取响应文本
	responseText := ""
	switch v := response.(type) {
	case *llmschema.AssistantMessage:
		responseText = v.Content.Text()
	case string:
		responseText = v
	default:
		responseText = fmt.Sprintf("%v", response)
	}

	// 解析评估结果
	evaluatedResult := d.extractEvaluateResult(ctx, responseText, case_, predict)
	if evaluatedResult == nil {
		ec.Reason = "Failed to evaluate case due to parsing error"
		return ec, nil
	}

	result, _ := evaluatedResult["result"]
	reason, _ := evaluatedResult["reason"].(string)

	ec.SetScore(mapBoolToScore(result))
	ec.Reason = reason
	return ec, nil
}

// BatchEvaluate 并行批量评估。
//
// 使用 errgroup.Group + SetLimit(numParallel) 控制并发。
//
// 对应 Python: BaseEvaluator.batch_evaluate(cases, predicts, num_parallel)
func (d *DefaultEvaluator) BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error) {
	if err := validateBatchArgs(len(cases), len(predicts), numParallel); err != nil {
		return nil, err
	}

	results := make([]*dataset.EvaluatedCase, len(cases))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(numParallel)

	for i := range cases {
		i := i
		g.Go(func() error {
			ec, err := d.Evaluate(gCtx, cases[i], predicts[i])
			if err != nil {
				return err
			}
			results[i] = ec
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// Evaluate 使用多指标聚合评估单个样本。
//
// 对应 Python: MetricEvaluator.evaluate(case, predict)
func (m *MetricEvaluator) Evaluate(_ context.Context, case_ dataset.Case, predict map[string]any) (*dataset.EvaluatedCase, error) {
	ec := dataset.NewEvaluatedCase(case_, predict)
	perMetric := make(map[string]float64)
	var scores []float64

	for _, metric := range m.metrics {
		result, err := metric.Compute(predict, case_.Label, metrics.WithQuestion(case_.Inputs), metrics.WithCase(case_))
		if err != nil {
			logger.Warn(logComponent).
				Str("metric_name", metric.Name()).
				Err(err).
				Msg("指标计算失败")
			continue
		}
		for k, v := range result {
			vf := safeConvert(v)
			perMetric[k] = vf
			scores = append(scores, vf)
		}
	}

	ec.SetScore(aggScore(scores, m.aggregate))
	if len(perMetric) > 0 {
		ec.PerMetric = perMetric
	}
	return ec, nil
}

// BatchEvaluate 并行批量评估。
func (m *MetricEvaluator) BatchEvaluate(ctx context.Context, cases []dataset.Case, predicts []map[string]any, numParallel int) ([]*dataset.EvaluatedCase, error) {
	if err := validateBatchArgs(len(cases), len(predicts), numParallel); err != nil {
		return nil, err
	}

	results := make([]*dataset.EvaluatedCase, len(cases))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(numParallel)

	for i := range cases {
		i := i
		g.Go(func() error {
			ec, err := m.Evaluate(gCtx, cases[i], predicts[i])
			if err != nil {
				return err
			}
			results[i] = ec
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractEvaluateResult 解析 LLM 评估响应，解析失败时用重试模板再调一次 LLM。
//
// 对应 Python: DefaultEvaluator._extract_evaluate_result(response, case, predict)
func (d *DefaultEvaluator) extractEvaluateResult(ctx context.Context, response string, case_ dataset.Case, predict map[string]any) map[string]any {
	// 第一次解析
	parsed, err := d.parser.Parse(response)
	if err == nil {
		if data, ok := parsed.(map[string]any); ok {
			if _, hasResult := data["result"]; hasResult {
				if _, hasReason := data["reason"]; hasReason {
					return data
				}
			}
		}
	}

	// 解析失败，使用重试模板
	logger.Warn(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "DefaultEvaluator.extractEvaluateResult").
		Msg("首次解析评估结果失败，尝试重试模板")

	retryFormatted, err := LLMMetricRetryTemplate.Format(map[string]any{
		"question":                      fmt.Sprintf("%v", case_.Inputs),
		"expected_answer":               fmt.Sprintf("%v", case_.Label),
		"model_answer":                  fmt.Sprintf("%v", predict),
		"nonstandard_evaluated_result":  response,
	})
	if err != nil {
		return nil
	}

	retryMessages, err := retryFormatted.ToMessages()
	if err != nil {
		return nil
	}

	retryResponse, err := d.model.Invoke(ctx, retryMessages)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "DefaultEvaluator.extractEvaluateResult").
			Err(err).
			Msg("重试 LLM 调用失败")
		return nil
	}

	retryText := ""
	switch v := retryResponse.(type) {
	case *llmschema.AssistantMessage:
		retryText = v.Content.Text()
	case string:
		retryText = v
	default:
		return nil
	}

	retryParsed, err := d.parser.Parse(retryText)
	if err != nil {
		return nil
	}
	if data, ok := retryParsed.(map[string]any); ok {
		return data
	}
	return nil
}

// mapBoolToScore 将 result 字段映射为 0.0 或 1.0。
//
// 对应 Python: DefaultEvaluator._is_pass_result(result)
func mapBoolToScore(result any) float64 {
	if isPassResult(result) {
		return 1.0
	}
	return 0.0
}

// isPassResult 判断评估结果是否通过。
func isPassResult(result any) bool {
	if result == true {
		return true
	}
	if s, ok := result.(string); ok {
		return s == "true" || s == "True" || s == "TRUE"
	}
	return false
}

// aggScore 聚合分数。
//
// 对应 Python: _agg_score(results, aggregate)
func aggScore(results []float64, aggregate string) float64 {
	if len(results) == 0 {
		return 0.0
	}
	switch aggregate {
	case "first":
		return results[0]
	case "mean":
		fallthrough
	default:
		sum := 0.0
		for _, v := range results {
			sum += v
		}
		return sum / float64(len(results))
	}
}

// safeConvert 安全转换 metric 值为 float64。
//
// 对应 Python: MetricEvaluator._safe_convert(num)
func safeConvert(num float64) float64 {
	if math.IsNaN(num) || math.IsInf(num, 0) {
		logger.Warn(logComponent).
			Float64("value", num).
			Msg("metric 值为 NaN 或 Inf，已转换为 0.0")
		return 0.0
	}
	return num
}

// validateBatchArgs 校验批量评估参数。
func validateBatchArgs(casesLen, predictsLen, numParallel int) error {
	if casesLen != predictsLen {
		return exception.NewBaseError(
			exception.StatusToolchainEvaluatorExecutionError,
			exception.WithMsg(fmt.Sprintf(
				"length of cases: %d does not equal with length of predicts: %d",
				casesLen, predictsLen,
			)),
		)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/evaluator/ -run "TestNewDefaultEvaluator|TestDefaultEvaluator|TestMetricEvaluator|TestBatchEvaluate|TestAggScore" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/evaluator/evaluator.go internal/evolving/evaluator/evaluator_test.go
git commit -m "feat(evolving/evaluator): 添加 BaseEvaluator/DefaultEvaluator/MetricEvaluator (9.71)"
```

---

### Task 10: evaluator_pipeline 占位 + metrics 包 doc.go + evaluator 包 doc.go

**Files:**
- Create: `internal/evolving/evaluator/evaluator_pipeline/doc.go`
- Create: `internal/evolving/evaluator/metrics/doc.go`
- Create: `internal/evolving/evaluator/doc.go`

- [ ] **Step 1: 编写 evaluator_pipeline/doc.go**

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

- [ ] **Step 2: 编写 metrics/doc.go**

```go
// Package metrics 提供自演化系统的评估指标。
//
// 包含 Metric 抽象接口、MetricResult 统一返回类型、
// ExactMatchMetric 精确匹配指标和 LLMAsJudgeMetric LLM 语义评估指标。
//
// 文件目录：
//
//	metrics/
//	├── doc.go             # 包文档
//	├── base.go            # Metric 接口 + MetricResult + MetricOption
//	├── exact_match.go     # ExactMatchMetric 精确匹配指标
//	└── llm_as_judge.go    # LLMAsJudgeMetric LLM 语义评估指标
//
// 对应 Python 代码：openjiuwen/agent_evolving/evaluator/metrics/
package metrics
```

- [ ] **Step 3: 编写 evaluator/doc.go**

```go
// Package evaluator 提供自演化系统的评估器。
//
// 包含 BaseEvaluator 抽象接口、DefaultEvaluator（LLM-as-Judge 评估器）、
// MetricEvaluator（多指标聚合评估器）和评估提示词模板。
// BaseEvaluator 定义 Evaluate/BatchEvaluate 两个核心方法，
// BatchEvaluate 使用 errgroup 实现并行评估。
//
// 文件目录：
//
//	evaluator/
//	├── doc.go                # 包文档
//	├── evaluator.go          # BaseEvaluator + DefaultEvaluator + MetricEvaluator + aggScore
//	├── templates.go          # LLMMetricTemplate + LLMMetricRetryTemplate 评估提示词模板
//	├── metrics/              # 评估指标子包
//	│   ├── doc.go            # 包文档
//	│   ├── base.go           # Metric 接口 + MetricResult + MetricOption
//	│   ├── exact_match.go    # ExactMatchMetric
//	│   └── llm_as_judge.go   # LLMAsJudgeMetric
//	└── evaluator_pipeline/   # ⤵️ 容器化技能演化流水线，等待后续章节回填
//	    └── doc.go
//
// 对应 Python 代码：openjiuwen/agent_evolving/evaluator/
package evaluator
```

- [ ] **Step 4: 运行全量测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -v`
Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/evaluator/evaluator_pipeline/doc.go internal/evolving/evaluator/metrics/doc.go internal/evolving/evaluator/doc.go
git commit -m "docs(evolving/evaluator): 添加包文档和 evaluator_pipeline 占位 (9.71)"
```

---

### Task 11: 回填 Trainer 的 evaluator 字段类型

**Files:**
- Modify: `internal/evolving/trainer/trainer.go`
- Modify: `internal/evolving/trainer/trainer_test.go`

- [ ] **Step 1: 修改 trainer.go 中 evaluator 字段和 WithEvaluator 签名**

在 trainer.go 中：
1. 将 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/evolving/evaluator"`
2. 将 `evaluator any` 替换为 `evaluator evaluator.BaseEvaluator`
3. 将 `WithEvaluator(evaluator any)` 替换为 `WithEvaluator(e evaluator.BaseEvaluator)`

- [ ] **Step 2: 更新 trainer_test.go 中的 WithEvaluator 测试**

在 `TestNewTrainer_使用选项` 测试中，`WithEvaluator` 参数从 `any` 改为 `evaluator.BaseEvaluator`。添加一个 DefaultEvaluator 的 mock 或使用 nil 测试。

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/trainer/ -v`
Expected: PASS

- [ ] **Step 4: 运行全部 evolving 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/evolving/... -v`
Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/evolving/trainer/trainer.go internal/evolving/trainer/trainer_test.go
git commit -m "refactor(evolving/trainer): 回填 evaluator 字段类型为 BaseEvaluator (9.71 回填)"
```

---

### Task 12: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.70b 和 9.71 状态**

将以下行从 `☐` 改为 `✅`：
- `9.70b` → `✅`
- `9.71` → `✅`（核心评估器层部分，evaluator_pipeline 标注 ⤵️）

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 9.70b + 9.71 状态为已完成"
```
