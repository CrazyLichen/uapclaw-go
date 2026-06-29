package spawn

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnedProcessHandle 子进程句柄，管理子进程的生命周期、通信和健康检查。
// 对齐 Python: SpawnedProcessHandle (process_manager.py)
type SpawnedProcessHandle struct {
	// processID 进程唯一标识
	processID string
	// cmd 底层 exec.Cmd
	cmd *exec.Cmd
	// stdin 子进程标准输入
	stdin io.WriteCloser
	// stdout 子进程标准输出
	stdout io.Reader
	// config Spawn 配置
	config SpawnConfig
	// onUnhealthy 不健康回调
	onUnhealthy func()
	// maxHealthFailures 最大连续健康检查失败次数
	maxHealthFailures int

	// healthCheckTask 健康检查后台任务
	healthCheckTask *utils.BackgroundTask
	// isHealthy 是否健康
	isHealthy bool
	// shutdownRequested 是否已请求关闭
	shutdownRequested bool
	// consecutiveFails 连续健康检查失败次数
	consecutiveFails int
	// unhealthyFired 不健康回调是否已触发
	unhealthyFired bool
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpawnedProcessHandle 创建子进程句柄。
// 对齐 Python: SpawnedProcessHandle.__init__()
func NewSpawnedProcessHandle(
	processID string,
	cmd *exec.Cmd,
	stdin io.WriteCloser,
	stdout io.Reader,
	config SpawnConfig,
	onUnhealthy func(),
	maxHealthFailures int,
) *SpawnedProcessHandle {
	if maxHealthFailures <= 0 {
		maxHealthFailures = DefaultMaxHealthFailures
	}
	return &SpawnedProcessHandle{
		processID:         processID,
		cmd:               cmd,
		stdin:             stdin,
		stdout:            stdout,
		config:            config,
		onUnhealthy:       onUnhealthy,
		maxHealthFailures: maxHealthFailures,
		isHealthy:         true,
	}
}

// ProcessID 返回进程唯一标识。
func (h *SpawnedProcessHandle) ProcessID() string {
	return h.processID
}

// IsAlive 检查子进程是否存活。
func (h *SpawnedProcessHandle) IsAlive() bool {
	if h.cmd == nil {
		return false
	}
	if h.cmd.ProcessState != nil {
		return false
	}
	return h.cmd.Process != nil
}

// PID 返回子进程的操作系统进程 ID，未启动时返回 -1。
func (h *SpawnedProcessHandle) PID() int {
	if h.cmd == nil || h.cmd.Process == nil {
		return -1
	}
	return h.cmd.Process.Pid
}

// ExitCode 返回子进程退出码，未退出时返回 -1。
func (h *SpawnedProcessHandle) ExitCode() int {
	if h.cmd == nil || h.cmd.ProcessState == nil {
		return -1
	}
	return h.cmd.ProcessState.ExitCode()
}

// IsHealthy 返回子进程是否健康（健康且存活）。
func (h *SpawnedProcessHandle) IsHealthy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.isHealthy && h.IsAlive()
}

// SendMessage 向子进程发送消息。
// 对齐 Python: SpawnedProcessHandle.send_message()
func (h *SpawnedProcessHandle) SendMessage(ctx context.Context, msg Message) error {
	if !h.IsAlive() {
		return fmt.Errorf("子进程 %s 未运行，无法发送消息", h.processID)
	}
	logger.Debug(logComponent).
		Str("message_type", msg.Type.String()).
		Str("process_id", h.processID).
		Msg("向子进程发送消息")
	return WriteMessage(h.stdin, msg)
}

// ReceiveMessage 从子进程接收消息。
// 对齐 Python: SpawnedProcessHandle.receive_message()
func (h *SpawnedProcessHandle) ReceiveMessage(ctx context.Context) (Message, error) {
	msg, err := ReadMessage(h.stdout)
	if err != nil {
		return Message{}, err
	}
	logger.Debug(logComponent).
		Str("message_type", msg.Type.String()).
		Str("process_id", h.processID).
		Msg("从子进程接收消息")
	return msg, nil
}

