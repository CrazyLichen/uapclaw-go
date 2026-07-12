package local

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── StreamEventType 枚举 ────────────────────────────

// TestStreamEventType_String 测试流事件类型字符串
func TestStreamEventType_String(t *testing.T) {
	assert.Equal(t, "stdout", StreamEventTypeStdout.String())
	assert.Equal(t, "stderr", StreamEventTypeStderr.String())
	assert.Equal(t, "exit", StreamEventTypeExit.String())
	assert.Equal(t, "error", StreamEventTypeError.String())
}

// ──────────────────────────── OperationUtils ────────────────────────────

// TestOperationUtils_PrepareEnvironment 测试环境准备
func TestOperationUtils_PrepareEnvironment(t *testing.T) {
	utils := OperationUtils{}
	customEnv := map[string]string{"FOO": "bar", "MY_VAR": "my_val"}

	env := utils.PrepareEnvironment(customEnv)
	assert.NotEmpty(t, env)
}

// TestOperationUtils_PrepareEnvironment_空 测试空自定义环境
func TestOperationUtils_PrepareEnvironment_空(t *testing.T) {
	utils := OperationUtils{}
	env := utils.PrepareEnvironment(nil)
	assert.NotEmpty(t, env)
}

// TestResolveCwd 测试工作目录解析
func TestResolveCwd(t *testing.T) {
	cwd := ResolveCwd("")
	assert.NotEmpty(t, cwd)

	tmpDir := t.TempDir()
	cwd = ResolveCwd(tmpDir)
	assert.Equal(t, tmpDir, cwd)
}

// TestOperationUtils_CreateTmpFile 测试临时文件创建
func TestOperationUtils_CreateTmpFile(t *testing.T) {
	utils := OperationUtils{}
	content := "hello world"

	path, err := utils.CreateTmpFile(content, ".py")
	require.NoError(t, err)
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

// TestOperationUtils_DeleteTmpFile 测试临时文件删除
func TestOperationUtils_DeleteTmpFile(t *testing.T) {
	utils := OperationUtils{}

	path, err := utils.CreateTmpFile("test", ".txt")
	require.NoError(t, err)

	err = utils.DeleteTmpFile(path)
	assert.NoError(t, err)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

// ──────────────────────────── LocalShellOperation ────────────────────────────

// TestNewLocalShellOperation 测试创建 Shell 操作实例
func TestNewLocalShellOperation(t *testing.T) {
	op := NewLocalShellOperation(nil)
	assert.NotNil(t, op)
	shellOp, ok := op.(*LocalShellOperation)
	assert.True(t, ok)
	assert.NotNil(t, shellOp)
}

// TestLocalShellOperation_ListTools 测试 Shell 操作 ListTools 返回 3 个 ToolCard
func TestLocalShellOperation_ListTools(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)
	cards := shellOp.ListTools()
	assert.Len(t, cards, 3)

	expectedNames := []string{"execute_cmd", "execute_cmd_stream", "execute_cmd_background"}
	for i, name := range expectedNames {
		assert.Equal(t, name, cards[i].Name, "第 %d 个工具名称不匹配", i)
		assert.NotEmpty(t, cards[i].Description, "第 %d 个工具描述不应为空", i)
		assert.NotEmpty(t, cards[i].InputParams, "第 %d 个工具应有输入参数", i)
	}

	// execute_cmd 第一个参数 command 应为必填
	assert.True(t, cards[0].InputParams[0].Required, "execute_cmd 的 command 参数应为必填")
	assert.Equal(t, "command", cards[0].InputParams[0].Name)

	// execute_cmd_background 没有 timeout 参数，有 grace 参数
	bgParams := cards[2].InputParams
	hasTimeout := false
	hasGrace := false
	for _, p := range bgParams {
		if p.Name == "timeout" {
			hasTimeout = true
		}
		if p.Name == "grace" {
			hasGrace = true
		}
	}
	assert.False(t, hasTimeout, "execute_cmd_background 不应有 timeout 参数")
	assert.True(t, hasGrace, "execute_cmd_background 应有 grace 参数")
}

// TestLocalShellOperation_ExecuteCmd_简单命令 测试 ExecuteCmd 执行简单命令
func TestLocalShellOperation_ExecuteCmd_简单命令(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)
	ctx := context.Background()

	res, err := shellOp.ExecuteCmd(ctx, "echo hello", sys_operation.WithShellTimeout(10))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
	assert.NotNil(t, res.Data)
}

