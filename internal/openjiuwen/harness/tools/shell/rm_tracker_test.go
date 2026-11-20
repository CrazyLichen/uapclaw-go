//go:build test

package shell

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestParseRmTargets_简单rm 测试简单 rm 命令
func TestParseRmTargets_简单rm(t *testing.T) {
	targets := ParseRmTargets("rm file1.txt file2.txt")
	if len(targets) != 2 {
		t.Fatalf("应有 2 个目标，got %d", len(targets))
	}
	if targets[0] != "file1.txt" || targets[1] != "file2.txt" {
		t.Errorf("目标不匹配，got %v", targets)
	}
}

// TestParseRmTargets_复合命令 测试复合命令返回空
func TestParseRmTargets_复合命令(t *testing.T) {
	for _, cmd := range []string{"rm a; ls", "rm a && ls", "rm a || ls", "rm a | cat"} {
		if targets := ParseRmTargets(cmd); targets != nil {
			t.Errorf("复合命令 %q 应返回 nil，got %v", cmd, targets)
		}
	}
}

// TestParseRmTargets_递归标志 测试递归标志
func TestParseRmTargets_递归标志(t *testing.T) {
	if targets := ParseRmTargets("rm -rf dir"); targets != nil {
		t.Errorf("递归删除应返回 nil，got %v", targets)
	}
	if targets := ParseRmTargets("rm -r dir"); targets != nil {
		t.Errorf("-r 标志应返回 nil，got %v", targets)
	}
}

// TestParseRmTargets_通配符跳过 测试通配符跳过
func TestParseRmTargets_通配符跳过(t *testing.T) {
	targets := ParseRmTargets("rm file.txt *.log")
	if len(targets) != 1 || targets[0] != "file.txt" {
		t.Errorf("通配符应跳过，got %v", targets)
	}
}

// TestParseRmTargets_非rm命令 测试非 rm 命令
func TestParseRmTargets_非rm命令(t *testing.T) {
	if targets := ParseRmTargets("ls -la"); targets != nil {
		t.Errorf("非 rm 命令应返回 nil，got %v", targets)
	}
}

// TestParseRmTargets_路径前缀rm 测试带路径前缀的 rm
func TestParseRmTargets_路径前缀rm(t *testing.T) {
	targets := ParseRmTargets("/usr/bin/rm file.txt")
	if len(targets) != 1 || targets[0] != "file.txt" {
		t.Errorf("路径前缀 rm 应正常解析，got %v", targets)
	}
}

// TestParseRmTargets_仅标志 测试仅标志无目标
func TestParseRmTargets_仅标志(t *testing.T) {
	targets := ParseRmTargets("rm -f")
	if len(targets) != 0 {
		t.Errorf("仅标志无目标应返回空切片，got %v", targets)
	}
}

// TestParsePSRemoveTargets_RemoveItem 测试 Remove-Item 命令
func TestParsePSRemoveTargets_RemoveItem(t *testing.T) {
	targets := ParsePSRemoveTargets("remove-item file.txt")
	if len(targets) != 1 || targets[0] != "file.txt" {
		t.Errorf("Remove-Item 应解析目标，got %v", targets)
	}
}

// TestParsePSRemoveTargets_别名rm 测试 rm 别名
func TestParsePSRemoveTargets_别名rm(t *testing.T) {
	targets := ParsePSRemoveTargets("rm file.txt")
	if len(targets) != 1 || targets[0] != "file.txt" {
		t.Errorf("rm 别名应解析目标，got %v", targets)
	}
}

// TestParsePSRemoveTargets_递归删除 测试递归删除返回空
func TestParsePSRemoveTargets_递归删除(t *testing.T) {
	if targets := ParsePSRemoveTargets("remove-item -Recurse dir"); targets != nil {
		t.Errorf("递归删除应返回 nil，got %v", targets)
	}
}

// TestParsePSRemoveTargets_复合命令 测试复合命令
func TestParsePSRemoveTargets_复合命令(t *testing.T) {
	if targets := ParsePSRemoveTargets("rm file; ls"); targets != nil {
		t.Errorf("复合命令应返回 nil，got %v", targets)
	}
}

// TestParsePSRemoveTargets_非删除命令 测试非删除命令
func TestParsePSRemoveTargets_非删除命令(t *testing.T) {
	if targets := ParsePSRemoveTargets("Get-ChildItem"); targets != nil {
		t.Errorf("非删除命令应返回 nil，got %v", targets)
	}
}

// TestParsePSRemoveTargets_通配符跳过 测试通配符跳过
func TestParsePSRemoveTargets_通配符跳过(t *testing.T) {
	targets := ParsePSRemoveTargets("rm file.txt *.log")
	if len(targets) != 1 || targets[0] != "file.txt" {
		t.Errorf("通配符应跳过，got %v", targets)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestShlexSplit 测试 shell 词法拆分
func TestShlexSplit(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"rm file.txt", []string{"rm", "file.txt"}},
		{"rm 'file with space'", []string{"rm", "file with space"}},
		{`rm "file with space"`, []string{"rm", "file with space"}},
		{`rm file\ with\ space`, []string{"rm", "file with space"}},
		{"  rm   file.txt  ", []string{"rm", "file.txt"}},
	}
	for _, tt := range tests {
		got, err := shlexSplit(tt.in)
		if err != nil {
			t.Errorf("shlexSplit(%q) 出错: %v", tt.in, err)
		}
		if len(got) != len(tt.want) {
			t.Errorf("shlexSplit(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("shlexSplit(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

// TestStripValuelessFlags 测试去除无值标志
func TestStripValuelessFlags(t *testing.T) {
	result := stripValuelessFlags("file.txt -Force -Verbose")
	if result != "file.txt" {
		t.Errorf("got %q, want %q", result, "file.txt")
	}
	result = stripValuelessFlags("file.txt")
	if result != "file.txt" {
		t.Errorf("无标志时应保留原样，got %q", result)
	}
}

// TestStripErrorAction 测试去除 ErrorAction
func TestStripErrorAction(t *testing.T) {
	result := stripErrorAction("file.txt -ErrorAction Stop")
	if result != "file.txt" {
		t.Errorf("got %q, want %q", result, "file.txt")
	}
}
