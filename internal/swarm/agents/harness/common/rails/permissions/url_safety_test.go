package permissions

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// parseURLForTest 解析 URL 供测试用。
func parseURLForTest(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

// ──────────────────────────── 辅助 ────────────────────────────

// urlFactsSubject 构造带网络副作用的 ToolDecisionFacts。
func urlFactsSubject(toolName string, sideEffects []string, args map[string]any) ToolDecisionFacts {
	effects := map[string]struct{}{}
	for _, e := range sideEffects {
		effects[e] = struct{}{}
	}
	return ToolDecisionFacts{
		Capability: ToolCapability{
			ToolName:          toolName,
			Category:          "network",
			RiskTier:          "low",
			StaticSideEffects: effects,
		},
		UntrustedArgs: args,
		AccessesKnown: true,
	}
}

// hostUserEvidence 构造宿主用户消息证据。
func hostUserEvidence(text string) *OriginalUserIntentEvidence {
	return &OriginalUserIntentEvidence{
		Source: UserIntentSourceHostUserMessage,
		Text:   text,
	}
}

// ──────────────────────────── 常量 ────────────────────────────

func TestURLSafety_常量(t *testing.T) {
	if len(UploadKeys) != 6 || !UploadKeys["upload"] || !UploadKeys["file_path"] {
		t.Errorf("UploadKeys = %v", UploadKeys)
	}
	if !SafeURLSchemes["https"] || SafeURLSchemes["http"] {
		t.Errorf("SafeURLSchemes = %v", SafeURLSchemes)
	}
	if len(InternalHostSuffixes) != 4 {
		t.Errorf("InternalHostSuffixes = %v", InternalHostSuffixes)
	}
	for _, h := range []string{"169.254.169.254", "metadata", "metadata.google.internal"} {
		if !MetadataHosts[h] {
			t.Errorf("%q missing from MetadataHosts", h)
		}
	}
	if len(HardBlockReasons) != 10 {
		t.Errorf("HardBlockReasons count = %d, want 10", len(HardBlockReasons))
	}
	for _, r := range []string{"network_scheme_not_https", "network_url_userinfo", "network_metadata_host"} {
		if !HardBlockReasons[r] {
			t.Errorf("%q missing from HardBlockReasons", r)
		}
	}
	if TrailingURLPunctuation != ".,;:!?)]}'\"" {
		t.Errorf("TrailingURLPunctuation = %q", TrailingURLPunctuation)
	}
}

// ──────────────────────────── 用例：UnsafePublicHTTPSURLReason ───────────────────────────────

func TestUnsafePublicHTTPSURLReason_各类硬拦(t *testing.T) {
	cases := []struct{ url, want string }{
		{"http://example.com", "network_scheme_not_https"},
		{"https://user:pass@example.com", "network_url_userinfo"},
		{"https://169.254.169.254/latest", "network_metadata_host"},
		{"https://foo.internal/x", "network_internal_hostname"},
		{"https://foo.local/x", "network_internal_hostname"},
		{"https://example.com?api_key=secret", "network_secret_query"},
		{"https://example.com:99999/x", "network_url_invalid_port"},
	}
	for _, c := range cases {
		parsed, err := parseURLForTest(c.url)
		if err != nil {
			t.Fatalf("parse %q: %v", c.url, err)
		}
		if got := UnsafePublicHTTPSURLReason(parsed); got != c.want {
			t.Errorf("%q -> %q, want %q", c.url, got, c.want)
		}
	}
}

func TestUnsafePublicHTTPSURLReason_安全URL返回空(t *testing.T) {
	parsed, err := parseURLForTest("https://example.com/path")
	if err != nil {
		t.Fatal(err)
	}
	if got := UnsafePublicHTTPSURLReason(parsed); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestUnsafePublicHTTPSURLReason_单标签主机(t *testing.T) {
	parsed, err := parseURLForTest("https://intranet/x")
	if err != nil {
		t.Fatal(err)
	}
	got := UnsafePublicHTTPSURLReason(parsed)
	if got == "" {
		t.Error("single-label host should be rejected")
	}
	if !HardBlockReasons[got] {
		t.Errorf("reason %q should be in HardBlockReasons", got)
	}
}

// ──────────────────────────── 用例：AbsoluteURIHardBlockReason ───────────────────────────────

func TestAbsoluteURIHardBlockReason_各类(t *testing.T) {
	cases := []struct {
		name string
		uri  AbsoluteURIFacts
		want string
	}{
		{"invalid_port", AbsoluteURIFacts{InvalidPort: true}, "network_url_invalid_port"},
		{"userinfo", AbsoluteURIFacts{UserinfoPresent: true}, "network_url_userinfo"},
		{"secret_query_https", AbsoluteURIFacts{Scheme: "https", SecretQueryPresent: true}, "network_secret_query"},
		{"metadata", AbsoluteURIFacts{NetworkLike: true, Host: "169.254.169.254"}, "network_metadata_host"},
		{"internal", AbsoluteURIFacts{NetworkLike: true, Host: "foo.internal"}, "network_internal_hostname"},
		{"not_network", AbsoluteURIFacts{NetworkLike: false, Host: "foo.internal"}, ""},
	}
	for _, c := range cases {
		if got := AbsoluteURIHardBlockReason(c.uri); got != c.want {
			t.Errorf("%s -> %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAbsoluteURIHardBlockReason_http密钥query不拦(t *testing.T) {
	// Python: 只有 scheme == "https" 且 secret_query_present 才拦
	uri := AbsoluteURIFacts{Scheme: "http", SecretQueryPresent: true, NetworkLike: true, Host: "example.com"}
	if got := AbsoluteURIHardBlockReason(uri); got != "" {
		t.Errorf("http with secret query should not hard-block on that rule, got %q", got)
	}
}

// ──────────────────────────── 用例：ExtractURLsFromText ───────────────────────────────

func TestExtractURLsFromText_基础(t *testing.T) {
	got := ExtractURLsFromText("see https://example.com/a and http://foo.org/b")
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "https://example.com/a" {
		t.Errorf("got[0] = %q", got[0])
	}
}

func TestExtractURLsFromText_去尾标点(t *testing.T) {
	got := ExtractURLsFromText("visit https://example.com/a.")
	if len(got) != 1 || strings.HasSuffix(got[0], ".") {
		t.Errorf("trailing punctuation should be stripped, got %v", got)
	}
}

func TestExtractURLsFromText_去重(t *testing.T) {
	got := ExtractURLsFromText("https://a.com/x https://a.com/x")
	if len(got) != 1 {
		t.Errorf("got %v, want deduped", got)
	}
}

func TestExtractURLsFromText_空(t *testing.T) {
	if got := ExtractURLsFromText(""); len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if got := ExtractURLsFromText("no urls here"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：NormalizeURLForMatch ───────────────────────────────

func TestNormalizeURLForMatch_小写scheme与主机(t *testing.T) {
	got := NormalizeURLForMatch("HTTPS://Example.COM/Path")
	if got != "https://example.com/Path" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeURLForMatch_保留端口与query(t *testing.T) {
	got := NormalizeURLForMatch("https://example.com:8443/a?x=1")
	if got != "https://example.com:8443/a?x=1" {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeURLForMatch_去尾标点(t *testing.T) {
	got := NormalizeURLForMatch("https://example.com/a.")
	if strings.HasSuffix(got, ".") {
		t.Errorf("got %q", got)
	}
}

func TestNormalizeURLForMatch_无scheme或host返回空(t *testing.T) {
	for _, s := range []string{"", "not-a-url", "https://", "/just/path"} {
		if got := NormalizeURLForMatch(s); got != "" {
			t.Errorf("%q -> %q, want empty", s, got)
		}
	}
}

func TestNormalizeURLForMatch_非法端口返回空(t *testing.T) {
	if got := NormalizeURLForMatch("https://example.com:99999/a"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ──────────────────────────── 用例：RegistrableDomain ───────────────────────────────

func TestRegistrableDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"example.com", "example.com"},
		{"www.example.com", "example.com"},
		{"a.b.example.com", "example.com"},
		{"example.co.uk", "example.co.uk"},
		{"a.example.co.uk", "example.co.uk"},
		{"localhost", "localhost"},
		{"", ""},
	}
	for _, c := range cases {
		if got := RegistrableDomain(c.in); got != c.want {
			t.Errorf("RegistrableDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ──────────────────────────── 用例：InspectAbsoluteUris ───────────────────────────────

func TestInspectAbsoluteUris_提取与去重(t *testing.T) {
	args := map[string]any{
		"a": "fetch https://example.com/x and https://example.com/x",
		"b": "also http://foo.org/y",
	}
	got := InspectAbsoluteUris(args, nil)
	if len(got) != 2 {
		t.Fatalf("got %d uris: %v", len(got), got)
	}
}

func TestInspectAbsoluteUris_嵌套结构(t *testing.T) {
	args := map[string]any{"list": []any{"https://a.com/x", map[string]any{"k": "https://b.com/y"}}}
	got := InspectAbsoluteUris(args, nil)
	if len(got) != 2 {
		t.Errorf("got %d", len(got))
	}
}

func TestInspectAbsoluteUris_非字符串忽略(t *testing.T) {
	if got := InspectAbsoluteUris(42, nil); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestInspectAbsoluteUris_uriFacts字段(t *testing.T) {
	got := InspectAbsoluteUris("https://user:pw@example.com:8443/a?token=x", nil)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	f := got[0]
	if f.Scheme != "https" || f.Host != "example.com" {
		t.Errorf("scheme/host = %q/%q", f.Scheme, f.Host)
	}
	if !f.UserinfoPresent {
		t.Error("userinfo_present should be true")
	}
	if !f.SecretQueryPresent {
		t.Error("secret_query_present should be true")
	}
	if !f.NetworkLike {
		t.Error("network_like should be true")
	}
}

func TestInspectAbsoluteUris_fileURI(t *testing.T) {
	dir := t.TempDir()
	got := InspectAbsoluteUris("file://"+dir+"/a.txt", nil)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if !got[0].FileLocal {
		t.Errorf("file_local should be true: %+v", got[0])
	}
	if got[0].FilePath == "" {
		t.Errorf("file_path should be resolved: %+v", got[0])
	}
	if got[0].NetworkLike {
		t.Error("file URI should not be network_like")
	}
}

func TestInspectAbsoluteUris_fileURI非本地主机(t *testing.T) {
	got := InspectAbsoluteUris("file://otherhost/a.txt", nil)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if got[0].FileResolutionReason != "file_uri_nonlocal_host" {
		t.Errorf("got %q", got[0].FileResolutionReason)
	}
}

func TestInspectAbsoluteUris_fileURI带query(t *testing.T) {
	got := InspectAbsoluteUris("file:///tmp/a.txt?x=1", nil)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if got[0].FileResolutionReason != "file_uri_query_or_fragment" {
		t.Errorf("got %q", got[0].FileResolutionReason)
	}
}

// ──────────────────────────── 用例：InspectNetworkScope ───────────────────────────────

func TestInspectNetworkScope_汇总(t *testing.T) {
	subject := urlFactsSubject("web_fetch", []string{"network_read"}, map[string]any{
		"url": "https://example.com/a",
	})
	got := InspectNetworkScope(subject)
	if len(got.URLs) != 1 || got.URLs[0] != "https://example.com/a" {
		t.Errorf("URLs = %v", got.URLs)
	}
	if len(got.Hosts) != 1 || got.Hosts[0] != "example.com" {
		t.Errorf("Hosts = %v", got.Hosts)
	}
	if len(got.Schemes) != 1 || got.Schemes[0] != "https" {
		t.Errorf("Schemes = %v", got.Schemes)
	}
	if got.UploadLike {
		t.Error("upload_like should be false")
	}
}

func TestInspectNetworkScope_upload检测(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"upload": "file-content",
	})
	if !InspectNetworkScope(subject).UploadLike {
		t.Error("upload key with truthy value should set upload_like")
	}
}

func TestInspectNetworkScope_upload空值不算(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{"upload": ""})
	if InspectNetworkScope(subject).UploadLike {
		t.Error("empty upload value should not set upload_like")
	}
}

func TestInspectNetworkScope_嵌套upload(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"nested": []any{map[string]any{"attachment": "x"}},
	})
	if !InspectNetworkScope(subject).UploadLike {
		t.Error("nested attachment should be detected")
	}
}

// ──────────────────────────── ReviewableURLDecision 校验 ────────────────────────────

func TestNewReviewableURLDecision_semantic不能覆盖拒绝(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("semantic review on rejected decision should raise ValueError")
		}
	}()
	NewReviewableURLDecision(false, "readonly_network_fetch", "r", nil, false, true)
}

func TestNewReviewableURLDecision_hardBlock冲突(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("semantic review with hard_block should raise ValueError")
		}
	}()
	NewReviewableURLDecision(true, "readonly_network_fetch", "r", nil, true, true)
}

func TestNewReviewableURLDecision_合法组合(t *testing.T) {
	d := NewReviewableURLDecision(true, "readonly_network_fetch", "r", nil, false, true)
	if !d.Accepted || !d.SemanticReviewRequired {
		t.Errorf("got %+v", d)
	}
}

// ──────────────────────────── 用例：RecentURLSource ───────────────────────────────

func TestRecentURLSource_ToJSONDict(t *testing.T) {
	src := RecentURLSource{
		URL:        "https://example.com/a",
		Host:       "example.com",
		SourceTool: "mcp_free_search",
		SourceKind: "recent_search_result",
		Trusted:    true,
	}
	got := src.ToJSONDict()
	if got["url"] != "https://example.com/a" || got["host"] != "example.com" {
		t.Errorf("got %v", got)
	}
	if got["trusted"] != true || got["source_kind"] != "recent_search_result" {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：EvaluateReviewableURLScope ───────────────────────────────

func TestEvaluateReviewableURLScope_externalSend拒绝(t *testing.T) {
	subject := urlFactsSubject("web_fetch", []string{"external_send"}, map[string]any{
		"url": "https://example.com/a",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("fetch https://example.com/a"), nil)
	if got.Accepted || got.Reason != "network_external_send" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_upload拒绝(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url":    "https://example.com/a",
		"upload": "content",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("x"), nil)
	if got.Accepted || got.Reason != "network_upload_like" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_无URL拒绝(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("x"), nil)
	if got.Reason != "network_url_missing" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_多URL拒绝(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"a": "https://a.com/x", "b": "https://b.com/y",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("x"), nil)
	if got.Reason != "network_url_count_not_one" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_不安全URL硬拦(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "http://example.com/a",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("x"), nil)
	if got.Accepted || !got.HardBlock || got.Reason != "network_scheme_not_https" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_无用户意图拒绝(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com/a",
	})
	got := EvaluateReviewableURLScope(subject, nil, nil)
	if got.Accepted || got.Reason != "original_user_intent_missing" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_显式用户意图接受(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com/a",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("please fetch https://example.com/a"), nil)
	if !got.Accepted || got.Reason != "readonly_network_url_reviewable" {
		t.Errorf("got %+v", got)
	}
	if got.EvidenceSummary["source_kind"] != "explicit_user_intent" {
		t.Errorf("source_kind = %v", got.EvidenceSummary["source_kind"])
	}
	if got.SemanticReviewRequired {
		t.Error("explicit intent should not require semantic review")
	}
}

func TestEvaluateReviewableURLScope_最近搜索结果(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com/a",
	})
	sources := []RecentURLSource{{
		URL: "https://example.com/a", Host: "example.com",
		SourceTool: "mcp_free_search", SourceKind: "recent_search_result", Trusted: true,
	}}
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("fetch it"), sources)
	if !got.Accepted || got.EvidenceSummary["source_kind"] != "recent_search_result" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_不可信来源忽略(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com/a",
	})
	sources := []RecentURLSource{{
		URL: "https://example.com/a", SourceTool: "x", Trusted: false,
	}}
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("fetch it"), sources)
	if got.Reason != "network_url_semantic_unresolved" {
		t.Errorf("untrusted source should not resolve, got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_语义未决(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com/a",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("fetch it"), nil)
	if !got.Accepted || !got.SemanticReviewRequired {
		t.Errorf("got %+v", got)
	}
	if got.Reason != "network_url_semantic_unresolved" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// ──────────────────────────── 私有助手 ────────────────────────────

func TestLabel(t *testing.T) {
	if got := label("  HeLLo  "); got != "hello" {
		t.Errorf("got %q", got)
	}
	if got := label(nil); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestSafeRoot(t *testing.T) {
	if got := safeRoot(nil); got != nil {
		t.Errorf("got %v", got)
	}
	dir := t.TempDir()
	if got := safeRoot(dir); got == nil {
		t.Error("should resolve")
	}
}

func TestStringValues_各类型(t *testing.T) {
	if got := stringValues("a"); len(got) != 1 {
		t.Errorf("string -> %v", got)
	}
	if got := stringValues(map[string]any{"k": "v"}); len(got) != 1 || got[0] != "v" {
		t.Errorf("map -> %v", got)
	}
	if got := stringValues([]any{"a", "b"}); len(got) != 2 {
		t.Errorf("list -> %v", got)
	}
	if got := stringValues(42); len(got) != 0 {
		t.Errorf("int -> %v", got)
	}
}

func TestContainsUpload_各类型(t *testing.T) {
	if !containsUpload(map[string]any{"file": "x"}) {
		t.Error("file key should count")
	}
	if containsUpload(map[string]any{"other": "x"}) {
		t.Error("non-upload key should not count")
	}
	if !containsUpload([]any{map[string]any{"upload": 1}}) {
		t.Error("nested list upload should count")
	}
	if containsUpload("upload") {
		t.Error("bare string should not count")
	}
}

func TestExtractNetworkIntentTokens(t *testing.T) {
	urls, hosts := extractNetworkIntentTokens("see https://example.com/a and foo.org")
	if len(urls) != 1 || !urls["https://example.com/a"] {
		t.Errorf("urls = %v", urls)
	}
	if len(hosts) != 2 {
		t.Errorf("hosts = %v", hosts)
	}
}

func TestHasTrustedHostUserIntent_各分支(t *testing.T) {
	if hasTrustedHostUserIntent(nil) {
		t.Error("nil evidence should be false")
	}
	if hasTrustedHostUserIntent(&OriginalUserIntentEvidence{Source: "other", Text: "x"}) {
		t.Error("non-host source should be false")
	}
	if !hasTrustedHostUserIntent(hostUserEvidence("text")) {
		t.Error("host message with text should be true")
	}
	if hasTrustedHostUserIntent(hostUserEvidence("")) {
		t.Error("empty text without context should be false")
	}
	// context 里有可信轮次
	ev := &OriginalUserIntentEvidence{
		Source: UserIntentSourceHostUserMessage,
		Context: &RootDecisionContext{
			TrustedTurns: []RootIntentTurn{{Text: "turn text"}},
		},
	}
	if !hasTrustedHostUserIntent(ev) {
		t.Error("trusted turn text should make it true")
	}
}

func TestMatchesHostUserIntent(t *testing.T) {
	ev := hostUserEvidence("fetch https://example.com/a")
	if !matchesHostUserIntent("https://example.com/a", "example.com", ev) {
		t.Error("exact URL should match")
	}
	if !matchesHostUserIntent("https://example.com/b", "example.com", ev) {
		t.Error("same host should match via domain")
	}
	if matchesHostUserIntent("https://other.com/a", "other.com", ev) {
		t.Error("different host should not match")
	}
	if matchesHostUserIntent("https://example.com/a", "example.com", nil) {
		t.Error("nil evidence should not match")
	}
}

func TestResolveURLSourceKind(t *testing.T) {
	ev := hostUserEvidence("fetch https://example.com/a")
	if got := resolveURLSourceKind("https://example.com/a", "example.com", ev, nil); got != "explicit_user_intent" {
		t.Errorf("got %q", got)
	}
	sources := []RecentURLSource{{URL: "https://x.com/a", SourceKind: "recent_search_result", Trusted: true}}
	if got := resolveURLSourceKind("https://x.com/a", "x.com", nil, sources); got != "recent_search_result" {
		t.Errorf("got %q", got)
	}
	if got := resolveURLSourceKind("https://y.com/a", "y.com", nil, sources); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestHasInvalidPort(t *testing.T) {
	if !hasInvalidPort("99999") {
		t.Error("99999 out of 0-65535 should be invalid")
	}
	if !hasInvalidPort("65536") {
		t.Error("65536 should be invalid")
	}
	if hasInvalidPort("") || hasInvalidPort("443") {
		t.Error("empty and 443 should be valid")
	}
	if !hasInvalidPort("abc") {
		t.Error("non-numeric should be invalid")
	}
}

func TestSafePortSuffix(t *testing.T) {
	if got, ok := safePortSuffix("443"); got != ":443" || !ok {
		t.Errorf("got (%q,%v)", got, ok)
	}
	if got, ok := safePortSuffix(""); got != "" || !ok {
		t.Errorf("got (%q,%v)", got, ok)
	}
	if _, ok := safePortSuffix("99999"); ok {
		t.Error("invalid port should return ok=false")
	}
}

// ──────────────────────────── 补充：file URI 分支与覆盖率 ────────────────────────────

func TestInspectURI_fileURI路径非法(t *testing.T) {
	// file:/// 后跟相对路径（不以 / 开头）
	got := inspectURI("file:relative/path.txt", nil)
	if got.FileResolutionReason != "file_uri_path_invalid" {
		t.Errorf("got %+v", got)
	}
}

func TestInspectURI_fileURI含NUL(t *testing.T) {
	// Python: parse_failure = "uri_nul"，随后因 parse_failure 非空直接返回 common
	got := inspectURI("file:///tmp/a%00b.txt", nil)
	if got.ParseFailureReason != "uri_nul" {
		t.Errorf("NUL should set parse_failure_reason=uri_nul, got %+v", got)
	}
	if got.FileLocal {
		t.Error("parse failure should short-circuit before file resolution")
	}
}

func TestInspectURI_fileURI相对路径被拒(t *testing.T) {
	// Python: file:relative.txt 的 path 不以 "/" 开头 → file_uri_path_invalid，
	// 不会走到 root 拼接分支（root 只对已通过 / 前缀检查的路径生效）。
	dir := t.TempDir()
	got := inspectURI("file:relative.txt", &dir)
	if got.FileResolutionReason != "file_uri_path_invalid" {
		t.Errorf("got %+v", got)
	}
	if got.FilePath != "" {
		t.Errorf("should not resolve, got %q", got.FilePath)
	}
}

func TestInspectURI_fileURI绝对路径加root(t *testing.T) {
	dir := t.TempDir()
	got := inspectURI("file://"+dir+"/a.txt", &dir)
	if !got.FileLocal || got.FilePath == "" {
		t.Errorf("got %+v", got)
	}
	if !strings.HasPrefix(got.FilePath, filepath.ToSlash(dir)) {
		t.Errorf("FilePath %q should be under %q", got.FilePath, dir)
	}
}

func TestInspectURI_非file无host(t *testing.T) {
	got := inspectURI("https:///nohost", nil)
	if got.ParseFailureReason != "uri_host_missing" {
		t.Errorf("got %+v", got)
	}
	if got.NetworkLike {
		t.Error("should not be network_like")
	}
}

func TestInspectURI_localhost允许(t *testing.T) {
	got := inspectURI("file://localhost/tmp/a.txt", nil)
	if got.FileResolutionReason == "file_uri_nonlocal_host" {
		t.Error("localhost should be allowed")
	}
	if !got.FileLocal {
		t.Errorf("should be file_local: %+v", got)
	}
}

func TestIsTruthy_各类型(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{nil, false}, {true, true}, {false, false},
		{"", false}, {"x", true},
		{0, false}, {1, true},
		{int64(0), false}, {int64(2), true},
		{float64(0), false}, {float64(1.5), true},
		{[]any{}, false}, {[]any{1}, true},
		{map[string]any{}, false}, {map[string]any{"k": 1}, true},
		{struct{}{}, true},
	}
	for _, c := range cases {
		if got := isTruthy(c.in); got != c.want {
			t.Errorf("isTruthy(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestHeadStrings_超长(t *testing.T) {
	got := headStrings([]string{"a", "b", "c"}, 2)
	if len(got) != 2 || got[0] != "a" {
		t.Errorf("got %v", got)
	}
	if got := headStrings([]string{"a"}, 5); len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestBaseSummary_超5项截断(t *testing.T) {
	args := map[string]any{}
	for i := 0; i < 8; i++ {
		args[fmt.Sprintf("k%d", i)] = fmt.Sprintf("https://h%d.com/x", i)
	}
	subject := urlFactsSubject("web_fetch", nil, args)
	summary := baseSummary(subject)
	if len(summary["hosts"].([]string)) != 5 {
		t.Errorf("hosts should be capped at 5, got %v", summary["hosts"])
	}
	if summary["host_count"] != 8 {
		t.Errorf("host_count = %v", summary["host_count"])
	}
}

func TestEvaluateReviewableURLScope_非法端口URL(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com:99999/a",
	})
	got := EvaluateReviewableURLScope(subject, hostUserEvidence("x"), nil)
	if got.Reason != "network_url_invalid_port" {
		t.Errorf("got %+v", got)
	}
}

func TestEvaluateReviewableURLScope_askUser澄清走语义(t *testing.T) {
	subject := urlFactsSubject("web_fetch", nil, map[string]any{
		"url": "https://example.com/a",
	})
	ev := &OriginalUserIntentEvidence{
		Source: UserIntentSourceHostUserMessage,
		Text:   "fetch it",
		Context: &RootDecisionContext{
			TrustedTurns: []RootIntentTurn{{
				Kind:           RootIntentTurnKindAskUserClarification,
				Clarifications: []RootAskUserClarification{{Question: "which?", Answers: []string{"a"}}},
			}},
		},
	}
	got := EvaluateReviewableURLScope(subject, ev, nil)
	if !got.SemanticReviewRequired {
		t.Errorf("ask_user clarification without search result should be semantic, got %+v", got)
	}
}
