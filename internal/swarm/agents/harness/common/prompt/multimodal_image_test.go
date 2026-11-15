package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── IsImageContentBlock ────────────────────────────

// TestIsImageContentBlock 对齐 Python is_image_content_block:35-42。
func TestIsImageContentBlock(t *testing.T) {
	cases := []struct {
		part  llmschema.ContentPart
		expect bool
	}{
		{llmschema.ContentPart{Type: "image_url", ImageURL: &llmschema.ImageURL{URL: "data:image/png;base64,x"}}, true},
		{llmschema.ContentPart{Type: "image"}, true},
		{llmschema.ContentPart{Type: "input_image"}, true},
		{llmschema.ContentPart{Type: "IMAGE_URL"}, true},
		{llmschema.ContentPart{Type: "text", Text: "hello"}, false},
		{llmschema.ContentPart{Type: ""}, false},
	}
	for i, c := range cases {
		if got := IsImageContentBlock(c.part); got != c.expect {
			t.Fatalf("case %d: got %v", i, got)
		}
	}
}

// ──────────────────────────── ExtractMultimodalImageFiles ────────────────────────────

// TestExtractMultimodalImageFiles_mediaItems与uploadedImages 对齐 :44-73 双来源。
func TestExtractMultimodalImageFiles_mediaItems与uploadedImages(t *testing.T) {
	params := map[string]any{
		"media_items": []any{
			map[string]any{"type": "image", "path": "/tmp/a.png", "mime_type": "image/png", "filename": "a.png"},
		},
		"files": map[string]any{
			"uploaded_images": []any{
				map[string]any{"path": "/tmp/b.jpg", "mime_type": "image/jpeg"},
			},
		},
	}
	files := ExtractMultimodalImageFiles(params)
	if len(files) != 2 {
		t.Fatalf("期望 2 条，得 %d: %+v", len(files), files)
	}
	if files[0].Type != "image" || files[0].Path != "/tmp/a.png" || files[0].Filename != "a.png" {
		t.Fatalf("media_items 归一化错: %+v", files[0])
	}
	if files[1].Filename != "b.jpg" {
		t.Fatalf("filename 兜底取 base: %+v", files[1])
	}
}

// TestExtractMultimodalImageFiles_过滤与去重 对齐 :323-351 归一化规则与 :63-70 去重。
func TestExtractMultimodalImageFiles_过滤与去重(t *testing.T) {
	params := map[string]any{
		"media_items": []any{
			"not-a-map",                                                             // 非 dict → 丢
			map[string]any{"type": "video", "path": "/tmp/v.mp4"},                   // type 非 image → 丢
			map[string]any{"path": ""},                                              // 无 path → 丢
			map[string]any{"path": "/tmp/t.txt"},                                    // mime 不在白名单 → 丢
			map[string]any{"path": "/tmp/d.webp"},                                   // mime 缺失按扩展名补 webp
			map[string]any{"path": "/tmp/a.png", "mime_type": "image/png"},          // 第一份
			map[string]any{"path": "/tmp/a.png", "mime_type": "image/png", "size_bytes": int64(3)}, // 重复 path → 丢
		},
	}
	files := ExtractMultimodalImageFiles(params)
	if len(files) != 2 {
		t.Fatalf("期望 2 条（d.webp + a.png），得 %d: %+v", len(files), files)
	}
	if files[0].MimeType != "image/webp" {
		t.Fatalf("扩展名推断错: %v", files[0].MimeType)
	}
	if files[1].SizeBytes != 0 {
		t.Fatalf("重复条目不应进入: %+v", files[1])
	}
	// params 非 dict / nil → 空
	if got := ExtractMultimodalImageFiles(nil); len(got) != 0 {
		t.Fatal("nil 应返回空")
	}
}

// TestExtractMultimodalImageFiles_sizeBytes透传 对齐 :349-351 仅 int 透传。
func TestExtractMultimodalImageFiles_sizeBytes透传(t *testing.T) {
	params := map[string]any{"media_items": []any{
		map[string]any{"path": "/tmp/a.png", "mime_type": "image/png", "size_bytes": int64(123)},
	}}
	files := ExtractMultimodalImageFiles(params)
	if files[0].SizeBytes != 123 {
		t.Fatalf("size_bytes = %d", files[0].SizeBytes)
	}
}

// ──────────────────────────── LatestUserMessageIndex ────────────────────────────

// TestLatestUserMessageIndex 对齐 :353-368。
func TestLatestUserMessageIndex(t *testing.T) {
	mk := func(role llmschema.RoleType) llmschema.BaseMessage {
		return llmschema.NewDefaultMessage(role, "x")
	}
	msgs := []llmschema.BaseMessage{
		mk(llmschema.RoleTypeSystem), mk(llmschema.RoleTypeUser), mk(llmschema.RoleTypeAssistant), mk(llmschema.RoleTypeUser),
	}
	if got := LatestUserMessageIndex(msgs); got != 3 {
		t.Fatalf("index = %d", got)
	}
	if got := LatestUserMessageIndex(nil); got != -1 {
		t.Fatalf("空应 -1: %d", got)
	}
	if got := LatestUserMessageIndex([]llmschema.BaseMessage{mk(llmschema.RoleTypeSystem)}); got != -1 {
		t.Fatalf("无 user 应 -1: %d", got)
	}
}

// ──────────────────────────── ImageDataURIFromPath ────────────────────────────

// TestImageDataURIFromPath_正常编码 对齐 :433-447 成功路径。
func TestImageDataURIFromPath_正常编码(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.png")
	if err := os.WriteFile(p, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := ImageDataURIFromPath(p, "image/png")
	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("URI 前缀错: %q", uri[:min(40, len(uri))])
	}
	// base64 内容与文件字节一致由 encoding/base64 保证，此处断言非空正文
	if len(uri) <= len("data:image/png;base64,") {
		t.Fatal("正文为空")
	}
}

// TestImageDataURIFromPath_失败路径 对齐 :433-447 四个空串分支。
func TestImageDataURIFromPath_失败路径(t *testing.T) {
	if got := ImageDataURIFromPath("/nonexistent/x.png", "image/png"); got != "" {
		t.Fatalf("文件不存在应空串: %q", got)
	}
	p := filepath.Join(t.TempDir(), "x.png")
	_ = os.WriteFile(p, []byte("data"), 0o644)
	if got := ImageDataURIFromPath(p, "text/plain"); got != "" {
		t.Fatalf("mime 超白名单应空串: %q", got)
	}
	if got := ImageDataURIFromPath("", "image/png"); got != "" {
		t.Fatalf("空路径应空串: %q", got)
	}
	// 空文件 → 空串
	empty := filepath.Join(t.TempDir(), "e.png")
	_ = os.WriteFile(empty, nil, 0o644)
	if got := ImageDataURIFromPath(empty, "image/png"); got != "" {
		t.Fatalf("空文件应空串: %q", got)
	}
}
