//go:build !windows

package processcli

// unixPipeSource POSIX 管道源：descriptorSource 内部不再细分，
// 零超时 select 已由 descriptorSource 直接实现。
// Python: duplex_input.py:109-112（POSIX 分支内联在 _DescriptorSource）
//
// 本文件仅为平台工厂占位：!windows 下无 PeekNamedPipe 等价物，
// descriptorSource.pipe 保持 nil，全部走 selectReadable。
// Python: _WindowsPipe 仅存在于 nt 平台，无 POSIX 对应。

// newPipeSource !windows 平台恒返回 nil（管道探测由 select 承担）。
// Python: duplex_input.py:101-102 os.name != "nt" 时 _windows_pipe = None
func newPipeSource(fd int) (duplexSource, error) {
	return nil, nil
}
