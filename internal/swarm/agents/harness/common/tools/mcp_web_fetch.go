// mcp_fetch_webpage 工具：网页抓取。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/web_fetch_tools.py（273 行）。

package tools

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/saintfish/chardet"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mcpFetchWebpageInput mcp_fetch_webpage 工具输入参数
// Python: mcp_fetch_webpage(url, max_chars=0, timeout_seconds=30)
type mcpFetchWebpageInput struct {
	// URL 目标地址
	URL string `json:"url" jsonschema:"description=The URL to fetch,required"`
	// MaxChars 输出裁剪上限（0 不裁剪）
	MaxChars int `json:"max_chars" jsonschema:"description=Maximum output chars before clipping (0 disables clipping)"`
	// TimeoutSeconds 超时秒数（默认 30）
	TimeoutSeconds int `json:"timeout_seconds" jsonschema:"description=Timeout in seconds for the fetch request"`
}

// fetchWebpageResult 网页抓取结果（对齐 Python dict: {url, status_code, title, content}）
type fetchWebpageResult struct {
	// URL 最终地址（响应后的 URL）
	URL string
	// StatusCode HTTP 状态码
	StatusCode int
	// Title 页面标题
	Title string
	// Content 正文内容
	Content string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fetchWebpageMaxTimeoutEnv 超时上限环境变量
	fetchWebpageMaxTimeoutEnv = "MCP_FETCH_WEBPAGE_MAX_TIMEOUT_SECONDS"
	// fetchWebpageDefaultTimeout 默认超时
	fetchWebpageDefaultTimeout = 30
	// fetchWebpageDefaultMaxTimeout 超时上限默认值
	fetchWebpageDefaultMaxTimeout = 3600
	// fetchWebpageFallbackMaxTimeout env 解析失败回退值
	fetchWebpageFallbackMaxTimeout = 3600
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// charsetHeaderPattern Content-Type header charset 正则
	// Python: _CHARSET_HEADER_RE
	charsetHeaderPattern = regexp.MustCompile(`(?i)charset=([^\s;]+)`)
	// charsetMetaPattern meta 标签 charset 正则（4KB 头部扫描）
	// Python: _CHARSET_META_RE（bytes 模式）
	charsetMetaPattern = regexp.MustCompile(`(?i)<meta[^>]+charset=["']?\s*([A-Za-z0-9._-]+)`)
	// titlePattern title 标签正则
	titlePattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	// scriptStripPattern script 标签剥离
	scriptStripPattern = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	// styleStripPattern style 标签剥离
	styleStripPattern = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	// fetchTagStripPattern 标签剥离（web_fetch 文件独立副本）
	fetchTagStripPattern = regexp.MustCompile(`<[^>]+>`)
	// fetchWhitespacePattern 空白折叠（web_fetch 文件独立副本）
	fetchWhitespacePattern = regexp.MustCompile(`\s+`)

	// jinaReaderURLPrefix Jina Reader 前缀（测试注入点）
	jinaReaderURLPrefix = "https://r.jina.ai/"
)

// McpFetchWebpage mcp_fetch_webpage 工具单例（对齐 Python @tool 模块级实例）
var McpFetchWebpage = newMcpFetchWebpageTool()

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractDeclaredCharset 从 Content-Type header 或 HTML meta 提取声明编码。
// Python: _extract_declared_charset(response)（header → 4KB meta 二级）
func extractDeclaredCharset(contentType string, headBytes []byte) string {
	if contentType != "" {
		if match := charsetHeaderPattern.FindStringSubmatch(contentType); match != nil {
			return strings.TrimSpace(strings.Trim(match[1], "\"'"))
		}
	}
	if len(headBytes) == 0 {
		return ""
	}
	// 4KB 限制（Python L41: head_bytes[:4096]）
	limit := len(headBytes)
	if limit > 4096 {
		limit = 4096
	}
	if match := charsetMetaPattern.FindSubmatch(headBytes[:limit]); match != nil {
		return strings.TrimSpace(string(match[1]))
	}
	return ""
}

// decodeResponseText 按候选链严格解码响应文本。
// Python: _decode_response_text —— 候选链：
// declared（非拉丁）→ utf-8 → apparent → response_encoding → gb18030 → big5
// → shift_jis → cp1252 → iso-8859-1；去重 strict 尝试，最后 utf-8 replace。
func decodeResponseText(raw []byte, declared, responseEncoding, apparent string) string {
	if len(raw) == 0 {
		return ""
	}

	declaredLower := strings.ToLower(strings.TrimSpace(declared))
	var candidates []string
	// 显式非拉丁声明优先
	if declaredLower != "" && declaredLower != "iso-8859-1" && declaredLower != "latin-1" && declaredLower != "latin1" {
		candidates = append(candidates, declaredLower)
	}
	candidates = append(candidates,
		"utf-8",
		strings.ToLower(strings.TrimSpace(apparent)),
		strings.ToLower(strings.TrimSpace(responseEncoding)),
		"gb18030",
		"big5",
		"shift_jis",
		"cp1252",
		"iso-8859-1",
	)

	seen := make(map[string]bool)
	for _, enc := range candidates {
		enc = strings.ToLower(strings.TrimSpace(enc))
		if enc == "" || seen[enc] {
			continue
		}
		seen[enc] = true
		if decoded, err := decodeWithEncoding(raw, enc); err == nil {
			return decoded
		}
	}
	// 最后兜底：utf-8 replace（Go string 转换天然 replace 无效字节）
	return string(raw)
}

