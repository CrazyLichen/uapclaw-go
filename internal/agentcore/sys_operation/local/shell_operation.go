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

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
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

// LocalShellOperation 本地 Shell 操作。
// 对齐 Python local/shell_operation.py ShellOperation。
type LocalShellOperation struct {
	sysop.BaseShellOperation
	// runConfig 本地工作配置，对齐 Python self._run_config。
	runConfig *sysop.LocalWorkConfig
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

// 编译时验证 LocalShellOperation 满足 ShellOperation 接口
var _ sysop.ShellOperation = (*LocalShellOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLocalShellOperation 创建本地 Shell 操作实例（工厂函数，供 OperationRegistry 调用）。
// 对齐 Python：run_config 传递到实例，用于 shell_allowlist 和 dangerous_patterns。
func NewLocalShellOperation(runConfig any) sysop.SysSubOperation {
	op := &LocalShellOperation{}
	// 解析 runConfig，对齐 Python：self._run_config = card.work_config or LocalWorkConfig()
	if rc, ok := runConfig.(*sysop.LocalWorkConfig); ok && rc != nil {
		op.runConfig = rc
	} else {
		op.runConfig = sysop.NewLocalWorkConfig()
	}
	// 如果有自定义 dangerous_patterns，使用自定义；否则用内置模式
	if len(op.runConfig.DangerousPatterns) > 0 {
		op.initCustomDangerousPatterns()
	} else {
		op.initPatterns()
	}
	return op
}

// ExecuteCmd 执行 Shell 命令。
// 对齐 Python ShellOperation.execute_cmd：参数校验 → 安全检查 → 超时上限 →
// 环境变量准备 → TUI 检测 → 子进程创建 → Shell 进程注册 → 执行 → 注销 → 结果构造。
func (s *LocalShellOperation) ExecuteCmd(ctx context.Context, command string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
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

	// allowlist 检查，对齐 Python _check_allowlist
	if !s.checkAllowlist(command) {
		return createErrResult("command not allowed by allowlist",
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
	isTUI, tuiWarning := s.detectAndMitigateTUI(command, execEnv)
	if isTUI && tuiWarning != "" {
		logger.Warn(shellLogComponent).
			Str("event_type", "SYS_OP_ERROR").
			Str("command", truncate(command, 200)).
			Msg(tuiWarning)
	}

	// Windows 编码检测 + LANG 注入，对齐 Python _detect_shell_encoding + _get_lang_encoding
	if runtime.GOOS == "windows" {
		systemEncoding := detectShellEncoding()
		if systemEncoding != "" && strings.ToLower(systemEncoding) != "utf-8" && strings.ToLower(systemEncoding) != "utf8" {
			langEncoding := getLangEncoding(systemEncoding)
			execEnv["LANG"] = fmt.Sprintf("C.%s", langEncoding)
		}
	}

	// 解析执行计划
	args, _, shellName, planErr := s.resolveExecutionPlan(command, o.ShellType)
	if planErr != nil {
		return nil, planErr
	}

	// 创建子进程
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = envMapToSlice(execEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 从 options 读取 encoding，对齐 Python：(options or {}).get("encoding", self._detect_shell_encoding())
	encoding := defaultShellEncoding
	if o.Options != nil {
		if enc, ok := o.Options["encoding"].(string); ok && enc != "" {
			encoding = enc
		}
	}
	if encoding == "" {
		encoding = detectShellEncoding()
	}

	// 创建进程处理器
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, encoding, timeout)

	// Shell 进程注册，对齐 Python _track_shell_process
	// 注意：Invoke 内部会 Start 进程，Start 后 handler.cmd.Process 才可用
	// 对齐 Python: proc = await _create_subprocess → track → try: invoke → finally: untrack

	// 执行
	invokeData, err := handler.Invoke(ctx)

	// Shell 进程注销，对齐 Python _untrack_shell_process（finally 语义）
	// Invoke 后 handler.cmd.Process 可用，立即 track 并 defer untrack
	if handler.cmd.Process != nil {
		sid := trackShellProcess(ctx, handler.cmd.Process)
		defer untrackShellProcess(sid, handler.cmd.Process)
	}
	if err != nil {
		return createErrResult(fmt.Sprintf("unexpected error: %s", err),
			&result.ExecuteCmdData{Command: command, Cwd: actualCwd}), nil
	}

	if invokeData.Exception != nil {
		return createErrResult(fmt.Sprintf("execution error: %s", invokeData.Exception),
			&result.ExecuteCmdData{
				Command:  command,
				Cwd:      actualCwd,
				ExitCode: &invokeData.ExitCode,
				Stdout:   invokeData.Stdout,
				Stderr:   invokeData.Stderr,
			}), nil
	}

	exitCode := invokeData.ExitCode
	successResult := &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: "Command executed successfully"},
		Data: &result.ExecuteCmdData{
			Command:  command,
			Cwd:      actualCwd,
			ExitCode: &exitCode,
			Stdout:   invokeData.Stdout,
			Stderr:   invokeData.Stderr,
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
// 对齐 Python ShellOperation.execute_cmd_stream：参数校验 → 安全检查 → allowlist →
// 超时上限 → 环境变量准备 → TUI 检测 → Windows 编码 → 子进程创建(stream=True) →
// Shell 进程注册 → stream 循环 → Shell 进程注销 → 结果构造。
func (s *LocalShellOperation) ExecuteCmdStream(ctx context.Context, command string, opts ...sysop.ShellOption) (<-chan result.ExecuteCmdStreamResult, error) {
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

	// allowlist 检查，对齐 Python _check_allowlist
	if !s.checkAllowlist(command) {
		exitCode := -1
		ch <- result.ExecuteCmdStreamResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"command not allowed by allowlist",
			),
			Data: &result.ExecuteCmdChunkData{ChunkIndex: 0, ExitCode: &exitCode},
		}
		close(ch)
		return ch, nil
	}

	// 框架层超时上限，对齐 Python JW_EXECUTE_CMD_MAX_TIMEOUT
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
	isTUI, tuiWarning := s.detectAndMitigateTUI(command, execEnv)
	if isTUI && tuiWarning != "" {
		logger.Warn(shellLogComponent).
			Str("event_type", "SYS_OP_ERROR").
			Str("command", truncate(command, 200)).
			Msg(tuiWarning)
	}

	// Windows 编码检测 + LANG 注入，对齐 Python
	if runtime.GOOS == "windows" {
		systemEncoding := detectShellEncoding()
		if systemEncoding != "" && strings.ToLower(systemEncoding) != "utf-8" && strings.ToLower(systemEncoding) != "utf8" {
			langEncoding := getLangEncoding(systemEncoding)
			execEnv["LANG"] = fmt.Sprintf("C.%s", langEncoding)
		}
	}

	// 解析执行计划（stream 模式：应用 buffering wrapper）
	args, useShell, _, planErr := s.resolveExecutionPlan(command, o.ShellType)
	if planErr != nil {
		return nil, planErr
	}
	if useShell && len(args) > 0 {
		// stream=True 时应用 buffering wrapper，对齐 Python _wrap_command_with_buffering
		// useShell=true 时 args 最后一个元素是命令字符串
		lastIdx := len(args) - 1
		wrapped := wrapCommandWithBuffering(args[lastIdx])
		if wrapped != args[lastIdx] {
			args[lastIdx] = wrapped
		}
	}

	// 创建子进程
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = envMapToSlice(execEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 从 options 读取 chunk_size 和 encoding，对齐 Python
	chunkSize := defaultChunkSize
	encoding := defaultShellEncoding
	if o.Options != nil {
		if cs, ok := o.Options["chunk_size"].(int); ok && cs > 0 {
			chunkSize = cs
		}
		if enc, ok := o.Options["encoding"].(string); ok && enc != "" {
			encoding = enc
		}
	}
	if encoding == "" {
		encoding = detectShellEncoding()
	}

	handler := NewAsyncProcessHandler(cmd, chunkSize, encoding, timeout)

	streamCh, err := handler.Stream(ctx)
	if err != nil {
		close(ch)
		return ch, err
	}

	// Shell 进程注册，对齐 Python _track_shell_process
	var trackSid string
	var trackProc *os.Process
	if handler.cmd.Process != nil {
		trackSid = trackShellProcess(ctx, handler.cmd.Process)
		trackProc = handler.cmd.Process
	}

	go func() {
		defer close(ch)
		// Shell 进程注销，对齐 Python _untrack_shell_process（finally 语义）
		defer untrackShellProcess(trackSid, trackProc)

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
						ExitCode:   &event.ExitCode,
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
// 对齐 Python ShellOperation.execute_cmd_background：参数校验 → 安全检查 →
// allowlist → 环境变量准备 → 子进程创建(background=True) → Shell 进程注册 →
// grace 检测 → Shell 进程注销（失败时） → 结果构造。
func (s *LocalShellOperation) ExecuteCmdBackground(ctx context.Context, command string, opts ...sysop.ShellOption) (*result.ExecuteCmdBackgroundResult, error) {
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

	// allowlist 检查，对齐 Python _check_allowlist
	if !s.checkAllowlist(command) {
		return &result.ExecuteCmdBackgroundResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				"command not allowed by allowlist",
			),
			Data: &result.ExecuteCmdBackgroundData{Command: command, Cwd: actualCwd},
		}, nil
	}

	// 环境变量准备
	opUtils := OperationUtils{}
	execEnv := opUtils.PrepareEnvironment(o.Environment)

	// 创建后台子进程
	args, _, _, planErr := s.resolveExecutionPlan(command, o.ShellType)
	if planErr != nil {
		return nil, planErr
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = envMapToSlice(execEnv)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, defaultShellEncoding, 0)

	pid, err := handler.Background(o.Grace)
	if err != nil {
		// Shell 进程注销（失败时），对齐 Python _untrack_shell_process（finally 语义）
		if handler.cmd.Process != nil {
			sid := trackShellProcess(ctx, handler.cmd.Process)
			defer untrackShellProcess(sid, handler.cmd.Process)
		}
		return &result.ExecuteCmdBackgroundResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationShellExecutionError.Code(),
				fmt.Sprintf("background command failed: %s", err),
			),
			Data: &result.ExecuteCmdBackgroundData{Command: command, Cwd: actualCwd},
		}, nil
	}

	// Shell 进程注册，对齐 Python _track_shell_process
	trackSid := ""
	if handler.cmd.Process != nil {
		trackSid = trackShellProcess(ctx, handler.cmd.Process)
	}

	logger.Info(shellLogComponent).
		Str("event_type", "SYS_OP_END").
		Str("method_name", methodName).
		Int("pid", pid).
		Str("session_id", trackSid).
		Msg("后台命令启动成功")

	return &result.ExecuteCmdBackgroundResult{
		BaseResult: result.BaseResult{Code: 0, Message: "Background command started successfully"},
		Data: &result.ExecuteCmdBackgroundData{
			Command: command,
			Cwd:     actualCwd,
			Pid:     &pid,
		},
	}, nil
}

// ListTools 返回 Shell 操作的工具卡片列表（硬编码）。
// description 严格使用 Python 方法英文 docstring 原文，不翻译。
// 对齐 Python BaseShellOperation.list_tools：execute_cmd, execute_cmd_stream, execute_cmd_background。
func (s *LocalShellOperation) ListTools() []*tool.ToolCard {
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
	written, firstErr := sysop.DefaultRegistry.WriteStdinForSession(sessionID, []byte(data))
	if firstErr != nil {
		logger.Warn(shellLogComponent).
			Str("session_id", sessionID).
			Err(firstErr).
			Msg("写入 stdin 部分失败")
	}

	if written == 0 {
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

// KillProcess 终止指定后台进程。
// 对齐 Python ShellOperation.kill_process：通过 ShellProcessRegistry 查找并终止进程。
func (s *LocalShellOperation) KillProcess(ctx context.Context, sessionID string, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
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
func (s *LocalShellOperation) ListProcesses(ctx context.Context, opts ...sysop.ShellOption) (*result.ExecuteCmdResult, error) {
	infos := sysop.DefaultRegistry.ListProcesses()
	return &result.ExecuteCmdResult{
		BaseResult: result.BaseResult{Code: 0, Message: fmt.Sprintf("ListProcesses succeeded, count=%d", len(infos))},
		Data: &result.ExecuteCmdData{
			ExitCode: intPtr(0),
		},
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initPatterns 初始化危险模式和 TUI 模式。
// 对齐 Python ShellOperation._DANGEROUS_PATTERNS 和 _TUI_COMMAND_PATTERNS。
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

	s.tuiCommandPatterns = []TUICommandPattern{
		{regexp.MustCompile(`\b(npx\s+)?playwright\s+test\b`), "Playwright test runner may require TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\b(npm|npx|yarn|pnpm)\s+(run\s+)?test\b`), "Test runner (npm/pnpm/yarn) may require TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\bvitest\b.*(--watch|--ui)`), "Vitest watch/UI mode requires TTY", map[string]string{"CI": "true"}},
		{regexp.MustCompile(`\b(top|htop|vim|vi|nano|less|more)\b`), "Interactive TUI program will hang without TTY", nil},
	}
}

// checkCommandSafety 检查命令安全性。返回匹配的标签，空字符串表示安全。
// 对齐 Python ShellOperation._check_command_safety。
// 对齐 Python：自定义 dangerous_patterns 优先于内置模式。
// 对齐 Python：(?!-tui) 负向前瞻：pkill/killall jiuwenswarm 排除 -tui 变体。
func (s *LocalShellOperation) checkCommandSafety(command string) string {
	for _, dp := range s.dangerousPatterns {
		if dp.Pattern.MatchString(command) {
			// 对齐 Python (?!-tui)：pkill/killall jiuwenswarm 允许 -tui 后缀通过
			if dp.Label == "pkill targeting jiuwenswarm backend" || dp.Label == "killall targeting jiuwenswarm backend" {
				loc := dp.Pattern.FindStringIndex(command)
				if loc != nil && loc[1] <= len(command) && strings.HasPrefix(command[loc[1]:], "-tui") {
					continue // 放行 jiuwenswarm-tui
				}
			}
			return dp.Label
		}
	}
	return ""
}

// checkAllowlist 检查命令是否在白名单中。
// 对齐 Python ShellOperation._check_allowlist。
func (s *LocalShellOperation) checkAllowlist(command string) bool {
	if s.runConfig == nil || len(s.runConfig.ShellAllowlist) == 0 {
		return true // 没配置 allowlist → 放行所有
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

// initCustomDangerousPatterns 用自定义 dangerous_patterns 初始化。
// 对齐 Python：custom_patterns = getattr(self._run_config, 'dangerous_patterns', None)。
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

// detectAndMitigateTUI 检测 TUI/PTY 依赖命令并注入缓解环境变量。
// 对齐 Python ShellOperation._detect_and_mitigate_tui。
func (s *LocalShellOperation) detectAndMitigateTUI(command string, execEnv map[string]string) (bool, string) {
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
func (s *LocalShellOperation) resolveExecutionPlan(command string, shellType sysop.ShellType) (args []string, useShell bool, shellName string, err error) {
	isWindows := runtime.GOOS == "windows"

	switch shellType {
	case sysop.ShellTypePowerShell:
		// unwrap 仅在 PowerShell 模式生效，对齐 Python
		if unwrapped := UnwrapPowerShellCommand(command); unwrapped != "" {
			command = unwrapped
		}
		pwshPath := AvailablePowerShell()
		return []string{pwshPath, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell", nil

	case sysop.ShellTypeBash:
		bashPath := AvailableBash(true)
		if bashPath == "" {
			bashPath = "/bin/bash"
		}
		if isWindows {
			return []string{bashPath, "-lc", NormalizeWindowsPathsForBash(command)}, false, "bash", nil
		}
		return []string{bashPath, "-lc", command}, false, "bash", nil

	case sysop.ShellTypeCmd:
		if !isWindows {
			// 对齐 Python：cmd 仅 Windows 支持，返回 error 而非字符串防止 panic
			return nil, false, "", exception.BuildError(exception.StatusSysOperationShellExecutionError,
				exception.WithMsg("shell_type 'cmd' is only supported on Windows"))
		}
		return []string{"cmd", "/c", command}, true, "cmd", nil

	case sysop.ShellTypeSh:
		shPath := AvailableSh()
		if shPath == "" {
			shPath = "sh"
		}
		if isWindows {
			return []string{shPath, "-c", NormalizeWindowsPathsForBash(command)}, false, "sh", nil
		}
		return []string{shPath, "-c", command}, true, "sh", nil

	case sysop.ShellTypeAuto:
		fallthrough
	default:
		// 自动检测逻辑，严格对齐 Python _resolve_execution_plan 的 auto 分支
		if isWindows {
			// Windows AUTO：unwrap → PowerShell → POSIX → cmd
			if unwrapped := UnwrapPowerShellCommand(command); unwrapped != "" {
				exe := AvailablePowerShell()
				return []string{exe, "-NoProfile", "-NonInteractive", "-Command", unwrapped}, false, "powershell", nil
			}
			if LooksLikePowerShell(command) {
				exe := AvailablePowerShell()
				return []string{exe, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell", nil
			}
			if LooksLikePosix(command) {
				exe := AvailableBash(false) // allow_wsl=False，对齐 Python
				if exe != "" {
					return []string{exe, "-lc", NormalizeWindowsPathsForBash(command)}, false, "bash", nil
				}
			}
			return []string{"cmd", "/c", command}, true, "cmd", nil
		}

		// 非 Windows AUTO：对齐 Python 用 sh（create_subprocess_shell）
		return []string{"sh", "-c", command}, true, "sh", nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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

// intPtr 返回 int 的指针
func intPtr(v int) *int { return &v }

// detectShellEncoding 检测 Shell 输出编码。
// 对齐 Python ShellOperation._detect_shell_encoding：locale.getpreferredencoding(False)。
func detectShellEncoding() string {
	// Go 标准库没有 locale.getpreferredencoding 等价函数
	// 在 Linux/macOS 上读取环境变量 LC_ALL > LC_CTYPE > LANG
	for _, envKey := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if val := os.Getenv(envKey); val != "" {
			// 提取编码部分，如 en_US.UTF-8 → UTF-8
			if idx := strings.LastIndex(val, "."); idx >= 0 {
				enc := val[idx+1:]
				if enc != "" {
					return enc
				}
			}
		}
	}
	return defaultShellEncoding
}

// getLangEncoding 将编码名转换为 LANG 风格编码名。
// 对齐 Python ShellOperation._get_lang_encoding：codecs.lookup(encoding).name.upper()。
func getLangEncoding(encoding string) string {
	// 简化实现：常见编码映射
	encMap := map[string]string{
		"cp936":   "GBK",
		"gbk":     "GBK",
		"gb2312":  "GBK",
		"gb18030": "GB18030",
		"utf-8":   "UTF-8",
		"utf8":    "UTF-8",
		"latin-1": "ISO-8859-1",
		"latin1":  "ISO-8859-1",
	}
	lower := strings.ToLower(encoding)
	if mapped, ok := encMap[lower]; ok {
		return mapped
	}
	return strings.ToUpper(encoding)
}

// wrapCommandWithBuffering 用 OS 特定的缓冲包装器包装命令（仅在 stream 模式下使用）。
// 对齐 Python ShellOperation._BUFFERING_WRAPPERS：
// - Linux: stdbuf -oL -eL /bin/sh -c <quoted_cmd>
// - macOS: script -q /dev/null /bin/sh -c <quoted_cmd>
// - Windows: 不包装
func wrapCommandWithBuffering(command string) string {
	switch runtime.GOOS {
	case "linux":
		quoted := shellQuote(command)
		return fmt.Sprintf("stdbuf -oL -eL /bin/sh -c %s", quoted)
	case "darwin":
		quoted := shellQuote(command)
		return fmt.Sprintf("script -q /dev/null /bin/sh -c %s", quoted)
	default:
		return command
	}
}

// shellQuote 对命令进行 shell 单引号转义。
// 对齐 Python shlex.quote。
func shellQuote(s string) string {
	// 单引号内只需转义单引号本身：替换 ' → '\''
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// init 注册到 GlobalRegistry
func init() {
	_ = sysop.GlobalRegistry.Register(sysop.OperationDef{
		Name:        "shell",
		Mode:        sysop.OperationModeLocal,
		Description: "local shell operation",
		NewFunc:     NewLocalShellOperation,
	})
}
