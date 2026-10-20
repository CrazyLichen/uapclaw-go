# 13.9 分块工具（TextChunker + text_preprocessor）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 13.9 的两个剩余文件——text_preprocessor.go（4 类文本清洗组件）+ text_chunker.go（TextChunker SDK 门面），对齐 Python chunking.py + text_preprocessor.py，Chunker 层收尾。

**Architecture:** TextChunker 嵌入 BaseChunker，完全覆写 ChunkDocuments（预处理管线 → inner.ChunkText → mergeChunkMetadata 复用），ChunkText 返回显式错误（A1），Process 局部覆写转发（13.8 模式）。正则用项目既有 dlclark/regexp2 v1.12.0 + 显式 Python 语义集（D1'：`\w`→`[\p{L}\p{Nd}\p{Nl}\p{No}_]`、`\s`→`[\s\x{1c}-\x{1f}]`），`\b` 保留原生（偏差 4 标注）。

**Tech Stack:** Go 1.24 / regexp2 v1.12.0（已在 go.mod）/ tiktoken-go（13.7 既有）/ testify。

**设计文档:** `docs/superpowers/specs/2026-10-20-textchunker-13.9-design.md`（commit 3a8d5e2a）——决策清单 §2、偏差清单 §7、对拍基准 §4.2/§8。

**Python 源:** `/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/chunker/chunking.py` + `text_preprocessor.py`

**编译约束（项目规则 5）:** 每次 go build/test 前先 `pgrep -f 'go (build|test)'` 检查残留进程；设置 `export GOPROXY=https://goproxy.cn,direct`；禁止并发编译。

---

## 文件结构

```
internal/agentcore/retrieval/indexing/processor/chunker/
├── doc.go                    # 修改：文件目录补 2 个新文件条目
├── text_preprocessor.go      # ★ 新建：TextPreprocessor 接口 + CharReplacement + 3 预处理器 + Pipeline + 语义集 + 6 正则
├── text_preprocessor_test.go # ★ 新建：对齐 Python 38 用例（None 并入空串）
├── text_chunker.go           # ★ 新建：TokenizerProvider + PreprocessOptions + TextChunker 门面 + getChunker 路由
└── text_chunker_test.go      # ★ 新建：对齐 Python 12 用例 + Go 专项
```

既有 8 文件零改动（base.go/registry.go/char_chunker.go/tokenizer_chunker.go/hybrid_chunker.go/text_splitter.go 及测试）。

---

### Task 1: text_preprocessor.go——接口 + WhitespaceNormalizer

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor_test.go`

- [ ] **Step 1: 写失败测试（接口 + WhitespaceNormalizer）**

```go
package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── WhitespaceNormalizer ────────────────────────────

// TestWhitespaceNormalizer_正常文本 对齐 Python test_process_normal_text
func TestWhitespaceNormalizer_正常文本(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This is a test"))
}

// TestWhitespaceNormalizer_多空格 对齐 Python test_process_multiple_spaces
func TestWhitespaceNormalizer_多空格(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This   is    a     test"))
}

// TestWhitespaceNormalizer_换行 对齐 Python test_process_newlines
func TestWhitespaceNormalizer_换行(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This\nis\na\ntest"))
}

// TestWhitespaceNormalizer_制表符 对齐 Python test_process_tabs
func TestWhitespaceNormalizer_制表符(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This\tis\ta\ttest"))
}

// TestWhitespaceNormalizer_混合空白 对齐 Python test_process_mixed_whitespace
func TestWhitespaceNormalizer_混合空白(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This  \n\t  is  \n\t  a  \n\t  test"))
}

// TestWhitespaceNormalizer_两端空白 对齐 Python test_process_leading_trailing_whitespace
func TestWhitespaceNormalizer_两端空白(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("   This is a test   "))
}

// TestWhitespaceNormalizer_空文本 对齐 Python test_process_empty_string + test_process_none（并入）
func TestWhitespaceNormalizer_空文本(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "", n.Process(""))
}

// TestWhitespaceNormalizer_全角空格归一 Go 专项：U+3000 在 Python \s 集内（对拍基准）
func TestWhitespaceNormalizer_全角空格归一(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "This is a test", n.Process("This　is a test"))
}

// TestWhitespaceNormalizer_补偿集命中 Go 专项：\x1c 在 Python \s 集内（.NET \s 缺）
func TestWhitespaceNormalizer_补偿集命中(t *testing.T) {
	n := WhitespaceNormalizer{}
	assert.Equal(t, "a b", n.Process("a\x1cb"))
}
```

- [ ] **Step 2: 运行测试确认编译失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestWhitespaceNormalizer -v 2>&1 | head -5
```

预期：编译失败 `undefined: WhitespaceNormalizer`

- [ ] **Step 3: 实现 text_preprocessor.go（第一部分）**

```go
package chunker

import (
	"strings"

	"github.com/dlclark/regexp2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextPreprocessor 文本预处理器抽象接口。
//
// Python __call__ 语法糖由 Go 方法调用天然替代，不单列。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/text_preprocessor.py (TextPreprocessor)
type TextPreprocessor interface {
	// Process 处理输入文本并返回清洗结果。
	Process(text string) string
}

// CharReplacement 单字符替换规则。
//
// 对齐 Python chars_to_replace dict 单项。有序切片替代 dict：
// Python dict 按插入序迭代，链式替换（a→b, b→c 需 a 变 c）依赖顺序。
type CharReplacement struct {
	// Old 被替换字符
	Old string
	// New 替换为的字符
	New string
}

// WhitespaceNormalizer 空白归一化器：连续空白序列 → 单空格 + 两端去除。
//
// Python: text_preprocessor.py (WhitespaceNormalizer)
type WhitespaceNormalizer struct{}

// URLEmailRemover URL/邮箱移除器。
//
// Python: text_preprocessor.py (URLEmailRemover)
type URLEmailRemover struct {
	// RemoveURLs 是否移除 URL（默认 true 语义由构造方保证）
	RemoveURLs bool
	// RemoveEmails 是否移除邮箱
	RemoveEmails bool
	// Replacement 替换字符串（默认 ""）
	Replacement string
}

// SpecialCharacterNormalizer 特殊字符归一化器。
//
// Python: text_preprocessor.py (SpecialCharacterNormalizer)
type SpecialCharacterNormalizer struct {
	// CharsToRemove 待移除字符集（字符串形式，空串表示不移除）
	CharsToRemove string
	// CharsToReplace 有序替换规则（nil 表示无替换）
	CharsToReplace []CharReplacement
}

// PreprocessingPipeline 预处理管线：顺序应用预处理器列表。
//
// Python: text_preprocessor.py (PreprocessingPipeline)
type PreprocessingPipeline struct {
	// preprocessors 按序应用的预处理器
	preprocessors []TextPreprocessor
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// pyWordClassStr Python \w 精确集。
	//
	// Python \w = [a-zA-Z0-9_] + Unicode 词字符，实测含 No/Nl 类
	//（①②③/½/Ⅷ 是词字符）；regexp2(.NET) \w 含 Mn/Pc 但缺 No/Nl——
	// 若用原生 \w，"①②③序号" 会被 REDUNDANT_SYMBOLS 误删（Python 保留）。
	// 显式集消除该差异（D1' 决策）。
	pyWordClassStr = `[\p{L}\p{Nd}\p{Nl}\p{No}_]`
	// pySpaceClassStr Python \s 补偿集。
	//
	// .NET \s 与 Python \s 在 U+0085/U+00A0/U+1680/U+2028/U+3000 等
	// 已一致，唯缺文件分隔符组 \x1c-\x1f，显式补齐（D1' 决策）。
	pySpaceClassStr = `[\s\x{1c}-\x{1f}]`
)

// wCore/sCore 语义集"内芯"（去掉外层方括号），供字符类拼接。
const (
	wCore = `\p{L}\p{Nd}\p{Nl}\p{No}_`
	sCore = `\s\x{1c}-\x{1f}`
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pySpaceSeqPattern Python r"\s+"：连续空白 → 单空格。
	//
	// Python: text_preprocessor.py WhitespaceNormalizer.process L53
	pySpaceSeqPattern = regexp2.MustCompile(pySpaceClassStr+`+`, regexp2.None)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Process 归一化空白：连续 pySpace 序列替换为单空格，两端去除。
//
// 对齐 Python：if not text → 原样；re.sub(r"\s+", " ", text)；text.strip()。
// Python text.strip() 去除的字符集与 \s 一致，故两端 trim 用同一集合。
//
// Python: text_preprocessor.py WhitespaceNormalizer.process
func (n WhitespaceNormalizer) Process(text string) string {
	if text == "" {
		return text
	}
	out, err := pySpaceSeqPattern.Replace(text, " ", 0, -1)
	if err != nil {
		// regexp2 替换纯文本无失败路径（模式编译期已校验），防御性原样返回
		return text
	}
	return strings.TrimFunc(out, isPySpaceRune)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isPySpaceRune 判定 rune 是否属于 Python \s 集合（regexp2 \s + \x1c-\x1f）。
//
// 供 WhitespaceNormalizer 两端 trim 使用（与 pySpaceSeqPattern 同一语义）。
// unicode.IsSpace 与 Python \s 在 \x1c-\x1f 上不同（Go 不含、Python 含），
// 显式补齐后两者集合一致。
func isPySpaceRune(r rune) bool {
	switch {
	case r == 0x1c || r == 0x1d || r == 0x1e || r == 0x1f:
		return true // 补偿集：unicode.IsSpace 缺文件分隔符组
	default:
		return unicode.IsSpace(r)
	}
}
```

