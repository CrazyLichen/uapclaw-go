package interfaces

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 接口 ────────────────────────────

// DeepAgentInterface DeepAgent 运行时能力接口。
// 提供 Executor/Handler/Tool 所需的运行时访问。
// 对齐 Python: DeepAgent 的公开方法集（TYPE_CHECKING 引用）。
// 由 *DeepAgent 直接实现此接口。
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
	// ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke。
	// 对齐 Python: DeepAgent.schedule_auto_invoke_on_spawn_done(query, delay=0.5)
	ScheduleAutoInvokeOnSpawnDone(steerText string, delay float64) error
	// CreateSubagent 创建子 Agent 实例。
	// 对齐 Python: DeepAgent.create_subagent
	CreateSubagent(subagentType string, subSessionID string) (DeepAgentInterface, error)
	// Invoke 执行 Agent：若 enable_task_loop=true 走完整多轮循环，否则走单轮 ReAct。
	// 对齐 Python: DeepAgent.invoke
	Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
	// SwitchMode 切换当前会话模式（normal/plan）。
	// 对齐 Python: DeepAgent.switch_mode
	SwitchMode(sess sessioninterfaces.SessionFacade, mode string)
	// RestoreModeAfterPlanExit 退出 plan 模式后恢复之前模式。
	// 对齐 Python: DeepAgent.restore_mode_after_plan_exit
	RestoreModeAfterPlanExit(sess sessioninterfaces.SessionFacade)
	// GetPlanFilePath 返回当前 plan 文件路径（空字符串表示无 plan）。
	// 对齐 Python: DeepAgent.get_plan_file_path
	GetPlanFilePath(sess sessioninterfaces.SessionFacade) string
	// SaveState 保存 DeepAgentState 到会话。
	// 对齐 Python: DeepAgent.save_state
	SaveState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState)
}

// LoopCoordinatorInterface 循环协调器接口（最小集）。
// 仅包含 DeepAgentInterface 消费者实际调用的方法。
// 对齐 Python: LoopCoordinator 的消费者方法子集。
type LoopCoordinatorInterface interface {
	// Iteration 返回当前迭代次数
	Iteration() int
	// RequestAbort 请求中止循环
	RequestAbort()
	// GetCompletionPromiseEvaluator 返回第一个 CompletionPromiseEvaluator（可能为 nil）
	// 对齐 Python: LoopCoordinator.get_completion_promise_evaluator
	GetCompletionPromiseEvaluator() CompletionPromiseEvaluatorInterface
}

// CompletionPromiseEvaluatorInterface 完成承诺评估器接口（最小集）。
// 仅包含 TaskCompletionRail.notifyEvaluator 实际调用的方法。
// 对齐 Python: CompletionPromiseEvaluator 的消费者方法子集。
type CompletionPromiseEvaluatorInterface interface {
	// NotifyFulfilled 标记 promise 已满足
	// 对齐 Python: CompletionPromiseEvaluator.notify_fulfilled
	NotifyFulfilled(matchedText string)
}
