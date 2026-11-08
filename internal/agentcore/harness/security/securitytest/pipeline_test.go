// Package securitytest 是 security 包的外部集成测试包。
//
// 放在独立包而非 security 包内，是因为 A/B/C 三段管线测试需要 import
// fileguard / netguard 触发它们的 init（注册判定器工厂），而 security 包
// 自身 import 子包会形成循环。Go 允许「包外测试」import 被测包与子包。
package securitytest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	// blank import 触发 fileguard / netguard 的 init：
	// 注册 FileGuardFactory / NetGuardFactory 与配置注入函数。
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/netguard"
)

const (
	allow = security.PermissionLevelAllow
	ask   = security.PermissionLevelAsk
	deny  = security.PermissionLevelDeny
)

// ──────────────────────────── A/B/C 三段管线 ────────────────────────────

// TestCheckPermission_B段路径防护 测试 workspace 外路径被 B 段抬到 ASK/DENY
func TestCheckPermission_B段路径防护(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")

	engine := security.NewPermissionEngine(
		map[string]any{"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}}},
		nil, "", workspace,
	)
	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": outside})
	if result.Permission != ask {
		t.Errorf("Permission = %v, want ASK（外部路径）rule=%s", result.Permission, result.MatchedRule)
	}
	if len(result.ExternalPaths) == 0 {
		t.Error("ExternalPaths 为空，B 段未生效")
	}
}

// TestCheckPermission_C段网络防护 测试黑名单 URL 被 C 段拒绝
func TestCheckPermission_C段网络防护(t *testing.T) {
	engine := security.NewPermissionEngine(
		map[string]any{
			"net_guard": map[string]any{
				"enabled": true,
				"urls":    map[string]any{"evil.com": "deny"},
			},
		},
		nil, "", t.TempDir(),
	)
	result := engine.CheckPermission(context.Background(), "mcp_fetch_webpage", map[string]any{"url": "https://evil.com/x"})
	if result.Permission != deny {
		t.Errorf("Permission = %v, want DENY, rule=%s", result.Permission, result.MatchedRule)
	}
	if !strings.Contains(result.MatchedRule, "net_guard") {
		t.Errorf("MatchedRule = %q, want 含 net_guard", result.MatchedRule)
	}
}

// TestCheckPermission_三段取最严 测试 A 段 allow + B 段 deny → deny
func TestCheckPermission_三段取最严(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.txt")
	engine := security.NewPermissionEngine(
		map[string]any{
			"tools":      map[string]any{"read_file": "allow"},
			"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "deny"}},
		},
		nil, "", workspace,
	)
	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": outside})
	if result.Permission != deny {
		t.Errorf("Permission = %v, want DENY（B 段更严），rule=%s", result.Permission, result.MatchedRule)
	}
}

// TestCheckPermission_B段无意见不改A段 测试内部路径读不抬升
func TestCheckPermission_B段无意见不改A段(t *testing.T) {
	workspace := t.TempDir()
	inside := filepath.Join(workspace, "ok.txt")
	engine := security.NewPermissionEngine(
		map[string]any{
			"tools": map[string]any{"read_file": "allow"},
			"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"},
				"workspace": map[string]any{"read": "allow"}},
		},
		nil, "", workspace,
	)
	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": inside})
	if result.Permission != allow {
		t.Errorf("Permission = %v, want ALLOW（内部路径），rule=%s", result.Permission, result.MatchedRule)
	}
}

// TestCheckPermission_matchedRule拼接顺序 测试 B/C 段的 rule 用 | 追加
func TestCheckPermission_matchedRule拼接顺序(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "y.txt")
	engine := security.NewPermissionEngine(
		map[string]any{
			"tools":      map[string]any{"read_file": "ask"},
			"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}},
		},
		nil, "", workspace,
	)
	result := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": outside})
	parts := strings.Split(result.MatchedRule, "|")
	if len(parts) < 2 {
		t.Errorf("MatchedRule = %q, want A|B 两段", result.MatchedRule)
	}
	if !strings.Contains(result.MatchedRule, "file_guard") {
		t.Errorf("MatchedRule = %q, want 含 file_guard", result.MatchedRule)
	}
}

