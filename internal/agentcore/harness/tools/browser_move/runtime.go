package browser_move

import (
	"context"
	"fmt"
	"strings"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeExecutorFunc JavaScript 代码执行函数类型。
//
// ⤵️ 9.38-49 回填：实际由 Playwright MCP 工具提供
type CodeExecutorFunc func(ctx context.Context, jsCode string) (any, error)

// BrowserAgentRuntime 浏览器运行时内核，管理浏览器生命周期和确定性辅助动作。
//
// 对齐 Python: openjiuwen/harness/tools/browser_move/playwright_runtime/runtime.py (BrowserAgentRuntime L56-537)
type BrowserAgentRuntime struct {
	// service 浏览器后端服务
	service *BrowserService
	// codeExecutor JavaScript 代码执行器
	codeExecutor CodeExecutorFunc
	// controller 动作控制器
	controller *ActionController
	// browserCustomActionTool 自定义动作工具
	browserCustomActionTool *BrowserCustomActionTool
	// browserListActionsTool 列表动作工具
	browserListActionsTool *BrowserListActionsTool
	// browserProbeInteractivesTool 交互探测工具
	browserProbeInteractivesTool *BrowserProbeInteractivesTool
	// browserProbeCardsTool 卡片探测工具
	browserProbeCardsTool *BrowserProbeCardsTool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponentBR 日志组件标识
	logComponentBR = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBrowserAgentRuntime 创建新的浏览器运行时内核。
//
// 对齐 Python: BrowserAgentRuntime.__init__
func NewBrowserAgentRuntime(
	provider, apiKey, apiBase, modelName string,
	mcpCfg *mcptypes.McpServerConfig,
	guardrails *BrowserRunGuardrails,
	cancelStoreKV ...any,
) *BrowserAgentRuntime {
	var cancelStore any
	if len(cancelStoreKV) > 0 {
		cancelStore = cancelStoreKV[0]
	}

	_ = cancelStore // 传递给 NewBrowserService 时使用

	svc := NewBrowserService(provider, apiKey, apiBase, modelName, mcpCfg, guardrails, nil)

	return &BrowserAgentRuntime{
		service: svc,
	}
}

// Service 返回浏览器后端服务实例。
//
// 对齐 Python: BrowserAgentRuntime.service 属性
func (r *BrowserAgentRuntime) Service() *BrowserService {
	return r.service
}

// CodeExecutor 返回 JavaScript 代码执行器。
func (r *BrowserAgentRuntime) CodeExecutor() CodeExecutorFunc {
	return r.codeExecutor
}

// Controller 返回动作控制器。
func (r *BrowserAgentRuntime) Controller() *ActionController {
	return r.controller
}

// BrowserCustomActionTool 返回自定义动作工具。
func (r *BrowserAgentRuntime) BrowserCustomActionTool() *BrowserCustomActionTool {
	return r.browserCustomActionTool
}

// BrowserListActionsTool 返回列表动作工具。
func (r *BrowserAgentRuntime) BrowserListActionsTool() *BrowserListActionsTool {
	return r.browserListActionsTool
}

// BrowserProbeInteractivesTool 返回交互探测工具。
func (r *BrowserAgentRuntime) BrowserProbeInteractivesTool() *BrowserProbeInteractivesTool {
	return r.browserProbeInteractivesTool
}

// BrowserProbeCardsTool 返回卡片探测工具。
func (r *BrowserAgentRuntime) BrowserProbeCardsTool() *BrowserProbeCardsTool {
	return r.browserProbeCardsTool
}

// SetCodeExecutor 设置 JavaScript 代码执行器。
func (r *BrowserAgentRuntime) SetCodeExecutor(fn CodeExecutorFunc) {
	r.codeExecutor = fn
}

// CancelRun 请求取消指定会话/请求的浏览器执行。
//
// 对齐 Python: BrowserAgentRuntime.cancel_run
func (r *BrowserAgentRuntime) CancelRun(ctx context.Context, sessionID, requestID string) map[string]any {
	if err := r.service.RequestCancel(ctx, sessionID, requestID); err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "browser_cancel_run_error").
			Str("session_id", sessionID).
			Str("request_id", requestID).
			Err(err).
			Msg("取消浏览器执行失败")
	}
	return map[string]any{
		"ok":         true,
		"session_id": sessionID,
		"request_id": requestID,
		"error":      nil,
	}
}

