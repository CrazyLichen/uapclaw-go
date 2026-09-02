package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEvaluateTieredPolicy_无配置返回默认ASK 测试无任何权限配置时返回 ASK
func TestEvaluateTieredPolicy_无配置返回默认ASK(t *testing.T) {
	config := map[string]any{}
	level, rule := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/tmp/test.txt"})
	assert.Equal(t, PermissionLevelAsk, level)
	assert.Contains(t, rule, "fallback")
}

// TestEvaluateTieredPolicy_整工具deny短路 测试 tools 级别 deny 立即短路返回
func TestEvaluateTieredPolicy_整工具deny短路(t *testing.T) {
	config := map[string]any{
		"tools": map[string]any{
			"read_file": "deny",
		},
	}
	level, rule := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/tmp/test.txt"})
	assert.Equal(t, PermissionLevelDeny, level)
	assert.Contains(t, rule, "tools.read_file")
}

// TestEvaluateTieredPolicy_defaults允许 测试 defaults.*=allow 时返回 ALLOW
func TestEvaluateTieredPolicy_defaults允许(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"*": "allow",
		},
	}
	level, _ := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/tmp/test.txt"})
	assert.Equal(t, PermissionLevelAllow, level)
}

// TestEvaluateTieredPolicy_内置规则CRITICAL普通模式 测试内置规则 CRITICAL + normal 模式 → ASK
// 内置规则 shell_fs_recursive_or_forced_delete 匹配 rm -rf，severity=CRITICAL。
// 在 normal 模式下，CRITICAL → ASK。
func TestEvaluateTieredPolicy_内置规则CRITICAL普通模式(t *testing.T) {
	config := map[string]any{
		"permission_mode": "normal",
		"rules": []any{
			map[string]any{
				"id":        "test_critical_rule",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "rm -rf",
				"severity":  "CRITICAL",
			},
		},
	}
	level, rule := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "rm -rf /tmp/xyz"})
	// 内置规则 CRITICAL + normal → ASK
	assert.Equal(t, PermissionLevelAsk, level)
	assert.Contains(t, rule, "builtin")
}

// TestEvaluateTieredPolicy_内置规则CRITICAL严格模式 测试内置规则 CRITICAL + strict 模式 → DENY
// 内置规则 shell_fs_recursive_or_forced_delete 匹配 rm -rf，severity=CRITICAL。
// 在 strict 模式下，CRITICAL → DENY。
func TestEvaluateTieredPolicy_内置规则CRITICAL严格模式(t *testing.T) {
	config := map[string]any{
		"permission_mode": "strict",
		"rules": []any{
			map[string]any{
				"id":        "test_critical_strict",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "rm -rf",
				"severity":  "CRITICAL",
			},
		},
	}
	level, rule := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "rm -rf /tmp/xyz"})
	// 内置规则 CRITICAL + strict → DENY
	assert.Equal(t, PermissionLevelDeny, level)
	assert.Contains(t, rule, "builtin")
}

// TestEvaluateTieredPolicy_approval覆盖匹配 测试 approval_overrides 匹配后返回 ALLOW
func TestEvaluateTieredPolicy_approval覆盖匹配(t *testing.T) {
	config := map[string]any{
		"approval_overrides": []any{
			map[string]any{
				"id":        "allow_ls",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "ls",
				"action":    "allow",
			},
		},
	}
	level, rule := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "ls"})
	assert.Equal(t, PermissionLevelAllow, level)
	assert.Contains(t, rule, "approval_overrides")
}

// TestEvaluateTieredPolicy_shell管道命令 测试 shell 管道命令。
// 注意：管道不是 shellAstFloor 检查的 5 种风险结构之一，
// 且 evaluate_tiered_policy 本身不调用 maybe_escalate_shell_operators。
// 管道命令含多子命令时，会逐个评估后聚合。
func TestEvaluateTieredPolicy_shell管道命令(t *testing.T) {
	config := map[string]any{
		"defaults": map[string]any{
			"*": "allow",
		},
	}
	level, _ := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "ls | grep foo"})
	// 管道拆为 2 个子命令，每个都 defaults.*=allow → 允许
	assert.Equal(t, PermissionLevelAllow, level)
}

