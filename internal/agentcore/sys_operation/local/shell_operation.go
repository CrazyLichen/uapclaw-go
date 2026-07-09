package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DangerousPattern 危险命令模式。
// 对齐 Python ShellOperation._DANGEROUS_PATTERNS。
type DangerousPattern struct {
	// Pattern 正则模式
	Pattern *regexp.Regexp
	// Label 模式标签
	Label string
}

// TUICommandPattern TUI 命令模式。
// 对齐 Python ShellOperation._TUI_COMMAND_PATTERNS。
type TUICommandPattern struct {
	// Pattern 正则模式
	Pattern *regexp.Regexp
	// Desc 检测描述
	Desc string
	// AutoEnv 自动注入的环境变量
	AutoEnv map[string]string
}

// ShellOperation 本地 Shell 操作。
// 对齐 Python local/shell_operation.py ShellOperation。
type ShellOperation struct {
	sysop.BaseShellOperation
	// dangerousPatterns 危险命令正则
	dangerousPatterns []DangerousPattern
	// tuiCommandPatterns TUI 命令检测正则
	tuiCommandPatterns []TUICommandPattern
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// shellLogComponent 日志组件
	shellLogComponent = logger.ComponentAgentCore
	// defaultExecCmdTimeout 默认命令执行超时
	defaultExecCmdTimeout = 300
	// defaultShellEncoding 默认 Shell 编码
	defaultShellEncoding = "utf-8"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 ShellOperation 满足 ShellOperation 接口
var _ sysop.ShellOperation = (*ShellOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewShellOperation 创建本地 Shell 操作实例（工厂函数，供 OperationRegistry 调用）。
func NewShellOperation(runConfig any) sysop.SysSubOperation {
	op := &ShellOperation{}
	op.initPatterns()
	return op
}

// ExecuteCmd 执行 Shell 命令。
// 对齐 Python ShellOperation.execute_cmd：参数校验 → 安全检查 → 超时上限 →
// 环境变量准备 → TUI 检测 → 子进程创建 → Shell 进程注册 → 执行 → 注销 → 结果构造。
func (s *ShellOperation) ExecuteCmd(ctx context.Context, command string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	o := sysop.NewShellOptions(opts...)
	methodName := "execute_cmd"

	startTime := time.Now()
	logger.Info(shellLogComponent).
		Str("event_type", "SYS_OP_START").
		Str("method_name", methodName).
		Str("command", truncate(command, 200)).
		Msg("开始执行命令")

	createErrResult := func(errMsg string, data *result.ExecuteCmdData) *result.ExecuteCmdResult {
		if data != nil && data.ExitCode == nil {
			exitCode := -1
			data.ExitCode = &exitCode
		}
		errResult := &result.ExecuteCmdResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				fmt.Sprintf("execute_cmd: %s", errMsg),
			),
			Data: data,
		}
		logger.Error(shellLogComponent).
			Str("event_type", "SYS_OP_ERROR").
			Str("method_name", methodName).
			Str("error_msg", errMsg).
			Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
			Msg("执行命令失败")
		return errResult
	}

	// 空命令校验
	if command == "" || strings.TrimSpace(command) == "" {
		return createErrResult("command can not be empty", nil), nil
	}

	actualCwd := ResolveCwd(o.Cwd)

	// 安全检查
	if blocked := s.checkCommandSafety(command); blocked != "" {
		return createErrResult(fmt.Sprintf("command rejected for safety: %s", blocked),
			&result.ExecuteCmdData{Command: command, Cwd: actualCwd}), nil
	}

	// 框架层超时上限
	maxTimeout := 600
	if v := os.Getenv("JW_EXECUTE_CMD_MAX_TIMEOUT"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &maxTimeout); n != 1 || err != nil {
			maxTimeout = 600
		}
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultExecCmdTimeout
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}

	// 环境变量准备
	opUtils := OperationUtils{}
	execEnv := opUtils.PrepareEnvironment(o.Environment)

	// TUI 检测
	s.detectAndMitigateTUI(command, execEnv)

	// 解析执行计划
	args, _, shellName := s.resolveExecutionPlan(command, o.ShellType)

	// 创建子进程
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = envMapToSlice(execEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 创建进程处理器
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, defaultShellEncoding, timeout)

	// 执行
	invokeData, err := handler.Invoke(ctx)
	if err != nil {
		return createErrResult(fmt.Sprintf("unexpected error: %s", err),
			&result.ExecuteCmdData{Command: command, Cwd: actualCwd}), nil
	}

	if invokeData.Exception != nil {
		return createErrResult(fmt.Sprintf("execution error: %s", invokeData.Exception),
			&result.ExecuteCmdData{
				Command:  command,
				Cwd:     actualCwd,
				ExitCode: &invokeData.ExitCode,
				Stdout:  invokeData.Stdout,
				Stderr:  invokeData.Stderr,
			}), nil
	}

	exitCode := invokeData.ExitCode
	successResult := &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: "Command executed successfully"},
		Data: &result.ExecuteCmdData{
			Command:  command,
			Cwd:     actualCwd,
			ExitCode: &exitCode,
			Stdout:  invokeData.Stdout,
			Stderr:  invokeData.Stderr,
		},
	}

	logger.Info(shellLogComponent).
		Str("event_type", "SYS_OP_END").
		Str("method_name", methodName).
		Str("shell_name", shellName).
		Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
		Msg("命令执行完成")

	return successResult, nil
}

