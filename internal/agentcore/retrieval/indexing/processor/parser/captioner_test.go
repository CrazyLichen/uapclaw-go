package parser

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// stubCaptionClient 惰性 mock：所有方法零行为，具体断言场景用 captionClient。
type stubCaptionClient struct{}

func (s *stubCaptionClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubCaptionClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubCaptionClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubCaptionClient) SupportsKVCacheRelease() bool { return false }

// newTestModel 构造带 stub 客户端的 Model（WithClient 注入，走真实 Invoke 链）。
func newTestModel(t *testing.T, modelCfg *llmschema.ModelRequestConfig) *llm.Model {
	t.Helper()
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	if err != nil {
		t.Fatalf("构造客户端配置失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, modelCfg, llm.WithClient(&stubCaptionClient{}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// writeTempImage 在临时目录写一个最小 PNG 字节文件，返回路径。
func writeTempImage(t *testing.T, name string) string {
	t.Helper()
	// 1x1 透明 PNG 真实字节（zlib 结构有效，供 base64 断言复用）
	pngBytes := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG 魔数
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR 长度+类型
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, // 8bit RGBA + CRC
		0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, // IDAT
		0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01, // 最小数据
		0x0D, 0x0A, 0x2D, 0xB4, // CRC
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82, // IEND
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
		t.Fatalf("写入测试图片失败: %v", err)
	}
	return path
}

// mustRead 读文件失败时 Fatal。
func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return data
}

// ──────────────────────────── CpImage 测试 ────────────────────────────

// TestCpImage_正常复制 测试图片复制到 images 目录且内容一致。
func TestCpImage_正常复制(t *testing.T) {
	// CpImage 的 images/ 相对当前工作目录，切到临时目录防污染仓库
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换工作目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "photo.png")

	dest, err := CpImage(src)
	if err != nil {
		t.Fatalf("CpImage 不应报错: %v", err)
	}
	if filepath.Base(dest) != "photo.png" {
		t.Errorf("目标文件名应为 photo.png，实际 %s", dest)
	}
	got, _ := os.ReadFile(dest)
	want := mustRead(t, src)
	if string(got) != string(want) {
		t.Error("副本内容应与原件一致")
	}
}

// TestCpImage_文件不存在 测试源文件不存在时报错。
func TestCpImage_文件不存在(t *testing.T) {
	_, err := CpImage(filepath.Join(t.TempDir(), "no_such.png"))
	if err == nil {
		t.Fatal("文件不存在应返回 error（对齐 Python FileNotFoundError）")
	}
}

