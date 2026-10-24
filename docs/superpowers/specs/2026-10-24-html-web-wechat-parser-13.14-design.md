# 13.14 HTML/Web/WeChat Parser 设计

- 日期：2026-10-24
- 状态：设计定稿（brainstorming 四节全确认）
- 前置事实：Parser 层换序实际执行链 13.16✅→13.15✅→13.12✅→13.13✅→**13.14（本节）**→13.11
- Python 源：`openjiuwen/core/retrieval/indexing/processor/parser/html_file_parser.py`（149 行）· `web_page_parser.py`（210 行）· `wechat_article_parser.py`（101 行）

## 1. 目标与流程位置

领域 13 外部文档索引管线的 **URL 入口**：把网页/微信公众号文章（及本地 .html 文件）解析为 Document 供 Chunker→Embedding→VectorStore 入库。13.11 AutoLinkParser 默认路由表 `(WECHAT_MP_URL_PATTERN → WeChatArticleParser, HTTP_URL_PATTERN → WebPageParser)` 两项全部依赖本节产出——本节是 13.11 的直接前置。

调用链（Python 侧）：`knowledge_retrieval_comp.py` → `KnowledgeBase.parse_urls` → `AutoParser/AutoLinkParser`（13.11）→ **本节三解析器** → Document[] → Chunker → VectorStore。

### 1.1 Python 实现盘点（对齐基准）

| 模块 | 关键机制 |
|------|---------|
| `html_file_parser.py` | 模块级共享工具：`_parse_html`（lxml 优先/HTMLParser 回落）、`_extract_title`（og:title → title 标签）、`MAIN_CONTENT_SELECTORS` 11 选择器 + >100 字符门槛、`_find_main_content` 两段式（选择器 → body 内 [article,main,div,section] 树序第一个 >200）、`_text_length`（排除 script/style 的**不改树**近似长度）、`_get_text_from_soup`（decompose script/style → get_text("\n", strip=True) → `[ \t]+`→空格 → `\n\s*\n`→`\n\n`）。**硬错误**范式（155110，非其他解析器的软失败范式）：读不到文件/找不到主内容/正文<50 字符。`HTMLFileParser(TxtMdParser)` 覆写整体 `parse`：`super()._parse` 编码检测读文件 → `_parse_html` classmethod → 单 Document（title/`(无标题)`/source_type=web_page） |
| `web_page_parser.py` | `DEFAULT_USER_AGENT`/`DEFAULT_TIMEOUT=30.0`。`HTTP_URL_PATTERN`（`^https?://\S+` 忽略大小写）、`WECHAT_MP_URL_PATTERN`（`^https?://(?:mp\.weixin\.qq\.com\|.*?\.weixin\.qq\.com)/s\b.*` 忽略大小写）。`_validate_url` 静态方法（非 http / WeChat URL → 155110）。`_download_html`：注入 client or 自建 httpx.AsyncClient(verify/timeout/UA) → `_do_get` → 三层 except（HTTPStatusError 带 status / RequestError / 兜底，均 155110 带 cause）。`parse_url` classmethod：validate → download → `effective_id = doc_id or url or uuid4()` → `_parse_html`（**动态派发**：子类覆写生效）→ 补 source_url + Info 日志。`parse` 从 kwargs 取 timeout/user_agent/verify/client 四覆盖参数。`supports`：http 且非 WeChat。模块尾 `parse_web_page_url = WebPageParser.parse_url` |
| `wechat_article_parser.py` | 复用 html_file_parser 的三个模块级函数。`_extract_js_content`（`div#js_content`）。覆写 `_parse_html`：title → js_content → 缺失/空 → 155110（**无 50 字符门槛只判空**）→ 单 Document（source_type=wechat_article）。覆写 `_validate_url`（非 WeChat URL → 155110）。覆写 `supports`。模块尾 `parse_wechat_article_url` 别名 |

## 2. 总体设计：方案 A 三层继承逐级嵌入

