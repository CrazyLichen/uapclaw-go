package skilldev

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StageHandler SkillDev Pipeline 阶段处理器接口。
//
// 每个阶段独立实现，通过 Execute() 与 Pipeline 交互。
// 处理器不应持有跨请求的状态——所有状态均通过 SkillDevContext 传入。
type StageHandler interface {
	// Execute 执行阶段逻辑。
	//
	// 参数：
	//   - ctx:  上下文（控制生命周期）
	//   - sctx: SkillDevContext，包含 state、workspace、emit、create_stage_agent 等
	//
	// 返回值：
	//   - StageResult：Pipeline 据此跳转到下一阶段
	//   - error：执行错误
	Execute(ctx context.Context, sctx *SkillDevContext) (*StageResult, error)
}

// SkillDevContext 阶段执行上下文。
//
// Context 不是 Agent，它是每阶段 StageHandler 的运行环境：
//   - 持有 deps（外部依赖）和 state（运行时状态）的引用
//   - 提供 Emit() 向前端推送事件
//   - 提供 CreateStageAgent() 为当前阶段创建隔离的 ReActAgent
//
// 每阶段独立 Agent 的核心价值：
//   - 工具隔离：PLAN 只有搜索，GENERATE 才有文件写入
//   - Prompt 隔离：每阶段有焦点明确的专属 system prompt
//   - 内存隔离：阶段结束 Agent 即释放，无残留上下文
type SkillDevContext struct {
	// TaskID 任务标识
	TaskID string
	// Deps 外部依赖
	Deps *SkillDevDeps
	// State 运行时状态
	State *SkillDevState
	// Workspace 工作区路径
	Workspace string
	// eventQueue 事件队列（向 Pipeline 推送事件）
	eventQueue chan<- SkillDevEvent
}

// StageResult 阶段执行结果，由 Pipeline 读取以驱动状态跳转。
type StageResult struct {
	// NextStage 下一个跳转阶段
	NextStage SkillDevStage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillDevContext 创建新的阶段执行上下文。
func NewSkillDevContext(
	taskID string,
	deps *SkillDevDeps,
	state *SkillDevState,
	workspace string,
	eventQueue chan<- SkillDevEvent,
) *SkillDevContext {
	return &SkillDevContext{
		TaskID:     taskID,
		Deps:       deps,
		State:      state,
		Workspace:  workspace,
		eventQueue: eventQueue,
	}
}

// Emit 向前端推送一个事件（放入 Pipeline 的事件队列）。
func (c *SkillDevContext) Emit(eventType SkillDevEventType, payload map[string]any) {
	merged := make(map[string]any, len(payload)+1)
	merged["task_id"] = c.TaskID
	for k, v := range payload {
		merged[k] = v
	}
	event := SkillDevEvent{
		EventType: eventType,
		Payload:   merged,
		TaskID:    c.TaskID,
	}
	select {
	case c.eventQueue <- event:
	default:
		logger.Warn(logComponent).
			Str("task_id", c.TaskID).
			Str("event_type", string(eventType)).
			Msg("[SkillDevContext] 事件队列已满，丢弃事件")
	}
}

// CreateStageAgent 为当前阶段创建隔离的 ReActAgent。
//
// 参数：
//   - stageName:     阶段标识，用于 agent 命名（调试/日志用）
//   - systemPrompt:  该阶段专属的 system prompt
//   - tools:         工具名白名单，如 ["file_read", "file_write", "web_search"]
//   - maxIterations: ReAct 最大循环次数
//
// 返回：配置完毕的 ReActAgent 实例（尚未执行）
//
// 待实现: 接入 openjiuwen ReActAgent 的实际构造逻辑，参考 UapClaw.create_instance()
func (c *SkillDevContext) CreateStageAgent(
	stageName string,
	systemPrompt string,
	tools []string,
	maxIterations int,
) (any, error) {
	logger.Info(logComponent).
		Str("stage", stageName).
		Strs("tools", tools).
		Int("max_iterations", maxIterations).
		Msg("[SkillDevContext] create_stage_agent")
	return nil, fmt.Errorf("create_stage_agent 尚未接入 openjiuwen，待实现")
}

// RegisterTools 根据工具名白名单将工具注册到 Agent。
//
// 待实现: 接入实际工具注册逻辑
func (c *SkillDevContext) RegisterTools(_ any, _ []string) error {
	return fmt.Errorf("_register_tools 尚未实现")
}
