package shell

import (
	"context"
	"fmt"
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

// BashStreamInput BashStreamTool 输入参数。
// 对齐 Python: _BashInputs (bash/_tool.py L61-70)，与 BashInput 一致
type BashStreamInput struct {
	// Command 要执行的命令（必需）
	Command string `json:"command"`
	// Timeout 超时秒数，默认 300，上限 3600
	Timeout int `json:"timeout"`
	// Description 命令描述
	Description string `json:"description"`
	// Workdir 工作目录
	Workdir string `json:"workdir"`
	// MaxOutputChars 最大输出字符数，0=无限制
	MaxOutputChars int `json:"max_output_chars"`
	// ShellType shell 类型: auto/cmd/powershell/bash/sh
	ShellType string `json:"shell_type"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewBashStreamTool 创建 BashStreamTool 实例（流式执行）。
// 对齐 Python: BashTool.stream (bash/_tool.py L250-340)
// 流式返回命令输出块，并在流结束后返回汇总渲染内容。
func NewBashStreamTool(op sys_operation.SysOperation, language, agentID string, permConfig PermissionConfig) tool.Tool {
	card, _ := tools.BuildToolCard("bash", "BashStreamTool", language, nil, agentID)

	fn := func(ctx context.Context, input BashStreamInput, opts ...tool.ToolOption) (<-chan map[string]any, error) {
		// ── 参数解析 ──
		command := makeSudoNoninteractive(strings.TrimSpace(input.Command))
		timeout := resolveBashTimeout(input.Timeout)
		workdir := input.Workdir
		maxOutputChars := resolveBashMaxOutputChars(input.MaxOutputChars)
		shellType := input.ShellType
		if !validShellTypes[shellType] {
			shellType = "auto"
		}
		description := input.Description

		ch := make(chan map[string]any, 64)

		// ── 空命令检查 ──
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
		// 对齐 Python L264-268
		if os.Getenv("OPENJIUWEN_BASH_STRICT") == "1" {
			blocked, reason := CheckBashInjection(command)
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
			allowed, permReason := CheckPermission(command, permConfig, false)
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
		currentCwd := cwd.GetCwd(ctx)
		resolvedCwd := workdir
		if resolvedCwd == "" {
			resolvedCwd = currentCwd
		}
		if workdir != "" {
			if info, err := os.Stat(resolvedCwd); err != nil || !info.IsDir() {
				go func() {
					defer close(ch)
					ch <- map[string]any{
						"success": false,
						"error":   fmt.Sprintf("workdir does not exist: %s", resolvedCwd),
					}
				}()
				return ch, nil
			}
		}

		// ── 破坏性命令警告 ──
		warning := GetBashDestructiveWarning(command)

		// ── description 日志 ──
		// 对齐 Python L273-274
		if description != "" {
			logger.Debug(logComponent).
				Str("description", description).
				Str("command", command).
				Msg("BashTool(stream)")
		}

		// ── rm 目标记录（执行前）──
		// 对齐 Python L275-282
		historyPath := buildHistoryPathFromOpts(opts, agentID)
		if historyPath != "" {
			rmTargets := ParseRmTargets(command)
			recordRmTargetsBeforeDeletion(historyPath, rmTargets, resolvedCwd)
		}

		// ── 流式执行 ──
		// 对齐 Python L289-318: async for chunk in execute_cmd_stream(...)
		streamCh, err := op.Shell().ExecuteCmdStream(
			ctx, command,
			sys_operation.WithShellCwd(resolvedCwd),
			sys_operation.WithShellTimeout(timeout),
			sys_operation.WithShellType(sys_operation.ParseShellType(shellType)),
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
			// 对齐 Python L289-318
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
					// 对齐 Python L309-318: yield ToolOutput(success=True, data={...})
					ch <- map[string]any{
						"success":              true,
						"text":                 text,
						"type":                 streamType,
						"chunk_index":          data.ChunkIndex,
						"exit_code":            data.ExitCode,
						"elapsed_time_seconds": elapsed,
					}
				}
			}

			// ── 流结束：后处理 ──
			// 对齐 Python L320-340

			// 退出码语义解释
			meaning := InterpretBashExitCode(command, finalExitCode, accumulatedStdout, accumulatedStderr)

			// ── rm 目标记录（执行后）──
			// 对齐 Python L322-323
			if historyPath != "" && !meaning.IsError {
				filesystem.DetectAndRecordDeletions(historyPath)
			}

			// 渲染汇总输出
			content, isError := RenderToolContent(
				CommandOutput{
					Stdout:         accumulatedStdout,
					Stderr:         accumulatedStderr,
					ExitCode:       finalExitCode,
					Warning:        warning,
					MaxOutputChars: maxOutputChars,
				},
				meaning.IsError,
			)

			// 发送最终汇总块
			// 对齐 Python L335-339: yield ToolOutput(success=not is_error, data={"content": content})
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// roundElapsed 将秒数保留两位小数。
// 对齐 Python: round(time.monotonic() - start, 2)
func roundElapsed(seconds float64) float64 {
	return float64(int(seconds*100+0.5)) / 100
}
