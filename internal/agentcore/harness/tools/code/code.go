package code

import (
	"context"
	"os"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeInput 代码执行工具的输入参数。
// 对齐 Python: CodeTool inputs (code.py L34)
type CodeInput struct {
	// Code 要执行的代码
	Code string `json:"code"`
	// Language 编程语言，默认 python
	Language string `json:"language"`
	// Timeout 超时时间，默认 300
	Timeout int `json:"timeout"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultTimeout 代码执行默认超时时间（秒）。
	// 对齐 Python: CodeTool._resolve_timeout default=300 (code.py L21)
	defaultTimeout = 300

	// defaultMaxTimeout 代码执行最大超时时间（秒）。
	// 对齐 Python: CodeTool._resolve_timeout CODE_TOOL_MAX_TIMEOUT_SECONDS (code.py L28)
	defaultMaxTimeout = 3600

	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeTool 创建 CodeTool 实例。
// 对齐 Python: CodeTool (code.py L14)
func NewCodeTool(op sys_operation.SysOperation, language, agentID string) tool.Tool {
	card, _ := tools.BuildToolCard("code", "CodeTool", language, nil, agentID)

	fn := func(ctx context.Context, input CodeInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 参数解析，默认值
		// 对齐 Python L36-37
		code := input.Code
		lang := input.Language
		if lang == "" {
			lang = "python"
		}
		timeout := resolveTimeout(input.Timeout)

		// 构建选项
		// 对齐 Python L39-41
		codeOpts := []sys_operation.CodeOption{
			sys_operation.WithCodeLanguage(lang),
			sys_operation.WithCodeTimeout(timeout),
		}
		// 如果是 Local 模式注入 cwd
		if op.Card() != nil && op.Card().Mode == sys_operation.OperationModeLocal {
			codeOpts = append(codeOpts, sys_operation.WithCodeCwd(cwd.GetCwd(ctx)))
		}

		// 执行代码
		// 对齐 Python L43
		res, execErr := op.Code().ExecuteCode(ctx, code, codeOpts...)
		if execErr != nil {
			logger.Error(logComponent).
				Str("language", lang).
				Int("timeout", timeout).
				Err(execErr).
				Msg("CodeTool ExecuteCode 调用失败")
			return map[string]any{
				"success": false,
				"error":   execErr.Error(),
			}, nil
		}

		// 失败: res.Code != SUCCESS
		// 对齐 Python L44-45
		if !res.IsSuccess() {
			logger.Error(logComponent).
				Str("language", lang).
				Str("message", res.Message).
				Msg("CodeTool ExecuteCode 返回失败")
			return map[string]any{
				"success": false,
				"error":   res.Message,
			}, nil
		}

		// 成功
		// 对齐 Python L47-55
		if res.Data != nil {
			exitCode := -1
			if res.Data.ExitCode != nil {
				exitCode = *res.Data.ExitCode
			}
			success := exitCode == 0

			errMsg := ""
			if exitCode != 0 {
				errMsg = res.Data.Stderr
			}

			logger.Info(logComponent).
				Str("language", lang).
				Int("exit_code", exitCode).
				Bool("success", success).
				Msg("CodeTool 执行完成")

			return map[string]any{
				"success": success,
				"data": map[string]any{
					"stdout":   res.Data.Stdout,
					"stderr":   res.Data.Stderr,
					"exit_code": exitCode,
				},
				"error": errMsg,
			}, nil
		}

		// data 为空
		logger.Warn(logComponent).
			Str("language", lang).
			Msg("CodeTool ExecuteCode 返回空 data")

		return map[string]any{
			"success": false,
			"data": map[string]any{
				"stdout":   "",
				"stderr":   "",
				"exit_code": -1,
			},
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveTimeout 解析并校验超时时间。
// 对齐 Python: CodeTool._resolve_timeout (code.py L20-32)
func resolveTimeout(rawValue int) int {
	timeout := rawValue
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	// 从环境变量读取最大超时
	maxTimeout := defaultMaxTimeout
	if envVal := os.Getenv("CODE_TOOL_MAX_TIMEOUT_SECONDS"); envVal != "" {
		if parsed, err := strconv.Atoi(envVal); err == nil && parsed > 0 {
			maxTimeout = parsed
		}
	}
	if maxTimeout < 1 {
		maxTimeout = 1
	}

	// 钳位到 [1, maxTimeout]
	if timeout < 1 {
		timeout = 1
	}
	if timeout > maxTimeout {
		timeout = maxTimeout
	}
	return timeout
}
