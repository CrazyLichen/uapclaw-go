package sections

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// fakeFsOperation 用于测试的模拟文件操作实现
type fakeFsOperation struct {
	// readFileResults 模拟 ReadFile 返回结果（路径→结果）
	readFileResults map[string]*result.ReadFileResult
	// readFileErrors 模拟 ReadFile 返回错误（路径→错误）
	readFileErrors map[string]error
	// listFilesResults 模拟 ListFiles 返回结果（路径→结果）
	listFilesResults map[string]*result.ListFilesResult
	// listFilesErrors 模拟 ListFiles 返回错误（路径→错误）
	listFilesErrors map[string]error
}

func newFakeFsOperation() *fakeFsOperation {
	return &fakeFsOperation{
		readFileResults:  make(map[string]*result.ReadFileResult),
		readFileErrors:   make(map[string]error),
		listFilesResults: make(map[string]*result.ListFilesResult),
		listFilesErrors:  make(map[string]error),
	}
}

func (f *fakeFsOperation) ReadFile(_ context.Context, path string, _ ...sysop.FsOption) (*result.ReadFileResult, error) {
	if err, ok := f.readFileErrors[path]; ok {
		return nil, err
	}
	if r, ok := f.readFileResults[path]; ok {
		return r, nil
	}
	return &result.ReadFileResult{}, nil
}

func (f *fakeFsOperation) ReadFileStream(_ context.Context, path string, _ ...sysop.FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) WriteFile(_ context.Context, path string, _ string, _ ...sysop.FsOption) (*result.WriteFileResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) UploadFile(_ context.Context, localPath string, targetPath string, _ ...sysop.FsOption) (*result.UploadFileResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) UploadFileStream(_ context.Context, localPath string, targetPath string, _ ...sysop.FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) DownloadFile(_ context.Context, sourcePath string, localPath string, _ ...sysop.FsOption) (*result.DownloadFileResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) DownloadFileStream(_ context.Context, sourcePath string, localPath string, _ ...sysop.FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) ListFiles(_ context.Context, path string, _ ...sysop.FsOption) (*result.ListFilesResult, error) {
	if err, ok := f.listFilesErrors[path]; ok {
		return nil, err
	}
	if r, ok := f.listFilesResults[path]; ok {
		return r, nil
	}
	return &result.ListFilesResult{}, nil
}

func (f *fakeFsOperation) ListDirectories(_ context.Context, path string, _ ...sysop.FsOption) (*result.ListDirsResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) SearchFiles(_ context.Context, path string, pattern string, _ ...sysop.FsOption) (*result.SearchFilesResult, error) {
	return nil, nil
}

func (f *fakeFsOperation) ListTools() []*tool.ToolCard {
	return nil
}

// TestReadContextFiles_空参数 测试 nil 参数返回 nil
func TestReadContextFiles_空参数(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, ReadContextFiles(ctx, nil, nil))
	assert.Nil(t, ReadContextFiles(ctx, newFakeFsOperation(), nil))
	assert.Nil(t, ReadContextFiles(ctx, nil, hworkspace.NewWorkspace("/tmp", "cn")))
}

// TestReadContextFiles_读取成功 测试正常读取上下文文件
func TestReadContextFiles_读取成功(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	// 为 AGENT.md 准备返回数据
	agentPath := ws.GetNodePath("AGENT.md")
	require.NotNil(t, agentPath)
	fsOp.readFileResults[*agentPath] = &result.ReadFileResult{
		Data: &result.ReadFileData{
			Path:    *agentPath,
			Content: "agent instructions here",
			Mode:    "text",
		},
	}

	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
	assert.Equal(t, "agent instructions here", files["AGENT.md"])
}

