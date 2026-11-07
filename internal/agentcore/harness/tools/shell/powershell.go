package shell

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PowerShellInput PowerShellTool 输入参数。
// Python: _PowerShellInputs (powershell/_tool.py L47-55)
type PowerShellInput struct {
	// Command 要执行的命令（必需）
	Command string `json:"command"`
	// Timeout 超时秒数，0 表示调用方未传（取默认 300），上限 3600
	Timeout int `json:"timeout"`
	// Workdir 工作目录
	Workdir string `json:"workdir"`
	// Background 后台运行
	Background bool `json:"background"`
	// MaxOutputChars 最大输出字符数；nil 表示未传（取默认 20000），显式 0 表示不限制
	MaxOutputChars *int `json:"max_output_chars"`
	// Description 命令描述
	Description string `json:"description"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// psDefaultTimeout 未传 timeout 时的默认超时秒数。
	// Python: PowerShellTool._parse_inputs inputs.get("timeout", 300)
	psDefaultTimeout = 300

	// psDefaultMaxOutputChars 未传 max_output_chars 时的默认最大输出字符数。
	// Python: PowerShellTool._resolve_max_output_chars default=20000
	psDefaultMaxOutputChars = 20000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPowerShellTool 创建 PowerShellTool 实例。
// Python: PowerShellTool (powershell/_tool.py L58-78)
func NewPowerShellTool(op sys_operation.SysOperation, language, agentID string, permConfig PermissionConfig) tool.Tool {
	card, _ := tools.BuildToolCard("powershell", "PowerShellTool", language, nil, agentID)

	fn := func(ctx context.Context, input PowerShellInput, opts ...tool.ToolOption) (map[string]any, error) {
		// ── 参数解析 ──
		// Python: _parse_inputs (powershell/_tool.py L111-120)
		// PowerShell 无 sudo 注入，仅 strip
		command := strings.TrimSpace(input.Command)
		timeout := resolvePSTimeout(input.Timeout)
		workdir := input.Workdir
		background := input.Background
		maxOutputChars := resolvePSMaxOutputChars(input.MaxOutputChars)
		description := input.Description

		// ── 空命令检查 ──
		// Python: L135-136
		if command == "" {
			return map[string]any{
				"success": false,
				"error":   "command cannot be empty",
			}, nil
		}

		// ── cwd 解析 ──
		// Python: L138-139
		currentCwd := cwd.GetCwd(ctx)
		resolvedCwd := workdir
		if resolvedCwd == "" {
			resolvedCwd = currentCwd
		}

		// ── 安全守卫 (OPENJIUWEN_BASH_STRICT=1) ──
		// Python: L141-144
		if os.Getenv("OPENJIUWEN_BASH_STRICT") == "1" {
			blocked, reason := CheckPowerShellInjection(command)
			if blocked {
				return map[string]any{
					"success": false,
					"error":   reason,
				}, nil
			}
			// PowerShell 权限检查 isPowerShell=true
			allowed, permReason := CheckPermission(command, permConfig, true)
			if !allowed {
				return map[string]any{
					"success": false,
					"error":   permReason,
				}, nil
			}
		}

		// ── 破坏性命令警告 ──
		// Python: L146
		warning := GetPSDestructiveWarning(command)

		// ── description 日志 ──
		// Python: L148-149
		if description != "" {
			logger.Debug(logComponent).
				Str("description", description).
				Str("command", command).
				Msg("PowerShellTool")
		}

		// ── 后台执行 ──
		// Python: L151-159
		if background {
			bgRes, err := op.Shell().ExecuteCmdBackground(
				ctx, command,
				sys_operation.WithShellCwd(resolvedCwd),
				sys_operation.WithShellType(sys_operation.ShellTypePowerShell),
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

		// ── rm 目标记录（执行前）──
		// Python: L161-168: 执行前记录 PowerShell Remove-Item 目标
		var historyPath string
		callOpts := tool.NewToolCallOptions(opts...)
		session := callOpts.Session
		if session != nil {
			historyPath = buildHistoryPathFromOpts(opts, agentID)
			rmTargets := ParsePSRemoveTargets(command)
			if len(rmTargets) > 0 {
				recordRmTargetsBeforeDeletion(historyPath, rmTargets, resolvedCwd)
			}
		}

		// ── 前台执行 ──
		// Python: L170-219
		res, err := op.Shell().ExecuteCmd(
			ctx, command,
			sys_operation.WithShellCwd(resolvedCwd),
			sys_operation.WithShellTimeout(timeout),
			sys_operation.WithShellType(sys_operation.ShellTypePowerShell),
		)
		if err != nil {
			return map[string]any{
				"success": false,
				"error":   err.Error(),
			}, nil
		}

		// 失败路径：部分输出渲染
		// Python: L176-193
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
						IsPowerShell:   true,
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
		// Python: L195-219
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

		// PowerShell 退出码解释
		meaning := InterpretPowerShellExitCode(command, exitCode, stdout, stderr)

		// ── rm 目标记录（执行后）──
		// Python: L201-203: 执行后检测并记录删除
		if historyPath != "" && !meaning.IsError {
			filesystem.DetectAndRecordDeletions(historyPath)
		}

		content, isError := RenderToolContent(
			CommandOutput{
				Stdout:         stdout,
				Stderr:         stderr,
				ExitCode:       exitCode,
				Warning:        warning,
				MaxOutputChars: maxOutputChars,
				IsPowerShell:   true,
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

// resolvePSTimeout 解析并钳制 PowerShell 超时值。
// Python: PowerShellTool._resolve_timeout (powershell/_tool.py L81-92)
// 环境变量名: POWER_SHELL_TOOL_MAX_TIMEOUT_SECONDS
// 调用方未传 timeout 时该值为 0，按默认 300 秒处理；显式传 0/负值同样视为未传。
// 与 Python 的差异：Python inputs.get("timeout", 300) 只对「缺 key」取 300，
// 显式传 0 会被钳成 1 秒——1 秒对任何真实命令都是误杀，故 Go 统一按未传处理。
func resolvePSTimeout(rawValue int) int {
	timeout := rawValue
	if timeout <= 0 {
		timeout = psDefaultTimeout
	}
	maxTimeout := 3600
	if v := os.Getenv("POWER_SHELL_TOOL_MAX_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			maxTimeout = parsed
		}
	}
	if maxTimeout < 1 {
		maxTimeout = 1
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}
	return timeout
}

// resolvePSMaxOutputChars 解析并钳制 PowerShell 最大输出字符数。
// Python: PowerShellTool._resolve_max_output_chars (powershell/_tool.py L94-108)
// 环境变量名: POWER_SHELL_TOOL_MAX_OUTPUT_CHARS
//
// rawValue 为 nil 表示调用方未传，取默认值（同样受环境变量钳制）；
// 显式 0 表示不限制；正数取 [200, maxChars]。
func resolvePSMaxOutputChars(rawValue *int) int {
	maxChars := psDefaultMaxOutputChars
	if v := os.Getenv("POWER_SHELL_TOOL_MAX_OUTPUT_CHARS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			maxChars = parsed
		}
	}
	if maxChars < 200 {
		maxChars = 200
	}

	value := psDefaultMaxOutputChars
	if rawValue != nil {
		value = *rawValue
		if value == 0 {
			return 0
		}
	}
	if value < 200 {
		value = 200
	}
	if value > maxChars {
		value = maxChars
	}
	return value
}