// decodeWithEncoding 用指定编码严格解码（失败返回 error，对齐 Python errors="strict"）。
// x/text 解码器对无效字节产生替换符 —— 严格模式由解码结果回检实现：
// 解码后若含 U+FFFD 替换符则视为失败（近似 Python strict 语义）。
func decodeWithEncoding(raw []byte, enc string) (string, error) {
	decodeOrErr := func(decoded []byte, err error) (string, error) {
		if err != nil {
			return "", err
		}
		s := string(decoded)
		if strings.ContainsRune(s, '\uFFFD') {
			return "", fmt.Errorf("decode produced replacement char (strict mode)")
		}
		return s, nil
	}
	switch strings.ToLower(enc) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		// Go string 即 UTF-8——严格校验
		if !utf8Valid(raw) {
			return "", fmt.Errorf("invalid utf-8")
		}
		return string(raw), nil
	case "gb18030":
		return decodeOrErr(simplifiedchinese.GB18030.NewDecoder().Bytes(raw))
	case "gbk", "gb2312", "cp936":
		// Python gbk 是 gb18030 子集；x/text GBK 解码器即 cp936
		return decodeOrErr(simplifiedchinese.GBK.NewDecoder().Bytes(raw))
	case "big5", "big5-hkscs":
		return decodeOrErr(traditionalchinese.Big5.NewDecoder().Bytes(raw))
	case "shift_jis", "shift-jis", "sjis", "cp932":
		return decodeOrErr(japanese.ShiftJIS.NewDecoder().Bytes(raw))
	case "cp1252", "windows-1252":
		return decodeOrErr(charmap.Windows1252.NewDecoder().Bytes(raw))
	case "iso-8859-1", "latin-1", "latin1":
		return decodeOrErr(charmap.ISO8859_1.NewDecoder().Bytes(raw))
	default:
		return "", fmt.Errorf("unsupported encoding: %s", enc)
	}
}

// utf8Valid 校验字节序列是否合法 UTF-8（Go 1.18+ 标准库等价）。
func utf8Valid(b []byte) bool {
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			i++
			continue
		}
		size := utf8SequenceLen(b[i])
		if size == 0 || i+size > len(b) {
			return false
		}
		for _, c := range b[i+1 : i+size] {
			if c&0xC0 != 0x80 {
				return false
			}
		}
		i += size
	}
	return true
}

// utf8SequenceLen UTF-8 首字节序列长度（非法返回 0）。
func utf8SequenceLen(first byte) int {
	switch {
	case first&0xE0 == 0xC0:
		return 2
	case first&0xF0 == 0xE0:
		return 3
	case first&0xF8 == 0xF0:
		return 4
	default:
		return 0
	}
}

// detectApparentCharset chardet 探测明显编码（对齐 Python response.apparent_encoding）。
func detectApparentCharset(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	detector := chardet.NewTextDetector()
	result, err := detector.DetectBest(raw)
	if err != nil || result == nil {
		return ""
	}
	return strings.ToLower(result.Charset)
}

// clipTextFetch 裁剪文本（web_fetch 文件独立副本）。
// Python: _clip_text（本文件副本，mcp_command.go 另有一份——对齐 Python 双文件边界）
func clipTextFetch(value string, maxChars int) string {
	if maxChars <= 0 || len(value) <= maxChars {
		return value
	}
	return value[:maxChars] + "\n...[truncated]"
}

// stripTagsFetch 剥离 HTML 标签并折叠空白（web_fetch 文件独立副本）。
// Python: _strip_tags（本文件副本）
func stripTagsFetch(value string) string {
	value = fetchTagStripPattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(fetchWhitespacePattern.ReplaceAllString(html.UnescapeString(value), " "))
}

// decodeDDGRedirectFetch 解码 DDG 重定向（web_fetch 文件独立副本）。
// Python: _decode_ddg_redirect（本文件副本）
func decodeDDGRedirectFetch(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if parsed.Path != "/l/" {
		return u
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return u
	}
	target := values.Get("uddg")
	if target == "" {
		return u
	}
	if decoded, err := url.QueryUnescape(target); err == nil {
		return decoded
	}
	return u
}

// normalizeURL 归一化 URL（DDG 解码 + https 前缀补齐）。
// Python: _normalize_url
func normalizeURL(u string) string {
	raw := strings.TrimSpace(u)
	if raw == "" {
		return raw
	}
	decoded := decodeDDGRedirectFetch(raw)
	if strings.HasPrefix(decoded, "http://") || strings.HasPrefix(decoded, "https://") {
		return decoded
	}
	return "https://" + decoded
}

