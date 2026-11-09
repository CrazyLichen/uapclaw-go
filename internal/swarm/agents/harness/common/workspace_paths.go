// 共享工作区路径解析与显示脱敏助手。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/workspace_paths.py（518 行 / 22 符号）
//
// 作用：把模型可见路径解析为规范化绝对路径，并在 UI / reviewer / 模型可见的
// 工具消息里脱敏沙箱产物路径与绝对路径前缀。
//
// 依赖：review_ui 模式末尾调用 permissions.RedactSecretValuesPreserveFormat
// （reviewer_redaction.go，原属 9.90d-2，本批用户拍板提前落地）。
//
// 正则说明：Python re 的 lookbehind / lookahead 在 RE2 不支持，统一用
// 项目已引入的 github.com/dlclark/regexp2。

package common

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dlclark/regexp2"
	"golang.org/x/text/unicode/norm"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkspacePathResolution 工作区范围路径检查的规范化解析结果。
//
// Python: @dataclass(frozen=True) WorkspacePathResolution
type WorkspacePathResolution struct {
	// OriginalText 归一化后的原始文本
	OriginalText string
	// CanonicalPath 规范化绝对路径，nil 表示无法解析
	CanonicalPath *string
	// DisplayPath 展示路径
	DisplayPath string
	// Relation 与当前工作区的关系（workspace / external / unresolved）
	Relation string
	// RejectedReason 拒绝原因
	RejectedReason string
}

// resolveWorkspacePathOptions resolve_workspace_path 的选项。
type resolveWorkspacePathOptions struct {
	resolveRelative    bool
	rejectStaleSandbox bool
}

// ResolveWorkspacePathOption resolve_workspace_path 的选项函数。
type ResolveWorkspacePathOption func(*resolveWorkspacePathOptions)

// sanitizeVisibleOptions sanitize_visible_text 的选项。
type sanitizeVisibleOptions struct {
	mode      string
	maxLength int
}

// SanitizeVisibleOption sanitize_visible_text 的选项函数。
type SanitizeVisibleOption func(*sanitizeVisibleOptions)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// bt 反引号字符（Go raw string 无法直接内嵌，正则字符集里需拼接）。
const bt = "\x60"

