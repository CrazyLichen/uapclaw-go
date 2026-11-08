package fileguard

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// TestNormalizePathGuardConfig_enabled推导 测试三种 enabled 推导路径
func TestNormalizePathGuardConfig_enabled推导(t *testing.T) {
	cases := []struct {
		name        string
		perms       map[string]any
		trustedDirs []string
		wantEnabled bool
	}{
		{"无任何配置默认关闭", map[string]any{}, nil, false},
		{"显式 enabled=true", map[string]any{"file_guard": map[string]any{"enabled": true}}, nil, true},
		{
			"显式 enabled=false 覆盖 external_directory",
			map[string]any{"file_guard": map[string]any{"enabled": false}, "external_directory": map[string]any{"*": "ask"}},
			nil, false,
		},
		{"external_directory 非空隐式开启", map[string]any{"external_directory": map[string]any{"*": "ask"}}, nil, true},
		{"trustedDirs 非空隐式开启", map[string]any{}, []string{"/trusted"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizePathGuardConfig(c.perms, "/ws", c.trustedDirs)
			if got.Enabled != c.wantEnabled {
				t.Errorf("Enabled = %v, want %v", got.Enabled, c.wantEnabled)
			}
		})
	}
}

// TestNormalizePathGuardConfig_native判定 测试 hasNativeFileGuard 的五条规则
func TestNormalizePathGuardConfig_native判定(t *testing.T) {
	cases := []struct {
		name     string
		perms    map[string]any
		wantMode string
	}{
		{
			"显式 defaults 轴 → native",
			map[string]any{"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}}},
			"native",
		},
		{
			"显式 workspace 轴 → native",
			map[string]any{"file_guard": map[string]any{"enabled": true, "workspace": map[string]any{"read": "allow"}}},
			"native",
		},
		{
			"paths 含 glob → native",
			map[string]any{"file_guard": map[string]any{"enabled": true, "paths": []any{map[string]any{"path": "/a/**", "match": "glob"}}}},
			"native",
		},
		{
			"仅 paths 无 external_directory → native",
			map[string]any{"file_guard": map[string]any{"enabled": true, "paths": []any{map[string]any{"path": "/a", "read": "ask"}}}},
			"native",
		},
		{
			"仅 paths 但有 external_directory → legacy",
			map[string]any{
				"file_guard":         map[string]any{"enabled": true, "paths": []any{map[string]any{"path": "/a", "read": "ask"}}},
				"external_directory": map[string]any{"*": "ask"},
			},
			"legacy",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizePathGuardConfig(c.perms, "/ws", nil)
			if got.Mode != c.wantMode {
				t.Errorf("Mode = %q, want %q", got.Mode, c.wantMode)
			}
		})
	}
}

// TestNormalizePathGuardConfig_过长前缀跳过 测试无 "/" 的前缀规则被跳过
func TestNormalizePathGuardConfig_过长前缀跳过(t *testing.T) {
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{"path": "C:", "read": "deny"},
				map[string]any{"path": "/valid/path", "read": "deny"},
			},
		},
	}
	got := NormalizePathGuardConfig(perms, "/ws", nil)
	for _, r := range got.Paths {
		if r.Path == "C:" {
			t.Error("过短前缀 C: 应被跳过（避免误匹配整盘）")
		}
	}
}

// TestBuildFileGuardChecker_未启用返回nil 测试路径层关闭时返回 nil
func TestBuildFileGuardChecker_未启用返回nil(t *testing.T) {
	if c := BuildFileGuardChecker(map[string]any{}, "/ws", nil); c != nil {
		t.Error("未启用时应返回 nil")
	}
}

// TestFileGuardChecker_Evaluate_native外部路径 测试 workspace 外路径被抬到 ASK
func TestFileGuardChecker_Evaluate_native外部路径(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}},
	}, ws, nil)
	if checker == nil {
		t.Fatal("checker 为 nil")
	}

	result := checker.Evaluate("read_file", map[string]any{"file_path": outside})
	if result == nil {
		t.Fatal("外部路径应产生判定结果")
	}
	if result.Permission != security.PermissionLevelAsk {
		t.Errorf("Permission = %v, want ASK", result.Permission)
	}
	if len(result.ExternalPaths) == 0 {
		t.Error("ExternalPaths 为空")
	}
}

