package controller

import (
	"context"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// ControllerInterface 控制器接口，定义事件驱动任务编排的核心能力。
// Controller 和 TaskLoopController 均实现此接口。
// 对齐 Python: openjiuwen/core/controller/base.py::Controller 的公开方法
type ControllerInterface interface {
	// Init 两阶段初始化
	Init(card *agentschema.AgentCard, cfg *config.ControllerConfig,
		abilityMgr agentinterfaces.AbilityManagerInterface,
		contextEngine iface.ContextEngine)
	// Start 启动控制器
	Start(ctx context.Context) error
	// Stop 停止控制器
	Stop(ctx context.Context) error
	// Invoke 批量执行
	Invoke(ctx context.Context, inputs *schema.InputEvent, sess *session.Session) (*schema.ControllerOutput, error)
	// Stream 流式执行
	Stream(ctx context.Context, inputs *schema.InputEvent, sess *session.Session,
		streamModes []stream.StreamMode) (<-chan *stream.OutputSchema, <-chan error)
	// PublishEventAsync 异步发布事件（fire-and-forget）
	PublishEventAsync(ctx context.Context, sess *session.Session, event schema.Event) error
	// SetEventHandler 设置事件处理器
	SetEventHandler(handler modules.EventHandler)
	// AddTaskExecutor 注册任务执行器（链式调用返回 ControllerInterface）
	AddTaskExecutor(taskType string, builder func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor) ControllerInterface
	// BindSession 绑定 session
	BindSession(ctx context.Context, sess *session.Session) error
	// UnbindSession 解绑 session
	UnbindSession(ctx context.Context, sess *session.Session) error
	// Config 获取配置
	Config() *config.ControllerConfig
	// EventHandler 获取事件处理器
	EventHandler() modules.EventHandler
}
