package web_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PaidSearchInput paid_search 工具的输入参数
// 对齐 Python: WebPaidSearchTool.invoke inputs (web_tools.py L1300-1313)
type PaidSearchInput struct {
	// Query 搜索查询文本
	Query string `json:"query"`
	// Provider 搜索提供商
	Provider string `json:"provider"`
	// MaxResults 最大结果数
	MaxResults int `json:"max_results"`
	// TimeoutSeconds 请求超时时间
	TimeoutSeconds int `json:"timeout_seconds"`
}

// paidSearchResult 付费搜索结果
type paidSearchResult struct {
	// Provider 提供商名称
	Provider string `json:"provider"`
	// Answer 答案文本
	Answer string `json:"answer"`
	// URLs 结果 URL 列表
	URLs []string `json:"urls"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWebPaidSearchTool 创建付费搜索工具
// 对齐 Python: WebPaidSearchTool.__init__ + invoke (web_tools.py L1091-1381)
func NewWebPaidSearchTool(language, agentID string) tool.Tool {
	card, _ := hprompts.BuildToolCard("paid_search", "WebPaidSearchTool", language, nil, agentID)

	fn := func(ctx context.Context, input PaidSearchInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: WebPaidSearchTool.invoke (web_tools.py L1298-1376)
		query := strings.TrimSpace(input.Query)
		provider := strings.ToLower(strings.TrimSpace(input.Provider))
		maxResults := input.MaxResults
		timeoutSeconds := input.TimeoutSeconds

		if query == "" {
			return map[string]any{"result": "[ERROR]: query cannot be empty."}, nil
		}

		// 对齐 Python: L1302-1308 — 环境变量覆盖 provider
		envProvider := strings.ToLower(strings.TrimSpace(os.Getenv(paidSearchProviderEnv)))
		if envProvider == "" {
			envProvider = strings.ToLower(strings.TrimSpace(os.Getenv(paidSearchProviderAltEnv)))
		}
		if provider == "auto" && envProvider != "" {
			provider = envProvider
		}

		// 对齐 Python: L1309-1313 — 默认值
		if maxResults <= 0 {
			maxResults = 8
		}
		if timeoutSeconds <= 0 {
			timeoutSeconds = paidSearchDefaultTimeoutSeconds
		}

		// 对齐 Python: L1318-1319 — provider 校验
		validProviders := map[string]bool{"auto": true, "bocha": true, "jina": true, "serper": true, "perplexity": true}
		if !validProviders[provider] {
			return map[string]any{"result": "[ERROR]: provider must be one of auto|bocha|jina|serper|perplexity."}, nil
		}

		// 对齐 Python: L1321-1325 — 参数钳位
		timeoutSeconds = max(paidSearchMinTimeoutSeconds, min(timeoutSeconds, paidSearchMaxTimeoutSeconds))
		maxResults = max(1, min(maxResults, 20))

		// 对齐 Python: L1327-1348 — 确定搜索顺序
		var order []string
		if provider == "auto" {
			order = configuredPaidSearchProviders()
			if len(order) == 0 {
				return map[string]any{"result": "[ERROR]: no paid search provider API key configured. Set one of BOCHA_API_KEY, PERPLEXITY_API_KEY, SERPER_API_KEY, JINA_API_KEY."}, nil
			}
		} else {
			order = []string{provider}
		}

		// 对齐 Python: L1349-1376 — 逐 provider 尝试
		var errors []string
		for _, name := range order {
			result, err := runPaidSearchProvider(name, query, maxResults, timeoutSeconds)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %s", name, err))
				continue
			}

			answer := strings.TrimSpace(result.Answer)
			urls := result.URLs
			if maxResults > 0 && len(urls) > maxResults {
				urls = urls[:maxResults]
			}

			// 对齐 Python: L1361-1374 — 格式化输出
			if answer == "" && len(urls) == 0 {
				errors = append(errors, fmt.Sprintf("%s: no usable result payload", name))
				continue
			}

			var lines []string
			lines = append(lines, fmt.Sprintf("Paid search provider: %s", name))
			if answer != "" {
				lines = append(lines, "Answer:")
				lines = append(lines, answer)
			}
			if len(urls) > 0 {
				lines = append(lines, "URLs:")
				for idx, u := range urls {
					lines = append(lines, fmt.Sprintf("%d. %s", idx+1, u))
				}
			}
			return map[string]any{"result": strings.Join(lines, "\n")}, nil
		}

		return map[string]any{"result": "[ERROR]: paid search failed. " + strings.Join(errors, " | ")}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runPaidSearchProvider 运行指定付费搜索提供商
func runPaidSearchProvider(name, query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	switch name {
	case "jina":
		return jinaSearch(query, timeoutSeconds)
	case "bocha":
		return bochaSearch(query, maxResults, timeoutSeconds)
	case "serper":
		return serperSearch(query, maxResults, timeoutSeconds)
	case "perplexity":
		return perplexitySearch(query, maxResults, timeoutSeconds)
	default:
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "runPaidSearchProvider").Str("provider", name).Msg("未找到对应的搜索提供商运行器")
		return nil, fmt.Errorf("runner not found for provider: %s", name)
	}
}

// jinaSearch 使用 Jina DeepSearch API 搜索
// 对齐 Python: WebPaidSearchTool._jina_search_sync() (web_tools.py L1103-1130)
func jinaSearch(query string, timeoutSeconds int) (*paidSearchResult, error) {
	jinaKey := strings.TrimSpace(os.Getenv("JINA_API_KEY"))
	if jinaKey == "" {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "jinaSearch").Msg("JINA_API_KEY 未设置")
		return nil, fmt.Errorf("JINA_API_KEY not set")
	}

	// 对齐 Python: L1110-1114
	model := safeEnvChoice("JINA_MODEL", jinaDefaultModel, jinaAllowedModels)
	payload := map[string]any{
		"model":            model,
		"messages":         []map[string]string{{"role": "user", "content": query}},
		"stream":           false,
		"reasoning_effort": "low",
	}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := httpRequest("POST", "https://deepsearch.jina.ai/v1/chat/completions",
		withHeaders(map[string]string{
			"Authorization": "Bearer " + jinaKey,
			"Content-Type":  "application/json",
		}),
		withTimeout(timeoutSeconds),
		withBody(string(payloadBytes)),
	)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "jinaSearch").Str("model_provider", "jina").Err(err).Msg("jina 搜索请求失败")
		return nil, fmt.Errorf("jina search request failed: %w", err)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "jinaSearch").Str("model_provider", "jina").Int("status_code", resp.statusCode).Err(err).Msg("jina 搜索 HTTP 错误")
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(resp.body, &data); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "jinaSearch").Str("model_provider", "jina").Err(err).Msg("jina 搜索解析响应失败")
		return nil, fmt.Errorf("jina search parse response failed: %w", err)
	}

	// 对齐 Python: L1126-1128
	answer := ""
	if choices, ok := data["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					answer = content
				}
			}
		}
	}

	// 对齐 Python: L1129
	urls := urlExtractRe.FindAllString(answer, -1)
	return &paidSearchResult{Provider: "jina", Answer: strings.TrimSpace(answer), URLs: urls}, nil
}

// bochaSearch 使用 Bocha Web Search API 搜索
// 对齐 Python: WebPaidSearchTool._bocha_search_sync() (web_tools.py L1187-1206)
func bochaSearch(query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	bochaKey := strings.TrimSpace(os.Getenv("BOCHA_API_KEY"))
	if bochaKey == "" {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "bochaSearch").Msg("BOCHA_API_KEY 未设置")
		return nil, fmt.Errorf("BOCHA_API_KEY not set")
	}

	// 对齐 Python: L1193-1195
	apiURL := os.Getenv("BOCHA_API_URL")
	if apiURL == "" {
		apiURL = "https://api.bocha.cn/v1/web-search"
	}

	payload := map[string]any{"query": query, "summary": true, "count": maxResults}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := httpRequest("POST", apiURL,
		withHeaders(map[string]string{
			"Authorization": "Bearer " + bochaKey,
			"Content-Type":  "application/json",
		}),
		withTimeout(timeoutSeconds),
		withBody(string(payloadBytes)),
	)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "bochaSearch").Str("model_provider", "bocha").Err(err).Msg("bocha 搜索请求失败")
		return nil, fmt.Errorf("bocha search request failed: %w", err)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "bochaSearch").Str("model_provider", "bocha").Int("status_code", resp.statusCode).Err(err).Msg("bocha 搜索 HTTP 错误")
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(resp.body, &data); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "bochaSearch").Str("model_provider", "bocha").Err(err).Msg("bocha 搜索解析响应失败")
		return nil, fmt.Errorf("bocha search parse response failed: %w", err)
	}

	return &paidSearchResult{
		Provider: "bocha",
		Answer:   extractBochaAnswer(data),
		URLs:     extractBochaURLs(data, maxResults),
	}, nil
}

// serperSearch 使用 Serper (Google Search API) 搜索
// 对齐 Python: WebPaidSearchTool._serper_search_sync() (web_tools.py L1209-1239)
func serperSearch(query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	serperKey := strings.TrimSpace(os.Getenv("SERPER_API_KEY"))
	if serperKey == "" {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "serperSearch").Msg("SERPER_API_KEY 未设置")
		return nil, fmt.Errorf("SERPER_API_KEY not set")
	}

	headers := map[string]string{
		"X-API-KEY":    serperKey,
		"Content-Type": "application/json",
	}

	payload := map[string]any{"q": query, "num": maxResults}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := httpRequest("POST", "https://google.serper.dev/search",
		withHeaders(headers),
		withTimeout(timeoutSeconds),
		withBody(string(payloadBytes)),
	)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "serperSearch").Str("model_provider", "serper").Err(err).Msg("serper 搜索请求失败")
		return nil, fmt.Errorf("serper search request failed: %w", err)
	}

	// 对齐 Python: L1223-1230 — 400 时重试不带 num
	if resp.statusCode == 400 {
		payload2 := map[string]any{"q": query}
		payloadBytes2, _ := json.Marshal(payload2)
		resp, err = httpRequest("POST", "https://google.serper.dev/search",
			withHeaders(headers),
			withTimeout(timeoutSeconds),
			withBody(string(payloadBytes2)),
		)
		if err != nil {
			logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "serperSearch").Str("model_provider", "serper").Err(err).Msg("serper 搜索重试请求失败")
			return nil, fmt.Errorf("serper search retry request failed: %w", err)
		}
	}

	if err := raiseForStatusWithBody(resp); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "serperSearch").Str("model_provider", "serper").Int("status_code", resp.statusCode).Err(err).Msg("serper 搜索 HTTP 错误")
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(resp.body, &data); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "serperSearch").Str("model_provider", "serper").Err(err).Msg("serper 搜索解析响应失败")
		return nil, fmt.Errorf("serper search parse response failed: %w", err)
	}

	// 对齐 Python: L1233-1238
	var urls []string
	if organic, ok := data["organic"].([]any); ok {
		for i, item := range organic {
			if i >= maxResults {
				break
			}
			if m, ok := item.(map[string]any); ok {
				if link, ok := m["link"].(string); ok && link != "" {
					urls = append(urls, link)
				}
			}
		}
	}

	return &paidSearchResult{Provider: "serper", Answer: "", URLs: urls}, nil
}

// perplexitySearch 使用 Perplexity AI 搜索
// 对齐 Python: WebPaidSearchTool._perplexity_search_sync() (web_tools.py L1261-1296)
func perplexitySearch(query string, maxResults, timeoutSeconds int) (*paidSearchResult, error) {
	perplexityKey := strings.TrimSpace(os.Getenv("PERPLEXITY_API_KEY"))
	if perplexityKey == "" {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "perplexitySearch").Msg("PERPLEXITY_API_KEY 未设置")
		return nil, fmt.Errorf("PERPLEXITY_API_KEY not set")
	}

	// 对齐 Python: L1268-1269
	model := safeEnvChoice("PPLX_MODEL", pplxDefaultModel, pplxAllowedModels)
	apiURL := os.Getenv("PPLX_API_URL")
	if apiURL == "" {
		apiURL = "https://api.perplexity.ai/chat/completions"
	}

	// 对齐 Python: L1270-1276
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "Provide concise answer and include citations."},
			{"role": "user", "content": query},
		},
		"max_tokens":  1024,
		"temperature": 0.2,
		"stream":      false,
	}
	payloadBytes, _ := json.Marshal(payload)

	resp, err := httpRequest("POST", apiURL,
		withHeaders(map[string]string{
			"Authorization": "Bearer " + perplexityKey,
			"Content-Type":  "application/json",
		}),
		withTimeout(timeoutSeconds),
		withBody(string(payloadBytes)),
	)
	if err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "perplexitySearch").Str("model_provider", "perplexity").Err(err).Msg("perplexity 搜索请求失败")
		return nil, fmt.Errorf("perplexity search request failed: %w", err)
	}
	if err := raiseForStatusWithBody(resp); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "perplexitySearch").Str("model_provider", "perplexity").Int("status_code", resp.statusCode).Err(err).Msg("perplexity 搜索 HTTP 错误")
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(resp.body, &data); err != nil {
		logger.Error(logComponent).Str("event_type", "LLM_CALL_ERROR").Str("method", "perplexitySearch").Str("model_provider", "perplexity").Err(err).Msg("perplexity 搜索解析响应失败")
		return nil, fmt.Errorf("perplexity search parse response failed: %w", err)
	}

	// 对齐 Python: L1288-1290
	answer := ""
	if choices, ok := data["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					answer = content
				}
			}
		}
	}

	// 对齐 Python: L1292-1295
	urls := parsePerplexityCitations(data)
	if maxResults > 0 && len(urls) > maxResults {
		urls = urls[:maxResults]
	}

	return &paidSearchResult{Provider: "perplexity", Answer: strings.TrimSpace(answer), URLs: urls}, nil
}

// extractBochaURLs 从 Bocha 响应中提取 URL
// 对齐 Python: WebPaidSearchTool._extract_bocha_urls() (web_tools.py L1133-1155)
func extractBochaURLs(data map[string]any, maxResults int) []string {
	var candidates []any
	// 对齐 Python: L1136-1145 — 多容器路径
	paths := []string{"data.webPages.value", "webPages.value", "data.webPages", "webPages", "data.results", "results"}
	for _, path := range paths {
		if container := getNestedValue(data, path); container != nil {
			if arr, ok := container.([]any); ok {
				candidates = arr
				break
			}
		}
	}

	var urls []string
	for i, item := range candidates {
		if i >= maxResults {
			break
		}
		if m, ok := item.(map[string]any); ok {
			if u, ok := m["url"].(string); ok && u != "" {
				urls = append(urls, u)
			} else if u, ok := m["link"].(string); ok && u != "" {
				urls = append(urls, u)
			}
		}
	}
	return urls
}

// extractBochaAnswer 从 Bocha 响应中提取答案
// 对齐 Python: WebPaidSearchTool._extract_bocha_answer() (web_tools.py L1158-1184)
func extractBochaAnswer(data map[string]any) string {
	// 对齐 Python: L1160-1169 — 直接字段
	candidates := []string{"summary", "answer", "data.summary", "data.answer", "data.message"}
	for _, path := range candidates {
		if v := getNestedValue(data, path); v != nil {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}

	// 对齐 Python: L1171-1183 — 从 webPages.value 中提取
	if webPages, ok := getNestedValue(data, "data.webPages").(map[string]any); ok {
		if value, ok := webPages["value"].([]any); ok {
			var snippets []string
			for i, item := range value {
				if i >= 3 {
					break
				}
				if m, ok := item.(map[string]any); ok {
					if s, ok := m["summary"].(string); ok && strings.TrimSpace(s) != "" {
						snippets = append(snippets, strings.TrimSpace(s))
					} else if s, ok := m["snippet"].(string); ok && strings.TrimSpace(s) != "" {
						snippets = append(snippets, strings.TrimSpace(s))
					}
				}
			}
			if len(snippets) > 0 {
				return strings.Join(snippets[:min(len(snippets), 3)], "\n\n")
			}
		}
	}
	return ""
}

// parsePerplexityCitations 从 Perplexity 响应中提取引用 URL
// 对齐 Python: WebPaidSearchTool._parse_perplexity_citations() (web_tools.py L1242-1258)
func parsePerplexityCitations(data map[string]any) []string {
	// 对齐 Python: L1244-1257
	keys := []string{"citations", "search_results", "web_search_results", "sources"}
	for _, key := range keys {
		if entries, ok := data[key].([]any); ok {
			var urls []string
			for _, item := range entries {
				if s, ok := item.(string); ok && s != "" {
					urls = append(urls, s)
				} else if m, ok := item.(map[string]any); ok {
					for _, k := range []string{"url", "link", "source_url"} {
						if u, ok := m[k].(string); ok && u != "" {
							urls = append(urls, u)
							break
						}
					}
				}
			}
			if len(urls) > 0 {
				return urls
			}
		}
	}
	return nil
}
