// AutoReviewer 证据共享脱敏助手。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/reviewer_redaction.py（689 行）
//
// 原属 9.90d-2，因 9.90d-1 的 workspace_paths.py 末尾
// _redact_review_ui_secrets 依赖本文件的 redact_secret_values_preserve_format，
// 用户拍板提前到本批一起落地。
//
// 正则说明：Python re 的 lookbehind / lookahead / 反向引用 / DOTALL
// 在 Go 标准库 regexp（RE2）中不受支持，统一改用项目已引入的
// github.com/dlclark/regexp2（先例见 gateway/message_handler/at_file.go）：
//   - (?<!...) / (?<=...) / (?!...) / (?=...) 原生支持
//   - Python (?P=quote) → .NET \k<quote>
//   - Python re.DOTALL → regexp2.Singleline

package permissions

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/dlclark/regexp2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReviewerPathLocationResult 路径投影结果。
//
// Python: reviewer_path_location 返回的 dict {"base", "relative_path"}
type ReviewerPathLocationResult struct {
	// Base 根标识（workspace / platform_trusted_root / 盘符 anchor）
	Base string
	// RelativePath 脱敏后的相对路径
	RelativePath string
}

// redactTextOptions redact_text 的可选参数（Go 用 option 模式替代 Python 关键字参数）。
type redactTextOptions struct {
	maxLength int
}

// RedactTextOption redact_text / redact_secret_values 的选项函数。
type RedactTextOption func(*redactTextOptions)

// redactReviewablePayloadOptions redact_reviewable_payload_text 的选项。
type redactReviewablePayloadOptions struct {
	maxLength           int
	redactRelativePaths bool
}

// RedactReviewablePayloadOption redact_reviewable_payload_text 的选项函数。
type RedactReviewablePayloadOption func(*redactReviewablePayloadOptions)

// permissionUIOptions sanitize_permission_ui_payload 的选项。
type permissionUIOptions struct {
	maxDepth      int
	maxItems      int
	maxStringLen  int
	maxTotalBytes int
}

// PermissionUIOption sanitize_permission_ui_payload 的选项函数。
type PermissionUIOption func(*permissionUIOptions)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// bt 反引号字符（Go raw string 无法直接内嵌，正则字符集里需拼接）。
const bt = "\x60"

