package videotask

import (
	"errors"
	"fmt"
	"sync"

	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner/callback"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// railLogComponent 视频任务 rail 日志组件。
const railLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 结构体 ────────────────────────────

// VoiceAgentTaskRail 语音委派任务的根 Agent 回调 rail。
// 对齐 rail.py:8-17 VoiceAgentTaskRail(AgentRail)：
// priority 81——刻意排在 StreamEventRail（80）之前，让检查点先于流事件看到请求。
type VoiceAgentTaskRail struct {
	sainterfaces.BaseRail
	owner any
	// managedTasks 会话 ID → 绑定（rail per-session、binding per-execution）
	managedTasks map[string]*TaskExecutionBinding
	mu           sync.Mutex
}

// NewVoiceAgentTaskRail 创建 rail。
func NewVoiceAgentTaskRail(owner any) *VoiceAgentTaskRail {
	return &VoiceAgentTaskRail{owner: owner, managedTasks: map[string]*TaskExecutionBinding{}}
}

// Priority 执行优先级，对齐 rail.py:9 priority = 81。
func (r *VoiceAgentTaskRail) Priority() int { return 81 }

// Owner 归属（对齐 self.owner）。
func (r *VoiceAgentTaskRail) Owner() any { return r.owner }

// ManagedTasks 读绑定表（BindTaskExecution/适配器装配用）。
func (r *VoiceAgentTaskRail) ManagedTasks() map[string]*TaskExecutionBinding {
	return r.managedTasks
}

// GetCallbacks 注册三个检查点钩子（对齐 rail.py:18-22 的 before/after_model 与 before_tool）。
func (r *VoiceAgentTaskRail) GetCallbacks() map[sainterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(sainterfaces.CallbackBeforeModelCall, r.wrapBeforeModelCall),
		r.CallbackFrom(sainterfaces.CallbackAfterModelCall, r.wrapAfterModelCall),
		r.CallbackFrom(sainterfaces.CallbackBeforeToolCall, r.wrapBeforeToolCall),
	)
}

// wrapBeforeModelCall 模型前钩子。
func (r *VoiceAgentTaskRail) wrapBeforeModelCall(ctx context.Context, cbcAny any) error {
	return r.dispatchFromCBC(ctx, "before_model", cbcAny)
}

// wrapAfterModelCall 模型后钩子。
func (r *VoiceAgentTaskRail) wrapAfterModelCall(ctx context.Context, cbcAny any) error {
	return r.dispatchFromCBC(ctx, "after_model", cbcAny)
}

// wrapBeforeToolCall 工具前钩子。
func (r *VoiceAgentTaskRail) wrapBeforeToolCall(ctx context.Context, cbcAny any) error {
	return r.dispatchFromCBC(ctx, "before_tool", cbcAny)
}

// dispatchFromCBC 从回调上下文提取受管请求 ID 并分派。
// run_context 透传模式对齐 root_permission_queue_rail.go runExtraOf（cbc.GetExtra("run_context")）。
func (r *VoiceAgentTaskRail) dispatchFromCBC(_ context.Context, stage string, cbcAny any) error {
	cbc, ok := cbcAny.(*sainterfaces.AgentCallbackContext)
	if !ok || cbc == nil {
		return nil
	}
	var runExtra map[string]any
	// 读取顺序：cbc.Inputs() 按 InvokeInputs / TaskIterationInputs 双形状取
	// RunContext（对齐 task_completion.go:841 与 task_loop/executor.go:179 两处透传），
	// 回退 cbc.GetExtra("run_context")
	switch inv := cbc.Inputs().(type) {
	case *sainterfaces.InvokeInputs:
		if inv != nil && inv.RunContext != nil {
			runExtra = inv.RunContext.Extra
		}
	case *sainterfaces.TaskIterationInputs:
		if inv != nil && inv.RunContext != nil {
			runExtra = inv.RunContext.Extra
		}
	}
	if runExtra == nil {
		if raw, ok := cbc.GetExtra("run_context"); ok {
			// 透传有两种形状：类型化 *RunContext 与 map（旧路径）
			switch rv := raw.(type) {
			case *sainterfaces.RunContext:
				if rv != nil {
					runExtra = rv.Extra
				}
			case map[string]any:
				runExtra, _ = rv["extra"].(map[string]any)
			}
		}
	}
	managedRequest := ""
	if runExtra != nil {
		managedRequest = strOf(runExtra["managed_task_request"])
	}
	var observed []string
	if stage == "after_model" && cbc.ModelContext() != nil {
		if msgs, err := cbc.ModelContext().GetMessages(0, false); err == nil {
			for _, msg := range msgs {
				observed = append(observed, fmt.Sprint(messageContentOf(msg)))
			}
		}
	}
	return TaskCheckpointDispatch(r, stage, managedRequest,
		map[string]any{"run_context": map[string]any{"extra": runExtra}},
		func(*TaskExecutionBinding) bool { return true }, // 绑定按 request_id 精确匹配，root 兜底退化为通过
		func(changeID, instruction string) error {
			// 注入用户消息（对齐 checkpoint.py:34-38 的 JSON 载荷形状）
			if cbc.ModelContext() == nil {
				return nil
			}
			payload := fmt.Sprintf(`{"managed_task_change":%q,"instruction":%q,"meaning":%q}`,
				changeID, instruction,
				"User changes this task's requirements; retain other constraints and permissions.")
			_, err := cbc.ModelContext().AddMessages(context.Background(),
				[]llmschema.BaseMessage{llmschema.NewUserMessage(payload)})
			return err
		}, observed)
}

