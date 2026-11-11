package permissions

import (
	"strings"
	"testing"
)

// 9.90e：原 OwnerScopesPermissionContext 相关用例已删除——该结构体与
// schema.PermissionContext 重复，Scene/OwnerScopeKey 的 trim 用例已迁到
// schema/permission_test.go。

// ──────────────────────────── MatchArgs 测试 ────────────────────────────

// TestMatchArgs_命令匹配 验证 command 参数走 CommandMatcher
func TestMatchArgs_命令匹配(t *testing.T) {
	if !MatchArgs("git status", map[string]any{"command": "git status"}) {
		t.Error("command=git status 匹配 git status 应返回 true")
	}
}

// TestMatchArgs_命令不匹配 验证不匹配返回 false
func TestMatchArgs_命令不匹配(t *testing.T) {
	if MatchArgs("npm*", map[string]any{"command": "git status"}) {
		t.Error("command=git status 匹配 npm* 应返回 false")
	}
}

// TestMatchArgs_路径匹配 验证 path 参数走 PathMatcher
func TestMatchArgs_路径匹配(t *testing.T) {
	if !MatchArgs("/home/user/*", map[string]any{"path": "/home/user/file.txt"}) {
		t.Error("path=/home/user/file.txt 匹配 /home/user/* 应返回 true")
	}
}

// TestMatchArgs_URL匹配 验证 url 参数走 URLMatcher
func TestMatchArgs_URL匹配(t *testing.T) {
	if !MatchArgs("https://example.com/*", map[string]any{"url": "https://example.com/page"}) {
		t.Error("url 匹配应返回 true")
	}
}

// TestMatchArgs_通配符兜底 验证非特定 key 走 MatchWildcard
func TestMatchArgs_通配符兜底(t *testing.T) {
	if !MatchArgs("git*", map[string]any{"raw": "git status"}) {
		t.Error("raw=git status 匹配 git* 应返回 true（通配符兜底）")
	}
}

// TestMatchArgs_非字符串跳过 验证非字符串值被跳过
func TestMatchArgs_非字符串跳过(t *testing.T) {
	if MatchArgs("42", map[string]any{"count": 42}) {
		t.Error("非字符串值应被跳过，返回 false")
	}
}

// TestMatchArgs_无匹配 验证全部不匹配返回 false
func TestMatchArgs_无匹配(t *testing.T) {
	if MatchArgs("npm*", map[string]any{"command": "git status"}) {
		t.Error("无匹配时应返回 false")
	}
}

// TestMatchArgs_cmd别名 验证 cmd 也走 CommandMatcher
func TestMatchArgs_cmd别名(t *testing.T) {
	if !MatchArgs("git*", map[string]any{"cmd": "git status"}) {
		t.Error("cmd=git status 匹配 git* 应返回 true")
	}
}

// TestMatchArgs_filePath别名 验证 file_path 也走 PathMatcher
func TestMatchArgs_filePath别名(t *testing.T) {
	if !MatchArgs("/tmp/*", map[string]any{"file_path": "/tmp/test.txt"}) {
		t.Error("file_path=/tmp/test.txt 匹配 /tmp/* 应返回 true")
	}
}

// ──────────────────────────── ResolveOwnerScopeLevel 测试 ────────────────────────────

// TestResolveOwnerScopeLevel_空配置 验证 nil 返回空
func TestResolveOwnerScopeLevel_空配置(t *testing.T) {
	result := ResolveOwnerScopeLevel(nil, "read_file", map[string]any{})
	if result != "" {
		t.Fatalf("nil 配置应返回空，得到 %q", result)
	}
}

// TestResolveOwnerScopeLevel_字符串级别 验证 tools.<name> 为字符串直接返回
func TestResolveOwnerScopeLevel_字符串级别(t *testing.T) {
	scopeCfg := map[string]any{"tools": map[string]any{"read_file": "allow"}}
	result := ResolveOwnerScopeLevel(scopeCfg, "read_file", map[string]any{})
	if result != "allow" {
		t.Fatalf("应为 allow，得到 %q", result)
	}
}