const (
	// DefaultMaxRedactedTextLength 脱敏文本默认上限
	DefaultMaxRedactedTextLength = 240
	// DefaultMaxRedactedItems 列表默认条目上限
	DefaultMaxRedactedItems = 5
	// ReviewablePayloadTextLimit 可复核 payload 文本上限
	ReviewablePayloadTextLimit = 1024
	// PermissionUIPayloadMaxDepth 权限 UI payload 最大深度
	PermissionUIPayloadMaxDepth = 6
	// PermissionUIPayloadMaxItems 权限 UI payload 每层最大条目
	PermissionUIPayloadMaxItems = 32
	// PermissionUIPayloadMaxStringLength 权限 UI payload 字符串上限
	PermissionUIPayloadMaxStringLength = 1024
	// PermissionUIPayloadMaxTotalBytes 权限 UI payload 总字节上限
	PermissionUIPayloadMaxTotalBytes = 16 * 1024

	// PermissionUIRedacted 密钥键替换标记
	PermissionUIRedacted = "[REDACTED]"
	// PermissionUITruncated 截断标记
	PermissionUITruncated = "[TRUNCATED]"
	// PermissionUIUnavailable 不可用标记
	PermissionUIUnavailable = "[UNAVAILABLE]"

	// reviewLocationLimit 路径投影相对部分长度上限
	reviewLocationLimit = 240
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pathLikePattern 匹配常见绝对路径
	pathLikePattern = regexp2.MustCompile(
		`(?:`+
			`(?:/Users/|/private/|/tmp/)[^\s"']+`+
			`|/home/[^/\s"']+/[^\s"']+`+
			`|~(?:[/\\][^\s"']+)+`+
			`|[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/]`+
			`(?:(?!\s+[A-Za-z]:[\\/])[^'";|&<>`+bt+`,，。；、\r\n])+`+
			`)`,
		regexp2.None,
	)
	// fileURIPattern 匹配 file:// URI
	fileURIPattern = regexp2.MustCompile(`file://[^\s"'<>]*`, regexp2.IgnoreCase)
	// absoluteURIPrefixPattern 匹配绝对 URI 前缀
	absoluteURIPrefixPattern = regexp2.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`, regexp2.None)

	// reviewablePosixAbsolutePathPattern 匹配 POSIX 绝对路径
	reviewablePosixAbsolutePathPattern = regexp2.MustCompile(
		`(?<![\w:/])/(?!/)(?:`+
			`(?:Applications|Library|System|Users|Volumes|bin|dev|etc|home|media|mnt|opt|`+
			`private|root|run|sbin|srv|tmp|usr|var|workspace)`+
			`(?:/[^'";|&<>`+bt+`,，。；、\r\n\s)\]}]+)+`+
			`|[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)+\.[A-Za-z0-9]{1,16}`+
			`)`,
		regexp2.None,
	)
	// reviewableWindowsAbsolutePathPattern 匹配 Windows 绝对路径
	reviewableWindowsAbsolutePathPattern = regexp2.MustCompile(
		`\b[A-Za-z]:[\\/][^'";|&<>`+bt+`,，。；、\r\n\s]+`,
		regexp2.None,
	)
	// reviewableWindowsUNCPathPattern 匹配 Windows UNC 路径
	reviewableWindowsUNCPathPattern = regexp2.MustCompile(
		`\\\\[^\\/\s'";|&<>`+bt+`,，。；、\r\n]+[\\/][^'";|&<>`+bt+`,，。；、\r\n]+`,
		regexp2.None,
	)
	// reviewableQuotedLocalPathPattern 匹配引号包裹的本地路径（反向引用）
	reviewableQuotedLocalPathPattern = regexp2.MustCompile(
		`(?<quote>["'])(?:`+
			`/Users/|/home/|/private/|/tmp/|/workspace/|~[/\\]|`+
			`[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/]`+
			`)[^"']+\k<quote>`,
		regexp2.None,
	)
	// reviewableEscapedLocalPathPattern 匹配转义空格的本地路径
	reviewableEscapedLocalPathPattern = regexp2.MustCompile(
		`(?:/Users/|/home/|/private/|/tmp/|/workspace/|~[/\\])`+
			`(?:\\\s|[^\s"';|&<>`+bt+`,，。；、\r\n])+`,
		regexp2.None,
	)
	// reviewableRelativeFilePathPattern 匹配相对文件路径
	reviewableRelativeFilePathPattern = regexp2.MustCompile(
		`(?<![\w:/])(?:\.{1,2}/)?[A-Za-z0-9._-]+`+
			`(?:/[A-Za-z0-9._-]+)+\.[A-Za-z0-9]{1,16}(?![\w./-])`,
		regexp2.None,
	)

	// secretKeyPattern 匹配密钥键名（整键）
	secretKeyPattern = regexp2.MustCompile(
		`^(api[_-]?key|authorization|credential|credentials|password|`+
			`private[_-]?key|secret|token|access[_-]?token|refresh[_-]?token|`+
			`session[_-]?token|client[_-]?secret|aws[_-]?access[_-]?key[_-]?id|`+
			`aws[_-]?secret[_-]?access[_-]?key)$`,
		regexp2.IgnoreCase,
	)
	// secretURLQueryKeyPattern 匹配 URL query 中的敏感键
	secretURLQueryKeyPattern = regexp2.MustCompile(
		`^(api[_-]?key|apikey|auth|authorization|code|credential|credentials|`+
			`key|oauth[_-]?token|password|private[_-]?key|refresh[_-]?token|secret|`+
			`session|session[_-]?id|session[_-]?token|sig|signature|token|`+
			`access[_-]?token|client[_-]?secret|aws[_-]?access[_-]?key[_-]?id|`+
			`aws[_-]?secret[_-]?access[_-]?key)$`,
		regexp2.IgnoreCase,
	)
	// secretAssignmentPattern 匹配 key=value / key: value 赋值
	secretAssignmentPattern = regexp2.MustCompile(
		`(["']?(?:api[_-]?key|authorization|credential|credentials|password|`+
			`private[_-]?key|secret|token|access[_-]?token|refresh[_-]?token|`+
			`session[_-]?token|client[_-]?secret|aws[_-]?access[_-]?key[_-]?id|`+
			`aws[_-]?secret[_-]?access[_-]?key)["']?\s*[:=]\s*)`+
			`(?:"[^"]*"|'[^']*'|[^"'\s,;}&|<>`+bt+`]+)`,
		regexp2.IgnoreCase,
	)
	// authValuePattern 匹配 Bearer / Basic 认证值
	authValuePattern = regexp2.MustCompile(
		`\b(Bearer|Basic)\s+[A-Za-z0-9._~+/=-]{8,}`,
		regexp2.IgnoreCase,
	)
	// commonSecretValuePatterns 常见密钥字面量模式（sk- / ghp_ / AKIA / xox / PEM）
	commonSecretValuePatterns = []*regexp2.Regexp{
		regexp2.MustCompile(`(?<![A-Za-z0-9_])sk-[A-Za-z0-9_-]{8,}`, regexp2.None),
		regexp2.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`, regexp2.None),
		regexp2.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`, regexp2.None),
		regexp2.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`, regexp2.None),
		regexp2.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`, regexp2.None),
		regexp2.MustCompile(
			`-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`,
			regexp2.Singleline,
		),
	}
	// whitespacePattern 连续空白
	whitespacePattern = regexp2.MustCompile(`\s+`, regexp2.None)
	// camelCaseBoundaryPattern 驼峰边界（后行 + 前行断言）
	camelCaseBoundaryPattern = regexp2.MustCompile(`(?<=[a-z0-9])(?=[A-Z])`, regexp2.None)
	// nonAlnumKeyPattern 非字母数字键字符
	nonAlnumKeyPattern = regexp2.MustCompile(`[^a-z0-9]+`, regexp2.None)

	// absoluteURLValuePattern 匹配任意 scheme://... 值（preserve_format 用）
	absoluteURLValuePattern = regexp2.MustCompile(
		`[A-Za-z][A-Za-z0-9+.-]*://[^\s'"<>;|`+bt+`(){}]+`,
		regexp2.None,
	)
	// urlQueryAssignmentPattern 匹配 URL query/fragment 的 key=value
	urlQueryAssignmentPattern = regexp2.MustCompile(
		`([?&#])([^=&#]+)=([^&#]*)`,
		regexp2.None,
	)

	// permissionUISecretKeyTokens 密钥键 token 集合
	permissionUISecretKeyTokens = map[string]bool{
		"auth": true, "authorization": true, "cookie": true, "credential": true,
		"credentials": true, "password": true, "passwd": true, "pwd": true,
		"secret": true, "token": true,
	}
	// permissionUISecretKeyCompact 密钥键紧凑形式集合
	permissionUISecretKeyCompact = map[string]bool{
		"accesskey": true, "accesskeyid": true, "apikey": true, "authheader": true,
		"authorization": true, "authorizationheader": true, "awsaccesskeyid": true,
		"awssecretaccesskey": true, "clientsecret": true, "cookie": true,
		"privatekey": true, "proxyauth": true, "proxyauthorization": true,
		"setcookie": true, "signingkey": true,
	}
	// permissionUISecretKeySuffixes 密钥键后缀
	permissionUISecretKeySuffixes = []string{
		"accesskey", "accesskeyid", "apikey", "authheader", "authorization",
		"authorizationheader", "clientsecret", "credential", "credentials",
		"privatekey", "proxyauth", "proxyauthorization", "password", "secret",
		"secretkey", "signingkey", "token",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithRedactTextMaxLength 设置脱敏文本长度上限。
func WithRedactTextMaxLength(n int) RedactTextOption {
	return func(o *redactTextOptions) { o.maxLength = n }
}

// WithRedactRelativePaths 设置是否脱敏相对文件路径。
func WithRedactRelativePaths(v bool) RedactReviewablePayloadOption {
	return func(o *redactReviewablePayloadOptions) { o.redactRelativePaths = v }
}

// WithPermissionUIMaxDepth 设置权限 UI payload 最大深度。
func WithPermissionUIMaxDepth(n int) PermissionUIOption {
	return func(o *permissionUIOptions) { o.maxDepth = n }
}

// WithPermissionUIMaxTotalBytes 设置权限 UI payload 总字节上限。
func WithPermissionUIMaxTotalBytes(n int) PermissionUIOption {
	return func(o *permissionUIOptions) { o.maxTotalBytes = n }
}

// RedactText 返回带常见密钥脱敏的有界文本表示。
//
// Python: redact_text(value, *, max_length=DEFAULT_MAX_REDACTED_TEXT_LENGTH)
func RedactText(value any, opts ...RedactTextOption) string {
	o := redactTextOptions{maxLength: DefaultMaxRedactedTextLength}
	for _, opt := range opts {
		opt(&o)
	}
	rawText := stringify(value)
	text := strings.NewReplacer("\r", " ", "\n", " ").Replace(rawText)
	text = mustReplace(fileURIPattern, text, "file://[redacted-path]")
	text = mustReplace(pathLikePattern, text, "[path]")
	return redactSecretValuesWithLimit(text, o.maxLength)
}

// ReviewerPathLocation 投影 Smart 证据路径；绝不改变底层访问身份。
//
// Python: reviewer_path_location(raw_path, *, workspace_root, platform_trusted_root)
// 返回 (dict | None, status)，status ∈ complete/redacted/omitted/unavailable。
func ReviewerPathLocation(rawPath, workspaceRoot, platformTrustedRoot string) (*ReviewerPathLocationResult, string) {
	if containsControlChar(rawPath) {
		return nil, "redacted"
	}
	rawNative := strings.ReplaceAll(rawPath, string(os.PathSeparator), "/")
	rawSecretParts := map[string]bool{}
	rawParts := strings.Split(rawNative, "/")
	safeParts := strings.Split(redactPathValues(rawNative), "/")
	for i, part := range rawParts {
		if i < len(safeParts) && part != safeParts[i] && part != "" && part != "." && part != ".." {
			rawSecretParts[part] = true
		}
	}
	expanded := expandHome(rawPath)
	if !filepath.IsAbs(expanded) || strings.HasPrefix(rawPath, "//") || strings.HasPrefix(rawPath, "\\\\") {
		return nil, "unavailable"
	}
	target := resolvePathStrictFalse(expanded)
	targetPosix := filepath.ToSlash(target)
	if containsControlChar(targetPosix) {
		return nil, "redacted"
	}
	safeTarget := redactPathValues(targetPosix)
	targetSegs := strings.Split(targetPosix, "/")
	safeSegs := strings.Split(safeTarget, "/")
	merged := make([]string, len(safeSegs))
	copy(merged, safeSegs)
	for i := range merged {
		if i < len(targetSegs) && safeSegs[i] == targetSegs[i] && rawSecretParts[targetSegs[i]] {
			merged[i] = "[redacted]"
		}
	}
	safeTarget = strings.Join(merged, "/")

	base := ""
	relative := ""
	for _, candidate := range []struct{ name, root string }{
		{"workspace", workspaceRoot},
		{"platform_trusted_root", platformTrustedRoot},
	} {
		if candidate.root == "" {
			continue
		}
		r := expandHome(candidate.root)
		if !filepath.IsAbs(r) {
			continue
		}
		rr := resolvePathStrictFalse(r)
		if rel, ok := relativeToPosix(target, rr); ok {
			base, relative = candidate.name, rel
			break
		}
	}
	if base == "" {
		base = filepath.VolumeName(target) + string(os.PathSeparator)
		relative = strings.TrimPrefix(filepath.ToSlash(target), filepath.ToSlash(base))
	}
	if relative == "" {
		relative = "."
	}
	relativeSegs := strings.Split(relative, "/")
	if len(relativeSegs) > len(merged) {
		return nil, "unavailable"
	}
	safeRelative := strings.Join(merged[len(merged)-len(relativeSegs):], "/")
	if len(relativeSegs) == 1 && relative == "." {
		safeRelative = "."
	}
	if max(len(relative), len(safeRelative)) > reviewLocationLimit {
		return nil, "omitted"
	}
	status := "complete"
	if safeRelative != relative {
		status = "redacted"
	}
	return &ReviewerPathLocationResult{Base: base, RelativePath: safeRelative}, status
}

// RedactReviewablePayloadText 返回 reviewer 可见的 payload 文本（广谱路径 + 密钥脱敏）。
//
// Python: redact_reviewable_payload_text(value, *, max_length, redact_relative_paths)
func RedactReviewablePayloadText(value any, opts ...RedactReviewablePayloadOption) string {
	o := redactReviewablePayloadOptions{maxLength: ReviewablePayloadTextLimit}
	for _, opt := range opts {
		opt(&o)
	}
	rawText := stringify(value)
	text := strings.NewReplacer("\r", " ", "\n", " ").Replace(rawText)
	text = mustReplace(fileURIPattern, text, "file://[redacted-path]")
	text = mustReplace(reviewableQuotedLocalPathPattern, text, "[path]")
	text = mustReplace(reviewableEscapedLocalPathPattern, text, "[path]")
	text = mustReplace(reviewablePosixAbsolutePathPattern, text, "[path]")
	text = mustReplace(reviewableWindowsAbsolutePathPattern, text, "[path]")
	text = mustReplace(reviewableWindowsUNCPathPattern, text, "[path]")
	if o.redactRelativePaths {
		text = mustReplace(reviewableRelativeFilePathPattern, text, "[path]")
	}
	return RedactText(text, WithRedactTextMaxLength(o.maxLength))
}

// RedactSecretValues 返回有界文本，脱敏密钥值但保留路径。
//
// Python: redact_secret_values(value, *, max_length)
func RedactSecretValues(value any, opts ...RedactTextOption) string {
	o := redactTextOptions{maxLength: DefaultMaxRedactedTextLength}
	for _, opt := range opts {
		opt(&o)
	}
	return redactSecretValuesWithLimit(stringify(value), o.maxLength)
}

// RedactSecretValuesPreserveFormat 脱敏值但不改变路径与可执行空白。
//
// Python: redact_secret_values_preserve_format(value, *, max_length, require_literal_values)
// maxLength 省略时使用 DefaultMaxRedactedTextLength；Python 的 max_length=None
// 用 RedactSecretValuesPreserveFormatNoLimit。
// 本函数吞掉 require_literal_values=True 时的错误；需要感知错误请用 E 变体。
func RedactSecretValuesPreserveFormat(value any, maxLength ...int) string {
	limit := DefaultMaxRedactedTextLength
	if len(maxLength) > 0 {
		limit = maxLength[0]
	}
	got, _ := redactSecretValuesPreserveFormatE(stringify(value), &limit, false)
	return got
}

// RedactSecretValuesPreserveFormatNoLimit 同 PreserveFormat 但不截断
// （对齐 Python max_length=None）。
func RedactSecretValuesPreserveFormatNoLimit(value any) string {
	got, _ := redactSecretValuesPreserveFormatE(stringify(value), nil, false)
	return got
}

// RedactSecretValuesPreserveFormatE 同 RedactSecretValuesPreserveFormat，
// 但把 require_literal_values 的 ValueError 以 error 返回（偏差 V-10 口径）。
func RedactSecretValuesPreserveFormatE(value string, requireLiteralValues bool) (string, error) {
	limit := DefaultMaxRedactedTextLength
	return redactSecretValuesPreserveFormatE(value, &limit, requireLiteralValues)
}

// RedactReviewerValue 用既有权限密钥键规则脱敏完整 JSON 证据。
//
// Python: redact_reviewer_value(value)
func RedactReviewerValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			normalized, compact := normalizedPermissionUIKey(key)
			safeKey, _ := redactSecretValuesPreserveFormatE(key, nil, false)
			if isPermissionUISecretKey(normalized, compact) {
				result[safeKey] = "[redacted]"
			} else {
				result[safeKey] = RedactReviewerValue(item)
			}
		}
		return result
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = RedactReviewerValue(item)
		}
		return out
	case string:
		got, _ := redactSecretValuesPreserveFormatE(v, nil, true)
		return got
	default:
		return value
	}
}

