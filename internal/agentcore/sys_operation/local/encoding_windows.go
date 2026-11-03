//go:build windows

package local

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// systemANSICodePage 返回 Windows ANSI 代码页名（如 "cp936"）。
// Python: locale.getpreferredencoding(False) 在中文 Windows 上返回 "cp936"。
// 仅 Windows 编译（平台绑定，CI linux 无法执行，已登记覆盖率豁免）。
func systemANSICodePage() string {
	return "cp" + strconv.Itoa(int(windows.GetACP()))
}
