package filesystem

import (
	"path/filepath"
	"strings"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// intp 返回整型指针，便于构造可空参数。
func intp(v int) *int { return &v }

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestShellQuote 验证 Shell 引号包裹与内部单引号转义。
func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b")
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") {
		t.Fatalf("结果应以单引号包裹，实际 %q", got)
	}
	if !strings.Contains(got, "'\\''") {
		t.Fatalf("内部单引号应转义为 '\\''，实际 %q", got)
	}
	if got := shellQuote("abc"); got != "'abc'" {
		t.Fatalf("普通值 = %q, 期望 'abc'", got)
	}
}

// TestSplitGlobPatterns 验证 glob 模式拆分（花括号保留、逗号拆分、空项丢弃）。
func TestSplitGlobPatterns(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"空串返回 nil", "", nil},
		{"单模式", "*.go", []string{"*.go"}},
		{"逗号拆分", "*.go,*.md", []string{"*.go", "*.md"}},
		{"空白分隔", "*.go *.md", []string{"*.go", "*.md"}},
		{"花括号整体保留", "*.{go,md}", []string{"*.{go,md}"}},
		{"逗号空项丢弃", "*.go,,*.md", []string{"*.go", "*.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitGlobPatterns(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitGlobPatterns(%q) = %v, 期望 %v", tt.in, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("第 %d 项 = %q, 期望 %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestIntPtrOrNil 验证零值转 nil、非零转为指针。
func TestIntPtrOrNil(t *testing.T) {
	if got := intPtrOrNil(0); got != nil {
		t.Fatalf("0 应转 nil，实际 %v", *got)
	}
	if got := intPtrOrNil(7); got == nil || *got != 7 {
		t.Fatalf("7 应转 *7，实际 %v", got)
	}
}

// TestBuildRgCommand 验证 ripgrep 命令拼装的关键分支。
func TestBuildRgCommand(t *testing.T) {
	base := func(mode string) string {
		return buildRgCommand("pat", "/tmp/x", "", mode, nil, nil, nil, nil, true, false, "", false)
	}

	t.Run("files_with_matches 用 -l", func(t *testing.T) {
		cmd := base("files_with_matches")
		if !strings.Contains(cmd, " -l") {
			t.Fatalf("应含 -l，实际 %q", cmd)
		}
	})

	t.Run("count 用 -c", func(t *testing.T) {
		if cmd := base("count"); !strings.Contains(cmd, " -c") {
			t.Fatalf("应含 -c，实际 %q", cmd)
		}
	})

	t.Run("content 且显示行号用 -n", func(t *testing.T) {
		if cmd := base("content"); !strings.Contains(cmd, " -n") {
			t.Fatalf("应含 -n，实际 %q", cmd)
		}
	})

	t.Run("content 且不显示行号无 -n", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "", "content", nil, nil, nil, nil, false, false, "", false)
		if strings.Contains(cmd, " -n ") || strings.HasSuffix(cmd, " -n") {
			t.Fatalf("不应含 -n，实际 %q", cmd)
		}
	})

	t.Run("大小写不敏感加 -i", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "", "content", nil, nil, nil, nil, true, true, "", false)
		if !strings.Contains(cmd, " -i") {
			t.Fatalf("应含 -i，实际 %q", cmd)
		}
	})

	t.Run("multiline 加 -U 与 dotall", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "", "content", nil, nil, nil, nil, true, false, "", true)
		if !strings.Contains(cmd, "-U") || !strings.Contains(cmd, "--multiline-dotall") {
			t.Fatalf("应含 multiline 标志，实际 %q", cmd)
		}
	})

	t.Run("context 优先于 contextC", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "", "content", nil, nil, intp(9), intp(3), true, false, "", false)
		if !strings.Contains(cmd, "-C 3") {
			t.Fatalf("应使用 context=3，实际 %q", cmd)
		}
	})

	t.Run("contextC 在 context 缺失时生效", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "", "content", nil, nil, intp(9), nil, true, false, "", false)
		if !strings.Contains(cmd, "-C 9") {
			t.Fatalf("应使用 contextC=9，实际 %q", cmd)
		}
	})

	t.Run("contextBefore/After 分别拼装", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "", "content", intp(2), intp(4), nil, nil, true, false, "", false)
		if !strings.Contains(cmd, "-B 2") || !strings.Contains(cmd, "-A 4") {
			t.Fatalf("应含 -B 2 与 -A 4，实际 %q", cmd)
		}
	})

	t.Run("fileType 与 glob 生效", func(t *testing.T) {
		cmd := buildRgCommand("pat", "/tmp/x", "*.go,*.md", "content", nil, nil, nil, nil, true, false, "go", false)
		if !strings.Contains(cmd, "--type 'go'") {
			t.Fatalf("应含 --type，实际 %q", cmd)
		}
		if !strings.Contains(cmd, "--glob '*.go'") || !strings.Contains(cmd, "--glob '*.md'") {
			t.Fatalf("应含两条 glob，实际 %q", cmd)
		}
	})

	t.Run("以短横线开头的模式用 -e", func(t *testing.T) {
		cmd := buildRgCommand("-foo", "/tmp/x", "", "content", nil, nil, nil, nil, true, false, "", false)
		if !strings.Contains(cmd, "-e '-foo'") {
			t.Fatalf("应使用 -e 传递模式，实际 %q", cmd)
		}
	})

	t.Run("排除 VCS 目录", func(t *testing.T) {
		cmd := base("content")
		if !strings.Contains(cmd, "--glob '!.git'") {
			t.Fatalf("应排除 .git，实际 %q", cmd)
		}
	})
}

