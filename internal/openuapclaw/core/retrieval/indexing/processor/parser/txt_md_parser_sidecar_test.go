package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── TxtMdParser sidecar 测试（30.3 Task5） ────────────────────────────

// TestTxtMdParser_Parse_charRange标记 文档 metadata 携带 char_range 定位标记。
func TestTxtMdParser_Parse_charRange标记(t *testing.T) {
	p, err := NewTxtMdParser()
	if err != nil {
		t.Fatalf("NewTxtMdParser: %v", err)
	}
	dir := t.TempDir()
	txtPath := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(txtPath, []byte("hello world\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs, err := p.Parse(t.Context(), txtPath)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	if docs[0].SourceMaps == nil || !docs[0].SourceMaps.CharRange {
		t.Fatal("Document.SourceMaps 应携带 CharRange=true")
	}
	if docs[0].Text != "hello world\nsecond line" {
		t.Errorf("TrimSpace 语义回归: %q", docs[0].Text)
	}
}