// RedactJSONValue 返回 JSON-safe 的脱敏值，保留简单结构。
//
// Python: redact_json_value(value, *, max_length, max_items)
func RedactJSONValue(value any, opts ...RedactTextOption) any {
	o := redactTextOptions{maxLength: DefaultMaxRedactedTextLength}
	for _, opt := range opts {
		opt(&o)
	}
	return redactJSONValue(value, o.maxLength, DefaultMaxRedactedItems)
}

// RedactURL 返回有界 URL，凭据与敏感 query 值已脱敏。
//
// Python: redact_url(value, *, max_length)
func RedactURL(value any, opts ...RedactTextOption) string {
	o := redactTextOptions{maxLength: DefaultMaxRedactedTextLength}
	for _, opt := range opts {
		opt(&o)
	}
	rawURL := strings.TrimSpace(stringify(value))
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return RedactText(rawURL, WithRedactTextMaxLength(o.maxLength))
	}
	if strings.EqualFold(parsed.Scheme, "file") && regexp2Match(absoluteURIPrefixPattern, rawURL) {
		return "file://[redacted-path]"
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return RedactText(rawURL, WithRedactTextMaxLength(o.maxLength))
	}
	safeQuery := buildSafeQuery(parsed.RawQuery, o.maxLength)
	safeFragment := redactURLFragment(parsed.Fragment, o.maxLength)
	redacted := (&url.URL{
		Scheme:   parsed.Scheme,
		Host:     safeURLNetloc(parsed),
		Path:     RedactText(parsed.Path, WithRedactTextMaxLength(o.maxLength)),
		RawQuery: safeQuery,
		Fragment: safeFragment,
	}).String()
	// Python urlencode(...).replace("%5Bredacted%5D", "[redacted]") 是字符串层还原；
	// Go 的 URL.String() 会重新转义方括号，这里在最终串上做等价还原。
	redacted = strings.ReplaceAll(redacted, "%5Bredacted%5D", "[redacted]")
	return truncateText(redacted, o.maxLength)
}

