package local

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newTestFsOperation 构造无沙箱限制的本地文件操作实例。
func newTestFsOperation() *LocalFsOperation {
	return NewLocalFsOperation(nil).(*LocalFsOperation)
}

// writeTempFile 在临时目录写入内容并返回路径。
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// ──────────────────────────── 上传 ────────────────────────────

// TestUploadFile_成功拷贝 本地模式下上传等价于文件拷贝。
func TestUploadFile_成功拷贝(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "hello upload")
	dst := filepath.Join(dir, "dst.txt")

	res, err := fsOp.UploadFile(context.Background(), src, dst)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, 0, res.BaseResult.Code)
	assert.Equal(t, len("hello upload"), res.Data.Size)

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "hello upload", string(got))
}

// TestUploadFile_源文件不存在 源文件缺失时返回错误。
func TestUploadFile_源文件不存在(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()

	_, err := fsOp.UploadFile(context.Background(), filepath.Join(dir, "missing.txt"), filepath.Join(dir, "dst.txt"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "源文件未找到")
}

// TestUploadFile_目标父目录缺失 未开启建父目录时写目标失败。
func TestUploadFile_目标父目录缺失(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "x")
	dst := filepath.Join(dir, "no-such-dir", "dst.txt")

	_, err := fsOp.UploadFile(context.Background(), src, dst, sysop.WithFsCreateParentDirs(false))
	require.Error(t, err)
}

// TestUploadFileStream_多分块拷贝 分块大小小于文件时逐块写出并标记末块。
func TestUploadFileStream_多分块拷贝(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "0123456789")
	dst := filepath.Join(dir, "dst.txt")

	ch, err := fsOp.UploadFileStream(context.Background(), src, dst, sysop.WithFsChunkSize(4))
	require.NoError(t, err)

	var chunks []*result.UploadFileChunkData
	for item := range ch {
		require.Equal(t, 0, item.BaseResult.Code, item.BaseResult.Message)
		chunks = append(chunks, item.Data)
	}
	require.Len(t, chunks, 3)
	assert.Equal(t, 0, chunks[0].ChunkIndex)
	assert.False(t, chunks[0].IsLastChunk)
	assert.Equal(t, 2, chunks[2].ChunkIndex)
	assert.True(t, chunks[2].IsLastChunk)

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "0123456789", string(got), "分块拷贝不得破坏内容")
}

// TestUploadFileStream_源文件不存在 源缺失时通道回错误结果。
func TestUploadFileStream_源文件不存在(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()

	ch, err := fsOp.UploadFileStream(context.Background(), filepath.Join(dir, "missing.txt"), filepath.Join(dir, "dst.txt"))
	require.NoError(t, err)

	item, ok := <-ch
	require.True(t, ok)
	assert.NotEqual(t, 0, item.BaseResult.Code)
	assert.Contains(t, item.BaseResult.Message, "源文件未找到")
}

// TestUploadFileStream_目标已存在且不覆盖 未开启覆盖时拒绝写入。
func TestUploadFileStream_目标已存在且不覆盖(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "new")
	dst := writeTempFile(t, dir, "dst.txt", "old")

	ch, err := fsOp.UploadFileStream(context.Background(), src, dst)
	require.NoError(t, err)

	item, ok := <-ch
	require.True(t, ok)
	assert.Contains(t, item.BaseResult.Message, "目标文件已存在")
}

// TestUploadFileStream_覆盖并保留权限 覆盖写入且复制源文件权限。
func TestUploadFileStream_覆盖并保留权限(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "fresh")
	require.NoError(t, os.Chmod(src, 0o600))
	dst := writeTempFile(t, dir, "dst.txt", "old")

	ch, err := fsOp.UploadFileStream(context.Background(), src, dst,
		sysop.WithFsOverwrite(true), sysop.WithFsPreservePerms(true))
	require.NoError(t, err)
	for item := range ch {
		require.Equal(t, 0, item.BaseResult.Code, item.BaseResult.Message)
	}

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "fresh", string(got))

	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(dst)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

// ──────────────────────────── 下载 ────────────────────────────

