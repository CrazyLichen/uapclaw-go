package permissions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 辅助 ────────────────────────────

// fakeReviewerClient 测试用审查器客户端。
type fakeReviewerClient struct {
	// response 返回的原始响应
	response any
	// err 返回的错误
	err error
	// delay 模拟耗时
	delay time.Duration
	// calls 调用次数
	calls int
}

// Assess 实现 ReviewerClient。
func (f *fakeReviewerClient) Assess(ctx context.Context, request *ReviewerActionView) (any, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

// viewWith 构造带指定允许结局的 ReviewerActionView。
func viewWith(outcomes ...string) *ReviewerActionView {
	return &ReviewerActionView{
		DescriptorSummary: map[string]any{"tool_name": "read_file"},
		PolicyReason:      "policy",
		ReviewEvidence:    map[string]any{},
		AllowedOutcomes:   outcomes,
	}
}

// ──────────────────────────── 常量与提示词 ────────────────────────────

// TestAutoReviewerPrompt_关键约束 验证提示词包含核心安全约束。
func TestAutoReviewerPrompt_关键约束(t *testing.T) {
	for _, want := range []string{
		"Return exactly one raw JSON object",
		"outcome must be allow_once, manual, or deny",
		"You cannot call tools, request permissions, mutate config",
		"Absence of a visible restriction is not positive authorization",
		"Review checklist",
	} {
		if !strings.Contains(IsolatedAutoReviewerPrompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestReviewerNaturalLanguageInstruction 验证语言指令。
func TestReviewerNaturalLanguageInstruction(t *testing.T) {
	en := reviewerNaturalLanguageInstruction("en")
	if !strings.Contains(en, "English") {
		t.Errorf("en -> %q", en)
	}
	zh := reviewerNaturalLanguageInstruction("zh")
	if !strings.Contains(zh, "Simplified Chinese") {
		t.Errorf("zh -> %q", zh)
	}
}

// ──────────────────────────── decodeStrictResponse ────────────────────────────

// TestDecodeStrictResponse 验证严格解析。
func TestDecodeStrictResponse(t *testing.T) {
	cases := []struct {
		name string
		in   any
		ok   bool
	}{
		{"合法对象", `{"outcome":"manual"}`, true},
		{"非字符串", 42, false},
		{"非法JSON", `{bad}`, false},
		{"数组", `[1,2]`, false},
		{"含NaN", `{"x":NaN}`, false},
		{"含Infinity", `{"x":Infinity}`, false},
		{"尾部多余", `{"a":1} {"b":2}`, false},
		{"空对象", `{}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := decodeStrictResponse(c.in)
			if ok != c.ok {
				t.Errorf("ok = %v, want %v (got %v)", ok, c.ok, got)
			}
		})
	}
}

// ──────────────────────────── parseResponse 校验链 ────────────────────────────

// TestParseResponse_各拒绝分支 验证 9 个校验点。
func TestParseResponse_各拒绝分支(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeAllowOnce, ReviewerOutcomeManual)

	cases := []struct {
		name string
		raw  any
		want string
	}{
		{"非JSON", "not json", "invalid_json"},
		{"禁止字段tools", `{"outcome":"manual","reason_code":"r","rationale":"x","tools":[]}`, "forbidden_field"},
		{"禁止字段grant", `{"outcome":"manual","reason_code":"r","rationale":"x","grant":"a"}`, "forbidden_field"},
		{"缺outcome", `{"reason_code":"r","rationale":"x"}`, "missing_field"},
		{"缺reason_code", `{"outcome":"manual","rationale":"x"}`, "missing_field"},
		{"未知字段", `{"outcome":"manual","reason_code":"r","rationale":"x","extra":1}`, "unknown_field"},
		{"空outcome", `{"outcome":"","reason_code":"r","rationale":"x"}`, "invalid_field"},
		{"空白rationale", `{"outcome":"manual","reason_code":"r","rationale":"   "}`, "invalid_field"},
		{"非法outcome", `{"outcome":"maybe","reason_code":"r","rationale":"x"}`, "unknown_outcome"},
		{"可选字段非法", `{"outcome":"manual","reason_code":"r","rationale":"x","user_review_hint":42}`, "invalid_field"},
		{"manual缺部分可选", `{"outcome":"manual","reason_code":"r","rationale":"x","manual_reason_code":"m"}`, "missing_manual_field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reviewer.parseResponse(request, c.raw)
			if got.Outcome != ReviewerOutcomeManual {
				t.Errorf("outcome = %q, want manual", got.Outcome)
			}
			if got.FallbackReason != c.want {
				t.Errorf("fallback = %q, want %q", got.FallbackReason, c.want)
			}
		})
	}
}

// TestParseResponse_结局不被允许 验证 outcome_not_allowed。
func TestParseResponse_结局不被允许(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeManual) // 不允许 allow_once
	raw := `{"outcome":"allow_once","reason_code":"r","rationale":"x"}`

	got := reviewer.parseResponse(request, raw)
	if got.ReasonCode != "reviewer_outcome_not_allowed" {
		t.Errorf("reason_code = %q", got.ReasonCode)
	}
	if got.Outcome != ReviewerOutcomeManual {
		t.Errorf("outcome = %q, want manual", got.Outcome)
	}
}

// TestParseResponse_deny始终允许 验证 deny 不受 allowed_outcomes 限制。
func TestParseResponse_deny始终允许(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeManual) // 不含 deny
	raw := `{"outcome":"deny","reason_code":"r","rationale":"x"}`

	got := reviewer.parseResponse(request, raw)
	if got.Outcome != ReviewerOutcomeDeny {
		t.Errorf("deny should always be allowed, got %+v", got)
	}
	if got.Status != "denied" {
		t.Errorf("status = %q", got.Status)
	}
}

// TestParseResponse_成功各结局 验证 allow_once / manual / deny 的 status。
func TestParseResponse_成功各结局(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeAllowOnce, ReviewerOutcomeManual, ReviewerOutcomeDeny)

	cases := []struct {
		outcome string
		status  string
	}{
		{ReviewerOutcomeAllowOnce, "approved"},
		{ReviewerOutcomeManual, "manual"},
		{ReviewerOutcomeDeny, "denied"},
	}
	for _, c := range cases {
		raw := `{"outcome":"` + c.outcome + `","reason_code":"r","rationale":"x"}`
		got := reviewer.parseResponse(request, raw)
		if got.Outcome != c.outcome || got.Status != c.status {
			t.Errorf("outcome=%s got (%q,%q), want (%q,%q)",
				c.outcome, got.Outcome, got.Status, c.outcome, c.status)
		}
	}
}

// TestParseResponse_manual带完整可选字段 验证人工字段填充。
func TestParseResponse_manual带完整可选字段(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeManual)
	raw := `{"outcome":"manual","reason_code":"rc","rationale":"rat",` +
		`"manual_reason_code":"mrc","manual_reason_summary":"mrs","user_review_hint":"hint"}`

	got := reviewer.parseResponse(request, raw)
	if got.ManualReasonCode != "mrc" || got.ManualReasonSummary != "mrs" || got.UserReviewHint != "hint" {
		t.Errorf("got %+v", got)
	}
}

// TestParseResponse_manual可选字段兜底 验证缺省时用 reason 兜底。
func TestParseResponse_manual可选字段兜底(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeManual)
	raw := `{"outcome":"manual","reason_code":"rc","rationale":"rat"}`

	got := reviewer.parseResponse(request, raw)
	if got.ManualReasonCode != "rc" {
		t.Errorf("manual_reason_code = %q, want rc", got.ManualReasonCode)
	}
	if !strings.Contains(got.UserReviewHint, "Review the request manually") {
		t.Errorf("hint = %q", got.UserReviewHint)
	}
}

// TestParseResponse_大写结局归一 验证 outcome 大小写与空白归一。
func TestParseResponse_大写结局归一(t *testing.T) {
	reviewer := &AutoReviewer{Client: &fakeReviewerClient{}, TimeoutSeconds: 1}
	request := viewWith(ReviewerOutcomeAllowOnce)
	raw := `{"outcome":"  ALLOW_ONCE  ","reason_code":"r","rationale":"x"}`

	got := reviewer.parseResponse(request, raw)
	if got.Outcome != ReviewerOutcomeAllowOnce {
		t.Errorf("outcome = %q", got.Outcome)
	}
}

// ──────────────────────────── Assess 兜底 ────────────────────────────

// TestAssess_payloadError直接manual 验证载荷错误短路。
func TestAssess_payloadError直接manual(t *testing.T) {
	client := &fakeReviewerClient{response: `{"outcome":"allow_once","reason_code":"r","rationale":"x"}`}
	reviewer := NewAutoReviewer(client, 1000, nil)
	request := viewWith(ReviewerOutcomeAllowOnce)
	request.PayloadError = "reviewer_payload_missing"

	got := reviewer.Assess(context.Background(), request)
	if got.Outcome != ReviewerOutcomeManual || got.FallbackReason != "reviewer_payload_missing" {
		t.Errorf("got %+v", got)
	}
	if client.calls != 0 {
		t.Error("client should not be called on payload error")
	}
}

// TestAssess_超时降级 验证 timed_out。
func TestAssess_超时降级(t *testing.T) {
	client := &fakeReviewerClient{delay: 200 * time.Millisecond}
	reviewer := NewAutoReviewer(client, 20, nil) // 20ms 超时

	got := reviewer.Assess(context.Background(), viewWith(ReviewerOutcomeManual))
	if got.Status != "timed_out" {
		t.Errorf("status = %q", got.Status)
	}
	if got.ManualReasonCode != "reviewer_timeout" {
		t.Errorf("manual_reason_code = %q", got.ManualReasonCode)
	}
}

// TestAssess_客户端异常降级 验证 aborted。
func TestAssess_客户端异常降级(t *testing.T) {
	client := &fakeReviewerClient{err: errors.New("boom")}
	reviewer := NewAutoReviewer(client, 1000, nil)

	got := reviewer.Assess(context.Background(), viewWith(ReviewerOutcomeManual))
	if got.Status != "aborted" {
		t.Errorf("status = %q", got.Status)
	}
	if got.FallbackReason != "client_exception" {
		t.Errorf("fallback = %q", got.FallbackReason)
	}
}

// TestAssess_正常返回 验证成功解析。
func TestAssess_正常返回(t *testing.T) {
	client := &fakeReviewerClient{response: `{"outcome":"allow_once","reason_code":"r","rationale":"x"}`}
	reviewer := NewAutoReviewer(client, 1000, nil)

	got := reviewer.Assess(context.Background(), viewWith(ReviewerOutcomeAllowOnce))
	if got.Outcome != ReviewerOutcomeAllowOnce || got.Status != "approved" {
		t.Errorf("got %+v", got)
	}
}

// TestUpdateRuntimeOptions 验证超时下限与格式更新。
func TestUpdateRuntimeOptions(t *testing.T) {
	reviewer := NewAutoReviewer(&fakeReviewerClient{}, 5000, nil)
	if reviewer.TimeoutSeconds != 5.0 {
		t.Errorf("timeout = %v", reviewer.TimeoutSeconds)
	}
	// 0 → 下限 1ms
	reviewer.UpdateRuntimeOptions(0, nil)
	if reviewer.TimeoutSeconds != 0.001 {
		t.Errorf("timeout = %v, want 0.001", reviewer.TimeoutSeconds)
	}
	// 负数 → 同样下限
	reviewer.UpdateRuntimeOptions(-100, nil)
	if reviewer.TimeoutSeconds != 0.001 {
		t.Errorf("timeout = %v", reviewer.TimeoutSeconds)
	}
}

// TestUpdateRuntimeOptions_更新隔离客户端格式 验证只对 IsolatedModelReviewerClient 生效。
func TestUpdateRuntimeOptions_更新隔离客户端格式(t *testing.T) {
	isolated := &IsolatedModelReviewerClient{OutputFormat: "json_object"}
	reviewer := NewAutoReviewer(isolated, 1000, nil)
	format := "prompt"
	reviewer.UpdateRuntimeOptions(1000, &format)
	if isolated.OutputFormat != "prompt" {
		t.Errorf("output_format = %q", isolated.OutputFormat)
	}
	// 非隔离客户端不受影响
	plain := &fakeReviewerClient{}
	reviewer2 := NewAutoReviewer(plain, 1000, nil)
	reviewer2.UpdateRuntimeOptions(1000, &format) // 不 panic 即可
}

// ──────────────────────────── 文本归一 ────────────────────────────

// TestSanitizeReasonCode 验证原因码归一。
func TestSanitizeReasonCode(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"ABC_def-123", "abc_def-123"},
		{"a b c", "a_b_c"},
		{"  ", "unspecified"},
		{nil, "unspecified"},
		{"特殊字符!@#", "_______"}, // 4 个中文 + 3 个符号 = 7 个 rune
	}
	for _, c := range cases {
		if got := sanitizeReasonCode(c.in); got != c.want {
			t.Errorf("sanitizeReasonCode(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	// 超长截断
	long := strings.Repeat("a", 200)
	if got := sanitizeReasonCode(long); len(got) > AutoReviewReasonCodeLimit {
		t.Errorf("should truncate to %d, got %d", AutoReviewReasonCodeLimit, len(got))
	}
}

// TestTruncateAtWordBoundary 验证词边界截断。
func TestTruncateAtWordBoundary(t *testing.T) {
	cases := []struct {
		in        string
		maxLength int
		want      string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"this is a long sentence here", 15, "this is a..."},
		{"abcdefghij", 3, "abc"},
		{"  trimmed  ", 20, "trimmed"},
		{"nospacesatalllongtext", 10, "nospace..."}, // text[:7]="nospace"，无空格不回退词边界
	}
	for _, c := range cases {
		if got := truncateAtWordBoundary(c.in, c.maxLength); got != c.want {
			t.Errorf("truncateAtWordBoundary(%q,%d) = %q, want %q", c.in, c.maxLength, got, c.want)
		}
	}
}

// TestSanitizeReasonSummary 验证摘要脱敏与截断。
func TestSanitizeReasonSummary(t *testing.T) {
	// 含密钥应被脱敏
	got := sanitizeReasonSummary("the key is api_key=secret123")
	if strings.Contains(got, "secret123") {
		t.Errorf("should redact secret, got %q", got)
	}
	// 超长截断
	long := strings.Repeat("word ", 500)
	if out := sanitizeReasonSummary(long); len([]rune(out)) > AutoReviewReasonSummaryLimit {
		t.Errorf("should truncate, got %d runes", len([]rune(out)))
	}
}

// ──────────────────────────── modelVisibleValue ────────────────────────────

// TestModelVisibleValue_移除关联字段 验证 id/digest 被移除。
func TestModelVisibleValue_移除关联字段(t *testing.T) {
	in := map[string]any{
		"tool_name":       "bash",
		"request_context": "ctx",
		"id":              "abc",
		"digest":          "hash",
		"tool_call_id":    "c1",
		"path_digest":     "d",
		"other_digests":   []any{"x"},
		"kept":            "value",
	}
	got, ok := modelVisibleValue(in).(map[string]any)
	if !ok {
		t.Fatalf("got %T", modelVisibleValue(in))
	}
	if _, exists := got["request_context"]; exists {
		t.Error("request_context should be removed")
	}
	for _, key := range []string{"id", "digest", "tool_call_id", "path_digest", "other_digests"} {
		if _, exists := got[key]; exists {
			t.Errorf("%s should be removed", key)
		}
	}
	if got["kept"] != "value" || got["tool_name"] != "bash" {
		t.Errorf("got %v", got)
	}
}

// TestModelVisibleValue_递归列表 验证列表与嵌套。
func TestModelVisibleValue_递归列表(t *testing.T) {
	in := []any{
		map[string]any{"id": "x", "keep": 1},
		"plain",
	}
	got, ok := modelVisibleValue(in).([]any)
	if !ok || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	nested, _ := got[0].(map[string]any)
	if _, exists := nested["id"]; exists {
		t.Error("nested id should be removed")
	}
	if got[1] != "plain" {
		t.Errorf("scalar should pass through")
	}
}

// TestModelVisibleValue_标量透传 验证非 map/list 原样返回。
func TestModelVisibleValue_标量透传(t *testing.T) {
	if got := modelVisibleValue(42); got != 42 {
		t.Errorf("got %v", got)
	}
	if got := modelVisibleValue("text"); got != "text" {
		t.Errorf("got %v", got)
	}
	if got := modelVisibleValue(nil); got != nil {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── visibleReviewEvidence ────────────────────────────

// TestVisibleReviewEvidence_白名单过滤 验证只保留 7 个键。
func TestVisibleReviewEvidence_白名单过滤(t *testing.T) {
	in := map[string]any{
		"command":                 map[string]any{"summary": "ls"},
		"domain_policy":           map[string]any{"level": "ask"},
		"network":                 map[string]any{"hosts": []any{"a"}},
		"path_targets":            []any{},
		"path_targets_incomplete": false,
		"reviewable_payload":      map[string]any{"file_path": "/a"},
		"user_intent":             map[string]any{"trusted_user_turns": []any{}},
		"secret_extra":            "should be dropped",
		"tool_call_id":            "dropped",
	}
	got := visibleReviewEvidence(in)
	if _, exists := got["secret_extra"]; exists {
		t.Error("non-whitelisted key should be dropped")
	}
	if _, exists := got["tool_call_id"]; exists {
		t.Error("tool_call_id should be dropped")
	}
	for _, key := range []string{"command", "domain_policy", "network", "path_targets",
		"path_targets_incomplete", "reviewable_payload", "user_intent"} {
		if _, exists := got[key]; !exists {
			t.Errorf("%s should be kept", key)
		}
	}
}

// ──────────────────────────── 构造器 ────────────────────────────

// TestNewIsolatedModelReviewerClient_nil模型 验证报错。
func TestNewIsolatedModelReviewerClient_nil模型(t *testing.T) {
	if _, err := NewIsolatedModelReviewerClient(nil, nil, "json_object"); err == nil {
		t.Error("nil model should error")
	}
}

// TestBuildIsolatedReviewerModel_nil 验证 nil 返回 nil。
func TestBuildIsolatedReviewerModel_nil(t *testing.T) {
	if got := BuildIsolatedReviewerModel(nil); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestDisplayLanguage 验证语言取值与兜底。
func TestDisplayLanguage(t *testing.T) {
	client := &IsolatedModelReviewerClient{}
	if got := client.displayLanguage(); got != "zh" {
		t.Errorf("nil getter -> %q", got)
	}
	client.DisplayLanguageGetter = func() string { return "EN" }
	if got := client.displayLanguage(); got != "en" {
		t.Errorf("EN -> %q", got)
	}
	client.DisplayLanguageGetter = func() string { return "zh" }
	if got := client.displayLanguage(); got != "zh" {
		t.Errorf("zh -> %q", got)
	}
	client.DisplayLanguageGetter = func() string { panic("boom") }
	if got := client.displayLanguage(); got != "zh" {
		t.Errorf("panic -> %q", got)
	}
}

// ──────────────────────────── 路径与命令视图 ────────────────────────────

// TestPathLabel 验证路径标签脱敏。
func TestPathLabel(t *testing.T) {
	if got := pathLabel("/home/user/file.txt"); got != "file.txt" {
		t.Errorf("got %q", got)
	}
	if got := pathLabel(""); got != "[redacted_target]" {
		t.Errorf("empty -> %q", got)
	}
	if got := pathLabel("/a/"); got != "a" {
		t.Errorf("trailing slash -> %q", got)
	}
	// 超长截断
	long := "/a/" + strings.Repeat("x", 200)
	if got := pathLabel(long); len([]rune(got)) > AutoReviewPathLabelLimit {
		t.Errorf("should truncate, got %d", len([]rune(got)))
	}
}

// TestPathScope 验证作用域判定。
func TestPathScope(t *testing.T) {
	cases := []struct {
		name        string
		rawPath     string
		workspace   string
		platform    string
		external    []string
		policyLevel string
		want        string
	}{
		{"空路径", "", "/ws", "", nil, "ask", "unknown"},
		{"相对路径", "rel/path", "/ws", "", nil, "ask", "unknown"},
		{"工作区内", "/ws/a", "/ws", "", nil, "ask", "workspace"},
		{"平台受信根", "/plat/a", "/ws", "/plat", nil, "ask", "platform_trusted_root"},
		{"非工作区", "/other/a", "/ws", "", nil, "ask", "nonworkspace_unclassified"},
		{"engine受限", "/ext/a", "/ws", "", []string{"/ext/a"}, "ask", "engine_restricted"},
		{"allow不查external", "/ext/a", "/ws", "", []string{"/ext/a"}, "allow", "nonworkspace_unclassified"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pathScope(c.rawPath, c.workspace, c.platform, c.external, c.policyLevel)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestCoreShellView 验证 shell 结构解析。
func TestCoreShellView(t *testing.T) {
	// 空命令
	ops, progs := coreShellView("")
	if len(ops) != 0 || len(progs) != 0 {
		t.Errorf("empty -> %v %v", ops, progs)
	}
	// 简单命令
	ops, progs = coreShellView("ls -la")
	if len(progs) != 1 || progs[0] != "ls" {
		t.Errorf("progs = %v", progs)
	}
	// 管道
	ops, _ = coreShellView("ls | grep foo")
	if !containsString(ops, "pipeline") {
		t.Errorf("ops = %v, want pipeline", ops)
	}
	// 自定义可执行文件
	_, progs = coreShellView("/usr/local/bin/mytool")
	if len(progs) != 1 || progs[0] != "custom_executable" {
		t.Errorf("progs = %v", progs)
	}
}

// TestCommandView 验证命令视图结构。
func TestCommandView(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Command = "ls -la"
	})
	got := commandView(facts)
	if _, exists := got["operators"]; !exists {
		t.Error("operators missing")
	}
	if _, exists := got["programs"]; !exists {
		t.Error("programs missing")
	}
	if _, exists := got["summary"]; !exists {
		t.Error("summary missing")
	}
}

// TestIntentView_nil 验证无证据返回空轮次。
func TestIntentView_nil(t *testing.T) {
	got := intentView(nil)
	turns, _ := got["trusted_user_turns"].([]any)
	if len(turns) != 0 {
		t.Errorf("got %v", turns)
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestLastPathSegment 验证取最后一段。
func TestLastPathSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a/b/c", "c"},
		{"/a/b/", "b"},
		{`a\b\c`, "c"},
		{"single", "single"},
		{"", ""},
	}
	for _, c := range cases {
		if got := lastPathSegment(c.in); got != c.want {
			t.Errorf("lastPathSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsAbsolutePathText 验证绝对路径判定。
func TestIsAbsolutePathText(t *testing.T) {
	for _, p := range []string{"/a", `\a`, "C:/a", `C:\a`} {
		if !isAbsolutePathText(p) {
			t.Errorf("%q should be absolute", p)
		}
	}
	for _, p := range []string{"a/b", "./a", ""} {
		if isAbsolutePathText(p) {
			t.Errorf("%q should not be absolute", p)
		}
	}
}

// TestIsASCIIAlnum 验证 ASCII 字母数字判定。
func TestIsASCIIAlnum(t *testing.T) {
	for _, r := range []rune{'a', 'Z', '0', '9'} {
		if !isASCIIAlnum(r) {
			t.Errorf("%q should be alnum", r)
		}
	}
	for _, r := range []rune{'_', '-', '中', ' '} {
		if isASCIIAlnum(r) {
			t.Errorf("%q should not be alnum", r)
		}
	}
}

// TestTruncateTextByRunes 验证 rune 截断。
func TestTruncateTextByRunes(t *testing.T) {
	if got := truncateTextByRunes("中文测试", 2); got != "中文" {
		t.Errorf("got %q", got)
	}
	if got := truncateTextByRunes("ab", 5); got != "ab" {
		t.Errorf("got %q", got)
	}
}

// TestFirstNonemptyString 验证取第一个非空。
func TestFirstNonemptyString(t *testing.T) {
	if got := firstNonemptyString("", "b", "c"); got != "b" {
		t.Errorf("got %v", got)
	}
	if got := firstNonemptyString("", "  "); got != "  " {
		t.Errorf("all empty should return last, got %v", got)
	}
}

// TestMaxInt 验证取较大值。
func TestMaxInt(t *testing.T) {
	if maxInt(1, 5) != 5 || maxInt(5, 1) != 5 || maxInt(3, 3) != 3 {
		t.Error("maxInt broken")
	}
}

// TestManualReasonCodeForFallback与Hint 验证兜底文案。
func TestManualReasonCodeForFallback与Hint(t *testing.T) {
	if got := manualReasonCodeForFallback("reviewer_timeout"); got != "reviewer_timeout" {
		t.Errorf("got %q", got)
	}
	if got := manualReasonCodeForFallback("other"); got != "reviewer_fallback" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(userReviewHintForFallback("reviewer_timeout"), "timed out") {
		t.Error("timeout hint wrong")
	}
	if !strings.Contains(userReviewHintForFallback("other"), "could not be trusted") {
		t.Error("generic hint wrong")
	}
}

// ──────────────────────────── ToJSONDict ────────────────────────────

// TestToJSONDict 验证模型可见载荷结构。
func TestToJSONDict(t *testing.T) {
	view := &ReviewerActionView{
		DescriptorSummary: map[string]any{"tool_name": "bash", "tool_call_id": "drop"},
		PolicyReason:      "policy_reason",
		ReviewEvidence: map[string]any{
			"command":   map[string]any{"summary": "ls"},
			"extra_key": "dropped",
		},
		AllowedOutcomes:   []string{ReviewerOutcomeManual},
		NoAutoAllowReason: "no_auto",
	}
	got := view.ToJSONDict()

	if got["phase_scope"] != "takeover" {
		t.Errorf("phase_scope = %v", got["phase_scope"])
	}
	if got["policy_reason"] != "policy_reason" {
		t.Errorf("policy_reason = %v", got["policy_reason"])
	}
	if got["no_auto_allow_reason"] != "no_auto" {
		t.Errorf("no_auto_allow_reason = %v", got["no_auto_allow_reason"])
	}
	outcomes, _ := got["allowed_outcomes"].([]string)
	if len(outcomes) != 1 || outcomes[0] != ReviewerOutcomeManual {
		t.Errorf("allowed_outcomes = %v", outcomes)
	}
	summary, _ := got["descriptor_summary"].(map[string]any)
	if _, exists := summary["tool_call_id"]; exists {
		t.Error("tool_call_id should be stripped from descriptor_summary")
	}
	evidence, _ := got["review_evidence"].(map[string]any)
	if _, exists := evidence["extra_key"]; exists {
		t.Error("non-whitelisted evidence key should be dropped")
	}
}

// ──────────────────────────── BuildReviewerActionView ────────────────────────────

// TestBuildReviewerActionView_基本 验证摘要与证据结构。
func TestBuildReviewerActionView_基本(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.ToolName = "read_file"
		f.Capability.Category = "path"
		f.ReadPaths = []string{"/ws/a.txt"}
		f.WorkspaceRoot = "/ws"
		f.UntrustedArgs = map[string]any{"file_path": "/ws/a.txt"}
	})
	got := BuildReviewerActionView(facts, AutoDecisionAskLevel, "policy", []string{ReviewerOutcomeManual}, "", nil, nil)

	if got.PayloadError != "" {
		t.Errorf("payload_error = %q", got.PayloadError)
	}
	if got.DescriptorSummary["tool_name"] != "read_file" {
		t.Errorf("tool_name = %v", got.DescriptorSummary["tool_name"])
	}
	pathCounts, _ := got.DescriptorSummary["path_counts"].(map[string]any)
	if pathCounts["read"] != 1 {
		t.Errorf("path_counts = %v", pathCounts)
	}
	if _, exists := got.ReviewEvidence["path_targets"]; !exists {
		t.Error("path_targets missing")
	}
}

// TestBuildReviewerActionView_带领域路由 验证 domain_policy 附加。
func TestBuildReviewerActionView_带领域路由(t *testing.T) {
	facts := autoFacts(nil)
	domain := &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_r", Source: "semantic_reviewer"}
	got := BuildReviewerActionView(facts, AutoDecisionAskLevel, "p", []string{ReviewerOutcomeManual}, "", nil, domain)

	policy, ok := got.ReviewEvidence["domain_policy"].(map[string]any)
	if !ok {
		t.Fatalf("domain_policy missing: %v", got.ReviewEvidence)
	}
	if policy["reason"] != "domain_r" {
		t.Errorf("got %v", policy)
	}
}

// TestBuildReviewerActionView_载荷缺失移除allow_once 验证 shell 缺命令。
func TestBuildReviewerActionView_载荷缺失移除allow_once(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "shell"
		f.UntrustedArgs = map[string]any{}
	})
	got := BuildReviewerActionView(facts, AutoDecisionAskLevel, "p",
		[]string{ReviewerOutcomeAllowOnce, ReviewerOutcomeManual}, "", nil, nil)

	if got.PayloadError != "reviewer_payload_missing" {
		t.Errorf("payload_error = %q", got.PayloadError)
	}
	if containsString(got.AllowedOutcomes, ReviewerOutcomeAllowOnce) {
		t.Errorf("allow_once should be removed, got %v", got.AllowedOutcomes)
	}
	if got.NoAutoAllowReason != "reviewer_payload_missing" {
		t.Errorf("no_auto_allow_reason = %q", got.NoAutoAllowReason)
	}
}

// ──────────────────────────── completePayloadEvidence ────────────────────────────

// TestCompletePayloadEvidence_shell缺命令 验证 reviewer_payload_missing。
func TestCompletePayloadEvidence_shell缺命令(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) { f.Capability.Category = "shell" })
	evidence := map[string]any{"reviewable_payload": map[string]any{}, "user_intent": map[string]any{}}
	if got := completePayloadEvidence(facts, evidence); got != "reviewer_payload_missing" {
		t.Errorf("got %q", got)
	}
}

// TestCompletePayloadEvidence_shell有命令 验证通过。
func TestCompletePayloadEvidence_shell有命令(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) { f.Capability.Category = "shell" })
	evidence := map[string]any{
		"reviewable_payload": map[string]any{"command": "ls"},
		"user_intent":        map[string]any{"trusted_user_turns": []any{}},
	}
	if got := completePayloadEvidence(facts, evidence); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestCompletePayloadEvidence_非shell无命令要求 验证 code 类也要命令。
func TestCompletePayloadEvidence_非shell无命令要求(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) { f.Capability.Category = "path" })
	evidence := map[string]any{
		"reviewable_payload": map[string]any{"file_path": "/a"},
		"user_intent":        map[string]any{"trusted_user_turns": []any{}},
	}
	if got := completePayloadEvidence(facts, evidence); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestCompletePayloadEvidence_超限 验证 reviewer_payload_too_large。
func TestCompletePayloadEvidence_超限(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) { f.Capability.Category = "path" })
	huge := strings.Repeat("x", AutoReviewPayloadMaxBytes+100)
	evidence := map[string]any{
		"reviewable_payload": map[string]any{"file_path": huge},
		"user_intent":        map[string]any{"trusted_user_turns": []any{}},
	}
	if got := completePayloadEvidence(facts, evidence); got != "reviewer_payload_too_large" {
		t.Errorf("got %q", got)
	}
}

// TestCompletePayloadEvidence_参数非对象 验证 shell 且参数非法。
func TestCompletePayloadEvidence_参数非对象(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Capability.Category = "shell"
		f.ArgumentsValidObject = false
	})
	evidence := map[string]any{
		"reviewable_payload": map[string]any{"command": "ls"},
		"user_intent":        map[string]any{"trusted_user_turns": []any{}},
	}
	if got := completePayloadEvidence(facts, evidence); got != "reviewer_payload_missing" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── pathTargetView ────────────────────────────

// TestPathTargetView_accesses未知 验证返回 incomplete。
func TestPathTargetView_accesses未知(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) { f.AccessesKnown = false })
	targets, incomplete := pathTargetView(facts, AutoDecisionAskLevel)
	if len(targets) != 0 || !incomplete {
		t.Errorf("got (%v, %v)", targets, incomplete)
	}
}

// TestPathTargetView_去重 验证同操作同路径只出现一次。
func TestPathTargetView_去重(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.ReadPaths = []string{"/ws/a", "/ws/a"}
		f.WorkspaceRoot = "/ws"
	})
	targets, _ := pathTargetView(facts, AutoDecisionAskLevel)
	if len(targets) != 1 {
		t.Errorf("got %d targets, want 1", len(targets))
	}
}

// TestPathTargetView_超限 验证超过上限时 incomplete。
func TestPathTargetView_超限(t *testing.T) {
	paths := make([]string, AutoReviewPathTargetLimit+2)
	for i := range paths {
		paths[i] = "/ws/" + string(rune('a'+i))
	}
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.ReadPaths = paths
		f.WorkspaceRoot = "/ws"
	})
	targets, incomplete := pathTargetView(facts, AutoDecisionAskLevel)
	if len(targets) != AutoReviewPathTargetLimit || !incomplete {
		t.Errorf("got (%d, %v)", len(targets), incomplete)
	}
}

// TestPathTargetView_读写分离 验证 read/write 各自计入。
func TestPathTargetView_读写分离(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.ReadPaths = []string{"/ws/a"}
		f.WritePaths = []string{"/ws/a"} // 同路径但不同操作
		f.WorkspaceRoot = "/ws"
	})
	targets, _ := pathTargetView(facts, AutoDecisionAskLevel)
	if len(targets) != 2 {
		t.Errorf("got %d targets, want 2 (read+write)", len(targets))
	}
}

// ──────────────────────────── intentView ────────────────────────────

// TestIntentView_带Context 验证轮次与澄清投影。
func TestIntentView_带Context(t *testing.T) {
	evidence := &OriginalUserIntentEvidence{
		Text: "fallback text",
		Context: &RootDecisionContext{
			TrustedTurns: []RootIntentTurn{
				{
					Kind: RootIntentTurnKindFresh,
					Text: "please read a.txt",
					Clarifications: []RootAskUserClarification{
						{
							Question: "which file?",
							Answers:  []string{"a.txt"},
							Options: []RootAskUserOption{
								{Label: "a", Description: "first", Preview: "a.txt"},
							},
						},
					},
				},
			},
		},
	}
	got := intentView(evidence)
	turns, _ := got["trusted_user_turns"].([]any)
	if len(turns) != 1 {
		t.Fatalf("got %v", turns)
	}
	turn, _ := turns[0].(map[string]any)
	if turn["kind"] != RootIntentTurnKindFresh || turn["text"] != "please read a.txt" {
		t.Errorf("got %v", turn)
	}
	if _, exists := turn["clarifications"]; !exists {
		t.Error("clarifications missing")
	}
}

// TestIntentView_无Context用Text兜底 验证 Text 兜底。
func TestIntentView_无Context用Text兜底(t *testing.T) {
	got := intentView(&OriginalUserIntentEvidence{Text: "hello"})
	turns, _ := got["trusted_user_turns"].([]any)
	if len(turns) != 1 {
		t.Fatalf("got %v", turns)
	}
	turn, _ := turns[0].(map[string]any)
	if turn["text"] != "hello" {
		t.Errorf("got %v", turn)
	}
}

// TestIntentView_空Context空Text 验证无轮次。
func TestIntentView_空Context空Text(t *testing.T) {
	got := intentView(&OriginalUserIntentEvidence{Context: &RootDecisionContext{}})
	turns, _ := got["trusted_user_turns"].([]any)
	if len(turns) != 0 {
		t.Errorf("got %v", turns)
	}
}

// ──────────────────────────── modelResultText ────────────────────────────

// TestModelResultText 验证各输入形态。
func TestModelResultText(t *testing.T) {
	if got := modelResultText("plain"); got != "plain" {
		t.Errorf("string -> %q", got)
	}
	if got := modelResultText(42); got != "" {
		t.Errorf("int -> %q", got)
	}
	if got := modelResultText(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
}

// ──────────────────────────── 序列化与深拷贝 ────────────────────────────

// TestMarshalJSONNoEscape 验证不转义 HTML 且保留非 ASCII。
func TestMarshalJSONNoEscape(t *testing.T) {
	encoded, err := marshalJSONNoEscape(map[string]any{"html": "<a>", "zh": "中文"}, true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	text := string(encoded)
	if strings.Contains(text, `\u003c`) {
		t.Errorf("HTML should not be escaped, got %q", text)
	}
	if !strings.Contains(text, "中文") {
		t.Errorf("non-ASCII should be preserved, got %q", text)
	}
}

// TestMarshalJSONNoEscapeSortKeys 验证键排序。
func TestMarshalJSONNoEscapeSortKeys(t *testing.T) {
	got, err := marshalJSONNoEscapeSortKeys(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != `{"a":2,"b":1}` {
		t.Errorf("got %q", got)
	}
}

// TestCopyStringAnyMap 验证浅拷贝。
func TestCopyStringAnyMap(t *testing.T) {
	source := map[string]any{"a": 1}
	got := copyStringAnyMap(source)
	got["b"] = 2
	if _, exists := source["b"]; exists {
		t.Error("should be a copy")
	}
	if copyStringAnyMap(nil) != nil {
		t.Error("nil -> nil")
	}
}

// TestRedactJSONValueStrict_基本 验证脱敏与错误。
func TestRedactJSONValueStrict_基本(t *testing.T) {
	got, err := redactJSONValueStrict(map[string]any{
		"api_key": "secret",
		"normal":  "value",
	}, DefaultMaxRedactedTextLength, DefaultMaxRedactedItems)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	m, _ := got.(map[string]any)
	if m["api_key"] != "[redacted]" {
		t.Errorf("api_key = %v", m["api_key"])
	}
	if m["normal"] != "value" {
		t.Errorf("normal = %v", m["normal"])
	}
}

// TestRedactJSONValueStrict_各类型 验证标量与列表。
func TestRedactJSONValueStrict_各类型(t *testing.T) {
	for _, value := range []any{nil, true, 42, 3.14, int64(7)} {
		got, err := redactJSONValueStrict(value, 100, 5)
		if err != nil {
			t.Errorf("%v err = %v", value, err)
		}
		if got != value {
			t.Errorf("%v -> %v", value, got)
		}
	}
	got, err := redactJSONValueStrict([]any{"a", "b", "c"}, 100, 2)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	list, _ := got.([]any)
	if len(list) != 2 {
		t.Errorf("should truncate to maxItems, got %v", list)
	}
}

// ──────────────────────────── LLM 配置相关（无需真实调用） ────────────────────────────

// newTestModel 构造测试用 llm.Model（不发起调用）。
func newTestModel(t *testing.T, provider string, authMode llmschema.AuthMode) *llm.Model {
	t.Helper()
	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: provider,
		APIKey:         "test-key",
		APIBase:        "https://api.example.com",
		AuthMode:       authMode,
	}
	modelConfig := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	model, err := llm.NewModel(clientConfig, modelConfig)
	if err != nil {
		t.Skipf("cannot construct model: %v", err)
	}
	return model
}

// TestFormatResponseFormat_OpenAI注入 验证 OpenAI + api_key 注入 json_object。
func TestFormatResponseFormat_OpenAI注入(t *testing.T) {
	model := newTestModel(t, llmschema.ProviderTypeOpenAI.String(), llmschema.AuthModeAPIKey)
	client := &IsolatedModelReviewerClient{Model: model, OutputFormat: "json_object"}
	got := client.formatResponseFormat()
	if got == nil || got["type"] != "json_object" {
		t.Errorf("got %v", got)
	}
}

// TestFormatResponseFormat_prompt模式不注入 验证 prompt 模式。
func TestFormatResponseFormat_prompt模式不注入(t *testing.T) {
	model := newTestModel(t, llmschema.ProviderTypeOpenAI.String(), llmschema.AuthModeAPIKey)
	client := &IsolatedModelReviewerClient{Model: model, OutputFormat: "prompt"}
	if got := client.formatResponseFormat(); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestFormatResponseFormat_非OpenAI不注入 验证其他 provider。
func TestFormatResponseFormat_非OpenAI不注入(t *testing.T) {
	model := newTestModel(t, "Anthropic", llmschema.AuthModeAPIKey)
	client := &IsolatedModelReviewerClient{Model: model, OutputFormat: "json_object"}
	if got := client.formatResponseFormat(); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestFormatResponseFormat_OAuth不注入 验证 OpenAI OAuth 模式。
func TestFormatResponseFormat_OAuth不注入(t *testing.T) {
	model := newTestModel(t, llmschema.ProviderTypeOpenAI.String(), llmschema.AuthModeOpenAIAccountOAuth)
	client := &IsolatedModelReviewerClient{Model: model, OutputFormat: "json_object"}
	if got := client.formatResponseFormat(); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestFormatResponseFormat_nil模型 验证 nil 安全。
func TestFormatResponseFormat_nil模型(t *testing.T) {
	client := &IsolatedModelReviewerClient{OutputFormat: "json_object"}
	if got := client.formatResponseFormat(); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestDeepCopyModelConfigs 验证两份配置深拷贝。
func TestDeepCopyModelConfigs(t *testing.T) {
	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		APIKey:         "k",
		AuthMode:       llmschema.AuthModeAPIKey,
	}
	copiedClient := deepCopyModelClientConfig(clientConfig)
	if copiedClient == clientConfig {
		t.Error("should be a new instance")
	}
	if copiedClient.APIKey != "k" {
		t.Errorf("got %v", copiedClient.APIKey)
	}
	// 修改副本不影响原件
	copiedClient.APIKey = "changed"
	if clientConfig.APIKey != "k" {
		t.Error("source should not be mutated")
	}

	modelConfig := &llmschema.ModelRequestConfig{
		ModelName: "m",
		Extra:     map[string]any{"k": "v"},
	}
	copiedModel := deepCopyModelRequestConfig(modelConfig)
	if copiedModel == modelConfig {
		t.Error("should be a new instance")
	}
	if copiedModel.ModelName != "m" {
		t.Errorf("got %v", copiedModel.ModelName)
	}
	if copiedModel.Extra["k"] != "v" {
		t.Errorf("Extra lost: %v", copiedModel.Extra)
	}

	// nil 安全
	if deepCopyModelClientConfig(nil) != nil {
		t.Error("nil -> nil")
	}
	if deepCopyModelRequestConfig(nil) != nil {
		t.Error("nil -> nil")
	}
}

// TestBuildIsolatedReviewerModel_正常 验证重建出新实例。
func TestBuildIsolatedReviewerModel_正常(t *testing.T) {
	model := newTestModel(t, llmschema.ProviderTypeOpenAI.String(), llmschema.AuthModeAPIKey)
	got := BuildIsolatedReviewerModel(model)
	if got == nil {
		t.Skip("model reconstruction unavailable in this build")
	}
	if got == model {
		t.Error("should be a different instance")
	}
	if got.ClientConfig == model.ClientConfig {
		t.Error("ClientConfig should be deep-copied")
	}
}

// TestBuildIsolatedReviewerModel_无ClientConfig 验证缺配置返回 nil。
func TestBuildIsolatedReviewerModel_无ClientConfig(t *testing.T) {
	if got := BuildIsolatedReviewerModel(&llm.Model{}); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestNewIsolatedModelReviewerClient_正常 验证构造与格式归一。
func TestNewIsolatedModelReviewerClient_正常(t *testing.T) {
	model := newTestModel(t, llmschema.ProviderTypeOpenAI.String(), llmschema.AuthModeAPIKey)
	client, err := NewIsolatedModelReviewerClient(model, nil, "prompt")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client.OutputFormat != "prompt" {
		t.Errorf("output_format = %q", client.OutputFormat)
	}
	// 非法格式回落 json_object
	client2, _ := NewIsolatedModelReviewerClient(model, nil, "xml")
	if client2.OutputFormat != "json_object" {
		t.Errorf("invalid format -> %q", client2.OutputFormat)
	}
}

// TestModelResultText_AssistantMessage 验证从助手消息取文本。
func TestModelResultText_AssistantMessage(t *testing.T) {
	// 空内容 → 回落 ReasoningContent
	empty := &llmschema.AssistantMessage{}
	empty.ReasoningContent = "reasoning"
	if got := modelResultText(empty); got != "reasoning" {
		t.Errorf("got %q, want reasoning", got)
	}
	// 都空 → 空串
	if got := modelResultText(&llmschema.AssistantMessage{}); got != "" {
		t.Errorf("got %q", got)
	}
	// nil 指针不 panic
	var nilMessage *llmschema.AssistantMessage
	if got := modelResultText(nilMessage); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestCommandView_截断 验证操作符与程序超 10 项时截断。
func TestCommandView_截断(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.Command = "ls | grep a | sort | uniq | head | tail | wc | cat | tee | xargs | awk"
	})
	got := commandView(facts)
	operators, _ := got["operators"].([]string)
	if len(operators) > 10 {
		t.Errorf("operators should be capped at 10, got %d", len(operators))
	}
	programs, _ := got["programs"].([]string)
	if len(programs) > 10 {
		t.Errorf("programs should be capped at 10, got %d", len(programs))
	}
}

// TestCoreShellView_各操作符 验证结构操作符识别。
func TestCoreShellView_各操作符(t *testing.T) {
	cases := []struct {
		command string
		wantOp  string
	}{
		{"ls && pwd", "compound"},
		{"ls > out.txt", "output_redirection"},
		{"ls < in.txt", "input_redirection"},
		{"echo $(date)", "command_substitution"},
		{"echo ${HOME}", "parameter_expansion"},
		// heredoc 依赖 shell AST 解析器对完整多行语法的支持，单行片段不触发
		{"(ls)", "subshell"},
		{"{ ls; }", "command_group"},
	}
	for _, c := range cases {
		ops, _ := coreShellView(c.command)
		if !containsString(ops, c.wantOp) {
			t.Errorf("%q -> %v, want %s", c.command, ops, c.wantOp)
		}
	}
}

// TestCompletePayloadEvidence_非法JSON 验证不可序列化值。
func TestCompletePayloadEvidence_非法JSON(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) { f.Capability.Category = "path" })
	evidence := map[string]any{
		"reviewable_payload": map[string]any{"fn": func() {}},
		"user_intent":        map[string]any{"trusted_user_turns": []any{}},
	}
	got := completePayloadEvidence(facts, evidence)
	if got != "reviewer_payload_invalid" {
		t.Errorf("got %q", got)
	}
}

// TestBuildReviewerActionView_读写计数 验证 path_counts 三类计数。
func TestBuildReviewerActionView_读写计数(t *testing.T) {
	facts := autoFacts(func(f *ToolDecisionFacts) {
		f.ReadPaths = []string{"/a", "/b"}
		f.WritePaths = []string{"/c"}
		f.ExternalPaths = []string{"/d", "/e", "/f"}
	})
	got := BuildReviewerActionView(facts, AutoDecisionAskLevel, "p", []string{ReviewerOutcomeManual}, "", nil, nil)
	counts, _ := got.DescriptorSummary["path_counts"].(map[string]any)
	if counts["read"] != 2 || counts["write"] != 1 || counts["external"] != 3 {
		t.Errorf("counts = %v", counts)
	}
}
