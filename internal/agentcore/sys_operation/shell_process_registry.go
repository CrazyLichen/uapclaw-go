package sys_operation

import (
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// terminateGracePeriod 优雅终止等待时间，对齐 Python 的 3 秒
	terminateGracePeriod = 3 * time.Second
	// forceKillWait 强制杀死后等待时间，对齐 Python 的 1 秒
	forceKillWait = 1 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件常量，对齐 Python 的 sys_operation_logger
const logComponent = logger.ComponentAgentCore

// DefaultRegistry 全局 Shell 进程注册表实例
var DefaultRegistry = NewShellProcessRegistry()

// ──────────────────────────── 结构体 ────────────────────────────

// ShellProcessRegistry 会话级别的 Shell 子进程注册表，追踪在途进程以支持用户中断时批量终止。
type ShellProcessRegistry struct {
	// mu 互斥锁，保护 processes 和 cancelledSessions
	mu sync.Mutex
	// processes sessionID → 进程集合
	processes map[string]map[*os.Process]struct{}
	// cancelledSessions 已取消的会话集合
	cancelledSessions map[string]struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewShellProcessRegistry 创建空的 Shell 进程注册表。
func NewShellProcessRegistry() *ShellProcessRegistry {
	return &ShellProcessRegistry{
		processes:         make(map[string]map[*os.Process]struct{}),
		cancelledSessions: make(map[string]struct{}),
	}
}

// Register 按 sessionID 注册 Shell 进程。空 sessionID 或 nil proc 时忽略。
func (r *ShellProcessRegistry) Register(sessionID string, proc *os.Process) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || proc == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket, ok := r.processes[sid]
	if !ok {
		bucket = make(map[*os.Process]struct{})
		r.processes[sid] = bucket
	}
	bucket[proc] = struct{}{}
}

// Unregister 从注册表注销 Shell 进程。桶空后自动清理 key。
func (r *ShellProcessRegistry) Unregister(sessionID string, proc *os.Process) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || proc == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket, ok := r.processes[sid]
	if !ok {
		return
	}
	delete(bucket, proc)
	if len(bucket) == 0 {
		delete(r.processes, sid)
	}
}

// KillSession 终止指定会话的所有 Shell 进程，标记会话为已取消，返回杀掉的进程数量。
func (r *ShellProcessRegistry) KillSession(sessionID string) int {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return 0
	}
	r.mu.Lock()
	r.cancelledSessions[sid] = struct{}{}
	procs := make([]*os.Process, 0, len(r.processes[sid]))
	for proc := range r.processes[sid] {
		procs = append(procs, proc)
	}
	delete(r.processes, sid)
	r.mu.Unlock()

	killed := 0
	for _, proc := range procs {
		if TerminateShellProcess(proc) {
			killed++
		}
	}
	return killed
}

// KillSessionTree 终止指定会话及其子会话（前缀 {sessionID}_* 匹配）的所有 Shell 进程。
func (r *ShellProcessRegistry) KillSessionTree(sessionID string) int {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return 0
	}
	prefix := sid + "_"

	// 第一阶段：加锁收集匹配的 key 并标记为已取消
	r.mu.Lock()
	matchingKeys := make([]string, 0)
	for key := range r.processes {
		if key == sid || strings.HasPrefix(key, prefix) {
			matchingKeys = append(matchingKeys, key)
		}
	}
	for _, key := range matchingKeys {
		r.cancelledSessions[key] = struct{}{}
	}
	r.mu.Unlock()

	// 第二阶段：逐个 key 取出进程并终止
	killed := 0
	for _, key := range matchingKeys {
		r.mu.Lock()
		procs := make([]*os.Process, 0, len(r.processes[key]))
		for proc := range r.processes[key] {
			procs = append(procs, proc)
		}
		delete(r.processes, key)
		r.mu.Unlock()

		for _, proc := range procs {
			if TerminateShellProcess(proc) {
				killed++
			}
		}
	}
	return killed
}

// ConsumeCancelled 检查并消费会话的取消标记（一次性）。返回该会话是否已被取消。
func (r *ShellProcessRegistry) ConsumeCancelled(sessionID string) bool {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cancelledSessions[sid]; !ok {
		return false
	}
	delete(r.cancelledSessions, sid)
	return true
}

// TerminateShellProcess 两阶段终止 Shell 进程。
//
// Linux: syscall.Kill(-pgid, SIGTERM) → wait 3s → syscall.Kill(-pgid, SIGKILL)
// Windows: proc.Signal(os.Interrupt) → wait 3s → proc.Kill()
//
// 返回 true 表示成功终止，false 表示进程已退出或终止失败。
// 对齐 Python: openjiuwen/core/sys_operation/shell_process_registry.py:terminate_shell_process
func TerminateShellProcess(proc *os.Process) bool {
	if proc == nil {
		return false
	}

	// 检查进程是否已退出
	if isProcessExited(proc) {
		return false
	}

	if runtime.GOOS == "windows" {
		return terminateShellProcessWindows(proc)
	}
	return terminateShellProcessPOSIX(proc)
}

