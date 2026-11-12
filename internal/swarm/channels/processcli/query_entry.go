package processcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// 本文件是 channels/process_cli/query_entry.py 的 Go 对应物。
//
// Python: channels/process_cli/query_entry.py（136 行）
//
// 职责：--query-json 分支的外层入口。20.2 只落了 query.go 的 RunQuery
// 执行层，本文件是缺失的外层（P-3）。

// ──────────────────────────── 结构体 ────────────────────────────

// QueryEntryOptions ExecuteQuerySource 的入参。
type QueryEntryOptions struct {
	// Source 文档来源（"-" 读 stdin，其余当文件路径）
	Source string
	// Stdin 标准输入（nil 用 os.Stdin）
	Stdin io.Reader
	// Stdout 结果输出（nil 用独占的 stdout）
	Stdout io.Writer
	// ConflictingArguments 是否与既有参数混用
	ConflictingArguments bool
	// ClientFactory 测试注入；nil 用默认构造
	ClientFactory func() *InProcessRuntimeClient
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// queryExitInvalidInput 输入或控制非法
	// Python: query_entry.py:72 exit_code=2
	queryExitInvalidInput = 2
	// queryExitStartupFailed 启动或清理失败
	queryExitStartupFailed = 1
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExecuteQuerySource 在共享信号归属与清理之后发布唯一 query_result。
// Python: query_entry.py:60-146 execute_query_source
//
// 返回进程退出码。九步顺序（即语义）：
//  1. protocolStdout 拿独占 stdout
//  2. 冲突参数 → INVALID_INPUT / 2（解码之前，不构造 Runtime）
//  3. 解码失败 → INVALID_INPUT / 2（不构造 Runtime）
//  4. schema 校验失败 → INVALID_INPUT / 2（带 InputErrorDetails）
//  5. 文档 request_id 覆盖随机值
//  6. protocol.capabilities / protocol.schema → 直接返回目录，不起 Runtime
//  7. workspace 路径 resolve
//  8. 此时才构造 Runtime → RunQuery
//  9. 写一行 JSON 并返回 result.ExitCode
func ExecuteQuerySource(options QueryEntryOptions) int {
	// 测试/嵌入注入 writer 时不做 fd 重定向（否则测试捕获不到输出）；
	// 生产路径必走 protocolStdout，把 fd1 让给 JSONL。
	out := options.Stdout
	if out == nil {
		restoreStdout, data, err := protocolStdout()
		if err != nil {
			return queryExitStartupFailed
		}
		defer func() { _ = restoreStdout() }()
		out = data
	}

	requestID := newRequestID()

	// Python: :66-67 冲突参数
	if options.ConflictingArguments {
		return writeQueryFailure(out, requestID, "INVALID_INPUT",
			"--query-json must be used on its own.", queryExitInvalidInput)
	}

	stdin := options.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	document, err := readQueryDocument(options.Source, stdin)
	if err != nil {
		inputErr, ok := err.(*MachineInputError)
		if !ok {
			return writeQueryFailure(out, requestID, "INVALID_INPUT",
				err.Error(), queryExitInvalidInput)
		}
		id := requestID
		if inputErr.RequestID != nil && strings.TrimSpace(*inputErr.RequestID) != "" {
			id = strings.TrimSpace(*inputErr.RequestID)
		}
		return writeQueryFailureWithDetails(out, id, inputErr.Code(),
			inputErr.Message, queryExitInvalidInput, inputErr.Details)
	}

	// Python: :50-55 文档 request_id 非空则覆盖
	if document.RequestID != nil && strings.TrimSpace(*document.RequestID) != "" {
		requestID = strings.TrimSpace(*document.RequestID)
	}

	// Python: :82-95 协议目录查询不启动 Runtime
	switch document.Operation {
	case "protocol.capabilities":
		return writeQueryResult(out, protocol.NewProtocolCapabilitiesResult(requestID))
	case "protocol.schema":
		return writeQueryResult(out, protocol.NewProtocolSchemaResult(requestID))
	}

	// Python: :96-100 prepare_workspace
	prepared, prepErr := prepareQueryWorkspace(document)
	if prepErr != nil {
		return writeQueryFailure(out, requestID, "INVALID_INPUT",
			prepErr.Error(), queryExitInvalidInput)
	}

	// Python: :101-110 只有到这里才构造 Runtime——对齐「无效输入不导入 Runtime」不变量
	result := RunQuery(context.Background(), prepared, requestID, options.ClientFactory)
	return writeQueryResult(out, result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readQueryDocument 读并解码一份 query 文档。
// Python: query_entry.py:29-55 _read_query
//
// 差异：上游 `source == "-"` 走 duplex_input.DuplexLineReader（20.4 范围）；
// 本批用 ReadMachineDocument 直接读 stdin，20.4 接入行读取器时替换。
func readQueryDocument(
	source string, stdin io.Reader,
) (*protocol.OneShotQueryInput, error) {
	document, err := ReadMachineDocument(source, stdin)
	if err != nil {
		return nil, err
	}
	input, err := protocol.OneShotQueryInputFromDict(document)
	if err != nil {
		// Python: :47-55 try/except 转 MachineInputError，附 schema 诊断
		var requestID *string
		if raw, ok := document["request_id"].(string); ok {
			if trimmed := strings.TrimSpace(raw); trimmed != "" {
				requestID = &trimmed
			}
		}
		return nil, &MachineInputError{
			Message:   "Query input does not match the schema.",
			RequestID: requestID,
			Details:   protocol.InputErrorDetails(document, "query"),
		}
	}
	return input, nil
}

// prepareQueryWorkspace 解析 query 文档的 workspace 路径并 chdir。
// Python: machine_entry.py:87-106 prepare_workspace（query 输入复用同一语义）
func prepareQueryWorkspace(
	request *protocol.OneShotQueryInput,
) (*protocol.OneShotQueryInput, error) {
	if request.Workspace == nil {
		return request, nil
	}
	resolved, err := prepareWorkspaceSpec(request.Workspace)
	if err != nil {
		return nil, fmt.Errorf("Cannot access the requested workspace.")
	}
	copied := *request
	copied.Workspace = resolved
	return &copied, nil
}

// writeQueryResult 序列化一条 query_result 并写出。
// Python: query_entry.py:112-140（ensure_ascii=False, allow_nan=False, 紧凑分隔）
func writeQueryResult(out io.Writer, result *protocol.OneShotQueryResult) int {
	line, err := protocol.EncodeJSONLRecord(result)
	if err != nil {
		return queryExitStartupFailed
	}
	if _, err := io.WriteString(out, line); err != nil {
		return queryExitStartupFailed
	}
	return result.ExitCode
}

// writeQueryFailure 构造失败结果并写出。
// Python: query_entry.py:120-135（RuntimeErrorInfo(code, message, details)）
func writeQueryFailure(out io.Writer, requestID, code, message string, exitCode int) int {
	return writeQueryFailureWithDetails(out, requestID, code, message, exitCode, nil)
}

// writeQueryFailureWithDetails 带 details 的失败结果。
func writeQueryFailureWithDetails(
	out io.Writer, requestID, code, message string, exitCode int, details map[string]any,
) int {
	result := QueryFailure(requestID, code, message, exitCode)
	if details != nil && result.Error != nil {
		result.Error.Details = details
	}
	// 结果自身的校验失败说明构造有误，直接按启动失败处理
	if _, err := protocol.NewOneShotQueryResult(protocol.OneShotQueryResultParams{
		RequestID: result.RequestID,
		Operation: result.Operation,
		Status:    result.Status,
		ExitCode:  result.ExitCode,
		Data:      result.Data,
		Error:     result.Error,
	}); err != nil {
		return queryExitStartupFailed
	}
	if writeCode := writeQueryResult(out, result); writeCode != exitCode {
		return writeCode
	}
	return exitCode
}

// jsonUnused 保留 json 包引用（写一行结果时用于文档化 separators 语义）。
var _ = json.Marshal