// StartHealthCheck 启动健康检查后台任务。
// 对齐 Python: SpawnedProcessHandle.start_health_check()
func (h *SpawnedProcessHandle) StartHealthCheck(ctx context.Context, interval ...time.Duration) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.healthCheckTask != nil {
		return fmt.Errorf("健康检查已在运行")
	}

	checkInterval := DefaultHealthCheckInterval
	if len(interval) > 0 && interval[0] > 0 {
		checkInterval = interval[0]
	}

	logger.Info(logComponent).
		Str("process_id", h.processID).
		Dur("interval", checkInterval).
		Msg("启动健康检查")

	h.healthCheckTask = utils.NewBackgroundTask(
		fmt.Sprintf("health-check-%s", h.processID),
		"spawn",
		func(taskCtx context.Context) error {
			ticker := time.NewTicker(checkInterval)
			defer ticker.Stop()
			for {
				select {
				case <-taskCtx.Done():
					return nil
				case <-ticker.C:
					h.performHealthCheck(taskCtx)
				}
			}
		},
	)
	h.healthCheckTask.Start(ctx)

	return nil
}

// StopHealthCheck 停止健康检查后台任务。
// 对齐 Python: SpawnedProcessHandle.stop_health_check()
func (h *SpawnedProcessHandle) StopHealthCheck() error {
	h.mu.Lock()
	task := h.healthCheckTask
	h.healthCheckTask = nil
	h.mu.Unlock()

	if task == nil {
		return nil
	}

	logger.Info(logComponent).
		Str("process_id", h.processID).
		Msg("停止健康检查")

	return task.Stop(5 * time.Second)
}

// Shutdown 优雅关闭子进程。
// 流程：停止健康检查 → 发送 SHUTDOWN → 等待 SHUTDOWN_ACK → 等待进程退出 → 超时回退 forceTerminate。
// 对齐 Python: SpawnedProcessHandle.shutdown()
func (h *SpawnedProcessHandle) Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error) {
	h.mu.Lock()
	if h.shutdownRequested {
		h.mu.Unlock()
		return false, fmt.Errorf("关闭已请求")
	}
	h.shutdownRequested = true
	h.mu.Unlock()

	// 停止健康检查
	_ = h.StopHealthCheck()

	// 检查进程是否存活
	if !h.IsAlive() {
		return true, nil
	}

	// 发送 SHUTDOWN 消息
	shutdownMsg := NewMessage(MessageTypeShutdown, map[string]any{
		"reason": "parent_initiated",
	})
	if err := h.SendMessage(ctx, shutdownMsg); err != nil {
		logger.Warn(logComponent).
			Str("process_id", h.processID).
			Err(err).
			Msg("发送 SHUTDOWN 消息失败，尝试强制终止")
		killed, terminateErr := h.forceTerminate()
		return killed, terminateErr
	}

	// 等待 SHUTDOWN_ACK
	shutdownTimeout := DefaultShutdownTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		shutdownTimeout = timeout[0]
	}

	ackReceived := h.waitForShutdownAck(ctx)
	if ackReceived {
		logger.Info(logComponent).
			Str("process_id", h.processID).
			Msg("收到 SHUTDOWN_ACK，等待进程退出")
	} else {
		logger.Warn(logComponent).
			Str("process_id", h.processID).
			Dur("timeout", shutdownTimeout).
			Msg("等待 SHUTDOWN_ACK 超时")
	}

	// 等待进程退出（宽限期）
	if h.IsAlive() {
		waitCh := make(chan struct{})
		go func() {
			_, _ = h.cmd.Process.Wait()
			close(waitCh)
		}()

		select {
		case <-waitCh:
			return true, nil
		case <-time.After(ShutdownWaitPeriod):
			// 宽限期超时，强制终止
			logger.Warn(logComponent).
				Str("process_id", h.processID).
				Msg("进程未在宽限期内退出，执行强制终止")
			return h.forceTerminate()
		}
	}

	return true, nil
}

