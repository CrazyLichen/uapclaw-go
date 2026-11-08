package netguard

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// TestNetGuardChecker_黑名单命中 测试 URL 命中 deny 规则
func TestNetGuardChecker_黑名单命中(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"evil.com": "deny"},
		},
	})
	if checker == nil {
		t.Fatal("checker 为 nil")
	}
	result := checker.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://evil.com/x"})
	if result == nil {
		t.Fatal("未命中")
	}
	if result.Permission != security.PermissionLevelDeny {
		t.Errorf("Permission = %v, want DENY", result.Permission)
	}
	if !strings.Contains(result.MatchedRule, "net_guard:url:evil.com") {
		t.Errorf("MatchedRule = %q, want 含 net_guard:url:evil.com", result.MatchedRule)
	}
}

// TestNetGuardChecker_defaults为deny 测试无规则命中但 defaults=deny 时拒绝
func TestNetGuardChecker_defaults为deny(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{
			"enabled":  true,
			"defaults": "deny",
			"urls":     map[string]any{},
		},
	})
	result := checker.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://other.com"})
	if result == nil || result.Permission != security.PermissionLevelDeny {
		t.Fatalf("result = %+v, want DENY（defaults=deny）", result)
	}
	if result.MatchedRule != "net_guard:defaults" {
		t.Errorf("MatchedRule = %q, want net_guard:defaults", result.MatchedRule)
	}
}

// TestNetGuardChecker_allow命中返回nil 测试 allow 命中返回 nil（不抬升 A 段）
func TestNetGuardChecker_allow命中返回nil(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"good.com": "allow"},
		},
	})
	if got := checker.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://good.com/x"}); got != nil {
		t.Errorf("allow 命中应返回 nil，got %+v", got)
	}
}

// TestNetGuardChecker_非fetch工具跳过 测试非 fetch 工具返回 nil
func TestNetGuardChecker_非fetch工具跳过(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{
		"net_guard": map[string]any{"enabled": true, "urls": map[string]any{"*": "deny"}},
	})
	if got := checker.Evaluate("read_file", map[string]any{"file_path": "/x"}); got != nil {
		t.Errorf("非 fetch 工具应返回 nil，got %+v", got)
	}
}

// TestNetGuardChecker_未启用返回nil 测试 enabled=false 或缺失时构造返回 nil
func TestNetGuardChecker_未启用返回nil(t *testing.T) {
	if checker := BuildNetGuardChecker(map[string]any{"net_guard": map[string]any{"enabled": false}}); checker != nil {
		t.Error("enabled=false 时 BuildNetGuardChecker 应返回 nil")
	}
	if checker := BuildNetGuardChecker(map[string]any{}); checker != nil {
		t.Error("无 net_guard 段时应返回 nil")
	}
}

// TestNetGuardChecker_Enabled访问器 测试 Enabled 方法
func TestNetGuardChecker_Enabled访问器(t *testing.T) {
	checker := BuildNetGuardChecker(map[string]any{"net_guard": map[string]any{"enabled": true}})
	if checker == nil {
		t.Fatal("checker 为 nil")
	}
	if !checker.Enabled() {
		t.Error("Enabled() = false, want true")
	}
	// disabled 的 checker 直接构造（不通过 Build）也应返回 nil
	direct := &NetGuardChecker{enabled: false}
	if got := direct.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://x.com"}); got != nil {
		t.Errorf("disabled checker 应返回 nil，got %+v", got)
	}
}

// TestExtractFetchURL_键顺序 测试六个 URL 参数键按顺序取第一个非空
func TestExtractFetchURL_键顺序(t *testing.T) {
	got := ExtractFetchURL(map[string]any{"target_url": "https://a.com", "url": "https://b.com"})
	if got != "https://b.com" {
		t.Errorf("ExtractFetchURL = %q, want https://b.com（url 优先于 target_url）", got)
	}
	if got = ExtractFetchURL(map[string]any{"page_url": "https://c.com"}); got != "https://c.com" {
		t.Errorf("ExtractFetchURL = %q, want https://c.com", got)
	}
	if got = ExtractFetchURL(map[string]any{}); got != "" {
		t.Errorf("空 args 应返回空串，got %q", got)
	}
	// 反斜杠归一化为 /
	if got = ExtractFetchURL(map[string]any{"url": `https://x.com\a`}); got != "https://x.com/a" {
		t.Errorf("ExtractFetchURL = %q, want https://x.com/a", got)
	}
}