// TestCpImage_源即目标 测试源文件已在 images 目录时跳过复制。
func TestCpImage_源即目标(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("切换工作目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "in_place.png")
	// 预先放入 images/ 使源=目标
	if err := os.MkdirAll("images", 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join("images", "in_place.png")
	if err := os.WriteFile(dest, mustRead(t, src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := CpImage(dest)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != dest {
		t.Errorf("源=目标时应原样返回 %s，实际 %s", dest, got)
	}
}

// TestCpImage_复制失败回落原路径 测试复制失败时软失败返回源路径。
func TestCpImage_复制失败回落原路径(t *testing.T) {
	// 源是目录：Stat 成功但 ReadFile 失败，构造复制失败分支
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "fake.png")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := CpImage(srcDir)
	if err != nil {
		t.Fatalf("复制失败应软失败不报错，实际: %v", err)
	}
	if got != srcDir {
		t.Errorf("复制失败应回落原路径 %s，实际 %s", srcDir, got)
	}
}

// ──────────────────────────── ImageCaptioner 构造测试 ────────────────────────────

// TestNewImageCaptioner_三类客户端 测试 nil/匹配名/不匹配名构造均不报错。
func TestNewImageCaptioner_三类客户端(t *testing.T) {
	// nil 客户端：禁用态，构造不报错
	c := NewImageCaptioner(nil)
	if c == nil {
		t.Fatal("nil client 构造应返回非 nil 实例")
	}

	// 匹配名：qwen3-vl 前缀命中白名单
	cfg := llmschema.NewModelRequestConfig(llmschema.WithModelName("qwen3-vl-plus"))
	matched := NewImageCaptioner(newTestModel(t, cfg))
	if matched == nil {
		t.Fatal("匹配名构造应返回非 nil 实例")
	}

	// 不匹配名：仅 warn 不报错
	cfg2 := llmschema.NewModelRequestConfig(llmschema.WithModelName("deepseek-r1"))
	unmatched := NewImageCaptioner(newTestModel(t, cfg2))
	if unmatched == nil {
		t.Fatal("不匹配名构造应返回非 nil 实例（仅 warn）")
	}
}

// ──────────────────────────── llmCall/CaptionImages 测试 ────────────────────────────

// captionInvokeFn Invoke 注入函数类型。
type captionInvokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)

// captionClient 可断言 Invoke 入参的 mock：记录收到的消息。
type captionClient struct {
	stubCaptionClient
	invokeFn captionInvokeFn
}

func (c *captionClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if c.invokeFn != nil {
		return c.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("default caption"), nil
}

// newCaptionModel 构造带断言 mock 的 Model。
func newCaptionModel(t *testing.T, modelCfg *llmschema.ModelRequestConfig, fn captionInvokeFn) *llm.Model {
	t.Helper()
	clientCfg, _ := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	model, err := llm.NewModel(clientCfg, modelCfg, llm.WithClient(&captionClient{invokeFn: fn}))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// TestCaptionImages_单图成功 核心用例：断言 LLM 收到正确的多模态消息。
func TestCaptionImages_单图成功(t *testing.T) {
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	src := writeTempImage(t, "photo.png")
	wantBytes := mustRead(t, src)

	var gotRole string
	var gotParts []llmschema.ContentPart
	model := newCaptionModel(t, nil, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		msgs := messages.Messages()
		if len(msgs) != 1 {
			t.Fatalf("应发 1 条消息，实际 %d", len(msgs))
		}
		gotRole = msgs[0].GetRole().String()
		gotParts = msgs[0].GetContent().Parts()
		return llmschema.NewAssistantMessage("A chart."), nil
	})

	c := NewImageCaptioner(model)
	got := c.CaptionImages(context.Background(), []string{src})
	if len(got) != 1 || got[0] != "A chart." {
		t.Fatalf("应返回 [A chart.]，实际 %v", got)
	}
	if gotRole != "user" {
		t.Errorf("role 应为 user，实际 %s", gotRole)
	}
	if len(gotParts) != 2 {
		t.Fatalf("content 应为 2 个分片，实际 %d", len(gotParts))
	}
	// 分片 1：提示词文本
	if gotParts[0].Type != "text" || !strings.HasPrefix(gotParts[0].Text, "You are an assistant") {
		t.Errorf("分片 1 应为提示词文本，实际 type=%s", gotParts[0].Type)
	}
	// 分片 2：data URI，base64 解码后等于原件字节
	if gotParts[1].Type != "image_url" || gotParts[1].ImageURL == nil {
		t.Fatalf("分片 2 应为 image_url，实际 %+v", gotParts[1])
	}
	url := gotParts[1].ImageURL.URL
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("data URI 前缀错误: %s", url[:40])
	}
	b64 := strings.TrimPrefix(url, "data:image/png;base64,")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	if string(decoded) != string(wantBytes) {
		t.Error("解码后字节应与图片原件一致")
	}
}

// TestCaptionImages_文件不存在 测试跳过不存在的图。
func TestCaptionImages_文件不存在(t *testing.T) {
	c := NewImageCaptioner(nil) // 任何 client：文件不存在分支在前
	got := c.CaptionImages(context.Background(), []string{"/no/such/file.png"})
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("不存在文件应返回 [\"\"]，实际 %v", got)
	}
}

// TestLLMCall_禁用态 测试 nil client 返回空串。
func TestLLMCall_禁用态(t *testing.T) {
	c := NewImageCaptioner(nil)
	if got := c.llmCall(context.Background(), "whatever.png"); got != "" {
		t.Errorf("禁用态应返回空串，实际 %q", got)
	}
}

// TestLLMCall_调用失败软失败 测试 Invoke 报错时返回空串。
func TestLLMCall_调用失败软失败(t *testing.T) {
	src := writeTempImage(t, "fail.png")
	model := newCaptionModel(t, nil, func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		return nil, errors.New("boom")
	})
	c := NewImageCaptioner(model)
	if got := c.llmCall(context.Background(), src); got != "" {
		t.Errorf("Invoke 失败应返回空串，实际 %q", got)
	}
}

// TestLLMCall_mime回落 测试无法识别扩展名时回落 image/png。
func TestLLMCall_mime回落(t *testing.T) {
	src := writeTempImage(t, "noext") // 无扩展名
	var gotURL string
	model := newCaptionModel(t, nil, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		parts := messages.Messages()[0].GetContent().Parts()
		gotURL = parts[1].ImageURL.URL
		return llmschema.NewAssistantMessage("x"), nil
	})
	c := NewImageCaptioner(model)
	_ = c.llmCall(context.Background(), src)
	if !strings.HasPrefix(gotURL, "data:image/png;base64,") {
		t.Errorf("无扩展名应回落 image/png，实际 %s", gotURL[:30])
	}
}
