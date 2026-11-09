package permissions

import (
	"testing"
)

// ──────────────────────────── 常量与默认值 ────────────────────────────

// TestAutoPermissionDefaultOptions 验证默认选项的键与值。
func TestAutoPermissionDefaultOptions(t *testing.T) {
	if got := AutoPermissionDefaultOptions["reviewer_timeout_ms"]; got != 60000 {
		t.Errorf("reviewer_timeout_ms = %v, want 60000", got)
	}
	// ⚠️ 默认是 json_object（不是 json）
	if got := AutoPermissionDefaultOptions["reviewer_output_format"]; got != "json_object" {
		t.Errorf("reviewer_output_format = %v, want json_object", got)
	}
	if got := AutoPermissionDefaultOptions["persistent_audit_enabled"]; got != false {
		t.Errorf("persistent_audit_enabled = %v, want false", got)
	}
	if got := AutoPermissionDefaultOptions["bounded_write_max_files"]; got != 3 {
		t.Errorf("bounded_write_max_files = %v, want 3", got)
	}
}

// ──────────────────────────── NormalizeAutoPermissionOptions ────────────────────────────

// TestNormalizeAutoPermissionOptions_默认值 验证空输入返回默认。
func TestNormalizeAutoPermissionOptions_默认值(t *testing.T) {
	got := NormalizeAutoPermissionOptions(nil)

	if got["reviewer_output_format"] != "json_object" {
		t.Errorf("reviewer_output_format = %v", got["reviewer_output_format"])
	}
	if got["reviewer_timeout_ms"] != 60000 {
		t.Errorf("reviewer_timeout_ms = %v", got["reviewer_timeout_ms"])
	}
	if got["persistent_audit_enabled"] != false {
		t.Errorf("persistent_audit_enabled = %v", got["persistent_audit_enabled"])
	}
	if got["bounded_write_max_files"] != 3 {
		t.Errorf("bounded_write_max_files = %v", got["bounded_write_max_files"])
	}
	if _, exists := got["bounded_write_excluded_paths"]; !exists {
		t.Error("bounded_write_excluded_paths should be present")
	}
}

// TestNormalizeAutoPermissionOptions_显式值 验证用户配置生效。
func TestNormalizeAutoPermissionOptions_显式值(t *testing.T) {
	raw := map[string]any{
		"reviewer_output_format":   "prompt",
		"reviewer_timeout_ms":      5000,
		"persistent_audit_enabled": true,
		"bounded_write_max_files":  7,
	}
	got := NormalizeAutoPermissionOptions(raw)

	if got["reviewer_output_format"] != "prompt" {
		t.Errorf("got %v", got["reviewer_output_format"])
	}
	if got["reviewer_timeout_ms"] != 5000 {
		t.Errorf("got %v", got["reviewer_timeout_ms"])
	}
	if got["persistent_audit_enabled"] != true {
		t.Errorf("got %v", got["persistent_audit_enabled"])
	}
	if got["bounded_write_max_files"] != 7 {
		t.Errorf("got %v", got["bounded_write_max_files"])
	}
}

// TestNormalizeAutoPermissionOptions_非正整数回落 验证 0/负数/非法串回落默认。
func TestNormalizeAutoPermissionOptions_非正整数回落(t *testing.T) {
	for _, v := range []any{0, -1, "abc", nil, 0.0} {
		got := NormalizeAutoPermissionOptions(map[string]any{"reviewer_timeout_ms": v})
		if got["reviewer_timeout_ms"] != 60000 {
			t.Errorf("timeout=%v (%T) -> %v, want default 60000", v, v, got["reviewer_timeout_ms"])
		}
	}
}

// TestNormalizeAutoPermissionOptions_字符串数字可转 验证 "5000" 能转成 5000。
func TestNormalizeAutoPermissionOptions_字符串数字可转(t *testing.T) {
	got := NormalizeAutoPermissionOptions(map[string]any{"reviewer_timeout_ms": "5000"})
	if got["reviewer_timeout_ms"] != 5000 {
		t.Errorf("got %v", got["reviewer_timeout_ms"])
	}
}

