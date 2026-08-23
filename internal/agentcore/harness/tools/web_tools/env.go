package web_tools

import (
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// userAgent 模拟浏览器 User-Agent
	// 对齐 Python: _USER_AGENT (web_tools.py L27-31)
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/124.0.0.0 Safari/537.36"

	// freeSearchDebugEnv 调试模式环境变量
	// 对齐 Python: _FREE_SEARCH_DEBUG_ENV (web_tools.py L84)
	freeSearchDebugEnv = "FREE_SEARCH_DEBUG"
	// freeSearchDebugDirEnv 调试输出目录环境变量
	// 对齐 Python: _FREE_SEARCH_DEBUG_DIR_ENV (web_tools.py L85)
	freeSearchDebugDirEnv = "FREE_SEARCH_DEBUG_DIR"
	// freeSearchDDGEnabledEnv DDG 搜索开关环境变量
	// 对齐 Python: _FREE_SEARCH_DDG_ENABLED_ENV (web_tools.py L86)
	freeSearchDDGEnabledEnv = "FREE_SEARCH_DDG_ENABLED"
	// freeSearchBingEnabledEnv Bing 搜索开关环境变量
	// 对齐 Python: _FREE_SEARCH_BING_ENABLED_ENV (web_tools.py L87)
	freeSearchBingEnabledEnv = "FREE_SEARCH_BING_ENABLED"
	// freeSearchProxyURLEnv 免费搜索代理 URL 环境变量
	// 对齐 Python: _FREE_SEARCH_PROXY_URL_ENV (web_tools.py L88)
	freeSearchProxyURLEnv = "FREE_SEARCH_PROXY_URL"
	// freeSearchSSLVerifyEnv SSL 验证环境变量
	// 对齐 Python: _FREE_SEARCH_SSL_VERIFY_ENV (web_tools.py L89)
	freeSearchSSLVerifyEnv = "FREE_SEARCH_SSL_VERIFY"
	// freeSearchDDGURLEnv DDG 搜索 URL 环境变量
	// 对齐 Python: _FREE_SEARCH_DDG_URL_ENV (web_tools.py L90)
	freeSearchDDGURLEnv = "FREE_SEARCH_DDG_URL"

	// paidSearchProviderEnv 付费搜索提供商环境变量
	// 对齐 Python: _PAID_SEARCH_PROVIDER_ENV (web_tools.py L91)
	paidSearchProviderEnv = "PAID_SEARCH_PROVIDER"
	// paidSearchProviderAltEnv 付费搜索提供商备用环境变量
	// 对齐 Python: _PAID_SEARCH_PROVIDER_ALT_ENV (web_tools.py L92)
	paidSearchProviderAltEnv = "WEB_PAID_SEARCH_PROVIDER"

	// paidSearchDefaultTimeoutSeconds 付费搜索默认超时
	// 对齐 Python: _PAID_SEARCH_DEFAULT_TIMEOUT_SECONDS (web_tools.py L101)
	paidSearchDefaultTimeoutSeconds = 180
	// paidSearchMinTimeoutSeconds 付费搜索最小超时
	// 对齐 Python: _PAID_SEARCH_MIN_TIMEOUT_SECONDS (web_tools.py L102)
	paidSearchMinTimeoutSeconds = 30
	// paidSearchMaxTimeoutSeconds 付费搜索最大超时
	// 对齐 Python: _PAID_SEARCH_MAX_TIMEOUT_SECONDS (web_tools.py L103)
	paidSearchMaxTimeoutSeconds = 300

	// pplxDefaultModel Perplexity 默认模型
	// 对齐 Python: _PPLX_DEFAULT_MODEL (web_tools.py L105)
	pplxDefaultModel = "sonar-pro"
	// jinaDefaultModel Jina 默认模型
	// 对齐 Python: _JINA_DEFAULT_MODEL (web_tools.py L107)
	jinaDefaultModel = "jina-deepsearch-v1"

	// fetchWebpageDefaultMaxChars 抓取网页默认最大字符数
	// 对齐 Python: _FETCH_WEBPAGE_DEFAULT_MAX_CHARS (web_tools.py L111)
	fetchWebpageDefaultMaxChars = 20000
	// fetchWebpageDefaultTimeoutSeconds 抓取网页默认超时
	// 对齐 Python: _FETCH_WEBPAGE_DEFAULT_TIMEOUT_SECONDS (web_tools.py L112)
	fetchWebpageDefaultTimeoutSeconds = 45
	// fetchWebpageMaxTimeoutEnv 抓取网页最大超时环境变量
	// 对齐 Python: _FETCH_WEBPAGE_MAX_TIMEOUT_ENV (web_tools.py L113)
	fetchWebpageMaxTimeoutEnv = "MCP_FETCH_WEBPAGE_MAX_TIMEOUT_SECONDS"
	// fetchWebpageMaxCharsEnv 抓取网页最大字符数环境变量
	// 对齐 Python: _FETCH_WEBPAGE_MAX_CHARS_ENV (web_tools.py L114)
	fetchWebpageMaxCharsEnv = "MCP_FETCH_WEBPAGE_MAX_CHARS"

	// freeSearchDefaultNoProxy 默认不走代理的域名列表
	// 对齐 Python: _FREE_SEARCH_DEFAULT_NO_PROXY (web_tools.py L108-110)
	freeSearchDefaultNoProxy = "127.0.0.1,.huawei.com,localhost,local,.local,10.155.97.247,.myhuaweicloud.com, api.openai.rnd.huawei.com"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// paidSearchProviderKeyEnvs 付费搜索提供商 API Key 环境变量映射
	// 对齐 Python: _PAID_SEARCH_PROVIDER_KEY_ENVS (web_tools.py L93-98)
	paidSearchProviderKeyEnvs = map[string]string{
		"perplexity": "PERPLEXITY_API_KEY",
		"bocha":      "BOCHA_API_KEY",
		"jina":       "JINA_API_KEY",
		"serper":     "SERPER_API_KEY",
	}

	// paidSearchProviderOrder 付费搜索提供商降级顺序
	// 对齐 Python: _PAID_SEARCH_PROVIDER_ORDER (web_tools.py L99)
	paidSearchProviderOrder = []string{"perplexity", "bocha", "jina", "serper"}

	// queryStopwords 查询词停用词
	// 对齐 Python: _QUERY_STOPWORDS (web_tools.py L38-51)
	queryStopwords = map[string]bool{
		"the": true, "and": true, "for": true, "with": true,
		"news": true, "latest": true, "today": true,
		"热点": true, "新闻": true, "最新": true, "今日": true, "今天": true,
	}

	// timelyQueryHints 时效性查询提示词
	// 对齐 Python: _TIMELY_QUERY_HINTS (web_tools.py L52-70)
	timelyQueryHints = map[string]bool{
		"news": true, "latest": true, "today": true, "breaking": true,
		"update": true, "updates": true,
		"新闻": true, "最新": true, "今日": true, "今天": true,
		"动态": true, "快讯": true, "头条": true, "热点": true,
		"天气": true, "weather": true, "forecast": true,
	}

	// lowConfidenceResultDomains 低置信度结果域名
	// 对齐 Python: _LOW_CONFIDENCE_RESULT_DOMAINS (web_tools.py L71-79)
	lowConfidenceResultDomains = map[string]bool{
		"zhihu.com": true, "baike.baidu.com": true, "tieba.baidu.com": true,
		"zhidao.baidu.com": true, "douban.com": true, "bilibili.com": true,
		"weibo.com": true,
	}

	// lowFetchValueDomains 低抓取价值域名
	// 对齐 Python: _LOW_FETCH_VALUE_DOMAINS (web_tools.py L80-83)
	lowFetchValueDomains = map[string]bool{
		"mp.weixin.qq.com": true, "so.html5.qq.com": true,
	}

	// pplxAllowedModels Perplexity 允许的模型列表
	// 对齐 Python: _PPLX_ALLOWED_MODELS (web_tools.py L104)
	pplxAllowedModels = map[string]bool{"sonar": true, "sonar-pro": true}

	// jinaAllowedModels Jina 允许的模型列表
	// 对齐 Python: _JINA_ALLOWED_MODELS (web_tools.py L106)
	jinaAllowedModels = map[string]bool{"jina-deepsearch-v1": true}

	// mojibakeMarkers 乱码标记
	// 对齐 Python: _MOJIBAKE_MARKERS (web_tools.py L115)
	mojibakeMarkers = []string{"mojibake", "Ã", "Â", "â", "ï¿½"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsFreeSearchEnabled 是否至少有一个免费搜索后端启用
// 对齐 Python: is_free_search_enabled() (web_tools.py L444-449)
func IsFreeSearchEnabled() bool {
	return envFlag(freeSearchDDGEnabledEnv, false) ||
		envFlag(freeSearchBingEnabledEnv, false)
}

// IsPaidSearchEnabled 是否至少有一个付费搜索提供商 API Key 已配置
// 对齐 Python: is_paid_search_enabled() (web_tools.py L452-454)
func IsPaidSearchEnabled() bool {
	for _, key := range paidSearchAPIKeyEnvs() {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// envFlag 解析布尔型环境变量，空值时使用默认值
// 对齐 Python: _env_flag() (web_tools.py L436-441)
func envFlag(name string, defaultVal bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return defaultVal
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on" || raw == "enabled"
}

// envBool 解析布尔型环境变量
// 对齐 Python: _env_bool() (web_tools.py L123-127)
func envBool(name string, defaultVal bool) bool {
	return envFlag(name, defaultVal)
}

// freeSearchSSLVerify 是否验证 SSL
// 对齐 Python: _free_search_ssl_verify() (web_tools.py L130-131)
func freeSearchSSLVerify() bool {
	return envBool(freeSearchSSLVerifyEnv, false)
}

// getFreeSearchProxyURL 获取免费搜索代理 URL
// 对齐 Python: _get_free_search_proxy_url() (web_tools.py L118-120)
func getFreeSearchProxyURL() string {
	return strings.TrimSpace(os.Getenv(freeSearchProxyURLEnv))
}

// configuredPaidSearchProviders 返回已配置 API Key 的付费搜索提供商（按降级顺序）
// 对齐 Python: _configured_paid_search_providers() (web_tools.py L457-463)
func configuredPaidSearchProviders() []string {
	var result []string
	for _, provider := range paidSearchProviderOrder {
		key := paidSearchProviderKeyEnvs[provider]
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			result = append(result, provider)
		}
	}
	return result
}

// safeEnvChoice 读取环境变量，仅当值在允许列表中时返回
// 对齐 Python: _safe_env_choice() (web_tools.py L466-475)
func safeEnvChoice(name, defaultVal string, allowed map[string]bool) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultVal
	}
	normalized := strings.ToLower(raw)
	if allowed[normalized] {
		return normalized
	}
	logger.Warn(logComponent).
		Str("env", name).
		Str("value", raw).
		Str("default", defaultVal).
		Msg("环境变量值不在允许列表中，使用默认值")
	return defaultVal
}

// paidSearchAPIKeyEnvs 返回所有付费搜索 API Key 环境变量名
// 对齐 Python: _PAID_SEARCH_API_KEY_ENVS (web_tools.py L100)
func paidSearchAPIKeyEnvs() []string {
	var result []string
	for _, key := range paidSearchProviderOrder {
		result = append(result, paidSearchProviderKeyEnvs[key])
	}
	return result
}

// noProxyEntries 返回 NO_PROXY 配置列表
// 对齐 Python: _no_proxy_entries() (web_tools.py L147-149)
func noProxyEntries() []string {
	configured := os.Getenv("NO_PROXY")
	if configured == "" {
		configured = os.Getenv("no_proxy")
	}
	if configured == "" {
		configured = freeSearchDefaultNoProxy
	}
	var entries []string
	for _, entry := range strings.Split(configured, ",") {
		trimmed := strings.TrimSpace(strings.ToLower(entry))
		if trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}

// isDebugEnabled 调试模式是否启用
// 对齐 Python: _is_debug_enabled() (web_tools.py L305-307)
func isDebugEnabled() bool {
	return envFlag(freeSearchDebugEnv, false)
}

// getDebugDir 获取调试输出目录
// 对齐 Python: _get_debug_dir() (web_tools.py L310-315)
func getDebugDir() string {
	configured := strings.TrimSpace(os.Getenv(freeSearchDebugDirEnv))
	if configured != "" {
		return configured
	}
	return ".tmp/free_search_debug"
}