// TestDownloadFile_成功拷贝 本地模式下下载等价于文件拷贝。
func TestDownloadFile_成功拷贝(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "hello download")
	dst := filepath.Join(dir, "dst.txt")

	res, err := fsOp.DownloadFile(context.Background(), src, dst)
	require.NoError(t, err)
	assert.Equal(t, 0, res.BaseResult.Code)
	assert.Equal(t, len("hello download"), res.Data.Size)

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "hello download", string(got))
}

// TestDownloadFile_源文件不存在 源缺失时返回错误。
func TestDownloadFile_源文件不存在(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()

	_, err := fsOp.DownloadFile(context.Background(), filepath.Join(dir, "missing.txt"), filepath.Join(dir, "dst.txt"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "源文件未找到")
}

// TestDownloadFile_目标不可写 目标是目录时写失败。
func TestDownloadFile_目标不可写(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "x")
	target := filepath.Join(dir, "sub")
	require.NoError(t, os.Mkdir(target, 0o755))

	_, err := fsOp.DownloadFile(context.Background(), src, target)
	require.Error(t, err)
}

// TestDownloadFileStream_多分块拷贝 分块大小小于文件时逐块写出并标记末块。
func TestDownloadFileStream_多分块拷贝(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "0123456789")
	dst := filepath.Join(dir, "dst.txt")

	ch, err := fsOp.DownloadFileStream(context.Background(), src, dst, sysop.WithFsChunkSize(4))
	require.NoError(t, err)

	var chunks []*result.DownloadFileChunkData
	for item := range ch {
		require.Equal(t, 0, item.BaseResult.Code, item.BaseResult.Message)
		chunks = append(chunks, item.Data)
	}
	require.Len(t, chunks, 3)
	assert.True(t, chunks[2].IsLastChunk)

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "0123456789", string(got), "分块拷贝不得破坏内容")
}

// TestDownloadFileStream_源文件不存在 源缺失时通道回错误结果。
func TestDownloadFileStream_源文件不存在(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()

	ch, err := fsOp.DownloadFileStream(context.Background(), filepath.Join(dir, "missing.txt"), filepath.Join(dir, "dst.txt"))
	require.NoError(t, err)

	item, ok := <-ch
	require.True(t, ok)
	assert.Contains(t, item.BaseResult.Message, "源文件未找到")
}

// TestDownloadFileStream_目标已存在且不覆盖 未开启覆盖时拒绝写入。
func TestDownloadFileStream_目标已存在且不覆盖(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "new")
	dst := writeTempFile(t, dir, "dst.txt", "old")

	ch, err := fsOp.DownloadFileStream(context.Background(), src, dst)
	require.NoError(t, err)

	item, ok := <-ch
	require.True(t, ok)
	assert.Contains(t, item.BaseResult.Message, "目标路径已存在")
}

// TestDownloadFileStream_创建父目录 目标父目录缺失时按选项创建。
func TestDownloadFileStream_创建父目录(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "data")
	dst := filepath.Join(dir, "nested", "deep", "dst.txt")

	ch, err := fsOp.DownloadFileStream(context.Background(), src, dst, sysop.WithFsCreateParentDirs(true))
	require.NoError(t, err)
	for item := range ch {
		require.Equal(t, 0, item.BaseResult.Code, item.BaseResult.Message)
	}

	got, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, "data", string(got))
}

// ──────────────────────────── 读取/写入 ────────────────────────────

// TestReadFile_四种读取模式 head/tail/line_range/bytes 各自生效。
func TestReadFile_四种读取模式(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	path := writeTempFile(t, dir, "lines.txt", "l1\nl2\nl3\nl4\n")

	ctx := context.Background()

	head, err := fsOp.ReadFile(ctx, path, sysop.WithFsHead(2))
	require.NoError(t, err)
	assert.Equal(t, "l1\nl2", head.Data.Content)

	tail, err := fsOp.ReadFile(ctx, path, sysop.WithFsTail(2))
	require.NoError(t, err)
	assert.Equal(t, "l3\nl4", tail.Data.Content)

	ranged, err := fsOp.ReadFile(ctx, path, sysop.WithFsLineRange(2, 3))
	require.NoError(t, err)
	assert.Equal(t, "l2\nl3", ranged.Data.Content)

	bytesRes, err := fsOp.ReadFile(ctx, path, sysop.WithFsMode("bytes"))
	require.NoError(t, err)
	assert.Equal(t, "l1\nl2\nl3\nl4\n", bytesRes.Data.Content)
}

