// Package evaluationresultanalyzer 提供评估结果的独立诊断分析（读件层）。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/，
// 是 RSI harness 演进闭环（17.4）中的"会诊"环节：上游吃评估器（17.6）
// 产出的成绩单，逐案深挖失败证据链，产出结构化病历（TeamIssue），下游喂
// 给手术系统 member_optimizer（17.4c）与单 harness 迭代循环（17.4d）。
// 整个子包是只读的：不修改任何被评估产物。
//
// 17.4b1 / 17.4b2 交棒（见 spec 2026-11-15-rsi-17.4b1）：
//
//   - b1（本节）：不碰 DeepAgent 运行时的读件层——接口契约、任务条款、
//     仓库快照、路径预算、harness 只读投映、案例读取、信号提取、运行时
//     证据、证据压缩。
//   - b2（已完成）：evidence_investigation（确定性调查计划执行器，Python 侧
//     零生产调用方 ⤴️17.4d，偏差 D-12）、agent_runtime（DiagnosisAgentRuntime/
//     run_deep_agent_text，只读轨+预算轨双 Rail）、analyzer（门面 + 诊断提示词
//   - 聚合会诊 + TeamIssue 后处理）、导出收口（见下方 __all__ 对照）。
//
// 前置件切片（归属在各自未来包，见 harness_rsi/evaluator 与
// harness_rsi/member_optimizer 的 doc.go）：
//
//   - evaluator.RSISysOperationRail ⤴️ 17.6（b2 的诊断 agent 挂载）
//   - member_optimizer loader 切片（load_eval_ref 等）⤴️ 17.4c
//   - member_optimizer model_config（全量对齐，无回填）
//
// 文件目录：
//
//	evaluation_result_analyzer/
//	├── doc.go                    # 包文档
//	├── interfaces.go             # SignalExtractor / EvaluationResultAnalysisStrategy 契约
//	├── task_contract.go          # 任务条款切分与精确引用
//	├── repository_snapshot.go    # 仓库快照拷贝（部分失败容忍）
//	├── diagnosis_paths.go        # 诊断文件路径预算（Windows 247）
//	├── harness_context.go        # harness 声明只读投映
//	├── case_reader.go            # 评估工件文件系统读取
//	├── signal_extractor.go       # 确定性信号提取（指纹归一化 + 注册工厂）
//	├── runtime_evidence.go       # 运行时证据白名单快照
//	├── evidence_compactor.go     # 决策中心证据压缩
//	├── evidence_investigation.go # 17.4b2 待落地
//	├── agent_runtime.go          # 17.4b2 待落地
//	└── analyzer.go              # 17.4b2 待落地
//
// Python __init__.py __all__ 导出对照（偏差 D-8：Go 以包级导出符号承载）：
//
//	CaseAnalysisInput / DeterministicSignals / EvaluationSummaryInput
//	    → case_reader.go 同名导出类型
//	EvaluationResultAnalysisStrategy / SignalExtractor
//	    → interfaces.go 同名导出接口
//	EvaluationResultAnalyzer / build_analysis_strategy
//	    → analyzer.go EvaluationResultAnalyzer / BuildAnalysisStrategy
//	RewardSignalExtractor / build_signal_extractor / register_signal_extractor
//	    → signal_extractor.go 同名导出类型与工厂
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/
package evaluationresultanalyzer
