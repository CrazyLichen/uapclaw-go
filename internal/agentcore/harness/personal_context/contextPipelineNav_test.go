package personal_context

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHashFile_分块(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.bin")
	data := make([]byte, 1024*1024+7)
	for i := range data {
		data[i] = byte(i % 251)
	}
	require.NoError(t, os.WriteFile(p, data, 0o644))
	hash, err := HashFile(p)
	require.NoError(t, err)
	require.Len(t, hash, 64)
	_, err = HashFile(filepath.Join(root, "missing"))
	require.ErrorContains(t, err, "could not be read")
}

func TestSnapshotManagedFiles_快照(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("A"), 0o644))
	snapshot, err := SnapshotManagedFiles(root)
	require.NoError(t, err)
	require.Contains(t, snapshot, "a.md")
	// 改内容 → ValidateUnchangedTree 报错
	require.NoError(t, ValidateUnchangedTree(root, snapshot, "inputs"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.md"), []byte("B"), 0o644))
	err = ValidateUnchangedTree(root, snapshot, "inputs")
	require.ErrorContains(t, err, "must not be modified")
}

func TestResetFilesystemSandbox_保留输入树(t *testing.T) {
	sandbox := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "inputs", "records"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(sandbox, "context"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "inputs", "records", "r.json"), []byte("{}"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "context", "old.md"), []byte("old"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(sandbox, "materialized-source"), []byte("m"), 0o644))
	require.NoError(t, ResetFilesystemSandbox(sandbox))
	require.True(t, pipelinePathIsFile(filepath.Join(sandbox, "inputs", "records", "r.json")))
	require.True(t, pipelinePathIsFile(filepath.Join(sandbox, "materialized-source")))
	require.False(t, pipelinePathIsDir(filepath.Join(sandbox, "context")))
}

func TestRecordContextRelocations_组合(t *testing.T) {
	relocations := map[string]string{"orig/a.md": "mid/a.md"}
	RecordContextRelocations(relocations, map[string]string{"mid/a.md": "final/a.md"})
	require.Equal(t, "final/a.md", relocations["orig/a.md"])
	// 新搬迁直接登记
	RecordContextRelocations(relocations, map[string]string{"x.md": "y.md"})
	require.Equal(t, "y.md", relocations["x.md"])
}

func TestHumanLabelIsURI_判定(t *testing.T) {
	require.True(t, HumanLabelIsURI("https://example.com"))
	require.True(t, HumanLabelIsURI("mailto:a@b.com"))
	require.False(t, HumanLabelIsURI("安装指南"))
	require.False(t, HumanLabelIsURI("title:subtitle")) // 无 // 且 scheme 不在集合
}

func TestPageLabelCandidate_拒绝分类(t *testing.T) {
	label, reason := pageLabelCandidate("安装指南")
	require.Equal(t, "accepted", reason)
	require.Equal(t, "安装指南", label)
	_, reason = pageLabelCandidate("https://example.com")
	require.Equal(t, "generic", reason)
	_, reason = pageLabelCandidate("src_0123456789abcdef0123456789abcdef")
	require.Equal(t, "generic", reason)
	_, reason = pageLabelCandidate("2024-01")
	require.Equal(t, "generic", reason)
	// 2024年01月：dateLabel 模式不匹配（月后无数字），CJK ≥2 → accepted（对齐 Python）
	_, reason = pageLabelCandidate("2024年01月")
	require.Equal(t, "accepted", reason)
	// 纯空白 → markdownLabel 回落"来源"（Python 语义）→ 可读标签被接受
	label, reason = pageLabelCandidate("   ")
	require.Equal(t, "accepted", reason)
	require.Equal(t, "来源", label)
}

func TestSourceTitleLabel_拒绝投影(t *testing.T) {
	require.Equal(t, "真标题", sourceTitleLabel(map[string]string{"title": "真标题", "locator": "https://e.com/x"}))
	require.Equal(t, "", sourceTitleLabel(map[string]string{"title": "https://e.com/x", "locator": "https://e.com/x"}))
	require.Equal(t, "", sourceTitleLabel(map[string]string{"title": "/abs/path.md", "locator": "https://e.com/x"}))
}

func TestSourceFilenameLabel_末段(t *testing.T) {
	require.Equal(t, "报告", sourceFilenameLabel(map[string]string{"locator": "https://e.com/path/报告.pdf"}))
	require.Equal(t, "readme", sourceFilenameLabel(map[string]string{"locator": "./docs/readme.md"}))
	require.Equal(t, "", sourceFilenameLabel(map[string]string{"locator": ""}))
}

func TestTimePartitionDirectory_月份与分组(t *testing.T) {
	root := t.TempDir()
	runTime := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	base, err := timePartitionDirectory(root, runTime, 0, false)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "2026年11月"), base)
	// max=1，放 1 页 → 分组目录
	require.NoError(t, os.MkdirAll(base, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "p.md"), []byte("x"), 0o644))
	grouped, err := timePartitionDirectory(root, runTime, 1, true)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "2026年11月", "第2组"), grouped)
}

func TestProviderFallbackDirectory_展示名(t *testing.T) {
	root := t.TempDir()
	runTime := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	dir, err := providerFallbackDirectory(root, "github", runTime, 0, false)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "待整理", "GitHub", "2026年11月"), dir)
	dir, err = providerFallbackDirectory(root, "local_files", runTime, 0, false)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "待整理", "本地文件", "2026年11月"), dir)
}

func TestContentCapacityNavigationDirectory_溢出拆层(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "主题")
	require.NoError(t, os.MkdirAll(full, 0o755))
	// 空目录未满 → 直接返回
	navigation, err := contentCapacityNavigationDirectory(root, full, "主题", "seed123456789012", 20, true, 20, true)
	require.NoError(t, err)
	require.Equal(t, full, navigation)
	// 满容量 → 建导航子目录
	require.NoError(t, os.WriteFile(filepath.Join(full, "p.md"), []byte("x"), 0o644))
	navigation, err = contentCapacityNavigationDirectory(root, full, "主题", "seed123456789012", 1, true, 20, true)
	require.NoError(t, err)
	require.NotEqual(t, full, navigation)
	require.Contains(t, navigation, "导航")
}
