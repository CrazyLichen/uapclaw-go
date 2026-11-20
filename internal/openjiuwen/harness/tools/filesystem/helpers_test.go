//go:build test

package filesystem

import (
	"strings"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExpandBracePattern_无花括号 测试无花括号模式
func TestExpandBracePattern_无花括号(t *testing.T) {
	result := ExpandBracePattern("*.go")
	if len(result) != 1 || result[0] != "*.go" {
		t.Errorf("无花括号应返回原模式，got %v", result)
	}
}

// TestExpandBracePattern_简单花括号 测试简单花括号模式
// 此前 expandGroup 索引错位（capture group 边界 vs 整个匹配边界）导致
// 花括号残留与无限递归，两个 Skip 桩在修复后启用真实断言。
// 对齐 Python: _expand_brace_pattern("*.{py,js}") == ["*.py", "*.js"]
func TestExpandBracePattern_简单花括号(t *testing.T) {
	result := ExpandBracePattern("*.{py,js}")
	if len(result) != 2 {
		t.Fatalf("应展开 2 个模式，got %v", result)
	}
	if result[0] != "*.py" || result[1] != "*.js" {
		t.Errorf("应展开为 [*.py *.js]，got %v", result)
	}
}

// TestExpandBracePattern_多个选项 测试多个选项
func TestExpandBracePattern_多个选项(t *testing.T) {
	result := ExpandBracePattern("src/*.{go,md,json}")
	if len(result) != 3 {
		t.Fatalf("应展开 3 个模式，got %v", result)
	}
	expect := []string{"src/*.go", "src/*.md", "src/*.json"}
	for i, e := range expect {
		if result[i] != e {
			t.Errorf("第 %d 项应为 %q，got %q", i, e, result[i])
		}
	}
}

// TestExpandBracePattern_无逗号单选项 测试无逗号组（真实 LLM glob 常见形态）
// 修复前：无逗号组（如 "**/*.{md}"）递归输入原样复现 → 无限递归
// stack overflow（1GB 栈炸测试进程，本地 5 连跑 2 次复现）
func TestExpandBracePattern_无逗号单选项(t *testing.T) {
	// 对齐 Python: expand_group("**/*.{md}") 递归一层后 "**/*.md" 无组 → 终止
	result := ExpandBracePattern("**/*.{md}")
	if len(result) != 1 || result[0] != "**/*.md" {
		t.Errorf("无逗号组应展开为 [**/*.md]，got %v", result)
	}
}

// TestExpandBracePattern_空组 测试空花括号组
// 修复前："{}" 经 capture 索引错位后递归自身；修复后 inner="" 拆出
// 一个空 opt，等价于删除花括号
func TestExpandBracePattern_空组(t *testing.T) {
	result := ExpandBracePattern("a{}b")
	if len(result) != 1 || result[0] != "ab" {
		t.Errorf("空组应展开为 [ab]，got %v", result)
	}
}

// TestExpandBracePattern_尾随逗号 测试尾随逗号（空选项展开为空串分支）
// 对齐 Python: "{a,}" 拆分 ["a", ""] → ["a", ""]（含空串）
func TestExpandBracePattern_尾随逗号(t *testing.T) {
	result := ExpandBracePattern("{a,}")
	if len(result) != 2 || result[0] != "a" || result[1] != "" {
		t.Errorf("尾随逗号应展开为 [a ]，got %v", result)
	}
}

// TestExpandBracePattern_嵌套组 测试多组连续展开
// 对齐 Python: 递归逐组展开，每次处理最左无嵌套组
func TestExpandBracePattern_嵌套组(t *testing.T) {
	result := ExpandBracePattern("{a,b}/{c,d}")
	if len(result) != 4 {
		t.Fatalf("应展开 4 个模式，got %v", result)
	}
	expect := []string{"a/c", "a/d", "b/c", "b/d"}
	for i, e := range expect {
		if result[i] != e {
			t.Errorf("第 %d 项应为 %q，got %q", i, e, result[i])
		}
	}
}

// TestExpandBracePattern_选项空格裁剪 测试选项两侧空格裁剪
// 对齐 Python: opt.strip()
func TestExpandBracePattern_选项空格裁剪(t *testing.T) {
	result := ExpandBracePattern("*.{ py , js }")
	if len(result) != 2 || result[0] != "*.py" || result[1] != "*.js" {
		t.Errorf("选项应裁剪空格后展开为 [*.py *.js]，got %v", result)
	}
}

// TestCatN_空内容 测试空内容
func TestCatN_空内容(t *testing.T) {
	if CatN("") != "" {
		t.Errorf("空内容应返回空字符串")
	}
}

// TestCatN_单行 测试单行
func TestCatN_单行(t *testing.T) {
	result := CatN("hello")
	if !strings.Contains(result, "1") || !strings.Contains(result, "hello") {
		t.Errorf("单行应包含行号和内容，got %q", result)
	}
}

// TestCatN_多行 测试多行
func TestCatN_多行(t *testing.T) {
	result := CatN("line1\nline2\nline3")
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("应有 3 行，got %d", len(lines))
	}
}

