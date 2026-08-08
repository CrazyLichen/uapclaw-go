package web_tools

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateWebTools 创建 Web 工具集
// 对齐 Python: create_web_tools() (web_tools.py L1635-1652)
func CreateWebTools(language, agentID string, includeFreeSearch, includePaidSearch, includeFetchWebpage bool) []tool.Tool {
	var tools []tool.Tool
	// 对齐 Python: L1645-1646 — 付费搜索优先
	if includePaidSearch && IsPaidSearchEnabled() {
		tools = append(tools, NewWebPaidSearchTool(language, agentID))
	}
	// 对齐 Python: L1647-1648
	if includeFreeSearch && IsFreeSearchEnabled() {
		tools = append(tools, NewWebFreeSearchTool(language, agentID))
	}
	// 对齐 Python: L1649-1650
	if includeFetchWebpage {
		tools = append(tools, NewWebFetchWebpageTool(language, agentID))
	}
	return tools
}

// ──────────────────────────── 非导出函数 ────────────────────────────
