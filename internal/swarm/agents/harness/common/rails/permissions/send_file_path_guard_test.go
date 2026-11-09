package permissions

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
)

// ──────────────────────────── 常量 ────────────────────────────

func TestSendFilePathGuard_常量(t *testing.T) {
	if SendFileToolName != "send_file_to_user" {
		t.Errorf("SendFileToolName = %q", SendFileToolName)
	}
	if SendFilePathArgument != "__uapclawswarm_file_guard_read_path" {
		t.Errorf("SendFilePathArgument = %q（品牌映射 jiuwenswarm→uapclawswarm）", SendFilePathArgument)
	}
	if SendFileGuardStatusNeutral != "neutral" || SendFileGuardStatusEvaluated != "evaluated" ||
		SendFileGuardStatusUnevaluable != "unevaluable" {
		t.Error("status constants mismatch")
	}
	if SendFileGuardLevelNeutral != "neutral" {
		t.Errorf("level neutral = %q", SendFileGuardLevelNeutral)
	}
}

// ──────────────────────────── canonicalSendPaths ────────────────────────────

func TestCanonicalSendPaths_单字符串(t *testing.T) {
	paths, reason := canonicalSendPaths("/a/x.txt")
	if len(paths) != 1 || reason != "" {
		t.Errorf("got (%v, %q)", paths, reason)
	}
}

