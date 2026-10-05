package filesystem

import (
	"runtime"
	"testing"
)

// ──────────────────────────── shellQuote 测试 ────────────────────────────

// TestShellQuote_简单字符串 测试无特殊字符的引号包裹
func TestShellQuote_简单字符串(t *testing.T) {
	got := shellQuote("hello")
	want := "'hello'"
	if got != want {
		t.Errorf("shellQuote(%q) = %q, want %q", "hello", got, want)
	}
}

// TestShellQuote_含单引号 测试含单引号的 POSIX 引号转义
func TestShellQuote_含单引号(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows: ' 替换为 ''
		got := shellQuote("it's")
		want := "'it''s'"
		if got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", "it's", got, want)
		}
	} else {
		// POSIX: ' 替换为 '\''
		got := shellQuote("it's")
		want := "'it'\\''s'"
		if got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", "it's", got, want)
		}
	}
}

// TestShellQuote_空字符串 测试空字符串引号包裹
func TestShellQuote_空字符串(t *testing.T) {
	got := shellQuote("")
	want := "''"
	if got != want {
		t.Errorf("shellQuote(%q) = %q, want %q", "", got, want)
	}
}

// TestShellQuote_含空格 测试含空格的字符串
func TestShellQuote_含空格(t *testing.T) {
	got := shellQuote("hello world")
	want := "'hello world'"
	if got != want {
		t.Errorf("shellQuote(%q) = %q, want %q", "hello world", got, want)
	}
}

// ──────────────────────────── splitGlobPatterns 测试 ────────────────────────────

// TestSplitGlobPatterns_空字符串 测试空输入返回 nil
func TestSplitGlobPatterns_空字符串(t *testing.T) {
	got := splitGlobPatterns("")
	if got != nil {
		t.Errorf("splitGlobPatterns(\"\") = %v, want nil", got)
	}
}

// TestSplitGlobPatterns_单个模式 测试单个 glob 模式
func TestSplitGlobPatterns_单个模式(t *testing.T) {
	got := splitGlobPatterns("*.go")
	if len(got) != 1 || got[0] != "*.go" {
		t.Errorf("splitGlobPatterns(\"*.go\") = %v, want [*.go]", got)
	}
}

// TestSplitGlobPatterns_逗号分隔 测试逗号分隔的多个模式
func TestSplitGlobPatterns_逗号分隔(t *testing.T) {
	got := splitGlobPatterns("*.go,*.ts")
	if len(got) != 2 || got[0] != "*.go" || got[1] != "*.ts" {
		t.Errorf("splitGlobPatterns(\"*.go,*.ts\") = %v, want [*.go *.ts]", got)
	}
}

// TestSplitGlobPatterns_空格分隔 测试空格分隔的多个模式
func TestSplitGlobPatterns_空格分隔(t *testing.T) {
	got := splitGlobPatterns("*.go *.ts")
	if len(got) != 2 || got[0] != "*.go" || got[1] != "*.ts" {
		t.Errorf("splitGlobPatterns(\"*.go *.ts\") = %v, want [*.go *.ts]", got)
	}
}

// TestSplitGlobPatterns_花括号模式 测试花括号模式不拆分
func TestSplitGlobPatterns_花括号模式(t *testing.T) {
	got := splitGlobPatterns("{a,b}")
	if len(got) != 1 || got[0] != "{a,b}" {
		t.Errorf("splitGlobPatterns(\"{a,b}\") = %v, want [{a,b}]", got)
	}
}

// TestSplitGlobPatterns_混合模式 测试混合花括号和逗号模式
func TestSplitGlobPatterns_混合模式(t *testing.T) {
	// 空格分隔：*.go 和 {a,b} 各为独立模式
	got := splitGlobPatterns("*.go {a,b}")
	if len(got) != 2 || got[0] != "*.go" || got[1] != "{a,b}" {
		t.Errorf("splitGlobPatterns(\"*.go {a,b}\") = %v, want [*.go {a,b}]", got)
	}
}

// ──────────────────────────── intPtrOrNil 测试 ────────────────────────────

// TestIntPtrOrNil_零值 测试零值返回 nil
func TestIntPtrOrNil_零值(t *testing.T) {
	got := intPtrOrNil(0)
	if got != nil {
		t.Errorf("intPtrOrNil(0) = %v, want nil", got)
	}
}

// TestIntPtrOrNil_非零值 测试非零值返回指针
func TestIntPtrOrNil_非零值(t *testing.T) {
	got := intPtrOrNil(5)
	if got == nil {
		t.Fatal("intPtrOrNil(5) = nil, want non-nil")
	}
	if *got != 5 {
		t.Errorf("intPtrOrNil(5) = %d, want 5", *got)
	}
}

// TestIntPtrOrNil_负值 测试负值返回指针
func TestIntPtrOrNil_负值(t *testing.T) {
	got := intPtrOrNil(-3)
	if got == nil {
		t.Fatal("intPtrOrNil(-3) = nil, want non-nil")
	}
	if *got != -3 {
		t.Errorf("intPtrOrNil(-3) = %d, want -3", *got)
	}
}

// ──────────────────────────── applyHeadLimit 测试 ────────────────────────────

// TestApplyHeadLimit_默认限制 测试使用默认限制
func TestApplyHeadLimit_默认限制(t *testing.T) {
	items := make([]string, 300)
	for i := range items {
		items[i] = "line"
	}
	result, applied := applyHeadLimit(items, nil, 0)
	if len(result) != grepDefaultHeadLimit {
		t.Errorf("默认限制应返回 %d 行，实际 %d", grepDefaultHeadLimit, len(result))
	}
	if applied == nil {
		t.Error("截断时 appliedLimit 不应为 nil")
	}
}

