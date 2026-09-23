package project_memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	utilpath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

func TestFindProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	root := FindProjectRoot(subDir)
	if root == "" {
		t.Fatal("expected non-empty root")
	}
	resolved, _ := filepath.EvalSymlinks(tmpDir)
	got, _ := filepath.EvalSymlinks(root)
	if got != resolved {
		t.Fatalf("expected %s, got %s", resolved, got)
	}
}

func TestFindProjectRoot_多个marker(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "src")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// go.mod marker
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatal(err)
	}

	root := FindProjectRoot(subDir)
	if root == "" {
		t.Fatal("expected non-empty root")
	}
}

func TestFindProjectRoot_无标记时返回空(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "a", "b")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := FindProjectRoot(subDir)
	if root != "" {
		t.Fatalf("expected empty, got %s", root)
	}
}

func TestDiscoverAndLoadMemoryFiles_项目根加载(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	mdContent := "# Project Memory\nThis is project memory."
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte(mdContent), 0o644)

	// 清除可能残留的缓存
	ClearProjectMemoryCache(tmpDir)

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files) == 0 {
		t.Fatal("expected at least one file")
	}
	if files[0].Kind != "project" {
		t.Fatalf("expected kind=project, got %s", files[0].Kind)
	}
	if files[0].Content != mdContent {
		t.Fatalf("content mismatch: %q", files[0].Content)
	}
}

func TestDiscoverAndLoadMemoryFiles_空目录不生成结果(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	ClearProjectMemoryCache(tmpDir)

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}

func TestDiscoverAndLoadMemoryFiles_Glob扫描(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	rulesDir := filepath.Join(tmpDir, ".uapclaw", "rules")
	os.MkdirAll(rulesDir, 0o755)
	os.WriteFile(filepath.Join(rulesDir, "rule1.md"), []byte("Rule 1 content"), 0o644)
	os.WriteFile(filepath.Join(rulesDir, "rule2.md"), []byte("Rule 2 content"), 0o644)
	ClearProjectMemoryCache(tmpDir)

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files) < 2 {
		t.Fatalf("expected at least 2 files, got %d", len(files))
	}
}

func TestDiscoverAndLoadMemoryFiles_Local优先级最高(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("Project content"), 0o644)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.local.md"), []byte("Local content"), 0o644)
	ClearProjectMemoryCache(tmpDir)

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files) < 2 {
		t.Fatalf("expected at least 2 files, got %d", len(files))
	}
	// local (priority=40) 应在 project (priority=30) 之后
	var projectIdx, localIdx int
	for i, f := range files {
		if f.Kind == "project" && strings.Contains(f.Path, "UAPCLAWSWARM.md") && !strings.Contains(f.Path, "local") {
			projectIdx = i
		}
		if f.Kind == "local" {
			localIdx = i
		}
	}
	if localIdx <= projectIdx {
		t.Fatalf("local (idx=%d) should come after project (idx=%d)", localIdx, projectIdx)
	}
}

func TestDiscoverAndLoadMemoryFiles_Include展开(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	// 创建被 include 的文件
	os.WriteFile(filepath.Join(tmpDir, "extra.md"), []byte("Extra content from include"), 0o644)
	// 创建包含 @include 的主文件
	mainContent := "Main content\n@include extra.md\nMore content"
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte(mainContent), 0o644)
	ClearProjectMemoryCache(tmpDir)

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files) < 2 {
		t.Fatalf("expected at least 2 files (main + included), got %d", len(files))
	}
	// included 文件应作为独立 LoadedMemoryFile 出现
	var foundIncluded bool
	for _, f := range files {
		if strings.Contains(f.Path, "extra.md") {
			foundIncluded = true
			if f.Content != "Extra content from include" {
				t.Fatalf("included content mismatch: %q", f.Content)
			}
		}
	}
	if !foundIncluded {
		t.Fatal("expected included file to be loaded")
	}
}

