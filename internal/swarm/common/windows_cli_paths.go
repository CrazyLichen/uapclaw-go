// Python 源：jiuwenswarm/common/windows_cli_paths.py（159 行，本批随 T1 移植解析半区）
//
// ResolvePreservedCLIPath 系：在缺失的绝对路径上，用已验证的保留（preserved）
// 映射把路径翻译回可用副本——不依赖安装器自有文件。消费方：mcp_config（stdio
// MCP 命令/args/cwd）、external_cli_catalog、config 归一化、面板写链。
//
// 已登记不移植段（偏差 D-15.16-e8，Python 源行号）：
//   - _preserved_prefixes 的 sdk 分支（windows_cli_paths.py:40-46）：消费
//     external_cli_runtime 的运行状态查询——D-15.16-d7 已裁决安装器（桌面
//     打包版）专属不移植，Go 无对应运行态，sdk 分支恒空；
//   - configure_windows_npm_environment / _configured_prefix /
//     _inside_installation（windows_cli_paths.py:75-159）：唯一消费方是
//     scripts/jiuwenswarm_exe_entry.py:108（桌面版启动脚本），同属安装器专属。

package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolvePreservedCLIDir 目录语义包装（对应 Python directory=True 形参）。
// Python: resolve_preserved_cli_path(value, directory=True)
func ResolvePreservedCLIDir(value string) string {
	return resolvePreservedCLIPath(value, true)
}

// ResolvePreservedCLIPath 只翻译「缺失且被已验证保留映射覆盖」的绝对路径；
// 其余原样返回。对应 Python 文件型调用（directory=False 缺省）。
// Python: resolve_preserved_cli_path (windows_cli_paths.py:54-72)
func ResolvePreservedCLIPath(value string) string {
	return resolvePreservedCLIPath(value, false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// windowsNPMRoot 用户数据目录下的托管 npm 根。
// 与预装 PowerShell 助手共享同一用户数据路径契约。
// Python: windows_npm_root (windows_cli_paths.py:13-17)
//   - get_user_workspace_dir() → Go path.WorkspaceDir()（UAPCLAW_DATA_DIR →
//     数据根映射已确立，见 web_file_download.go:447 注释）。
func windowsNPMRoot() string {
	return filepath.Join(path.WorkspaceDir(), "runtime", "npm")
}

// preservedCLIPrefix 读已完成记录，把工作副本限定在自有安装库内。
// Python: _preserved_prefixes (windows_cli_paths.py:20-51)；sdk 形参分支不移植
// （D-15.16-e8，见文件头）——本函数恒等价于 Python _preserved_prefixes(sdk=False)。
func preservedCLIPrefixes() [][2]string {
	root := windowsNPMRoot()
	store := filepath.Join(filepath.Dir(root), "external-cli-preserved")
	installed := filepath.Join(filepath.Dir(root), "external-cli")
	if stat, err := os.Stat(store); err != nil || !stat.IsDir() {
		return nil
	}
	manifests, err := filepath.Glob(filepath.Join(store, "*", "mapping.json"))
	if err != nil {
		return nil
	}
	// mtime 倒序（Python sorted(..., key=st_mtime, reverse=True)）。
	type manifestEntry struct {
		path  string
		mtime int64
	}
	entries := make([]manifestEntry, 0, len(manifests))
	for _, manifest := range manifests {
		info, err := os.Stat(manifest)
		if err != nil {
			continue
		}
		entries = append(entries, manifestEntry{path: manifest, mtime: info.ModTime().UnixNano()})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].mtime > entries[j].mtime })

	prefixes := make([][2]string, 0, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(entry.path)
		if err != nil {
			continue
		}
		// Python read_text(encoding="utf-8-sig")——剥离 BOM。
		text := strings.TrimPrefix(string(data), "\ufeff")
		var mapping map[string]any
		if err := json.Unmarshal([]byte(text), &mapping); err != nil {
			continue
		}
		if mapping["schema"] != float64(1) {
			continue
		}
		sourceStr, _ := mapping["source"].(string)
		targetStr, _ := mapping["target"].(string)
		if sourceStr == "" || targetStr == "" {
			// Python KeyError → continue
			continue
		}
		if !filepath.IsAbs(sourceStr) {
			// Python source.is_absolute() 校验在三元组入列前
			continue
		}
		installedAbs, err := filepath.EvalSymlinks(installed)
		if err != nil {
			continue
		}
		targetAbs, err := filepath.EvalSymlinks(targetStr)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(installedAbs, targetAbs)
		if err != nil || strings.HasPrefix(rel, "..") {
			// Python target.resolve().relative_to(installed.resolve()) 越界抛 ValueError
			continue
		}
		if stat, err := os.Stat(targetStr); err != nil || !stat.IsDir() {
			continue
		}
		// Python 去重：(source, target) not in prefixes
		dup := false
		for _, pair := range prefixes {
			if pair[0] == sourceStr && pair[1] == targetStr {
				dup = true
				break
			}
		}
		if !dup {
			prefixes = append(prefixes, [2]string{sourceStr, targetStr})
		}
	}
	return prefixes
}

// resolvePreservedCLIPath 只翻译「缺失且被已验证保留映射覆盖」的绝对路径。
// Python: resolve_preserved_cli_path (windows_cli_paths.py:54-72)
func resolvePreservedCLIPath(value string, directory bool) string {
	if runtime.GOOS != "windows" || value == "" {
		// Python: os.name != "nt" or not value → return value
		return value
	}
	// 引号剥离：value 首尾成对的单/双引号保留在返回值里（Python quote 语义）。
	quote := ""
	trimmed := value
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		quote = string(value[0])
		trimmed = value[1 : len(value)-1]
	}
	if !filepath.IsAbs(trimmed) {
		// Python original.is_absolute() 为假 → 原样返回
		return value
	}
	if _, err := os.Stat(trimmed); err == nil {
		// Python original.exists() 为真 → 原样返回
		return value
	}
	for _, prefix := range preservedCLIPrefixes() {
		source, target := prefix[0], prefix[1]
		rel, err := filepath.Rel(source, trimmed)
		if err != nil || strings.HasPrefix(rel, "..") {
			// Python original.relative_to(source) 越界抛 ValueError
			continue
		}
		candidate := filepath.Join(target, rel)
		candidateAbs, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		targetAbs, err := filepath.EvalSymlinks(target)
		if err != nil {
			continue
		}
		relCandidate, err := filepath.Rel(targetAbs, candidateAbs)
		if err != nil || strings.HasPrefix(relCandidate, "..") {
			continue
		}
		stat, statErr := os.Stat(candidate)
		var matches bool
		if directory {
			matches = statErr == nil && stat.IsDir()
		} else {
			matches = statErr == nil && !stat.IsDir()
		}
		if matches {
			return quote + candidate + quote
		}
	}
	return value
}
