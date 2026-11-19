// Package scheduling 提供 scheduled dispatch 相关的 spec 变换与调度面。
//
// 当前内容：reviewer 基座收窄变换（BuildReviewerAgentSpec）——把成员 spec
// 收窄成一次性 reviewer 所需的形态（文件工具只读、无 todo planning、无
// subagents、1200s 硬超时）。调度循环本体（scheduling 包）⤴️ 25.2 域。
//
// 文件目录：
//
//	scheduling/
//	├── doc.go                # 包文档
//	└── reviewer_spec.go     # reviewer 基座收窄变换（reviewer_spec.py 1:1）
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/scheduling/reviewer_spec.py
package scheduling
