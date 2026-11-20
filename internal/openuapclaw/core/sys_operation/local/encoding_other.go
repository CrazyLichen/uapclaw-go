//go:build !windows

package local

// systemANSICodePage 非 Windows 平台的占位实现，不参与编译进 Windows 之外
// 的检测路径（detectShellEncoding 在 Windows 分支才调用它）。
// 对应 Windows 实现见 encoding_windows.go。
func systemANSICodePage() string {
	return ""
}