// TestFileGuardChecker_Evaluate_内部路径返回nil 测试 workspace 内且默认 allow 时返回 nil
func TestFileGuardChecker_Evaluate_内部路径返回nil(t *testing.T) {
	ws := t.TempDir()
	inside := filepath.Join(ws, "ok.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{
			"enabled":   true,
			"defaults":  map[string]any{"read": "allow"},
			"workspace": map[string]any{"read": "allow"},
		},
	}, ws, nil)
	if checker == nil {
		t.Fatal("checker 为 nil")
	}
	if result := checker.Evaluate("read_file", map[string]any{"file_path": inside}); result != nil {
		t.Errorf("内部路径全 ALLOW 时应返回 nil，got %+v", result)
	}
}

// TestFileGuardChecker_Evaluate_未启用返回nil 测试 disabled 时返回 nil
func TestFileGuardChecker_Evaluate_未启用返回nil(t *testing.T) {
	checker := &FileGuardChecker{effective: EffectiveFileGuardConfig{Enabled: false}}
	if result := checker.Evaluate("read_file", map[string]any{"file_path": "/x"}); result != nil {
		t.Errorf("disabled 时应返回 nil，got %+v", result)
	}
}

// TestFileGuardChecker_Evaluate_无workspace返回nil 测试无 workspace 时返回 nil
func TestFileGuardChecker_Evaluate_无workspace返回nil(t *testing.T) {
	checker := &FileGuardChecker{effective: EffectiveFileGuardConfig{Enabled: true, WorkspaceRoot: ""}}
	if result := checker.Evaluate("read_file", map[string]any{"file_path": "/x"}); result != nil {
		t.Errorf("无 workspace 时应返回 nil，got %+v", result)
	}
}

// TestFileGuardChecker_CollectAskAccesses 测试只收 ASK 且按 (path, action) 去重
func TestFileGuardChecker_CollectAskAccesses(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "s.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}},
	}, ws, nil)
	if checker == nil {
		t.Fatal("checker 为 nil")
	}

	// read_file 有两个 spec 参数，同一个 path 各一次 → 应按 (path, read) 去重为 1
	got := checker.CollectAskAccesses("read_file", map[string]any{
		"file_path": outside,
		"path":      outside,
	})
	if len(got) != 1 {
		t.Fatalf("got %d 条, want 1（同 path+action 应去重）: %+v", len(got), got)
	}
	if got[0].Path != outside {
		t.Errorf("Path = %q, want %q", got[0].Path, outside)
	}
	if got[0].Action != string(FileActionRead) {
		t.Errorf("Action = %q, want read", got[0].Action)
	}
}

// TestFileGuardChecker_CollectAskAccesses_只收ASK 测试 DENY 不进 collect
func TestFileGuardChecker_CollectAskAccesses_只收ASK(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "d.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "deny"}},
	}, ws, nil)

	got := checker.CollectAskAccesses("read_file", map[string]any{"file_path": outside})
	if len(got) != 0 {
		t.Errorf("DENY 不应进 collect，got %+v", got)
	}
}

// TestFileGuardChecker_Enabled与Mode 测试两个访问器
func TestFileGuardChecker_Enabled与Mode(t *testing.T) {
	c := &FileGuardChecker{effective: EffectiveFileGuardConfig{Enabled: true, Mode: "native"}}
	if !c.Enabled() {
		t.Error("Enabled() = false, want true")
	}
	if c.Mode() != "native" {
		t.Errorf("Mode() = %q, want native", c.Mode())
	}
}

// TestFileGuardChecker_legacy模式 测试 legacy 模式的外部路径判定
func TestFileGuardChecker_legacy模式(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "ext.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard":         map[string]any{"enabled": true},
		"external_directory": map[string]any{"*": "ask"},
	}, ws, nil)
	if checker == nil {
		t.Fatal("checker 为 nil")
	}
	if checker.Mode() != "legacy" {
		t.Fatalf("Mode = %q, want legacy", checker.Mode())
	}

	result := checker.Evaluate("read_file", map[string]any{"file_path": outside})
	if result == nil {
		t.Fatal("外部路径应产生结果")
	}
	if !strings.Contains(result.MatchedRule, "external_directory") {
		t.Errorf("MatchedRule = %q, want 含 external_directory", result.MatchedRule)
	}
	if !strings.Contains(result.Reason, "outside workspace") {
		t.Errorf("Reason = %q, want 含 outside workspace", result.Reason)
	}
}

