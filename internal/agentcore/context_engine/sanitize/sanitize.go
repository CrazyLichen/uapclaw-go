package sanitize

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ImageDataURLPlaceholder 图片 data URL 的占位文本
	ImageDataURLPlaceholder = "[image:data-url-omitted]"
	// ImageBase64Placeholder 图片 base64 载荷的占位文本
	ImageBase64Placeholder = "[image:base64-omitted]"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// dataURLPayloadPattern 匹配图片 data URL 的 base64 载荷。
//
// 字符类不含空白：data URL 不会带换行，允许空白会把后续散文一起吞掉。
var dataURLPayloadPattern = regexp.MustCompile(`(?i)data:image/[^;,\s]+;base64,[A-Za-z0-9+/=]+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// SanitizeValueForText 递归替换结构化值里的图片/base64 载荷。
//
// 只替换"整值是图片 data URL"或"图片块载荷字段"，不改变其它结构，
// 使父结构（消息块列表）可以直接序列化进提示词/日志而不携带几十万字符。
//
// Python: content_sanitize.sanitize_value_for_text()
func SanitizeValueForText(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return sanitizeString(typed)
	case map[string]any:
		if replaced, ok := sanitizeImageBlockMap(typed); ok {
			return replaced
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = SanitizeValueForText(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = SanitizeValueForText(item)
		}
		return out
	default:
		return value
	}
}

// SanitizeContentForText 把消息内容序列化为不含图片原始字节的文本。
//
// 文本内容原样返回；块列表先净化再以紧凑 JSON 序列化。
//
// Python: content_sanitize.sanitize_content_for_text()
func SanitizeContentForText(content any) string {
	if text, ok := content.(string); ok {
		return sanitizeString(text)
	}
	sanitized := SanitizeValueForText(content)
	data, err := json.Marshal(sanitized)
	if err != nil {
		return ImageDataURLPlaceholder
	}
	return string(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sanitizeString 把字符串里的图片 data URL 换成占位符。
func sanitizeString(text string) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:image/") && !strings.Contains(trimmed, " ") {
		return ImageDataURLPlaceholder
	}
	if !strings.Contains(strings.ToLower(text), "data:image/") {
		return text
	}
	return dataURLPayloadPattern.ReplaceAllString(text, ImageDataURLPlaceholder)
}

// sanitizeImageBlockMap 识别图片块形态并整体替换其载荷字段。
//
// 覆盖三种常见形态：{"image_url": {"url": "data:image/..."}}、
// {"data_url": "data:image/..."}、{"source": {"type": "base64", "data": "..."}}。
func sanitizeImageBlockMap(block map[string]any) (map[string]any, bool) {
	if imageURL, ok := block["image_url"].(map[string]any); ok {
		if url, ok := imageURL["url"].(string); ok && isImageDataURL(url) {
			replaced := cloneMap(block)
			imageCopy := cloneMap(imageURL)
			imageCopy["url"] = ImageDataURLPlaceholder
			replaced["image_url"] = imageCopy
			return replaced, true
		}
	}
	for _, key := range []string{"data", "data_url", "url"} {
		value, ok := block[key].(string)
		if ok && isImageDataURL(value) {
			replaced := cloneMap(block)
			replaced[key] = ImageDataURLPlaceholder
			return replaced, true
		}
	}
	if source, ok := block["source"].(map[string]any); ok {
		if data, ok := source["data"].(string); ok && data != "" {
			replaced := cloneMap(block)
			sourceCopy := cloneMap(source)
			sourceCopy["data"] = ImageBase64Placeholder
			replaced["source"] = sourceCopy
			return replaced, true
		}
	}
	return nil, false
}

// isImageDataURL 判断是否为图片 data URL（application/pdf 等不算图片）。
func isImageDataURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:image/")
}

// cloneMap 浅拷贝 map，避免改写调用方数据。
func cloneMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src)+1)
	for key, value := range src {
		out[key] = value
	}
	return out
}
