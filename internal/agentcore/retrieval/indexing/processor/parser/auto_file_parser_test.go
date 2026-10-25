package parser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// backupRegistry 备份当前注册表（锁内复制）。
// Python: original_registry = _PARSER_REGISTRY.copy()
func backupRegistry(t *testing.T) map[string]func() Parser {
	t.Helper()
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	backup := make(map[string]func() Parser, len(parserRegistry))
	for k, v := range parserRegistry {
		backup[k] = v
	}
	return backup
}

// restoreRegistry 恢复注册表（覆盖式还原）。
// Python: _PARSER_REGISTRY.clear(); _PARSER_REGISTRY.update(original_registry)
func restoreRegistry(t *testing.T, backup map[string]func() Parser) {
	t.Helper()
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	parserRegistry = make(map[string]func() Parser, len(backup))
	for k, v := range backup {
		parserRegistry[k] = v
	}
}

// writeTempFile 写临时文件返回路径。
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

// ──────────────────────────── AutoFileParser 测试 ────────────────────────────

// TestNewAutoFileParser 构造成功（隐含全 registry init 加载）。
// Python: test_init
func TestNewAutoFileParser(t *testing.T) {
	p, err := NewAutoFileParser()
	if err != nil {
		t.Fatalf("NewAutoFileParser() error = %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil parser")
	}
}

