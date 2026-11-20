// interfaces.go 定义评估结果诊断分析的可插拔扩展接口。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/interfaces.py。
// 偏差 D-1/D-2（Protocol 属性→方法、async→ctx）见 17.4b1 spec §6。

package evaluationresultanalyzer

import (
	"context"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
)

// 确保编译期引用 schema 契约（Invocation/Artifact 来自 17.4a schema.go）。
var (
	_ harnessrsi.EvaluationResultAnalysisInvocation
	_ harnessrsi.EvaluationResultAnalysisArtifact
)

// ──────────────────────────── 结构体 ────────────────────────────

// SignalExtractor 按 judger 方法键控的零 LLM 预提取策略（可插拔）。
// Python: interfaces.py:17-29 SignalExtractor Protocol（name 属性 → Name() 方法，D-1）。
type SignalExtractor interface {
	// Name 提取器稳定名（日志与元数据用）。
	// Python: name 属性
	Name() string
	// Extract 从评估输出提取确定性信号，不发起 LLM 调用。
	// Python: extract
	Extract(summary EvaluationSummaryInput, caseInputs []CaseAnalysisInput) DeterministicSignals
}

// EvaluationResultAnalysisStrategy 产出 Team 问题分析产物的可插拔策略。
// 每个实现只接收原始调用单，独立负责自身的数据载入、信号提取与推理。
// Python: interfaces.py:32-48 EvaluationResultAnalysisStrategy Protocol。
type EvaluationResultAnalysisStrategy interface {
	// Name 策略稳定名（分析元数据用）。
	// Python: name 属性
	Name() string
	// Analyze 运行完整分析流水线并返回结构化问题产物。
	// Python: async analyze（async → ctx 显式取消通道，偏差 D-2）
	Analyze(ctx context.Context, invocation harnessrsi.EvaluationResultAnalysisInvocation) (harnessrsi.EvaluationResultAnalysisArtifact, error)
}
