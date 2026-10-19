package interfaces

import (
	"context"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgentInterface DeepAgent 运行时能力接口。
// 提供 Executor/Handler/Tool 所需的运行时访问。
// Python: DeepAgent 的公开方法集（TYPE_CHECKING 引用）。
// 由 *DeepAgent 直接实现此接口。
type DeepAgentInterface interface {
	// ReactAgent 返回内层 ReActAgent 实例
	ReactAgent() *agents.ReActAgent
	// Card 返回 Agent 身份卡片。
	// Python: DeepAgent.card 属性
	Card() *agentschema.AgentCard
	// AbilityManager 返回能力管理器。
	// Python: DeepAgent.ability_manager 属性（BaseAgent 扩展）
	AbilityManager() agentinterfaces.AbilityManagerInterface
	// SystemPromptBuilder 返回系统提示词构建器。
	// Python: DeepAgent.system_prompt_builder 属性（harness 层扩展）
	SystemPromptBuilder() saprompt.SystemPromptBuilderInterface
	// UapswarmProjectDir 返回项目目录。
	// Python: DeepAgent._jiuwenswarm_project_dir
	UapswarmProjectDir() string
	// SetUapswarmProjectDir 设置项目目录。
	// Python: setattr(instance, "_jiuwenswarm_project_dir", value)
	SetUapswarmProjectDir(dir string)
	// UapswarmCodeProjectDir 返回代码项目目录。
	// Python: DeepAgent._jiuwenswarm_code_project_dir
	UapswarmCodeProjectDir() string
	// SetUapswarmCodeProjectDir 设置代码项目目录。
	// Python: setattr(instance, "_jiuwenswarm_code_project_dir", value)
	SetUapswarmCodeProjectDir(dir string)
	// UapswarmAdapterMode 返回适配器模式。
	// Python: DeepAgent._jiuwenswarm_adapter_mode
	UapswarmAdapterMode() string
	// SetUapswarmAdapterMode 设置适配器模式。
	// Python: setattr(instance, "_jiuwenswarm_adapter_mode", value)
	SetUapswarmAdapterMode(mode string)
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
	// Python: DeepAgent.schedule_auto_invoke_on_spawn_done(query, delay=0.5)
	ScheduleAutoInvokeOnSpawnDone(ctx context.Context, steerText string, delay float64) error
	// CreateSubagent 创建子 Agent 实例。
	// Python: DeepAgent.create_subagent
	CreateSubagent(ctx context.Context, subagentType string, subSessionID string) (DeepAgentInterface, error)
	// Invoke 执行 Agent：若 enable_task_loop=true 走完整多轮循环，否则走单轮 ReAct。
	// Python: DeepAgent.invoke
	Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
	// SwitchMode 切换当前会话模式（normal/plan）。
	// Python: DeepAgent.switch_mode
	SwitchMode(sess sessioninterfaces.SessionFacade, mode string)
	// RestoreModeAfterPlanExit 退出 plan 模式后恢复之前模式。
	// Python: DeepAgent.restore_mode_after_plan_exit
	RestoreModeAfterPlanExit(sess sessioninterfaces.SessionFacade)
	// GetPlanFilePath 返回当前 plan 文件路径（空字符串表示无 plan）。
	// Python: DeepAgent.get_plan_file_path
	GetPlanFilePath(sess sessioninterfaces.SessionFacade) string
	// SaveState 保存 DeepAgentState 到会话。
	// Python: DeepAgent.save_state
	SaveState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState)
	// FindRailsByType 返回排队和已注册中匹配指定类型的 Rail。
	// Python: DeepAgent.find_rails_by_type(rail_types)
	FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail
	// AddRail 同步排队一个 Rail 以便延迟注册。
	// Python: DeepAgent.add_rail(rail)
	// M-02: 补齐接口面（customizer 回填后 team 层需通过接口调用 agent.add_rail）
	AddRail(r agentinterfaces.AgentRail)
	// UnregisterRail 注销 Rail。
	// Python: DeepAgent.unregister_rail(rail)
	UnregisterRail(ctx context.Context, r agentinterfaces.AgentRail) error
	// InnerInvokeOverride 返回内层 invoke 覆写函数（仅测试使用）。
	// 在 TaskLoop 模式下，executor 调用此函数优先于 reactAgent.Invoke。
	// Python: 测试中通过 agent.set_react_agent(fake_react) 替换内层 Agent。
	InnerInvokeOverride() func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error)
}

// LoopCoordinatorInterface 循环协调器接口（最小集）。
// 仅包含 DeepAgentInterface 消费者实际调用的方法。
// Python: LoopCoordinator 的消费者方法子集。
type LoopCoordinatorInterface interface {
	// Iteration 返回当前迭代次数
	Iteration() int
	// RequestAbort 请求中止循环
	RequestAbort()
	// GetCompletionPromiseEvaluator 返回第一个 CompletionPromiseEvaluator（可能为 nil）
	// Python: LoopCoordinator.get_completion_promise_evaluator
	GetCompletionPromiseEvaluator() CompletionPromiseEvaluatorInterface
}

// CompletionPromiseEvaluatorInterface 完成承诺评估器接口（最小集）。
// 仅包含 TaskCompletionRail.notifyEvaluator 实际调用的方法。
// Python: CompletionPromiseEvaluator 的消费者方法子集。
type CompletionPromiseEvaluatorInterface interface {
	// NotifyFulfilled 标记 promise 已满足
	// Python: CompletionPromiseEvaluator.notify_fulfilled
	NotifyFulfilled(matchedText string)
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
