package tool_discovery

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchToolsInput search_tools 工具的输入参数。
// 对齐 Python: SearchToolsInput
type SearchToolsInput struct {
	// Query 搜索查询文本
	Query string `json:"query"`
	// Limit 返回候选工具的最大数量，默认 10，范围 [1, 20]
	Limit int `json:"limit"`
	// DetailLevel 详情级别：1=name+描述, 2=+参数摘要, 3=+完整参数
	DetailLevel int `json:"detail_level"`
}

// SearchToolsTool 搜索候选工具的元工具。
// 调用 searchToolsFn 执行搜索，再调用 appendTraceFn 记录搜索轨迹。
// 对齐 Python: SearchToolsTool
type SearchToolsTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
	// searchToolsFn 搜索工具回调
	searchToolsFn func(ctx context.Context, query string, limit int, detailLevel int) ([]map[string]any, error)
	// appendTraceFn 追加轨迹回调
	appendTraceFn func(session interfaces.SessionFacade, event map[string]any)
	// language 语言
	language string
	// agentID Agent 标识
	agentID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// searchToolsLimitMin limit 最小值
	searchToolsLimitMin = 1
	// searchToolsLimitMax limit 最大值
	searchToolsLimitMax = 20
	// searchToolsLimitDefault limit 默认值
	searchToolsLimitDefault = 10
	// searchToolsDetailLevelDefault detail_level 默认值
	searchToolsDetailLevelDefault = 1
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 SearchToolsTool 满足 Tool 接口
var _ tool.Tool = (*SearchToolsTool)(nil)

// logComponent 日志组件标识
var searchLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSearchToolsTool 创建搜索候选工具元工具。
// 对齐 Python: SearchToolsTool.__init__
func NewSearchToolsTool(
	searchFn func(ctx context.Context, query string, limit int, detailLevel int) ([]map[string]any, error),
	traceFn func(session interfaces.SessionFacade, event map[string]any),
	language string,
	agentID string,
) *SearchToolsTool {
	provider := &tools.SearchToolsMetadataProvider{}
	desc := provider.GetDescription(language)
	inputParams := buildSearchToolsInputParams()
	card := tool.NewToolCard("search_tools", desc, inputParams, map[string]any{
		"tool_id": "SearchToolsTool",
	})
	return &SearchToolsTool{
		card:          card,
		searchToolsFn: searchFn,
		appendTraceFn: traceFn,
		language:      language,
		agentID:       agentID,
	}
}

// Card 返回工具配置卡片。
func (t *SearchToolsTool) Card() *tool.ToolCard {
	return t.card
}

// Invoke 搜索候选工具，返回匹配结果。
// 对齐 Python: SearchToolsTool.invoke
func (t *SearchToolsTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 步骤 1：解析输入参数
	query, _ := inputs["query"].(string)
	limit := searchToolsLimitDefault
	if v, ok := inputs["limit"]; ok {
		switch n := v.(type) {
		case int:
			limit = n
		case float64:
			limit = int(n)
		case int64:
			limit = int(n)
		}
	}
	detailLevel := searchToolsDetailLevelDefault
	if v, ok := inputs["detail_level"]; ok {
		switch n := v.(type) {
		case int:
			detailLevel = n
		case float64:
			detailLevel = int(n)
		case int64:
			detailLevel = int(n)
		}
	}

	// 步骤 2：限幅 limit 到 [1, 20]
	limit = clampLimit(limit)

	// 步骤 3：调用搜索回调
	matches, err := t.searchToolsFn(ctx, query, limit, detailLevel)
	if err != nil {
		logger.Warn(searchLogComponent).
			Str("tool_name", "search_tools").
			Str("query", query).
			Int("limit", limit).
			Int("detail_level", detailLevel).
			Err(err).
			Msg("SearchToolsTool 搜索失败")
		return map[string]any{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	// 步骤 4：调用轨迹回调
	callOpts := tool.NewToolCallOptions(opts...)
	if t.appendTraceFn != nil && callOpts.Session != nil {
		if sess, ok := callOpts.Session.(interfaces.SessionFacade); ok {
			t.appendTraceFn(sess, map[string]any{
				"event_type": "tool_search",
				"query":      query,
				"limit":      limit,
				"count":      len(matches),
				"agent_id":   t.agentID,
			})
		}
	}

	// 步骤 5：日志
	logger.Info(searchLogComponent).
		Str("tool_name", "search_tools").
		Str("query", query).
		Int("limit", limit).
		Int("detail_level", detailLevel).
		Int("match_count", len(matches)).
		Msg("SearchToolsTool 搜索完成")

	// 步骤 6：返回结果
	return map[string]any{
		"query":            query,
		"matches":          matches,
		"count":            len(matches),
		"callability_note": t.buildCallabilityNote(),
		"next_step_hint":   t.buildNextStepHint(),
	}, nil
}

// Stream 不支持流式调用。
func (t *SearchToolsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported("search_tools")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSearchToolsInputParams 构建 search_tools 工具的输入参数。
func buildSearchToolsInputParams() []*schema.Param {
	return []*schema.Param{
		schema.NewStringParam("query", "搜索候选工具的查询文本", true),
		schema.NewIntegerParam("limit", "返回候选工具的最大数量", false, 10),
		schema.NewIntegerParam("detail_level", "1=name+描述, 2=+参数摘要, 3=+完整参数", false, 1),
	}
}

// clampLimit 将 limit 限制到 [1, 20] 范围内。
func clampLimit(limit int) int {
	if limit < searchToolsLimitMin {
		return searchToolsLimitMin
	}
	if limit > searchToolsLimitMax {
		return searchToolsLimitMax
	}
	return limit
}

// buildCallabilityNote 构建可调用性提示。
func (t *SearchToolsTool) buildCallabilityNote() string {
	if t.language == "cn" {
		return "搜索结果中的工具仅为发现用途，尚未可调用。请使用 load_tools 加载所需工具。"
	}
	return "Tools in search results are for discovery only and not yet callable. Use load_tools to make them callable."
}

// buildNextStepHint 构建下一步提示。
func (t *SearchToolsTool) buildNextStepHint() string {
	if t.language == "cn" {
		return "从搜索结果中选择需要的工具，然后调用 load_tools 加载它们。"
	}
	return "Select the tools you need from search results, then call load_tools to load them."
}

// formatMatches 格式化匹配结果用于日志。
func formatMatches(matches []map[string]any) string {
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if name, ok := m["name"].(string); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "[]"
	}
	return fmt.Sprintf("%v", names)
}
