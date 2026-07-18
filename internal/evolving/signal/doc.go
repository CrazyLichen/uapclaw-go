// Package signal 提供自演化信号类型与转换工具。
//
// 信号（EvolutionSignal）标识 Agent 执行过程中的问题类型和诊断信息，
// 驱动优化器决定优化方向。本包同时提供离线评估结果到信号的转换函数。
//
// 文件目录：
//
//	signal/
//	├── doc.go           # 包文档
//	├── signal.go        # EvolutionSignal 最小 struct
//	└── from_eval.go     # EvaluatedCase → EvolutionSignal 转换
//
// 对应 Python 代码：openjiuwen/agent_evolving/signal/
package signal
