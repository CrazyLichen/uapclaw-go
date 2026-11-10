// 只读网络与浏览器动作共享的 URL 审查助手。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/url_safety.py（541 行）
//
// 职责：从不可信参数里提取绝对 URI 事实、判定 URL 是否可交给 AutoReviewer
// 自动审查（vs 必须硬拦或走语义审查）。
//
// 正则说明：Python re 的 lookbehind 在 Go RE2 不支持，本文件沿用 9.90d-1
// Task 5 的 regexp2 方案（项目已引入），保持与本批其他文件一致。
//
// 端口语义差异：Python urlparse().port 对超范围端口抛 ValueError；
// Go url.Parse 不报错且 url.URL.Port() 返回原始字符串。故 hasInvalidPort
// 自行校验 0-65535 范围。

package permissions

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dlclark/regexp2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbsoluteURIFacts URL 安全模块持有的有界 URI 事实。
//
// Python: @dataclass(frozen=True, slots=True) AbsoluteUriFacts
type AbsoluteURIFacts struct {
	// URI 原始 URI 文本
	URI string
	// Scheme 小写 scheme
	Scheme string
	// Host 归一化主机
	Host string
	// InvalidPort 端口是否非法
	InvalidPort bool
	// UserinfoPresent 是否含 userinfo
	UserinfoPresent bool
	// SecretQueryPresent query 是否含密钥
	SecretQueryPresent bool
	// NetworkLike 是否网络类 URI
	NetworkLike bool
	// ParseFailureReason 解析失败原因
	ParseFailureReason string
	// FileLocal 是否本地 file URI
	FileLocal bool
	// FilePath 解析后的文件路径
	FilePath string
	// FileResolutionReason 文件解析失败原因
	FileResolutionReason string
}

// NetworkScopeFacts 当前硬守卫或视图消费的网络值。
//
// Python: @dataclass(frozen=True, slots=True) NetworkScopeFacts
type NetworkScopeFacts struct {
	// AbsoluteURIs 绝对 URI 事实
	AbsoluteURIs []AbsoluteURIFacts
	// URLs http/https URL 列表
	URLs []string
	// Hosts 主机列表
	Hosts []string
	// Schemes scheme 列表
	Schemes []string
	// UploadLike 是否像上传
	UploadLike bool
}

// RecentURLSource 来自先前发现工具结果的低信任 URL 来源。
//
// Python: @dataclass(frozen=True) RecentUrlSource
type RecentURLSource struct {
	// URL 归一化 URL
	URL string
	// Host 主机
	Host string
	// SourceTool 来源工具
	SourceTool string
	// SourceKind 来源类别
	SourceKind string
	// Trusted 是否可信
	Trusted bool
}