// TestMatchNetPattern_通配符 测试域名通配符与完整 URL glob
func TestMatchNetPattern_通配符(t *testing.T) {
	cases := []struct {
		pattern, url string
		want         bool
	}{
		{"*.evil.com", "https://a.evil.com/x", true},
		{"evil.com", "https://evil.com/x", true},
		{"evil.com", "https://sub.evil.com/x", false},
		{"https://evil.com/*", "https://evil.com/a", true},
		{"https://evil.com/*", "https://other.com/a", false},
		{"", "https://x.com", false},
		{"evil.com", "", false},
	}
	for _, c := range cases {
		if got := MatchNetPattern(c.pattern, c.url); got != c.want {
			t.Errorf("MatchNetPattern(%q, %q) = %v, want %v", c.pattern, c.url, got, c.want)
		}
	}
}

// TestMatchNetPattern_带协议前缀 测试协议前缀的精确匹配
func TestMatchNetPattern_带协议前缀(t *testing.T) {
	// 无通配符时要求前缀匹配且 netloc 相同
	if !MatchNetPattern("https://evil.com/", "https://evil.com/path") {
		t.Error("同 netloc 前缀应匹配")
	}
	if MatchNetPattern("https://evil.com/", "https://other.com/https://evil.com/") {
		t.Error("netloc 不同不应匹配")
	}
}

// TestParseAction 测试 action 解析（含 dict 包装）
func TestParseAction(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"deny", "deny"},
		{"ALLOW", "allow"},
		{"  deny  ", "deny"},
		{map[string]any{"action": "deny"}, "deny"},
		{map[string]any{"fetch": "allow"}, "allow"},
		{map[string]any{"action": nil, "fetch": "deny"}, "deny"},
		{"ask", ""}, // ask 不是 net_guard 合法动作
		{"", ""},
		{nil, ""},
		{123, ""},
	}
	for _, c := range cases {
		if got := parseAction(c.in); got != c.want {
			t.Errorf("parseAction(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLoadPackageNetURLs_四条 测试包内 net_urls 加载
func TestLoadPackageNetURLs_四条(t *testing.T) {
	urls := LoadPackageNetURLs()
	if len(urls) != 4 {
		t.Fatalf("条数 = %d, want 4", len(urls))
	}
	for _, k := range []string{"169.254.169.254", "metadata.google.internal", "localhost", "127.0.0.1"} {
		if urls[k] != "deny" {
			t.Errorf("urls[%q] = %q, want deny", k, urls[k])
		}
	}
}

// TestMergePackageNetURLs_内置deny不可放宽 测试 overlay 的 allow 被内置 deny 覆盖
func TestMergePackageNetURLs_内置deny不可放宽(t *testing.T) {
	perms := map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"169.254.169.254": "allow"},
		},
	}
	merged := MergePackageNetURLs(perms, nil)
	ng, _ := merged["net_guard"].(map[string]any)
	urls, _ := ng["urls"].(map[string]any)
	if urls["169.254.169.254"] != "deny" {
		t.Errorf("169.254.169.254 = %v, want deny（内置 floor 优先）", urls["169.254.169.254"])
	}
}

// TestMergePackageNetURLs_开关关闭 测试 package_builtin_rules=false 时不注入
func TestMergePackageNetURLs_开关关闭(t *testing.T) {
	perms := map[string]any{
		"package_builtin_rules": false,
		"net_guard":             map[string]any{"enabled": true},
	}
	merged := MergePackageNetURLs(perms, nil)
	ng, _ := merged["net_guard"].(map[string]any)
	if urls, _ := ng["urls"].(map[string]any); len(urls) != 0 {
		t.Errorf("开关关闭时仍注入了 %d 条", len(urls))
	}
}