// TestEvaluateTieredPolicy_shell元字符加approval覆盖 测试 shell 元字符 + approval_override → ALLOW 不升级
func TestEvaluateTieredPolicy_shell元字符加approval覆盖(t *testing.T) {
	config := map[string]any{
		"approval_overrides": []any{
			map[string]any{
				"id":        "allow_git_status_pipe",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "re:^git\\s+status$",
				"action":    "allow",
			},
		},
	}
	// approval_overrides 匹配后返回 ALLOW，matchedRule 带 approval_overrides 前缀
	// MaybeEscalateShellOperators 检测到 approval_override 匹配时不升级
	// 注意：此命令不含元字符，所以不会触发 MaybeEscalateShellOperators
	level, _ := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "git status"})
	assert.Equal(t, PermissionLevelAllow, level)
}

// TestStrictest_最严格权限 测试 Strictest 返回最严格的权限级别
func TestStrictest_最严格权限(t *testing.T) {
	assert.Equal(t, PermissionLevelAsk, Strictest(PermissionLevelAllow, PermissionLevelAsk))
	assert.Equal(t, PermissionLevelDeny, Strictest(PermissionLevelDeny, PermissionLevelAllow))
	assert.Equal(t, PermissionLevelDeny, Strictest(PermissionLevelDeny, PermissionLevelAsk, PermissionLevelAllow))
	assert.Equal(t, PermissionLevelAllow, Strictest(PermissionLevelAllow))
	assert.Equal(t, PermissionLevelAsk, Strictest()) // 无参数默认 ASK
}