// RegisterShellProcess 向全局注册表注册 Shell 进程。⤵️ 9.33 回填：LocalShellOperation 调用
func RegisterShellProcess(sessionID string, proc *os.Process) {
	DefaultRegistry.Register(sessionID, proc)
}

// UnregisterShellProcess 从全局注册表注销 Shell 进程。⤵️ 9.33 回填：LocalShellOperation 调用
func UnregisterShellProcess(sessionID string, proc *os.Process) {
	DefaultRegistry.Unregister(sessionID, proc)
}

// KillShellProcessesForSession 终止指定会话的所有 Shell 进程
func KillShellProcessesForSession(sessionID string) int {
	return DefaultRegistry.KillSession(sessionID)
}

// KillShellProcessesForSessionTree 终止指定会话及其子会话的所有 Shell 进程
func KillShellProcessesForSessionTree(sessionID string) int {
	return DefaultRegistry.KillSessionTree(sessionID)
}

// ConsumeShellSessionCancelled 检查并消费会话取消标记
func ConsumeShellSessionCancelled(sessionID string) bool {
	return DefaultRegistry.ConsumeCancelled(sessionID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isProcessExited 检查进程是否已退出（非阻塞）
func isProcessExited(proc *os.Process) bool {
	// 尝试用 Signal(0) 探测进程是否存活（POSIX 标准）
	if runtime.GOOS != "windows" {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return true // 进程不存在
		}
		return false
	}
	// Windows: Signal(0) 不可用，保守返回 false，让后续 terminate 路径自行处理
	return false
}

// terminateShellProcessPOSIX POSIX 平台两阶段终止：先 SIGTERM 进程组，等 3 秒后 SIGKILL
func terminateShellProcessPOSIX(proc *os.Process) bool {
	pid := proc.Pid
	if pid <= 0 {
		return false
	}

	// 阶段 1：SIGTERM 整个进程组
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		// 进程组 SIGTERM 失败，尝试单进程 Signal
		logger.Warn(logComponent).Int("pid", pid).Err(err).Msg("进程组 SIGTERM 失败，尝试单进程终止")
		if sigErr := proc.Signal(syscall.SIGTERM); sigErr != nil {
			// 单进程也失败，尝试 SIGKILL
			return forceKillPOSIX(proc)
		}
	}

	// 等待进程退出
	if waitProcessWithTimeout(proc, terminateGracePeriod) {
		return true
	}

	// 阶段 2：超时，SIGKILL 整个进程组
	return forceKillPOSIX(proc)
}

// forceKillPOSIX 强制杀死 POSIX 进程组
func forceKillPOSIX(proc *os.Process) bool {
	pid := proc.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		logger.Warn(logComponent).Int("pid", pid).Err(err).Msg("进程组 SIGKILL 失败，尝试单进程 Kill")
		if killErr := proc.Kill(); killErr != nil {
			logger.Warn(logComponent).Int("pid", pid).Err(killErr).Msg("单进程 Kill 也失败")
			return false
		}
	}
	// 等待进程退出
	waitProcessWithTimeout(proc, forceKillWait)
	return true
}

// terminateShellProcessWindows Windows 平台两阶段终止：先 Interrupt，等 3 秒后 Kill
func terminateShellProcessWindows(proc *os.Process) bool {
	// 阶段 1：发送 os.Interrupt（等价于 Python 的 proc.terminate()）
	if err := proc.Signal(os.Interrupt); err != nil {
		logger.Warn(logComponent).Int("pid", proc.Pid).Err(err).Msg("Windows Interrupt 失败，尝试直接 Kill")
		if killErr := proc.Kill(); killErr != nil {
			logger.Warn(logComponent).Int("pid", proc.Pid).Err(killErr).Msg("Windows Kill 失败")
			return false
		}
		waitProcessWithTimeout(proc, forceKillWait)
		return true
	}

	// 等待进程退出
	if waitProcessWithTimeout(proc, terminateGracePeriod) {
		return true
	}

	// 阶段 2：超时，强制 Kill
	if err := proc.Kill(); err != nil {
		logger.Warn(logComponent).Int("pid", proc.Pid).Err(err).Msg("Windows 强制 Kill 失败")
		return false
	}
	waitProcessWithTimeout(proc, forceKillWait)
	return true
}

// waitProcessWithTimeout 带超时等待进程退出。返回 true 表示进程已退出。
func waitProcessWithTimeout(proc *os.Process, timeout time.Duration) bool {
	done := make(chan struct{}, 1)
	go func() {
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(proc.Pid, &status, 0, nil)
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
