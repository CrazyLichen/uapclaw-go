package personal_context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ──────────────────────────── 常量 ────────────────────────────

// PortableForbidden 可移植段禁用字符集。
// Python: path_safety.py:18 PORTABLE_FORBIDDEN
const PortableForbidden = `<>:"/\|?*`

// MaxSegmentChars 单段最大字符数。
// Python: path_safety.py:24 MAX_SEGMENT_CHARS
const MaxSegmentChars = 80

// MaxSegmentUTF8Bytes 单段最大 UTF-8 字节数。
// Python: path_safety.py:25 MAX_SEGMENT_UTF8_BYTES
const MaxSegmentUTF8Bytes = 240

// SemanticNameMaxChars 语义名最大字符数。
// Python: path_safety.py:26 SEMANTIC_NAME_MAX_CHARS
const SemanticNameMaxChars = 20

// legacyServiceSegment 传统服务段正则。
// Python: path_safety.py:27 _LEGACY_SERVICE_SEGMENT
var legacyServiceSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// hashedServiceSegment 哈希服务段正则。
// Python: path_safety.py:28 _HASHED_SERVICE_SEGMENT
var hashedServiceSegment = regexp.MustCompile(`^\.service-[0-9a-f]{64}$`)

// windowsDeviceNames Windows 保留设备名集合。
// Python: path_safety.py:19-23 WINDOWS_DEVICE_NAMES
var windowsDeviceNames = func() map[string]struct{} {
	names := map[string]struct{}{"con": {}, "prn": {}, "aux": {}, "nul": {}}
	for index := 1; index <= 9; index++ {
		names[fmt.Sprintf("com%d", index)] = struct{}{}
		names[fmt.Sprintf("lpt%d", index)] = struct{}{}
	}
	return names
}()

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateServiceID 使用前端 UTF-16 长度上限校验展示名。
// Python: path_safety.py:31 validate_service_id
func ValidateServiceID(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("service_id must be a non-empty string")
	}
	text := strings.TrimSpace(value)
	encoded := utf16Length(text)
	if encoded > 1000 {
		return "", fmt.Errorf("service_id must be at most 500 characters")
	}
	return text, nil
}

// ServiceStorageSegment 传统路径保持可移植；新名字在命名空间外编码。
// Python: path_safety.py:42 service_storage_segment
func ServiceStorageSegment(serviceID string) string {
	text, err := ValidateServiceID(serviceID)
	if err != nil {
		panic(err) // Python 直接上抛 ValueError；调用点已在 Host 层校验过
	}
	firstDot := text
	if idx := strings.Index(text, "."); idx >= 0 {
		firstDot = text[:idx]
	}
	if legacyServiceSegment.MatchString(text) && !strings.HasSuffix(text, ".") &&
		!isWindowsDeviceName(strings.ToLower(firstDot)) {
		return text
	}
	sum := sha256.Sum256([]byte(text))
	return ".service-" + hex.EncodeToString(sum[:])
}

// ServiceStorageSegmentIsSafe 清理期识别磁盘组件而不重复编码。
// Python: path_safety.py:55 service_storage_segment_is_safe
func ServiceStorageSegmentIsSafe(segment string) bool {
	return legacyServiceSegment.MatchString(segment) || hashedServiceSegment.MatchString(segment)
}

// PortableContextSegmentIsSafe 单个 Context 路径段是否可移植且规范。
// Python: path_safety.py:85 portable_context_segment_is_safe
func PortableContextSegmentIsSafe(segment string) bool {
	if segment == "" || segment == "." || segment == ".." ||
		norm.NFC.String(segment) != segment {
		return false
	}
	encoded := len([]byte(segment))
	if len([]rune(segment)) > MaxSegmentChars || encoded > MaxSegmentUTF8Bytes ||
		strings.HasSuffix(segment, " ") || strings.HasSuffix(segment, ".") {
		return false
	}
	for _, character := range segment {
		if strings.ContainsRune(PortableForbidden, character) {
			return false
		}
		if isControlOrFormatCategory(character) {
			return false
		}
	}
	firstDot := segment
	if idx := strings.Index(segment, "."); idx >= 0 {
		firstDot = segment[:idx]
	}
	return !isWindowsDeviceName(strings.ToLower(firstDot))
}

// SemanticContextSegmentIsSafe 新的用户可见语义名是否满足产品上限。
// Python: path_safety.py:101 semantic_context_segment_is_safe
func SemanticContextSegmentIsSafe(segment string, markdownFile bool) bool {
	if !PortableContextSegmentIsSafe(segment) {
		return false
	}
	name := segment
	if markdownFile {
		lower := strings.ToLower(segment)
		if !strings.HasSuffix(lower, ".md") || lower == "description.md" {
			return false
		}
		name = segment[:len(segment)-3]
	}
	return name != "" && len([]rune(name)) <= SemanticNameMaxChars
}

