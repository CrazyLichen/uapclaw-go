# 9.33 LocalSysOperation 对齐修复计划

## 背景

9.33 LocalSysOperation 已完成初步实现，但与 Python 源码对比后发现 8 个对齐问题 + 2 个额外问题。
本文档记录所有问题的修复方案，供实施时逐项对照。

## 修复清单

### 问题 1：`Stream()` 缺失超时控制 [🔴 高]

**文件**: `local/utils.go` — `AsyncProcessHandler.Stream()`

**Python 参考**: `local/utils.py` 第 207-304 行

**现状**: Stream() 完全没有超时控制，协程会无限等待进程输出。

**修复方案**: 内部用 select+timer 替代 Python 的 `queue.get(timeout=0.1)` + `elapsed_time` 循环：

1. stdout/stderr goroutine 读 pipe → 写内部 queue channel（相当于 Python 的 asyncio.Queue）
2. 主协程 `select` 循环：
   - `case event := <-queue:` → 转发到外部 ch
   - `case <-ticker.C:` → 检查 elapsed_time >= overallTimeout，超时则 killProcessTree + 发 ERROR 事件 + close(ch)
   - `case <-ctx.Done():` → killProcessTree + 发 ERROR 事件 + close(ch)
3. 两个 reader goroutine 完成后，等 cmd.Wait()，发 EXIT 事件

**对齐要点**:
- 超时后发 `StreamEventTypeError` 事件（对齐 Python 的 `yield StreamEvent(type=ERROR, data="execution timeout after N seconds")`)
- 取消后也发 ERROR 事件（对齐 Python 的 CancelledError 处理）
- reader 异常也通过 queue 传 ERROR 事件（对齐 Python 的 _reader 异常处理）

---

### 问题 2：`Invoke()` stdout/stderr 串行收集 + 缺失 grace period [🔴 高]

**文件**: `local/utils.go` — `AsyncProcessHandler.Invoke()`

**Python 参考**: `local/utils.py` 第 113-194 行

**现状**:
- stdout/stderr 串行收集（先读完 stdout channel 再读 stderr channel）
- `<-collectDone` 没有超时保护，可能无限阻塞

**修复方案**:

1. **并行收集**: 两个 goroutine 各用局部 `strings.Builder` 收集 stdout/stderr
   ```go
   go func() {
       var stdoutBuf, stderrBuf strings.Builder
       var wg sync.WaitGroup
       wg.Add(2)
       go func() {
           defer wg.Done()
           for s := range stdoutCh { stdoutBuf.WriteString(s) }
       }()
       go func() {
           defer wg.Done()
           for s := range stderrCh { stderrBuf.WriteString(s) }
       }()
       wg.Wait()
       // 合并结果
       result.Stdout = stdoutBuf.String()
       result.Stderr = stderrBuf.String()
       close(collectDone)
   }()
   ```

2. **grace period**: kill 进程后用 `select+timer` 等待 `collectDone` 最多 30 秒
   ```go
   select {
   case <-collectDone:
       // 正常收尾
   case <-time.After(30 * time.Second):
       // 超时放弃，日志警告
   }
   ```

---

### 问题 3：`Background()` grace 检测逻辑错误 [🟡 中]

**文件**: `local/utils.go` — `AsyncProcessHandler.Background()`

**Python 参考**: `local/utils.py` 第 306-333 行

**现状**:
- 用 `time.Sleep(grace)` 等满 grace 时间才返回
- 用 `h.cmd.ProcessState != nil` 检测进程退出（不可靠，Start 后 ProcessState 未初始化）

**修复方案**: 用 goroutine+cmd.Wait+select/timer 对齐 Python：

```go
func (h *AsyncProcessHandler) Background(grace float64) (pid int, err error) {
    // ... Start 进程 ...
    pid = h.cmd.Process.Pid

    waitCh := make(chan error, 1)
    go func() { waitCh <- h.cmd.Wait() }()

    timer := time.NewTimer(time.Duration(grace * float64(time.Second)))
    defer timer.Stop()

    select {
    case waitErr := <-waitCh:
        // 进程在 grace 内退出
        if waitErr != nil {
            if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
                return pid, fmt.Errorf("process exited early with code %d", exitErr.ExitCode())
            }
        }
        // 退出码 0，也视为成功（对齐 Python）
        return pid, nil
    case <-timer.C:
        // grace 超时，进程还在运行 → 成功
        return pid, nil
    }
}
```