注意：文件头 import 需加 `"unicode"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestWhitespaceNormalizer -v
```

预期：11 个测试全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor.go internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor_test.go
git commit -m "feat(chunker): 13.9 TextPreprocessor 接口 + WhitespaceNormalizer（Python \\s 语义对齐）"
```

---

### Task 2: text_preprocessor.go——URLEmailRemover

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor_test.go`

- [ ] **Step 1: 追加失败测试（URLEmailRemover，断言值从 Python 对拍基准推导）**

```go
// ──────────────────────────── URLEmailRemover ────────────────────────────

// TestURLEmailRemover_构造默认值 对齐 Python test_init_defaults
func TestURLEmailRemover_构造默认值(t *testing.T) {
	r := URLEmailRemover{}
	assert.False(t, r.RemoveURLs) // Go 零值 false；"默认 true" 由构造方显式传参
	assert.False(t, r.RemoveEmails)
	assert.Equal(t, "", r.Replacement)
}

// TestURLEmailRemover_构造自定义值 对齐 Python test_init_custom
func TestURLEmailRemover_构造自定义值(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: false, RemoveEmails: true, Replacement: "[removed]"}
	assert.False(t, r.RemoveURLs)
	assert.True(t, r.RemoveEmails)
	assert.Equal(t, "[removed]", r.Replacement)
}

// TestURLEmailRemover_移除HTTP_URL 对齐 Python test_remove_urls_http
// Python 基准: 'Visit http://example.com for more info' → 'Visit  for more info'
func TestURLEmailRemover_移除HTTP_URL(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  for more info", r.Process("Visit http://example.com for more info"))
}

// TestURLEmailRemover_移除HTTPS_URL 对齐 Python test_remove_urls_https
func TestURLEmailRemover_移除HTTPS_URL(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  for more info", r.Process("Visit https://example.com for more info"))
}

// TestURLEmailRemover_移除WWW_URL 对齐 Python test_remove_urls_www
func TestURLEmailRemover_移除WWW_URL(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  for more info", r.Process("Visit www.example.com for more info"))
}

// TestURLEmailRemover_移除邮箱 对齐 Python test_remove_emails
// Python 基准: 'Contact us at test@example.com for support' → 'Contact us at  for support'
func TestURLEmailRemover_移除邮箱(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Contact us at  for support", r.Process("Contact us at test@example.com for support"))
}

// TestURLEmailRemover_URL替换字符串 对齐 Python test_remove_urls_with_replacement
// Python 基准: 'Visit http://example.com for more info' + '[URL]' → 'Visit [URL] for more info'
func TestURLEmailRemover_URL替换字符串(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true, Replacement: "[URL]"}
	assert.Equal(t, "Visit [URL] for more info", r.Process("Visit http://example.com for more info"))
}

// TestURLEmailRemover_邮箱替换字符串 对齐 Python test_remove_emails_with_replacement
// Python 基准: 'Contact test@example.com' + '[EMAIL]' → 'Contact [EMAIL]'
func TestURLEmailRemover_邮箱替换字符串(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true, Replacement: "[EMAIL]"}
	assert.Equal(t, "Contact [EMAIL]", r.Process("Contact test@example.com"))
}

// TestURLEmailRemover_禁用URL移除 对齐 Python test_disable_url_removal
func TestURLEmailRemover_禁用URL移除(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: false, RemoveEmails: true}
	assert.Contains(t, r.Process("Visit http://example.com for more info"), "http://example.com")
}

// TestURLEmailRemover_禁用邮箱移除 Go 专项（对偶覆盖）
func TestURLEmailRemover_禁用邮箱移除(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: false}
	assert.Contains(t, r.Process("Contact test@example.com"), "test@example.com")
}

// TestURLEmailRemover_空文本 对齐 Python test_process_empty_string + test_process_none（并入）
func TestURLEmailRemover_空文本(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "", r.Process(""))
}

// TestURLEmailRemover_URL加邮箱混合 Go 专项（对拍基准）
// Python 基准: 'Visit https://example.com and email a@b.com now' → 'Visit  and email  now'
func TestURLEmailRemover_URL加邮箱混合(t *testing.T) {
	r := URLEmailRemover{RemoveURLs: true, RemoveEmails: true}
	assert.Equal(t, "Visit  and email  now", r.Process("Visit https://example.com and email a@b.com now"))
}
```

- [ ] **Step 2: 运行测试确认编译失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestURLEmailRemover -v 2>&1 | head -5
```

预期：编译失败 `undefined: URLEmailRemover.Process`

- [ ] **Step 3: 实现 URLEmailRemover（追加到 text_preprocessor.go）**

在全局变量区块追加 3 个模式（ur lPattern/comPattern/emailPattern），导出函数区块追加 Process 方法：

```go
// 全局变量区块追加：

var (
	// urlPattern Python URL_PATTERN r"https?://\S+|www\.\S+"。
	//
	// \S → 非 pySpace 集（D1' 决策：显式语义集替换）。
	//
	// Python: text_preprocessor.py URLEmailRemover.URL_PATTERN L66
	urlPattern = regexp2.MustCompile(`https?://[^\s\x{1c}-\x{1f}]+|www\.[^\s\x{1c}-\x{1f}]+`, regexp2.None)
	// comPattern Python COM_PATTERN
	// r"(?:https?://|www\.)?\S+?\.(?:com|net|org|cn)(?:[/?#]\S*)?\b"。
	//
	// \S 同上替换；\b 保留 regexp2 原生语义（偏差 4：No/Nl/Mn/Pc 紧贴
	// 场景与 Python \b 判定分叉，Python 测试不覆盖，设计文档标注）。
	//
	// Python: text_preprocessor.py URLEmailRemover.COM_PATTERN L67
	comPattern = regexp2.MustCompile(
		`(?:https?://|www\.)?[^\s\x{1c}-\x{1f}]+?\.(?:com|net|org|cn)(?:[/?#][^\s\x{1c}-\x{1f}]*)?\b`,
		regexp2.None)
	// emailPattern Python EMAIL_PATTERN r"\b[\w\.-]+@[\w\.-]+\.\w+\b"。
	//
	// \w → 显式集（wCore）；\b 原生（偏差 4）。
	// 实测对拍：`联系①user@example.com` Go/Python 同为吞掉 ①（两侧行为一致）。
	// 注意尾部 `\.\w+\b` 的 + 量词不可丢（设计期实测踩坑：遗漏致匹配失败）。
	//
	// Python: text_preprocessor.py URLEmailRemover.EMAIL_PATTERN L69
	emailPattern = regexp2.MustCompile(
		`\b[\p{L}\p{Nd}\p{Nl}\p{No}_\.-]+@[\p{L}\p{Nd}\p{Nl}\p{No}_\.-]+\.[\p{L}\p{Nd}\p{Nl}\p{No}_]+\b`,
		regexp2.None)
)