```
BaseParser ← TxtMdParser ← HTMLFileParser ← WebPageParser ← WeChatArticleParser
                     (Go struct 嵌入链，self 经 NewBaseParser 注入最终实例)
```

- **否决方案 B（组合+共享函数包）**：无法表达"子类覆写 `_parse_html` 后 `parse_url` 自动派发到子类版本"（Python 多态核心），模拟需引入函数字段/接口间接层，复杂度反超，且与 13.10 spec"嵌入遮蔽模式"回填点冲突。
- 方案 A 与 Python 继承树一比一同构，审查对照表直接可写；多级嵌入的构造样板是既有模式（13.8/13.10/13.13/13.15）的级数延长。
- `super()._parse(doc)` 的 Go 对应物：`p.TxtMdParser.ParseContent(ctx, doc, nil)`（嵌入显式穿透，替代 Python MRO）。
- **多级链 self 注入**：仅最底层 `NewBaseParser(self)` 一次，self 指最终实例（如 WeChat）。`BaseParser.Parse/LazyParse/Process/Supports` 经 self 派发到最终类型覆写版本。

### 2.1 动态派发载体（决策已定：self 接口派发）

`parse_url` 调 `_parse_html` 与 `parse` 调 `_validate_url` 都需"子类覆写生效"。Go 嵌入无运行时多态（`*WebPageParser` 值上的方法调用静态绑定）。解法（与 BaseParser 同构）：本包新增非导出接口

```go
// htmlParseDispatcher _parse_html/_validate_url 覆写动态派发接口（仅本包实现）
type htmlParseDispatcher interface {
    // parseHTMLToDocs 对齐 Python _parse_html classmethod
    parseHTMLToDocs(html string, docID string, source string) ([]reranker.Document, error)
    // validateURLFor 对齐 Python _validate_url 静态方法
    validateURLFor(url string) error
}
```

`WebPageParser` 构造时注入完整实例引用 `dispatch htmlParseDispatcher`（默认为自身）；`WeChatArticleParser` 构造时覆写注入自身（覆写版本生效）。ParseURL 内经 `p.dispatch.parseHTMLToDocs(...)` / `p.dispatch.validateURLFor(...)` 派发。

- 每级构造函数显式逐级初始化嵌入字段（Go 无 MRO 自动链）。
- self（BaseParser 侧）与 dispatch（WebPage 侧）双轨并存，互不干扰：self 派发 Parser 五方法，dispatch 派发 _parse_html/_validate_url 两个内部钩子。

## 3. 文件布局与类型定义

```
parser/
├── html_file_parser.go    # parseHTML/extractTitle/findMainContent/textLength/getTextFromSoup
│                          #   + HTMLFileParser（嵌入 *TxtMdParser，覆写整体 Parse）+ init 注册
├── web_page_parser.go     # WebPageParser（嵌入 *HTMLFileParser）+ URL 正则 + verify sum type
│                          #   + downloadHTML + ParseURL + ParseWebPageURL 包级别名函数
└── wechat_article_parser.go # WeChatArticleParser（嵌入 *WebPageParser）+ extractJSContent
│                          #   + ParseWeChatArticleURL 包级别名函数
```

doc.go 补三文件条目。

### 3.1 ParseOptions 扩展（决策 B：结构化字段）

```go
type ParseOptions struct {
    DocID string
    LLMClient *llm.Model
    Extra map[string]any
    Timeout time.Duration   // Python timeout float 秒；零值=未设
    UserAgent string        // 零值=未设
    Verify verifyMode       // nil=未设（回落实例字段）
    HTTPClient *http.Client // 注入即生效（verify/timeout/UA 失效，client 自身配置生效）
}
```

`WithTimeout/WithUserAgent/WithVerify/WithHTTPClient` 四构造器。语义对齐 Python parse kwargs 四键：未设回落实例字段（WebPageParser 构造时的实例字段值，其自身缺省 DEFAULT_TIMEOUT/DEFAULT_USER_AGENT/verifyDefault）。