// ExecuteCmdStream 流式执行 Shell 命令。
// 对齐 Python ShellOperation.execute_cmd_stream。
func (s *ShellOperation) ExecuteCmdStream(ctx context.Context, command string, opts ...sysop.ShellOption) (<-chan result.ExecuteCmdStreamResult, error) {
	o := sysop.NewShellOptions(opts...)
	ch := make(chan result.ExecuteCmdStreamResult, 64)

	if command == "" || strings.TrimSpace(command) == "" {
		exitCode := -1
		ch <- result.ExecuteCmdStreamResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"command can not be empty",
			),
			Data: &result.ExecuteCmdChunkData{ChunkIndex: 0, ExitCode: &exitCode},
		}
		close(ch)
		return ch, nil
	}

	actualCwd := ResolveCwd(o.Cwd)

	if blocked := s.checkCommandSafety(command); blocked != "" {
		exitCode := -1
		ch <- result.ExecuteCmdStreamResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				fmt.Sprintf("command rejected for safety: %s", blocked),
			),
			Data: &result.ExecuteCmdChunkData{ChunkIndex: 0, ExitCode: &exitCode},
		}
		close(ch)
		return ch, nil
	}

	// 创建子进程
	args, _, _ := s.resolveExecutionPlan(command, o.ShellType)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, defaultShellEncoding, o.Timeout)

	streamCh, err := handler.Stream(ctx)
	if err != nil {
		close(ch)
		return ch, err
	}

	go func() {
		defer close(ch)
		chunkIndex := 0
		for event := range streamCh {
			switch event.Type {
			case StreamEventTypeStdout, StreamEventTypeStderr:
				eventType := event.Type.String()
				ch <- result.ExecuteCmdStreamResult{
					BaseResult: result.BaseResult{Code: 0, Message: "stream output"},
					Data: &result.ExecuteCmdChunkData{
						Text:       event.Data,
						Type:       &eventType,
						ChunkIndex: chunkIndex,
					},
				}
				chunkIndex++
			case StreamEventTypeExit:
				ch <- result.ExecuteCmdStreamResult{
					BaseResult: result.BaseResult{Code: 0, Message: "Command executed successfully"},
					Data: &result.ExecuteCmdChunkData{
						ChunkIndex: chunkIndex,
						ExitCode:  &event.ExitCode,
					},
				}
				return
			case StreamEventTypeError:
				exitCode := -1
				ch <- result.ExecuteCmdStreamResult{
					BaseResult: result.BuildOperationErrorResult(
						exception.StatusSysOperationShellExecutionError.Code(),
						fmt.Sprintf("execution error: %s", event.Data),
					),
					Data: &result.ExecuteCmdChunkData{ChunkIndex: chunkIndex, ExitCode: &exitCode},
				}
				return
			}
		}
	}()

	return ch, nil
}

