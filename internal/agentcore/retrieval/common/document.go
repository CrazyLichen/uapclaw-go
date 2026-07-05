package common

import (
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

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
	// contentCache Content() 方法的缓存结果，对齐 Python @cached_property
	contentCache []map[string]any
	// contentOnce 保证 Content() 只计算一次
	contentOnce sync.Once
	// dashscopeCache DashscopeInput() 方法的缓存结果，对齐 Python @cached_property
	dashscopeCache map[string]any
	// dashscopeOnce 保证 DashscopeInput() 只计算一次
	dashscopeOnce sync.Once
}

// addFieldOptions AddField 的可选参数
type addFieldOptions struct {
	// filePath 文件路径（与 data 二选一）
	filePath string
	// dataID 自定义数据 ID（仅非文本模态有效，长度 ≤ 32）
	dataID string
}

// ModalityKind 内容模态类型。
type ModalityKind string

// AddFieldOption AddField 可选参数函数
type AddFieldOption func(*addFieldOptions)

// ──────────────────────────── 常量 ────────────────────────────
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

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// audioBase64Pattern 音频 base64 正则，提取格式类型。
	// 提取为包级变量避免每次调用 Content() 时重复编译，对齐 T-28 修复。
	audioBase64Pattern = regexp.MustCompile(`data:audio/(.+?);base64,`)

	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMultimodalDocument 创建空的多模态文档。
func NewMultimodalDocument() *MultimodalDocument {
	return &MultimodalDocument{}
}

// FieldFilePath 设置文件路径选项
func FieldFilePath(path string) AddFieldOption {
	return func(o *addFieldOptions) {
		o.filePath = path
	}
}

// FieldDataID 设置自定义数据 ID 选项
func FieldDataID(id string) AddFieldOption {
	return func(o *addFieldOptions) {
		o.dataID = id
	}
}

// AddField 添加模态字段，支持链式调用。
//
// kind: 模态类型；data: 文本内容/URL/base64（kind 为 text 时必传）；
// opts: 可选参数，支持 FieldFilePath(string) 和 FieldDataID(string)。
//
// 对应 Python: MultimodalDocument.add_field()
// 注意：验证错误返回 error 而非 panic，对齐 Python 的 ValidationError 行为。
// 注意：与 Python 对齐，add_field 不会更新 text 字段，text 始终保持默认空字符串。
func (d *MultimodalDocument) AddField(kind ModalityKind, data string, opts ...AddFieldOption) (*MultimodalDocument, error) {
	o := defaultAddFieldOptions()
	for _, opt := range opts {
		opt(&o)
	}

	var fp string
	if o.filePath != "" {
		fp = o.filePath
	}

	k, resolvedData, err := loadMultimodalData(kind, data, fp)
	if err != nil {
		return nil, err
	}
	dataID := ""
	if kind != ModalityText {
		if o.dataID != "" {
			if len(o.dataID) > 32 {
				return nil, exception.BuildError(
					exception.StatusRetrievalEmbeddingInputInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("data_id 长度不能超过 32，当前长度: %d", len(o.dataID))),
				)
			}
			dataID = o.dataID
		} else {
			dataID = strings.ReplaceAll(uuid.NewString(), "-", "")
		}
	}
	d.fields = append(d.fields, ModalityField{Kind: k, Data: resolvedData, ID: dataID})
	return d, nil
}

// Content 返回 OpenAI/vLLM 格式的结构化内容列表。
// 使用 sync.Once 缓存结果，避免重复计算，对齐 Python @cached_property。
//
// 对应 Python: MultimodalDocument.content
func (d *MultimodalDocument) Content() []map[string]any {
	d.contentOnce.Do(func() {
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
					"type":                        fmt.Sprintf("%s_url", f.Kind),
					fmt.Sprintf("%s_url", f.Kind): map[string]any{"url": f.Data},
				})
			case ModalityAudio:
				matches := audioBase64Pattern.FindStringSubmatch(f.Data)
				if len(matches) < 2 {
					// 音频字段格式不匹配时记录警告，对齐 T-27 修复
					logger.Warn(logComponent).
						Str("modality", "audio").
						Str("data_prefix", truncatePrefix(f.Data, 50)).
						Msg("音频字段不匹配 base64 格式，已跳过")
					continue
				}
				content = append(content, map[string]any{
					"type":        "input_audio",
					"input_audio": map[string]any{"data": f.Data, "format": matches[1]},
				})
			}
			if f.ID != "" && len(content) > 0 {
				content[len(content)-1]["uuid"] = f.ID
			}
		}
		d.contentCache = content
	})
	return d.contentCache
}

