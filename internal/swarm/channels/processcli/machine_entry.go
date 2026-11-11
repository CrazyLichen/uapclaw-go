package processcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExecuteOptions ExecuteSource 的入参。
// Python: machine_entry.py:217-219 execute_source(source, *, conflicting_arguments, json_lines)
type ExecuteOptions struct {
	// Source 输入来源（"-" 表示 stdin）
	Source string
	// ConflictingArguments 是否与遗留执行参数冲突
	ConflictingArguments bool
	// JSONLines 是否走 JSONL 多行模式
	JSONLines bool
	// Stdin 自定义 stdin（nil 用进程 stdin）
	Stdin *os.File
	// Stdout 自定义输出（nil 用 protocolStdout 隔离出的 writer）
	Stdout *os.File
	// RunFunc 执行体（nil 用 runWithSignalsDefault，对应上游 run_with_signals）
	RunFunc func(*protocol.OneShotRunInput, *OneShotWriter) *protocol.OneShotRunResult
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// exitInvalidInput 输入类失败退出码。
	// Python: machine_entry.py:235 exit_code=2
	exitInvalidInput = 2
	// exitCancelled 被中断退出码。
	// Python: machine_entry.py:246 exit_code=130
	exitCancelled = 130
	// exitStartupFailed 启动/关闭失败退出码。
	// Python: machine_entry.py:254 exit_code=1
	exitStartupFailed = 1
)

// ──────────────────────────── 导出函数 ────────────────────────────

// PrepareWorkspace 在 chdir 之前解析全部路径，让 project_dir 与 cwd 各自保留角色。
// Python: machine_entry.py:87-106 prepare_workspace
func PrepareWorkspace(runInput *protocol.OneShotRunInput) (*protocol.OneShotRunInput, error) {
	if runInput == nil {
		return nil, &MachineInputError{Message: "run input must not be nil"}
	}
	// Python: :90-91 if workspace is None: return run_input
	if runInput.Workspace == nil {
		return runInput, nil
	}
	resolved, err := prepareWorkspaceSpec(runInput.Workspace)
	if err != nil {
		return nil, err
	}
	// Python: :106 return replace(run_input, workspace=resolved)
	copied := *runInput
	copied.Workspace = resolved
	return &copied, nil
}

// prepareWorkspaceSpec 解析 workspace 三段路径并 chdir 到 cwd。
// Python: machine_entry.py:87-106 prepare_workspace（run 与 query 输入共用）
func prepareWorkspaceSpec(workspace *protocol.WorkspaceSpec) (*protocol.WorkspaceSpec, error) {
	resolvedCwd, err := absolutePathOptional(workspace.Cwd)
	if err != nil {
		return nil, err
	}
	resolvedProjectDir, err := absolutePathOptional(workspace.ProjectDir)
	if err != nil {
		return nil, err
	}
	// Python: :100 trusted_dirs=tuple(_absolute_path(path) for path in workspace.trusted_dirs)
	resolvedTrusted := make([]string, 0, len(workspace.TrustedDirs))
	for _, path := range workspace.TrustedDirs {
		absolute, absErr := absolutePath(path)
		if absErr != nil {
			return nil, absErr
		}
		resolvedTrusted = append(resolvedTrusted, absolute)
	}
	// Python: :102-103 if resolved.cwd is not None: os.chdir(resolved.cwd)
	if resolvedCwd != nil {
		if chdirErr := os.Chdir(*resolvedCwd); chdirErr != nil {
			// Python: :104-105 except (OSError, ValueError) → MachineInputError
			return nil, &MachineInputError{Message: "Cannot access the requested workspace."}
		}
	}
	return &protocol.WorkspaceSpec{
		Cwd:         resolvedCwd,
		ProjectDir:  resolvedProjectDir,
		TrustedDirs: resolvedTrusted,
	}, nil
}

