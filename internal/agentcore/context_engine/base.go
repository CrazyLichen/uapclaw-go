package context_engine

import (
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// StatContextWindow 统计 ContextWindow 的完整统计信息。
//
// 内部调用 StatMessages + StatTools + 计算对话轮次，填充 window.Statistic 各字段。
// 对应 Python: Context._stat_context_window(window)
func StatContextWindow(window *iface.ContextWindow, tokenCounter token.TokenCounter) {
	window.Statistic.StatMessages(window.GetMessages(), tokenCounter)
	window.Statistic.StatTools(window.GetTools(), tokenCounter)
	window.Statistic.TotalDialogues = len(processor.FindAllDialogueRound(window.GetMessages()))
}

// ──────────────────────────── 非导出函数 ────────────────────────────