// SanitizePermissionUIPayload 返回仅适合权限 UI 展示的有界脱敏副本。
//
// Python: sanitize_permission_ui_payload(value, *, max_depth, max_items, ...)
// 显示边界必须 fail-closed：任何异常返回 [UNAVAILABLE]，绝不回落原值。
func SanitizePermissionUIPayload(value any, opts ...PermissionUIOption) (result any) {
	o := permissionUIOptions{
		maxDepth:      PermissionUIPayloadMaxDepth,
		maxItems:      PermissionUIPayloadMaxItems,
		maxStringLen:  PermissionUIPayloadMaxStringLength,
		maxTotalBytes: PermissionUIPayloadMaxTotalBytes,
	}
	for _, opt := range opts {
		opt(&o)
	}
	if min(o.maxDepth, o.maxItems, o.maxStringLen) < 1 || o.maxTotalBytes < 64 {
		panic(fmt.Errorf("permission UI payload limits are too small"))
	}
	// 显示边界必须 fail-closed：任何 panic 都回落 [UNAVAILABLE]，绝不返回原值。
	defer func() {
		if r := recover(); r != nil {
			result = PermissionUIUnavailable
		}
	}()
	sanitized := sanitizePermissionUIValue(value, 0, o, map[uintptr]bool{})
	return boundPermissionUIPayload(sanitized, o.maxTotalBytes)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// redactPathValues 复用密钥匹配但不丢失路径分量或跨根跨度。
//
// Python: _redact_path_values(value)
func redactPathValues(value string) string {
	text := mustReplaceFunc(authValuePattern, value, func(m regexp2.Match) string {
		prefix := m.GroupByName("1")
		if prefix == nil {
			prefix = m.GroupByNumber(1)
		}
		rest := strings.TrimLeft(m.String()[len(prefix.String()):], " \t")
		return prefix.String() + " " + maskParts(rest)
	})
	for _, p := range commonSecretValuePatterns {
		text = mustReplaceFunc(p, text, func(m regexp2.Match) string {
			return maskParts(m.String())
		})
	}
	return mustReplaceFunc(secretAssignmentPattern, text, func(m regexp2.Match) string {
		g := m.GroupByNumber(1)
		prefix := ""
		if g != nil {
			prefix = g.String()
		}
		return prefix + maskParts(strings.TrimPrefix(m.String(), prefix))
	})
}

// maskParts 把非斜杠片段替换为 [redacted]，保留 "/" 结构。
//
// Python: _redact_path_values 内的 mask_parts
func maskParts(secret string) string {
	parts := strings.Split(secret, "/")
	for i, p := range parts {
		if p != "" {
			parts[i] = "[redacted]"
		}
	}
	return strings.Join(parts, "/")
}

// redactSecretValuesWithLimit 实现 redact_secret_values 主体。
//
// Python: redact_secret_values 的 270-280 行
func redactSecretValuesWithLimit(value string, maxLength int) string {
	text := strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
	text = mustReplaceFunc(authValuePattern, text, func(m regexp2.Match) string {
		g := m.GroupByNumber(1)
		prefix := ""
		if g != nil {
			prefix = g.String()
		}
		return prefix + " [redacted]"
	})
	for _, p := range commonSecretValuePatterns {
		text = mustReplace(p, text, "[redacted]")
	}
	text = mustReplaceFunc(secretAssignmentPattern, text, func(m regexp2.Match) string {
		g := m.GroupByNumber(1)
		prefix := ""
		if g != nil {
			prefix = g.String()
		}
		return prefix + "[redacted]"
	})
	normalized := strings.TrimSpace(mustReplace(whitespacePattern, text, " "))
	return truncateText(normalized, maxLength)
}

// redactSecretValuesPreserveFormatE 实现 preserve_format 主体并返回 error。
//
// Python: redact_secret_values_preserve_format 的 289-309 行
func redactSecretValuesPreserveFormatE(text string, maxLength *int, requireLiteral bool) (string, error) {
	if requireLiteral {
		// 不把表达式伪装成密钥擦掉：这是脱敏资格检查，不是命令解释。
		for m, err := absoluteURLValuePattern.FindStringMatch(text); err == nil && m != nil; m, err = absoluteURLValuePattern.FindNextMatch(m) {
			_ = m
		}
		if m, err := secretAssignmentPattern.FindStringMatch(text); err == nil && m != nil {
			g := m.GroupByNumber(1)
			prefix := ""
			if g != nil {
				prefix = g.String()
			}
			val := strings.TrimPrefix(m.String(), prefix)
			if !strings.HasPrefix(val, "'") && strings.ContainsAny(val, "$`()\\\n\r") {
				return text, fmt.Errorf("reviewer_redaction_incomplete")
			}
		}
	}
	redacted := mustReplaceFunc(absoluteURLValuePattern, text, func(m regexp2.Match) string {
		return redactURLValues(m.String())
	})
	redacted = mustReplaceFunc(authValuePattern, redacted, func(m regexp2.Match) string {
		g := m.GroupByNumber(1)
		prefix := ""
		if g != nil {
			prefix = g.String()
		}
		return prefix + " [redacted]"
	})
	for _, p := range commonSecretValuePatterns {
		redacted = mustReplace(p, redacted, "[redacted]")
	}
	redacted = mustReplaceFunc(secretAssignmentPattern, redacted, func(m regexp2.Match) string {
		g := m.GroupByNumber(1)
		prefix := ""
		if g != nil {
			prefix = g.String()
		}
		val := strings.TrimPrefix(m.String(), prefix)
		if requireLiteral && !strings.HasPrefix(val, "'") && strings.ContainsAny(val, "$`()\\\n\r") {
			return m.String()
		}
		return prefix + "[redacted]"
	})
	if maxLength == nil {
		return redacted, nil
	}
	return truncateText(redacted, *maxLength), nil
}

// redactURLValues 复用 URL 密钥键规则，不规范化外围源码。
//
// Python: _redact_url_values(match)
func redactURLValues(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "[redacted-url]"
	}
	// Python 的 urlparse().netloc 含 userinfo；Go 的 url.Parse 把 userinfo
	// 拆到 parsed.User，Host 不含 "@"。需显式重建 netloc 才能对齐。
	if parsed.User != nil {
		hostPort := parsed.Host
		safeNetloc := "[redacted]@" + hostPort
		if idx := strings.LastIndex(value, parsed.User.String()+"@"); idx >= 0 {
			userinfoEnd := idx + len(parsed.User.String()) + 1
			value = value[:idx] + safeNetloc + value[userinfoEnd:]
		}
	}
	return mustReplaceFunc(urlQueryAssignmentPattern, value, func(m regexp2.Match) string {
		sep, key, val := m.GroupByNumber(1).String(), m.GroupByNumber(2).String(), m.GroupByNumber(3).String()
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			decoded = key
		}
		if regexp2FullMatch(secretURLQueryKeyPattern, decoded) {
			return sep + key + "=[redacted]"
		}
		return sep + key + "=" + val
	})
}

