package attachmentsafety

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// unsafeFilenameChars 无主流文件系统接受的名字字符：ASCII 控制字符、
	// POSIX/Windows 路径分隔符与 Windows 保留标点；其余（含中日韩、重音拉丁等
	// 非 ASCII 字母）原样保留。
	// Python: attachment_safety.py:24 _UNSAFE_FILENAME_CHARS。
	unsafeFilenameChars = `[\x00-\x1f\x7f<>:"/\\|?*]+`

	// bidiControlChars 双向文本控制字符（BIDI），从名字中静默剔除。
	// Python: attachment_safety.py:27 _BIDI_CONTROL_CHARS。
	bidiControlChars = "[‎‏‪-‮⁦-⁩]"

	// unsafeSessionChars 会话目录名只允许字母数字点下划线连字符。
	// Python: attachment_safety.py:30 _UNSAFE_SESSION_CHARS。
	unsafeSessionChars = `[^A-Za-z0-9._-]+`

	// maxFilenameBytes 文件名 UTF-8 字节上限。
	// Python: attachment_safety.py:36 _MAX_FILENAME_BYTES。
	maxFilenameBytes = 180

	// maxSessionIDChars 会话 id 字符上限。
	// Python: attachment_safety.py:37 _MAX_SESSION_ID_CHARS。
	maxSessionIDChars = 120
)

// forbiddenDocumentExtensions 文档上传扩展名黑名单。
// Python: attachment_safety.py:110-143 FORBIDDEN_DOCUMENT_EXTENSIONS（集合逐字）。
var forbiddenDocumentExtensions = map[string]struct{}{
	".exe": {}, ".dll": {}, ".msi": {}, ".scr": {}, ".bat": {}, ".cmd": {},
	".ps1": {}, ".vbs": {}, ".wsf": {}, ".hta": {}, ".jar": {}, ".lnk": {},
	".bin": {}, ".so": {}, ".dylib": {}, ".app": {}, ".dmg": {}, ".pkg": {},
	".command": {}, ".scpt": {}, ".scptd": {}, ".workflow": {}, ".xpc": {},
	".bundle": {}, ".framework": {}, ".kext": {}, ".prefpane": {}, ".saver": {},
	".component": {},
}

// windowsReservedNames Windows 静默保留名。
// Python: attachment_safety.py:32-35 _WINDOWS_RESERVED_NAMES。
var windowsReservedNames = func() map[string]struct{} {
	names := map[string]struct{}{"CON": {}, "PRN": {}, "AUX": {}, "NUL": {}}
	for i := 1; i <= 9; i++ {
		names["COM"+string(rune('0'+i))] = struct{}{}
		names["LPT"+string(rune('0'+i))] = struct{}{}
	}
	return names
}()

// supportedImageMimeTypes 受支持图片 MIME→规范后缀。
// Python: attachment_safety.py:156-161 _SUPPORTED_IMAGE_MIME_TYPES。
var supportedImageMimeTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// imageFilenameSuffixAliases 图片后缀等价别名。
// Python: attachment_safety.py:162 _IMAGE_FILENAME_SUFFIX_ALIASES。
var imageFilenameSuffixAliases = map[string]struct{}{".jpeg": {}, ".jfif": {}}

