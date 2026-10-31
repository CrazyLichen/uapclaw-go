// ACP 协议响应构造纯函数（10.6.24d Task 9）。
// 对齐 Python jiuwenswarm/common/e2a/acp/protocol.py：
// build_acp_initialize_result / build_acp_session_new_result /
// build_acp_session_list_result / build_acp_prompt_result。

package acp

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/version"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// acpDefaultCapabilities 默认 capabilities（Python 模块级 ACP_DEFAULT_CAPABILITIES
// 在 agent_manager 内部 copy 使用，Go 以函数每次构造等价）。
var acpDefaultCapabilities = buildAcpInitializeResult()

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildAcpInitializeResult 构造 initialize 响应。
// Python: build_acp_initialize_result (protocol.py L8-32)
func BuildAcpInitializeResult() map[string]any {
	return copyAcpResultMap(buildAcpInitializeResult())
}

// AcpDefaultCapabilities 返回默认 capabilities 的拷贝。
// Python: ACP_DEFAULT_CAPABILITIES.copy() (agent_manager.py L181)
func AcpDefaultCapabilities() map[string]any {
	return copyAcpResultMap(acpDefaultCapabilities)
}

// BuildAcpSessionNewResult 构造 session/new 响应。
// Python: build_acp_session_new_result (protocol.py L35-39)
func BuildAcpSessionNewResult(sessionID string) map[string]any {
	return map[string]any{
		"sessionId":     strings.TrimSpace(sessionID),
		"configOptions": []any{},
	}
}

// BuildAcpSessionListResult 构造 session/list 响应（trim + 去重）。
// Python: build_acp_session_list_result (protocol.py L42-51)
func BuildAcpSessionListResult(sessionIDs []string) map[string]any {
	normalized := make([]any, 0, len(sessionIDs))
	seen := map[string]struct{}{}
	for _, sid := range sessionIDs {
		s := strings.TrimSpace(sid)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		normalized = append(normalized, map[string]any{"sessionId": s})
	}
	return map[string]any{"sessions": normalized}
}

// BuildAcpPromptResult 构造 session/prompt 响应。
// Python: build_acp_prompt_result (protocol.py L54-62)
func BuildAcpPromptResult(stopReason string, userMessageID string) map[string]any {
	result := map[string]any{"stopReason": stopReason}
	uid := strings.TrimSpace(userMessageID)
	if uid != "" {
		result["userMessageId"] = uid
	}
	return result
}

// NormalizeJSONRPCID 归一化 JSON-RPC id 为 trim 后的字符串。
//
// Python: 全链一律 `str(data.get("id") or "").strip()`（acp_connect.py L159/L212 /
// app_gateway.py L607）——联合类型 string|number 经 str() 收敛到字符串，再用于
// pending 表键与日志字段。Go 若只做 `v.(string)`，IDE 把 id 归一成 number 时会得到
// 空串，导致 pending 查不到、tool response 被丢弃。
//
// 支持的输入形态：string / float64（encoding/json 默认数字） / json.Number（UseNumber）
// / int / int64 / nil。其余类型返回空串（与 Python str(x or "") 对 None 的语义一致）。
// 数字一律按最短往返格式化（float64(777) → "777"，与 Python str(int) 对齐）。
func NormalizeJSONRPCID(id any) string {
	switch v := id.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return strings.TrimSpace(v.String())
	case nil:
		return ""
	default:
		return ""
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAcpInitializeResult 内部构造（不拷贝，供包内单例初始化）。
func buildAcpInitializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": 1,
		"agentInfo": map[string]any{
			"name":    "jiuwenswarm",
			"title":   "JiuwenSwarm",
			"version": version.Version,
		},
		"agentCapabilities": map[string]any{
			"loadSession": false,
			"promptCapabilities": map[string]any{
				"image":           false,
				"audio":           false,
				"embeddedContext": false,
			},
			"sessionCapabilities": map[string]any{
				"list": map[string]any{},
			},
			"mcpCapabilities": map[string]any{
				"http": false,
				"sse":  false,
			},
		},
		"authMethods": []any{},
	}
}

// copyAcpResultMap 深拷贝两层（值均为字面量，两层足够）。
func copyAcpResultMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if nested, ok := v.(map[string]any); ok {
			out[k] = copyAcpResultMap(nested)
			continue
		}
		out[k] = v
	}
	return out
}
