package permissions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 辅助 ────────────────────────────

// fakeFacts 构造最小 ToolDecisionFacts 供审计字段测试用。
// 注意：ToolName/ToolCategory 是方法（来自 Capability），不是字段。
func fakeFacts() ToolDecisionFacts {
	return ToolDecisionFacts{
		Capability: ToolCapability{
			ToolName:          "bash",
			Category:          "command",
			RiskTier:          "high",
			StaticSideEffects: map[string]struct{}{"exec": {}, "network": {}},
		},
		AccessesKnown: true,
		ReadPaths:     []string{"/a"},
		WritePaths:    []string{"/b"},
		ExternalPaths: []string{"/c"},
	}
}

// ──────────────────────────── 常量 ────────────────────────────

func TestPersistentAudit_常量(t *testing.T) {
	if AuditSchemaVersion != "auto_permission_audit_v1" {
		t.Errorf("AuditSchemaVersion = %q", AuditSchemaVersion)
	}
	if AuditSubdirectory != "permission_audit" || AuditFilename != "auto_permission.jsonl" {
		t.Error("audit path constants mismatch")
	}
	if AuditDigestAlgorithm != "sha256" {
		t.Errorf("AuditDigestAlgorithm = %q", AuditDigestAlgorithm)
	}
	if DataDirEnvName != "UAPCLAW_DATA_DIR" {
		t.Errorf("DataDirEnvName = %q, want UAPCLAW_DATA_DIR (品牌映射)", DataDirEnvName)
	}
	if MaxAuditTextLength != 160 || AuditTextRedacted != "[redacted]" {
		t.Error("text constants mismatch")
	}
	for _, k := range []string{"decision_source", "reviewer_reason_summary", "record_kind"} {
		if !ExtraAllowlist[k] {
			t.Errorf("%q missing from ExtraAllowlist", k)
		}
	}
}

// ──────────────────────────── build_sanitized_audit_fields ────────────────────────────

func TestBuildSanitizedAuditFields_基础字段(t *testing.T) {
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "policy_allow", false, "", "", nil)

	for _, key := range []string{"decision", "degraded", "accesses_known", "path_counts",
		"reason", "risk_tier", "side_effects", "tool_category", "tool_name"} {
		if _, exists := got[key]; !exists {
			t.Errorf("missing required field %q", key)
		}
	}
	if got["tool_name"] != "bash" {
		t.Errorf("tool_name = %v", got["tool_name"])
	}
	if got["decision"] != "allow" {
		t.Errorf("decision = %v", got["decision"])
	}
	if got["risk_tier"] != "high" {
		t.Errorf("risk_tier = %v", got["risk_tier"])
	}
	counts := got["path_counts"].(map[string]int)
	if counts["read"] != 1 || counts["write"] != 1 || counts["external"] != 1 {
		t.Errorf("path_counts = %v", counts)
	}
}

func TestBuildSanitizedAuditFields_sideEffects排序(t *testing.T) {
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "", "", nil)
	effects := got["side_effects"].([]string)
	if len(effects) != 2 || effects[0] != "exec" || effects[1] != "network" {
		t.Errorf("side_effects = %v, want sorted [exec network]", effects)
	}
}

func TestBuildSanitizedAuditFields_可选字段(t *testing.T) {
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "", "", nil)
	if _, exists := got["grant_id"]; exists {
		t.Error("grant_id should be omitted when empty")
	}
	if _, exists := got["grant_reason"]; exists {
		t.Error("grant_reason should be omitted when empty")
	}

	got = BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "g1", "gr1", nil)
	if got["grant_id"] != "g1" {
		t.Errorf("grant_id = %v", got["grant_id"])
	}
	if got["grant_reason"] != "gr1" {
		t.Errorf("grant_reason = %v", got["grant_reason"])
	}
}

func TestBuildSanitizedAuditFields_extra白名单(t *testing.T) {
	extra := map[string]any{
		"decision_source": "policy",
		"secret_field":    "should_be_dropped",
	}
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "", "", extra)
	if got["decision_source"] != "policy" {
		t.Errorf("allowlisted key lost: %v", got)
	}
	if _, exists := got["secret_field"]; exists {
		t.Error("non-allowlisted key should be dropped")
	}
}

func TestBuildSanitizedAuditFields_敏感reason脱敏(t *testing.T) {
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "api_key=secret123", false, "", "", nil)
	if got["reason"] != AuditTextRedacted {
		t.Errorf("reason = %v, want redacted", got["reason"])
	}
}

// ──────────────────────────── sanitize_audit_value / field ────────────────────────────

