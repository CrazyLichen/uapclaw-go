# 13.16 JSON/TxtMd Parser 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 13.16 节——JSONParser/TxtMdParser 两个具体解析器 + 从 auto_file_parser.py 带出的 registry 骨架（parserRegistry + RegisterParser），对齐 Python openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py · txt_md_parser.py · auto_file_parser.py（registry 部分）。

**Architecture:** 13.10 已交付 Parser 五方法接口 + BaseParser 模板适配器（嵌入+构造注入 self+覆写 ParseContent 模式）。本计划新增 3 个源文件：registry.go（带出依赖：map+mutex+RegisterParser first-wins）、json_parser.go、txt_md_parser.go（chardet 统计检测+x/text 解码）。AutoFileParser 类本体留 13.11。TDD：每文件先写失败测试再实现。

**Tech Stack:** Go stdlib（os/json/strings/sync/bytes）+ `github.com/saintfish/chardet v0.0.0-20230101081208-5e3ef4b5456d`（新增，纯 Go 零传递依赖）+ `golang.org/x/net/html/charset` Lookup（已在依赖树，编码名→encoding 映射）+ `golang.org/x/text/encoding`（GBK/GB18030/Big5/Shift_JIS 等）。

**Spec:** `docs/superpowers/specs/2026-10-22-txt-md-json-parser-13.16-design.md`

**关键实测事实（探针已验证，实施时直接引用）：**

- chardet 对 GBK 中文文章返回 `charset="GB-18030" lang="zh"`（带连字符）；对 UTF-8 中文返回 `"UTF-8"`；Big5 长样本返回 `"Big5"`；Shift_JIS 长样本返回 `"Shift_JIS"`
- `html/charset.Lookup("GB-18030")` 返回 nil（不认带连字符名）；`Lookup("gbk")`/`Lookup("big5")`/`Lookup("Shift_JIS")`/`Lookup("windows-1252")` 全部 OK——**必须手写 `GB-18030→simplifiedchinese.GB18030` 归一分支**
- `Lookup("ISO-8859-1")` 返回 windows-1252（WHATWG 别名表）；`Lookup("ascii")` 也返回 windows-1252——纯 ASCII 输入 chardet 可能返回 ISO-8859-1，解码为 windows-1252 与 ASCII 兼容，无乱码
- chardet `DetectBest([]byte{})` 空输入返回 `(nil, nil)`——**r 为 nil 需防御**（err 也为 nil！），此路径回落 utf-8
- chardet 检测置信度：短样本可能误判（16 字节 Big5 误判 ISO-8859-1 conf=46），≥300 字节真实文本 conf=100——测试用例须用长样本

---

### Task 1: 引入 chardet 依赖

**Files:**
- Modify: `go.mod`（require 块 + 领域注释）
- Modify: `go.sum`

- [ ] **Step 1: 检查无残留编译进程（规则 5）**

Run: `pgrep -f 'go (build|test)' || echo "无残留"`
Expected: 无残留（有则先 kill）

- [ ] **Step 2: 拉取依赖**

```bash
export GOPROXY=https://goproxy.cn,direct
GOFLAGS=-mod=mod go get github.com/saintfish/chardet@v0.0.0-20230101081208-5e3ef4b5456d
```

Expected: `go: added github.com/saintfish/chardet ...`

- [ ] **Step 3: go.mod 注释区登记（领域十三条目）**

在 `go.mod` 的领域注释区（`// 领域十二：沙箱` 块之后）追加：

```
//
// 领域十三：检索文档索引
// - github.com/saintfish/chardet         字符编码统计检测（对齐 Python charset-normalizer）
```

- [ ] **Step 4: 验证依赖解析**

Run: `go list -m github.com/saintfish/chardet`
Expected: `github.com/saintfish/chardet v0.0.0-20230101081208-5e3ef4b5456d`（无 `// indirect`——本任务后将被 parser 包 import）

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum
git commit -m "chore(deps): 引入 saintfish/chardet——13.16 TxtMdParser 编码统计检测"
```

---

### Task 2: registry.go（带出依赖）

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/registry.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/registry_test.go`

**Python 源对照：** `auto_file_parser.py` L20-46（`_PARSER_REGISTRY` + `register_parser` 装饰器）

- [ ] **Step 1: 写失败测试**

创建 `registry_test.go`：