// TestMergePackageNetURLs_未启用不注入 测试 net_guard 未启用时不注入
func TestMergePackageNetURLs_未启用不注入(t *testing.T) {
	perms := map[string]any{"net_guard": map[string]any{"enabled": false}}
	merged := MergePackageNetURLs(perms, nil)
	ng, _ := merged["net_guard"].(map[string]any)
	if urls, _ := ng["urls"].(map[string]any); len(urls) != 0 {
		t.Errorf("未启用时仍注入了 %d 条", len(urls))
	}
}

// TestHasPackageNetURLs 测试内置 urls 存在性判定
func TestHasPackageNetURLs(t *testing.T) {
	// 全部内置键都在 → true
	all := map[string]any{
		"169.254.169.254": "deny", "metadata.google.internal": "deny",
		"localhost": "deny", "127.0.0.1": "deny",
	}
	if !HasPackageNetURLs(all) {
		t.Error("含全部内置键应为 true")
	}
	// 缺一个 → false
	partial := map[string]any{"169.254.169.254": "deny"}
	if HasPackageNetURLs(partial) {
		t.Error("缺键应为 false")
	}
	// 非 dict → false
	if HasPackageNetURLs("not a map") {
		t.Error("非 map 应为 false")
	}
	if HasPackageNetURLs(nil) {
		t.Error("nil 应为 false")
	}
}

// TestInit_注册工厂 测试 init 注册的工厂能构造判定器
func TestInit_注册工厂(t *testing.T) {
	evaluator := security.NewNetGuardEvaluator(map[string]any{
		"net_guard": map[string]any{"enabled": true, "urls": map[string]any{"evil.com": "deny"}},
	})
	if evaluator == nil {
		t.Fatal("工厂未注册或返回 nil")
	}
	result := evaluator.Evaluate("mcp_fetch_webpage", map[string]any{"url": "https://evil.com/x"})
	if result == nil || result.Permission != security.PermissionLevelDeny {
		t.Errorf("result = %+v, want DENY", result)
	}
	// 未启用应返回 nil
	if got := security.NewNetGuardEvaluator(map[string]any{}); got != nil {
		t.Error("无 net_guard 段时应返回 nil")
	}
}

// TestMatchURLGlob 测试 URL 形态的 glob（含查询串字符）
func TestMatchURLGlob(t *testing.T) {
	cases := []struct {
		value, pattern string
		want           bool
	}{
		{"https://a.com/x?k=v&j=w", "https://a.com/x?k=*", true},
		{"https://a.com/path", "https://a.com/*", true},
		{"https://b.com/path", "https://a.com/*", false},
		{"https://a.com/p", "https://a.com/?", true},
		{"", "https://a.com/*", false},
		{"https://a.com", "", false},
	}
	for _, c := range cases {
		if got := matchURLGlob(c.value, c.pattern); got != c.want {
			t.Errorf("matchURLGlob(%q, %q) = %v, want %v", c.value, c.pattern, got, c.want)
		}
	}
}

