package sys_operation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── mockSysOperation ────────────────────────────

// mockSysOperation 用于测试的 SysOperation 模拟实现
type mockSysOperation struct {
	card    *SysOperationCard
	fsOp    FsOperation
	shellOp ShellOperation
	codeOp  CodeOperation
}

func (m *mockSysOperation) Card() *SysOperationCard      { return m.card }
func (m *mockSysOperation) Fs() FsOperation              { return m.fsOp }
func (m *mockSysOperation) Shell() ShellOperation        { return m.shellOp }
func (m *mockSysOperation) Code() CodeOperation          { return m.codeOp }
func (m *mockSysOperation) IsolationKeyTemplate() string { return "" }

// mockFsOperation 用于测试的 FsOperation 模拟实现
type mockFsOperation struct{}

func (m *mockFsOperation) ReadFile(ctx context.Context, path string, opts ...FsOption) (*result.ReadFileResult, error) {
	return &result.ReadFileResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}, Data: &result.ReadFileData{Content: "test content"}}, nil
}
func (m *mockFsOperation) ReadFileStream(ctx context.Context, path string, opts ...FsOption) (<-chan result.ReadFileStreamResult, error) {
	ch := make(chan result.ReadFileStreamResult)
	close(ch)
	return ch, nil
}
func (m *mockFsOperation) WriteFile(ctx context.Context, path string, content string, opts ...FsOption) (*result.WriteFileResult, error) {
	return &result.WriteFileResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockFsOperation) UploadFile(ctx context.Context, localPath string, targetPath string, opts ...FsOption) (*result.UploadFileResult, error) {
	return &result.UploadFileResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockFsOperation) UploadFileStream(ctx context.Context, localPath string, targetPath string, opts ...FsOption) (<-chan result.UploadFileStreamResult, error) {
	ch := make(chan result.UploadFileStreamResult)
	close(ch)
	return ch, nil
}
func (m *mockFsOperation) DownloadFile(ctx context.Context, sourcePath string, localPath string, opts ...FsOption) (*result.DownloadFileResult, error) {
	return &result.DownloadFileResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockFsOperation) DownloadFileStream(ctx context.Context, sourcePath string, localPath string, opts ...FsOption) (<-chan result.DownloadFileStreamResult, error) {
	ch := make(chan result.DownloadFileStreamResult)
	close(ch)
	return ch, nil
}
func (m *mockFsOperation) ListFiles(ctx context.Context, path string, opts ...FsOption) (*result.ListFilesResult, error) {
	return &result.ListFilesResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockFsOperation) ListDirectories(ctx context.Context, path string, opts ...FsOption) (*result.ListDirsResult, error) {
	return &result.ListDirsResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockFsOperation) SearchFiles(ctx context.Context, path string, pattern string, opts ...FsOption) (*result.SearchFilesResult, error) {
	return &result.SearchFilesResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockFsOperation) ListTools() []*tool.ToolCard {
	return []*tool.ToolCard{
		tool.NewToolCard("read_file", "Read a file", nil, nil),
		tool.NewToolCard("read_file_stream", "Read a file streaming", nil, nil),
		tool.NewToolCard("write_file", "Write a file", nil, nil),
		tool.NewToolCard("upload_file", "Upload file", nil, nil),
		tool.NewToolCard("upload_file_stream", "Upload file streaming", nil, nil),
		tool.NewToolCard("download_file", "Download file", nil, nil),
		tool.NewToolCard("download_file_stream", "Download file streaming", nil, nil),
		tool.NewToolCard("list_files", "List files", nil, nil),
		tool.NewToolCard("list_directories", "List directories", nil, nil),
		tool.NewToolCard("search_files", "Search files", nil, nil),
	}
}

// mockShellOperation 用于测试的 ShellOperation 模拟实现
type mockShellOperation struct{}

func (m *mockShellOperation) ExecuteCmd(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdResult, error) {
	return &result.ExecuteCmdResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockShellOperation) ExecuteCmdStream(ctx context.Context, command string, opts ...ShellOption) (<-chan result.ExecuteCmdStreamResult, error) {
	ch := make(chan result.ExecuteCmdStreamResult)
	close(ch)
	return ch, nil
}
func (m *mockShellOperation) ExecuteCmdBackground(ctx context.Context, command string, opts ...ShellOption) (*result.ExecuteCmdBackgroundResult, error) {
	return &result.ExecuteCmdBackgroundResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockShellOperation) WriteStdin(ctx context.Context, sessionID string, data string, opts ...ShellOption) (*result.ExecuteCmdResult, error) {
	return &result.ExecuteCmdResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockShellOperation) KillProcess(ctx context.Context, sessionID string, opts ...ShellOption) (*result.ExecuteCmdResult, error) {
	return &result.ExecuteCmdResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockShellOperation) ListProcesses(ctx context.Context, opts ...ShellOption) (*result.ExecuteCmdResult, error) {
	return &result.ExecuteCmdResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockShellOperation) ListTools() []*tool.ToolCard {
	return []*tool.ToolCard{
		tool.NewToolCard("execute_cmd", "Execute a command", nil, nil),
		tool.NewToolCard("execute_cmd_stream", "Execute a command with streaming", nil, nil),
		tool.NewToolCard("execute_cmd_background", "Execute a command in background", nil, nil),
	}
}