// TestFileGuardChecker_legacy内部路径放行 测试 workspace 内路径在 legacy 下放行
func TestFileGuardChecker_legacy内部路径放行(t *testing.T) {
	ws := t.TempDir()
	inside := filepath.Join(ws, "in.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard":         map[string]any{"enabled": true},
		"external_directory": map[string]any{"*": "ask"},
	}, ws, nil)

	if result := checker.Evaluate("read_file", map[string]any{"file_path": inside}); result != nil {
		t.Errorf("workspace 内路径应放行，got %+v", result)
	}
}

// TestFileGuardChecker_glob规则 测试 glob 匹配生效
func TestFileGuardChecker_glob规则(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "secrets", "key.pem")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{
			"enabled":  true,
			"defaults": map[string]any{"read": "allow"},
			"paths": []any{
				map[string]any{"path": "**/*.pem", "match": "glob", "read": "deny"},
			},
		},
	}, ws, nil)
	if checker == nil {
		t.Fatal("checker 为 nil")
	}

	result := checker.Evaluate("read_file", map[string]any{"file_path": target})
	if result == nil {
		t.Fatal("glob 规则应命中 *.pem")
	}
	if result.Permission != security.PermissionLevelDeny {
		t.Errorf("Permission = %v, want DENY", result.Permission)
	}
	if !strings.Contains(result.MatchedRule, "glob") {
		t.Errorf("MatchedRule = %q, want 含 glob", result.MatchedRule)
	}
}

// TestFileGuardChecker_trustedDirs放行 测试 trusted_dirs 内的路径被放行
func TestFileGuardChecker_trustedDirs放行(t *testing.T) {
	ws := t.TempDir()
	trusted := t.TempDir()
	target := filepath.Join(trusted, "ok.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}},
	}, ws, []string{trusted})
	if checker == nil {
		t.Fatal("checker 为 nil")
	}

	if result := checker.Evaluate("read_file", map[string]any{"file_path": target}); result != nil {
		t.Errorf("trusted_dirs 内路径应放行，got %+v", result)
	}
}

// TestFileGuardChecker_最长前缀优先 测试多个 prefix 命中取最长
func TestFileGuardChecker_最长前缀优先(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	target := filepath.Join(sub, "f.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{
			"enabled":  true,
			"defaults": map[string]any{"read": "ask"},
			"paths": []any{
				map[string]any{"path": ws, "read": "deny"},
				map[string]any{"path": sub, "read": "allow"},
			},
		},
	}, ws, nil)

	if result := checker.Evaluate("read_file", map[string]any{"file_path": target}); result != nil {
		t.Errorf("更长的 allow 前缀应生效，got %+v", result)
	}
}

// TestApplyImplications 测试 Write⇒Read / Exec⇒Read 蕴含
func TestApplyImplications(t *testing.T) {
	const (
		allow = security.PermissionLevelAllow
		ask   = security.PermissionLevelAsk
		deny  = security.PermissionLevelDeny
	)
	// write=allow 时 read 被提升为 allow
	r, w, e := applyImplications(ask, allow, ask, "/p")
	if r != allow || w != allow || e != ask {
		t.Errorf("got (%v,%v,%v), want (allow,allow,ask)", r, w, e)
	}
	// exec=allow 时 read 被提升为 allow
	r, w, e = applyImplications(ask, ask, allow, "/p")
	if r != allow || w != ask || e != allow {
		t.Errorf("got (%v,%v,%v), want (allow,ask,allow)", r, w, e)
	}
	// 显式 read=deny 优先于蕴含（不被提升）
	r, _, _ = applyImplications(deny, allow, allow, "/p")
	if r != deny {
		t.Errorf("显式 deny 应保留，got %v", r)
	}
	// write/exec 都非 allow 时不做蕴含
	r, w, e = applyImplications(ask, ask, ask, "/p")
	if r != ask || w != ask || e != ask {
		t.Errorf("got (%v,%v,%v), want (ask,ask,ask)", r, w, e)
	}
}

