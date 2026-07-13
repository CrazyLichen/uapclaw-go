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
}

// ──────────────────────────── 全局变量 ────────────────────────────

// maxPipelineIterations Pipeline 最大迭代次数，防止无限循环
const maxPipelineIterations = 20

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
	}
}

// Run 从当前阶段开始执行，直到遇到挂起点或终态。
//
// 返回只读事件 channel，调用方逐个读取事件。
// Pipeline goroutine 结束后自动 close channel。
//
// 对齐 Python: SkillDevPipeline.run() → AsyncIterator[SkillDevEvent]
func (p *SkillDevPipeline) Run(ctx context.Context) (<-chan SkillDevEvent, error) {
	eventCh := make(chan SkillDevEvent, 64)

	go func() {
		defer close(eventCh)

		iterations := 0
		for p.State.Stage != SkillDevStageCompleted && p.State.Stage != SkillDevStageError {
			iterations++
			if iterations > maxPipelineIterations {
				errMsg := fmt.Sprintf("Pipeline 超过最大迭代次数 %d，可能存在无限循环", maxPipelineIterations)
				p.State.Stage = SkillDevStageError
				p.State.Error = &errMsg
				p.emit(eventCh, SkillDevEventTypeError, map[string]any{"message": errMsg})
				_ = p.checkpoint()
				break
			}
			// 命中挂起点：推送确认请求 → checkpoint → 暂停
			if suspension, ok := SuspensionPoints[p.State.Stage]; ok {
				p.emit(eventCh, SkillDevEventTypeTodosUpdate, map[string]any{
					"todos": ComputeTodos(p.State.Stage, &p.State.Mode),
				})
				p.emit(eventCh, SkillDevEventTypeConfirmRequest, map[string]any{
					"confirm_type": suspension.ConfirmType,
					"title":        suspension.Title,
					"message":      suspension.Message,
					"data":         suspension.ExtractData(p.State),
					"actions":      suspension.Actions,
				})
				if err := p.checkpoint(); err != nil {
					p.emit(eventCh, SkillDevEventTypeError, map[string]any{
						"message": fmt.Sprintf("checkpoint 失败: %s", err),
					})
					return
				}
				break
			}

			// 执行当前阶段
			handler, ok := stageHandlers[p.State.Stage]
			if !ok {
				p.emit(eventCh, SkillDevEventTypeError, map[string]any{
					"message": fmt.Sprintf("阶段 %s 没有对应的处理器", p.State.Stage),
				})
				return
			}

			workspace, err := p.deps.WorkspaceProvider.EnsureLocal(p.TaskID)
			if err != nil {
				p.State.Stage = SkillDevStageError
				errMsg := fmt.Sprintf("确保工作区失败: %s", err.Error())
				p.State.Error = &errMsg
				p.emit(eventCh, SkillDevEventTypeError, map[string]any{"message": errMsg})
				_ = p.checkpoint()
				return
			}

			stageEventCh := make(chan SkillDevEvent, 64)
			sctx := NewSkillDevContext(p.TaskID, p.deps, p.State, workspace, stageEventCh)

			p.emit(eventCh, SkillDevEventTypeStageChanged, map[string]any{
				"stage":     string(p.State.Stage),
				"iteration": p.State.Iteration,
			})
			p.emit(eventCh, SkillDevEventTypeTodosUpdate, map[string]any{
				"todos": ComputeTodos(p.State.Stage, &p.State.Mode),
			})

			// 启动事件转发 goroutine，将 Context 的事件转发到 Pipeline 的 eventCh
			done := make(chan struct{})
			go func() {
				defer close(done)
				for evt := range stageEventCh {
					eventCh <- evt
				}
			}()

			result, execErr := handler.Execute(ctx, sctx)

			// 关闭 stageEventCh，等待转发 goroutine 结束
			close(stageEventCh)
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
				p.emit(eventCh, SkillDevEventTypeError, map[string]any{"message": errMsg})
				_ = p.checkpoint()
				break
			}

			p.State.Stage = result.NextStage
			if err := p.checkpoint(); err != nil {
				p.emit(eventCh, SkillDevEventTypeError, map[string]any{
					"message": fmt.Sprintf("checkpoint 失败: %s", err),
				})
				return
			}
		}
	}()

	return eventCh, nil
}

// Resume 从挂起点恢复执行。
//
// 返回只读事件 channel，调用方逐个读取事件。
//
// 对齐 Python: SkillDevPipeline.resume(data) → AsyncIterator[SkillDevEvent]
func (p *SkillDevPipeline) Resume(ctx context.Context, data map[string]any) (<-chan SkillDevEvent, error) {
	currentStage := p.State.Stage
	suspension, ok := SuspensionPoints[currentStage]
	if !ok {
		// 非挂起点，返回错误（不启动 goroutine，不创建 channel）
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

// emit 向事件 channel 发送一个事件。
//
// 对齐 Python: SkillDevPipeline._emit()
func (p *SkillDevPipeline) emit(eventCh chan<- SkillDevEvent, eventType SkillDevEventType, payload map[string]any) {
	merged := make(map[string]any, len(payload)+1)
	merged["task_id"] = p.TaskID
	for k, v := range payload {
		merged[k] = v
	}
	eventCh <- SkillDevEvent{
		EventType: eventType,
		Payload:   merged,
		TaskID:    p.TaskID,
	}
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
