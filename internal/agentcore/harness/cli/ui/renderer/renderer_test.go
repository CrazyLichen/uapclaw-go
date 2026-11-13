package renderer

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// chunk 构造辅助。
func mkChunk(chunkType string, payload any) stream.OutputSchema {
	return stream.OutputSchema{Type: chunkType, Index: 0, Payload: payload}
}

// textChunk 文本载荷。
func textChunk(chunkType, text string) stream.OutputSchema {
	return mkChunk(chunkType, map[string]any{"content": text})
}

// TestExtractContent_三形态 对齐 renderer.py:70-81。
func TestExtractContent_三形态(t *testing.T) {
	assert.Equal(t, "文本", ExtractContent(textChunk("t", "文本")))
	assert.Equal(t, "直接串", ExtractContent(mkChunk("t", "直接串")))
	assert.Equal(t, "", ExtractContent(mkChunk("t", map[string]any{})))
	// output 键回退
	assert.Equal(t, "输出", ExtractContent(mkChunk("t", map[string]any{"output": "输出"})))
}

// TestExtractControllerOutputError_taskFailed 对齐 renderer.py:83-109。
func TestExtractControllerOutputError_taskFailed(t *testing.T) {
	payload := map[string]any{
		"type": "task_failed",
		"data": []any{
			map[string]any{"text": "  失败原因  "},
			map[string]any{"text": "第二行"},
		},
	}
	got := ExtractControllerOutputError(payload)
	assert.Equal(t, "失败原因\n第二行", got)
	// 非 task_failed
	assert.Equal(t, "", ExtractControllerOutputError(map[string]any{"type": "task_ok"}))
	// 字符串形态
	assert.Equal(t, "boom", ExtractControllerOutputError(`task_failed text="boom"`))
	// 非 map 非 task_failed 串
	assert.Equal(t, "", ExtractControllerOutputError("plain text"))
}

// TestExtractTodoMessage_repr 提取 message。
func TestExtractTodoMessage_repr(t *testing.T) {
	got := ExtractTodoMessage(`{'message': '成功创建 3 个任务\n更多'}`)
	assert.Equal(t, "成功创建 3 个任务", got)
	// 双引号形态
	assert.Equal(t, "任务", ExtractTodoMessage(`{"message": "任务"}`))
	assert.Equal(t, "", ExtractTodoMessage("没有 message"))
}

// TestRenderToolCall_带参与无参 对齐 renderer.py:135-151。
func TestRenderToolCall_带参与无参(t *testing.T) {
	var out bytes.Buffer
	RenderToolCall(&out, map[string]any{
		"tool_name": "read_file",
		"tool_args": map[string]any{"file_path": "/x/y.py"},
	})
	assert.Equal(t, "● Read(/x/y.py)\n", out.String())

	out.Reset()
	RenderToolCall(&out, map[string]any{"tool_name": "todo_create", "tool_args": map[string]any{}})
	assert.Equal(t, "● TodoWrite\n", out.String())
}

// TestRenderToolResult_普通与todo 对齐 renderer.py:153-207。
func TestRenderToolResult_普通与todo(t *testing.T) {
	var out bytes.Buffer
	RenderToolResult(&out, map[string]any{
		"tool_name": "bash", "tool_result": "ok",
	}, nil)
	assert.Contains(t, out.String(), "⎿  ok")

	// todo 工具：复选框渲染
	out.Reset()
	items := RenderToolResult(&out, map[string]any{
		"tool_name":   "todo_create",
		"tool_result": `[{"content":"任务","status":"pending"}]`,
	}, nil)
	require.Len(t, items, 1)
	assert.Contains(t, out.String(), "☐ 任务")

	// write_file 预览
	out.Reset()
	RenderToolResult(&out, map[string]any{
		"tool_name": "write_file", "tool_result": "l1\nl2", "tool_args": map[string]any{"file_path": "/p"},
	}, nil)
	assert.Contains(t, out.String(), "     1 l1")
}

// TestRenderStream_llmOutput与answer 兜底与累计。
func TestRenderStream_llmOutput与answer(t *testing.T) {
	var out bytes.Buffer
	chunks := []stream.OutputSchema{
		textChunk(ChunkLLMOutput, "你好"),
		textChunk(ChunkLLMOutput, "，世界"),
		textChunk(ChunkAnswer, "不应显示"), // 有 llm_output 时跳过
	}
	result := RenderStream(&out, chunks, Options{})
	assert.Equal(t, "你好，世界", result.Text)
	assert.Contains(t, out.String(), "● ")
	assert.Contains(t, out.String(), "你好，世界")
	assert.NotContains(t, out.String(), "不应显示")
}