// ClearCancel 清除指定会话/请求的取消标记。
//
// 对齐 Python: BrowserAgentRuntime.clear_cancel
func (r *BrowserAgentRuntime) ClearCancel(ctx context.Context, sessionID, requestID string) map[string]any {
	if err := r.service.ClearCancel(ctx, sessionID, requestID); err != nil {
		logger.Warn(logComponentBR).
			Str("event_type", "browser_clear_cancel_error").
			Str("session_id", sessionID).
			Str("request_id", requestID).
			Err(err).
			Msg("清除取消标记失败")
	}
	return map[string]any{
		"ok":         true,
		"session_id": sessionID,
		"request_id": requestID,
		"error":      nil,
	}
}

// EnsureRuntimeReady 确保浏览器运行时已就绪。
//
// 对齐 Python: BrowserAgentRuntime.ensure_runtime_ready
// ⤵️ 9.38-49 回填：ensureBrowserRuntimeClientPatch + code executor 初始化
func (r *BrowserAgentRuntime) EnsureRuntimeReady(ctx context.Context) error {
	if err := r.service.EnsureRuntimeReady(ctx); err != nil {
		return err
	}
	if r.codeExecutor != nil {
		return nil
	}

	// TODO: ⤵️ 9.38-49 回填 _callPlaywrightRunCodeUnsafe 初始化
	// 对齐 Python:
	//   async def _direct_code_executor(js_code):
	//       return await self._call_playwright_run_code_unsafe(js_code)
	//   self._code_executor = _direct_code_executor
	//   self._controller.bind_code_executor(_direct_code_executor)
	//   self._controller.register_builtin_actions()

	logger.Debug(logComponentBR).
		Str("event_type", "browser_runtime_ready").
		Msg("浏览器运行时就绪（code executor 待回填）")

	return nil
}

// EnsureStarted 确保浏览器服务已启动（运行时就绪 + 运行时工具已注册）。
//
// 对齐 Python: BrowserAgentRuntime.ensure_started
// ⤵️ 9.38-49 回填：runtime tools 注册
func (r *BrowserAgentRuntime) EnsureStarted(ctx context.Context) error {
	if err := r.EnsureRuntimeReady(ctx); err != nil {
		return err
	}
	if err := r.service.EnsureStarted(ctx); err != nil {
		return err
	}
	if r.browserCustomActionTool != nil {
		return nil
	}

	// 对齐 Python:
	//   from .runtime_tools import (
	//       BrowserCustomActionTool, BrowserListActionsTool,
	//       BrowserProbeCardsTool, BrowserProbeInteractivesTool,
	//   )
	//   self._browser_custom_action_tool = BrowserCustomActionTool(self, language="en")
	//   self._browser_list_actions_tool = BrowserListActionsTool(self, language="en")
	//   self._browser_probe_interactives_tool = BrowserProbeInteractivesTool(self, language="en")
	//   self._browser_probe_cards_tool = BrowserProbeCardsTool(self, language="en")
	r.browserCustomActionTool = NewBrowserCustomActionTool(r)
	r.browserListActionsTool = NewBrowserListActionsTool(r)
	r.browserProbeInteractivesTool = NewBrowserProbeInteractivesTool(r)
	r.browserProbeCardsTool = NewBrowserProbeCardsTool(r)

	// TODO: ⤵️ 9.38-49 回填 _register_runtime_tool + ability_manager.add
	// 对齐 Python:
	//   self._register_runtime_tool(self._browser_custom_action_tool, tool_name="browser_custom_action")
	//   self._register_runtime_tool(self._browser_list_actions_tool, tool_name="browser_list_custom_actions")
	//   self._register_runtime_tool(self._browser_probe_interactives_tool, tool_name="browser_probe_interactives")
	//   self._register_runtime_tool(self._browser_probe_cards_tool, tool_name="browser_probe_cards")
	//   if self._service.browser_agent is not None:
	//       self._service.browser_agent.ability_manager.add(self._browser_custom_action_tool.card)
	//       self._service.browser_agent.ability_manager.add(self._browser_list_actions_tool.card)
	//       self._service.browser_agent.ability_manager.add(self._browser_probe_interactives_tool.card)
	//       self._service.browser_agent.ability_manager.add(self._browser_probe_cards_tool.card)

	logger.Debug(logComponentBR).
		Str("event_type", "browser_runtime_started").
		Msg("浏览器运行时已启动（runtime tools 已注册，register_runtime_tool 待回填）")

	return nil
}