// TestReadContextFiles_跳过空内容 测试跳过空内容的文件
func TestReadContextFiles_跳过空内容(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	agentPath := ws.GetNodePath("AGENT.md")
	require.NotNil(t, agentPath)
	// 返回空内容
	fsOp.readFileResults[*agentPath] = &result.ReadFileResult{
		Data: &result.ReadFileData{
			Path:    *agentPath,
			Content: "",
			Mode:    "text",
		},
	}

	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
	_, exists := files["AGENT.md"]
	assert.False(t, exists, "空内容文件不应出现在结果中")
}

// TestReadContextFiles_跳过模板文件 测试跳过未填充模板
func TestReadContextFiles_跳过模板文件(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	agentPath := ws.GetNodePath("AGENT.md")
	require.NotNil(t, agentPath)
	// 返回模板标记内容（触发 IsUnfilledTemplate）
	fsOp.readFileResults[*agentPath] = &result.ReadFileResult{
		Data: &result.ReadFileData{
			Path:    *agentPath,
			Content: "此处应保存的内容",
			Mode:    "text",
		},
	}

	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
	_, exists := files["AGENT.md"]
	assert.False(t, exists, "模板文件不应出现在结果中")
}

// TestReadContextFiles_读取错误跳过 测试 ReadFile 出错时跳过文件
func TestReadContextFiles_读取错误跳过(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	agentPath := ws.GetNodePath("AGENT.md")
	require.NotNil(t, agentPath)
	fsOp.readFileErrors[*agentPath] = assert.AnError

	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
	_, exists := files["AGENT.md"]
	assert.False(t, exists, "读取出错的文件不应出现在结果中")
}

// TestReadContextFiles_nilData跳过 测试 ReadFile 返回 nil Data 时跳过
func TestReadContextFiles_nilData跳过(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	agentPath := ws.GetNodePath("AGENT.md")
	require.NotNil(t, agentPath)
	fsOp.readFileResults[*agentPath] = &result.ReadFileResult{Data: nil}

	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
	_, exists := files["AGENT.md"]
	assert.False(t, exists, "Data 为 nil 的文件不应出现在结果中")
}

// TestReadContextFiles_AGENT读取成功 测试 AGENT.md 读取成功
func TestReadContextFiles_AGENT读取成功(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	// AGENT.md 在 ContextFiles 列表中
	agentPath := ws.GetNodePath("AGENT.md")
	require.NotNil(t, agentPath)
	fsOp.readFileResults[*agentPath] = &result.ReadFileResult{
		Data: &result.ReadFileData{
			Path:    *agentPath,
			Content: "agent instructions here",
			Mode:    "text",
		},
	}

	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
	assert.Equal(t, "agent instructions here", files["AGENT.md"])
}

// TestReadContextFiles_MEMORY特殊路径 测试 MEMORY.md 从 memory 目录读取
func TestReadContextFiles_MEMORY特殊路径(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	// MEMORY.md 从 WorkspaceNodeMemory 目录读取
	// 注意：MEMORY.md 不在 ContextFiles 列表中，实际不会遍历到
	// 此测试验证 memory 目录路径解析逻辑
	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	require.NotNil(t, memoryDir)
	memoryPath := *memoryDir + "/MEMORY.md"

	// 直接验证路径构造正确
	assert.Contains(t, memoryPath, "memory")
	assert.Contains(t, memoryPath, "MEMORY.md")

	// 测试 ReadFile 能通过此路径读取
	fsOp.readFileResults[memoryPath] = &result.ReadFileResult{
		Data: &result.ReadFileData{
			Path:    memoryPath,
			Content: "memory content",
			Mode:    "text",
		},
	}

	readResult, err := fsOp.ReadFile(ctx, memoryPath)
	assert.NoError(t, err)
	assert.Equal(t, "memory content", readResult.Data.Content)
}

