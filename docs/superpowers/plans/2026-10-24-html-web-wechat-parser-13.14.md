# 13.14 HTML/Web/WeChat Parser Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域 13.14——HTMLFileParser（本地 .html）/WebPageParser（通用网页 URL）/WeChatArticleParser（微信公众号文章）三解析器 + 共享 HTML 工具 + verify 密封 sum type + ParseOptions 四结构化字段扩展，155110 错误码首次启用。

**Architecture:** 方案 A 三层继承逐级嵌入链：`TxtMdParser ← HTMLFileParser ← WebPageParser ← WeChatArticleParser`（Go struct 嵌入）。动态派发双轨：self（BaseParser 派发 Parser 五方法）+ dispatch（`htmlParseDispatcher` 非导出接口派发 `parseHTMLToDocs`/`validateURLFor`，Python classmethod 覆写对应物）。硬错误范式（155110，非软失败）。

**Tech Stack:** goquery v1.12.0（已在树）· net/http + httptest · crypto/tls + x509 · regexp · google/uuid（已在树）· 零新增 go.mod 依赖。

**Spec:** `docs/superpowers/specs/2026-10-24-html-web-wechat-parser-13.14-design.md`

**Python 源（对齐基准，实施时逐行对照）：**
- `/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/parser/html_file_parser.py`
- `/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/parser/web_page_parser.py`
- `/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/parser/wechat_article_parser.py`

**项目规则提醒：**
- 所有注释中文；声明排列：结构体→枚举→常量→全局变量→导出函数→非导出函数（含区块分隔注释）
- 日志：`logger.Info/Warn/Error(logger.ComponentAgentCore)` 风格（parser 包 `logComponent = logger.ComponentAgentCore` 已在 registry.go 定义）；Msg 英文、数据值保留中文
- 测试命令必须带构建标签：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/indexing/processor/parser/...`（CI 同款；单包测试可省 sqlite_fts5 但保留 test）
- 编译前 `pgrep -f 'go (build|test)'` 查残留进程，有则 kill
- 每任务完成即 commit；覆盖率目标 ≥ 89%（parser 包基线 87.7%）

**错误码断言惯例**（excel_parser_test.go 先例）：

```go
var be *exception.BaseError
if !errors.As(err, &be) { t.Fatalf("应为 *exception.BaseError, got %T", err) }
if be.Status() != exception.StatusRetrievalIndexingFetchError { t.Errorf(...) }
if be.Message() != "期望文案" { t.Errorf(...) } // WithMsg 覆盖渲染，Message() 返回完整自定义文案
```

---

### Task 1: ParseOptions 四结构化字段 + With 构造器

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/base.go`（ParseOptions 结构体 + 四 With 函数）
- Test: `internal/agentcore/retrieval/indexing/processor/parser/base_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

在 `base_test.go` 追加：

```go
// TestParseOptions_网页四字段覆盖 测试 13.14 新增四结构化字段的 With 构造器。
func TestParseOptions_网页四字段覆盖(t *testing.T) {
	o := NewParseOptions(
		WithTimeout(5*time.Second),
		WithUserAgent("test-ua"),
		WithVerify(NewVerifySkip()),
		WithHTTPClient(&http.Client{}),
	)
	if o.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", o.Timeout)
	}
	if o.UserAgent != "test-ua" {
		t.Errorf("UserAgent = %q, want %q", o.UserAgent, "test-ua")
	}
	if _, ok := o.Verify.(verifySkip); !ok {
		t.Errorf("Verify 类型 = %T, want verifySkip", o.Verify)
	}
	if o.HTTPClient == nil {
		t.Error("HTTPClient 应非 nil")
	}
}

// TestParseOptions_网页四字段零值未设 测试零值回退语义（调用方判零值回落实例字段）。
func TestParseOptions_网页四字段零值未设(t *testing.T) {
	o := NewParseOptions()
	if o.Timeout != 0 || o.UserAgent != "" || o.Verify != nil || o.HTTPClient != nil {
		t.Error("零值 ParseOptions 四字段应全未设")
	}
}
```

base_test.go 需补 import：`"net/http"`、`"time"`。

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestParseOptions_网页' -v
```

Expected: 编译失败 `undefined: WithTimeout`（verify 类型 Task 2 才定义，本任务先建最小占位——见 Step 3 说明）。

- [ ] **Step 3: 写最小实现**

`base.go` ParseOptions 结构体追加四字段（DocID/LLMClient/Extra 既有字段不动）：

```go
// ParseOptions Parser 可选参数集合。
type ParseOptions struct {
	// DocID 文档 ID（Python doc_id）。
	DocID string
	// LLMClient 可选 LLM 客户端（Python llm_client），13.15 Captioner 消费。
	LLMClient *llm.Model
	// Extra 额外参数（Python **kwargs），13.11 AutoFileParser 消费 file_name 等。
	Extra map[string]any
	// Timeout 请求超时（Python timeout float 秒→Duration）。零值=未设，
	// 调用方回落实例字段。仅自建 client 时生效（注入 client 自带 Timeout）。
	Timeout time.Duration
	// UserAgent HTTP UA 头（Python user_agent）。空串=未设。
	UserAgent string
	// Verify TLS 校验模式（Python verify: bool|str|SSLContext）。nil=未设。
	// 密封 sum type 四变体，13.14 定义。
	Verify verifyMode
	// HTTPClient 注入共享客户端（Python httpx.AsyncClient）。非 nil 即生效，
	// 此时 Verify/Timeout/UserAgent 失效（client 自身配置生效）。
	HTTPClient *http.Client
}
```

base.go import 追加：`"net/http"`、`"time"`。导出函数区块追加四构造器（紧邻 WithExtra 之后）：

```go
// WithTimeout 设置请求超时（13.14 网页解析）。
func WithTimeout(d time.Duration) ParseOption {
	return func(o *ParseOptions) { o.Timeout = d }
}

// WithUserAgent 设置 HTTP User-Agent（13.14 网页解析）。
func WithUserAgent(ua string) ParseOption {
	return func(o *ParseOptions) { o.UserAgent = ua }
}

// WithVerify 设置 TLS 校验模式（13.14 网页解析）。
func WithVerify(v verifyMode) ParseOption {
	return func(o *ParseOptions) { o.Verify = v }
}

// WithHTTPClient 注入共享 HTTP 客户端（13.14 网页解析）。
func WithHTTPClient(c *http.Client) ParseOption {
	return func(o *ParseOptions) { o.HTTPClient = c }
}
```

**本任务最小占位**：verifyMode 接口与 verifySkip 变体在 Task 2 完整定义；本任务在 `html_file_parser.go` 尚未创建前，先在 base.go 顶部临时声明最小集（Task 2 会搬到 web_page_parser.go 并删除此处）：

```go
// verifyMode TLS 校验模式密封接口（Task 2 完整实现，先占位编译）。
type verifyMode interface{ isVerifyMode() }

// verifySkip 跳过校验变体（Task 2 完整实现）。
type verifySkip struct{}

func (verifySkip) isVerifyMode() {}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestParseOptions_网页' -v
```