// TestBuildGrepCommand 验证 grep 命令拼装。
func TestBuildGrepCommand(t *testing.T) {
	t.Run("multiline 不支持返回空串", func(t *testing.T) {
		cmd := buildGrepCommand("p", "/tmp", "", "content", nil, nil, nil, nil, true, false, true)
		if cmd != "" {
			t.Fatalf("multiline 应返回空串，实际 %q", cmd)
		}
	})

	t.Run("content 模式基本拼装", func(t *testing.T) {
		cmd := buildGrepCommand("pat", "/tmp/x", "*.go", "content", nil, nil, nil, nil, true, false, false)
		if !strings.HasPrefix(cmd, "grep -R") {
			t.Fatalf("应以 grep -R 开头，实际 %q", cmd)
		}
		if !strings.Contains(cmd, "--include='*.go'") {
			t.Fatalf("应含 include，实际 %q", cmd)
		}
		if !strings.HasSuffix(cmd, "'pat' '/tmp/x'") {
			t.Fatalf("结尾应为模式与路径，实际 %q", cmd)
		}
	})

	t.Run("count 与 files_with_matches 模式", func(t *testing.T) {
		if cmd := buildGrepCommand("p", "/t", "", "count", nil, nil, nil, nil, true, false, false); !strings.Contains(cmd, " -c") {
			t.Fatalf("应含 -c，实际 %q", cmd)
		}
		if cmd := buildGrepCommand("p", "/t", "", "files_with_matches", nil, nil, nil, nil, true, false, false); !strings.Contains(cmd, " -l") {
			t.Fatalf("应含 -l，实际 %q", cmd)
		}
	})

	t.Run("context 组合", func(t *testing.T) {
		cmd := buildGrepCommand("p", "/t", "", "content", intp(1), intp(2), nil, nil, true, false, false)
		if !strings.Contains(cmd, "-B 1") || !strings.Contains(cmd, "-A 2") {
			t.Fatalf("应含 -B/-A，实际 %q", cmd)
		}
	})
}

// TestApplyHeadLimit 验证 head_limit 截断语义。
func TestApplyHeadLimit(t *testing.T) {
	items := []string{"a", "b", "c", "d"}

	t.Run("limit 为 0 表示不限并清空 limit", func(t *testing.T) {
		got, lim := applyHeadLimit(items, intp(0), 0)
		if len(got) != 4 || lim != nil {
			t.Fatalf("limit=0 应全量返回且清空 limit，实际 %v %v", got, lim)
		}
	})

	t.Run("负 offset 归一为 0", func(t *testing.T) {
		got, _ := applyHeadLimit(items, intp(2), -5)
		if len(got) != 2 || got[0] != "a" {
			t.Fatalf("offset 应归一为 0，实际 %v", got)
		}
	})

	t.Run("未超限时不标记截断", func(t *testing.T) {
		got, lim := applyHeadLimit(items, intp(10), 0)
		if len(got) != 4 || lim != nil {
			t.Fatalf("未超限应无截断标记，实际 %v %v", got, lim)
		}
	})

	t.Run("超限时返回 appliedLimit", func(t *testing.T) {
		got, lim := applyHeadLimit(items, intp(2), 0)
		if len(got) != 2 || lim == nil || *lim != 2 {
			t.Fatalf("超限应返回 appliedLimit=2，实际 %v %v", got, lim)
		}
	})

	t.Run("limit 为 nil 时用默认上限", func(t *testing.T) {
		got, lim := applyHeadLimit(items, nil, 0)
		if len(got) != 4 || lim != nil {
			t.Fatalf("小集合不应截断，实际 %v %v", got, lim)
		}
	})

	t.Run("offset 超出长度返回空", func(t *testing.T) {
		got, _ := applyHeadLimit(items, intp(2), 10)
		if len(got) != 0 {
			t.Fatalf("offset 越界应返回空，实际 %v", got)
		}
	})
}

// TestExtractFilePathFromLine 验证按输出模式提取文件路径。
func TestExtractFilePathFromLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		mode string
		want string
	}{
		{"空行返回空", "", "content", ""},
		{"files_with_matches 整行即路径", "/a/b.go", "files_with_matches", "/a/b.go"},
		{"count 取最后一个冒号前", "/a/b.go:12", "count", "/a/b.go"},
		{"count 无冒号返回空", "/a/b.go", "count", ""},
		{"content 正则提取", "/a/b.go:12:hit", "content", "/a/b.go"},
		{"content 无行号时取首个冒号前", "/a/b.go:hit", "content", "/a/b.go"},
		{"content 无冒号返回空", "plain", "content", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractFilePathFromLine(tt.line, tt.mode); got != tt.want {
				t.Fatalf("extractFilePathFromLine(%q, %q) = %q, 期望 %q", tt.line, tt.mode, got, tt.want)
			}
		})
	}
}