// TestSeverityToDecision_完整映射 测试 SeverityToDecision 所有严重级别映射
func TestSeverityToDecision_完整映射(t *testing.T) {
	tests := []struct {
		name     string
		severity string
		mode     string
		want     PermissionLevel
	}{
		{"LOW普通模式", "LOW", "normal", PermissionLevelAllow},
		{"MEDIUM普通模式", "MEDIUM", "normal", PermissionLevelAllow},
		{"MEDIUM严格模式", "MEDIUM", "strict", PermissionLevelAsk},
		{"HIGH普通模式", "HIGH", "normal", PermissionLevelAsk},
		{"CRITICAL普通模式", "CRITICAL", "normal", PermissionLevelAsk},
		{"CRITICAL严格模式", "CRITICAL", "strict", PermissionLevelDeny},
		{"未知级别默认ASK", "UNKNOWN", "normal", PermissionLevelAsk},
		{"空模式默认normal", "HIGH", "", PermissionLevelAsk},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SeverityToDecision(tt.severity, tt.mode)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMaybeEscalateShellOperators_bash元字符升级 测试 bash + ALLOW + 含 ; → ASK
func TestMaybeEscalateShellOperators_bash元字符升级(t *testing.T) {
	result := MaybeEscalateShellOperators(
		"bash",
		map[string]any{"command": "echo hello; echo world"},
		PermissionLevelAllow,
		"rules[test_rule]",
	)
	assert.Equal(t, PermissionLevelAsk, result)
}

// TestMaybeEscalateShellOperators_bash元字符加approval覆盖不升级 测试 bash + ALLOW + approval_override → 保持 ALLOW
func TestMaybeEscalateShellOperators_bash元字符加approval覆盖不升级(t *testing.T) {
	result := MaybeEscalateShellOperators(
		"bash",
		map[string]any{"command": "echo hello; echo world"},
		PermissionLevelAllow,
		"tiered_policy:approval_overrides:allow_echo",
	)
	assert.Equal(t, PermissionLevelAllow, result)
}

// TestMaybeEscalateShellOperators_非shell工具不升级 测试非 shell 工具不触发升级
func TestMaybeEscalateShellOperators_非shell工具不升级(t *testing.T) {
	result := MaybeEscalateShellOperators(
		"read_file",
		map[string]any{"path": "/tmp/test.txt"},
		PermissionLevelAllow,
		"defaults.*",
	)
	assert.Equal(t, PermissionLevelAllow, result)
}

// TestMaybeEscalateShellOperators_非ALLOW不升级 测试权限非 ALLOW 时不触发升级
func TestMaybeEscalateShellOperators_非ALLOW不升级(t *testing.T) {
	result := MaybeEscalateShellOperators(
		"bash",
		map[string]any{"command": "echo hello; echo world"},
		PermissionLevelAsk,
		"rules[test_rule]",
	)
	assert.Equal(t, PermissionLevelAsk, result)
}

// TestMatchedRuleUsesApprovalOverride_判断 测试 matchedRule 是否来自 approval_overrides
func TestMatchedRuleUsesApprovalOverride_判断(t *testing.T) {
	assert.True(t, MatchedRuleUsesApprovalOverride("tiered_policy:approval_overrides:allow_git"))
	assert.True(t, MatchedRuleUsesApprovalOverride("tiered_policy:approval_overrides:allow_ls+allow_git"))
	assert.False(t, MatchedRuleUsesApprovalOverride("rules[test_rule]"))
	assert.False(t, MatchedRuleUsesApprovalOverride("builtin[some_builtin]"))
	assert.False(t, MatchedRuleUsesApprovalOverride(""))
}

// TestRuleToolsCategoryConsistent_同类 测试同类工具返回 true
func TestRuleToolsCategoryConsistent_同类(t *testing.T) {
	assert.True(t, RuleToolsCategoryConsistent([]string{"bash", "mcp_exec_command"}))   // 同为 shell
	assert.True(t, RuleToolsCategoryConsistent([]string{"read_file", "write_file"}))   // 同为 path
	assert.True(t, RuleToolsCategoryConsistent([]string{"bash"}))                      // 单个工具
}

// TestRuleToolsCategoryConsistent_混合 测试混合工具类别返回 false
func TestRuleToolsCategoryConsistent_混合(t *testing.T) {
	assert.False(t, RuleToolsCategoryConsistent([]string{"bash", "read_file"}))         // shell + path
	assert.False(t, RuleToolsCategoryConsistent([]string{"read_file", "mcp_fetch_webpage"})) // path + network
	assert.False(t, RuleToolsCategoryConsistent([]string{}))                            // 空列表
	assert.False(t, RuleToolsCategoryConsistent([]string{"unknown_tool"}))              // 未知类别
}

// TestShellPatternMatches_通配符 测试通配符模式匹配
func TestShellPatternMatches_通配符(t *testing.T) {
	assert.True(t, shellPatternMatches("ls *", "ls -la"))
	assert.True(t, shellPatternMatches("ls", "ls"))
	assert.False(t, shellPatternMatches("rm", "ls"))
	assert.False(t, shellPatternMatches("", "ls"))
	assert.False(t, shellPatternMatches("ls", ""))
}

// TestShellPatternMatches_正则 测试正则模式匹配
func TestShellPatternMatches_正则(t *testing.T) {
	assert.True(t, shellPatternMatches("re:^git\\s+status$", "git status"))
	assert.False(t, shellPatternMatches("re:^git\\s+status$", "git status --verbose"))
	assert.True(t, shellPatternMatches("re:^git\\s+.*", "git commit -m 'test'"))
	assert.False(t, shellPatternMatches("re:invalid[", "anything")) // 非法正则不 panic
}

// TestPathPatternMatches_基本 测试路径模式匹配
func TestPathPatternMatches_基本(t *testing.T) {
	assert.True(t, pathPatternMatches("re:\\.env$", "/home/user/.env"))
	assert.False(t, pathPatternMatches("re:\\.env$", "/home/user/config.yaml"))
	assert.False(t, pathPatternMatches("", "/tmp/test"))
	assert.False(t, pathPatternMatches("/tmp/test", ""))
}

// TestBaselineLevel_整工具deny 测试 tools 配置 deny 返回 DENY
func TestBaselineLevel_整工具deny(t *testing.T) {
	toolsCfg := map[string]any{
		"bash": "deny",
	}
	level, rule := baselineLevel(toolsCfg, "bash")
	assert.Equal(t, PermissionLevelDeny, level)
	assert.Equal(t, "tools.bash", rule)
}

// TestBaselineLevel_无配置 测试无 tools 配置返回 ALLOW
func TestBaselineLevel_无配置(t *testing.T) {
	level, rule := baselineLevel(nil, "bash")
	assert.Equal(t, PermissionLevelAllow, level)
	assert.Empty(t, rule)
}

// TestBaselineLevel_工具未配置 测试工具未在 tools 配置中返回 ALLOW
func TestBaselineLevel_工具未配置(t *testing.T) {
	toolsCfg := map[string]any{
		"bash": "deny",
	}
	level, rule := baselineLevel(toolsCfg, "read_file")
	assert.Equal(t, PermissionLevelAllow, level)
	assert.Empty(t, rule)
}

// TestBaselineLevel_dict类型星号键 测试 dict 类型配置取 * 键
func TestBaselineLevel_dict类型星号键(t *testing.T) {
	toolsCfg := map[string]any{
		"read_file": map[string]any{
			"*": "ask",
		},
	}
	level, rule := baselineLevel(toolsCfg, "read_file")
	assert.Equal(t, PermissionLevelAsk, level)
	assert.Equal(t, "tools.read_file.*", rule)
}

// TestEvaluateTieredPolicy_用户规则deny短路 测试用户规则 action=deny 短路返回
func TestEvaluateTieredPolicy_用户规则deny短路(t *testing.T) {
	config := map[string]any{
		"rules": []any{
			map[string]any{
				"id":        "deny_env",
				"tools":     []string{"read_file"},
				"match_type": "path",
				"pattern":   "re:\\.env$",
				"action":    "deny",
			},
		},
	}
	level, rule := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/home/user/.env"})
	assert.Equal(t, PermissionLevelDeny, level)
	assert.Contains(t, rule, "deny")
}

// TestEvaluateTieredPolicy_路径工具匹配规则 测试路径工具匹配规则
func TestEvaluateTieredPolicy_路径工具匹配规则(t *testing.T) {
	config := map[string]any{
		"rules": []any{
			map[string]any{
				"id":        "ask_tmp",
				"tools":     []string{"read_file"},
				"match_type": "path",
				"pattern":   "/tmp/*",
				"severity":  "HIGH",
			},
		},
	}
	level, rule := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/tmp/test.txt"})
	assert.Equal(t, PermissionLevelAsk, level)
	assert.Contains(t, rule, "rules")
}

// TestEvaluateTieredPolicy_规则不匹配 测试规则 pattern 不匹配时不命中
func TestEvaluateTieredPolicy_规则不匹配(t *testing.T) {
	config := map[string]any{
		"rules": []any{
			map[string]any{
				"id":        "deny_env",
				"tools":     []string{"read_file"},
				"match_type": "path",
				"pattern":   "re:\\.env$",
				"action":    "deny",
			},
		},
	}
	// 访问非 .env 文件，规则不命中，走 fallback
	level, _ := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/home/user/config.yaml"})
	assert.Equal(t, PermissionLevelAsk, level) // fallback=ASK
}

// TestStrictest_重复级别 测试 Strictest 重复级别
func TestStrictest_重复级别(t *testing.T) {
	assert.Equal(t, PermissionLevelDeny, Strictest(PermissionLevelDeny, PermissionLevelDeny))
	assert.Equal(t, PermissionLevelAsk, Strictest(PermissionLevelAsk, PermissionLevelAsk))
	assert.Equal(t, PermissionLevelAllow, Strictest(PermissionLevelAllow, PermissionLevelAllow))
}

// TestShellPatternMatches_反斜杠规范化 测试反斜杠路径规范化
func TestShellPatternMatches_反斜杠规范化(t *testing.T) {
	// 正则模式下命令中的反斜杠会被规范化为 /
	// re:^dir/sub$ 匹配 dir/sub
	assert.True(t, shellPatternMatches("re:^dir/sub$", "dir/sub"))
	// Windows 路径 C:\Users → C:/Users
	assert.True(t, shellPatternMatches("re:^C:/Users", "C:\\Users\\test"))
}

// TestPathPatternMatches_通配符 测试路径通配符匹配
func TestPathPatternMatches_通配符(t *testing.T) {
	assert.True(t, pathPatternMatches("/tmp/*", "/tmp/test.txt"))
	assert.False(t, pathPatternMatches("/etc/*", "/tmp/test.txt"))
}

// TestEvaluateTieredPolicy_网络工具无参数规则 测试网络类工具不匹配参数规则
func TestEvaluateTieredPolicy_网络工具无参数规则(t *testing.T) {
	config := map[string]any{
		"rules": []any{
			map[string]any{
				"id":        "deny_url",
				"tools":     []string{"mcp_fetch_webpage"},
				"match_type": "url",
				"pattern":   "re:evil\\.com",
				"severity":  "CRITICAL",
			},
		},
	}
	// 网络类工具不匹配参数级规则（产品设计）
	level, _ := EvaluateTieredPolicy(config, "mcp_fetch_webpage", map[string]any{"url": "https://evil.com"})
	assert.Equal(t, PermissionLevelAsk, level) // 走 fallback
}

// TestRuleToolsCategoryConsistent_网络同类 测试网络类工具同类
func TestRuleToolsCategoryConsistent_网络同类(t *testing.T) {
	assert.True(t, RuleToolsCategoryConsistent([]string{"mcp_fetch_webpage", "mcp_free_search"}))
}

// TestShellPatternMatches_管道分割回退 测试正则管道分割回退
func TestShellPatternMatches_管道分割回退(t *testing.T) {
	// 当正则包含 | 且整体编译失败时，按 | 分割逐个尝试
	assert.True(t, shellPatternMatches("re:git status|git log", "git status"))
	assert.True(t, shellPatternMatches("re:git status|git log", "git log"))
}

// TestEvaluateTieredPolicy_shell子命令聚合 测试 shell 简单命令含子命令时逐个评估
func TestEvaluateTieredPolicy_shell子命令聚合(t *testing.T) {
	config := map[string]any{
		"rules": []any{
			map[string]any{
				"id":        "deny_rm",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "rm *", // 通配符匹配 "rm -rf /" 等
				"action":    "deny",
			},
		},
	}
	// "echo hello; rm -rf /" 含子命令，逐个评估，rm 命中 deny
	level, rule := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "echo hello; rm -rf /"})
	require.NotEqual(t, PermissionLevelAllow, level, "含 deny 规则的命令不应允许")
	assert.Contains(t, rule, "deny")
}