**Timeout 形态**：构造器 `NewWebPageParser(timeout float64, ...)` 收 **float64 秒**（字面贴近 Python `timeout: float = DEFAULT_TIMEOUT`），内部转 `time.Duration(timeout * float64(time.Second))`；ParseOptions.Timeout 为 time.Duration（零值未设）。Python timeout 只作用于**自建 client**（注入 client 时忽略）——Go 侧同：HTTPClient 非 nil 时不套 Timeout。

### 3.2 verify 密封 sum type（决策 C，对齐 13.13 cellValue 先例）

```go
// verifyMode TLS 校验模式密封接口（对齐 Python verify: bool | str | SSLContext）
type verifyMode interface{ isVerifyMode() }

// verifyDefault Python verify=True：系统默认 CA
type verifyDefault struct{}
// verifySkip Python verify=False：跳过校验（企业 TLS 检查场景）
type verifySkip struct{}
// verifyCAPath Python verify=str：CA bundle 文件路径
type verifyCAPath struct{ path string }
// verifyTLSConfig Python verify=ssl.SSLContext：自定义 TLS 配置
type verifyTLSConfig struct{ cfg *tls.Config }

var verifyDefaultInstance = verifyDefault{} // 单例（对齐 Python True 字面量）
```

密封机制：非导出标记方法 `isVerifyMode()`，包外无法新增变体（13.13 先例）。导出构造：`NewVerifyDefault()/NewVerifySkip()/NewVerifyCAPath(path)/NewVerifyTLSConfig(cfg)`（返回 verifyMode）。

### 3.3 结构体链

```go
// HTMLFileParser 本地 .htm/.html 文件解析（对齐 Python HTMLFileParser(TxtMdParser)）
type HTMLFileParser struct {
    *TxtMdParser
}

// WebPageParser 通用网页 URL 解析
type WebPageParser struct {
    *HTMLFileParser
    timeout time.Duration
    userAgent string
    verify verifyMode
    dispatch htmlParseDispatcher // 动态派发钩子（构造注入）
}

// WeChatArticleParser 微信公众号文章解析
type WeChatArticleParser struct {
    *WebPageParser
}
```

构造函数：`NewHTMLFileParser()` / `NewWebPageParser(timeout float64, userAgent string, verify verifyMode) (*WebPageParser, error)`（零参时传 DEFAULT_TIMEOUT/DEFAULT_USER_AGENT/verifyDefault）→ `NewWebPageParserWithDefaults()` 便捷构造 / `NewWeChatArticleParser() (*WeChatArticleParser, error)`。

**多级嵌入构造次序**（WeChat 为例）：
```go
func NewWeChatArticleParser() (*WeChatArticleParser, error) {
    p := &WeChatArticleParser{}
    wpp := &WebPageParser{
        timeout: defaultTimeout, userAgent: defaultUserAgent,
        verify: verifyDefaultInstance,
        dispatch: p, // 最终实例注入（覆写版本生效）
    }
    p.WebPageParser = wpp
    wpp.HTMLFileParser = &HTMLFileParser{} // 嵌入逐级显式赋值
    // TxtMdParser 已由 13.16 构造链就绪：NewHTMLFileParser 内部完成 BaseParser(self) 注入
    return p, nil
}
```

（实际实现中 BaseParser 的 self 注入在最底层 NewTxtMdParser 链完成——13.16 的 NewTxtMdParser 构造 NewBaseParser(p) 指向 TxtMd 实例，**多级链上 self 需指向最终实例**，故本节构造链重排：HTMLFile/WebPage/WeChat 构造时重建 TxtMd 层并注入最终 self。细节实施时按"self 始终指最终实例"原则排布，spec 不锁定具体语句级样板。）

### 3.4 URL 正则（Python 字面复制）

