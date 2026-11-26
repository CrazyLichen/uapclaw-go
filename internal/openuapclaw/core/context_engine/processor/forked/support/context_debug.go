package support

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// ContextDebugDirEnv 调试目录环境变量。
	// 偏差 D-24.2-5：Python 为 OPENJIWEN_CONTEXT_DEBUG_DIR，本仓按品牌改名约定
	// 统一 OPENUAPCLAW_ 前缀。
	// Python: context_debug.py CONTEXT_DEBUG_DIR_ENV
	ContextDebugDirEnv = "OPENUAPCLAW_CONTEXT_DEBUG_DIR"
)

// debugWriteMu JSONL 追加写的进程级互斥。
// Python 依赖 asyncio 单线程串行；Go 侧并发调用需显式锁（平台偏差）。
var debugWriteMu sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// WriteDebugRecord 向处理器的调试日志文件追加一条 JSONL 记录。
// enabled/dumpDir 由调用方显式传入（统一 enable_context_debug 或处理器局部开关，
// 本函数不读配置）；写入失败返回 ""（仅 warn 日志）。
// Python: context_debug.py write_debug_record
func WriteDebugRecord(
	ctx context.Context,
	mc iface.ModelContext,
	processorType string,
	event string,
	enabled bool,
	dumpDir string,
	payload map[string]any,
) string {
	_ = ctx
	if !enabled {
		return ""
	}
	logDir := resolveDebugDir(mc, dumpDir)
	if logDir == "" {
		return ""
	}
	record := map[string]any{
		"timestamp":  float64(time.Now().UnixNano()) / 1e9,
		"event":      event,
		"processor":  processorType,
		"session_id": safeContextValue(mc, "session_id", "unknown_session"),
		"context_id": safeContextValue(mc, "context_id", "unknown_context"),
	}
	for k, v := range payload {
		record[k] = v
	}
	logPath := filepath.Join(logDir, debugLogFileName(processorType))
	debugWriteMu.Lock()
	defer debugWriteMu.Unlock()
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("processor", processorType).
			Err(err).
			Msg("failed to write context debug record")
		return ""
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		logger.Warn(logComponent).
			Str("processor", processorType).
			Err(err).
			Msg("failed to write context debug record")
		return ""
	}
	defer f.Close()
	data, err := json.Marshal(record)
	if err != nil {
		logger.Warn(logComponent).
			Str("processor", processorType).
			Err(err).
			Msg("failed to write context debug record")
		return ""
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		logger.Warn(logComponent).
			Str("processor", processorType).
			Err(err).
			Msg("failed to write context debug record")
		return ""
	}
	return logPath
}

