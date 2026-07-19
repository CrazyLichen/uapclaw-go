package tool_call

import (
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SimpleAPIWrapperFromCallable 基于可调用函数的简易 API 包装器。
// 将工具调用委托给预先注册的可调用函数，对齐 Python SimpleAPIWrapperFromCallable。
//
// 对齐 Python: openjiuwen/agent_evolving/optimizer/tool_call/customized_api.py (SimpleAPIWrapperFromCallable)
type SimpleAPIWrapperFromCallable struct {
	// callable 可调用函数
	callable APIWrapperFunc
	// fnCallName 函数调用名称
	fnCallName string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// APIWrapperFunc API 包装器函数类型。
// 接收工具信息和输入参数，返回 JSON 响应字符串和状态码。
// 状态码 0 表示成功，12 表示失败（对齐 Python __call__ 返回值）。
type APIWrapperFunc func(tool map[string]any, toolInput map[string]any) (string, int)

// NewSimpleAPIWrapperFromCallable 创建基于可调用函数的 API 包装器。
//
// 对齐 Python: SimpleAPIWrapperFromCallable(tool_callable, name, config)
func NewSimpleAPIWrapperFromCallable(callable APIWrapperFunc, name string) *SimpleAPIWrapperFromCallable {
	return &SimpleAPIWrapperFromCallable{
		callable:   callable,
		fnCallName: name,
	}
}

// Call 执行工具调用。
// 返回 (JSON 响应字符串, 状态码)，状态码 0 表示成功，12 表示失败。
//
// 对齐 Python: SimpleAPIWrapperFromCallable.__call__(tool, tool_input)
//
//	1. 记录调用日志
//	2. 查找已注册的函数，未找到时返回错误（状态码 12）
//	3. 调用函数成功时返回 {'response': output}（状态码 0）
//	4. 调用函数异常时返回 {'error': ..., 'response': ''}（状态码 12）
func (w *SimpleAPIWrapperFromCallable) Call(tool map[string]any, toolInput map[string]any) (string, int) {
	// 对齐 Python: logger.info(f"=== Trying to execute tool: {tool}, tool_input: {tool_input} ===")
	toolName := ""
	if name, ok := tool["name"]; ok {
		toolName = fmt.Sprintf("%v", name)
	}
	logger.Info(logComponent).
		Str("method", "Call").
		Str("tool_name", toolName).
		Str("fn_call_name", w.fnCallName).
		Msg("=== Trying to execute tool ===")

	// 对齐 Python: fn = self.functions.get(self.fn_call_name)
	if w.callable == nil {
		// 对齐 Python: logger.error(f"request invalid, no function '{tool_name}' found")
		logger.Error(logComponent).
			Str("method", "Call").
			Str("tool_name", toolName).
			Str("fn_call_name", w.fnCallName).
			Msg("request invalid, no function found")
		result, _ := json.Marshal(map[string]string{
			"error":    fmt.Sprintf("request invalid, no function '%s' found", toolName),
			"response": "",
		})
		return string(result), 12
	}

	// 对齐 Python: output = fn(params)
	output, err := w.callable(tool, toolInput)
	if err != 0 {
		return output, err
	}
	return output, 0
}

// ──────────────────────────── 非导出函数 ────────────────────────────
