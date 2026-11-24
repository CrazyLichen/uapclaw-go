package attachments

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// tinyPNG 1x1 PNG（合法图片头，供落盘与格式探测用例复用）。
var tinyPNG = func() []byte {
	b, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	return b
}()

// mediaTestEnv 隔离 DATA_DIR 并返回会话 uploads 目录。
func mediaTestEnv(t *testing.T, sid string) string {
	t.Helper()
	uploads := filepath.Join(t.TempDir(), "agent", "sessions", sid, "uploads")
	t.Setenv("UAPCLAW_DATA_DIR", filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(uploads)))))
	return uploads
}

// b64 图片 base64 编码助手。
func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNormalizeChatMediaAttachments_落盘与回填 对齐 normalize_chat_media_attachments 主链。
// Python: media_attachments.py:95-147。
func TestNormalizeChatMediaAttachments_落盘与回填(t *testing.T) {
	uploads := mediaTestEnv(t, "media-sess-1")
	params := map[string]any{
		"content": "看看这张图",
		"media_items": []any{
			map[string]any{"type": "image", "filename": "cat.png", "mime_type": "image/png", "base64_data": b64(tinyPNG)},
			map[string]any{"type": "text", "text": "非图片项透传跳过"},
			"非 dict 项跳过",
		},
	}
	NormalizeChatMediaAttachments(params, "media-sess-1")

	items, ok := params["media_items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("应只剩 1 条 stored 图片记录: %v", params["media_items"])
	}
	stored := items[0].(map[string]any)
	path, _ := stored["path"].(string)
	if path == "" {
		t.Fatalf("stored 记录缺 path: %v", stored)
	}
	// 磁盘断言：真实落盘
	b, err := os.ReadFile(path)
	if err != nil || string(b) != string(tinyPNG) {
		t.Fatalf("落盘字节不符: %v", err)
	}
	if filepath.Dir(path) != uploads {
		t.Fatalf("落盘目录不符: %q, want %q", filepath.Dir(path), uploads)
	}
	// files.uploaded_images 四字段摘要
	files := params["files"].(map[string]any)
	sum := files["uploaded_images"].([]map[string]any)
	if len(sum) != 1 || sum[0]["filename"] != "cat.png" {
		t.Fatalf("uploaded_images 摘要不符: %v", files)
	}
}

// TestNormalizeChatMediaAttachments_全无效删除key 对齐 :138-140。
func TestNormalizeChatMediaAttachments_全无效删除key(t *testing.T) {
	mediaTestEnv(t, "media-sess-2")
	params := map[string]any{
		"media_items": []any{
			map[string]any{"type": "image", "mime_type": "image/heic", "base64_data": b64(tinyPNG)}, // mime 不支持
			map[string]any{"type": "image", "mime_type": "image/png", "base64_data": "!!非base64!!"},
		},
	}
	NormalizeChatMediaAttachments(params, "media-sess-2")
	if _, exists := params["media_items"]; exists {
		t.Fatalf("全无效应删除 media_items key: %v", params)
	}
}

// TestNormalizeChatMediaAttachments_上限8张 对齐 _MAX_IMAGE_COUNT 截断。
func TestNormalizeChatMediaAttachments_上限8张(t *testing.T) {
	mediaTestEnv(t, "media-sess-3")
	items := make([]any, 12)
	for i := range items {
		items[i] = map[string]any{"type": "image", "mime_type": "image/png", "base64_data": b64(tinyPNG)}
	}
	params := map[string]any{"media_items": items}
	NormalizeChatMediaAttachments(params, "media-sess-3")
	if got := len(params["media_items"].([]any)); got != MaxImageCount {
		t.Fatalf("应截断为 %d 张, 实际 %d", MaxImageCount, got)
	}
}

// TestNormalizeChatMediaAttachments_超限与空图 对齐 :176-179（>10MB 与空数据无效）。
func TestNormalizeChatMediaAttachments_超限与空图(t *testing.T) {
	mediaTestEnv(t, "media-sess-4")
	huge := make([]byte, MaxImageBytes+1)
	params := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "mime_type": "image/png", "base64_data": b64(huge)},
		map[string]any{"type": "image", "mime_type": "image/png", "base64_data": ""},
	}}
	NormalizeChatMediaAttachments(params, "media-sess-4")
	if _, exists := params["media_items"]; exists {
		t.Fatalf("超限/空图应判无效: %v", params)
	}
}