```go
package parser

import (
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── 测试 fake ────────────────────────────

// fakeExtParser 注册表测试用假解析器（内容可控）。
type fakeExtParser struct {
	*BaseParser
	// content 预置返回内容
	content string
}

// newFakeExtParser 构造并完成 self 注入。
func newFakeExtParser(content string) *fakeExtParser {
	p := &fakeExtParser{content: content}
	bp, _ := NewBaseParser(p)
	p.BaseParser = bp
	return p
}

// ParseContent 返回预置内容。
func (p *fakeExtParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	return p.content, nil
}

// ──────────────────────────── registry 测试 ────────────────────────────

// TestRegisterParser_注册成功 首次注册可查询到工厂。
func TestRegisterParser_注册成功(t *testing.T) {
	RegisterParser([]string{".gotest1"}, func() Parser { return newFakeExtParser("c1") })
	f, ok := getParserFactory(".gotest1")
	if !ok {
		t.Fatal("expected factory registered for .gotest1")
	}
	if _, isFake := f().(*fakeExtParser); !isFake {
		t.Fatalf("expected *fakeExtParser, got %T", f())
	}
}

// TestRegisterParser_大写扩展名归一 ".JSON" 注册后 ".json" 可查。
func TestRegisterParser_大写扩展名归一(t *testing.T) {
	RegisterParser([]string{".GOTESTUP"}, func() Parser { return newFakeExtParser("up") })
	if _, ok := getParserFactory(".gotestup"); !ok {
		t.Fatal("expected .GOTESTUP normalized to .gotestup")
	}
	if _, ok := getParserFactory(".GOTESTUP"); ok {
		t.Fatal(".GOTESTUP raw key should not exist (normalized only)")
	}
}

// TestRegisterParser_重复扩展名静默跳过 first-wins：第二个工厂不覆盖第一个。
func TestRegisterParser_重复扩展名静默跳过(t *testing.T) {
	RegisterParser([]string{".gotestdup"}, func() Parser { return newFakeExtParser("first") })
	RegisterParser([]string{".gotestdup"}, func() Parser { return newFakeExtParser("second") })
	f, ok := getParserFactory(".gotestdup")
	if !ok {
		t.Fatal("expected factory registered")
	}
	p := f()
	docs, err := p.Parse(t.Context(), "whatever")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Text != "first" {
		t.Fatalf("expected first-wins 'first', got %v", docs)
	}
}

// TestRegisterParser_空扩展名列表 零注册不报错。
func TestRegisterParser_空扩展名列表(t *testing.T) {
	before := len(snapshotRegistry())
	RegisterParser(nil, func() Parser { return newFakeExtParser("x") })
	if after := len(snapshotRegistry()); after != before {
		t.Fatalf("nil list should register nothing: before=%d after=%d", before, after)
	}
}

// TestRegisterParser_并发安全 并发注册不同扩展名 + 并发读，-race 验证。
func TestRegisterParser_并发安全(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		ext := ".con" + string(rune('a'+i))
		go func(ext string) {
			defer wg.Done()
			RegisterParser([]string{ext}, func() Parser { return newFakeExtParser(ext) })
		}(ext)
		go func() {
			defer wg.Done()
			getParserFactory(".json") // 并发读已有键
		}()
	}
	wg.Wait()
}

// TestGetParserFactory_未注册返回false 查询不存在的扩展名。
func TestGetParserFactory_未注册返回false(t *testing.T) {
	if _, ok := getParserFactory(".nonexistent-xyz"); ok {
		t.Fatal("expected not found")
	}
}
```

注意：`snapshotRegistry` 是 registry.go 的辅助函数（Task 2 Step 3 定义），此处引用使测试先行失败。

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test -run 'TestRegisterParser|TestGetParserFactory' ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | head -5`
Expected: 编译失败 `undefined: RegisterParser` / `getParserFactory` / `snapshotRegistry`

- [ ] **Step 3: 实现 registry.go**

```go
package parser

