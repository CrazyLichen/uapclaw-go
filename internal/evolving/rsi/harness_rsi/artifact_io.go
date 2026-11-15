package harnessrsi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadYAMLMapping 读取 YAML 映射，文件缺失或顶层非映射时返回空映射。
func ReadYAMLMapping(path string) map[string]any {
	artifactPath, err := filepath.Abs(expandUser(path))
	if err != nil {
		return map[string]any{}
	}
	info, err := os.Stat(artifactPath)
	if err != nil || info.IsDir() {
		return map[string]any{}
	}
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return map[string]any{}
	}
	var data any
	if err := yaml.Unmarshal(raw, &data); err != nil || data == nil {
		return map[string]any{}
	}
	if mapping, ok := data.(map[string]any); ok {
		return mapping
	}
	return map[string]any{}
}

// WriteYAMLMapping 写入 YAML 映射并返回其绝对路径。
func WriteYAMLMapping(path string, data map[string]any) string {
	artifactPath, err := filepath.Abs(expandUser(path))
	if err != nil {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return path
	}
	raw, err := yaml.Marshal(data)
	if err != nil {
		return path
	}
	if err := os.WriteFile(artifactPath, raw, 0o644); err != nil {
		return path
	}
	return artifactPath
}

// ReadJSONMapping 读取 JSON 映射，文件缺失或顶层非映射时返回空映射。
func ReadJSONMapping(path string) map[string]any {
	artifactPath, err := filepath.Abs(expandUser(path))
	if err != nil {
		return map[string]any{}
	}
	info, err := os.Stat(artifactPath)
	if err != nil || info.IsDir() {
		return map[string]any{}
	}
	raw, err := os.ReadFile(artifactPath)
	if err != nil {
		return map[string]any{}
	}
	var data any
	if err := json.Unmarshal(raw, &data); err != nil || data == nil {
		return map[string]any{}
	}
	if mapping, ok := data.(map[string]any); ok {
		return mapping
	}
	return map[string]any{}
}

// WriteJSONMapping 写入 JSON 映射并返回其绝对路径。
func WriteJSONMapping(path string, data map[string]any) string {
	artifactPath, err := filepath.Abs(expandUser(path))
	if err != nil {
		return path
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return path
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return path
	}
	if err := os.WriteFile(artifactPath, raw, 0o644); err != nil {
		return path
	}
	return artifactPath
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// expandUser 展开路径前缀 ~ 为当前用户 HOME 目录。
func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// ioPath 文件系统 IO 使用扩展长度路径（仅 Windows），绝不用于模型工具输入。
// 对齐 Python artifact_io._io_path：UNC 前缀 \\?\UNC、盘符前缀 \\?\X\。
func ioPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if runtime.GOOS != "windows" || strings.HasPrefix(absolute, `\\?\`) {
		return absolute
	}
	if strings.HasPrefix(absolute, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(absolute, `\\`)
	}
	// 盘符形如 C:\...：加 \\?\ 前缀
	if len(absolute) >= 2 && absolute[1] == ':' {
		return `\\?\` + absolute
	}
	return absolute
}