```go
var httpURLPattern = regexp.MustCompile(`^https?://\S+`)        // IGNORECASE
var wechatMPURLPattern = regexp.MustCompile(
    `^https?://(?:mp\.weixin\.qq\.com|.*?\.weixin\.qq\.com)/s\b.*`) // IGNORECASE
```

Go regexp 原生大小写敏感，需 `(?i)` 前缀对齐 IGNORECASE：`(?i)^https?://\S+`。`\b` Go regexp 支持。`.*?` 惰性量词 Go regexp 不支持（RE2 无回溯）——模式尾部 `.*` 与 `.*?` 在此处等价（无后续锚点），按 `.*` 实现并在注释标注偏差。

### 3.5 常量

```go
const (
    defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
    defaultTimeoutSeconds = 30.0
    // mainContentSelectors 对齐 MAIN_CONTENT_SELECTORS 11 选择器（字面复制）
    // htmlMinTextLength = 100 / htmlFallbackTextLength = 200 / htmlMinArticleLength = 50
)
```

## 4. 核心流程对齐

### 4.1 共享工具函数（html_file_parser.go，对齐 Python 模块级函数）

| Go 函数 | Python 对应 | 语义要点 |
|---------|------------|---------|
| `parseHTML(html string) *goquery.Document` | `_parse_html` | goquery.NewDocumentFromReader；解析失败返回空文档（对齐 web_tools 先例降级） |
| `extractTitle(soup) string` | `_extract_title` | `soup.Find(`meta[property="og:title"]`)` 取 content 属性 TrimSpace → 回落 `Find("title")` 文本 TrimSpace → 空串。og:title content 空串也回落（Python `meta.get("content")` falsy 判断） |
| `findMainContent(soup) *goquery.Selection` | `_find_main_content` | 两段式：①11 选择器逐一 Find，`textLength(sel) > 100` 命中即返 ②`body.Find("article,main,div,section")` **树序**第一个 >200 ③body 不存在 → nil。树序=文档序：goquery Each 按文档序遍历，嵌套时外层先到（Python find_all 同序） |
| `textLength(sel) int` | `_text_length` | **不改树**的近似长度：遍历文本节点（排除 parent 为 script/style），累加 strip 后长度。实现：递归 Contents() 遍历，文本节点 TrimSpace 长度累加；script/style 子树跳过 |
| `getTextFromSoup(sel) string` | `_get_text_from_soup` | ①**先移除** script/style（对齐 decompose，Find("script").Remove()）②逐文本节点收集 TrimSpace，"\n" 拼接（对齐 get_text(separator="\n", strip=True)）③`[ \t]+`→单空格 ④`\n\s*\n`→`\n\n` ⑤TrimSpace。正则用 regexp2（D1' 先例，`\s` 语义对齐 Python） |

**getTextFromSoup 文本节点遍历实现**：goquery 无 get_text 等价物，须自实现：`sel.Contents()` 递归 + `sel.Find("*")` 的 Contents 收集所有文本节点，按文档序拼接。实施时以 goquery 遍历 API 实测为准（`Each` 文档序保证）。

### 4.2 HTMLFileParser.Parse（覆写整体，对齐 Python L143-150）

```
1. html, _ := p.TxtMdParser.ParseContent(ctx, doc, nil)   // super()._parse：编码检测读文件
   （TxtMdParser 软失败恒返回 ("", nil)，无 err 路径——对齐 Python _parse except return None；
    空串 → 155110 "Could not read HTML file or file is empty (source=%s)"）
2. soup := parseHTML(html)
3. title := extractTitle(soup)
4. contentNode := findMainContent(soup)
   nil → 155110 "Could not find main content in HTML (source=%s)"
5. text := getTextFromSoup(contentNode)
6. len(text) == 0 || len(text) < 50 → 155110 "Article content too short or empty after parsing html (source=%s)"
   （Python `not text or len(text) < 50`——text 已 TrimSpace，not text 与 len<50 前者包含后者，两条件字面保留）
7. 单 Document{ID: docID(=doc), Text: text, Metadata: {"title": title or "(无标题)", "source_type": "web_page"}}
   （docID 取 opts.DocID，空则 doc——对齐 Python parse(doc_id=doc_id or doc)）
```

