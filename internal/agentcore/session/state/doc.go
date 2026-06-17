// Package state 提供会话状态管理的双层抽象接口和内存实现。
//
// 本包定义了双层接口体系，完全对齐 Python 的 StateLike/State 分离设计：
//
// 底层体系（StateLike 系列，面向存储实现）：
//   - ReadableStateLike（只读访问）→ RecoverableStateLike（快照恢复）
//   - → StateLike（可读写）→ CommitStateLike（事务性提交/回滚）
//
// 上层体系（面向会话调用方）：
//   - SessionState：提供 GetGlobal/UpdateGlobal/UpdateTrace/Dump 等方法，
//     由 AgentStateCollection 和 WorkflowStateCollection 实现。
//     消费方通过此接口多态调用，无需类型断言。
//
// 文件目录：
//
//	state/
//	├── doc.go                           # 包文档
//	├── key.go                           # StateKey 类型 + StateKeyType 枚举 + 构造函数
//	├── state.go                         # 双层接口 + Transformer 类型 + 常量
//	├── agent_state_collection.go        # Agent 状态集合（组合 global + agent state + trace）
//	├── workflow_state_collection.go     # Workflow 四区状态集合（io/global/comp/workflow）
//	├── workflow_commit_state.go         # Workflow 可提交状态（commit/rollback/IO 操作）
//	├── workflow_inmemory_state.go       # InMemoryWorkflowState 便捷构造器
//	├── inmemory_state.go                # InMemoryStateLike 实现 StateLike + SessionState 接口
//	├── inmemory_commit_state.go         # InMemoryCommitState 实现 CommitStateLike 接口（不实现 SessionState）
//	└── utils.go                         # 深拷贝 / 嵌套路径解析 / 状态读写工具函数
//
// 对应 Python 代码：openjiuwen/core/session/state/base.py + openjiuwen/core/session/state/agent_state.py + openjiuwen/core/session/state/workflow_state.py + openjiuwen/core/session/utils.py
//
// 核心类型/接口索引：
//
//	底层体系：
//	ReadableStateLike        — 只读状态访问接口
//	RecoverableStateLike     — 可恢复状态接口，支持快照保存和恢复
//	StateLike                — 可读写状态接口，组合只读和可恢复能力
//	CommitStateLike          — 事务性状态接口，支持按节点 ID 的提交/回滚
//
//	上层体系：
//	SessionState             — 会话状态接口，面向调用方统一抽象
//
//	具体实现：
//	InMemoryStateLike        — StateLike + SessionState 接口的内存实现
//	InMemoryCommitState      — CommitStateLike 接口的内存实现（不实现 SessionState）
//	AgentStateCollection     — Agent 会话状态集合，组合 globalState + agentState + traceState
//	WorkflowStateCollection  — Workflow 四区状态集合，组合 io/global/comp/workflow
//	WorkflowCommitState      — Workflow 可提交状态，增加 commit/rollback/IO 操作
//	StateKey                 — 状态访问键，封装 string/map/slice/all 四态
package state