// redactJSONValue 递归实现。
func redactJSONValue(value any, maxLength, maxItems int) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, nested := range v {
			out[fmt.Sprint(key)] = redactedMappingValue(key, nested, maxLength, maxItems)
		}
		return out
	case string:
		if isAbsoluteURL(v) {
			return RedactURL(v, WithRedactTextMaxLength(maxLength))
		}
		return RedactText(v, WithRedactTextMaxLength(maxLength))
	case bool, int, int64, float64, nil:
		return value
	case float32:
		return float64(v)
	case []any:
		n := min(len(v), maxItems)
		out := make([]any, n)
		for i := 0; i < n; i++ {
			out[i] = redactJSONValue(v[i], maxLength, maxItems)
		}
		return out
	default:
		return RedactText(v, WithRedactTextMaxLength(maxLength))
	}
}

// redactedMappingValue 密钥键直接 [redacted]。
func redactedMappingValue(key any, value any, maxLength, maxItems int) any {
	if regexp2Match(secretKeyPattern, strings.TrimSpace(fmt.Sprint(key))) {
		return "[redacted]"
	}
	return redactJSONValue(value, maxLength, maxItems)
}

// sanitizePermissionUIValue 递归 sanitize（含深度/条目/循环引用保护）。
func sanitizePermissionUIValue(value any, depth int, o permissionUIOptions, active map[uintptr]bool) any {
	switch v := value.(type) {
	case nil, bool, int, int64:
		return value
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return PermissionUIUnavailable
		}
		return v
	case string:
		return permissionUIText(v, o.maxStringLen)
	case map[string]any:
		if depth >= o.maxDepth {
			return PermissionUITruncated
		}
		id := mapID(v)
		if active[id] {
			return PermissionUIUnavailable
		}
		active[id] = true
		defer delete(active, id)
		result := map[string]any{}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i >= o.maxItems {
				result[uniquePayloadKey(result, PermissionUITruncated)] = PermissionUITruncated
				break
			}
			normalized, compact := normalizedPermissionUIKey(key)
			if normalized == "secret_context" || compact == "secretcontext" {
				continue
			}
			if isPermissionUISecretKey(normalized, compact) {
				result[uniquePayloadKey(result, key)] = PermissionUIRedacted
				continue
			}
			result[uniquePayloadKey(result, key)] = sanitizePermissionUIValue(v[key], depth+1, o, active)
		}
		return result
	case []any:
		if depth >= o.maxDepth {
			return PermissionUITruncated
		}
		id := sliceID(v)
		if active[id] {
			return PermissionUIUnavailable
		}
		active[id] = true
		defer delete(active, id)
		out := []any{}
		for i, item := range v {
			if i >= o.maxItems {
				out = append(out, PermissionUITruncated)
				break
			}
			out = append(out, sanitizePermissionUIValue(item, depth+1, o, active))
		}
		return out
	default:
		return PermissionUIUnavailable
	}
}