// TestIsBlockedDevice_设备路径 测试设备路径
func TestIsBlockedDevice_设备路径(t *testing.T) {
	if !IsBlockedDevice("/dev/zero") {
		t.Errorf("/dev/zero 应为阻塞设备")
	}
	if !IsBlockedDevice("/dev/random") {
		t.Errorf("/dev/random 应为阻塞设备")
	}
	if !IsBlockedDevice("/dev/stdin") {
		t.Errorf("/dev/stdin 应为阻塞设备")
	}
}

// TestIsBlockedDevice_普通路径 测试普通路径
func TestIsBlockedDevice_普通路径(t *testing.T) {
	if IsBlockedDevice("/home/user/file.txt") {
		t.Errorf("普通路径不应为阻塞设备")
	}
}

// TestIsBlockedDevice_procfd 测试 /proc fd 路径
func TestIsBlockedDevice_procfd(t *testing.T) {
	if !IsBlockedDevice("/proc/123/fd/0") {
		t.Errorf("/proc/123/fd/0 应为阻塞设备")
	}
	if !IsBlockedDevice("/proc/123/fd/2") {
		t.Errorf("/proc/123/fd/2 应为阻塞设备")
	}
	if IsBlockedDevice("/proc/123/fd/3") {
		t.Errorf("/proc/123/fd/3 不应为阻塞设备")
	}
}

// TestIsBinaryCandidate_二进制扩展名 测试二进制扩展名
func TestIsBinaryCandidate_二进制扩展名(t *testing.T) {
	if !IsBinaryCandidate("file.exe") {
		t.Errorf(".exe 应为二进制")
	}
	if !IsBinaryCandidate("file.dll") {
		t.Errorf(".dll 应为二进制")
	}
}

// TestIsBinaryCandidate_文本扩展名 测试文本扩展名
func TestIsBinaryCandidate_文本扩展名(t *testing.T) {
	if IsBinaryCandidate("file.go") {
		t.Errorf(".go 不应为二进制")
	}
	if IsBinaryCandidate("file.txt") {
		t.Errorf(".txt 不应为二进制")
	}
}

// TestStripTrailingWhitespace_非Markdown 测试非 Markdown 去除空白
func TestStripTrailingWhitespace_非Markdown(t *testing.T) {
	result := StripTrailingWhitespace("hello   \nworld  \n", false)
	if result != "hello\nworld\n" {
		t.Errorf("应去除行尾空白，got %q", result)
	}
}