// TestLocalShellOperation_ExecuteCmd_危险命令 测试 ExecuteCmd 拒绝危险命令
func TestLocalShellOperation_ExecuteCmd_危险命令(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)
	ctx := context.Background()

	res, err := shellOp.ExecuteCmd(ctx, "rm -rf /", sys_operation.WithShellTimeout(10))
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotEqual(t, 0, res.Code)
	}
}

// TestLocalShellOperation_checkCommandSafety 测试命令安全检查
func TestLocalShellOperation_checkCommandSafety(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)

	assert.Equal(t, "", shellOp.checkCommandSafety("ls -la"))
	assert.Equal(t, "", shellOp.checkCommandSafety("echo hello"))
	assert.Equal(t, "", shellOp.checkCommandSafety("cat file.txt"))

	assert.NotEqual(t, "", shellOp.checkCommandSafety("rm -rf /"))
	assert.NotEqual(t, "", shellOp.checkCommandSafety("shutdown -h now"))
	assert.NotEqual(t, "", shellOp.checkCommandSafety("mkfs /dev/sda1"))
}

// TestLocalShellOperation_detectAndMitigateTUI 测试 TUI 检测
func TestLocalShellOperation_detectAndMitigateTUI(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)
	env := map[string]string{"HOME": "/tmp"}

	mitigated, _ := shellOp.detectAndMitigateTUI("vim", env)
	assert.True(t, mitigated)

	mitigated, _ = shellOp.detectAndMitigateTUI("echo", env)
	assert.False(t, mitigated)
}

// TestLocalShellOperation_ExecuteCmdBackground 测试后台执行命令
func TestLocalShellOperation_ExecuteCmdBackground(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)
	ctx := context.Background()

	res, err := shellOp.ExecuteCmdBackground(ctx, "echo hello_bg", sys_operation.WithShellTimeout(10))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
	if res.Data != nil {
		assert.NotZero(t, res.Data.Pid)
	}
}

// TestLocalShellOperation_ExecuteCmdStream 测试流式执行命令
func TestLocalShellOperation_ExecuteCmdStream(t *testing.T) {
	shellOp := NewLocalShellOperation(nil).(*LocalShellOperation)
	ctx := context.Background()

	ch, err := shellOp.ExecuteCmdStream(ctx, "echo hello_stream", sys_operation.WithShellTimeout(10))
	require.NoError(t, err)

	var chunks []result.ExecuteCmdStreamResult
	timeout := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				goto done
			}
			chunks = append(chunks, chunk)
		case <-timeout:
			t.Fatal("stream timeout")
		}
	}
done:
	assert.NotEmpty(t, chunks)
}

// ──────────────────────────── LocalFsOperation ────────────────────────────

// TestNewLocalFsOperation 测试创建 FS 操作实例
func TestNewLocalFsOperation(t *testing.T) {
	op := NewLocalFsOperation(nil)
	assert.NotNil(t, op)
}

// TestLocalFsOperation_ListTools 测试 FS 操作 ListTools 返回 10 个 ToolCard
func TestLocalFsOperation_ListTools(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	cards := fsOp.ListTools()
	assert.Len(t, cards, 10)

	expectedNames := []string{
		"read_file", "read_file_stream", "write_file",
		"upload_file", "upload_file_stream",
		"download_file", "download_file_stream",
		"list_files", "list_directories", "search_files",
	}
	for i, name := range expectedNames {
		assert.Equal(t, name, cards[i].Name, "第 %d 个工具名称不匹配", i)
		assert.NotEmpty(t, cards[i].Description, "第 %d 个工具描述不应为空", i)
		assert.NotEmpty(t, cards[i].InputParams, "第 %d 个工具应有输入参数", i)
	}

	// read_file 第一个参数 path 应为必填
	assert.True(t, cards[0].InputParams[0].Required, "read_file 的 path 参数应为必填")
	assert.Equal(t, "path", cards[0].InputParams[0].Name)

	// write_file 的 content 参数应为必填
	assert.True(t, cards[2].InputParams[1].Required, "write_file 的 content 参数应为必填")
	assert.Equal(t, "content", cards[2].InputParams[1].Name)

	// search_files 只有 3 个参数（path, pattern, exclude_patterns）
	assert.Len(t, cards[9].InputParams, 3, "search_files 应有 3 个输入参数")
}

// TestLocalFsOperation_WriteFile_ReadFile 测试文件写入和读取
func TestLocalFsOperation_WriteFile_ReadFile(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	writeRes, err := fsOp.WriteFile(ctx, testFile, "hello world")
	require.NoError(t, err)
	assert.Equal(t, 0, writeRes.Code)

	readRes, err := fsOp.ReadFile(ctx, testFile)
	require.NoError(t, err)
	assert.Equal(t, 0, readRes.Code)
	if readRes.Data != nil {
		assert.Contains(t, readRes.Data.Content, "hello world")
	}
}

