package sys_operation

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

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

// shellSessionIDKey context key 用于传递 Shell session ID。
// 对齐 Python _shell_session_id: contextvars.ContextVar。
type shellSessionIDKey struct{}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// terminateGracePeriod 优雅终止等待时间，对齐 Python 的 3 秒
	terminateGracePeriod = 3 * time.Second
	// forceKillWait 强制杀死后等待时间，对齐 Python 的 1 秒
	forceKillWait = 1 * time.Second
)

// logComponent 日志组件常量，对齐 Python 的 sys_operation_logger
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// DefaultRegistry 全局 Shell 进程注册表实例
var DefaultRegistry = NewShellProcessRegistry()

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
// Linux：先 syscall.Kill(-pgid, SIGTERM) → 等待 3s → 再 syscall.Kill(-pgid, SIGKILL)
// Windows：先 proc.Signal(os.Interrupt) → 等待 3s → 再 proc.Kill()
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

	return terminateShellProcessPlatform(proc)
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

// SetShellSessionID 将 session ID 绑定到 context。
// 对齐 Python set_shell_session_id。
func SetShellSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, shellSessionIDKey{}, sessionID)
}

// GetShellSessionID 从 context 获取 session ID。
// 对齐 Python get_shell_session_id。
func GetShellSessionID(ctx context.Context) string {
	if v, ok := ctx.Value(shellSessionIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ResetShellSessionID 重置 context 中的 session ID。
// 对齐 Python reset_shell_session_id（Go 中通过覆盖 WithValue 实现）。
func ResetShellSessionID(ctx context.Context) context.Context {
	return context.WithValue(ctx, shellSessionIDKey{}, "")
}

// ResolveShellSessionID 解析 session ID：先从 context 取，再 fallback 到空。
// 对齐 Python resolve_shell_session_id：先从 contextvars 取，fallback 到 get_session_id()。
// Go 版本简化了 fallback 路径：当前 logger 包无 trace ID 机制，后续可补充。
func ResolveShellSessionID(ctx context.Context) string {
	sid := strings.TrimSpace(GetShellSessionID(ctx))
	if sid != "" {
		return sid
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isProcessExited 检查进程是否已退出（非阻塞）
func isProcessExited(proc *os.Process) bool {
	return isProcessExitedPlatform(proc)
}
