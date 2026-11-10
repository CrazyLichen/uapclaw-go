package permissions

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

// ──────────────────────────── 常量 ────────────────────────────

// TestReviewerRedaction_常量值 验证 9.90d-1 提前落地的常量与 Python 一致
func TestReviewerRedaction_常量值(t *testing.T) {
	if DefaultMaxRedactedTextLength != 240 {
		t.Errorf("DefaultMaxRedactedTextLength = %d, want 240", DefaultMaxRedactedTextLength)
	}
	if DefaultMaxRedactedItems != 5 {
		t.Errorf("DefaultMaxRedactedItems = %d, want 5", DefaultMaxRedactedItems)
	}
	if ReviewablePayloadTextLimit != 1024 {
		t.Errorf("ReviewablePayloadTextLimit = %d, want 1024", ReviewablePayloadTextLimit)
	}
	if PermissionUIPayloadMaxDepth != 6 || PermissionUIPayloadMaxItems != 32 {
		t.Error("permission UI payload limits mismatch")
	}
	if PermissionUIPayloadMaxStringLength != 1024 || PermissionUIPayloadMaxTotalBytes != 16*1024 {
		t.Error("permission UI payload size limits mismatch")
	}
	if PermissionUIRedacted != "[REDACTED]" || PermissionUITruncated != "[TRUNCATED]" ||
		PermissionUIUnavailable != "[UNAVAILABLE]" {
		t.Error("permission UI markers mismatch")
	}
}

// ──────────────────────────── 用例：redact text ───────────────────────────────

// TestRedactText_路径与URI脱敏 验证 file:// 与绝对路径被替换为 [path]
func TestRedactText_路径与URI脱敏(t *testing.T) {
	got := RedactText("read file:///Users/alice/secret.txt and /tmp/abc/file.py")
	if !strings.Contains(got, "file://[redacted-path]") {
		t.Errorf("file uri not redacted: %q", got)
	}
	if strings.Contains(got, "/Users/alice") || strings.Contains(got, "/tmp/abc") {
		t.Errorf("path not redacted: %q", got)
	}
}

// TestRedactText_常见密钥脱敏 验证 sk-/ghp_/AKIA/xox 等模式
func TestRedactText_常见密钥脱敏(t *testing.T) {
	cases := []string{
		"key=sk-abcdefgh12345678",
		"token=ghp_abcdefghijklmnopqrstuvwxyz1234",
		"id=AKIA1234567890123456",
		"auth=xoxb-1234567890abcdef",
		"Authorization: Bearer abcdefgh12345678",
	}
	for _, in := range cases {
		got := RedactText(in)
		if strings.Contains(got, "sk-abcdefgh") || strings.Contains(got, "ghp_abc") ||
			strings.Contains(got, "AKIA1234") || strings.Contains(got, "xoxb-123") ||
			strings.Contains(got, "Bearer abcdefgh") {
			t.Errorf("secret leaked in %q -> %q", in, got)
		}
	}
}

