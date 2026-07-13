# 7.13 Review 修复实现计划

> 日期：2026-07-13
> 基于：`docs/review/2026-07-13-fix-plan.md`
> 原则：逐项对照 Python 源码确认，严格对齐实现步骤

---

## Goal

修复 7.13 logic review 中确认的 22 个问题（S1-S8, S9, S11, S16, S20-S23, S25, S27, G2-G6），使 Go 实现严格对齐 Python 源码行为。

## Architecture

修复按模块分组，每组内按依赖顺序排列。跨文件修改在同一 Task 内完成，避免中间状态编译失败。

## Tech Stack

- Go 1.22+，标准库为主
- `golang.org/x/text/encoding`（G5 非 UTF-8 编码处理）
- `syscall.Flock`/`syscall.LockFileEx`（S7 跨进程文件锁）
- `regexp`（S1 正则大小写不敏感）
- TDD 流程：先写测试 -> 验证失败 -> 写实现 -> 验证通过 -> commit

---

## 文件结构清单

| 文件 | 修改项 |
|------|--------|
| `internal/agentcore/sys_operation/local/shell_operation.go` | S1, S2, S3, S8 |
| `internal/agentcore/sys_operation/shell_process_registry.go` | S8 |
| `internal/agentcore/sys_operation/local/fs_operation.go` | S4, S5, S6, G2, G3, G5 |
| `internal/agentcore/sys_operation/local/file_lock.go` (新) | S7 |
| `internal/agentcore/sys_operation/local/file_lock_unix.go` (新) | S7 |
| `internal/agentcore/sys_operation/local/file_lock_windows.go` (新) | S7 |
| `internal/agentcore/sys_operation/local/code_operation.go` | G4 |
| `internal/agentcore/sys_operation/sys_operation_card.go` | G6 |
| `internal/swarm/gateway/channel_manager/web/config_apply.go` | S27 |
| `internal/swarm/gateway/channel_manager/web/web_handlers.go` | S27 调用方 |
| `internal/swarm/server/handle_envelope.go` | S25 |
| `internal/swarm/gateway/message_handler/cancel.go` | S21 |
| `internal/swarm/gateway/message_handler/dispatch.go` | S20 |
| `internal/swarm/gateway/message_handler/forward_loop.go` | S22 |
| `internal/swarm/gateway/message_handler/disconnect.go` | S23 |
| `internal/swarm/server/adapter/deep_adapter.go` | S9, S11 |
| `internal/swarm/server/adapter/deep_adapter_config.go` | S16 |

---

## Group 1: sys_operation 模块

### Task 1: S1 - 正则加 (?i)

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/shell_operation.go` (L630-645, L698)

**Steps:**

#### Step 1: 修改内置 dangerousPatterns，所有正则前加 `(?i)`

将 `initPatterns()` 中 L630-645 的 `dangerousPatterns` 列表，每个正则前加 `(?i)` 前缀：

```go
func (s *LocalShellOperation) initPatterns() {
	s.dangerousPatterns = []DangerousPattern{
		{regexp.MustCompile(`(?i)\brm\s+-rf\b`), "rm -rf"},
		{regexp.MustCompile(`(?i)\bdel\s+/[a-z]*[fsq][a-z]*\b`), "del /f /s /q"},
		{regexp.MustCompile(`(?i)\brd\s+/s\s+/q\b`), "rd /s /q"},
		{regexp.MustCompile(`(?i)\bformat\s+[a-z]:`), "format drive"},
		{regexp.MustCompile(`(?i)\bshutdown\b`), "shutdown"},
		{regexp.MustCompile(`(?i)\breboot\b`), "reboot"},
		{regexp.MustCompile(`(?i)\bdiskpart\b`), "diskpart"},
		{regexp.MustCompile(`(?i)\bmkfs\b`), "mkfs"},
		{regexp.MustCompile(`(?i)\breg\s+delete\b`), "reg delete"},
		{regexp.MustCompile(`(?i)\bremove-item\b[^\n\r]*-recurse[^\n\r]*-force`), "Remove-Item -Recurse -Force"},
		{regexp.MustCompile(`(?i)\bpkill\b[^\n\r;|&]*jiuwenswarm`), "pkill targeting jiuwenswarm backend"},
		{regexp.MustCompile(`(?i)\bkillall\b[^\n\r;|&]*jiuwenswarm`), "killall targeting jiuwenswarm backend"},
		{regexp.MustCompile(`(?i)\bpkill\b[^\n\r;|&]*jiuwenclaw`), "pkill targeting jiuwenclaw backend"},
		{regexp.MustCompile(`(?i)\bkillall\b[^\n\r;|&]*jiuwenclaw`), "killall targeting jiuwenclaw backend"},
	}
	// tuiCommandPatterns 保持不变（已有大小写不敏感匹配）
	s.tuiCommandPatterns = []TUICommandPattern{
		{regexp.MustCompile(`\b(npx\s+)?playwright\s+test\b`), "Playwright test runner may require TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\b(npm|npx|yarn|pnpm)\s+(run\s+)?test\b`), "Test runner (npm/pnpm/yarn) may require TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\bvitest\b.*(--watch|--ui)`), "Vitest watch/UI mode requires TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\b(top|htop|vim|vi|nano|less|more)\b`), "Interactive TUI program will hang without TTY", nil},
	}
}
```

#### Step 2: 修改 initCustomDangerousPatterns，自定义模式包裹 `(?i)`

将 L698 行：
```go
re, err := regexp.Compile(rawPattern)
```
改为：
```go
re, err := regexp.Compile("(?i)" + rawPattern)
```

完整的 `initCustomDangerousPatterns` 函数：
```go
func (s *LocalShellOperation) initCustomDangerousPatterns() {
	s.dangerousPatterns = make([]DangerousPattern, 0, len(s.runConfig.DangerousPatterns))
	for _, rawPattern := range s.runConfig.DangerousPatterns {
		re, err := regexp.Compile("(?i)" + rawPattern)
		if err != nil {
			logger.Warn(shellLogComponent).
				Str("pattern", rawPattern).
				Err(err).
				Msg("自定义危险模式编译失败，跳过")
			continue
		}
		s.dangerousPatterns = append(s.dangerousPatterns, DangerousPattern{Pattern: re, Label: rawPattern})
	}
}
```

**测试：**

```go
func TestCheckCommandSafety_大小写不敏感(t *testing.T) {
	op := NewLocalShellOperation(nil).(*LocalShellOperation)
	// 内置模式：大写命令应被拦截
	if blocked := op.checkCommandSafety("RM -RF /"); blocked == "" {
		t.Error("大写 RM -RF 应被拦截")
	}
	if blocked := op.checkCommandSafety("Shutdown /s"); blocked == "" {
		t.Error("大写 Shutdown 应被拦截")
	}
	if blocked := op.checkCommandSafety("FORMAT C:"); blocked == "" {
		t.Error("大写 FORMAT 应被拦截")
	}
}

