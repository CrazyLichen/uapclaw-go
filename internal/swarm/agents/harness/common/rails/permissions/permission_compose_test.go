package permissions

import (
	"testing"
)

// ──────────────────────────── 用例：ComposeHostEffectivePermissions ───────────────────────────────

// TestCompose_整工具deny优先 验证任一层 deny 都不可放宽。
func TestCompose_整工具deny优先(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"tools": map[string]any{"bash": "deny"}},
		map[string]any{"tools": map[string]any{"bash": "allow"}},
		nil, "", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "deny" {
		t.Errorf("bash = %v, want deny", tools["bash"])
	}
}

// TestCompose_userAllow放宽globalAsk 验证 user allow 可放宽。
func TestCompose_userAllow放宽globalAsk(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"tools": map[string]any{"read_file": "ask"}},
		map[string]any{"tools": map[string]any{"read_file": "allow"}},
		nil, "", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["read_file"] != "allow" {
		t.Errorf("read_file = %v, want allow", tools["read_file"])
	}
}

// TestCompose_sessionAllow放宽 验证 session allow 可放宽 global/user ask。
func TestCompose_sessionAllow放宽(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"tools": map[string]any{"x": "ask"}},
		map[string]any{"tools": map[string]any{"x": "ask"}},
		map[string]any{"tools": map[string]any{"x": "allow"}},
		"", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["x"] != "allow" {
		t.Errorf("x = %v, want allow", tools["x"])
	}
}

// TestCompose_session的deny与ask被丢弃 验证会话层不能收紧。
func TestCompose_session的deny与ask被丢弃(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"tools": map[string]any{"x": "allow"}},
		nil,
		map[string]any{"tools": map[string]any{"x": "deny"}},
		"", false)
	tools, _ := got["tools"].(map[string]any)
	if tools["x"] != "allow" {
		t.Errorf("x = %v, want allow（session deny 应被丢弃）", tools["x"])
	}
}

// TestCompose_sessionDropKeys 验证 enabled/deny_tools/ask_tools 被丢弃。
func TestCompose_sessionDropKeys(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"enabled": true},
		nil,
		map[string]any{"enabled": false, "deny_tools": []any{"a"}, "ask_tools": []any{"b"}},
		"", false)
	if got["enabled"] != true {
		t.Errorf("enabled = %v, want true（session 的 enabled 应被丢弃）", got["enabled"])
	}
}

// TestCompose_列表键输出 验证 deny/ask/allow 三列表。
func TestCompose_列表键输出(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"tools": map[string]any{
			"a": "deny", "b": "ask", "c": "allow",
		}}, nil, nil, "", false)

	deny, _ := got["deny_tools"].([]string)
	ask, _ := got["ask_tools"].([]string)
	allow, _ := got["allow_tools"].([]string)
	if len(deny) != 1 || deny[0] != "a" {
		t.Errorf("deny_tools = %v", deny)
	}
	if len(ask) != 1 || ask[0] != "b" {
		t.Errorf("ask_tools = %v", ask)
	}
	if len(allow) != 1 || allow[0] != "c" {
		t.Errorf("allow_tools = %v", allow)
	}
}

// TestCompose_autoMode归一 验证 auto 模式走 normalize 且旧列表镜像被清。
func TestCompose_autoMode归一(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{
			"mode":  "auto",
			"tools": map[string]any{"bash": "allow"},
		}, nil, nil, "", true)

	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "ask" {
		t.Errorf("bash = %v, want ask（auto 模式高风险工具强制 ask）", tools["bash"])
	}
	if _, exists := got["allow_tools"]; exists {
		// 归一化后仍会重新生成列表键，这里只验证不残留旧镜像的 allow
		allow, _ := got["allow_tools"].([]string)
		for _, name := range allow {
			if name == "bash" {
				t.Error("bash should not be in allow_tools")
			}
		}
	}
}

// TestCompose_categories强制覆盖 验证 shell 名单由 Host 决定。
func TestCompose_categories强制覆盖(t *testing.T) {
	got := ComposeHostEffectivePermissions(
		map[string]any{"categories": map[string]any{"shell": []any{"evil"}}},
		map[string]any{"categories": map[string]any{"shell": []any{"user"}}},
		nil, "", false)
	categories, _ := got["categories"].(map[string]any)
	shell, _ := categories["shell"].([]string)
	for _, name := range shell {
		if name == "evil" || name == "user" {
			t.Errorf("shell should be host-owned, got %v", shell)
		}
	}
}

