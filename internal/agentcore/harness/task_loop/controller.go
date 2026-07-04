package task_loop

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// interactionQueuesProvider 类型断言接口，用于从 EventHandler 获取 LoopQueues。
// 对齐 Python: getattr(handler, "interaction_queues", None)
// 只有 TaskLoopEventHandler（9.6）实现此接口，其他 EventHandler 不实现。
type interactionQueuesProvider interface {
	InteractionQueues() *LoopQueues
}

// ──────────────────────────── 结构体 ────────────────────────────

// TaskLoopController 任务循环控制器，嵌入 Controller 并扩展轮次管理能力。
// 封装轮次提交/等待/完成、follow-up 队列操作和循环退出逻辑，
// 是 DeepAgent 外层循环的"方向盘"。
// 对齐 Python: TaskLoopController(Controller)
type TaskLoopController struct {
	*controller.Controller
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskLoopController 创建任务循环控制器。
// 必须随后调用 Init() 完成初始化（与 Controller 相同）。
// 对齐 Python: TaskLoopController.__init__
func NewTaskLoopController() *TaskLoopController {
	return &TaskLoopController{
		Controller: controller.NewController(),
	}
}

// SubmitRound 提交一轮任务：prepare_round → 构建 InputEvent → 注入元数据 → 发布。
// runKind 为运行模式（normal/heartbeat/cron），零值空串表示未设置。
// runContext 为结构化运行时上下文（心跳等场景），nil 表示无上下文。
// 对齐 Python: TaskLoopController.submit_round
func (tc *TaskLoopController) SubmitRound(
	ctx context.Context,
	sess *session.Session,
	query string,
	isFollowUp bool,
	runKind rail.RunKind,
	runContext *rail.RunContext,
) error {
	handler := tc.EventHandler()
	if handler == nil {
		logger.Error(logComponent).Msg("SubmitRound: EventHandler 为 nil")
		return nil
	}

	roundID := handler.PrepareRound()

	event, err := schema.FromUserInput(query)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("query", query).Msg("SubmitRound: 构建 InputEvent 失败")
		return err
	}

	meta := event.GetMetadata()
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["_handler_round_id"] = roundID
	if isFollowUp {
		meta["is_follow_up"] = true
	}
	if runKind != "" {
		meta["run_kind"] = string(runKind)
	}
	if runContext != nil {
		meta["run_context"] = runContext
	}
	event.SetMetadata(meta)

	logger.Info(logComponent).
		Int("round_id", roundID).
		Bool("is_follow_up", isFollowUp).
		Str("run_kind", string(runKind)).
		Msg("提交任务轮次")

	return tc.PublishEventAsync(ctx, sess, event)
}

// WaitRoundCompletion 等待当前轮次完成。
// timeout 为超时时间（秒），nil 表示不超时。
// 对齐 Python: TaskLoopController.wait_round_completion
func (tc *TaskLoopController) WaitRoundCompletion(ctx context.Context, timeout *float64) map[string]any {
	handler := tc.EventHandler()
	if handler == nil {
		logger.Warn(logComponent).Msg("WaitRoundCompletion: EventHandler 为 nil，返回空结果")
		return nil
	}

	var d time.Duration
	if timeout != nil && *timeout > 0 {
		d = time.Duration(*timeout * float64(time.Second))
	}

	return handler.WaitCompletion(ctx, d)
}

// DrainFollowUp 排空 follow-up 消息。
// 对齐 Python: TaskLoopController.drain_follow_up
func (tc *TaskLoopController) DrainFollowUp() []string {
	queues := tc.getInteractionQueues()
	if queues != nil {
		return queues.DrainFollowUp()
	}
	return nil
}

// EnqueueFollowUp 入队 follow-up 消息（Rails 用于请求继续/确认轮次）。
// 对齐 Python: TaskLoopController.enqueue_follow_up
func (tc *TaskLoopController) EnqueueFollowUp(msg string) {
	queues := tc.getInteractionQueues()
	if queues != nil {
		queues.PushFollowUp(msg)
		return
	}
	logger.Warn(logComponent).Str("msg", msg).Msg("EnqueueFollowUp: 无 InteractionQueues，消息丢弃")
}

// HasFollowUp 检查是否有待处理的 follow-up 消息。
// 对齐 Python: TaskLoopController.has_follow_up
func (tc *TaskLoopController) HasFollowUp() bool {
	queues := tc.getInteractionQueues()
	if queues != nil {
		return queues.HasFollowUp()
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getInteractionQueues 从 EventHandler 防御性获取 LoopQueues。
// 使用类型断言对齐 Python getattr(handler, "interaction_queues", None) 语义。
// 只有实现了 interactionQueuesProvider 接口的 EventHandler 才能返回非 nil。
// 对齐 Python: TaskLoopController._get_interaction_queues
func (tc *TaskLoopController) getInteractionQueues() *LoopQueues {
	handler := tc.EventHandler()
	if handler == nil {
		return nil
	}
	provider, ok := handler.(interactionQueuesProvider)
	if !ok {
		return nil
	}
	return provider.InteractionQueues()
}

// 确保 TaskLoopController 满足 ControllerInterface
var _ controller.ControllerInterface = (*TaskLoopController)(nil)