// ExecuteCmdBackground 后台执行 Shell 命令。
// 对齐 Python ShellOperation.execute_cmd_background。
func (s *ShellOperation) ExecuteCmdBackground(ctx context.Context, command string, opts ...sysop.ShellOption) (*result.ExecuteCmdBackgroundResult, error) {
	o := sysop.NewShellOptions(opts...)
	methodName := "execute_cmd_background"

	if command == "" || strings.TrimSpace(command) == "" {
		return &result.ExecuteCmdBackgroundResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"command can not be empty",
			),
		}, nil
	}

	actualCwd := ResolveCwd(o.Cwd)

	if blocked := s.checkCommandSafety(command); blocked != "" {
		return &result.ExecuteCmdBackgroundResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				fmt.Sprintf("command rejected for safety: %s", blocked),
			),
			Data: &result.ExecuteCmdBackgroundData{Command: command, Cwd: actualCwd},
		}, nil
	}

	// 创建后台子进程
	args, _, _ := s.resolveExecutionPlan(command, o.ShellType)

	var cmd *exec.Cmd
	cmd = exec.Command(args[0], args[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, defaultShellEncoding, 0)

	pid, err := handler.Background(3.0)
	if err != nil {
		return &result.ExecuteCmdBackgroundResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				fmt.Sprintf("background command failed: %s", err),
			),
			Data: &result.ExecuteCmdBackgroundData{Command: command, Cwd: actualCwd},
		}, nil
	}

	logger.Info(shellLogComponent).
		Str("event_type", "SYS_OP_END").
		Str("method_name", methodName).
		Int("pid", pid).
		Msg("后台命令启动成功")

	return &result.ExecuteCmdBackgroundResult{
		BaseResult: result.BaseResult{Code: 0, Message: "Background command started successfully"},
		Data: &result.ExecuteCmdBackgroundData{
			Command: command,
			Cwd:    actualCwd,
			Pid:    &pid,
		},
	}, nil
}

