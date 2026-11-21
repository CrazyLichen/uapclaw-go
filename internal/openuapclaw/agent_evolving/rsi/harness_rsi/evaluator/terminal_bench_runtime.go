// terminal_bench_runtime.go Terminal-Bench 容器运行时助手（单 harness 执行）。
// Python: openjiuwen/rsi/harness_rsi/evaluator/terminal_bench_runtime.py（533 行全量对齐）。
//
// 对齐说明：
// - Python subprocess.run 的测试注入点（monkeypatch subprocess.run）在 Go 等价为
//   包级函数变量 terminalBenchRun（run_docker 与宿主 git apply、容器命令执行共用，
//   对齐 Python 全部经 subprocess 模块同一注入点的语义）；非架构缝，不登记偏差。
// - Python subprocess.run(timeout=N) → Go context.WithTimeout；超时以 errTBDeadline
//   哨兵承载（Python TimeoutExpired 带部分输出 → tbRunResult 携带部分流）。
// - StatusCode.SUCCESS（code=0, errmsg）→ Go BaseResult{Code:0, Message:"Command
//   executed successfully"}（对齐 local/shell_operation.go 既有约定）。

package evaluator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
	sysopresult "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TerminalBenchCommandLogEntry 评估轨迹中有界的 shell 命令记录。
// Python: terminal_bench_runtime.py:33 TerminalBenchCommandLogEntry。
type TerminalBenchCommandLogEntry struct {
	// Command 命令原文
	Command string
	// Cwd 容器内工作目录
	Cwd string
	// ExitCode 退出码（nil=超时/未退出，对齐 Python int|None）
	ExitCode *int
	// StdoutExcerpt 标准输出摘录
	StdoutExcerpt string
	// StderrExcerpt 标准错误摘录
	StderrExcerpt string
	// TimeoutSec 超时秒数（nil=未设，对齐 Python int|None）
	TimeoutSec *int
	// Background 是否后台执行
	Background bool
}

// TerminalBenchCommandRecorder 单次用例运行的有界命令录音机。
// Python: terminal_bench_runtime.py:46 TerminalBenchCommandRecorder。
type TerminalBenchCommandRecorder struct {
	// MaxEntries 最大记录条数（Python 默认 80）
	MaxEntries int
	// MaxStreamChars 单流最大字符数（Python 默认 2000）
	MaxStreamChars int
	// Entries 命令记录
	Entries []TerminalBenchCommandLogEntry
}

// ShellOpParams TerminalBenchDockerShellOperation 构造参数
// （Python __init__ 关键字参数）。
type ShellOpParams struct {
	// ContainerName 容器名
	ContainerName string
	// HostWorkspaceDir 宿主工作区目录（resolve 后）
	HostWorkspaceDir string
	// ContainerWorkspaceDir 容器内工作区目录（默认 /app）
	ContainerWorkspaceDir string
	// Recorder 命令录音机（可 nil）
	Recorder *TerminalBenchCommandRecorder
	// CleanShell 是否 --noprofile --norc 干净 shell
	CleanShell bool
	// RuntimeEnvironment 注入容器的运行时环境变量
	RuntimeEnvironment map[string]string
	// EnforceInContainerTime 是否容器内强制 timeout（Python enforce_in_container_timeout）
	EnforceInContainerTime bool
}

// TerminalBenchDockerShellOperation 在存活 Terminal-Bench 容器内执行 shell 命令。
// Python: terminal_bench_runtime.py:93 TerminalBenchDockerShellOperation(BaseShellOperation)。
type TerminalBenchDockerShellOperation struct {
	// sysop.BaseShellOperation 基类（name/mode/description/runConfig 载体）
	sysop.BaseShellOperation
	containerName         string
	hostWorkspaceDir      string
	containerWorkspaceDir string
	recorder              *TerminalBenchCommandRecorder
	cleanShell            bool
	runtimeEnvironment    map[string]string
	enforceInContainer    bool
}