func TestDiscoverAndLoadMemoryFiles_缓存快照失效(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("Original"), 0o644)
	ClearProjectMemoryCache(tmpDir)

	// 首次加载
	files1, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files1) == 0 || files1[0].Content != "Original" {
		t.Fatal("first load failed")
	}

	// 修改文件
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("Modified"), 0o644)

	// 再次加载应获取新内容（缓存快照失效）
	files2, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files2) == 0 {
		t.Fatal("second load returned empty")
	}
	if files2[0].Content != "Modified" {
		t.Fatalf("expected Modified, got %s", files2[0].Content)
	}
}

func TestDiscoverAndLoadMemoryFiles_FrontmatterPaths作用域(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	rulesDir := filepath.Join(tmpDir, ".uapclaw", "rules")
	os.MkdirAll(rulesDir, 0o755)
	// 创建带 paths frontmatter 的规则，限定仅匹配 src/**
	scopedContent := "---\npaths: [\"src/**\"]\n---\nScoped rule content"
	os.WriteFile(filepath.Join(rulesDir, "scoped.md"), []byte(scopedContent), 0o644)
	ClearProjectMemoryCache(tmpDir)

	// targetPath 是项目根目录，不匹配 src/**，应跳过
	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	for _, f := range files {
		if strings.Contains(f.Path, "scoped.md") {
			t.Fatal("scoped rule should not match workspace root path")
		}
	}
}

func TestMergeMemoryContent_基本合并(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/a.md", Kind: "project", Content: "AAA", Priority: 30},
		{Path: "/b.md", Kind: "local", Content: "BBB", Priority: 40},
	}
	merged := MergeMemoryContent(files, DefaultMaxChars)
	if !strings.Contains(merged, "AAA") || !strings.Contains(merged, "BBB") {
		t.Fatalf("merged should contain both contents, got: %s", merged)
	}
	if !strings.Contains(merged, "project memory") || !strings.Contains(merged, "local memory") {
		t.Fatalf("merged should contain kind headers, got: %s", merged)
	}
}

func TestMergeMemoryContent_超限截断(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/a.md", Kind: "project", Content: strings.Repeat("A", 100), Priority: 30},
		{Path: "/b.md", Kind: "local", Content: strings.Repeat("B", 100), Priority: 40},
	}
	merged := MergeMemoryContent(files, 50)
	if !strings.Contains(merged, "truncated") {
		t.Fatal("expected truncation marker")
	}
}

func TestMergeMemoryContent_空列表(t *testing.T) {
	merged := MergeMemoryContent(nil, DefaultMaxChars)
	if merged != "" {
		t.Fatalf("expected empty, got %q", merged)
	}
}

func TestClearProjectMemoryCache(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("test"), 0o644)
	ClearProjectMemoryCache(tmpDir)

	_, _ = DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	ClearProjectMemoryCache(tmpDir)
	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, nil)
	if len(files) == 0 {
		t.Fatal("expected files after cache clear")
	}
}

func TestGetLargeMemoryFiles_超限文件(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/small.md", Kind: "project", Content: strings.Repeat("A", 100), Priority: 30},
		{Path: "/large.md", Kind: "project", Content: strings.Repeat("B", 50000), Priority: 30},
	}
	warnings := GetLargeMemoryFiles(files, MaxMemoryCharCount)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0]["kind"] != "project" {
		t.Fatalf("expected kind=project, got %v", warnings[0]["kind"])
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

func TestParseFrontmatter_基本(t *testing.T) {
	raw := "---\npaths: [\"src/**\", \"test/**\"]\n---\nBody content\n"
	fm, body := parseFrontmatter(raw)
	if len(fm) == 0 {
		t.Fatal("expected non-empty frontmatter")
	}
	paths, ok := fm["paths"]
	if !ok {
		t.Fatal("expected paths key")
	}
	list, ok := paths.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", paths)
	}
	if len(list) != 2 || list[0] != "src/**" || list[1] != "test/**" {
		t.Fatalf("unexpected paths: %v", list)
	}
	if body != "Body content\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestParseFrontmatter_无frontmatter(t *testing.T) {
	raw := "Just body\nNo frontmatter\n"
	fm, body := parseFrontmatter(raw)
	if len(fm) != 0 {
		t.Fatalf("expected empty frontmatter, got %v", fm)
	}
	if body != raw {
		t.Fatalf("expected body unchanged, got %q", body)
	}
}

