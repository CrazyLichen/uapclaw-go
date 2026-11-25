package attachments

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// steerTestEnv 隔离 DATA_DIR，建会话 uploads 并放一张合法 PNG，返回 (uploads, pngPath)。
func steerTestEnv(t *testing.T, sid string) (string, string) {
	t.Helper()
	uploads := docTestEnv(t, sid)
	p := filepath.Join(uploads, "steer.png")
	if err := os.WriteFile(p, tinyPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	return uploads, p
}

// steerRef 构造 steer 引用条目。
func steerRef(path, filename, mime string) map[string]any {
	return map[string]any{"type": "image", "path": path, "filename": filename, "mime_type": mime}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestValidateSteerImageReferences_形状校验 对齐 session_input.py:62-82。
func TestValidateSteerImageReferences_形状校验(t *testing.T) {
	// 携带 files → 拒绝（steer 只收上传图片引用）
	_, err := ValidateSteerImageReferences(map[string]any{"files": []any{"x"}})
	if err == nil || !strings.Contains(err.Error(), "queued task") {
		t.Fatalf("files 字段应拒绝: %v", err)
	}
	// 无 media_items → 空
	out, err := ValidateSteerImageReferences(map[string]any{"query": "hi"})
	if err != nil || out != nil {
		t.Fatalf("无 media_items 应返回空: %v %v", out, err)
	}
	// media_items 非 list
	_, err = ValidateSteerImageReferences(map[string]any{"media_items": "x"})
	if err == nil || !strings.Contains(err.Error(), "must be a list") {
		t.Fatalf("非 list 文案不符: %v", err)
	}
	// 条目带字段集外字段（内联数据）
	_, err = ValidateSteerImageReferences(map[string]any{"media_items": []any{
		map[string]any{"type": "image", "path": "/p", "filename": "a.png", "mime_type": "image/png", "base64_data": "xx"},
	}})
	if err == nil || !strings.Contains(err.Error(), "without inline data") {
		t.Fatalf("内联数据应拒绝: %v", err)
	}
	// type 非 image
	_, err = ValidateSteerImageReferences(map[string]any{"media_items": []any{
		map[string]any{"type": "document", "path": "/p", "filename": "a.txt", "mime_type": "text/plain"},
	}})
	if err == nil || !strings.Contains(err.Error(), "uploaded images only") {
		t.Fatalf("非图片应拒绝: %v", err)
	}
	// mime 不在白名单
	_, err = ValidateSteerImageReferences(map[string]any{"media_items": []any{
		steerRef("/p/a.tiff", "a.tiff", "image/tiff"),
	}})
	if err == nil || !strings.Contains(err.Error(), "unsupported steer image MIME") {
		t.Fatalf("MIME 白名单应拒绝: %v", err)
	}
}

// TestResolveSteerImageReferences_正常与伪造mime 对齐 _resolve_image 主链。
func TestResolveSteerImageReferences_正常与伪造mime(t *testing.T) {
	_, pngPath := steerTestEnv(t, "steer-sess-1")

	// 声明一致 → 通过，尺寸以磁盘为准
	out, err := ResolveSteerImageReferences(map[string]any{"media_items": []any{
		steerRef(pngPath, "steer.png", "image/png"),
	}}, "steer-sess-1")
	if err != nil {
		t.Fatalf("合法引用应通过: %v", err)
	}
	if out[0]["mime_type"] != "image/png" || out[0]["size_bytes"].(int) != len(tinyPNG) {
		t.Fatalf("规范化记录不符: %v", out[0])
	}

	// 伪造 mime：png 内容声明 image/jpeg → 拒绝
	_, err = ResolveSteerImageReferences(map[string]any{"media_items": []any{
		steerRef(pngPath, "steer.png", "image/jpeg"),
	}}, "steer-sess-1")
	if err == nil || !strings.Contains(err.Error(), "must match its actual image format") {
		t.Fatalf("伪造 mime 应拒绝: %v", err)
	}
}

// TestResolveSteerImageReferences_路径边界 对齐 :71-79。
func TestResolveSteerImageReferences_路径边界(t *testing.T) {
	uploads, pngPath := steerTestEnv(t, "steer-sess-2")

	cases := []struct {
		name string
		path string
		want string
	}{
		{"跨会话目录", filepath.Join(filepath.Dir(uploads), "other", "steer.png"), "outside the current session uploads"},
		{"相对路径", "steer.png", "outside the current session uploads"},
		{"路径含点点", pngPath + "/../steer.png", "outside the current session uploads"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveSteerImageReferences(map[string]any{"media_items": []any{
				steerRef(tc.path, filepath.Base(tc.path), "image/png"),
			}}, "steer-sess-2")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want 含 %q", err, tc.want)
			}
		})
	}
	// filename 与 path 不符
	_, err := ResolveSteerImageReferences(map[string]any{"media_items": []any{
		steerRef(pngPath, "other.png", "image/png"),
	}}, "steer-sess-2")
	if err == nil || !strings.Contains(err.Error(), "does not match its uploaded path") {
		t.Fatalf("文件名不符应拒绝: %v", err)
	}
}

// TestResolveSteerImageReferences_空会话与缺失文件 对齐 :112-126。
func TestResolveSteerImageReferences_空会话与缺失文件(t *testing.T) {
	uploads, pngPath := steerTestEnv(t, "steer-sess-3")

	// 空会话 id
	_, err := ResolveSteerImageReferences(map[string]any{"media_items": []any{
		steerRef(pngPath, "steer.png", "image/png"),
	}}, "  ")
	if err == nil || err.Error() != "image steering requires a current session" {
		t.Fatalf("空会话文案不符: %v", err)
	}
	// 文件缺失 → 统一重传文案
	ghost := filepath.Join(uploads, "ghost.png")
	_, err = ResolveSteerImageReferences(map[string]any{"media_items": []any{
		steerRef(ghost, "ghost.png", "image/png"),
	}}, "steer-sess-3")
	if err == nil || err.Error() != "steer image upload is missing or cannot be read; upload it again" {
		t.Fatalf("缺失文件文案不符: %v", err)
	}
	// symlink 拒绝
	link := filepath.Join(uploads, "link.png")
	if err := os.Symlink(pngPath, link); err == nil {
		_, err := ResolveSteerImageReferences(map[string]any{"media_items": []any{
			steerRef(link, "link.png", "image/png"),
		}}, "steer-sess-3")
		if err == nil || !strings.Contains(err.Error(), "symlinks or junctions") {
			t.Fatalf("symlink 应拒绝: %v", err)
		}
	}
}

// TestProbeImageMime_格式与像素门 解码探测矩阵。
func TestProbeImageMime_格式与像素门(t *testing.T) {
	// 非图片字节
	if _, err := probeImageMime([]byte("not an image at all")); err == nil ||
		err.Error() != "steer image is invalid or exceeds the safe decoded size" {
		t.Fatalf("非图片文案不符: %v", err)
	}
	// jpeg
	jpeg := makeJPEG(t)
	mt, err := probeImageMime(jpeg)
	if err != nil || mt != "image/jpeg" {
		t.Fatalf("jpeg 探测: %q %v", mt, err)
	}
	// jpeg 声明 .jfif 别名后缀由调用方校验（validSuffix 分支已覆盖）
	// gif
	gif := makeGIF(t)
	if mt, err := probeImageMime(gif); err != nil || mt != "image/gif" {
		t.Fatalf("gif 探测: %q %v", mt, err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeJPEG 生成 1x1 JPEG。
func makeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// makeGIF 生成 1x1 GIF。
func makeGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
