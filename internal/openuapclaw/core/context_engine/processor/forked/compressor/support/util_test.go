package support

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
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