func TestExtractIncludeSpec(t *testing.T) {
	tests := []struct {
		line     string
		expected string
	}{
		{"@include path/to/file.md", "path/to/file.md"},
		{"@path/to/file.md", "path/to/file.md"},
		{"@@not-an-include", ""},
		{"normal text", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractIncludeSpec(tt.line)
		if got != tt.expected {
			t.Errorf("extractIncludeSpec(%q) = %q, want %q", tt.line, got, tt.expected)
		}
	}
}

func TestParseFrontmatterValue_布尔(t *testing.T) {
	if parseFrontmatterValue("true") != true {
		t.Error("expected true")
	}
	if parseFrontmatterValue("false") != false {
		t.Error("expected false")
	}
}

func TestParseFrontmatterValue_内联列表(t *testing.T) {
	got := parseFrontmatterValue("[a, b, c]")
	list, ok := got.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", got)
	}
	if len(list) != 3 || list[0] != "a" || list[1] != "b" || list[2] != "c" {
		t.Fatalf("unexpected list: %v", list)
	}
}

func TestParseFrontmatterValue_null(t *testing.T) {
	if parseFrontmatterValue("null") != nil {
		t.Error("expected nil for null")
	}
	if parseFrontmatterValue("none") != nil {
		t.Error("expected nil for none")
	}
}

func TestParseFrontmatterValue_引号字符串(t *testing.T) {
	got := parseFrontmatterValue(`"hello"`)
	if got != "hello" {
		t.Fatalf("expected hello, got %v", got)
	}
	got = parseFrontmatterValue(`'world'`)
	if got != "world" {
		t.Fatalf("expected world, got %v", got)
	}
}

func TestDetectGitWorktree_非Git目录(t *testing.T) {
	tmpDir := t.TempDir()
	info := detectGitWorktree(context.Background(), tmpDir)
	// 非 git 目录应返回 nil
	if info != nil {
		t.Fatalf("expected nil for non-git directory, got %+v", info)
	}
}

func TestNormalizeAdditionalDirectories_环境变量(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv(AdditionalDirectoriesEnv, tmpDir)
	defer os.Unsetenv(AdditionalDirectoriesEnv)

	result := normalizeAdditionalDirectories(nil, "")
	if result == "" {
		t.Fatal("expected non-empty result from env var")
	}
}

func TestNormalizeAdditionalDirectories_显式目录(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := []string{tmpDir}
	result := normalizeAdditionalDirectories(dirs, "")
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestNormalizeAdditionalDirectories_相对路径(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := []string{"subdir"}
	result := normalizeAdditionalDirectories(dirs, tmpDir)
	if result == "" {
		t.Fatal("expected non-empty result with workspace resolution")
	}
}

func TestNormalizeAdditionalDirectories_去重(t *testing.T) {
	tmpDir := t.TempDir()
	dirs := []string{tmpDir, tmpDir}
	result := normalizeAdditionalDirectories(dirs, "")
	// 不应重复
	parts := filepath.SplitList(result)
	count := 0
	for _, p := range parts {
		if p == tmpDir {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 occurrence, got %d", count)
	}
}

func TestPathsMatchTarget_匹配(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("test"), 0o644)

	rulePath := filepath.Join(tmpDir, "UAPCLAWSWARM.md")
	srcDir := filepath.Join(tmpDir, "src")
	// paths: ["src/**"] 应匹配 src 目录
	globs := []string{"src/**"}
	if !pathsMatchTarget(globs, rulePath, srcDir) {
		t.Fatal("expected src/** to match src directory")
	}
}

func TestPathsMatchTarget_不匹配(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, "docs"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("test"), 0o644)

	rulePath := filepath.Join(tmpDir, "UAPCLAWSWARM.md")
	docsDir := filepath.Join(tmpDir, "docs")
	globs := []string{"src/**"}
	if pathsMatchTarget(globs, rulePath, docsDir) {
		t.Fatal("expected src/** to NOT match docs directory")
	}
}

