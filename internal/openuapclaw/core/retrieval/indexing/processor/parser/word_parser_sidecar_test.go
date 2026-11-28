package parser

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// ──────────────────────────── WordParser sidecar 测试（30.3 Task4） ────────────────────────────

// TestWordParser_Parse_段落sidecar 段落映射注入 + 段落文本应存在于全文。
func TestWordParser_Parse_段落sidecar(t *testing.T) {
	p, err := NewWordParser()
	if err != nil {
		t.Fatalf("NewWordParser: %v", err)
	}
	docs, err := p.Parse(t.Context(), "testdata/fixture_word.docx")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	raw, ok := docs[0].Metadata[common.MetaKeySourceParagraphMap]
	if !ok {
		t.Fatal("Metadata 应含 source_paragraph_map sidecar")
	}
	paraMaps, ok := raw.([]common.ParagraphMap)
	if !ok || len(paraMaps) == 0 {
		t.Fatalf("sidecar 应为非空 []common.ParagraphMap，实际 %T", raw)
	}
	for _, pm := range paraMaps {
		if pm.Paragraph < 0 {
			t.Fatalf("段落号非法: %+v", pm)
		}
		if pm.Text != "" && !strings.Contains(docs[0].Text, pm.Text) {
			t.Errorf("段落 %d 文本应存在于全文: %q", pm.Paragraph, pm.Text)
		}
	}
	// 既有 golden 全链不受影响（ParseContent 路径回归由 word_parser_test.go 覆盖）
}