// normalizedPermissionUIKey 归一键名（驼峰拆词 + 非字母数字转下划线）。
func normalizedPermissionUIKey(value string) (string, string) {
	separated := mustReplace(camelCaseBoundaryPattern, value, "_")
	normalized := strings.Trim(mustReplace(nonAlnumKeyPattern, strings.ToLower(separated), "_"), "_")
	return normalized, strings.ReplaceAll(normalized, "_", "")
}

// isPermissionUISecretKey 判定是否为密钥键。
func isPermissionUISecretKey(normalized, compact string) bool {
	for _, token := range strings.Split(normalized, "_") {
		if token != "" && permissionUISecretKeyTokens[token] {
			return true
		}
	}
	if permissionUISecretKeyCompact[compact] {
		return true
	}
	for _, suffix := range permissionUISecretKeySuffixes {
		if strings.HasSuffix(compact, suffix) {
			return true
		}
	}
	return false
}

// permissionUIText 生成权限 UI 可见文本。
func permissionUIText(value string, maxLength int) string {
	var redacted string
	if isAbsoluteURL(value) {
		redacted = RedactURL(value, WithRedactTextMaxLength(max(len(value)+256, maxLength)))
	} else {
		limit := max(len(value)+1, maxLength)
		redacted, _ = redactSecretValuesPreserveFormatE(value, &limit, false)
	}
	redacted = strings.ReplaceAll(redacted, "[redacted]", PermissionUIRedacted)
	marker := " " + PermissionUITruncated
	if len([]rune(redacted)) <= maxLength {
		return redacted
	}
	r := []rune(redacted)
	keep := maxLength - len([]rune(marker))
	if keep < 0 {
		keep = 0
	}
	return string(r[:keep]) + marker
}

