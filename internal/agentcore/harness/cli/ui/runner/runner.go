package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/agent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/renderer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// 非交互运行模式。
//
// 三种输出格式：
//
//	text        — 渲染到终端（与 REPL 相同）
//	json        — 单个 JSON 对象（result + 元数据）
//	stream-json — JSONL，每块一行
//
// Python: openjiuwen/harness/cli/ui/runner.py（146 行）

// ──────────────────────────── 常量 ────────────────────────────

// 输出格式取值。
const (
	// OutputFormatText 终端渲染
	OutputFormatText = "text"
	// OutputFormatJSON 单 JSON 对象
	OutputFormatJSON = "json"
	// OutputFormatStreamJSON JSONL 流
	OutputFormatStreamJSON = "stream-json"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RunOnce 执行一次非交互查询。
// Python: runner.py:36-77 run_once
//
// 返回退出码：成功 0，错误 1。
func RunOnce(cfg agent.CLIConfig, prompt string, outputFormat string) int {
	backend, err := agent.CreateBackend(cfg)
	if err != nil {
		PrintError(err)
		return 1
	}
	startCtx := contextBackground()
	if err := backend.Start(startCtx); err != nil {
		PrintError(err)
		_ = backend.Stop(contextBackground())
		return 1
	}
	ch, streamErr := backend.RunStreaming(contextBackground(), prompt, nil)
	if streamErr != nil {
		PrintError(streamErr)
		_ = backend.Stop(contextBackground())
		return 1
	}
	var chunks []stream.OutputSchema
	for schema := range ch {
		chunks = append(chunks, schema)
	}

	var exitCode int
	switch outputFormat {
	case OutputFormatText:
		exitCode = outputText(chunks)
	case OutputFormatJSON:
		exitCode = outputJSON(chunks, cfg)
	case OutputFormatStreamJSON:
		exitCode = outputStreamJSON(chunks)
	default:
		PrintError(fmt.Errorf("Unknown output format: %s", outputFormat))
		exitCode = 1
	}
	_ = backend.Stop(contextBackground())
	return exitCode
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// outputText 把流渲染到终端（与 REPL 相同）。
// Python: runner.py:79-84 _output_text
func outputText(chunks []stream.OutputSchema) int {
	renderer.RenderStream(os.Stdout, chunks, renderer.Options{})
	return 0
}

// outputJSON 收集全部块并输出单个 JSON 对象。
// Python: runner.py:86-110 _output_json
//
// result 取 llm_output 累计；无 llm_output 时用 answer 兜底。
func outputJSON(chunks []stream.OutputSchema, cfg agent.CLIConfig) int {
	var resultText strings.Builder
	chunkCount := 0
	hasLLMOutput := false
	for _, chunk := range chunks {
		chunkCount++
		switch chunk.Type {
		case renderer.ChunkLLMOutput:
			hasLLMOutput = true
			resultText.WriteString(renderer.ExtractContent(chunk))
		case renderer.ChunkAnswer:
			if !hasLLMOutput {
				resultText.WriteString(renderer.ExtractContent(chunk))
			}
		}
	}
	output := map[string]any{
		"result": resultText.String(),
		"chunks": chunkCount,
		"model":  cfg.Model,
	}
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		PrintError(err)
		return 1
	}
	writeTerminal(string(encoded))
	writeTerminal("\n")
	return 0
}

// outputStreamJSON 每块输出一行 JSONL。
// Python: runner.py:112-128 _output_stream_json
func outputStreamJSON(chunks []stream.OutputSchema) int {
	for _, chunk := range chunks {
		payload := chunk.Payload
		var payloadValue any
		switch typed := payload.(type) {
		case string, map[string]any:
			payloadValue = typed
		default:
			payloadValue = fmt.Sprint(typed)
		}
		line := map[string]any{
			"type":    chunk.Type,
			"index":   chunk.Index,
			"payload": payloadValue,
		}
		encoded, err := json.Marshal(line)
		if err != nil {
			continue
		}
		writeTerminal(string(encoded))
		writeTerminal("\n")
	}
	return 0
}

// writeTerminal 把文本直接写 stdout（CLI 用户输出）。
// Python: runner.py:24-26 _write_terminal
func writeTerminal(text string) {
	fmt.Fprint(os.Stdout, text)
}

// contextBackground 便捷上下文。
func contextBackground() context.Context { return context.Background() }

// containsAny 任一子串命中。
func containsAny(message string, substrings ...string) bool {
	for _, substring := range substrings {
		if strings.Contains(message, substring) {
			return true
		}
	}
	return false
}

// PrintError 把 API 错误转成用户友好消息。
// Python: runner.py:130-150 _print_error
func PrintError(err error) {
	message := err.Error()
	var hint string
	switch {
	case containsAny(message, "rate_limit", "429"):
		hint = "Rate limited. Please try again later."
	case containsAny(message, "authentication", "401"):
		hint = "API Key invalid. Check OPENJIUWEN_API_KEY."
	case containsAny(message, "too long", "context_length"):
		hint = "Context too long. Use /compact to trim history."
	case containsAny(message, "timeout"):
		hint = "Request timed out. Check your network."
	default:
		hint = fmt.Sprintf("Error: %s", err)
	}
	// Python: Console(stderr=True) + [red]✗ ...[/red]
	fmt.Fprintf(os.Stderr, "\033[31m✗ %s\033[0m\n", hint)
}