// WriteLLMRequestRecord 落盘即将发送给 LLM 的完整 messages/tools 载荷。
// 刻意 opt-in（载荷可能含用户数据/工具结果/系统提示词原文）；
// 与处理器 trace 同目录，但用固定 llm_request.jsonl 文件名便于独立查看。
// Python: context_debug.py write_llm_request_record
func WriteLLMRequestRecord(
	ctx context.Context,
	mc iface.ModelContext,
	enabled bool,
	dumpDir string,
	model string,
	provider string,
	requestID string,
	sequence int,
	messages any,
	tools any,
	contextWindowTokens int,
	systemMessageCount int,
	contextMessageCount int,
	statistic any,
	usageReport any,
) string {
	messageItems := asDebugSequence(messages)
	toolItems := asDebugSequence(tools)
	return WriteDebugRecord(ctx, mc, "llm_request", "pre_call", enabled, dumpDir, map[string]any{
		"request_id":            requestID,
		"sequence":              sequence,
		"model":                 model,
		"provider":              provider,
		"context_window_tokens": contextWindowTokens,
		"message_count":         len(messageItems),
		"system_message_count":  systemMessageCount,
		"context_message_count": contextMessageCount,
		"tool_count":            len(toolItems),
		"messages":              jsonSafeDebugValue(messageItems),
		"tools":                 jsonSafeDebugValue(toolItems),
		"statistic":             jsonSafeDebugValue(statistic),
		"usage_report":          jsonSafeDebugValue(usageReport),
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// asDebugSequence 归一为列表（nil→空列表，标量→单元素列表）。
// Python: context_debug.py _as_debug_sequence
func asDebugSequence(value any) []any {
	if value == nil {
		return []any{}
	}
	switch v := value.(type) {
	case []any:
		return v
	default:
		return []any{value}
	}
}

// jsonSafeDebugValue 转换 pydantic/自定义对象为 JSON 安全值（保留结构化载荷）。
// Python: context_debug.py _json_safe_debug_value（model_dump 等价 = json.Marshal 往返）
func jsonSafeDebugValue(value any) any {
	switch v := value.(type) {
	case nil, string, int, int64, float64, bool:
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[fmt.Sprintf("%v", k)] = jsonSafeDebugValue(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = jsonSafeDebugValue(item)
		}
		return out
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Sprintf("%v", value)
	}
	return out
}

// resolveDebugDir 目录解析顺序：显式 dump_dir → 环境变量 → workspace 默认。
// Python: context_debug.py _resolve_debug_dir
func resolveDebugDir(mc iface.ModelContext, dumpDir string) string {
	if dumpDir != "" {
		return mustAbs(expandTemplate(dumpDir, mc))
	}
	if envDir := os.Getenv(ContextDebugDirEnv); envDir != "" {
		return mustAbs(expandTemplate(envDir, mc))
	}
	workspaceDir := debugWorkspaceDir(mc)
	if workspaceDir != "" {
		sessionID := safeContextValue(mc, "session_id", "unknown_session")
		return filepath.Join(workspaceDir, "context", sessionID+"_context", "context_debug")
	}
	return mustAbs(filepath.Join("context", "context_debug"))
}

// debugLogFileName 处理器类型 → 安全文件名（{safe}.jsonl）。
// Python: context_debug.py _debug_log_file_name
func debugLogFileName(processorType string) string {
	safe := safeFilenamePart(processorType)
	if safe == "" {
		safe = "processor"
	}
	return safe + ".jsonl"
}

// expandTemplate 展开 {session_id}/{context_id} 模板占位。
// Python: context_debug.py _expand_template
func expandTemplate(path string, mc iface.ModelContext) string {
	if !strings.Contains(path, "{session_id}") && !strings.Contains(path, "{context_id}") {
		return path
	}
	path = strings.ReplaceAll(path, "{session_id}", safeFilenamePart(safeContextValue(mc, "session_id", "unknown_session")))
	path = strings.ReplaceAll(path, "{context_id}", safeFilenamePart(safeContextValue(mc, "context_id", "unknown_context")))
	return path
}

// debugWorkspaceDir 取 workspace_dir 绝对路径（失败回空串）。
// Python: context_debug.py _workspace_dir
func debugWorkspaceDir(mc iface.ModelContext) string {
	if mc == nil {
		return ""
	}
	dir := mc.WorkspaceDir()
	if dir == "" {
		return ""
	}
	return mustAbs(dir)
}

// safeContextValue 反射等价的上下文取值（session_id/context_id）。
// Python: context_debug.py _safe_context_value（getattr 可调用协议 → Go 显式断言）
func safeContextValue(mc iface.ModelContext, method string, fallback string) string {
	if mc == nil {
		return fallback
	}
	var value string
	switch method {
	case "session_id":
		value = mc.SessionID()
	case "context_id":
		value = mc.ContextID()
	default:
		return fallback
	}
	if value == "" {
		return fallback
	}
	return value
}

// safeFilenamePart 值 → 安全文件名片段（非字母数字替换为 -，截断 80）。
// Python: context_debug.py _safe_filename_part
func safeFilenamePart(value string) string {
	re := regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
	safe := strings.Trim(re.ReplaceAllString(value, "-"), "-")
	if len(safe) > 80 {
		safe = safe[:80]
	}
	if safe == "" {
		return "unknown"
	}
	return safe
}

// mustAbs 转绝对路径（失败时原样返回）
func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
