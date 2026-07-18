// Package signal 提供自演化信号类型与转换工具。
//
// 信号（EvolutionSignal）标识 Agent 执行过程中的问题类型和诊断信息，
// 驱动优化器决定优化方向。本包同时提供离线评估结果到信号的转换函数，
// 对话信号检测器（ConversationSignalDetector）用于从轨迹和消息中在线检测演化信号，
// 以及团队域信号检测器（TeamSignalDetector）用于检测团队技能的协作问题和用户改进意图。
//
// 文件目录：
//
//	signal/
//	├── doc.go           # 包文档
//	├── signal.go        # EvolutionSignal 核心类型、工厂函数、去重指纹
//	├── from_eval.go     # EvaluatedCase → EvolutionSignal 转换
//	├── from_conv.go     # ConversationSignalDetector 对话信号检测
//	└── team.go          # TeamSignalDetector 团队域信号检测
//
// 对应 Python 代码：openjiuwen/agent_evolving/signal/
package signal
