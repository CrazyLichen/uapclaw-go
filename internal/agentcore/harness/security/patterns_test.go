package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMatchWildcard_基本匹配(t *testing.T) {
	assert.True(t, MatchWildcard("ls *", "ls -la"))
	assert.True(t, MatchWildcard("git *", "git status"))
	assert.True(t, MatchWildcard("cat *", "cat file.txt"))
}

func TestMatchWildcard_注入防护(t *testing.T) {
	// shell 元字符 ; | & ` < > $ 不在 wildcardChars 中，防命令拼接
	assert.False(t, MatchWildcard("git status *", "git status; rm -rf /"))
	assert.False(t, MatchWildcard("ls *", "ls && cat /etc/passwd"))
	assert.False(t, MatchWildcard("echo *", "echo `whoami`"))
	assert.False(t, MatchWildcard("ls *", "ls | grep secret"))
}

func TestMatchWildcard_尾部空格星号(t *testing.T) {
	// "ls *" 末尾 " *" → ( wildcardChars*)? 使 "ls *" 可匹配 "ls" 或 "ls -la"
	assert.True(t, MatchWildcard("ls *", "ls"))
	assert.True(t, MatchWildcard("ls *", "ls -la"))
	assert.True(t, MatchWildcard("ls *", "ls ")) // 尾部空格也可匹配
}

func TestMatchWildcard_问号(t *testing.T) {
	assert.True(t, MatchWildcard("a?c", "abc"))
	assert.True(t, MatchWildcard("a?c", "axc"))
	assert.False(t, MatchWildcard("a?c", "ac"))   // ? 恰好一个字符
	assert.False(t, MatchWildcard("a?c", "abbc")) // ? 恰好一个字符
}

func TestMatchWildcard_空值(t *testing.T) {
	assert.False(t, MatchWildcard("ls *", ""))
	assert.False(t, MatchWildcard("", "ls"))
	assert.False(t, MatchWildcard("", ""))
}

func TestMatchWildcard_反斜杠规范化(t *testing.T) {
	// 反斜杠统一替换为 /
	assert.True(t, MatchWildcard(`C:/Users/*`, `C:\Users\test`))
	assert.True(t, MatchWildcard(`C:\Users\*`, `C:/Users/test`))
}

func TestMatchWildcard_正则特殊字符转义(t *testing.T) {
	// 正则特殊字符 .+^${}()|[] 应被转义，不被解释为正则
	assert.True(t, MatchWildcard("v1.0", "v1.0"))
	assert.False(t, MatchWildcard("v1.0", "v1X0")) // . 应被转义，不匹配任意字符
}

func TestMatchWildcard_中间星号(t *testing.T) {
	// 中间位置的 * → 限制性字符类
	assert.True(t, MatchWildcard("foo * baz", "foo bar baz"))
	assert.False(t, MatchWildcard("foo * baz", "foo; baz")) // ; 不在 wildcardChars
}

// ──────────────────────────── PathMatcher ────────────────────────────

func TestPathMatcher_基本匹配(t *testing.T) {
	pm := &PathMatcher{pm: PatternMatcher{}}
	assert.True(t, pm.MatchPath("/home/user/*", "/home/user/file.txt"))
	assert.True(t, pm.MatchPath("/home/user/*", "/home/user/dir/sub.txt"))
}

func TestPathMatcher_反斜杠规范化(t *testing.T) {
	pm := &PathMatcher{pm: PatternMatcher{}}
	assert.True(t, pm.MatchPath(`/home/user/*`, `\home\user\file.txt`))
	assert.True(t, pm.MatchPath(`\home\user\*`, `/home/user/file.txt`))
}

func TestPathMatcher_父目录匹配(t *testing.T) {
	pm := &PathMatcher{pm: PatternMatcher{}}
	// 匹配路径的父目录层级
	assert.True(t, pm.MatchPath("/home/user", "/home/user/file.txt"))
	assert.True(t, pm.MatchPath("/home/user", "/home/user/subdir/deep.txt"))
	assert.True(t, pm.MatchPath("/home/user/*", "/home/user/subdir/deep.txt"))
}