// dockerExecParams dockerExecCommand 组装参数（Python 私有方法关键字参数）。
type dockerExecParams struct {
	// Cwd 容器内工作目录
	Cwd string
	// Environment 调用方环境变量（与运行时环境合并）
	Environment map[string]string
	// Detached 是否后台分离执行（docker exec -d）
	Detached bool
	// Timeout 超时秒数指针（nil=无超时）
	Timeout *int
}

// TerminalBenchDockerSysOperation 本地文件/代码操作 + Docker 转发 shell 操作。
// Python: terminal_bench_runtime.py:358 TerminalBenchDockerSysOperation(SysOperation)。
type TerminalBenchDockerSysOperation struct {
	// sysop.BaseSysOperation 基类（Fs/Code 等价 Python SysOperation 基类缺省）
	sysop.BaseSysOperation
	card          *sysop.SysOperationCard
	dockerShellOp *TerminalBenchDockerShellOperation
}

// SysOpBuildParams BuildTerminalBenchSysOperation 构造参数
// （Python build_terminal_bench_sys_operation 关键字参数）。
type SysOpBuildParams struct {
	// SysOperationID 系统操作卡 ID
	SysOperationID string
	// ContainerName 容器名
	ContainerName string
	// WorkspaceDir 宿主工作区目录
	WorkspaceDir string
	// Recorder 命令录音机（可 nil）
	Recorder *TerminalBenchCommandRecorder
	// ContainerWorkspaceDir 容器内工作区目录（默认 /app）
	ContainerWorkspaceDir string
	// CleanShell 是否干净 shell
	CleanShell bool
	// RuntimeEnvironment 运行时环境变量
	RuntimeEnvironment map[string]string
	// EnforceInContainerTime 是否容器内强制 timeout
	EnforceInContainerTime bool
}

// StartSolverContainerParams StartTerminalBenchSolverContainer 参数
// （Python start_terminal_bench_solver_container 关键字参数）。
type StartSolverContainerParams struct {
	// DockerImage 镜像名
	DockerImage string
	// CaseID 用例 ID
	CaseID string
	// WorkspaceDir 宿主工作区目录
	WorkspaceDir string
	// TimeoutSec create/start 超时秒数
	TimeoutSec int
	// ContainerWorkspaceDir 容器内工作区目录
	ContainerWorkspaceDir string
	// MountWorkspace 是否 bind mount 工作区
	MountWorkspace bool
}

// SyncPatchParams SyncContainerGitPatchToWorkspace 参数
// （Python sync_container_git_patch_to_workspace 关键字参数）。
type SyncPatchParams struct {
	// ContainerName 容器名
	ContainerName string
	// WorkspaceDir 宿主原始检出目录
	WorkspaceDir string
	// ContainerWorkspaceDir 容器内工作区目录
	ContainerWorkspaceDir string
}

// tbRunResult Python subprocess.CompletedProcess[str] 的 Go 等价。
type tbRunResult struct {
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
	// ExitCode 退出码
	ExitCode int
}

// ──────────────────────────── 常量 ────────────────────────────

// errTBDeadline 宿主侧命令超时哨兵（Python subprocess.TimeoutExpired 等价）。
var errTBDeadline = errors.New("terminal-bench command deadline exceeded")

// ──────────────────────────── 全局变量 ────────────────────────────

