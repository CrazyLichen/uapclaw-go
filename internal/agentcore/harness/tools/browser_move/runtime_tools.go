package browser_move

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserCancelTool 取消正在进行的浏览器任务。
//
// 对齐 Python: BrowserCancelTool
type BrowserCancelTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// BrowserClearCancelTool 清除浏览器任务的取消标记。
//
// 对齐 Python: BrowserClearCancelTool
type BrowserClearCancelTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// BrowserCustomActionTool 运行已注册的自定义浏览器动作。
//
// 对齐 Python: BrowserCustomActionTool
type BrowserCustomActionTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// BrowserListActionsTool 列出可用的自定义浏览器动作。
//
// 对齐 Python: BrowserListActionsTool
type BrowserListActionsTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// BrowserProbeInteractivesTool 紧凑可见交互元素探测。
//
// 对齐 Python: BrowserProbeInteractivesTool
type BrowserProbeInteractivesTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// BrowserProbeCardsTool 紧凑重复卡片/列表结构探测。
//
// 对齐 Python: BrowserProbeCardsTool
type BrowserProbeCardsTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// BrowserRuntimeHealthTool 返回运行时健康状态。
//
// 对齐 Python: BrowserRuntimeHealthTool
type BrowserRuntimeHealthTool struct {
	card    *tool.ToolCard
	runtime *BrowserAgentRuntime
}

// ──────────────────────────── 枚举 ────────────────────────────

// json_number 兼容 json.Number 类型
type json_number = interface{ Int64() (int64, error) }

// ──────────────────────────── 常量 ────────────────────────────

