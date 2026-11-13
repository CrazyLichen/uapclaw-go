package renderer

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/todo_render"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/ui/tool_display"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// 流式渲染器——把 OutputSchema 块映射成终端输出。
//
// 支持八种块类型：
//
//	llm_output      — LLM 生成文本（累计进结果）
//	llm_reasoning   — 推理 / 思考（默认隐藏）
//	answer          — 最终答案（跳过避免重复）
//	__interaction__ — HITL 交互请求
//	message         — 系统 / 状态消息
//	tool_call       — 工具开始（● ToolName）
//	tool_result     — 工具结果（⎿ 摘要）
//	todo.updated    — todo 清单变化（复选框渲染）
//
// Python: openjiuwen/harness/cli/ui/renderer.py（420 行）
//
// Rich Console → Go 用 io.Writer + ANSI 码（下游 runner/repl 注入）。

// ──────────────────────────── 常量 ────────────────────────────

// 块类型常量（与 SDK OutputSchema.type 对齐）。
// Python: renderer.py:29-38
const (
	ChunkLLMOutput        = "llm_output"
	ChunkLLMReasoning     = "llm_reasoning"
	ChunkAnswer           = "answer"
	ChunkInteraction      = "__interaction__"
	ChunkMessage          = "message"
	ChunkToolCall         = "tool_call"
	ChunkToolResult       = "tool_result"
	ChunkTodoUpdated      = "todo.updated"
	ChunkControllerOutput = "controller_output"
)

// ANSI 前缀（Rich 样式的最小等价物）。
const (
	ansiGreen     = "\033[92m"
	ansiReset     = "\033[0m"
	ansiDimPrefix = "\033[2m"
	ansiRedPrefix = "\033[31m"
)

// controllerTextRe 从 str(payload) 里抓 text="..." 段。
// Python: renderer.py:126 re.findall(r'text="(.*?)"', raw)
var controllerTextRe = regexp.MustCompile(`text="(.*?)"`)

// todoMessageRe 从 dict repr 里抓 message 值。
// Python: renderer.py:145-150 re.search(r"['\"]message['\"]\s*:\s*['\"](.+?)['\"]}\s*$", ..., DOTALL)
var todoMessageRe = regexp.MustCompile(`(?s)['"]message['"]\s*:\s*['"](.+?)['"]}\s*$`)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingInteraction 等待用户输入的交互请求。
// Python: renderer.py:40-55 PendingInteraction
type PendingInteraction struct {
	// InteractionID 必须作为 InteractiveInput 键的工具调用 ID
	InteractionID string
	// Request 交互的原始 payload
	Request any
}

// RenderResult renderStream 的返回值。
// Python: renderer.py:57-68 RenderResult
type RenderResult struct {
	// Text 累计的 LLM 输出文本
	Text string
	// PendingInteractions 需要 user 解答后 agent 才能继续的交互
	PendingInteractions []PendingInteraction
}

