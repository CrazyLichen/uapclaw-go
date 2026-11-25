package attachments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// docTestEnv 隔离 DATA_DIR 并返回会话 uploads 目录（含预建）。
func docTestEnv(t *testing.T, sid string) string {
	t.Helper()
	uploads := mediaTestEnv(t, sid)
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	return uploads
}

// TestPersistAndParseDocuments_base64落盘与回填 对齐主链（documents 入口）。
// Python: document_attachments.py:58-161。
func TestPersistAndParseDocuments_base64落盘与回填(t *testing.T) {
	uploads := docTestEnv(t, "doc-sess-1")
	content := "hello document"
	params := map[string]any{
		"documents": []any{
			map[string]any{"filename": "notes.txt", "mime_type": "text/plain", "base64_data": b64([]byte(content))},
		},
	}
	got := PersistAndParseDocuments(params, "doc-sess-1")
	items := got["media_items"].([]any)
	if len(items) != 1 {
		t.Fatalf("应产出 1 条文档记录: %v", got)
	}
	stored := items[0].(map[string]any)
	path, _ := stored["path"].(string)
	b, err := os.ReadFile(path)
	if err != nil || string(b) != content {
		t.Fatalf("落盘内容不符: %v", err)
	}
	if filepath.Dir(path) != uploads {
		t.Fatalf("落盘目录不符: %q", filepath.Dir(path))
	}
	// 原始条目 base64 已剥离（响应不回显整文件）
	if raw, _ := got["documents"].([]any); len(raw) > 0 {
		m := raw[0].(map[string]any)
		if _, exists := m["base64_data"]; exists {
			t.Fatalf("document.persist 响应不应回显 base64: %v", m)
		}
	}
	// files.uploaded_documents 五字段
	files := got["files"].(map[string]any)
	sum := files["uploaded_documents"].([]map[string]any)
	if len(sum) != 1 || sum[0]["filename"] != "notes.txt" || sum[0]["original_path"] == "" {
		t.Fatalf("uploaded_documents 不符: %v", files)
	}
	// forbidden_formats 兜底
	if _, ok := got["forbidden_formats"]; !ok {
		t.Fatal("缺 forbidden_formats 兜底")
	}
}

// TestPersistAndParseDocuments_黑名单拒绝与错误回显 对齐 document_errors。
func TestPersistAndParseDocuments_黑名单拒绝与错误回显(t *testing.T) {
	docTestEnv(t, "doc-sess-2")
	params := map[string]any{
		"documents": []any{
			map[string]any{"filename": "virus.exe", "base64_data": b64([]byte("MZ"))},
			map[string]any{"filename": "尾空格绕过.exe ", "base64_data": b64([]byte("MZ"))},
			map[string]any{"filename": "no-source.txt"},
			map[string]any{"filename": "ok.txt", "base64_data": b64([]byte("fine"))},
		},
	}
	got := PersistAndParseDocuments(params, "doc-sess-2")
	errs, ok := got["document_errors"].([]any)
	if !ok || len(errs) != 3 {
		t.Fatalf("应回显 3 条错误: %v", got["document_errors"])
	}
	first := errs[0].(map[string]any)
	if !strings.Contains(first["error"].(string), "Forbidden document type") {
		t.Fatalf("黑名单错误文案不符: %v", first)
	}
	// 尾空格名字先消毒再校验黑名单——"尾空格绕过.exe " 消毒为 "尾空格绕过.exe" 命中
	second := errs[1].(map[string]any)
	if !strings.Contains(second["error"].(string), "Forbidden document type") {
		t.Fatalf("尾空格绕过应被消毒后拦截: %v", second)
	}
	third := errs[2].(map[string]any)
	if third["error"].(string) != ErrMissingDocSource.Error() {
		t.Fatalf("缺源错误文案不符: %v", third)
	}
	// 合法项仍落盘
	if got["media_items"].([]any)[0].(map[string]any)["filename"] != "ok.txt" {
		t.Fatalf("合法项应落盘: %v", got)
	}
}