// 导出函数区块追加：

// Process 移除 URL 和/或邮箱地址。
//
// 对齐 Python 执行序：if not text → 原样；remove_urls → URL_PATTERN.sub →
// COM_PATTERN.sub；remove_emails → EMAIL_PATTERN.sub。注意 Python 的
// Go 零值差异：Python 构造默认 remove_urls=remove_emails=true，Go 结构体
// 零值为 false——调用方需显式设置（Python 默认语义不重造构造函数）。
//
// Python: text_preprocessor.py URLEmailRemover.process L84-104
func (r URLEmailRemover) Process(text string) string {
	if text == "" {
		return text
	}
	if r.RemoveURLs {
		text = replaceAll(urlPattern, text, r.Replacement)
		text = replaceAll(comPattern, text, r.Replacement)
	}
	if r.RemoveEmails {
		text = replaceAll(emailPattern, text, r.Replacement)
	}
	return text
}

// 非导出函数区块追加：

// replaceAll regexp2 版 re.sub（全部替换）。Replace 错误为模式编译
// 期已校验的防御路径，发生时原样返回输入。
func replaceAll(re *regexp2.Regexp, text, replacement string) string {
	out, err := re.Replace(text, replacement, 0, -1)
	if err != nil {
		return text
	}
	return out
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestURLEmailRemover -v
```

预期：12 个测试全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor.go internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor_test.go
git commit -m "feat(chunker): 13.9 URLEmailRemover（三模式显式语义集 + \\b 原生偏差标注）"
```

---

### Task 3: text_preprocessor.go——SpecialCharacterNormalizer + Pipeline

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor_test.go`

- [ ] **Step 1: 追加失败测试（SpecialCharacterNormalizer + Pipeline，断言值从 Python 对拍基准推导）**

```go
// ──────────────────────────── SpecialCharacterNormalizer ────────────────────────────

// TestSpecialCharacterNormalizer_构造默认值 对齐 Python test_init_defaults
func TestSpecialCharacterNormalizer_构造默认值(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "", n.CharsToRemove)
	assert.Empty(t, n.CharsToReplace)
}

// TestSpecialCharacterNormalizer_移除控制字符 对齐 Python test_remove_control_characters
// Python 基准: 'Test\x00text\x1fwith\x7fcontrol' → 'Testtextwithcontrol'
func TestSpecialCharacterNormalizer_移除控制字符(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "Testtextwithcontrol", n.Process("Test\x00text\x1fwith\x7fcontrol"))
}

// TestSpecialCharacterNormalizer_替换字符 对齐 Python test_replace_characters
// Python 基准: 'Tom & Jerry @ home' + {'&':'and','@':'at'} → 'Tom and Jerry at home'
// 注意：& 和 @ 均为单字符，REDUNDANT {2,} 不触发（Python 行为）
func TestSpecialCharacterNormalizer_替换字符(t *testing.T) {
	n := SpecialCharacterNormalizer{CharsToReplace: []CharReplacement{{Old: "&", New: "and"}, {Old: "@", New: "at"}}}
	assert.Equal(t, "Tom and Jerry at home", n.Process("Tom & Jerry @ home"))
}

// TestSpecialCharacterNormalizer_移除指定字符 对齐 Python test_remove_specified_characters
// Python 基准: 'Test!text@with#special' + '!@#' → 'Testtextwithspecial'
func TestSpecialCharacterNormalizer_移除指定字符(t *testing.T) {
	n := SpecialCharacterNormalizer{CharsToRemove: "!@#"}
	assert.Equal(t, "Testtextwithspecial", n.Process("Test!text@with#special"))
}

// TestSpecialCharacterNormalizer_空文本 对齐 Python test_process_empty_string + test_process_none（并入）
func TestSpecialCharacterNormalizer_空文本(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "", n.Process(""))
}

// TestSpecialCharacterNormalizer_冗余符号删除 Go 专项（对拍基准，Python \w 语义验证）
// Python 基准: 'hello!!!world' → 'helloworld'（!!! 连续 3 个非词非空白符号 → 删）
func TestSpecialCharacterNormalizer_冗余符号删除(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "helloworld", n.Process("hello!!!world"))
}

// TestSpecialCharacterNormalizer_CJK保护字符不动 Go 专项（对拍基准）
// Python 基准: '中文。。。句子' → 不变（。在 protected 集，双前瞻命中跳过）
func TestSpecialCharacterNormalizer_CJK保护字符不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "中文。。。句子", n.Process("中文。。。句子"))
}

// TestSpecialCharacterNormalizer_双标记符不动 Go 专项（对拍基准）
// Python 基准: '###标题###' → 不变（(?![#*\-_]{2,}) 前瞻命中跳过）
func TestSpecialCharacterNormalizer_双标记符不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "###标题###", n.Process("###标题###"))
}

// TestSpecialCharacterNormalizer_货币符号删除 Go 专项（对拍基准）
// Python 基准: '￥￥￥三个' → '三个'（￥ 连续 3 个非词符号 → 删）
func TestSpecialCharacterNormalizer_货币符号删除(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "三个", n.Process("￥￥￥三个"))
}

// TestSpecialCharacterNormalizer_单符号不动 Go 专项（对拍基准）
// Python 基准: '单一&符号' → 不变（{2,} 需连续 2+）
func TestSpecialCharacterNormalizer_单符号不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "单一&符号", n.Process("单一&符号"))
}

// TestSpecialCharacterNormalizer_带圈数字保留 Go 专项（D1' 显式 \w 集回归，对拍基准）
// Python 基准: '①②③序号' → 不变（①②③ 是 Python \w 词字符；regexp2 原生 \w 缺 No 类会误删）
func TestSpecialCharacterNormalizer_带圈数字保留(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "①②③序号", n.Process("①②③序号"))
}

// TestSpecialCharacterNormalizer_连字符不动 Go 专项（对拍基准）
// Python 基准: 'a-b-c正常' → 不变（- 在字符类白名单内）
func TestSpecialCharacterNormalizer_连字符不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "a-b-c正常", n.Process("a-b-c正常"))
}

// TestSpecialCharacterNormalizer_邮箱符号不动 Go 专项（对拍基准）
// Python 基准: 'a@b@c' → 不变（@ 单字符不触发 {2,}）
func TestSpecialCharacterNormalizer_邮箱符号不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "a@b@c", n.Process("a@b@c"))
}

// TestSpecialCharacterNormalizer_中英混合不动 Go 专项（对拍基准）
func TestSpecialCharacterNormalizer_中英混合不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "mixed中English文", n.Process("mixed中English文"))
}

// TestSpecialCharacterNormalizer_链式替换插入序 Go 专项（F 决策回归，对拍基准）
// Python 基准: '链式a→b→c' + {'a':'b','b':'c'} → '链式c→c→c'
// （dict 插入序 a→b 先执行，b→c 后执行，a 经两步变 c）
func TestSpecialCharacterNormalizer_链式替换插入序(t *testing.T) {
	n := SpecialCharacterNormalizer{CharsToReplace: []CharReplacement{{Old: "a", New: "b"}, {Old: "b", New: "c"}}}
	assert.Equal(t, "链式c→c→c", n.Process("链式a→b→c"))
}

