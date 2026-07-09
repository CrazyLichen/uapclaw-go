package sys_operation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── OperationMode 枚举 ────────────────────────────

// TestOperationMode_String 测试操作模式的字符串表示
func TestOperationMode_String(t *testing.T) {
	assert.Equal(t, "local", OperationModeLocal.String())
	assert.Equal(t, "sandbox", OperationModeSandbox.String())
	assert.Equal(t, "unknown(99)", OperationMode(99).String())
}

// TestOperationMode_MarshalJSON 测试操作模式的 JSON 序列化
func TestOperationMode_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(OperationModeLocal)
	require.NoError(t, err)
	assert.JSONEq(t, `"local"`, string(data))

	data, err = json.Marshal(OperationModeSandbox)
	require.NoError(t, err)
	assert.JSONEq(t, `"sandbox"`, string(data))
}

// TestOperationMode_UnmarshalJSON 测试操作模式的 JSON 反序列化
func TestOperationMode_UnmarshalJSON(t *testing.T) {
	var m OperationMode

	err := json.Unmarshal([]byte(`"local"`), &m)
	require.NoError(t, err)
	assert.Equal(t, OperationModeLocal, m)

	err = json.Unmarshal([]byte(`"sandbox"`), &m)
	require.NoError(t, err)
	assert.Equal(t, OperationModeSandbox, m)
}

// TestOperationMode_UnmarshalJSON_无效值 测试操作模式 JSON 反序列化无效值返回错误
func TestOperationMode_UnmarshalJSON_无效值(t *testing.T) {
	var m OperationMode
	err := json.Unmarshal([]byte(`"invalid"`), &m)
	assert.Error(t, err)
}

// TestOperationMode_默认值为LOCAL 测试操作模式默认值为 LOCAL(0)
func TestOperationMode_默认值为LOCAL(t *testing.T) {
	var m OperationMode
	assert.Equal(t, OperationModeLocal, m)
	assert.Equal(t, OperationMode(0), m)
}

// ──────────────────────────── ShellType 枚举 ────────────────────────────

// TestShellType_String 测试 Shell 类型的字符串表示
func TestShellType_String(t *testing.T) {
	assert.Equal(t, "auto", ShellTypeAuto.String())
	assert.Equal(t, "cmd", ShellTypeCmd.String())
	assert.Equal(t, "powershell", ShellTypePowerShell.String())
	assert.Equal(t, "bash", ShellTypeBash.String())
	assert.Equal(t, "sh", ShellTypeSh.String())
	assert.Equal(t, "unknown(99)", ShellType(99).String())
}

// TestParseShellType 测试字符串解析为 ShellType
func TestParseShellType(t *testing.T) {
	assert.Equal(t, ShellTypeAuto, ParseShellType("auto"))
	assert.Equal(t, ShellTypeCmd, ParseShellType("cmd"))
	assert.Equal(t, ShellTypePowerShell, ParseShellType("powershell"))
	assert.Equal(t, ShellTypeBash, ParseShellType("bash"))
	assert.Equal(t, ShellTypeSh, ParseShellType("sh"))
}

// TestParseShellType_无效输入 测试无效输入解析返回 ShellTypeAuto
func TestParseShellType_无效输入(t *testing.T) {
	assert.Equal(t, ShellTypeAuto, ParseShellType("zsh"))
	assert.Equal(t, ShellTypeAuto, ParseShellType(""))
	assert.Equal(t, ShellTypeAuto, ParseShellType("fish"))
}

// ──────────────────────────── ContainerScope 枚举 ────────────────────────────

// TestContainerScope_String 测试容器作用域的字符串表示
func TestContainerScope_String(t *testing.T) {
	assert.Equal(t, "system", ContainerScopeSystem.String())
	assert.Equal(t, "session", ContainerScopeSession.String())
	assert.Equal(t, "custom", ContainerScopeCustom.String())
	assert.Equal(t, "unknown(99)", ContainerScope(99).String())
}

