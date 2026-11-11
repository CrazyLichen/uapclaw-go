package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 日历事件的对象字段候选键。
// 不同 MCP 日历服务用不同命名（Google/Outlook/FastMCP 各异），逐个探测取首个非空值。
//
// Python: calendar_source.py:42-45
var (
	// eventTitleKeys 标题候选键
	eventTitleKeys = []string{"title", "summary", "subject", "name"}
	// eventStartKeys 开始时间候选键
	eventStartKeys = []string{"start", "start_time", "dtStart", "startTime", "begin"}
	// eventEndKeys 结束时间候选键
	eventEndKeys = []string{"end", "end_time", "dtEnd", "endTime", "finish"}
	// eventLocationKeys 地点候选键
	eventLocationKeys = []string{"location", "place", "where"}
)

// eventToolCandidates 日历事件列表工具的候选名，逐个探测首个能解析到的。
// Python: calendar_source.py:34-39
var eventToolCandidates = []string{
	"list_events",
	"get_events",
	"search_events",
	"list_calendars", // 部分服务按日历列出
}

const (
	// maxCalendarEvents 单次拉取的日程条数上限
	// Python: calendar_source.py:59 fetch_calendar_events(max_events=10)
	maxCalendarEvents = 10
	// calendarLookaheadHours 向前看的小时数
	// Python: calendar_source.py:59 fetch_calendar_events(lookahead_hours=24)
	calendarLookaheadHours = 24
	// calendarServerNameKeyword 日历 MCP server 名的识别关键字
	// Python: calendar_source.py:51 if "calendar" in name
	calendarServerNameKeyword = "calendar"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FetchCalendarEvents 从已配置的 MCP 日历服务拉取即将到来的日程。
//
// 返回归一化后的事件列表（键：title/start/end/location）。
// **任何失败都返回空列表，绝不返回错误**——tick 不能因日历抖动而中断。
//
// Python: calendar_source.py:59 fetch_calendar_events
func FetchCalendarEvents(ctx context.Context, maxEvents, lookaheadHours int) []map[string]any {
	return fetchCalendarEventsWith(ctx, maxEvents, lookaheadHours, findCalendarServerEntry, resolveEventTool)
}

// fetchCalendarEventsWith 用注入的配置查找与工具解析执行拉取，便于测试。
//
// MCP 是外部协议边界，按项目规则允许在此 mock；被测逻辑（时间窗构造、
// 结果归一化、截断、异常吞噬）不 mock。
func fetchCalendarEventsWith(
	ctx context.Context,
	maxEvents, lookaheadHours int,
	findEntry func() map[string]any,
	resolveTool func(ctx context.Context, serverName string) tool.Tool,
) []map[string]any {
	if maxEvents <= 0 {
		maxEvents = maxCalendarEvents
	}
	if lookaheadHours <= 0 {
		lookaheadHours = calendarLookaheadHours
	}

	entry := findEntry()
	if entry == nil {
		logger.Debug(logComponent).Msg("no enabled calendar MCP server configured")
		return nil
	}
	serverName := strings.TrimSpace(stringField(entry, "name"))
	if serverName == "" {
		return nil
	}

	resolvedTool := resolveTool(ctx, serverName)
	if resolvedTool == nil {
		logger.Debug(logComponent).Str("server", serverName).
			Strs("candidates", eventToolCandidates).
			Msg("no event-listing tool found on MCP server")
		return nil
	}

	// 时间窗：多数日历 MCP 服务接受 ISO 8601
	now := time.Now().UTC()
	startISO := now.Format(time.RFC3339)
	endISO := now.Add(time.Duration(lookaheadHours) * time.Hour).Format(time.RFC3339)

	out, err := invokeCalendarTool(ctx, resolvedTool, map[string]any{"start": startISO, "end": endISO})
	if err != nil {
		logger.Debug(logComponent).Err(err).Str("server", serverName).Msg("calendar tool invoke failed, skipping")
		return nil
	}

	events := extractCalendarEvents(out)
	if len(events) == 0 {
		logger.Debug(logComponent).Str("server", serverName).
			Str("start", startISO).Str("end", endISO).
			Msg("calendar server returned no events")
		return nil
	}

	normalized := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		item := normalizeCalendarEvent(ev)
		if stringField(item, "title") != "" {
			normalized = append(normalized, item)
		}
	}
	if len(normalized) > maxEvents {
		normalized = normalized[:maxEvents]
	}
	return normalized
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// findCalendarServerEntry 返回第一个启用的、名字含 "calendar" 的 MCP server 配置项。
//
// 配置源与 Go 侧既有 MCP 注册链路一致：configBase["mcp"]["servers"]。
// 注意 Python 的 get_mcp_servers() 还会合并 mcp/state.json（marketplace 连接的
// 服务），Go 侧当前只有 config.yaml 这一源——缺口已登记 10.3.31g。
//
// Python: calendar_source.py:48 _find_calendar_server_entry
func findCalendarServerEntry() map[string]any {
	cfgMap := loadRecommendationConfigOrNil()
	mcpCfg, _ := cfgMap["mcp"].(map[string]any)
	if mcpCfg == nil {
		return nil
	}
	return pickCalendarServerEntry(asMapSlice(mcpCfg["servers"]))
}

// pickCalendarServerEntry 从候选列表里挑第一个「启用且名字含 calendar」的条目。
//
// 单列出来是为了让匹配规则（名字子串 + enabled 缺省为 true）能被直接测试，
// 而不必构造完整 config.yaml——测试若另写一份匹配逻辑，就会与生产实现脱节。
func pickCalendarServerEntry(entries []map[string]any) map[string]any {
	for _, entry := range entries {
		name := strings.ToLower(stringField(entry, "name"))
		if !strings.Contains(name, calendarServerNameKeyword) {
			continue
		}
		// enabled 缺省为 true（对齐 Python entry.get("enabled", True)）
		if enabled, ok := entry["enabled"].(bool); ok && !enabled {
			continue
		}
		return entry
	}
	return nil
}

// resolveEventTool 在指定 MCP server 上探测候选工具名，返回首个能解析到的工具。
// 探测失败继续试下一个候选，全部失败返回 nil。
//
// Python: calendar_source.py:114 _resolve_event_tool
func resolveEventTool(ctx context.Context, serverName string) tool.Tool {
	return resolveEventToolWith(ctx, serverName, lookupMcpTools)
}

// lookupMcpTools MCP 工具查询函数（可注入，MCP 属外部协议边界）。
var lookupMcpTools = func(ctx context.Context, toolName, serverName string) ([]tool.Tool, error) {
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr == nil {
		return nil, nil
	}
	return resourceMgr.GetMcpTool(ctx, toolName, serverName)
}

// resolveEventToolWith 用注入的查询函数做工具探测。
func resolveEventToolWith(
	ctx context.Context,
	serverName string,
	lookup func(ctx context.Context, toolName, serverName string) ([]tool.Tool, error),
) tool.Tool {
	for _, candidate := range eventToolCandidates {
		tools, err := lookup(ctx, candidate, serverName)
		if err != nil {
			logger.Debug(logComponent).Err(err).Str("tool", candidate).
				Str("server", serverName).Msg("get mcp tool failed, trying next candidate")
			continue
		}
		if len(tools) == 0 || tools[0] == nil {
			continue
		}
		return tools[0]
	}
	return nil
}

// invokeCalendarTool 调用日历工具并返回原始结果 map。
func invokeCalendarTool(ctx context.Context, calendarToolImpl tool.Tool, inputs map[string]any) (map[string]any, error) {
	result, err := calendarToolImpl.Invoke(ctx, inputs)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result, nil
}

// extractCalendarEvents 从 MCP 工具调用结果中宽松提取事件列表。
//
// MCP 返回形如 {"result": <...>}，其中 <...> 可能是 JSON 字符串、对象或列表，
// 取决于服务实现。逐层归一化到事件对象列表。
//
// Python: calendar_source.py:133 _extract_events
func extractCalendarEvents(out map[string]any) []any {
	if out == nil {
		return nil
	}
	// Python: result = out.get("result", out)
	var result any = out
	if raw, exists := out["result"]; exists {
		result = raw
	}

	// result 常是 JSON 字符串（FastMCP 把内容作为文本返回）
	if s, ok := result.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return nil
		}
		result = decoded
		// 双重编码
		if inner, isStr := result.(string); isStr {
			var twice any
			if err := json.Unmarshal([]byte(inner), &twice); err != nil {
				return nil
			}
			result = twice
		}
	}

	// 拆常见信封：{"events": [...]} / {"items": [...]} / 裸列表
	switch typed := result.(type) {
	case map[string]any:
		for _, key := range []string{"events", "items", "data", "result"} {
			if list, ok := typed[key].([]any); ok {
				return list
			}
		}
		// 单个事件对象？
		for _, key := range eventTitleKeys {
			if _, exists := typed[key]; exists {
				return []any{typed}
			}
		}
		return nil
	case []any:
		return typed
	default:
		return nil
	}
}

