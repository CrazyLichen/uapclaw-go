package rails

import (
	"context"
	"strconv"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewModelAnomalyDetectionRail_默认值 校验默认参数与上游一致（L329-391）。
func TestNewModelAnomalyDetectionRail_默认值(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if r.Priority() != 70 {
		t.Fatalf("priority 期望 70，实际 %d", r.Priority())
	}
	if r.maxRetries != 2 || r.repeatMinPatternChars != 2 || r.repeatMaxPatternChars != 64 ||
		r.repeatMinCount != 6 || r.repeatMinTotalChars != 160 || r.repeatWindowChars != 1024 ||
		r.singleCharRepeatCount != 100 {
		t.Fatalf("默认参数与上游不一致: %+v", r)
	}
	if r.transientMaxRetries != 3 || r.transientBaseDelay != 2.0 {
		t.Fatalf("瞬时重试默认值与上游不一致: %d / %v", r.transientMaxRetries, r.transientBaseDelay)
	}
	if len(r.backoffSeconds) != 3 || r.backoffSeconds[0] != 0.5 || r.backoffSeconds[2] != 2.0 {
		t.Fatalf("退避表默认值应为 0.5/1/2，实际 %v", r.backoffSeconds)
	}
	if r.toolLoopCompact.Enabled {
		t.Fatal("工具循环压缩默认必须关闭（对齐上游 enabled=False）")
	}
	if r.toolLoopCompact.ConsecutiveThreshold != 4 || r.toolLoopCompact.BailoutThreshold != 3 {
		t.Fatalf("压缩阈值默认值应为 4/3，实际 %d/%d",
			r.toolLoopCompact.ConsecutiveThreshold, r.toolLoopCompact.BailoutThreshold)
	}
}

// TestNewModelAnomalyDetectionRail_十条校验 逐条覆盖上游 10 个 ValueError。
func TestNewModelAnomalyDetectionRail_十条校验(t *testing.T) {
	cases := []struct {
		name string
		opt  ModelAnomalyOption
		want string
	}{
		{"max_retries 为负", WithMaxRetries(-1), "max_retries must be >= 0"},
		{"transient_max_retries 为负", WithTransientMaxRetries(-1), "transient_max_retries must be >= 0"},
		{"base_delay 为负", WithTransientBaseDelaySeconds(-0.1), "transient_base_delay_seconds must be >= 0"},
		{"退避表含负数", WithBackoffSeconds([]float64{0.5, -1}), "backoff_seconds entries must be >= 0"},
		{"重复片段下限为 0", WithRepeatMinPatternChars(0), "repeat_min_pattern_chars must be >= 1"},
		{"上限小于下限", WithRepeatParams(8, 4, 6, 160, 1024), "repeat_max_pattern_chars must be >= repeat_min_pattern_chars"},
		{"重复次数为 1", WithRepeatMinCount(1), "repeat_min_count must be >= 2"},
		{"最小总长为 0", WithRepeatMinTotalChars(0), "repeat_min_total_chars must be >= 1"},
		{"单字符阈值为 1", WithSingleCharRepeatCount(1), "single_char_repeat_count must be >= 2"},
		{"窗口过小", WithRepeatWindowChars(100), "repeat_window_chars is too small"},
	}
	for _, c := range cases {
		_, err := NewModelAnomalyDetectionRail(c.opt)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: 期望错误含 %q，实际 %v", c.name, c.want, err)
		}
	}
}

// TestNewModelAnomalyDetectionRail_退避表越界取末项 验证 backoff 语义（L408-415）。
func TestNewModelAnomalyDetectionRail_退避表越界取末项(t *testing.T) {
	r, err := NewModelAnomalyDetectionRail(WithBackoffSeconds([]float64{0.5, 1.0}))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := r.BackoffDelay(0); got != 0.5 {
		t.Fatalf("第 0 次退避应为 0.5，实际 %v", got)
	}
	if got := r.BackoffDelay(5); got != 1.0 {
		t.Fatalf("越界应取末项 1.0，实际 %v", got)
	}
}

// ──────────────────────────── 重复检测测试辅助 ────────────────────────────