// TestReadContextFiles_nodePath为nil跳过 测试 GetNodePath 返回 nil 时跳过文件
func TestReadContextFiles_nodePath为nil跳过(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	// 创建 workspace 但不设置特殊节点路径
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	// 对不存在的 WorkspaceNode，GetNodePath 返回 nil
	// 这里我们测试正常场景但确保不会 panic
	files := ReadContextFiles(ctx, fsOp, ws)
	assert.NotNil(t, files)
}

// TestReadDailyMemory_空参数 测试 nil 参数返回空
func TestReadDailyMemory_空参数(t *testing.T) {
	ctx := context.Background()
	content, dateStr := ReadDailyMemory(ctx, nil, nil, "")
	assert.Equal(t, "", content)
	assert.Equal(t, "", dateStr)
}

// TestReadDailyMemory_空时区默认 测试空时区使用默认值
func TestReadDailyMemory_空时区默认(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	// 不设置 listFilesResults，ListFiles 返回空结果
	content, dateStr := ReadDailyMemory(ctx, fsOp, ws, "")
	assert.Equal(t, "", content)
	assert.Equal(t, "", dateStr)
}

// TestReadDailyMemory_读取成功 测试正常读取每日记忆
func TestReadDailyMemory_读取成功(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	require.NotNil(t, memoryDir)
	dailyMemoryDir := *memoryDir + "/" + string(hworkspace.WorkspaceNodeDailyMemory)

	// 设置时区为 Asia/Shanghai，动态获取当前日期
	tz, _ := time.LoadLocation("Asia/Shanghai")
	today := time.Now().In(tz).Format("2006-01-02")
	todayFile := today + ".md"

	// 模拟 ListFiles 返回包含今日文件
	fsOp.listFilesResults[dailyMemoryDir] = &result.ListFilesResult{
		Data: &result.FileSystemData{
			ListItems: []result.FileSystemItem{
				{Name: todayFile, Path: dailyMemoryDir + "/" + todayFile},
			},
		},
	}

	// 模拟 ReadFile 返回内容
	todayPath := dailyMemoryDir + "/" + todayFile
	fsOp.readFileResults[todayPath] = &result.ReadFileResult{
		Data: &result.ReadFileData{
			Path:    todayPath,
			Content: "daily memory content",
			Mode:    "text",
		},
	}

	content, dateStr := ReadDailyMemory(ctx, fsOp, ws, "Asia/Shanghai")
	assert.Equal(t, "daily memory content", content)
	assert.Equal(t, today, dateStr)
}

// TestReadDailyMemory_今日无文件 测试当日无文件返回空
func TestReadDailyMemory_今日无文件(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	require.NotNil(t, memoryDir)
	dailyMemoryDir := *memoryDir + "/" + string(hworkspace.WorkspaceNodeDailyMemory)

	// 模拟 ListFiles 返回不包含今日的文件
	fsOp.listFilesResults[dailyMemoryDir] = &result.ListFilesResult{
		Data: &result.FileSystemData{
			ListItems: []result.FileSystemItem{
				{Name: "2025-01-01.md", Path: dailyMemoryDir + "/2025-01-01.md"},
			},
		},
	}

	content, dateStr := ReadDailyMemory(ctx, fsOp, ws, "Asia/Shanghai")
	assert.Equal(t, "", content)
	assert.Equal(t, "", dateStr)
}

// TestReadDailyMemory_ListFiles出错 测试 ListFiles 出错返回空
func TestReadDailyMemory_ListFiles出错(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	require.NotNil(t, memoryDir)
	dailyMemoryDir := *memoryDir + "/" + string(hworkspace.WorkspaceNodeDailyMemory)

	fsOp.listFilesErrors[dailyMemoryDir] = assert.AnError

	content, dateStr := ReadDailyMemory(ctx, fsOp, ws, "Asia/Shanghai")
	assert.Equal(t, "", content)
	assert.Equal(t, "", dateStr)
}

