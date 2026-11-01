//go:build !linux

package testhelpers

import (
	"errors"
	"os"
	"testing"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPTYUnsupported 当前平台不支持 PTY 终端模拟。
var ErrPTYUnsupported = errors.New("PTY 终端模拟仅在 Linux 支持")

// ──────────────────────────── 导出函数 ────────────────────────────

// OpenPTY 非 Linux 平台恒返回 ErrPTYUnsupported，调用方应 t.Skip。
func OpenPTY() (*os.File, *os.File, error) {
	return nil, nil, ErrPTYUnsupported
}

// WithPTYStdin 非 Linux 平台无实现（OpenPTY 已返回错误，正常不会走到这里）。
func WithPTYStdin(_ *testing.T, _ *os.File) {}

// WritePTYInput 非 Linux 平台无实现（OpenPTY 已返回错误，正常不会走到这里）。
func WritePTYInput(_ *testing.T, _ *os.File, _ string) {}
