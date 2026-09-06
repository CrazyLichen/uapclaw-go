//go:build test

package shell

import (
	"regexp"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsReadOnlyCommand_bash只读 测试 bash 只读命令
func TestIsReadOnlyCommand_bash只读(t *testing.T) {
	if !IsReadOnlyCommand("grep foo bar", false) {
		t.Errorf("grep 应为只读命令")
	}
	if !IsReadOnlyCommand("ls -la", false) {
		t.Errorf("ls 应为只读命令")
	}
	if !IsReadOnlyCommand("cat file.txt", false) {
		t.Errorf("cat 应为只读命令")
	}
}

// TestIsReadOnlyCommand_bash非只读 测试 bash 非只读命令
func TestIsReadOnlyCommand_bash非只读(t *testing.T) {
	if IsReadOnlyCommand("rm file.txt", false) {
		t.Errorf("rm 不应为只读命令")
	}
	if IsReadOnlyCommand("python3 script.py", false) {
		t.Errorf("未知命令不应为只读命令")
	}
}

// TestIsReadOnlyCommand_ps只读 测试 PowerShell 只读命令
func TestIsReadOnlyCommand_ps只读(t *testing.T) {
	if !IsReadOnlyCommand("Get-ChildItem", true) {
		t.Errorf("Get-ChildItem 应为只读命令")
	}
}

// TestSplitPipeline_bash 测试 bash 管道拆分
func TestSplitPipeline_bash(t *testing.T) {
	parts := SplitPipeline("cat file | grep foo", false)
	if len(parts) != 2 {
		t.Errorf("应有 2 个管道段，got %d: %v", len(parts), parts)
	}
	parts = SplitPipeline("ls && cat file", false)
	if len(parts) != 2 {
		t.Errorf("&& 应拆分，got %d: %v", len(parts), parts)
	}
}

// TestSplitPipeline_ps 测试 PowerShell 管道拆分
func TestSplitPipeline_ps(t *testing.T) {
	parts := SplitPipeline("Get-ChildItem | Where-Object { $_ }", true)
	if len(parts) != 2 {
		t.Errorf("应有 2 个管道段，got %d: %v", len(parts), parts)
	}
}

// TestCompilePatterns 测试编译正则模式
func TestCompilePatterns(t *testing.T) {
	patterns := CompilePatterns([]string{"rm.*", "cat.*", "[invalid"})
	if len(patterns) != 2 {
		t.Errorf("有效模式应为 2，got %d", len(patterns))
	}
	if patterns[0].MatchString("rm -rf") == false {
		t.Errorf("第一个模式应匹配 rm -rf")
	}
}

// TestCompilePatterns_空列表 测试空列表
func TestCompilePatterns_空列表(t *testing.T) {
	patterns := CompilePatterns(nil)
	if patterns != nil {
		t.Errorf("空列表应返回 nil，got %v", patterns)
	}
	patterns = CompilePatterns([]string{})
	if patterns != nil {
		t.Errorf("空列表应返回 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestPsOperatorLengthAt 测试操作符长度
func TestPsOperatorLengthAt(t *testing.T) {
	tests := []struct {
		cmd      string
		index    int
		expected int
	}{
		{"a | b", 2, 1},  // |
		{"a || b", 2, 2}, // ||
		{"a ; b", 2, 1},  // ;
		{"a && b", 2, 2}, // &&
		{"abc", 0, 0},    // 普通字符
	}
	for _, tt := range tests {
		got := psOperatorLengthAt(tt.cmd, tt.index)
		if got != tt.expected {
			t.Errorf("psOperatorLengthAt(%q, %d) = %d, want %d", tt.cmd, tt.index, got, tt.expected)
		}
	}
}

// TestSplitBashPipeline 测试 bash 管道拆分
func TestSplitBashPipeline(t *testing.T) {
	tests := []struct {
		cmd  string
		want int
	}{
		{"cat file | grep foo", 2},
		{"ls && cat file", 2},
		{"a; b; c", 3},
		{"a || b", 2},
		{"simple", 1},
	}
	for _, tt := range tests {
		parts := splitBashPipeline(tt.cmd)
		if len(parts) != tt.want {
			t.Errorf("splitBashPipeline(%q) = %v (len=%d), want len=%d", tt.cmd, parts, len(parts), tt.want)
		}
	}
}

// TestSplitPSPipeline 测试 PowerShell 管道拆分
func TestSplitPSPipeline(t *testing.T) {
	parts := splitPSPipeline("Get-ChildItem | Where-Object { $_ }")
	if len(parts) != 2 {
		t.Errorf("简单管道应有 2 段，got %d: %v", len(parts), parts)
	}
	// 花括号内管道不应拆分
	parts = splitPSPipeline("Where-Object { $_ | foo }")
	if len(parts) != 1 {
		t.Errorf("花括号内管道不应拆分，got %d: %v", len(parts), parts)
	}
}

// TestCompilePatterns_大小写不敏感 测试编译模式大小写不敏感
func TestCompilePatterns_大小写不敏感(t *testing.T) {
	patterns := CompilePatterns([]string{"RM.*"})
	if len(patterns) != 1 {
		t.Fatalf("应有 1 个模式")
	}
	// (?i) 前缀使匹配大小写不敏感
	if !patterns[0].MatchString("rm -rf") {
		t.Errorf("模式应大小写不敏感匹配")
	}
	_ = regexp.MustCompile("") // 确保导入
}