// TestStripTrailingWhitespace_Markdown 测试 Markdown 保留空白
func TestStripTrailingWhitespace_Markdown(t *testing.T) {
	input := "hello   \nworld  \n"
	result := StripTrailingWhitespace(input, true)
	if result != input {
		t.Errorf("Markdown 文件应保留行尾空白，got %q", result)
	}
}

// TestStripTrailingWhitespace_空内容 测试空内容
func TestStripTrailingWhitespace_空内容(t *testing.T) {
	result := StripTrailingWhitespace("", false)
	if result != "" {
		t.Errorf("空内容应返回空字符串，got %q", result)
	}
}

// TestStripTrailingWhitespace_CRLF 测试 CRLF 行尾
func TestStripTrailingWhitespace_CRLF(t *testing.T) {
	result := StripTrailingWhitespace("hello   \r\nworld  \r\n", false)
	if result != "hello\r\nworld\r\n" {
		t.Errorf("应保留 CRLF 行尾并去除空白，got %q", result)
	}
}

// TestDetectEOL_LF 测试 LF 行尾
func TestDetectEOL_LF(t *testing.T) {
	if DetectEOL("hello\nworld") != "\n" {
		t.Errorf("LF 行尾应检测为 \\n")
	}
}

// TestDetectEOL_CRLF 测试 CRLF 行尾
func TestDetectEOL_CRLF(t *testing.T) {
	if DetectEOL("hello\r\nworld") != "\r\n" {
		t.Errorf("CRLF 行尾应检测为 \\r\\n")
	}
}

// TestRelativizePaths_绝对路径 测试绝对路径转相对路径
func TestRelativizePaths_绝对路径(t *testing.T) {
	base := "/home/user/project"
	paths := []string{"/home/user/project/src/main.go", "/home/user/project/README.md"}
	result := RelativizePaths(base, paths)
	if len(result) != 2 {
		t.Fatalf("应有 2 个路径，got %d", len(result))
	}
	// 结果应以相对路径形式
	if result[0] != "src/main.go" {
		t.Errorf("第一个路径应为 src/main.go，got %q", result[0])
	}
}

// TestRelativizePaths_不可相对化 测试不可相对化路径
func TestRelativizePaths_不可相对化(t *testing.T) {
	base := "/home/user/project"
	paths := []string{"/etc/passwd"}
	result := RelativizePaths(base, paths)
	if len(result) != 1 {
		t.Fatalf("应有 1 个路径")
	}
	// 不可相对化时保留原路径
	if result[0] == "" {
		t.Errorf("不应为空")
	}
}

// TestGetDestructiveWarning_破坏性命令 测试破坏性命令
func TestGetDestructiveWarning_破坏性命令(t *testing.T) {
	warning := GetDestructiveWarning("git reset --hard HEAD")
	if warning == "" {
		t.Errorf("git reset --hard 应产生警告")
	}
	warning = GetDestructiveWarning("kubectl delete pod my-pod")
	if warning == "" {
		t.Errorf("kubectl delete 应产生警告")
	}
}

// TestGetDestructiveWarning_安全命令 测试安全命令
func TestGetDestructiveWarning_安全命令(t *testing.T) {
	warning := GetDestructiveWarning("ls -la")
	if warning != "" {
		t.Errorf("ls 不应产生警告，got %q", warning)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestExpandGroup 测试递归展开花括号组（直接验证 expandGroup 终止性）
func TestExpandGroup(t *testing.T) {
	// 修复前该输入触发无限递归 stack overflow；此处直接调用确认终止
	result := expandGroup("**/*.{md}")
	if len(result) != 1 || result[0] != "**/*.md" {
		t.Errorf("expandGroup 应展开为 [**/*.md]，got %v", result)
	}
}

// TestExpandGroup_无花括号 测试无花括号
func TestExpandGroup_无花括号(t *testing.T) {
	result := expandGroup("simple.go")
	if len(result) != 1 || result[0] != "simple.go" {
		t.Errorf("无花括号应返回原字符串，got %v", result)
	}
}
