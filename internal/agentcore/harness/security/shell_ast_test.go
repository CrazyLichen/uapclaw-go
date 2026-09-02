package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestParseShellForPermission_简单命令 测试简单命令解析
// 对齐 Python: parse_shell_for_permission("ls -la") → Simple, 1 subcommand
func TestParseShellForPermission_简单命令(t *testing.T) {
	result := ParseShellForPermission("ls -la")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindSimple, result.Kind)
	require.Len(t, result.Subcommands, 1)
	assert.Equal(t, "ls -la", result.Subcommands[0].Text)
	assert.Contains(t, result.Subcommands[0].Argv, "ls")
}

// TestParseShellForPermission_空命令 测试空命令
// 对齐 Python: parse_shell_for_permission("") → Simple, 0 subcommands
func TestParseShellForPermission_空命令(t *testing.T) {
	result := ParseShellForPermission("")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindSimple, result.Kind)
	assert.Empty(t, result.Subcommands)
	assert.Equal(t, "fallback", result.Backend)
}

// TestParseShellForPermission_管道 测试管道命令
// 对齐 Python: pipeline → Pipeline=True, Simple（管道不是风险结构）
func TestParseShellForPermission_管道(t *testing.T) {
	result := ParseShellForPermission("echo hello | grep h")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindSimple, result.Kind, "管道不是风险结构，应为 Simple")
	assert.True(t, result.Flags.Pipeline)
	require.Len(t, result.Subcommands, 2, "管道应拆分为 2 个子命令")
	assert.Equal(t, "echo hello", result.Subcommands[0].Text)
	assert.Equal(t, "grep h", result.Subcommands[1].Text)
}

// TestParseShellForPermission_复合命令 测试复合操作符
// 对齐 Python: list/compound → CompoundOperators=True, Simple
func TestParseShellForPermission_复合命令(t *testing.T) {
	result := ParseShellForPermission("ls && pwd")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindSimple, result.Kind, "复合操作符不是风险结构，应为 Simple")
	assert.True(t, result.Flags.CompoundOperators)
	require.Len(t, result.Subcommands, 2)
}

// TestParseShellForPermission_命令替换 测试命令替换
// 对齐 Python: command_substitution → TooComplex
func TestParseShellForPermission_命令替换(t *testing.T) {
	result := ParseShellForPermission("$(whoami)")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindTooComplex, result.Kind, "命令替换是风险结构，应为 TooComplex")
	assert.True(t, result.Flags.CommandSubstitution)
}

// TestParseShellForPermission_Heredoc 测试 Here 文档
// 对齐 Python: heredoc → TooComplex
func TestParseShellForPermission_Heredoc(t *testing.T) {
	result := ParseShellForPermission("cat <<EOF\nhello\nEOF")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindTooComplex, result.Kind, "heredoc 是风险结构，应为 TooComplex")
	assert.True(t, result.Flags.Heredoc)
}

// TestParseShellForPermission_分号 测试分号分隔命令
// 对齐 Python: semicolon → CompoundOperators=True, Simple
func TestParseShellForPermission_分号(t *testing.T) {
	result := ParseShellForPermission("ls; pwd")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindSimple, result.Kind)
	assert.True(t, result.Flags.CompoundOperators)
	require.Len(t, result.Subcommands, 2)
}

// TestParseShellForPermission_子Shell 测试子 shell
// 对齐 Python: subshell → TooComplex
func TestParseShellForPermission_子Shell(t *testing.T) {
	result := ParseShellForPermission("(ls && pwd)")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindTooComplex, result.Kind, "子 shell 是风险结构，应为 TooComplex")
	assert.True(t, result.Flags.Subshell)
}

// TestParseShellForPermission_参数展开 测试参数展开
// 对齐 Python: parameter_expansion → TooComplex
func TestParseShellForPermission_参数展开(t *testing.T) {
	result := ParseShellForPermission("echo ${HOME}")
	require.NotNil(t, result)
	assert.Equal(t, ShellAstKindTooComplex, result.Kind, "参数展开是风险结构，应为 TooComplex")
	assert.True(t, result.Flags.ParameterExpansion)
}

// TestShellStructureFlags_HasRiskyStructure 测试风险结构判断
func TestShellStructureFlags_HasRiskyStructure(t *testing.T) {
	// 无任何标志
	empty := ShellStructureFlags{}
	assert.False(t, empty.HasRiskyStructure())

	// 有管道
	pipeline := ShellStructureFlags{Pipeline: true}
	assert.True(t, pipeline.HasRiskyStructure())

	// 有命令替换
	cmdSub := ShellStructureFlags{CommandSubstitution: true}
	assert.True(t, cmdSub.HasRiskyStructure())
}

// TestShellAstKind_String 测试 ShellAstKind 字符串表示
func TestShellAstKind_String(t *testing.T) {
	assert.Equal(t, "simple", ShellAstKindSimple.String())
	assert.Equal(t, "too_complex", ShellAstKindTooComplex.String())
	assert.Equal(t, "parse_unavailable", ShellAstKindParseUnavailable.String())
}

// TestShlexSplit 测试 shell 分词
func TestShlexSplit(t *testing.T) {
	assert.Equal(t, []string{"ls", "-la"}, shlexSplit("ls -la"))
	assert.Equal(t, []string{"echo", "hello world"}, shlexSplit(`echo "hello world"`))
	assert.Equal(t, []string{"echo", "hello world"}, shlexSplit(`echo 'hello world'`))
	assert.Equal(t, []string{"echo", "hello world"}, shlexSplit(`echo hello\ world`))
	assert.Nil(t, shlexSplit(""))
	assert.Equal(t, []string{"ls"}, shlexSplit("ls"))
}

// TestScanShellStructure 测试保守扫描
func TestScanShellStructure(t *testing.T) {
	flags := scanShellStructure("ls -la")
	assert.False(t, flags.Pipeline)
	assert.False(t, flags.CompoundOperators)
	assert.False(t, flags.CommandSubstitution)

	flagsPipe := scanShellStructure("echo hello | grep h")
	assert.True(t, flagsPipe.Pipeline)

	flagsCompound := scanShellStructure("ls && pwd")
	assert.True(t, flagsCompound.CompoundOperators)

	flagsCmdSub := scanShellStructure("$(whoami)")
	assert.True(t, flagsCmdSub.CommandSubstitution)

	flagsHeredoc := scanShellStructure("cat <<EOF")
	assert.True(t, flagsHeredoc.Heredoc)
}

// TestParseWithConservativeFallback 测试保守扫描 fallback
func TestParseWithConservativeFallback(t *testing.T) {
	// 简单命令 → Simple
	result := parseWithConservativeFallback("ls -la")
	assert.Equal(t, ShellAstKindSimple, result.Kind)
	assert.Equal(t, "fallback", result.Backend)

	// 含管道 → ParseUnavailable（保守扫描将管道视为风险结构）
	resultPipe := parseWithConservativeFallback("echo hello | grep h")
	assert.Equal(t, ShellAstKindParseUnavailable, resultPipe.Kind, "保守扫描中管道应返回 ParseUnavailable")
}
