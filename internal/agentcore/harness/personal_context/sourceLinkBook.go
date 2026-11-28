package personal_context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// 每次 run 的原文链接登记与发布期目标解析。
// Python: openjiuwen/harness/personal_context/source_link_book.py（一比一移植）。
// ntpath/posixpath 双路径语义按 locator 前缀分流（Windows 盘符/UNC 走 Windows 规则）。

// ──────────────────────────── 全局变量 ────────────────────────────

// sourceLinkPattern 已登记源链接令牌。
// Python: source_link_book.py:11 _LINK
var sourceLinkPattern = regexp.MustCompile(`\[([^\]\n]*)\]\(pcs-source-link:([0-9a-f]{32})\)`)

// sourceLinkTokenPrefix 程序令牌前缀。
// Python: source_link_book.py:12 _TOKEN_PREFIX
const sourceLinkTokenPrefix = "pcs-source-link:"

// windowsDrivePathPattern Windows 盘符路径前缀。
var windowsDrivePathPattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// dosDevicePathPattern file:// 形式下的 /C:/ 前缀。
var dosDevicePathPattern = regexp.MustCompile(`^/[A-Za-z]:/`)

// escapedPunctuationPattern Markdown 转义标点还原。
func escapedPunctuationUnescape(target string) string {
	var b strings.Builder
	for index := 0; index < len(target); index++ {
		if target[index] == '\\' && index+1 < len(target) &&
			strings.ContainsRune("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", rune(target[index+1])) {
			b.WriteByte(target[index+1])
			index++
			continue
		}
		b.WriteByte(target[index])
	}
	return b.String()
}

// ──────────────────────────── 类型 ────────────────────────────

// SourceLinkRecord 单条登记记录。
// Python: source_link_book.py register() 内 record 字典（固定五字段）。
type SourceLinkRecord struct {
	// SourceID 原文所属源 ID
	SourceID string `json:"source_id"`
	// Origin 登记时的原文定位符
	Origin string `json:"origin"`
	// OriginalTarget 链接原始目标
	OriginalTarget string `json:"original_target"`
	// ResolvedTarget 解析后的规范目标
	ResolvedTarget string `json:"resolved_target"`
	// Label 链接标签
	Label string `json:"label"`
}

// SourceLinkBook 身份 → 记录 的登记簿。
type SourceLinkBook map[string]SourceLinkRecord

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterSourceLinks 登记原文位置身份，不读取原始目标。
// Python: source_link_book.py:80 register_source_links
func RegisterSourceLinks(markdown string, origin string) (string, SourceLinkBook, error) {
	book := SourceLinkBook{}
	originID, err := SourceIDForLocator(origin)
	if err != nil {
		return "", nil, err
	}
	register := func(label string, target string) string {
		record := SourceLinkRecord{
			SourceID:       originID,
			Origin:         origin,
			OriginalTarget: target,
			ResolvedTarget: resolveLinkTarget(origin, target),
			Label:          label,
		}
		identity := record.Identity()
		book[identity] = record
		// Python: _text(label or target)——label 非空取 label，否则取 target
		display := label
		if display == "" {
			display = target
		}
		return fmt.Sprintf("[原文链接：%s](%s%s)", sourceLinkText(display), sourceLinkTokenPrefix, identity)
	}
	out := NormalizeSourceLinks(markdown, register)
	return out, book, nil
}

// SourceLinkPreview 有界摘要中保留标签而不截断程序令牌。
// Python: source_link_book.py:99 source_link_preview
func SourceLinkPreview(markdown string) string {
	return sourceLinkPattern.ReplaceAllString(markdown, "$1")
}

