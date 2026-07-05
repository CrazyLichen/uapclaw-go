package interfaces

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
)

// ──────────────────────────── 接口 ────────────────────────────

// DeepAgentInterface DeepAgent 运行时能力接口。
// 提供 Executor/Handler/Tool 所需的运行时访问。
// 对齐 Python: DeepAgent 的公开方法集（TYPE_CHECKING 引用）。
// ⤵️ 9.1 回填：9.1 实现 DeepAgent 后，由 *DeepAgent 直接实现此接口。
type DeepAgentInterface interface {
	// ReactAgent 返回内层 ReActAgent 实例
	ReactAgent() *agents.ReActAgent
	// LoopCoordinator 返回循环协调器（可能为 nil）
	LoopCoordinator() LoopCoordinatorInterface
	// LoopController 返回任务循环控制器（对齐 Python: DeepAgent.loop_controller）
	LoopController() controller.ControllerInterface
	// EventHandler 返回事件处理器
	EventHandler() modules.EventHandler
	// LoadState 从会话加载 DeepAgentState
	LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState
	// DeepConfig 返回 DeepAgent 配置
	DeepConfig() *hschema.DeepAgentConfig
	// IsInvokeActive 判断是否有活跃的 invoke
	IsInvokeActive() bool
	// IsAutoInvokeScheduled 判断是否已调度自动 invoke
	IsAutoInvokeScheduled() bool
	// SetAutoInvokeScheduled 设置自动 invoke 调度标记
	SetAutoInvokeScheduled(scheduled bool)
	// ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke
	// ⤵️ 9.1 回填：实现 SessionSpawn 完成后的自动 invoke 调度
	ScheduleAutoInvokeOnSpawnDone(steerText string) error
	// CreateSubagent 创建子 Agent 实例。
	// ⤵️ 9.1 回填：9.1 实现 DeepAgent 后，由 *DeepAgent.CreateSubagent 实现。
	// 对齐 Python: DeepAgent.create_subagent
	CreateSubagent(subagentType string, subSessionID string) (DeepAgentInterface, error)
}

// LoopCoordinatorInterface 循环协调器接口（最小集）。
// 仅包含 DeepAgentInterface 消费者实际调用的方法。
// 对齐 Python: LoopCoordinator 的消费者方法子集。
type LoopCoordinatorInterface interface {
	// Iteration 返回当前迭代次数
	Iteration() int
	// RequestAbort 请求中止循环
	RequestAbort()
}