// TestRenderStream_answer兜底 无 llm_output 时 answer 兜底。
func TestRenderStream_answer兜底(t *testing.T) {
	var out bytes.Buffer
	result := RenderStream(&out, []stream.OutputSchema{
		textChunk(ChunkAnswer, "最终答案"),
	}, Options{})
	assert.Equal(t, "最终答案", result.Text)
}

// TestRenderStream_reasoning默认隐藏 verbose 开关。
func TestRenderStream_reasoning默认隐藏(t *testing.T) {
	chunks := []stream.OutputSchema{textChunk(ChunkLLMReasoning, "思考中")}
	var out bytes.Buffer
	RenderStream(&out, chunks, Options{})
	assert.NotContains(t, out.String(), "思考中")

	out.Reset()
	RenderStream(&out, chunks, Options{ShowReasoning: true})
	assert.Contains(t, out.String(), "思考中")
}

// TestRenderStream_message与工具 对齐各块类型。
func TestRenderStream_message与工具(t *testing.T) {
	var out bytes.Buffer
	chunks := []stream.OutputSchema{
		textChunk(ChunkMessage, "正在启动"),
		mkChunk(ChunkToolCall, map[string]any{"tool_name": "bash", "tool_args": map[string]any{"command": "ls"}}),
		mkChunk(ChunkToolResult, map[string]any{"tool_name": "bash", "tool_result": "文件"}),
	}
	result := RenderStream(&out, chunks, Options{})
	assert.Contains(t, out.String(), "⚙ 正在启动")
	assert.Contains(t, out.String(), "● Bash(ls)")
	assert.Contains(t, out.String(), "⎿  文件")
	assert.Empty(t, result.PendingInteractions)
}

// TestRenderStream_interaction 收集与回调。
func TestRenderStream_interaction(t *testing.T) {
	var out bytes.Buffer
	var questions []string
	chunks := []stream.OutputSchema{
		mkChunk(ChunkInteraction, map[string]any{
			"interaction_id": "i-1",
			"kind":           "user_input",
			"prompt":         "继续吗？",
		}),
	}
	result := RenderStream(&out, chunks, Options{
		OnInteraction: func(id string, question any) string {
			questions = append(questions, id)
			return "是"
		},
	})
	require.Len(t, result.PendingInteractions, 1)
	assert.Equal(t, "i-1", result.PendingInteractions[0].InteractionID)
	require.Len(t, questions, 1)
}

// TestRenderStream_todoUpdated 与 controller_output。
func TestRenderStream_todoUpdated(t *testing.T) {
	var out bytes.Buffer
	chunks := []stream.OutputSchema{
		mkChunk(ChunkTodoUpdated, map[string]any{
			"items": `[{"content":"任务","status":"pending"}]`,
		}),
	}
	RenderStream(&out, chunks, Options{})
	assert.Contains(t, out.String(), "☐ 任务")

	// controller_output 错误
	out.Reset()
	chunks = []stream.OutputSchema{
		mkChunk(ChunkControllerOutput, map[string]any{
			"type": "task_failed", "data": []any{map[string]any{"text": "炸了"}},
		}),
	}
	RenderStream(&out, chunks, Options{})
	assert.Contains(t, out.String(), "✗ 炸了")
}

// TestRenderStream_完整性警告 空流与全不可见。
func TestRenderStream_完整性警告(t *testing.T) {
	var out bytes.Buffer
	RenderStream(&out, nil, Options{})
	assert.Contains(t, out.String(), "No output received")

	out.Reset()
	RenderStream(&out, []stream.OutputSchema{mkChunk("unknown_type", nil)}, Options{})
	assert.Contains(t, out.String(), "No visible output received")
	assert.Contains(t, out.String(), "unknown_type")
}

// TestRenderStream_流式换行 llm_output 后跟其它块时补换行。
func TestRenderStream_流式换行(t *testing.T) {
	var out bytes.Buffer
	chunks := []stream.OutputSchema{
		textChunk(ChunkLLMOutput, "文本"),
		textChunk(ChunkMessage, "状态"),
	}
	RenderStream(&out, chunks, Options{})
	assert.True(t, strings.Contains(out.String(), "文本\n"), "llm_output 结束后补换行")
}