// TestSpecialCharacterNormalizer_组合记号不动 Go 专项（对拍基准，Mn 类回归）
// Python 基准: 'a\u0301b组合记号' → 不变（组合尖音符后 REDUNDANT 段不足 2 字符）
func TestSpecialCharacterNormalizer_组合记号不动(t *testing.T) {
	n := SpecialCharacterNormalizer{}
	assert.Equal(t, "áb组合记号", n.Process("á组合记号"))
}

// ──────────────────────────── PreprocessingPipeline ────────────────────────────

// TestPreprocessingPipeline_空管线 对齐 Python test_init_empty
func TestPreprocessingPipeline_空管线(t *testing.T) {
	p := NewPreprocessingPipeline()
	assert.Equal(t, 0, p.Len())
}

// TestPreprocessingPipeline_带预处理器构造 对齐 Python test_init_with_preprocessors
func TestPreprocessingPipeline_带预处理器构造(t *testing.T) {
	p := NewPreprocessingPipeline(WhitespaceNormalizer{}, URLEmailRemover{RemoveURLs: true, RemoveEmails: true})
	assert.Equal(t, 2, p.Len())
}

// TestPreprocessingPipeline_追加预处理器 对齐 Python test_add_preprocessor
func TestPreprocessingPipeline_追加预处理器(t *testing.T) {
	p := NewPreprocessingPipeline()
	p.AddPreprocessor(WhitespaceNormalizer{})
	assert.Equal(t, 1, p.Len())
}

// TestPreprocessingPipeline_单预处理器 对齐 Python test_process_single_preprocessor
func TestPreprocessingPipeline_单预处理器(t *testing.T) {
	p := NewPreprocessingPipeline(WhitespaceNormalizer{})
	assert.Equal(t, "This is a test", p.Process("This   is   a   test"))
}

// TestPreprocessingPipeline_顺序应用 Go 专项（对拍基准）
// TextChunker 组装序：whitespace 先、url 后
// Python 基准: 'Visit https://example.com and email a@b.com now' → 'Visit  and email  now'
func TestPreprocessingPipeline_顺序应用(t *testing.T) {
	p := NewPreprocessingPipeline(
		WhitespaceNormalizer{},
		URLEmailRemover{RemoveURLs: true, RemoveEmails: true},
	)
	assert.Equal(t, "Visit  and email  now", p.Process("Visit https://example.com and email a@b.com now"))
}

// TestPreprocessingPipeline_空文本 对齐 Python test_process_empty_string
func TestPreprocessingPipeline_空文本(t *testing.T) {
	p := NewPreprocessingPipeline(WhitespaceNormalizer{})
	assert.Equal(t, "", p.Process(""))
}
```

- [ ] **Step 2: 运行测试确认编译失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestSpecialCharacterNormalizer|TestPreprocessingPipeline" -v 2>&1 | head -5
```

预期：编译失败 `undefined: NewPreprocessingPipeline`

- [ ] **Step 3: 实现 SpecialCharacterNormalizer + Pipeline（追加到 text_preprocessor.go）**

```go
// 全局变量区块追加：

var (
	// controlCharPattern Python CONTROL_CHAR_PATTERN r"[\x00-\x1F\x7F]"。
	//
	// Python: text_preprocessor.py SpecialCharacterNormalizer.CONTROL_CHAR_PATTERN L112
	controlCharPattern = regexp2.MustCompile(`[\x00-\x1F\x7F]`, regexp2.None)
	// redundantSymbolsPattern Python REDUNDANT_SYMBOLS_PATTERN
	// r"(?![\u4e00-\u9fa5，。！？；：“”‘’（）【】《》、…·])(?![#*\-_]{2,})([^\w\s#*_\-\|]){2,}"
	//
	// 双负向前瞻 regexp2 原生支持；[^\w\s#*_\-\|] 内的 \w/\s
	// → 显式语义集（wCore+sCore，D1' 决策——①②③ 保留的根因）。
	//
	// Python: text_preprocessor.py SpecialCharacterNormalizer.REDUNDANT_SYMBOLS_PATTERN L113-115
	redundantSymbolsPattern = regexp2.MustCompile(
		`(?![\x{4e00}-\x{9fa5}，。！？；：“”‘’（）【】《》、…·])(?![#*\-_]{2,})([^\p{L}\p{Nd}\p{Nl}\p{No}_\s\x{1c}-\x{1f}#*_\-\|]){2,}`,
		regexp2.None)
)

// 导出函数区块追加：

// Process 归一化特殊字符：控制字符删除 → 冗余符号段删除 →
// 有序替换 → 指定字符删除。
//
// 对齐 Python 执行序 L128-154：if not text → 原样；
// CONTROL.sub("") → REDUNDANT.sub("") → chars_to_replace 依插入序
// str.replace → chars_to_remove 构造字符类 sub("")。
//
// Python: text_preprocessor.py SpecialCharacterNormalizer.process
func (n SpecialCharacterNormalizer) Process(text string) string {
	if text == "" {
		return text
	}
	text = replaceAll(controlCharPattern, text, "")
	text = replaceAll(redundantSymbolsPattern, text, "")
	for _, cr := range n.CharsToReplace {
		text = strings.ReplaceAll(text, cr.Old, cr.New)
	}
	if n.CharsToRemove != "" {
		text = removeChars(text, n.CharsToRemove)
	}
	return text
}

// NewPreprocessingPipeline 构造预处理管线。
//
// preprocessors 可为空（空管线 Process 直通）。
//
// Python: text_preprocessor.py PreprocessingPipeline.__init__
func NewPreprocessingPipeline(preprocessors ...TextPreprocessor) *PreprocessingPipeline {
	return &PreprocessingPipeline{preprocessors: preprocessors}
}

// AddPreprocessor 追加预处理器到管线尾部。
//
// Python: text_preprocessor.py PreprocessingPipeline.add_preprocessor
func (p *PreprocessingPipeline) AddPreprocessor(pre TextPreprocessor) {
	p.preprocessors = append(p.preprocessors, pre)
}

// Process 顺序应用全部预处理器。
//
// Python: text_preprocessor.py PreprocessingPipeline.process
func (p *PreprocessingPipeline) Process(text string) string {
	for _, pre := range p.preprocessors {
		text = pre.Process(text)
	}
	return text
}

// Len 返回预处理器数量（对齐 Python __len__）。
//
// Python: text_preprocessor.py PreprocessingPipeline.__len__
func (p *PreprocessingPipeline) Len() int {
	return len(p.preprocessors)
}

// 非导出函数区块追加：

