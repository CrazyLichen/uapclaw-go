package context_engineer

import (
	"testing"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/offloader"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestApplyCompressionRatios_只下调不放大 验证 min(绝对, ratio×window) 与不参与字段不变
func TestApplyCompressionRatios_只下调不放大(t *testing.T) {
	cases := []struct {
		name         string
		window       int
		wantTrigger  int
		wantTarget   int
		wantDialogue int
	}{
		{"32K窗口", 32000, 27200, 17600, 27200},
		{"128K窗口", 128000, 108800, 70400, 100000},
		{"256K窗口", 256000, 217600, 140800, 100000},
		{"512K窗口", 512000, 230000, 160000, 100000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			specs := []ceiface.ProcessorSpec{
				{Type: "DialogueCompressor", Config: &compressor.DialogueCompressorConfig{TokensThreshold: 100000}},
				{Type: "CurrentRoundCompressor", Config: &compressor.CurrentRoundCompressorConfig{TokensThreshold: 100000}},
				{Type: "RoundLevelCompressor", Config: &compressor.RoundLevelCompressorConfig{
					TriggerTotalTokens: 230000, TargetTotalTokens: 160000}},
				{Type: "FullCompactProcessor", Config: &compressor.FullCompactProcessorConfig{
					TriggerTotalTokens: 180000, MessagesToKeep: 10}},
			}
			out := applyCompressionRatios(specs, tc.window, 0.85, 0.55)

			dialogue := out[0].Config.(*compressor.DialogueCompressorConfig)
			roundLevel := out[2].Config.(*compressor.RoundLevelCompressorConfig)
			if dialogue.TokensThreshold != tc.wantDialogue {
				t.Errorf("Dialogue 阈值应为 %d，实际 %d", tc.wantDialogue, dialogue.TokensThreshold)
			}
			if roundLevel.TriggerTotalTokens != tc.wantTrigger {
				t.Errorf("trigger 应为 %d，实际 %d", tc.wantTrigger, roundLevel.TriggerTotalTokens)
			}
			if roundLevel.TargetTotalTokens != tc.wantTarget {
				t.Errorf("target 应为 %d，实际 %d", tc.wantTarget, roundLevel.TargetTotalTokens)
			}
			// 不参与收口的字段保持不变
			full := out[3].Config.(*compressor.FullCompactProcessorConfig)
			if full.MessagesToKeep != 10 {
				t.Errorf("MessagesToKeep 不应被改动，实际 %d", full.MessagesToKeep)
			}
		})
	}
}

// TestApplyCompressionRatios_比例缺省时保持绝对值 验证 ratio=0 时不改动
func TestApplyCompressionRatios_比例缺省时保持绝对值(t *testing.T) {
	specs := []ceiface.ProcessorSpec{
		{Type: "RoundLevelCompressor", Config: &compressor.RoundLevelCompressorConfig{
			TriggerTotalTokens: 230000, TargetTotalTokens: 160000}},
	}
	out := applyCompressionRatios(specs, 32000, 0, 0)
	rl := out[0].Config.(*compressor.RoundLevelCompressorConfig)
	if rl.TriggerTotalTokens != 230000 || rl.TargetTotalTokens != 160000 {
		t.Errorf("比例缺省时不应收敛，实际 %d/%d", rl.TriggerTotalTokens, rl.TargetTotalTokens)
	}
}

// TestApplyCompressionRatios_target不小于trigger时兜底 验证极窄窗口的兜底
func TestApplyCompressionRatios_target不小于trigger时兜底(t *testing.T) {
	// trigger 绝对值小、target 绝对值大：收口后 target(550) >= trigger(100) 触发兜底
	specs := []ceiface.ProcessorSpec{
		{Type: "RoundLevelCompressor", Config: &compressor.RoundLevelCompressorConfig{
			TriggerTotalTokens: 100, TargetTotalTokens: 1000}},
	}
	out := applyCompressionRatios(specs, 1000, 0.85, 0.55)
	rl := out[0].Config.(*compressor.RoundLevelCompressorConfig)
	if rl.TargetTotalTokens >= rl.TriggerTotalTokens {
		t.Errorf("target(%d) 应小于 trigger(%d)", rl.TargetTotalTokens, rl.TriggerTotalTokens)
	}
}

// TestApplyCompressionRatios_offloader不参与 验证 MessageSummaryOffloader 的阈值不被缩放
func TestApplyCompressionRatios_offloader不参与(t *testing.T) {
	specs := []ceiface.ProcessorSpec{
		{Type: "MessageSummaryOffloader", Config: &offloader.MessageSummaryOffloaderConfig{
			TokensThreshold: 60000, LargeMessageThreshold: 60000}},
	}
	out := applyCompressionRatios(specs, 32000, 0.85, 0.55)
	mo := out[0].Config.(*offloader.MessageSummaryOffloaderConfig)
	if mo.TokensThreshold != 60000 || mo.LargeMessageThreshold != 60000 {
		t.Errorf("offloader 阈值不应被缩放，实际 %d/%d", mo.TokensThreshold, mo.LargeMessageThreshold)
	}
}

// TestShrinkThreshold_边界 覆盖 ratio/window/absolute 的边界分支
func TestShrinkThreshold_边界(t *testing.T) {
	cases := []struct {
		name     string
		absolute int
		window   int
		ratio    float64
		want     int
	}{
		{"ratio为0", 230000, 32000, 0, 230000},
		{"window为0", 230000, 0, 0.85, 230000},
		{"absolute为0", 0, 32000, 0.85, 0},
		{"比例值更小", 230000, 32000, 0.85, 27200},
		{"绝对值更小", 100000, 512000, 0.85, 100000},
		{"比例值取整为0", 230000, 1, 0.1, 230000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shrinkThreshold(tc.absolute, tc.window, tc.ratio); got != tc.want {
				t.Errorf("shrinkThreshold(%d,%d,%v) = %d，期望 %d",
					tc.absolute, tc.window, tc.ratio, got, tc.want)
			}
		})
	}
}

// TestUserProcessorTypes_返回注入顺序 覆盖访问器
func TestUserProcessorTypes_返回注入顺序(t *testing.T) {
	rail := NewContextProcessorRail(WithUserProcessors([]ceiface.ProcessorSpec{
		{Type: "DialogueCompressor"},
		{Type: "FullCompactProcessor"},
	}))
	got := rail.UserProcessorTypes()
	if len(got) != 2 || got[0] != "DialogueCompressor" || got[1] != "FullCompactProcessor" {
		t.Errorf("应返回注入顺序，实际 %v", got)
	}
}