// TestPersistAndParseDocuments_超限回显 对齐 max_count 不静默丢弃。
func TestPersistAndParseDocuments_超限回显(t *testing.T) {
	docTestEnv(t, "doc-sess-3")
	docs := make([]any, MaxDocumentCount+2)
	for i := range docs {
		docs[i] = map[string]any{"filename": "d.txt", "base64_data": b64([]byte("x"))}
	}
	params := map[string]any{"documents": docs}
	got := PersistAndParseDocuments(params, "doc-sess-3")
	errs := got["document_errors"].([]any)
	if len(errs) != 2 {
		t.Fatalf("超限 2 条应回显错误: %d", len(errs))
	}
	if !strings.Contains(errs[0].(map[string]any)["error"].(string), "exceeds max document count (8)") {
		t.Fatalf("超限文案不符: %v", errs[0])
	}
	if got["media_items"].([]any) == nil || len(got["media_items"].([]any)) != MaxDocumentCount {
		t.Fatalf("上限内 %d 条应落盘", MaxDocumentCount)
	}
}

// TestPersistAndParseDocuments_本地path校验 对齐 :247-280。
func TestPersistAndParseDocuments_本地path校验(t *testing.T) {
	docTestEnv(t, "doc-sess-4")
	dir := t.TempDir()
	local := filepath.Join(dir, "report.md")
	if err := os.WriteFile(local, []byte("# report"), 0o644); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"documents": []any{
		map[string]any{"path": local},
		map[string]any{"path": filepath.Join(dir, "ghost.md")},
		map[string]any{"path": filepath.Join(dir, "evil.EXE")},
	}}
	got := PersistAndParseDocuments(params, "doc-sess-4")
	items := got["media_items"].([]any)
	if len(items) != 1 {
		t.Fatalf("仅本地合法项应通过: %v", items)
	}
	if items[0].(map[string]any)["path"] != local {
		t.Fatalf("本地引用 path 不符: %v", items[0])
	}
	errs := got["document_errors"].([]any)
	if len(errs) != 2 {
		t.Fatalf("缺失/黑名单两项应报错: %v", errs)
	}
	if !strings.Contains(errs[0].(map[string]any)["error"].(string), "File not found") {
		t.Fatalf("缺失文案不符: %v", errs[0])
	}
}

// TestPersistAndParseDocuments_mediaItems混入图片保留 对齐 :121-129。
func TestPersistAndParseDocuments_mediaItems混入图片保留(t *testing.T) {
	uploads := docTestEnv(t, "doc-sess-5")
	img := filepath.Join(uploads, "keep.png")
	if err := os.WriteFile(img, tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{
		"media_items": []any{
			map[string]any{"type": "image", "filename": "keep.png", "path": img},
			map[string]any{"type": "document", "filename": "doc.txt", "base64_data": b64([]byte("d"))},
		},
	}
	got := PersistAndParseDocuments(params, "doc-sess-5")
	items := got["media_items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["type"] != "image" {
		t.Fatalf("图片记录应保留在文档记录之前: %v", items)
	}
}

// TestPersistAndParseDocuments_无文档删除key 对齐 :66-68。
func TestPersistAndParseDocuments_无文档删除key(t *testing.T) {
	docTestEnv(t, "doc-sess-6")
	params := map[string]any{"documents": []any{
		map[string]any{"type": "text", "text": "非文档项"},
	}}
	got := PersistAndParseDocuments(params, "doc-sess-6")
	if _, exists := got["documents"]; exists {
		t.Fatalf("无有效文档应删除 documents key: %v", got)
	}
}

// TestPersistAndParseDocuments_超大小文档 对齐 :302-305。
func TestPersistAndParseDocuments_超大小文档(t *testing.T) {
	docTestEnv(t, "doc-sess-7")
	huge := make([]byte, MaxDocumentBytes+1)
	params := map[string]any{"documents": []any{
		map[string]any{"filename": "big.txt", "base64_data": b64(huge)},
	}}
	got := PersistAndParseDocuments(params, "doc-sess-7")
	errs := got["document_errors"].([]any)
	if !strings.Contains(errs[0].(map[string]any)["error"].(string), "Document too large") {
		t.Fatalf("超限文案不符: %v", errs[0])
	}
}
