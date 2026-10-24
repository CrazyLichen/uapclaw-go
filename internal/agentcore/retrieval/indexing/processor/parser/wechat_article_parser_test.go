package parser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── WeChatArticleParser 测试（13.14） ────────────────────────────

// wechatHTML 生成微信文章页面。
func wechatHTML(body string) string {
	return `<html><head><title>公众号文章</title></head><body><div id="js_content">` + body + `</div></body></html>`
}

// TestWeChatParserParse_直调parseHTMLToDocs js_content 命中（无 50 字符门槛——短正文成功）。
func TestWeChatParserParse_直调parseHTMLToDocs(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	docs, err := p.parseHTMLToDocs(wechatHTML(`<p>短正文</p>`), "id1", "src")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d", len(docs))
	}
	if docs[0].Metadata["source_type"] != "wechat_article" {
		t.Errorf("source_type = %v", docs[0].Metadata["source_type"])
	}
	if docs[0].Metadata["title"] != "公众号文章" {
		t.Errorf("title = %v", docs[0].Metadata["title"])
	}
	if docs[0].Text != "短正文" {
		t.Errorf("text = %q", docs[0].Text)
	}
	if docs[0].ID != "id1" {
		t.Errorf("ID = %q, want id1", docs[0].ID)
	}
}

// TestWeChatParserParse_无50字符门槛对照 与 WebPage 的 <50 门槛对照：
// 同长度短正文 WeChat 成功（js_content 无长度门槛只判空）。
func TestWeChatParserParse_无50字符门槛对照(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	docs, err := p.parseHTMLToDocs(wechatHTML(`<p>二十字符以内短文</p>`), "id", "src")
	if err != nil || len(docs) != 1 {
		t.Fatalf("短正文应成功（无 50 门槛）: err=%v", err)
	}
}

// TestWeChatParserParse_jsContent缺失 155110。
func TestWeChatParserParse_jsContent缺失(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	_, err := p.parseHTMLToDocs(`<html><body><div>无微信结构</div></body></html>`, "id", "src")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110, got %v", err)
	}
	if be.Message() != "Could not find article content (js_content) in page (source: src)" {
		t.Errorf("message = %q", be.Message())
	}
}

// TestWeChatParserParse_正文空 155110。
func TestWeChatParserParse_正文空(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	_, err := p.parseHTMLToDocs(wechatHTML(""), "id", "src")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Message() != "Article content is empty after parsing (source: src)" {
		t.Fatalf("message = %q", be.Message())
	}
}

// TestWeChatParserParse_标题兜底无标题 title 缺失兜底 "(无标题)"。
func TestWeChatParserParse_标题兜底无标题(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	docs, err := p.parseHTMLToDocs(`<html><body><div id="js_content">正文</div></body></html>`, "id", "src")
	if err != nil || len(docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(docs))
	}
	if docs[0].Metadata["title"] != "(无标题)" {
		t.Errorf("title = %v, want (无标题)", docs[0].Metadata["title"])
	}
}

// TestWeChatParserValidateURL_非微信 155110。
func TestWeChatParserValidateURL_非微信(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	err := p.validateURLFor("https://example.com/x")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Message() != `Not a WeChat article URL: "https://example.com/x"` {
		t.Fatalf("message = %q", be.Message())
	}
}

// TestWeChatParserValidateURL_微信通过 微信 URL 校验通过。
func TestWeChatParserValidateURL_微信通过(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	if err := p.validateURLFor("https://mp.weixin.qq.com/s/abc"); err != nil {
		t.Errorf("err = %v", err)
	}
}

// TestWeChatParserSupports 微信 URL 才 true。
func TestWeChatParserSupports(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	if !p.Supports("https://mp.weixin.qq.com/s/abc") {
		t.Error("微信 URL 应 true")
	}
	if p.Supports("https://example.com/x") {
		t.Error("普通 URL 应 false")
	}
	if p.Supports("") {
		t.Error("空串应 false")
	}
}

// TestWeChatParserParse_继承链dispatch 覆写经 dispatch 生效：
// Parse（继承 WebPage）→ ParseURL → dispatch 到 WeChat 版 validateURLFor。
// 本地 httptest URL 非 mp.weixin 域名，WeChat 覆写版校验拦截普通 URL——
// 错误文案来自 WeChat 覆写版即 dispatch 生效的证明（spec §6.2 记录的约束）。
func TestWeChatParserParse_继承链dispatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, wechatHTML(`<p>`+strings.Repeat("公众号正文。", 10)+`</p>`))
	}))
	defer srv.Close()
	p, _ := NewWeChatArticleParser()
	_, err := p.Parse(context.Background(), srv.URL)
	var be *exception.BaseError
	if !errors.As(err, &be) {
		t.Fatalf("err = %v", err)
	}
	// dispatch 生效证明：WeChat 覆写的 validateURLFor 拦截了普通 URL
	if be.Message() != `Not a WeChat article URL: "`+srv.URL+`"` {
		t.Errorf("message = %q（应为 WeChat 覆写版校验文案）", be.Message())
	}
}

// TestWeChatParserParse_继承链dispatch解析覆写 parseHTMLToDocs 覆写经
// dispatch 生效：本地 URL + 注入自定义 dispatch stub 验证 ParseURL 调的是
// WeChat 版解析（js_content 路径）而非 HTMLFile 版（findMainContent 路径）。
//
// 构造页面同时含 article（HTMLFile 路径会命中）与 js_content（WeChat 路径
// 命中）：直调 WeChat.parseHTMLToDocs 断言正文来自 js_content。
func TestWeChatParserParse_继承链dispatch解析覆写(t *testing.T) {
	p, _ := NewWeChatArticleParser()
	html := `<html><head><title>t</title></head><body>` +
		`<article>` + longBody(20) + `</article>` +
		`<div id="js_content">微信正文内容</div></body></html>`
	docs, err := p.parseHTMLToDocs(html, "id", "src")
	if err != nil || len(docs) != 1 {
		t.Fatalf("err=%v docs=%d", err, len(docs))
	}
	// 正文来自 js_content 而非 article（WeChat 覆写生效）
	if docs[0].Text != "微信正文内容" {
		t.Errorf("text = %q, want 微信正文内容（js_content 提取）", docs[0].Text)
	}
}

// TestWeChatParserLazyParse_经self派发 多级链 self 指最终实例：
// LazyParse（BaseParser 模板）经 self 派发到 WebPageParser 覆写的 Parse。
func TestWeChatParserLazyParse_经self派发(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, wechatHTML(`<p>`+longBody(10)+`</p>`))
	}))
	defer srv.Close()
	p, _ := NewWeChatArticleParser()
	var seen int
	err := p.LazyParse(context.Background(), srv.URL, func(d reranker.Document) error {
		seen++
		return nil
	})
	// 本地 URL 被 WeChat 覆写版 validateURLFor 拦截——LazyParse 派发到
	// WebPageParser.Parse（经 self）已生效（错误来自覆写链路）
	var be *exception.BaseError
	if !errors.As(err, &be) || seen != 0 {
		t.Fatalf("LazyParse 应经 self 派发并触达覆写校验: err=%v seen=%d", err, seen)
	}
}

// TestParseWeChatArticleURL_别名函数 非微信 URL 被 WeChat 校验拦截。
func TestParseWeChatArticleURL_别名函数(t *testing.T) {
	_, err := ParseWeChatArticleURL(context.Background(), "https://example.com/x", "")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应 155110, got %v", err)
	}
}
