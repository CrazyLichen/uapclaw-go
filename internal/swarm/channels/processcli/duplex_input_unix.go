//go:build !windows

package processcli

import (
	"io"
	"os"
	"syscall"
)

// descriptorSource 借用描述符且不改其阻塞与继承标志的读源。
// Python: duplex_input.py:94-112 _DescriptorSource
//
// 常规文件直接同步读；其余（管道/tty/socket）用零超时 select 探测。
type descriptorSource struct {
	// fd 借用的文件描述符
	fd int
	// regular 是否常规文件
	regular bool
	// pipe 平台管道源（nil 表示用 select）
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
		// Python: :101-102 非 nt 平台 pipe 为 nil，走 select
		pipe, pipeErr := newPipeSource(fd)
		if pipeErr != nil {
			return nil, pipeErr
		}
		src.pipe = pipe
	}
	return src, nil
}

// readAvailable 按常规文件/平台管道/零超时 select 三分支读。
// Python: duplex_input.py:104-112 _DescriptorSource.read_available
func (s *descriptorSource) readAvailable(size int) ([]byte, error) {
	if s.regular {
		// Python: :105-106 常规文件直接 os.read
		return blockingRead(s.fd, size)
	}
	if s.pipe != nil {
		return s.pipe.readAvailable(size)
	}
	// Python: :109-112 零超时 select：不可读返回 None（暂无数据）
	readable, selErr := selectReadable(s.fd)
	if selErr != nil {
		return nil, selErr
	}
	if !readable {
		return nil, nil
	}
	return blockingRead(s.fd, size)
}

// blockingRead 同步读 fd（常规文件与 select 就绪后的共用出口）。
func blockingRead(fd, size int) ([]byte, error) {
	src := &blockingFileSource{fd: uintptr(fd)}
	return src.readAvailable(size)
}

// syscallRead 同步读 fd（裸 syscall，不经 os.File 包装——重复 NewFile
// 同一 fd 会反复注册 netpoller 并触发终局器，读会间歇失败）。
func syscallRead(fd int, buf []byte) (int, error) {
	for {
		n, err := syscall.Read(fd, buf)
		if err == syscall.EINTR {
			// Go 运行时异步抢占信号会打断慢系统调用（race 检测器下高频），
			// 与 Python os.read 的自动重试语义对齐
			continue
		}
		if err != nil && err == io.EOF {
			return n, nil
		}
		return n, err
	}
}

// selectReadable 零超时 select 探测 fd 是否可读。
// Python: duplex_input.py:109 select.select([fd], [], [], 0)
func selectReadable(fd int) (bool, error) {
	readSet := &syscall.FdSet{}
	// fd 位图：fd/64 为下标，fd%64 为位（Linux amd64 FD_SETSIZE=1024）
	readSet.Bits[fd/64] |= 1 << (uint(fd) % 64)
	// 零超时 = 立即返回
	timeout := syscall.Timeval{Sec: 0, Usec: 0}
	n, err := syscall.Select(fd+1, readSet, nil, nil, &timeout)
	if err != nil && err != syscall.EINTR {
		return false, err
	}
	return n > 0, nil
}