// TestMatchGlobPattern 测试 glob 的三种通配
func TestMatchGlobPattern(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.pem", "/any/dir/key.pem", true},
		{"**/*.pem", "key.pem", true}, // **/ 可匹配零层
		{"/a/*/c", "/a/b/c", true},
		{"/a/*/c", "/a/b/d/c", false}, // * 不跨 /
		{"/a/?/c", "/a/x/c", true},
		{"/a/?/c", "/a/xy/c", false},
		{"/exact", "/exact", true},
		{"/exact", "/other", false},
	}
	for _, c := range cases {
		if got := matchGlobPattern(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlobPattern(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// TestExplicitEnabled 测试显式 enabled 的读取
func TestExplicitEnabled(t *testing.T) {
	if _, ok := explicitEnabled(nil); ok {
		t.Error("nil 应返回 ok=false")
	}
	if _, ok := explicitEnabled(map[string]any{}); ok {
		t.Error("键缺失应返回 ok=false")
	}
	if v, ok := explicitEnabled(map[string]any{"enabled": true}); !ok || !v {
		t.Error("enabled=true 应返回 (true,true)")
	}
	if v, ok := explicitEnabled(map[string]any{"enabled": false}); !ok || v {
		t.Error("enabled=false 应返回 (false,true)")
	}
	// 非 bool 按 Python truthy 语义
	if v, ok := explicitEnabled(map[string]any{"enabled": "yes"}); !ok || !v {
		t.Error("非空字符串应为真")
	}
	if v, ok := explicitEnabled(map[string]any{"enabled": ""}); !ok || v {
		t.Error("空字符串应为假")
	}
}

// TestReportLegacyPathRulesAtLoad 测试加载期遗留规则告警
func TestReportLegacyPathRulesAtLoad(t *testing.T) {
	perms := map[string]any{
		"rules": []any{
			// path 类工具 + 像路径的 pattern → 应告警
			map[string]any{"id": "legacy1", "tools": []any{"read_file"}, "pattern": "/etc/**"},
			// 非 path 类工具 → 不告警
			map[string]any{"id": "ok1", "tools": []any{"bash"}, "pattern": "/etc/**"},
			// pattern 不像路径 → 不告警
			map[string]any{"id": "ok2", "tools": []any{"read_file"}, "pattern": "re:.*"},
		},
	}
	flagged := ReportLegacyPathRulesAtLoad(perms)
	if len(flagged) != 1 || flagged[0] != "legacy1" {
		t.Errorf("flagged = %v, want [legacy1]", flagged)
	}
	// 无 rules 段
	if got := ReportLegacyPathRulesAtLoad(map[string]any{}); got != nil {
		t.Errorf("无 rules 应返回 nil，got %v", got)
	}
}

// TestFileGuardChecker_无访问返回nil 测试不涉及路径的工具返回 nil
func TestFileGuardChecker_无访问返回nil(t *testing.T) {
	ws := t.TempDir()
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}},
	}, ws, nil)

	if result := checker.Evaluate("some_unknown_tool", map[string]any{"x": "y"}); result != nil {
		t.Errorf("无路径访问应返回 nil，got %+v", result)
	}
}

// TestFileGuardChecker_DENY原因文案 测试 DENY 与 ASK 的 reason 差异
func TestFileGuardChecker_DENY原因文案(t *testing.T) {
	ws := t.TempDir()
	outside := filepath.Join(t.TempDir(), "d.txt")
	checker := BuildFileGuardChecker(map[string]any{
		"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "deny"}},
	}, ws, nil)

	result := checker.Evaluate("read_file", map[string]any{"file_path": outside})
	if result == nil {
		t.Fatal("应产生结果")
	}
	if result.Permission != security.PermissionLevelDeny {
		t.Errorf("Permission = %v, want DENY", result.Permission)
	}
	if !strings.Contains(result.Reason, "denied") {
		t.Errorf("Reason = %q, want 含 denied", result.Reason)
	}
}