// TestNormalizeAutoPermissionOptions_布尔真值语义 验证 Python bool() 语义。
func TestNormalizeAutoPermissionOptions_布尔真值语义(t *testing.T) {
	// Python bool("false") == True（非空串为真）
	got := NormalizeAutoPermissionOptions(map[string]any{"persistent_audit_enabled": "false"})
	if got["persistent_audit_enabled"] != true {
		t.Errorf(`bool("false") should be true, got %v`, got["persistent_audit_enabled"])
	}
	// 0 / 空串为假
	for _, v := range []any{0, "", 0.0} {
		got := NormalizeAutoPermissionOptions(map[string]any{"persistent_audit_enabled": v})
		if got["persistent_audit_enabled"] != false {
			t.Errorf("bool(%v) should be false, got %v", v, got["persistent_audit_enabled"])
		}
	}
}

// TestNormalizeAutoPermissionOptions_保护路径合并 验证与内置保护路径合并。
func TestNormalizeAutoPermissionOptions_保护路径合并(t *testing.T) {
	got := NormalizeAutoPermissionOptions(map[string]any{
		"bounded_write_excluded_paths": []any{"/custom/secret"},
	})
	paths, ok := got["bounded_write_excluded_paths"].([]string)
	if !ok {
		t.Fatalf("type = %T", got["bounded_write_excluded_paths"])
	}
	if len(paths) < 2 {
		t.Errorf("should merge with protected paths, got %v", paths)
	}
	found := false
	for _, p := range paths {
		if p == "/custom/secret" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom path should be kept, got %v", paths)
	}
}

// TestNormalizeAutoPermissionOptions_未知键忽略 验证只处理已知键。
func TestNormalizeAutoPermissionOptions_未知键忽略(t *testing.T) {
	got := NormalizeAutoPermissionOptions(map[string]any{"unknown_key": "x"})
	if _, exists := got["unknown_key"]; exists {
		t.Error("unknown key should be dropped")
	}
}

// ──────────────────────────── NormalizeReviewerOutputFormat ────────────────────────────

