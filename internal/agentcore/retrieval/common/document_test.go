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
	doc, err := NewMultimodalDocument().AddField(ModalityText, "你好世界")
	assert.NoError(t, err)
	assert.Equal(t, "你好世界", doc.Text)
	assert.Len(t, doc.Fields(), 1)
	assert.Equal(t, ModalityText, doc.Fields()[0].Kind)
	assert.Equal(t, "你好世界", doc.Fields()[0].Data)
	assert.Equal(t, "", doc.Fields()[0].ID) // 文本模态无 ID
}

func TestMultimodalDocument_AddField_图片URL(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityText, "描述")
	assert.NoError(t, err)
	doc, err = doc.AddField(ModalityImage, "https://example.com/img.png")
	assert.NoError(t, err)
	assert.Len(t, doc.Fields(), 2)
	assert.Equal(t, ModalityImage, doc.Fields()[1].Kind)
	assert.NotEmpty(t, doc.Fields()[1].ID) // 非文本模态有 ID
}

func TestMultimodalDocument_AddField_链式调用(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityText, "文本")
	assert.NoError(t, err)
	doc, err = doc.AddField(ModalityImage, "https://example.com/img.png")
	assert.NoError(t, err)
	assert.Len(t, doc.Fields(), 2)
}

func TestMultimodalDocument_Content_文本(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityText, "描述文本")
	assert.NoError(t, err)
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "text", content[0]["type"])
	assert.Equal(t, "描述文本", content[0]["text"])
}

func TestMultimodalDocument_Content_图片URL(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityImage, "https://example.com/img.png")
	assert.NoError(t, err)
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "image_url", content[0]["type"])
	urlMap := content[0]["image_url"].(map[string]any)
	assert.Equal(t, "https://example.com/img.png", urlMap["url"])
}

func TestMultimodalDocument_Content_视频URL(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityVideo, "https://example.com/video.mp4")
	assert.NoError(t, err)
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "video_url", content[0]["type"])
	urlMap := content[0]["video_url"].(map[string]any)
	assert.Equal(t, "https://example.com/video.mp4", urlMap["url"])
}

func TestMultimodalDocument_Content_音频Base64(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityAudio, "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEARKwAAIhYAQACABAAZGF0YQAAAAA=")
	assert.NoError(t, err)
	content := doc.Content()
	assert.Len(t, content, 1)
	assert.Equal(t, "input_audio", content[0]["type"])
	audioMap := content[0]["input_audio"].(map[string]any)
	assert.Equal(t, "wav", audioMap["format"])
}

func TestMultimodalDocument_DashscopeInput_文本和图片(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityText, "描述")
	assert.NoError(t, err)
	doc, err = doc.AddField(ModalityImage, "https://example.com/img.png")
	assert.NoError(t, err)
	input, err := doc.DashscopeInput()
	assert.NoError(t, err)
	assert.Equal(t, "描述", input["text"])
	assert.Equal(t, "https://example.com/img.png", input["image"])
}

func TestMultimodalDocument_DashscopeInput_视频URL(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityText, "描述")
	assert.NoError(t, err)
	doc, err = doc.AddField(ModalityVideo, "https://example.com/video.mp4")
	assert.NoError(t, err)
	input, err := doc.DashscopeInput()
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/video.mp4", input["video"])
}

func TestMultimodalDocument_DashscopeInput_多图片(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityImage, "https://example.com/img1.png")
	assert.NoError(t, err)
	doc, err = doc.AddField(ModalityImage, "https://example.com/img2.png")
	assert.NoError(t, err)
	input, err := doc.DashscopeInput()
	assert.NoError(t, err)
	images, ok := input["multi_images"].([]string)
	assert.True(t, ok)
	assert.Len(t, images, 2)
}

func TestMultimodalDocument_Strip(t *testing.T) {
	doc := NewMultimodalDocument()
	assert.Nil(t, doc.Strip())

	doc, err := doc.AddField(ModalityText, "文本")
	assert.NoError(t, err)
	assert.NotNil(t, doc.Strip())
}

func TestMultimodalDocument_AddField_从文件加载(t *testing.T) {
	// 创建临时文本文件
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(txtFile, []byte("文件内容"), 0644)
	assert.NoError(t, err)

	doc, err := NewMultimodalDocument().AddField(ModalityText, "", txtFile)
	assert.NoError(t, err)
	assert.Equal(t, "文件内容", doc.Text)
}

func TestMultimodalDocument_AddField_无效模态(t *testing.T) {
	_, err := NewMultimodalDocument().AddField(ModalityKind("invalid"), "data")
	assert.Error(t, err)
}

func TestMultimodalDocument_AddField_从文件加载图片(t *testing.T) {
	// 测试视频 URL 模式（无需文件系统 MIME 支持）
	doc, err := NewMultimodalDocument().AddField(ModalityVideo, "https://example.com/video.mp4")
	assert.NoError(t, err)
	assert.Len(t, doc.Fields(), 1)
	assert.Equal(t, ModalityVideo, doc.Fields()[0].Kind)
	assert.Contains(t, doc.Fields()[0].Data, "https://example.com/video.mp4")
}

func TestMultimodalDocument_AddField_空Data和空路径(t *testing.T) {
	_, err := NewMultimodalDocument().AddField(ModalityImage, "")
	assert.Error(t, err)
}

func TestMultimodalDocument_AddField_同时提供Data和路径(t *testing.T) {
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(txtFile, []byte("内容"), 0644)

	_, err := NewMultimodalDocument().AddField(ModalityText, "data", txtFile)
	assert.Error(t, err)
}

func TestMultimodalDocument_AddField_图片Base64(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityImage, "data:image/png;base64,iVBORw0KGgo=")
	assert.NoError(t, err)
	assert.Len(t, doc.Fields(), 1)
	assert.Equal(t, ModalityImage, doc.Fields()[0].Kind)
}

func TestMultimodalDocument_DashscopeInput_重复模态字段(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityText, "文本1")
	assert.NoError(t, err)
	doc, err = doc.AddField(ModalityText, "文本2")
	assert.NoError(t, err)
	_, err = doc.DashscopeInput()
	assert.Error(t, err)
}

func TestMultimodalDocument_DashscopeInput_base64视频不支持(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityVideo, "data:video/mp4;base64,AAAA")
	assert.NoError(t, err)
	_, err = doc.DashscopeInput()
	assert.Error(t, err)
}

func TestMultimodalDocument_AddField_从文件加载不存在的文件(t *testing.T) {
	_, err := NewMultimodalDocument().AddField(ModalityImage, "", "/nonexistent/path/test.png")
	assert.Error(t, err)
}

func TestLoadFromFile_路径是目录(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := NewMultimodalDocument().AddField(ModalityImage, "", tmpDir)
	assert.Error(t, err)
}

func TestMultimodalDocument_AddField_视频URL(t *testing.T) {
	doc, err := NewMultimodalDocument().AddField(ModalityVideo, "https://example.com/video.mp4")
	assert.NoError(t, err)
	assert.Len(t, doc.Fields(), 1)
	assert.Equal(t, ModalityVideo, doc.Fields()[0].Kind)
}

func TestMultimodalDocument_AddField_无效图片Data(t *testing.T) {
	_, err := NewMultimodalDocument().AddField(ModalityImage, "not-a-url-or-base64")
	assert.Error(t, err)
}
