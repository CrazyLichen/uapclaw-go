package token

import (
	"encoding/base64"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// png1024x768Base64 最小 PNG 头（IHDR 声明 1024×768，后续截断不影响解析）
const png1024x768HeaderBase64 = "iVBORw0KGgoAAAANSUhEUgAABAAAAAMACAYAAAC6uhUNAAAAAXNSR0IArs4c6QAA"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEstimateImageTokens_取两种计费较大者 验证 OpenAI 分块与 Claude patch 取大。
func TestEstimateImageTokens_取两种计费较大者(t *testing.T) {
	if got := EstimateImageTokens(0, 100); got != DefaultImagePlaceholderTokens {
		t.Fatalf("非法尺寸应返回占位值，got %d", got)
	}
	// 1024×768：OpenAI = 85 + 170*2*2 = 765；Claude = ceil(1024/28)*ceil(768/28) = 37*28 = 1036
	if got := EstimateImageTokens(1024, 768); got != 1036 {
		t.Fatalf("1024x768 应取 Claude patch 值 1036，got %d", got)
	}
}

// TestImageBlockTokens_detailLow与显式尺寸 验证计费优先级。
func TestImageBlockTokens_detailLow与显式尺寸(t *testing.T) {
	low := llm_schema.ContentPart{
		Type:     "image_url",
		ImageURL: &llm_schema.ImageURL{URL: "https://example.com/a.png", Detail: "low"},
	}
	if got := ImageBlockTokens(low); got != 85 {
		t.Fatalf("detail=low 应固定 85，got %d", got)
	}

	width, height := 512, 512
	explicit := llm_schema.ContentPart{Type: "image", Width: &width, Height: &height}
	if got := ImageBlockTokens(explicit); got != EstimateImageTokens(512, 512) {
		t.Fatalf("显式尺寸应走公式，got %d", got)
	}

	dimensions := llm_schema.ContentPart{Type: "image", Dimensions: "512x512"}
	if got := ImageBlockTokens(dimensions); got != EstimateImageTokens(512, 512) {
		t.Fatalf("dimensions 字段应等价于显式尺寸，got %d", got)
	}
}

// TestImageBlockTokens_base64头嗅探 验证读不到显式尺寸时解 PNG 头。
func TestImageBlockTokens_base64头嗅探(t *testing.T) {
	part := llm_schema.ContentPart{
		Type:     "image_url",
		ImageURL: &llm_schema.ImageURL{URL: "data:image/png;base64," + png1024x768HeaderBase64},
	}
	if got := ImageBlockTokens(part); got != 1036 {
		t.Fatalf("应按 PNG 头 1024x768 计费 1036，got %d", got)
	}
}

// TestImageBlockTokens_远程URL与坏数据回退占位 验证兜底路径。
func TestImageBlockTokens_远程URL与坏数据回退占位(t *testing.T) {
	remote := llm_schema.ContentPart{
		Type:     "image_url",
		ImageURL: &llm_schema.ImageURL{URL: "https://example.com/a.png"},
	}
	if got := ImageBlockTokens(remote); got != DefaultImagePlaceholderTokens {
		t.Fatalf("远程图读不到尺寸应回退 1445，got %d", got)
	}

	broken := llm_schema.ContentPart{
		Type:     "image_url",
		ImageURL: &llm_schema.ImageURL{URL: "data:image/png;base64,@@@@"},
	}
	if got := ImageBlockTokens(broken); got != DefaultImagePlaceholderTokens {
		t.Fatalf("坏 base64 应回退 1445，got %d", got)
	}
}

// TestImageBlockTokens_环境变量覆盖 验证运维标定值最高优先。
func TestImageBlockTokens_环境变量覆盖(t *testing.T) {
	t.Setenv(imagePlaceholderEnv, "2000")
	part := llm_schema.ContentPart{Type: "image", Dimensions: "512x512"}
	if got := ImageBlockTokens(part); got != 2000 {
		t.Fatalf("环境变量应覆盖为 2000，got %d", got)
	}

	t.Setenv(imagePlaceholderEnv, "abc")
	if got := ImageBlockTokens(part); got == 2000 {
		t.Fatalf("非法环境变量应被忽略")
	}
}

// TestIsImagePart 覆盖图像块识别。
func TestIsImagePart(t *testing.T) {
	if !IsImagePart(llm_schema.ContentPart{Type: "image_url"}) || !IsImagePart(llm_schema.ContentPart{Type: "image"}) {
		t.Fatal("image/image_url 应识别为图像块")
	}
	if IsImagePart(llm_schema.ContentPart{Type: "text", Text: "x"}) {
		t.Fatal("文本块不应识别为图像块")
	}
}

// TestCountMessages_图像按尺寸计费不外溢 验证大 base64 不会被当文本计数。
func TestCountMessages_图像按尺寸计费不外溢(t *testing.T) {
	counter := NewTiktokenCounter("gpt-4o")
	payload := base64.StdEncoding.EncodeToString(make([]byte, 300000))
	width, height := 512, 512
	msg := llm_schema.NewUserMessage("", llm_schema.WithMultiModalContent(llm_schema.ContentPart{
		Type:     "image_url",
		ImageURL: &llm_schema.ImageURL{URL: "data:image/png;base64," + payload},
		Width:    &width,
		Height:   &height,
	}))
	got, err := counter.CountMessages([]llm_schema.BaseMessage{msg}, "gpt-4o")
	if err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if got > 2000 {
		t.Fatalf("300KB base64 应按尺寸计费（约 1k），got %d —— 载荷被当文本了", got)
	}
}
