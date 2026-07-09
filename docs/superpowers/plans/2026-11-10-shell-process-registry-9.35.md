# 9.35 Shell Process Registry 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Shell Process Registry（9.35），为 Shell 子进程提供会话级别的生命周期追踪与批量终止能力，完全对齐 Python 的 `shell_process_registry.py`。

**Architecture:** ShellProcessRegistry 用 `sync.Mutex` 保护 `map[string]map[*os.Process]struct{}`（session→进程集合）和 `cancelledSessions` 集合。提供 Register/Unregister/KillSession/KillSessionTree/ConsumeCancelled 五个核心方法 + 全局 DefaultRegistry 实例 + 包级便捷函数。TerminateShellProcess 实现两阶段终止（SIGTERM→3s→SIGKILL），Linux 用进程组 killpg，Windows 也对齐 Python 两阶段。

**Tech Stack:** Go 标准库 `os`/`sync`/`syscall`/`time`/`runtime`，项目内部 `internal/common/logger`，testify 断言库。

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agentcore/sys_operation/shell_process_registry.go` | ShellProcessRegistry 结构体 + 核心方法 + TerminateShellProcess + 全局实例 + 便捷函数 |
| 创建 | `internal/agentcore/sys_operation/shell_process_registry_test.go` | 单元测试 |
| 修改 | `internal/agentcore/sys_operation/sys_operation.go` | 补全 ShellOperation 接口（+ExecuteCmdStream/+ExecuteCmdBackground），补全 BaseShellOperation 空桩，新增 ExecuteCmdStreamResult/ExecuteCmdStreamChunk/ExecuteCmdBackgroundResult 类型 |
| 修改 | `internal/agentcore/sys_operation/sys_operation_test.go` | 补全 BaseShellOperation 新空桩方法的测试 |
| 修改 | `internal/agentcore/sys_operation/doc.go` | 文件目录添加 shell_process_registry.go |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.32 状态 ☐→🔄→✅（接口补全），9.35 状态 ☐→🔄→✅ |

---

### Task 1: 补全 ShellOperation 接口（9.32 范围）

**Files:**
- Modify: `internal/agentcore/sys_operation/sys_operation.go`
- Modify: `internal/agentcore/sys_operation/sys_operation_test.go`

- [ ] **Step 1: 在 sys_operation.go 中添加 ExecuteCmdStream 和 ExecuteCmdBackground 到 ShellOperation 接口**

在 `ShellOperation` 接口中，在 `ExecuteCmd` 方法之后、`ListTools` 之前添加两个新方法：

```go
// ShellOperation Shell 操作接口，定义命令执行等操作。
type ShellOperation interface {
	// ExecuteCmd 执行 Shell 命令
	ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*ExecuteCmdResult, error)
	// ExecuteCmdStream 流式执行 Shell 命令，返回命令输出块通道
	ExecuteCmdStream(ctx context.Context, command string, opts ...ShellOption) (<-chan ExecuteCmdStreamChunk, error)
	// ExecuteCmdBackground 后台执行 Shell 命令，立即返回进程 PID
	ExecuteCmdBackground(ctx context.Context, command string, opts ...ShellOption) (*ExecuteCmdBackgroundResult, error)
	// ListTools 返回 Shell 操作的工具卡片列表
	ListTools() []*tool.ToolCard
}
```

- [ ] **Step 2: 在 sys_operation.go 中添加新的结果类型**

在 `ExecuteCmdResult` 结构体之后、`ExecuteCodeResult` 结构体之前添加：

```go
// ExecuteCmdStreamChunk Shell 命令流式输出块
type ExecuteCmdStreamChunk struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
	// Type 输出类型（stdout/stderr/exit/error）
	Type string
	// Text 输出文本
	Text string
	// ChunkIndex 块序号
	ChunkIndex int
	// ExitCode 退出码（仅 exit 类型有效）
	ExitCode int
}

// ExecuteCmdBackgroundResult 后台执行命令结果
type ExecuteCmdBackgroundResult struct {
	// Code 状态码
	Code int
	// Message 状态消息
	Message string
	// PID 后台进程标识
	PID int
	// Command 执行的命令
	Command string
	// Cwd 工作目录
	Cwd string
}
```

- [ ] **Step 3: 在 sys_operation.go 中添加 BaseShellOperation 的空桩方法**

在 `BaseShellOperation` 的 `ExecuteCmd` 空桩之后添加：

```go
// ExecuteCmdStream 流式执行命令（BaseShellOperation 空实现）
func (b *BaseShellOperation) ExecuteCmdStream(_ context.Context, _ string, _ ...ShellOption) (<-chan ExecuteCmdStreamChunk, error) {
	return nil, fmt.Errorf("未实现: ExecuteCmdStream")
}

