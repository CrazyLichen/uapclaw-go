// case_reader.go 提供评估工件的文件系统读取层。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/case_reader.py（全量对齐）。

package evaluationresultanalyzer

// ──────────────────────────── 结构体 ────────────────────────────

// EvaluationSummaryInput 分析器上下文使用的聚合评估汇总。
// Python: case_reader.py:16-22 EvaluationSummaryInput（frozen dataclass → Go struct，偏差 D-5）。
type EvaluationSummaryInput struct {
	// TotalCases 案例总数
	TotalCases int
	// PassedCount 通过数
	PassedCount int
	// FailedCount 失败数
	FailedCount int
	// AverageScore 平均分
	AverageScore float64
	// EvaluationMethod 评估方法
	EvaluationMethod string
}

// DeterministicSignals 评估案例输出的零 LLM 预提取信号。
// Python: case_reader.py:25-31 DeterministicSignals。
type DeterministicSignals struct {
	// Method 评估方法
	Method string
	// ExecFailures 执行失败案例 ID 清单
	ExecFailures []string
	// JudgeFailures 评估未通过案例 ID 清单
	JudgeFailures []string
	// ErrorClusters 错误聚类（元素含 fingerprint/cases，偏差 D-6）
	ErrorClusters []map[string]any
	// MethodSpecific 方法专属信号
	MethodSpecific map[string]any
}

// CaseAnalysisInput 单个评估案例的结构化输入（result + trace 组装）。
// Python: case_reader.py:34-54 CaseAnalysisInput。
type CaseAnalysisInput struct {
	// CaseID 案例 ID
	CaseID string
	// Status 执行状态
	Status string
	// Score 分数
	Score float64
	// Input 案例输入
	Input string
	// Expected 期望输出（可缺失）
	Expected *string
	// Response 响应
	Response string
	// Error 错误
	Error string
	// EvaluationMethod 评估方法
	EvaluationMethod string
	// EvaluationPassed 是否通过
	EvaluationPassed bool
	// EvaluationReason 评估理由
	EvaluationReason string
	// EvaluationMetadata 评估元数据
	EvaluationMetadata map[string]any
	// TracePath 轨迹文件路径
	TracePath string
	// ResultPath 结果文件路径
	ResultPath string
	// TrajectoryWindowSummary 有界轨迹窗口摘要
	TrajectoryWindowSummary map[string]any
	// NormalizedTraceSummary 有界归一化轨迹摘要
	NormalizedTraceSummary map[string]any
	// TrainingSignal 训练信号
	TrainingSignal map[string]any
	// BenchmarkTestContract 基准测试契约
	BenchmarkTestContract map[string]any
	// TerminationReason 终止原因
	TerminationReason string
	// ExecutionMetadata 执行元数据
	ExecutionMetadata map[string]any
}
