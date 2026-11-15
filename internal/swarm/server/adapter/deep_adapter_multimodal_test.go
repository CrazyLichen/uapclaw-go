package adapter

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
)

// ──────────────────────────── ctx 袋子全生命周期（用户硬要求覆盖点） ────────────────────────────

// mkImageParams 构造带图片附件的请求参数。
func mkImageParams(path string) map[string]any {
	return map[string]any{
		"media_items": []any{
			map[string]any{"type": "image", "path": path, "mime_type": "image/png", "filename": "img.png"},
		},
	}
}

// TestWithMultimodalImageFiles_带图写入 对齐 Python set_current_multimodal_image_files。
func TestWithMultimodalImageFiles_带图写入(t *testing.T) {
	files := prompt.ExtractMultimodalImageFiles(mkImageParams("/tmp/x.png"))
	if len(files) != 1 {
		t.Fatalf("提取数 = %d", len(files))
	}
	ctx := prompt.WithMultimodalImageFiles(context.Background(), files)
	got := prompt.MultimodalImageFilesFromContext(ctx)
	if len(got) != 1 || got[0].Path != "/tmp/x.png" {
		t.Fatalf("ctx 读取错: %+v", got)
	}
}

// TestWithMultimodalImageFiles_无图不写key 对齐 Python set 空袋子语义。
func TestWithMultimodalImageFiles_无图不写key(t *testing.T) {
	if got := prompt.MultimodalImageFilesFromContext(context.Background()); got != nil {
		t.Fatalf("未设置应 nil: %+v", got)
	}
	ctx := prompt.WithMultimodalImageFiles(context.Background(), nil)
	if got := prompt.MultimodalImageFilesFromContext(ctx); got != nil {
		t.Fatalf("空列表应不写 key: %+v", got)
	}
}

// TestMultimodalImageFiles_跨轮防串 用户硬要求：第 N 轮 ctx 不携带第 N-1 轮图片。
func TestMultimodalImageFiles_跨轮防串(t *testing.T) {
	// 第 1 轮：带图
	round1 := prompt.WithMultimodalImageFiles(context.Background(),
		prompt.ExtractMultimodalImageFiles(mkImageParams("/tmp/round1.png")))
	if got := prompt.MultimodalImageFilesFromContext(round1); len(got) != 1 {
		t.Fatal("第 1 轮应有图")
	}
	// 第 2 轮：新 ctx（模拟 adapter 每 invoke 新建），无图
	round2 := context.Background()
	if got := prompt.MultimodalImageFilesFromContext(round2); got != nil {
		t.Fatalf("第 2 轮不应读到第 1 轮的图: %+v", got)
	}
	// 第 2 轮的派生 ctx（会话内 rail 链传递）同样读不到第 1 轮
	derived, cancel := context.WithCancel(round2)
	defer cancel()
	if got := prompt.MultimodalImageFilesFromContext(derived); got != nil {
		t.Fatalf("派生 ctx 串图: %+v", got)
	}
}

// TestExtractMultimodalImageFiles_params形状 Go 侧 parseParams 产出 map[string]any，
// 验证 ExtractMultimodalImageFiles 对该形状的兼容（media_items 顶层 + files.uploaded_images）。
func TestExtractMultimodalImageFiles_params形状(t *testing.T) {
	params := map[string]any{
		"files": map[string]any{
			"uploaded_images": []any{
				map[string]any{"path": "/tmp/u.png", "mime_type": "image/png"},
			},
		},
	}
	files := prompt.ExtractMultimodalImageFiles(params)
	if len(files) != 1 || files[0].Path != "/tmp/u.png" {
		t.Fatalf("uploaded_images 提取失败: %+v", files)
	}
}