// TestRelativizeLine 验证输出行路径相对化。
func TestRelativizeLine(t *testing.T) {
	base := filepath.Join("/work", "repo")

	t.Run("files_with_matches 整行替换为相对路径", func(t *testing.T) {
		got := relativizeLine(filepath.Join(base, "a.go"), base, "files_with_matches")
		if got != "a.go" {
			t.Fatalf("相对路径 = %q, 期望 a.go", got)
		}
	})

	t.Run("content 保留行号与内容", func(t *testing.T) {
		line := filepath.Join(base, "a.go") + ":12:hit"
		got := relativizeLine(line, base, "content")
		if got != "a.go:12:hit" {
			t.Fatalf("相对化结果 = %q, 期望 a.go:12:hit", got)
		}
	})

	t.Run("无法提取路径时原样返回", func(t *testing.T) {
		if got := relativizeLine("plain", base, "content"); got != "plain" {
			t.Fatalf("应原样返回，实际 %q", got)
		}
	})

	t.Run("路径不在 base 下时返回带 .. 的相对路径", func(t *testing.T) {
		got := relativizeLine("/other/a.go", base, "files_with_matches")
		if !strings.HasPrefix(got, "..") {
			t.Fatalf("应产生 .. 前缀，实际 %q", got)
		}
	})
}

// TestBuildStructuredOutput 验证三种输出模式的结构化结果。
func TestBuildStructuredOutput(t *testing.T) {
	base := "/work/repo"

	t.Run("content 模式统计行数", func(t *testing.T) {
		stdout := base + "/a.go:1:hit\n" + base + "/a.go:2:hit2\n"
		data := buildStructuredOutput(stdout, "", 0, "content", nil, 0, base)
		if data["numLines"] != 2 || data["count"] != 2 {
			t.Fatalf("应统计 2 行，实际 %v", data)
		}
		if lines, _ := data["filenames"].([]string); len(lines) != 0 {
			t.Fatalf("content 模式 filenames 应为空，实际 %v", lines)
		}
	})

	t.Run("count 模式汇总匹配数与文件数", func(t *testing.T) {
		stdout := base + "/a.go:3\n" + base + "/b.go:2\n"
		data := buildStructuredOutput(stdout, "", 0, "count", nil, 0, base)
		if data["numMatches"] != 5 || data["numFiles"] != 2 {
			t.Fatalf("应汇总 5 匹配 / 2 文件，实际 %v", data)
		}
	})

	t.Run("count 模式忽略无冒号行", func(t *testing.T) {
		data := buildStructuredOutput("garbage\n", "", 0, "count", nil, 0, base)
		if data["numMatches"] != 0 || data["numFiles"] != 0 {
			t.Fatalf("无冒号行不应计数，实际 %v", data)
		}
	})

	t.Run("files_with_matches 模式返回文件名列表", func(t *testing.T) {
		stdout := base + "/a.go\n" + base + "/b.go\n"
		data := buildStructuredOutput(stdout, "", 0, "files_with_matches", nil, 0, base)
		files, _ := data["filenames"].([]string)
		if len(files) != 2 || files[0] != "a.go" {
			t.Fatalf("文件名列表 = %v", files)
		}
		if data["numFiles"] != 2 {
			t.Fatalf("numFiles 应为 2，实际 %v", data["numFiles"])
		}
	})

	t.Run("offset 与 limit 写入结果", func(t *testing.T) {
		stdout := base + "/a.go:1:x\n" + base + "/a.go:2:y\n" + base + "/a.go:3:z\n"
		data := buildStructuredOutput(stdout, "", 0, "content", intp(1), 1, base)
		if data["appliedOffset"] != 1 {
			t.Fatalf("appliedOffset 应为 1，实际 %v", data["appliedOffset"])
		}
		if data["appliedLimit"] != 1 {
			t.Fatalf("appliedLimit 应为 1，实际 %v", data["appliedLimit"])
		}
	})

	t.Run("空输出产生零行", func(t *testing.T) {
		data := buildStructuredOutput("\n\n", "", 0, "content", nil, 0, base)
		if data["numLines"] != 0 {
			t.Fatalf("空输出应为 0 行，实际 %v", data["numLines"])
		}
	})

	t.Run("stderr 与 exit_code 透传", func(t *testing.T) {
		data := buildStructuredOutput("", "boom", 2, "content", nil, 0, base)
		if data["stderr"] != "boom" || data["exit_code"] != 2 {
			t.Fatalf("应透传 stderr/exit_code，实际 %v", data)
		}
	})
}
