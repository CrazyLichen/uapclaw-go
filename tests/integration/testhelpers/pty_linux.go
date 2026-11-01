//go:build linux

package testhelpers

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPTYUnsupported 当前环境无法提供 PTY（/dev/ptmx 缺失等）。
var ErrPTYUnsupported = errors.New("PTY 终端模拟不可用")

// ──────────────────────────── 导出函数 ────────────────────────────

// OpenPTY 打开一对伪终端。
//
// slave 的 Stat().Mode() 含 ModeCharDevice，等价于真实终端——用于覆盖
// 依赖 isatty 判定的交互分支（Go 无 monkeypatch，只能真造终端）。
// 调用方遇错应 t.Skip，不要当作测试失败。
func OpenPTY() (master *os.File, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 打开 /dev/ptmx 失败: %v", ErrPTYUnsupported, err)
	}

	// TIOCGPTN 取从端编号
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("TIOCGPTN 失败: %w", err)
	}

	// TIOCSPTLCK 写 0 解锁从端
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("TIOCSPTLCK 失败: %w", err)
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("打开 /dev/pts/%d 失败: %w", n, err)
	}
	return master, slave, nil
}

// WithPTYStdin 把 slave 装为 os.Stdin，测试结束自动复原。
//
// 必须在读取 stdin 的代码执行前调用；slave 由调用方负责关闭。
func WithPTYStdin(t *testing.T, slave *os.File) {
	t.Helper()
	old := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = old })
}

// WritePTYInput 向 master 写入内容，模拟用户在终端敲键。
//
// 内容需自带换行符：终端行规程（canonical mode）只在收到 \n 时才把
// 输入交给读端。
func WritePTYInput(t *testing.T, master *os.File, input string) {
	t.Helper()
	if _, err := master.WriteString(input); err != nil {
		t.Fatalf("写 PTY 输入失败: %v", err)
	}
}