// TestEvaluateTieredPolicy_工具配置allow 测试 tools 配置 allow
func TestEvaluateTieredPolicy_工具配置allow(t *testing.T) {
	config := map[string]any{
		"tools": map[string]any{
			"read_file": "allow",
		},
	}
	level, _ := EvaluateTieredPolicy(config, "read_file", map[string]any{"path": "/tmp/test.txt"})
	assert.Equal(t, PermissionLevelAllow, level)
}

// TestSeverityToDecision_大小写不敏感 测试 severity 和 mode 大小写不敏感
func TestSeverityToDecision_大小写不敏感(t *testing.T) {
	assert.Equal(t, PermissionLevelAllow, SeverityToDecision("low", "NORMAL"))
	assert.Equal(t, PermissionLevelDeny, SeverityToDecision("critical", "STRICT"))
	assert.Equal(t, PermissionLevelAsk, SeverityToDecision("High", "Normal"))
}

// TestEvaluateTieredPolicy_approval覆盖优先于规则命中 测试 approval_overrides 优先于规则命中
func TestEvaluateTieredPolicy_approval覆盖优先于规则命中(t *testing.T) {
	config := map[string]any{
		"rules": []any{
			map[string]any{
				"id":        "ask_ls",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "ls",
				"severity":  "HIGH",
			},
		},
		"approval_overrides": []any{
			map[string]any{
				"id":        "allow_ls",
				"tools":     []string{"bash"},
				"match_type": "command",
				"pattern":   "ls",
				"action":    "allow",
			},
		},
	}
	// approval_overrides 优先于 rules 命中（在 evaluateSingleInvocation 中 3 号优先级）
	level, rule := EvaluateTieredPolicy(config, "bash", map[string]any{"command": "ls"})
	assert.Equal(t, PermissionLevelAllow, level)
	assert.Contains(t, rule, "approval_overrides")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBaselineLevel_无效级别 测试无效级别字符串
func TestBaselineLevel_无效级别(t *testing.T) {
	toolsCfg := map[string]any{
		"bash": "invalid_level",
	}
	level, rule := baselineLevel(toolsCfg, "bash")
	assert.Equal(t, PermissionLevelAllow, level) // 解析失败默认 allow
	assert.Empty(t, rule)
}

// TestBaselineLevel_dict类型无星号键 测试 dict 类型配置无 * 键
func TestBaselineLevel_dict类型无星号键(t *testing.T) {
	toolsCfg := map[string]any{
		"read_file": map[string]any{
			"other_key": "deny",
		},
	}
	level, rule := baselineLevel(toolsCfg, "read_file")
	assert.Equal(t, PermissionLevelAllow, level)
	assert.Empty(t, rule)
}