// ReviewableURLDecision 共享 URL 可审查性检查的决策。
//
// Python: @dataclass(frozen=True) ReviewableUrlDecision
type ReviewableURLDecision struct {
	// Accepted 是否接受
	Accepted bool
	// Kind 决策类别
	Kind string
	// Reason 原因
	Reason string
	// EvidenceSummary 证据摘要
	EvidenceSummary map[string]any
	// HardBlock 是否硬拦
	HardBlock bool
	// SemanticReviewRequired 是否需要语义审查
	SemanticReviewRequired bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TrailingURLPunctuation URL 尾部需剥离的标点
	TrailingURLPunctuation = ".,;:!?)]}'\""

	// urlSafetyKindReadonlyFetch 只读网络抓取类别
	urlSafetyKindReadonlyFetch = "readonly_network_fetch"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// URLPattern 匹配 http(s) URL
	URLPattern = regexp2.MustCompile(`https?://[^\s"'<>]+`, regexp2.IgnoreCase)
	// DomainPattern 匹配域名（lookbehind 排除相邻标识符字符）
	DomainPattern = regexp2.MustCompile(
		`(?<![A-Za-z0-9.-])(?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,}(?![A-Za-z0-9.-])`,
		regexp2.IgnoreCase,
	)
	// AbsoluteURIPattern 匹配任意 scheme:// 绝对 URI
	AbsoluteURIPattern = regexp2.MustCompile(
		`(?<![A-Za-z0-9+.-])[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]*`,
		regexp2.None,
	)

	// UploadKeys 上传类参数键集合
	UploadKeys = map[string]bool{
		"attachment": true, "attachments": true, "file": true,
		"file_path": true, "files": true, "upload": true,
	}
	// SafeURLSchemes 可自动审查的 scheme 集合
	SafeURLSchemes = map[string]bool{"https": true}
	// InternalHostSuffixes 内部主机后缀
	InternalHostSuffixes = []string{".internal", ".local", ".lan", ".home.arpa"}
	// MetadataHosts 云元数据主机集合
	MetadataHosts = map[string]bool{
		"169.254.169.254": true, "metadata": true, "metadata.google.internal": true,
	}
	// HardBlockReasons 不可覆盖的硬拦原因集合
	HardBlockReasons = map[string]bool{
		"network_host_missing":       true,
		"network_host_not_public":    true,
		"network_internal_hostname":  true,
		"network_metadata_host":      true,
		"network_public_suffix_host": true,
		"network_scheme_not_https":   true,
		"network_secret_query":       true,
		"network_single_label_host":  true,
		"network_url_invalid_port":   true,
		"network_url_userinfo":       true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReviewableURLDecision 构造决策并校验语义审查与拒绝的互斥性。
//
// Python: ReviewableUrlDecision.__post_init__
// semantic_review_required 为真时不得 accepted=false 或 hard_block=true。
func NewReviewableURLDecision(
	accepted bool,
	kind, reason string,
	evidenceSummary map[string]any,
	hardBlock, semanticReviewRequired bool,
) ReviewableURLDecision {
	if semanticReviewRequired && (!accepted || hardBlock) {
		panic(fmt.Errorf("semantic review cannot override URL safety rejection"))
	}
	if evidenceSummary == nil {
		evidenceSummary = map[string]any{}
	}
	return ReviewableURLDecision{
		Accepted:               accepted,
		Kind:                   kind,
		Reason:                 reason,
		EvidenceSummary:        evidenceSummary,
		HardBlock:              hardBlock,
		SemanticReviewRequired: semanticReviewRequired,
	}
}

// ToJSONDict 返回 reviewer 证据用的 JSON-safe 表示。
//
// Python: RecentUrlSource.to_json_dict()
func (s RecentURLSource) ToJSONDict() map[string]any {
	return map[string]any{
		"host":        s.Host,
		"source_kind": s.SourceKind,
		"source_tool": s.SourceTool,
		"trusted":     s.Trusted,
		"url":         s.URL,
	}
}

// InspectNetworkScope 检查有界 URL 事实，不写入 action carrier。
//
// Python: inspect_network_scope(subject)
func InspectNetworkScope(subject ToolDecisionFacts) NetworkScopeFacts {
	uris := InspectAbsoluteUris(subject.UntrustedArgs, subject.WorkspaceRoot)
	urls := []string{}
	seenURL := map[string]bool{}
	hosts := []string{}
	seenHost := map[string]bool{}
	schemes := []string{}
	seenScheme := map[string]bool{}
	for _, uri := range uris {
		if (uri.Scheme == "http" || uri.Scheme == "https") && !seenURL[uri.URI] {
			seenURL[uri.URI] = true
			urls = append(urls, uri.URI)
		}
		if uri.Host != "" && !seenHost[uri.Host] {
			seenHost[uri.Host] = true
			hosts = append(hosts, uri.Host)
		}
		if uri.Scheme != "" && !seenScheme[uri.Scheme] {
			seenScheme[uri.Scheme] = true
			schemes = append(schemes, uri.Scheme)
		}
	}
	return NetworkScopeFacts{
		AbsoluteURIs: uris,
		URLs:         urls,
		Hosts:        hosts,
		Schemes:      schemes,
		UploadLike:   containsUpload(subject.UntrustedArgs),
	}
}

// InspectAbsoluteUris 返回当前参数里嵌入的每个不同绝对 URI。
//
// Python: inspect_absolute_uris(value, workspace_root=None)
func InspectAbsoluteUris(value any, workspaceRoot any) []AbsoluteURIFacts {
	root := safeRoot(workspaceRoot)
	found := []AbsoluteURIFacts{}
	seen := map[string]bool{}
	for _, text := range stringValues(value) {
		for _, uri := range regexp2FindAll(AbsoluteURIPattern, text) {
			if seen[uri] {
				continue
			}
			seen[uri] = true
			found = append(found, inspectURI(uri, root))
		}
	}
	return found
}

// EvaluateReviewableURLScope 判定单个公开 HTTPS URL 是否可交给 AutoReviewer。
//
// Python: evaluate_reviewable_url_scope(subject, *, evidence, recent_url_sources=())
func EvaluateReviewableURLScope(
	subject ToolDecisionFacts,
	evidence *OriginalUserIntentEvidence,
	recentURLSources []RecentURLSource,
) ReviewableURLDecision {
	sideEffects, urls, _, _, uploadLike := networkFacts(subject)
	summary := baseSummary(subject)

	if _, hasExternalSend := sideEffects["external_send"]; hasExternalSend {
		return rejectDecision("network_external_send", summary, false)
	}
	if uploadLike {
		return rejectDecision("network_upload_like", summary, false)
	}
	if len(urls) != 1 {
		reason := "network_url_count_not_one"
		if len(urls) == 0 {
			reason = "network_url_missing"
		}
		return rejectDecision(reason, summary, false)
	}
	urlText := urls[0]
	parsed, parseErr := url.Parse(strings.TrimSpace(urlText))
	if parseErr != nil {
		return rejectDecision("network_url_invalid_port", summary, true)
	}
	if unsafeReason := UnsafePublicHTTPSURLReason(parsed); unsafeReason != "" {
		return rejectDecision(unsafeReason, summary, HardBlockReasons[unsafeReason])
	}
	normalizedURL := NormalizeURLForMatch(urlText)
	host := NormalizeNetworkHost(parsed.Hostname())
	if !hasTrustedHostUserIntent(evidence) {
		enriched := cloneSummary(summary)
		enriched["host"] = host
		enriched["registrable_domain"] = RegistrableDomain(host)
		return rejectDecision("original_user_intent_missing", enriched, false)
	}
	sourceKind := resolveURLSourceKind(normalizedURL, host, evidence, recentURLSources)
	enrichedSummary := cloneSummary(summary)
	enrichedSummary["host"] = host
	enrichedSummary["query_safety"] = "no_secret_query"
	enrichedSummary["registrable_domain"] = RegistrableDomain(host)
	enrichedSummary["source_kind"] = sourceKind

	if HasValidAskUserClarification(evidence) && sourceKind != "recent_search_result" {
		return semanticUnresolved(enrichedSummary)
	}
	if sourceKind != "" {
		return NewReviewableURLDecision(true, urlSafetyKindReadonlyFetch,
			"readonly_network_url_reviewable", enrichedSummary, false, false)
	}
	return semanticUnresolved(enrichedSummary)
}

// UnsafePublicHTTPSURLReason 返回 URL 不可自动审查的原因；空串表示合格。
//
// Python: unsafe_public_https_url_reason(parsed)
func UnsafePublicHTTPSURLReason(parsed *url.URL) string {
	if !SafeURLSchemes[strings.ToLower(parsed.Scheme)] {
		return "network_scheme_not_https"
	}
	if hasInvalidPort(parsed.Port()) {
		return "network_url_invalid_port"
	}
	if parsed.User != nil {
		if _, hasPwd := parsed.User.Password(); hasPwd || parsed.User.Username() != "" {
			return "network_url_userinfo"
		}
	}
	if HasSecretQuery(parsed.RawQuery) {
		return "network_secret_query"
	}
	host := NormalizeNetworkHost(parsed.Hostname())
	if MetadataHosts[host] {
		return "network_metadata_host"
	}
	if hasAnySuffix(host, InternalHostSuffixes) {
		return "network_internal_hostname"
	}
	return NetworkHostRejectionReason(host)
}

// AbsoluteURIHardBlockReason 返回规范 URI 事实里的不可覆盖安全证据。
//
// Python: absolute_uri_hard_block_reason(uri)
func AbsoluteURIHardBlockReason(uri AbsoluteURIFacts) string {
	if uri.InvalidPort {
		return "network_url_invalid_port"
	}
	if uri.UserinfoPresent {
		return "network_url_userinfo"
	}
	if uri.Scheme == "https" && uri.SecretQueryPresent {
		return "network_secret_query"
	}
	if !uri.NetworkLike {
		return ""
	}
	host := uri.Host
	if MetadataHosts[host] {
		return "network_metadata_host"
	}
	if hasAnySuffix(host, InternalHostSuffixes) {
		return "network_internal_hostname"
	}
	return NetworkHostRejectionReason(host)
}

// ExtractURLsFromText 返回文本中归一化后的 HTTP(S) URL。
//
// Python: extract_urls_from_text(text)
func ExtractURLsFromText(text string) []string {
	urls := []string{}
	seen := map[string]bool{}
	for _, match := range regexp2FindAll(URLPattern, text) {
		trimmed := strings.TrimRight(match, TrailingURLPunctuation)
		normalized := NormalizeURLForMatch(trimmed)
		if normalized == "" || seen[normalized] {
			continue
		}
		urls = append(urls, normalized)
		seen[normalized] = true
	}
	return urls
}

// NormalizeURLForMatch 返回用于精确来源比对的稳定 URL 形式。
//
// Python: normalize_url_for_match(url)
func NormalizeURLForMatch(rawURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(rawURL), TrailingURLPunctuation)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := NormalizeNetworkHost(parsed.Hostname())
	if scheme == "" || host == "" {
		return ""
	}
	portSuffix, ok := safePortSuffix(parsed.Port())
	if !ok {
		return ""
	}
	query := ""
	if parsed.RawQuery != "" {
		query = "?" + parsed.RawQuery
	}
	return scheme + "://" + host + portSuffix + parsed.Path + query
}

// RegistrableDomain 返回尽力而为的可注册域名（不依赖外部库）。
//
// Python: registrable_domain(host)
func RegistrableDomain(host string) string {
	normalized := NormalizeNetworkHost(host)
	labels := []string{}
	for _, label := range strings.Split(normalized, ".") {
		if label != "" {
			labels = append(labels, label)
		}
	}
	if len(labels) < 2 {
		return normalized
	}
	if len(labels) >= 3 {
		secondLevel := labels[len(labels)-2]
		switch secondLevel {
		case "co", "com", "edu", "gov", "net", "org":
			return strings.Join(labels[len(labels)-3:], ".")
		}
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveURLSourceKind 解析 URL 来源类别。
//
// Python: _resolve_url_source_kind(*, normalized_url, host, evidence, recent_url_sources)
func resolveURLSourceKind(
	normalizedURL, host string,
	evidence *OriginalUserIntentEvidence,
	recentURLSources []RecentURLSource,
) string {
	if matchesHostUserIntent(normalizedURL, host, evidence) {
		return "explicit_user_intent"
	}
	for _, source := range recentURLSources {
		if source.Trusted && source.URL == normalizedURL {
			return source.SourceKind
		}
	}
	return ""
}

// hasTrustedHostUserIntent 判定是否存在可信宿主用户意图。
//
// Python: _has_trusted_host_user_intent(evidence)
func hasTrustedHostUserIntent(evidence *OriginalUserIntentEvidence) bool {
	if evidence == nil || evidence.Source != UserIntentSourceHostUserMessage {
		return false
	}
	if strings.TrimSpace(evidence.Text) != "" {
		return true
	}
	if evidence.Context == nil {
		return false
	}
	for _, turn := range evidence.Context.TrustedTurns {
		if strings.TrimSpace(turn.Text) != "" || len(turn.Clarifications) > 0 {
			return true
		}
	}
	return false
}

// matchesHostUserIntent 判定 URL/主机是否匹配宿主用户意图。
//
// Python: _matches_host_user_intent(*, normalized_url, host, evidence)
func matchesHostUserIntent(normalizedURL, host string, evidence *OriginalUserIntentEvidence) bool {
	if evidence == nil || evidence.Source != UserIntentSourceHostUserMessage {
		return false
	}
	evidenceURLs, evidenceHosts := extractNetworkIntentTokens(evidence.Text)
	if evidenceURLs[normalizedURL] {
		return true
	}
	for evidenceHost := range evidenceHosts {
		if HostMatchesAllowedDomain(host, evidenceHost) {
			return true
		}
	}
	return false
}

// extractNetworkIntentTokens 提取意图文本里的 URL 与域名集合。
//
// Python: _extract_network_intent_tokens(text)
func extractNetworkIntentTokens(text string) (map[string]bool, map[string]bool) {
	urls := map[string]bool{}
	for _, u := range ExtractURLsFromText(text) {
		if normalized := NormalizeURLForMatch(u); normalized != "" {
			urls[normalized] = true
		}
	}
	hosts := map[string]bool{}
	for _, match := range regexp2FindAll(DomainPattern, text) {
		if host := NormalizeNetworkHost(match); host != "" {
			hosts[host] = true
		}
	}
	return urls, hosts
}

// hasInvalidPort 判定端口字符串是否非法（Python urlparse().port 抛 ValueError 的等价物）。
//
// Go url.Parse 对 :abc 直接报错，对 :99999 则放行且 Port() 返回 "99999"；
// Python 对两者都抛 ValueError。故此处自行校验范围。
func hasInvalidPort(port string) bool {
	if port == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return true
	}
	return n < 0 || n > 65535
}

// safePortSuffix 返回 ":port" 后缀；第二个返回值为 false 表示端口非法。
//
// Python: _safe_port_suffix(parsed) 返回 str | None
func safePortSuffix(port string) (string, bool) {
	if port == "" {
		return "", true
	}
	if hasInvalidPort(port) {
		return "", false
	}
	return ":" + port, true
}

// networkFacts 汇总决策所需的网络事实。
//
// Python: _network_facts(subject)
func networkFacts(subject ToolDecisionFacts) (map[string]struct{}, []string, []string, []string, bool) {
	network := InspectNetworkScope(subject)
	return subject.Capability.StaticSideEffects,
		network.URLs, network.Hosts, network.Schemes, network.UploadLike
}

// baseSummary 构造基础证据摘要。
//
// Python: _base_summary(subject)
func baseSummary(subject ToolDecisionFacts) map[string]any {
	_, urls, hosts, schemes, _ := networkFacts(subject)
	hostsHead := headStrings(hosts, 5)
	domains := make([]string, len(hostsHead))
	for i, host := range hostsHead {
		domains[i] = RegistrableDomain(host)
	}
	urlsHead := headStrings(urls, 5)
	normalizedURLs := make([]string, len(urlsHead))
	for i, u := range urlsHead {
		normalizedURLs[i] = NormalizeURLForMatch(u)
	}
	return map[string]any{
		"host_count":          len(hosts),
		"hosts":               hostsHead,
		"registrable_domains": domains,
		"schemes":             headStrings(schemes, 5),
		"url_count":           len(urls),
		"urls":                normalizedURLs,
	}
}

// rejectDecision 构造拒绝决策。
//
// Python: _reject(reason, summary, *, hard_block=False)
func rejectDecision(reason string, summary map[string]any, hardBlock bool) ReviewableURLDecision {
	return NewReviewableURLDecision(false, urlSafetyKindReadonlyFetch, reason, summary, hardBlock, false)
}

// semanticUnresolved 构造「接受但需语义审查」决策。
//
// Python: _semantic_unresolved(summary)
func semanticUnresolved(summary map[string]any) ReviewableURLDecision {
	return NewReviewableURLDecision(true, urlSafetyKindReadonlyFetch,
		"network_url_semantic_unresolved", summary, false, true)
}

// label 归一化标签文本（Python _label）。
func label(value any) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
}

// inspectURI 检查单个 URI 并产出有界事实。
//
// Python: _inspect_uri(uri, root)
func inspectURI(uri string, root *string) AbsoluteURIFacts {
	// 第一步：拆出协议、主机、端口、账号等基础事实。
	parsed, err := url.Parse(uri)
	if err != nil {
		return AbsoluteURIFacts{URI: uri, ParseFailureReason: "uri_parse_invalid"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := NormalizeNetworkHost(parsed.Hostname())
	invalidPort := hasInvalidPort(parsed.Port())
	userinfo := false
	if parsed.User != nil {
		_, hasPwd := parsed.User.Password()
		userinfo = parsed.User.Username() != "" || hasPwd
	}

	// 第二步：再做两项额外的文本检查——百分号编码能否解开、有没有 NUL 字符；
	// 非 file 协议缺主机也算解析失败。
	parseFailure := ""
	if strings.Contains(uri, "%") {
		decoded, decodeErr := url.PathUnescape(uri)
		if decodeErr != nil {
			parseFailure = "uri_utf8_invalid"
		} else if strings.ContainsRune(decoded, 0) {
			parseFailure = "uri_nul"
		}
	}
	if scheme != "file" && host == "" {
		if parseFailure == "" {
			parseFailure = "uri_host_missing"
		}
	}

	// 第三步：先落基础事实。非 file 协议或已判定解析失败的，到这里就返回。
	common := AbsoluteURIFacts{
		URI:                uri,
		Scheme:             scheme,
		Host:               host,
		InvalidPort:        invalidPort,
		UserinfoPresent:    userinfo,
		SecretQueryPresent: HasSecretQuery(parsed.RawQuery),
		NetworkLike:        scheme != "file" && host != "" && parseFailure == "",
		ParseFailureReason: parseFailure,
	}
	if scheme != "file" || parseFailure != "" {
		return common
	}
	// 第四步：file 协议还要确认是本机、且没带查询串或片段，否则只算「本地但不可解析」。
	if host != "" && host != "localhost" {
		common.FileResolutionReason = "file_uri_nonlocal_host"
		return common
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		common.FileLocal = true
		common.FileResolutionReason = "file_uri_query_or_fragment"
		return common
	}
	// 第五步：把路径解出来，相对路径按工作区根补全，最后给出规范化后的绝对路径。
	pathText, decodeErr := url.PathUnescape(parsed.Path)
	if decodeErr != nil {
		common.FileLocal = true
		common.FileResolutionReason = "file_uri_utf8_invalid"
		return common
	}
	if !strings.HasPrefix(pathText, "/") || strings.ContainsRune(pathText, 0) {
		common.FileLocal = true
		common.FileResolutionReason = "file_uri_path_invalid"
		return common
	}
	path := expandHomeString(pathText)
	if !filepath.IsAbs(path) && root != nil {
		path = filepath.Join(*root, path)
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		common.FileLocal = true
		common.FileResolutionReason = "file_uri_path_unresolvable"
		return common
	}
	common.FileLocal = true
	common.FilePath = filepath.ToSlash(filepath.Clean(abs))
	return common
}

// safeRoot 安全解析工作区根（失败返回 nil）。
//
// Python: _safe_root(value)
func safeRoot(value any) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return nil
	}
	path := expandHomeString(text)
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	cleaned := filepath.Clean(abs)
	return &cleaned
}

// stringValues 递归收集值里的所有字符串。
//
// Python: _string_values(value)
func stringValues(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case map[string]any:
		values := []string{}
		// Python dict 保序，Go map 无序；排序保证确定性
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			values = append(values, stringValues(v[k])...)
		}
		return values
	case []any:
		values := []string{}
		for _, nested := range v {
			values = append(values, stringValues(nested)...)
		}
		return values
	case []string:
		return v
	default:
		return nil
	}
}