### 4.3 WebPageParser（对齐 web_page_parser.py）

**Parse**（覆写整体，对齐 L189-201）：
```
timeout := opts.Timeout（零值回退 p.timeout）
userAgent := opts.UserAgent（空回退 p.userAgent）
verify := opts.Verify（nil 回退 p.verify）
client := opts.HTTPClient
return p.ParseURL(ctx, doc, opts.DocID 或 doc, timeout, userAgent, verify, client)
```

**ParseURL**（公开方法，对齐 classmethod parse_url L143-187）：
```
1. err := p.dispatch.validateURLFor(url)   // 动态派发（WeChat 覆写生效）
2. html, err := downloadHTML(ctx, url, timeout, userAgent, verify, client)
3. effectiveID := docID；空则 url；url 也空则 uuid.NewString()   // Python doc_id or url or uuid4
4. docs, err := p.dispatch.parseHTMLToDocs(html, effectiveID, url)   // 动态派发（WeChat 覆写生效）
5. 每个 doc：metadata["source_url"] = url
   logger.Info(logComponent).Str("url", url).Str("title", title兜底).Msg("parsed web page")
6. return docs
```

**validateURLFor**（WebPage 默认实现，对齐 _validate_url L57-74）：
```
url = TrimSpace(url)
非 httpURLPattern 命中 → 155110 "Not a valid HTTP URL: %q"（Python repr → Go %q）
isWeChatArticleURL(url) → 155110 "Use WeChatArticleParser for WeChat URLs"
```

**downloadHTML**（对齐 _download_html L82-141）：
```
requestHeaders := {"User-Agent": userAgent}
注入 client（非 nil）→ doGet(ctx, url, client, requestHeaders)
自建 → 按三 verify 变体配 tls.Config：
    verifyDefault → transport 零配置（nil TLSClientConfig）
    verifySkip → InsecureSkipVerify: true
    verifyCAPath → x509 读文件 append RootCAs（失败走兜底错误分支 155110）
    verifyTLSConfig → transport.TLSClientConfig = cfg
  + client.Timeout = timeout
doGet → GET（带 UA header）→ 错误判定次序（对齐 Python except 捕获序）：
    ① Do 返回 err 非 nil（网络/超时/DNS——RequestError 对应物）→ 155110 "Web page fetch failed for %s: %v"（url, err）
    ② resp.StatusCode < 200 || >= 300（HTTPStatusError 对应物——raise_for_status 语义）→ 155110 "Web page request failed: %d for %s"（code, url）
    ③ io.ReadAll 失败等其余异常（兜底 Exception 分支）→ 155110 "Web page fetch failed for %s: %v"（同 ① 格式，Python 兜底分支与 RequestError 同文案）
成功 → io.ReadAll → string
```

**注**：Python ①是 HTTPStatusError ②是 RequestError ③兜底（捕获序），Go 侧因 `http.Client.Do` 把网络错误走 error 返回、状态码走 resp.StatusCode 显式判，天然次序为网络先状态码后——两分支文案各自对齐，捕获序差异无语义影响（三类互斥）。

**WebPage 的 parseHTMLToDocs 默认实现**（对齐 HTMLFileParser._parse_html classmethod L96-141）：
```
soup := parseHTML(html)
title := extractTitle(soup)
contentNode := findMainContent(soup)
nil → 155110 "Could not find main content in HTML (source=%s)"
text := getTextFromSoup(contentNode)
len(text) == 0 || len(text) < 50 → 155110 "Article content too short or empty after parsing html (source=%s)"
单 Document{ID: docID, Text: text, Metadata: {"title": title or "(无标题)", "source_type": "web_page"}}
```

**Supports**（对齐 L203-207）：`httpURLPattern 命中(doc strip 后) && !isWeChatArticleURL(doc)`。

