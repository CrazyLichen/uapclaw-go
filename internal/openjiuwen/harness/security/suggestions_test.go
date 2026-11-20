package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildPermissionSuggestions_路径工具 测试路径工具生成路径建议
func TestBuildPermissionSuggestions_路径工具(t *testing.T) {
	toolArgs := map[string]any{
		"path": "/home/user/file.txt",
	}
	result := BuildPermissionSuggestions("read_file", toolArgs, nil)
	require.Len(t, result, 1)
	assert.Equal(t, []string{"read_file"}, result[0].Tools)
	assert.Equal(t, "path", result[0].MatchType)
	assert.Equal(t, "/home/user/file.txt", result[0].Pattern)
	assert.Equal(t, "exact", result[0].Scope)
	assert.Equal(t, "allow", result[0].Action)
	assert.Equal(t, "exact_path", result[0].Reason)
}

// TestBuildPermissionSuggestions_未知工具 测试未知工具返回空
func TestBuildPermissionSuggestions_未知工具(t *testing.T) {
	toolArgs := map[string]any{
		"something": "value",
	}
	result := BuildPermissionSuggestions("unknown_tool", toolArgs, nil)
	assert.Nil(t, result)
}

// TestBuildPermissionSuggestions_Shell工具 测试 Shell 工具走 Shell 建议
func TestBuildPermissionSuggestions_Shell工具(t *testing.T) {
	toolArgs := map[string]any{
		"command": "ls -la",
	}
	// 手动构造 ShellAstParseResult 避免依赖 tree-sitter
	astResult := &ShellAstParseResult{
		Kind: ShellAstKindSimple,
		Subcommands: []ShellSubcommand{
			{Text: "ls -la", Argv: []string{"ls", "-la"}},
		},
		Backend: "test",
	}
	result := BuildPermissionSuggestions("bash", toolArgs, astResult)
	require.Len(t, result, 1)
	assert.Equal(t, "exact", result[0].Scope)
	assert.Equal(t, "ls -la", result[0].Pattern)
}

// TestBuildShellPermissionSuggestions_简单命令 测试单子命令 exact 建议
func TestBuildShellPermissionSuggestions_简单命令(t *testing.T) {
	astResult := &ShellAstParseResult{
		Kind: ShellAstKindSimple,
		Subcommands: []ShellSubcommand{
			{Text: "ls -la", Argv: []string{"ls", "-la"}},
		},
		Backend: "test",
	}
	result := BuildShellPermissionSuggestions("bash", "ls -la", astResult)
	require.Len(t, result, 1)
	assert.Equal(t, []string{"bash"}, result[0].Tools)
	assert.Equal(t, "command", result[0].MatchType)
	assert.Equal(t, "ls -la", result[0].Pattern)
	assert.Equal(t, "exact", result[0].Scope)
	assert.Equal(t, "allow", result[0].Action)
	assert.Equal(t, "exact_command", result[0].Reason)
}

// TestBuildShellPermissionSuggestions_过于复杂 测试 too_complex 返回空
func TestBuildShellPermissionSuggestions_过于复杂(t *testing.T) {
	astResult := &ShellAstParseResult{
		Kind:    ShellAstKindTooComplex,
		Reason:  "tree-sitter 检测到不支持的复杂 shell 结构",
		Backend: "test",
	}
	result := BuildShellPermissionSuggestions("bash", "some complex cmd", astResult)
	assert.Nil(t, result)
}

// TestBuildShellPermissionSuggestions_多子命令管道 测试管道多子命令生成 prefix 建议
func TestBuildShellPermissionSuggestions_多子命令管道(t *testing.T) {
	astResult := &ShellAstParseResult{
		Kind: ShellAstKindSimple,
		Subcommands: []ShellSubcommand{
			{Text: "echo hello", Argv: []string{"echo", "hello"}},
			{Text: "grep h", Argv: []string{"grep", "h"}},
		},
		Flags:   ShellStructureFlags{Pipeline: true},
		Backend: "test",
	}
	result := BuildShellPermissionSuggestions("bash", "echo hello | grep h", astResult)
	require.Len(t, result, 2)
	// 每条建议都是 exact scope（简单非多行命令）
	for _, s := range result {
		assert.Equal(t, "exact", s.Scope)
		assert.Equal(t, "command", s.MatchType)
	}
	assert.Equal(t, "echo hello", result[0].Pattern)
	assert.Equal(t, "grep h", result[1].Pattern)
}