// ──────────────────────────── FsOption 函数选项 ────────────────────────────

// TestFsOption 测试文件系统操作函数选项
func TestFsOption(t *testing.T) {
	opts := NewFsOptions(
		WithFsMode("read"),
		WithFsHead(10),
		WithFsTail(20),
		WithFsEncoding("utf-8"),
	)
	assert.Equal(t, "read", opts.Mode)
	assert.Equal(t, 10, opts.Head)
	assert.Equal(t, 20, opts.Tail)
	assert.Equal(t, "utf-8", opts.Encoding)
}

// TestFsOption_默认值 测试文件系统操作选项默认值
// 对齐 Python：mode 默认 "text"，encoding 默认 "utf-8"。
func TestFsOption_默认值(t *testing.T) {
	opts := NewFsOptions()
	assert.Equal(t, "text", opts.Mode)
	assert.Equal(t, 0, opts.Head)
	assert.Equal(t, 0, opts.Tail)
	assert.Equal(t, "utf-8", opts.Encoding)
}

// ──────────────────────────── ShellOption 函数选项 ────────────────────────────

// TestShellOption 测试 Shell 操作函数选项
func TestShellOption(t *testing.T) {
	env := map[string]string{"FOO": "bar"}
	opts := NewShellOptions(
		WithShellTimeout(60),
		WithShellEnvironment(env),
		WithShellType(ShellTypeBash),
	)
	assert.Equal(t, 60, opts.Timeout)
	assert.Equal(t, env, opts.Environment)
	assert.Equal(t, ShellTypeBash, opts.ShellType)
}

// TestShellOption_默认值 测试 Shell 操作选项默认值
// 对齐 Python：timeout 默认 300。
func TestShellOption_默认值(t *testing.T) {
	opts := NewShellOptions()
	assert.Equal(t, 300, opts.Timeout)
	assert.Nil(t, opts.Environment)
	assert.Equal(t, ShellTypeAuto, opts.ShellType)
}

// ──────────────────────────── CodeOption 函数选项 ────────────────────────────

// TestCodeOption 测试代码执行函数选项
func TestCodeOption(t *testing.T) {
	env := map[string]string{"PATH": "/usr/bin"}
	opts := NewCodeOptions(
		WithCodeLanguage("python"),
		WithCodeTimeout(30),
		WithCodeEnvironment(env),
	)
	assert.Equal(t, "python", opts.Language)
	assert.Equal(t, 30, opts.Timeout)
	assert.Equal(t, env, opts.Environment)
}

// TestCodeOption_默认值 测试代码执行选项默认值
// 对齐 Python：language 默认 "python"，timeout 默认 300。
func TestCodeOption_默认值(t *testing.T) {
	opts := NewCodeOptions()
	assert.Equal(t, "python", opts.Language)
	assert.Equal(t, 300, opts.Timeout)
	assert.Nil(t, opts.Environment)
}

// ──────────────────────────── Base*Operation 桩实现 ────────────────────────────

// TestBaseFsOperation_接口实现 测试 BaseFsOperation 满足 FsOperation 接口
func TestBaseFsOperation_接口实现(t *testing.T) {
	var _ FsOperation = (*BaseFsOperation)(nil)
}

// TestBaseShellOperation_接口实现 测试 BaseShellOperation 满足 ShellOperation 接口
func TestBaseShellOperation_接口实现(t *testing.T) {
	var _ ShellOperation = (*BaseShellOperation)(nil)
}

// TestBaseCodeOperation_接口实现 测试 BaseCodeOperation 满足 CodeOperation 接口
func TestBaseCodeOperation_接口实现(t *testing.T) {
	var _ CodeOperation = (*BaseCodeOperation)(nil)
}

