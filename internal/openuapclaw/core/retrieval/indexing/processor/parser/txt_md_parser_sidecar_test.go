package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
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
	raw, ok := docs[0].Metadata[common.MetaKeySourceCharRange]
	if !ok || raw != true {
		t.Fatalf("Metadata 应含 source_char_range=true，实际 %v", raw)
	}
	if docs[0].Text != "hello world\nsecond line" {
		t.Errorf("TrimSpace 语义回归: %q", docs[0].Text)
	}
}