// DashscopeInput 返回 DashScope 格式的输入字典。
// 使用 sync.Once 缓存结果，避免重复计算，对齐 Python @cached_property。
//
// 对应 Python: MultimodalDocument.dashscope_input
// 注意：验证错误返回 error 而非 panic，对齐 Python 的 ValidationError 行为。
func (d *MultimodalDocument) DashscopeInput() (map[string]any, error) {
	var cacheErr error
	d.dashscopeOnce.Do(func() {
		content := make(map[string]any)
		var images []string
		hasField := make(map[ModalityKind]bool)

		for _, f := range d.fields {
			if hasField[f.Kind] {
				cacheErr = exception.BuildError(
					exception.StatusRetrievalEmbeddingInputInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("Dashscope 格式不支持多个相同模态字段: %s", f.Kind)),
				)
				return
			}

			switch f.Kind {
			case ModalityText:
				hasField[f.Kind] = true
				content["text"] = f.Data
			case ModalityImage:
				images = append(images, f.Data)
			case ModalityVideo:
				if strings.HasPrefix(f.Data, "data:video/") {
					cacheErr = exception.BuildError(
						exception.StatusRetrievalEmbeddingInputInvalid,
						exception.WithParam("error_msg", "Dashscope 格式不支持 base64 视频输入，仅支持 URL"),
					)
					return
				}
				hasField[f.Kind] = true
				content["video"] = f.Data
			default:
				cacheErr = exception.BuildError(
					exception.StatusRetrievalEmbeddingInputInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("Dashscope 格式不支持模态类型: %s", f.Kind)),
				)
				return
			}
		}

		if len(images) == 1 {
			content["image"] = images[0]
		} else if len(images) > 1 {
			content["multi_images"] = images
		}

		d.dashscopeCache = content
	})
	return d.dashscopeCache, cacheErr
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultAddFieldOptions 返回默认的 AddField 选项
func defaultAddFieldOptions() addFieldOptions {
	return addFieldOptions{}
}

// loadMultimodalData 加载多模态数据。
//
// 对应 Python: _load_multimodal_data()
// 注意：验证错误返回 error 而非 panic，对齐 Python 的 ValidationError 行为。
func loadMultimodalData(kind ModalityKind, data string, filePath string) (ModalityKind, string, error) {
	validKinds := map[ModalityKind]bool{
		ModalityText:  true,
		ModalityImage: true,
		ModalityAudio: true,
		ModalityVideo: true,
	}
	if !validKinds[kind] {
		return "", "", exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("未知的模态类型: %s", kind)),
		)
	}

	// 文件路径模式
	if filePath != "" {
		if data != "" {
			return "", "", exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", "不能同时提供 data 和 filePath"),
			)
		}
		return loadFromFile(kind, filePath)
	}

	// data 模式
	if data == "" {
		return "", "", exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("必须提供 %s 类型的数据或文件路径", kind)),
		)
	}

	if kind == ModalityText {
		return kind, data, nil
	}

	// 检查 base64 前缀
	kindPrefix := fmt.Sprintf("data:%s/", kind)
	if strings.HasPrefix(data, kindPrefix) {
		return kind, data, nil
	}

	// URL 模式（音频只接受 base64）
	if kind != ModalityAudio && strings.HasPrefix(data, "http") && strings.Contains(data, "://") {
		return kind, data, nil
	}

	return "", "", exception.BuildError(
		exception.StatusRetrievalEmbeddingInputInvalid,
		exception.WithParam("error_msg", fmt.Sprintf("无效的 %s 数据，应为 URL 或以 'data:%s/' 开头的 base64", kind, kind)),
	)
}

// loadFromFile 从文件路径加载数据为 base64。
func loadFromFile(kind ModalityKind, path string) (ModalityKind, string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", "", exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法打开 %s 文件: %s", kind, path)),
		)
	}

	// 文本模态直接读取文件内容
	if kind == ModalityText {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", "", exception.BuildError(
				exception.StatusRetrievalEmbeddingInputInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("读取文本文件失败: %s", err)),
			)
		}
		return kind, string(content), nil
	}

	mimeType := mime.TypeByExtension(path)
	if mimeType == "" || !strings.Contains(mimeType, "/") {
		return "", "", exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("无法确定 %s 文件的 MIME 类型: %s", kind, path)),
		)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("读取 %s 文件失败: %s", kind, err)),
		)
	}

	b64Str := base64.StdEncoding.EncodeToString(raw)
	return kind, fmt.Sprintf("data:%s;base64,%s", mimeType, b64Str), nil
}

// truncatePrefix 截取字符串前 n 个字符，用于日志输出避免过长。
func truncatePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
