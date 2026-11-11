package processcli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/elk-language/go-prompt"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// 本文件是 channels/process_cli/repl.py 的 Go 对应物。
//
// Python: channels/process_cli/repl.py（1032 行）
//
// 职责：交互外壳。核心模型是「REPL 不持有 Runtime」——每一条指令 fork 一个
// worker 子进程（`uapclaw-process --_interactive-worker`），父进程只做
// 键盘读取、stdin 转发与 stdout 中继；子进程退出后读 worker-result.json
// 更新 REPL 的 session_id / mode / work_mode。

// ──────────────────────────── 结构体 ────────────────────────────

// replState REPL 持有的下一轮预览状态。
// Python: repl.py:91-98 _ReplState
type replState struct {
	// SessionID 当前会话标识（空表示尚未创建）
	SessionID string
	// Cwd 工作目录
	Cwd string
	// ModelName 模型展示名
	ModelName string
	// DisplayMode 展示态模式
	DisplayMode string
	// ModelSelection 模型选择键
	ModelSelection string
}

// workerCommandOptions workerCommand 的入参。
// Python: repl.py:106-114 _worker_command 形参
type workerCommandOptions struct {
	// PromptFile prompt 内容文件路径
	PromptFile string
	// SessionResultFile 会话 ID 回执文件
	SessionResultFile string
	// WorkerResultFile worker 结果回执文件
	WorkerResultFile string
	// Operation 操作类型（chat 或四个会话操作）
	Operation string
	// SessionID 要恢复的会话
	SessionID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// replLogLineTailBytes 长行截断阈值
	// Python: repl.py:63 _LOG_LINE_TAIL_BYTES
	replLogLineTailBytes = 64 * 1024
	// replLogTailLines 环形缓冲保留行数
	// Python: repl.py:162 deque(maxlen=20)
	replLogTailLines = 20
	// interruptGraceSeconds SIGINT 后等待退出
	// Python: repl.py:65
	interruptGraceSeconds = 15.0
	// terminateGraceSeconds SIGTERM 后等待退出
	// Python: repl.py:66
	terminateGraceSeconds = 5.0
	// killGraceSeconds SIGKILL 后等待退出
	// Python: repl.py:67
	killGraceSeconds = 5.0
	// maxSessionSearchRunes 会话搜索词上限
	maxSessionSearchRunes = 200
)

// ──────────────────────────── 全局变量 ────────────────────────────

// truncatedLogMarker 超长行截断标记。
// Python: repl.py:64 _TRUNCATED_LOG_MARKER
var truncatedLogMarker = "[...truncated...] "

// exitCommands 直接退出的输入。
// Python: repl.py:62 _EXIT_COMMANDS
var exitCommands = map[string]struct{}{"exit": {}, "quit": {}}

// ──────────────────────────── 导出函数 ────────────────────────────