### 4.4 WeChatArticleParser（对齐 wechat_article_parser.py）

- **覆写 parseHTMLToDocs**（对齐 _parse_html L49-94）：
```
soup := parseHTML(html)
title := extractTitle(soup)
contentNode := extractJSContent(soup)   // soup.Find("div#js_content")，找不到 nil
nil → 155110 "Could not find article content (js_content) in page (source: %s)"
text := getTextFromSoup(contentNode)
空 → 155110 "Article content is empty after parsing (source: %s)"
   （无 50 字符门槛，只判空——Python L79-83 字面）
单 Document{ID: docID, Text: text, Metadata: {"title": title or "(无标题)", "source_type": "wechat_article"}}
```
- **覆写 validateURLFor**（对齐 _validate_url L40-47）：`!isWeChatArticleURL(TrimSpace(url)) → 155110 "Not a WeChat article URL: %q"`
- **覆写 Supports**（对齐 L96-98）：`isWeChatArticleURL(doc)`
- **不覆写 Parse**：继承 WebPage.Parse → ParseURL 链（dispatch 注入自身后 validateURLFor/parseHTMLToDocs 覆写版本生效）
- `extractJSContent` 包级函数（对齐 _extract_js_content）：`soup.Find(`div#js_content`)`

### 4.5 init 注册与别名函数

```go
// html_file_parser.go init（对齐 @register_parser([".htm", ".HTM", ".html", ".HTML"])）
func init() {
    RegisterParser([]string{".htm", ".HTM", ".html", ".HTML"}, func() Parser {
        p, _ := NewHTMLFileParser()
        return p
    })
}
```

web_page_parser.go / wechat_article_parser.go 模块尾（对齐 Python 模块级别名赋值）：
```go
// ParseWebPageURL 包级别名（对齐 parse_web_page_url = WebPageParser.parse_url）
func ParseWebPageURL(ctx context.Context, url string, docID string, opts ...ParseOption) ([]reranker.Document, error) {
    p, _ := NewWebPageParserWithDefaults()
    return p.ParseURL(ctx, url, docID, ...)
}
// ParseWeChatArticleURL 同款（对齐 parse_wechat_article_url）
```

别名函数的参数形态：ParseURL 签名收 `(ctx, url, docID, timeout, userAgent, verify, client)`——Python classmethod 带 keyword-only 四参。Go 侧取参数结构化路线（决策 B），别名函数经 ParseOption 转发（`WithTimeout(...)` 等），保持调用方体验。

## 5. 错误码与日志

### 5.1 155110 首次启用

`exception.StatusRetrievalIndexingFetchError`（codes_context.go L102 已定义）首次投入使用。九处 error_msg 逐字符对齐（BuildError + WithMsg）：

| # | 场景 | Python error_msg | Go WithMsg |
|---|------|-----------------|-----------|
| 1 | WebPage validate 非http | `f"Not a valid HTTP URL: {url!r}"` | `"Not a valid HTTP URL: " + fmt.Sprintf("%q", url)` |
| 2 | WebPage validate 微信URL | `"Use WeChatArticleParser for WeChat URLs"` | 同字面 |
| 3 | HTMLFile 读文件空 | `f"Could not read HTML file or file is empty (source={doc})"` | 同 f-string 形态 |
| 4 | 主内容缺失 | `f"Could not find main content in HTML (source={source})"` | 同 |
| 5 | 正文过短 | `f"Article content too short or empty after parsing html (source={source})"` | 同 |
| 6 | WeChat js_content 缺失 | `f"Could not find article content (js_content) in page (source: {source})"` | 同（注意冒号后空格与前四处的 = 不同） |
| 7 | WeChat 正文空 | `f"Article content is empty after parsing (source: {source})"` | 同 |
| 8 | WeChat validate 非微信 | `f"Not a WeChat article URL: {url!r}"` | `%q` |
| 9 | download 三分支 | `f"Web page request failed: {status} for {url}"` / `f"Web page fetch failed for {url}: {e}"`（两格式） | 同 |