// TestReadFile_互斥参数校验 非法组合直接返回错误。
func TestReadFile_互斥参数校验(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.txt", "x")

	cases := []struct {
		name string
		opts []sysop.FsOption
	}{
		{"head与tail互斥", []sysop.FsOption{sysop.WithFsHead(1), sysop.WithFsTail(1)}},
		{"tail与line_range互斥", []sysop.FsOption{sysop.WithFsTail(1), sysop.WithFsLineRange(1, 2)}},
		{"head与line_range互斥", []sysop.FsOption{sysop.WithFsHead(1), sysop.WithFsLineRange(1, 2)}},
		{"bytes不支持范围参数", []sysop.FsOption{sysop.WithFsMode("bytes"), sysop.WithFsHead(1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fsOp.ReadFile(context.Background(), path, tc.opts...)
			require.Error(t, err)
		})
	}
}

// TestReadFile_文件不存在与目录 两类目标都回错误结果而非 error。
func TestReadFile_文件不存在与目录(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()

	missing, err := fsOp.ReadFile(context.Background(), filepath.Join(dir, "missing.txt"))
	require.NoError(t, err)
	assert.NotEqual(t, 0, missing.BaseResult.Code)
	assert.Contains(t, missing.BaseResult.Message, "文件未找到")

	asDir, err := fsOp.ReadFile(context.Background(), dir)
	require.NoError(t, err)
	assert.NotEqual(t, 0, asDir.BaseResult.Code)
	assert.Contains(t, asDir.BaseResult.Message, "路径是目录")
}

// TestWriteFile_创建追加与换行 覆盖建目录、追加、前后换行与 bytes 模式。
func TestWriteFile_创建追加与换行(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	ctx := context.Background()

	nested := filepath.Join(dir, "a", "b", "f.txt")
	res, err := fsOp.WriteFile(ctx, nested, "hello", sysop.WithFsCreateParentDirs(true))
	require.NoError(t, err)
	assert.Equal(t, 0, res.BaseResult.Code)

	appendRes, err := fsOp.WriteFile(ctx, nested, " world", sysop.WithFsAppend(true))
	require.NoError(t, err)
	assert.Equal(t, 0, appendRes.BaseResult.Code)

	got, readErr := os.ReadFile(nested)
	require.NoError(t, readErr)
	assert.Equal(t, "hello world", string(got))

	withNewlines := filepath.Join(dir, "n.txt")
	_, err = fsOp.WriteFile(ctx, withNewlines, "x",
		sysop.WithFsPrependNewline(true), sysop.WithFsAppendNewline(true))
	require.NoError(t, err)
	got, readErr = os.ReadFile(withNewlines)
	require.NoError(t, readErr)
	assert.Equal(t, "\nx\n", string(got))

	rawPath := filepath.Join(dir, "raw.bin")
	_, err = fsOp.WriteFile(ctx, rawPath, "\x00\x01", sysop.WithFsMode("bytes"))
	require.NoError(t, err)
	got, readErr = os.ReadFile(rawPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte{0x00, 0x01}, got)
}

// TestWriteFile_非UTF8编码 指定 GBK 时按编码落盘。
func TestWriteFile_非UTF8编码(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	path := filepath.Join(dir, "gbk.txt")

	res, err := fsOp.WriteFile(context.Background(), path, "你好", sysop.WithFsEncoding("gbk"))
	require.NoError(t, err)
	assert.Equal(t, 0, res.BaseResult.Code)

	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, []byte{0xC4, 0xE3, 0xBA, 0xC3}, got)
}

