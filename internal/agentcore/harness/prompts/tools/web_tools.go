package tools

// ──────────────────────────── 结构体 ────────────────────────────

// FreeSearchMetadataProvider free_search 工具元数据提供者
type FreeSearchMetadataProvider struct{}

// PaidSearchMetadataProvider paid_search 工具元数据提供者
type PaidSearchMetadataProvider struct{}

// FetchWebpageMetadataProvider fetch_webpage 工具元数据提供者
type FetchWebpageMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// freeSearchDescription free_search 工具双语描述
var freeSearchDescription = map[string]string{
	"cn": `如果 paid_search 可用或已配置 API，优先使用 paid_search；free_search 仅作为兜底或用户明确要求免费搜索时使用。
免费搜索，返回结果 URL 和摘要。如果前几条结果看起来相关但还不足以直接回答任务，应先抓取前 1-3 条中的至少 2 条；如果第一条抓取失败、是动态壳页或内容仍然不完整，就继续抓下一条，而不是立刻继续改写搜索词。
当用户询问最新、当前、今年、实时、近期等信息时，query 必须使用系统提示中的当前年份或日期；`,
	"en": `Free search. If paid_search is available/configured, call paid_search first; use free_search only as fallback or when the user explicitly asks for free search.
Input a query and return result URLs with snippets.
If the top results look relevant but do not directly answer the task, you must fetch at least 2 of the top 1-3 results first.
If the first fetch fails, is a dynamic shell page, or is still incomplete, continue with the next result instead of searching again immediately.
For latest/current/this-year/recent information, the query must use the current year or date from the system prompt.`,
}

// paidSearchDescription paid_search 工具双语描述
var paidSearchDescription = map[string]string{
	"cn": `配置 API 时这是首选联网搜索工具；对搜索、最新、当前信息任务应先调用 paid_search，再考虑 free_search 兜底。
付费搜索，支持 provider=auto|bocha|perplexity|serper|jina。
当用户询问最新、当前、今年、实时、近期等信息时，query 必须使用系统提示中的当前年份或日期；`,
	"en": `Paid search via Bocha/Perplexity/SERPER/JINA. Support provider=auto|bocha|perplexity|serper|jina.
When available, this is the preferred web search tool; call it before free_search for search, latest, current, or recent-information tasks.
For latest/current/this-year/recent information, the query must use the current year or date from the system prompt.`,
}

// fetchWebpageDescription fetch_webpage 工具双语描述
var fetchWebpageDescription = map[string]string{
	"cn": `通常配合 paid_search 或 free_search 使用：先搜索，再抓取结果页，不要只依赖摘要。
抓取网页文本，返回状态码、标题和正文文本。可设置 max_chars=0 关闭截断，也可以调大 timeout_seconds 处理慢站点。
适用场景：文档、博客、新闻、API 参考等普通网页。
代码仓地址（GitHub/GitLab/Gitee/Gitcode/Bitbucket 等）一般不适合用本工具——网页只能看到渲染后的目录页;要读源码、看历史、跨文件搜索，更顺手的方式是用 shell 工具(bash 或 powershell)执行 ` + "`git clone`" + ` 拉到本地。`,
	"en": `Fetch webpage text content from a URL and return status, title, and plain text.
Usually used after paid_search or free_search: search first, then fetch the top few result pages instead of reasoning only from snippets. Set max_chars=0 to disable clipping and use a larger timeout_seconds for slow pages.
Best fit: documentation, blog posts, news, API references, and similar general web content.
Git repository URLs (GitHub/GitLab/Gitee/Gitcode/Bitbucket, etc.) are usually a poor fit - the webpage only shows a rendered file tree, while reading source, history, or searching across files is far easier after a local ` + "`git clone`" + ` via the shell tool (bash or powershell).`,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetFreeSearchMetadataProviderInputParams 构建 free_search 工具的参数 Schema
func GetFreeSearchMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"query":           {"cn": "搜索查询文本。查询最新、当前、今年、实时、近期信息时，必须使用系统提示中的当前年份或日期。", "en": "Free search query text. For latest/current/this-year/recent information, use the current year or date from the system prompt."},
		"max_results":     {"cn": "最大结果数（1-20）。", "en": "Maximum number of results (1-20)."},
		"timeout_seconds": {"cn": "请求超时时间（秒，5-60）。", "en": "Request timeout in seconds (5-60)."},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":           map[string]any{"type": "string", "description": d("query")},
			"max_results":     map[string]any{"type": "integer", "description": d("max_results"), "default": 8},
			"timeout_seconds": map[string]any{"type": "integer", "description": d("timeout_seconds"), "default": 20},
		},
		"required": []any{"query"},
	}
}

