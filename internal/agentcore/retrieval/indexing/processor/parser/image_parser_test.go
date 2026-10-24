package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
)

// ──────────────────────────── ImageParser 测试 ────────────────────────────

// newImageParserOrFatal 构造 ImageParser。
func newImageParserOrFatal(t *testing.T) *ImageParser {
	t.Helper()
	p, err := NewImageParser()
	if err != nil {
		t.Fatalf("构造 ImageParser 失败: %v", err)
	}
	return p
}

// TestImageParser_Parse_正常 测试完整链路：归档 + caption + image_path 元数据。
func TestImageParser_Parse_正常(t *testing.T) {
	// 切工作目录使 images/ 落在临时目录
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "photo.png")
	model := newCaptionModel(t, nil, nil) // 默认返回 "default caption"

	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), src, WithDocID("doc-1"), WithLLMClient(model))
	if err != nil {
		t.Fatalf("Parse 不应报错: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("应返回 1 个 Document，实际 %d", len(docs))
	}
	d := docs[0]
	if d.ID != "doc-1" {
		t.Errorf("ID 应透传 doc-1，实际 %q", d.ID)
	}
	if d.Text != "default caption" {
		t.Errorf("Text 应为 caption，实际 %q", d.Text)
	}
	gotPath, _ := d.Metadata["image_path"].(string)
	if gotPath != filepath.Join("images", "photo.png") {
		t.Errorf("image_path 应为归档路径，实际 %q", gotPath)
	}
}

// TestImageParser_Parse_禁用态 ★核心用例：nil client 仍返回单 Document。
func TestImageParser_Parse_禁用态(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "disabled.png")

	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), src)
	if err != nil {
		t.Fatalf("禁用态不应报错: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("禁用态仍应返回 1 个 Document（空文本走 13.4 纯图片嵌入），实际 %d", len(docs))
	}
	if docs[0].Text != "" {
		t.Errorf("禁用态 Text 应为空串，实际 %q", docs[0].Text)
	}
	if _, ok := docs[0].Metadata["image_path"]; !ok {
		t.Error("禁用态 metadata.image_path 必须存在")
	}
}

// TestImageParser_Parse_源不存在 测试软失败返回空切片。
func TestImageParser_Parse_源不存在(t *testing.T) {
	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), "/no/such/file.png")
	if err != nil {
		t.Fatalf("软失败不应报错: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("失败应返回空切片，实际 %d", len(docs))
	}
}

// TestImageParser_Parse_B1归档路径直通 测试 caption 读的是归档副本。
func TestImageParser_Parse_B1归档路径直通(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "b1.png")
	var invoked bool
	model := newCaptionModel(t, nil, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		invoked = true
		return llmschema.NewAssistantMessage("cap"), nil
	})

	p := newImageParserOrFatal(t)
	docs, err := p.Parse(t.Context(), src, WithLLMClient(model))
	if err != nil || len(docs) != 1 {
		t.Fatalf("Parse 应成功: %v, %d docs", err, len(docs))
	}
	if !invoked {
		t.Fatal("LLM 应被调用一次")
	}
	// 副本存在且与原件一致（caption 走副本）
	copied := filepath.Join("images", "b1.png")
	if _, err := os.Stat(copied); err != nil {
		t.Fatalf("归档副本应存在: %v", err)
	}
}

// TestImageParser_注册 测试六扩展名已注册（init 副作用）。
func TestImageParser_注册(t *testing.T) {
	want := []string{".png", ".jpg", ".jpeg", ".webp", ".gif", ".jfif"}
	for _, ext := range want {
		if _, ok := getParserFactory(ext); !ok {
			t.Errorf("扩展名 %s 应已注册", ext)
		}
	}
	// 工厂产出正确类型
	factory, _ := getParserFactory(".png")
	if _, ok := factory().(*ImageParser); !ok {
		t.Error("工厂应产出 *ImageParser")
	}
}

// TestImageParser_LazyParse转发 测试继承 BaseParser 默认实现。
func TestImageParser_LazyParse转发(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "lazy.png")
	p := newImageParserOrFatal(t)

	var seen []string
	err := p.LazyParse(t.Context(), src, func(d reranker.Document) error {
		seen = append(seen, d.Text)
		return nil
	})
	if err != nil {
		t.Fatalf("LazyParse 不应报错: %v", err)
	}
	if len(seen) != 1 {
		t.Fatalf("禁用态 LazyParse 应回调 1 次（空文本 Document 也产出），实际 %d", len(seen))
	}
}