// mockCodeOperation 用于测试的 CodeOperation 模拟实现
type mockCodeOperation struct{}

func (m *mockCodeOperation) ExecuteCode(ctx context.Context, code string, opts ...CodeOption) (*result.ExecuteCodeResult, error) {
	return &result.ExecuteCodeResult{BaseResult: result.BaseResult{Code: 0, Message: "ok"}}, nil
}
func (m *mockCodeOperation) ExecuteCodeStream(ctx context.Context, code string, opts ...CodeOption) (<-chan result.ExecuteCodeStreamResult, error) {
	ch := make(chan result.ExecuteCodeStreamResult)
	close(ch)
	return ch, nil
}
func (m *mockCodeOperation) ListTools() []*tool.ToolCard {
	return []*tool.ToolCard{
		tool.NewToolCard("execute_code", "Execute code", nil, nil),
		tool.NewToolCard("execute_code_stream", "Execute code with streaming", nil, nil),
	}
}

// ──────────────────────────── ToolAdapter 测试 ────────────────────────────

// TestSysOperationToolAdapter_ExtractTools 测试提取工具
func TestSysOperationToolAdapter_ExtractTools(t *testing.T) {
	// 在 GlobalRegistry 注册 mock 操作（幂等，不影响其他测试）
	GlobalRegistry.Register(OperationDef{
		Name:        "fs",
		Mode:        OperationModeLocal,
		Description: "FS 操作",
		NewFunc:     func(any) SysSubOperation { return &mockFsOperation{} },
	})
	GlobalRegistry.Register(OperationDef{
		Name:        "shell",
		Mode:        OperationModeLocal,
		Description: "Shell 操作",
		NewFunc:     func(any) SysSubOperation { return &mockShellOperation{} },
	})
	GlobalRegistry.Register(OperationDef{
		Name:        "code",
		Mode:        OperationModeLocal,
		Description: "Code 操作",
		NewFunc:     func(any) SysSubOperation { return &mockCodeOperation{} },
	})

	card := NewSysOperationCard(WithSysOpMode(OperationModeLocal))
	instance := &mockSysOperation{
		card:    card,
		fsOp:    &mockFsOperation{},
		shellOp: &mockShellOperation{},
		codeOp:  &mockCodeOperation{},
	}

	adapter := SysOperationToolAdapter{}
	entries, err := adapter.ExtractTools(card, instance, "", "")
	require.NoError(t, err)
	// 应该提取出 10(fs) + 3(shell) + 2(code) = 15 个工具
	assert.Len(t, entries, 15)

	// 验证工具 ID 格式
	for _, entry := range entries {
		assert.NotEmpty(t, entry.ToolID)
		assert.NotNil(t, entry.Tool)
	}
}

// TestSysOperationToolAdapter_ExtractTools_nilCard 测试 nil 卡片返回错误
func TestSysOperationToolAdapter_ExtractTools_nilCard(t *testing.T) {
	adapter := SysOperationToolAdapter{}
	_, err := adapter.ExtractTools(nil, &mockSysOperation{}, "", "")
	assert.Error(t, err)
}

// TestSysOperationToolAdapter_ExtractTools_nilInstance 测试 nil 实例返回错误
func TestSysOperationToolAdapter_ExtractTools_nilInstance(t *testing.T) {
	card := NewSysOperationCard()
	adapter := SysOperationToolAdapter{}
	_, err := adapter.ExtractTools(card, nil, "", "")
	assert.Error(t, err)
}

