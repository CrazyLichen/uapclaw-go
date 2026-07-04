package processor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
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
		input := `"content": "hello skill"`
		got := ExtractSkillFileContent(nil, input)
		if got == "" {
			t.Errorf("ExtractSkillFileContent() = empty, want non-empty")
		}
	})
}

func TestEstimateMessageTokens_字符串内容(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello world")
	result := EstimateMessageTokens(msg)
	if result != len("hello world")/3 {
		t.Errorf("期望 %d, 实际 %d", len("hello world")/3, result)
	}
}

func TestEstimateMessageTokens_空内容(t *testing.T) {
	msg := llm_schema.NewUserMessage("")
	result := EstimateMessageTokens(msg)
	// 空内容最小返回 1，对齐 Python max(len//3, 1)
	if result != 1 {
		t.Errorf("期望 1（最小值保护），实际 %d", result)
	}
}

func TestEstimateMessageTokens_长内容(t *testing.T) {
	content := strings.Repeat("x", 3000)
	msg := llm_schema.NewToolMessage("tc-1", content)
	result := EstimateMessageTokens(msg)
	if result != len(content)/3 {
		t.Errorf("期望 %d, 实际 %d", len(content)/3, result)
	}
}

// TestGroupCompletedAPIRoundsMessages 测试按 API 轮次分组消息
func TestGroupCompletedAPIRoundsMessages(t *testing.T) {
	t.Run("空消息列表", func(t *testing.T) {
		groups := GroupCompletedAPIRoundsMessages(nil)
		if len(groups) != 0 {
			t.Errorf("期望 0 组，实际 %d", len(groups))
		}
	})

	t.Run("单轮对话", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("你好"),
			llm_schema.NewAssistantMessage("你好！"),
		}
		groups := GroupCompletedAPIRoundsMessages(messages)
		if len(groups) < 1 {
			t.Error("应至少有 1 组")
		}
	})
}

// TestMessageSignature 测试消息签名生成
func TestMessageSignature(t *testing.T) {
	t.Run("用户消息", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("你好")
		sig := MessageSignature(msg)
		if sig == "" {
			t.Error("签名不应为空")
		}
		if !strings.Contains(sig, "user") {
			t.Errorf("签名应包含 role=user，实际: %s", sig)
		}
	})

	t.Run("助手消息", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("回复")
		sig := MessageSignature(msg)
		if !strings.Contains(sig, "assistant") {
			t.Errorf("签名应包含 role=assistant，实际: %s", sig)
		}
	})
}

// TestRoundSignature 测试轮次签名生成
func TestRoundSignature(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	sig := RoundSignature(messages)
	if sig == "" {
		t.Error("轮次签名不应为空")
	}
}

// TestFlattenGroups 测试分组展平
func TestFlattenGroups(t *testing.T) {
	t.Run("空分组", func(t *testing.T) {
		result := FlattenGroups(nil)
		if len(result) != 0 {
			t.Errorf("期望 0 条消息，实际 %d", len(result))
		}
	})

	t.Run("多个分组", func(t *testing.T) {
		groups := [][]llm_schema.BaseMessage{
			{llm_schema.NewUserMessage("a"), llm_schema.NewAssistantMessage("b")},
			{llm_schema.NewUserMessage("c"), llm_schema.NewAssistantMessage("d")},
		}
		result := FlattenGroups(groups)
		if len(result) != 4 {
			t.Errorf("期望 4 条消息，实际 %d", len(result))
		}
	})
}

// TestIsSkillFilePath 判断文件路径是否为 skill 文件
func TestIsSkillFilePath(t *testing.T) {
	t.Run("空路径", func(t *testing.T) {
		assert.False(t, IsSkillFilePath(""))
	})
	t.Run("标准skill路径", func(t *testing.T) {
		assert.True(t, IsSkillFilePath("skills/grep/skill.md"))
	})
	t.Run("根目录skill文件", func(t *testing.T) {
		assert.True(t, IsSkillFilePath("skill.md"))
	})
	t.Run("Windows风格路径", func(t *testing.T) {
		assert.True(t, IsSkillFilePath("skills\\grep\\skill.md"))
	})
	t.Run("大写Skill", func(t *testing.T) {
		assert.True(t, IsSkillFilePath("skills/grep/Skill.md"))
	})
	t.Run("非skill文件", func(t *testing.T) {
		assert.False(t, IsSkillFilePath("skills/grep/readme.md"))
	})
}