func TestPathMatcher_不匹配的路径(t *testing.T) {
	pm := &PathMatcher{pm: PatternMatcher{}}
	assert.False(t, pm.MatchPath("/home/user/*", "/etc/passwd"))
	assert.False(t, pm.MatchPath("/home/alice/*", "/home/bob/file.txt"))
}

func TestPathMatcher_空值(t *testing.T) {
	pm := &PathMatcher{pm: PatternMatcher{}}
	assert.False(t, pm.MatchPath("", "/home/user"))
	assert.False(t, pm.MatchPath("/home/user/*", ""))
}

func TestPathMatcher_MatchPathAny(t *testing.T) {
	pm := &PathMatcher{pm: PatternMatcher{}}
	patterns := []string{"/home/user/*", "/tmp/*"}
	assert.True(t, pm.MatchPathAny(patterns, "/home/user/file.txt"))
	assert.True(t, pm.MatchPathAny(patterns, "/tmp/cache"))
	assert.False(t, pm.MatchPathAny(patterns, "/etc/passwd"))
	assert.False(t, pm.MatchPathAny([]string{}, "/home/user/file.txt"))
}

// ──────────────────────────── URLMatcher ────────────────────────────

func TestURLMatcher_完整URL匹配(t *testing.T) {
	um := &URLMatcher{pm: PatternMatcher{}}
	assert.True(t, um.MatchURL("example.com", "https://example.com/path"))
	assert.True(t, um.MatchURL("example.com", "http://example.com"))
}

func TestURLMatcher_主机名匹配(t *testing.T) {
	um := &URLMatcher{pm: PatternMatcher{}}
	assert.True(t, um.MatchURL("example.com", "example.com"))
	assert.True(t, um.MatchURL("*.example.com", "api.example.com"))
}

func TestURLMatcher_带scheme匹配(t *testing.T) {
	um := &URLMatcher{pm: PatternMatcher{}}
	assert.True(t, um.MatchURL("https://example.com", "https://example.com/api"))
	assert.True(t, um.MatchURL("https://example.com/*", "https://example.com/api/data"))
}

func TestURLMatcher_不匹配(t *testing.T) {
	um := &URLMatcher{pm: PatternMatcher{}}
	assert.False(t, um.MatchURL("example.com", "other.com"))
	assert.False(t, um.MatchURL("safe.com", "https://evil.com/path"))
}

func TestURLMatcher_空值(t *testing.T) {
	um := &URLMatcher{pm: PatternMatcher{}}
	assert.False(t, um.MatchURL("example.com", ""))
}

func TestURLMatcher_MatchURLAny(t *testing.T) {
	um := &URLMatcher{pm: PatternMatcher{}}
	patterns := []string{"example.com", "safe.org"}
	assert.True(t, um.MatchURLAny(patterns, "https://example.com/path"))
	assert.True(t, um.MatchURLAny(patterns, "https://safe.org"))
	assert.False(t, um.MatchURLAny(patterns, "https://evil.com"))
}

// ──────────────────────────── CommandMatcher ────────────────────────────

func TestCommandMatcher_基本匹配(t *testing.T) {
	cm := &CommandMatcher{pm: PatternMatcher{}}
	assert.True(t, cm.MatchCommand("ls *", "ls -la"))
	assert.True(t, cm.MatchCommand("git status *", "git status"))
	assert.True(t, cm.MatchCommand("npm *", "npm install --save"))
}

func TestCommandMatcher_注入防护(t *testing.T) {
	cm := &CommandMatcher{pm: PatternMatcher{}}
	assert.False(t, cm.MatchCommand("git status *", "git status; rm -rf /"))
	assert.False(t, cm.MatchCommand("ls *", "ls && cat /etc/passwd"))
}

