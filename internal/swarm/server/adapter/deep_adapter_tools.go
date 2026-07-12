package adapter

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// syncToolGroup 同步工具组。
// 对齐 Python: _sync_tool_group() (line 1319-1350)
// ⤵️ agentcore: 需要实例化后的 AbilityManager
func (d *DeepAdapter) syncToolGroup(toolGroup string, configBase map[string]any) {
	// ⤵️ agentcore: 调用 AbilityManager.Add/Remove 同步工具组
	logger.Info(logComponent).Str("tool_group", toolGroup).Msg("syncToolGroup 等待回填")
}

// removeRegisteredTools 移除已注册的工具。
// 对齐 Python: _remove_registered_tools() (line 1351-1380)
func (d *DeepAdapter) removeRegisteredTools(toolIDs []string) {
	// ⤵️ agentcore: 调用 AbilityManager.Remove
	logger.Info(logComponent).Int("count", len(toolIDs)).Msg("removeRegisteredTools 等待回填")
}

// appendToolCard 追加工具卡片。
// 对齐 Python: _append_tool_card() (line 1381-1410)
func (d *DeepAdapter) appendToolCard(cards []any) {
	// ⤵️ agentcore: 调用 AbilityManager.Add
	logger.Info(logComponent).Int("count", len(cards)).Msg("appendToolCard 等待回填")
}

// prioritizePaidSearchToolCard 优先付费搜索工具卡片。
// 对齐 Python: _prioritize_paid_search_tool_card() (line 1411-1440)
func (d *DeepAdapter) prioritizePaidSearchToolCard(cards []any) []any {
	// ⤵️ agentcore: 付费搜索工具优先排序
	return cards
}

// pruneToolCards 裁剪工具卡片。
// 对齐 Python: _prune_tool_cards() (line 1441-1476)
func (d *DeepAdapter) pruneToolCards(cards []any, mode string) []any {
	// ⤵️ agentcore: 按模式裁剪工具卡片
	return cards
}

// syncMultimodalToolsForRuntime 热同步多模态工具。
// 对齐 Python: _sync_multimodal_tools_for_runtime() (line 1170-1238)
func (d *DeepAdapter) syncMultimodalToolsForRuntime() {
	// ⤵️ agentcore: 根据 visionModelConfig/audioModelConfig 注册/注销多模态工具
	if d.visionModelConfig != nil && !d.visionToolsRegistered {
		logger.Info(logComponent).Msg("视觉模型配置已就绪，等待工具注册回填")
	}
	if d.audioModelConfig != nil && !d.audioToolsRegistered {
		logger.Info(logComponent).Msg("音频模型配置已就绪，等待工具注册回填")
	}
}

// syncPaidSearchToolForRuntime 热同步付费搜索工具。
// 对齐 Python: _sync_paid_search_tool_for_runtime() (line 1240-1270)
func (d *DeepAdapter) syncPaidSearchToolForRuntime() {
	// ⤵️ agentcore: 付费搜索工具热同步
	logger.Info(logComponent).Msg("syncPaidSearchToolForRuntime 等待回填")
}