var (
	unsafeFilenameRe = regexp.MustCompile(unsafeFilenameChars)
	bidiControlRe    = regexp.MustCompile(bidiControlChars)
	unsafeSessionRe  = regexp.MustCompile(unsafeSessionChars)
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SanitizeUploadFilename 把浏览器上传名变成 uploads 目录下的单个安全路径分量。
// 目录部分丢弃；消毒后为空时用 fallback 再消毒，仍空则 "upload"。
// 非 ASCII（中日韩、重音拉丁等）原样保留——存储路径要让用户与 agent 都认得出文件。
// Python: attachment_safety.py:67-94 safe_upload_filename。
func SanitizeUploadFilename(filename, fallback string) string {
	name := sanitizeName(filename)
	if name == "" {
		name = sanitizeName(fallback)
		if name == "" {
			name = "upload"
		}
	}
	return truncateFilenameBytes(name, maxFilenameBytes)
}

// ForbiddenFormats 返回排序后的禁传扩展名列表。
// Python: attachment_safety.py:146-148 forbidden_formats。
func ForbiddenFormats() []string {
	out := make([]string, 0, len(forbiddenDocumentExtensions))
	for ext := range forbiddenDocumentExtensions {
		out = append(out, ext)
	}
	sortStrings(out)
	return out
}

// IsForbiddenDocument 判断文件名/后缀是否命中上传黑名单。
// suffix 优先；无点前缀时补点；大小写不敏感。
// Python: attachment_safety.py:155-161 is_forbidden_document。
func IsForbiddenDocument(filename, suffix string) bool {
	ext := strings.ToLower(suffix)
	if ext == "" {
		ext = strings.ToLower(path.Ext(path.Base(filename)))
	}
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	_, ok := forbiddenDocumentExtensions[ext]
	return ok
}

// IsSupportedDocument 判断文件名是否允许上传（未命中黑名单）。
// Python: attachment_safety.py:150-152 is_supported_document。
func IsSupportedDocument(filename string) bool {
	name := strings.TrimSpace(path.Base(filename))
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !IsForbiddenDocument(filename, "")
}

// SafeSessionDirname 返回会话上传目录名。
// 与文件名同一套点目录口径：“.“/“..“/Windows 尾点剥离后等价跳出 sessions 树
// 的名字一律回落 "default"。
// Python: attachment_safety.py:164-183 safe_session_dirname。
func SafeSessionDirname(sessionID string) string {
	text := strings.TrimSpace(sessionID)
	if text == "" {
		text = "default"
	}
	name := unsafeSessionRe.ReplaceAllString(text, "_")
	name = truncateRunes(name, maxSessionIDChars)
	name = strings.TrimSpace(strings.TrimRight(name, ". "))
	if name == "" || name == "." || name == ".." {
		return "default"
	}
	return name
}

// StripDataURIPrefix 剥离 “data:...;base64,“ 前缀。
// 前端上传库（react-dropzone + FileReader.readAsDataURL）会产出该形式，
// 不剥离会使 base64 校验因前缀中的 “: / ; ,“ 抛错导致合法文件被丢弃。
// Python: attachment_safety.py:186-195 strip_data_uri_prefix。
func StripDataURIPrefix(rawBase64 string) string {
	if strings.HasPrefix(rawBase64, "data:") {
		if _, rest, found := strings.Cut(rawBase64, ","); found {
			return rest
		}
		return ""
	}
	return rawBase64
}

// ImageSuffixForMime 返回受支持图片 MIME 对应的规范扩展名（含点），不支持返回空串。
// Python: attachment_safety.py:165-167 image_suffix_for_mime。
func ImageSuffixForMime(mimeType string) string {
	return supportedImageMimeTypes[mimeType]
}

// SupportedImageSuffixes 返回已有合法图片扩展名集合（含点）。
// 含规范后缀与 .jpeg/.jfif 等价别名。
// Python: attachment_safety.py:169-171 supported_image_suffixes。
func SupportedImageSuffixes() map[string]struct{} {
	out := make(map[string]struct{}, len(supportedImageMimeTypes)+len(imageFilenameSuffixAliases))
	for _, suffix := range supportedImageMimeTypes {
		out[suffix] = struct{}{}
	}
	for alias := range imageFilenameSuffixAliases {
		out[alias] = struct{}{}
	}
	return out
}

// EnsureImageUploadFilename 已有合法图片后缀时保留原名，否则末尾补规范后缀。
// Python: attachment_safety.py:173-178 ensure_image_upload_filename。
func EnsureImageUploadFilename(filename, canonicalSuffix string) string {
	if _, ok := SupportedImageSuffixes()[strings.ToLower(path.Ext(filename))]; ok {
		return filename
	}
	return filename + canonicalSuffix
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sanitizeName 剥目录、剔 BIDI 控制符、替换非法字符、剥尾点空格并拒 Windows 保留名。
// 什么都不剩时返回 ""。
// Python: attachment_safety.py:34-48 _sanitize_name。
func sanitizeName(filename string) string {
	name := path.Base(strings.TrimSpace(filename))
	name = bidiControlRe.ReplaceAllString(name, "")
	name = unsafeFilenameRe.ReplaceAllString(name, "_")
	// Windows 静默丢弃名字末尾的点与空格；同步剥离使存储名与各平台回读一致。
	name = strings.TrimSpace(strings.TrimRight(name, ". "))
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if _, reserved := windowsReservedNames[strings.ToUpper(splitExt(name))]; reserved {
		return ""
	}
	return name
}

// truncateFilenameBytes 按 UTF-8 字节截断名字，保扩展名、不劈 UTF-8 序列。
// Python: attachment_safety.py:60-73 _truncate_filename_bytes。
func truncateFilenameBytes(name string, maxBytes int) string {
	if len(name) <= maxBytes {
		return name
	}
	suffix := path.Ext(name)
	if len(suffix) >= maxBytes {
		return truncateTextBytes(name, maxBytes)
	}
	stem := truncateTextBytes(name[:len(name)-len(suffix)], maxBytes-len(suffix))
	if stem == "" {
		return truncateTextBytes(name, maxBytes)
	}
	return stem + suffix
}

// truncateTextBytes 按 UTF-8 字节截断文本，永不劈开 UTF-8 序列。
// Python: attachment_safety.py:52-57 _truncate_text_bytes。
func truncateTextBytes(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	cut := text[:maxBytes]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// truncateRunes 按字符数截断。
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// splitExt 取不含扩展名的 stem（用于 Windows 保留名判定）。
func splitExt(name string) string {
	ext := path.Ext(name)
	return name[:len(name)-len(ext)]
}

// sortStrings 升序排序（Go 1.22 无 slices 泛型依赖负担，保持标准库 strings 风格）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
