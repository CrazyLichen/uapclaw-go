// JiuwenClaw 自动权限的集中保护路径默认值。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/protected_paths.py
package permissions

import "strings"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// JiuwenclawProtectedWritePaths 默认保护的写入路径。
//
// 这些路径即便在自动权限模式下也不允许工具写入。
//
// Python: JIUWENCLAW_PROTECTED_WRITE_PATHS
var JiuwenclawProtectedWritePaths = []string{
	"jiuwenbox",
	"jiuwenswarm/agents/harness/common/rails/permissions",
	"tests/unit_tests/agentserver/permissions",
	"tests/system_tests/auto_permission_matrix",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MergeProtectedWritePaths 合并多组保护路径，去空白、去重、保序。
//
// 顺序按首次出现保留，便于上层做稳定的日志与审计输出。
//
// Python: merge_protected_write_paths(*path_groups)
func MergeProtectedWritePaths(pathGroups ...[]string) []string {
	merged := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range pathGroups {
		for _, path := range group {
			text := strings.TrimSpace(path)
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			merged = append(merged, text)
		}
	}
	return merged
}

// ──────────────────────────── 非导出函数 ────────────────────────────