// TestAutoFileParserParse_真实txt文件全链 .txt → TxtMdParser 全链 → 四键 metadata。
// Python: test_parse_pdf_file 的 Go 对应物之一（轻量文件验证路由+metadata）
func TestAutoFileParserParse_真实txt文件全链(t *testing.T) {
	p, err := NewAutoFileParser()
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	path := writeTempFile(t, "hello.txt", "hello world")
	docs, err := p.Parse(t.Context(), path, WithDocID("doc_1"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	m := docs[0].Metadata
	if m["doc_id"] != "doc_1" || m["title"] != "hello.txt" ||
		m["file_path"] != path || m["file_ext"] != ".txt" {
		t.Fatalf("metadata 四键不符: %#v", m)
	}
	if docs[0].Text != "hello world" {
		t.Fatalf("text = %q, want %q", docs[0].Text, "hello world")
	}
}

// TestAutoFileParserParse_真实json文件全链 .json → JSONParser 全链。
// Python: test_parse_json_file
func TestAutoFileParserParse_真实json文件全链(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "data.json", `{"key": "value"}`)
	docs, err := p.Parse(t.Context(), path, WithDocID("doc_1"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Metadata["file_ext"] != ".json" {
		t.Fatalf("file_ext = %v, want .json", docs[0].Metadata["file_ext"])
	}
}

// TestAutoFileParserParse_真实pdf文件全链 复用 13.12 合成辅助走 PDFParser 真路由。
// Python: test_parse_pdf_file 本意对应物——测 .pdf 扩展名路由+metadata 四键。
func TestAutoFileParserParse_真实pdf文件全链(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempPDF(t, "sample.pdf", []string{"PDF content"}, map[int]bool{1: true})
	docs, err := p.Parse(t.Context(), path, WithDocID("doc_1"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Metadata["file_ext"] != ".pdf" {
		t.Fatalf("file_ext = %v, want .pdf", docs[0].Metadata["file_ext"])
	}
	if !strings.Contains(docs[0].Text, "PDF content") {
		t.Fatalf("text 应含页面文本: %q", docs[0].Text)
	}
}

// TestAutoFileParserParse_文件不存在 155103 + 消息断言。
// Python: test_parse_file_not_found
func TestAutoFileParserParse_文件不存在(t *testing.T) {
	p, _ := NewAutoFileParser()
	_, err := p.Parse(t.Context(), "nonexistent.txt", WithDocID("doc_1"))
	be, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFileNotFound {
		t.Errorf("status = %v, want StatusRetrievalIndexingFileNotFound", be.Status())
	}
	if !strings.Contains(be.Error(), "does not exist") {
		t.Errorf("error message 应含 'does not exist': %s", be.Error())
	}
}

// TestAutoFileParserParse_ENOTDIR路径归155103 路径中组件为普通文件（非目录）时
// stat 报 ENOTDIR——Python os.path.exists 吞该 OSError 返回 False 触发 155103；
// 字面修复后（err != nil 全判）Go 同为 155103。
// 偏差 #11 回归验证：IsNotExist 旧惯例会放行 ENOTDIR 落到扩展名查找。
func TestAutoFileParserParse_ENOTDIR路径归155103(t *testing.T) {
	p, _ := NewAutoFileParser()
	blocker := writeTempFile(t, "blocker", "x")
	nonDir := filepath.Join(blocker, "sub.txt")
	_, err := p.Parse(t.Context(), nonDir)
	be, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFileNotFound {
		t.Errorf("status = %v, want StatusRetrievalIndexingFileNotFound", be.Status())
	}
	if !strings.Contains(be.Error(), "does not exist") {
		t.Errorf("error message 应含 'does not exist': %s", be.Error())
	}
}

// TestAutoFileParserParse_不支持格式 155104 + 消息断言。
// Python: test_parse_unsupported_format
func TestAutoFileParserParse_不支持格式(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "bad.xyz", "content")
	_, err := p.Parse(t.Context(), path, WithDocID("doc_1"))
	be, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("expected *exception.BaseError, got %T", err)
	}
	if be.Status() != exception.StatusRetrievalIndexingFormatNotSupport {
		t.Errorf("status = %v, want StatusRetrievalIndexingFormatNotSupport", be.Status())
	}
	msg := be.Error()
	if !strings.Contains(msg, "Unsupported format: .xyz") {
		t.Errorf("message 应含扩展名: %s", msg)
	}
	if !strings.Contains(msg, "are supported") {
		t.Errorf("message 应含 'are supported': %s", msg)
	}
}

// TestAutoFileParserParse_空结果 空 .txt → 空切片。
// Python: test_parse_empty_result
func TestAutoFileParserParse_空结果(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "empty.txt", "")
	docs, err := p.Parse(t.Context(), path, WithDocID("doc_1"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("expected 0 docs, got %d", len(docs))
	}
}

// TestAutoFileParserParse_ExtraFileName覆盖 Extra 传 file_name → title 覆盖。
func TestAutoFileParserParse_ExtraFileName覆盖(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "hello.txt", "content")
	docs, _ := p.Parse(t.Context(), path, WithExtra("file_name", "custom name.txt"))
	if docs[0].Metadata["title"] != "custom name.txt" {
		t.Fatalf("title = %v, want custom name.txt", docs[0].Metadata["title"])
	}
}

// TestAutoFileParserParse_ExtraFileName兜底 无 Extra → title=basename。
func TestAutoFileParserParse_ExtraFileName兜底(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "hello.txt", "content")
	docs, _ := p.Parse(t.Context(), path)
	if docs[0].Metadata["title"] != "hello.txt" {
		t.Fatalf("title = %v, want hello.txt", docs[0].Metadata["title"])
	}
}

// TestAutoFileParserParse_Extra非字符串透传 file_name=123 → title=123 原值。
// 决策 5 字面透传：键存在即原值 any 进 title（对齐 Python dict.get + Dict[str,Any]）。
func TestAutoFileParserParse_Extra非字符串透传(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "hello.txt", "content")
	docs, _ := p.Parse(t.Context(), path, WithExtra("file_name", 123))
	if v, ok := docs[0].Metadata["title"].(int); !ok || v != 123 {
		t.Fatalf("title = %#v, want 123 (int)", docs[0].Metadata["title"])
	}
}

// TestAutoFileParserParse_DocID透传含空串 WithDocID("") → metadata["doc_id"]=""。
// 13.10 spec 决策 6 回填验证：字面空串不触发 uuid。
func TestAutoFileParserParse_DocID透传含空串(t *testing.T) {
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "hello.txt", "content")
	docs, _ := p.Parse(t.Context(), path, WithDocID(""))
	if docs[0].Metadata["doc_id"] != "" {
		t.Fatalf("doc_id = %v, want 空串", docs[0].Metadata["doc_id"])
	}
}