// normalizeCalendarEvent 把事件的候选键归一到 title/start/end/location。
// Python: calendar_source.py:172 _normalize_event
func normalizeCalendarEvent(ev any) map[string]any {
	obj, ok := ev.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return map[string]any{
		"title":    firstPresent(obj, eventTitleKeys),
		"start":    firstPresent(obj, eventStartKeys),
		"end":      firstPresent(obj, eventEndKeys),
		"location": firstPresent(obj, eventLocationKeys),
	}
}

// firstPresent 返回候选键中首个非空值，全部缺失返回空串。
//
// 兼容三种嵌套形态：
//   - 嵌套对象 {"dateTime": ...} / {"date": ...} / {"value": ...}（Google Calendar 风格）
//   - 列表/元组取首元素
//   - 标量直接转字符串
//
// Python: calendar_source.py:184 _first_present
func firstPresent(obj map[string]any, keys []string) string {
	for _, key := range keys {
		value, exists := obj[key]
		if !exists || value == nil {
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			for _, sub := range []string{"dateTime", "date", "value"} {
				if nested, ok := typed[sub]; ok && nested != nil && fmt.Sprintf("%v", nested) != "" {
					return fmt.Sprintf("%v", nested)
				}
			}
			continue
		case []any:
			if len(typed) > 0 {
				return fmt.Sprintf("%v", typed[0])
			}
			continue
		default:
			if rendered := fmt.Sprintf("%v", typed); rendered != "" {
				return rendered
			}
		}
	}
	return ""
}

// loadRecommendationConfigOrNil 加载配置，失败返回空 map（日历是可选能力，不应阻断）。
func loadRecommendationConfigOrNil() map[string]any {
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return nil
	}
	cfgMap, err := cfg.Load()
	if err != nil {
		return nil
	}
	return cfgMap
}