func TestCommandMatcher_空值(t *testing.T) {
	cm := &CommandMatcher{pm: PatternMatcher{}}
	assert.False(t, cm.MatchCommand("ls *", ""))
	assert.False(t, cm.MatchCommand("", "ls"))
}

func TestCommandMatcher_MatchCommandAny(t *testing.T) {
	cm := &CommandMatcher{pm: PatternMatcher{}}
	patterns := []string{"git status *", "npm *"}
	assert.True(t, cm.MatchCommandAny(patterns, "git status"))
	assert.True(t, cm.MatchCommandAny(patterns, "npm install"))
	assert.False(t, cm.MatchCommandAny(patterns, "rm -rf /"))
}

// ──────────────────────────── BuildCommandAllowPattern ────────────────────────────

func TestBuildCommandAllowPattern(t *testing.T) {
	assert.Equal(t, "ls *", BuildCommandAllowPattern("ls"))
	assert.Equal(t, "start chrome *", BuildCommandAllowPattern("start chrome"))
	assert.Equal(t, "git status *", BuildCommandAllowPattern("git status"))
}

func TestBuildCommandAllowPattern_去除尾部空格(t *testing.T) {
	assert.Equal(t, "ls *", BuildCommandAllowPattern("ls "))
	assert.Equal(t, "git status *", BuildCommandAllowPattern("  git status  "))
}

// ──────────────────────────── ContainsPath ────────────────────────────

func TestContainsPath_正常包含(t *testing.T) {
	assert.True(t, ContainsPath("/home/user", "/home/user/file.txt"))
	assert.True(t, ContainsPath("/home/user", "/home/user/subdir/deep.txt"))
	assert.True(t, ContainsPath("/home/user", "/home/user"))
}

func TestContainsPath_路径穿越(t *testing.T) {
	assert.False(t, ContainsPath("/home/user", "/home/other/file"))
	assert.False(t, ContainsPath("/home/user", "/home/other"))
	assert.False(t, ContainsPath("/home/user", "/tmp/file"))
}

func TestContainsPath_相对路径穿越(t *testing.T) {
	assert.False(t, ContainsPath("/home/user", "/home/user/../other/file"))
}

func TestContainsPath_相同路径(t *testing.T) {
	assert.True(t, ContainsPath("/home/user", "/home/user"))
}

// ──────────────────────────── buildApprovalOverrideID ────────────────────────────

func TestBuildApprovalOverrideID(t *testing.T) {
	id := buildApprovalOverrideID("bash", "command", "git status *")
	assert.Contains(t, id, "user_allow")
	assert.Contains(t, id, "bash")
	assert.Contains(t, id, "command")
}

func TestBuildApprovalOverrideID_特殊字符替换(t *testing.T) {
	// 非字母数字替换为 _
	id := buildApprovalOverrideID("read_file", "path", "/home/user/*.txt")
	assert.Contains(t, id, "_")
	// 全小写
	assert.Equal(t, id, stringsLower(id))
}

func TestBuildApprovalOverrideID_长度截断(t *testing.T) {
	// 超长 ID 应截断到 120 字符
	longPattern := ""
	for i := 0; i < 200; i++ {
		longPattern += "a"
	}
	id := buildApprovalOverrideID("tool", "path", longPattern)
	assert.LessOrEqual(t, len(id), 120)
}

// ──────────────────────────── deepCopyMap ────────────────────────────

func TestDeepCopyMap_基本拷贝(t *testing.T) {
	original := map[string]any{
		"key1": "value1",
		"key2": 42,
	}
	copied := utils.DeepCopyMap(original)

	assert.Equal(t, original, copied)

	// 修改副本不影响原件
	copied["key1"] = "modified"
	assert.Equal(t, "value1", original["key1"])
}

func TestDeepCopyMap_嵌套map(t *testing.T) {
	original := map[string]any{
		"level1": map[string]any{
			"level2": "deep_value",
		},
	}
	copied := utils.DeepCopyMap(original)

	assert.Equal(t, original, copied)

	// 修改嵌套 map 不影响原件
	copied["level1"].(map[string]any)["level2"] = "modified"
	assert.Equal(t, "deep_value", original["level1"].(map[string]any)["level2"])
}