const (
	// cancelDesc 取消工具描述
	// 对齐 Python: _CANCEL_DESC
	cancelDesc = "Cancel an in-progress browser task by session_id. " +
		"Optionally pass request_id to target a specific request within the session. " +
		"Returns JSON with ok/session_id/request_id/error."

	// clearCancelDesc 清除取消标记工具描述
	// 对齐 Python: _CLEAR_CANCEL_DESC
	clearCancelDesc = "Clear the cancellation flag for a browser session or request. " +
		"Returns JSON with ok/session_id/request_id/error."

	// customActionDesc 自定义动作工具描述
	// 对齐 Python: _CUSTOM_ACTION_DESC
	customActionDesc = "Run a registered custom browser action by name. " +
		"Use for deterministic helpers such as drag-and-drop or coordinate resolution " +
		"alongside the direct Playwright MCP browser tools. " +
		"Call browser_list_custom_actions first to discover available actions and parameters. " +
		"Aliases source/target and source_x/source_y/target_x/target_y are accepted."

	// listActionsDesc 列表动作工具描述
	// 对齐 Python: _LIST_ACTIONS_DESC
	listActionsDesc = "List available custom browser actions and detailed parameter guidance " +
		"for browser_custom_action."

	// runtimeHealthDesc 健康状态工具描述
	// 对齐 Python: _RUNTIME_HEALTH_DESC
	runtimeHealthDesc = "Return runtime readiness, heartbeat status, and selected provider/model configuration."

	// probeInteractivesDesc 交互探测工具描述
	// 对齐 Python: _PROBE_INTERACTIVES_DESC
	probeInteractivesDesc = "Return a compact list of visible, high-value interactive elements on the current page. " +
		"Use this for page-level controls such as buttons, links, inputs, forms, navigation, login, " +
		"pagination, menus, and visible actions. Prefer max_items around 20-30 unless a larger inventory " +
		"is needed. For product/search/listing card data, prefer browser_probe_cards first. " +
		"The result includes role/text/aria-label/testid/bbox/selector_hint for likely controls."

	// probeCardsDesc 卡片探测工具描述
	// 对齐 Python: _PROBE_CARDS_DESC
	probeCardsDesc = "Return compact repeated card/listing structures from the current page. " +
		"Use this first on product pages, marketplace pages, search-result pages, catalog pages, " +
		"article-list pages, or any page with repeated visible cards/listings. " +
		"The result includes candidate card title, price, rating, review count, availability, " +
		"primary link, visible buttons, bbox, selector_hint, and recurring structure signatures. " +
		"This should usually be preferred over browser_probe_interactives for product/listing/item-data tasks."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserCancelTool 创建取消工具。
func NewBrowserCancelTool(runtime *BrowserAgentRuntime) *BrowserCancelTool {
	return &BrowserCancelTool{
		card:    newCancelToolCard(),
		runtime: runtime,
	}
}

// NewBrowserClearCancelTool 创建清除取消标记工具。
func NewBrowserClearCancelTool(runtime *BrowserAgentRuntime) *BrowserClearCancelTool {
	return &BrowserClearCancelTool{
		card:    newClearCancelToolCard(),
		runtime: runtime,
	}
}

// NewBrowserCustomActionTool 创建自定义动作工具。
func NewBrowserCustomActionTool(runtime *BrowserAgentRuntime) *BrowserCustomActionTool {
	return &BrowserCustomActionTool{
		card:    newCustomActionToolCard(),
		runtime: runtime,
	}
}

// NewBrowserListActionsTool 创建列表动作工具。
func NewBrowserListActionsTool(runtime *BrowserAgentRuntime) *BrowserListActionsTool {
	return &BrowserListActionsTool{
		card:    newListActionsToolCard(),
		runtime: runtime,
	}
}

// NewBrowserProbeInteractivesTool 创建交互探测工具。
func NewBrowserProbeInteractivesTool(runtime *BrowserAgentRuntime) *BrowserProbeInteractivesTool {
	return &BrowserProbeInteractivesTool{
		card:    newProbeInteractivesToolCard(),
		runtime: runtime,
	}
}

// NewBrowserProbeCardsTool 创建卡片探测工具。
func NewBrowserProbeCardsTool(runtime *BrowserAgentRuntime) *BrowserProbeCardsTool {
	return &BrowserProbeCardsTool{
		card:    newProbeCardsToolCard(),
		runtime: runtime,
	}
}

// NewBrowserRuntimeHealthTool 创建健康状态工具。
func NewBrowserRuntimeHealthTool(runtime *BrowserAgentRuntime) *BrowserRuntimeHealthTool {
	return &BrowserRuntimeHealthTool{
		card:    newRuntimeHealthToolCard(),
		runtime: runtime,
	}
}

// BuildBrowserRuntimeTools 构建所有浏览器运行时辅助工具。
//
// 对齐 Python: build_browser_runtime_tools
func BuildBrowserRuntimeTools(runtime *BrowserAgentRuntime) []tool.Tool {
	return []tool.Tool{
		NewBrowserCancelTool(runtime),
		NewBrowserClearCancelTool(runtime),
		NewBrowserCustomActionTool(runtime),
		NewBrowserListActionsTool(runtime),
		NewBrowserProbeInteractivesTool(runtime),
		NewBrowserProbeCardsTool(runtime),
		NewBrowserRuntimeHealthTool(runtime),
	}
}

// ── BrowserCancelTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserCancelTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行取消操作。
func (t *BrowserCancelTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	_ = t.runtime.EnsureRuntimeReady(ctx)
	sessionID := fmt.Sprintf("%v", inputs["session_id"])
	requestID := ""
	if v, ok := inputs["request_id"]; ok && v != nil {
		requestID = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	result := t.runtime.CancelRun(ctx, sessionID, requestID)
	return result, nil
}

// Stream 不支持流式调用。
func (t *BrowserCancelTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ── BrowserClearCancelTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserClearCancelTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行清除取消操作。
func (t *BrowserClearCancelTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	_ = t.runtime.EnsureRuntimeReady(ctx)
	sessionID := fmt.Sprintf("%v", inputs["session_id"])
	requestID := ""
	if v, ok := inputs["request_id"]; ok && v != nil {
		requestID = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	result := t.runtime.ClearCancel(ctx, sessionID, requestID)
	return result, nil
}

// Stream 不支持流式调用。
func (t *BrowserClearCancelTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ── BrowserCustomActionTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserCustomActionTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行自定义动作。
func (t *BrowserCustomActionTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	action := fmt.Sprintf("%v", inputs["action"])
	sessionID := ""
	if v, ok := inputs["session_id"]; ok && v != nil {
		sessionID = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	requestID := ""
	if v, ok := inputs["request_id"]; ok && v != nil {
		requestID = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	params := map[string]any{}
	if p, ok := inputs["params"].(map[string]any); ok {
		params = p
	}
	result := t.runtime.RunCustomAction(ctx, action, sessionID, requestID, params)
	return result, nil
}

// Stream 不支持流式调用。
func (t *BrowserCustomActionTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ── BrowserListActionsTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserListActionsTool) Card() *tool.ToolCard { return t.card }

// Invoke 列出所有可用动作。
func (t *BrowserListActionsTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return t.runtime.ListActions(), nil
}

// Stream 不支持流式调用。
func (t *BrowserListActionsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ── BrowserProbeInteractivesTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserProbeInteractivesTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行交互元素探测。
func (t *BrowserProbeInteractivesTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	maxItems := 50
	if v, ok := inputs["max_items"]; ok && v != nil {
		if n, err := parseInt(v); err == nil {
			maxItems = clampInt(n, 1, 100)
		}
	}
	viewportOnly := true
	if v, ok := inputs["viewport_only"]; ok {
		viewportOnly = parseBool(v, true)
	}
	query := ""
	if v, ok := inputs["query"]; ok && v != nil {
		query = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return t.runtime.ProbeInteractives(ctx, maxItems, viewportOnly, query), nil
}

// Stream 不支持流式调用。
func (t *BrowserProbeInteractivesTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ── BrowserProbeCardsTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserProbeCardsTool) Card() *tool.ToolCard { return t.card }

// Invoke 执行卡片探测。
func (t *BrowserProbeCardsTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	maxCards := 20
	if v, ok := inputs["max_cards"]; ok && v != nil {
		if n, err := parseInt(v); err == nil {
			maxCards = clampInt(n, 1, 50)
		}
	}
	viewportOnly := true
	if v, ok := inputs["viewport_only"]; ok {
		viewportOnly = parseBool(v, true)
	}
	includeButtons := true
	if v, ok := inputs["include_buttons"]; ok {
		includeButtons = parseBool(v, true)
	}
	query := ""
	if v, ok := inputs["query"]; ok && v != nil {
		query = strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return t.runtime.ProbeCards(ctx, maxCards, viewportOnly, includeButtons, query), nil
}

// Stream 不支持流式调用。
func (t *BrowserProbeCardsTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ── BrowserRuntimeHealthTool 方法 ──

// Card 返回工具卡片。
func (t *BrowserRuntimeHealthTool) Card() *tool.ToolCard { return t.card }

// Invoke 返回运行时健康状态。
func (t *BrowserRuntimeHealthTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return t.runtime.RuntimeHealth(), nil
}

// Stream 不支持流式调用。
func (t *BrowserRuntimeHealthTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ── ToolCard 工厂函数 ──

func newCancelToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_cancel_run", cancelDesc, []*cschema.Param{
		cschema.NewStringParam("session_id", "Session ID of the task to cancel", true),
		cschema.NewStringParam("request_id", "Optional: specific request ID to cancel", false),
	}, nil)
}

func newClearCancelToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_clear_cancel", clearCancelDesc, []*cschema.Param{
		cschema.NewStringParam("session_id", "Session ID to clear", true),
		cschema.NewStringParam("request_id", "Optional: specific request ID to clear", false),
	}, nil)
}

func newCustomActionToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_custom_action", customActionDesc, []*cschema.Param{
		cschema.NewStringParam("action", "Name of the custom action to run", true),
		cschema.NewStringParam("session_id", "Session ID (optional)", false),
		cschema.NewStringParam("request_id", "Request ID (optional)", false),
		cschema.NewObjectParam("params", "Extra key-value parameters forwarded to the action", false, nil),
	}, nil)
}

func newListActionsToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_list_custom_actions", listActionsDesc, []*cschema.Param{}, nil)
}

func newRuntimeHealthToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_runtime_health", runtimeHealthDesc, []*cschema.Param{}, nil)
}

func newProbeInteractivesToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_probe_interactives", probeInteractivesDesc, []*cschema.Param{
		cschema.NewIntegerParam("max_items", "Maximum number of elements to return. Default 50, hard-capped at 100.", false),
		cschema.NewBooleanParam("viewport_only", "When true, only return elements currently visible in the viewport. Default true.", false),
		cschema.NewStringParam("query", "Optional text filter, e.g. 'cart', 'search', 'next', or 'login'.", false),
	}, nil)
}

