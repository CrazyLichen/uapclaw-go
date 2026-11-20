// package_write_allowed.go 包写允许判定（父包落点）。
// Python: factory.py:390-421 package_write_allowed + 四张保护表；executor 与
// planner 均在父包消费（agents→父包方向可引），故核心实现落父包——
// agents/factory.go 的导出包装转调本实现（D-c2-5 与 D-c2-3 同模式）。

package memberoptimizer

import (
	"path/filepath"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// protectedPackageNames 编排器产物文件名（禁写）。
// Python: factory.py:37-40 protected_package_names。
var protectedPackageNames = map[string]bool{
	"plan.yaml": true, "execution_results.json": true, "verification.json": true,
	"fix_result.json": true, "current_harness_refs.yaml": true,
	"member_optimization_ref.yaml": true,
}

// protectedTopDirs 写入禁用的顶层目录。
// Python: factory.py:42-45 protected_top_dirs。
var protectedTopDirs = map[string]bool{
	"current_harnesses": true, "evaluation": true, "evaluations": true,
	"audit": true, "checkpoints": true,
}

// repairableTopDirs repair 模式可写的顶层目录。
// Python: factory.py:47-50 repairable_top_dirs。
var repairableTopDirs = map[string]bool{
	"prompt_sections": true, "skills": true, "tools": true,
	"rails": true, "subagents": true,
}

// repairableTopFiles repair 模式可写的顶层文件。
// Python: factory.py:52-55 repairable_top_files。
var repairableTopFiles = map[string]bool{
	"harness.yaml": true, "identity.md": true, "soul.md": true,
}

// repairableSuffixes repair 模式可写的文件后缀。
// Python: factory.py:57-60 repairable_suffixes。
var repairableSuffixes = map[string]bool{
	".yaml": true, ".yml": true, ".json": true, ".py": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PackageWriteAllowedCore 包写允许判定（Python factory.py:390-421 语义）。
func PackageWriteAllowedCore(path string, declaredPaths []string, repair bool) bool {
	// Python: candidate = Path(path); if is_absolute or == ".": return False
	candidate := path
	if strings.HasPrefix(candidate, "/") || candidate == "" {
		return false
	}
	parts := strings.Split(strings.ReplaceAll(candidate, "\\", "/"), "/")
	if len(parts) == 0 || containsDotDotPath(candidate) {
		return false
	}
	// Python: any(part.startswith(".") or part in protected_package_names ...)
	for _, part := range parts {
		if strings.HasPrefix(part, ".") || protectedPackageNames[part] {
			return false
		}
	}
	// Python: if candidate.parts[0] in protected_top_dirs: return False
	if protectedTopDirs[parts[0]] {
		return false
	}
	// Python: if repair: repairable 分支
	if repair {
		if repairableTopDirs[parts[0]] {
			return true
		}
		if repairableTopFiles[candidate] {
			return true
		}
		return repairableSuffixes[strings.ToLower(filepath.Ext(candidate))]
	}
	// Python: any(candidate == declared or declared in candidate.parents ...)
	for _, declared := range declaredPaths {
		if strings.HasPrefix(declared, "/") || containsDotDotPath(declared) {
			continue
		}
		declaredNorm := strings.Trim(NormalizeRelPath(declared), "/")
		if declaredNorm == "" {
			continue
		}
		if candidate == declaredNorm || strings.HasPrefix(candidate, declaredNorm+"/") {
			return true
		}
	}
	return false
}