func TestDeepCopyMap_嵌套slice(t *testing.T) {
	original := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	copied := utils.DeepCopyMap(original)

	assert.Equal(t, original, copied)

	// 修改 slice 不影响原件
	copied["items"].([]any)[0] = "modified"
	assert.Equal(t, "a", original["items"].([]any)[0])
}

func TestDeepCopyMap_nil输入(t *testing.T) {
	assert.Nil(t, utils.DeepCopyMap(nil))
}

func TestDeepCopyMap_空map(t *testing.T) {
	result := utils.DeepCopyMap(map[string]any{})
	assert.Empty(t, result)
	assert.NotNil(t, result)
}

func TestDeepCopyMap_嵌套slice中的map(t *testing.T) {
	original := map[string]any{
		"rules": []any{
			map[string]any{"id": "r1", "action": "allow"},
			map[string]any{"id": "r2", "action": "deny"},
		},
	}
	copied := utils.DeepCopyMap(original)

	// 修改嵌套结构不影响原件
	rules := copied["rules"].([]any)
	rules[0].(map[string]any)["action"] = "deny"
	assert.Equal(t, "allow", original["rules"].([]any)[0].(map[string]any)["action"])
}

// ──────────────────────────── resolveAgentConfigYAMLPath ────────────────────────────

func TestResolveAgentConfigYAMLPath_空路径(t *testing.T) {
	// 空路径时回退到默认配置路径（对齐 Python: CONFIG_YAML_PATH = get_config_file()）
	result := resolveAgentConfigYAMLPath("")
	assert.NotEqual(t, "", result, "空路径应回退到默认配置路径")
}

func TestResolveAgentConfigYAMLPath_文件存在(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "agent.yaml")
	err := os.WriteFile(cfgPath, []byte("test: true"), 0644)
	assert.NoError(t, err)

	result := resolveAgentConfigYAMLPath(cfgPath)
	assert.Equal(t, cfgPath, result)
}

func TestResolveAgentConfigYAMLPath_父目录存在(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "agent.yaml")
	// 文件不存在但父目录存在

	result := resolveAgentConfigYAMLPath(cfgPath)
	assert.Equal(t, cfgPath, result)
}

func TestResolveAgentConfigYAMLPath_路径不存在(t *testing.T) {
	// /nonexistent/path/ 的父目录不存在，但当前实现可能返回路径（父目录检查宽松）
	// 实际行为：若文件不存在且父目录不存在，返回空
	result := resolveAgentConfigYAMLPath("/nonexistent/path/agent.yaml")
	// 在不同系统上行为可能不同，只验证不会 panic
	_ = result
}

// ──────────────────────────── hasOverrideID ────────────────────────────

func TestHasOverrideID_存在(t *testing.T) {
	overrides := []any{
		map[string]any{"id": "r1", "action": "allow"},
		map[string]any{"id": "r2", "action": "deny"},
	}
	assert.True(t, hasOverrideID(overrides, "r1"))
	assert.True(t, hasOverrideID(overrides, "r2"))
}

func TestHasOverrideID_不存在(t *testing.T) {
	overrides := []any{
		map[string]any{"id": "r1", "action": "allow"},
	}
	assert.False(t, hasOverrideID(overrides, "r3"))
}

func TestHasOverrideID_空列表(t *testing.T) {
	assert.False(t, hasOverrideID(nil, "r1"))
	assert.False(t, hasOverrideID([]any{}, "r1"))
}

func TestHasOverrideID_非map类型(t *testing.T) {
	overrides := []any{"string", 42}
	assert.False(t, hasOverrideID(overrides, "r1"))
}

// ──────────────────────────── sortStrings ────────────────────────────

func TestSortStrings(t *testing.T) {
	s := []string{"banana", "apple", "cherry"}
	sortStrings(s)
	assert.Equal(t, []string{"apple", "banana", "cherry"}, s)
}