Expected: 2 用例 PASS。

- [ ] **Step 5: 全包回归 + gofmt + commit**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ && gofmt -l internal/agentcore/retrieval/indexing/processor/parser/ | grep -v _test.go; git add -A && git commit -m "feat(parser): 13.14 ParseOptions 扩展网页四结构化字段"
```

Expected: 全包 PASS、gofmt 无输出、commit 成功。

---

### Task 2: verify 密封 sum type 四变体

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/web_page_parser.go`（本任务只放 verify 类型 + 常量 + 正则；解析器本体 Task 4）
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/base.go`（删除 Task 1 占位 verifyMode/verifySkip）
- Test: `internal/agentcore/retrieval/indexing/processor/parser/web_page_parser_test.go`（本任务只放 verify 用例）

- [ ] **Step 1: 写失败测试**

创建 `web_page_parser_test.go`：

```go
package parser

import (
	"crypto/tls"
	"testing"
)

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

// TestVerifyMode_密封包外不可实现 编译期约束靠非导出方法——
// 包外类型无法实现 isVerifyMode（此处仅验证包内四变体均实现）。
func TestVerifyMode_密封包外不可实现(t *testing.T) {
	var modes []verifyMode = []verifyMode{
		verifyDefault{}, verifySkip{}, verifyCAPath{}, verifyTLSConfig{},
	}
	for i, m := range modes {
		if m == nil {
			t.Errorf("变体 %d 不应为 nil", i)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestVerifyMode' -v
```

Expected: 编译失败 `undefined: NewVerifyDefault` 等。

- [ ] **Step 3: 写实现**

创建 `web_page_parser.go`（文件头 + verify 类型 + 常量 + 正则；WebPageParser 结构体 Task 4 追加）。注意声明排序区块注释：

```go
// Package 内 web_page_parser.go：通用网页 URL 解析器（13.14）。
package parser

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// verifyMode TLS 校验模式密封接口（对齐 Python verify: bool | str | ssl.SSLContext）。
//
// 密封机制：非导出标记方法 isVerifyMode()，包外无法新增变体（13.13 cellValue 先例）。
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
	// httpURLPattern http(s) URL 模式（对齐 HTTP_URL_PATTERN，(?i) 对齐 IGNORECASE）。
	httpURLPattern = regexp.MustCompile(`(?i)^https?://\S+`)
	// wechatMPURLPattern 微信公众号文章 URL 模式（对齐 WECHAT_MP_URL_PATTERN）。
	// 偏差：Go RE2 无惰性量词，尾部 .*? 按 .* 实现——无后续锚点两者等价。
	wechatMPURLPattern = regexp.MustCompile(`(?i)^https?://(?:mp\.weixin\.qq\.com|.*?\.weixin\.qq\.com)/s\b.*`)
	// verifyDefaultInstance 默认变体单例（对齐 Python True 字面量）。
	verifyDefaultInstance = verifyDefault{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerifyDefault 构造默认校验变体（单例）。
func NewVerifyDefault() verifyMode { return verifyDefaultInstance }

// NewVerifySkip 构造跳过校验变体。
func NewVerifySkip() verifyMode { return verifySkip{} }

// NewVerifyCAPath 构造 CA bundle 路径变体。
func NewVerifyCAPath(path string) verifyMode { return verifyCAPath{path: path} }

// NewVerifyTLSConfig 构造自定义 TLS 配置变体。
func NewVerifyTLSConfig(cfg *tls.Config) verifyMode { return verifyTLSConfig{cfg: cfg} }

// ──────────────────────────── 非导出函数 ────────────────────────────

// isWeChatArticleURL 判定微信文章 URL（对齐 _is_wechat_article_url）。
func isWeChatArticleURL(url string) bool {
	return url != "" && wechatMPURLPattern.MatchString(strings.TrimSpace(url))
}
```

同时删除 base.go 中 Task 1 的占位 verifyMode/verifySkip（避免重复定义）。

**注意**：本任务文件含未使用 import（goquery/uuid/exception 等为 Task 4/5 预留），编译会报 unused——本任务 Step 3 先只写实际引用的 import（`crypto/tls`、`regexp`、`strings`），Task 4/5 时再补。

- [ ] **Step 4: 运行测试确认通过**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestVerifyMode' -v
```

Expected: 3 用例 PASS。

- [ ] **Step 5: commit**

```bash
git add -A && git commit -m "feat(parser): 13.14 verify 密封 sum type 四变体 + URL 正则 + 默认常量"
```

---

### Task 3: 共享 HTML 工具函数（html_file_parser.go 上半部）

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/html_file_parser.go`（本任务只放共享工具五个函数；HTMLFileParser 结构体 Task 4）
- Test: Create: `internal/agentcore/retrieval/indexing/processor/parser/html_file_parser_test.go`（本任务只放工具用例）

- [ ] **Step 1: 写失败测试**

创建 `html_file_parser_test.go`：

```go
package parser

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// 工具函数测试用 HTML 母版（正文 ≥50 字符，article 命中主内容选择器）。
const toolTestHTML = `<!DOCTYPE html>
<html><head>
<meta property="og:title" content=" 测试标题 "/>
<title>fallback title</title>
</head><body>
<script>var s = "should not appear";</script>
<style>.x { color: red; }</style>
<article><p>这是正文第一段，包含足够长的中文内容用于跨过一百字符门槛。</p>
<p>这是正文第二段，同样包含足够长的中文内容。</p><div>填充</div></article>
<nav>导航文本</nav>
</body></html>`

// TestParseHTML_正常解析 goquery 文档可查询。
func TestParseHTML_正常解析(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	if doc == nil {
		t.Fatal("parseHTML 应返回非 nil 文档")
	}
	if doc.Find("article").Length() != 1 {
		t.Errorf("article 数量 = %d, want 1", doc.Find("article").Length())
	}
}

// TestParseHTML_空输入 空 HTML 返回可查询空文档。
func TestParseHTML_空输入(t *testing.T) {
	doc := parseHTML("")
	if doc == nil || doc.Find("article").Length() != 0 {
		t.Error("空输入应返回空文档")
	}
}

// TestParseHTML_非法输入降级 非法 HTML 降级空文档不 panic。
func TestParseHTML_非法输入降级(t *testing.T) {
	doc := parseHTML("<<<>>>broken")
	if doc == nil {
		t.Error("非法输入应降级非 nil 空文档")
	}
}

// TestExtractTitle_ogTitle命中 og:title 优先且 TrimSpace。
func TestExtractTitle_ogTitle命中(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	if got := extractTitle(doc); got != "测试标题" {
		t.Errorf("title = %q, want %q", got, "测试标题")
	}
}

// TestExtractTitle_回落title标签 og:title content 空串回落 title 标签。
func TestExtractTitle_回落title标签(t *testing.T) {
	html := `<html><head><meta property="og:title" content=""/><title>页签标题</title></head><body></body></html>`
	if got := extractTitle(parseHTML(html)); got != "页签标题" {
		t.Errorf("title = %q, want %q", got, "页签标题")
	}
}

// TestExtractTitle_缺失返回空串 无任何标题来源。
func TestExtractTitle_缺失返回空串(t *testing.T) {
	html := `<html><head></head><body><p>x</p></body></html>`
	if got := extractTitle(parseHTML(html)); got != "" {
		t.Errorf("title = %q, want 空串", got)
	}
}

// TestTextLength_排除scriptStyle script/style 文本不计入且不改树。
func TestTextLength_排除scriptStyle(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	article := doc.Find("article")
	got := textLength(article)
	if strings.ContainsRune(rune(got), 0) { // 占位防御（恒假），真实断言见下
		t.Error("不可能分支")
	}
	// article 内两段约 60+ 字符 + "填充"：脚本与样式在 article 外不计入
	if got < 30 || got > 100 {
		t.Errorf("textLength = %d, 期望 30..100 区间", got)
	}
	// 不改树：article 仍含原 p/div 子节点
	if article.Find("p").Length() != 2 || article.Find("div").Length() != 1 {
		t.Error("textLength 不应修改树结构")
	}
}

// TestTextLength_scriptStyle子树跳过 script 在选区内其内容不计。
func TestTextLength_scriptStyle子树跳过(t *testing.T) {
	html := `<div>短文本<script>超长脚本内容超长脚本内容超长脚本内容超长脚本内容</script></div>`
	doc := parseHTML(html)
	got := textLength(doc.Find("div"))
	if got > 10 {
		t.Errorf("script 内容应不计入，got %d", got)
	}
}

// TestFindMainContent_选择器命中 article 文本>100 命中。
func TestFindMainContent_选择器命中(t *testing.T) {
	long := strings.Repeat("主内容文本。", 20) // >100
	html := `<html><body><article>` + long + `</article></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || sel.Length() == 0 {
		t.Fatal("应命中 article")
	}
	if sel.Is("article") != true {
		t.Error("应返回 article 节点")
	}
}

// TestFindMainContent_门槛不足继续 选择器命中但文本≤100 跳过走 fallback。
func TestFindMainContent_门槛不足继续(t *testing.T) {
	html := `<html><body><article>短</article><div>` + strings.Repeat("后备块文本。", 30) + `</div></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || !sel.Is("div") {
		t.Error("article 门槛不足应走 fallback 命中 div")
	}
}

// TestFindMainContent_fallback树序第一个 body 内 article/main/div/section 树序第一个 >200。
func TestFindMainContent_fallback树序第一个(t *testing.T) {
	long := strings.Repeat("块文本。", 40) // >200
	html := `<html><body><section>短</section><div>` + long + `</div></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || !sel.Is("div") {
		t.Error("树序第一个超200的应为 div")
	}
}

// TestFindMainContent_嵌套外层先到 嵌套块树序外层先遍历到。
func TestFindMainContent_嵌套外层先到(t *testing.T) {
	long := strings.Repeat("嵌套文本。", 40)
	html := `<html><body><div>` + long + `<div>` + long + `</div></div></body></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || sel.Length() != 1 || !sel.Is("div") {
		t.Fatal("应返回单一 div")
	}
	// 外层 div 先到：返回节点应包含内层 div（外层是祖先）
	if sel.Find("div").Length() != 1 {
		t.Error("应返回外层 div（含内层）")
	}
}

// TestFindMainContent_无body返回nil 无 body 元素。
func TestFindMainContent_无body返回nil(t *testing.T) {
	// goquery 对片段自动补 html/body——用纯 head 文档
	html := `<html><head><title>x</title></head></html>`
	sel := findMainContent(parseHTML(html))
	if sel != nil {
		t.Error("无 body 应返回 nil")
	}
}

// TestGetTextFromSoup_脚本样式移除 script/style 内容不出现在产出。
func TestGetTextFromSoup_脚本样式移除(t *testing.T) {
	doc := parseHTML(toolTestHTML)
	text := getTextFromSoup(doc.Find("body"))
	if strings.Contains(text, "should not appear") {
		t.Error("script 内容不应出现")
	}
	if strings.Contains(text, "color: red") {
		t.Error("style 内容不应出现")
	}
}

// TestGetTextFromSoup_拼接与压缩 文本节点 \n 拼接 + 空白压缩。
func TestGetTextFromSoup_拼接与压缩(t *testing.T) {
	html := `<div><p>第一段</p><p>第二段</p></div>`
	text := getTextFromSoup(parseHTML(html).Find("div"))
	if text != "第一段\n第二段" {
		t.Errorf("text = %q, want %q", text, "第一段\n第二段")
	}
}

// TestGetTextFromSoup_空白归一 多空格压缩单空格、多换行压缩双换行。
func TestGetTextFromSoup_空白归一(t *testing.T) {
	html := `<div><p>词一   词二</p><p>第二段</p><p>第三段</p></div>`
	// 三个 p → 两处 \n 分隔；段内多空格压一
	text := getTextFromSoup(parseHTML(html).Find("div"))
	if text != "词一 词二\n第二段\n第三段" {
		t.Errorf("text = %q", text)
	}
	// 连续空行压缩 \n\n：goquery 逐节点 \n 拼接本身不产生空行，
	// 该场景由空白文本节点产生——单独构造
	html2 := `<div><p>a</p>   <p>b</p></div>`
	text2 := getTextFromSoup(parseHTML(html2).Find("div"))
	if text2 != "a\nb" {
		t.Errorf("text2 = %q, want %q", text2, "a\nb")
	}
}

// TestGetTextFromSoup_首尾TrimSpace 前后空白剥离。
func TestGetTextFromSoup_首尾TrimSpace(t *testing.T) {
	html := `<div>  前后有空格  </div>`
	if got := getTextFromSoup(parseHTML(html).Find("div")); got != "前后有空格" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestParseHTML|TestExtractTitle|TestTextLength|TestFindMainContent|TestGetTextFromSoup' -v
```

Expected: 编译失败 `undefined: parseHTML` 等。

- [ ] **Step 3: 写实现**

创建 `html_file_parser.go`（本任务仅共享工具；import：`regexp`、`regexp2`、`strings`、`goquery`）：

```go
package parser

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/dlclark/regexp2"
)

// ──────────────────────────── 结构体 ────────────────────────────
// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// htmlMainSelectorTextLength 主内容选择器文本门槛（Python _find_main_content > 100）。
	htmlMainSelectorTextLength = 100
	// htmlFallbackTextLength fallback 块文本门槛（Python > 200）。
	htmlFallbackTextLength = 200
	// htmlMinArticleLength 正文最小长度（Python len(text) < 50）。
	htmlMinArticleLength = 50
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mainContentSelectors 主内容选择器列表（对齐 MAIN_CONTENT_SELECTORS 字面复制）。
	mainContentSelectors = []string{
		"article",
		"main",
		`[role="main"]`,
		".article-body",
		".post-content",
		".content",
		".entry-content",
		".post-body",
		"#content",
		".main-content",
	}
	// htmlSpaceRe 行内空白压缩（对齐 re.sub(r"[ \t]+", " ", text)）。
	htmlSpaceRe = regexp.MustCompile(`[ \t]+`)
	// htmlBlankLineRe 空行压缩（对齐 re.sub(r"\n\s*\n", "\n\n", text)；regexp2 对齐 Python \s 含义）。
	htmlBlankLineRe = regexp2.MustCompile(`\n\s*\n`, regexp2.None)
)

// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// parseHTML 解析 HTML 字符串（对齐 _parse_html；goquery 对应 BeautifulSoup）。
//
// 解析失败返回空文档（对齐 web_tools 先例降级）。
// Python: html_file_parser.py _parse_html
func parseHTML(html string) *goquery.Document {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		doc, _ = goquery.NewDocumentFromReader(strings.NewReader(""))
		return doc
	}
	return doc
}

// extractHTMLTitle 提取标题（对齐 _extract_title）。
//
// og:title meta content 优先（空串回落），title 标签文本兜底。
// Python: html_file_parser.py _extract_title
func extractTitle(soup *goquery.Document) string {
	meta := soup.Find(`meta[property="og:title"]`)
	if meta.Length() > 0 {
		if content, ok := meta.Attr("content"); ok && content != "" {
			return strings.TrimSpace(content)
		}
	}
	title := soup.Find("title")
	if title.Length() > 0 {
		return strings.TrimSpace(title.Text())
	}
	return ""
}

// findMainContent 查找主内容节点（对齐 _find_main_content 两段式）。
//
// ①10 选择器逐一命中且 textLength > 100 即返；②fallback：body 内
// article/main/div/section 树序第一个 textLength > 200；③无 body 返回 nil。
// Python: html_file_parser.py _find_main_content
func findMainContent(soup *goquery.Document) *goquery.Selection {
	for _, sel := range mainContentSelectors {
		node := soup.Find(sel)
		if node.Length() > 0 && textLength(node) > htmlMainSelectorTextLength {
			return node
		}
	}
	body := soup.Find("body")
	if body.Length() == 0 {
		return nil
	}
	found := body.Find("article,main,div,section").FilterFunction(func(i int, s *goquery.Selection) bool {
		return textLength(s) > htmlFallbackTextLength
	})
	if found.Length() > 0 {
		return found.First()
	}
	return body
}

// textLength 近似主文本长度（对齐 _text_length：排除 script/style，不改树）。
//
// 遍历选区内全部文本节点（含选区自身直接文本），跳过 script/style
// 子树，累加 TrimSpace 后长度。
// Python: html_file_parser.py _text_length
func textLength(sel *goquery.Selection) int {
	total := 0
	collectTextLength(sel, &total)
	return total
}

// collectTextLength 递归收集文本节点长度（textLength 辅助）。
//
// 文本节点 TrimSpace 后计入；script/style 标签整体跳过。
func collectTextLength(sel *goquery.Selection, total *int) {
	sel.Contents().Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			*total += len(strings.TrimSpace(s.Text()))
			return
		}
		name := strings.ToLower(goquery.NodeName(s))
		if name == "script" || name == "style" {
			return
		}
		collectTextLength(s, total)
	})
}