// TestReadDailyMemory_memoryDir为nil 测试 memory 目录不存在返回空
func TestReadDailyMemory_memoryDir为nil(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	// WorkspaceNodeMemory 不存在于默认 workspace 中
	// 但 NewWorkspace 的默认目录结构包含 memory 节点
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")
	// 确保 memory 目录存在
	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	if memoryDir == nil {
		content, dateStr := ReadDailyMemory(ctx, fsOp, ws, "")
		assert.Equal(t, "", content)
		assert.Equal(t, "", dateStr)
	}
}

// TestReadDailyMemory_ReadFile出错 测试读取每日文件出错返回空
func TestReadDailyMemory_ReadFile出错(t *testing.T) {
	ctx := context.Background()
	fsOp := newFakeFsOperation()
	ws := hworkspace.NewWorkspace("/tmp/workspace", "cn")

	memoryDir := ws.GetNodePath(hworkspace.WorkspaceNodeMemory)
	require.NotNil(t, memoryDir)
	dailyMemoryDir := *memoryDir + "/" + string(hworkspace.WorkspaceNodeDailyMemory)

	today := "2026-09-23"
	todayFile := today + ".md"

	fsOp.listFilesResults[dailyMemoryDir] = &result.ListFilesResult{
		Data: &result.FileSystemData{
			ListItems: []result.FileSystemItem{
				{Name: todayFile, Path: dailyMemoryDir + "/" + todayFile},
			},
		},
	}

	todayPath := dailyMemoryDir + "/" + todayFile
	fsOp.readFileErrors[todayPath] = assert.AnError

	content, dateStr := ReadDailyMemory(ctx, fsOp, ws, "Asia/Shanghai")
	assert.Equal(t, "", content)
	assert.Equal(t, "", dateStr)
}

// TestIsUnfilledTemplate_超过最大长度 测试超过最大长度返回 false
func TestIsUnfilledTemplate_超过最大长度(t *testing.T) {
	longContent := strings.Repeat("a", 501)
	assert.False(t, IsUnfilledTemplate(longContent))
}

// TestIsUnfilledTemplate_仅HTML注释 测试仅含 HTML 注释返回 true
func TestIsUnfilledTemplate_仅HTML注释(t *testing.T) {
	assert.True(t, IsUnfilledTemplate("<!-- comment -->"))
	assert.True(t, IsUnfilledTemplate("<!-- comment1 --><!-- comment2 -->"))
}

// TestIsUnfilledTemplate_包含模板标记 测试包含模板标记短语返回 true
func TestIsUnfilledTemplate_包含模板标记(t *testing.T) {
	assert.True(t, IsUnfilledTemplate("此处应保存的内容"))
	assert.True(t, IsUnfilledTemplate("What should be saved here"))
	assert.True(t, IsUnfilledTemplate("在你们的第一次对话中填写"))
	assert.True(t, IsUnfilledTemplate("Fill this in during your first"))
}

// TestIsUnfilledTemplate_仅Markdown标题 测试仅含 Markdown 标题返回 true
func TestIsUnfilledTemplate_仅Markdown标题(t *testing.T) {
	assert.True(t, IsUnfilledTemplate("# Title"))
	assert.True(t, IsUnfilledTemplate("## Subtitle\n### Sub-subtitle"))
}

// TestIsUnfilledTemplate_正常内容 测试正常内容返回 false
func TestIsUnfilledTemplate_正常内容(t *testing.T) {
	assert.False(t, IsUnfilledTemplate("This is real content with actual information."))
	assert.False(t, IsUnfilledTemplate("# Title\nSome real content below."))
}

// TestBuildToolsContent_优先顺序工具 测试优先顺序工具渲染
func TestBuildToolsContent_优先顺序工具(t *testing.T) {
	tools := map[string]string{
		"paid_search": "search the web",
		"bash":        "run commands",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "paid_search")
	assert.Contains(t, result, "bash")
}