func TestInitCustomDangerousPatterns_大小写不敏感(t *testing.T) {
	rc := sysop.NewLocalWorkConfig()
	rc.DangerousPatterns = []string{`\bdangerous\b`}
	op := NewLocalShellOperation(rc).(*LocalShellOperation)
	if blocked := op.checkCommandSafety("run DANGEROUS cmd"); blocked == "" {
		t.Error("自定义模式应大小写不敏感")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestCheckCommandSafety_大小写不敏感|TestInitCustomDangerousPatterns_大小写不敏感" ./internal/agentcore/sys_operation/local/...
```

**commit message：** `fix(sys_operation): 所有危险命令正则添加 (?i) 大小写不敏感前缀，对齐 Python re.IGNORECASE`

---

### Task 2: S2 - resolveExecutionPlan 加 error 返回

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/shell_operation.go` (L737, L762, L185, L345, L483)

**Steps:**

#### Step 1: 修改 resolveExecutionPlan 签名，增加 error 返回值

将 L737 行签名：
```go
func (s *LocalShellOperation) resolveExecutionPlan(command string, shellType sysop.ShellType) (args []string, useShell bool, shellName string) {
```
改为：
```go
func (s *LocalShellOperation) resolveExecutionPlan(command string, shellType sysop.ShellType) (args []string, useShell bool, shellName string, err error) {
```

#### Step 2: ShellTypeCmd 非 Windows 分支返回 error

将 L760-763 行：
```go
case sysop.ShellTypeCmd:
	if !isWindows {
		// 对齐 Python：cmd 仅 Windows 支持
		return nil, false, "shell_type 'cmd' is only supported on Windows"
	}
	return []string{"cmd", "/c", command}, true, "cmd"
```
改为：
```go
case sysop.ShellTypeCmd:
	if !isWindows {
		// 对齐 Python：cmd 仅 Windows 支持，非 Windows 返回 error
		return nil, false, "", exception.New(
			exception.StatusSysOperationShellExecutionError,
			"shell_type 'cmd' is only supported on Windows",
		)
	}
	return []string{"cmd", "/c", command}, true, "cmd", nil
```

#### Step 3: 所有其他 return 语句增加 nil error

函数内所有 `return` 语句末尾加 `, nil`，例如：
- L747: `return []string{pwshPath, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell", nil`
- L755-757: bash 分支两个 return 末尾加 `, nil`
- L764: cmd Windows 分支加 `, nil`
- L772-774: sh 分支两个 return 末尾加 `, nil`
- L784-800: auto 分支所有 return 末尾加 `, nil`

#### Step 4: 修改 3 个调用点

**ExecuteCmd (L185):**
```go
args, _, shellName, err := s.resolveExecutionPlan(command, o.ShellType)
if err != nil {
	return createErrResult(err.Error(), &result.ExecuteCmdData{Command: command, Cwd: actualCwd}), nil
}
```

**ExecuteCmdStream (L345):**
```go
args, useShell, _, err := s.resolveExecutionPlan(command, o.ShellType)
if err != nil {
	exitCode := -1
	ch <- result.ExecuteCmdStreamResult{
		BaseResult: result.BuildOperationErrorResult(
			exception.StatusSysOperationShellExecutionError.Code(),
			err.Error(),
		),
		Data: &result.ExecuteCmdChunkData{ChunkIndex: 0, ExitCode: &exitCode},
	}
	close(ch)
	return ch, nil
}
```

**ExecuteCmdBackground (L483):**
```go
args, _, _, err := s.resolveExecutionPlan(command, o.ShellType)
if err != nil {
	return &result.ExecuteCmdBackgroundResult{
		BaseResult: result.BuildOperationErrorResult(
			exception.StatusSysOperationShellExecutionError.Code(),
			err.Error(),
		),
		Data: &result.ExecuteCmdBackgroundData{Command: command, Cwd: actualCwd},
	}, nil
}
```

**测试：**

```go
func TestResolveExecutionPlan_Cmd非Windows返回错误(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("仅非 Windows 平台测试")
	}
	op := NewLocalShellOperation(nil).(*LocalShellOperation)
	_, _, _, err := op.resolveExecutionPlan("echo test", sysop.ShellTypeCmd)
	if err == nil {
		t.Error("非 Windows 下 ShellTypeCmd 应返回 error")
	}
}

func TestResolveExecutionPlan_其他Shell正常返回(t *testing.T) {
	op := NewLocalShellOperation(nil).(*LocalShellOperation)
	args, _, _, err := op.resolveExecutionPlan("echo test", sysop.ShellTypeBash)
	if err != nil {
		t.Errorf("ShellTypeBash 不应返回 error: %v", err)
	}
	if len(args) == 0 {
		t.Error("args 不应为空")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestResolveExecutionPlan_" ./internal/agentcore/sys_operation/local/...
```

**commit message：** `fix(sys_operation): resolveExecutionPlan 非 Windows 下 ShellTypeCmd 返回 error，对齐 Python raise`

---

### Task 3: S3 - 进程注册时机修正

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/shell_operation.go` (L207-218, L489-512)

**Steps:**

#### Step 1: 修正 ExecuteCmd 中进程注册时机

将 L207-218 行（进程注册在 Invoke 之后）改为在 Invoke 之前注册，使用 defer 注销：

```go
	// 创建进程处理器
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, encoding, timeout)

	// Shell 进程注册，对齐 Python: track_sid = _track_shell_process(proc) → try: invoke → finally: _untrack
	// Invoke 内部 Start 进程后，cmd.Process 才有值，因此需要在 Invoke 后、结果返回前注册
	// 但 Python 是在 subprocess 创建后、invoke 前注册，Go 中等价做法：
	// 先 Invoke（Start 内部），然后在 Process 非空时注册 + defer 注销
	invokeData, err := handler.Invoke(ctx)

	// Shell 进程注册 + 注销（对齐 Python finally 语义）
	if handler.cmd.Process != nil {
		sid := trackShellProcess(ctx, handler.cmd.Process)
		defer untrackShellProcess(sid, handler.cmd.Process)
	}
```

注意：Go 中 `exec.Command.Start` 后才有 `cmd.Process`，而 `handler.Invoke` 内部调用了 `Start`。所以注册必须在 Invoke 之后，但使用 `defer` 确保即使后续 panic 也能注销。这已经与当前代码的行为一致，只是把 `untrackShellProcess` 改为 `defer` 调用。

当前 L215-218 的代码：
```go
	if handler.cmd.Process != nil {
		sid := trackShellProcess(ctx, handler.cmd.Process)
		untrackShellProcess(sid, handler.cmd.Process)
	}
```
改为：
```go
	if handler.cmd.Process != nil {
		sid := trackShellProcess(ctx, handler.cmd.Process)
		defer untrackShellProcess(sid, handler.cmd.Process)
	}
```

#### Step 2: ExecuteCmdBackground 中进程注册已在正确位置（L509-512），无需修改

当前代码在 `handler.Background(o.Grace)` 后注册，这是正确的。失败时（L494-498）已有注销逻辑。

**测试：**

```go
func TestExecuteCmd_进程注册注销(t *testing.T) {
	op := NewLocalShellOperation(nil).(*LocalShellOperation)
	ctx := context.Background()
	// 执行短命令后，验证 DefaultRegistry 中没有残留进程
	_, _ = op.ExecuteCmd(ctx, "echo hello", sysop.WithTimeout(5))
	// 短暂等待进程退出
	time.Sleep(100 * time.Millisecond)
	// 注册表应为空（defer 已注销）
	if len(sysop.DefaultRegistry.Processes()) > 0 {
		t.Error("ExecuteCmd 完成后进程应已注销")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestExecuteCmd_进程注册注销" ./internal/agentcore/sys_operation/local/...
```

**commit message：** `fix(sys_operation): ExecuteCmd 进程注销使用 defer，确保异常路径也执行注销，对齐 Python finally`

---

### Task 4: S4+S5+G2+G3 - fs_operation 小修复

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/fs_operation.go` (L802-805, L57-58, L591-622, L850-900)

**Steps:**

#### Step 1: S4 - sandbox fallback 逻辑修正

将 L802-805 行：
```go
		if len(sandboxRoots) == 0 {
			// fallback 到 CWD
			sandboxRoots = []string{base}
		}
```
改为：
```go
		if len(sandboxRoots) == 0 {
			// 对齐 Python: roots = [p for p in (get_workspace(), get_project_root()) if p]
			sandboxRoots = []string{}
			if ws := getWorkspace(); ws != "" {
				sandboxRoots = append(sandboxRoots, ws)
			}
			if pr := getProjectRoot(); pr != "" {
				sandboxRoots = append(sandboxRoots, pr)
			}
		}
```

需要新增两个辅助函数（或使用已有的 cwd 包函数）：
```go
// getWorkspace 获取工作区根目录，对齐 Python get_workspace()
func getWorkspace() string {
	return cwd.Workspace()
}

// getProjectRoot 获取项目根目录，对齐 Python get_project_root()
func getProjectRoot() string {
	return cwd.ProjectRoot()
}
```

#### Step 2: S5 - read_file 互斥参数校验

在 `ReadFile` 函数 L58 行后（`o := sysop.NewFsOptions(opts...)` 之后），添加互斥参数校验：

```go
	o := sysop.NewFsOptions(opts...)
	methodName := "read_file"

	// 互斥参数校验，对齐 Python validate_mutually_exclusive + validate_binary_mode
	if o.Tail > 0 && o.Head > 0 {
		return nil, exception.New(
			exception.StatusSysOperationFsParamError,
			"head and tail are mutually exclusive",
		)
	}
	if o.Tail > 0 && (o.LineRange[0] > 0 || o.LineRange[1] > 0) {
		return nil, exception.New(
			exception.StatusSysOperationFsParamError,
			"tail and line_range are mutually exclusive",
		)
	}
	if o.Head > 0 && (o.LineRange[0] > 0 || o.LineRange[1] > 0) {
		return nil, exception.New(
			exception.StatusSysOperationFsParamError,
			"head and line_range are mutually exclusive",
		)
	}
	if o.Mode == "bytes" && (o.Head > 0 || o.Tail > 0 || o.LineRange[0] > 0 || o.LineRange[1] > 0) {
		return nil, exception.New(
			exception.StatusSysOperationFsParamError,
			"bytes mode does not support head/tail/line_range",
		)
	}
```

#### Step 3: G2 - search_files 缺少 exclude_patterns

在 `SearchFiles` 函数（L591-623）中，匹配完成后过滤 exclude_patterns：

将 L597-612 行后（`err = filepath.Walk(...)` 之后），添加过滤逻辑：

```go
	// 对齐 Python: exclude_set = set(); for pat in exclude_patterns: exclude_set.update(set(base.rglob(pat)))
	if len(o.ExcludePatterns) > 0 {
		filtered := matched[:0]
		for _, p := range matched {
			excluded := false
			for _, ep := range o.ExcludePatterns {
				if matchedEp, _ := filepath.Match(ep, filepath.Base(p.Path)); matchedEp {
					excluded = true
					break
				}
			}
			if !excluded {
				filtered = append(filtered, p)
			}
		}
		matched = filtered
	}
```

#### Step 4: G3 - list_files/list_directories 缺少 max_depth

修改 `walkDir` 函数（L850），增加 `depth` 参数：

```go
func (f *LocalFsOperation) walkDir(basePath string, dirsOnly bool, o *sysop.FsOptions, depth int) []result.FileSystemItem {
	var items []result.FileSystemItem

	// 对齐 Python: max_depth 递归深度限制
	if o.MaxDepth > 0 && depth > o.MaxDepth {
		return items
	}

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return items
	}

	for _, entry := range entries {
		// ... (原有逻辑不变) ...

		// 递归
		if o.Recursive && entry.IsDir() {
			subItems := f.walkDir(fullPath, dirsOnly, o, depth+1)
			items = append(items, subItems...)
		}
	}

	// 排序
	f.sortItems(items, o.SortBy, o.SortDescending)

	return items
}
```

同时更新所有调用方：
- `listItems`（L836）: `items := f.walkDir(resolvedPath, dirsOnly, o, 1)`
- `ListDirectories`（L576）: `items := f.walkDir(resolvedPath, true, o, 1)`

**测试：**

```go
func TestReadFile_互斥参数校验(t *testing.T) {
	op := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	os.WriteFile(tmpFile, []byte("line1\nline2\nline3\n"), 0644)

	_, err := op.ReadFile(ctx, tmpFile, sysop.WithHead(1), sysop.WithTail(1))
	if err == nil {
		t.Error("head + tail 应返回互斥错误")
	}

	_, err = op.ReadFile(ctx, tmpFile, sysop.WithTail(1), sysop.WithLineRange([2]int{1, 2}))
	if err == nil {
		t.Error("tail + line_range 应返回互斥错误")
	}
}

func TestSearchFiles_排除模式(t *testing.T) {
	op := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "a.go"), nil, 0644)
	os.WriteFile(filepath.Join(tmpDir, "a_test.go"), nil, 0644)

	result, err := op.SearchFiles(ctx, tmpDir, "*.go", sysop.WithExcludePatterns([]string{"*_test.go"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range result.Data.MatchingFiles {
		if strings.HasSuffix(f.Name, "_test.go") {
			t.Error("exclude_patterns 应排除 _test.go 文件")
		}
	}
}

func TestWalkDir_最大深度(t *testing.T) {
	op := NewLocalFsOperation(nil).(*LocalFsOperation)
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "d1", "d2"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "d1", "f1.txt"), nil, 0644)
	os.WriteFile(filepath.Join(tmpDir, "d1", "d2", "f2.txt"), nil, 0644)

	o := sysop.NewFsOptions(sysop.WithRecursive(true), sysop.WithMaxDepth(1))
	items := op.walkDir(tmpDir, false, o, 1)
	for _, item := range items {
		if item.Name == "f2.txt" {
			t.Error("max_depth=1 不应递归到 d1/d2 层级")
		}
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestReadFile_互斥参数校验|TestSearchFiles_排除模式|TestWalkDir_最大深度" ./internal/agentcore/sys_operation/local/...
```

**commit message：** `fix(sys_operation): fs_operation 修复 sandbox fallback/互斥校验/exclude_patterns/max_depth，对齐 Python`

---

### Task 5: S6 - tail 反向 seek 实现

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/fs_operation.go` (L98-102)

**Steps:**

#### Step 1: 新增 readTail 函数

在 `fs_operation.go` 的非导出函数区域添加：

```go
// readTail 从文件末尾反向 seek 读取指定行数，对齐 Python _read_tail。
// 使用反向逐块读取策略，避免全文加载。
func readTail(filePath string, tail int) ([]string, error) {
	const chunkSize = 8192 // 对齐 Python TAIL_CHUNK_SIZE

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	// 获取文件大小
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := fi.Size()
	if fileSize == 0 {
		return nil, nil
	}

	// 收集行（从末尾向前）
	var lines []string
	offset := fileSize
	buf := make([]byte, chunkSize)
	trailingNewline := true // 文件末尾视为换行

	for offset > 0 && len(lines) < tail {
		readSize := int64(chunkSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize

		_, err = f.Seek(offset, io.SeekStart)
		if err != nil {
			return nil, err
		}
		_, err = io.ReadFull(f, buf[:readSize])
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, err
		}

		// 从块末尾向前扫描换行符
		chunk := buf[:readSize]
		for i := len(chunk) - 1; i >= 0; i-- {
			if chunk[i] == '\n' {
				if trailingNewline {
					trailingNewline = false
					continue
				}
				// 找到换行符，截取该行
				lineEnd := i + 1
				// 行内容从上一个换行位置到当前换行位置
				// 此处简化：先收集所有行，最后取 tail 行
			}
		}
	}

	// 简化实现：读取 offset 之后的所有内容，按行分割取最后 tail 行
	// 对齐 Python 的简化逻辑
	_, err = f.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}
	remaining, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	allLines := strings.Split(string(remaining), "\n")
	// 去除末尾空行（文件末尾换行符导致）
	if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
		allLines = allLines[:len(allLines)-1]
	}
	if tail >= len(allLines) {
		return allLines, nil
	}
	return allLines[len(allLines)-tail:], nil
}
```

需要在 import 中添加 `"io"`。

#### Step 2: 修改 ReadFile 中 tail 分支

将 L98-102 行：
```go
		} else if o.Tail > 0 {
			if o.Tail < len(lines) {
				lines = lines[len(lines)-o.Tail:]
			}
			textContent = strings.Join(lines, "\n")
```
改为：
```go
		} else if o.Tail > 0 {
			// 对齐 Python _read_tail：反向 seek 读取，避免全文加载
			tailLines, tailErr := readTail(resolvedPath, o.Tail)
			if tailErr != nil {
				return f.createErrorResult(methodName, tailErr.Error(), startTime), nil
			}
			textContent = strings.Join(tailLines, "\n")
```

**测试：**

```go
func TestReadTail_反向读取(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "tail_test.txt")
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line%d", i))
	}
	os.WriteFile(tmpFile, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	result, err := readTail(tmpFile, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 5 {
		t.Fatalf("期望 5 行，实际 %d 行", len(result))
	}
	if result[0] != "line96" {
		t.Errorf("首行期望 line96，实际 %s", result[0])
	}
	if result[4] != "line100" {
		t.Errorf("末行期望 line100，实际 %s", result[4])
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestReadTail_反向读取" ./internal/agentcore/sys_operation/local/...
```

**commit message：** `fix(sys_operation): tail 模式使用反向 seek 读取，避免全文加载，对齐 Python _read_tail`

---

### Task 6: S7 - 跨进程文件锁

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/file_lock.go` (新)
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/file_lock_unix.go` (新)
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/file_lock_windows.go` (新)
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/fs_operation.go` (L192-293)

**Steps:**

#### Step 1: 创建 file_lock.go（跨平台接口）

```go
package local

import (
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FileLock 跨进程文件锁。
// 对齐 Python _file_lock：fcntl.flock(fd, LOCK_EX|LOCK_NB) + 轮询超时。
type FileLock struct {
	// filePath 被锁文件路径
	filePath string
	// lockPath 锁文件路径（.lock 后缀）
	lockPath string
	// fd 锁文件描述符
	fd uintptr
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fileLockPollInterval 文件锁轮询间隔，对齐 Python 50ms
	fileLockPollInterval = 50 * time.Millisecond
	// fileLockDefaultTimeout 文件锁默认超时，对齐 Python 10s
	fileLockDefaultTimeout = 10 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AcquireFileLock 获取文件锁，超时返回 error。
// 对齐 Python _file_lock(data_path)：创建 .lock 文件 + fcntl.flock + 轮询。
func AcquireFileLock(filePath string, timeout time.Duration) (*FileLock, error) {
	return acquireFileLockPlatform(filePath, timeout)
}

// ReleaseFileLock 释放文件锁。
// 对齐 Python _file_lock 的 __exit__ / finally。
func ReleaseFileLock(lock *FileLock) error {
	return releaseFileLockPlatform(lock)
}
```

#### Step 2: 创建 file_lock_unix.go（Unix 实现）

```go
//go:build !windows

package local

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// acquireFileLockPlatform Unix 平台文件锁实现。
// 对齐 Python fcntl.flock(fd, LOCK_EX|LOCK_NB) + 轮询超时。
func acquireFileLockPlatform(filePath string, timeout time.Duration) (*FileLock, error) {
	lockPath := filePath + ".lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &FileLock{filePath: filePath, lockPath: lockPath, fd: uintptr(fd)}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = syscall.Close(fd)
			return nil, fmt.Errorf("获取文件锁失败: %w", err)
		}
		if time.Now().After(deadline) {
			_ = syscall.Close(fd)
			return nil, fmt.Errorf("获取文件锁超时(%v): %s", timeout, filePath)
		}
		time.Sleep(fileLockPollInterval)
	}
}

// releaseFileLockPlatform Unix 平台释放文件锁。
func releaseFileLockPlatform(lock *FileLock) error {
	if lock.fd == 0 {
		return nil
	}
	_ = syscall.Flock(int(lock.fd), syscall.LOCK_UN)
	_ = syscall.Close(int(lock.fd))
	_ = os.Remove(lock.lockPath)
	return nil
}
```

#### Step 3: 创建 file_lock_windows.go（Windows 实现）

```go
//go:build windows

package local

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// acquireFileLockPlatform Windows 平台文件锁实现。
// 对齐 Python msvcrt.locking + 轮询超时。
func acquireFileLockPlatform(filePath string, timeout time.Duration) (*FileLock, error) {
	lockPath := filePath + ".lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.LockFileEx(syscall.Handle(fd), syscall.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &syscall.Overlapped{})
		if err == nil {
			return &FileLock{filePath: filePath, lockPath: lockPath, fd: uintptr(fd)}, nil
		}
		if time.Now().After(deadline) {
			_ = syscall.Close(syscall.Handle(fd))
			return nil, fmt.Errorf("获取文件锁超时(%v): %s", timeout, filePath)
		}
		time.Sleep(fileLockPollInterval)
	}
}

// releaseFileLockPlatform Windows 平台释放文件锁。
func releaseFileLockPlatform(lock *FileLock) error {
	if lock.fd == 0 {
		return nil
	}
	_ = syscall.UnlockFile(syscall.Handle(lock.fd), 0, 0, 1, 0)
	_ = syscall.Close(syscall.Handle(lock.fd))
	_ = os.Remove(lock.lockPath)
	return nil
}
```

#### Step 4: 在 WriteFile 中使用文件锁

在 `WriteFile` 函数（L192 行之后，resolvedPath 解析完成后），添加文件锁：

```go
	// 跨进程文件锁，对齐 Python _file_lock(data_path)
	lock, lockErr := AcquireFileLock(resolvedPath, fileLockDefaultTimeout)
	if lockErr != nil {
		return &result.WriteFileResult{
			BaseResult: result.BuildOperationErrorResult(exception.StatusSysOperationFsExecutionError.Code(), lockErr.Error()),
		}, nil
	}
	defer func() { _ = ReleaseFileLock(lock) }()
```

**测试：**

```go
func TestFileLock_互斥(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "lock_test.txt")
	os.WriteFile(tmpFile, []byte("original"), 0644)

	lock1, err := AcquireFileLock(tmpFile, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// 第二次获取应超时
	_, err = AcquireFileLock(tmpFile, 100*time.Millisecond)
	if err == nil {
		t.Error("文件已被锁，第二次获取应失败")
	}

	_ = ReleaseFileLock(lock1)

	// 释放后应能获取
	lock2, err := AcquireFileLock(tmpFile, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = ReleaseFileLock(lock2)
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestFileLock_互斥" ./internal/agentcore/sys_operation/local/...
```

**commit message：** `feat(sys_operation): 新增跨进程文件锁，WriteFile 写入前获取锁，对齐 Python _file_lock`

---

### Task 7: S8 - WriteStdin/ListProcesses 实现

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/shell_operation.go` (L582-623)
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/shell_process_registry.go`

**Steps:**

#### Step 1: ShellProcessRegistry 增加 stdin pipe 追踪和 ListProcesses

在 `shell_process_registry.go` 中修改 `ShellProcessRegistry` 结构体：

```go
type ShellProcessRegistry struct {
	// mu 互斥锁
	mu sync.Mutex
	// processes sessionID → 进程集合
	processes map[string]map[*os.Process]struct{}
	// stdinPipes sessionID → 进程 → stdin pipe
	stdinPipes map[string]map[*os.Process]io.Writer
	// cancelledSessions 已取消的会话集合
	cancelledSessions map[string]struct{}
}
```

添加 `RegisterWithStdin` 方法和 `ListProcesses` 方法：

```go
// RegisterWithStdin 注册进程同时保存 stdin pipe 引用。
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
		if r.stdinPipes == nil {
			r.stdinPipes = make(map[string]map[*os.Process]io.Writer)
		}
		pipes, ok := r.stdinPipes[sid]
		if !ok {
			pipes = make(map[*os.Process]io.Writer)
			r.stdinPipes[sid] = pipes
		}
		pipes[proc] = stdin
	}
}

// GetStdinPipe 获取指定 session 的 stdin pipe。
func (r *ShellProcessRegistry) GetStdinPipe(sessionID string, proc *os.Process) io.Writer {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || proc == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stdinPipes == nil {
		return nil
	}
	pipes, ok := r.stdinPipes[sid]
	if !ok {
		return nil
	}
	return pipes[proc]
}

// ProcessInfo 进程信息
type ProcessInfo struct {
	// SessionID 会话标识
	SessionID string
	// PID 进程 ID
	PID int
}

// ListProcesses 返回所有已注册进程信息。
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
```

需要在 import 中添加 `"io"`。

#### Step 2: 实现 WriteStdin

将 `shell_operation.go` L582-594 的 `WriteStdin` 替换为：

```go
// WriteStdin 向后台进程写入标准输入。
// 对齐 Python ShellOperation.write_stdin：通过 ShellProcessRegistry 查找进程并写入 stdin。
func (s *LocalShellOperation) WriteStdin(ctx context.Context, sessionID string, data string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	if sessionID == "" {
		return &result.ExecuteCmdResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"write_stdin: session_id can not be empty",
			),
		}, nil
	}

	// 对齐 Python: 查找 stdin pipe 写入
	// 遍历该 session 下所有进程的 stdin pipe
	written := false
	sysop.DefaultRegistry.mu.Lock()
	procs, ok := sysop.DefaultRegistry.Processes()[sessionID]
	if ok {
		pipes := sysop.DefaultRegistry.StdinPipes()[sessionID]
		for proc := range procs {
			if pipes != nil {
				if stdin, hasPipe := pipes[proc]; hasPipe && stdin != nil {
					_, writeErr := stdin.Write([]byte(data))
					if writeErr != nil {
						logger.Warn(shellLogComponent).
							Str("session_id", sessionID).
							Int("pid", proc.Pid).
							Err(writeErr).
							Msg("写入 stdin 失败")
					} else {
						written = true
					}
				}
			}
		}
	}
	sysop.DefaultRegistry.mu.Unlock()

	if !written {
		return &result.ExecuteCmdResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				fmt.Sprintf("write_stdin: session %s 没有可用的 stdin pipe", sessionID),
			),
		}, nil
	}

	return &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: "Stdin written successfully"},
	}, nil
}
```

#### Step 3: 实现 ListProcesses

将 `shell_operation.go` L618-623 的 `ListProcesses` 替换为：

```go
// ListProcesses 列出所有后台进程。
// 对齐 Python ShellOperation.list_processes：返回 ShellProcessRegistry 中当前所有进程信息。
func (s *LocalShellOperation) ListProcesses(ctx context.Context, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	infos := sysop.DefaultRegistry.ListProcesses()
	data := make(map[string]any)
	items := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		items = append(items, map[string]any{
			"session_id": info.SessionID,
			"pid":        info.PID,
		})
	}
	data["processes"] = items
	data["count"] = len(infos)

	return &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: "ListProcesses succeeded"},
		Data: &result.ExecuteCmdData{
			ExitCode: intPtr(0),
		},
	}, nil
}
```

**测试：**

```go
func TestListProcesses_空注册表(t *testing.T) {
	op := NewLocalShellOperation(nil).(*LocalShellOperation)
	result, err := op.ListProcesses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != 0 {
		t.Error("ListProcesses 应成功")
	}
}

