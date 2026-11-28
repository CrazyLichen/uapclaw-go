package parser

import (
	"strings"
	"testing"
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
	sm := docs[0].SourceMaps
	if sm == nil || len(sm.Paragraphs) == 0 {
		t.Fatal("Document.SourceMaps 应含非空 Paragraphs")
	}
	paraMaps := sm.Paragraphs
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