// TestCompose_nil输入 验证全 nil 不 panic。
func TestCompose_nil输入(t *testing.T) {
	got := ComposeHostEffectivePermissions(nil, nil, nil, "", false)
	if got == nil {
		t.Fatal("should return non-nil")
	}
	if got["enabled"] != false {
		t.Errorf("enabled = %v, want false", got["enabled"])
	}
}

// ──────────────────────────── 用例：packageBuiltinRulesEnabled ───────────────────────────────

// TestPackageBuiltinRulesEnabled 验证三种输入形态。
func TestPackageBuiltinRulesEnabled(t *testing.T) {
	cases := []struct {
		name  string
		layer map[string]any
		want  bool
	}{
		{"缺失默认true", map[string]any{}, true},
		{"显式true", map[string]any{"package_builtin_rules": true}, true},
		{"显式false", map[string]any{"package_builtin_rules": false}, false},
		{"字符串false", map[string]any{"package_builtin_rules": "false"}, false},
		{"字符串0", map[string]any{"package_builtin_rules": "0"}, false},
		{"字符串no", map[string]any{"package_builtin_rules": "NO"}, false},
		{"字符串off", map[string]any{"package_builtin_rules": " Off "}, false},
		{"字符串true", map[string]any{"package_builtin_rules": "true"}, true},
		{"非法类型默认true", map[string]any{"package_builtin_rules": 42}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := packageBuiltinRulesEnabled(c.layer); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ──────────────────────────── 用例：namedLevels ───────────────────────────────

// TestNamedLevels_基础 验证 tools 与三列表键。
func TestNamedLevels_基础(t *testing.T) {
	got := namedLevels(map[string]any{
		"tools":       map[string]any{"a": "deny", "b": map[string]any{"*": "ask"}},
		"deny_tools":  []any{"c"},
		"ask_tools":   []any{"d"},
		"allow_tools": []any{"e"},
	}, false)
	if got["a"] != "deny" || got["b"] != "ask" {
		t.Errorf("tools -> %v", got)
	}
	if got["c"] != "deny" || got["d"] != "ask" || got["e"] != "allow" {
		t.Errorf("lists -> %v", got)
	}
}

// TestNamedLevels_dropTighten 验证只保留 allow。
func TestNamedLevels_dropTighten(t *testing.T) {
	got := namedLevels(map[string]any{
		"tools":       map[string]any{"a": "deny", "b": "ask", "c": "allow"},
		"deny_tools":  []any{"x"},
		"ask_tools":   []any{"y"},
		"allow_tools": []any{"z"},
	}, true)
	if len(got) != 2 {
		t.Errorf("should keep only allow entries, got %v", got)
	}
	if got["c"] != "allow" || got["z"] != "allow" {
		t.Errorf("got %v", got)
	}
}

// TestNamedLevels_跳过非法 验证空名与非法档位被跳过。
func TestNamedLevels_跳过非法(t *testing.T) {
	got := namedLevels(map[string]any{
		"tools": map[string]any{"": "allow", "  ": "allow", "a": "turbo"},
	}, false)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：pickLevel ───────────────────────────────

// TestPickLevel_各组合 验证优先级。
func TestPickLevel_各组合(t *testing.T) {
	cases := []struct {
		g, u, s string
		want    string
		ok      bool
	}{
		{"deny", "allow", "allow", "deny", true}, // 任一层 deny
		{"ask", "deny", "", "deny", true},        // user deny
		{"ask", "ask", "allow", "allow", true},   // session allow 放宽
		{"ask", "allow", "", "allow", true},      // user allow 放宽
		{"ask", "ask", "", "ask", true},          // 都 ask
		{"", "", "allow", "allow", true},         // 仅 session allow
		{"", "allow", "", "allow", true},         // 仅 user allow
		{"", "", "", "", false},                  // 全空
		{"allow", "", "", "allow", true},         // 仅 global allow
	}
	for _, c := range cases {
		got, ok := pickLevel(c.g, c.u, c.s)
		if ok != c.ok || got != c.want {
			t.Errorf("pickLevel(%q,%q,%q) = (%q,%v), want (%q,%v)",
				c.g, c.u, c.s, got, ok, c.want, c.ok)
		}
	}
}

// ──────────────────────────── 用例：mergeListOfDicts ───────────────────────────────

// TestMergeListOfDicts 验证多层拼接与深拷贝。
func TestMergeListOfDicts(t *testing.T) {
	got := mergeListOfDicts(
		[]any{map[string]any{"a": 1}},
		[]any{map[string]any{"b": 2}},
		[]any{map[string]any{"c": 3}},
	)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	// 非 map 项被跳过
	got2 := mergeListOfDicts([]any{"str", 42, map[string]any{"d": 4}})
	if len(got2) != 1 {
		t.Errorf("got %v", got2)
	}
	// 非 list 层被跳过
	got3 := mergeListOfDicts("not-list", nil)
	if len(got3) != 0 {
		t.Errorf("got %v", got3)
	}
}

// ──────────────────────────── 用例：applyProductP1RuleActions ───────────────────────────────

// TestApplyProductP1RuleActions_按严重度补动作 验证四级映射。
func TestApplyProductP1RuleActions_按严重度补动作(t *testing.T) {
	cases := []struct{ severity, want string }{
		{"LOW", "allow"},
		{"MEDIUM", "allow"},
		{"HIGH", "ask"},
		{"CRITICAL", "deny"},
		{"low", "allow"},
		{" HIGH ", "ask"},
	}
	for _, c := range cases {
		got := applyProductP1RuleActions([]any{
			map[string]any{"severity": c.severity},
		})
		m, _ := got[0].(map[string]any)
		if m["action"] != c.want {
			t.Errorf("severity=%q -> action=%v, want %q", c.severity, m["action"], c.want)
		}
	}
}

// TestApplyProductP1RuleActions_已有动作保留 验证不覆盖显式 action。
func TestApplyProductP1RuleActions_已有动作保留(t *testing.T) {
	got := applyProductP1RuleActions([]any{
		map[string]any{"severity": "CRITICAL", "action": "allow"},
	})
	m, _ := got[0].(map[string]any)
	if m["action"] != "allow" {
		t.Errorf("action = %v, want allow（显式值优先）", m["action"])
	}
}

// TestApplyProductP1RuleActions_未知严重度不补 验证映射不到时保持无 action。
func TestApplyProductP1RuleActions_未知严重度不补(t *testing.T) {
	got := applyProductP1RuleActions([]any{
		map[string]any{"severity": "UNKNOWN"},
	})
	m, _ := got[0].(map[string]any)
	if _, exists := m["action"]; exists {
		t.Errorf("action should be absent, got %v", m["action"])
	}
}

// TestApplyProductP1RuleActions_跳过非map 验证非 map 项被丢弃。
func TestApplyProductP1RuleActions_跳过非map(t *testing.T) {
	got := applyProductP1RuleActions([]any{"str", 42})
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：netAction ───────────────────────────────

// TestNetAction 验证各种输入形态。
func TestNetAction(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{"allow", "allow", true},
		{"DENY", "deny", true},
		{" deny ", "deny", true},
		{"ask", "", false},
		{42, "", false},
		{nil, "", false},
		{map[string]any{"action": "deny"}, "deny", true},
		{map[string]any{"fetch": "allow"}, "allow", true},
		{map[string]any{"action": nil, "fetch": "deny"}, "deny", true},
		{map[string]any{"action": "ask", "fetch": "allow"}, "", false}, // action 优先
		{map[string]any{}, "", false},
	}
	for _, c := range cases {
		got, ok := netAction(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("netAction(%v) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ──────────────────────────── 用例：mergeNetURLs ───────────────────────────────

// TestMergeNetURLs_deny不可放宽 验证后续 allow 不能覆盖 deny。
func TestMergeNetURLs_deny不可放宽(t *testing.T) {
	got := mergeNetURLs(
		map[string]any{"*.evil.com": "deny"},
		map[string]any{"*.evil.com": "allow"},
	)
	if got["*.evil.com"] != "deny" {
		t.Errorf("got %v, want deny", got["*.evil.com"])
	}
}

// TestMergeNetURLs_allow可被收紧 验证后续 deny 覆盖 allow。
func TestMergeNetURLs_allow可被收紧(t *testing.T) {
	got := mergeNetURLs(
		map[string]any{"a.com": "allow"},
		map[string]any{"a.com": "deny"},
	)
	if got["a.com"] != "deny" {
		t.Errorf("got %v, want deny", got["a.com"])
	}
}

// TestMergeNetURLs_跳过非法 验证空 pattern 与非法动作被跳过。
func TestMergeNetURLs_跳过非法(t *testing.T) {
	got := mergeNetURLs(
		map[string]any{"": "deny", "  ": "allow", "x.com": "ask"},
		"not-a-map",
	)
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：composeShellGuard ───────────────────────────────

// TestComposeShellGuard_默认全开 验证缺失时 fail-closed 为 true。
func TestComposeShellGuard_默认全开(t *testing.T) {
	got := composeShellGuard(map[string]any{}, map[string]any{})
	if got["unknown_structure"] != true || got["interpreter_sink"] != true {
		t.Errorf("got %v, want all true", got)
	}
}

// TestComposeShellGuard_用户覆盖 验证 user 优先。
func TestComposeShellGuard_用户覆盖(t *testing.T) {
	got := composeShellGuard(
		map[string]any{"shell_guard": map[string]any{"unknown_structure": false}},
		map[string]any{"shell_guard": map[string]any{"unknown_structure": true}},
	)
	if got["unknown_structure"] != true {
		t.Errorf("user should win, got %v", got["unknown_structure"])
	}
	// interpreter_sink 未设置 → 默认 true
	if got["interpreter_sink"] != true {
		t.Errorf("missing should default true, got %v", got["interpreter_sink"])
	}
}

// TestComposeShellGuard_全局生效 验证 user 缺失时取 global。
func TestComposeShellGuard_全局生效(t *testing.T) {
	got := composeShellGuard(
		map[string]any{"shell_guard": map[string]any{"interpreter_sink": false}},
		map[string]any{},
	)
	if got["interpreter_sink"] != false {
		t.Errorf("global should apply, got %v", got["interpreter_sink"])
	}
}

// TestComposeShellGuard_非法值忽略 验证非布尔值被忽略。
func TestComposeShellGuard_非法值忽略(t *testing.T) {
	got := composeShellGuard(
		map[string]any{"shell_guard": map[string]any{"unknown_structure": "false"}},
		map[string]any{},
	)
	if got["unknown_structure"] != true {
		t.Errorf("non-bool should be ignored, got %v", got["unknown_structure"])
	}
}

// ──────────────────────────── 用例：composeNetGuard ───────────────────────────────

// TestComposeNetGuard_默认 验证缺失时 enabled=true/defaults=allow。
func TestComposeNetGuard_默认(t *testing.T) {
	got := composeNetGuard(map[string]any{}, map[string]any{})
	if got["enabled"] != true {
		t.Errorf("enabled = %v", got["enabled"])
	}
	if got["defaults"] != "allow" {
		t.Errorf("defaults = %v", got["defaults"])
	}
}

// TestComposeNetGuard_user不能放宽deny 验证 global deny + user allow → deny。
func TestComposeNetGuard_user不能放宽deny(t *testing.T) {
	got := composeNetGuard(
		map[string]any{"net_guard": map[string]any{"defaults": "deny"}},
		map[string]any{"net_guard": map[string]any{"defaults": "allow"}},
	)
	if got["defaults"] != "deny" {
		t.Errorf("defaults = %v, want deny", got["defaults"])
	}
}

// TestComposeNetGuard_user可收紧 验证 user deny 覆盖 global allow。
func TestComposeNetGuard_user可收紧(t *testing.T) {
	got := composeNetGuard(
		map[string]any{"net_guard": map[string]any{"defaults": "allow"}},
		map[string]any{"net_guard": map[string]any{"defaults": "deny"}},
	)
	if got["defaults"] != "deny" {
		t.Errorf("defaults = %v, want deny", got["defaults"])
	}
}

// TestComposeNetGuard_enabled取global 验证 user 不能改 enabled。
func TestComposeNetGuard_enabled取global(t *testing.T) {
	got := composeNetGuard(
		map[string]any{"net_guard": map[string]any{"enabled": false}},
		map[string]any{"net_guard": map[string]any{"enabled": true}},
	)
	if got["enabled"] != false {
		t.Errorf("enabled = %v, want false（user 不能改）", got["enabled"])
	}
}

// TestComposeNetGuard_urls合并 验证 urls 参与合并。
func TestComposeNetGuard_urls合并(t *testing.T) {
	got := composeNetGuard(
		map[string]any{"net_guard": map[string]any{"urls": map[string]any{"a.com": "allow"}}},
		map[string]any{"net_guard": map[string]any{"urls": map[string]any{"b.com": "deny"}}},
	)
	urls, _ := got["urls"].(map[string]any)
	if urls["a.com"] != "allow" || urls["b.com"] != "deny" {
		t.Errorf("urls = %v", urls)
	}
}

// ──────────────────────────── 用例：composeFileGuard ───────────────────────────────

// TestComposeFileGuard_默认空 验证无配置时返回空 map。
func TestComposeFileGuard_默认空(t *testing.T) {
	got := composeFileGuard(map[string]any{}, map[string]any{}, map[string]any{})
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// TestComposeFileGuard_叠加paths 验证 user/session 追加路径。
func TestComposeFileGuard_叠加paths(t *testing.T) {
	got := composeFileGuard(
		map[string]any{"file_guard": map[string]any{
			"enabled": true,
			"paths":   []any{map[string]any{"path": "/g", "read": "deny"}},
		}},
		map[string]any{"file_guard": map[string]any{
			"paths": []any{map[string]any{"path": "/u"}},
		}},
		map[string]any{"file_guard": map[string]any{
			"paths": []any{map[string]any{"path": "/s"}},
		}},
	)
	paths, _ := got["paths"].([]any)
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
	// enabled 来自 global
	if got["enabled"] != true {
		t.Errorf("enabled = %v", got["enabled"])
	}
}

// TestComposeFileGuard_非dict层忽略 验证 user 层 file_guard 非 map 时跳过。
func TestComposeFileGuard_非dict层忽略(t *testing.T) {
	got := composeFileGuard(
		map[string]any{"file_guard": map[string]any{"paths": []any{map[string]any{"p": 1}}}},
		map[string]any{"file_guard": "not-a-map"},
		map[string]any{"file_guard": map[string]any{"paths": "not-a-list"}},
	)
	paths, _ := got["paths"].([]any)
	if len(paths) != 1 {
		t.Errorf("paths = %v", paths)
	}
}

// ──────────────────────────── 用例：failClosed ───────────────────────────────

// TestFailClosed 验证兜底配置的关键字段。
func TestFailClosed(t *testing.T) {
	got := failClosed("boom", true)

	if got["enabled"] != true {
		t.Errorf("enabled = %v", got["enabled"])
	}
	if got["schema"] != "tiered_policy" {
		t.Errorf("schema = %v", got["schema"])
	}
	defaults, _ := got["defaults"].(map[string]any)
	if defaults["*"] != "ask" {
		t.Errorf(`defaults["*"] = %v, want ask`, defaults["*"])
	}
	fileGuard, _ := got["file_guard"].(map[string]any)
	if fileGuard["enabled"] != true {
		t.Errorf("file_guard.enabled = %v", fileGuard["enabled"])
	}
	fgDefaults, _ := fileGuard["defaults"].(map[string]any)
	if fgDefaults["read"] != "ask" || fgDefaults["write"] != "ask" || fgDefaults["exec"] != "ask" {
		t.Errorf("file_guard.defaults = %v", fgDefaults)
	}
	shellGuard, _ := got["shell_guard"].(map[string]any)
	if shellGuard["unknown_structure"] != true || shellGuard["interpreter_sink"] != true {
		t.Errorf("shell_guard = %v", shellGuard)
	}
	if got["package_builtin_rules"] != true {
		t.Errorf("package_builtin_rules = %v", got["package_builtin_rules"])
	}
}

// ──────────────────────────── 文本辅助 ────────────────────────────

// TestTrimHelpers 验证三个文本辅助。
func TestTrimHelpers(t *testing.T) {
	if got := trimSpaceText("  a  "); got != "a" {
		t.Errorf("trimSpaceText = %q", got)
	}
	if got := trimLowerText("  ABC  "); got != "abc" {
		t.Errorf("trimLowerText = %q", got)
	}
	if got := upperTrimSpace("  abc  "); got != "ABC" {
		t.Errorf("upperTrimSpace = %q", got)
	}
}

// TestSortedKeys 验证有序输出。
func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]struct{}{"c": {}, "a": {}, "b": {}})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
	if got := sortedKeys(nil); len(got) != 0 {
		t.Errorf("nil -> %v", got)
	}
}