// TestBuildShellPermissionSuggestions_含Heredoc 测试含 heredoc 的建议
func TestBuildShellPermissionSuggestions_含Heredoc(t *testing.T) {
	// shellAstResult 为 nil 时会自动解析，但此处我们直接构造
	// 含 heredoc 标志位时，风险结构导致返回空
	astResult := &ShellAstParseResult{
		Kind: ShellAstKindSimple,
		Subcommands: []ShellSubcommand{
			{Text: "cat <<EOF", Argv: []string{"cat"}},
		},
		Flags:   ShellStructureFlags{Heredoc: true},
		Backend: "test",
	}
	// Flags.Heredoc=true 导致 HasRiskyStructure()=true → 返回空
	result := BuildShellPermissionSuggestions("bash", "cat <<EOF", astResult)
	assert.Nil(t, result)
}

// TestBuildShellPermissionSuggestions_含HeredocPrefix 测试 heredoc 前缀建议
func TestBuildShellPermissionSuggestions_含HeredocPrefix(t *testing.T) {
	// 不走 AST（Flags 无风险标志），直接让 buildSingleShellSuggestion 处理 heredoc
	astResult := &ShellAstParseResult{
		Kind: ShellAstKindSimple,
		Subcommands: []ShellSubcommand{
			{Text: "cat <<EOF\nhello\nEOF", Argv: []string{"cat"}},
		},
		Backend: "test",
	}
	result := BuildShellPermissionSuggestions("bash", "cat <<EOF\nhello\nEOF", astResult)
	require.Len(t, result, 1)
	assert.Equal(t, "prefix", result[0].Scope)
	assert.Equal(t, "cat *", result[0].Pattern)
	assert.Equal(t, "heredoc_prefix", result[0].Reason)
}

// TestBuildShellPermissionSuggestions_风险结构标志 测试各种风险标志返回空
func TestBuildShellPermissionSuggestions_风险结构标志(t *testing.T) {
	riskFlags := []ShellStructureFlags{
		{InputRedirection: true},
		{OutputRedirection: true},
		{CommandSubstitution: true},
		{ProcessSubstitution: true},
		{Subshell: true},
		{CommandGroup: true},
		{ParameterExpansion: true},
	}
	for _, flags := range riskFlags {
		astResult := &ShellAstParseResult{
			Kind: ShellAstKindSimple,
			Subcommands: []ShellSubcommand{
				{Text: "ls", Argv: []string{"ls"}},
			},
			Flags:   flags,
			Backend: "test",
		}
		result := BuildShellPermissionSuggestions("bash", "ls", astResult)
		assert.Nil(t, result, "风险标志 %v 应返回空", flags)
	}
}

// TestBuildShellPermissionSuggestions_ParseUnavailable风险 测试 parse_unavailable + risky 返回空
func TestBuildShellPermissionSuggestions_ParseUnavailable风险(t *testing.T) {
	astResult := &ShellAstParseResult{
		Kind:    ShellAstKindParseUnavailable,
		Flags:   ShellStructureFlags{Pipeline: true},
		Reason:  "tree-sitter 后端不可用且保守扫描检测到 shell 结构",
		Backend: "fallback",
	}
	result := BuildShellPermissionSuggestions("bash", "echo hello | grep h", astResult)
	assert.Nil(t, result)
}