### 5.2 日志

- **Info 一条**（parse_url 尾部）：Python `"Parsed web page: url=%s title=%s"`（title 兜底 `(无标题)`）→ Go `logger.Info(logComponent).Str("url", url).Str("title", titleVal).Msg("parsed web page")`。**Msg 英文**（日志文案英文规则）；**title 数据值保留中文 `(无标题)`**（metadata 兜底值是数据契约非文案——Python 原字面，品牌字符串映射不适用，这不是品牌标识）
- **Error 日志**：HTMLFile.Parse 第 1 步 super 链路触发 TxtMdParser 既有 Error 日志（读文件失败）；本节新增错误均以 155110 error 返回值表达（Python 对应处无 logger 调用，不补）
- **Debug**：AutoLinkParser 的 delegating 日志属 13.11，本节不做

## 6. 测试策略

### 6.1 测试文件

`html_file_parser_test.go` / `web_page_parser_test.go` / `wechat_article_parser_test.go` 三件。

### 6.2 用例矩阵

**共享工具**：
- extractTitle：og:title 命中（含 content 前后空格 Trim）/ og:title content 空 → 回落 title 标签 / title 缺失 → 空串
- findMainContent：11 选择器逐一命中（>100 门槛）/ 选择器命中但文本 ≤100 跳过继续 / 全部不中走 fallback / fallback body 内 article/main/div/section 树序第一个 >200 / body 无 → nil / 嵌套块外层先到（树序）
- textLength：script/style 内容不计入 / 普通 div 计入 / 空节点 0
- getTextFromSoup：script/style 内容不出现在产出 / 多文本节点 "\n" 拼接 / `[ \t]+` 压缩 / `\n\s*\n` 压缩 / 首尾 TrimSpace
- parseHTML：正常 HTML / 空 HTML / 非法 HTML 降级空文档

**HTMLFileParser**：
- 正常 .html 文件（UTF-8）→ 单 Document title/source_type/正文
- GBK 编码 .html 文件 → 编码检测链路触发（TxtMdParser super 链）
- 空文件 → 155110 "Could not read HTML file..."
- 无主内容 → 155110
- 正文 <50 字符 → 155110
- DocID 空时回落 doc 路径

**WebPageParser**：
- Supports：http URL true / https true / ftp false / 空串 false / WeChat URL false
- validateURL：非 http → 155110 格式 / WeChat URL → 155110 / 正常通过
- downloadHTML 注入 client：直接用 client（UA header 仍注入）
- downloadHTML 自建：verifyDefault 走系统 CA / verifySkip InsecureSkipVerify / verifyCAPath 读文件（临时 CA 文件 fixture）/ verifyTLSConfig 直用
- downloadHTML 三分支错误：httptest 返回 404/500 → "Web page request failed: %d for %s"；server 关闭 → "Web page fetch failed for %s: %v"
- Timeout 传递：自建 client 时 Timeout 生效（httptest 慢响应 + 短 timeout 断言超时错误）
- ParseURL 全链：effectiveID 三分支（docID 优先 / url / uuid 兜底）/ metadata source_url / Info 日志（Assert 日志断言方式与既有测试一致）
- Parse 覆盖参数：opts.Timeout/UserAgent/Verify/HTTPClient 四键覆盖实例字段 / 零值回落实例字段