// fetchHTTPGet GET 请求（verify + proxy 语义，web_fetch 文件独立副本）。
// Python: _http_get（本文件副本）
func fetchHTTPGet(rawURL string, timeoutSeconds int, headers map[string]string) (*http.Response, []byte, error) {
	return httpDo("GET", rawURL, nil, headers, timeoutSeconds, freeSearchSSLVerify(), getFreeSearchProxyURL())
}

// fetchViaJinaReader 经 Jina Reader 抓取。
// Python: _fetch_via_jina_reader_sync —— 401/403/429 兜底链路
func fetchViaJinaReader(u string, timeoutSeconds int) (*fetchWebpageResult, error) {
	readerURL := jinaReaderURLPrefix + u
	resp, data, err := fetchHTTPGet(readerURL, timeoutSeconds, map[string]string{"User-Agent": searchUA})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jina reader returned non-2xx status: %d", resp.StatusCode)
	}
	return &fetchWebpageResult{
		URL:        u,
		StatusCode: resp.StatusCode,
		Title:      "",
		Content:    strings.TrimSpace(decodeResponseText(data, "", "", "")),
	}, nil
}

// fetchWebpage 抓取网页正文。
// Python: _fetch_webpage_sync —— 401/403/429 → Jina；HTML script/style 剥离 + stripTags
func fetchWebpage(u string, timeoutSeconds int) (*fetchWebpageResult, error) {
	resp, data, err := fetchHTTPGet(u, timeoutSeconds, map[string]string{"User-Agent": searchUA})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
		return fetchViaJinaReader(u, timeoutSeconds)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("webpage returned non-2xx status: %d", resp.StatusCode)
	}

	declared := extractDeclaredCharset(resp.Header.Get("Content-Type"), data)
	// Python response.encoding：requests 从 header 推断（Go 侧 header charset 已由 declared 覆盖，
	// 此处传空对齐"header 无声明时 encoding 为 None"的分支）
	apparent := detectApparentCharset(data)
	text := decodeResponseText(data, declared, "", apparent)

	contentType := resp.Header.Get("Content-Type")
	var title string
	if match := titlePattern.FindStringSubmatch(text); match != nil {
		title = stripTagsFetch(match[1])
	}

	if strings.Contains(strings.ToLower(contentType), "html") {
		text = scriptStripPattern.ReplaceAllString(text, " ")
		text = styleStripPattern.ReplaceAllString(text, " ")
		text = stripTagsFetch(text)
	} else {
		text = strings.TrimSpace(fetchWhitespacePattern.ReplaceAllString(text, " "))
	}

	finalURL := u
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return &fetchWebpageResult{
		URL:        finalURL,
		StatusCode: resp.StatusCode,
		Title:      title,
		Content:    text,
	}, nil
}

// newMcpFetchWebpageTool 构造 mcp_fetch_webpage 工具实例。
// Python: @tool 装饰的 mcp_fetch_webpage（description 逐字）
func newMcpFetchWebpageTool() tool.Tool {
	card := tool.NewToolCard(
		"mcp_fetch_webpage",
		"Fetch webpage text content from URL. Returns status/title/plain text content. "+
			"Set max_chars=0 to disable output clipping. "+
			"Use a larger timeout_seconds for slow websites.",
		nil, nil,
	)

	fn := func(ctx context.Context, input mcpFetchWebpageInput, opts ...tool.ToolOption) (map[string]any, error) {
		u := normalizeURL(input.URL)
		if u == "" {
			return map[string]any{"result": "[ERROR]: url cannot be empty."}, nil
		}

		maxChars := input.MaxChars
		if maxChars < 0 {
			maxChars = 0
		}
		timeoutSeconds := input.TimeoutSeconds
		if timeoutSeconds == 0 {
			timeoutSeconds = fetchWebpageDefaultTimeout
		}
		maxTimeoutSeconds := fetchWebpageDefaultMaxTimeout
		if raw := os.Getenv(fetchWebpageMaxTimeoutEnv); raw != "" {
			if parsed, pErr := strconv.Atoi(raw); pErr == nil {
				maxTimeoutSeconds = parsed
			} else {
				maxTimeoutSeconds = fetchWebpageFallbackMaxTimeout
			}
		}
		if maxTimeoutSeconds < 1 {
			maxTimeoutSeconds = 1
		}
		if timeoutSeconds < 1 {
			timeoutSeconds = 1
		}
		if timeoutSeconds > maxTimeoutSeconds {
			timeoutSeconds = maxTimeoutSeconds
		}

		data, err := fetchWebpage(u, timeoutSeconds)
		if err != nil {
			return map[string]any{"result": fmt.Sprintf("[ERROR]: failed to fetch webpage: %v", err)}, nil
		}

		lines := []string{
			fmt.Sprintf("URL: %s", data.URL),
			fmt.Sprintf("Status: %d", data.StatusCode),
		}
		if data.Title != "" {
			lines = append(lines, fmt.Sprintf("Title: %s", data.Title))
		}
		lines = append(lines, "Content:")
		content := clipTextFetch(data.Content, maxChars)
		if content == "" {
			content = "[empty]"
		}
		lines = append(lines, content)
		return map[string]any{"result": strings.Join(lines, "\n")}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}