const (
	// WorkspaceCurrentURI 当前工作区 URI 标识
	WorkspaceCurrentURI = "workspace://current"
	// WorkspaceCurrentURIPrefix 当前工作区 URI 前缀
	WorkspaceCurrentURIPrefix = WorkspaceCurrentURI + "/"
	// StaleSandboxArtifactPath 陈旧沙箱产物路径的脱敏替换串
	StaleSandboxArtifactPath = "[stale sandbox artifact path redacted]"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// pathKeys 路径类参数键集合（Python _PATH_KEYS frozenset）
	pathKeys = map[string]bool{
		"abs_file_path": true, "abs_file_path_list": true, "abs_file_paths": true,
		"cwd": true, "destination": true, "directory": true, "file": true,
		"file_path": true, "file_path_list": true, "file_paths": true,
		"filepath": true, "filename": true, "output": true, "output_path": true,
		"path": true, "source": true, "source_path": true, "src": true,
		"target_path": true, "workdir": true,
	}

	// sandboxArtifactPattern 匹配 .sandbox-artifacts/ 路径
	sandboxArtifactPattern = regexp2.MustCompile(
		`(?<![\w:/\\])`+
			`(?:`+
			`[A-Za-z]:[\\/](?:(?!\s(?:[\\/]|~[\\/]))[^'"<>)\],;:\r\n])*(?<=[\\/])`+
			`|(?:~/?|/)[^'"<>)\],;:\s\r\n]*`+
			`)`+
			`\.sandbox-artifacts[\\/][^'"<>)\],;:\r\n]+`,
		regexp2.None,
	)

	// reviewUIPosixAbsolutePathPattern review_ui 的 POSIX 绝对路径
	reviewUIPosixAbsolutePathPattern = regexp2.MustCompile(
		`(?<![\w:/*])/(?!/)[^'"<>)\],;:\s\r\n]+(?:/[^'"<>)\],;:\s\r\n]+)*`,
		regexp2.None,
	)
	// reviewUIHomePathPattern review_ui 的 ~ 家目录路径
	reviewUIHomePathPattern = regexp2.MustCompile(
		`(?<![\w])~(?:/[^'"<>)\],;:\s\r\n]+)+`,
		regexp2.None,
	)
	// reviewUIWindowsAbsolutePathPattern review_ui 的 Windows 绝对路径
	reviewUIWindowsAbsolutePathPattern = regexp2.MustCompile(
		`\b[A-Za-z]:[\\/][^'";|&<>`+bt+`,，。；、\r\n\s]+`,
		regexp2.None,
	)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithResolveRelative 设置是否解析相对路径（默认 true）。
func WithResolveRelative(v bool) ResolveWorkspacePathOption {
	return func(o *resolveWorkspacePathOptions) { o.resolveRelative = v }
}

// WithRejectStaleSandboxPath 设置是否拒绝陈旧沙箱路径（默认 true）。
func WithRejectStaleSandboxPath(v bool) ResolveWorkspacePathOption {
	return func(o *resolveWorkspacePathOptions) { o.rejectStaleSandbox = v }
}

// WithSanitizeMode 设置脱敏模式（workspace_visible / review_ui）。
func WithSanitizeMode(mode string) SanitizeVisibleOption {
	return func(o *sanitizeVisibleOptions) { o.mode = mode }
}

// WithSanitizeMaxLength 设置脱敏文本长度上限。
func WithSanitizeMaxLength(n int) SanitizeVisibleOption {
	return func(o *sanitizeVisibleOptions) { o.maxLength = n }
}

// NormalizeWorkspaceRoot 规范化工作区根；nil 返回 nil。
//
// Python: normalize_workspace_root(workspace_root)
func NormalizeWorkspaceRoot(workspaceRoot any) *string {
	if workspaceRoot == nil {
		return nil
	}
	// 允许传 *string（本包内部把已规范化的根继续往下传），
	// 对齐 Python 里 Path 对象可直接作为 workspace_root 参数的多态性。
	text := ""
	switch v := workspaceRoot.(type) {
	case *string:
		if v == nil {
			return nil
		}
		text = *v
	case string:
		text = v
	default:
		text = fmt.Sprint(v)
	}
	s := cleanAbs(text)
	return &s
}

// SandboxArtifactSessionRoot 返回路径中 .sandbox-artifacts/<session> 会话根。
//
// Python: sandbox_artifact_session_root(path) -> Path | None
func SandboxArtifactSessionRoot(path any) *string {
	normalized := cleanAbs(anyToString(path))
	parts := strings.Split(normalized, "/")
	for i, part := range parts {
		if part == ".sandbox-artifacts" && i+1 < len(parts) {
			joined := strings.Join(parts[:i+2], "/")
			return &joined
		}
	}
	return nil
}

// IsSandboxArtifactWorkspaceRoot 判定路径本身是否就是一个沙箱会话根。
//
// Python: is_sandbox_artifact_workspace_root(path)
func IsSandboxArtifactWorkspaceRoot(path any) bool {
	normalized := cleanAbs(anyToString(path))
	session := SandboxArtifactSessionRoot(normalized)
	return session != nil && *session == normalized
}

// ResolveWorkspacePath 把模型可见路径解析为规范化路径。
//
// Python: resolve_workspace_path(raw_path, workspace_root, *,
//
//	resolve_relative=True, reject_stale_sandbox_path=True)
//
// workspace://current/... 与相对路径受当前工作区根约束；绝对路径保留，
// 但已知当前工作区根时会拒绝遗留沙箱产物根。
func ResolveWorkspacePath(rawPath any, workspaceRoot any, opts ...ResolveWorkspacePathOption) *string {
	o := resolveWorkspacePathOptions{resolveRelative: true, rejectStaleSandbox: true}
	for _, opt := range opts {
		opt(&o)
	}
	pathText := normalizePathText(rawPath)
	if pathText == "" {
		return nil
	}
	root := NormalizeWorkspaceRoot(workspaceRoot)

	if pathText == WorkspaceCurrentURI {
		pathText = WorkspaceCurrentURIPrefix
	}
	if strings.HasPrefix(pathText, WorkspaceCurrentURIPrefix) {
		if root == nil {
			return nil
		}
		relativeText := strings.TrimPrefix(pathText, WorkspaceCurrentURIPrefix)
		candidate := cleanAbs(filepath.Join(*root, relativeText))
		if isRelativeToPath(candidate, *root) {
			return &candidate
		}
		return nil
	}

	rawCandidate := expandHome(pathText)
	if !filepath.IsAbs(rawCandidate) {
		if root == nil || !o.resolveRelative {
			resolved := cleanAbs(rawCandidate)
			return &resolved
		}
		candidate := cleanAbs(filepath.Join(*root, rawCandidate))
		if isRelativeToPath(candidate, *root) {
			return &candidate
		}
		return nil
	}

	resolved := cleanAbs(rawCandidate)
	if o.rejectStaleSandbox && root != nil {
		currentSessionRoot := SandboxArtifactSessionRoot(*root)
		candidateSessionRoot := SandboxArtifactSessionRoot(resolved)
		if candidateSessionRoot != nil && currentSessionRoot == nil {
			return nil
		}
		if candidateSessionRoot != nil && currentSessionRoot != nil &&
			*candidateSessionRoot != *currentSessionRoot {
			return nil
		}
	}
	return &resolved
}

// ResolveWorkspacePathDetails 解析路径并描述其与当前工作区的关系。
//
// Python: resolve_workspace_path_details(raw_path, workspace_root, *, resolve_relative=True)
func ResolveWorkspacePathDetails(rawPath any, workspaceRoot any, opts ...ResolveWorkspacePathOption) WorkspacePathResolution {
	originalText := normalizePathText(rawPath)
	root := NormalizeWorkspaceRoot(workspaceRoot)

	// Python 传 reject_stale_sandbox_path=False；root 是 Path 对象，
	// Go 侧需解引用 *string，否则 fmt.Sprint 会打出指针地址。
	var rootArg any
	if root != nil {
		rootArg = *root
	}
	allOpts := append([]ResolveWorkspacePathOption{WithRejectStaleSandboxPath(false)}, opts...)
	resolved := ResolveWorkspacePath(rawPath, rootArg, allOpts...)

	if resolved == nil {
		return WorkspacePathResolution{
			OriginalText:   originalText,
			CanonicalPath:  nil,
			DisplayPath:    originalText,
			Relation:       "unresolved",
			RejectedReason: "workspace_path_unresolved",
		}
	}
	relation := "external"
	if root != nil && isRelativeToPath(*resolved, *root) {
		relation = "workspace"
	}
	return WorkspacePathResolution{
		OriginalText:  originalText,
		CanonicalPath: resolved,
		DisplayPath:   DisplayWorkspacePath(*resolved, root),
		Relation:      relation,
	}
}

// DisplayWorkspacePath 返回稳定的模型/UI 可见路径。
//
// Python: display_workspace_path(raw_path, workspace_root)
// 当前工作区路径不再改写为 workspace://current；URI 形式仍作为输入兼容。
func DisplayWorkspacePath(rawPath any, workspaceRoot any) string {
	pathText := normalizePathText(rawPath)
	if pathText == "" {
		return pathText
	}
	root := NormalizeWorkspaceRoot(workspaceRoot)
	if pathText == WorkspaceCurrentURI {
		if root != nil {
			return *root
		}
		return WorkspaceCurrentURIPrefix
	}
	if strings.HasPrefix(pathText, WorkspaceCurrentURIPrefix) {
		resolved := ResolveWorkspacePath(pathText, root, WithRejectStaleSandboxPath(false))
		if resolved != nil {
			return *resolved
		}
		return pathText
	}

	if root == nil {
		if SandboxArtifactSessionRoot(pathText) != nil {
			return StaleSandboxArtifactPath
		}
		return pathText
	}

	path := cleanAbs(pathText)
	if !isRelativeToPath(path, *root) {
		if SandboxArtifactSessionRoot(path) != nil {
			return StaleSandboxArtifactPath
		}
		return pathText
	}
	if path == *root {
		return *root
	}
	return path
}

// SanitizeVisibleText 脱敏发往 UI/reviewer/模型可见工具消息的文本。
//
// Python: sanitize_visible_text(value, workspace_root, *, mode, max_length=60000)
func SanitizeVisibleText(value string, workspaceRoot any, opts ...SanitizeVisibleOption) string {
	if value == "" {
		return value
	}
	o := sanitizeVisibleOptions{mode: "workspace_visible", maxLength: 60000}
	for _, opt := range opts {
		opt(&o)
	}
	if o.mode == "review_ui" {
		return sanitizeReviewUIText(value, workspaceRoot, o.maxLength)
	}
	return regexp2ReplaceFunc(sandboxArtifactPattern, value, func(m regexp2.Match) string {
		return sandboxArtifactReplacement(m.String())
	})
}

// SanitizeVisibleValue 递归脱敏任意值（str / Mapping / list / tuple）。
//
// Python: sanitize_visible_value(value, workspace_root, *, mode, max_length)
func SanitizeVisibleValue(value any, workspaceRoot any, opts ...SanitizeVisibleOption) any {
	switch v := value.(type) {
	case string:
		return SanitizeVisibleText(v, workspaceRoot, opts...)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			safeKey := SanitizeVisibleText(key, workspaceRoot, opts...)
			out[safeKey] = SanitizeVisibleValue(item, workspaceRoot, opts...)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = SanitizeVisibleValue(item, workspaceRoot, opts...)
		}
		return out
	default:
		return value
	}
}