// terminalBenchRun Python subprocess.run 的 Go 注入点（run_docker / git apply /
// 容器命令执行共用同一变量；测试替换，对齐 Python monkeypatch subprocess.run）。
var terminalBenchRun = func(name string, args []string, timeoutSec float64, input []byte) (tbRunResult, error) {
	return tbRunProcess(name, args, timeoutSec, input)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTerminalBenchCommandRecorder 构造录音机（Python dataclass 默认值
// max_entries=80 / max_stream_chars=2000）。
func NewTerminalBenchCommandRecorder() *TerminalBenchCommandRecorder {
	return &TerminalBenchCommandRecorder{MaxEntries: 80, MaxStreamChars: 2000}
}

// Record 记录一条命令；超出 max_entries 时丢弃（Python L64-65 提前返回）。
func (r *TerminalBenchCommandRecorder) Record(command, cwd string, exitCode *int, stdout, stderr string, timeoutSec *int, background bool) {
	if len(r.Entries) >= r.MaxEntries {
		return
	}
	r.Entries = append(r.Entries, TerminalBenchCommandLogEntry{
		Command:       command,
		Cwd:           cwd,
		ExitCode:      exitCode,
		StdoutExcerpt: tbExcerpt(stdout, r.MaxStreamChars),
		StderrExcerpt: tbExcerpt(stderr, r.MaxStreamChars),
		TimeoutSec:    timeoutSec,
		Background:    background,
	})
}

// ToList 导出记录列表（Python to_list 的 dict 键逐字对齐）。
func (r *TerminalBenchCommandRecorder) ToList() []map[string]any {
	list := make([]map[string]any, 0, len(r.Entries))
	for _, entry := range r.Entries {
		list = append(list, map[string]any{
			"command":        entry.Command,
			"cwd":            entry.Cwd,
			"exit_code":      entry.ExitCode,
			"stdout_excerpt": entry.StdoutExcerpt,
			"stderr_excerpt": entry.StderrExcerpt,
			"timeout_sec":    entry.TimeoutSec,
			"background":     entry.Background,
		})
	}
	return list
}

// NewTerminalBenchDockerShellOperation 构造容器 shell 操作
// （Python __init__；name/mode/description/run_config 经基座对齐 L107-112）。
func NewTerminalBenchDockerShellOperation(params ShellOpParams) *TerminalBenchDockerShellOperation {
	base := sysop.NewBaseOperation("terminal_bench_docker_shell", sysop.OperationModeLocal,
		"Run shell commands in the Terminal-Bench task container.",
		&sysop.LocalWorkConfig{RestrictToSandbox: false})
	op := &TerminalBenchDockerShellOperation{
		BaseShellOperation:    sysop.BaseShellOperation{BaseOperation: base},
		containerName:         params.ContainerName,
		containerWorkspaceDir: strings.TrimSuffix(params.ContainerWorkspaceDir, "/"),
		recorder:              params.Recorder,
		cleanShell:            params.CleanShell,
		runtimeEnvironment:    map[string]string{},
		enforceInContainer:    params.EnforceInContainerTime,
	}
	if op.containerWorkspaceDir == "" {
		op.containerWorkspaceDir = "/" // Python rstrip("/") or "/"
	}
	if params.HostWorkspaceDir != "" {
		if resolved, err := filepath.Abs(params.HostWorkspaceDir); err == nil {
			op.hostWorkspaceDir = resolved // Python resolve()
		} else {
			op.hostWorkspaceDir = params.HostWorkspaceDir
		}
	}
	for key, value := range params.RuntimeEnvironment {
		op.runtimeEnvironment[key] = value
	}
	return op
}

// CommandLog 返回命令记录（Python command_log property：recorder 为 nil 时空表）。
func (o *TerminalBenchDockerShellOperation) CommandLog() []map[string]any {
	if o.recorder == nil {
		return []map[string]any{}
	}
	return o.recorder.ToList()
}

// ExecuteCmd 容器内执行命令（Python execute_cmd L124-199 步骤序逐条对齐）。
func (o *TerminalBenchDockerShellOperation) ExecuteCmd(ctx context.Context, command string, opts ...sysop.ShellOption) (*sysopresult.ExecuteCmdResult, error) {
	options := sysop.NewShellOptions(opts...)
	containerCwd := o.containerCwd(options.Cwd)
	timeout := options.Timeout
	var timeoutPtr *int
	if timeout > 0 {
		timeoutPtr = &timeout
	}
	dockerCommand := o.dockerExecCommand(command, dockerExecParams{
		Cwd: containerCwd, Environment: options.Environment, Detached: false, Timeout: timeoutPtr,
	})
	hostTimeout := float64(timeout)
	if timeout > 0 && o.enforceInContainer {
		// 容器内 timeout 先收割命令树，宿主 docker client 再多等 10 秒（Python L143-146）。
		hostTimeout = float64(timeout + 10)
	}
	completed, runErr := terminalBenchRun(dockerCommand[0], dockerCommand[1:], hostTimeout, nil)
	if errors.Is(runErr, errTBDeadline) {
		// Python except subprocess.TimeoutExpired（L177-199）：exit_code=None +
		// 超时说明写进 stderr 尾部。
		stdout := completed.Stdout
		stderr := completed.Stderr + fmt.Sprintf("\ncommand timed out after %ds", timeout)
		if o.recorder != nil {
			o.recorder.Record(command, containerCwd, nil, stdout, stderr, timeoutPtr, false)
		}
		return o.cmdResult(command, containerCwd, nil, stdout, stderr), nil
	}
	if runErr != nil {
		return nil, runErr
	}
	if o.recorder != nil {
		code := completed.ExitCode
		o.recorder.Record(command, containerCwd, &code, completed.Stdout, completed.Stderr, timeoutPtr, false)
	}
	return o.cmdResult(command, containerCwd, &completed.ExitCode, completed.Stdout, completed.Stderr), nil
}

// ExecuteCmdStream 流式执行（Python execute_cmd_stream L201-254：整段 stdout 块 →
// 整段 stderr 块 → 汇总终块，chunk_index 递增）。
func (o *TerminalBenchDockerShellOperation) ExecuteCmdStream(ctx context.Context, command string, opts ...sysop.ShellOption) (<-chan sysopresult.ExecuteCmdStreamResult, error) {
	result, err := o.ExecuteCmd(ctx, command, opts...)
	if err != nil {
		return nil, err
	}
	ch := make(chan sysopresult.ExecuteCmdStreamResult, 3)
	data := result.Data
	index := 0
	if data != nil && data.Stdout != "" {
		ch <- sysopresult.ExecuteCmdStreamResult{
			BaseResult: sysopresult.BaseResult{Code: 0, Message: "stream output"},
			Data: &sysopresult.ExecuteCmdChunkData{
				Text: data.Stdout, Type: strPtr("stdout"), ChunkIndex: index,
			},
		}
		index++
	}
	if data != nil && data.Stderr != "" {
		ch <- sysopresult.ExecuteCmdStreamResult{
			BaseResult: sysopresult.BaseResult{Code: 0, Message: "stream output"},
			Data: &sysopresult.ExecuteCmdChunkData{
				Text: data.Stderr, Type: strPtr("stderr"), ChunkIndex: index,
			},
		}
		index++
	}
	var exitCode *int
	if data != nil {
		exitCode = data.ExitCode
	}
	ch <- sysopresult.ExecuteCmdStreamResult{
		BaseResult: sysopresult.BaseResult{Code: 0, Message: "stream output"},
		Data:       &sysopresult.ExecuteCmdChunkData{Text: "", ChunkIndex: index, ExitCode: exitCode},
	}
	close(ch)
	return ch, nil
}

// ExecuteCmdBackground 后台执行（Python execute_cmd_background L256-306：detached
// 组装 + grace 早期失败检测窗口 + returncode≠0 仍返回成功且 pid=None）。
func (o *TerminalBenchDockerShellOperation) ExecuteCmdBackground(ctx context.Context, command string, opts ...sysop.ShellOption) (*sysopresult.ExecuteCmdBackgroundResult, error) {
	options := sysop.NewShellOptions(opts...)
	grace := options.Grace
	if grace <= 0 {
		grace = 3.0 // Python 默认 grace=3.0
	}
	containerCwd := o.containerCwd(options.Cwd)
	dockerCommand := o.dockerExecCommand(command, dockerExecParams{
		Cwd: containerCwd, Environment: options.Environment, Detached: true,
	})
	completed, runErr := terminalBenchRun(dockerCommand[0], dockerCommand[1:], max64(grace, 1.0), nil)
	if runErr != nil {
		return nil, runErr
	}
	timeoutSec := int(max64(grace, 1.0))
	if o.recorder != nil {
		code := completed.ExitCode
		o.recorder.Record(command, containerCwd, &code, completed.Stdout, completed.Stderr, &timeoutSec, true)
	}
	backgroundData := &sysopresult.ExecuteCmdBackgroundData{Command: command, Cwd: containerCwd}
	return &sysopresult.ExecuteCmdBackgroundResult{
		BaseResult: sysopresult.BaseResult{Code: 0, Message: "Command executed successfully"},
		Data:       backgroundData,
	}, nil
}

// WriteStdin 容器 shell 不支持后台会话输入（Python BaseShellOperation 缺省未实现）。
func (o *TerminalBenchDockerShellOperation) WriteStdin(ctx context.Context, sessionID string, data string, opts ...sysop.ShellOption) (*sysopresult.ExecuteCmdResult, error) {
	return o.BaseShellOperation.WriteStdin(ctx, sessionID, data, opts...)
}

// KillProcess 容器 shell 不支持进程终止（Python 缺省未实现）。
func (o *TerminalBenchDockerShellOperation) KillProcess(ctx context.Context, sessionID string, opts ...sysop.ShellOption) (*sysopresult.ExecuteCmdResult, error) {
	return o.BaseShellOperation.KillProcess(ctx, sessionID, opts...)
}

// ListProcesses 容器 shell 不支持进程列举（Python 缺省未实现）。
func (o *TerminalBenchDockerShellOperation) ListProcesses(ctx context.Context, opts ...sysop.ShellOption) (*sysopresult.ExecuteCmdResult, error) {
	return o.BaseShellOperation.ListProcesses(ctx, opts...)
}

// BuildTerminalBenchSysOperation 组装 Terminal-Bench 容器系统操作
// （Python build_terminal_bench_sys_operation L381-403）。
func BuildTerminalBenchSysOperation(params SysOpBuildParams) *TerminalBenchDockerSysOperation {
	containerWorkspaceDir := params.ContainerWorkspaceDir
	if containerWorkspaceDir == "" {
		containerWorkspaceDir = "/app" // Python 默认值
	}
	card := sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
	)
	card.ID = params.SysOperationID // Python SysOperationCard(id=sys_operation_id)
	return &TerminalBenchDockerSysOperation{
		card: card,
		dockerShellOp: NewTerminalBenchDockerShellOperation(ShellOpParams{
			ContainerName:          params.ContainerName,
			HostWorkspaceDir:       params.WorkspaceDir,
			ContainerWorkspaceDir:  containerWorkspaceDir,
			Recorder:               params.Recorder,
			CleanShell:             params.CleanShell,
			RuntimeEnvironment:     params.RuntimeEnvironment,
			EnforceInContainerTime: params.EnforceInContainerTime,
		}),
	}
}

