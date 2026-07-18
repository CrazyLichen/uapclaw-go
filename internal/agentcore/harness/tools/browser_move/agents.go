package browser_move

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserWorkerConfig 浏览器 Worker Agent 配置。
//
// 对齐 Python: build_browser_worker_agent 参数 (agents.py L301-312)
type BrowserWorkerConfig struct {
	// Provider 模型提供商
	Provider string
	// APIKey API 密钥
	APIKey string
	// APIBase API 基础地址
	APIBase string
	// ModelName 模型名称
	ModelName string
	// MCPCfg MCP 服务器配置
	MCPCfg *mcptypes.McpServerConfig
	// MaxSteps 最大步数
	MaxSteps int
	// ScreenshotSubdir 截图子目录
	ScreenshotSubdir string
	// ArtifactsSubdir 产物子目录
	ArtifactsSubdir string
	// ToolResultObserver 工具结果观察者回调
	ToolResultObserver ToolResultObserverFunc
}

// ToolResultObserverFunc 工具结果观察者回调函数类型。
// 对齐 Python: ToolResultObserver callback
type ToolResultObserverFunc func(ctx context.Context, toolName string, result any) error

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildBrowserWorkerSystemPrompt 构建浏览器 Worker Agent 系统提示词。
//
// 对齐 Python: build_browser_worker_system_prompt (agents.py L169-225)
// 提示词逐字符复制 Python 原文，不做自行翻译
func BuildBrowserWorkerSystemPrompt(screenshotSubdir string, artifactsSubdir string) string {
	// 对齐 Python: 截图和产物子目录规范化
	screenshotSubdir = strings.TrimSpace(screenshotSubdir)
	screenshotSubdir = strings.ReplaceAll(screenshotSubdir, "\\", "/")
	screenshotSubdir = strings.TrimRight(screenshotSubdir, "/")
	if screenshotSubdir == "" {
		screenshotSubdir = "screenshots"
	}

	artifactsSubdir = strings.TrimSpace(artifactsSubdir)
	artifactsSubdir = strings.ReplaceAll(artifactsSubdir, "\\", "/")
	artifactsSubdir = strings.TrimRight(artifactsSubdir, "/")
	if artifactsSubdir == "" {
		artifactsSubdir = "artifacts"
	}

	// 对齐 Python: agents.py L173-225
	// 提示词逐字符复制 Python 原文
	return "You are a browser worker agent.\n" +
		"Execute browser tasks step-by-step with Playwright MCP tools and approved runtime helper tools only.\n" +
		"Before interacting, ensure page or selector readiness.\n" +
		"Keep actions targeted and avoid unnecessary page snapshots.\n" +
		"Before broad page snapshots, full-body scans, or generic DOM scraping, " +
		"choose the smallest compact probe that matches the task. " +
		"Use browser_probe_interactives for buttons, links, inputs, forms, navigation controls, " +
		"login controls, pagination controls, menus, and other visible interactive elements. " +
		"When using browser_probe_interactives only for page-level controls, prefer max_items around 20-30 " +
		"unless the task explicitly requires a larger inventory. " +
		"On product pages, marketplace pages, search-result pages, catalog pages, article-list pages, " +
		"or any page with repeated visible cards/listings, call browser_probe_cards before broad extraction. " +
		"Use browser_probe_cards to identify compact repeated structures such as product cards, result cards, " +
		"book cards, article cards, listing rows, title/price/rating/review/availability fields, primary links, " +
		"visible buttons, bounding boxes, selector hints, and recurring structure signatures. " +
		"For product/listing/item-data tasks, prefer browser_probe_cards first; call browser_probe_interactives " +
		"only if you also need page-level navigation, filters, forms, or controls outside the cards. " +
		"Prefer selector_hint values from compact probes when they are relevant. " +
		"Use browser_snapshot only when the compact probes are insufficient, when accessibility structure is needed, " +
		"or when you need exact element references required by a Playwright MCP action. " +
		"Use browser_run_code_unsafe or browser_run_code only when you already know the exact selector/computation, " +
		"or when the compact probes and browser_snapshot are insufficient. " +
		"Do not use browser_run_code_unsafe or browser_run_code to dump the entire document body " +
		" unless all compact approaches fail.\n" +
		"If actions repeatedly fail, stop and report the exact failing action.\n" +
		"If you use browser_tabs, action MUST be one of: list, new, close, select.\n" +
		"For specialized operations (file upload, drag-and-drop, coordinates, etc.), " +
		"call browser_list_custom_actions to discover available actions and their params, " +
		"then call browser_custom_action with the matching action name and params.\n" +
		"Never call browser_custom_action with action='browser_task' or action='run_browser_task'. " +
		"Do not launch nested browser tasks from the browser worker. " +
		"If you cannot finish without recursion, return a JSON error object instead.\n" +
		"IMPORTANT: Do NOT use browser_take_screenshot unless strictly necessary. " +
		fmt.Sprintf("If a screenshot is needed, always save it under '%s/'. ", screenshotSubdir) +
		"Use browser_run_code_unsafe or browser_run_code with: " +
		fmt.Sprintf("async (page) => { await page.screenshot({ path: '%s/screenshot.png' }); ", screenshotSubdir) +
		fmt.Sprintf("return '%s/screenshot.png'; }\n", screenshotSubdir) +
		"If you produce any output files (reports, notes, summaries, markdown, text files, etc.), " +
		fmt.Sprintf("write them to the '%s/' directory relative to the working directory. ", artifactsSubdir) +
		"Never write output files to the project root or any other location.\n" +
		"Final output MUST be a single JSON object with keys:\n" +
		"ok (boolean), final (string), page (object with url and title), " +
		"screenshot (string|null), error (string|null).\n" +
		"Also include status (completed|partial|blocked|failed) whenever possible. " +
		"If the task is not fully complete, include progress as an object with " +
		"completed_steps, remaining_steps, next_step, completion_evidence, and missing_requirements.\n" +
		"Set ok=true only when the exact user-visible goal is fully satisfied and you can cite concrete evidence " +
		"from the page, compact probe results, browser snapshots, screenshots, or generated artifacts. " +
		"If the task is incomplete or blocked, set ok=false and fill the progress fields so a continuation can " +
		"resume with minimal repetition.\n" +
		"Return JSON only, even on failures. " +
		"Do not output markdown, code fences, or plain text outside the JSON object."
}