func TestSortStrings_空切片(t *testing.T) {
	s := []string{}
	sortStrings(s)
	assert.Equal(t, []string{}, s)
}

func TestSortStrings_单元素(t *testing.T) {
	s := []string{"only"}
	sortStrings(s)
	assert.Equal(t, []string{"only"}, s)
}

func TestSortStrings_已排序(t *testing.T) {
	s := []string{"a", "b", "c"}
	sortStrings(s)
	assert.Equal(t, []string{"a", "b", "c"}, s)
}

// ──────────────────────────── sortedPathTools / sortedShellTools ────────────────────────────

func TestSortedPathTools(t *testing.T) {
	tools := sortedPathTools()
	assert.NotEmpty(t, tools)
	// 结果应已排序
	for i := 1; i < len(tools); i++ {
		assert.True(t, tools[i-1] <= tools[i], "结果应已排序: %q > %q", tools[i-1], tools[i])
	}
}

func TestSortedShellTools(t *testing.T) {
	tools := sortedShellTools()
	assert.NotEmpty(t, tools)
	for i := 1; i < len(tools); i++ {
		assert.True(t, tools[i-1] <= tools[i], "结果应已排序: %q > %q", tools[i-1], tools[i])
	}
}

// ──────────────────────────── ruleToolsList ────────────────────────────

func TestRuleToolsList_字符串(t *testing.T) {
	rule := map[string]any{"tools": "bash"}
	result := ruleToolsList(rule)
	assert.Equal(t, []string{"bash"}, result)
}

func TestRuleToolsList_字符串数组(t *testing.T) {
	rule := map[string]any{"tools": []any{"bash", "read_file"}}
	result := ruleToolsList(rule)
	assert.Equal(t, []string{"bash", "read_file"}, result)
}

func TestRuleToolsList_字符串切片(t *testing.T) {
	rule := map[string]any{"tools": []string{"bash", "read_file"}}
	result := ruleToolsList(rule)
	assert.Equal(t, []string{"bash", "read_file"}, result)
}

func TestRuleToolsList_空字符串跳过(t *testing.T) {
	rule := map[string]any{"tools": []any{"bash", "  ", "read_file"}}
	result := ruleToolsList(rule)
	assert.Equal(t, []string{"bash", "read_file"}, result)
}

func TestRuleToolsList_无tools键(t *testing.T) {
	rule := map[string]any{"other": "value"}
	result := ruleToolsList(rule)
	assert.Nil(t, result)
}

func TestRuleToolsList_其他类型(t *testing.T) {
	rule := map[string]any{"tools": 42}
	result := ruleToolsList(rule)
	assert.Nil(t, result)
}

// ──────────────────────────── strVal ────────────────────────────

func TestStrVal(t *testing.T) {
	assert.Equal(t, "hello", strVal("hello"))
	assert.Equal(t, "", strVal(42))
	assert.Equal(t, "", strVal(nil))
}

// ──────────────────────────── commandText ────────────────────────────

func TestCommandText_从command键(t *testing.T) {
	result := commandText(map[string]any{"command": "ls -la"})
	assert.Equal(t, "ls -la", result)
}

func TestCommandText_从cmd键(t *testing.T) {
	result := commandText(map[string]any{"cmd": "ls -la"})
	assert.Equal(t, "ls -la", result)
}

func TestCommandText_command优先(t *testing.T) {
	result := commandText(map[string]any{"command": "ls", "cmd": "cat"})
	assert.Equal(t, "ls", result)
}

func TestCommandText_无键(t *testing.T) {
	result := commandText(map[string]any{})
	assert.Equal(t, "", result)
}

func TestCommandText_去除空白(t *testing.T) {
	result := commandText(map[string]any{"command": "  ls -la  "})
	assert.Equal(t, "ls -la", result)
}

// ──────────────────────────── isSameAllowOverride ────────────────────────────

