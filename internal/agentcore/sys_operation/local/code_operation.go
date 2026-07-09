package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeOperation 本地代码执行。
// 对齐 Python local/code_operation.py CodeOperation。
type CodeOperation struct {
	sysop.BaseCodeOperation
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	codeLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var _ sysop.CodeOperation = (*CodeOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeOperation 创建本地代码执行实例（工厂函数，供 OperationRegistry 调用）。
func NewCodeOperation(runConfig any) sysop.SysSubOperation {
	return &CodeOperation{}
}

// ExecuteCode 执行代码。
// 对齐 Python CodeOperation.execute_code：参数校验 → 语言支持检查 →
// buildSubprocessCmd → 环境变量 → 子进程执行 → 结果构造 → 日志记录。
func (c *CodeOperation) ExecuteCode(ctx context.Context, code string, opts ...sysop.CodeOption) (*result.ExecuteCodeResult, error) {
	o := sysop.NewCodeOptions(opts...)
	methodName := "execute_code"

	startTime := time.Now()
	logger.Info(codeLogComponent).Str("method_name", methodName).Str("language", o.Language).Msg("开始执行代码")

	if code == "" {
		return &result.ExecuteCodeResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationCodeExecutionError.Code(),
				"code can not be empty",
			),
		}, nil
	}

	// 构建子进程命令
	cmdArgs, tmpFile, err := c.buildSubprocessCmd(code, o.Language)
	if err != nil {
		return &result.ExecuteCodeResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationCodeExecutionError.Code(),
				err.Error(),
			),
		}, nil
	}
	if tmpFile != "" {
		defer os.Remove(tmpFile)
	}

	// 解析 CWD
	actualCwd := ResolveCwd(o.Cwd)

	// 创建子进程
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = c.prepareCodeEnv(o.Environment, o.Language)

	// 创建进程处理器
	handler := NewAsyncProcessHandler(cmd, defaultChunkSize, defaultShellEncoding, o.Timeout)

	// 执行
	invokeData, err := handler.Invoke(ctx)
	if err != nil {
		return &result.ExecuteCodeResult{
			BaseResult: result.BuildOperationErrorResult(
				exception.StatusSysOperationCodeExecutionError.Code(),
				fmt.Sprintf("unexpected error: %s", err),
			),
		}, nil
	}

	exitCode := invokeData.ExitCode
	if invokeData.Exception != nil {
		exitCode = -1
	}

	successResult := &result.ExecuteCodeResult{
		BaseResult: result.BaseResult{Code: 0, Message: "success"},
		Data: &result.ExecuteCodeData{
			CodeContent: code,
			Language:    o.Language,
			ExitCode:    &exitCode,
			Stdout:      invokeData.Stdout,
			Stderr:      invokeData.Stderr,
		},
	}

	logger.Info(codeLogComponent).Str("method_name", methodName).
		Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
		Msg("代码执行完成")

	return successResult, nil
}

// ExecuteCodeStream 流式执行代码。
// 对齐 Python CodeOperation.execute_code_stream。
func (c *CodeOperation) ExecuteCodeStream(ctx context.Context, code string, opts ...sysop.CodeOption) (<-chan result.ExecuteCodeStreamResult, error) {
	ch := make(chan result.ExecuteCodeStreamResult, 64)

	o := sysop.NewCodeOptions(opts...)
	cmdArgs, tmpFile, err := c.buildSubprocessCmd(code, o.Language)
	if err != nil {
		close(ch)
		return ch, err
	}
	if tmpFile != "" {
		defer os.Remove(tmpFile)
	}

	actualCwd := ResolveCwd(o.Cwd)
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = actualCwd
	cmd.Env = c.prepareCodeEnv(o.Environment, o.Language)

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
				ch <- result.ExecuteCodeStreamResult{
					BaseResult: result.BaseResult{Code: 0, Message: "stream output"},
					Data: &result.ExecuteCodeChunkData{
						Text:       event.Data,
						Type:       &eventType,
						ChunkIndex: chunkIndex,
					},
				}
				chunkIndex++
			case StreamEventTypeExit:
				ch <- result.ExecuteCodeStreamResult{
					BaseResult: result.BaseResult{Code: 0, Message: "success"},
					Data: &result.ExecuteCodeChunkData{
						ChunkIndex: chunkIndex,
						ExitCode:  &event.ExitCode,
					},
				}
				return
			case StreamEventTypeError:
				exitCode := -1
				ch <- result.ExecuteCodeStreamResult{
					BaseResult: result.BuildOperationErrorResult(
						exception.StatusSysOperationCodeExecutionError.Code(),
						event.Data,
					),
					Data: &result.ExecuteCodeChunkData{ChunkIndex: chunkIndex, ExitCode: &exitCode},
				}
				return
			}
		}
	}()

	return ch, nil
}