// TestAutoFileParserParse_nilMetadata防御 底层返回 nil Metadata → 四键写入不 panic。
// 决策 6：防御仅服务 RegisterNewParser 第三方解析器路径。
func TestAutoFileParserParse_nilMetadata防御(t *testing.T) {
	backup := backupRegistry(t)
	defer restoreRegistry(t, backup)
	RegisterNewParser(".nilmeta", func() Parser {
		return newNilMetadataParser()
	})
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "x.nilmeta", "any")
	docs, err := p.Parse(t.Context(), path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Metadata["file_ext"] != ".nilmeta" {
		t.Fatalf("file_ext = %v, want .nilmeta", docs[0].Metadata["file_ext"])
	}
}

// TestAutoFileParserSupports_三态 存在+注册 / 不存在 / 不注册扩展名。
// Python: test_supports_existing_file + test_supports_nonexistent_file + test_supports_unsupported_format
func TestAutoFileParserSupports_三态(t *testing.T) {
	p, _ := NewAutoFileParser()
	existing := writeTempFile(t, "hello.txt", "content")
	if !p.Supports(existing) {
		t.Error("存在+注册扩展名应支持")
	}
	if p.Supports("nonexistent.txt") {
		t.Error("不存在文件应不支持")
	}
	unknown := writeTempFile(t, "x.xyz", "content")
	if p.Supports(unknown) {
		t.Error("不注册扩展名应不支持")
	}
}

// TestRegisterNewParser_动态注册 last-wins 覆盖既有扩展名 + supports 生效。
// Python: test_register_new_parser
func TestRegisterNewParser_动态注册(t *testing.T) {
	backup := backupRegistry(t)
	defer restoreRegistry(t, backup)

	RegisterNewParser(".custom", func() Parser { return newFakeExtParser("custom content") })
	if _, ok := getParserFactory(".custom"); !ok {
		t.Fatal("expected .custom registered")
	}
	p, _ := NewAutoFileParser()
	path := writeTempFile(t, "x.custom", "test")
	if !p.Supports(path) {
		t.Fatal("动态注册后 supports 应生效")
	}
	// last-wins：覆盖既有 .custom 注册（Python register_new_parser 无条件覆盖语义）
	RegisterNewParser(".custom", func() Parser { return newFakeExtParser("replaced") })
	docs, _ := p.Parse(t.Context(), path)
	if len(docs) != 1 || docs[0].Text != "replaced" {
		t.Fatalf("last-wins 覆盖未生效: %#v", docs)
	}
	if docs[0].Metadata["file_ext"] != ".custom" {
		t.Fatalf("file_ext = %v", docs[0].Metadata["file_ext"])
	}
}

// TestGetSupportedFormats 返回全部已注册扩展名。
// Python: test_get_supported_formats
func TestGetSupportedFormats(t *testing.T) {
	formats := GetSupportedFormats()
	found := 0
	for _, f := range formats {
		if f == ".txt" || f == ".pdf" || f == ".json" {
			found++
		}
	}
	if found < 3 {
		t.Fatalf("应含 .txt/.pdf/.json 常见格式, got %v", formats)
	}
}

// ──────────────────────────── 测试桩 ────────────────────────────

// nilMetadataParser 返回 nil Metadata 文档的解析器（决策 6 防御测试桩，
// 模拟契约违背的第三方解析器；LazyParse/Process/ParseContent 走 BaseParser
// 嵌入默认实现）。
type nilMetadataParser struct {
	*BaseParser
}

// newNilMetadataParser 构造并完成 self 注入。
func newNilMetadataParser() *nilMetadataParser {
	p := &nilMetadataParser{}
	bp, _ := NewBaseParser(p)
	p.BaseParser = bp
	return p
}

// Parse 覆写：返回单文档但 Metadata 为 nil。
func (p *nilMetadataParser) Parse(ctx context.Context, doc string, opts ...ParseOption) ([]reranker.Document, error) {
	o := NewParseOptions(opts...)
	return []reranker.Document{{ID: o.DocID, Text: "raw"}}, nil
}

// Supports 覆写：恒真。
func (p *nilMetadataParser) Supports(doc string) bool { return true }
