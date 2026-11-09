package common

import (
	"strings"
	"testing"
)

// ──────────────────────────── 常量 ────────────────────────────

func TestWorkspacePaths_常量(t *testing.T) {
	if WorkspaceCurrentURI != "workspace://current" {
		t.Errorf("WorkspaceCurrentURI = %q", WorkspaceCurrentURI)
	}
	if WorkspaceCurrentURIPrefix != "workspace://current/" {
		t.Errorf("WorkspaceCurrentURIPrefix = %q", WorkspaceCurrentURIPrefix)
	}
	if StaleSandboxArtifactPath != "[stale sandbox artifact path redacted]" {
		t.Errorf("StaleSandboxArtifactPath = %q", StaleSandboxArtifactPath)
	}
}

// ──────────────────────────── normalize_workspace_root ────────────────────────────

func TestNormalizeWorkspaceRoot_nil(t *testing.T) {
	if got := NormalizeWorkspaceRoot(nil); got != nil {
		t.Errorf("nil root should return nil, got %v", got)
	}
}

func TestNormalizeWorkspaceRoot_绝对路径(t *testing.T) {
	dir := t.TempDir()
	got := NormalizeWorkspaceRoot(dir)
	if got == nil || *got != cleanAbs(dir) {
		t.Errorf("got %v, want %v", got, cleanAbs(dir))
	}
}

// ──────────────────────────── sandbox_artifact_session_root ────────────────────────────

func TestSandboxArtifactSessionRoot_命中(t *testing.T) {
	got := SandboxArtifactSessionRoot("/ws/.sandbox-artifacts/sess-1/out/file.txt")
	if got == nil || !strings.HasSuffix(*got, "/.sandbox-artifacts/sess-1") {
		t.Errorf("got %v", got)
	}
}