// TestDispatchOperationMethod 测试操作方法分发
func TestDispatchOperationMethod(t *testing.T) {
	instance := &mockSysOperation{
		fsOp:    &mockFsOperation{},
		shellOp: &mockShellOperation{},
		codeOp:  &mockCodeOperation{},
	}
	ctx := context.Background()

	// shell - execute_cmd
	result, err := dispatchOperationMethod(instance, "shell", "execute_cmd", ctx, map[string]any{"command": "ls"})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// fs - read_file
	result, err = dispatchOperationMethod(instance, "fs", "read_file", ctx, map[string]any{"path": "/tmp/test"})
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// code - execute_code
	result, err = dispatchOperationMethod(instance, "code", "execute_code", ctx, map[string]any{"code": "print('hello')"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestDispatchOperationMethod_未知类型 测试未知操作类型
func TestDispatchOperationMethod_未知类型(t *testing.T) {
	instance := &mockSysOperation{}
	_, err := dispatchOperationMethod(instance, "unknown", "method", context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未知的操作类型")
}

// TestDispatchShellMethod 测试 Shell 方法分发
func TestDispatchShellMethod(t *testing.T) {
	shellOp := &mockShellOperation{}
	ctx := context.Background()

	// execute_cmd
	res, err := dispatchShellMethod(shellOp, ctx, "execute_cmd", map[string]any{"command": "echo hello"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// execute_cmd_stream
	res, err = dispatchShellMethod(shellOp, ctx, "execute_cmd_stream", map[string]any{"command": "echo hello"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// execute_cmd_background
	res, err = dispatchShellMethod(shellOp, ctx, "execute_cmd_background", map[string]any{"command": "sleep 1"})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchShellMethod_带选项 测试 Shell 方法带选项分发
func TestDispatchShellMethod_带选项(t *testing.T) {
	shellOp := &mockShellOperation{}
	ctx := context.Background()

	res, err := dispatchShellMethod(shellOp, ctx, "execute_cmd", map[string]any{
		"command":    "ls",
		"cwd":        "/tmp",
		"timeout":    float64(60),
		"shell_type": "bash",
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchShellMethod_未知方法 测试 Shell 未知方法
func TestDispatchShellMethod_未知方法(t *testing.T) {
	shellOp := &mockShellOperation{}
	_, err := dispatchShellMethod(shellOp, context.Background(), "unknown_method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未知的 shell 方法")
}

// TestDispatchFsMethod 测试 FS 方法分发
func TestDispatchFsMethod(t *testing.T) {
	fsOp := &mockFsOperation{}
	ctx := context.Background()

	// read_file
	res, err := dispatchFsMethod(fsOp, ctx, "read_file", map[string]any{"path": "/tmp/test"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// write_file
	res, err = dispatchFsMethod(fsOp, ctx, "write_file", map[string]any{"path": "/tmp/test", "content": "hello"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// list_files
	res, err = dispatchFsMethod(fsOp, ctx, "list_files", map[string]any{"path": "/tmp"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// list_directories
	res, err = dispatchFsMethod(fsOp, ctx, "list_directories", map[string]any{"path": "/tmp"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// search_files
	res, err = dispatchFsMethod(fsOp, ctx, "search_files", map[string]any{"path": "/tmp", "pattern": "*.go"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// upload_file
	res, err = dispatchFsMethod(fsOp, ctx, "upload_file", map[string]any{"local_path": "/tmp/a", "target_path": "/tmp/b"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// download_file
	res, err = dispatchFsMethod(fsOp, ctx, "download_file", map[string]any{"source_path": "/tmp/a", "local_path": "/tmp/b"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// read_file_stream
	res, err = dispatchFsMethod(fsOp, ctx, "read_file_stream", map[string]any{"path": "/tmp/test"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// upload_file_stream
	res, err = dispatchFsMethod(fsOp, ctx, "upload_file_stream", map[string]any{"local_path": "/tmp/a", "target_path": "/tmp/b"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// download_file_stream
	res, err = dispatchFsMethod(fsOp, ctx, "download_file_stream", map[string]any{"source_path": "/tmp/a", "local_path": "/tmp/b"})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchFsMethod_未知方法 测试 FS 未知方法
func TestDispatchFsMethod_未知方法(t *testing.T) {
	fsOp := &mockFsOperation{}
	_, err := dispatchFsMethod(fsOp, context.Background(), "unknown_method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未知的 fs 方法")
}

// TestDispatchCodeMethod 测试 Code 方法分发
func TestDispatchCodeMethod(t *testing.T) {
	codeOp := &mockCodeOperation{}
	ctx := context.Background()

	// execute_code
	res, err := dispatchCodeMethod(codeOp, ctx, "execute_code", map[string]any{"code": "print('hello')"})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	// execute_code_stream
	res, err = dispatchCodeMethod(codeOp, ctx, "execute_code_stream", map[string]any{"code": "print('hello')"})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchCodeMethod_未知方法 测试 Code 未知方法
func TestDispatchCodeMethod_未知方法(t *testing.T) {
	codeOp := &mockCodeOperation{}
	_, err := dispatchCodeMethod(codeOp, context.Background(), "unknown_method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未知的 code 方法")
}

// TestStructToMap 测试结构体转 map
func TestStructToMap(t *testing.T) {
	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	s := testStruct{Name: "test", Value: 42}
	m := structToMap(s)
	assert.Equal(t, "test", m["name"])
	assert.Equal(t, float64(42), m["value"])
}

// TestStructToMap_无效JSON 测试不可序列化值
func TestStructToMap_无效JSON(t *testing.T) {
	m := structToMap(make(chan int))
	assert.Contains(t, m, "error")
}

// TestStructToMap_嵌套 测试嵌套结构体
func TestStructToMap_嵌套(t *testing.T) {
	r := result.BaseResult{Code: 0, Message: "ok"}
	m := structToMap(r)
	assert.Equal(t, float64(0), m["code"])
	assert.Equal(t, "ok", m["message"])
}

// TestDispatchShellMethod_带选项_全参数 测试 Shell 全参数分发
func TestDispatchShellMethod_带选项_全参数(t *testing.T) {
	shellOp := &mockShellOperation{}
	ctx := context.Background()

	res, err := dispatchShellMethod(shellOp, ctx, "execute_cmd", map[string]any{
		"command":    "ls",
		"cwd":        "/tmp",
		"timeout":    float64(60),
		"shell_type": "bash",
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchCodeMethod_带选项 测试 Code 带选项分发
func TestDispatchCodeMethod_带选项(t *testing.T) {
	codeOp := &mockCodeOperation{}
	ctx := context.Background()

	res, err := dispatchCodeMethod(codeOp, ctx, "execute_code", map[string]any{
		"code":     "print(1)",
		"language": "python",
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)

	res, err = dispatchCodeMethod(codeOp, ctx, "execute_code_stream", map[string]any{
		"code":     "print(1)",
		"language": "node",
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// ──────────────────────────── GetToolIDPrefix ────────────────────────────

// TestGetToolIDPrefix_字符串 测试字符串输入
func TestGetToolIDPrefix_字符串(t *testing.T) {
	adapter := SysOperationToolAdapter{}
	result := adapter.GetToolIDPrefix("my_op")
	assert.Equal(t, "my_op.", result)
}

// TestGetToolIDPrefix_字符串列表 测试字符串列表输入
func TestGetToolIDPrefix_字符串列表(t *testing.T) {
	adapter := SysOperationToolAdapter{}
	result := adapter.GetToolIDPrefix([]string{"op1", "op2"})
	assert.Equal(t, []string{"op1.", "op2."}, result)
}

// TestGetToolIDPrefix_其他类型 测试其他类型返回空字符串
func TestGetToolIDPrefix_其他类型(t *testing.T) {
	adapter := SysOperationToolAdapter{}
	assert.Equal(t, "", adapter.GetToolIDPrefix(123))
	assert.Equal(t, "", adapter.GetToolIDPrefix(nil))
}

// ──────────────────────────── dispatchShellMethod 新方法 ────────────────────────────

// TestDispatchShellMethod_WriteStdin 测试 write_stdin 分发
func TestDispatchShellMethod_WriteStdin(t *testing.T) {
	shellOp := &mockShellOperation{}
	ctx := context.Background()
	res, err := dispatchShellMethod(shellOp, ctx, "write_stdin", map[string]any{
		"session_id": "sess1",
		"data":       "hello",
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchShellMethod_KillProcess 测试 kill_process 分发
func TestDispatchShellMethod_KillProcess(t *testing.T) {
	shellOp := &mockShellOperation{}
	ctx := context.Background()
	res, err := dispatchShellMethod(shellOp, ctx, "kill_process", map[string]any{
		"session_id": "sess1",
	})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchShellMethod_ListProcesses 测试 list_processes 分发
func TestDispatchShellMethod_ListProcesses(t *testing.T) {
	shellOp := &mockShellOperation{}
	ctx := context.Background()
	res, err := dispatchShellMethod(shellOp, ctx, "list_processes", map[string]any{})
	assert.NoError(t, err)
	assert.NotNil(t, res)
}

// TestDispatchOperationMethod_空Op 测试操作方法空子操作
func TestDispatchOperationMethod_空Op(t *testing.T) {
	instance := &mockSysOperation{
		fsOp:    nil,
		shellOp: nil,
		codeOp:  nil,
	}
	_, err := dispatchOperationMethod(instance, "fs", "read_file", context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不可用")

	_, err = dispatchOperationMethod(instance, "shell", "execute_cmd", context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不可用")

	_, err = dispatchOperationMethod(instance, "code", "execute_code", context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不可用")
}