// BuildBrowserWorkerAgent 构建浏览器 Worker Agent。
//
// 对齐 Python: build_browser_worker_agent (agents.py L301-355)
// 当前为占位实现，⤵️ 9.38-49 完整实现 ReActAgent 配置
func BuildBrowserWorkerAgent(config *BrowserWorkerConfig) (*agents.ReActAgent, error) {
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	_ = BuildBrowserWorkerSystemPrompt(
		config.ScreenshotSubdir,
		config.ArtifactsSubdir,
	)

	// ⤵️ 9.38-49 完整实现 ReActAgent 配置
	// 当前仅返回 nil，待后续章节回填完整的 ReActAgent 构建逻辑
	// 对齐 Python:
	//   agent = ReActAgent(
	//       model_client=model_client,
	//       system_prompt=system_prompt,
	//       max_steps=max_steps,
	//       ...
	//   )
	return nil, fmt.Errorf("BuildBrowserWorkerAgent: ReActAgent 完整配置待 9.38-49 回填")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveToolTimeoutS 解析工具超时时间。
// 对齐 Python: _resolve_tool_timeout_s (agents.py L52-65)
func resolveToolTimeoutS(defaultS float64) float64 {
	raw := ""
	for _, key := range []string{"PLAYWRIGHT_TOOL_TIMEOUT_S", "PLAYWRIGHT_MCP_TIMEOUT_S", "BROWSER_TIMEOUT_S"} {
		if v := os.Getenv(key); v != "" {
			raw = v
			break
		}
	}
	if raw == "" {
		raw = strconv.FormatFloat(defaultS, 'f', -1, 64)
	}
	if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 {
		return parsed
	}
	return defaultS
}

// resolveSamplingValue 解析采样参数值。
// 对齐 Python: _resolve_sampling_value (agents.py L68-85)
func resolveSamplingValue(keys []string, defaultVal float64, minValue float64, maxValue float64) float64 {
	for _, key := range keys {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		if minValue <= value && value <= maxValue {
			return value
		}
	}
	return defaultVal
}

// resolveSamplingParams 解析采样参数。
// 对齐 Python: _resolve_sampling_params (agents.py L88-107)
func resolveSamplingParams(temperatureKeys []string, topPKeys []string, defaultTemperature float64, defaultTopP float64) (float64, float64) {
	temperature := resolveSamplingValue(temperatureKeys, defaultTemperature, 0.0, 2.0)
	topP := resolveSamplingValue(topPKeys, defaultTopP, 0.0, 1.0)
	return temperature, topP
}
