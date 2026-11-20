package skills

import (
	"path"
	"regexp"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mediaImageExtensions 图片扩展名集合
	mediaImageExtensions = map[string]struct{}{".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}}
	// mediaVideoExtensions 视频扩展名集合
	mediaVideoExtensions = map[string]struct{}{".mp4": {}, ".mov": {}, ".webm": {}, ".mkv": {}}
	// markdownImagePattern markdown 图片语法
	markdownImagePattern = regexp.MustCompile(`!\[([^\]]*)\]\((.*?)\)`)
	// markdownLinkPattern markdown 链接语法（第一个捕获组为 "!" 时表示图片，需跳过）
	markdownLinkPattern = regexp.MustCompile(`(!?)\[([^\]]*)\]\((.*?)\)`)
	// htmlMediaSrcPattern HTML img/video 的 src 属性
	htmlMediaSrcPattern = regexp.MustCompile(`(?i)<(?:video|img)\b[^>]*?\bsrc=["']([^"']*)["']`)
	// bareMediaPathPattern 裸媒体路径（RE2 无 lookbehind，用前后边界字符近似）
	bareMediaPathPattern = regexp.MustCompile(`(?i)(?:^|[^\w./])([\w./-]+\.(?:png|jpe?g|gif|webp|mp4|mov|webm|mkv))(?:[^\w.]|$)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// markdownHasImageReference 判断文本是否含图片引用。
// Python: markdown_has_image_reference (markdown_media.py:70-73)
func markdownHasImageReference(text string) bool {
	return hasMediaReference(text, mediaImageExtensions)
}

// markdownHasVideoReference 判断文本是否含视频引用。
// Python: markdown_has_video_reference (markdown_media.py:75-77)
func markdownHasVideoReference(text string) bool {
	return hasMediaReference(text, mediaVideoExtensions)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasMediaReference 按扩展名集合判定是否存在媒体引用。
// Python: _has_media_reference (markdown_media.py:68-77)
func hasMediaReference(text string, extensions map[string]struct{}) bool {
	for _, candidate := range iterMediaReferenceURLs(text) {
		if _, ok := extensions[mediaURLSuffix(candidate)]; ok {
			return true
		}
	}
	return false
}

// iterMediaReferenceURLs 依次产出 markdown 图片/链接、HTML src、裸路径候选。
// Python: _iter_media_reference_urls (markdown_media.py:59-66)
func iterMediaReferenceURLs(text string) []string {
	var urls []string
	for _, m := range markdownImagePattern.FindAllStringSubmatch(text, -1) {
		urls = append(urls, strings.TrimSpace(m[2]))
	}
	for _, m := range markdownLinkPattern.FindAllStringSubmatch(text, -1) {
		if m[1] == "!" {
			continue
		}
		urls = append(urls, strings.TrimSpace(m[3]))
	}
	for _, m := range htmlMediaSrcPattern.FindAllStringSubmatch(text, -1) {
		urls = append(urls, strings.TrimSpace(m[1]))
	}
	for _, m := range bareMediaPathPattern.FindAllStringSubmatch(text, -1) {
		urls = append(urls, strings.TrimSpace(m[1]))
	}
	return urls
}

// mediaURLSuffix 取 URL 路径部分的扩展名（忽略 query/fragment，解码 %20）。
// Python: _extension_for_markdown_url (markdown_media.py:31-36)
func mediaURLSuffix(rawURL string) string {
	decoded := strings.TrimSpace(rawURL)
	decoded = strings.ReplaceAll(decoded, "%20", " ")
	for _, sep := range []string{"?", "#"} {
		if idx := strings.Index(decoded, sep); idx >= 0 {
			decoded = decoded[:idx]
		}
	}
	decoded = strings.ReplaceAll(decoded, "\\", "/")
	return strings.ToLower(path.Ext(decoded))
}
