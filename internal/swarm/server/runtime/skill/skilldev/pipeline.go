package skilldev

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevPipeline 确定性状态机编排器。
//
// Pipeline 是整个 SkillDev 流程的骨架：
//   - 维护阶段跳转顺序（stageHandlers 注册表）
//   - 在挂起点（PLAN_CONFIRM / REVIEW / DESC_OPTIMIZE_CONFIRM）checkpoint 并暂停
//   - 提供 Run() 和 Resume() 两个执行入口
//   - 每次请求创建、执行到挂起点/完成后释放（不长驻内存）
//
// Pipeline 不关心"怎么做"，只关心"做什么顺序"。
// 具体逻辑全部委托给各阶段的 StageHandler.Execute()。
//
// 对应 Python: jiuwenswarm/server/runtime/skill/skilldev/pipeline.py (SkillDevPipeline)
type SkillDevPipeline struct {
	// TaskID 任务标识
	TaskID string
	// State 运行时状态
	State *SkillDevState
	// deps 外部依赖
	deps *SkillDevDeps
	// events 事件收集切片（替代 Python asyncio.Queue）
	events []SkillDevEvent
}

// ──────────────────────────── 全局变量 ────────────────────────────

// stageHandlers 阶段→Handler 映射，对应 Python STAGE_HANDLERS。
//
// PLAN_CONFIRM / REVIEW / DESC_OPTIMIZE_CONFIRM 是挂起点，
// 由 SuspensionPoints 处理，不在此注册。
// 通过 RegisterStageHandler 注册各阶段处理器，避免循环依赖。
var stageHandlers = map[SkillDevStage]StageHandler{}

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterStageHandler 注册阶段处理器。
//
// 由 stages 包的 init() 调用，将各阶段处理器注册到全局映射。
// 此设计打破 skilldev → stages 的编译时依赖，消除循环导入。
func RegisterStageHandler(stage SkillDevStage, handler StageHandler) {
	stageHandlers[stage] = handler
}

// NewSkillDevPipeline 创建新的 Pipeline 实例。
func NewSkillDevPipeline(taskID string, state *SkillDevState, deps *SkillDevDeps) *SkillDevPipeline {
	return &SkillDevPipeline{
		TaskID: taskID,
		State:  state,
		deps:   deps,
		events: make([]SkillDevEvent, 0),
	}
}