// getTextFromSoup 从选区提取文本（对齐 _get_text_from_soup）。
//
// ①移除 script/style（对齐 decompose）；②文本节点按文档序收集
// TrimSpace 后 "\n" 拼接（对齐 get_text(separator="\n", strip=True)）；
// ③[ \t]+→单空格；④\n\s*\n→\n\n；⑤TrimSpace。
// Python: html_file_parser.py _get_text_from_soup
func getTextFromSoup(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	clone := sel. /* 需复制防共享树——见下方实现说明 */ Nodes[0]
	_ = clone
	// 实现见 Step 3 续
	return collectTextAndJoin(sel)
}

// collectTextAndJoin 收集并拼接文本（getTextFromSoup 辅助）。
func collectTextAndJoin(sel *goquery.Selection) string {
	var parts []string
	sel.Find("script,style").Remove()
	appendTextNodes(sel, &parts)
	joined := strings.Join(parts, "\n")
	joined = htmlSpaceRe.ReplaceAllString(joined, " ")
	replaced, _ := htmlBlankLineRe.Replace(joined, "\n\n", -1, -1)
	return strings.TrimSpace(replaced)
}

// appendTextNodes 递归收集文本节点（collectTextAndJoin 辅助）。
func appendTextNodes(sel *goquery.Selection, parts *[]string) {
	sel.Contents().Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			if t := strings.TrimSpace(s.Text()); t != "" {
				*parts = append(*parts, t)
			}
			return
		}
		appendTextNodes(s, parts)
	})
}
```

**实现说明（重要，写给实施者）：**
- 上方 `getTextFromSoup` 草稿含伪代码占位（`sel.Nodes[0]` 复制行）——**最终实现以此为准**：

```go
// getTextFromSoup 从选区提取文本（对齐 _get_text_from_soup）。
//
// ①移除选区内 script/style（对齐 decompose——直接 Remove 修改树，
// 与 textLength 的不改树相反，Python 语义即如此）；②文本节点按文档序
// 收集 TrimSpace 后 "\n" 拼接（对齐 get_text(separator="\n", strip=True)）；
// ③[ \t]+→单空格；④\n\s*\n→\n\n；⑤TrimSpace。
// Python: html_file_parser.py _get_text_from_soup
func getTextFromSoup(sel *goquery.Selection) string {
	if sel == nil || sel.Length() == 0 {
		return ""
	}
	sel.Find("script,style").Remove()
	var parts []string
	appendTextNodes(sel, &parts)
	joined := strings.Join(parts, "\n")
	joined = htmlSpaceRe.ReplaceAllString(joined, " ")
	replaced, err := htmlBlankLineRe.Replace(joined, "\n\n", -1, -1)
	if err != nil { // regexp2 替换失败保持原串（防御，理论不达）
		replaced = joined
	}
	return strings.TrimSpace(replaced)
}
```

- `findMainContent` 的 fallback 用 `FilterFunction` + `First()`：goquery `Find("article,main,div,section")` 返回文档序全部匹配，Filter 保序过滤后 First 取树序第一个（嵌套时外层先到——goquery 文档序遍历保证）。
- `regexp2` 的 `\s` 与 Python 语义一致（含 `\x1c`-`\x1f` 等），故空行压缩用 regexp2；行内空白 `[ \t]+` 无语义差异用标准 regexp。
- goquery.NodeName 对文本节点返回 `#text`；对元素返回标签名（大小写按原文，统一 ToLower 比较）。

