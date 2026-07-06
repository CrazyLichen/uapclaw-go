package task_loop

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionSpawnExecutor 会话子进程执行器，执行 SESSION_SPAWN_TASK_TYPE 类型任务。
// 从 TaskManager 获取任务元数据，提取 subagent_type/sub_session_id，
// 通过 DeepAgent.create_subagent 创建子 Agent 并 invoke。
// 对齐 Python: SessionSpawnExecutor
type SessionSpawnExecutor struct {
	// deps 任务执行器依赖
	deps *modules.TaskExecutorDependencies
	// provider 深层 Agent 提供者（用于 CreateSubagent）
	provider interfaces.DeepAgentInterface
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionSpawnExecutor 创建会话子进程执行器。
// 对齐 Python: SessionSpawnExecutor.__init__
func NewSessionSpawnExecutor(deps *modules.TaskExecutorDependencies, provider interfaces.DeepAgentInterface) *SessionSpawnExecutor {
	return &SessionSpawnExecutor{
		deps:     deps,
		provider: provider,
	}
}

// ExecuteAbility 执行子 Agent 任务。
// 获取任务元数据 → 创建子 Agent → invoke → 发送完成/失败事件。
// 对齐 Python: SessionSpawnExecutor.execute_ability
func (e *SessionSpawnExecutor) ExecuteAbility(
	ctx context.Context,
	taskID string,
	sess sessioninterfaces.SessionFacade,
) (<-chan *stream.OutputSchema, error) {
	ch := make(chan *stream.OutputSchema, 1)

	// 步骤 1：获取任务元数据
	tasks, err := e.deps.TaskManager.GetTask(ctx, MakeFilter(taskID))
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("task_id", taskID).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "SessionSpawnExecutor.ExecuteAbility").
			Msg("查询任务失败")
		ch <- e.buildErrorChunk(taskID, err.Error())
		close(ch)
		return ch, nil
	}
	if len(tasks) == 0 {
		logger.Warn(logComponent).
			Str("task_id", taskID).
			Msg("未找到任务")
		ch <- e.buildErrorChunk(taskID, "Task not found")
		close(ch)
		return ch, nil
	}

	task := tasks[0]
	meta := task.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}

	// 步骤 2：提取元数据
	subagentType, _ := meta["subagent_type"].(string)
	if subagentType == "" {
		subagentType = "general-purpose"
	}
	query, _ := meta["task_description"].(string)
	cid, _ := meta["sub_session_id"].(string)

	// 步骤 3：日志
	logger.Info(logComponent).
		Str("task_id", taskID).
		Str("subagent_type", subagentType).
		Str("sub_session_id", cid).
		Msg("开始执行 SessionSpawn 任务")

	// 步骤 4-8：异步执行子 Agent
	go func() {
		defer close(ch)

		// 步骤 4：创建子 Agent
		subAgent, createErr := e.provider.CreateSubagent(subagentType, cid)
		if createErr != nil {
			logger.Error(logComponent).
				Err(createErr).
				Str("task_id", taskID).
				Str("subagent_type", subagentType).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("创建子 Agent 失败")
			ch <- e.buildErrorChunk(taskID, createErr.Error())
			return
		}

		// 步骤 5：调用子 Agent
		effective := map[string]any{
			"query":           query,
			"conversation_id": cid,
		}
		result, invokeErr := subAgent.ReactAgent().Invoke(ctx, effective, agentinterfaces.WithSession(sess))

		if invokeErr != nil {
			// 异常路径
			logger.Error(logComponent).
				Err(invokeErr).
				Str("task_id", taskID).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("SessionSpawn 任务执行失败")
			ch <- e.buildErrorChunk(taskID, invokeErr.Error())
			return
		}

		// 步骤 6：提取输出
		var payload string
		if result != nil {
			if output, ok := result["output"]; ok {
				payload = fmt.Sprintf("%v", output)
			}
		}

		// 步骤 7：完成日志
		logger.Info(logComponent).
			Str("task_id", taskID).
			Int("output_len", len(payload)).
			Msg("SessionSpawn 任务执行完成")

		// 步骤 8：发送完成事件
		ch <- &stream.OutputSchema{
			Type: string(cschema.EventTaskCompletion),
			Payload: &cschema.ControllerOutputPayload{
				Type: string(cschema.EventTaskCompletion),
				Data: []cschema.DataFrame{&cschema.JsonDataFrame{Data: map[string]any{"output": payload}}},
				Metadata: map[string]any{
					"task_id":   taskID,
					"task_type": hschema.SessionSpawnTaskType,
				},
			},
			IsLastSchema: true,
		}
	}()

	return ch, nil
}

// CanPause 检查任务是否可暂停。SessionSpawn 任务不支持暂停。
// 对齐 Python: SessionSpawnExecutor.can_pause
func (e *SessionSpawnExecutor) CanPause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return false, "Session spawn 任务不支持暂停", nil
}

// Pause 暂停任务。SessionSpawn 任务不支持暂停，始终返回 false。
// 对齐 Python: SessionSpawnExecutor.pause
func (e *SessionSpawnExecutor) Pause(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, error) {
	return false, nil
}

// CanCancel 检查任务是否可取消。SessionSpawn 任务始终可取消。
// 对齐 Python: SessionSpawnExecutor.can_cancel
func (e *SessionSpawnExecutor) CanCancel(_ context.Context, _ string, _ sessioninterfaces.SessionFacade) (bool, string, error) {
	return true, "", nil
}

// Cancel 取消任务。已在 TaskScheduler 中取消，此处直接返回 true。
// 对齐 Python: SessionSpawnExecutor.cancel
func (e *SessionSpawnExecutor) Cancel(_ context.Context, taskID string, _ sessioninterfaces.SessionFacade) (bool, error) {
	logger.Info(logComponent).
		Str("task_id", taskID).
		Msg("取消 SessionSpawn 任务")
	return true, nil
}

// BuildSessionSpawnExecutor 构建 hschema.SessionSpawnTaskType 执行器的工厂闭包。
// 返回的闭包捕获 provider，供 TaskExecutorRegistry 注册。
// 对齐 Python: build_session_spawn_executor
func BuildSessionSpawnExecutor(provider interfaces.DeepAgentInterface) func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
	return func(deps *modules.TaskExecutorDependencies) modules.TaskExecutor {
		return NewSessionSpawnExecutor(deps, provider)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildErrorChunk 构建错误输出分片。
// 对齐 Python: SessionSpawnExecutor._build_error_chunk
func (e *SessionSpawnExecutor) buildErrorChunk(taskID string, errMsg string) *stream.OutputSchema {
	return &stream.OutputSchema{
		Type: string(cschema.EventTaskFailed),
		Payload: &cschema.ControllerOutputPayload{
			Type: string(cschema.EventTaskFailed),
			Data: []cschema.DataFrame{&cschema.TextDataFrame{Text: errMsg}},
			Metadata: map[string]any{
				"task_id":   taskID,
				"task_type": hschema.SessionSpawnTaskType,
			},
		},
		IsLastSchema: true,
	}
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时接口检查：SessionSpawnExecutor 必须满足 modules.TaskExecutor
var _ modules.TaskExecutor = (*SessionSpawnExecutor)(nil)