// IsReparsePoint 既有 Windows 路径是否为文件系统重解析点。
// Python: path_safety.py:114 is_reparse_point
// 偏差 D-24.3a-7：Go 标准库不暴露 st_file_attributes；非 Windows 平台恒 false
// （symlink 检查由 _pathIsLinkOrReparse 的 Lstat 分支承担）。回收条件：24.3b
// browser_bookmarks LOCALAPPDATA 批次裁决 Windows reparse 支持方案。
func IsReparsePoint(_ string) bool {
	return false
}

// AssertExistingChainIsPlain 拒绝从 path 到受管根之间出现链接或重解析点。
// Python: path_safety.py:124 assert_existing_chain_is_plain
func AssertExistingChainIsPlain(pathStr string, stop string) error {
	if pathIsLinkOrReparse(stop) {
		return fmt.Errorf("managed Context path traverses a link or reparse point")
	}
	stopResolved, err := filepath.EvalSymlinks(stop)
	if err != nil {
		return fmt.Errorf("managed Context root is unavailable")
	}
	absolute := mustAbsolute(pathStr)
	stopAbs := mustAbsolute(stopResolved)
	relative, relErr := filepath.Rel(stopAbs, absolute)
	if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("managed Context path escaped its root")
	}
	current := stopResolved
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		if pathIsLinkOrReparse(current) {
			return fmt.Errorf("managed Context path traverses a link or reparse point")
		}
	}
	return nil
}

// ResolveContextRelativePath 在干净的受管 Context 根下解析规范 POSIX 路径。
// Python: path_safety.py:144 resolve_context_relative_path
func ResolveContextRelativePath(root string, value string, mustExist bool) (string, error) {
	if value == "" || strings.Contains(value, "\\") {
		return "", fmt.Errorf("Context path must be a POSIX relative path")
	}
	if value == "." || value == ".." || path.IsAbs(value) || path.Clean(value) != value {
		return "", fmt.Errorf("Context path is unsafe")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("Context path is unsafe")
		}
		if !PortableContextSegmentIsSafe(part) {
			return "", fmt.Errorf("Context path contains a non-portable segment")
		}
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("managed Context root is unavailable")
	}
	candidate := rootResolved
	for _, part := range parts {
		candidate = filepath.Join(candidate, part)
	}
	chainStart := candidate
	if !pathExists(candidate) && !pathIsLinkOrReparse(candidate) {
		chainStart = filepath.Dir(candidate)
	}
	if err := AssertExistingChainIsPlain(chainStart, root); err != nil {
		return "", err
	}
	if mustExist && !pathExists(candidate) {
		return "", fmt.Errorf("Context path is unavailable")
	}
	return candidate, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extendedPath 返回 Windows 扩展路径而不解析链接。
// Python: path_safety.py:61 _extended_path —— 非 Windows 原样返回。
// 偏差 D-24.3a-8：Go 侧按 runtime.GOOS 分支处理；Windows 盘符/UNC 语义随
// 24.3b browser_bookmarks LOCALAPPDATA 批次裁决后补齐。
func extendedPath(pathStr string) string {
	if runtime.GOOS != "windows" {
		return pathStr
	}
	absolute := mustAbsolute(pathStr)
	if strings.HasPrefix(absolute, `\\?\`) {
		return absolute
	}
	if strings.HasPrefix(absolute, `\\`) {
		return `\\?\UNC\` + absolute[2:]
	}
	return `\\?\` + absolute
}

// pathExists 探测路径存在性。Python: path_safety.py:76 _path_exists
func pathExists(pathStr string) bool {
	_, err := os.Lstat(extendedPath(pathStr))
	return err == nil
}

// pathIsLinkOrReparse 路径是否为链接或重解析点。
// Python: path_safety.py:80 _path_is_link_or_reparse
func pathIsLinkOrReparse(pathStr string) bool {
	if IsReparsePoint(pathStr) {
		return true
	}
	info, err := os.Lstat(pathStr)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// utf16Length 计算 UTF-16 LE 编码字节数（每码元 2 字节，代理对 2 码元）。
func utf16Length(text string) int {
	total := 0
	for _, character := range text {
		if character > 0xFFFF {
			total += 2
			continue
		}
		total++
	}
	return total * 2
}

// isWindowsDeviceName 首段点前缀是否为 Windows 保留设备名。
func isWindowsDeviceName(firstSegment string) bool {
	_, reserved := windowsDeviceNames[firstSegment]
	return reserved
}

// isControlOrFormatCategory 字符是否属 Unicode Cc/Cf 类别。
// Python: unicodedata.category(character) in {"Cc", "Cf"}
func isControlOrFormatCategory(character rune) bool {
	return unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
}

// mustAbsolute 返回绝对路径（Python Path.absolute 等价；失败时返回原始值）。
func mustAbsolute(pathStr string) string {
	absolute, err := filepath.Abs(pathStr)
	if err != nil {
		return pathStr
	}
	return absolute
}
