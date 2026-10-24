// Package parser web_page_parser.go：通用网页 URL 解析器（13.14）。
//
// 本文件对齐 Python web_page_parser.py：verify 密封 sum type、URL 正则、
// 默认常量与 WebPageParser 本体（dispatch 动态派发 + downloadHTML + ParseURL）。
package parser

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// verifyMode TLS 校验模式密封接口（对齐 Python verify: bool | str | ssl.SSLContext）。
//
// 三态单参数在 Python 是一个值的四种形态：True 默认 CA / False 跳过 /
// str CA bundle 路径 / ssl.SSLContext 自定义。Go 侧密封 sum type 四变体
// 承载（非导出标记方法 isVerifyMode()，包外无法新增变体——13.13 cellValue 先例）。
type verifyMode interface {
	// isVerifyMode 密封标记（非导出，包外不可实现）
	isVerifyMode()
}

// verifyDefault Python verify=True：系统默认 CA。
type verifyDefault struct{}

// isVerifyMode 密封标记。
func (verifyDefault) isVerifyMode() {}

// verifySkip Python verify=False：跳过校验（企业 TLS 检查场景）。
type verifySkip struct{}

// isVerifyMode 密封标记。
func (verifySkip) isVerifyMode() {}

// verifyCAPath Python verify=str：CA bundle 文件路径。
type verifyCAPath struct {
	// path CA bundle 文件路径
	path string
}

// isVerifyMode 密封标记。
func (verifyCAPath) isVerifyMode() {}

// verifyTLSConfig Python verify=ssl.SSLContext：自定义 TLS 配置。
type verifyTLSConfig struct {
	// cfg 自定义 TLS 配置
	cfg *tls.Config
}

// isVerifyMode 密封标记。
func (verifyTLSConfig) isVerifyMode() {}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultUserAgent 默认 UA（对齐 Python DEFAULT_USER_AGENT 字面复制）。
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	// defaultTimeoutSeconds 默认超时秒数（对齐 Python DEFAULT_TIMEOUT = 30.0）。
	defaultTimeoutSeconds = 30.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// httpURLPattern http(s) URL 模式（对齐 HTTP_URL_PATTERN = re.compile(r"^https?://\S+", re.IGNORECASE)；
	// Go 原生大小写敏感，(?i) 前缀对齐 IGNORECASE）。
	httpURLPattern = regexp.MustCompile(`(?i)^https?://\S+`)
	// wechatMPURLPattern 微信公众号文章 URL 模式（对齐 WECHAT_MP_URL_PATTERN 字面）。
	// 偏差：Go RE2 无惰性量词，`.*?` 按 `.*` 实现等价（尾部无后续锚点）。
	wechatMPURLPattern = regexp.MustCompile(`(?i)^https?://(?:mp\.weixin\.qq\.com|.*?\.weixin\.qq\.com)/s\b.*`)
	// verifyDefaultInstance 默认变体单例（对齐 Python True 字面量语义）。
	verifyDefaultInstance = verifyDefault{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerifyDefault 构造默认校验变体（单例，对齐 Python verify=True）。
func NewVerifyDefault() verifyMode { return verifyDefaultInstance }

// NewVerifySkip 构造跳过校验变体（对齐 Python verify=False）。
func NewVerifySkip() verifyMode { return verifySkip{} }

// NewVerifyCAPath 构造 CA bundle 路径变体（对齐 Python verify=str 路径）。
func NewVerifyCAPath(path string) verifyMode { return verifyCAPath{path: path} }

// NewVerifyTLSConfig 构造自定义 TLS 配置变体（对齐 Python verify=ssl.SSLContext）。
func NewVerifyTLSConfig(cfg *tls.Config) verifyMode { return verifyTLSConfig{cfg: cfg} }

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWeChatArticleURL 判定微信文章 URL（对齐 _is_wechat_article_url）。
//
// 空串/空 url 直接 false（对齐 bool(url and ...)）。
// Python: web_page_parser.py _is_wechat_article_url
func isWeChatArticleURL(url string) bool {
	return url != "" && wechatMPURLPattern.MatchString(strings.TrimSpace(url))
}

// ──────────────────────────── WebPageParser 本体（Task 4） ────────────────────────────

// htmlParseDispatcher _parse_html/_validate_url 覆写动态派发接口。
//
// Python classmethod 覆写（WeChatArticleParser._parse_html/_validate_url）
// 的 Go 对应物：WebPageParser 构造时注入最终实例，ParseURL 经此派发——
// Go 嵌入无运行时多态（*WebPageParser 上的方法调用静态绑定），覆写
// 语义靠 dispatch 注入实现。仅本包实现（接口非导出）。
type htmlParseDispatcher interface {
	// parseHTMLToDocs 对齐 Python _parse_html classmethod
	parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error)
	// validateURLFor 对齐 Python _validate_url 静态方法
	validateURLFor(url string) error
}

