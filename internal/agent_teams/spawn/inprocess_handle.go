package spawn

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InProcessSpawnHandle 进程内生成句柄，管理 goroutine 生命周期。
// 对齐 Python: InProcessSpawnHandle (inprocess_handle.py)
//
// 用 context.CancelFunc 对齐 Python task.cancel()，
// 用 done chan 对齐 Python task.done()。
// agentRef 对齐 Python agent_ref: Any，InProcess 模式独有。
type InProcessSpawnHandle struct {
	// processID 进程唯一标识（"inproc-{memberName}"）
	processID string
	// cancelCtx 取消 goroutine 的 context.CancelFunc
	cancelCtx context.CancelFunc
	// done goroutine 完成通知 chan，close 表示完成
	done chan struct{}
	// agentRef 进程内 Agent 引用（对齐 Python agent_ref: Any）
	agentRef SpawnableAgent
	// chunkForward chunk 转发观察者引用，cleanup 时可确定性断开
	// ⤵️ 预留：StreamController（9.60）实现后回填类型
	chunkForward any
	// onUnhealthy 不健康回调
	onUnhealthy func()
	// shutdownRequested 是否已请求关闭
	shutdownRequested bool
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// inprocessLogComponent 日志组件
	inprocessLogComponent = logger.ComponentChannel
	// defaultShutdownTimeout 默认关闭超时
	defaultShutdownTimeout = 10 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInProcessSpawnHandle 创建新的 InProcessSpawnHandle。
// 对齐 Python: InProcessSpawnHandle(process_id, _task, agent_ref)
func NewInProcessSpawnHandle(
	processID string,
	cancelCtx context.CancelFunc,
	done chan struct{},
	agentRef SpawnableAgent,
) *InProcessSpawnHandle {
	return &InProcessSpawnHandle{
		processID: processID,
		cancelCtx: cancelCtx,
		done:      done,
		agentRef:  agentRef,
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// ProcessID 返回进程唯一标识。
func (h *InProcessSpawnHandle) ProcessID() string {
	return h.processID
}

// IsAlive 检查任务是否仍在运行。
// 对齐 Python: InProcessSpawnHandle.is_alive
func (h *InProcessSpawnHandle) IsAlive() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// IsHealthy 检查任务是否健康（存活且未请求关闭）。
// 对齐 Python: InProcessSpawnHandle.is_healthy
func (h *InProcessSpawnHandle) IsHealthy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.IsAlive() && !h.shutdownRequested
}

// StartHealthCheck 启动健康检查后台任务（No-op：进程内无需 IPC 健康检查）。
// 对齐 Python: InProcessSpawnHandle.start_health_check
func (h *InProcessSpawnHandle) StartHealthCheck(_ context.Context, _ ...time.Duration) error {
	return nil
}

// StopHealthCheck 停止健康检查后台任务（No-op）。
// 对齐 Python: InProcessSpawnHandle.stop_health_check
func (h *InProcessSpawnHandle) StopHealthCheck() error {
	return nil
}

// Shutdown 优雅关闭：取消 goroutine 并等待完成。
// 对齐 Python: InProcessSpawnHandle.shutdown
//
// 返回 (graceful, error)：graceful=true 表示在超时内完成。
func (h *InProcessSpawnHandle) Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error) {
	h.mu.Lock()
	if h.shutdownRequested {
		h.mu.Unlock()
		return false, fmt.Errorf("关闭已请求")
	}
	h.shutdownRequested = true
	h.mu.Unlock()

	if !h.IsAlive() {
		return true, nil
	}

	// 取消 goroutine
	if h.cancelCtx != nil {
		h.cancelCtx()
	}

	// 等待完成
	shutdownTimeout := defaultShutdownTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		shutdownTimeout = timeout[0]
	}

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	select {
	case <-h.done:
		logger.Info(inprocessLogComponent).
			Str("process_id", h.processID).
			Msg("进程内任务正常关闭")
		return true, nil
	case <-timer.C:
		logger.Warn(inprocessLogComponent).
			Str("process_id", h.processID).
			Dur("timeout", shutdownTimeout).
			Msg("进程内任务关闭超时")
		return false, fmt.Errorf("关闭超时: %s", h.processID)
	}
}

// ForceKill 强制终止：取消 goroutine，不等待完成。
// 对齐 Python: InProcessSpawnHandle.force_kill
func (h *InProcessSpawnHandle) ForceKill() error {
	h.mu.Lock()
	h.shutdownRequested = true
	h.mu.Unlock()

	if h.cancelCtx != nil {
		h.cancelCtx()
	}

	logger.Info(inprocessLogComponent).
		Str("process_id", h.processID).
		Msg("强制终止进程内任务")
	return nil
}

// WaitForCompletion 等待任务完成。
// 对齐 Python: InProcessSpawnHandle.wait_for_completion
//
// 返回 0=成功，-1=异常或未启动。
func (h *InProcessSpawnHandle) WaitForCompletion() (int, error) {
	if h.done == nil {
		return -1, nil
	}
	<-h.done
	return 0, nil
}

// SetOnUnhealthy 设置不健康回调。
// 对齐 Python: SpawnedProcessHandle.on_unhealthy 构造后赋值
func (h *InProcessSpawnHandle) SetOnUnhealthy(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUnhealthy = fn
}

// OnUnhealthy 触发不健康回调（内部使用）。
func (h *InProcessSpawnHandle) OnUnhealthy() {
	h.mu.Lock()
	fn := h.onUnhealthy
	h.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// AgentRef 返回进程内 Agent 引用。
// 对齐 Python: InProcessSpawnHandle.agent_ref
// 消费者需自行断言为具体类型。
func (h *InProcessSpawnHandle) AgentRef() SpawnableAgent {
	return h.agentRef
}

// ChunkForward 返回 chunk 转发观察者引用。
// ⤵️ 预留：StreamController（9.60）实现后回填
func (h *InProcessSpawnHandle) ChunkForward() any {
	return h.chunkForward
}

// SetChunkForward 设置 chunk 转发观察者引用。
// ⤵️ 预留：StreamController（9.60）实现后回填
func (h *InProcessSpawnHandle) SetChunkForward(v any) {
	h.chunkForward = v
}

// AgentCard 返回 AgentCard（便利方法，等价于 h.agentRef.AgentCard()）。
func (h *InProcessSpawnHandle) AgentCard() *agentschema.AgentCard {
	if h.agentRef == nil {
		return nil
	}
	return h.agentRef.AgentCard()
}