// TestBaseSysOperation_接口实现 测试 BaseSysOperation 满足 SysOperation 接口
func TestBaseSysOperation_接口实现(t *testing.T) {
	var _ SysOperation = (*BaseSysOperation)(nil)
}

// TestBaseFsOperation_桩方法返回错误 测试 BaseFsOperation 所有桩方法返回未实现错误
func TestBaseFsOperation_桩方法返回错误(t *testing.T) {
	b := &BaseFsOperation{}
	ctx := context.Background()

	res, err := b.ReadFile(ctx, "/tmp/test")
	assert.Nil(t, res)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")

	res2, err := b.WriteFile(ctx, "/tmp/test", "content")
	assert.Nil(t, res2)
	assert.Error(t, err)

	res3, err := b.ListFiles(ctx, "/tmp")
	assert.Nil(t, res3)
	assert.Error(t, err)

	res4, err := b.ListDirectories(ctx, "/tmp")
	assert.Nil(t, res4)
	assert.Error(t, err)

	res5, err := b.SearchFiles(ctx, "/tmp", "*.go")
	assert.Nil(t, res5)
	assert.Error(t, err)

	tools := b.ListTools()
	assert.Nil(t, tools)
}

// TestBaseShellOperation_桩方法返回错误 测试 BaseShellOperation 所有桩方法返回未实现错误
func TestBaseShellOperation_桩方法返回错误(t *testing.T) {
	b := &BaseShellOperation{}
	ctx := context.Background()

	res, err := b.ExecuteCmd(ctx, "ls")
	assert.Nil(t, res)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")

	resStream, err := b.ExecuteCmdStream(ctx, "ls")
	assert.Nil(t, resStream)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")

	resBg, err := b.ExecuteCmdBackground(ctx, "ls")
	assert.Nil(t, resBg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")

	tools := b.ListTools()
	assert.Nil(t, tools)
}

// TestBaseCodeOperation_桩方法返回错误 测试 BaseCodeOperation 所有桩方法返回未实现错误
func TestBaseCodeOperation_桩方法返回错误(t *testing.T) {
	b := &BaseCodeOperation{}
	ctx := context.Background()

	res, err := b.ExecuteCode(ctx, "print('hello')")
	assert.Nil(t, res)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未实现")

	tools := b.ListTools()
	assert.Nil(t, tools)
}

// TestBaseSysOperation_桩方法返回零值 测试 BaseSysOperation 所有桩方法返回 nil 或零值
func TestBaseSysOperation_桩方法返回零值(t *testing.T) {
	b := &BaseSysOperation{}

	assert.Nil(t, b.Card())
	assert.Nil(t, b.Fs())
	assert.Nil(t, b.Shell())
	assert.Nil(t, b.Code())
	assert.Equal(t, "", b.IsolationKeyTemplate())
}

// ──────────────────────────── 结果类型默认值 ────────────────────────────

// TestReadFileResult_默认值 测试 ReadFileResult 默认值
func TestReadFileResult_默认值(t *testing.T) {
	var r result.ReadFileResult
	assert.Equal(t, 0, r.Code)
	assert.Equal(t, "", r.Message)
}

// TestWriteFileResult_默认值 测试 WriteFileResult 默认值
func TestWriteFileResult_默认值(t *testing.T) {
	var r result.WriteFileResult
	assert.Equal(t, 0, r.Code)
	assert.Equal(t, "", r.Message)
}

// TestExecuteCmdResult_默认值 测试 ExecuteCmdResult 默认值
func TestExecuteCmdResult_默认值(t *testing.T) {
	var r result.ExecuteCmdResult
	assert.Equal(t, 0, r.Code)
	assert.Equal(t, "", r.Message)
}

// TestExecuteCodeResult_默认值 测试 ExecuteCodeResult 默认值
func TestExecuteCodeResult_默认值(t *testing.T) {
	var r result.ExecuteCodeResult
	assert.Equal(t, 0, r.Code)
	assert.Equal(t, "", r.Message)
}