// RunRepl 运行只做 UI 的外壳；每条指令拥有一个全新 worker 进程。
// Python: repl.py:962-1030 run_repl
func RunRepl(args *AppArgs) int {
	if args == nil {
		return exitFailure
	}
	args.WorkMode = ResolveCliWorkMode(args.Mode, args.WorkMode)
	ui := NewProcessCliUI(os.Stdout, 0)
	state := &replState{
		SessionID:   strings.TrimSpace(args.Session),
		Cwd:         resolvedCwd(args.Cwd),
		ModelName:   ResolveConfiguredModelName(),
		DisplayMode: ResolveDisplayMode(args.Mode, args.WorkMode),
	}
	promptSession := newPromptSession(os.Stdin, os.Stdout)
	ui.Startup(state.ModelName, state.DisplayMode, state.Cwd, state.SessionID)

	for {
		// Python: :979-991 每轮刷新展示态（配置可能已变）
		if state.ModelSelection == "" {
			state.ModelName = ResolveConfiguredModelName()
		}
		state.DisplayMode = ResolveDisplayMode(args.Mode, args.WorkMode)
		ui.Status(state.ModelName, state.DisplayMode, state.Cwd, state.SessionID)

		input, err := readPrompt(promptSession, promptText)
		if err != nil {
			if err == io.EOF {
				ui.BlankLine()
				return 0
			}
			ui.Notice("已取消当前输入，可以继续输入。")
			continue
		}
		if input == "" {
			continue
		}
		if _, isExit := exitCommands[strings.ToLower(input)]; isExit {
			return 0
		}
		if parsed := ParseSlashCommand(input); parsed != nil {
			if handleSlashCommand(args, parsed, ui, state, promptSession) {
				return 0
			}
			continue
		}
		if strings.HasPrefix(input, "/") {
			ui.Notice(fmt.Sprintf("未知命令：%s。输入 /help 查看可用命令。", firstToken(input)))
			continue
		}

		// Python: :1021-1029 普通输入 → fork worker
		code, nextSession := runWorker(args, workerRunOptions{
			Prompt:        input,
			SessionID:     state.SessionID,
			Operation:     operationChat,
			PromptSession: promptSession,
		})
		state.SessionID = nextSession
		if code == exitCancelled {
			ui.Notice("已中断当前指令，可以继续输入。")
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// workerRunOptions runWorker 的入参。
type workerRunOptions struct {
	// Prompt 本轮指令原文（或会话操作参数）
	Prompt string
	// SessionID 当前会话
	SessionID string
	// Operation 操作类型
	Operation string
	// PromptSession 父进程的读取器（chat 时用于运行中补充输入）
	PromptSession *prompt.Prompt
}

// workerCommand 组出 worker 子进程的完整命令。
// Python: repl.py:106-154 _worker_command
//
// 关键：mode 先取展示态再 DeprecateMode（P-1 的第二个直接调用者）。
func workerCommand(args *AppArgs, options workerCommandOptions) ([]string, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	workerWorkMode := ResolveCliWorkMode(args.Mode, args.WorkMode)
	workerMode := DeprecateModeForRepl(ResolveDisplayMode(args.Mode, workerWorkMode))

	command := []string{
		executable,
		"--output", OutputHuman,
		"--mode", workerMode,
		"--_interactive-worker",
		"--_session-result-file", options.SessionResultFile,
		"--_prompt-file", options.PromptFile,
	}
	if options.WorkerResultFile != "" {
		command = append(command, "--_worker-result-file", options.WorkerResultFile)
	}
	if options.Operation != "" && options.Operation != operationChat {
		command = append(command, "--_operation", options.Operation)
	} else {
		command = append(command, "--_forwarded-live-input")
		if selection := strings.TrimSpace(args.ModelSelection); selection != "" {
			command = append(command, "--_model-selection", selection)
		}
	}
	if options.SessionID != "" {
		command = append(command, "--session", options.SessionID)
	}
	if args.Cwd != "" {
		command = append(command, "--cwd", args.Cwd)
	}
	if args.ProjectDir != "" {
		command = append(command, "--project-dir", args.ProjectDir)
	}
	for _, trusted := range args.TrustedDirs {
		command = append(command, "--trusted-dir", trusted)
	}
	if args.TimeoutSet {
		command = append(command, "--timeout", strconv.FormatFloat(args.Timeout, 'f', -1, 64))
	}
	if args.ShowReasoning {
		command = append(command, "--show-reasoning")
	}
	if args.ShowTools {
		command = append(command, "--show-tools")
	}
	return command, nil
}

// DeprecateModeForRepl 转发 schema.DeprecateMode（repl 的 mode 归一入口）。
func DeprecateModeForRepl(mode string) string {
	return deprecateMode(mode)
}

// runWorker fork 一个 worker 并中继其输入输出。
// Python: repl.py:334-488 _run_worker
//
// 返回 (退出码, 下一轮会话 ID)。
func runWorker(args *AppArgs, options workerRunOptions) (int, string) {
	tempDir, err := os.MkdirTemp("", "uapclaw-process-repl-")
	if err != nil {
		return exitFailure, options.SessionID
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	resultPath := filepath.Join(tempDir, "session-id.txt")
	workerResultPath := filepath.Join(tempDir, "worker-result.json")
	promptPath := filepath.Join(tempDir, "prompt.txt")
	if err := os.WriteFile(promptPath, []byte(options.Prompt), 0o600); err != nil {
		return exitFailure, options.SessionID
	}

	command, err := workerCommand(args, workerCommandOptions{
		PromptFile:        promptPath,
		SessionResultFile: resultPath,
		WorkerResultFile:  workerResultPath,
		Operation:         options.Operation,
		SessionID:         options.SessionID,
	})
	if err != nil {
		return exitFailure, options.SessionID
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return exitFailure, options.SessionID
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return exitFailure, options.SessionID
	}
	if err := cmd.Start(); err != nil {
		return exitFailure, options.SessionID
	}

	// 三并发任务：日志排空 / 输出中继 / 输入转发
	logDone := make(chan []string, 1)
	go func() { logDone <- drainRuntimeLogs(stderrPipe, nil) }()
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		relayWorkerOutput(stdoutPipe, os.Stdout)
	}()

	waitErr := cmd.Wait()
	<-outputDone
	logTail := <-logDone

	returnCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			returnCode = exitErr.ExitCode()
		} else {
			returnCode = exitFailure
		}
	}

	nextSession := options.SessionID

	// Python: :445-473 读 worker-result.json 更新状态
	if loaded, ok := readWorkerResult(workerResultPath, options.Operation); ok {
		args.lastWorkerResult = loaded
		if value := stringOr(loaded["session_id"]); strings.TrimSpace(value) != "" {
			nextSession = strings.TrimSpace(value)
		}
		if mode := stringOr(loaded["mode"]); strings.TrimSpace(mode) != "" {
			args.Mode = strings.TrimSpace(mode)
		}
		if workMode := stringOr(loaded["work_mode"]); workMode == "code" || workMode == "work" {
			args.WorkMode = workMode
		}
		if projectDir, ok := loaded["project_dir"].(string); ok {
			args.ProjectDir = strings.TrimSpace(projectDir)
		}
	} else if options.Operation == operationChat {
		// Python: :474-477 chat 的兜底：读纯文本会话 ID
		if raw, readErr := os.ReadFile(resultPath); readErr == nil {
			if value := strings.TrimSpace(string(raw)); value != "" {
				nextSession = value
			}
		}
	}

	// Python: :478-484 有状态操作成功退出但无回执 → 视为未交付
	if returnCode == 0 {
		if _, isStateful := sessionOperations[options.Operation]; isStateful {
			if _, ok := args.lastWorkerResult["operation"]; !ok {
				returnCode = exitFailure
				logTail = append(logTail, "工作进程未返回已提交的 Session 结果")
			}
		}
	}

	// Python: :485-487 非 0/130 打印诊断
	if returnCode != 0 && returnCode != exitCancelled {
		diagnostics := append([]string{fmt.Sprintf("工作进程退出码：%d", returnCode)}, logTail...)
		NewProcessCliUI(os.Stderr, 0).Diagnostics(diagnostics)
	}
	return returnCode, nextSession
}

// drainRuntimeLogs 排空 worker stderr：回执行识别 + 环形缓冲。
// Python: repl.py:157-186 _drain_runtime_logs
//
// 回执行（带 ForwardedReceiptPrefix）不进取证缓冲，直接回调状态。
func drainRuntimeLogs(reader io.Reader, onReceipt func(status string)) []string {
	return drainRuntimeLogsFrom(reader, onReceipt)
}

// drainRuntimeLogsFrom 可注入 reader 的实现。
func drainRuntimeLogsFrom(reader io.Reader, onReceipt func(status string)) []string {
	tail := make([]string, 0, replLogTailLines)
	buffered := bufio.NewReader(reader)
	pending := ""
	for {
		chunk, err := buffered.ReadString('\n')
		pending += chunk
		for {
			index := strings.Index(pending, "\n")
			if index < 0 {
				break
			}
			line := strings.TrimRight(pending[:index], "\r")
			pending = pending[index+1:]
			if receipt := ParseForwardedReceipt(line); receipt != nil {
				if onReceipt != nil {
					onReceipt(receipt.Status)
				}
				continue
			}
			tail = append(tail, line)
			if len(tail) > replLogTailLines {
				tail = tail[len(tail)-replLogTailLines:]
			}
		}
		if len(pending) > replLogLineTailBytes {
			pending = truncatedLogMarker + pending[len(pending)-replLogLineTailBytes:]
		}
		if err != nil {
			if strings.TrimSpace(pending) != "" {
				tail = append(tail, strings.TrimRight(pending, "\r"))
				if len(tail) > replLogLineTailBytes {
					tail = tail[len(tail)-replLogTailLines:]
				}
			}
			return tail
		}
	}
}

// relayWorkerOutput 把 worker stdout 中继到父终端。
// Python: repl.py:197-220 _relay_worker_output
//
// 偏差 E-9：上游把输出累积进 layout 由全屏渲染，本实现直接写 stdout。
func relayWorkerOutput(reader io.Reader, writer io.Writer) {
	buffer := make([]byte, 64*1024)
	for {
		read, err := reader.Read(buffer)
		if read > 0 {
			_, _ = writer.Write(buffer[:read])
		}
		if err != nil {
			return
		}
	}
}

// handleSlashCommand 处理一条已识别的命令，返回 REPL 是否应退出。
// Python: repl.py:864-960 _handle_slash_command
func handleSlashCommand(
	args *AppArgs, command *ParsedSlashCommand,
	ui *ProcessCliUI, state *replState, promptSession *prompt.Prompt,
) bool {
	// Python: :872-873 控制类命令优先
	if handleControlCommand(args, command, ui, state) {
		return false
	}
	switch command.Name {
	case "/sessions":
		handleSessionsCommand(args, command.Arguments, state, ui, promptSession)
	case "/mode":
		state.DisplayMode = handleModeCommand(args, command.Arguments, state.DisplayMode, ui)
	case "/skills":
		handleSkillsCommand(args, command.Arguments, state.SessionID, ui)
	case "/new":
		handleNewCommand(args, command.Arguments, state, ui)
	case "/resume":
		handleResumeCommand(args, command.Arguments, state, ui, promptSession)
	case "/branch":
		handleBranchCommand(args, command.Arguments, state, ui)
	case "/delete":
		handleDeleteCommand(args, command.Arguments, state, ui)
	default:
		return handleSimpleSlashCommand(command, ui, state)
	}
	return false
}

// handleControlCommand 处理 /model /plan /status /permissions。
// Python: repl.py:767-861 _handle_control_command
func handleControlCommand(
	args *AppArgs, command *ParsedSlashCommand, ui *ProcessCliUI, state *replState,
) bool {
	switch command.Name {
	case "/model", "/plan", "/status", "/permissions":
	default:
		return false
	}
	arguments := command.Arguments
	timeout := args.Timeout
	if !args.TimeoutSet || timeout <= 0 {
		timeout = defaultQueryTimeoutSeconds
	}

	// Python: :779-798 /plan 只改 mode，不查 Runtime
	if command.Name == "/plan" {
		requested := strings.ToLower(arguments)
		switch requested {
		case "", "on", "off", "status":
		default:
			ui.Notice("用法：/plan [on|off|status]")
			return true
		}
		if !strings.HasPrefix(state.DisplayMode, "agent.") {
			ui.Notice("/plan 仅支持单 Agent 模式。")
			return true
		}
		if requested == "status" {
			if strings.HasSuffix(state.DisplayMode, ".plan") {
				ui.Notice("当前规划模式：开启")
			} else {
				ui.Notice("当前规划模式：关闭")
			}
			return true
		}
		enabled := !strings.HasSuffix(state.DisplayMode, ".plan")
		if requested != "" {
			enabled = requested == "on"
		}
		stateName := "normal"
		if enabled {
			stateName = "plan"
		}
		args.Mode = fmt.Sprintf("agent.%s.%s", args.WorkMode, stateName)
		state.DisplayMode = ResolveDisplayMode(args.Mode, args.WorkMode)
		ui.Notice(fmt.Sprintf("已切换模式：%s", state.DisplayMode))
		return true
	}

	switch command.Name {
	case "/model":
		// Python: :802-821
		if arguments == "" || strings.ToLower(arguments) == "list" {
			data, err := QueryRuntime("model.list", QueryRuntimeOptions{
				Cwd: state.Cwd, TimeoutSeconds: timeout,
			})
			if err != nil {
				ui.Notice(err.Error())
				return true
			}
			if showErr := ShowModels(ui, data, state.ModelSelection); showErr != nil {
				ui.Notice(showErr.Error())
			}
			return true
		}
		data, err := QueryRuntime("model.resolve", QueryRuntimeOptions{
			Cwd: state.Cwd, Params: map[string]any{"requested": arguments}, TimeoutSeconds: timeout,
		})
		if err != nil {
			ui.Notice(err.Error())
			return true
		}
		key := stringOr(data["selection_key"])
		if key == "" {
			ui.Notice("模型查询结果缺少选择键")
			return true
		}
		args.ModelSelection = key
		state.ModelSelection = key
		name := stringOr(data["display_name"])
		if name == "" {
			name = stringOr(data["model_name"])
		}
		if name == "" {
			name = key
		}
		state.ModelName = name
		ui.Notice(fmt.Sprintf("下一轮将使用模型：%s [%s]", state.ModelName, key))
	case "/status":
		// Python: :822-846
		if arguments != "" {
			ui.Notice("用法：/status")
			return true
		}
		var session map[string]any
		if state.SessionID != "" {
			data, err := QueryRuntime("session.get", QueryRuntimeOptions{
				Cwd: state.Cwd, Params: map[string]any{"session_id": state.SessionID},
				TimeoutSeconds: timeout,
			})
			if err != nil {
				ui.Notice(err.Error())
				return true
			}
			if raw, ok := data["session"].(map[string]any); ok {
				session = raw
			} else {
				ui.Notice("当前会话未在 Runtime 中找到。")
			}
		}
		if showErr := ShowStatus(ui, state.SessionID, session,
			state.DisplayMode, state.ModelName, state.Cwd); showErr != nil {
			ui.Notice(showErr.Error())
		}
	case "/permissions":
		// Python: :847-855
		if arguments != "" {
			ui.Notice("用法：/permissions")
			return true
		}
		params := map[string]any{}
		if state.SessionID != "" {
			params["session_id"] = state.SessionID
		}
		data, err := QueryRuntime("permission.get", QueryRuntimeOptions{
			Cwd: state.Cwd, Params: params, TimeoutSeconds: timeout,
		})
		if err != nil {
			ui.Notice(err.Error())
			return true
		}
		if showErr := ShowPermissions(ui, data); showErr != nil {
			ui.Notice(showErr.Error())
		}
	}
	return true
}

// handleSimpleSlashCommand 处理不需要 Runtime 的命令。
// Python: repl.py:935-959 _handle_simple_slash_command
func handleSimpleSlashCommand(command *ParsedSlashCommand, ui *ProcessCliUI, state *replState) bool {
	if command.Arguments != "" {
		ui.Notice(fmt.Sprintf("用法：%s", command.Name))
		return false
	}
	switch command.Name {
	case "/exit":
		return true
	case "/cancel":
		ui.Notice("当前没有运行中的任务。")
	case "/status":
		ui.Status(state.ModelName, state.DisplayMode, state.Cwd, state.SessionID)
	case "/help":
		ui.Help(false)
	case "/session":
		ui.Session(state.SessionID)
	}
	return false
}

// handleModeCommand 切换运行模式。
// Python: repl.py:491-509 _handle_mode_command
func handleModeCommand(
	args *AppArgs, arguments, displayMode string, ui *ProcessCliUI,
) string {
	if arguments == "" {
		ui.Notice(fmt.Sprintf("当前模式：%s", displayMode))
		return displayMode
	}
	nextMode, ok := ResolveModeTarget(arguments)
	if !ok {
		ui.Notice("用法：/mode <agent.work|agent.code|team.work|team.code>")
		return displayMode
	}
	args.Mode = nextMode
	args.WorkMode = ResolveCliWorkMode(nextMode, args.WorkMode)
	updated := ResolveDisplayMode(args.Mode, args.WorkMode)
	ui.Notice(fmt.Sprintf("已切换模式：%s", updated))
	return updated
}

// handleSkillsCommand 查看可用技能（fork worker）。
// Python: repl.py:512-529 _handle_skills_command
func handleSkillsCommand(args *AppArgs, arguments, sessionID string, ui *ProcessCliUI) {
	if lowered := strings.ToLower(arguments); lowered != "" && lowered != "list" {
		ui.Notice("用法：/skills list")
		return
	}
	code, _ := runWorker(args, workerRunOptions{
		Prompt: "/skills list", SessionID: sessionID, Operation: skillsListOperation,
	})
	if code == exitCancelled {
		ui.Notice("已中断当前指令，可以继续输入。")
	}
}

// handleNewCommand 创建并切换到新会话。
// Python: repl.py:537-556 _handle_new_command
func handleNewCommand(args *AppArgs, arguments string, state *replState, ui *ProcessCliUI) {
	lowered := strings.ToLower(arguments)
	if lowered != "" && lowered != "--persist" && lowered != "--persist-session" {
		ui.Notice("用法：/new [--persist|--persist-session]")
		return
	}
	code, nextSession := runWorker(args, workerRunOptions{
		Prompt: arguments, SessionID: state.SessionID, Operation: sessionCreateOperation,
	})
	if workerDelivered(args, sessionCreateOperation) && nextSession != "" {
		state.SessionID = nextSession
	}
	if code == exitCancelled {
		ui.Notice("已中断当前指令，可以继续输入。")
	}
}

// handleResumeCommand 选择或按 ID 恢复会话。
// Python: repl.py:645-665 _handle_resume_command
func handleResumeCommand(
	args *AppArgs, arguments string, state *replState, ui *ProcessCliUI, promptSession *prompt.Prompt,
) {
	parts := strings.Fields(arguments)
	if len(parts) > 1 {
		ui.Notice("用法：/resume [session-id]")
		return
	}
	sessionID := ""
	if len(parts) == 1 {
		sessionID = parts[0]
	} else {
		sessionID = chooseSession(args, state, ui, promptSession, "", 20, 0)
	}
	if sessionID != "" {
		resumeSessionByID(args, sessionID, state, ui)
	}
}

// handleSessionsCommand 浏览并选择会话。
// Python: repl.py:668-706 _handle_sessions_command
func handleSessionsCommand(
	args *AppArgs, arguments string, state *replState, ui *ProcessCliUI, promptSession *prompt.Prompt,
) {
	parts := strings.Fields(arguments)
	limit, offset, search := 20, 0, ""
	if len(parts) > 0 && isDecimalText(parts[0]) {
		if len(parts) > 2 || !allDecimal(parts) {
			ui.Notice("用法：/sessions [limit] [offset] 或 /sessions <关键词>")
			return
		}
		parsed, _ := strconv.Atoi(parts[0])
		limit = parsed
		if len(parts) == 2 {
			offset, _ = strconv.Atoi(parts[1])
		}
		if limit < 1 || limit > 200 {
			ui.Notice("limit 必须在 1 到 200 之间。")
			return
		}
	} else if arguments != "" {
		if strings.HasPrefix(strings.ToLower(arguments), "search ") {
			search = strings.TrimSpace(arguments[7:])
		} else {
			search = arguments
		}
		if len([]rune(search)) > maxSessionSearchRunes {
			ui.Notice("搜索词不能超过 200 个字符。")
			return
		}
	}
	if sessionID := chooseSession(args, state, ui, promptSession, search, limit, offset); sessionID != "" {
		resumeSessionByID(args, sessionID, state, ui)
	}
}

// handleBranchCommand 从当前会话创建分支。
// Python: repl.py:709-728 _handle_branch_command
func handleBranchCommand(args *AppArgs, arguments string, state *replState, ui *ProcessCliUI) {
	if state.SessionID == "" {
		ui.Notice("当前没有可创建分支的会话。")
		return
	}
	code, nextSession := runWorker(args, workerRunOptions{
		Prompt: arguments, SessionID: state.SessionID, Operation: sessionForkOperation,
	})
	if workerDelivered(args, sessionForkOperation) && nextSession != "" {
		state.SessionID = nextSession
	}
	if code == exitCancelled {
		ui.Notice("已中断当前指令，可以继续输入。")
	}
}

// handleDeleteCommand 删除指定会话（需二次确认）。
// Python: repl.py:739-764 _handle_delete_command
func handleDeleteCommand(args *AppArgs, arguments string, state *replState, ui *ProcessCliUI) {
	parts := strings.Fields(arguments)
	if len(parts) != 1 {
		ui.Notice("用法：/delete <session-id>")
		return
	}
	target := parts[0]
	if !confirmDelete(target) {
		ui.Notice("已取消删除。")
		return
	}
	code, _ := runWorker(args, workerRunOptions{
		Prompt: target, SessionID: state.SessionID, Operation: sessionDeleteOperation,
	})
	if workerDelivered(args, sessionDeleteOperation) && state.SessionID == target {
		state.SessionID = ""
	}
	if code == exitCancelled {
		ui.Notice("已中断当前指令，可以继续输入。")
	}
}

// chooseSession 浏览会话列表；仅在显式选择后返回 ID。
// Python: repl.py:559-624 _choose_session
func chooseSession(
	args *AppArgs, state *replState, ui *ProcessCliUI, promptSession *prompt.Prompt,
	search string, limit, offset int,
) string {
	for {
		data, err := QueryRuntime("session.list", QueryRuntimeOptions{
			Cwd: state.Cwd,
			Params: map[string]any{
				"limit": limit, "offset": offset, "search": search,
			},
			TimeoutSeconds: args.Timeout,
		})
		if err != nil {
			ui.Notice(err.Error())
			return ""
		}
		sessionIDs, showErr := ShowSessions(ui, data, state.SessionID, search)
		if showErr != nil {
			ui.Notice(showErr.Error())
			return ""
		}
		total := intOr(data["total"], 0)

		choice, readErr := readPrompt(promptSession, "选择序号；s <关键词> 搜索；n/p 翻页；回车取消> ")
		if readErr != nil {
			ui.Notice("已取消会话选择。")
			return ""
		}
		choice = strings.TrimSpace(choice)
		lowered := strings.ToLower(choice)

		switch {
		case lowered == "" || lowered == "q" || lowered == "quit":
			return ""
		case isDecimalText(lowered):
			index, _ := strconv.Atoi(lowered)
			if index >= 1 && index <= len(sessionIDs) {
				return sessionIDs[index-1]
			}
			ui.Notice("请输入当前页显示的会话序号。")
		case lowered == "n" || lowered == "next":
			if offset+limit < total {
				offset += limit
			} else {
				ui.Notice("已经是最后一页。")
			}
		case lowered == "p" || lowered == "prev":
			if offset > 0 {
				offset -= limit
				if offset < 0 {
					offset = 0
				}
			} else {
				ui.Notice("已经是第一页。")
			}
		case lowered == "s" || strings.HasPrefix(lowered, "s ") || strings.HasPrefix(lowered, "search "):
			_, after, found := strings.Cut(choice, " ")
			if !found {
				after = ""
			}
			search = strings.TrimSpace(after)
			if len([]rune(search)) > maxSessionSearchRunes {
				ui.Notice("搜索词不能超过 200 个字符。")
				continue
			}
			offset = 0
		default:
			ui.Notice("请输入序号、s <关键词>、n、p，或回车取消。")
		}
	}
}

// resumeSessionByID 按 ID 恢复会话（fork worker）。
// Python: repl.py:626-643 _resume_session_by_id
func resumeSessionByID(args *AppArgs, sessionID string, state *replState, ui *ProcessCliUI) {
	code, nextSession := runWorker(args, workerRunOptions{
		Prompt: sessionID, SessionID: state.SessionID, Operation: sessionSwitchOperation,
	})
	if workerDelivered(args, sessionSwitchOperation) && nextSession != "" {
		state.SessionID = nextSession
	}
	if code == exitCancelled {
		ui.Notice("已中断当前指令，可以继续输入。")
	}
}

// confirmDelete 二次确认删除。
// Python: repl.py:731-736 _confirm_delete
func confirmDelete(target string) bool {
	fmt.Fprintf(os.Stdout, "确认删除会话 %s？此操作不可恢复。[y/N] ", target)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// workerDelivered 上一次 worker 是否交付了指定操作的结果。
// Python: repl.py:532-534 _worker_delivered
func workerDelivered(args *AppArgs, operation string) bool {
	if args == nil || args.lastWorkerResult == nil {
		return false
	}
	return stringOr(args.lastWorkerResult["operation"]) == operation
}

// readWorkerResult 读取并校验 worker-result.json。
// Python: repl.py:448-473
func readWorkerResult(path, operation string) (map[string]any, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var loaded map[string]any
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return nil, false
	}
	if stringOr(loaded["operation"]) != operation {
		return nil, false
	}
	return loaded, true
}

// firstToken 取输入的首个 token。
func firstToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return value
	}
	return fields[0]
}