// RunBrowserTask 执行浏览器任务。
//
// 对齐 Python: BrowserAgentRuntime.run_browser_task
func (r *BrowserAgentRuntime) RunBrowserTask(
	ctx context.Context,
	task, sessionID, requestID string,
	timeoutS *int,
) (map[string]any, error) {
	if err := r.EnsureStarted(ctx); err != nil {
		return nil, err
	}
	return r.service.RunTask(ctx, task, sessionID, requestID, timeoutS)
}

// RunCustomAction 运行自定义浏览器动作。
//
// 对齐 Python: BrowserAgentRuntime.run_custom_action
// ⤵️ 9.38-49 回填：controller 实际调用
func (r *BrowserAgentRuntime) RunCustomAction(
	_ context.Context,
	action, sessionID, requestID string,
	params map[string]any,
) map[string]any {
	// 对齐 Python:
	//   await self.ensure_runtime_ready()
	//   self._controller.bind_runtime(self)
	//   if self._code_executor is not None:
	//       self._controller.bind_code_executor(self._code_executor)
	//   return await self._controller.run_action(action=action, session_id=session_id, request_id=request_id, **(params or {}))

	if r.controller == nil {
		return map[string]any{
			"ok":    false,
			"error": "controller_not_initialized",
		}
	}

	// 绑定运行时和代码执行器
	_ = r.controller.BindRuntime(r)
	if r.codeExecutor != nil {
		r.controller.BindCodeExecutor(r.codeExecutor)
	}

	return r.controller.RunAction(context.Background(), action, sessionID, requestID, params)
}

// ProbeInteractives 返回当前页面上可见/高价值交互元素的紧凑信息。
//
// 对齐 Python: BrowserAgentRuntime.probe_interactives
func (r *BrowserAgentRuntime) ProbeInteractives(
	ctx context.Context,
	maxItems int,
	viewportOnly bool,
	query string,
) map[string]any {
	if err := r.EnsureRuntimeReady(ctx); err != nil {
		return map[string]any{
			"ok":       false,
			"error":    fmt.Sprintf("runtime not ready: %v", err),
			"elements": []any{},
		}
	}

	if r.codeExecutor == nil {
		return map[string]any{
			"ok":       false,
			"error":    "browser_code_executor_not_ready",
			"elements": []any{},
		}
	}

	// TODO: ⤵️ 9.38-49 回填 buildInteractiveProbeJS
	// 对齐 Python:
	//   js_code = build_interactive_probe_js(max_items=max_items, viewport_only=viewport_only, query=query)
	//   raw = await self._code_executor(js_code)
	//   raw = self._unwrap_mcp_text_result(raw)
	jsCode := buildInteractiveProbeJSPlaceholder(maxItems, viewportOnly, query)

	raw, err := r.codeExecutor(ctx, jsCode)
	if err != nil {
		return map[string]any{
			"ok":       false,
			"error":    fmt.Sprintf("browser_probe_interactives failed: %v", err),
			"elements": []any{},
		}
	}

	raw = unwrapMCPTextResult(raw)
	parsed := ExtractJSONObject(raw)
	if parsed == nil || len(parsed) == 0 {
		return map[string]any{
			"ok":          false,
			"error":       "Could not parse browser_probe_interactives result JSON",
			"raw_preview": trimText(raw, 400),
			"elements":    []any{},
		}
	}

	if _, ok := parsed["ok"]; !ok {
		parsed["ok"] = true
	}
	if _, ok := parsed["error"]; !ok {
		parsed["error"] = nil
	}
	if _, ok := parsed["elements"]; !ok {
		parsed["elements"] = []any{}
	}
	return parsed
}