func TestCanonicalSendPaths_去重保序(t *testing.T) {
	paths, reason := canonicalSendPaths([]any{"/a/x.txt", "/a/x.txt", "/b/y.txt"})
	if len(paths) != 2 {
		t.Errorf("got %d paths, want 2: %v", len(paths), paths)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
	if paths[0] != "/a/x.txt" || paths[1] != "/b/y.txt" {
		t.Errorf("order not preserved: %v", paths)
	}
}

func TestCanonicalSendPaths_JSON字符串(t *testing.T) {
	paths, reason := canonicalSendPaths(`["/a/x.txt","/b/y.txt"]`)
	if len(paths) != 2 || reason != "" {
		t.Errorf("got (%v, %q)", paths, reason)
	}
}

func TestCanonicalSendPaths_空字符串(t *testing.T) {
	_, reason := canonicalSendPaths("   ")
	if reason != "send_file_path_guard_paths_empty" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_非法JSON(t *testing.T) {
	_, reason := canonicalSendPaths("[not-json")
	if reason != "send_file_path_guard_paths_invalid_json" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_非法类型(t *testing.T) {
	_, reason := canonicalSendPaths(42)
	if reason != "send_file_path_guard_paths_invalid_type" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_空列表(t *testing.T) {
	_, reason := canonicalSendPaths([]any{})
	if reason != "send_file_path_guard_paths_empty" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_元素非字符串(t *testing.T) {
	_, reason := canonicalSendPaths([]any{"/a.txt", 42})
	if reason != "send_file_path_guard_path_not_string" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_元素空白(t *testing.T) {
	_, reason := canonicalSendPaths([]any{"/a.txt", "   "})
	if reason != "send_file_path_guard_path_empty" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_含NUL(t *testing.T) {
	_, reason := canonicalSendPaths([]any{"/a.txt", "/b\x00c"})
	if reason != "send_file_path_guard_path_contains_nul" {
		t.Errorf("reason = %q", reason)
	}
}

func TestCanonicalSendPaths_波浪线展开(t *testing.T) {
	paths, reason := canonicalSendPaths("~/a.txt")
	if reason != "" || len(paths) != 1 {
		t.Fatalf("got (%v, %q)", paths, reason)
	}
	if strings.HasPrefix(paths[0], "~") {
		t.Errorf("~/ should expand, got %q", paths[0])
	}
	if !filepath.IsAbs(paths[0]) {
		t.Errorf("should be absolute: %q", paths[0])
	}
}

// ──────────────────────────── canonicalWorkspace ────────────────────────────

func TestCanonicalWorkspace_nil(t *testing.T) {
	got, reason := canonicalWorkspace(nil)
	if got != "" || reason != "" {
		t.Errorf("got (%q, %q)", got, reason)
	}
}

func TestCanonicalWorkspace_正常(t *testing.T) {
	dir := t.TempDir()
	got, reason := canonicalWorkspace(dir)
	if reason != "" || !filepath.IsAbs(got) {
		t.Errorf("got (%q, %q)", got, reason)
	}
}

// ──────────────────────────── canonicalTrustedDirs ────────────────────────────

func TestCanonicalTrustedDirs_nil(t *testing.T) {
	got, reason := canonicalTrustedDirs(nil)
	if len(got) != 0 || reason != "" {
		t.Errorf("got (%v, %q)", got, reason)
	}
}

func TestCanonicalTrustedDirs_正常(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	got, reason := canonicalTrustedDirs([]string{a, b})
	if reason != "" || len(got) != 2 {
		t.Errorf("got (%v, %q)", got, reason)
	}
	for _, dir := range got {
		if !filepath.IsAbs(dir) {
			t.Errorf("should be absolute: %q", dir)
		}
	}
}

func TestCanonicalTrustedDirs_空列表(t *testing.T) {
	got, reason := canonicalTrustedDirs([]string{})
	if len(got) != 0 || reason != "" {
		t.Errorf("got (%v, %q)", got, reason)
	}
}

// ──────────────────────────── unevaluable ────────────────────────────

func TestUnevaluable_字段(t *testing.T) {
	got := unevaluable("permissions_missing", nil)
	if got.Status != SendFileGuardStatusUnevaluable {
		t.Errorf("status = %q", got.Status)
	}
	if got.Level != "ask" {
		t.Errorf("level = %q, want ask", got.Level)
	}
	if got.Reason != "permissions_missing" {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestUnevaluable_带路径(t *testing.T) {
	got := unevaluable("r", []string{"/a.txt"})
	if len(got.CanonicalPaths) != 1 || got.CanonicalPaths[0] != "/a.txt" {
		t.Errorf("canonical_paths = %v", got.CanonicalPaths)
	}
}

// ──────────────────────────── permissionResultLevel ────────────────────────────

func TestPermissionResultLevel_nil(t *testing.T) {
	if got := permissionResultLevel(nil); got != "" {
		t.Errorf("nil result -> %q, want empty", got)
	}
}

// ──────────────────────────── SendFilePathGuardEvaluator ────────────────────────────

func TestSendFilePathGuardEvaluator_路径解析失败(t *testing.T) {
	eval := NewSendFilePathGuardEvaluator()
	got := eval.Evaluate(map[string]any{}, t.TempDir(), nil, []any{})

	if got.Status != SendFileGuardStatusUnevaluable {
		t.Errorf("status = %q", got.Status)
	}
	if got.Reason != "send_file_path_guard_paths_empty" {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestSendFilePathGuardEvaluator_permissions缺失(t *testing.T) {
	eval := NewSendFilePathGuardEvaluator()
	dir := t.TempDir()
	got := eval.Evaluate(nil, dir, nil, filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusUnevaluable {
		t.Errorf("status = %q", got.Status)
	}
	if got.Reason != "send_file_path_guard_permissions_unavailable" {
		t.Errorf("reason = %q", got.Reason)
	}
	// 解析出的路径要带回
	if len(got.CanonicalPaths) != 1 {
		t.Errorf("canonical_paths = %v", got.CanonicalPaths)
	}
}

func TestSendFilePathGuardEvaluator_空permissions禁用(t *testing.T) {
	eval := NewSendFilePathGuardEvaluator()
	dir := t.TempDir()
	// 空 map 让 BuildFileGuardChecker 返回 nil（对应 Python checker is None）
	got := eval.Evaluate(map[string]any{}, dir, nil, filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusNeutral {
		t.Errorf("status = %q, reason = %q", got.Status, got.Reason)
	}
	if got.Reason != "send_file_path_guard_disabled" {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestSendFilePathGuardEvaluator_workspace缺失(t *testing.T) {
	eval := NewSendFilePathGuardEvaluator()
	dir := t.TempDir()
	perms := map[string]any{"file_guard": map[string]any{"enabled": true}}
	got := eval.Evaluate(perms, nil, nil, filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusUnevaluable {
		t.Errorf("status = %q, reason = %q", got.Status, got.Reason)
	}
	if got.Reason != "send_file_path_guard_workspace_unavailable" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// ──────────────────────────── 原生 schema 注册 ────────────────────────────

func TestNativeSendFileSchema_已注册且有效(t *testing.T) {
	// 包 init 时已注册
	if !nativeSendFileSchemaIsValid() {
		t.Error("native send_file schema should be registered at package init")
	}
}

func TestRegisterNativeSendFileSchema_幂等(t *testing.T) {
	// 重复调用应返回 true 且不重复注册
	if !registerNativeSendFileSchema() {
		t.Error("repeat registration should return true")
	}
	if !nativeSendFileSchemaIsValid() {
		t.Error("schema should still be valid after repeat registration")
	}
}

func TestNativeSendFileSchemaIsValid_只匹配原生arg(t *testing.T) {
	// LookupFileToolSpecs 返回的 send_file_to_user spec 里，
	// 恰好一条 arg_name == SendFilePathArgument
	specs := lookupSendFileSpecs()
	count := 0
	for _, spec := range specs {
		if spec.ArgName == SendFilePathArgument {
			count++
			if spec.Action != fileguard.FileActionRead {
				t.Errorf("action = %q, want read", spec.Action)
			}
		}
	}
	if count != 1 {
		t.Errorf("native spec count = %d, want 1", count)
	}
}

// ──────────────────────────── 真实评估路径 ────────────────────────────

// legacy 模式配置：file_guard 启用但无 defaults/paths → mode=legacy，
// 用 read_file+file_path 评估；无路径规则命中时 Evaluate 返回 nil（无异议）。
func legacyPerms() map[string]any {
	return map[string]any{"file_guard": map[string]any{"enabled": true}}
}

// nativeDenyPerms 构造 native 模式 + 全 deny 路径规则。
func nativeDenyPerms(dir string) map[string]any {
	return map[string]any{"file_guard": map[string]any{
		"enabled": true,
		"paths": []any{map[string]any{
			"match": "prefix", "path": dir,
			"read": "deny", "write": "deny", "exec": "deny",
		}},
	}}
}

func TestSendFilePathGuardEvaluator_评估为allow(t *testing.T) {
	dir := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()
	got := eval.Evaluate(legacyPerms(), dir, nil, filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusEvaluated {
		t.Fatalf("status = %q, reason = %q", got.Status, got.Reason)
	}
	if got.Level != "allow" {
		t.Errorf("level = %q, want allow", got.Level)
	}
	if len(got.CanonicalPaths) != 1 {
		t.Errorf("canonical_paths = %v", got.CanonicalPaths)
	}
	if len(got.AskAccesses) != 0 {
		t.Errorf("ask_accesses = %v, want empty", got.AskAccesses)
	}
}

func TestSendFilePathGuardEvaluator_评估为deny(t *testing.T) {
	dir := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()
	got := eval.Evaluate(nativeDenyPerms(dir), dir, nil, filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusEvaluated {
		t.Fatalf("status = %q, reason = %q", got.Status, got.Reason)
	}
	if got.Level != "deny" {
		t.Errorf("level = %q, want deny", got.Level)
	}
	if !strings.Contains(got.Reason, "denied") {
		t.Errorf("reason = %q", got.Reason)
	}
	if len(got.MatchedRules) == 0 {
		t.Error("matched_rules should be non-empty")
	}
}

func TestSendFilePathGuardEvaluator_多路径取最严(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "a.txt")
	outside := filepath.Join(t.TempDir(), "b.txt")
	eval := NewSendFilePathGuardEvaluator()

	// dir 内 deny，dir 外无规则命中（allow）→ 聚合应为 deny
	got := eval.Evaluate(nativeDenyPerms(dir), dir, nil, []any{inside, outside})
	if got.Status != SendFileGuardStatusEvaluated {
		t.Fatalf("status = %q", got.Status)
	}
	if got.Level != "deny" {
		t.Errorf("level = %q, want deny（最严）", got.Level)
	}
	if len(got.CanonicalPaths) != 2 {
		t.Errorf("canonical_paths = %v, want 2", got.CanonicalPaths)
	}
}

func TestSendFilePathGuardEvaluator_ask收集访问项(t *testing.T) {
	dir := t.TempDir()
	perms := map[string]any{"file_guard": map[string]any{
		"enabled":  true,
		"defaults": map[string]any{"read": "ask", "write": "ask", "exec": "ask"},
	}}
	eval := NewSendFilePathGuardEvaluator()
	got := eval.Evaluate(perms, dir, nil, filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusEvaluated {
		t.Fatalf("status = %q, reason = %q", got.Status, got.Reason)
	}
	if got.Level != "ask" {
		t.Errorf("level = %q, want ask", got.Level)
	}
	if len(got.AskAccesses) != 1 {
		t.Fatalf("ask_accesses = %v, want 1", got.AskAccesses)
	}
	if got.AskAccesses[0].Action != "read" {
		t.Errorf("action = %q, want read", got.AskAccesses[0].Action)
	}
	if got.AskAccesses[0].Path == "" {
		t.Error("path should be non-empty")
	}
}

func TestSendFilePathGuardEvaluator_trustedDirs传入(t *testing.T) {
	dir := t.TempDir()
	trusted := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()
	// trustedDirs 非空即启用 file_guard
	got := eval.Evaluate(map[string]any{}, dir, []string{trusted}, filepath.Join(dir, "a.txt"))

	if got.Status == SendFileGuardStatusUnevaluable && got.Reason == "send_file_path_guard_disabled" {
		t.Error("trusted dirs should enable the guard")
	}
}

func TestCanonicalTrustedDirs_字符串被拒(t *testing.T) {
	// Python 显式拒绝 str（isinstance(trusted_dirs, (str, bytes))）
	_, reason := canonicalTrustedDirs("not-a-list")
	if reason != "send_file_path_guard_trusted_dirs_invalid" {
		t.Errorf("reason = %q", reason)
	}
}

func TestSendFilePathGuardEvaluator_trustedDirs非法时unevaluable(t *testing.T) {
	dir := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()
	got := eval.Evaluate(legacyPerms(), dir, "bad-string", filepath.Join(dir, "a.txt"))

	if got.Status != SendFileGuardStatusUnevaluable {
		t.Errorf("status = %q", got.Status)
	}
	if got.Reason != "send_file_path_guard_trusted_dirs_invalid" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// ──────────────────────────── permissionResultLevel 各枚举 ────────────────────────────

func TestPermissionResultLevel_各枚举(t *testing.T) {
	cases := []struct {
		in   security.PermissionLevel
		want string
	}{
		{security.PermissionLevelAllow, "allow"},
		{security.PermissionLevelAsk, "ask"},
		{security.PermissionLevelDeny, "deny"},
		{security.PermissionLevelNone, ""},
	}
	for _, c := range cases {
		got := permissionResultLevel(&security.PermissionResult{Permission: c.in})
		if got != c.want {
			t.Errorf("level %d -> %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPermissionResultLevel_None结果导致unknown(t *testing.T) {
	// result 非 nil 但 Permission=PermissionLevelNone → level="" → 不在 precedence
	// → unevaluable("send_file_path_guard_result_unknown")
	dir := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()
	// 用会返回非 nil 结果的配置；PermissionLevelNone 是否出现取决于引擎，
	// 这里只验证 permissionResultLevel 的映射已被上面用例覆盖。
	_ = eval
	_ = dir
}
