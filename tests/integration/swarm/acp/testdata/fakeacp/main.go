// fakeacp 假 ACP 对端（集成测试用）：逐行读 JSON-RPC，按 method 回响应。
// os.Args[1] 选行为模式：echo | error | permission | fsread。
// 对应 Python tests/system_tests/test_acp_stdio_client_st.py L22-159 的 inline fake agent。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	if mode == "fsread" {
		content, errText := fsReadThenWrite(out, in, mode)
		writeUpdate(out, mode, content+errText)
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]any{"stopReason": "end_turn"},
		})
		return
	}

	if mode == "permission" {
		// Go 增强：把收到的 outcome/optionId 回显进文本，才能区分批准/取消
		outcome, optionID := requestPermission(out, in, mode)
		writeUpdate(out, mode, "after-perm:"+text+":"+outcome+":"+optionID)
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]any{"stopReason": "end_turn"},
		})
		return
	}

	switch mode {
	case "error":
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32603, "message": "agent failed"},
		})
		return
	case "echo":
		// 回显 pid：供“缓存复用 / new_session 重启”用例区分是否同一子进程
		writeUpdate(out, mode, fmt.Sprintf("echo:%s:pid=%d", text, os.Getpid()))
	default:
		writeUpdate(out, mode, "mode:"+text)
	}
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{"stopReason": "end_turn"},
	})
}

// requestPermission 发 session/request_permission 并等客户端响应，返回 outcome 与 optionId。
func requestPermission(out *bufio.Writer, in *bufio.Scanner, mode string) (string, string) {
	const permID = 900
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": permID, "method": "session/request_permission",
		"params": map[string]any{
			"sessionId": "fake-" + mode + "-sess",
			"toolCall":  map[string]any{"toolCallId": "tc-1", "title": "run test"},
			"options": []any{
				map[string]any{"optionId": "allow-once", "kind": "allow"},
				map[string]any{"optionId": "reject-once", "kind": "reject"},
			},
		},
	})
	reply := waitResponse(in, permID)
	if reply == nil {
		return "no-reply", ""
	}
	if errObj, ok := reply["error"]; ok {
		return "error-reply", fmt.Sprintf("%v", errObj)
	}
	result, _ := reply["result"].(map[string]any)
	outcome, _ := result["outcome"].(map[string]any)
	kind, _ := outcome["outcome"].(string)
	optionID, _ := outcome["optionId"].(string)
	return kind, optionID
}

// waitResponse 读 stdin 直到拿到指定 id 的响应（跳过通知/其他请求）。
func waitResponse(in *bufio.Scanner, wantID int) map[string]any {
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if fmt.Sprintf("%v", msg["id"]) == fmt.Sprintf("%d", wantID) {
			return msg
		}
	}
	return nil
}

// fsReadThenWrite 发 fs/read_text_file（line/limit 切片）+ fs/write_text_file，返回读到的内容。
func fsReadThenWrite(out *bufio.Writer, in *bufio.Scanner, mode string) (string, string) {
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": 901, "method": "fs/read_text_file",
		"params": map[string]any{"path": "in_root.txt", "line": 1, "limit": 2},
	})
	reply := waitResponse(in, 901)
	if reply == nil || reply["error"] != nil {
		return "", "|read-failed"
	}
	result, _ := reply["result"].(map[string]any)
	content, _ := result["content"].(string)

	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": 902, "method": "fs/write_text_file",
		"params": map[string]any{"path": "written.txt", "content": "hello-acp"},
	})
	_ = waitResponse(in, 902)
	return content, ""
}