- [ ] **Step 4: 运行测试确认通过**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestParseHTML|TestExtractTitle|TestTextLength|TestFindMainContent|TestGetTextFromSoup' -v
```

Expected: 全部 PASS。**若 `TestFindMainContent_无body返回nil` 失败**（goquery 解析 `<html><head>...` 可能自动补 body）：改用 `goquery.NewDocumentFromReader` 之外的 `html.Parse` 直接构建（head-only 文档 goquery 同样会补全 body）——处理办法：该用例改为验证"body 存在但空"时返回 body 本身（`findMainContent` ③语义：body 存在即返回，哪怕空），并将"无 body"场景标注为 goquery 解析器不可能路径（Python html.parser 对无 body 片段 body 为 None）。**决策已预置**：采用后者——测试改为：

```go
// TestFindMainContent_空body返回body body 存在但无内容时返回 body 本身
// （goquery 解析自动补全 body，无 body 为不可能路径，Python 侧 body None 场景不达）。
func TestFindMainContent_空body返回body(t *testing.T) {
	html := `<html><head><title>x</title></head></html>`
	sel := findMainContent(parseHTML(html))
	if sel == nil || !sel.Is("body") {
		t.Error("空 body 应回退返回 body 自身")
	}
}
```

- [ ] **Step 5: commit**

```bash
git add -A && git commit -m "feat(parser): 13.14 共享 HTML 工具函数（parseHTML/extractTitle/findMainContent/textLength/getTextFromSoup）"
```

---

### Task 4: HTMLFileParser + WebPageParser 本体（dispatch 接口 + downloadHTML + ParseURL）

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/html_file_parser.go`（追加 HTMLFileParser 结构体 + Parse + init 注册）
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/web_page_parser.go`（追加 htmlParseDispatcher 接口 + WebPageParser + downloadHTML + ParseURL + 别名函数）
- Test: Modify: 两个对应 `_test.go`

- [ ] **Step 1: 写失败测试**

`html_file_parser_test.go` 追加：

```go
// longBody 生成超 50 字符正文。
func longBody(n int) string { return strings.Repeat("正文内容段落。", n) }

