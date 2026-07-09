# 9.35 Shell Process Registry 设计

## 概述

Shell Process Registry 是 Shell 子进程的生命周期追踪器。当 Agent 执行 shell 命令时会产生 `os/exec` 子进程，Registry 负责：
- 按 session_id 归类注册/注销进程
- 用户中断时批量终止该会话下所有 shell 子进程（含子 Agent 进程树）
- 标记已取消会话，防止新命令继续启动

## 在 Agent 会话流程中的位置

```
9.32 SysOperation 接口 (✅) → 9.33 LocalSysOperation (☐, ⤵️ 9.35) → 9.34 SandboxSysOperation (☐)
→ 9.35 Shell Process Registry (☐) → 9.36 JiuwenBoxProvider (☐) → 9.37 AioProvider (☐)
```

9.35 被 9.33 LocalShellOperation 依赖，LocalShellOperation 的每个命令执行方法（execute_cmd / execute_cmd_stream / execute_cmd_background）都需要在进程启动后 register、退出后 unregister。

## Python 参考

Python 源码：`openjiuwen/core/sys_operation/shell_process_registry.py`

核心机制：
- `threading.Lock` 保护 `_processes: dict[str, set[ProcessHandle]]` 和 `_cancelled_sessions: set[str]`
- `ProcessHandle = Union[subprocess.Popen, asyncio.subprocess.Process]`
- `contextvars.ContextVar` 传递 session_id（Go 改为显式参数）
- `terminate_shell_process()`：POSIX 先 `os.killpg(SIGTERM)` → wait 3s → `os.killpg(SIGKILL)`；Windows 先 `proc.terminate()` → wait 3s → `proc.kill()`
- `kill_session_tree()`：前缀匹配 `{session_id}_*` 遍历 map keys
- 全局单例 `SHELL_PROCESS_REGISTRY` + 模块级便捷函数

## Go 设计

### 文件

- `internal/agentcore/sys_operation/shell_process_registry.go` — 核心实现
- `internal/agentcore/sys_operation/shell_process_registry_test.go` — 单元测试

### 核心结构体

```go
// ShellProcessRegistry 会话级别的 Shell 子进程注册表，追踪在途进程以支持用户中断时批量终止。
type ShellProcessRegistry struct {
    mu                sync.Mutex
    processes         map[string]map[*os.Process]struct{}  // sessionID → 进程集合
    cancelledSessions map[string]struct{}                   // 已取消的会话集合
}
```

### 核心方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `NewShellProcessRegistry` | `() *ShellProcessRegistry` | 创建空注册表 |
| `Register` | `(r *ShellProcessRegistry) Register(sessionID string, proc *os.Process)` | 按 sessionID 注册进程，空 sessionID 忽略 |
| `Unregister` | `(r *ShellProcessRegistry) Unregister(sessionID string, proc *os.Process)` | 注销进程，桶空后自动清理 key |
| `KillSession` | `(r *ShellProcessRegistry) KillSession(sessionID string) int` | 终止该会话所有进程，标记为已取消，返回杀掉数量 |
| `KillSessionTree` | `(r *ShellProcessRegistry) KillSessionTree(sessionID string) int` | 终止该会话 + 前缀 `{sessionID}_*` 子会话的所有进程 |
| `ConsumeCancelled` | `(r *ShellProcessRegistry) ConsumeCancelled(sessionID string) bool` | 检查并消费取消标记（一次性） |

### 进程终止函数

```go
// TerminateShellProcess 两阶段终止 shell 进程。
// Linux: syscall.Kill(-pgid, SIGTERM) → wait 3s → syscall.Kill(-pgid, SIGKILL)
// Windows: proc.Signal(os.Interrupt) → wait 3s → proc.Kill()
// 返回 true 表示成功终止，false 表示进程已退出或终止失败。
func TerminateShellProcess(proc *os.Process) bool
```

**Windows 对齐 Python**：也是两阶段，先尝试优雅终止（`Signal(os.Interrupt)` 等价于 SIGTERM），等待 3 秒超时后强制 `Kill()`。

**日志**：终止失败/超时时用 `logger.Warn(logComponent)` 记录，对齐 Python 的 `sys_operation_logger.warning`。

### 全局实例 + 便捷函数

```go
// DefaultRegistry 全局 Shell 进程注册表实例
var DefaultRegistry = NewShellProcessRegistry()

// RegisterShellProcess 向全局注册表注册 Shell 进程
func RegisterShellProcess(sessionID string, proc *os.Process)

// UnregisterShellProcess 从全局注册表注销 Shell 进程
func UnregisterShellProcess(sessionID string, proc *os.Process)

// KillShellProcessesForSession 终止指定会话的所有 Shell 进程
func KillShellProcessesForSession(sessionID string) int

// KillShellProcessesForSessionTree 终止指定会话及其子会话的所有 Shell 进程
func KillShellProcessesForSessionTree(sessionID string) int

// ConsumeShellSessionCancelled 检查并消费会话取消标记
func ConsumeShellSessionCancelled(sessionID string) bool
```

### 日志

```go
const logComponent = logger.ComponentAgentCore
```

对齐 Python 的 `sys_operation_logger`（`openjiuwen/core/common/logging/`），sys_operation 属于 agentcore 层。

### kill_session_tree 前缀匹配

加锁后遍历 `processes` map 的所有 key，筛选 `key == sessionID || strings.HasPrefix(key, sessionID+"_")`。O(n) 但 n 极小（同时活跃会话通常几十个），完全对齐 Python 实现。

## 前置补全（9.32 范围内）

ShellOperation 接口需补全 `ExecuteCmdStream` 和 `ExecuteCmdBackground` 方法：

```go
type ShellOperation interface {
    ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*ExecuteCmdResult, error)
    ExecuteCmdStream(ctx context.Context, command string, opts ...ShellOption) (<-chan ExecuteCmdStreamChunk, error)  // 新增
    ExecuteCmdBackground(ctx context.Context, command string, opts ...ShellOption) (*ExecuteCmdBackgroundResult, error)  // 新增
    ListTools() []*tool.ToolCard
}
```

## 回填标记

9.33 LocalShellOperation 实现时，在以下位置标记 `⤵️ 9.35`：
- `_trackShellProcess(proc)` 调用 `DefaultRegistry.Register(sessionID, proc)` 处
- `_untrackShellProcess(sessionID, proc)` 调用 `DefaultRegistry.Unregister(sessionID, proc)` 处
- factory.go L426-428 `⤵️ 9.32 回填` 处
- builder.go L332-335 `⤵️ 9.32 回填` 处

## 依赖

- OperationRegistry（操作注册表）：9.33 需要，但 9.35 不直接依赖。9.33 实现时通过 init 注册，当前可用 stub + 注释标记。

## doc.go 更新

添加 `shell_process_registry.go` 条目到文件目录树。