// TestUpdateTrustedDirs 测试受信目录注入后外部路径不再 ASK
func TestUpdateTrustedDirs(t *testing.T) {
	workspace := t.TempDir()
	trusted := t.TempDir()
	target := filepath.Join(trusted, "a.txt")

	engine := security.NewPermissionEngine(
		map[string]any{"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}}},
		nil, "", workspace,
	)
	// 加 trusted 前应有路径层意见
	before := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": target})
	engine.UpdateTrustedDirs([]string{trusted})
	after := engine.CheckPermission(context.Background(), "read_file", map[string]any{"file_path": target})

	if after.Permission == deny {
		t.Errorf("加入 trusted_dirs 后仍 DENY（%s）", after.MatchedRule)
	}
	if len(after.ExternalPaths) != 0 {
		t.Errorf("trusted 内路径不应有 ExternalPaths，got %v", after.ExternalPaths)
	}
	_ = before
	if got := engine.TrustedDirs(); len(got) != 1 || got[0] != trusted {
		t.Errorf("TrustedDirs() = %v, want [%s]", got, trusted)
	}
}

// TestPreparePermissionsForEngine_注入内置规则 测试内置规则被注入且带 layer
func TestPreparePermissionsForEngine_注入内置规则(t *testing.T) {
	got := security.PreparePermissionsForEngine(map[string]any{})
	rules, ok := got["rules"].([]any)
	if !ok || len(rules) != 17 {
		t.Fatalf("rules 条数 = %d, want 17", len(rules))
	}
	first, _ := rules[0].(map[string]any)
	if first["layer"] != "builtin" {
		t.Errorf("首条 layer = %v, want builtin", first["layer"])
	}
}

// TestPreparePermissionsForEngine_开关关闭 测试 package_builtin_rules=false 时不注入
func TestPreparePermissionsForEngine_开关关闭(t *testing.T) {
	got := security.PreparePermissionsForEngine(map[string]any{"package_builtin_rules": false})
	rules, _ := got["rules"].([]any)
	if len(rules) != 0 {
		t.Errorf("开关关闭时 rules = %d 条, want 0", len(rules))
	}
}

// TestPreparePermissionsForEngine_注入敏感路径 测试 file_guard.enabled 时注入敏感路径
func TestPreparePermissionsForEngine_注入敏感路径(t *testing.T) {
	got := security.PreparePermissionsForEngine(map[string]any{"file_guard": map[string]any{"enabled": true}})
	fg, _ := got["file_guard"].(map[string]any)
	paths, _ := fg["paths"].([]any)
	if len(paths) != 18 {
		t.Errorf("敏感路径 = %d 条, want 18", len(paths))
	}
}

// TestPreparePermissionsForEngine_注入netUrls 测试 net_guard.enabled 时注入内置黑名单
func TestPreparePermissionsForEngine_注入netUrls(t *testing.T) {
	got := security.PreparePermissionsForEngine(map[string]any{"net_guard": map[string]any{"enabled": true}})
	ng, _ := got["net_guard"].(map[string]any)
	urls, _ := ng["urls"].(map[string]any)
	if len(urls) != 4 {
		t.Errorf("net_urls = %d 条, want 4", len(urls))
	}
	if urls["169.254.169.254"] != "deny" {
		t.Errorf("169.254.169.254 = %v, want deny", urls["169.254.169.254"])
	}
}

// TestPreparePermissionsForEngine_不重复注入 测试已有 builtin 层时不再注入
func TestPreparePermissionsForEngine_不重复注入(t *testing.T) {
	once := security.PreparePermissionsForEngine(map[string]any{})
	twice := security.PreparePermissionsForEngine(once)
	r1, _ := once["rules"].([]any)
	r2, _ := twice["rules"].([]any)
	if len(r1) != len(r2) {
		t.Errorf("二次 prepare 后 %d != %d，规则被重复注入", len(r2), len(r1))
	}
}

// TestCheckPermission_fallback走ASK 测试无配置无规则时 fallback ASK
func TestCheckPermission_fallback走ASK(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{}, nil, "", t.TempDir())
	result := engine.CheckPermission(context.Background(), "some_unknown_tool", map[string]any{})
	if result.Permission != ask {
		t.Errorf("Permission = %v, want ASK（无配置 fallback）", result.Permission)
	}
}

// TestCheckPermission_禁用短路 测试 enabled=false 时直接允许
func TestCheckPermission_禁用短路(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{"enabled": false}, nil, "", "")
	result := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "rm -rf /"})
	if result.Permission != allow {
		t.Errorf("Permission = %v, want ALLOW（系统禁用）", result.Permission)
	}
}