// SanitizeReviewUIValue review_ui 模式的便捷入口。
//
// Python: sanitize_review_ui_value(value, workspace_root, *, max_length=60000)
func SanitizeReviewUIValue(value any, workspaceRoot any, opts ...SanitizeVisibleOption) any {
	all := append([]SanitizeVisibleOption{WithSanitizeMode("review_ui")}, opts...)
	return SanitizeVisibleValue(value, workspaceRoot, all...)
}

// ResolveWorkspacePathArguments 解析显式 opt-in 工具的路径类参数字段。
//
// Python: resolve_workspace_path_arguments(value, workspace_root)
func ResolveWorkspacePathArguments(value any, workspaceRoot any) any {
	root := NormalizeWorkspaceRoot(workspaceRoot)
	if root == nil {
		return value
	}
	return resolveWorkspacePathArguments(value, *root, "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveWorkspacePathArguments 递归实现（携带 currentKey）。
//
// Python: _resolve_workspace_path_arguments(value, workspace_root, *, current_key)
func resolveWorkspacePathArguments(value any, workspaceRoot string, currentKey string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = resolveWorkspacePathArguments(item, workspaceRoot, strings.ToLower(fmt.Sprint(key)))
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = resolveWorkspacePathArguments(item, workspaceRoot, currentKey)
		}
		return out
	case string:
		if pathKeys[currentKey] {
			resolved := ResolveWorkspacePath(v, workspaceRoot)
			if resolved == nil {
				panic(fmt.Errorf("Path is outside current workspace or stale: %s", v))
			}
			return *resolved
		}
		return v
	default:
		return value
	}
}