// TestHTMLFileParserParse_正常 单 Document 全字段。
func TestHTMLFileParserParse_正常(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/page.html"
	html := `<html><head><meta property="og:title" content="文档标题"/></head><body><article>` + longBody(12) + `</article></body></html>`
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := NewHTMLFileParser()
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if docs[0].Metadata["title"] != "文档标题" {
		t.Errorf("title = %v", docs[0].Metadata["title"])
	}
	if docs[0].Metadata["source_type"] != "web_page" {
		t.Errorf("source_type = %v", docs[0].Metadata["source_type"])
	}
	if docs[0].Text == "" {
		t.Error("正文不应为空")
	}
	if docs[0].ID != path { // DocID 未设回落 doc 路径
		t.Errorf("ID = %q, want %q", docs[0].ID, path)
	}
}

// TestHTMLFileParserParse_空文件 155110。
func TestHTMLFileParserParse_空文件(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/empty.html"
	os.WriteFile(path, []byte(""), 0o644)
	p, _ := NewHTMLFileParser()
	_, err := p.Parse(context.Background(), path)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110, got %v", err)
	}
	if be.Message() != "Could not read HTML file or file is empty (source="+path+")" {
		t.Errorf("message = %q", be.Message())
	}
}

// TestHTMLFileParserParse_无主内容 155110。
func TestHTMLFileParserParse_无主内容(t *testing.T) {
	// 选择器全不中 + body fallback 全 <200：返回 body，但正文会 <50 → 走 too short 分支；
	// 真正"找不到主内容"需 body 缺失——goquery 自动补 body，该分支以 findMainContent
	// nil 语义直接单元测（见 Task 3 空body用例）。此处测 too short 分支。
	dir := t.TempDir()
	path := dir + "/short.html"
	html := `<html><body><article>短</article></body></html>`
	os.WriteFile(path, []byte(html), 0o644)
	p, _ := NewHTMLFileParser()
	_, err := p.Parse(context.Background(), path)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应返回 155110, got %v", err)
	}
	if be.Message() != "Article content too short or empty after parsing html (source="+path+")" {
		t.Errorf("message = %q", be.Message())
	}
}

// TestHTMLFileParserParse_GBK编码 super 链编码检测。
func TestHTMLFileParserParse_GBK编码(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/gbk.html"
	// GBK 编码的中文正文（经 Python encode 交叉验证的字节）
	encoder := simplifiedchinese.GB18030.NewEncoder()
	gbk, err := encoder.Bytes([]byte(`<html><head><meta property="og:title" content="标题"/></head><body><article>` + longBody(12) + `</article></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, gbk, 0o644)
	p, _ := NewHTMLFileParser()
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(docs) != 1 || docs[0].Metadata["title"] != "标题" {
		t.Fatalf("GBK 链路失败: %v", docs)
	}
}

// TestHTMLFileParserParse_注册表查询 .html/.htm 注册可查。
func TestHTMLFileParserParse_注册表查询(t *testing.T) {
	for _, ext := range []string{".html", ".htm", ".HTML", ".HTM"} {
		if _, ok := getParserFactory(ext); !ok {
			t.Errorf("扩展名 %s 应已注册", ext)
		}
	}
}
```

`web_page_parser_test.go` 追加：

```go
// TestWebPageParserSupports http(s) 且非微信。
func TestWebPageParserSupports(t *testing.T) {
	p, _ := NewWebPageParserWithDefaults()
	cases := []struct {
		in   string
		want bool
	}{
		{"http://a.com/x", true},
		{"https://a.com/x", true},
		{"HTTP://A.COM/X", true},   // IGNORECASE
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

// TestWebPageParserParse_注入client client 生效且 UA 仍注入。
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

// TestWebPageParserParse_uuid兜底 docID 与 url 双空时 ID 为 uuid。
// 注：Parse 的 docID = opts.DocID 或 doc（doc 即 url），url 空串走不到 download——
// 该分支仅 ParseURL 直调 docID="" 时也不可达（url 非空才有请求）。
// 真实可达路径：ParseURL(docID="", url非空) → effectiveID=url。uuid 分支
// 在 ParseURL 入参 url 空时触发——validateURL 先拦截。故本用例改为验证
// ParseURL(docID="自定义") 时 ID=自定义。
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
```

`html_file_parser_test.go` import 追加：`"context"`、`"errors"`、`"os"`、`exception`、`golang.org/x/text/encoding/simplifiedchinese`。
`web_page_parser_test.go` import 追加：`"context"`、`"errors"`、`"fmt"`、`"net/http"`、`"net/http/httptest"`、`"strings"`、`"time"`、`exception`。

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestHTMLFileParser|TestWebPageParser|TestValidateURL|TestDownloadHTML' -v
```

Expected: 编译失败 `undefined: NewHTMLFileParser` 等。

- [ ] **Step 3: 写实现**

`html_file_parser.go` 追加（import 追加 `context`、`llm`、`reranker`、`exception`、`os` 不需要——ParseContent 经 TxtMd 嵌入调用）：

结构体区块追加：

```go
// HTMLFileParser 本地 .htm/.html 文件解析器（对齐 Python HTMLFileParser(TxtMdParser)）。
//
// 嵌入 TxtMdParser 复用编码容错读文件（super()._parse 对应物：
// p.TxtMdParser.ParseContent 显式穿透）。覆写整体 Parse（非 ParseContent）
// 产出带 title/source_type metadata 的单 Document。
// 硬错误范式：读不到文件/找不到主内容/正文过短 → 155110。
//
// Python: html_file_parser.py HTMLFileParser
type HTMLFileParser struct {
	// TxtMdParser 父级（编码检测读文件）
	*TxtMdParser
}
```

导出函数区块追加：

```go
// NewHTMLFileParser 构造 HTMLFileParser。
//
// 零参数（对齐 Python __init__(**kwargs): super().__init__(**kwargs)）。
// 多级嵌入链构造：TxtMdParser 层的 BaseParser.self 注入最终实例。
// Python: html_file_parser.py HTMLFileParser.__init__
func NewHTMLFileParser() (*HTMLFileParser, error) {
	p := &HTMLFileParser{}
	// 重建 TxtMdParser 链并注入最终 self（多级链 self 指向最终实例）
	tmp, err := NewTxtMdParser()
	if err != nil {
		return nil, err
	}
	p.TxtMdParser = tmp
	// 覆盖 BaseParser.self 为最终实例：BaseParser 模板方法派发到 HTMLFile 覆写版本
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.TxtMdParser.BaseParser = bp
	return p, nil
}

// Parse 解析本地 HTML 文件（覆写整体，对齐 Python HTMLFileParser.parse）。
//
// 1. super()._parse 读文件（TxtMdParser 编码检测，软失败返回空串）
//    空串 → 155110 "Could not read HTML file or file is empty (source=%s)"
// 2. findMainContent 找主内容；找不到 → 155110
// 3. getTextFromSoup 提取正文；空或 <50 字符 → 155110
// 4. 单 Document（title 兜底 "(无标题)"，source_type=web_page）
//
// Python: html_file_parser.py HTMLFileParser.parse
func (p *HTMLFileParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)
	html, _ := p.TxtMdParser.ParseContent(ctx, doc, o.LLMClient)
	if html == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Could not read HTML file or file is empty (source="+doc+")"),
		)
	}
	return p.parseHTMLToDocs(html, o.DocID, doc)
}
```

非导出函数区块追加（供 WebPage 共用 + dispatch 接口实现）：

```go
// parseHTMLToDocs 解析 HTML 字符串产出 Document（对齐 Python _parse_html classmethod）。
//
// title 提取 → findMainContent → getTextFromSoup → 门槛校验 → 单 Document。
// 空门槛 htmlMinArticleLength=50（Python len(text) < 50）。
// Python: html_file_parser.py HTMLFileParser._parse_html
func (p *HTMLFileParser) parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error) {
	soup := parseHTML(html)
	title := extractTitle(soup)
	contentNode := findMainContent(soup)
	if contentNode == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Could not find main content in HTML (source="+source+")"),
		)
	}
	text := getTextFromSoup(contentNode)
	if len(text) == 0 || len(text) < htmlMinArticleLength {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Article content too short or empty after parsing html (source="+source+")"),
		)
	}
	return []reranker.Document{{
		ID:   docID,
		Text: text,
		Metadata: map[string]any{
			"title":       title,
			"source_type": "web_page",
		},
	}}, nil
}
```

**title 兜底修正**：Python `title or "(无标题)"` 在 metadata 构造处——上面代码写 `"title": title`，需改为：

```go
	effectiveTitle := title
	if effectiveTitle == "" {
		effectiveTitle = "(无标题)"
	}
	return []reranker.Document{{
		ID:   docID,
		Text: text,
		Metadata: map[string]any{
			"title":       effectiveTitle,
			"source_type": "web_page",
		},
	}}, nil