// TestNewFileGuardEvaluator_导出包装 测试 security 侧的导出构造入口
func TestNewFileGuardEvaluator_导出包装(t *testing.T) {
	ws := t.TempDir()
	ev := security.NewFileGuardEvaluator(
		map[string]any{"file_guard": map[string]any{"enabled": true}},
		ws, nil)
	if ev == nil {
		t.Fatal("应返回判定器")
	}
	if !ev.Enabled() {
		t.Error("Enabled() = false")
	}
}

// TestNewNetGuardEvaluator_导出包装 测试 security 侧的导出构造入口
func TestNewNetGuardEvaluator_导出包装(t *testing.T) {
	ev := security.NewNetGuardEvaluator(map[string]any{
		"net_guard": map[string]any{"enabled": true},
	})
	if ev == nil {
		t.Fatal("应返回判定器")
	}
	if got := ev.Evaluate("read_file", map[string]any{}); got != nil {
		t.Errorf("非 fetch 工具应返回 nil，got %+v", got)
	}
}

// TestEvaluateGlobalPolicyDirectly_含B段 测试 includeExternalDirectory 时纳入路径层
func TestEvaluateGlobalPolicyDirectly_含B段(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.txt")
	engine := security.NewPermissionEngine(
		map[string]any{"file_guard": map[string]any{"enabled": true, "defaults": map[string]any{"read": "ask"}}},
		nil, "", workspace)
	level, rule, err := engine.EvaluateGlobalPolicyDirectly("read_file", map[string]any{"file_path": outside}, true)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if level != ask {
		t.Errorf("level = %v, want ASK, rule=%s", level, rule)
	}
	if !strings.Contains(rule, "file_guard") {
		t.Errorf("rule = %q, want 含 file_guard", rule)
	}
}

// TestCheckToolPermissionDirectly 测试直接检查（不短路）
func TestCheckToolPermissionDirectly(t *testing.T) {
	engine := security.NewPermissionEngine(
		map[string]any{"enabled": false}, nil, "", t.TempDir())
	// enabled=false 时 CheckPermission 会允许，但 Directly 不受影响
	level, _, err := engine.CheckToolPermissionDirectly("bash", map[string]any{"command": "rm -rf /"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if level == allow {
		t.Error("Directly 应绕过 enabled 开关，不应直接 ALLOW")
	}
}

// TestPermissionEngine_UpdateConfig 测试热更新后判定器重建
func TestPermissionEngine_UpdateConfig(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{}, nil, "", t.TempDir())
	engine.UpdateConfig(map[string]any{
		"net_guard": map[string]any{"enabled": true, "urls": map[string]any{"evil.com": "deny"}},
	})
	got := engine.CheckPermission(context.Background(), "mcp_fetch_webpage",
		map[string]any{"url": "https://evil.com/x"})
	if got.Permission != deny {
		t.Errorf("Permission = %v, want DENY（热更新后 C 段生效）", got.Permission)
	}
}

// TestPermissionEngine_SetPermissionChecksActive 测试宿主关闭校验时放行
func TestPermissionEngine_SetPermissionChecksActive(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{}, nil, "", t.TempDir())
	engine.SetPermissionChecksActive(func() bool { return false })
	got := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "rm -rf /"})
	if got.Permission != allow {
		t.Errorf("Permission = %v, want ALLOW（宿主关闭校验）", got.Permission)
	}
}

// TestPermissionEngine_SetSceneHook 测试场景钩子短路
func TestPermissionEngine_SetSceneHook(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{}, nil, "", t.TempDir())
	engine.SetSceneHook(func(in security.PermissionSceneHookInput) ([]string, error) {
		return []string{"approve"}, nil
	})
	got := engine.CheckPermission(context.Background(), "bash", map[string]any{"command": "rm -rf /"})
	if got.Permission != allow {
		t.Errorf("Permission = %v, want ALLOW（钩子放行）", got.Permission)
	}
	if got.MatchedRule != "scene_hook" {
		t.Errorf("MatchedRule = %q, want scene_hook", got.MatchedRule)
	}

	// reject 分支
	engine.SetSceneHook(func(in security.PermissionSceneHookInput) ([]string, error) {
		return []string{"reject", "自定义拒绝原因"}, nil
	})
	got = engine.CheckPermission(context.Background(), "bash", map[string]any{})
	if got.Permission != deny {
		t.Errorf("Permission = %v, want DENY（钩子拒绝）", got.Permission)
	}
	if got.Reason != "自定义拒绝原因" {
		t.Errorf("Reason = %q, want 自定义拒绝原因", got.Reason)
	}
}