// ListTools 返回 Shell 操作的工具卡片列表（硬编码）。
// description 严格使用 Python 方法英文 docstring 原文，不翻译。
// 对齐 Python BaseShellOperation.list_tools：execute_cmd, execute_cmd_stream, execute_cmd_background。
func (s *ShellOperation) ListTools() []*tool.ToolCard {
	return []*tool.ToolCard{
		tool.NewToolCard(
			"execute_cmd",
			"Asynchronously execute a command(shell mode only).",
			[]*schema.Param{
				{Name: "command", Description: "Command to execute.", Type: schema.ParamTypeString, Required: true},
				{Name: "cwd", Description: "Working directory for command execution (default: current directory).", Type: schema.ParamTypeString, Nullable: true},
				{Name: "timeout", Description: "Command execution timeout in seconds (default: 300 seconds).", Type: schema.ParamTypeInteger, Default: 300},
				{Name: "environment", Description: "Key-value dict of custom environment variables.", Type: schema.ParamTypeObject, Nullable: true},
				{Name: "options", Description: "Additional execution configuration options.", Type: schema.ParamTypeObject, Nullable: true},
				{Name: "shell_type", Description: `Shell to use, one of "auto"/"cmd"/"powershell"/"bash"/"sh" (default: "auto").`, Type: schema.ParamTypeString, Default: "auto",
					Enum: []any{"auto", "cmd", "powershell", "bash", "sh"}},
			},
			nil,
		),
		tool.NewToolCard(
			"execute_cmd_stream",
			"Asynchronously execute a command streaming(shell mode only).",
			[]*schema.Param{
				{Name: "command", Description: "Command to execute.", Type: schema.ParamTypeString, Required: true},
				{Name: "cwd", Description: "Working directory for command execution (default: current directory).", Type: schema.ParamTypeString, Nullable: true},
				{Name: "timeout", Description: "Command execution timeout in seconds (default: 300 seconds).", Type: schema.ParamTypeInteger, Default: 300},
				{Name: "environment", Description: "Key-value dict of custom environment variables.", Type: schema.ParamTypeObject, Nullable: true},
				{Name: "options", Description: "Additional execution configuration options.", Type: schema.ParamTypeObject, Nullable: true},
				{Name: "shell_type", Description: `Shell to use, one of "auto"/"cmd"/"powershell"/"bash"/"sh" (default: "auto").`, Type: schema.ParamTypeString, Default: "auto",
					Enum: []any{"auto", "cmd", "powershell", "bash", "sh"}},
			},
			nil,
		),
		tool.NewToolCard(
			"execute_cmd_background",
			"Launch a command in the background and return immediately with its PID.",
			[]*schema.Param{
				{Name: "command", Description: "Command to execute.", Type: schema.ParamTypeString, Required: true},
				{Name: "cwd", Description: "Working directory for command execution (default: current directory).", Type: schema.ParamTypeString, Nullable: true},
				{Name: "environment", Description: "Key-value dict of custom environment variables.", Type: schema.ParamTypeObject, Nullable: true},
				{Name: "grace", Description: "Seconds to wait for early failure detection (default: 3.0).", Type: schema.ParamTypeNumber, Default: 3.0},
				{Name: "shell_type", Description: `Shell to use, one of "auto"/"cmd"/"powershell"/"bash"/"sh" (default: "auto").`, Type: schema.ParamTypeString, Default: "auto",
					Enum: []any{"auto", "cmd", "powershell", "bash", "sh"}},
			},
			nil,
		),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initPatterns 初始化危险模式和 TUI 模式。
// 对齐 Python ShellOperation._DANGEROUS_PATTERNS 和 _TUI_COMMAND_PATTERNS。
func (s *ShellOperation) initPatterns() {
	s.dangerousPatterns = []DangerousPattern{
		{regexp.MustCompile(`\brm\s+-rf\b`), "rm -rf"},
		{regexp.MustCompile(`\bdel\s+/[a-z]*[fsq][a-z]*\b`), "del /f /s /q"},
		{regexp.MustCompile(`\brd\s+/s\s+/q\b`), "rd /s /q"},
		{regexp.MustCompile(`\bformat\s+[a-z]:`), "format drive"},
		{regexp.MustCompile(`\bshutdown\b`), "shutdown"},
		{regexp.MustCompile(`\breboot\b`), "reboot"},
		{regexp.MustCompile(`\bdiskpart\b`), "diskpart"},
		{regexp.MustCompile(`\bmkfs\b`), "mkfs"},
		{regexp.MustCompile(`\breg\s+delete\b`), "reg delete"},
		{regexp.MustCompile(`\bremove-item\b[^\n\r]*-recurse[^\n\r]*-force`), "Remove-Item -Recurse -Force"},
		{regexp.MustCompile(`\bpkill\b[^\n\r;|&]*jiuwenswarm`), "pkill targeting jiuwenswarm backend"},
		{regexp.MustCompile(`\bkillall\b[^\n\r;|&]*jiuwenswarm`), "killall targeting jiuwenswarm backend"},
		{regexp.MustCompile(`\bpkill\b[^\n\r;|&]*jiuwenclaw`), "pkill targeting jiuwenclaw backend"},
		{regexp.MustCompile(`\bkillall\b[^\n\r;|&]*jiuwenclaw`), "killall targeting jiuwenclaw backend"},
	}

	s.tuiCommandPatterns = []TUICommandPattern{
		{regexp.MustCompile(`\b(npx\s+)?playwright\s+test\b`), "Playwright test runner may require TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\b(npm|npx|yarn|pnpm)\s+(run\s+)?test\b`), "Test runner (npm/pnpm/yarn) may require TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\bvitest\b.*(--watch|--ui)`), "Vitest watch/UI mode requires TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\b(top|htop|vim|vi|nano|less|more)\b`), "Interactive TUI program will hang without TTY", nil},
	}
}

// checkCommandSafety 检查命令安全性。返回匹配的标签，空字符串表示安全。
// 对齐 Python ShellOperation._check_command_safety。
func (s *ShellOperation) checkCommandSafety(command string) string {
	for _, dp := range s.dangerousPatterns {
		if dp.Pattern.MatchString(command) {
			return dp.Label
		}
	}
	return ""
}

// detectAndMitigateTUI 检测 TUI/PTY 依赖命令并注入缓解环境变量。
// 对齐 Python ShellOperation._detect_and_mitigate_tui。
func (s *ShellOperation) detectAndMitigateTUI(command string, execEnv map[string]string) (bool, string) {
	if v := os.Getenv("JW_TUI_DETECTION_ENABLED"); v != "" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "no", "off":
			return false, ""
		}
	}
	for _, tp := range s.tuiCommandPatterns {
		if tp.Pattern.MatchString(command) {
			if tp.AutoEnv != nil {
				for k, v := range tp.AutoEnv {
					if _, exists := execEnv[k]; !exists {
						execEnv[k] = v
					}
				}
			}
			return true, fmt.Sprintf("TUI command detected: %s (auto-mitigated)", tp.Desc)
		}
	}
	return false, ""
}

