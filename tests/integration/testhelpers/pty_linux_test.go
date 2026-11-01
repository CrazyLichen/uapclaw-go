//go:build linux

package testhelpers

import (
	"os"
	"testing"
)

// TestOpenPTY_从端是字符设备 验证 slave 的 Stat 报 ModeCharDevice。
// 这正是 workspace.isInteractive() 的判据：管道/普通文件都不满足。
func TestOpenPTY_从端是字符设备(t *testing.T) {
	master, slave, err := OpenPTY()
	if err != nil {
		t.Fatalf("OpenPTY 失败: %v", err)
	}
	defer master.Close()
	defer slave.Close()

	fi, err := slave.Stat()
	if err != nil {
		t.Fatalf("slave.Stat 失败: %v", err)
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		t.Fatalf("slave 应为字符设备，实际 mode=%v", fi.Mode())
	}
}

// TestOpenPTY_写读闭环 验证 master 写、slave 读的闭环。
func TestOpenPTY_写读闭环(t *testing.T) {
	master, slave, err := OpenPTY()
	if err != nil {
		t.Fatalf("OpenPTY 失败: %v", err)
	}
	defer master.Close()
	defer slave.Close()

	if _, err := master.WriteString("zh\n"); err != nil {
		t.Fatalf("写 master 失败: %v", err)
	}

	buf := make([]byte, 16)
	n, err := slave.Read(buf)
	if err != nil {
		t.Fatalf("读 slave 失败: %v", err)
	}
	if n == 0 {
		t.Fatal("未读到数据")
	}
}
