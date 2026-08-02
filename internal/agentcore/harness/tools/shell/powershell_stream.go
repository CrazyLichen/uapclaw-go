package shell

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PowerShellStreamInput PowerShellStreamTool 输入参数。
// 对齐 Python: _PowerShellInputs (powershell/_tool.py L47-55)，与 PowerShellInput 一致
type PowerShellStreamInput struct {
	// Command 要执行的命令（必需）
	Command string `json:"command"`
	// Timeout 超时秒数，默认 300，上限 3600
	Timeout int `json:"timeout"`
	// Workdir 工作目录
	Workdir string `json:"workdir"`
	// MaxOutputChars 最大输出字符数，0=无限制
	MaxOutputChars int `json:"max_output_chars"`
	// Description 命令描述
	Description string `json:"description"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPowerShellStreamTool 创建 PowerShellStreamTool 实例（流式执行）。
// 对齐 Python: PowerShellTool.stream (powershell/_tool.py L221-311)
// 流式返回命令输出块，并在流结束后返回汇总渲染内容。
func NewPowerShellStreamTool(op sys_operation.SysOperation, language, agentID string, permConfig PermissionConfig) tool.Tool {
	card, _ := tools.BuildToolCard("powershell", "PowerShellStreamTool", language, nil, agentID)

	fn := func(ctx context.Context, input PowerShellStreamInput, opts ...tool.ToolOption) (<-chan map[string]any, error) {
		// ── 参数解析 ──
		// 对齐 Python: _parse_inputs (powershell/_tool.py L111-120)
		command := strings.TrimSpace(input.Command)
		timeout := resolvePSTimeout(input.Timeout)
		workdir := input.Workdir
		maxOutputChars := resolvePSMaxOutputChars(input.MaxOutputChars)
		description := input.Description

		ch := make(chan map[string]any, 64)

		// ── 空命令检查 ──
		// 对齐 Python L226-228
		if command == "" {
			go func() {
				defer close(ch)
				ch <- map[string]any{
					"success": false,
					"error":   "command cannot be empty",
				}
			}()
			return ch, nil
		}

		// ── 安全守卫 (OPENJIUWEN_BASH_STRICT=1) ──
		// 对齐 Python L233-237
		if os.Getenv("OPENJIUWEN_BASH_STRICT") == "1" {
			blocked, reason := CheckPowerShellInjection(command)
			if blocked {
				go func() {
					defer close(ch)
					ch <- map[string]any{
						"success": false,
						"error":   reason,
					}
				}()
				return ch, nil
			}
			// PowerShell 权限检查 isPowerShell=true
			allowed, permReason := CheckPermission(command, permConfig, true)
			if !allowed {
				go func() {
					defer close(ch)
					ch <- map[string]any{
						"success": false,
						"error":   permReason,
					}
				}()
				return ch, nil
			}
		}

		// ── cwd 解析 ──
		// 对齐 Python L230-231
		currentCwd := cwd.GetCwd(ctx)
		resolvedCwd := workdir
		if resolvedCwd == "" {
			resolvedCwd = currentCwd
		}

		// ── 破坏性命令警告 ──
		// 对齐 Python L239
		warning := GetPSDestructiveWarning(command)

		// ── description 日志 ──
		// 对齐 Python L241-242
		if description != "" {
			logger.Debug(logComponent).
				Str("description", description).
				Str("command", command).
				Msg("PowerShellTool(stream)")
		}

		// ── rm 目标记录（执行前）──
		// 对齐 Python L244-251: 执行前记录 PowerShell Remove-Item 目标
		historyPath := buildHistoryPathFromOpts(opts, agentID)
		if historyPath != "" {
			rmTargets := ParsePSRemoveTargets(command)
			recordRmTargetsBeforeDeletion(historyPath, rmTargets, resolvedCwd)
		}

		// ── 流式执行 ──
		// 对齐 Python L258-290: async for chunk in execute_cmd_stream(...)
		streamCh, err := op.Shell().ExecuteCmdStream(
			ctx, command,
			sys_operation.WithShellCwd(resolvedCwd),
			sys_operation.WithShellTimeout(timeout),
			sys_operation.WithShellType(sys_operation.ShellTypePowerShell),
		)
		if err != nil {
			go func() {
				defer close(ch)
				ch <- map[string]any{
					"success": false,
					"error":   err.Error(),
				}
			}()
			return ch, nil
		}

		go func() {
			defer close(ch)

			start := time.Now()
			accumulatedStdout := ""
			accumulatedStderr := ""
			finalExitCode := -1

			// 遍历流式输出块
			// 对齐 Python L258-290
			for chunk := range streamCh {
				if !chunk.IsSuccess() {
					// 流错误：直接返回错误块
					ch <- map[string]any{
						"success": false,
						"error":   chunk.Message,
					}
					return
				}

				data := chunk.Data
				elapsed := roundElapsed(time.Since(start).Seconds())

				if data != nil {
					// 记录退出码
					if data.ExitCode != nil {
						finalExitCode = *data.ExitCode
					}

					text := data.Text
					streamType := "stdout"
					if data.Type != nil {
						streamType = *data.Type
					}

					// 累积 stdout/stderr
					if streamType == "stderr" {
						accumulatedStderr += text
					} else {
						accumulatedStdout += text
					}

					// 发送流式输出块
					// 对齐 Python L281-290: yield ToolOutput(success=True, data={...})
					ch <- map[string]any{
						"success":             true,
						"text":                text,
						"type":                streamType,
						"chunk_index":         data.ChunkIndex,
						"exit_code":           data.ExitCode,
						"elapsed_time_seconds": elapsed,
					}
				}
			}

			// ── 流结束：后处理 ──
			// 对齐 Python L292-311

			// 退出码语义解释（PowerShell）
			meaning := InterpretPowerShellExitCode(command, finalExitCode, accumulatedStdout, accumulatedStderr)

			// ── rm 目标记录（执行后）──
			// 对齐 Python L294-295
			if historyPath != "" && !meaning.IsError {
				filesystem.DetectAndRecordDeletions(historyPath)
			}

			// 渲染汇总输出（PowerShell 标记）
			content, isError := RenderToolContent(
				CommandOutput{
					Stdout:         accumulatedStdout,
					Stderr:         accumulatedStderr,
					ExitCode:       finalExitCode,
					Warning:        warning,
					MaxOutputChars: maxOutputChars,
					IsPowerShell:   true,
				},
				meaning.IsError,
			)

			// 发送最终汇总块
			// 对齐 Python L307-311: yield ToolOutput(success=not is_error, data={"content": content})
			if isError {
				ch <- map[string]any{
					"success": false,
					"data":    map[string]any{"content": content},
					"error":   content,
				}
			} else {
				ch <- map[string]any{
					"success": true,
					"data":    map[string]any{"content": content},
				}
			}
		}()

		return ch, nil
	}

	streamFn, _ := tool.NewStreamTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return streamFn
}