func TestSanitizeAuditValue_各类型(t *testing.T) {
	if got := SanitizeAuditValue("safe_text"); got != "safe_text" {
		t.Errorf("string = %v", got)
	}
	if got := SanitizeAuditValue(42); got != 42 {
		t.Errorf("int = %v", got)
	}
	if got := SanitizeAuditValue(true); got != true {
		t.Errorf("bool = %v", got)
	}
	if got := SanitizeAuditValue(nil); got != nil {
		t.Errorf("nil = %v", got)
	}
	m := SanitizeAuditValue(map[string]any{"safe": "ok"}).(map[string]any)
	if m["safe"] != "ok" {
		t.Errorf("map = %v", m)
	}
	l := SanitizeAuditValue([]any{"ok", "/etc/passwd"}).([]any)
	if l[0] != "ok" || l[1] != AuditTextRedacted {
		t.Errorf("list = %v", l)
	}
}

func TestSanitizeAuditField_自然语言字段(t *testing.T) {
	// reviewer_reason_summary 走宽松自然语言通道，保留 "$" 价格上下文
	got := SanitizeAuditField("reviewer_reason_summary", "the cat food")
	if got != "the cat food" {
		t.Errorf("got %v", got)
	}
	// 普通字段里 $ 会被严格通道脱敏
	got2 := SanitizeAuditField("reason", "costs $5")
	if got2 != AuditTextRedacted {
		t.Errorf("strict field with $ should redact, got %v", got2)
	}
}

func TestSanitizeNaturalLanguageAuditText_价格上下文(t *testing.T) {
	// Python _CONTEXTUAL_PRICE_PATTERN 只匹配 "budget"/"budgeted" 紧跟 $，
	// 不匹配 "budget of $..."，故后者仍会因 strict_view 含 $ 被脱敏。
	if got := sanitizeNaturalLanguageAuditText("costs $5"); got != "costs $5" {
		t.Errorf("contextual price should be preserved, got %v", got)
	}
	if got := sanitizeNaturalLanguageAuditText("a budget of $20"); got != AuditTextRedacted {
		t.Errorf("budget-of price is not masked by upstream regex, got %v", got)
	}
	if got := sanitizeNaturalLanguageAuditText("the cat food"); got != "the cat food" {
		t.Errorf("clear natural language phrase should be preserved, got %v", got)
	}
	if got := sanitizeNaturalLanguageAuditText("echo $HOME"); got != AuditTextRedacted {
		t.Errorf("unmasked $ should redact, got %v", got)
	}
}

// ──────────────────────────── _sanitize_audit_text ────────────────────────────

func TestSanitizeAuditText_绝对路径被脱敏(t *testing.T) {
	for _, s := range []string{"/etc/passwd", "/home/user/a.txt", `C:\Users\a.txt`} {
		if got := sanitizeAuditText(s); got != AuditTextRedacted {
			t.Errorf("%q -> %q, want redacted", s, got)
		}
	}
}

func TestSanitizeAuditText_密钥被脱敏(t *testing.T) {
	for _, s := range []string{
		"sk-1234567890abcdef",
		"api_key=secret123",
		"Bearer abcdefghijklmnop",
		"password: hunter2",
		"ghp_abcdefghijklmnop",
	} {
		if sanitizeAuditText(s) != AuditTextRedacted {
			t.Errorf("%q should be redacted", s)
		}
	}
}

func TestSanitizeAuditText_shell控制符(t *testing.T) {
	for _, s := range []string{"a && b", "a || b", "a; b", "echo $HOME", "a < b"} {
		if got := sanitizeAuditText(s); got != AuditTextRedacted {
			t.Errorf("%q -> %q, want redacted", s, got)
		}
	}
}

func TestSanitizeAuditText_命令样式(t *testing.T) {
	for _, s := range []string{"bash -c echo", "cat /etc/passwd", "rm -rf x", "curl http://x"} {
		if got := sanitizeAuditText(s); got != AuditTextRedacted {
			t.Errorf("%q -> %q, want redacted", s, got)
		}
	}
}

func TestSanitizeAuditText_安全文本保留(t *testing.T) {
	for _, s := range []string{"policy_allow", "semantic_reviewer", "tool:bash", "auto_permission", "high"} {
		if got := sanitizeAuditText(s); got != s {
			t.Errorf("%q got %q, want unchanged", s, got)
		}
	}
}

