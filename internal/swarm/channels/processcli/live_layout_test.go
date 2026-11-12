package processcli

import (
	"strings"
	"testing"
)

// TestLiveTurnLayout_空请求归一 空白请求替换为占位文案。
// Python: live_layout.py:27 request_text.strip() or "（空请求）"
func TestLiveTurnLayout_空请求归一(t *testing.T) {
	if got := NewLiveTurnLayout("   ").RequestText; got != "（空请求）" {
		t.Errorf("空请求 = %q, 期望 （空请求）", got)
	}
	if got := NewLiveTurnLayout("do it").RequestText; got != "do it" {
		t.Errorf("正常请求 = %q", got)
	}
}

// TestLiveTurnLayout_applyReceipt只更新首个pending。
// Python: live_layout.py:44-53
func TestLiveTurnLayout_applyReceipt只更新首个pending(t *testing.T) {
	layout := NewLiveTurnLayout("req")
	layout.AddSupplement("first")
	layout.AddSupplement("second")

	layout.ApplyReceipt(supplementStatusAccepted)
	if layout.Supplements[0].Status != supplementStatusAccepted {
		t.Errorf("首个 supplement = %q, 期望 accepted", layout.Supplements[0].Status)
	}
	if layout.Supplements[1].Status != supplementStatusPending {
		t.Errorf("第二个 supplement = %q, 期望 pending", layout.Supplements[1].Status)
	}

	layout.ApplyReceipt(supplementStatusRejected)
	if layout.Supplements[1].Status != supplementStatusRejected {
		t.Errorf("第二个 supplement = %q, 期望 rejected", layout.Supplements[1].Status)
	}
}

// TestLiveTurnLayout_applyReceipt非法状态转unknown。
func TestLiveTurnLayout_applyReceipt非法状态转unknown(t *testing.T) {
	layout := NewLiveTurnLayout("req")
	layout.AddSupplement("x")
	layout.ApplyReceipt("bogus")
	if layout.Supplements[0].Status != supplementStatusUnknown {
		t.Errorf("非法状态应转 unknown，得 %q", layout.Supplements[0].Status)
	}
}

// TestLiveTurnLayout_applyReceipt无pending 无 pending 时不 panic。
func TestLiveTurnLayout_applyReceipt无pending(t *testing.T) {
	layout := NewLiveTurnLayout("req")
	layout.ApplyReceipt(supplementStatusAccepted)
	if len(layout.Supplements) != 0 {
		t.Error("不应凭空产生 supplement")
	}
}

// TestLiveTurnLayout_finalText三态标记。
func TestLiveTurnLayout_finalText三态标记(t *testing.T) {
	layout := NewLiveTurnLayout("do it")
	layout.AddSupplement("s1")
	layout.ApplyReceipt(supplementStatusAccepted)
	layout.AddSupplement("s2")
	layout.ApplyReceipt(supplementStatusRejected)
	layout.AddSupplement("s3")
	layout.ApplyReceipt("bogus")
	layout.AddNotice("notice-1")

	text := layout.FinalText()
	for _, want := range []string{"do it", "s1", "s2", "s3", "notice-1", "✓", "×", "!", "›"} {
		if !strings.Contains(text, want) {
			t.Errorf("FinalText 缺 %q：\n%s", want, text)
		}
	}
}

// TestLiveTurnLayout_不含OutputText 偏差 E-9：不缓存模型输出。
func TestLiveTurnLayout_不含OutputText(t *testing.T) {
	layout := NewLiveTurnLayout("req")
	text := layout.FinalText()
	if strings.Contains(text, "UAPClaw") {
		t.Error("FinalText 不应含模型输出块（偏差 E-9）")
	}
	if strings.Contains(text, "正在处理") {
		t.Error("FinalText 不应含处理状态（偏差 E-9）")
	}
}

// TestLiveTurnLayout_pending标记 未收到回执时为 ↳。
func TestLiveTurnLayout_pending标记(t *testing.T) {
	layout := NewLiveTurnLayout("req")
	layout.AddSupplement("waiting")
	text := layout.FinalText()
	if !strings.Contains(text, "↳ waiting") {
		t.Errorf("pending 标记缺失：%q", text)
	}
}