// TestMessageToText 提取消息纯文本内容
func TestMessageToText(t *testing.T) {
	t.Run("纯文本消息", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello world")
		assert.Equal(t, "hello world", MessageToText(msg))
	})
	t.Run("空文本消息", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("")
		assert.Equal(t, "", MessageToText(msg))
	})
	t.Run("Parts消息_有文本", func(t *testing.T) {
		content := llm_schema.NewMultiModalContent(
			llm_schema.ContentPart{Type: "text", Text: "part1"},
			llm_schema.ContentPart{Type: "text", Text: "part2"},
		)
		msg := &llm_schema.UserMessage{
			DefaultMessage: llm_schema.DefaultMessage{Content: content},
		}
		got := MessageToText(msg)
		assert.Equal(t, "part1\npart2", got)
	})
	t.Run("Parts消息_空Text", func(t *testing.T) {
		content := llm_schema.NewMultiModalContent(
			llm_schema.ContentPart{Type: "text", Text: ""},
		)
		msg := &llm_schema.UserMessage{
			DefaultMessage: llm_schema.DefaultMessage{Content: content},
		}
		got := MessageToText(msg)
		assert.Equal(t, "", got)
	})
}

// TestExtractArgumentValue 从 JSON 参数中提取指定 key 的值
func TestExtractArgumentValue(t *testing.T) {
	t.Run("从parsedArgs提取", func(t *testing.T) {
		parsed := map[string]any{"file_path": "/tmp/test.go"}
		got := ExtractArgumentValue(parsed, "", "file_path")
		assert.Equal(t, "/tmp/test.go", got)
	})
	t.Run("parsedArgs中key不存在", func(t *testing.T) {
		parsed := map[string]any{"other": "val"}
		got := ExtractArgumentValue(parsed, "", "file_path")
		assert.Equal(t, "", got)
	})
	t.Run("从argumentsText的JSON提取", func(t *testing.T) {
		got := ExtractArgumentValue(nil, `{"file_path": "/tmp/test.go"}`, "file_path")
		assert.Equal(t, "/tmp/test.go", got)
	})
	t.Run("正则回退提取", func(t *testing.T) {
		got := ExtractArgumentValue(nil, `{"file_path": "/tmp/test.go", "extra": 123}`, "missing_key")
		assert.Equal(t, "", got)
	})
	t.Run("正则回退提取_非标准JSON", func(t *testing.T) {
		// 正则回退：JSON 解析失败时，使用正则提取
		got := ExtractArgumentValue(nil, `{"file_path": "/tmp/test.go"`, "file_path")
		assert.Equal(t, "/tmp/test.go", got)
	})
	t.Run("多个key_优先匹配第一个", func(t *testing.T) {
		parsed := map[string]any{"path": "/a", "file_path": "/b"}
		got := ExtractArgumentValue(parsed, "", "path", "file_path")
		assert.Equal(t, "/a", got)
	})
	t.Run("值为空白字符串", func(t *testing.T) {
		parsed := map[string]any{"file_path": "  "}
		got := ExtractArgumentValue(parsed, "", "file_path")
		assert.Equal(t, "", got)
	})
	t.Run("nil parsedArgs和空argumentsText", func(t *testing.T) {
		got := ExtractArgumentValue(nil, "", "file_path")
		assert.Equal(t, "", got)
	})
}