// TestUpdateTrustedDirs_空串过滤 测试空串与空白目录被过滤
func TestUpdateTrustedDirs_空串过滤(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{}, nil, "", t.TempDir())
	engine.UpdateTrustedDirs([]string{"", "   ", "/tmp/a"})
	got := engine.TrustedDirs()
	if len(got) != 1 {
		t.Errorf("TrustedDirs() = %v, want 1 项", got)
	}
}

// TestPreparePermissionsForEngine_补legacy_action 测试无 action 的规则按 severity 补齐
func TestPreparePermissionsForEngine_补legacy_action(t *testing.T) {
	got := security.PreparePermissionsForEngine(map[string]any{
		"package_builtin_rules": false, // 跳过内置注入，只看 action 补齐
		"permission_mode":       "strict",
		"rules": []any{
			map[string]any{"id": "r_critical", "severity": "CRITICAL", "tools": []any{"bash"}},
			map[string]any{"id": "r_high", "severity": "HIGH", "tools": []any{"bash"}},
			map[string]any{"id": "r_low", "severity": "LOW", "tools": []any{"bash"}},
			map[string]any{"id": "r_nosev", "tools": []any{"bash"}},
			map[string]any{"id": "r_haveaction", "action": "deny", "severity": "LOW"},
		},
	})
	rules, _ := got["rules"].([]any)
	if len(rules) != 5 {
		t.Fatalf("rules = %d 条, want 5", len(rules))
	}
	byID := map[string]string{}
	for _, r := range rules {
		m, _ := r.(map[string]any)
		id, _ := m["id"].(string)
		a, _ := m["action"].(string)
		byID[id] = a
	}
	// strict 模式：CRITICAL→deny、HIGH→ask、LOW→allow
	if byID["r_critical"] != "deny" {
		t.Errorf("r_critical action = %q, want deny（strict）", byID["r_critical"])
	}
	if byID["r_high"] != "ask" {
		t.Errorf("r_high action = %q, want ask", byID["r_high"])
	}
	if byID["r_low"] != "allow" {
		t.Errorf("r_low action = %q, want allow", byID["r_low"])
	}
	// 无 severity 时保持空（不写 action）
	if byID["r_nosev"] != "" {
		t.Errorf("r_nosev action = %q, want 空", byID["r_nosev"])
	}
	// 已有 action 时不被覆盖
	if byID["r_haveaction"] != "deny" {
		t.Errorf("r_haveaction action = %q, want deny（保持原值）", byID["r_haveaction"])
	}
}

// TestPreparePermissionsForEngine_normal模式 测试 normal 模式下的 severity 映射差异
func TestPreparePermissionsForEngine_normal模式(t *testing.T) {
	got := security.PreparePermissionsForEngine(map[string]any{
		"package_builtin_rules": false,
		"rules": []any{
			map[string]any{"id": "r_critical", "severity": "CRITICAL", "tools": []any{"bash"}},
			map[string]any{"id": "r_low", "severity": "LOW", "tools": []any{"bash"}},
		},
	})
	rules, _ := got["rules"].([]any)
	for _, r := range rules {
		m, _ := r.(map[string]any)
		if m["id"] == "r_critical" && m["action"] != "ask" {
			t.Errorf("normal 模式 CRITICAL action = %v, want ask", m["action"])
		}
		if m["id"] == "r_low" && m["action"] != "allow" {
			t.Errorf("normal 模式 LOW action = %v, want allow", m["action"])
		}
	}
}

// TestUpdateTrustedDirs_波浪号展开 测试 ~ 前缀目录被展开
func TestUpdateTrustedDirs_波浪号展开(t *testing.T) {
	engine := security.NewPermissionEngine(map[string]any{}, nil, "", t.TempDir())
	engine.UpdateTrustedDirs([]string{"~/mydir"})
	got := engine.TrustedDirs()
	if len(got) != 1 {
		t.Fatalf("TrustedDirs() = %v", got)
	}
	if strings.Contains(got[0], "~") {
		t.Errorf("未展开 ~：%q", got[0])
	}
}
