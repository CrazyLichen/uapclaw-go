package spawn

import (
	"context"
	"time"

	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnHandle 统一 inprocess 和 subprocess 的操作接口。
// 不含回调设置——回调在构造句柄时注入。
// Python: SpawnedProcessHandle / InProcessSpawnHandle 的公共方法集。
type SpawnHandle interface {
	// ProcessID 返回进程唯一标识
	ProcessID() string
	// IsAlive 检查进程/任务是否仍在运行
	IsAlive() bool
	// IsHealthy 检查进程/任务是否健康（健康且存活）
	IsHealthy() bool
	// Shutdown 优雅关闭，返回是否在超时内完成
	Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error)
	// ForceKill 强制终止
	ForceKill() error
	// WaitForCompletion 等待完成，0=成功，-1=异常
	WaitForCompletion() (int, error)
	// StartHealthCheck 启动健康检查后台任务
	StartHealthCheck(ctx context.Context, interval ...time.Duration) error
	// StopHealthCheck 停止健康检查后台任务
	StopHealthCheck() error
}

// SpawnableAgent 进程内生成的 Agent 最小接口。
// 仅暴露 InProcessSpawnHandle 消费者所需操作，不包含运行方法——运行由 Runner 层负责。
// Python: InProcessSpawnHandle.agent_ref 的 Any 类型，
// Go 中用接口替代以保留最小类型安全，同时避免 spawn/ 包 import agent/ 包。
type SpawnableAgent interface {
	// AgentCard 返回 Agent 身份卡片
	AgentCard() *agentschema.AgentCard
}

// ForkContextSource fork 继承的源上下文读取面。
// Python agent_ref 的 resources.harness.get_deep_agent().get_current_context(...)
// 窄化——spawn 包不 import harness 包，源读取经此窄接口（依赖倒置）。
// Python: _resolve_fork_native (team_agent.py:1644-1661) 的读取半区。
type ForkContextSource interface {
	// GetCurrentContext 返回当前上下文消息
	GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error)
}

// ForkTarget fork 继承的注入目标写入面（新成员 context engine 创建）。
// Python: native.create_new_context_engine(session_id=..., messages=...)
// （inprocess_spawn.py:105-108）。
type ForkTarget interface {
	// CreateNewContextEngine 以 messages 为种子创建新的上下文引擎
	CreateNewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error)
}
