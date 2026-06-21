package context_engine

import (
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StatContextWindow 统计 ContextWindow 的完整统计信息。
//
// 内部调用 StatMessages + StatTools + 计算对话轮次，填充 window.Statistic 各字段。
// 对应 Python: Context._stat_context_window(window)
//
// ⤵️ 待 5.31 Context 具体实现时回填实际统计逻辑
func StatContextWindow(window *iface.ContextWindow, tokenCounter token.TokenCounter) {
	// ⤵️ 待 5.31 回填：统计窗口消息 + 工具 + 对话轮次
	// 参见 Python: openjiuwen/core/context_engine/context/context.py (_stat_context_window)
	//
	// 实现要点：
	//   1. window.Statistic.StatMessages(window.GetMessages(), tokenCounter)
	//   2. window.Statistic.StatTools(window.GetTools(), tokenCounter)
	//   3. window.Statistic.TotalDialogues = 计算对话轮次（依赖 ContextUtils.FindAllDialogueRound）
}

// ──────────────────────────── 非导出函数 ────────────────────────────