// TestBuildShellPermissionSuggestions_NilAstResult 测试 nil AST 自动解析
func TestBuildShellPermissionSuggestions_NilAstResult(t *testing.T) {
	// shellAstResult 为 nil 时自动调用 ParseShellForPermission
	// "ls -la" 是简单命令，应返回 exact 建议
	result := BuildShellPermissionSuggestions("bash", "ls -la", nil)
	require.Len(t, result, 1)
	assert.Equal(t, "exact", result[0].Scope)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBuildSingleShellSuggestion_精确命令 测试简单命令生成 exact 建议
func TestBuildSingleShellSuggestion_精确命令(t *testing.T) {
	result := buildSingleShellSuggestion("bash", "git status")
	require.NotNil(t, result)
	assert.Equal(t, []string{"bash"}, result.Tools)
	assert.Equal(t, "command", result.MatchType)
	assert.Equal(t, "git status", result.Pattern)
	assert.Equal(t, "exact", result.Scope)
	assert.Equal(t, "allow", result.Action)
	assert.Equal(t, "exact_command", result.Reason)
}

// TestBuildSingleShellSuggestion_空命令 测试空命令返回 nil
func TestBuildSingleShellSuggestion_空命令(t *testing.T) {
	result := buildSingleShellSuggestion("bash", "   ")
	assert.Nil(t, result)
}

// TestBuildSingleShellSuggestion_多行命令 测试多行命令取首行 prefix
func TestBuildSingleShellSuggestion_多行命令(t *testing.T) {
	result := buildSingleShellSuggestion("bash", "echo hello\necho world")
	require.NotNil(t, result)
	assert.Equal(t, "prefix", result.Scope)
	assert.Equal(t, "echo hello *", result.Pattern)
	assert.Equal(t, "first_line_prefix", result.Reason)
}

// TestExtractPrefixBeforeHeredoc_含Heredoc 测试含 << 的前缀提取
func TestExtractPrefixBeforeHeredoc_含Heredoc(t *testing.T) {
	prefix := extractPrefixBeforeHeredoc("cat <<EOF")
	assert.Equal(t, "cat", prefix)
}

// TestExtractPrefixBeforeHeredoc_无Heredoc 测试不含 << 返回空
func TestExtractPrefixBeforeHeredoc_无Heredoc(t *testing.T) {
	prefix := extractPrefixBeforeHeredoc("ls -la")
	assert.Equal(t, "", prefix)
}

// TestExtractPrefixBeforeHeredoc_仅Heredoc 测试 << 前无内容返回空
func TestExtractPrefixBeforeHeredoc_仅Heredoc(t *testing.T) {
	prefix := extractPrefixBeforeHeredoc("<<EOF")
	assert.Equal(t, "", prefix)
}

// TestExtractSimpleCommandPrefix_双参数 测试取前两个 argv
func TestExtractSimpleCommandPrefix_双参数(t *testing.T) {
	prefix := extractSimpleCommandPrefix("git status")
	assert.Equal(t, "git status", prefix)
}

// TestExtractSimpleCommandPrefix_单参数 测试只有一个 argv
func TestExtractSimpleCommandPrefix_单参数(t *testing.T) {
	prefix := extractSimpleCommandPrefix("ls")
	assert.Equal(t, "ls", prefix)
}

// TestExtractSimpleCommandPrefix_多参数 测试超过两个 argv 只取前两个
func TestExtractSimpleCommandPrefix_多参数(t *testing.T) {
	prefix := extractSimpleCommandPrefix("git commit -m 'hello world'")
	assert.Equal(t, "git commit", prefix)
}

// TestExtractSimpleCommandPrefix_空字符串 测试空字符串返回空
func TestExtractSimpleCommandPrefix_空字符串(t *testing.T) {
	prefix := extractSimpleCommandPrefix("")
	assert.Equal(t, "", prefix)
}

// TestBuildPrefixPattern 测试前缀模式构建
func TestBuildPrefixPattern(t *testing.T) {
	assert.Equal(t, "ls *", buildPrefixPattern("ls"))
	assert.Equal(t, "git status *", buildPrefixPattern("git status"))
	assert.Equal(t, "cat *", buildPrefixPattern(" cat "))
}

// TestBuildPathPermissionSuggestion_已知路径键 测试已知路径键生成建议
func TestBuildPathPermissionSuggestion_已知路径键(t *testing.T) {
	toolArgs := map[string]any{
		"path": "/home/user/file.txt",
	}
	result := buildPathPermissionSuggestion("read_file", toolArgs)
	require.NotNil(t, result)
	assert.Equal(t, "path", result.MatchType)
	assert.Equal(t, "/home/user/file.txt", result.Pattern)
	assert.Equal(t, "exact", result.Scope)
	assert.Equal(t, "exact_path", result.Reason)
}

// TestBuildPathPermissionSuggestion_派生路径 测试非标准键但值含 /
func TestBuildPathPermissionSuggestion_派生路径(t *testing.T) {
	toolArgs := map[string]any{
		"my_custom_key": "/var/log/app.log",
	}
	result := buildPathPermissionSuggestion("read_file", toolArgs)
	require.NotNil(t, result)
	assert.Equal(t, "path", result.MatchType)
	assert.Equal(t, "/var/log/app.log", result.Pattern)
	assert.Equal(t, "derived_exact_path", result.Reason)
}

// TestBuildPathPermissionSuggestion_无可识别路径 测试无路径参数返回 nil
func TestBuildPathPermissionSuggestion_无可识别路径(t *testing.T) {
	toolArgs := map[string]any{
		"content": "hello world",
	}
	result := buildPathPermissionSuggestion("read_file", toolArgs)
	assert.Nil(t, result)
}

// TestValueLooksLikePath_已知路径键 测试已知路径键直接返回 true
func TestValueLooksLikePath_已知路径键(t *testing.T) {
	assert.True(t, valueLooksLikePath("path", "/anything"))
	assert.True(t, valueLooksLikePath("file_path", "value"))
	assert.True(t, valueLooksLikePath("target_file", "value"))
}

// TestValueLooksLikePath_含斜杠 测试值含 / 返回 true
func TestValueLooksLikePath_含斜杠(t *testing.T) {
	assert.True(t, valueLooksLikePath("foo", "/home/user/file"))
	assert.True(t, valueLooksLikePath("bar", "C:\\Users\\file"))
}

// TestValueLooksLikePath_无路径特征 测试无路径特征返回 false
func TestValueLooksLikePath_无路径特征(t *testing.T) {
	assert.False(t, valueLooksLikePath("foo", "hello"))
	assert.False(t, valueLooksLikePath("bar", "world"))
}

// TestValueLooksLikePath_Windows盘符 测试 Windows 盘符返回 true
func TestValueLooksLikePath_Windows盘符(t *testing.T) {
	assert.True(t, valueLooksLikePath("foo", "C:"))
	assert.True(t, valueLooksLikePath("bar", "D:\\path"))
}

// TestDedupeSuggestions_去重 测试重复建议去重
func TestDedupeSuggestions_去重(t *testing.T) {
	suggestions := []PermissionSuggestion{
		{
			Tools:     []string{"bash"},
			MatchType: "command",
			Pattern:   "ls -la",
			Action:    "allow",
			Scope:     "exact",
			Reason:    "exact_command",
		},
		{
			Tools:     []string{"bash"},
			MatchType: "command",
			Pattern:   "ls -la",
			Action:    "allow",
			Scope:     "exact",
			Reason:    "exact_command",
		},
		{
			Tools:     []string{"bash"},
			MatchType: "command",
			Pattern:   "git status",
			Action:    "allow",
			Scope:     "exact",
			Reason:    "exact_command",
		},
	}
	result := dedupeSuggestions(suggestions)
	assert.Len(t, result, 2)
	assert.Equal(t, "ls -la", result[0].Pattern)
	assert.Equal(t, "git status", result[1].Pattern)
}

// TestDedupeSuggestions_无重复 测试无重复时保持不变
func TestDedupeSuggestions_无重复(t *testing.T) {
	suggestions := []PermissionSuggestion{
		{
			Tools:     []string{"bash"},
			MatchType: "command",
			Pattern:   "ls -la",
			Action:    "allow",
			Scope:     "exact",
			Reason:    "exact_command",
		},
		{
			Tools:     []string{"bash"},
			MatchType: "command",
			Pattern:   "git status",
			Action:    "allow",
			Scope:     "exact",
			Reason:    "exact_command",
		},
	}
	result := dedupeSuggestions(suggestions)
	assert.Len(t, result, 2)
}

// TestDedupeSuggestions_空列表 测试空列表返回空
func TestDedupeSuggestions_空列表(t *testing.T) {
	result := dedupeSuggestions(nil)
	assert.Nil(t, result)
}