// ExecuteSource 只读一份请求然后退出；绝不进入 REPL 或不启动 worker。
// Python: machine_entry.py:217-263 execute_source
//
// 返回进程退出码。
func ExecuteSource(options ExecuteOptions) int {
	// Python: :221 protocol_stdout() 把 fd1 重定向到 stderr，stdout 只剩 JSONL
	restore, data, err := protocolStdout()
	if err != nil {
		return exitStartupFailed
	}
	defer func() { _ = restore() }()

	// 调用方可指定输出目标（测试/嵌入用）
	output := data
	if options.Stdout != nil {
		output = options.Stdout
	}

	// Python: :222 writer = OneShotWriter(output, request_id=str(uuid.uuid4()))
	writer, writerErr := NewOneShotWriter(output, newRequestID())
	if writerErr != nil {
		return exitStartupFailed
	}

	// Python: :221 command_signals() —— 装信号处理器
	signals, restoreSignals := CommandSignalsContext()
	defer restoreSignals()

	var result *protocol.OneShotRunResult
	switch {
	case options.ConflictingArguments:
		// Python: :224-228
		option := "--run-json"
		if options.JSONLines {
			option = "--run-jsonl"
		}
		result = failureFromInputError(writer, &MachineInputError{
			Message: option + " cannot be combined with legacy execution arguments.",
		}, exitInvalidInput)
	default:
		result = executeDocument(options, writer)
	}

	// Python: :256 result = signals.finalize(result)
	result = signals.Finalize(result)

	// Python: :257-258 流已损坏时不再尝试写
	if writer.Broken {
		if result.ExitCode != 0 {
			return result.ExitCode
		}
		return 1
	}
	// Python: :259-262 write_result
	if writeErr := writer.WriteResult(result); writeErr != nil {
		return 1
	}
	// Python: :263 return result.exit_code
	return result.ExitCode
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// executeDocument 读一份文档并跑一次 Runtime。
// Python: machine_entry.py:147-171 _execute_document
func executeDocument(options ExecuteOptions, writer *OneShotWriter) *protocol.OneShotRunResult {
	// Python: :148-159 source == "-" 走 stdin
	source := options.Source
	if source == "-" {
		// ⤵️ 回填 20.4：Python 用 DuplexLineReader(max_line_bytes=MAX_RUN_INPUT_BYTES)
		// 按行读一份文档；Go 侧先直接用 ReadRunInputBytes 读到 EOF，
		// 单文档语义一致（本批不含 JSONL 多行模式）。
		stdin := options.Stdin
		if stdin == nil {
			stdin = os.Stdin
		}
		// Python: :159 run_input = read_run_input("-", stdin=io.BytesIO(document))
		document, err := ReadRunInputBytes(stdin)
		if err != nil {
			return failureFromInputError(writer, asInputError(err), exitInvalidInput)
		}
		input, inputErr := readRunInputFromString(document)
		if inputErr != nil {
			return failureFromInputError(writer, asInputError(inputErr), exitInvalidInput)
		}
		return runPreparedInput(input, options, writer)
	}
	// Python: :160-161 run_input = read_run_input(source)
	input, err := ReadRunInput(source, nil)
	if err != nil {
		return failureFromInputError(writer, asInputError(err), exitInvalidInput)
	}
	return runPreparedInput(input, options, writer)
}

// runPreparedInput 解析 workspace 后跑一次 Runtime。
// Python: machine_entry.py:162-171
func runPreparedInput(
	input *protocol.OneShotRunInput, options ExecuteOptions, writer *OneShotWriter,
) *protocol.OneShotRunResult {
	// Python: :162 writer.request_id = run_input.request_id or writer.request_id
	if input.RequestID != nil && strings.TrimSpace(*input.RequestID) != "" {
		writer.RequestID = strings.TrimSpace(*input.RequestID)
	}
	// Python: :163 run_input = prepare_workspace(run_input)
	prepared, err := PrepareWorkspace(input)
	if err != nil {
		return failureFromInputError(writer, asInputError(err), exitInvalidInput)
	}
	// Python: :169-171 run_with_signals(run_input, writer, control=DuplexController(None, writer, unattended=True))
	//
	// ⤵️ 回填 20.4：DuplexController 未实现（用户决策：control 恒 nil，只走非交互分支）。
	// unattended=True 在语义上就是「没有 reader、不回答交互」——与 nil 等价。
	runFunc := options.RunFunc
	if runFunc == nil {
		runFunc = runWithSignalsDefault
	}
	return runFunc(prepared, writer)
}

// failure 构造失败结果。
// Python: machine_entry.py:109-119 _failure
func failure(writer *OneShotWriter, code, message string, exitCode int) *protocol.OneShotRunResult {
	// Python: :116 status = CANCELLED if exit_code == 130 else FAILED
	status := string(protocol.StatusFailed)
	if exitCode == exitCancelled {
		status = string(protocol.StatusCancelled)
	}
	return &protocol.OneShotRunResult{
		SchemaVersion: protocol.CurrentSchemaVersion,
		Sequence:      writer.Sequence,
		RequestID:     writer.RequestID,
		SessionID:     writer.SessionID,
		Status:        status,
		ExitCode:      exitCode,
		Error:         &protocol.RuntimeErrorInfo{Code: code, Message: message},
	}
}

// failureFromInputError 把 MachineInputError 转成失败结果（带 request_id 与 details）。
// Python: machine_entry.py:233-243
func failureFromInputError(
	writer *OneShotWriter, inputErr *MachineInputError, exitCode int,
) *protocol.OneShotRunResult {
	// Python: :234 writer.request_id = error.request_id or writer.request_id
	if inputErr != nil && inputErr.RequestID != nil && strings.TrimSpace(*inputErr.RequestID) != "" {
		writer.RequestID = strings.TrimSpace(*inputErr.RequestID)
	}
	code := "INVALID_INPUT"
	message := "run input is invalid"
	if inputErr != nil {
		code = inputErr.Code()
		message = inputErr.Message
	}
	result := failure(writer, code, message, exitCode)
	// Python: :236-243 result = replace(result, error=RuntimeErrorInfo(code, message, details))
	if inputErr != nil && inputErr.Details != nil {
		result.Error.Details = inputErr.Details
	}
	return result
}

// asInputError 把任意 error 归一为 MachineInputError。
func asInputError(err error) *MachineInputError {
	var inputErr *MachineInputError
	if errors.As(err, &inputErr) {
		return inputErr
	}
	if err == nil {
		return &MachineInputError{Message: "run input is invalid"}
	}
	return &MachineInputError{Message: err.Error()}
}

// readRunInputFromString 从内存字符串读一份运行输入。
// Python: machine_entry.py:159 read_run_input("-", stdin=io.BytesIO(document))
func readRunInputFromString(document string) (*protocol.OneShotRunInput, error) {
	return ReadRunInput("-", strings.NewReader(document))
}

// absolutePath 解析为绝对路径（展开 ~ 并 clean）。
// Python: machine_entry.py:83-84 str(Path(value).expanduser().resolve())
func absolutePath(value string) (string, error) {
	expanded := pathutil.ExpandHome(value)
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", &MachineInputError{Message: "Cannot access the requested workspace."}
	}
	return absolute, nil
}

// absolutePathOptional 可空版本。
func absolutePathOptional(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	absolute, err := absolutePath(*value)
	if err != nil {
		return nil, err
	}
	return &absolute, nil
}

// newRequestID 生成一个请求标识。
// Python: machine_entry.py:222 request_id=str(uuid.uuid4())
func newRequestID() string {
	return uuid.New().String()
}

// runWithSignalsDefault 默认执行体：包一层信号归属后调用 RunMachine。
// Python: machine_entry.py:169 run_with_signals(run_input, writer, control=...)
//
// ⤵️ 回填 20.4：control 恒 nil（用户决策），只走非交互分支。
func runWithSignalsDefault(
	runInput *protocol.OneShotRunInput, writer *OneShotWriter,
) *protocol.OneShotRunResult {
	return RunWithSignals(context.Background(), runInput, writer, nil)
}