// newTestCBC 构造不带会话的空回调上下文。
func newTestCBC() *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
}

// feedChunk 向检查器投递一个分片。
func feedChunk(r *ModelAnomalyDetectionRail, cbc *agentinterfaces.AgentCallbackContext, reasoning, content string) error {
	return r.InspectStreamChunk(context.Background(), cbc,
		&llmschema.AssistantMessageChunk{AssistantMessage: llmschema.AssistantMessage{
			DefaultMessage:   llmschema.DefaultMessage{Content: llmschema.NewTextContent(content)},
			ReasoningContent: reasoning,
		}})
}

// noiseText 生成不含重复后缀的噪声文本，用于验证尾窗滚动。
func noiseText(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		b.WriteString(strconv.Itoa(i*7919 + 13))
		b.WriteByte(';')
	}
	return b.String()
}

// TestInspectStreamChunk_多字符重复触发 54 次 abc → 命中多字符检测（上游默认阈值）。
func TestInspectStreamChunk_多字符重复触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	var err error
	for i := 0; i < 54 && err == nil; i++ {
		err = feedChunk(r, cbc, "abc", "")
	}
	if err == nil {
		t.Fatal("54 次 abc 应触发重复检测")
	}
	msg := err.Error()
	for _, want := range []string{repeatErrorMarker, "field=reasoning_content", "unit='abc'", "repeat_count="} {
		if !strings.Contains(msg, want) {
			t.Errorf("文案缺少 %q: %s", want, msg)
		}
	}
}

// TestInspectStreamChunk_短重复不触发 5 次 abc（15 字符）不达总长阈值。
func TestInspectStreamChunk_短重复不触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	for i := 0; i < 5; i++ {
		if err := feedChunk(r, cbc, "", "abc"); err != nil {
			t.Fatalf("不应触发: %v", err)
		}
	}
}

// TestInspectStreamChunk_单字符重复触发 100 个 '-' 命中单字符阈值。
func TestInspectStreamChunk_单字符重复触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	var err error
	for i := 0; i < 100 && err == nil; i++ {
		err = feedChunk(r, cbc, "", "-")
	}
	if err == nil {
		t.Fatal("100 个 '-' 应触发单字符重复检测")
	}
	if !strings.Contains(err.Error(), "unit='-'") {
		t.Fatalf("文案应含 unit='-'，实际 %s", err.Error())
	}
}

// TestInspectStreamChunk_空白后缀不触发 空白字符不参与重复判定。
func TestInspectStreamChunk_空白后缀不触发(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	for i := 0; i < 200; i++ {
		if err := feedChunk(r, cbc, "", " "); err != nil {
			t.Fatalf("空白重复不应触发: %v", err)
		}
	}
}

// TestInspectStreamChunk_两字段独立窗口 两个字段各自累计、互不影响。
func TestInspectStreamChunk_两字段独立窗口(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	for i := 0; i < 3; i++ {
		if err := feedChunk(r, cbc, "xy", "zz"); err != nil {
			t.Fatalf("不应触发: %v", err)
		}
	}
	raw, _ := cbc.GetExtra(modelAnomalyStateKey)
	state := raw.(*anomalyState)
	if state.content != "zzzzzz" || state.reasoningContent != "xyxyxy" {
		t.Fatalf("两字段应各自累计，实际 content=%q reasoning=%q", state.content, state.reasoningContent)
	}
}

// TestInspectStreamChunk_尾窗滚动仍能检出 噪声超过窗口后仍能检出尾部重复。
func TestInspectStreamChunk_尾窗滚动仍能检出(t *testing.T) {
	r, _ := NewModelAnomalyDetectionRail()
	cbc := newTestCBC()

	if err := feedChunk(r, cbc, "", noiseText(2000)); err != nil {
		t.Fatalf("噪声不应触发: %v", err)
	}
	var err error
	for i := 0; i < 60 && err == nil; i++ {
		err = feedChunk(r, cbc, "", "abc")
	}
	if err == nil {
		t.Fatal("滚动窗口内仍应检出")
	}
}
