package processor

import (
	"fmt"
	"strings"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestExtractToolName(t *testing.T) {
	t.Run("Name字段有值", func(t *testing.T) {
		tc := &llm_schema.ToolCall{Name: "grep"}
		got := ExtractToolName(tc)
		if got != "grep" {
			t.Errorf("ExtractToolName() = %q, want %q", got, "grep")
		}
	})

	t.Run("Name字段为空", func(t *testing.T) {
		tc := &llm_schema.ToolCall{Name: ""}
		got := ExtractToolName(tc)
		if got != "" {
			t.Errorf("ExtractToolName() = %q, want empty string", got)
		}
	})

	t.Run("nil ToolCall", func(t *testing.T) {
		got := ExtractToolName(nil)
		if got != "" {
			t.Errorf("ExtractToolName(nil) = %q, want empty string", got)
		}
	})
}

func TestResolveToolCallFromMessage(t *testing.T) {
	t.Run("非ToolMessage返回nil", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := ResolveToolCallFromMessage(msg, nil)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})

	t.Run("ToolMessage无ToolCallID返回nil", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("", "result")
		got := ResolveToolCallFromMessage(msg, nil)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})

	t.Run("匹配到ToolCall", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_1", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
				{ID: "call_2", Name: "glob"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got == nil {
			t.Fatal("ResolveToolCallFromMessage() = nil, want non-nil")
		}
		if got.ID != "call_1" {
			t.Errorf("got.ID = %q, want %q", got.ID, "call_1")
		}
	})

	t.Run("多条AssistantMessage从后匹配", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_2", "result")
		assistant1 := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		assistant2 := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_2", Name: "glob"},
			},
		}
		messages := []llm_schema.BaseMessage{assistant1, assistant2, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got == nil {
			t.Fatal("ResolveToolCallFromMessage() = nil, want non-nil")
		}
		if got.Name != "glob" {
			t.Errorf("got.Name = %q, want %q", got.Name, "glob")
		}
	})

	t.Run("未匹配返回nil", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_999", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolCallFromMessage(toolMsg, messages)
		if got != nil {
			t.Errorf("ResolveToolCallFromMessage() = %v, want nil", got)
		}
	})
}

func TestResolveToolNameFromMessage(t *testing.T) {
	t.Run("回溯找到工具名", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_1", "result")
		assistantMsg := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{ID: "call_1", Name: "grep"},
			},
		}
		messages := []llm_schema.BaseMessage{assistantMsg, toolMsg}
		got := ResolveToolNameFromMessage(toolMsg, messages)
		if got != "grep" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want %q", got, "grep")
		}
	})

	t.Run("未找到返回空字符串", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call_999", "result")
		got := ResolveToolNameFromMessage(toolMsg, []llm_schema.BaseMessage{})
		if got != "" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want empty string", got)
		}
	})

	t.Run("非ToolMessage返回空字符串", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		got := ResolveToolNameFromMessage(msg, nil)
		if got != "" {
			t.Errorf("ResolveToolNameFromMessage() = %q, want empty string", got)
		}
	})
}

func TestIsSummaryMessage(t *testing.T) {
	t.Run("是摘要消息", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("[CURRENT_ROUND_MEMORY_BLOCK]\nSummary:\ntest")
		if !IsSummaryMessage(msg, "[CURRENT_ROUND_MEMORY_BLOCK]") {
			t.Error("IsSummaryMessage() = false, want true")
		}
	})
	t.Run("不是摘要消息_标记不匹配", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("[DIALOGUE_MEMORY_BLOCK]\nSummary:\ntest")
		if IsSummaryMessage(msg, "[CURRENT_ROUND_MEMORY_BLOCK]") {
			t.Error("IsSummaryMessage() = true, want false")
		}
	})
	t.Run("不是摘要消息_非UserMessage", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("hello")
		if IsSummaryMessage(msg, "[CURRENT_ROUND_MEMORY_BLOCK]") {
			t.Error("IsSummaryMessage() = true, want false")
		}
	})
}