// ExecuteCmdBackground 后台执行命令（BaseShellOperation 空实现）
func (b *BaseShellOperation) ExecuteCmdBackground(_ context.Context, _ string, _ ...ShellOption) (*ExecuteCmdBackgroundResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCmdBackground")
}
```

- [ ] **Step 4: 更新 sys_operation_test.go 中的 BaseShellOperation 桩方法测试**

在 `TestBaseShellOperation_桩方法返回错误` 函数中，在 `ExecuteCmd` 断言之后添加：

```go
	resStream, err := b.ExecuteCmdStream(ctx, "ls")
	assert.Nil(t, resStream)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")

	resBg, err := b.ExecuteCmdBackground(ctx, "ls")
	assert.Nil(t, resBg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")
```

- [ ] **Step 5: 运行测试验证编译和测试通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -count=1`
Expected: PASS，所有现有测试 + 新增测试通过

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/sys_operation/sys_operation.go internal/agentcore/sys_operation/sys_operation_test.go
git commit -m "feat(sys_operation): 补全 ShellOperation 接口，添加 ExecuteCmdStream/ExecuteCmdBackground"
```

---

### Task 2: 实现 ShellProcessRegistry 核心结构体与方法

**Files:**
- Create: `internal/agentcore/sys_operation/shell_process_registry.go`

- [ ] **Step 1: 创建 shell_process_registry.go，写入核心结构体和所有方法**

```go
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
	// terminateGracePeriod 优雅终止等待时间（秒），对齐 Python 的 3 秒
	terminateGracePeriod = 3 * time.Second
	// forceKillWait 强制杀死后等待时间（秒），对齐 Python 的 1 秒
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
	// Windows: Signal(0) 不可用，通过 WaitPid 非阻塞检查
	// 注意：os.Process.Wait() 只能调用一次，此处保守返回 false
	// 让后续 terminate 路径自行处理
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
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/sys_operation/...`
Expected: 编译成功，无错误

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/sys_operation/shell_process_registry.go
git commit -m "feat(sys_operation): 实现 ShellProcessRegistry 9.35，会话级进程追踪与两阶段终止"
```

---

### Task 3: 实现 ShellProcessRegistry 单元测试

**Files:**
- Create: `internal/agentcore/sys_operation/shell_process_registry_test.go`

- [ ] **Step 1: 创建 shell_process_registry_test.go**

```go
package sys_operation

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── NewShellProcessRegistry ────────────────────────────

// TestNewShellProcessRegistry 测试创建空注册表
func TestNewShellProcessRegistry(t *testing.T) {
	r := NewShellProcessRegistry()
	assert.NotNil(t, r)
	assert.Empty(t, r.processes)
	assert.Empty(t, r.cancelledSessions)
}

// ──────────────────────────── Register / Unregister ────────────────────────────

// TestShellProcessRegistry_Register 测试注册进程
func TestShellProcessRegistry_Register(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("session1", proc)

	r.mu.Lock()
	bucket, ok := r.processes["session1"]
	r.mu.Unlock()
	assert.True(t, ok)
	_, exists := bucket[proc]
	assert.True(t, exists)
}

// TestShellProcessRegistry_Register_空SessionID 测试空 sessionID 被忽略
func TestShellProcessRegistry_Register_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("", proc)
	r.Register("   ", proc)

	r.mu.Lock()
	assert.Empty(t, r.processes)
	r.mu.Unlock()
}

// TestShellProcessRegistry_Register_nil进程 测试 nil 进程被忽略
func TestShellProcessRegistry_Register_nil进程(t *testing.T) {
	r := NewShellProcessRegistry()
	r.Register("session1", nil)

	r.mu.Lock()
	assert.Empty(t, r.processes)
	r.mu.Unlock()
}

// TestShellProcessRegistry_Register_同一会话多进程 测试同一会话注册多个进程
func TestShellProcessRegistry_Register_同一会话多进程(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	defer proc1.Kill()
	proc2 := startSleepProcess(t)
	defer proc2.Kill()

	r.Register("session1", proc1)
	r.Register("session1", proc2)

	r.mu.Lock()
	bucket := r.processes["session1"]
	r.mu.Unlock()
	assert.Len(t, bucket, 2)
}

// TestShellProcessRegistry_Register_幂等 测试重复注册同一进程为幂等操作
func TestShellProcessRegistry_Register_幂等(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("session1", proc)
	r.Register("session1", proc)

	r.mu.Lock()
	bucket := r.processes["session1"]
	r.mu.Unlock()
	assert.Len(t, bucket, 1)
}

// TestShellProcessRegistry_Unregister 测试注销进程
func TestShellProcessRegistry_Unregister(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("session1", proc)
	r.Unregister("session1", proc)

	r.mu.Lock()
	_, ok := r.processes["session1"]
	r.mu.Unlock()
	assert.False(t, ok, "桶空后应自动清理 key")
}

// TestShellProcessRegistry_Unregister_部分注销 测试部分注销后桶仍存在
func TestShellProcessRegistry_Unregister_部分注销(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	defer proc1.Kill()
	proc2 := startSleepProcess(t)
	defer proc2.Kill()

	r.Register("session1", proc1)
	r.Register("session1", proc2)
	r.Unregister("session1", proc1)

	r.mu.Lock()
	bucket := r.processes["session1"]
	r.mu.Unlock()
	assert.Len(t, bucket, 1)
	_, exists := bucket[proc2]
	assert.True(t, exists)
}

// TestShellProcessRegistry_Unregister_不存在的会话 测试注销不存在的会话不 panic
func TestShellProcessRegistry_Unregister_不存在的会话(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	// 不应 panic
	r.Unregister("nonexistent", proc)
}

// ──────────────────────────── KillSession ────────────────────────────

// TestShellProcessRegistry_KillSession 测试终止会话所有进程
func TestShellProcessRegistry_KillSession(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)

	r.Register("session1", proc)
	killed := r.KillSession("session1")
	assert.Equal(t, 1, killed)

	// 验证会话已被标记为已取消
	assert.True(t, r.ConsumeCancelled("session1"))
}

// TestShellProcessRegistry_KillSession_空SessionID 测试空 sessionID 返回 0
func TestShellProcessRegistry_KillSession_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	killed := r.KillSession("")
	assert.Equal(t, 0, killed)
}

// TestShellProcessRegistry_KillSession_不存在的会话 测试终止不存在的会话返回 0
func TestShellProcessRegistry_KillSession_不存在的会话(t *testing.T) {
	r := NewShellProcessRegistry()
	killed := r.KillSession("nonexistent")
	assert.Equal(t, 0, killed)
}

// TestShellProcessRegistry_KillSession_已退出进程 测试终止已退出的进程返回 0
func TestShellProcessRegistry_KillSession_已退出进程(t *testing.T) {
	r := NewShellProcessRegistry()

	// 启动一个会立即退出的进程
	cmd := exec.Command("true")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", "0")
	}
	require.NoError(t, cmd.Start())
	cmd.Wait() // 等待进程退出

	// 进程已退出，KillSession 不会真正终止它
	r.Register("session1", cmd.Process)
	killed := r.KillSession("session1")
	assert.Equal(t, 0, killed)
}

// ──────────────────────────── KillSessionTree ────────────────────────────

// TestShellProcessRegistry_KillSessionTree 测试终止会话树（前缀匹配）
func TestShellProcessRegistry_KillSessionTree(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	proc2 := startSleepProcess(t)
	proc3 := startSleepProcess(t)

	// 注册主会话和两个子会话
	r.Register("parent", proc1)
	r.Register("parent_child1", proc2)
	r.Register("parent_child2", proc3)

	killed := r.KillSessionTree("parent")
	assert.Equal(t, 3, killed)

	// 验证所有会话已被标记为已取消
	assert.True(t, r.ConsumeCancelled("parent"))
	assert.True(t, r.ConsumeCancelled("parent_child1"))
	assert.True(t, r.ConsumeCancelled("parent_child2"))
}

// TestShellProcessRegistry_KillSessionTree_不匹配前缀 测试不匹配的会话不受影响
func TestShellProcessRegistry_KillSessionTree_不匹配前缀(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	proc2 := startSleepProcess(t)
	defer proc2.Kill()

	r.Register("abc", proc1)
	r.Register("abcd", proc2) // "abcd" 不以 "abc_" 开头，不应被匹配

	killed := r.KillSessionTree("abc")
	assert.Equal(t, 1, killed)

	// "abcd" 不应被取消
	assert.False(t, r.ConsumeCancelled("abcd"))
}

// TestShellProcessRegistry_KillSessionTree_空SessionID 测试空 sessionID 返回 0
func TestShellProcessRegistry_KillSessionTree_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	killed := r.KillSessionTree("")
	assert.Equal(t, 0, killed)
}

// ──────────────────────────── ConsumeCancelled ────────────────────────────

// TestShellProcessRegistry_ConsumeCancelled 测试消费取消标记
func TestShellProcessRegistry_ConsumeCancelled(t *testing.T) {
	r := NewShellProcessRegistry()

	// 未取消的会话
	assert.False(t, r.ConsumeCancelled("session1"))

	// 标记为已取消
	r.KillSession("session1")
	assert.True(t, r.ConsumeCancelled("session1"))

	// 消费后再次检查返回 false
	assert.False(t, r.ConsumeCancelled("session1"))
}

// TestShellProcessRegistry_ConsumeCancelled_空SessionID 测试空 sessionID 返回 false
func TestShellProcessRegistry_ConsumeCancelled_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	assert.False(t, r.ConsumeCancelled(""))
	assert.False(t, r.ConsumeCancelled("   "))
}

// ──────────────────────────── 全局便捷函数 ────────────────────────────

// TestRegisterShellProcess 测试全局便捷函数 RegisterShellProcess
func TestRegisterShellProcess(t *testing.T) {
	// 使用临时 registry 避免影响全局
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	RegisterShellProcess("global_session", proc)

	DefaultRegistry.mu.Lock()
	_, ok := DefaultRegistry.processes["global_session"]
	DefaultRegistry.mu.Unlock()
	assert.True(t, ok)
}

// TestKillShellProcessesForSession 测试全局便捷函数 KillShellProcessesForSession
func TestKillShellProcessesForSession(t *testing.T) {
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc := startSleepProcess(t)

	RegisterShellProcess("global_session", proc)
	killed := KillShellProcessesForSession("global_session")
	assert.Equal(t, 1, killed)
}

// TestKillShellProcessesForSessionTree 测试全局便捷函数 KillShellProcessesForSessionTree
func TestKillShellProcessesForSessionTree(t *testing.T) {
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	proc2 := startSleepProcess(t)

	RegisterShellProcess("parent", proc1)
	RegisterShellProcess("parent_sub", proc2)
	killed := KillShellProcessesForSessionTree("parent")
	assert.Equal(t, 2, killed)
}

// TestConsumeShellSessionCancelled 测试全局便捷函数 ConsumeShellSessionCancelled
func TestConsumeShellSessionCancelled(t *testing.T) {
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc := startSleepProcess(t)

	RegisterShellProcess("session", proc)
	KillShellProcessesForSession("session")
	assert.True(t, ConsumeShellSessionCancelled("session"))
	assert.False(t, ConsumeShellSessionCancelled("session"))
}

// ──────────────────────────── TerminateShellProcess ────────────────────────────

// TestTerminateShellProcess_成功终止 测试两阶段终止正在运行的进程
func TestTerminateShellProcess_成功终止(t *testing.T) {
	proc := startSleepProcess(t)
	result := TerminateShellProcess(proc)
	assert.True(t, result)
}

// TestTerminateShellProcess_nil进程 测试 nil 进程返回 false
func TestTerminateShellProcess_nil进程(t *testing.T) {
	result := TerminateShellProcess(nil)
	assert.False(t, result)
}

// TestTerminateShellProcess_已退出进程 测试已退出进程返回 false
func TestTerminateShellProcess_已退出进程(t *testing.T) {
	cmd := exec.Command("true")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", "0")
	}
	require.NoError(t, cmd.Start())
	cmd.Wait()

	result := TerminateShellProcess(cmd.Process)
	assert.False(t, result)
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// startSleepProcess 启动一个长时间休眠的子进程（用于测试终止），调用方负责清理。
func startSleepProcess(t *testing.T) *os.Process {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "timeout", "/t", "300", "/nobreak")
	} else {
		cmd = exec.Command("sleep", "300")
	}
	// 设置进程组隔离（POSIX: 新 session；Windows: 新进程组）
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	return cmd.Process
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -run "ShellProcessRegistry|RegisterShell|KillShell|ConsumeShell|TerminateShell" -count=1`
Expected: PASS，所有 ShellProcessRegistry 测试通过

- [ ] **Step 3: 运行完整包测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/sys_operation/shell_process_registry_test.go
git commit -m "test(sys_operation): 添加 ShellProcessRegistry 单元测试 9.35"
```

---

### Task 4: 更新 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/sys_operation/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录，添加 shell_process_registry.go**

将文件目录部分从：

```
//	sys_operation/
//	├── doc.go                   # 包文档
//	├── sys_operation.go         # SysOperation/FsOperation/ShellOperation/CodeOperation 接口 + 枚举
//	└── sys_operation_card.go   # SysOperationCard + WorkConfig 类型
```

更新为：

```
//	sys_operation/
//	├── doc.go                         # 包文档
//	├── sys_operation.go               # SysOperation/FsOperation/ShellOperation/CodeOperation 接口 + 枚举 + 结果类型
//	├── sys_operation_card.go         # SysOperationCard + WorkConfig 类型
//	└── shell_process_registry.go     # ShellProcessRegistry 会话级进程追踪 + 两阶段终止
```

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/sys_operation/doc.go
git commit -m "docs(sys_operation): 更新 doc.go 添加 shell_process_registry.go"
```

---

### Task 5: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.32 和 9.35 的状态标记**

将 9.32 行的 `☐` 改为 `✅`（接口补全完成），将 9.35 行的 `☐` 改为 `✅`。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 9.32/9.35 状态为已完成"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 运行完整包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -count=1`
Expected: PASS，所有测试通过

- [ ] **Step 2: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/sys_operation/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 验证编译无错误**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/sys_operation/...`
Expected: 编译成功
