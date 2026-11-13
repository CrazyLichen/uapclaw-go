package intellirouter

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsDataURI 判断字符串是否为 data URI。← utils/media.py:10 is_data_uri
func IsDataURI(s string) bool {
	return strings.HasPrefix(s, "data:")
}

// ParseDataURI 解析 data URI 为 (mime_type, base64_data)。
// ← utils/media.py:15 parse_data_uri
//
// 例："data:image/png;base64,iVBOR..." → ("image/png", "iVBOR...")
// URI 格式不合法时返回错误（对齐 Python ValueError）。
func ParseDataURI(uri string) (string, string, error) {
	if !strings.HasPrefix(uri, "data:") {
		// ← py:21 raise ValueError(f"Not a data URI: {uri[:50]}")
		return "", "", fmt.Errorf("Not a data URI: %s", truncate(uri, 50))
	}

	// data:image/png;base64,iVBOR... ← py:26-30
	idx := strings.Index(uri, ",")
	if idx < 0 {
		return "", "", fmt.Errorf("Invalid data URI (no comma): %s", truncate(uri, 50))
	}
	header := uri[:idx]
	data := uri[idx+1:]

	// header = "data:image/png;base64" ← py:33-36
	meta := header[5:] // strip "data:"
	parts := strings.Split(meta, ";")
	mimeType := parts[0]
	if mimeType == "" {
		mimeType = "application/octet-stream" // ← py:35
	}
	return mimeType, data, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncate rune 截断（对齐 Python 切片 [:n] 的字符语义）。
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
