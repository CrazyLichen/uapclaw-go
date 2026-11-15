// Package evaluator 提供 RSI 评估器侧的运行时适配件。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/evaluator/。17.4b1 按
// 前置件方案A只落地 evaluation_result_analyzer 实际使用的窄切片：
// RSISysOperationRail（诊断 agent 的只读系统操作轨，b2 的
// agent_runtime.DiagnosisAgentRuntime 挂载）。包内其余符号（LLM judge
// 适配、verdict 解析等）⤴️ 17.6 评估 agent 章节补齐，届时删除本说明中
// 的切片标注。
//
// 文件目录：
//
//	evaluator/
//	├── doc.go                # 包文档
//	└── runtime_adapters.go  # RSISysOperationRail（17.4b1 切片 ⤴️ 17.6 补齐其余）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/
package evaluator