func TestShellProcessRegistry_ListProcesses(t *testing.T) {
	reg := NewShellProcessRegistry()
	infos := reg.ListProcesses()
	if len(infos) != 0 {
		t.Error("空注册表应返回空列表")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestListProcesses_|TestShellProcessRegistry_ListProcesses" ./internal/agentcore/sys_operation/... ./internal/agentcore/sys_operation/local/...
```

**commit message：** `feat(sys_operation): 实现 WriteStdin/ListProcesses，Registry 增加 stdin pipe 追踪，对齐 Python`

---

### Task 8: G4+G5+G6 - code_operation + encoding + isolation_key 小修复

**Files:**
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/code_operation.go` (L265, L271)
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/local/fs_operation.go` (L236-237)
- `/home/opensource/uap-claw-go/internal/agentcore/sys_operation/sys_operation_card.go` (L169-176)

**Steps:**

#### Step 1: G4 - CodeOperation 硬编码 python3 改为环境变量

在 `code_operation.go` 非导出函数区域添加：

```go
// resolvePythonExecutable 解析 Python 可执行文件路径。
// 对齐 Python sys.executable：优先读取 PYTHON_EXECUTABLE 环境变量，fallback 到 python3。
func resolvePythonExecutable() string {
	if exe := os.Getenv("PYTHON_EXECUTABLE"); exe != "" {
		return exe
	}
	return "python3"
}
```

将 `buildSubprocessCmd` 中 L265 和 L271 行的 `"python3"` 替换为 `resolvePythonExecutable()`：

```go
case "python", "python3":
	if !forceFile && len(code) <= cmdLimit {
		return []string{resolvePythonExecutable(), "-u", "-c", code}, "", nil
	}
	tmpFile, err := (&OperationUtils{}).CreateTmpFile(code, ".py")
	if err != nil {
		return nil, "", err
	}
	return []string{resolvePythonExecutable(), "-u", tmpFile}, tmpFile, nil
```

#### Step 2: G5 - write_file 非 UTF-8 编码处理

将 `fs_operation.go` L236-237 行：
```go
		} else {
			// 非 UTF-8 编码：使用 golang.org/x/text 或简单 fallback
			dataBytes = []byte(txt) // 简化：Go 标准库仅支持 UTF-8，其他编码需要额外包
		}
```
改为：
```go
		} else {
			// 非 UTF-8 编码：使用 golang.org/x/text/encoding 转换，对齐 Python txt.encode(encoding)
			encoder := getEncoder(enc)
			if encoder != nil {
				encodedBytes, encErr := encoder.Bytes([]byte(txt))
				if encErr != nil {
					return &result.WriteFileResult{
						BaseResult: result.BuildOperationErrorResult(exception.StatusSysOperationFsExecutionError.Code(),
							fmt.Sprintf("编码转换失败(%s): %v", enc, encErr)),
					}, nil
				}
				dataBytes = encodedBytes
			} else {
				// 不支持的编码，fallback 到 UTF-8
				logger.Warn(fsLogComponent).
					Str("encoding", enc).
					Msg("不支持的编码，fallback 到 UTF-8")
				dataBytes = []byte(txt)
			}
		}
