package support

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	ceprocessor "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor"
	"github.com/stretchr/testify/assert"
)

// TestInternalUserPrefixes 测试内部前缀表完整性（Python 七项一比一）
func TestInternalUserPrefixes(t *testing.T) {
	want := []string{
		"<system-reminder>",
		"<memory_block_current>",
		"<memory_block_dialogue>",
		"<memory_block_round>",
		"<memory_block_session>",
		"<recovered_context>",
		"[STATE_REINJECTION]",
	}
	if len(InternalUserPrefixes) != len(want) {
		t.Fatalf("前缀表应含 %d 项，实际 %d", len(want), len(InternalUserPrefixes))
	}
	for i, w := range want {
		if InternalUserPrefixes[i] != w {
			t.Errorf("第 %d 项应为 %s，实际 %s", i, w, InternalUserPrefixes[i])
		}
	}
}

// TestEstimateContentTokens len//3 近似计数
func TestEstimateContentTokens(t *testing.T) {
	if got := EstimateContentTokens("abcdefghij"); got != 3 { // 10/3=3
		t.Errorf("10 字符应估 3 token，实际 %d", got)
	}
	// 短文本下限 1
	if got := EstimateContentTokens("ab"); got != 1 {
		t.Errorf("短文本应取下限 1，实际 %d", got)
	}
	// nil → 空 → 下限 1
	if got := EstimateContentTokens(nil); got != 1 {
		t.Errorf("nil 内容应取下限 1，实际 %d", got)
	}
}

// TestCountMessagesTokens_Empty 空列表
func TestCountMessagesTokens_Empty(t *testing.T) {
	if got := CountMessagesTokens(nil, nil, "", "", false); got != 0 {
		t.Errorf("空列表应为 0，实际 %d", got)
	}
}

// TestCountMessagesTokens_CharFallback 无计数器时字符兜底
func TestCountMessagesTokens_CharFallback(t *testing.T) {
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("abcdefghij")}
	// 10/3 = 3
	if got := CountMessagesTokens(msgs, nil, "", "", false); got != 3 {
		t.Errorf("字符兜底应估 3，实际 %d", got)
	}
}

// TestCountMessagesTokens_UsageAware usage 基线 + 尾部估算
func TestCountMessagesTokens_UsageAware(t *testing.T) {
	base := llm_schema.NewAssistantMessage("done")
	base.UsageMetadata = &llm_schema.UsageMetadata{TotalTokens: 100}
	msgs := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("abcdefghij"), // 基线前的消息不计
		base,
		llm_schema.NewUserMessage("abcdefghij"), // 尾部：3
	}
	got, ok := countUsageTokensWithTail(msgs)
	if !ok || got != 103 {
		t.Fatalf("usage-aware 应为 100+3=103，实际 %d ok=%v", got, ok)
	}
	if got2 := CountMessagesTokens(msgs, nil, "", "", true); got2 != 103 {
		t.Errorf("usageAware 计数应为 103，实际 %d", got2)
	}
}

// TestCountMessagesTokens_UsageAwareNoBase 无 usage 基线时全量字符兜底
func TestCountMessagesTokens_UsageAwareNoBase(t *testing.T) {
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("abcdefghij")}
	if got := CountMessagesTokens(msgs, nil, "", "", true); got != 3 {
		t.Errorf("无基线应全量兜底 3，实际 %d", got)
	}
}

// TestResolveRatioTokenThreshold 比例换算与下限
func TestResolveRatioTokenThreshold(t *testing.T) {
	if got := ResolveRatioTokenThreshold(1000, 0.8); got != 800 {
		t.Errorf("1000*0.8 应为 800，实际 %d", got)
	}
	if got := ResolveRatioTokenThreshold(1, 0.1); got != 1 {
		t.Errorf("比例过小应取下限 1，实际 %d", got)
	}
}

// ───────────────── 24.15 Task 2 补齐符号（附录 A.2） ─────────────────

// TestSkillPathHelpers 测试 skill.md 路径判定与技能名抽取
// Python: util.py:73/:80
func TestSkillPathHelpers(t *testing.T) {
	assert.True(t, IsSkillFilePath("/a/b/SKILL.md"))
	assert.True(t, IsSkillFilePath(`C:\x\skill.md`))
	assert.True(t, IsSkillFilePath("skill.md"))
	assert.False(t, IsSkillFilePath("/a/readme.md"))
	assert.False(t, IsSkillFilePath(""))
	assert.Equal(t, "my-skill", ExtractSkillNameFromPath("/skills/my-skill/SKILL.md"))
	assert.Equal(t, "", ExtractSkillNameFromPath("/skills/other.md"))
}