// TestLocalFsOperation_ListFiles 测试列出文件
func TestLocalFsOperation_ListFiles(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	require.NoError(t, err)

	res, err := fsOp.ListFiles(ctx, tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
}

// TestLocalFsOperation_ListDirectories 测试列出目录
func TestLocalFsOperation_ListDirectories(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	require.NoError(t, err)

	res, err := fsOp.ListDirectories(ctx, tmpDir)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
}

// TestLocalFsOperation_SearchFiles 测试搜索文件
func TestLocalFsOperation_SearchFiles(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpDir := t.TempDir()

	err := os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("package main"), 0644)
	require.NoError(t, err)

	res, err := fsOp.SearchFiles(ctx, tmpDir, "*.go")
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
}

// TestLocalFsOperation_ReadFile_不存在 测试读取不存在的文件
func TestLocalFsOperation_ReadFile_不存在(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()

	res, err := fsOp.ReadFile(ctx, "/tmp/nonexistent_file_12345.txt")
	if err != nil {
		assert.Error(t, err)
	} else {
		assert.NotEqual(t, 0, res.Code)
	}
}

// TestLocalFsOperation_WriteFile_Append 测试追加写入
func TestLocalFsOperation_WriteFile_Append(t *testing.T) {
	fsOp := NewLocalFsOperation(nil).(*LocalFsOperation)
	ctx := context.Background()
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "append.txt")

	_, err := fsOp.WriteFile(ctx, testFile, "line1\n")
	require.NoError(t, err)

	_, err = fsOp.WriteFile(ctx, testFile, "line2\n", sys_operation.WithFsAppend(true))
	require.NoError(t, err)

	readRes, err := fsOp.ReadFile(ctx, testFile)
	require.NoError(t, err)
	if readRes.Data != nil {
		assert.Contains(t, readRes.Data.Content, "line1")
		assert.Contains(t, readRes.Data.Content, "line2")
	}
}

// ──────────────────────────── LocalCodeOperation ────────────────────────────

// TestNewLocalCodeOperation 测试创建 Code 操作实例
func TestNewLocalCodeOperation(t *testing.T) {
	op := NewLocalCodeOperation(nil)
	assert.NotNil(t, op)
}

// TestLocalCodeOperation_ListTools 测试 Code 操作 ListTools 返回 2 个 ToolCard
func TestLocalCodeOperation_ListTools(t *testing.T) {
	codeOp := NewLocalCodeOperation(nil).(*LocalCodeOperation)
	cards := codeOp.ListTools()
	assert.Len(t, cards, 2)

	assert.Equal(t, "execute_code", cards[0].Name)
	assert.Equal(t, "execute_code_stream", cards[1].Name)
	assert.NotEmpty(t, cards[0].Description)
	assert.NotEmpty(t, cards[1].Description)
	assert.NotEmpty(t, cards[0].InputParams)
	assert.NotEmpty(t, cards[1].InputParams)

	// execute_code 的 code 参数应为必填
	assert.True(t, cards[0].InputParams[0].Required, "execute_code 的 code 参数应为必填")
	assert.Equal(t, "code", cards[0].InputParams[0].Name)

	// language 参数有枚举约束
	langParam := cards[0].InputParams[1]
	assert.Equal(t, "language", langParam.Name)
	assert.NotEmpty(t, langParam.Enum, "language 参数应有枚举约束")
}

// TestLocalCodeOperation_ExecuteCode_简单Python 测试执行简单 Python 代码
func TestLocalCodeOperation_ExecuteCode_简单Python(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); os.IsNotExist(err) {
		t.Skip("python3 not available")
	}

	codeOp := NewLocalCodeOperation(nil).(*LocalCodeOperation)
	ctx := context.Background()

	res, err := codeOp.ExecuteCode(ctx, "print('hello from python')", sys_operation.WithCodeLanguage("python"), sys_operation.WithCodeTimeout(10))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Code)
	if res.Data != nil {
		assert.Contains(t, res.Data.Stdout, "hello from python")
	}
}

// ──────────────────────────── ExpandUser ────────────────────────────

// TestExpandUser 测试路径展开
func TestExpandUser(t *testing.T) {
	home, _ := os.UserHomeDir()

	result := expandUser("~/test")
	if home != "" {
		assert.Equal(t, filepath.Join(home, "test"), result)
	}

	result = expandUser("/tmp/test")
	assert.Equal(t, "/tmp/test", result)
}
