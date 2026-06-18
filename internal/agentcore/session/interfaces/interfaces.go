package interfaces

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseSession 会话基类接口，定义所有会话类型共有的核心能力。
// 对应 Python: openjiuwen/core/session/session.py BaseSession(ABC)
//
// AgentSession、WorkflowSession、NodeSession、SubWorkflowSession 均实现此接口。
// ProxySession 通过委托模式实现此接口。
//
// Python 中 Checkpointer/Storage 方法签名直接接受 BaseSession，
// Go 同样使用 BaseSession 作为 Checkpointer/Storage 方法的 session 参数类型，
// 不再需要独立的 CheckpointerSession 子集接口（循环依赖已通过 interfaces 包解决）。
//
// 子类型独有的方法通过 *Provider 接口 + 类型断言获取，
// 对齐 Python hasattr/isinstance 运行时探测模式：
//
//	AgentID      → AgentIDProvider       (Python: hasattr(session, "agent_id"))
//	TeamID       → TeamIDProvider         (Python: hasattr(session, "team_id"))
//	WorkflowID   → WorkflowIDProvider     (Python: hasattr(session, "workflow_id"))
//	Parent       → ParentProvider         (Python: isinstance(session.parent(), AgentSession))
//	ExecutableID → ExecutableIDProvider   (Python: hasattr(session, "executable_id"))
type BaseSession interface {
	// Config 获取会话配置
	// ⤵️ 5.12 回填：返回类型从 any 改为 SessionConfig
	Config() any
	// State 获取会话状态
	State() state.SessionState
	// Tracer 获取会话追踪器
	// ⤵️ 5.11 回填：返回类型从 any 改为 Tracer
	Tracer() any
	// StreamWriterManager 获取流写入管理器
	// ⤵️ 5.10 回填：返回类型从 any 改为 StreamWriterManager
	StreamWriterManager() any
	// SessionID 获取会话唯一标识
	SessionID() string
	// Checkpointer 获取检查点管理器
	Checkpointer() Checkpointer
	// ActorManager 获取 Actor 管理器（可选，默认返回 nil）
	// ⤵️ 后续回填：返回类型从 any 改为 ActorManager
	ActorManager() any
	// Close 关闭会话，释放资源
	Close() error
}

// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
//
// Python 中所有方法签名接受 BaseSession，Go 同样使用 BaseSession。
// AgentID/TeamID/WorkflowID/Parent 等通过 *Provider 类型断言获取，
// 对齐 Python hasattr/isinstance 运行时探测模式。
type Checkpointer interface {
	// PreWorkflowExecute 工作流执行前
	PreWorkflowExecute(ctx context.Context, session BaseSession, inputs any) error
	// PostWorkflowExecute 工作流执行后
	PostWorkflowExecute(ctx context.Context, session BaseSession, result any, exception error) error
	// PreAgentExecute Agent 执行前
	PreAgentExecute(ctx context.Context, session BaseSession, inputs any) error
	// PreAgentTeamExecute AgentTeam 执行前
	PreAgentTeamExecute(ctx context.Context, session BaseSession, inputs any) error
	// InterruptAgentExecute Agent 中断时保存检查点
	InterruptAgentExecute(ctx context.Context, session BaseSession) error
	// PostAgentExecute Agent 执行后保存检查点
	PostAgentExecute(ctx context.Context, session BaseSession) error
	// PostAgentTeamExecute AgentTeam 执行后保存检查点
	PostAgentTeamExecute(ctx context.Context, session BaseSession) error
	// SessionExists 检查会话是否存在
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	// Release 释放会话资源。
	// agentID 可选参数：提供时仅释放指定 Agent 的检查点（支持多个，循环清除），否则释放整个 session。
	// 对齐 Python: release(session_id, agent_id=None)，Go 扩展支持批量 agentID。
	Release(ctx context.Context, sessionID string, agentID ...string) error
	// GraphStore 获取图状态存储
	// ⤵️ 8.7 回填：Graph Store 实现后返回 Store 实例
	GraphStore() any
}

// Storage 状态存储接口，负责单个实体的状态保存/恢复/清除。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Storage)
//
// Python 中所有方法签名接受 BaseSession，Go 同样使用 BaseSession。
type Storage interface {
	// Save 保存会话状态
	Save(ctx context.Context, session BaseSession) error
	// Recover 恢复会话状态
	Recover(ctx context.Context, session BaseSession, inputs any) error
	// Clear 清除会话数据
	// entityID 为实体标识（Agent 的 agentID / Workflow 的 workflowID）
	// sessionID 为会话标识，Persistence 版用于构建 KV key，InMemory 版忽略
	Clear(ctx context.Context, entityID, sessionID string) error
	// Exists 检查状态是否存在
	Exists(ctx context.Context, session BaseSession) (bool, error)
}

// AgentIDProvider 提供 Agent ID 的接口（通过类型断言获取）。
// AgentSession 天然满足此接口。
// 对应 Python: hasattr(session, "agent_id") 检测。
type AgentIDProvider interface {
	AgentID() string
}

// TeamIDProvider 提供 Team ID 的接口（通过类型断言获取）。
// AgentTeamSession 天然满足此接口。
// 对应 Python: hasattr(session, "team_id") 检测。
type TeamIDProvider interface {
	TeamID() string
}

// WorkflowIDProvider 提供 WorkflowID 的接口（通过类型断言获取）。
// WorkflowSession/NodeSession 天然满足此接口。
// 对应 Python: hasattr(session, "workflow_id") 检测。
type WorkflowIDProvider interface {
	WorkflowID() string
}

// ParentProvider 提供 Parent 的接口（通过类型断言获取）。
// WorkflowSession/NodeSession 天然满足此接口，AgentSession 不满足。
// 对应 Python: isinstance(session.parent(), AgentSession) 检测。
type ParentProvider interface {
	Parent() BaseSession
}

// CheckpointerConfigProvider 提供 GetEnv 方法的接口（通过类型断言获取）。
// 用于 session.Config() 返回 any 后，需要调用 GetEnv 的场景。
// ⤵️ 5.12 回填：Config() 返回 SessionConfig 后此接口可移除。
type CheckpointerConfigProvider interface {
	// GetEnv 获取环境变量值
	GetEnv(key string, defaultValue ...any) any
}

// ExecutableIDProvider 提供可执行路径 ID 的接口（通过类型断言获取）。
// NodeSession 天然满足此接口（有 ExecutableID() 方法），AgentSession 不满足。
// 通过类型断言延迟绑定：WorkflowInteraction/AgentInteraction 运行时断言获取 nodeID。
// 对应 Python: hasattr(session, "executable_id") 检测。
type ExecutableIDProvider interface {
	ExecutableID() string
}