// Card 返回系统操作卡（Python SysOperation.card property）。
func (s *TerminalBenchDockerSysOperation) Card() *sysop.SysOperationCard { return s.card }

// Shell 返回 Docker shell 操作（Python shell() L374-375）。
func (s *TerminalBenchDockerSysOperation) Shell() sysop.ShellOperation { return s.dockerShellOp }

// CommandLog 转发 shell 操作的命令记录（Python command_log L377-378）。
func (s *TerminalBenchDockerSysOperation) CommandLog() []map[string]any {
	return s.dockerShellOp.CommandLog()
}

// StartTerminalBenchSolverContainer 启动 solver 容器（Python L406-435 调用序：
// rm -f（check=False）→ create（--workdir/可选 --mount）→ start，均 timeout_sec）。
func StartTerminalBenchSolverContainer(params StartSolverContainerParams) (string, error) {
	containerName := TerminalBenchSolverContainerName(params.CaseID)
	if _, err := runDocker([]string{"docker", "rm", "-f", containerName}, false, 0); err != nil {
		return "", err
	}
	command := []string{"docker", "create", "--name", containerName,
		"--workdir", params.ContainerWorkspaceDir}
	if params.MountWorkspace {
		command = append(command, "--mount",
			fmt.Sprintf("type=bind,source=%s,target=%s", absPath(params.WorkspaceDir), params.ContainerWorkspaceDir))
	}
	command = append(command, params.DockerImage, "sleep", "infinity")
	if _, err := runDocker(command, true, params.TimeoutSec); err != nil {
		return "", err
	}
	if _, err := runDocker([]string{"docker", "start", containerName}, true, params.TimeoutSec); err != nil {
		return "", err
	}
	return containerName, nil
}