// TestResolveOwnerScopeLevel_字符串级别deny 验证 deny 级别
func TestResolveOwnerScopeLevel_字符串级别deny(t *testing.T) {
	scopeCfg := map[string]any{"tools": map[string]any{"write_file": "deny"}}
	result := ResolveOwnerScopeLevel(scopeCfg, "write_file", map[string]any{})
	if result != "deny" {
		t.Fatalf("应为 deny，得到 %q", result)
	}
}

// TestResolveOwnerScopeLevel_patterns优先 验证 patterns > wildcard(*) > defaults
func TestResolveOwnerScopeLevel_patterns优先(t *testing.T) {
	scopeCfg := map[string]any{
		"tools": map[string]any{
			"bash": map[string]any{
				"patterns": map[string]any{
					"git status": "allow",
				},
				"*": "deny",
			},
		},
		"defaults": map[string]any{"*": "ask"},
	}
	// command 匹配 patterns → 返回 allow
	result := ResolveOwnerScopeLevel(scopeCfg, "bash", map[string]any{"command": "git status"})
	if result != "allow" {
		t.Fatalf("patterns 优先，应为 allow，得到 %q", result)
	}
}

// TestResolveOwnerScopeLevel_wildcard次之 验证 patterns 无匹配后走 wildcard
func TestResolveOwnerScopeLevel_wildcard次之(t *testing.T) {
	scopeCfg := map[string]any{
		"tools": map[string]any{
			"bash": map[string]any{
				"patterns": map[string]any{
					"git status": "allow",
				},
				"*": "deny",
			},
		},
	}
	// command 不匹配 patterns，走 wildcard → 返回 deny
	result := ResolveOwnerScopeLevel(scopeCfg, "bash", map[string]any{"command": "rm -rf /"})
	if result != "deny" {
		t.Fatalf("wildcard 应为 deny，得到 %q", result)
	}
}

// TestResolveOwnerScopeLevel_defaults兜底 验证无 tool 级配置走 defaults
func TestResolveOwnerScopeLevel_defaults兜底(t *testing.T) {
	scopeCfg := map[string]any{
		"defaults": map[string]any{"*": "ask"},
	}
	result := ResolveOwnerScopeLevel(scopeCfg, "read_file", map[string]any{})
	if result != "ask" {
		t.Fatalf("defaults 兜底应为 ask，得到 %q", result)
	}
}

// TestResolveOwnerScopeLevel_无匹配返回空 验证全无匹配返回空
func TestResolveOwnerScopeLevel_无匹配返回空(t *testing.T) {
	scopeCfg := map[string]any{
		"tools": map[string]any{"bash": "allow"},
	}
	result := ResolveOwnerScopeLevel(scopeCfg, "read_file", map[string]any{})
	if result != "" {
		t.Fatalf("无匹配应返回空，得到 %q", result)
	}
}

// ──────────────────────────── CheckAvatarPermission 测试 ────────────────────────────

// TestCheckAvatarPermission_空PrincipalUserID 验证无主体用户返回 deny
func TestCheckAvatarPermission_空PrincipalUserID(t *testing.T) {
	result, err := CheckAvatarPermission(map[string]any{}, "read_file", map[string]any{}, "ch1", "", "")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "deny" {
		t.Fatalf("空 principalUserID 应返回 deny，得到 %q", result)
	}
}

// TestCheckAvatarPermission_空格PrincipalUserID 验证空格等效空
func TestCheckAvatarPermission_空格PrincipalUserID(t *testing.T) {
	result, err := CheckAvatarPermission(map[string]any{}, "read_file", map[string]any{}, "ch1", "", "  ")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "deny" {
		t.Fatalf("空格 principalUserID 应返回 deny，得到 %q", result)
	}
}

