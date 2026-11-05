package rails

import (
	"strings"
	"testing"
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
