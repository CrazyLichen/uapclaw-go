package shell

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BashInput BashTool 输入参数。
// 对齐 Python: _BashInputs (bash/_tool.py L61-70)
type BashInput struct {
	// Command 要执行的命令（必需）
	Command string `json:"command"`
	// Timeout 超时秒数，默认 300，上限 3600
	Timeout int `json:"timeout"`
	// Description 命令描述
	Description string `json:"description"`
	// RunInBackground 后台运行
	RunInBackground bool `json:"run_in_background"`
	// Workdir 工作目录
	Workdir string `json:"workdir"`
	// MaxOutputChars 最大输出字符数，0=无限制
	MaxOutputChars int `json:"max_output_chars"`
	// ShellType shell 类型: auto/cmd/powershell/bash/sh
	ShellType string `json:"shell_type"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// bashDefaultTimeout 默认超时秒数。
	// 对齐 Python: BashTool._resolve_timeout default=300
	bashDefaultTimeout = 300

	// bashDefaultMaxOutputChars 默认最大输出字符数，0=无限制。
	// 对齐 Python: BashTool._resolve_max_output_chars default=0
	bashDefaultMaxOutputChars = 0
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件常量
	logComponent = logger.ComponentAgentCore

	// validShellTypes 合法的 shell 类型集合。
	// 对齐 Python: _VALID_SHELL_TYPES (bash/_tool.py L57)
	validShellTypes = map[string]bool{
		"auto": true, "cmd": true, "powershell": true, "bash": true, "sh": true,
	}

	// sudoNeedsNRe 匹配需要注入 -n 的 sudo。
	// 对齐 Python: _SUDO_NEEDS_N_RE (bash/_tool.py L47-49)
	// 使用 regexp2 支持 Perl lookahead 语法 (?!...) 和 (?=...)
	sudoNeedsNRe = regexp2.MustCompile(`\bsudo\b(?!(?:\s+-[a-zA-Z]*n|\s+--non-interactive))(?=\s)`, 0)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBashTool 创建 BashTool 实例。
// 对齐 Python: BashTool (bash/_tool.py L73-95)
func NewBashTool(op sys_operation.SysOperation, language, agentID string, permConfig PermissionConfig) tool.Tool {
	card, _ := tools.BuildToolCard("bash", "BashTool", language, nil, agentID)

	fn := func(ctx context.Context, input BashInput, opts ...tool.ToolOption) (map[string]any, error) {
		// ── 参数解析 ──
		// 对齐 Python: _parse_inputs (bash/_tool.py L129-142)
		command := makeSudoNoninteractive(strings.TrimSpace(input.Command))
		timeout := resolveBashTimeout(input.Timeout)
		workdir := input.Workdir
		runInBackground := input.RunInBackground
		maxOutputChars := resolveBashMaxOutputChars(input.MaxOutputChars)
		shellType := input.ShellType
		if !validShellTypes[shellType] {
			shellType = "auto"
		}
		description := input.Description

		// ── 空命令检查 ──
		// 对齐 Python L164-165
		if command == "" {
			return map[string]any{
				"success": false,
				"error":   "command cannot be empty",
			}, nil
		}

		// ── 安全守卫 (OPENJIUWEN_BASH_STRICT=1) ──
		// 对齐 Python L167-170
		if os.Getenv("OPENJIUWEN_BASH_STRICT") == "1" {
			blocked, reason := CheckBashInjection(command)
			if blocked {
				return map[string]any{
					"success": false,
					"error":   reason,
				}, nil
			}
			allowed, permReason := CheckPermission(command, permConfig, false)
			if !allowed {
				return map[string]any{
					"success": false,
					"error":   permReason,
				}, nil
			}
		}

		// ── cwd 解析 ──
		// 对齐 Python L172-176
		currentCwd := cwd.GetCwd(ctx)
		resolvedCwd := workdir
		if resolvedCwd == "" {
			resolvedCwd = currentCwd
		}
		if workdir != "" {
			if info, err := os.Stat(resolvedCwd); err != nil || !info.IsDir() {
				return map[string]any{
					"success": false,
					"error":   fmt.Sprintf("workdir does not exist: %s", resolvedCwd),
				}, nil
			}
		}

		// ── 破坏性命令警告 ──
		// 对齐 Python L178
		warning := GetBashDestructiveWarning(command)

		// ── description 日志 ──
		// 对齐 Python L180-181
		if description != "" {
			logger.Debug(logComponent).
				Str("description", description).
				Str("command", command).
				Msg("BashTool")
		}

		// ── 后台执行 ──
		// 对齐 Python L184-190
		if runInBackground {
			bgRes, err := op.Shell().ExecuteCmdBackground(
				ctx, command,
				sys_operation.WithShellCwd(resolvedCwd),
				sys_operation.WithShellType(sys_operation.ParseShellType(shellType)),
			)
			if err != nil {
				return map[string]any{
					"success": false,
					"error":   err.Error(),
				}, nil
			}
			if !bgRes.IsSuccess() {
				return map[string]any{
					"success": false,
					"error":   bgRes.Message,
				}, nil
			}
			pid := 0
			if bgRes.Data != nil && bgRes.Data.Pid != nil {
				pid = *bgRes.Data.Pid
			}
			return map[string]any{
				"success": true,
				"data": map[string]any{
					"pid":    pid,
					"status": "started",
				},
			}, nil
		}

		// ── 前台执行 ──
		// 对齐 Python L202-248
		res, err := op.Shell().ExecuteCmd(
			ctx, command,
			sys_operation.WithShellCwd(resolvedCwd),
			sys_operation.WithShellTimeout(timeout),
			sys_operation.WithShellType(sys_operation.ParseShellType(shellType)),
		)
		if err != nil {
			return map[string]any{
				"success": false,
				"error":   err.Error(),
			}, nil
		}

		// 失败路径：部分输出渲染
		// 对齐 Python L205-222
		if !res.IsSuccess() {
			var partial string
			if res.Data != nil {
				exitCode := -1
				if res.Data.ExitCode != nil {
					exitCode = *res.Data.ExitCode
				}
				partial = RenderPartialOnFailure(
					CommandOutput{
						Stdout:         res.Data.Stdout,
						Stderr:         res.Data.Stderr,
						ExitCode:       exitCode,
						Warning:        warning,
						MaxOutputChars: maxOutputChars,
					},
					res.Message,
				)
			}
			if partial != "" {
				return map[string]any{
					"success": false,
					"data":    map[string]any{"content": partial},
					"error":   partial,
				}, nil
			}
			return map[string]any{
				"success": false,
				"error":   res.Message,
			}, nil
		}

		// 成功路径
		// 对齐 Python L224-248
		exitCode := -1
		stdout := ""
		stderr := ""
		if res.Data != nil {
			if res.Data.ExitCode != nil {
				exitCode = *res.Data.ExitCode
			}
			stdout = res.Data.Stdout
			stderr = res.Data.Stderr
		}

		meaning := InterpretBashExitCode(command, exitCode, stdout, stderr)

		content, isError := RenderToolContent(
			CommandOutput{
				Stdout:         stdout,
				Stderr:         stderr,
				ExitCode:       exitCode,
				Warning:        warning,
				MaxOutputChars: maxOutputChars,
			},
			meaning.IsError,
		)

		if isError {
			return map[string]any{
				"success": false,
				"data":    map[string]any{"content": content},
				"error":   content,
			}, nil
		}
		return map[string]any{
			"success": true,
			"data":    map[string]any{"content": content},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeSudoNoninteractive 注入 sudo -n 标志，让 sudo 非交互失败而非挂起等密码。
// 对齐 Python: _make_sudo_noninteractive (bash/_tool.py L52-54)
func makeSudoNoninteractive(command string) string {
	result, err := sudoNeedsNRe.Replace(command, "sudo -n", 0, -1)
	if err != nil {
		return command
	}
	return result
}

// resolveBashTimeout 解析并钳制超时值。
// 对齐 Python: BashTool._resolve_timeout (bash/_tool.py L99-110)
func resolveBashTimeout(rawValue int) int {
	timeout := rawValue
	maxTimeout := 3600
	if v := os.Getenv("BASH_TOOL_MAX_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			maxTimeout = parsed
		}
	}
	if maxTimeout < 1 {
		maxTimeout = 1
	}
	if timeout < 1 {
		timeout = 1
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}
	return timeout
}

// resolveBashMaxOutputChars 解析并钳制最大输出字符数。0 表示无限制。
// 对齐 Python: BashTool._resolve_max_output_chars (bash/_tool.py L113-126)
func resolveBashMaxOutputChars(rawValue int) int {
	value := rawValue
	if value == 0 {
		return 0
	}
	maxChars := 20000
	if v := os.Getenv("BASH_TOOL_MAX_OUTPUT_CHARS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			maxChars = parsed
		}
	}
	if maxChars < 200 {
		maxChars = 200
	}
	if value < 200 {
		value = 200
	}
	if value > maxChars {
		value = maxChars
	}
	return value
}