// resolveExecutionPlan 解析命令执行计划。
// 对齐 Python ShellOperation._resolve_execution_plan。
// 返回 args 列表：useShell=true 时为 [shellName, "-c", command]；useShell=false 时为 [executable, arg1, ...]。
func (s *ShellOperation) resolveExecutionPlan(command string, shellType sysop.ShellType) (args []string, useShell bool, shellName string) {
	isWindows := runtime.GOOS == "windows"

	// 尝试解包 PowerShell -Command 包装
	// 对齐 Python：先尝试 unwrap_powershell_command
	if unwrapped := UnwrapPowerShellCommand(command); unwrapped != "" {
		command = unwrapped
	}

	switch shellType {
	case sysop.ShellTypePowerShell:
		pwshPath := AvailablePowerShell()
		return []string{pwshPath, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"

	case sysop.ShellTypeBash:
		bashPath := AvailableBash(true)
		if bashPath == "" {
			bashPath = "/bin/bash"
		}
		if isWindows {
			return []string{bashPath, "-lc", NormalizeWindowsPathsForBash(command)}, false, "bash"
		}
		return []string{bashPath, "-lc", command}, false, "bash"

	case sysop.ShellTypeCmd:
		return []string{"cmd", "/c", command}, true, "cmd"

	case sysop.ShellTypeSh:
		shPath := AvailableSh()
		if shPath == "" {
			shPath = "sh"
		}
		return []string{shPath, "-c", command}, true, "sh"

	case sysop.ShellTypeAuto:
		fallthrough
	default:
		// 自动检测逻辑，对齐 Python _resolve_execution_plan 的 auto 分支
		if isWindows {
			if LooksLikePowerShell(command) {
				pwshPath := AvailablePowerShell()
				return []string{pwshPath, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"
			}
			return []string{"cmd", "/c", command}, true, "cmd"
		}

		// 非 Windows：优先 bash，fallback sh
		if LooksLikePowerShell(command) {
			pwshPath := AvailablePowerShell()
			return []string{pwshPath, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"
		}

		bashPath := AvailableBash(true)
		if bashPath != "" {
			return []string{bashPath, "-lc", command}, false, "bash"
		}

		shPath := AvailableSh()
		if shPath != "" {
			return []string{shPath, "-c", command}, true, "sh"
		}

		return []string{"sh", "-c", command}, true, "sh"
	}
}

// envMapToSlice 环境变量 map → slice
func envMapToSlice(env map[string]string) []string {
	slice := make([]string, 0, len(env))
	for k, v := range env {
		slice = append(slice, k+"="+v)
	}
	return slice
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// WriteStdin 向后台进程写入标准输入。
// 对齐 Python ShellOperation.write_stdin：通过 ShellProcessRegistry 查找进程并写入 stdin。
func (s *ShellOperation) WriteStdin(ctx context.Context, sessionID string, data string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	if sessionID == "" {
		return &result.ExecuteCmdResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"write_stdin: session_id can not be empty",
			),
		}, nil
	}
	// 当前实现：ShellProcessRegistry 追踪的是 *os.Process，没有 stdin pipe 引用。
	// 返回未实现错误，待后续迭代补充 stdin pipe 追踪。
	return nil, fmt.Errorf("未实现: WriteStdin（需补充 stdin pipe 追踪）")
}

// KillProcess 终止指定后台进程。
// 对齐 Python ShellOperation.kill_process：通过 ShellProcessRegistry 查找并终止进程。
func (s *ShellOperation) KillProcess(ctx context.Context, sessionID string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	if sessionID == "" {
		return &result.ExecuteCmdResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"kill_process: session_id can not be empty",
			),
		}, nil
	}
	_ = sysop.KillShellProcessesForSession(sessionID)
	return &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: "Process killed successfully"},
		Data: &result.ExecuteCmdData{
			ExitCode: intPtr(0),
		},
	}, nil
}

// ListProcesses 列出所有后台进程。
// 对齐 Python ShellOperation.list_processes：返回 ShellProcessRegistry 中当前所有进程信息。
func (s *ShellOperation) ListProcesses(ctx context.Context, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	// 当前实现：ShellProcessRegistry 未暴露列举接口，返回空列表。
	return &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: "ListProcesses not fully implemented"},
	}, nil
}

// intPtr 返回 int 的指针
func intPtr(v int) *int { return &v }

// init 注册到 GlobalRegistry
func init() {
	_ = sysop.GlobalRegistry.Register(sysop.OperationDef{
		Name:        "shell",
		Mode:        sysop.OperationModeLocal,
		Description: "local shell operation",
		NewFunc:     NewShellOperation,
	})
}
