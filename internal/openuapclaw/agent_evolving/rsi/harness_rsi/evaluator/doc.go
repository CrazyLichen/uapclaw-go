// Package evaluator 提供 RSI 评估器侧的运行时适配件。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/evaluator/。17.4b1 按
// 前置件方案A只落地 evaluation_result_analyzer 实际使用的窄切片：
// RSISysOperationRail（诊断 agent 的只读系统操作轨，b2 的
// agent_runtime.DiagnosisAgentRuntime 挂载）。包内其余符号（LLM judge
// 适配、verdict 解析等）⤴️ 17.6 评估 agent 章节补齐，届时删除本说明中
// 的切片标注。17.4f 增补：TeamEvaluator 打分桩（team_evaluator.go）已落地——
// 真签名 + 哨兵错误 ErrEvaluatorNotImplemented，17.6 真身落地时按规则 8 删除。
//
// 文件目录：
//
//	evaluator/
//	├── doc.go                # 包文档
//	├── runtime_adapters.go  # RSIBashTool/RSISysOperationRail（17.4b1）+ RSITrajectoryRail（25.2b）+ RSISkillUseRail + 空响应恢复（17.6a 全量）
//	└── team_evaluator.go    # TeamEvaluator 打分桩（⤴️ 17.6 回填删除）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/
package evaluator
