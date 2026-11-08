package fileguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// TestLoadPackageSensitivePaths_十八条 测试包内敏感路径全部加载且带固定字段
func TestLoadPackageSensitivePaths_十八条(t *testing.T) {
	paths := LoadPackageSensitivePaths()
	if len(paths) != 18 {
		t.Fatalf("条数 = %d, want 18", len(paths))
	}
	for _, p := range paths {
		if p["layer"] != "builtin" {
			t.Errorf("%v: layer = %v, want builtin", p["id"], p["layer"])
		}
		for _, axis := range []string{"read", "write", "exec"} {
			if p[axis] == nil {
				t.Errorf("%v: 缺 %s 轴", p["id"], axis)
			}
		}
		if p["match"] == nil {
			t.Errorf("%v: 缺 match", p["id"])
		}
	}
}

// TestLoadPackageSensitivePaths_home展开 测试 ~ 前缀被展开为绝对路径
func TestLoadPackageSensitivePaths_home展开(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法取 home 目录")
	}
	for _, p := range LoadPackageSensitivePaths() {
		path, _ := p["path"].(string)
		if strings.Contains(path, "~") {
			t.Errorf("%v: path 未展开 ~: %q", p["id"], path)
		}
	}
	// home_ssh 应展开为 <home>/.ssh/**
	found := false
	for _, p := range LoadPackageSensitivePaths() {
		if p["id"] == "home_ssh" {
			found = true
			want := filepath.ToSlash(filepath.Join(home, ".ssh")) + "/**"
			if p["path"] != want {
				t.Errorf("home_ssh path = %v, want %v", p["path"], want)
			}
		}
	}
	if !found {
		t.Error("缺 home_ssh 条目")
	}
}

// TestLoadPackageSensitivePaths_副本语义 测试返回副本，修改不影响缓存
func TestLoadPackageSensitivePaths_副本语义(t *testing.T) {
	first := LoadPackageSensitivePaths()
	if len(first) == 0 {
		t.Fatal("无条目")
	}
	first[0]["path"] = "hacked"
	second := LoadPackageSensitivePaths()
	if second[0]["path"] == "hacked" {
		t.Error("返回的是共享对象，修改影响了缓存")
	}
}

// TestMergePackageSensitivePaths_取严 测试 overlay 的 allow 被 floor 的 deny 覆盖
func TestMergePackageSensitivePaths_取严(t *testing.T) {
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{
					"path": "~/.ssh/**", "match": "glob",
					"read": "allow", "write": "allow", "exec": "allow",
				},
			},
		},
	}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)

	var sshRule map[string]any
	for _, item := range paths {
		m, _ := item.(map[string]any)
		if m["id"] == "home_ssh" {
			sshRule = m
			break
		}
	}
	if sshRule == nil {
		t.Fatal("未找到 home_ssh 规则（应合并到同 path 的 overlay 项上）")
	}
	if sshRule["read"] != "deny" {
		t.Errorf("read = %v, want deny（内置 floor 优先于 overlay 的 allow）", sshRule["read"])
	}
	if sshRule["layer"] != "builtin" {
		t.Errorf("layer = %v, want builtin", sshRule["layer"])
	}
}

// TestMergePackageSensitivePaths_开关关闭 测试 package_builtin_rules=false 时不注入
func TestMergePackageSensitivePaths_开关关闭(t *testing.T) {
	perms := map[string]any{
		"package_builtin_rules": false,
		"file_guard":            map[string]any{"enabled": true},
	}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	if paths, _ := fg["paths"].([]any); len(paths) != 0 {
		t.Errorf("开关关闭时仍注入了 %d 条", len(paths))
	}
}

// TestMergePackageSensitivePaths_未启用不注入 测试 file_guard 未启用时不注入
func TestMergePackageSensitivePaths_未启用不注入(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": false}}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	if paths, _ := fg["paths"].([]any); len(paths) != 0 {
		t.Errorf("未启用时仍注入了 %d 条", len(paths))
	}
}

// TestMergePackageSensitivePaths_inject覆盖 测试 inject 参数覆盖 enabled 判断
func TestMergePackageSensitivePaths_inject覆盖(t *testing.T) {
	inject := true
	perms := map[string]any{"file_guard": map[string]any{"enabled": false}}
	merged := MergePackageSensitivePaths(perms, &inject)
	fg, _ := merged["file_guard"].(map[string]any)
	if paths, _ := fg["paths"].([]any); len(paths) == 0 {
		t.Error("inject=true 时应强制注入")
	}

	noInject := false
	perms2 := map[string]any{"file_guard": map[string]any{"enabled": true}}
	merged2 := MergePackageSensitivePaths(perms2, &noInject)
	fg2, _ := merged2["file_guard"].(map[string]any)
	if paths, _ := fg2["paths"].([]any); len(paths) != 0 {
		t.Error("inject=false 时不应注入")
	}
}