// TestApplyHeadLimit_自定义限制 测试自定义限制
func TestApplyHeadLimit_自定义限制(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	limit := 3
	result, applied := applyHeadLimit(items, &limit, 0)
	if len(result) != 3 {
		t.Errorf("自定义限制 3 应返回 3 行，实际 %d", len(result))
	}
	if applied == nil {
		t.Error("截断时 appliedLimit 不应为 nil")
	}
}

// TestApplyHeadLimit_不截断 测试结果不超出限制
func TestApplyHeadLimit_不截断(t *testing.T) {
	items := []string{"a", "b"}
	limit := 10
	result, applied := applyHeadLimit(items, &limit, 0)
	if len(result) != 2 {
		t.Errorf("应返回全部 2 行，实际 %d", len(result))
	}
	if applied != nil {
		t.Error("不截断时 appliedLimit 应为 nil")
	}
}

// TestApplyHeadLimit_带偏移 测试带偏移的分页
func TestApplyHeadLimit_带偏移(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	limit := 2
	result, applied := applyHeadLimit(items, &limit, 2)
	if len(result) != 2 {
		t.Errorf("偏移 2 + 限制 2 应返回 2 行，实际 %d", len(result))
	}
	if result[0] != "c" || result[1] != "d" {
		t.Errorf("偏移 2 应从 c 开始，实际 %v", result)
	}
	if applied == nil {
		t.Error("截断时 appliedLimit 不应为 nil")
	}
}

// TestApplyHeadLimit_偏移超出 测试偏移接近范围边界
func TestApplyHeadLimit_偏移超出(t *testing.T) {
	items := []string{"a", "b"}
	// 偏移等于长度，返回空切片
	result, _ := applyHeadLimit(items, nil, 2)
	if len(result) != 0 {
		t.Errorf("偏移等于长度应返回 0 行，实际 %d", len(result))
	}
}

// TestApplyHeadLimit_负偏移 测试负偏移修正为零
func TestApplyHeadLimit_负偏移(t *testing.T) {
	items := []string{"a", "b", "c"}
	result, _ := applyHeadLimit(items, nil, -1)
	if result[0] != "a" {
		t.Errorf("负偏移应修正为 0，实际首行 %q", result[0])
	}
}

// TestApplyHeadLimit_限制零值 测试限制为 0 时不做截断
func TestApplyHeadLimit_限制零值(t *testing.T) {
	items := []string{"a", "b", "c"}
	limit := 0
	result, applied := applyHeadLimit(items, &limit, 0)
	if len(result) != 3 {
		t.Errorf("限制 0 表示不截断，应返回全部 3 行，实际 %d", len(result))
	}
	if applied != nil {
		t.Error("限制 0 时 appliedLimit 应为 nil")
	}
}

// ──────────────────────────── extractFilePathFromLine 测试 ────────────────────────────

// TestExtractFilePathFromLine_空行 测试空行返回空字符串
func TestExtractFilePathFromLine_空行(t *testing.T) {
	got := extractFilePathFromLine("", "content")
	if got != "" {
		t.Errorf("空行应返回空字符串，实际 %q", got)
	}
}

// TestExtractFilePathFromLine_filesWithMatches 测试 files_with_matches 模式
func TestExtractFilePathFromLine_filesWithMatches(t *testing.T) {
	got := extractFilePathFromLine("/path/to/file.go", "files_with_matches")
	if got != "/path/to/file.go" {
		t.Errorf("files_with_matches 模式应返回原行，实际 %q", got)
	}
}

// TestExtractFilePathFromLine_count 测试 count 模式
func TestExtractFilePathFromLine_count(t *testing.T) {
	got := extractFilePathFromLine("/path/to/file.go:5", "count")
	if got != "/path/to/file.go" {
		t.Errorf("count 模式应提取冒号前路径，实际 %q", got)
	}
}

// TestExtractFilePathFromLine_content 测试 content 模式
func TestExtractFilePathFromLine_content(t *testing.T) {
	got := extractFilePathFromLine("/path/to/file.go:10:hello world", "content")
	if got != "/path/to/file.go" {
		t.Errorf("content 模式应提取文件路径，实际 %q", got)
	}
}

// TestExtractFilePathFromLine_content无行号 测试 content 模式无行号
func TestExtractFilePathFromLine_content无行号(t *testing.T) {
	got := extractFilePathFromLine("/path/to/file.go:", "content")
	if got != "/path/to/file.go" {
		t.Errorf("content 模式应提取冒号前路径，实际 %q", got)
	}
}

// ──────────────────────────── relativizeLine 测试 ────────────────────────────

// TestRelativizeLine_filesWithMatches 测试 files_with_matches 模式
func TestRelativizeLine_filesWithMatches(t *testing.T) {
	got := relativizeLine("/home/user/project/file.go", "/home/user/project", "files_with_matches")
	if got != "file.go" {
		t.Errorf("files_with_matches 模式应返回相对路径，实际 %q", got)
	}
}

// TestRelativizeLine_content 测试 content 模式
func TestRelativizeLine_content(t *testing.T) {
	got := relativizeLine("/home/user/project/file.go:10:hello", "/home/user/project", "content")
	if got != "file.go:10:hello" {
		t.Errorf("content 模式应替换路径前缀，实际 %q", got)
	}
}

// TestRelativizeLine_空路径 测试空提取路径返回原行
func TestRelativizeLine_空路径(t *testing.T) {
	got := relativizeLine("", "/home/user/project", "content")
	if got != "" {
		t.Errorf("空行应返回空字符串，实际 %q", got)
	}
}
