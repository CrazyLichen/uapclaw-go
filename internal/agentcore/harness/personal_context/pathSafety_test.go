package personal_context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateServiceID_合法与非法(t *testing.T) {
	text, err := ValidateServiceID("  my-service ")
	require.NoError(t, err)
	require.Equal(t, "my-service", text)

	_, err = ValidateServiceID("   ")
	require.EqualError(t, err, "service_id must be a non-empty string")

	long := make([]rune, 501)
	for i := range long {
		long[i] = 'a'
	}
	_, err = ValidateServiceID(string(long))
	require.EqualError(t, err, "service_id must be at most 500 characters")

	_, err = ValidateServiceID(string(make([]rune, 500)))
	require.NoError(t, err)
}

func TestServiceStorageSegment_传统段与哈希段(t *testing.T) {
	seg, err := ServiceStorageSegment("my-service")
	require.NoError(t, err)
	require.Equal(t, "my-service", seg)

	// 含大写/非法字符（但仍是合法 service_id）的名字走哈希段
	hashed, err := ServiceStorageSegment("My Service")
	require.NoError(t, err)
	require.Regexp(t, `^\.service-[0-9a-f]{64}$`, hashed)

	// 同输入稳定
	stable, err := ServiceStorageSegment("My Service")
	require.NoError(t, err)
	require.Equal(t, hashed, stable)

	// Windows 保留名走哈希段
	conSeg, err := ServiceStorageSegment("con")
	require.NoError(t, err)
	require.Regexp(t, `^\.service-[0-9a-f]{64}$`, conSeg)

	// 非法 service_id 返回 error（由调用者判断，Python raise ValueError 对应）
	_, err = ServiceStorageSegment("   ")
	require.EqualError(t, err, "service_id must be a non-empty string")
}

func TestServiceStorageSegmentIsSafe_两形态识别(t *testing.T) {
	require.True(t, ServiceStorageSegmentIsSafe("abc-123"))
	hashed, err := ServiceStorageSegment("My Service")
	require.NoError(t, err)
	require.True(t, ServiceStorageSegmentIsSafe(hashed))
	require.False(t, ServiceStorageSegmentIsSafe("../escape"))
	require.False(t, ServiceStorageSegmentIsSafe(".service-zzz"))
}

func TestPortableContextSegmentIsSafe_边界(t *testing.T) {
	require.True(t, PortableContextSegmentIsSafe("安装指南"))
	require.False(t, PortableContextSegmentIsSafe(""))
	require.False(t, PortableContextSegmentIsSafe("."))
	require.False(t, PortableContextSegmentIsSafe(".."))
	// 非法字符
	require.False(t, PortableContextSegmentIsSafe("a<b"))
	// 尾部点/空格
	require.False(t, PortableContextSegmentIsSafe("name."))
	require.False(t, PortableContextSegmentIsSafe("name "))
	// 超长字符数
	long := make([]rune, MaxSegmentChars+1)
	for i := range long {
		long[i] = 'a'
	}
	require.False(t, PortableContextSegmentIsSafe(string(long)))
	// Windows 保留名首段
	require.False(t, PortableContextSegmentIsSafe("aux.md"))
}

func TestSemanticContextSegmentIsSafe_产品上限(t *testing.T) {
	require.True(t, SemanticContextSegmentIsSafe("安装指南.md", true))
	require.False(t, SemanticContextSegmentIsSafe("描述页.md", true) && false || SemanticContextSegmentIsSafe("description.md", true))
	require.False(t, SemanticContextSegmentIsSafe("note.txt", true), "markdown 文件必须 .md 结尾")
	long := make([]rune, SemanticNameMaxChars+1)
	for i := range long {
		long[i] = 'a'
	}
	require.False(t, SemanticContextSegmentIsSafe(string(long)+".md", true))
	require.True(t, SemanticContextSegmentIsSafe("简单目录名", false))
}

func TestAssertExistingChainIsPlain_symlink注入(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, AssertExistingChainIsPlain(filepath.Join(sub, "page.md"), root))

	// 中途插入 symlink → 拒绝
	require.NoError(t, os.Symlink(root, filepath.Join(sub, "loop")))
	require.ErrorContains(t, AssertExistingChainIsPlain(filepath.Join(sub, "loop", "page.md"), root),
		"traverses a link")

	// 逃逸根外 → 拒绝
	require.ErrorContains(t, AssertExistingChainIsPlain(filepath.Join(os.TempDir(), "outside.md"), root),
		"escaped its root")
}

func TestResolveContextRelativePath_规范解析(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "a.md"), []byte("# A"), 0o644))

	resolved, err := ResolveContextRelativePath(root, "docs/a.md", true)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "docs", "a.md"), resolved)

	_, err = ResolveContextRelativePath(root, "docs/missing.md", true)
	require.EqualError(t, err, "Context path is unavailable")

	_, err = ResolveContextRelativePath(root, `docs\a.md`, false)
	require.EqualError(t, err, "Context path must be a POSIX relative path")

	_, err = ResolveContextRelativePath(root, "../escape.md", false)
	require.EqualError(t, err, "Context path is unsafe")

	_, err = ResolveContextRelativePath(root, "docs//a.md", false)
	require.EqualError(t, err, "Context path is unsafe")

	_, err = ResolveContextRelativePath(root, "bad<name.md", false)
	require.EqualError(t, err, "Context path contains a non-portable segment")
}

func TestIsReparsePoint_非Windows恒假(t *testing.T) {
	require.False(t, IsReparsePoint(t.TempDir()))
}