// GetPaidSearchMetadataProviderInputParams 构建 paid_search 工具的参数 Schema
func GetPaidSearchMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"query":           {"cn": "付费搜索查询文本。", "en": "Paid search query text."},
		"provider":        {"cn": "Provider: auto|bocha|perplexity|serper|jina。", "en": "Provider: auto|bocha|perplexity|serper|jina."},
		"max_results":     {"cn": "最大 URL 数（1-20）。", "en": "Maximum number of URLs (1-20)."},
		"timeout_seconds": {"cn": "请求超时时间（秒，30-300）。", "en": "Request timeout in seconds (30-300)."},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":           map[string]any{"type": "string", "description": d("query")},
			"provider":        map[string]any{"type": "string", "description": d("provider"), "default": "auto"},
			"max_results":     map[string]any{"type": "integer", "description": d("max_results"), "default": 8},
			"timeout_seconds": map[string]any{"type": "integer", "description": d("timeout_seconds"), "default": 180, "minimum": 30, "maximum": 300},
		},
		"required": []any{"query"},
	}
}

// GetFetchWebpageMetadataProviderInputParams 构建 fetch_webpage 工具的参数 Schema
func GetFetchWebpageMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"url":             {"cn": "要抓取的网页 URL。", "en": "Webpage URL to fetch."},
		"max_chars":       {"cn": "返回内容最大字符数；设为 0 表示不截断。", "en": "Maximum content characters. Set to 0 to disable clipping."},
		"timeout_seconds": {"cn": "请求超时时间（秒）；慢站点可适当调大。", "en": "Request timeout in seconds. Larger values can be used for slow websites."},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":             map[string]any{"type": "string", "description": d("url")},
			"max_chars":       map[string]any{"type": "integer", "description": d("max_chars"), "default": 20000},
			"timeout_seconds": map[string]any{"type": "integer", "description": d("timeout_seconds"), "default": 45},
		},
		"required": []any{"url"},
	}
}

func (p *FreeSearchMetadataProvider) GetName() string { return "free_search" }
func (p *FreeSearchMetadataProvider) GetDescription(language string) string {
	if d, ok := freeSearchDescription[language]; ok {
		return d
	}
	return freeSearchDescription["cn"]
}
func (p *FreeSearchMetadataProvider) GetInputParams(language string) map[string]any {
	return GetFreeSearchMetadataProviderInputParams(language)
}

func (p *PaidSearchMetadataProvider) GetName() string { return "paid_search" }
func (p *PaidSearchMetadataProvider) GetDescription(language string) string {
	if d, ok := paidSearchDescription[language]; ok {
		return d
	}
	return paidSearchDescription["cn"]
}
func (p *PaidSearchMetadataProvider) GetInputParams(language string) map[string]any {
	return GetPaidSearchMetadataProviderInputParams(language)
}

func (p *FetchWebpageMetadataProvider) GetName() string { return "fetch_webpage" }
func (p *FetchWebpageMetadataProvider) GetDescription(language string) string {
	if d, ok := fetchWebpageDescription[language]; ok {
		return d
	}
	return fetchWebpageDescription["cn"]
}
func (p *FetchWebpageMetadataProvider) GetInputParams(language string) map[string]any {
	return GetFetchWebpageMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&FreeSearchMetadataProvider{})
	RegisterToolProvider(&PaidSearchMetadataProvider{})
	RegisterToolProvider(&FetchWebpageMetadataProvider{})
}
