package parser

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
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
		{"https://any.weixin.qq.com/s/abc", true}, // .*?.weixin.qq.com
		{"HTTPS://MP.WEIXIN.QQ.COM/S/ABC", true},  // IGNORECASE
		{"https://mp.weixin.qq.com/other", false}, // /s 后必须 \b（无 /s 前缀）
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

// TestDownloadHTML_TLSConfig自定义 verifyTLSConfig 直用注入的 tls.Config。
func TestDownloadHTML_TLSConfig自定义(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><body>tls-config</body></html>`)
	}))
	defer srv.Close()
	// InsecureSkipVerify 自定义配置承载 verifySkip 等价语义
	html, err := downloadHTML(context.Background(), srv.URL, time.Second, defaultUserAgent,
		NewVerifyTLSConfig(&tls.Config{InsecureSkipVerify: true}), nil)
	if err != nil {
		t.Fatalf("verifyTLSConfig 应成功, got %v", err)
	}
	if !strings.Contains(html, "tls-config") {
		t.Errorf("html = %q", html)
	}
}

// TestNewWebPageParser_自定义参数 构造器参数落字段 + verify nil 防御回落默认。
func TestNewWebPageParser_自定义参数(t *testing.T) {
	p, err := NewWebPageParser(1.5, "custom-ua", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.timeout != 1500*time.Millisecond {
		t.Errorf("timeout = %v, want 1.5s", p.timeout)
	}
	if p.userAgent != "custom-ua" {
		t.Errorf("userAgent = %q", p.userAgent)
	}
	if _, ok := p.verify.(verifyDefault); !ok {
		t.Errorf("verify nil 应回落 verifyDefault, got %T", p.verify)
	}
}

// TestParseURL_uuid兜底 docID 与 url 双空走 uuid 分支。
func TestParseURL_uuid兜底(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	// validateURLFor 先拦空串——绕过校验直测 effectiveID 兜底需走 dispatch
	// 覆写路径不可达；改为验证 url 非空时 effectiveID=url 的常规路径已覆盖，
	// uuid 分支字面保留（Python doc_id or url or uuid4 三级字面）。
	// 此处断言：空 docID + 非空 url → effectiveID = url（第二级）。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	docs, err := p.ParseURL(context.Background(), srv.URL, "", p.timeout, p.userAgent, p.verify, nil)
	if err != nil || len(docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(docs))
	}
	if docs[0].ID != srv.URL {
		t.Errorf("ID = %q, want %q", docs[0].ID, srv.URL)
	}
}

// TestParseWebPageURL_覆盖参数传别名 别名函数透传 WithTimeout/WithUserAgent。
func TestParseWebPageURL_覆盖参数传别名(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		fmt.Fprintf(w, `<html><body><article>%s</article></body></html>`, longBody(12))
	}))
	defer srv.Close()
	_, err := ParseWebPageURL(context.Background(), srv.URL, "", WithUserAgent("alias-ua"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotUA != "alias-ua" {
		t.Errorf("UA = %q, want alias-ua", gotUA)
	}
}

// TestVerifyMode_密封方法被调用 变体方法体空语句计数（覆盖密封标记方法）。
func TestVerifyMode_密封方法被调用(t *testing.T) {
	var m verifyMode = verifyDefault{}
	m.isVerifyMode()
	m = verifySkip{}
	m.isVerifyMode()
	m = verifyCAPath{}
	m.isVerifyMode()
	m = verifyTLSConfig{}
	m.isVerifyMode()
}

// ──────────────────────────── dispatch stub 测试（13.14 覆盖率补齐） ────────────────────────────

// stubParseDispatcher dispatch 桩（替换 WebPageParser.dispatch 测内部分支）。
type stubParseDispatcher struct {
	// docs parseHTMLToDocs 返回值
	docs []reranker.Document
	// parseErr parseHTMLToDocs 返回错误
	parseErr error
	// validateErr validateURLFor 返回错误
	validateErr error
	// gotHTML parseHTMLToDocs 收到的 html
	gotHTML string
	// gotDocID parseHTMLToDocs 收到的 docID（验证 effectiveID 三级兜底）
	gotDocID string
}

// parseHTMLToDocs 桩实现：记录入参并返回预设值。
func (s *stubParseDispatcher) parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error) {
	s.gotHTML = html
	s.gotDocID = docID
	return s.docs, s.parseErr
}

// validateURLFor 桩实现：返回预设错误。
func (s *stubParseDispatcher) validateURLFor(url string) error {
	return s.validateErr
}

// TestParseURL_dispatch错误传播 桩 parseHTMLToDocs 返回错误时 ParseURL 原样透传。
func TestParseURL_dispatch错误传播(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	want := errors.New("parse failed")
	p.dispatch = &stubParseDispatcher{parseErr: want}
	_, err := p.ParseURL(context.Background(), "http://example.com", "id", p.timeout, p.userAgent, p.verify, nil)
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// okBodyTransport 任意请求均返回 200 与固定 body（供空 URL 注入 client 场景）。
type okBodyTransport struct{}

// RoundTrip 返回 200 响应与固定 body。
func (okBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("<html><body>ok</body></html>")),
		Header:     http.Header{},
	}, nil
}

// TestParseURL_dispatch返回nil元数据 建无 metadata 的 Document 补 source_url + effectiveID uuid 兜底。
func TestParseURL_dispatch返回nil元数据(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	// uuid 兜底分支：docID 与 url 双空（dispatch 桩绕开 validateURLFor 拦截；
	// 注入 okBodyTransport 假 client 使 downloadHTML 对空 URL 也返回成功）
	stub := &stubParseDispatcher{docs: []reranker.Document{{ID: "", Text: "x"}}}
	p.dispatch = stub
	client := &http.Client{Transport: okBodyTransport{}}
	docs, err := p.ParseURL(context.Background(), "", "", p.timeout, p.userAgent, p.verify, client)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d", len(docs))
	}
	if docs[0].Metadata["source_url"] != "" {
		t.Errorf("source_url = %q, want \"\"", docs[0].Metadata["source_url"])
	}
	// effectiveID 三级兜底第三级：docID 与 url 双空 → 传给 dispatch 的是 uuid
	if stub.gotDocID == "" {
		t.Errorf("gotDocID 应为 uuid 兜底非空")
	}
	// dispatch 桩收到了假 client 返回的 body
	if stub.gotHTML != "<html><body>ok</body></html>" {
		t.Errorf("dispatch 未收到预期 html, got %q", stub.gotHTML)
	}
}

// errBodyTransport 读 body 阶段返回错误（覆盖 io.ReadAll 失败分支）。
type errBodyTransport struct{}

// RoundTrip 返回 200 响应但 body 读取即错。
func (errBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(errReader{}),
		Header:     http.Header{},
	}, nil
}

// errReader 永远返回错误的读取器。
type errReader struct{}

// Read 永远返回错误。
func (errReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

// TestDownloadHTML_读body失败 响应 200 但 body 读取失败 → 网络错格式。
func TestDownloadHTML_读body失败(t *testing.T) {
	client := &http.Client{Transport: errBodyTransport{}}
	_, err := downloadHTML(context.Background(), "http://example.com", time.Second, "ua", NewVerifyDefault(), client)
	if err == nil {
		t.Fatal("读 body 失败应返回错误")
	}
	if !strings.Contains(err.Error(), "Web page fetch failed for") {
		t.Errorf("err = %v, want 网络错格式", err)
	}
}

// TestDownloadHTML_非法URL NewRequest 阶段即错（控制字符 URL）。
func TestDownloadHTML_非法URL(t *testing.T) {
	_, err := downloadHTML(context.Background(), "http://exa mple.com/\x7f", time.Second, "ua", NewVerifyDefault(), nil)
	if err == nil {
		t.Fatal("非法 URL 应返回错误")
	}
	if !strings.Contains(err.Error(), "Web page fetch failed for") {
		t.Errorf("err = %v, want 网络错格式", err)
	}
}

// TestDownloadHTML_CAPath无效PEM CA 文件非 PEM 内容 → append 失败分支。
func TestDownloadHTML_CAPath无效PEM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, []byte("not a pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := downloadHTML(context.Background(), "https://example.com", time.Second, "ua", NewVerifyCAPath(path), nil)
	if err == nil {
		t.Fatal("无效 PEM 应返回错误")
	}
	if !strings.Contains(err.Error(), "failed to append CA certs") {
		t.Errorf("err = %v, want append 失败", err)
	}
}