// messageContentOf 提取消息内容（llm_schema.BaseMessage 的 Content 字段鸭子访问）。
func messageContentOf(msg llmschema.BaseMessage) any {
	if msg == nil {
		return ""
	}
	return msg.GetContent()
}

// LockGuard 暴露锁（execution.go 的 BindTaskExecution 需要并发安全访问）。
func (r *VoiceAgentTaskRail) LockGuard() *sync.Mutex { return &r.mu }

// currentVoiceRail 当前会话已装 rail 的登记点（装配层注册；Python 由 adapter
// task_execution_binding 元组承担，Go 显式化，偏差 O-15）。
var currentVoiceRail *VoiceAgentTaskRail

// RegisterVoiceRail 装配层登记当前 rail（InstallVoiceTaskRail 调用）。
func RegisterVoiceRail(rail *VoiceAgentTaskRail) { currentVoiceRail = rail }

// CurrentVoiceRail 读当前 rail（BindTaskExecution 用；无则视为未安装）。
func CurrentVoiceRail() *VoiceAgentTaskRail { return currentVoiceRail }

// InstallTaskRail 注册语音任务 rail（幂等；reload 强制重装）。
// 对齐 rail.py:19-31 install_task_rail（owner.register_rail 由调用方回调承担）。
func InstallTaskRail(owner any, registerRail func(rail *VoiceAgentTaskRail) error,
	unregisterRail func(rail *VoiceAgentTaskRail) error, reload bool,
) (*VoiceAgentTaskRail, error) {
	rail := CurrentVoiceRail()
	if rail == nil || rail.Owner() != owner {
		rail = NewVoiceAgentTaskRail(owner)
	} else if !reload {
		return rail, nil
	} else {
		if err := unregisterRail(rail); err != nil {
			return nil, err
		}
	}
	if err := registerRail(rail); err != nil {
		return nil, err
	}
	RegisterVoiceRail(rail)
	return rail, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskCheckpointDispatch rail 回调分派核心。
// 对齐 rail.py:33-64 task_checkpoint：
// 从 run_context.extra.managed_task_request 定位绑定 → 根匹配 → 按阶段分派；
// 异常一律 AbortError 语义（Go 返回 error，消息 TASK_CHECKPOINT_REJECTED）。
// 偏差 O-16：Python before_model 直接改 openjiuwen 上下文对象，Go 经 addChange
// 注入钩子（装配层对接 DeepAgent 上下文）。
func TaskCheckpointDispatch(rail *VoiceAgentTaskRail, stage string,
	managedTaskRequest string, runExtra map[string]any,
	agentIsRoot func(binding *TaskExecutionBinding) bool,
	addChange func(changeID, instruction string) error,
	observedContents []string,
) error {
	rail.mu.Lock()
	var binding *TaskExecutionBinding
	for _, candidate := range rail.managedTasks {
		if candidate.RequestID == managedTaskRequest ||
			(managedTaskRequest == "" && agentIsRoot(candidate)) {
			binding = candidate
			break
		}
	}
	rail.mu.Unlock()
	if binding == nil {
		if managedTaskRequest == "" {
			return nil // 普通请求不使用任务存储与检查点（对齐 :46-47）
		}
		return errors.New("TASK_EXECUTION_BINDING_CLOSED")
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error(railLogComponent).Msgf("任务检查点异常: %v", rec)
		}
	}()
	var err error
	switch stage {
	case "before_model":
		// 远端 check 可能失败/取消之前先登记原生工作（对齐 :54 注释）
		binding.CaptureExecution(true)
		err = binding.Check(runExtra)
		if err == nil {
			err = binding.BeforeModel(runExtra, addChange)
		}
	case "after_model":
		err = binding.AfterModel(runExtra, observedContents)
	default: // before_tool
		err = binding.Check(runExtra)
	}
	if err != nil {
		return fmt.Errorf("TASK_CHECKPOINT_REJECTED: %w", err)
	}
	return nil
}