// TestExtractPathsFromCommand 测试 legacy 模式的命令行路径抽取
func TestExtractPathsFromCommand(t *testing.T) {
	ws := t.TempDir()
	abs := filepath.Join(t.TempDir(), "x.txt")

	// 路径类命令 + 绝对路径
	got := extractPathsFromCommand("cat "+abs, ws)
	if len(got) != 1 || got[0] != abs {
		t.Errorf("got %v, want [%s]", got, abs)
	}
	// 非路径类命令不抽取
	if got := extractPathsFromCommand("echo hello", ws); len(got) != 0 {
		t.Errorf("echo 不应抽取，got %v", got)
	}
	// 相对路径按 workdir 解析
	got = extractPathsFromCommand("cat ./rel.txt", ws)
	if len(got) != 1 || got[0] != filepath.Join(ws, "rel.txt") {
		t.Errorf("相对路径解析错，got %v", got)
	}
	// 开关被跳过
	if got := extractPathsFromCommand("cat -n /tmp/a", ws); len(got) != 1 {
		t.Errorf("开关应跳过，got %v", got)
	}
	// 空命令
	if got := extractPathsFromCommand("", ws); got != nil {
		t.Errorf("空命令应返回 nil，got %v", got)
	}
}

// TestLooksLikePathLegacy 测试 legacy 路径判定
func TestLooksLikePathLegacy(t *testing.T) {
	yes := []string{"/abs/path", "./rel", "../up", `\\server\share`, `C:\win`}
	no := []string{"plain", "-flag", ""}
	for _, s := range yes {
		if !looksLikePathLegacy(s) {
			t.Errorf("looksLikePathLegacy(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikePathLegacy(s) {
			t.Errorf("looksLikePathLegacy(%q) = true, want false", s)
		}
	}
}

// TestNormalizeNative_externalDirectory迁移 测试旧 external_directory 迁移为路径规则
func TestNormalizeNative_externalDirectory迁移(t *testing.T) {
	ws := t.TempDir()
	migrated := filepath.Join(t.TempDir(), "migrated")
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled":  true,
			"defaults": map[string]any{"read": "ask"},
		},
		"external_directory": map[string]any{
			"*":      "ask",
			migrated: "allow",
		},
	}
	got := NormalizePathGuardConfig(perms, ws, nil)
	if got.Mode != "native" {
		t.Fatalf("Mode = %q, want native", got.Mode)
	}
	found := false
	for _, r := range got.Paths {
		if strings.HasPrefix(r.Path, migrated) && r.Read == security.PermissionLevelAllow {
			found = true
		}
	}
	if !found {
		t.Errorf("未迁移 external_directory 具名键，paths = %+v", got.Paths)
	}
}

