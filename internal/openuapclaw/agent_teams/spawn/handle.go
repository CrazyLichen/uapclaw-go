package spawn

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"

	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
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
// ForkDeep 返回成员的内层 DeepAgent（25.5 fork 半区）：注入/压缩以具体类型
// 向下调用 harness——harness 不 import spawn，方向合法。nil = 非 in-process
// 成员（外部 CLI 等），fork 继承跳过。
// Python: agent_ref.resources.harness.get_deep_agent()（鸭子调用）。
type SpawnableAgent interface {
	// AgentCard 返回 Agent 身份卡片
	AgentCard() *agentschema.AgentCard
	// ForkDeep 返回内层 DeepAgent；非 in-process 实现返回 nil
	ForkDeep() *harness.DeepAgent
}