// TestRoundContainsSkillRead 测试 read_file(skill.md) 检测
// Python: util.py:90
func TestRoundContainsSkillRead(t *testing.T) {
	readSkill := &llm_schema.AssistantMessage{}
	readSkill.ToolCalls = []*llm_schema.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"file_path":"/s/foo/SKILL.md"}`}}
	readOther := &llm_schema.AssistantMessage{}
	readOther.ToolCalls = []*llm_schema.ToolCall{{ID: "2", Name: "read_file", Arguments: `{"file_path":"/s/other.txt"}`}}
	assert.True(t, RoundContainsSkillRead([]llm_schema.BaseMessage{readSkill}))
	assert.False(t, RoundContainsSkillRead([]llm_schema.BaseMessage{readOther}))
	assert.False(t, RoundContainsSkillRead(nil))
}

// TestMessageSignatureAndHelpers 测试签名与参数解析
// Python: util.py:110/:167/:177
func TestMessageSignatureAndHelpers(t *testing.T) {
	assistant := &llm_schema.AssistantMessage{}
	assistant.ToolCalls = []*llm_schema.ToolCall{{ID: "t1", Name: "read_file", Arguments: `{"file_path":"/x"}`}}
	sig := MessageSignature(assistant)
	assert.Contains(t, sig, "assistant")
	assert.Contains(t, sig, "t1")
	assert.Equal(t, map[string]any{"file_path": "/x"}, ParseToolArguments(`{"file_path":"/x"}`))
	assert.Empty(t, ParseToolArguments("not-json"))
	assert.Equal(t, "/x", ExtractArgumentValue(ParseToolArguments(`{"file_path":"/x"}`), "", []string{"file_path"}))
	// 正则兜底分支
	assert.Equal(t, "/y", ExtractArgumentValue(map[string]any{}, `{"file_path":"/y"}`, []string{"file_path"}))
	assert.Equal(t, "", ExtractArgumentValue(map[string]any{}, "", []string{"file_path"}))
}

// TestDescribeToolCall 测试工具调用描述
// Python: util.py:145
func TestDescribeToolCall(t *testing.T) {
	assert.Equal(t, "read_file path=/a", DescribeToolCall("read_file", `{"file_path":"/a"}`))
	assert.Equal(t, "write_file path=[unknown]", DescribeToolCall("write_file", `{}`))
	assert.Equal(t, "glob pattern=[unknown] path=.", DescribeToolCall("glob", `{}`))
	assert.Equal(t, "glob pattern=*.go path=/w", DescribeToolCall("glob", `{"pattern":"*.go","path":"/w"}`))
	assert.Equal(t, "grep pattern=p path=[unknown]", DescribeToolCall("grep", `{"pattern":"p"}`))
	assert.Equal(t, "grep pattern=p path=/f", DescribeToolCall("grep", `{"pattern":"p","file_path":"/f"}`))
	assert.Equal(t, "custom args={}", DescribeToolCall("custom", `{}`))
}

// TestFindToolResultText 测试按 tool_call_id 逆序查找
// Python: util.py:193
func TestFindToolResultText(t *testing.T) {
	toolMsg := llm_schema.NewToolMessage("t9", "result-body")
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hi"), toolMsg}
	assert.Contains(t, FindToolResultText(msgs, "t9"), "result-body")
	assert.Equal(t, "", FindToolResultText(msgs, "missing"))
	assert.Equal(t, "", FindToolResultText(msgs, ""))
}

// TestExtractToolResultHint 测试工具结果摘要提示
// Python: util.py:205
func TestExtractToolResultHint(t *testing.T) {
	allowed := []string{"read_file", "glob", "grep", "edit_file", "write_file"}
	assert.Equal(t, "result_path=/f lines=12",
		ExtractToolResultHint("read_file", `{"file_path":"/f","line_count":12}`, allowed))
	assert.Equal(t, "matches=3", ExtractToolResultHint("glob", `{"count":3}`, allowed))
	assert.Equal(t, "hits=7", ExtractToolResultHint("grep", `{"count":7}`, allowed))
	assert.Equal(t, "replacements=2", ExtractToolResultHint("edit_file", `{"replacements":2}`, allowed))
	assert.Equal(t, "bytes_written=99", ExtractToolResultHint("write_file", `{"bytes_written":99}`, allowed))
	assert.Equal(t, "", ExtractToolResultHint("read_file", "", allowed))
	assert.Equal(t, "", ExtractToolResultHint("unknown", `{"count":1}`, allowed))
	assert.Equal(t, "", ExtractToolResultHint("read_file", `{}`, allowed))
}

// TestSummaryMessageHelpers 测试摘要消息判定/收集/合并区间
// Python: util.py:247/:256/:427/:410
func TestSummaryMessageHelpers(t *testing.T) {
	mk := func(text string) llm_schema.BaseMessage { return llm_schema.NewUserMessage(text) }
	msgs := []llm_schema.BaseMessage{
		mk("<memory_block>a</memory_block>"),
		mk("real user"),
		mk("<memory_block>b</memory_block>"),
		mk("<memory_block>c</memory_block>"),
	}
	assert.True(t, IsSummaryMessage(msgs[0], "<memory_block>"))
	assert.False(t, IsSummaryMessage(msgs[1], "<memory_block>"))
	assert.Equal(t, []int{0, 2, 3}, CollectSummaryIndices(msgs, "<memory_block>"))
	assert.Equal(t, [][2]int{{2, 3}}, IterSummaryMergeRanges(msgs, "<memory_block>", 2))
	assert.Empty(t, IterSummaryMergeRanges(msgs, "<memory_block>", 4))

	// FindLastCompletedAPIRoundEndIdx
	ranges := ceprocessor.GroupCompletedAPIRounds(msgs)
	if len(ranges) > 0 {
		end := FindLastCompletedAPIRoundEndIdx(msgs, 0, len(msgs)-1)
		assert.GreaterOrEqual(t, end, 0)
	}
	assert.Equal(t, -1, FindLastCompletedAPIRoundEndIdx(nil, 0, -1))
}