// normalizePathText 归一化路径文本：NFKC + strip + unquote + 反斜杠转正斜杠。
//
// Python: _normalize_path_text(raw_path)
func normalizePathText(rawPath any) string {
	text := strings.TrimSpace(anyToString(rawPath))
	text = norm.NFKC.String(text)
	if decoded, err := url.QueryUnescape(text); err == nil {
		text = decoded
	}
	return strings.ReplaceAll(text, "\\", "/")
}

// isRelativeToPath path.relative_to(root) 等价判定。
//
// Python: _is_relative_to(path, root)
func isRelativeToPath(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

// sandboxArtifactReplacement workspace://current 开头的匹配原样保留。
//
// Python: _sandbox_artifact_replacement(matched)
func sandboxArtifactReplacement(matched string) string {
	if strings.HasPrefix(matched, WorkspaceCurrentURI) {
		return matched
	}
	return StaleSandboxArtifactPath
}

// sanitizeReviewUIText review_ui 模式的文本脱敏主流程。
//
// Python: _sanitize_review_ui_text(value, workspace_root, *, max_length)
func sanitizeReviewUIText(value string, workspaceRoot any, maxLength int) string {
	sanitized := relativizeWorkspaceRootPrefixes(value, workspaceRoot)
	sanitized = regexp2ReplaceFunc(reviewUIHomePathPattern, sanitized, func(m regexp2.Match) string {
		return reviewUIPathReplacement(m.String(), workspaceRoot)
	})
	sanitized = regexp2ReplaceFunc(reviewUIWindowsAbsolutePathPattern, sanitized, func(m regexp2.Match) string {
		return reviewUIPathReplacement(m.String(), workspaceRoot)
	})
	sanitized = regexp2ReplaceFunc(reviewUIPosixAbsolutePathPattern, sanitized, func(m regexp2.Match) string {
		return reviewUIPathReplacement(m.String(), workspaceRoot)
	})
	// Python 末尾：_redact_review_ui_secrets(value, max_length=max_length)
	return redactReviewUISecrets(sanitized, maxLength)
}

// relativizeWorkspaceRootPrefixes 把当前工作区根前缀相对化。
//
// Python: _relativize_workspace_root_prefixes(value, workspace_root)
// 注意：Python 用有界 token 扫描而非嵌套正则，避免 agent 可控 payload
// 让 review 渲染产生无界回溯。Go 侧同样用顺序扫描实现。
func relativizeWorkspaceRootPrefixes(value string, workspaceRoot any) string {
	root := NormalizeWorkspaceRoot(workspaceRoot)
	if root == nil {
		return value
	}
	rootForms := map[string]bool{*root: true}
	home, err := os.UserHomeDir()
	if err == nil {
		homeClean := cleanAbs(home)
		if rel, ok := isRelativeToPathReturn(*root, homeClean); ok {
			rootForms["~/"+rel] = true
		}
	}
	forms := make([]string, 0, len(rootForms))
	for f := range rootForms {
		if f != "" {
			forms = append(forms, f)
		}
	}
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })

	sanitized := value
	for _, rootForm := range forms {
		traversalStarts := workspaceRootTraversalStarts(sanitized, rootForm)
		// 第一次：把 "rootForm/" 前缀去掉（除非该路径含 .. 段）
		sanitized = replaceRootPrefix(sanitized, rootForm, traversalStarts)
		// 第二次：单独出现的 rootForm 替换为 "."
		sanitized = replaceBareRoot(sanitized, rootForm)
	}
	return sanitized
}

