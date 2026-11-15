package prompt

// 多模态图片输入适配：附件提取、归一化、data URI 读取与消息定位。
//
// Python: jiuwenswarm/agents/harness/common/prompt/user_prompt_builder.py 的图片函数段
// （23.8 批2；Python 单文件中的图片函数聚为一个 Go 文件，结构映射见 doc.go）。
// 语言差异：Python duck-typed dict 块 → Go 强类型 schema.ContentPart /
// schema.MessageContent（字段一一对应：{"type":"image_url","image_url":{"url":…}}）。

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ImageContentOmitted 图片内容全量剥离后留下的占位文本。
// Python: IMAGE_CONTENT_OMITTED:15-18（协议字符串，不本地化）
const ImageContentOmitted = "[Image content omitted from chat-model context. Use the original image " +
	"path or a vision tool when image analysis is required.]"

// imageContentTypes 视为图片块的 content part 类型集合。
// Python: _IMAGE_CONTENT_TYPES:20
var imageContentTypes = map[string]struct{}{
	"image":       {},
	"image_url":   {},
	"input_image": {},
}

// visionInlineImageMimeTypes 允许内联进模型输入的图片 mime 白名单。
// Python: _VISION_INLINE_IMAGE_MIME_TYPES:21-26
var visionInlineImageMimeTypes = map[string]struct{}{
	"image/png":  {},
	"image/jpeg": {},
	"image/webp": {},
	"image/gif":  {},
}

// maxVisionInlineImageBytes 单张内联图片字节上限。
// Python: _MAX_VISION_INLINE_IMAGE_BYTES:27
const maxVisionInlineImageBytes = 10 * 1024 * 1024

// ──────────────────────────── 结构体 ────────────────────────────

// MultimodalImageFile 归一化后的图片附件记录。
// Python: _normalize_image_file 输出 dict（type/filename/path/mime_type/size_bytes）。
type MultimodalImageFile struct {
	// Type 固定为 "image"
	Type string `json:"type"`
	// Filename 文件名（展示用）
	Filename string `json:"filename"`
	// Path 服务器磁盘上的绝对路径
	Path string `json:"path"`
	// MimeType 归一化后的小写 mime
	MimeType string `json:"mime_type"`
	// SizeBytes 字节数（可选，Python 仅在原始条目携带 int 时透传）
	SizeBytes int64 `json:"size_bytes,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsImageContentBlock 判断 content part 是否为图片块。
// Python: is_image_content_block:35-42
// Python 的 `“image_url” in part or “image” in part` 对应 Go 的
// ImageURL / InputAudio 等 image 形字段非 nil。
func IsImageContentBlock(part llmschema.ContentPart) bool {
	if _, ok := imageContentTypes[strings.ToLower(strings.TrimSpace(part.Type))]; ok {
		return true
	}
	return part.ImageURL != nil
}

// ExtractMultimodalImageFiles 从请求参数中提取归一化图片文件记录。
// Python: extract_multimodal_image_files:44-73
// 候选来源：params["media_items"] 列表 + params["files"]["uploaded_images"] 列表；
// 逐条归一化后按 path 去重。
func ExtractMultimodalImageFiles(params map[string]any) []MultimodalImageFile {
	if params == nil {
		return nil
	}

	var candidates []any
	// Python: raw_media_items = params.get("media_items")
	if rawMediaItems, ok := params["media_items"].([]any); ok {
		candidates = append(candidates, rawMediaItems...)
	}
	// Python: files = params.get("files")；files["uploaded_images"]
	if files, ok := params["files"].(map[string]any); ok {
		if uploadedImages, ok := files["uploaded_images"].([]any); ok {
			candidates = append(candidates, uploadedImages...)
		}
	}

	normalized := make([]MultimodalImageFile, 0, len(candidates))
	seenPaths := make(map[string]struct{})
	for _, item := range candidates {
		image, ok := item.(map[string]any)
		if !ok {
			continue
		}
		norm := normalizeImageFile(image)
		if norm == nil {
			continue
		}
		if _, dup := seenPaths[norm.Path]; dup {
			continue
		}
		seenPaths[norm.Path] = struct{}{}
		normalized = append(normalized, *norm)
	}
	return normalized
}

// LatestUserMessageIndex 倒序定位最后一条 user 消息的下标；找不到返回 -1。
// Python: _latest_user_message_index:353-358 + _is_user_message:360-368
// Python 按 role 字符串或类名 UserMessage 判定，Go 统一按 RoleType == RoleTypeUser。
func LatestUserMessageIndex(messages []llmschema.BaseMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].GetRole() == llmschema.RoleTypeUser {
			return i
		}
	}
	return -1
}

// ImageDataURIFromPath 读取图片文件并编码为 data URI；失败/超限/超白名单返回空串。
// Python: _image_data_uri_from_path:433-447
func ImageDataURIFromPath(pathText string, mimeType string) string {
	path := strings.TrimSpace(pathText)
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	if _, ok := visionInlineImageMimeTypes[mimeType]; !ok {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(data) == 0 || len(data) > maxVisionInlineImageBytes {
		return ""
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeImageFile 归一化单个图片附件条目；非法条目返回 nil。
// Python: _normalize_image_file:323-351
// 校验顺序对齐：type 过滤 → path 非空 → mime 补全与白名单 → filename 兜底 → size_bytes 透传。
func normalizeImageFile(item map[string]any) *MultimodalImageFile {
	// Python: item_type is not None and item_type != "image" → 拒绝
	if itemType, ok := item["type"]; ok && itemType != nil {
		if s := stripAnyGo(itemType); s != "image" {
			return nil
		}
	}

	path := stripAnyGo(item["path"])
	if path == "" {
		return nil
	}

	// Python: mime_type = item.get("mime_type") or item.get("mimeType")；缺失按扩展名推断
	mimeType := stripAnyGo(strOrAnyGo(item["mime_type"], item["mimeType"]))
	if mimeType == "" {
		mimeType = guessMimeTypeByExtension(path)
	}
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if _, ok := visionInlineImageMimeTypes[mimeType]; !ok {
		return nil
	}

	filename := stripAnyGo(strOrAnyGo(item["filename"], filepath.Base(path)))
	if filename == "" {
		filename = filepath.Base(path)
	}

	image := &MultimodalImageFile{
		Type:     "image",
		Filename: filename,
		Path:     path,
		MimeType: mimeType,
	}
	// Python: size_bytes 仅在 isinstance(int) 时透传
	if sizeBytes, ok := item["size_bytes"].(int64); ok {
		image.SizeBytes = sizeBytes
	}
	return image
}

// guessMimeTypeByExtension 按扩展名推断 mime 类型。
// Python: mimetypes.guess_type(path)[0]
func guessMimeTypeByExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

// stripAnyGo 等价 Python str(x or "").strip()（None/空 → ""）。
func stripAnyGo(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(s)
	default:
		return strings.TrimSpace(stringifyAnyGo(v))
	}
}

// strOrAnyGo 第一个 strip 后非空的值。
func strOrAnyGo(v1, v2 any) any {
	if s := stripAnyGo(v1); s != "" {
		return v1
	}
	return v2
}

// stringifyAnyGo 非 string 的 any 转字符串（对齐 Python str(x)）。
func stringifyAnyGo(v any) string {
	return fmt.Sprintf("%v", v)
}