// CollectSourceLinkBook 在 Filesystem 模型运行前装载程序写入的记录。
// Python: source_link_book.py:104 collect_source_link_book
func CollectSourceLinkBook(documents []map[string]any) (SourceLinkBook, error) {
	book := SourceLinkBook{}
	for _, document := range documents {
		rawRecords, ok := document["source_links"].(map[string]any)
		if !ok {
			return nil, sourceLinkError("source link book is invalid")
		}
		for identity, rawRecord := range rawRecords {
			recordMap, ok := rawRecord.(map[string]any)
			if !ok || len(recordMap) != 5 {
				return nil, sourceLinkError("source link record is invalid")
			}
			record := SourceLinkRecord{}
			for _, name := range []string{"source_id", "origin", "original_target", "resolved_target", "label"} {
				value, ok := recordMap[name].(string)
				if !ok {
					return nil, sourceLinkError("source link record values are invalid")
				}
				switch name {
				case "source_id":
					record.SourceID = value
				case "origin":
					record.Origin = value
				case "original_target":
					record.OriginalTarget = value
				case "resolved_target":
					record.ResolvedTarget = value
				case "label":
					record.Label = value
				}
			}
			expected := record.Identity()
			expectedSourceID, err := SourceIDForLocator(record.Origin)
			if err != nil || identity != expected || record.SourceID != expectedSourceID {
				return nil, sourceLinkError("source link record identity is invalid")
			}
			book[identity] = record
		}
	}
	return book, nil
}