// uniquePayloadKey 避免键冲突（Python 用 dict 键存在性）。
func uniquePayloadKey(payload map[string]any, preferred string) string {
	candidate := preferred
	for suffix := 2; ; suffix++ {
		if _, exists := payload[candidate]; !exists {
			return candidate
		}
		candidate = fmt.Sprintf("%s (%d)", preferred, suffix)
	}
}

// boundPermissionUIPayload 按总字节上限二分截断序列化结果。
func boundPermissionUIPayload(value any, maxTotalBytes int) any {
	serialized, err := json.Marshal(value)
	if err != nil {
		return PermissionUIUnavailable
	}
	s := string(serialized)
	if len([]byte(s)) <= maxTotalBytes {
		return value
	}
	low, high := 0, len(s)
	best := map[string]string{"preview": PermissionUITruncated}
	for low <= high {
		mid := (low + high) / 2
		candidate := map[string]string{"preview": s[:mid] + " " + PermissionUITruncated}
		encoded, _ := json.Marshal(candidate)
		if len(encoded) <= maxTotalBytes {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best
}

// buildSafeQuery 脱敏 URL query 值。
func buildSafeQuery(rawQuery string, maxLength int) string {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, key := range keys {
		for _, item := range values[key] {
			if regexp2Match(secretURLQueryKeyPattern, strings.TrimSpace(key)) {
				parts = append(parts, url.QueryEscape(key)+"=[redacted]")
			} else {
				parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(RedactText(item, WithRedactTextMaxLength(maxLength))))
			}
		}
	}
	return strings.Join(parts, "&")
}

// safeURLNetloc 保留 hostname + port，去掉 userinfo。
func safeURLNetloc(parsed *url.URL) string {
	hostname := parsed.Hostname()
	if strings.Contains(hostname, ":") && !strings.HasPrefix(hostname, "[") {
		hostname = "[" + hostname + "]"
	}
	port := parsed.Port()
	if port == "" {
		return hostname
	}
	return hostname + ":" + port
}

// redactURLFragment 脱敏 URL fragment 中的 key=value。
func redactURLFragment(fragment string, maxLength int) string {
	if fragment == "" {
		return ""
	}
	if !strings.Contains(fragment, "=") {
		return RedactText(fragment, WithRedactTextMaxLength(maxLength))
	}
	parts := strings.Split(fragment, "&")
	out := []string{}
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		key := kv[0]
		val := ""
		if len(kv) == 2 {
			val = kv[1]
		}
		if regexp2Match(secretURLQueryKeyPattern, strings.TrimSpace(key)) {
			out = append(out, key+"=[redacted]")
		} else {
			out = append(out, key+"="+url.QueryEscape(RedactText(val, WithRedactTextMaxLength(maxLength))))
		}
	}
	if len(out) == 0 {
		return RedactText(fragment, WithRedactTextMaxLength(maxLength))
	}
	return strings.Join(out, "&")
}

