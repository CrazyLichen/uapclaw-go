package web_tools

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// requestConfig HTTP 请求配置
type requestConfig struct {
	// headers 请求头
	headers map[string]string
	// timeoutSeconds 超时秒数
	timeoutSeconds int
	// sslVerify 是否验证 SSL
	sslVerify bool
	// body 请求体
	body string
}

// RequestOption 请求选项函数
type RequestOption func(*requestConfig)

// httpResponse HTTP 响应封装
type httpResponse struct {
	// statusCode HTTP 状态码
	statusCode int
	// status HTTP 状态描述
	status string
	// headers 响应头
	headers http.Header
	// body 响应体字节
	body []byte
	// text 响应体文本
	text string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// httpRequest 统一 HTTP 请求，支持代理、重试、SSL 跳过验证
// 对齐 Python: _http_request() (web_tools.py L181-200)
func httpRequest(method, reqURL string, opts ...RequestOption) (*httpResponse, error) {
	cfg := defaultRequestConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if !cfg.sslVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	// 对齐 Python: _apply_free_search_proxy() (web_tools.py L172-178)
	proxyURL := getFreeSearchProxyURL()
	explicitProxy := false
	if proxyURL != "" && !shouldBypassFreeSearchProxy(reqURL) {
		if proxyParsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyParsed)
			explicitProxy = true
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(cfg.timeoutSeconds) * time.Second,
	}

	var bodyReader io.Reader
	if cfg.body != "" {
		bodyReader = strings.NewReader(cfg.body)
	}

	req, err := http.NewRequestWithContext(context.Background(), strings.ToUpper(method), reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	for k, v := range cfg.headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := client.Do(req)
	if err != nil {
		// 对齐 Python: L195-200 — 当代理出错且未显式指定代理时（即使用环境变量代理），
		// 回退到直连重试；如果用户显式配置了代理则不回退，直接报错
		if isProxyError(err) && !explicitProxy {
			transport2 := http.DefaultTransport.(*http.Transport).Clone()
			transport2.Proxy = nil
			if !cfg.sslVerify {
				transport2.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			}
			client2 := &http.Client{
				Transport: transport2,
				Timeout:   time.Duration(cfg.timeoutSeconds) * time.Second,
			}
			req2, err2 := http.NewRequestWithContext(context.Background(), strings.ToUpper(method), reqURL, bodyReader)
			if err2 != nil {
				return nil, fmt.Errorf("创建重试请求失败: %w", err2)
			}
			for k, v := range cfg.headers {
				req2.Header.Set(k, v)
			}
			if req2.Header.Get("User-Agent") == "" {
				req2.Header.Set("User-Agent", userAgent)
			}
			resp, err = client2.Do(req2)
			if err != nil {
				return nil, fmt.Errorf("请求失败（重试后）: %w", err)
			}
		} else {
			return nil, fmt.Errorf("请求失败: %w", err)
		}
	}

	return newHTTPResponse(resp), nil
}

// shouldBypassFreeSearchProxy 判断是否绕过代理
// 对齐 Python: _should_bypass_free_search_proxy() (web_tools.py L152-169)
func shouldBypassFreeSearchProxy(reqURL string) bool {
	proxyURL := getFreeSearchProxyURL()
	if proxyURL == "" {
		return true
	}
	parsed, err := url.Parse(reqURL)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return false
	}
	for _, entry := range noProxyEntries() {
		if entry == "*" {
			return true
		}
		if strings.HasPrefix(entry, ".") {
			if hostname == entry[1:] || strings.HasSuffix(hostname, entry) {
				return true
			}
		}
		if hostname == entry || strings.HasSuffix(hostname, "."+entry) {
			return true
		}
	}
	return false
}

// raiseForStatusWithBody HTTP 错误时包含响应体
// 对齐 Python: _raise_for_status_with_body() (web_tools.py L203-216)
func raiseForStatusWithBody(resp *httpResponse) error {
	if resp.statusCode >= 200 && resp.statusCode < 300 {
		return nil
	}
	body := strings.TrimSpace(resp.text)
	if len(body) > 1000 {
		body = body[:1000]
	}
	if body != "" {
		return fmt.Errorf("HTTP %d: %s; response body: %s", resp.statusCode, resp.status, body)
	}
	return fmt.Errorf("HTTP %d: %s", resp.statusCode, resp.status)
}

// defaultRequestConfig 构造默认请求配置
func defaultRequestConfig() requestConfig {
	return requestConfig{
		headers:        make(map[string]string),
		timeoutSeconds: 30,
		sslVerify:      freeSearchSSLVerify(),
	}
}

// withHeaders 设置请求头
func withHeaders(headers map[string]string) RequestOption {
	return func(c *requestConfig) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

// withTimeout 设置超时
func withTimeout(seconds int) RequestOption {
	return func(c *requestConfig) { c.timeoutSeconds = seconds }
}

// withBody 设置请求体
func withBody(body string) RequestOption {
	return func(c *requestConfig) { c.body = body }
}

// newHTTPResponse 从 http.Response 构造 httpResponse
func newHTTPResponse(resp *http.Response) *httpResponse {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// 读取失败时记录日志，返回空 body 和原始状态码
		return &httpResponse{
			statusCode: resp.StatusCode,
			status:     resp.Status,
			headers:    resp.Header,
			body:       nil,
			text:       "",
		}
	}
	return &httpResponse{
		statusCode: resp.StatusCode,
		status:     resp.Status,
		headers:    resp.Header,
		body:       body,
		text:       string(body),
	}
}

// isProxyError 判断是否为代理错误
func isProxyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "proxy") || strings.Contains(msg, "tunnel")
}
