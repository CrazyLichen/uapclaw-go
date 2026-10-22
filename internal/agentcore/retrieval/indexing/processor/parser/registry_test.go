package parser

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
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

// TestRegisterParser_大写扩展名归一 ".JSON" 注册后 ".json" 可查，原键不存在。
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

// TestRegisterParser_并发安全 并发注册不同扩展名 + 并发读已有键，-race 验证。
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
			getParserFactory(".json") // 并发读（届时 .json 由 Task 3 init 注册；当前不存在也安全）
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

// TestParserTypeName 取实例类型名（注册日志用辅助函数）。
func TestParserTypeName(t *testing.T) {
	if got := parserTypeName(newFakeExtParser("t")); got != "parser.fakeExtParser" && !strings.Contains(got, "fakeExtParser") {
		t.Fatalf("unexpected type name %q", got)
	}
	if got := parserTypeName(nil); got != "unknown" {
		t.Fatalf("nil should be 'unknown', got %q", got)
	}
}