func TestCollectSummaryIndices(t *testing.T) {
	t.Run("多个摘要", func(t *testing.T) {
		marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns1"),
			llm_schema.NewAssistantMessage("hi"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns2"),
		}
		got := CollectSummaryIndices(messages, marker)
		if len(got) != 2 || got[0] != 1 || got[1] != 3 {
			t.Errorf("CollectSummaryIndices() = %v, want [1 3]", got)
		}
	})
	t.Run("无摘要", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := CollectSummaryIndices(messages, "[CURRENT_ROUND_MEMORY_BLOCK]")
		if len(got) != 0 {
			t.Errorf("CollectSummaryIndices() = %v, want []", got)
		}
	})
}

func TestCountMessagesTokens(t *testing.T) {
	t.Run("空消息返回0", func(t *testing.T) {
		got := CountMessagesTokens(nil, nil, "", "")
		if got != 0 {
			t.Errorf("CountMessagesTokens(nil) = %d, want 0", got)
		}
	})
	t.Run("无TokenCounter降级到字符估算", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello world")}
		got := CountMessagesTokens(nil, messages, "", "TestProcessor")
		if got <= 0 {
			t.Errorf("CountMessagesTokens() = %d, want > 0", got)
		}
	})
}

func TestFindLastFinalAssistantIdx(t *testing.T) {
	t.Run("无AssistantMessage返回负一", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewSystemMessage("系统消息"),
		}
		idx := FindLastFinalAssistantIdx(messages)
		if idx != -1 {
			t.Errorf("无 AssistantMessage 应返回 -1，实际: %d", idx)
		}
	})
	t.Run("有ToolCalls跳过", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("我来查",
				llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					{ID: "call_1", Name: "get_weather", Arguments: `{}`},
				}),
			),
		}
		idx := FindLastFinalAssistantIdx(messages)
		if idx != -1 {
			t.Errorf("含 tool_calls 的 AssistantMessage 应被跳过，实际: %d", idx)
		}
	})
	t.Run("找到最终Assistant", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("你好！"),
			llm_schema.NewUserMessage("查询天气"),
			llm_schema.NewAssistantMessage("我来查",
				llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					{ID: "call_1", Name: "get_weather", Arguments: `{}`},
				}),
			),
			llm_schema.NewToolMessage("call_1", "晴天"),
			llm_schema.NewAssistantMessage("今天晴天"),
		}
		idx := FindLastFinalAssistantIdx(messages)
		if idx != 5 {
			t.Errorf("最后一条不含 tool_calls 的 AssistantMessage 应在索引 5，实际: %d", idx)
		}
	})
	t.Run("最后一条有ToolCalls往前找", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("你好！"),
			llm_schema.NewUserMessage("查询天气"),
			llm_schema.NewAssistantMessage("查询中",
				llm_schema.WithToolCalls([]*llm_schema.ToolCall{
					{ID: "call_1", Name: "get_weather", Arguments: `{}`},
				}),
			),
		}
		idx := FindLastFinalAssistantIdx(messages)
		if idx != 1 {
			t.Errorf("应找到索引 1 的 AssistantMessage，实际: %d", idx)
		}
	})
}

func TestFindLastCompletedAPIRoundEndIdx(t *testing.T) {
	t.Run("有完成轮次", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewAssistantMessage("hi"),
			llm_schema.NewUserMessage("world"),
		}
		got := FindLastCompletedAPIRoundEndIdx(messages, 0, 1)
		if got != 1 {
			t.Errorf("FindLastCompletedAPIRoundEndIdx() = %d, want 1", got)
		}
	})
	t.Run("无完成轮次", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := FindLastCompletedAPIRoundEndIdx(messages, 0, 0)
		if got != -1 {
			t.Errorf("FindLastCompletedAPIRoundEndIdx() = %d, want -1", got)
		}
	})
	t.Run("endIdx小于startIdx", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := FindLastCompletedAPIRoundEndIdx(messages, 1, 0)
		if got != 0 {
			t.Errorf("FindLastCompletedAPIRoundEndIdx() = %d, want 0", got)
		}
	})
}