// ListTools 返回代码执行的工具卡片列表（硬编码）。
// description 严格使用 Python 方法英文 docstring 原文，不翻译。
// 对齐 Python BaseCodeOperation.list_tools：execute_code, execute_code_stream。
func (c *CodeOperation) ListTools() []*tool.ToolCard {
	executeCodeParams := []*schema.Param{
		{Name: "code", Description: "Non-empty string containing the source code to execute (required positional argument).", Type: schema.ParamTypeString, Required: true},
		{Name: "language", Description: `Programming language of the code. Strict type constraint to 'python' or 'javascript'.`, Type: schema.ParamTypeString, Default: "python",
			Enum: []any{"python", "javascript"}},
		{Name: "timeout", Description: "Maximum execution time in seconds. Defaults to 300 seconds (5 minutes).", Type: schema.ParamTypeInteger, Default: 300},
		{Name: "environment", Description: "Key-value dict of custom environment variables.", Type: schema.ParamTypeObject, Nullable: true},
		{Name: "cwd", Description: "Working directory for the execution environment, when supported by the provider.", Type: schema.ParamTypeString, Nullable: true},
		{Name: "options", Description: "Additional execution configuration options.", Type: schema.ParamTypeObject, Nullable: true},
	}

	executeCodeStreamParams := []*schema.Param{
		{Name: "code", Description: "Non-empty string containing the source code to execute (required positional argument).", Type: schema.ParamTypeString, Required: true},
		{Name: "language", Description: `Programming language of the code. Strict type constraint to 'python' or 'javascript'. Defaults to "python".`, Type: schema.ParamTypeString, Default: "python",
			Enum: []any{"python", "javascript"}},
		{Name: "timeout", Description: "Maximum execution time in seconds. Terminates the process if exceeded. Must be a positive integer. Defaults to 300 seconds (5 minutes).", Type: schema.ParamTypeInteger, Default: 300},
		{Name: "environment", Description: "Key-value dict of custom environment variables.", Type: schema.ParamTypeObject, Nullable: true},
		{Name: "cwd", Description: "Working directory for the execution environment, when supported by the provider.", Type: schema.ParamTypeString, Nullable: true},
		{Name: "options", Description: "Additional execution configuration options.", Type: schema.ParamTypeObject, Nullable: true},
	}

	return []*tool.ToolCard{
		tool.NewToolCard("execute_code",
			"Execute arbitrary code asynchronously.",
			executeCodeParams, nil),
		tool.NewToolCard("execute_code_stream",
			"Execute arbitrary code asynchronously, by streaming.",
			executeCodeStreamParams, nil),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSubprocessCmd 构建代码执行子进程命令。
// 对齐 Python CodeOperation 的 supportLanguageConfigDict + buildSubprocessCmd。
func (c *CodeOperation) buildSubprocessCmd(code string, language string) ([]string, string, error) {
	switch language {
	case "python", "python3":
		// 短代码用 -c，长代码写临时文件
		if len(code) < 4000 {
			return []string{"python3", "-c", code}, "", nil
		}
		tmpFile, err := (&OperationUtils{}).CreateTmpFile(code, ".py")
		if err != nil {
			return nil, "", err
		}
		return []string{"python3", tmpFile}, tmpFile, nil
	case "javascript", "node":
		if len(code) < 4000 {
			return []string{"node", "-e", code}, "", nil
		}
		tmpFile, err := (&OperationUtils{}).CreateTmpFile(code, ".js")
		if err != nil {
			return nil, "", err
		}
		return []string{"node", tmpFile}, tmpFile, nil
	default:
		return nil, "", fmt.Errorf("unsupported language: %s (supported: python, javascript)", language)
	}
}

// prepareCodeEnv 准备代码执行环境变量。
// 对齐 Python 中的 PYTHONIOENCODING/PYTHONUTF8/NODE_DISABLE_COLORS。
func (c *CodeOperation) prepareCodeEnv(customEnv map[string]string, language string) []string {
	env := os.Environ()

	// 语言特定环境变量
	switch language {
	case "python", "python3":
		env = append(env, "PYTHONIOENCODING=utf-8")
		env = append(env, "PYTHONUTF8=1")
	case "javascript", "node":
		env = append(env, "NODE_DISABLE_COLORS=1")
	}

	// 自定义环境变量
	for k, v := range customEnv {
		env = append(env, k+"="+v)
	}

	return env
}

// init 注册到 GlobalRegistry
func init() {
	_ = sysop.GlobalRegistry.Register(sysop.OperationDef{
		Name:        "code",
		Mode:        sysop.OperationModeLocal,
		Description: "local code operation",
		NewFunc:     NewCodeOperation,
	})
}
