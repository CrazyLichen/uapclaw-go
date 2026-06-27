package modules

import (
	"context"
	"fmt"
	"sync"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	ability "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskExecutor 任务执行器接口。
// 对应 Python: TaskExecutor(ABC)
type TaskExecutor interface {
	// ExecuteAbility 执行任务，返回输出分片 channel。
	// channel 关闭表示执行结束。
	ExecuteAbility(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (<-chan *schema.ControllerOutputChunk, error)
	// CanPause 检查任务是否可暂停，返回 (是否可暂停, 原因)。
	CanPause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, string, error)
	// Pause 暂停任务，返回 (是否成功, 系统级错误)。
	Pause(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error)
	// CanCancel 检查任务是否可取消，返回 (是否可取消, 原因)。
	CanCancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, string, error)
	// Cancel 取消任务，返回 (是否成功, 系统级错误)。
	Cancel(ctx context.Context, taskID string, sess sessioninterfaces.SessionFacade) (bool, error)
}

// TaskExecutorDependencies 任务执行器依赖。
// 对齐 Python: TaskExecutorDependencies
type TaskExecutorDependencies struct {
	// Config 配置
	Config *config.ControllerConfig
	// AbilityMgr 能力管理器
	AbilityMgr *ability.AbilityManager
	// ContextEngine 上下文引擎
	ContextEngine iface.ContextEngine
	// TaskManager 任务管理器（同包前向引用）
	TaskManager *TaskManager
	// EventQueue 事件队列（同包前向引用）
	EventQueue *EventQueue
}

// TaskExecutorRegistry 任务执行器注册表。
// 对齐 Python: TaskExecutorRegistry
type TaskExecutorRegistry struct {
	// builders 任务执行器构建函数映射
	builders map[string]func(deps *TaskExecutorDependencies) TaskExecutor
	// mu 读写互斥锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskExecutorRegistry 创建新的任务执行器注册表。
func NewTaskExecutorRegistry() *TaskExecutorRegistry {
	return &TaskExecutorRegistry{
		builders: make(map[string]func(deps *TaskExecutorDependencies) TaskExecutor),
	}
}

// AddTaskExecutor 注册任务执行器构建函数。
func (r *TaskExecutorRegistry) AddTaskExecutor(taskType string, builder func(deps *TaskExecutorDependencies) TaskExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.builders[taskType] = builder
}

// RemoveTaskExecutor 移除任务执行器构建函数。
func (r *TaskExecutorRegistry) RemoveTaskExecutor(taskType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.builders, taskType)
}

// GetTaskExecutor 获取任务执行器实例。
// 未注册时返回 StatusAgentControllerTaskExecutionError 错误。
func (r *TaskExecutorRegistry) GetTaskExecutor(taskType string, deps *TaskExecutorDependencies) (TaskExecutor, error) {
	r.mu.RLock()
	builder, ok := r.builders[taskType]
	r.mu.RUnlock()

	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusAgentControllerTaskExecutionError,
			exception.WithMsg(fmt.Sprintf("未注册的任务执行器类型: %s", taskType)),
		)
	}
	return builder(deps), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
