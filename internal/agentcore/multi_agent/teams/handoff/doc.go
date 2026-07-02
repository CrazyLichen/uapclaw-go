// Package handoff 提供 HandoffTeam 交接编排模式实现。
//
// 交接编排模式下，Agent 按照预定义的路由规则依次交接任务，
// 每个 Agent 处理完毕后决定下一步交接给哪个 Agent，直到满足终止条件。
//
// 文件目录：
//
//	handoff/
//	├── doc.go                # 包文档
//	├── handoff_config.go     # HandoffRoute/HandoffConfig/HandoffTeamConfig 配置定义
//	├── handoff_request.go    # HandoffHistoryEntry/HandoffRequest 交接驱动消息
//	└── interrupt.go          # TeamInterruptSignal/ExtractInterruptSignal/FlushTeamSession 团队中断信号
//
// 对应 Python 代码：openjiuwen/core/multi_agent/teams/handoff/
package handoff