// TestNormalizeChatMediaAttachments_persisted透传 对齐 :163-175（分流已落盘项不重复写盘）。
func TestNormalizeChatMediaAttachments_persisted透传(t *testing.T) {
	mediaTestEnv(t, "media-sess-5")
	shared := filepath.Join(t.TempDir(), "shared", "big.png")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shared, tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "mime_type": "image/png", "_persisted": true, "path": shared},
		map[string]any{"type": "image", "mime_type": "image/png", "_persisted": true, "path": ""},
		map[string]any{"type": "image", "mime_type": "image/png", "_persisted": true, "path": "/nonexistent/ghost.png"},
	}}
	NormalizeChatMediaAttachments(params, "media-sess-5")
	items := params["media_items"].([]any)
	if len(items) != 1 {
		t.Fatalf("仅存在的 _persisted 项应保留: %v", params)
	}
	if items[0].(map[string]any)["path"] != shared {
		t.Fatalf("透传 path 不符: %v", items[0])
	}
}

// TestNormalizeChatMediaAttachments_jpeg别名保留原名 对齐 ensure_image_upload_filename。
func TestNormalizeChatMediaAttachments_jpeg别名保留原名(t *testing.T) {
	mediaTestEnv(t, "media-sess-6")
	params := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "filename": "sample.jpeg", "mime_type": "image/jpeg", "base64_data": b64(tinyPNG)},
	}}
	NormalizeChatMediaAttachments(params, "media-sess-6")
	stored := params["media_items"].([]any)[0].(map[string]any)
	if stored["filename"] != "sample.jpeg" {
		t.Fatalf(".jpeg 别名应保留原名防双重后缀: %v", stored)
	}
}

// TestDiscardSessionUpload_边界矩阵 对齐 discard_session_upload。
// Python: media_attachments.py:55-88。
func TestDiscardSessionUpload_边界矩阵(t *testing.T) {
	uploads := mediaTestEnv(t, "discard-sess")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "gone.png"
	target := filepath.Join(uploads, name)
	if err := os.WriteFile(target, tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}

	// 正常删除
	got, err := DiscardSessionUpload("discard-sess", target)
	if err != nil || got["deleted"] != true {
		t.Fatalf("删除失败: %v %v", got, err)
	}
	// 重复删除 = 成功 {deleted:false}
	got, err = DiscardSessionUpload("discard-sess", target)
	if err != nil || got["deleted"] != false {
		t.Fatalf("重复删除应 {deleted:false}: %v %v", got, err)
	}

	// 错误矩阵
	if _, err := DiscardSessionUpload("discard-sess", "  "); err == nil || err.Error() != "path is required" {
		t.Fatalf("空 path 文案不符: %v", err)
	}
	if _, err := DiscardSessionUpload("discard-sess", "rel.png"); err == nil || err.Error() != "path must be absolute" {
		t.Fatalf("相对路径文案不符: %v", err)
	}
	if _, err := DiscardSessionUpload("discard-sess", uploads+string(os.PathSeparator)+".."); err == nil || err.Error() != "invalid filename" {
		t.Fatalf("点点文件名文案不符: %v", err)
	}
	if _, err := DiscardSessionUpload("discard-sess", filepath.Join(t.TempDir(), "elsewhere.png")); err == nil ||
		err.Error() != "path is outside session uploads" {
		t.Fatalf("跨目录文案不符: %v", err)
	}
	// symlink 拒绝
	link := filepath.Join(uploads, "link.png")
	if err := os.Symlink(target, link); err == nil {
		// 先重建目标再建链（target 已删）
		if err := os.WriteFile(target, tinyPNG, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := DiscardSessionUpload("discard-sess", link); err == nil || err.Error() != "refusing to delete symlink" {
			t.Fatalf("symlink 应拒绝: %v", err)
		}
	}
	// 目录拒绝
	dirInUploads := filepath.Join(uploads, "subdir")
	if err := os.MkdirAll(dirInUploads, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscardSessionUpload("discard-sess", dirInUploads); err == nil || err.Error() != "path is not a file" {
		t.Fatalf("目录应拒绝: %v", err)
	}
}
