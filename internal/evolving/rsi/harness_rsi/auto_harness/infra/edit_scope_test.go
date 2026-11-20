package infra

import (
	"context"
	"testing"
)

// TestEditScope
func TestEditScope(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		path string
		want bool
	}{
		{"openjiuwen/core/foo.py", true},
		{"jiuwenswarm/agents/harness/README.md", true},
		{"openjiuwen/harness/cli/README.md", true},
		{"tests/unit_tests/test_x.py", true},
		{"examples/demo.py", true},
		{"docs/en/guide.md", true},
		{"docs/zh/指南.md", true},
		{"docs/misc/x.md", false},
		{"openjiuwen/rsi/harness_rsi/auto_harness/schema.py", false},
		{"random/file.py", false},
	}
	for _, c := range cases {
		if got := IsAllowedRepoEditPath(ctx, c.path); got != c.want {
			t.Fatalf("IsAllowedRepoEditPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// TestNormalizeRepoPath
func TestNormalizeRepoPath(t *testing.T) {
	ctx := context.Background()
	// 非 cwd 语义直接透传（无项目根上下文时按绝对路径归一化）
	got := NormalizeRepoPath(ctx, "  ")
	if got != "" {
		t.Fatalf("空路径 = %q", got)
	}
	got = NormalizeRepoPath(ctx, "~/x")
	if got == "" || got == "~" {
		t.Fatalf("expanduser = %q", got)
	}
}

// TestRenderEditScope
func TestRenderEditScope(t *testing.T) {
	out := RenderEditScope("")
	if !containsStr([]string{out}, "本轮允许变更范围") ||
		!containsStr([]string{out}, "jiuwenswarm/**") ||
		!containsStr([]string{out}, "docs/en/") ||
		!containsStr([]string{out}, "不要自行越界") {
		t.Fatalf("RenderEditScope = %q", out)
	}
	if out2 := RenderEditScope("自定义头"); !containsStr([]string{out2}, "自定义头") {
		t.Fatalf("自定义头未生效")
	}
}

// TestCommitScope_Documentation
func TestCommitScope_Documentation(t *testing.T) {
	if !IsDocumentationFile("docs/en/a.md") {
		t.Fatal("docs markdown 应命中")
	}
	if IsDocumentationFile("docs/a.txt") || IsDocumentationFile("src/a.md") {
		t.Fatal("非 docs markdown 不应命中")
	}
	// 反斜杠归一化
	if !IsDocumentationFile("docs\\en\\a.md") {
		t.Fatal("反斜杠路径应归一化命中")
	}
}

// TestDeriveTestFiles
func TestDeriveTestFiles(t *testing.T) {
	files := DeriveTestFiles([]string{
		"openjiuwen/core/foo.py",
		"openjiuwen/core/__init__.py",
		"tests/unit_tests/test_x.py",
		"src/main.go",
		"",
	})
	want := []string{
		"tests/unit_tests/**/test_foo.py",
		"tests/system_tests/**/test_foo.py",
	}
	if len(files) != len(want) {
		t.Fatalf("derived = %v", files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("derived[%d] = %q, want %q", i, files[i], want[i])
		}
	}
	// 去重
	files2 := DeriveTestFiles([]string{"a/b.py", "a/b.py"})
	if len(files2) != 2 {
		t.Fatalf("去重失败 = %v", files2)
	}
}

// TestIsDerivedTestFile
func TestIsDerivedTestFile(t *testing.T) {
	sources := []string{"openjiuwen/core/foo.py", "openjiuwen/core/__init__.py", "tests/test_foo.py"}
	if !IsDerivedTestFile(sources, "tests/unit_tests/x/test_foo.py") {
		t.Fatal("匹配源→测试应命中")
	}
	if IsDerivedTestFile(sources, "tests/test_bar.py") {
		t.Fatal("不匹配不应命中")
	}
	if IsDerivedTestFile(sources, "openjiuwen/core/foo.py") {
		t.Fatal("非 tests/ 不应命中")
	}
}

// TestExtractVerifyRelatedFiles
func TestExtractVerifyRelatedFiles(t *testing.T) {
	ci := map[string]any{
		"errors": "failed tests/unit_tests/test_a.py ok",
		"gates": []any{
			map[string]any{"output": "see tests/system_tests/test_b.py:12"},
		},
	}
	files := ExtractVerifyRelatedFiles(ci, "also tests/unit_tests/test_a.py and tests/unit_tests/test_c.py")
	want := []string{
		"tests/unit_tests/test_a.py",
		"tests/system_tests/test_b.py",
		"tests/unit_tests/test_c.py",
	}
	if len(files) != len(want) {
		t.Fatalf("files = %v", files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files[%d] = %q, want %q", i, files[i], want[i])
		}
	}
	if got := ExtractVerifyRelatedFiles(nil, ""); len(got) != 0 {
		t.Fatalf("空输入 = %v", got)
	}
}

// TestDeriveLegacyRelatedTestFiles
func TestDeriveLegacyRelatedTestFiles(t *testing.T) {
	edited := []string{"tests/unit_tests/test_a.py", "tests/test_b.py", "src/x.py"}
	verify := []string{"tests/unit_tests/test_a.py ", "tests/unit_tests/test_c.py"}
	got := DeriveLegacyRelatedTestFiles(edited, verify)
	if len(got) != 1 || got[0] != "tests/unit_tests/test_a.py" {
		t.Fatalf("legacy = %v", got)
	}
}
