package evaluationresultanalyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareRepositorySnapshot_完整拷贝 对照 Python prepare_repository_snapshot 正常路径。
// Python: repository_snapshot.py:26-81
func TestPrepareRepositorySnapshot_完整拷贝(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()

	// workspace：含子目录与普通文件
	ws := filepath.Join(caseRoot, "ws")
	if err := os.MkdirAll(filepath.Join(ws, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "src", "main.py"), []byte("print(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	// patch 独立证据
	patch := filepath.Join(caseRoot, "p.diff")
	if err := os.WriteFile(patch, []byte("diff --git a"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := PrepareRepositorySnapshot(ws, patch, runtimeDir, "")
	if manifest["repository"] != "complete" {
		t.Fatalf("repository 应 complete: %v", manifest["repository"])
	}
	if manifest["patch"] != "copied" {
		t.Fatalf("patch 应 copied: %v", manifest["patch"])
	}
	// artifacts 未提供 → 保持 unavailable（Python: if not location: continue）
	if manifest["artifacts"] != "unavailable" {
		t.Fatalf("artifacts 未提供应 unavailable: %v", manifest["artifacts"])
	}
	if len(manifest["errors"].([]any)) != 0 {
		t.Fatalf("应无错误: %v", manifest["errors"])
	}
	// 拷贝内容核对
	content, err := os.ReadFile(filepath.Join(runtimeDir, "repository", "src", "main.py"))
	if err != nil || string(content) != "print(1)" {
		t.Fatalf("嵌套文件拷贝不符: %v %s", err, content)
	}
	if content2, err2 := os.ReadFile(filepath.Join(runtimeDir, "source_patch.diff")); err2 != nil || string(content2) != "diff --git a" {
		t.Fatalf("patch 拷贝不符: %v", err2)
	}
	// manifest 落盘
	if _, err := os.Stat(filepath.Join(runtimeDir, "repository_snapshot.json")); err != nil {
		t.Fatal("repository_snapshot.json 应落盘")
	}
}

// TestPrepareRepositorySnapshot_排除集与根级运行时目录 对照 Python ignore 回调。
// Python: repository_snapshot.py:13-23, 54-59
func TestPrepareRepositorySnapshot_排除集与根级运行时目录(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()

	ws := filepath.Join(caseRoot, "ws")
	for _, name := range []string{".git", "__pycache__", "node_modules", ".venv", ".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache"} {
		if err := os.MkdirAll(filepath.Join(ws, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// repository 根级：agents/context/memory/messages/todo 也排除
	for _, name := range []string{"agents", "context", "memory", "messages", "todo"} {
		if err := os.MkdirAll(filepath.Join(ws, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, name, "f.txt"), []byte("runtime"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 普通文件 + 子目录内的同名运行时目录（仅根级排除）
	if err := os.WriteFile(filepath.Join(ws, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "src", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "src", "agents", "f.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := PrepareRepositorySnapshot(ws, "", runtimeDir, "")
	if manifest["repository"] != "complete" {
		t.Fatalf("应 complete: %v", manifest["repository"])
	}
	dst := filepath.Join(runtimeDir, "repository")
	for _, excluded := range []string{".git", "__pycache__", "node_modules", ".venv", "agents", "context", "memory", "messages", "todo"} {
		if _, err := os.Stat(filepath.Join(dst, excluded)); err == nil {
			t.Fatalf("%s 应被排除", excluded)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "keep.txt")); err != nil {
		t.Fatal("keep.txt 应保留")
	}
	// 子目录内同名目录不排除（仅根级）
	if _, err := os.Stat(filepath.Join(dst, "src", "agents", "f.txt")); err != nil {
		t.Fatal("非根级 agents 不应排除")
	}
}

// TestPrepareRepositorySnapshot_部分失败收集 对照 Python shutil.Error 部分失败契约。
// Python: repository_snapshot.py:33-36, 64-73
func TestPrepareRepositorySnapshot_部分失败收集(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()

	ws := filepath.Join(caseRoot, "ws")
	if err := os.MkdirAll(filepath.Join(ws, "good"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "good.txt"), []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "good", "nested.txt"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 制造不可读文件（非 root 才生效；root 环境降级为目录占位冲突）
	badFile := filepath.Join(ws, "bad.txt")
	if err := os.WriteFile(badFile, []byte("bad"), 0o000); err != nil {
		t.Fatal(err)
	}
	canFail := true
	if info, statErr := os.Stat(badFile); statErr == nil && info.Mode().Perm() != 0 {
		_ = info
	}
	if err := os.Chmod(badFile, 0o000); err != nil {
		t.Fatal(err)
	}
	// 若以 root 运行（0o000 仍可读），改用目录占位冲突制造失败
	defer func() { _ = os.Chmod(badFile, 0o644) }()

	manifest := PrepareRepositorySnapshot(ws, "", runtimeDir, "")
	if isRunningAsRoot() {
		// root 下 0o000 可读：改用占位冲突验证部分失败
		t.Log("root 环境，0o000 权限注入无效，跳过部分失败断言（占位冲突由下方 Case2 覆盖）")
	} else {
		if manifest["repository"] != "partial" {
			t.Fatalf("应 partial: %v", manifest)
		}
		// 可读文件不被丢弃
		if _, err := os.Stat(filepath.Join(runtimeDir, "repository", "good.txt")); err != nil {
			t.Fatal("good.txt 应已拷贝（部分失败不中断）")
		}
		if _, err := os.Stat(filepath.Join(runtimeDir, "repository", "good", "nested.txt")); err != nil {
			t.Fatal("nested.txt 应已拷贝")
		}
		errList := manifest["errors"].([]any)
		found := false
		for _, item := range errList {
			entry := item.(map[string]any)
			if entry["path"] == "bad.txt" {
				found = true
			}
		}
		if !found {
			t.Fatalf("bad.txt 失败应被披露: %v", errList)
		}
	}
	_ = canFail
}

// isRunningAsRoot 是否以 root 运行（权限注入失效守卫）。
func isRunningAsRoot() bool {
	return os.Geteuid() == 0
}

// TestPrepareRepositorySnapshot_占位冲突部分失败 用目标侧同名目录制造单文件失败。
func TestPrepareRepositorySnapshot_占位冲突部分失败(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()

	ws := filepath.Join(caseRoot, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "clash.txt"), []byte("clash"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 目标侧预置同名"目录"，文件拷贝必然失败
	if err := os.MkdirAll(filepath.Join(runtimeDir, "repository", "clash.txt"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := PrepareRepositorySnapshot(ws, "", runtimeDir, "")
	if manifest["repository"] != "partial" {
		t.Fatalf("应 partial: %v", manifest)
	}
	// 可读文件不被丢弃
	if content, err := os.ReadFile(filepath.Join(runtimeDir, "repository", "ok.txt")); err != nil || string(content) != "ok" {
		t.Fatalf("ok.txt 应已拷贝（部分失败不中断）: %v", err)
	}
	errList := manifest["errors"].([]any)
	found := false
	for _, item := range errList {
		entry := item.(map[string]any)
		if entry["path"] == "clash.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("clash.txt 失败应被披露: %v", errList)
	}
}

// TestPrepareRepositorySnapshot_根不可达 对照 Python OSError 分支。
// Python: repository_snapshot.py:74-75
func TestPrepareRepositorySnapshot_根不可达(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	// workspace 指向不存在路径
	manifest := PrepareRepositorySnapshot(filepath.Join(caseRoot, "nope"), "", runtimeDir, "")
	// Python: OSError → errors 加 {name}/ 条目；manifest[name] 不置 complete/partial
	errList := manifest["errors"].([]any)
	found := false
	for _, item := range errList {
		entry := item.(map[string]any)
		if entry["path"] == "repository/" {
			found = true
		}
	}
	if !found {
		t.Fatalf("根不可达应披露 repository/: %v", errList)
	}
	if v, ok := manifest["repository"].(string); ok && v == "complete" {
		t.Fatal("根不可达不应 complete")
	}
}

// TestPrepareRepositorySnapshot_patch失败披露 对照 Python patch OSError 分支。
// Python: repository_snapshot.py:41-46
func TestPrepareRepositorySnapshot_patch失败披露(t *testing.T) {
	caseRoot := t.TempDir()
	runtimeDir := t.TempDir()
	// patch 指向不存在文件
	manifest := PrepareRepositorySnapshot("", filepath.Join(caseRoot, "missing.diff"), runtimeDir, "")
	if manifest["patch"] != "unavailable" {
		t.Fatalf("patch 缺失应 unavailable: %v", manifest["patch"])
	}
	errList := manifest["errors"].([]any)
	if len(errList) != 1 {
		t.Fatalf("应披露 1 错误, got %v", errList)
	}
	entry := errList[0].(map[string]any)
	if entry["path"] != "source_patch.diff" {
		t.Fatalf("错误 path 不符: %v", entry["path"])
	}
	if !strings.Contains(entry["error"].(string), "missing.diff") {
		t.Fatalf("错误信息应含路径: %v", entry["error"])
	}
}