func TestSandboxArtifactSessionRoot_无会话层(t *testing.T) {
	// .sandbox-artifacts 是最后一段时无 session root
	got := SandboxArtifactSessionRoot("/ws/.sandbox-artifacts")
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestIsSandboxArtifactWorkspaceRoot(t *testing.T) {
	if !IsSandboxArtifactWorkspaceRoot("/ws/.sandbox-artifacts/sess-1") {
		t.Error("session root should be recognized")
	}
	if IsSandboxArtifactWorkspaceRoot("/ws/normal") {
		t.Error("normal path should not be recognized")
	}
}

// ──────────────────────────── _normalize_path_text ────────────────────────────

func TestNormalizePathText_转义与反斜杠(t *testing.T) {
	// URL 解码 + NFKC + 反斜杠归一
	got := normalizePathText("  /ws/a%20b\\c  ")
	if got != "/ws/a b/c" {
		t.Errorf("got %q, want /ws/a b/c", got)
	}
}

func TestNormalizePathText_nil与空(t *testing.T) {
	if got := normalizePathText(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	if got := normalizePathText(""); got != "" {
		t.Errorf("empty -> %q", got)
	}
}

// ──────────────────────────── resolve_workspace_path ────────────────────────────

func TestResolveWorkspacePath_URI(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("workspace://current/src/main.py", root)
	if got == nil || !strings.HasSuffix(*got, "/src/main.py") {
		t.Errorf("got %v", got)
	}
}

func TestResolveWorkspacePath_URI无root(t *testing.T) {
	if got := ResolveWorkspacePath("workspace://current/src/main.py", nil); got != nil {
		t.Errorf("URI without root should return nil, got %v", got)
	}
}

func TestResolveWorkspacePath_URI根本身(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("workspace://current", root)
	if got == nil || *got != cleanAbs(root) {
		t.Errorf("bare URI should resolve to root, got %v", got)
	}
}

func TestResolveWorkspacePath_相对路径(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("src/main.py", root)
	if got == nil || !strings.HasSuffix(*got, "/src/main.py") {
		t.Errorf("got %v", got)
	}
}

func TestResolveWorkspacePath_相对路径关闭resolve(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("src/main.py", root, WithResolveRelative(false))
	// resolve_relative=False 时直接 resolve 到进程 cwd，不在 root 下
	if got == nil {
		t.Error("should not be nil")
	}
}

func TestResolveWorkspacePath_绝对路径保留(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("/etc/hosts", root)
	if got == nil || *got != cleanAbs("/etc/hosts") {
		t.Errorf("absolute path should be preserved, got %v", got)
	}
}

func TestResolveWorkspacePath_陈旧沙箱拒绝(t *testing.T) {
	root := t.TempDir() // 不含 .sandbox-artifacts
	got := ResolveWorkspacePath("/other/.sandbox-artifacts/sess-9/file.txt", root)
	if got != nil {
		t.Errorf("stale sandbox path should be rejected, got %v", got)
	}
}

func TestResolveWorkspacePath_同会话沙箱允许(t *testing.T) {
	root := t.TempDir() + "/.sandbox-artifacts/sess-1"
	got := ResolveWorkspacePath(root+"/out/a.txt", root)
	if got == nil {
		t.Error("same-session sandbox path should be allowed")
	}
}

func TestResolveWorkspacePath_关闭陈旧检查(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("/other/.sandbox-artifacts/sess-9/file.txt", root,
		WithRejectStaleSandboxPath(false))
	if got == nil {
		t.Error("reject_stale_sandbox_path=False should allow")
	}
}

func TestResolveWorkspacePath_空路径(t *testing.T) {
	if got := ResolveWorkspacePath("", t.TempDir()); got != nil {
		t.Errorf("empty path should return nil, got %v", got)
	}
}

// ──────────────────────────── resolve_workspace_path_details ────────────────────────────

func TestResolveWorkspacePathDetails_工作区内(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePathDetails("src/main.py", root)
	if got.Relation != "workspace" || got.CanonicalPath == nil {
		t.Errorf("got %+v", got)
	}
	if got.RejectedReason != "" {
		t.Errorf("rejected_reason should be empty, got %q", got.RejectedReason)
	}
}

func TestResolveWorkspacePathDetails_外部(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePathDetails("/etc/hosts", root)
	if got.Relation != "external" {
		t.Errorf("relation = %q, want external", got.Relation)
	}
}

func TestResolveWorkspacePathDetails_无法解析(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePathDetails("../../escape", root)
	if got.Relation != "unresolved" || got.RejectedReason != "workspace_path_unresolved" {
		t.Errorf("got %+v", got)
	}
	if got.DisplayPath != got.OriginalText {
		t.Errorf("display should fall back to original: %+v", got)
	}
}

// ──────────────────────────── display_workspace_path ────────────────────────────

func TestDisplayWorkspacePath_裸URI(t *testing.T) {
	root := t.TempDir()
	if got := DisplayWorkspacePath("workspace://current", root); got != cleanAbs(root) {
		t.Errorf("got %q, want %q", got, cleanAbs(root))
	}
}

func TestDisplayWorkspacePath_裸URI无root(t *testing.T) {
	if got := DisplayWorkspacePath("workspace://current", nil); got != "workspace://current/" {
		t.Errorf("got %q", got)
	}
}

func TestDisplayWorkspacePath_URI前缀(t *testing.T) {
	root := t.TempDir()
	got := DisplayWorkspacePath("workspace://current/src/a.py", root)
	if !strings.HasSuffix(got, "/src/a.py") {
		t.Errorf("got %q", got)
	}
}

func TestDisplayWorkspacePath_无root陈旧沙箱(t *testing.T) {
	got := DisplayWorkspacePath("/ws/.sandbox-artifacts/sess-1/out.txt", nil)
	if got != StaleSandboxArtifactPath {
		t.Errorf("got %q, want %q", got, StaleSandboxArtifactPath)
	}
}

func TestDisplayWorkspacePath_无root普通路径(t *testing.T) {
	if got := DisplayWorkspacePath("/just/a/path", nil); got != "/just/a/path" {
		t.Errorf("got %q", got)
	}
}

func TestDisplayWorkspacePath_工作区内不改写为URI(t *testing.T) {
	root := t.TempDir()
	got := DisplayWorkspacePath(root+"/src/a.py", root)
	if strings.HasPrefix(got, "workspace://") {
		t.Errorf("current workspace must NOT be rewritten to URI: %q", got)
	}
}

func TestDisplayWorkspacePath_外部路径保持原样(t *testing.T) {
	root := t.TempDir()
	got := DisplayWorkspacePath("/etc/hosts", root)
	if got != "/etc/hosts" {
		t.Errorf("got %q", got)
	}
}

func TestDisplayWorkspacePath_空串(t *testing.T) {
	if got := DisplayWorkspacePath("", t.TempDir()); got != "" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── sanitize_visible_text ────────────────────────────

func TestSanitizeVisibleText_workspaceVisible(t *testing.T) {
	in := "output at /ws/.sandbox-artifacts/sess-1/out.txt done"
	got := SanitizeVisibleText(in, nil)
	if strings.Contains(got, "/ws/.sandbox-artifacts/sess-1/out.txt") {
		t.Errorf("sandbox artifact path not redacted: %q", got)
	}
	if !strings.Contains(got, StaleSandboxArtifactPath) {
		t.Errorf("no redaction marker: %q", got)
	}
}

func TestSanitizeVisibleText_URI不脱敏(t *testing.T) {
	in := "workspace://current/.sandbox-artifacts/sess-1/out.txt"
	got := SanitizeVisibleText(in, nil)
	if got != in {
		t.Errorf("workspace:// URI should be preserved: %q", got)
	}
}

func TestSanitizeVisibleText_空串(t *testing.T) {
	if got := SanitizeVisibleText("", nil); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeVisibleText_reviewUI模式(t *testing.T) {
	root := t.TempDir()
	in := "read " + root + "/src/main.py and api_key=sk-abcdefgh12345678"
	got := SanitizeVisibleText(in, root, WithSanitizeMode("review_ui"))
	if strings.Contains(got, cleanAbs(root)+"/src/main.py") {
		t.Errorf("workspace root prefix should be relativized: %q", got)
	}
	if strings.Contains(got, "sk-abcdefgh12345678") {
		t.Errorf("secret should be redacted: %q", got)
	}
}

// ──────────────────────────── sanitize_visible_value ────────────────────────────

func TestSanitizeVisibleValue_嵌套结构(t *testing.T) {
	in := map[string]any{
		"path":  "/ws/.sandbox-artifacts/sess-1/out.txt",
		"items": []any{"/ws/.sandbox-artifacts/sess-2/a.txt", "plain"},
		"num":   42,
	}
	got := SanitizeVisibleValue(in, nil).(map[string]any)
	if strings.Contains(got["path"].(string), ".sandbox-artifacts") {
		t.Errorf("string value not sanitized: %v", got["path"])
	}
	items := got["items"].([]any)
	if strings.Contains(items[0].(string), ".sandbox-artifacts") {
		t.Errorf("list item not sanitized: %v", items[0])
	}
	if items[1] != "plain" || got["num"] != 42 {
		t.Errorf("non-path values changed: %v", got)
	}
}

func TestSanitizeVisibleValue_键也脱敏(t *testing.T) {
	in := map[string]any{"/ws/.sandbox-artifacts/sess-1/k.txt": "v"}
	got := SanitizeVisibleValue(in, nil).(map[string]any)
	for k := range got {
		if strings.Contains(k, ".sandbox-artifacts") {
			t.Errorf("key not sanitized: %q", k)
		}
	}
}

func TestSanitizeReviewUIValue_转发(t *testing.T) {
	root := t.TempDir()
	got := SanitizeReviewUIValue("plain text", root)
	if got != "plain text" {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── resolve_workspace_path_arguments ────────────────────────────

func TestResolveWorkspacePathArguments_路径键(t *testing.T) {
	root := t.TempDir()
	in := map[string]any{
		"file_path": "src/a.py",
		"command":   "echo hello",
	}
	got := ResolveWorkspacePathArguments(in, root).(map[string]any)
	if !strings.HasPrefix(got["file_path"].(string), cleanAbs(root)) {
		t.Errorf("path key not resolved: %v", got["file_path"])
	}
	if got["command"] != "echo hello" {
		t.Errorf("non-path key changed: %v", got["command"])
	}
}

func TestResolveWorkspacePathArguments_无root原样返回(t *testing.T) {
	in := map[string]any{"path": "src/a.py"}
	got := ResolveWorkspacePathArguments(in, nil)
	if got.(map[string]any)["path"] != "src/a.py" {
		t.Errorf("no root should return original, got %v", got)
	}
}

func TestResolveWorkspacePathArguments_越界报错(t *testing.T) {
	root := t.TempDir()
	defer func() {
		if r := recover(); r == nil {
			t.Error("out-of-workspace path should raise ValueError (Go: panic)")
		}
	}()
	ResolveWorkspacePathArguments(map[string]any{"path": "../../escape"}, root)
}

func TestResolveWorkspacePathArguments_列表路径键(t *testing.T) {
	root := t.TempDir()
	in := map[string]any{"file_paths": []any{"a.py", "b.py"}}
	got := ResolveWorkspacePathArguments(in, root).(map[string]any)
	items := got["file_paths"].([]any)
	if !strings.HasPrefix(items[0].(string), cleanAbs(root)) {
		t.Errorf("list path not resolved: %v", items[0])
	}
}

// ──────────────────────────── review_ui 私有助手 ────────────────────────────

func TestRelativizeWorkspaceRootPrefixes(t *testing.T) {
	root := t.TempDir()
	in := "at " + cleanAbs(root) + "/src/main.py done"
	got := relativizeWorkspaceRootPrefixes(in, root)
	if strings.Contains(got, cleanAbs(root)+"/src") {
		t.Errorf("prefix not relativized: %q", got)
	}
	if !strings.Contains(got, "src/main.py") {
		t.Errorf("relative path should survive: %q", got)
	}
}

func TestWorkspaceRootTraversalStarts_父目录段(t *testing.T) {
	root := "/ws/project"
	in := "at " + root + "/../escape done"
	starts := workspaceRootTraversalStarts(in, root)
	if len(starts) != 1 {
		t.Errorf("traversal start should be detected, got %v", starts)
	}
}

func TestPathFromRootHasParentSegment(t *testing.T) {
	if !pathFromRootHasParentSegment("/../escape", 0) {
		t.Error("leading .. should be detected")
	}
	if pathFromRootHasParentSegment("/src/main.py", 0) {
		t.Error("normal path should not be detected")
	}
}

func TestIsLocalPathPrefixCharacter(t *testing.T) {
	for _, c := range []rune{'a', 'Z', '0', '_', '.', '/', ':', '-'} {
		if !isLocalPathPrefixCharacter(c) {
			t.Errorf("%q should be prefix character", c)
		}
	}
	for _, c := range []rune{' ', '(', '@'} {
		if isLocalPathPrefixCharacter(c) {
			t.Errorf("%q should NOT be prefix character", c)
		}
	}
}

func TestReviewUIPathReplacement_工作区内(t *testing.T) {
	root := t.TempDir()
	got := reviewUIPathReplacement(root+"/src/main.py", root)
	if got != "src/main.py" {
		t.Errorf("got %q, want src/main.py", got)
	}
}

func TestReviewUIPathReplacement_外部保持(t *testing.T) {
	root := t.TempDir()
	got := reviewUIPathReplacement("/etc/hosts", root)
	if got != "/etc/hosts" {
		t.Errorf("got %q", got)
	}
}

func TestSandboxArtifactReplacement(t *testing.T) {
	if got := sandboxArtifactReplacement("workspace://current/x"); got != "workspace://current/x" {
		t.Errorf("URI should be preserved: %q", got)
	}
	if got := sandboxArtifactReplacement("/ws/.sandbox-artifacts/s1/x"); got != StaleSandboxArtifactPath {
		t.Errorf("got %q", got)
	}
}

func TestIsRelativeToPath(t *testing.T) {
	if !isRelativeToPath("/ws/a/b", "/ws") {
		t.Error("child should be relative")
	}
	if isRelativeToPath("/other/a", "/ws") {
		t.Error("sibling should not be relative")
	}
	if !isRelativeToPath("/ws", "/ws") {
		t.Error("equal path should be relative")
	}
}

// ──────────────────────────── 补充：覆盖率 ────────────────────────────

func TestWithSanitizeMaxLength(t *testing.T) {
	long := strings.Repeat("a", 500)
	got := SanitizeVisibleText(long, nil, WithSanitizeMode("review_ui"), WithSanitizeMaxLength(50))
	if len([]rune(got)) > 50 {
		t.Errorf("maxLength should bound result, got %d", len([]rune(got)))
	}
}

func TestAnyToString_各分支(t *testing.T) {
	s := "abc"
	if anyToString(nil) != "" || anyToString("x") != "x" || anyToString(&s) != "abc" {
		t.Error("string branches mismatch")
	}
	var nilPtr *string
	if anyToString(nilPtr) != "" {
		t.Error("nil *string should be empty")
	}
	if anyToString(42) != "42" {
		t.Error("default branch mismatch")
	}
}

func TestNormalizeWorkspaceRoot_指针与字符串(t *testing.T) {
	dir := t.TempDir()
	p := &dir
	if got := NormalizeWorkspaceRoot(p); got == nil || *got != cleanAbs(dir) {
		t.Errorf("*string branch: %v", got)
	}
	if got := NormalizeWorkspaceRoot(dir); got == nil || *got != cleanAbs(dir) {
		t.Errorf("string branch: %v", got)
	}
	var nilPtr *string
	if got := NormalizeWorkspaceRoot(nilPtr); got != nil {
		t.Errorf("nil *string should return nil, got %v", got)
	}
}

func TestExpandHome_波浪线(t *testing.T) {
	got := expandHome("~")
	if !strings.Contains(got, "root") && !strings.Contains(got, "home") {
		t.Logf("expanded home = %q", got)
	}
	got2 := expandHome("~/project/a.py")
	if strings.HasPrefix(got2, "~") {
		t.Errorf("~/ should be expanded, got %q", got2)
	}
	if got3 := expandHome("/absolute/path"); got3 != "/absolute/path" {
		t.Errorf("absolute unchanged: %q", got3)
	}
}

func TestReviewUIPathReplacement_nilRoot(t *testing.T) {
	if got := reviewUIPathReplacement("/etc/hosts", nil); got != "/etc/hosts" {
		t.Errorf("nil root should return matched, got %q", got)
	}
}

func TestReviewUIPathReplacement_根本身(t *testing.T) {
	root := t.TempDir()
	if got := reviewUIPathReplacement(cleanAbs(root), root); got != "." {
		t.Errorf("root itself should be '.', got %q", got)
	}
}

func TestSanitizeReviewUIText_家目录与Windows(t *testing.T) {
	root := t.TempDir()
	in := "see ~/project/a.py and C:\\Users\\alice\\file.txt and /etc/hosts"
	got := sanitizeReviewUIText(in, root, 60000)
	if got == "" {
		t.Error("should not return empty")
	}
	// 不应泄露工作区根前缀
	if strings.Contains(got, cleanAbs(root)+"/") {
		t.Errorf("root prefix leaked: %q", got)
	}
}

func TestReplaceBareRoot_单独出现(t *testing.T) {
	root := cleanAbs(t.TempDir())
	in := "workspace is " + root + " done"
	got := replaceBareRoot(in, root)
	if !strings.Contains(got, "workspace is . done") {
		t.Errorf("bare root not replaced: %q", got)
	}
}

func TestReplaceBareRoot_路径内不替换(t *testing.T) {
	root := cleanAbs(t.TempDir())
	in := "at " + root + "/src/a.py done"
	got := replaceBareRoot(in, root)
	if strings.Contains(got, "workspace is .") {
		t.Errorf("path-internal root should not be replaced: %q", got)
	}
}

func TestReplaceRootPrefix_前置字符跳过(t *testing.T) {
	root := "/ws/project"
	in := "x" + root + "/src/a.py"
	got := replaceRootPrefix(in, root, map[int]bool{})
	// 前一字符 'x' 是 \w，Python 的 (?<![\w./:-]) 应跳过
	if !strings.Contains(got, root+"/src/a.py") {
		t.Errorf("prefixed root should be preserved: %q", got)
	}
}

func TestResolveWorkspacePath_URI越界(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("workspace://current/../../escape", root)
	if got != nil {
		t.Errorf("URI traversal should be rejected, got %v", got)
	}
}

func TestResolveWorkspacePath_相对路径越界(t *testing.T) {
	root := t.TempDir()
	got := ResolveWorkspacePath("../../escape", root)
	if got != nil {
		t.Errorf("relative traversal should be rejected, got %v", got)
	}
}

func TestResolveWorkspacePath_同会话但不同session(t *testing.T) {
	root := t.TempDir() + "/.sandbox-artifacts/sess-1"
	other := t.TempDir() + "/.sandbox-artifacts/sess-2/out.txt"
	got := ResolveWorkspacePath(other, root)
	if got != nil {
		t.Errorf("different session should be rejected, got %v", got)
	}
}

func TestSanitizeVisibleValue_标量(t *testing.T) {
	if got := SanitizeVisibleValue(42, nil); got != 42 {
		t.Errorf("scalar passthrough: %v", got)
	}
	if got := SanitizeVisibleValue(nil, nil); got != nil {
		t.Errorf("nil passthrough: %v", got)
	}
}

func TestSandboxArtifactSessionRoot_Windows风格(t *testing.T) {
	got := SandboxArtifactSessionRoot("C:/ws/.sandbox-artifacts/sess-1/out.txt")
	if got == nil || !strings.Contains(*got, ".sandbox-artifacts/sess-1") {
		t.Errorf("got %v", got)
	}
}

func TestIsSandboxArtifactWorkspaceRoot_nilPath(t *testing.T) {
	// 空路径不应 panic
	_ = IsSandboxArtifactWorkspaceRoot("")
}

func TestCleanAbs_空串(t *testing.T) {
	if got := cleanAbs(""); got != "" {
		t.Errorf("empty -> %q", got)
	}
}