// containsUpload 递归检测上传类参数。
//
// Python: _contains_upload(value)
func containsUpload(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, nested := range v {
			if UploadKeys[strings.ToLower(strings.TrimSpace(fmt.Sprint(key)))] && isTruthy(nested) {
				return true
			}
			if containsUpload(nested) {
				return true
			}
		}
		return false
	case []any:
		for _, item := range v {
			if containsUpload(item) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// hasAnySuffix 判定 host 是否以任一后缀结尾。
func hasAnySuffix(host string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// headStrings 取前 n 个元素。
func headStrings(values []string, n int) []string {
	if len(values) <= n {
		out := make([]string, len(values))
		copy(out, values)
		return out
	}
	out := make([]string, n)
	copy(out, values[:n])
	return out
}

// cloneSummary 浅拷贝证据摘要（Python `{**summary, ...}`）。
func cloneSummary(summary map[string]any) map[string]any {
	out := make(map[string]any, len(summary)+4)
	for k, v := range summary {
		out[k] = v
	}
	return out
}

// isTruthy Python 真值判定（非空字符串 / 非零数字 / 非空容器 / true）。
func isTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// regexp2FindAll 返回所有匹配文本。
func regexp2FindAll(re *regexp2.Regexp, input string) []string {
	out := []string{}
	m, err := re.FindStringMatch(input)
	if err != nil {
		return out
	}
	for m != nil {
		out = append(out, m.String())
		next, err := re.FindNextMatch(m)
		if err != nil {
			return out
		}
		m = next
	}
	return out
}
