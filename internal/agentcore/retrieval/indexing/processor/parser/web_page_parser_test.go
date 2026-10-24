package parser

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── verify 密封 sum type 测试（13.14） ────────────────────────────

// TestVerifyMode_默认单例 两次 NewVerifyDefault 返回同值。
func TestVerifyMode_默认单例(t *testing.T) {
	a, b := NewVerifyDefault(), NewVerifyDefault()
	if a != b {
		t.Error("NewVerifyDefault 应返回单例")
	}
}

// TestVerifyMode_四变体类型 构造器返回对应变体。
func TestVerifyMode_四变体类型(t *testing.T) {
	if _, ok := NewVerifyDefault().(verifyDefault); !ok {
		t.Error("NewVerifyDefault 应返回 verifyDefault")
	}
	if _, ok := NewVerifySkip().(verifySkip); !ok {
		t.Error("NewVerifySkip 应返回 verifySkip")
	}
	if _, ok := NewVerifyCAPath("/tmp/ca.pem").(verifyCAPath); !ok {
		t.Error("NewVerifyCAPath 应返回 verifyCAPath")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if _, ok := NewVerifyTLSConfig(cfg).(verifyTLSConfig); !ok {
		t.Error("NewVerifyTLSConfig 应返回 verifyTLSConfig")
	}
	vp := NewVerifyCAPath("/x")
	if p := vp.(verifyCAPath); p.path != "/x" {
		t.Errorf("path = %q, want /x", p.path)
	}
	vt := NewVerifyTLSConfig(cfg).(verifyTLSConfig)
	if vt.cfg != cfg {
		t.Error("cfg 应为同一指针")
	}
}

// TestVerifyMode_四变体均实现密封接口 包内四变体可赋值 verifyMode。
func TestVerifyMode_四变体均实现密封接口(t *testing.T) {
	var modes []verifyMode = []verifyMode{
		verifyDefault{}, verifySkip{}, verifyCAPath{}, verifyTLSConfig{},
	}
	for i, m := range modes {
		if m == nil {
			t.Errorf("变体 %d 不应为 nil", i)
		}
	}
}

// TestIsWeChatArticleURL_微信URL判定 正则模式逐分支。
func TestIsWeChatArticleURL_微信URL判定(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://mp.weixin.qq.com/s/abc", true},
		{"http://mp.weixin.qq.com/s/x?y=1", true},
		{"https://any.weixin.qq.com/s/abc", true},  // .*?.weixin.qq.com
		{"HTTPS://MP.WEIXIN.QQ.COM/S/ABC", true},   // IGNORECASE
		{"https://mp.weixin.qq.com/other", false},  // /s 后必须 \b（无 /s 前缀）
		{"https://example.com/s/abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isWeChatArticleURL(c.in); got != c.want {
			t.Errorf("isWeChatArticleURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestHTTPURLPattern_http匹配 http(s) 模式（含 IGNORECASE）。
func TestHTTPURLPattern_http匹配(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://a.com", true},
		{"https://a.com/x", true},
		{"HTTP://A.COM", true},
		{"ftp://a.com", false},
		{"a.com", false},
	}
	for _, c := range cases {
		if got := httpURLPattern.MatchString(c.in); got != c.want {
			t.Errorf("httpURLPattern(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// ──────────────────────────── WebPageParser 测试（13.14） ────────────────────────────

// TestWebPageParserSupports http(s) 且非微信。
func TestWebPageParserSupports(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	cases := []struct {
		in   string
		want bool
	}{
		{"http://a.com/x", true},
		{"https://a.com/x", true},
		{"HTTP://A.COM/X", true}, // IGNORECASE
		{"ftp://a.com/x", false},
		{"", false},
		{"https://mp.weixin.qq.com/s/abc", false},
		{"https://x.weixin.qq.com/s/abc", false},
	}
	for _, c := range cases {
		if got := p.Supports(c.in); got != c.want {
			t.Errorf("Supports(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestValidateURL_非http 155110 "Not a valid HTTP URL: %q"。
func TestValidateURL_非http(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	err := p.validateURLFor("notaurl")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110, got %v", err)
	}
	if be.Message() != `Not a valid HTTP URL: "notaurl"` {
		t.Errorf("message = %q", be.Message())
	}
}

// TestValidateURL_空串校验 空串同样 155110（对齐 not url 分支）。
func TestValidateURL_空串校验(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	err := p.validateURLFor("")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Message() != `Not a valid HTTP URL: ""` {
		t.Fatalf("message = %q", be.Message())
	}
}

// TestValidateURL_微信URL拒绝 155110 引导用 WeChatArticleParser。
func TestValidateURL_微信URL拒绝(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	err := p.validateURLFor("https://mp.weixin.qq.com/s/abc")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Message() != "Use WeChatArticleParser for WeChat URLs" {
		t.Fatalf("message = %q", be.Message())
	}
}

// TestWebPageParserParse_全链 httptest 服务端到端。
func TestWebPageParserParse_全链(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != defaultUserAgent {
			w.WriteHeader(400)
			return
		}
		fmt.Fprintf(w, `<html><head><title>网页标题</title></head><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	p, _ := NewWebPageParserWithDefaults()
	docs, err := p.Parse(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d", len(docs))
	}
	if docs[0].Metadata["title"] != "网页标题" {
		t.Errorf("title = %v", docs[0].Metadata["title"])
	}
	if docs[0].Metadata["source_type"] != "web_page" {
		t.Errorf("source_type = %v", docs[0].Metadata["source_type"])
	}
	if docs[0].Metadata["source_url"] != srv.URL {
		t.Errorf("source_url = %v", docs[0].Metadata["source_url"])
	}
	if docs[0].ID != srv.URL { // effectiveID：docID 空回落 url
		t.Errorf("ID = %q, want %q", docs[0].ID, srv.URL)
	}
}

// TestWebPageParserParse_错误状态码 404 → "Web page request failed: %d for %s"。
func TestWebPageParserParse_错误状态码(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	p, _ := NewWebPageParserWithDefaults()
	_, err := p.Parse(context.Background(), srv.URL)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110, got %v", err)
	}
	if be.Message() != "Web page request failed: 404 for "+srv.URL {
		t.Errorf("message = %q", be.Message())
	}
}

// TestWebPageParserParse_网络错误 server 关闭 → "Web page fetch failed for %s: %v"。
func TestWebPageParserParse_网络错误(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 立即关闭制造连接失败
	p, _ := NewWebPageParserWithDefaults()
	_, err := p.Parse(context.Background(), url)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110, got %v", err)
	}
	if !strings.HasPrefix(be.Message(), "Web page fetch failed for "+url+": ") {
		t.Errorf("message = %q", be.Message())
	}
}

// TestWebPageParserParse_注入client client 生效且 UA 仍注入（覆盖参数）。
func TestWebPageParserParse_注入client(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		fmt.Fprintf(w, `<html><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	p, _ := NewWebPageParserWithDefaults()
	_, err := p.Parse(context.Background(), srv.URL, WithHTTPClient(srv.Client()), WithUserAgent("custom-ua"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotUA != "custom-ua" {
		t.Errorf("UA = %q, want custom-ua", gotUA)
	}
}

// TestWebPageParserParse_docID优先 WithDocID 覆盖 effectiveID。
func TestWebPageParserParse_docID优先(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	p, _ := NewWebPageParserWithDefaults()
	docs, err := p.Parse(context.Background(), srv.URL, WithDocID("my-id"))
	if err != nil || len(docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(docs))
	}
	if docs[0].ID != "my-id" {
		t.Errorf("ID = %q, want my-id", docs[0].ID)
	}
}

// TestDownloadHTML_超时传递 自建 client Timeout 生效。
func TestDownloadHTML_超时传递(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		fmt.Fprint(w, `<html></html>`)
	}))
	defer srv.Close()
	_, err := downloadHTML(context.Background(), srv.URL, 50*time.Millisecond, defaultUserAgent, NewVerifyDefault(), nil)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110 超时, got %v", err)
	}
	if !strings.Contains(be.Message(), "fetch failed") {
		t.Errorf("message = %q", be.Message())
	}
}

// TestDownloadHTML_CAPath证书路径 verifyCAPath 读不存在的文件走兜底 155110。
func TestDownloadHTML_CAPath证书路径(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html></html>`)
	}))
	defer srv.Close()
	_, err := downloadHTML(context.Background(), srv.URL, time.Second, defaultUserAgent, NewVerifyCAPath("/nonexistent/ca.pem"), nil)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("CA 路径无效应 155110, got %v", err)
	}
}

// TestDownloadHTML_TLS跳过校验 verifySkip 对 TLS server 生效。
func TestDownloadHTML_TLS跳过校验(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	html, err := downloadHTML(context.Background(), srv.URL, time.Second, defaultUserAgent, NewVerifySkip(), nil)
	if err != nil {
		t.Fatalf("verifySkip 应成功, got %v", err)
	}
	if !strings.Contains(html, "article") {
		t.Errorf("html = %q", html[:50])
	}
}

// TestDownloadHTML_TLS自签证书默认校验失败 verifyDefault 对自签证书 155110
// （本地自签不在系统 CA——证明 verifyDefault 确实启用了校验）。
func TestDownloadHTML_TLS自签证书默认校验失败(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html></html>`)
	}))
	defer srv.Close()
	_, err := downloadHTML(context.Background(), srv.URL, time.Second, defaultUserAgent, NewVerifyDefault(), nil)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("verifyDefault 对自签证书应失败, got %v", err)
	}
}

// TestDownloadHTML_CAPath真证书 verifyCAPath 指向 httptest 自签证书可成功。
func TestDownloadHTML_CAPath真证书(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>x</body></html>`)
	}))
	defer srv.Close()
	// httptest.NewTLSServer 证书落盘临时文件供 CA bundle 消费
	certFile := filepath.Join(t.TempDir(), "cert.pem")
	if err := os.WriteFile(certFile, pemEncode(srv.Certificate()), 0o644); err != nil {
		t.Fatal(err)
	}
	html, err := downloadHTML(context.Background(), srv.URL, time.Second, defaultUserAgent, NewVerifyCAPath(certFile), nil)
	if err != nil {
		t.Fatalf("verifyCAPath 注入 httptest 证书应成功, got %v", err)
	}
	if !strings.Contains(html, "x") {
		t.Errorf("html = %q", html)
	}
}

// pemEncode 证书编码为 PEM（测试辅助；x509.Certificate.Raw 是 DER 字节）。
func pemEncode(cert *x509.Certificate) []byte {
	var out []byte
	out = append(out, []byte("-----BEGIN CERTIFICATE-----\n")...)
	out = append(out, base64.StdEncoding.EncodeToString(cert.Raw)...)
	out = append(out, []byte("\n-----END CERTIFICATE-----\n")...)
	return out
}

// TestDownloadHTML_注入client直用 注入的 client 直接复用（自带 Transport TLS 配置）。
func TestDownloadHTML_注入client直用(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>injected</body></html>`)
	}))
	defer srv.Close()
	// srv.Client() 自带自签信任；注入时 verify/timeout 参数失效
	html, err := downloadHTML(context.Background(), srv.URL, time.Second, "ua", NewVerifyDefault(), srv.Client())
	if err != nil {
		t.Fatalf("注入 client 应直用, got %v", err)
	}
	if !strings.Contains(html, "injected") {
		t.Errorf("html = %q", html)
	}
}

// TestParseWebPageURL_别名函数 包级别名走默认参数全链。
func TestParseWebPageURL_别名函数(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	docs, err := ParseWebPageURL(context.Background(), srv.URL, "")
	if err != nil || len(docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(docs))
	}
	if docs[0].Metadata["source_url"] != srv.URL {
		t.Errorf("source_url = %v", docs[0].Metadata["source_url"])
	}
}