// workspaceRootTraversalStarts 返回路径含 .. 的当前工作区根偏移集合。
//
// Python: _workspace_root_traversal_starts(value, root_form)
func workspaceRootTraversalStarts(value, rootForm string) map[int]bool {
	starts := map[int]bool{}
	searchFrom := 0
	for {
		start := strings.Index(value[min(searchFrom, len(value)):], rootForm)
		if start < 0 {
			return starts
		}
		start += searchFrom
		searchFrom = start + 1
		if start > 0 && isLocalPathPrefixCharacter(rune(value[start-1])) {
			continue
		}
		if pathFromRootHasParentSegment(value, start+len(rootForm)) {
			starts[start] = true
		}
	}
}

// pathFromRootHasParentSegment 扫描根后以斜杠分隔的段，不做回溯。
//
// Python: _path_from_root_has_parent_segment(value, root_end)
func pathFromRootHasParentSegment(value string, rootEnd int) bool {
	position := rootEnd
	valueLength := len(value)
	for position < valueLength && (value[position] == '/' || value[position] == '\\') {
		position++
		segmentStart := position
		for position < valueLength {
			current := value[position]
			if current == '/' || current == '\\' {
				break
			}
			if strings.ContainsRune(`'";|&<>`+bt+`,，。；、`+"\r\n", rune(current)) {
				return false
			}
			position++
		}
		if value[segmentStart:position] == ".." {
			return true
		}
	}
	return false
}

