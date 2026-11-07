package tool_discovery

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolSearchInput tool_search 工具的输入参数。
// Python: tool_search.py (ToolSearchInput)
type ToolSearchInput struct {
	// Query 搜索查询文本
	Query string `json:"query"`
	// Limit 返回候选工具的最大数量，缺省用工具默认值
	Limit int `json:"limit"`
}

// ToolSearchTool 模型可见的延迟工具搜索元工具。
// Python: harness/tools/tool_discovery/tool_search.py (ToolSearchTool)
type ToolSearchTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// search 检索回调（query, limit, session）
	search func(ctx context.Context, query string, limit int, sess sessioninterfaces.SessionFacade) ([]map[string]any, error)
	// resultLimit 默认结果条数
	resultLimit int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultToolSearchLimit tool_search 默认结果条数
	DefaultToolSearchLimit = 5
	// MaxToolSearchLimit tool_search 结果条数上限
	MaxToolSearchLimit = 20
	// mappedContentKey 模型可见文本键（BuildToolMessageContent 优先消费）
	mappedContentKey = "__mapped_content"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeToolSearchLimit 把请求条数夹到 1..20。
// Python: _normalize_tool_search_limit
func NormalizeToolSearchLimit(value int) int {
	if value < 1 {
		return 1
	}
	if value > MaxToolSearchLimit {
		return MaxToolSearchLimit
	}
	return value
}

// NewToolSearchTool 创建搜索元工具。
// Python: ToolSearchTool.__init__
func NewToolSearchTool(
	search func(ctx context.Context, query string, limit int, sess sessioninterfaces.SessionFacade) ([]map[string]any, error),
	language string,
	agentID string,
	resultLimit int,
) *ToolSearchTool {
	card, err := tools.BuildToolCard("tool_search", "ToolSearchTool", language, nil, agentID)
	if err != nil {
		// Python: build_tool_card 失败时退化为同名卡片
		card = tool.NewToolCardWithID("ToolSearchTool", "tool_search", "tool_search", nil, nil)
	}
	card.Exposure = tool.ToolExposureDirect
	card.SetExposureDeclared(true)
	return &ToolSearchTool{
		card:        card,
		search:      search,
		resultLimit: NormalizeToolSearchLimit(resultLimit),
	}
}

// Card 返回工具卡片。
func (t *ToolSearchTool) Card() *tool.ToolCard { return t.card }

// GetCallTimeout 返回外层调用预算，沿用卡片级策略。
func (t *ToolSearchTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return tool.BaseCallTimeout(inputs, defaultTimeout)
}

// Invoke 执行搜索。
// Python: ToolSearchTool.invoke
func (t *ToolSearchTool) Invoke(ctx context.Context, inputs map[string]any, options ...tool.ToolOption) (map[string]any, error) {
	opts := &tool.ToolCallOptions{}
	for _, option := range options {
		option(opts)
	}

	query, _ := inputs["query"].(string)
	query = strings.TrimSpace(query)
	limit := t.resultLimit
	if raw, ok := inputs["limit"]; ok {
		if value, ok := toInt(raw); ok {
			limit = NormalizeToolSearchLimit(value)
		}
	}

	results, err := t.search(ctx, query, limit, opts.Session)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "tool_search_invoke_failed").
			Str("query", query).
			Err(err).
			Msg("tool_search invoke failed")
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	if len(results) > limit {
		results = results[:limit]
	}

	logger.Info(logComponent).
		Str("event_type", "tool_search_invoked").
		Str("query", query).
		Int("limit", limit).
		Int("match_count", len(results)).
		Msg("tool_search invoked")

	payload := map[string]any{
		"query":   query,
		"results": results,
		"count":   len(results),
		"success": true,
	}
	payload[mappedContentKey] = RenderToolSearchForLLM(query, results)
	return payload, nil
}

// Stream 不支持流式。
// Python: ToolSearchTool.stream（空实现）
func (t *ToolSearchTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// RenderToolSearchForLLM 渲染匹配结果的模型可见文本。
// 每个结果给出名称、描述与精确 JSON parameters schema。
// Python: ToolSearchTool.render_for_llm
func RenderToolSearchForLLM(query string, results []map[string]any) string {
	if len(results) == 0 {
		return "No tools matched \"" + query + "\"."
	}
	sections := make([]string, 0, len(results))
	for _, item := range results {
		name, _ := item["name"].(string)
		description, _ := item["description"].(string)
		payload, err := json.Marshal(item["parameters"])
		if err != nil {
			payload = []byte("{}")
		}
		sections = append(sections, "## "+name+"\n"+description+"\nParameters: "+string(payload))
	}
	return strconv.Itoa(len(results)) + " tool(s) matched \"" + query + "\".\n\n" + strings.Join(sections, "\n\n")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toInt 把输入值归一化为 int。
func toInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	}
	return 0, false
}