import (
	"context"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────
// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识（对齐 chunker/indexer 层先例）
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// parserRegistry 扩展名 → 解析器工厂。并发访问由 parserRegistryMu 保护
	//（Python 模块级 dict 无并发问题，Go 包级 map 需要互斥——chunker registry 同款先例）。
	//
	// Python: auto_file_parser.py _PARSER_REGISTRY
	parserRegistry = map[string]func() Parser{}
	// parserRegistryMu registry 互斥锁
	parserRegistryMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterParser 按扩展名列表注册解析器工厂。
//
// 扩展名小写归一（".JSON"→".json"）；已存在的扩展名静默跳过（first-wins，
// 字面对齐 Python if normalized_ext not in _PARSER_REGISTRY）。
// 与 chunker registry 的"重名报错"风格不同——Python 两处 registry 语义本就不同，
// 各自忠实对齐。13.11 的 registerNewParser 届时按 last-wins 覆盖语义单独实现。
//
// Python: auto_file_parser.py register_parser 装饰器（Go 无装饰器，
// 各具体解析器文件 init() 调用本函数，import 即触发，语义对齐装饰器 import 时执行）
func RegisterParser(fileExtensions []string, factory func() Parser) {
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	for _, ext := range fileExtensions {
		normalizedExt := strings.ToLower(ext)
		if _, exists := parserRegistry[normalizedExt]; exists {
			continue
		}
		parserRegistry[normalizedExt] = factory
		// 对齐 Python: logger.info(f"Registered parser {parser_class.__name__} for {normalized_ext}")
		logger.Info(logComponent).
			Str("parser_name", parserTypeName(factory())).
			Str("ext", normalizedExt).
			Msg("registered parser")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getParserFactory 按扩展名取工厂。调用方（13.11 AutoFileParser）传入
// 小写归一后的扩展名；未注册返回 false。
//
// Python: auto_file_parser.py parse 中 _PARSER_REGISTRY[file_ext]() 查取
func getParserFactory(fileExt string) (func() Parser, bool) {
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	factory, exists := parserRegistry[fileExt]
	return factory, exists
}

// snapshotRegistry 返回当前注册扩展名列表（测试用）。
func snapshotRegistry() []string {
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	exts := make([]string, 0, len(parserRegistry))
	for k := range parserRegistry {
		exts = append(exts, k)
	}
	return exts
}

// parserTypeName 取工厂产出实例的类型名（日志用）。
// 产出失败返回 "unknown"。
func parserTypeName(p Parser) string {
	if p == nil {
		return "unknown"
	}
	return strings.TrimSpace(strings.SplitN(typeOf(p), ".", 2)[0])
}

// typeOf 抽象 %T（测试替身可覆写；生产恒 reflect）。
// 简化：直接内联使用 fmt，见下方真实实现替换说明。
```

**实现说明（重要）**：`parserTypeName`/`typeOf` 的拆分是过度设计——**直接用 `fmt.Sprintf("%T", factory())` 一行实现**，删除 `parserTypeName`/`typeOf` 两个函数。即注册日志行改为：

```go
logger.Info(logComponent).
	Str("parser_name", fmt.Sprintf("%T", factory())).
	Str("ext", normalizedExt).
	Msg("registered parser")
```

import 增加 `"fmt"`。factory() 每次实例化开销可忽略（对齐 Python `_create_parser_instance()` 惰性调用语义——Python 注册时**不**实例化只记类名；Go 此处为了取类型名实例化一次，行为偏差仅在日志触发时机，注册表内容零偏差。若要字面零实例化，可改为工厂侧传名——但 Python `parser_class.__name__` 也是类对象非实例，Go `func() Parser` 闭包无类型名可取，实例化取名是最低成本近似，归档为已知偏差）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -run 'TestRegisterParser|TestGetParserFactory' -v ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | tail -20`
Expected: 6 用例全 PASS，无 race

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/registry.go internal/agentcore/retrieval/indexing/processor/parser/registry_test.go
git commit -m "feat(parser): 13.16 registry 骨架——RegisterParser first-wins+并发互斥（带出自 auto_file_parser.py）"
```

---

### Task 3: json_parser.go

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/json_parser.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/json_parser_test.go`

**Python 源对照：** `json_parser.py` 全文（39 行）

- [ ] **Step 1: 写失败测试**

创建 `json_parser_test.go`：

```go
package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// TestNewJSONParser 构造成功。
func TestNewJSONParser(t *testing.T) {
	p, err := NewJSONParser()
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

// TestJSONParser_成功解析 JSON 对象缩进两空格且保 Unicode。
func TestJSONParser_成功解析(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "t.json")
	if err := os.WriteFile(path, []byte(`{"name":"测试","value":123}`), 0o600); err != nil {
		t.Fatal(err)
	}
	docs, err := p.Parse(context.Background(), path, WithDocID("doc_1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].ID != "doc_1" {
		t.Fatalf("expected ID doc_1, got %q", docs[0].ID)
	}
	want := "{\n  \"name\": \"测试\",\n  \"value\": 123\n}"
	if docs[0].Text != want {
		t.Fatalf("text mismatch:\n got: %q\nwant: %q", docs[0].Text, want)
	}
}

// TestJSONParser_空对象 输出字面 {}。
func TestJSONParser_空对象(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "e.json")
	os.WriteFile(path, []byte(`{}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Text != "{}" {
		t.Fatalf("expected {}, got %v", docs)
	}
}

// TestJSONParser_数组 数组合法 JSON。
func TestJSONParser_数组(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "a.json")
	os.WriteFile(path, []byte(`[1,2,3,"test"]`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	for _, s := range []string{"1", "2", "test"} {
		if !contains(docs[0].Text, s) {
			t.Fatalf("expected %q in %q", s, docs[0].Text)
		}
	}
}

// TestJSONParser_非法JSON返回原文 格式错误分支返回原始内容。
func TestJSONParser_非法JSON返回原文(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(path, []byte("{ invalid json }"), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Text != "{ invalid json }" {
		t.Fatalf("expected raw content, got %v", docs)
	}
}

// TestJSONParser_HTML字符不转义 SetEscapeHTML(false) 对齐 ensure_ascii=False 输出。
func TestJSONParser_HTML字符不转义(t *testing.T) {
	p, _ := NewJSONParser()
	path := filepath.Join(t.TempDir(), "h.json")
	os.WriteFile(path, []byte(`{"a":"<b>&"}`), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 {
		t.Fatal("expected 1 doc")
	}
	if contains(docs[0].Text, `\u003c`) || contains(docs[0].Text, `&lt;`) {
		t.Fatalf("HTML chars should stay raw: %q", docs[0].Text)
	}
	if !contains(docs[0].Text, "<b>&") {
		t.Fatalf("expected raw <b>& in %q", docs[0].Text)
	}
}

// TestJSONParser_文件不存在软失败 空切片 nil err。
func TestJSONParser_文件不存在软失败(t *testing.T) {
	p, _ := NewJSONParser()
	docs, err := p.Parse(context.Background(), "/nonexistent/path.json")
	if err != nil {
		t.Fatalf("expected nil err (soft fail), got %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected empty docs, got %v", docs)
	}
}

// TestJSONParser_registry注册 init 注册后工厂可实例化解析。
func TestJSONParser_registry注册(t *testing.T) {
	f, ok := getParserFactory(".json")
	if !ok {
		t.Fatal("expected .json registered by init()")
	}
	p := f()
	if _, isJSON := p.(*JSONParser); !isJSON {
		t.Fatalf("expected *JSONParser, got %T", p)
	}
	path := filepath.Join(t.TempDir(), "r.json")
	os.WriteFile(path, []byte(`{"k":"v"}`), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil || len(docs) != 1 {
		t.Fatalf("registry instance parse failed: %v %v", docs, err)
	}
}

// contains 子串包含（避免引入 strings 仅两处使用）。
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringContains(s, sub))
}

// stringContains strings.Contains 转发（见实现文件说明——直接用 strings.Contains 即可，本对函数仅测试占位说明）。
func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

var _ = reranker.Document{}
```

**实现说明**：`contains`/`stringContains` 自造轮子是反模式——**直接 `import "strings"` 用 `strings.Contains`**，删除这两个函数与 `var _ = reranker.Document{}` 行。测试代码最终以 `strings.Contains` 版本为准。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run 'TestNewJSONParser|TestJSONParser' ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | head -5`
Expected: 编译失败 `undefined: NewJSONParser` / `JSONParser`

- [ ] **Step 3: 实现 json_parser.go**

```go
package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JSONParser JSON 文档解析器。
//
// 嵌入 BaseParser 仅覆写 ParseContent（对齐 Python JSONParser 仅覆写 _parse）。
// 软失败语义：读文件/内部失败返回 ("", nil)（对齐 Python except → return None），
// BaseParser.Parse 收空串自然产空切片（13.10 决策 4 子类自治）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py (JSONParser)
type JSONParser struct {
	// BaseParser 默认适配器（Parse/LazyParse/Process/Supports 默认实现）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 JSONParser 实现 Parser
var _ Parser = (*JSONParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJSONParser 构造 JSONParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）；NewBaseParser(self)
// 注入使模板方法动态派发到本类 ParseContent。
//
// Python: json_parser.py JSONParser.__init__
func NewJSONParser() (*JSONParser, error) {
	p := &JSONParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ParseContent 解析 JSON 文件内容。
//
// 对齐 Python _parse：
//  1. 读文件（utf-8 容错，对齐 errors="ignore"）
//  2. json.Unmarshal 失败 → Error 日志 + 返回原始内容（JSONDecodeError 分支）
//  3. 成功 → 两空格缩进序列化（对齐 indent=2），HTML 字符不转义
//     （SetEscapeHTML(false) 对齐 ensure_ascii=False 的原样输出语义）
//  4. 整体失败 → ("", nil) 软失败 + Error 日志
//
// Python: json_parser.py JSONParser._parse
func (p *JSONParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	_ = ctx // 对齐 Python 无 ctx 语义
	_ = llmClient
	raw, err := os.ReadFile(doc)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse JSON {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse JSON")
		return "", nil
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		// 对齐 Python: logger.error(f"JSON format error: {file_path}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Msg("JSON format error")
		return string(raw), nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse JSON")
		return "", nil
	}
	// json.Encoder.Encode 尾部追加 \n，对齐 json.dumps 无尾换行
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册到 registry（对齐 Python @register_parser([".json", ".JSON"]) import 时执行）
func init() {
	RegisterParser([]string{".json", ".JSON"}, func() Parser {
		p, _ := NewJSONParser()
		return p
	})
}
```

**注意**：`json.Encoder.Encode` 默认尾部加 `\n`，Python `json.dumps` 不加——`TrimSuffix` 消除差异（这是 Python→Go 的真实差异点，测试 `TestJSONParser_成功解析` 的精确断言依赖此处理）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -run 'TestNewJSONParser|TestJSONParser' -v ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | tail -12`
Expected: 7 用例全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/json_parser.go internal/agentcore/retrieval/indexing/processor/parser/json_parser_test.go
git commit -m "feat(parser): 13.16 JSONParser——两空格缩进+HTML不转义+软失败"
```

---

### Task 4: txt_md_parser.go

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/parser/txt_md_parser.go`
- Test: `internal/agentcore/retrieval/indexing/processor/parser/txt_md_parser_test.go`

**Python 源对照：** `txt_md_parser.py` 全文（47 行）

- [ ] **Step 1: 写失败测试**

创建 `txt_md_parser_test.go`：

```go
package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// gbkArticle GBK 编码的中文文章字节（长样本保证 chardet conf=100）。
// 内容："中国的传统文化悠久以来，在历史长河中形成了独特的美" × 重复
var gbkArticle = bytesRepeat([]byte{
	0xD6, 0xD0, 0xB9, 0xFA, 0xB5, 0xC4, 0xB4, 0xAB, 0xCD, 0xB3, 0xCE, 0xC4, 0xBB, 0xAF, 0xD3, 0xC6,
	0xBE, 0xC3, 0xC0, 0xB4, 0xA3, 0xAC, 0xD4, 0xDA, 0xC0, 0xFA, 0xCA, 0xB7, 0xB3, 0xA4, 0xBA, 0xD3,
	0xD6, 0xD0, 0xD0, 0xCE, 0xB3, 0xC9, 0xC1, 0xCB, 0xB6, 0xC0, 0xCC, 0xD8, 0xB5, 0xC4, 0xC3, 0xC0,
}, 16)

// big5Article Big5 编码的"這是一個中文測試" × 重复（长样本）。
var big5Article = bytesRepeat([]byte{
	0xB3, 0x6F, 0xAC, 0x4F, 0xA4, 0x40, 0xAD, 0x4D, 0xA4, 0xA4, 0xA4, 0xE5, 0xB4, 0xFA, 0xB8, 0xD5,
}, 20)

// bytesRepeat 复制字节切片 n 次。
func bytesRepeat(b []byte, n int) []byte {
	out := make([]byte, 0, len(b)*n)
	for i := 0; i < n; i++ {
		out = append(out, b...)
	}
	return out
}

// TestNewTxtMdParser 构造成功。
func TestNewTxtMdParser(t *testing.T) {
	p, err := NewTxtMdParser()
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

// TestTxtMdParser_UTF8中文 UTF-8 文件原样解析。
func TestTxtMdParser_UTF8中文(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "u.txt")
	os.WriteFile(path, []byte("你好世界，这是一个中文测试"), 0o600)
	docs, err := p.Parse(context.Background(), path, WithDocID("d1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != "d1" {
		t.Fatalf("expected 1 doc d1, got %v", docs)
	}
	if docs[0].Text != "你好世界，这是一个中文测试" {
		t.Fatalf("got %q", docs[0].Text)
	}
}

// TestTxtMdParser_GBK解码 chardet 核心价值场景：GBK 文件正确解码为中文。
func TestTxtMdParser_GBK解码(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "g.txt")
	os.WriteFile(path, gbkArticle, 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	// GBK 解码后必须包含中文（不能是乱码问号或 U+FFFD）
	if !strings.Contains(docs[0].Text, "中国的传统文化") {
		t.Fatalf("GBK decode failed, got: %q", docs[0].Text[:min(40, len(docs[0].Text))])
	}
}

// TestTxtMdParser_Big5解码 Big5 长样本正确解码。
func TestTxtMdParser_Big5解码(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "b.txt")
	os.WriteFile(path, big5Article, 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if !strings.Contains(docs[0].Text, "這是一個中文測試") {
		t.Fatalf("Big5 decode failed, got: %q", docs[0].Text[:min(40, len(docs[0].Text))])
	}
}

// TestTxtMdParser_纯ASCII ASCII 文件原样解析。
func TestTxtMdParser_纯ASCII(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(path, []byte("plain ascii text"), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 || docs[0].Text != "plain ascii text" {
		t.Fatalf("got %v", docs)
	}
}

// TestTxtMdParser_strip前后空白 对齐 content.strip()。
func TestTxtMdParser_strip前后空白(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "s.txt")
	os.WriteFile(path, []byte("   \n  Content  \n   "), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Text != "Content" {
		t.Fatalf("expected stripped 'Content', got %q", docs[0].Text)
	}
}

// TestTxtMdParser_空文件空切片 对齐 content falsy → None → []。
func TestTxtMdParser_空文件空切片(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "e.txt")
	os.WriteFile(path, []byte(""), 0o600)
	docs, err := p.Parse(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected empty docs, got %v", docs)
	}
}

// TestTxtMdParser_文件不存在软失败 空切片 nil err。
func TestTxtMdParser_文件不存在软失败(t *testing.T) {
	p, _ := NewTxtMdParser()
	docs, err := p.Parse(context.Background(), "/nonexistent/path.txt")
	if err != nil {
		t.Fatalf("expected nil err (soft fail), got %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected empty docs, got %v", docs)
	}
}

// TestTxtMdParser_registry注册 init 注册全部扩展名。
func TestTxtMdParser_registry注册(t *testing.T) {
	for _, ext := range []string{".txt", ".md", ".markdown"} {
		f, ok := getParserFactory(ext)
		if !ok {
			t.Fatalf("expected %s registered by init()", ext)
		}
		p := f()
		if _, isTxt := p.(*TxtMdParser); !isTxt {
			t.Fatalf("expected *TxtMdParser for %s, got %T", ext, p)
		}
	}
}

// min Go 1.21+ 内置 min（若项目 go 版本低于 1.21 用 if 替代）。
var _ = reranker.Document{}
```

**实现说明**：`var _ = reranker.Document{}` 行删除（未实际使用）；`min` 是 Go 1.21+ 内置函数，若项目 go directive 低于 1.21 则测试中 `min(40, len(...))` 改为手写 if。**BOM 用例补充**（spec §4.3 承诺实施时固化）：追加下面的用例（UTF-8 BOM 文件解析后文本不含 BOM 字符）：

```go
// TestTxtMdParser_UTF8BOM剥离 BOM 前缀不残留（对齐 Python strip 兜底）。
func TestTxtMdParser_UTF8BOM剥离(t *testing.T) {
	p, _ := NewTxtMdParser()
	path := filepath.Join(t.TempDir(), "bom.txt")
	os.WriteFile(path, append([]byte{0xEF, 0xBB, 0xBF}, []byte("BOM content")...), 0o600)
	docs, _ := p.Parse(context.Background(), path)
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Text != "BOM content" {
		t.Fatalf("BOM should be stripped, got %q", docs[0].Text)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run 'TestNewTxtMdParser|TestTxtMdParser' ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | head -5`
Expected: 编译失败 `undefined: NewTxtMdParser` / `TxtMdParser`

- [ ] **Step 3: 实现 txt_md_parser.go**

```go
package parser

import (
	"context"
	"os"
	"strings"

	"github.com/saintfish/chardet"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TxtMdParser 文本/Markdown 解析器。
//
// 嵌入 BaseParser 仅覆写 ParseContent。编码统计检测用 chardet
// （对齐 Python charset_normalizer.detect），经 x/net Lookup 映射
// x/text encoding 解码。软失败语义同 JSONParser。
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/txt_md_parser.py (TxtMdParser)
type TxtMdParser struct {
	// BaseParser 默认适配器
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 编译期断言 TxtMdParser 实现 Parser
	_ Parser = (*TxtMdParser)(nil)
	// textDetector 编码检测器（chardet 探测器无状态可复用）
	textDetector = chardet.NewTextDetector()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTxtMdParser 构造 TxtMdParser。
//
// Python: txt_md_parser.py TxtMdParser.__init__
func NewTxtMdParser() (*TxtMdParser, error) {
	p := &TxtMdParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ParseContent 解析 TXT/MD 文件内容。
//
// 对齐 Python _parse：
//  1. 读原始字节
//  2. chardet 统计检测编码；检测失败/空输入 → utf-8（对齐 detected None 分支）
//  3. 编码名映射 x/text encoding 解码（GB-18030 手工归一——htmlindex
//     不认带连字符名，探针实测）；未知编码名 → utf-8 容错解码
//  4. TrimSpace；空串 → ("", nil)（对齐 content.strip() if content else None）
//  5. 整体失败 → ("", nil) 软失败 + Error 日志
//
// Python: txt_md_parser.py TxtMdParser._parse
func (p *TxtMdParser) ParseContent(ctx context.Context, doc string, llmClient *llm.Model) (string, error) {
	_ = ctx
	_ = llmClient
	raw, err := os.ReadFile(doc)
	if err != nil {
		// 对齐 Python: logger.error(f"Failed to parse TXT/MD {file_path}: {e}")
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse TXT/MD")
		return "", nil
	}
	content, err := decodeDetected(raw)
	if err != nil {
		logger.Error(logComponent).
			Str("file_path", doc).
			Err(err).
			Msg("failed to parse TXT/MD")
		return "", nil
	}
	if content == "" {
		return "", nil
	}
	return strings.TrimSpace(content), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decodeDetected 统计检测编码并解码字节流。
//
// chardet 对 GBK 中文返回 "GB-18030"（带连字符，htmlindex 不认，
// 手工映射 simplifiedchinese.GB18030——探针实测）；其余 IANA 名经
// html/charset.Lookup 解析。Lookup 失败/检测失败回落 utf-8 容错解码
//（对齐 Python detect 返回 None 或未知编码名时的 open 回落语义）。
// UTF-8 BOM 前缀剥离（对齐 Python strip 对 \ufeff 的兜底处理）。
func decodeDetected(raw []byte) (string, error) {
	enc := detectEncoding(raw)
	if enc == nil {
		return strings.TrimPrefix(string(raw), "\uFEFF"), nil
	}
	decoded, err := enc.NewDecoder().Bytes(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(string(decoded), "\uFEFF"), nil
}

// detectEncoding 检测字节流编码，返回 x/text encoding（nil 表示 UTF-8）。
//
// chardet.DetectBest 空输入返回 (nil, nil)——result 为 nil 需防御
// （探针实测）。UTF-8 检出时返回 nil 走原样路径（零转换）。
func detectEncoding(raw []byte) encoding.Encoding {
	if len(raw) == 0 {
		return nil
	}
	result, err := textDetector.DetectBest(raw)
	if err != nil || result == nil {
		return nil
	}
	name := strings.TrimSpace(result.Charset)
	switch strings.ToLower(name) {
	case "utf-8":
		return nil // 原样（BOM 由 decodeDetected 剥离）
	case "gb-18030", "gb18030":
		// chardet 返回带连字符名，htmlindex 不认（探针实测），手工映射
		return simplifiedchinese.GB18030
	default:
		e, _ := charset.Lookup(name)
		return e
	}
}

// init 注册到 registry（对齐 Python @register_parser([".txt", ".TXT", ".md", ".MD", ".markdown", ".MARKDOWN"])）
func init() {
	RegisterParser([]string{".txt", ".TXT", ".md", ".MD", ".markdown", ".MARKDOWN"}, func() Parser {
		p, _ := NewTxtMdParser()
		return p
	})
}
```

**注意（探针实测结论）**：chardet 检出 GBK 系编码名是 `GB-18030`；`charset.Lookup("GB-18030")` 返回 nil 但 `Lookup("gbk")`/`Lookup("gb18030")` OK。上面 switch 的 `"gb-18030"` 分支就是为此存在——**不可删除**，否则 GBK 用例必挂。GB18030 是 GBK 超集，GBK 字节流按 GB18030 解码结果正确（探针 4 已验证中文完整还原）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -race -run 'TestNewTxtMdParser|TestTxtMdParser' -v ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | tail -15`
Expected: 10 用例全 PASS（含 GBK/Big5/BOM）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/txt_md_parser.go internal/agentcore/retrieval/indexing/processor/parser/txt_md_parser_test.go
git commit -m "feat(parser): 13.16 TxtMdParser——chardet 统计检测+GB-18030 归一+BOM 剥离"
```

---

### Task 5: doc.go 更新 + 全量验证 + IMPLEMENTATION_PLAN.md 状态同步

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/parser/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md:802`（13.11 行）与 `:807`（13.16 行）

- [ ] **Step 1: 更新 doc.go 文件目录**

`doc.go` 的文件目录段替换为：

```
// 文件目录：
//
//	parser/
//	├── doc.go           # 包文档
//	├── base.go          # Parser 接口 + ParseOptions + BaseParser 模板适配器（13.10）
//	├── registry.go      # parserRegistry + RegisterParser 注册骨架（13.16 带出自 auto_file_parser.py）
//	├── json_parser.go   # JSONParser：JSON 格式化解析（13.16）
//	└── txt_md_parser.go # TxtMdParser：文本/Markdown 编码检测解析（13.16）
```

包概述段"使用方式"追加一行：

```
//   - 文件解析器注册：具体解析器文件 init() 调 RegisterParser 按扩展名注册工厂
//     （对齐 Python @register_parser 装饰器 import 时执行）
```

- [ ] **Step 2: 编译 + 全量测试 + 覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go vet ./internal/agentcore/retrieval/indexing/processor/parser/ && go test -race -cover ./internal/agentcore/retrieval/indexing/processor/parser/ 2>&1 | tail -3`
Expected: vet 干净；覆盖率 ≥88%（13.10 基线 88.6%，新增三文件目标 ≥90%）

Run: `go test -cover ./internal/agentcore/retrieval/... 2>&1 | grep -c '^ok'`
Expected: 9 包全 ok（retrieval 全域无回归）

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

13.16 行（807 附近）：

```
| 13.16 | ✅ | JSON/TxtMd Parser | `JSONParser(嵌入BaseParser覆写ParseContent：utf-8容错读+Unmarshal失败返回原文+两空格缩进序列化SetEscapeHTML(false)对齐ensure_ascii=False+Encoder尾换行TrimSuffix对齐json.dumps+软失败空串) + TxtMdParser(chardet统计检测对齐charset_normalizer+GB-18030手工归一htmlindex不认带连字符名+x/net Lookup映射x/text解码+UTF-8原样路径+BOM剥离+TrimSpace+软失败) + registry骨架带出(parserRegistry map+parserRegistryMu互斥+RegisterParser扩展名小写归一first-wins静默跳过+info注册日志+getParserFactory包内查询；AutoFileParser类本体留13.11⤴️；两解析器init注册.json/.txt/.md/.markdown六扩展名)`。覆盖率：三新文件≥90%。设计：docs/superpowers/specs/2026-10-22-txt-md-json-parser-13.16-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py + txt_md_parser.py + auto_file_parser.py(registry部分)` |
```

13.11 行（802 附近）加 ⤴️ 标注（"registry 骨架已由 13.16 带出"）：

```
| 13.11 | ☐ | AutoParser 路由 | 文件/链接自动识别分发 | `openjiuwen/core/retrieval/indexing/processor/parser/auto_*.py` |
```

改为：

```
| 13.11 | ☐ | AutoParser 路由 | 文件/链接自动识别分发（⤴️ registry 骨架已由 13.16 带出：parserRegistry/RegisterParser；AutoFileParser/AutoLinkParser/AutoParser 三件套本节实现） | `openjiuwen/core/retrieval/indexing/processor/parser/auto_*.py` |
```

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/retrieval/indexing/processor/parser/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 13.16 完成状态同步——doc.go 三条目+IMPLEMENTATION_PLAN 13.16✅/13.11⤴️"
```

---

### Task 6: 终验推送

- [ ] **Step 1: 全域回归**

Run: `go build ./... && go test -cover ./internal/agentcore/retrieval/... 2>&1 | tail -10`
Expected: 构建成功，retrieval 9 包 ok 无回归

- [ ] **Step 2: 更新记忆**

`/root/.codebuddy/projects/home-opensource-uapclaw-gateway/memory/parser-reorder-13.11.md` 的 How to apply 段更新："13.16 已实施完成（chardet 编码检测 + registry 骨架），下一步 13.12 PDFParser"。

- [ ] **Step 3: 推送**

```bash
git push origin main
```

Expected: 5-6 个 commit 推送成功

---

## Self-Review 记录

1. **Spec 覆盖**：spec §2 决策 1-7 → Task 2-4（决策 5 first-wins = Task 2；决策 2 chardet = Task 4；决策 7 软失败 = Task 3/4）；spec §4.1-4.3 类型设计 → Task 2-4 实现块；spec §6 测试用例表 → 三个测试文件（registry 6/JSON 7+1 registry/TxtMd 10 含 BOM 用例——BOM 用例是 spec §4.3 "实施时固化"承诺的兑现）；spec §5 文件布局 → Tasks 2-5；spec §7 同步清单 → Task 5/6。零缺口
2. **占位符扫描**：Task 2 Step 3 与 Task 3 Step 1 各有一处"实现说明"标注反模式修正（parserTypeName 拆分/contains 自造轮子）——这是计划作者对草稿的自纠，最终代码块即修正后版本，非留给执行者的占位
3. **类型一致性**：`RegisterParser([]string, func() Parser)` 在 Task 2 定义、Task 3/4 init() 消费签名一致；`getParserFactory(string) (func() Parser, bool)` Task 2 定义、Task 3/4 测试消费一致；`logComponent` Task 2 定义于 registry.go、Task 3/4 引用（同包共享，chunker 层 char_chunker.go 先例同款）