// TestRoundContainsSkillRead 检查轮次中是否包含 skill 文件读取
func TestRoundContainsSkillRead(t *testing.T) {
	t.Run("包含skill文件读取", func(t *testing.T) {
		am := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{Name: "read_file", Arguments: `{"file_path": "skills/grep/skill.md"}`},
			},
		}
		messages := []llm_schema.BaseMessage{am}
		assert.True(t, RoundContainsSkillRead(messages))
	})
	t.Run("非read_file工具", func(t *testing.T) {
		am := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{Name: "grep", Arguments: `{"pattern": "test"}`},
			},
		}
		messages := []llm_schema.BaseMessage{am}
		assert.False(t, RoundContainsSkillRead(messages))
	})
	t.Run("read_file但非skill文件", func(t *testing.T) {
		am := &llm_schema.AssistantMessage{
			DefaultMessage: *llm_schema.NewDefaultMessage(llm_schema.RoleTypeAssistant, ""),
			ToolCalls: []*llm_schema.ToolCall{
				{Name: "read_file", Arguments: `{"file_path": "/tmp/readme.md"}`},
			},
		}
		messages := []llm_schema.BaseMessage{am}
		assert.False(t, RoundContainsSkillRead(messages))
	})
	t.Run("非AssistantMessage", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		assert.False(t, RoundContainsSkillRead(messages))
	})
}

// TestEstimateContentTokens 估算内容的 Token 数
func TestEstimateContentTokens(t *testing.T) {
	t.Run("字符串内容", func(t *testing.T) {
		assert.Equal(t, 3, EstimateContentTokens("hello world")) // 11/3=3
	})
	t.Run("非字符串_可JSON序列化", func(t *testing.T) {
		result := EstimateContentTokens(map[string]any{"key": "val"})
		assert.Greater(t, result, 0)
	})
	t.Run("非字符串_序列化失败", func(t *testing.T) {
		// 不可 JSON 序列化的值（如 channel）
		result := EstimateContentTokens(make(chan int))
		assert.GreaterOrEqual(t, result, 0)
	})
}

// TestEstimateMessageTokens_nil消息
func TestEstimateMessageTokens_nil消息(t *testing.T) {
	assert.Equal(t, 0, EstimateMessageTokens(nil))
}

// TestEstimateMessageTokens_非文本内容
func TestEstimateMessageTokens_非文本内容(t *testing.T) {
	content := llm_schema.NewMultiModalContent(
		llm_schema.ContentPart{Type: "image_url", ImageURL: &llm_schema.ImageURL{URL: "http://example.com/img.png"}},
	)
	msg := &llm_schema.UserMessage{
		DefaultMessage: llm_schema.DefaultMessage{Content: content},
	}
	result := EstimateMessageTokens(msg)
	assert.GreaterOrEqual(t, result, 0)
}

// TestCountMessagesTokens_有TokenCounter
func TestCountMessagesTokens_有TokenCounter(t *testing.T) {
	t.Run("TokenCounter报错降级", func(t *testing.T) {
		// 使用报错的 TokenCounter
		tc := &errorTokenCounter{}
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := CountMessagesTokens(tc, messages, "test-model", "TestProcessor")
		assert.Greater(t, got, 0)
	})
	t.Run("TokenCounter成功", func(t *testing.T) {
		tc := &fakeTokenCounter{count: 42}
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := CountMessagesTokens(tc, messages, "test-model", "")
		assert.Equal(t, 42, got)
	})
	t.Run("空processorType", func(t *testing.T) {
		tc := &errorTokenCounter{}
		messages := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
		got := CountMessagesTokens(tc, messages, "test-model", "")
		assert.Greater(t, got, 0)
	})
}

// TestDescribeToolCall_所有工具 生成工具调用的可读描述
func TestDescribeToolCall_所有工具(t *testing.T) {
	t.Run("write_file", func(t *testing.T) {
		got := DescribeToolCall("write_file", `{"file_path": "/tmp/test.go"}`)
		assert.Contains(t, got, "write_file")
		assert.Contains(t, got, "/tmp/test.go")
	})
	t.Run("edit_file", func(t *testing.T) {
		got := DescribeToolCall("edit_file", `{"file_path": "/tmp/test.go"}`)
		assert.Contains(t, got, "edit_file")
		assert.Contains(t, got, "/tmp/test.go")
	})
	t.Run("glob", func(t *testing.T) {
		got := DescribeToolCall("glob", `{"pattern": "*.go", "path": "/tmp"}`)
		assert.Contains(t, got, "glob")
		assert.Contains(t, got, "*.go")
	})
	t.Run("grep", func(t *testing.T) {
		got := DescribeToolCall("grep", `{"pattern": "test", "path": "/tmp"}`)
		assert.Contains(t, got, "grep")
	})
	t.Run("grep_with_file_path_key", func(t *testing.T) {
		got := DescribeToolCall("grep", `{"pattern": "test", "file_path": "/tmp/app.go"}`)
		assert.Contains(t, got, "grep")
	})
}