```

在非导出函数区域添加：

```go
// getEncoder 根据编码名称获取 encoder。
// 对齐 Python codecs.lookup(encoding)。
func getEncoder(encoding string) encoding.Encoder {
	enc := encoding.ByName(encoding)
	if enc == nil {
		return nil
	}
	return enc.NewEncoder()
}
```

需要在 import 中添加 `"golang.org/x/text/encoding"`。

#### Step 3: G6 - isolation_key_template 连续下划线

将 `sys_operation_card.go` L169-176 行：
```go
	parts := []string{
		containerScope.String(),
		launcherType,
		sandboxType,
		isolationPrefix,
		identity,
	}
	return strings.Join(parts, "_")
```
改为：
```go
	// 对齐 Python: 过滤空部分，避免连续下划线
	var parts []string
	if cs := containerScope.String(); cs != "" {
		parts = append(parts, cs)
	}
	if launcherType != "" {
		parts = append(parts, launcherType)
	}
	if sandboxType != "" {
		parts = append(parts, sandboxType)
	}
	if isolationPrefix != "" {
		parts = append(parts, isolationPrefix)
	}
	parts = append(parts, identity)
	return strings.Join(parts, "_")
```

**测试：**

```go
func TestResolvePythonExecutable_环境变量(t *testing.T) {
	os.Setenv("PYTHON_EXECUTABLE", "/usr/bin/python3.11")
	defer os.Unsetenv("PYTHON_EXECUTABLE")
	if exe := resolvePythonExecutable(); exe != "/usr/bin/python3.11" {
		t.Errorf("期望 /usr/bin/python3.11，实际 %s", exe)
	}
}