// SyncContainerGitPatchToWorkspace 把容器内 tracked git patch 应用回宿主原始检出
// （Python L438-487 步骤序：容器 git diff --binary HEAD → 空 diff 返回 "" →
// 宿主 git apply --binary --whitespace=nowarn -）。
func SyncContainerGitPatchToWorkspace(params SyncPatchParams) (string, error) {
	result, err := runDocker([]string{
		"docker", "exec", "-w", params.ContainerWorkspaceDir, params.ContainerName,
		"git", "diff", "--binary", "HEAD",
	}, true, 120)
	if err != nil {
		return "", err
	}
	diff := result.Stdout
	if diff == "" {
		return "", nil
	}
	gitExecutable, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git executable is required to synchronize the task patch")
	}
	completed, err := terminalBenchRun(gitExecutable, []string{
		"-C", absPath(params.WorkspaceDir), "apply", "--binary", "--whitespace=nowarn", "-",
	}, 120, []byte(diff))
	if err != nil {
		return "", err
	}
	if completed.ExitCode != 0 {
		return "", fmt.Errorf(
			"failed to sync solver patch from container: stdout=%s\nstderr=%s",
			tbTimeoutStream(completed.Stdout), tbTimeoutStream(completed.Stderr))
	}
	return diff, nil
}

