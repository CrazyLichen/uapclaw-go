// fakeacp 假 ACP 对端（集成测试用）：逐行读 JSON-RPC，按 method 回响应。
// os.Args[1] 选行为模式：echo | error | permission | fsread。
// 对应 Python tests/system_tests/test_acp_stdio_client_st.py L22-159 的 inline fake agent。
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// main 逐行处理 stdin 上的 JSON-RPC 请求。
func main() {
	mode := "echo"
	if len(os.Args) > 1 {
		mode = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := bufio.NewWriter(os.Stdout)

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		method, _ := msg["method"].(string)
		id := msg["id"]

		switch method {
		case "initialize":
			writeMsg(out, map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"protocolVersion": 1},
			})
		case "session/new":
			// sessionId 带 mode，便于断言 profile→进程→会话不串台
			writeMsg(out, map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"sessionId": "fake-" + mode + "-sess"},
			})
		case "session/prompt":
			handlePrompt(out, in, mode, id, promptText(msg))
		default:
			if id != nil {
				writeMsg(out, map[string]any{
					"jsonrpc": "2.0", "id": id,
					"error": map[string]any{"code": -32601, "message": "method not found: " + method},
				})
			}
		}
	}
}

// writeMsg 写一条单行 compact JSON 并 flush。
func writeMsg(w *bufio.Writer, obj map[string]any) {
	b, err := json.Marshal(obj)
	if err != nil {
		return
	}
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}

// writeUpdate 发一条 session/update（agent_message_chunk + text）。
func writeUpdate(w *bufio.Writer, mode, text string) {
	writeMsg(w, map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": "fake-" + mode + "-sess",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": text},
			},
		},
	})
}

// promptText 取 params.prompt[0].text。
func promptText(msg map[string]any) string {
	params, _ := msg["params"].(map[string]any)
	prompt, _ := params["prompt"].([]any)
	if len(prompt) == 0 {
		return ""
	}
	first, _ := prompt[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

// handlePrompt 按 mode 处理 session/prompt。
func handlePrompt(out *bufio.Writer, in *bufio.Scanner, mode string, id any, text string) {
	switch mode {
	case "error":
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32603, "message": "agent failed"},
		})
		return
	case "echo":
		writeUpdate(out, mode, "echo:"+text)
	default:
		writeUpdate(out, mode, "mode:"+text)
	}
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{"stopReason": "end_turn"},
	})
}