// TestRedactText_换行归一与截断 验证 \r\n → 空格、空白折叠、超长截断
func TestRedactText_换行归一与截断(t *testing.T) {
	got := RedactText("a\r\nb\n  c   d")
	if got != "a b c d" {
		t.Errorf("whitespace normalization = %q", got)
	}

	long := strings.Repeat("x", 300)
	got = RedactText(long)
	if len([]rune(got)) != 240 {
		t.Errorf("truncated length = %d, want 240", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("should end with ellipsis: %q", got[len(got)-10:])
	}
}

// TestRedactText_小上限 验证 max_length <= 3 时直接切
func TestRedactText_小上限(t *testing.T) {
	got := RedactText("abcdef", WithRedactTextMaxLength(3))
	if got != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}

// ──────────────────────────── 用例：redact secret values ───────────────────────────────

// TestRedactSecretValues_赋值脱敏 验证 key=value 形态
func TestRedactSecretValues_赋值脱敏(t *testing.T) {
	got := RedactSecretValues("api_key: my-secret-value password = 'hunter2'")
	if strings.Contains(got, "my-secret-value") || strings.Contains(got, "hunter2") {
		t.Errorf("secret not redacted: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("no redaction marker: %q", got)
	}
}

// TestRedactSecretValues_保留路径 验证不误伤普通路径
func TestRedactSecretValues_保留路径(t *testing.T) {
	got := RedactSecretValues("cd /home/user/project && ls")
	if !strings.Contains(got, "/home/user/project") {
		t.Errorf("path should be preserved: %q", got)
	}
}

// ──────────────────────────── 用例：redact secret values preserve format ───────────────────────────────

// TestRedactSecretValuesPreserveFormat_保留空白 验证与 redact_secret_values 的差异
func TestRedactSecretValuesPreserveFormat_保留空白(t *testing.T) {
	in := "cmd\n  api_key=sk-abcdefgh12345678\n  echo ok"
	got := RedactSecretValuesPreserveFormat(in)
	if !strings.Contains(got, "\n") {
		t.Errorf("newline should be preserved: %q", got)
	}
	if strings.Contains(got, "sk-abcdefgh") {
		t.Errorf("secret leaked: %q", got)
	}
}

// TestRedactSecretValuesPreserveFormat_URL凭据 验证 URL netloc 与敏感 query
func TestRedactSecretValuesPreserveFormat_URL凭据(t *testing.T) {
	cases := []string{
		"https://user:pw@host.example.com/path?token=abc&x=1",
		"https://host.example.com/cb?code=secret&state=ok",
		"https://host.example.com/cb?api_key=xyz&safe=1",
	}
	for _, in := range cases {
		got := RedactSecretValuesPreserveFormat(in)
		if strings.Contains(got, "user:pw@") || strings.Contains(got, "token=abc") ||
			strings.Contains(got, "code=secret") || strings.Contains(got, "api_key=xyz") {
			t.Errorf("URL secret leaked: %q -> %q", in, got)
		}
	}
}

// TestRedactSecretValuesPreserveFormat_字面量校验 验证 require_literal_values
func TestRedactSecretValuesPreserveFormat_字面量校验(t *testing.T) {
	// 非字面量表达式（含 $ 等）应拒绝，避免把表达式伪装成密钥擦掉
	_, err := RedactSecretValuesPreserveFormatE("api_key=$ENV_SECRET", true)
	if err == nil {
		t.Error("non-literal value should raise reviewer_redaction_incomplete")
	}
	// 单引号包裹的字面量允许
	got, err := RedactSecretValuesPreserveFormatE("api_key='literal-value'", true)
	if err != nil || !strings.Contains(got, "[redacted]") {
		t.Errorf("quoted literal should redact: got %q, err %v", got, err)
	}
}

// ──────────────────────────── 用例：redact reviewable payload text ───────────────────────────────

// TestRedactReviewablePayloadText_路径脱敏 验证 6 类路径模式
func TestRedactReviewablePayloadText_路径脱敏(t *testing.T) {
	cases := []string{
		`/Users/alice/project/main.py`,
		`/home/bob/docs/readme.md`,
		`C:\Users\alice\file.txt`,
		`\\server\share\doc.txt`,
		`"~/project/main.py"`,
		`src/app/module.py`,
	}
	for _, in := range cases {
		got := RedactReviewablePayloadText(in, WithRedactRelativePaths(true))
		if strings.Contains(got, "alice/project") || strings.Contains(got, "module.py") {
			t.Errorf("path not redacted: %q -> %q", in, got)
		}
	}
}

// ──────────────────────────── 用例：redact url ───────────────────────────────

// TestRedactURL_凭据与query 验证 URL 重组
func TestRedactURL_凭据与query(t *testing.T) {
	got := RedactURL("https://user:pw@host.example.com:8443/a/b?token=abc&safe=1#frag")
	if strings.Contains(got, "user:pw") || strings.Contains(got, "token=abc") {
		t.Errorf("URL secret leaked: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("no marker: %q", got)
	}
	if !strings.Contains(got, "8443") {
		t.Errorf("port should be preserved: %q", got)
	}
}

// TestRedactURL_file协议 验证 file:// 直接返回固定串
func TestRedactURL_file协议(t *testing.T) {
	got := RedactURL("file:///Users/alice/secret.txt")
	if got != "file://[redacted-path]" {
		t.Errorf("got %q", got)
	}
}

// TestRedactURL_非URL退回redactText 验证无 scheme 走 redact_text
func TestRedactURL_非URL退回redactText(t *testing.T) {
	got := RedactURL("just some text /tmp/x")
	if strings.Contains(got, "/tmp/x") && !strings.Contains(got, "[redacted]") {
		t.Errorf("should go through redact_text: %q", got)
	}
}

// ──────────────────────────── 用例：reviewer path location ───────────────────────────────

// TestReviewerPathLocation_工作区内 验证 base=workspace 与相对路径
func TestReviewerPathLocation_工作区内(t *testing.T) {
	root := t.TempDir()
	loc, status := ReviewerPathLocation(root+"/src/main.py", root, "")
	if status != "complete" {
		t.Errorf("status = %q, want complete", status)
	}
	if loc == nil || loc.Base != "workspace" || loc.RelativePath != "src/main.py" {
		t.Errorf("loc = %+v", loc)
	}
}

// TestReviewerPathLocation_相对路径unavailable 验证非绝对路径
func TestReviewerPathLocation_相对路径unavailable(t *testing.T) {
	loc, status := ReviewerPathLocation("src/main.py", t.TempDir(), "")
	if status != "unavailable" || loc != nil {
		t.Errorf("got (%v, %q)", loc, status)
	}
}

// TestReviewerPathLocation_控制字符redacted 验证 unicodedata.category 以 C 开头
func TestReviewerPathLocation_控制字符redacted(t *testing.T) {
	loc, status := ReviewerPathLocation("/tmp/a\x00b", t.TempDir(), "")
	if status != "redacted" || loc != nil {
		t.Errorf("got (%v, %q)", loc, status)
	}
}

// ──────────────────────────── 用例：redact reviewer value ───────────────────────────────

// TestRedactReviewerValue_密钥键脱敏 验证 secret key 的值被 [redacted] 替换
func TestRedactReviewerValue_密钥键脱敏(t *testing.T) {
	in := map[string]any{
		"api_key":  "sk-abcdefgh12345678",
		"password": "hunter2",
		"safe":     "visible",
	}
	got := RedactReviewerValue(in).(map[string]any)
	if got["api_key"] != "[redacted]" || got["password"] != "[redacted]" {
		t.Errorf("secret keys not redacted: %v", got)
	}
	if got["safe"] != "visible" {
		t.Errorf("safe value changed: %v", got)
	}
}

// TestRedactReviewerValue_递归结构 验证嵌套 map/list
func TestRedactReviewerValue_递归结构(t *testing.T) {
	in := map[string]any{
		"items": []any{map[string]any{"token": "abc"}, "plain"},
	}
	got := RedactReviewerValue(in).(map[string]any)
	items := got["items"].([]any)
	if items[0].(map[string]any)["token"] != "[redacted]" {
		t.Errorf("nested secret not redacted: %v", items)
	}
}

// ──────────────────────────── 用例：redact json value ───────────────────────────────

// TestRedactJSONValue_结构与密钥 验证 JSON-safe 结构保留
func TestRedactJSONValue_结构与密钥(t *testing.T) {
	in := map[string]any{
		"api_key": "sk-abcdefgh12345678",
		"count":   3,
		"items":   []any{"a", "b", "c", "d", "e", "f", "g"},
	}
	got := RedactJSONValue(in).(map[string]any)
	if got["api_key"] != "[redacted]" {
		t.Errorf("secret key = %v", got["api_key"])
	}
	if got["count"] != 3 {
		t.Errorf("number changed: %v", got["count"])
	}
	if len(got["items"].([]any)) != 5 {
		t.Errorf("items should be capped at 5, got %v", got["items"])
	}
}

// ──────────────────────────── 用例：sanitize permission ui payload ───────────────────────────────

// TestSanitizePermissionUIPayload_基本 验证密钥键、深度、item 上限
func TestSanitizePermissionUIPayload_基本(t *testing.T) {
	in := map[string]any{
		"password":       "hunter2",
		"secret_context": map[string]any{"x": 1},
		"visible":        "hello",
	}
	got := SanitizePermissionUIPayload(in).(map[string]any)
	if got["password"] != "[REDACTED]" {
		t.Errorf("password = %v", got["password"])
	}
	if _, exists := got["secret_context"]; exists {
		t.Error("secret_context-like key should be dropped")
	}
	if got["visible"] != "hello" {
		t.Errorf("visible = %v", got["visible"])
	}
}

// TestSanitizePermissionUIPayload_过深截断 验证 depth 上限
func TestSanitizePermissionUIPayload_过深截断(t *testing.T) {
	nested := map[string]any{"l1": map[string]any{"l2": map[string]any{"l3": map[string]any{
		"l4": map[string]any{"l5": map[string]any{"l6": map[string]any{"l7": "deep"}}}}}}}
	got := SanitizePermissionUIPayload(nested)
	if got == nil || got == "[UNAVAILABLE]" {
		t.Fatal("should return bounded structure")
	}
}

// TestSanitizePermissionUIPayload_限值非法 验证 ValueError
func TestSanitizePermissionUIPayload_限值非法(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("too-small limits should raise ValueError (Go: panic)")
		}
	}()
	SanitizePermissionUIPayload(map[string]any{"x": 1},
		WithPermissionUIMaxDepth(0))
}

// TestSanitizePermissionUIPayload_循环引用 验证 active_ids 检测
func TestSanitizePermissionUIPayload_循环引用(t *testing.T) {
	m := map[string]any{}
	m["self"] = m
	got := SanitizePermissionUIPayload(m).(map[string]any)
	if got["self"] != "[UNAVAILABLE]" {
		t.Errorf("cycle should be [UNAVAILABLE], got %v", got["self"])
	}
}

// ──────────────────────────── 补充：低覆盖率分支 ────────────────────────────

// TestRedactSecretValuesPreserveFormatNoLimit 验证 max_length=None 不截断
func TestRedactSecretValuesPreserveFormatNoLimit(t *testing.T) {
	long := strings.Repeat("a", 300) + " api_key=secret"
	got := RedactSecretValuesPreserveFormatNoLimit(long)
	if len([]rune(got)) < 300 {
		t.Errorf("NoLimit should not truncate, got len %d", len([]rune(got)))
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("secret should still be redacted: %q", got[len(got)-40:])
	}
}

// TestRedactSecretValues_带自定义上限 覆盖 option 分支
func TestRedactSecretValues_带自定义上限(t *testing.T) {
	got := RedactSecretValues("abcdefgh", WithRedactTextMaxLength(3))
	if got != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}

// TestRedactReviewablePayloadText_自定义上限 覆盖 option 分支
func TestRedactReviewablePayloadText_自定义上限(t *testing.T) {
	got := RedactReviewablePayloadText(strings.Repeat("x", 50), WithRedactRelativePaths(false))
	if len([]rune(got)) > 1024 {
		t.Errorf("should respect default limit, got %d", len([]rune(got)))
	}
}

// TestReviewerPathLocation_平台受信根 覆盖 platform_trusted_root 分支
func TestReviewerPathLocation_平台受信根(t *testing.T) {
	trusted := t.TempDir()
	loc, status := ReviewerPathLocation(trusted+"/a/b.py", "", trusted)
	if status != "complete" || loc == nil {
		t.Fatalf("got (%+v, %q)", loc, status)
	}
	if loc.Base != "platform_trusted_root" || loc.RelativePath != "a/b.py" {
		t.Errorf("loc = %+v", loc)
	}
}

// TestReviewerPathLocation_超长omitted 覆盖 reviewLocationLimit 分支
func TestReviewerPathLocation_超长omitted(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("a", 300)
	loc, status := ReviewerPathLocation(root+"/"+long+".py", root, "")
	if status != "omitted" || loc != nil {
		t.Errorf("got (%+v, %q), want (nil, omitted)", loc, status)
	}
}

// TestReviewerPathLocation_工作区外anchor 覆盖 base=target.anchor 分支
func TestReviewerPathLocation_工作区外anchor(t *testing.T) {
	loc, status := ReviewerPathLocation("/tmp/outside/file.py", t.TempDir(), "")
	if status != "complete" || loc == nil {
		t.Fatalf("got (%+v, %q)", loc, status)
	}
	if loc.Base == "workspace" {
		t.Errorf("outside workspace should use anchor, got %+v", loc)
	}
}

// TestRedactPathValues_路径分量保留 通过 ReviewerPathLocation 间接覆盖
func TestRedactPathValues_路径分量保留(t *testing.T) {
	root := t.TempDir()
	// 路径里含密钥字面量时，safe_target 应把它按分量脱敏
	loc, status := ReviewerPathLocation(root+"/sk-abcdefgh12345678/file.py", root, "")
	if loc == nil || status == "unavailable" {
		t.Fatalf("got (%+v, %q)", loc, status)
	}
	if strings.Contains(loc.RelativePath, "sk-abcdefgh") {
		t.Errorf("secret should be masked: %q", loc.RelativePath)
	}
}

// TestRedactJSONValue_URL与float 覆盖 isAbsoluteURL / float32 / default 分支
func TestRedactJSONValue_URL与float(t *testing.T) {
	in := map[string]any{
		"url":   "https://u:p@host.example.com/p?token=abc",
		"nan":   math.NaN(),
		"str":   "plain text",
		"other": struct{ X int }{1},
	}
	got := RedactJSONValue(in).(map[string]any)
	if strings.Contains(fmt.Sprint(got["url"]), "token=abc") {
		t.Errorf("URL secret leaked: %v", got["url"])
	}
	if got["str"] != "plain text" {
		t.Errorf("str = %v", got["str"])
	}
	if _, ok := got["other"].(string); !ok {
		t.Errorf("unknown type should fall back to redact_text, got %T", got["other"])
	}
}

// TestSanitizePermissionUIPayload_列表与NaN 覆盖 list / float 分支
func TestSanitizePermissionUIPayload_列表与NaN(t *testing.T) {
	in := map[string]any{
		"items": []any{"a", "b", math.NaN(), map[string]any{"token": "x"}},
	}
	got := SanitizePermissionUIPayload(in).(map[string]any)
	items := got["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("items = %v", items)
	}
	if items[2] != "[UNAVAILABLE]" {
		t.Errorf("NaN should be [UNAVAILABLE], got %v", items[2])
	}
	if items[3].(map[string]any)["token"] != "[REDACTED]" {
		t.Errorf("nested token should be redacted: %v", items[3])
	}
}

// TestSanitizePermissionUIPayload_字节上限二分 覆盖 boundPermissionUIPayload 截断分支
func TestSanitizePermissionUIPayload_字节上限二分(t *testing.T) {
	big := map[string]any{"data": strings.Repeat("x", 600)}
	got := SanitizePermissionUIPayload(big, WithPermissionUIMaxTotalBytes(128))
	preview, ok := got.(map[string]string)
	if !ok {
		t.Errorf("oversized payload should return preview dict, got %T", got)
		return
	}
	if _, exists := preview["preview"]; !exists {
		t.Errorf("preview key missing: %v", preview)
	}
}

// TestRedactURL_fragment脱敏 覆盖 redactURLFragment 的 key=value 分支
func TestRedactURL_fragment脱敏(t *testing.T) {
	got := RedactURL("https://host.example.com/p#token=abc&safe=1")
	if strings.Contains(got, "token=abc") {
		t.Errorf("fragment secret leaked: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("no marker: %q", got)
	}
}

// TestRedactURL_非法URL 覆盖 url.Parse 失败分支
func TestRedactURL_非法URL(t *testing.T) {
	got := RedactURL("://bad-url")
	if got == "" {
		t.Error("should fall back to redact_text")
	}
}

// TestStringify_映射 覆盖 _stringify 的 Mapping 分支
func TestStringify_映射(t *testing.T) {
	got := RedactSecretValues(map[string]any{"api_key": "sk-abcdefgh12345678"})
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("mapping should be json-stringified then redacted: %q", got)
	}
}

// TestPermissionUIText_超长marker 覆盖 marker 截断分支
func TestPermissionUIText_超长marker(t *testing.T) {
	big := map[string]any{"text": strings.Repeat("a", 2000)}
	got := SanitizePermissionUIPayload(big).(map[string]any)
	text := fmt.Sprint(got["text"])
	if !strings.Contains(text, "[TRUNCATED]") {
		t.Errorf("long text should carry truncation marker: %q", text[len(text)-30:])
	}
}

// TestSafeURLNetloc_IPv6 覆盖 hostname 含冒号分支
func TestSafeURLNetloc_IPv6(t *testing.T) {
	got := RedactURL("https://[::1]:8080/p")
	if !strings.Contains(got, "[::1]:8080") {
		t.Errorf("IPv6 host+port should be preserved: %q", got)
	}
}

// TestIsPermissionUISecretKey_后缀 覆盖 suffix 分支
func TestIsPermissionUISecretKey_后缀(t *testing.T) {
	in := map[string]any{"myAccessToken": "x", "dbSecretKey": "y", "safeName": "z"}
	got := SanitizePermissionUIPayload(in).(map[string]any)
	if got["myAccessToken"] != "[REDACTED]" || got["dbSecretKey"] != "[REDACTED]" {
		t.Errorf("suffix keys not redacted: %v", got)
	}
	if got["safeName"] != "z" {
		t.Errorf("safe key changed: %v", got)
	}
}

// ──────────────────────────── 覆盖率补齐（9.90d-1 Task 16） ────────────────────────────

// TestRedactPathValues_常见密钥与赋值 覆盖三个替换块中的后两个。
func TestRedactPathValues_常见密钥与赋值(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"sk前缀", "/home/u/sk-abcdefgh12345678/x", "/home/u/[redacted]/x"},
		{"ghp前缀", "/tmp/ghp_abcdefghij1234567890/y", "/tmp/[redacted]/y"},
		{"赋值password", "password=/home/u/secret", "password=/[redacted]/[redacted]/[redacted]"},
		{"赋值token", "token: abcdef", "token: [redacted]"},
		{"Bearer", "/x/Bearer abcdefgh12345678", "/x/Bearer [redacted]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := redactPathValues(c.in); got != c.want {
				t.Errorf("redactPathValues(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestRedactSecretValuesPreserveFormat_默认上限 覆盖不传 maxLength 的分支。
func TestRedactSecretValuesPreserveFormat_默认上限(t *testing.T) {
	got := RedactSecretValuesPreserveFormat("password=abc123")
	if strings.Contains(got, "abc123") {
		t.Errorf("secret should be redacted, got %q", got)
	}
}

// TestRedactJSONValue_无选项 覆盖 opts 为空的分支。
func TestRedactJSONValue_无选项(t *testing.T) {
	got := RedactJSONValue(map[string]any{"key": "password=abc"})
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if s, _ := m["key"].(string); strings.Contains(s, "abc") {
		t.Errorf("should be redacted, got %v", m)
	}
}

// TestRedactSecretValuesPreserveFormatE_字面量校验失败 覆盖 requireLiteral 报错分支。
func TestRedactSecretValuesPreserveFormatE_字面量校验失败(t *testing.T) {
	// require_literal_values=True 且赋值值含 shell 元字符（$ ` ( ) \ 换行）
	// 且非单引号开头 → 报 reviewer_redaction_incomplete
	if _, err := RedactSecretValuesPreserveFormatE("password=$(whoami)", true); err == nil {
		t.Error("shell metachar in assignment value should error")
	}
	// 单引号开头不报错
	if _, err := RedactSecretValuesPreserveFormatE("password='$(whoami)'", true); err != nil {
		t.Errorf("single-quoted value should not error: %v", err)
	}
	// 普通字面量不报错
	if _, err := RedactSecretValuesPreserveFormatE("password=abc123", true); err != nil {
		t.Errorf("plain literal should not error: %v", err)
	}
}

// TestSanitizePermissionUIValue_各标量类型 覆盖 nil/bool/int/int64/float64 分支。
func TestSanitizePermissionUIValue_各标量类型(t *testing.T) {
	o := permissionUIOptions{maxDepth: 5, maxItems: 10, maxStringLen: 100}
	active := map[uintptr]bool{}

	for _, v := range []any{nil, true, false, 1, int64(2), 3.5} {
		if got := sanitizePermissionUIValue(v, 0, o, active); got != v {
			t.Errorf("scalar %v (%T) -> %v", v, v, got)
		}
	}
	// Inf / NaN → unavailable
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		if got := sanitizePermissionUIValue(v, 0, o, active); got != PermissionUIUnavailable {
			t.Errorf("float %v -> %v", v, got)
		}
	}
}

// TestUniquePayloadKey_冲突递增 覆盖第二轮循环。
func TestUniquePayloadKey_冲突递增(t *testing.T) {
	payload := map[string]any{
		PermissionUITruncated:          1,
		PermissionUITruncated + " (2)": 2,
	}
	got := uniquePayloadKey(payload, PermissionUITruncated)
	if got != PermissionUITruncated+" (3)" {
		t.Errorf("got %q, want %q", got, PermissionUITruncated+" (3)")
	}
}

// TestStringify_Marshal失败 覆盖不可序列化值的回退分支。
func TestStringify_Marshal失败(t *testing.T) {
	// 含 func 的 map 无法 json.Marshal
	got := stringify(map[string]any{"fn": func() {}})
	if got == "" {
		t.Error("should fall back to fmt.Sprint")
	}
	if !strings.Contains(got, "map[") {
		t.Errorf("expected fmt.Sprint output, got %q", got)
	}
}

// TestStringify_nil与非map 覆盖其余分支。
func TestStringify_nil与非map(t *testing.T) {
	if got := stringify(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	if got := stringify(42); got != "42" {
		t.Errorf("int -> %q", got)
	}
}

// TestExpandHome_三种前缀 覆盖 ~ / ~/ / ~\ 分支。
func TestExpandHome_三种前缀(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// 裸 ~
	if got := expandHome("~"); got != home {
		t.Errorf("~ -> %q, want %q", got, home)
	}
	// ~/sub
	if got := expandHome("~/sub"); !strings.HasPrefix(got, home) || !strings.HasSuffix(got, "sub") {
		t.Errorf("~/sub -> %q", got)
	}
	// ~\sub（Windows 风格前缀）
	if got := expandHome(`~\sub`); !strings.HasPrefix(got, home) {
		t.Errorf(`~\sub -> %q`, got)
	}
	// 非 ~ 开头原样返回
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("abs -> %q", got)
	}
}