// ForceKill 强制终止子进程（直接 SIGKILL）。
// 对齐 Python: SpawnedProcessHandle.force_kill()
//
// 与 forceTerminate 的区别：
//   - ForceKill：直接 SIGKILL，无宽限期
//   - forceTerminate：SIGTERM → 3s → SIGKILL，给子进程清理机会
func (h *SpawnedProcessHandle) ForceKill() error {
	if !h.IsAlive() {
		return nil
	}

	// 设置关闭标志
	h.mu.Lock()
	h.shutdownRequested = true
	h.mu.Unlock()

	// 停止健康检查
	_ = h.StopHealthCheck()

	logger.Info(logComponent).
		Str("process_id", h.processID).
		Msg("强制终止子进程（SIGKILL）")

	err := h.cmd.Process.Kill()
	if err != nil {
		logger.Warn(logComponent).
			Str("process_id", h.processID).
			Err(err).
			Msg("SIGKILL 失败（进程可能已退出）")
		return err
	}

	// 等待进程退出
	_, _ = h.cmd.Process.Wait()

	logger.Info(logComponent).
		Str("process_id", h.processID).
		Msg("子进程已强制终止")
	return nil
}

// WaitForCompletion 等待子进程完成，返回退出码。
// 对齐 Python: SpawnedProcessHandle.wait_for_completion()
func (h *SpawnedProcessHandle) WaitForCompletion() (int, error) {
	if h.stdin != nil {
		_ = h.stdin.Close()
	}

	if h.cmd == nil || h.cmd.Process == nil {
		return -1, fmt.Errorf("进程未启动")
	}

	err := h.cmd.Wait()
	exitCode := h.ExitCode()
	return exitCode, err
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// performHealthCheck 执行一次健康检查。
// 对齐 Python: SpawnedProcessHandle._perform_health_check()
func (h *SpawnedProcessHandle) performHealthCheck(ctx context.Context) {
	if !h.IsAlive() {
		h.recordHealthFailure()
		return
	}

	// 发送健康检查消息
	healthMsg := NewMessage(MessageTypeHealthCheck, nil)
	if err := h.SendMessage(ctx, healthMsg); err != nil {
		logger.Error(logComponent).
			Str("process_id", h.processID).
			Err(err).
			Msg("发送健康检查消息失败")
		h.recordHealthFailure()
		return
	}

	// 等待健康检查响应
	checkTimeout := h.config.HealthCheckTimeout
	if checkTimeout <= 0 {
		checkTimeout = DefaultHealthCheckTimeout
	}

	respCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	resp, err := h.waitForHealthCheckResponse(respCtx, healthMsg.MessageID)
	if err != nil {
		logger.Warn(logComponent).
			Str("process_id", h.processID).
			Dur("timeout", checkTimeout).
			Msg("健康检查响应超时")
		h.recordHealthFailure()
		return
	}

	if resp.Type == MessageTypeHealthCheckResponse {
		logger.Debug(logComponent).
			Str("process_id", h.processID).
			Msg("健康检查通过")
		h.mu.Lock()
		h.isHealthy = true
		h.consecutiveFails = 0
		h.unhealthyFired = false
		h.mu.Unlock()
	} else {
		logger.Error(logComponent).
			Str("process_id", h.processID).
			Str("response_type", resp.Type.String()).
			Msg("健康检查响应类型不正确")
		h.recordHealthFailure()
	}
}

// recordHealthFailure 记录一次健康检查失败。
// 对齐 Python: SpawnedProcessHandle._record_health_failure()
func (h *SpawnedProcessHandle) recordHealthFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.consecutiveFails++
	h.isHealthy = false

	if h.consecutiveFails >= h.maxHealthFailures && !h.unhealthyFired && h.onUnhealthy != nil {
		h.unhealthyFired = true
		// 在锁外调用回调避免死锁
		onUnhealthy := h.onUnhealthy
		h.mu.Unlock()
		onUnhealthy()
		h.mu.Lock()
	}
}

// waitForHealthCheckResponse 等待健康检查响应（循环读取，跳过非目标消息）。
// 对齐 Python: SpawnedProcessHandle._wait_for_health_check_response()
//
// 持续读取 stdout 直到拿到 HEALTH_CHECK_RESPONSE、EOF 或超时。
// streaming 场景下 STREAM_CHUNK 等消息会被跳过。
func (h *SpawnedProcessHandle) waitForHealthCheckResponse(ctx context.Context, messageID string) (Message, error) {
	for {
		msgCh := make(chan Message, 1)
		errCh := make(chan error, 1)

		go func() {
			msg, err := ReadMessage(h.stdout)
			if err != nil {
				errCh <- err
				return
			}
			msgCh <- msg
		}()

		select {
		case msg := <-msgCh:
			if msg.Type == MessageTypeHealthCheckResponse {
				return msg, nil
			}
			// 跳过非健康检查消息（如 STREAM_CHUNK/OUTPUT/DONE 等），继续读
			logger.Debug(logComponent).
				Str("process_id", h.processID).
				Str("message_type", msg.Type.String()).
				Msg("健康检查等待期间收到非目标消息，跳过")
			continue

		case err := <-errCh:
			if err == io.EOF {
				return Message{}, fmt.Errorf("子进程关闭，读取健康检查响应失败")
			}
			return Message{}, fmt.Errorf("读取健康检查响应失败: %w", err)

		case <-ctx.Done():
			return Message{}, fmt.Errorf("健康检查响应超时")
		}
	}
}

