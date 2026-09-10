package tools

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamTool 团队工具基类。
// Python: TeamTool(Tool, ABC)
// 子类嵌入 TeamTool 获得 Card() 默认实现，只需实现 Invoke()。
type TeamTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamTool 创建 TeamTool 实例。
func NewTeamTool(card *tool.ToolCard) TeamTool {
	return TeamTool{card: card}
}

// Card 返回工具配置卡片。
func (t *TeamTool) Card() *tool.ToolCard {
	return t.card
}

// ──────────────────────────── 非导出函数 ────────────────────────────
