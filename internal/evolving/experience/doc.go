// Package experience 提供在线经验生命周期编排的类型定义。
//
// 当前仅定义 PendingChange 结构体（供 9.78 checkpointing 引用），
// 其余类型（EvolutionContext/ExperienceProposal/ExperienceApprovalRequest/OnlineEvolutionResult 等）
// 将在 9.79 完整实现时补充。
//
// 文件目录：
//
//	experience/
//	├── doc.go   # 包文档
//	├── types.go # PendingChange 及相关类型
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/types.py
package experience
