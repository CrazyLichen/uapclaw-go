// repository_snapshot.go 拷贝被评估证据并在部分失败时不丢弃可读文件。
// Python: openjiuwen/rsi/harness_rsi/evaluation_result_analyzer/repository_snapshot.py（全量对齐）。
// 部分失败契约（Python docstring 明示）：copytree 收集逐条目失败并继续拷贝可读文件。

package evaluationresultanalyzer

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// ──────────────────────────── 常量 ────────────────────────────

// copyIgnores 拷贝排除集。
// Python: repository_snapshot.py:13-22 _COPY_IGNORES
var copyIgnores = map[string]bool{
	".git": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true,
	".tox": true, ".venv": true, "__pycache__": true, "node_modules": true,
}

// rootRuntimeDirs repository 根级运行时目录排除集。
// Python: repository_snapshot.py:23 _ROOT_RUNTIME_DIRS
var rootRuntimeDirs = map[string]bool{
	"agents": true, "context": true, "memory": true, "messages": true, "todo": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PrepareRepositorySnapshot 保留部分快照并向诊断披露每一次失败的拷贝。
// patch 是独立证据：仓库不可用不得阻止其拷贝。
// Python: repository_snapshot.py:26-81 prepare_repository_snapshot
func PrepareRepositorySnapshot(workspace, patch, runtimeDir, artifacts string) (map[string]any, error) {
	// Python: runtime = _io_path(runtime_dir); runtime.mkdir(parents=True, exist_ok=True)
	runtime := harnessrsi.IoPath(runtimeDir)
	if err := os.MkdirAll(runtime, 0o755); err != nil {
		return nil, err
	}
	manifest := map[string]any{
		"repository": "unavailable", "patch": "unavailable",
		"artifacts": "unavailable", "errors": []any{},
	}
	errors := func() []any { return manifest["errors"].([]any) }
	appendError := func(path, errMsg string) {
		manifest["errors"] = append(errors(), map[string]any{"path": path, "error": errMsg})
	}

	// Python: if patch: shutil.copy2 → source_patch.diff
	if patch != "" {
		expanded, _ := expandUserPath(patch)
		source := harnessrsi.IoPath(expanded)
		target := filepath.Join(runtime, "source_patch.diff")
		if copyErr := copyFilePreserve(source, target); copyErr != nil {
			appendError("source_patch.diff", copyErr.Error())
		} else {
			manifest["patch"] = "copied"
		}
	}

	// Python: for name, location in (("repository", workspace), ("artifacts", artifacts))
	for _, pair := range []struct{ name, location string }{
		{"repository", workspace}, {"artifacts", artifacts},
	} {
		if pair.location == "" {
			continue
		}
		expanded, _ := expandUserPath(pair.location)
		source := harnessrsi.IoPath(expanded)
		destination := filepath.Join(runtime, pair.name)

		// Python: ignore(directory, names) —— 排除集；repository 根级另排 _ROOT_RUNTIME_DIRS
		ignore := func(dir string, names []string) []string {
			var ignored []string
			for _, name := range names {
				if copyIgnores[name] {
					ignored = append(ignored, name)
				}
			}
			// Python: if snapshot_name == "repository" and Path(directory) == snapshot_source
			if pair.name == "repository" && resolvePath(dir) == resolvePath(source) {
				for _, name := range names {
					if rootRuntimeDirs[name] && !containsStr(ignored, name) {
						ignored = append(ignored, name)
					}
				}
			}
			return ignored
		}

		// Python: shutil.copytree(..., symlinks=False, copy_function=shutil.copy2)
		// Go 等价：递归拷贝 + 逐条目失败收集（shutil.Error 语义）+ 根级 OSError 语义
		if _, statErr := os.Stat(source); statErr != nil {
			// Python: 根不可达 → OSError → errors 加 {name}/ 条目
			appendError(pair.name+"/", statErr.Error())
			continue
		}
		failures := copyTree(source, destination, ignore)
		if len(failures) == 0 {
			manifest[pair.name] = "complete"
		} else {
			// Python: except shutil.Error → manifest[name] = "partial" + 逐条目披露
			manifest[pair.name] = "partial"
			for _, failure := range failures {
				// Python: relative = Path(src).relative_to(source).as_posix()
				rel, relErr := filepath.Rel(source, failure.src)
				if relErr != nil {
					rel = failure.src
				}
				pathKey := rel
				if pair.name == "artifacts" {
					pathKey = "artifacts/" + filepath.ToSlash(rel)
				}
				appendError(filepath.ToSlash(pathKey), failure.err.Error())
			}
		}
	}

	// Python: (runtime / "repository_snapshot.json").write_text(json.dumps(..., indent=2))
	content, marshalErr := json.MarshalIndent(manifest, "", "  ")
	if marshalErr != nil {
		return nil, marshalErr
	}
	if err := os.WriteFile(filepath.Join(runtime, "repository_snapshot.json"), content, 0o644); err != nil {
		// Python: write_text OSError 上抛；Go 以 error 等价透传
		return nil, err
	}
	return manifest, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// copyFailure 逐条目拷贝失败记录（shutil.Error.args[0] 元组等价）。
type copyFailure struct {
	src string
	err error
}

// copyTree 递归拷贝目录树（shutil.copytree 等价）：
// ignore 决定跳过项；symlinks=False 意味着跟随符号链接拷贝内容；
// 单条目失败收集后继续（shutil.Error 语义），根级失败整批上抛。
func copyTree(source, destination string, ignore func(dir string, names []string) []string) []copyFailure {
	var failures []copyFailure
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return []copyFailure{{src: source, err: err}}
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return []copyFailure{{src: source, err: err}}
	}
	// Python: ignore(directory, names) 返回被忽略的名字集合
	ignored := map[string]bool{}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, name := range ignore(source, names) {
		ignored[name] = true
	}

	for _, entry := range entries {
		if ignored[entry.Name()] {
			continue
		}
		src := filepath.Join(source, entry.Name())
		dst := filepath.Join(destination, entry.Name())
		if entry.IsDir() {
			failures = append(failures, copyTree(src, dst, ignore)...)
			continue
		}
		// symlinks=False：符号链接按目标内容拷贝（跟随）
		if entry.Type()&fs.ModeSymlink != 0 {
			target, linkErr := os.Readlink(src)
			if linkErr == nil {
				if resolved, resolveErr := resolveLinkTarget(src, target); resolveErr == nil {
					if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
						failures = append(failures, copyTree(resolved, dst, ignore)...)
						continue
					}
					src = resolved
				}
			}
		}
		if copyErr := copyFilePreserve(src, dst); copyErr != nil {
			failures = append(failures, copyFailure{src: src, err: copyErr})
		}
	}
	return failures
}

// copyFilePreserve 单文件拷贝（shutil.copy2 等价：内容 + mtime 权限尽力保留）。
func copyFilePreserve(source, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("is a directory: %s", source)
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// shutil.copy2 的元数据保留（尽力而为）
	_ = os.Chmod(target, info.Mode())
	return nil
}

// resolveLinkTarget 解析符号链接目标的绝对路径。
func resolveLinkTarget(linkPath, target string) (string, error) {
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Abs(filepath.Join(filepath.Dir(linkPath), target))
}