// WebPageParser 通用网页 URL 解析器（对齐 Python WebPageParser(HTMLFileParser)）。
//
// 嵌入 HTMLFileParser 复用 parseHTMLToDocs/共享工具；dispatch 注入最终
// 实现实现 classmethod 覆写派发。构造四参数对齐 Python __init__
// timeout/user_agent/verify（client 仅 parse 时 kwargs 注入）。
// Python: web_page_parser.py WebPageParser
type WebPageParser struct {
	// HTMLFileParser 父级（HTML 解析工具链）
	*HTMLFileParser
	// timeout 请求超时（实例默认，可被 ParseOptions.Timeout 覆盖）
	timeout time.Duration
	// userAgent UA 头（实例默认，可被 ParseOptions.UserAgent 覆盖）
	userAgent string
	// verify TLS 校验模式（实例默认，可被 ParseOptions.Verify 覆盖）
	verify verifyMode
	// dispatch 动态派发钩子（构造注入最终实例）
	dispatch htmlParseDispatcher
}

// NewWebPageParserWithDefaults 默认参数构造（对齐 Python 默认
// timeout=30.0/user_agent=DEFAULT_USER_AGENT/verify=True）。
func NewWebPageParserWithDefaults() (*WebPageParser, error) {
	return NewWebPageParser(defaultTimeoutSeconds, defaultUserAgent, NewVerifyDefault())
}

// NewWebPageParser 构造 WebPageParser。
//
// timeout 收 float64 秒（字面贴近 Python timeout: float = DEFAULT_TIMEOUT），
// 内部转 time.Duration。verify nil 防御回落默认（NewBaseParser 先例风格）。
// Python: web_page_parser.py WebPageParser.__init__
func NewWebPageParser(timeout float64, userAgent string, verify verifyMode) (*WebPageParser, error) {
	hfp, err := NewHTMLFileParser()
	if err != nil {
		return nil, err
	}
	if verify == nil {
		verify = NewVerifyDefault()
	}
	p := &WebPageParser{
		HTMLFileParser: hfp,
		timeout:        time.Duration(timeout * float64(time.Second)),
		userAgent:      userAgent,
		verify:         verify,
	}
	p.dispatch = p // 默认派发自身；子类构造时覆写注入
	// self 链指向最终实例：BaseParser 模板方法派发到 WebPageParser 覆写版本
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.HTMLFileParser.TxtMdParser.BaseParser = bp
	return p, nil
}

// Parse 解析网页 URL（覆写整体，对齐 Python WebPageParser.parse）。
//
// 四覆盖参数从 opts 取值（未设回落实例字段）后转发 ParseURL。
// 对齐 Python kwargs 四键：timeout/user_agent/verify/client。
// Python: web_page_parser.py WebPageParser.parse
func (p *WebPageParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)
	timeout := o.Timeout
	if timeout == 0 {
		timeout = p.timeout
	}
	userAgent := o.UserAgent
	if userAgent == "" {
		userAgent = p.userAgent
	}
	verify := o.Verify
	if verify == nil {
		verify = p.verify
	}
	client := o.HTTPClient
	docID := o.DocID
	if docID == "" {
		docID = doc
	}
	return p.ParseURL(ctx, doc, docID, timeout, userAgent, verify, client)
}

// ParseURL 抓取并解析网页 URL（对齐 Python classmethod parse_url）。
//
// validateURL（经 dispatch，子类覆写生效）→ downloadHTML → effectiveID
// 三级回落（docID or url or uuid）→ parseHTMLToDocs（经 dispatch）→
// 每个 doc 补 source_url + Info 日志。
// Python: web_page_parser.py WebPageParser.parse_url
func (p *WebPageParser) ParseURL(ctx context.Context, url string, docID string, timeout time.Duration, userAgent string, verify verifyMode, client *http.Client) ([]reranker.Document, error) {
	if err := p.dispatch.validateURLFor(url); err != nil {
		return nil, err
	}
	html, err := downloadHTML(ctx, url, timeout, userAgent, verify, client)
	if err != nil {
		return nil, err
	}
	// 对齐 Python: effective_id = doc_id or url or str(uuid.uuid4())
	effectiveID := docID
	if effectiveID == "" {
		effectiveID = url
	}
	if effectiveID == "" {
		effectiveID = uuid.NewString()
	}
	docs, err := p.dispatch.parseHTMLToDocs(html, effectiveID, url)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		if docs[i].Metadata == nil {
			docs[i].Metadata = map[string]any{}
		}
		docs[i].Metadata["source_url"] = url
		// 对齐 Python: logger.info("Parsed web page: url=%s title=%s", url, title or "(无标题)")
		title, _ := docs[i].Metadata["title"].(string)
		if title == "" {
			title = "(无标题)"
		}
		logger.Info(logComponent).
			Str("url", url).
			Str("title", title).
			Msg("parsed web page")
	}
	return docs, nil
}

