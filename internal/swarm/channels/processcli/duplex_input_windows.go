//go:build windows

package processcli

import (
	"os"
	"syscall"
	"unsafe"
)

// windowsPipeSource 每次 bounded read 前用 PeekNamedPipe 探测同步打开的
// Windows 管道。Python: duplex_input.py:56-91 _WindowsPipe
type windowsPipeSource struct {
	// fd 借用的文件描述符
	fd int
}

// newPipeSource Windows 管道源工厂（descriptorSource 非 git 常规文件分支）。
// Python: duplex_input.py:101-102 os.name == "nt" and not regular → _WindowsPipe
func newPipeSource(fd int) (duplexSource, error) {
	return &windowsPipeSource{fd: fd}, nil
}

// readAvailable 探测可用字节后再读；EOF 错误码返回空切片，其余报错。
// Python: duplex_input.py:79-91 _WindowsPipe.read_available
func (s *windowsPipeSource) readAvailable(size int) ([]byte, error) {
	handle := os.NewFile(uintptr(s.fd), "").Fd()
	kernel32 := syscall.MustLoadDLL("kernel32.dll")
	peek := kernel32.MustFindProc("PeekNamedPipe")
	var available uint32
	// Python: :80-83 PeekNamedPipe(handle, None, 0, None, &available, None)
	r1, _, errNo := peek.Call(
		handle,
		0, 0, 0,
		uintptr(unsafe.Pointer(&available)),
		0,
	)
	if r1 == 0 {
		// Python: :26 _PIPE_EOF_ERRORS = (109, 232, 233)
		// 109 ERROR_BROKEN_PIPE；232/233 为 no data / pipe ending 变体
		if errNo == syscall.ERROR_BROKEN_PIPE || errNo == 232 || errNo == 233 {
			return []byte{}, nil
		}
		return nil, &DuplexInputError{Message: "cannot inspect the input pipe"}
	}
	// Python: :89-90 无可用字节 → None（暂无数据）
	if available == 0 {
		return nil, nil
	}
	// Python: :91 os.read(fd, min(size, available))
	if int(available) < size {
		size = int(available)
	}
	buf := make([]byte, size)
	n, err := syscall.Read(syscall.Handle(handle), buf)
	if err != nil {
		return nil, &DuplexInputError{Message: "duplex input could not be read"}
	}
	return buf[:n], nil
}