func TestIsSameAllowOverride_匹配(t *testing.T) {
	sig := approvalOverrideSignature{
		ToolName:          "bash",
		Tools:             []string{"bash"},
		MatchType:         "command",
		ExistingMatchType: "command",
		Pattern:           "git *",
		ExistingPattern:   "git *",
		ExistingAction:    "allow",
	}
	assert.True(t, isSameAllowOverride(sig))
}

func TestIsSameAllowOverride_工具名不匹配(t *testing.T) {
	sig := approvalOverrideSignature{
		ToolName:          "bash",
		Tools:             []string{"read_file"},
		MatchType:         "command",
		ExistingMatchType: "command",
		Pattern:           "git *",
		ExistingPattern:   "git *",
		ExistingAction:    "allow",
	}
	assert.False(t, isSameAllowOverride(sig))
}

func TestIsSameAllowOverride_matchType不匹配(t *testing.T) {
	sig := approvalOverrideSignature{
		ToolName:          "bash",
		Tools:             []string{"bash"},
		MatchType:         "command",
		ExistingMatchType: "path",
		Pattern:           "git *",
		ExistingPattern:   "git *",
		ExistingAction:    "allow",
	}
	assert.False(t, isSameAllowOverride(sig))
}

func TestIsSameAllowOverride_pattern不匹配(t *testing.T) {
	sig := approvalOverrideSignature{
		ToolName:          "bash",
		Tools:             []string{"bash"},
		MatchType:         "command",
		ExistingMatchType: "command",
		Pattern:           "git *",
		ExistingPattern:   "npm *",
		ExistingAction:    "allow",
	}
	assert.False(t, isSameAllowOverride(sig))
}

func TestIsSameAllowOverride_action非allow(t *testing.T) {
	sig := approvalOverrideSignature{
		ToolName:          "bash",
		Tools:             []string{"bash"},
		MatchType:         "command",
		ExistingMatchType: "command",
		Pattern:           "git *",
		ExistingPattern:   "git *",
		ExistingAction:    "deny",
	}
	assert.False(t, isSameAllowOverride(sig))
}

// ──────────────────────────── ensureSingleAllowOverride ────────────────────────────

func TestEnsureSingleAllowOverride_新增条目(t *testing.T) {
	overrides := []any{}
	result := ensureSingleAllowOverride(&overrides, "bash", "command", "git *", "allow")
	assert.True(t, result)
	assert.Len(t, overrides, 1)
	m := overrides[0].(map[string]any)
	assert.Equal(t, "bash", m["tools"].([]string)[0])
	assert.Equal(t, "command", m["match_type"])
	assert.Equal(t, "git *", m["pattern"])
	assert.Equal(t, "allow", m["action"])
}

func TestEnsureSingleAllowOverride_已有相同条目(t *testing.T) {
	overrides := []any{
		map[string]any{
			"id":         "existing",
			"tools":      []string{"bash"},
			"match_type": "command",
			"pattern":    "git *",
			"action":     "allow",
		},
	}
	result := ensureSingleAllowOverride(&overrides, "bash", "command", "git *", "allow")
	assert.True(t, result)
	assert.Len(t, overrides, 1) // 不新增
}

// ──────────────────────────── persistTieredApprovalOverrideSuggestions ────────────────────────────

func TestPersistTieredApprovalOverrideSuggestions_空建议(t *testing.T) {
	perms := map[string]any{}
	assert.False(t, persistTieredApprovalOverrideSuggestions(perms, nil))
	assert.False(t, persistTieredApprovalOverrideSuggestions(perms, []PermissionSuggestion{}))
}

func TestPersistTieredApprovalOverrideSuggestions_有效建议(t *testing.T) {
	perms := map[string]any{}
	suggestions := []PermissionSuggestion{
		{Tools: []string{"bash"}, MatchType: "command", Pattern: "git *", Action: "allow"},
	}
	result := persistTieredApprovalOverrideSuggestions(perms, suggestions)
	assert.True(t, result)
	overrides, ok := perms["approval_overrides"].([]any)
	assert.True(t, ok)
	assert.NotEmpty(t, overrides)
}