---

### 问题 4：`ExecuteCmdStream` 缺失 5 个逻辑步骤 [🟡 中]

**文件**: `local/shell_operation.go` — `LocalShellOperation.ExecuteCmdStream()`

**Python 参考**: `local/shell_operation.py` 第 553-734 行

**缺失步骤**:
1. `_check_allowlist` 检查
2. 框架层超时上限 `JW_EXECUTE_CMD_MAX_TIMEOUT`
3. TUI 检测 + 缓解 (`_detect_and_mitigate_tui`)
4. Windows 编码检测 + LANG 注入 (`_detect_shell_encoding` + `_get_lang_encoding`)
5. Shell 进程注册 (`_track_shell_process`) / 注销 (`_untrack_shell_process`)
6. stream=True 时应用 buffering wrapper (`_wrap_command_with_buffering`)

**修复方案**: 按顺序插入到 ExecuteCmdStream 的流程中，与 ExecuteCmd 保持一致的模式。

---

### 问题 5：`ExecuteCmd` / `ExecuteCmdBackground` 缺失部分步骤 [🟡 中]

**文件**: `local/shell_operation.go`

**ExecuteCmd 缺失** (对齐 Python shell_operation.py 第 423-551 行):
1. `_check_allowlist` 检查
2. Windows 编码检测 + LANG 注入
3. Shell 进程注册/注销
4. encoding 从 options 读取（当前硬编码 utf-8）

**ExecuteCmdBackground 缺失** (对齐 Python shell_operation.py 第 736-840 行):
1. `_check_allowlist` 检查
2. Shell 进程注册/注销（失败时注销）

**修复方案**: 与问题 4 一起，统一补齐三个方法的缺失步骤。

---

### 问题 6：`_check_allowlist` 未实现 + runConfig 未传递 [🟡 中]

**文件**: `local/shell_operation.go` — `LocalShellOperation` 结构体和工厂函数

**Python 参考**: `shell_operation.py` 第 842-862 行, `base.py`, `sys_operation.py` 第 174 行

**现状**:
- `NewLocalShellOperation(runConfig any)` 完全忽略 runConfig
- 没有 `checkAllowlist` 方法
- `checkCommandSafety` 不支持 custom `dangerous_patterns`

**修复方案**:

1. `LocalShellOperation` 增加 `runConfig *sysop.LocalWorkConfig` 字段
2. `NewLocalShellOperation` 解析 runConfig：
   ```go
   func NewLocalShellOperation(runConfig any) sysop.SysSubOperation {
       op := &LocalShellOperation{}
       op.initPatterns()
       if rc, ok := runConfig.(*sysop.LocalWorkConfig); ok && rc != nil {
           op.runConfig = rc
       } else {
           op.runConfig = sysop.NewLocalWorkConfig()
       }
       // 如果有 custom dangerous_patterns，覆盖内置模式
       if len(op.runConfig.DangerousPatterns) > 0 {
           op.initCustomDangerousPatterns()
       }
       return op
   }
   ```
3. 实现 `checkAllowlist`:
   ```go
   func (s *LocalShellOperation) checkAllowlist(command string) bool {
       if len(s.runConfig.ShellAllowlist) == 0 {
           return true  // 空白名单 = 放行所有
       }
       cmdPrefix := ""
       if parts := strings.Fields(command); len(parts) > 0 {
           cmdPrefix = parts[0]
       }
       for _, allowed := range s.runConfig.ShellAllowlist {
           if cmdPrefix == allowed || strings.HasSuffix(cmdPrefix, string(os.PathSeparator)+allowed) {
               return true
           }
       }
       return false
   }
   ```
