package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeFsOperation 用于测试的 FsOperation 模拟实现
type fakeFsOperation struct {
	// readFileResult ReadFile 返回值
	readFileResult *result.ReadFileResult
	// readFileErr ReadFile 返回错误
	readFileErr error
	// writeFileResult WriteFile 返回值
	writeFileResult *result.WriteFileResult
	// writeFileErr WriteFile 返回错误
	writeFileErr error
}

func (f *fakeFsOperation) ReadFile(_ context.Context, _ string, _ ...sysop.FsOption) (*result.ReadFileResult, error) {
	return f.readFileResult, f.readFileErr
}
func (f *fakeFsOperation) ReadFileStream(_ context.Context, _ string, _ ...sysop.FsOption) (<-chan result.ReadFileStreamResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) WriteFile(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.WriteFileResult, error) {
	return f.writeFileResult, f.writeFileErr
}
func (f *fakeFsOperation) UploadFile(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.UploadFileResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) UploadFileStream(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (<-chan result.UploadFileStreamResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) DownloadFile(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.DownloadFileResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) DownloadFileStream(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (<-chan result.DownloadFileStreamResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ListFiles(_ context.Context, _ string, _ ...sysop.FsOption) (*result.ListFilesResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ListDirectories(_ context.Context, _ string, _ ...sysop.FsOption) (*result.ListDirsResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) SearchFiles(_ context.Context, _ string, _ string, _ ...sysop.FsOption) (*result.SearchFilesResult, error) {
	return nil, nil
}
func (f *fakeFsOperation) ListTools() []*tool.ToolCard { return nil }

// fakeSysOperation 用于测试的 SysOperation 模拟实现
type fakeSysOperation struct {
	// fsOp 文件系统操作
	fsOp sysop.FsOperation
}

func (f *fakeSysOperation) Card() *sysop.SysOperationCard { return nil }
func (f *fakeSysOperation) Fs() sysop.FsOperation         { return f.fsOp }
func (f *fakeSysOperation) Shell() sysop.ShellOperation   { return nil }
func (f *fakeSysOperation) Code() sysop.CodeOperation     { return nil }
func (f *fakeSysOperation) IsolationKeyTemplate() string  { return "" }

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSharedMemoryManager_EnsureDir 验证目录创建
func TestSharedMemoryManager_EnsureDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "team-memory")); os.IsNotExist(err) {
		t.Errorf("Directory not created")
	}
}

// TestSharedMemoryManager_ReadTeamSummary_空文件 验证不存在时返回空字符串
func TestSharedMemoryManager_ReadTeamSummary_空文件(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()
	result := mgr.ReadTeamSummary(context.Background())
	if result != "" {
		t.Errorf("Expected empty string for nonexistent file, got %q", result)
	}
}

// TestSharedMemoryManager_WriteAndRead 验证写入后读取
func TestSharedMemoryManager_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	content := "# 团队共享记忆\n\n### [decision] 选择了方案 A"
	if err := mgr.WriteTeamSummary(context.Background(), content); err != nil {
		t.Fatalf("WriteTeamSummary failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}
}

// TestSharedMemoryManager_AppendEntry 验证追加条目
func TestSharedMemoryManager_AppendEntry(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	if err := mgr.WriteTeamSummary(context.Background(), "初始内容"); err != nil {
		t.Fatalf("WriteTeamSummary failed: %v", err)
	}
	if err := mgr.AppendEntry(context.Background(), "追加条目"); err != nil {
		t.Fatalf("AppendEntry failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if !strings.Contains(result, "初始内容") || !strings.Contains(result, "追加条目") {
		t.Errorf("Expected both contents, got %q", result)
	}
	if !strings.Contains(result, "---") {
		t.Errorf("Expected separator ---, got %q", result)
	}
}

// TestSharedMemoryManager_AppendEntry_空初始 验证追加到空文件
func TestSharedMemoryManager_AppendEntry_空初始(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	if err := mgr.AppendEntry(context.Background(), "首条内容"); err != nil {
		t.Fatalf("AppendEntry failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if result != "首条内容" {
		t.Errorf("Expected %q, got %q", "首条内容", result)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestSharedMemoryManager_ReadTeamSummary_SysOperation成功 验证通过 sysOperation 读取团队记忆
func TestSharedMemoryManager_ReadTeamSummary_SysOperation成功(t *testing.T) {
	dir := t.TempDir()
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			Data: &result.ReadFileData{
				Content: "### [decision] 选择方案 A\n原因说明",
				Path:    filepath.Join(dir, "team-memory", "TEAM_MEMORY.md"),
			},
		},
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	got := mgr.ReadTeamSummary(context.Background())
	if got != "### [decision] 选择方案 A\n原因说明" {
		t.Errorf("期望通过 sysOperation 读取内容，实际 %q", got)
	}
}

// TestSharedMemoryManager_ReadTeamSummary_SysOperation错误 验证 sysOperation 读取失败时返回空字符串
func TestSharedMemoryManager_ReadTeamSummary_SysOperation错误(t *testing.T) {
	dir := t.TempDir()
	fsOp := &fakeFsOperation{
		readFileErr: os.ErrNotExist,
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	got := mgr.ReadTeamSummary(context.Background())
	if got != "" {
		t.Errorf("期望读取失败返回空字符串，实际 %q", got)
	}
}

// TestSharedMemoryManager_ReadTeamSummary_SysOperation内容为空 验证 sysOperation 返回空内容时回退到本地
func TestSharedMemoryManager_ReadTeamSummary_SysOperation内容为空(t *testing.T) {
	dir := t.TempDir()
	// sysOperation 返回空内容
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			Data: &result.ReadFileData{Content: ""},
		},
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	got := mgr.ReadTeamSummary(context.Background())
	if got != "" {
		t.Errorf("期望空内容返回空字符串，实际 %q", got)
	}
}

// TestSharedMemoryManager_ReadTeamSummary_SysOperationData为nil 验证 Data 为 nil 时返回空字符串
func TestSharedMemoryManager_ReadTeamSummary_SysOperationData为nil(t *testing.T) {
	dir := t.TempDir()
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{Data: nil},
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	got := mgr.ReadTeamSummary(context.Background())
	if got != "" {
		t.Errorf("期望 Data nil 时返回空字符串，实际 %q", got)
	}
}

// TestSharedMemoryManager_ReadTeamSummary_SysOperation超长截断 验证超过最大行数时截断
func TestSharedMemoryManager_ReadTeamSummary_SysOperation超长截断(t *testing.T) {
	dir := t.TempDir()
	// 生成超过 200 行的内容
	var lines []string
	for i := 0; i < 250; i++ {
		lines = append(lines, fmt.Sprintf("行 %d", i))
	}
	content := strings.Join(lines, "\n")
	fsOp := &fakeFsOperation{
		readFileResult: &result.ReadFileResult{
			Data: &result.ReadFileData{Content: content},
		},
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	got := mgr.ReadTeamSummary(context.Background())
	gotLines := strings.Split(got, "\n")
	if len(gotLines) > teamMemoryMaxReadLines {
		t.Errorf("期望截断到 %d 行，实际 %d 行", teamMemoryMaxReadLines, len(gotLines))
	}
}

// TestSharedMemoryManager_WriteTeamSummary_SysOperation成功 验证通过 sysOperation 写入团队记忆
func TestSharedMemoryManager_WriteTeamSummary_SysOperation成功(t *testing.T) {
	dir := t.TempDir()
	fsOp := &fakeFsOperation{
		writeFileResult: &result.WriteFileResult{},
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	if err := mgr.WriteTeamSummary(context.Background(), "测试内容"); err != nil {
		t.Errorf("期望写入成功，实际失败: %v", err)
	}
}

// TestSharedMemoryManager_WriteTeamSummary_SysOperation失败回退 验证 sysOperation 写入失败时回退到本地写入
func TestSharedMemoryManager_WriteTeamSummary_SysOperation失败回退(t *testing.T) {
	dir := t.TempDir()
	fsOp := &fakeFsOperation{
		writeFileErr: os.ErrPermission,
	}
	sysOp := &fakeSysOperation{fsOp: fsOp}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	// sysOperation 写入失败后回退到本地原子写入
	if err := mgr.WriteTeamSummary(context.Background(), "回退内容"); err != nil {
		t.Errorf("期望回退写入成功，实际失败: %v", err)
	}
}

// TestSharedMemoryManager_WriteTeamSummary_EnsureDir失败 验证 EnsureDir 失败时返回错误
func TestSharedMemoryManager_WriteTeamSummary_EnsureDir失败(t *testing.T) {
	// 使用一个不可能创建的路径
	mgr := NewSharedMemoryManager("/proc/impossible/path/team-memory", nil)
	err := mgr.WriteTeamSummary(context.Background(), "内容")
	if err == nil {
		t.Errorf("期望 EnsureDir 失败返回错误，实际成功")
	}
}

// TestSharedMemoryManager_ReadTeamSummary_SysOpFs为nil 验证 sysOperation.Fs() 返回 nil 时回退到本地
func TestSharedMemoryManager_ReadTeamSummary_SysOpFs为nil(t *testing.T) {
	dir := t.TempDir()
	sysOp := &fakeSysOperation{fsOp: nil}
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), sysOp)

	got := mgr.ReadTeamSummary(context.Background())
	if got != "" {
		t.Errorf("期望 Fs nil 回退到本地返回空字符串，实际 %q", got)
	}
}