func newProbeCardsToolCard() *tool.ToolCard {
	return tool.NewToolCard("browser_probe_cards", probeCardsDesc, []*cschema.Param{
		cschema.NewIntegerParam("max_cards", "Maximum number of cards to return. Default 20, hard-capped at 50.", false),
		cschema.NewBooleanParam("viewport_only", "When true, only inspect cards visible in the current viewport. Default true.", false),
		cschema.NewBooleanParam("include_buttons", "When true, include visible buttons/links inside each card. Default true.", false),
		cschema.NewStringParam("query", "Optional text filter, e.g. 'mouse', 'book', 'laptop', or 'cart'.", false),
	}, nil)
}

// ── 辅助函数 ──

// parseInt 从 any 值解析整数。
func parseInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case json_number:
		i, err := n.Int64()
		return int(i), err
	default:
		return 0, fmt.Errorf("cannot parse int from %T", v)
	}
}

// parseBool 从 any 值解析布尔值。
func parseBool(v any, defaultVal bool) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		lowered := strings.TrimSpace(strings.ToLower(b))
		if lowered == "0" || lowered == "false" || lowered == "no" {
			return false
		}
		if lowered == "1" || lowered == "true" || lowered == "yes" {
			return true
		}
		return defaultVal
	default:
		return defaultVal
	}
}

// clampInt 将整数限制在 [min, max] 范围内。
func clampInt(v, minVal, maxVal int) int {
	if v < minVal {
		return minVal
	}
	if v > maxVal {
		return maxVal
	}
	return v
}