// TestNormalizeNative_defaultsFromExternalStar 测试未写 defaults 时从 external["*"] 迁移
func TestNormalizeNative_defaultsFromExternalStar(t *testing.T) {
	ws := t.TempDir()
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths":   []any{map[string]any{"path": "/a/**", "match": "glob"}},
		},
		"external_directory": map[string]any{"*": "deny"},
	}
	got := NormalizePathGuardConfig(perms, ws, nil)
	if got.Defaults.Read != security.PermissionLevelDeny {
		t.Errorf("Defaults.Read = %v, want DENY（从 external[*] 迁移）", got.Defaults.Read)
	}
}

// TestTruthyAny 测试 Python bool() 语义
func TestTruthyAny(t *testing.T) {
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

// TestContainsPath 测试 containsPath 的前缀判定
func TestContainsPath(t *testing.T) {
	if !containsPath("/a/b", "/a/b/c") {
		t.Error("/a/b 应包含 /a/b/c")
	}
	if !containsPath("/a/b", "/a/b") {
		t.Error("相等应包含")
	}
	if containsPath("/a/b", "/a/bc") {
		t.Error("/a/b 不应包含 /a/bc（边界必须是 /）")
	}
	// Windows 盘符大小写不敏感
	if !containsPath(`C:\Users`, `c:/users/x`) {
		t.Error("Windows 盘符应大小写不敏感")
	}
}

// TestLevelForAction 测试三轴取值
func TestLevelForAction(t *testing.T) {
	rule := &FileGuardPathRule{
		Read:  security.PermissionLevelAllow,
		Write: security.PermissionLevelAsk,
		Exec:  security.PermissionLevelDeny,
	}
	if got := levelForAction(rule, FileActionRead); got != security.PermissionLevelAllow {
		t.Errorf("read 轴 = %v, want ALLOW", got)
	}
	if got := levelForAction(rule, FileActionWrite); got != security.PermissionLevelAsk {
		t.Errorf("write 轴 = %v, want ASK", got)
	}
	if got := levelForAction(rule, FileActionExec); got != security.PermissionLevelDeny {
		t.Errorf("exec 轴 = %v, want DENY", got)
	}
}

// TestToolDefaultAction 测试 legacy 默认轴（写工具 write，其余 read）
func TestToolDefaultAction(t *testing.T) {
	if got := toolDefaultAction("write_file", nil); got != FileActionWrite {
		t.Errorf("write_file 默认轴 = %v, want write", got)
	}
	if got := toolDefaultAction("read_file", nil); got != FileActionRead {
		t.Errorf("read_file 默认轴 = %v, want read", got)
	}
	// shell 在 legacy 下也按 read 轴（不区分 exec）
	if got := toolDefaultAction("bash", nil); got != FileActionRead {
		t.Errorf("bash 默认轴 = %v, want read", got)
	}
}

// TestExtractPathsLegacy_shell工具 测试 legacy 下 shell 工具的命令行抽取
func TestExtractPathsLegacy_shell工具(t *testing.T) {
	ws := t.TempDir()
	target := filepath.Join(ws, "s.txt")
	got := ExtractPathsLegacy("bash", map[string]any{"command": "cat " + target}, ws, nil)
	if len(got) != 1 || got[0] != target {
		t.Errorf("got %v, want [%s]", got, target)
	}
}

// TestExtractPathsLegacy_带workdir 测试 shell 的 workdir 参数生效
func TestExtractPathsLegacy_带workdir(t *testing.T) {
	ws := t.TempDir()
	sub := filepath.Join(ws, "sub")
	got := ExtractPathsLegacy("bash", map[string]any{
		"command": "cat ./a.txt",
		"workdir": sub,
	}, ws, nil)
	want := filepath.Join(sub, "a.txt")
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %v, want [%s]", got, want)
	}
}

// TestNormalizeLegacy_含fileGuardPaths 测试含 glob 项时走 native（paths 只有 prefix 时并入 legacy）
func TestNormalizeLegacy_含fileGuardPaths(t *testing.T) {
	ws := t.TempDir()
	extra := filepath.Join(t.TempDir(), "extra")
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{"path": extra, "read": "allow", "write": "allow", "exec": "ask"},
				map[string]any{"path": "/skip/**", "match": "glob"},
			},
		},
		"external_directory": map[string]any{"*": "ask"},
	}
	got := NormalizePathGuardConfig(perms, ws, nil)
	// paths 里有 glob 项 → hasNativeFileGuard 返回 true → native
	// （Python 同此行为：_has_native_file_guard 见 match==glob 即返回 True）
	if got.Mode != "native" {
		t.Fatalf("Mode = %q, want native（含 glob 项应进 native）", got.Mode)
	}
}

// TestNormalizeLegacy_仅prefix并入 测试 legacy 下 file_guard.paths 的 prefix 项被并入
func TestNormalizeLegacy_仅prefix并入(t *testing.T) {
	ws := t.TempDir()
	extra := filepath.Join(t.TempDir(), "extra")
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{"path": extra, "read": "allow", "write": "allow", "exec": "ask"},
			},
		},
		"external_directory": map[string]any{"*": "ask"},
	}
	got := NormalizePathGuardConfig(perms, ws, nil)
	// paths 全是 prefix 且存在 external_directory → legacy
	if got.Mode != "legacy" {
		t.Fatalf("Mode = %q, want legacy", got.Mode)
	}
	found := false
	for _, r := range got.Paths {
		if strings.HasPrefix(r.Path, extra) {
			found = true
		}
	}
	if !found {
		t.Errorf("未并入 file_guard.paths 的 prefix 项，paths = %+v", got.Paths)
	}
}