// Options renderStream 的可选参数。
// Python: renderer.py:299-306 的 keyword-only 参数
type Options struct {
	// OnInteraction __interaction__ 块的回调：收 (interactionID, question)，
	// 返回 user 的答案串。nil 表示跳过交互。
	OnInteraction func(interactionID string, question any) string
	// ShowReasoning true 时以暗样式显示推理块；默认 false（隐藏）
	ShowReasoning bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractContent 从块的 payload 提取文本。
// Python: renderer.py:70-81 _extract_content
func ExtractContent(chunk stream.OutputSchema) string {
	switch typed := chunk.Payload.(type) {
	case map[string]any:
		if content, ok := typed["content"].(string); ok && content != "" {
			return content
		}
		if output, ok := typed["output"].(string); ok {
			return output
		}
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

// ExtractControllerOutputError 提取可读的控制器任务失败消息。
// Python: renderer.py:83-109 _extract_controller_output_error
func ExtractControllerOutputError(payload any) string {
	if mapping, isMap := payload.(map[string]any); isMap {
		payloadType := strings.ToLower(fmt.Sprint(mapping["type"]))
		if strings.Contains(payloadType, "task_failed") {
			var texts []string
			if data, isList := mapping["data"].([]any); isList {
				for _, item := range data {
					if entry, isEntry := item.(map[string]any); isEntry {
						text := strings.TrimSpace(fmt.Sprint(entry["text"]))
						if text != "" && text != "<nil>" {
							texts = append(texts, text)
						}
					}
				}
			}
			if len(texts) > 0 {
				return strings.Join(texts, "\n")
			}
		}
		return ""
	}
	raw := fmt.Sprint(payload)
	if !strings.Contains(strings.ToLower(raw), "task_failed") {
		return ""
	}
	matches := controllerTextRe.FindAllStringSubmatch(raw, -1)
	if len(matches) > 0 {
		var lines []string
		for _, match := range matches {
			lines = append(lines, match[1])
		}
		return strings.Join(lines, "\n")
	}
	return raw
}

// ExtractTodoMessage 从 Python dict repr 里提取 message 值。
// Python: renderer.py:111-133 _extract_todo_message
//
// todo 工具返回 {'message': 'text...'}；str() 后变成 repr 串。
// 本助手提取 message 值供兜底展示。
func ExtractTodoMessage(toolResult string) string {
	match := todoMessageRe.FindStringSubmatch(toolResult)
	if match != nil {
		message := strings.ReplaceAll(match[1], "\\n", "\n")
		firstLine := strings.TrimSpace(strings.SplitN(message, "\n", 2)[0])
		if firstLine != "" {
			return firstLine
		}
	}
	return ""
}

// RenderToolCall 以 Claude Code 风格渲染一次工具调用。
// Python: renderer.py:135-151 _render_tool_call
//
// 示例：● Read(src/main.py)
func RenderToolCall(out io.Writer, payload map[string]any) {
	toolName := stringOr(fmt.Sprint(payload["tool_name"]), "")
	toolArgs := payload["tool_args"]
	displayName := tool_display.GetDisplayName(toolName)
	argsStr := tool_display.FormatToolArgs(toolName, toolArgs)
	if argsStr != "" {
		fmt.Fprintf(out, "● %s(%s)\n", displayName, argsStr)
	} else {
		fmt.Fprintf(out, "● %s\n", displayName)
	}
}

// RenderToolResult 以 Claude Code 风格渲染一次工具结果。
// Python: renderer.py:153-207 _render_tool_result
//
// 示例：⎿  Read 42 lines。todo 工具走复选框渲染，返回更新后的清单。
func RenderToolResult(out io.Writer, payload map[string]any,
	todoItems []map[string]any) []map[string]any {
	toolName := fmt.Sprint(payload["tool_name"])
	toolResult := fmt.Sprint(payload["tool_result"])
	toolArgs := payload["tool_args"]

	// 任务列表工具走特殊复选框渲染
	if tool_display.TodoTools()[toolName] {
		items := todo_render.ParseTodoResult(toolResult)
		if items != nil {
			for _, line := range todo_render.RenderTodoList(items) {
				fmt.Fprintln(out, line)
			}
			fmt.Fprintln(out) // 之后空行
			return items
		}
		if toolName == "todo_modify" && todoItems != nil {
			items = todo_render.ApplyTodoModifyArgs(todoItems,
				todo_render.ParseTodoToolArgs(toolArgs))
			if items != nil {
				for _, line := range todo_render.RenderTodoList(items) {
					fmt.Fprintln(out, line)
				}
				fmt.Fprintln(out)
				return items
			}
		}
		// 兜底：解析失败时展示原始 message
		if message := ExtractTodoMessage(toolResult); message != "" {
			fmt.Fprintf(out, "%s  ⎿  %s%s\n", ansiDimPrefix, message, ansiReset)
			fmt.Fprintln(out)
			return todoItems
		}
	}

	// 展示模型读到的文本；tool_result 只是旧式字符串形态，
	// 为不发 rendered_result 的生产者保留。
	displayText := toolResult
	if rendered, ok := payload["rendered_result"].(string); ok && rendered != "" {
		displayText = rendered
	}
	summary := tool_display.FormatToolResult(toolName, displayText, toolArgs, payload)
	if summary != "" {
		fmt.Fprintf(out, "%s  ⎿  %s%s\n", ansiDimPrefix, summary, ansiReset)
	}
	// 写入 / 编辑内容预览
	if toolName == "write_file" && displayText != "" {
		if preview := tool_display.FormatWritePreview(displayText); preview != "" {
			fmt.Fprintf(out, "%s%s%s\n", ansiDimPrefix, preview, ansiReset)
		}
	}
	fmt.Fprintln(out) // 工具结果之后的空行
	return todoItems
}

// RenderStream 消费 OutputSchema 流并渲染到终端。
// Python: renderer.py:209-419 render_stream
//
// 返回累计的响应文本与需要 user 解答的挂起交互。
func RenderStream(out io.Writer, chunks []stream.OutputSchema, opts Options) RenderResult {
	var resultParts []string
	chunkCount := 0
	hasLLMOutput := false
	inLLMOutput := false
	visibleChunkSeen := false
	seenTypes := map[string]bool{}
	var pending []PendingInteraction
	var todoItems []map[string]any

	for _, chunk := range chunks {
		chunkCount++
		chunkType := chunk.Type
		if chunkType != "" {
			seenTypes[chunkType] = true
		}
		if chunkType != ChunkLLMOutput && inLLMOutput {
			fmt.Fprint(out, "\n")
			inLLMOutput = false
		}

		switch chunkType {
		case ChunkLLMOutput:
			text := ExtractContent(chunk)
			if text != "" {
				// 新一轮响应的第一个 llm_output token 打绿色圆点前缀
				if !inLLMOutput {
					fmt.Fprint(out, ansiGreen+"● "+ansiReset)
					inLLMOutput = true
				}
				hasLLMOutput = true
				resultParts = append(resultParts, text)
				fmt.Fprint(out, text)
				visibleChunkSeen = true
			}
		case ChunkAnswer:
			// answer 块与 llm_output 内容重复；只在没收到 llm_output 时兜底
			if !hasLLMOutput {
				text := ExtractContent(chunk)
				if text != "" {
					resultParts = append(resultParts, text)
					fmt.Fprint(out, text)
					visibleChunkSeen = true
				}
			}
		case ChunkLLMReasoning:
			// 推理默认隐藏；--verbose 或 showReasoning 才显示
			if opts.ShowReasoning {
				text := ExtractContent(chunk)
				if text != "" {
					fmt.Fprintf(out, "%s%s%s", ansiDimPrefix, text, ansiReset)
				}
			}
		case ChunkMessage:
			text := ExtractContent(chunk)
			if text != "" {
				fmt.Fprintf(out, "%s  ⚙ %s%s\n", ansiDimPrefix, text, ansiReset)
				visibleChunkSeen = true
			}
		case ChunkToolCall:
			payload, isMap := chunk.Payload.(map[string]any)
			if !isMap {
				payload = map[string]any{}
			}
			RenderToolCall(out, payload)
			visibleChunkSeen = true
		case ChunkToolResult:
			payload, isMap := chunk.Payload.(map[string]any)
			if !isMap {
				payload = map[string]any{}
			}
			todoItems = RenderToolResult(out, payload, todoItems)
			visibleChunkSeen = true
		case ChunkTodoUpdated:
			payload, isMap := chunk.Payload.(map[string]any)
			if !isMap {
				payload = map[string]any{}
			}
			items := todo_render.ParseTodoResult(fmt.Sprint(payload["items"]))
			if items != nil {
				todoItems = items
				for _, line := range todo_render.RenderTodoList(items) {
					fmt.Fprintln(out, line)
				}
				visibleChunkSeen = true
			}
		case ChunkControllerOutput:
			if errorText := ExtractControllerOutputError(chunk.Payload); errorText != "" {
				fmt.Fprintf(out, "%s✗ %s%s\n", ansiRedPrefix, errorText, ansiReset)
				visibleChunkSeen = true
			}
		case ChunkInteraction:
			interactionID := "unknown"
			value := any(nil)
			if payload, isMap := chunk.Payload.(map[string]any); isMap {
				if id, ok := payload["interaction_id"].(string); ok && id != "" {
					interactionID = id
				}
				value = payload
			} else if output, isOutput := chunk.Payload.(interface{ GetID() string }); isOutput {
				interactionID = output.GetID()
				value = chunk.Payload
			} else {
				value = chunk.Payload
			}
			// 把交互问题渲染到终端
			if opts.OnInteraction != nil {
				opts.OnInteraction(interactionID, value)
			}
			// 收集供恢复——REPL 必须经 InteractiveInput 送回
			pending = append(pending, PendingInteraction{
				InteractionID: interactionID,
				Request:       value,
			})
			visibleChunkSeen = true
		}
		// 未知的类型静默跳过（controller_output 等）
	}

	// 流式输出后确保尾随换行
	if inLLMOutput {
		fmt.Fprint(out, "\n")
	}

	// 完整性警告
	result := strings.Join(resultParts, "")
	if chunkCount == 0 {
		fmt.Fprintf(out, "%s⚠ No output received. Check your API configuration.%s\n",
			ansiDimPrefix, ansiReset)
	} else if !visibleChunkSeen && len(pending) == 0 {
		types := make([]string, 0, len(seenTypes))
		for chunkType := range seenTypes {
			types = append(types, chunkType)
		}
		sort.Strings(types)
		label := strings.Join(types, ", ")
		if label == "" {
			label = "unknown"
		}
		fmt.Fprintf(out, "%s⚠ No visible output received. Chunk types: %s%s\n",
			ansiDimPrefix, label, ansiReset)
	}
	return RenderResult{Text: result, PendingInteractions: pending}
}

// stringOr 空串或 "<nil>" 回退。
func stringOr(value, fallback string) string {
	if value == "" || value == "<nil>" {
		return fallback
	}
	return value
}
