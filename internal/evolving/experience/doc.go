// Package experience 提供在线经验生命周期编排。
//
// Experience 包是在线经验生命周期的核心编排层，连接上游信号/轨迹检测
// 与下游 checkpointing 持久化。包含评分器（Scorer）、展示追踪器（Tracker）、
// 全生命周期管理器（Manager）和在线演进编排器（Orchestrator）。
//
// 文件目录：
//
//	experience/
//	├── doc.go           # 包文档
//	├── types.go         # EvolutionContext / OnlineEvolutionStatus / ExperienceProposal / ExperienceApprovalRequest / OnlineEvolutionResult / ExperienceApplyResult
//	├── lifecycle.go     # LocalApplyPreview / PendingCommitResult / HostFacingExperienceResult / RebuildRequest
//	├── common.go        # MakePendingChange / RejectPendingChange / CommitPendingChange / ExecuteSimplifyActions / RequestRebuildContext
//	├── scorer.go        # ExperienceScorer + CalcE/U/F/Score/UpdateScore + 双语提示词 + ParseLLMJSON
//	├── tracker.go       # ExperienceTracker + PresentedRecordEntry + 包级 session map
//	├── manager.go       # ExperienceManager 全生命周期管理 + rebuild 提示词模板
//	└── orchestrator.go  # OnlineEvolutionOrchestrator Evolve 管线
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/
package experience
