package main

import (
	"os"
	"testing"
)

// TestExecute_help 走参数解析路径，断言退出码 0。
//
// 用 --help 避免真正进入 REPL（无 TTY 时会读 stdin 直至 EOF）。
func TestExecute_help(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"uapclaw-process", "--help"}

	if code := Execute(); code != 0 {
		t.Errorf("--help 退出码 = %d, 期望 0", code)
	}
}

// TestExecute_非法参数 未知参数返回 2（输入或控制非法）。
func TestExecute_非法参数(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())
	original := os.Args
	t.Cleanup(func() { os.Args = original })
	os.Args = []string{"uapclaw-process", "--bogus-flag"}

	if code := Execute(); code != 2 {
		t.Errorf("非法参数退出码 = %d, 期望 2", code)
	}
}