4. `checkCommandSafety` 支持 custom patterns：如果 `runConfig.DangerousPatterns` 有值则用自定义，否则用内置 `_DANGEROUS_PATTERNS`

---

### 问题 7：`resolveExecutionPlan` AUTO 与 Python 不一致 [🟡 中]

**文件**: `local/shell_operation.go` — `resolveExecutionPlan()`

**Python 参考**: `shell_operation.py` 第 290-353 行

**差异**:
1. Windows AUTO 缺少 `_looks_like_posix` 检测
2. 非 Windows AUTO 应该用 `sh` 而非优先 `bash`
3. `unwrap_powershell_command` 应仅在 AUTO/PowerShell 时执行，当前无条件执行

**修复方案**: 严格按 Python 逻辑重写 AUTO 分支：

```go
func (s *LocalShellOperation) resolveExecutionPlan(command string, shellType sysop.ShellType) ([]string, bool, string) {
    isWindows := runtime.GOOS == "windows"

    switch shellType {
    case sysop.ShellTypeAuto:
        if isWindows {
            // 1. 尝试 unwrap
            if unwrapped := UnwrapPowerShellCommand(command); unwrapped != "" {
                exe := AvailablePowerShell()
                return []string{exe, "-NoProfile", "-NonInteractive", "-Command", unwrapped}, false, "powershell"
            }
            // 2. 检测 PowerShell
            if LooksLikePowerShell(command) {
                exe := AvailablePowerShell()
                return []string{exe, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"
            }
            // 3. 检测 POSIX
            if LooksLikePosix(command) {
                exe := AvailableBash(false) // allow_wsl=False
                if exe != "" {
                    return []string{exe, "-lc", NormalizeWindowsPathsForBash(command)}, false, "bash"
                }
            }
            // 4. fallback cmd
            return []string{"cmd", "/c", command}, true, "cmd"
        }
        // 非 Windows：AUTO 用 sh（create_subprocess_shell）
        return []string{"sh", "-c", command}, true, "sh"

    case sysop.ShellTypePowerShell:
        unwrapped := UnwrapPowerShellCommand(command)
        if unwrapped != "" { command = unwrapped }
        exe := AvailablePowerShell()
        return []string{exe, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"

    // Bash/Sh/Cmd 保持不变...
    }
}
```

需要新增的辅助函数（对齐 Python）:
- `LooksLikePosix(command)` — 对齐 `_looks_like_posix`，检测命令是否包含 POSIX 命令（ls, grep, cat 等）

---

### 问题 8：`fs_operation` 缺失 sandbox 限制校验 [🟢 低]

**文件**: `local/fs_operation.go` — `LocalFsOperation.resolvePath()`

**Python 参考**: `fs_operation.py` 第 1126-1131 行

**现状**: resolvePath 完全没有 `restrict_to_sandbox` 校验。

**修复方案**:

1. `LocalFsOperation` 增加 `runConfig *sysop.LocalWorkConfig` 字段
2. `NewLocalFsOperation` 解析 runConfig（同 shell 模式）
3. `resolvePath` 加 sandbox 校验：
   ```go
   if f.runConfig != nil && f.runConfig.RestrictToSandbox {
       sandboxRoots := f.runConfig.SandboxRoot
       if len(sandboxRoots) == 0 {
           // fallback 到 CWD 的 workspace/project_root
           sandboxRoots = []string{ResolveCwd("")}
       }
       allowed := false
       for _, root := range sandboxRoots {
           if strings.HasPrefix(resolved, root) {
               allowed = true
               break
           }
       }
       if !allowed {
           return "", fmt.Errorf("path %s is outside sandbox roots", resolved)
       }
   }
   ```

---

### 额外问题 A：pkill/killall 缺少 `(?!-tui)` 负向前瞻 [🟢 低]

**文件**: `local/shell_operation.go` — `initPatterns()`

**现状**: `pkill\b[^\n\r;|&]*jiuwenswarm` 会拦截 `pkill jiuwenswarm-tui`

**修复方案**: Go 的 `regexp` 不支持负向前瞻 `(?!...)`，需要用替代方案：

