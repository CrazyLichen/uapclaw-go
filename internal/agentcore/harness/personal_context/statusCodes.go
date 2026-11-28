// Package personal_context 提供嵌入式 PersonalContext Core 运行时（24.3a 基座）。
//
// PersonalContext 单例持有少量编排职责：一个内存 pipeline 队列、一个消费者、
// 每个启用的 fetch provider 一个调度协程、每 provider 一个原子 JSON cursor。
// 知识图谱以发布的 Markdown 目录树为唯一事实源，查询时即时投影（无缓存）。
//
// 对应 Python 代码：openjiuwen/harness/personal_context/（真相源逐文件一比一移植）。
package personal_context

import (
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 枚举 ────────────────────────────

// PersonalContext 包专属状态码（status_codes.py:30-57，命名对齐 Python）。
// Python: openjiuwen/harness/personal_context/status_codes.py StatusCode
var (
	// StatusCodeContextProactiveConfigInvalid 主动上下文配置非法
	// Python: status_codes.py:31 CONTEXT_PROACTIVE_CONFIG_INVALID
	StatusCodeContextProactiveConfigInvalid = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_CONFIG_INVALID", 154000, "context proactive_config is invalid, reason: {error_msg}",
	)
	// StatusCodeContextProactiveStateInvalid 主动上下文状态非法
	// Python: status_codes.py:34 CONTEXT_PROACTIVE_STATE_INVALID
	StatusCodeContextProactiveStateInvalid = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_STATE_INVALID", 154001, "context proactive_state is invalid, reason: {error_msg}",
	)
	// StatusCodeContextProactiveFileExecutionError 主动上下文文件执行错误
	// Python: status_codes.py:37 CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR
	StatusCodeContextProactiveFileExecutionError = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR", 154002, "context proactive_file execution error, reason: {error_msg}",
	)
	// StatusCodeContextProactiveFetchExecutionError 主动上下文抓取执行错误
	// Python: status_codes.py:40 CONTEXT_PROACTIVE_FETCH_EXECUTION_ERROR
	StatusCodeContextProactiveFetchExecutionError = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_FETCH_EXECUTION_ERROR", 154003, "context proactive_fetch execution error, reason: {error_msg}",
	)
	// StatusCodeContextProactivePipelineExecutionError 主动上下文流水线执行错误
	// Python: status_codes.py:44 CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR
	StatusCodeContextProactivePipelineExecutionError = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR", 154004, "context proactive_pipeline execution error, reason: {error_msg}",
	)
	// StatusCodeContextProactivePublishExecutionError 主动上下文发布执行错误
	// Python: status_codes.py:48 CONTEXT_PROACTIVE_PUBLISH_EXECUTION_ERROR
	StatusCodeContextProactivePublishExecutionError = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_PUBLISH_EXECUTION_ERROR", 154005, "context proactive_publish execution error, reason: {error_msg}",
	)
	// StatusCodeContextProactiveRuntimeTimeout 主动上下文运行时超时
	// Python: status_codes.py:52 CONTEXT_PROACTIVE_RUNTIME_TIMEOUT
	StatusCodeContextProactiveRuntimeTimeout = exception.NewStatusCode(
		"CONTEXT_PROACTIVE_RUNTIME_TIMEOUT", 154006, "context proactive_runtime timeout, reason: {error_msg}",
	)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildError 在解析 PersonalContext 专属状态码的同时构建框架异常。
// Python: status_codes.py:15 build_error —— 本地 _PersonalContextStatus 走
// resolve_exception_class 解析，框架码走 framework build_error。
// Go 侧 exception.BuildError 已统一承担两类分类解析（偏差 D-24.3a-6：
// 内部异常框架在 NewStatusCode/ResolveCategory 层完成同等归类，无需本地 shim 分支），
// 本函数保留该名称以维持调用点一比一。
func BuildError(status exception.StatusCode, opts ...exception.ErrorOption) *exception.BaseError {
	return exception.BuildError(status, opts...)
}