// waitForShutdownAck 等待 SHUTDOWN_ACK 或 DONE 消息（循环读取，跳过非目标消息）。
// 对齐 Python: SpawnedProcessHandle._wait_for_shutdown_ack()
//
// 持续读取 stdout 直到拿到 SHUTDOWN_ACK 或 DONE（Agent 自然完成也视为可退出）。
// 其他消息（如 STREAM_CHUNK/OUTPUT/ERROR 等）会被跳过。
func (h *SpawnedProcessHandle) waitForShutdownAck(ctx context.Context) bool {
	shutdownTimeout := h.config.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = DefaultShutdownTimeout
	}

	ackCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	for {
		msgCh := make(chan Message, 1)
		errCh := make(chan error, 1)

		go func() {
			msg, err := ReadMessage(h.stdout)
			if err != nil {
				errCh <- err
				return
			}
			msgCh <- msg
		}()

		select {
		case msg := <-msgCh:
			if msg.Type == MessageTypeShutdownAck {
				return true
			}
			if msg.Type == MessageTypeDone {
				// Agent 自然完成也视为可正常退出
				logger.Info(logComponent).
					Str("process_id", h.processID).
					Msg("等待 SHUTDOWN_ACK 期间收到 DONE，视为正常退出")
				return true
			}
			// 跳过其他消息，继续读
			logger.Debug(logComponent).
				Str("process_id", h.processID).
				Str("message_type", msg.Type.String()).
				Msg("等待 SHUTDOWN_ACK 期间收到非目标消息，跳过")
			continue

		case <-errCh:
			return false

		case <-ackCtx.Done():
			return false
		}
	}
}

// forceTerminate 强制终止子进程（SIGTERM → 3s → SIGKILL）。
// 对齐 Python: SpawnedProcessHandle._force_terminate()
//
// 返回 (graceful, error)：graceful=true 表示优雅退出，false 表示强制终止。
func (h *SpawnedProcessHandle) forceTerminate() (bool, error) {
	if !h.IsAlive() {
		return true, nil
	}

	// 设置关闭标志
	h.mu.Lock()
	h.shutdownRequested = true
	h.mu.Unlock()

	// 停止健康检查
	_ = h.StopHealthCheck()

	logger.Info(logComponent).
		Str("process_id", h.processID).
		Msg("发送 SIGTERM 终止子进程")

	// Unix: SIGTERM → 等3s → SIGKILL
	if !h.isWindows() {
		_ = h.cmd.Process.Signal(syscall.SIGTERM)

		waitCh := make(chan struct{})
		go func() {
			_, _ = h.cmd.Process.Wait()
			close(waitCh)
		}()

		select {
		case <-waitCh:
			logger.Info(logComponent).
				Str("process_id", h.processID).
				Msg("子进程在 SIGTERM 后正常退出")
			return false, nil
		case <-time.After(ForceTerminateGracePeriod):
			logger.Warn(logComponent).
				Str("process_id", h.processID).
				Msg("子进程未在宽限期内退出，执行 SIGKILL")
			killErr := h.cmd.Process.Kill()
			if killErr != nil {
				return false, fmt.Errorf("SIGKILL 子进程 %s 失败: %w", h.processID, killErr)
			}
			return false, nil
		}
	}

	// Windows: 直接 Kill（Windows 没有 SIGTERM）
	killErr := h.cmd.Process.Kill()
	if killErr != nil {
		return false, fmt.Errorf("强制终止子进程 %s 失败: %w", h.processID, killErr)
	}
	return false, nil
}

// isWindows 判断当前平台是否为 Windows。
func (h *SpawnedProcessHandle) isWindows() bool {
	return runtime.GOOS == "windows"
}
