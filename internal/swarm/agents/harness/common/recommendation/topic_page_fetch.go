package recommendation

// topic_page_fetch.go 仅服务 topic 检索的页面抓取。
// 共享 mcp_fetch_webpage 工具保留其原文本抽取；topic 证据需要发布时间与
// 无导航的文章正文，因此解析逻辑收敛在此文件、底层调用共享 HTTP 通道。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_page_fetch.py
// （342 行，一比一移植；lxml → golang.org/x/net/html 等价转换）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding/unicode"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultRedirectLimit 重定向上限（对齐 Python requests.models.DEFAULT_REDIRECT_LIMIT）。
	defaultRedirectLimit = 30
)

// msnHosts msn 文章页宿主（对齐 :54 _MSN_HOSTS）。
var msnHosts = []string{"msn.com", "msn.cn"}

// msnArticleSegmentRE ar-AAxxxx 段（对齐 :55）。
var msnArticleSegmentRE = regexp.MustCompile(`ar-(AA[0-9A-Za-z]+)`)

// msnDetailAPIURLPrefix msn detail API 前缀（对齐 :56）。
const msnDetailAPIURLPrefix = "https://assets.msn.com/content/view/v2/Detail/zh-cn/"

// jinaReaderBaseURL Jina reader 基址（对齐 :216 f-string；测试可注入替换）。
var jinaReaderBaseURL = "https://r.jina.ai/"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// metaTagRE meta 标签（对齐 :36 _META_TAG_RE）。
	metaTagRE = regexp.MustCompile(`(?i)<meta\b[^>]*>`)
	// metaAttributeRE 属性键值（对齐 :37-40；RE2 无反向引用，
	// 用双引号/单引号两支捕获组手写等价，第 3/5 组取值）。
	metaAttributeRE = regexp.MustCompile(`(?is)([\w:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	// jsonLDPublishedRE JSON-LD datePublished（对齐 :41-43）。
	jsonLDPublishedRE = regexp.MustCompile(`(?i)["']datePublished["']\s*:\s*["']([^"']+)["']`)
	// jinaPublishedRE Jina 发布时间行（对齐 :44-46）。
	jinaPublishedRE = regexp.MustCompile(`(?im)^Published Time:\s*(\S.*?)\s*$`)
	// jinaTitleRE Jina 标题行（对齐 :47-49）。
	jinaTitleRE = regexp.MustCompile(`(?im)^Title:[ \t]*(.*?)[ \t]*\r?$`)
	// jinaContentRE Jina 正文标记行（对齐 :50-52）。
	jinaContentRE = regexp.MustCompile(`(?im)^Markdown Content:[ \t]*\r?$`)
	// markdownATXHeadingRE ATX 标题（对齐 :53-56）。
	markdownATXHeadingRE = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+[ \t]*)?\r?$`)
	// markdownSetextHeadingRE Setext 标题（对齐 :57-60）。
	markdownSetextHeadingRE = regexp.MustCompile(`(?m)^([^\r\n]+)\r?\n[ \t]{0,3}(?:=+|-+)[ \t]*\r?$`)
	// arxivHistoryRE arXiv 版本历史日期（对齐 :66-70 内联正则）。
	arxivHistoryTemplate = `\[v%s\]\s*` +
		`([A-Za-z]{3},\s*\d{1,2}\s+[A-Za-z]{3}\s+\d{4}\s+\d{2}:\d{2}:\d{2}\s+UTC)`
	// arxivPathRE /abs/ 路径与版本（对齐 :51 paper 正则）。
	arxivPathRE = regexp.MustCompile(`/(\d{4}\.\d{4,5})(?:v(\d+))?/?$`)
	// arxivCitedVersionRE 正文引用版本（对齐 :54）。
	arxivCitedVersionRE = regexp.MustCompile(`(?i)arXiv:%sv(\d+)\b`)
	// htmlTitleRE <title>（对齐 :259）。
	htmlTitleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	// tagRE 去标签（对齐 :50 re.sub(r"<[^>]+>")）。
	tagRE = regexp.MustCompile(`<[^>]+>`)
	// whitespaceRE 空白折叠（对齐多处 re.sub(r"\s+", " ")）。
	whitespaceRE = regexp.MustCompile(`\s+`)
	// sqliteHeadingRE sqlite 发布标题（对齐 :129-131）。
	sqliteHeadingRE = regexp.MustCompile(`(?i)<h2>\s*SQLite Release [0-9.]+ On (\d{4}-\d{2}-\d{2})\s*</h2>`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractPublishedAt 读取显式发布字段；绝不从正文任意日期推断
// （对齐 :48-133 extract_published_at）。
func ExtractPublishedAt(text, urlText string) (string, string) {
	parsedURL, _ := url.Parse(urlText)
	// arXiv abs 页：从版本历史读显式时间
	if parsedURL != nil && (parsedURL.Hostname() == "arxiv.org" || parsedURL.Hostname() == "www.arxiv.org") &&
		strings.HasPrefix(parsedURL.Path, "/abs/") {
		pageText := htmlUnescape(tagRE.ReplaceAllString(text, " "))
		paper := arxivPathRE.FindStringSubmatch(parsedURL.Path)
		version := ""
		if paper != nil && len(paper) > 2 {
			version = paper[2]
		}
		if paper != nil && version == "" {
			cited := arxivCitedVersionRE.FindStringSubmatch(
				strings.ReplaceAll(regexp.QuoteMeta(paper[1]), `\E`, paper[1]))
			_ = cited
			if m := regexp.MustCompile(`(?i)arXiv:` + regexp.QuoteMeta(paper[1]) + `v(\d+)\b`).FindStringSubmatch(pageText); m != nil {
				version = m[1]
			}
		}
		if version != "" {
			history := regexp.MustCompile(fmt.Sprintf(arxivHistoryTemplate, regexp.QuoteMeta(version))).
				FindStringSubmatch(pageText)
			if history != nil {
				parsedDate, err := time.Parse("Mon, 02 Jan 2006 15:04:05 MST", history[1])
				if err == nil {
					return parsedDate.UTC().Format("2006-01-02T15:04:05Z07:00"), "arxiv.version_history"
				}
				return parsedDate.Format("2006-01-02T15:04:05Z07:00"), "arxiv.version_history"
			}
			if version != "1" {
				return "", ""
			}
		}
	}
	citationDate := ""
	citationOnlineDate := ""
	for _, tag := range metaTagRE.FindAllString(text, -1) {
		attributes := map[string]string{}
		for _, m := range metaAttributeRE.FindAllStringSubmatch(tag, -1) {
			// RE2 两支捕获组：双引号取 m[2]，单引号取 m[3]（等价 Python \2 回引）
			value := m[2]
			if value == "" {
				value = m[3]
			}
			attributes[strings.ToLower(m[1])] = strings.TrimSpace(htmlUnescape(value))
		}
		key := strings.ToLower(strings.TrimSpace(attributes["property"]))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(attributes["name"]))
		}
		value := strings.TrimSpace(attributes["content"])
		switch key {
		case "article:published_time", "og:published_time", "datepublished", "date":
			if value != "" {
				return value, "meta." + key
			}
		case "citation_date":
			if value != "" && citationDate == "" {
				citationDate = value
			}
		case "citation_online_date":
			if value != "" && citationOnlineDate == "" {
				citationOnlineDate = value
			}
		}
	}
	citationValue := citationDate
	if citationValue == "" {
		citationValue = citationOnlineDate
	}
	if citationValue != "" {
		if parsed, err := time.Parse("2006/01/02", citationValue); err == nil {
			key := "citation_online_date"
			if citationDate != "" {
				key = "citation_date"
			}
			return parsed.UTC().Format("2006-01-02T15:04:05Z07:00"), "meta." + key
		}
	}
	if m := jsonLDPublishedRE.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(htmlUnescape(m[1])), "json_ld.datePublished"
	}
	if m := jinaPublishedRE.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1]), "jina_reader.published_time"
	}
	if parsedURL != nil && (parsedURL.Hostname() == "sqlite.org" || parsedURL.Hostname() == "www.sqlite.org") &&
		strings.HasPrefix(parsedURL.Path, "/releaselog/") {
		if heading := sqliteHeadingRE.FindStringSubmatch(text); heading != nil {
			return heading[1], "sqlite.release_heading"
		}
	}
	return "", ""
}

// MsnDetailAPIURL msn.com / msn.cn 文章页的 detail API 地址——页面 HTML 是脚本壳
// （对齐 :168-176 msn_detail_api_url）。
func MsnDetailAPIURL(urlText string) string {
	parsed, err := url.Parse(urlText)
	if err != nil {
		return ""
	}
	hostname := strings.ToLower(parsed.Hostname())
	isMSN := false
	for _, host := range msnHosts {
		if hostname == host || strings.HasSuffix(hostname, "."+host) {
			isMSN = true
			break
		}
	}
	if !isMSN {
		return ""
	}
	segments := strings.Split(strings.TrimRight(parsed.Path, "/"), "/")
	last := segments[len(segments)-1]
	article := msnArticleSegmentRE.FindStringSubmatch(last)
	if article == nil {
		return ""
	}
	return msnDetailAPIURLPrefix + article[1]
}

// FetchTopicPageViaJinaSync 经 Jina reader 读取被挡页面，保留标题与发布时间
// （对齐 :208-236 fetch_topic_page_via_jina_sync）。
func FetchTopicPageViaJinaSync(urlText string, timeoutSeconds int, publicHTTPS bool) (map[string]any, error) {
	readerURL := jinaReaderBaseURL + urlText
	resp, err := fetchVerifiedResponse(readerURL, timeoutSeconds, publicHTTPS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.body.Close() }()
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		return nil, fmt.Errorf("jina reader fetch failed: status %d", resp.statusCode)
	}
	text := strings.TrimSpace(resp.text)
	publishedAt, publishedAtSource := ExtractPublishedAt(text, urlText)
	title := ""
	if m := jinaTitleRE.FindStringSubmatch(text); m != nil {
		title = htmlUnescape(strings.TrimSpace(m[1]))
	}
	result := map[string]any{
		"url":          urlText,
		"status_code":  resp.statusCode,
		"title":        title,
		"content":      extractJinaReaderContent(text, title),
		"fetch_reader": "jina-reader",
	}
	if publishedAt != "" && publishedAtSource != "" {
		result["published_at"] = publishedAt
		result["published_at_source"] = publishedAtSource
	}
	return result, nil
}

// FetchTopicPageSync 抓取一个 topic 来源，带发布时间与文章正文
// （对齐 :241-283 fetch_topic_page_sync）。
func FetchTopicPageSync(urlText string, timeoutSeconds int, publicHTTPS bool) (map[string]any, error) {
	resp, err := fetchVerifiedResponse(urlText, timeoutSeconds, publicHTTPS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.body.Close() }()
	if resp.statusCode == 401 || resp.statusCode == 403 || resp.statusCode == 429 {
		return FetchTopicPageViaJinaSync(urlText, timeoutSeconds, publicHTTPS)
	}
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		return nil, fmt.Errorf("topic page fetch failed: status %d", resp.statusCode)
	}
	text := resp.text
	publishedAt, publishedAtSource := ExtractPublishedAt(text, resp.finalURL)
	contentType := resp.contentType
	title := ""
	if m := htmlTitleRE.FindStringSubmatch(text); m != nil {
		title = stripTagsTopic(m[1])
	}
	if strings.Contains(strings.ToLower(contentType), "html") {
		text = extractHTMLContent(text, title)
	} else {
		text = strings.TrimSpace(whitespaceRE.ReplaceAllString(text, " "))
	}
	result := map[string]any{
		"url":          resp.finalURL,
		"status_code":  resp.statusCode,
		"title":        title,
		"content":      text,
		"fetch_reader": "direct",
	}
	if publishedAt != "" && publishedAtSource != "" {
		result["published_at"] = publishedAt
		result["published_at_source"] = publishedAtSource
	}
	return result, nil
}

// FetchMsnDetailSync 从 msn detail API 读取文章：正文、发布时间与原文 URL
// （对齐 :285-319 fetch_msn_detail_sync）。
func FetchMsnDetailSync(urlText, apiURL string, timeoutSeconds int, publicHTTPS bool) (map[string]any, error) {
	resp, err := fetchVerifiedResponse(apiURL, timeoutSeconds, publicHTTPS)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.body.Close() }()
	if resp.statusCode < 200 || resp.statusCode >= 300 {
		return nil, fmt.Errorf("msn detail fetch failed: status %d", resp.statusCode)
	}
	detail := map[string]any{}
	if err := json.Unmarshal([]byte(resp.text), &detail); err != nil {
		detail = map[string]any{}
	}
	title, _ := detail["title"].(string)
	sourceHref := strings.TrimSpace(stringValue(detail["sourceHref"]))
	parsedHref, _ := url.Parse(sourceHref)
	finalURL := urlText
	if parsedHref != nil && (parsedHref.Scheme == "http" || parsedHref.Scheme == "https") && parsedHref.Host != "" {
		finalURL = sourceHref
	}
	result := map[string]any{
		"url":          finalURL,
		"status_code":  resp.statusCode,
		"title":        title,
		"content":      extractHTMLContent(stringValue(detail["body"]), title),
		"fetch_reader": "msn-detail-api",
		"api_url":      apiURL,
	}
	publishedAt := strings.TrimSpace(stringValue(detail["publishedDateTime"]))
	if publishedAt != "" {
		result["published_at"] = publishedAt
		result["published_at_source"] = "msn_detail_api.publishedDateTime"
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fetchedResponse 抓取结果（等价 Python requests.Response 的被使用面）。
type fetchedResponse struct {
	statusCode  int
	finalURL    string
	contentType string
	text        string
	// location 响应 Location 头（重定向逐跳复核用）
	location string
	body     interface{ Close() error }
}

// stringValue 把 any 安全转字符串（对齐 Python str()）。
func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

// htmlUnescape 等价 Python html.unescape。
func htmlUnescape(value string) string {
	return html.UnescapeString(value)
}

// stripTagsTopic 去标签（对齐 web_fetch_tools.strip_tags）。
func stripTagsTopic(value string) string {
	return strings.TrimSpace(tagRE.ReplaceAllString(value, ""))
}

// normalizedTitleText 标题归一（对齐 :136-137 _normalized_title_text）。
func normalizedTitleText(value string) string {
	return strings.ToLower(strings.TrimSpace(htmlUnescape(whitespaceRE.ReplaceAllString(value, " "))))
}

// fetchVerifiedResponse 抓取一页，重定向只跟随公网目标。
// 首个 URL 在请求前校验，但 3xx 的目标也要逐跳复核，公网 URL 重定向进
// 链路本地 metadata 段必须拒绝（对齐 :178-206 _fetch_verified_response）。
func fetchVerifiedResponse(urlText string, timeoutSeconds int, publicHTTPS bool) (*fetchedResponse, error) {
	if !publicHTTPS {
		resp, err := topicHTTPGet(urlText, timeoutSeconds, true)
		return resp, err
	}
	current := urlText
	for i := 0; i <= defaultRedirectLimit; i++ {
		if reason := PublicFetchURLRejectionReasonSync(current); reason != "" {
			return nil, fmt.Errorf("unsafe fetch target: %s", reason)
		}
		resp, err := topicHTTPGet(current, timeoutSeconds, false)
		if err != nil {
			return nil, err
		}
		if resp.statusCode < 300 || resp.statusCode >= 400 || resp.location == "" {
			return resp, nil
		}
		base, _ := url.Parse(resp.finalURL)
		ref, _ := url.Parse(resp.location)
		if base != nil && ref != nil {
			current = base.ResolveReference(ref).String()
		} else {
			current = resp.location
		}
		_ = resp.body.Close()
	}
	return nil, fmt.Errorf("unsafe fetch target: exceeded redirect limit")
}

// topicHTTPGet 底层 GET（等价 web_fetch_tools.fetch_response 的消费面）。
func topicHTTPGet(urlText string, timeoutSeconds int, followRedirects bool) (*fetchedResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlText, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	contentType := resp.Header.Get("Content-Type")
	raw := make([]byte, 0, 1<<20)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if readErr != nil {
			break
		}
	}
	text := topicDecodeResponseText(raw, contentType)
	return &fetchedResponse{
		statusCode:  resp.StatusCode,
		finalURL:    resp.Request.URL.String(),
		contentType: contentType,
		text:        text,
		location:    resp.Header.Get("Location"),
		body:        resp.Body,
	}, nil
}

// topicDecodeResponseText 按 Content-Type charset 解码（等价
// web_fetch_tools.decode_response_text 的消费面）。
func topicDecodeResponseText(raw []byte, contentType string) string {
	enc := unicode.UTF8
	if params := strings.ToLower(contentType); strings.Contains(params, "charset=") {
		idx := strings.Index(params, "charset=")
		name := strings.TrimSpace(strings.SplitN(params[idx+len("charset="):], ";", 2)[0])
		if name != "" && name != "utf-8" && name != "utf8" {
			if e, _ := charset.Lookup(name); e != nil {
				enc = e
			}
		}
	} else if !utf8.Valid(raw) {
		// 无声明且非 UTF-8：让 charset.NewReader 做判定
		if r, err := charset.NewReader(strings.NewReader(string(raw)), contentType); err == nil {
			if decoded, err := decodeAll(r); err == nil {
				return decoded
			}
		}
	}
	decoded, err := enc.NewDecoder().Bytes(raw)
	if err != nil {
		return string(raw)
	}
	return string(decoded)
}

// decodeAll 读取整个 reader。
func decodeAll(r interface{ Read([]byte) (int, error) }) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String(), nil
}

// extractJinaReaderContent 抽取 Jina 正文并剔除重复标题（对齐 :139-156）。
func extractJinaReaderContent(text, title string) string {
	marker := jinaContentRE.FindStringIndex(text)
	content := text
	if marker != nil {
		content = text[marker[1]:]
	}
	normalizedTitle := normalizedTitleText(title)
	if normalizedTitle != "" {
		content = markdownATXHeadingRE.ReplaceAllStringFunc(content, func(m string) string {
			groups := markdownATXHeadingRE.FindStringSubmatch(m)
			if groups != nil && normalizedTitleText(groups[1]) == normalizedTitle {
				return ""
			}
			return m
		})
		content = markdownSetextHeadingRE.ReplaceAllStringFunc(content, func(m string) string {
			groups := markdownSetextHeadingRE.FindStringSubmatch(m)
			if groups != nil && normalizedTitleText(groups[1]) == normalizedTitle {
				return ""
			}
			return m
		})
	}
	return strings.TrimSpace(collapseBlankLines(content))
}

// collapseBlankLines 三连换行折叠为两连（对齐 re.sub(r"\n{3,}", "\n\n")）。
func collapseBlankLines(content string) string {
	return regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
}

// extractHTMLContent 抽取 HTML 正文（对齐 :158-186 _extract_html_content）。
// lxml → golang.org/x/net/html 等价转换：drop_tree → RemoveChild。
func extractHTMLContent(value, title string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	document, err := html.Parse(strings.NewReader(value))
	if err != nil {
		return stripTagsTopic(value)
	}
	// html.Parse 总能产出完整文档，不返回错误；对齐 lxml.ParserError 分支由
	// 上面 stripTags 兜底（Go 无解析器错误形态，登记等价转换）。
	dropNodes(document, map[string]bool{"script": true, "style": true, "noscript": true,
		"template": true, "title": true, "nav": true})
	normalizedTitle := normalizedTitleText(title)
	if normalizedTitle != "" {
		dropHeadings(document, normalizedTitle)
	}
	for _, selector := range []string{"main", "article", "body"} {
		nodes := findElementsByTagName(document, selector)
		var sb strings.Builder
		for _, node := range nodes {
			collectText(node, &sb)
		}
		content := strings.TrimSpace(whitespaceRE.ReplaceAllString(sb.String(), " "))
		if content != "" {
			return content
		}
	}
	var sb strings.Builder
	collectText(document, &sb)
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(sb.String(), " "))
}

// dropNodes 递归删除指定标签节点。
func dropNodes(node *html.Node, tags map[string]bool) {
	var children []*html.Node
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}
	for _, c := range children {
		if c.Type == html.ElementNode && tags[c.Data] {
			node.RemoveChild(c)
			continue
		}
		dropNodes(c, tags)
	}
}

// dropHeadings 删除文本归一后等于标题的 h1-h6 节点。
func dropHeadings(node *html.Node, normalizedTitle string) {
	var children []*html.Node
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		children = append(children, c)
	}
	for _, c := range children {
		if c.Type == html.ElementNode && len(c.Data) == 2 && c.Data[0] == 'h' && c.Data[1] >= '1' && c.Data[1] <= '6' {
			var sb strings.Builder
			collectText(c, &sb)
			if normalizedTitleText(sb.String()) == normalizedTitle {
				node.RemoveChild(c)
				continue
			}
		}
		dropHeadings(c, normalizedTitle)
	}
}

// findElementsByTagName 收集指定标签节点（等价 document.xpath("//x")）。
func findElementsByTagName(node *html.Node, tag string) []*html.Node {
	var found []*html.Node
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			found = append(found, c)
		}
		found = append(found, findElementsByTagName(c, tag)...)
	}
	return found
}

// collectText 深度收集文本（等价 itertext()）。
func collectText(node *html.Node, sb *strings.Builder) {
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		} else if c.Type == html.ElementNode {
			collectText(c, sb)
		}
	}
}
