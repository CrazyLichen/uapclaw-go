package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentAdapter Agent 适配器接口（swarm 侧定义）。
//
// 最小能力集，UapClaw 门面仅依赖此接口驱动任意 SDK 后端，
// 不耦合其内部结构。
//
// 对应 Python: jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py (AgentAdapter)
type AgentAdapter interface {
	// CreateInstance 初始化底层 SDK Agent。
	// 启动时调用一次，skill install/uninstall 后再次调用。
	CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error

	// ReloadAgentConfig 热重载配置，不重启进程。
	// configBase: 完整配置快照，若提供则不再读 config.yaml。
	// envOverrides: 环境变量覆盖，仅覆盖请求中存在的键。
	ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error

	// ProcessMessageImpl 执行非流式请求，返回完整响应。
	// inputs: 预构建的输入字典，含 conversation_id/query/channel 等。
	ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error)

	// ProcessMessageStreamImpl 执行流式请求，通过 channel 返回响应块。
	// 返回的 channel 由适配器关闭（发送终止哨兵后 close）。
	// inputs: 预构建的输入字典，含 conversation_id/query/channel 等。
	ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error)

	// ProcessInterrupt 处理中断请求（pause/resume/cancel/supplement）。
	ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// HandleUserAnswer 处理用户回答（evolution 审批或权限审批）。
	HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// HandleHeartbeat 处理心跳请求。
	// 返回 nil 表示非心跳请求，继续正常流程；
	// 返回非 nil 表示心跳已处理，上层应短路。
	HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// Cleanup 清理适配器资源。
	// Python 中不在 Protocol 里但门面会调用，Go 纳入接口更规范，避免运行时类型断言。
	Cleanup() error
}