// RemoveTerminalBenchContainer 强删容器（Python L490-491，check=False）。
func RemoveTerminalBenchContainer(containerName string) {
	_, _ = runDocker([]string{"docker", "rm", "-f", containerName}, false, 0)
}

// TerminalBenchSolverContainerName 生成容器名（Python L494-497：小写 + 非字母数字
// → '-'，截 48，空回退 "case"，uuid hex 8 位后缀）。
func TerminalBenchSolverContainerName(caseID string) string {
	safe := tbSafeSegment(caseID)
	if safe == "" {
		safe = "case"
	}
	return fmt.Sprintf("ach-tb-solver-%s-%s", safe, uuidHex8())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runDocker 跑 docker 命令（Python run_docker L500-518：capture + 可选 check）。
func runDocker(command []string, check bool, timeoutSec int) (tbRunResult, error) {
	var timeout float64
	if timeoutSec > 0 {
		timeout = float64(timeoutSec)
	}
	result, err := terminalBenchRun(command[0], command[1:], timeout, nil)
	if err != nil {
		return result, err
	}
	if check && result.ExitCode != 0 {
		return result, fmt.Errorf(
			"command failed: %s\nstdout:\n%s\nstderr:\n%s",
			strings.Join(command, " "), result.Stdout, result.Stderr)
	}
	return result, nil
}

// tbRunProcess subprocess.run 的 Go 实现：capture 输出 + 超时（timeoutSec<=0 不限时）。
func tbRunProcess(name string, args []string, timeoutSec float64, input []byte) (tbRunResult, error) {
	ctx := context.Background()
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec*float64(time.Second)))
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	runErr := cmd.Run()
	result := tbRunResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: 0,
	}
	// 宿主限时收割（Python subprocess.TimeoutExpired 等价）：携带部分输出 + 哨兵。
	if ctx.Err() != nil {
		result.ExitCode = -1
		return result, fmt.Errorf("%w: %v", errTBDeadline, runErr)
	}
	if runErr == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	// 超时（context deadline）等不可恢复错误：携带已捕获的部分输出。
	result.ExitCode = -1
	return result, fmt.Errorf("%w: %v", errTBDeadline, runErr)
}

// containerCwd 宿主 cwd → 容器 cwd（Python _container_cwd L308-319：空/越界/根
// → 容器工作区默认目录；子目录 → 默认目录/相对 POSIX 路径）。
func (o *TerminalBenchDockerShellOperation) containerCwd(cwd string) string {
	if cwd == "" {
		return o.containerWorkspaceDir
	}
	expanded := pathutil.ExpandHome(cwd)
	resolved, err := filepath.Abs(expanded)
	if err != nil {
		return o.containerWorkspaceDir
	}
	relative, err := filepath.Rel(o.hostWorkspaceDir, resolved)
	if err != nil || strings.HasPrefix(relative, "..") {
		return o.containerWorkspaceDir
	}
	relativePosix := filepath.ToSlash(relative)
	if relativePosix == "." {
		return o.containerWorkspaceDir
	}
	return o.containerWorkspaceDir + "/" + relativePosix
}

