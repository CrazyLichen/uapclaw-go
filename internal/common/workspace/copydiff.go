package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CopyDiffResult 记录文件复制操作的变更差异。
//
// 对应 Python: jiuwenswarm/common/utils.py CopyDiffResult
//
// 追踪三类变更：
//   - AddedDirs: 新增的目录
//   - AddedFiles: 新增的文件
//   - OverwrittenFiles: 被覆盖的文件
type CopyDiffResult struct {
	AddedDirs        []string
	AddedFiles       []string
	OverwrittenFiles []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Merge 将另一个 CopyDiffResult 的内容合并到当前实例。
//
// 用于多次复制操作后汇总差异，对应 Python 中 TrackCopyDiff 的 cumulative 参数。
func (r *CopyDiffResult) Merge(other CopyDiffResult) {
	r.AddedDirs = append(r.AddedDirs, other.AddedDirs...)
	r.AddedFiles = append(r.AddedFiles, other.AddedFiles...)
	r.OverwrittenFiles = append(r.OverwrittenFiles, other.OverwrittenFiles...)
}

// PrintSummary 打印文件变更统计摘要。
//
// overwrite 为 true 时不打印（强制覆盖模式下差异无意义），
// 对应 Python: jiuwenswarm/common/utils.py _print_diff_summary()
func (r *CopyDiffResult) PrintSummary(overwrite bool) {
	if overwrite {
		return
	}

	total := len(r.AddedFiles) + len(r.OverwrittenFiles)
	if total == 0 {
		fmt.Println("[uapclaw-init] 初始化完成：工作区已就绪，无新文件需创建 / Init complete: workspace ready, no new files needed")
		return
	}

	fmt.Println("[uapclaw-init] 初始化完成，文件变更如下 / Init complete, file changes:")
	if len(r.AddedFiles) > 0 {
		fmt.Printf("  新增文件 / New files: %d\n", len(r.AddedFiles))
		for i, f := range r.AddedFiles {
			if i >= 10 {
				fmt.Printf("    ...等 %d 个 / ...and %d more\n", len(r.AddedFiles)-10, len(r.AddedFiles)-10)
				break
			}
			fmt.Printf("    + %s\n", f)
		}
	}
	if len(r.OverwrittenFiles) > 0 {
		fmt.Printf("  更新文件 / Updated files: %d\n", len(r.OverwrittenFiles))
		for i, f := range r.OverwrittenFiles {
			if i >= 10 {
				fmt.Printf("    ...等 %d 个 / ...and %d more\n", len(r.OverwrittenFiles)-10, len(r.OverwrittenFiles)-10)
				break
			}
			fmt.Printf("    ~ %s\n", f)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyFileWithDiff 复制单个文件并记录差异到 diff。
//
// 如果目标文件已存在，记录到 OverwrittenFiles；否则记录到 AddedFiles。
// 复制前会确保目标目录存在。
func copyFileWithDiff(src, dst string, diff *CopyDiffResult) error {
	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", filepath.Dir(dst), err)
	}

	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败 %s: %w", src, err)
	}
	defer func() { _ = srcFile.Close() }()

	// 检查目标文件是否已存在
	_, err = os.Stat(dst)
	overwritten := err == nil

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建目标文件失败 %s: %w", dst, err)
	}
	defer func() { _ = dstFile.Close() }()

	// 复制内容
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("复制文件失败 %s -> %s: %w", src, dst, err)
	}

	// 记录差异
	if diff != nil {
		if overwritten {
			diff.OverwrittenFiles = append(diff.OverwrittenFiles, dst)
		} else {
			diff.AddedFiles = append(diff.AddedFiles, dst)
		}
	}

	return nil
}

// copyDirWithDiff 递归复制目录并记录差异到 diff。
//
// 跳过 _ZH.md 和 _EN.md 后缀的文件（多语言文件由单独逻辑处理）。
// 对应 Python: jiuwenswarm/common/utils.py _copy_dir()
func copyDirWithDiff(src, dst string, diff *CopyDiffResult, ignorePatterns []string) error {
	// 确保目标目录存在
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("读取目录失败 %s: %w", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// 检查是否匹配忽略模式
		if shouldIgnore(entry.Name(), ignorePatterns) {
			continue
		}

		if entry.IsDir() {
			if diff != nil {
				diff.AddedDirs = append(diff.AddedDirs, dstPath)
			}
			if err := copyDirWithDiff(srcPath, dstPath, diff, ignorePatterns); err != nil {
				return err
			}
		} else {
			if err := copyFileWithDiff(srcPath, dstPath, diff); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyDirWithDiffIncremental 增量复制目录：目标已存在的文件跳过，只复制缺失文件。
//
// 对应 Python: jiuwenswarm/common/utils.py _copy_dir() 的 copy_if_missing 模式
func copyDirWithDiffIncremental(src, dst string, diff *CopyDiffResult, ignorePatterns []string) error {
	// 确保目标目录存在
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("读取目录失败 %s: %w", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// 检查是否匹配忽略模式
		if shouldIgnore(entry.Name(), ignorePatterns) {
			continue
		}

		if entry.IsDir() {
			if err := copyDirWithDiffIncremental(srcPath, dstPath, diff, ignorePatterns); err != nil {
				return err
			}
		} else {
			// 增量模式：目标已存在则跳过
			if _, err := os.Stat(dstPath); err == nil {
				continue
			}
			if err := copyFileWithDiff(srcPath, dstPath, diff); err != nil {
				return err
			}
		}
	}

	return nil
}

// shouldIgnore 检查文件名是否匹配忽略模式。
//
// 支持简单的通配符匹配（仅 * 后缀模式，如 "*_ZH.md"）。
func shouldIgnore(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchIgnorePattern(name, pattern) {
			return true
		}
	}
	return false
}

// matchIgnorePattern 简单的忽略模式匹配。
//
// 支持两种模式：
//   - "*后缀"：匹配以指定后缀结尾的文件名（如 "*_ZH.md" 匹配 "AGENT_ZH.md"）
//   - "前缀*"：匹配以指定前缀开头的文件名
//   - 精确匹配：无通配符时直接比较
func matchIgnorePattern(name, pattern string) bool {
	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(name, suffix)
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(name, prefix)
	}
	return name == pattern
}
