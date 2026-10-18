package filesystem

import "testing"

// ──────────────────────────── isHidden 测试 ────────────────────────────

// TestIsHidden_点开头 测试隐藏文件
func TestIsHidden_点开头(t *testing.T) {
	if !isHidden(".gitignore") {
		t.Error(".gitignore 应为隐藏文件")
	}
}

// TestIsHidden_双点开头 测试双点开头
func TestIsHidden_双点开头(t *testing.T) {
	if !isHidden("..") {
		t.Error("\"..\" 应为隐藏文件")
	}
}

// TestIsHidden_普通文件 测试非隐藏文件
func TestIsHidden_普通文件(t *testing.T) {
	if isHidden("main.go") {
		t.Error("main.go 不应为隐藏文件")
	}
}

// TestIsHidden_空字符串 测试空字符串
func TestIsHidden_空字符串(t *testing.T) {
	if isHidden("") {
		t.Error("空字符串不应为隐藏文件")
	}
}

// TestIsHidden_单点 测试单点
func TestIsHidden_单点(t *testing.T) {
	if !isHidden(".") {
		t.Error("\".\" 应为隐藏文件")
	}
}