// ProbeCards 返回当前页面上紧凑的重复卡片/列表结构信息。
//
// 对齐 Python: BrowserAgentRuntime.probe_cards
func (r *BrowserAgentRuntime) ProbeCards(
	ctx context.Context,
	maxCards int,
	viewportOnly, includeButtons bool,
	query string,
) map[string]any {
	if err := r.EnsureRuntimeReady(ctx); err != nil {
		return map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("runtime not ready: %v", err),
			"cards": []any{},
		}
	}

	if r.codeExecutor == nil {
		return map[string]any{
			"ok":    false,
			"error": "browser_code_executor_not_ready",
			"cards": []any{},
		}
	}

	// TODO: ⤵️ 9.38-49 回填 buildCardProbeJS + builtinSiteProfiles + getSelectorCache
	// 对齐 Python:
	//   site_profiles = builtin_site_profiles()
	//   selector_cache = get_selector_cache()
	//   selector_cache_records = selector_cache.export_for_probe()
	//   js_code = build_card_probe_js(max_cards=max_cards, viewport_only=viewport_only,
	//       include_buttons=include_buttons, query=query,
	//       site_profiles=site_profiles, selector_cache_records=selector_cache_records)
	jsCode := buildCardProbeJSPlaceholder(maxCards, viewportOnly, includeButtons, query)

	raw, err := r.codeExecutor(ctx, jsCode)
	if err != nil {
		return map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("browser_probe_cards failed: %v", err),
			"cards": []any{},
		}
	}

	raw = unwrapMCPTextResult(raw)
	parsed := ExtractJSONObject(raw)
	if parsed == nil || len(parsed) == 0 {
		return map[string]any{
			"ok":          false,
			"error":       "Could not parse browser_probe_cards result JSON",
			"raw_preview": trimText(raw, 400),
			"cards":       []any{},
		}
	}

	if _, ok := parsed["ok"]; !ok {
		parsed["ok"] = true
	}
	if _, ok := parsed["error"]; !ok {
		parsed["error"] = nil
	}
	if _, ok := parsed["cards"]; !ok {
		parsed["cards"] = []any{}
	}

	// TODO: ⤵️ 9.38-49 回填 selector cache 记录
	// 对齐 Python:
	//   if parsed.get("ok") and parsed.get("cards"):
	//       try:
	//           selector_cache.record_card_probe_result(parsed)
	//       except Exception:
	//           logger.debug("Failed to record card probe result in selector cache", exc_info=True)

	return parsed
}

// ListActions 列出所有可用的自定义浏览器动作。
//
// 对齐 Python: BrowserAgentRuntime.list_actions
func (r *BrowserAgentRuntime) ListActions() map[string]any {
	if r.controller != nil {
		return map[string]any{
			"ok":      true,
			"actions": r.controller.ListActions(),
			"details": r.controller.DescribeActions(),
		}
	}
	return map[string]any{
		"ok":      true,
		"actions": []string{},
		"details": map[string]any{},
	}
}

