package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxStdoutBuffer stdout 解析缓冲上限（24MB）。
	// Python: _MAX_STDOUT_BUFFER (stdio_client.py L27)
	defaultMaxStdoutBuffer = 24 * 1024 * 1024
	// defaultStdoutReadChunk stdout 读块尺寸（64KB）。
	// Python: _STDOUT_READ_CHUNK (stdio_client.py L28)
	defaultStdoutReadChunk = 65536
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// consumeOneJSON 从缓冲头解析一个完整 JSON 值；不完整返回 (nil, 原缓冲)。
// Python: _consume_one_json (stdio_client.py L65-81)
func consumeOneJSON(buf []byte) (any, []byte) {
	obj, rest, err := consumeOneJSONErr(buf)
	if err != nil {
		panic(err)
	}
	return obj, rest
}

// consumeOneJSONErr consumeOneJSON 的错误变体（readerLoop 使用，超限不 panic）。
func consumeOneJSONErr(buf []byte) (any, []byte, error) {
	s := bytes.TrimLeft(buf, " \t\r\n")
	if len(s) == 0 {
		return nil, buf, nil
	}
	if len(s) > maxStdoutBuffer() {
		return nil, buf, fmt.Errorf("ACP stdout parse buffer exceeds %d bytes", maxStdoutBuffer())
	}
	dec := json.NewDecoder(bytes.NewReader(s))
	var obj any
	if err := dec.Decode(&obj); err != nil {
		return nil, buf, nil
	}
	// 消费长度 = Decode 后的输入偏移（等价 Python raw_decode 的 idx）
	consumed := dec.InputOffset()
	rest := bytes.TrimLeft(s[consumed:], " \t\r\n")
	return obj, rest, nil
}

// isPeerJSONRPCRequest 判定消息是否为对端 JSON-RPC 请求（期待我们回响应）。
// Python: _is_peer_jsonrpc_request (stdio_client.py L84-97)
func isPeerJSONRPCRequest(msg map[string]any) bool {
	if msg["jsonrpc"] != "2.0" {
		return false
	}
	if _, ok := msg["method"].(string); !ok {
		return false
	}
	if msg["id"] == nil {
		return false
	}
	if _, hasResult := msg["result"]; hasResult {
		return false
	}
	if _, hasError := msg["error"]; hasError {
		return false
	}
	return true
}

// extractSessionUpdateText 提取 session/update 通知的文本内容。
// Python: _extract_session_update_text (stdio_client.py L100-114)
func extractSessionUpdateText(msg map[string]any) string {
	if msg["method"] != "session/update" {
		return ""
	}
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		return ""
	}
	update, _ := params["update"].(map[string]any)
	if update == nil {
		return ""
	}
	content, _ := update["content"].(map[string]any)
	if content == nil {
		return ""
	}
	if content["type"] == "text" {
		return strOf(content["text"])
	}
	return ""
}

// jsonrpcErrorMessage 人类可读的 JSON-RPC 错误描述。
// Python: _jsonrpc_error_message (stdio_client.py L48-62)
func jsonrpcErrorMessage(err any) string {
	m, ok := err.(map[string]any)
	if !ok {
		return fmt.Sprintf("ACP JSON-RPC error: %v", err)
	}
	var parts []string
	if code, exists := m["code"]; exists && code != nil {
		parts = append(parts, strOf(code))
	}
	if message, exists := m["message"]; exists && message != nil {
		if s, ok := message.(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	if data, exists := m["data"]; exists && data != nil {
		parts = append(parts, fmt.Sprintf("data=%v", data))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("ACP JSON-RPC error: %v", err)
	}
	return "ACP JSON-RPC error: " + strings.Join(parts, " ")
}

// maxStdoutBuffer 读 env 覆盖（int 解析失败回退默认）。
// Python: _MAX_STDOUT_BUFFER (L27)
func maxStdoutBuffer() int {
	if raw := os.Getenv("ACP_CHAT_MAX_PARSE_BUFFER_BYTES"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			return v
		}
	}
	return defaultMaxStdoutBuffer
}

// stdoutReadChunk 读 env 覆盖。
// Python: _STDOUT_READ_CHUNK (L28)
func stdoutReadChunk() int {
	if raw := os.Getenv("ACP_CHAT_STDOUT_READ_CHUNK"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return defaultStdoutReadChunk
}
