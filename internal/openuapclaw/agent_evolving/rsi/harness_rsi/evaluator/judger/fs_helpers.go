package judger

// judger 包内文件系统与类型小助手（judge_search/judge_evidence 共用）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// fmtErrorf 别名助手。
func fmtErrorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// toInt 宽松整型提取（bool 拒绝，对齐 Python isinstance(v, bool) 检查）。
func toInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case bool:
		return 0, false
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	default:
		return 0, false
	}
}

// pathExists 路径存在（文件或目录）。
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// osStat os.Stat 包装（便于测试缝）。
func osStat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// osReadDir 读目录项名列表。
func osReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

// osReadFileBounded 有界读文件。
func osReadFileBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

// isSymlink 是否符号链接。
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// withinWorkspace 路径是否在冻结工作区内（is_relative_to 语义）。
func withinWorkspace(workspace, path string) bool {
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// relPath 相对路径（posix 斜杠，as_posix 语义）。
func relPath(workspace, path string) string {
	rel, err := filepath.Rel(workspace, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// globMatch fnmatch.fnmatch 近似：转 * ? 为 path.Match 通配（大小写敏感，
// Windows 侧 fnmatch 也大小写不敏感——本包运行环境为服务端 Linux，属平台差异）。
func globMatch(pattern, name string) bool {
	converted := strings.ReplaceAll(pattern, "**", "*")
	ok, err := path.Match(converted, name)
	if err != nil {
		return false
	}
	return ok
}

// containsNullByte 是否含 NUL 字节。
func containsNullByte(raw []byte) bool {
	for _, b := range raw {
		if b == 0 {
			return true
		}
	}
	return false
}

// decodeUTF8Sigil 去 BOM 后解码；非法序列按 replace 劣化（Go string 恒 UTF-8，
// 非法字节在stringify时以 U+FFFD 呈现——bytes→string 不会出错，无需 errors=replace 分支）。
func decodeUTF8Sigil(raw []byte) string {
	return strings.TrimPrefix(string(raw), "\uFEFF")
}

// jsonMarshalCompactAny 任意 JSON 值紧凑序列化（探测与输出共用）。
func jsonMarshalCompactAny(value any) ([]byte, error) {
	return json.Marshal(value)
}