// TestUrlHostname 测试 hostname 提取的各种形态
func TestUrlHostname(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://Example.COM./x", "example.com"},
		{"http://a.com:8080/x", "a.com"},
		{"https://user:pass@a.com/x", "a.com"},
		{"https://[::1]:8080/x", "::1"},
		{"a.com/x", "a.com"},
		{"", ""},
	}
	for _, c := range cases {
		if got := urlHostname(c.in); got != c.want {
			t.Errorf("urlHostname(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMergePackageNetURLs_inject覆盖 测试 inject 参数覆盖 enabled 判断
func TestMergePackageNetURLs_inject覆盖(t *testing.T) {
	inject := true
	perms := map[string]any{"net_guard": map[string]any{"enabled": false}}
	merged := MergePackageNetURLs(perms, &inject)
	ng, _ := merged["net_guard"].(map[string]any)
	if urls, _ := ng["urls"].(map[string]any); len(urls) == 0 {
		t.Error("inject=true 时应强制注入")
	}

	noInject := false
	perms2 := map[string]any{"net_guard": map[string]any{"enabled": true}}
	merged2 := MergePackageNetURLs(perms2, &noInject)
	ng2, _ := merged2["net_guard"].(map[string]any)
	if urls, _ := ng2["urls"].(map[string]any); len(urls) != 0 {
		t.Error("inject=false 时不应注入")
	}
}

// TestMergePackageNetURLs_保留overlay自定项 测试 overlay 独有 url 保留
func TestMergePackageNetURLs_保留overlay自定项(t *testing.T) {
	perms := map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			"urls":    map[string]any{"custom.com": "deny"},
		},
	}
	merged := MergePackageNetURLs(perms, nil)
	ng, _ := merged["net_guard"].(map[string]any)
	urls, _ := ng["urls"].(map[string]any)
	if urls["custom.com"] != "deny" {
		t.Error("overlay 自定项丢失")
	}
	if len(urls) != 5 {
		t.Errorf("总条数 = %d, want 5（4 内置 + 1 overlay）", len(urls))
	}
}

// TestDeepCopyPermissions_不修改入参 测试深拷贝
func TestDeepCopyPermissions_不修改入参(t *testing.T) {
	perms := map[string]any{"net_guard": map[string]any{"enabled": true, "urls": map[string]any{}}}
	_ = MergePackageNetURLs(perms, nil)
	ng, _ := perms["net_guard"].(map[string]any)
	urls, _ := ng["urls"].(map[string]any)
	if len(urls) != 0 {
		t.Error("入参被修改")
	}
}

// TestStricterNetAction 测试取严（deny 优先）
func TestStricterNetAction(t *testing.T) {
	cases := []struct{ left, right, want string }{
		{"deny", "allow", "deny"},
		{"allow", "deny", "deny"},
		{"allow", "allow", "allow"},
	}
	for _, c := range cases {
		if got := stricterNetAction(c.left, c.right); got != c.want {
			t.Errorf("stricterNetAction(%q,%q) = %q, want %q", c.left, c.right, got, c.want)
		}
	}
}

// TestMergePackageNetURLs_双allow路径 测试 overlay 与 floor 都 allow 时保留 allow
func TestMergePackageNetURLs_双allow路径(t *testing.T) {
	perms := map[string]any{
		"net_guard": map[string]any{
			"enabled": true,
			// 内置 localhost 是 deny，这里给一个内置里没有的键走 stricter 路径
			"urls": map[string]any{"some.internal": "allow"},
		},
	}
	merged := MergePackageNetURLs(perms, nil)
	ng, _ := merged["net_guard"].(map[string]any)
	urls, _ := ng["urls"].(map[string]any)
	if urls["some.internal"] != "allow" {
		t.Errorf("overlay 独有 allow 应保留，got %v", urls["some.internal"])
	}
}

// TestTruthyAny_netguard 测试 Python bool() 语义
func TestTruthyAny_netguard(t *testing.T) {
	falsy := []any{nil, false, "", 0, int64(0), 0.0, []any{}, map[string]any{}}
	for _, v := range falsy {
		if truthyAny(v) {
			t.Errorf("truthyAny(%v) = true, want false", v)
		}
	}
	truthy := []any{true, "x", 1, int64(1), 1.5, []any{1}, map[string]any{"a": 1}}
	for _, v := range truthy {
		if !truthyAny(v) {
			t.Errorf("truthyAny(%v) = false, want true", v)
		}
	}
}

// TestParseNetURLAction 测试 net_urls 的动作解析
func TestParseNetURLAction(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"deny", "deny"},
		{"ALLOW", "allow"},
		{map[string]any{"action": "deny"}, "deny"},
		{map[string]any{"fetch": "allow"}, "allow"},
		{"ask", ""},
		{"", ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := parseNetURLAction(c.in); got != c.want {
			t.Errorf("parseNetURLAction(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
