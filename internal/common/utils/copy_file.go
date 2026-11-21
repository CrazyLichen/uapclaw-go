// copy_file.go 文件复制共享助手。
// Python: shutil.copy2 等价语义（内容 + 权限 + 修改时间，父目录自动创建）。
// 收口背景：judger/judge_evidence.go、data_loader/case_files.go、
// evaluator/case_runner.go 三处私有同款实现去重（pythonsem 共享助手纪律延伸）。
package utils

import (
	"os"
	"path/filepath"
)

// CopyFile 复制文件（shutil.copy2 等价：内容 + 原文件权限 + 修改时间，
// 目标父目录自动创建）。源文件 stat 失败时返回错误（对齐 Python 抛 OSError）。
func CopyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, raw, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}
