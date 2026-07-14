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
//	├── metrics/              # 评估指标子包
//	│   ├── doc.go            # 包文档
//	│   ├── base.go           # Metric 接口 + MetricResult + MetricOption
//	│   ├── exact_match.go    # ExactMatchMetric
//	│   ├── llm_as_judge.go   # LLMAsJudgeMetric
//	│   └── templates.go      # LLMMetricTemplate + LLMMetricRetryTemplate
//	└── evaluator_pipeline/   # ⤵️ 容器化技能演化流水线，等待后续章节回填
//	    └── doc.go
//
// 对应 Python 代码：openjiuwen/agent_evolving/evaluator/
package evaluator