// TestWriteFile_不存在且禁止创建 关闭 CreateIfNotExist 时拒绝写入。
func TestWriteFile_不存在且禁止创建(t *testing.T) {
	fsOp := newTestFsOperation()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.txt")

	res, err := fsOp.WriteFile(context.Background(), path, "x", sysop.WithFsCreateIfNotExist(false))
	require.NoError(t, err)
	assert.NotEqual(t, 0, res.BaseResult.Code)
	assert.Contains(t, res.BaseResult.Message, "文件不存在")
}

// ──────────────────────────── 目录遍历 ────────────────────────────

// TestListDirectories_递归与深度限制 递归返回子目录并按 max_depth 截断。
func TestListDirectories_递归与深度限制(t *testing.T) {
	fsOp := newTestFsOperation()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))

	shallow, err := fsOp.ListDirectories(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, 1, shallow.Data.TotalCount)

	deep, err := fsOp.ListDirectories(context.Background(), root, sysop.WithFsRecursive(true))
	require.NoError(t, err)
	assert.Equal(t, 2, deep.Data.TotalCount)

	limited, err := fsOp.ListDirectories(context.Background(), root,
		sysop.WithFsRecursive(true), sysop.WithFsMaxDepth(1))
	require.NoError(t, err)
	assert.Equal(t, 1, limited.Data.TotalCount)
}

// TestListFiles_类型过滤 只返回指定扩展名的文件。
func TestListFiles_类型过滤(t *testing.T) {
	fsOp := newTestFsOperation()
	root := t.TempDir()
	writeTempFile(t, root, "a.go", "x")
	writeTempFile(t, root, "b.txt", "y")
	require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), 0o755))

	all, err := fsOp.ListFiles(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, 2, all.Data.TotalCount)

	filtered, err := fsOp.ListFiles(context.Background(), root, sysop.WithFsFileTypes([]string{".go"}))
	require.NoError(t, err)
	require.Equal(t, 1, filtered.Data.TotalCount)
	assert.Equal(t, "a.go", filtered.Data.ListItems[0].Name)
}

// ──────────────────────────── 权限与编码助手 ────────────────────────────

// TestGetEncoder_已知与未知编码 已注册编码返回编码器，未知返回 nil。
func TestGetEncoder_已知与未知编码(t *testing.T) {
	assert.NotNil(t, getEncoder("gbk"))
	assert.Nil(t, getEncoder("not-a-real-encoding"))
}

// TestCopyPermissions_复制源权限 目标权限跟随源文件。
func TestCopyPermissions_复制源权限(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无 POSIX 权限位")
	}
	dir := t.TempDir()
	src := writeTempFile(t, dir, "src.txt", "x")
	dst := writeTempFile(t, dir, "dst.txt", "y")
	require.NoError(t, os.Chmod(src, 0o640))

	copyPermissions(src, dst)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

// TestCopyPermissions_源不存在静默返回 源缺失不 panic 也不改目标。
func TestCopyPermissions_源不存在静默返回(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 无 POSIX 权限位")
	}
	dir := t.TempDir()
	dst := writeTempFile(t, dir, "dst.txt", "y")
	require.NoError(t, os.Chmod(dst, 0o644))

	copyPermissions(filepath.Join(dir, "missing.txt"), dst)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

// TestParsePermissions_合法与非法 三位八进制解析，非法回退 0644。
func TestParsePermissions_合法与非法(t *testing.T) {
	assert.Equal(t, os.FileMode(0o600), parsePermissions("600"))
	assert.Equal(t, os.FileMode(0o755), parsePermissions("755"))
	assert.Equal(t, os.FileMode(0o644), parsePermissions("bad"))
	assert.Equal(t, os.FileMode(0o644), parsePermissions(""))
}

// TestReadTail_行数与编码 tail 读取保留最后 N 行。
func TestReadTail_行数与编码(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "f.txt", "a\nb\nc\n")

	lines, err := readTail(path, 2, "utf-8")
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "c"}, lines)

	_, err = readTail(filepath.Join(dir, "missing.txt"), 2, "utf-8")
	require.Error(t, err)
}