// truncateText 超长截断（Python _truncate_text）。
func truncateText(value string, maxLength int) string {
	r := []rune(value)
	if len(r) <= maxLength {
		return value
	}
	if maxLength <= 3 {
		return string(r[:maxLength])
	}
	return string(r[:maxLength-3]) + "..."
}

// stringify Python _stringify：Mapping → json.dumps(sort_keys=True)，其余 str(value or "")。
func stringify(value any) string {
	if m, ok := value.(map[string]any); ok {
		encoded, err := json.Marshal(m)
		if err == nil {
			return string(encoded)
		}
		return fmt.Sprint(value)
	}
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// isAbsoluteURL 是否绝对 URI。
func isAbsoluteURL(value string) bool {
	matched, _ := absoluteURIPrefixPattern.MatchString(strings.TrimSpace(value))
	return matched
}

// containsControlChar 是否含 Unicode 类别以 C 开头的字符。
func containsControlChar(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.C, r) {
			return true
		}
	}
	return false
}

// expandHome 展开 ~ 为用户主目录（Python Path.expanduser）。
func expandHome(value string) string {
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(value, "~"), string(os.PathSeparator)))
		}
	}
	return value
}

// resolvePathStrictFalse Path.resolve(strict=False) 等价：Abs + Clean（不解析 symlink 失败即原样）。
func resolvePathStrictFalse(value string) string {
	abs, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return filepath.Clean(abs)
}

// relativeToPosix 判断 target 是否在 root 下，返回 posix 相对路径。
func relativeToPosix(target, root string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// ──────────────────────────── regexp2 辅助 ────────────────────────────

// mustReplace 用 regexp2 做全量替换，出错时返回原串。
func mustReplace(re *regexp2.Regexp, input, replacement string) string {
	out, err := re.Replace(input, replacement, -1, -1)
	if err != nil {
		return input
	}
	return out
}

// mustReplaceFunc 用 regexp2 做函数式全量替换。
func mustReplaceFunc(re *regexp2.Regexp, input string, fn func(regexp2.Match) string) string {
	out, err := re.ReplaceFunc(input, fn, -1, -1)
	if err != nil {
		return input
	}
	return out
}

// regexp2Match 是否匹配（任意位置）。
func regexp2Match(re *regexp2.Regexp, value string) bool {
	ok, err := re.MatchString(value)
	return err == nil && ok
}

// regexp2FullMatch 是否完整匹配（Python fullmatch）。
func regexp2FullMatch(re *regexp2.Regexp, value string) bool {
	m, err := re.FindStringMatch(value)
	if err != nil || m == nil {
		return false
	}
	return m.Index == 0 && m.Length == len(value)
}

// mapID 取 map 的标识（替代 Python id()）。
func mapID(m map[string]any) uintptr {
	return reflect.ValueOf(m).Pointer()
}

// sliceID 取 slice 的标识。
func sliceID(s []any) uintptr {
	return reflect.ValueOf(s).Pointer()
}