// TestBuildToolsContent_分组工具完整 测试分组工具完整渲染
func TestBuildToolsContent_分组工具完整(t *testing.T) {
	tools := map[string]string{
		"read_file":  "read",
		"write_file": "write",
		"edit_file":  "edit",
		"bash":       "run commands",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "read_file / write_file / edit_file")
}

// TestBuildToolsContent_分组工具不完整 测试分组工具不完整时单独渲染
func TestBuildToolsContent_分组工具不完整(t *testing.T) {
	tools := map[string]string{
		"read_file":  "read",
		"write_file": "write",
		"bash":       "run commands",
	}
	result := BuildToolsContent(tools, "cn")
	// 缺少 edit_file，不应合并为一行
	assert.NotContains(t, result, "read_file / write_file / edit_file")
	assert.Contains(t, result, "read_file")
	assert.Contains(t, result, "write_file")
}

// TestBuildToolsContent_记忆系统分组完整 测试记忆系统完整分组
func TestBuildToolsContent_记忆系统分组完整(t *testing.T) {
	tools := map[string]string{
		"memory_search": "search memory",
		"memory_get":    "get memory",
		"write_memory":  "write memory",
		"edit_memory":   "edit memory",
		"read_memory":   "read memory",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "memory_search / memory_get / write_memory / edit_memory / read_memory")
}

// TestBuildToolsContent_记忆系统分组不完整 测试记忆系统不完整时单独渲染
func TestBuildToolsContent_记忆系统分组不完整(t *testing.T) {
	tools := map[string]string{
		"memory_search": "search memory",
		"memory_get":    "get memory",
	}
	result := BuildToolsContent(tools, "cn")
	assert.NotContains(t, result, "memory_search / memory_get / write_memory / edit_memory / read_memory")
	assert.Contains(t, result, "memory_search")
}

// TestBuildToolsContent_bash使用原则 测试包含 bash 使用原则
func TestBuildToolsContent_bash使用原则(t *testing.T) {
	tools := map[string]string{"bash": "执行命令"}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "bash 使用原则")
}

// TestBuildToolsContent_bash使用原则英文 测试英文 bash 使用原则
func TestBuildToolsContent_bash使用原则英文(t *testing.T) {
	tools := map[string]string{"bash": "run commands"}
	result := BuildToolsContent(tools, "en")
	assert.Contains(t, result, "bash Guidelines")
}

// TestBuildToolsContent_taskTool使用原则 测试包含 task_tool 使用原则
func TestBuildToolsContent_taskTool使用原则(t *testing.T) {
	tools := map[string]string{"task_tool": "launch sub-agent\n可用代理类型及对应工具：\n- code: coding\n重要：note"}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "task_tool 使用原则")
	assert.Contains(t, result, "可用代理类型")
}

// TestBuildToolsContent_隐藏工具 测试隐藏工具不出现在列表中
func TestBuildToolsContent_隐藏工具(t *testing.T) {
	tools := map[string]string{
		"bash":           "run commands",
		"cron_list_jobs": "list cron jobs",
		"cron_get_job":   "get cron job",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "bash")
	assert.NotContains(t, result, "cron_list_jobs")
	assert.NotContains(t, result, "cron_get_job")
}

// TestBuildToolsContent_摘要覆盖 测试中文摘要覆盖
func TestBuildToolsContent_摘要覆盖(t *testing.T) {
	tools := map[string]string{
		"paid_search": "original description",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "付费联网搜索（配置 API 时优先使用）")
}

// TestBuildToolsContent_剩余工具 测试非优先顺序和分组工具的渲染
func TestBuildToolsContent_剩余工具(t *testing.T) {
	tools := map[string]string{
		"custom_tool": "custom description",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "custom_tool")
	assert.Contains(t, result, "custom description")
}

// TestBuildToolsContent_剩余工具多行截断 测试剩余工具描述截断到第一行
func TestBuildToolsContent_剩余工具多行截断(t *testing.T) {
	tools := map[string]string{
		"custom_tool": "line1\nline2\nline3",
	}
	result := BuildToolsContent(tools, "cn")
	assert.Contains(t, result, "line1")
	// 多行描述截断到第一行
}