**WeChatArticleParser**：
- parseHTMLToDocs：js_content 命中 → source_type=wechat_article / js_content 缺失 → 155110 / js_content 空 → 155110 / **无 50 字符门槛**（20 字符正文成功——对照 WebPage 同长度会 155110）
- validateURLFor：WeChat URL 通过 / 非 WeChat → 155110 "Not a WeChat article URL: %q"
- Supports：mp.weixin.qq.com/s/xxx true / 普通 URL false
- 继承链：WeChat.Parse 走 WebPage.Parse→ParseURL→dispatch（覆写生效）——用 httptest 模拟微信页面 URL（Server 绑定 127.0.0.1 非 mp.weixin.qq.com——**validateURLFor 先拦截**，此用例仅测 dispatch 路径时需绕过 URL 校验直接调 parseHTMLToDocs；完整 Parse 链测试用 ParseURL 直调 + 手工构造微信正则命中的 URL 但 httptest 无法绑定 mp.weixin.qq.com 域名——**解析**：完整链测试改为直接调 p.Parse(传入本地 httptest URL) 验证 dispatch 生效（URL 非 WeChat 但 dispatch.parseHTMLToDocs 已是 WeChat 版本，页面含 js_content 即可成功），或 ParseURL 直接喂 html。实施时按可测性调整，spec 记录此约束）
- verify 单例：NewVerifyDefault() 两次返回值相等（对齐 Python True 字面量语义，测试可断言接口值相等）

### 6.3 覆盖率目标

parser 包当前 87.7%（13.13 后基线）。本节新增约 900 行生产代码（三文件+ParseOptions 扩展）+ 约 1400 行测试。目标 ≥ 89%。

### 6.4 build tag

零 build tag。全部可 mock：httptest 本地服务 / t.TempDir 文件 / verify fixture 临时证书文件。符合规范 3.3 可 mock 禁 tag。

### 6.5 测试坑预判（借鉴 13.12/13.13 经验）

- goquery 文本节点遍历 API（Contents/Nodes）语义需实测——getTextFromSoup 的 "\n" 拼接顺序
- httptest.NewTLSServer 证书域名不匹配问题：verifyDefault 走系统 CA 会失败（本地自签证书不在系统 CA）——verifySkip 或 InsecureSkipVerify transport 测 TLS 路径
- uuid 兜底分支断言：docID/url 双空时 ID 非空且非 url
- `(无标题)` 中文数据值在日志与 metadata 双处出现——断言各自准确
- Info 日志断言方式：对照既有 parser 测试的日志断言先例（若无可跳过日志断言，以行为断言为主）

## 7. 依赖与循环依赖

- 新增 import：`net/http`、`crypto/tls`、`crypto/x509`、`time`、`io`、`regexp`（或 regexp2）、`github.com/google/uuid`（已在树）、`github.com/PuerkitoBio/goquery`（已在树 v1.12.0）、`regexp2`（已在树）。**零新增 go.mod 依赖**。
- 依赖方向：parser → llm/reranker/logger/exception/goquery（既有方向不变）。parser 不 import harness/web_tools（两域独立，Python 两处也是独立实现——13.14 与 web_tools 的 fetch/html 语义相近但分层不同，**不合并**）。
- 循环依赖：parser 包内 html_file→web_page→wechat 顺序依赖（文件级 import 同包无依赖问题），无新增跨包环。

## 8. 实施偏差记录（实施时回填）

| # | 偏差 | 原因 | 处理 |
|---|------|------|------|
| | | | |

## 9. 回填点

- 本节无 ⤵️ 新增回填点
- 13.10 spec 的四个回填点（Extra 消费 file_name/DocID 透传/Supports 覆写/嵌入遮蔽模式）属 13.11 范围，本节 ParseOptions 结构化扩展（Timeout/UserAgent/Verify/HTTPClient）不动 Extra 口径
- 13.11 AutoLinkParser 将消费本节产出：WECHAT_MP_URL_PATTERN（wechatMPURLPattern）、WebPageParser/WeChatArticleParser 实例、HTTP_URL_PATTERN

## 10. 验收标准

1. 三解析器 + 共享工具 + verify sum type + ParseOptions 扩展全部落地，编译零错误
2. IMPLEMENTATION_PLAN.md 13.14 行打 ✅（含产出摘要）
3. doc.go 补三文件条目
4. 覆盖率 ≥ 89%（parser 包）
5. 九处 155110 error_msg 逐字符对齐 Python（测试断言）
6. gofmt/go vet 零告警