// removeChars 删除文本中出现的指定字符集字符。
//
// 对齐 Python re.sub("[" + re.escape(chars) + "]", "", text)：
// 逐 rune 判定是否属于 chars，直接扫描无需正则（语义等价且无转义陷阱）。
func removeChars(text, chars string) string {
	charSet := make(map[rune]struct{}, len(chars))
	for _, r := range chars {
		charSet[r] = struct{}{}
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if _, ok := charSet[r]; !ok {
			b.WriteRune(r)
		}
	}
	return b.String()
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestSpecialCharacterNormalizer|TestPreprocessingPipeline" -v
```

预期：22 个测试全 PASS（含 ①②③ 保留回归、链式替换插入序）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor.go internal/agentcore/retrieval/indexing/processor/chunker/text_preprocessor_test.go
git commit -m "feat(chunker): 13.9 SpecialCharacterNormalizer（双前瞻直译+显式集）+ PreprocessingPipeline"
```

---

### Task 4: text_chunker.go——TokenizerProvider + TextChunker 骨架与构造

**Files:**
- Create: `internal/agentcore/retrieval/indexing/processor/chunker/text_chunker.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go`

- [ ] **Step 1: 写失败测试（构造 + 选项）**

```go
package chunker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 构造（char 路径） ────────────────────────────

// TestNewTextChunker_默认值 对齐 Python test_init_with_char_unit（默认 char 单位）
func TestNewTextChunker_默认值(t *testing.T) {
	c, err := NewTextChunker(512, 50)
	assert.NoError(t, err)
	assert.Equal(t, 512, c.ChunkSize)
	assert.Equal(t, 50, c.ChunkOverlap)
	// inner 路由到 CharChunker
	_, ok := c.inner.(*CharChunker)
	assert.True(t, ok)
}

// TestNewTextChunker_自定义值 对齐 Python test_init_with_custom_values
func TestNewTextChunker_自定义值(t *testing.T) {
	c, err := NewTextChunker(256, 25)
	assert.NoError(t, err)
	assert.Equal(t, 256, c.ChunkSize)
	assert.Equal(t, 25, c.ChunkOverlap)
}

// TestNewTextChunker_构造校验穿透 参数经 NewBaseChunker 校验（155100/155101）
func TestNewTextChunker_构造校验穿透(t *testing.T) {
	_, err := NewTextChunker(0, 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155100")

	_, err = NewTextChunker(512, -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")

	_, err = NewTextChunker(10, 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "155101")
}

// TestNewTextChunker_无预处理选项 对齐 Python test_init_without_preprocess_options
func TestNewTextChunker_无预处理选项(t *testing.T) {
	c, err := NewTextChunker(512, 50)
	assert.NoError(t, err)
	assert.Equal(t, 0, c.pipeline.Len())
}

// TestNewTextChunker_预处理选项全开 对齐 Python test_init_with_preprocess_options
func TestNewTextChunker_预处理选项全开(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{
		NormalizeWhitespace: true,
		RemoveURLEmail:      true,
	}))
	assert.NoError(t, err)
	assert.Equal(t, 2, c.pipeline.Len())
}

// TestNewTextChunker_只开空白归一 对齐 Python test_init_with_normalize_whitespace
func TestNewTextChunker_只开空白归一(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	assert.Equal(t, 1, c.pipeline.Len())
}

// TestNewTextChunker_只开URL邮箱移除 对齐 Python test_init_with_remove_url_email
func TestNewTextChunker_只开URL邮箱移除(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{RemoveURLEmail: true}))
	assert.NoError(t, err)
	assert.Equal(t, 1, c.pipeline.Len())
}

// TestNewTextChunker_组装顺序whitespace先url后 Go 专项：TextChunker 组装的
// preprocessors[0] 是 WhitespaceNormalizer、[1] 是 URLEmailRemover（对齐 Python if 序）
func TestNewTextChunker_组装顺序whitespace先url后(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithPreprocessOptions(PreprocessOptions{
		NormalizeWhitespace: true,
		RemoveURLEmail:      true,
	}))
	assert.NoError(t, err)
	assert.IsType(t, WhitespaceNormalizer{}, c.pipeline.preprocessors[0])
	assert.IsType(t, URLEmailRemover{}, c.pipeline.preprocessors[1])
}
```

- [ ] **Step 2: 运行测试确认编译失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestNewTextChunker -v 2>&1 | head -5
```

预期：编译失败 `undefined: NewTextChunker`

- [ ] **Step 3: 实现 text_chunker.go（骨架 + 构造 + 选项）**

```go
package chunker

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TokenizerProvider 可选 tokenizer 提供者。
//
// 对齐 Python getattr(embed_model, "tokenizer", None)：embed_model 类型
// 为 Any，Go 侧以 any 参数 + 类型断言提取（B 决策）。
type TokenizerProvider interface {
	// Tokenizer 返回嵌入模型关联的分词器；nil 表示未提供。
	Tokenizer() Tokenizer
}

// TextChunker 分块工具门面：预处理管线 + 单位路由组合的一站式分块器。
//
// 嵌入 BaseChunker 获得尺寸字段与校验；ChunkText 不被本类型支持
//（返回显式错误，A1 决策）；ChunkDocuments 完全覆写（预处理内嵌）；
// Process 局部覆写转发（13.8 决策 3 同款）。
//
// Python: openjiuwen/core/retrieval/indexing/processor/chunker/chunking.py (TextChunker)
type TextChunker struct {
	// BaseChunker 默认适配器（ChunkSize/ChunkOverlap）
	*BaseChunker
	// pipeline 预处理管线（构造时由 preprocessOptions 组装）
	pipeline *PreprocessingPipeline
	// inner 路由产物（CharChunker 或 TokenizerChunker）
	inner Chunker
}

// PreprocessOptions 预处理开关。
//
// 对齐 Python preprocess_options dict 的 2 个已知键
//（normalize_whitespace/remove_url_email）；未知键 Python 静默忽略，
// 结构体天然收窄（偏差 2）。
type PreprocessOptions struct {
	// NormalizeWhitespace 启用空白归一化（组装序：先）
	NormalizeWhitespace bool
	// RemoveURLEmail 启用 URL/邮箱移除（组装序：后）
	RemoveURLEmail bool
}

// textChunkerOptions 构造选项聚合。
type textChunkerOptions struct {
	// chunkUnit 分块单位（"char"|"token"，默认 "char"）
	chunkUnit string
	// embedModel 嵌入模型（any，断言 TokenizerProvider 提取 tokenizer）
	embedModel any
	// preprocessOptions 预处理开关（nil 表示不开）
	preprocessOptions *PreprocessOptions
}

// TextChunkerOption TextChunker 构造选项函数。
type TextChunkerOption func(*textChunkerOptions)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TextChunker 实现 Chunker
var _ Chunker = (*TextChunker)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithChunkUnit 设置分块单位（对齐 Python chunk_unit 参数，默认 "char"）。
func WithChunkUnit(unit string) TextChunkerOption {
	return func(o *textChunkerOptions) { o.chunkUnit = unit }
}

// WithEmbedModel 设置嵌入模型（对齐 Python embed_model 参数）。
//
// token 路径经 TokenizerProvider 断言提取 tokenizer。
func WithEmbedModel(model any) TextChunkerOption {
	return func(o *textChunkerOptions) { o.embedModel = model }
}

// WithPreprocessOptions 设置预处理开关（对齐 Python preprocess_options 参数）。
func WithPreprocessOptions(opts PreprocessOptions) TextChunkerOption {
	return func(o *textChunkerOptions) { o.preprocessOptions = &opts }
}

// NewTextChunker 构造 TextChunker。
//
// 构造链严格对齐 Python __init__ 执行序：
//  1. super().__init__（NewBaseChunker 校验 155100/155101）
//  2. preprocessors 组装：normalize_whitespace → WhitespaceNormalizer、
//     remove_url_email → URLEmailRemover（顺序：whitespace 先、url 后）
//  3. getChunker 路由（char → CharChunker；token → tokenizer 提取链）
//
// Python: chunking.py TextChunker.__init__
func NewTextChunker(chunkSize, chunkOverlap int, opts ...TextChunkerOption) (*TextChunker, error) {
	o := &textChunkerOptions{chunkUnit: "char"}
	for _, opt := range opts {
		opt(o)
	}

	bc, err := NewBaseChunker(chunkSize, chunkOverlap)
	if err != nil {
		return nil, err
	}

	var preprocessors []TextPreprocessor
	if o.preprocessOptions != nil {
		if o.preprocessOptions.NormalizeWhitespace {
			preprocessors = append(preprocessors, WhitespaceNormalizer{})
		}
		if o.preprocessOptions.RemoveURLEmail {
			preprocessors = append(preprocessors, URLEmailRemover{RemoveURLs: true, RemoveEmails: true})
		}
	}
	pipeline := NewPreprocessingPipeline(preprocessors...)

	inner, err := getChunker(chunkSize, chunkOverlap, o.chunkUnit, o.embedModel)
	if err != nil {
		return nil, err
	}

	c := &TextChunker{BaseChunker: bc, pipeline: pipeline, inner: inner}
	bc.SetSelf(c)
	return c, nil
}
```

同时在本文件非导出函数区块添加 getChunker（此时先写 char 分支完整 + token 分支完整——token 分支代码见 Task 5，本 Task 先落地能编译的完整版）：

```go
// getChunker 单位路由（对齐 Python get_chunker 方法）。
//
// 执行序严格对齐 Python chunking.py L56-95：
//  1. chunk_unit == "char" → CharChunker
//  2. 否则：embedModel 断言 TokenizerProvider 提取 tokenizer
//  3. tokenizer == nil → tiktoken fallback（155102 失败分支保留，
//     "未安装"分支 Go 不可达——编译期依赖，偏差 3）
//  4. chunk_size 超限下调（MaxLengthProvider 断言，0=未提供）
//  5. NewTokenizerChunker（language="auto"/splitterConfig=nil 为 Python 默认值）
//
// Python: chunking.py TextChunker.get_chunker
func getChunker(chunkSize, chunkOverlap int, chunkUnit string, embedModel any) (Chunker, error) {
	if chunkUnit == "char" {
		return NewCharChunker(WithChunkSize(chunkSize), WithChunkOverlap(chunkOverlap))
	}

	var tk Tokenizer
	if embedModel != nil {
		if p, ok := embedModel.(TokenizerProvider); ok {
			tk = p.Tokenizer()
		}
	}

	if tk == nil {
		tt, err := NewTiktokenTokenizer()
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalIndexingTokenizerProcessError,
				exception.WithParam("error_msg", "Failed to load tokenizer for token-based chunking: "+err.Error()),
			)
		}
		tk = tt
		// 对齐 Python: logger.info("Using tiktoken(cl100k_base) as tokenizer")
		logger.Info(logComponent).Msg("Using tiktoken(cl100k_base) as tokenizer")
	}

	if p, ok := tk.(MaxLengthProvider); ok {
		if maxLen := p.MaxLength(); maxLen > 0 && chunkSize > maxLen {
			original := chunkSize
			chunkSize = maxLen
			// 对齐 Python: logger.warning("chunk_size (%d) exceeds tokenizer limit, automatically adjusted to: %d")
			logger.Warn(logComponent).
				Int("original_size", original).
				Int("chunk_size", chunkSize).
				Msg("chunk_size exceeds tokenizer limit, automatically adjusted")
		}
	}

	return NewTokenizerChunker(chunkSize, chunkOverlap, tk, "auto", nil)
}
```

注意：import 区需补 `"github.com/uapclaw/uapclaw-go/internal/common/exception"`（logger 已在骨架 import）。

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestNewTextChunker -v
```