// isLocalPathPrefixCharacter 判定是否为本地路径前缀字符。
//
// Python: _is_local_path_prefix_character(character)
func isLocalPathPrefixCharacter(character rune) bool {
	return (character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9') ||
		character == '_' || strings.ContainsRune("./:-", character)
}

// reviewUIPathReplacement 把工作区内的绝对路径替换为相对路径。
//
// Python: _review_ui_path_replacement(matched, workspace_root)
func reviewUIPathReplacement(matched string, workspaceRoot any) string {
	if matched == "" {
		return matched
	}
	root := NormalizeWorkspaceRoot(workspaceRoot)
	path := cleanAbs(matched)
	if root == nil {
		return matched
	}
	rel, ok := isRelativeToPathReturn(path, *root)
	if !ok {
		return matched
	}
	if rel == "" || rel == "." {
		return "."
	}
	return rel
}

// redactReviewUISecrets 末尾密钥脱敏（直接复用 permissions 包）。
//
// Python: _redact_review_ui_secrets(value, *, max_length)
func redactReviewUISecrets(value string, maxLength int) string {
	return permissions.RedactSecretValuesPreserveFormat(value, maxLength)
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// anyToString 把 string / *string / 其他值转为文本。
func anyToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case *string:
		if v == nil {
			return ""
		}
		return *v
	default:
		return fmt.Sprint(v)
	}
}

// cleanAbs Path.expanduser().resolve(strict=False) 等价：~ 展开 + Abs + Clean。
func cleanAbs(value string) string {
	if value == "" {
		return ""
	}
	return filepath.Clean(filepath.ToSlash(resolveAbs(expandHome(value))))
}

// expandHome 展开 ~ 前缀。
func expandHome(value string) string {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(value, "~"))
		}
	}
	return value
}

// resolveAbs filepath.Abs 的容错包装。
func resolveAbs(value string) string {
	abs, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return abs
}

// isRelativeToPathReturn 返回相对路径形式。
func isRelativeToPathReturn(path, root string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// replaceRootPrefix 去掉 rootForm/ 前缀（等价 Python 第一个 re.sub）。
func replaceRootPrefix(value, rootForm string, traversalStarts map[int]bool) string {
	if rootForm == "" {
		return value
	}
	var b strings.Builder
	pos := 0
	for {
		idx := strings.Index(value[pos:], rootForm)
		if idx < 0 {
			b.WriteString(value[pos:])
			break
		}
		start := pos + idx
		// (?<![\w./:-]) 前置断言
		if start > 0 && isLocalPathPrefixCharacter(rune(value[start-1])) {
			b.WriteString(value[pos : start+len(rootForm)])
			pos = start + len(rootForm)
			continue
		}
		end := start + len(rootForm)
		if end >= len(value) || (value[end] != '/' && value[end] != '\\') {
			b.WriteString(value[pos : start+len(rootForm)])
			pos = start + len(rootForm)
			continue
		}
		// (?![\\/]\.\.(?:[\\/]|$))：不做该检查，用 traversalStarts 判定
		if traversalStarts[start] {
			b.WriteString(value[pos : end+1])
			pos = end + 1
			continue
		}
		b.WriteString(value[pos:start])
		pos = end + 1
	}
	return b.String()
}

// replaceBareRoot 单独出现的 rootForm 替换为 "."（等价 Python 第二个 re.sub）。
func replaceBareRoot(value, rootForm string) string {
	if rootForm == "" {
		return value
	}
	var b strings.Builder
	pos := 0
	for {
		idx := strings.Index(value[pos:], rootForm)
		if idx < 0 {
			b.WriteString(value[pos:])
			break
		}
		start := pos + idx
		if start > 0 && isLocalPathPrefixCharacter(rune(value[start-1])) {
			b.WriteString(value[pos : start+len(rootForm)])
			pos = start + len(rootForm)
			continue
		}
		end := start + len(rootForm)
		followOK := end == len(value) ||
			strings.ContainsRune(" \t\r\n'\"<>)] ,;:", rune(value[end]))
		if followOK {
			b.WriteString(value[pos:start])
			b.WriteString(".")
			pos = end
			continue
		}
		b.WriteString(value[pos : start+len(rootForm)])
		pos = end
	}
	return b.String()
}

// regexp2ReplaceFunc regexp2 函数式替换，出错返回原串。
func regexp2ReplaceFunc(re *regexp2.Regexp, input string, fn func(regexp2.Match) string) string {
	out, err := re.ReplaceFunc(input, fn, -1, -1)
	if err != nil {
		return input
	}
	return out
}