// TestExtractToolResultHint 提取工具结果的简要提示
func TestExtractToolResultHint(t *testing.T) {
	t.Run("空结果文本", func(t *testing.T) {
		assert.Equal(t, "", ExtractToolResultHint("read_file", "", []string{"read_file"}))
	})
	t.Run("不允许的工具名", func(t *testing.T) {
		assert.Equal(t, "", ExtractToolResultHint("custom", "some result", []string{"read_file"}))
	})
	t.Run("read_file_匹配", func(t *testing.T) {
		result := `"file_path": "/tmp/test.go", "line_count": 42`
		got := ExtractToolResultHint("read_file", result, []string{"read_file"})
		assert.Contains(t, got, "result_path=/tmp/test.go")
		assert.Contains(t, got, "lines=42")
	})
	t.Run("read_file_仅file_path", func(t *testing.T) {
		result := `"file_path": "/tmp/test.go"`
		got := ExtractToolResultHint("read_file", result, []string{"read_file"})
		assert.Contains(t, got, "result_path=/tmp/test.go")
	})
	t.Run("glob_匹配", func(t *testing.T) {
		result := `"count": 5`
		got := ExtractToolResultHint("glob", result, []string{"glob"})
		assert.Equal(t, "matches=5", got)
	})
	t.Run("grep_匹配", func(t *testing.T) {
		result := `"count": 3`
		got := ExtractToolResultHint("grep", result, []string{"grep"})
		assert.Equal(t, "hits=3", got)
	})
	t.Run("edit_file_匹配", func(t *testing.T) {
		result := `"replacements": 2`
		got := ExtractToolResultHint("edit_file", result, []string{"edit_file"})
		assert.Equal(t, "replacements=2", got)
	})
	t.Run("write_file_匹配", func(t *testing.T) {
		result := `"bytes_written": 1024`
		got := ExtractToolResultHint("write_file", result, []string{"write_file"})
		assert.Equal(t, "bytes_written=1024", got)
	})
	t.Run("允许列表中的工具但无匹配", func(t *testing.T) {
		result := "no structured data"
		got := ExtractToolResultHint("read_file", result, []string{"read_file"})
		assert.Equal(t, "", got)
	})
}

// TestFilePathOrDefault 文件路径默认值
func TestFilePathOrDefault(t *testing.T) {
	t.Run("空路径", func(t *testing.T) {
		assert.Equal(t, "[unknown]", filePathOrDefault(""))
	})
	t.Run("有路径", func(t *testing.T) {
		assert.Equal(t, "/tmp/test.go", filePathOrDefault("/tmp/test.go"))
	})
}

// TestPathOrDefault 路径默认值
func TestPathOrDefault(t *testing.T) {
	t.Run("空路径", func(t *testing.T) {
		assert.Equal(t, ".", pathOrDefault(""))
	})
	t.Run("有路径", func(t *testing.T) {
		assert.Equal(t, "/tmp", pathOrDefault("/tmp"))
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errorTokenCounter 报错的 TokenCounter
type errorTokenCounter struct{}

func (e *errorTokenCounter) Count(_ string, _ string) (int, error) { return 0, assert.AnError }
func (e *errorTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	return 0, assert.AnError
}
func (e *errorTokenCounter) CountTools(_ []*common_schema.ToolInfo, _ string) (int, error) {
	return 0, assert.AnError
}

// fakeTokenCounter 正常的 TokenCounter
type fakeTokenCounter struct {
	count int
}

func (f *fakeTokenCounter) Count(_ string, _ string) (int, error) { return f.count, nil }
func (f *fakeTokenCounter) CountMessages(_ []llm_schema.BaseMessage, _ string) (int, error) {
	return f.count, nil
}
func (f *fakeTokenCounter) CountTools(_ []*common_schema.ToolInfo, _ string) (int, error) {
	return f.count, nil
}
