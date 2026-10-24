// Package parser web_page_parser.go：通用网页 URL 解析器（13.14）。
//
// 本文件对齐 Python web_page_parser.py：verify 密封 sum type、URL 正则、
// 默认常量与 WebPageParser 本体（Task 4 追加解析器结构体）。
package parser

import (
	"crypto/tls"
	"regexp"
	"strings"
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