func TestFrontmatterPathsMatch_无paths时始终匹配(t *testing.T) {
	fm := map[string]any{"name": "test"}
	if !frontmatterPathsMatch(fm, "/any/path", "/any/target") {
		t.Fatal("expected match when no paths key")
	}
}

func TestExpandIncludes_代码围栏内不展开(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "extra.md"), []byte("extra"), 0o644)

	body := "```\n@include extra.md\n```\nNormal line"
	out := &[]LoadedMemoryFile{}
	seen := map[string]bool{}
	watchPaths := map[string]bool{}

	result := expandIncludes(body, filepath.Join(tmpDir, "main.md"), "project", out, seen, tmpDir, watchPaths)
	// 围栏内的 @include 不应展开
	if strings.Contains(result, "extra content") {
		t.Fatal("@include inside code fence should NOT be expanded")
	}
}

func TestResolveIncludePath_绝对路径(t *testing.T) {
	result := resolveIncludePath("/absolute/path.md", "/some/current.md")
	if result != "/absolute/path.md" {
		t.Fatalf("expected /absolute/path.md, got %s", result)
	}
}

func TestResolveIncludePath_家目录(t *testing.T) {
	result := resolveIncludePath("~/test.md", "/some/current.md")
	if !strings.Contains(result, "test.md") {
		t.Fatalf("expected resolved home path, got %s", result)
	}
}

func TestSafeResolve(t *testing.T) {
	result := safeResolve(".")
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestIsRelativeTo(t *testing.T) {
	if !isRelativeTo("/home/user/project/file.md", "/home/user/project") {
		t.Fatal("expected true for subdirectory")
	}
	if isRelativeTo("/other/path/file.md", "/home/user/project") {
		t.Fatal("expected false for unrelated path")
	}
}

func TestShort(t *testing.T) {
	home := utilpath.UserHomeDir()
	if home == "" {
		t.Skip("no home dir")
	}
	path := home + "/test.md"
	result := short(path)
	if !strings.HasPrefix(result, "~") {
		t.Fatalf("expected ~ prefix, got %s", result)
	}
}

func TestShouldSkipProjectDir_nilWorktree(t *testing.T) {
	if shouldSkipProjectDir("/any", nil) {
		t.Fatal("expected false with nil worktree")
	}
}

func TestShouldSkipProjectDir_相同根(t *testing.T) {
	info := &GitWorktreeInfo{WorktreeRoot: "/a", CanonicalRoot: "/a"}
	if shouldSkipProjectDir("/any", info) {
		t.Fatal("expected false when roots are equal")
	}
}

func TestCanonicalRootFromCommonDir(t *testing.T) {
	// .git 目录
	if canonicalRootFromCommonDir("/project/.git") != "/project" {
		t.Fatal("expected /project for .git common dir")
	}
	// worktrees 目录
	if canonicalRootFromCommonDir("/project/.git/worktrees/abc") != "/project" {
		t.Fatal("expected /project for worktrees common dir")
	}
	// 未知格式
	if canonicalRootFromCommonDir("/something/else") != "" {
		t.Fatal("expected empty for unknown common dir")
	}
}

func TestDiscoverAndLoadMemoryFiles_额外目录(t *testing.T) {
	tmpDir := t.TempDir()
	extraDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(extraDir, "UAPCLAWSWARM.md"), []byte("Extra dir content"), 0o644)
	ClearProjectMemoryCache(tmpDir)

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(),tmpDir, tmpDir, []string{extraDir})
	var found bool
	for _, f := range files {
		if strings.Contains(f.Content, "Extra dir content") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected content from additional directory")
	}
}