// Run 从当前阶段开始执行，直到遇到挂起点或终态。
//
// 对齐 Python: SkillDevPipeline.run() → AsyncIterator[SkillDevEvent]
// Go 中改为返回 ([]SkillDevEvent, error)，因为 Go 没有 yield。
func (p *SkillDevPipeline) Run(ctx context.Context) ([]SkillDevEvent, error) {
	for p.State.Stage != SkillDevStageCompleted && p.State.Stage != SkillDevStageError {
		// 命中挂起点：推送确认请求 → checkpoint → 暂停
		if suspension, ok := SuspensionPoints[p.State.Stage]; ok {
			p.emit(SkillDevEventTypeTodosUpdate, map[string]any{
				"todos": ComputeTodos(p.State.Stage, &p.State.Mode),
			})
			p.emit(SkillDevEventTypeConfirmRequest, map[string]any{
				"confirm_type": suspension.ConfirmType,
				"title":        suspension.Title,
				"message":      suspension.Message,
				"data":         suspension.ExtractData(p.State),
				"actions":      suspension.Actions,
			})
			if err := p.checkpoint(); err != nil {
				return p.events, fmt.Errorf("checkpoint 失败: %w", err)
			}
			break
		}

		// 执行当前阶段
		handler, ok := stageHandlers[p.State.Stage]
		if !ok {
			return p.events, fmt.Errorf("阶段 %s 没有对应的处理器", p.State.Stage)
		}

		workspace, err := p.deps.WorkspaceProvider.EnsureLocal(p.TaskID)
		if err != nil {
			p.State.Stage = SkillDevStageError
			errMsg := fmt.Sprintf("确保工作区失败: %s", err.Error())
			p.State.Error = &errMsg
			p.emit(SkillDevEventTypeError, map[string]any{"message": errMsg})
			_ = p.checkpoint()
			return p.events, err
		}

		eventCh := make(chan SkillDevEvent, 64)
		sctx := NewSkillDevContext(p.TaskID, p.deps, p.State, workspace, eventCh)

		p.emit(SkillDevEventTypeStageChanged, map[string]any{
			"stage":     string(p.State.Stage),
			"iteration": p.State.Iteration,
		})
		p.emit(SkillDevEventTypeTodosUpdate, map[string]any{
			"todos": ComputeTodos(p.State.Stage, &p.State.Mode),
		})

		// 启动事件转发 goroutine，将 Context 的事件收集到 Pipeline
		done := make(chan struct{})
		go func() {
			defer close(done)
			for evt := range eventCh {
				p.events = append(p.events, evt)
			}
		}()

		result, execErr := handler.Execute(ctx, sctx)

		// 关闭 eventCh，等待转发 goroutine 结束
		close(eventCh)
		<-done

		if execErr != nil {
			logger.Error(logComponent).
				Str("task_id", p.TaskID).
				Str("stage", string(p.State.Stage)).
				Err(execErr).
				Msg("[Pipeline] 阶段执行失败")
			p.State.Stage = SkillDevStageError
			errMsg := execErr.Error()
			p.State.Error = &errMsg
			p.emit(SkillDevEventTypeError, map[string]any{"message": errMsg})
			_ = p.checkpoint()
			break
		}

		p.State.Stage = result.NextStage
		if err := p.checkpoint(); err != nil {
			return p.events, fmt.Errorf("checkpoint 失败: %w", err)
		}
	}

	return p.events, nil
}

// Resume 从挂起点恢复执行。
//
// 对齐 Python: SkillDevPipeline.resume(data) → AsyncIterator[SkillDevEvent]
// Go 中改为返回 ([]SkillDevEvent, error)。
func (p *SkillDevPipeline) Resume(ctx context.Context, data map[string]any) ([]SkillDevEvent, error) {
	currentStage := p.State.Stage
	suspension, ok := SuspensionPoints[currentStage]
	if !ok {
		return nil, fmt.Errorf("阶段 %s 不是挂起点，无法调用 Resume()", currentStage)
	}

	// 调用 OnResume 更新状态（写入用户确认的 plan / 反馈）
	suspension.OnResume(p.State, data)

	// 计算下一阶段（REVIEW 阶段的 next_stage 是函数，根据 action 动态决定）
	p.State.Stage = suspension.GetNextStage(data)

	// 继续执行 Run
	return p.Run(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emit 向事件切片追加一个事件。
//
// 对齐 Python: SkillDevPipeline._emit()
func (p *SkillDevPipeline) emit(eventType SkillDevEventType, payload map[string]any) {
	merged := make(map[string]any, len(payload)+1)
	merged["task_id"] = p.TaskID
	for k, v := range payload {
		merged[k] = v
	}
	p.events = append(p.events, SkillDevEvent{
		EventType: eventType,
		Payload:   merged,
		TaskID:    p.TaskID,
	})
}

// checkpoint 阶段边界：持久化状态 + 同步工作区文件。
//
// 对齐 Python: SkillDevPipeline._checkpoint()
func (p *SkillDevPipeline) checkpoint() error {
	if err := p.deps.StateStore.SaveState(p.TaskID, p.State); err != nil {
		logger.Error(logComponent).
			Str("task_id", p.TaskID).
			Err(err).
			Msg("[Pipeline] 持久化状态失败")
		return err
	}
	if err := p.deps.WorkspaceProvider.SyncToRemote(p.TaskID); err != nil {
		logger.Error(logComponent).
			Str("task_id", p.TaskID).
			Err(err).
			Msg("[Pipeline] 同步工作区失败")
		return err
	}
	logger.Debug(logComponent).
		Str("task_id", p.TaskID).
		Str("stage", string(p.State.Stage)).
		Msg("[Pipeline] checkpoint")
	return nil
}
