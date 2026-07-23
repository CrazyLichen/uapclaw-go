package sys_operation

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

type ShellProcessRegistry struct {
	// mu 互斥锁，保护 processes 和 cancelledSessions
	mu sync.Mutex
	// processes sessionID → 进程集合
	processes map[string]map[*os.Process]struct{}
	// stdinPipes sessionID → 进程 → stdin pipe
	stdinPipes map[string]map[*os.Process]io.Writer
	// cancelledSessions 已取消的会话集合
	cancelledSessions map[string]struct{}
}

// shellSessionIDKey context key 用于传递 Shell session ID。
// 对齐 Python _shell_session_id: contextvars.ContextVar。
type shellSessionIDKey struct{}

// ProcessInfo 进程信息
type ProcessInfo struct {
	// SessionID 会话标识
	SessionID string
	// PID 进程 ID
	PID int
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// terminateGracePeriod 优雅终止等待时间，对齐 Python 的 3 秒
	terminateGracePeriod = 3 * time.Second
	// forceKillWait 强制杀死后等待时间，对齐 Python 的 1 秒
	forceKillWait = 1 * time.Second
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var DefaultRegistry = NewShellProcessRegistry()

// ──────────────────────────── 导出函数 ────────────────────────────

func NewShellProcessRegistry() *ShellProcessRegistry {
	return &ShellProcessRegistry{
		processes:         make(map[string]map[*os.Process]struct{}),
		stdinPipes:        make(map[string]map[*os.Process]io.Writer),
		cancelledSessions: make(map[string]struct{}),
	}
}

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

// RegisterWithStdin 注册进程同时保存 stdin pipe 引用。
// 对齐 Python ShellProcessRegistry.track + stdin pipe 追踪。
func (r *ShellProcessRegistry) RegisterWithStdin(sessionID string, proc *os.Process, stdin io.Writer) {
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
	if stdin != nil {
		pipes, ok := r.stdinPipes[sid]
		if !ok {
			pipes = make(map[*os.Process]io.Writer)
			r.stdinPipes[sid] = pipes
		}
		pipes[proc] = stdin
	}
}

// GetStdinPipe 获取指定 session 和进程的 stdin pipe。
func (r *ShellProcessRegistry) GetStdinPipe(sessionID string, proc *os.Process) io.Writer {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || proc == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pipes, ok := r.stdinPipes[sid]
	if !ok {
		return nil
	}
	return pipes[proc]
}

// ListProcesses 返回所有已注册进程信息。
// 对齐 Python ShellProcessRegistry.list_processes。
func (r *ShellProcessRegistry) ListProcesses() []ProcessInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []ProcessInfo
	for sid, procs := range r.processes {
		for proc := range procs {
			result = append(result, ProcessInfo{
				SessionID: sid,
				PID:       proc.Pid,
			})
		}
	}
	return result
}

// WriteStdinForSession 向指定 session 的所有进程写入 stdin。
// 对齐 Python ShellProcessRegistry.write_stdin：遍历 stdinPipes 写入。
// 返回成功写入的进程数和第一个遇到的错误。
func (r *ShellProcessRegistry) WriteStdinForSession(sessionID string, data []byte) (int, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return 0, nil
	}
	r.mu.Lock()
	procs, ok := r.processes[sid]
	if !ok {
		r.mu.Unlock()
		return 0, nil
	}
	pipes := r.stdinPipes[sid]
	// 复制引用以避免持锁时间过长
	procList := make([]*os.Process, 0, len(procs))
	pipeMap := make(map[*os.Process]io.Writer)
	for proc := range procs {
		procList = append(procList, proc)
		if pipes != nil {
			pipeMap[proc] = pipes[proc]
		}
	}
	r.mu.Unlock()

	written := 0
	var firstErr error
	for _, proc := range procList {
		if stdin, ok := pipeMap[proc]; ok && stdin != nil {
			if _, err := stdin.Write(data); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				written++
			}
		}
	}
	return written, firstErr
}

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

func RegisterShellProcess(sessionID string, proc *os.Process) {
	DefaultRegistry.Register(sessionID, proc)
}

func UnregisterShellProcess(sessionID string, proc *os.Process) {
	DefaultRegistry.Unregister(sessionID, proc)
}

func KillShellProcessesForSession(sessionID string) int {
	return DefaultRegistry.KillSession(sessionID)
}

func KillShellProcessesForSessionTree(sessionID string) int {
	return DefaultRegistry.KillSessionTree(sessionID)
}

func ConsumeShellSessionCancelled(sessionID string) bool {
	return DefaultRegistry.ConsumeCancelled(sessionID)
}

// SetShellSessionID 将 session ID 绑定到 context。
// 对齐 Python set_shell_session_id。
func SetShellSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, shellSessionIDKey{}, sessionID)
}

func GetShellSessionID(ctx context.Context) string {
	if v, ok := ctx.Value(shellSessionIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ClearShellSessionID 清除 context 中的 session ID，设为空字符串。
// 对齐 Python reset_shell_session_id：Go 中无 Token 回退机制，
// 调用方如需恢复旧值，应保存旧 context 后恢复。
func ClearShellSessionID(ctx context.Context) context.Context {
	return context.WithValue(ctx, shellSessionIDKey{}, "")
}

// ResolveShellSessionID 解析 session ID：先从 context 取，再 fallback 到 trace_id。
// 对齐 Python resolve_shell_session_id：先从 contextvars 取，fallback 到 get_session_id()。
//
// TODO(#通用): 补充 fallback 到 trace_id 的逻辑。Python 在 shell_session_id 为空时，
// 会从 logging.utils.get_session_id() 获取 trace_id 并排除 "default_trace_id" 哨兵值。
// Go 侧等 logger 包实现 GetTraceID(context.Context) 后，在此处补充等价 fallback：
//
//	traceID := logger.GetTraceID(ctx)
//	if traceID != "" && traceID != "default_trace_id" {
//	    return traceID
//	}
func ResolveShellSessionID(ctx context.Context) string {
	sid := strings.TrimSpace(GetShellSessionID(ctx))
	if sid != "" {
		return sid
	}
	// TODO(#通用): fallback 到 trace_id（等 logger 包实现 GetTraceID 后补充）
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isProcessExited 检查进程是否已退出（非阻塞）
func isProcessExited(proc *os.Process) bool {
	return isProcessExitedPlatform(proc)
}
