// fakecodebuddy 假 CodeBuddy CLI：按脚本回放 stream-json 线帧。
//
// 15.22（真进程映射验证）与 15.17（失败恢复）两章复用。故障注入开关：
//
//	--script path   JSONL 帧序列文件（收到 user 帧后逐帧回放）
//	--crash-after N 写 N 帧后 exit 1（崩溃恢复）
//	--auth-fail     先发 init，再发 result error，stderr 写鉴权文案
//	--hang          读完 stdin 后挂起不回帧（超时用例）
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

// initFrame 标准会话初始化帧。
var initFrame = map[string]any{
	"type":       "system",
	"subtype":    "init",
	"session_id": "fake-session-1",
	"model":      "fake-model",
}

// errorResult 鉴权失败结果帧。
var errorResult = map[string]any{
	"type":       "result",
	"subtype":    "error_during_execution",
	"result":     "authentication failed: invalid api key",
	"session_id": "fake-session-1",
	"is_error":   true,
}

func main() {
	scriptPath := flag.String("script", "", "JSONL 帧序列文件路径")
	crashAfter := flag.Int("crash-after", 0, "写 N 帧后 exit 1（0 = 不注入）")
	authFail := flag.Bool("auth-fail", false, "回放 init + result error（鉴权失败）")
	hang := flag.Bool("hang", false, "读完 stdin 后挂起")
	// --echo-mcp：解析 harness 传入的 --mcp-config 内联 JSON，把 server 名回显
	// 进 result 文本（端到端验证 context.McpServers → argv → CLI 进程链路）
	echoMcp := flag.Bool("echo-mcp", false, "把 --mcp-config 里的 server 名回显进 result")
	mcpConfig := flag.String("mcp-config", "", "CLI MCP 配置（JSON 或文件路径）")
	// 真 CLI 启动参数面（harness 固定追加，声明以容忍解析）
	_ = flag.Bool("p", false, "")
	_ = flag.String("output-format", "text", "")
	_ = flag.String("input-format", "text", "")
	_ = flag.Bool("no-session-persistence", false, "")
	_ = flag.String("model", "", "")
	_ = flag.String("permission-mode", "", "")
	flag.Parse()

	stdin := bufio.NewReader(os.Stdin)
	stdout := bufio.NewWriter(os.Stdout)
	stderr := bufio.NewWriter(os.Stderr)

	// 步骤 1：立即发 init 帧（真 CLI 启动即发，不等输入）
	writeFrame(stdout, initFrame)

	// 读取输入帧（内容不解析语义，仅等 user 帧到达）
	decoder := json.NewDecoder(stdin)
	var input map[string]any
	_ = decoder.Decode(&input)

	if *hang {
		// 挂起：永不回帧（由测试侧超时触发）
		select {}
	}

	if *authFail {
		// 步骤 2a：鉴权失败路径
		_, _ = stderr.WriteString("codebuddy: authentication failed: invalid api key\n")
		_ = stderr.Flush()
		writeFrame(stdout, errorResult)
		_ = stdout.Flush()
		os.Exit(1)
	}

	// 步骤 2b：按脚本回放帧序列
	// --echo-mcp：把 --mcp-config 里的 server 名注入 result 文本
	echoSuffix := ""
	if *echoMcp && *mcpConfig != "" {
		echoSuffix = "\n[mcp-servers: " + mcpServerNames(*mcpConfig) + "]"
	}
	written := 0
	if *scriptPath != "" {
		script, err := os.ReadFile(*scriptPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakecodebuddy: read script failed:", err)
			os.Exit(2)
		}
		for _, line := range splitLines(script) {
			var frame map[string]any
			if err := json.Unmarshal([]byte(line), &frame); err != nil {
				fmt.Fprintln(os.Stderr, "fakecodebuddy: bad script line:", err)
				os.Exit(2)
			}
			if echoSuffix != "" {
				if text, ok := frame["result"].(string); ok {
					frame["result"] = text + echoSuffix
				}
				// assistant 帧的 text 块同步回显（集成断言拼的是 answer 文本）
				if message, ok := frame["message"].(map[string]any); ok {
					if blocks, ok := message["content"].([]any); ok {
						for _, block := range blocks {
							if entry, ok := block.(map[string]any); ok && entry["type"] == "text" {
								if text, ok := entry["text"].(string); ok {
									entry["text"] = text + echoSuffix
								}
							}
						}
					}
				}
			}
			writeFrame(stdout, frame)
			written++
			_ = stdout.Flush()
			if *crashAfter > 0 && written >= *crashAfter {
				os.Exit(1)
			}
		}
	}
	_ = stdout.Flush()
}

// mcpServerNames 从 --mcp-config 内联 JSON 提取 server 名（逗号连接）。
// 非 JSON 载荷（文件路径形态）返回原串。
func mcpServerNames(payload string) string {
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil || doc.McpServers == nil {
		return payload
	}
	names := make([]string, 0, len(doc.McpServers))
	for name := range doc.McpServers {
		names = append(names, name)
	}
	// map 迭代序不稳定；测试只断言包含，不需稳定序
	return strings.Join(names, ",")
}

// writeFrame 写一帧 JSONL。
func writeFrame(w *bufio.Writer, frame map[string]any) {
	encoded, err := json.Marshal(frame)
	if err != nil {
		return
	}
	_, _ = w.Write(encoded)
	_, _ = w.WriteRune('\n')
	_ = w.Flush()
}

// splitLines 按行拆分脚本内容（忽略空行）。
func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for index, b := range data {
		if b == '\n' {
			if line := string(data[start:index]); line != "" {
				lines = append(lines, line)
			}
			start = index + 1
		}
	}
	if line := string(data[start:]); line != "" {
		lines = append(lines, line)
	}
	return lines
}