// validateURLFor 校验 URL（WebPage 默认实现，对齐 Python _validate_url 静态方法）。
//
// 非 http(s) → 155110 "Not a valid HTTP URL: %q"（%q 对齐 Python repr）；
// 微信 URL → 155110 引导专用解析器。
// Python: web_page_parser.py WebPageParser._validate_url
func (p *WebPageParser) validateURLFor(url string) error {
	url = strings.TrimSpace(url)
	if url == "" || !httpURLPattern.MatchString(url) {
		return exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Not a valid HTTP URL: %q", url)),
		)
	}
	if isWeChatArticleURL(url) {
		return exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Use WeChatArticleParser for WeChat URLs"),
		)
	}
	return nil
}

// Supports 检查是否支持该文档源（覆写，对齐 Python WebPageParser.supports）。
//
// http(s) URL 且非微信文章 URL（strip 后匹配，对齐 doc.strip()）。
// Python: web_page_parser.py WebPageParser.supports
func (p *WebPageParser) Supports(doc string) bool {
	if doc == "" || !httpURLPattern.MatchString(strings.TrimSpace(doc)) {
		return false
	}
	return !isWeChatArticleURL(doc)
}

// downloadHTML 下载网页 HTML（对齐 Python _download_html）。
//
// 注入 client 时直接使用（UA header 仍注入——对齐 Python 注入分支
// headers_for_request=request_headers）；自建时按 verify 变体配
// transport + client.Timeout（对齐 httpx.AsyncClient(verify=, timeout=,
// headers=)）。三分支错误对齐 Python 三层 except：网络错→"Web page
// fetch failed for %s: %v"；非 2xx（raise_for_status）→"Web page
// request failed: %d for %s"；其余异常→兜底同网络错格式。
// Python: web_page_parser.py WebPageParser._download_html
func downloadHTML(ctx context.Context, url string, timeout time.Duration, userAgent string, verify verifyMode, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Web page fetch failed for %s: %v", url, err)),
			exception.WithCause(err),
		)
	}
	req.Header.Set("User-Agent", userAgent)
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		switch v := verify.(type) {
		case verifySkip:
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		case verifyCAPath:
			pem, err := os.ReadFile(v.path)
			if err != nil {
				return "", exception.BuildError(
					exception.StatusRetrievalIndexingFetchError,
					exception.WithMsg(fmt.Sprintf("Web page fetch failed for %s: %v", url, err)),
					exception.WithCause(err),
				)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				err := fmt.Errorf("failed to append CA certs from %s", v.path)
				return "", exception.BuildError(
					exception.StatusRetrievalIndexingFetchError,
					exception.WithMsg(fmt.Sprintf("Web page fetch failed for %s: %v", url, err)),
					exception.WithCause(err),
				)
			}
			transport.TLSClientConfig = &tls.Config{RootCAs: pool}
		case verifyTLSConfig:
			transport.TLSClientConfig = v.cfg
		default: // verifyDefault / nil：零配置走系统 CA（对齐 verify=True）
		}
		client = &http.Client{
			Transport: transport,
			Timeout:   timeout,
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Web page fetch failed for %s: %v", url, err)),
			exception.WithCause(err),
		)
	}
	defer func() {
		// 关闭失败不影响已读取结果，仅记录（errcheck 合规）
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.Warn(logComponent).
				Str("url", url).
				Err(closeErr).
				Msg("failed to close response body")
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Web page request failed: %d for %s", resp.StatusCode, url)),
		)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Web page fetch failed for %s: %v", url, err)),
			exception.WithCause(err),
		)
	}
	return string(body), nil
}

// ParseWebPageURL 包级别名（对齐 parse_web_page_url = WebPageParser.parse_url）。
//
// 默认参数全链；覆盖参数经 ParseOption 传入（kwargs 对应物）。
// Python: web_page_parser.py 模块尾别名赋值
func ParseWebPageURL(ctx context.Context, url string, docID string, opts ...ParseOption) ([]reranker.Document, error) {
	p, err := NewWebPageParserWithDefaults()
	if err != nil {
		return nil, err
	}
	o := NewParseOptions(opts...)
	timeout := o.Timeout
	if timeout == 0 {
		timeout = p.timeout
	}
	userAgent := o.UserAgent
	if userAgent == "" {
		userAgent = p.userAgent
	}
	verify := o.Verify
	if verify == nil {
		verify = p.verify
	}
	return p.ParseURL(ctx, url, docID, timeout, userAgent, verify, o.HTTPClient)
}