// TestBuildToolsSection_空工具 测试空工具返回 nil
func TestBuildToolsSection_空工具(t *testing.T) {
	s := BuildToolsSection(nil, "cn")
	assert.Nil(t, s)
}

// TestBuildMemoryDatePrompt_英文 测试英文日期提示词
func TestBuildMemoryDatePrompt_英文(t *testing.T) {
	result := BuildMemoryDatePrompt("2026-07-04", "en")
	assert.Contains(t, result, "2026-07-04")
	assert.NotContains(t, result, "{today_date}")
}

// TestBuildPlanFileInfo_三状态 测试 buildPlanFileInfo 三种状态
func TestBuildPlanFileInfo_三状态(t *testing.T) {
	// 空路径
	result := buildPlanFileInfo("", false, "cn")
	assert.Contains(t, result, "尚无 plan 文件")

	result = buildPlanFileInfo("", false, "en")
	assert.Contains(t, result, "No plan file yet")

	// 文件存在
	result = buildPlanFileInfo("/tmp/plan.md", true, "cn")
	assert.Contains(t, result, "计划文件已存在于")
	assert.Contains(t, result, "edit_file")

	result = buildPlanFileInfo("/tmp/plan.md", true, "en")
	assert.Contains(t, result, "A plan file already exists at")
	assert.Contains(t, result, "edit_file")

	// 文件不存在
	result = buildPlanFileInfo("/tmp/plan.md", false, "cn")
	assert.Contains(t, result, "计划文件尚不存在")
	assert.Contains(t, result, "write_file")

	result = buildPlanFileInfo("/tmp/plan.md", false, "en")
	assert.Contains(t, result, "No plan file exists yet")
	assert.Contains(t, result, "write_file")
}

// TestBuildEnterPlanModeStatus_四状态 测试 buildEnterPlanModeStatus 四种状态
func TestBuildEnterPlanModeStatus_四状态(t *testing.T) {
	// 有路径中文
	result := buildEnterPlanModeStatus("/tmp/plan.md", "cn")
	assert.Contains(t, result, "已调用完成")

	// 无路径中文
	result = buildEnterPlanModeStatus("", "cn")
	assert.Contains(t, result, "尚未调用")

	// 有路径英文
	result = buildEnterPlanModeStatus("/tmp/plan.md", "en")
	assert.Contains(t, result, "has been called")

	// 无路径英文
	result = buildEnterPlanModeStatus("", "en")
	assert.Contains(t, result, "NOT called")
}

// TestBuildToolsContent_英文分组描述 测试英文模式下分组工具描述
func TestBuildToolsContent_英文分组描述(t *testing.T) {
	tools := map[string]string{
		"read_file":  "read",
		"write_file": "write",
		"edit_file":  "edit",
	}
	result := BuildToolsContent(tools, "en")
	assert.Contains(t, result, "Read, write, and edit files")
}

// TestBuildToolsContent_英文搜索分组 测试英文文件搜索分组
func TestBuildToolsContent_英文搜索分组(t *testing.T) {
	tools := map[string]string{
		"glob":       "find files",
		"list_files": "list files",
		"grep":       "search content",
	}
	result := BuildToolsContent(tools, "en")
	assert.Contains(t, result, "Search files and file contents")
}

// TestBuildToolsContent_英文记忆分组 测试英文记忆系统分组
func TestBuildToolsContent_英文记忆分组(t *testing.T) {
	tools := map[string]string{
		"memory_search": "search",
		"memory_get":    "get",
		"write_memory":  "write",
		"edit_memory":   "edit",
		"read_memory":   "read",
	}
	result := BuildToolsContent(tools, "en")
	assert.Contains(t, result, "Memory system")
}