func TestIterSummaryMergeRanges(t *testing.T) {
	marker := "[CURRENT_ROUND_MEMORY_BLOCK]"
	t.Run("足够连续块", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(marker + "\nSummary:\ns1"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns2"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns3"),
			llm_schema.NewAssistantMessage("break"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns4"),
		}
		got := IterSummaryMergeRanges(messages, marker, 3)
		if len(got) != 1 || got[0] != [2]int{0, 2} {
			t.Errorf("IterSummaryMergeRanges() = %v, want [[0 2]]", got)
		}
	})
	t.Run("不足连续块", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(marker + "\nSummary:\ns1"),
			llm_schema.NewUserMessage(marker + "\nSummary:\ns2"),
		}
		got := IterSummaryMergeRanges(messages, marker, 3)
		if len(got) != 0 {
			t.Errorf("IterSummaryMergeRanges() = %v, want []", got)
		}
	})
}

func TestParseToolArguments(t *testing.T) {
	t.Run("正常JSON", func(t *testing.T) {
		got := ParseToolArguments(`{"file_path": "/tmp/test.go"}`)
		if got["file_path"] != "/tmp/test.go" {
			t.Errorf("ParseToolArguments() = %v, want file_path=/tmp/test.go", got)
		}
	})
	t.Run("空字符串", func(t *testing.T) {
		got := ParseToolArguments("")
		if len(got) != 0 {
			t.Errorf("ParseToolArguments('') = %v, want empty map", got)
		}
	})
	t.Run("非法JSON", func(t *testing.T) {
		got := ParseToolArguments("not json")
		if len(got) != 0 {
			t.Errorf("ParseToolArguments('not json') = %v, want empty map", got)
		}
	})
}

func TestDescribeToolCall(t *testing.T) {
	t.Run("read_file", func(t *testing.T) {
		got := DescribeToolCall("read_file", `{"file_path": "/tmp/test.go"}`)
		if !strings.Contains(got, "read_file") || !strings.Contains(got, "/tmp/test.go") {
			t.Errorf("DescribeToolCall() = %q", got)
		}
	})
	t.Run("未知工具", func(t *testing.T) {
		got := DescribeToolCall("custom_tool", `{"arg": "val"}`)
		if !strings.Contains(got, "custom_tool") {
			t.Errorf("DescribeToolCall() = %q", got)
		}
	})
}

func TestFindToolResultText(t *testing.T) {
	t.Run("找到结果", func(t *testing.T) {
		tm := llm_schema.NewToolMessage("call_1", "file content here")
		messages := []llm_schema.BaseMessage{tm}
		got := FindToolResultText(messages, "call_1")
		if got != "file content here" {
			t.Errorf("FindToolResultText() = %q, want %q", got, "file content here")
		}
	})
	t.Run("未找到", func(t *testing.T) {
		got := FindToolResultText(nil, "call_999")
		if got != "" {
			t.Errorf("FindToolResultText() = %q, want empty", got)
		}
	})
	t.Run("空ToolCallID", func(t *testing.T) {
		got := FindToolResultText(nil, "")
		if got != "" {
			t.Errorf("FindToolResultText() = %q, want empty", got)
		}
	})
}

func TestExtractSkillNameFromPath(t *testing.T) {
	t.Run("正常skill路径", func(t *testing.T) {
		got := ExtractSkillNameFromPath("skills/grep/skill.md")
		if got != "grep" {
			t.Errorf("ExtractSkillNameFromPath() = %q, want %q", got, "grep")
		}
	})
	t.Run("非skill路径", func(t *testing.T) {
		got := ExtractSkillNameFromPath("path/to/readme.md")
		if got != "" {
			t.Errorf("ExtractSkillNameFromPath() = %q, want empty", got)
		}
	})
	t.Run("空路径", func(t *testing.T) {
		got := ExtractSkillNameFromPath("")
		if got != "" {
			t.Errorf("ExtractSkillNameFromPath() = %q, want empty", got)
		}
	})
}

func TestExtractSkillFileContent(t *testing.T) {
	t.Run("空内容", func(t *testing.T) {
		got := ExtractSkillFileContent(nil, "")
		if got != "" {
			t.Errorf("ExtractSkillFileContent() = %q, want empty", got)
		}
	})
	t.Run("正常JSON内容", func(t *testing.T) {
		input := fmt.Sprintf(`"content": "hello skill"`)
		got := ExtractSkillFileContent(nil, input)
		if got == "" {
			t.Errorf("ExtractSkillFileContent() = empty, want non-empty")
		}
	})
}