```

init 注册（文件尾非导出区块后追加，或独立 init 函数——Go 允许多 init）：

```go
// init 注册 .htm/.html（对齐 @register_parser([".htm", ".HTM", ".html", ".HTML"])）。
func init() {
	RegisterParser([]string{".htm", ".HTM", ".html", ".HTML"}, func() Parser {
		p, _ := NewHTMLFileParser()
		return p
	})
}
```

`web_page_parser.go` 追加。结构体区块：

```go
// htmlParseDispatcher _parse_html/_validate_url 覆写动态派发接口。
//
// Python classmethod 覆写（WeChatArticleParser._parse_html/_validate_url）
// 的 Go 对应物：WebPageParser 构造时注入最终实例，ParseURL 经此派发。
// 仅本包实现（接口非导出）。
type htmlParseDispatcher interface {
	// parseHTMLToDocs 对齐 Python _parse_html classmethod
	parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error)
	// validateURLFor 对齐 Python _validate_url 静态方法
	validateURLFor(url string) error
}

// WebPageParser 通用网页 URL 解析器（对齐 Python WebPageParser(HTMLFileParser)）。
//
// 嵌入 HTMLFileParser 复用 parseHTMLToDocs/extractTitle 等共享工具；
// dispatch 注入最终实例实现 classmethod 覆写派发。
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
```

导出函数区块：

```go
// NewWebPageParserWithDefaults 默认参数构造（对齐 Python 默认 timeout=30.0/user_agent=DEFAULT/verify=True）。
func NewWebPageParserWithDefaults() (*WebPageParser, error) {
	return NewWebPageParser(defaultTimeoutSeconds, defaultUserAgent, NewVerifyDefault())
}

