package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMultimodalDocument(t *testing.T) {
	doc := NewMultimodalDocument()
	assert.NotNil(t, doc)
	assert.Empty(t, doc.Fields())
	assert.Equal(t, "", doc.Text)
}

func TestMultimodalDocument_AddField_文本(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityText, "你好世界")
	assert.Equal(t, "你好世界", doc.Text)
	assert.Len(t, doc.Fields(), 1)
	assert.Equal(t, ModalityText, doc.Fields()[0].Kind)
	assert.Equal(t, "你好世界", doc.Fields()[0].Data)
	assert.Equal(t, "", doc.Fields()[0].ID) // 文本模态无 ID
}

func TestMultimodalDocument_AddField_图片URL(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "描述").
		AddField(ModalityImage, "https://example.com/img.png")
	assert.Len(t, doc.Fields(), 2)
	assert.Equal(t, ModalityImage, doc.Fields()[1].Kind)
	assert.NotEmpty(t, doc.Fields()[1].ID) // 非文本模态有 ID
}

func TestMultimodalDocument_AddField_链式调用(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "文本").
		AddField(ModalityImage, "https://example.com/img.png")
	assert.Len(t, doc.Fields(), 2)
}

func TestMultimodalDocument_Content_文本(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityText, "描述文本")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "描述文本", content[0]["text"])
}

func TestMultimodalDocument_Content_图片URL(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityImage, "https://example.com/img.png")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "image_url", content[0]["type"])
	urlMap := content[0]["image_url"].(map[string]any)
	assert.Equal(t, "https://example.com/img.png", urlMap["url"])
}

func TestMultimodalDocument_Content_视频URL(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityVideo, "https://example.com/video.mp4")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "video_url", content[0]["type"])
	urlMap := content[0]["video_url"].(map[string]any)
	assert.Equal(t, "https://example.com/video.mp4", urlMap["url"])
}

func TestMultimodalDocument_Content_音频Base64(t *testing.T) {
	doc := NewMultimodalDocument().AddField(ModalityAudio, "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEARKwAAIhYAQACABAAZGF0YQAAAAA=")
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "input_audio", content[0]["type"])
	audioMap := content[0]["input_audio"].(map[string]any)
	assert.Equal(t, "wav", audioMap["format"])
}

func TestMultimodalDocument_DashscopeInput_文本和图片(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "描述").
		AddField(ModalityImage, "https://example.com/img.png")
	input := doc.DashscopeInput()
	assert.Equal(t, "描述", input["text"])
	assert.Equal(t, "https://example.com/img.png", input["image"])
}

func TestMultimodalDocument_DashscopeInput_视频URL(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityText, "描述").
		AddField(ModalityVideo, "https://example.com/video.mp4")
	input := doc.DashscopeInput()
	assert.Equal(t, "https://example.com/video.mp4", input["video"])
}

func TestMultimodalDocument_DashscopeInput_多图片(t *testing.T) {
	doc := NewMultimodalDocument().
		AddField(ModalityImage, "https://example.com/img1.png").
		AddField(ModalityImage, "https://example.com/img2.png")
	input := doc.DashscopeInput()
	images, ok := input["multi_images"].([]string)
	assert.True(t, ok)
	assert.Len(t, images, 2)
}

func TestMultimodalDocument_Strip(t *testing.T) {
	doc := NewMultimodalDocument()
	assert.Nil(t, doc.Strip())

	doc.AddField(ModalityText, "文本")
	assert.NotNil(t, doc.Strip())
}

func TestMultimodalDocument_AddField_从文件加载(t *testing.T) {
	// 创建临时文本文件
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(txtFile, []byte("文件内容"), 0644)
	assert.NoError(t, err)

	doc := NewMultimodalDocument().AddField(ModalityText, "", txtFile)
	assert.Equal(t, "文件内容", doc.Text)
}

func TestMultimodalDocument_AddField_无效模态(t *testing.T) {
	assert.Panics(t, func() {
		NewMultimodalDocument().AddField(ModalityKind("invalid"), "data")
	})
}