预期：8 个测试全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_chunker.go internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go
git commit -m "feat(chunker): 13.9 TextChunker 门面构造 + TokenizerProvider + getChunker 路由"
```

---

### Task 5: text_chunker.go——三方法覆写（A1 决策落地）

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/text_chunker.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go`

- [ ] **Step 1: 追加失败测试（ChunkText 显式错误 / ChunkDocuments / Process）**

```go
// ──────────────────────────── ChunkText（A1 决策） ────────────────────────────

// TestTextChunker_ChunkText_显式错误 Go 专项（A1 决策回归）：
// Python 继承基类 chunk_text 返回 None（事实不可用），Go 返回显式错误
func TestTextChunker_ChunkText_显式错误(t *testing.T) {
	c, err := NewTextChunker(512, 50)
	assert.NoError(t, err)
	chunks, err := c.ChunkText("any text")
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "does not support ChunkText")
}

// ──────────────────────────── ChunkDocuments ────────────────────────────

// TestTextChunker_ChunkDocuments_预处理生效 对齐 Python test_chunk_documents_with_preprocessing
// Python 基准: 'This   is   document   1'（normalize_whitespace）→ 块文本无三连空格
func TestTextChunker_ChunkDocuments_预处理生效(t *testing.T) {
	c, err := NewTextChunker(100, 10, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	docs := []reranker.Document{{
		ID:       "doc_1",
		Text:     "This   is   document   1",
		Metadata: map[string]any{"source": "test"},
	}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotContains(t, chunk.Text, "   ")
		assert.Equal(t, "doc_1", chunk.DocID)
		assert.Contains(t, chunk.Metadata, "chunk_index")
	}
}

// TestTextChunker_ChunkDocuments_无预处理 对齐 Python test_chunk_documents_without_preprocessing
func TestTextChunker_ChunkDocuments_无预处理(t *testing.T) {
	c, err := NewTextChunker(100, 10)
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is document 1"}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
	}
}

// TestTextChunker_ChunkDocuments_多文档 对齐 Python test_chunk_documents_multiple_docs
func TestTextChunker_ChunkDocuments_多文档(t *testing.T) {
	c, err := NewTextChunker(100, 10)
	assert.NoError(t, err)
	docs := []reranker.Document{
		{ID: "doc_1", Text: "This is document 1"},
		{ID: "doc_2", Text: "This is document 2"},
	}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	docIDs := map[string]bool{}
	for _, chunk := range chunks {
		docIDs[chunk.DocID] = true
	}
	assert.True(t, docIDs["doc_1"])
	assert.True(t, docIDs["doc_2"])
}

// TestTextChunker_ChunkDocuments_元数据保留 对齐 Python test_chunk_documents_preserves_metadata
func TestTextChunker_ChunkDocuments_元数据保留(t *testing.T) {
	c, err := NewTextChunker(100, 10)
	assert.NoError(t, err)
	docs := []reranker.Document{{
		ID:       "doc_1",
		Text:     "This is document 1",
		Metadata: map[string]any{"source": "test", "author": "test_author"},
	}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.Equal(t, "test", chunk.Metadata["source"])
		assert.Equal(t, "test_author", chunk.Metadata["author"])
		// 溯源三键
		assert.Contains(t, chunk.Metadata, "chunk_index")
		assert.Contains(t, chunk.Metadata, "total_chunks")
		assert.Contains(t, chunk.Metadata, "chunk_id")
		assert.Equal(t, chunk.ID, chunk.Metadata["chunk_id"])
	}
}

// TestTextChunker_ChunkDocuments_预处理后空文本 Go 专项：清洗后空串 → 0 块
// （inner CharChunker.ChunkText("") 返回 []）
func TestTextChunker_ChunkDocuments_预处理后空文本(t *testing.T) {
	c, err := NewTextChunker(100, 10, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "   \n\t  "}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Empty(t, chunks)
}

// TestTextChunker_ChunkDocuments_实际分块值 Go 专项（真实 CharChunker 路径，Python 对拍推演）
// 'This is a test text'(19 字符) size=10 overlap=2 → 3 块（对齐 CharChunker 既有测试）
func TestTextChunker_ChunkDocuments_实际分块值(t *testing.T) {
	c, err := NewTextChunker(10, 2)
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is a test text"}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Len(t, chunks, 3)
	assert.Equal(t, "This is a ", chunks[0].Text)
	assert.Equal(t, "a test tex", chunks[1].Text)
	assert.Equal(t, "ext", chunks[2].Text)
}

// ──────────────────────────── Process（局部覆写） ────────────────────────────

// TestTextChunker_Process_路由到覆写版ChunkDocuments Go 专项（A1 决策回归）：
// 经 Process 入口预处理仍生效（BaseChunker.Process 静态解析缺陷的防御，13.8 决策 3 同款）
func TestTextChunker_Process_路由到覆写版ChunkDocuments(t *testing.T) {
	c, err := NewTextChunker(100, 10, WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This   is   document   1"}}
	chunks, err := c.Process(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotContains(t, chunk.Text, "   ")
	}
}
```