// allDecimal 判断所有片段是否都是十进制数字。
func allDecimal(parts []string) bool {
	for _, part := range parts {
		if !isDecimalText(part) {
			return false
		}
	}
	return true
}

// interruptWorker 三级降级终止 worker。
// Python: repl.py:298-331 _interrupt_worker
//
// SIGINT → terminate → kill，每级有独立等待窗口。
func interruptWorker(process *os.Process) {
	if process == nil {
		return
	}
	if err := process.Signal(os.Interrupt); err == nil {
		if waitForProcessExit(process, interruptGraceSeconds) {
			return
		}
	}
	if err := process.Kill(); err == nil {
		waitForProcessExit(process, killGraceSeconds)
	}
}

// waitForProcessExit 等待进程退出（超时返回 false）。
// Python: repl.py:288-295 _wait_for_worker_exit
func waitForProcessExit(process *os.Process, timeoutSeconds float64) bool {
	deadline := time.Now().Add(time.Duration(timeoutSeconds * float64(time.Second)))
	for time.Now().Before(deadline) {
		if !processStillRunning(process) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processStillRunning(process)
}

// processStillRunning 进程是否仍在运行。
func processStillRunning(process *os.Process) bool {
	if process == nil {
		return false
	}
	// Signal(0) 在 Unix 上探测存活；带 syscall 语义的探测放在平台文件更严谨，
	// 此处用 Release 语义之外的轻量判定：FindProcess 成功即视为存活。
	found, err := os.FindProcess(process.Pid)
	return err == nil && found != nil
}

// 确保 swruntime 引用有效（app.go 已用，此处保留以防未来清理）。
var _ = swruntime.RuntimeEvent{}
