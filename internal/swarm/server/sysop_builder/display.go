package sysop_builder

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ListAutoManagedSandboxPaths 列出自动管理的沙箱路径。
// 对齐 Python: list_auto_managed_sandbox_paths(project_dir, *, is_code_agent)
// 返回 {"allow_write": [...], "deny_write": [...]}，每项为 {"path": str, "permissions": str, "kind": str}
func ListAutoManagedSandboxPaths(
	projectDir string,
	isCodeAgent bool,
) map[string][]map[string]string {
	allow := make([]map[string]string, 0)
	deny := make([]map[string]string, 0)

	// 固有 rw 文件
	for _, fn := range intrinsicRWFilePathFuncs {
		raw := fn()
		if raw == "" {
			continue
		}
		allow = appendUnique(allow, map[string]string{
			"path":        raw,
			"permissions": "0666",
			"kind":        "file",
		})
	}

	// daily_memory 目录（仅 host 存在时加入）
	dailyMemoryPath := filepath.Join(workspace.AgentMemoryDir(), "daily_memory")
	if info, err := os.Stat(dailyMemoryPath); err == nil && info.IsDir() {
		allow = appendUnique(allow, map[string]string{
			"path":        dailyMemoryPath + "/",
			"permissions": "0777",
			"kind":        "directory",
		})
	}

	// agent_skills 目录
	agentSkills := resolveAgentSkillsDir()
	if agentSkills != "" {
		allow = appendUnique(allow, map[string]string{
			"path":        agentSkills + "/",
			"permissions": "0777",
			"kind":        "directory",
		})
	}

	// 仅 code-agent 才挂 project_dir
	if isCodeAgent && projectDir != "" {
		resolved, err := filepath.Abs(projectDir)
		if err == nil {
			if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
				// 拒绝文件系统根
				if resolved != "/" && resolved != filepath.VolumeName(resolved)+string(os.PathSeparator) {
					allow = appendUnique(allow, map[string]string{
						"path":        resolved + "/",
						"permissions": "0777",
						"kind":        "directory",
					})
				}
			}
		}
	}

	// 固有 ro 文件（deny_write）
	for _, fn := range intrinsicROFilePathFuncs {
		raw := fn()
		if raw == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(raw)
		if err != nil {
			logger.Debug(logComponent).
				Str("path", raw).
				Err(err).
				Msg("auto view: 固有 ro 路径解析失败")
			continue
		}
		deny = appendUnique(deny, map[string]string{
			"path":        resolved,
			"permissions": "0444",
			"kind":        "file",
		})
	}

	return map[string][]map[string]string{
		"allow_write": allow,
		"deny_write":  deny,
	}
}

// ListEffectiveSandboxFiles 只读视图：auto + 用户条目合并。
// 对齐 Python: list_effective_sandbox_files(files_runtime, *, project_dir, is_code_agent)
func ListEffectiveSandboxFiles(
	filesRuntime map[string]any,
	projectDir string,
	isCodeAgent bool,
) map[string][]map[string]string {
	auto := ListAutoManagedSandboxPaths(projectDir, isCodeAgent)
	allow := make([]map[string]string, len(auto["allow_write"]))
	copy(allow, auto["allow_write"])
	deny := make([]map[string]string, len(auto["deny_write"]))
	copy(deny, auto["deny_write"])

	if filesRuntime == nil {
		filesRuntime = make(map[string]any)
	}

	// 处理 allow
	if allowEntries, ok := toSlice(filesRuntime["allow"]); ok {
		for _, entry := range allowEntries {
			normalized := normalizeFSEntry(entry, "0666")
			if normalized == nil {
				continue
			}
			path, _ := normalized["path"].(string)
			stripped := strings.TrimRight(path, "/")
			if stripped == "" {
				stripped = "/"
			}
			kind := classifyHostKind(stripped)
			display := stripped
			if kind == "directory" && stripped != "/" {
				display = stripped + "/"
			}
			permStr := "0666"
			if p, ok := normalized["permissions"].(string); ok && p != "" {
				permStr = p
			}
			allow = appendUnique(allow, map[string]string{
				"path":        display,
				"permissions": permStr,
				"kind":        kind,
			})
		}
	}

	// 处理 deny
	if denyEntries, ok := toSlice(filesRuntime["deny"]); ok {
		for _, entry := range denyEntries {
			normalized := normalizeFSEntry(entry, "0000")
			if normalized == nil {
				continue
			}
			path, _ := normalized["path"].(string)
			stripped := strings.TrimRight(path, "/")
			if stripped == "" {
				stripped = "/"
			}
			kind := classifyHostKind(stripped)
			display := stripped
			if kind == "directory" && stripped != "/" {
				display = stripped + "/"
			}
			permStr := "0000"
			if p, ok := normalized["permissions"].(string); ok && p != "" {
				permStr = p
			}
			deny = appendUnique(deny, map[string]string{
				"path":        display,
				"permissions": permStr,
				"kind":        kind,
			})
		}
	}

	return map[string][]map[string]string{
		"allow_write": allow,
		"deny_write":  deny,
	}
}

// FindAutoManagedMatch 判断路径是否已被 auto 管理。
// 对齐 Python: find_auto_managed_match(path, *, project_dir, is_code_agent)
// 返回 (bucket, canonicalPath, found)
func FindAutoManagedMatch(
	path string,
	projectDir string,
	isCodeAgent bool,
) (bucket, canonicalPath string, found bool) {
	target := resolveDisplayPath(path)
	if target == "" {
		return "", "", false
	}
	auto := ListAutoManagedSandboxPaths(projectDir, isCodeAgent)
	for _, b := range []string{"allow_write", "deny_write"} {
		entries, ok := auto[b]
		if !ok {
			continue
		}
		for _, entry := range entries {
			candidate := resolveDisplayPath(entry["path"])
			if candidate != "" && candidate == target {
				return b, entry["path"], true
			}
		}
	}
	return "", "", false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// appendUnique 去重追加（按 path 比较）。
// 对齐 Python: _append_unique(target, entry)
func appendUnique(target []map[string]string, entry map[string]string) []map[string]string {
	for _, item := range target {
		if item["path"] == entry["path"] {
			return target
		}
	}
	return append(target, entry)
}

// classifyHostKind 判定 path 是 "directory" 还是 "file"。
// 对齐 Python: _classify_host_kind(path)
func classifyHostKind(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "file"
	}
	if info.IsDir() {
		return "directory"
	}
	return "file"
}

// resolveDisplayPath 解析为绝对路径用于展示/比较。
// 对齐 Python: _resolve_display_path(raw)
func resolveDisplayPath(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	resolved, err := filepath.Abs(text)
	if err != nil {
		logger.Debug(logComponent).
			Str("path", text).
			Err(err).
			Msg("路径解析失败，无法用于展示")
		return ""
	}
	return resolved
}
