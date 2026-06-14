// Package session 提供会话管理的抽象接口、代理实现和 Agent/Workflow 公开会话。
//
// 本包定义 BaseSession 接口，作为所有会话类型的统一抽象。ProxySession 实现代理模式。
// Session 是 Agent 场景下的公开会话，组合内部层 AgentSession，提供 PreRun/PostRun
// 生命周期、状态读写、流写入等用户面向 API。
// WorkflowSession 是工作流场景下的公开会话，组合内部层 WorkflowSession，提供状态读写、
// 环境变量管理、工作流卡片等业务功能。
//
// 本包依赖 state 子包提供的状态接口（State/CommitState 等），Config/Tracer/
// StreamWriterManager/Checkpointer/ActorManager 等依赖类型暂用 any 占位，
// 待后续步骤（5.8/5.10/5.11/5.12）回填具体类型。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session.go          # BaseSession 接口 + ProxySession 实现
//	├── agent.go            # Session 公开会话（Agent 场景）
//	├── workflow.go         # WorkflowSession 公开会话（Workflow 场景）
//	├── state/              # 状态接口与内存实现
//	│   ├── doc.go                           # state 包文档
//	│   ├── state.go                         # 4 层接口 + 常量
//	│   ├── key.go                           # StateKey 类型
//	│   ├── agent_state_collection.go        # Agent 状态集合
//	│   ├── workflow_state_collection.go     # Workflow 四区状态集合
//	│   ├── workflow_commit_state.go         # Workflow 可提交状态
//	│   ├── workflow_inmemory_state.go       # InMemoryWorkflowState 构造器
//	│   ├── inmemory_state.go                # InMemoryState
//	│   ├── inmemory_commit_state.go         # InMemoryCommitState
//	│   └── utils.go                         # 工具函数
//	└── internal/           # 内部会话实现
//	    ├── doc.go                # internal 包文档
//	    ├── agent_session.go      # AgentSession
//	    └── workflow_session.go   # WorkflowSession/NodeSession/SubWorkflowSession
//
// 对应 Python 代码：openjiuwen/core/session/agent.py + openjiuwen/core/session/session.py + openjiuwen/core/session/workflow.py
//
// 核心类型/接口索引：
//
//	BaseSession       — 会话基类接口，所有会话类型的核心抽象
//	ProxySession      — 代理会话，将调用委托给内部 stub
//	Session           — Agent 公开会话，用户面向 API
//	WorkflowSession   — Workflow 公开会话，用户面向 API
package session