// TestMergePackageSensitivePaths_不修改入参 测试深拷贝
func TestMergePackageSensitivePaths_不修改入参(t *testing.T) {
	perms := map[string]any{"file_guard": map[string]any{"enabled": true}}
	_ = MergePackageSensitivePaths(perms, nil)
	fg, _ := perms["file_guard"].(map[string]any)
	if _, has := fg["paths"]; has {
		t.Error("入参被修改")
	}
}

// TestMergePackageSensitivePaths_overlay独有保留 测试 overlay 独有的 path 原样保留
func TestMergePackageSensitivePaths_overlay独有保留(t *testing.T) {
	perms := map[string]any{
		"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{
				map[string]any{"path": "/custom/only", "read": "ask"},
			},
		},
	}
	merged := MergePackageSensitivePaths(perms, nil)
	fg, _ := merged["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)

	foundCustom := false
	for _, item := range paths {
		m, _ := item.(map[string]any)
		if m["path"] == "/custom/only" {
			foundCustom = true
			if m["layer"] == "builtin" {
				t.Error("overlay 独有条目不应被标为 builtin")
			}
		}
	}
	if !foundCustom {
		t.Error("overlay 独有条目丢失")
	}
	// 内置 18 条也应都在
	if len(paths) != 19 {
		t.Errorf("总条数 = %d, want 19（18 内置 + 1 overlay 独有）", len(paths))
	}
}

// TestStricterAxis 测试取严
func TestStricterAxis(t *testing.T) {
	cases := []struct{ left, right, want string }{
		{"allow", "deny", "deny"},
		{"ask", "allow", "ask"},
		{"deny", "ask", "deny"},
		{"allow", "allow", "allow"},
		{"bogus", "allow", "ask"}, // 未知按 ask(1)，比 allow(2) 严
	}
	for _, c := range cases {
		if got := stricterAxis(c.left, c.right); got != c.want {
			t.Errorf("stricterAxis(%q,%q) = %q, want %q", c.left, c.right, got, c.want)
		}
	}
}

// TestInit_注册工厂 测试 init 注册的工厂能构造判定器
//
// fileguard 的 init 会在 security 包注册 FileGuardFactory，
// 因此 security.NewFileGuardEvaluator 应能拿到本包的实现。
func TestInit_注册工厂(t *testing.T) {
	ws := t.TempDir()
	evaluator := security.NewFileGuardEvaluator(
		map[string]any{"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}}},
		ws, nil,
	)
	if evaluator == nil {
		t.Fatal("工厂未注册或返回 nil")
	}
	if !evaluator.Enabled() {
		t.Error("Enabled() = false, want true")
	}
	if evaluator.Mode() != "native" {
		t.Errorf("Mode() = %q, want native（有 defaults 轴）", evaluator.Mode())
	}
	// 路径层关闭时应返回 nil
	if got := security.NewFileGuardEvaluator(map[string]any{}, ws, nil); got != nil {
		t.Error("未启用时应返回 nil")
	}
}

// TestBuildFileGuardChecker_禁用时nil 测试 enabled=false 时工厂返回 nil
func TestBuildFileGuardChecker_禁用时nil(t *testing.T) {
	ws := t.TempDir()
	evaluator := security.NewFileGuardEvaluator(
		map[string]any{"file_guard": map[string]any{"enabled": false}},
		ws, nil,
	)
	if evaluator != nil {
		t.Error("enabled=false 时应返回 nil")
	}
}

// TestYamlSensitiveAction_非法输入 测试缺失/非法/allow 都返回空串
func TestYamlSensitiveAction_非法输入(t *testing.T) {
	cases := []map[string]any{
		{"id": "no_action"},
		{"id": "bad_type", "action": 123},
		{"id": "blank", "action": "   "},
		{"id": "invalid", "action": "block"},
		{"id": "allow_rule", "action": "allow"},
	}
	for _, item := range cases {
		if got := yamlSensitiveAction(item); got != "" {
			t.Errorf("yamlSensitiveAction(%v) = %q, want empty", item["id"], got)
		}
	}
	if got := yamlSensitiveAction(map[string]any{"id": "ok", "action": "DENY"}); got != "deny" {
		t.Errorf("got %q, want deny（小写化）", got)
	}
}

// TestExpandHomePath 测试 ~ 展开的三种形态
func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法取 home")
	}
	home = filepath.ToSlash(home)
	if got := expandHomePath("~/.ssh/**"); got != home+"/.ssh/**" {
		t.Errorf("got %q, want %q", got, home+"/.ssh/**")
	}
	if got := expandHomePath("~"); got != home {
		t.Errorf("got %q, want %q", got, home)
	}
	// 非 ~ 开头原样
	if got := expandHomePath("/abs/path"); got != "/abs/path" {
		t.Errorf("got %q, want /abs/path", got)
	}
}