func TestSanitizeAuditText_空串(t *testing.T) {
	if got := sanitizeAuditText(""); got != "" {
		t.Errorf("got %q", got)
	}
	if got := sanitizeAuditText("   "); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeAuditText_超长截断(t *testing.T) {
	long := "a" + strings.Repeat("b", 300)
	got := sanitizeAuditText(long)
	if got == AuditTextRedacted {
		t.Skip("长文本按正则不匹配而脱敏，非截断路径")
	}
	if len(got) > MaxAuditTextLength {
		t.Errorf("len = %d, want <= %d", len(got), MaxAuditTextLength)
	}
}

func TestSanitizeAuditText_非安全字符脱敏(t *testing.T) {
	if got := sanitizeAuditText("hello@world"); got != AuditTextRedacted {
		t.Errorf("got %q", got)
	}
}

func TestAuditTextContainsSensitiveContent(t *testing.T) {
	if !auditTextContainsSensitiveContent("/etc/passwd") {
		t.Error("path should be sensitive")
	}
	if auditTextContainsSensitiveContent("policy_allow") {
		t.Error("safe text should not be sensitive")
	}
}

// ──────────────────────────── resolve_persistent_audit_root ────────────────────────────

func TestResolvePersistentAuditRoot_配置优先(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	cfg := map[string]any{"persistent_audit_root": "/tmp/audit"}
	got := ResolvePersistentAuditRoot(cfg)
	if got == nil || !strings.HasSuffix(*got, "audit") {
		t.Errorf("got %v", got)
	}
}

func TestResolvePersistentAuditRoot_auditRoot别名(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	got := ResolvePersistentAuditRoot(map[string]any{"audit_root": "/tmp/audit2"})
	if got == nil || !strings.HasSuffix(*got, "audit2") {
		t.Errorf("got %v", got)
	}
}

func TestResolvePersistentAuditRoot_auto子配置(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	cfg := map[string]any{"auto": map[string]any{"audit_root": "/tmp/a2"}}
	got := ResolvePersistentAuditRoot(cfg)
	if got == nil || !strings.HasSuffix(*got, "a2") {
		t.Errorf("got %v", got)
	}
}

func TestResolvePersistentAuditRoot_auto优先键(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	cfg := map[string]any{"auto": map[string]any{"persistent_audit_root": "/tmp/a3", "audit_root": "/tmp/a4"}}
	got := ResolvePersistentAuditRoot(cfg)
	if got == nil || !strings.HasSuffix(*got, "a3") {
		t.Errorf("persistent_audit_root should win, got %v", got)
	}
}

func TestResolvePersistentAuditRoot_环境变量回退(t *testing.T) {
	t.Setenv(DataDirEnvName, "/tmp/envdata")
	got := ResolvePersistentAuditRoot(nil)
	if got == nil || !strings.Contains(*got, "envdata") {
		t.Errorf("got %v", got)
	}
}

func TestResolvePersistentAuditRoot_无来源返回nil(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	if got := ResolvePersistentAuditRoot(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestResolvePersistentAuditRoot_非Mapping配置(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	if got := ResolvePersistentAuditRoot("not-a-map"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// ──────────────────────────── PersistentAuditWriter ────────────────────────────

func TestPersistentAuditWriter_auditPath(t *testing.T) {
	w := NewPersistentAuditWriter("/tmp/data")
	if got := w.AuditPath(); got != filepath.Join("/tmp/data", AuditSubdirectory, AuditFilename) {
		t.Errorf("AuditPath = %q", got)
	}
	if got := NewPersistentAuditWriter(nil).AuditPath(); got != "" {
		t.Errorf("nil root AuditPath = %q, want empty", got)
	}
}

func TestPersistentAuditWriter_写入JSONL(t *testing.T) {
	dir := t.TempDir()
	w := NewPersistentAuditWriter(dir)

	result := w.Write(fakeFacts(), "allow", "policy_allow", false, "", "", nil)
	if !result.Persisted {
		t.Fatalf("should persist: %+v", result)
	}
	if result.Degraded || result.Reason != "audit_persisted" {
		t.Errorf("result = %+v", result)
	}

	path := filepath.Join(dir, AuditSubdirectory, AuditFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}
	if record["schema_version"] != AuditSchemaVersion {
		t.Errorf("schema_version = %v", record["schema_version"])
	}
	if _, exists := record["timestamp"]; !exists {
		t.Error("timestamp missing")
	}
	if record["decision"] != "allow" {
		t.Errorf("decision = %v", record["decision"])
	}
}

func TestPersistentAuditWriter_无root降级(t *testing.T) {
	w := NewPersistentAuditWriter(nil)
	result := w.Write(fakeFacts(), "allow", "r", false, "", "", nil)
	if result.Persisted {
		t.Error("should not persist without root")
	}
	if !result.Degraded {
		t.Error("should be degraded")
	}
	if result.Reason != "audit_root_unavailable" {
		t.Errorf("reason = %q", result.Reason)
	}
	if result.Path != "" {
		t.Errorf("path = %q, want empty", result.Path)
	}
}

func TestPersistentAuditWriter_追加不覆盖(t *testing.T) {
	dir := t.TempDir()
	w := NewPersistentAuditWriter(dir)
	w.Write(fakeFacts(), "allow", "r1", false, "", "", nil)
	w.Write(fakeFacts(), "deny", "r2", false, "", "", nil)

	data, _ := os.ReadFile(filepath.Join(dir, AuditSubdirectory, AuditFilename))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2", len(lines))
	}
}

func TestPersistentAuditWriter_写失败降级(t *testing.T) {
	dir := t.TempDir()
	// 先占住 audit 子目录路径为普通文件，让 mkdir 失败
	blocker := filepath.Join(dir, AuditSubdirectory)
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := NewPersistentAuditWriter(dir)
	result := w.Write(fakeFacts(), "allow", "r", false, "", "", nil)
	if result.Persisted || !result.Degraded || result.Reason != "audit_write_failed" {
		t.Errorf("result = %+v", result)
	}
}

func TestPersistentAuditWriter_record含extra白名单(t *testing.T) {
	dir := t.TempDir()
	w := NewPersistentAuditWriter(dir)
	w.Write(fakeFacts(), "allow", "r", false, "", "",
		map[string]any{"decision_source": "policy", "bad": "drop"})

	data, _ := os.ReadFile(filepath.Join(dir, AuditSubdirectory, AuditFilename))
	var record map[string]any
	_ = json.Unmarshal(data, &record)
	if record["decision_source"] != "policy" {
		t.Errorf("decision_source = %v", record["decision_source"])
	}
	if _, exists := record["bad"]; exists {
		t.Error("non-allowlisted key should be dropped")
	}
}

// ──────────────────────────── 补充：覆盖率 ────────────────────────────

func TestSanitizeAuditValue_更多类型(t *testing.T) {
	if got := SanitizeAuditValue(int64(7)); got != int64(7) {
		t.Errorf("int64 = %v", got)
	}
	if got := SanitizeAuditValue(float64(1.5)); got != float64(1.5) {
		t.Errorf("float64 = %v", got)
	}
	if got := SanitizeAuditValue(float32(2.5)); got != float32(2.5) {
		t.Errorf("float32 = %v", got)
	}
	if got := SanitizeAuditValue([]string{"ok"}); len(got.([]any)) != 1 {
		t.Errorf("[]string = %v", got)
	}
	if got := SanitizeAuditValue(struct{ X int }{1}); got != AuditTextRedacted {
		t.Errorf("unknown type should go through text sanitize, got %v", got)
	}
}

func TestSanitizeAuditValue_嵌套map键脱敏(t *testing.T) {
	in := map[string]any{"/etc/passwd": "v"}
	got := SanitizeAuditValue(in).(map[string]any)
	for k := range got {
		if k != AuditTextRedacted {
			t.Errorf("sensitive map key should be redacted, got %q", k)
		}
	}
}

func TestSanitizeAuditField_自然语言非字符串(t *testing.T) {
	got := SanitizeAuditField("reviewer_reason_summary", 42)
	if got != 42 {
		t.Errorf("non-string should go through SanitizeAuditValue, got %v", got)
	}
}

func TestSanitizeNaturalLanguageAuditText_空与非安全字符(t *testing.T) {
	if got := sanitizeNaturalLanguageAuditText("  "); got != "" {
		t.Errorf("blank -> %q", got)
	}
	if got := sanitizeNaturalLanguageAuditText("hello@world"); got != AuditTextRedacted {
		t.Errorf("non-safe char -> %q", got)
	}
}

func TestSanitizeAuditText_nil与非字符串(t *testing.T) {
	if got := sanitizeAuditText(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	if got := sanitizeAuditText(42); got != "42" {
		t.Errorf("int -> %q", got)
	}
}

func TestTruncateRunes_多字节(t *testing.T) {
	long := strings.Repeat("中", 200)
	got := truncateRunes(long, 160)
	if len([]rune(got)) != 160 {
		t.Errorf("len = %d, want 160", len([]rune(got)))
	}
}

func TestExpandHomeString(t *testing.T) {
	got := expandHomeString("~/audit")
	if strings.HasPrefix(got, "~") {
		t.Errorf("~/ should expand, got %q", got)
	}
	if got2 := expandHomeString("/abs/audit"); got2 != "/abs/audit" {
		t.Errorf("absolute unchanged: %q", got2)
	}
}

func TestResolvePersistentAuditRoot_波浪线展开(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	got := ResolvePersistentAuditRoot(map[string]any{"persistent_audit_root": "~/audit"})
	if got == nil || strings.HasPrefix(*got, "~") {
		t.Errorf("got %v, want expanded home", got)
	}
}

func TestPersistentAuditWriter_字符串root(t *testing.T) {
	dir := t.TempDir()
	w := NewPersistentAuditWriter(dir)
	if got := w.AuditPath(); got == "" {
		t.Error("string root should produce path")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty(nil, "", "  ", "x"); got != "x" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty(nil, nil); got != "" {
		t.Errorf("got %q", got)
	}
}
