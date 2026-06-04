package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCopyDiffResultMerge 测试 CopyDiffResult.Merge
func TestCopyDiffResultMerge(t *testing.T) {
	r1 := CopyDiffResult{
		AddedDirs:        []string{"dir1", "dir2"},
		AddedFiles:       []string{"file1"},
		OverwrittenFiles: []string{"old1"},
	}
	r2 := CopyDiffResult{
		AddedDirs:        []string{"dir3"},
		AddedFiles:       []string{"file2", "file3"},
		OverwrittenFiles: []string{"old2"},
	}

	r1.Merge(r2)

	if len(r1.AddedDirs) != 3 {
		t.Errorf("AddedDirs 长度期望 3，实际 %d", len(r1.AddedDirs))
	}
	if len(r1.AddedFiles) != 3 {
		t.Errorf("AddedFiles 长度期望 3，实际 %d", len(r1.AddedFiles))
	}
	if len(r1.OverwrittenFiles) != 2 {
		t.Errorf("OverwrittenFiles 长度期望 2，实际 %d", len(r1.OverwrittenFiles))
	}
}

// TestCopyDiffResultPrintSummary_覆盖模式不打印 测试 overwrite=true 时 PrintSummary 不输出
func TestCopyDiffResultPrintSummary_覆盖模式不打印(t *testing.T) {
	r := CopyDiffResult{
		AddedFiles: []string{"file1"},
	}
	// 覆盖模式不 panic 即可
	r.PrintSummary(true)
}

// TestCopyDiffResultPrintSummary_无变更 测试无变更时的输出
func TestCopyDiffResultPrintSummary_无变更(t *testing.T) {
	r := CopyDiffResult{}
	// 不 panic 即可
	r.PrintSummary(false)
}

// TestCopyFileWithDiff_新文件 测试复制新文件
func TestCopyFileWithDiff_新文件(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "dst", "out.txt")
	content := "hello world"

	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}

	var diff CopyDiffResult
	if err := copyFileWithDiff(src, dst, &diff); err != nil {
		t.Fatalf("copyFileWithDiff 失败: %v", err)
	}

	// 验证文件内容
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != content {
		t.Errorf("文件内容期望 %q，实际 %q", content, string(data))
	}

	// 验证差异记录
	if len(diff.AddedFiles) != 1 {
		t.Errorf("AddedFiles 长度期望 1，实际 %d", len(diff.AddedFiles))
	}
	if len(diff.OverwrittenFiles) != 0 {
		t.Errorf("OverwrittenFiles 长度期望 0，实际 %d", len(diff.OverwrittenFiles))
	}
}

// TestCopyFileWithDiff_覆盖文件 测试覆盖已有文件
func TestCopyFileWithDiff_覆盖文件(t *testing.T) {
	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src.txt")
	dst := filepath.Join(tmpDir, "out.txt")

	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("写入目标文件失败: %v", err)
	}

	var diff CopyDiffResult
	if err := copyFileWithDiff(src, dst, &diff); err != nil {
		t.Fatalf("copyFileWithDiff 失败: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("读取目标文件失败: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("文件内容期望 %q，实际 %q", "new", string(data))
	}

	if len(diff.OverwrittenFiles) != 1 {
		t.Errorf("OverwrittenFiles 长度期望 1，实际 %d", len(diff.OverwrittenFiles))
	}
	if len(diff.AddedFiles) != 0 {
		t.Errorf("AddedFiles 长度期望 0，实际 %d", len(diff.AddedFiles))
	}
}

// TestCopyDirWithDiff_递归复制 测试递归复制目录
func TestCopyDirWithDiff_递归复制(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")

	// 创建源目录结构
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("b"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "skip_ZH.md"), []byte("zh"), 0o644)

	var diff CopyDiffResult
	ignorePatterns := []string{"*_ZH.md", "*_EN.md"}
	if err := copyDirWithDiff(srcDir, dstDir, &diff, ignorePatterns); err != nil {
		t.Fatalf("copyDirWithDiff 失败: %v", err)
	}

	// 验证 a.txt 被复制
	if data, err := os.ReadFile(filepath.Join(dstDir, "a.txt")); err != nil || string(data) != "a" {
		t.Errorf("a.txt 内容不正确")
	}
	// 验证 sub/b.txt 被复制
	if data, err := os.ReadFile(filepath.Join(dstDir, "sub", "b.txt")); err != nil || string(data) != "b" {
		t.Errorf("sub/b.txt 内容不正确")
	}
	// 验证 skip_ZH.md 被跳过
	if _, err := os.Stat(filepath.Join(dstDir, "skip_ZH.md")); !os.IsNotExist(err) {
		t.Errorf("skip_ZH.md 应该被跳过")
	}
}

// TestCopyDirWithDiffIncremental_增量复制 测试增量模式跳过已有文件
func TestCopyDirWithDiffIncremental_增量复制(t *testing.T) {
	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "src")
	dstDir := filepath.Join(tmpDir, "dst")

	// 创建源目录
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(srcDir, "b.txt"), []byte("b"), 0o644)

	// 目标目录已有 a.txt
	os.MkdirAll(dstDir, 0o755)
	os.WriteFile(filepath.Join(dstDir, "a.txt"), []byte("old_a"), 0o644)

	var diff CopyDiffResult
	if err := copyDirWithDiffIncremental(srcDir, dstDir, &diff, nil); err != nil {
		t.Fatalf("copyDirWithDiffIncremental 失败: %v", err)
	}

	// a.txt 应该保持原内容（未被覆盖）
	if data, err := os.ReadFile(filepath.Join(dstDir, "a.txt")); err != nil || string(data) != "old_a" {
		t.Errorf("a.txt 应保持原内容，实际 %q", string(data))
	}
	// b.txt 应该被复制
	if data, err := os.ReadFile(filepath.Join(dstDir, "b.txt")); err != nil || string(data) != "b" {
		t.Errorf("b.txt 内容不正确")
	}
	// 差异只应记录 b.txt
	if len(diff.AddedFiles) != 1 || !strings.HasSuffix(diff.AddedFiles[0], "b.txt") {
		t.Errorf("差异应只记录 b.txt，实际 %v", diff.AddedFiles)
	}
}

// TestShouldIgnore 测试忽略模式匹配
func TestShouldIgnore(t *testing.T) {
	patterns := []string{"*_ZH.md", "*_EN.md", "skills"}

	tests := []struct {
		name     string
		expected bool
	}{
		{"AGENT_ZH.md", true},
		{"AGENT_EN.md", true},
		{"AGENT.md", false},
		{"skills", true},
		{"skills_state.json", false},
		{"memory", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldIgnore(tt.name, patterns)
			if result != tt.expected {
				t.Errorf("shouldIgnore(%q) 期望 %v，实际 %v", tt.name, tt.expected, result)
			}
		})
	}
}

// TestMatchIgnorePattern 测试模式匹配算法
func TestMatchIgnorePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		input   string
		expect  bool
	}{
		{"后缀匹配_匹配", "*_ZH.md", "AGENT_ZH.md", true},
		{"后缀匹配_不匹配", "*_ZH.md", "AGENT.md", false},
		{"前缀匹配_匹配", "skill*", "skills", true},
		{"前缀匹配_不匹配", "skill*", "other", false},
		{"精确匹配_匹配", "skills", "skills", true},
		{"精确匹配_不匹配", "skills", "skills_state", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchIgnorePattern(tt.input, tt.pattern)
			if result != tt.expect {
				t.Errorf("matchIgnorePattern(%q, %q) 期望 %v，实际 %v",
					tt.input, tt.pattern, tt.expect, result)
			}
		})
	}
}
