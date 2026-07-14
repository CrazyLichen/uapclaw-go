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
