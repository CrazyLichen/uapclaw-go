package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentAdapter Agent 适配器接口（swarm 侧定义）。
// 最小能力集，UapClawSwarm 门面仅依赖此接口驱动任意 SDK 后端，
// 不耦合其内部结构。
// Python: jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py (AgentAdapter)
//
// 会话分身的三方法（IsSessionScoped / MarkAsSessionScoped / GetLiveSessionInstance）
// 与 HandleSwarmflowReply / PendingInteractions 直接进本接口而非另立窄接口：
// 它们是"调用方用来驱动适配器"的能力面（规则 10），且 Python 的 AgentAdapter
// Protocol（agent_adapters.py:25）本就含后两者。会话子适配器缓存
// （sessionAdapterCache.entries）以本接口为元素类型，故根与 child 都是
// 同一个接口的实例，无需额外抽象层。
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
	// HasSessionRuntime 该会话是否还占用 adapter 侧运行时。
	// Python: interface_deep.py has_session_runtime（:4565）
	HasSessionRuntime(sessionID string) bool

	// CleanupSessionAdapter 清理该会话的 adapter 侧缓存，返回是否清到东西。
	// Python: interface_deep.py cleanup_session_adapter
	CleanupSessionAdapter(sessionID string) bool

	// Instance 返回底层 DeepAgent 实例，未初始化时返回 nil。
	// Python: JiuWenClaw.get_instance() → self._adapter._instance
	Instance() *harness.DeepAgent

	// SwitchMode 切换运行模式，执行完整的 session 生命周期。
	// 流程：preRun → switchMode → loadState → updateState → postRun
	// Python: jiuwenswarm/server/agent_ws_server.py:1145-1154
	SwitchMode(ctx context.Context, sessionID, subMode string) error

	// IsSessionScoped 本适配器是否为某会话的子适配器。
	// Python: interface_deep.py _is_session_scoped_adapter（:2110 定义，:2711 置真）
	IsSessionScoped() bool

	// MarkAsSessionScoped 把本适配器标记为指定会话的子适配器。
	// Python: interface_deep.py mark_as_session_scoped（:2711）
	MarkAsSessionScoped(sessionID string)

	// GetLiveSessionInstance 返回正在跑该会话的 DeepAgent；该会话未启动时返回 nil。
	// Python: interface_deep.py get_live_session_instance（:12744）
	GetLiveSessionInstance(sessionID string) *harness.DeepAgent

	// StartInteraction 绑定会话并启动交互循环（GoalManager 的唯一构造点）。
	// Python: interface_deep.py:15044 start_interaction
	StartInteraction(ctx context.Context, sessionID string) error

	// HasActiveGoal 该会话是否持有活跃 goal（只读缓存，不借不建 agent）。
	// Python: interface_deep.py:15450
	HasActiveGoal(sessionID string) bool

	// DispatchGoalControl 把协议字段映射到 Goal 能力方法。
	// Python: interface_deep.py:17744 dispatch_goal_control（分流头 :17775）
	DispatchGoalControl(
		ctx context.Context, sessionID, action, objective string,
		overwriteConfirmed bool, tokenBudget, maxAttempts int,
	) (map[string]any, error)

	// HandleSwarmflowReply 处理人对 swarmflow 人工轮次的回复。
	// Python: agent_adapters.py AgentAdapter.handle_swarmflow_reply（:86）
	HandleSwarmflowReply(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// PendingInteractions 会话被暂停的轮次仍在询问什么。
	// Python: agent_adapters.py AgentAdapter.pending_interactions（:94）
	PendingInteractions(sessionID string) ([]map[string]any, error)

	// EnsureInstance 确保本适配器自己的 DeepAgent 就绪并返回。
	// Python: interface_deep.py ensure_instance（:12809）
	//
	// 根适配器惰建时用它；调用方是"需要 DeepAgent 句柄的非 chat 路径"
	// （harness 打包、rail 开关、会话 fork）。
	// 注意与 EnsureLiveSessionInstance 是两个不同方法，不可互换（规则 10：
	// 二者都是 DeepAdapter 自身能力，必须进本接口而不是靠门面类型断言）。
	EnsureInstance(ctx context.Context) (*harness.DeepAgent, error)

	// EnsureLiveSessionInstance 确保该会话的子适配器已启动，返回其 DeepAgent。
	// Python: interface_deep.py ensure_live_session_instance（:12798）
	//
	// 与 EnsureInstance 的区别见上；plan 状态同步必须用这个。
	EnsureLiveSessionInstance(ctx context.Context, sessionID string) (*harness.DeepAgent, error)

	// PlanStateAccessor 返回 plan 状态读写入口。
	// Python: PlanModeController.open_state_session(agent, session_id) 的 Go 侧适配。
	//
	// 放在主接口而非匿名断言：CodeAdapter 是具名组合（deep *DeepAdapter），
	// 不写委托就拿不到该方法，用断言会静默退化为 nil —— 那会让 code 模式
	// 的 plan 状态读写整体失效（checkPlanExit 直接跳过）。
	PlanStateAccessor() swruntime.PlanStateAccessor
}

// ContextCompressor 上下文压缩可选接口。
// DeepAdapter 额外实现此接口，JiuWenClaw 门面通过类型断言调用。
// Python: JiuWenClawDeepAdapter.compress_context / get_context_usage / generate_recap
type ContextCompressor interface {
	// CompressContext 触发上下文压缩。
	// Python: JiuWenClawDeepAdapter.compress_context() (line 5380-5570)
	CompressContext(ctx context.Context, sessionID string, session sessioninterfaces.SessionFacade, returnState bool) (map[string]any, error)
	// GetContextUsage 获取上下文窗口占用率。
	// Python: JiuWenClawDeepAdapter.get_context_usage() (line 5572-5588)
	GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error)
	// GenerateRecap 生成会话回顾摘要。
	// Python: JiuWenClawDeepAdapter.generate_recap() (line 5590-5663)
	GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error)
}

// DreamingController Dreaming 启停可选接口。
// DeepAdapter 额外实现此接口，JiuWenClaw 门面通过类型断言调用。
// Python: JiuWenClawDeepAdapter.try_start_dreaming / try_stop_dreaming
type DreamingController interface {
	// TryStartDreaming 尝试启动 dreaming 进程。
	// Python: JiuWenClawDeepAdapter.try_start_dreaming() (line 5935-5954)
	TryStartDreaming(ctx context.Context, busyChecker func() bool) error
	// TryStopDreaming 停止 dreaming 进程。
	// Python: JiuWenClawDeepAdapter.try_stop_dreaming() (line 5956-5965)
	TryStopDreaming(ctx context.Context) error
}

// GatewayDisconnectHandler Gateway 断连处理可选接口。
// DeepAdapter 额外实现此接口，JiuWenClaw 门面通过类型断言调用。
// Python: JiuWenClawDeepAdapter.abort_on_gateway_disconnect
type GatewayDisconnectHandler interface {
	// AbortOnGatewayDisconnect Gateway 断连时全局中止。
	// Python: JiuWenClawDeepAdapter.abort_on_gateway_disconnect() (line 3539-3578)
	AbortOnGatewayDisconnect(ctx context.Context)
}
