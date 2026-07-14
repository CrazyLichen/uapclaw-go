// Package metrics 提供自演化系统的评估指标。
//
// 包含 Metric 抽象接口、MetricResult 统一返回类型、
// ExactMatchMetric 精确匹配指标和 LLMAsJudgeMetric LLM 语义评估指标。
// 同时包含 LLM 评估提示词模板（LLMMetricTemplate + LLMMetricRetryTemplate）。
//
// 文件目录：
//
//	metrics/
//	├── doc.go             # 包文档
//	├── base.go            # Metric 接口 + MetricResult + MetricOption
//	├── exact_match.go     # ExactMatchMetric 精确匹配指标
//	├── llm_as_judge.go    # LLMAsJudgeMetric LLM 语义评估指标
//	└── templates.go       # LLMMetricTemplate + LLMMetricRetryTemplate 评估模板
//
// 对应 Python 代码：openjiuwen/agent_evolving/evaluator/metrics/
package metrics