// ResolveSourceLinks 只重写已登记令牌，随后走常规引用校验。
// Python: source_link_book.py:176 resolve_source_links
func ResolveSourceLinks(contextRoot string, finalContextRoot string, sourceRoot string, book SourceLinkBook) error {
	var targets map[string]string
	if len(book) > 0 {
		var err error
		targets, err = registeredTargets(sourceRoot)
		if err != nil {
			return err
		}
	} else {
		targets = map[string]string{}
	}
	type replacement struct {
		page string
		text string
	}
	var replacements []replacement
	walkErr := filepath.WalkDir(contextRoot, func(page string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(page, ".md") {
			return nil
		}
		raw, readErr := os.ReadFile(page)
		if readErr != nil {
			return readErr
		}
		text := string(raw)
		relative, relErr := filepath.Rel(contextRoot, page)
		if relErr != nil {
			return relErr
		}
		pageParent := filepath.Join(finalContextRoot, filepath.Dir(relative))
		result, resolveErr := resolvePageSourceLinks(text, pageParent, targets, book)
		if resolveErr != nil {
			return resolveErr
		}
		if result != text {
			replacements = append(replacements, replacement{page: page, text: result})
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}
	for _, item := range replacements {
		if err := os.WriteFile(item.page, []byte(item.text), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sourceLinkError 构造 CONTEXT_PROACTIVE_PIPELINE_EXECUTION_ERROR。
// Python: source_link_book.py:36 _error
func sourceLinkError(message string) error {
	return BuildError(StatusCodeContextProactivePipelineExecutionError, exception.WithMsg(message))
}

// Identity 记录的稳定身份：json（键按字母序）sha256 前 32 位。
// Python: register() 内 hashlib.sha256(json.dumps(record, sort_keys=True))[:32]；
// sort_keys 顺序 = label, origin, original_target, resolved_target, source_id。
func (r SourceLinkRecord) Identity() string {
	encoded, _ := json.Marshal(struct {
		Label          string `json:"label"`
		Origin         string `json:"origin"`
		OriginalTarget string `json:"original_target"`
		ResolvedTarget string `json:"resolved_target"`
		SourceID       string `json:"source_id"`
	}{r.Label, r.Origin, r.OriginalTarget, r.ResolvedTarget, r.SourceID})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])[:32]
}

// sourceLinkText HTML 转义 + 令牌前缀中和。
// Python: source_link_book.py:29 _text
func sourceLinkText(value string) string {
	value = escapeHTMLQuoteFalse(value)
	for _, character := range "[]`\\*_" {
		value = strings.ReplaceAll(value, string(character), fmt.Sprintf("&#%d;", character))
	}
	return strings.ReplaceAll(value, sourceLinkTokenPrefix, "pcs&#45;source-link:")
}

// locatorKey 定位符规范键（Windows 路径/file/http 分流）。
// Python: source_link_book.py:40 _locator_key
func locatorKey(locator string) (string, error) {
	if windowsDrivePathPattern.MatchString(locator) || strings.HasPrefix(locator, "\\\\") {
		return windowsNormCase(windowsNormPath(locator)), nil
	}
	parsed, err := url.Parse(locator)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme == "http" || scheme == "https" {
		return NormalizeSourceLocator(locator)
	}
	if scheme == "file" {
		linkPath := unquotePath(parsed.Path)
		if parsed.Host != "" {
			linkPath = path.Join("//", parsed.Host, strings.TrimPrefix(linkPath, "/"))
		}
		if dosDevicePathPattern.MatchString(linkPath) {
			linkPath = linkPath[1:]
		}
		return locatorKey(linkPath)
	}
	return path.Clean(locator), nil
}

// resolveLinkTarget 解析目标为规范定位符；非法 scheme 返回空串。
// Python: source_link_book.py:56 _resolve_target
func resolveLinkTarget(origin string, target string) string {
	target = htmlUnescape(target)
	target = escapedPunctuationUnescape(target)
	if windowsDrivePathPattern.MatchString(target) || strings.HasPrefix(target, "\\\\") {
		key, err := locatorKey(target)
		if err != nil {
			return ""
		}
		return key
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "" && scheme != "file" && scheme != "http" && scheme != "https" {
		return ""
	}
	if parsed.Scheme != "" {
		key, keyErr := locatorKey(target)
		if keyErr != nil {
			return ""
		}
		return key
	}
	originParsed, originErr := url.Parse(origin)
	if originErr == nil && (strings.EqualFold(originParsed.Scheme, "http") || strings.EqualFold(originParsed.Scheme, "https")) {
		// Python urljoin(origin, target)：RFC 3986 相对引用解析（Go JoinPath 是
		// path.Join 语义，不等价，必须用 ResolveReference）。
		joined := originParsed.ResolveReference(&url.URL{Path: target})
		key, keyErr := locatorKey(joined.String())
		if keyErr != nil {
			return ""
		}
		return key
	}
	linkPath := unquotePath(parsed.Path)
	base, baseErr := locatorKey(origin)
	if baseErr != nil {
		return ""
	}
	if linkPath == "" {
		return base
	}
	if windowsDrivePathPattern.MatchString(base) || strings.HasPrefix(base, "\\\\") {
		joined := windowsJoin(windowsDirName(base), linkPath)
		key, keyErr := locatorKey(joined)
		if keyErr != nil {
			return ""
		}
		return key
	}
	joined := path.Join(path.Dir(base), linkPath)
	key, keyErr := locatorKey(joined)
	if keyErr != nil {
		return ""
	}
	return key
}

// registeredTargets 扫描 source-meta 的 src_*.md，键 → 首个命中文件。
// Python: source_link_book.py:124 _registered_targets
func registeredTargets(sourceRoot string) (map[string]string, error) {
	targets := map[string]string{}
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "src_") && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		record, readErr := ReadSourceMetadata(filepath.Join(sourceRoot, name))
		if readErr != nil {
			// 目标必须可用；此处不修复也不绕过 Context 自身出处引用的校验。
			continue
		}
		key, keyErr := locatorKey(record["locator"])
		if keyErr != nil {
			continue
		}
		if _, exists := targets[key]; !exists {
			targets[key] = filepath.Join(sourceRoot, name)
		}
	}
	return targets, nil
}

// resolvePageSourceLinks 重写单页中的已登记令牌。
// Python: source_link_book.py:138 _resolve_page_source_links —— rewrite() 的
// 两处 _error 上抛路径前置为显式校验（未登记令牌 / 重写后残留令牌前缀），
// 重写本体不再需要 panic 桥接。
func resolvePageSourceLinks(text string, pageParent string, targets map[string]string, book SourceLinkBook) (string, error) {
	for _, match := range sourceLinkPattern.FindAllStringSubmatch(text, -1) {
		if _, exists := book[match[2]]; !exists {
			return "", sourceLinkError("candidate contains an unregistered source link")
		}
	}
	resolve := func(match []string) string {
		record := book[match[2]]
		target, hasTarget := targets[record.ResolvedTarget]
		label := record.Label
		if label == "" {
			label = record.OriginalTarget
		}
		if hasTarget {
			relative, relErr := filepath.Rel(pageParent, target)
			if relErr != nil {
				relative = target
			}
			relative = strings.ReplaceAll(relative, "\\", "/")
			fragment := ""
			if parsed, err := url.Parse(record.OriginalTarget); err == nil {
				fragment = parsed.Fragment
			}
			suffix := ""
			if fragment != "" {
				suffix = fmt.Sprintf("（原文锚点：%s）", sourceLinkText(fragment))
			}
			return fmt.Sprintf("[%s](<%s>)%s", sourceLinkText(label), relative, suffix)
		}
		original := record.OriginalTarget
		if parsed, err := url.Parse(original); err == nil {
			scheme := strings.ToLower(parsed.Scheme)
			if (scheme == "http" || scheme == "https") && parsed.Hostname() != "" {
				destination := quoteURL(original)
				return fmt.Sprintf("[%s](<%s>)", sourceLinkText(label), destination)
			}
		}
		return fmt.Sprintf("%s（原文链接：%s）", sourceLinkText(record.Label), sourceLinkText(original))
	}
	rewritten := RewriteMarkdownProse(text, func(prose string) string {
		return sourceLinkPattern.ReplaceAllStringFunc(prose, func(raw string) string {
			return resolve(sourceLinkPattern.FindStringSubmatch(raw))
		})
	})
	if strings.Contains(rewritten, sourceLinkTokenPrefix) {
		return "", sourceLinkError("candidate contains a malformed or unregistered source link")
	}
	return rewritten, nil
}

// quoteURL 等价 Python quote(value, safe=":/?#[]@!$&'*,;=%+-._~")：
// 未保留字符与 safe 集原样，其余按 UTF-8 百分号编码（大写 hex）。
func quoteURL(value string) string {
	const safe = ":/?#[]@!$&'*,;=%+-._~"
	var b strings.Builder
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.IndexByte(safe, character) >= 0 {
			b.WriteByte(character)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", character)
	}
	return b.String()
}

// unquotePath 百分号解码。
func unquotePath(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

// windowsNormPath 等价 ntpath.normpath。
func windowsNormPath(value string) string {
	value = strings.ReplaceAll(value, "/", "\\")
	parts := strings.Split(value, "\\")
	var stack []string
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(stack) > 0 && stack[len(stack)-1] != ".." && !strings.HasSuffix(strings.ToLower(stack[len(stack)-1]), ":") {
				stack = stack[:len(stack)-1]
				continue
			}
			if len(stack) == 0 || strings.HasSuffix(strings.ToLower(stack[len(stack)-1]), ":") {
				continue
			}
			stack = append(stack, part)
		default:
			stack = append(stack, part)
		}
	}
	result := strings.Join(stack, "\\")
	if strings.HasPrefix(value, "\\\\") {
		return "\\\\" + result
	}
	if len(value) >= 2 && value[1] == ':' {
		if result == "" {
			result = value[:2] + "."
		}
		return result
	}
	if result == "" {
		return "."
	}
	return result
}

// windowsNormCase 等价 ntpath.normcase：小写 + 斜杠归一。
func windowsNormCase(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "/", "\\"))
}

// windowsJoin 等价 ntpath.join(dirname, path)。
func windowsJoin(dir string, name string) string {
	if dir == "" {
		return name
	}
	if strings.HasSuffix(dir, "\\") {
		return dir + name
	}
	return dir + "\\" + name
}

// windowsDirName 等价 ntpath.dirname。
func windowsDirName(value string) string {
	index := strings.LastIndexAny(value, "\\/")
	if index < 0 {
		return ""
	}
	return value[:index]
}