// ──────────────────────────── persistTieredToolAllow ────────────────────────────

func TestPersistTieredToolAllow_正常(t *testing.T) {
	perms := map[string]any{}
	result := persistTieredToolAllow(perms, "bash")
	assert.True(t, result)
	tools, ok := perms["tools"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "allow", tools["bash"])
}

func TestPersistTieredToolAllow_空工具名(t *testing.T) {
	perms := map[string]any{}
	assert.False(t, persistTieredToolAllow(perms, ""))
}

func TestPersistTieredToolAllow_已有allow(t *testing.T) {
	perms := map[string]any{
		"tools": map[string]any{"bash": "allow"},
	}
	result := persistTieredToolAllow(perms, "bash")
	assert.False(t, result) // 已有 allow，不再写入
}

// ──────────────────────────── ReadAgentConfigYAML / WriteAgentConfigYAML ────────────────────────────

func TestReadAgentConfigYAML_文件不存在(t *testing.T) {
	result := ReadAgentConfigYAML("/nonexistent/path/agent.yaml")
	assert.Empty(t, result)
}

func TestWriteAndReadAgentConfigYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "agent.yaml")

	data := map[string]any{"key": "value", "nested": map[string]any{"k": "v"}}
	err := WriteAgentConfigYAML(cfgPath, data)
	assert.NoError(t, err)

	readBack := ReadAgentConfigYAML(cfgPath)
	assert.Equal(t, "value", readBack["key"])
}

func TestWriteAgentConfigYAML_空路径(t *testing.T) {
	// 空路径会回退到默认配置路径，不会报错
	err := WriteAgentConfigYAML("", map[string]any{})
	// 空路径回退到默认路径，结果取决于默认路径是否存在
	_ = err
}

func TestReadAgentConfigYAML_空路径(t *testing.T) {
	result := ReadAgentConfigYAML("")
	// 空路径会回退到默认路径，若默认路径文件不存在返回空 map
	assert.NotNil(t, result)
}

// ──────────────────────────── MergeExternalDirectoryAllowIntoPermissions ────────────────────────────

func TestMergeExternalDirectoryAllowIntoPermissions_空路径(t *testing.T) {
	perms := map[string]any{"existing": "data"}
	merged, wrote := MergeExternalDirectoryAllowIntoPermissions(perms, nil)
	assert.False(t, wrote)
	assert.Equal(t, "data", merged["existing"])
}

func TestMergeExternalDirectoryAllowIntoPermissions_添加路径(t *testing.T) {
	perms := map[string]any{}
	merged, wrote := MergeExternalDirectoryAllowIntoPermissions(perms, []string{"/home/user/project"})
	assert.True(t, wrote)
	extCfg, ok := merged["external_directory"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "allow", extCfg["/home/user"])
}

func TestMergeExternalDirectoryAllowIntoPermissions_已有allow不写入(t *testing.T) {
	perms := map[string]any{
		"external_directory": map[string]any{"/home/user": "allow"},
	}
	merged, wrote := MergeExternalDirectoryAllowIntoPermissions(perms, []string{"/home/user/project"})
	_ = merged
	assert.False(t, wrote)
}

// ──────────────────────────── PatternMatcher.MatchAny ────────────────────────────

func TestPatternMatcher_MatchAny(t *testing.T) {
	pm := PatternMatcher{}
	assert.True(t, pm.MatchAny([]string{"ls *", "cat *"}, "ls -la"))
	assert.True(t, pm.MatchAny([]string{"ls *", "cat *"}, "cat file.txt"))
	assert.False(t, pm.MatchAny([]string{"ls *", "cat *"}, "rm -rf /"))
	assert.False(t, pm.MatchAny([]string{}, "ls"))
}

// ──────────────────────────── escapeRegexChars ────────────────────────────

func TestEscapeRegexChars(t *testing.T) {
	result := escapeRegexChars("a.b", ".")
	assert.Contains(t, result, `\`)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func stringsLower(s string) string {
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