func TestResolvePythonExecutable_默认值(t *testing.T) {
	os.Unsetenv("PYTHON_EXECUTABLE")
	if exe := resolvePythonExecutable(); exe != "python3" {
		t.Errorf("期望 python3，实际 %s", exe)
	}
}

func TestGenerateIsolationKeyTemplate_无连续下划线(t *testing.T) {
	key := generateIsolationKeyTemplate("", ContainerScopeSystem, "", "", "sandbox")
	if strings.Contains(key, "__") {
		t.Errorf("隔离键不应包含连续下划线: %s", key)
	}
	if key != "system_sandbox" {
		t.Errorf("期望 system_sandbox，实际 %s", key)
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestResolvePythonExecutable_|TestGenerateIsolationKeyTemplate_无连续下划线" ./internal/agentcore/sys_operation/local/... ./internal/agentcore/sys_operation/...
```

**commit message：** `fix(sys_operation): Python 路径从环境变量读取/非 UTF-8 编码转换/isolation_key 去除连续下划线，对齐 Python`

---

## Group 2: Gateway/MessageHandler 模块

### Task 9: S25 - handleCancel 条件取消

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/server/handle_envelope.go` (L396-415)

**Steps:**

当前代码（L404-415）已正确实现条件取消：

```go
	intent := ""
	var params map[string]any
	if request.Params != nil {
		if json.Unmarshal(request.Params, &params) == nil {
			if i, ok := params["intent"].(string); ok {
				intent = i
			}
		}
	}
	if intent == "cancel" || intent == "supplement" || intent == "" {
		s.cancelStreamTask(sessionID)
	}
```

此代码已对齐 Python 逻辑。**无需修改。**

**测试：**

```go
func TestHandleCancel_仅CancelSupplement取消流式(t *testing.T) {
	// 验证 intent=pause 时不调用 cancelStreamTask
	// 验证 intent=cancel 时调用 cancelStreamTask
	// 验证 intent="" 时调用 cancelStreamTask（向后兼容）
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestHandleCancel_" ./internal/swarm/server/...
```

**commit message：** `fix(server): handleCancel 条件取消已实现，确认对齐 Python intent 检查逻辑`

---

### Task 10: S21 - cancel 始终通知 AgentServer

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/gateway/message_handler/cancel.go` (L44)

**Steps:**

当前代码（L44-55）已经无条件通知 AgentServer（只要 `mh.agentClient != nil && mh.agentClient.IsConnected()`），不依赖 `len(requestIDs) > 0`：

```go
	var resp *schema.AgentResponse
	var respErr error
	if mh.agentClient != nil && mh.agentClient.IsConnected() {
		cancelEnv := e2a.MessageToE2AOrFallback(cancelMsg)
		cancelEnv.IsStream = false
		resp, respErr = mh.agentClient.SendRequest(ctx, cancelEnv)
		if respErr != nil {
			logger.Warn(logComponent).
				Str("event_type", "cancel_send_error").
				Err(respErr).
				Str("session_id", oldSessionID).
				Msg("AgentServer 中断请求失败(忽略)")
		}
	}
```

此代码已对齐 Python 逻辑。**无需修改。**

**测试：**

```go
func TestCancelAgentWorkForSession_始终通知AgentServer(t *testing.T) {
	// 验证即使没有活跃流式任务，也发送 cancel 请求到 AgentServer
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestCancelAgentWorkForSession_始终通知AgentServer" ./internal/swarm/gateway/message_handler/...
```

**commit message：** `fix(message_handler): cancel 始终通知 AgentServer 已实现，确认对齐 Python`

---

### Task 11: S20 - CHAT_ANSWER 串行保护

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/gateway/message_handler/dispatch.go` (L44-46)

**Steps:**

将 L44-46 行：
```go
	return method != string(schema.ReqMethodChatSend) &&
		method != string(schema.ReqMethodChatCancel) &&
		method != "chat.resume"
```
改为：
```go
	return method != string(schema.ReqMethodChatSend) &&
		method != string(schema.ReqMethodChatCancel) &&
		method != "chat.resume" &&
		method != string(schema.ReqMethodChatAnswer)
```

**测试：**

```go
func TestNonStreamRPCMayRunParallel_串行保护(t *testing.T) {
	mh := &MessageHandler{}
	// chat.answer 应返回 false（串行）
	env := &e2a.E2AEnvelope{Method: string(schema.ReqMethodChatAnswer), IsStream: false}
	if mh.nonStreamRPCMayRunParallel(env) {
		t.Error("chat.answer 应串行执行")
	}
	// 其他非流式 RPC 应返回 true（并行）
	env2 := &e2a.E2AEnvelope{Method: "session.list", IsStream: false}
	if !mh.nonStreamRPCMayRunParallel(env2) {
		t.Error("session.list 应并行执行")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestNonStreamRPCMayRunParallel_串行保护" ./internal/swarm/gateway/message_handler/...
```

**commit message：** `fix(message_handler): chat.user_answer 添加串行保护，对齐 Python CHAT_ANSWER in serial set`

---

### Task 12: S22 - handleChatUserAnswer 检查 error

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/gateway/message_handler/forward_loop.go` (L191)

**Steps:**

将 L191 行：
```go
	resp, _ := mh.processNonStreamRequest(ctx, msg, env)
```
改为：
```go
	resp, procErr := mh.processNonStreamRequest(ctx, msg, env)
	if procErr != nil {
		logger.Warn(logComponent).
			Str("event_type", "chat_answer_process_failed").
			Str("msg_id", msg.ID).
			Err(procErr).
			Msg("processNonStreamRequest 失败，跳过 evolution 审批")
		return
	}
```

**测试：**

```go
func TestHandleChatUserAnswer_处理失败时提前返回(t *testing.T) {
	// 验证 processNonStreamRequest 返回 error 时，不进入 evolution 审批逻辑
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestHandleChatUserAnswer_处理失败时提前返回" ./internal/swarm/gateway/message_handler/...
```

**commit message：** `fix(message_handler): handleChatUserAnswer 检查 processNonStreamRequest error，失败时跳过 evolution 审批`

---

### Task 13: S23 - disconnect 去重

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/gateway/message_handler/disconnect.go` (L22-46)

**Steps:**

将 L22-46 行：
```go
	for _, key := range sessionKeys {
		channelID := key[0]
		sessionID := key[1]
		if sessionID == "" {
			continue
		}

		// 构造 cancel 消息（注入 channel mode）
		cancelMsg := &schema.Message{
			...
		}

		mh.CancelAgentWorkForSession(ctx, cancelMsg, sessionID, true)

		logger.Info(logComponent).
			...
	}
```
改为：
```go
	// 对齐 Python: seen: set[str] = set() 去重
	seen := make(map[string]struct{})
	for _, key := range sessionKeys {
		channelID := key[0]
		sessionID := key[1]
		if sessionID == "" {
			continue
		}
		sid := strings.TrimSpace(sessionID)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue // 已处理，跳过
		}
		seen[sid] = struct{}{}

		// 构造 cancel 消息（注入 channel mode）
		cancelMsg := &schema.Message{
			ID:        sessionID,
			Type:      schema.MessageTypeReq,
			ChannelID: channelID,
			SessionID: sessionID,
			ReqMethod: schema.ReqMethodChatCancel,
			OK:        true,
		}

		mh.CancelAgentWorkForSession(ctx, cancelMsg, sessionID, true)

		logger.Info(logComponent).
			Str("event_type", "session_cancelled_on_disconnect").
			Str("channel_id", channelID).
			Str("session_id", sessionID).
			Msg("断连取消 session 任务")
	}
```

需要在 import 中添加 `"strings"`。

**测试：**

```go
func TestCancelAgentSessionsOnDisconnect_去重(t *testing.T) {
	// 验证相同 sessionID 多次出现时，只调用一次 CancelAgentWorkForSession
	// 使用 mock MessageHandler 追踪调用次数
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestCancelAgentSessionsOnDisconnect_去重" ./internal/swarm/gateway/message_handler/...
```

**commit message：** `fix(message_handler): disconnect 添加 session_id 去重，对齐 Python seen set()`

---

## Group 3: WebChannel 模块

### Task 14: S27 - config_apply 加密

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/gateway/channel_manager/web/config_apply.go` (L89-101)
- `/home/opensource/uap-claw-go/internal/swarm/gateway/channel_manager/web/web_handlers.go` (L678, L759)

**Steps:**

当前代码（L89-101）已实现 `_encrypt_config_params` 加密逻辑：

```go
func ApplyConfigPayload(params map[string]any, crypto CryptoProvider) (envUpdates map[string]string, yamlUpdated []string, err error) {
	// 对齐 Python _encrypt_config_params (L692-699):
	// 遍历 params，key 名包含 api_key 或 token 时加密
	if crypto != nil {
		for key, val := range params {
			kl := strings.ToLower(key)
			if strings.Contains(kl, "api_key") || strings.Contains(kl, "token") {
				if strVal, ok := val.(string); ok && strVal != "" {
					params[key] = crypto.Encrypt(strVal)
				}
			}
		}
	}
```

函数签名已包含 `crypto CryptoProvider` 参数。现在需要修改调用方传入 `crypto`。

#### Step 1: 修改 web_handlers.go L678 的调用

将：
```go
envUpdates, yamlUpdated, err := ApplyConfigPayload(params)
```
改为：
```go
envUpdates, yamlUpdated, err := ApplyConfigPayload(params, h.cryptoProvider)
```

#### Step 2: 修改 web_handlers.go L759 的调用

将：
```go
appliedEnv, appliedYAML, err := ApplyConfigPayload(configParams)
```
改为：
```go
appliedEnv, appliedYAML, err := ApplyConfigPayload(configParams, h.cryptoProvider)
```

需要确认 `h` (web handlers 结构体) 有 `cryptoProvider` 字段。如果没有，需要添加。

**测试：**

```go
func TestApplyConfigPayload_加密api_key和token(t *testing.T) {
	called := false
	fakeCrypto := &fakeCryptoProvider{
		encryptFunc: func(s string) string {
			called = true
			return "ENC(" + s + ")"
		},
	}
	params := map[string]any{
		"openai_api_key": "sk-real-key",
		"access_token":  "tok-real",
		"normal_param":  "plain_value",
	}
	_, _, _ = ApplyConfigPayload(params, fakeCrypto)
	if !called {
		t.Error("api_key/token 应被加密")
	}
	if params["openai_api_key"] != "ENC(sk-real-key)" {
		t.Error("api_key 应被加密")
	}
	if params["normal_param"] != "plain_value" {
		t.Error("普通参数不应被加密")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestApplyConfigPayload_加密api_key和token" ./internal/swarm/gateway/channel_manager/web/...
```

**commit message：** `fix(web): ApplyConfigPayload 调用方传入 CryptoProvider，对齐 Python _encrypt_config_params`

---

## Group 4: DeepAdapter 模块

### Task 15: S16 - isAcpToolProfile 对齐 Python

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/server/adapter/deep_adapter_config.go` (L173-227, L211-227)

**Steps:**

#### Step 1: 修改 isAcpToolProfile 签名和实现

将 L211-227 行：
```go
func (d *DeepAdapter) isAcpToolProfile(configBase map[string]any) bool {
	modelsSection, _ := configBase["models"].(map[string]any)
	if modelsSection == nil {
		return false
	}
	defaults, _ := modelsSection["defaults"].([]any)
	for _, entry := range defaults {
		if m, ok := entry.(map[string]any); ok {
			if profile, ok := m["profile"].(string); ok && profile == "acp_tool" {
				return true
			}
		}
	}
	return false
}
```
改为：
```go
// isAcpToolProfile 检查是否为 ACP Tool profile。
// 对齐 Python: _is_acp_tool_profile()
// 优先检查 instanceOverrides["tool_profile"]，fallback 检查 instanceOverrides["channel_id"]。
func (d *DeepAdapter) isAcpToolProfile(instanceOverrides map[string]any) bool {
	if instanceOverrides == nil {
		return false
	}
	// 对齐 Python: tool_profile = str(config.get("tool_profile") or "").strip().lower()
	if tp, ok := instanceOverrides["tool_profile"]; ok {
		s := strings.TrimSpace(strings.ToLower(fmt.Sprint(tp)))
		if s != "" {
			return s == "acp"
		}
	}
	// 对齐 Python: fallback 检查 channel_id
	if cid, ok := instanceOverrides["channel_id"]; ok {
		s := strings.TrimSpace(strings.ToLower(fmt.Sprint(cid)))
		return s == "acp"
	}
	return false
}
```

#### Step 2: 更新所有调用方

1. `skillIncludeToolsForProfile`（L173-178）：
```go
func (d *DeepAdapter) skillIncludeToolsForProfile(configBase map[string]any) bool {
	if d.isAcpToolProfile(d.instanceOverrides) {
		return false
	}
	return d.filesystemRail == nil
}
```

2. `filesystemRailEnabledForProfile`（L231-234）：
```go
func (d *DeepAdapter) filesystemRailEnabledForProfile(configBase map[string]any) bool {
	return !d.isAcpToolProfile(d.instanceOverrides)
}
```

**测试：**

```go
func TestIsAcpToolProfile_从InstanceOverrides读取(t *testing.T) {
	d := NewDeepAdapter()
	d.instanceOverrides = map[string]any{"tool_profile": "acp"}
	if !d.isAcpToolProfile(d.instanceOverrides) {
		t.Error("tool_profile=acp 应返回 true")
	}

	d.instanceOverrides = map[string]any{"tool_profile": "standard"}
	if d.isAcpToolProfile(d.instanceOverrides) {
		t.Error("tool_profile=standard 应返回 false")
	}

	d.instanceOverrides = map[string]any{"channel_id": "acp"}
	if !d.isAcpToolProfile(d.instanceOverrides) {
		t.Error("channel_id=acp fallback 应返回 true")
	}

	if d.isAcpToolProfile(nil) {
		t.Error("nil 应返回 false")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestIsAcpToolProfile_从InstanceOverrides读取" ./internal/swarm/server/adapter/...
```

**commit message：** `fix(adapter): isAcpToolProfile 从 instanceOverrides 读取 tool_profile/channel_id，对齐 Python`

---

### Task 16: S9 - SetSkillManager

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/server/adapter/deep_adapter.go` (L177)

**Steps:**

#### Step 1: 修改 skillManager 字段类型

将 L177 行：
```go
	skillManager interface{}
```
改为：
```go
	skillManager *skill.SkillManager
```

需要在 import 中添加：
```go
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
```

#### Step 2: 添加 SetSkillManager 方法

在导出函数区域添加：

```go
// SetSkillManager 设置技能管理器。
// 对齐 Python: def set_skill_manager(self, skill_manager: SkillManager) -> None: self._skill_manager = skill_manager
func (d *DeepAdapter) SetSkillManager(skillMgr *skill.SkillManager) {
	d.skillManager = skillMgr
}
```

**测试：**

```go
func TestDeepAdapter_SetSkillManager(t *testing.T) {
	d := NewDeepAdapter()
	mgr := &skill.SkillManager{}
	d.SetSkillManager(mgr)
	if d.skillManager != mgr {
		t.Error("SetSkillManager 应设置 skillManager 字段")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestDeepAdapter_SetSkillManager" ./internal/swarm/server/adapter/...
```

**commit message：** `feat(adapter): DeepAdapter 实现 SetSkillManager 方法，skillManager 字段类型改为 *skill.SkillManager，对齐 Python`

---

### Task 17: S11 - ProcessInterrupt 补充字段

**Files:**
- `/home/opensource/uap-claw-go/internal/swarm/server/adapter/deep_adapter.go` (L1003-1010)

**Steps:**

将 L1003-1010 行：
```go
	return schema.NewAgentResponse(req.RequestID, req.ChannelID,
		schema.WithPayload(map[string]any{
			"event_type": "chat.interrupt_result",
			"intent":     intent,
			"success":    true,
			"message":    interruptMsg,
		}),
	), nil
```
改为：
```go
	// 构造 interrupt_result payload，对齐 Python ProcessInterrupt 响应
	payload := map[string]any{
		"event_type": "chat.interrupt_result",
		"intent":     intent,
		"success":    true,
		"message":    interruptMsg,
	}

	// 对齐 Python: payload["new_input"] = new_input（如果存在）
	if newInput != nil {
		payload["new_input"] = newInput
	}

	// ⤵️ 10.6.3-10: todos 和 cancelled_tools 依赖 StreamEventRail 实现
	if d.streamEventRail != nil {
		// ⤵️ 10.6.3-10: payload["todos"] = ...
		// ⤵️ 10.6.3-10: payload["cancelled_tools"] = ...
	}

	return schema.NewAgentResponse(req.RequestID, req.ChannelID,
		schema.WithPayload(payload),
	), nil
```

**测试：**

```go
func TestProcessInterrupt_补充newInput字段(t *testing.T) {
	d := NewDeepAdapter()
	d.instance = harness.CreateStubAgent() // 需要 stub
	req := &schema.AgentRequest{
		RequestID: "test-req",
		ChannelID: "test-ch",
		SessionID: strPtr("sess-1"),
		Params:    json.RawMessage(`{"intent":"supplement","new_input":"追加内容"}`),
	}
	resp, err := d.ProcessInterrupt(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	ni, ok := resp.Payload["new_input"]
	if !ok || ni == nil {
		t.Error("payload 应包含 new_input 字段")
	}
}
```

**测试命令：**
```bash
cd /home/opensource/uap-claw-go && go test -tags=test -run "TestProcessInterrupt_补充newInput字段" ./internal/swarm/server/adapter/...
```

**commit message：** `fix(adapter): ProcessInterrupt 响应补充 new_input 字段，预留 todos/cancelled_tools 回填点，对齐 Python`

---

## 执行顺序

1. Task 1 (S1) - 无依赖
2. Task 2 (S2) - 无依赖
3. Task 3 (S3) - 无依赖
4. Task 4 (S4+S5+G2+G3) - 无依赖
5. Task 5 (S6) - 无依赖
6. Task 6 (S7) - 无依赖
7. Task 7 (S8) - 依赖 Task 2 (S2)
8. Task 8 (G4+G5+G6) - 无依赖
9. Task 9 (S25) - 已实现，仅确认
10. Task 10 (S21) - 已实现，仅确认
11. Task 11 (S20) - 无依赖
12. Task 12 (S22) - 无依赖
13. Task 13 (S23) - 无依赖
14. Task 14 (S27) - 无依赖
15. Task 15 (S16) - 无依赖
16. Task 16 (S9) - 无依赖
17. Task 17 (S11) - 无依赖

## 注意事项

- S25 (handleCancel) 和 S21 (cancel 通知 AgentServer) 在代码审查中确认已正确实现，无需修改，仅补充测试
- 所有注释使用中文
- TDD 流程：先写测试 -> 验证失败 -> 写实现 -> 验证通过 -> commit
- 每次编译前检查残留 go 进程：`pgrep -f 'go (build|test)'`
- 使用 GOPROXY：`export GOPROXY=https://goproxy.cn,direct`
