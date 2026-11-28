package personal_context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 程序持有的原子源元数据 Markdown（source-meta/*.md）读写。
// Python: openjiuwen/harness/personal_context/source_metadata.py（一比一移植）。

// ──────────────────────────── 全局变量 ────────────────────────────

// sourceIDPattern src_<32位hex> 形态。
// Python: source_metadata.py:11 _SOURCE_ID
var sourceIDPattern = regexp.MustCompile(`^src_[0-9a-f]{32}$`)

// sha256Pattern 64 位 hex。
// Python: source_metadata.py:12 _SHA256
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// metadataFieldPattern "- name: value" 字段行。
// Python: source_metadata.py:13 _FIELD
var metadataFieldPattern = regexp.MustCompile(`^- ([a-z_]+): (.+)$`)

// metadataFields 固定字段顺序。
// Python: source_metadata.py:14 _FIELDS
var metadataFields = []string{
	"source_id", "source_type", "title", "locator", "provider", "service",
	"first_seen", "last_seen", "latest_revision", "latest_hash",
}

// maxMetadataBytes 单文件上限 64KiB。
// Python: source_metadata.py:24 _MAX_METADATA_BYTES
const maxMetadataBytes = 64 * 1024

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeSourceLocator 返回剥离 URL 凭据后的稳定定位符。
// Python: source_metadata.py:63 normalize_source_locator
func NormalizeSourceLocator(originalRef string) (string, error) {
	locator, err := requiredMetadataText(originalRef, "locator")
	if err != nil {
		return "", err
	}
	parsed, parseErr := url.Parse(locator)
	if parseErr != nil {
		return locator, nil
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return locator, nil
	}
	host := parsed.Hostname()
	port := parsed.Port()
	// Python urlsplit 对畸形端口抛 ValueError → 返回 [redacted]；
	// Go url.Parse 已在上方吞错，此处以 Port() 解析失败兜底。
	if parsed.Host != "" && port == "" && strings.Contains(parsed.Host, ":") &&
		!strings.HasSuffix(parsed.Host, "]") && !ipv6Host(parsed.Host) {
		return scheme + "://[redacted]", nil
	}
	if host == "" {
		return scheme + "://[redacted]", nil
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	safeNetloc := host
	if port != "" {
		safeNetloc = host + ":" + port
	}
	return rebuildURL(scheme, safeNetloc, parsed.Path), nil
}

// SourceIDForLocator 返回定位符的稳定 128 位摘要 ID。
// Python: source_metadata.py:83 source_id_for_locator
func SourceIDForLocator(locator string) (string, error) {
	normalized, err := NormalizeSourceLocator(locator)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(normalized))
	return "src_" + hex.EncodeToString(sum[:])[:32], nil
}

// SourceItemVersion 返回该条目持久化的精确 revision/hash 对。
// Python: source_metadata.py:104 source_item_version
func SourceItemVersion(item *RawChangeItem) (string, string, error) {
	revision, err := requiredMetadataText(item.RevisionID, "latest_revision")
	if err != nil {
		return "", "", err
	}
	return revision, latestHash(item), nil
}

// ReadSourceMetadata 读取并校验一份 PersonalContext 持有的元数据 Markdown。
// Python: source_metadata.py:141 read_source_metadata
func ReadSourceMetadata(path string) (map[string]string, error) {
	if err := assertNoSymlinkChain(path); err != nil {
		return nil, err
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	ext := strings.ToLower(filepath.Ext(path))
	if !sourceIDPattern.MatchString(stem) || ext != ".md" {
		return nil, sourceMetadataError("atomic source metadata filename is invalid", nil)
	}
	info, statErr := os.Lstat(path)
	if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxMetadataBytes {
		return nil, sourceMetadataError("atomic source metadata file is missing or too large", statErr)
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, sourceMetadataError("atomic source metadata could not be read", readErr)
	}
	text := string(raw)
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	// Python splitlines() 对 \r\n 同样成行；Go 先归一换行。
	if strings.Contains(text, "\r\n") {
		lines = strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}
	if len(lines) != len(metadataFields)+2 || !strings.HasPrefix(lines[0], "# ") || lines[1] != "" {
		return nil, sourceMetadataError("atomic source metadata Markdown is invalid", nil)
	}
	values := map[string]string{}
	var order []string
	for _, line := range lines[2:] {
		match := metadataFieldPattern.FindStringSubmatch(line)
		if match == nil {
			return nil, sourceMetadataError("atomic source metadata field is invalid", nil)
		}
		if _, exists := values[match[1]]; exists {
			return nil, sourceMetadataError("atomic source metadata field is invalid", nil)
		}
		var decoded string
		if err := json.Unmarshal([]byte(match[2]), &decoded); err != nil {
			return nil, sourceMetadataError("atomic source metadata field is invalid", err)
		}
		value, err := requiredMetadataText(decoded, match[1])
		if err != nil {
			return nil, err
		}
		values[match[1]] = value
		order = append(order, match[1])
	}
	if strings.Join(order, ",") != strings.Join(metadataFields, ",") {
		return nil, sourceMetadataError("atomic source metadata fields are invalid", nil)
	}
	sourceID := values["source_id"]
	locator := values["locator"]
	expectedID, err := SourceIDForLocator(locator)
	if err != nil {
		return nil, err
	}
	if sourceID != stem || sourceID != expectedID {
		return nil, sourceMetadataError("atomic source metadata identity does not match its filename", nil)
	}
	if lines[0][2:] != displayTitle(values["title"]) {
		return nil, sourceMetadataError("atomic source metadata title does not match its heading", nil)
	}
	if !sha256Pattern.MatchString(values["latest_hash"]) {
		return nil, sourceMetadataError("atomic source metadata latest_hash is invalid", nil)
	}
	return values, nil
}

// ReadSourceDetail 按 ID 读取并只暴露公开明细字段。
// Python: source_metadata.py:179 read_source_detail
func ReadSourceDetail(sourceRoot string, sourceID string) (map[string]string, error) {
	if !sourceIDPattern.MatchString(sourceID) {
		return nil, sourceMetadataError("atomic source ID is invalid", nil)
	}
	if err := assertNoSymlinkChain(sourceRoot); err != nil {
		return nil, err
	}
	metadata, err := ReadSourceMetadata(filepath.Join(sourceRoot, sourceID+".md"))
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"source_id":   metadata["source_id"],
		"title":       metadata["title"],
		"source_type": metadata["source_type"],
		"locator":     metadata["locator"],
		"provider":    metadata["provider"],
		"service_id":  metadata["service"],
		"first_seen":  metadata["first_seen"],
		"last_seen":   metadata["last_seen"],
	}, nil
}

// UpsertSourceMetadata 创建或更新一份程序持有的元数据 Markdown 并返回其 ID。
// Python: source_metadata.py:198 upsert_source_metadata
func UpsertSourceMetadata(sourceRoot string, item *RawChangeItem, provider string, serviceID string, observedAt string) (string, error) {
	locator, err := NormalizeSourceLocator(item.OriginalRef)
	if err != nil {
		return "", err
	}
	sourceID, err := SourceIDForLocator(locator)
	if err != nil {
		return "", err
	}
	providerValue, err := requiredMetadataText(provider, "provider")
	if err != nil {
		return "", err
	}
	serviceValue, err := requiredMetadataText(serviceID, "service")
	if err != nil {
		return "", err
	}
	observedValue, err := requiredMetadataText(observedAt, "observed_at")
	if err != nil {
		return "", err
	}
	if err := assertNoSymlinkChain(sourceRoot); err != nil {
		return "", err
	}
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		return "", sourceMetadataError("atomic source metadata directory could not be created", err)
	}
	if err := assertNoSymlinkChain(sourceRoot); err != nil {
		return "", err
	}
	info, statErr := os.Lstat(sourceRoot)
	if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", sourceMetadataError("atomic source metadata root is invalid", nil)
	}
	target := filepath.Join(sourceRoot, sourceID+".md")
	var existing map[string]string
	targetInfo, targetErr := os.Lstat(target)
	if targetErr == nil {
		existing, err = ReadSourceMetadata(target)
		if err != nil {
			return "", err
		}
	} else if targetInfo != nil && targetInfo.Mode()&os.ModeSymlink != 0 {
		existing, err = ReadSourceMetadata(target)
		if err != nil {
			return "", err
		}
	}
	var previousTitle string
	if existing != nil {
		previousTitle = existing["title"]
	}
	titleValue := locator
	if item.Title != nil && strings.TrimSpace(*item.Title) != "" {
		titleValue = *item.Title
	} else if previousTitle != "" {
		titleValue = previousTitle
	}
	revision, contentHash, err := SourceItemVersion(item)
	if err != nil {
		return "", err
	}
	firstSeen := observedValue
	if existing != nil {
		firstSeen = existing["first_seen"]
	}
	metadata := map[string]string{
		"source_id":       sourceID,
		"source_type":     sourceTypeOf(item, providerValue),
		"title":           displayTitle(titleValue),
		"locator":         locator,
		"provider":        providerValue,
		"service":         serviceValue,
		"first_seen":      firstSeen,
		"last_seen":       observedValue,
		"latest_revision": revision,
		"latest_hash":     contentHash,
	}
	if err := atomicMetadataWrite(target, renderSourceMetadata(metadata)); err != nil {
		return "", err
	}
	return sourceID, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sourceMetadataError 构造 CONTEXT_PROACTIVE_FILE_EXECUTION_ERROR。
// Python: source_metadata.py:37 _source_error
func sourceMetadataError(message string, cause error) error {
	opts := []exception.ErrorOption{exception.WithMsg(message)}
	if cause != nil {
		opts = append(opts, exception.WithCause(cause))
	}
	return BuildError(StatusCodeContextProactiveFileExecutionError, opts...)
}

// assertNoSymlinkChain 路径链上不得出现 symlink。
// Python: source_metadata.py:41 _assert_no_symlink_chain
func assertNoSymlinkChain(path string) error {
	current := path
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return sourceMetadataError("atomic source metadata path must not traverse a symlink", nil)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

// requiredMetadataText 必填文本校验。
// Python: source_metadata.py:52 _required_text
func requiredMetadataText(value string, name string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", sourceMetadataError(fmt.Sprintf("atomic source metadata %s is invalid", name), nil)
	}
	return strings.TrimSpace(value), nil
}

// displayTitle 折叠空白并截断到 512 字符。
// Python: source_metadata.py:58 _display_title
func displayTitle(value string) string {
	title := strings.Join(strings.Fields(value), " ")
	if len([]rune(title)) > 512 {
		runes := []rune(title)
		title = string(runes[:512])
	}
	if title == "" {
		return "Atomic source"
	}
	return title
}

// ipv6Host 主机部分是否已是方括号 IPv6。
func ipv6Host(host string) bool {
	return strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]")
}

// rebuildURL 等价 Python urlunsplit((scheme, netloc, path, "", ""))。
func rebuildURL(scheme string, netloc string, path string) string {
	if netloc == "" {
		if path == "" {
			return scheme + ":"
		}
		return scheme + ":" + path
	}
	return scheme + "://" + netloc + path
}

// latestHash 快照 > 内容 > revision 的哈希优先级。
// Python: source_metadata.py:90 _latest_hash
func latestHash(item *RawChangeItem) string {
	var data []byte
	switch value := item.RawSnapshot.(type) {
	case []byte:
		data = value
	case string:
		data = []byte(value)
	default:
		if item.Content != nil {
			data = []byte(*item.Content)
		} else {
			data = []byte(item.RevisionID)
		}
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// sourceTypeOf 资源类型：metadata.resource 优先，回落 provider。
// Python: source_metadata.py:110 _source_type
func sourceTypeOf(item *RawChangeItem, provider string) string {
	resource, ok := item.Metadata["resource"].(string)
	if !ok {
		resource = provider
	}
	value, err := requiredMetadataText(resource, "source_type")
	if err != nil {
		return provider
	}
	return value
}

// renderSourceMetadata 渲染元数据 Markdown。
// Python: source_metadata.py:115 _render_source_metadata
func renderSourceMetadata(metadata map[string]string) []byte {
	title := displayTitle(metadata["title"])
	var fields []string
	for _, name := range metadataFields {
		encoded, _ := json.Marshal(metadata[name])
		fields = append(fields, fmt.Sprintf("- %s: %s", name, string(encoded)))
	}
	return []byte(fmt.Sprintf("# %s\n\n%s\n", title, strings.Join(fields, "\n")))
}

// atomicMetadataWrite 临时文件 + fsync + rename 原子替换，失败清理。
// Python: source_metadata.py:121 _atomic_write
func atomicMetadataWrite(path string, data []byte) error {
	tempFile, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".")
	if err != nil {
		return sourceMetadataError("atomic source metadata could not be written", err)
	}
	temporary := tempFile.Name()
	defer func() {
		if temporary != "" {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := tempFile.Write(data); err != nil {
		tempFile.Close()
		return sourceMetadataError("atomic source metadata could not be written", err)
	}
	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return sourceMetadataError("atomic source metadata could not be written", err)
	}
	if err := tempFile.Close(); err != nil {
		return sourceMetadataError("atomic source metadata could not be written", err)
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return sourceMetadataError("atomic source metadata path must not be a symlink", nil)
	}
	if err := os.Rename(temporary, path); err != nil {
		return sourceMetadataError("atomic source metadata could not be written", err)
	}
	temporary = ""
	return nil
}