// NewWebPageParser 构造 WebPageParser。
//
// timeout 收 float64 秒（字面贴近 Python timeout: float = DEFAULT_TIMEOUT）。
// Python: web_page_parser.py WebPageParser.__init__
func NewWebPageParser(timeout float64, userAgent string, verify verifyMode) (*WebPageParser, error) {
	hfp, err := NewHTMLFileParser()
	if err != nil {
		return nil, err
	}
	p := &WebPageParser{
		HTMLFileParser: hfp,
		timeout:        time.Duration(timeout * float64(time.Second)),
		userAgent:      userAgent,
		verify:         verify,
	}
	p.dispatch = p // 默认派发自身；子类构造时覆写注入
	// 重建 self 链：BaseParser.self 指向 WebPageParser 最终实例
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
// validateURL（经 dispatch，WeChat 覆写生效）→ downloadHTML →
// effectiveID 三级回落 → parseHTMLToDocs（经 dispatch）→ 补 source_url + Info 日志。
// Python: web_page_parser.py WebPageParser.parse_url
func (p *WebPageParser) ParseURL(ctx context.Context, url string, docID string, timeout time.Duration, userAgent string, verify verifyMode, client *http.Client) ([]reranker.Document, error) {
	if err := p.dispatch.validateURLFor(url); err != nil {
		return nil, err
	}
	html, err := downloadHTML(ctx, url, timeout, userAgent, verify, client)
	if err != nil {
		return nil, err
	}
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
	}
	// 对齐 Python: logger.info("Parsed web page: url=%s title=%s", ...)
	for _, d := range docs {
		title, _ := d.Metadata["title"].(string)
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
```

非导出函数区块：

```go
// validateURLFor 校验 URL（WebPage 默认实现，对齐 Python _validate_url 静态方法）。
//
// 非 http(s) → 155110 "Not a valid HTTP URL: %q"；微信 URL → 155110 引导专用解析器。
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

// downloadHTML 下载网页 HTML（对齐 Python _download_html）。
//
// 注入 client 时直接使用（UA header 仍注入）；自建时按 verify 变体配
// transport + Timeout。三分支错误对齐：网络错→"Web page fetch failed for
// %s: %v"；非 2xx→"Web page request failed: %d for %s"；其余→同网络错格式。
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
		default: // verifyDefault / nil：零配置走系统 CA
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
	defer resp.Body.Close()
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
func ParseWebPageURL(ctx context.Context, url string, docID string, opts ...ParseOption) ([]reranker.Document, error) {
	p, err := NewWebPageParserWithDefaults()
	if err != nil {
		return nil, err
	}
	o := NewParseOptions(opts...)
	return p.ParseURL(ctx, url, docID, o.Timeout, o.UserAgent, o.Verify, o.HTTPClient)
}
```

web_page_parser.go import 补齐：`context`、`fmt`、`io`、`net/http`、`os`、`crypto/x509`、`goquery`（Task 2 后实际未用则不加）、`uuid`、`llm`、`reranker`、`exception`、`logger`。

- [ ] **Step 4: 运行测试确认通过**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestHTMLFileParser|TestWebPageParser|TestValidateURL|TestDownloadHTML' -v
```

Expected: 全部 PASS。

- [ ] **Step 5: commit**

```bash
git add -A && git commit -m "feat(parser): 13.14 HTMLFileParser + WebPageParser（dispatch 派发 + downloadHTML 三分支 + ParseURL 全链）"
```

---

### Task 5: WeChatArticleParser

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/wechat_article_parser.go`
- Test: Create: `internal/agentcore/retrieval/indexing/processor/parser/wechat_article_parser_test.go`

- [ ] **Step 1: 写失败测试**

```go
package parser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// wechatHTML 生成微信文章页面。
func wechatHTML(body string) string {
	return `<html><head><title>公众号文章</title></head><body><div id="js_content">` + body + `</div></body></html>`
}

// TestWeChatParserParse_直调parseHTMLToDocs js_content 命中（无 50 字符门槛）。
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
// Parse（继承 WebPage）→ ParseURL → dispatch 到 WeChat 版本。
// 本地 httptest URL 非 mp.weixin 域名但 dispatch.validateURLFor 是 WeChat
// 覆写版（只认微信正则会拦截）——为验证 dispatch 路径，页面含 js_content
// 且 URL 为普通 URL 时 WeChat validateURLFor 会报"Not a WeChat article URL"。
// 因此完整链可达性测试用例设计为：断言错误文案来自 WeChat 覆写版。
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

// TestWeChatParserParse_完整链注入client绕过域名校验不可行——
// validateURLFor 在 download 之前执行，无法绕过。微信域名的端到端
// 抓取测试依赖真实网络，不纳入单测（spec §6.2 已记录此约束）。
// dispatch.parseHTMLToDocs 覆写生效由上面的直调用例覆盖。
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestWeChatParser' -v
```

Expected: 编译失败 `undefined: NewWeChatArticleParser`。

- [ ] **Step 3: 写实现**

```go
// wechat_article_parser.go：微信公众号文章解析器（13.14）。
package parser

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WeChatArticleParser 微信公众号文章解析器（对齐 Python WeChatArticleParser(WebPageParser)）。
//
// 覆写 parseHTMLToDocs（js_content 提取）与 validateURLFor（微信正则），
// 经 WebPageParser.dispatch 注入生效。不覆写 Parse（继承 WebPage 链）。
// 无 50 字符门槛，只判空（Python L79-83 字面）。
// Python: wechat_article_parser.py WeChatArticleParser
type WeChatArticleParser struct {
	// WebPageParser 父级（Parse/ParseURL/downloadHTML 继承）
	*WebPageParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewWeChatArticleParser 构造 WeChatArticleParser。
//
// 默认参数复用 WebPage 默认（timeout=30s/UA=DEFAULT/verify=default）。
// dispatch 注入自身（覆写版本生效）。
// Python: wechat_article_parser.py WeChatArticleParser（无自定义 __init__）
func NewWeChatArticleParser() (*WeChatArticleParser, error) {
	wpp, err := NewWebPageParserWithDefaults()
	if err != nil {
		return nil, err
	}
	p := &WeChatArticleParser{WebPageParser: wpp}
	wpp.dispatch = p // 覆写注入：WeChat 版本生效
	// self 链指向最终实例
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.WebPageParser.HTMLFileParser.TxtMdParser.BaseParser = bp
	return p, nil
}

// ParseWeChatArticleURL 包级别名（对齐 parse_wechat_article_url = WeChatArticleParser.parse_url）。
func ParseWeChatArticleURL(ctx context.Context, url string, docID string, opts ...ParseOption) ([]reranker.Document, error) {
	p, err := NewWeChatArticleParser()
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseHTMLToDocs 解析微信文章页面（覆写，对齐 Python _parse_html classmethod）。
//
// title → js_content 提取 → 缺失/空 → 155110（无 50 字符门槛只判空）。
// Python: wechat_article_parser.py WeChatArticleParser._parse_html
func (p *WeChatArticleParser) parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error) {
	soup := parseHTML(html)
	title := extractTitle(soup)
	contentNode := extractJSContent(soup)
	if contentNode == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Could not find article content (js_content) in page (source: "+source+")"),
		)
	}
	text := getTextFromSoup(contentNode)
	if text == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg("Article content is empty after parsing (source: "+source+")"),
		)
	}
	effectiveTitle := title
	if effectiveTitle == "" {
		effectiveTitle = "(无标题)"
	}
	return []reranker.Document{{
		ID:   docID,
		Text: text,
		Metadata: map[string]any{
			"title":       effectiveTitle,
			"source_type": "wechat_article",
		},
	}}, nil
}

// validateURLFor 校验微信文章 URL（覆写，对齐 Python _validate_url 静态方法覆写）。
//
// 非微信 URL → 155110 "Not a WeChat article URL: %q"。
// Python: wechat_article_parser.py WeChatArticleParser._validate_url
func (p *WeChatArticleParser) validateURLFor(url string) error {
	url = strings.TrimSpace(url)
	if !isWeChatArticleURL(url) {
		return exception.BuildError(
			exception.StatusRetrievalIndexingFetchError,
			exception.WithMsg(fmt.Sprintf("Not a WeChat article URL: %q", url)),
		)
	}
	return nil
}

// Supports 覆写：微信文章 URL 才支持（对齐 Python supports 覆写）。
func (p *WeChatArticleParser) Supports(doc string) bool {
	return isWeChatArticleURL(doc)
}

// extractJSContent 提取 js_content 节点（对齐 _extract_js_content）。
func extractJSContent(soup *goquery.Document) *goquery.Selection {
	return soup.Find("div#js_content")
}
```

import 补齐：`fmt`、`strings`、`goquery`。（`logger` 若未用到则不加。）

- [ ] **Step 4: 运行测试确认通过**

```bash
go test -tags test ./internal/agentcore/retrieval/indexing/processor/parser/ -run 'TestWeChatParser' -v
```

Expected: 全部 PASS。

- [ ] **Step 5: commit**

```bash
git add -A && git commit -m "feat(parser): 13.14 WeChatArticleParser（js_content 提取 + dispatch 覆写链）"
```

---

### Task 6: doc.go 更新 + IMPLEMENTATION_PLAN.md 状态 + 覆盖率验证

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/doc.go`（文件目录补三文件）
- Modify: `IMPLEMENTATION_PLAN.md:806`（13.14 行 ☐→✅ + 产出摘要）

