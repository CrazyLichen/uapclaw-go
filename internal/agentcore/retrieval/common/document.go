package common

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModalityKind 内容模态类型。
type ModalityKind string

const (
	// ModalityText 文本模态
	ModalityText ModalityKind = "text"
	// ModalityImage 图片模态
	ModalityImage ModalityKind = "image"
	// ModalityAudio 音频模态
	ModalityAudio ModalityKind = "audio"
	// ModalityVideo 视频模态
	ModalityVideo ModalityKind = "video"
)

// ModalityField 单个模态字段。
type ModalityField struct {
	// Kind 模态类型
	Kind ModalityKind
	// Data 文本内容、URL 或 base64 编码数据
	Data string
	// ID 多模态缓存用的 UUID（文本模态为空）
	ID string
}

// MultimodalDocument 多模态文档，支持文本+图片+音频+视频混合输入。
//
// 对应 Python: openjiuwen/core/retrieval/common/document.py (MultimodalDocument)
type MultimodalDocument struct {
	// Text 文本回退字段，供不支持多模态的服务使用
	Text string
	// fields 有序模态字段列表
	fields []ModalityField
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMultimodalDocument 创建空的多模态文档。
func NewMultimodalDocument() *MultimodalDocument {
	return &MultimodalDocument{}
}

// AddField 添加模态字段，支持链式调用。
//
// kind: 模态类型；data: 文本内容/URL/base64（kind 为 text 时必传）；
// filePath: 文件路径（与 data 二选一，最多传一个）。
//
// 对应 Python: MultimodalDocument.add_field()
func (d *MultimodalDocument) AddField(kind ModalityKind, data string, filePath ...string) *MultimodalDocument {
	k, resolvedData := loadMultimodalData(kind, data, filePath...)
	dataID := ""
	if kind != ModalityText {
		dataID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	d.fields = append(d.fields, ModalityField{Kind: k, Data: resolvedData, ID: dataID})
	if kind == ModalityText {
		d.Text = resolvedData
	}
	return d
}

// Content 返回 OpenAI/vLLM 格式的结构化内容列表。
//
// 对应 Python: MultimodalDocument.content
func (d *MultimodalDocument) Content() []map[string]any {
	var content []map[string]any
	for _, f := range d.fields {
		switch f.Kind {
		case ModalityText:
			content = append(content, map[string]any{
				"type": "text",
				"text": f.Data,
			})
		case ModalityImage, ModalityVideo:
			content = append(content, map[string]any{
				"type":                         fmt.Sprintf("%s_url", f.Kind),
				fmt.Sprintf("%s_url", f.Kind):  map[string]any{"url": f.Data},
			})
		case ModalityAudio:
			re := regexp.MustCompile(`data:audio/(.+?);base64,`)
			matches := re.FindStringSubmatch(f.Data)
			if len(matches) < 2 {
				continue
			}
			content = append(content, map[string]any{
				"type":        "input_audio",
				"input_audio": map[string]any{"data": f.Data, "format": matches[1]},
			})
		}
		if f.ID != "" {
			content[len(content)-1]["uuid"] = f.ID
		}
	}
	return content
}

// DashscopeInput 返回 DashScope 格式的输入字典。
//
// 对应 Python: MultimodalDocument.dashscope_input
func (d *MultimodalDocument) DashscopeInput() map[string]any {
	content := make(map[string]any)
	var images []string
	hasField := make(map[ModalityKind]bool)

	for _, f := range d.fields {
		if hasField[f.Kind] {
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("Dashscope 格式不支持多个相同模态字段: %s", f.Kind)),
			))
		}

		switch f.Kind {
		case ModalityText:
			hasField[f.Kind] = true
			content["text"] = f.Data
		case ModalityImage:
			images = append(images, f.Data)
		case ModalityVideo:
			if strings.HasPrefix(f.Data, "data:video/") {
				panic(exception.BuildError(
					exception.StatusRetrievalEmbeddingInputInvalid,
					exception.WithParam("error_msg", "Dashscope 格式不支持 base64 视频输入，仅支持 URL"),
				))
			}
			hasField[f.Kind] = true
			content["video"] = f.Data
		default:
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("Dashscope 格式不支持模态类型: %s", f.Kind)),
			))
		}
	}

	if len(images) == 1 {
		content["image"] = images[0]
	} else if len(images) > 1 {
		content["multi_images"] = images
	}

	return content
}

// Strip 兼容 Python 的 strip() 语义，无字段时返回 nil。
//
// 对应 Python: MultimodalDocument.strip()
func (d *MultimodalDocument) Strip() *MultimodalDocument {
	if len(d.fields) == 0 {
		return nil
	}
	return d
}

// Fields 返回模态字段列表的只读副本。
func (d *MultimodalDocument) Fields() []ModalityField {
	result := make([]ModalityField, len(d.fields))
	copy(result, d.fields)
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadMultimodalData 加载多模态数据。
//
// 对应 Python: _load_multimodal_data()
func loadMultimodalData(kind ModalityKind, data string, filePath ...string) (ModalityKind, string) {
	validKinds := map[ModalityKind]bool{
		ModalityText:  true,
		ModalityImage: true,
		ModalityAudio: true,
		ModalityVideo: true,
	}
	if !validKinds[kind] {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("未知的模态类型: %s", kind)),
		))
	}

	// 文件路径模式
	if len(filePath) > 0 && filePath[0] != "" {
		if data != "" {
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", "不能同时提供 data 和 filePath"),
			))
		}
		return loadFromFile(kind, filePath[0])
	}

	// data 模式
	if data == "" {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("必须提供 %s 类型的数据或文件路径", kind)),
		))
	}

	if kind == ModalityText {
		return kind, data
	}

	// 检查 base64 前缀
	kindPrefix := fmt.Sprintf("data:%s/", kind)
	if strings.HasPrefix(data, kindPrefix) {
		return kind, data
	}

	// URL 模式（音频只接受 base64）
	if kind != ModalityAudio && strings.HasPrefix(data, "http") && strings.Contains(data, "://") {
		return kind, data
	}

	panic(exception.BuildError(
		exception.StatusRetrievalEmbeddingInputInvalid,
		exception.WithParam("error_msg", fmt.Sprintf("无效的 %s 数据，应为 URL 或以 'data:%s/' 开头的 base64", kind, kind)),
	))
}

// loadFromFile 从文件路径加载数据为 base64。
func loadFromFile(kind ModalityKind, path string) (ModalityKind, string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法打开 %s 文件: %s", kind, path)),
		))
	}

	// 文本模态直接读取文件内容
	if kind == ModalityText {
		content, err := os.ReadFile(path)
		if err != nil {
			panic(exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("读取文本文件失败: %s", err)),
			))
		}
		return kind, string(content)
	}

	mimeType := mime.TypeByExtension(path)
	if mimeType == "" || !strings.Contains(mimeType, "/") {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法确定 %s 文件的 MIME 类型: %s", kind, path)),
		))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		panic(exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("读取 %s 文件失败: %s", kind, err)),
		))
	}

	b64Str := base64.StdEncoding.EncodeToString(raw)
	return kind, fmt.Sprintf("data:%s;base64,%s", mimeType, b64Str)
}
