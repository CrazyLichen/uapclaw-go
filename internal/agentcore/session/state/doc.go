// Package state 提供会话状态管理的抽象接口定义和内存实现。
//
// 本包定义了 4 层接口层次：ReadableState（只读访问）→ RecoverableState（快照恢复）
// → State（可读写）→ CommitState（事务性提交/回滚），以及 StateKey 类型用于封装
// string/map/slice 三态访问键。InMemoryState 和 InMemoryCommitState 提供基于内存的实现。
//
// 本包是 Agent Session 和 Workflow Session 共享的状态基础设施。
// AgentStateCollection 组合 globalState + agentState，用于 Agent 会话场景。
// Workflow State 的 StateCollection 后续基于本层接口构建。
//
// 文件目录：
//
//	state/
//	├── doc.go                        # 包文档
//	├── key.go                        # StateKey 类型 + StateKeyType 枚举 + 构造函数
//	├── state.go                      # 4 层接口 + Transformer 类型 + 常量
//	├── agent_state_collection.go     # Agent 状态集合（组合 global + agent state）
//	├── inmemory_state.go             # InMemoryState 实现 State 接口
//	├── inmemory_commit_state.go      # InMemoryCommitState 实现 CommitState 接口
//	└── utils.go                      # 深拷贝 / 嵌套路径解析 / 状态读写工具函数
//
// 对应 Python 代码：openjiuwen/core/session/state/base.py + openjiuwen/core/session/state/agent_state.py + openjiuwen/core/session/utils.py
//
// 核心类型/接口索引：
//
//	ReadableState        — 只读状态访问接口
//	RecoverableState     — 可恢复状态接口，支持快照保存和恢复
//	State                — 可读写状态接口，组合只读和可恢复能力
//	CommitState          — 事务性状态接口，支持按节点 ID 的提交/回滚
//	StateKey             — 状态访问键，封装 string/map/slice 三态
//	InMemoryState        — State 接口的内存实现
//	InMemoryCommitState  — CommitState 接口的内存实现
//	AgentStateCollection — Agent 会话状态集合，组合 globalState + agentState
package state