// dockerExecCommand 组装 docker exec 命令（Python _docker_exec_command L321-355：
// exec → 可选 -d → 合并环境按 key 排序逐个 -e → shell 形态（clean: --noprofile
// --norc -c / 默认: -lc）→ enforce timeout 前缀 → -w cwd 容器名 shell）。
func (o *TerminalBenchDockerShellOperation) dockerExecCommand(command string, params dockerExecParams) []string {
	dockerCommand := []string{"docker", "exec"}
	if params.Detached {
		dockerCommand = append(dockerCommand, "-d")
	}
	mergedEnvironment := map[string]string{}
	for key, value := range o.runtimeEnvironment {
		mergedEnvironment[key] = value
	}
	for key, value := range params.Environment {
		mergedEnvironment[key] = value
	}
	keys := make([]string, 0, len(mergedEnvironment))
	for key := range mergedEnvironment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		dockerCommand = append(dockerCommand, "-e", fmt.Sprintf("%s=%s", key, mergedEnvironment[key]))
	}
	shellCommand := []string{"bash", "-lc", command}
	if o.cleanShell {
		shellCommand = []string{"bash", "--noprofile", "--norc", "-c", command}
	}
	if o.enforceInContainer && params.Timeout != nil && !params.Detached {
		timeoutSec := *params.Timeout
		if timeoutSec < 1 {
			timeoutSec = 1 // Python max(int(timeout), 1)
		}
		shellCommand = append([]string{"timeout", "--signal=TERM", "--kill-after=5s",
			fmt.Sprintf("%ds", timeoutSec)}, shellCommand...)
	}
	dockerCommand = append(dockerCommand, "-w", params.Cwd, o.containerName)
	dockerCommand = append(dockerCommand, shellCommand...)
	return dockerCommand
}

// cmdResult 组装成功结果（Python ExecuteCmdResult(code=StatusCode.SUCCESS.code,
// message=StatusCode.SUCCESS.errmsg, data=...)）。
func (o *TerminalBenchDockerShellOperation) cmdResult(command, cwd string, exitCode *int, stdout, stderr string) *sysopresult.ExecuteCmdResult {
	return &sysopresult.ExecuteCmdResult{
		BaseResult: sysopresult.BaseResult{Code: 0, Message: "Command executed successfully"},
		Data: &sysopresult.ExecuteCmdData{
			Command: command, Cwd: cwd, ExitCode: exitCode, Stdout: stdout, Stderr: stderr,
		},
	}
}

// tbSafeSegment case_id → 安全段（Python terminal_bench_solver_container_name
// L495-496：小写 → 非字母数字映射 '-' → strip('-') → 截 48）。
func tbSafeSegment(value string) string {
	var builder strings.Builder
	for _, ch := range strings.ToLower(value) {
		if isAlnumASCII(ch) {
			builder.WriteRune(ch)
		} else {
			builder.WriteByte('-')
		}
	}
	trimmed := strings.Trim(builder.String(), "-")
	if len(trimmed) > 48 {
		trimmed = trimmed[:48]
	}
	return trimmed
}

// isAlnumASCII 是否 ASCII 字母/数字（Python str.isalnum 对 ASCII 子集等价）。
func isAlnumASCII(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

// tbTimeoutStream Python _timeout_stream L521-526（Go 侧输入恒为 string，直返）。
func tbTimeoutStream(value string) string {
	return value
}

// tbExcerpt Python _excerpt L529-533：超长截 max-3 加 "..."。
func tbExcerpt(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars-3] + "..."
}

// uuidHex8 生成 8 位 hex 随机段（Python uuid.uuid4().hex[:8]）。
func uuidHex8() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// max64 float64 取大。
func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
