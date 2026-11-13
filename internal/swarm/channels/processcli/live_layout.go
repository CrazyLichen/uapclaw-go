package processcli

import (
	"strings"
)

// 本文件是 channels/process_cli/live_layout.py 的 Go 对应物。
//
// Python: channels/process_cli/live_layout.py（136 行）
//
// 偏差 E-9：上游用 prompt_toolkit 的 alternate screen 在全屏里整体渲染
// 三段（请求 / 补充输入 / 模型输出），本实现退化为「流式输出 + 底部输入行」：
//   - 不含 OutputText 字段：模型输出由 relayWorkerOutput 直接写 stdout
//   - 不含 Message()：无全屏渲染
//   - FinalText() 只输出「请求 + 补充输入三态 + 提示」摘要
//
// 保留的能力：补充输入三态标记、回执与 supplement 的配对、回执前缀识别。

// ──────────────────────────── 结构体 ────────────────────────────

// Supplement 一条补充输入及其投递状态。
// Python: live_layout.py:16-20 _Supplement
type Supplement struct {
	// Text 输入原文
	Text string
	// Status æéç¶æï¼pending / accepted / rejected / unknown
	Status string
}

// LiveTurnLayout 一轮交互的输入、补充与提示快照。
// Python: live_layout.py:22-119 LiveTurnLayout（偏差 E-9：去掉 output_text）
type LiveTurnLayout struct {
	// RequestText 本轮请求原文（空请求已替换为「（空请求）」）
	RequestText string
	// Supplements 补充输入列表
	Supplements []Supplement
	// Notices 本地提示（不支持的命令、用法提示等）
	Notices []string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ForwardedReceiptPrefix worker stderr 上的补充输入回执前缀。
	// Python: live_layout.py:14 FORWARDED_RECEIPT_PREFIX
	ForwardedReceiptPrefix = "JIUWEN_PROCESS_CLI_RECEIPT "
	// supplementStatusPending 待投递
	supplementStatusPending = "pending"
	// supplementStatusAccepted 已接受
	supplementStatusAccepted = "accepted"
	// supplementStatusRejected 已拒绝
	supplementStatusRejected = "rejected"
	// supplementStatusUnknown 状态未知
	supplementStatusUnknown = "unknown"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLiveTurnLayout 构造一轮布局。
// Python: live_layout.py:25-31 LiveTurnLayout.__init__
func NewLiveTurnLayout(requestText string) *LiveTurnLayout {
	trimmed := strings.TrimSpace(requestText)
	if trimmed == "" {
		trimmed = "（空请求）"
	}
	return &LiveTurnLayout{RequestText: trimmed}
}

// AddSupplement 追加一条待投递的补充输入。
// Python: live_layout.py:36-38 add_supplement
func (l *LiveTurnLayout) AddSupplement(text string) {
	l.Supplements = append(l.Supplements, Supplement{Text: text, Status: supplementStatusPending})
}

// AddNotice 追加一条本地提示。
// Python: live_layout.py:40-42 add_notice
func (l *LiveTurnLayout) AddNotice(text string) {
	l.Notices = append(l.Notices, text)
}

// ApplyReceipt 把回执状态应用到第一条 pending 的补充输入。
// Python: live_layout.py:44-53 apply_receipt
//
// 只更新第一个 pending（FIFO 配对）；非法状态归一为 unknown。
func (l *LiveTurnLayout) ApplyReceipt(status string) {
	normalized := supplementStatusUnknown
	switch status {
	case supplementStatusAccepted:
		normalized = supplementStatusAccepted
	case supplementStatusRejected:
		normalized = supplementStatusRejected
	case supplementStatusUnknown:
		normalized = supplementStatusUnknown
	}
	for index := range l.Supplements {
		if l.Supplements[index].Status != supplementStatusPending {
			continue
		}
		l.Supplements[index].Status = normalized
		return
	}
}

// FinalText 返回本轮摘要（偏差 E-9：不含模型输出全文）。
// Python: live_layout.py:90-107 final_text（原含 output_text 段）
//
// 只输出「请求 + 补充输入三态 + 提示」；模型输出已在 scrollback 里。
func (l *LiveTurnLayout) FinalText() string {
	var builder strings.Builder
	builder.WriteString("› ")
	builder.WriteString(l.RequestText)
	builder.WriteString("\n")
	for _, supplement := range l.Supplements {
		builder.WriteString("  ")
		builder.WriteString(supplementMarker(supplement.Status))
		builder.WriteString(" ")
		builder.WriteString(supplement.Text)
		builder.WriteString("\n")
	}
	for _, notice := range l.Notices {
		builder.WriteString("  ! ")
		builder.WriteString(notice)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	return builder.String()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// supplementMarker 补充输入状态的三态标记。
// Python: live_layout.py:73-86 marker 映射
func supplementMarker(status string) string {
	switch status {
	case supplementStatusAccepted:
		return "✓"
	case supplementStatusRejected:
		return "×"
	case supplementStatusUnknown:
		return "!"
	default:
		return "↳"
	}
}