// TestCheckAvatarPermission_nil配置 验证 nil 配置不会 panic
func TestCheckAvatarPermission_nil配置(t *testing.T) {
	result, err := CheckAvatarPermission(nil, "read_file", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	// nil 配置 → owner_scopes 为空 → allow
	if result != "allow" {
		t.Fatalf("nil 配置无 owner_scopes 应返回 allow，得到 %q", result)
	}
}

// TestCheckAvatarPermission_无ownerScopes 验证无 owner_scopes 返回 allow
func TestCheckAvatarPermission_无ownerScopes(t *testing.T) {
	permCfg := map[string]any{"enabled": true}
	result, err := CheckAvatarPermission(permCfg, "read_file", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "allow" {
		t.Fatalf("无 owner_scopes 应返回 allow，得到 %q", result)
	}
}

// TestCheckAvatarPermission_ownerScope允许 验证 owner_scopes 工具级 allow
func TestCheckAvatarPermission_ownerScope允许(t *testing.T) {
	permCfg := map[string]any{
		"owner_scopes": map[string]any{
			"ch1": map[string]any{
				"user1": map[string]any{
					"tools": map[string]any{
						"read_file": "allow",
					},
				},
			},
		},
	}
	result, err := CheckAvatarPermission(permCfg, "read_file", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "allow" {
		t.Fatalf("owner_scopes allow 应返回 allow，得到 %q", result)
	}
}

// TestCheckAvatarPermission_ownerScope拒绝 验证 owner_scopes 工具级 deny
func TestCheckAvatarPermission_ownerScope拒绝(t *testing.T) {
	permCfg := map[string]any{
		"owner_scopes": map[string]any{
			"ch1": map[string]any{
				"user1": map[string]any{
					"tools": map[string]any{
						"write_file": "deny",
					},
				},
			},
		},
	}
	result, err := CheckAvatarPermission(permCfg, "write_file", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "deny" {
		t.Fatalf("owner_scopes deny 应返回 deny，得到 %q", result)
	}
}

// TestCheckAvatarPermission_ask降级为deny 验证 ASK 自动降级为 DENY
func TestCheckAvatarPermission_ask降级为deny(t *testing.T) {
	permCfg := map[string]any{
		"owner_scopes": map[string]any{
			"ch1": map[string]any{
				"user1": map[string]any{
					"tools": map[string]any{
						"bash": "ask",
					},
				},
			},
		},
	}
	result, err := CheckAvatarPermission(permCfg, "bash", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "deny" {
		t.Fatalf("ask 应降级为 deny，得到 %q", result)
	}
}

// TestCheckAvatarPermission_global更严取严者 验证 global 比 owner 更严时取 global
func TestCheckAvatarPermission_global更严取严者(t *testing.T) {
	// owner = allow, global tools.bash = deny → 应取 deny
	permCfg := map[string]any{
		"tools": map[string]any{
			"bash": "deny",
		},
		"owner_scopes": map[string]any{
			"ch1": map[string]any{
				"user1": map[string]any{
					"tools": map[string]any{
						"bash": "allow",
					},
				},
			},
		},
	}
	result, err := CheckAvatarPermission(permCfg, "bash", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "deny" {
		t.Fatalf("global deny 比 owner allow 更严，应取 deny，得到 %q", result)
	}
}

// TestCheckAvatarPermission_无ownerScope配置 验证 owner 无匹配时 global 为 allow 则 allow
func TestCheckAvatarPermission_无ownerScope配置(t *testing.T) {
	// owner 无匹配（空）, global 默认 allow → allow
	permCfg := map[string]any{
		"defaults": map[string]any{"*": "allow"},
	}
	result, err := CheckAvatarPermission(permCfg, "read_file", map[string]any{}, "ch1", "", "user1")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if result != "allow" {
		t.Fatalf("global allow + owner 无匹配应返回 allow，得到 %q", result)
	}
}

// ──────────────────────────── 依赖 strings 验证 ────────────────────────────

// TestStringsImportUsed 验证 strings 包被正确使用（编译时验证）
func TestStringsImportUsed(t *testing.T) {
	_ = strings.TrimSpace
}