// RuntimeHealth 返回浏览器运行时健康状态。
//
// 对齐 Python: BrowserAgentRuntime.runtime_health
func (r *BrowserAgentRuntime) RuntimeHealth() map[string]any {
	return map[string]any{
		"ok":                r.service.connectionHealthy,
		"started":           r.service.started,
		"last_heartbeat_ok": r.service.lastHeartbeatOK,
		"provider":          r.service.Provider,
		"api_base":          r.service.APIBase,
		"model_name":        r.service.ModelName,
	}
}

// Shutdown 关闭浏览器运行时。
//
// 对齐 Python: BrowserAgentRuntime.shutdown
func (r *BrowserAgentRuntime) Shutdown(ctx context.Context) error {
	return r.service.Shutdown(ctx)
}

// PlaywrightClientLookupKeys 返回 Playwright MCP 客户端的候选查找键。
//
// 对齐 Python: BrowserAgentRuntime._playwright_client_lookup_keys
func (r *BrowserAgentRuntime) PlaywrightClientLookupKeys() []string {
	serverID := strings.TrimSpace(fmt.Sprintf("%v", r.service.MCPCfg.ServerID))
	serverName := strings.TrimSpace(fmt.Sprintf("%v", r.service.MCPCfg.ServerName))

	candidates := []string{
		serverID,
		serverName,
		strings.ReplaceAll(serverID, "-", "_"),
		strings.ReplaceAll(serverID, "_", "-"),
		strings.ReplaceAll(serverName, "-", "_"),
		strings.ReplaceAll(serverName, "_", "-"),
		// 常见 Playwright 运行时标识
		"playwright_official_stdio",
		"playwright-official",
		"playwright",
	}

	var result []string
	seen := make(map[string]bool)
	for _, item := range candidates {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// unwrapMCPTextResult 从 MCP 工具结果中提取文本负载。
//
// 对齐 Python: BrowserAgentRuntime._unwrap_mcp_text_result
func unwrapMCPTextResult(raw any) any {
	if raw == nil {
		return raw
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return raw
	}

	// 处理 content 列表 [{type: "text", text: "..."}]
	if content, ok := m["content"]; ok {
		if contentList, ok := content.([]any); ok {
			var texts []string
			for _, item := range contentList {
				if itemMap, ok := item.(map[string]any); ok {
					if fmt.Sprintf("%v", itemMap["type"]) == "text" {
						if text, ok := itemMap["text"].(string); ok {
							texts = append(texts, text)
						}
					}
				}
			}
			if len(texts) > 0 {
				return strings.Join(texts, "\n")
			}
		}
	}

	// 处理 result 字段
	if result, ok := m["result"]; ok {
		return result
	}

	// 处理 text 字段
	if text, ok := m["text"]; ok {
		return text
	}

	// 处理 data 字段
	if data, ok := m["data"]; ok {
		return data
	}

	return raw
}

// buildInteractiveProbeJSPlaceholder 交互探测 JS 占位代码。
// ⤵️ 9.38-49 回填：替换为 buildInteractiveProbeJS 完整实现
func buildInteractiveProbeJSPlaceholder(maxItems int, viewportOnly bool, query string) string {
	return fmt.Sprintf(
		`// placeholder: browser_probe_interactives(maxItems=%d, viewportOnly=%v, query=%q)`,
		maxItems, viewportOnly, query,
	)
}

// buildCardProbeJSPlaceholder 卡片探测 JS 占位代码。
// ⤵️ 9.38-49 回填：替换为 buildCardProbeJS 完整实现
func buildCardProbeJSPlaceholder(maxCards int, viewportOnly, includeButtons bool, query string) string {
	return fmt.Sprintf(
		`// placeholder: browser_probe_cards(maxCards=%d, viewportOnly=%v, includeButtons=%v, query=%q)`,
		maxCards, viewportOnly, includeButtons, query,
	)
}
