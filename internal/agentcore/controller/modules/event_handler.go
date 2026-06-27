package modules

import (
	"context"
	"time"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventHandler 事件处理器接口。
// 对齐 Python EventHandler ABC 的抽象方法。
type EventHandler interface {
	// HandleInput 处理输入事件
	HandleInput(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
	// HandleTaskInteraction 处理任务交互事件
	HandleTaskInteraction(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
	// HandleTaskCompletion 处理任务完成事件
	HandleTaskCompletion(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
	// HandleTaskFailed 处理任务失败事件
	HandleTaskFailed(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
	// HandleFollowUp 处理跟进事件
	HandleFollowUp(ctx context.Context, input *EventHandlerInput) (map[string]any, error)
	// GetBase 获取基础依赖容器
	GetBase() *EventHandlerBase
	// PrepareRound 准备轮次，返回轮次编号
	PrepareRound() int
	// WaitCompletion 等待完成，返回结果映射
	WaitCompletion(ctx context.Context, timeout time.Duration) map[string]any
	// OnAbort 中止回调
	OnAbort()
}

// EventHandlerBase 依赖容器 + 默认实现。
// 对齐 Python EventHandler ABC 的 self._config 等属性 + 非抽象方法的默认实现。
type EventHandlerBase struct {
	// Config 配置
	Config *config.ControllerConfig
	// ContextEngine 上下文引擎
	ContextEngine iface.ContextEngine
	// TaskManager 任务管理器（同包内类型，前向引用）
	TaskManager *TaskManager
	// TaskScheduler 任务调度器（同包内类型，前向引用）
	TaskScheduler *TaskScheduler
	// AbilityMgr 能力管理器
	AbilityMgr *ability.AbilityManager
}

// EventHandlerInput 事件处理器输入参数。
type EventHandlerInput struct {
	// Event 事件
	Event schema.Event
	// Session 会话门面
	Session sessioninterfaces.SessionFacade
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleFollowUp 默认实现：返回 not_supported 状态。
// 对齐 Python EventHandler.HandleFollowUp 默认实现。
func (b *EventHandlerBase) HandleFollowUp(_ context.Context, _ *EventHandlerInput) (map[string]any, error) {
	return map[string]any{"status": "not_supported"}, nil
}

// GetBase 返回 EventHandlerBase 自身。
func (b *EventHandlerBase) GetBase() *EventHandlerBase {
	return b
}

// PrepareRound 默认实现：返回 0。
// 对齐 Python EventHandler.PrepareRound 默认实现。
func (b *EventHandlerBase) PrepareRound() int {
	return 0
}

// WaitCompletion 默认实现：直接返回 completed 状态。
// 对齐 Python EventHandler.WaitCompletion 默认实现。
func (b *EventHandlerBase) WaitCompletion(_ context.Context, _ time.Duration) map[string]any {
	return map[string]any{"status": "completed"}
}

// OnAbort 默认实现：空操作。
// 对齐 Python EventHandler.OnAbort 默认实现。
func (b *EventHandlerBase) OnAbort() {}

// ──────────────────────────── 非导出函数 ────────────────────────────