```go
// 方案：匹配 jiuwenswarm 后面不跟 -tui
// 用两个正则：先匹配 jiuwenswarm，再排除 -tui 后缀
{regexp.MustCompile(`\bpkill\b[^\n\r;|&]*jiuwenswarm(?!-tui)`), ...}
```

由于 Go 的 `regexp` 不支持 `(?!...)`，需要改为两步检查：
1. 用 `\bpkill\b[^\n\r;|&]*jiuwenswarm` 匹配
2. 匹配后检查后续是否紧跟 `-tui`，如果是则放行

或者使用 `regexp2` 库（支持前瞻），或手动实现排除逻辑。

**推荐方案**: 在 `checkCommandSafety` 中对 pkill/killall 模式做特殊处理：
```go
// 匹配后检查是否是 -tui 变体
if dp.Label == "pkill targeting jiuwenswarm backend" {
    // 检查匹配位置后是否紧跟 -tui
    loc := dp.Pattern.FindStringIndex(command)
    if loc != nil && strings.HasPrefix(command[loc[1]:], "-tui") {
        continue  // 放行 jiuwenswarm-tui
    }
}
```

对 jiuwenclaw 的 pkill/killall 不需要排除（Python 也没有 `(?!-tui)`）。

---

### 额外问题 B：实现 `_BUFFERING_WRAPPERS` [🟢 低]

**文件**: `local/shell_operation.go` 或新建 `local/buffering.go`

**Python 参考**: `shell_operation.py` 第 284-288 行

**现状**: 完全没有实现，stream 模式下命令以块缓冲输出。

**修复方案**:

```go
// wrapCommandWithBuffering 用 OS 特定的缓冲包装器包装命令（仅在 stream 模式下使用）。
// 对齐 Python _BUFFERING_WRAPPERS：
// - Linux: stdbuf -oL -eL /bin/sh -c <quoted_cmd>
// - macOS: script -q /dev/null /bin/sh -c <quoted_cmd>
// - Windows: 不包装
func wrapCommandWithBuffering(command string) string {
    switch runtime.GOOS {
    case "linux":
        quoted := shlex.Quote(command)  // 需要 shell 引号转义
        return fmt.Sprintf("stdbuf -oL -eL /bin/sh -c %s", quoted)
    case "darwin":
        quoted := shlex.Quote(command)
        return fmt.Sprintf("script -q /dev/null /bin/sh -c %s", quoted)
    default:
        return command
    }
}
```

在 `ExecuteCmdStream` 创建子进程时，如果 `useShell=true`，应用 buffering wrapper：
```go
if useShell {
    cmd = wrapCommandWithBuffering(args)
}
```

注意：需要在 Go 中实现 `shlex.Quote` 或等效的 shell 引号转义函数。

---

## 实施顺序

建议按依赖关系和严重程度排序：

1. **问题 6** — runConfig 传递（其他问题依赖此基础）
2. **问题 7** — resolveExecutionPlan AUTO 对齐
3. **额外问题 A** — pkill/killall 负向前瞻
4. **额外问题 B** — buffering wrapper 实现
5. **问题 1** — Stream() 超时控制
6. **问题 2** — Invoke() 并行收集 + grace period
7. **问题 3** — Background() grace 检测
8. **问题 4** — ExecuteCmdStream 补齐缺失步骤
9. **问题 5** — ExecuteCmd/Background 补齐缺失步骤
10. **问题 8** — fs_operation sandbox 限制

## 涉及文件

| 文件 | 修改类型 |
|------|---------|
| `local/utils.go` | 重写 Stream()、Invoke()、Background() |
| `local/shell_operation.go` | 重写 resolveExecutionPlan()、补齐 ExecuteCmd/Stream/Background 缺失步骤、实现 checkAllowlist、runConfig 传递 |
| `local/shell_helpers.go` | 新增 LooksLikePosix()、shlex.Quote()、wrapCommandWithBuffering() |
| `local/fs_operation.go` | runConfig 传递、resolvePath 加 sandbox 校验 |
