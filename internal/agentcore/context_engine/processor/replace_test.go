package processor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestReplaceMessages_SingleReplacement 测试单个替换
func TestReplaceMessages_SingleReplacement(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewUserMessage("msg3"),
	}

	replacements := []Replacement{
		{
			StartIdx: 1,
			EndIdx:   2,
			Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("replaced")},
		},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 3 {
		t.Fatalf("期望 3 条消息，实际 %d", len(result))
	}
	if result[0].GetContent().Text() != "msg0" {
		t.Fatalf("期望 msg0，实际 %s", result[0].GetContent().Text())
	}
	if result[1].GetContent().Text() != "replaced" {
		t.Fatalf("期望 replaced，实际 %s", result[1].GetContent().Text())
	}
	if result[2].GetContent().Text() != "msg3" {
		t.Fatalf("期望 msg3，实际 %s", result[2].GetContent().Text())
	}
}

// TestReplaceMessages_MultipleReplacements 测试多个替换（从后往前）
func TestReplaceMessages_MultipleReplacements(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewUserMessage("msg3"),
		llm_schema.NewUserMessage("msg4"),
		llm_schema.NewUserMessage("msg5"),
	}

	replacements := []Replacement{
		{StartIdx: 1, EndIdx: 2, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("A")}},
		{StartIdx: 4, EndIdx: 5, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("B")}},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 4 {
		t.Fatalf("期望 4 条消息，实际 %d", len(result))
	}
	expected := []string{"msg0", "A", "msg3", "B"}
	for i, exp := range expected {
		if result[i].GetContent().Text() != exp {
			t.Fatalf("位置 %d: 期望 %s，实际 %s", i, exp, result[i].GetContent().Text())
		}
	}
}

// TestReplaceMessages_ExpansionReplacement 测试替换后消息数增加
func TestReplaceMessages_ExpansionReplacement(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
	}

	replacements := []Replacement{
		{
			StartIdx: 1,
			EndIdx:   1,
			Messages: []llm_schema.BaseMessage{
				llm_schema.NewUserMessage("a"),
				llm_schema.NewUserMessage("b"),
			},
		},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 4 {
		t.Fatalf("期望 4 条消息，实际 %d", len(result))
	}
	expected := []string{"msg0", "a", "b", "msg2"}
	for i, exp := range expected {
		if result[i].GetContent().Text() != exp {
			t.Fatalf("位置 %d: 期望 %s，实际 %s", i, exp, result[i].GetContent().Text())
		}
	}
}

// TestReplaceMessages_EmptyReplacements 测试空替换列表
func TestReplaceMessages_EmptyReplacements(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
	}

	result := ReplaceMessages(messages, nil)

	if len(result) != 2 {
		t.Fatalf("期望 2 条消息，实际 %d", len(result))
	}
}

// TestReplaceMessages_UnorderedReplacements 测试乱序输入（应自动按 StartIdx 降序处理）
func TestReplaceMessages_UnorderedReplacements(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
		llm_schema.NewUserMessage("msg2"),
		llm_schema.NewUserMessage("msg3"),
	}

	// 先写后面的替换，再写前面的，验证排序逻辑
	replacements := []Replacement{
		{StartIdx: 2, EndIdx: 3, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("B")}},
		{StartIdx: 0, EndIdx: 0, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("A")}},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 3 {
		t.Fatalf("期望 3 条消息，实际 %d", len(result))
	}
	expected := []string{"A", "msg1", "B"}
	for i, exp := range expected {
		if result[i].GetContent().Text() != exp {
			t.Fatalf("位置 %d: 期望 %s，实际 %s", i, exp, result[i].GetContent().Text())
		}
	}
}

// TestReplaceMessages_InvalidIndex 测试无效索引跳过
func TestReplaceMessages_InvalidIndex(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("msg0"),
		llm_schema.NewUserMessage("msg1"),
	}

	replacements := []Replacement{
		{StartIdx: -1, EndIdx: 0, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("A")}},
		{StartIdx: 5, EndIdx: 6, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("B")}},
		{StartIdx: 2, EndIdx: 1, Messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("C")}},
	}

	result := ReplaceMessages(messages, replacements)

	if len(result) != 2 {
		t.Fatalf("期望 2 条消息（所有替换无效），实际 %d", len(result))
	}
}
