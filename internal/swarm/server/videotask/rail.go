package videotask

import (
	"errors"
	"fmt"
	"sync"

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