注意：text_chunker_test.go 头部 import 需补 `"context"` 与 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"`（Task 4 的测试还没用到，本 Task 追加）。

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestTextChunker_ChunkText|TestTextChunker_ChunkDocuments|TestTextChunker_Process" -v 2>&1 | head -10
```

预期：编译失败——TextChunker 未实现 Chunker 接口（缺 ChunkText/ChunkDocuments/Process 方法），编译期断言 `var _ Chunker = (*TextChunker)(nil)` 报错

- [ ] **Step 3: 实现三方法覆写（追加到 text_chunker.go 导出函数区块）**

```go
// ChunkText 返回显式错误。
//
// Python 继承基类 chunk_text 返回 None（事实不可用——调用者迭代即崩），
// Go 无 None 语义，显式错误是最贴近表达（A1 决策；对齐 base.go
// chunkTextOf 的 standalone 错误先例）。
//
// Python: chunking.py TextChunker（未覆写 chunk_text，继承 base.py L79 return None）
func (c *TextChunker) ChunkText(text string) ([]string, error) {
	return nil, fmt.Errorf("TextChunker does not support ChunkText; use ChunkDocuments instead")
}

// ChunkDocuments 分块文档列表（完全覆写：预处理内嵌在循环）。
//
// 对齐 Python chunk_documents L97-125 执行序：每文档
// doc_text = pipeline(doc.text) → self.chunker.chunk_text(doc_text)（注意
// 调 inner 的 ChunkText，非本类型覆写版）→ 组块（uuid + 溯源三键，
// mergeChunkMetadata 复用 13.8 提取的 base.go 函数）。
//
// Python: chunking.py TextChunker.chunk_documents
func (c *TextChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	_ = ctx // 对齐 Python 同步签名，当前无 ctx 消费
	chunks := make([]common.TextChunk, 0)
	for _, doc := range docs {
		docText := doc.Text
		if c.pipeline != nil {
			docText = c.pipeline.Process(docText)
		}
		texts, err := c.inner.ChunkText(docText)
		if err != nil {
			return nil, err
		}
		for i, text := range texts {
			uid := uuid.NewString()
			meta := mergeChunkMetadata(doc.Metadata, i, len(texts), uid)
			chunks = append(chunks, common.TextChunk{
				ID:       uid,
				Text:     text,
				DocID:    doc.ID,
				Metadata: meta,
			})
		}
	}
	return chunks, nil
}

// Process 处理文档（Processor 契约入口），局部覆写转发到自身 ChunkDocuments。
//
// BaseChunker.Process 静态解析无法路由子类覆写的 ChunkDocuments（Go 嵌入
// 是组合非继承），与 HybridChunker 同款局部覆写（13.8 决策 3 预警的惯例）。
//
// Python: chunking.py（继承 base.py process → 动态分发到覆写版 chunk_documents）
func (c *TextChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return c.ChunkDocuments(ctx, docs)
}
```

注意：import 区补 `"context"` / `"fmt"` / `"github.com/google/uuid"` / `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"` / `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestTextChunker" -v
```

预期：全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_chunker.go internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go
git commit -m "feat(chunker): 13.9 TextChunker 三方法覆写（A1：ChunkText 显式错误 + Process 局部覆写）"
```

---

### Task 6: token 路径测试 + fake 策略

**Files:**
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go`

**Files:**（本 Task 纯测试，无源码改动）

- [ ] **Step 1: 追加 fake 类型与 token 路径测试**

```go
// ──────────────────────────── fake 类型 ────────────────────────────

// fakeEmbedModelWithTokenizer 实现 TokenizerProvider 的嵌入模型 fake
type fakeEmbedModelWithTokenizer struct {
	// tk 携带的 tokenizer
	tk Tokenizer
}

// Tokenizer 返回携带的 tokenizer
func (f *fakeEmbedModelWithTokenizer) Tokenizer() Tokenizer { return f.tk }

// fakeEmbedModelPlain 无 tokenizer 属性的嵌入模型 fake（不实现 TokenizerProvider）
type fakeEmbedModelPlain struct{}

// fakeTokenizerWithMaxLength 实现 MaxLengthProvider 的 fake tokenizer
type fakeTokenizerWithMaxLength struct {
	// maxLen MaxLength() 返回值（0=未提供）
	maxLen int
}

// Encode 空实现（token 路由测试不触发编码）
func (f *fakeTokenizerWithMaxLength) Encode(text string) ([]int, error) {
	return []int{}, nil
}

// MaxLength 返回预设上限
func (f *fakeTokenizerWithMaxLength) MaxLength() int { return f.maxLen }

// fakeTokenizerPlain 仅实现 Tokenizer 接口的 fake
type fakeTokenizerPlain struct{}

// Encode 空实现
func (f *fakeTokenizerPlain) Encode(text string) ([]int, error) {
	return []int{}, nil
}

// ──────────────────────────── token 路径 ────────────────────────────

// TestNewTextChunker_token路径无embedModel 对齐 Python get_chunker token 分支：
// 无 embedModel → tiktoken fallback（真实 tiktoken，决策 10）
func TestNewTextChunker_token路径无embedModel(t *testing.T) {
	c, err := NewTextChunker(512, 50, WithChunkUnit("token"))
	assert.NoError(t, err)
	_, ok := c.inner.(*TokenizerChunker)
	assert.True(t, ok)
}

// TestNewTextChunker_token路径embedModel带tokenizer 对齐 Python 分支①：
// getattr(embed_model, "tokenizer") 提取成功
func TestNewTextChunker_token路径embedModel带tokenizer(t *testing.T) {
	c, err := NewTextChunker(512, 50,
		WithChunkUnit("token"),
		WithEmbedModel(&fakeEmbedModelWithTokenizer{tk: &fakeTokenizerPlain{}}),
	)
	assert.NoError(t, err)
	_, ok := c.inner.(*TokenizerChunker)
	assert.True(t, ok)
}

// TestNewTextChunker_token路径embedModel无tokenizer 对齐 Python：
// embed_model 无 tokenizer 属性 → tiktoken fallback
func TestNewTextChunker_token路径embedModel无tokenizer(t *testing.T) {
	c, err := NewTextChunker(512, 50,
		WithChunkUnit("token"),
		WithEmbedModel(fakeEmbedModelPlain{}),
	)
	assert.NoError(t, err)
	_, ok := c.inner.(*TokenizerChunker)
	assert.True(t, ok)
}

// TestNewTextChunker_token路径size超限下调 对齐 Python test_get_chunker_token_unit_adjusts_size：
// tokenizer.model_max_length=256 < chunk_size=512 → 下调至 256
func TestNewTextChunker_token路径size超限下调(t *testing.T) {
	c, err := NewTextChunker(512, 50,
		WithChunkUnit("token"),
		WithEmbedModel(&fakeEmbedModelWithTokenizer{tk: &fakeTokenizerWithMaxLength{maxLen: 256}}),
	)
	assert.NoError(t, err)
	assert.Equal(t, 512, c.ChunkSize) // 外层尺寸不变
	tc, ok := c.inner.(*TokenizerChunker)
	assert.True(t, ok)
	assert.Equal(t, 256, tc.ChunkSize) // inner 已下调
}

// TestNewTextChunker_token路径size不超限不下调 Go 专项：
// maxLen=0（未提供）→ 不下调
func TestNewTextChunker_token路径size不超限不下调(t *testing.T) {
	c, err := NewTextChunker(512, 50,
		WithChunkUnit("token"),
		WithEmbedModel(&fakeEmbedModelWithTokenizer{tk: &fakeTokenizerWithMaxLength{maxLen: 0}}),
	)
	assert.NoError(t, err)
	tc, ok := c.inner.(*TokenizerChunker)
	assert.True(t, ok)
	assert.Equal(t, 512, tc.ChunkSize)
}

