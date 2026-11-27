//go:build windows

package processcli

import (
	"io"
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

// syscallRead Windows 同步读（裸 HANDLE，不经 os.File 包装）。
func syscallRead(fd int, buf []byte) (int, error) {
	n, err := syscall.Read(syscall.Handle(fd), buf)
	if err != nil && err == io.EOF {
		return n, nil
	}
	return n, err
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
		if errno, isErrno := errNo.(syscall.Errno); isErrno &&
			(errno == syscall.ERROR_BROKEN_PIPE || errno == 232 || errno == 233) {
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
	n, err := syscallRead(s.fd, buf)
	if err != nil {
		return nil, &DuplexInputError{Message: "duplex input could not be read"}
	}
	return buf[:n], nil
}

// descriptorSource 借用描述符且不改其阻塞与继承标志的读源。
// Python: duplex_input.py:94-112 _DescriptorSource
type descriptorSource struct {
	// fd 借用的文件描述符
	fd int
	// regular 是否常规文件
	regular bool
	// pipe 平台管道源（Windows 非 git 常规文件恒走 windowsPipeSource）
	pipe duplexSource
}

// newDescriptorSource 构造描述符读源。
// Python: duplex_input.py:97-102（fstat 常规文件判定 + 平台分支）
func newDescriptorSource(file *os.File) (duplexSource, error) {
	fd := int(file.Fd())
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	regular := stat.Mode().IsRegular()
	src := &descriptorSource{fd: fd, regular: regular}
	if !regular {
		// Python: :101-102 nt 平台非常规文件走 _WindowsPipe
		pipe, pipeErr := newPipeSource(fd)
		if pipeErr != nil {
			return nil, pipeErr
		}
		src.pipe = pipe
	}
	return src, nil
}

// readAvailable 按常规文件/平台管道两分支读。
// Python: duplex_input.py:104-112 _DescriptorSource.read_available
func (s *descriptorSource) readAvailable(size int) ([]byte, error) {
	if s.regular {
		// Python: :105-106 常规文件直接 os.read
		return blockingRead(s.fd, size)
	}
	if s.pipe != nil {
		return s.pipe.readAvailable(size)
	}
	return nil, nil
}

// blockingRead 同步读 fd（常规文件的共用出口）。
func blockingRead(fd, size int) ([]byte, error) {
	src := &blockingFileSource{fd: uintptr(fd)}
	return src.readAvailable(size)
}