- [ ] **Step 1: 更新 doc.go**

文件目录区块（excel_parser.go 行后）追加：

```
//	├── html_file_parser.go # 共享 HTML 工具 + HTMLFileParser：本地 .htm/.html（13.14）
//	├── web_page_parser.go  # WebPageParser：通用网页 URL + verify 密封 sum type（13.14）
//	└── wechat_article_parser.go # WeChatArticleParser：微信公众号文章（13.14）
```

注意原目录树最后一项 `excel_parser.go` 前的 `└──` 改为 `├──`。包概述段落追加一句：

```
// 网页与微信文章经 13.14 三解析器处理：HTMLFileParser 读本地 .html、
// WebPageParser 抓取通用 URL、WeChatArticleParser 提取 js_content——
// 共享 goquery 工具链，硬错误范式（155110）区别于其他解析器的软失败。
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

13.14 行改为（保持表格结构）：

```markdown
| 13.14 | ✅ | HTML/Web Parser | `HTMLFileParser(嵌入TxtMdParser覆写整体Parse:super()._parse编码检测读文件空串→155110+findMainContent两段式[10选择器>100门槛→body内article/main/div/section树序第一个>200→body兜底]+getTextFromSoup[script/style移除+文本节点\n拼接+[ \t]+压缩+\n\s*\n→\n\n]+正文<50→155110+单Document title兜底(无标题)source_type=web_page) + WebPageParser(嵌入HTMLFileParser+dispatch动态派发接口[parseHTMLToDocs/validateURLFor构造注入最终实例——Python classmethod覆写对应物]+Parse四覆盖参数Timeout/UserAgent/Verify/HTTPClient未设回落实例字段+ParseURL[validateURL经dispatch非http→155110 %q/微信URL→155110引导]→downloadHTML[注入client直用UA仍注入/自建按verify四变体配transport+Timeout]三分支错误[网络错→fetch failed for/非2xx→request failed: %d/兜底同网络错]→effectiveID三级回落docID or url or uuid→parseHTMLToDocs经dispatch→补source_url+Info日志parsed web page] + downloadHTML(verifyDefault零配置/verifySkip InsecureSkipVerify/verifyCAPath x509读文件AppendCertsFromPEM/verifyTLSConfig直用) + verifyMode密封sum type四变体(非导出isVerifyMode密封包外不可新增,对齐13.13 cellValue先例) + ParseOptions扩展四结构化字段(Timeout time.Duration/UserAgent/Verify verifyMode/HTTPClient *http.Client注入即生效verify/timeout/UA失效) + WeChatArticleParser(嵌入WebPageParser覆写parseHTMLToDocs[title→js_content提取→缺失/空→155110无50字符门槛只判空→单Document source_type=wechat_article]+覆写validateURLFor[非微信→155110 Not a WeChat article URL: %q]+覆写Supports+不覆写Parse继承WebPage链+dispatch注入自身) + extractJSContent(div#js_content) + 共享工具(parseHTML goquery/extractTitle og:title→title标签/findMainContent/textLength不改树排除scriptstyle/getTextFromSoup) + URL正则(?i)^https?://\S+与WECHAT_MP_URL_PATTERN(尾部.*?按.*实现RE2无惰性量词等价) + init注册.htm/.HTM/.html/.HTML + ParseWebPageURL/ParseWeChatArticleURL包级别名(对齐模块尾赋值) + 155110 FETCH_ERROR首次启用九处error_msg逐字符对齐(注意#6/#7冒号后空格与前四处=不同)`。硬错误范式（parser 层唯一硬错误解析器）。覆盖率：parser包 XX.X%（基线87.7%）。设计：docs/superpowers/specs/2026-10-24-html-web-wechat-parser-13.14-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/html_file_parser.py · web_page_parser.py · wechat_article_parser.py` |
```

覆盖率数字待 Step 3 实测后回填。

- [ ] **Step 3: 覆盖率验证**

```bash
cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -cover ./internal/agentcore/retrieval/indexing/processor/parser/
```

Expected: ≥ 89%。若不足：补用例（优先覆盖 downloadHTML 的 verifyTLSConfig 分支、ParseURL 的 uuid 兜底不可达说明、ParseWebPageURL/ParseWeChatArticleURL 别名函数——别名函数目前无测试，必须补）。

**别名函数补充测试**（若覆盖率不足时加进 web_page_parser_test.go）：

```go
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
}

// TestParseWeChatArticleURL_别名函数 非微信 URL 被 WeChat 校验拦截。
func TestParseWeChatArticleURL_别名函数(t *testing.T) {
	_, err := ParseWeChatArticleURL(context.Background(), "https://example.com/x", "")
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusRetrievalIndexingFetchError {
		t.Fatalf("应 155110, got %v", err)
	}
}
```

- [ ] **Step 4: 全包回归 + spec 偏差表回填**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/indexing/processor/parser/... && gofmt -l internal/agentcore/retrieval/indexing/processor/parser/ ; go vet -tags test ./internal/agentcore/retrieval/indexing/processor/parser/
```

实施偏差回填 spec §8 表格（实施中发现的偏差逐项记录：如 goquery 自动补 body 导致"无 body 分支"不可达、RE2 惰性量词等已预置的偏差确认）。

- [ ] **Step 5: commit**

```bash
git add -A && git commit -m "docs(parser): 13.14 计划状态回填 + doc.go 补三文件 + 覆盖率达标"
```

---

## Self-Review 记录

1. **Spec 覆盖核对**：§2 方案A嵌入链→Task 4/5 构造链；§3 文件布局/类型→Task 1-5；§4 流程→Task 3-5（HTMLFile.Parse/WebPage.ParseURL/downloadHTML/WeChat.parseHTMLToDocs 全覆盖）；§5 错误九处→Task 4/5 测试断言逐一覆盖（#1-#5 在 Task 4，#6-#8 在 Task 5，#9 三分支 Task 4）；§6 测试矩阵→各 Task 用例；§10 验收→Task 6。**缺口**：spec §6.2 "verify 单例断言" 在 Task 2 已含。
2. **占位符扫描**：Task 4 Step 3 的 `parseHTMLToDocs` 代码块含"title 兜底修正"补丁段——为防实施者漏看已内联完整版；Task 6 覆盖率数字"XX.X%"留待实测回填（这是计划内回填点非占位）。
3. **类型一致性**：`parseHTMLToDocs(html, docID, source)` 三参签名 Task 4 定义 Task 5 覆写一致；`validateURLFor(url string) error` 两处一致；`downloadHTML(ctx, url, timeout, userAgent, verify, client)` Task 4 定义、Task 4 测试与 Task 5 别名调用一致；`longBody` 在 html_file_parser_test.go 定义、web_page_parser_test.go 引用（同包可见）。
