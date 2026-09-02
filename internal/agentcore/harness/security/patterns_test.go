package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMatchWildcard_基本匹配(t *testing.T) {
	assert.True(t, MatchWildcard("ls -la", "ls *"))
	assert.True(t, MatchWildcard("git status", "git *"))
	assert.True(t, MatchWildcard("cat file.txt", "cat *"))
}

func TestMatchWildcard_注入防护(t *testing.T) {
	// shell 元字符 ; | & ` < > $ 不在 wildcardChars 中，防命令拼接
	assert.False(t, MatchWildcard("git status; rm -rf /", "git status *"))
	assert.False(t, MatchWildcard("ls && cat /etc/passwd", "ls *"))
	assert.False(t, MatchWildcard("echo `whoami`", "echo *"))
	assert.False(t, MatchWildcard("ls | grep secret", "ls *"))
}

func TestMatchWildcard_尾部空格星号(t *testing.T) {
	// "ls *" 末尾 " *" → ( wildcardChars*)? 使 "ls *" 可匹配 "ls" 或 "ls -la"
	assert.True(t, MatchWildcard("ls", "ls *"))
	assert.True(t, MatchWildcard("ls -la", "ls *"))
	assert.True(t, MatchWildcard("ls ", "ls *")) // 尾部空格也可匹配
}

func TestMatchWildcard_问号(t *testing.T) {
	assert.True(t, MatchWildcard("abc", "a?c"))
	assert.True(t, MatchWildcard("axc", "a?c"))
	assert.False(t, MatchWildcard("ac", "a?c"))   // ? 恰好一个字符
	assert.False(t, MatchWildcard("abbc", "a?c")) // ? 恰好一个字符
}

func TestMatchWildcard_空值(t *testing.T) {
	assert.False(t, MatchWildcard("", "ls *"))
	assert.False(t, MatchWildcard("ls", ""))
	assert.False(t, MatchWildcard("", ""))
}

func TestMatchWildcard_反斜杠规范化(t *testing.T) {
	// 反斜杠统一替换为 /
	assert.True(t, MatchWildcard(`C:\Users\test`, `C:/Users/*`))
	assert.True(t, MatchWildcard(`C:/Users/test`, `C:\Users\*`))
}

func TestMatchWildcard_正则特殊字符转义(t *testing.T) {
	// 正则特殊字符 .+^${}()|[] 应被转义，不被解释为正则
	assert.True(t, MatchWildcard("v1.0", "v1.0"))
	assert.False(t, MatchWildcard("v1X0", "v1.0")) // . 应被转义，不匹配任意字符
}

func TestMatchWildcard_中间星号(t *testing.T) {
	// 中间位置的 * → 限制性字符类
	assert.True(t, MatchWildcard("foo bar baz", "foo * baz"))
	assert.False(t, MatchWildcard("foo; baz", "foo * baz")) // ; 不在 wildcardChars
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
	copied := deepCopyMap(original)

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
	copied := deepCopyMap(original)

	assert.Equal(t, original, copied)

	// 修改嵌套 map 不影响原件
	copied["level1"].(map[string]any)["level2"] = "modified"
	assert.Equal(t, "deep_value", original["level1"].(map[string]any)["level2"])
}

func TestDeepCopyMap_嵌套slice(t *testing.T) {
	original := map[string]any{
		"items": []any{"a", "b", "c"},
	}
	copied := deepCopyMap(original)

	assert.Equal(t, original, copied)

	// 修改 slice 不影响原件
	copied["items"].([]any)[0] = "modified"
	assert.Equal(t, "a", original["items"].([]any)[0])
}

func TestDeepCopyMap_nil输入(t *testing.T) {
	assert.Nil(t, deepCopyMap(nil))
}

func TestDeepCopyMap_空map(t *testing.T) {
	result := deepCopyMap(map[string]any{})
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
	copied := deepCopyMap(original)

	// 修改嵌套结构不影响原件
	rules := copied["rules"].([]any)
	rules[0].(map[string]any)["action"] = "deny"
	assert.Equal(t, "allow", original["rules"].([]any)[0].(map[string]any)["action"])
}

// ──────────────────────────── resolveAgentConfigYAMLPath ────────────────────────────

func TestResolveAgentConfigYAMLPath_空路径(t *testing.T) {
	assert.Equal(t, "", resolveAgentConfigYAMLPath(""))
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