// TestNewTextChunker_token路径真实分块 Go 专项（决策 10：真实 tiktoken 端到端）
// cl100k_base 词表本地计算不联网
func TestNewTextChunker_token路径真实分块(t *testing.T) {
	c, err := NewTextChunker(50, 10, WithChunkUnit("token"))
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This is a longer document that should be split into multiple token budget chunks."}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.Greater(t, len(chunks), 1)
	for _, chunk := range chunks {
		assert.Equal(t, "doc_1", chunk.DocID)
	}
}

// TestNewTextChunker_token路径预处理组合 Go 专项：token 路径 + 预处理组合
func TestNewTextChunker_token路径预处理组合(t *testing.T) {
	c, err := NewTextChunker(50, 10,
		WithChunkUnit("token"),
		WithPreprocessOptions(PreprocessOptions{NormalizeWhitespace: true}),
	)
	assert.NoError(t, err)
	docs := []reranker.Document{{ID: "doc_1", Text: "This   is   document   1"}}
	chunks, err := c.ChunkDocuments(context.Background(), docs)
	assert.NoError(t, err)
	assert.NotEmpty(t, chunks)
	for _, chunk := range chunks {
		assert.NotContains(t, chunk.Text, "   ")
	}
}
```

- [ ] **Step 2: 运行测试确认通过（getChunker 已在 Task 4 实现，本组应直接全绿）**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run "TestNewTextChunker_token" -v
```

预期：7 个测试全 PASS（若失败则修 Task 4 的 getChunker 实现——注意验证 MaxLengthProvider 断言在 tiktoken fallback 之后执行，顺序对齐 Python：先 fallback 后下调）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go
git commit -m "test(chunker): 13.9 token 路径（fake embedModel 三态 + 真实 tiktoken 端到端）"
```

---

### Task 7: registry 零注册验证 + doc.go 更新

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/processor/chunker/doc.go`
- Test: `internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go`

- [ ] **Step 1: 追加 registry 零注册验证测试**

```go
// ──────────────────────────── registry 零注册（E 决策） ────────────────────────────

// TestGetChunker_text未注册 Go 专项（E 决策回归）：Python 只注册 char/hybrid，
// "text" 不注册（TextChunker 是 SDK 门面非底层策略）
func TestGetChunker_text未注册(t *testing.T) {
	_, err := GetChunker("text")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Unknown chunker: text")
}
```

- [ ] **Step 2: 运行测试确认通过**

```bash
go test ./internal/agentcore/retrieval/indexing/processor/chunker/ -run TestGetChunker_text未注册 -v
```

预期：PASS（"text" 未注册，GetChunker 报 Unknown）

- [ ] **Step 3: 更新 doc.go 文件目录**

`doc.go` 的文件目录区块替换为（在 tokenizer_chunker.go 行后追加 2 行）：

```
//	chunker/
//	├── doc.go               # 包文档
//	├── base.go              # Chunker 接口 + DimsProvider + BaseChunker 默认适配器 + 构造校验
//	├── registry.go          # 分块器注册表 + GetChunker/RegisterChunker + hybridFactory
//	├── text_splitter.go     # TextSplitter 接口 + CharSplitter + Tokenizer 三态接口
//	│                        #   + TiktokenTokenizer + IndexSentenceSplitter（13.7）
//	├── char_chunker.go      # CharChunker 字符级分块器（"char" 注册项）
//	├── hybrid_chunker.go    # HybridChunker 混合分块器（"hybrid" 注册项，13.8）
//	├── tokenizer_chunker.go # TokenizerChunker token 预算分块器（"tokenizer" 注册项）
//	├── text_preprocessor.go # TextPreprocessor 接口 + 3 预处理器 + Pipeline（13.9）
//	└── text_chunker.go      # TextChunker SDK 门面（预处理+单位路由，13.9）
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/indexing/processor/chunker/doc.go internal/agentcore/retrieval/indexing/processor/chunker/text_chunker_test.go
git commit -m "docs(chunker): 13.9 doc.go 目录补 text_preprocessor/text_chunker + registry 零注册验证"
```

---

### Task 8: 全量验证 + 计划表回填

**Files:**
- Modify: `/home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md:799`

- [ ] **Step 1: 全量测试 + 覆盖率**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
go test -cover ./internal/agentcore/retrieval/indexing/processor/chunker/... -tags "test" 2>&1 | tail -5
```

预期：全 PASS，覆盖率 ≥85%（基线 93.7%，新增两文件后预计 90%+；155102 失败分支 4 行不覆盖是已知留白——决策 9）

- [ ] **Step 2: 全包编译验证（无循环依赖）**

```bash
go build ./... && go vet ./internal/agentcore/retrieval/indexing/processor/chunker/...
```

预期：零错误零警告

- [ ] **Step 3: 回填 IMPLEMENTATION_PLAN.md 13.9**

第 799 行（13.9 行）`☐` 改 `✅`，产出列追加摘要（格式对齐 13.8 行风格，既有行一字不动只改本行）：

```markdown
| 13.9 | ✅ | 分块工具 | `TextChunker SDK门面(嵌入BaseChunker+三处覆写：ChunkText显式错误/ChunkDocuments完全覆写pipeline→inner.ChunkText→mergeChunkMetadata/Process局部覆写转发 + getChunker四分支路由：char→CharChunker、token→TokenizerProvider断言提取→nil则NewTiktokenTokenizer 155102失败分支保留→MaxLengthProvider断言超限下调Warn双字段→NewTokenizerChunker默认参) + text_preprocessor.go(TextPreprocessor接口 + WhitespaceNormalizer/URLEmailRemover/SpecialCharacterNormalizer三预处理器 + PreprocessingPipeline顺序应用 + D1'显式Python语义集regexp2：\w→[\p{L}\p{Nd}\p{Nl}\p{No}_]、\s→[\s\x{1c}-\x{1f}]补偿、\b原生偏差标注 + CharReplacement有序切片链式替换 + TokenizerProvider可选接口 + PreprocessOptions结构体)。registry零注册"text"(对齐Python)。设计：docs/superpowers/specs/2026-10-20-textchunker-13.9-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/chunking.py + text_preprocessor.py` |
```

注意：原 13.9 行的 Python 参考路径列是 `openjiuwen/core/retrieval/indexing/processor/chunker/`，保持路径前缀一致改为具体文件（chunking.py + text_preprocessor.py），⚠️ 带出说明保留语义已由本行描述覆盖（13.6/13.7/13.8 行内的 ⚠️ 标注一字不动）。

- [ ] **Step 4: 提交回填**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 13.9 计划表回填（Chunker 层 13.5-13.9 收尾）"
```

- [ ] **Step 5: 更新记忆**

将 `textchunker-13.9-design-decisions.md` 从"设计定稿待实施"更新为"已实施完成"状态（记录覆盖率数字、commits 数、审查发现如有）。

---

## 验收清单

- [ ] `go test -cover ./internal/agentcore/retrieval/indexing/processor/chunker/...` 全绿且 ≥85%
- [ ] ①②③序号保留（D1' 显式 \w 集回归）测试通过
- [ ] 链式替换插入序（F 决策）测试通过
- [ ] ChunkText 显式错误（A1）测试通过
- [ ] Process 路由覆写版（13.8 惯例）测试通过
- [ ] token 路径真实 tiktoken 端到端（决策 10）测试通过
- [ ] `GetChunker("text")` 报 Unknown（E 决策）测试通过
- [ ] 既有 13.5-13.8 全部测试不回归
- [ ] IMPLEMENTATION_PLAN.md 13.9 ✅ 且既有行一字不动