// TestNormalizeReviewerOutputFormat 验证白名单与回落。
func TestNormalizeReviewerOutputFormat(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"json_object", "json_object"},
		{"prompt", "prompt"},
		{"json", "json_object"}, // 计划测试曾误写 json，实际回落
		{"xml", "json_object"},
		{"", "json_object"},
		{nil, "json_object"},
		{42, "json_object"},
		{"PROMPT", "json_object"}, // 大小写敏感
	}
	for _, c := range cases {
		if got := NormalizeReviewerOutputFormat(c.in); got != c.want {
			t.Errorf("NormalizeReviewerOutputFormat(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ──────────────────────────── NormalizePermissionsForRuntime ────────────────────────────

// TestNormalizePermissionsForRuntime_非auto原样 验证 manual 模式不改动。
func TestNormalizePermissionsForRuntime_非auto原样(t *testing.T) {
	perms := map[string]any{
		"mode":  "manual",
		"tools": map[string]any{"bash": "allow"},
	}
	got := NormalizePermissionsForRuntime(perms)
	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "allow" {
		t.Errorf("manual mode should not normalize tools, got %v", tools["bash"])
	}
	if got["mode"] != "manual" {
		t.Errorf("mode = %v", got["mode"])
	}
}

// TestNormalizePermissionsForRuntime_auto补全auto段 验证 auto 模式补默认。
func TestNormalizePermissionsForRuntime_auto补全auto段(t *testing.T) {
	got := NormalizePermissionsForRuntime(map[string]any{"mode": "auto"})
	auto, ok := got["auto"].(map[string]any)
	if !ok {
		t.Fatalf("auto type = %T", got["auto"])
	}
	if auto["reviewer_output_format"] != "json_object" {
		t.Errorf("got %v", auto["reviewer_output_format"])
	}
}

// TestNormalizePermissionsForRuntime_auto_defaults收窄 验证 defaults["*"] 不能 allow。
func TestNormalizePermissionsForRuntime_auto_defaults收窄(t *testing.T) {
	got := NormalizePermissionsForRuntime(map[string]any{
		"mode":     "auto",
		"defaults": map[string]any{"*": "allow"},
	})
	defaults, _ := got["defaults"].(map[string]any)
	if defaults["*"] != "ask" {
		t.Errorf(`defaults["*"] = %v, want ask（auto 模式不允许 allow 默认）`, defaults["*"])
	}
	// deny 保留
	got2 := NormalizePermissionsForRuntime(map[string]any{
		"mode":     "auto",
		"defaults": map[string]any{"*": "deny"},
	})
	defaults2, _ := got2["defaults"].(map[string]any)
	if defaults2["*"] != "deny" {
		t.Errorf(`defaults["*"] = %v, want deny`, defaults2["*"])
	}
}

// TestNormalizePermissionsForRuntime_tools高风险强制ask 验证 allow 被收窄。
func TestNormalizePermissionsForRuntime_tools高风险强制ask(t *testing.T) {
	// bash 是高风险工具，allow 应被强制改为 ask
	got := NormalizePermissionsForRuntime(map[string]any{
		"mode":  "auto",
		"tools": map[string]any{"bash": "allow"},
	})
	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "ask" {
		t.Errorf("bash = %v, want ask（高风险工具强制 ask）", tools["bash"])
	}
}

// TestNormalizePermissionsForRuntime_tools低风险allow保留 验证只读工具 allow 保留。
func TestNormalizePermissionsForRuntime_tools低风险allow保留(t *testing.T) {
	got := NormalizePermissionsForRuntime(map[string]any{
		"mode":  "auto",
		"tools": map[string]any{"read_file": "allow"},
	})
	tools, _ := got["tools"].(map[string]any)
	if tools["read_file"] != "allow" {
		t.Errorf("read_file = %v, want allow（低风险只读工具）", tools["read_file"])
	}
}

// TestNormalizePermissionsForRuntime_不改动入参 验证深拷贝。
func TestNormalizePermissionsForRuntime_不改动入参(t *testing.T) {
	perms := map[string]any{
		"mode":     "auto",
		"defaults": map[string]any{"*": "allow"},
	}
	_ = NormalizePermissionsForRuntime(perms)
	defaults, _ := perms["defaults"].(map[string]any)
	if defaults["*"] != "allow" {
		t.Error("input should not be mutated")
	}
}

// TestNormalizePermissionsForRuntime_nil输入 验证 nil 不 panic。
func TestNormalizePermissionsForRuntime_nil输入(t *testing.T) {
	got := NormalizePermissionsForRuntime(nil)
	if got == nil {
		t.Error("should return non-nil map")
	}
}

// ──────────────────────────── 工具名归一与合并 ────────────────────────────

// TestNormalizeToolsForAutoMode_别名归一 验证工具名别名折叠。
func TestNormalizeToolsForAutoMode_别名归一(t *testing.T) {
	got := normalizeToolsForAutoMode(map[string]any{
		"free_search": "ask",
	})
	if _, exists := got["free_search"]; exists {
		t.Error("alias should be normalized away")
	}
}

// TestNormalizeToolsForAutoMode_同名合并取严格 验证重复键按严格者合并。
func TestNormalizeToolsForAutoMode_同名合并取严格(t *testing.T) {
	// 两个不同写法归一后同名，allow + deny → deny
	got := normalizeToolsForAutoMode(map[string]any{
		"bash":     "allow",
		"exec_cmd": "deny",
	})
	// 只验证不 panic 且结果合法（具体键名依赖别名表）
	for name, level := range got {
		if level != "allow" && level != "ask" && level != "deny" {
			if _, isMap := level.(map[string]any); !isMap {
				t.Errorf("%s -> %v, want valid level or map", name, level)
			}
		}
	}
}

// TestNormalizeToolEntryForAutoMode_map条目 验证 map 的四个档位键分别归一。
func TestNormalizeToolEntryForAutoMode_map条目(t *testing.T) {
	entry := map[string]any{
		"*":          "allow",
		"paths":      map[string]any{"/a": "deny"},
		"level":      "deny",
		"permission": "ask",
	}
	got := normalizeToolEntryForAutoMode("read_file", entry)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	// 四个档位键都被归一为合法档位
	for _, key := range []string{"*", "level", "permission"} {
		if v, _ := m[key].(string); v != "allow" && v != "ask" && v != "deny" {
			t.Errorf("%s = %v", key, m[key])
		}
	}
	// 结构化规则原样保留
	if _, exists := m["paths"]; !exists {
		t.Error("paths should be preserved")
	}
}

// TestNormalizeLevel 验证档位归一与 map 取值。
func TestNormalizeLevel(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"allow", "allow"},
		{"ALLOW", "allow"},
		{"  Deny  ", "deny"},
		{"ask", "ask"},
		{"turbo", "ask"},
		{"", "ask"},
		{nil, "ask"},
		{42, "ask"},
		{map[string]any{"*": "deny"}, "deny"},
		{map[string]any{}, "ask"},
	}
	for _, c := range cases {
		if got := normalizeLevel(c.in); got != c.want {
			t.Errorf("normalizeLevel(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPositiveInt 验证正整数解析。
func TestPositiveInt(t *testing.T) {
	cases := []struct {
		in   any
		def  int
		want int
	}{
		{5000, 1000, 5000},
		{"5000", 1000, 5000},
		{0, 1000, 1000},
		{-5, 1000, 1000},
		{"abc", 1000, 1000},
		{nil, 1000, 1000},
		{3.9, 1000, 3},
		{true, 1000, 1},
	}
	for _, c := range cases {
		if got := positiveInt(c.in, c.def); got != c.want {
			t.Errorf("positiveInt(%v, %d) = %d, want %d", c.in, c.def, got, c.want)
		}
	}
}

// TestNormalizeStringTuple 验证字符串切片归一。
func TestNormalizeStringTuple(t *testing.T) {
	if got := normalizeStringTuple("not-a-list", nil); got != nil {
		t.Errorf("non-list -> %v", got)
	}
	got := normalizeStringTuple([]any{" a ", "", "b"}, nil)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

// TestStructuredRuleMapFromValue_map形态 验证 map 输入。
func TestStructuredRuleMapFromValue_map形态(t *testing.T) {
	got := structuredRuleMapFromValue(map[string]any{"/a": "deny", "/b": "allow"})
	if got["/a"] != "deny" || got["/b"] != "allow" {
		t.Errorf("got %v", got)
	}
}

// TestStructuredRuleMapFromValue_列表形态 验证 list 输入的两种元素。
func TestStructuredRuleMapFromValue_列表形态(t *testing.T) {
	// 字符串元素 → 视为 deny
	got := structuredRuleMapFromValue([]any{"/a"})
	if got["/a"] != "deny" {
		t.Errorf("string element should be deny, got %v", got)
	}
	// map 元素带 pattern + level
	got2 := structuredRuleMapFromValue([]any{
		map[string]any{"pattern": "/b", "level": "ask"},
	})
	if got2["/b"] != "ask" {
		t.Errorf("got %v", got2)
	}
	// path / command 别名
	got3 := structuredRuleMapFromValue([]any{
		map[string]any{"path": "/c", "permission": "deny"},
	})
	if got3["/c"] != "deny" {
		t.Errorf("got %v", got3)
	}
}

// TestStructuredRuleMapFromValue_非法输入 验证非 map/list 返回空。
func TestStructuredRuleMapFromValue_非法输入(t *testing.T) {
	if got := structuredRuleMapFromValue("x"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if got := structuredRuleMapFromValue(nil); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// TestStructuredRuleItemLevel 验证档位键优先级。
func TestStructuredRuleItemLevel(t *testing.T) {
	if lv, ok := structuredRuleItemLevel(map[string]any{"level": "deny"}); !ok || lv != "deny" {
		t.Errorf("level -> (%q,%v)", lv, ok)
	}
	if lv, ok := structuredRuleItemLevel(map[string]any{"permission": "ask"}); !ok || lv != "ask" {
		t.Errorf("permission -> (%q,%v)", lv, ok)
	}
	if lv, ok := structuredRuleItemLevel(map[string]any{"action": "allow"}); !ok || lv != "allow" {
		t.Errorf("action -> (%q,%v)", lv, ok)
	}
	if _, ok := structuredRuleItemLevel(map[string]any{}); ok {
		t.Error("empty should be not ok")
	}
}

// TestIsToolLevelKey 验证档位键判定。
func TestIsToolLevelKey(t *testing.T) {
	for _, key := range []string{"*", "default", "level", "permission"} {
		if !isToolLevelKey(key) {
			t.Errorf("%q should be level key", key)
		}
	}
	for _, key := range []string{"paths", "commands", "patterns", "other"} {
		if isToolLevelKey(key) {
			t.Errorf("%q should not be level key", key)
		}
	}
}

// TestMergeStructuredRuleMap_同键取严格 验证逐项合并。
func TestMergeStructuredRuleMap_同键取严格(t *testing.T) {
	got := mergeStructuredRuleMap("paths",
		map[string]any{"/a": "allow", "/b": "ask"},
		map[string]any{"/a": "deny", "/c": "allow"})
	if got["/a"] != "deny" {
		t.Errorf("/a = %v, want deny（严格者胜）", got["/a"])
	}
	if got["/b"] != "ask" {
		t.Errorf("/b = %v", got["/b"])
	}
	if got["/c"] != "allow" {
		t.Errorf("/c = %v", got["/c"])
	}
}

// ──────────────────────────── ResolveDeclaredAutoWorkspace ────────────────────────────

// TestResolveDeclaredAutoWorkspace_未声明 验证都空时返回 nil。
func TestResolveDeclaredAutoWorkspace_未声明(t *testing.T) {
	got, err := ResolveDeclaredAutoWorkspace(map[string]any{}, nil)
	if err != nil || got != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", got, err)
	}
}

// TestResolveDeclaredAutoWorkspace_workspace优先 验证 workspace_dir 优先。
func TestResolveDeclaredAutoWorkspace_workspace优先(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDeclaredAutoWorkspace(map[string]any{"workspace_dir": dir}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || *got == "" {
		t.Fatal("should resolve workspace")
	}
}

// TestResolveDeclaredAutoWorkspace_project兜底 验证 project_dir 兜底。
func TestResolveDeclaredAutoWorkspace_project兜底(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDeclaredAutoWorkspace(map[string]any{"project_dir": dir}, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil {
		t.Fatal("should resolve project")
	}
}

// TestResolveDeclaredAutoWorkspace_metadata兜底 验证 metadata.project_dir 兜底。
func TestResolveDeclaredAutoWorkspace_metadata兜底(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDeclaredAutoWorkspace(
		map[string]any{}, map[string]any{"project_dir": dir})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil {
		t.Fatal("should resolve from metadata")
	}
}

// TestResolveDeclaredAutoWorkspace_冲突报错 验证两个不同根报错。
func TestResolveDeclaredAutoWorkspace_冲突报错(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	_, err := ResolveDeclaredAutoWorkspace(
		map[string]any{"workspace_dir": a, "project_dir": b}, nil)
	if err == nil {
		t.Fatal("conflicting roots should error")
	}
	if err.Error() != "auto_permission_workspace_conflict:new_session_required" {
		t.Errorf("err = %v", err)
	}
}

// TestResolveDeclaredAutoWorkspace_相同不报错 验证同根不冲突。
func TestResolveDeclaredAutoWorkspace_相同不报错(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveDeclaredAutoWorkspace(
		map[string]any{"workspace_dir": dir, "project_dir": dir}, nil)
	if err != nil {
		t.Fatalf("same root should not conflict: %v", err)
	}
	if got == nil {
		t.Fatal("should resolve")
	}
}

// ──────────────────────────── SupportsPhaseAutoRoot ────────────────────────────

// TestSupportsPhaseAutoRoot 验证 team 排除与 mode 组合。
func TestSupportsPhaseAutoRoot(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   bool
	}{
		{"默认agent", map[string]any{}, true},
		{"team排除", map[string]any{"team": true}, false},
		{"team为假", map[string]any{"team": false}, true},
		{"agent+work", map[string]any{"mode": "agent", "work_mode": "work"}, true},
		{"agent+code", map[string]any{"mode": "agent", "work_mode": "code"}, true},
		{"非法work_mode", map[string]any{"mode": "agent", "work_mode": "deploy"}, false},
		{"team模式", map[string]any{"mode": "team"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SupportsPhaseAutoRoot(c.params); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestAnyToBool 验证 Python bool() 语义。
func TestAnyToBool(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{nil, false}, {true, true}, {false, false},
		{"", false}, {"x", true}, {"false", true},
		{0, false}, {1, true}, {0.0, false}, {1.5, true},
		{[]any{}, false}, {[]any{1}, true},
		{map[string]any{}, false}, {map[string]any{"k": 1}, true},
	}
	for _, c := range cases {
		if got := anyToBool(c.in); got != c.want {
			t.Errorf("anyToBool(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestAnyToInt 验证 Python int() 语义。
func TestAnyToInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{5000, 5000, true},
		{"5000", 5000, true},
		{" 5000 ", 5000, true},
		{3.9, 3, true},
		{true, 1, true},
		{false, 0, true},
		{int64(7), 7, true},
		{"abc", 0, false},
		{nil, 0, false},
		{[]any{}, 0, false},
	}
	for _, c := range cases {
		got, ok := anyToInt(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("anyToInt(%v) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestAnyToStringSlice 验证两种切片形态。
func TestAnyToStringSlice(t *testing.T) {
	if got := anyToStringSlice([]string{"a", "b"}); len(got) != 2 || got[0] != "a" {
		t.Errorf("[]string -> %v", got)
	}
	if got := anyToStringSlice([]any{"a", 1}); len(got) != 2 || got[1] != "1" {
		t.Errorf("[]any -> %v", got)
	}
	if got := anyToStringSlice("x"); got != nil {
		t.Errorf("string -> %v", got)
	}
}

// TestMapGet 验证安全取值。
func TestMapGet(t *testing.T) {
	if got := mapGet(nil, "k"); got != nil {
		t.Errorf("nil map -> %v", got)
	}
	if got := mapGet(map[string]any{"k": "v"}, "k"); got != "v" {
		t.Errorf("got %v", got)
	}
	if got := mapGet(map[string]any{}, "missing"); got != nil {
		t.Errorf("missing -> %v", got)
	}
}

// TestAutoPermissionConfigError 验证错误类型。
func TestAutoPermissionConfigError(t *testing.T) {
	err := &AutoPermissionConfigError{Code: "some_code"}
	if err.Error() != "some_code" {
		t.Errorf("Error() = %q", err.Error())
	}
	var nilErr *AutoPermissionConfigError
	if nilErr.Error() != "" {
		t.Errorf("nil Error() = %q", nilErr.Error())
	}
}

// TestResolveUserPathStrictFalse 验证路径解析。
func TestResolveUserPathStrictFalse(t *testing.T) {
	got, err := resolveUserPathStrictFalse("/tmp/x")
	if err != nil || got != "/tmp/x" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

// TestExpandHomePath 验证 ~ 展开。
func TestExpandHomePath(t *testing.T) {
	if got := expandHomePath("/abs"); got != "/abs" {
		t.Errorf("abs -> %q", got)
	}
	home := userHomeDir()
	if home != "" {
		if got := expandHomePath("~"); got != home {
			t.Errorf("~ -> %q, want %q", got, home)
		}
	}
}

// ──────────────────────────── 同名工具合并（覆盖率补齐） ────────────────────────────

// TestNormalizeToolsForAutoMode_别名折叠后合并 验证两个别名归一后合并取严格。
//
// exec_command 与 mcp_exec_command 归一后同名。
func TestNormalizeToolsForAutoMode_别名折叠后合并(t *testing.T) {
	got := normalizeToolsForAutoMode(map[string]any{
		"exec_command":     "allow",
		"mcp_exec_command": "deny",
	})
	level, exists := got["mcp_exec_command"]
	if !exists {
		t.Fatalf("merged key should be mcp_exec_command, got %v", got)
	}
	if level != "deny" {
		t.Errorf("merged level = %v, want deny（严格者胜）", level)
	}
}

// TestNormalizeToolsForAutoMode_别名折叠都是allow 验证 allow+allow 仍 allow。
func TestNormalizeToolsForAutoMode_别名折叠都是allow(t *testing.T) {
	got := normalizeToolsForAutoMode(map[string]any{
		"free_search":     "allow",
		"mcp_free_search": "allow",
	})
	if got["mcp_free_search"] != "allow" {
		t.Errorf("got %v", got["mcp_free_search"])
	}
}

// TestMergeToolEntriesForAutoMode_两侧标量 验证非 map 时直接返回严格档位。
func TestMergeToolEntriesForAutoMode_两侧标量(t *testing.T) {
	got := mergeToolEntriesForAutoMode("bash", "allow", "deny")
	if got != "deny" {
		t.Errorf("got %v, want deny", got)
	}
	got2 := mergeToolEntriesForAutoMode("bash", "ask", "allow")
	if got2 != "ask" {
		t.Errorf("got %v, want ask", got2)
	}
}

// TestMergeToolEntriesForAutoMode_一侧为map 验证 map 侧触发结构化合并。
func TestMergeToolEntriesForAutoMode_一侧为map(t *testing.T) {
	existing := map[string]any{"*": "allow", "paths": map[string]any{"/a": "allow"}}
	candidate := "deny"
	got := mergeToolEntriesForAutoMode("read_file", existing, candidate)
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if m["*"] != "deny" {
		t.Errorf("* = %v, want deny", m["*"])
	}
	if _, exists := m["paths"]; !exists {
		t.Error("paths should be preserved")
	}
}

// TestToolEntryDefaultLevel 验证各形态取档位。
func TestToolEntryDefaultLevel(t *testing.T) {
	// 标量
	if got := toolEntryDefaultLevel("read_file", "allow"); got != "allow" {
		t.Errorf("scalar -> %q", got)
	}
	// map 按 * 优先
	if got := toolEntryDefaultLevel("read_file", map[string]any{"*": "deny", "level": "allow"}); got != "deny" {
		t.Errorf("* 优先 -> %q", got)
	}
	// map 无 * 取 default
	if got := toolEntryDefaultLevel("read_file", map[string]any{"default": "ask"}); got != "ask" {
		t.Errorf("default -> %q", got)
	}
	// map 无档位键 → ask
	if got := toolEntryDefaultLevel("read_file", map[string]any{"paths": map[string]any{}}); got != "ask" {
		t.Errorf("no level key -> %q, want ask", got)
	}
}

// TestMergeStructuredToolRules 验证结构化规则合并。
func TestMergeStructuredToolRules(t *testing.T) {
	left := map[string]any{
		"*":     "allow",
		"paths": map[string]any{"/a": "allow"},
		"other": "left-value",
	}
	right := map[string]any{
		"level": "deny",
		"paths": map[string]any{"/a": "deny", "/b": "ask"},
	}
	got := mergeStructuredToolRules(left, right)

	// 档位键被跳过
	if _, exists := got["*"]; exists {
		t.Error("* should be skipped")
	}
	if _, exists := got["level"]; exists {
		t.Error("level should be skipped")
	}
	// paths 逐项合并，/a 取严格
	paths, _ := got["paths"].(map[string]any)
	if paths["/a"] != "deny" {
		t.Errorf("/a = %v, want deny", paths["/a"])
	}
	if paths["/b"] != "ask" {
		t.Errorf("/b = %v", paths["/b"])
	}
	// 非结构化键取右侧或保留左侧
	if got["other"] != "left-value" {
		t.Errorf("other = %v", got["other"])
	}
}

// TestMergeStructuredToolRules_非map侧 验证一侧非 map 时跳过。
func TestMergeStructuredToolRules_非map侧(t *testing.T) {
	got := mergeStructuredToolRules("not-a-map", map[string]any{"paths": map[string]any{"/a": "deny"}})
	if _, exists := got["paths"]; !exists {
		t.Errorf("got %v", got)
	}
	got2 := mergeStructuredToolRules(nil, nil)
	if len(got2) != 0 {
		t.Errorf("got %v", got2)
	}
}

// TestMergeStructuredToolRules_嵌套map合并 验证非结构化嵌套 map 递归合并。
func TestMergeStructuredToolRules_嵌套map合并(t *testing.T) {
	left := map[string]any{"nested": map[string]any{"a": "1"}}
	right := map[string]any{"nested": map[string]any{"b": "2"}}
	got := mergeStructuredToolRules(left, right)
	nested, _ := got["nested"].(map[string]any)
	if nested["a"] != "1" || nested["b"] != "2" {
		t.Errorf("nested = %v", nested)
	}
}

// TestMergeStructuredRuleValues 验证规则值合并。
func TestMergeStructuredRuleValues(t *testing.T) {
	// 两侧都能解析成规则 map → 逐项合并
	got := mergeStructuredRuleValues("paths",
		map[string]any{"/a": "allow"},
		map[string]any{"/a": "deny"})
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if m["/a"] != "deny" {
		t.Errorf("/a = %v, want deny", m["/a"])
	}
	// 两侧都解析不出 → 取右侧
	got2 := mergeStructuredRuleValues("paths", 42, "raw")
	if got2 != "raw" {
		t.Errorf("got %v, want raw", got2)
	}
}

// TestAnyToBool_未知类型 覆盖 default 分支。
func TestAnyToBool_未知类型(t *testing.T) {
	if !anyToBool(struct{ X int }{X: 1}) {
		t.Error("unknown struct should be true (non-zero)")
	}
}

// TestExpandHomePath_波浪号反斜杠 覆盖 ~\ 前缀。
func TestExpandHomePath_波浪号反斜杠(t *testing.T) {
	home := userHomeDir()
	if home == "" {
		t.Skip("no home dir")
	}
	got := expandHomePath(`~\sub`)
	if got == `~\sub` {
		t.Errorf(`~\sub should be expanded, got %q`, got)
	}
}
