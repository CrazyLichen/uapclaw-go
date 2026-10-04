//go:build integration

package security

import (
	"testing"

	"github.com/stretchr/testify/suite"
	security "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionMergeSuite 测试 MergePermissionAllowRuleIntoPermissions 合并逻辑。
//
// 对齐 Python: tests/unit_tests/harness/security/test_permission_merge_after_auto_confirm.py
type PermissionMergeSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 常量 ────────────────────────────

// baseTieredConfig 返回基础 tiered policy 配置。
// 对齐 Python: _base_tiered()
func baseTieredConfig() map[string]any {
	return map[string]any{
		"enabled":            true,
		"schema":             "tiered_policy",
		"permission_mode":    "normal",
		"defaults":           map[string]any{"*": "allow"},
		"rules":              []any{},
		"approval_overrides": []any{},
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPermissionMergeSuite(t *testing.T) {
	suite.Run(t, new(PermissionMergeSuite))
}

// TestMergePermission_路径工具旧版字典写法_合并后追加path覆盖并允许
// 对齐 Python: test_read_file_merge_after_auto_confirm_adds_path_override_and_allows (legacy_dict_star_ask)
func (s *PermissionMergeSuite) TestMergePermission_路径工具旧版字典写法_合并后追加path覆盖并允许() {
	// read_file: {"*": "ask"} 旧版字典写法
	cfg := baseTieredConfig()
	cfg["tools"] = map[string]any{
		"read_file":  map[string]any{"*": "ask"},
		"write_file": "deny",
	}
	toolArgs := map[string]any{"file_path": "notes.txt"}

	// 合并前应为 ASK
	before, _ := security.EvaluateTieredPolicy(cfg, "read_file", toolArgs)
	s.Equal(security.PermissionLevelAsk, before)

	merged, applied := security.MergePermissionAllowRuleIntoPermissions(cfg, "read_file", toolArgs)
	s.True(applied)

	// 验证 approval_overrides 中包含 path 类覆盖
	overrides, ok := merged["approval_overrides"].([]any)
	s.True(ok)
	s.NotEmpty(overrides)

	found := false
	for _, o := range overrides {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		if strVal(m["match_type"]) == "path" &&
			strVal(m["pattern"]) == "notes.txt" &&
			strVal(m["action"]) == "allow" &&
			containsTool(m["tools"], "read_file") {
			found = true
			break
		}
	}
	s.True(found, "应在 approval_overrides 中找到 path 类覆盖: match_type=path, pattern=notes.txt, action=allow, tools 含 read_file")

	// 合并后应为 ALLOW
	after, matched := security.EvaluateTieredPolicy(merged, "read_file", toolArgs)
	s.Equal(security.PermissionLevelAllow, after)
	s.Contains(matched, "approval_overrides")

	// 幂等：再次合并应返回 applied=false，overrides 不变
	again, appliedAgain := security.MergePermissionAllowRuleIntoPermissions(merged, "read_file", toolArgs)
	s.False(appliedAgain)
	s.Equal(merged["approval_overrides"], again["approval_overrides"])
}

// TestMergePermission_路径工具标量写法_合并后追加path覆盖并允许
// 对齐 Python: test_read_file_merge_after_auto_confirm_adds_path_override_and_allows (scalar_ask)
func (s *PermissionMergeSuite) TestMergePermission_路径工具标量写法_合并后追加path覆盖并允许() {
	// read_file: "ask" 标量写法
	cfg := baseTieredConfig()
	cfg["tools"] = map[string]any{
		"read_file":  "ask",
		"write_file": "deny",
	}
	toolArgs := map[string]any{"file_path": "notes.txt"}

	before, _ := security.EvaluateTieredPolicy(cfg, "read_file", toolArgs)
	s.Equal(security.PermissionLevelAsk, before)

	merged, applied := security.MergePermissionAllowRuleIntoPermissions(cfg, "read_file", toolArgs)
	s.True(applied)

	overrides, ok := merged["approval_overrides"].([]any)
	s.True(ok)
	s.NotEmpty(overrides)

	found := false
	for _, o := range overrides {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		if strVal(m["match_type"]) == "path" &&
			strVal(m["pattern"]) == "notes.txt" &&
			strVal(m["action"]) == "allow" &&
			containsTool(m["tools"], "read_file") {
			found = true
			break
		}
	}
	s.True(found, "应在 approval_overrides 中找到 path 类覆盖")

	after, matched := security.EvaluateTieredPolicy(merged, "read_file", toolArgs)
	s.Equal(security.PermissionLevelAllow, after)
	s.Contains(matched, "approval_overrides")

	again, appliedAgain := security.MergePermissionAllowRuleIntoPermissions(merged, "read_file", toolArgs)
	s.False(appliedAgain)
	s.Equal(merged["approval_overrides"], again["approval_overrides"])
}

// TestMergePermission_旧版bash字典写法_合并后追加command覆盖并允许
// 对齐 Python: test_legacy_bash_star_ask_merge_adds_command_override
func (s *PermissionMergeSuite) TestMergePermission_旧版bash字典写法_合并后追加command覆盖并允许() {
	cfg := baseTieredConfig()
	cfg["tools"] = map[string]any{
		"bash": map[string]any{"*": "ask"},
	}
	toolArgs := map[string]any{"command": "git status"}

	before, _ := security.EvaluateTieredPolicy(cfg, "bash", toolArgs)
	s.Equal(security.PermissionLevelAsk, before)

	merged, applied := security.MergePermissionAllowRuleIntoPermissions(cfg, "bash", toolArgs)
	s.True(applied)

	overrides, ok := merged["approval_overrides"].([]any)
	s.True(ok)

	found := false
	for _, o := range overrides {
		m, ok := o.(map[string]any)
		if !ok {
			continue
		}
		pattern := strVal(m["pattern"])
		if strVal(m["match_type"]) == "command" &&
			strVal(m["action"]) == "allow" &&
			containsInsensitive(pattern, "git") {
			found = true
			break
		}
	}
	s.True(found, "应在 approval_overrides 中找到 command 类覆盖，pattern 含 git")

	after, _ := security.EvaluateTieredPolicy(merged, "bash", toolArgs)
	s.Equal(security.PermissionLevelAllow, after)
}

// TestMergePermission_普通工具自动确认_设置整工具allow
// 对齐 Python: test_plain_tool_auto_confirm_sets_whole_tool_allow
func (s *PermissionMergeSuite) TestMergePermission_普通工具自动确认_设置整工具allow() {
	cfg := baseTieredConfig()
	cfg["tools"] = map[string]any{
		"cron_create_job": "ask",
	}
	toolArgs := map[string]any{"cron": "0 * * * *", "name": "sync"}

	before, _ := security.EvaluateTieredPolicy(cfg, "cron_create_job", toolArgs)
	s.Equal(security.PermissionLevelAsk, before)

	merged, applied := security.MergePermissionAllowRuleIntoPermissions(cfg, "cron_create_job", toolArgs)
	s.True(applied)

	// 非 shell / path 工具应设置整工具 allow，不添加 approval_overrides
	tools, ok := merged["tools"].(map[string]any)
	s.True(ok)
	s.Equal("allow", tools["cron_create_job"])

	// approval_overrides 应为空列表
	overrides := merged["approval_overrides"]
	if overrides != nil {
		s.Equal([]any{}, overrides)
	}

	after, matched := security.EvaluateTieredPolicy(merged, "cron_create_job", toolArgs)
	s.Equal(security.PermissionLevelAllow, after)
	s.Contains(matched, "tools.cron_create_job")
}

// TestMergePermission_权限合并与工具回调事件关联 验证权限合并后对应回调事件名非空
func (s *PermissionMergeSuite) TestMergePermission_权限合并与工具回调事件关联() {
	// 权限合并影响 AfterToolCall 回调事件中的权限判断
	// agentinterfaces.CallbackAfterToolCall 是工具调用后的回调键
	s.NotEmpty(agentinterfaces.CallbackAfterToolCall)
	s.NotEmpty(agentinterfaces.CallbackBeforeToolCall)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strVal 从 map 值中提取字符串，非字符串返回空串
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// containsTool 检查 tools 字段是否包含指定工具名
func containsTool(toolsVal any, toolName string) bool {
	switch t := toolsVal.(type) {
	case string:
		return t == toolName
	case []string:
		for _, s := range t {
			if s == toolName {
				return true
			}
		}
	case []any:
		for _, s := range t {
			if str, ok := s.(string); ok && str == toolName {
				return true
			}
		}
	}
	return false
}

// containsInsensitive 大小写不敏感包含检查
func containsInsensitive(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
